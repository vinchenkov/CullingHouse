package verbs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mc/dispatch"
)

var (
	dvOld    = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	dvFuture = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
)

func dvSpine(t *testing.T, initArgs ...func(*InitArgs)) *sql.DB {
	t.Helper()
	a := InitArgs{
		Spine:         filepath.Join(t.TempDir(), "spine.db"),
		Worksource:    "ws-test",
		WorkspaceRoot: "/tmp/ws-test",
	}
	for _, apply := range initArgs {
		apply(&a)
	}
	if _, err := Init(a); err != nil {
		t.Fatalf("init spine: %v", err)
	}
	t.Setenv("MC_HOME", filepath.Dir(a.Spine))
	if err := os.WriteFile(filepath.Join(filepath.Dir(a.Spine), "routing.md"), []byte(`# test routing
| role | harness | binding |
| --- | --- | --- |
| strategist | claude-sdk | claude |
| editor | codex | chatgpt |
| worker | claude-sdk | minimax |
| verifier | codex | chatgpt |
| packager | claude-sdk | minimax |
| refiner | codex | chatgpt |
| homie | claude-sdk | claude |
`), 0o600); err != nil {
		t.Fatalf("write routing.md: %v", err)
	}
	db, err := OpenSpine(a.Spine)
	if err != nil {
		t.Fatalf("open spine: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func dvExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func dvInsertTask(t *testing.T, db *sql.DB, task dispatch.Task) {
	t.Helper()
	dvExec(t, db, `
		INSERT INTO tasks
			(id, title, scope, priority, created_at, status,
			 dispatch_retries, origin, worksource, target_ref)
		VALUES (?, ?, ?, ?, ?, 'proposed', ?, 'user', ?, ?)`,
		task.ID, task.Title, string(task.Scope), task.Priority,
		task.CreatedAt.Format(spineTime), task.DispatchRetries, task.Worksource, task.TargetRef)
	var path []dispatch.Status
	switch task.Status {
	case dispatch.StatusProposed:
	case dispatch.StatusSeeded:
		path = []dispatch.Status{dispatch.StatusSeeded}
	case dispatch.StatusWorked:
		path = []dispatch.Status{dispatch.StatusSeeded, dispatch.StatusWorked}
	case dispatch.StatusVerified:
		path = []dispatch.Status{dispatch.StatusSeeded, dispatch.StatusWorked, dispatch.StatusVerified}
	case dispatch.StatusPackaged:
		path = []dispatch.Status{dispatch.StatusSeeded, dispatch.StatusWorked, dispatch.StatusVerified, dispatch.StatusPackaged}
	default:
		t.Fatalf("unsupported fixture status %q", task.Status)
	}
	for _, status := range path {
		dvExec(t, db, `UPDATE tasks SET status=? WHERE id=?`, string(status), task.ID)
	}
	if task.Branch != "" || task.VerifiedSHA != "" {
		dvExec(t, db, `UPDATE tasks SET branch=?, verified_sha=? WHERE id=?`,
			nullIfEmpty(task.Branch), nullIfEmpty(task.VerifiedSHA), task.ID)
	}
	if task.Blocked {
		dvExec(t, db, `UPDATE tasks SET blocked=1, blocked_reason='fixture block' WHERE id=?`, task.ID)
	}
	if task.Decision != "" {
		dvExec(t, db, `UPDATE tasks SET decision=?, decided_at=? WHERE id=?`,
			string(task.Decision), nullableTime(task.DecidedAt), task.ID)
	}
	if task.Archived {
		dvExec(t, db, `UPDATE tasks SET archived=1 WHERE id=?`, task.ID)
	}
}

func nullableTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.Format(spineTime)
}

func dvTask(id int64, scope dispatch.Scope, status dispatch.Status, priority int) dispatch.Task {
	return dispatch.Task{
		ID: id, Title: fmt.Sprintf("task-%d", id), Scope: scope,
		Priority: priority, CreatedAt: dvOld, Status: status,
		DispatchRetries: 3, Worksource: "ws-test", TargetRef: "main",
	}
}

func dvConfig(hour int) dispatch.Config {
	return dispatch.Config{
		ReviewWIPCap: 3, TimeoutMinutes: 60, GraceMinutes: 15,
		SpawnGraceS: 60, ConsoleHour: hour, ConsoleMinute: 0,
		ConsoleLoc: time.UTC,
	}
}

func dvNow(t *testing.T, db *sql.DB) time.Time {
	t.Helper()
	now, err := spineNow(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return now
}

func dvAssertEffect(t *testing.T, want dispatch.Action, gotAny any) map[string]any {
	t.Helper()
	got, ok := gotAny.(map[string]any)
	if !ok {
		t.Fatalf("effect type = %T, want map", gotAny)
	}
	if got["action"] != string(want.Kind) {
		t.Fatalf("effect action = %v, pure Decide = %q (effect %v)", got["action"], want.Kind, got)
	}
	switch want.Kind {
	case dispatch.KindIdle:
		if got["reason"] != string(want.Idle) {
			t.Fatalf("idle reason = %v, want %q", got["reason"], want.Idle)
		}
	case dispatch.KindSpawn:
		if got["role"] != string(want.Spawn.Role) {
			t.Fatalf("spawn role = %v, want %q", got["role"], want.Spawn.Role)
		}
		var subject any
		if want.Spawn.SubjectID != nil {
			subject = *want.Spawn.SubjectID
		}
		if got["subject_id"] != subject {
			t.Fatalf("spawn subject = %#v, want %#v", got["subject_id"], subject)
		}
		pool := want.Spawn.ProposedPool
		if pool == nil {
			pool = []int64{}
		}
		if !reflect.DeepEqual(got["pool_ids"], pool) {
			t.Fatalf("spawn pool = %#v, want %#v", got["pool_ids"], pool)
		}
	case dispatch.KindReap:
		if got["run_id"] != want.Reap.RunID || got["reason"] != string(want.Reap.Reason) || got["stop_container"] != want.Reap.StopContainer {
			t.Fatalf("reap effect = %v, want %+v", got, want.Reap)
		}
	case dispatch.KindLand:
		if got["task_id"] != want.Land.TaskID || got["branch"] != want.Land.Branch || got["verified_sha"] != want.Land.VerifiedSHA || got["target_ref"] != want.Land.TargetRef {
			t.Fatalf("land effect = %v, want %+v", got, want.Land)
		}
	case dispatch.KindReenter:
		if got["task_id"] != want.Reenter.TaskID {
			t.Fatalf("reenter effect = %v, want %+v", got, want.Reenter)
		}
	}
	return got
}

func dvDispatch(t *testing.T, db *sql.DB, rec dispatch.Records, lock dispatch.Lock, cfg dispatch.Config) (dispatch.Action, map[string]any) {
	t.Helper()
	want := dispatch.Decide(rec, lock, cfg, dispatch.Clock{Now: dvNow(t, db)})
	got, err := Dispatch(db)
	if err != nil {
		t.Fatalf("verbs.Dispatch: %v", err)
	}
	return want, dvAssertEffect(t, want, got)
}

// TestDispatchSQLDifferentialActions is the Phase 2 adapter differential:
// hand-built projections drive the frozen pure Decide oracle while equivalent
// rows drive the real SQL loader/action applier. Every Action kind must agree.
func TestDispatchSQLDifferentialActions(t *testing.T) {
	t.Run("idle_loads_nullable_last_heartbeat", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusSeeded, 2)
		dvInsertTask(t, db, task)
		dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('fresh-run', 'pipeline', 'worker', 'ws-test', 1)`)
		dvExec(t, db, `
			UPDATE lock SET run_id='fresh-run', worksource='ws-test', subject=1,
			owner='worker', acquired_at=?, last_heartbeat_at=NULL,
			hard_deadline_at=? WHERE id=1`, dvFuture.Format(spineTime), dvFuture.Add(time.Hour).Format(spineTime))
		lock := dispatch.Lock{Held: true, RunID: "fresh-run", Owner: "worker", SubjectID: ptr64(1), AcquiredAt: dvFuture, HardDeadlineAt: dvFuture.Add(time.Hour)}
		want, _ := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, lock, dvConfig(24))
		if want.Kind != dispatch.KindIdle || want.Idle != dispatch.IdleLeaseHeld {
			t.Fatalf("pure oracle = %+v", want)
		}
	})

	t.Run("spawn_editor_loads_null_task_fields_and_claims", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2)
		dvInsertTask(t, db, task)
		want, got := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, dispatch.Lock{}, dvConfig(24))
		if want.Kind != dispatch.KindSpawn || want.Spawn.Role != dispatch.RoleEditor {
			t.Fatalf("pure oracle = %+v", want)
		}
		runID := got["run_id"].(string)
		var lockRun string
		var subject int64
		if err := db.QueryRow(`SELECT run_id, subject FROM lock WHERE id=1`).Scan(&lockRun, &subject); err != nil {
			t.Fatal(err)
		}
		if lockRun != runID || subject != 1 {
			t.Fatalf("claim lock = %q/%d, effect run=%q", lockRun, subject, runID)
		}
		var binding, pool string
		if err := db.QueryRow(`SELECT binding, pool_snapshot FROM runs WHERE id=?`, runID).Scan(&binding, &pool); err != nil {
			t.Fatal(err)
		}
		if binding != "codex/chatgpt" || pool != "[1]" {
			t.Fatalf("run binding/pool = %q/%q", binding, pool)
		}
	})

	t.Run("reap_applies_watchdog_charge_and_releases", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusSeeded, 2)
		dvInsertTask(t, db, task)
		dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('dead-run', 'pipeline', 'worker', 'ws-test', 1)`)
		dvExec(t, db, `
			UPDATE lock SET run_id='dead-run', worksource='ws-test', subject=1,
			owner='worker', acquired_at=?, last_heartbeat_at=NULL,
			hard_deadline_at=? WHERE id=1`, dvOld.Format(spineTime), dvFuture.Format(spineTime))
		lock := dispatch.Lock{Held: true, RunID: "dead-run", Owner: "worker", SubjectID: ptr64(1), AcquiredAt: dvOld, HardDeadlineAt: dvFuture}
		want, _ := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, lock, dvConfig(24))
		if want.Kind != dispatch.KindReap || want.Reap.Reason != dispatch.ReapSpawnWatchdog {
			t.Fatalf("pure oracle = %+v", want)
		}
		var retries int
		var outcome string
		if err := db.QueryRow(`SELECT dispatch_retries FROM tasks WHERE id=1`).Scan(&retries); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT outcome FROM runs WHERE id='dead-run'`).Scan(&outcome); err != nil {
			t.Fatal(err)
		}
		if retries != 2 || outcome != "reaped" {
			t.Fatalf("reap writes retries=%d outcome=%q", retries, outcome)
		}
		var lockRun sql.NullString
		if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil || lockRun.Valid {
			t.Fatalf("lease not released: run=%v err=%v", lockRun, err)
		}
	})

	t.Run("land_is_effect_only", func(t *testing.T) {
		db := dvSpine(t)
		decided := dvOld.Add(time.Hour)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusPackaged, 0)
		task.Decision, task.DecidedAt = dispatch.DecisionApproved, &decided
		task.Branch, task.VerifiedSHA = "mc/task-1", "abc123"
		fixture := task
		fixture.Decision, fixture.DecidedAt = "", nil
		dvInsertTask(t, db, fixture)
		dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (1, ?)`, dvOld.Format(spineTime))
		dvExec(t, db, `UPDATE tasks SET decision='approved', decided_at=? WHERE id=1`, decided.Format(spineTime))
		packet := dispatch.Packet{TaskID: 1, CreatedAt: dvOld}
		want, _ := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}, Packets: []dispatch.Packet{packet}}, dispatch.Lock{}, dvConfig(24))
		if want.Kind != dispatch.KindLand {
			t.Fatalf("pure oracle = %+v", want)
		}
		var archived int
		if err := db.QueryRow(`SELECT archived FROM tasks WHERE id=1`).Scan(&archived); err != nil || archived != 0 {
			t.Fatalf("land effect mutated task: archived=%d err=%v", archived, err)
		}
	})

	t.Run("reenter_mutates_selected_initiative", func(t *testing.T) {
		db := dvSpine(t)
		rec := dispatch.Records{}
		for i := int64(1); i <= 3; i++ {
			task := dvTask(i, dispatch.ScopeInitiative, dispatch.StatusPackaged, int(i-1))
			dvInsertTask(t, db, task)
			dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (?, ?)`, i, dvOld.Add(time.Duration(i)*time.Minute).Format(spineTime))
			rec.Tasks = append(rec.Tasks, task)
			rec.Packets = append(rec.Packets, dispatch.Packet{TaskID: i, CreatedAt: dvOld.Add(time.Duration(i) * time.Minute)})
		}
		want, _ := dvDispatch(t, db, rec, dispatch.Lock{}, dvConfig(24))
		if want.Kind != dispatch.KindReenter || want.Reenter.TaskID != 1 {
			t.Fatalf("pure oracle = %+v", want)
		}
		var status string
		if err := db.QueryRow(`SELECT status FROM tasks WHERE id=1`).Scan(&status); err != nil || status != "seeded" {
			t.Fatalf("reenter status=%q err=%v", status, err)
		}
	})
}

func TestDispatchSQLDifferentialBriefingAndSubjectlessNulls(t *testing.T) {
	db := dvSpine(t, func(a *InitArgs) {
		a.ConsoleScheduleSet = true
		a.ConsoleHour, a.ConsoleMinute, a.ConsoleTZ = 0, 0, "UTC"
	})
	dvExec(t, db, `INSERT INTO activity (actor, kind, created_at) VALUES ('strategist', 'daily.briefing', datetime('now'))`)
	var briefingText string
	if err := db.QueryRow(`SELECT created_at FROM activity WHERE kind='daily.briefing'`).Scan(&briefingText); err != nil {
		t.Fatal(err)
	}
	briefing, err := parseSpineTime(briefingText)
	if err != nil {
		t.Fatal(err)
	}
	want, got := dvDispatch(t, db, dispatch.Records{LastBriefingAt: &briefing}, dispatch.Lock{}, dvConfig(0))
	if want.Kind != dispatch.KindSpawn || want.Spawn.Role != dispatch.RoleStrategistPropose || want.Spawn.SubjectID != nil {
		t.Fatalf("same-day briefing should suppress console and spawn subjectless propose: %+v", want)
	}
	runID := got["run_id"].(string)
	var subject sql.NullInt64
	var worksource sql.NullString
	if err := db.QueryRow(`SELECT subject, worksource FROM runs WHERE id=?`, runID).Scan(&subject, &worksource); err != nil {
		t.Fatal(err)
	}
	if subject.Valid || worksource.Valid || got["subject_id"] != nil || got["worksource"] != nil {
		t.Fatalf("subjectless NULLs lost: db subject=%v ws=%v effect=%v", subject, worksource, got)
	}
}

func TestDispatchSpawnBriefCarriesClaimedState(t *testing.T) {
	t.Run("editor_gets_full_proposal_records", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2)
		task.Title = "contrast this proposal"
		dvInsertTask(t, db, task)
		dvExec(t, db, `UPDATE tasks SET description='criterion: exact output' WHERE id=1`)
		_, got := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		for _, want := range []string{"contrast this proposal", "criterion: exact output", `"proposed_pool"`, "Orchestrate by default."} {
			if !strings.Contains(brief, want) {
				t.Fatalf("Editor brief missing %q: %s", want, brief)
			}
		}
	})

	t.Run("worker_gets_refine_notes_and_latest_correction", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusSeeded, 2)
		dvInsertTask(t, db, task)
		dvExec(t, db, `UPDATE tasks SET refine_notes='deepen the risk proof', correction_count=1 WHERE id=1`)
		dvExec(t, db, `INSERT INTO runs
			(id, tier, role, worksource, subject, verdict_outcome, evidence_path, correction_path)
			VALUES ('prior-verifier', 'pipeline', 'verifier', 'ws-test', 1, 'correct', 'evidence/e1.md', 'corrections/c1.md')`)
		_, got := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		for _, want := range []string{"deepen the risk proof", "corrections/c1.md", `"correction_count": 1`} {
			if !strings.Contains(brief, want) {
				t.Fatalf("Worker brief missing %q: %s", want, brief)
			}
		}
	})

	t.Run("verifier_gets_latest_worker_output", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusWorked, 2)
		dvInsertTask(t, db, task)
		dvExec(t, db, `INSERT INTO runs
			(id, tier, role, worksource, subject, output_path, ended_at, outcome)
			VALUES ('prior-worker', 'pipeline', 'worker', 'ws-test', 1,
			        'outputs/worker-report.md', datetime('now'), 'completed')`)
		_, got := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		if !strings.Contains(brief, `"latest_output_path": "outputs/worker-report.md"`) {
			t.Fatalf("Verifier brief lost Worker output: %s", brief)
		}
	})

	t.Run("packager_gets_budget_spent_exception_and_evidence", func(t *testing.T) {
		db := dvSpine(t)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusVerified, 2)
		dvInsertTask(t, db, task)
		dvExec(t, db, `INSERT INTO runs
			(id, tier, role, worksource, subject, verdict_outcome, evidence_path)
			VALUES ('budget-verifier', 'pipeline', 'verifier', 'ws-test', 1, 'budget-spent', 'evidence/unresolved.md')`)
		_, got := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		for _, want := range []string{`"exception_labeled": true`, "evidence/unresolved.md", "budget-spent"} {
			if !strings.Contains(brief, want) {
				t.Fatalf("Packager brief missing %q: %s", want, brief)
			}
		}
	})

	t.Run("strategist_gets_rejected_title_dedupe_memory", func(t *testing.T) {
		db := dvSpine(t)
		decided := dvOld.Add(time.Hour)
		task := dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2)
		task.Title = "do not pitch this again"
		task.Decision, task.DecidedAt, task.Archived = dispatch.DecisionRejected, &decided, true
		dvInsertTask(t, db, task)
		_, got := dvDispatch(t, db, dispatch.Records{Tasks: []dispatch.Task{task}}, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		if !strings.Contains(brief, "do not pitch this again") || !strings.Contains(brief, `"dedupe_titles"`) {
			t.Fatalf("Strategist brief lost dedupe memory: %s", brief)
		}
	})
}

func TestDispatchConsoleBriefCarriesQueueAndBlockedState(t *testing.T) {
	db := dvSpine(t, func(a *InitArgs) {
		a.ConsoleScheduleSet = true
		a.ConsoleHour, a.ConsoleMinute, a.ConsoleTZ = 0, 0, "UTC"
	})
	queued := dvTask(1, dispatch.ScopeTask, dispatch.StatusPackaged, 1)
	queued.Title = "queued decision"
	blockedTask := dvTask(2, dispatch.ScopeTask, dispatch.StatusSeeded, 0)
	blockedTask.Title, blockedTask.Blocked = "blocked decision", true
	dvInsertTask(t, db, queued)
	dvInsertTask(t, db, blockedTask)
	dvExec(t, db, `INSERT INTO review_packets (task_id, created_at) VALUES (1, ?)`, dvOld.Format(spineTime))

	_, got := dvDispatch(t, db, dispatch.Records{
		Tasks:   []dispatch.Task{queued, blockedTask},
		Packets: []dispatch.Packet{{TaskID: 1, CreatedAt: dvOld}},
	}, dispatch.Lock{}, dvConfig(0))
	if got["role"] != string(dispatch.RoleStrategistConsole) {
		t.Fatalf("role = %v, want Console", got["role"])
	}
	brief := fmt.Sprint(got["brief"])
	for _, want := range []string{`"review_queue"`, "queued decision", `"blocked_tasks"`, "blocked decision"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("Console brief missing %q: %s", want, brief)
		}
	}
}

func TestDispatchRejectsInvalidStoredTimezoneBeforeMutation(t *testing.T) {
	db := dvSpine(t)
	dvExec(t, db, `UPDATE lock SET console_tz='Mars/Olympus' WHERE id=1`)
	if _, err := Dispatch(db); err == nil {
		t.Fatal("invalid stored timezone dispatched instead of failing closed")
	}
	var runs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("invalid timezone mutated runs=%d err=%v", runs, err)
	}
}

func TestDispatchReapsStaleLeaseBeforeReadingInvalidConsoleTimezone(t *testing.T) {
	db := dvSpine(t)
	task := dvTask(1, dispatch.ScopeTask, dispatch.StatusSeeded, 2)
	dvInsertTask(t, db, task)
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('stale-run', 'pipeline', 'worker', 'ws-test', 1)`)
	dvExec(t, db, `
		UPDATE lock SET run_id='stale-run', worksource='ws-test', subject=1,
		owner='worker', acquired_at=?, last_heartbeat_at=NULL,
		hard_deadline_at=?, console_tz='Mars/Olympus' WHERE id=1`,
		dvOld.Format(spineTime), dvFuture.Format(spineTime))

	effect, err := Dispatch(db)
	if err != nil {
		t.Fatalf("stale lease was wedged behind invalid Console timezone: %v", err)
	}
	got := effect.(map[string]any)
	if got["action"] != "reap" || got["run_id"] != "stale-run" {
		t.Fatalf("effect = %v, want stale-run reap", got)
	}
	var retries int
	if err := db.QueryRow(`SELECT dispatch_retries FROM tasks WHERE id=1`).Scan(&retries); err != nil || retries != 2 {
		t.Fatalf("authoritative subject charge = %d err=%v", retries, err)
	}
	var lockRun sql.NullString
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil {
		t.Fatal(err)
	}
	if lockRun.Valid {
		t.Fatalf("stale lease still held by %q", lockRun.String)
	}
}

func ptr64(v int64) *int64 { return &v }

// ---------------------------------------------------------------------------
// ADR-020 D2(e): the SQL↔dispatch differential for the plan-review predicate.
// The pure layer is only as true as its projection: if loadRecords does not
// read plan_reviewed, every real child reads unreviewed and the whole wave
// lane inverts (children never dispatch; the Editor re-reviews forever).
// ---------------------------------------------------------------------------

func TestDispatchLoadRecordsProjectsPlanReviewed(t *testing.T) {
	db := dvSpine(t)

	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (1, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`,
		dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded' WHERE id=1`)
	for _, id := range []int64{2, 3} {
		dvExec(t, db, `INSERT INTO tasks (id, title, scope, status, initiative_id,
			priority, created_at, dispatch_retries, origin, worksource, target_ref)
			VALUES (?, 'child', 'task', 'seeded', 1, 1, ?, 3, 'autonomous', 'ws-test', 'main')`,
			id, dvOld.Format(spineTime))
	}
	// Child 2 passed the plan review; child 3 has not.
	dvExec(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = 2`)

	rec, err := loadRecords(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, task := range rec.Tasks {
		got[task.ID] = task.PlanReviewed
	}
	want := map[int64]bool{1: false, 2: true, 3: false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projected plan_reviewed = %v, want %v", got, want)
	}

	// The real projection must drive the real decision: an unreviewed child
	// keeps the wave under review, and the initiative — which §10 would park —
	// is the Editor's, over the whole open wave.
	a := dispatch.Decide(rec, dispatch.Lock{}, dvConfig(24), dispatch.Clock{Now: dvNow(t, db)})
	if a.Kind != dispatch.KindSpawn || a.Spawn.Role != dispatch.RoleEditorPlanReview {
		t.Fatalf("action = %+v, want the plan review from real spine rows", a.Spawn)
	}
	if !reflect.DeepEqual(a.Spawn.Wave, []int64{2, 3}) {
		t.Fatalf("wave = %v, want both open children [2 3]", a.Spawn.Wave)
	}

	// Pass the rest of the wave: the initiative parks and the children work.
	dvExec(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = 3`)
	rec, err = loadRecords(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	a = dispatch.Decide(rec, dispatch.Lock{}, dvConfig(24), dispatch.Clock{Now: dvNow(t, db)})
	if a.Spawn.Role != dispatch.RoleWorker || *a.Spawn.SubjectID != 2 {
		t.Fatalf("action = %+v, want worker on child 2 once the wave passed", a.Spawn)
	}
}

// ---------------------------------------------------------------------------
// ADR-020 D4: the plan-review brief. The charter and the wave, and — the one
// finding the ADR's adversarial review rated major — NOT the producer's own
// completion report.
// ---------------------------------------------------------------------------

// dvUnreviewedWave builds a seeded initiative with two open, unreviewed
// children and returns the records that select the plan review.
func dvUnreviewedWave(t *testing.T, db *sql.DB) {
	t.Helper()
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (1, 'ship the arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`,
		dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded',
		description='charter: the arc is done when the gate holds' WHERE id=1`)
	for id, title := range map[int64]string{2: "first child", 3: "second child"} {
		dvExec(t, db, `INSERT INTO tasks (id, title, description, scope, status, initiative_id,
			priority, created_at, dispatch_retries, origin, worksource, target_ref)
			VALUES (?, ?, 'criterion: the command exits 0', 'task', 'seeded', 1, 1, ?, 3,
			        'autonomous', 'ws-test', 'main')`, id, title, dvOld.Format(spineTime))
	}
}

func TestPlanReviewBrief(t *testing.T) {
	t.Run("carries_the_charter_and_the_whole_wave", func(t *testing.T) {
		db := dvSpine(t)
		dvUnreviewedWave(t, db)
		rec, err := loadRecords(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		_, got := dvDispatch(t, db, rec, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		for _, want := range []string{
			"charter: the arc is done when the gate holds", // the subject = the charter
			`"wave"`, "first child", "second child",
			"criterion: the command exits 0",
			"Orchestrate by default.",
		} {
			if !strings.Contains(brief, want) {
				t.Fatalf("plan-review brief missing %q: %s", want, brief)
			}
		}
		// The wave is not a contest: no proposal pool rides along.
		if strings.Contains(brief, `"proposed_pool"`) {
			t.Fatalf("plan-review brief carries a proposed pool: %s", brief)
		}
	})

	// The major finding. An initiative that round-tripped (§6.1's ordinary
	// packaged → seeded, or the correction rally) carries a mandatory
	// completion report on a run whose subject IS the initiative — the
	// producer's own authored prose. buildSpawnBrief loads latest_output_path
	// for every subject role, so without D4's suppression the judge reads the
	// producer's reasoning and Inv. 9's decorrelation is defeated.
	//
	// A virgin initiative passes this vacuously, so the fixture constructs the
	// round-trip: a property that holds only until the first one is not a
	// property.
	t.Run("never_carries_the_producers_own_completion_report", func(t *testing.T) {
		db := dvSpine(t)
		dvUnreviewedWave(t, db)
		dvExec(t, db, `INSERT INTO runs
			(id, tier, role, worksource, subject, output_path, ended_at, outcome)
			VALUES ('prior-strategist', 'pipeline', 'strategist', 'ws-test', 1,
			        'outputs/strategist-initiative-report.md', datetime('now'), 'completed')`)
		rec, err := loadRecords(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		_, got := dvDispatch(t, db, rec, dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		if strings.Contains(brief, "latest_output_path") ||
			strings.Contains(brief, "outputs/strategist-initiative-report.md") {
			t.Fatalf("Inv. 9 leak: the plan review reads its own producer's report: %s", brief)
		}
		// The suppression is surgical — the charter and wave still arrive.
		if !strings.Contains(brief, "charter: the arc is done when the gate holds") {
			t.Fatalf("suppression took the charter with it: %s", brief)
		}
	})
}

// ADR-020 D4's recency rule: the Strategist(initiative) brief carries the
// latest UNANSWERED objection. activity is append-only and an initiative has
// many wave boundaries, so an unqualified "latest wave.sent_back" would
// re-serve a long-answered objection at every future boundary forever. A
// send-back is answered exactly when a later wave passes.
func TestStrategistInitiativeBriefSendbackRecency(t *testing.T) {
	drained := func(t *testing.T, db *sql.DB) dispatch.Records {
		t.Helper()
		rec, err := loadRecords(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	newInitiative := func(t *testing.T, db *sql.DB) {
		t.Helper()
		dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
			dispatch_retries, origin, worksource, target_ref)
			VALUES (1, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`,
			dvOld.Format(spineTime))
		dvExec(t, db, `UPDATE tasks SET status='seeded', description='charter' WHERE id=1`)
	}

	t.Run("unanswered_objection_rides_the_replan", func(t *testing.T) {
		db := dvSpine(t)
		newInitiative(t, db)
		dvExec(t, db, `INSERT INTO activity (actor, kind, subject, detail, created_at)
			VALUES ('editor', 'wave.sent_back', 1, 'child 2 criterion cannot fail', datetime('now'))`)
		_, got := dvDispatch(t, db, drained(t, db), dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		if !strings.Contains(brief, "child 2 criterion cannot fail") {
			t.Fatalf("replan is blind to the objection it must answer: %s", brief)
		}
	})

	t.Run("answered_objection_does_not_ride_the_next_boundary", func(t *testing.T) {
		db := dvSpine(t)
		newInitiative(t, db)
		dvExec(t, db, `INSERT INTO activity (actor, kind, subject, detail, created_at)
			VALUES ('editor', 'wave.sent_back', 1, 'the stale objection', datetime('now', '-2 hours'))`)
		// The replanned wave passed: the objection is answered, and the wave
		// it produced has since drained. This boundary is owed nothing.
		dvExec(t, db, `INSERT INTO activity (actor, kind, subject, detail, created_at)
			VALUES ('editor', 'wave.passed', 1, '2,3', datetime('now', '-1 hours'))`)
		_, got := dvDispatch(t, db, drained(t, db), dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		if strings.Contains(brief, "the stale objection") {
			t.Fatalf("an answered objection rode a later wave boundary: %s", brief)
		}
	})

	t.Run("a_later_sendback_supersedes_an_earlier_pass", func(t *testing.T) {
		db := dvSpine(t)
		newInitiative(t, db)
		dvExec(t, db, `INSERT INTO activity (actor, kind, subject, detail, created_at)
			VALUES ('editor', 'wave.passed', 1, '2,3', datetime('now', '-2 hours'))`)
		dvExec(t, db, `INSERT INTO activity (actor, kind, subject, detail, created_at)
			VALUES ('editor', 'wave.sent_back', 1, 'the live objection', datetime('now', '-1 hours'))`)
		_, got := dvDispatch(t, db, drained(t, db), dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		if !strings.Contains(brief, "the live objection") {
			t.Fatalf("the current round's objection was masked by an older pass: %s", brief)
		}
	})

	t.Run("sendback_does_not_clobber_refine_notes", func(t *testing.T) {
		db := dvSpine(t)
		newInitiative(t, db)
		// An initiative under §8 refinement whose replanned wave was sent back
		// needs BOTH carriers: the operator's revision notes and the Editor's
		// objection. refine_notes is single-slot and owned by the §7/§8 path.
		dvExec(t, db, `UPDATE tasks SET refine_notes='operator: deepen the risk proof' WHERE id=1`)
		dvExec(t, db, `INSERT INTO activity (actor, kind, subject, detail, created_at)
			VALUES ('editor', 'wave.sent_back', 1, 'editor: child 2 is not actionable', datetime('now'))`)
		_, got := dvDispatch(t, db, drained(t, db), dispatch.Lock{}, dvConfig(24))
		brief := fmt.Sprint(got["brief"])
		for _, want := range []string{"operator: deepen the risk proof", "editor: child 2 is not actionable"} {
			if !strings.Contains(brief, want) {
				t.Fatalf("brief lost a carrier (%q): %s", want, brief)
			}
		}
	})
}
