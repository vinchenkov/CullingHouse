-- Mission Control spine schema — Phase 1a substrate.
--
-- Behavioral contract: specs/mission-control-spec.md (§4 canonical state,
-- §5 core entities, §6/§6.1 state machine, §7 decisions, §8 WIP cap and
-- saturation, §10 lock/runs, §15.4/§15.5 Homie state and outbox, §16.4 meta).
--
-- This file is the redundant backstop layer (spec §4 "make impossible states
-- impossible"): a bug in mc cannot write an illegal state because the storage
-- rejects it. Real domain enforcement lives above, in the Phase 2 layer;
-- every rule here must hold standalone.
--
-- Connection discipline (spec §10, validated by spike S5): the schema is
-- WAL-compatible and every connection must open with
--   _pragma=busy_timeout(N) & _pragma=journal_mode(WAL) & _pragma=foreign_keys(1)
-- and run every mutation under BEGIN IMMEDIATE. The trigger lattice below is
-- deliberately recursive_triggers-agnostic: no trigger depends on another
-- trigger firing from inside its body — every cascade writes all of its
-- downstream effects directly (NOTE(P1.12) in NOTES.md).

------------------------------------------------------------------------------
-- meta — spine identity (spec §16.4). The singleton row is written by
-- first-ever provisioning (mc onboard), never seeded here (NOTE(P1.15)).
------------------------------------------------------------------------------

CREATE TABLE meta (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    deployment_uuid TEXT    NOT NULL,
    schema_version  INTEGER NOT NULL,
    created_at      TEXT    NOT NULL DEFAULT (datetime('now'))
);

------------------------------------------------------------------------------
-- worksources / sandbox_profiles — infrastructure records (spec §5).
------------------------------------------------------------------------------

CREATE TABLE sandbox_profiles (
    id                    TEXT PRIMARY KEY,
    workspace_root        TEXT NOT NULL,
    artifact_roots        TEXT,                -- JSON array of paths
    readonly_mounts       TEXT,                -- JSON array of paths
    denied_paths          TEXT,                -- JSON array of paths
    tool_home_dir         TEXT,
    runtime_control_dir   TEXT,
    harness_env_policy    TEXT,
    tool_env_policy       TEXT,
    egress_policy         TEXT NOT NULL DEFAULT 'open+audit'
                          CHECK (egress_policy IN ('none', 'allowlist', 'open+audit')),
    network_allow         TEXT NOT NULL DEFAULT '[]',  -- JSON array of host:port rules; empty = deny all non-proxy TCP (§5)
    tool_allowlist        TEXT,
    runtime_auth_delivery TEXT NOT NULL DEFAULT 'gateway'
                          CHECK (runtime_auth_delivery IN ('gateway', 'materialized'))
);

CREATE TABLE worksources (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('repo', 'personal', 'transient')),
    jurisdiction    TEXT,
    sandbox_profile TEXT REFERENCES sandbox_profiles(id),
    directive       TEXT,
    seeding_mode    TEXT NOT NULL DEFAULT 'propose-only'
                    CHECK (seeding_mode IN ('propose-only', 'auto')),
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'paused', 'archived'))
);

------------------------------------------------------------------------------
-- tasks — the one work table (spec §5): proposals, ordinary tasks,
-- initiatives, and wave children are all rows here.
------------------------------------------------------------------------------

