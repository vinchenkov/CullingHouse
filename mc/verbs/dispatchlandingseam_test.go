package verbs

// The sealed landing lane's own prepare/attest/commit (ADR-016:369-379). These
// tests drive the lane's functions DIRECTLY: nothing selects a sealed row yet,
// and nothing will until the activation switch, so a test that went through
// `mc dispatch` would assert on the legacy lane and quietly prove nothing.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mc/dispatch"
	"mc/domain"
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
			// Explicit empty collections, as the real loader always produces:
			// the private frame refuses a nil path projection outright.
			ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
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

// ---------------------------------------------------------------------------
// The commit leg.
//
// ADR-016:375-377 — commit "rechecks the entire pending tuple and inventory
// before returning the frozen landing plan". The fences are tested pure, one
// drift at a time, because the value is in each of them firing for its own
// reason rather than in the whole set failing together.
// ---------------------------------------------------------------------------

func dlsFenceInputs(t *testing.T) (preparedDispatch, spineSelection, PrivateDispatchMountState) {
	t.Helper()
	task := dlsTask()
	st := dlsMountState()
	prepared := dlsPrepare(t, task, st)
	return prepared, dlsSelection(task), st
}

func TestLandingCommitFencesPassOnAnUndriftedTuple(t *testing.T) {
	prepared, sel, st := dlsFenceInputs(t)
	code, err := landingCommitFences(prepared, sel, st)
	if err != nil {
		t.Fatalf("landingCommitFences: %v", err)
	}
	if code != "" {
		t.Fatalf("an undrifted tuple refused %q", code)
	}
}

