package substrate

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// The lock-domain guard (Invariant 24, spec §4 line 41).
//
// SQLite is only safe when ONE operating-system kernel arbitrates its locks.
// The spine therefore lives on a volume local to the container runtime's
// kernel, never on a shared host path. Spike S5 proved this guard's exact
// shape (spikes/05-sqlite-wal/RESULT.md row 5): parse /proc/self/mountinfo,
// longest-prefix match the spine's directory, and ALLOWLIST only
// block-device-backed local filesystems — refusing before sql.Open, so a
// spine outside the lock domain is never opened at all rather than opened and
// corrupted later.
//
// The allowlist shape is load-bearing, not stylistic. On this Docker Desktop
// (VirtioFS enabled) a host bind surfaces as `fstype=fakeowner
// source=/run/host_mark/Users` — Docker Desktop's FUSE ownership shim, not
// "virtiofs". A denylist keyed on "virtiofs" would have ACCEPTED the bind. The
// absence of this guard is what let the E2E fixture put the spine on a
// VirtioFS bind and corrupt it, which then cost four corrections chasing a
// misdiagnosed flake (docs/ledger/phase-3.md, 2026-07-19).

// allowedFstypes is an ALLOWLIST. Anything not named here is refused,
// including filesystems that have not been invented yet: fail-closed means an
// unrecognized mount is a refusal, never a default-accept.
var allowedFstypes = map[string]bool{
	"ext4":  true,
	"ext3":  true,
	"xfs":   true,
	"btrfs": true,
}

// mountEntry is the subset of a /proc/self/mountinfo line the guard decides on.
type mountEntry struct {
	mountPoint string // field 5
	fstype     string // first field after the " - " separator
	source     string // the field after fstype
}

// checkLockDomain reports whether dir sits on a filesystem inside the lock
// domain, deciding from mountinfo content. It is pure so the decision is
// provable on every host, not only where /proc exists.
func checkLockDomain(mountinfo io.Reader, dir string) error {
	entries, err := parseMountInfo(mountinfo)
	if err != nil {
		return lockDomainRefusal(dir, "cannot read /proc/self/mountinfo (%v)", err)
	}
	best := longestPrefixMount(entries, dir)
	if best == nil {
		return lockDomainRefusal(dir, "no mount in /proc/self/mountinfo contains it")
	}
	if !allowedFstypes[best.fstype] || !strings.HasPrefix(best.source, "/dev/") {
		return lockDomainRefusal(dir,
			"its mount %s is fstype=%s source=%s, which is not a block-device-backed local filesystem",
			best.mountPoint, best.fstype, best.source)
	}
	return nil
}

// lockDomainRefusal keeps every refusal citing the invariant and naming the
// remedy, so an operator who trips it is not left reading mount tables.
func lockDomainRefusal(dir, why string, args ...any) error {
	return fmt.Errorf("refusing to open the spine under %s: %s — the spine must live on a runtime-local named volume (Inv. 24), because a shared host path splits its locks across two kernels and corrupts it",
		dir, fmt.Sprintf(why, args...))
}

// longestPrefixMount returns the mount entry that actually governs dir: the
// containing mount with the longest mount point. Nesting matters — a bind
// mounted *inside* an allowed volume must be judged on its own line, not on
// its parent's.
func longestPrefixMount(entries []mountEntry, dir string) *mountEntry {
	var best *mountEntry
	for i := range entries {
		mp := entries[i].mountPoint
		// Whole-path-component containment only: /mc/spine must not match
		// /mc/spineless.
		if dir != mp && !strings.HasPrefix(dir, strings.TrimSuffix(mp, "/")+"/") {
			continue
		}
		if best == nil || len(mp) > len(best.mountPoint) {
			best = &entries[i]
		}
	}
	return best
}

// parseMountInfo reads the mountinfo format: space-separated fields, a
// variable-length optional-fields run, then a single " - " separator, then
// fstype and source. Lines that do not parse are skipped rather than guessed
// at; if that leaves no containing mount, checkLockDomain refuses.
func parseMountInfo(r io.Reader) ([]mountEntry, error) {
	var out []mountEntry
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		// Need fields 1..5 before the separator, and fstype+source after it.
		if sep < 5 || len(fields) < sep+3 {
			continue
		}
		out = append(out, mountEntry{
			mountPoint: unescapeOctal(fields[4]),
			fstype:     fields[sep+1],
			source:     unescapeOctal(fields[sep+2]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// unescapeOctal decodes the \040-style escapes mountinfo uses for space, tab,
// newline, and backslash in path fields. Without it a mount point containing a
// space never matches and the guard refuses a legitimate volume.
func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var v int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%o", &v); err == nil && v < 256 {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// GuardLockDomain refuses a spine path that is not inside the lock domain. It
// is called before every open of the spine — substrate.Open and the onboard
// read-only inspection alike — so no code path can reach sql.Open on a
// bind-mounted database.
//
// The check is on the spine's DIRECTORY, not the file: SQLite's -wal and -shm
// siblings live beside it and are equally lock-bearing, and the guard must
// also refuse a spine that does not exist yet.
func GuardLockDomain(spinePath string) error {
	if spinePath == "" {
		return nil // "no spine configured" is the caller's error to report.
	}
	dir, err := filepath.Abs(filepath.Dir(spinePath))
	if err != nil {
		return lockDomainRefusal(filepath.Dir(spinePath), "cannot resolve it (%v)", err)
	}
	// Resolve symlinks so a link into a bind cannot launder the path past the
	// longest-prefix match. A directory that does not exist yet is judged at
	// its nearest existing ancestor, which is the mount that will hold it.
	resolved := resolveExistingAncestor(dir)
	return guardLockDomainAt(resolved)
}