CREATE TABLE tasks (
    id               INTEGER PRIMARY KEY,
    title            TEXT    NOT NULL,
    description      TEXT,                    -- for an initiative: the charter (Inv. 12)
    scope            TEXT    NOT NULL DEFAULT 'task'
                     CHECK (scope IN ('task', 'initiative')),
    initiative_id    INTEGER REFERENCES tasks(id),  -- set on wave children only
    priority         INTEGER NOT NULL DEFAULT 2
                     CHECK (priority BETWEEN -1 AND 3),  -- -1 = expedited, 0-3 = P0-P3
    created_at       TEXT    NOT NULL DEFAULT (datetime('now')),
    status           TEXT    NOT NULL DEFAULT 'proposed'
                     CHECK (status IN ('proposed', 'seeded', 'worked', 'verified', 'packaged')),
    stage_entered_at TEXT    NOT NULL DEFAULT (datetime('now')),
    correction_count INTEGER NOT NULL DEFAULT 0
                     CHECK (correction_count BETWEEN 0 AND 3),  -- rally budget (§4, §7)
    blocked          INTEGER NOT NULL DEFAULT 0 CHECK (blocked IN (0, 1)),
    blocked_reason   TEXT,
    dispatch_retries INTEGER NOT NULL DEFAULT 3 CHECK (dispatch_retries >= 0),
    decision         TEXT    CHECK (decision IN ('approved', 'rejected', 'cancelled')),
    decided_at       TEXT,
    archived         INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    origin           TEXT    NOT NULL DEFAULT 'autonomous'
                     CHECK (origin IN ('user', 'autonomous')),
    worksource       TEXT    NOT NULL REFERENCES worksources(id),

    -- Landing inputs (§7): landing-pending is DERIVED state — approved +
    -- branch-carrying + unarchived (+ not blocked, NOTE(S6.3)); there is no
    -- separate mark column (NOTE(P1.9)).
    branch           TEXT,
    verified_sha     TEXT,
    target_ref       TEXT,

    -- NOTE(P2.3): the carried notes of §7 ("Revise → … notes carried into
    -- the next run brief") and §8's Refiner deepening scope. Written by the
    -- domain re-entry (task.Reenter), overwritten per re-entry, read at
    -- spawn-brief assembly, cleared on the next packaging (A-P2-6).
    refine_notes     TEXT,

    -- stage_rank: how far into the pipeline the row is, for furthest-first
    -- dispatch (§5, §10). packaged ranks 0 — in the review queue, out of
    -- pipeline dispatch. Generated: unwritable by construction.
    stage_rank       INTEGER GENERATED ALWAYS AS (
                         CASE status
                             WHEN 'packaged' THEN 0
                             WHEN 'proposed' THEN 1
                             WHEN 'seeded'   THEN 2
                             WHEN 'worked'   THEN 3
                             WHEN 'verified' THEN 4
                         END
                     ) STORED,

    -- Blocked requires a reason (§4).
    CHECK (blocked = 0 OR blocked_reason IS NOT NULL),
    -- No archive without a decision (§4).
    CHECK (archived = 0 OR decision IS NOT NULL),
    -- Decision and timestamp travel together (§4).
    CHECK ((decision IS NULL) = (decided_at IS NULL)),
    -- Approval only of packaged work (§4, §6).
    CHECK (decision <> 'approved' OR status = 'packaged'),
    -- No nested initiatives (§4, §5): a wave child is always scope = 'task'.
    CHECK (initiative_id IS NULL OR scope = 'task')
);

CREATE INDEX tasks_children      ON tasks (initiative_id) WHERE initiative_id IS NOT NULL;
CREATE INDEX tasks_dispatch      ON tasks (archived, blocked, stage_rank, priority, created_at, id);
CREATE INDEX tasks_reject_ledger ON tasks (decided_at DESC) WHERE decision = 'rejected';

-- Birth rules (§6, §6.1): ordinary rows are born 'proposed'; wave children
-- are born 'seeded', only into a live, still-seeded initiative; nothing is
-- born decided or archived (NOTE(P1.2)).
CREATE TRIGGER tasks_birth_rules
BEFORE INSERT ON tasks
BEGIN
    SELECT CASE
        WHEN NEW.initiative_id IS NULL AND NEW.status <> 'proposed'
            THEN RAISE(ABORT, 'tasks are born proposed (§6)')
        WHEN NEW.initiative_id IS NOT NULL AND NEW.status <> 'seeded'
            THEN RAISE(ABORT, 'wave children are born seeded (§6.1)')
        WHEN NEW.initiative_id IS NOT NULL AND NOT EXISTS (
                SELECT 1 FROM tasks p
                WHERE p.id = NEW.initiative_id
                  AND p.scope = 'initiative'
                  AND p.status = 'seeded'
                  AND p.archived = 0)
            THEN RAISE(ABORT, 'wave children are born only into a live, still-seeded initiative (§6.1)')
        WHEN NEW.decision IS NOT NULL OR NEW.archived <> 0
            THEN RAISE(ABORT, 'tasks are born undecided and unarchived (NOTE(P1.2))')
    END;
END;

-- The §6 state machine, both scopes: only the six legal edges move a row.
-- Self-assignment (status unchanged) is a no-op, not a transition (NOTE(P1.1)).
CREATE TRIGGER tasks_legal_transitions
BEFORE UPDATE OF status ON tasks
WHEN OLD.status <> NEW.status
  AND NOT (   (OLD.status = 'proposed' AND NEW.status = 'seeded')    -- promote
           OR (OLD.status = 'seeded'   AND NEW.status = 'worked')    -- work / plan / declare done
           OR (OLD.status = 'worked'   AND NEW.status = 'verified')  -- verify pass
           OR (OLD.status = 'worked'   AND NEW.status = 'seeded')    -- correction rally
           OR (OLD.status = 'verified' AND NEW.status = 'packaged')  -- package
           OR (OLD.status = 'packaged' AND NEW.status = 'seeded'))   -- refinement / operator revise
BEGIN
    SELECT RAISE(ABORT, 'illegal status transition (§6)');
END;

-- Archived rows are terminal: no status movement ever again (NOTE(P1.4)).
CREATE TRIGGER tasks_archived_are_terminal
BEFORE UPDATE OF status ON tasks
WHEN OLD.status <> NEW.status AND OLD.archived = 1
BEGIN
    SELECT RAISE(ABORT, 'archived rows are terminal: no status transitions (§6)');
END;

-- Decided rows never transition (§6: a rejected/cancelled row leaves the pool
-- only through archive; an approved row holds at packaged through landing).
-- Without this, a decided-but-unarchived row could ride the pipeline with its
-- decision still set and be selected by the §10 with-room query (NOTE(P1.4)).
CREATE TRIGGER tasks_decided_never_move
BEFORE UPDATE OF status ON tasks
WHEN OLD.status <> NEW.status AND OLD.decision IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'decided rows never transition (§6)');
END;

