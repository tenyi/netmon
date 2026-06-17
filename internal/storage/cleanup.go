package storage

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// Cleanup 定期刪除過期資料。
type Cleanup struct {
	done chan struct{}
}

// StartCleanup 啟動每小時執行一次的資料清理 goroutine。
func StartCleanup(ctx context.Context, db *sql.DB, retentionDays int) *Cleanup {
	c := &Cleanup{done: make(chan struct{})}
	go c.run(ctx, db, retentionDays)
	return c
}

func (c *Cleanup) run(ctx context.Context, db *sql.DB, retentionDays int) {
	defer close(c.done)

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	c.purge(ctx, db, retentionDays)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.purge(ctx, db, retentionDays)
		}
	}
}

func (c *Cleanup) purge(ctx context.Context, db *sql.DB, retentionDays int) {
	threshold := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()

	for _, table := range []string{"events", "stats"} {
		col := "started_at"
		if table == "stats" {
			col = "bucket_start"
		}
		query := "DELETE FROM " + table + " WHERE " + col + " < ?"
		res, err := db.ExecContext(ctx, query, threshold)
		if err != nil {
			log.Printf("清理 %s 失敗: %v", table, err)
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			log.Printf("已清理 %s 中 %d 筆過期資料", table, n)
		}
	}
}

// Wait 等待清理 goroutine 結束。
func (c *Cleanup) Wait() {
	<-c.done
}
