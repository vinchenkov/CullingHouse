// Task state machine suite (contract §1.2 row 1): the Editor arms, forward
// advances, the §7 verdict table (PASS / CORRECT / BUDGET-SPENT), re-entry,
// blocking, and the decision writes — with one backstop-agreement case per
// rule class: the domain rejects with its named error AND the same write as
// raw SQL aborts on the trigger lattice (contract §1.1).
package domain_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"mc/domain"
)

func TestPromote(t *testing.T) {
	t.Run("proposed_to_seeded", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "proposed")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Promote(ctx, q, id)
		})
		if got := taskStr(t, db, id, "status"); got != "seeded" {
			t.Fatalf("status = %q, want seeded", got)
		}
	})

	// Backstop agreement (illegal-transition rule class): named error in the
	// domain, trigger abort on the raw write.
	t.Run("non_proposed_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "verified")
		wantCode(t, db, domain.CodeIllegalTransition, func(ctx context.Context, q domain.Q) error {
			return domain.Promote(ctx, q, id)
		})
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
	})

	t.Run("decided_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "proposed")
		mustExec(t, db, `UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1 WHERE id = ?`, id)
		wantCode(t, db, domain.CodeArchived, func(ctx context.Context, q domain.Q) error {
			return domain.Promote(ctx, q, id)
		})
	})

	t.Run("missing_task", func(t *testing.T) {
		db := openSpine(t)
		wantCode(t, db, domain.CodeNotFound, func(ctx context.Context, q domain.Q) error {
			return domain.Promote(ctx, q, 999)
		})
	})
}

func TestRejectProposal(t *testing.T) {
	t.Run("rejects_archives_and_records_reason", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "proposed")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.RejectProposal(ctx, q, id, "duplicate of existing work")
		})
		if got := taskStr(t, db, id, "decision"); got != "rejected" {
			t.Fatalf("decision = %q", got)
		}
		if got := taskInt(t, db, id, "archived"); got != 1 {
			t.Fatalf("not archived")
		}
		if got := taskStr(t, db, id, "decided_at"); got == "<NULL>" {
			t.Fatalf("decided_at not stamped")
		}
		if got := oneStr(t, db, `SELECT detail FROM activity WHERE kind = 'task.rejected' AND subject = ?`, id); got != "duplicate of existing work" {
			t.Fatalf("activity reason = %q", got)
		}
	})

	t.Run("reason_mandatory", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "proposed")
		wantCode(t, db, domain.CodeReasonRequired, func(ctx context.Context, q domain.Q) error {
			return domain.RejectProposal(ctx, q, id, "")
		})
	})

	t.Run("non_proposed_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		wantCode(t, db, domain.CodeIllegalTransition, func(ctx context.Context, q domain.Q) error {
			return domain.RejectProposal(ctx, q, id, "r")
		})
	})
}

func TestAdvanceStage(t *testing.T) {
	t.Run("seeded_to_worked", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, id, "worked")
		})
		if got := taskStr(t, db, id, "status"); got != "worked" {
			t.Fatalf("status = %q", got)
		}
	})

	t.Run("verified_to_packaged_clears_refine_notes", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "verified")
		mustExec(t, db, `UPDATE tasks SET refine_notes = 'this round''s brief' WHERE id = ?`, id)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, id, "packaged")
		})
		if got := taskStr(t, db, id, "status"); got != "packaged" {
			t.Fatalf("status = %q", got)
		}
		if got := taskStr(t, db, id, "refine_notes"); got != "<NULL>" {
			t.Fatalf("refine_notes = %q, want cleared on packaging (NOTE(P2.3))", got)
		}
	})

	// Backstop agreement: wrong-source-status rejections, both layers.
	t.Run("illegal_edges_rejected_both_layers", func(t *testing.T) {
		tests := []struct {
			name, from, to string
		}{
			{"proposed_to_worked", "proposed", "worked"},
			{"worked_to_worked_from_verified", "verified", "worked"},
			{"seeded_to_packaged", "seeded", "packaged"},
			{"packaged_to_worked", "packaged", "worked"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				db := openSpine(t)
				id := mkTask(t, db, "task", tc.from)
				wantCode(t, db, domain.CodeIllegalTransition, func(ctx context.Context, q domain.Q) error {
					return domain.AdvanceStage(ctx, q, id, tc.to)
				})
				wantAbort(t, db, `UPDATE tasks SET status = ? WHERE id = ?`, tc.to, id)
			})
		}
	})

	// Strict drain (§6.1), both layers: the initiative done-declaration is
	// refused while children are open, named ahead of the trigger.
	t.Run("initiative_strict_drain", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		child := mkChildTask(t, db, init)
		wantCode(t, db, domain.CodeStrictDrain, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, init, "worked")
		})
		wantAbort(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, init)

		// Drain the child (cancel-archive) → the declaration goes through.
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, child, "descoped")
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, init, "worked")
		})
		if got := taskStr(t, db, init, "status"); got != "worked" {
			t.Fatalf("initiative status = %q, want worked", got)
		}
	})

	t.Run("unknown_target_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		wantCode(t, db, domain.CodeIllegalTransition, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, id, "verified") // rides ApplyVerdict, never this
		})
	})
}

