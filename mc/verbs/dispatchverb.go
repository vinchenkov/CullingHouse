package verbs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // the console's IANA zones (§16.3) with no host dependency

	"mc/dispatch"
	"mc/domain"
	"mc/routing"
)

// Dispatch is the one caller of dispatch.Decide (contract §3; §10; Inv. 2/3).
// One BEGIN IMMEDIATE transaction: load Records/Lock from the spine and
// Config from the lock row's tunable columns, read Clock.Now from
// datetime('now'), call Decide, apply the action's writes, commit, and
// return the effect JSON. A losing racer aborts before effect data (§10
// fencing): claim is CAS on the free lock row.
func Dispatch(db *sql.DB) (any, error) {
	processLock, err := acquireDispatchProcessLock(db)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = syscall.Flock(int(processLock.Fd()), syscall.LOCK_UN)
		_ = processLock.Close()
	}()

	var effect map[string]any
	err = inTx(db, func(ctx context.Context, q Q) error {
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
		// Step (0) lease reconciliation outranks every free-lock concern
		// (§10). A held lease never consults the Console clock: Decide either
		// keeps it or reaps it. This prevents corrupt schedule configuration
		// from wedging the global liveness fence.
		loc := time.UTC
		if !lk.Held {
			loc, err = time.LoadLocation(tun.consoleTZ)
			if err != nil {
				// Fail-closed (contract §4.3): once the lock is free, a broken
				// stored zone aborts rather than delivering on a guessed clock.
				return Domainf("lock.console_tz %q is not a loadable IANA zone (§16.3): %v", tun.consoleTZ, err)
			}
		}
		cfg := dispatch.Config{
			ReviewWIPCap:   3, // Inv. 18; the substrate trigger is the backstop
			TimeoutMinutes: tun.timeoutMinutes,
			GraceMinutes:   tun.graceMinutes,
			SpawnGraceS:    tun.spawnGraceS,
			// The stored §16.3 schedule (NOTE(P2.1)); the default hour 24
			// normalizes past end-of-day, so consoleDue never fires until an
			// operator configures a real time (D-mc-4, resolved).
			ConsoleHour:   tun.consoleHour,
			ConsoleMinute: tun.consoleMinute,
			ConsoleLoc:    loc,
		}
		action := dispatch.Decide(rec, lk, cfg, dispatch.Clock{Now: now})
		effect, err = applyAction(ctx, q, now, action, tun)
		return err
	})
	if err != nil {
		return nil, err
	}
	return effect, nil
}

