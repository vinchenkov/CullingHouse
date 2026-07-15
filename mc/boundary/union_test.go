package boundary_test

import (
	"os"
	"path/filepath"
	"strings"
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
		OtherMissionControlRoots: []boundary.ProtectedID{protectedID(t, member)},
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
		OtherMissionControlRoots: []boundary.ProtectedID{protectedID(t, member)},
	}, dir)

	if err := rejects(alias); err == nil {
		t.Error("a symlink alias of a protected root was permitted; matching must be by identity")
	}
	if err := rejects(sibling); err != nil {
		t.Errorf("a SIBLING sharing a name prefix was rejected: %v — that is lexical prefix "+
			"matching, which ADR-017:1348-1349 rejects on the merits", err)
	}
}

// A symlink alias canonicalizes to the protected spelling, so it cannot by
// itself distinguish filesystem identity from a separator-aware canonical
// prefix comparison. A regular-file hardlink can: the spellings are unrelated
// while os.SameFile says they are the same protected object.
func TestUnionMatchesHardlinkIdentityAcrossUnrelatedSpellings(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "protected-secret")
	if err := os.WriteFile(protected, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "unrelated-alias")
	if err := os.Link(protected, alias); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "ordinary")
	if err := os.WriteFile(unrelated, []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}

	protectedID, err := boundary.ResolveSource(protected)
	if err != nil {
		t.Fatal(err)
	}
	aliasID, err := boundary.ResolveSource(alias)
	if err != nil {
		t.Fatal(err)
	}
	if protectedID.Canonical == aliasID.Canonical || !os.SameFile(protectedID.Info, aliasID.Info) {
		t.Fatal("fixture is inert: need different canonical spellings for one inode")
	}

	_, rejects := unionFixture(t, boundary.JurisdictionInput{
		DeniedPaths: []string{protected},
	}, dir)
	if err := rejects(alias); codeOf(t, err) != boundary.CodeDeniedRoot {
		t.Fatalf("hardlink alias code = %q, want %q (error: %v)",
			codeOf(t, err), boundary.CodeDeniedRoot, err)
	}
	if err := rejects(unrelated); err != nil {
		t.Fatalf("unrelated file rejected: %v", err)
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

// D4's load-bearing qualification: the own workspace is necessarily an
// ancestor of its own .mission-control root, and ADR-017:302 requires that
// ordinary workspace mount to pass. Equality and descendants of the control
// root remain protected; only the exact own workspace earns the ancestor
// exemption.
func TestOwnWorkspaceMayContainItsOwnMissionControlRoot(t *testing.T) {
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "own-workspace"), 0o755)
	control := mkdir(t, filepath.Join(workspace, ".mission-control"), 0o755)
	controlChild := mkdir(t, filepath.Join(control, "tasks"), 0o755)

	allowlist, err := boundary.ParseMountAllowlist([]byte(`version = 1
[[allow]]
path = "` + workspace + `"
target = "workspace"
access = "rw"
`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := boundary.ResolveAllowlist(allowlist)
	if err != nil {
		t.Fatal(err)
	}
	j, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
		Home:                   mkdir(t, filepath.Join(dir, "home"), 0o750),
		OwnWorksource:          boundary.WorksourceRoots{Workspace: protectedID(t, workspace)},
		OwnMissionControlRoots: []boundary.ProtectedID{protectedID(t, control)},
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := resolved.Authorize(workspace, boundary.AccessRW, boundary.BlockPolicy{}, j); err != nil {
		t.Fatalf("own workspace rejected because it contains its own .mission-control root: %v", err)
	}

	for _, path := range []string{control, controlChild} {
		id, err := boundary.ResolveSource(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := j.Rejects(id, boundary.TypedClaim{}); err == nil {
			t.Errorf("protected own control path %q permitted; the exemption is ancestor-only", path)
		}
	}
}

// D4's exemption is deliberately narrower than "a source belongs to the own
// Worksource" and narrower than "a control root is below the own workspace".
// The host must associate each control with its owner, and only the exact own
// workspace identity may pass the ancestor arm for an explicitly own control.
func TestOwnControlAncestorExemptionIsExactAndAssociated(t *testing.T) {
	dir := t.TempDir()
	home := mkdir(t, filepath.Join(dir, "home"), 0o750)
	workspace := mkdir(t, filepath.Join(dir, "own-workspace"), 0o755)
	artifact := mkdir(t, filepath.Join(workspace, "artifact"), 0o755)
	ownControl := mkdir(t, filepath.Join(artifact, "own.git"), 0o755)
	nested := mkdir(t, filepath.Join(workspace, "nested"), 0o755)
	absentOwnControl := filepath.Join(nested, "deeper", "control")
	otherControl := mkdir(t, filepath.Join(workspace, "vendor", "other.git"), 0o755)
	workspaceID := protectedID(t, workspace)

	rejects := func(t *testing.T, in boundary.JurisdictionInput, source string) error {
		t.Helper()
		in.Home = home
		j, err := boundary.ResolveJurisdiction(in, os.Getuid())
		if err != nil {
			t.Fatalf("ResolveJurisdiction() = %v", err)
		}
		id, err := boundary.ResolveSource(source)
		if err != nil {
			t.Fatalf("ResolveSource(%q) = %v", source, err)
		}
		return j.Rejects(id, boundary.TypedClaim{})
	}

	base := boundary.JurisdictionInput{
		OwnWorksource: boundary.WorksourceRoots{
			Workspace: workspaceID,
			Artifacts: []boundary.ProtectedID{protectedID(t, artifact)},
		},
		OwnGitControls: []boundary.ProtectedID{protectedID(t, ownControl)},
	}

	t.Run("exact own workspace identity permits", func(t *testing.T) {
		if err := rejects(t, base, workspace); err != nil {
			t.Fatalf("exact own workspace rejected: %v", err)
		}
	})

	t.Run("same-file different-spelling workspace permits", func(t *testing.T) {
		// A symlink alias is inert here because ResolveSource canonicalizes both
		// spellings to the same string. On a case-insensitive volume this pair has
		// different canonical strings but one inode, so a string-based ownership
		// mutation fails the test.
		caseVariant := filepath.Join(filepath.Dir(workspace), strings.ToUpper(filepath.Base(workspace)))
		variantID, err := boundary.ResolveSource(caseVariant)
		if err != nil || !os.SameFile(workspaceID.Info, variantID.Info) {
			t.Skip("case-sensitive volume: no same-file/different-spelling pair available")
		}
		if workspaceID.Canonical == variantID.Canonical {
			t.Fatalf("fixture inert: both spellings canonicalized to %q", workspaceID.Canonical)
		}
		if err := rejects(t, base, caseVariant); err != nil {
			t.Fatalf("same-file different-spelling own workspace rejected: %v", err)
		}
	})

	t.Run("own artifact ancestor remains denied", func(t *testing.T) {
		if err := rejects(t, base, artifact); err == nil {
			t.Fatal("own artifact was granted the workspace-only control ancestry exemption")
		}
	})

	t.Run("absent own control is retained and the exemption stays exact", func(t *testing.T) {
		in := boundary.JurisdictionInput{
			OwnWorksource:  boundary.WorksourceRoots{Workspace: workspaceID},
			OwnGitControls: []boundary.ProtectedID{absentID(absentOwnControl)},
		}
		if err := rejects(t, in, workspace); err != nil {
			t.Fatalf("exact own workspace rejected for an absent own control: %v", err)
		}
		if err := rejects(t, in, nested); err == nil {
			t.Fatal("absent own control was skipped or its intermediate ancestor was exempted")
		}
	})

	t.Run("absent own control anchored at exact workspace permits only that ancestor", func(t *testing.T) {
		directAbsent := filepath.Join(workspace, "future-control", "git")
		in := boundary.JurisdictionInput{
			OwnWorksource:  boundary.WorksourceRoots{Workspace: workspaceID},
			OwnGitControls: []boundary.ProtectedID{absentID(directAbsent)},
		}
		if err := rejects(t, in, workspace); err != nil {
			t.Fatalf("exact own workspace rejected when it is the absent control's anchor: %v", err)
		}
		intermediate := mkdir(t, filepath.Join(workspace, "future-control"), 0o755)
		if err := rejects(t, in, intermediate); err == nil {
			t.Fatal("intermediate absent-control ancestor inherited the workspace-only exemption")
		}
	})

	t.Run("control without an own workspace association fails closed", func(t *testing.T) {
		in := boundary.JurisdictionInput{
			OwnGitControls: []boundary.ProtectedID{protectedID(t, ownControl)},
		}
		if err := rejects(t, in, workspace); err == nil {
			t.Fatal("control ancestry was exempted without an own workspace identity")
		}
	})

	t.Run("another Worksource control beneath own workspace remains denied", func(t *testing.T) {
		in := base
		in.OtherGitControls = []boundary.ProtectedID{protectedID(t, otherControl)}
		if err := rejects(t, in, workspace); err == nil {
			t.Fatal("own workspace exposed another Worksource's nested Git control")
		}
	})

	t.Run("denied_paths remains additive", func(t *testing.T) {
		in := base
		in.DeniedPaths = []string{workspace}
		if err := rejects(t, in, workspace); err == nil {
			t.Fatal("own-control exemption subtracted an explicit denied_paths member")
		}
	})
}

// ADR-017:366-386's protected set is closed and mandatory. This sweep is
// intentionally mechanical: every input collection and every Worksource slot
// gets a public-API witness, every rejection pins its stable code, and every
// row proves an unrelated source still passes. A prior combined mutant removed
// several of these members while the fast suite remained green.
func TestMandatoryJurisdictionMemberSweep(t *testing.T) {
	dir := t.TempDir()
	unrelated := mkdir(t, filepath.Join(dir, "unrelated"), 0o755)

	mcHome := mkdir(t, filepath.Join(dir, "mc-home"), 0o700)
	quarantine := mkdir(t, filepath.Join(mcHome, "quarantine"), 0o700)
	homeClass := mkdir(t, filepath.Join(dir, "home", "Library", "Keychains"), 0o700)
	homeClass2 := mkdir(t, filepath.Join(dir, "home", "credential-class-2"), 0o700)
	gatewaySecret := mkdir(t, filepath.Join(dir, "gateway-secret"), 0o700)
	caPrivate := mkdir(t, filepath.Join(dir, "ca-private"), 0o700)
	selectedRuntime := mkdir(t, filepath.Join(dir, "runtime-selected"), 0o700)
	otherRuntime := mkdir(t, filepath.Join(dir, "runtime-other"), 0o700)
	ownWorkspace := mkdir(t, filepath.Join(dir, "control-workspace"), 0o755)
	ownGit := mkdir(t, filepath.Join(ownWorkspace, "own.git"), 0o700)
	ownGit2 := mkdir(t, filepath.Join(ownWorkspace, "own-2.git"), 0o700)
	ownMC := mkdir(t, filepath.Join(ownWorkspace, ".mission-control"), 0o700)
	ownMC2 := mkdir(t, filepath.Join(ownWorkspace, ".mission-control-2"), 0o700)
	otherGit := mkdir(t, filepath.Join(dir, "other-git"), 0o700)
	otherGit2 := mkdir(t, filepath.Join(dir, "other-git-2"), 0o700)
	otherMC := mkdir(t, filepath.Join(dir, "other-mc"), 0o700)
	otherMC2 := mkdir(t, filepath.Join(dir, "other-mc-2"), 0o700)
	deniedParent := mkdir(t, filepath.Join(dir, "denied-parent"), 0o755)
	deniedExact := mkdir(t, filepath.Join(deniedParent, "declared"), 0o755)
	deniedChild := mkdir(t, filepath.Join(deniedExact, "child"), 0o755)
	deniedSecond := mkdir(t, filepath.Join(dir, "denied-second"), 0o755)

	otherWS := boundary.WorksourceRoots{
		Workspace: mkdirProtectedID(t, filepath.Join(dir, "other-workspace")),
		Worktree:  mkdirProtectedID(t, filepath.Join(dir, "other-worktree")),
		Artifacts: []boundary.ProtectedID{
			mkdirProtectedID(t, filepath.Join(dir, "other-artifact-0")),
			mkdirProtectedID(t, filepath.Join(dir, "other-artifact-1")),
		},
		State:    mkdirProtectedID(t, filepath.Join(dir, "other-state")),
		Cache:    mkdirProtectedID(t, filepath.Join(dir, "other-cache")),
		ToolHome: mkdirProtectedID(t, filepath.Join(dir, "other-tool-home")),
	}
	secondOtherWorkspace := mkdirProtectedID(t, filepath.Join(dir, "second-other-workspace"))

	type probe struct {
		name string
		path string
	}
	tests := []struct {
		name   string
		in     boundary.JurisdictionInput
		want   string
		probes []probe
	}{
		{
			name: "whole MC_HOME catches an unenumerated child",
			in:   boundary.JurisdictionInput{MCHome: protectedID(t, mcHome)},
			want: boundary.CodeDeniedRoot,
			probes: []probe{
				{name: "quarantine", path: quarantine},
			},
		},
		{
			name: "present HOME credential class",
			in: boundary.JurisdictionInput{
				HomeClassRoots: []boundary.ProtectedID{
					protectedID(t, homeClass),
					protectedID(t, homeClass2),
				},
			},
			want: boundary.CodeDeniedRoot,
			probes: []probe{
				{name: "Library Keychains", path: homeClass},
				{name: "second class", path: homeClass2},
			},
		},
		{
			name: "gateway and CA private roots",
			in: boundary.JurisdictionInput{
				GatewaySecrets: []boundary.ProtectedID{
					protectedID(t, gatewaySecret),
					protectedID(t, caPrivate),
				},
			},
			want: boundary.CodeDeniedRoot,
			probes: []probe{
				{name: "gateway secret", path: gatewaySecret},
				{name: "CA private root", path: caPrivate},
			},
		},
		{
			name: "selected and non-selected runtime controls",
			in: boundary.JurisdictionInput{
				RuntimeControls: []boundary.ProtectedID{
					protectedID(t, selectedRuntime),
					protectedID(t, otherRuntime),
				},
			},
			want: boundary.CodeDeniedRoot,
			probes: []probe{
				{name: "selected", path: selectedRuntime},
				{name: "non-selected", path: otherRuntime},
			},
		},
		{
			name: "all control ownership collections",
			in: boundary.JurisdictionInput{
				OwnWorksource: boundary.WorksourceRoots{Workspace: protectedID(t, ownWorkspace)},
				OwnGitControls: []boundary.ProtectedID{
					protectedID(t, ownGit),
					protectedID(t, ownGit2),
				},
				OtherGitControls: []boundary.ProtectedID{
					protectedID(t, otherGit),
					protectedID(t, otherGit2),
				},
				OwnMissionControlRoots: []boundary.ProtectedID{
					protectedID(t, ownMC),
					protectedID(t, ownMC2),
				},
				OtherMissionControlRoots: []boundary.ProtectedID{
					protectedID(t, otherMC),
					protectedID(t, otherMC2),
				},
			},
			want: boundary.CodeDeniedRoot,
			probes: []probe{
				{name: "own Git", path: ownGit},
				{name: "own Git second", path: ownGit2},
				{name: "other Git", path: otherGit},
				{name: "other Git second", path: otherGit2},
				{name: "own mission-control", path: ownMC},
				{name: "own mission-control second", path: ownMC2},
				{name: "other mission-control", path: otherMC},
				{name: "other mission-control second", path: otherMC2},
			},
		},
		{
			name: "denied_paths is bidirectional",
			in:   boundary.JurisdictionInput{DeniedPaths: []string{deniedExact, deniedSecond}},
			want: boundary.CodeDeniedRoot,
			probes: []probe{
				{name: "ancestor", path: deniedParent},
				{name: "exact", path: deniedExact},
				{name: "descendant", path: deniedChild},
				{name: "second entry", path: deniedSecond},
			},
		},
		{
			name: "every other-Worksource root slot",
			in: boundary.JurisdictionInput{
				OtherWorksources: []boundary.WorksourceRoots{
					otherWS,
					{Workspace: secondOtherWorkspace},
				},
			},
			want: boundary.CodeCrossWorksource,
			probes: []probe{
				{name: "workspace", path: otherWS.Workspace.Canonical},
				{name: "worktree", path: otherWS.Worktree.Canonical},
				{name: "artifact 0", path: otherWS.Artifacts[0].Canonical},
				{name: "artifact 1", path: otherWS.Artifacts[1].Canonical},
				{name: "state", path: otherWS.State.Canonical},
				{name: "cache", path: otherWS.Cache.Canonical},
				{name: "tool home", path: otherWS.ToolHome.Canonical},
				{name: "second Worksource", path: secondOtherWorkspace.Canonical},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, rejects := unionFixture(t, tt.in, dir)
			for _, p := range tt.probes {
				t.Run(p.name, func(t *testing.T) {
					err := rejects(p.path)
					if got := codeOf(t, err); got != tt.want {
						t.Fatalf("code = %q, want %q (error: %v)", got, tt.want, err)
					}
				})
			}
			if err := rejects(unrelated); err != nil {
				t.Fatalf("unrelated control source rejected: %v", err)
			}
		})
	}

	t.Run("omitted HOME credential class is not a member", func(t *testing.T) {
		_, rejects := unionFixture(t, boundary.JurisdictionInput{}, dir)
		if err := rejects(homeClass); err != nil {
			t.Fatalf("omitted HOME credential class rejected: %v", err)
		}
	})
}

// The own Worksource is not a protected member. Each source is deliberately
// also supplied in another Worksource's Workspace slot: it passes only if
// identity-based own detection actually visits that particular own slot.
func TestEveryOwnWorksourceRootSlotPermitsByIdentity(t *testing.T) {
	dir := t.TempDir()
	members := []struct {
		name string
		id   boundary.ProtectedID
		own  func(boundary.ProtectedID) boundary.WorksourceRoots
	}{
		{name: "workspace", id: mkdirProtectedID(t, filepath.Join(dir, "workspace")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots { return boundary.WorksourceRoots{Workspace: id} }},
		{name: "worktree", id: mkdirProtectedID(t, filepath.Join(dir, "worktree")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots { return boundary.WorksourceRoots{Worktree: id} }},
		{name: "artifact 0", id: mkdirProtectedID(t, filepath.Join(dir, "artifact-0")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots {
			return boundary.WorksourceRoots{Artifacts: []boundary.ProtectedID{id}}
		}},
		{name: "artifact 1", id: mkdirProtectedID(t, filepath.Join(dir, "artifact-1")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots {
			return boundary.WorksourceRoots{Artifacts: []boundary.ProtectedID{{}, id}}
		}},
		{name: "state", id: mkdirProtectedID(t, filepath.Join(dir, "state")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots { return boundary.WorksourceRoots{State: id} }},
		{name: "cache", id: mkdirProtectedID(t, filepath.Join(dir, "cache")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots { return boundary.WorksourceRoots{Cache: id} }},
		{name: "tool home", id: mkdirProtectedID(t, filepath.Join(dir, "tool-home")), own: func(id boundary.ProtectedID) boundary.WorksourceRoots { return boundary.WorksourceRoots{ToolHome: id} }},
	}

	for _, member := range members {
		t.Run(member.name, func(t *testing.T) {
			own := member.own(member.id)
			_, rejects := unionFixture(t, boundary.JurisdictionInput{
				OwnWorksource: own,
				OtherWorksources: []boundary.WorksourceRoots{{
					Workspace: member.id,
				}},
			}, dir)
			if err := rejects(member.id.Canonical); err != nil {
				t.Fatalf("own %s rejected through same-file other alias: %v", member.name, err)
			}
		})
	}
}

func mkdirProtectedID(t *testing.T, path string) boundary.ProtectedID {
	t.Helper()
	return protectedID(t, mkdir(t, path, 0o755))
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
		Home:                     mkdir(t, filepath.Join(dir, "home"), 0o750),
		OtherMissionControlRoots: []boundary.ProtectedID{protectedID(t, protected)},
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
			Home:                     mkdir(t, filepath.Join(dir, "home3"), 0o750),
			OtherMissionControlRoots: []boundary.ProtectedID{protectedID(t, roProtected)},
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
			Home:                     mkdir(t, filepath.Join(dir, "home2"), 0o750),
			OtherMissionControlRoots: []boundary.ProtectedID{protectedID(t, outside)},
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
