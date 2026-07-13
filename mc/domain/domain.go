// Package domain is the real enforcement layer between the CLI plane
// (mc/verbs) and the storage backstop (mc/substrate) — phase2-contract §1.
//
// Every state-law precondition is checked in Go and rejected with a *named*
// DomainError before SQL runs; budget and streak arithmetic live here; every
// function is transaction-scoped: it operates inside the caller's
// BEGIN IMMEDIATE via the Q interface and never opens connections, never
// BEGINs/COMMITs, and never reads the wall clock — a timestamp needed for
// arithmetic arrives as an explicit `now time.Time` parameter whose only
// production source is SELECT datetime('now') inside the same transaction
// (spec §10 clock discipline). Stamps (not arithmetic) keep using
// datetime('now') directly in SQL.
//
// The substrate's trigger lattice stays the redundant backstop (spec §4): a
// bug here cannot store an illegal state. Cascades (archive→packet,
// child-cancel, block propagation/auto-clear, saturation computation) stay
// substrate-implemented — they are writes, not validations, and a Go
// re-implementation would be the drifting duplicate (contract §1.1).
package domain

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Q is the query surface domain functions run against inside the caller's
// transaction (moved here from mc/verbs; re-exported there so existing
// signatures survive — contract §1.1).
type Q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Stable error-code slugs (contract §1.1). Wave 1 sets codes at every domain
// rejection; rendering them in stdout JSON is wave 2's §18 error-surface
// decision — the CLI keeps its Phase 1b contract (exit 1, stderr message).
const (
	CodeStaleRun           = "stale-run"
	CodeRoleMismatch       = "role-mismatch"
	CodeIllegalTransition  = "illegal-transition"
	CodeWIPCap             = "wip-cap"
	CodeBudgetExhausted    = "budget-exhausted"
	CodeBudgetRemaining    = "budget-remaining"
	CodeCorrectionRequired = "correction-required"
	CodeDeepeningRequired  = "deepening-required"
	CodeDeepeningForbidden = "deepening-forbidden"
	CodeNotPackaged        = "not-packaged"
	CodeLandingFence       = "landing-fence"
	CodeAlreadyDecided     = "already-decided"
	CodeStrictDrain        = "strict-drain"
	CodeZeroPromotion      = "zero-promotion"
	CodePoolMismatch       = "pool-mismatch"
	CodeLeaseHeld          = "lease-held"
	CodeNotFound           = "not-found"
	CodeNotBlocked         = "not-blocked"
	CodeBlockedChild       = "blocked-child"
	CodeReasonRequired     = "reason-required"
	CodeSaturated          = "saturated"
	CodeEmptyWave          = "empty-wave"
	CodeNotInitiative      = "not-initiative"
	CodeArchived           = "archived"
)

// DomainError is a domain rejection: exit 1 at the CLI (phase1b-contract §2),
// carrying a stable Code slug for the wave-2 §18 error surface.
type DomainError struct {
	Code string
	Msg  string
}

func (e *DomainError) Error() string { return e.Msg }

// Errf builds a coded DomainError.
func Errf(code, format string, a ...any) error {
	return &DomainError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// SpineTime is the datetime('now') storage format.
const SpineTime = "2006-01-02 15:04:05"

// Now reads the lock domain's own clock inside the caller's transaction —
// the one production source of the `now` parameters (spec §10).
func Now(ctx context.Context, q Q) (time.Time, error) {
	var s string
	if err := q.QueryRowContext(ctx, `SELECT datetime('now')`).Scan(&s); err != nil {
		return time.Time{}, err
	}
	return ParseSpineTime(s)
}

// ParseSpineTime parses a stored spine timestamp.
func ParseSpineTime(s string) (time.Time, error) {
	t, err := time.Parse(SpineTime, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse spine timestamp %q: %w", s, err)
	}
	return t, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// taskRow is the domain's read of one tasks row.
type taskRow struct {
	ID              int64
	Scope           string
	Status          string
	InitiativeID    sql.NullInt64
	CorrectionCount int
	Blocked         bool
	Decision        sql.NullString
	Archived        bool
	Branch          sql.NullString
	VerifiedSHA     sql.NullString
	TargetRef       sql.NullString
	Worksource      string
}

func getTask(ctx context.Context, q Q, id int64) (taskRow, error) {
	var r taskRow
	var blocked, archived int
	err := q.QueryRowContext(ctx, `
		SELECT id, scope, status, initiative_id, correction_count, blocked,
		       decision, archived, branch, verified_sha, target_ref, worksource
		FROM tasks WHERE id = ?`, id).Scan(
		&r.ID, &r.Scope, &r.Status, &r.InitiativeID, &r.CorrectionCount,
		&blocked, &r.Decision, &archived, &r.Branch, &r.VerifiedSHA,
		&r.TargetRef, &r.Worksource)
	if err == sql.ErrNoRows {
		return r, Errf(CodeNotFound, "no task %d", id)
	}
	if err != nil {
		return r, err
	}
	r.Blocked = blocked == 1
	r.Archived = archived == 1
	return r, nil
}

func requireLivePacket(ctx context.Context, q Q, taskID int64) error {
	var archived int
	err := q.QueryRowContext(ctx,
		`SELECT archived FROM review_packets WHERE task_id = ?`, taskID).Scan(&archived)
	if err == sql.ErrNoRows {
		return Errf(CodeNotFound, "task %d holds no Review Packet (Inv. 11, Inv. 17)", taskID)
	}
	if err != nil {
		return err
	}
	if archived != 0 {
		return Errf(CodeArchived, "task %d's Review Packet is archived (Inv. 11)", taskID)
	}
	return nil
}

// requireLive rejects archived and decided rows: archived rows are terminal
// and decided rows never transition (§6, NOTE(P1.4)).
func requireLive(r taskRow) error {
	if r.Archived {
		return Errf(CodeArchived, "task %d is archived: terminal, no transitions (§6)", r.ID)
	}
	if r.Decision.Valid {
		return Errf(CodeAlreadyDecided,
			"task %d is already decided (%s); decisions never rewrite (§5)", r.ID, r.Decision.String)
	}
	return nil
}
