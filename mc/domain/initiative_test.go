// Initiative suite (contract §1.2 row 3): whole-wave-or-nothing birth,
// the strict-drain law named in domain (task_test covers the declare edge),
// and the block propagation / auto-clear cascades asserted against the
// substrate lattice — outcomes, never re-implementations.
package domain_test

import (
	"context"
	"strings"
	"testing"

	"mc/domain"
)

func TestBirthWave(t *testing.T) {
	t.Run("whole_wave_born_seeded", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		p1 := 1
		var ids []int64
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			ids, err = domain.BirthWave(ctx, q, init, []domain.WaveChild{
				{Title: "child one", Description: "criteria one", Priority: &p1},
				{Title: "child two"},
			})
			return err
		})
		if len(ids) != 2 {
			t.Fatalf("ids = %v", ids)
		}
		for _, id := range ids {
			if got := taskStr(t, db, id, "status"); got != "seeded" {
				t.Fatalf("child %d status = %q, want born seeded (§6.1)", id, got)
			}
			if got := taskInt(t, db, id, "initiative_id"); got != init {
				t.Fatalf("child %d initiative_id = %d", id, got)
			}
			if got := taskStr(t, db, id, "worksource"); got != "ws" {
				t.Fatalf("child %d worksource = %q, want inherited", id, got)
			}
			if got := taskStr(t, db, id, "scope"); got != "task" {
				t.Fatalf("child %d scope = %q (no nesting)", id, got)
			}
		}
		if got := taskInt(t, db, ids[0], "priority"); got != 1 {
			t.Fatalf("child priority = %d, want 1", got)
		}
		// The initiative's own status does not move (ADR-001 D4): it stays
		// seeded, now parked behind open children.
		if got := taskStr(t, db, init, "status"); got != "seeded" {
			t.Fatalf("initiative status = %q", got)
		}
	})

	t.Run("empty_wave_rejected", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		wantCode(t, db, domain.CodeEmptyWave, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, init, nil)
			return err
		})
	})

	t.Run("next_wave_requires_previous_wave_drained", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "open child"}})
			return err
		})
		before := oneInt(t, db, `SELECT COUNT(*) FROM tasks WHERE initiative_id = ?`, init)
		wantCode(t, db, domain.CodeStrictDrain, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "overlapping child"}})
			return err
		})
		if got := oneInt(t, db, `SELECT COUNT(*) FROM tasks WHERE initiative_id = ?`, init); got != before {
			t.Fatalf("overlapping wave inserted children: %d → %d", before, got)
		}
	})

	// Whole wave or nothing (constraint a): an invalid child anywhere in the
	// batch aborts before any insert lands.
	t.Run("invalid_child_aborts_whole_wave", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		before := oneInt(t, db, `SELECT COUNT(*) FROM tasks`)
		wantCode(t, db, domain.CodeReasonRequired, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, init, []domain.WaveChild{
				{Title: "fine"}, {Title: ""},
			})
			return err
		})
		if got := oneInt(t, db, `SELECT COUNT(*) FROM tasks`); got != before {
			t.Fatalf("half a wave landed: %d → %d rows", before, got)
		}
	})

	// Both layers: a wave into a non-seeded (or archived) initiative is
	// named in domain and aborted by the birth trigger.
	t.Run("worked_initiative_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, init)
		wantCode(t, db, domain.CodeIllegalTransition, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "late child"}})
			return err
		})
		wantAbort(t, db, `
			INSERT INTO tasks (title, scope, status, initiative_id, worksource)
			VALUES ('late child', 'task', 'seeded', ?, 'ws')`, init)
	})

	t.Run("non_initiative_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		wantCode(t, db, domain.CodeNotInitiative, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, id, []domain.WaveChild{{Title: "c"}})
			return err
		})
	})

	t.Run("cancelled_initiative_rejected", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, init, "descoped", "operator")
		})
		err := tx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "c"}})
			return err
		})
		if err == nil {
			t.Fatalf("wave into a cancelled initiative committed")
		}
	})
}

