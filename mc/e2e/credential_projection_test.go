//go:build docker_e2e

package e2e

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"mc/verbs"
)

// TestCredentialProjectionDockerBoundary is the contract §3 "Credential
// projection" acceptance row (ADR-022) proved through the REAL projector,
// resident spawn seam, and a real agent container — with SYNTHETIC mints. A
// local token authority (the first httptest server in this tree) stands in
// for the provider: the resident POSTs the real refresh grant to it host-side
// and mints a short-lived access token, so the container never needs a live
// subscription. The live-provider legs (a real Claude/Codex subscription
// call) stay parked for the operator; everything else is proved here.
//
// The proof is in-container and self-checking: the Worker behavior asserts the
// projected auth.json and env, exercises the codex refresh broker across the
// container boundary, and refuses to seal on any violation — so reaching
// `worked` IS the acceptance, and a projection defect leaves the task seeded.
func TestCredentialProjectionDockerBoundary(t *testing.T) {
	const (
		taskID       = int64(7)
		setupRun     = "cred-setup-run"
		sealRequest  = "0011223344556677"
		accountID    = "account-cred-e2e"
		realRefresh  = "real-refresh-grant-SENTINEL-never-in-container"
		accessMarker = "synthetic-access-"
	)

	// The synthetic token authority: the provider endpoint the resident's
	// projector POSTs the REAL refresh grant to. It records what it received
	// (so the test proves the host used the real grant) and mints a marked,
	// short-lived access token (so the container proves it holds only that).
	var (
		mu           sync.Mutex
		sawRealGrant atomic.Bool
		mintCount    atomic.Int64
	)
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] == "refresh_token" && body["refresh_token"] == realRefresh {
			sawRealGrant.Store(true)
		}
		n := mintCount.Add(1)
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("%s%d", accessMarker, n),
			"id_token":     fmt.Sprintf("synthetic-id-%d", n),
			"expires_in":   3600,
		})
	}))
	t.Cleanup(authority.Close)

	f := setup(t, withSeededSpine(credentialSeedHook(t, taskID, setupRun)))

	// Route the Worker to the non-fake codex binding and authorize the
	// agent-runner to stand in for it (the 2026-07-18 stand-in deviation).
	if err := os.WriteFile(filepath.Join(f.home, "routing.md"), []byte(`# credential projection E2E routing
| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | codex | chatgpt |
| verifier | fake | fake |
| packager | fake | fake |
| refiner | fake | fake |
| homie | fake | fake |
`), 0o600); err != nil {
		t.Fatal(err)
	}

	// The ADR-022 refresh-grant store the resident reads at startup. The real
	// refresh grant lives ONLY here (deny-mounted host-side) and at the
	// authority; no projection may carry it into a container.
	writeRefreshGrant(t, f.home, map[string]any{
		"binding":       "chatgpt",
		"channel":       "codex",
		"token_url":     authority.URL,
		"client_id":     "cred-e2e-client",
		"refresh_token": realRefresh,
		"account_id":    accountID,
	})

	// The Worker behavior is the acceptance probe. Step 1 (set -e) asserts the
	// projection at the container boundary and exits nonzero on any violation;
	// step 2 only runs if step 1 passed, makes a real commit, and seals. So
	// reaching `worked` proves the projection, and a defect strands the task.
	probe := `set -e
test -f /mc/codex/auth.json
grep -q '"account_id":"` + accountID + `"' /mc/codex/auth.json
grep -q '"auth_mode":"chatgpt"' /mc/codex/auth.json
grep -q '"refresh_token":"mc-projected-dummy-refresh"' /mc/codex/auth.json
grep -q '"access_token":"` + accessMarker + `' /mc/codex/auth.json
if grep -q '` + realRefresh + `' /mc/codex/auth.json; then echo "REAL GRANT IN auth.json" >&2; exit 1; fi
if env | grep -q '` + realRefresh + `'; then echo "REAL GRANT IN ENV" >&2; exit 1; fi
echo "$CODEX_REFRESH_TOKEN_URL_OVERRIDE" | grep -q 'host.docker.internal'
resp=$(curl -sf -X POST "$CODEX_REFRESH_TOKEN_URL_OVERRIDE" -H 'content-type: application/json' -d '{"grant_type":"refresh_token","refresh_token":"mc-projected-dummy-refresh","client_id":"x"}')
echo "$resp" | grep -q '"access_token":"` + accessMarker + `'
echo "$resp" | grep -q '"refresh_token":"mc-projected-dummy-refresh"'
if echo "$resp" | grep -q '` + realRefresh + `'; then echo "REAL GRANT IN BROKER RESPONSE" >&2; exit 1; fi`

	workerCred := `{"steps":[
		{"do":"exec","command":` + strconv.Quote(probe) + `},
		{"do":"exec","command":"set -e; cd /workspace/source; printf 'worker change\n' > WORKED.md; git add -A; git -c user.email=w@example.invalid -c user.name=worker commit -qm 'worker change'; mc complete \"$MC_SUBJECT_ID\" --run \"$MC_RUN_ID\" --seal-request ` + sealRequest + `"},
		{"do":"succeed","output":"sealed"}]}`
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", "worker-cred.json"), []byte(workerCred), 0o644); err != nil {
		t.Fatal(err)
	}
	setResidentWorkerBehavior(t, f, "/mc/behaviors/worker-cred.json", []string{"codex/chatgpt"})

	f.startResident()

	// Reaching `worked` requires the sealed completion, which the probe step
	// gates: the projection is correct end to end.
	f.waitForTaskStatus(taskID, "worked", 120*time.Second)

	// Host-side corroboration: the authority received the REAL refresh grant
	// (so the host, not the container, holds it) and minted at least once.
	if !sawRealGrant.Load() {
		t.Fatal("the token authority never received the real refresh grant; the host did not mint from it")
	}
	if mintCount.Load() == 0 {
		t.Fatal("the token authority minted no access token")
	}
}

