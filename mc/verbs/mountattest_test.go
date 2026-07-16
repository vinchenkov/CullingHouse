package verbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mc/boundary"
	"mc/dispatch"
	"mc/refusal"
)

func maID(t *testing.T, path string) boundary.ProtectedID {
	t.Helper()
	id, err := boundary.ResolveSource(path)
	if err != nil {
		t.Fatalf("ResolveSource(%q): %v", path, err)
	}
	return boundary.ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}
}

func maMkdir(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAssembleDispatchMountInputsUsesSelectedProfileAndCompleteJurisdiction(t *testing.T) {
	root := t.TempDir()
	operatorHome := maMkdir(t, root, "operator-home")
	mcHome := maMkdir(t, root, "mc-home")
	homeClass := maMkdir(t, operatorHome, ".ssh")
	gateway := maMkdir(t, root, "gateway-secret")
	ownWorkspace := maMkdir(t, root, "own-workspace")
	ownWorktree := maMkdir(t, root, "own-worktree")
	ownArtifact := maMkdir(t, root, "own-artifact")
	ownState := maMkdir(t, root, "own-state")
	ownCache := maMkdir(t, root, "own-cache")
	ownTool := maMkdir(t, root, "own-tool")
	ownGit := maMkdir(t, root, "own-git")
	ownMC := maMkdir(t, root, "own-mc")
	otherWorkspace := maMkdir(t, root, "other-workspace")
	otherWorktree := maMkdir(t, root, "other-worktree")
	otherArtifact := maMkdir(t, root, "other-artifact")
	otherState := maMkdir(t, root, "other-state")
	otherCache := maMkdir(t, root, "other-cache")
	otherTool := maMkdir(t, root, "other-tool")
	otherGit := maMkdir(t, root, "other-git")
	otherMC := maMkdir(t, root, "other-mc")
	ownRuntime := maMkdir(t, root, "own-runtime")
	otherRuntime := maMkdir(t, root, "other-runtime")
	reference := maMkdir(t, root, "reference")
	typed := maMkdir(t, root, "typed")

	state := PrivateDispatchMountState{SelectedWorksource: "own", Worksources: []PrivateDispatchWorksource{
		{WorksourceID: "other", Kind: "personal", Status: "active", ProfilePresent: true,
			ProfileID: "other-profile", WorkspaceRoot: otherWorkspace,
			ArtifactRoots: []string{otherArtifact}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
			ToolHomeDir: otherTool, RuntimeControlDir: otherRuntime},
		{WorksourceID: "own", Kind: "personal", Status: "active", ProfilePresent: true,
			ProfileID: "own-profile", WorkspaceRoot: ownWorkspace,
			ArtifactRoots: []string{ownArtifact}, ReadonlyMounts: []string{reference},
			DeniedPaths: []string{filepath.Join(ownWorkspace, "private")},
			ToolHomeDir: ownTool, RuntimeControlDir: ownRuntime},
	}}
	snapshot := dispatchMountHostSnapshot{
		OperatorHome:   operatorHome,
		MCHome:         maID(t, mcHome),
		HomeClassRoots: []boundary.ProtectedID{maID(t, homeClass)},
		GatewaySecrets: []boundary.ProtectedID{maID(t, gateway)},
		WorksourceRoots: map[string]boundary.WorksourceRoots{
			"own": {
				Workspace: maID(t, ownWorkspace), Worktree: maID(t, ownWorktree),
				Artifacts: []boundary.ProtectedID{maID(t, ownArtifact)}, State: maID(t, ownState),
				Cache: maID(t, ownCache), ToolHome: maID(t, ownTool),
			},
			"other": {
				Workspace: maID(t, otherWorkspace), Worktree: maID(t, otherWorktree),
				Artifacts: []boundary.ProtectedID{maID(t, otherArtifact)}, State: maID(t, otherState),
				Cache: maID(t, otherCache), ToolHome: maID(t, otherTool),
			},
		},
		GitControls: map[string][]boundary.ProtectedID{
			"own": {maID(t, ownGit)}, "other": {maID(t, otherGit)},
		},
		MissionControlRoots: map[string][]boundary.ProtectedID{
			"own": {maID(t, ownMC)}, "other": {maID(t, otherMC)},
		},
		TypedRoots: map[boundary.TypedKind][]boundary.ProtectedID{
			boundary.KindOwnSession: {maID(t, typed)},
		},
		ResolveDeclared: func(path string) (boundary.ProtectedID, error) {
			id, err := boundary.ResolveSource(path)
			if err != nil {
				return boundary.ProtectedID{}, err
			}
			return boundary.ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}, nil
		},
	}

	assembled, r, err := assembleDispatchMountInputs(snapshot, state, false)
	if err != nil || r != nil {
		t.Fatalf("assemble = refusal %+v err %v", r, err)
	}
	wantRequests := []mountRequest{
		{Source: ownArtifact, Access: boundary.AccessRW, Authority: refusal.AuthorityCandidate},
		{Source: reference, Access: boundary.AccessRO, Authority: refusal.AuthorityCandidate},
	}
	if !reflect.DeepEqual(assembled.Requests, wantRequests) {
		t.Fatalf("requests = %+v, want %+v", assembled.Requests, wantRequests)
	}
	in := assembled.Jurisdiction
	if in.Home != operatorHome || !reflect.DeepEqual(in.DeniedPaths, state.Worksources[1].DeniedPaths) ||
		len(in.HomeClassRoots) != 1 || len(in.GatewaySecrets) != 1 || len(in.RuntimeControls) != 2 ||
		len(in.OtherWorksources) != 1 || len(in.OwnGitControls) != 1 || len(in.OtherGitControls) != 1 ||
		len(in.OwnMissionControlRoots) != 1 || len(in.OtherMissionControlRoots) != 1 || len(in.TypedRoots) != 1 {
		t.Fatalf("jurisdiction input omitted a required class: %+v", in)
	}
	j, err := boundary.ResolveJurisdiction(in, os.Getuid())
	if err != nil {
		t.Fatalf("ResolveJurisdiction: %v", err)
	}
	ownID, _ := boundary.ResolveSource(ownArtifact)
	if err := j.Rejects(ownID, boundary.TypedClaim{}); err != nil {
		t.Fatalf("own ordinary artifact rejected: %v", err)
	}
	otherID, _ := boundary.ResolveSource(otherArtifact)
	if err := j.Rejects(otherID, boundary.TypedClaim{}); err == nil {
		t.Fatalf("other Worksource artifact was accepted")
	} else if varME := new(boundary.MountError); !errors.As(err, &varME) || varME.Code != boundary.CodeCrossWorksource {
		t.Fatalf("other Worksource artifact = %v, want %s", err, boundary.CodeCrossWorksource)
	}
}

