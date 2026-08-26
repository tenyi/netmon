package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenFileEnablesWAL(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("QueryRow journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode=wal, got %s", mode)
	}
}

func setupTestDB(t *testing.T) *EventRepo {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewEventRepo(db)
}

func TestEventRepoInsertCloseList(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	startedAt := time.Now().UnixMilli()
	id, err := repo.InsertOpen(ctx, startedAt, "timeout")
	if err != nil {
		t.Fatalf("InsertOpen: %v", err)
	}
	if id < 1 {
		t.Fatalf("expected positive id, got %d", id)
	}

	open, err := repo.GetOpen(ctx)
	if err != nil {
		t.Fatalf("GetOpen: %v", err)
	}
	if open == nil || open.ID != id {
		t.Fatalf("expected open event id %d, got %+v", id, open)
	}

	endedAt := startedAt + 5000
	if err := repo.CloseOpen(ctx, endedAt); err != nil {
		t.Fatalf("CloseOpen: %v", err)
	}

	open, err = repo.GetOpen(ctx)
	if err != nil {
		t.Fatalf("GetOpen after close: %v", err)
	}
	if open != nil {
		t.Fatalf("expected no open event, got %+v", open)
	}

	events, err := repo.List(ctx, startedAt-1, endedAt+1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EndedAt == nil || *events[0].EndedAt != endedAt {
		t.Fatalf("unexpected ended_at: %+v", events[0].EndedAt)
	}
}

// TestEventRepoCloseOpenErrNoOpen 釘住 spec:沒有未結束事件時 CloseOpen 必須回錯。
// 這保證呼叫端若先 GetOpen 判斷 nil,可拿到明確語意而非靜默成功。
func TestEventRepoCloseOpenErrNoOpen(t *testing.T) {
	t.Parallel()
	repo := setupTestDB(t)
	ctx := context.Background()

	if _, err := repo.InsertOpen(ctx, 100, "x"); err != nil {
		t.Fatalf("InsertOpen: %v", err)
	}
	if err := repo.CloseOpen(ctx, 100); err != nil {
		t.Fatalf("first CloseOpen: %v", err)
	}

	if err := repo.CloseOpen(ctx, 200); err == nil {
		t.Fatal("expected error when no open event exists")
	}
}

// TestEventRepoListPageAndCount 驗證分頁與總數:30 筆事件,
// 跨頁讀取應依 started_at DESC 排序,Count 與分頁總和一致。
func TestEventRepoListPageAndCount(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	base := time.Now().Add(-1 * time.Hour).UnixMilli()
	const total = 30
	for i := range total {
		// 間隔 1 秒,確保 started_at 不重複
		if _, err := repo.InsertOpen(ctx, base+int64(i)*1000, "test"); err != nil {
			t.Fatalf("InsertOpen[%d]: %v", i, err)
		}
	}

	from := base - 1
	to := base + int64(total)*1000 + 1

	count, err := repo.Count(ctx, from, to, EventStatusAll)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != total {
		t.Fatalf("expected count %d, got %d", total, count)
	}

	// 第一頁:應為最新的 10 筆 (i=29..20)
	page1, err := repo.ListPage(ctx, from, to, 10, 0, EventStatusAll)
	if err != nil {
		t.Fatalf("ListPage 1: %v", err)
	}
	if len(page1) != 10 {
		t.Fatalf("page1 expected 10, got %d", len(page1))
	}
	if page1[0].StartedAt != base+int64(total-1)*1000 {
		t.Fatalf("page1[0] expected newest, got %d", page1[0].StartedAt)
	}
	if page1[9].StartedAt != base+int64(total-10)*1000 {
		t.Fatalf("page1[9] expected 10th newest, got %d", page1[9].StartedAt)
	}

	// 第二頁:接續 10 筆 (i=19..10)
	page2, err := repo.ListPage(ctx, from, to, 10, 10, EventStatusAll)
	if err != nil {
		t.Fatalf("ListPage 2: %v", err)
	}
	if len(page2) != 10 {
		t.Fatalf("page2 expected 10, got %d", len(page2))
	}
	if page2[0].StartedAt != base+int64(total-11)*1000 {
		t.Fatalf("page2[0] expected 20th newest, got %d", page2[0].StartedAt)
	}

	// 第三頁:剩餘 10 筆 (i=9..0)
	page3, err := repo.ListPage(ctx, from, to, 10, 20, EventStatusAll)
	if err != nil {
		t.Fatalf("ListPage 3: %v", err)
	}
	if len(page3) != 10 {
		t.Fatalf("page3 expected 10, got %d", len(page3))
	}
	if page3[9].StartedAt != base {
		t.Fatalf("page3[9] expected oldest, got %d", page3[9].StartedAt)
	}

	// 越界:offset 超出總數應回空 slice
	pageBeyond, err := repo.ListPage(ctx, from, to, 10, 100, EventStatusAll)
	if err != nil {
		t.Fatalf("ListPage beyond: %v", err)
	}
	if len(pageBeyond) != 0 {
		t.Fatalf("pageBeyond expected 0, got %d", len(pageBeyond))
	}

	// limit=0 表示無上限,應回全部
	all, err := repo.ListPage(ctx, from, to, 0, 0, EventStatusAll)
	if err != nil {
		t.Fatalf("ListPage no limit: %v", err)
	}
	if len(all) != total {
		t.Fatalf("ListPage(0,0) expected %d, got %d", total, len(all))
	}

	// 負數應回錯
	if _, err := repo.ListPage(ctx, from, to, -1, 0, EventStatusAll); err == nil {
		t.Fatal("expected error for negative limit")
	}
	if _, err := repo.ListPage(ctx, from, to, 10, -1, EventStatusAll); err == nil {
		t.Fatal("expected error for negative offset")
	}
}

func setupStatsDB(t *testing.T) *StatsRepo {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewStatsRepo(db)
}

// TestSinkGetOpenEvent:Sink 應暴露「目前未結束事件」供 monitor 重啟時 reconciliation。
func TestSinkGetOpenEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ev := NewEventRepo(db)
	st := NewStatsRepo(db)
	c := NewSink(ev, st)

	if _, err := ev.InsertOpen(ctx, 111, "stale"); err != nil {
		t.Fatalf("InsertOpen: %v", err)
	}
	got, err := c.GetOpenEvent(ctx)
	if err != nil {
		t.Fatalf("GetOpenEvent: %v", err)
	}
	if got == nil || got.StartedAt != 111 || got.Reason != "stale" {
		t.Fatalf("expected stale open event, got %+v", got)
	}

	if err := ev.CloseOpen(ctx, 222); err != nil {
		t.Fatalf("CloseOpen: %v", err)
	}
	got, err = c.GetOpenEvent(ctx)
	if err != nil {
		t.Fatalf("GetOpenEvent after close: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after close, got %+v", got)
	}
}

