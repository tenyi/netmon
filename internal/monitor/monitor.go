package monitor

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/tenyi/netmon/internal/config"
)

type connState int

const (
	stateUnknown connState = iota
	stateOnline
	stateOffline
)

// Monitor 週期性 ping gateway 並偵測狀態變化。
type Monitor struct {
	cfg    *config.Config
	sink   EventSink
	pinger Pinger

	mu            sync.RWMutex
	state         connState
	lastLatencyMs *float64
	lastCheckAt   int64
	openReason    string
	openStartedAt int64

	bucketStart      int64
	bucketTotal      int
	bucketFail       int
	bucketLatencySum float64
	bucketSuccess    int
}

// New 建立 Monitor。
func New(cfg *config.Config, sink EventSink, pinger Pinger) *Monitor {
	if pinger == nil {
		pinger = NewICMPPinger(cfg.GatewayIP, cfg.PingTimeout)
	}
	return &Monitor{
		cfg:    cfg,
		sink:   sink,
		pinger: pinger,
		state:  stateUnknown,
	}
}

// Run 啟動 ping 主迴圈，直到 ctx 取消。
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.PingInterval)
	defer ticker.Stop()

	m.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runOnce(ctx)
		}
	}
}

func (m *Monitor) runOnce(ctx context.Context) {
	now := time.Now()
	latency, ok, err := m.pinger.Ping(ctx)
	if err != nil {
		log.Printf("ping 錯誤: %v（請確認是否以管理員權限執行）", err)
	}

	checkAt := now.UnixMilli()
	m.recordSample(now, ok, latency)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastCheckAt = checkAt
	if ok {
		ms := float64(latency.Microseconds()) / 1000.0
		m.lastLatencyMs = &ms
	} else {
		m.lastLatencyMs = nil
	}

	switch m.state {
	case stateUnknown:
		if ok {
			m.state = stateOnline
		} else {
			m.state = stateOffline
			m.openReason = disconnectReason(err)
			m.openStartedAt = checkAt
			if err := m.sink.OnDisconnect(ctx, checkAt, m.openReason); err != nil {
				log.Printf("寫入斷線事件失敗: %v", err)
			}
		}
	case stateOnline:
		if !ok {
			m.state = stateOffline
			m.openReason = disconnectReason(err)
			m.openStartedAt = checkAt
			if err := m.sink.OnDisconnect(ctx, checkAt, m.openReason); err != nil {
				log.Printf("寫入斷線事件失敗: %v", err)
			}
		}
	case stateOffline:
		if ok {
			m.state = stateOnline
			m.openReason = ""
			m.openStartedAt = 0
			if err := m.sink.OnRecover(ctx, checkAt); err != nil {
				log.Printf("寫入恢復事件失敗: %v", err)
			}
		}
	}
}

func disconnectReason(err error) string {
	if err != nil {
		return "error"
	}
	return "unreachable"
}

func (m *Monitor) recordSample(now time.Time, ok bool, latency time.Duration) {
	bucket := now.Truncate(m.cfg.StatsInterval).UnixMilli()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.bucketStart == 0 {
		m.bucketStart = bucket
	}

	if bucket != m.bucketStart {
		m.flushBucket(context.Background())
		m.bucketStart = bucket
	}

	m.bucketTotal++
	if !ok {
		m.bucketFail++
		return
	}
	m.bucketSuccess++
	m.bucketLatencySum += float64(latency.Microseconds()) / 1000.0
}

func (m *Monitor) flushBucket(ctx context.Context) {
	if m.bucketTotal == 0 {
		return
	}

	var avgMs float64
	if m.bucketSuccess > 0 {
		avgMs = m.bucketLatencySum / float64(m.bucketSuccess)
	}
	lossPct := float64(m.bucketFail) / float64(m.bucketTotal) * 100

	if err := m.sink.OnStats(ctx, m.bucketStart, avgMs, lossPct, m.bucketTotal); err != nil {
		log.Printf("寫入統計失敗: %v", err)
	}

	m.bucketTotal = 0
	m.bucketFail = 0
	m.bucketSuccess = 0
	m.bucketLatencySum = 0
}

// Status 回傳即時監控狀態。
func (m *Monitor) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st := Status{
		GatewayIP:     m.cfg.GatewayIP,
		LastCheckAt:   m.lastCheckAt,
		LastLatencyMs: m.lastLatencyMs,
	}

	switch m.state {
	case stateUnknown:
		st.Unknown = true
		st.Online = false
	case stateOnline:
		st.Online = true
	case stateOffline:
		st.Online = false
		st.OpenEvent = &OpenEvent{
			StartedAt: m.openStartedAt,
			Reason:    m.openReason,
		}
	}
	return st
}