-- No resurrection (§6: archived rows are invisible to everything; no spec
-- flow un-archives a task). Fail-closed: un-archiving would re-derive
-- landing-pending on an already-landed row (NOTE(P1.9)/NOTE(S6.3)) and dodge
-- strict drain and the leverage ledger (NOTE(P1.4)).
CREATE TRIGGER tasks_no_unarchive
BEFORE UPDATE OF archived ON tasks
WHEN OLD.archived = 1 AND NEW.archived = 0
BEGIN
    SELECT RAISE(ABORT, 'archived tasks are never un-archived (§6)');
END;

-- Terminal bookkeeping is itself terminal: the decision on an archived row is
-- the leverage-ledger record (§5) and never rewrites (NOTE(P1.4), §18 threat
-- model — same provenance class as tasks_identity_immutable).
CREATE TRIGGER tasks_archived_decision_frozen
BEFORE UPDATE OF decision, decided_at ON tasks
WHEN OLD.archived = 1
  AND (NEW.decision IS NOT OLD.decision OR NEW.decided_at IS NOT OLD.decided_at)
BEGIN
    SELECT RAISE(ABORT, 'an archived row''s decision record is frozen (§5)');
END;

-- Strict drain (§6.1): an initiative may declare done (seeded → worked) only
-- when every child is archived.
CREATE TRIGGER initiatives_declare_requires_drained
BEFORE UPDATE OF status ON tasks
WHEN OLD.scope = 'initiative'
  AND OLD.status = 'seeded' AND NEW.status = 'worked'
  AND EXISTS (SELECT 1 FROM tasks c WHERE c.initiative_id = OLD.id AND c.archived = 0)
BEGIN
    SELECT RAISE(ABORT, 'strict drain: initiative has open children (§6.1)');
END;

-- Identity columns never move after birth (NOTE(P1.5)): re-parenting or
-- re-scoping a row would dodge the birth rules and corrupt drain/cascade.
CREATE TRIGGER tasks_identity_immutable
BEFORE UPDATE ON tasks
WHEN NEW.id <> OLD.id
  OR NEW.scope <> OLD.scope
  OR NEW.initiative_id IS NOT OLD.initiative_id
  OR NEW.origin <> OLD.origin
  OR NEW.worksource <> OLD.worksource
  OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'task identity columns are immutable (NOTE(P1.5))');
END;

-- stage_entered_at follows the transition (§5), stamped with the lock
-- domain's own clock (§10) (NOTE(P1.11)).
CREATE TRIGGER tasks_stamp_stage_entered
AFTER UPDATE OF status ON tasks
WHEN OLD.status <> NEW.status
BEGIN
    UPDATE tasks SET stage_entered_at = datetime('now') WHERE id = NEW.id;
END;

-- A propagated initiative block cannot be manually defeated while any live
-- child remains blocked (§6.1 maximally strict blocking).
CREATE TRIGGER initiatives_unblock_requires_children_clear
BEFORE UPDATE OF blocked ON tasks
WHEN OLD.scope = 'initiative' AND OLD.blocked = 1 AND NEW.blocked = 0
  AND EXISTS (
      SELECT 1 FROM tasks c
      WHERE c.initiative_id = OLD.id AND c.blocked = 1 AND c.archived = 0)
BEGIN
    SELECT RAISE(ABORT, 'initiative cannot unblock while a live child remains blocked (§6.1)');
END;

-- Unblock clears the reason (§6: unblocking resumes exactly where it
-- stopped; a stale question must not linger on the row).
CREATE TRIGGER tasks_unblock_clears_reason
AFTER UPDATE OF blocked ON tasks
WHEN OLD.blocked = 1 AND NEW.blocked = 0 AND NEW.blocked_reason IS NOT NULL
BEGIN
    UPDATE tasks SET blocked_reason = NULL WHERE id = NEW.id;
