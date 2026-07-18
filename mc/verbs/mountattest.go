package verbs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"

	"mc/boundary"
	"mc/refusal"
)

// dispatchMountHostSnapshot is the host-only half of ADR-021's jurisdiction
// input. Worksource/profile text crosses in the token-bound candidate; actual
// filesystem identities and account/deployment roots never cross into the
// helper and are assembled during attest.
type dispatchMountHostSnapshot struct {
	OperatorHome        string
	OwnerUID            int
	MCHome              boundary.ProtectedID
	HomeClassRoots      []boundary.ProtectedID
	GatewaySecrets      []boundary.ProtectedID
	WorksourceRoots     map[string]boundary.WorksourceRoots
	GitControls         map[string][]boundary.ProtectedID
	MissionControlRoots map[string][]boundary.ProtectedID
	TypedRoots          map[boundary.TypedKind][]boundary.ProtectedID
	TaskPrecreate       *PrivateDispatchTaskPrecreate
	ResolveDeclared     func(string) (boundary.ProtectedID, error)
}

// taskSkeletonChildMode is the closed mode the Phase-3 final-uid VirtioFS
// canary must prove before acceptance. The immutable carrier transports the
// exact value; there is no permission-widening fallback if the Docker lane
// disproves this minimum.
const taskSkeletonChildMode = 0o700
const maxJavaScriptSafeInteger = int64(1<<53 - 1)

type dispatchMountAssembly struct {
	Requests     []mountRequest
	Jurisdiction boundary.JurisdictionInput
}

type jurisdictionDigestMember struct {
	Anchor   string   `json:"anchor"`
	Class    string   `json:"class"`
	Declared string   `json:"declared"`
	Device   string   `json:"device"`
	Inode    string   `json:"inode"`
	Kind     string   `json:"kind"`
	Present  bool     `json:"present"`
	Suffix   []string `json:"suffix"`
}

// jurisdictionInputDigest preserves ADR-021 D9/D11's distinction between
// rerunning the predicate and rerunning the input. A protected-root identity
// change must stale even when every requested source and final verdict remain
// unchanged, so the authorized plan binds this canonical non-secret snapshot
// in addition to its mount entries.
func jurisdictionInputDigest(in boundary.JurisdictionInput, ownerUID int) (string, error) {
	projection := struct {
		DeniedPaths []string                   `json:"denied_paths"`
		Home        string                     `json:"home"`
		Members     []jurisdictionDigestMember `json:"members"`
		OwnerUID    int                        `json:"owner_uid"`
	}{DeniedPaths: append([]string(nil), in.DeniedPaths...), Home: in.Home, OwnerUID: ownerUID}
	sort.Strings(projection.DeniedPaths)
	add := func(class string, id boundary.ProtectedID) error {
		effective, err := boundary.ResolveProtectedEvidence(id)
		if err != nil {
			return err
		}
		anchor := effective.Anchor
		member := jurisdictionDigestMember{
			Anchor: anchor.Canonical, Class: class, Declared: effective.Declared,
			Present: anchor.Present(), Suffix: append([]string(nil), effective.Suffix...),
		}
		if anchor.Present() {
			st, ok := anchor.Info.Sys().(*syscall.Stat_t)
			if !ok {
				return Domainf("protected root %q has no native identity evidence (ADR-021 D11)", id.Canonical)
			}
			member.Device = strconv.FormatUint(uint64(st.Dev), 10)
			member.Inode = strconv.FormatUint(st.Ino, 10)
			if anchor.IsDir {
				member.Kind = "dir"
			} else {
				member.Kind = "file"
			}
		}
		projection.Members = append(projection.Members, member)
		return nil
	}
	for i, path := range projection.DeniedPaths {
		if err := add("denied."+strconv.Itoa(i), boundary.ProtectedID{Canonical: path}); err != nil {
			var mountErr *boundary.MountError
			if errors.As(err, &mountErr) {
				candidateErr := *mountErr
				candidateErr.CandidateAuthored = true
				return "", &candidateErr
			}
			return "", err
		}
	}
	addRoots := func(prefix string, roots boundary.WorksourceRoots) error {
		for class, id := range map[string]boundary.ProtectedID{
			"workspace": roots.Workspace, "worktree": roots.Worktree,
			"state": roots.State, "cache": roots.Cache, "tool_home": roots.ToolHome,
		} {
			if err := add(prefix+"."+class, id); err != nil {
				return err
			}
		}
		for i, id := range roots.Artifacts {
			if err := add(prefix+".artifact."+strconv.Itoa(i), id); err != nil {
				return err
			}
		}
		return nil
	}
	if err := add("mc_home", in.MCHome); err != nil {
		return "", err
	}
	if err := addRoots("own", in.OwnWorksource); err != nil {
		return "", err
	}
	for i, roots := range in.OtherWorksources {
		if err := addRoots("other."+strconv.Itoa(i), roots); err != nil {
			return "", err
		}
	}
	groups := []struct {
		name string
		ids  []boundary.ProtectedID
	}{
		{"home_class", in.HomeClassRoots}, {"gateway", in.GatewaySecrets},
		{"runtime_control", in.RuntimeControls}, {"own_git", in.OwnGitControls},
		{"other_git", in.OtherGitControls}, {"own_mc", in.OwnMissionControlRoots},
		{"other_mc", in.OtherMissionControlRoots},
	}
	for _, group := range groups {
		for i, id := range group.ids {
			if err := add(group.name+"."+strconv.Itoa(i), id); err != nil {
				return "", err
			}
		}
	}
	kinds := make([]int, 0, len(in.TypedRoots))
	for kind := range in.TypedRoots {
		kinds = append(kinds, int(kind))
	}
	sort.Ints(kinds)
	for _, rawKind := range kinds {
		kind := boundary.TypedKind(rawKind)
		for i, id := range in.TypedRoots[kind] {
			if err := add("typed."+kind.String()+"."+strconv.Itoa(i), id); err != nil {
				return "", err
			}
		}
	}
	sort.Slice(projection.Members, func(i, j int) bool {
		a, b := projection.Members[i], projection.Members[j]
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		if a.Declared != b.Declared {
			return a.Declared < b.Declared
		}
		return a.Anchor < b.Anchor
	})
	body, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("MC-JURISDICTION-SNAPSHOT-V1\x00"), body...))
	return hex.EncodeToString(sum[:]), nil
}

