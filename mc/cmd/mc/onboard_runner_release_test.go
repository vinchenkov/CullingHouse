package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"mc/deployment"
)

func onboardRunnerSource(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "runner")
	files := []string{
		"agent-runner/adapters/claude.ts",
		"agent-runner/adapters/codex.ts",
		"agent-runner/adapters/session-store.ts",
		"agent-runner/main.ts",
		"homie-runner/main.ts",
	}
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("// "+rel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestOnboardHomePublishesTheInstallerSuppliedRunnerRelease(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	spine := filepath.Join(home, "spine.db")
	env := []string{"MC_HOME=" + home, "MC_SPINE=" + spine}
	source := onboardRunnerSource(t)
	res := runMC(t, env, "", "onboard", "home", "--release-source", source)
	if res.code != 0 {
		t.Fatalf("onboard home release = code %d stderr %q", res.code, res.stderr)
	}
	sections, ok := res.json["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("home sections = %#v", res.json["sections"])
	}
	section, ok := sections[0].(map[string]any)
	if !ok {
		t.Fatalf("home section = %#v", sections[0])
	}
	if section["status"] != "done" {
		t.Fatalf("home section = %#v", section)
	}
	if err := deployment.ValidateRunnerRelease(filepath.Join(home, "release", "runner")); err != nil {
		t.Fatal(err)
	}
}
