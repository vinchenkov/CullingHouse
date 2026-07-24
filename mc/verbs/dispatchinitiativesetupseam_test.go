package verbs

import (
	"testing"
)

// A promoted (seeded, branch-set) initiative with no setup receipt drives the
// full route-free lane end-to-end under real routing: Dispatch emits the
// initiative-setup effect, claims the lease, opens a Worker-tier run keyed on the
// initiative, and carries the shared-store precreate mount plan — with NO
// harness/brief/route.
func TestDispatchInitiativeSetupLaneClaimsAndCarriesThePrecreatePlan(t *testing.T) {
	ws := ipGitWorkspace(t)
	ipParents(t, ws)
	db := dvSpine(t, func(a *InitArgs) { a.WorkspaceRoot = ws })
	dvExec(t, db, `UPDATE worksources SET kind='repo' WHERE id='ws-test'`)
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (1, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`,
		dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded', branch='mc/initiative-1' WHERE id=1`)

	raw, err := Dispatch(db)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	effect, ok := raw.(map[string]any)
	if !ok || effect["action"] != "initiative-setup" {
		t.Fatalf("effect = %v, want an initiative-setup action", raw)
	}
	if effect["initiative_id"] != int64(1) || effect["subject_id"] != int64(1) {
		t.Fatalf("effect ids = %v", effect)
	}
	if _, hasHarness := effect["harness"]; hasHarness {
		t.Fatalf("route-free effect carries a harness: %v", effect)
	}
	if _, hasBrief := effect["brief"]; hasBrief {
		t.Fatalf("route-free effect carries a brief: %v", effect)
	}
	runID, _ := effect["run_id"].(string)
	if runID == "" {
		t.Fatal("effect carries no run id")
	}
	plan, ok := effect["mount_plan"].(*PrivateDispatchMountPlan)
	if !ok || plan.InitiativePrecreate == nil || plan.InitiativePrecreate.InitiativeID != 1 ||
		plan.InitiativePrecreate.Setup == nil || plan.InitiativePrecreate.Setup.Mode != "fresh" ||
		plan.InitiativePrecreate.Setup.TargetRef != "main" {
		t.Fatalf("mount plan = %+v", effect["mount_plan"])
	}

	// The lease is claimed and the run is a Worker-tier run keyed on the arc.
	var lockRun string
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil {
		t.Fatal(err)
	}
	if lockRun != runID {
		t.Fatalf("lease run = %q, want the setup run %q", lockRun, runID)
	}
	var role, tier, binding string
	var subject int64
	if err := db.QueryRow(`SELECT role, tier, subject, COALESCE(binding,'') FROM runs WHERE id=?`, runID).
		Scan(&role, &tier, &subject, &binding); err != nil {
		t.Fatal(err)
	}
	if role != "worker" || tier != "pipeline" || subject != 1 || binding != "" {
		t.Fatalf("run = role %q tier %q subject %d binding %q, want worker/pipeline/1/empty", role, tier, subject, binding)
	}
}
