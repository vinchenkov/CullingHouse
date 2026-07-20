package verbs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"mc/boundary"
)

// The two setup-record crossings are split into a host half and a spine half.
//
// `mc task setup-record` and `mc task accepted-seal-record` both read HOST
// files (the Worksource task root, and the store the setup container landed in
// it) AND write the spine. On the primary Darwin target the spine is reachable
// only by self-delegating into the helper container, which carries spine and
// MC_HOME binds and no Worksource — so the single-process form resolved a host
// path from inside the helper and refused on a loop.
//
// Binding the Worksource into the helper is not the fix. Docker Desktop
// exposes namespace-local device/inode values across a bind — the same
// crossing defect the accepted-seal recheck hit (690fb08) — so an identity
// attested inside the helper could never match the resident's host
// registration. The host must remain the sole observer of these facts.
//
// So the host attests and the spine records identity, never a path: the idiom
// the rest of this boundary already uses (`mc task setup-register`,
// `mc __mount-recheck`). The task id the host derives its path from is an
// input, not an authority. The spine half refuses unless that id equals its
// live lease's task AND the attested identity reproduces the durable receipt,
// so a wrong or hostile id only ever fails closed.

// attestTaskRootByID derives and attests the one task root for taskID beneath
// workspaceRoot without consulting the spine: the exact canonical Worksource
// directory, the fixed non-symlink mode-0555 shape, operator ownership, and
// the constructed path being its own canonical resolution.
//
// It deliberately does not compare what it observes against any receipt. Only
// the spine knows which receipt is live, so that comparison belongs to the
// spine half; the composed in-process callers below re-add it immediately.
func attestTaskRootByID(subject string, taskID int64, workspaceRoot string) (string, TaskSetupIdentity, error) {
	workspace, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return "", TaskSetupIdentity{}, err
	}
	if !workspace.IsDir || workspace.Canonical != filepath.Clean(workspaceRoot) {
		return "", TaskSetupIdentity{}, Domainf("%s Worksource root is not its exact canonical directory", subject)
	}
	root := filepath.Join(workspace.Canonical, ".mission-control", "tasks", "task-"+strconv.FormatInt(taskID, 10))
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return "", TaskSetupIdentity{}, Domainf("%s registered root is absent", subject)
	}
	if err != nil {
		return "", TaskSetupIdentity{}, Domainf("%s registered root is unreadable: %v", subject, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o555 {
		return "", TaskSetupIdentity{}, Domainf("%s registered root is not the fixed non-symlink mode-0555 directory", subject)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return "", TaskSetupIdentity{}, Domainf("%s registered root identity changed", subject)
	}
	resolved, err := boundary.ResolveSource(root)
	if err != nil {
		return "", TaskSetupIdentity{}, err
	}
	if resolved.Canonical != root {
		return "", TaskSetupIdentity{}, Domainf("%s registered root does not resolve to its constructed path", subject)
	}
	return root, TaskSetupIdentity{
		Device:   strconv.FormatUint(uint64(st.Dev), 10),
		Inode:    strconv.FormatUint(st.Ino, 10),
		OwnerUID: int(st.Uid),
	}, nil
}

// HostAttestFirstTaskSetupClosure is the host half of `mc task setup-record`:
// it attests the derived task root, proves the landed store reproduces the
// container's SetupResult, and walks the 15-row typed skeleton. Every
// filesystem observation the record depends on happens here, in the one
// process that can see the Worksource with host device/inode values.
func HostAttestFirstTaskSetupClosure(taskID int64, workspaceRoot string, result SetupResult) (FirstTaskSetupRoot, map[boundary.TypedKind]boundary.ProtectedID, error) {
	if err := validateSetupResult(result); err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	canonical, identity, err := attestTaskRootByID("task setup", taskID, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	root := FirstTaskSetupRoot{
		Receipt:   TaskSetupReceipt{TaskID: taskID, Root: identity},
		Canonical: canonical,
	}
	if err := crossCheckLandedStore(root, result); err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	rows, err := inspectFirstTaskTable(root, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	return root, rows, nil
}

// HostAttestAcceptedSealRebuild is the host half of
// `mc task accepted-seal-record`. The rebuilt canonical store is cross-checked
// against the same landed-store evidence the first-task closure uses; the
// accepted seal it must reproduce is a spine fact, proven by the spine half.
func HostAttestAcceptedSealRebuild(taskID int64, workspaceRoot string, result SetupResult) (FirstTaskSetupRoot, error) {
	if err := validateSetupResult(result); err != nil {
		return FirstTaskSetupRoot{}, err
	}
	canonical, identity, err := attestTaskRootByID("accepted-seal rebuild", taskID, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, err
	}
	root := FirstTaskSetupRoot{
		Receipt:   TaskSetupReceipt{TaskID: taskID, Root: identity},
		Canonical: canonical,
	}
	if err := crossCheckLandedStore(root, result); err != nil {
		return FirstTaskSetupRoot{}, err
	}
	return root, nil
}

// RecordFirstTaskSetupClosureAttested is the spine half of
// `mc task setup-record`. It consumes only identity: the live run/task-fenced
// receipt must name exactly the attested task and carry exactly the attested
// device/inode/owner, and only then is the immutable closure assignment
// recorded. It touches no host path.
func RecordFirstTaskSetupClosureAttested(db *sql.DB, runID string, taskID int64, root TaskSetupIdentity, result SetupResult) (TaskSetupReceipt, error) {
	if err := validateSetupResult(result); err != nil {
		return TaskSetupReceipt{}, err
	}
	receipt, err := ReadFirstTaskSetup(db, runID)
	if err != nil {
		return TaskSetupReceipt{}, err
	}
	if receipt.TaskID != taskID {
		return TaskSetupReceipt{}, Domainf("task setup attested root names a different task")
	}
	if receipt.Root != root {
		return TaskSetupReceipt{}, Domainf("task setup registered root identity changed")
	}
	if _, err := RegisterFirstTaskAssignment(db, runID, FirstTaskAssignment{
		ObjectFormat:  result.ObjectFormat,
		BaseSHA:       result.BaseSHA,
		LocalRepoUUID: result.LocalRepoUUID,
		ClosureDigest: result.ClosureDigest,
	}); err != nil {
		return TaskSetupReceipt{}, err
	}
	return receipt, nil
}

// RecordAcceptedSealRebuildAttested is the spine half of
// `mc task accepted-seal-record`. The attested identity must belong to the
// live Verifier lease's task and reproduce a registered task-root receipt
// before the rebuilt store is bound to the task-pointed accepted Worker seal.
func RecordAcceptedSealRebuildAttested(db *sql.DB, runID string, taskID int64, root TaskSetupIdentity, result SetupResult) (AcceptedSealRebuildReceipt, error) {
	if runID == "" {
		return AcceptedSealRebuildReceipt{}, Domainf("accepted-seal rebuild record run is absent")
	}
	if err := validateSetupResult(result); err != nil {
		return AcceptedSealRebuildReceipt{}, err
	}
	var out AcceptedSealRebuildReceipt
	err := inTx(db, func(ctx context.Context, q Q) error {
		liveTask, err := liveAcceptedSealRebuildTask(ctx, q, runID)
		if err != nil {
			return err
		}
		if liveTask != taskID {
			return Domainf("accepted-seal rebuild root names a different task")
		}
		if err := requireRegisteredTaskRoot(ctx, q, taskID, root); err != nil {
			return err
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
			ManifestDigest: seal.ManifestDigest, Root: root,
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

// requireRegisteredTaskRoot is the spine-side half of the identity comparison
// the host can no longer make: the attested root must reproduce a task-keyed
// registration the resident made on this host.
func requireRegisteredTaskRoot(ctx context.Context, q Q, taskID int64, root TaskSetupIdentity) error {
	var found int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_setup_receipts
		WHERE task_id=? AND root_device=? AND root_inode=? AND root_owner_uid=?`,
		taskID, root.Device, root.Inode, root.OwnerUID).Scan(&found); err != nil {
		return err
	}
	if found == 0 {
		return Domainf("accepted-seal rebuild root does not reproduce a registered task root receipt")
	}
	return nil
}