// TestCloseOrphanedOpen:驗證多筆歷史未結束事件時，CloseOrphanedOpen 會關閉舊事件並保留最新一筆。
func TestCloseOrphanedOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := NewEventRepo(db)

	if _, err := repo.InsertOpen(ctx, 100, "old1"); err != nil {
		t.Fatalf("InsertOpen 1: %v", err)
	}
	if _, err := repo.InsertOpen(ctx, 200, "old2"); err != nil {
		t.Fatalf("InsertOpen 2: %v", err)
	}
	id3, err := repo.InsertOpen(ctx, 300, "newest")
	if err != nil {
		t.Fatalf("InsertOpen 3: %v", err)
	}

	closedCount, err := repo.CloseOrphanedOpen(ctx)
	if err != nil {
		t.Fatalf("CloseOrphanedOpen: %v", err)
	}
	if closedCount != 2 {
		t.Fatalf("expected 2 closed events, got %d", closedCount)
	}

	open, err := repo.GetOpen(ctx)
	if err != nil {
		t.Fatalf("GetOpen: %v", err)
	}
	if open == nil || open.ID != id3 || open.StartedAt != 300 {
		t.Fatalf("expected newest open event (id=%d, start=300), got %+v", id3, open)
	}

	events, err := repo.List(ctx, 0, 400)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events in total, got %d", len(events))
	}
	for _, e := range events {
		if e.ID == id3 {
			if e.EndedAt != nil {
				t.Fatalf("newest event should remain open, got ended_at: %v", e.EndedAt)
			}
		} else {
			if e.EndedAt == nil || *e.EndedAt != e.StartedAt {
				t.Fatalf("orphaned event %d should be resolved with ended_at=started_at, got ended_at: %v", e.ID, e.EndedAt)
			}
		}
	}
}

