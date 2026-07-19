//go:build docker_e2e

// Package e2e is the Phase 1b walking-skeleton end-to-end test
// (docs/phase1b-contract.md §7): one origin:user task traverses
//
//	tick → dispatch → lease → fake-harness Worker → mc complete → …
//	→ packet → approve → land
//
// through the REAL mc binary (self-delegating into the warm helper — the
// spine never leaves the lock domain, §11.5/Inv. 24), the REAL resident
// process on a REAL interval timer, and real containers. The test invokes
// ONLY operator/host verbs (init, task add, packet decide, and the reads);
// it NEVER calls `mc dispatch` — every stage advance below therefore proves
// timer-driven advancement (§10). Docker-gated by the docker_e2e build tag:
// the fast suite never compiles this file (contract §1).
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"mc/verbs"
)

const (
	spineDBPath = "/mc/spine/spine.db" // in the lock domain (named volume at /mc/spine)
	image       = "mc-fake-e2e"
	worksource  = "ws-e2e"
)

// fixture owns every external resource; all cleanup is registered on t.
type fixture struct {
	t       *testing.T
	base    string // temp base under /private/tmp (Docker Desktop file sharing)
	hostMC  string // darwin mc binary
	env     []string
	helper  string
	volume  string
	home    string // MC_HOME
	ws      string // workspace git repo (the Worksource)
	resLog  *os.File
	resProc *exec.Cmd
}