func TestAssembleDispatchMountInputsRefusesDirectGitWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := maMkdir(t, root, "repo")
	maMkdir(t, workspace, ".git")
	state := PrivateDispatchMountState{SelectedWorksource: "repo", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "repo", Kind: "repo", Status: "active", ProfilePresent: true,
		ProfileID: "default", WorkspaceRoot: workspace,
		ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
	}}}
	assembled, r, err := assembleDispatchMountInputs(dispatchMountHostSnapshot{}, state, false)
	if err != nil || r == nil || len(assembled.Requests) != 0 {
		t.Fatalf("direct Git assembly = %+v refusal %+v err %v", assembled, r, err)
	}
	if r.Code != boundary.CodeRuntimeUnappliable || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("direct Git refusal = %+v", r)
	}
	detail, err := refusal.DetailFor(*r)
	if err != nil {
		t.Fatalf("DetailFor: %v", err)
	}
	encoded, err := detail.Canonical()
	if err != nil || strings.Contains(string(encoded), workspace) {
		t.Fatalf("sanitized refusal detail = %s err %v", encoded, err)
	}
}

func TestDispatchInvalidSelectedProfileMountNeverClaimsOrSpawns(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	dvExec(t, db, `UPDATE worksources SET kind='personal' WHERE id='ws-test'`)
	root := t.TempDir()
	operatorHome := maMkdir(t, root, "operator-home")
	t.Setenv("HOME", operatorHome)
	if err := os.Chmod(os.Getenv("MC_HOME"), 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := maMkdir(t, root, "allowed")
	missing := filepath.Join(allowed, "missing")
	dvExec(t, db, `UPDATE sandbox_profiles SET readonly_mounts=? WHERE id='default'`, fmt.Sprintf(`[%q]`, missing))
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"reference\"\naccess = \"ro\"\n", allowed)
	if err := os.WriteFile(filepath.Join(os.Getenv("MC_HOME"), "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCapture := captureDispatchMountSnapshot
	captureDispatchMountSnapshot = func(home string, state PrivateDispatchMountState, allowFake bool) (dispatchMountHostSnapshot, error) {
		return dispatchMountHostSnapshot{
			OperatorHome: operatorHome, OwnerUID: os.Getuid(), MCHome: maID(t, home),
			HomeClassRoots: []boundary.ProtectedID{}, GatewaySecrets: []boundary.ProtectedID{},
			WorksourceRoots: map[string]boundary.WorksourceRoots{
				"ws-test": {Workspace: boundary.ProtectedID{Canonical: "/tmp/ws-test"}},
			},
			GitControls: map[string][]boundary.ProtectedID{"ws-test": {}},
			MissionControlRoots: map[string][]boundary.ProtectedID{
				"ws-test": {{Canonical: "/tmp/ws-test/.mission-control"}},
			},
			TypedRoots: map[boundary.TypedKind][]boundary.ProtectedID{},
			ResolveDeclared: func(path string) (boundary.ProtectedID, error) {
				return boundary.ProtectedID{Canonical: filepath.Clean(path)}, nil
			},
		}, nil
	}
	t.Cleanup(func() { captureDispatchMountSnapshot = oldCapture })

	prepared := dfPrepare(t, db, dfRequestID)
	attested, err := dispatchAttest(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatalf("dispatchAttest: %v", err)
	}
	if attested.refusal == nil || attested.refusal.Code != boundary.CodeSourceMissing {
		t.Fatalf("attestation refusal = %+v, want %s", attested.refusal, boundary.CodeSourceMissing)
	}
	if attested.refusal.Authority != refusal.AuthorityCandidate {
		t.Fatalf("invalid selected-profile source authority = %q", attested.refusal.Authority)
	}
	eff := dfCommit(t, db, prepared, attested)
	dfAssertInert(t, db, eff)
	if eff["action"] == "spawn" || eff["code"] != boundary.CodeSourceMissing {
		t.Fatalf("invalid mount effect = %v", eff)
	}
	if n := dfInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind='dispatch.spawn'`); n != 0 {
		t.Fatalf("invalid mount wrote %d dispatch.spawn rows", n)
	}
	if n := dfInt(t, db, `SELECT COUNT(*) FROM tasks WHERE id=1 AND blocked=1`); n != 1 {
		t.Fatalf("invalid candidate mount blocked %d subject rows, want one", n)
	}
}

func TestDispatchInvalidDeniedPathWithoutRequestsNeverClaims(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	dvExec(t, db, `UPDATE worksources SET kind='personal' WHERE id='ws-test'`)
	dvExec(t, db, `UPDATE sandbox_profiles SET denied_paths='["relative/path"]' WHERE id='default'`)
	root := t.TempDir()
	operatorHome := maMkdir(t, root, "operator-home")
	if err := os.Chmod(os.Getenv("MC_HOME"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldCapture := captureDispatchMountSnapshot
	captureDispatchMountSnapshot = func(home string, state PrivateDispatchMountState, allowFake bool) (dispatchMountHostSnapshot, error) {
		return dispatchMountHostSnapshot{
			OperatorHome: operatorHome, OwnerUID: os.Getuid(), MCHome: maID(t, home),
			HomeClassRoots: []boundary.ProtectedID{}, GatewaySecrets: []boundary.ProtectedID{},
			WorksourceRoots: map[string]boundary.WorksourceRoots{
				"ws-test": {Workspace: boundary.ProtectedID{Canonical: "/tmp/ws-test"}},
			},
			GitControls: map[string][]boundary.ProtectedID{"ws-test": {}},
			MissionControlRoots: map[string][]boundary.ProtectedID{
				"ws-test": {{Canonical: "/tmp/ws-test/.mission-control"}},
			},
			TypedRoots: map[boundary.TypedKind][]boundary.ProtectedID{},
			ResolveDeclared: func(path string) (boundary.ProtectedID, error) {
				return boundary.ProtectedID{Canonical: filepath.Clean(path)}, nil
			},
		}, nil
	}
	t.Cleanup(func() { captureDispatchMountSnapshot = oldCapture })

	prepared := dfPrepare(t, db, "0123456789abcdec")
	attested, err := dispatchAttest(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatalf("dispatchAttest: %v", err)
	}
	if attested.refusal == nil || attested.refusal.Code != boundary.CodeDeniedRoot ||
		attested.refusal.Authority != refusal.AuthorityCandidate {
		t.Fatalf("denied-path attestation = %+v", attested.refusal)
	}
	eff := dfCommit(t, db, prepared, attested)
	dfAssertInert(t, db, eff)
	if eff["action"] == "spawn" || eff["code"] != boundary.CodeDeniedRoot {
		t.Fatalf("invalid denied-path effect = %v", eff)
	}
}

func TestJurisdictionErrorProvenanceDoesNotChargeCandidateForDeploymentFailure(t *testing.T) {
	root := t.TempDir()
	home := maMkdir(t, root, "home")
	base := boundary.JurisdictionInput{
		Home: home, MCHome: boundary.ProtectedID{Canonical: filepath.Join(root, "mc-home")},
		DeniedPaths: []string{"relative/path"},
	}
	_, err := boundary.ResolveJurisdiction(base, os.Getuid())
	var me *boundary.MountError
	if !errors.As(err, &me) || !me.CandidateAuthored {
		t.Fatalf("denied-path failure provenance = %#v, want candidate-authored", err)
	}

	base.Home = filepath.Join(root, "missing-home")
	_, err = boundary.ResolveJurisdiction(base, os.Getuid())
	me = nil
	if !errors.As(err, &me) || me.CandidateAuthored {
		t.Fatalf("deployment failure provenance = %#v, must not charge candidate", err)
	}
}
