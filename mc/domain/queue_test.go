// Review-queue suite (contract §1.2 row 5): the global occupancy read, cap
// enforcement at both layers, and the at-cap selection order asserted by
// driving the frozen mc/dispatch.Decide as a black-box oracle over states
// built through domain operations.
package domain_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mc/dispatch"
	"mc/domain"
)

func occupancy(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	mustTx(t, db, func(ctx context.Context, q domain.Q) error {
		var err error
		n, err = domain.Occupancy(ctx, q)
		return err
	})
	return n
}

func TestOccupancy(t *testing.T) {
	t.Run("counts_unarchived_only", func(t *testing.T) {
		db := openSpine(t)
		a := mkTask(t, db, "task", "packaged")
		b := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, a)
		mkPacket(t, db, b)
		if got := occupancy(t, db); got != 2 {
			t.Fatalf("occupancy = %d, want 2", got)
		}
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, a, "drop") // cascade archives the packet
		})
		if got := occupancy(t, db); got != 1 {
			t.Fatalf("occupancy = %d, want 1 after archive", got)
		}
	})

	// Inv. 18: the cap is global across Worksources — one count, one queue.
	t.Run("global_across_worksources", func(t *testing.T) {
		db := openSpine(t)
		mustExec(t, db, `INSERT INTO worksources (id, title, kind) VALUES ('ws2', 'Two', 'repo')`)
		mustExec(t, db, `INSERT INTO worksources (id, title, kind) VALUES ('ws3', 'Three', 'personal')`)
		for _, ws := range []string{"ws", "ws2", "ws3"} {
			res := mustExec(t, db, `INSERT INTO tasks (title, worksource) VALUES ('t', ?)`, ws)
			id, _ := res.LastInsertId()
			for _, s := range walkOrder["packaged"] {
				mustExec(t, db, `UPDATE tasks SET status = ? WHERE id = ?`, s, id)
			}
			mkPacket(t, db, id)
		}
		if got := occupancy(t, db); got != 3 {
			t.Fatalf("occupancy = %d, want 3 (global, Inv. 18)", got)
		}
		// A fourth Worksource's packaging is capped: both layers.
		fourth := mkTask(t, db, "task", "packaged")
		wantCode(t, db, domain.CodeWIPCap, func(ctx context.Context, q domain.Q) error {
			return domain.Birth(ctx, q, fourth, "r.html")
		})
		wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, fourth)
	})
}

// loadProjection reads the stored state into the dispatch projection — the
// oracle's input is the spine rows themselves, scanned mechanically.
func loadProjection(t *testing.T, db *sql.DB) dispatch.Records {
	t.Helper()
	rec := dispatch.Records{}
	rows, err := db.Query(`
		SELECT id, title, scope, initiative_id, priority, created_at, status,
		       blocked, dispatch_retries, decision, decided_at, archived,
		       worksource, branch, verified_sha, target_ref
		FROM tasks`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			tk                          dispatch.Task
			scope, status, createdAt    string
			initiativeID                sql.NullInt64
			blocked, archived           int
			decision, decidedAt         sql.NullString
			branch, verifiedSHA, target sql.NullString
		)
		if err := rows.Scan(&tk.ID, &tk.Title, &scope, &initiativeID, &tk.Priority,
			&createdAt, &status, &blocked, &tk.DispatchRetries, &decision,
			&decidedAt, &archived, &tk.Worksource, &branch, &verifiedSHA, &target); err != nil {
			t.Fatal(err)
		}
		tk.Scope = dispatch.Scope(scope)
		tk.Status = dispatch.Status(status)
		tk.Blocked = blocked == 1
		tk.Archived = archived == 1
		if initiativeID.Valid {
			v := initiativeID.Int64
			tk.InitiativeID = &v
		}
		if tk.CreatedAt, err = domain.ParseSpineTime(createdAt); err != nil {
			t.Fatal(err)
		}
		if decision.Valid {
			tk.Decision = dispatch.TaskDecision(decision.String)
		}
		if decidedAt.Valid {
			d, err := domain.ParseSpineTime(decidedAt.String)
			if err != nil {
				t.Fatal(err)
			}
			tk.DecidedAt = &d
		}
		tk.Branch = branch.String
		tk.VerifiedSHA = verifiedSHA.String
		tk.TargetRef = target.String
		rec.Tasks = append(rec.Tasks, tk)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	prows, err := db.Query(`SELECT task_id, created_at, saturated, archived FROM review_packets`)
	if err != nil {
		t.Fatal(err)
	}
	defer prows.Close()
	for prows.Next() {
		var p dispatch.Packet
		var createdAt string
		var saturated, archived int
		if err := prows.Scan(&p.TaskID, &createdAt, &saturated, &archived); err != nil {
			t.Fatal(err)
		}
		if p.CreatedAt, err = domain.ParseSpineTime(createdAt); err != nil {
			t.Fatal(err)
		}
		p.Saturated = saturated == 1
		p.Archived = archived == 1
		rec.Packets = append(rec.Packets, p)
	}
	if err := prows.Err(); err != nil {
		t.Fatal(err)
	}
	return rec
}

