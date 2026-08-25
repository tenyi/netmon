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

// EventStatus 是 events API 的狀態過濾值。
type EventStatus string

const (
	// EventStatusAll 不過濾(全部事件)。
	EventStatusAll EventStatus = "all"
	// EventStatusOngoing 目前未結束的斷線事件(ended_at IS NULL)。
	EventStatusOngoing EventStatus = "ongoing"
	// EventStatusResolved 已恢復的事件(ended_at IS NOT NULL)。
	EventStatusResolved EventStatus = "resolved"
)

// valid 回報是否為受認可的過濾值;空字串視同 all。
func (s EventStatus) valid() bool {
	if s == "" {
		return true
	}
	switch s {
	case EventStatusAll, EventStatusOngoing, EventStatusResolved:
		return true
	}
	return false
}

// whereClause 回傳該過濾的 SQL 片段(帶前導空白 + AND;all/空為 "" )。
func (s EventStatus) whereClause() string {
	switch s {
	case EventStatusOngoing:
		return " AND ended_at IS NULL"
	case EventStatusResolved:
		return " AND ended_at IS NOT NULL"
	default: // EventStatusAll / ""
		return ""
	}
}
