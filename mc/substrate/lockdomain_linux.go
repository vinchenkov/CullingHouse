//go:build linux

package substrate

import (
	"os"
	"path/filepath"
)

// guardLockDomainAt is the real guard: on Linux, /proc/self/mountinfo exists
// and mc opens the spine for real, so the decision is made from the kernel's
// own mount table.
func guardLockDomainAt(dir string) error {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return lockDomainRefusal(dir, "cannot open /proc/self/mountinfo (%v)", err)
	}
	defer f.Close()
	return checkLockDomain(f, dir)
}

// resolveExistingAncestor resolves symlinks in dir, walking up to the nearest
// existing ancestor when the directory has not been created yet. A spine is
// provisioned into a directory that may not exist, and the mount that will
// hold it is the one governing its nearest existing ancestor.
func resolveExistingAncestor(dir string) string {
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return resolved
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
