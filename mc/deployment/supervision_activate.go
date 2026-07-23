package deployment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LaunchdController interface {
	Loaded(label string) (bool, error)
	Bootstrap(plist string) error
	Bootout(label string) error
}

type SupervisionActivationDeps struct {
	Launchd LaunchdController
	Now     func() time.Time
	Sleep   func(time.Duration)
}

type residentTickReceipt struct {
	Version             int    `json:"version"`
	ReleaseBuildID      string `json:"release_build_id"`
	ConfigSchemaVersion int    `json:"config_schema_version"`
	CompletedAt         string `json:"completed_at"`
}

func matchingTickReceipt(path, releaseBuildID string, notBefore, notAfter time.Time) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	uid, links, ok := statOwner(info)
	if !ok || int(uid) != os.Getuid() || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 || links != 1 || info.Size() <= 0 || info.Size() > 4096 {
		return false, fmt.Errorf("resident tick receipt is not an owner-only regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var receipt residentTickReceipt
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&receipt); err != nil {
		return false, fmt.Errorf("decode resident tick receipt: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return false, fmt.Errorf("resident tick receipt carries trailing JSON")
	}
	completed, err := time.Parse(time.RFC3339Nano, receipt.CompletedAt)
	if err != nil || receipt.Version != 1 || receipt.ReleaseBuildID != releaseBuildID ||
		receipt.ConfigSchemaVersion != 1 {
		return false, fmt.Errorf("resident tick receipt has mismatched release/schema identity")
	}
	if !notBefore.IsZero() && completed.Before(notBefore) {
		return false, nil
	}
	if !notAfter.IsZero() && completed.After(notAfter) {
		return false, fmt.Errorf("resident tick receipt timestamp is in the future")
	}
	return true, nil
}

func installLaunchAgent(source, destination string) (os.FileInfo, bool, error) {
	body, err := os.ReadFile(source)
	if err != nil {
		return nil, false, err
	}
	if existing, statErr := os.Lstat(destination); statErr == nil {
		uid, links, ok := statOwner(existing)
		current, readErr := os.ReadFile(destination)
		if readErr != nil || !ok || int(uid) != os.Getuid() || existing.Mode()&os.ModeSymlink != 0 ||
			!existing.Mode().IsRegular() || existing.Mode().Perm() != 0o644 || links != 1 || !bytes.Equal(current, body) {
			return nil, false, fmt.Errorf("installed LaunchAgent %q differs from the prepared owner-controlled definition", destination)
		}
		return existing, false, nil
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return nil, false, statErr
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".mc-launch-agent-")
	if err != nil {
		return nil, false, err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		return nil, false, err
	}
	if _, err := tmp.Write(body); err != nil {
		return nil, false, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, false, err
	}
	if err := tmp.Close(); err != nil {
		return nil, false, err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return nil, false, err
	}
	cleanup = false
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		identity, statErr := os.Lstat(destination)
		if statErr != nil {
			return nil, false, fmt.Errorf("sync installed LaunchAgent: %v; inspect rollback identity: %w", err, statErr)
		}
		return identity, true, err
	}
	info, err := os.Lstat(destination)
	return info, true, err
}

func removeInstalledLaunchAgent(path string, identity os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(identity, current) {
		return fmt.Errorf("installed LaunchAgent %q changed identity during rollback", path)
	}
	return os.Remove(path)
}

func verifyInstalledLaunchAgent(source, destination string) error {
	sourceBody, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	uid, links, ok := statOwner(info)
	destinationBody, readErr := os.ReadFile(destination)
	if readErr != nil || !ok || int(uid) != os.Getuid() || info.Mode()&os.ModeSymlink != 0 ||
		!info.Mode().IsRegular() || info.Mode().Perm() != 0o644 || links != 1 || !bytes.Equal(sourceBody, destinationBody) {
		return fmt.Errorf("installed LaunchAgent %q differs from its prepared definition", destination)
	}
	return nil
}