// TestSinkReconcileOpen 驗證 Sink.ReconcileOpen 為薄 wrapper,回傳清理筆數並具冪等性。
func TestSinkReconcileOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	events := NewEventRepo(db)
	sink := NewSink(events, NewStatsRepo(db))

	if _, err := events.InsertOpen(ctx, 100, "old"); err != nil {
		t.Fatalf("InsertOpen 1: %v", err)
	}
	if _, err := events.InsertOpen(ctx, 200, "newest"); err != nil {
		t.Fatalf("InsertOpen 2: %v", err)
	}

	n, err := sink.ReconcileOpen(ctx)
	if err != nil {
		t.Fatalf("ReconcileOpen: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 orphan closed, got %d", n)
	}

	n2, err := sink.ReconcileOpen(ctx)
	if err != nil {
		t.Fatalf("ReconcileOpen 2: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 on second call, got %d", n2)
	}
}

func TestStatsRepoUpsertList(t *testing.T) {
	repo := setupStatsDB(t)
	ctx := context.Background()

	bucket := time.Now().Truncate(time.Minute).UnixMilli()
	stat := Stat{
		BucketStart:  bucket,
		LatencyAvgMs: 12.5,
		LossPct:      0,
		SampleCount:  10,
	}
	if err := repo.Upsert(ctx, stat); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	stat.LatencyAvgMs = 15.0
	stat.SampleCount = 20
	if err := repo.Upsert(ctx, stat); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	stats, err := repo.List(ctx, bucket-1, bucket+1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].LatencyAvgMs != 15.0 || stats[0].SampleCount != 20 {
		t.Fatalf("unexpected stat: %+v", stats[0])
	}
}

func TestCleanupPurgesOldData(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	events := NewEventRepo(db)
	stats := NewStatsRepo(db)
	ctx := context.Background()

	oldTime := time.Now().Add(-48 * time.Hour).UnixMilli()
	newTime := time.Now().UnixMilli()

	if _, err := events.InsertOpen(ctx, oldTime, "timeout"); err != nil {
		t.Fatalf("InsertOpen old: %v", err)
	}
	if _, err := events.InsertOpen(ctx, newTime, "timeout"); err != nil {
		t.Fatalf("InsertOpen new: %v", err)
	}
	if err := stats.Upsert(ctx, Stat{BucketStart: oldTime, LatencyAvgMs: 1, LossPct: 0, SampleCount: 1}); err != nil {
		t.Fatalf("Upsert old: %v", err)
	}
	if err := stats.Upsert(ctx, Stat{BucketStart: newTime, LatencyAvgMs: 1, LossPct: 0, SampleCount: 1}); err != nil {
		t.Fatalf("Upsert new: %v", err)
	}

	c := &Cleanup{}
	c.purge(ctx, db, 1)

	oldEvents, err := events.List(ctx, 0, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("List events: %v", err)
	}
	if len(oldEvents) != 1 {
		t.Fatalf("expected 1 remaining event, got %d", len(oldEvents))
	}
	if oldEvents[0].StartedAt != newTime {
		t.Fatalf("expected new event to remain, got %+v", oldEvents[0])
	}

	allStats, err := stats.List(ctx, 0, time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("List stats: %v", err)
	}
	if len(allStats) != 1 {
		t.Fatalf("expected 1 remaining stat, got %d", len(allStats))
	}
}
