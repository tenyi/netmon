package monitor

import "context"

// EventSink 接收 monitor 產生的斷線、恢復與統計事件。
type EventSink interface {
	OnDisconnect(ctx context.Context, startedAt int64, reason string) error
	OnRecover(ctx context.Context, endedAt int64) error
	OnStats(ctx context.Context, bucketStart int64, latencyAvgMs, lossPct float64, sampleCount int) error
}

// OpenEvent 描述進行中的斷線事件摘要。
type OpenEvent struct {
	StartedAt int64  `json:"started_at"`
	Reason    string `json:"reason"`
}

// Status 代表 gateway 的即時監控狀態。
type Status struct {
	GatewayIP     string     `json:"gateway_ip"`
	Online        bool       `json:"online"`
	Unknown       bool       `json:"unknown"`
	LastLatencyMs *float64   `json:"last_latency_ms"`
	LastCheckAt   int64      `json:"last_check_at"`
	OpenEvent     *OpenEvent `json:"open_event,omitempty"`
}

// StatusProvider 提供即時狀態查詢。
type StatusProvider interface {
	Status() Status
}