func selectedDispatchWorksource(state PrivateDispatchMountState) (PrivateDispatchWorksource, error) {
	if state.SelectedWorksource == "" {
		return PrivateDispatchWorksource{}, Domainf("dispatch: a filesystem-bearing candidate has no selected Worksource")
	}
	for _, ws := range state.Worksources {
		if ws.WorksourceID == state.SelectedWorksource {
			return ws, nil
		}
	}
	return PrivateDispatchWorksource{}, Domainf("dispatch: selected Worksource is absent from the mount projection")
}

func selectedProfileMountRequests(state PrivateDispatchMountState) ([]mountRequest, PrivateDispatchWorksource, *refusal.Refusal, error) {
	if state.SelectedWorksource == "" {
		// Subjectless strategist/console candidates own no Worksource and carry
		// no ordinary profile mounts. Their typed run/session plane is a later
		// plan class, not authority to guess a Worksource here.
		return []mountRequest{}, PrivateDispatchWorksource{}, nil, nil
	}
	selected, err := selectedDispatchWorksource(state)
	if err != nil {
		return nil, selected, nil, err
	}
	if !selected.ProfilePresent {
		// An absent profile is deployment configuration the candidate never
		// authored: health, not a per-task confinement block (contract §1.3;
		// takeover-review reclassification, 2026-07-16).
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: "selected Worksource has no sandbox profile",
		}, refusal.AuthorityDeployment, nil)
		return nil, selected, &r, err
	}
	requests := make([]mountRequest, 0, len(selected.ArtifactRoots)+len(selected.ReadonlyMounts))
	for _, source := range selected.ArtifactRoots {
		requests = append(requests, mountRequest{Source: source, Access: boundary.AccessRW, Authority: refusal.AuthorityCandidate, Class: classArtifact})
	}
	for _, source := range selected.ReadonlyMounts {
		requests = append(requests, mountRequest{Source: source, Access: boundary.AccessRO, Authority: refusal.AuthorityCandidate, Class: classReference})
	}
	return requests, selected, nil, nil
}

