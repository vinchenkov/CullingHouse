package verbs

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
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

	requests, selected, r, err := deriveDispatchMountRequests(state, "worker", nil, false)
	if err != nil || r != nil {
		t.Fatalf("derive = refusal %+v err %v", r, err)
	}
	assembled, err := assembleDispatchMountInputs(snapshot, state, requests, selected)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	wantRequests := []mountRequest{
		{Source: ownArtifact, Access: boundary.AccessRW, Authority: refusal.AuthorityCandidate, Class: classArtifact},
		{Source: reference, Access: boundary.AccessRO, Authority: refusal.AuthorityCandidate, Class: classReference},
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

func TestDeriveDispatchMountRequestsRefusesDirectGitWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := maMkdir(t, root, "repo")
	maMkdir(t, workspace, ".git")
	state := PrivateDispatchMountState{SelectedWorksource: "repo", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "repo", Kind: "repo", Status: "active", ProfilePresent: true,
		ProfileID: "default", WorkspaceRoot: workspace,
		ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
	}}}
	requests, _, r, err := deriveDispatchMountRequests(state, "worker", nil, false)
	if err != nil || r == nil || len(requests) != 0 {
		t.Fatalf("direct Git derivation = %+v refusal %+v err %v", requests, r, err)
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

func TestDeriveDispatchMountRequestsRefusesInitiativeChildStandaloneTable(t *testing.T) {
	root := t.TempDir()
	workspace := maMkdir(t, root, "repo")
	initiativeID := int64(9)
	taskID := int64(7)
	state := PrivateDispatchMountState{
		SelectedWorksource:  "repo",
		SubjectInitiativeID: &initiativeID,
		Worksources: []PrivateDispatchWorksource{{
			WorksourceID: "repo", Kind: "repo", Status: "active", ProfilePresent: true,
			ProfileID: "default", WorkspaceRoot: workspace,
			ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
		}},
	}
	requests, _, r, err := deriveDispatchMountRequests(state, "worker", &taskID, false)
	if err != nil || r == nil || len(requests) != 0 {
		t.Fatalf("initiative-child derivation = %+v refusal %+v err %v", requests, r, err)
	}
	if r.Code != boundary.CodeRuntimeUnappliable || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("initiative-child refusal = %+v", r)
	}
}

