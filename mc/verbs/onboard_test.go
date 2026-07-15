package verbs

import (
	"os"
	"path/filepath"
	"strings"
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

// §16.4: "First-ever provisioning writes a meta table — deployment UUID,
// schema version, created-at". The version it writes must be the version of
// the schema it just applied. Stamping a literal leaves meta describing a
// spine that no longer exists: onboard would then read its own fresh spine
// back as migratable and run ALTER TABLE against columns already there.
func TestProvisioningStampsTheSchemaItApplied(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mc-home")
	spine := filepath.Join(home, "spine.db")
	t.Setenv("MC_HOME", home)

	if _, _, err := onboardHome(spine); err != nil {
		t.Fatalf("onboard: %v", err)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow(`SELECT schema_version FROM meta WHERE id = 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != substrate.CurrentSchemaVersion {
		t.Errorf("onboard stamped schema_version %d, want %d — meta must name the schema on disk",
			version, substrate.CurrentSchemaVersion)
	}
	// The self-consistency proof: a spine mc just provisioned has nothing to
	// migrate. If this reports a change, provisioning and Schema disagree.
	changed, err := substrate.Migrate(db)
	if err != nil {
		t.Fatalf("Migrate a freshly provisioned spine: %v", err)
	}
	if changed {
		t.Error("a freshly provisioned spine reported a pending migration")
	}
}

// §16.4: "present with an older schema → migrate". Accepting an older spine
// without converting it is the worst of both worlds — mc then drives a v1
// spine with code that expects the current one — so onboard owns the
// conversion, and it must preserve the deployment's identity and history.
func TestOnboardMigratesAnOlderSpine(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mc-home")
	spine := filepath.Join(home, "spine.db")
	t.Setenv("MC_HOME", home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}

	// A real v1 deployment carrying history.
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV1ForOnboard(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'legacy-uuid', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO activity (actor, kind, detail) VALUES ('operator', 'task.added', '{}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, _, err := onboardHome(spine); err != nil {
		t.Fatalf("onboard an older spine: %v", err)
	}

	db, err = substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var uuid string
	if err := db.QueryRow(`SELECT schema_version, deployment_uuid FROM meta WHERE id = 1`).
		Scan(&version, &uuid); err != nil {
		t.Fatal(err)
	}
	if version != substrate.CurrentSchemaVersion {
		t.Errorf("onboard left schema_version %d, want %d — the older spine was accepted but never migrated",
			version, substrate.CurrentSchemaVersion)
	}
	if uuid != "legacy-uuid" {
		t.Errorf("migration changed the deployment identity to %q", uuid)
	}
	var history int
	if err := db.QueryRow(`SELECT count(*) FROM activity`).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history != 1 {
		t.Errorf("migration lost history: %d activity rows, want 1", history)
	}
	// The converted spine carries the fences, not just the columns.
	if _, err := db.Exec(`INSERT INTO activity (actor, kind, dispatch_key)
		VALUES ('mc', 'dispatch.health', ?)`, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("migrated spine rejects a legal receipt: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO activity (actor, kind, dispatch_key)
		VALUES ('mc', 'dispatch.health', ?)`, strings.Repeat("a", 64)); err == nil {
		t.Error("migrated spine accepted a duplicate dispatch_key — the D2 replay fence is missing")
	}
}

func schemaV1ForOnboard(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "substrate", "testdata", "schema-v1.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
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