// mkChildTask births a wave child (born seeded) into the initiative.
func mkChildTask(t *testing.T, db *sql.DB, initiative int64) int64 {
	t.Helper()
	res := mustExec(t, db, `
		INSERT INTO tasks (title, scope, status, initiative_id, worksource)
		VALUES ('child', 'task', 'seeded', ?, 'ws')`, initiative)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func applyVerdict(t *testing.T, db *sql.DB, a domain.VerdictArgs) (domain.VerdictResult, error) {
	t.Helper()
	var res domain.VerdictResult
	err := tx(t, db, func(ctx context.Context, q domain.Q) error {
		var err error
		res, err = domain.ApplyVerdict(ctx, q, a)
		return err
	})
	return res, err
}

func TestApplyVerdictPass(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "worked")
	mkRun(t, db, "v1", "verifier", id)

	res, err := applyVerdict(t, db, domain.VerdictArgs{
		TaskID: id, RunID: "v1", Outcome: "pass",
		EvidencePath: "e.md", VerifiedSHA: "abc123",
	})
	if err != nil {
		t.Fatalf("pass verdict: %v", err)
	}
	if res.Status != "verified" || res.ExceptionLabeled {
		t.Fatalf("result = %+v", res)
	}
	if got := taskStr(t, db, id, "status"); got != "verified" {
		t.Fatalf("status = %q", got)
	}
	if got := taskStr(t, db, id, "verified_sha"); got != "abc123" {
		t.Fatalf("verified_sha = %q", got)
	}
	// The verdict record lands on the verifier's own runs row (NOTE(P2.2)).
	if got := oneStr(t, db, `SELECT verdict_outcome || '/' || evidence_path FROM runs WHERE id = 'v1'`); got != "pass/e.md" {
		t.Fatalf("verdict record = %q", got)
	}
}