// acquireDispatchProcessLock takes the §10 `mc.dispatch` flock before any
// durable-state evaluation. The lock file lives beside the spine bytes, so it
// is arbitrated by the same container-runtime kernel (Inv. 24). BEGIN
// IMMEDIATE + the lease CAS remain the correctness fence; this process lock
// prevents multiple host invocations from wasting concurrent evaluations.
func acquireDispatchProcessLock(db *sql.DB) (*os.File, error) {
	rows, err := db.Query(`PRAGMA database_list`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spinePath := ""
	for rows.Next() {
		var seq int
		var name, path string
		if err := rows.Scan(&seq, &name, &path); err != nil {
			return nil, err
		}
		if name == "main" {
			spinePath = path
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if spinePath == "" {
		return nil, Domainf("mc dispatch requires a file-backed main spine for the §10 process flock")
	}
	lockPath := filepath.Join(filepath.Dir(spinePath), "mc.dispatch.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, Domainf("open dispatch process flock %q: %v", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, Domainf("acquire dispatch process flock %q: %v", lockPath, err)
	}
	return f, nil
}

// tunables are the lock row's stored knobs (§16.3; contract §2 mc init;
// NOTE(P2.1) console schedule).
type tunables struct {
	timeoutMinutes      int
	graceMinutes        int
	heartbeatIntervalS  int
	spawnGraceS         int
	hardDeadlineMinutes int
	consoleHour         int
	consoleMinute       int
	consoleTZ           string
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
		       heartbeat_interval_s, spawn_grace_s, hard_deadline_minutes,
		       console_hour, console_minute, console_tz
		FROM lock WHERE id = 1`).Scan(
		&runID, &owner, &subject, &acquiredAt, &lastHB, &hardDeadline,
		&tun.timeoutMinutes, &tun.graceMinutes, &tun.heartbeatIntervalS,
		&tun.spawnGraceS, &tun.hardDeadlineMinutes,
		&tun.consoleHour, &tun.consoleMinute, &tun.consoleTZ)
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
		SELECT t.id, t.title, t.scope, t.initiative_id, t.priority, t.created_at, t.status,
		       blocked, dispatch_retries, decision, decided_at, archived,
		       worksource, branch, verified_sha, target_ref, plan_reviewed, w.status
		FROM tasks t JOIN worksources w ON w.id = t.worksource`)
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
			planReviewed                int
			decision, decidedAt         sql.NullString
			branch, verifiedSHA, target sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.Title, &scope, &initiativeID, &t.Priority,
			&createdAt, &status, &blocked, &t.DispatchRetries, &decision,
			&decidedAt, &archived, &t.Worksource, &branch, &verifiedSHA,
			&target, &planReviewed, &t.WorksourceStatus); err != nil {
			return rec, err
		}
		t.Scope = dispatch.Scope(scope)
		t.Status = dispatch.Status(status)
		t.Blocked = blocked == 1
		t.Archived = archived == 1
		t.PlanReviewed = planReviewed == 1 // ADR-020 D2(e)
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
// builds the effect JSON pinned by contract §2. The write bodies live behind
// the domain aggregates (lease.Claim / lease.ApplyReap / task.Reenter —
// phase2-contract §1.3).
func applyAction(ctx context.Context, q Q, now time.Time, a dispatch.Action, tun tunables) (map[string]any, error) {
	switch a.Kind {
	case dispatch.KindIdle:
		return map[string]any{"action": "idle", "reason": string(a.Idle)}, nil

	case dispatch.KindSpawn:
		route, err := resolveSpawnRoute(a.Spawn.Role)
		if err != nil {
			return nil, err
		}
		return applySpawn(ctx, q, now, a.Spawn, tun, route)

	case dispatch.KindReap:
		// The reap decision's charge/block computation is Decide's; the write
		// side (mark reaped → charge → free) is the lease aggregate's, which
		// re-derives blocking from the charge itself (§10 "never a silent
		// loop") — the two agree by construction on the same records.
		reap := domain.ReapArgs{RunID: a.Reap.RunID, Reason: string(a.Reap.Reason)}
		if _, err := domain.ApplyReap(ctx, q, reap); err != nil {
			return nil, err
		}
		return map[string]any{
			"action":         "reap",
			"run_id":         a.Reap.RunID,
			"reason":         string(a.Reap.Reason),
			"stop_container": a.Reap.StopContainer,
		}, nil

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
		// Step (2b), initiative arm: one pure mutation, packaged → seeded via
		// the one re-entry edge (Inv. 11); no spawn this tick, no notes —
		// Strategist(initiative) carries the deepening as waves (§8).
		if err := domain.Reenter(ctx, q, a.Reenter.TaskID, ""); err != nil {
			return nil, err
		}
		return map[string]any{"action": "reenter", "task_id": a.Reenter.TaskID}, nil
	}
	return nil, Domainf("dispatch: unknown action kind %q", a.Kind)
}

// applySpawn is the claim-and-spawn behind lease.Claim: CAS the free lock +
// INSERT the runs row (Inv. 4), one transaction.
func applySpawn(ctx context.Context, q Q, now time.Time, sp *dispatch.Spawn, tun tunables, route routing.Route) (map[string]any, error) {
	runID, err := newRunID()
	if err != nil {
		return nil, err
	}
	owner := baseRole(string(sp.Role))
	sessionPath := "sessions/" + runID // MC_HOME-relative (§16.1)
	brief, err := buildSpawnBrief(ctx, q, sp)
	if err != nil {
		return nil, err
	}

	// Both Editor modes snapshot at claim the exact id set they were shown and
	// must act on (§10 step 3; ADR-001 D4 and ADR-020 D5 read it back):
	// runs.pool_snapshot carries the proposed pool for the contrastive pass and
	// the wave for the holistic plan review. A switch, not an if-chain, so the
	// two modes are visibly exhaustive — omitting one here computes the set in
	// the pure layer and silently discards it at the seam.
	var pool []int64
	switch sp.Role {
	case dispatch.RoleEditor:
		pool = sp.ProposedPool
		if pool == nil {
			pool = []int64{}
		}
	case dispatch.RoleEditorPlanReview:
		pool = sp.Wave // ADR-020 D4
		if pool == nil {
			pool = []int64{}
		}
	}

	claim, err := domain.Claim(ctx, q, now, domain.ClaimArgs{
		RunID:               runID,
		Owner:               owner,
		SubjectID:           sp.SubjectID,
		SessionPath:         sessionPath,
		Binding:             route.HistoricalBinding(),
		PoolSnapshot:        pool,
		HardDeadlineMinutes: tun.hardDeadlineMinutes,
	})
	if err != nil {
		return nil, err
	}

	poolIDs := sp.ProposedPool
	if poolIDs == nil {
		poolIDs = []int64{}
	}
	var subjectID any
	if sp.SubjectID != nil {
		subjectID = *sp.SubjectID
	}
	var worksource any
	if claim.Worksource != nil {
		worksource = *claim.Worksource
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
		"harness":              route.Harness,
		"model_binding":        route.Binding,
		"brief":                brief,
	}, nil
}

// resolveSpawnRoute reads the one authoritative role map. It is deliberately
// called only for Spawn: reap/land/reenter reconciliation must remain usable
// while routing is being repaired. Parse/validation completes before
// lease.Claim, so an invalid route opens no Run and returns no spawn effect.
func resolveSpawnRoute(role dispatch.Role) (routing.Route, error) {
	return resolveRoleRoute(baseRole(string(role)))
}

// resolveRoleRoute reads the authoritative route for both leased pipeline
// spawns and the lease-free Homie registry. Capturing Homie's exact historical
// binding at start makes later resume independent of routing.md drift (§15.4).
func resolveRoleRoute(role string) (routing.Route, error) {
	home := os.Getenv("MC_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return routing.Route{}, Usagef("resolve default MC_HOME for routing.md: %v (run: mc onboard routing)", err)
		}
		home = filepath.Join(userHome, ".mission-control")
	}
	if !filepath.IsAbs(home) {
		return routing.Route{}, Usagef("MC_HOME must be absolute to resolve routing.md, got %q (run: mc onboard routing)", home)
	}
	path := filepath.Join(filepath.Clean(home), "routing.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return routing.Route{}, Usagef("read routing.md at %q: %v (run: mc onboard routing)", path, err)
	}
	registry, allowFakeDecorrelation := routing.ActiveRegistry()
	table, err := routing.Parse(data, registry, allowFakeDecorrelation)
	if err != nil {
		return routing.Route{}, Domainf("invalid routing.md at %q: %v (run: mc onboard routing)", path, err)
	}
	resolved, err := table.Resolve(baseRole(role))
	if err != nil {
		return routing.Route{}, Domainf("invalid routing.md at %q: %v (run: mc onboard routing)", path, err)
	}
	return resolved, nil
}
