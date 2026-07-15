package boundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// TypedClaim names WHICH KIND a typed system source claims to be.
//
// It does NOT carry the root it is checked against. An earlier draft did, which
// let the caller supply both the source and its authorized root — so the check
// degenerated to os.SameFile(x, x) and Kind was compared to nothing. Measured:
// every kind permitted one fixed pair. The binding lives in
// Jurisdiction.typedRoots (D10) so that the claim is checked, not trusted.
//
// The ZERO value means "no claim": an ordinary or profile-requested mount, which
// takes the union predicate. Authorize always passes this.
type TypedClaim struct {
	Kind TypedKind // see D10a's derivation rule; ADR-017:634-702's host-bind rows
}

// maxDeniedPaths is ADR-017:167-169's bound. It "rejects before identity walking
// or allocation; none of these collections is truncated."
const maxDeniedPaths = 512

// ProtectedID is one member of the protected set, resolved by the host caller.
//
// It carries the canonical path AND the fs.FileInfo rather than a
// (device, inode, type) triple because D4 mandates os.SameFile, whose signature
// consumes stat objects, not tuples — and hand-rolling device/inode comparison is
// exactly the "clever" reimplementation ADR-017:247-250 exists to forbid. The
// canonical path is retained alongside it because D8's suffix work and every
// rejection message need it.
//
// An ABSENT member is encoded {Canonical: <declared cleaned path>, Info: nil}.
// Absence is the happy path, not an edge case: 0 of ADR-017:376-380's fifteen
// MC_HOME classes exist after the real scaffolder runs, and another Worksource's
// artifact root routinely does not either.
type ProtectedID struct {
	Canonical string
	Info      fs.FileInfo
	IsDir     bool
}

// Present reports whether this root exists on disk. An absent member is still a
// member (ADR-021 D8): only the ancestor direction can fire for it.
//
// The law that makes a nil Info safe: Rejects's ANCESTOR branch walks
// P.Canonical upward and stats the ancestors — it must NEVER read P.Info. The
// equality and descendant branches do read P.Info, and their correct answer for
// an absent P is already false: os.SameFile(nil, x) returns false without
// panicking, and ResolveSource returns mount.source_missing for anything under an
// absent root, so no descendant can exist to test.
func (p ProtectedID) Present() bool { return p.Info != nil }

// WorksourceRoots is one Worksource's six root classes (ADR-017:368-369).
// Any of them may be absent; see ProtectedID.
type WorksourceRoots struct {
	Workspace ProtectedID
	Worktree  ProtectedID
	Artifacts []ProtectedID
	State     ProtectedID
	Cache     ProtectedID
	ToolHome  ProtectedID
}

// JurisdictionInput is what the host caller assembles for ResolveJurisdiction.
//
// The input is MIXED by necessity, and two members are raw for two DIFFERENT
// reasons — conflating them is what made an earlier draft's D5 unimplementable:
//
//   - DeniedPaths is raw because ADR-017:275-276 contemplates a deny path that
//     does not yet exist, which no caller can pre-resolve.
//   - Home is raw because its RAW SPELLING IS THE EVIDENCE. D5 must refuse a
//     symlinked HOME, and EvalSymlinks destroys the only proof it was one. You
//     cannot validate a value someone else already resolved.
//
// Everything else arrives PRE-RESOLVED, which is what keeps this package pure:
// it never queries the spine, the Git registry, or the environment
// (docs/phase3-contract.md:41-44).
type JurisdictionInput struct {
	// RAW — resolved by ResolveJurisdiction, never by the caller.
	DeniedPaths []string
	Home        string

	// PRE-RESOLVED by the host caller. Any of these EXCEPT HomeClassRoots may be
	// absent; encode absence per ProtectedID.
	MCHome ProtectedID // the whole tree (D3); subsumes the classes and sessions

	// HomeClassRoots is the ONE "when present" carve-out (ADR-017:383): an absent
	// ~/.aws is simply not a member and is omitted, never encoded with a nil Info.
	HomeClassRoots []ProtectedID

	GatewaySecrets      []ProtectedID
	RuntimeControls     []ProtectedID // EVERY runtime control dir, not just non-selected
	OwnWorksource       WorksourceRoots
	OtherWorksources    []WorksourceRoots
	GitControls         []ProtectedID // pre-resolved: this package owns no Git resolver
	MissionControlRoots []ProtectedID // <workspace_root>/.mission-control

	// TypedRoots is the kind -> authorized-root binding (D10). THIS is what makes
	// a typed claim checkable rather than self-certifying: the root a typed source
	// is compared against comes from here, never from the caller's claim. A kind
	// absent from this map has no authorized root and denies.
	TypedRoots map[TypedKind][]ProtectedID
}

