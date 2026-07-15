package boundary_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mc/boundary"
)

// ADR-021 D4 — broad_root: "a source may not equal or be an ancestor of HOME,
// while an allowlisted descendant such as ~/src/project remains eligible unless
// it hits another protected root" (ADR-017:390-393).
//
// PERMIT SIDE FIRST. A deny-only test here passes trivially — an implementation
// that rejects everything satisfies every deny assertion — and the alias-route
// expansion below is exactly the kind of widening that could swallow the one
// mount the system exists to make.

func jurisdictionWithHome(t *testing.T, home string) boundary.Jurisdiction {
	t.Helper()
	j, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{Home: home}, os.Getuid())
	if err != nil {
		t.Fatalf("ResolveJurisdiction(home=%q) = %v", home, err)
	}
	return j
}

func rejectsPath(t *testing.T, j boundary.Jurisdiction, path string) error {
	t.Helper()
	id, err := boundary.ResolveSource(path)
	if err != nil {
		t.Fatalf("ResolveSource(%q) = %v", path, err)
	}
	return j.Rejects(id, boundary.TypedClaim{})
}

// THE permit side. ADR-017:391-392 keeps an allowlisted descendant eligible; §5's
// Worksource model puts every workspace under HOME, so a broad_root that
// swallowed descendants would make the product unable to mount anything.
func TestBroadRootPermitsDescendantsOfHome(t *testing.T) {
	dir := t.TempDir()
	home := mkdir(t, filepath.Join(dir, "home"), 0o750)
	j := jurisdictionWithHome(t, home)

	project := mkdir(t, filepath.Join(home, "src", "project"), 0o755)
	if err := rejectsPath(t, j, project); err != nil {
		t.Fatalf("~/src/project rejected: %v — broad_root is HOME's DIRECTIONAL weakening; "+
			"descendants stay eligible (ADR-017:390-393)", err)
	}
	// HOME's immediate child, and a sibling of HOME, both stay eligible.
	if err := rejectsPath(t, j, mkdir(t, filepath.Join(home, "src"), 0o755)); err != nil {
		t.Errorf("~/src rejected: %v", err)
	}
	if err := rejectsPath(t, j, mkdir(t, filepath.Join(dir, "elsewhere"), 0o755)); err != nil {
		t.Errorf("a sibling of HOME rejected: %v", err)
	}
}

// The deny side: S == HOME, and S a strict ancestor of HOME.
func TestBroadRootRejectsHomeAndItsAncestors(t *testing.T) {
	dir := t.TempDir()
	home := mkdir(t, filepath.Join(dir, "home"), 0o750)
	j := jurisdictionWithHome(t, home)

	for _, path := range []string{home, dir, filepath.Dir(dir), "/"} {
		t.Run(path, func(t *testing.T) {
			err := rejectsPath(t, j, path)
			if err == nil {
				t.Fatalf("%q permitted; it equals or is an ancestor of HOME %q", path, home)
			}
			if got := codeOf(t, err); got != boundary.CodeDeniedRoot {
				t.Errorf("code = %q, want %q — broad_root borrows denied_root; ADR-017's closed "+
					"code list has no mount.broad_root", got, boundary.CodeDeniedRoot)
			}
		})
	}
}

// ADR-021 D4: the parenthetical ($HOME, /Users, /) is "illustrative of HOME's
// ancestor chain on macOS, NOT a literal set". Implementing it as a hardcoded
// list would silently under-protect any non-/Users HOME.
//
// This HOME lives under /var/folders, so: its own ancestors must reject, and
// /Users — the literal from the parenthetical — must NOT, because it is nothing
// to this HOME.
func TestBroadRootIsComputedNotHardcoded(t *testing.T) {
	dir := t.TempDir() // /var/folders/... — a non-/Users HOME
	home := mkdir(t, filepath.Join(dir, "home"), 0o750)
	j := jurisdictionWithHome(t, home)

	if err := rejectsPath(t, j, "/private/var"); err == nil {
		t.Error("/private/var permitted; it is a real ancestor of this HOME, and a hardcoded " +
			"(/Users, /) list would miss it")
	}
	if _, err := os.Stat("/Users"); err == nil {
		if err := rejectsPath(t, j, "/Users"); err != nil {
			t.Errorf("/Users rejected for a HOME that is not under it: %v — the parenthetical "+
				"is illustrative, not a literal set", err)
		}
	}
}

// The firmlink hole, measured. /Users is an APFS firmlink: lstat shows a plain
// directory, EvalSymlinks does not see through it, and HOME's filepath.Dir chain
// is (HOME, /Users, /). But HOME's volume mount point is /System/Volumes/Data,
// which is in no chain, is os.SameFile with nothing in one, and through which the
// whole of HOME — including ~/.ssh — is readable.
//
// Before this rule, an adversarial skeptic drove /System/Volumes/Data through the
// real Authorize and got AUTHORIZED rw, then read 419 bytes of the operator's
// OpenSSH private key out of the resulting mount.
func TestBroadRootRejectsFirmlinkAliasRoutes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("firmlinks are macOS-specific")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no real HOME: %v", err)
	}
	j := jurisdictionWithHome(t, home)

	// Sanity: the plain Dir chain still rejects.
	for _, path := range []string{home, "/"} {
		if err := rejectsPath(t, j, path); err == nil {
			t.Fatalf("fixture broken: %q permitted", path)
		}
	}

	for _, path := range []string{"/System/Volumes/Data", "/System/Volumes", "/System"} {
		t.Run(path, func(t *testing.T) {
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%q absent on this machine: %v", path, err)
			}
			err := rejectsPath(t, j, path)
			if err == nil {
				t.Fatalf("%q PERMITTED. It is an ancestor of HOME by reachability — the whole of "+
					"HOME, ~/.ssh included, is readable through it — and no filepath.Dir walk from "+
					"HOME reaches it. This exact source was AUTHORIZED rw and leaked the operator's "+
					"private key.", path)
			}
			if got := codeOf(t, err); got != boundary.CodeDeniedRoot {
				t.Errorf("code = %q, want %q", got, boundary.CodeDeniedRoot)
			}
		})
	}

	// And the permit side survives the expansion: a descendant of the real HOME is
	// still eligible. The expansion must not swallow the mount the system exists
	// to make.
	if err := rejectsPath(t, j, filepath.Join(home, "Documents")); err != nil {
		if _, statErr := os.Stat(filepath.Join(home, "Documents")); statErr == nil {
			t.Errorf("~/Documents rejected after the alias-route expansion: %v", err)
		}
	}
}