// deriveDispatchMountRequests is the single derivation point for a
// candidate's mount requests: the selected profile's artifact RW and
// reference RO sources, the ADR-017 D6 task-local typed rows for a
// production standalone-task Worker on a repo Worksource, and — under
// test-fake routing only — the Phase-1 legacy workspace bind rerouted
// through the same allowlist/jurisdiction authorization as every other
// request. A repo Worksource categorically cannot use its real checkout as
// /workspace/source in production; every repo arm whose materialization does
// not exist yet (committed projections, the Verifier's disposable source,
// the sealed views Packager/Refiner read) refuses health rather than being
// guessed.
func deriveDispatchMountRequests(state PrivateDispatchMountState, role string, subjectID *int64, allowLegacyFakeWorkspace bool) ([]mountRequest, PrivateDispatchWorksource, *refusal.Refusal, error) {
	if state.SubjectInitiativeID != nil && !allowLegacyFakeWorkspace {
		// ADR-017 D6 explicitly excludes initiative children from the
		// standalone-task table while their shared-worktree representation is
		// parked. Preserve that fact in the prepared mount projection so a
		// child Worker cannot be mistaken for an ordinary task merely because
		// both carry a positive subject id.
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "initiative children have no authorized mount representation (ADR-017 D6)",
		}, refusal.AuthorityDeployment, nil)
		return nil, PrivateDispatchWorksource{}, &r, err
	}
	requests, selected, r, err := selectedProfileMountRequests(state)
	if err != nil || r != nil {
		return nil, selected, r, err
	}
	if selected.WorksourceID == "" || selected.Kind != "repo" {
		return requests, selected, nil, nil
	}
	if allowLegacyFakeWorkspace {
		if selected.WorkspaceRoot != "" {
			requests = append(requests, mountRequest{
				Source: selected.WorkspaceRoot, Access: boundary.AccessRW,
				Authority: refusal.AuthorityCandidate, Class: classWorkspaceLegacy,
			})
		}
		return requests, selected, nil, nil
	}
	sealConsumer := baseRole(role) == "verifier" && subjectID != nil && state.SubjectAcceptedCompletionSeal != nil
	if !sealConsumer && (baseRole(role) != "worker" || subjectID == nil || selected.WorkspaceRoot == "") {
		// The only realizable production repo arm today is the standalone-task
		// Worker over an existing exact skeleton. A projection-consuming or
		// seal-consuming role's mount source is materialized by later setup
		// slices; until they exist the arm is deployment health, never a
		// guessed bind and never a per-task confinement block.
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "no realizable Git mount arm for this role: task-local skeletons exist only for standalone-task Workers until the setup slices land",
		}, refusal.AuthorityDeployment, nil)
		return nil, selected, &r, err
	}
	root := filepath.Join(selected.WorkspaceRoot, ".mission-control", "tasks", "task-"+strconv.FormatInt(*subjectID, 10))
	for _, row := range taskPlanRows(*subjectID) {
		source := root
		if row.Rel != "" {
			source = filepath.Join(root, filepath.FromSlash(row.Rel))
		}
		request := mountRequest{
			Source: source, Access: row.Access, Authority: refusal.AuthorityDeployment,
			Kind: row.Kind, Destination: row.Dest,
			RequireEmptyDir: row.MustBeEmptyDir,
		}
		if sealConsumer {
			// The pre-verifier rebuild owns the same canonical store and may
			// change it only inside trusted setup. The eventual verifier gets
			// every task row RO; its disposable source is a later D6 setup arm.
			request.Access = boundary.AccessRO
		}
		if row.WantBytes != nil {
			sum := sha256.Sum256(row.WantBytes)
			request.ContentSHA256 = hex.EncodeToString(sum[:])
		} else if row.ConfigGrammar {
			// The generated git/config has no fixed bytes (per-task object
			// format + UUID), so pin the exact bytes setup landed; the resolver
			// separately proves they satisfy the closed grammar, and the recheck
			// fence re-verifies this digest before bind.
			if body, err := os.ReadFile(source); err == nil && len(body) <= maxTaskGitConfigBytes {
				sum := sha256.Sum256(body)
				request.ContentSHA256 = hex.EncodeToString(sum[:])
			}
		}
		requests = append(requests, request)
	}
	return requests, selected, nil, nil
}

