package deployment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeLaunchd struct {
	loaded      map[string]bool
	bootstraps  int
	failAt      int
	onBootstrap func(label string)
}

func (f *fakeLaunchd) Loaded(label string) (bool, error) { return f.loaded[label], nil }

func (f *fakeLaunchd) Bootstrap(plist string) error {
	f.bootstraps++
	label := strings.TrimSuffix(filepath.Base(plist), ".plist")
	if f.failAt == f.bootstraps {
		return errors.New("injected bootstrap failure")
	}
	f.loaded[label] = true
	if f.onBootstrap != nil {
		f.onBootstrap(label)
	}
	return nil
}

func (f *fakeLaunchd) Bootout(label string) error {
	delete(f.loaded, label)
	return nil
}

func activationFixture(t *testing.T) (string, string, string, *fakeLaunchd, SupervisionActivationDeps) {
	t.Helper()
	home, spec := supervisionFixture(t)
	_, result, err := PrepareSupervision(home, spec, func(string) (bool, error) { return false, nil })
	if err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(t.TempDir(), "Library")
	if err := os.Mkdir(library, 0o700); err != nil {
		t.Fatal(err)
	}
	launchAgents := filepath.Join(library, "LaunchAgents")
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	launchd := &fakeLaunchd{loaded: map[string]bool{}}
	launchd.onBootstrap = func(label string) {
		if label != result.ResidentLabel {
			return
		}
		health := filepath.Join(home, "health")
		if err := os.Mkdir(health, 0o700); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
		body := fmt.Sprintf("{\"version\":1,\"release_build_id\":%q,\"config_schema_version\":1,\"completed_at\":%q}\n",
			spec.ReleaseBuildID, now.Add(time.Second).Format(time.RFC3339Nano))
		if err := os.WriteFile(filepath.Join(health, "resident-tick.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deps := SupervisionActivationDeps{Launchd: launchd, Now: func() time.Time { return now }, Sleep: func(time.Duration) {}}
	return home, launchAgents, spec.ReleaseBuildID, launchd, deps
}

func TestActivateSupervisionLoadsBothAndRequiresNewTick(t *testing.T) {
	home, launchAgents, release, launchd, deps := activationFixture(t)
	status, result, err := ActivateSupervision(home, launchAgents, release, deps)
	if err != nil || status != "done" {
		t.Fatalf("activate status=%q result=%+v err=%v", status, result, err)
	}
	for _, label := range []string{result.DashboardLabel, result.ResidentLabel} {
		if !launchd.loaded[label] {
			t.Fatalf("%s not loaded", label)
		}
		path := filepath.Join(launchAgents, label+".plist")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			t.Fatalf("installed plist %s info=%v err=%v", path, info, err)
		}
	}
}

func TestActivateSupervisionRollsBackEveryPartialFirstActivation(t *testing.T) {
	t.Run("second bootstrap", func(t *testing.T) {
		home, launchAgents, release, launchd, deps := activationFixture(t)
		launchd.failAt = 2
		if _, _, err := ActivateSupervision(home, launchAgents, release, deps); err == nil || !strings.Contains(err.Error(), "rolled back") {
			t.Fatalf("err=%v", err)
		}
		if len(launchd.loaded) != 0 {
			t.Fatalf("jobs survived rollback: %v", launchd.loaded)
		}
		entries, err := os.ReadDir(launchAgents)
		if err != nil || len(entries) != 0 {
			t.Fatalf("installed plists survived rollback: %v %v", entries, err)
		}
	})

	t.Run("missing tick", func(t *testing.T) {
		home, launchAgents, release, launchd, deps := activationFixture(t)
		launchd.onBootstrap = nil
		if _, _, err := ActivateSupervision(home, launchAgents, release, deps); err == nil || !strings.Contains(err.Error(), "no successful release-bound tick") {
			t.Fatalf("err=%v", err)
		}
		if len(launchd.loaded) != 0 {
			t.Fatalf("jobs survived receipt timeout: %v", launchd.loaded)
		}
		entries, err := os.ReadDir(launchAgents)
		if err != nil || len(entries) != 0 {
			t.Fatalf("plists survived receipt timeout: %v %v", entries, err)
		}
	})
}

func TestActivateSupervisionRefusesPartialPreexistingState(t *testing.T) {
	home, launchAgents, release, launchd, deps := activationFixture(t)
	names, _ := RuntimeNames(home)
	launchd.loaded["com.mission-control."+names.Suffix+".resident"] = true
	if _, _, err := ActivateSupervision(home, launchAgents, release, deps); err == nil || !strings.Contains(err.Error(), "partially loaded") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(launchAgents); !os.IsNotExist(err) {
		t.Fatalf("partial-state refusal wrote LaunchAgents: %v", err)
	}
}

func TestInspectActiveSupervisionRequiresBothExactJobsAndFreshReceipt(t *testing.T) {
	home, launchAgents, release, launchd, deps := activationFixture(t)
	_, result, err := ActivateSupervision(home, launchAgents, release, deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectActiveSupervision(home, launchAgents, release, launchd, deps.Now().Add(time.Minute)); err != nil {
		t.Fatalf("inspect active: %v", err)
	}
	delete(launchd.loaded, result.DashboardLabel)
	if _, err := InspectActiveSupervision(home, launchAgents, release, launchd, deps.Now().Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("missing dashboard accepted: %v", err)
	}
	launchd.loaded[result.DashboardLabel] = true
	if _, err := InspectActiveSupervision(home, launchAgents, release, launchd, deps.Now().Add(4*time.Minute)); err == nil || !strings.Contains(err.Error(), "last two minutes") {
		t.Fatalf("stale receipt accepted: %v", err)
	}
}
