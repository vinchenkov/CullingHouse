// This file is in the internal package (not substrate_test) on purpose: the
// mountinfo parse and the accept/refuse decision are pure and platform-neutral,
// so they are proven on every host, while only the /proc/self/mountinfo read
// behind them is Linux-only.
package substrate

import (
	"errors"
	"strings"
	"testing"
)

// The mountinfo fixtures are the shapes spike S5 actually observed on this
// Docker Desktop (spikes/05-sqlite-wal/RESULT.md row 5 and its finding note).
const (
	// A named volume inside the Docker Desktop VM: block-device-backed ext4.
	mountNamedVolume = `1234 1000 254:1 /var/lib/docker/volumes/mc-spine/_data /mc/spine rw,relatime - ext4 /dev/vda1 rw`
	// The container rootfs.
	mountOverlayRoot = `1000 999 0:50 / / rw,relatime - overlay overlay rw,lowerdir=/x`
	// The host bind as Docker Desktop actually surfaces it: NOT "virtiofs" but
	// "fakeowner", its FUSE ownership shim. A denylist keyed on "virtiofs"
	// would have accepted this, which is why the allowlist shape is the
	// load-bearing one.
	mountFakeownerBind = `1500 1000 0:77 /Users/op/dev /mc/spine rw,relatime - fakeowner /run/host_mark/Users rw`
)

func mountinfo(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestLockDomainAcceptsOnlyBlockDeviceBackedLocalFilesystems(t *testing.T) {
	for _, tc := range []struct {
		name    string
		info    string
		dir     string
		wantErr string // "" means the directory is inside the lock domain
	}{
		{
			name: "named volume is the lock domain",
			info: mountinfo(mountOverlayRoot, mountNamedVolume),
			dir:  "/mc/spine",
		},
		{
			name: "a subdirectory of the named volume rides its mount",
			info: mountinfo(mountOverlayRoot, mountNamedVolume),
			dir:  "/mc/spine/nested",
		},
		{
			name:    "Docker Desktop host bind is refused despite not being named virtiofs",
			info:    mountinfo(mountOverlayRoot, mountFakeownerBind),
			dir:     "/mc/spine",
			wantErr: "fakeowner",
		},
		{
			name:    "container rootfs is refused",
			info:    mountinfo(mountOverlayRoot),
			dir:     "/tmp",
			wantErr: "overlay",
		},
		{
			name:    "longest-prefix wins, so a bind nested under an allowed mount still loses",
			info:    mountinfo(mountOverlayRoot, mountNamedVolume, `1600 1234 0:78 / /mc/spine/bound rw - fakeowner /run/host_mark/Users rw`),
			dir:     "/mc/spine/bound",
			wantErr: "fakeowner",
		},
		{
			name:    "a prefix that is not a whole path component does not match",
			info:    mountinfo(mountOverlayRoot, mountNamedVolume),
			dir:     "/mc/spineless",
			wantErr: "overlay",
		},
		{
			name: "octal-escaped mount points are decoded before matching",
			info: mountinfo(mountOverlayRoot, `1234 1000 254:1 / /mc/my\040spine rw - ext4 /dev/vda1 rw`),
			dir:  "/mc/my spine",
		},
		{
			// Fail-closed: an allowed fstype without a block device behind it
			// is a bind of somebody else's ext4, not a runtime-local volume.
			name:    "an allowed fstype backed by a non-device source is refused",
			info:    mountinfo(mountOverlayRoot, `1234 1000 254:1 / /mc/spine rw - ext4 hostshare rw`),
			dir:     "/mc/spine",
			wantErr: "hostshare",
		},
		{
			name:    "an unknown filesystem is refused rather than assumed safe",
			info:    mountinfo(mountOverlayRoot, `1234 1000 0:90 / /mc/spine rw - 9p hostshare rw`),
			dir:     "/mc/spine",
			wantErr: "9p",
		},
		{
			name:    "no containing mount refuses rather than defaulting to accept",
			info:    mountinfo(mountNamedVolume),
			dir:     "/elsewhere",
			wantErr: "no mount",
		},
		{
			name:    "unparseable mountinfo refuses",
			info:    "garbage without a separator\n",
			dir:     "/mc/spine",
			wantErr: "no mount",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkLockDomain(strings.NewReader(tc.info), tc.dir)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("checkLockDomain(%s) = %v, want accept", tc.dir, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkLockDomain(%s) = accept, want refusal naming %q", tc.dir, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("checkLockDomain(%s) = %v, want refusal naming %q", tc.dir, err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "Inv. 24") {
				t.Fatalf("refusal %v does not cite Inv. 24", err)
			}
		})
	}
}

