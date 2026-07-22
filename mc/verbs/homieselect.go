package verbs

import "time"

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
