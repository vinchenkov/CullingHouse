package verbs

// The attest-side authoring of the ADR-025 D3 shared-store precreate step. It
// runs host-side during the route-free InitiativeSetup attest (it never runs
// Git — ADR-016 D5 — it only reads administrative files). Unlike the task
// precreate, an initiative has no spine pin to restate: the initiative setup
// receipt IS its assignment (D3), so when it is absent fresh-vs-retry is decided
// from ON-DISK residue and the retry pins are re-derived from the landed bytes,
// re-proven only through the commit-time DeepEqual/token/recheck fences.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"mc/boundary"
)

// captureInitiativePrecreate proves the two operator-owned mode-0700 parents
// (`.mission-control/initiatives` for the store and `.mc-worktrees` for the
// shared worktree — separate host bases, ADR-025 D1), then decides fresh-vs-retry
// from the presence of the `initiative-<id>` children: both absent → fresh (cut
// at the current tip of targetRef); both present → retry (reuse the recorded cut
// read from the on-disk store's ref, never re-resolving main, with the closure
// pins re-derived from the landed bytes). A partial/mixed state refuses.
func captureInitiativePrecreate(workspaceRoot string, initiativeID int64, ownerUID int, targetRef string) (PrivateDispatchInitiativePrecreate, error) {
	if initiativeID < 1 || initiativeID > maxJavaScriptSafeInteger {
		return PrivateDispatchInitiativePrecreate{}, Domainf("initiative id %d is not a canonical positive decimal (ADR-025 D3)", initiativeID)
	}
	workspace, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	if workspace.Canonical != filepath.Clean(workspaceRoot) {
		return PrivateDispatchInitiativePrecreate{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: "repo workspace is not its exact canonical path",
		}
	}
	storeParentPath := filepath.Join(workspace.Canonical, ".mission-control", "initiatives")
	worktreeParentPath := filepath.Join(workspace.Canonical, ".mc-worktrees")
	storeParent, err := proveInitiativeParent(storeParentPath, ownerUID, "initiative store parent")
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	worktreeParent, err := proveInitiativeParent(worktreeParentPath, ownerUID, "shared worktree parent")
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}

	child := "initiative-" + strconv.FormatInt(initiativeID, 10)
	storeChild := filepath.Join(storeParentPath, child)
	worktreeChild := filepath.Join(worktreeParentPath, child)
	storePresent, err := initiativeChildPresent(storeChild)
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	worktreePresent, err := initiativeChildPresent(worktreeChild)
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	if storePresent != worktreePresent {
		return PrivateDispatchInitiativePrecreate{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "initiative store and shared worktree are in a partial setup state; refusing to guess",
		}
	}

	step := PrivateDispatchInitiativePrecreate{
		ChildMode: taskSkeletonChildMode, InitiativeID: initiativeID, WorkspaceRoot: workspace.Canonical,
		StoreParent: storeParent, WorktreeParent: worktreeParent,
	}
	if !storePresent {
		format, err := probeRepoObjectFormat(workspace.Canonical)
		if err != nil {
			return PrivateDispatchInitiativePrecreate{}, err
		}
		if targetRef == "" {
			return PrivateDispatchInitiativePrecreate{}, &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: "initiative carries no target ref; a fresh cut cannot be pinned",
			}
		}
		step.Setup = &PrivateDispatchTaskSetup{Mode: "fresh", ObjectFormat: format, TargetRef: targetRef}
		return step, nil
	}

	// Retry over on-disk residue: preserve the two exact roots and reuse the
	// recorded cut. The store root is mode 0555 (D1 task-root discipline), the
	// worktree root 0700.
	recoverStore, err := proveInitiativeRoot(storeChild, ownerUID, 0o555, "initiative store root")
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	recoverWorktree, err := proveInitiativeRoot(worktreeChild, ownerUID, 0o700, "shared worktree root")
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	setup, err := deriveInitiativeRetrySetup(storeChild, initiativeID)
	if err != nil {
		return PrivateDispatchInitiativePrecreate{}, err
	}
	step.Setup = setup
	step.RecoverStore = &recoverStore
	step.RecoverWorktree = &recoverWorktree
	return step, nil
}

