package verbs

// The sealed landing lane's own prepare/attest/commit (ADR-016:369-379). These
// tests drive the lane's functions DIRECTLY: nothing selects a sealed row yet,
// and nothing will until the activation switch, so a test that went through
// `mc dispatch` would assert on the legacy lane and quietly prove nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/dispatch"
	"mc/refusal"
	"mc/routing"
	"mc/substrate"
)

// dlsTask builds a row that satisfies SealedLandingPending: approved, packaged,
// unarchived, unblocked, branchless in `tasks`, and carrying its branch on the
// immutable assignment instead.
func dlsTask() dispatch.Task {
	return dispatch.Task{
		ID: 7, Title: "fix the gate", Scope: dispatch.ScopeTask,
		Status: dispatch.StatusPackaged, Decision: dispatch.DecisionApproved,
		Archived: false, Blocked: false, Worksource: "ws-test",
		Branch:      "", // the sealed lane is branchless in `tasks` by construction
		VerifiedSHA: "3333333333333333333333333333333333333333",
		TargetRef:   "refs/heads/main",
		Sealed: &dispatch.SealedAssignment{
			Branch:        "mc/task-7",
			TargetRef:     "refs/heads/main",
			TaskRootKey:   "16777220:424242",
			ObjectFormat:  "sha1",
			BaseSHA:       "1111111111111111111111111111111111111111",
			LocalRepoUUID: "de41cafe-0000-4000-8000-00000000beef",
			ClosureDigest: "2222222222222222222222222222222222222222222222222222222222222222",
		},
	}
}

func dlsMountState() PrivateDispatchMountState {
	return PrivateDispatchMountState{
		SelectedWorksource: "ws-test",
		Worksources: []PrivateDispatchWorksource{{
			WorksourceID: "ws-test", Kind: "repo", Status: "active",
			ProfilePresent: true, ProfileID: "default", WorkspaceRoot: "/srv/ws-test",
		}},
		SubjectAcceptedCompletionSeal: &substrate.DispatchAcceptedCompletionSeal{
			RunID:             "0123456789abcdef",
			CompletionRequest: "fedcba9876543210",
			ObjectFormat:      "sha1",
		},
	}
}

// ---------------------------------------------------------------------------
// The candidate projection.
//
// A landing occupies the same canonicalCandidate slot a spawn does, but it has
// no run and no role — it claims no lease and opens no Run, and `runs.role` has
// no landing member. Asserting the two fields are EMPTY is the point: a landing
// that ever acquired a run id here would be a reap target with no container,
// and one that named a role would log as that role's spawn.
// ---------------------------------------------------------------------------

func TestLandingCandidateProjectionNamesNoRunAndNoRole(t *testing.T) {
	c := landingCandidateProjection(7)
	if c.Kind != "landing" {
		t.Fatalf("candidate kind = %q, want landing", c.Kind)
	}
	if c.RunID != "" {
		t.Fatalf("landing candidate allocated run id %q; it opens no Run and would become a reap target with no container", c.RunID)
	}
	if c.Role != "" {
		t.Fatalf("landing candidate named role %q; runs.role has no landing member", c.Role)
	}
	if c.SubjectID == nil || *c.SubjectID != 7 {
		t.Fatalf("landing candidate subject = %v, want 7", c.SubjectID)
	}
	// Normalized like every other candidate so the canonical bytes never carry
	// a null where an empty collection belongs.
	if c.ProposedPool == nil || c.Wave == nil || c.DedupeTitles == nil {
		t.Fatalf("landing candidate left a nil collection: %+v", c)
	}
	if len(c.ProposedPool) != 0 || len(c.Wave) != 0 || len(c.DedupeTitles) != 0 {
		t.Fatalf("landing candidate carried spawn-only pool state: %+v", c)
	}
}

// ---------------------------------------------------------------------------
// The frozen tuple.
// ---------------------------------------------------------------------------

func TestLandingTupleProjectionCarriesBothTargetRefs(t *testing.T) {
	task := dlsTask()
	task.TargetRef = "refs/heads/main"
	task.Sealed.TargetRef = "refs/heads/release" // diverged on purpose

	got, err := landingTupleProjection(task, dlsMountState(), "0f1e2d3c4b5a6978")
	if err != nil {
		t.Fatalf("landingTupleProjection: %v", err)
	}
	if got.TargetRef != "refs/heads/main" {
		t.Fatalf("tuple target ref = %q, want the task's current ref", got.TargetRef)
	}
	if got.AssignedTargetRef != "refs/heads/release" {
		t.Fatalf("tuple assigned target ref = %q, want the assignment's frozen ref", got.AssignedTargetRef)
	}
}

