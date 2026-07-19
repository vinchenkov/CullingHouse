package verbs

// ADR-016 D5's launch-time identity legs: the resident re-runs authorization
// identity/trust for the committed plan immediately before Docker create and
// again after create/before start, via `mc __mount-recheck <plan-file>`. The
// verb is host-scope and read-only — it opens no spine and mutates nothing;
// its whole consequence is the exit code the resident acts on (any rejection
// removes the unstarted container). ACL-snapshot and containment rechecks at
// these legs await their own carriers and are logged residuals (2026-07-16).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
	"mc/boundary"
)

// TaskParentRecheck repeats the complete trust predicate for the exact
// precreate parent immediately before the resident creates the task root.
// Unlike an ordinary mount entry, this path is a creation authority, so its
// fixed owner-only mode and native macOS ACL predicate are both mandatory.
func TaskParentRecheck(step PrivateDispatchTaskPrecreate) (map[string]any, error) {
	plan := &PrivateDispatchMountPlan{
		Entries: []PrivateDispatchMountEntry{}, TaskPrecreate: &step, Version: 1,
	}
	if err := validatePrivateMountPlan(plan); err != nil {
		return nil, err
	}
	parent := step.TasksParent
	if err := boundary.TrustHomeDir(parent.Canonical, parent.OwnerUID); err != nil {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged,
			Msg: "task parent recheck: trust predicate changed: " + err.Error()}
	}
	identity, err := boundary.ResolveSource(parent.Canonical)
	if err != nil {
		return nil, err
	}
	st, ok := identity.Info.Sys().(*syscall.Stat_t)
	if !ok || identity.Canonical != parent.Canonical || !identity.IsDir || identity.Info.Mode().Perm() != 0o700 ||
		strconv.FormatUint(uint64(st.Dev), 10) != parent.Device ||
		strconv.FormatUint(st.Ino, 10) != parent.Inode || int(st.Uid) != parent.OwnerUID {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged,
			Msg: "task parent recheck: identity changed after preclaim"}
	}
	root := filepath.Join(parent.Canonical, "task-"+strconv.FormatInt(step.TaskID, 10))
	if step.RecoverRoot == nil {
		if _, err := os.Lstat(root); err == nil {
			return nil, &DomainError{Code: boundary.CodeIdentityChanged,
				Msg: "task parent recheck: expected-absent task root appeared"}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	} else if err := recheckRecoveryRoot(root, *step.RecoverRoot); err != nil {
		return nil, err
	}
	return map[string]any{"action": "task-parent-recheck", "status": "ok"}, nil
}

// CompletionSealRecheck repeats the parent trust/identity predicate and then
// requires the derived run child either to remain absent (before post-claim
// creation) or to be the closed private staging directory (at launch legs).
// No caller-supplied child path is accepted.
func CompletionSealRecheck(step PrivateDispatchCompletionSeal, requireRoot bool) (map[string]any, error) {
	plan := &PrivateDispatchMountPlan{CompletionSeal: &step, Entries: []PrivateDispatchMountEntry{}, Version: 1}
	if err := validatePrivateMountPlan(plan); err != nil {
		return nil, err
	}
	parent := step.SealsParent
	if err := boundary.TrustHomeDir(parent.Canonical, parent.OwnerUID); err != nil {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "completion seal parent recheck: trust predicate changed: " + err.Error()}
	}
	identity, err := boundary.ResolveSource(parent.Canonical)
	if err != nil {
		return nil, err
	}
	st, ok := identity.Info.Sys().(*syscall.Stat_t)
	if !ok || !identity.IsDir || identity.Canonical != parent.Canonical || identity.Info.Mode().Perm() != 0o700 ||
		strconv.FormatUint(uint64(st.Dev), 10) != parent.Device || strconv.FormatUint(st.Ino, 10) != parent.Inode || int(st.Uid) != parent.OwnerUID {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "completion seal parent recheck: identity changed after preclaim"}
	}
	root := filepath.Join(parent.Canonical, step.RunID)
	info, err := os.Lstat(root)
	if !requireRoot {
		if err == nil {
			return nil, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "completion seal parent recheck: expected-absent root appeared"}
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		return map[string]any{"action": "completion-seal-recheck", "status": "absent"}, nil
	}
	if err != nil {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "completion seal root is absent or unreadable: " + err.Error()}
	}
	rst, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || int(rst.Uid) != parent.OwnerUID {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "completion seal root is not the fixed private operator-owned directory"}
	}
	return map[string]any{"action": "completion-seal-recheck", "status": "ready"}, nil
}

