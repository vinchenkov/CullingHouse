package verbs

import (
	"strings"
	"testing"
)

// Step 3 of the sealed landing lane: the landing envelope as a fourth arm of
// the closed operation union, and the `Landing` instruction on the private
// mount plan. Both are INERT — nothing produces either yet. These tests pin
// the grammar so step 4's lander and step 5's wiring cannot widen it silently.

func landingEnvelope() SetupEnvelope {
	return SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationSealedLanding,
		TaskID: 7, ObjectFormat: "sha1",
		LandingID:           "0011223344556677",
		Branch:              taskAssignmentBranch(7),
		TargetRef:           "main",
		VerifiedSHA:         strings.Repeat("a", 40),
		PreMergeSHA:         strings.Repeat("b", 40),
		PinnedBaseSHA:       strings.Repeat("c", 40),
		PinnedClosureDigest: strings.Repeat("d", 64),
		PinnedLocalRepoUUID: "0f9c1e2a-3b4c-4d5e-8f60-112233445566",
		SourceRepo:          "/repo/source",
		TaskRoot:            "/repo/task",
		CoverRoot:           "/repo/source/.mission-control",
	}
}

func TestSealedLandingEnvelopeIsAClosedArm(t *testing.T) {
	if err := validateSetupEnvelope(landingEnvelope()); err != nil {
		t.Fatalf("valid sealed landing envelope: %v", err)
	}
	bad := map[string]func(*SetupEnvelope){
		// ADR-017:702's destinations are FIXED cells of the landing table
		// (landingplan.go). A host path here would mean the resident let
		// dispatch choose where the RW real-repository grant lands.
		"host-source":    func(e *SetupEnvelope) { e.SourceRepo = "/Users/op/repo" },
		"host-task-root": func(e *SetupEnvelope) { e.TaskRoot = "/Users/op/repo/.mission-control/tasks/task-7" },
		"host-cover":     func(e *SetupEnvelope) { e.CoverRoot = "/Users/op/repo/.mission-control" },
		"source-slash":   func(e *SetupEnvelope) { e.SourceRepo = "/repo/source/" },
		// The cover is not optional: without it the landing container reaches
		// the sealed task bytes through the RW `/repo/source` alias, which is
		// exactly what ADR-017:700 exists to prevent.
		"no-cover":      func(e *SetupEnvelope) { e.CoverRoot = "" },
		"cover-outside": func(e *SetupEnvelope) { e.CoverRoot = "/repo/task/.mission-control" },
		// Landing holds no lease and opens no Run (§7); a run id here means
		// some caller conflated it with the setup class.
		"run-id":      func(e *SetupEnvelope) { e.RunID = "worker-1" },
		"landing-id":  func(e *SetupEnvelope) { e.LandingID = "not-hex" },
		"landing-len": func(e *SetupEnvelope) { e.LandingID = strings.Repeat("a", 15) },
		"no-landing":  func(e *SetupEnvelope) { e.LandingID = "" },
		"branch":      func(e *SetupEnvelope) { e.Branch = "mc/task-8" },
		"no-branch":   func(e *SetupEnvelope) { e.Branch = "" },
		"no-target":   func(e *SetupEnvelope) { e.TargetRef = "" },
		"verified":    func(e *SetupEnvelope) { e.VerifiedSHA = "bad" },
		"premerge":    func(e *SetupEnvelope) { e.PreMergeSHA = "bad" },
		"base":        func(e *SetupEnvelope) { e.PinnedBaseSHA = "bad" },
		"digest":      func(e *SetupEnvelope) { e.PinnedClosureDigest = "bad" },
		"uuid":        func(e *SetupEnvelope) { e.PinnedLocalRepoUUID = "not-a-uuid" },
		"format":      func(e *SetupEnvelope) { e.ObjectFormat = "sha3" },
		"task":        func(e *SetupEnvelope) { e.TaskID = 0 },
	}
	for name, mutate := range bad {
		env := landingEnvelope()
		mutate(&env)
		if err := validateSetupEnvelope(env); err == nil {
			t.Fatalf("%s: a malformed sealed landing envelope was accepted", name)
		}
	}
}

// The object format is a real fence, not decoration: a sha256 landing carries
// 64-hex SHAs and a sha1 one 40-hex, and neither may borrow the other's width.
func TestSealedLandingEnvelopeBindsSHAWidthToTheObjectFormat(t *testing.T) {
	env := landingEnvelope()
	env.ObjectFormat = "sha256"
	if err := validateSetupEnvelope(env); err == nil {
		t.Fatal("a sha256 landing envelope accepted sha1-width SHAs")
	}
	env.VerifiedSHA = strings.Repeat("a", 64)
	env.PreMergeSHA = strings.Repeat("b", 64)
	env.PinnedBaseSHA = strings.Repeat("c", 64)
	if err := validateSetupEnvelope(env); err != nil {
		t.Fatalf("a coherent sha256 landing envelope: %v", err)
	}
}

