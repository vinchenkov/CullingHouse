// Lease suite (contract §1.2 row 6, §3): CAS claim, fenced heartbeat that
// never extends the hard deadline (Inv. 1), fenced release, the reap write
// side, and zombie-vs-new-holder at the transaction grain.
package domain_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mc/domain"
)

var testNow = time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)

func claim(t *testing.T, db *sql.DB, a domain.ClaimArgs) error {
	t.Helper()
	return tx(t, db, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Claim(ctx, q, testNow, a)
		return err
	})
}

func lockStr(t *testing.T, db *sql.DB, col string) string {
	t.Helper()
	return oneStr(t, db, `SELECT `+col+` FROM lock WHERE id = 1`)
}

func TestClaim(t *testing.T) {
	t.Run("stamps_lease_and_runs_row", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		var res domain.ClaimResult
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			res, err = domain.Claim(ctx, q, testNow, domain.ClaimArgs{
				RunID: "r1", Owner: "worker", SubjectID: &id,
				SessionPath: "sessions/r1", Binding: "fake",
				HardDeadlineMinutes: 240,
			})
			return err
		})
		if res.Worksource == nil || *res.Worksource != "ws" {
			t.Fatalf("worksource = %v, want derived from the subject", res.Worksource)
		}
		if got := lockStr(t, db, "run_id"); got != "r1" {
			t.Fatalf("run_id = %q", got)
		}
		if got := lockStr(t, db, "owner"); got != "worker" {
			t.Fatalf("owner = %q", got)
		}
		// Lease math on the injected clock (contract §1.1): acquired_at = now,
		// hard_deadline_at = now + hard_deadline_minutes, exactly.
		if got := lockStr(t, db, "acquired_at"); got != "2026-07-12 10:00:00" {
			t.Fatalf("acquired_at = %q", got)
		}
		if got := lockStr(t, db, "hard_deadline_at"); got != "2026-07-12 14:00:00" {
			t.Fatalf("hard_deadline_at = %q, want now + 240m (Inv. 1)", got)
		}
		if got := lockStr(t, db, "last_heartbeat_at"); got != "<NULL>" {
			t.Fatalf("last_heartbeat_at = %q, want NULL at claim (the watchdog's case)", got)
		}
		// Inv. 4: the runs row exists in the same transaction.
		if got := oneStr(t, db, `SELECT role || '/' || binding FROM runs WHERE id = 'r1'`); got != "worker/fake" {
			t.Fatalf("runs row = %q", got)
		}
	})

	t.Run("editor_pool_snapshot_stamped", func(t *testing.T) {
		db := openSpine(t)
		a := mkTask(t, db, "task", "proposed")
		b := mkTask(t, db, "task", "proposed")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Claim(ctx, q, testNow, domain.ClaimArgs{
				RunID: "e1", Owner: "editor", SubjectID: &a,
				PoolSnapshot: []int64{a, b}, HardDeadlineMinutes: 240,
			})
			return err
		})
		want := `[` + itoa(a) + `,` + itoa(b) + `]`
		if got := oneStr(t, db, `SELECT pool_snapshot FROM runs WHERE id = 'e1'`); got != want {
			t.Fatalf("pool_snapshot = %q, want %q", got, want)
		}
	})

	// CAS single-winner at the domain grain (contract §3): a claim against a
	// held lock aborts before any write lands — no runs row for the loser.
	t.Run("claim_against_held_lock_aborts_before_writes", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		if err := claim(t, db, domain.ClaimArgs{
			RunID: "winner", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
		}); err != nil {
			t.Fatalf("first claim: %v", err)
		}
		wantCode(t, db, domain.CodeLeaseHeld, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Claim(ctx, q, testNow, domain.ClaimArgs{
				RunID: "loser", Owner: "editor", HardDeadlineMinutes: 240,
			})
			return err
		})
		if got := oneInt(t, db, `SELECT COUNT(*) FROM runs`); got != 1 {
			t.Fatalf("runs rows = %d, want 1 (the loser must not open a run)", got)
		}
		if got := lockStr(t, db, "run_id"); got != "winner" {
			t.Fatalf("run_id = %q, want the winner untouched", got)
		}
	})

	t.Run("subjectless_claim_carries_no_worksource", func(t *testing.T) {
		db := openSpine(t)
		var res domain.ClaimResult
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			res, err = domain.Claim(ctx, q, testNow, domain.ClaimArgs{
				RunID: "s1", Owner: "strategist", HardDeadlineMinutes: 240,
			})
			return err
		})
		if res.Worksource != nil {
			t.Fatalf("worksource = %v, want nil (subjectless, ADR-001 constraint b)", *res.Worksource)
		}
		if got := lockStr(t, db, "subject"); got != "<NULL>" {
			t.Fatalf("subject = %q", got)
		}
	})
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func TestHeartbeat(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "seeded")
	if err := claim(t, db, domain.ClaimArgs{
		RunID: "r1", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
	}); err != nil {
		t.Fatal(err)
	}

	deadlineBefore := lockStr(t, db, "hard_deadline_at")
	var stamped string
	mustTx(t, db, func(ctx context.Context, q domain.Q) error {
		var err error
		stamped, err = domain.Heartbeat(ctx, q, "r1")
		return err
	})
	if stamped == "" {
		t.Fatalf("heartbeat returned no stamp")
	}
	if got := lockStr(t, db, "last_heartbeat_at"); got != stamped {
		t.Fatalf("last_heartbeat_at = %q, want %q", got, stamped)
	}
	// Inv. 1, asserted on the column: no heartbeat can extend the deadline.
	if got := lockStr(t, db, "hard_deadline_at"); got != deadlineBefore {
		t.Fatalf("heartbeat moved hard_deadline_at %q → %q (Inv. 1)", deadlineBefore, got)
	}

	// Fenced: a non-holder is rejected and stamps nothing.
	before := lockStr(t, db, "last_heartbeat_at")
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Heartbeat(ctx, q, "not-the-run")
		return err
	})
	if got := lockStr(t, db, "last_heartbeat_at"); got != before {
		t.Fatalf("stale heartbeat moved the stamp")
	}
}

