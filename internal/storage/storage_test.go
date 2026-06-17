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
