package verbs

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strconv"
	"syscall"

	"mc/boundary"
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

// FirstTaskSetupRoot is the fixed setup action's host-side input. The caller
// supplies only the registered Worksource root; the task-root spelling is
// constructed from the durable receipt's task id and then re-attested. No
// caller-provided task path can be substituted for the resident-created root.
type FirstTaskSetupRoot struct {
	Receipt   TaskSetupReceipt
	Canonical string
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
	if receipt.Root.OwnerUID != os.Getuid() {
		return TaskSetupReceipt{}, Domainf("task setup receipt root is not owned by the host operator")
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

// ReadFirstTaskSetup returns the one durable root identity a fixed setup
// action may consume. It intentionally returns no host path: the setup
// effector derives its constructed task path from the attested Worksource,
// then must prove that path still has this exact identity. The same live
// run/task/lease fence as registration prevents an abandoned Worker from
// donating a skeleton to a later claim.
func ReadFirstTaskSetup(db *sql.DB, runID string) (TaskSetupReceipt, error) {
	if runID == "" {
		return TaskSetupReceipt{}, Domainf("task setup receipt run is absent")
	}
	var out TaskSetupReceipt
	err := inTx(db, func(ctx context.Context, q Q) error {
		var role, tier string
		var subject sql.NullInt64
		var ended sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT role, tier, subject, ended_at FROM runs WHERE id=?`, runID).
			Scan(&role, &tier, &subject, &ended); err != nil {
			return Domainf("task setup receipt run is absent")
		}
		if tier != "pipeline" || role != "worker" || !subject.Valid || ended.Valid {
			return Domainf("task setup receipt does not name a live standalone Worker run")
		}
		var lockRun sql.NullString
		var lockSubject sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id=1`).Scan(&lockRun, &lockSubject); err != nil {
			return err
		}
		if !lockRun.Valid || lockRun.String != runID || !lockSubject.Valid || lockSubject.Int64 != subject.Int64 {
			return Domainf("task setup receipt lost its run/task lease fence")
		}
		out.RunID = runID
		out.TaskID = subject.Int64
		if err := q.QueryRowContext(ctx, `SELECT root_device, root_inode, root_owner_uid
			FROM task_setup_receipts WHERE run_id=? AND task_id=?`, runID, subject.Int64).
			Scan(&out.Root.Device, &out.Root.Inode, &out.Root.OwnerUID); err == sql.ErrNoRows {
			return Domainf("task setup receipt is absent")
		} else if err != nil {
			return err
		}
		return nil
	})
	return out, err
}

// AttestFirstTaskSetupRoot is the first fixed-setup action's narrow entry
// gate. It consumes the durable live run/task-fenced receipt, derives the one
// allowed task-root path under the registered Worksource, and proves that it
// is still the exact operator-owned 0555 directory the resident registered.
//
// It deliberately does not populate Git state or invent mount rows. Those
// later setup stages must build on this result after closure extraction and
// inspect; accepting a path supplied by a resident retry would reopen the
// post-claim root substitution hole the receipt closes.
func AttestFirstTaskSetupRoot(db *sql.DB, runID, workspaceRoot string) (FirstTaskSetupRoot, error) {
	receipt, err := ReadFirstTaskSetup(db, runID)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	root, identity, err := attestTaskRootByID("task setup", receipt.TaskID, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	if identity != receipt.Root {
		return FirstTaskSetupRoot{}, Domainf("task setup registered root identity changed")
	}
	return FirstTaskSetupRoot{Receipt: receipt, Canonical: root}, nil
}

// InspectFirstTaskSetup joins the two gates that must bracket first-task
// materialization. A fixed setup writer calls the receipt-attested root gate
// before it writes, then this inspection before it can expose the task rows to
// an agent plan. Keeping the result path-free outside this narrow boundary is
// deliberate: every row is reconstructed from the receipt's task id under the
// canonical registered Worksource, never accepted from a setup caller.
//
// This does not populate the Git closure. That writer is the next setup
// operation; until it exists an empty resident skeleton still refuses here.
func InspectFirstTaskSetup(db *sql.DB, runID, workspaceRoot string) (FirstTaskSetupRoot, map[boundary.TypedKind]boundary.ProtectedID, error) {
	root, err := AttestFirstTaskSetupRoot(db, runID, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	rows, err := inspectFirstTaskTable(root, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	return root, rows, nil
}

// inspectFirstTaskTable is the walk half of the joined inspection. The walked
// KindTaskRoot row must carry the durable receipt's exact device/inode/owner
// identity, not merely its constructed path: the attest gate's stat and the
// resolver's stat are separate observations, and a same-path root swapped
// between them would otherwise be returned as receipt-attested (takeover
// review of c27616e..9c5d6c3, 2026-07-17).
func inspectFirstTaskTable(root FirstTaskSetupRoot, workspaceRoot string) (map[boundary.TypedKind]boundary.ProtectedID, error) {
	rows, err := resolveTaskLocalSkeleton(workspaceRoot, root.Receipt.TaskID, root.Receipt.Root.OwnerUID)
	if err != nil {
		return nil, err
	}
	got, ok := rows[boundary.KindTaskRoot]
	if !ok || got.Canonical != root.Canonical || got.Info == nil {
		return nil, Domainf("task setup inspection did not recover its receipt-attested task root")
	}
	st, ok := got.Info.Sys().(*syscall.Stat_t)
	if !ok || strconv.FormatUint(uint64(st.Dev), 10) != root.Receipt.Root.Device ||
		strconv.FormatUint(st.Ino, 10) != root.Receipt.Root.Inode ||
		int(st.Uid) != root.Receipt.Root.OwnerUID {
		return nil, Domainf("task setup inspection root identity does not match the durable receipt")
	}
	return rows, nil
}
