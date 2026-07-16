// Pure-SQL backstop tests for the spine substrate (handoff Part 3, Phase
// 1(a)): no mc binary, no domain layer — raw SQL against a temp spine file,
// asserting commit/abort for every rule the trigger lattice and CHECK
// constraints must enforce standalone.
package substrate_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Schema smoke: WAL discipline, singletons.
// ---------------------------------------------------------------------------

func TestPragmasAndSingletons(t *testing.T) {
	db := openSpine(t)

	if jm := oneStr(t, db, `PRAGMA journal_mode`); jm != "wal" {
		t.Fatalf("journal_mode = %q, want wal", jm)
	}
	if fk := oneInt(t, db, `PRAGMA foreign_keys`); fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}

	t.Run("meta_singleton", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'uuid-1', 1)`)
		wantAbort(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (2, 'uuid-2', 1)`)
	})

	t.Run("lock_singleton", func(t *testing.T) {
		if n := oneInt(t, db, `SELECT COUNT(*) FROM lock`); n != 1 {
			t.Fatalf("lock rows = %d, want the seeded singleton", n)
		}
		if v := oneStr(t, db, `SELECT run_id FROM lock`); v != "<NULL>" {
			t.Fatalf("seeded lock not free: run_id = %q", v)
		}
		wantAbort(t, db, `INSERT INTO lock (id) VALUES (2)`)
		wantAbort(t, db, `DELETE FROM lock`)
		// A claim and a release both commit (CAS mechanics are mc's, shape is ours).
		mustExec(t, db, `UPDATE lock SET run_id = 'r1', owner = 'worker',
			acquired_at = datetime('now'), hard_deadline_at = datetime('now', '+75 minutes')
			WHERE id = 1 AND run_id IS NULL`)
		mustExec(t, db, `UPDATE lock SET run_id = NULL, owner = NULL, subject = NULL,
			acquired_at = NULL, last_heartbeat_at = NULL, hard_deadline_at = NULL WHERE id = 1`)

		// A free lock carries no run residue — worksource included (NOTE(P1.18)).
		mustExec(t, db, `UPDATE lock SET run_id = 'r2', owner = 'worker', worksource = 'ws',
			acquired_at = datetime('now'), hard_deadline_at = datetime('now', '+75 minutes')
			WHERE id = 1 AND run_id IS NULL`)
		wantAbort(t, db, `UPDATE lock SET run_id = NULL, owner = NULL, subject = NULL,
			acquired_at = NULL, last_heartbeat_at = NULL, hard_deadline_at = NULL WHERE id = 1`)
		mustExec(t, db, `UPDATE lock SET run_id = NULL, owner = NULL, subject = NULL, worksource = NULL,
			acquired_at = NULL, last_heartbeat_at = NULL, hard_deadline_at = NULL WHERE id = 1`)
		if v := oneStr(t, db, `SELECT worksource FROM lock`); v != "<NULL>" {
			t.Fatalf("released lock keeps worksource residue: %q", v)
		}
		// And a claimed worksource must be a real one (FK, like runs.worksource).
		wantAbort(t, db, `UPDATE lock SET run_id = 'r3', owner = 'worker', worksource = 'no-such-ws',
			acquired_at = datetime('now'), hard_deadline_at = datetime('now', '+75 minutes')
			WHERE id = 1 AND run_id IS NULL`)
	})
}

