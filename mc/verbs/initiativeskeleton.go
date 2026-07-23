package verbs

// The typed plan class of an initiative's shared store: ADR-025 D2's extension
// of ADR-017 D6's closed table for a wave child under real routing. The
// destinations, kinds, access, shape discipline, and pinned pointer bytes are
// the standalone-task table's with the worktree name substituted
// (`mc-initiative-<id>`); only the host sources differ.
//
// Unlike a task, an initiative has TWO host bases (ADR-025 D1): the sanitized
// store `<workspace_root>/.mission-control/initiatives/initiative-<id>`, whose
// root binds at /workspace, and the one shared worktree
// `<workspace_root>/.mc-worktrees/initiative-<id>` (ADR-023 D5), which binds at
// /workspace/source over the store's empty structural `source` mountpoint.
// The worktree is therefore NOT a child of the bound root, which is why the
// rows carry an explicit base instead of one root plus a relative path.
//
// The pointer bytes are container-relative verbatim, so they resolve inside the
// container and not on the host. That is deliberate (ADR-025 D1): the host
// never executes Git (ADR-016 D5), and writing them verbatim keeps these rows
// byte-identical to the proven task grammar.
//
// This slice resolves and validates the skeleton; it never creates one.

import (
	"path/filepath"
	"strconv"
	"strings"

	"mc/boundary"
)

// planBase names which of an initiative's two host bases a row resolves
// against.
type planBase int

const (
	baseStoreRoot planBase = iota
	baseSharedWorktree
)

func (b planBase) String() string {
	if b == baseSharedWorktree {
		return "shared-worktree"
	}
	return "store-root"
}

// initiativePlanRow is one host-bind row of the initiative table: a standalone
// row plus the base its Rel is taken from.
type initiativePlanRow struct {
	taskPlanRow
	Base planBase
}

func initiativeWorktreeName(initiativeID int64) string {
	return "mc-initiative-" + strconv.FormatInt(initiativeID, 10)
}

// initiativePlanRows returns the complete host-bind row set for one
// initiative's shared store, in resolution order. It is derived from
// taskPlanRows so the two tables cannot drift: the destinations and pinned
// bytes are rewritten from the task worktree name to the initiative one, and
// the source family is re-based onto the shared worktree.
func initiativePlanRows(initiativeID int64) []initiativePlanRow {
	oldName := taskWorktreeName(initiativeID)
	newName := initiativeWorktreeName(initiativeID)
	rows := make([]initiativePlanRow, 0, 15)
	for _, row := range taskPlanRows(initiativeID) {
		row.Dest = strings.Replace(row.Dest, oldName, newName, 1)
		row.Rel = strings.Replace(row.Rel, oldName, newName, 1)
		if row.WantBytes != nil {
			row.WantBytes = []byte(strings.Replace(string(row.WantBytes), oldName, newName, 1))
		}
		base := baseStoreRoot
		switch row.Dest {
		case "/workspace/source", "/workspace/source/.git", "/workspace/source/.mission-control":
			// The shared worktree IS the source, so these rows drop the
			// task-era "source/" path segment and bind the worktree instead.
			base = baseSharedWorktree
			row.Rel = strings.TrimPrefix(strings.TrimPrefix(row.Rel, "source"), "/")
		}
		rows = append(rows, initiativePlanRow{taskPlanRow: row, Base: base})
	}
	return rows
}

// InitiativeStoreRoot is the canonical host path of an initiative's sanitized
// store (ADR-025 D1). It is derived, never accepted from a caller.
func InitiativeStoreRoot(workspaceRoot string, initiativeID int64) string {
	return filepath.Join(workspaceRoot, ".mission-control", "initiatives",
		"initiative-"+strconv.FormatInt(initiativeID, 10))
}

// InitiativeWorktreeRoot is the canonical host path of an initiative's one
// shared worktree (ADR-023 D5, ADR-025 D1).
func InitiativeWorktreeRoot(workspaceRoot string, initiativeID int64) string {
	return filepath.Join(workspaceRoot, ".mc-worktrees",
		"initiative-"+strconv.FormatInt(initiativeID, 10))
}

// resolveInitiativeSkeleton resolves every ADR-025 D2 row against the live
// filesystem and returns the typed-root bindings for ADR-021 D10. It applies
// exactly the standalone-task row discipline (no symlink components,
// operator-owned, declared kind, pinned content, store root exactly mode
// 0555), so absence refuses source_missing, an aliased or wrong-kind row
// refuses source_wrong_kind, and every trust/shape failure refuses
// runtime_unappliable — all deployment authority.
func resolveInitiativeSkeleton(workspaceRoot string, initiativeID int64, ownerUID int) (map[boundary.TypedKind]boundary.ProtectedID, error) {
	if initiativeID < 1 {
		return nil, Domainf("initiative id %d is not a canonical positive decimal (ADR-025 D1)", initiativeID)
	}
	ws, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return nil, err
	}
	store := InitiativeStoreRoot(ws.Canonical, initiativeID)
	worktree := InitiativeWorktreeRoot(ws.Canonical, initiativeID)

	roots := make(map[boundary.TypedKind]boundary.ProtectedID, 15)
	for _, row := range initiativePlanRows(initiativeID) {
		path := store
		if row.Base == baseSharedWorktree {
			path = worktree
		}
		if row.Rel != "" {
			path = filepath.Join(path, filepath.FromSlash(row.Rel))
		}
		id, err := resolveTaskPlanRow(row.taskPlanRow, path, ownerUID, "initiative-local")
		if err != nil {
			return nil, err
		}
		roots[row.Kind] = id
	}
	return roots, nil
}
