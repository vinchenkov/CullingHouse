package domain

import (
	"context"
	"database/sql"
)

// ---------------------------------------------------------------------------
// Review-packet aggregate (spec §7, §8; Inv. 11/18). Saturation is a *read*
// here — the substrate trigger computes it at refine_streak >= 3, never this
// layer (NOTE(P1.8), contract §1.2).
// ---------------------------------------------------------------------------

// Birth creates the task's one packet, for life (Inv. 11), or — on a
// re-packaging, when the task already holds its unarchived packet —
// re-renders it in place (updates render_path, no second birth; §8
// "re-packaging re-renders it in place", A-P2-8). Requires a live packaged
// task; a fresh birth names the WIP cap ahead of the substrate trigger.
func Birth(ctx context.Context, q Q, taskID int64, renderPath string) error {
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if r.Archived || r.Status != "packaged" {
		return Errf(CodeNotPackaged,
			"a packet is born only from a live packaged task (Inv. 11); task %d is %q", taskID, r.Status)
	}

	var archived sql.NullInt64
	err = q.QueryRowContext(ctx,
		`SELECT archived FROM review_packets WHERE task_id = ?`, taskID).Scan(&archived)
	switch {
	case err == sql.ErrNoRows:
		// Fresh birth: the queue must have room (Inv. 18), named here; the
		// packets_wip_cap trigger is the backstop.
		occ, err := Occupancy(ctx, q)
		if err != nil {
			return err
		}
		if occ >= 3 {
			return Errf(CodeWIPCap, "review WIP cap: at most 3 unarchived packets (Inv. 18)")
		}
		_, err = q.ExecContext(ctx,
			`INSERT INTO review_packets (task_id, render_path) VALUES (?, ?)`,
			taskID, nullIfEmpty(renderPath))
		return err
	case err != nil:
		return err
	case archived.Int64 == 1:
		// A live packaged task with an archived packet is unreachable through
		// any spec flow (archive cascades task→packet together); defensive.
		return Errf(CodeArchived,
			"task %d's one packet is archived; its slot is spent for life (Inv. 11)", taskID)
	default:
		// Re-packaging: the round-trip re-renders the same packet in place.
		_, err = q.ExecContext(ctx,
			`UPDATE review_packets SET render_path = ? WHERE task_id = ?`,
			nullIfEmpty(renderPath), taskID)
		return err
	}
}

// ApplyDeepening applies one refinement round-trip's judgment to the streak
// (§8): a genuine deepening resets it, churn increments it. Saturation is
// trigger-computed from the streak, never written here. A saturated packet
// never legitimately receives a genuine reset — refinement never dispatches
// on saturated = 1 (§10 step 2b) — so that call is named illegal ahead of
// the packets_saturated_streak_frozen backstop.
func ApplyDeepening(ctx context.Context, q Q, taskID int64, genuine bool) error {
	var streak, saturated, archived int
	err := q.QueryRowContext(ctx,
		`SELECT refine_streak, saturated, archived FROM review_packets WHERE task_id = ?`,
		taskID).Scan(&streak, &saturated, &archived)
	if err == sql.ErrNoRows {
		return Errf(CodeNotFound, "task %d holds no packet (Inv. 11)", taskID)
	}
	if err != nil {
		return err
	}
	if archived == 1 {
		return Errf(CodeArchived, "task %d's packet is archived; no deepening applies", taskID)
	}
	if genuine {
		if saturated == 1 {
			return Errf(CodeSaturated,
				"a saturated packet's streak never resets (§8, NOTE(P1.8)); it waits on the operator")
		}
		_, err = q.ExecContext(ctx,
			`UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, taskID)
		return err
	}
	_, err = q.ExecContext(ctx,
		`UPDATE review_packets SET refine_streak = refine_streak + 1 WHERE task_id = ?`, taskID)
	return err
}
