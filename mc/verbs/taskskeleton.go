package verbs

// The typed plan class of the Git registry: ADR-017 D6's standalone-task
// repository rows, resolved as jurisdiction typed roots (ADR-021 D10) from
// the exact task-local skeleton
// `<workspace_root>/.mission-control/tasks/task-<id>/{source,git}`.
//
// This slice resolves and validates the skeleton; it never creates one.
// Creation (resident precreate) and population (setup's closure extraction,
// ADR-016 D5) are later slices, so an absent or mis-shaped skeleton refuses
// health here rather than being guessed into existence. Two content pins are
// deliberately stricter than the end state and are owed to the setup slice:
// `git/config` must be EMPTY until setup owns the sanitized-config grammar,
// and the three worktree pointers must carry exactly the fixed relative
// contents ADR-017:425-429 names. The worktree administrative name is pinned
// `mc-task-<id>` (canonical decimal), matching the repo's other managed-name
// grammars (refs/heads/mc/task-<id>, mc-<worksource>-<run_id>).

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"mc/boundary"
)

// taskPlanRow is one host-bind row of the closed D6 standalone-task table:
// the typed kind it claims, its task-root-relative host path, its fixed
// container destination, the Worker-arm access, and this slice's shape
// discipline for it.
type taskPlanRow struct {
	Kind   boundary.TypedKind
	Rel    string // task-root-relative host path; "" is the root itself
	Dest   string // fixed container destination (ADR-017 D6)
	Access boundary.Access
	IsDir  bool
	// WantBytes pins exact file content (a zero-length non-nil value pins an
	// empty file); nil leaves content to a later trust plane (seal/setup).
	WantBytes []byte
	// MustBeEmptyDir pins a generated empty directory cover.
	MustBeEmptyDir bool
	// ConfigGrammar validates a regular file against generatedTaskGitConfig's
	// closed grammar instead of pinning fixed bytes: the generated git/config
	// carries a per-task object format and repository UUID, so it has no fixed
	// content, but its key set is closed (ADR-017:463-467).
	ConfigGrammar bool
}

func taskWorktreeName(taskID int64) string {
	return "mc-task-" + strconv.FormatInt(taskID, 10)
}

