package verbs

import (
	"context"
	"database/sql"
	"testing"
)

// hwGet runs a single-row query and scans it, failing the test on error.
func hwGet(t *testing.T, db *sql.DB, query string, dest ...any) {
	t.Helper()
	if err := db.QueryRow(query).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}

// hwSession seeds one active Homie session with a distinct binding/container.
func hwSession(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	dvExec(t, db, `
		INSERT INTO homie_sessions (id, container_name, verb_allowlist, session_path, binding)
		VALUES (?, ?, '[]', ?, 'claude')`, id, "mc-homie-"+id, "sessions/"+id)
}

// hwPendingInbound appends an unclaimed inbound turn (conversation_pending).
func hwPendingInbound(t *testing.T, db *sql.DB, id string, seq int) {
	t.Helper()
	dvExec(t, db, `
		INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		VALUES (?, ?, 'inbound', 'dashboard', 'hi')`, id, seq)
}

func hwWakeRound(t *testing.T, db *sql.DB, idleTimeoutS int) (map[string]any, bool) {
	t.Helper()
	var (
		effect  map[string]any
		handled bool
	)
	err := inTx(db, func(ctx context.Context, q Q) error {
		now, err := spineNow(ctx, q)
		if err != nil {
			return err
		}
		effect, handled, err = homieWakeRound(ctx, q, now, idleTimeoutS)
		return err
	})
	if err != nil {
		t.Fatalf("homieWakeRound: %v", err)
	}
	return effect, handled
}