func recheckRecoveryRoot(root string, expected PrivateDispatchPathIdentity) error {
	info, err := os.Lstat(root)
	if err != nil {
		return &DomainError{Code: boundary.CodeIdentityChanged,
			Msg: "task recovery root is absent or unreadable: " + err.Error()}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o555 ||
		strconv.FormatUint(uint64(st.Dev), 10) != expected.Device || strconv.FormatUint(st.Ino, 10) != expected.Inode ||
		int(st.Uid) != expected.OwnerUID || int(st.Uid) != os.Getuid() {
		return &DomainError{Code: boundary.CodeIdentityChanged,
			Msg: "task recovery root identity changed after preclaim"}
	}
	return nil
}

// RecoverTaskSkeleton exact-empties a receipt-vouched root's prior contents,
// then recreates only its fixed empty source/git pair without ever resolving a
// child through a mutable path. The directory descriptors keep deletion beneath
// the attested parent/root pair; a raced symlink is refused by O_NOFOLLOW and a
// raced mount/undeletable entry leaves the recovery run failed rather than
// broadening cleanup authority.
func RecoverTaskSkeleton(step PrivateDispatchTaskPrecreate) (PrivateDispatchPathIdentity, error) {
	if step.RecoverRoot == nil {
		return PrivateDispatchPathIdentity{}, Domainf("task recovery has no recovery root evidence")
	}
	if _, err := TaskParentRecheck(step); err != nil {
		return PrivateDispatchPathIdentity{}, err
	}
	parentFD, err := unix.Open(step.TasksParent.Canonical, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return PrivateDispatchPathIdentity{}, Domainf("task recovery cannot open attested parent: %v", err)
	}
	defer unix.Close(parentFD)
	var parent unix.Stat_t
	if err := unix.Fstat(parentFD, &parent); err != nil ||
		strconv.FormatUint(uint64(parent.Dev), 10) != step.TasksParent.Device ||
		strconv.FormatUint(parent.Ino, 10) != step.TasksParent.Inode || int(parent.Uid) != step.TasksParent.OwnerUID {
		return PrivateDispatchPathIdentity{}, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "task recovery parent changed while opening"}
	}
	name := "task-" + strconv.FormatInt(step.TaskID, 10)
	rootFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return PrivateDispatchPathIdentity{}, &DomainError{Code: boundary.CodeIdentityChanged, Msg: "task recovery cannot open attested root: " + err.Error()}
	}
	defer unix.Close(rootFD)
	if err := recoveryRootFDMatches(rootFD, *step.RecoverRoot); err != nil {
		return PrivateDispatchPathIdentity{}, err
	}
	if err := unix.Fchmod(rootFD, 0o700); err != nil {
		return PrivateDispatchPathIdentity{}, Domainf("task recovery cannot make root private: %v", err)
	}
	if err := clearDirectoryFD(rootFD); err != nil {
		return PrivateDispatchPathIdentity{}, Domainf("task recovery could not exact-empty root: %v", err)
	}
	// The setup mount table needs its fixed writable children, but no prior
	// bytes. Recreate only this closed pair while the attested root remains
	// private; the root inode itself is never replaced.
	for _, child := range []string{"source", "git"} {
		if err := unix.Mkdirat(rootFD, child, taskSkeletonChildMode); err != nil {
			return PrivateDispatchPathIdentity{}, Domainf("task recovery cannot recreate %s child: %v", child, err)
		}
		childFD, err := unix.Openat(rootFD, child, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return PrivateDispatchPathIdentity{}, Domainf("task recovery cannot reopen %s child: %v", child, err)
		}
		var childStat unix.Stat_t
		statErr := unix.Fstat(childFD, &childStat)
		chmodErr := unix.Fchmod(childFD, taskSkeletonChildMode)
		closeErr := unix.Close(childFD)
		if statErr != nil || childStat.Mode&unix.S_IFMT != unix.S_IFDIR || int(childStat.Uid) != os.Getuid() ||
			chmodErr != nil || closeErr != nil {
			return PrivateDispatchPathIdentity{}, Domainf("task recovery recreated %s child with invalid identity/mode", child)
		}
	}
	if err := recoveryRootFDMatches(rootFD, *step.RecoverRoot); err != nil {
		return PrivateDispatchPathIdentity{}, err
	}
	if err := unix.Fchmod(rootFD, 0o555); err != nil {
		return PrivateDispatchPathIdentity{}, Domainf("task recovery cannot restore root mode: %v", err)
	}
	return *step.RecoverRoot, nil
}