// assembleDispatchMountInputs associates the frozen all-Worksource projection
// with independently resolved host identities. Request derivation (including
// the repo gate and the test-fake workspace exception) lives in
// deriveDispatchMountRequests; this function owns only the ADR-021
// jurisdiction assembly for an already-derived request set.
func assembleDispatchMountInputs(snapshot dispatchMountHostSnapshot, state PrivateDispatchMountState, requests []mountRequest, selected PrivateDispatchWorksource) (dispatchMountAssembly, error) {
	if selected.WorksourceID == "" {
		return dispatchMountAssembly{Requests: requests}, nil
	}
	if snapshot.ResolveDeclared == nil {
		return dispatchMountAssembly{}, Domainf("dispatch: host mount snapshot has no protected-path resolver")
	}
	own, ok := snapshot.WorksourceRoots[selected.WorksourceID]
	if !ok {
		return dispatchMountAssembly{}, Domainf("dispatch: host snapshot omitted selected Worksource roots")
	}

	in := boundary.JurisdictionInput{
		DeniedPaths:              append([]string(nil), selected.DeniedPaths...),
		Home:                     snapshot.OperatorHome,
		MCHome:                   snapshot.MCHome,
		HomeClassRoots:           append([]boundary.ProtectedID(nil), snapshot.HomeClassRoots...),
		GatewaySecrets:           append([]boundary.ProtectedID(nil), snapshot.GatewaySecrets...),
		RuntimeControls:          []boundary.ProtectedID{},
		OwnWorksource:            own,
		OtherWorksources:         []boundary.WorksourceRoots{},
		OwnGitControls:           append([]boundary.ProtectedID(nil), snapshot.GitControls[selected.WorksourceID]...),
		OtherGitControls:         []boundary.ProtectedID{},
		OwnMissionControlRoots:   append([]boundary.ProtectedID(nil), snapshot.MissionControlRoots[selected.WorksourceID]...),
		OtherMissionControlRoots: []boundary.ProtectedID{},
		TypedRoots:               snapshot.TypedRoots,
	}
	for _, ws := range state.Worksources {
		roots, ok := snapshot.WorksourceRoots[ws.WorksourceID]
		if !ok {
			return dispatchMountAssembly{}, Domainf("dispatch: host snapshot omitted Worksource %q roots", ws.WorksourceID)
		}
		if ws.RuntimeControlDir != "" {
			id, err := snapshot.ResolveDeclared(ws.RuntimeControlDir)
			if err != nil {
				return dispatchMountAssembly{}, err
			}
			in.RuntimeControls = append(in.RuntimeControls, id)
		}
		if ws.WorksourceID == selected.WorksourceID {
			continue
		}
		in.OtherWorksources = append(in.OtherWorksources, roots)
		in.OtherGitControls = append(in.OtherGitControls, snapshot.GitControls[ws.WorksourceID]...)
		in.OtherMissionControlRoots = append(in.OtherMissionControlRoots, snapshot.MissionControlRoots[ws.WorksourceID]...)
	}
	return dispatchMountAssembly{Requests: requests, Jurisdiction: in}, nil
}

func resolveDispatchProtected(path string, absentAllowed bool) (boundary.ProtectedID, error) {
	if path == "" {
		return boundary.ProtectedID{}, nil
	}
	id, err := boundary.ResolveSource(path)
	if err == nil {
		return boundary.ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}, nil
	}
	var me *boundary.MountError
	if absentAllowed && errors.As(err, &me) && me.Code == boundary.CodeSourceMissing && filepath.IsAbs(path) {
		return boundary.ProtectedID{Canonical: filepath.Clean(path)}, nil
	}
	return boundary.ProtectedID{}, err
}

func realOperatorHome(uid int) (string, error) {
	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	if account.HomeDir == "" || !filepath.IsAbs(account.HomeDir) {
		return "", Domainf("dispatch: operator account database returned no absolute home")
	}
	return filepath.Clean(account.HomeDir), nil
}

// resolveGatewaySecretRoots is authoritative even though the answer is empty:
// ADR-018 D5 keeps the CA private key and injection table in resident memory,
// and D6 streams one-use launch credentials without a credential file. No
// disk root exists to register; inventing one would be false evidence.
func resolveGatewaySecretRoots() []boundary.ProtectedID { return []boundary.ProtectedID{} }

