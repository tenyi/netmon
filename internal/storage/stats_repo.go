package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// DefaultStatsListLimit 為 StatsRepo.List 預設上限,保護查詢端避免單次回傳過大。
const DefaultStatsListLimit = 1024

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

// List 查詢 bucket_start 落在 [from, to] 內的統計,最多 limit 筆。
// limit <= 0 時使用 DefaultStatsListLimit (1024),避免 handler 一口氣拉回大量 bucket。
func (r *StatsRepo) List(ctx context.Context, from, to int64, limit int) ([]Stat, error) {
	if limit <= 0 {
		limit = DefaultStatsListLimit
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, bucket_start, latency_avg_ms, loss_pct, sample_count FROM stats
		 WHERE bucket_start >= ? AND bucket_start <= ?
		 ORDER BY bucket_start ASC LIMIT ?`,
		from, to, limit,
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

// ListAggregated 在 SQL 內依 granularityMs 重新分桶,以 sample_count 為權重做加權平均。
// 先彙總再 LIMIT (取最新 limit 桶後依時間升冪回傳),避免長區間時最新資料被截掉。
// limit <= 0 時使用 DefaultStatsListLimit;granularityMs <= 0 退回 List。
func (r *StatsRepo) ListAggregated(ctx context.Context, from, to, granularityMs int64, limit int) ([]Stat, error) {
	if granularityMs <= 0 {
		return r.List(ctx, from, to, limit)
	}
	if limit <= 0 {
		limit = DefaultStatsListLimit
	}
	// 內層取最新 limit 桶 (DESC),外層再反轉為時間升冪
	rows, err := r.db.QueryContext(ctx,
		`SELECT bucket_start, latency_avg_ms, loss_pct, sample_count FROM (
		   SELECT (bucket_start / ?1) * ?1 AS bucket_start,
		          CASE WHEN SUM(sample_count) > 0
		               THEN SUM(latency_avg_ms * sample_count) * 1.0 / SUM(sample_count) ELSE 0 END AS latency_avg_ms,
		          CASE WHEN SUM(sample_count) > 0
		               THEN SUM(loss_pct * sample_count) * 1.0 / SUM(sample_count) ELSE 0 END AS loss_pct,
		          SUM(sample_count) AS sample_count
		   FROM stats
		   WHERE bucket_start >= ?2 AND bucket_start <= ?3
		   GROUP BY bucket_start / ?1
		   ORDER BY 1 DESC LIMIT ?4
		 ) ORDER BY bucket_start ASC`,
		granularityMs, from, to, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢彙總統計失敗: %w", err)
	}
	defer rows.Close()

	stats := []Stat{}
	for rows.Next() {
		var s Stat
		if err := rows.Scan(&s.BucketStart, &s.LatencyAvgMs, &s.LossPct, &s.SampleCount); err != nil {
			return nil, fmt.Errorf("讀取彙總統計列失敗: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代彙總統計列失敗: %w", err)
	}
	return stats, nil
}
