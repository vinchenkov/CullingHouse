package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"mc/domain"
)

// EditorVerdicts is mc editor decide's stdin payload (ADR-001 D4).
type EditorVerdicts struct {
	Verdicts []struct {
		Task     int64  `json:"task"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	} `json:"verdicts"`
}

func decodeStrictJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value after the batch document")
		}
		return fmt.Errorf("trailing data after the batch document: %w", err)
	}
	return nil
}

// EditorDecide applies the Editor's batch verdict pass (ADR-001 D4): parse
// fully, validate fully, commit in one transaction (constraint a). promote →
// proposed → seeded; reject → decision='rejected' + archive, reason
// mandatory. Zero-promotion batches are rejected when the ready queue is
// empty — no unarchived, unblocked, dispatchable row exists outside the
// proposed pool (spec §3's guard, mechanical).
func EditorDecide(db *sql.DB, id *RunIdentity, run string, batch io.Reader) (any, error) {
	// Exact, not base: lock.owner is flat, so run.json is the only place that
	// can stop the two Editor modes from crossing (ADR-020 D5). The pool run's
	// role string stays the plain "editor", so this costs the arm nothing.
	if err := requireExactRole(id, "editor"); err != nil {
		return nil, err
	}
	if err := requireOwnRun(id, run); err != nil {
		return nil, err
	}
	var payload EditorVerdicts
	if err := decodeStrictJSON(batch, &payload); err != nil {
		return nil, Domainf("mc editor decide: bad batch payload: %v", err)
	}
	allReject := true
	for _, v := range payload.Verdicts {
		switch v.Decision {
		case "promote":
			allReject = false
		case "reject":
			if v.Reason == "" {
				return nil, &DomainError{Code: domain.CodeReasonRequired,
					Msg: fmt.Sprintf("reject of task %d requires a reason (ADR-001 D4)", v.Task)}
			}
		default:
			return nil, Domainf("unknown decision %q (ADR-001 D4: promote|reject)", v.Decision)
		}
	}

	promoted, rejected := []int64{}, []int64{}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if _, err := fenceRun(ctx, q, run); err != nil {
			return err
		}
		// Coverage: the batch must cover exactly the run's snapshotted pool —
		// no more, no fewer (ADR-001 D4); proposals that arrived after the
		// snapshot wait for the next batch.
		var poolJSON sql.NullString
		err := q.QueryRowContext(ctx,
			`SELECT pool_snapshot FROM runs WHERE id = ?`, run).Scan(&poolJSON)
		if err != nil {
			return err
		}
		var pool []int64
		if poolJSON.Valid {
			if err := json.Unmarshal([]byte(poolJSON.String), &pool); err != nil {
				return fmt.Errorf("parse pool_snapshot for run %s: %w", run, err)
			}
		}
		got := make([]int64, 0, len(payload.Verdicts))
		for _, v := range payload.Verdicts {
			got = append(got, v.Task)
		}
		if !sameIDSet(pool, got) {
			return &DomainError{Code: domain.CodePoolMismatch,
				Msg: fmt.Sprintf("batch must cover exactly the run's snapshotted pool %v, got %v (ADR-001 D4)", pool, got)}
		}

		// The zero-promotion guard (ADR-001 D4; spec §3): an all-reject batch
		// is refused while nothing dispatchable exists outside the pool.
		if allReject && len(payload.Verdicts) > 0 {
			args := make([]any, 0, len(pool))
			ph := make([]string, 0, len(pool))
			for _, p := range pool {
				args = append(args, p)
				ph = append(ph, "?")
			}
			query := `SELECT COUNT(*) FROM tasks WHERE archived = 0 AND blocked = 0 AND stage_rank > 0`
			if len(ph) > 0 {
				query += ` AND id NOT IN (` + strings.Join(ph, ",") + `)`
			}
			var ready int
			if err := q.QueryRowContext(ctx, query, args...).Scan(&ready); err != nil {
				return err
			}
			if ready == 0 {
				return &DomainError{Code: domain.CodeZeroPromotion,
					Msg: "zero-promotion batch rejected: the ready queue would be empty (spec §3, ADR-001 D4)"}
			}
		}

		for _, v := range payload.Verdicts {
			switch v.Decision {
			case "promote":
				if err := domain.Promote(ctx, q, v.Task); err != nil {
					return err
				}
				promoted = append(promoted, v.Task)
			case "reject":
				if err := domain.RejectProposal(ctx, q, v.Task, v.Reason); err != nil {
					return err
				}
				rejected = append(rejected, v.Task)
			}
		}
		if err := endRun(ctx, q, run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, run)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"promoted": promoted, "rejected": rejected}, nil
}

func sameIDSet(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]int64(nil), a...)
	bs := append([]int64(nil), b...)
	sort.Slice(as, func(i, j int) bool { return as[i] < as[j] })
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// StrategistProposals is mc strategist propose's stdin payload (ADR-001 D4).
// An empty array is legal (contract §2): the fake Strategist's liveness
// terminal.
type StrategistProposals struct {
	Proposals []struct {
		Worksource  string `json:"worksource"`
		Scope       string `json:"scope"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    *int   `json:"priority"`
	} `json:"proposals"`
}