func TestApplyVerdictCorrect(t *testing.T) {
	t.Run("correct_sends_back_and_increments", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mkRun(t, db, "v1", "verifier", id)
		mustExec(t, db, `UPDATE tasks SET dispatch_retries = 3 WHERE id = ?`, id)

		res, err := applyVerdict(t, db, domain.VerdictArgs{
			TaskID: id, RunID: "v1", Outcome: "correct",
			EvidencePath: "e.md", CorrectionPath: "corrections/mc-1-corrections1",
		})
		if err != nil {
			t.Fatalf("correct verdict: %v", err)
		}
		if res.Status != "seeded" || res.CorrectionCount != 1 {
			t.Fatalf("result = %+v", res)
		}
		if got := taskInt(t, db, id, "correction_count"); got != 1 {
			t.Fatalf("correction_count = %d", got)
		}
		if got := taskStr(t, db, id, "status"); got != "seeded" {
			t.Fatalf("status = %q", got)
		}
		if got := oneStr(t, db, `SELECT correction_path FROM runs WHERE id = 'v1'`); got != "corrections/mc-1-corrections1" {
			t.Fatalf("correction_path = %q", got)
		}
		// Two budgets never blur (§10, contract §2): a CORRECT verdict leaves
		// dispatch_retries untouched.
		if got := taskInt(t, db, id, "dispatch_retries"); got != 3 {
			t.Fatalf("dispatch_retries = %d, want 3 untouched (two budgets)", got)
		}
	})

	t.Run("correction_path_required", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mkRun(t, db, "v1", "verifier", id)
		wantCode(t, db, domain.CodeCorrectionRequired, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "correct", EvidencePath: "e",
			})
			return err
		})
	})

	// The fourth verdict must be budget-spent, both layers: the domain names
	// the exhaustion; the CHECK (correction_count BETWEEN 0 AND 3) backstops
	// the arithmetic.
	t.Run("fourth_correct_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mkRun(t, db, "v1", "verifier", id)
		mustExec(t, db, `UPDATE tasks SET correction_count = 3 WHERE id = ?`, id)
		wantCode(t, db, domain.CodeBudgetExhausted, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "correct",
				EvidencePath: "e", CorrectionPath: "c",
			})
			return err
		})
		wantAbort(t, db, `UPDATE tasks SET correction_count = 4 WHERE id = ?`, id)
	})

	// The rally walks 0→3 through real verdicts, then budget-spent ships.
	t.Run("full_rally_walk", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		for i := 1; i <= 3; i++ {
			run := "v" + string(rune('0'+i))
			mkRun(t, db, run, "verifier", id)
			res, err := applyVerdict(t, db, domain.VerdictArgs{
				TaskID: id, RunID: run, Outcome: "correct",
				EvidencePath: "e", CorrectionPath: "c",
			})
			if err != nil {
				t.Fatalf("round %d: %v", i, err)
			}
			if res.CorrectionCount != i {
				t.Fatalf("round %d count = %d", i, res.CorrectionCount)
			}
			// Worker round: seeded → worked again.
			mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, id)
		}
		mkRun(t, db, "v4", "verifier", id)
		res, err := applyVerdict(t, db, domain.VerdictArgs{
			TaskID: id, RunID: "v4", Outcome: "budget-spent",
			EvidencePath: "e", VerifiedSHA: "sha4",
		})
		if err != nil {
			t.Fatalf("budget-spent: %v", err)
		}
		if res.Status != "verified" || !res.ExceptionLabeled {
			t.Fatalf("result = %+v, want exception-labeled verified (§7)", res)
		}
	})
}

func TestApplyVerdictBudgetSpent(t *testing.T) {
	t.Run("requires_count_exactly_three", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mkRun(t, db, "v1", "verifier", id)
		wantCode(t, db, domain.CodeBudgetRemaining, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "budget-spent", EvidencePath: "e", VerifiedSHA: "s",
			})
			return err
		})
	})

	t.Run("ships_exception_labeled", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mkRun(t, db, "v1", "verifier", id)
		mustExec(t, db, `UPDATE tasks SET correction_count = 3 WHERE id = ?`, id)
		res, err := applyVerdict(t, db, domain.VerdictArgs{
			TaskID: id, RunID: "v1", Outcome: "budget-spent", EvidencePath: "e", VerifiedSHA: "cafe",
		})
		if err != nil {
			t.Fatalf("budget-spent: %v", err)
		}
		if !res.ExceptionLabeled || res.Status != "verified" {
			t.Fatalf("result = %+v", res)
		}
		if got := oneStr(t, db, `SELECT verdict_outcome FROM runs WHERE id = 'v1'`); got != "budget-spent" {
			t.Fatalf("verdict record = %q", got)
		}
		if got := taskInt(t, db, id, "correction_count"); got != 3 {
			t.Fatalf("correction_count = %d, want 3 untouched", got)
		}
	})
}