// taskPlanRows returns the complete host-bind row set for one standalone
// task. The order is resolution order; planMounts re-sorts entries by
// destination.
func taskPlanRows(taskID int64) []taskPlanRow {
	wt := "git/worktrees/" + taskWorktreeName(taskID)
	empty := []byte{}
	pointer := []byte("gitdir: ../git/worktrees/" + taskWorktreeName(taskID) + "\n")
	return []taskPlanRow{
		{Kind: boundary.KindTaskRoot, Rel: "", Dest: "/workspace", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindTaskSource, Rel: "source", Dest: "/workspace/source", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindWorkspaceSourceGitCover, Rel: "source/.git", Dest: "/workspace/source/.git", Access: boundary.AccessRO, WantBytes: pointer},
		{Kind: boundary.KindWorkspaceSourceMissionControlCover, Rel: "source/.mission-control", Dest: "/workspace/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true},
		{Kind: boundary.KindTaskGit, Rel: "git", Dest: "/workspace/git", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindTaskGitConfigCover, Rel: "git/config", Dest: "/workspace/git/config", Access: boundary.AccessRO, ConfigGrammar: true},
		{Kind: boundary.KindTaskGitHooksCover, Rel: "git/hooks", Dest: "/workspace/git/hooks", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true},
		{Kind: boundary.KindTaskGitInfoCover, Rel: "git/info", Dest: "/workspace/git/info", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true},
		{Kind: boundary.KindTaskGitObjectsInfoCover, Rel: "git/objects/info", Dest: "/workspace/git/objects/info", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true},
		{Kind: boundary.KindSealedPack, Rel: "git/objects/pack", Dest: "/workspace/git/objects/pack", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindTaskGitPackedRefsCover, Rel: "git/packed-refs", Dest: "/workspace/git/packed-refs", Access: boundary.AccessRO, WantBytes: empty},
		{Kind: boundary.KindTaskGitShallowCover, Rel: "git/shallow", Dest: "/workspace/git/shallow", Access: boundary.AccessRO, WantBytes: empty},
		{Kind: boundary.KindTaskGitWorktreeCommondirCover, Rel: wt + "/commondir", Dest: "/workspace/" + wt + "/commondir", Access: boundary.AccessRO, WantBytes: []byte("../..\n")},
		{Kind: boundary.KindTaskGitWorktreeGitdirCover, Rel: wt + "/gitdir", Dest: "/workspace/" + wt + "/gitdir", Access: boundary.AccessRO, WantBytes: []byte("../../../source/.git\n")},
		{Kind: boundary.KindTaskGitWorktreeConfigCover, Rel: wt + "/config.worktree", Dest: "/workspace/" + wt + "/config.worktree", Access: boundary.AccessRO, WantBytes: empty},
	}
}

// resolveTaskLocalSkeleton resolves every D6 standalone-task row against the
// live filesystem and returns the typed-root bindings for ADR-021 D10. Every
// row must exist at its exact constructed path (no symlink components inside
// the skeleton), be operator-owned, match its declared kind, and obey this
// slice's content pins; the task root itself must be exactly mode 0555
// (ADR-017:418). Absence refuses source_missing, an aliased or
// wrong-kind row refuses source_wrong_kind, and every trust/shape failure
// refuses runtime_unappliable — all deployment authority, so a repo
// candidate without its materialized skeleton records health, never a task
// block.
func resolveTaskLocalSkeleton(workspaceRoot string, taskID int64, ownerUID int) (map[boundary.TypedKind]boundary.ProtectedID, error) {
	if taskID < 1 {
		return nil, Domainf("task id %d is not a canonical positive decimal (ADR-017 D6)", taskID)
	}
	ws, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(ws.Canonical, ".mission-control", "tasks", "task-"+strconv.FormatInt(taskID, 10))

	roots := make(map[boundary.TypedKind]boundary.ProtectedID, 15)
	for _, row := range taskPlanRows(taskID) {
		path := root
		if row.Rel != "" {
			path = filepath.Join(root, filepath.FromSlash(row.Rel))
		}
		id, err := resolveTaskPlanRow(row, path, ownerUID)
		if err != nil {
			return nil, err
		}
		roots[row.Kind] = id
	}
	return roots, nil
}

func resolveTaskPlanRow(row taskPlanRow, path string, ownerUID int) (boundary.ProtectedID, error) {
	name := "task-local row " + row.Dest
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceMissing,
			Msg:  name + " is absent; the skeleton is materialized by later setup slices, never guessed",
		}
	}
	if err != nil {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: name + " is unreadable: " + err.Error(),
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: name + " is a symlink; skeleton rows are never aliased",
		}
	}
	if info.IsDir() != row.IsDir || (!row.IsDir && !info.Mode().IsRegular()) {
		return boundary.ProtectedID{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: name + " does not match its declared kind",
		}
	}
	if err := checkTaskRowTrust(row, path, info, ownerUID, name); err != nil {
		return boundary.ProtectedID{}, err
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

func checkTaskRowTrust(row taskPlanRow, path string, info fs.FileInfo, ownerUID int, name string) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != ownerUID {
		return &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: name + " is not owned by the operator",
		}
	}
	if row.Kind == boundary.KindTaskRoot && info.Mode().Perm() != 0o555 {
		return &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: name + " is not the operator-owned mode-0555 task root (ADR-017:418)",
		}
	}
	if row.WantBytes != nil {
		if info.Size() > maxGitPointerBytes {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " exceeds its content bound",
			}
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " is unreadable: " + err.Error(),
			}
		}
		if !bytes.Equal(body, row.WantBytes) {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable,
				Msg:  name + " does not carry its fixed generated content (ADR-017:425-429; empty until the setup slice owns the grammar)",
			}
		}
	}
	if row.MustBeEmptyDir {
		entries, err := os.ReadDir(path)
		if err != nil {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " is unreadable: " + err.Error(),
			}
		}
		if len(entries) != 0 {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " must be a generated empty directory cover",
			}
		}
	}
	if row.ConfigGrammar {
		if info.Size() > maxTaskGitConfigBytes {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " exceeds its content bound",
			}
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " is unreadable: " + err.Error(),
			}
		}
		if err := validateTaskGitConfig(body); err != nil {
			return &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable, Msg: name + " is not a valid generated task config: " + err.Error(),
			}
		}
	}
	return nil
}