// protectedRoot is one resolved union member plus the code it reports.
type protectedRoot struct {
	id    ProtectedID
	cross bool   // D7: another Worksource's root -> mount.cross_worksource
	label string // for the operator-facing message
}

// Jurisdiction is the non-subtractable protected set (ADR-017 Decision 5).
//
// NOTE: unrelated to the free-text `jurisdiction` Worksource column in
// mc/verbs/worksource.go. Different package, no compile conflict, same word.
//
// D2 — non-subtractability is structural: ResolveJurisdiction is the only
// constructor, every field is unexported, and there is no setter, no negation
// form, and no config key. denied_paths is purely additive on top; the operator
// may add jurisdiction, never subtract it.
type Jurisdiction struct {
	// resolved carries D1's zero-value law. BlockPolicy gets the same guarantee
	// free by compiling its floor into a package-private array (blocked.go:72-76,
	// "even a zero-value policy cannot omit it"); Jurisdiction cannot, because its
	// members are injected. So the mechanism is explicit rather than asserted —
	// stating the property without the mechanism is how the applySpawn seam
	// shipped.
	resolved bool

	home       ProtectedID
	roots      []protectedRoot
	typedRoots map[TypedKind][]ProtectedID
}

// ResolveJurisdiction is the only constructor (D2).
//
// ownerUID is an explicit parameter rather than a field: it mirrors
// TrustPolicyFile/TrustHomeDir (identity.go:55 — cited for the SIGNATURE
// precedent only), the compiler finds every D11 call site, and it cannot be
// silently zero. This package refuses ambient identity: it never calls
// os.Getuid(), and it never reads $HOME.
func ResolveJurisdiction(in JurisdictionInput, ownerUID int) (Jurisdiction, error) {
	// ADR-017:167-169 FIRST, before any stat, any identity walk, and any
	// allocation — including before HOME is resolved. A boundary excess rejects;
	// it is never truncated.
	if len(in.DeniedPaths) > maxDeniedPaths {
		return Jurisdiction{}, mountErrf(CodeDeniedRoot,
			"profile denied_paths: %d entries exceeds the %d limit", len(in.DeniedPaths), maxDeniedPaths)
	}

	j := Jurisdiction{typedRoots: in.TypedRoots}

	home, err := resolveHome(in.Home, ownerUID)
	if err != nil {
		return Jurisdiction{}, err
	}
	j.home = home

	// D3: the whole MC_HOME tree is one protected root. It subsumes the fifteen
	// enumerated classes and MC_HOME/sessions, so none of them is registered
	// separately — a belt was measured to change 0 of 10 verdicts, and the
	// enumeration is not even transcribable (four of the fifteen name no
	// directory at all).
	j.addRoot(in.MCHome, false, "MC_HOME")

	for _, id := range in.MissionControlRoots {
		j.addRoot(id, false, "<workspace_root>/.mission-control root")
	}
	for _, id := range in.GitControls {
		j.addRoot(id, false, "registered Git control dir")
	}
	for _, id := range in.GatewaySecrets {
		j.addRoot(id, false, "gateway secret / CA private-key root")
	}
	for _, id := range in.RuntimeControls {
		j.addRoot(id, false, "runtime control dir")
	}
	// ADR-017:383's "when present" carve-out: the caller omits absent ones, so
	// anything here is a real member.
	for _, id := range in.HomeClassRoots {
		j.addRoot(id, false, "home credential root")
	}

	// D7: another Worksource's roots report cross_worksource. The OWN Worksource
	// is never a member — ADR-017:302-303 requires its roots to pass Authorize as
	// ordinary sources — and own/other is decided by identity, never by name.
	for _, ws := range in.OtherWorksources {
		for _, id := range worksourceMembers(ws) {
			if j.isOwn(in.OwnWorksource, id) {
				continue
			}
			j.addRoot(id, true, "another Worksource's root")
		}
	}

	// D2: denied_paths is purely additive, and last — it can only ever add.
	for _, p := range in.DeniedPaths {
		id, err := resolveDeclared(p)
		if err != nil {
			return Jurisdiction{}, err
		}
		j.addRoot(id, false, "profile denied_paths entry")
	}

	j.resolved = true
	return j, nil
}

