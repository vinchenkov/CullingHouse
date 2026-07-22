package verbs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

// ADR-016 D3's lease-free Homie wake selector — the spine-only core.
//
// The selector runs after the pipeline Decide() when no pipeline candidate is
// committed (Homie is the explicit lease-free tier, Inv. 1/22). This file is
// branch 6 (idle end) and branch 7's happy case (a non-idle session with no
// launch and pending inbound or resume debt gets a fresh/resume wake) — the
// decisions that need only spine state.
//
// DELIBERATELY DEFERRED to a later hardening slice (they need the resident's
// container inventory, so they are recovery machinery for a resident outage,
// not the happy path):
//   - branch 5's transitional/terminal container reconciliation
//     (paused|restarting|removing|exited|dead|created|running → stop/remove/
//     confirmed-absent-end), and
//   - branch 7's unstarted-launch-debt / adopt-a-created-container recovery.
// The resident normally invokes the launch-bind/runner-started/exit receipts
// itself the moment it acts, so these branches are the tick's recovery path.

type homieWakeKind string

const (
	homieWakeNone  homieWakeKind = ""
	homieWakeIdle  homieWakeKind = "idle-end"
	homieWakeSpawn homieWakeKind = "wake"
)

// homieSchedRow is one active session's spine-only scheduling state, loaded in
// (last_activity_at, id) order.
type homieSchedRow struct {
	SessionID      string
	LastActivityAt time.Time
	HasLaunch      bool   // current_launch_id IS NOT NULL
	LaunchMode     string // when HasLaunch
	PendingInput   bool   // an inbound turn awaits (conversation_pending)
	ResumeOwed     bool
	ResumeMode     string // native|rows, when ResumeOwed
}

// homieWakeDecision is the selector's verdict for this tick — at most one
// session, one action.
type homieWakeDecision struct {
	Kind       homieWakeKind
	SessionID  string
	LaunchMode string // for a spawn wake: fresh|native|rows
}

// selectHomieWake picks at most one action from active sessions ordered by
// (last_activity_at, id): branch 6 (idle end) wins over branch 7 (spawn), and
// each picks the oldest matching row. A session with no traffic since start
// (no pending input, no resume debt) is not eligible. See the file header for
// the deferred recovery branches.
func selectHomieWake(rows []homieSchedRow, now time.Time, idleTimeout time.Duration) homieWakeDecision {
	var idle, spawn *homieSchedRow
	for i := range rows {
		r := &rows[i]
		if !now.Before(r.LastActivityAt.Add(idleTimeout)) {
			// Branch 6 owns idle rows regardless of launch state.
			if idle == nil {
				idle = r
			}
			continue
		}
		// Branch 7 happy case: a non-idle session that has no launch yet and
		// something to do (a pending inbound turn or resume debt). An
		// already-launched session is skipped here (its unstarted-debt recovery
		// is the deferred branch).
		if spawn == nil && !r.HasLaunch && (r.PendingInput || r.ResumeOwed) {
			spawn = r
		}
	}
	if idle != nil {
		return homieWakeDecision{Kind: homieWakeIdle, SessionID: idle.SessionID}
	}
	if spawn != nil {
		// Fresh for a new inbound turn; otherwise the recorded resume mode.
		mode := "fresh"
		if !spawn.PendingInput && spawn.ResumeOwed {
			mode = spawn.ResumeMode
		}
		return homieWakeDecision{Kind: homieWakeSpawn, SessionID: spawn.SessionID, LaunchMode: mode}
	}
	return homieWakeDecision{Kind: homieWakeNone}
}