func TestWorksourceArchiveIsHistorical(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO worksources (id, title, kind) VALUES ('history', 'History', 'repo')`)
	mustExec(t, db, `UPDATE worksources SET status='paused' WHERE id='history'`)
	mustExec(t, db, `UPDATE worksources SET status='archived' WHERE id='history'`)
	wantAbort(t, db, `UPDATE worksources SET status='active' WHERE id='history'`)
	wantAbort(t, db, `DELETE FROM worksources WHERE id='history'`)
}

// ---------------------------------------------------------------------------
// The full state-transition matrix, both scopes (spec §6, §6.1): every legal
// edge commits, every illegal edge aborts — all 5x5 status pairs.
// ---------------------------------------------------------------------------

func TestTransitionMatrix(t *testing.T) {
	legal := map[string]bool{
		"proposed>seeded":   true, // promote
		"seeded>worked":     true, // work / plan / done-declaration
		"worked>verified":   true, // verify pass
		"worked>seeded":     true, // correction rally
		"verified>packaged": true, // package
		"packaged>seeded":   true, // refinement / operator revise
	}
	// Three populations: standalone tasks, initiatives, and wave children —
	// the same trigger must enforce the same matrix for all of them.
	for _, pop := range []string{"task", "initiative", "child"} {
		db := openSpine(t)
		for _, from := range statuses {
			for _, to := range statuses {
				t.Run(fmt.Sprintf("%s/%s->%s", pop, from, to), func(t *testing.T) {
					var id int64
					if pop == "child" {
						parent := mkTask(t, db, "initiative", "seeded")
						if from == "proposed" {
							// Unreachable population cell: a child cannot exist at
							// 'proposed' at all — born seeded, and seeded never goes
							// back (§6.1). Pin the unreachability instead.
							wantAbort(t, db,
								`INSERT INTO tasks (title, scope, status, initiative_id, worksource)
								 VALUES ('c', 'task', 'proposed', ?, 'ws')`, parent)
							return
						}
						id = mkChild(t, db, parent)
						walkChild(t, db, id, from)
					} else {
						id = mkTask(t, db, pop, from)
					}
					if from == "packaged" && to == "seeded" {
						mkPacket(t, db, id)
					}
					_, err := db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, to, id)
					switch {
					case from == to:
						// Self-assignment is a no-op, not a transition (NOTE(P1.1)).
						if err != nil {
							t.Fatalf("no-op self-assignment aborted: %v", err)
						}
					case legal[from+">"+to]:
						if err != nil {
							t.Fatalf("legal edge %s->%s aborted: %v", from, to, err)
						}
						if got := taskStr(t, db, id, "status"); got != to {
							t.Fatalf("status = %q after legal edge, want %q", got, to)
						}
					default:
						if err == nil {
							t.Fatalf("illegal edge %s->%s committed", from, to)
						}
						if got := taskStr(t, db, id, "status"); got != from {
							t.Fatalf("status = %q after aborted edge, want unchanged %q", got, from)
						}
					}
				})
			}
		}
	}
}

// Birth cases: ordinary rows (both scopes) are born only 'proposed'; wave
// children only 'seeded' (§6, §6.1).
func TestBirthStatusMatrix(t *testing.T) {
	db := openSpine(t)
	for _, scope := range []string{"task", "initiative"} {
		for _, st := range statuses {
			t.Run(fmt.Sprintf("%s/born-%s", scope, st), func(t *testing.T) {
				q := `INSERT INTO tasks (title, scope, status, worksource) VALUES ('b', ?, ?, 'ws')`
				if st == "proposed" {
					mustExec(t, db, q, scope, st)
				} else {
					wantAbort(t, db, q, scope, st)
				}
			})
		}
	}
	parent := mkTask(t, db, "initiative", "seeded")
	for _, st := range statuses {
		t.Run("child/born-"+st, func(t *testing.T) {
			q := `INSERT INTO tasks (title, scope, status, initiative_id, worksource) VALUES ('c', 'task', ?, ?, 'ws')`
			if st == "seeded" {
				mustExec(t, db, q, st, parent)
			} else {
				wantAbort(t, db, q, st, parent)
			}
		})
	}

	// Nothing is born decided or archived (NOTE(P1.2)): these reach ONLY the
	// birth trigger's terminal-marks arm — every table CHECK is satisfied
	// (decision/decided_at paired, archive-carries-decision holds).
	t.Run("task/born-decided", func(t *testing.T) {
		wantAbort(t, db,
			`INSERT INTO tasks (title, worksource, decision, decided_at)
			 VALUES ('b', 'ws', 'rejected', datetime('now'))`)
	})
	t.Run("task/born-decided-archived", func(t *testing.T) {
		wantAbort(t, db,
			`INSERT INTO tasks (title, worksource, decision, decided_at, archived)
			 VALUES ('b', 'ws', 'cancelled', datetime('now'), 1)`)
	})
	t.Run("child/born-decided", func(t *testing.T) {
		wantAbort(t, db,
			`INSERT INTO tasks (title, scope, status, initiative_id, worksource, decision, decided_at)
			 VALUES ('c', 'task', 'seeded', ?, 'ws', 'rejected', datetime('now'))`, parent)
	})
	t.Run("child/born-decided-archived", func(t *testing.T) {
		wantAbort(t, db,
			`INSERT INTO tasks (title, scope, status, initiative_id, worksource, decision, decided_at, archived)
			 VALUES ('c', 'task', 'seeded', ?, 'ws', 'cancelled', datetime('now'), 1)`, parent)
	})
}

// Terminal cases: an archived (decided) row never transitions again.
func TestArchivedRowsAreTerminal(t *testing.T) {
	db := openSpine(t)

	t.Run("rejected_proposal", func(t *testing.T) {
		id := mkTask(t, db, "task", "proposed")
		mustExec(t, db, `UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1 WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
	})

	t.Run("cancelled_at_worked", func(t *testing.T) {
		id := mkTask(t, db, "task", "worked")
		cancelTask(t, db, id)
		wantAbort(t, db, `UPDATE tasks SET status = 'verified' WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
	})

	t.Run("approved_and_landed", func(t *testing.T) {
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustExec(t, db, `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET archived = 1 WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
	})

	// Terminal bookkeeping is itself terminal: an archived row's decision
	// record never rewrites — it is the leverage ledger (§5).
	t.Run("archived_decision_frozen", func(t *testing.T) {
		id := mkTask(t, db, "task", "proposed")
		mustExec(t, db, `UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1 WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET decision = 'cancelled' WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET decided_at = datetime('now', '+1 hour') WHERE id = ?`, id)
		if got := taskStr(t, db, id, "decision"); got != "rejected" {
			t.Fatalf("archived decision rewritten to %q", got)
		}
	})

	// No resurrection: un-archiving is refused outright (§6 "archived rows
	// are invisible to everything"; no spec flow un-archives a task) — it
	// would re-enter §10 dispatch and, on a landed row, re-derive
	// landing-pending for an already-merged branch (NOTE(P1.4)).
	t.Run("unarchive_refused", func(t *testing.T) {
		id := mkTask(t, db, "task", "proposed")
		mustExec(t, db, `UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1 WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET archived = 0 WHERE id = ?`, id)
	})
	t.Run("landed_unarchive_refused", func(t *testing.T) {
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		mustExec(t, db, `UPDATE tasks SET branch = 'mc/task-x', verified_sha = 'abc', target_ref = 'main' WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id)
		mustExec(t, db, `UPDATE tasks SET archived = 1 WHERE id = ?`, id) // the §7 landing-success write
		wantAbort(t, db, `UPDATE tasks SET archived = 0 WHERE id = ?`, id)
	})

	// A decided-but-unarchived row (the transient two-statement window) never
	// transitions either: rejected/cancelled rows only archive (§6).
	t.Run("decided_unarchived_never_moves", func(t *testing.T) {
		id := mkTask(t, db, "task", "proposed")
		mustExec(t, db, `UPDATE tasks SET decision = 'rejected', decided_at = datetime('now') WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
		if got := taskStr(t, db, id, "status"); got != "proposed" {
			t.Fatalf("rejected row moved to %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// correction_count bounds (spec §4: CHECK 0-3).
// ---------------------------------------------------------------------------

func TestCorrectionCountBounds(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "seeded")
	for v := 0; v <= 3; v++ {
		mustExec(t, db, `UPDATE tasks SET correction_count = ? WHERE id = ?`, v, id)
	}
	wantAbort(t, db, `UPDATE tasks SET correction_count = 4 WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET correction_count = -1 WHERE id = ?`, id)
	wantAbort(t, db, `INSERT INTO tasks (title, scope, worksource, correction_count) VALUES ('x', 'task', 'ws', 4)`)
}

// ---------------------------------------------------------------------------
// Blocked requires a reason; unblocking clears it (spec §4, §6).
// ---------------------------------------------------------------------------

func TestBlockedNeedsReasonAndUnblockClears(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "seeded")

	wantAbort(t, db, `UPDATE tasks SET blocked = 1 WHERE id = ?`, id)
	mustExec(t, db, `UPDATE tasks SET blocked = 1, blocked_reason = 'needs operator input' WHERE id = ?`, id)
	// Dropping the reason while still blocked aborts.
	wantAbort(t, db, `UPDATE tasks SET blocked_reason = NULL WHERE id = ?`, id)
	// Unblock clears the reason.
	mustExec(t, db, `UPDATE tasks SET blocked = 0 WHERE id = ?`, id)
	if got := taskStr(t, db, id, "blocked_reason"); got != "<NULL>" {
		t.Fatalf("blocked_reason = %q after unblock, want cleared", got)
	}
}

// ---------------------------------------------------------------------------
// Archive needs a decision; decision and decided_at travel together (§4, §6).
// ---------------------------------------------------------------------------

func TestArchiveNeedsDecisionAndTimestampPairing(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "proposed")

	wantAbort(t, db, `UPDATE tasks SET archived = 1 WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET decision = 'cancelled' WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET decided_at = datetime('now') WHERE id = ?`, id)

	mustExec(t, db, `UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now') WHERE id = ?`, id)
	// Un-pairing after the fact aborts too.
	wantAbort(t, db, `UPDATE tasks SET decided_at = NULL WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET decision = NULL WHERE id = ?`, id)
	mustExec(t, db, `UPDATE tasks SET archived = 1 WHERE id = ?`, id)
}

// ---------------------------------------------------------------------------
// Approve only from packaged (§4, §6, §7).
// ---------------------------------------------------------------------------

func TestApproveOnlyFromPackaged(t *testing.T) {
	db := openSpine(t)
	for _, scope := range []string{"task", "initiative"} { // §6.1: the arc packet approves the same way
		for _, st := range statuses {
			t.Run(scope+"/"+st, func(t *testing.T) {
				id := mkTask(t, db, scope, st)
				q := `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`
				if st == "packaged" {
					mkPacket(t, db, id)
					mustExec(t, db, q, id)
					// A landing-pending row (approved, unarchived, at packaged)
					// can never be pulled back into the pipeline: the refinement
					// edge packaged -> seeded is refused mid-landing (§7).
					wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
					if got := taskStr(t, db, id, "status"); got != "packaged" {
						t.Fatalf("landing-pending row moved to %q", got)
					}
				} else {
					wantAbort(t, db, q, id)
				}
			})
		}
	}

	t.Run("live_packet_required", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		wantAbort(t, db,
			`UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id)
		wantAbort(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
	})

	for _, tc := range []struct {
		name  string
		setup string
	}{
		{name: "missing_verified_sha", setup: `UPDATE tasks SET branch = 'mc/task-x', target_ref = 'main' WHERE id = ?`},
		{name: "missing_target_ref", setup: `UPDATE tasks SET branch = 'mc/task-x', verified_sha = 'abc', target_ref = NULL WHERE id = ?`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openSpine(t)
			id := mkTask(t, db, "task", "packaged")
			mkPacket(t, db, id)
			mustExec(t, db, tc.setup, id)
			wantAbort(t, db,
				`UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id)
		})
	}
}

// ---------------------------------------------------------------------------
// No initiative nesting; children only into a live, still-seeded initiative
// (§4, §5, §6.1).
// ---------------------------------------------------------------------------

func TestNoInitiativeNesting(t *testing.T) {
	db := openSpine(t)
	parent := mkTask(t, db, "initiative", "seeded")

	// A child of an initiative cannot itself be scope = 'initiative'.
	wantAbort(t, db,
		`INSERT INTO tasks (title, scope, status, initiative_id, worksource) VALUES ('n', 'initiative', 'seeded', ?, 'ws')`,
		parent)
	// Nor can an existing child be re-scoped into one.
	child := mkChild(t, db, parent)
	wantAbort(t, db, `UPDATE tasks SET scope = 'initiative' WHERE id = ?`, child)

	// Children only under a scope-initiative parent…
	taskParent := mkTask(t, db, "task", "seeded")
	wantAbort(t, db,
		`INSERT INTO tasks (title, scope, status, initiative_id, worksource) VALUES ('c', 'task', 'seeded', ?, 'ws')`,
		taskParent)
	// …that is still seeded…
	for _, st := range []string{"proposed", "worked", "verified", "packaged"} {
		p := mkTask(t, db, "initiative", st)
		wantAbort(t, db,
			`INSERT INTO tasks (title, scope, status, initiative_id, worksource) VALUES ('c', 'task', 'seeded', ?, 'ws')`,
			p)
	}
	// …and still live.
	dead := mkTask(t, db, "initiative", "seeded")
	cancelTask(t, db, dead)
	wantAbort(t, db,
		`INSERT INTO tasks (title, scope, status, initiative_id, worksource) VALUES ('c', 'task', 'seeded', ?, 'ws')`,
		dead)
	// A nonexistent parent refuses too.
	wantAbort(t, db,
		`INSERT INTO tasks (title, scope, status, initiative_id, worksource) VALUES ('c', 'task', 'seeded', 999999, 'ws')`)
}

func TestWaveChildBirthRules(t *testing.T) {
	db := openSpine(t)
	parent := mkTask(t, db, "initiative", "seeded")

	child := mkChild(t, db, parent)
	if got := taskStr(t, db, child, "status"); got != "seeded" {
		t.Fatalf("wave child born %q, want seeded", got)
	}
	if got := taskInt(t, db, child, "initiative_id"); got != parent {
		t.Fatalf("wave child initiative_id = %d, want %d", got, parent)
	}
	// Re-parenting a child later is refused (identity immutable, NOTE(P1.5)).
	other := mkTask(t, db, "initiative", "seeded")
	wantAbort(t, db, `UPDATE tasks SET initiative_id = ? WHERE id = ?`, other, child)
	// Adopting a standalone task into an initiative after birth is refused too.
	loose := mkTask(t, db, "task", "seeded")
	wantAbort(t, db, `UPDATE tasks SET initiative_id = ? WHERE id = ?`, parent, loose)
}

// ---------------------------------------------------------------------------
// Strict drain (§6.1): seeded → worked (the done-declaration) refused while
// any child is unarchived.
// ---------------------------------------------------------------------------

func TestStrictDrain(t *testing.T) {
	db := openSpine(t)

	ini := mkTask(t, db, "initiative", "seeded")
	c1 := mkChild(t, db, ini)
	c2 := mkChild(t, db, ini)

	wantAbort(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, ini)
	cancelTask(t, db, c1)
	// One child still open — still refused, even though c1 is archived.
	wantAbort(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, ini)
	// A decided-but-unarchived child still counts as open: §6.1 requires every
	// child operator-decided AND archived, and the window is real (an approved
	// child sits decided-but-unarchived until landing succeeds). archived is
	// the drain test.
	mustExec(t, db, `UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now') WHERE id = ?`, c2)
	wantAbort(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, ini)
	// Archiving the decided child completes the drain; the declare commits.
	mustExec(t, db, `UPDATE tasks SET archived = 1 WHERE id = ?`, c2)
	mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, ini)
	if got := taskStr(t, db, ini, "status"); got != "worked" {
		t.Fatalf("drained initiative status = %q, want worked", got)
	}
}