func TestLoadDispatchMountStateFreezesInitiativeChildIdentity(t *testing.T) {
	db := dvSpine(t)
	initiativeID := int64(9)
	taskID := int64(7)
	state, err := loadDispatchMountState(context.Background(), db, &dispatch.Spawn{SubjectID: &taskID}, dispatch.Records{
		Tasks: []dispatch.Task{{ID: taskID, Worksource: "ws-test", InitiativeID: &initiativeID}},
	})
	if err != nil {
		t.Fatalf("loadDispatchMountState: %v", err)
	}
	if state.SubjectInitiativeID == nil || *state.SubjectInitiativeID != initiativeID {
		t.Fatalf("subject initiative = %v, want %d", state.SubjectInitiativeID, initiativeID)
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
	captureDispatchMountSnapshot = func(home string, state PrivateDispatchMountState, subjectTaskID *int64, allowFake bool) (dispatchMountHostSnapshot, error) {
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
	captureDispatchMountSnapshot = func(home string, state PrivateDispatchMountState, subjectTaskID *int64, allowFake bool) (dispatchMountHostSnapshot, error) {
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

// maStubSnapshot installs a hand-built host snapshot for the given
// Worksources, returning the operator home it fabricates. The real capture is
// exercised separately; these tests own the plan construction after it.
func maStubSnapshot(t *testing.T, root string, wsIDs ...string) string {
	t.Helper()
	operatorHome := maMkdir(t, root, "operator-home")
	oldCapture := captureDispatchMountSnapshot
	captureDispatchMountSnapshot = func(home string, state PrivateDispatchMountState, subjectTaskID *int64, allowFake bool) (dispatchMountHostSnapshot, error) {
		snapshot := dispatchMountHostSnapshot{
			OperatorHome: operatorHome, OwnerUID: os.Getuid(), MCHome: maID(t, home),
			HomeClassRoots: []boundary.ProtectedID{}, GatewaySecrets: []boundary.ProtectedID{},
			WorksourceRoots:     map[string]boundary.WorksourceRoots{},
			GitControls:         map[string][]boundary.ProtectedID{},
			MissionControlRoots: map[string][]boundary.ProtectedID{},
			TypedRoots:          map[boundary.TypedKind][]boundary.ProtectedID{},
			ResolveDeclared: func(path string) (boundary.ProtectedID, error) {
				return resolveDispatchProtected(path, true)
			},
		}
		for _, id := range wsIDs {
			snapshot.WorksourceRoots[id] = boundary.WorksourceRoots{
				Workspace: boundary.ProtectedID{Canonical: "/tmp/" + id},
			}
			snapshot.GitControls[id] = []boundary.ProtectedID{}
			snapshot.MissionControlRoots[id] = []boundary.ProtectedID{
				{Canonical: "/tmp/" + id + "/.mission-control"},
			}
		}
		return snapshot, nil
	}
	t.Cleanup(func() { captureDispatchMountSnapshot = oldCapture })
	return operatorHome
}

func maEvidence(t *testing.T, source string) (device, inode string, uid, mode int) {
	t.Helper()
	res, err := boundary.ResolveSource(source)
	if err != nil {
		t.Fatalf("ResolveSource(%q): %v", source, err)
	}
	st, ok := res.Info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no Stat_t for %q", source)
	}
	return strconv.FormatUint(uint64(st.Dev), 10), strconv.FormatUint(st.Ino, 10),
		int(st.Uid), int(res.Info.Mode().Perm())
}

// The replacement for the runtime_unappliable stop: a valid nonempty ordinary
// profile yields ADR-016 D5's evidence-backed plan carrier instead of a
// refusal — canonical sources, class-prefixed deterministic destinations,
// access, and (device,inode,kind,owner,mode) identity, sorted by destination.
func TestAttestCandidateMountsBuildsEvidencePlan(t *testing.T) {
	root := t.TempDir()
	mcHome := maMkdir(t, root, "mc-home")
	artifact := maMkdir(t, root, "artifact")
	reference := filepath.Join(root, "reference.md")
	if err := os.WriteFile(reference, []byte("ref\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"art\"\naccess = \"rw\"\n"+
		"\n[[allow]]\npath = %q\ntarget = \"refdoc\"\naccess = \"ro\"\n", artifact, reference)
	if err := os.WriteFile(filepath.Join(mcHome, "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	maStubSnapshot(t, root, "ws-plan")
	state := PrivateDispatchMountState{SelectedWorksource: "ws-plan", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "ws-plan", Kind: "personal", Status: "active", ProfilePresent: true,
		ProfileID: "p", WorkspaceRoot: "/tmp/ws-plan",
		ArtifactRoots: []string{artifact}, ReadonlyMounts: []string{reference}, DeniedPaths: []string{},
	}}}

	plan, r, err := attestCandidateMounts(mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: dispatch.RoleWorker}, mountState: state}, false)
	if err != nil || r != nil {
		t.Fatalf("attest = refusal %+v err %v, want a plan", r, err)
	}
	if plan == nil || plan.Version != 1 || len(plan.Entries) != 2 {
		t.Fatalf("plan = %+v, want version 1 with two entries", plan)
	}
	artDev, artIno, artUID, artMode := maEvidence(t, artifact)
	artCanonical, _ := boundary.ResolveSource(artifact)
	want0 := PrivateDispatchMountEntry{
		LogicalID: "artifact:art", Source: artCanonical.Canonical, Destination: "/workspace/artifacts/art",
		Kind: "dir", Access: "rw", Device: artDev, Inode: artIno, OwnerUID: artUID, Mode: artMode,
	}
	if plan.Entries[0] != want0 {
		t.Fatalf("artifact entry = %+v, want %+v", plan.Entries[0], want0)
	}
	refDev, refIno, refUID, refMode := maEvidence(t, reference)
	refCanonical, _ := boundary.ResolveSource(reference)
	want1 := PrivateDispatchMountEntry{
		LogicalID: "reference:refdoc", Source: refCanonical.Canonical, Destination: "/workspace/references/refdoc",
		Kind: "file", Access: "ro", Device: refDev, Inode: refIno, OwnerUID: refUID, Mode: refMode,
	}
	if plan.Entries[1] != want1 {
		t.Fatalf("reference entry = %+v, want %+v", plan.Entries[1], want1)
	}
}

// Under test-fake routing only, the Phase-1 workspace bind is rerouted
// through the same carrier: the profile's workspace root authorizes via the
// allowlist to exactly /workspace/source RW. The same candidate without the
// fake exception keeps the production repo health stop.
func TestAttestCandidateMountsFakeLegacyWorkspaceRidesTheCarrier(t *testing.T) {
	root := t.TempDir()
	mcHome := maMkdir(t, root, "mc-home")
	workspace := maMkdir(t, root, "checkout")
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"source\"\naccess = \"rw\"\n", workspace)
	if err := os.WriteFile(filepath.Join(mcHome, "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	maStubSnapshot(t, root, "ws-fake")
	state := PrivateDispatchMountState{SelectedWorksource: "ws-fake", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "ws-fake", Kind: "repo", Status: "active", ProfilePresent: true,
		ProfileID: "default", WorkspaceRoot: workspace,
		ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
	}}}

	plan, r, err := attestCandidateMounts(mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: dispatch.RoleWorker}, mountState: state}, true)
	if err != nil || r != nil {
		t.Fatalf("fake-lane attest = refusal %+v err %v, want a plan", r, err)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("fake-lane plan = %+v, want exactly the workspace entry", plan)
	}
	entry := plan.Entries[0]
	wsCanonical, _ := boundary.ResolveSource(workspace)
	if entry.Destination != "/workspace/source" || entry.Access != "rw" || entry.Kind != "dir" ||
		entry.LogicalID != "workspace:source" || entry.Source != wsCanonical.Canonical ||
		entry.Device == "" || entry.Inode == "" {
		t.Fatalf("workspace entry = %+v", entry)
	}

	plan, r, err = attestCandidateMounts(mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: dispatch.RoleWorker}, mountState: state}, false)
	if err != nil || r == nil || plan != nil {
		t.Fatalf("production repo attest = plan %+v refusal %+v err %v, want the health stop", plan, r, err)
	}
	if r.Code != boundary.CodeRuntimeUnappliable || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("production repo refusal = %+v", r)
	}
}

