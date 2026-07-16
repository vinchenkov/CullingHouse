package domain

import (
	"context"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Initiative aggregate (spec §6.1) — wave birth. The done-declaration rides
// task.AdvanceStage (strict-drain named there); block propagation and
// auto-clear are substrate cascades, asserted (not re-implemented) by this
// aggregate's suite (contract §1.1/§1.2).
// ---------------------------------------------------------------------------

// WaveChild is one child in a wave batch (ADR-001 D4).
type WaveChild struct {
	Title       string
	Description string
	Priority    *int // nil defaults to the schema's P2
}

// BirthWave inserts a whole wave into a live, still-seeded initiative —
// whole wave or nothing (ADR-001 constraint a; the §10 crash table relies on
// it): the caller's transaction is the atomicity boundary, and every
// precondition is named here before any insert. Children are born seeded,
// scope task, inheriting the initiative's worksource; the initiative's own
// status does not move (it stays seeded, now parked behind open children).
func BirthWave(ctx context.Context, q Q, initiativeID int64, children []WaveChild) ([]int64, error) {
	if len(children) == 0 {
		// An empty wave would leave the initiative seeded-and-drained: the
		// next tick re-selects Strategist(initiative) forever. Declaring done
		// is the terminal for a drained initiative, not an empty wave.
		return nil, Errf(CodeEmptyWave, "a wave carries at least one child (§6.1); declare done instead")
	}
	for i, c := range children {
		if c.Title == "" {
			return nil, Errf(CodeReasonRequired, "wave child %d: title is required (ADR-001 D4)", i)
		}
		if !validDispatchScalarAdmission(c.Title) {
			return nil, Errf(CodeCarrierForbidden,
				"wave child %d: title must be valid UTF-8 without controls and at most 4096 bytes (ADR-016 D2)", i)
		}
	}
	r, err := getTask(ctx, q, initiativeID)
	if err != nil {
		return nil, err
	}
	if r.Scope != "initiative" {
		return nil, Errf(CodeNotInitiative, "task %d is scope %q, not an initiative (§6.1)", initiativeID, r.Scope)
	}
	if err := requireLive(r); err != nil {
		return nil, err
	}
	if r.Status != "seeded" {
		return nil, Errf(CodeIllegalTransition,
			"wave children are born only into a live, still-seeded initiative (§6.1); initiative %d is %q",
			initiativeID, r.Status)
	}
	var open int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE initiative_id = ? AND archived = 0`,
		initiativeID).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, Errf(CodeStrictDrain,
			"initiative %d already has %d open children; the current wave must drain before the next wave is born (§6.1)",
			initiativeID, open)
	}

	ids := make([]int64, 0, len(children))
	for _, c := range children {
		pri := 2
		if c.Priority != nil {
			pri = *c.Priority
		}
		res, err := q.ExecContext(ctx, `
			INSERT INTO tasks (title, description, scope, status, initiative_id,
			                   priority, origin, worksource, target_ref)
			VALUES (?, ?, 'task', 'seeded', ?, ?, 'autonomous', ?, 'main')`,
			c.Title, nullIfEmpty(c.Description), initiativeID, pri, r.Worksource)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// PassWaveReview applies the Editor's holistic pass (ADR-020 D5): every child
// in the run's snapshot becomes plan_reviewed, and nothing else moves.
//
// The snapshot must equal the live open-child set exactly. A holistic verdict
// asserts a property OF A SET, so a verdict rendered over a set that no longer
// exists (an operator cancelled a child mid-review) is stale: it is refused
// whole, never partially applied.
//
// The initiative's own status and decision deliberately do not move — Inv. 10:
// the plan review completes no stage. Afterwards planReviewPending is false and
// hasOpenChildren is true, so the initiative returns to parked and its children
// dispatch to Workers through the unchanged (status, scope) table.
func PassWaveReview(ctx context.Context, q Q, initiativeID int64, snapshot []int64) error {
	if err := requireReviewableWave(ctx, q, initiativeID); err != nil {
		return err
	}
	open, err := openChildIDs(ctx, q, initiativeID)
	if err != nil {
		return err
	}
	if !sameInt64Set(open, snapshot) {
		return Errf(CodePoolMismatch,
			"the wave changed under review: snapshot %v, live open children %v (ADR-020 D5)",
			snapshot, open)
	}
	for _, id := range snapshot {
		if _, err := q.ExecContext(ctx,
			`UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return recordWaveEvent(ctx, q, "wave.passed", initiativeID, formatIDs(snapshot))
}

// SendBackWave applies the Editor's holistic send-back (ADR-020 D5): the wave
// is destroyed, not un-reviewed, and Strategist(initiative) replans against
// the objection.
//
// Asymmetric with the pass by design: the pass asserts a property over a set
// and so demands the exact set; this arm asserts nothing about the set, it
// destroys it, so a snapshot member already archived is skipped rather than an
// error — it is already in the target state.
func SendBackWave(ctx context.Context, q Q, initiativeID int64, snapshot []int64, reason string) error {
	if reason == "" {
		return Errf(CodeReasonRequired,
			"--reason is required for send-back (ADR-020 D5: asymmetric by design)")
	}
	if err := requireReviewableWave(ctx, q, initiativeID); err != nil {
		return err
	}
	open, err := openChildIDs(ctx, q, initiativeID)
	if err != nil {
		return err
	}
	live := make(map[int64]bool, len(open))
	for _, id := range open {
		live[id] = true
	}
	for _, id := range snapshot {
		if !live[id] {
			continue // already archived: nothing to destroy
		}
		// The ordinary cancellation write, so the substrate's existing
		// cascades fire; actor is the Editor (Inv. 7).
		if err := Cancel(ctx, q, id, reason, "editor"); err != nil {
			return err
		}
	}
	// Assert D5's own postcondition rather than trust the snapshot. The
	// license above — this arm asserts nothing about the set, it destroys it —
	// covers an individual member already in the target state; it is NOT a
	// license to accept a degenerate snapshot, destroy nothing, and report
	// success. That failure is silent and worse than the pass arm's: the
	// terminal is ACCEPTED, so no dispatch_retries are charged and it never
	// self-blocks, while planReviewPending stays true and the next tick
	// re-dispatches the same plan review forever. The transaction is the
	// atomicity boundary, so refusing here rolls the whole arm back.
	remaining, err := openChildIDs(ctx, q, initiativeID)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return Errf(CodePoolMismatch,
			"send-back must drain the wave: snapshot %v left open children %v (ADR-020 D5)",
			snapshot, remaining)
	}
	return recordWaveEvent(ctx, q, "wave.sent_back", initiativeID, reason)
}

// requireReviewableWave: the subject must be a live initiative. An initiative
// decided or archived mid-run (an operator cancel) rejects the terminal.
func requireReviewableWave(ctx context.Context, q Q, initiativeID int64) error {
	r, err := getTask(ctx, q, initiativeID)
	if err != nil {
		return err
	}
	if r.Scope != "initiative" {
		return Errf(CodeNotInitiative, "task %d is scope %q, not an initiative (§6.1)", initiativeID, r.Scope)
	}
	return requireLive(r)
}

func openChildIDs(ctx context.Context, q Q, initiativeID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id FROM tasks WHERE initiative_id = ? AND archived = 0 ORDER BY id`, initiativeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func recordWaveEvent(ctx context.Context, q Q, kind string, initiativeID int64, detail string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO activity (actor, kind, subject, detail)
		VALUES ('editor', ?, ?, ?)`, kind, initiativeID, detail)
	return err
}

func sameInt64Set(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[int64]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}
	return true
}

func formatIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}
