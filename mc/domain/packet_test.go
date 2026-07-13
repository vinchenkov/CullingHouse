// Review-packet suite (contract §1.2 row 4): birth from a live packaged task
// only, the WIP cap named ahead of the trigger, re-render in place on
// re-packaging (Inv. 11), refine_streak arithmetic, and saturation as a
// trigger-computed read.
package domain_test

import (
	"context"
	"database/sql"
	"testing"

	"mc/domain"
)

func birthPacket(t *testing.T, db *sql.DB, taskID int64, render string) error {
	t.Helper()
	return tx(t, db, func(ctx context.Context, q domain.Q) error {
		return domain.Birth(ctx, q, taskID, render)
	})
}

func TestPacketBirth(t *testing.T) {
	t.Run("born_from_live_packaged_task", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		if err := birthPacket(t, db, id, "packet/render.html"); err != nil {
			t.Fatalf("birth: %v", err)
		}
		if got := oneStr(t, db, `SELECT render_path FROM review_packets WHERE task_id = ?`, id); got != "packet/render.html" {
			t.Fatalf("render_path = %q", got)
		}
		if got := oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, id); got != 0 {
			t.Fatalf("refine_streak = %d", got)
		}
	})

	// Backstop agreement (Inv. 11): non-packaged birth named in domain,
	// aborted by the trigger.
	t.Run("non_packaged_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "verified")
		wantCode(t, db, domain.CodeNotPackaged, func(ctx context.Context, q domain.Q) error {
			return domain.Birth(ctx, q, id, "r.html")
		})
		wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, id)
	})

	// Backstop agreement (Inv. 18): the WIP cap named ahead of the trigger.
	t.Run("wip_cap_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		for i := 0; i < 3; i++ {
			id := mkTask(t, db, "task", "packaged")
			if err := birthPacket(t, db, id, "r.html"); err != nil {
				t.Fatalf("packet %d: %v", i, err)
			}
		}
		fourth := mkTask(t, db, "task", "packaged")
		wantCode(t, db, domain.CodeWIPCap, func(ctx context.Context, q domain.Q) error {
			return domain.Birth(ctx, q, fourth, "r.html")
		})
		wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, fourth)
	})

	// Re-packaging re-renders in place (Inv. 11 "exactly one per task, for
	// life"; §8; A-P2-8): the round-trip updates render_path, no second row,
	// streak untouched by the render itself.
	t.Run("repackaging_rerenders_in_place", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		if err := birthPacket(t, db, id, "v1.html"); err != nil {
			t.Fatalf("birth: %v", err)
		}
		mustExec(t, db, `UPDATE review_packets SET refine_streak = 1 WHERE task_id = ?`, id)

		// Round-trip: packaged → seeded → … → packaged again.
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "deepen")
		})
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, id)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, id, "packaged")
		})

		if err := birthPacket(t, db, id, "v2.html"); err != nil {
			t.Fatalf("re-packaging: %v", err)
		}
		if got := oneInt(t, db, `SELECT COUNT(*) FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("packet rows = %d, want exactly one for life (Inv. 11)", got)
		}
		if got := oneStr(t, db, `SELECT render_path FROM review_packets WHERE task_id = ?`, id); got != "v2.html" {
			t.Fatalf("render_path = %q, want re-rendered in place", got)
		}
		if got := oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("refine_streak = %d, want untouched by the render (§8: the verdict owns the streak)", got)
		}
	})

	t.Run("missing_task", func(t *testing.T) {
		db := openSpine(t)
		wantCode(t, db, domain.CodeNotFound, func(ctx context.Context, q domain.Q) error {
			return domain.Birth(ctx, q, 999, "r.html")
		})
	})
}

func TestApplyDeepening(t *testing.T) {
	newPacket := func(t *testing.T) (*sql.DB, int64) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		return db, id
	}
	apply := func(t *testing.T, db *sql.DB, id int64, genuine bool) error {
		t.Helper()
		return tx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.ApplyDeepening(ctx, q, id, genuine)
		})
	}
	streak := func(t *testing.T, db *sql.DB, id int64) int64 {
		t.Helper()
		return oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, id)
	}

	t.Run("churn_increments_genuine_resets", func(t *testing.T) {
		db, id := newPacket(t)
		for i := int64(1); i <= 2; i++ {
			if err := apply(t, db, id, false); err != nil {
				t.Fatalf("churn %d: %v", i, err)
			}
			if got := streak(t, db, id); got != i {
				t.Fatalf("streak = %d, want %d", got, i)
			}
		}
		if err := apply(t, db, id, true); err != nil {
			t.Fatalf("genuine: %v", err)
		}
		if got := streak(t, db, id); got != 0 {
			t.Fatalf("streak = %d, want reset to 0 (§8)", got)
		}
	})

	// Saturation is computed, never hand-set (§8): the third churn saturates
	// via the trigger — the domain only reads the outcome.
	t.Run("saturation_computed_at_three", func(t *testing.T) {
		db, id := newPacket(t)
		for i := 0; i < 3; i++ {
			if err := apply(t, db, id, false); err != nil {
				t.Fatalf("churn %d: %v", i, err)
			}
		}
		if got := oneInt(t, db, `SELECT saturated FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("saturated = %d, want trigger-computed 1 at streak 3 (§8)", got)
		}
	})

	// Backstop agreement (NOTE(P1.8)): a genuine reset on a saturated packet
	// is named in domain; the raw streak decrease aborts on the trigger.
	t.Run("saturated_reset_rejected_both_layers", func(t *testing.T) {
		db, id := newPacket(t)
		for i := 0; i < 3; i++ {
			if err := apply(t, db, id, false); err != nil {
				t.Fatalf("churn %d: %v", i, err)
			}
		}
		wantCode(t, db, domain.CodeSaturated, func(ctx context.Context, q domain.Q) error {
			return domain.ApplyDeepening(ctx, q, id, true)
		})
		wantAbort(t, db, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, id)
	})

	t.Run("no_packet_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		wantCode(t, db, domain.CodeNotFound, func(ctx context.Context, q domain.Q) error {
			return domain.ApplyDeepening(ctx, q, id, false)
		})
	})

	t.Run("archived_packet_rejected", func(t *testing.T) {
		db, id := newPacket(t)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, id, "drop")
		})
		wantCode(t, db, domain.CodeArchived, func(ctx context.Context, q domain.Q) error {
			return domain.ApplyDeepening(ctx, q, id, false)
		})
	})
}