// ActivateSupervision is the operator-present half of §17.9. The prepared
// bundle remains the source of truth; this function installs those exact two
// plists, bootstraps both jobs, and requires a new release-bound resident tick
// receipt. Every partial first activation is rolled back.
func ActivateSupervision(home, launchAgentsDir, releaseBuildID string, deps SupervisionActivationDeps) (string, SupervisionResult, error) {
	var result SupervisionResult
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		return "", result, err
	}
	if !validProductionBuildID(releaseBuildID) {
		return "", result, fmt.Errorf("activation requires the immutable production release identity")
	}
	if deps.Launchd == nil || deps.Now == nil || deps.Sleep == nil {
		return "", result, fmt.Errorf("activation requires launchd, clock, and wait dependencies")
	}
	names, err := RuntimeNames(canonicalHome)
	if err != nil {
		return "", result, err
	}
	result = SupervisionResult{
		ResidentLabel:  "com.mission-control." + names.Suffix + ".resident",
		DashboardLabel: "com.mission-control." + names.Suffix + ".dashboard",
		Root:           filepath.Join(canonicalHome, "supervision"),
	}
	if err := validateSupervisionTree(result.Root); err != nil {
		return "", result, fmt.Errorf("validate prepared supervision: %w", err)
	}
	residentBody, err := os.ReadFile(filepath.Join(result.Root, "resident.json"))
	if err != nil || !bytes.Contains(residentBody, []byte(`"releaseBuildId": "`+releaseBuildID+`"`)) {
		return "", result, fmt.Errorf("prepared resident config does not match activation release")
	}
	if launchAgentsDir == "" || !filepath.IsAbs(launchAgentsDir) || filepath.Clean(launchAgentsDir) != launchAgentsDir {
		return "", result, fmt.Errorf("LaunchAgents directory must be a clean absolute path")
	}
	labels := []string{result.DashboardLabel, result.ResidentLabel}
	sources := []string{
		filepath.Join(result.Root, "LaunchAgents", "dashboard.plist"),
		filepath.Join(result.Root, "LaunchAgents", "resident.plist"),
	}
	destinations := []string{
		filepath.Join(launchAgentsDir, result.DashboardLabel+".plist"),
		filepath.Join(launchAgentsDir, result.ResidentLabel+".plist"),
	}
	for i, source := range sources {
		body, err := os.ReadFile(source)
		if err != nil || !bytes.Contains(body, []byte("<string>"+labels[i]+"</string>")) {
			return "", result, fmt.Errorf("prepared LaunchAgent does not carry expected label %s", labels[i])
		}
	}
	loaded := make([]bool, len(labels))
	for i, label := range labels {
		loaded[i], err = deps.Launchd.Loaded(label)
		if err != nil {
			return "", result, err
		}
	}
	if loaded[0] != loaded[1] {
		return "", result, fmt.Errorf("supervision is partially loaded; repair requires explicit operator cleanup")
	}
	if loaded[0] {
		for i := range labels {
			if err := verifyInstalledLaunchAgent(sources[i], destinations[i]); err != nil {
				return "", result, err
			}
		}
		ok, err := matchingTickReceipt(filepath.Join(canonicalHome, "health", "resident-tick.json"), releaseBuildID, time.Time{}, deps.Now().UTC().Add(5*time.Second))
		if err != nil || !ok {
			return "", result, fmt.Errorf("loaded supervision has no matching resident tick receipt: %v", err)
		}
		return "ok", result, nil
	}
	parent := filepath.Dir(launchAgentsDir)
	if err := requireOperatorSourceDirectory(parent, "LaunchAgents parent"); err != nil {
		return "", result, err
	}
	if err := os.Mkdir(launchAgentsDir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", result, err
	}
	if err := requireOperatorSourceDirectory(launchAgentsDir, "LaunchAgents directory"); err != nil {
		return "", result, err
	}
	created := make([]bool, len(labels))
	identities := make([]os.FileInfo, len(labels))
	bootstrapped := make([]bool, len(labels))
	rollback := func(primary error) error {
		problems := []string{primary.Error()}
		for i := len(labels) - 1; i >= 0; i-- {
			if bootstrapped[i] {
				if err := deps.Launchd.Bootout(labels[i]); err != nil {
					problems = append(problems, fmt.Sprintf("bootout %s: %v", labels[i], err))
				}
			}
		}
		for i := len(labels) - 1; i >= 0; i-- {
			if created[i] {
				if err := removeInstalledLaunchAgent(destinations[i], identities[i]); err != nil {
					problems = append(problems, err.Error())
				}
			}
		}
		return fmt.Errorf("supervision activation rolled back: %s", strings.Join(problems, "; "))
	}
	for i := range labels {
		identities[i], created[i], err = installLaunchAgent(sources[i], destinations[i])
		if err != nil {
			return "", result, rollback(err)
		}
	}
	started := deps.Now().UTC()
	for i := range labels {
		if err := deps.Launchd.Bootstrap(destinations[i]); err != nil {
			return "", result, rollback(fmt.Errorf("bootstrap %s: %w", labels[i], err))
		}
		bootstrapped[i] = true
	}
	for i, label := range labels {
		isLoaded, err := deps.Launchd.Loaded(label)
		if err != nil || !isLoaded {
			if err == nil {
				err = fmt.Errorf("job is not loaded")
			}
			return "", result, rollback(fmt.Errorf("verify %s: %w", label, err))
		}
		destinationBody, readErr := os.ReadFile(destinations[i])
		sourceBody, sourceErr := os.ReadFile(sources[i])
		if readErr != nil || sourceErr != nil || !bytes.Equal(destinationBody, sourceBody) {
			return "", result, rollback(fmt.Errorf("installed LaunchAgent %s changed after bootstrap", label))
		}
	}
	receiptPath := filepath.Join(canonicalHome, "health", "resident-tick.json")
	for attempt := 0; attempt < 90; attempt++ {
		ok, err := matchingTickReceipt(receiptPath, releaseBuildID, started, deps.Now().UTC().Add(2*time.Minute))
		if err != nil {
			return "", result, rollback(err)
		}
		if ok {
			return "done", result, nil
		}
		deps.Sleep(time.Second)
	}
	return "", result, rollback(fmt.Errorf("resident produced no successful release-bound tick within 90 seconds"))
}