// TestCredentialProjectionFailClosedDockerBoundary is the D8 leg: with the
// token authority unreachable, the projector refuses and the Worker never
// launches — the run stalls rather than proceeding uncredentialed, and no
// real refresh material leaks.
func TestCredentialProjectionFailClosedDockerBoundary(t *testing.T) {
	const (
		taskID      = int64(7)
		setupRun    = "cred-d8-setup-run"
		realRefresh = "real-refresh-grant-SENTINEL-d8"
	)

	f := setup(t, withSeededSpine(credentialSeedHook(t, taskID, setupRun)))

	if err := os.WriteFile(filepath.Join(f.home, "routing.md"), []byte(`# credential D8 routing
| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | codex | chatgpt |
| verifier | fake | fake |
| packager | fake | fake |
| refiner | fake | fake |
| homie | fake | fake |
`), 0o600); err != nil {
		t.Fatal(err)
	}

	// A token authority that is DOWN: a closed loopback port. The projector's
	// mint fails, the token service serves null, and project() refuses.
	writeRefreshGrant(t, f.home, map[string]any{
		"binding":       "chatgpt",
		"channel":       "codex",
		"token_url":     "http://127.0.0.1:1/oauth/token",
		"client_id":     "cred-d8-client",
		"refresh_token": realRefresh,
		"account_id":    "account-d8",
	})

	// A behavior that would SUCCEED if it ever ran — so a stalled task proves
	// the refusal, not an unrelated failure.
	workerD8 := `{"steps":[{"do":"succeed","output":"should never launch"}]}`
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", "worker-d8.json"), []byte(workerD8), 0o644); err != nil {
		t.Fatal(err)
	}
	setResidentWorkerBehavior(t, f, "/mc/behaviors/worker-d8.json", []string{"codex/chatgpt"})

	f.startResident()

	// The task must NOT reach worked: the projector refuses before any
	// container is created. Give the timer several ticks to prove the stall.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if f.mcOK("", "task", "get", fmt.Sprint(taskID))["status"] == "worked" {
			t.Fatalf("task %d reached worked despite an unreachable token authority (D8 breach)", taskID)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// The resident log must record the D8 refusal and must not leak the grant.
	log, err := os.ReadFile(filepath.Join(f.base, "resident.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAny(string(log), "credential unavailable", "credential projection unavailable", "mint failed") {
		t.Fatalf("resident log does not record the D8 credential refusal:\n%s", log)
	}
	if strings.Contains(string(log), realRefresh) {
		t.Fatal("resident log leaked the real refresh grant")
	}
}

// credentialSeedHook materializes task <taskID> as an assigned, setup-complete
// standalone task whose FIRST dispatch resolves a sealed Worker plan — the
// same seeding the production completion-seal E2E uses, so the credential test
// reuses a proven path and adds only the projection.
func credentialSeedHook(t *testing.T, taskID int64, setupRun string) func(*fixture, *sql.DB) {
	return func(f *fixture, db *sql.DB) {
		taskRoot := filepath.Join(f.ws, ".mission-control", "tasks", fmt.Sprintf("task-%d", taskID))
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
			t.Fatalf("materialize task store: %v", err)
		}
		if err := os.Chmod(taskRoot, 0o555); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO tasks (id,title,scope,priority,created_at,status,dispatch_retries,origin,worksource,target_ref)
			VALUES (?,'credential projection fixture','task',2,datetime('now'),'proposed',3,'user',?,'main')`, taskID, worksource); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=?`, taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO runs (id,tier,role,worksource,subject) VALUES (?, 'pipeline', 'worker', ?, ?)`, setupRun, worksource, taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource=?, subject=?, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`, setupRun, worksource, taskID); err != nil {
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
			t.Fatalf("register skeleton receipt: %v", err)
		}
		if _, _, err := verbs.RecordFirstTaskSetupClosure(db, setupRun, f.ws, seeded); err != nil {
			t.Fatalf("record closure assignment: %v", err)
		}
		if _, err := db.Exec(`UPDATE runs SET ended_at=datetime('now'), outcome='setup-complete' WHERE id=?`, setupRun); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET run_id=NULL, worksource=NULL, subject=NULL, owner=NULL, acquired_at=NULL, hard_deadline_at=NULL WHERE id=1`); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRefreshGrant(t *testing.T, home string, grant map[string]any) {
	t.Helper()
	dir := filepath.Join(home, "refresh-grants")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%v.json", grant["binding"])), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func setResidentWorkerBehavior(t *testing.T, f *fixture, behaviorPath string, routes []string) {
	t.Helper()
	cfgBytes, err := os.ReadFile(filepath.Join(f.base, "resident.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["roleBehaviors"].(map[string]any)["worker"] = behaviorPath
	cfg["agentRunnerRoutes"] = routes
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.base, "resident.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