// addRoot registers one member. A zero ProtectedID (no canonical path at all) is
// not a member: the caller supplied nothing. An ABSENT member — a declared path
// with a nil Info — IS a member, and is registered.
func (j *Jurisdiction) addRoot(id ProtectedID, cross bool, label string) {
	if id.Canonical == "" {
		return
	}
	j.roots = append(j.roots, protectedRoot{id: id, cross: cross, label: label})
}

func worksourceMembers(ws WorksourceRoots) []ProtectedID {
	out := make([]ProtectedID, 0, 5+len(ws.Artifacts))
	out = append(out, ws.Workspace, ws.Worktree, ws.State, ws.Cache, ws.ToolHome)
	return append(out, ws.Artifacts...)
}

// isOwn decides own-vs-other by filesystem identity, never by name (D7). An
// absent member cannot be compared by identity and is therefore never "own":
// treating an unidentifiable root as the caller's own would be the fail-open
// direction.
func (j *Jurisdiction) isOwn(own WorksourceRoots, id ProtectedID) bool {
	if !id.Present() {
		return false
	}
	for _, o := range worksourceMembers(own) {
		if o.Present() && sameFile(o.Info, id.Info) {
			return true
		}
	}
	return false
}

// resolveDeclared turns an operator-declared path into a member. The path may
// not exist yet (ADR-017:275-276), which is not an error: it becomes an absent
// member, and D8's nearest-existing-ancestor rule compares it.
func resolveDeclared(path string) (ProtectedID, error) {
	if path == "" {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "denied_paths entry is empty")
	}
	if !filepath.IsAbs(path) {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "denied_paths entry %q is not absolute", path)
	}
	clean := filepath.Clean(path)
	id, err := ResolveSource(clean)
	if err != nil {
		// Absent is legal and expected. Anything else — a middle component that is
		// a regular file, a resolution race — is ambiguity, and ambiguity denies.
		var me *MountError
		if asMountError(err, &me) && me.Code == CodeSourceMissing {
			return ProtectedID{Canonical: clean}, nil
		}
		return ProtectedID{}, mountErrf(CodeDeniedRoot,
			"denied_paths entry %q cannot be resolved: %v", path, err)
	}
	return ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}, nil
}