func TestApplyVerdictCarrierMatrix(t *testing.T) {
	tests := []struct {
		name  string
		args  domain.VerdictArgs
		code  string
		spent bool
	}{
		{name: "pass_requires_evidence", args: domain.VerdictArgs{Outcome: "pass", VerifiedSHA: "sha"}, code: domain.CodeEvidenceRequired},
		{name: "pass_requires_sha", args: domain.VerdictArgs{Outcome: "pass", EvidencePath: "e"}, code: domain.CodeSHARequired},
		{name: "pass_forbids_correction", args: domain.VerdictArgs{Outcome: "pass", EvidencePath: "e", VerifiedSHA: "sha", CorrectionPath: "c"}, code: domain.CodeCarrierForbidden},
		{name: "correct_requires_evidence", args: domain.VerdictArgs{Outcome: "correct", CorrectionPath: "c"}, code: domain.CodeEvidenceRequired},
		{name: "correct_forbids_sha", args: domain.VerdictArgs{Outcome: "correct", EvidencePath: "e", VerifiedSHA: "sha", CorrectionPath: "c"}, code: domain.CodeCarrierForbidden},
		{name: "budget_spent_requires_sha", args: domain.VerdictArgs{Outcome: "budget-spent", EvidencePath: "e"}, code: domain.CodeSHARequired, spent: true},
		{name: "budget_spent_forbids_correction", args: domain.VerdictArgs{Outcome: "budget-spent", EvidencePath: "e", VerifiedSHA: "sha", CorrectionPath: "c"}, code: domain.CodeCarrierForbidden, spent: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openSpine(t)
			id := mkTask(t, db, "task", "worked")
			mkRun(t, db, "v1", "verifier", id)
			if tc.spent {
				mustExec(t, db, `UPDATE tasks SET correction_count = 3 WHERE id = ?`, id)
			}
			a := tc.args
			a.TaskID, a.RunID = id, "v1"
			wantCode(t, db, tc.code, func(ctx context.Context, q domain.Q) error {
				_, err := domain.ApplyVerdict(ctx, q, a)
				return err
			})
			if got := taskStr(t, db, id, "status"); got != "worked" {
				t.Fatalf("invalid carrier moved task to %q", got)
			}
			if got := oneStr(t, db, `SELECT verdict_outcome FROM runs WHERE id = 'v1'`); got != "<NULL>" {
				t.Fatalf("invalid carrier wrote verdict %q", got)
			}
		})
	}
}

func TestApplyVerdictRequiresMatchingVerifierRun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		makeRun func(t *testing.T, db *sql.DB, taskID int64)
		code    string
	}{
		{name: "missing_run", makeRun: func(t *testing.T, db *sql.DB, taskID int64) {}, code: domain.CodeNotFound},
		{name: "wrong_role", makeRun: func(t *testing.T, db *sql.DB, taskID int64) {
			mkRun(t, db, "v1", "worker", taskID)
		}, code: domain.CodeRoleMismatch},
		{name: "wrong_subject", makeRun: func(t *testing.T, db *sql.DB, taskID int64) {
			other := mkTask(t, db, "task", "worked")
			mkRun(t, db, "v1", "verifier", other)
		}, code: domain.CodeStaleRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openSpine(t)
			id := mkTask(t, db, "task", "worked")
			tc.makeRun(t, db, id)
			wantCode(t, db, tc.code, func(ctx context.Context, q domain.Q) error {
				_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
					TaskID: id, RunID: "v1", Outcome: "pass",
					EvidencePath: "e", VerifiedSHA: "sha",
				})
				return err
			})
			if got := taskStr(t, db, id, "status"); got != "worked" {
				t.Fatalf("invalid Run moved task to %q", got)
			}
		})
	}
}

