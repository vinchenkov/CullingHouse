package boundary_test

import (
	"os"
	"path/filepath"
	"testing"

	"mc/boundary"
)

// ADR-021 D4 (bidirectionality), D7 (the code split and its precedence), and D6
// (where step 5 sits in the walk).

// unionFixture builds a jurisdiction with one protected member and returns a
// prober. HOME is a sibling of everything else so broad_root never fires and the
// assertions can only be about the member under test.
func unionFixture(t *testing.T, in boundary.JurisdictionInput, dir string) (boundary.Jurisdiction, func(string) error) {
	t.Helper()
	in.Home = mkdir(t, filepath.Join(dir, "home"), 0o750)
	j, err := boundary.ResolveJurisdiction(in, os.Getuid())
	if err != nil {
		t.Fatalf("ResolveJurisdiction() = %v", err)
	}
	return j, func(path string) error {
		id, err := boundary.ResolveSource(path)
		if err != nil {
			t.Fatalf("ResolveSource(%q) = %v", path, err)
		}
		return j.Rejects(id, boundary.TypedClaim{})
	}
}

// ADR-017:388-389: "A source intersecting a protected root in EITHER DIRECTION
// rejects, so mounting a parent of another Worksource cannot expose a denied
// descendant."
func TestUnionIsBidirectional(t *testing.T) {
	dir := t.TempDir()
	parent := mkdir(t, filepath.Join(dir, "tree"), 0o755)
	member := mkdir(t, filepath.Join(parent, "protected"), 0o755)
	child := mkdir(t, filepath.Join(member, "inner"), 0o755)
	unrelated := mkdir(t, filepath.Join(dir, "unrelated"), 0o755)

	_, rejects := unionFixture(t, boundary.JurisdictionInput{
		MissionControlRoots: []boundary.ProtectedID{protectedID(t, member)},
	}, dir)

	t.Run("permit: an unrelated source", func(t *testing.T) {
		if err := rejects(unrelated); err != nil {
			t.Fatalf("an unrelated source rejected: %v — the union must not deny everything", err)
		}
	})
	t.Run("equal", func(t *testing.T) {
		if err := rejects(member); err == nil {
			t.Fatal("the protected root itself was permitted")
		}
	})
	t.Run("descendant", func(t *testing.T) {
		if err := rejects(child); err == nil {
			t.Fatal("a descendant of a protected root was permitted")
		}
	})
	// The direction that is load-bearing and easy to omit: without it, mounting a
	// parent hands over every protected descendant inside it.
	t.Run("ancestor", func(t *testing.T) {
		if err := rejects(parent); err == nil {
			t.Fatal("an ANCESTOR of a protected root was permitted; mounting it would expose " +
				"the protected descendant (ADR-017:388-389)")
		}
	})
}

// Identity, never strings (ADR-017:247-250; the rejected alternative at :1348-1349
// names exactly why: "Lexical prefix comparison fails under symlinks, APFS case
// behavior, and sibling prefixes").
func TestUnionMatchesByIdentityNotByString(t *testing.T) {
	dir := t.TempDir()
	member := mkdir(t, filepath.Join(dir, "protected"), 0o755)
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(member, alias); err != nil {
		t.Fatal(err)
	}
	// A sibling whose name has the protected root as a byte-prefix. A
	// strings.HasPrefix implementation rejects this; identity does not.
	sibling := mkdir(t, filepath.Join(dir, "protected-evil"), 0o755)

	_, rejects := unionFixture(t, boundary.JurisdictionInput{
		MissionControlRoots: []boundary.ProtectedID{protectedID(t, member)},
	}, dir)

	if err := rejects(alias); err == nil {
		t.Error("a symlink alias of a protected root was permitted; matching must be by identity")
	}
	if err := rejects(sibling); err != nil {
		t.Errorf("a SIBLING sharing a name prefix was rejected: %v — that is lexical prefix "+
			"matching, which ADR-017:1348-1349 rejects on the merits", err)
	}
}

