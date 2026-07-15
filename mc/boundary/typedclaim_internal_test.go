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
		if k == KindNotABind {
			continue
		}
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
	j, err := ResolveJurisdiction(JurisdictionInput{
		Home:       tjHome(t, dir),
		TypedRoots: map[TypedKind][]ProtectedID{KindNotABind: {tjID(t, root)}},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	// Even registered, even against its own "root", even for the exact path.
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
