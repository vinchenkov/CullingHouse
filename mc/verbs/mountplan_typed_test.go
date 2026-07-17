package verbs

import (
	"os"
	"path/filepath"
	"testing"

	"mc/boundary"
	"mc/refusal"
)

// mtFixture builds a trusted allowlist-less mountPlanInputs whose
// jurisdiction authorizes the task-local typed roots of the exact skeleton
// built by tsBuild.
func mtInputs(t *testing.T, ws string, taskID int64) mountPlanInputs {
	t.Helper()
	dir := t.TempDir()
	allowlist := filepath.Join(dir, "mount-allowlist.toml")
	if err := os.WriteFile(allowlist, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}
	typed, err := resolveTaskLocalSkeleton(ws, taskID, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	typedRoots := map[boundary.TypedKind][]boundary.ProtectedID{}
	for kind, id := range typed {
		typedRoots[kind] = []boundary.ProtectedID{id}
	}
	j, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
		Home: home, TypedRoots: typedRoots,
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	return mountPlanInputs{
		AllowlistPath: allowlist, OwnerUID: os.Getuid(),
		Blocked: boundary.BlockPolicy{}, Jurisdiction: j,
	}
}

func mtTypedRequests(ws string, taskID int64) []mountRequest {
	root := filepath.Join(ws, ".mission-control", "tasks", "task-7")
	requests := make([]mountRequest, 0, 15)
	for _, row := range taskPlanRows(taskID) {
		source := root
		if row.Rel != "" {
			source = filepath.Join(root, filepath.FromSlash(row.Rel))
		}
		requests = append(requests, mountRequest{
			Source: source, Access: row.Access, Authority: refusal.AuthorityDeployment,
			Kind: row.Kind, Destination: row.Dest,
		})
	}
	return requests
}

func TestPlanMountsAuthorizesTypedTaskRows(t *testing.T) {
	ws, root := tsBuild(t)
	entries, r, err := planMounts(mtTypedRequests(ws, 7), mtInputs(t, ws, 7))
	if err != nil || r != nil {
		t.Fatalf("plan: refusal=%+v err=%v", r, err)
	}
	if len(entries) != 15 {
		t.Fatalf("want 15 entries, got %d", len(entries))
	}
	if entries[0].Destination != "/workspace" || entries[0].LogicalID != "task-root" {
		t.Fatalf("first sorted entry = %+v, want the task root at /workspace", entries[0])
	}
	byDest := map[string]PrivateDispatchMountEntry{}
	for _, e := range entries {
		byDest[e.Destination] = e
	}
	src := byDest["/workspace/source"]
	if src.Access != "rw" || src.Kind != "dir" || src.Source != filepath.Join(root, "source") {
		t.Fatalf("source entry = %+v", src)
	}
	if src.Device == "" || src.Inode == "" {
		t.Fatalf("typed entries carry identity evidence: %+v", src)
	}
	cfg := byDest["/workspace/git/config"]
	if cfg.Access != "ro" || cfg.Kind != "file" || cfg.LogicalID != "task-git-config-cover" {
		t.Fatalf("config cover entry = %+v", cfg)
	}
}

func TestPlanMountsTypedRowRejectsWrongIdentity(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)
	// A source that is not the authorized typed root for its claimed kind is
	// denied, not trusted (ADR-021 D10).
	imposter := filepath.Join(ws, "imposter")
	if err := os.Mkdir(imposter, 0o700); err != nil {
		t.Fatal(err)
	}
	requests := mtTypedRequests(ws, 7)
	requests[1].Source = imposter // claims KindTaskSource
	_, r, err := planMounts(requests, in)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Code != boundary.CodeDeniedRoot {
		t.Fatalf("want denied_root refusal, got %+v", r)
	}
}

func TestPlanMountsTypedRowsBypassAllowlistButNotBlockedFloor(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)
	// The allowlist in mtInputs authorizes nothing, yet the typed rows plan:
	// typed system sources bypass the external allowlist requirement only
	// (ADR-017:349-351).
	entries, r, err := planMounts(mtTypedRequests(ws, 7), in)
	if err != nil || r != nil || len(entries) != 15 {
		t.Fatalf("typed rows must not require allowlist membership: refusal=%+v err=%v n=%d", r, err, len(entries))
	}
}

func TestPlanMountsTypedDestinationMustBeFixed(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)
	requests := mtTypedRequests(ws, 7)
	requests[0].Destination = "/mc/session" // outside the closed task table
	_, _, err := planMounts(requests, in)
	if err == nil {
		t.Fatal("a typed request with an out-of-table destination is a protocol error, not a plannable row")
	}
}

func TestPlanMountsNamedEdgesPermitOnlyTaskNesting(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)

	// The full task set plans: every ancestor/descendant pair among its rows
	// is a named D6 edge (checked by TestPlanMountsAuthorizesTypedTaskRows).
	// An artifact-class row nests under the task root through the root edge.
	artifactRoot := filepath.Join(ws, "artifacts")
	if err := os.Mkdir(artifactRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	allowlist := "version = 1\n\n[[allow]]\npath = \"" + artifactRoot + "\"\ntarget = \"art\"\naccess = \"rw\"\n"
	if err := os.WriteFile(in.AllowlistPath, []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := append(mtTypedRequests(ws, 7), mountRequest{
		Source: artifactRoot, Access: boundary.AccessRW,
		Authority: refusal.AuthorityCandidate, Class: classArtifact,
	})
	entries, r, err := planMounts(requests, in)
	if err != nil || r != nil || len(entries) != 16 {
		t.Fatalf("artifact under the task root edge must plan: refusal=%+v err=%v n=%d", r, err, len(entries))
	}

	// Two ordinary rows overlapping each other stay a collision: the named
	// edges never relax the generic rule.
	nested := filepath.Join(artifactRoot, "inner")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	requests = append(requests, mountRequest{
		Source: nested, Access: boundary.AccessRW,
		Authority: refusal.AuthorityCandidate, Class: classArtifact,
	})
	_, r, err = planMounts(requests, in)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Code != boundary.CodeTargetCollision {
		t.Fatalf("ordinary nesting must still collide, got %+v", r)
	}
}

func TestValidatePrivateMountPlanAcceptsTypedTaskPlan(t *testing.T) {
	ws, _ := tsBuild(t)
	entries, r, err := planMounts(mtTypedRequests(ws, 7), mtInputs(t, ws, 7))
	if err != nil || r != nil {
		t.Fatalf("plan: refusal=%+v err=%v", r, err)
	}
	plan := &PrivateDispatchMountPlan{Version: 1, Entries: entries}
	if err := validatePrivateMountPlan(plan); err != nil {
		t.Fatalf("the helper boundary must accept the exact typed task plan: %v", err)
	}

	// The named edges never open the runtime planes or free-form nesting.
	evil := *plan
	evil.Entries = append([]PrivateDispatchMountEntry(nil), plan.Entries...)
	evil.Entries[0].Destination = "/workspace/evil"
	evil.Entries[0].LogicalID = "task-root-evil"
	if err := validatePrivateMountPlan(&evil); err == nil {
		t.Fatal("an out-of-table destination must refuse at the helper boundary")
	}
}
