package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Lease aggregate (spec §10 lock/lease; Inv. 1/4). The three reap
// *conditions* are not re-implemented here: they live in mc/dispatch.reapable
// (frozen, S6-proven); this file owns the write side. All lease arithmetic
// takes `now` by parameter — the lock domain's own clock, injectable for
// tests exactly like dispatch.Clock (contract §1.1).
// ---------------------------------------------------------------------------

// ClaimArgs is one claim-and-spawn write (§10; Inv. 4: the runs row is
// committed before the process starts, in the same transaction as the CAS).
type ClaimArgs struct {
	RunID       string
	Owner       string // lock.owner vocabulary (base role)
	SubjectID   *int64 // nil for subjectless runs (propose/console)
	SessionPath string
	Binding     string
	// PoolSnapshot: Editor runs snapshot the entire proposed pool at claim
	// (§10 step 3; ADR-001 D4 coverage reads it back). nil = not an Editor.
	PoolSnapshot []int64
	// HardDeadlineMinutes: hard_deadline_at = now + this (Inv. 1; the lock
	// row's tunable).
	HardDeadlineMinutes int
}

// ClaimResult reports the claim's derived fields.
type ClaimResult struct {
	Worksource *string // the subject's worksource (nil for subjectless runs)
}

// Claim is the CAS claim: UPDATE … WHERE run_id IS NULL first — against a
// held lock it aborts before any write lands (§10 fencing; the in-transaction
// backstop branch) — then the runs-row insert. hard_deadline_at is stamped
// from now (arithmetic on the injected clock); last_heartbeat_at starts NULL
// (the first-heartbeat watchdog's case).
func Claim(ctx context.Context, q Q, now time.Time, a ClaimArgs) (ClaimResult, error) {
	var res ClaimResult

	var worksource any
	var subject any
	if a.SubjectID != nil {
		subject = *a.SubjectID
		var ws string
		if err := q.QueryRowContext(ctx,
			`SELECT worksource FROM tasks WHERE id = ?`, *a.SubjectID).Scan(&ws); err != nil {
			if err == sql.ErrNoRows {
				return res, Errf(CodeNotFound, "no task %d", *a.SubjectID)
			}
			return res, err
		}
		worksource = ws
		res.Worksource = &ws
	}

	acquired := now.UTC().Format(SpineTime)
	deadline := now.UTC().Add(time.Duration(a.HardDeadlineMinutes) * time.Minute).Format(SpineTime)
	cas, err := q.ExecContext(ctx, `
		UPDATE lock SET run_id = ?, worksource = ?, subject = ?, owner = ?,
			acquired_at = ?, last_heartbeat_at = NULL, hard_deadline_at = ?
		WHERE id = 1 AND run_id IS NULL`,
		a.RunID, worksource, subject, a.Owner, acquired, deadline)
	if err != nil {
		return res, err
	}
	n, err := cas.RowsAffected()
	if err != nil {
		return res, err
	}
	if n != 1 {
		return res, Errf(CodeLeaseHeld, "lost the claim race: lock is held (§10 fencing)")
	}

	var pool any
	if a.PoolSnapshot != nil {
		b, err := json.Marshal(a.PoolSnapshot)
		if err != nil {
			return res, err
		}
		pool = string(b)
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO runs (id, tier, role, worksource, subject, session_path,
		                  binding, pool_snapshot)
		VALUES (?, 'pipeline', ?, ?, ?, ?, ?, ?)`,
		a.RunID, a.Owner, worksource, subject, a.SessionPath, a.Binding, pool); err != nil {
		return res, err
	}
	return res, nil
}

// Fence verifies the run's fencing token against the live lease (§10, §18
// deny rule 2): a call whose run_id no longer matches is rejected, never
// double-applied. Returns the lease's subject (nil for subjectless runs).
func Fence(ctx context.Context, q Q, runID string) (*int64, error) {
	var lockRun sql.NullString
	var subject sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id = 1`).
		Scan(&lockRun, &subject)
	if err != nil {
		return nil, err
	}
	if !lockRun.Valid || lockRun.String != runID {
		return nil, Errf(CodeStaleRun,
			"stale run: %q does not hold the live lease (§10 fencing)", runID)
	}
	if subject.Valid {
		s := subject.Int64
		return &s, nil
	}
	return nil, nil
}

// Heartbeat stamps lock.last_heartbeat_at iff runID holds the live lease —
// fenced: a zombie run can neither renew its old lease nor touch the new
// holder's. It may never touch hard_deadline_at (Inv. 1). Returns the stamp.
func Heartbeat(ctx context.Context, q Q, runID string) (string, error) {
	res, err := q.ExecContext(ctx, `
		UPDATE lock SET last_heartbeat_at = datetime('now')
		WHERE id = 1 AND run_id = ?`, runID)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", Errf(CodeStaleRun,
			"stale run: %q does not hold the live lease (§10 fencing)", runID)
	}
	var stamped string
	err = q.QueryRowContext(ctx,
		`SELECT last_heartbeat_at FROM lock WHERE id = 1`).Scan(&stamped)
	return stamped, err
}

// Release frees the lease iff runID holds it (fenced): NULL every claim
// column — a free lock carries no run residue (substrate CHECKs).
func Release(ctx context.Context, q Q, runID string) error {
	res, err := q.ExecContext(ctx, `
		UPDATE lock SET run_id = NULL, worksource = NULL, subject = NULL,
			owner = NULL, acquired_at = NULL, last_heartbeat_at = NULL,
			hard_deadline_at = NULL
		WHERE id = 1 AND run_id = ?`, runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return Errf(CodeStaleRun,
			"stale run: %q does not hold the live lease (§10 fencing)", runID)
	}
	return nil
}

// ReapArgs is the write side of one step-(0) reap decision (the conditions
// were evaluated by the frozen mc/dispatch.reapable).
type ReapArgs struct {
	RunID  string
	Reason string
}

// ReapResult reports the applied charge.
type ReapResult struct {
	Charged bool
	Blocked bool // the charge exhausted the budget → blocked, same transaction
}

// ApplyReap is the one reap mutation (§10 step 0): mark the run reaped,
// charge the subject's dispatch budget when subject-carrying (blocking on
// exhaustion rides ChargeInfra — never a silent loop), free the lease. One
// transaction; the container stop returns to the resident as effect data.
func ApplyReap(ctx context.Context, q Q, a ReapArgs) (ReapResult, error) {
	var res ReapResult
	var subject sql.NullInt64
	if err := q.QueryRowContext(ctx,
		`SELECT subject FROM lock WHERE id = 1 AND run_id = ?`, a.RunID,
	).Scan(&subject); err != nil {
		if err == sql.ErrNoRows {
			return res, Errf(CodeStaleRun,
				"stale run: %q does not hold the live lease (§10 fencing)", a.RunID)
		}
		return res, err
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE runs SET ended_at = datetime('now'), outcome = 'reaped' WHERE id = ?`,
		a.RunID); err != nil {
		return res, err
	}
	if subject.Valid {
		charge, err := ChargeInfra(ctx, q, subject.Int64, a.Reason)
		if err != nil {
			return res, err
		}
		res.Charged = true
		res.Blocked = charge.Blocked
	}
	return res, Release(ctx, q, a.RunID)
}
