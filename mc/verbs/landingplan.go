package verbs

// The landing effect class: ADR-017:697-702's closed four-row mount table.
//
// Landing is NOT the agent table and not the setup table. ADR-017:686-687
// makes that separation load-bearing rather than cosmetic, and the sharpest
// edge is access: setup's `/repo/source` is RO (:691) while landing's is RW
// (:699) — "the only grant in the system that gets a real Worksource
// repository RW, intentionally including its primary checkout".
//
// THE `/repo` PLANE IS NOT PLAN-ADDRESSABLE. The ADR-016 D5 mount plan is an
// AGENT-plane carrier: `resident/src/effects.ts:90-95` admits only
// `/workspace` destinations and refuses the whole spawn otherwise. Every
// `/repo` row of the sibling setup class is composed by the resident from
// paths it derives itself (effects.ts:598-603), and landing follows it. So
// none of the rows below is ever a `PrivateDispatchMountEntry`, and neither
// `planMounts` nor `validatePrivateMountPlan` knows this table: teaching them
// a plane the carrier structurally cannot reach buys no capability and costs a
// defence-in-depth layer, because the same predicate also gates the
// task-precreate fabrication guard and the agent/landing class separation.
// A first draft of this slice did exactly that; the review that caught it is
// in docs/ledger/phase-3.md (2026-07-20).
//
// What this file therefore is: the landing class's own destination grammar
// and host-anchor resolver, which the landing INSTRUCTION carries — the same
// division `/mc/setup.json` has had since the D5 slice, where the envelope
// pins fixed container destinations (`setupenvelope.go:100,116`) and the
// resident binds them.
//
// The lane is built INERT and turned on only at the end (PROGRESS.md): a
// half-built lane converts a loud `Approve` refusal into durable blocked rows.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"mc/boundary"
)

// landingMountRow is one row of the closed landing table: the typed kind it
// claims, its fixed container destination, its access, and its shape. Unlike
// taskPlanRow there is no task-root-relative path — landing's rows are
// anchored to two different host roots (the real Worksource and the sealed
// task root) — and "Plan" is deliberately absent from the name, because a
// plan is the one thing these rows never become.
type landingMountRow struct {
	Kind           boundary.TypedKind
	Dest           string
	Access         boundary.Access
	IsDir          bool
	MustBeEmptyDir bool
	// Generated marks a row the resident MAKES per run rather than one that
	// exists on the host beforehand — ADR-017's own word at :700 and :702.
	// A generated row has no identity for `resolveLandingRoots` to resolve;
	// the landing instruction must still name it, and the resident must still
	// place it.
	Generated bool
}