// An absent sandbox profile is deployment configuration the candidate never
// authored: deployment health, not a per-task confinement block
// (takeover-review reclassification, 2026-07-16).
func TestAttestCandidateMountsAbsentProfileIsDeploymentHealth(t *testing.T) {
	root := t.TempDir()
	mcHome := maMkdir(t, root, "mc-home")
	state := PrivateDispatchMountState{SelectedWorksource: "ws-bare", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "ws-bare", Kind: "personal", Status: "active", ProfilePresent: false,
		ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
	}}}
	plan, r, err := attestCandidateMounts(mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: dispatch.RoleWorker}, mountState: state}, false)
	if err != nil || r == nil || plan != nil {
		t.Fatalf("absent-profile attest = plan %+v refusal %+v err %v", plan, r, err)
	}
	if r.Code != boundary.CodeRuntimeUnappliable || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("absent-profile refusal = %+v", r)
	}
	if class, cerr := refusal.Classify(*r); cerr != nil || class != refusal.ClassHealth {
		t.Fatalf("absent-profile classify = %v/%v, want health", class, cerr)
	}
}

// A boundary rejection while assembling jurisdiction (a declared
// runtime-control path failing resolution) is deployment health, never a
// dispatch protocol error (takeover-review fix, 2026-07-16).
func TestAttestCandidateMountsAssemblyFailureIsDeploymentHealth(t *testing.T) {
	root := t.TempDir()
	mcHome := maMkdir(t, root, "mc-home")
	artifact := maMkdir(t, root, "artifact")
	maStubSnapshot(t, root, "own", "other")
	state := PrivateDispatchMountState{SelectedWorksource: "own", Worksources: []PrivateDispatchWorksource{
		{WorksourceID: "other", Kind: "personal", Status: "active", ProfilePresent: true,
			ProfileID: "p2", RuntimeControlDir: "relative/path",
			ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{}},
		{WorksourceID: "own", Kind: "personal", Status: "active", ProfilePresent: true,
			ProfileID: "p1", ArtifactRoots: []string{artifact}, ReadonlyMounts: []string{}, DeniedPaths: []string{}},
	}}
	plan, r, err := attestCandidateMounts(mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: dispatch.RoleWorker}, mountState: state}, false)
	if err != nil || r == nil || plan != nil {
		t.Fatalf("assembly-failure attest = plan %+v refusal %+v err %v, want a health refusal", plan, r, err)
	}
	if r.Code != boundary.CodeSourceWrongKind || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("assembly-failure refusal = %+v", r)
	}
	if class, cerr := refusal.Classify(*r); cerr != nil || class != refusal.ClassHealth {
		t.Fatalf("assembly-failure classify = %v/%v, want health", class, cerr)
	}
}

