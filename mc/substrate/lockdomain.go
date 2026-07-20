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

// checkLockDomain reports whether every given path sits on a filesystem inside
// the lock domain, deciding from mountinfo content. It is pure so the decision
// is provable on every host, not only where /proc exists.
//
// Callers pass BOTH the spine's directory and the spine file itself. The
// directory alone is not enough: Docker will happily bind a single file over
// `/mc/spine/spine.db` inside an otherwise-legitimate named volume, which puts
// the database on VirtioFS while its directory still reports ext4. Checking
// the file path catches that, because a single-file bind appears in mountinfo
// as a mount point equal to the file.
func checkLockDomain(mountinfo io.Reader, paths ...string) error {
	entries, err := parseMountInfo(mountinfo)
	if err != nil {
		return lockDomainRefusal(paths[0], "cannot read /proc/self/mountinfo (%v)", err)
	}
	for _, path := range paths {
		best := longestPrefixMount(entries, path)
		if best == nil {
			// Only the directory must be accounted for; a spine file that does
			// not exist yet has no mount line of its own and rides its
			// directory's, which was already checked.
			if path == paths[0] {
				return lockDomainRefusal(path, "no mount in /proc/self/mountinfo contains it")
			}
			continue
		}
		if !allowedFstypes[best.fstype] || !strings.HasPrefix(best.source, "/dev/") {
			return lockDomainRefusal(path,
				"its mount %s is fstype=%s source=%s, which is not a block-device-backed local filesystem",
				best.mountPoint, best.fstype, best.source)
		}
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
//
// Ties go to the LAST entry, which is kernel semantics, not a preference.
// mountinfo is ordered, and mounting twice at the same point shadows rather
// than replaces: both lines remain, and the later one is what processes
// actually see. Keeping the first would let a bind stacked over a named volume
// at the very same path be judged on the volume's ext4 line while the process
// reads and writes the bind.
func longestPrefixMount(entries []mountEntry, dir string) *mountEntry {
	var best *mountEntry
	for i := range entries {
		mp := entries[i].mountPoint
		// Whole-path-component containment only: /mc/spine must not match
		// /mc/spineless.
		if dir != mp && !strings.HasPrefix(dir, strings.TrimSuffix(mp, "/")+"/") {
			continue
		}
		if best == nil || len(mp) >= len(best.mountPoint) {
			best = &entries[i]
		}
	}
	return best
}

// resolveExistingAncestor resolves symlinks in path, walking up to the nearest
// existing ancestor when the path has not been created yet. A spine is
// provisioned into a directory that may not exist, and the mount that will
// hold it is the one governing its nearest existing ancestor.
func resolveExistingAncestor(path string) string {
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// parseMountInfo reads the mountinfo format: space-separated fields, a
// variable-length optional-fields run, then a single " - " separator, then
// fstype and source.
//
// A line that does not parse is an ERROR, not a line to skip. Skipping is a
// false-accept generator: drop the line describing the bind at /mc/spine and
// the longest remaining prefix becomes its parent — which on a Linux host is
// an ext4 root that passes. The guard would then approve the very mount it
// could not read. If this kernel's mountinfo is a shape we do not understand,
// the honest answer is refusal.
func parseMountInfo(r io.Reader) ([]mountEntry, error) {
	var out []mountEntry
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		// Fields 1..6 precede the separator (mountinfo's fixed head), and
		// fstype+source follow it.
		if sep < 6 || len(fields) < sep+3 {
			return nil, fmt.Errorf("unparseable mountinfo line %q", line)
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

// openMountinfo yields the mount table to decide from. It is a package
// variable ONLY so the package's own tests can pin that the guard is actually
// wired into the spine openers — the fast lane runs on darwin, where the
// platform source is inert, so deleting a call site would otherwise turn no
// test red. It is unexported and settable from nowhere outside this package:
// there is deliberately no env var, no flag, and no exported setter.
var openMountinfo = platformMountinfo

// GuardLockDomain refuses a spine path that is not inside the lock domain. It
// is called before every open of the spine — substrate.Open and the onboard
// read-only inspection alike — so no code path can reach sql.Open on a
// bind-mounted database.
//
// It checks the spine's DIRECTORY and the spine FILE. The directory because
// SQLite's -wal and -shm siblings live beside the database and are equally
// lock-bearing, and because the guard must also judge a spine that does not
// exist yet; the file because Docker will bind a single file over an
// otherwise-legitimate volume.
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
	//
	// The FILE is resolved separately and not merely rejoined to the resolved
	// directory. A spine file that is itself a symlink into a bind would
	// otherwise be judged on its directory's blameless mount while sql.Open
	// follows the link onto the bind — the exact laundering this resolution
	// exists to stop, one path component further down.
	resolved := resolveExistingAncestor(dir)
	file := resolveExistingAncestor(filepath.Join(resolved, filepath.Base(spinePath)))

	r, err := openMountinfo()
	if err != nil {
		return lockDomainRefusal(resolved, "cannot read the mount table (%v)", err)
	}
	if r == nil {
		return nil // platform without /proc — see lockdomain_other.go.
	}
	defer r.Close()
	return checkLockDomain(r, resolved, file)
}