END;

-- Maximally strict blocking (§6.1), propagation half: one blocked child
-- blocks the whole initiative, reason 'blocked child #N'. An initiative
-- already blocked (operator-set or an earlier child) is left untouched.
CREATE TRIGGER children_block_propagates
AFTER UPDATE OF blocked ON tasks
WHEN NEW.initiative_id IS NOT NULL AND NEW.archived = 0
  AND OLD.blocked = 0 AND NEW.blocked = 1
BEGIN
    UPDATE tasks
    SET blocked = 1, blocked_reason = 'blocked child #' || NEW.id
    WHERE id = NEW.initiative_id AND blocked = 0 AND archived = 0;
END;

CREATE TRIGGER children_block_propagates_on_birth
AFTER INSERT ON tasks
WHEN NEW.initiative_id IS NOT NULL AND NEW.blocked = 1 AND NEW.archived = 0
BEGIN
    UPDATE tasks
    SET blocked = 1, blocked_reason = 'blocked child #' || NEW.id
    WHERE id = NEW.initiative_id AND blocked = 0 AND archived = 0;
END;

-- Auto-clear half (§6.1): the propagated block (and only the propagated
-- block — an operator-set reason never auto-clears) lifts when the last
-- blocked child is resolved (unblocked) or cancelled (archived).
CREATE TRIGGER children_block_clears_on_unblock
AFTER UPDATE OF blocked ON tasks
WHEN NEW.initiative_id IS NOT NULL AND OLD.blocked = 1 AND NEW.blocked = 0
BEGIN
    UPDATE tasks
    SET blocked = 0, blocked_reason = NULL
    WHERE id = NEW.initiative_id
      AND blocked = 1
      AND blocked_reason LIKE 'blocked child #%'
      AND NOT EXISTS (
          SELECT 1 FROM tasks c
          WHERE c.initiative_id = NEW.initiative_id
            AND c.blocked = 1 AND c.archived = 0);
END;

CREATE TRIGGER children_block_clears_on_archive
AFTER UPDATE OF archived ON tasks
WHEN NEW.initiative_id IS NOT NULL AND OLD.archived = 0 AND NEW.archived = 1
  AND NEW.blocked = 1
BEGIN
    UPDATE tasks
    SET blocked = 0, blocked_reason = NULL
    WHERE id = NEW.initiative_id
      AND blocked = 1
      AND blocked_reason LIKE 'blocked child #%'
      AND NOT EXISTS (
          SELECT 1 FROM tasks c
          WHERE c.initiative_id = NEW.initiative_id
            AND c.blocked = 1 AND c.archived = 0);
END;

-- Cascade (§6.1): archiving an initiative cancels its open children and
-- archives their packets with them. All downstream effects are written
-- directly in this body (NOTE(P1.12)).
CREATE TRIGGER initiatives_archive_cascade
AFTER UPDATE OF archived ON tasks
WHEN NEW.scope = 'initiative' AND OLD.archived = 0 AND NEW.archived = 1
BEGIN
    UPDATE tasks
    SET decision   = 'cancelled',
        decided_at = datetime('now'),
        archived   = 1
    WHERE initiative_id = NEW.id AND archived = 0;
    -- Keep the lattice recursive_triggers-agnostic: write the downstream
    -- packet effect explicitly, but only after its owning child is archived
    -- so packets_archive_requires_task_archive remains true.
    UPDATE review_packets
    SET archived = 1
    WHERE archived = 0
      AND task_id IN (SELECT id FROM tasks WHERE initiative_id = NEW.id AND archived = 1);
END;

-- Archiving any task archives its packet: the packet's life ends with the
-- operator decision that archives the row (Inv. 11, §7) (NOTE(P1.6)).
CREATE TRIGGER tasks_archive_cascades_packet
AFTER UPDATE OF archived ON tasks
WHEN OLD.archived = 0 AND NEW.archived = 1
BEGIN
    UPDATE review_packets SET archived = 1 WHERE task_id = NEW.id AND archived = 0;
END;

-- The record is forever (§5: rejected rows are the leverage ledger; Inv. 10:
-- dispatch is recomputed from the records) (NOTE(P1.10)).
CREATE TRIGGER tasks_no_delete
BEFORE DELETE ON tasks
BEGIN
    SELECT RAISE(ABORT, 'task rows are never deleted');
END;

