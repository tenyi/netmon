package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// StatsRepo 管理 stats 表的讀寫。
type StatsRepo struct {
	db *sql.DB
}

// NewStatsRepo 建立 StatsRepo。
func NewStatsRepo(db *sql.DB) *StatsRepo {
	return &StatsRepo{db: db}
}

// Upsert 寫入或更新指定 bucket 的統計資料。
func (r *StatsRepo) Upsert(ctx context.Context, stat Stat) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO stats (bucket_start, latency_avg_ms, loss_pct, sample_count)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(bucket_start) DO UPDATE SET
		   latency_avg_ms = excluded.latency_avg_ms,
		   loss_pct = excluded.loss_pct,
		   sample_count = excluded.sample_count`,
		stat.BucketStart, stat.LatencyAvgMs, stat.LossPct, stat.SampleCount,
	)
	if err != nil {
		return fmt.Errorf("寫入統計失敗: %w", err)
	}
	return nil
}

// List 查詢 bucket_start 落在 [from, to] 內的統計。
func (r *StatsRepo) List(ctx context.Context, from, to int64) ([]Stat, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, bucket_start, latency_avg_ms, loss_pct, sample_count FROM stats
		 WHERE bucket_start >= ? AND bucket_start <= ?
		 ORDER BY bucket_start ASC`,
		from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢統計失敗: %w", err)
	}
	defer rows.Close()

	var stats []Stat
	for rows.Next() {
		var s Stat
		if err := rows.Scan(&s.ID, &s.BucketStart, &s.LatencyAvgMs, &s.LossPct, &s.SampleCount); err != nil {
			return nil, fmt.Errorf("讀取統計列失敗: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代統計列失敗: %w", err)
	}
	if stats == nil {
		stats = []Stat{}
	}
	return stats, nil
}
