package storage

// Event 代表一筆斷線/恢復事件。
type Event struct {
	ID        int64  `json:"id"`
	StartedAt int64  `json:"started_at"`
	EndedAt   *int64 `json:"ended_at"`
	Reason    string `json:"reason"`
}

// Stat 代表一個統計時間桶的彙總資料。
type Stat struct {
	ID           int64   `json:"id"`
	BucketStart  int64   `json:"bucket_start"`
	LatencyAvgMs float64 `json:"latency_avg_ms"`
	LossPct      float64 `json:"loss_pct"`
	SampleCount  int     `json:"sample_count"`
}
