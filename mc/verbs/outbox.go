package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
)

func validateDeliverySurface(surface string) error {
	switch surface {
	case "discord", "dashboard", "cli":
		return nil
	default:
		return Usagef("outbox surface must be discord|dashboard|cli (§15.5)")
	}
}

// OutboxPoll is a native surface's read-only delivery cursor. Polling never
// claims or hides a row: delivery remains at-least-once until the owning
// surface confirms it through ack (§15.5).
func OutboxPoll(db *sql.DB, id *RunIdentity, surface string, limit int) (any, error) {
	if err := RequireHostScope(id, "mc outbox poll"); err != nil {
		return nil, err
	}
	if err := validateDeliverySurface(surface); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, Usagef("mc outbox poll --limit must be 1..1000")
	}
	rows, err := db.Query(`
		SELECT id, kind, session_id, surface, channel_ref, payload, created_at
		FROM outbox
		WHERE surface = ? AND delivered_at IS NULL
		ORDER BY id LIMIT ?`, surface, limit)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var rowID int64
		var kind, ownerSurface, payloadJSON, createdAt string
		var sessionID, channelRef sql.NullString
		if err := rows.Scan(
			&rowID, &kind, &sessionID, &ownerSurface, &channelRef,
			&payloadJSON, &createdAt,
		); err != nil {
			return nil, classify(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, classify(err)
		}
		var session, channel any
		if sessionID.Valid {
			session = sessionID.String
		}
		if channelRef.Valid {
			channel = channelRef.String
		}
		out = append(out, map[string]any{
			"id": rowID, "kind": kind, "session_id": session,
			"surface": ownerSurface, "channel_ref": channel,
			"payload": payload, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, classify(err)
	}
	return map[string]any{"surface": surface, "rows": out}, nil
}

// OutboxAck marks exactly one row delivered, but only when the caller names
// the row's owning surface. Same-surface replay preserves the first delivery
// timestamp and succeeds idempotently.
func OutboxAck(db *sql.DB, id *RunIdentity, rowID int64, surface string) (any, error) {
	if err := RequireHostScope(id, "mc outbox ack"); err != nil {
		return nil, err
	}
	if err := validateDeliverySurface(surface); err != nil {
		return nil, err
	}
	if rowID < 1 {
		return nil, Usagef("mc outbox ack requires a positive row id")
	}
	acked := false
	deliveredAt := ""
	err := inTx(db, func(ctx context.Context, q Q) error {
		var owner string
		var delivered sql.NullString
		err := q.QueryRowContext(ctx,
			`SELECT surface, delivered_at FROM outbox WHERE id = ?`, rowID,
		).Scan(&owner, &delivered)
		if err == sql.ErrNoRows {
			return Domainf("unknown outbox row %d", rowID)
		}
		if err != nil {
			return err
		}
		if owner != surface {
			return Domainf("outbox row %d belongs to surface %q, not %q", rowID, owner, surface)
		}
		if delivered.Valid {
			deliveredAt = delivered.String
			return nil
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE outbox SET delivered_at = datetime('now')
			WHERE id = ? AND surface = ? AND delivered_at IS NULL`, rowID, surface); err != nil {
			return err
		}
		if err := q.QueryRowContext(ctx,
			`SELECT delivered_at FROM outbox WHERE id = ?`, rowID,
		).Scan(&deliveredAt); err != nil {
			return err
		}
		acked = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": rowID, "surface": surface,
		"acked": acked, "delivered_at": deliveredAt,
	}, nil
}
