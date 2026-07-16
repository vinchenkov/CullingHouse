package verbs

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

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
	ResolveDeclared     func(string) (boundary.ProtectedID, error)
}

type dispatchMountAssembly struct {
	Requests     []mountRequest
	Jurisdiction boundary.JurisdictionInput
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
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable, Msg: "selected Worksource has no sandbox profile",
		}, refusal.AuthorityCandidate, nil)
		return nil, selected, &r, err
	}
	requests := make([]mountRequest, 0, len(selected.ArtifactRoots)+len(selected.ReadonlyMounts))
	for _, source := range selected.ArtifactRoots {
		requests = append(requests, mountRequest{Source: source, Access: boundary.AccessRW, Authority: refusal.AuthorityCandidate})
	}
	for _, source := range selected.ReadonlyMounts {
		requests = append(requests, mountRequest{Source: source, Access: boundary.AccessRO, Authority: refusal.AuthorityCandidate})
	}
	return requests, selected, nil, nil
}

// assembleDispatchMountInputs associates the frozen all-Worksource projection
// with independently resolved host identities. A repo Worksource categorically
// cannot use its real checkout as /workspace/source: ADR-017 requires the
// task-local repository/projection typed plane, which is not represented by an
// ordinary profile request. The explicit test-fake exception preserves the
// Phase-1 fake resident only; fake routing is unreachable in production.
func assembleDispatchMountInputs(snapshot dispatchMountHostSnapshot, state PrivateDispatchMountState, allowLegacyFakeWorkspace bool) (dispatchMountAssembly, *refusal.Refusal, error) {
	requests, selected, r, err := selectedProfileMountRequests(state)
	if err != nil || r != nil {
		return dispatchMountAssembly{}, r, err
	}
	if selected.WorksourceID == "" {
		return dispatchMountAssembly{Requests: requests}, nil, nil
	}
	if selected.Kind == "repo" && !allowLegacyFakeWorkspace {
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "a Git Worksource requires a task-local repository or committed projection; its real workspace is never an agent mount",
		}, refusal.AuthorityDeployment, nil)
		return dispatchMountAssembly{}, &r, err
	}
	if snapshot.ResolveDeclared == nil {
		return dispatchMountAssembly{}, nil, Domainf("dispatch: host mount snapshot has no protected-path resolver")
	}
	own, ok := snapshot.WorksourceRoots[selected.WorksourceID]
	if !ok {
		return dispatchMountAssembly{}, nil, Domainf("dispatch: host snapshot omitted selected Worksource roots")
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
			return dispatchMountAssembly{}, nil, Domainf("dispatch: host snapshot omitted Worksource %q roots", ws.WorksourceID)
		}
		if ws.RuntimeControlDir != "" {
			id, err := snapshot.ResolveDeclared(ws.RuntimeControlDir)
			if err != nil {
				return dispatchMountAssembly{}, nil, err
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
	return dispatchMountAssembly{Requests: requests, Jurisdiction: in}, nil, nil
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

func captureDispatchMountHostSnapshot(home string, state PrivateDispatchMountState, allowLegacyFakeWorkspace bool) (dispatchMountHostSnapshot, error) {
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
		if ws.Kind == "repo" && !allowLegacyFakeWorkspace {
			return dispatchMountHostSnapshot{}, &boundary.MountError{
				Code: boundary.CodeRuntimeUnappliable,
				Msg:  "registered Git control identities are not yet available to host mount attest",
			}
		}
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
		snapshot.GitControls[ws.WorksourceID] = []boundary.ProtectedID{}
		snapshot.MissionControlRoots[ws.WorksourceID] = []boundary.ProtectedID{
			mustDispatchProtected(filepath.Join(ws.WorkspaceRoot, ".mission-control")),
		}
	}
	return snapshot, nil
}

var captureDispatchMountSnapshot = captureDispatchMountHostSnapshot

func mustDispatchProtected(path string) boundary.ProtectedID {
	if path == "" || !filepath.IsAbs(path) {
		return boundary.ProtectedID{}
	}
	return boundary.ProtectedID{Canonical: filepath.Clean(path)}
}

func attestCandidateMounts(home string, cand *preparedCandidate, allowLegacyFakeWorkspace bool) (*refusal.Refusal, error) {
	requests, selected, r, err := selectedProfileMountRequests(cand.mountState)
	if err != nil || r != nil {
		return r, err
	}
	if selected.Kind == "repo" && !allowLegacyFakeWorkspace {
		_, r, err := assembleDispatchMountInputs(dispatchMountHostSnapshot{}, cand.mountState, false)
		return r, err
	}
	if selected.WorksourceID == "" {
		return nil, nil
	}
	// A truly empty ordinary policy has nothing to authorize. A nonempty
	// denied_paths set still constructs below even with zero requests: malformed
	// candidate policy must refuse before claim, not hide behind an empty plan.
	if len(requests) == 0 && len(selected.DeniedPaths) == 0 {
		return nil, nil
	}
	snapshot, err := captureDispatchMountSnapshot(home, cand.mountState, allowLegacyFakeWorkspace)
	if err != nil {
		r, aerr := adaptMountError(err, refusal.AuthorityDeployment, nil)
		return r, aerr
	}
	assembled, r, err := assembleDispatchMountInputs(snapshot, cand.mountState, allowLegacyFakeWorkspace)
	if err != nil || r != nil {
		return r, err
	}
	j, err := boundary.ResolveJurisdiction(assembled.Jurisdiction, snapshot.OwnerUID)
	if err != nil {
		authority := refusal.AuthorityDeployment
		var me *boundary.MountError
		if errors.As(err, &me) && me.CandidateAuthored {
			authority = refusal.AuthorityCandidate
		}
		r, aerr := adaptMountError(err, authority, nil)
		return r, aerr
	}
	auths, r, err := planMounts(assembled.Requests, mountPlanInputs{
		AllowlistPath: filepath.Join(home, "mount-allowlist"), OwnerUID: snapshot.OwnerUID,
		Blocked: boundary.BlockPolicy{}, Jurisdiction: j,
	})
	if err != nil || r != nil {
		return r, err
	}
	if len(auths) != 0 {
		r, err := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeRuntimeUnappliable,
			Msg:  "authorized ordinary mounts cannot be applied until the closed plan carrier replaces the fake resident mount",
		}, refusal.AuthorityDeployment, nil)
		return &r, err
	}
	return nil, nil
}