func TestWalkingSkeleton(t *testing.T) {
	f := setup(t)

	// ── Ladder 1: file the one origin:user task; spine starts clean ──────
	res := f.mcOK("", "task", "add", "skeleton task", "--worksource", worksource,
		"--description", "the walking skeleton's single task")
	taskID := int64(res["task_id"].(float64))
	task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if task["status"] != "proposed" || task["origin"] != "user" {
		t.Fatalf("fresh task = status %v origin %v, want proposed/user", task["status"], task["origin"])
	}
	lock := f.mcOK("", "lock", "get")
	if lock["run_id"] != nil {
		t.Fatalf("lock must start free, got %v", lock)
	}
	branch := fmt.Sprintf("mc/task-%d", taskID)
	worktreeDir := filepath.Join(f.ws, ".mc-worktrees", fmt.Sprintf("task-%d", taskID))
	mainBefore := f.git("rev-parse", "main")

	// ── Ladder 2: start the resident; from here the timer drives ─────────
	f.startResident()

	// ── Ladder 3: tick → Editor spawn (claim CAS + runs row, Inv. 4) ─────
	editorRun := f.waitForOwner("editor", 30*time.Second)
	f.waitFor(15*time.Second, "editor session native.jsonl appears (Inv. 26)", func() (bool, string) {
		p := filepath.Join(f.home, "sessions", editorRun, "native.jsonl")
		if _, err := os.Stat(p); err != nil {
			return false, p + " missing"
		}
		return true, ""
	})
	runRow := f.runRow(editorRun)
	if runRow == nil || runRow["role"] != "editor" {
		t.Fatalf("runs row for editor claim missing/wrong: %v (Inv. 4)", runRow)
	}

	// ── Ladder 4: fake Editor promotes its exact pool ─────────────────────
	f.waitForTaskStatus(taskID, "seeded", 30*time.Second)
	f.waitFor(10*time.Second, "editor lease released + runs row ended", func() (bool, string) {
		lock := f.mcOK("", "lock", "get")
		if lock["run_id"] != nil && lock["run_id"] == editorRun {
			return false, "editor still holds the lease"
		}
		row := f.runRow(editorRun)
		if row["ended_at"] == nil {
			return false, "runs.ended_at not stamped"
		}
		return true, ""
	})

	// ── Ladder 5: tick → Worker; heartbeats advance while it works ───────
	workerRun := f.waitForOwner("worker", 30*time.Second)
	var hb1 string
	f.waitFor(15*time.Second, "first worker heartbeat (runner-side, §10)", func() (bool, string) {
		lock := f.mcOK("", "lock", "get")
		if lock["run_id"] != workerRun {
			return false, fmt.Sprintf("lease moved: %v", lock["run_id"])
		}
		if lock["last_heartbeat_at"] == nil {
			return false, "last_heartbeat_at still NULL"
		}
		hb1 = lock["last_heartbeat_at"].(string)
		return true, ""
	})
	f.waitFor(15*time.Second, "heartbeat ADVANCES during the worker run (§11.6)", func() (bool, string) {
		lock := f.mcOK("", "lock", "get")
		if lock["run_id"] != workerRun {
			return false, "lease moved before a second heartbeat was observed"
		}
		hb, _ := lock["last_heartbeat_at"].(string)
		if hb == hb1 || hb == "" {
			return false, "still " + hb1
		}
		return true, ""
	})

	f.waitForTaskStatus(taskID, "worked", 30*time.Second)
	task = f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if task["branch"] != branch {
		t.Fatalf("tasks.branch = %v, want %s (contract A2)", task["branch"], branch)
	}
	// The commit is visible on the branch from the HOST (§6.2 relative
	// worktree links; read-only host git is sanctioned).
	workedSHA := f.git("rev-parse", "refs/heads/"+branch)
	if workedSHA == mainBefore {
		t.Fatalf("worker branch has no new commit (still %s)", mainBefore)
	}
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worker worktree %s missing: %v (contract A3)", worktreeDir, err)
	}
	workerRow := f.runRow(workerRun)
	if workerRow["native_session_ref"] != "fake-session" || workerRow["trace_filename"] != "native.jsonl" {
		t.Fatalf("register-session locators wrong on runs row: %v (ADR-001 D5)", workerRow)
	}

	// ── Ladder 6: tick → Verifier records the exact reviewed SHA ─────────
	f.waitForTaskStatus(taskID, "verified", 30*time.Second)
	task = f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if task["verified_sha"] != workedSHA {
		t.Fatalf("verified_sha = %v, want %s (§7: only the exact reviewed commit lands)", task["verified_sha"], workedSHA)
	}

	// ── Ladder 7: tick → Packager; packet born in the same transaction ───
	f.waitForTaskStatus(taskID, "packaged", 30*time.Second)
	packets := f.packets()
	if len(packets) != 1 {
		t.Fatalf("packet count = %d, want 1 (Inv. 10/11)", len(packets))
	}
	if int64(packets[0]["task_id"].(float64)) != taskID || packets[0]["archived"].(float64) != 0 {
		t.Fatalf("packet = %v, want unarchived packet for task %d", packets[0], taskID)
	}

	// ── Ladder 8: board drained → Strategist(propose) ticks are survived ─
	f.waitFor(30*time.Second, "a Strategist(propose) run spawns and terminates via empty batch", func() (bool, string) {
		for _, r := range f.runs() {
			if r["role"] == "strategist" && r["ended_at"] != nil && r["outcome"] == "completed" {
				return true, ""
			}
		}
		return false, "no completed strategist run yet"
	})
	if got := f.mcRun("", "task", "get", fmt.Sprint(taskID+1)); got.code != 1 {
		t.Fatalf("a task %d appeared (exit %d): empty strategist batches must add nothing", taskID+1, got.code)
	}

	// ── Ladder 9: APPROVE — the split's first half: a pure state write ───
	if got := f.mcRun("", "packet", "decide", fmt.Sprint(taskID), "--approve"); got.code != 0 {
		t.Fatalf("mc packet decide --approve exited %d: %s", got.code, got.stderr)
	}
	// The split is asserted FIRST and host-locally (~ms read): a collapsed
	// split would move main synchronously, before decide returned. The
	// spine reads that follow are docker-exec round trips racing the live
	// 500 ms tick loop — the land effect may legitimately complete under
	// them, so they tolerate an already-landed row; ladder 10 re-verifies
	// every landed invariant deterministically.
	if now := f.git("rev-parse", "main"); now != mainBefore {
		t.Fatalf("host main moved at approve time (%s → %s): approve must be a pure state write (Inv. 2)", mainBefore, now)
	}
	task = f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if task["decision"] != "approved" {
		t.Fatalf("after approve: decision = %v, want approved (§7)", task["decision"])
	}
	if task["archived"].(float64) == 0 && task["status"] != "packaged" {
		t.Fatalf("after approve: %v, want status=packaged while unarchived (§7, Inv. 2)", task)
	}
	if p := f.packets(); p[0]["archived"].(float64) != 0 {
		// Only legitimate if the timer already landed the task between the
		// two reads; a packet archived ahead of its task is the collapse.
		if tk := f.mcOK("", "task", "get", fmt.Sprint(taskID)); tk["archived"].(float64) != 1 {
			t.Fatalf("packet archived while task unarchived — the split collapsed (§7)")
		}
	}

	// ── Ladder 10: tick → land effect → mc-land → mc land report ─────────
	f.waitFor(60*time.Second, "task archived after landing", func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
		if task["archived"].(float64) != 1 {
			return false, fmt.Sprintf("archived=%v blocked=%v (%v)", task["archived"], task["blocked"], task["blocked_reason"])
		}
		return true, ""
	})
	if p := f.packets(); p[0]["archived"].(float64) != 1 {
		t.Fatalf("packet not archived with the task (cascade trigger)")
	}
	mainAfter := f.git("rev-parse", "main")
	if mainAfter == mainBefore {
		t.Fatalf("main did not advance after landing")
	}
	if p1 := f.git("rev-parse", "main^1"); p1 != mainBefore {
		t.Fatalf("merge first parent = %s, want prior main %s (--no-ff)", p1, mainBefore)
	}
	if p2 := f.git("rev-parse", "main^2"); p2 != workedSHA {
		t.Fatalf("merge second parent = %s, want verified_sha %s (§7)", p2, workedSHA)
	}
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Fatalf("worktree %s still present after landing (§7 step 3)", worktreeDir)
	}
	if out, err := f.gitErr("rev-parse", "--verify", "refs/heads/"+branch); err == nil {
		t.Fatalf("branch %s still exists after landing: %s (§7 step 3)", branch, out)
	}
	f.waitFor(10*time.Second, "lock free at rest", func() (bool, string) {
		if lock := f.mcOK("", "lock", "get"); lock["run_id"] != nil {
			return false, fmt.Sprintf("held by %v", lock["owner"])
		}
		return true, ""
	})

	// Every session folder holds NOTHING but the trace (Inv. 26, spec §4):
	// no run.json alias, no role outputs, no scratch — ever.
	sessions, err := os.ReadDir(filepath.Join(f.home, "sessions"))
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	for _, s := range sessions {
		entries, err := os.ReadDir(filepath.Join(f.home, "sessions", s.Name()))
		if err != nil {
			t.Fatalf("read session %s: %v", s.Name(), err)
		}
		for _, e := range entries {
			if e.Name() != "native.jsonl" {
				t.Fatalf("session %s holds %q — the folder holds nothing but the trace (Inv. 26)", s.Name(), e.Name())
			}
		}
	}
}

