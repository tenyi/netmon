package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/go-ping/ping"
)

// Pinger 執行單次 ICMP 探測。
type Pinger interface {
	Ping(ctx context.Context) (latency time.Duration, ok bool, err error)
}

// ICMPPinger 使用 go-ping 進行 ICMP 探測。
type ICMPPinger struct {
	addr    string
	timeout time.Duration
}

// NewICMPPinger 建立 ICMPPinger。
func NewICMPPinger(addr string, timeout time.Duration) *ICMPPinger {
	return &ICMPPinger{addr: addr, timeout: timeout}
}

// Ping 對目標執行單次 ping。
func (p *ICMPPinger) Ping(ctx context.Context) (time.Duration, bool, error) {
	pinger, err := ping.NewPinger(p.addr)
	if err != nil {
		return 0, false, fmt.Errorf("建立 pinger 失敗: %w", err)
	}

	pinger.SetPrivileged(true)
	pinger.Count = 1
	pinger.Timeout = p.timeout

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			pinger.Stop()
		case <-done:
		}
	}()

	if err := pinger.Run(); err != nil {
		close(done)
		return 0, false, fmt.Errorf("ping 執行失敗: %w", err)
	}
	close(done)

	stats := pinger.Statistics()
	if stats.PacketsRecv == 0 {
		return 0, false, nil
	}

	var latency time.Duration
	if stats.AvgRtt > 0 {
		latency = stats.AvgRtt
	} else if len(stats.Rtts) > 0 {
		latency = stats.Rtts[0]
	}
	return latency, true, nil
}
