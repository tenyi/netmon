package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tenyi/netmon/internal/config"
)

type fakeSink struct {
	mu          sync.Mutex
	disconnects []int64
	recovers    []int64
	stats       []statRecord
}

type statRecord struct {
	bucketStart  int64
	latencyAvgMs float64
	lossPct      float64
	sampleCount  int
}

func (f *fakeSink) OnDisconnect(_ context.Context, startedAt int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects = append(f.disconnects, startedAt)
	return nil
}

func (f *fakeSink) OnRecover(_ context.Context, endedAt int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recovers = append(f.recovers, endedAt)
	return nil
}

func (f *fakeSink) OnStats(_ context.Context, bucketStart int64, latencyAvgMs, lossPct float64, sampleCount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats = append(f.stats, statRecord{
		bucketStart:  bucketStart,
		latencyAvgMs: latencyAvgMs,
		lossPct:      lossPct,
		sampleCount:  sampleCount,
	})
	return nil
}

type sequencePinger struct {
	results []pingResult
	index   int
}

type pingResult struct {
	latency time.Duration
	ok      bool
	err     error
}

func (s *sequencePinger) Ping(_ context.Context) (time.Duration, bool, error) {
	if s.index >= len(s.results) {
		r := s.results[len(s.results)-1]
		return r.latency, r.ok, r.err
	}
	r := s.results[s.index]
	s.index++
	return r.latency, r.ok, r.err
}

func TestMonitorDisconnectAndRecover(t *testing.T) {
	sink := &fakeSink{}
	pinger := &sequencePinger{results: []pingResult{
		{latency: 10 * time.Millisecond, ok: true},
		{ok: false},
		{latency: 12 * time.Millisecond, ok: true},
	}}

	cfg := &config.Config{
		GatewayIP:     "192.168.1.1",
		PingInterval:  time.Hour,
		PingTimeout:   time.Second,
		StatsInterval: time.Minute,
	}

	mon := New(cfg, sink, pinger)
	ctx := context.Background()

	mon.runOnce(ctx)
	st := mon.Status()
	if st.Unknown || !st.Online {
		t.Fatalf("expected online after first success, got %+v", st)
	}

	mon.runOnce(ctx)
	st = mon.Status()
	if st.Online || st.OpenEvent == nil {
		t.Fatalf("expected offline with open event, got %+v", st)
	}
	if len(sink.disconnects) != 1 {
		t.Fatalf("expected 1 disconnect, got %d", len(sink.disconnects))
	}

	mon.runOnce(ctx)
	st = mon.Status()
	if !st.Online {
		t.Fatalf("expected online after recover, got %+v", st)
	}
	if len(sink.recovers) != 1 {
		t.Fatalf("expected 1 recover, got %d", len(sink.recovers))
	}
}

func newTestConfig() *config.Config {
	return &config.Config{
		GatewayIP:     "192.168.1.1",
		PingInterval:  time.Hour,
		PingTimeout:   time.Second,
		StatsInterval: time.Minute,
	}
}

// TestMonitorStatusNotBlockedWhileSinkWriting:
// sink 寫入中(可能很慢)時,Status() 不應該被阻塞 → sink I/O 不能在 Monitor 鎖內執行。
func TestMonitorStatusNotBlockedWhileSinkWriting(t *testing.T) {
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	mon := New(newTestConfig(), sink, &sequencePinger{results: []pingResult{
		{ok: false, err: errors.New("timeout")},
	}})

	go mon.runOnce(context.Background())
	<-sink.entered // sink 開始執行(內部會阻塞到 release)

	statusDone := make(chan bool, 1)
	go func() { _ = mon.Status(); statusDone <- true }()
	select {
	case <-statusDone:
	case <-time.After(time.Second):
		t.Fatal("Status() 被 sink 寫入阻塞(sink I/O 在鎖內?)")
	}
	close(sink.release)
}

// blockingSink 在 OnDisconnect 內阻塞,直到 release 被 close。
type blockingSink struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s *blockingSink) OnDisconnect(context.Context, int64, string) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return nil
}
func (s *blockingSink) OnRecover(context.Context, int64) error                      { return nil }
func (s *blockingSink) OnStats(context.Context, int64, float64, float64, int) error { return nil }