// Maximally strict blocking (§6.1), asserted against the lattice through
// domain operations: one blocked child blocks the initiative; the propagated
// block auto-clears when the last blocked child resolves; an operator-set
// reason never auto-clears.
func TestInitiativeBlockCascades(t *testing.T) {
	t.Run("parent_cannot_unblock_while_child_remains_blocked", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		var children []int64
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			children, err = domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "blocked child"}})
			return err
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, children[0], "waiting")
		})
		wantCode(t, db, domain.CodeBlockedChild, func(ctx context.Context, q domain.Q) error {
			return domain.Unblock(ctx, q, init)
		})
		if got := taskInt(t, db, init, "blocked"); got != 1 {
			t.Fatalf("parent block cleared while child remained blocked")
		}
		if got := taskInt(t, db, children[0], "blocked"); got != 1 {
			t.Fatalf("child block changed")
		}
		wantAbort(t, db, `UPDATE tasks SET blocked = 0 WHERE id = ?`, init)
	})

	t.Run("propagation_and_auto_clear", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		var children []int64
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			children, err = domain.BirthWave(ctx, q, init, []domain.WaveChild{
				{Title: "c1"}, {Title: "c2"},
			})
			return err
		})

		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, children[0], "stuck on operator input")
		})
		if got := taskInt(t, db, init, "blocked"); got != 1 {
			t.Fatalf("block did not propagate to the initiative (§6.1)")
		}
		if got := taskStr(t, db, init, "blocked_reason"); !strings.Contains(got, "blocked child #") {
			t.Fatalf("propagated reason = %q", got)
		}

		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Unblock(ctx, q, children[0])
		})
		if got := taskInt(t, db, init, "blocked"); got != 0 {
			t.Fatalf("propagated block did not auto-clear (§6.1)")
		}
	})

	t.Run("cancelling_last_blocked_child_clears", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		var children []int64
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			children, err = domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "c1"}})
			return err
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, children[0], "dead end")
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, children[0], "abandon this line", "operator")
		})
		if got := taskInt(t, db, init, "blocked"); got != 0 {
			t.Fatalf("cancel-archive of the last blocked child did not clear the initiative (§6.1)")
		}
	})

	t.Run("operator_set_reason_never_auto_clears", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		var children []int64
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			children, err = domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "c1"}})
			return err
		})
		// Operator blocks the initiative directly, then a child blocks and
		// unblocks: the operator's reason must survive.
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, init, "operator hold")
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, children[0], "child stuck")
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Unblock(ctx, q, children[0])
		})
		if got := taskStr(t, db, init, "blocked_reason"); got != "operator hold" {
			t.Fatalf("operator-set reason = %q, want preserved (§6.1)", got)
		}
	})
}

// ADR-020 D5, defense in depth: the send-back's license to skip an
// already-archived member ("this arm asserts nothing about the set, it
// destroys it") covers INDIVIDUAL members — it is not a license to accept a
// degenerate snapshot and report success having destroyed nothing. The arm
// asserts its own postcondition (drained) instead of trusting its input.
func TestSendBackWaveAssertsItsPostcondition(t *testing.T) {
	t.Run("a_degenerate_snapshot_fails_closed", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c1 := mkChildTask(t, db, ini)

		// An empty snapshot would destroy nothing while reporting success and
		// leaving the wave intact — a silent live-lock, since the terminal is
		// accepted and charges no dispatch_retries.
		wantCode(t, db, domain.CodePoolMismatch, func(ctx context.Context, q domain.Q) error {
			return domain.SendBackWave(ctx, q, ini, nil, "the objection")
		})
		if got := taskInt(t, db, c1, "archived"); got != 0 {
			t.Fatalf("the refused send-back still archived child %d", c1)
		}
	})

	t.Run("a_partial_snapshot_fails_closed", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c1 := mkChildTask(t, db, ini)
		c2 := mkChildTask(t, db, ini)

		// Naming only one of two open children would leave the initiative
		// undrained, so planReviewPending stays true and the Editor is
		// re-dispatched forever.
		wantCode(t, db, domain.CodePoolMismatch, func(ctx context.Context, q domain.Q) error {
			return domain.SendBackWave(ctx, q, ini, []int64{c1}, "the objection")
		})
		_ = c2
	})

	t.Run("an_already_archived_member_is_still_tolerated", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c1 := mkChildTask(t, db, ini)
		c2 := mkChildTask(t, db, ini)
		// The operator cancels one child mid-review: the snapshot still names
		// it, and the arm must destroy the rest rather than refuse — it is
		// already in the target state. The postcondition still holds.
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, c1, "operator changed their mind", "operator")
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.SendBackWave(ctx, q, ini, []int64{c1, c2}, "the objection")
		})
		if got := taskStr(t, db, c2, "decision"); got != "cancelled" {
			t.Fatalf("survivor decision = %q, want cancelled", got)
		}
		var open int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM tasks WHERE initiative_id = ? AND archived = 0`, ini).Scan(&open); err != nil {
			t.Fatal(err)
		}
		if open != 0 {
			t.Fatalf("initiative not drained: %d open", open)
		}
	})
}