// The carrier is byte-bounded at attest, BEFORE any claim: the committed
// spawn effect embeds the plan and must survive the broker's 64 KiB result
// cap, so an oversized plan refuses health here rather than wedging
// post-commit (takeover-review finding, 2026-07-16).
func TestAttestCandidateMountsOversizedPlanFailsHealth(t *testing.T) {
	root := t.TempDir()
	mcHome := maMkdir(t, root, "mc-home")
	bulk := maMkdir(t, root, "bulk")
	sources := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("%s%03d", strings.Repeat("a", 237), i)
		sources = append(sources, maMkdir(t, bulk, name))
	}
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"bulk\"\naccess = \"ro\"\n", bulk)
	if err := os.WriteFile(filepath.Join(mcHome, "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	maStubSnapshot(t, root, "ws-bulk")
	state := PrivateDispatchMountState{SelectedWorksource: "ws-bulk", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "ws-bulk", Kind: "personal", Status: "active", ProfilePresent: true,
		ProfileID: "p", ArtifactRoots: []string{}, ReadonlyMounts: sources, DeniedPaths: []string{},
	}}}
	plan, r, err := attestCandidateMounts(mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: dispatch.RoleWorker}, mountState: state}, false)
	if err != nil || r == nil || plan != nil {
		t.Fatalf("oversized attest = plan %+v refusal %+v err %v, want the byte-bound refusal", plan, r, err)
	}
	if r.Code != boundary.CodeRuntimeUnappliable || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("oversized refusal = %+v", r)
	}
}