// TestMonitorFlushesPendingBucketOnShutdown:
// 關閉時累積中(未滿桶)的統計樣本應該被 flush,避免資料遺失。
func TestMonitorFlushesPendingBucketOnShutdown(t *testing.T) {
	sink := &recordingSink{}
	cfg := newTestConfig()
	cfg.PingInterval = 10 * time.Millisecond
	cfg.StatsInterval = time.Hour // 桶很大,樣本必然累積中

	mon := New(cfg, sink, &sequencePinger{results: []pingResult{
		{latency: 10 * time.Millisecond, ok: true},
		{latency: 10 * time.Millisecond, ok: true},
	}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { mon.Run(ctx); close(done) }()

	time.Sleep(80 * time.Millisecond) // 讓它跑幾個 tick
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel 後 Run 未回傳")
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.stats) != 1 {
		t.Fatalf("預期關閉時 flush 出 1 筆 stats,實際 %d 筆", len(sink.stats))
	}
	if sink.stats[0].sampleCount < 1 {
		t.Fatalf("stats 樣本數應大於 0,實際 %+v", sink.stats[0])
	}
	if sink.stats[0].latencyAvgMs != 10 {
		t.Fatalf("latencyAvgMs 應為 10,實際 %+v", sink.stats[0])
	}
}

// recordingSink 記錄 OnStats 呼叫。
type recordingSink struct {
	mu    sync.Mutex
	stats []statRecord
}

func (s *recordingSink) OnDisconnect(context.Context, int64, string) error { return nil }
func (s *recordingSink) OnRecover(context.Context, int64) error            { return nil }
func (s *recordingSink) OnStats(_ context.Context, start int64, avg, loss float64, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = append(s.stats, statRecord{bucketStart: start, latencyAvgMs: avg, lossPct: loss, sampleCount: n})
	return nil
}

// TestMonitorReconcilesStaleOpenEvent:
// 重啟時 DB 裡可能有「進行中」的舊斷線事件:
//   - 首次 ping 成功 → 補記恢復(關閉舊事件)
//   - 首次 ping 失敗 → 延遲是延續性,沿用舊事件(不重複插入新事件)
//   - 沒有舊事件 → 原行為(插入新斷線事件)
func TestMonitorReconcilesStaleOpenEvent(t *testing.T) {
	cfg := newTestConfig()
	stale := &OpenEvent{StartedAt: 111, Reason: "stale"}

	// A: 有舊事件 + 首次成功
	sink := &fakeSink2{openEvent: stale}
	mon := New(cfg, sink, &sequencePinger{results: []pingResult{
		{latency: 5 * time.Millisecond, ok: true},
	}})
	mon.runOnce(context.Background())
	if sink.recovers != 1 || sink.disconnects != 0 {
		t.Fatalf("A: 預期 recovers=1/disconnects=0,實際 recovers=%d/disconnects=%d", sink.recovers, sink.disconnects)
	}
	if st := mon.Status(); !st.Online || st.OpenEvent != nil {
		t.Fatalf("A: 預期 online 且無 open event,實際 %+v", st)
	}

	// B: 有舊事件 + 首次失敗 → 沿用舊事件、不插入、不關閉
	sink = &fakeSink2{openEvent: stale}
	mon = New(cfg, sink, &sequencePinger{results: []pingResult{
		{ok: false, err: errors.New("timeout")},
	}})
	mon.runOnce(context.Background())
	if sink.recovers != 0 || sink.disconnects != 0 {
		t.Fatalf("B: 預期 recovers=0/disconnects=0,實際 recovers=%d/disconnects=%d", sink.recovers, sink.disconnects)
	}
	if st := mon.Status(); st.Online || st.OpenEvent == nil ||
		st.OpenEvent.StartedAt != 111 || st.OpenEvent.Reason != "stale" {
		t.Fatalf("B: 預期 offline 且沿用舊事件,實際 %+v", st)
	}

	// C: 無舊事件 + 首次失敗 → 插入新斷線事件(原行為)
	sink = &fakeSink2{openEvent: nil}
	mon = New(cfg, sink, &sequencePinger{results: []pingResult{
		{ok: false, err: errors.New("timeout")},
	}})
	mon.runOnce(context.Background())
	if sink.disconnects != 1 || sink.recovers != 0 {
		t.Fatalf("C: 預期 disconnects=1/recovers=0,實際 disconnects=%d/recovers=%d", sink.disconnects, sink.recovers)
	}
}

// fakeSink2 額外實作 OpenEventInspector(可查詢殘留未結束事件)。
type fakeSink2 struct {
	mu          sync.Mutex
	openEvent   *OpenEvent
	disconnects int
	recovers    int
}

func (f *fakeSink2) OnDisconnect(context.Context, int64, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects++
	return nil
}
func (f *fakeSink2) OnRecover(context.Context, int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recovers++
	return nil
}
func (f *fakeSink2) OnStats(context.Context, int64, float64, float64, int) error { return nil }
func (f *fakeSink2) GetOpenEvent(context.Context) (*OpenEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openEvent, nil
}

func TestMonitorStatsBucket(t *testing.T) {
	sink := &fakeSink{}
	pinger := &sequencePinger{results: []pingResult{
		{latency: 10 * time.Millisecond, ok: true},
		{latency: 20 * time.Millisecond, ok: true},
	}}

	cfg := &config.Config{
		GatewayIP:     "192.168.1.1",
		PingInterval:  time.Hour,
		PingTimeout:   time.Second,
		StatsInterval: time.Millisecond,
	}

	mon := New(cfg, sink, pinger)
	ctx := context.Background()

	mon.runOnce(ctx)
	time.Sleep(2 * time.Millisecond)
	mon.runOnce(ctx)

	if len(sink.stats) < 1 {
		t.Fatalf("expected stats to be flushed, got %d", len(sink.stats))
	}
}