func TestFenceAndRelease(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "seeded")
	if err := claim(t, db, domain.ClaimArgs{
		RunID: "r1", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
	}); err != nil {
		t.Fatal(err)
	}

	mustTx(t, db, func(ctx context.Context, q domain.Q) error {
		subject, err := domain.Fence(ctx, q, "r1")
		if err != nil {
			return err
		}
		if subject == nil || *subject != id {
			t.Fatalf("fence subject = %v, want %d", subject, id)
		}
		return nil
	})
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Fence(ctx, q, "stale")
		return err
	})
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		return domain.Release(ctx, q, "stale")
	})

	mustTx(t, db, func(ctx context.Context, q domain.Q) error {
		return domain.Release(ctx, q, "r1")
	})
	for _, col := range []string{"run_id", "owner", "subject", "worksource",
		"acquired_at", "last_heartbeat_at", "hard_deadline_at"} {
		if got := lockStr(t, db, col); got != "<NULL>" {
			t.Fatalf("released lock.%s = %q, want NULL (no run residue)", col, got)
		}
	}
	// Release is single-shot: the second release is fenced off.
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		return domain.Release(ctx, q, "r1")
	})
}

func TestApplyReap(t *testing.T) {
	t.Run("marks_charges_frees", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		if err := claim(t, db, domain.ClaimArgs{
			RunID: "r1", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
		}); err != nil {
			t.Fatal(err)
		}
		var res domain.ReapResult
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			res, err = domain.ApplyReap(ctx, q, domain.ReapArgs{
				RunID: "r1", Reason: "spawn-watchdog", SubjectID: &id,
			})
			return err
		})
		if !res.Charged || res.Blocked {
			t.Fatalf("reap = %+v, want charged unblocked", res)
		}
		if got := oneStr(t, db, `SELECT outcome FROM runs WHERE id = 'r1'`); got != "reaped" {
			t.Fatalf("outcome = %q", got)
		}
		if got := taskInt(t, db, id, "dispatch_retries"); got != 2 {
			t.Fatalf("dispatch_retries = %d, want charged once", got)
		}
		if got := lockStr(t, db, "run_id"); got != "<NULL>" {
			t.Fatalf("lease not freed")
		}
	})

	t.Run("exhaustion_blocks_in_same_transaction", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		mustExec(t, db, `UPDATE tasks SET dispatch_retries = 1 WHERE id = ?`, id)
		if err := claim(t, db, domain.ClaimArgs{
			RunID: "r1", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
		}); err != nil {
			t.Fatal(err)
		}
		var res domain.ReapResult
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			res, err = domain.ApplyReap(ctx, q, domain.ReapArgs{
				RunID: "r1", Reason: "lease-timeout", SubjectID: &id,
			})
			return err
		})
		if !res.Blocked {
			t.Fatalf("reap = %+v, want blocked at exhaustion (§10 step 0)", res)
		}
		if got := taskInt(t, db, id, "blocked"); got != 1 {
			t.Fatalf("subject not blocked")
		}
	})

	t.Run("subjectless_reap_charges_nothing", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded") // an unrelated row that must stay uncharged
		if err := claim(t, db, domain.ClaimArgs{
			RunID: "s1", Owner: "strategist", HardDeadlineMinutes: 240,
		}); err != nil {
			t.Fatal(err)
		}
		var res domain.ReapResult
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			res, err = domain.ApplyReap(ctx, q, domain.ReapArgs{RunID: "s1", Reason: "spawn-watchdog"})
			return err
		})
		if res.Charged {
			t.Fatalf("subjectless reap charged (§10 step 0)")
		}
		if got := taskInt(t, db, id, "dispatch_retries"); got != 3 {
			t.Fatalf("dispatch_retries = %d, want 3 untouched", got)
		}
	})

	// Two budgets (§10 crash table; contract §2): reaping a verifier lease
	// mid-rally charges dispatch_retries, never correction_count.
	t.Run("verifier_reap_leaves_rally_budget_untouched", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mustExec(t, db, `UPDATE tasks SET correction_count = 2 WHERE id = ?`, id)
		if err := claim(t, db, domain.ClaimArgs{
			RunID: "v1", Owner: "verifier", SubjectID: &id, HardDeadlineMinutes: 240,
		}); err != nil {
			t.Fatal(err)
		}
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyReap(ctx, q, domain.ReapArgs{
				RunID: "v1", Reason: "hard-deadline", SubjectID: &id,
			})
			return err
		})
		if got := taskInt(t, db, id, "correction_count"); got != 2 {
			t.Fatalf("correction_count = %d, want 2 untouched (two budgets)", got)
		}
		if got := taskInt(t, db, id, "dispatch_retries"); got != 2 {
			t.Fatalf("dispatch_retries = %d, want charged once", got)
		}
	})
}

