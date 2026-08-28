package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Pinger 執行單次 ICMP 探測。
type Pinger interface {
	Ping(ctx context.Context) (latency time.Duration, ok bool, err error)
}

// attempt 執行「一發」ping 的低層抽象:privileged=true 走 raw socket,
// false 走 UDP/ICMP datagram(多數平台免 root)。抽出來讓「先非特權、
// 遇權限錯誤才回退」的順序可用 fake 測試,不需要真實 socket。
type attempt func(ctx context.Context, privileged bool) (latency time.Duration, ok bool, err error)

// ICMPPinger 用 golang.org/x/net/icmp 標準庫發 ICMP echo request。
//
// 為何不用 github.com/go-ping/ping:該專案已 deprecated (無 maintainer),
// go vet 持續警告。x/net/icmp 是 Go 官方標準庫,生命週期綁定 Go 版本。
//
// 注意:每次 Ping 都新建 ICMP socket (對 IP literal 而言 ListenPacket
// 很輕量),不要試圖 cache 重用 — UDP/ICMP 模式下的 socket 狀態會跨呼叫
// 累積,raw socket 也可能受系統狀態影響。
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

// shouldRetryPrivileged 回報 err 是否屬於「非特權 ping 不可用,該回退到 raw socket」。
//
// 涵蓋:
//   - Linux/macOS 權限錯誤 (os.ErrPermission、"operation not permitted"、
//     "permission denied")。
//   - Windows 沒有 UDP ICMP datagram 路徑,SetPrivileged(false) 會回
//     "The requested protocol has not been configured into the system,
//     or no implementation for it exists." — 此時也應回退到 raw socket,
//     否則會被鎖死成「永遠走非特權」每次都失敗。
//
// 純函式,可獨立測試。
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
		strings.Contains(s, "access is denied") ||
		// Windows:非特權走 UDP ICMP 失敗時的系統錯誤字串
		strings.Contains(s, "protocol has not been configured") ||
		strings.Contains(s, "no implementation for it")
}

// protocolICMP 是 ICMPv4 的 protocol number (RFC 792),用於 icmp.ParseMessage。
const protocolICMP = 1

// goPingAttempt 實際呼叫 x/net/icmp 完成單發 ping。
//
// privileged=true:用 raw socket ("ip4:icmp") — Windows / Linux root。
// privileged=false:用 UDP datagram socket ("udp4") — Linux/macOS 免 root。
func (p *ICMPPinger) goPingAttempt(ctx context.Context, privileged bool) (time.Duration, bool, error) {
	network := "ip4:icmp"
	bindAddr := "0.0.0.0"
	if !privileged {
		network = "udp4"
		bindAddr = "0.0.0.0:0" // UDP socket 需 bind port,讓 kernel 分配
	}

	conn, err := icmp.ListenPacket(network, bindAddr)
	if err != nil {
		return 0, false, fmt.Errorf("icmp listen (%s) 失敗: %w", network, err)
	}
	defer conn.Close()

	// deadline:優先用 ctx,否則 fallback 到 p.timeout
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(p.timeout)
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return 0, false, fmt.Errorf("set read deadline 失敗: %w", err)
	}

	dst, err := net.ResolveIPAddr("ip4", p.addr)
	if err != nil {
		return 0, false, fmt.Errorf("解析 %s 失敗: %w", p.addr, err)
	}

	// 組 echo request。ID 用 PID 區隔不同 process,Seq 用 1 (單發)。
	echo := icmp.Echo{
		ID:   os.Getpid() & 0xffff,
		Seq:  1,
		Data: []byte("netmon"),
	}
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &echo,
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return 0, false, fmt.Errorf("marshal icmp 失敗: %w", err)
	}

	sent := time.Now()
	if privileged {
		// raw socket:dst 為 *net.IPAddr
		if _, err := conn.WriteTo(wb, dst); err != nil {
			return 0, false, fmt.Errorf("send icmp 失敗: %w", err)
		}
	} else {
		// udp4 socket:dst 需包成 *net.UDPAddr (digineo/go-ping 內部也是這樣處理)
		if _, err := conn.WriteTo(wb, &net.UDPAddr{IP: dst.IP, Zone: dst.Zone}); err != nil {
			return 0, false, fmt.Errorf("send icmp 失敗: %w", err)
		}
	}

	// Read loop:過濾 echo reply 且 ID/Seq 對應自己 request
	rb := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(rb)
		if err != nil {
			// deadline 到期會回 "i/o timeout"
			return 0, false, err
		}
		rm, err := icmp.ParseMessage(protocolICMP, rb[:n])
		if err != nil {
			continue
		}
		if rm.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		reply, ok := rm.Body.(*icmp.Echo)
		if !ok || reply.ID != echo.ID || reply.Seq != echo.Seq {
			continue
		}
		return time.Since(sent), true, nil
	}
}