func captureDispatchMountHostSnapshot(home string, state PrivateDispatchMountState, subjectTaskID *int64, allowLegacyFakeWorkspace bool) (dispatchMountHostSnapshot, error) {
	uid := os.Getuid()
	if err := boundary.TrustHomeDir(home, uid); err != nil {
		return dispatchMountHostSnapshot{}, err
	}
	operatorHome, err := realOperatorHome(uid)
	if err != nil {
		return dispatchMountHostSnapshot{}, err
	}
	mcHome, err := resolveDispatchProtected(home, false)
	if err != nil {
		return dispatchMountHostSnapshot{}, err
	}
	snapshot := dispatchMountHostSnapshot{
		OperatorHome: operatorHome, OwnerUID: uid, MCHome: mcHome,
		HomeClassRoots: []boundary.ProtectedID{}, GatewaySecrets: resolveGatewaySecretRoots(),
		WorksourceRoots: map[string]boundary.WorksourceRoots{},
		GitControls:     map[string][]boundary.ProtectedID{}, MissionControlRoots: map[string][]boundary.ProtectedID{},
		TypedRoots: map[boundary.TypedKind][]boundary.ProtectedID{},
		ResolveDeclared: func(path string) (boundary.ProtectedID, error) {
			return resolveDispatchProtected(path, true)
		},
	}
	for _, rel := range []string{
		".ssh", ".aws", ".azure", ".config", ".docker", ".gnupg", ".kube", ".codex", ".claude",
		"Library/Keychains", ".netrc", ".npmrc", ".pypirc", ".git-credentials",
	} {
		path := filepath.Join(operatorHome, filepath.FromSlash(rel))
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return dispatchMountHostSnapshot{}, err
		}
		id, err := resolveDispatchProtected(path, false)
		if err != nil {
			return dispatchMountHostSnapshot{}, err
		}
		snapshot.HomeClassRoots = append(snapshot.HomeClassRoots, id)
	}
	for _, ws := range state.Worksources {
		workspace, err := resolveDispatchProtected(ws.WorkspaceRoot, true)
		if err != nil {
			return dispatchMountHostSnapshot{}, err
		}
		artifacts := make([]boundary.ProtectedID, 0, len(ws.ArtifactRoots))
		for _, path := range ws.ArtifactRoots {
			id, err := resolveDispatchProtected(path, true)
			if err != nil {
				return dispatchMountHostSnapshot{}, err
			}
			artifacts = append(artifacts, id)
		}
		stateHome := filepath.Join(home, "state", "worksources", ws.WorksourceID, "home")
		worktree := filepath.Join(ws.WorkspaceRoot, ".mission-control", "tasks")
		toolHome, err := resolveDispatchProtected(ws.ToolHomeDir, true)
		if err != nil {
			return dispatchMountHostSnapshot{}, err
		}
		snapshot.WorksourceRoots[ws.WorksourceID] = boundary.WorksourceRoots{
			Workspace: workspace,
			Worktree:  mustDispatchProtected(worktree),
			Artifacts: artifacts,
			State:     mustDispatchProtected(stateHome),
			Cache:     mustDispatchProtected(filepath.Join(stateHome, ".cache")),
			ToolHome:  toolHome,
		}
		// The Git control registry resolves live per attest (ADR-021 D9/D11).
		// The fake lane keeps the Phase-1 posture: registering even an
		// absent-encoded own `.git` member would reject the sanctioned legacy
		// workspace bind through D8's absent-member protection, so the
		// registry activates only where that bind cannot exist.
		if ws.Kind == "repo" && !allowLegacyFakeWorkspace {
			controls, err := resolveWorksourceGitControls(ws.WorkspaceRoot)
			if err != nil {
				return dispatchMountHostSnapshot{}, err
			}
			snapshot.GitControls[ws.WorksourceID] = controls
		} else {
			snapshot.GitControls[ws.WorksourceID] = []boundary.ProtectedID{}
		}
		snapshot.MissionControlRoots[ws.WorksourceID] = []boundary.ProtectedID{}
		if ws.WorkspaceRoot != "" {
			mcRoot, err := resolveDispatchProtected(filepath.Join(ws.WorkspaceRoot, ".mission-control"), true)
			if err != nil {
				return dispatchMountHostSnapshot{}, err
			}
			snapshot.MissionControlRoots[ws.WorksourceID] = []boundary.ProtectedID{mcRoot}
		}
	}
	if state.SelectedWorksource != "" && subjectTaskID != nil && !allowLegacyFakeWorkspace {
		selected, err := selectedDispatchWorksource(state)
		if err != nil {
			return dispatchMountHostSnapshot{}, err
		}
		if selected.Kind == "repo" && selected.WorkspaceRoot != "" {
			root := filepath.Join(selected.WorkspaceRoot, ".mission-control", "tasks", "task-"+strconv.FormatInt(*subjectTaskID, 10))
			if _, err := os.Lstat(root); os.IsNotExist(err) {
				step, err := captureTaskPrecreate(selected.WorkspaceRoot, *subjectTaskID, uid)
				if err != nil {
					return dispatchMountHostSnapshot{}, err
				}
				setup, err := captureTaskSetup(selected.WorkspaceRoot, state)
				if err != nil {
					return dispatchMountHostSnapshot{}, err
				}
				step.Setup = &setup
				snapshot.TaskPrecreate = &step
			} else if err != nil {
				return dispatchMountHostSnapshot{}, err
			} else {
				// A receipt says the resident created this identity, but it does
				// not say the closure extraction reached its durable last step.
				// Never mint an agent plan from a skeleton whose immutable
				// assignment is absent: a crashed/lost setup-record must recover
				// through the closed recovery setup path, not expose an unpinned
				// tree to an agent.
				if state.SubjectTaskAssignment == nil {
					step, err := captureTaskRecovery(selected.WorkspaceRoot, *subjectTaskID, uid, state.SubjectTaskSetupRoots)
					if err != nil {
						return dispatchMountHostSnapshot{}, err
					}
					setup, err := captureTaskSetup(selected.WorkspaceRoot, state)
					if err != nil {
						return dispatchMountHostSnapshot{}, err
					}
					step.Setup = &setup
					snapshot.TaskPrecreate = &step
					return snapshot, nil
				}
				typed, err := resolveTaskLocalSkeleton(selected.WorkspaceRoot, *subjectTaskID, uid)
				if err != nil {
					return dispatchMountHostSnapshot{}, err
				}
				// The resolver proves the skeleton's shape, ownership, and
				// content, but not that Mission Control's own resident set it up.
				// A well-formed tree planted at the exact task path would
				// otherwise become an agent workspace. Admit it only when its
				// resolved root identity matches a durable first-task setup
				// receipt frozen under the token (ADR-016 D5); the run-keyed
				// InspectFirstTaskSetup fence is unsatisfiable here (spawn attest
				// holds no lease), so the identity travels in the mount state.
				if err := requireTaskSetupReceiptVouch(typed[boundary.KindTaskRoot], state.SubjectTaskSetupRoots); err != nil {
					return dispatchMountHostSnapshot{}, err
				}
				for kind, id := range typed {
					snapshot.TypedRoots[kind] = []boundary.ProtectedID{id}
				}
			}
		}
	}
	return snapshot, nil
}