func TestLoadHomieSchedRows(t *testing.T) {
	db := dvSpine(t)
	// Insert out of activity order to prove the loader sorts.
	hwSession(t, db, "h-newer")
	hwSession(t, db, "h-older")
	dvExec(t, db, `UPDATE homie_sessions SET last_activity_at = '2000-01-01 00:00:00' WHERE id = 'h-older'`)
	hwPendingInbound(t, db, "h-newer", 1)
	// A launched, resume-clean session — HasLaunch must be observed.
	hwSession(t, db, "h-launched")
	dvExec(t, db, `UPDATE homie_sessions
		SET current_launch_id = 'ffffffffffffffff', current_launch_mode = 'fresh'
		WHERE id = 'h-launched'`)
	// An ended session must not appear.
	hwSession(t, db, "h-gone")
	dvExec(t, db, `UPDATE homie_sessions SET status = 'ended' WHERE id = 'h-gone'`)

	var rows []homieSchedRow
	if err := inTx(db, func(ctx context.Context, q Q) error {
		var e error
		rows, e = loadHomieSchedRows(ctx, q)
		return e
	}); err != nil {
		t.Fatalf("loadHomieSchedRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 active rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].SessionID != "h-older" {
		t.Fatalf("rows not ordered by (last_activity_at, id): %+v", rows)
	}
	byID := map[string]homieSchedRow{}
	for _, r := range rows {
		byID[r.SessionID] = r
	}
	if !byID["h-newer"].PendingInput {
		t.Fatalf("h-newer should carry pending inbound: %+v", byID["h-newer"])
	}
	if byID["h-older"].PendingInput {
		t.Fatalf("h-older has no inbound turn: %+v", byID["h-older"])
	}
	if !byID["h-launched"].HasLaunch || byID["h-launched"].LaunchMode != "fresh" {
		t.Fatalf("h-launched should observe its launch: %+v", byID["h-launched"])
	}
}

func TestLoadHomieSchedRowsCompletedInboundIsNotPending(t *testing.T) {
	db := dvSpine(t)
	hwSession(t, db, "h-done")
	// An inbound turn already claimed and completed is not pending.
	dvExec(t, db, `
		INSERT INTO conversation_messages (session_id, seq, direction, surface, body, claimed_by, claimed_at, completed_at)
		VALUES ('h-done', 1, 'inbound', 'dashboard', 'hi', 'runner', datetime('now'), datetime('now'))`)
	var rows []homieSchedRow
	if err := inTx(db, func(ctx context.Context, q Q) error {
		var e error
		rows, e = loadHomieSchedRows(ctx, q)
		return e
	}); err != nil {
		t.Fatalf("loadHomieSchedRows: %v", err)
	}
	if len(rows) != 1 || rows[0].PendingInput {
		t.Fatalf("a completed inbound turn is not pending: %+v", rows)
	}
}

func TestHomieWakeRoundFreshSpawn(t *testing.T) {
	db := dvSpine(t)
	hwSession(t, db, "h-1")
	hwPendingInbound(t, db, "h-1", 1)

	effect, handled := hwWakeRound(t, db, 1800)
	if !handled {
		t.Fatalf("a pending inbound turn should produce a wake")
	}
	if effect["action"] != "homie-wake" || effect["session"] != "h-1" || effect["mode"] != "fresh" {
		t.Fatalf("unexpected wake effect: %+v", effect)
	}
	if effect["binding"] != "claude" || effect["container_name"] != "mc-homie-h-1" {
		t.Fatalf("wake effect missing binding/container: %+v", effect)
	}
	launch, _ := effect["launch"].(string)
	if !validLowercaseHex(launch, 16) {
		t.Fatalf("wake effect launch is not 16-hex: %q", launch)
	}

	// The launch generation is persisted onto the session and resume debt clear.
	var gotLaunch, gotMode sql.NullString
	var resumeOwed int
	hwGet(t, db, `SELECT current_launch_id, current_launch_mode, resume_owed
		FROM homie_sessions WHERE id = 'h-1'`, &gotLaunch, &gotMode, &resumeOwed)
	if gotLaunch.String != launch || gotMode.String != "fresh" || resumeOwed != 0 {
		t.Fatalf("launch not persisted / resume not clear: %q %q owed=%d", gotLaunch.String, gotMode.String, resumeOwed)
	}
	var n int
	hwGet(t, db, `SELECT count(*) FROM activity WHERE kind = 'homie.launched' AND subject = 'h-1'`, &n)
	if n != 1 {
		t.Fatalf("want one homie.launched activity, got %d", n)
	}
}

func TestHomieWakeRoundRowsResume(t *testing.T) {
	db := dvSpine(t)
	hwSession(t, db, "h-1")
	// Resume debt in rows mode with a prime cutoff — no pending inbound.
	dvExec(t, db, `UPDATE homie_sessions
		SET resume_owed = 1, resume_mode = 'rows',
		    resume_prime_through_seq = 9, resume_prime_row_count = 4
		WHERE id = 'h-1'`)

	effect, handled := hwWakeRound(t, db, 1800)
	if !handled || effect["mode"] != "rows" {
		t.Fatalf("resume debt should wake in rows mode: handled=%v %+v", handled, effect)
	}
	// The resume prime cutoff is carried into the current prime pair and the
	// resume debt is cleared.
	var curSeq, curCount, resSeq sql.NullInt64
	var resumeOwed int
	hwGet(t, db, `SELECT current_prime_through_seq, current_prime_row_count,
		resume_prime_through_seq, resume_owed FROM homie_sessions WHERE id = 'h-1'`, &curSeq, &curCount, &resSeq, &resumeOwed)
	if curSeq.Int64 != 9 || curCount.Int64 != 4 {
		t.Fatalf("prime cutoff not carried forward: seq=%v count=%v", curSeq, curCount)
	}
	if resSeq.Valid || resumeOwed != 0 {
		t.Fatalf("resume debt not cleared: resSeq=%v owed=%d", resSeq, resumeOwed)
	}
}

func TestHomieWakeRoundIdleEnd(t *testing.T) {
	db := dvSpine(t)
	hwSession(t, db, "h-1")
	dvExec(t, db, `UPDATE homie_sessions SET last_activity_at = '2000-01-01 00:00:00' WHERE id = 'h-1'`)
	dvExec(t, db, `INSERT INTO homie_bindings (session_id, surface, channel_ref)
		VALUES ('h-1', 'dashboard', 'chan-1')`)

	effect, handled := hwWakeRound(t, db, 1800)
	if !handled {
		t.Fatalf("an idled session should produce an end")
	}
	if effect["action"] != "homie-stop" || effect["session"] != "h-1" || effect["reason"] != "idle" {
		t.Fatalf("unexpected stop effect: %+v", effect)
	}
	var status string
	hwGet(t, db, `SELECT status FROM homie_sessions WHERE id = 'h-1'`, &status)
	if status != "ended" {
		t.Fatalf("idled session not ended: %q", status)
	}
	var active int
	hwGet(t, db, `SELECT count(*) FROM homie_bindings WHERE session_id = 'h-1' AND active = 1`, &active)
	if active != 0 {
		t.Fatalf("bindings not deactivated on end: %d still active", active)
	}
	var n int
	hwGet(t, db, `SELECT count(*) FROM activity WHERE kind = 'homie.ended' AND subject = 'h-1'`, &n)
	if n != 1 {
		t.Fatalf("want one homie.ended activity, got %d", n)
	}
}

func TestHomieWakeRoundNothingToDo(t *testing.T) {
	db := dvSpine(t)
	// An active session with no traffic and no idle is not eligible.
	hwSession(t, db, "h-1")
	if _, handled := hwWakeRound(t, db, 1800); handled {
		t.Fatalf("a quiet non-idle session must not be woken")
	}
}

// TestDispatchWakesHomieOverPipelineCandidate proves the full prepare path and
// the branch-7 preemption: a fresh spine liveness-retains a strategist propose
// (KindSpawn), yet an eligible Homie wake wins, returning a homie-wake final
// effect instead of the pipeline candidate (the pipeline turn is re-decided
// next tick). The resident e2e depends on this preemption.
func TestDispatchWakesHomieOverPipelineCandidate(t *testing.T) {
	db := dvSpine(t)
	hwSession(t, db, "h-1")
	hwPendingInbound(t, db, "h-1", 1)

	var uuid string
	if err := db.QueryRow(`SELECT deployment_uuid FROM meta WHERE id = 1`).Scan(&uuid); err != nil {
		t.Fatalf("read deployment uuid: %v", err)
	}
	var prepared preparedDispatch
	if err := inTx(db, func(ctx context.Context, q Q) error {
		var e error
		prepared, e = dispatchPrepareWithIdentity(ctx, q, defaultDispatchProtocolIdentity, uuid, "00112233445566ff")
		return e
	}); err != nil {
		t.Fatalf("dispatchPrepareWithIdentity: %v", err)
	}
	if prepared.final == nil {
		t.Fatalf("no final effect; prepared = %+v (expected a homie-wake)", prepared)
	}
	if prepared.final["action"] != "homie-wake" || prepared.final["session"] != "h-1" {
		t.Fatalf("final effect = %+v, want a homie-wake of h-1", prepared.final)
	}
}

func TestHomieWakeRoundAlreadyLaunchedIsSkipped(t *testing.T) {
	db := dvSpine(t)
	hwSession(t, db, "h-1")
	hwPendingInbound(t, db, "h-1", 1)
	dvExec(t, db, `UPDATE homie_sessions
		SET current_launch_id = 'ffffffffffffffff', current_launch_mode = 'fresh'
		WHERE id = 'h-1'`)
	if _, handled := hwWakeRound(t, db, 1800); handled {
		t.Fatalf("an already-launched session is deferred recovery, not a happy-path wake")
	}
}