------------------------------------------------------------------------------
-- review_packets — the operator-facing queue (spec §5, §8). Exactly one per
-- task, for life (Inv. 11): task_id is both primary key and foreign key.
------------------------------------------------------------------------------

CREATE TABLE review_packets (
    task_id      INTEGER PRIMARY KEY REFERENCES tasks(id),
    render_path  TEXT,
    thesis       TEXT,
    refine_streak INTEGER NOT NULL DEFAULT 0 CHECK (refine_streak >= 0),
    saturated    INTEGER NOT NULL DEFAULT 0 CHECK (saturated IN (0, 1)),
    archived     INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    created_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX packets_queue ON review_packets (archived, saturated, created_at);

-- A packet is born only from a live packaged task (§5, Inv. 11).
CREATE TRIGGER packets_require_packaged
BEFORE INSERT ON review_packets
WHEN NOT EXISTS (
    SELECT 1 FROM tasks t
    WHERE t.id = NEW.task_id AND t.status = 'packaged'
      AND t.decision IS NULL AND t.archived = 0)
BEGIN
    SELECT RAISE(ABORT, 'a packet is born only from a packaged task (Inv. 11)');
END;

-- Review-only state writes (§7, Inv. 11/17). Re-entry and approval are
-- legal only while the task's one Review Packet is live. A branch-carrying
-- approval must already carry the complete immutable landing fence.
CREATE TRIGGER tasks_reentry_requires_live_packet
BEFORE UPDATE OF status ON tasks
WHEN OLD.status = 'packaged' AND NEW.status = 'seeded'
  AND NOT EXISTS (
      SELECT 1 FROM review_packets p
      WHERE p.task_id = OLD.id AND p.archived = 0)
BEGIN
    SELECT RAISE(ABORT, 'packaged re-entry requires a live Review Packet (Inv. 11, Inv. 17)');
END;

CREATE TRIGGER tasks_approve_requires_live_packet
BEFORE UPDATE OF decision ON tasks
WHEN NEW.decision = 'approved'
  AND NOT EXISTS (
      SELECT 1 FROM review_packets p
      WHERE p.task_id = OLD.id AND p.archived = 0)
BEGIN
    SELECT RAISE(ABORT, 'approval requires a live Review Packet (Inv. 11, Inv. 17)');
END;

CREATE TRIGGER tasks_approve_requires_landing_fence
BEFORE UPDATE OF decision ON tasks
WHEN NEW.decision = 'approved' AND NEW.branch IS NOT NULL AND NEW.branch <> ''
  AND (NEW.verified_sha IS NULL OR NEW.verified_sha = ''
       OR NEW.target_ref IS NULL OR NEW.target_ref = '')
BEGIN
    SELECT RAISE(ABORT, 'branch approval requires verified_sha and target_ref (§7 landing fence)');
END;

-- Packets are born live, into the queue (Inv. 11): a born-archived packet
-- would dodge the WIP cap's birth arm while permanently consuming the task's
-- one-packet-for-life slot — no spec flow produces it (NOTE(P1.2) symmetry).
CREATE TRIGGER packets_born_live
BEFORE INSERT ON review_packets
WHEN NEW.archived <> 0
BEGIN
    SELECT RAISE(ABORT, 'packets are born live, into the queue (Inv. 11)');
END;

-- A decided packet is the immutable operator record. Re-packaging may
-- replace the render only while the owning task is still undecided; a late
-- or buggy caller cannot rewrite what was approved/cancelled (§5, Inv. 11).
CREATE TRIGGER packets_decided_task_render_frozen
BEFORE UPDATE OF render_path ON review_packets
WHEN EXISTS (
    SELECT 1 FROM tasks t
    WHERE t.id = OLD.task_id AND t.decision IS NOT NULL)
BEGIN
    SELECT RAISE(ABORT, 'a decided task packet cannot be rerendered (§5, Inv. 11)');
END;

-- Review WIP cap (Inv. 18, §8): at most 3 unarchived packets, enforced where
-- it cannot be cheated — at packet birth. The cap value is the shipped
-- default REVIEW_WIP_CAP; retuning it is a schema migration (NOTE(P1.3)).
CREATE TRIGGER packets_wip_cap
BEFORE INSERT ON review_packets
WHEN NEW.archived = 0
  AND (SELECT COUNT(*) FROM review_packets WHERE archived = 0) >= 3
BEGIN
    SELECT RAISE(ABORT, 'review WIP cap: at most 3 unarchived packets (Inv. 18)');
END;

-- A packet holds its queue slot until the owning task is operator-decided and
-- archived (Inv. 11/18). Task-archive cascade triggers are the only legal
-- writer of packet.archived; a direct packet update could otherwise evade
-- backpressure while leaving undecided work live.
CREATE TRIGGER packets_archive_requires_task_archive
BEFORE UPDATE OF archived ON review_packets
WHEN OLD.archived = 0 AND NEW.archived = 1
  AND NOT EXISTS (
      SELECT 1 FROM tasks t WHERE t.id = NEW.task_id AND t.archived = 1)
BEGIN
    SELECT RAISE(ABORT, 'packet archive requires its owning task to be archived (Inv. 11)');
END;

-- Archival is one-way. No spec flow resurrects a packet or reuses its spent
-- queue slot; refinement keeps the original packet live throughout (§8).
CREATE TRIGGER packets_never_unarchive
BEFORE UPDATE OF archived ON review_packets
WHEN OLD.archived = 1 AND NEW.archived = 0
BEGIN
    SELECT RAISE(ABORT, 'a review packet can never be unarchived (Inv. 11)');
END;

-- Saturation is computed, never hand-set (§8): refine_streak >= 3 saturates.
CREATE TRIGGER packets_saturate
AFTER UPDATE OF refine_streak ON review_packets
WHEN NEW.refine_streak >= 3 AND NEW.saturated = 0
BEGIN
    UPDATE review_packets SET saturated = 1 WHERE task_id = NEW.task_id;
END;

CREATE TRIGGER packets_saturate_on_birth
AFTER INSERT ON review_packets
WHEN NEW.refine_streak >= 3 AND NEW.saturated = 0
BEGIN
    UPDATE review_packets SET saturated = 1 WHERE task_id = NEW.task_id;
END;

-- A saturated packet's streak never decreases (§8): refinement never
-- dispatches on saturated = 1 (§10 step 2b), so no genuine-deepening reset
-- can legitimately occur there — a streak decrease is only ever the first
-- half of a two-step hand-clear (NOTE(P1.8)).
CREATE TRIGGER packets_saturated_streak_frozen
BEFORE UPDATE OF refine_streak ON review_packets
WHEN OLD.saturated = 1 AND NEW.refine_streak < OLD.refine_streak
BEGIN
    SELECT RAISE(ABORT, 'saturated is computed, never hand-set (§8)');
END;

-- Hand-setting saturated out of line with the streak aborts, in both
-- directions (NOTE(P1.8)). The computed path above satisfies this guard.
CREATE TRIGGER packets_saturated_never_hand_set
BEFORE UPDATE OF saturated ON review_packets
WHEN (OLD.saturated = 0 AND NEW.saturated = 1 AND NEW.refine_streak < 3)
  OR (OLD.saturated = 1 AND NEW.saturated = 0 AND NEW.refine_streak >= 3)
BEGIN
    SELECT RAISE(ABORT, 'saturated is computed, never hand-set (§8)');
END;

CREATE TRIGGER packets_saturated_birth_guard
BEFORE INSERT ON review_packets
WHEN NEW.saturated = 1 AND NEW.refine_streak < 3
BEGIN
    SELECT RAISE(ABORT, 'saturated is computed, never hand-set (§8)');
END;

-- A packet holds its queue slot for life (Inv. 11); the row never leaves.
CREATE TRIGGER packets_no_delete
BEFORE DELETE ON review_packets
BEGIN
    SELECT RAISE(ABORT, 'packet rows are never deleted (Inv. 11)');
END;

------------------------------------------------------------------------------
-- lock — the single execution lease (spec §10). Singleton row, CAS-only;
-- free when run_id IS NULL.
------------------------------------------------------------------------------

CREATE TABLE lock (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    run_id               TEXT,
    worksource           TEXT REFERENCES worksources(id),
    subject              INTEGER REFERENCES tasks(id),  -- nullable: propose-style runs (§10)
    owner                TEXT CHECK (owner IN
                         ('strategist', 'editor', 'worker', 'verifier', 'packager', 'refiner')),
    acquired_at          TEXT,
    last_heartbeat_at    TEXT,
    hard_deadline_at     TEXT,   -- non-renewable; no heartbeat can extend it (Inv. 1)
    timeout_minutes      INTEGER NOT NULL DEFAULT 60,
    grace_minutes        INTEGER NOT NULL DEFAULT 15,
    heartbeat_interval_s INTEGER NOT NULL DEFAULT 60,
    spawn_grace_s        INTEGER NOT NULL DEFAULT 60,
    -- hard_deadline_minutes: the tunable mc dispatch adds to acquired_at when
    -- stamping hard_deadline_at at claim (Inv. 1; §10). Phase 1b contract §2
    -- pins it as an `mc init` tunable stored on the lock row alongside the
    -- other lease tunables (NOTE(P1b.1): additive column, default preserves
    -- every Phase 1a behavior).
    hard_deadline_minutes INTEGER NOT NULL DEFAULT 240,
    -- NOTE(P2.1): the Daily Console stored schedule (§16.3 "delivery time +
    -- IANA timezone"), lock-row tunable pattern of NOTE(P1b.1). The default
    -- hour 24 normalizes past end-of-day, so consoleDue never fires — the
    -- stored not-configured value, replacing the Phase 1b hardcoded sentinel
    -- (deviation D-mc-4; phase2-contract §4.3). Settable via `mc init`.
    console_hour          INTEGER NOT NULL DEFAULT 24
                          CHECK (console_hour BETWEEN 0 AND 24),
    console_minute        INTEGER NOT NULL DEFAULT 0
                          CHECK (console_minute BETWEEN 0 AND 59),
    console_tz            TEXT    NOT NULL DEFAULT 'UTC',

    -- Claim fields travel together; a free lock carries no run residue.
    CHECK ((run_id IS NULL) = (owner IS NULL)),
    CHECK ((run_id IS NULL) = (acquired_at IS NULL)),
    CHECK ((run_id IS NULL) = (hard_deadline_at IS NULL)),
    CHECK (run_id IS NOT NULL OR subject IS NULL),
    CHECK (run_id IS NOT NULL OR last_heartbeat_at IS NULL),
    CHECK (run_id IS NOT NULL OR worksource IS NULL)
);

INSERT INTO lock (id) VALUES (1);

CREATE TRIGGER lock_singleton_no_delete
BEFORE DELETE ON lock
BEGIN
    SELECT RAISE(ABORT, 'the lock singleton is never deleted (§10)');
END;

------------------------------------------------------------------------------
-- runs — one row per dispatched run, committed before the process starts
-- (Inv. 4); carries the four session-folder locators (§9, §15.4).
------------------------------------------------------------------------------

CREATE TABLE runs (
    id                 TEXT PRIMARY KEY,
    tier               TEXT NOT NULL CHECK (tier IN ('pipeline', 'homie')),
    role               TEXT CHECK (role IN
                       ('strategist', 'editor', 'worker', 'verifier', 'packager', 'refiner', 'homie')),
    worksource         TEXT REFERENCES worksources(id),
    subject            INTEGER REFERENCES tasks(id),
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    ended_at           TEXT,
    outcome            TEXT,
    session_path       TEXT,   -- where the session folder is
    binding            TEXT,   -- immutable "harness/binding" locator for the runtime that wrote it (§9)
    native_session_ref TEXT,   -- the harness's own session handle
    trace_filename     TEXT,   -- the trace's filename
    pool_snapshot      TEXT,   -- Editor runs: JSON array of the proposed pool
                               -- snapshotted at claim; `mc editor decide` must
                               -- cover it exactly (ADR-001 D4; Phase 1b
                               -- contract §2, NOTE(P1b.2))

    -- NOTE(P2.2): the verdict record lives on the Verifier's own runs row
    -- (ADR-001 D4 "one transaction writes the verdict record (gate-ladder
    -- evidence path included)"); the next Worker brief's correction file is
    -- queried from the subject's latest correct-verdict run — no task column.
    verdict_outcome    TEXT CHECK (verdict_outcome IN ('pass', 'correct', 'budget-spent')),
    evidence_path      TEXT,
    correction_path    TEXT,
    deepening          TEXT CHECK (deepening IN ('genuine', 'churn'))
);

-- Every run's trace is kept forever (Inv. 26); so is its row.
CREATE TRIGGER runs_no_delete
BEFORE DELETE ON runs
BEGIN
    SELECT RAISE(ABORT, 'run rows are never deleted (Inv. 26)');
END;

------------------------------------------------------------------------------
-- activity — the append-only log (Inv. 7). actor is the logical originator;
-- the physical writer is always mc.
------------------------------------------------------------------------------

CREATE TABLE activity (
    id         INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    actor      TEXT NOT NULL,
    kind       TEXT NOT NULL,   -- e.g. 'daily.briefing' (§10 step 0b, §14)
    subject    TEXT,
    detail     TEXT
);

CREATE INDEX activity_by_kind ON activity (kind, created_at);

CREATE TRIGGER activity_append_only_update
BEFORE UPDATE ON activity
BEGIN
    SELECT RAISE(ABORT, 'activity is append-only (Inv. 7)');
END;

CREATE TRIGGER activity_append_only_delete
BEFORE DELETE ON activity
BEGIN
    SELECT RAISE(ABORT, 'activity is append-only (Inv. 7)');
END;

------------------------------------------------------------------------------
-- Homie tier: session registry, bindings, conversation rows (spec §15.4).
------------------------------------------------------------------------------

CREATE TABLE homie_sessions (
    id                 TEXT PRIMARY KEY,   -- also the session's run_id
    status             TEXT NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'ended', 'reaped')),
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    last_activity_at   TEXT NOT NULL DEFAULT (datetime('now')),
    container_name     TEXT,
    verb_allowlist     TEXT,   -- frozen at mc homie start (§15.3)
    session_path       TEXT,   -- the four locators (§9, §15.4)
    binding            TEXT,
    native_session_ref TEXT,
    trace_filename     TEXT
);