// requireTaskSetupReceiptVouch admits a resolved first-task skeleton root into
// the agent plan only when its exact device/inode/owner identity is a member
// of the frozen setup-receipt set. It reuses inspectFirstTaskTable's tuple
// encoding so a materialized-but-unattested skeleton (or one swapped to a
// different identity than any receipt recorded) health-refuses rather than
// being trusted.
// captureTaskRecovery preserves an existing task-root inode only when its
// exact identity is already frozen from a durable receipt. It deliberately
// checks less than resolveTaskLocalSkeleton: this is the partial-store path
// whose child contents the resident will later scrub under its new lease.
func captureTaskRecovery(workspaceRoot string, taskID int64, ownerUID int, frozen []PrivateDispatchTaskSetupIdentity) (PrivateDispatchTaskPrecreate, error) {
	if taskID < 1 || taskID > maxJavaScriptSafeInteger {
		return PrivateDispatchTaskPrecreate{}, Domainf("task id %d is not a canonical positive decimal (ADR-017 D6)", taskID)
	}
	workspace, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	if workspace.Canonical != filepath.Clean(workspaceRoot) {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{Code: boundary.CodeSourceWrongKind, Msg: "repo workspace is not its exact canonical path"}
	}
	parentPath := filepath.Join(workspace.Canonical, ".mission-control", "tasks")
	if err := boundary.TrustHomeDir(parentPath, ownerUID); err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	parent, err := boundary.ResolveSource(parentPath)
	if err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	if !parent.IsDir || parent.Canonical != parentPath || parent.Info.Mode().Perm() != 0o700 {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{Code: boundary.CodeSourceWrongKind, Msg: "task parent is not the exact canonical mode-0700 .mission-control/tasks directory"}
	}
	pst, ok := parent.Info.Sys().(*syscall.Stat_t)
	if !ok || int(pst.Uid) != ownerUID {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "task parent is not owned by the operator"}
	}
	rootPath := filepath.Join(parentPath, "task-"+strconv.FormatInt(taskID, 10))
	root, err := os.Lstat(rootPath)
	if err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	if !root.IsDir() || root.Mode()&os.ModeSymlink != 0 || root.Mode().Perm() != 0o555 {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "task recovery root is not the fixed non-symlink mode-0555 directory"}
	}
	rst, ok := root.Sys().(*syscall.Stat_t)
	if !ok || int(rst.Uid) != ownerUID {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "task recovery root is not owned by the operator"}
	}
	identity := PrivateDispatchPathIdentity{Canonical: rootPath, Device: strconv.FormatUint(uint64(rst.Dev), 10), Inode: strconv.FormatUint(rst.Ino, 10), OwnerUID: int(rst.Uid)}
	for _, prior := range frozen {
		if prior.Device == identity.Device && prior.Inode == identity.Inode && prior.OwnerUID == identity.OwnerUID {
			return PrivateDispatchTaskPrecreate{ChildMode: taskSkeletonChildMode, RecoverRoot: &identity, TaskID: taskID,
				TasksParent: PrivateDispatchPathIdentity{Canonical: parent.Canonical, Device: strconv.FormatUint(uint64(pst.Dev), 10), Inode: strconv.FormatUint(pst.Ino, 10), OwnerUID: int(pst.Uid)}, WorkspaceRoot: workspace.Canonical}, nil
		}
	}
	return PrivateDispatchTaskPrecreate{}, &boundary.MountError{Code: boundary.CodeRuntimeUnappliable, Msg: "task recovery root has no durable first-task setup receipt"}
}

func requireTaskSetupReceiptVouch(root boundary.ProtectedID, frozen []PrivateDispatchTaskSetupIdentity) error {
	if root.Info == nil {
		return &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: "task skeleton root carries no identity evidence",
		}
	}
	st, ok := root.Info.Sys().(*syscall.Stat_t)
	if !ok {
		return &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: "task skeleton root exposes no stat identity",
		}
	}
	device := strconv.FormatUint(uint64(st.Dev), 10)
	inode := strconv.FormatUint(st.Ino, 10)
	uid := int(st.Uid)
	for _, id := range frozen {
		if id.Device == device && id.Inode == inode && id.OwnerUID == uid {
			return nil
		}
	}
	return &boundary.MountError{
		Code: boundary.CodeRuntimeUnappliable,
		Msg:  "task skeleton has no durable first-task setup receipt (ADR-016 D5); a materialized-but-unattested skeleton never becomes an agent workspace",
	}
}