// ---------------------------------------------------------------------------
// Blocked-child propagation + auto-clear (§6.1).
// ---------------------------------------------------------------------------

func TestBlockedChildPropagationAndAutoClear(t *testing.T) {
	t.Run("propagates_and_clears_on_unblock", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c1 := mkChild(t, db, ini)
		c2 := mkChild(t, db, ini)

		mustExec(t, db, `UPDATE tasks SET blocked = 1, blocked_reason = 'stuck on schema question' WHERE id = ?`, c1)
		if got := taskInt(t, db, ini, "blocked"); got != 1 {
			t.Fatalf("initiative not blocked by blocked child")
		}
		want := fmt.Sprintf("blocked child #%d", c1)
		if got := taskStr(t, db, ini, "blocked_reason"); got != want {
			t.Fatalf("initiative blocked_reason = %q, want %q", got, want)
		}

		// A second blocked child leaves the existing propagated block in place.
		mustExec(t, db, `UPDATE tasks SET blocked = 1, blocked_reason = 'also stuck' WHERE id = ?`, c2)
		if got := taskStr(t, db, ini, "blocked_reason"); got != want {
			t.Fatalf("initiative blocked_reason = %q after second block, want unchanged %q", got, want)
		}

		// Unblocking one child while another is still blocked keeps the block.
		mustExec(t, db, `UPDATE tasks SET blocked = 0 WHERE id = ?`, c1)
		if got := taskInt(t, db, ini, "blocked"); got != 1 {
			t.Fatalf("initiative auto-cleared while a child is still blocked")
		}
		// Unblocking the last blocked child auto-clears.
		mustExec(t, db, `UPDATE tasks SET blocked = 0 WHERE id = ?`, c2)
		if got := taskInt(t, db, ini, "blocked"); got != 0 {
			t.Fatalf("initiative still blocked after last blocked child resolved")
		}
		if got := taskStr(t, db, ini, "blocked_reason"); got != "<NULL>" {
			t.Fatalf("initiative blocked_reason = %q after auto-clear, want NULL", got)
		}
	})

	t.Run("clears_when_last_blocked_child_cancelled", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c := mkChild(t, db, ini)
		mustExec(t, db, `UPDATE tasks SET blocked = 1, blocked_reason = 'stuck' WHERE id = ?`, c)
		if got := taskInt(t, db, ini, "blocked"); got != 1 {
			t.Fatalf("initiative not blocked")
		}
		cancelTask(t, db, c) // child stays blocked = 1 but leaves the open set
		if got := taskInt(t, db, ini, "blocked"); got != 0 {
			t.Fatalf("initiative still blocked after last blocked child cancelled")
		}
	})

	t.Run("operator_set_block_never_auto_clears", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c := mkChild(t, db, ini)
		mustExec(t, db, `UPDATE tasks SET blocked = 1, blocked_reason = 'waiting on operator decision' WHERE id = ?`, ini)
		// A child block does not overwrite the operator's reason.
		mustExec(t, db, `UPDATE tasks SET blocked = 1, blocked_reason = 'stuck' WHERE id = ?`, c)
		if got := taskStr(t, db, ini, "blocked_reason"); got != "waiting on operator decision" {
			t.Fatalf("operator reason overwritten: %q", got)
		}
		// Resolving the child never lifts the operator's block.
		mustExec(t, db, `UPDATE tasks SET blocked = 0 WHERE id = ?`, c)
		if got := taskInt(t, db, ini, "blocked"); got != 1 {
			t.Fatalf("operator-set block auto-cleared")
		}
		if got := taskStr(t, db, ini, "blocked_reason"); got != "waiting on operator decision" {
			t.Fatalf("operator reason lost: %q", got)
		}
	})

	t.Run("child_born_blocked_propagates", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		res := mustExec(t, db,
			`INSERT INTO tasks (title, scope, status, initiative_id, worksource, blocked, blocked_reason)
			 VALUES ('c', 'task', 'seeded', ?, 'ws', 1, 'born stuck')`, ini)
		id, _ := res.LastInsertId()
		want := fmt.Sprintf("blocked child #%d", id)
		if got := taskStr(t, db, ini, "blocked_reason"); got != want {
			t.Fatalf("initiative blocked_reason = %q, want %q", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Cascade archive (§6.1): cancelling an initiative cancels + archives open
// children, and their packets with them.
// ---------------------------------------------------------------------------

func TestCascadeArchive(t *testing.T) {
	t.Run("open_children_and_their_packets", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		c1 := mkChild(t, db, ini)
		c2 := mkChild(t, db, ini)
		walkChild(t, db, c1, "packaged")
		mkPacket(t, db, c1)

		cancelTask(t, db, ini)

		for _, c := range []int64{c1, c2} {
			if got := taskStr(t, db, c, "decision"); got != "cancelled" {
				t.Fatalf("child %d decision = %q, want cancelled", c, got)
			}
			if got := taskInt(t, db, c, "archived"); got != 1 {
				t.Fatalf("child %d not archived by cascade", c)
			}
			if got := taskStr(t, db, c, "decided_at"); got == "<NULL>" {
				t.Fatalf("child %d cancelled without decided_at", c)
			}
		}
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, c1); got != 1 {
			t.Fatalf("open child's packet not archived by cascade")
		}
	})

	t.Run("initiatives_own_packet_archives_too", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "packaged") // drained, declared, verified, packaged
		mkPacket(t, db, ini)
		cancelTask(t, db, ini)
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, ini); got != 1 {
			t.Fatalf("cancelled initiative's arc packet not archived")
		}
	})

	t.Run("plain_task_archive_archives_its_packet", func(t *testing.T) {
		db := openSpine(t)
		id := mkTask(t, db, "task", "packaged")
		mkPacket(t, db, id)
		cancelTask(t, db, id)
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("archived task's packet not archived")
		}
	})

	t.Run("open_landing_pending_child_becomes_cancelled", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		child := mkChild(t, db, ini)
		walkChild(t, db, child, "packaged")
		mkPacket(t, db, child)
		mustExec(t, db, `UPDATE tasks SET branch = 'mc/child', verified_sha = 'abc', target_ref = 'main' WHERE id = ?`, child)
		mustExec(t, db, `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, child)

		cancelTask(t, db, ini)
		if got := taskStr(t, db, child, "decision"); got != "cancelled" {
			t.Fatalf("open child decision = %q, want cancelled", got)
		}
		if got := taskInt(t, db, child, "archived"); got != 1 {
			t.Fatalf("open child not archived")
		}
		if got := oneInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, child); got != 1 {
			t.Fatalf("open child packet not archived")
		}
	})
}

// ---------------------------------------------------------------------------
// Review WIP cap (Inv. 18): max 3 unarchived packets, enforced at birth;
// packet born only from a packaged task; exactly one per task (Inv. 11).
// ---------------------------------------------------------------------------

func TestReviewWipCap(t *testing.T) {
	db := openSpine(t)

	var tasks []int64
	for i := 0; i < 4; i++ {
		tasks = append(tasks, mkTask(t, db, "task", "packaged"))
	}
	for _, id := range tasks[:3] {
		mkPacket(t, db, id)
	}
	// The fourth packet aborts at birth.
	wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, tasks[3])

	// Only the owning task's terminal decision may archive its packet and
	// free the slot (Inv. 11). A packet can never be resurrected, even while
	// the queue is below cap.
	cancelTask(t, db, tasks[0])
	wantAbort(t, db, `UPDATE review_packets SET archived = 0 WHERE task_id = ?`, tasks[0])

	// Directly archiving an undecided task's live packet would evade
	// backpressure, so the substrate refuses it.
	wantAbort(t, db, `UPDATE review_packets SET archived = 1 WHERE task_id = ?`, tasks[1])

	// The valid task decision freed one slot; the fourth birth now commits.
	mkPacket(t, db, tasks[3])

	// Exactly one packet per task, for life — even the archived one blocks a second.
	wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, tasks[1])
	wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, tasks[0])
}

func TestPacketBirthRequiresPackagedTask(t *testing.T) {
	db := openSpine(t)
	for _, st := range []string{"proposed", "seeded", "worked", "verified"} {
		t.Run(st, func(t *testing.T) {
			id := mkTask(t, db, "task", st)
			wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, id)
		})
	}
	t.Run("archived_packaged", func(t *testing.T) {
		id := mkTask(t, db, "task", "packaged")
		cancelTask(t, db, id)
		wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (?)`, id)
	})
	t.Run("no_such_task", func(t *testing.T) {
		wantAbort(t, db, `INSERT INTO review_packets (task_id) VALUES (999999)`)
	})
	t.Run("born_archived", func(t *testing.T) {
		// Packets are born live, into the queue (Inv. 11): a born-archived
		// packet would dodge the WIP cap while consuming the task's
		// one-packet-for-life slot.
		id := mkTask(t, db, "task", "packaged")
		wantAbort(t, db, `INSERT INTO review_packets (task_id, archived) VALUES (?, 1)`, id)
	})
}