// StrategistPropose inserts all proposals in one transaction under the
// subjectless lease (ADR-001 D4, constraint b) and releases it — the run's
// terminal action. The insert rides the domain birth helper.
func StrategistPropose(db *sql.DB, id *RunIdentity, run string, batch io.Reader) (any, error) {
	if err := requireExactRole(id, "strategist(propose)"); err != nil {
		return nil, err
	}
	if err := requireOwnRun(id, run); err != nil {
		return nil, err
	}
	var payload StrategistProposals
	if err := decodeStrictJSON(batch, &payload); err != nil {
		return nil, Domainf("mc strategist propose: bad batch payload: %v", err)
	}
	for i, p := range payload.Proposals {
		if p.Title == "" || p.Worksource == "" {
			return nil, Domainf("proposal %d: title and worksource are required (ADR-001 D4)", i)
		}
		if p.Scope != "" && p.Scope != "task" && p.Scope != "initiative" {
			return nil, Domainf("proposal %d: scope must be task|initiative", i)
		}
	}

	ids := []int64{}
	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, run)
		if err != nil {
			return err
		}
		if subject != nil {
			return Domainf("Strategist(propose) requires a subjectless lease; run %s carries task %d (ADR-001 D4)", run, *subject)
		}
		for _, p := range payload.Proposals {
			// origin='autonomous': the schema's agent-provenance value
			// (ADR-001 D4 calls it 'agent'; the substrate CHECK pins the
			// vocabulary to user|autonomous — see deviation note D-mc-6).
			tid, err := domain.BirthProposal(ctx, q, domain.ProposalArgs{
				Title:       p.Title,
				Description: p.Description,
				Scope:       p.Scope,
				Priority:    p.Priority,
				Origin:      "autonomous",
				Worksource:  p.Worksource,
			})
			if err != nil {
				return err
			}
			ids = append(ids, tid)
		}
		if err := endRun(ctx, q, run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, run)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_ids": ids}, nil
}

// StrategistWaveChildren is mc strategist wave's stdin payload (ADR-001 D4).
type StrategistWaveChildren struct {
	Children []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    *int   `json:"priority"`
	} `json:"children"`
}

// StrategistWave is Strategist(initiative)'s wave terminal (ADR-001 D4):
// whole-wave atomic birth into a live, still-seeded initiative — the lease's
// subject. Whole wave or nothing (constraint a).
func StrategistWave(db *sql.DB, id *RunIdentity, run string, initiative int64, batch io.Reader) (any, error) {
	if err := requireExactRole(id, "strategist(initiative)"); err != nil {
		return nil, err
	}
	if err := requireOwnRun(id, run); err != nil {
		return nil, err
	}
	var payload StrategistWaveChildren
	if err := decodeStrictJSON(batch, &payload); err != nil {
		return nil, Domainf("mc strategist wave: bad batch payload: %v", err)
	}
	children := make([]domain.WaveChild, 0, len(payload.Children))
	for _, c := range payload.Children {
		children = append(children, domain.WaveChild{
			Title: c.Title, Description: c.Description, Priority: c.Priority,
		})
	}

	ids := []int64{}
	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, run)
		if err != nil {
			return err
		}
		if subject == nil || *subject != initiative {
			return Domainf("initiative %d is not the live lease's subject (§10 fencing)", initiative)
		}
		ids, err = domain.BirthWave(ctx, q, initiative, children)
		if err != nil {
			return err
		}
		if err := endRun(ctx, q, run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, run)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"initiative": initiative, "child_ids": ids}, nil
}

// VerdictArgs carries mc verifier verdict's flags (ADR-001 D4; §7).
type VerdictArgs struct {
	Task       int64
	Run        string
	Outcome    string // pass | correct | budget-spent
	Evidence   string
	SHA        string
	Correction string // required for correct
	Deepening  string // genuine | churn — the §8 refinement judgment
}

