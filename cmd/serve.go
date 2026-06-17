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

	srv := &http.Server{
		Addr:    cfg.WebAddr,
		Handler: engine,
	}

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
