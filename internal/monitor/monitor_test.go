package monitor

import (
	"context"
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
