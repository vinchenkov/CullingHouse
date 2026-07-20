package domain

import (
	"context"
	"database/sql"
	"unicode/utf8"
)

const maxDispatchScalarBytes = 4 * 1024

func validDispatchScalarAdmission(value string) bool {
	if value == "" || len(value) > maxDispatchScalarBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Task state machine (spec §6, §7) — the Editor arms, the forward stage
// advances, the verdict table, re-entry, blocking, and the decision writes.
// Contract: phase2-contract §1.2 row 1.
// ---------------------------------------------------------------------------

// Promote is the Editor's promote arm: proposed → seeded (§6).
func Promote(ctx context.Context, q Q, taskID int64) error {
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if err := requireLive(r); err != nil {
		return err
	}
	if r.Blocked {
		return Errf(CodeBlocked,
			"blocked proposal %d is outside the Editor snapshot until unblocked (§6)", taskID)
	}
	if r.Status != "proposed" {
		return Errf(CodeIllegalTransition,
			"only proposed rows promote (§6); task %d is %q", taskID, r.Status)
	}
	_, err = q.ExecContext(ctx, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, taskID)
	return err
}

// RejectProposal is the Editor's reject arm (ADR-001 D4): decision='rejected'
// + archive, reason mandatory. The reason prose is recorded as an activity
// row (Inv. 7) — the leverage ledger's row keeps only the mark (see the
// Phase 2 deviation note on decision-reason storage).
func RejectProposal(ctx context.Context, q Q, taskID int64, reason string) error {
	if reason == "" {
		return Errf(CodeReasonRequired, "reject requires a reason (ADR-001 D4)")
	}
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if err := requireLive(r); err != nil {
		return err
	}
	if r.Blocked {
		return Errf(CodeBlocked,
			"blocked proposal %d is outside the Editor snapshot until unblocked (§6)", taskID)
	}
	if r.Status != "proposed" {
		return Errf(CodeIllegalTransition,
			"only proposed rows are rejected (§6); task %d is %q", taskID, r.Status)
	}
	if _, err := q.ExecContext(ctx, `
		UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1
		WHERE id = ?`, taskID); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
		INSERT INTO activity (actor, kind, subject, detail)
		VALUES ('editor', 'task.rejected', ?, ?)`, taskID, reason)
	return err
}

// AdvanceStage moves a live subject along a forward pipeline edge:
// seeded → worked (the Worker terminal; for an initiative the
// done-declaration, strict-drain guarded — §6.1) or verified → packaged
// (the Packager terminal; the caller births/renders the packet in the same
// transaction). worked → verified rides ApplyVerdict, never this function.
func AdvanceStage(ctx context.Context, q Q, taskID int64, to string) error {
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if err := requireLive(r); err != nil {
		return err
	}
	switch to {
	case "worked":
		if r.Status != "seeded" {
			return Errf(CodeIllegalTransition,
				"seeded → worked only (§6); task %d is %q", taskID, r.Status)
		}
		if r.Scope == "initiative" {
			// Strict drain (§6.1), named in domain ahead of the trigger.
			var open int
			if err := q.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM tasks WHERE initiative_id = ? AND archived = 0`,
				taskID).Scan(&open); err != nil {
				return err
			}
			if open > 0 {
				return Errf(CodeStrictDrain,
					"strict drain: initiative %d has %d open children (§6.1)", taskID, open)
			}
		}
	case "packaged":
		if r.Status != "verified" {
			return Errf(CodeIllegalTransition,
				"verified → packaged only (§6); task %d is %q", taskID, r.Status)
		}
	default:
		return Errf(CodeIllegalTransition, "AdvanceStage moves to worked|packaged, not %q", to)
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE tasks SET status = ? WHERE id = ?`, to, taskID); err != nil {
		return err
	}
	if to == "packaged" {
		// The carried notes were this round's brief payload; packaging ends
		// the round (NOTE(P2.3): cleared on the next packaging).
		if _, err := q.ExecContext(ctx,
			`UPDATE tasks SET refine_notes = NULL WHERE id = ?`, taskID); err != nil {
			return err
		}
	}
	return nil
}

// VerdictArgs carries one Verifier verdict (§7 decision-outcome table).
type VerdictArgs struct {
	TaskID         int64
	RunID          string // the verdict record lands on this runs row (NOTE(P2.2))
	Outcome        string // pass | correct | budget-spent
	EvidencePath   string
	VerifiedSHA    string // stored on the verified transitions (pass, budget-spent)
	CorrectionPath string // required for correct
	Deepening      string // "" | genuine | churn — required iff the rally-ending
	// verdict lands on a subject holding an unarchived packet (A-P2-1)
}

// VerdictResult reports the applied outcome.
type VerdictResult struct {
	Status           string // resulting task status
	CorrectionCount  int
	ExceptionLabeled bool // budget-spent ships exception-labeled (§7)
}

// ApplyVerdict applies the §7 decision-outcome table:
//
//	PASS         → worked → verified; the Packager renders next
//	CORRECT      → correction file required; worked → seeded, correction_count++
//	BUDGET SPENT → correction_count = 3 required; worked → verified anyway,
//	               exception-labeled
//
// The refinement-round-trip fact is derived: the subject holds an unarchived
// packet (Inv. 11 — no carrier column). On a rally-ending verdict (pass or
// budget-spent) of a refinement round, --deepening is required and applied to
// the packet's streak (A-P2-1): genuine resets, churn increments; budget-spent
// is churn by definition (genuine rejected). A mid-rally CORRECT never applies
// deepening — the rally has not ended.
//
// Two budgets, never blurred (§10, contract §2): this function owns
// correction_count and has no access to dispatch_retries.
func ApplyVerdict(ctx context.Context, q Q, a VerdictArgs) (VerdictResult, error) {
	var res VerdictResult
	r, err := getTask(ctx, q, a.TaskID)
	if err != nil {
		return res, err
	}
	if err := requireLive(r); err != nil {
		return res, err
	}
	if r.Status != "worked" {
		return res, Errf(CodeIllegalTransition,
			"a verdict judges worked rows (§7); task %d is %q", a.TaskID, r.Status)
	}

	if a.EvidencePath == "" {
		return res, Errf(CodeEvidenceRequired,
			"a Verifier verdict requires evidence for every gate (Inv. 12, §7)")
	}
	switch a.Outcome {
	case "pass", "budget-spent":
		if a.VerifiedSHA == "" {
			return res, Errf(CodeSHARequired,
				"%s requires the exact verified commit SHA (§7 landing fence)", a.Outcome)
		}
		if a.CorrectionPath != "" {
			return res, Errf(CodeCarrierForbidden,
				"--correction belongs only to a CORRECT verdict (§7)")
		}
	case "correct":
		if a.CorrectionPath == "" {
			return res, Errf(CodeCorrectionRequired,
				"a CORRECT verdict requires --correction <path> (§7: the feedback rides the file plane)")
		}
		if a.VerifiedSHA != "" {
			return res, Errf(CodeCarrierForbidden,
				"--sha belongs only to PASS or BUDGET-SPENT; CORRECT does not verify a landing commit (§7)")
		}
	default:
		return res, Errf(CodeIllegalTransition,
			"unknown verdict outcome %q (§7: pass|correct|budget-spent)", a.Outcome)
	}
	var runRole string
	var runSubject sql.NullInt64
	if err := q.QueryRowContext(ctx,
		`SELECT role, subject FROM runs WHERE id = ?`, a.RunID,
	).Scan(&runRole, &runSubject); err != nil {
		if err == sql.ErrNoRows {
			return res, Errf(CodeNotFound, "no Verifier Run %q for verdict", a.RunID)
		}
		return res, err
	}
	if runRole != "verifier" {
		return res, Errf(CodeRoleMismatch,
			"Run %q is role %q, not verifier", a.RunID, runRole)
	}
	if !runSubject.Valid || runSubject.Int64 != a.TaskID {
		return res, Errf(CodeStaleRun,
			"Verifier Run %q is not bound to task %d (§10 fencing)", a.RunID, a.TaskID)
	}

	// The refinement-round-trip fact, derived (A-P2-1).
	var livePacket int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM review_packets WHERE task_id = ? AND archived = 0`,
		a.TaskID).Scan(&livePacket); err != nil {
		return res, err
	}
	refinementRound := livePacket > 0

	rallyEnding := a.Outcome == "pass" || a.Outcome == "budget-spent"
	if rallyEnding && refinementRound && a.Deepening == "" {
		return res, Errf(CodeDeepeningRequired,
			"task %d holds a live packet: the rally-ending verdict must declare --deepening genuine|churn (§8, A-P2-1)", a.TaskID)
	}
	if a.Deepening != "" && !(rallyEnding && refinementRound) {
		return res, Errf(CodeDeepeningForbidden,
			"--deepening is only legal on the rally-ending verdict of a refinement round-trip (§8, A-P2-1)")
	}
	if refinementRound && a.Outcome == "pass" && a.Deepening != "genuine" {
		return res, Errf(CodeDeepeningForbidden,
			"a PASS refinement is genuine by definition and requires --deepening genuine (§8, A-P2-1)")
	}
	if refinementRound && a.Outcome == "budget-spent" && a.Deepening != "churn" {
		return res, Errf(CodeDeepeningForbidden,
			"BUDGET-SPENT on a refinement is churn by definition and requires --deepening churn (§8, A-P2-1)")
	}
	if rallyEnding && refinementRound {
		if err := ApplyDeepening(ctx, q, a.TaskID, a.Deepening == "genuine"); err != nil {
			return res, err
		}
	}

	switch a.Outcome {
	case "pass":
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET status = 'verified', verified_sha = ? WHERE id = ?`,
			a.VerifiedSHA, a.TaskID); err != nil {
			return res, err
		}
		res = VerdictResult{Status: "verified", CorrectionCount: r.CorrectionCount}
	case "correct":
		if r.CorrectionCount >= 3 {
			return res, Errf(CodeBudgetExhausted,
				"correction budget spent (%d of 3): the fourth verdict must be budget-spent (§7)", r.CorrectionCount)
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET status = 'seeded', correction_count = correction_count + 1
			WHERE id = ?`, a.TaskID); err != nil {
			return res, err
		}
		res = VerdictResult{Status: "seeded", CorrectionCount: r.CorrectionCount + 1}
	case "budget-spent":
		if r.CorrectionCount != 3 {
			return res, Errf(CodeBudgetRemaining,
				"budget-spent requires correction_count = 3, task %d has %d (§7)", a.TaskID, r.CorrectionCount)
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET status = 'verified', verified_sha = ? WHERE id = ?`,
			a.VerifiedSHA, a.TaskID); err != nil {
			return res, err
		}
		res = VerdictResult{Status: "verified", CorrectionCount: r.CorrectionCount, ExceptionLabeled: true}
	}

	// The verdict record, on the Verifier's own runs row (NOTE(P2.2)).
	updated, err := q.ExecContext(ctx, `
		UPDATE runs SET verdict_outcome = ?, evidence_path = ?,
		       correction_path = ?, deepening = ?
		WHERE id = ?`,
		a.Outcome, nullIfEmpty(a.EvidencePath), nullIfEmpty(a.CorrectionPath),
		nullIfEmpty(a.Deepening), a.RunID)
	if err != nil {
		return res, err
	}
	n, err := updated.RowsAffected()
	if err != nil {
		return res, err
	}
	if n != 1 {
		return res, Errf(CodeStaleRun,
			"Verifier Run %q disappeared before its verdict carrier write", a.RunID)
	}
	return res, nil
}

// Reenter is the one re-entry transition, packaged → seeded (Inv. 11):
// operator revise, Refiner re-entry, and dispatch step 2b's initiative arm
// are the same edge. notes land in tasks.refine_notes (NOTE(P2.3)),
// overwritten per re-entry — the row carries only the *next* brief's payload.
func Reenter(ctx context.Context, q Q, taskID int64, notes string) error {
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if err := requireLive(r); err != nil {
		return err
	}
	if r.Status != "packaged" {
		return Errf(CodeNotPackaged,
			"only packaged rows re-enter (§6, §8); task %d is %q", taskID, r.Status)
	}
	if err := requireLivePacket(ctx, q, taskID); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx,
		`UPDATE tasks SET status = 'seeded', refine_notes = ? WHERE id = ?`,
		nullIfEmpty(notes), taskID)
	return err
}

// Block sets the dispatchability flag with its mandatory reason (§4, §6).
// Blocking never destroys pipeline position — status is untouched.
func Block(ctx context.Context, q Q, taskID int64, reason string) error {
	if reason == "" {
		return Errf(CodeReasonRequired, "blocking requires a reason (§4)")
	}
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if r.Archived {
		return Errf(CodeArchived, "task %d is archived (§6)", taskID)
	}
	_, err = q.ExecContext(ctx,
		`UPDATE tasks SET blocked = 1, blocked_reason = ? WHERE id = ?`, reason, taskID)
	return err
}

// Unblock clears the flag; the substrate trigger clears the stale reason
// with it (§6: unblocking resumes exactly where it stopped).
func Unblock(ctx context.Context, q Q, taskID int64) error {
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if !r.Blocked {
		return Errf(CodeNotBlocked, "task %d is not blocked", taskID)
	}
	if r.Scope == "initiative" {
		var blockedChildren int
		if err := q.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM tasks
			WHERE initiative_id = ? AND blocked = 1 AND archived = 0`, taskID,
		).Scan(&blockedChildren); err != nil {
			return err
		}
		if blockedChildren > 0 {
			return Errf(CodeBlockedChild,
				"initiative %d cannot unblock while %d live children remain blocked (§6.1)",
				taskID, blockedChildren)
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE tasks SET blocked = 0 WHERE id = ?`, taskID)
	return err
}

// Approve is the operator approve: a pure state write (§7). A branchless
// task (artifact-plane deliverable) archives synchronously; a branch-carrying
// one gains the derived landing-pending mark (NOTE(P1.9)) and archives on
// landing success, never here. Returns whether the row archived.
func Approve(ctx context.Context, q Q, taskID int64) (bool, error) {
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return false, err
	}
	if err := requireLive(r); err != nil {
		return false, err
	}
	// The substrate CHECK (approved requires packaged) backstops this;
	// surfacing it here names the rule.
	if r.Status != "packaged" {
		return false, Errf(CodeNotPackaged,
			"only packaged work can be approved (§4, §6); task %d is %q", taskID, r.Status)
	}
	if err := requireLivePacket(ctx, q, taskID); err != nil {
		return false, err
	}
	if r.Branch.Valid && r.Branch.String != "" &&
		(!r.VerifiedSHA.Valid || r.VerifiedSHA.String == "" ||
			!r.TargetRef.Valid || r.TargetRef.String == "") {
		return false, Errf(CodeLandingFence,
			"branch-carrying task %d requires verified_sha and target_ref before approval (§7 landing fence)", taskID)
	}
	// An assigned (sealed) standalone task is branchless in `tasks` by
	// construction: its branch lives in `task_assignments.branch`, because the
	// only writer of `tasks.branch` is the `--status worked --branch` terminal
	// that ADR-016 D6 closes to assigned tasks. Without this fence the
	// branchless arm below would classify it as an artifact-plane deliverable
	// and archive it — the operator approves a merge, the task disappears, and
	// main is never touched, silently. Sealed landing (ADR-017:1226-1240) is
	// not implemented, so refuse loudly and strand nothing: the seal, the
	// packet, and the task all survive for the landing slice to pick up.
	if !r.Branch.Valid || r.Branch.String == "" {
		var assigned int
		if err := q.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM task_assignments WHERE task_id = ?)`, taskID).Scan(&assigned); err != nil {
			return false, err
		}
		if assigned != 0 {
			return false, Errf(CodeLandingFence,
				"task %d carries a sealed closure assignment and cannot be approved until sealed landing exists (ADR-017:1226-1240); approving it now would archive it without ever merging", taskID)
		}
	}
	if _, err := q.ExecContext(ctx, `
		UPDATE tasks SET decision = 'approved', decided_at = datetime('now')
		WHERE id = ?`, taskID); err != nil {
		return false, err
	}
	if !r.Branch.Valid || r.Branch.String == "" {
		if _, err := q.ExecContext(ctx,
			`UPDATE tasks SET archived = 1 WHERE id = ?`, taskID); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// Cancel is the operator cancel: decision='cancelled' + archive, reason
// mandatory (§7). For an initiative the substrate cascade cancels open
// children and archives their packets (§6.1) — one implementation, in the
// lattice. The reason prose is recorded as an activity row (Inv. 7).
// Cancel writes the cancellation mark. actor is the logical originator
// (Inv. 7): "operator" for the operator's own verbs, "editor" for a wave
// send-back (ADR-020 D5), which is the first non-operator-rooted writer of
// this mark — every other path into it, including the archive cascades, is
// rooted in an operator decision.
func Cancel(ctx context.Context, q Q, taskID int64, reason string, actor string) error {
	if reason == "" {
		return Errf(CodeReasonRequired, "--reason is required for cancel (§7: asymmetric by design)")
	}
	r, err := getTask(ctx, q, taskID)
	if err != nil {
		return err
	}
	if err := requireLive(r); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `
		UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now'), archived = 1
		WHERE id = ?`, taskID); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
		INSERT INTO activity (actor, kind, subject, detail)
		VALUES (?, 'task.cancelled', ?, ?)`, actor, taskID, reason)
	return err
}

// CancelPacket is the operator Review Packet cancel arm. Generic Cancel is
// also used for seeded initiative/child teardown, so packet existence belongs
// at this narrower aggregate boundary: operator packet decisions may never
// act on work that was not made reviewable (Inv. 11/17).
func CancelPacket(ctx context.Context, q Q, taskID int64, reason string) error {
	if err := requireLivePacket(ctx, q, taskID); err != nil {
		return err
	}
	return Cancel(ctx, q, taskID, reason, "operator")
}

// ProposalArgs births one proposed row (§6: tasks are born proposed).
type ProposalArgs struct {
	Title       string
	Description string
	Scope       string // "" defaults to task
	Priority    *int   // nil defaults to the schema's P2
	Origin      string // user | autonomous
	Worksource  string
}

// requireActiveWorksource refuses filing new work into a paused or archived
// Worksource. Pause stops intake, archive is terminal, and no unpause verb
// exists (§18) — a row filed there would be invisible to every selection
// site forever, silently swallowing operator intent.
func requireActiveWorksource(ctx context.Context, q Q, id string) error {
	var status string
	err := q.QueryRowContext(ctx,
		`SELECT status FROM worksources WHERE id = ?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return Errf(CodeNotFound, "unknown worksource %q", id)
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return Errf(CodeWorksourceInactive,
			"worksource %q is %s and accepts no new work", id, status)
	}
	return nil
}

// BirthProposal files one row into the proposed pool.
func BirthProposal(ctx context.Context, q Q, a ProposalArgs) (int64, error) {
	if a.Title == "" || a.Worksource == "" {
		return 0, Errf(CodeReasonRequired, "a proposal requires title and worksource (ADR-001 D4)")
	}
	if !validDispatchScalarAdmission(a.Title) {
		return 0, Errf(CodeCarrierForbidden, "proposal title must be valid UTF-8 without controls and at most 4096 bytes (ADR-016 D2)")
	}
	if err := requireActiveWorksource(ctx, q, a.Worksource); err != nil {
		return 0, err
	}
	scope := a.Scope
	if scope == "" {
		scope = "task"
	}
	if scope != "task" && scope != "initiative" {
		return 0, Errf(CodeIllegalTransition, "scope must be task|initiative, not %q", scope)
	}
	pri := 2
	if a.Priority != nil {
		pri = *a.Priority
	}
	res, err := q.ExecContext(ctx, `
		INSERT INTO tasks (title, description, scope, priority, origin, worksource, target_ref)
		VALUES (?, ?, ?, ?, ?, ?, 'main')`,
		a.Title, nullIfEmpty(a.Description), scope, pri, a.Origin, a.Worksource)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
