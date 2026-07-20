package verbs

// The landing effect class: ADR-017:697-702's closed four-row mount table.
//
// Landing is NOT the agent table and not the setup table. ADR-017:686-687
// makes that separation load-bearing rather than cosmetic, and the sharpest
// edge is access: setup's `/repo/source` is RO (:691) while landing's is RW
// (:699) — "the only grant in the system that gets a real Worksource
// repository RW, intentionally including its primary checkout". The sealed
// task bytes stay reachable only through the RO `/repo/task` row, never
// through that RW alias, which is what the nested `.mission-control` cover
// buys (:700).
//
// This slice ships the GRAMMAR only, per the sealed-landing build order in
// PROGRESS.md: the rows and the destination predicate exist so `mountplan.go`
// stops classifying every `/repo/...` cell as a confused-planner protocol
// error. No producer resolves a landing kind to an authorized typed root yet
// (boundary/typedkind.go's four landing kinds have no jurisdiction entry), so
// every landing request still DENIES — fail-closed, and deliberately inert
// until the lane is turned on as a whole. A half-built lane is the failure
// mode this order exists to avoid.

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"mc/boundary"
)

// landingPlanRow is one host-bind row of the closed landing table: the typed
// kind it claims, its fixed container destination, its access, and its shape.
// Unlike taskPlanRow there is no task-root-relative path: landing's rows are
// anchored to two different host roots (the real Worksource and the sealed
// task root), so the producer resolves each one separately.
type landingPlanRow struct {
	Kind           boundary.TypedKind
	Dest           string
	Access         boundary.Access
	IsDir          bool
	MustBeEmptyDir bool
	// ResidentMaterialized marks a row whose host source does not exist until
	// the resident writes it, so dispatch never plans it as a bind entry — the
	// same division `/mc/setup.json` already has, where the plan carries a
	// setup INSTRUCTION and the resident writes the file and binds it. The row
	// stays in this table because the table is ADR-017's, not the planner's.
	ResidentMaterialized bool
}

// landingPlanRows returns the complete landing mount table (ADR-017:699-702).
// The order is the ADR's; planMounts re-sorts entries by destination.
func landingPlanRows() []landingPlanRow {
	return []landingPlanRow{
		{Kind: boundary.KindLandingWorksource, Dest: "/repo/source", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindLandingMissionControlCover, Dest: "/repo/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true, ResidentMaterialized: true},
		{Kind: boundary.KindLandingTaskRoot, Dest: "/repo/task", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindLandingEnvelope, Dest: "/mc/landing.json", Access: boundary.AccessRO, ResidentMaterialized: true},
	}
}

// resolveLandingRoots binds the landing kinds that HAVE a host source to their
// authorized typed roots (ADR-021 D10): the real registered Worksource root
// and the sealed task-local root of the subject task.
//
// The other two rows are GENERATED per run by the resident — an empty cover
// directory and the envelope file — so no identity exists to capture when
// attest runs, and they deliberately receive no authorized root. Requesting
// one therefore still denies, fail-closed.
//
// That split has a consequence worth stating plainly: because the cover is
// not a plan entry, the plan cannot itself prove that the sealed bytes are
// unreachable through the RW `/repo/source` alias (ADR-017:700). The plan
// authorizes the grant; the realized mount table is what establishes
// containment, which is why the RO-alias property is a Docker-lane
// obligation. The resident's obligation to place the cover belongs to the
// landing instruction (the envelope slice), not here.
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
	wsID, err := resolveLandingRow(ws.Canonical, ownerUID, false, "landing Worksource root")
	if err != nil {
		return nil, err
	}
	taskRoot := filepath.Join(ws.Canonical, ".mission-control", "tasks", "task-"+strconv.FormatInt(taskID, 10))
	// The sealed task root carries the reviewed repository. Landing reads it
	// RO, and the 0555 operator-owned shape is the same fence the agent plan
	// applies to it (ADR-017:418).
	rootID, err := resolveLandingRow(taskRoot, ownerUID, true, "landing task root")
	if err != nil {
		return nil, err
	}
	return map[boundary.TypedKind]boundary.ProtectedID{
		boundary.KindLandingWorksource: wsID,
		boundary.KindLandingTaskRoot:   rootID,
	}, nil
}

// resolveLandingRow proves one landing anchor is a real, operator-owned,
// unaliased directory at its constructed path, and (for the sealed root) that
// it carries the exact mode the sealing side left it at.
func resolveLandingRow(path string, ownerUID int, sealedMode bool, name string) (boundary.ProtectedID, error) {
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
	if sealedMode && info.Mode().Perm() != 0o555 {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  name + " is not the operator-owned mode-0555 sealed task root (ADR-017:418)",
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
