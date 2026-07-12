package verbs

import (
	"context"
	"database/sql"
)

// Heartbeat is the pipeline runner's private lifecycle verb (§10, §11.5,
// contract §2): lock.last_heartbeat_at = datetime('now') iff run_id matches
// the live lease (fenced — a zombie run can neither renew its old lease nor
// touch the new holder's). It can never extend hard_deadline_at (Inv. 1).
func Heartbeat(db *sql.DB, runID string) (any, error) {
	var stamped string
	err := inTx(db, func(ctx context.Context, q Q) error {
		res, err := q.ExecContext(ctx, `
			UPDATE lock SET last_heartbeat_at = datetime('now')
			WHERE id = 1 AND run_id = ?`, runID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return Domainf("stale run: %q does not hold the live lease (§10 fencing)", runID)
		}
		return q.QueryRowContext(ctx,
			`SELECT last_heartbeat_at FROM lock WHERE id = 1`).Scan(&stamped)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "last_heartbeat_at": stamped}, nil
}

// RegisterSession records the harness's native session locators on the runs
// row (ADR-001 D5; §15.4): the native session handle and the trace filename
// — a locator, never assumed, because real harnesses differ (contract §4).
func RegisterSession(db *sql.DB, runID, nativeRef, file string) (any, error) {
	if nativeRef == "" || file == "" {
		return nil, Usagef("mc run register-session requires --native-ref and --file")
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if _, err := fenceRun(ctx, q, runID); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `
			UPDATE runs SET native_session_ref = ?, trace_filename = ?
			WHERE id = ?`, nativeRef, file, runID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "native_session_ref": nativeRef, "trace_filename": file}, nil
}