// The refinement round-trip (§8, A-P2-1): the live packet is the derived
// marker; --deepening is required at the rally-ending verdict and applied to
// the streak in the same transaction.
func TestApplyVerdictDeepening(t *testing.T) {
	// roundTrip builds a task holding a live packet that re-entered and
	// reached worked again.
	roundTrip := func(t *testing.T) (*sql.DB, int64) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustExec(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, id)
		mkRun(t, db, "v1", "verifier", id)
		return db, id
	}

	t.Run("pass_without_deepening_rejected", func(t *testing.T) {
		db, id := roundTrip(t)
		wantCode(t, db, domain.CodeDeepeningRequired, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "pass", EvidencePath: "e", VerifiedSHA: "s",
			})
			return err
		})
	})

	t.Run("genuine_pass_resets_streak", func(t *testing.T) {
		db, id := roundTrip(t)
		mustExec(t, db, `UPDATE review_packets SET refine_streak = 2 WHERE task_id = ?`, id)
		if _, err := applyVerdict(t, db, domain.VerdictArgs{
			TaskID: id, RunID: "v1", Outcome: "pass",
			EvidencePath: "e", VerifiedSHA: "s", Deepening: "genuine",
		}); err != nil {
			t.Fatalf("genuine pass: %v", err)
		}
		if got := oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, id); got != 0 {
			t.Fatalf("refine_streak = %d, want reset to 0 (§8)", got)
		}
		if got := oneStr(t, db, `SELECT deepening FROM runs WHERE id = 'v1'`); got != "genuine" {
			t.Fatalf("deepening record = %q", got)
		}
	})

	t.Run("churn_pass_rejected_unchanged", func(t *testing.T) {
		db, id := roundTrip(t)
		wantCode(t, db, domain.CodeDeepeningForbidden, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "pass",
				EvidencePath: "e", VerifiedSHA: "s", Deepening: "churn",
			})
			return err
		})
		if got := taskStr(t, db, id, "status"); got != "worked" {
			t.Fatalf("rejected churn PASS moved task to %q", got)
		}
		if got := oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, id); got != 0 {
			t.Fatalf("rejected churn PASS changed streak to %d", got)
		}
	})

	t.Run("budget_spent_genuine_rejected", func(t *testing.T) {
		db, id := roundTrip(t)
		mustExec(t, db, `UPDATE tasks SET correction_count = 3 WHERE id = ?`, id)
		wantCode(t, db, domain.CodeDeepeningForbidden, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "budget-spent",
				EvidencePath: "e", VerifiedSHA: "s", Deepening: "genuine",
			})
			return err
		})
	})

	t.Run("budget_spent_churn_increments", func(t *testing.T) {
		db, id := roundTrip(t)
		mustExec(t, db, `UPDATE tasks SET correction_count = 3 WHERE id = ?`, id)
		if _, err := applyVerdict(t, db, domain.VerdictArgs{
			TaskID: id, RunID: "v1", Outcome: "budget-spent",
			EvidencePath: "e", VerifiedSHA: "s", Deepening: "churn",
		}); err != nil {
			t.Fatalf("budget-spent churn: %v", err)
		}
		if got := oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("refine_streak = %d, want 1 (budget-spent is churn)", got)
		}
	})

	// A mid-rally CORRECT never applies deepening — the rally has not ended
	// (A-P2-1): the flag is refused, the streak untouched.
	t.Run("correct_with_deepening_rejected", func(t *testing.T) {
		db, id := roundTrip(t)
		wantCode(t, db, domain.CodeDeepeningForbidden, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "correct",
				EvidencePath: "e", CorrectionPath: "c", Deepening: "churn",
			})
			return err
		})
	})

	t.Run("deepening_outside_round_trip_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked") // no packet: first pass through
		mkRun(t, db, "v1", "verifier", id)
		wantCode(t, db, domain.CodeDeepeningForbidden, func(ctx context.Context, q domain.Q) error {
			_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: id, RunID: "v1", Outcome: "pass",
				EvidencePath: "e", VerifiedSHA: "s", Deepening: "genuine",
			})
			return err
		})
	})
}

func TestApplyVerdictOnNonWorked(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "seeded")
	mkRun(t, db, "v1", "verifier", id)
	wantCode(t, db, domain.CodeIllegalTransition, func(ctx context.Context, q domain.Q) error {
		_, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
			TaskID: id, RunID: "v1", Outcome: "pass", EvidencePath: "e", VerifiedSHA: "s",
		})
		return err
	})
	wantAbort(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, id)
}

func TestReenter(t *testing.T) {
	t.Run("packaged_to_seeded_with_notes", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "deepen the risk section")
		})
		if got := taskStr(t, db, id, "status"); got != "seeded" {
			t.Fatalf("status = %q", got)
		}
		if got := taskStr(t, db, id, "refine_notes"); got != "deepen the risk section" {
			t.Fatalf("refine_notes = %q", got)
		}
	})

	// The row carries only the *next* brief's payload (A-P2-6): a re-entry
	// with no notes overwrites the stale ones.
	t.Run("notes_overwritten_per_reentry", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "first notes")
		})
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET status = 'packaged' WHERE id = ?`, id)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "")
		})
		if got := taskStr(t, db, id, "refine_notes"); got != "<NULL>" {
			t.Fatalf("refine_notes = %q, want overwritten to NULL", got)
		}
	})

	t.Run("non_packaged_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		wantCode(t, db, domain.CodeNotPackaged, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "n")
		})
	})

	t.Run("packaged_without_live_packet_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		wantCode(t, db, domain.CodeNotFound, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "cannot revise invisible work")
		})
		if got := taskStr(t, db, id, "status"); got != "packaged" {
			t.Fatalf("packetless re-entry moved task to %q", got)
		}
	})

	// Decided rows never transition (§6): an approved packaged row holds at
	// packaged through landing — revise-after-approve is refused both layers.
	t.Run("decided_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustExec(t, db, `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id)
		wantCode(t, db, domain.CodeAlreadyDecided, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, id, "n")
		})
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
	})
}

