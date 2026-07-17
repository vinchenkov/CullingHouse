package verbs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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
	workspace, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	if !workspace.IsDir || workspace.Canonical != filepath.Clean(workspaceRoot) {
		return FirstTaskSetupRoot{}, Domainf("task setup Worksource root is not its exact canonical directory")
	}
	root := filepath.Join(workspace.Canonical, ".mission-control", "tasks", "task-"+strconv.FormatInt(receipt.TaskID, 10))
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return FirstTaskSetupRoot{}, Domainf("task setup registered root is absent")
	}
	if err != nil {
		return FirstTaskSetupRoot{}, Domainf("task setup registered root is unreadable: %v", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o555 {
		return FirstTaskSetupRoot{}, Domainf("task setup registered root is not the fixed non-symlink mode-0555 directory")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || strconv.FormatUint(uint64(st.Dev), 10) != receipt.Root.Device ||
		strconv.FormatUint(st.Ino, 10) != receipt.Root.Inode || int(st.Uid) != receipt.Root.OwnerUID ||
		int(st.Uid) != os.Getuid() {
		return FirstTaskSetupRoot{}, Domainf("task setup registered root identity changed")
	}
	resolved, err := boundary.ResolveSource(root)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	if resolved.Canonical != root {
		return FirstTaskSetupRoot{}, Domainf("task setup registered root does not resolve to its constructed path")
	}
	return FirstTaskSetupRoot{Receipt: receipt, Canonical: root}, nil
}
