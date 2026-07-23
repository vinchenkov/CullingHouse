package deployment

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type WorkspaceRoot struct {
	ID   string `json:"id"`
	Root string `json:"root"`
}

type SupervisionSpec struct {
	ReleaseBuildID string
	MCPath         string
	BunPath        string
	DockerPath     string
	Image          string
	SpineVolume    string
	WorkspaceRoots []WorkspaceRoot
}

type SupervisionResult struct {
	ResidentLabel  string
	DashboardLabel string
	Root           string
}

type LaunchdLoadedProbe func(label string) (bool, error)

var supervisionFiles = []string{
	"LaunchAgents/dashboard.plist",
	"LaunchAgents/resident.plist",
	"dashboard.json",
	"resident.json",
}

func validProductionBuildID(value string) bool {
	if len(value) < 40 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func canonicalExecutable(path, label string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("%s must be an absolute executable path", label)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s must resolve to an executable regular file", label)
	}
	return filepath.Clean(resolved), nil
}

func validateWorkspaceRoots(roots []WorkspaceRoot) ([]WorkspaceRoot, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("supervision requires at least one Worksource workspace")
	}
	out := append([]WorkspaceRoot(nil), roots...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i := range out {
		if out[i].ID == "" || len(out[i].ID) > 128 {
			return nil, fmt.Errorf("supervision workspace id is invalid")
		}
		for j, r := range out[i].ID {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
				(j > 0 && (r == '.' || r == '_' || r == '-')) {
				continue
			}
			return nil, fmt.Errorf("supervision workspace id %q is invalid", out[i].ID)
		}
		if i > 0 && out[i-1].ID == out[i].ID {
			return nil, fmt.Errorf("supervision workspace id %q is duplicated", out[i].ID)
		}
		if out[i].Root == "" || !filepath.IsAbs(out[i].Root) || filepath.Clean(out[i].Root) != out[i].Root {
			return nil, fmt.Errorf("Worksource %s workspace root is not a clean absolute path", out[i].ID)
		}
		resolved, err := filepath.EvalSymlinks(out[i].Root)
		if err != nil || filepath.Clean(resolved) != out[i].Root {
			return nil, fmt.Errorf("Worksource %s workspace root is unavailable or changed identity", out[i].ID)
		}
		info, err := os.Stat(out[i].Root)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("Worksource %s workspace root is not a directory", out[i].ID)
		}
	}
	return out, nil
}

func xmlText(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func launchAgent(label, bun, entry, config, working, mcHome, pathEnv, stdout, stderr string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>%s</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>MC_HOME</key>
    <string>%s</string>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, xmlText(label), xmlText(bun), xmlText(entry), xmlText(config), xmlText(working),
		xmlText(mcHome), xmlText(pathEnv), xmlText(stdout), xmlText(stderr)))
}

func ensureOwnerDirectory(path, label string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create %s: %w", label, err)
	}
	return requireOwnerOnlyDirectory(path, label)
}

