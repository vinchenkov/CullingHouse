package property

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"mc/domain"
	"mc/substrate"
)

const (
	mutantDropBlockedFilter   = "drop-blocked-filter"
	mutantIgnorePacketArchive = "ignore-packet-archive"
	mutantBlurBudgets         = "blur-budgets"
	mutantWeakenLeaseToken    = "weaken-lease-token"
	mutantExceedWIPCap        = "exceed-wip-cap"
)

var requiredMutants = []string{
	mutantDropBlockedFilter,
	mutantIgnorePacketArchive,
	mutantBlurBudgets,
	mutantWeakenLeaseToken,
	mutantExceedWIPCap,
}

type mutantOutcome struct {
	Hit     bool
	Killed  bool
	Witness string
}

func TestPlantedMutantsKilled(t *testing.T) {
	registry := map[string]func(*testing.T) mutantOutcome{
		mutantDropBlockedFilter:   killDropBlockedFilter,
		mutantIgnorePacketArchive: killIgnorePacketArchive,
		mutantBlurBudgets:         killBlurBudgets,
		mutantWeakenLeaseToken:    killWeakenLeaseToken,
		mutantExceedWIPCap:        killExceedWIPCap,
	}
	got := make([]string, 0, len(registry))
	for name := range registry {
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), requiredMutants...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planted-mutant registry = %v, want exact contract list %v", got, want)
	}

	for _, name := range want {
		t.Run(name, func(t *testing.T) {
			outcome := registry[name](t)
			if !outcome.Hit {
				t.Fatalf("mutant override was never exercised; witness=%s", outcome.Witness)
			}
			if !outcome.Killed {
				t.Fatalf("mutant survived bounded corpus; witness=%s", outcome.Witness)
			}
		})
	}
}

func killDropBlockedFilter(t *testing.T) mutantOutcome {
	t.Helper()
	for _, seed := range []int64{1801, 1802} {
		for i := 0; i < 32; i++ {
			c := dominantIneligibleCase(seed, i, "blocked")
			base, extended, mutant := evaluateInvisibilityCase(c)
			if !sameAction(base, extended) {
				t.Fatalf("baseline blocked-row invisibility failed seed=%d case=%d: %+v != %+v",
					seed, i, base, extended)
			}
			if !sameAction(base, mutant) {
				return mutantOutcome{Hit: true, Killed: true,
					Witness: fmt.Sprintf("seed=%d case=%d inserted=%d", seed, i, c.Inserted)}
			}
		}
	}
	return mutantOutcome{Hit: true, Witness: "all blocked rows cleared by mutant"}
}

func killIgnorePacketArchive(t *testing.T) mutantOutcome {
	t.Helper()
	for _, seed := range []int64{1803, 1804} {
		for i := 0; i < 32; i++ {
			c := archivedPacketCase(seed, i)
			base, extended, mutant := evaluateInvisibilityCase(c)
			if !sameAction(base, extended) {
				t.Fatalf("baseline archived-packet invisibility failed seed=%d case=%d: %+v != %+v",
					seed, i, base, extended)
			}
			if !sameAction(base, mutant) {
				return mutantOutcome{Hit: true, Killed: true,
					Witness: fmt.Sprintf("seed=%d case=%d packet=%d", seed, i, c.Inserted)}
			}
		}
	}
	return mutantOutcome{Hit: true, Witness: "all archived packet flags cleared by mutant"}
}

type counters struct {
	Correction int
	Retries    int
}

func infraChargeProperty(before, after counters) bool {
	wantRetries := before.Retries - 1
	if wantRetries < 0 {
		wantRetries = 0
	}
	return after.Correction == before.Correction && after.Retries == wantRetries
}

