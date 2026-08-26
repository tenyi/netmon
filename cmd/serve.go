package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tenyi/netmon/internal/config"
	"github.com/tenyi/netmon/internal/monitor"
	"github.com/tenyi/netmon/internal/storage"
	"github.com/tenyi/netmon/internal/web"
)

var serveCmd = &cobra.Command{
	Use:           "serve",
	Short:         "啟動監控與 Web 服務",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServe()
	},
}

func init() {
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runServe()
	}
}

// newServer 建立帶齊 timeout 的 http.Server。
//
//	ReadHeaderTimeout 5s  防 Slowloris(慢速 header 攻擊)
//	ReadTimeout     30s  讀取整個 request 的期限
//	WriteTimeout    30s  寫完 response 的期限
//	IdleTimeout    120s  keep-alive 閒置連線生命週期(大於讀寫上限)
//
// 本服務皆是小 response(HTML/JSON)且無 WebSocket/SSE/長輪詢,
// 固定 30s 上限足敷,不會正常請求被截斷。
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func runServe() error {
	cfg, err := config.LoadFromEnv(configPath)
	if err != nil {
		return fmt.Errorf("載入設定失敗: %w", err)
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		return err
	}

	eventRepo := storage.NewEventRepo(db)
	statsRepo := storage.NewStatsRepo(db)
	sink := storage.NewSink(eventRepo, statsRepo)

	// 啟動期 reconciliation:清理 DB 中除最新一筆外的歷史孤兒未結束事件。
	// 失敗只 log 不中斷啟動,讓服務先上線由人工處理。
	if n, err := sink.ReconcileOpen(context.Background()); err != nil {
		log.Printf("啟動期清理孤兒斷線事件失敗: %v", err)
	} else if n > 0 {
		log.Printf("已清理 %d 筆孤兒斷線事件", n)
	}

	mon := monitor.New(cfg, sink, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mon.Run(ctx)
	cleanup := storage.StartCleanup(ctx, db, cfg.RetentionDays)

	engine := web.New(web.Deps{
		Config: cfg,
		Events: eventRepo,
		Stats:  statsRepo,
		Status: mon,
	})

	srv := newServer(cfg.WebAddr, engine)

	go func() {
		log.Printf("Web 服務啟動於 %s", cfg.WebAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web 服務錯誤: %v", err)
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("正在關閉服務...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Web 服務關閉錯誤: %v", err)
	}

	cleanup.Wait()
	log.Println("服務已停止")
	return nil
}