// Cross-arm field bleed, BOTH directions. The union is closed only if each arm
// refuses every other arm's authority — otherwise an envelope can carry setup
// authority into the one class that holds a real repository RW.
func TestSealedLandingEnvelopeRefusesCrossArmBleedBothDirections(t *testing.T) {
	// Landing must refuse every setup-only field.
	intoLanding := map[string]func(*SetupEnvelope){
		"mode":            func(e *SetupEnvelope) { e.Mode = "fresh" },
		"worktree":        func(e *SetupEnvelope) { e.WorktreeName = taskWorktreeName(7) },
		"seal-root":       func(e *SetupEnvelope) { e.SealRoot = "/repo/seal" },
		"projection-root": func(e *SetupEnvelope) { e.ProjectionRoot = "/repo/projection" },
		"completion-run":  func(e *SetupEnvelope) { e.CompletionRunID = "worker-1" },
		"completion-req":  func(e *SetupEnvelope) { e.CompletionRequest = "0011223344556677" },
		"sealed-sha":      func(e *SetupEnvelope) { e.SealedSHA = strings.Repeat("e", 40) },
		"closure-digest":  func(e *SetupEnvelope) { e.ClosureDigest = strings.Repeat("e", 64) },
		"manifest":        func(e *SetupEnvelope) { e.ManifestDigest = strings.Repeat("e", 64) },
		"seal-device":     func(e *SetupEnvelope) { e.SealDevice = "1" },
		"seal-inode":      func(e *SetupEnvelope) { e.SealInode = "2" },
		"seal-owner":      func(e *SetupEnvelope) { e.SealOwnerUID = 501 },
	}
	for name, mutate := range intoLanding {
		env := landingEnvelope()
		mutate(&env)
		if err := validateSetupEnvelope(env); err == nil {
			t.Fatalf("%s: a sealed landing envelope carried setup authority", name)
		}
	}

	// And every setup arm must refuse the landing-only fields.
	landingOnly := map[string]func(*SetupEnvelope){
		"landing-id":   func(e *SetupEnvelope) { e.LandingID = "0011223344556677" },
		"verified-sha": func(e *SetupEnvelope) { e.VerifiedSHA = strings.Repeat("a", 40) },
		"premerge-sha": func(e *SetupEnvelope) { e.PreMergeSHA = strings.Repeat("b", 40) },
		"cover-root":   func(e *SetupEnvelope) { e.CoverRoot = "/repo/source/.mission-control" },
	}
	setupArms := map[string]SetupEnvelope{
		"first-task": freshEnvelope("/repo/source", "/repo/task", "sha1"),
		"accepted":   acceptedSealEnvelopeFixture(),
		"projection": verifierProjectionEnvelopeFixture(),
	}
	for arm, base := range setupArms {
		if err := validateSetupEnvelope(base); err != nil {
			t.Fatalf("%s fixture is not itself valid: %v", arm, err)
		}
		for name, mutate := range landingOnly {
			env := base
			mutate(&env)
			if err := validateSetupEnvelope(env); err == nil {
				t.Fatalf("%s arm accepted landing-only field %s", arm, name)
			}
		}
	}
}

// The three in-container setup executors must refuse a landing operation
// outright: landing is a different class with a different binary, and none of
// them may be reached by relabelling an envelope.
func TestSetupExecutorsRefuseALandingEnvelope(t *testing.T) {
	env := landingEnvelope()
	if _, err := RunFirstTaskSetup(env); err == nil {
		t.Fatal("the first-task executor accepted a landing operation")
	}
	if _, err := RunAcceptedSealSetup(env); err == nil {
		t.Fatal("the accepted-seal executor accepted a landing operation")
	}
	if err := RunVerifierProjectionSetup(env); err == nil {
		t.Fatal("the verifier projection executor accepted a landing operation")
	}
}

func acceptedSealEnvelopeFixture() SetupEnvelope {
	return SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationAcceptedSealRebuild,
		RunID: "worker", TaskID: 7, ObjectFormat: "sha1",
		CompletionRunID: "completion-worker", CompletionRequest: "0011223344556677",
		SealedSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64),
		ManifestDigest: strings.Repeat("c", 64), SealRoot: "/repo/seal", TaskRoot: "/repo/task",
		SealDevice: "1", SealInode: "2", SealOwnerUID: 501,
	}
}

func verifierProjectionEnvelopeFixture() SetupEnvelope {
	return SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationVerifierProjection,
		RunID: "verifier", TaskID: 7, ObjectFormat: "sha1",
		CompletionRequest: "0011223344556677", SealedSHA: strings.Repeat("a", 40),
		ClosureDigest: strings.Repeat("b", 64), ManifestDigest: strings.Repeat("c", 64),
		TaskRoot: "/repo/task", ProjectionRoot: "/repo/projection",
		SealDevice: "1", SealInode: "2", SealOwnerUID: 501,
	}
}
