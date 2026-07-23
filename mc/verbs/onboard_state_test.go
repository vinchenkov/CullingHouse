package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/substrate"
)

const (
	testOnboardBuild   = "test-build"
	testOnboardControl = 1
	testOnboardConfig  = 1
)

func provisionOnboardStateFixture(t *testing.T) (string, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	spine := filepath.Join(home, "spine.db")
	t.Setenv("MC_HOME", home)
	if _, _, err := onboardHome(spine); err != nil {
		t.Fatal(err)
	}
	return home, spine
}

func TestOnboardStateWorksourceKeepsFilesystemAuthorityOnHost(t *testing.T) {
	_, spine := provisionOnboardStateFixture(t)
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	req, err := PrepareOnboardState(OnboardArgs{
		Section: "worksource", Worksource: "primary", WorkspaceRoot: link,
	}, testOnboardBuild, testOnboardControl, testOnboardConfig)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if req.WorkspaceRoot != canonicalRoot {
		t.Fatalf("prepared workspace root = %q, want canonical %q", req.WorkspaceRoot, canonicalRoot)
	}

	// The helper half must not stat or resolve a host path. Removing it after
	// host attestation therefore does not prevent the spine mutation; the
	// host-side finalizer catches the race and refuses to call it healthy.
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := RecheckOnboardStateHost(req); err == nil || !strings.Contains(err.Error(), "recheck workspace root") {
		t.Fatalf("host recheck error = %v", err)
	}
	result, err := OnboardState(spine, req, testOnboardBuild, testOnboardControl, testOnboardConfig)
	if err != nil {
		t.Fatalf("helper state operation consulted host path: %v", err)
	}
	if _, _, err := FinalizeOnboardState(req, result); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("finalize error = %v, want unavailable host root", err)
	}
}

func TestOnboardStateReplaysAllSpineSections(t *testing.T) {
	_, spine := provisionOnboardStateFixture(t)
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []OnboardArgs{
		{Section: "worksource", Worksource: "primary", WorkspaceRoot: root},
		{Section: "tunables", TimeoutMinutes: 17},
		{Section: "surfaces", ConsoleScheduleSet: true, ConsoleHour: 8, ConsoleMinute: 30, ConsoleTZ: "UTC"},
	}
	for _, args := range cases {
		t.Run(args.Section, func(t *testing.T) {
			req, err := PrepareOnboardState(args, testOnboardBuild, testOnboardControl, testOnboardConfig)
			if err != nil {
				t.Fatal(err)
			}
			first, err := OnboardState(spine, req, testOnboardBuild, testOnboardControl, testOnboardConfig)
			if err != nil {
				t.Fatal(err)
			}
			status, _, err := FinalizeOnboardState(req, first)
			if err != nil || status != "done" {
				t.Fatalf("first finalize = %q, %v", status, err)
			}
			second, err := OnboardState(spine, req, testOnboardBuild, testOnboardControl, testOnboardConfig)
			if err != nil {
				t.Fatal(err)
			}
			status, _, err = FinalizeOnboardState(req, second)
			if err != nil || status != "ok" {
				t.Fatalf("replay finalize = %q, %v", status, err)
			}
		})
	}
}

func TestOnboardStateFrameIsClosedAndIdentityBound(t *testing.T) {
	_, spine := provisionOnboardStateFixture(t)
	req, err := PrepareOnboardState(OnboardArgs{Section: "tunables"}, testOnboardBuild, testOnboardControl, testOnboardConfig)
	if err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.ReleaseBuildID = "other-build"
	if _, err := OnboardState(spine, bad, testOnboardBuild, testOnboardControl, testOnboardConfig); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("mismatched build error = %v", err)
	}
	if req.SpineSchemaVersion != substrate.CurrentSchemaVersion || req.DeploymentUUID == "" {
		t.Fatalf("request lacks spine/deployment identity: %+v", req)
	}
	routing, err := PrepareOnboardState(OnboardArgs{Section: "routing"}, testOnboardBuild, testOnboardControl, testOnboardConfig)
	if err != nil {
		t.Fatal(err)
	}
	if routing.WorkspaceRoot != "" || routing.Worksource != "" || routing.ConsoleTZ != "" {
		t.Fatalf("routing identity frame leaked host/config data: %+v", routing)
	}
}