func recoveryRootFDMatches(fd int, expected PrivateDispatchPathIdentity) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR ||
		strconv.FormatUint(uint64(st.Dev), 10) != expected.Device || strconv.FormatUint(st.Ino, 10) != expected.Inode ||
		int(st.Uid) != expected.OwnerUID || int(st.Uid) != os.Getuid() {
		return &DomainError{Code: boundary.CodeIdentityChanged, Msg: "task recovery root changed while emptying"}
	}
	return nil
}

func clearDirectoryFD(fd int) error {
	dup, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dup), "task-recovery-root")
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		name := entry.Name()
		var st unix.Stat_t
		if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			childFD, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			err = clearDirectoryFD(childFD)
			closeErr := unix.Close(childFD)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if err := unix.Unlinkat(fd, name, unix.AT_REMOVEDIR); err != nil {
				return err
			}
			continue
		}
		if err := unix.Unlinkat(fd, name, 0); err != nil {
			return err
		}
	}
	return nil
}

// MountRecheck validates the plan file's own trust seam, decodes the closed
// carrier strictly, and requires every entry's source to still resolve to the
// same canonical path and (device,inode,kind,owner,mode) identity captured at
// attest. Drift is a coded domain rejection, never repaired or downgraded.
func MountRecheck(path string) (map[string]any, error) {
	if err := boundary.TrustPolicyFile(path, os.Getuid()); err != nil {
		return nil, &DomainError{Code: boundary.CodeAllowlistUntrusted,
			Msg: "mount recheck: plan file is untrusted: " + err.Error()}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &DomainError{Code: boundary.CodeAllowlistUntrusted,
			Msg: "mount recheck: plan file vanished after trust: " + err.Error()}
	}
	if len(data) > maxDispatchMountPlanBytes {
		return nil, Domainf("mount recheck: plan file exceeds its byte budget")
	}
	var plan PrivateDispatchMountPlan
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return nil, Domainf("mount recheck: plan file is not a closed mount plan: %v", err)
	}
	if dec.More() {
		return nil, Domainf("mount recheck: plan file carries trailing data")
	}
	if err := validatePrivateMountPlan(&plan); err != nil {
		return nil, err
	}
	for i, entry := range plan.Entries {
		if err := recheckMountEntry(entry); err != nil {
			return nil, &DomainError{Code: boundary.CodeIdentityChanged,
				Msg: "mount recheck: entry " + strconv.Itoa(i) + " (" + entry.LogicalID + "): " + err.Error()}
		}
	}
	return map[string]any{"action": "mount-recheck", "status": "ok", "entries": len(plan.Entries)}, nil
}

// recheckMountEntry repeats the identity half of the attest predicate for one
// carried source: same canonical resolution (a swapped symlink component
// changes it), same filesystem object, same kind, owner, and mode grant bits.
func recheckMountEntry(entry PrivateDispatchMountEntry) error {
	identity, err := boundary.ResolveSource(entry.Source)
	if err != nil {
		return err
	}
	if identity.Canonical != entry.Source {
		return Domainf("source resolves to %q, not its attested canonical path", identity.Canonical)
	}
	st, ok := identity.Info.Sys().(*syscall.Stat_t)
	if !ok {
		return Domainf("source has no native identity evidence")
	}
	kind := "file"
	if identity.IsDir {
		kind = "dir"
	}
	if strconv.FormatUint(uint64(st.Dev), 10) != entry.Device ||
		strconv.FormatUint(st.Ino, 10) != entry.Inode {
		return Domainf("source is a different filesystem object than the attested one")
	}
	if kind != entry.Kind {
		return Domainf("source kind changed from the attested %q", entry.Kind)
	}
	if int(st.Uid) != entry.OwnerUID {
		return Domainf("source owner changed from the attested uid")
	}
	if int(identity.Info.Mode().Perm()) != entry.Mode {
		return Domainf("source mode grant changed from the attested bits")
	}
	if entry.ContentSHA256 != "" {
		if identity.Info.Size() > maxGitPointerBytes {
			return Domainf("fixed control file exceeds its attested content bound")
		}
		body, err := os.ReadFile(entry.Source)
		if err != nil {
			return Domainf("fixed control file is unreadable: %v", err)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != entry.ContentSHA256 {
			return Domainf("fixed control file content changed from the attested bytes")
		}
	}
	if entry.RequireEmptyDir {
		children, err := os.ReadDir(entry.Source)
		if err != nil {
			return Domainf("generated empty control directory is unreadable: %v", err)
		}
		if len(children) != 0 {
			return Domainf("generated empty control directory gained an entry")
		}
	}
	return nil
}
