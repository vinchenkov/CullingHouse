package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func freshEnvelope(src, taskRoot, objfmt string) SetupEnvelope {
	return SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationFirstTaskClosure,
		RunID: "setup-run", TaskID: 7, Mode: "fresh", ObjectFormat: objfmt,
		TargetRef: "HEAD", Branch: "mc/task-7", WorktreeName: "mc-task-7",
		SourceRepo: src, TaskRoot: taskRoot,
	}
}

func TestRunFirstTaskSetupFreshMaterializesAndReports(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	root := mkTaskChildren(t)
	res, err := RunFirstTaskSetup(freshEnvelope(src, root, objfmt))
	if err != nil {
		t.Fatalf("fresh setup: %v", err)
	}
	if res.BaseSHA != base || !res.FsckClean || res.ObjectCount < 5 {
		t.Fatalf("result = %+v, want base %s and a clean nonempty closure", res, base)
	}
	if _, err := os.Stat(filepath.Join(root, "git", "refs", "heads", "mc", "task-7")); err != nil {
		t.Fatalf("fresh setup left no ref: %v", err)
	}
}

func TestRunFirstTaskSetupRetryReproducesThePinnedClosure(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	fresh, err := RunFirstTaskSetup(freshEnvelope(src, mkTaskChildren(t), objfmt))
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	env := SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationFirstTaskClosure,
		RunID: "setup-run", TaskID: 7, Mode: "retry", ObjectFormat: objfmt,
		PinnedBaseSHA: fresh.BaseSHA, PinnedClosureDigest: fresh.ClosureDigest,
		PinnedLocalRepoUUID: fresh.LocalRepoUUID,
		Branch:              "mc/task-7", WorktreeName: "mc-task-7",
		SourceRepo: src, TaskRoot: mkTaskChildren(t),
	}
	res, err := RunFirstTaskSetup(env)
	if err != nil {
		t.Fatalf("retry setup: %v", err)
	}
	if res.ClosureDigest != fresh.ClosureDigest || res.BaseSHA != fresh.BaseSHA || res.LocalRepoUUID != fresh.LocalRepoUUID {
		t.Fatalf("retry result %+v did not reproduce the pinned closure %+v", res, fresh)
	}
}

func TestRunFirstTaskSetupRetryRefusesAClosureThatMoved(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	fresh, err := RunFirstTaskSetup(freshEnvelope(src, mkTaskChildren(t), objfmt))
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	env := SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationFirstTaskClosure,
		RunID: "setup-run", TaskID: 7, Mode: "retry", ObjectFormat: objfmt,
		PinnedBaseSHA: fresh.BaseSHA, PinnedClosureDigest: strings.Repeat("0", 64),
		PinnedLocalRepoUUID: fresh.LocalRepoUUID,
		Branch:              "mc/task-7", WorktreeName: "mc-task-7",
		SourceRepo: src, TaskRoot: mkTaskChildren(t),
	}
	if _, err := RunFirstTaskSetup(env); err == nil {
		t.Fatal("retry accepted a closure that does not reproduce the pinned digest")
	}
}

func TestValidateSetupEnvelopeRejectsMalformed(t *testing.T) {
	base := freshEnvelope("/src", "/task", "sha1")
	bad := map[string]func(e *SetupEnvelope){
		"schema":    func(e *SetupEnvelope) { e.SchemaVersion = 2 },
		"operation": func(e *SetupEnvelope) { e.Operation = "land" },
		"mode":      func(e *SetupEnvelope) { e.Mode = "sideways" },
		"branch":    func(e *SetupEnvelope) { e.Branch = "mc/task-8" },
		"worktree":  func(e *SetupEnvelope) { e.WorktreeName = "mc-task-8" },
		"no-target": func(e *SetupEnvelope) { e.TargetRef = "" },
		"no-source": func(e *SetupEnvelope) { e.SourceRepo = "" },
		"format":    func(e *SetupEnvelope) { e.ObjectFormat = "sha3" },
	}
	for name, mut := range bad {
		e := base
		mut(&e)
		if err := validateSetupEnvelope(e); err == nil {
			t.Fatalf("%s: a malformed envelope was accepted", name)
		}
	}
}

