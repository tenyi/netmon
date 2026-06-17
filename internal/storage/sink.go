package storage

import (
	"context"

	"github.com/tenyi/netmon/internal/monitor"
)

// Sink 將 monitor 事件寫入 SQLite。
type Sink struct {
	events *EventRepo
	stats  *StatsRepo
}

// NewSink 建立 Sink。
func NewSink(events *EventRepo, stats *StatsRepo) *Sink {
	return &Sink{events: events, stats: stats}
}

// OnDisconnect 記錄斷線事件。
func (s *Sink) OnDisconnect(ctx context.Context, startedAt int64, reason string) error {
	_, err := s.events.InsertOpen(ctx, startedAt, reason)
	return err
}

// OnRecover 關閉未結束的斷線事件。
func (s *Sink) OnRecover(ctx context.Context, endedAt int64) error {
	return s.events.CloseOpen(ctx, endedAt)
}

// OnStats 寫入統計桶。
func (s *Sink) OnStats(ctx context.Context, bucketStart int64, latencyAvgMs, lossPct float64, sampleCount int) error {
	return s.stats.Upsert(ctx, Stat{
		BucketStart:  bucketStart,
		LatencyAvgMs: latencyAvgMs,
		LossPct:      lossPct,
		SampleCount:  sampleCount,
	})
}

// 確保 Sink 實作 monitor.EventSink。
var _ monitor.EventSink = (*Sink)(nil)