// TestVerifierProjectionDockerBoundary proves the D6 disposable-source setup
// against the shipped Linux image, rather than only asserting the resident's
// argv in its fake-effects tests. The existing walking skeleton deliberately
// keeps its legacy fake mount arm, so this probe creates the exact sealed
// task-local input directly and exercises the production setup command.
func TestVerifierProjectionDockerBoundary(t *testing.T) {
	f := setup(t)
	const (
		taskID  = int64(42)
		worker  = "worker-42"
		request = "0011223344556677"
		verify  = "verify-42"
	)
	taskRoot := filepath.Join(f.base, "task-42")
	for _, child := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(taskRoot, child), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := verbs.MaterializeFirstTaskStore(f.ws, taskRoot, verbs.FirstTaskSetupSpec{
		TaskID: taskID, Mode: "fresh", TargetRef: "main", ObjectFormat: "sha1",
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	})
	if err != nil {
		t.Fatalf("materialize canonical task store: %v", err)
	}
	sealDir := filepath.Join(f.home, "seals", worker)
	if err := os.MkdirAll(filepath.Dir(sealDir), 0o700); err != nil {
		t.Fatal(err)
	}
	seal, err := verbs.SealTaskCompletion(taskRoot, sealDir, worker, request, taskID)
	if err != nil {
		t.Fatalf("seal canonical task store: %v", err)
	}
	projection := filepath.Join(f.home, "runs", "projections", verify)
	if err := os.MkdirAll(projection, 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := verbs.SetupEnvelope{
		SchemaVersion: 1, Operation: verbs.SetupOperationVerifierProjection,
		RunID: verify, TaskID: taskID, ObjectFormat: seal.ObjectFormat,
		CompletionRequest: seal.CompletionRequest, SealedSHA: seal.SealedSHA,
		ClosureDigest: seal.ClosureDigest, ManifestDigest: seal.ManifestDigest,
		SealDevice: seal.Device, SealInode: seal.Inode, SealOwnerUID: seal.OwnerUID,
		TaskRoot: "/repo/task", ProjectionRoot: "/repo/projection",
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelopePath := filepath.Join(f.home, verify+".projection.json")
	if err := os.WriteFile(envelopePath, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	f.docker("run", "--rm", "--name", "mc-setup-"+verify, "--network", "none",
		"--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", "mc-run-id="+verify,
		"--user", "10002:10002", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
		"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
		"-v", taskRoot+":/repo/task:ro", "-v", projection+":/repo/projection",
		"-v", envelopePath+":/mc/setup.json:ro", image, "mc", "__setup-verifier-projection", "/mc/setup.json")

	if got, err := os.ReadFile(filepath.Join(projection, "README.md")); err != nil || string(got) != "walking skeleton worksource\n" {
		t.Fatalf("projection README = (%q, %v), want sealed source bytes", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(projection, ".git")); err != nil || string(got) != "gitdir: ../git/worktrees/mc-task-42\n" {
		t.Fatalf("projection .git pointer = (%q, %v), want fixed relative task control", got, err)
	}
	spineDir := filepath.Join(f.base, "verdict-spine")
	if err := os.MkdirAll(spineDir, 0o777); err != nil {
		t.Fatal(err)
	}
	spine := filepath.Join(spineDir, "spine.db")
	if _, err := verbs.Init(verbs.InitArgs{Spine: spine, Worksource: "ws-verdict", WorkspaceRoot: f.ws}); err != nil {
		t.Fatalf("init verdict spine: %v", err)
	}
	db, err := verbs.OpenSpine(spine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id,title,scope,priority,created_at,status,dispatch_retries,origin,worksource,target_ref)
		VALUES (42,'verdict fixture','task',2,datetime('now'),'proposed',3,'user','ws-verdict','main')`); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"seeded", "worked"} {
		if _, err := db.Exec(`UPDATE tasks SET status=? WHERE id=42`, status); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject,ended_at,outcome)
		VALUES (?, 'pipeline', 'worker', 'ws-verdict', 42, datetime('now'), 'completed')`, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO completion_seals
		(run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid,state,accepted_at)
		VALUES (?,42,?,?,?,?,?,?,?,?,'accepted',datetime('now'))`, worker, request, seal.ObjectFormat, seal.SealedSHA, seal.ClosureDigest, seal.ManifestDigest, seal.Device, seal.Inode, seal.OwnerUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET accepted_completion_run_id=?, accepted_completion_request_id=? WHERE id=42`, worker, request); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'verifier', 'ws-verdict', 42)`, verify); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-verdict', subject=42, owner='verifier', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, verify); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	runJSON := filepath.Join(f.base, "verify-42.run.json")
	if err := os.WriteFile(runJSON, []byte("{\"run_id\":\"verify-42\",\"tier\":\"pipeline\",\"role\":\"verifier\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := "mc-run-" + verify
	f.docker("create", "--rm", "--name", agent, "--network", "none", "--user", "10002:10002",
		"-v", taskRoot+":/workspace:ro", "-v", projection+":/workspace/source",
		"-v", filepath.Join(taskRoot, "source", ".git")+":/workspace/source/.git:ro",
		"-v", filepath.Join(taskRoot, "source", ".mission-control")+":/workspace/source/.mission-control:ro",
		"-v", spineDir+":/mc/spine", "-v", runJSON+":/mc/run.json:ro", "-e", "MC_SPINE=/mc/spine/spine.db",
		image, "sh", "-ec", "printf drift > /workspace/source/README.md; mc verifier verdict 42 --run verify-42 --outcome pass --evidence d6-evidence --sha "+seal.SealedSHA)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", agent).Run() })
	mounts := f.dockerOutput("inspect", "--format", "{{range .Mounts}}{{.Source}}|{{.Destination}}|{{.RW}}\\n{{end}}", agent)
	for _, want := range []string{
		taskRoot + "|/workspace|false",
		projection + "|/workspace/source|true",
		filepath.Join(taskRoot, "source", ".git") + "|/workspace/source/.git|false",
		filepath.Join(taskRoot, "source", ".mission-control") + "|/workspace/source/.mission-control|false",
	} {
		if !strings.Contains(mounts, want) {
			t.Fatalf("agent bind inspection missing %q:\n%s", want, mounts)
		}
	}
	output, err := exec.Command("docker", "start", "-a", agent).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "tracked-tree drift") {
		t.Fatalf("dirty disposable verdict = (%v, %q), want tracked-tree fence refusal", err, output)
	}
}

// TestDeploymentMirrorDockerBoundary covers D1's real resident/helper seam.
// The unit frame tests own the prepare→attest swap race; this tagged probe
// proves the host file is actually visible in the helper, a foreign value
// prevents a resident-driven claim, and recovery requires the exact mirror.
func TestDeploymentMirrorDockerBoundary(t *testing.T) {
	f := setup(t)
	mirror := filepath.Join(f.home, "deployment.uuid")
	good, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.dockerOutput("exec", f.helper, "cat", "/mc/home/deployment.uuid"); got != string(good) {
		t.Fatalf("helper deployment mirror = %q, want host bind bytes %q", got, good)
	}
	if err := os.WriteFile(mirror, []byte("foreign-deployment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.startResident()
	res := f.mcOK("", "task", "add", "mirror boundary task", "--worksource", worksource,
		"--description", "must not claim under a foreign deployment mirror")
	taskID := int64(res["task_id"].(float64))
	f.waitFor(10*time.Second, "helper prepare rejection for a foreign deployment mirror", func() (bool, string) {
		body, err := os.ReadFile(filepath.Join(f.base, "resident.log"))
		if err != nil || !strings.Contains(string(body), "private helper __dispatch-prepare failed") {
			return false, "mirror mismatch not yet logged"
		}
		if len(f.runs()) != 0 {
			return false, "foreign mirror created a Run"
		}
		lock := f.mcOK("", "lock", "get")
		if lock["run_id"] != nil {
			return false, fmt.Sprintf("foreign mirror held lock %v", lock["run_id"])
		}
		return true, ""
	})
	if task := f.mcOK("", "task", "get", fmt.Sprint(taskID)); task["status"] != "proposed" || task["blocked"].(float64) != 0 {
		t.Fatalf("foreign mirror mutated task state: %v", task)
	}
	if err := os.WriteFile(mirror, good, 0o600); err != nil {
		t.Fatal(err)
	}
	f.waitFor(15*time.Second, "dispatch recovery after restoring the exact deployment mirror", func() (bool, string) {
		for _, run := range f.runs() {
			if run["role"] == "editor" {
				return true, ""
			}
		}
		return false, "no editor Run after mirror recovery"
	})
}

// TestFirstTaskSetupDockerBoundary runs the exact D5 setup container against
// a resident-shaped empty skeleton, then records its emitted result only
// through the live Worker receipt and task-root re-attestation gates.
func TestFirstTaskSetupDockerBoundary(t *testing.T) {
	f := setup(t)
	const (
		taskID = int64(7)
		runID  = "setup-run"
	)
	taskRoot := filepath.Join(f.ws, ".mission-control", "tasks", "task-7")
	for _, child := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(taskRoot, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(taskRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	setupCover := filepath.Join(f.home, runID+".setup-cover")
	if err := os.MkdirAll(setupCover, 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := verbs.SetupEnvelope{
		SchemaVersion: 1, Operation: verbs.SetupOperationFirstTaskClosure,
		RunID: runID, TaskID: taskID, Mode: "fresh", ObjectFormat: "sha1",
		TargetRef: "main", Branch: "mc/task-7", WorktreeName: "mc-task-7",
		SourceRepo: "/repo/source", TaskRoot: "/repo/task",
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelopePath := filepath.Join(f.home, runID+".setup.json")
	if err := os.WriteFile(envelopePath, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	output := f.dockerOutput("run", "--rm", "--name", "mc-setup-"+runID, "--network", "none",
		"--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", "mc-run-id="+runID,
		"--user", "10002:10002", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
		"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
		"-v", f.ws+":/repo/source:ro", "-v", setupCover+":/repo/source/.mission-control:ro",
		"-v", taskRoot+":/repo/task:ro", "-v", filepath.Join(taskRoot, "source")+":/repo/task/source",
		"-v", filepath.Join(taskRoot, "git")+":/repo/task/git", "-v", envelopePath+":/mc/setup.json:ro",
		image, "mc", "__setup-first-task", "/mc/setup.json")
	var result verbs.SetupResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("setup result %q: %v", output, err)
	}
	if !result.FsckClean || result.ObjectFormat != "sha1" || result.BaseSHA != f.git("rev-parse", "main") {
		t.Fatalf("setup result = %+v, want clean pinned main store", result)
	}

	spine := filepath.Join(f.base, "d5-spine.db")
	if _, err := verbs.Init(verbs.InitArgs{Spine: spine, Worksource: "ws-d5", WorkspaceRoot: f.ws}); err != nil {
		t.Fatalf("init D5 spine: %v", err)
	}
	db, err := verbs.OpenSpine(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO tasks (id,title,scope,priority,created_at,status,dispatch_retries,origin,worksource,target_ref)
		VALUES (7,'D5 setup fixture','task',2,datetime('now'),'proposed',3,'user','ws-d5','main')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', 'ws-d5', 7)`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-d5', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, runID); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(taskRoot)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("task root lacks native filesystem identity")
	}
	if _, err := verbs.RegisterFirstTaskSetup(db, verbs.TaskSetupReceipt{RunID: runID, TaskID: taskID,
		Root: verbs.TaskSetupIdentity{Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(uint64(st.Ino), 10), OwnerUID: int(st.Uid)}}); err != nil {
		t.Fatalf("register resident-style skeleton: %v", err)
	}
	recorded, rows, err := verbs.RecordFirstTaskSetupClosure(db, runID, f.ws, result)
	if err != nil {
		t.Fatalf("record setup result: %v", err)
	}
	if recorded.Canonical != taskRoot || len(rows) != 15 {
		t.Fatalf("recorded D5 store = (%+v, %d rows), want canonical task root and 15 typed rows", recorded, len(rows))
	}
	if assignment, err := verbs.ReadFirstTaskAssignment(db, runID); err != nil || assignment.BaseSHA != result.BaseSHA || assignment.ClosureDigest != result.ClosureDigest {
		t.Fatalf("recorded assignment = (%+v, %v), want setup result identity", assignment, err)
	}
}

// ───────────────────────────── fixtures ─────────────────────────────────

func setup(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker CLI not found: %v", err)
	}

	// /private/tmp is Docker-Desktop-shared by default; t.TempDir() lands in
	// /var/folders which may not be. MC_HOME must be a scratch path, never
	// ~/.mission-control (AGENTS.md env facts).
	base, err := os.MkdirTemp("/private/tmp", "mc-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })

	f := &fixture{t: t, base: base}
	sfx := strings.ToLower(filepath.Base(base)[len("mc-e2e-"):])
	f.volume = "mc-e2e-spine-" + sfx
	f.helper = "mc-e2e-helper-" + sfx
	f.home = filepath.Join(base, "home")
	f.ws = filepath.Join(base, "ws")
	for _, d := range []string{f.home, f.ws, filepath.Join(base, "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Host mount attest trusts MC_HOME the way it trusts the allowlist:
	// operator-only, no group/other bits (boundary.TrustHomeDir).
	if err := os.Chmod(f.home, 0o700); err != nil {
		t.Fatal(err)
	}
	// The test-fake workspace bind rides the plan carrier: the allowlist
	// authorizes the host worksource root to exactly /workspace/source RW,
	// and the resident consumes only the attested plan.
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"source\"\naccess = \"rw\"\n", f.ws)
	if err := os.WriteFile(filepath.Join(f.home, "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.home, "routing.md"), []byte(`# fake E2E routing
| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | fake | fake |
| verifier | fake | fake |
| packager | fake | fake |
| refiner | fake | fake |
| homie | fake | fake |
`), 0o600); err != nil {
		t.Fatal(err)
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	// Host (darwin) mc binary for the test and the resident.
	f.hostMC = filepath.Join(base, "bin", "mc")
	cmd := exec.Command("go", "build", "-tags", "test_fake_routing", "-o", f.hostMC, "./cmd/mc")
	cmd.Dir = filepath.Join(repoRoot, "mc")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build host mc: %v\n%s", err, out)
	}

	// The image: linux/arm64 mc + mc-land baked (contract §1). Built once;
	// Docker's cache makes reruns cheap.
	build := exec.Command("bash", filepath.Join(repoRoot, "runner", "image", "build.sh"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build image: %v\n%s", err, out)
	}

	// Spine volume + warm helper: the lock domain (Inv. 24, §11.5).
	f.docker("volume", "create", f.volume)
	t.Cleanup(func() {
		// Volume removal must outlive (run after) container removal: LIFO.
		exec.Command("docker", "volume", "rm", "-f", f.volume).Run()
	})
	f.docker("run", "-d", "--rm", "--name", f.helper,
		"--label", "mc-managed", "--label", "mc-tier=helper",
		"-v", f.volume+":/mc/spine",
		"-v", f.home+":/mc/home:ro",
		"-e", "MC_SPINE="+spineDBPath,
		"-e", "MC_HOME=/mc/home",
		image, "sleep", "infinity")
	t.Cleanup(func() {
		// Reap any straggler agent containers this run spawned, then the helper.
		if out, err := exec.Command("docker", "ps", "-aq",
			"--filter", "label=mc-managed", "--filter", "name=mc-run-").Output(); err == nil {
			for _, id := range strings.Fields(string(out)) {
				exec.Command("docker", "rm", "-f", id).Run()
			}
		}
		exec.Command("docker", "rm", "-f", f.helper).Run()
	})

	// Host-side mc env: helper delegation only — MC_SPINE deliberately unset
	// (the spine never leaves the lock domain, §11.5).
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "MC_SPINE=") || strings.HasPrefix(e, "MC_HELPER=") ||
			strings.HasPrefix(e, "MC_RUN_JSON=") || strings.HasPrefix(e, "MC_TICK_INTERVAL_MS=") {
			continue
		}
		f.env = append(f.env, e)
	}
	f.env = append(f.env, "MC_HELPER="+f.helper)

	// Provision: shrunk tunables (contract §7 fixture list).
	// The profile's workspace root is the HOST path: the mount attest derives
	// the plan's canonical source from it; /workspace/source is the derived
	// container destination, never operator input.
	initEffect := f.mcOK("", "init", "--spine", spineDBPath,
		"--worksource", worksource, "--workspace-root", f.ws,
		"--timeout-minutes", "10", "--grace-minutes", "5",
		"--heartbeat-interval-s", "1", "--spawn-grace-s", "5",
		"--hard-deadline-minutes", "30")

	// The ADR-016 D1 deployment identity mirror: dispatch refuses to prepare
	// without it matching meta.deployment_uuid. f.home is the host side of
	// the container's MC_HOME bind.
	uuid, _ := initEffect["deployment_uuid"].(string)
	if uuid == "" {
		t.Fatalf("mc init effect carries no deployment_uuid: %v", initEffect)
	}
	if err := os.WriteFile(filepath.Join(f.home, "deployment.uuid"), []byte(uuid+"\n"), 0o600); err != nil {
		t.Fatalf("write deployment mirror: %v", err)
	}

	// The Worksource: host git repo, one commit on main, relative worktree
	// links (§6.2), .mc-worktrees/ ignored.
	f.git("init", "-q", "-b", "main")
	f.git("config", "user.name", "e2e operator")
	f.git("config", "user.email", "operator@e2e.invalid")
	f.git("config", "worktree.useRelativePaths", "true")
	if err := os.WriteFile(filepath.Join(f.ws, ".gitignore"), []byte(".mc-worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ws, "README.md"), []byte("walking skeleton worksource\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git("add", ".gitignore", "README.md")
	f.git("commit", "-q", "-m", "initial commit")

	f.writeBehaviors()
	f.writeResidentConfig(repoRoot)
	return f
}

// writeBehaviors materializes the five scripted role behaviors (contract §6:
// behaviors dir bind-mounted RO at /mc/behaviors; the exec steps are the
// fake family's "brief comprehension" — the agent side of the container
// invoking the real scoped mc, contract §4).
func (f *fixture) writeBehaviors() {
	f.t.Helper()
	dir := filepath.Join(f.base, "behaviors")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	behaviors := map[string]string{
		// Editor: promote the exact snapshotted pool (single task in the
		// skeleton, so $MC_POOL_IDS is one id).
		"editor.json": `{"steps":[
			{"do":"exec","command":"printf '{\"verdicts\":[{\"task\":%s,\"decision\":\"promote\",\"reason\":\"skeleton editor pass\"}]}' \"$MC_POOL_IDS\" | mc editor decide --run \"$MC_RUN_ID\" --batch -"},
			{"do":"succeed","output":"promoted"}]}`,
		// Worker: sleep 2.5 s (spans heartbeat intervals — ladder 5), then
		// worktree + branch + commit (contract A3), then the terminal write.
		"worker.json": `{"steps":[
			{"do":"sleep","seconds":2.5},
			{"do":"exec","command":"set -e; cd /workspace/source; git worktree add .mc-worktrees/task-$MC_SUBJECT_ID -b mc/task-$MC_SUBJECT_ID; cd .mc-worktrees/task-$MC_SUBJECT_ID; echo \"skeleton work for task $MC_SUBJECT_ID\" > skeleton.txt; git add skeleton.txt; git -c user.name='mc worker' -c user.email='worker@mc.invalid' commit -q -m \"mc/task-$MC_SUBJECT_ID: skeleton work\""},
			{"do":"exec","command":"mc complete \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --status worked --branch \"mc/task-$MC_SUBJECT_ID\""},
			{"do":"succeed","output":"worked"}]}`,
		// Verifier: verdict on the exact commit it inspected (contract A2).
		// Role outputs never land in the trace-only session folder (Inv. 26,
		// spec §4): evidence goes under the gitignored .mc-worktrees/ — as a
		// SIBLING of the registered worktree, so `git worktree remove` (§7
		// step 3) never sees an untracked file inside it.
		"verifier.json": `{"steps":[
			{"do":"exec","command":"set -e; sha=$(git -C /workspace/source rev-parse \"refs/heads/mc/task-$MC_SUBJECT_ID\"); printf 'gate ladder: fake pass\\n' > \"/workspace/source/.mc-worktrees/task-$MC_SUBJECT_ID.evidence.txt\"; mc verifier verdict \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --outcome pass --evidence \"/workspace/source/.mc-worktrees/task-$MC_SUBJECT_ID.evidence.txt\" --sha \"$sha\""},
			{"do":"succeed","output":"verified"}]}`,
		// Packager: packaged + packet birth in one transaction (Inv. 10/11);
		// the rendered packet is a role output too — same non-session home.
		"packager.json": `{"steps":[
			{"do":"exec","command":"mc complete \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --status packaged --outputs \"/workspace/source/.mc-worktrees/task-$MC_SUBJECT_ID.packet.md\""},
			{"do":"succeed","output":"packaged"}]}`,
		// Strategist(propose): the liveness terminal — empty batch, which
		// also exercises the subjectless lease (ADR-001 constraint b).
		"strategist.json": `{"steps":[
			{"do":"exec","command":"printf '{\"proposals\":[]}' | mc strategist propose --run \"$MC_RUN_ID\" --batch -"},
			{"do":"succeed","output":"no proposals"}]}`,
	}
	for name, body := range behaviors {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) writeResidentConfig(repoRoot string) {
	f.t.Helper()
	cfg := map[string]any{
		"mcPath":              f.hostMC,
		"tickIntervalMs":      500,
		"mcHome":              f.home,
		"releaseBuildId":      "development",
		"configSchemaVersion": 1,
		"image":               image,
		"agentCmd":            []string{"bun", "/app/src/agent-runner/main.ts"},
		"landCmd":             []string{"mc-land"},
		"behaviorsDir":        filepath.Join(f.base, "behaviors"),
		"runnerSrcDir":        filepath.Join(repoRoot, "runner"),
		"workspaceRoot":       f.ws,
		"spineVolume":         f.volume,
		"spineDbPath":         spineDBPath,
		"roleBehaviors": map[string]string{
			"editor":              "/mc/behaviors/editor.json",
			"worker":              "/mc/behaviors/worker.json",
			"verifier":            "/mc/behaviors/verifier.json",
			"packager":            "/mc/behaviors/packager.json",
			"strategist(propose)": "/mc/behaviors/strategist.json",
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.base, "resident.json"), b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// startResident launches the real resident on the real timer; the test only
// observes from here (contract A6: timer-driven, real clock).
func (f *fixture) startResident() {
	f.t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		f.t.Fatalf("bun not found (run via mise exec): %v", err)
	}
	logPath := filepath.Join(f.base, "resident.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		f.t.Fatal(err)
	}
	f.resLog = logFile

	cmd := exec.Command(bun, "src/main.ts", "--config", filepath.Join(f.base, "resident.json"))
	cmd.Dir = filepath.Join(mustAbs(f.t, "../.."), "resident")
	cmd.Env = f.env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		f.t.Fatalf("start resident: %v", err)
	}
	f.resProc = cmd
	f.t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		logFile.Close()
		if f.t.Failed() {
			if b, err := os.ReadFile(logPath); err == nil {
				f.t.Logf("resident log:\n%s", tail(string(b), 8000))
			}
		}
	})
}

// ───────────────────────────── helpers ──────────────────────────────────

type mcResult struct {
	code   int
	stdout string
	stderr string
}

func (f *fixture) mcRun(stdin string, args ...string) mcResult {
	f.t.Helper()
	cmd := exec.Command(f.hostMC, args...)
	cmd.Env = f.env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	res := mcResult{stdout: out.String(), stderr: errb.String()}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			res.code = exit.ExitCode()
		} else {
			f.t.Fatalf("run mc %v: %v", args, err)
		}
	}
	return res
}

func (f *fixture) mcOK(stdin string, args ...string) map[string]any {
	f.t.Helper()
	res := f.mcRun(stdin, args...)
	if res.code != 0 {
		f.t.Fatalf("mc %v exited %d: %s", args, res.code, res.stderr)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &m); err != nil {
		f.t.Fatalf("mc %v: bad JSON %q: %v", args, res.stdout, err)
	}
	return m
}

func (f *fixture) runs() []map[string]any {
	f.t.Helper()
	raw := f.mcOK("", "run", "list")["runs"].([]any)
	rows := make([]map[string]any, len(raw))
	for i, r := range raw {
		rows[i] = r.(map[string]any)
	}
	return rows
}

func (f *fixture) runRow(runID string) map[string]any {
	f.t.Helper()
	for _, r := range f.runs() {
		if r["id"] == runID {
			return r
		}
	}
	return nil
}

func (f *fixture) packets() []map[string]any {
	f.t.Helper()
	raw := f.mcOK("", "packet", "list")["packets"].([]any)
	rows := make([]map[string]any, len(raw))
	for i, r := range raw {
		rows[i] = r.(map[string]any)
	}
	return rows
}

func (f *fixture) waitFor(d time.Duration, desc string, cond func() (bool, string)) {
	f.t.Helper()
	deadline := time.Now().Add(d)
	last := "(no observation)"
	for time.Now().Before(deadline) {
		ok, note := cond()
		if ok {
			return
		}
		if note != "" {
			last = note
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.t.Fatalf("timed out (%s) waiting for %s; last: %s", d, desc, last)
}

// waitForOwner polls the lease until the given role holds it and returns the
// claiming run_id (ladder steps 3/5: lock held, owner=<role> — Inv. 4).
func (f *fixture) waitForOwner(role string, d time.Duration) string {
	f.t.Helper()
	var runID string
	f.waitFor(d, "lock held by "+role, func() (bool, string) {
		lock := f.mcOK("", "lock", "get")
		if lock["owner"] != role {
			return false, fmt.Sprintf("owner=%v run=%v", lock["owner"], lock["run_id"])
		}
		runID = lock["run_id"].(string)
		return true, ""
	})
	return runID
}

func (f *fixture) waitForTaskStatus(taskID int64, status string, d time.Duration) {
	f.t.Helper()
	f.waitFor(d, fmt.Sprintf("task %d status %s", taskID, status), func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
		if task["status"] != status {
			return false, fmt.Sprintf("status=%v blocked=%v (%v)", task["status"], task["blocked"], task["blocked_reason"])
		}
		return true, ""
	})
}

func (f *fixture) docker(args ...string) {
	f.t.Helper()
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		f.t.Fatalf("docker %v: %v\n%s", args, err, out)
	}
}

func (f *fixture) dockerOutput(args ...string) string {
	f.t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("docker %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	out, err := f.gitErr(args...)
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func (f *fixture) gitErr(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = f.ws
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