func TestLandingTupleProjectionTakesTheBranchFromTheAssignment(t *testing.T) {
	// `tasks.branch` is empty for every sealed row by construction — its only
	// writer is closed to assigned tasks, which is what makes the two landing
	// lanes partition. The assignment is the sealed lane's branch home.
	got, err := landingTupleProjection(dlsTask(), dlsMountState(), "0f1e2d3c4b5a6978")
	if err != nil {
		t.Fatalf("landingTupleProjection: %v", err)
	}
	if got.Branch != "mc/task-7" {
		t.Fatalf("tuple branch = %q, want the assignment's branch", got.Branch)
	}
	if got.TaskRootKey != "16777220:424242" ||
		got.ObjectFormat != "sha1" ||
		got.PinnedBaseSHA != "1111111111111111111111111111111111111111" ||
		got.LocalRepoUUID != "de41cafe-0000-4000-8000-00000000beef" {
		t.Fatalf("tuple dropped an assignment pin: %+v", got)
	}
	if got.ApprovedRunID != "0123456789abcdef" || got.ApprovedRequestID != "fedcba9876543210" {
		t.Fatalf("tuple dropped the accepted seal's identity: %+v", got)
	}
	if got.VerifiedSHA != "3333333333333333333333333333333333333333" {
		t.Fatalf("tuple verified sha = %q", got.VerifiedSHA)
	}
	if got.LandingID != "0f1e2d3c4b5a6978" {
		t.Fatalf("tuple landing id = %q", got.LandingID)
	}
}

func TestLandingTupleProjectionRefusesMissingEvidence(t *testing.T) {
	// Both halves are load-bearing and neither has a safe default. Without the
	// assignment there is no branch, base, or repo identity to land against;
	// without the accepted seal the landing id has no approved-run input, and an
	// id derived over empty strings would collide across every task in the
	// deployment.
	noAssignment := dlsTask()
	noAssignment.Sealed = nil
	if _, err := landingTupleProjection(noAssignment, dlsMountState(), "0f1e2d3c4b5a6978"); err == nil {
		t.Fatal("projected a landing tuple with no closure assignment")
	}

	noSeal := dlsMountState()
	noSeal.SubjectAcceptedCompletionSeal = nil
	if _, err := landingTupleProjection(dlsTask(), noSeal, "0f1e2d3c4b5a6978"); err == nil {
		t.Fatal("projected a landing tuple with no accepted completion seal")
	}
}

// ---------------------------------------------------------------------------
// Lane membership.
//
// sealedLandingSubject is the commit-side re-assertion that the row prepare
// froze is still the row the sealed lane owes a landing to. It re-checks the
// whole predicate rather than the id alone, so a row that was approved at
// prepare and blocked by the time commit runs is a candidate mismatch, not a
// landing.
// ---------------------------------------------------------------------------

