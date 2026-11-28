package codex

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/sunfayao/persona/server/internal/transcript"
)

type HistoryQuery func(context.Context, string, string, any) error

type indexedRow struct {
	Ordinal     int64  `json:"history_ordinal"`
	TurnID      string `json:"turn_id"`
	Status      string `json:"status"`
	DurationMS  int64  `json:"duration_ms"`
	ErrorJSON   string `json:"error_json"`
	CreatedAtMS int64  `json:"created_at_ms"`
	ItemType    string `json:"item_type"`
	ItemJSON    string `json:"item_json"`
}

// ReadHistory projects SQLite rows read in one snapshot through the same timeline mapper as a rollout.
// A running terminal turn is readable. An app turn with its own live feed is visible when it finishes.
func ReadHistory(ctx context.Context, db string, thread AppThread, query HistoryQuery) ([]transcript.CodexRecord, error) {
	cte, err := HistoryTurnsSQL(filepath.Dir(db), thread)
	if err != nil {
		return nil, err
	}
	const selectRows = `
SELECT t.history_ordinal, t.turn_id, t.status, coalesce(t.duration_ms, -1) AS duration_ms,
       coalesce(t.error_json, '') AS error_json, coalesce(i.created_at_ms, 0) AS created_at_ms,
       coalesce(i.item_type, '') AS item_type, coalesce(i.item_json, '') AS item_json
FROM history t
LEFT JOIN thread_items i ON i.thread_id = t.thread_id AND i.turn_id = t.turn_id
ORDER BY t.history_ordinal, i.rollout_ordinal
`
	var rows []indexedRow
	if err := query(ctx, db, cte+selectRows, &rows); err != nil {
		return nil, err
	}
	var records []transcript.CodexRecord
	appendRecord := func(at time.Time, kind string, payload any) {
		data, _ := json.Marshal(payload)
		records = append(records, transcript.CodexRecord{Timestamp: at, Type: kind, Payload: data})
	}
	var cursor time.Time
	if thread.Paginated && len(rows) == 0 {
		appendRecord(cursor, "session_meta", map[string]string{"id": thread.ID, "cwd": thread.Cwd, "originator": "Codex"})
	}
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].Ordinal == rows[start].Ordinal {
			end++
		}
		turn := rows[start]
		switch turn.Status {
		case "completed", "failed", "interrupted":
		default:
			if end < len(rows) {
				turn.Status = "interrupted"
			} else if !thread.IncludeRunning {
				return records, nil
			}
		}
		if cursor.IsZero() {
			cursor = time.UnixMilli(turn.CreatedAtMS).UTC()
			originator := "Codex Desktop"
			if thread.Paginated && thread.Source != "vscode" {
				originator = "Codex"
			}
			appendRecord(cursor, "session_meta", map[string]string{"id": thread.ID, "cwd": thread.Cwd, "originator": originator})
		}
		if turn.CreatedAtMS > 0 {
			cursor = time.UnixMilli(turn.CreatedAtMS).UTC()
		}
		appendRecord(cursor, "event_msg", map[string]string{"type": "task_started", "turn_id": turn.TurnID})
		for _, row := range rows[start:end] {
			if row.CreatedAtMS > 0 {
				cursor = time.UnixMilli(row.CreatedAtMS).UTC()
			}
			if record, ok := ThreadItemRecord(cursor, turn.TurnID, row.ItemType, []byte(row.ItemJSON)); ok {
				records = append(records, record)
			}
		}
		switch turn.Status {
		case "interrupted":
			appendRecord(cursor, "event_msg", map[string]string{"type": "turn_aborted", "turn_id": turn.TurnID})
		case "completed", "failed":
			close := map[string]any{"type": "task_complete", "turn_id": turn.TurnID}
			if turn.Status == "failed" {
				if json.Valid([]byte(turn.ErrorJSON)) {
					close["error"] = json.RawMessage(turn.ErrorJSON)
				} else {
					close["error"] = map[string]string{"message": transcript.CodexTurnFailed}
				}
			}
			if turn.DurationMS >= 0 {
				close["duration_ms"] = turn.DurationMS
			}
			appendRecord(cursor, "event_msg", close)
		}
		start = end
	}
	return records, nil
}
