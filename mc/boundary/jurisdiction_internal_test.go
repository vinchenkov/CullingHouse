package boundary

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ADR-021 D1/D8: "an absent member is a MEMBER". The external test can only see
// that construction does not error, which a silent skip also satisfies — a
// mutation that dropped absent members passed it. The direction that would
// expose the skip (the ancestor branch) does not exist until step 7, so this
// internal test pins the registration itself now rather than leaving a
// known-vacuous assertion in the suite.
func TestAbsentMemberIsRegisteredNotSkipped(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}

	absent := filepath.Join(dir, "mc-home-not-created-yet")
	j, err := ResolveJurisdiction(JurisdictionInput{
		Home:   home,
		MCHome: ProtectedID{Canonical: absent},
	}, os.Getuid())
	if err != nil {
		t.Fatalf("ResolveJurisdiction() = %v", err)
	}

	var found bool
	for _, r := range j.roots {
		if r.id.Canonical == absent {
			found = true
			if r.id.Present() {
				t.Errorf("absent member %q reports Present()", absent)
			}
		}
	}
	if !found {
		t.Fatalf("absent member %q was SKIPPED, not registered; ADR-021 D8 makes protection "+
			"depend on directory creation order if absent members are dropped", absent)
	}
}

// The converse, so the test above cannot pass by registering everything blindly:
// a member the caller simply did not supply (a zero ProtectedID, no declared
// path at all) is not a member.
func TestZeroProtectedIDIsNotAMember(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}

	j, err := ResolveJurisdiction(JurisdictionInput{Home: home}, os.Getuid())
	if err != nil {
		t.Fatalf("ResolveJurisdiction() = %v", err)
	}
	for _, r := range j.roots {
		if r.id.Canonical == "" {
			t.Errorf("a zero ProtectedID was registered as a member (label %q)", r.label)
		}
	}
	if len(j.roots) != 0 {
		t.Errorf("empty input produced %d members, want 0", len(j.roots))
	}
}

// D7: the OWN Worksource's roots are not cross-Worksource members —
// ADR-017:302-303 requires them to pass Authorize as ordinary sources — and
// own/other is decided by IDENTITY, never by name. A symlink alias of the own
// root must therefore also count as own.
func TestOwnWorksourceIsNotACrossMember(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}

	mk := func(p string) ProtectedID {
		id, err := ResolveSource(p)
		if err != nil {
			t.Fatal(err)
		}
		return ProtectedID{Canonical: id.Canonical, Info: id.Info, IsDir: id.IsDir}
	}

	// The discriminator has to be two spellings that are the SAME FILE but whose
	// canonical strings DIFFER — otherwise a name-based check passes the test and
	// proves nothing. A symlink alias does not work: ResolveSource canonicalizes
	// it away, both sides land on one string, and the mutation survives (it did).
	//
	// Case does work, and only because of a measured macOS property:
	// filepath.EvalSymlinks does NOT canonicalize case and does NOT error on a
	// case-variant path — it hands your spelling back verbatim — while the kernel
	// resolves both to one inode. So "WS" and "ws" are one file with two canonical
	// spellings.
	own := mk(ws)
	other := mk(filepath.Join(dir, "WS"))
	if !sameFile(own.Info, other.Info) {
		t.Skip("case-sensitive volume: no same-file/different-spelling pair available here")
	}
	if own.Canonical == other.Canonical {
		t.Fatalf("fixture is inert: both spellings canonicalized to %q, so a name-based "+
			"own/other split would pass this test without being identity-based", own.Canonical)
	}

	// The "other" list names the own workspace through that second spelling: a
	// name-based own/other split files it as cross-Worksource.
	j, err := ResolveJurisdiction(JurisdictionInput{
		Home:             home,
		OwnWorksource:    WorksourceRoots{Workspace: own},
		OtherWorksources: []WorksourceRoots{{Workspace: other}},
	}, os.Getuid())
	if err != nil {
		t.Fatalf("ResolveJurisdiction() = %v", err)
	}
	for _, r := range j.roots {
		if r.cross {
			t.Errorf("the own Worksource's root was registered as cross-Worksource via its "+
				"alias %q; own/other must be decided by identity, not by name (ADR-021 D7)", r.id.Canonical)
		}
	}
}

// D5 leg 4 requires a portable device-boundary check in addition to Darwin's
// mount-point evidence. The injected lookup deliberately reports that no
// mount-point API exists, so parent.Dev != child.Dev is the only possible
// reason this can classify the child as a filesystem root.
func TestFilesystemRootDetectsDeviceBoundaryWithoutMountPointAPI(t *testing.T) {
	child := deviceFileInfo{name: "volume", stat: &syscall.Stat_t{Dev: 11, Ino: 2}}
	parent := deviceFileInfo{name: "parent", stat: &syscall.Stat_t{Dev: 10, Ino: 3}}

	root, why := isFilesystemRootWith(
		"/parent/volume",
		child,
		func(path string) (fs.FileInfo, error) {
			if path != "/parent" {
				t.Fatalf("Lstat(%q), want parent only", path)
			}
			return parent, nil
		},
		func(string) (string, error) { return "", errNoMountPoint },
	)
	if !root {
		t.Fatal("device-boundary filesystem root was accepted when mount-point lookup was unavailable")
	}
	if !strings.Contains(why, "device") {
		t.Fatalf("reason %q does not identify the device boundary", why)
	}
}

func TestFilesystemRootSameDeviceWithoutMountPointAPIIsOrdinary(t *testing.T) {
	child := deviceFileInfo{name: "child", stat: &syscall.Stat_t{Dev: 10, Ino: 2}}
	parent := deviceFileInfo{name: "parent", stat: &syscall.Stat_t{Dev: 10, Ino: 3}}

	root, why := isFilesystemRootWith(
		"/parent/child",
		child,
		func(string) (fs.FileInfo, error) { return parent, nil },
		func(string) (string, error) { return "", errNoMountPoint },
	)
	if root || why != "" {
		t.Fatalf("ordinary same-device directory = (%v, %q), want (false, empty)", root, why)
	}
}

// The device comparison itself is boundary evidence. If either stat object is
// unavailable, treating the candidate as an ordinary directory would be the
// fail-open answer.
func TestFilesystemRootDeviceAmbiguityDenies(t *testing.T) {
	child := deviceFileInfo{name: "volume", stat: &syscall.Stat_t{Dev: 11, Ino: 2}}
	parent := deviceFileInfo{name: "parent"}

	root, why := isFilesystemRootWith(
		"/parent/volume",
		child,
		func(string) (fs.FileInfo, error) { return parent, nil },
		func(string) (string, error) { return "", errNoMountPoint },
	)
	if !root || !strings.Contains(why, "ambiguity") {
		t.Fatalf("unknown parent device = (%v, %q), want ambiguity denial", root, why)
	}
}

type deviceFileInfo struct {
	name string
	stat *syscall.Stat_t
}

func (i deviceFileInfo) Name() string     { return i.name }
func (deviceFileInfo) Size() int64        { return 0 }
func (deviceFileInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (deviceFileInfo) ModTime() time.Time { return time.Time{} }
func (deviceFileInfo) IsDir() bool        { return true }
func (i deviceFileInfo) Sys() any         { return i.stat }
