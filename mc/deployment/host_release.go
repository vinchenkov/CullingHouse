package deployment

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var hostReleaseFiles = []string{
	"dashboard/src/api.ts",
	"dashboard/src/main.ts",
	"dashboard/src/mc.ts",
	"dashboard/src/server.ts",
	"dashboard/src/ui/app.js",
	"dashboard/src/ui/index.html",
	"dashboard/src/ui/style.css",
	"resident/src/credential-projector.ts",
	"resident/src/effects.ts",
	"resident/src/main.ts",
	"resident/src/refresh-broker.ts",
	"resident/src/resident-control.ts",
	"resident/src/task-skeleton.ts",
	"resident/src/tick-loop.ts",
	"resident/src/token-service.ts",
	"resident/src/types.ts",
}

var hostReleaseDirs = []string{
	".",
	"dashboard",
	"dashboard/src",
	"dashboard/src/ui",
	"resident",
	"resident/src",
}

// ValidateHostRelease closes the two native host programs over their exact
// transitive source/UI graph. Tests, lockfiles, and package metadata are not
// executable deployment inputs.
func ValidateHostRelease(root string) error {
	expectedDirs := make(map[string]bool, len(hostReleaseDirs))
	for _, rel := range hostReleaseDirs {
		expectedDirs[rel] = true
	}
	expectedFiles := make(map[string]bool, len(hostReleaseFiles))
	for _, rel := range hostReleaseFiles {
		expectedFiles[rel] = true
	}
	seenFiles := map[string]bool{}
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
			return fmt.Errorf("installed host entry %q lost operator ownership or became a symlink", rel)
		}
		if entry.IsDir() {
			if !expectedDirs[rel] || info.Mode().Perm() != 0o700 {
				return fmt.Errorf("installed host release contains unexpected or non-owner-only directory %q", rel)
			}
			return nil
		}
		if !expectedFiles[rel] || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || links != 1 ||
			info.Size() <= 0 || info.Size() > maxRunnerReleaseFileBytes {
			return fmt.Errorf("installed host release contains unexpected or unsafe file %q", rel)
		}
		seenFiles[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	for _, rel := range hostReleaseFiles {
		if !seenFiles[rel] {
			return fmt.Errorf("installed host release is missing %q", rel)
		}
	}
	return nil
}

func hostReleasesEqual(left, right string) (bool, error) {
	for _, rel := range hostReleaseFiles {
		leftBody, err := os.ReadFile(filepath.Join(left, filepath.FromSlash(rel)))
		if err != nil {
			return false, err
		}
		rightBody, err := os.ReadFile(filepath.Join(right, filepath.FromSlash(rel)))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(leftBody, rightBody) {
			return false, nil
		}
	}
	return true, nil
}

// InstallHostRelease publishes resident/dashboard source independently of the
// clone. It is a sibling of the runner payload so agent containers never gain
// visibility into native-host source.
func InstallHostRelease(home, repoRoot string) (string, error) {
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		return "", fmt.Errorf("resolve host-release MC_HOME: %w", err)
	}
	if err := requireOwnerOnlyDirectory(canonicalHome, "MC_HOME"); err != nil {
		return "", err
	}
	if repoRoot == "" || !filepath.IsAbs(repoRoot) {
		return "", fmt.Errorf("host release source must be an absolute repository root")
	}
	repoRoot = filepath.Clean(repoRoot)
	if err := requireOperatorSourceDirectory(repoRoot, "host release source root"); err != nil {
		return "", err
	}
	bodies := make(map[string][]byte, len(hostReleaseFiles))
	for _, rel := range hostReleaseFiles {
		body, err := readReleaseSource(repoRoot, rel)
		if err != nil {
			return "", err
		}
		bodies[rel] = body
	}
	releaseRoot := filepath.Join(canonicalHome, "release")
	if err := os.Mkdir(releaseRoot, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("create release root: %w", err)
	}
	if err := requireOwnerOnlyDirectory(releaseRoot, "release root"); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(releaseRoot, ".host-stage-")
	if err != nil {
		return "", fmt.Errorf("create host release stage: %w", err)
	}
	defer os.RemoveAll(stage)
	for _, rel := range hostReleaseDirs[1:] {
		if err := os.Mkdir(filepath.Join(stage, filepath.FromSlash(rel)), 0o700); err != nil {
			return "", fmt.Errorf("stage host directory %q: %w", rel, err)
		}
	}
	for _, rel := range hostReleaseFiles {
		if err := writeReleaseFile(stage, rel, bodies[rel]); err != nil {
			return "", fmt.Errorf("stage host file %q: %w", rel, err)
		}
	}
	for i := len(hostReleaseDirs) - 1; i >= 0; i-- {
		if err := syncDirectory(filepath.Join(stage, filepath.FromSlash(hostReleaseDirs[i]))); err != nil {
			return "", fmt.Errorf("sync staged host directory: %w", err)
		}
	}
	if err := ValidateHostRelease(stage); err != nil {
		return "", fmt.Errorf("validate staged host release: %w", err)
	}
	canonical := filepath.Join(releaseRoot, "host")
	info, statErr := os.Lstat(canonical)
	if statErr == nil {
		uid, _, ok := statOwner(info)
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || int(uid) != os.Getuid() {
			return "", fmt.Errorf("installed host release %q must remain a real operator-owned directory", canonical)
		}
		if err := ValidateHostRelease(canonical); err == nil {
			equal, err := hostReleasesEqual(stage, canonical)
			if err != nil {
				return "", fmt.Errorf("compare installed host release: %w", err)
			}
			if equal {
				return "ok", nil
			}
		}
		if err := atomicExchangeDirectories(stage, canonical); err != nil {
			return "", fmt.Errorf("atomically exchange installed host release: %w", err)
		}
		if err := syncDirectory(releaseRoot); err != nil {
			return "", fmt.Errorf("sync release root after host exchange: %w", err)
		}
		if err := os.RemoveAll(stage); err != nil {
			return "", fmt.Errorf("remove replaced host release: %w", err)
		}
		if err := syncDirectory(releaseRoot); err != nil {
			return "", fmt.Errorf("sync release root after replaced host removal: %w", err)
		}
		return "done", nil
	}
	if !errors.Is(statErr, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect installed host release: %w", statErr)
	}
	if err := os.Rename(stage, canonical); err != nil {
		return "", fmt.Errorf("publish installed host release: %w", err)
	}
	if err := syncDirectory(releaseRoot); err != nil {
		return "", fmt.Errorf("sync release root after host publication: %w", err)
	}
	return "done", nil
}
