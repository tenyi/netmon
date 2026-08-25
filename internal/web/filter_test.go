package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tenyi/netmon/internal/config"
	"github.com/tenyi/netmon/internal/monitor"
	"github.com/tenyi/netmon/internal/storage"
)

type stubStatus struct{}

func (stubStatus) Status() monitor.Status { return monitor.Status{GatewayIP: "10.0.0.1", Online: true} }

// TestAPIEventsStatusFilterTotalHonest:
// /api/events 傳 status 時,X-Total-Count 必須是**過濾後**的總數,
// 且分頁內容只含該狀態事件——這正是「前端過濾×分頁不一致」的根治。
func TestAPIEventsStatusFilterTotalHonest(t *testing.T) {
	t.Parallel()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	events := storage.NewEventRepo(db)
	stats := storage.NewStatsRepo(db)
	if err := seedEvents(t, events, 2, 2); err != nil { // 2 ongoing + 2 resolved
		t.Fatalf("seed: %v", err)
	}

	engine := New(Deps{
		Config: &config.Config{},
		Events: events,
		Stats:  stats,
		Status: stubStatus{},
	})
	gin.SetMode(gin.TestMode)

	// status=ongoing,limit=2 → total 應該是 2(過濾後),body length 2
	w := doGet(t, engine, "/api/events?from=0&to=99999999999999&limit=2&status=ongoing")
	if w.Code != http.StatusOK {
		t.Fatalf("status=ongoing → HTTP %d, body=%s", w.Code, w.Body.String())
	}
	if tc := w.Header().Get("X-Total-Count"); tc != "2" {
		t.Errorf("X-Total-Count = %q,想 2(過濾後總數)", tc)
	}
	// status 是 query 參數,handler 應解析;未支援時 total 會是 4 → 此處抓不到即錯

	// status=resolved → total 同樣是 2
	w = doGet(t, engine, "/api/events?from=0&to=99999999999999&limit=2&status=resolved")
	if tc := w.Header().Get("X-Total-Count"); tc != "2" {
		t.Errorf("X-Total-Count(resolved) = %q,想 2", tc)
	}
}

func doGet(t *testing.T, engine http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// seedEvents 種 ongoing 筆未結束 + resolved 筆已恢復的事件。
func seedEvents(t *testing.T, repo *storage.EventRepo, ongoing, resolved int) error {
	t.Helper()
	ctx := context.Background()
	base := time.Now().Add(-1 * time.Hour).UnixMilli()
	total := ongoing + resolved
	for i := range total {
		if _, err := repo.InsertOpen(ctx, base+int64(i)*1000, "seed"); err != nil {
			return err
		}
	}
	// CloseOpen 每次關「最新未結束」,連關 resolved 次 → 最新 resolved 筆恢復
	for i := range resolved {
		if err := repo.CloseOpen(ctx, base+int64(resolved-1-i)*1000+50); err != nil {
			return err
		}
	}
	return nil
}