// ADR-021 D4's worked example, and the reason the ancestor direction is not
// redundant with the blocked floor: ~/Library is an ancestor of the protected
// ~/Library/Keychains. The floor does not match it ("library" is no pattern),
// and broad_root does not apply (~/Library is not an ancestor of HOME). Only
// bidirectionality rejects it.
func TestAncestorOfAProtectedRootTheFloorCannotSee(t *testing.T) {
	dir := t.TempDir()
	home := mkdir(t, filepath.Join(dir, "home"), 0o750)
	library := mkdir(t, filepath.Join(home, "Library"), 0o755)
	keychains := mkdir(t, filepath.Join(library, "Keychains"), 0o755)

	// The floor is genuinely blind to ~/Library — assert it, so this test cannot
	// pass for the wrong reason.
	var floor boundary.BlockPolicy
	if floor.Rejects(library, library) {
		t.Fatalf("fixture inert: the blocked floor already rejects %q, so this proves nothing "+
			"about the ancestor direction", library)
	}
	if !floor.Rejects(keychains, keychains) {
		t.Logf("note: the floor does not reject %q either", keychains)
	}

	j, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
		Home:           home,
		HomeClassRoots: []boundary.ProtectedID{protectedID(t, keychains)},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	id, err := boundary.ResolveSource(library)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Rejects(id, boundary.TypedClaim{}); err == nil {
		t.Fatal("~/Library permitted. It is an ancestor of the protected ~/Library/Keychains; " +
			"the floor cannot see it and broad_root does not apply, so the ancestor direction " +
			"is the only thing that rejects it (ADR-021 D4)")
	}
}

// ADR-021 D7: the split ADR-017 never states, and its precedence.
func TestJurisdictionCodes(t *testing.T) {
	dir := t.TempDir()
	otherWS := mkdir(t, filepath.Join(dir, "other-ws"), 0o755)
	mcHome := mkdir(t, filepath.Join(dir, "mc-home"), 0o700)

	t.Run("another Worksource's root reports cross_worksource", func(t *testing.T) {
		_, rejects := unionFixture(t, boundary.JurisdictionInput{
			OtherWorksources: []boundary.WorksourceRoots{{Workspace: protectedID(t, otherWS)}},
		}, dir)
		err := rejects(otherWS)
		if err == nil {
			t.Fatal("permitted")
		}
		if got := codeOf(t, err); got != boundary.CodeCrossWorksource {
			t.Errorf("code = %q, want %q", got, boundary.CodeCrossWorksource)
		}
	})

	t.Run("every other member reports denied_root", func(t *testing.T) {
		_, rejects := unionFixture(t, boundary.JurisdictionInput{
			MCHome: protectedID(t, mcHome),
		}, dir)
		err := rejects(mcHome)
		if err == nil {
			t.Fatal("permitted")
		}
		if got := codeOf(t, err); got != boundary.CodeDeniedRoot {
			t.Errorf("code = %q, want %q", got, boundary.CodeDeniedRoot)
		}
	})

	// ADR-017 gives no precedence rule. D7 decides cross_worksource wins: it is
	// the more specific statement of WHY, and the one an operator can act on —
	// they can re-scope a Worksource, whereas "denied_root" tells them only that
	// something, somewhere, said no. Determinism matters more than the choice.
	t.Run("a path that is BOTH reports cross_worksource, deterministically", func(t *testing.T) {
		_, rejects := unionFixture(t, boundary.JurisdictionInput{
			OtherWorksources: []boundary.WorksourceRoots{{Workspace: protectedID(t, otherWS)}},
			DeniedPaths:      []string{otherWS},
		}, dir)
		for i := 0; i < 8; i++ {
			err := rejects(otherWS)
			if err == nil {
				t.Fatal("permitted")
			}
			if got := codeOf(t, err); got != boundary.CodeCrossWorksource {
				t.Fatalf("run %d: code = %q, want %q — the same plan must never report two "+
					"different codes on two runs", i, got, boundary.CodeCrossWorksource)
			}
		}
	})

	// ADR-017:302-303 requires the OWN Worksource's roots to pass Authorize as
	// ordinary sources.
	t.Run("the own Worksource's roots do not trip it", func(t *testing.T) {
		ownWS := mkdir(t, filepath.Join(dir, "own-ws"), 0o755)
		_, rejects := unionFixture(t, boundary.JurisdictionInput{
			OwnWorksource:    boundary.WorksourceRoots{Workspace: protectedID(t, ownWS)},
			OtherWorksources: []boundary.WorksourceRoots{{Workspace: protectedID(t, otherWS)}},
		}, dir)
		if err := rejects(ownWS); err != nil {
			t.Fatalf("the own Worksource's workspace root was rejected: %v", err)
		}
	})
}