-- ended/reaped are not terminal for resumability; the row persists forever.
CREATE TRIGGER homie_sessions_no_delete
BEFORE DELETE ON homie_sessions
BEGIN
    SELECT RAISE(ABORT, 'homie session rows are never deleted (§15.4)');
END;

-- One row per bind *event* (§15.4: active bindings clear when the session
-- ends; a resume "creates a fresh binding" for the requesting surface, and
-- "the row, its bindings history ... persist indefinitely") — so the key is
-- a surrogate id, at most one ACTIVE row per (session, surface, place), and
-- the history is never deleted (NOTE(P1.19)).
CREATE TABLE homie_bindings (
    id          INTEGER PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES homie_sessions(id),
    surface     TEXT NOT NULL CHECK (surface IN ('discord', 'dashboard', 'cli')),
    channel_ref TEXT NOT NULL,
    bound_at    TEXT NOT NULL DEFAULT (datetime('now')),
    active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1))
);

CREATE UNIQUE INDEX bindings_one_active
    ON homie_bindings (session_id, surface, channel_ref) WHERE active = 1;

CREATE TRIGGER homie_bindings_no_delete
BEFORE DELETE ON homie_bindings
BEGIN
    SELECT RAISE(ABORT, 'bindings history persists indefinitely (§15.4)');