// ---------------------------------------------------------------------------
// Saturation (§8): refine_streak >= 3 saturates via trigger; saturated is
// computed, never hand-set.
// ---------------------------------------------------------------------------

func TestSaturationTrigger(t *testing.T) {
	db := openSpine(t)

	t1 := mkTask(t, db, "task", "packaged")
	mkPacket(t, db, t1)
	for _, streak := range []int{1, 2} {
		mustExec(t, db, `UPDATE review_packets SET refine_streak = ? WHERE task_id = ?`, streak, t1)
		if got := oneInt(t, db, `SELECT saturated FROM review_packets WHERE task_id = ?`, t1); got != 0 {
			t.Fatalf("saturated at streak %d, want only at 3", streak)
		}
	}
	mustExec(t, db, `UPDATE review_packets SET refine_streak = 3 WHERE task_id = ?`, t1)
	if got := oneInt(t, db, `SELECT saturated FROM review_packets WHERE task_id = ?`, t1); got != 1 {
		t.Fatalf("not saturated at streak 3")
	}
	// Hand-clearing a genuinely saturated packet aborts.
	wantAbort(t, db, `UPDATE review_packets SET saturated = 0 WHERE task_id = ?`, t1)
	// The two-step side door aborts at step one: a saturated packet's streak
	// never decreases (refinement never dispatches on saturated = 1, so no
	// genuine-deepening reset can occur there, §8/§10 step 2b)…
	wantAbort(t, db, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, t1)
	wantAbort(t, db, `UPDATE review_packets SET refine_streak = 2 WHERE task_id = ?`, t1)
	// …so the direct clear stays refused with the streak intact.
	wantAbort(t, db, `UPDATE review_packets SET saturated = 0 WHERE task_id = ?`, t1)
	if got := oneInt(t, db, `SELECT refine_streak FROM review_packets WHERE task_id = ?`, t1); got != 3 {
		t.Fatalf("saturated packet's streak lowered to %d", got)
	}
	// Operator revise makes a recovery possible. Once that same task reaches
	// worked, the genuine verdict's streak reset also recomputes saturation.
	mustExec(t, db, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, t1)
	mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, t1)
	mustExec(t, db, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, t1)
	if got := oneInt(t, db, `SELECT saturated FROM review_packets WHERE task_id = ?`, t1); got != 0 {
		t.Fatalf("operator-revised genuine recovery stayed saturated = %d", got)
	}

	// Hand-setting saturated on a fresh packet aborts.
	t2 := mkTask(t, db, "task", "packaged")
	mkPacket(t, db, t2)
	wantAbort(t, db, `UPDATE review_packets SET saturated = 1 WHERE task_id = ?`, t2)

	// Birth cannot smuggle it in either.
	t3 := mkTask(t, db, "task", "packaged")
	wantAbort(t, db, `INSERT INTO review_packets (task_id, saturated) VALUES (?, 1)`, t3)

	// A genuine deepening resets the streak on an unsaturated packet.
	mustExec(t, db, `UPDATE review_packets SET refine_streak = 2 WHERE task_id = ?`, t2)
	mustExec(t, db, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, t2)
	if got := oneInt(t, db, `SELECT saturated FROM review_packets WHERE task_id = ?`, t2); got != 0 {
		t.Fatalf("unsaturated packet saturated by a streak reset")
	}
}

// ---------------------------------------------------------------------------
// Activity is append-only (Inv. 7): UPDATE and DELETE both abort.
// ---------------------------------------------------------------------------

func TestActivityAppendOnly(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO activity (actor, kind, detail) VALUES ('mc', 'daily.briefing', 'x')`)
	wantAbort(t, db, `UPDATE activity SET detail = 'edited'`)
	wantAbort(t, db, `UPDATE activity SET detail = 'edited' WHERE kind = 'daily.briefing'`)
	wantAbort(t, db, `DELETE FROM activity`)
	wantAbort(t, db, `DELETE FROM activity WHERE kind = 'daily.briefing'`)
	if n := oneInt(t, db, `SELECT COUNT(*) FROM activity`); n != 1 {
		t.Fatalf("activity rows = %d, want the 1 appended row intact", n)
	}
}

// ADR-016 Decision 2: every mutation-producing dispatch action has one
// database-backed replay fence. Commit-side actions use a 32-byte action hash;
// prepare-side actions pair a command-scoped request id with the exact final
// JSON result. The spine rejects malformed, half-paired, or duplicate receipts
// even if the Go layer regresses.
func TestDispatchActivityReceiptBackstops(t *testing.T) {
	assertActivityReceiptBackstops(t, openSpine(t))
}

// assertActivityReceiptBackstops states D2's activity-side storage contract
// once. A migrated spine is held to it byte-for-byte alongside a fresh one
// (see TestMigrateV1ToCurrent): a fence the migration fails to carry over is
// a fence that does not exist on any real deployment.
func assertActivityReceiptBackstops(t *testing.T, db *sql.DB) {
	t.Helper()
	dispatchKey := strings.Repeat("a", 64)
	requestID := strings.Repeat("b", 16)
	result := `{"action":"health","code":"health.runtime_unavailable"}`

	mustExec(t, db, `
		INSERT INTO activity (actor, kind, detail, dispatch_key)
		VALUES ('mc', 'dispatch.health', '{}', ?)`, dispatchKey)
	wantAbort(t, db, `
		INSERT INTO activity (actor, kind, detail, dispatch_key)
		VALUES ('mc', 'dispatch.health', '{}', ?)`, dispatchKey)

	// nulTail and nulPad are the two halves of the same hole: length() and
	// GLOB both stop at the first NUL, so a character test misses the trailing
	// bytes and a byte test never shows the NUL to GLOB. Either shape would
	// store a key whose own receipt lookup cannot find it, so a replayed commit
	// would duplicate the activity, its consequence, and its outbox fan-out.
	for name, key := range map[string]string{
		"short":     strings.Repeat("a", 63),
		"uppercase": strings.Repeat("A", 64),
		"non_hex":   strings.Repeat("a", 63) + "g",
		"nul_tail":  strings.Repeat("a", 64) + "\x00EVIL",
		"nul_pad":   strings.Repeat("a", 63) + "\x00",
	} {
		t.Run("dispatch_key_"+name, func(t *testing.T) {
			wantAbort(t, db, `
				INSERT INTO activity (actor, kind, detail, dispatch_key)
				VALUES ('mc', 'dispatch.health', '{}', ?)`, key)
		})
	}

	// The prepare-side receipt is all-or-nothing and stores an object-shaped,
	// bounded final result. A retry can therefore return the original bytes
	// without re-entering selection.
	wantAbort(t, db, `
		INSERT INTO activity (actor, kind, dispatch_request_id)
		VALUES ('mc', 'dispatch.reap', ?)`, requestID)
	wantAbort(t, db, `
		INSERT INTO activity (actor, kind, dispatch_result)
		VALUES ('mc', 'dispatch.reap', ?)`, result)
	mustExec(t, db, `
		INSERT INTO activity (actor, kind, dispatch_request_id, dispatch_result)
		VALUES ('mc', 'dispatch.reap', ?, ?)`, requestID, result)
	wantAbort(t, db, `
		INSERT INTO activity (actor, kind, dispatch_request_id, dispatch_result)
		VALUES ('mc', 'dispatch.reap', ?, ?)`, requestID, result)

	for name, args := range map[string][]any{
		"request_short":     {strings.Repeat("b", 15), result},
		"request_upper":     {strings.Repeat("B", 16), result},
		"request_nonhex":    {strings.Repeat("b", 15) + "z", result},
		"request_nul_tail":  {strings.Repeat("b", 16) + "\x00X", result},
		"request_nul_pad":   {strings.Repeat("b", 15) + "\x00", result},
		"result_not_json":   {strings.Repeat("c", 16), "not-json"},
		"result_not_object": {strings.Repeat("d", 16), `[]`},
		"result_oversize":   {strings.Repeat("e", 16), `{"x":"` + strings.Repeat("x", 65536) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			wantAbort(t, db, `
				INSERT INTO activity (actor, kind, dispatch_request_id, dispatch_result)
				VALUES ('mc', 'dispatch.reap', ?, ?)`, args...)
		})
	}

	// BLOB forgeries (the v4 typeof fence): TEXT affinity never converts a
	// BLOB, length(blob) counts bytes, and GLOB reads only to the first NUL —
	// so both a fully-hex BLOB (a UNIQUE value distinct from its TEXT twin,
	// which the replay lookup can never find) and a NUL-embedded one pass
	// every column CHECK. The columns shipped in frozen v1→v2, so the fence
	// is a trigger, not a CHECK.
	for name, args := range map[string][]any{
		"key_hex_blob":     {[]byte(strings.Repeat("a", 64)), nil, nil},
		"key_nul_blob":     {append([]byte(strings.Repeat("a", 63)), 0x00), nil, nil},
		"request_hex_blob": {nil, []byte(strings.Repeat("b", 16)), result},
		"request_nul_blob": {nil, append([]byte(strings.Repeat("b", 15)), 0x00), result},
		"result_blob":      {nil, strings.Repeat("f", 16), []byte(`{"forged":true}`)},
	} {
		t.Run("blob_"+name, func(t *testing.T) {
			wantAbort(t, db, `
				INSERT INTO activity (actor, kind, detail, dispatch_key, dispatch_request_id, dispatch_result)
				VALUES ('mc', 'dispatch.health', '{}', ?, ?, ?)`, args...)
		})
	}
}

// ---------------------------------------------------------------------------
// stage_rank generation (§5, §10): status → rank, packaged = 0, unwritable.
// ---------------------------------------------------------------------------

func TestStageRankGeneration(t *testing.T) {
	db := openSpine(t)
	want := map[string]int64{
		"packaged": 0,
		"proposed": 1,
		"seeded":   2,
		"worked":   3,
		"verified": 4,
	}
	for st, rank := range want {
		t.Run(st, func(t *testing.T) {
			id := mkTask(t, db, "task", st)
			if got := taskInt(t, db, id, "stage_rank"); got != rank {
				t.Fatalf("stage_rank(%s) = %d, want %d", st, got, rank)
			}
		})
	}
	t.Run("generated_never_written", func(t *testing.T) {
		id := mkTask(t, db, "task", "seeded")
		wantAbort(t, db, `UPDATE tasks SET stage_rank = 9 WHERE id = ?`, id)
		wantAbort(t, db, `INSERT INTO tasks (title, scope, worksource, stage_rank) VALUES ('x', 'task', 'ws', 4)`)
	})
}

// ---------------------------------------------------------------------------
// Fail-closed extras the lattice adds (documented in NOTES.md).
// ---------------------------------------------------------------------------

func TestIdentityColumnsImmutable(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO worksources (id, title, kind) VALUES ('ws2', 'Other', 'repo')`)
	id := mkTask(t, db, "task", "seeded")
	wantAbort(t, db, `UPDATE tasks SET scope = 'initiative' WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET worksource = 'ws2' WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET origin = 'user' WHERE id = ?`, id)
	wantAbort(t, db, `UPDATE tasks SET created_at = datetime('now', '-1 day') WHERE id = ?`, id)
}