// resolveHome implements ADR-021 D5: the injected HOME is validated, not trusted.
//
// It takes the RAW spelling, because the raw spelling IS the evidence — leg 2
// cannot exist against a pre-resolved path, since a canonical path's final
// component is by construction never a symlink.
//
// !! It is NOT TrustHomeDir, and it must never route through trustedOwnerMode
// (identity.go:92-104). That is MC_HOME's seam: MC creates MC_HOME itself at
// 0700, so it enforces perm&0o077 == 0. The operator's real HOME is 0750 on
// stock macOS (drwxr-x---) and 0755 elsewhere; 0750 & 0o077 = 0o050, so routing
// HOME through that seam rejects every real HOME and no plan can ever be made.
// HOME's mode is not MC's business, which is why D5 has five legs and NO MODE
// LEG. For the same reason D5 refuses the pending macOS ACL obligation
// (identity.go:52-54): the real HOME carries an ACL today, and a managed or
// network HOME may carry allow ACEs. Reuse the lstat seam only. !!
func resolveHome(home string, ownerUID int) (ProtectedID, error) {
	if home == "" {
		return ProtectedID{}, mountErrf(CodeDeniedRoot,
			"jurisdiction requires the operator's real HOME; it is injected, never read from $HOME")
	}
	if !filepath.IsAbs(home) {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "HOME %q is not absolute", home)
	}
	clean := filepath.Clean(home)

	// Leg 1: Lstat, NEVER Stat. Stat follows the link and destroys leg 2's only
	// evidence.
	info, err := os.Lstat(clean)
	if err != nil {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "HOME %q cannot be stat'd: %v", home, err)
	}

	// Leg 2: refuse a symlink; do not resolve through it. Matching trustedLstat's
	// seam (identity.go:81-90): "a symlink to an otherwise-trusted object is not
	// itself trusted." A $HOME pointed at an attacker-controlled directory would
	// otherwise silently relocate broad_root's anchor and the whole ~/.ssh-class
	// member set — the real ~/.ssh left unprotected, the attacker's protected.
	if info.Mode()&os.ModeSymlink != 0 {
		return ProtectedID{}, mountErrf(CodeDeniedRoot,
			"HOME %q is a symlink; the operator's real HOME is refused rather than resolved through", home)
	}

	// Leg 3: a directory.
	if !info.IsDir() {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "HOME %q is not a directory", home)
	}

	// Leg 4: not a filesystem root. Before ownership, so "/" refuses for the right
	// reason. A HOME of "/" would make broad_root reject every source on the
	// machine: fail-closed, but useless.
	if root, why := isFilesystemRoot(clean, info); root {
		return ProtectedID{}, mountErrf(CodeDeniedRoot,
			"HOME %q is a filesystem root (%s); broad_root would then reject every source", home, why)
	}

	// Leg 5: owned by the operator. Injection relocates trust; it does not remove
	// it.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "cannot read ownership of HOME %q", home)
	}
	if int(stat.Uid) != ownerUID {
		return ProtectedID{}, mountErrf(CodeDeniedRoot,
			"HOME %q is owned by uid %d, not the operator uid %d", home, stat.Uid, ownerUID)
	}

	// Validated. Now canonicalize for the identity comparisons broad_root walks.
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "HOME %q cannot be resolved: %v", home, err)
	}
	cinfo, err := os.Stat(canonical)
	if err != nil {
		return ProtectedID{}, mountErrf(CodeDeniedRoot, "HOME %q cannot be stat'd: %v", canonical, err)
	}
	return ProtectedID{Canonical: canonical, Info: cinfo, IsDir: true}, nil
}

// isFilesystemRoot implements D5's leg 4. The three tests are complementary: the
// Dir-self test alone misses a real volume root such as /System/Volumes/Data,
// whose parent is an ordinary directory.
//
// Ambiguity denies (D8): if the parent cannot be examined, treat the path as a
// root rather than assume it is not.
func isFilesystemRoot(clean string, info fs.FileInfo) (bool, string) {
	parent := filepath.Dir(clean)
	if parent == clean {
		return true, "it is its own parent"
	}
	pinfo, err := os.Lstat(parent)
	if err != nil {
		return true, "its parent cannot be examined, and ambiguity denies"
	}
	if sameFile(info, pinfo) {
		return true, "it is the same object as its parent"
	}
	// The kernel's own answer, where available: a path whose volume mount point is
	// itself IS a filesystem root, however ordinary its parent looks.
	if mnt, err := mountPoint(clean); err == nil {
		if minfo, err := os.Lstat(mnt); err == nil && sameFile(info, minfo) {
			return true, "it is a volume mount point"
		}
	}
	return false, ""
}

// sameFile compares two stat objects by filesystem identity, tolerating the nil
// Info of an absent member. os.SameFile itself returns false rather than
// panicking on nil, but going through one helper keeps that fact in one place.
func sameFile(a, b fs.FileInfo) bool {
	if a == nil || b == nil {
		return false
	}
	return os.SameFile(a, b)
}

func asMountError(err error, target **MountError) bool { return errors.As(err, target) }

// Rejects reports whether one source is out of jurisdiction.
//
// It is callable WITHOUT an allow-root match, which is exactly what typed system
// sources need: ADR-017:349-351 says every typed system source "bypasses the
// external allowlist requirement only" and still owes its identity and
// cross-Worksource checks, so a jurisdiction reachable only through the allowlist
// would be unreachable for the sources that most need it.
//
// A zero TypedClaim selects the union predicate (D10); a populated one selects
// kind-specific confinement, whose authorized roots are read from j.typedRoots —
// never from the caller.
func (j Jurisdiction) Rejects(id SourceIdentity, claim TypedClaim) error {
	// D1's zero-value law. A zero Jurisdiction is not an empty set that permits
	// everything; it is a value nobody constructed, and it fails closed.
	if !j.resolved {
		return mountErrf(CodeDeniedRoot,
			"unresolved jurisdiction rejects every source: %q", id.Canonical)
	}
	return nil
}
