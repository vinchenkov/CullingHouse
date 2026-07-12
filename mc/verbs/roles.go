package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// EditorVerdicts is mc editor decide's stdin payload (ADR-001 D4).
type EditorVerdicts struct {
	Verdicts []struct {
		Task     int64  `json:"task"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	} `json:"verdicts"`
}

// EditorDecide applies the Editor's batch verdict pass (ADR-001 D4): parse
// fully, validate fully, commit in one transaction (constraint a). Skeleton:
// the promote arm (proposed → seeded) with the exact-coverage check against
// the pool snapshotted on the runs row at claim; the reject arm and the
// zero-promotion guard are [P2].
func EditorDecide(db *sql.DB, id *RunIdentity, run string, batch io.Reader) (any, error) {
	if err := requireRole(id, "editor"); err != nil {
		return nil, err
	}
	var payload EditorVerdicts
	if err := json.NewDecoder(batch).Decode(&payload); err != nil {
		return nil, Domainf("mc editor decide: bad batch payload: %v", err)
	}
	for _, v := range payload.Verdicts {
		switch v.Decision {
		case "promote":
		case "reject":
			return nil, Domainf("the reject arm is deferred to Phase 2 (contract §2 [P2])")
		default:
			return nil, Domainf("unknown decision %q (ADR-001 D4: promote|reject)", v.Decision)
		}
	}

	var promoted []int64
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
			return Domainf("batch must cover exactly the run's snapshotted pool %v, got %v (ADR-001 D4)", pool, got)
		}
		for _, v := range payload.Verdicts {
			if _, err := q.ExecContext(ctx,
				`UPDATE tasks SET status = 'seeded' WHERE id = ?`, v.Task); err != nil {
				return err
			}
			promoted = append(promoted, v.Task)
		}
		if err := endRun(ctx, q, run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, run)
	})
	if err != nil {
		return nil, err
	}
	if promoted == nil {
		promoted = []int64{}
	}
	return map[string]any{"promoted": promoted}, nil
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
// terminal action.
func StrategistPropose(db *sql.DB, id *RunIdentity, run string, batch io.Reader) (any, error) {
	if err := requireRole(id, "strategist"); err != nil {
		return nil, err
	}
	var payload StrategistProposals
	if err := json.NewDecoder(batch).Decode(&payload); err != nil {
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

	var ids []int64
	err := inTx(db, func(ctx context.Context, q Q) error {
		if _, err := fenceRun(ctx, q, run); err != nil {
			return err
		}
		for _, p := range payload.Proposals {
			scope := p.Scope
			if scope == "" {
				scope = "task"
			}
			pri := 2
			if p.Priority != nil {
				pri = *p.Priority
			}
			// origin='autonomous': the schema's agent-provenance value
			// (ADR-001 D4 calls it 'agent'; the substrate CHECK pins the
			// vocabulary to user|autonomous — see deviation note D-mc-6).
			res, err := q.ExecContext(ctx, `
				INSERT INTO tasks (title, description, scope, priority,
				                   origin, worksource, target_ref)
				VALUES (?, ?, ?, ?, 'autonomous', ?, 'main')`,
				p.Title, nullIfEmpty(p.Description), scope, pri, p.Worksource)
			if err != nil {
				return err
			}
			tid, err := res.LastInsertId()
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
	if ids == nil {
		ids = []int64{}
	}
	return map[string]any{"task_ids": ids}, nil
}

// VerifierVerdict is the Verifier terminal (ADR-001 D4). Skeleton: the pass
// arm only (worked → verified, records tasks.verified_sha — contract
// Ambiguity A2: the SHA is verification-time knowledge). correct /
// budget-spent / --deepening are [P2].
func VerifierVerdict(db *sql.DB, id *RunIdentity, task int64, run, outcome, evidence, sha string) (any, error) {
	if err := requireRole(id, "verifier"); err != nil {
		return nil, err
	}
	switch outcome {
	case "pass":
	case "correct", "budget-spent":
		return nil, Domainf("outcome %q is deferred to Phase 2 (contract §2 [P2])", outcome)
	default:
		return nil, Usagef("mc verifier verdict requires --outcome pass|correct|budget-spent")
	}
	if evidence == "" {
		return nil, Usagef("mc verifier verdict requires --evidence (Inv. 12: every gate records evidence)")
	}
	if sha == "" {
		return nil, Usagef("mc verifier verdict requires --sha (§7: only the exact reviewed commit can land)")
	}

	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, run)
		if err != nil {
			return err
		}
		if subject == nil || *subject != task {
			return Domainf("task %d is not the live lease's subject (§10 fencing)", task)
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET status = 'verified', verified_sha = ?
			WHERE id = ?`, sha, task); err != nil {
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
	return map[string]any{"task_id": task, "status": "verified", "verified_sha": sha}, nil
}