// decide runs the frozen oracle over the stored state with a free lock.
func decide(t *testing.T, db *sql.DB) dispatch.Action {
	t.Helper()
	cfg := dispatch.DefaultConfig()
	cfg.ConsoleHour = 24 // not configured (D-mc-4)
	return dispatch.Decide(loadProjection(t, db), dispatch.Lock{}, cfg,
		dispatch.Clock{Now: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)})
}

// atCap builds a full queue (3 packets) through domain operations and
// returns the packet-holding task ids in birth order.
func atCap(t *testing.T, db *sql.DB) []int64 {
	t.Helper()
	ids := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		id := mkTask(t, db, "task", "packaged")
		if err := birthPacket(t, db, id, "r.html"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestAtCapSelectionOrder(t *testing.T) {
	// (2a) before (2b): an in-flight refinement (re-entered through the
	// domain) is advanced ahead of starting a new one.
	t.Run("advance_in_flight_first", func(t *testing.T) {
		db := openSpine(t)
		ids := atCap(t, db)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Reenter(ctx, q, ids[1], "deepen")
		})
		a := decide(t, db)
		if a.Kind != dispatch.KindSpawn || a.Spawn.Role != dispatch.RoleWorker ||
			a.Spawn.SubjectID == nil || *a.Spawn.SubjectID != ids[1] {
			t.Fatalf("action = %+v, want worker spawn on re-entered task %d (§10 2a)", a, ids[1])
		}
	})

	// (2b): no refinement in flight → the best non-saturated packet spawns
	// the Refiner; order is task priority, then packet age.
	t.Run("start_on_best_packet", func(t *testing.T) {
		db := openSpine(t)
		ids := atCap(t, db)
		mustExec(t, db, `UPDATE tasks SET priority = 0 WHERE id = ?`, ids[2])
		a := decide(t, db)
		if a.Kind != dispatch.KindSpawn || a.Spawn.Role != dispatch.RoleRefiner ||
			a.Spawn.SubjectID == nil || *a.Spawn.SubjectID != ids[2] {
			t.Fatalf("action = %+v, want refiner on P0 task %d (§10 2b)", a, ids[2])
		}
	})

	// (2b) initiative arm: an initiative's arc packet re-enters directly —
	// a pure mutation, no spawn this tick.
	t.Run("initiative_packet_reenters", func(t *testing.T) {
		db := openSpine(t)
		init := mkTask(t, db, "initiative", "seeded")
		var children []int64
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			var err error
			children, err = domain.BirthWave(ctx, q, init, []domain.WaveChild{{Title: "c"}})
			return err
		})
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.Cancel(ctx, q, children[0], "drained for the arc")
		})
		// Walk the drained initiative to packaged and give it the arc packet.
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, init, "worked")
		})
		mustExec(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, init)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			return domain.AdvanceStage(ctx, q, init, "packaged")
		})
		if err := birthPacket(t, db, init, "arc.html"); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `UPDATE tasks SET priority = 0 WHERE id = ?`, init)
		// Fill the rest of the queue.
		for i := 0; i < 2; i++ {
			id := mkTask(t, db, "task", "packaged")
			if err := birthPacket(t, db, id, "r.html"); err != nil {
				t.Fatal(err)
			}
		}
		a := decide(t, db)
		if a.Kind != dispatch.KindReenter || a.Reenter.TaskID != init {
			t.Fatalf("action = %+v, want reenter of initiative %d (§8 move 2)", a, init)
		}
	})

	// Saturated packets leave the candidate set (§8): all saturated → idle.
	t.Run("all_saturated_idles", func(t *testing.T) {
		db := openSpine(t)
		ids := atCap(t, db)
		for _, id := range ids {
			for i := 0; i < 3; i++ {
				mustTx(t, db, func(ctx context.Context, q domain.Q) error {
					return domain.ApplyDeepening(ctx, q, id, false)
				})
			}
		}
		a := decide(t, db)
		if a.Kind != dispatch.KindIdle || a.Idle != dispatch.IdleQueueSaturated {
			t.Fatalf("action = %+v, want idle queue-saturated (§8)", a)
		}
	})

	// Below cap the tick never runs refinement: query (3) selects instead.
	t.Run("below_cap_uses_step_three", func(t *testing.T) {
		db := openSpine(t)
		ids := atCap(t, db)
		mustTx(t, db, func(ctx context.Context, q domain.Q) error {
			_, err := domain.Approve(ctx, q, ids[0]) // branchless → archives, slot frees
			return err
		})
		seeded := mkTask(t, db, "task", "seeded")
		a := decide(t, db)
		if a.Kind != dispatch.KindSpawn || a.Spawn.Role != dispatch.RoleWorker ||
			a.Spawn.SubjectID == nil || *a.Spawn.SubjectID != seeded {
			t.Fatalf("action = %+v, want step-(3) worker on %d with room", a, seeded)
		}
	})
}
