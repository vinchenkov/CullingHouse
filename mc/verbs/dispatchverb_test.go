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
		for _, want := range []string{"contrast this proposal", "criterion: exact output", `"proposed_pool"`} {
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