func validateSupervisionTree(root string) error {
	expectedDirs := map[string]bool{".": true, "LaunchAgents": true}
	expectedFiles := map[string]bool{}
	for _, rel := range supervisionFiles {
		expectedFiles[rel] = true
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		uid, links, ok := statOwner(info)
		if !ok || int(uid) != os.Getuid() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("supervision entry %q lost operator ownership or became a symlink", rel)
		}
		if entry.IsDir() {
			if !expectedDirs[rel] || info.Mode().Perm() != 0o700 {
				return fmt.Errorf("supervision contains unexpected or non-owner-only directory %q", rel)
			}
			return nil
		}
		if !expectedFiles[rel] || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || links != 1 || info.Size() == 0 {
			return fmt.Errorf("supervision contains unexpected or unsafe file %q", rel)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	for _, rel := range supervisionFiles {
		if !seen[rel] {
			return fmt.Errorf("supervision is missing %q", rel)
		}
	}
	return nil
}

func supervisionTreesEqual(left, right string) (bool, error) {
	for _, rel := range supervisionFiles {
		a, err := os.ReadFile(filepath.Join(left, filepath.FromSlash(rel)))
		if err != nil {
			return false, err
		}
		b, err := os.ReadFile(filepath.Join(right, filepath.FromSlash(rel)))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(a, b) {
			return false, nil
		}
	}
	return true, nil
}

// PrepareSupervision atomically publishes the exact native configs and two
// per-user LaunchAgent definitions. It deliberately never writes into
// ~/Library/LaunchAgents and never bootstraps a job: activation is the one
// operator-present Phase-5 action. Both labels must be absent before and after
// publication so this preparation cannot race an already supervised process.
func PrepareSupervision(home string, spec SupervisionSpec, loaded LaunchdLoadedProbe) (string, SupervisionResult, error) {
	var result SupervisionResult
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		return "", result, err
	}
	if err := requireOwnerOnlyDirectory(canonicalHome, "MC_HOME"); err != nil {
		return "", result, err
	}
	if !validProductionBuildID(spec.ReleaseBuildID) {
		return "", result, fmt.Errorf("production supervision requires a 40..64 lowercase-hex release identity")
	}
	if spec.Image != "mc-prod" {
		return "", result, fmt.Errorf("production supervision requires image mc-prod")
	}
	names, err := RuntimeNames(canonicalHome)
	if err != nil {
		return "", result, err
	}
	if spec.SpineVolume != names.Volume {
		return "", result, fmt.Errorf("supervision spine volume does not match canonical MC_HOME")
	}
	mcPath, err := canonicalExecutable(spec.MCPath, "mc binary")
	if err != nil {
		return "", result, err
	}
	bunPath, err := canonicalExecutable(spec.BunPath, "bun binary")
	if err != nil {
		return "", result, err
	}
	dockerPath, err := canonicalExecutable(spec.DockerPath, "docker binary")
	if err != nil {
		return "", result, err
	}
	roots, err := validateWorkspaceRoots(spec.WorkspaceRoots)
	if err != nil {
		return "", result, err
	}
	runnerRoot := filepath.Join(canonicalHome, "release", "runner")
	hostRoot := filepath.Join(canonicalHome, "release", "host")
	if err := ValidateRunnerRelease(runnerRoot); err != nil {
		return "", result, fmt.Errorf("validate installed runner release: %w", err)
	}
	if err := ValidateHostRelease(hostRoot); err != nil {
		return "", result, fmt.Errorf("validate installed host release: %w", err)
	}

	result = SupervisionResult{
		ResidentLabel:  "com.mission-control." + names.Suffix + ".resident",
		DashboardLabel: "com.mission-control." + names.Suffix + ".dashboard",
		Root:           filepath.Join(canonicalHome, "supervision"),
	}
	if loaded == nil {
		return "", result, fmt.Errorf("launchd loaded-state probe is required")
	}
	labels := []string{result.ResidentLabel, result.DashboardLabel}
	for _, label := range labels {
		isLoaded, err := loaded(label)
		if err != nil {
			return "", result, fmt.Errorf("inspect LaunchAgent %s: %w", label, err)
		}
		if isLoaded {
			return "", result, fmt.Errorf("LaunchAgent %s is already loaded; refuse offline configuration replacement", label)
		}
	}
	for _, item := range []struct{ path, label string }{
		{filepath.Join(canonicalHome, "logs"), "log root"},
		{filepath.Join(canonicalHome, "behaviors"), "behavior root"},
	} {
		if err := ensureOwnerDirectory(item.path, item.label); err != nil {
			return "", result, err
		}
	}

	resident := map[string]any{
		"agentCmd":          []string{"bun", "/app/src/agent-runner/main.ts"},
		"agentRunnerRoutes": []string{}, "behaviorsDir": filepath.Join(canonicalHome, "behaviors"),
		"configSchemaVersion": 1, "dockerPath": dockerPath,
		"homieCmd": []string{"bun", "/app/src/homie-runner/main.ts"},
		"image":    spec.Image, "landCmd": []string{"mc-land"}, "mcHome": canonicalHome,
		"mcPath": mcPath, "releaseBuildId": spec.ReleaseBuildID,
		"roleBehaviors": map[string]string{}, "runnerSrcDir": runnerRoot,
		"spineDbPath": "/mc/spine/spine.db", "spineVolume": names.Volume,
		"workspaceRoot": roots[0].Root, "workspaceRoots": roots,
	}
	residentJSON, err := json.MarshalIndent(resident, "", "  ")
	if err != nil {
		return "", result, err
	}
	residentJSON = append(residentJSON, '\n')
	dashboardJSON, _ := json.MarshalIndent(map[string]string{"bind": "127.0.0.1:7333", "mc_bin": mcPath}, "", "  ")
	dashboardJSON = append(dashboardJSON, '\n')

	stage, err := os.MkdirTemp(canonicalHome, ".supervision-stage-")
	if err != nil {
		return "", result, err
	}
	defer os.RemoveAll(stage)
	if err := os.Mkdir(filepath.Join(stage, "LaunchAgents"), 0o700); err != nil {
		return "", result, err
	}
	residentConfig := filepath.Join(result.Root, "resident.json")
	dashboardConfig := filepath.Join(result.Root, "dashboard.json")
	pathDirs := []string{filepath.Dir(mcPath), filepath.Dir(bunPath), filepath.Dir(dockerPath), "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	seenPath := map[string]bool{}
	pathEnv := []string{}
	for _, dir := range pathDirs {
		if !seenPath[dir] {
			seenPath[dir] = true
			pathEnv = append(pathEnv, dir)
		}
	}
	logRoot := filepath.Join(canonicalHome, "logs")
	residentPlist := launchAgent(result.ResidentLabel, bunPath,
		filepath.Join(hostRoot, "resident", "src", "main.ts"), residentConfig,
		filepath.Join(hostRoot, "resident"), canonicalHome, strings.Join(pathEnv, ":"),
		filepath.Join(logRoot, "resident.stdout.log"), filepath.Join(logRoot, "resident.stderr.log"))
	dashboardPlist := launchAgent(result.DashboardLabel, bunPath,
		filepath.Join(hostRoot, "dashboard", "src", "main.ts"), dashboardConfig,
		filepath.Join(hostRoot, "dashboard"), canonicalHome, strings.Join(pathEnv, ":"),
		filepath.Join(logRoot, "dashboard.stdout.log"), filepath.Join(logRoot, "dashboard.stderr.log"))
	for rel, body := range map[string][]byte{
		"resident.json": residentJSON, "dashboard.json": dashboardJSON,
		"LaunchAgents/resident.plist": residentPlist, "LaunchAgents/dashboard.plist": dashboardPlist,
	} {
		if err := writeReleaseFile(stage, rel, body); err != nil {
			return "", result, fmt.Errorf("stage supervision file %q: %w", rel, err)
		}
	}
	if err := syncDirectory(filepath.Join(stage, "LaunchAgents")); err != nil {
		return "", result, err
	}
	if err := syncDirectory(stage); err != nil {
		return "", result, err
	}
	if err := validateSupervisionTree(stage); err != nil {
		return "", result, err
	}

	status := "done"
	if info, statErr := os.Lstat(result.Root); statErr == nil {
		uid, _, ok := statOwner(info)
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || int(uid) != os.Getuid() {
			return "", result, fmt.Errorf("supervision root must remain a real operator-owned directory")
		}
		if err := validateSupervisionTree(result.Root); err == nil {
			equal, err := supervisionTreesEqual(stage, result.Root)
			if err != nil {
				return "", result, err
			}
			if equal {
				status = "ok"
			} else if err := atomicExchangeDirectories(stage, result.Root); err != nil {
				return "", result, err
			} else if err := os.RemoveAll(stage); err != nil {
				return "", result, err
			}
		} else if err := atomicExchangeDirectories(stage, result.Root); err != nil {
			return "", result, err
		} else if err := os.RemoveAll(stage); err != nil {
			return "", result, err
		}
	} else if errors.Is(statErr, fs.ErrNotExist) {
		if err := os.Rename(stage, result.Root); err != nil {
			return "", result, err
		}
	} else {
		return "", result, statErr
	}
	if err := syncDirectory(canonicalHome); err != nil {
		return "", result, err
	}
	for _, label := range labels {
		isLoaded, err := loaded(label)
		if err != nil {
			return "", result, fmt.Errorf("reinspect LaunchAgent %s: %w", label, err)
		}
		if isLoaded {
			return "", result, fmt.Errorf("LaunchAgent %s became loaded during offline publication", label)
		}
	}
	return status, result, nil
}
