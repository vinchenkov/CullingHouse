package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// PacketDecide is the operator decision verb (§7, contract §2): exactly one
// of approve/revise/cancel, with the reason asymmetry (§7: required for
// revise/cancel, forbidden for approve). Operator verbs only write state;
// they never dispatch (Inv. 2).
//
//   - approve: a pure state write (decision='approved', decided_at); a
//     branchless task archives synchronously (§7); landing-pending is
//     derived, never a column.
//   - revise: the refinement transition packaged → seeded via task.Reenter,
//     the reason stored as the carried notes (§7; NOTE(P2.3)); the packet
//     keeps its slot (Inv. 11).
//   - cancel: decision='cancelled' + archive via task.Cancel; for an
//     initiative the substrate cascade cancels open children (§6.1).
func PacketDecide(db *sql.DB, id *RunIdentity, task int64, decision, reason string) (any, error) {
	if err := RequireOperatorVerb(id, "packet.decide"); err != nil {
		return nil, err
	}
	switch decision {
	case "approve":
		if reason != "" {
			return nil, Domainf("--reason is forbidden for approve (§7: asymmetric by design)")
		}
	case "revise", "cancel":
		if reason == "" {
			return nil, Domainf("--reason is required for %s (§7: asymmetric by design)", decision)
		}
	default:
		return nil, Usagef("mc packet decide requires exactly one of --approve, --revise, --cancel")
	}

	result := map[string]any{"task_id": task}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireOperatorVerbTx(ctx, q, id, "packet.decide"); err != nil {
			return err
		}
		switch decision {
		case "approve":
			archived, err := domain.Approve(ctx, q, task)
			if err != nil {
				return err
			}
			result["decision"] = "approved"
			result["archived"] = archived
		case "revise":
			if err := domain.Reenter(ctx, q, task, reason); err != nil {
				return err
			}
			result["decision"] = "revise"
			result["status"] = "seeded"
		case "cancel":
			if err := domain.CancelPacket(ctx, q, task, reason); err != nil {
				return err
			}
			result["decision"] = "cancelled"
			result["archived"] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PacketList returns every review_packets row as JSON (contract §2: the
// e2e's assertion channel; reads take no transaction).
func PacketList(db *sql.DB) (any, error) {
	rows, err := db.Query(`
		SELECT task_id, render_path, thesis, refine_streak, saturated,
		       archived, created_at
		FROM review_packets ORDER BY task_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out, err := rowsToMaps(rows)
	if err != nil {
		return nil, classify(err)
	}
	return map[string]any{"packets": out}, nil
}
