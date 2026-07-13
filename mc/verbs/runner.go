package verbs

import (
	"context"
	"database/sql"
	"fmt"

	"mc/domain"
)

// Heartbeat is the pipeline runner's private lifecycle verb (§10, §11.5,
// contract §2), behind the lease aggregate: last_heartbeat_at stamps iff
// run_id matches the live lease (fenced — a zombie run can neither renew its
// old lease nor touch the new holder's). It can never extend
// hard_deadline_at (Inv. 1).
func Heartbeat(db *sql.DB, id *RunIdentity, runID string) (any, error) {
	if err := requireOwnRun(id, runID); err != nil {
		return nil, err
	}
	var stamped string
	err := inTx(db, func(ctx context.Context, q Q) error {
		var err error
		stamped, err = domain.Heartbeat(ctx, q, runID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "last_heartbeat_at": stamped}, nil
}

// RegisterSession records the harness's native session locators on the runs
// row (ADR-001 D5; §15.4): the native session handle and the trace filename
// — a locator, never assumed, because real harnesses differ (contract §4).
//
// Deliberately NOT lease-fenced (ADR-001 D6 scopes it "(own run)", not
// "fenced"): a run writing locators to its own permanent runs row needs
// identity, not fencing — the runner fires register-session at session-start,
// which can race the behavior's terminal verb releasing the lease, and a
// lease fence here would silently lose the locators forever (Inv. 26).
// run.json supplies own-run identity; Heartbeat additionally stays
// live-lease fenced. Registration is idempotent for the same locators and
// immutable for conflicting ones.
func RegisterSession(db *sql.DB, id *RunIdentity, runID, nativeRef, file string) (any, error) {
	if id != nil && id.Tier == "homie" {
		return registerHomieSessionLocators(db, id, runID, nativeRef, file)
	}
	if err := requireOwnRun(id, runID); err != nil {
		return nil, err
	}
	if nativeRef == "" || file == "" {
		return nil, Usagef("mc run register-session requires --native-ref and --file")
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		var existingRef, existingFile sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT native_session_ref, trace_filename FROM runs WHERE id = ?`, runID,
		).Scan(&existingRef, &existingFile)
		if err == sql.ErrNoRows {
			return Domainf("unknown run %q: register-session writes only its own runs row (ADR-001 D6)", runID)
		}
		if err != nil {
			return err
		}
		if existingRef.Valid || existingFile.Valid {
			if existingRef.Valid && existingFile.Valid &&
				existingRef.String == nativeRef && existingFile.String == file {
				return nil // same-value lifecycle retry is idempotent
			}
			return Domainf("run %q session locators are immutable once registered (Inv. 26)", runID)
		}
		_, err = q.ExecContext(ctx, `
			UPDATE runs SET native_session_ref = ?, trace_filename = ? WHERE id = ?`,
			nativeRef, file, runID)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "native_session_ref": nativeRef, "trace_filename": file}, nil
}

// registerHomieSessionLocators is the ADR-012 Homie arm: the Homie runner
// records the native locator pair on its own canonical homie_sessions row.
// Same grammar as the pipeline arm — set-once, same-value retries
// idempotent — and no liveness requirement: a session that ended before its
// runner registered must still record its locators or resumability is lost
// forever (Inv. 26). The frozen verb_allowlist governs only the model's
// operator verbs, not this runner-private lifecycle write.
func registerHomieSessionLocators(db *sql.DB, id *RunIdentity, sessionID, nativeRef, file string) (any, error) {
	if id.RunID == "" || id.RunID != sessionID {
		return nil, &DomainError{Code: domain.CodeStaleRun,
			Msg: fmt.Sprintf("a Homie runner registers only its own session; run.json identifies %q, target is %q (ADR-012)", id.RunID, sessionID)}
	}
	if nativeRef == "" || file == "" {
		return nil, Usagef("mc run register-session requires --native-ref and --file")
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		var existingRef, existingFile sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT native_session_ref, trace_filename FROM homie_sessions WHERE id = ?`, sessionID,
		).Scan(&existingRef, &existingFile)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q (ADR-012)", sessionID)
		}
		if err != nil {
			return err
		}
		if existingRef.Valid || existingFile.Valid {
			if existingRef.Valid && existingFile.Valid &&
				existingRef.String == nativeRef && existingFile.String == file {
				return nil // same-value lifecycle retry is idempotent
			}
			return Domainf("Homie session %q locators are immutable once registered (Inv. 26)", sessionID)
		}
		_, err = q.ExecContext(ctx, `
			UPDATE homie_sessions SET native_session_ref = ?, trace_filename = ? WHERE id = ?`,
			nativeRef, file, sessionID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"session_id": sessionID, "native_session_ref": nativeRef, "trace_filename": file}, nil
}
