package verbs

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mc/substrate"
)

func TestOnboardHomeProvisioningIsAtomicAndRetryable(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mc-home")
	spine := filepath.Join(home, "spine.db")
	t.Setenv("MC_HOME", home)

	originalSchema := substrate.Schema
	t.Cleanup(func() { substrate.Schema = originalSchema })
	substrate.Schema = `
		CREATE TABLE partial_provision (id INTEGER PRIMARY KEY);
		INSERT INTO missing_table VALUES (1);
	`
	if _, _, err := onboardHome(spine); err == nil {
		t.Fatal("injected schema failure unexpectedly succeeded")
	}

	if _, err := os.Stat(spine); !os.IsNotExist(err) {
		t.Fatalf("failed fresh provisioning left spine bytes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "deployment.uuid")); !os.IsNotExist(err) {
		t.Fatalf("failed provisioning left a UUID mirror: %v", err)
	}

	substrate.Schema = originalSchema
	status, _, err := onboardHome(spine)
	if err != nil {
		t.Fatalf("retry after rolled-back provisioning: %v", err)
	}
	if status != "done" {
		t.Fatalf("retry status = %q, want done", status)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var uuid string
	if err := db.QueryRow(`SELECT deployment_uuid FROM meta WHERE id = 1`).Scan(&uuid); err != nil {
		t.Fatal(err)
	}
	if uuid == "" {
		t.Fatal("retry did not create the deployment identity")
	}
}

func TestOnboardConcurrentReplaysSerialize(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mc-home")
	spine := filepath.Join(home, "spine.db")
	t.Setenv("MC_HOME", home)
	if _, _, err := onboardHome(spine); err != nil {
		t.Fatal(err)
	}

	runPair := func(t *testing.T, fn func() (string, error)) []string {
		t.Helper()
		statuses := make([]string, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				statuses[i], errs[i] = fn()
			}(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("concurrent replay %d: %v", i, err)
			}
		}
		return statuses
	}
	wantDoneOK := func(t *testing.T, got []string) {
		t.Helper()
		counts := map[string]int{}
		for _, status := range got {
			counts[status]++
		}
		if counts["done"] != 1 || counts["ok"] != 1 {
			t.Fatalf("concurrent statuses = %v, want one done and one ok", got)
		}
	}

	wantDoneOK(t, runPair(t, func() (string, error) {
		status, _, err := onboardRouting()
		return status, err
	}))
	wantDoneOK(t, runPair(t, func() (string, error) {
		status, _, err := onboardTunables(OnboardArgs{Spine: spine, TimeoutMinutes: 7})
		return status, err
	}))
	wantDoneOK(t, runPair(t, func() (string, error) {
		status, _, err := onboardSurfaces(OnboardArgs{
			Spine: spine, ConsoleScheduleSet: true,
			ConsoleHour: 8, ConsoleMinute: 0, ConsoleTZ: "UTC",
		})
		return status, err
	}))

	workspaces := []string{filepath.Join(t.TempDir(), "one"), filepath.Join(t.TempDir(), "two")}
	for _, root := range workspaces {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	statuses := make([]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], _, errs[i] = onboardWorksource(OnboardArgs{
				Spine: spine, Worksource: []string{"ws-one", "ws-two"}[i], WorkspaceRoot: workspaces[i],
			})
		}(i)
	}
	wg.Wait()
	successes, refusals := 0, 0
	for i := range 2 {
		if errs[i] == nil && statuses[i] == "done" {
			successes++
		} else if errs[i] != nil {
			refusals++
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatalf("concurrent first Worksources = statuses %v errors %v", statuses, errs)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worksources`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent first Worksource left %d rows", count)
	}
}

func TestOnboardConcurrentFreshHomeNeverDeletesTheWinner(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mc-home")
	spine := filepath.Join(home, "spine.db")
	t.Setenv("MC_HOME", home)

	const callers = 8
	statuses := make([]string, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			statuses[i], _, errs[i] = onboardHome(spine)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("fresh-home caller %d failed (statuses %v): %v", i, statuses, err)
		}
	}
	if err := requireOnboardIdentity(spine); err != nil {
		t.Fatalf("concurrent fresh home lost its winner: %v", err)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var metaRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM meta WHERE id = 1`).Scan(&metaRows); err != nil {
		t.Fatal(err)
	}
	if metaRows != 1 {
		t.Fatalf("concurrent fresh home left %d meta rows", metaRows)
	}
}
