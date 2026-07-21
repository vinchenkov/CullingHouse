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
	"database/sql"
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

	_ "modernc.org/sqlite"

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
	// Built host-side in a directory nothing bind-mounts, then loaded into a
	// named volume: the container opens it inside one kernel (Inv. 24).
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
	spineVolume := f.probeSpineVolume("verdict", spine)
	runJSON := filepath.Join(f.base, "verify-42.run.json")
	if err := os.WriteFile(runJSON, []byte("{\"run_id\":\"verify-42\",\"tier\":\"pipeline\",\"role\":\"verifier\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := "mc-run-" + verify
	f.docker("create", "--rm", "--name", agent, "--network", "none", "--user", "10002:10002",
		"-v", taskRoot+":/workspace:ro", "-v", projection+":/workspace/source",
		"-v", filepath.Join(taskRoot, "source", ".git")+":/workspace/source/.git:ro",
		"-v", filepath.Join(taskRoot, "source", ".mission-control")+":/workspace/source/.mission-control:ro",
		"-v", spineVolume+":/mc/spine", "-v", runJSON+":/mc/run.json:ro", "-e", "MC_SPINE=/mc/spine/spine.db",
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

// TestSpineLockDomainGuardDockerBoundary pins BOTH directions of the Inv. 24
// lock-domain guard against the real kernel that enforces it.
//
// The guard's only production implementation is Linux-only, so the fast lane
// proves its decision function against captured mountinfo text and nothing
// more; this is where the actual /proc/self/mountinfo of the actual container
// gets a vote. Both arms are here on purpose. Every other Docker test proves
// ACCEPTANCE implicitly (they would all fail if the guard refused a named
// volume), so acceptance alone can pass while the refusal has quietly rotted
// into a no-op — the exact failure that made the sealed verdict unreachable
// while both suites stayed green (6657541).
//
// The refusal arm also pins the S5 finding in production: Docker Desktop
// surfaces a host bind as `fakeowner`, not `virtiofs`, so a denylist keyed on
// the obvious name would accept the very mount that corrupted the spine.
func TestSpineLockDomainGuardDockerBoundary(t *testing.T) {
	f := setup(t)

	// Refused: a host directory bound at /mc/spine, the shape the E2E fixture
	// used until this guard landed.
	bindDir := filepath.Join(f.base, "bound-spine")
	if err := os.MkdirAll(bindDir, 0o777); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("docker", "run", "--rm", "--network", "none",
		"-v", bindDir+":/mc/spine", "-e", "MC_SPINE=/mc/spine/spine.db",
		image, "mc", "lock", "get").CombinedOutput()
	if err == nil {
		t.Fatalf("bind-mounted spine was ACCEPTED: %q — Inv. 24's guard is not enforcing", out)
	}
	for _, want := range []string{"Inv. 24", "fakeowner", "not a block-device-backed local filesystem"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("bind refusal %q does not name %q", out, want)
		}
	}

	// Refused: a SINGLE FILE bound over spine.db inside an otherwise-legitimate
	// named volume. The directory still reports ext4; only the database itself
	// is on VirtioFS. This is the arm a directory-only guard would pass.
	fileBind := filepath.Join(f.base, "bound-spine.db")
	if err := os.WriteFile(fileBind, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command("docker", "run", "--rm", "--network", "none",
		"-v", f.volume+":/mc/spine", "-v", fileBind+":"+spineDBPath,
		"-e", "MC_SPINE="+spineDBPath, image, "mc", "lock", "get").CombinedOutput()
	if err == nil {
		t.Fatalf("single-file-bound spine was ACCEPTED: %q — the guard is checking only the directory", out)
	}
	if !strings.Contains(string(out), "Inv. 24") || !strings.Contains(string(out), spineDBPath) {
		t.Fatalf("single-file bind refusal %q does not name Inv. 24 and the spine file", out)
	}

	// Accepted: the named volume the production topology actually uses.
	if out := f.dockerOutput("run", "--rm", "--network", "none",
		"-v", f.volume+":/mc/spine", "-e", "MC_SPINE="+spineDBPath,
		image, "mc", "lock", "get"); !strings.Contains(out, "run_id") {
		t.Fatalf("named-volume spine read = %q, want the lock row — the guard must not refuse the lock domain", out)
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

// TestAcceptedSealRebuildDockerBoundary proves the D6 setup crossing with the
// shipped image. It publishes and accepts a real immutable seal, gives the
// image only that seal plus the resident-shaped empty canonical root, then
// records and continues the exact live Verifier setup receipt.
func TestAcceptedSealRebuildDockerBoundary(t *testing.T) {
	f := setup(t)
	const (
		taskID        = int64(7)
		setupRun      = "setup-run"
		workerRun     = "worker-seal"
		completionReq = "0011223344556677"
		verifierRun   = "verify-run"
	)
	seedRoot := filepath.Join(f.base, "sealed-source")
	for _, child := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(seedRoot, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	seeded, err := verbs.MaterializeFirstTaskStore(f.ws, seedRoot, verbs.FirstTaskSetupSpec{
		TaskID: taskID, Mode: "fresh", TargetRef: "main", ObjectFormat: "sha1",
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	})
	if err != nil {
		t.Fatalf("materialize seal source: %v", err)
	}
	seals := filepath.Join(f.home, "seals")
	if err := os.MkdirAll(seals, 0o700); err != nil {
		t.Fatal(err)
	}
	sealRoot := filepath.Join(seals, workerRun)
	publication, err := verbs.SealTaskCompletion(seedRoot, sealRoot, workerRun, completionReq, taskID)
	if err != nil {
		t.Fatalf("publish filesystem seal: %v", err)
	}

	taskRoot := filepath.Join(f.ws, ".mission-control", "tasks", "task-7")
	for _, child := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(taskRoot, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(taskRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	spine := filepath.Join(f.base, "d6-spine.db")
	if _, err := verbs.Init(verbs.InitArgs{Spine: spine, Worksource: "ws-d6", WorkspaceRoot: f.ws}); err != nil {
		t.Fatalf("init D6 spine: %v", err)
	}
	db, err := verbs.OpenSpine(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO tasks (id,title,scope,priority,created_at,status,dispatch_retries,origin,worksource,target_ref)
		VALUES (7,'D6 rebuild fixture','task',2,datetime('now'),'proposed',3,'user','ws-d6','main')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', 'ws-d6', 7)`, setupRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-d6', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, setupRun); err != nil {
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
	if _, err := verbs.RegisterFirstTaskSetup(db, verbs.TaskSetupReceipt{RunID: setupRun, TaskID: taskID,
		Root: verbs.TaskSetupIdentity{Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(uint64(st.Ino), 10), OwnerUID: int(st.Uid)}}); err != nil {
		t.Fatalf("register canonical D6 skeleton: %v", err)
	}
	if _, err := db.Exec(`UPDATE runs SET ended_at=datetime('now'), outcome='setup-complete' WHERE id=?`, setupRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=NULL, worksource=NULL, subject=NULL, owner=NULL, acquired_at=NULL, hard_deadline_at=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', 'ws-d6', 7)`, workerRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-d6', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, workerRun); err != nil {
		t.Fatal(err)
	}
	if err := verbs.PublishCompletionSeal(db, publication); err != nil {
		t.Fatalf("record published seal: %v", err)
	}
	if err := verbs.AcceptCompletionSeal(db, workerRun, completionReq); err != nil {
		t.Fatalf("accept sealed Worker completion: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'verifier', 'ws-d6', 7)`, verifierRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-d6', subject=7, owner='verifier', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, verifierRun); err != nil {
		t.Fatal(err)
	}

	envelope := verbs.SetupEnvelope{
		SchemaVersion: 1, Operation: verbs.SetupOperationAcceptedSealRebuild,
		RunID: verifierRun, TaskID: taskID, ObjectFormat: publication.ObjectFormat,
		CompletionRunID: workerRun, CompletionRequest: completionReq, SealedSHA: publication.SealedSHA,
		ClosureDigest: publication.ClosureDigest, ManifestDigest: publication.ManifestDigest,
		SealRoot: "/repo/seal", TaskRoot: "/repo/task", SealDevice: publication.Device,
		SealInode: publication.Inode, SealOwnerUID: publication.OwnerUID,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelopePath := filepath.Join(f.home, verifierRun+".setup.json")
	if err := os.WriteFile(envelopePath, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	output := f.dockerOutput("run", "--rm", "--name", "mc-setup-"+verifierRun, "--network", "none",
		"--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", "mc-run-id="+verifierRun,
		"--user", "10002:10002", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
		"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
		"-v", sealRoot+":/repo/seal:ro", "-v", taskRoot+":/repo/task:ro",
		"-v", filepath.Join(taskRoot, "source")+":/repo/task/source", "-v", filepath.Join(taskRoot, "git")+":/repo/task/git",
		"-v", envelopePath+":/mc/setup.json:ro", image, "mc", "__setup-accepted-seal", "/mc/setup.json")
	var result verbs.SetupResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("accepted-seal setup result %q: %v", output, err)
	}
	if !result.FsckClean || result.BaseSHA != seeded.BaseSHA || result.ClosureDigest != publication.ClosureDigest {
		t.Fatalf("accepted-seal setup result = %+v, want sealed clean closure", result)
	}
	receipt, err := verbs.RecordAcceptedSealRebuild(db, verifierRun, f.ws, result)
	if err != nil {
		t.Fatalf("record accepted-seal rebuild: %v", err)
	}
	if receipt.CompletionRunID != workerRun || receipt.ManifestDigest != publication.ManifestDigest || receipt.Root.Device != strconv.FormatUint(uint64(st.Dev), 10) {
		t.Fatalf("accepted-seal receipt = %+v, want live seal/root evidence", receipt)
	}
	continued, err := verbs.ContinueAcceptedSealRebuild(db, verifierRun)
	if err != nil || continued.AlreadyContinued {
		t.Fatalf("continue accepted-seal rebuild = (%+v, %v)", continued, err)
	}
	var holder any
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != nil {
		t.Fatalf("accepted-seal continuation left lease held by %v", holder)
	}
}

// TestSealedWorkerCompletionDockerBoundary runs the real mc completion wrapper
// in the shipped image. It starts with a recorded canonical store and live
// Worker lease, gives the container only the task root, run identity, private
// run-keyed seal root, and lock-domain spine, then proves publication and
// acceptance form one durable terminal crossing.
func TestSealedWorkerCompletionDockerBoundary(t *testing.T) {
	f := setup(t)
	const (
		taskID    = int64(7)
		setupRun  = "setup-run"
		workerRun = "worker-seal"
		requestID = "0011223344556677"
	)
	taskRoot := filepath.Join(f.ws, ".mission-control", "tasks", "task-7")
	for _, child := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(taskRoot, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	seeded, err := verbs.MaterializeFirstTaskStore(f.ws, taskRoot, verbs.FirstTaskSetupSpec{
		TaskID: taskID, Mode: "fresh", TargetRef: "main", ObjectFormat: "sha1",
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	})
	if err != nil {
		t.Fatalf("materialize completion store: %v", err)
	}
	if err := os.Chmod(taskRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	// Seeded host-side in a directory nothing bind-mounts, then handed to the
	// container through a named volume and read back out of it — the host and
	// the container never hold this database open at the same time (Inv. 24).
	spineDir := filepath.Join(f.base, "completion-spine")
	if err := os.MkdirAll(spineDir, 0o777); err != nil {
		t.Fatal(err)
	}
	spine := filepath.Join(spineDir, "spine.db")
	if _, err := verbs.Init(verbs.InitArgs{Spine: spine, Worksource: "ws-completion", WorkspaceRoot: f.ws}); err != nil {
		t.Fatalf("init completion spine: %v", err)
	}
	db, err := verbs.OpenSpine(spine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id,title,scope,priority,created_at,status,dispatch_retries,origin,worksource,target_ref)
		VALUES (7,'D6 completion fixture','task',2,datetime('now'),'proposed',3,'user','ws-completion','main')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', 'ws-completion', 7)`, setupRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-completion', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, setupRun); err != nil {
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
	if _, err := verbs.RegisterFirstTaskSetup(db, verbs.TaskSetupReceipt{RunID: setupRun, TaskID: taskID,
		Root: verbs.TaskSetupIdentity{Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(uint64(st.Ino), 10), OwnerUID: int(st.Uid)}}); err != nil {
		t.Fatalf("register canonical completion root: %v", err)
	}
	if _, _, err := verbs.RecordFirstTaskSetupClosure(db, setupRun, f.ws, seeded); err != nil {
		t.Fatalf("record canonical completion store: %v", err)
	}
	if _, err := db.Exec(`UPDATE runs SET ended_at=datetime('now'), outcome='setup-complete' WHERE id=?`, setupRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=NULL, worksource=NULL, subject=NULL, owner=NULL, acquired_at=NULL, hard_deadline_at=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', 'ws-completion', 7)`, workerRun); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-completion', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, workerRun); err != nil {
		t.Fatal(err)
	}
	sealRoot := filepath.Join(f.home, "seals", workerRun)
	if err := os.MkdirAll(sealRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runJSON := filepath.Join(f.base, workerRun+".run.json")
	if err := os.WriteFile(runJSON, []byte(`{"run_id":"worker-seal","tier":"pipeline","role":"worker"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hand the spine over: a clean close checkpoints the WAL away, and the
	// host holds no handle while the container writes.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	spineVolume := f.probeSpineVolume("completion", spine)
	output := f.dockerOutput("run", "--rm", "--name", "mc-complete-"+workerRun, "--network", "none",
		"--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", "mc-run-id="+workerRun,
		// The completion-only setuid wrapper is its deliberate D6 exception to
		// no-new-privileges: uid 10002 never traverses /mc/private directly.
		"--user", "10002:10002", "--cap-drop", "ALL",
		"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
		"-v", taskRoot+":/workspace", "-v", sealRoot+":/mc/private/completion-seal",
		"-v", spineVolume+":/mc/spine", "-v", runJSON+":/mc/run.json:ro", "-e", "MC_SPINE=/mc/spine/spine.db",
		image, "mc", "complete", "7", "--run", workerRun, "--seal-request", requestID)
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("completion result %q: %v", output, err)
	}
	// Take the database back out of the volume to assert on what the
	// in-container publisher durably committed.
	db, err = verbs.OpenSpine(f.readBackSpine(spineVolume, "completion"))
	if err != nil {
		t.Fatalf("reopen completion spine after the container wrote it: %v", err)
	}
	defer db.Close()
	if result["status"] != "worked" || int64(result["task_id"].(float64)) != taskID {
		t.Fatalf("completion result = %v, want accepted worked task", result)
	}
	var state, outcome, sealedSHA, closure, manifest string
	if err := db.QueryRow(`SELECT state,sealed_sha,closure_digest,manifest_digest FROM completion_seals WHERE run_id=? AND completion_request_id=?`, workerRun, requestID).Scan(&state, &sealedSHA, &closure, &manifest); err != nil {
		t.Fatalf("read durable completion seal: %v", err)
	}
	if state != "accepted" || sealedSHA != seeded.BaseSHA || closure != seeded.ClosureDigest || len(manifest) != 64 {
		t.Fatalf("completion seal = (%s,%s,%s,%s), want accepted immutable canonical closure", state, sealedSHA, closure, manifest)
	}
	if err := db.QueryRow(`SELECT outcome FROM runs WHERE id=?`, workerRun).Scan(&outcome); err != nil || outcome != "completed" {
		t.Fatalf("Worker terminal = (%q, %v), want completed", outcome, err)
	}
	var holder any
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != nil {
		t.Fatalf("completion acceptance left lease held by %v", holder)
	}
	entries, err := os.ReadDir(sealRoot)
	if err != nil || len(entries) != 3 {
		t.Fatalf("sealed root entries = (%v, %v), want pack/index/manifest", entries, err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("sealed entry %q = (%v, %v), want regular immutable content", entry.Name(), info.Mode(), err)
		}
	}
	// The model uid cannot traverse the image-owned private parent even though
	// Docker mounted this host seal RW below it; only the setuid wrapper crossed
	// that gate. The completion uid sees the final immutable mode inside the
	// Linux mount namespace (Desktop's host-side mode projection is not the
	// authority boundary).
	f.docker("run", "--rm", "--network", "none", "--user", "10002:10002",
		"-v", sealRoot+":/mc/private/completion-seal", image, "sh", "-ec", "test ! -e /mc/private/completion-seal")
	if mode := f.dockerOutput("run", "--rm", "--network", "none", "--user", "10001:10001",
		"-v", sealRoot+":/mc/private/completion-seal:ro", image, "stat", "-c", "%a", "/mc/private/completion-seal/manifest.json"); mode != "444\n" {
		t.Fatalf("completion namespace manifest mode = %q, want 0444", mode)
	}
}

// TestProductionWorkerCompletionSealDockerBoundary is the resident-driven half
// of the D6 completion-seal crossing. TestSealedWorkerCompletionDockerBoundary
// invokes `mc complete --seal-request` directly; this proves the SAME accepted
// immutable seal fence is reached when the REAL resident dispatches a
// production (non-fake) Worker whose plan carries the run-keyed completion-seal
// row. The Worker route is `codex/chatgpt` — a non-fake route, so the Go attest
// attaches the typed task-store plan plus the completion seal — and the
// resident's `agentRunnerRoutes` allowlist authorizes the shipped agent-runner
// to execute it. Nothing here calls `mc dispatch`; the timer drives the spawn.
func TestProductionWorkerCompletionSealDockerBoundary(t *testing.T) {
	const (
		taskID      = int64(7)
		setupRun    = "prod-setup-run"
		sealRequest = "0011223344556677"
	)

	// The canonical task store the production Worker will seal, and the spine
	// state that must already exist for the resident's FIRST dispatch to
	// resolve a 15-row Worker plan (never a precreate). Both are built by the
	// seeding hook, which the fixture runs host-side against a plain temp path
	// and then loads into the named volume: the spine the containers open is
	// only ever opened by ONE kernel, and no helper spine read crosses
	// VirtioFS.
	var seeded verbs.SetupResult
	f := setup(t, withSeededSpine(func(f *fixture, db *sql.DB) {
		// Materialize the store exactly as first-task setup would leave it,
		// then fix the root to 0555.
		taskRoot := filepath.Join(f.ws, ".mission-control", "tasks", "task-7")
		for _, child := range []string{"source", "git"} {
			if err := os.MkdirAll(filepath.Join(taskRoot, child), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		seeded, err = verbs.MaterializeFirstTaskStore(f.ws, taskRoot, verbs.FirstTaskSetupSpec{
			TaskID: taskID, Mode: "fresh", TargetRef: "main", ObjectFormat: "sha1",
			LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
		})
		if err != nil {
			t.Fatalf("materialize production task store: %v", err)
		}
		if err := os.Chmod(taskRoot, 0o555); err != nil {
			t.Fatal(err)
		}

		// Task 7 seeded and assigned, a durable receipt vouching the
		// materialized skeleton identity, and a free lock.
		if _, err := db.Exec(`INSERT INTO tasks (id,title,scope,priority,created_at,status,dispatch_retries,origin,worksource,target_ref)
			VALUES (7,'production seal fixture','task',2,datetime('now'),'proposed',3,'user',?,'main')`, worksource); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=7`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', ?, 7)`, setupRun, worksource); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource=?, subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, setupRun, worksource); err != nil {
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
		if _, err := verbs.RegisterFirstTaskSetup(db, verbs.TaskSetupReceipt{RunID: setupRun, TaskID: taskID,
			Root: verbs.TaskSetupIdentity{Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(uint64(st.Ino), 10), OwnerUID: int(st.Uid)}}); err != nil {
			t.Fatalf("register production skeleton receipt: %v", err)
		}
		if _, _, err := verbs.RecordFirstTaskSetupClosure(db, setupRun, f.ws, seeded); err != nil {
			t.Fatalf("record production closure assignment: %v", err)
		}
		if _, err := db.Exec(`UPDATE runs SET ended_at=datetime('now'), outcome='setup-complete' WHERE id=?`, setupRun); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET run_id=NULL, worksource=NULL, subject=NULL, owner=NULL, acquired_at=NULL, hard_deadline_at=NULL WHERE id=1`); err != nil {
			t.Fatal(err)
		}
	}))

	// Route the Worker to the non-fake production binding, authorize the
	// agent-runner to stand in for it, and give it a behavior that reaches the
	// real sealed completion terminal.
	if err := os.WriteFile(filepath.Join(f.home, "routing.md"), []byte(`# production seal E2E routing
| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | codex | chatgpt |
| verifier | claude-sdk | claude |
| packager | claude-sdk | minimax |
| refiner | fake | fake |
| homie | fake | fake |
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The Worker makes a REAL commit before sealing. Without it the sealed HEAD
	// equals the base, the eventual landing merge is trivially up-to-date, and
	// the walk would prove the lane runs without proving it MERGES anything.
	sealBehavior := `{"steps":[
		{"do":"exec","command":"set -e; cd /workspace/source; printf 'worker change\\n' > WORKED.md; git add -A; git -c user.email=w@example.invalid -c user.name=worker commit -qm 'worker change'; mc complete \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --seal-request ` + sealRequest + `"},
		{"do":"succeed","output":"sealed"}]}`
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", "worker-seal.json"), []byte(sealBehavior), 0o644); err != nil {
		t.Fatal(err)
	}
	// The sealed Verifier reads its disposable projection and renders the
	// verdict against the exact sealed HEAD. Evidence deliberately lands
	// OUTSIDE the projected tree: the D6 verdict fence requires a clean
	// index/tree, so a role output written into /workspace/source would be
	// the very drift the fence refuses.
	verifierSeal := `{"steps":[
		{"do":"exec","command":"set -e; sha=$(git -C /workspace/source rev-parse HEAD); printf 'sealed gate pass\\n' > /tmp/verifier-evidence.txt; mc verifier verdict \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --outcome pass --evidence /tmp/verifier-evidence.txt --sha \"$sha\""},
		{"do":"succeed","output":"verified"}]}`
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", "verifier-seal.json"), []byte(verifierSeal), 0o644); err != nil {
		t.Fatal(err)
	}
	// The sealed Packager is a pure reader of the canonical store: ADR-017:1218
	// has it "receive canonical source/control RO and fail representative
	// writes while their separate record outputs remain writable". The behavior
	// asserts BOTH halves in the container, where the RO bind is real — a Go
	// test can only check the plan's `access` field, never that Docker honored
	// it. Its render lands in /tmp for the same reason the Verifier's evidence
	// does: every canonical path it can see is read-only.
	packagerSeal := `{"steps":[
		{"do":"exec","command":"set -e; test -d /workspace/source; test -d /workspace/git; for p in /workspace/source/packager-probe /workspace/git/packager-probe; do if printf x > \"$p\" 2>/dev/null; then echo \"canonical path $p is writable; the sealed view is not RO\" >&2; exit 1; fi; done; printf 'packet for task %s\\n' \"$MC_SUBJECT_ID\" > /tmp/packet.md; mc complete \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --status packaged --outputs /tmp/packet.md"},
		{"do":"succeed","output":"packaged"}]}`
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", "packager-seal.json"), []byte(packagerSeal), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgBytes, err := os.ReadFile(filepath.Join(f.base, "resident.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["roleBehaviors"].(map[string]any)["worker"] = "/mc/behaviors/worker-seal.json"
	cfg["roleBehaviors"].(map[string]any)["verifier"] = "/mc/behaviors/verifier-seal.json"
	cfg["roleBehaviors"].(map[string]any)["packager"] = "/mc/behaviors/packager-seal.json"
	// All three non-fake routes stand in through the fake adapter (see the
	// 2026-07-18 deviation); the Verifier's is what carries the disposable
	// projection and the sealed verdict fence, and the Packager's is what
	// carries the sealed view RO. `claude-sdk/minimax` is the Packager's
	// canonical production route (spec §9.1 registry); Inv. 9 decorrelation
	// constrains only strategist↔editor and worker↔verifier (routing.go:117),
	// so sharing the Verifier's harness family is legal here.
	cfg["agentRunnerRoutes"] = []string{"codex/chatgpt", "claude-sdk/claude", "claude-sdk/minimax"}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.base, "resident.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}

	// From here the real timer drives: tick → dispatch → production Worker with
	// the completion-seal plan row → resident precreates MC_HOME/seals/<run>,
	// binds /mc/private/completion-seal, launches the agent-runner Worker (model
	// uid 10002, no NNP) → the behavior's `mc complete --seal-request` crosses
	// the setuid wrapper and publishes+accepts the seal in one transaction.
	f.startResident()

	// Task 7 is an assigned standalone task, so `worked` is unreachable through
	// the legacy unsealed `--status worked` bypass — only CompleteSealedWorker
	// can transition it, which publishes AND accepts the seal in one
	// transaction. Reaching `worked` therefore already proves the accepted fence.
	f.waitForTaskStatus(taskID, "worked", 90*time.Second)

	// The completing Worker is a fresh resident-dispatched run (not the seeded
	// setup run); find it by its completed terminal on task 7. A completed
	// terminal also proves its lease was released — CompleteSealedWorker accepts
	// the seal and frees the lease in one transaction. (The resident then moves
	// on to a downstream Verifier rebuild, which churns the lease, so this test
	// deliberately does not assert a free lock at rest — that is a later slice.)
	var workerRun string
	for _, r := range f.runs() {
		if r["role"] == "worker" && r["id"] != setupRun && r["subject"] != nil &&
			int64(r["subject"].(float64)) == taskID && r["outcome"] == "completed" {
			workerRun = r["id"].(string)
		}
	}
	if workerRun == "" {
		t.Fatalf("no completed production Worker run for task %d: %v", taskID, f.runs())
	}

	// The accepted immutable seal is the SAME fence TestSealedWorkerCompletion…
	// proves directly: three regular sealed files under the run-keyed private
	// root, the manifest frozen read-only.
	sealDir := filepath.Join(f.home, "seals", workerRun)
	entries, err := os.ReadDir(sealDir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("sealed root %s entries = (%v, %v), want pack/index/manifest", sealDir, entries, err)
	}
	for _, entry := range entries {
		fi, err := entry.Info()
		if err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("sealed entry %q = (%v, %v), want regular immutable content", entry.Name(), fi.Mode(), err)
		}
	}
	// The frozen 0444 mode is authoritative only inside the Linux mount
	// namespace — Docker Desktop projects a different host-side mode (see
	// TestSealedWorkerCompletionDockerBoundary). Read it as the completion uid.
	if mode := f.dockerOutput("run", "--rm", "--network", "none", "--user", "10001:10001",
		"-v", sealDir+":/mc/private/completion-seal:ro", image, "stat", "-c", "%a", "/mc/private/completion-seal/manifest.json"); mode != "444\n" {
		t.Fatalf("completion namespace manifest mode = %q, want 0444", mode)
	}

	// The carry-through: the SAME resident now drives the accepted-seal REBUILD
	// off that seal, with no further host action. The Verifier routes to
	// `claude-sdk/claude` — non-fake, so attest carries the 15-row task plan
	// plus the `accepted_seal_rebuild` step (a fake route takes the legacy
	// workspace branch and is gated out of both setup steps), and harness-
	// decorrelated from the Worker's `codex`, which Inv. 9 (routing.go:119)
	// requires. The rebuild is setup-only: the resident runs the network=none
	// setup class over the canonical task root and passes the result through
	// `mc task setup-record`/`setup-continue`, returning before any agent
	// launch — so this route needs no agent-runner authorization. (The later
	// VerifierProjection launch will; that is the next slice.)
	var rebuildRun string
	f.waitFor(180*time.Second, fmt.Sprintf("accepted-seal rebuild receipt for task %d", taskID), func() (bool, string) {
		runID, ok := f.acceptedSealRebuiltRun(taskID)
		if !ok {
			// Name who holds the lease and what the Verifier runs did: a stalled
			// rebuild is always one of "never dispatched" (no verifier run) or
			// "dispatched and refused" (a verifier run pinning the lease).
			lock := f.mcOK("", "lock", "get")
			var verifiers []string
			for _, r := range f.runs() {
				if r["role"] == "verifier" {
					verifiers = append(verifiers, fmt.Sprintf("%v(ended=%v outcome=%v)", r["id"], r["ended_at"], r["outcome"]))
				}
			}
			return false, fmt.Sprintf("no rebuild receipt yet; lock=%v; verifier runs=%v", lock, verifiers)
		}
		rebuildRun = runID
		return true, ""
	})

	// The receipt's fencing trigger already proved the live-Verifier/accepted-
	// seal/registered-root join at insert. What remains to prove out here is the
	// continuation: the rebuild ended only its own setup run, on the Verifier.
	row := f.runRow(rebuildRun)
	if row == nil || row["role"] != "verifier" || row["outcome"] != "accepted-seal-rebuilt" {
		t.Fatalf("rebuild run %s = %v, want a verifier run ended accepted-seal-rebuilt", rebuildRun, row)
	}
	if row["id"] == workerRun {
		t.Fatalf("rebuild run must be a distinct run from the sealing Worker %s", workerRun)
	}

	// The same resident carries straight on: a SECOND Verifier dispatch gets
	// the disposable projection overlaid at /workspace/source, and its agent
	// renders the sealed verdict. Reaching `verified` proves two crossings that
	// only the real container exercises — the projection REPLACES the canonical
	// source rows rather than duplicating them (Docker refuses a duplicate
	// mount point outright), and the verdict fence addresses the projection by
	// its fixed path with an explicit safe.directory grant (an agent
	// container's CWD is "/", and sourceGitEnv switches the image's system
	// config off, so the fence otherwise refused a CLEAN projection exactly
	// like a dirty one).
	f.waitForTaskStatus(taskID, "verified", 120*time.Second)
	verified := f.mcOK("", "task", "get", strconv.FormatInt(taskID, 10))
	// The Worker committed, so the verified commit must have ADVANCED past the
	// assignment's frozen base. Asserting inequality first is what keeps this
	// honest: if the behavior ever stopped committing, an equality check
	// against the base would still pass and the landing below would silently
	// become a no-op merge.
	sealedSHA, _ := verified["verified_sha"].(string)
	if sealedSHA == "" || sealedSHA == seeded.BaseSHA {
		t.Fatalf("verified_sha = %q, want a commit past the frozen base %q", sealedSHA, seeded.BaseSHA)
	}

	// The same resident carries on once more: `verified` dispatches the
	// Packager (dispatch.go:699), routed non-fake so attest derives the sealed
	// view — the 15-row task plan with EVERY row RO (mountattest.go's
	// sealedViewReader arm). Before this slice the Packager health-refused on
	// every repo Worksource; the E2E hid that only because it routed the
	// Packager `fake/fake` onto the legacy workspace lane.
	//
	// Reaching `packaged` proves the arm end-to-end through the real container:
	// the behavior refuses to complete unless both canonical children are
	// present AND unwritable, so a plan that regressed any row to `rw`, or that
	// dropped a child, fails here rather than passing silently. That is the
	// lesson of 6657541 — a fence asserted only in the negative direction let a
	// CLEAN projection be refused exactly like a dirty one for weeks.
	f.waitForTaskStatus(taskID, "packaged", 120*time.Second)

	// Inv. 11: the packet is born only from `packaged`, in the Packager
	// terminal's own transaction (complete.go:169-181).
	packets := f.packets()
	if len(packets) != 1 {
		t.Fatalf("packets = %v, want exactly the one born by the Packager terminal", packets)
	}
	packet := packets[0]
	if got, _ := packet["task_id"].(float64); int64(got) != taskID {
		t.Fatalf("packet task_id = %v, want %d", packet["task_id"], taskID)
	}
	if got, _ := packet["render_path"].(string); got != "/tmp/packet.md" {
		t.Fatalf("packet render_path = %q, want the path the Packager rendered", got)
	}
	if archived, _ := packet["archived"].(bool); archived {
		t.Fatalf("a freshly born packet is unarchived; got %v", packet)
	}

	// The Packager run is a distinct run on the packager role, and it released
	// its lease — the packet decision is an OPERATOR act, so the board must be
	// at rest waiting on a human, not holding a lease.
	var packagerRun string
	for _, r := range f.runs() {
		if r["role"] == "packager" && r["subject"] != nil &&
			int64(r["subject"].(float64)) == taskID && r["outcome"] == "completed" {
			packagerRun = r["id"].(string)
		}
	}
	if packagerRun == "" {
		t.Fatalf("no completed packager run for task %d: %v", taskID, f.runs())
	}
	if packagerRun == workerRun || packagerRun == rebuildRun {
		t.Fatalf("packager run %s collides with an earlier run", packagerRun)
	}


	// The sealed LANDING walk (packaged -> approve -> merge -> archived) belongs
	// here and is NOT yet written, because it cannot pass: `mc dispatch` on
	// Darwin routes through the private helper frame, which refuses a landing
	// candidate outright (dispatchprivate.go privateFrameRefusesLanding). The
	// lane is live on the native single-process path only. Repro, evidence and
	// the intended fix: docs/ledger/phase-3.md (2026-07-21, "the lane is live
	// on the wrong platform").
}

// ───────────────────────────── fixtures ─────────────────────────────────

// setupOptions tunes the shared fixture for the tests that need it.
type setupOptions struct {
	// seedSpine, when set, has the fixture build the spine HOST-SIDE in a
	// plain temp directory that is never bind-mounted, run this hook against
	// it with the verbs package (production task state the resident must find
	// already present), and only then `docker cp` the closed file into the
	// named volume — before any container opens it.
	//
	// The spine therefore stays in the named volume (Inv. 24's lock domain)
	// and is never reachable across a VirtioFS bind, while the test still gets
	// to seed it. The predecessor of this hook bound a host directory at
	// /mc/spine instead; that put every helper spine read on VirtioFS, which
	// is the latency the private helper's fixed 4s deadline could not absorb.
	seedSpine func(f *fixture, db *sql.DB)
}

func withSeededSpine(seed func(f *fixture, db *sql.DB)) func(*setupOptions) {
	return func(o *setupOptions) { o.seedSpine = seed }
}

// initTunables is the shrunk provisioning tail (contract §7 fixture list),
// shared by the delegated and host-side init calls so the two spines differ in
// nothing but where they were built. The profile's workspace root is the HOST
// path: the mount attest derives the plan's canonical source from it, and
// /workspace/source is the derived container destination, never operator input.
func initTunables(ws string) []string {
	return []string{
		"--worksource", worksource, "--workspace-root", ws,
		"--timeout-minutes", "10", "--grace-minutes", "5",
		"--heartbeat-interval-s", "1", "--spawn-grace-s", "5",
		"--hard-deadline-minutes", "30",
	}
}

// loadSpineIntoVolume copies a closed host-built spine into the named volume
// before any container opens it, using a never-started stager container as the
// `docker cp` target (a volume has no path of its own to copy into).
//
// It then widens the volume root. Docker materializes a fresh volume root as
// root:root 0755, which is enough for the root helper but not for the
// production seal path: those agent containers run with `--user 10002:10002`
// and the completion wrapper drops to uid 10001, and BOTH write the spine —
// including the -wal/-shm siblings SQLite creates in the directory.
func (f *fixture) loadSpineIntoVolume(hostSpine string) {
	f.t.Helper()
	f.loadSpineIntoNamedVolume(f.volume, hostSpine)
}

func (f *fixture) loadSpineIntoNamedVolume(volume, hostSpine string) {
	f.t.Helper()
	stager := volume + "-load"
	f.docker("create", "--name", stager, "-v", volume+":/mc/spine", image, "true")
	defer func() { exec.Command("docker", "rm", "-f", stager).Run() }()
	f.docker("cp", hostSpine, stager+":"+spineDBPath)
	f.docker("run", "--rm", "-v", volume+":/mc/spine", image,
		"sh", "-ec", "chmod 0777 /mc/spine && chmod 0666 "+spineDBPath)
}

// probeSpineVolume gives a single-container probe its own lock domain.
//
// A probe that binds a host directory at /mc/spine puts the database on
// VirtioFS, which mc now refuses outright (Inv. 24, substrate.GuardLockDomain)
// — and refused for good reason: that bind is what corrupted the spine in the
// production E2E. The host-built spine is closed by the caller, copied into a
// fresh named volume, and only then opened by the container.
//
// The volume name is derived from the fixture's so the cleanup sweep that
// already removes `f.volume` prefixes finds it too.
func (f *fixture) probeSpineVolume(name, hostSpine string) string {
	f.t.Helper()
	volume := f.volume + "-" + name
	exec.Command("docker", "volume", "rm", "-f", volume).Run()
	f.docker("volume", "create", volume)
	f.t.Cleanup(func() { exec.Command("docker", "volume", "rm", "-f", volume).Run() })
	f.loadSpineIntoNamedVolume(volume, hostSpine)
	return volume
}

// readBackSpine copies a probe's spine OUT of its volume so host-side
// assertions can read what the container wrote.
//
// The host must never open the volume's database directly — that is the
// two-kernel sharing Inv. 24 forbids, and it is precisely what the old
// bind-mounted probes did while a container held the same file. Copying the
// whole directory (not just spine.db) carries any -wal/-shm siblings along, so
// the copy is the complete database even if a writer exited without
// checkpointing.
func (f *fixture) readBackSpine(volume, name string) string {
	f.t.Helper()
	dir := filepath.Join(f.base, name+"-readback")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	stager := volume + "-read"
	f.docker("create", "--name", stager, "-v", volume+":/mc/spine", image, "true")
	defer func() { exec.Command("docker", "rm", "-f", stager).Run() }()
	f.docker("cp", stager+":/mc/spine/.", dir)
	return filepath.Join(dir, "spine.db")
}

func setup(t *testing.T, opts ...func(*setupOptions)) *fixture {
	t.Helper()
	var o setupOptions
	for _, fn := range opts {
		fn(&o)
	}
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

	// The Worksource: host git repo, one commit on main, relative worktree
	// links (§6.2), .mc-worktrees/ ignored. It precedes the spine because a
	// seeding hook materializes a task store inside it (see seedSpine).
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

	// Host-side mc env: helper delegation only — MC_SPINE deliberately unset
	// (the spine never leaves the lock domain, §11.5). MC_HELPER is appended
	// only after the helper exists, so a seeding init below runs DIRECT.
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "MC_SPINE=") || strings.HasPrefix(e, "MC_HELPER=") ||
			strings.HasPrefix(e, "MC_RUN_JSON=") || strings.HasPrefix(e, "MC_TICK_INTERVAL_MS=") {
			continue
		}
		f.env = append(f.env, e)
	}

	// Spine volume + warm helper: the lock domain (Inv. 24, §11.5).
	f.docker("volume", "create", f.volume)
	t.Cleanup(func() {
		// Volume removal must outlive (run after) container removal: LIFO.
		exec.Command("docker", "volume", "rm", "-f", f.volume).Run()
	})

	// Provision. The seeded variant provisions and seeds host-side against a
	// plain temp path, then loads the closed file into the volume; the plain
	// variant provisions through the helper once it is warm. Either way the
	// resident and every container see only the named volume.
	var initEffect map[string]any
	if o.seedSpine != nil {
		seedDir := filepath.Join(base, "seed")
		if err := os.MkdirAll(seedDir, 0o700); err != nil {
			t.Fatal(err)
		}
		hostSpine := filepath.Join(seedDir, "spine.db")
		initEffect = f.mcOK("", append([]string{"init", "--spine", hostSpine}, initTunables(f.ws)...)...)
		db, err := verbs.OpenSpine(hostSpine)
		if err != nil {
			t.Fatalf("open host-built spine: %v", err)
		}
		o.seedSpine(f, db)
		// Close before the copy: a clean close checkpoints the WAL away, so
		// spine.db alone is the whole database.
		if err := db.Close(); err != nil {
			t.Fatalf("close host-built spine: %v", err)
		}
		f.loadSpineIntoVolume(hostSpine)
	}

	f.docker("run", "-d", "--rm", "--name", f.helper,
		"--label", "mc-managed", "--label", "mc-tier=helper",
		"-v", f.volume+":/mc/spine",
		"-v", f.home+":/mc/home:ro",
		"-e", "MC_SPINE="+spineDBPath,
		"-e", "MC_HOME=/mc/home",
		image, "sleep", "infinity")
	t.Cleanup(func() {
		// NOTE: `docker logs` on the helper is useless for diagnosing a private
		// helper refusal — helper verbs run as `docker exec`, so their stderr
		// goes to the exec, never to the container's log. Tried and reverted
		// 2026-07-19. A private-helper refusal is diagnosable only by
		// reproducing the prepare host-side.
		// Reap any straggler agent containers this run spawned, then the helper.
		if out, err := exec.Command("docker", "ps", "-aq",
			"--filter", "label=mc-managed", "--filter", "name=mc-run-").Output(); err == nil {
			for _, id := range strings.Fields(string(out)) {
				exec.Command("docker", "rm", "-f", id).Run()
			}
		}
		exec.Command("docker", "rm", "-f", f.helper).Run()
	})

	f.env = append(f.env, "MC_HELPER="+f.helper)

	if initEffect == nil {
		initEffect = f.mcOK("", append([]string{"init", "--spine", spineDBPath}, initTunables(f.ws)...)...)
	}

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
				// A stalled resident emits an idle tick twice a second, so a raw
				// tail is 8000 characters of "tick: idle" and the line that
				// explains the stall has long scrolled off. Show the decisions.
				f.t.Logf("resident log (idle ticks elided):\n%s", tail(residentDecisions(string(b)), 8000))
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

// acceptedSealRebuiltRun finds the Verifier run that completed the accepted-seal
// rebuild for a task, THROUGH THE LOCK DOMAIN (`mc run list` self-delegates into
// the helper).
//
// It deliberately does not read `accepted_seal_rebuild_receipts` directly. That
// table has no read verb by design — it is internal recovery evidence, not an
// operator surface — and the obvious shortcut, opening the host-bound spine
// with sql.Open alongside the running resident, is exactly what Inv. 24 forbids
// (spec:69): it splits one WAL database across the macOS kernel and the VM's.
// Polling it here was the concrete cause of this test's intermittent
// SQLITE_PROTOCOL / SIGBUS failures (ledger 2026-07-19; a container-writer +
// host-reader probe on that bind produced 13 Bus errors per 400 writes).
//
// The substitute is exact, not weaker: ContinueAcceptedSealRebuild refuses
// unless the durable receipt for that run and task already exists
// (requireAcceptedSealRebuildEvidence), so a Verifier run carrying the
// `accepted-seal-rebuilt` outcome PROVES the receipt. The receipt's binding to
// the sealing Worker's run is enforced inside RecordAcceptedSealRebuild — it
// takes completion_run_id only from the task-pointed accepted seal — and is
// covered by the verbs-level tests.
func (f *fixture) acceptedSealRebuiltRun(taskID int64) (runID string, ok bool) {
	f.t.Helper()
	for _, r := range f.runs() {
		if r["role"] != "verifier" || r["outcome"] != "accepted-seal-rebuilt" {
			continue
		}
		if subject, isNum := r["subject"].(float64); !isNum || int64(subject) != taskID {
			continue
		}
		return r["id"].(string), true
	}
	return "", false
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

// residentDecisions drops the resident's idle-tick heartbeat, keeping only the
// lines where it decided something. A stall produces thousands of the former
// and exactly one of the latter, so the raw tail hides the cause.
// A tick that fails the same way every 500ms repeats its line thousands of
// times, which buries every earlier decision just as effectively as the idle
// ticks do — so collapse consecutive repeats of the same message too. The
// timestamp prefix is stripped for that comparison only.
func residentDecisions(s string) string {
	var kept []string
	elided, prev, repeats := 0, "", 0
	flush := func() {
		if repeats > 1 {
			kept = append(kept, fmt.Sprintf("        … previous line repeated %d more times", repeats-1))
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "tick: idle") {
			elided++
			continue
		}
		msg := line
		if i := strings.Index(line, "] "); i >= 0 {
			msg = line[i+2:]
		}
		if msg == prev && msg != "" {
			repeats++
			continue
		}
		flush()
		kept = append(kept, line)
		prev, repeats = msg, 1
	}
	flush()
	return fmt.Sprintf("(%d idle ticks elided)\n%s", elided, strings.Join(kept, "\n"))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