func TestNoDeleteBackstops(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "packaged")
	mkPacket(t, db, id)
	wantAbort(t, db, `DELETE FROM tasks WHERE id = ?`, id)
	wantAbort(t, db, `DELETE FROM review_packets WHERE task_id = ?`, id)

	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('r1', 'pipeline', 'worker', 'ws', ?)`, id)
	wantAbort(t, db, `DELETE FROM runs WHERE id = 'r1'`)

	mkHomieSession(t, db, "h1")
	wantAbort(t, db, `DELETE FROM homie_sessions WHERE id = 'h1'`)
}

func TestHomieSessionRegistryInvariants(t *testing.T) {
	db := openSpine(t)

	// Start-time identity and resume locators are authoritative registry
	// state, never nullable placeholders that a later component guesses.
	wantAbort(t, db, `INSERT INTO homie_sessions (id) VALUES ('incomplete')`)
	wantAbort(t, db, `INSERT INTO homie_sessions
		(id, container_name, verb_allowlist, session_path, binding)
		VALUES ('bad-json', 'mc-homie-bad-json', 'not-json', 'sessions/bad-json', 'fake/fake')`)
	wantAbort(t, db, `INSERT INTO homie_sessions
		(id, container_name, verb_allowlist, session_path, binding)
		VALUES ('bad-shape', 'mc-homie-bad-shape', '{}', 'sessions/bad-shape', 'fake/fake')`)
	mkHomieSession(t, db, "h1")

	for name, update := range map[string]string{
		"id":             `id = 'renamed'`,
		"created_at":     `created_at = datetime('now', '+1 day')`,
		"container_name": `container_name = 'mc-homie-other'`,
		"verb_allowlist": `verb_allowlist = '["task.add"]'`,
		"session_path":   `session_path = 'sessions/other'`,
		"binding":        `binding = 'codex/chatgpt'`,
	} {
		t.Run(name+"_immutable", func(t *testing.T) {
			wantAbort(t, db, `UPDATE homie_sessions SET `+update+` WHERE id = 'h1'`)
		})
	}

	// The runner registers the remaining locator pair exactly once. A
	// half-registration is unstorable and conflicting retries fail closed.
	wantAbort(t, db, `UPDATE homie_sessions SET native_session_ref = 'native-1' WHERE id = 'h1'`)
	mustExec(t, db, `UPDATE homie_sessions
		SET native_session_ref = 'native-1', trace_filename = 'native.jsonl'
		WHERE id = 'h1'`)
	wantAbort(t, db, `UPDATE homie_sessions
		SET native_session_ref = 'native-2', trace_filename = 'native.jsonl'
		WHERE id = 'h1'`)
}

// ADR-016 D3: launch fencing is canonical liveness state on homie_sessions,
// with the pairing rules as storage CHECKs, not as Go politeness.
func TestHomieLaunchFenceBackstops(t *testing.T) {
	assertHomieLaunchFenceBackstops(t, openSpine(t))
}

// assertHomieLaunchFenceBackstops states D3's homie_sessions storage contract
// once. A migrated spine is held to it alongside a fresh one (see
// TestMigrateV2ToCurrent): a fence the migration fails to carry over is a
// fence that does not exist on any real deployment.
func assertHomieLaunchFenceBackstops(t *testing.T, db *sql.DB) {
	t.Helper()
	mkHomieSession(t, db, "launch-fence")

	// `homie start` names only the five identity columns, so the schema's
	// defaults ARE its launch initialization: every launch/debt field must
	// start empty/zero without the Go layer spelling it.
	if got := oneInt(t, db, `
		SELECT count(*) FROM homie_sessions WHERE id = 'launch-fence'
		  AND current_launch_id IS NULL AND current_launch_mode IS NULL
		  AND current_prime_through_seq IS NULL AND current_prime_row_count IS NULL
		  AND current_container_id IS NULL
		  AND launch_bound_at IS NULL AND launch_started_at IS NULL
		  AND resume_owed = 0 AND resume_mode IS NULL
		  AND resume_prime_through_seq IS NULL AND resume_prime_row_count IS NULL`,
	); got != 1 {
		t.Fatal("a new session must start with every launch/debt field empty/zero (D3)")
	}

	launch := strings.Repeat("ab", 8)     // 16 lowercase hex
	container := strings.Repeat("cd", 32) // 64 lowercase hex

	// One full-width UPDATE per case: every launch/debt column is set at
	// once, so each shape is judged whole and no case leaks into the next.
	type launchRow struct {
		launchID, launchMode, primeSeq, primeCount any
		containerID, boundAt, startedAt            any
		resumeMode, resumeSeq, resumeCount         any
		resumeOwed                                 int
	}
	for name, c := range map[string]struct {
		row launchRow
		ok  bool
	}{
		// The legal states of D3's lattice.
		"all_empty_zero":   {ok: true, row: launchRow{}},
		"fresh_launch":     {ok: true, row: launchRow{launchID: launch, launchMode: "fresh"}},
		"native_launch":    {ok: true, row: launchRow{launchID: launch, launchMode: "native"}},
		"rows_launch":      {ok: true, row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 0, primeCount: 0}},
		"rows_launch_deep": {ok: true, row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 9214, primeCount: 118}},
		"bound_launch": {ok: true, row: launchRow{launchID: launch, launchMode: "fresh",
			containerID: container, boundAt: "2026-07-16 03:00:00"}},
		"started_launch": {ok: true, row: launchRow{launchID: launch, launchMode: "fresh",
			containerID: container, boundAt: "2026-07-16 03:00:00", startedAt: "2026-07-16 03:00:05"}},
		"resume_debt_native": {ok: true, row: launchRow{resumeOwed: 1, resumeMode: "native"}},
		"resume_debt_rows":   {ok: true, row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeSeq: 41, resumeCount: 2}},

		// Launch id and mode are both-null-or-both-present, and the id is
		// pinned by the D2 dual-length hex fence: length() and GLOB both stop
		// at the first NUL, so a forged id could otherwise store as a value
		// its own generation lookup cannot find.
		"launch_id_without_mode": {row: launchRow{launchID: launch}},
		"mode_without_launch_id": {row: launchRow{launchMode: "fresh"}},
		"launch_mode_unknown":    {row: launchRow{launchID: launch, launchMode: "resume"}},
		"launch_id_short":        {row: launchRow{launchID: launch[:15], launchMode: "fresh"}},
		"launch_id_long":         {row: launchRow{launchID: launch + "a", launchMode: "fresh"}},
		"launch_id_uppercase":    {row: launchRow{launchID: strings.ToUpper(launch), launchMode: "fresh"}},
		"launch_id_nonhex":       {row: launchRow{launchID: launch[:15] + "g", launchMode: "fresh"}},
		"launch_id_nul_tail":     {row: launchRow{launchID: launch + "\x00EVIL", launchMode: "fresh"}},
		"launch_id_nul_pad":      {row: launchRow{launchID: launch[:15] + "\x00", launchMode: "fresh"}},
		// A BLOB bypasses TEXT affinity entirely: length() counts its bytes
		// (NULs included), and GLOB reads it converted, only to the first
		// NUL — so without a typeof fence a NUL-embedded 16-byte BLOB passes
		// every other conjunct, and even a hex-clean BLOB stores as a value
		// the Go layer's string comparisons can never match.
		"launch_id_blob_nul_pad":   {row: launchRow{launchID: append([]byte(launch[:15]), 0), launchMode: "fresh"}},
		"launch_id_blob_hex_bytes": {row: launchRow{launchID: []byte(launch), launchMode: "fresh"}},

		// Only `rows` mode carries the prime cutoff/count, always as a
		// non-negative pair.
		"fresh_launch_with_prime_pair": {row: launchRow{launchID: launch, launchMode: "fresh", primeSeq: 0, primeCount: 0}},
		"native_launch_with_prime_pair": {row: launchRow{launchID: launch, launchMode: "native",
			primeSeq: 0, primeCount: 0}},
		"rows_launch_missing_prime_pair": {row: launchRow{launchID: launch, launchMode: "rows"}},
		"rows_launch_half_pair_seq":      {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 0}},
		"rows_launch_half_pair_count":    {row: launchRow{launchID: launch, launchMode: "rows", primeCount: 0}},
		"rows_launch_negative_seq":       {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: -1, primeCount: 0}},
		"rows_launch_negative_count":     {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 0, primeCount: -1}},
		"prime_pair_without_launch":      {row: launchRow{primeSeq: 0, primeCount: 0}},
		// INTEGER affinity stores non-numeric TEXT and any BLOB as-is, and
		// SQLite's cross-type ordering ranks both above every number — so
		// 'abc' >= 0 and X'FF' >= 0 are TRUE and only a typeof fence holds.
		// A non-integral REAL passes ">= 0" numerically the same way.
		"prime_seq_nonnumeric_text": {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: "abc", primeCount: 41}},
		"prime_seq_noninteger_real": {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 2.5, primeCount: 3}},
		"prime_count_blob":          {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 7, primeCount: []byte{0xff}}},
		// ADR-016 D3: an empty completed prefix has the single closed
		// encoding (0,0) — zero is a below-everything sentinel, not a row
		// identity. A zero cutoff claiming rows, or a positive cutoff
		// claiming none, is outside the closed grammar.
		"rows_launch_zero_cutoff_with_rows":     {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 0, primeCount: 41}},
		"rows_launch_positive_cutoff_zero_rows": {row: launchRow{launchID: launch, launchMode: "rows", primeSeq: 5, primeCount: 0}},

		// Container id and bound time are paired, require a current launch,
		// and the id is one 64-lowercase-hex Docker id.
		"container_without_bound":   {row: launchRow{launchID: launch, launchMode: "fresh", containerID: container}},
		"bound_without_container":   {row: launchRow{launchID: launch, launchMode: "fresh", boundAt: "2026-07-16 03:00:00"}},
		"bound_pair_without_launch": {row: launchRow{containerID: container, boundAt: "2026-07-16 03:00:00"}},
		"container_id_short":        {row: launchRow{launchID: launch, launchMode: "fresh", containerID: container[:63], boundAt: "t"}},
		"container_id_long":         {row: launchRow{launchID: launch, launchMode: "fresh", containerID: container + "c", boundAt: "t"}},
		"container_id_uppercase":    {row: launchRow{launchID: launch, launchMode: "fresh", containerID: strings.ToUpper(container), boundAt: "t"}},
		"container_id_nonhex":       {row: launchRow{launchID: launch, launchMode: "fresh", containerID: container[:63] + "x", boundAt: "t"}},
		"container_id_nul_tail":     {row: launchRow{launchID: launch, launchMode: "fresh", containerID: container + "\x00EVIL", boundAt: "t"}},
		"container_id_nul_pad":      {row: launchRow{launchID: launch, launchMode: "fresh", containerID: container[:63] + "\x00", boundAt: "t"}},
		"container_id_blob_nul_pad": {row: launchRow{launchID: launch, launchMode: "fresh", containerID: append([]byte(container[:63]), 0), boundAt: "t"}},

		// A start time requires the bound pair (transitively, a launch).
		"started_without_bound_pair": {row: launchRow{launchID: launch, launchMode: "fresh", startedAt: "t"}},
		"started_without_launch":     {row: launchRow{startedAt: "t"}},

		// Resume debt is resume_owed=1 plus a mode (`fresh` is not a resume
		// mode), carries the prime pair only for `rows`, and is mutually
		// exclusive with a current launch — the supersede-then-select order
		// is a storage rule, not scheduler politeness.
		"resume_owed_out_of_range": {row: launchRow{resumeOwed: 2, resumeMode: "native"}},
		// Out of range with NO mode: the (owed=1)=(mode present) pairing
		// CHECK is satisfied at owed=2/mode=NULL, so only the IN (0,1)
		// range fence rejects this shape — the case above alone would grade
		// a dropped range fence green.
		"resume_owed_out_of_range_alone": {row: launchRow{resumeOwed: 2}},
		"resume_owed_without_mode":       {row: launchRow{resumeOwed: 1}},
		"resume_mode_without_debt":       {row: launchRow{resumeMode: "native"}},
		"resume_mode_fresh_invalid":      {row: launchRow{resumeOwed: 1, resumeMode: "fresh"}},
		"resume_debt_with_current_launch": {row: launchRow{launchID: launch, launchMode: "fresh",
			resumeOwed: 1, resumeMode: "native"}},
		"resume_rows_missing_prime_pair": {row: launchRow{resumeOwed: 1, resumeMode: "rows"}},
		"resume_native_with_prime_pair":  {row: launchRow{resumeOwed: 1, resumeMode: "native", resumeSeq: 0, resumeCount: 0}},
		"resume_half_pair_seq":           {row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeSeq: 0}},
		"resume_half_pair_count":         {row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeCount: 0}},
		"resume_negative_seq":            {row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeSeq: -1, resumeCount: 0}},
		"resume_negative_count":          {row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeSeq: 0, resumeCount: -1}},
		"resume_prime_pair_without_debt": {row: launchRow{resumeSeq: 0, resumeCount: 0}},
		"resume_seq_nonnumeric_text":     {row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeSeq: "abc", resumeCount: 41}},
		"resume_zero_cutoff_with_rows":   {row: launchRow{resumeOwed: 1, resumeMode: "rows", resumeSeq: 0, resumeCount: 41}},
		"resume_positive_cutoff_zero_rows": {row: launchRow{resumeOwed: 1, resumeMode: "rows",
			resumeSeq: 5, resumeCount: 0}},
	} {
		t.Run(name, func(t *testing.T) {
			const set = `
				UPDATE homie_sessions SET
					current_launch_id = ?, current_launch_mode = ?,
					current_prime_through_seq = ?, current_prime_row_count = ?,
					current_container_id = ?, launch_bound_at = ?, launch_started_at = ?,
					resume_owed = ?, resume_mode = ?,
					resume_prime_through_seq = ?, resume_prime_row_count = ?
				WHERE id = 'launch-fence'`
			args := []any{
				c.row.launchID, c.row.launchMode, c.row.primeSeq, c.row.primeCount,
				c.row.containerID, c.row.boundAt, c.row.startedAt,
				c.row.resumeOwed, c.row.resumeMode, c.row.resumeSeq, c.row.resumeCount,
			}
			if c.ok {
				mustExec(t, db, set, args...)
			} else {
				wantAbort(t, db, set, args...)
			}
		})
	}

	// resume_owed is NOT NULL, and NOT NULL is its only fence against NULL:
	// the launchRow fixture cannot express NULL (a bare int), and NULL slides
	// through IN (0,1) and every pairing CHECK because a NULL CHECK passes in
	// SQLite. Probed from the known all-empty state so nothing else can
	// reject vacuously.
	mustExec(t, db, `
		UPDATE homie_sessions SET
			current_launch_id = NULL, current_launch_mode = NULL,
			current_prime_through_seq = NULL, current_prime_row_count = NULL,
			current_container_id = NULL, launch_bound_at = NULL, launch_started_at = NULL,
			resume_owed = 0, resume_mode = NULL,
			resume_prime_through_seq = NULL, resume_prime_row_count = NULL
		WHERE id = 'launch-fence'`)
	wantAbort(t, db, `UPDATE homie_sessions SET resume_owed = NULL WHERE id = 'launch-fence'`)
}

// homie_bindings is bind-event history (§15.4): end -> resume on the same
// surface/channel appends a fresh row; at most one ACTIVE row per place;
// the history persists indefinitely (NOTE(P1.19)).
func TestHomieBindingsHistory(t *testing.T) {
	db := openSpine(t)
	mkHomieSession(t, db, "h1")
	mkHomieSession(t, db, "h2")
	mustExec(t, db, `INSERT INTO homie_bindings (session_id, surface, channel_ref) VALUES ('h1', 'discord', 'chan-1')`)

	// A second ACTIVE binding for the same place is refused.
	wantAbort(t, db, `INSERT INTO homie_bindings (session_id, surface, channel_ref) VALUES ('h1', 'discord', 'chan-1')`)
	// The place is globally unambiguous: a different active session cannot
	// claim the same Discord/dashboard/CLI destination either (§15.4).
	wantAbort(t, db, `INSERT INTO homie_bindings (session_id, surface, channel_ref) VALUES ('h2', 'discord', 'chan-1')`)
	// Bind-event identity is immutable; only active -> inactive is legal.
	wantAbort(t, db, `UPDATE homie_bindings SET channel_ref = 'chan-2' WHERE session_id = 'h1'`)

	// Session ends: active bindings clear (§15.4)…
	mustExec(t, db, `UPDATE homie_sessions SET status = 'ended' WHERE id = 'h1'`)
	if active := oneInt(t, db, `SELECT active FROM homie_bindings WHERE session_id = 'h1'`); active != 0 {
		t.Fatalf("ended session retained active binding: %d", active)
	}
	wantAbort(t, db, `UPDATE homie_bindings SET active = 1 WHERE session_id = 'h1'`)
	wantAbort(t, db, `INSERT INTO homie_bindings (session_id, surface, channel_ref) VALUES ('h1', 'dashboard', 'ended-place')`)
	// …and resuming from the same surface creates a FRESH binding row — the
	// spec's primary resume flow must be storable.
	mustExec(t, db, `UPDATE homie_sessions SET status = 'active' WHERE id = 'h1'`)
	mustExec(t, db, `INSERT INTO homie_bindings (session_id, surface, channel_ref) VALUES ('h1', 'discord', 'chan-1')`)
	if n := oneInt(t, db, `SELECT COUNT(*) FROM homie_bindings WHERE session_id = 'h1' AND surface = 'discord' AND channel_ref = 'chan-1'`); n != 2 {
		t.Fatalf("bind-event rows = %d, want 2 (history, one per bind)", n)
	}

	// Bindings history persists indefinitely.
	wantAbort(t, db, `DELETE FROM homie_bindings`)
	wantAbort(t, db, `DELETE FROM homie_bindings WHERE active = 0`)
}

func TestConversationRowsAppendOnly(t *testing.T) {
	db := openSpine(t)
	mkHomieSession(t, db, "h1")
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, attachments)
		 VALUES ('h1', 90, 'inbound', 'discord', 'bad', 'not-json')`)
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, attachments)
		 VALUES ('h1', 91, 'inbound', 'discord', 'bad', '{}')`)
	mustExec(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		 VALUES ('h1', 1, 'inbound', 'discord', 'hello')`)

	// Content is immutable…
	wantAbort(t, db, `UPDATE conversation_messages SET body = 'edited' WHERE session_id = 'h1' AND seq = 1`)
	wantAbort(t, db, `UPDATE conversation_messages SET direction = 'reply' WHERE session_id = 'h1' AND seq = 1`)
	wantAbort(t, db, `DELETE FROM conversation_messages`)
	// …while the runner's fenced claim state may advance (§11.5).
	mustExec(t, db,
		`UPDATE conversation_messages SET claimed_by = 'runner-1', claimed_at = datetime('now')
		 WHERE session_id = 'h1' AND seq = 1`)
	mustExec(t, db,
		`UPDATE conversation_messages SET completed_at = datetime('now')
		 WHERE session_id = 'h1' AND seq = 1`)
}

