package boundary

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// ADR-017's stable rejection codes. Every code aborts the whole plan; none
// drops one mount.
const (
	CodeAllowlistUntrusted = "mount.allowlist_untrusted"
	CodeAllowlistInvalid   = "mount.allowlist_invalid"
	CodeSourceMissing      = "mount.source_missing"
	CodeSourceWrongKind    = "mount.source_wrong_kind"
	CodeSourceBlocked      = "mount.source_blocked"
	CodeSymlinkEscape      = "mount.symlink_escape"
	CodeNotAllowlisted     = "mount.not_allowlisted"
	CodeDeniedRoot         = "mount.denied_root"
	CodeCrossWorksource    = "mount.cross_worksource"
	CodeRWNotPermitted     = "mount.rw_not_permitted"
	CodeTargetInvalid      = "mount.target_invalid"
	CodeSourceAlias        = "mount.source_alias"
	CodeTargetCollision    = "mount.target_collision"
	CodeIdentityChanged    = "mount.identity_changed"
	CodeRuntimeUnappliable = "mount.runtime_unappliable"
)

// MountError is a boundary rejection carrying one ADR-017 code, mirroring the
// domain's coded-rejection convention.
type MountError struct {
	Code string
	Msg  string
}

func (e *MountError) Error() string { return e.Msg }

func mountErrf(code, format string, a ...any) error {
	return &MountError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// TrustPolicyFile enforces the ADR-017 Decision 1 trust seam for the
// allowlist: a non-symlink regular file owned by the real operator uid, with
// owner read/write and no group or other permission bits. A stricter owner
// mode is not a grant, so it is accepted.
//
// The macOS ACL leg ("any allow ACE granting a non-owner access rejects") is
// NOT implemented here; it needs the native ACL API and is carried as an
// explicit obligation in PROGRESS.md.
func TrustPolicyFile(path string, ownerUID int) error {
	info, err := trustedLstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return mountErrf(CodeAllowlistUntrusted, "mount-allowlist %q is not a regular file", path)
	}
	return trustedOwnerMode(path, info, ownerUID)
}

// TrustHomeDir enforces the same seam for MC_HOME: a non-symlink
// operator-owned directory with owner rwx and no group or other bits.
func TrustHomeDir(path string, ownerUID int) error {
	info, err := trustedLstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return mountErrf(CodeAllowlistUntrusted, "MC_HOME %q is not a directory", path)
	}
	return trustedOwnerMode(path, info, ownerUID)
}

// trustedLstat refuses to follow the final component: a symlink to an
// otherwise-trusted object is not itself trusted.
func trustedLstat(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, mountErrf(CodeAllowlistUntrusted, "cannot stat %q: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, mountErrf(CodeAllowlistUntrusted, "%q is a symlink", path)
	}
	return info, nil
}

func trustedOwnerMode(path string, info fs.FileInfo, ownerUID int) error {
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return mountErrf(CodeAllowlistUntrusted, "%q mode %04o grants group or other access", path, perm)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return mountErrf(CodeAllowlistUntrusted, "cannot read ownership of %q", path)
	}
	if int(stat.Uid) != ownerUID {
		return mountErrf(CodeAllowlistUntrusted, "%q is owned by uid %d, not the operator uid %d", path, stat.Uid, ownerUID)
	}
	return nil
}

// SourceIdentity is one canonically resolved mount source. RawClean is the
// address as spelled (absolute and lexically clean); Canonical is that address
// with every symlink resolved. Both are retained because the blocked floor is
// matched against each.
type SourceIdentity struct {
	Raw       string
	RawClean  string
	Canonical string
	Info      fs.FileInfo
	IsDir     bool
}