// Mounting twice at the same point SHADOWS rather than replaces: both lines
// stay in mountinfo, and the later one is what the process actually reads and
// writes. A guard that broke the length tie toward the first entry would judge
// a bind stacked over a named volume on the volume's ext4 line.
func TestLockDomainJudgesAShadowedMountPointOnTheEffectiveMount(t *testing.T) {
	shadowed := mountinfo(mountOverlayRoot, mountNamedVolume, mountFakeownerBind)
	if err := checkLockDomain(strings.NewReader(shadowed), "/mc/spine"); err == nil || !strings.Contains(err.Error(), "fakeowner") {
		t.Fatalf("bind stacked OVER the volume = %v, want the later mount refused", err)
	}
	// And the converse, so this is a tie-break rule and not a bias toward
	// refusal: a volume mounted over a bind is in the lock domain.
	restacked := mountinfo(mountOverlayRoot, mountFakeownerBind, mountNamedVolume)
	if err := checkLockDomain(strings.NewReader(restacked), "/mc/spine"); err != nil {
		t.Fatalf("volume stacked OVER the bind = %v, want accept", err)
	}
}

// The directory is not the whole story. Docker will bind a single file over
// spine.db inside an otherwise-legitimate named volume, which leaves the
// directory reporting ext4 while the database itself sits on VirtioFS — the
// corruption this guard exists to prevent, wearing a passing directory.
func TestLockDomainRefusesASingleFileBindInsideAnAllowedVolume(t *testing.T) {
	info := mountinfo(mountOverlayRoot, mountNamedVolume,
		`1700 1234 0:79 /Users/op/spine.db /mc/spine/spine.db rw - fakeowner /run/host_mark/Users rw`)

	if err := checkLockDomain(strings.NewReader(info), "/mc/spine"); err != nil {
		t.Fatalf("the volume DIRECTORY alone = %v; this fixture is only meaningful if the directory passes", err)
	}
	err := checkLockDomain(strings.NewReader(info), "/mc/spine", "/mc/spine/spine.db")
	if err == nil || !strings.Contains(err.Error(), "fakeowner") {
		t.Fatalf("checkLockDomain(dir+file) = %v, want the single-file bind refused", err)
	}
}

// A spine that does not exist yet has no mount line of its own; it rides its
// directory's, which is checked first. Absence must not read as a refusal, or
// provisioning a fresh spine becomes impossible.
func TestLockDomainAdmitsANotYetCreatedSpineInsideAnAllowedVolume(t *testing.T) {
	info := mountinfo(mountOverlayRoot, mountNamedVolume)
	if err := checkLockDomain(strings.NewReader(info), "/mc/spine", "/mc/spine/spine.db"); err != nil {
		t.Fatalf("checkLockDomain(fresh spine in the lock domain) = %v, want accept", err)
	}
}

// A read that fails is not a pass. The guard has no /proc to consult only on a
// platform where mc never opens the spine at all, and that exemption is made at
// the call site (GuardLockDomain), never by swallowing a read error here.
func TestLockDomainRefusesWhenMountinfoCannotBeRead(t *testing.T) {
	err := checkLockDomain(errReader{}, "/mc/spine")
	if err == nil || !strings.Contains(err.Error(), "Inv. 24") {
		t.Fatalf("checkLockDomain(unreadable) = %v, want a refusal citing Inv. 24", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("mountinfo unavailable") }