func TestValidateAcceptedSealSetupEnvelopeIsAClosedReceipt(t *testing.T) {
	base := SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationAcceptedSealRebuild,
		RunID: "worker", TaskID: 7, ObjectFormat: "sha1",
		CompletionRequest: "0011223344556677", SealedSHA: strings.Repeat("a", 40),
		ClosureDigest: strings.Repeat("b", 64), ManifestDigest: strings.Repeat("c", 64),
		SealRoot: "/repo/seal", TaskRoot: "/repo/task", SealDevice: "1", SealInode: "2", SealOwnerUID: 501,
	}
	if err := validateSetupEnvelope(base); err != nil {
		t.Fatalf("valid accepted-seal envelope: %v", err)
	}
	bad := map[string]func(*SetupEnvelope){
		"host-path":         func(e *SetupEnvelope) { e.SealRoot = "/private/seal" },
		"request":           func(e *SetupEnvelope) { e.CompletionRequest = "not-a-request" },
		"sha":               func(e *SetupEnvelope) { e.SealedSHA = "bad" },
		"digest":            func(e *SetupEnvelope) { e.ManifestDigest = "bad" },
		"identity":          func(e *SetupEnvelope) { e.SealInode = "01" },
		"first-task-source": func(e *SetupEnvelope) { e.SourceRepo = "/host/source" },
	}
	for name, mutate := range bad {
		env := base
		mutate(&env)
		if err := validateSetupEnvelope(env); err == nil {
			t.Fatalf("%s: malformed accepted-seal envelope was accepted", name)
		}
	}
	if _, err := RunFirstTaskSetup(base); err == nil {
		t.Fatal("first-task executor accepted an accepted-seal operation")
	}
}

func TestReadSetupEnvelopeStrictRoundTrip(t *testing.T) {
	env := freshEnvelope("/src", "/task", "sha1")
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSetupEnvelope(path)
	if err != nil || got != env {
		t.Fatalf("round trip = (%+v, %v), want %+v", got, err, env)
	}
	if err := os.WriteFile(path, append(body[:len(body)-1], []byte(`,"rogue":1}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSetupEnvelope(path); err == nil {
		t.Fatal("an envelope with an unknown field was accepted")
	}
	if err := os.WriteFile(path, append(body, []byte("\n{}")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSetupEnvelope(path); err == nil {
		t.Fatal("an envelope with a trailing document was accepted")
	}
}

func retryEnvelope(src, taskRoot, objfmt string, fresh SetupResult) SetupEnvelope {
	return SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationFirstTaskClosure,
		RunID: "setup-run", TaskID: 7, Mode: "retry", ObjectFormat: objfmt,
		PinnedBaseSHA: fresh.BaseSHA, PinnedClosureDigest: fresh.ClosureDigest,
		PinnedLocalRepoUUID: fresh.LocalRepoUUID,
		Branch:              "mc/task-7", WorktreeName: "mc-task-7",
		SourceRepo: src, TaskRoot: taskRoot,
	}
}

func TestRunFirstTaskSetupRetryAcceptsExactResidueIdempotently(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	root := mkTaskChildren(t)
	fresh, err := RunFirstTaskSetup(freshEnvelope(src, root, objfmt))
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	// Retry over the same, now-materialized root: D5 exact residue is idempotent.
	res, err := RunFirstTaskSetup(retryEnvelope(src, root, objfmt, fresh))
	if err != nil {
		t.Fatalf("retry over exact residue refused: %v", err)
	}
	if res.BaseSHA != fresh.BaseSHA || res.ClosureDigest != fresh.ClosureDigest ||
		res.LocalRepoUUID != fresh.LocalRepoUUID || !res.FsckClean || res.ObjectCount < 1 {
		t.Fatalf("idempotent retry result %+v does not reproduce fresh %+v", res, fresh)
	}
}

func TestRunFirstTaskSetupRetryRefusesDivergentResidue(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	root := mkTaskChildren(t)
	fresh, err := RunFirstTaskSetup(freshEnvelope(src, root, objfmt))
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	refPath := filepath.Join(root, "git", "refs", "heads", "mc", "task-7")
	if err := os.WriteFile(refPath, []byte(strings.Repeat("d", len(fresh.BaseSHA))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunFirstTaskSetup(retryEnvelope(src, root, objfmt, fresh)); err == nil {
		t.Fatal("a divergent residue was accepted on retry")
	}
}