func killBlurBudgets(t *testing.T) mutantOutcome {
	t.Helper()
	production, productionTask := budgetFixture(t)
	before := readCounters(t, production, productionTask)
	if err := inImmediate(production, func(ctx context.Context, q domain.Q) error {
		_, err := domain.ChargeInfra(ctx, q, productionTask, "generated infra failure")
		return err
	}); err != nil {
		t.Fatalf("production ChargeInfra: %v", err)
	}
	if after := readCounters(t, production, productionTask); !infraChargeProperty(before, after) {
		t.Fatalf("production violated two-budget property: before=%+v after=%+v", before, after)
	}

	mutant, mutantTask := budgetFixture(t)
	mutantBefore := readCounters(t, mutant, mutantTask)
	// Planted defect: the infra arm spends the quality/correction budget and
	// forgets the dispatch-death budget it exclusively owns.
	if _, err := mutant.Exec(`
		UPDATE tasks SET correction_count = correction_count + 1 WHERE id = ?`, mutantTask); err != nil {
		t.Fatalf("exercise blurred-budget mutant: %v", err)
	}
	mutantAfter := readCounters(t, mutant, mutantTask)
	return mutantOutcome{
		Hit: true, Killed: !infraChargeProperty(mutantBefore, mutantAfter),
		Witness: fmt.Sprintf("before=%+v after=%+v", mutantBefore, mutantAfter),
	}
}

func killWeakenLeaseToken(t *testing.T) mutantOutcome {
	t.Helper()
	production := openPropertySpine(t)
	productionTask := insertedTask(t, production, "seeded")
	claimFixture(t, production, productionTask, "live-token")
	before := lockSnapshot(t, production)
	err := inImmediate(production, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Heartbeat(ctx, q, "stale-token")
		return err
	})
	if codeOf(err) != domain.CodeStaleRun || lockSnapshot(t, production) != before {
		t.Fatalf("production stale heartbeat was not fenced atomically: code=%q before=%q after=%q",
			codeOf(err), before, lockSnapshot(t, production))
	}

	mutant := openPropertySpine(t)
	mutantTask := insertedTask(t, mutant, "seeded")
	claimFixture(t, mutant, mutantTask, "live-token")
	mutantBefore := lockSnapshot(t, mutant)
	// Planted defect: a stale caller updates the singleton without comparing
	// its run token to lock.run_id.
	_, mutantErr := mutant.Exec(`
		UPDATE lock SET last_heartbeat_at = '2099-01-01 00:00:00' WHERE id = 1`)
	mutantAfter := lockSnapshot(t, mutant)
	killed := codeOf(mutantErr) != domain.CodeStaleRun || mutantAfter != mutantBefore
	return mutantOutcome{
		Hit: true, Killed: killed,
		Witness: fmt.Sprintf("error=%v before=%q after=%q", mutantErr, mutantBefore, mutantAfter),
	}
}

func killExceedWIPCap(t *testing.T) mutantOutcome {
	t.Helper()
	production := openPropertySpine(t)
	productionTasks := fourPackagedTasks(t, production)
	for _, id := range productionTasks[:3] {
		if err := inImmediate(production, func(ctx context.Context, q domain.Q) error {
			return domain.Birth(ctx, q, id, fmt.Sprintf("packet-%d.html", id))
		}); err != nil {
			t.Fatalf("production packet birth %d: %v", id, err)
		}
	}
	err := inImmediate(production, func(ctx context.Context, q domain.Q) error {
		return domain.Birth(ctx, q, productionTasks[3], "fourth.html")
	})
	if codeOf(err) != domain.CodeWIPCap || packetOccupancy(t, production) != 3 {
		t.Fatalf("production WIP property failed: code=%q occupancy=%d",
			codeOf(err), packetOccupancy(t, production))
	}

	mutant := openPropertySpine(t)
	mutantTasks := fourPackagedTasks(t, mutant)
	for _, id := range mutantTasks[:3] {
		if _, err := mutant.Exec(`INSERT INTO review_packets (task_id) VALUES (?)`, id); err != nil {
			t.Fatalf("mutant fixture packet birth %d: %v", id, err)
		}
	}
	// Planted defect spans both redundant layers: the domain precheck is
	// bypassed and the substrate cap backstop is absent.
	if _, err := mutant.Exec(`DROP TRIGGER packets_wip_cap`); err != nil {
		t.Fatalf("drop planted cap trigger: %v", err)
	}
	_, mutantErr := mutant.Exec(`INSERT INTO review_packets (task_id) VALUES (?)`, mutantTasks[3])
	occupancy := packetOccupancy(t, mutant)
	return mutantOutcome{
		Hit:     true,
		Killed:  codeOf(mutantErr) != domain.CodeWIPCap || occupancy > 3,
		Witness: fmt.Sprintf("error=%v occupancy=%d", mutantErr, occupancy),
	}
}

