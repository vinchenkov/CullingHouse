package verbs

import (
	"context"
	"database/sql"
	"os"
)

// InitiativeSetupReceipt is the durable ADR-025 D3 record the resident registers
// after `mc __setup-initiative` materializes the shared store. Unlike a task
// setup receipt it carries TWO operator-owned roots (the sanitized store root
// and the shared worktree root — separate host bases, ADR-025 D1) and the
// recorded cut SHA, and it is keyed by the initiative (one immutable row), not
// the run: the receipt IS the initiative's assignment (there is no
// task_assignments row for an initiative — D3), so a retry reuses it rather than
// rebasing. RunID is a FENCE input (the live Worker-tier setup run keyed on the
// initiative that holds the lock), never stored.
type InitiativeSetupReceipt struct {
	RunID        string
	InitiativeID int64
	StoreRoot    TaskSetupIdentity
	WorktreeRoot TaskSetupIdentity
	CutSHA       string
}

// RegisterInitiativeSetup records the initiative's shared-store cut. The active
// lock/run/subject/role fence prevents a delayed resident from attaching a store
// to a later claim (mirroring RegisterFirstTaskSetup); repeating precisely the
// same receipt is an idempotent lost-response retry, and any change to a recorded
// root or the cut fails closed (the store is never re-cut — D3).
func RegisterInitiativeSetup(db *sql.DB, receipt InitiativeSetupReceipt) (InitiativeSetupReceipt, error) {
	if receipt.RunID == "" || receipt.InitiativeID < 1 {
		return InitiativeSetupReceipt{}, Domainf("initiative setup receipt names no run or initiative")
	}
	for _, id := range []TaskSetupIdentity{receipt.StoreRoot, receipt.WorktreeRoot} {
		if !decimalIdentity.MatchString(id.Device) || !decimalIdentity.MatchString(id.Inode) ||
			len(id.Device) > 20 || len(id.Inode) > 20 || id.OwnerUID < 0 {
			return InitiativeSetupReceipt{}, Domainf("initiative setup receipt identity is malformed")
		}
		if id.OwnerUID != os.Getuid() {
			return InitiativeSetupReceipt{}, Domainf("initiative setup receipt root is not owned by the host operator")
		}
	}
	if (len(receipt.CutSHA) != 40 && len(receipt.CutSHA) != 64) || !assignmentHex.MatchString(receipt.CutSHA) {
		return InitiativeSetupReceipt{}, Domainf("initiative setup receipt cut SHA is not a canonical object name")
	}

	var out InitiativeSetupReceipt
	err := inTx(db, func(ctx context.Context, q Q) error {
		var role, tier string
		var subject sql.NullInt64
		var ended sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT role, tier, subject, ended_at FROM runs WHERE id=?`, receipt.RunID).
			Scan(&role, &tier, &subject, &ended); err != nil {
			return Domainf("initiative setup receipt run is absent")
		}
		if tier != "pipeline" || role != "worker" || !subject.Valid || subject.Int64 != receipt.InitiativeID || ended.Valid {
			return Domainf("initiative setup receipt does not name a live Worker-tier setup run for this initiative")
		}
		var lockRun sql.NullString
		var lockSubject sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id=1`).Scan(&lockRun, &lockSubject); err != nil {
			return err
		}
		if !lockRun.Valid || lockRun.String != receipt.RunID || !lockSubject.Valid || lockSubject.Int64 != receipt.InitiativeID {
			return Domainf("initiative setup receipt lost its run/initiative lease fence")
		}

		var existing InitiativeSetupReceipt
		existing.InitiativeID = receipt.InitiativeID
		err := q.QueryRowContext(ctx, `SELECT store_device, store_inode, store_owner_uid,
			worktree_device, worktree_inode, worktree_owner_uid, cut_sha
			FROM initiative_setup_receipts WHERE initiative_id=?`, receipt.InitiativeID).
			Scan(&existing.StoreRoot.Device, &existing.StoreRoot.Inode, &existing.StoreRoot.OwnerUID,
				&existing.WorktreeRoot.Device, &existing.WorktreeRoot.Inode, &existing.WorktreeRoot.OwnerUID, &existing.CutSHA)
		if err == sql.ErrNoRows {
			if _, err := q.ExecContext(ctx, `INSERT INTO initiative_setup_receipts
				(initiative_id, store_device, store_inode, store_owner_uid,
				 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				receipt.InitiativeID, receipt.StoreRoot.Device, receipt.StoreRoot.Inode, receipt.StoreRoot.OwnerUID,
				receipt.WorktreeRoot.Device, receipt.WorktreeRoot.Inode, receipt.WorktreeRoot.OwnerUID, receipt.CutSHA); err != nil {
				return err
			}
			out = receipt
			return nil
		}
		if err != nil {
			return err
		}
		if existing.StoreRoot != receipt.StoreRoot || existing.WorktreeRoot != receipt.WorktreeRoot ||
			existing.CutSHA != receipt.CutSHA {
			return Domainf("initiative setup retry returned a different registered store identity or cut (ADR-025 D3: the store is never re-cut)")
		}
		out = existing
		out.RunID = receipt.RunID
		return nil
	})
	return out, err
}
