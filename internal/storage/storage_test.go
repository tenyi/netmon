package storage

import (
	"context"
	"testing"
	"time"
)

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

	count, err := repo.Count(ctx, from, to)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != total {
		t.Fatalf("expected count %d, got %d", total, count)
	}

	// 第一頁:應為最新的 10 筆 (i=29..20)
	page1, err := repo.ListPage(ctx, from, to, 10, 0)
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
	page2, err := repo.ListPage(ctx, from, to, 10, 10)
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
	page3, err := repo.ListPage(ctx, from, to, 10, 20)
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
	pageBeyond, err := repo.ListPage(ctx, from, to, 10, 100)
	if err != nil {
		t.Fatalf("ListPage beyond: %v", err)
	}
	if len(pageBeyond) != 0 {
		t.Fatalf("pageBeyond expected 0, got %d", len(pageBeyond))
	}

	// limit=0 表示無上限,應回全部
	all, err := repo.ListPage(ctx, from, to, 0, 0)
	if err != nil {
		t.Fatalf("ListPage no limit: %v", err)
	}
	if len(all) != total {
		t.Fatalf("ListPage(0,0) expected %d, got %d", total, len(all))
	}

	// 負數應回錯
	if _, err := repo.ListPage(ctx, from, to, -1, 0); err == nil {
		t.Fatal("expected error for negative limit")
	}
	if _, err := repo.ListPage(ctx, from, to, 10, -1); err == nil {
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
