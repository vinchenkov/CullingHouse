package verbs

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mc/refusal"
)

// seedDispatchableInitiativeChild builds a real-routed, dispatchable
// initiative-child Worker over the exact on-disk initiative-7 shared store: the
// seeded initiative 7, its seeded plan-reviewed child 10 (the one dispatch
// selects), an archived sibling child 11 carrying a prior ended pipeline run
// (the non-empty ADR-025 D6 producer-absence set — archived, so it never
// competes for selection but still counts as a prior child of the initiative),
// and the D3 receipt vouching the resolved store/worktree roots. dvSpine's
// routing.md maps worker → claude-sdk/minimax, a REAL route, so the D6 marker is
// authored.
func seedDispatchableInitiativeChild(t *testing.T) *sql.DB {
	t.Helper()
	ws := grWorkspace(t)
	store, wt := isBuildAt(t, ws)
	if err := os.Mkdir(filepath.Join(ws, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	db := dvSpine(t, func(a *InitArgs) { a.WorkspaceRoot = ws })
	dvExec(t, db, `UPDATE worksources SET kind='repo' WHERE id='ws-test'`)

	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (7, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`,
		dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded' WHERE id=7`)
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, status, initiative_id,
		priority, created_at, dispatch_retries, origin, worksource, target_ref)
		VALUES (10, 'child', 'task', 'seeded', 7, 1, ?, 3, 'autonomous', 'ws-test', 'main')`,
		dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = 10`)
	// A prior ENDED pipeline run on this child: a genuine prior initiative-family
	// container the D6 fence must confirm absent before the next one is prepared.
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject, ended_at)
		VALUES ('run-10-prior', 'pipeline', 'worker', 'ws-test', 10, datetime('now'))`)

	storeID := maSetupIdentity(t, store)
	wtID := maSetupIdentity(t, wt)
	dvExec(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (7, ?, ?, ?, ?, ?, ?, ?)`,
		storeID.Device, storeID.Inode, storeID.OwnerUID,
		wtID.Device, wtID.Inode, wtID.OwnerUID, strings.Repeat("a", 40))

	if err := os.Chmod(os.Getenv("MC_HOME"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("MC_HOME"), "mount-allowlist"), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return db
}

// End to end: a real-routed initiative-child Worker commit carries the ADR-025 D6
// producer-absence marker in its committed effect, populated with the frozen
// prior-child run set.
func TestDispatchInitiativeChildCommitsProducerAbsenceMarker(t *testing.T) {
	db := seedDispatchableInitiativeChild(t)
	prepared := dfPrepare(t, db, dfRequestID)
	attested, err := dispatchAttest(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatalf("dispatchAttest: %v", err)
	}
	if attested.refusal != nil {
		t.Fatalf("initiative child attest refused: %+v", attested.refusal)
	}
	eff := dfCommit(t, db, prepared, attested)
	if eff["action"] != "spawn" {
		t.Fatalf("effect = %v, want a spawn", eff)
	}
	body, err := json.Marshal(eff["mount_plan"])
	if err != nil {
		t.Fatal(err)
	}
	var plan PrivateDispatchMountPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("committed mount_plan does not decode as the carrier: %v", err)
	}
	if plan.InitiativeChild == nil || plan.InitiativeChild.InitiativeID != 7 {
		t.Fatalf("committed initiative_child marker = %+v, want initiative 7", plan.InitiativeChild)
	}
	if !reflect.DeepEqual(plan.InitiativeChild.PriorChildRuns, []string{"run-10-prior"}) {
		t.Fatalf("committed prior-child runs = %+v, want [run-10-prior]", plan.InitiativeChild.PriorChildRuns)
	}
}

// The commit re-authoring fence: a broker frame whose D6 marker was stripped
// commits nothing — the resident would otherwise launch a child that skips the
// producer-absence fence.
func TestDispatchInitiativeChildCommitStalesOnStrippedMarker(t *testing.T) {
	db := seedDispatchableInitiativeChild(t)
	prepared := dfPrepare(t, db, dfRequestID)
	attested, err := dispatchAttest(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatalf("dispatchAttest: %v", err)
	}
	if attested.mountPlan == nil || attested.mountPlan.InitiativeChild == nil {
		t.Fatalf("expected an initiative-child attestation carrying a marker, got %+v", attested.mountPlan)
	}
	attested.mountPlan.InitiativeChild = nil
	eff := dfCommit(t, db, prepared, attested)
	if eff["action"] != "refused" || eff["code"] != refusal.CodeStale {
		t.Fatalf("stripped-marker commit = %v, want a stale refusal", eff)
	}
	// No NEW run opened (only the seeded prior remains) and the lease is free —
	// the fence refused inertly rather than launching a fence-skipping child.
	if n := dfInt(t, db, `SELECT COUNT(*) FROM runs`); n != 1 {
		t.Fatalf("stale refusal left %d runs, want only the seeded prior", n)
	}
	var lockRun sql.NullString
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id = 1`).Scan(&lockRun); err != nil || lockRun.Valid {
		t.Fatalf("stale refusal held the lease: %v err=%v", lockRun, err)
	}
}