// ADR-021 D6: jurisdiction sits AFTER the allow-root match (Decision 3's own
// numbering) and BEFORE ResolveAccess.
func TestAuthorizeOrdering(t *testing.T) {
	dir := t.TempDir()
	root := mkdir(t, filepath.Join(dir, "root"), 0o755)
	protected := mkdir(t, filepath.Join(root, "protected"), 0o755)

	allowlist, err := boundary.ParseMountAllowlist([]byte(`version = 1
[[allow]]
path = "` + root + `"
target = "r"
access = "rw"
`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := boundary.ResolveAllowlist(allowlist)
	if err != nil {
		t.Fatal(err)
	}
	var blocked boundary.BlockPolicy
	j, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
		Home:                mkdir(t, filepath.Join(dir, "home"), 0o750),
		MissionControlRoots: []boundary.ProtectedID{protectedID(t, protected)},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}

	// Jurisdiction beats ResolveAccess. Jurisdiction is mode-independent — a
	// protected path is protected for RO as much as for RW — so resolving access
	// first would let rw_not_permitted mask denied_root and tell the operator to
	// DOWNGRADE a mount that must never exist at any access.
	//
	// The fixture has to make the two orderings DISAGREE, or it proves nothing: an
	// RW request under an RW-max root succeeds at ResolveAccess and reports
	// denied_root under either ordering. So this uses an RO-max root and an RW
	// request — jurisdiction-first says denied_root, access-first says
	// rw_not_permitted. The first version of this test used the RW-max root above,
	// and the mutation that moves jurisdiction after ResolveAccess sailed through
	// it.
	t.Run("jurisdiction beats ResolveAccess", func(t *testing.T) {
		roRoot := mkdir(t, filepath.Join(dir, "ro-root"), 0o755)
		roProtected := mkdir(t, filepath.Join(roRoot, "protected"), 0o755)
		roAllowlist, err := boundary.ParseMountAllowlist([]byte(`version = 1
[[allow]]
path = "` + roRoot + `"
target = "ro"
access = "ro"
`))
		if err != nil {
			t.Fatal(err)
		}
		roResolved, err := boundary.ResolveAllowlist(roAllowlist)
		if err != nil {
			t.Fatal(err)
		}
		roJ, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
			Home:                mkdir(t, filepath.Join(dir, "home3"), 0o750),
			MissionControlRoots: []boundary.ProtectedID{protectedID(t, roProtected)},
		}, os.Getuid())
		if err != nil {
			t.Fatal(err)
		}

		// Non-inertness: an RW request on a NON-protected path under this root must
		// really report rw_not_permitted, or the assertion below could pass for the
		// wrong reason.
		ordinary := mkdir(t, filepath.Join(roRoot, "ordinary"), 0o755)
		if _, err := roResolved.Authorize(ordinary, boundary.AccessRW, blocked, roJ); codeOf(t, err) != boundary.CodeRWNotPermitted {
			t.Fatalf("fixture inert: an RW request under an RO-max root did not report %q",
				boundary.CodeRWNotPermitted)
		}

		_, err = roResolved.Authorize(roProtected, boundary.AccessRW, blocked, roJ)
		if got := codeOf(t, err); got != boundary.CodeDeniedRoot {
			t.Errorf("code = %q, want %q — rw_not_permitted masking denied_root tells the operator "+
				"to downgrade a mount that must never exist at any access", got, boundary.CodeDeniedRoot)
		}
	})

	// ADR-021 D6's stated consequence: because step 4 precedes step 5, a protected
	// path under NO allow root exits at not_allowlisted and never reaches
	// jurisdiction. So denied_root is observable only when the path IS
	// allowlisted — which is exactly the case ADR-017:393-394 exists to cover
	// ("Allowlist membership never overrides jurisdiction"). Stated as a test so
	// nobody "fixes" it.
	t.Run("a non-allowlisted protected path exits at not_allowlisted", func(t *testing.T) {
		outside := mkdir(t, filepath.Join(dir, "outside"), 0o755)
		jOutside, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
			Home:                mkdir(t, filepath.Join(dir, "home2"), 0o750),
			MissionControlRoots: []boundary.ProtectedID{protectedID(t, outside)},
		}, os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolved.Authorize(outside, boundary.AccessRO, blocked, jOutside)
		if got := codeOf(t, err); got != boundary.CodeNotAllowlisted {
			t.Errorf("code = %q, want %q", got, boundary.CodeNotAllowlisted)
		}
	})

	// ADR-017:393-394 itself: an allow entry naming a protected path is not an
	// error at ResolveAllowlist time and does not authorize it at Authorize time.
	// It simply loses.
	t.Run("allowlist membership never overrides jurisdiction", func(t *testing.T) {
		_, err := resolved.Authorize(protected, boundary.AccessRO, blocked, j)
		if err == nil {
			t.Fatal("an allowlisted protected path was authorized")
		}
	})

	// And the permit side, so the ordering tests are not passing against an
	// Authorize that now rejects everything.
	t.Run("permit: an allowlisted ordinary path still authorizes", func(t *testing.T) {
		ordinary := mkdir(t, filepath.Join(root, "ordinary"), 0o755)
		if _, err := resolved.Authorize(ordinary, boundary.AccessRW, blocked, j); err != nil {
			t.Fatalf("an ordinary allowlisted path was rejected: %v", err)
		}
	})
}
