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
	if _, err := os.Lstat(root); err == nil {
		return nil, &DomainError{Code: boundary.CodeIdentityChanged,
			Msg: "task parent recheck: expected-absent task root appeared"}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return map[string]any{"action": "task-parent-recheck", "status": "ok"}, nil
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