// VerifierVerdict is the Verifier terminal (ADR-001 D4): one transaction
// writes the verdict record on the run's own row (NOTE(P2.2)) and applies
// the §7 outcome table via task.ApplyVerdict — pass (worked → verified),
// correct (worked → seeded, correction_count++), budget-spent (ships
// exception-labeled). On a refinement round-trip the rally-ending verdict
// carries --deepening into the packet's streak (§8, A-P2-1).
func VerifierVerdict(db *sql.DB, id *RunIdentity, a VerdictArgs) (any, error) {
	if err := requireRole(id, "verifier"); err != nil {
		return nil, err
	}
	if err := requireOwnRun(id, a.Run); err != nil {
		return nil, err
	}
	switch a.Outcome {
	case "pass", "correct", "budget-spent":
	default:
		return nil, Usagef("mc verifier verdict requires --outcome pass|correct|budget-spent")
	}
	if a.Evidence == "" {
		return nil, Usagef("mc verifier verdict requires --evidence (Inv. 12: every gate records evidence)")
	}
	if (a.Outcome == "pass" || a.Outcome == "budget-spent") && a.SHA == "" {
		return nil, Usagef("mc verifier verdict requires --sha (§7: only the exact reviewed commit can land)")
	}
	if a.Outcome == "correct" && a.SHA != "" {
		return nil, Usagef("--sha is forbidden for CORRECT; that outcome re-enters without a verified landing commit (§7)")
	}
	if a.Outcome != "correct" && a.Correction != "" {
		return nil, Usagef("--correction is legal only with --outcome correct (§7)")
	}
	if a.Deepening != "" && a.Deepening != "genuine" && a.Deepening != "churn" {
		return nil, Usagef("--deepening must be genuine|churn (§8)")
	}

	var res domain.VerdictResult
	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, a.Run)
		if err != nil {
			return err
		}
		if subject == nil || *subject != a.Task {
			return Domainf("task %d is not the live lease's subject (§10 fencing)", a.Task)
		}
		res, err = domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
			TaskID:         a.Task,
			RunID:          a.Run,
			Outcome:        a.Outcome,
			EvidencePath:   a.Evidence,
			VerifiedSHA:    a.SHA,
			CorrectionPath: a.Correction,
			Deepening:      a.Deepening,
		})
		if err != nil {
			return err
		}
		if err := endRun(ctx, q, a.Run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, a.Run)
	})
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"task_id":          a.Task,
		"outcome":          a.Outcome,
		"status":           res.Status,
		"correction_count": res.CorrectionCount,
	}
	if res.Status == "verified" {
		out["verified_sha"] = a.SHA
	}
	if res.ExceptionLabeled {
		out["exception_labeled"] = true
	}
	return out, nil
}

// PlanReviewArgs carries mc editor plan-review's flags (ADR-020 D5).
type PlanReviewArgs struct {
	Run        string
	Initiative int64
	Verdict    string // pass | send-back
	Reason     string // required for send-back, forbidden for pass
}

// EditorPlanReview applies the Editor's holistic wave verdict (ADR-020 D5) —
// a new verb rather than an arm of `mc editor decide`, which would otherwise
// have to carry a second meaning for its exact-pool coverage rule, a
// promote|reject vocabulary that fits nothing here, a conditional
// zero-promotion guard, and a second transition table, for no gain.
//
// Flags, not a stdin batch: the verdict is one wave-level scalar, not a batch
// of per-element decisions, so ADR-001 D1's `--batch -` grammar has nothing to
// carry (mc verifier verdict is the flags-only precedent).
//
// D3-standard terminal semantics: one transaction writes the verdict, ends the
// run, and releases the lease; it never dispatches (Inv. 2, Inv. 3), and a
// second call is rejected by the released lease.
func EditorPlanReview(db *sql.DB, id *RunIdentity, a PlanReviewArgs) (any, error) {
	// Role AND mode, from run.json — an editor pool run cannot invoke this.
	if err := requireExactRole(id, "editor(plan-review)"); err != nil {
		return nil, err
	}
	if err := requireOwnRun(id, a.Run); err != nil {
		return nil, err
	}
	switch a.Verdict {
	case "pass":
		if a.Reason != "" {
			return nil, Usagef("--reason is forbidden for pass (ADR-020 D5: asymmetric by design)")
		}
	case "send-back":
		if a.Reason == "" {
			return nil, &DomainError{Code: domain.CodeReasonRequired,
				Msg: "--reason is required for send-back (ADR-020 D5: asymmetric by design)"}
		}
	default:
		return nil, Usagef("unknown verdict %q (ADR-020 D5: pass|send-back)", a.Verdict)
	}

	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, a.Run)
		if err != nil {
			return err
		}
		if subject == nil || *subject != a.Initiative {
			return Domainf("initiative %d is not the live lease's subject (§10 fencing)", a.Initiative)
		}
		snapshot, err := runPoolSnapshot(ctx, q, a.Run)
		if err != nil {
			return err
		}
		if a.Verdict == "pass" {
			err = domain.PassWaveReview(ctx, q, a.Initiative, snapshot)
		} else {
			err = domain.SendBackWave(ctx, q, a.Initiative, snapshot, a.Reason)
		}
		if err != nil {
			return err
		}
		if err := endRun(ctx, q, a.Run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, a.Run)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"initiative": a.Initiative, "verdict": a.Verdict}, nil
}

// runPoolSnapshot reads the id set this Editor run was shown and must act on
// exactly (ADR-020 D4: runs.pool_snapshot carries the proposal pool for the
// pool pass and the wave for the plan review).
func runPoolSnapshot(ctx context.Context, q Q, run string) ([]int64, error) {
	var poolJSON sql.NullString
	if err := q.QueryRowContext(ctx,
		`SELECT pool_snapshot FROM runs WHERE id = ?`, run).Scan(&poolJSON); err != nil {
		return nil, err
	}
	var pool []int64
	if poolJSON.Valid {
		if err := json.Unmarshal([]byte(poolJSON.String), &pool); err != nil {
			return nil, fmt.Errorf("parse pool_snapshot for run %s: %w", run, err)
		}
	}
	return pool, nil
}
