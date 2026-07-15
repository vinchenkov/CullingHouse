package boundary

import (
	"os"
	"path/filepath"
	"testing"
)

// ADR-021 D10 — typed grants are confined per class: a SECOND predicate, not a
// hole in the union. ADR-017:396-401's load-bearing word is "instead": a typed
// source is not asked "do you intersect MC_HOME?" — it always does, by design —
// but "are you exactly the root your kind may occupy?", which is stricter.
//
// PERMIT SIDE FIRST, and here that is not a style preference. This ADR's
// predecessor draft pinned only the deny side; a skeptic implemented it verbatim
// and all seven typed grants rejected — no container could ever have launched —
// while the draft's own test list stayed green. The permit sweep below is the
// test that could not have passed.

func tjDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func tjID(t *testing.T, path string) ProtectedID {
	t.Helper()
	id, err := ResolveSource(path)
	if err != nil {
		t.Fatalf("ResolveSource(%q) = %v", path, err)
	}
	return ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}
}

func tjHome(t *testing.T, dir string) string {
	t.Helper()
	return tjDir(t, filepath.Join(dir, "home"))
}

func tjSource(t *testing.T, path string) SourceIdentity {
	t.Helper()
	id, err := ResolveSource(path)
	if err != nil {
		t.Fatalf("ResolveSource(%q) = %v", path, err)
	}
	return id
}

// every real kind: the enum minus the zero value and minus the explicit
// not-a-bind marker.
func realKinds() []TypedKind {
	var out []TypedKind
	for k := KindNone + 1; k < kindMax; k++ {
		out = append(out, k)
	}
	return out
}

// THE permit sweep. Every kind — and therefore, via the D10a derivation guard
// that binds kinds to rows, every host-bind row of ADR-017:634-702 — must plan
// against its own authorized root.
//
// "A suite that cannot launch a container is not green, whatever it reports."
func TestEveryTypedKindPlansAgainstItsAuthorizedRoot(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)

	for _, k := range realKinds() {
		t.Run(k.String(), func(t *testing.T) {
			root := tjDir(t, filepath.Join(dir, "roots", k.String()))
			j, err := ResolveJurisdiction(JurisdictionInput{
				Home: home,
				// The whole MC_HOME tree is protected, and the typed roots live
				// inside it — which is the entire point of D10's second predicate.
				MCHome:     tjID(t, tjDir(t, filepath.Join(dir, "roots"))),
				TypedRoots: map[TypedKind][]ProtectedID{k: {tjID(t, root)}},
			}, os.Getuid())
			if err != nil {
				t.Fatalf("ResolveJurisdiction() = %v", err)
			}
			if err := j.Rejects(tjSource(t, root), TypedClaim{Kind: k}); err != nil {
				t.Fatalf("kind %v CANNOT PLAN against its own authorized root: %v\n"+
					"ADR-017 mandates this grant. A boundary that rejects it cannot launch a "+
					"container, and pinning only the deny side is how that shipped once already.", k, err)
			}
		})
	}
}