func TestOutboxPayloadIsJSONObject(t *testing.T) {
	db := openSpine(t)
	wantAbort(t, db, `INSERT INTO outbox (kind, surface, payload) VALUES ('health', 'email', '{"status":"ok"}')`)
	wantAbort(t, db, `INSERT INTO outbox (kind, surface, payload) VALUES ('health', 'dashboard', 'not-json')`)
	wantAbort(t, db, `INSERT INTO outbox (kind, surface, payload) VALUES ('health', 'dashboard', '[]')`)
	mustExec(t, db, `INSERT INTO outbox (kind, surface, payload) VALUES ('health', 'dashboard', '{"status":"ok"}')`)
}

// ADR-016 Decision 2: one activity fans out at most once to each tagged
// destination. Both columns are nullable for pre-Phase-3 history, but a new
// keyed row must carry the pair and the event/destination key is globally
// unique.
func TestOutboxDispatchDestinationBackstops(t *testing.T) {
	assertOutboxDestinationBackstops(t, openSpine(t))
}

// assertOutboxDestinationBackstops states D2's outbox-side storage contract
// once, for both a fresh and a migrated spine.
func assertOutboxDestinationBackstops(t *testing.T, db *sql.DB) {
	t.Helper()
	res := mustExec(t, db, `INSERT INTO activity (actor, kind, detail) VALUES ('mc', 'dispatch.health', '{}')`)
	activityID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	eventKey := strings.Repeat("f", 64)

	mustExec(t, db, `
		INSERT INTO outbox (kind, surface, payload, source_activity_id, event_destination_key)
		VALUES ('health', 'dashboard', '{"status":"unavailable"}', ?, ?)`, activityID, eventKey)
	wantAbort(t, db, `
		INSERT INTO outbox (kind, surface, payload, source_activity_id, event_destination_key)
		VALUES ('health', 'cli', '{"status":"unavailable"}', ?, ?)`, activityID, eventKey)
	wantAbort(t, db, `
		INSERT INTO outbox (kind, surface, payload, source_activity_id)
		VALUES ('health', 'dashboard', '{"status":"unavailable"}', ?)`, activityID)
	wantAbort(t, db, `
		INSERT INTO outbox (kind, surface, payload, event_destination_key)
		VALUES ('health', 'dashboard', '{"status":"unavailable"}', ?)`, strings.Repeat("1", 64))
	wantAbort(t, db, `
		INSERT INTO outbox (kind, surface, payload, source_activity_id, event_destination_key)
		VALUES ('health', 'dashboard', '{"status":"unavailable"}', 999999, ?)`, strings.Repeat("2", 64))
	wantAbort(t, db, `
		INSERT INTO outbox (kind, surface, payload, source_activity_id, event_destination_key)
		VALUES ('health', 'dashboard', '{"status":"unavailable"}', ?, ?)`, activityID, strings.Repeat("F", 64))

	// The destination key carries activity's NUL fence, for the same reason.
	for name, key := range map[string]string{
		"nul_tail": strings.Repeat("4", 64) + "\x00EVIL",
		"nul_pad":  strings.Repeat("4", 63) + "\x00",
	} {
		t.Run("event_destination_key_"+name, func(t *testing.T) {
			wantAbort(t, db, `
				INSERT INTO outbox (kind, surface, payload, source_activity_id, event_destination_key)
				VALUES ('health', 'dashboard', '{"status":"unavailable"}', ?, ?)`, activityID, key)
		})
	}

	// And activity's v4 BLOB fence: a BLOB key passes every CHECK and stores
	// as a UNIQUE value distinct from its TEXT twin.
	for name, key := range map[string][]byte{
		"hex_blob": []byte(strings.Repeat("5", 64)),
		"nul_blob": append([]byte(strings.Repeat("5", 63)), 0x00),
	} {
		t.Run("event_destination_key_"+name, func(t *testing.T) {
			wantAbort(t, db, `
				INSERT INTO outbox (kind, surface, payload, source_activity_id, event_destination_key)
				VALUES ('health', 'dashboard', '{"status":"unavailable"}', ?, ?)`, activityID, key)
		})
	}

	// The existing append-only content fence covers the new provenance too.
	wantAbort(t, db, `UPDATE outbox SET source_activity_id = NULL`)
	wantAbort(t, db, `UPDATE outbox SET event_destination_key = ?`, strings.Repeat("3", 64))
}