// maRepoCandidate builds a production repo candidate over the exact task-7
// skeleton from tsBuild plus a real .git control, and a trusted MC_HOME with
// an empty allowlist. Nothing stubs the snapshot: these tests drive the real
// captureDispatchMountHostSnapshot and the live Git registry.
func maRepoCandidate(t *testing.T, role dispatch.Role, subject *int64) (string, *preparedCandidate, string) {
	t.Helper()
	ws, _ := tsBuild(t)
	if err := os.Mkdir(filepath.Join(ws, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	mcHome := maMkdir(t, filepath.Dir(ws), "mc-home-"+string(role))
	if err := os.Chmod(mcHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcHome, "mount-allowlist"), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := PrivateDispatchMountState{SelectedWorksource: "repo-ws", Worksources: []PrivateDispatchWorksource{{
		WorksourceID: "repo-ws", Kind: "repo", Status: "active", ProfilePresent: true,
		ProfileID: "p", WorkspaceRoot: ws,
		ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
	}}}
	return mcHome, &preparedCandidate{spawn: &dispatch.Spawn{Role: role, SubjectID: subject}, mountState: state}, ws
}

func TestAttestCandidateMountsDerivesTaskLocalPlanThroughRealCapture(t *testing.T) {
	subject := int64(7)
	mcHome, cand, ws := maRepoCandidate(t, dispatch.RoleWorker, &subject)

	plan, r, err := attestCandidateMounts(mcHome, cand, false)
	if err != nil || r != nil {
		t.Fatalf("attest = refusal %+v err %v", r, err)
	}
	if plan == nil || len(plan.Entries) != 15 {
		t.Fatalf("plan = %+v, want the 15 task-local rows", plan)
	}
	if plan.Entries[0].Destination != "/workspace" || plan.Entries[0].Access != "ro" ||
		plan.Entries[0].Mode != 0o555 {
		t.Fatalf("task root entry = %+v", plan.Entries[0])
	}
	taskRoot := filepath.Join(ws, ".mission-control", "tasks", "task-7")
	byDest := map[string]PrivateDispatchMountEntry{}
	for _, e := range plan.Entries {
		byDest[e.Destination] = e
	}
	if got := byDest["/workspace/git"]; got.Access != "rw" || got.Source != filepath.Join(taskRoot, "git") {
		t.Fatalf("git entry = %+v", got)
	}
	if got := byDest["/workspace/git/worktrees/mc-task-7/gitdir"]; got.Access != "ro" || got.Kind != "file" {
		t.Fatalf("worktree gitdir cover = %+v", got)
	}
	emptyDigest := fmt.Sprintf("%x", sha256.Sum256(nil))
	if got := byDest["/workspace/git/config"]; got.ContentSHA256 != emptyDigest || got.RequireEmptyDir {
		t.Fatalf("config cover evidence = %+v, want fixed empty-file digest", got)
	}
	if got := byDest["/workspace/git/hooks"]; !got.RequireEmptyDir || got.ContentSHA256 != "" {
		t.Fatalf("hooks cover evidence = %+v, want generated-empty-directory fence", got)
	}
}

func TestAttestCandidateMountsAbsentSkeletonIsDeploymentHealth(t *testing.T) {
	subject := int64(9) // no task-9 skeleton exists
	mcHome, cand, _ := maRepoCandidate(t, dispatch.RoleWorker, &subject)

	plan, r, err := attestCandidateMounts(mcHome, cand, false)
	if err != nil || r == nil || plan != nil {
		t.Fatalf("attest = plan %+v refusal %+v err %v, want the absent-skeleton refusal", plan, r, err)
	}
	if r.Code != boundary.CodeSourceMissing || r.Authority != refusal.AuthorityDeployment {
		t.Fatalf("absent-skeleton refusal = %+v", r)
	}
	class, cerr := refusal.Classify(*r)
	if cerr != nil || class != refusal.ClassHealth {
		t.Fatalf("absent skeleton must record health, got %v (%v)", class, cerr)
	}
}

func TestAttestCandidateMountsProjectionRolesStayHealthRefused(t *testing.T) {
	subject := int64(7)
	for _, role := range []dispatch.Role{"verifier", "packager", "refiner", "editor"} {
		mcHome, cand, _ := maRepoCandidate(t, role, &subject)
		plan, r, err := attestCandidateMounts(mcHome, cand, false)
		if err != nil || r == nil || plan != nil {
			t.Fatalf("%s attest = plan %+v refusal %+v err %v, want the unrealizable-arm refusal", role, plan, r, err)
		}
		if r.Code != boundary.CodeRuntimeUnappliable || r.Authority != refusal.AuthorityDeployment {
			t.Fatalf("%s refusal = %+v", role, r)
		}
	}
}

func TestAttestCandidateMountsRegistryProtectsRealGitControl(t *testing.T) {
	subject := int64(7)
	mcHome, cand, ws := maRepoCandidate(t, dispatch.RoleWorker, &subject)
	// An artifact root reaching inside the registered control refuses on
	// jurisdiction even though the operator allowlisted it: the registry now
	// feeds OwnGitControls, so the real object store can never ride an
	// ordinary mount.
	inside := filepath.Join(ws, ".git", "objects")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"objects\"\naccess = \"rw\"\n", inside)
	if err := os.WriteFile(filepath.Join(mcHome, "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	cand.mountState.Worksources[0].ArtifactRoots = []string{inside}

	plan, r, err := attestCandidateMounts(mcHome, cand, false)
	if err != nil || r == nil || plan != nil {
		t.Fatalf("attest = plan %+v refusal %+v err %v, want the jurisdiction refusal", plan, r, err)
	}
	if r.Code != boundary.CodeDeniedRoot {
		t.Fatalf("control-intersecting artifact = %+v, want denied_root", r)
	}
}

func TestAttestCandidateMountsCarriesProtectedSetIdentityDrift(t *testing.T) {
	subject := int64(7)
	mcHome, cand, ws := maRepoCandidate(t, dispatch.RoleWorker, &subject)
	first, r, err := attestCandidateMounts(mcHome, cand, false)
	if err != nil || r != nil {
		t.Fatalf("first attest = refusal %+v err %v", r, err)
	}

	gitPath := filepath.Join(ws, ".git")
	oldGit := filepath.Join(ws, ".git-old")
	if err := os.Rename(gitPath, oldGit); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(gitPath, 0o700); err != nil {
		t.Fatal(err)
	}
	second, r, err := attestCandidateMounts(mcHome, cand, false)
	if err != nil || r != nil {
		t.Fatalf("second attest = refusal %+v err %v", r, err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("protected Git-control identity drift disappeared behind an unchanged mount verdict (ADR-021 D9/D11)")
	}
}

func TestJurisdictionDigestCarriesAbsentMemberAnchorIdentity(t *testing.T) {
	root := t.TempDir()
	anchor := maMkdir(t, root, "anchor")
	declared := filepath.Join(anchor, "future", "git")
	in := boundary.JurisdictionInput{
		DeniedPaths:      []string{},
		OtherGitControls: []boundary.ProtectedID{{Canonical: declared}},
		TypedRoots:       map[boundary.TypedKind][]boundary.ProtectedID{},
	}
	first, err := jurisdictionInputDigest(in, os.Getuid())
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	if err := os.Rename(anchor, filepath.Join(root, "old-anchor")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(anchor, 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := jurisdictionInputDigest(in, os.Getuid())
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if first == second {
		t.Fatal("D8 absent-member digest omitted its effective nearest-existing-ancestor identity")
	}
}

func TestJurisdictionDigestCarriesDeniedPathIdentity(t *testing.T) {
	root := t.TempDir()
	denied := maMkdir(t, root, "denied")
	in := boundary.JurisdictionInput{
		DeniedPaths: []string{denied},
		TypedRoots:  map[boundary.TypedKind][]boundary.ProtectedID{},
	}
	first, err := jurisdictionInputDigest(in, os.Getuid())
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	if err := os.Rename(denied, filepath.Join(root, "old-denied")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(denied, 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := jurisdictionInputDigest(in, os.Getuid())
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if first == second {
		t.Fatal("denied-path identity drift disappeared behind its unchanged raw spelling")
	}
}

func TestDispatchRepoWorkerCommitsTaskLocalMountPlan(t *testing.T) {
	ws, _ := tsBuild(t)
	if err := os.Mkdir(filepath.Join(ws, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	db := dvSpine(t, func(a *InitArgs) { a.WorkspaceRoot = ws })
	// dvSpine parks the Worksource on the registered non-repository arm; this
	// test is exactly about the repo arm the registry slice opened.
	dvExec(t, db, `UPDATE worksources SET kind='repo' WHERE id='ws-test'`)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
	if err := os.Chmod(os.Getenv("MC_HOME"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("MC_HOME"), "mount-allowlist"), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	prepared := dfPrepare(t, db, dfRequestID)
	attested, err := dispatchAttest(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatalf("dispatchAttest: %v", err)
	}
	if attested.refusal != nil {
		t.Fatalf("repo worker over an exact skeleton refused: %+v", attested.refusal)
	}
	eff := dfCommit(t, db, prepared, attested)
	if eff["action"] != "spawn" {
		t.Fatalf("effect = %v, want a spawn", eff)
	}
	body, err := json.Marshal(eff["mount_plan"])
	if err != nil {
		t.Fatal(err)
	}
	var plan PrivateDispatchMountPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("committed mount_plan does not decode as the carrier: %v", err)
	}
	if plan.Version != 1 || len(plan.Entries) != 15 {
		t.Fatalf("committed plan = version %d with %d entries, want the 15 task-local rows", plan.Version, len(plan.Entries))
	}
	if plan.Entries[0].Destination != "/workspace" || plan.Entries[0].LogicalID != "task-root" {
		t.Fatalf("committed first row = %+v", plan.Entries[0])
	}
	if n := dfInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind='dispatch.spawn'`); n != 1 {
		t.Fatalf("spawn wrote %d dispatch.spawn rows, want one", n)
	}
}