// loadHomieSchedRows loads every active Homie session's spine-only scheduling
// state in (last_activity_at, id) order — the projection selectHomieWake
// consumes. Pending input is an inbound conversation turn not yet completed
// (conversation_pending, §15.3). The deferred recovery branches (file header)
// would additionally read launch_started_at/current_container_id; the happy
// path does not, so they are not loaded here.
func loadHomieSchedRows(ctx context.Context, q Q) ([]homieSchedRow, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.id, s.last_activity_at, s.current_launch_id, s.current_launch_mode,
		       s.resume_owed, s.resume_mode,
		       EXISTS (SELECT 1 FROM conversation_messages m
		               WHERE m.session_id = s.id
		                 AND m.direction = 'inbound'
		                 AND m.completed_at IS NULL)
		FROM homie_sessions s
		WHERE s.status = 'active'
		ORDER BY s.last_activity_at, s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []homieSchedRow{}
	for rows.Next() {
		var (
			r                                homieSchedRow
			lastAct                          string
			launchID, launchMode, resumeMode sql.NullString
			resumeOwed, pending              int
		)
		if err := rows.Scan(&r.SessionID, &lastAct, &launchID, &launchMode,
			&resumeOwed, &resumeMode, &pending); err != nil {
			return nil, err
		}
		t, err := parseSpineTime(lastAct)
		if err != nil {
			return nil, err
		}
		r.LastActivityAt = t
		r.HasLaunch = launchID.Valid
		r.LaunchMode = launchMode.String
		r.ResumeOwed = resumeOwed == 1
		r.ResumeMode = resumeMode.String
		r.PendingInput = pending == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// newHomieLaunchID mints a fresh 16-hex launch generation (ADR-016 D3), the
// same dual-length shape the schema fences (schema.sql homie_sessions).
func newHomieLaunchID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", Usagef("allocate homie launch id: %v", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// homieWakeRound is the lease-free Homie tick, run in the dispatch prepare
// transaction only when the pipeline Decide() committed nothing (Inv. 1/22).
// It returns (effect, handled, err): handled=false means no Homie action this
// tick (fall through to the pipeline-idle final). A spawn wake mints and
// persists the launch generation (clearing resume debt, carrying a rows-mode
// prime cutoff forward) and emits `homie-wake`; an idle end flips the session
// to ended (the trigger deactivates its bindings) and emits `homie-stop`.
func homieWakeRound(ctx context.Context, q Q, now time.Time, idleTimeoutS int) (map[string]any, bool, error) {
	rows, err := loadHomieSchedRows(ctx, q)
	if err != nil {
		return nil, false, err
	}
	d := selectHomieWake(rows, now, time.Duration(idleTimeoutS)*time.Second)
	switch d.Kind {
	case homieWakeNone:
		return nil, false, nil

	case homieWakeIdle:
		var containerID sql.NullString
		if err := q.QueryRowContext(ctx,
			`SELECT current_container_id FROM homie_sessions WHERE id = ?`,
			d.SessionID).Scan(&containerID); err != nil {
			return nil, false, err
		}
		if _, _, err := homieEndTx(ctx, q, "dispatch", d.SessionID, "idle timeout"); err != nil {
			return nil, false, err
		}
		effect := map[string]any{
			"action":  "homie-stop",
			"session": d.SessionID,
			"reason":  "idle",
		}
		if containerID.Valid {
			effect["container_id"] = containerID.String
		}
		return effect, true, nil

	case homieWakeSpawn:
		launchID, err := newHomieLaunchID()
		if err != nil {
			return nil, false, err
		}
		var binding, containerName string
		if err := q.QueryRowContext(ctx,
			`SELECT binding, container_name FROM homie_sessions WHERE id = ?`,
			d.SessionID).Scan(&binding, &containerName); err != nil {
			return nil, false, err
		}
		// Persist the launch generation and clear resume debt in one statement.
		// A rows-mode wake carries the resume prime cutoff into the current
		// prime pair (SQLite evaluates every RHS against the pre-update row, so
		// reading resume_prime_* while nulling it is safe); fresh/native leave
		// the current prime pair NULL. The CAS on current_launch_id IS NULL
		// keeps a re-entrant tick from double-launching.
		var res sql.Result
		if d.LaunchMode == "rows" {
			res, err = q.ExecContext(ctx, `
				UPDATE homie_sessions
				SET current_launch_id = ?, current_launch_mode = 'rows',
				    current_prime_through_seq = resume_prime_through_seq,
				    current_prime_row_count = resume_prime_row_count,
				    resume_owed = 0, resume_mode = NULL,
				    resume_prime_through_seq = NULL, resume_prime_row_count = NULL
				WHERE id = ? AND status = 'active' AND current_launch_id IS NULL`,
				launchID, d.SessionID)
		} else {
			res, err = q.ExecContext(ctx, `
				UPDATE homie_sessions
				SET current_launch_id = ?, current_launch_mode = ?,
				    resume_owed = 0, resume_mode = NULL,
				    resume_prime_through_seq = NULL, resume_prime_row_count = NULL
				WHERE id = ? AND status = 'active' AND current_launch_id IS NULL`,
				launchID, d.LaunchMode, d.SessionID)
		}
		if err != nil {
			return nil, false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, false, err
		}
		if n == 0 {
			// Already launched under this tick's own CAS window — nothing to emit.
			return nil, false, nil
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO activity (actor, kind, subject, detail)
			VALUES ('dispatch', 'homie.launched', ?, ?)`,
			d.SessionID, launchID); err != nil {
			return nil, false, err
		}
		effect := map[string]any{
			"action":         "homie-wake",
			"session":        d.SessionID,
			"launch":         launchID,
			"mode":           d.LaunchMode,
			"binding":        binding,
			"container_name": containerName,
		}
		return effect, true, nil
	}
	return nil, false, nil
}
