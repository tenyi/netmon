package storage

import (
	"context"
	"testing"
	"time"
)

// TestEventRepoListPageCountFiltered:
// 狀態過濾(ongoing/resolved)要同時作用於**分頁**與**總數**——
// 這樣 API 的 X-Total-Count 才反映「過濾後」的真實筆數,而不是全區間總數。
func TestEventRepoListPageCountFiltered(t *testing.T) {
	t.Parallel()
	repo := setupTestDB(t)
	ctx := context.Background()

	base := time.Now().Add(-1 * time.Hour).UnixMilli()
	// 4 ongoing + 4 resolved,依序錯開 1s
	for i := range 8 {
		id, err := repo.InsertOpen(ctx, base+int64(i)*1000, "test")
		if err != nil {
			t.Fatalf("InsertOpen[%d]: %v", i, err)
		}
		// 偶數 index → resolved(設 ended_at);奇數 → ongoing(NULL)
		if i%2 == 0 {
			if err := closeByID(ctx, repo, id, base+int64(i)*1000+100); err != nil {
				t.Fatalf("close[%d]: %v", i, err)
			}
		}
	}

	from := base - 1
	to := base + 9*1000

	// Count:過濾後各自為 4,all 為 8
	if n, err := repo.Count(ctx, from, to, EventStatusAll); err != nil || n != 8 {
		t.Fatalf("Count(all) = (%d,%v),想 (8,nil)", n, err)
	}
	if n, err := repo.Count(ctx, from, to, EventStatusOngoing); err != nil || n != 4 {
		t.Fatalf("Count(ongoing) = (%d,%v),想 (4,nil)", n, err)
	}
	if n, err := repo.Count(ctx, from, to, EventStatusResolved); err != nil || n != 4 {
		t.Fatalf("Count(resolved) = (%d,%v),想 (4,nil)", n, err)
	}

	// ListPage:過濾後只回該狀態事件
	if evs, err := repo.ListPage(ctx, from, to, 100, 0, EventStatusOngoing); err != nil || len(evs) != 4 {
		t.Fatalf("ListPage(ongoing) = (len %d, %v),想 (4,nil)", len(evs), err)
	} else {
		for _, e := range evs {
			if e.EndedAt != nil {
				t.Fatalf("ongoing 事件不該有 ended_at,得到 %+v", e)
			}
		}
	}
	if evs, err := repo.ListPage(ctx, from, to, 100, 0, EventStatusResolved); err != nil || len(evs) != 4 {
		t.Fatalf("ListPage(resolved) = (len %d,%v),想 (4,nil)", len(evs), err)
	} else {
		for _, e := range evs {
			if e.EndedAt == nil {
				t.Fatalf("resolved 事件不該缺 ended_at,得到 %+v", e)
			}
		}
	}

	// 非法 status → error
	if _, err := repo.ListPage(ctx, from, to, 10, 0, EventStatus("bogus")); err == nil {
		t.Fatal("invalid status 應該回錯")
	}
	if _, err := repo.Count(ctx, from, to, EventStatus("bogus")); err == nil {
		t.Fatal("invalid status 應該回錯")
	}
}

// closeByID 輔助:將指定 id 的事件標記為 resolved。
func closeByID(ctx context.Context, r *EventRepo, id, endedAt int64) error {
	_, err := r.db.ExecContext(ctx, "UPDATE events SET ended_at = ? WHERE id = ?", endedAt, id)
	return err
}
