package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"mc/dispatch"
)

// Dispatch is the one caller of dispatch.Decide (contract §3; §10; Inv. 2/3).
// One BEGIN IMMEDIATE transaction: load Records/Lock from the spine and
// Config from the lock row's tunable columns, read Clock.Now from
// datetime('now'), call Decide, apply the action's writes, commit, and
// return the effect JSON. A losing racer aborts before effect data (§10
// fencing): claim is CAS on the free lock row.
func Dispatch(db *sql.DB) (any, error) {
	var effect map[string]any
	err := inTx(db, func(ctx context.Context, q Q) error {
		now, err := spineNow(ctx, q)
		if err != nil {
			return err
		}
		lk, tun, err := loadLock(ctx, q)
		if err != nil {
			return err
		}
		rec, err := loadRecords(ctx, q)
		if err != nil {
			return err
		}
		cfg := dispatch.Config{
			ReviewWIPCap:   3, // Inv. 18; the substrate trigger is the backstop
			TimeoutMinutes: tun.timeoutMinutes,
			GraceMinutes:   tun.graceMinutes,
			SpawnGraceS:    tun.spawnGraceS,
			// Console scheduling is [P2] (contract §5 deferred list). Hour 24
			// normalizes to next-day 00:00, so consoleDue never fires: the
			// stored schedule is pinned "not yet configured" without touching
			// the promoted dispatch package (see deviation note D-mc-4).
			ConsoleHour:   24,
			ConsoleMinute: 0,
			ConsoleLoc:    time.UTC,
		}
		action := dispatch.Decide(rec, lk, cfg, dispatch.Clock{Now: now})
		effect, err = applyAction(ctx, q, action, tun)
		return err
	})
	if err != nil {
		return nil, err
	}
	return effect, nil
}

// tunables are the lock row's stored knobs (§16.3; contract §2 mc init).
type tunables struct {
	timeoutMinutes      int
	graceMinutes        int
	heartbeatIntervalS  int
	spawnGraceS         int
	hardDeadlineMinutes int
}

func spineNow(ctx context.Context, q Q) (time.Time, error) {
	var s string
	if err := q.QueryRowContext(ctx, `SELECT datetime('now')`).Scan(&s); err != nil {
		return time.Time{}, err
	}
	return parseSpineTime(s)
}

func loadLock(ctx context.Context, q Q) (dispatch.Lock, tunables, error) {
	var (
		runID, owner, acquiredAt, lastHB, hardDeadline sql.NullString
		subject                                        sql.NullInt64
		tun                                            tunables
	)
	err := q.QueryRowContext(ctx, `
		SELECT run_id, owner, subject, acquired_at, last_heartbeat_at,
		       hard_deadline_at, timeout_minutes, grace_minutes,
		       heartbeat_interval_s, spawn_grace_s, hard_deadline_minutes
		FROM lock WHERE id = 1`).Scan(
		&runID, &owner, &subject, &acquiredAt, &lastHB, &hardDeadline,
		&tun.timeoutMinutes, &tun.graceMinutes, &tun.heartbeatIntervalS,
		&tun.spawnGraceS, &tun.hardDeadlineMinutes)
	if err != nil {
		return dispatch.Lock{}, tun, err
	}
	lk := dispatch.Lock{}
	if runID.Valid {
		lk.Held = true
		lk.RunID = runID.String
		lk.Owner = owner.String
		if subject.Valid {
			s := subject.Int64
			lk.SubjectID = &s
		}
		if lk.AcquiredAt, err = parseSpineTime(acquiredAt.String); err != nil {
			return lk, tun, err
		}
		if lastHB.Valid {
			t, err := parseSpineTime(lastHB.String)
			if err != nil {
				return lk, tun, err
			}
			lk.LastHeartbeatAt = &t
		}
		if lk.HardDeadlineAt, err = parseSpineTime(hardDeadline.String); err != nil {
			return lk, tun, err
		}
	}
	return lk, tun, nil
}

