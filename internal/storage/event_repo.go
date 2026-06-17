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

// List 查詢 started_at 落在 [from, to] 內的事件。
func (r *EventRepo) List(ctx context.Context, from, to int64) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, started_at, ended_at, reason FROM events
		 WHERE started_at >= ? AND started_at <= ?
		 ORDER BY started_at DESC`,
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
