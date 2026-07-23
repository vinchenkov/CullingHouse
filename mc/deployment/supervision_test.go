package deployment

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func supervisionFixture(t *testing.T) (string, SupervisionSpec) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "mc-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	home, err := CanonicalHome(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{
		filepath.Join(home, "release"), filepath.Join(home, "release", "runner"),
		filepath.Join(home, "release", "host"),
	} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range runnerReleaseDirs[1:] {
		if err := os.Mkdir(filepath.Join(home, "release", "runner", filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range runnerReleaseFiles {
		if err := os.WriteFile(filepath.Join(home, "release", "runner", filepath.FromSlash(rel)), []byte("// runner\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range hostReleaseDirs[1:] {
		if err := os.Mkdir(filepath.Join(home, "release", "host", filepath.FromSlash(rel)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range hostReleaseFiles {
		if err := os.WriteFile(filepath.Join(home, "release", "host", filepath.FromSlash(rel)), []byte("// host\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, name := range []string{"mc", "bun", "docker"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		paths[name] = resolved
	}
	spec := SupervisionSpec{
		ReleaseBuildID: "0123456789abcdef0123456789abcdef01234567",
		MCPath:         paths["mc"], BunPath: paths["bun"], DockerPath: paths["docker"],
		Image:          "mc-prod",
		WorkspaceRoots: []WorkspaceRoot{{ID: "alpha", Root: workspace}},
	}
	names, err := RuntimeNames(home)
	if err != nil {
		t.Fatal(err)
	}
	spec.SpineVolume = names.Volume
	return home, spec
}

func TestPrepareSupervisionPublishesClosedUnloadedLaunchAgentBundle(t *testing.T) {
	home, spec := supervisionFixture(t)
	inspected := []string{}
	status, result, err := PrepareSupervision(home, spec, func(label string) (bool, error) {
		inspected = append(inspected, label)
		return false, nil
	})
	if err != nil {
		t.Fatalf("prepare supervision: %v", err)
	}
	if status != "done" || len(inspected) != 4 {
		t.Fatalf("status=%q inspected=%v", status, inspected)
	}
	names, _ := RuntimeNames(home)
	wantLabels := []string{"com.mission-control." + names.Suffix + ".resident", "com.mission-control." + names.Suffix + ".dashboard"}
	if result.ResidentLabel != wantLabels[0] || result.DashboardLabel != wantLabels[1] {
		t.Fatalf("labels=%+v", result)
	}

	root := filepath.Join(home, "supervision")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{entries[0].Name(), entries[1].Name(), entries[2].Name()}; strings.Join(got, ",") != "LaunchAgents,dashboard.json,resident.json" {
		t.Fatalf("supervision entries=%v", got)
	}
	for _, rel := range []string{"resident.json", "dashboard.json", "LaunchAgents/resident.plist", "LaunchAgents/dashboard.plist"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s info=%v err=%v", rel, info, err)
		}
	}

	var resident map[string]any
	body, _ := os.ReadFile(filepath.Join(root, "resident.json"))
	if err := json.Unmarshal(body, &resident); err != nil {
		t.Fatal(err)
	}
	if resident["releaseBuildId"] != spec.ReleaseBuildID || resident["mcPath"] != spec.MCPath || resident["dockerPath"] != spec.DockerPath || resident["runnerSrcDir"] != filepath.Join(home, "release", "runner") || resident["backupIntervalMs"] != float64(3600000) {
		t.Fatalf("resident config=%v", resident)
	}
	roots := resident["workspaceRoots"].([]any)
	if len(roots) != 1 || roots[0].(map[string]any)["id"] != "alpha" {
		t.Fatalf("workspace roots=%v", roots)
	}

	for i, label := range wantLabels {
		name := []string{"resident.plist", "dashboard.plist"}[i]
		body, err := os.ReadFile(filepath.Join(root, "LaunchAgents", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, required := range []string{"<key>RunAtLoad</key>", "<true/>", "<key>KeepAlive</key>", label, spec.BunPath, filepath.Join(home, "logs")} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s missing %q", label, required)
			}
		}
		if out, err := exec.Command("plutil", "-lint", filepath.Join(root, "LaunchAgents", name)).CombinedOutput(); err != nil {
			t.Fatalf("plist lint %s: %v: %s", name, err, out)
		}
	}

	before, _ := os.Lstat(root)
	status, _, err = PrepareSupervision(home, spec, func(string) (bool, error) { return false, nil })
	after, _ := os.Lstat(root)
	if err != nil || status != "ok" || !os.SameFile(before, after) {
		t.Fatalf("idempotent replay status=%q same=%v err=%v", status, os.SameFile(before, after), err)
	}
}

func TestPrepareSupervisionRefusesLoadedOrUnsafeInputsBeforePublication(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		home, spec := supervisionFixture(t)
		_, _, err := PrepareSupervision(home, spec, func(label string) (bool, error) {
			return strings.HasSuffix(label, ".resident"), nil
		})
		if err == nil || !strings.Contains(err.Error(), "already loaded") {
			t.Fatalf("err=%v", err)
		}
		if _, statErr := os.Lstat(filepath.Join(home, "supervision")); !os.IsNotExist(statErr) {
			t.Fatalf("loaded refusal published supervision: %v", statErr)
		}
	})

	t.Run("release identity", func(t *testing.T) {
		home, spec := supervisionFixture(t)
		spec.ReleaseBuildID = "development"
		if _, _, err := PrepareSupervision(home, spec, func(string) (bool, error) { return false, nil }); err == nil {
			t.Fatal("development identity accepted for production supervision")
		}
	})

	t.Run("workspace alias", func(t *testing.T) {
		home, spec := supervisionFixture(t)
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(spec.WorkspaceRoots[0].Root, alias); err != nil {
			t.Fatal(err)
		}
		spec.WorkspaceRoots[0].Root = alias
		if _, _, err := PrepareSupervision(home, spec, func(string) (bool, error) { return false, nil }); err == nil {
			t.Fatal("workspace symlink accepted")
		}
	})
}