// loadRecords mirrors §10's record queries into the dispatch projection.
func loadRecords(ctx context.Context, q Q) (dispatch.Records, error) {
	rec := dispatch.Records{}
	rows, err := q.QueryContext(ctx, `
		SELECT id, title, scope, initiative_id, priority, created_at, status,
		       blocked, dispatch_retries, decision, decided_at, archived,
		       worksource, branch, verified_sha, target_ref
		FROM tasks`)
	if err != nil {
		return rec, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			t                           dispatch.Task
			scope, status               string
			initiativeID                sql.NullInt64
			createdAt                   string
			blocked, archived           int
			decision, decidedAt         sql.NullString
			branch, verifiedSHA, target sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.Title, &scope, &initiativeID, &t.Priority,
			&createdAt, &status, &blocked, &t.DispatchRetries, &decision,
			&decidedAt, &archived, &t.Worksource, &branch, &verifiedSHA,
			&target); err != nil {
			return rec, err
		}
		t.Scope = dispatch.Scope(scope)
		t.Status = dispatch.Status(status)
		t.Blocked = blocked == 1
		t.Archived = archived == 1
		if initiativeID.Valid {
			v := initiativeID.Int64
			t.InitiativeID = &v
		}
		if t.CreatedAt, err = parseSpineTime(createdAt); err != nil {
			return rec, err
		}
		if decision.Valid {
			t.Decision = dispatch.TaskDecision(decision.String)
		}
		if decidedAt.Valid {
			d, err := parseSpineTime(decidedAt.String)
			if err != nil {
				return rec, err
			}
			t.DecidedAt = &d
		}
		t.Branch = branch.String
		t.VerifiedSHA = verifiedSHA.String
		t.TargetRef = target.String
		rec.Tasks = append(rec.Tasks, t)
	}
	if err := rows.Err(); err != nil {
		return rec, err
	}

	prows, err := q.QueryContext(ctx, `
		SELECT task_id, created_at, saturated, archived FROM review_packets`)
	if err != nil {
		return rec, err
	}
	defer prows.Close()
	for prows.Next() {
		var p dispatch.Packet
		var createdAt string
		var saturated, archived int
		if err := prows.Scan(&p.TaskID, &createdAt, &saturated, &archived); err != nil {
			return rec, err
		}
		if p.CreatedAt, err = parseSpineTime(createdAt); err != nil {
			return rec, err
		}
		p.Saturated = saturated == 1
		p.Archived = archived == 1
		rec.Packets = append(rec.Packets, p)
	}
	if err := prows.Err(); err != nil {
		return rec, err
	}

	var lastBriefing sql.NullString
	err = q.QueryRowContext(ctx, `
		SELECT created_at FROM activity WHERE kind = 'daily.briefing'
		ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&lastBriefing)
	if err != nil && err != sql.ErrNoRows {
		return rec, err
	}
	if lastBriefing.Valid {
		t, err := parseSpineTime(lastBriefing.String)
		if err != nil {
			return rec, err
		}
		rec.LastBriefingAt = &t
	}
	return rec, nil
}

// applyAction writes the decided action inside the same transaction and
// builds the effect JSON pinned by contract §2.
func applyAction(ctx context.Context, q Q, a dispatch.Action, tun tunables) (map[string]any, error) {
	switch a.Kind {
	case dispatch.KindIdle:
		return map[string]any{"action": "idle", "reason": string(a.Idle)}, nil

	case dispatch.KindSpawn:
		return applySpawn(ctx, q, a.Spawn, tun)

	case dispatch.KindReap:
		return applyReap(ctx, q, a.Reap)

	case dispatch.KindLand:
		// The land effect is pure effect data (§10 step 0c): the writes come
		// later, via mc land report.
		return map[string]any{
			"action":       "land",
			"task_id":      a.Land.TaskID,
			"branch":       a.Land.Branch,
			"verified_sha": a.Land.VerifiedSHA,
			"target_ref":   a.Land.TargetRef,
		}, nil

	case dispatch.KindReenter:
		// Step (2b), initiative arm [P2 tests]: one pure mutation,
		// packaged → seeded; no spawn this tick.
		if _, err := q.ExecContext(ctx,
			`UPDATE tasks SET status = 'seeded' WHERE id = ?`, a.Reenter.TaskID); err != nil {
			return nil, err
		}
		return map[string]any{"action": "reenter", "task_id": a.Reenter.TaskID}, nil
	}
	return nil, Domainf("dispatch: unknown action kind %q", a.Kind)
}

// applySpawn is the claim-and-spawn: CAS the free lock + INSERT the runs row
// (Inv. 4: the row is committed before the process starts), one transaction.
func applySpawn(ctx context.Context, q Q, sp *dispatch.Spawn, tun tunables) (map[string]any, error) {
	runID, err := newRunID()
	if err != nil {
		return nil, err
	}
	owner := baseRole(string(sp.Role))
	sessionPath := "sessions/" + runID // MC_HOME-relative (§16.1)

	// The subject's worksource rides the lock row; subjectless runs
	// (propose/console) carry none.
	var worksource any
	var subject any
	if sp.SubjectID != nil {
		subject = *sp.SubjectID
		var ws string
		if err := q.QueryRowContext(ctx,
			`SELECT worksource FROM tasks WHERE id = ?`, *sp.SubjectID).Scan(&ws); err != nil {
			return nil, err
		}
		worksource = ws
	}

	// Editor runs snapshot the entire proposed pool at claim (§10 step 3;
	// ADR-001 D4 coverage check reads it back).
	var pool any
	if sp.Role == dispatch.RoleEditor {
		b, err := json.Marshal(sp.ProposedPool)
		if err != nil {
			return nil, err
		}
		pool = string(b)
	}

	// binding='fake': routing.md resolution is [P2]; the skeleton stamps the
	// test-config-only fake family (contract Ambiguity A5).
	if _, err := q.ExecContext(ctx, `
		INSERT INTO runs (id, tier, role, worksource, subject, session_path,
		                  binding, pool_snapshot)
		VALUES (?, 'pipeline', ?, ?, ?, ?, 'fake', ?)`,
		runID, owner, worksource, subject, sessionPath, pool); err != nil {
		return nil, err
	}

	res, err := q.ExecContext(ctx, `
		UPDATE lock SET run_id = ?, worksource = ?, subject = ?, owner = ?,
			acquired_at = datetime('now'), last_heartbeat_at = NULL,
			hard_deadline_at = datetime('now', '+' || ? || ' minutes')
		WHERE id = 1 AND run_id IS NULL`,
		runID, worksource, subject, owner, tun.hardDeadlineMinutes)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		// Unreachable inside BEGIN IMMEDIATE (Decide only spawns on a free
		// lock read in this same transaction); kept as the CAS backstop.
		return nil, Domainf("lost the claim race: lock is held (§10 fencing)")
	}

	poolIDs := sp.ProposedPool
	if poolIDs == nil {
		poolIDs = []int64{}
	}
	var subjectID any
	if sp.SubjectID != nil {
		subjectID = *sp.SubjectID
	}
	return map[string]any{
		"action":               "spawn",
		"run_id":               runID,
		"role":                 string(sp.Role),
		"subject_id":           subjectID,
		"worksource":           worksource,
		"pool_ids":             poolIDs,
		"session_path":         sessionPath,
		"heartbeat_interval_s": tun.heartbeatIntervalS,
	}, nil
}

// applyReap is the step-(0) mutation: mark the run reaped, charge/block the
// subject per budget, free the lease — one transaction; the container stop
// returns as effect data (§10 step 0, §11.6).
func applyReap(ctx context.Context, q Q, r *dispatch.Reap) (map[string]any, error) {
	if err := endRun(ctx, q, r.RunID, "reaped"); err != nil {
		return nil, err
	}
	if r.SubjectID != nil && r.ChargeRetries {
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET dispatch_retries =
				CASE WHEN dispatch_retries > 0 THEN dispatch_retries - 1 ELSE 0 END
			WHERE id = ?`, *r.SubjectID); err != nil {
			return nil, err
		}
		if r.BlockSubject {
			if _, err := q.ExecContext(ctx, `
				UPDATE tasks SET blocked = 1,
					blocked_reason = 'dispatch retries exhausted (' || ? || ')'
				WHERE id = ? AND blocked = 0`, string(r.Reason), *r.SubjectID); err != nil {
				return nil, err
			}
		}
	}
	if err := releaseLease(ctx, q, r.RunID); err != nil {
		return nil, err
	}
	return map[string]any{
		"action":         "reap",
		"run_id":         r.RunID,
		"reason":         string(r.Reason),
		"stop_container": r.StopContainer,
	}, nil
}
