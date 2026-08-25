package monitor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-ping/ping"
)

// Pinger 執行單次 ICMP 探測。
type Pinger interface {
	Ping(ctx context.Context) (latency time.Duration, ok bool, err error)
}

// attempt 執行「一發」ping 的低層抽象:privileged=true 走 raw socket,
// false 走 UDP/ICMP datagram(多數平台免 root)。抽出來讓「先非特權、
// 遇權限錯誤才回退」的順序可用 fake 測試,不需要真實 socket。
type attempt func(ctx context.Context, privileged bool) (latency time.Duration, ok bool, err error)

// ICMPPinger 使用 go-ping 進行 ICMP 探測。
//
// 注意:go-ping v1.2.0 的 *ping.Pinger 在一次 Run() 結束後,其內部 done
// channel 已被 close,第二次 Run() 會立即結束而幾乎不发包,因此此處每次
// Ping 都新建一個 *ping.Pinger(對 IP literal 而言 NewPinger 很輕量,re-
// resolve 已因每次新建而自然避免)。不要試圖 cache 重用。
type ICMPPinger struct {
	addr    string
	timeout time.Duration

	mu      sync.Mutex
	priv    bool
	decided bool

	// attempt 可於測試注入;NewICMPPinger 預設為 goPingAttempt。
	attempt attempt
}

// NewICMPPinger 建立 ICMPPinger。
func NewICMPPinger(addr string, timeout time.Duration) *ICMPPinger {
	p := &ICMPPinger{addr: addr, timeout: timeout}
	p.attempt = p.goPingAttempt
	return p
}

// Ping 對目標執行單次 ping。
//
// 策略:先嘗試非特權(免 root);若失敗且錯誤屬於權限不足,回退到
// privileged 重試一次,並記住結果,之後直接沿用,避免每 tick 重複探測。
func (p *ICMPPinger) Ping(ctx context.Context) (time.Duration, bool, error) {
	p.mu.Lock()
	attempt := p.attempt
	priv, decided := p.priv, p.decided
	p.mu.Unlock()

	if decided {
		// 已決定模式,直接沿用(常見路徑,零探測開銷)。
		return attempt(ctx, priv)
	}

	// 首次:先非特權。
	lat, ok, err := attempt(ctx, false)
	if err != nil && shouldRetryPrivileged(err) {
		// 權限不足 → 回退 raw socket。
		lat, ok, err = attempt(ctx, true)
		p.mu.Lock()
		p.priv, p.decided = true, true
		p.mu.Unlock()
	} else {
		// 非權限錯誤(含成功、逾時、其他 I/O 錯誤):固定為非特權。
		p.mu.Lock()
		p.priv, p.decided = false, true
		p.mu.Unlock()
	}
	return lat, ok, err
}

// shouldRetryPrivileged 回報 err 是否屬於「非特權 ping 權限不足」,該情況
// 應回退到 raw socket。純函式,可獨立測試。
func shouldRetryPrivileged(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "operation not permitted") ||
		strings.Contains(s, "permission denied") ||
		strings.Contains(s, "access is denied")
}

// goPingAttempt 實際呼叫 go-ping 完成單發 ping。
func (p *ICMPPinger) goPingAttempt(ctx context.Context, privileged bool) (time.Duration, bool, error) {
	pr, err := ping.NewPinger(p.addr)
	if err != nil {
		return 0, false, fmt.Errorf("建立 pinger 失敗: %w", err)
	}

	pr.SetPrivileged(privileged)
	pr.Count = 1
	pr.Timeout = p.timeout

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			pr.Stop()
		case <-done:
		}
	}()

	if err := pr.Run(); err != nil {
		close(done)
		return 0, false, err
	}
	close(done)

	stats := pr.Statistics()
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