// Takeover finding — the outbox was the only communication table without a
// substrate fence: §15.5 "nothing pushed is ever lost" and ack idempotency
// lived solely in verbs code. Content is immutable, rows never delete, and
// delivered_at moves NULL→value exactly once.
func TestOutboxDeliveryBookkeepingIsFenced(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO outbox (kind, surface, payload) VALUES ('health', 'dashboard', '{"status":"ok"}')`)

	wantAbort(t, db, `DELETE FROM outbox`)
	wantAbort(t, db, `UPDATE outbox SET kind = 'console'`)
	wantAbort(t, db, `UPDATE outbox SET surface = 'cli'`)
	wantAbort(t, db, `UPDATE outbox SET channel_ref = 'c9'`)
	wantAbort(t, db, `UPDATE outbox SET payload = '{"forged":true}'`)
	wantAbort(t, db, `UPDATE outbox SET created_at = datetime('now', '+1 hour')`)

	// delivered_at is one-way and set-once (ack replay preserves the original).
	mustExec(t, db, `UPDATE outbox SET delivered_at = datetime('now')`)
	wantAbort(t, db, `UPDATE outbox SET delivered_at = NULL`)
	wantAbort(t, db, `UPDATE outbox SET delivered_at = datetime('now', '+1 hour')`)
	wantAbort(t, db, `DELETE FROM outbox`)
}

// Takeover finding — conversation rows had no active-session backstop: the
// domain layer refuses, but a resurrected runner replaying a reply append
// onto an ended/reaped session met no storage rejection (schema contract:
// "a bug in mc cannot write an illegal state").
func TestConversationRowsRequireActiveSession(t *testing.T) {
	db := openSpine(t)
	mkHomieSession(t, db, "h-conv")
	mustExec(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		 VALUES ('h-conv', 1, 'inbound', 'discord', 'hello')`)

	mustExec(t, db, `UPDATE homie_sessions SET status = 'ended' WHERE id = 'h-conv'`)
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		 VALUES ('h-conv', 2, 'reply', 'homie', 'ghost turn')`)
	mustExec(t, db, `UPDATE homie_sessions SET status = 'reaped' WHERE id = 'h-conv'`)
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		 VALUES ('h-conv', 2, 'inbound', 'discord', 'ghost inbound')`)
	// The permanent transcript survives the session ending untouched.
	if n := oneInt(t, db, `SELECT COUNT(*) FROM conversation_messages WHERE session_id = 'h-conv'`); n != 1 {
		t.Fatalf("transcript rows = %d, want 1", n)
	}
}

