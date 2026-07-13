package domain

import "context"

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
