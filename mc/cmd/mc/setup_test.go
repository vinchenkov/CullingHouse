package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The in-container executor path end to end: mc __setup-first-task reads the
// frozen envelope, materializes the task-local store from the source's
// reachable closure, and emits the SetupResult the resident hands the host.
func TestSetupFirstTaskSubcommandMaterializesAStore(t *testing.T) {
	src := t.TempDir()
	setupGit(t, src, "init", "-q")
	setupGit(t, src, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setupGit(t, src, "add", "-A")
	setupGit(t, src, "commit", "-qm", "c1")

	taskRoot := t.TempDir()
	for _, c := range []string{"source", "git"} {
		if err := os.Mkdir(filepath.Join(taskRoot, c), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	env := map[string]any{
		"schema_version": 1, "operation": "first-task-closure-extraction",
		"run_id": "setup-run", "task_id": 7, "mode": "fresh", "object_format": "sha1",
		"target_ref": "HEAD", "branch": "mc/task-7", "worktree_name": "mc-task-7",
		"source_repo": src, "task_root": taskRoot,
	}
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(envPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	res := runMC(t, nil, "", "__setup-first-task", envPath)
	if res.code != 0 {
		t.Fatalf("setup exit = %d stderr=%q json=%v", res.code, res.stderr, res.json)
	}
	if res.json["fsck_clean"] != true || res.json["base_sha"] == "" {
		t.Fatalf("result json = %v, want a clean materialized store", res.json)
	}
	if _, err := os.Stat(filepath.Join(taskRoot, "git", "refs", "heads", "mc", "task-7")); err != nil {
		t.Fatalf("subcommand left no ref: %v", err)
	}

	// An agent-scoped caller (a run.json present) is refused: only the setup
	// container's host-scope mc may run the executor.
	agentRun := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(agentRun, []byte(`{"run_id":"r","tier":"pipeline","role":"worker"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	denied := runMC(t, []string{"MC_RUN_JSON=" + agentRun}, "", "__setup-first-task", envPath)
	if denied.code == 0 {
		t.Fatalf("an agent-scoped caller ran the setup executor: %v", denied.json)
	}
}

func TestSetupRecordRejectsMalformedResult(t *testing.T) {
	res := runMC(t, nil, "", "task", "setup-record", "--run", "r", "--workspace", "/w", "--result", "{not json")
	if res.code != 2 {
		t.Fatalf("malformed setup result exit = %d json=%v stderr=%q", res.code, res.json, res.stderr)
	}
	if e, _ := res.json["error"].(map[string]any); e == nil || e["code"] != "usage" {
		t.Fatalf("want a usage error envelope, got %v", res.json)
	}
}

func TestAcceptedSealRecordRejectsMalformedResult(t *testing.T) {
	res := runMC(t, nil, "", "task", "accepted-seal-record", "--run", "r", "--workspace", "/w", "--result", "{not json")
	if res.code != 2 {
		t.Fatalf("malformed accepted-seal result exit = %d json=%v stderr=%q", res.code, res.json, res.stderr)
	}
	if e, _ := res.json["error"].(map[string]any); e == nil || e["code"] != "usage" {
		t.Fatalf("want a usage error envelope, got %v", res.json)
	}
}

// The setup-record crossing must attest the Worksource on the HOST even when
// every ordinary verb self-delegates into the helper container. The helper
// carries spine and MC_HOME binds and no Worksource, so a delegated whole-verb
// invocation resolved a host path it could not see and refused on a loop
// (`mc: source "<ws>" does not exist`). Naming an unreachable helper proves
// which side ran: a host frame reaches its own filesystem attest and returns a
// domain refusal about the task root; a delegated one never gets that far.
func TestSetupRecordAttestsTheWorksourceOnTheHostNotInTheHelper(t *testing.T) {
	result := `{"base_sha":"` + strings.Repeat("a", 40) + `","object_format":"sha1",` +
		`"local_repo_uuid":"3f2504e0-4f89-11d3-9a0c-0305e82c3301","closure_digest":"` +
		strings.Repeat("b", 64) + `","object_count":3,"fsck_clean":true}`

	for _, verb := range []string{"setup-record", "accepted-seal-record"} {
		t.Run(verb, func(t *testing.T) {
			// macOS hands out /var/folders/… temp dirs behind a symlink; the
			// attest requires the Worksource to be its own canonical spelling.
			ws, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			res := runMC(t, []string{"MC_HELPER=mc-helper-that-does-not-exist"}, "",
				"task", verb, "--run", "r", "--task", "7", "--workspace", ws, "--result", result)
			if res.code != 1 {
				t.Fatalf("%s exit = %d json=%v stderr=%q", verb, res.code, res.json, res.stderr)
			}
			e, _ := res.json["error"].(map[string]any)
			if e == nil {
				t.Fatalf("%s: want a domain error envelope from the host attest, got %v", verb, res.json)
			}
			msg, _ := e["message"].(string)
			if !strings.Contains(msg, "registered root is absent") {
				t.Fatalf("%s: want the host-side task-root refusal, got %q", verb, msg)
			}
		})
	}
}