func captureTaskPrecreate(workspaceRoot string, taskID int64, ownerUID int) (PrivateDispatchTaskPrecreate, error) {
	if taskID < 1 || taskID > maxJavaScriptSafeInteger {
		return PrivateDispatchTaskPrecreate{}, Domainf("task id %d is not a canonical positive decimal (ADR-017 D6)", taskID)
	}
	workspace, err := boundary.ResolveSource(workspaceRoot)
	if err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	if workspace.Canonical != filepath.Clean(workspaceRoot) {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: "repo workspace is not its exact canonical path",
		}
	}
	tasksParentPath := filepath.Join(workspace.Canonical, ".mission-control", "tasks")
	if err := boundary.TrustHomeDir(tasksParentPath, ownerUID); err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	parent, err := boundary.ResolveSource(tasksParentPath)
	if err != nil {
		return PrivateDispatchTaskPrecreate{}, err
	}
	if !parent.IsDir || parent.Canonical != tasksParentPath || parent.Info.Mode().Perm() != 0o700 {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind, Msg: "task parent is not the exact canonical mode-0700 .mission-control/tasks directory",
		}
	}
	st, ok := parent.Info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != ownerUID {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: "task parent is not owned by the operator",
		}
	}
	root := filepath.Join(tasksParentPath, "task-"+strconv.FormatInt(taskID, 10))
	if _, err := os.Lstat(root); err == nil {
		return PrivateDispatchTaskPrecreate{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: "task root appeared during absent-path attestation",
		}
	} else if !os.IsNotExist(err) {
		return PrivateDispatchTaskPrecreate{}, err
	}
	return PrivateDispatchTaskPrecreate{
		ChildMode: taskSkeletonChildMode, TaskID: taskID, WorkspaceRoot: workspace.Canonical,
		TasksParent: PrivateDispatchPathIdentity{
			Canonical: parent.Canonical,
			Device:    strconv.FormatUint(uint64(st.Dev), 10),
			Inode:     strconv.FormatUint(st.Ino, 10),
			OwnerUID:  int(st.Uid),
		},
	}, nil
}

// captureTaskSetup authors the precreate step's setup instruction. Mode and
// pins restate token-frozen spine state only (ADR-016 D5: a recorded
// assignment is reused, never rebased; a fresh run pins the frozen target
// ref), while the object format is fresh host evidence probed from the repo's
// administrative files — the resident is spine-blind, so this instruction is
// everything it may know when it writes /mc/setup.json.
func captureTaskSetup(workspaceRoot string, state PrivateDispatchMountState) (PrivateDispatchTaskSetup, error) {
	format, err := probeRepoObjectFormat(workspaceRoot)
	if err != nil {
		return PrivateDispatchTaskSetup{}, err
	}
	if assignment := state.SubjectTaskAssignment; assignment != nil {
		if assignment.ObjectFormat != format {
			return PrivateDispatchTaskSetup{}, &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable,
				Msg:  "repo object format diverged from the recorded first-task assignment (ADR-016 D5: a retry reuses, never rebases)",
			}
		}
		return PrivateDispatchTaskSetup{
			Mode: "retry", ObjectFormat: assignment.ObjectFormat,
			PinnedBaseSHA:       assignment.BaseSHA,
			PinnedClosureDigest: assignment.ClosureDigest,
			PinnedLocalRepoUUID: assignment.LocalRepoUUID,
		}, nil
	}
	if state.SubjectTaskTargetRef == "" {
		return PrivateDispatchTaskSetup{}, &boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "subject task carries no target ref; a first-task closure cannot be pinned",
		}
	}
	return PrivateDispatchTaskSetup{Mode: "fresh", ObjectFormat: format, TargetRef: state.SubjectTaskTargetRef}, nil
}

var captureDispatchMountSnapshot = captureDispatchMountHostSnapshot

func mustDispatchProtected(path string) boundary.ProtectedID {
	if path == "" || !filepath.IsAbs(path) {
		return boundary.ProtectedID{}
	}
	return boundary.ProtectedID{Canonical: filepath.Clean(path)}
}

// maxDispatchMountPlanBytes bounds the serialized carrier at attest, BEFORE
// any claim: the committed spawn effect embeds the plan and must survive the
// broker's 64 KiB canonical-result cap (ADR-016 D2), so an oversized plan is
// a pre-commit deployment-health refusal, never a post-commit wedge
// (takeover-review finding, 2026-07-16).
const maxDispatchMountPlanBytes = 32 * 1024