// InspectActiveSupervision is the read-only doctor half: both exact jobs must
// be loaded from the prepared definitions and the resident must have produced
// a recent receipt for this release/schema.
func InspectActiveSupervision(home, launchAgentsDir, releaseBuildID string, launchd LaunchdController, now time.Time) (SupervisionResult, error) {
	var result SupervisionResult
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		return result, err
	}
	if !validProductionBuildID(releaseBuildID) || launchd == nil {
		return result, fmt.Errorf("supervision inspection lacks release or launchd identity")
	}
	names, err := RuntimeNames(canonicalHome)
	if err != nil {
		return result, err
	}
	result = SupervisionResult{
		ResidentLabel:  "com.mission-control." + names.Suffix + ".resident",
		DashboardLabel: "com.mission-control." + names.Suffix + ".dashboard",
		Root:           filepath.Join(canonicalHome, "supervision"),
	}
	if err := validateSupervisionTree(result.Root); err != nil {
		return result, fmt.Errorf("prepared supervision is invalid: %w", err)
	}
	labels := []string{result.DashboardLabel, result.ResidentLabel}
	sources := []string{
		filepath.Join(result.Root, "LaunchAgents", "dashboard.plist"),
		filepath.Join(result.Root, "LaunchAgents", "resident.plist"),
	}
	destinations := []string{
		filepath.Join(launchAgentsDir, result.DashboardLabel+".plist"),
		filepath.Join(launchAgentsDir, result.ResidentLabel+".plist"),
	}
	for i, label := range labels {
		loaded, err := launchd.Loaded(label)
		if err != nil {
			return result, err
		}
		if !loaded {
			return result, fmt.Errorf("LaunchAgent %s is not loaded", label)
		}
		if err := verifyInstalledLaunchAgent(sources[i], destinations[i]); err != nil {
			return result, err
		}
	}
	ok, err := matchingTickReceipt(filepath.Join(canonicalHome, "health", "resident-tick.json"), releaseBuildID, now.UTC().Add(-2*time.Minute), now.UTC().Add(5*time.Second))
	if err != nil {
		return result, err
	}
	if !ok {
		return result, fmt.Errorf("resident has no release-bound tick receipt in the last two minutes")
	}
	return result, nil
}