// ResolveSource performs ADR-017 Decision 3 steps 1-2: reject non-absolute,
// missing, NUL/newline-containing, or wrong-kind sources, then compute
// Abs -> Clean -> EvalSymlinks and stat the resolved object.
func ResolveSource(source string) (SourceIdentity, error) {
	if source == "" {
		return SourceIdentity{}, mountErrf(CodeSourceWrongKind, "source is empty")
	}
	if strings.ContainsAny(source, "\x00\r\n") {
		return SourceIdentity{}, mountErrf(CodeSourceWrongKind, "source %q contains NUL or newline", source)
	}
	if !filepath.IsAbs(source) {
		return SourceIdentity{}, mountErrf(CodeSourceWrongKind, "source %q is not absolute", source)
	}

	rawClean := filepath.Clean(source)
	canonical, err := filepath.EvalSymlinks(rawClean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SourceIdentity{}, mountErrf(CodeSourceMissing, "source %q does not exist", source)
		}
		return SourceIdentity{}, mountErrf(CodeSourceMissing, "cannot resolve source %q: %v", source, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return SourceIdentity{}, mountErrf(CodeSourceMissing, "cannot stat resolved source %q: %v", canonical, err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return SourceIdentity{}, mountErrf(CodeSourceWrongKind, "source %q is neither a directory nor a regular file", source)
	}
	return SourceIdentity{
		Raw:       source,
		RawClean:  rawClean,
		Canonical: canonical,
		Info:      info,
		IsDir:     info.IsDir(),
	}, nil
}

type resolvedRoot struct {
	entry     AllowEntry
	canonical string
	info      fs.FileInfo
}

// ResolvedAllowlist is a MountAllowlist bound to real filesystem objects.
// Construct it with ResolveAllowlist.
type ResolvedAllowlist struct {
	roots []resolvedRoot
}

// ResolveAllowlist canonicalizes every allow root and enforces ADR-017
// Decision 1's uniqueness law with filesystem identity rather than string
// arithmetic: canonically identical and ancestor/descendant-overlapping roots
// reject, so every source has exactly one authorization root and one maximum
// mode.
func ResolveAllowlist(allowlist MountAllowlist) (ResolvedAllowlist, error) {
	roots := make([]resolvedRoot, 0, len(allowlist.entries))
	for _, e := range allowlist.entries {
		identity, err := ResolveSource(e.Path)
		if err != nil {
			return ResolvedAllowlist{}, err
		}
		roots = append(roots, resolvedRoot{entry: e, canonical: identity.Canonical, info: identity.Info})
	}

	for i := range roots {
		for j := 0; j < i; j++ {
			if os.SameFile(roots[i].info, roots[j].info) {
				return ResolvedAllowlist{}, mountErrf(CodeSourceAlias,
					"allow roots %q and %q are the same filesystem object", roots[j].entry.Path, roots[i].entry.Path)
			}
			if enclosesByIdentity(roots[j], roots[i].canonical) || enclosesByIdentity(roots[i], roots[j].canonical) {
				return ResolvedAllowlist{}, mountErrf(CodeSourceAlias,
					"allow roots %q and %q overlap", roots[j].entry.Path, roots[i].entry.Path)
			}
		}
	}
	return ResolvedAllowlist{roots: roots}, nil
}

func enclosesByIdentity(ancestor resolvedRoot, canonical string) bool {
	for current := canonical; ; {
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		info, err := os.Stat(parent)
		if err == nil && os.SameFile(info, ancestor.info) {
			return true
		}
		current = parent
	}
}

// Authorization is the evidence that one source may be mounted: its canonical
// identity, the single allow root that authorized it, the suffix derived from
// that identity walk, and the resolved access mode.
type Authorization struct {
	Source    SourceIdentity
	RootIndex int
	Target    string
	Suffix    string
	Access    Access
}

type rootMatch struct {
	index  int
	suffix string
}

// Authorize runs ADR-017 Decision 3 steps 2-4 and Decision 4's access table
// for one ordinary source. Protected-root and cross-Worksource jurisdiction
// (step 5) are the following slice and are NOT applied here.
func (r ResolvedAllowlist) Authorize(source string, requested Access, blocked BlockPolicy) (Authorization, error) {
	identity, err := ResolveSource(source)
	if err != nil {
		return Authorization{}, err
	}

	// Step 3: the floor classifies both the address as spelled and the address
	// it resolves to, so a symlink cannot launder a denied name.
	if blocked.Rejects(identity.RawClean, identity.Canonical) {
		return Authorization{}, mountErrf(CodeSourceBlocked, "source %q has a blocked address component", source)
	}

	// Step 4: filesystem-identity ancestry, accepted only at exactly one root.
	matches := r.match(identity.Canonical)
	if len(matches) == 0 {
		if r.rawAddressWasInsideARoot(identity.RawClean) {
			return Authorization{}, mountErrf(CodeSymlinkEscape,
				"source %q resolves to %q, outside every allow root", source, identity.Canonical)
		}
		return Authorization{}, mountErrf(CodeNotAllowlisted, "source %q is under no allow root", source)
	}
	if len(matches) > 1 {
		return Authorization{}, mountErrf(CodeSourceAlias, "source %q is under %d allow roots", source, len(matches))
	}
	matched := matches[0]

	// The suffix comes from the identity walk, never from string arithmetic,
	// and is validated independently. A colon in the matched root's own
	// spelling is legal; a colon in a descendant suffix is not.
	if matched.suffix != "" {
		if err := ValidateTarget(matched.suffix); err != nil {
			return Authorization{}, mountErrf(CodeTargetInvalid, "source %q suffix %q: %v", source, matched.suffix, err)
		}
	}

	root := r.roots[matched.index]
	access, err := ResolveAccess(requested, root.entry.Maximum)
	if err != nil {
		return Authorization{}, mountErrf(CodeRWNotPermitted, "source %q: %v", source, err)
	}
	return Authorization{
		Source:    identity,
		RootIndex: matched.index,
		Target:    root.entry.Target,
		Suffix:    matched.suffix,
		Access:    access,
	}, nil
}

// match walks the canonical source up to the filesystem root, comparing every
// ancestor to every allow root by identity and accumulating the suffix below
// each hit. It returns every root encountered so an ambiguous chain can be
// refused rather than silently resolved to the first hit.
func (r ResolvedAllowlist) match(canonical string) []rootMatch {
	var matches []rootMatch
	var below []string

	for current := canonical; ; {
		if info, err := os.Stat(current); err == nil {
			for i, root := range r.roots {
				if os.SameFile(info, root.info) {
					matches = append(matches, rootMatch{index: i, suffix: joinReversed(below)})
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return matches
		}
		below = append(below, filepath.Base(current))
		current = parent
	}
}

// rawAddressWasInsideARoot reports whether the directory containing the
// address as spelled is inside an allow root. It separates an escaping symlink
// (the link lives in the root, its target does not) from an address that was
// never in jurisdiction at all.
func (r ResolvedAllowlist) rawAddressWasInsideARoot(rawClean string) bool {
	parent, err := filepath.EvalSymlinks(filepath.Dir(rawClean))
	if err != nil {
		return false
	}
	return len(r.match(parent)) > 0
}

func joinReversed(below []string) string {
	if len(below) == 0 {
		return ""
	}
	ordered := make([]string, 0, len(below))
	for i := len(below) - 1; i >= 0; i-- {
		ordered = append(ordered, below[i])
	}
	return path.Join(ordered...)
}
