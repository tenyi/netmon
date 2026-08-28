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
// 取消後會把累積中(未滿桶)的統計 flush 出去,避免資料遺失。
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.PingInterval)
	defer ticker.Stop()
	defer m.flushPending()

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

type sinkOp int

const (
	opDisconnect sinkOp = iota
	opRecover
)

// pendingOp 記下狀態翻轉要發出的事件,在解鎖後才真正呼叫 sink,
// 避免 DB I/O 在 Monitor 鎖內執行(會阻塞 Status 等並發讀取)。
type pendingOp struct {
	kind   sinkOp
	at     int64
	reason string
}

func (m *Monitor) runOnce(ctx context.Context) {
	now := time.Now()
	latency, ok, err := m.pinger.Ping(ctx)
	if err != nil {
		// 已在 ICMPPinger 內嘗試非特權 → privileged 的 fallback (Linux/macOS 權限不足、
		// Windows protocol not configured 都會回退)。若仍印出此錯誤,表示 fallback
		// 後仍失敗 — 多為 Windows 防火牆 / GPO 阻擋 ICMP,或 socket 系統限制。
		// 不再加誤導性「請確認 admin」後綴,直接印原始錯誤供排查。
		log.Printf("ping 錯誤: %v", err)
	}
	// ctx 已取消時,本次 ping 的 !ok 多半來自 shutdown,語意上不該被當成真實斷線,
	// 否則 sink 在 OnDisconnect(OnRecover) 內會因 ctx 失敗,事件從此漏掉。
	ctxCanceled := ctx.Err() != nil

	checkAt := now.UnixMilli()
	m.recordSample(now, ok, latency)

	// 重啟 reconciliation:首次確定狀態前查一次 DB 是否有殘留的
	// 未結束事件。DB 讀取放在鎖外,避免 I/O 阻塞 Status。
	var openEvt *OpenEvent
	if m.stateIsUnknown() {
		openEvt = m.lookupOpenEvent(ctx)
	}

	m.mu.Lock()
	m.lastCheckAt = checkAt
	if ok {
		ms := float64(latency.Microseconds()) / 1000.0
		m.lastLatencyMs = &ms
	} else {
		m.lastLatencyMs = nil
	}

	var ops []pendingOp
	switch m.state {
	case stateUnknown:
		if ok {
			m.state = stateOnline
			if openEvt != nil {
				// 斷線在監控停機期間已恢復:補記恢復事件
				if !ctxCanceled {
					ops = append(ops, pendingOp{kind: opRecover, at: checkAt})
				}
			}
		} else {
			m.state = stateOffline
			reason := disconnectReason(err)
			if openEvt != nil {
				// 斷線在監控停機前就開始:視為延續,沿用舊事件不重複插入
				m.openStartedAt = openEvt.StartedAt
				if openEvt.Reason != "" {
					reason = openEvt.Reason
				}
			} else {
				m.openStartedAt = checkAt
				if !ctxCanceled {
					ops = append(ops, pendingOp{kind: opDisconnect, at: checkAt, reason: reason})
				}
			}
			m.openReason = reason
		}
	case stateOnline:
		if !ok && !ctxCanceled {
			m.state = stateOffline
			reason := disconnectReason(err)
			m.openReason = reason
			m.openStartedAt = checkAt
			ops = append(ops, pendingOp{kind: opDisconnect, at: checkAt, reason: reason})
		}
	case stateOffline:
		if ok && !ctxCanceled {
			m.state = stateOnline
			m.openReason = ""
			m.openStartedAt = 0
			ops = append(ops, pendingOp{kind: opRecover, at: checkAt})
		}
	}
	m.mu.Unlock()

	// 鎖外執行 sink I/O
	for _, op := range ops {
		if op.kind == opDisconnect {
			if err := m.sink.OnDisconnect(ctx, op.at, op.reason); err != nil {
				log.Printf("寫入斷線事件失敗: %v", err)
			}
		} else {
			if err := m.sink.OnRecover(ctx, op.at); err != nil {
				log.Printf("寫入恢復事件失敗: %v", err)
			}
		}
	}
}

func (m *Monitor) stateIsUnknown() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state == stateUnknown
}

// OpenEventInspector 是 EventSink 可選擇實作的介面:讓 monitor 在首次
// 確定狀態時,能查詢 DB 中殘留的未結束事件(重啟 reconciliation)。
type OpenEventInspector interface {
	GetOpenEvent(ctx context.Context) (*OpenEvent, error)
}

func (m *Monitor) lookupOpenEvent(ctx context.Context) *OpenEvent {
	insp, ok := m.sink.(OpenEventInspector)
	if !ok {
		return nil
	}
	ev, err := insp.GetOpenEvent(ctx)
	if err != nil {
		log.Printf("查詢進行中斷線事件失敗: %v", err)
		return nil
	}
	return ev
}

func disconnectReason(err error) string {
	if err != nil {
		return "error"
	}
	return "unreachable"
}

// bucketData 是一個統計桶的快照(在鎖內取出,鎖外使用)。
type bucketData struct {
	start      int64
	total      int
	fail       int
	success    int
	latencySum float64
}

func (b *bucketData) metrics() (avgMs, lossPct float64, samples int) {
	samples = b.total
	if b.success > 0 {
		avgMs = b.latencySum / float64(b.success)
	}
	if b.total > 0 {
		lossPct = float64(b.fail) / float64(b.total) * 100
	}
	return avgMs, lossPct, samples
}

func (m *Monitor) recordSample(now time.Time, ok bool, latency time.Duration) {
	bucket := now.Truncate(m.cfg.StatsInterval).UnixMilli()

	var snapshot *bucketData
	m.mu.Lock()
	if m.bucketStart == 0 {
		m.bucketStart = bucket
	}
	if bucket != m.bucketStart {
		snapshot = m.takeBucket()
		m.bucketStart = bucket
	}
	m.bucketTotal++
	if !ok {
		m.bucketFail++
	} else {
		m.bucketSuccess++
		m.bucketLatencySum += float64(latency.Microseconds()) / 1000.0
	}
	m.mu.Unlock()

	// 桶翻轉時發射:在鎖外呼叫 sink
	if snapshot != nil {
		m.emitStats(context.Background(), snapshot)
	}
}

// takeBucket 快照並清空目前累積的桶(呼叫端需已持有 m.mu)。
func (m *Monitor) takeBucket() *bucketData {
	b := &bucketData{
		start:      m.bucketStart,
		total:      m.bucketTotal,
		fail:       m.bucketFail,
		success:    m.bucketSuccess,
		latencySum: m.bucketLatencySum,
	}
	m.bucketTotal = 0
	m.bucketFail = 0
	m.bucketSuccess = 0
	m.bucketLatencySum = 0
	return b
}

func (m *Monitor) emitStats(ctx context.Context, b *bucketData) {
	if b.total == 0 {
		return
	}
	avgMs, lossPct, n := b.metrics()
	if err := m.sink.OnStats(ctx, b.start, avgMs, lossPct, n); err != nil {
		log.Printf("寫入統計失敗: %v", err)
	}
}

// flushPending 把尚未滿桶的樣本 flush 出去(Run 結束時呼叫,避免資料遺失)。
func (m *Monitor) flushPending() {
	m.mu.Lock()
	b := m.takeBucket()
	m.mu.Unlock()
	m.emitStats(context.Background(), b)
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
