package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// EventRepo 管理 events 表的讀寫。
type EventRepo struct {
	db *sql.DB
}

// NewEventRepo 建立 EventRepo。
func NewEventRepo(db *sql.DB) *EventRepo {
	return &EventRepo{db: db}
}

// InsertOpen 新增一筆尚未結束的斷線事件。
func (r *EventRepo) InsertOpen(ctx context.Context, startedAt int64, reason string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO events (started_at, ended_at, reason) VALUES (?, NULL, ?)`,
		startedAt, reason,
	)
	if err != nil {
		return 0, fmt.Errorf("新增斷線事件失敗: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("取得事件 ID 失敗: %w", err)
	}
	return id, nil
}

// CloseOpen 將最新一筆未結束事件標記為已恢復。
func (r *EventRepo) CloseOpen(ctx context.Context, endedAt int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET ended_at = ? WHERE id = (
			SELECT id FROM events WHERE ended_at IS NULL ORDER BY started_at DESC LIMIT 1
		)`,
		endedAt,
	)
	if err != nil {
		return fmt.Errorf("關閉斷線事件失敗: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("檢查更新結果失敗: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("找不到未結束的斷線事件")
	}
	return nil
}

// List 查詢 started_at 落在 [from, to] 內的所有事件,依 started_at DESC 排序 (安全上限 5000 筆)。
func (r *EventRepo) List(ctx context.Context, from, to int64) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, started_at, ended_at, reason FROM events
		 WHERE started_at >= ? AND started_at <= ?
		 ORDER BY started_at DESC LIMIT 5000`,
		from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢事件失敗: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var endedAt sql.NullInt64
		if err := rows.Scan(&e.ID, &e.StartedAt, &endedAt, &e.Reason); err != nil {
			return nil, fmt.Errorf("讀取事件列失敗: %w", err)
		}
		if endedAt.Valid {
			v := endedAt.Int64
			e.EndedAt = &v
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代事件列失敗: %w", err)
	}
	if events == nil {
		events = []Event{}
	}
	return events, nil
}

// ListPage 查詢 started_at 落在 [from, to] 內、符合 status 過濾的分頁事件,依 started_at DESC。
// limit 與 offset 由呼叫端保證為非負整數;limit 為 0 時視同無上限。
// status 必須為合法 EventStatus(包含空/all),否則回錯。
func (r *EventRepo) ListPage(ctx context.Context, from, to int64, limit, offset int, status EventStatus) ([]Event, error) {
	if !status.valid() {
		return nil, fmt.Errorf("事件狀態過濾值無效: %q", status)
	}
	if limit < 0 || offset < 0 {
		return nil, fmt.Errorf("limit 與 offset 必須為非負整數")
	}

	query := `SELECT id, started_at, ended_at, reason FROM events
		WHERE started_at >= ? AND started_at <= ?` + status.whereClause() + `
		ORDER BY started_at DESC`
	args := []any{from, to}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢事件失敗: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var endedAt sql.NullInt64
		if err := rows.Scan(&e.ID, &e.StartedAt, &endedAt, &e.Reason); err != nil {
			return nil, fmt.Errorf("讀取事件列失敗: %w", err)
		}
		if endedAt.Valid {
			v := endedAt.Int64
			e.EndedAt = &v
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代事件列失敗: %w", err)
	}
	if events == nil {
		events = []Event{}
	}
	return events, nil
}

// Count 回傳 started_at 落在 [from, to] 內、符合 status 過濾的事件總數。
func (r *EventRepo) Count(ctx context.Context, from, to int64, status EventStatus) (int64, error) {
	if !status.valid() {
		return 0, fmt.Errorf("事件狀態過濾值無效: %q", status)
	}
	var n int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE started_at >= ? AND started_at <= ?`+status.whereClause(),
		from, to,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("查詢事件總數失敗: %w", err)
	}
	return n, nil
}

// GetOpen 取得目前未結束的斷線事件。
func (r *EventRepo) GetOpen(ctx context.Context) (*Event, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, started_at, ended_at, reason FROM events
		 WHERE ended_at IS NULL ORDER BY started_at DESC LIMIT 1`,
	)

	var e Event
	var endedAt sql.NullInt64
	err := row.Scan(&e.ID, &e.StartedAt, &endedAt, &e.Reason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查詢未結束事件失敗: %w", err)
	}
	if endedAt.Valid {
		v := endedAt.Int64
		e.EndedAt = &v
	}
	return &e, nil
}

// CloseOrphanedOpen 自動關閉除最新一筆外的歷史孤兒未結束事件（設 ended_at = started_at），
// 確保資料庫中至多只保留一筆進行中的斷線事件。
func (r *EventRepo) CloseOrphanedOpen(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET ended_at = started_at
		 WHERE ended_at IS NULL AND id NOT IN (
			 SELECT id FROM events WHERE ended_at IS NULL ORDER BY started_at DESC LIMIT 1
		 )`,
	)
	if err != nil {
		return 0, fmt.Errorf("關閉孤兒斷線事件失敗: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("取得受影響列數失敗: %w", err)
	}
	return n, nil
}

// ListPageWithCount 在單一 read transaction 內同時取得分頁事件與總數,
// 確保兩者來自同一個 SQLite snapshot,避免在 ListPage 與 Count 之間有 insert 時造成 X-Total-Count 與實際頁數不一致。
// limit 與 offset 由呼叫端保證為非負整數;limit 為 0 時視同無上限。
// status 必須為合法 EventStatus(包含空/all),否則回錯。
func (r *EventRepo) ListPageWithCount(ctx context.Context, from, to int64, limit, offset int, status EventStatus) ([]Event, int64, error) {
	if !status.valid() {
		return nil, 0, fmt.Errorf("事件狀態過濾值無效: %q", status)
	}
	if limit < 0 || offset < 0 {
		return nil, 0, fmt.Errorf("limit 與 offset 必須為非負整數")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("開啟交易失敗: %w", err)
	}
	// Commit 後 defer Rollback 是 no-op,出錯時保證自動 rollback。
	defer func() { _ = tx.Rollback() }()

	listQuery := `SELECT id, started_at, ended_at, reason FROM events
		WHERE started_at >= ? AND started_at <= ?` + status.whereClause() + `
		ORDER BY started_at DESC`
	listArgs := []any{from, to}
	if limit > 0 {
		listQuery += ` LIMIT ? OFFSET ?`
		listArgs = append(listArgs, limit, offset)
	}

	rows, err := tx.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("查詢事件失敗: %w", err)
	}
	var events []Event
	for rows.Next() {
		var e Event
		var endedAt sql.NullInt64
		if err := rows.Scan(&e.ID, &e.StartedAt, &endedAt, &e.Reason); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("讀取事件列失敗: %w", err)
		}
		if endedAt.Valid {
			v := endedAt.Int64
			e.EndedAt = &v
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("迭代事件列失敗: %w", err)
	}
	rows.Close()

	var total int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE started_at >= ? AND started_at <= ?`+status.whereClause(),
		from, to,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("查詢事件總數失敗: %w", err)
	}

	if events == nil {
		events = []Event{}
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("提交交易失敗: %w", err)
	}
	return events, total, nil
}