// THE test that dies on a self-certifying claim.
//
// An earlier draft had TypedClaim carry its own Root, so Rejects could only
// compute os.SameFile(source, claim.Root) — the caller supplying both sides. A
// skeptic measured the consequence: with one fixed source/root pair, ALL TEN
// kinds permitted. claim.Kind was compared to nothing. D10's own sentence, "the
// claim is not trusted, it is checked", was false.
//
// The binding now lives in the Jurisdiction, so a kind the source does not own
// must deny.
func TestClaimKindIsNotInert(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	sessionRoot := tjDir(t, filepath.Join(dir, "mc-home", "sessions", "run-0123456789abcdef"))

	j, err := ResolveJurisdiction(JurisdictionInput{
		Home:       home,
		MCHome:     tjID(t, tjDir(t, filepath.Join(dir, "mc-home"))),
		TypedRoots: map[TypedKind][]ProtectedID{KindOwnSession: {tjID(t, sessionRoot)}},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	src := tjSource(t, sessionRoot)

	// The pair is genuinely correct for its own kind, or the sweep below would
	// pass against a Rejects that denies everything.
	if err := j.Rejects(src, TypedClaim{Kind: KindOwnSession}); err != nil {
		t.Fatalf("the authorized own-session root cannot plan: %v", err)
	}

	for _, k := range realKinds() {
		if k == KindOwnSession {
			continue
		}
		if err := j.Rejects(src, TypedClaim{Kind: k}); err == nil {
			t.Errorf("kind %v PERMITTED a source authorized only for own-session: claim.Kind is "+
				"inert, and the claim is certifying itself", k)
		}
	}
}

// The verdict must be a function of the JURISDICTION, not of the claim. Under a
// caller-supplied root the Jurisdiction contributed nothing but its resolved
// flag, which also made D9/D11's drift rules vacuous on the typed path.
func TestTypedVerdictDependsOnTheJurisdiction(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	root := tjDir(t, filepath.Join(dir, "mc-home", "sessions", "run-0123456789abcdef"))
	other := tjDir(t, filepath.Join(dir, "mc-home", "sessions", "run-ffffffffffffffff"))
	mcHome := tjID(t, tjDir(t, filepath.Join(dir, "mc-home")))
	src := tjSource(t, root)
	claim := TypedClaim{Kind: KindOwnSession}

	authorized, err := ResolveJurisdiction(JurisdictionInput{
		Home: home, MCHome: mcHome,
		TypedRoots: map[TypedKind][]ProtectedID{KindOwnSession: {tjID(t, root)}},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if err := authorized.Rejects(src, claim); err != nil {
		t.Fatalf("authorized: %v", err)
	}

	// Same source, same claim, different jurisdiction: the verdict must flip.
	elsewhere, err := ResolveJurisdiction(JurisdictionInput{
		Home: home, MCHome: mcHome,
		TypedRoots: map[TypedKind][]ProtectedID{KindOwnSession: {tjID(t, other)}},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if err := elsewhere.Rejects(src, claim); err == nil {
		t.Fatal("the same source and claim PERMITTED under a jurisdiction that authorizes a " +
			"different root: the verdict does not depend on the jurisdiction, so D9/D11's drift " +
			"rules are vacuous on the typed path")
	}
}

// D11 includes the host-resolved kind registry in the drift surface. The
// selector path is stable while its target identity changes; an old
// Jurisdiction must remain an immutable snapshot, and a freshly host-resolved
// TypedRoots input must flip the typed verdict.
func TestTypedRootsReconstructionTracksSelectorRetarget(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	a := tjDir(t, filepath.Join(dir, "sessions", "a"))
	b := tjDir(t, filepath.Join(dir, "sessions", "b"))
	selector := filepath.Join(dir, "selected-session")
	if err := os.Symlink(a, selector); err != nil {
		t.Fatal(err)
	}
	aSource := tjSource(t, a)
	bSource := tjSource(t, b)
	if sameFile(aSource.Info, bSource.Info) {
		t.Fatal("fixture is inert: target A and target B are the same identity")
	}
	claim := TypedClaim{Kind: KindOwnSession}

	resolveSelector := func(t *testing.T) (SourceIdentity, ProtectedID) {
		t.Helper()
		selected := tjSource(t, selector)
		return selected, ProtectedID{
			Canonical: selected.Canonical,
			Info:      selected.Info,
			IsDir:     selected.IsDir,
		}
	}
	build := func(t *testing.T, selected ProtectedID) Jurisdiction {
		t.Helper()
		j, err := ResolveJurisdiction(JurisdictionInput{
			Home:       home,
			TypedRoots: map[TypedKind][]ProtectedID{KindOwnSession: {selected}},
		}, os.Getuid())
		if err != nil {
			t.Fatalf("ResolveJurisdiction() = %v", err)
		}
		return j
	}
	assertDenied := func(t *testing.T, j Jurisdiction, source SourceIdentity) {
		t.Helper()
		err := j.Rejects(source, claim)
		var me *MountError
		if !asMountError(err, &me) || me.Code != CodeDeniedRoot {
			t.Fatalf("source %q error = %v, want %s", source.Canonical, err, CodeDeniedRoot)
		}
	}
	assertPermitted := func(t *testing.T, j Jurisdiction, source SourceIdentity) {
		t.Helper()
		if err := j.Rejects(source, claim); err != nil {
			t.Fatalf("source %q rejected: %v", source.Canonical, err)
		}
	}

	selectorBefore, firstRoot := resolveSelector(t)
	if !sameFile(firstRoot.Info, aSource.Info) || sameFile(firstRoot.Info, bSource.Info) {
		t.Fatal("fixture selector does not initially resolve only to target A")
	}
	first := build(t, firstRoot)
	assertPermitted(t, first, aSource)
	assertDenied(t, first, bSource)

	nextSelector := selector + ".next"
	if err := os.Symlink(b, nextSelector); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(nextSelector, selector); err != nil {
		t.Fatal(err)
	}
	selectorAfter, secondRoot := resolveSelector(t)
	if selectorAfter.RawClean != selectorBefore.RawClean ||
		!sameFile(secondRoot.Info, bSource.Info) || sameFile(secondRoot.Info, aSource.Info) {
		t.Fatal("selector address did not retain its spelling while changing from identity A to B")
	}
	aAfter := tjSource(t, a)
	bAfter := tjSource(t, b)
	if !sameFile(aAfter.Info, aSource.Info) || !sameFile(bAfter.Info, bSource.Info) {
		t.Fatal("direct source identities changed; fixture does not isolate TypedRoots drift")
	}

	// The caller cannot retroactively replace authority in an old snapshot.
	assertPermitted(t, first, aSource)
	assertDenied(t, first, bSource)

	// The host rebuilt the pre-resolved registry from the same selector path.
	// Passing firstRoot again would intentionally reproduce the stale answer.
	second := build(t, secondRoot)
	assertDenied(t, second, aSource)
	assertPermitted(t, second, bSource)
}

// D2's immutability is about the constructed VALUE, not merely whether its
// fields are exported. Retaining the caller's map or one of its slices lets a
// later planner mutation replace authority without another
// ResolveJurisdiction call — exactly the stale-input seam D11 forbids.
func TestTypedRootsInputCannotMutateConstructedJurisdiction(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	a := tjDir(t, filepath.Join(dir, "sessions", "a"))
	b := tjDir(t, filepath.Join(dir, "sessions", "b"))
	aID, bID := tjID(t, a), tjID(t, b)
	aSource, bSource := tjSource(t, a), tjSource(t, b)
	claim := TypedClaim{Kind: KindOwnSession}

	assertSnapshot := func(t *testing.T, mutate func(map[TypedKind][]ProtectedID)) {
		t.Helper()
		roots := map[TypedKind][]ProtectedID{KindOwnSession: {aID}}
		j, err := ResolveJurisdiction(JurisdictionInput{Home: home, TypedRoots: roots}, os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		if err := j.Rejects(aSource, claim); err != nil {
			t.Fatalf("fixture: original root does not authorize: %v", err)
		}
		if err := j.Rejects(bSource, claim); err == nil {
			t.Fatal("fixture: replacement root authorized before the caller mutation")
		}

		mutate(roots)

		if err := j.Rejects(aSource, claim); err != nil {
			t.Errorf("caller mutation removed authority from an already-constructed Jurisdiction: %v", err)
		}
		if err := j.Rejects(bSource, claim); err == nil {
			t.Error("caller mutation ADDED authority to an already-constructed Jurisdiction; " +
				"ResolveJurisdiction retained the input map/slice by alias")
		}
	}

	t.Run("map entry replacement", func(t *testing.T) {
		assertSnapshot(t, func(roots map[TypedKind][]ProtectedID) {
			roots[KindOwnSession] = []ProtectedID{bID}
		})
	})
	t.Run("retained slice element replacement", func(t *testing.T) {
		assertSnapshot(t, func(roots map[TypedKind][]ProtectedID) {
			roots[KindOwnSession][0] = bID
		})
	})
}

// D10a says the TypedKind domain is closed. Go's exported uint8 enum does not
// enforce that itself: every numeric value is constructible, so both the
// constructor and Rejects must reject values outside the declared domain before
// a caller-supplied map entry can turn one into authority.
func TestOutOfDomainTypedKindsDeny(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	root := tjDir(t, filepath.Join(dir, "root"))
	rootID := tjID(t, root)
	source := tjSource(t, root)

	invalid := []TypedKind{KindNone, KindNotABind, kindMax, TypedKind(254)}
	for _, kind := range invalid {
		t.Run(kind.String(), func(t *testing.T) {
			_, err := ResolveJurisdiction(JurisdictionInput{
				Home:       home,
				TypedRoots: map[TypedKind][]ProtectedID{kind: {rootID}},
			}, os.Getuid())
			if err == nil {
				t.Fatalf("ResolveJurisdiction accepted unauthorized typed-registry key %v", kind)
			}

			// Pin the use-side check independently of constructor validation. A
			// future alternate constructor or deserializer must not make the map
			// lookup itself fail open. KindNone is the deliberate exception here:
			// as a CLAIM it means the ordinary union predicate, even though it is
			// never legal as a TypedRoots registry key.
			if kind != KindNone {
				j := Jurisdiction{
					resolved:   true,
					typedRoots: map[TypedKind][]ProtectedID{kind: {rootID}},
				}
				if err := j.Rejects(source, TypedClaim{Kind: kind}); err == nil {
					t.Fatalf("Rejects authorized out-of-domain typed claim %v", kind)
				}
			}
		})
	}
}

// ADR-017:399's own case, and the one a planner is most likely to get wrong:
// /home/agent is MC_HOME/state/worksources/<scope-id>/home, so the own scope's
// home must plan and a SIBLING scope's home must not — both derived from one
// expression differing only in the scope id.
func TestSiblingScopeHomeDenies(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	base := filepath.Join(dir, "mc-home", "state", "worksources")
	own := tjDir(t, filepath.Join(base, "ws-own", "home"))
	sibling := tjDir(t, filepath.Join(base, "ws-sibling", "home"))

	j, err := ResolveJurisdiction(JurisdictionInput{
		Home:       home,
		MCHome:     tjID(t, tjDir(t, filepath.Join(dir, "mc-home"))),
		TypedRoots: map[TypedKind][]ProtectedID{KindOwnState: {tjID(t, own)}},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}

	if err := j.Rejects(tjSource(t, own), TypedClaim{Kind: KindOwnState}); err != nil {
		t.Fatalf("the own scope's synthetic home cannot plan: %v", err)
	}
	if err := j.Rejects(tjSource(t, sibling), TypedClaim{Kind: KindOwnState}); err == nil {
		t.Fatal("a SIBLING scope's synthetic home planned under the own scope's claim " +
			"(ADR-017:399: \"any sibling/ancestor/other identity is still denied\")")
	}
}

// ADR-017:399 in full: "any sibling/ancestor/other identity is still denied".
// Confinement is to ONE IDENTITY — os.SameFile against the exact authorized root
// — not to a subtree. Relaxing it to "inside the authorized root" is the natural
// mistake, since the surrounding union code is all containment walks, and the
// sibling test alone does not catch it.
//
// The DESCENDANT direction is the one that matters most in practice: the
// authorized root is a directory, and "inside it" looks obviously fine right up
// until a Worker mounts MC_HOME/sessions/<own>/../<other> — or, here, some inner
// path nobody granted.
func TestTypedConfinementIsIdentityNotContainment(t *testing.T) {
	dir := t.TempDir()
	home := tjHome(t, dir)
	sessions := tjDir(t, filepath.Join(dir, "mc-home", "sessions"))
	root := tjDir(t, filepath.Join(sessions, "run-0123456789abcdef"))

	j, err := ResolveJurisdiction(JurisdictionInput{
		Home:       home,
		MCHome:     tjID(t, tjDir(t, filepath.Join(dir, "mc-home"))),
		TypedRoots: map[TypedKind][]ProtectedID{KindOwnSession: {tjID(t, root)}},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}

	// Permit side of this very pair, so the denials below mean something.
	if err := j.Rejects(tjSource(t, root), TypedClaim{Kind: KindOwnSession}); err != nil {
		t.Fatalf("the exact authorized root cannot plan: %v", err)
	}

	cases := map[string]string{
		"descendant": tjDir(t, filepath.Join(root, "inner")),
		"ancestor":   sessions,
		"sibling":    tjDir(t, filepath.Join(sessions, "run-ffffffffffffffff")),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			if err := j.Rejects(tjSource(t, path), TypedClaim{Kind: KindOwnSession}); err == nil {
				t.Fatalf("a %s of the authorized root PLANNED (%q). D10 confines a typed source "+
					"to ONE identity; \"inside the authorized root\" is not the rule "+
					"(ADR-017:399)", name, path)
			}
		})
	}
}

// Fail closed: a kind the Jurisdiction authorizes nothing for has no authorized
// root, so nothing can be that root.
func TestKindWithNoAuthorizedRootDenies(t *testing.T) {
	dir := t.TempDir()
	j, err := ResolveJurisdiction(JurisdictionInput{Home: tjHome(t, dir)}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	src := tjSource(t, tjDir(t, filepath.Join(dir, "anything")))
	for _, k := range realKinds() {
		if err := j.Rejects(src, TypedClaim{Kind: k}); err == nil {
			t.Errorf("kind %v permitted a source with no authorized root registered", k)
		}
	}
}

// ADR-021 D10a: image-rootfs rows (:665 "never a bind", :667, :636's rootfs arm)
// and the named volume (:679) have no host inode. A jurisdiction question about
// them is meaningless and any answer would be a lie — so reaching Rejects with
// one means the planner is confused. Deny, loudly.
func TestNotABindAlwaysDenies(t *testing.T) {
	dir := t.TempDir()
	root := tjDir(t, filepath.Join(dir, "whatever"))
	if _, err := ResolveJurisdiction(JurisdictionInput{
		Home:       tjHome(t, dir),
		TypedRoots: map[TypedKind][]ProtectedID{KindNotABind: {tjID(t, root)}},
	}, os.Getuid()); err == nil {
		t.Fatal("KindNotABind was accepted as an authorized registry key")
	}

	j, err := ResolveJurisdiction(JurisdictionInput{Home: tjHome(t, dir)}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	// Even for an exact real path, the deny sentinel never reaches a root lookup.
	if err := j.Rejects(tjSource(t, root), TypedClaim{Kind: KindNotABind}); err == nil {
		t.Fatal("KindNotABind planned; a destination that is not a host bind must never reach " +
			"jurisdiction with an answer other than deny")
	}
}

// The zero claim is not a typed claim: it selects the union predicate, which is
// what Authorize always passes. If the zero value fell into the typed arm, every
// ordinary mount would need a TypedRoots entry and nothing would plan.
func TestZeroClaimTakesTheUnionPredicate(t *testing.T) {
	dir := t.TempDir()
	j, err := ResolveJurisdiction(JurisdictionInput{Home: tjHome(t, dir)}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	ordinary := tjSource(t, tjDir(t, filepath.Join(dir, "project")))
	if err := j.Rejects(ordinary, TypedClaim{}); err != nil {
		t.Fatalf("an ordinary source with a zero claim was rejected: %v — the zero claim must "+
			"select the union predicate, not kind-specific confinement", err)
	}
}
