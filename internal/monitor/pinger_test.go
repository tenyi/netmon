package monitor

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// shouldRetryPrivileged 是純函式:判斷一次 ping 失敗是否屬於「非特權模式
// 權限不足」,若是則應回退到 raw socket(true)。
// 同時涵蓋 Windows 環境:非特權走 UDP ICMP 會被系統拒絕
// ("protocol has not been configured" / "no implementation for it"),
// 也要回退到 raw socket,否則會被鎖死成「永遠走非特權」永遠失敗。
func TestShouldRetryPrivileged(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"os.ErrPermission", os.ErrPermission, true},
		{"EPERM 字串", errors.New("read icmp: operation not permitted"), true},
		{"permission denied", errors.New("permission denied"), true},
		{"Windows access denied", errors.New("Access is denied."), true},
		{
			"Windows protocol not configured",
			errors.New("socket: The requested protocol has not been configured into the system, or no implementation for it exists."),
			true,
		},
		{"Windows no implementation", errors.New("socket: no implementation for it exists"), true},
		{"一般連線錯誤不回退", errors.New("connection refused"), false},
		{"逾時不回退", errors.New("i/o timeout"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetryPrivileged(tc.err); got != tc.want {
				t.Fatalf("shouldRetryPrivileged(%v) = %v, 想 %v", tc.err, got, tc.want)
			}
		})
	}
}

// fakeAttempt 記錄每次呼叫的 privileged 值,並依序回傳預設結果。
type fakeAttempt struct {
	calls      []bool
	sequence   []attemptResult
	callsCount int
}

type attemptResult struct {
	latency time.Duration
	ok      bool
	err     error
}

func (f *fakeAttempt) call(ctx context.Context, privileged bool) (time.Duration, bool, error) {
	f.calls = append(f.calls, privileged)
	i := f.callsCount
	f.callsCount++
	if i >= len(f.sequence) {
		r := f.sequence[len(f.sequence)-1]
		return r.latency, r.ok, r.err
	}
	return f.sequence[i].latency, f.sequence[i].ok, f.sequence[i].err
}

func newTestPinger(attempt func(ctx context.Context, privileged bool) (time.Duration, bool, error)) *ICMPPinger {
	p := &ICMPPinger{addr: "192.168.1.1", timeout: time.Second}
	p.attempt = attempt
	return p
}

// TestPingTrTriesUnprivilegedFirstThenPrivileged:
// 第一次 unprivileged 遇權限錯誤 → 回退 privileged 成功;
// 之後不再重複探測,直接用已決定的 privileged。
func TestPingTriesUnprivilegedFirstThenPrivileged(t *testing.T) {
	fa := &fakeAttempt{sequence: []attemptResult{
		{err: errors.New("operation not permitted")},
		{latency: 8 * time.Millisecond, ok: true},
	}}
	p := newTestPinger(fa.call)
	ctx := context.Background()

	lat, ok, err := p.Ping(ctx)
	if err != nil {
		t.Fatalf("first Ping err: %v", err)
	}
	if !ok || lat != 8*time.Millisecond {
		t.Fatalf("first Ping = ok:%v lat:%v, 想 ok:true lat:8ms", ok, lat)
	}
	// 第二次:應直接走 privileged(不再重探)
	if _, _, err := p.Ping(ctx); err != nil {
		t.Fatalf("second Ping err: %v", err)
	}

	want := []bool{false, true, true}
	if len(fa.calls) != len(want) {
		t.Fatalf("calls = %v, 想 %v", fa.calls, want)
	}
	for i := range want {
		if fa.calls[i] != want[i] {
			t.Fatalf("calls = %v, 想 %v", fa.calls, want)
		}
	}
}

// TestPingSticksWithUnprivilegedAfterSuccess:
// unprivileged 直接成功 → 保持 unprivileged,不回退 privileged。
func TestPingSticksWithUnprivilegedAfterSuccess(t *testing.T) {
	fa := &fakeAttempt{sequence: []attemptResult{
		{latency: 5 * time.Millisecond, ok: true},
	}}
	p := newTestPinger(fa.call)
	ctx := context.Background()

	if _, _, err := p.Ping(ctx); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, _, err := p.Ping(ctx); err != nil {
		t.Fatalf("second: %v", err)
	}
	want := []bool{false, false}
	if len(fa.calls) != len(want) || fa.calls[0] != false || fa.calls[1] != false {
		t.Fatalf("calls = %v, 想 %v", fa.calls, want)
	}
}

// TestPingReturnsNonPermissionErrorWithoutRetry:
// unprivileged 回非權限錯誤(如 connection refused)→ 不應該再探 privileged,直接回錯。
func TestPingReturnsNonPermissionErrorWithoutRetry(t *testing.T) {
	fa := &fakeAttempt{sequence: []attemptResult{
		{err: errors.New("connection refused")},
		{latency: 1 * time.Millisecond, ok: true}, // 若被誤用會回成功
	}}
	p := newTestPinger(fa.call)

	_, ok, err := p.Ping(context.Background())
	if err == nil {
		t.Fatalf("想回錯,卻回 ok")
	}
	if ok {
		t.Fatalf("想 ok=false,卻 ok=true")
	}
	if len(fa.calls) != 1 || fa.calls[0] != false {
		t.Fatalf("calls = %v, 想只有 [false](未試 privileged)", fa.calls)
	}
}