// attestCandidateMounts is the host-authority mount leg of dispatchAttest: it
// derives the candidate's ordinary requests, assembles jurisdiction from
// independently resolved host identities, authorizes the whole set, and
// returns ADR-016 D5's bounded evidence-backed plan carrier — or exactly one
// classified refusal. An empty plan (subjectless candidate, empty profile) is
// explicit, never absent.
func attestCandidateMounts(home string, cand *preparedCandidate, allowLegacyFakeWorkspace bool) (*PrivateDispatchMountPlan, *refusal.Refusal, error) {
	empty := &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{}}
	requests, selected, r, err := deriveDispatchMountRequests(cand.mountState, string(cand.spawn.Role), cand.spawn.SubjectID, allowLegacyFakeWorkspace)
	if err != nil || r != nil {
		return nil, r, err
	}
	if selected.WorksourceID == "" {
		return empty, nil, nil
	}
	// A truly empty ordinary policy has nothing to authorize. A nonempty
	// denied_paths set still constructs below even with zero requests: malformed
	// candidate policy must refuse before claim, not hide behind an empty plan.
	if len(requests) == 0 && len(selected.DeniedPaths) == 0 {
		return empty, nil, nil
	}
	snapshot, err := captureDispatchMountSnapshot(home, cand.mountState, cand.spawn.SubjectID, allowLegacyFakeWorkspace)
	if err != nil {
		r, aerr := adaptMountError(err, refusal.AuthorityDeployment, nil)
		return nil, r, aerr
	}
	if snapshot.TaskPrecreate != nil {
		ordinary := requests[:0]
		for _, request := range requests {
			if request.Kind == boundary.KindNone {
				ordinary = append(ordinary, request)
			}
		}
		requests = ordinary
	}
	assembled, err := assembleDispatchMountInputs(snapshot, cand.mountState, requests, selected)
	if err != nil {
		// A boundary rejection during assembly (a declared runtime-control
		// path failing resolution) is deployment configuration: health, not a
		// dispatch protocol error (takeover-review fix, 2026-07-16). Non-mount
		// errors stay protocol errors.
		r, aerr := adaptMountError(err, refusal.AuthorityDeployment, nil)
		return nil, r, aerr
	}
	j, err := boundary.ResolveJurisdiction(assembled.Jurisdiction, snapshot.OwnerUID)
	if err != nil {
		authority := refusal.AuthorityDeployment
		var me *boundary.MountError
		if errors.As(err, &me) && me.CandidateAuthored {
			authority = refusal.AuthorityCandidate
		}
		r, aerr := adaptMountError(err, authority, nil)
		return nil, r, aerr
	}
	jurisdictionDigest, err := jurisdictionInputDigest(assembled.Jurisdiction, snapshot.OwnerUID)
	if err != nil {
		authority := refusal.AuthorityDeployment
		var mountErr *boundary.MountError
		if errors.As(err, &mountErr) && mountErr.CandidateAuthored {
			authority = refusal.AuthorityCandidate
		}
		r, aerr := adaptMountError(err, authority, nil)
		return nil, r, aerr
	}
	entries, r, err := planMounts(assembled.Requests, mountPlanInputs{
		AllowlistPath: filepath.Join(home, "mount-allowlist"), OwnerUID: snapshot.OwnerUID,
		Blocked: boundary.BlockPolicy{}, Jurisdiction: j,
	})
	if err != nil || r != nil {
		return nil, r, err
	}
	if entries == nil {
		entries = []PrivateDispatchMountEntry{}
	}
	plan := &PrivateDispatchMountPlan{
		Version: 1, Entries: entries, JurisdictionDigest: jurisdictionDigest,
		TaskPrecreate: snapshot.TaskPrecreate,
	}
	if baseRole(string(cand.spawn.Role)) == "verifier" && cand.spawn.SubjectID != nil && cand.mountState.SubjectAcceptedCompletionSeal != nil {
		s := cand.mountState.SubjectAcceptedCompletionSeal
		plan.AcceptedSealRebuild = &PrivateDispatchAcceptedSealRebuild{
			TaskID: *cand.spawn.SubjectID, RunID: s.RunID, CompletionRequest: s.CompletionRequest,
			ObjectFormat: s.ObjectFormat, SealedSHA: s.SealedSHA, ClosureDigest: s.ClosureDigest,
			ManifestDigest: s.ManifestDigest, Device: s.Device, Inode: s.Inode, OwnerUID: s.OwnerUID,
		}
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxDispatchMountPlanBytes {
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "the authorized mount plan exceeds its effect byte budget",
		}, refusal.AuthorityDeployment, nil)
		return nil, &r, err
	}
	return plan, nil, nil
}