func TestBlockUnblock(t *testing.T) {
	t.Run("block_sets_flag_and_reason", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, id, "needs the prod credentials decision")
		})
		if got := taskInt(t, db, id, "blocked"); got != 1 {
			t.Fatalf("blocked = %d", got)
		}
		if got := taskStr(t, db, id, "blocked_reason"); got != "needs the prod credentials decision" {
			t.Fatalf("blocked_reason = %q", got)
		}
		// Blocking never destroys pipeline position (§5).
		if got := taskStr(t, db, id, "status"); got != "worked" {
			t.Fatalf("status = %q, want worked untouched", got)
		}
	})

	t.Run("block_without_reason_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		wantCode(t, db, domain.CodeReasonRequired, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, id, "")
		})
		wantAbort(t, db, `UPDATE tasks SET blocked = 1 WHERE id = ?`, id)
	})

	t.Run("unblock_clears_reason", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Block(ctx, q, id, "r")
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Unblock(ctx, q, id)
		})
		if got := taskInt(t, db, id, "blocked"); got != 0 {
			t.Fatalf("blocked = %d", got)
		}
		if got := taskStr(t, db, id, "blocked_reason"); got != "<NULL>" {
			t.Fatalf("blocked_reason = %q, want trigger-cleared (§6)", got)
		}
	})

	t.Run("unblock_unblocked_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "seeded")
		wantCode(t, db, domain.CodeNotBlocked, func(ctx context.Context, q domain.Q) error {
			return domain.Unblock(ctx, q, id)
		})
	})
}

