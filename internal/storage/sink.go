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

// GetOpenEvent 回傳目前未結束的斷線事件;沒有時回 (nil, nil)。
// 供 monitor 首次確定狀態時使用(搭配 monitor.OpenEventInspector)。
// 本函式為純讀:若 DB 存在多筆歷史孤兒未結束事件,需先呼叫 ReconcileOpen 清理。
func (s *Sink) GetOpenEvent(ctx context.Context) (*monitor.OpenEvent, error) {
	e, err := s.events.GetOpen(ctx)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, nil
	}
	return &monitor.OpenEvent{StartedAt: e.StartedAt, Reason: e.Reason}, nil
}

// ReconcileOpen 關閉除最新一筆外的歷史孤兒未結束事件,回傳清理筆數與錯誤。
// 設計為啟動期顯式呼叫,不是熱路徑讀取副作用。
func (s *Sink) ReconcileOpen(ctx context.Context) (int64, error) {
	return s.events.CloseOrphanedOpen(ctx)
}

// 確保 Sink 實作 monitor.EventSink。
var _ monitor.EventSink = (*Sink)(nil)

// 確保 Sink 實作 monitor.OpenEventInspector。
var _ monitor.OpenEventInspector = (*Sink)(nil)