func TestSealedLandingSubjectSelectsOnlyPendingSealedRows(t *testing.T) {
	base := dlsTask()
	rec := dispatch.Records{Tasks: []dispatch.Task{base}}
	if got, ok := sealedLandingSubject(rec, 7); !ok || got.ID != 7 {
		t.Fatalf("sealed landing subject = %+v ok=%v, want task 7", got, ok)
	}
	if _, ok := sealedLandingSubject(rec, 8); ok {
		t.Fatal("selected a task id that is not in the records")
	}

	for name, mutate := range map[string]func(*dispatch.Task){
		"blocked":            func(t *dispatch.Task) { t.Blocked = true },
		"archived":           func(t *dispatch.Task) { t.Archived = true },
		"not approved":       func(t *dispatch.Task) { t.Decision = "" },
		"not packaged":       func(t *dispatch.Task) { t.Status = dispatch.StatusSeeded },
		"no assignment":      func(t *dispatch.Task) { t.Sealed = nil },
		"no verified sha":    func(t *dispatch.Task) { t.VerifiedSHA = "" },
		"no target ref":      func(t *dispatch.Task) { t.TargetRef = "" },
		"carries both homes": func(t *dispatch.Task) { t.Branch = "mc/task-7" },
	} {
		t.Run(name, func(t2 *testing.T) {
			task := dlsTask()
			mutate(&task)
			if _, ok := sealedLandingSubject(dispatch.Records{Tasks: []dispatch.Task{task}}, 7); ok {
				t2.Fatalf("%s row was admitted to the sealed landing lane", name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The prepare leg.
//
// Split into a pure core over the frozen mount state and a thin loading
// wrapper, so the freezing behaviour — which is the part with teeth — is
// testable without standing up an accepted-seal spine fixture. The loader half
// is already pinned by TestLoadDispatchLandingMountStateMatchesSubjectSpawn.
// ---------------------------------------------------------------------------

func dlsSelection(t dispatch.Task) spineSelection {
	return spineSelection{
		lk:     dispatch.Lock{},
		tun:    tunables{},
		rec:    dispatch.Records{Tasks: []dispatch.Task{t}},
		homies: []homieCandidateState{},
		action: dispatch.Action{Kind: dispatch.KindLand, Land: &dispatch.Land{
			TaskID: t.ID, Branch: t.Sealed.Branch,
			VerifiedSHA: t.VerifiedSHA, TargetRef: t.TargetRef,
		}},
	}
}

func dlsPrepare(t *testing.T, task dispatch.Task, st PrivateDispatchMountState) preparedDispatch {
	t.Helper()
	prepared, err := landingPrepareFromState(defaultDispatchProtocolIdentity,
		"de41cafe-0000-4000-8000-000000000001", "00112233445566ff", dlsSelection(task), task, st)
	if err != nil {
		t.Fatalf("landingPrepareFromState: %v", err)
	}
	return prepared
}

func TestDispatchLandingPrepareReturnsALandingNeverACandidate(t *testing.T) {
	prepared := dlsPrepare(t, dlsTask(), dlsMountState())

	if prepared.landing == nil {
		t.Fatal("prepare returned no landing")
	}
	// The whole safety argument of this lane: a landing is never a
	// preparedCandidate, so the spawn seam's unguarded cand.spawn dereferences
	// stay unreachable from it by type.
	if prepared.candidate != nil {
		t.Fatal("prepare returned a landing AS a spawn candidate; every unguarded cand.spawn deref is now reachable with a nil Spawn")
	}
	if prepared.final != nil {
		t.Fatal("prepare returned a landing AND a final effect; a landing owes attest and commit")
	}
	if !validLowercaseHex(prepared.landing.landingID, landingIDHexLen) {
		t.Fatalf("landing id %q is not %d lowercase hex", prepared.landing.landingID, landingIDHexLen)
	}
	if prepared.landing.workspaceRoot != "/srv/ws-test" {
		t.Fatalf("landing workspace root = %q", prepared.landing.workspaceRoot)
	}
	// The capture inputs must be the tuple, not a second copy assembled from
	// somewhere else; attest passes them straight through to captureLandingPlan.
	if prepared.landing.inputs.LandingID != prepared.landing.landingID {
		t.Fatalf("capture inputs carry a different landing id than the prepared landing: %q vs %q",
			prepared.landing.inputs.LandingID, prepared.landing.landingID)
	}
	if prepared.landing.inputs.TaskID != 7 ||
		prepared.landing.inputs.TargetRef != "refs/heads/main" ||
		prepared.landing.inputs.VerifiedSHA != "3333333333333333333333333333333333333333" ||
		prepared.landing.inputs.PinnedBaseSHA != "1111111111111111111111111111111111111111" {
		t.Fatalf("capture inputs dropped a frozen fact: %+v", prepared.landing.inputs)
	}
}

func TestDispatchLandingPrepareDerivesTheIdDeterministically(t *testing.T) {
	// ADR-017:753-756 requires a retry to match its exact action trailer, and
	// this lane's trailer is the landing id. A per-attempt id would leave a
	// crashed attempt's merge unrecognizable to its own successor.
	first := dlsPrepare(t, dlsTask(), dlsMountState())
	second := dlsPrepare(t, dlsTask(), dlsMountState())
	if first.landing.landingID != second.landing.landingID {
		t.Fatalf("landing id is not stable across attempts: %q then %q",
			first.landing.landingID, second.landing.landingID)
	}

	// ...but it must separate deployments and subjects, or two tasks would
	// share a container name and a MERGE_MSG trailer.
	other, err := landingPrepareFromState(defaultDispatchProtocolIdentity,
		"de41cafe-0000-4000-8000-000000000002", "00112233445566ff",
		dlsSelection(dlsTask()), dlsTask(), dlsMountState())
	if err != nil {
		t.Fatalf("landingPrepareFromState: %v", err)
	}
	if other.landing.landingID == first.landing.landingID {
		t.Fatal("two deployments derived the same landing id")
	}
}

func TestDispatchLandingPrepareFreezesTheTupleUnderTheToken(t *testing.T) {
	base := dlsPrepare(t, dlsTask(), dlsMountState())

	for name, mutate := range map[string]func(*dispatch.Task){
		"verified sha":        func(t *dispatch.Task) { t.VerifiedSHA = "9999999999999999999999999999999999999999" },
		"target ref":          func(t *dispatch.Task) { t.TargetRef = "refs/heads/other" },
		"assigned target ref": func(t *dispatch.Task) { t.Sealed.TargetRef = "refs/heads/other" },
		"branch":              func(t *dispatch.Task) { t.Sealed.Branch = "mc/task-8" },
		"pinned base":         func(t *dispatch.Task) { t.Sealed.BaseSHA = "9999999999999999999999999999999999999999" },
		"closure digest":      func(t *dispatch.Task) { t.Sealed.ClosureDigest = "9" },
		"task root key":       func(t *dispatch.Task) { t.Sealed.TaskRootKey = "16777220:999999" },
		"local repo uuid":     func(t *dispatch.Task) { t.Sealed.LocalRepoUUID = "de41cafe-0000-4000-8000-00000000feed" },
		"object format":       func(t *dispatch.Task) { t.Sealed.ObjectFormat = "sha256" },
	} {
		t.Run(name, func(t2 *testing.T) {
			task := dlsTask()
			mutate(&task)
			drifted := dlsPrepare(t2, task, dlsMountState())
			if drifted.landing.token == base.landing.token {
				t2.Fatalf("drifting the %s left the preparation token unchanged; commit could not detect it", name)
			}
		})
	}

	// The approved seal's identity is on the mount state rather than the task,
	// and it is equally inside the token.
	st := dlsMountState()
	st.SubjectAcceptedCompletionSeal.RunID = "aaaaaaaaaaaaaaaa"
	if dlsPrepare(t, dlsTask(), st).landing.token == base.landing.token {
		t.Fatal("drifting the approved run id left the preparation token unchanged")
	}
}

func TestDispatchLandingPrepareRefusesAnUnresolvableWorkspace(t *testing.T) {
	st := dlsMountState()
	st.Worksources[0].WorkspaceRoot = ""
	if _, err := landingPrepareFromState(defaultDispatchProtocolIdentity,
		"de41cafe-0000-4000-8000-000000000001", "00112233445566ff",
		dlsSelection(dlsTask()), dlsTask(), st); err == nil {
		t.Fatal("prepared a landing with no workspace root; attest would resolve the host anchors against the process working directory")
	}
}

// ---------------------------------------------------------------------------
// The attest leg.
//
// The landing attests the operator's Git views and NOTHING ELSE. It reads no
// routing.md, resolves no role, and has no path to CodeRoutingInvalid.
// ADR-016:53-60 is explicit that a land candidate "instead attests ADR-017's
// exact task-store/real-repository Git views ... without a gateway probe", and
// spec §7:231 puts no agent in the landing path, so there is no role to route.
// ---------------------------------------------------------------------------

const dlsUUID = "de41cafe-0000-4000-8000-000000000001"

// dlsHome is an MC_HOME carrying only the deployment identity mirror — the one
// host file both attest legs read before anything else.
func dlsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, deploymentUUIDFilename), []byte(dlsUUID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// dlsPreparedOverRepo prepares a landing whose frozen inputs describe a real
// git Worksource, so attest's capture can actually succeed.
func dlsPreparedOverRepo(t *testing.T) (string, preparedDispatch) {
	t.Helper()
	home := dlsHome(t)
	ws, _ := lcRepo(t, "7")

	task := dlsTask()
	task.TargetRef = "main"
	task.Sealed.TargetRef = "main"
	task.VerifiedSHA = strings.Repeat("a", 40)
	task.Sealed.BaseSHA = strings.Repeat("c", 40)
	task.Sealed.ClosureDigest = strings.Repeat("d", 64)
	task.Sealed.LocalRepoUUID = "0f9c1e2a-3b4c-4d5e-8f60-112233445566"

	st := dlsMountState()
	st.Worksources[0].WorkspaceRoot = ws

	prepared, err := landingPrepareFromState(defaultDispatchProtocolIdentity,
		dlsUUID, "00112233445566ff", dlsSelection(task), task, st)
	if err != nil {
		t.Fatalf("landingPrepareFromState: %v", err)
	}
	return home, prepared
}

func TestDispatchAttestLandingCarriesAValidMountPlan(t *testing.T) {
	home, prepared := dlsPreparedOverRepo(t)
	attested, err := dispatchAttestLanding(home, prepared)
	if err != nil {
		t.Fatalf("dispatchAttestLanding: %v", err)
	}
	if attested.refusal != nil {
		t.Fatalf("attest over a sound repository refused: %+v", attested.refusal)
	}
	if attested.mountPlan == nil || attested.mountPlan.Landing == nil {
		t.Fatalf("attest produced no landing plan: %+v", attested.mountPlan)
	}
	// The plan must satisfy the SAME validator the commit side and the resident
	// apply. A zero Version or a nil Entries is hard-refused by both, and a
	// producer that emitted one would surface only once the lane is armed.
	if err := validatePrivateMountPlan(attested.mountPlan); err != nil {
		t.Fatalf("the attested landing plan does not satisfy validatePrivateMountPlan: %v", err)
	}
	if len(attested.mountPlan.Entries) != 0 {
		t.Fatalf("landing plan carried mount entries: %+v", attested.mountPlan.Entries)
	}
}

func TestDispatchAttestLandingNeverReadsRouting(t *testing.T) {
	home, prepared := dlsPreparedOverRepo(t)
	// Bytes that routing.Parse cannot possibly accept. A spawn attesting this
	// home returns a routing-invalid deployment-health refusal; a landing must
	// not notice it at all.
	if err := os.WriteFile(filepath.Join(home, "routing.md"), []byte("\x00 not routing \x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	attested, err := dispatchAttestLanding(home, prepared)
	if err != nil {
		t.Fatalf("dispatchAttestLanding: %v", err)
	}
	if attested.refusal != nil {
		t.Fatalf("broken routing suppressed a landing: %+v — routing brokenness is never the landing's fault", attested.refusal)
	}
	if attested.route != (routing.Route{}) || attested.routingDigest != "" {
		t.Fatalf("landing attest resolved a route it has no use for: %+v / %q", attested.route, attested.routingDigest)
	}
}

func TestDispatchAttestLandingRefusesDivergedTargetRefAsHealth(t *testing.T) {
	home, prepared := dlsPreparedOverRepo(t)
	prepared.landing.assignedRef = "refs/heads/somewhere-else"

	attested, err := dispatchAttestLanding(home, prepared)
	if err != nil {
		t.Fatalf("dispatchAttestLanding returned an error rather than a classified refusal: %v", err)
	}
	if attested.refusal == nil {
		t.Fatal("a landing whose assignment ref diverged from the task's was planned anyway")
	}
	class, cerr := refusal.Classify(*attested.refusal)
	if cerr != nil || class != refusal.ClassHealth {
		t.Fatalf("diverged-ref refusal classified %v/%v, want health — a candidate-class refusal here would BLOCK the task", class, cerr)
	}
	if attested.mountPlan != nil {
		t.Fatal("a refused landing still carried a plan")
	}
}

func TestDispatchAttestLandingCaptureFailureIsHealthNotError(t *testing.T) {
	// Runtime, mount and shared-Git applicability failures are deployment
	// health and RETAIN the pending landing. Only the fixed mc-land program's
	// semantic Git refusal blocks, and it reports through `mc land report
	// failure` — never from here. An error return would be worse than either:
	// it would surface as a command failure with no classified consequence.
	home := dlsHome(t)
	task := dlsTask()
	st := dlsMountState()
	st.Worksources[0].WorkspaceRoot = t.TempDir() // a real path, but no repository
	prepared, err := landingPrepareFromState(defaultDispatchProtocolIdentity,
		dlsUUID, "00112233445566ff", dlsSelection(task), task, st)
	if err != nil {
		t.Fatalf("landingPrepareFromState: %v", err)
	}

	attested, aerr := dispatchAttestLanding(home, prepared)
	if aerr != nil {
		t.Fatalf("a capture failure errored the command instead of classifying: %v", aerr)
	}
	if attested.refusal == nil {
		t.Fatal("a landing over a non-repository was planned anyway")
	}
	class, cerr := refusal.Classify(*attested.refusal)
	if cerr != nil || class != refusal.ClassHealth {
		t.Fatalf("capture-failure refusal classified %v/%v, want health", class, cerr)
	}
}

func TestDispatchAttestLandingStillOwesTheDeploymentFence(t *testing.T) {
	// The landing reads no routing, but it owes the same deployment-identity
	// fence every attest leg owes. Dropping it alongside the routing read is
	// the easy mistake this asserts against.
	home, prepared := dlsPreparedOverRepo(t)
	if err := os.WriteFile(filepath.Join(home, deploymentUUIDFilename),
		[]byte("de41cafe-0000-4000-8000-999999999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatchAttestLanding(home, prepared); err == nil {
		t.Fatal("a deployment identity that moved between prepare and attest was accepted")
	}
}

// ---------------------------------------------------------------------------
// The pre-commit recheck.
//
// D1's immediate pre-commit host fence is shared: both legs run their own
// attest a second time and compare the canonical attestation. For a landing the
// comparison covers PreMergeSHA, because canonicalPrivateAttestation already
// carries the whole mount plan — so an operator commit that moves the target
// tip between the two attests abandons the prepared landing rather than landing
// onto a preimage that no longer exists.
// ---------------------------------------------------------------------------

func TestLandingRecheckAcceptsAnUnmovedHost(t *testing.T) {
	home, prepared := dlsPreparedOverRepo(t)
	first, err := dispatchAttestLanding(home, prepared)
	if err != nil {
		t.Fatalf("dispatchAttestLanding: %v", err)
	}
	got := dispatchRecheckAttestation(home, prepared, first)
	if got.refusal != nil {
		t.Fatalf("an unmoved host was refused as stale: %+v", got.refusal)
	}
	if got.mountPlan == nil || got.mountPlan.Landing == nil {
		t.Fatal("recheck dropped the landing plan")
	}
}

func TestLandingRecheckRefusesStaleOnMovedTargetTip(t *testing.T) {
	home := dlsHome(t)
	ws, _ := lcRepo(t, "7")

	task := dlsTask()
	task.TargetRef = "main"
	task.Sealed.TargetRef = "main"
	task.VerifiedSHA = strings.Repeat("a", 40)
	task.Sealed.BaseSHA = strings.Repeat("c", 40)
	task.Sealed.ClosureDigest = strings.Repeat("d", 64)
	task.Sealed.LocalRepoUUID = "0f9c1e2a-3b4c-4d5e-8f60-112233445566"

	st := dlsMountState()
	st.Worksources[0].WorkspaceRoot = ws
	prepared, err := landingPrepareFromState(defaultDispatchProtocolIdentity,
		dlsUUID, "00112233445566ff", dlsSelection(task), task, st)
	if err != nil {
		t.Fatalf("landingPrepareFromState: %v", err)
	}
	first, err := dispatchAttestLanding(home, prepared)
	if err != nil || first.refusal != nil {
		t.Fatalf("first attest = %+v err %v", first.refusal, err)
	}

	// The operator commits on the target between attest and commit.
	writeFile(t, filepath.Join(ws, "app.txt"), "v2\n")
	srcGit(t, ws, "add", "-A")
	srcGit(t, ws, "commit", "-qm", "operator c3")

	got := dispatchRecheckAttestation(home, prepared, first)
	if got.refusal == nil {
		t.Fatal("the target tip moved between attest and commit and the landing was committed anyway")
	}
	if got.refusal.Code != refusal.CodeStale {
		t.Fatalf("moved-tip refusal = %+v, want stale", got.refusal)
	}
}

func TestLandingRecheckDoesNotReattestAsASpawn(t *testing.T) {
	// The selector is two lines and easy to get backwards. If a landing were
	// re-attested through dispatchAttest, it would nil-deref cand.spawn — so
	// the assertion is simply that recheck completes at all, on a home whose
	// routing.md is unreadable and which therefore has nothing a spawn attest
	// could succeed against.
	home, prepared := dlsPreparedOverRepo(t)
	first, err := dispatchAttestLanding(home, prepared)
	if err != nil {
		t.Fatalf("dispatchAttestLanding: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "routing.md"), []byte("\x00 not routing \x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := dispatchRecheckAttestation(home, prepared, first); got.refusal != nil {
		t.Fatalf("recheck of a landing consulted routing: %+v", got.refusal)
	}
}