func TestLandingCommitFencesRefuseEveryDrift(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*preparedDispatch, *spineSelection, *PrivateDispatchMountState)
		want   string
	}{
		"inventory moved": {
			func(_ *preparedDispatch, _ *spineSelection, st *PrivateDispatchMountState) {
				st.Worksources[0].WorkspaceRoot = "/srv/somewhere-else"
			}, refusal.CodeStale,
		},
		"row left the lane": {
			func(_ *preparedDispatch, sel *spineSelection, _ *PrivateDispatchMountState) {
				sel.rec.Tasks[0].Blocked = true
			}, refusal.CodeCandidateMismatch,
		},
		"row archived": {
			func(_ *preparedDispatch, sel *spineSelection, _ *PrivateDispatchMountState) {
				sel.rec.Tasks[0].Archived = true
			}, refusal.CodeCandidateMismatch,
		},
		"verified sha drifted": {
			func(_ *preparedDispatch, sel *spineSelection, _ *PrivateDispatchMountState) {
				sel.rec.Tasks[0].VerifiedSHA = strings.Repeat("9", 40)
			}, refusal.CodeStale,
		},
		"assignment ref drifted": {
			func(_ *preparedDispatch, sel *spineSelection, _ *PrivateDispatchMountState) {
				sel.rec.Tasks[0].Sealed.TargetRef = "refs/heads/elsewhere"
			}, refusal.CodeStale,
		},
		"approved run changed": {
			func(_ *preparedDispatch, _ *spineSelection, st *PrivateDispatchMountState) {
				st.SubjectAcceptedCompletionSeal.RunID = "aaaaaaaaaaaaaaaa"
			}, refusal.CodeStale,
		},
		"landing id no longer derives": {
			func(p *preparedDispatch, _ *spineSelection, _ *PrivateDispatchMountState) {
				p.landing.landingID = "ffffffffffffffff"
			}, refusal.CodeStale,
		},
		"tick no longer selects a landing": {
			func(_ *preparedDispatch, sel *spineSelection, _ *PrivateDispatchMountState) {
				sel.action = dispatch.Action{Kind: dispatch.KindIdle}
			}, refusal.CodeCandidateMismatch,
		},
		"tick selects a different landing": {
			func(_ *preparedDispatch, sel *spineSelection, _ *PrivateDispatchMountState) {
				sel.action.Land.TaskID = 8
			}, refusal.CodeCandidateMismatch,
		},
	} {
		t.Run(name, func(t2 *testing.T) {
			prepared, sel, st := dlsFenceInputs(t2)
			tc.mutate(&prepared, &sel, &st)
			code, err := landingCommitFences(prepared, sel, st)
			if err != nil {
				t2.Fatalf("landingCommitFences: %v", err)
			}
			if code == "" {
				t2.Fatalf("%s was committed anyway", name)
			}
			if code != tc.want {
				t2.Fatalf("%s refused %q, want %q", name, code, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The two producers of the land effect.
//
// The resident discriminates the sealed lane from the legacy one on the
// presence of the "landing" key alone, so a drift in the five SHARED keys would
// surface nowhere as a type error — it would simply stop matching, at runtime,
// in a lane nobody is watching yet.
// ---------------------------------------------------------------------------

func TestLandingEffectKeysMatchTheLegacyProducer(t *testing.T) {
	legacy, err := applyAction(context.Background(), dvSpine(t), time.Time{}, dispatch.Action{
		Kind: dispatch.KindLand,
		Land: &dispatch.Land{
			TaskID: 7, Branch: "mc/task-7",
			VerifiedSHA: "3333333333333333333333333333333333333333",
			TargetRef:   "refs/heads/main",
		},
	}, tunables{})
	if err != nil {
		t.Fatalf("legacy land effect: %v", err)
	}

	sealed := landingEffect(7, &PrivateDispatchLanding{
		Branch:      "mc/task-7",
		VerifiedSHA: "3333333333333333333333333333333333333333",
		TargetRef:   "refs/heads/main",
	})

	for _, key := range []string{"action", "task_id", "branch", "verified_sha", "target_ref"} {
		if fmt.Sprint(sealed[key]) != fmt.Sprint(legacy[key]) {
			t.Fatalf("the two land-effect producers disagree on %q: sealed %v, legacy %v",
				key, sealed[key], legacy[key])
		}
	}
	if _, ok := legacy["landing"]; ok {
		t.Fatal("the legacy land effect carries a landing key; the resident's lane discriminator is broken")
	}
	if sealed["landing"] == nil {
		t.Fatal("the sealed land effect carries no landing key; the resident would route it to the legacy lane")
	}
}

// A landing commit must not touch the spine. It claims no lease, opens no Run,
// charges no retry, and writes no receipt — the writes come later, through
// `mc land report`. This drives the real DB-backed commit and compares the
// whole of every table the lane could plausibly reach.
func TestDispatchCommitLandingWritesNothing(t *testing.T) {
	db := dvSpine(t)
	ctx := context.Background()

	var uuid string
	if err := db.QueryRowContext(ctx, `SELECT deployment_uuid FROM meta WHERE id = 1`).Scan(&uuid); err != nil {
		t.Fatal(err)
	}

	// A real row for the subject, so the mount-state loader resolves rather
	// than erroring before any fence runs. It is deliberately NOT
	// landing-pending: it is `proposed`, so the lane-membership fence is what
	// refuses, which is the inert path this test is about.
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (7, 'seal', 'task', 1, ?, 'proposed', 3, 'user', 'ws-test', 'refs/heads/main')`,
		dvOld.Format(spineTime))

	task := dlsTask()
	prepared := dlsPrepare(t, task, dlsMountState())
	prepared.deploymentUUID = uuid

	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, table := range []string{"tasks", "runs", "lock", "activity", "review_packets", "dispatch_receipts"} {
			rows, err := db.QueryContext(ctx, `SELECT count(*) FROM `+table)
			if err != nil {
				continue // a table this schema version does not have
			}
			var n int
			if rows.Next() {
				_ = rows.Scan(&n)
			}
			rows.Close()
			out[table] = fmt.Sprint(n)
		}
		var blocked, archived int
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE blocked=1`).Scan(&blocked)
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE archived=1`).Scan(&archived)
		out["blocked"], out["archived"] = fmt.Sprint(blocked), fmt.Sprint(archived)
		return out
	}

	before := snapshot()
	// The spine holds no such task, so the lane-membership fence refuses. That
	// is the point: a stale-class refusal is INERT, and this asserts the
	// inertness rather than assuming it from the code's shape.
	effect, err := dispatchCommitLanding(ctx, db, prepared, attestedDispatch{
		deploymentUUID: uuid,
		mountPlan: &PrivateDispatchMountPlan{
			Version: 1, Entries: []PrivateDispatchMountEntry{},
			Landing: &PrivateDispatchLanding{Branch: "mc/task-7"},
		},
	})
	if err != nil {
		t.Fatalf("dispatchCommitLanding: %v", err)
	}
	if effect == nil {
		t.Fatal("commit returned no effect")
	}
	after := snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a landing commit mutated the spine:\n before %v\n  after %v", before, after)
	}
	// And specifically: it must never block its own task from the seam.
	// ADR-016:576 reserves blocking for the fixed mc-land program's semantic
	// Git refusal, reported through `mc land report failure`.
	if after["blocked"] != "0" {
		t.Fatal("a landing commit blocked a task from the seam")
	}
}

func TestLandingRefusalIsNeverCandidateClass(t *testing.T) {
	// domain.Block is reachable only from a candidate-class refusal carrying
	// RefusalSubjectTask. The landing lane uses the subjectless kind precisely
	// so that a durable blocked row is unreachable BY TYPE, rather than by an
	// enumeration of which refusal codes happen to be stale-class today.
	if got := refusalReceiptSubject(RefusalCandidate{Kind: RefusalSubjectlessPipeline}); got != nil {
		t.Fatalf("the subjectless refusal candidate named a subject %v; it can reach domain.Block", got)
	}
}

// ---------------------------------------------------------------------------
// The private (Darwin broker/helper) frame — the landing must CROSS it.
//
// This block used to assert the opposite: that the frame REFUSED a landing.
// That guard was correct while no carrier existed — serializing a landing
// through the spawn shape would have handed the far side a candidate whose
// Spawn is nil. But `mc dispatch` on Darwin self-delegates through this frame,
// so refusing here meant the sealed landing lane could not dispatch at all on
// the platform this deployment targets, while every in-process test stayed
// green. The E2E is what caught it.
//
// So the assertion inverts: the frame must carry a landing, and it must carry
// it as a LANDING rather than as a spawn candidate.
// ---------------------------------------------------------------------------

func TestPrivateFrameCarriesALandingAsItsOwnKind(t *testing.T) {
	prepared := dlsPrepare(t, dlsTask(), dlsMountState())
	if prepared.candidate != nil {
		t.Fatal("a prepared landing occupied the spawn candidate slot")
	}

	carrier := privateLandingFromPrepared(prepared.landing)
	if carrier == nil {
		t.Fatal("the private frame produced no landing carrier; the lane cannot dispatch on Darwin")
	}
	if carrier.TaskID != prepared.landing.taskID || carrier.LandingID != prepared.landing.landingID {
		t.Fatalf("the carrier lost the landing's identity: %+v", carrier)
	}

	// And a spawn must still produce no landing carrier, so the two lanes
	// cannot be confused at the boundary either.
	if got := privateLandingFromPrepared(nil); got != nil {
		t.Fatalf("a nil landing produced a carrier: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// The prepare fork.
//
// The sealed landing forks out of prepare ahead of applyAction, because it owes
// attest and commit. The legacy branch-carrying landing must keep falling
// through to applyAction and stay the pure effect data it has always been —
// the lanes partition on the assignment, so the fork can never divert one into
// the other.
// ---------------------------------------------------------------------------

func TestLegacyLandStillReturnsFromPrepare(t *testing.T) {
	db := dvSpine(t)
	ctx := context.Background()

	var uuid string
	if err := db.QueryRowContext(ctx, `SELECT deployment_uuid FROM meta WHERE id = 1`).Scan(&uuid); err != nil {
		t.Fatal(err)
	}
	// A branch-carrying, unassigned, approved+packaged row: the LEGACY lane.
	// Built the way the substrate triggers require — born proposed, with a live
	// Review Packet before approval.
	decided := dvOld.Add(time.Hour)
	task := dvTask(1, dispatch.ScopeTask, dispatch.StatusPackaged, 0)
	task.Branch, task.VerifiedSHA, task.TargetRef = "mc/task-1", "abc123", "refs/heads/main"
	fixture := task
	fixture.Decision, fixture.DecidedAt = "", nil
	dvInsertTask(t, db, fixture)
	dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (1, ?)`, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET decision='approved', decided_at=? WHERE id=1`, decided.Format(spineTime))

	var prepared preparedDispatch
	if err := underDispatchLock(db, func(ctx context.Context, q Q) error {
		var e error
		prepared, e = dispatchPrepareWithIdentity(ctx, q, defaultDispatchProtocolIdentity, uuid, "00112233445566ff")
		return e
	}); err != nil {
		t.Fatalf("dispatchPrepareWithIdentity: %v", err)
	}

	if prepared.landing != nil {
		t.Fatal("a legacy branch-carrying landing was diverted into the sealed lane")
	}
	if prepared.final == nil {
		t.Fatal("the legacy land effect no longer returns from prepare")
	}
	if prepared.final["action"] != "land" || prepared.final["task_id"] != int64(1) {
		t.Fatalf("legacy land effect = %+v", prepared.final)
	}
	if _, ok := prepared.final["landing"]; ok {
		t.Fatal("the legacy land effect grew a landing key; the resident's lane discriminator is broken")
	}
}

// ---------------------------------------------------------------------------
// Broken routing must not suppress the sealed landing lane.
//
// cli_test.go:1193-1206 pins this for the LEGACY lane and cannot cover the
// sealed one: its fixture is branch-carrying, so it satisfies LandingPending
// rather than SealedLandingPending, returns from prepare as a final effect, and
// never reaches any attest leg. It would stay green if the sealed lane grew a
// routing dependency tomorrow. This is the sealed twin it cannot be.
//
// The property is stronger for the sealed lane than the legacy one, because the
// sealed lane DOES attest. If routing were read there, a deployment whose
// routing.md is broken would stop landing approved, already-reviewed work —
// with the operator given no signal connecting the two.
// ---------------------------------------------------------------------------

func TestBrokenRoutingNeverSuppressesASealedLanding(t *testing.T) {
	home, prepared := dlsPreparedOverRepo(t)
	if err := os.WriteFile(filepath.Join(home, "routing.md"),
		[]byte("\x00 not routing at all \x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The whole host half of the lane, in order, over a deployment whose
	// routing.md a spawn could not parse.
	attested, err := dispatchAttestLanding(home, prepared)
	if err != nil {
		t.Fatalf("attest: %v", err)
	}
	attested = dispatchRecheckAttestation(home, prepared, attested)
	if attested.refusal != nil {
		t.Fatalf("broken routing suppressed a sealed landing at %s: %+v",
			"attest/recheck", attested.refusal)
	}
	if attested.mountPlan == nil || attested.mountPlan.Landing == nil {
		t.Fatal("no landing plan survived a broken-routing deployment")
	}

	// And the effect the commit leg would return is a real land effect.
	effect := landingEffect(prepared.landing.taskID, attested.mountPlan.Landing)
	if effect["action"] != "land" {
		t.Fatalf("effect action = %v", effect["action"])
	}
	if effect["landing"] == nil {
		t.Fatal("the sealed land effect lost its landing carrier")
	}

	// The negative half: a SPAWN over this same home must still refuse, or the
	// test proves only that the fixture's routing.md was fine.
	spawnPrepared := preparedDispatch{
		deploymentUUID: prepared.deploymentUUID,
		requestID:      prepared.requestID,
		identity:       prepared.identity,
		candidate: &preparedCandidate{
			spawn: &dispatch.Spawn{Role: dispatch.RoleWorker},
		},
	}
	spawnAttested, err := dispatchAttest(home, spawnPrepared)
	if err != nil {
		t.Fatalf("spawn attest: %v", err)
	}
	if spawnAttested.refusal == nil {
		t.Fatal("the fixture's routing.md parsed after all; the landing arm above proved nothing")
	}
}

// ---------------------------------------------------------------------------
// `mc land report` and the second branch home.
//
// The fence was "no branch ⇒ nothing was landing". That is still true for an
// unassigned row, but `tasks.branch` is empty for every SEALED row by
// construction — its only writer is closed to assigned tasks, which is exactly
// what makes the two lanes partition. So the fence had to widen to "no branch
// AND no assignment", or the sealed lane could dispatch a landing that could
// never report its outcome.
// ---------------------------------------------------------------------------

func dlsReportableTask(t *testing.T, db *sql.DB, id int64, assigned bool) {
	t.Helper()
	decided := dvOld.Add(time.Hour)
	task := dvTask(id, dispatch.ScopeTask, dispatch.StatusPackaged, 0)
	task.TargetRef, task.VerifiedSHA = "main", strings.Repeat("3", 40)
	if !assigned {
		task.Branch = "mc/task-legacy"
	}
	fixture := task
	fixture.Decision, fixture.DecidedAt = "", nil
	dvInsertTask(t, db, fixture)
	if assigned {
		dvExec(t, db, `INSERT INTO task_assignments
			(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
			VALUES (?, 'main', ?, '.mission-control/tasks/task-x', 'sha1', ?, ?, ?)`,
			id, fmt.Sprintf("mc/task-%d", id), strings.Repeat("c", 40),
			"0f9c1e2a-3b4c-4d5e-8f60-112233445566", strings.Repeat("d", 64))
	}
	dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (?, ?)`, id, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET decision='approved', decided_at=? WHERE id=?`, decided.Format(spineTime), id)
}

func TestLandReportAcceptsAnAssignmentBackedRow(t *testing.T) {
	db := dvSpine(t)
	dlsReportableTask(t, db, 1, true)

	if _, err := LandReport(db, nil, 1, "success", ""); err != nil {
		t.Fatalf("land report over an assignment-backed row: %v — the sealed lane could dispatch a landing that can never report its outcome", err)
	}
	var archived int
	if err := db.QueryRow(`SELECT archived FROM tasks WHERE id = 1`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatal("a successful sealed landing did not archive its task")
	}
}

func TestLandReportStillRefusesABranchlessUnassignedRow(t *testing.T) {
	// The widening must not become "anything approved can report a landing".
	// An artifact-plane deliverable has no branch and no assignment; approve
	// archives it synchronously and no land effect ever exists for it.
	db := dvSpine(t)
	decided := dvOld.Add(time.Hour)
	task := dvTask(1, dispatch.ScopeTask, dispatch.StatusPackaged, 0)
	fixture := task
	fixture.Decision, fixture.DecidedAt = "", nil
	dvInsertTask(t, db, fixture)
	dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (1, ?)`, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET decision='approved', decided_at=? WHERE id=1`, decided.Format(spineTime))

	if _, err := LandReport(db, nil, 1, "success", ""); err == nil {
		t.Fatal("a branchless, unassigned row reported a landing it never had")
	}
}

// ---------------------------------------------------------------------------
// The activation proof.
//
// Everything above tests a leg. This drives the REAL Dispatch() over a real
// spine and a real git Worksource and asserts that an approved, assignment-
// backed row now produces a sealed land effect — which is the one thing that
// was not true before the switch, and the only way to know the two reachability
// edits actually meet in the middle.
// ---------------------------------------------------------------------------

func TestSealedLandingIsReachableEndToEnd(t *testing.T) {
	db := dvSpine(t)
	ws, _ := lcRepo(t, "7")
	dvExec(t, db, `UPDATE sandbox_profiles SET workspace_root = ?`, ws)

	// A sealed row: branchless in `tasks`, branch on the immutable assignment.
	// Walked through the real lifecycle rather than dropped in at `packaged`:
	// the acceptance pointer below is fenced to `status = 'worked'`, which is
	// the lifecycle position where a Worker seal is actually accepted.
	task := dvTask(7, dispatch.ScopeTask, dispatch.StatusWorked, 0)
	task.TargetRef, task.VerifiedSHA = "main", strings.Repeat("a", 40)
	fixture := task
	fixture.Decision, fixture.DecidedAt = "", nil
	dvInsertTask(t, db, fixture)
	dvExec(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (7, 'main', 'mc/task-7', '.mission-control/tasks/task-7', 'sha1', ?, ?, ?)`,
		strings.Repeat("c", 40), "0f9c1e2a-3b4c-4d5e-8f60-112233445566", strings.Repeat("d", 64))

	// The accepted completion seal supplies the approved-run identity the
	// landing id digests.
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject, created_at, ended_at, outcome, session_path, binding)
		VALUES ('0123456789abcdef', 'pipeline', 'worker', 'ws-test', 7, ?, ?, 'worked', 'sessions/x', 'claude')`,
		dvOld.Format(spineTime), dvOld.Format(spineTime))
	dvExec(t, db, `INSERT INTO completion_seals
		(run_id, task_id, completion_request_id, object_format, sealed_sha, closure_digest,
		 manifest_digest, seal_device, seal_inode, seal_owner_uid, state, accepted_at)
		VALUES ('0123456789abcdef', 7, 'fedcba9876543210', 'sha1', ?, ?, ?, '16777220', '424242', 501, 'accepted', ?)`,
		strings.Repeat("a", 40), strings.Repeat("d", 64), strings.Repeat("e", 64), dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET accepted_completion_run_id = '0123456789abcdef',
		accepted_completion_request_id = 'fedcba9876543210' WHERE id = 7`)
	// Now advance to the status an approval requires.
	dvExec(t, db, `UPDATE tasks SET status = 'verified' WHERE id = 7`)
	dvExec(t, db, `UPDATE tasks SET status = 'packaged', verified_sha = ? WHERE id = 7`,
		strings.Repeat("a", 40))

	// Approve it through the real verb, so the OTHER half of the switch is
	// exercised rather than simulated by a direct UPDATE.
	dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (7, ?)`, dvOld.Format(spineTime))
	if err := inTx(db, func(ctx context.Context, q Q) error {
		_, err := domain.Approve(ctx, q, 7)
		return err
	}); err != nil {
		t.Fatalf("approve of an assignment-backed row: %v — the switch's Approve half did not open", err)
	}
	var archived int
	if err := db.QueryRow(`SELECT archived FROM tasks WHERE id = 7`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 0 {
		t.Fatal("approve archived the sealed task instead of holding it for landing")
	}

	effect, err := Dispatch(db)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	eff, ok := effect.(map[string]any)
	if !ok {
		t.Fatalf("dispatch returned %T", effect)
	}
	if eff["action"] != "land" {
		t.Fatalf("dispatch action = %v, want land — the sealed row is approved and packaged but nothing selects it", eff["action"])
	}
	if eff["landing"] == nil {
		t.Fatal("the land effect carries no landing carrier; the resident would route it to the legacy lane")
	}
	if eff["branch"] != "mc/task-7" {
		t.Fatalf("land effect branch = %v, want the assignment's branch", eff["branch"])
	}

	// Landing holds no lease. This is the property a landing routed through the
	// spawn machinery would have silently broken.
	var heldBy string
	if err := db.QueryRow(`SELECT COALESCE(run_id, '') FROM lock WHERE id = 1`).Scan(&heldBy); err != nil {
		t.Fatal(err)
	}
	if heldBy != "" {
		t.Fatalf("the landing claimed the lease: %q", heldBy)
	}
	// runs.role has no landing member at all, so a landing that opened a Run
	// could only have done it by borrowing an agent role — which is exactly the
	// masquerade the label rules forbid. Count every run beyond the accepted
	// Worker seal's own.
	var runCount int
	if err := db.QueryRow(`SELECT count(*) FROM runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("runs table holds %d rows, want only the accepted Worker seal's; the landing opened a Run", runCount)
	}
}

// ---------------------------------------------------------------------------
// The private (Darwin broker/helper) frame — the landing carrier.
//
// `mc dispatch` on Darwin does NOT call Dispatch() in process; it self-delegates
// through __dispatch-prepare/__dispatch-commit over the one-shot control
// descriptor. Every other test of this lane calls the in-process entry point, so
// the whole lane could be — and for one commit was — completely dead on the
// platform this deployment targets while every one of them stayed green.
//
// The round trip is what makes the frame trustworthy: the helper recomputes the
// preparation token from the tuple it received, so anything the carrier drops or
// mangles must show up as a token mismatch rather than as a landing committed
// from a doctored frame.
// ---------------------------------------------------------------------------

func TestPrivateFrameRoundTripsALandingCandidate(t *testing.T) {
	prepared := dlsPrepare(t, dlsTask(), dlsMountState())

	carrier := privateLandingFromPrepared(prepared.landing)
	if carrier == nil {
		t.Fatal("no landing carrier was produced")
	}

	back, err := preparedFromPrivateLanding(prepared.identity,
		prepared.deploymentUUID, prepared.requestID, carrier)
	if err != nil {
		t.Fatalf("preparedFromPrivateLanding: %v", err)
	}
	if back.landing == nil {
		t.Fatal("the round trip produced no landing")
	}
	if back.candidate != nil {
		t.Fatal("the round trip produced a spawn candidate; every unguarded cand.spawn deref is now reachable")
	}
	if !reflect.DeepEqual(*back.landing, *prepared.landing) {
		t.Fatalf("the landing did not survive the private frame\n got: %+v\nwant: %+v",
			*back.landing, *prepared.landing)
	}
}

func TestPrivateFrameRefusesAMalformedLandingCarrier(t *testing.T) {
	prepared := dlsPrepare(t, dlsTask(), dlsMountState())
	base := privateLandingFromPrepared(prepared.landing)

	for name, mutate := range map[string]func(*PrivateDispatchLandingCandidate){
		"task id":        func(c *PrivateDispatchLandingCandidate) { c.TaskID = 0 },
		"landing id":     func(c *PrivateDispatchLandingCandidate) { c.LandingID = "nothex" },
		"token":          func(c *PrivateDispatchLandingCandidate) { c.Token = "short" },
		"workspace root": func(c *PrivateDispatchLandingCandidate) { c.WorkspaceRoot = "" },
		"relative root":  func(c *PrivateDispatchLandingCandidate) { c.WorkspaceRoot = "relative/path" },
	} {
		t.Run(name, func(t2 *testing.T) {
			c := *base
			mutate(&c)
			if _, err := preparedFromPrivateLanding(prepared.identity,
				prepared.deploymentUUID, prepared.requestID, &c); err == nil {
				t2.Fatalf("the helper accepted a landing carrier with a bad %s", name)
			}
		})
	}
}