// ADR-013 claim/reply lattice — the storage law behind "runner death cannot
// create two logical replies to one inbound turn" (§11.5): a reply names the
// inbound turn it answers, exactly once, within its own conversation; claim
// and completion state are one-way and set-once.
func TestConversationClaimReplyLattice(t *testing.T) {
	db := openSpine(t)
	mkHomieSession(t, db, "h-cr")
	mkHomieSession(t, db, "h-cr2")
	mustExec(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		 VALUES ('h-cr', 1, 'inbound', 'discord', 'question')`)
	turn := oneInt(t, db, `SELECT id FROM conversation_messages WHERE session_id = 'h-cr' AND seq = 1`)

	// A reply names its inbound turn; inbound rows never carry reply_to.
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body)
		 VALUES ('h-cr', 2, 'reply', 'homie', 'orphan reply')`)
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, reply_to)
		 VALUES ('h-cr', 2, 'inbound', 'discord', 'inbound with reply_to', ?)`, turn)
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, reply_to)
		 VALUES ('h-cr', 2, 'reply', 'homie', 'dangling', 999)`)
	// …and only an inbound turn of its own conversation.
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, reply_to)
		 VALUES ('h-cr2', 1, 'reply', 'homie', 'cross-conversation', ?)`, turn)

	// Claim columns travel as a pair, and completion requires a claim.
	wantAbort(t, db, `UPDATE conversation_messages SET claimed_by = 'h-cr' WHERE id = ?`, turn)
	wantAbort(t, db, `UPDATE conversation_messages SET completed_at = datetime('now') WHERE id = ?`, turn)
	mustExec(t, db,
		`UPDATE conversation_messages SET claimed_by = 'h-cr', claimed_at = datetime('now') WHERE id = ?`, turn)

	// Claims are set-once: a fresh runner resumes the durable claim, never
	// rewrites or clears it.
	wantAbort(t, db, `UPDATE conversation_messages SET claimed_by = 'other' WHERE id = ?`, turn)
	wantAbort(t, db, `UPDATE conversation_messages SET claimed_at = datetime('now', '+1 hour') WHERE id = ?`, turn)
	wantAbort(t, db, `UPDATE conversation_messages SET claimed_by = NULL, claimed_at = NULL WHERE id = ?`, turn)

	mustExec(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, reply_to)
		 VALUES ('h-cr', 2, 'reply', 'homie', 'answer', ?)`, turn)
	reply := oneInt(t, db, `SELECT id FROM conversation_messages WHERE session_id = 'h-cr' AND seq = 2`)

	// One logical reply per inbound turn, immutable linkage, and reply rows
	// are never themselves claimable.
	wantAbort(t, db,
		`INSERT INTO conversation_messages (session_id, seq, direction, surface, body, reply_to)
		 VALUES ('h-cr', 3, 'reply', 'homie', 'second reply', ?)`, turn)
	wantAbort(t, db, `UPDATE conversation_messages SET reply_to = NULL WHERE id = ?`, reply)
	wantAbort(t, db,
		`UPDATE conversation_messages SET claimed_by = 'h-cr', claimed_at = datetime('now') WHERE id = ?`, reply)

	// Completion is set-once and never clears.
	mustExec(t, db, `UPDATE conversation_messages SET completed_at = datetime('now') WHERE id = ?`, turn)
	wantAbort(t, db, `UPDATE conversation_messages SET completed_at = datetime('now', '+1 hour') WHERE id = ?`, turn)
	wantAbort(t, db, `UPDATE conversation_messages SET completed_at = NULL WHERE id = ?`, turn)
}

// ---------------------------------------------------------------------------
// Phase 2 additive columns (phase2-contract §5) — new-column coverage only;
// every case above is Phase 1a/1b and stays untouched.
// ---------------------------------------------------------------------------

// NOTE(P2.1): the console schedule tunables live on the lock row with the
// not-configured default (hour 24 = never due), CHECK-bounded.
func TestLockConsoleScheduleColumns(t *testing.T) {
	db := openSpine(t)
	if h := oneInt(t, db, `SELECT console_hour FROM lock WHERE id = 1`); h != 24 {
		t.Fatalf("console_hour default = %d, want 24 (not configured, D-mc-4)", h)
	}
	if m := oneInt(t, db, `SELECT console_minute FROM lock WHERE id = 1`); m != 0 {
		t.Fatalf("console_minute default = %d, want 0", m)
	}
	if tz := oneStr(t, db, `SELECT console_tz FROM lock WHERE id = 1`); tz != "UTC" {
		t.Fatalf("console_tz default = %q, want UTC", tz)
	}
	mustExec(t, db, `UPDATE lock SET console_hour = 8, console_minute = 30, console_tz = 'America/New_York' WHERE id = 1`)
	wantAbort(t, db, `UPDATE lock SET console_hour = 25 WHERE id = 1`)
	wantAbort(t, db, `UPDATE lock SET console_hour = -1 WHERE id = 1`)
	wantAbort(t, db, `UPDATE lock SET console_minute = 60 WHERE id = 1`)
}

// NOTE(P2.2): the verdict record on the Verifier's own runs row — outcome
// vocabulary CHECK-pinned, evidence/correction paths free-form.
func TestRunsVerdictRecordColumns(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "worked")
	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('v1', 'pipeline', 'verifier', 'ws', ?)`, id)
	mustExec(t, db, `
		UPDATE runs SET verdict_outcome = 'correct', evidence_path = 'e.md',
		       correction_path = 'corrections/mc-1-corrections1', deepening = 'churn'
		WHERE id = 'v1'`)
	wantAbort(t, db, `UPDATE runs SET verdict_outcome = 'maybe' WHERE id = 'v1'`)
	wantAbort(t, db, `UPDATE runs SET deepening = 'sideways' WHERE id = 'v1'`)
}

func TestRunSessionLocatorsRegisterOnce(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO runs (id, tier, role) VALUES ('r1', 'pipeline', 'worker')`)
	wantAbort(t, db, `UPDATE runs SET native_session_ref='session-only' WHERE id='r1'`)
	mustExec(t, db, `UPDATE runs SET native_session_ref='session-1', trace_filename='native.jsonl' WHERE id='r1'`)
	mustExec(t, db, `UPDATE runs SET native_session_ref='session-1', trace_filename='native.jsonl' WHERE id='r1'`)
	wantAbort(t, db, `UPDATE runs SET native_session_ref='session-2', trace_filename='other.jsonl' WHERE id='r1'`)
}

// NOTE(P2.3): tasks.refine_notes is mutable carried-notes state, not an
// identity column — the immutability trigger must not catch it.
func TestTasksRefineNotesColumn(t *testing.T) {
	db := openSpine(t)
	id := mkTask(t, db, "task", "packaged")
	mustExec(t, db, `UPDATE tasks SET refine_notes = 'tighten the abstract' WHERE id = ?`, id)
	if got := taskStr(t, db, id, "refine_notes"); got != "tighten the abstract" {
		t.Fatalf("refine_notes = %q", got)
	}
	mustExec(t, db, `UPDATE tasks SET refine_notes = NULL WHERE id = ?`, id)
}

// ---------------------------------------------------------------------------
// Plan review (ADR-020 D1): the wave child's plan_reviewed carrier, its
// one-way law, and the storage-side gate on child work.
// ---------------------------------------------------------------------------

func TestPlanReviewCarrier(t *testing.T) {
	t.Run("children_born_unreviewed", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		child := mkChild(t, db, ini)
		if got := taskInt(t, db, child, "plan_reviewed"); got != 0 {
			t.Fatalf("wave child born plan_reviewed = %d, want 0 (ADR-020 D1)", got)
		}
		// Born reviewed is refused outright — the NOTE(P1.2) birth symmetry.
		wantAbort(t, db, `INSERT INTO tasks (title, scope, status, initiative_id, worksource, plan_reviewed)
		                  VALUES ('pre-blessed', 'task', 'seeded', ?, 'ws-1', 1)`, ini)
	})

	t.Run("only_a_wave_child_can_carry_the_mark", func(t *testing.T) {
		db := openSpine(t)
		// A standalone task and an initiative are pinned at 0: on them the
		// column reads "not applicable", never "unreviewed".
		loose := mkTask(t, db, "task", "seeded")
		wantAbort(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, loose)
		ini := mkTask(t, db, "initiative", "seeded")
		wantAbort(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, ini)
	})

	t.Run("one_way_and_live_rows_only", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		child := mkChild(t, db, ini)

		// 0 -> 1 on a live child is the one legal move.
		mustExec(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, child)
		if got := taskInt(t, db, child, "plan_reviewed"); got != 1 {
			t.Fatalf("plan_reviewed = %d after pass, want 1", got)
		}
		// It never clears: a send-back destroys the children instead (D5).
		wantAbort(t, db, `UPDATE tasks SET plan_reviewed = 0 WHERE id = ?`, child)

		// Never lands on a decided or archived row.
		other := mkChild(t, db, ini)
		mustExec(t, db, `UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now') WHERE id = ?`, other)
		wantAbort(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, other)
		mustExec(t, db, `UPDATE tasks SET archived = 1 WHERE id = ?`, other)
		wantAbort(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, other)
	})

	t.Run("child_work_requires_plan_review", func(t *testing.T) {
		db := openSpine(t)
		ini := mkTask(t, db, "initiative", "seeded")
		child := mkChild(t, db, ini)

		// The fence: the spine cannot record a seeded -> worked advance on a
		// child the Editor never passed, even if mc dispatch is buggy.
		wantAbort(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, child)

		// At 1 the same advance commits.
		mustExec(t, db, `UPDATE tasks SET plan_reviewed = 1 WHERE id = ?`, child)
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, child)
		if got := taskStr(t, db, child, "status"); got != "worked" {
			t.Fatalf("reviewed child status = %q, want worked", got)
		}

		// A standalone task is untouched by the gate (plan_reviewed = 0 on it
		// means not-applicable, not unreviewed).
		loose := mkTask(t, db, "task", "seeded")
		mustExec(t, db, `UPDATE tasks SET status = 'worked' WHERE id = ?`, loose)
	})
}
