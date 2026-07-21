//go:build docker_boundary

package boundarydocker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The fixed landing program, run for real.
//
// `mc __land-sealed` is the only thing in the system that writes to an
// operator's real repository. Until this file it had never executed inside a
// container at all — its behaviour was covered by host-side unit tests of the
// functions it calls, which cannot show that the baked binary, the mounted
// envelope, and the realized `/repo` plane agree with each other.
//
// What these tests pin is the FENCE ORDER on first contact, and that each fence
// fails CLOSED with a diagnostic naming what was missing. Order is the property
// worth protecting: the cover is checked before the sealed store is opened, so
// a landing never touches the reviewed bytes until it has proved they are not
// reachable through the writable source alias. A reordering would still refuse
// every malformed landing and would silently drop that guarantee.
//
// These are deliberately NOT a full merge. The complete
// `packaged -> approve -> merge -> archived` walk belongs in the e2e suite,
// which owns the machinery for building a sealed task store with a reviewed
// commit; see the ledger's outgoing NEXT.
// ---------------------------------------------------------------------------

type landingEnvelope struct {
	SchemaVersion       int    `json:"schema_version"`
	Operation           string `json:"operation"`
	TaskID              int    `json:"task_id"`
	ObjectFormat        string `json:"object_format"`
	LandingID           string `json:"landing_id"`
	Branch              string `json:"branch"`
	TargetRef           string `json:"target_ref"`
	VerifiedSHA         string `json:"verified_sha"`
	PreMergeSHA         string `json:"pre_merge_sha"`
	PinnedBaseSHA       string `json:"pinned_base_sha"`
	PinnedClosureDigest string `json:"pinned_closure_digest"`
	PinnedLocalRepoUUID string `json:"pinned_local_repo_uuid"`
	SourceRepo          string `json:"source_repo"`
	TaskRoot            string `json:"task_root"`
	CoverRoot           string `json:"cover_root"`
}

func wellFormedEnvelope(preMerge string) landingEnvelope {
	return landingEnvelope{
		SchemaVersion: 1, Operation: "sealed-landing", TaskID: 7,
		ObjectFormat: "sha1", LandingID: "0011223344556677",
		Branch: "mc/task-7", TargetRef: "main",
		VerifiedSHA:         strings.Repeat("a", 40),
		PreMergeSHA:         preMerge,
		PinnedBaseSHA:       preMerge,
		PinnedClosureDigest: strings.Repeat("d", 64),
		PinnedLocalRepoUUID: "0f9c1e2a-3b4c-4d5e-8f60-112233445566",
		SourceRepo:          "/repo/source",
		TaskRoot:            "/repo/task",
		CoverRoot:           "/repo/source/.mission-control",
	}
}

func writeEnvelope(t *testing.T, dir string, env landingEnvelope) string {
	t.Helper()
	body, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "landing.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// gitRepoAt makes a real git repository with one operator commit and returns
// its HEAD.
func gitRepoAt(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=operator", "GIT_AUTHOR_EMAIL=o@example.invalid",
			"GIT_COMMITTER_NAME=operator", "GIT_COMMITTER_EMAIL=o@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "operator c1")
	return run("rev-parse", "HEAD")
}

// runLandSealed executes the real fixed program in the production image over
// whatever `/repo` plane the caller composed.
func runLandSealed(t *testing.T, repo, cover, taskRoot, envelope string) string {
	t.Helper()
	out, _ := dockerRun(t,
		"--user", "10002:10002",
		"-v", repo+":/repo/source",
		"-v", cover+":/repo/source/.mission-control:ro",
		"-v", taskRoot+":/repo/task:ro",
		"-v", envelope+":/mc/landing.json:ro",
		prodImage, "mc", "__land-sealed", "/mc/landing.json")
	return out
}

func TestLandingProgramFencesCoverBeforeTheSealedStoreDockerBoundary(t *testing.T) {
	requireProdImage(t)

	scratch := sharedTempDir(t)
	repo := filepath.Join(scratch, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	head := gitRepoAt(t, repo)
	cover := filepath.Join(scratch, "cover")
	taskRoot := filepath.Join(scratch, "task")
	for _, d := range []string{cover, taskRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	envelope := writeEnvelope(t, scratch, wellFormedEnvelope(head))

	t.Run("an absent cover refuses before anything is read", func(t *testing.T) {
		// The cover bind is omitted entirely, so `/repo/source/.mission-control`
		// does not exist. The landing must refuse HERE — before it opens the
		// sealed store — because until the cover is proved present the sealed
		// bytes are reachable through the writable source alias.
		out, _ := dockerRun(t,
			"--user", "10002:10002",
			"-v", repo+":/repo/source",
			"-v", taskRoot+":/repo/task:ro",
			"-v", envelope+":/mc/landing.json:ro",
			prodImage, "mc", "__land-sealed", "/mc/landing.json")
		if !strings.Contains(out, "landing cover is absent") {
			t.Fatalf("an absent cover did not refuse first:\n%s", out)
		}
		if strings.Contains(out, "sealed store") {
			t.Fatalf("the landing reached the sealed store before proving the cover:\n%s", out)
		}
	})

	t.Run("with the cover present it advances to the sealed store", func(t *testing.T) {
		// Same envelope, cover now bound. The next fence must be the sealed
		// store — which is how we know the first arm refused for its own reason
		// rather than because the whole thing was malformed.
		out := runLandSealed(t, repo, cover, taskRoot, envelope)
		if strings.Contains(out, "landing cover is absent") {
			t.Fatalf("the cover fence still fired with a cover bound:\n%s", out)
		}
		if !strings.Contains(out, "sealed store config is unreadable") {
			t.Fatalf("expected the sealed-store fence next:\n%s", out)
		}
	})

	t.Run("every refusal is a classified domain rejection", func(t *testing.T) {
		// Not a crash, not a usage error, and not a silent success. The
		// resident's exit-code classification treats 1 as "the reviewed change
		// failed to land" and everything else as infrastructure health, so a
		// landing that refused with the wrong code would be misfiled.
		out := runLandSealed(t, repo, cover, taskRoot, envelope)
		if !strings.Contains(out, `"code":"domain-rejection"`) {
			t.Fatalf("a landing refusal was not a classified domain rejection:\n%s", out)
		}
	})
}
