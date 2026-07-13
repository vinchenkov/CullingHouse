package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// CompleteArgs carries mc complete's flags (§18; phase2-contract §1.3).
// Exactly one arm is chosen per call: --status worked|packaged|seeded, or
// the flag terminals --needs-operator / --infra.
type CompleteArgs struct {
	Task    int64
	Run     string
	Status  string // worked | packaged | seeded ("" for the flag terminals)
	Branch  string // Worker: records tasks.branch (phase1b Ambiguity A2)
	Outputs string // Packager: render_path; Refiner: the deepening scope (A-P2-2)
	Reason  string // required by --needs-operator and --infra
	// NeedsOperator blocks the subject on a genuine decision point (§6: a
	// flag, not a status) and releases the lease; run outcome 'blocked'.
	NeedsOperator bool
	// Infra marks a dispatch-infrastructure failure: charges
	// dispatch_retries, never any quality budget (§10); status untouched;
	// run outcome 'infra-failed'.
	Infra bool
}

// Complete is the run's terminal action (ADR-001 D3): it advances the
// subject's status (or blocks/charges it), releases the lease, and stamps
// the runs row — one transaction (Inv. 10). It never dispatches (Inv. 3):
// no effect data; the next mc dispatch selects the follow-on stage. Fenced:
// --run must match the live lease and the lease's subject must be the task.
//
// Role map (ADR-001 D2/D4; A-P2-3): --status worked requires role worker on
// a task subject and strategist on an initiative subject (the
// done-declaration); packaged requires packager; seeded requires refiner
// (the §8 re-entry terminal). The flag terminals are role-neutral — any
// pipeline role can hit a decision point or an infra failure on its own run.
func Complete(db *sql.DB, id *RunIdentity, a CompleteArgs) (any, error) {
	arms := 0
	if a.Status != "" {
		arms++
	}
	if a.NeedsOperator {
		arms++
	}
	if a.Infra {
		arms++
	}
	if arms != 1 {
		return nil, Usagef("mc complete requires exactly one of --status, --needs-operator, --infra (§18)")
	}
	if err := requirePipeline(id); err != nil {
		return nil, err
	}
	if a.Run == "" {
		return nil, Usagef("mc complete requires --run (the fencing token, §10)")
	}
	if err := requireOwnRun(id, a.Run); err != nil {
		return nil, err
	}
	switch a.Status {
	case "", "worked", "packaged", "seeded":
	default:
		return nil, Usagef("mc complete --status must be worked|packaged|seeded (§18, phase2-contract §1.3)")
	}
	if (a.NeedsOperator || a.Infra) && a.Reason == "" {
		return nil, Usagef("mc complete --needs-operator/--infra require --reason (§18)")
	}
	if a.Status == "seeded" && a.Outputs == "" {
		return nil, Usagef("the Refiner terminal requires --outputs <deepening scope> (§8, A-P2-2)")
	}

	result := map[string]any{"task_id": a.Task}
	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, a.Run)
		if err != nil {
			return err
		}
		if subject == nil || *subject != a.Task {
			return Domainf("task %d is not the live lease's subject (§10 fencing)", a.Task)
		}

		outcome := "completed"
		switch {
		case a.Status == "worked":
			// Role by subject scope (A-P2-3): the done-declaration is
			// Strategist(initiative)'s terminal, not a Worker's.
			var scope string
			if err := q.QueryRowContext(ctx,
				`SELECT scope FROM tasks WHERE id = ?`, a.Task).Scan(&scope); err != nil {
				return err
			}
			if scope == "initiative" {
				if id.Role != "strategist(initiative)" {
					return roleMismatch(id, "strategist(initiative)")
				}
			} else if baseRole(id.Role) != "worker" {
				return roleMismatch(id, "worker")
			}
			if err := domain.AdvanceStage(ctx, q, a.Task, "worked"); err != nil {
				return err
			}
			if a.Branch != "" {
				if _, err := q.ExecContext(ctx,
					`UPDATE tasks SET branch = ? WHERE id = ?`, a.Branch, a.Task); err != nil {
					return err
				}
			}
			result["status"] = "worked"

		case a.Status == "packaged":
			if baseRole(id.Role) != "packager" {
				return roleMismatch(id, "packager")
			}
			// verified → packaged AND packet birth/re-render in the same
			// transaction (ADR-001 "not new verbs"; Inv. 10/11; A-P2-8).
			if err := domain.AdvanceStage(ctx, q, a.Task, "packaged"); err != nil {
				return err
			}
			if err := domain.Birth(ctx, q, a.Task, a.Outputs); err != nil {
				return err
			}
			result["status"] = "packaged"

		case a.Status == "seeded":
			// The Refiner terminal (A-P2-2): packaged → seeded with the
			// deepening scope carried as the re-entry notes.
			if baseRole(id.Role) != "refiner" {
				return roleMismatch(id, "refiner")
			}
			if err := domain.Reenter(ctx, q, a.Task, a.Outputs); err != nil {
				return err
			}
			result["status"] = "seeded"

		case a.NeedsOperator:
			// A genuine decision point (§6, §7): block the subject — a flag,
			// never a status; pipeline position survives.
			if err := domain.Block(ctx, q, a.Task, a.Reason); err != nil {
				return err
			}
			outcome = "blocked"
			result["blocked"] = true

		case a.Infra:
			// Two budgets, distinct (§10): --infra charges dispatch_retries
			// and never touches any quality budget; status untouched.
			charge, err := domain.ChargeInfra(ctx, q, a.Task, a.Reason)
			if err != nil {
				return err
			}
			outcome = "infra-failed"
			result["dispatch_retries"] = charge.Remaining
			result["blocked"] = charge.Blocked
		}

		if err := endRun(ctx, q, a.Run, outcome); err != nil {
			return err
		}
		return releaseLease(ctx, q, a.Run)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