// landingMountRows returns the complete landing mount table (ADR-017:699-702).
func landingMountRows() []landingMountRow {
	return []landingMountRow{
		{Kind: boundary.KindLandingWorksource, Dest: "/repo/source", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindLandingMissionControlCover, Dest: "/repo/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true, Generated: true},
		{Kind: boundary.KindLandingTaskRoot, Dest: "/repo/task", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindLandingEnvelope, Dest: "/mc/landing.json", Access: boundary.AccessRO, Generated: true},
	}
}

// The fixed container destinations of the landing table, derived from the
// table itself so a row edit propagates to every validator rather than
// drifting away from a hand-copied string literal. The landing envelope and
// the plan's landing instruction are both validated against these.
var (
	landingSourceDest   = landingDest(boundary.KindLandingWorksource)
	landingCoverDest    = landingDest(boundary.KindLandingMissionControlCover)
	landingTaskRootDest = landingDest(boundary.KindLandingTaskRoot)
	landingEnvelopeDest = landingDest(boundary.KindLandingEnvelope)
)

// landingDest returns the fixed container destination of one landing row, or
// "" for a kind the table does not contain.
//
// The "" branch is dead in the compiled binary: the only callers are the four
// package vars above, each passing a compile-time constant that appears
// literally in landingMountRows(). A rename cannot desynchronize them — both
// references are the same identifier, so the package stops building — and a
// dropped or re-kinded row is caught by two independent guards, one of which
// reads ADR-017 itself rather than a transcription of it
// (landingplan_test.go's whole-table equality, and boundary's typedkind ADR
// parse).
//
// Recording that precisely because the earlier comment here claimed the
// callers were safe because "every caller compares it against a non-empty
// carried path". That was NOT true — the carried fields are omitempty with no
// independent non-empty check, so a "" constant would compare equal to an
// omitted field and ACCEPT, which is the fail-open direction on the one guard
// ADR-017:700 exists to enforce. The code is safe for the reason above, not
// for the reason first written down; a comment asserting the wrong invariant
// is how a fence ends up passing for the wrong reason.
func landingDest(kind boundary.TypedKind) string {
	for _, row := range landingMountRows() {
		if row.Kind == kind {
			return row.Dest
		}
	}
	return ""
}

// validLandingTargetBranch reports whether a landing target is a bare local
// branch name the lander may merge into.
//
// `tasks.target_ref` is free-form text (schema.sql:786) and the first-task
// setup arm treats it as a rev to resolve, where "HEAD" is a legitimate value.
// Landing cannot inherit that looseness: it constructs `refs/heads/<target>`
// in the REAL operator repository, so a `refs/`-prefixed value would yield
// `refs/heads/refs/heads/main`, "HEAD" is meaningless as a merge destination,
// and an option- or glob-shaped name turns a ref into an argument or a
// pattern. This restates git's own check-ref-format rules for a branch name
// rather than shelling out, because it runs at the helper boundary where no
// git process may be spawned on caller-supplied bytes.
func validLandingTargetBranch(name string) bool {
	if name == "" || len(name) > 255 || name == "HEAD" || name == "@" {
		return false
	}
	// A leading '-' makes the name an option wherever it is passed positionally.
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "refs/") {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.Contains(name, "//") || strings.HasSuffix(name, ".") ||
		strings.HasSuffix(name, ".lock") || strings.Contains(name, "..") ||
		strings.Contains(name, "@{") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
		if strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	// No path component may begin with '.' or end in '.lock'.
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// validLandingDestination reports whether one container path is a cell of the
// closed landing table. It is the grammar the landing INSTRUCTION is validated
// against — never a plan grammar; see the file comment.
func validLandingDestination(dest string) bool {
	for _, row := range landingMountRows() {
		if row.Dest == dest {
			return true
		}
	}
	return false
}

// resolveLandingRoots resolves the landing rows that HAVE a host source: the
// real registered Worksource root and the sealed task-local root of the
// subject task. The other two rows are generated per run by the resident, so
// there is nothing to resolve for them.
//
// Like resolveTaskLocalSkeleton this resolves and validates; it creates
// nothing. Absence refuses source_missing and every trust/shape failure
// refuses runtime_unappliable, so a task without its sealed root records
// health rather than blocking.
func resolveLandingRoots(workspaceRoot string, taskID int64, ownerUID int) (map[boundary.TypedKind]boundary.ProtectedID, error) {
	if taskID < 1 {
		return nil, Domainf("task id %d is not a canonical positive decimal (ADR-017 D6)", taskID)
	}
	// The RW real-repository grant is the strongest in the system, so its
	// anchor must be its own exact canonical path: an aliased Worksource would
	// place that grant somewhere the operator never registered.
	ws, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return nil, err
	}
	if !ws.IsDir || ws.Canonical != filepath.Clean(workspaceRoot) {
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "landing Worksource is not its exact canonical directory",
		}
	}
	wsID, err := resolveLandingAnchor(ws.Canonical, ownerUID, 0, "landing Worksource root")
	if err != nil {
		return nil, err
	}
	// ADR-017:699 grants RW to a "real Git Worksource root". A directory with
	// no administrative Git entry is not one, and landing into it would import
	// a closure and create a ref in a non-repository.
	if _, err := os.Lstat(filepath.Join(ws.Canonical, ".git")); err != nil {
		return nil, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "landing Worksource root carries no administrative .git entry; it is not a real Git Worksource (ADR-017:699)",
		}
	}
	// The shared blocked floor, which the agent plane already applies to every
	// typed source (mountplan.go:336,353) and the landing plane did not.
	//
	// The asymmetry was backwards: landing binds "the only grant in the system
	// that gets a real Worksource repository RW, intentionally including its
	// primary checkout" (ADR-017:699), so it had the strongest grant and the
	// weakest source check. A Worksource registered under a blocked component
	// was refused for every agent spawn and still bound RW into a landing.
	//
	// The zero-value policy is deliberate and matches mountattest.go:876 — the
	// shipped patterns are always evaluated, and a landing has no operator
	// extension of its own to honor.
	if (boundary.BlockPolicy{}).Rejects(ws.RawClean, ws.Canonical) {
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceBlocked,
			Msg:  "landing Worksource root has a blocked address component",
		}
	}
	taskRoot := filepath.Join(ws.Canonical, ".mission-control", "tasks", "task-"+strconv.FormatInt(taskID, 10))
	// The sealed task root carries the reviewed repository. Landing reads it
	// RO, and the 0555 operator-owned shape is the same fence the agent plan
	// applies to it (ADR-017:418).
	rootID, err := resolveLandingAnchor(taskRoot, ownerUID, 0o555, "landing task root")
	if err != nil {
		return nil, err
	}
	return map[boundary.TypedKind]boundary.ProtectedID{
		boundary.KindLandingWorksource: wsID,
		boundary.KindLandingTaskRoot:   rootID,
	}, nil
}

// resolveLandingAnchor proves one landing anchor is a real, operator-owned,
// unaliased directory at its constructed path. wantMode pins an exact
// permission set (the sealed root's 0555); zero means "no exact mode", which
// still refuses group/other WRITE — a real repository root is commonly 0755
// and may not be forced to 0700, but a non-owner able to plant content in the
// tree that is about to receive the system's only RW repository grant is not
// a trusted anchor.
//
// NOTE: the trust decision reads this Lstat's info while the recorded
// ProtectedID comes from a second stat inside boundary.ResolveSource. That
// split is the pattern taskskeleton.go:141-145 already uses, and exploiting it
// needs write access to an operator-owned tree; it is recorded here rather
// than fixed so the two stay consistent.
func resolveLandingAnchor(path string, ownerUID int, wantMode os.FileMode, name string) (boundary.ProtectedID, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceMissing,
			Msg:  name + " is absent; landing never guesses one into existence",
		}
	}
	if err != nil {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: name + " is unreadable: " + err.Error(),
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: name + " is a symlink; landing anchors are never aliased",
		}
	}
	if !info.IsDir() {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: name + " is not a directory",
		}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != ownerUID {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: name + " is not owned by the operator",
		}
	}
	perm := info.Mode().Perm()
	if wantMode != 0 && perm != wantMode {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  name + " is not the operator-owned mode-0555 sealed task root (ADR-017:418)",
		}
	}
	if wantMode == 0 && perm&0o022 != 0 {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  name + " is group- or world-writable; a non-owner can plant content in the RW landing anchor",
		}
	}
	id, err := boundary.ResolveSource(path)
	if err != nil {
		return boundary.ProtectedID{}, err
	}
	if id.Canonical != path {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: name + " does not resolve to its constructed path",
		}
	}
	return boundary.ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}, nil
}