func openPropertySpine(t *testing.T) *sql.DB {
	t.Helper()
	db, err := substrate.Open(filepath.Join(t.TempDir(), "spine.db"))
	if err != nil {
		t.Fatalf("open property spine: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := substrate.Init(db); err != nil {
		t.Fatalf("init property spine: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO worksources (id, title, kind) VALUES ('ws', 'Property', 'repo')`); err != nil {
		t.Fatalf("insert property worksource: %v", err)
	}
	return db
}

func inImmediate(db *sql.DB, fn func(context.Context, domain.Q) error) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	if err := fn(ctx, conn); err != nil {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		return err
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	return err
}

func insertedTask(t *testing.T, db *sql.DB, status string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO tasks (title, worksource) VALUES ('generated', 'ws')`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("task id: %v", err)
	}
	order := map[string][]string{
		"proposed": nil,
		"seeded":   {"seeded"},
		"worked":   {"seeded", "worked"},
		"verified": {"seeded", "worked", "verified"},
		"packaged": {"seeded", "worked", "verified", "packaged"},
	}
	path, ok := order[status]
	if !ok {
		t.Fatalf("unsupported fixture status %q", status)
	}
	for _, next := range path {
		if _, err := db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, next, id); err != nil {
			t.Fatalf("walk task %d to %s: %v", id, next, err)
		}
	}
	return id
}

func budgetFixture(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	db := openPropertySpine(t)
	id := insertedTask(t, db, "seeded")
	if _, err := db.Exec(`UPDATE tasks SET correction_count = 2, dispatch_retries = 2 WHERE id = ?`, id); err != nil {
		t.Fatalf("seed budgets: %v", err)
	}
	return db, id
}

func readCounters(t *testing.T, db *sql.DB, id int64) counters {
	t.Helper()
	var c counters
	if err := db.QueryRow(`
		SELECT correction_count, dispatch_retries FROM tasks WHERE id = ?`, id).
		Scan(&c.Correction, &c.Retries); err != nil {
		t.Fatalf("read counters: %v", err)
	}
	return c
}

func claimFixture(t *testing.T, db *sql.DB, subject int64, runID string) {
	t.Helper()
	err := inImmediate(db, func(ctx context.Context, q domain.Q) error {
		_, err := domain.Claim(ctx, q, propertyNow, domain.ClaimArgs{
			RunID: runID, Owner: "worker", SubjectID: &subject,
			SessionPath: "sessions/" + runID, Binding: "fake",
			HardDeadlineMinutes: 240,
		})
		return err
	})
	if err != nil {
		t.Fatalf("claim fixture: %v", err)
	}
}

func lockSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot string
	err := db.QueryRow(`
		SELECT COALESCE(run_id, '<null>') || '|' || COALESCE(worksource, '<null>') ||
		       '|' || COALESCE(subject, -1) || '|' || COALESCE(owner, '<null>') ||
		       '|' || COALESCE(acquired_at, '<null>') ||
		       '|' || COALESCE(last_heartbeat_at, '<null>') ||
		       '|' || COALESCE(hard_deadline_at, '<null>')
		FROM lock WHERE id = 1`).Scan(&snapshot)
	if err != nil {
		t.Fatalf("lock snapshot: %v", err)
	}
	return snapshot
}

func fourPackagedTasks(t *testing.T, db *sql.DB) []int64 {
	t.Helper()
	ids := make([]int64, 0, 4)
	for i := 0; i < 4; i++ {
		ids = append(ids, insertedTask(t, db, "packaged"))
	}
	return ids
}

func packetOccupancy(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_packets WHERE archived = 0`).Scan(&n); err != nil {
		t.Fatalf("packet occupancy: %v", err)
	}
	return n
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	var de *domain.DomainError
	if errors.As(err, &de) {
		return de.Code
	}
	return "non-domain"
}