// Zombie-vs-new-holder (contract §3, the §10/§18 sentence): after reap +
// re-claim, the old run's writes are rejected and the new lease is
// bit-for-bit untouched.
func TestZombieVersusNewHolder(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "seeded")
	if err := claim(t, db, domain.ClaimArgs{
		RunID: "zombie", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
	}); err != nil {
		t.Fatal(err)
	}
	mustTx(t, db, func(ctx context.Context, q domain.Q) error {
		_, err := domain.ApplyReap(ctx, q, domain.ReapArgs{
			RunID: "zombie", Reason: "lease-timeout", SubjectID: &id,
		})
		return err
	})
	if err := claim(t, db, domain.ClaimArgs{
		RunID: "fresh", Owner: "worker", SubjectID: &id, HardDeadlineMinutes: 240,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot := func() string {
		return oneStr(t, db, `
			SELECT run_id || '|' || owner || '|' || COALESCE(subject, -1) || '|' ||
			       acquired_at || '|' || COALESCE(last_heartbeat_at, 'null') || '|' ||
			       hard_deadline_at
			FROM lock WHERE id = 1`)
	}
	before := snapshot()

	// The zombie's heartbeat, release, and fence are all rejected…
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Heartbeat(ctx, q, "zombie")
		return err
	})
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		return domain.Release(ctx, q, "zombie")
	})
	wantCode(t, db, domain.CodeStaleRun, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Fence(ctx, q, "zombie")
		return err
	})

	// …and the new holder's lease is bit-for-bit untouched.
	if after := snapshot(); after != before {
		t.Fatalf("zombie writes disturbed the new lease:\n  before %s\n  after  %s", before, after)
	}
}
