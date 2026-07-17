package verbs

import (
	"context"
	"database/sql"
	"regexp"
)

// TaskSetupIdentity is the durable, path-free receipt for a resident-created
// task root.  Canonical host paths are intentionally excluded: the setup
// executor derives and re-attests them from the selected Worksource.
type TaskSetupIdentity struct {
	Device   string `json:"device"`
	Inode    string `json:"inode"`
	OwnerUID int    `json:"owner_uid"`
}

type TaskSetupReceipt struct {
	RunID  string            `json:"run_id"`
	TaskID int64             `json:"task_id"`
	Root   TaskSetupIdentity `json:"root"`
}

var decimalIdentity = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// RegisterFirstTaskSetup records the sole resident-created root that a
// first-task setup retry may consume.  The active lock/run/task/role fence
// prevents a delayed resident from attaching a skeleton to a later claim.
// Repeating precisely the same receipt is an idempotent lost-response retry;
// any identity change fails closed.
func RegisterFirstTaskSetup(db *sql.DB, receipt TaskSetupReceipt) (TaskSetupReceipt, error) {
	if receipt.RunID == "" || receipt.TaskID < 1 || !decimalIdentity.MatchString(receipt.Root.Device) ||
		!decimalIdentity.MatchString(receipt.Root.Inode) || receipt.Root.OwnerUID < 0 {
		return TaskSetupReceipt{}, Domainf("task setup receipt is malformed")
	}
	if len(receipt.Root.Device) > 20 || len(receipt.Root.Inode) > 20 {
		return TaskSetupReceipt{}, Domainf("task setup receipt identity exceeds its bound")
	}
	var out TaskSetupReceipt
	err := inTx(db, func(ctx context.Context, q Q) error {
		var role, tier string
		var subject sql.NullInt64
		var ended sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT role, tier, subject, ended_at FROM runs WHERE id=?`, receipt.RunID).
			Scan(&role, &tier, &subject, &ended); err != nil {
			return Domainf("task setup receipt run is absent")
		}
		if tier != "pipeline" || role != "worker" || !subject.Valid || subject.Int64 != receipt.TaskID || ended.Valid {
			return Domainf("task setup receipt does not name a live standalone Worker run")
		}
		var lockRun sql.NullString
		var lockSubject sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id=1`).Scan(&lockRun, &lockSubject); err != nil {
			return err
		}
		if !lockRun.Valid || lockRun.String != receipt.RunID || !lockSubject.Valid || lockSubject.Int64 != receipt.TaskID {
			return Domainf("task setup receipt lost its run/task lease fence")
		}

		var existing TaskSetupReceipt
		err := q.QueryRowContext(ctx, `SELECT run_id, task_id, root_device, root_inode, root_owner_uid
			FROM task_setup_receipts WHERE run_id=?`, receipt.RunID).
			Scan(&existing.RunID, &existing.TaskID, &existing.Root.Device, &existing.Root.Inode, &existing.Root.OwnerUID)
		if err == sql.ErrNoRows {
			if _, err := q.ExecContext(ctx, `INSERT INTO task_setup_receipts
				(run_id, task_id, root_device, root_inode, root_owner_uid) VALUES (?, ?, ?, ?, ?)`,
				receipt.RunID, receipt.TaskID, receipt.Root.Device, receipt.Root.Inode, receipt.Root.OwnerUID); err != nil {
				return err
			}
			out = receipt
			return nil
		}
		if err != nil {
			return err
		}
		if existing.TaskID != receipt.TaskID || existing.Root != receipt.Root {
			return Domainf("task setup retry returned a different registered root identity")
		}
		out = existing
		return nil
	})
	return out, err
}