// proveInitiativeParent proves a canonical, operator-owned, mode-0700 parent
// directory and returns its path-free identity.
func proveInitiativeParent(path string, ownerUID int, label string) (PrivateDispatchPathIdentity, error) {
	if err := boundary.TrustHomeDir(path, ownerUID); err != nil {
		return PrivateDispatchPathIdentity{}, err
	}
	parent, err := boundary.ResolveSource(path)
	if err != nil {
		return PrivateDispatchPathIdentity{}, err
	}
	if !parent.IsDir || parent.Canonical != path || parent.Info.Mode().Perm() != 0o700 {
		return PrivateDispatchPathIdentity{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: label + " is not the exact canonical mode-0700 directory",
		}
	}
	st, ok := parent.Info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != ownerUID {
		return PrivateDispatchPathIdentity{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: label + " is not owned by the operator",
		}
	}
	return PrivateDispatchPathIdentity{
		Canonical: parent.Canonical, Device: strconv.FormatUint(uint64(st.Dev), 10),
		Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int(st.Uid),
	}, nil
}

// initiativeChildPresent reports whether the `initiative-<id>` child exists,
// distinguishing a clean absence from an I/O error.
func initiativeChildPresent(path string) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

// proveInitiativeRoot proves a present, operator-owned, non-symlink directory at
// the exact mode, returning its identity for a retry recovery.
func proveInitiativeRoot(path string, ownerUID int, mode os.FileMode, label string) (PrivateDispatchPathIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return PrivateDispatchPathIdentity{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != mode {
		return PrivateDispatchPathIdentity{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: label + " is not the fixed non-symlink directory at its expected mode",
		}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != ownerUID {
		return PrivateDispatchPathIdentity{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: label + " is not owned by the operator",
		}
	}
	return PrivateDispatchPathIdentity{
		Canonical: path, Device: strconv.FormatUint(uint64(st.Dev), 10),
		Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int(st.Uid),
	}, nil
}

// deriveInitiativeRetrySetup re-derives the retry closure pins from the on-disk
// store, since the initiative has no spine assignment to restate (ADR-025 D3).
// It reads only administrative files (ref, config, pack) — never Git.
func deriveInitiativeRetrySetup(storeRoot string, initiativeID int64) (*PrivateDispatchTaskSetup, error) {
	gitDir := filepath.Join(storeRoot, "git")
	refBytes, err := os.ReadFile(filepath.Join(gitDir, "refs", "heads", "mc", "initiative-"+strconv.FormatInt(initiativeID, 10)))
	if err != nil {
		return nil, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "initiative retry residue has no recorded cut ref: " + err.Error()}
	}
	cutSHA := strings.TrimSpace(string(refBytes))
	format, uuid, err := parseGeneratedGitConfigIdentity(filepath.Join(gitDir, "config"))
	if err != nil {
		return nil, err
	}
	if len(cutSHA) != oidLen(format) || !assignmentHex.MatchString(cutSHA) {
		return nil, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "initiative retry residue cut ref is not a canonical object name"}
	}
	digest, err := digestLandedPack(filepath.Join(gitDir, "objects", "pack"))
	if err != nil {
		return nil, err
	}
	if len(digest) != 64 || !assignmentHex.MatchString(digest) {
		return nil, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "initiative retry residue closure digest is malformed"}
	}
	return &PrivateDispatchTaskSetup{
		Mode: "retry", ObjectFormat: format, PinnedBaseSHA: cutSHA,
		PinnedClosureDigest: digest, PinnedLocalRepoUUID: uuid,
	}, nil
}

// parseGeneratedGitConfigIdentity extracts the object format and repository UUID
// from a generatedTaskGitConfig, validating the closed grammar first so a
// tampered config cannot smuggle a foreign identity.
func parseGeneratedGitConfigIdentity(configPath string) (objectFormat, uuid string, err error) {
	body, err := os.ReadFile(configPath)
	if err != nil {
		return "", "", &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "initiative retry residue config is unreadable: " + err.Error()}
	}
	if err := validateTaskGitConfig(body); err != nil {
		return "", "", &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "initiative retry residue config is not a valid generated store config: " + err.Error()}
	}
	objectFormat = "sha1"
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(line), "objectformat") && strings.Contains(line, "sha256") {
			objectFormat = "sha256"
		}
		if strings.HasPrefix(strings.ToLower(line), "localrepouuid") {
			if i := strings.IndexByte(line, '='); i >= 0 {
				uuid = strings.TrimSpace(line[i+1:])
			}
		}
	}
	if !assignmentUUID.MatchString(uuid) {
		return "", "", &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "initiative retry residue config carries no valid MC identity"}
	}
	return objectFormat, uuid, nil
}