func TestApprove(t *testing.T) {
	t.Run("branchless_archives_synchronously", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		var archived bool
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			archived, err = domain.Approve(ctx, q, id)
			return err
		})
		if !archived {
			t.Fatalf("branchless approve did not archive (§7)")
		}
		// The packet cascade fired with it (NOTE(P1.6)).
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("packet not cascaded")
		}
	})

	t.Run("branch_carrying_holds_for_landing", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustExec(t, db, `UPDATE tasks SET branch = 'mc/task-1', verified_sha = 'abc', target_ref = 'main' WHERE id = ?`, id)
		var archived bool
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			archived, err = domain.Approve(ctx, q, id)
			return err
		})
		if archived {
			t.Fatalf("branch-carrying approve archived synchronously (§7)")
		}
		if got := taskStr(t, db, id, "decision"); got != "approved" {
			t.Fatalf("decision = %q", got)
		}
	})

	t.Run("packet_and_complete_landing_fence_required", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			setup string
			code  string
		}{
			{name: "missing_packet", setup: `UPDATE tasks SET branch = 'mc/task-x', verified_sha = 'abc', target_ref = 'main' WHERE id = ?`, code: domain.CodeNotFound},
			{name: "missing_verified_sha", setup: `UPDATE tasks SET branch = 'mc/task-x', target_ref = 'main' WHERE id = ?`, code: domain.CodeLandingFence},
			{name: "missing_target_ref", setup: `UPDATE tasks SET branch = 'mc/task-x', verified_sha = 'abc', target_ref = NULL WHERE id = ?`, code: domain.CodeLandingFence},
		} {
			t.Run(tc.name, func(t *testing.T) {
				db := openSpine(t)
				id := mkTask(t, db, "task", "packaged")
				if tc.name != "missing_packet" {
					mkPacket(t, db, id)
				}
				mustExec(t, db, tc.setup, id)
				wantCode(t, db, tc.code, func(ctx context.Context, q domain.Q) error {
					_, err := domain.Approve(ctx, q, id)
					return err
				})
				if got := taskStr(t, db, id, "decision"); got != "<NULL>" {
					t.Fatalf("invalid landing fence wrote decision %q", got)
				}
			})
		}
	})

	t.Run("non_packaged_rejected_both_layers", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "worked")
		wantCode(t, db, domain.CodeNotPackaged, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Approve(ctx, q, id)
			return err
		})
		wantAbort(t, db, `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id)
	})

	t.Run("double_decision_rejected", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustExec(t, db, `UPDATE tasks SET branch = 'mc/task-x', verified_sha = 'abc', target_ref = 'main' WHERE id = ?`, id) // holds unarchived
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Approve(ctx, q, id)
			return err
		})
		wantCode(t, db, domain.CodeAlreadyDecided, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Approve(ctx, q, id)
			return err
		})
	})
}

func TestCancel(t *testing.T) {
	t.Run("cancels_archives_and_records_reason", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, id, "direction changed")
		})
		if got := taskStr(t, db, id, "decision"); got != "cancelled" {
			t.Fatalf("decision = %q", got)
		}
		if got := taskInt(t, db, id, "archived"); got != 1 {
			t.Fatalf("not archived")
		}
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("packet not cascaded")
		}
		if got := oneStr(t, db, `SELECT detail FROM activity WHERE kind = 'task.cancelled' AND subject = ?`, id); got != "direction changed" {
			t.Fatalf("activity reason = %q", got)
		}
	})

	t.Run("reason_mandatory", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		wantCode(t, db, domain.CodeReasonRequired, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, id, "")
		})
	})

	// Cancelling an initiative cascades (§6.1): open children cancelled and
	// archived, their packets with them — one implementation, in the lattice;
	// this suite asserts the outcome, never re-implements it.
	t.Run("initiative_cancel_cascades", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		c1 := mkChildTask(t, db, init)
		c2 := mkChildTask(t, db, init)
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, c2)
		mustExec(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, c2)
		mustExec(t, db, `UPDATE tasks SET status = 'packaged' WHERE id = ?`, c2)
		mkPacket(t, db, c2)

		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, init, "descoped")
		})
		for _, c := range []int64{c1, c2} {
			if got := taskStr(t, db, c, "decision"); got != "cancelled" {
				t.Fatalf("child %d decision = %q", c, got)
			}
			if got := taskInt(t, db, c, "archived"); got != 1 {
				t.Fatalf("child %d not archived", c)
			}
		}
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, c2); got != 1 {
			t.Fatalf("child packet not cascaded")
		}
	})

	t.Run("initiative_cancel_overwrites_open_child_approval", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		child := mkChildTask(t, db, init)
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, child)
		mustExec(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, child)
		mustExec(t, db, `UPDATE tasks SET status = 'packaged', branch = 'mc/initiative-child', verified_sha = 'abc', target_ref = 'main' WHERE id = ?`, child)
		mkPacket(t, db, child)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Approve(ctx, q, child)
			return err
		})
		if got := taskInt(t, db, child, "archived"); got != 0 {
			t.Fatalf("landing-pending child archived before parent cancellation")
		}

		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, init, "initiative descoped")
		})
		if got := taskStr(t, db, child, "decision"); got != "cancelled" {
			t.Fatalf("open child decision = %q, want parent cancellation to win", got)
		}
		if got := taskInt(t, db, child, "archived"); got != 1 {
			t.Fatalf("open child not archived")
		}
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, child); got != 1 {
			t.Fatalf("open child's packet not archived")
		}
	})
}

func TestBirthProposal(t *testing.T) {
	db := openSpine(t)
	var id int64
	mustTx(t, db, func(ctx context.Context, q domain.Q) error {
		var err error
		id, err = domain.BirthProposal(ctx, q, domain.ProposalArgs{
			Title: "an idea", Description: "criteria", Origin: "autonomous", Worksource: "ws",
		})
		return err
	})
	if got := taskStr(t, db, id, "status"); got != "proposed" {
		t.Fatalf("status = %q", got)
	}
	if got := taskStr(t, db, id, "origin"); got != "autonomous" {
		t.Fatalf("origin = %q", got)
	}

	wantCode(t, db, domain.CodeReasonRequired, func(ctx context.Context, q domain.Q) error {
		_, err := domain.BirthProposal(ctx, q, domain.ProposalArgs{Title: "", Worksource: "ws", Origin: "user"})
		return err
	})
}

// Guard on message quality: named errors carry the section they enforce.
func TestErrorsNameTheirLaw(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "worked")
	err := tx(t, db, func(ctx context.Context, q domain.Q) error {
		return domain.Reenter(ctx, q, id, "")
	})
	if err == nil || !strings.Contains(err.Error(), "§") {
		t.Fatalf("domain rejection should cite its spec law: %v", err)
	}
}
