package verbs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"mc/boundary"
	"mc/domain"
)

// AcceptedSealRebuildReceipt is the durable host-side result of rebuilding a
// canonical task store from an accepted Worker seal. It is deliberately
// path-free: the task root is re-derived from the selected Worksource on every
// host crossing and represented here only by its operator-owned identity.
type AcceptedSealRebuildReceipt struct {
	RunID               string
	TaskID              int64
	CompletionRunID     string
	CompletionRequestID string
	ObjectFormat        string
	BaseSHA             string
	ClosureDigest       string
	ManifestDigest      string
	Root                TaskSetupIdentity
	LocalRepoUUID       string
	ObjectCount         int
	FsckClean           bool
}

// AcceptedSealRebuildContinuation is the terminal, lease-releasing handoff
// after a host has durably recorded a rebuilt store. It advances no task state:
// the completed Worker already did that when its seal was accepted.
type AcceptedSealRebuildContinuation struct {
	RunID            string `json:"run_id"`
	TaskID           int64  `json:"task_id"`
	AlreadyContinued bool   `json:"already_continued"`
}

// RecordAcceptedSealRebuild is the host-side commit for D6's sealed-pack
// reconstruction. It derives the one task root from the live Verifier lease,
// re-attests that filesystem object, proves the landed store reproduces the
// setup result, and atomically binds that result to the task-pointed accepted
// Worker seal. A byte-identical lost-response retry returns the same receipt;
// any changed fact fails closed.
func RecordAcceptedSealRebuild(db *sql.DB, runID, workspaceRoot string, result SetupResult) (AcceptedSealRebuildReceipt, error) {
	if runID == "" {
		return AcceptedSealRebuildReceipt{}, Domainf("accepted-seal rebuild record run is absent")
	}
	if err := validateSetupResult(result); err != nil {
		return AcceptedSealRebuildReceipt{}, err
	}
	root, err := attestAcceptedSealRebuildRoot(db, runID, workspaceRoot)
	if err != nil {
		return AcceptedSealRebuildReceipt{}, err
	}
	if err := crossCheckLandedStore(root, result); err != nil {
		return AcceptedSealRebuildReceipt{}, err
	}

	var out AcceptedSealRebuildReceipt
	err = inTx(db, func(ctx context.Context, q Q) error {
		taskID, err := liveAcceptedSealRebuildTask(ctx, q, runID)
		if err != nil {
			return err
		}
		if taskID != root.Receipt.TaskID {
			return Domainf("accepted-seal rebuild root names a different task")
		}
		seal, err := taskPointedAcceptedSeal(ctx, q, taskID)
		if err != nil {
			return err
		}
		if result.ObjectFormat != seal.ObjectFormat || result.BaseSHA != seal.SealedSHA || result.ClosureDigest != seal.ClosureDigest {
			return Domainf("accepted-seal rebuild result does not reproduce the task-pointed accepted seal")
		}
		want := AcceptedSealRebuildReceipt{
			RunID: runID, TaskID: taskID, CompletionRunID: seal.RunID,
			CompletionRequestID: seal.CompletionRequest, ObjectFormat: result.ObjectFormat,
			BaseSHA: result.BaseSHA, ClosureDigest: result.ClosureDigest,
			ManifestDigest: seal.ManifestDigest, Root: root.Receipt.Root,
			LocalRepoUUID: result.LocalRepoUUID, ObjectCount: result.ObjectCount, FsckClean: result.FsckClean,
		}
		var existing AcceptedSealRebuildReceipt
		err = q.QueryRowContext(ctx, `SELECT run_id,task_id,completion_run_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,root_device,root_inode,root_owner_uid,local_repo_uuid,object_count,fsck_clean
			FROM accepted_seal_rebuild_receipts WHERE run_id=?`, runID).Scan(
			&existing.RunID, &existing.TaskID, &existing.CompletionRunID, &existing.CompletionRequestID,
			&existing.ObjectFormat, &existing.BaseSHA, &existing.ClosureDigest, &existing.ManifestDigest,
			&existing.Root.Device, &existing.Root.Inode, &existing.Root.OwnerUID,
			&existing.LocalRepoUUID, &existing.ObjectCount, &existing.FsckClean)
		if err == sql.ErrNoRows {
			if _, err := q.ExecContext(ctx, `INSERT INTO accepted_seal_rebuild_receipts
				(run_id,task_id,completion_run_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,root_device,root_inode,root_owner_uid,local_repo_uuid,object_count,fsck_clean)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				want.RunID, want.TaskID, want.CompletionRunID, want.CompletionRequestID,
				want.ObjectFormat, want.BaseSHA, want.ClosureDigest, want.ManifestDigest,
				want.Root.Device, want.Root.Inode, want.Root.OwnerUID, want.LocalRepoUUID,
				want.ObjectCount, want.FsckClean); err != nil {
				return err
			}
			out = want
			return nil
		}
		if err != nil {
			return err
		}
		if existing != want {
			return Domainf("accepted-seal rebuild lost-response record differs from its durable receipt")
		}
		out = existing
		return nil
	})
	return out, err
}

// ContinueAcceptedSealRebuild ends exactly the live Verifier setup run after
// its immutable receipt exists, then frees only that run's singleton lease.
// An exact response replay remains readable after the terminal transition.
func ContinueAcceptedSealRebuild(db *sql.DB, runID string) (AcceptedSealRebuildContinuation, error) {
	if runID == "" {
		return AcceptedSealRebuildContinuation{}, Domainf("accepted-seal rebuild continuation run is absent")
	}
	var out AcceptedSealRebuildContinuation
	err := inTx(db, func(ctx context.Context, q Q) error {
		var role, tier string
		var subject sql.NullInt64
		var ended, outcome sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT role,tier,subject,ended_at,outcome FROM runs WHERE id=?`, runID).
			Scan(&role, &tier, &subject, &ended, &outcome); err != nil {
			if err == sql.ErrNoRows {
				return Domainf("accepted-seal rebuild continuation run is absent")
			}
			return err
		}
		if tier != "pipeline" || role != "verifier" || !subject.Valid {
			return Domainf("accepted-seal rebuild continuation does not name a Verifier run")
		}
		out = AcceptedSealRebuildContinuation{RunID: runID, TaskID: subject.Int64}
		if ended.Valid {
			if !outcome.Valid || outcome.String != "accepted-seal-rebuilt" {
				return Domainf("accepted-seal rebuild continuation run is already terminal")
			}
			if err := requireAcceptedSealRebuildEvidence(ctx, q, runID, subject.Int64); err != nil {
				return err
			}
			out.AlreadyContinued = true
			return nil
		}
		fenced, err := domain.Fence(ctx, q, runID)
		if err != nil {
			return err
		}
		if fenced == nil || *fenced != subject.Int64 {
			return Domainf("accepted-seal rebuild continuation lost its run/task lease fence")
		}
		if err := requireAcceptedSealRebuildEvidence(ctx, q, runID, subject.Int64); err != nil {
			return err
		}
		if err := endRun(ctx, q, runID, "accepted-seal-rebuilt"); err != nil {
			return err
		}
		return domain.Release(ctx, q, runID)
	})
	return out, err
}

// attestAcceptedSealRebuildRoot is deliberately distinct from the first-task
// setup gate: the original Worker is terminal by now. The only current
// authority is the live Verifier lease, while the root identity remains the
// earlier resident registration for this task.
func attestAcceptedSealRebuildRoot(db *sql.DB, runID, workspaceRoot string) (FirstTaskSetupRoot, error) {
	var receipt TaskSetupReceipt
	err := inTx(db, func(ctx context.Context, q Q) error {
		taskID, err := liveAcceptedSealRebuildTask(ctx, q, runID)
		if err != nil {
			return err
		}
		receipt = TaskSetupReceipt{RunID: runID, TaskID: taskID}
		return nil
	})
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	workspace, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	if !workspace.IsDir || workspace.Canonical != filepath.Clean(workspaceRoot) {
		return FirstTaskSetupRoot{}, Domainf("accepted-seal rebuild Worksource root is not its exact canonical directory")
	}
	root := filepath.Join(workspace.Canonical, ".mission-control", "tasks", "task-"+strconv.FormatInt(receipt.TaskID, 10))
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return FirstTaskSetupRoot{}, Domainf("accepted-seal rebuild registered root is absent")
	}
	if err != nil {
		return FirstTaskSetupRoot{}, Domainf("accepted-seal rebuild registered root is unreadable: %v", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o555 {
		return FirstTaskSetupRoot{}, Domainf("accepted-seal rebuild registered root is not the fixed non-symlink mode-0555 directory")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return FirstTaskSetupRoot{}, Domainf("accepted-seal rebuild registered root identity changed")
	}
	receipt.Root = TaskSetupIdentity{Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int(st.Uid)}
	resolved, err := boundary.ResolveSource(root)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	if resolved.Canonical != root {
		return FirstTaskSetupRoot{}, Domainf("accepted-seal rebuild registered root does not resolve to its constructed path")
	}
	err = inTx(db, func(ctx context.Context, q Q) error {
		taskID, err := liveAcceptedSealRebuildTask(ctx, q, runID)
		if err != nil {
			return err
		}
		if taskID != receipt.TaskID {
			return Domainf("accepted-seal rebuild root names a different task")
		}
		var found int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_setup_receipts
			WHERE task_id=? AND root_device=? AND root_inode=? AND root_owner_uid=?`,
			receipt.TaskID, receipt.Root.Device, receipt.Root.Inode, receipt.Root.OwnerUID).Scan(&found); err != nil {
			return err
		}
		if found == 0 {
			return Domainf("accepted-seal rebuild root does not reproduce a registered task root receipt")
		}
		return nil
	})
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	return FirstTaskSetupRoot{Receipt: receipt, Canonical: root}, nil
}

func liveAcceptedSealRebuildTask(ctx context.Context, q Q, runID string) (int64, error) {
	var role, tier string
	var taskID sql.NullInt64
	var ended sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT role,tier,subject,ended_at FROM runs WHERE id=?`, runID).
		Scan(&role, &tier, &taskID, &ended); err != nil {
		if err == sql.ErrNoRows {
			return 0, Domainf("accepted-seal rebuild run is absent")
		}
		return 0, err
	}
	if tier != "pipeline" || role != "verifier" || !taskID.Valid || ended.Valid {
		return 0, Domainf("accepted-seal rebuild does not name a live Verifier run")
	}
	fenced, err := domain.Fence(ctx, q, runID)
	if err != nil {
		return 0, err
	}
	if fenced == nil || *fenced != taskID.Int64 {
		return 0, Domainf("accepted-seal rebuild lost its run/task lease fence")
	}
	return taskID.Int64, nil
}

func taskPointedAcceptedSeal(ctx context.Context, q Q, taskID int64) (AcceptedCompletionSeal, error) {
	var seal AcceptedCompletionSeal
	var state, tier, role, outcome string
	var ended sql.NullString
	err := q.QueryRowContext(ctx, `SELECT s.run_id,s.task_id,s.completion_request_id,s.object_format,s.sealed_sha,s.closure_digest,s.manifest_digest,s.seal_device,s.seal_inode,s.seal_owner_uid,s.state,r.tier,r.role,r.ended_at,COALESCE(r.outcome,'')
		FROM tasks t JOIN completion_seals s ON s.run_id=t.accepted_completion_run_id AND s.completion_request_id=t.accepted_completion_request_id
		JOIN runs r ON r.id=s.run_id WHERE t.id=? AND t.status='worked'`, taskID).
		Scan(&seal.RunID, &seal.TaskID, &seal.CompletionRequest, &seal.ObjectFormat, &seal.SealedSHA,
			&seal.ClosureDigest, &seal.ManifestDigest, &seal.Device, &seal.Inode, &seal.OwnerUID,
			&state, &tier, &role, &ended, &outcome)
	if err == sql.ErrNoRows {
		return AcceptedCompletionSeal{}, Domainf("accepted-seal rebuild task has no task-pointed accepted completion")
	}
	if err != nil {
		return AcceptedCompletionSeal{}, err
	}
	if state != "accepted" || tier != "pipeline" || role != "worker" || !ended.Valid || outcome != "completed" {
		return AcceptedCompletionSeal{}, Domainf("accepted-seal rebuild task completion is not an accepted completed Worker seal")
	}
	return seal, nil
}

func requireAcceptedSealRebuildEvidence(ctx context.Context, q Q, runID string, taskID int64) error {
	var receiptTask int64
	if err := q.QueryRowContext(ctx, `SELECT task_id FROM accepted_seal_rebuild_receipts WHERE run_id=?`, runID).Scan(&receiptTask); err != nil {
		if err == sql.ErrNoRows {
			return Domainf("accepted-seal rebuild continuation has no durable rebuild receipt")
		}
		return err
	}
	if receiptTask != taskID {
		return Domainf("accepted-seal rebuild continuation receipt names a different task")
	}
	return nil
}