END;

-- Conversation rows (§15.4, §15.5): every inbound message and reply is an
-- append-only row with durable conversation identity and stable order,
-- alongside the pending-inbound claim state the Homie runner consumes.
-- Content columns are immutable; only the claim columns mutate (NOTE(P1.13)).
CREATE TABLE conversation_messages (
    id           INTEGER PRIMARY KEY,
    session_id   TEXT    NOT NULL REFERENCES homie_sessions(id),
    seq          INTEGER NOT NULL,           -- stable order within the conversation
    direction    TEXT    NOT NULL CHECK (direction IN ('inbound', 'reply')),
    surface      TEXT    NOT NULL CHECK (surface IN ('discord', 'dashboard', 'cli', 'homie')),
    channel_ref  TEXT,
    body         TEXT    NOT NULL,
    attachments  TEXT,                       -- JSON array of file-plane references (§15.5)
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    claimed_by   TEXT,                       -- fenced, idempotent runner claims (§11.5)
    claimed_at   TEXT,
    completed_at TEXT,
    UNIQUE (session_id, seq)
);

CREATE INDEX conversation_pending ON conversation_messages (session_id, id)
    WHERE direction = 'inbound' AND completed_at IS NULL;

CREATE TRIGGER conversation_content_immutable
BEFORE UPDATE ON conversation_messages
WHEN NEW.id <> OLD.id
  OR NEW.session_id <> OLD.session_id
  OR NEW.seq <> OLD.seq
  OR NEW.direction <> OLD.direction
  OR NEW.surface <> OLD.surface
  OR NEW.channel_ref IS NOT OLD.channel_ref
  OR NEW.body <> OLD.body
  OR NEW.attachments IS NOT OLD.attachments
  OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'conversation rows are append-only; only claim state may change (§15.4)');
END;

CREATE TRIGGER conversation_no_delete
BEFORE DELETE ON conversation_messages
BEGIN
    SELECT RAISE(ABORT, 'conversation rows are append-only (§15.4)');
END;

------------------------------------------------------------------------------
-- outbox — the one system→operator interconnect (spec §15.5). Delivery
-- bookkeeping only; the durable conversation row is never removed by
-- delivery. One row per (event, destination).
------------------------------------------------------------------------------

CREATE TABLE outbox (
    id           INTEGER PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN
                 ('homie_reply', 'homie_echo', 'blocked_alert', 'console', 'health')),
    session_id   TEXT REFERENCES homie_sessions(id),
    surface      TEXT NOT NULL,
    channel_ref  TEXT,
    payload      TEXT NOT NULL,   -- small inline JSON; anything large rides as a path reference
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    delivered_at TEXT
);

CREATE INDEX outbox_undelivered ON outbox (surface, id) WHERE delivered_at IS NULL;
