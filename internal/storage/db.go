package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/glebarez/go-sqlite"
)

// Open 開啟 SQLite 資料庫，必要時建立父目錄。
func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("建立資料庫目錄失敗: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("開啟資料庫失敗: %w", err)
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("連線資料庫失敗: %w", err)
	}

	return db, nil
}

// Migrate 建立資料表與索引。
func Migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			started_at INTEGER NOT NULL,
			ended_at INTEGER,
			reason TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_started_at ON events(started_at)`,
		`CREATE TABLE IF NOT EXISTS stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket_start INTEGER NOT NULL UNIQUE,
			latency_avg_ms REAL NOT NULL,
			loss_pct REAL NOT NULL,
			sample_count INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_stats_bucket_start ON stats(bucket_start)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("執行遷移失敗: %w", err)
		}
	}
	return nil
}
