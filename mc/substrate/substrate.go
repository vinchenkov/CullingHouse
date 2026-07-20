// Package substrate owns the spine schema and the connection discipline —
// Phase 1a of the Mission Control implementation (handoff Part 3, Phase 1(a)).
//
// The schema in schema.sql is the redundant backstop layer of spec §4:
// tables, CHECK constraints, and the trigger lattice that make illegal spine
// states unstorable regardless of what the layers above do. Real domain
// enforcement arrives in Phase 2; every rule here holds standalone and is
// proven by the pure-SQL tests in this package.
package substrate

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// Schema is the full spine DDL: tables, indexes, CHECK constraints, and the
// trigger lattice.
//
//go:embed schema.sql
var Schema string

// Open opens (creating if absent) a spine database at path with the
// S5-validated connection discipline: WAL journal mode, a busy timeout, and
// foreign keys on — applied via DSN pragmas so every pooled connection
// carries them (spike S5 confirmed modernc.org/sqlite applies DSN pragmas on
// every connection path).
//
// The spine must live on a real file in the lock domain (Inv. 24); WAL does
// not work on ":memory:" databases, and the tests deliberately use a temp
// file for that reason.
func Open(path string) (*sql.DB, error) {
	if err := GuardLockDomain(path); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?%s", path, strings.Join([]string{
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
	}, "&"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open spine %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open spine %s: %w", path, err)
	}
	return db, nil
}

// Init applies the full schema to a fresh spine. It is not idempotent by
// design: provisioning happens exactly once (§16.4), and re-initialization of
// a non-empty spine must fail loudly rather than silently re-create.
func Init(db *sql.DB) error {
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("apply spine schema: %w", err)
	}
	return nil
}

// CurrentSchemaVersion is the version Schema describes. Every writer of
// meta.schema_version stamps this constant rather than a literal, so meta
// always names the schema actually on disk (§16.4).
//
// Version 1 is the Phase 1a spine. Version 2 adds ADR-016 Decision 2's
// dispatch replay fences: activity.dispatch_key, the paired
// activity.dispatch_request_id/dispatch_result receipt, and
// outbox.source_activity_id/event_destination_key fan-out identity.
// Version 3 adds ADR-016 Decision 3's homie_sessions launch fencing: the
// current launch/container generation, typed resume debt, and the rows-mode
// prime cutoff/count pairs.
// Version 4 adds the typeof fence triggers over ADR-016 Decision 2's
// activity/outbox replay-key columns, whose v2 hex CHECKs hold only for TEXT.
// Version 5 adds the durable run/task-fenced first-task setup receipt.
// Version 10 adds the durable verifier-run-fenced accepted-seal rebuild receipt.
const CurrentSchemaVersion = 10

// migrationV1ToV2 is ADR-016 Decision 2's storage step, frozen as history.
//
// It is deliberately additive: §16.4 forbids any path that drops or
// re-initializes a spine holding data, so no table is rebuilt and no row is
// copied. That constrains the schema in return — SQLite cannot ALTER a UNIQUE
// column onto an existing table, so schema.sql declares these fences as named
// indexes, which this step creates verbatim. The pairing CHECKs ride on the
// second column of each pair because SQLite has no ADD CONSTRAINT; a column
// CHECK may reference its siblings, so the constraint is identical in force.
//
// The outbox immutability trigger is recreated rather than altered: a v1
// trigger cannot mention columns that did not exist when it was written, and
// leaving it would let the new provenance columns be rewritten after delivery.
//
// This text is frozen. A later v2 -> v3 step is a new constant, never an edit
// to this one: it must keep describing the v1 spines it still has to convert.
const migrationV1ToV2 = `
ALTER TABLE activity ADD COLUMN dispatch_key TEXT
    CHECK (dispatch_key IS NULL OR
           (length(dispatch_key) = 64 AND
            length(CAST(dispatch_key AS BLOB)) = 64 AND
            dispatch_key NOT GLOB '*[^0-9a-f]*'));

ALTER TABLE activity ADD COLUMN dispatch_request_id TEXT
    CHECK (dispatch_request_id IS NULL OR
           (length(dispatch_request_id) = 16 AND
            length(CAST(dispatch_request_id AS BLOB)) = 16 AND
            dispatch_request_id NOT GLOB '*[^0-9a-f]*'));

ALTER TABLE activity ADD COLUMN dispatch_result TEXT
    CHECK (dispatch_result IS NULL OR
           (length(CAST(dispatch_result AS BLOB)) <= 65536 AND
            json_valid(dispatch_result) AND
            json_type(dispatch_result) = 'object'))
    CHECK ((dispatch_request_id IS NULL) = (dispatch_result IS NULL));

CREATE UNIQUE INDEX activity_dispatch_key ON activity (dispatch_key);
CREATE UNIQUE INDEX activity_dispatch_request_id ON activity (dispatch_request_id);

ALTER TABLE outbox ADD COLUMN source_activity_id INTEGER REFERENCES activity(id);

ALTER TABLE outbox ADD COLUMN event_destination_key TEXT
    CHECK (event_destination_key IS NULL OR
           (length(event_destination_key) = 64 AND
            length(CAST(event_destination_key AS BLOB)) = 64 AND
            event_destination_key NOT GLOB '*[^0-9a-f]*'))
    CHECK ((source_activity_id IS NULL) = (event_destination_key IS NULL));

CREATE UNIQUE INDEX outbox_event_destination_key ON outbox (event_destination_key);

DROP TRIGGER outbox_content_immutable;
CREATE TRIGGER outbox_content_immutable
BEFORE UPDATE ON outbox
WHEN NEW.id <> OLD.id
  OR NEW.kind <> OLD.kind
  OR NEW.session_id IS NOT OLD.session_id
  OR NEW.surface <> OLD.surface
  OR NEW.channel_ref IS NOT OLD.channel_ref
  OR NEW.payload <> OLD.payload
  OR NEW.source_activity_id IS NOT OLD.source_activity_id
  OR NEW.event_destination_key IS NOT OLD.event_destination_key
  OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'outbox content is immutable; only delivery bookkeeping advances (§15.5)');
END;
`

// migrationV2ToV3 is ADR-016 Decision 3's storage step: launch fencing on
// homie_sessions, not a plan queue.
//
// Additive like v1 -> v2 (§16.4 forbids rebuilding a spine holding data), and
// under the same SQLite rule: there is no ADD CONSTRAINT, so each pairing
// CHECK rides on the later column of its pair, where it may reference the
// siblings added before it. Every existing row satisfies every CHECK at its
// defaults — a session predating launch fencing carries no launch and no
// debt, which is exactly the shape `homie start` initializes.
//
// No trigger is recreated: the launch/debt columns are mutable liveness
// bookkeeping, deliberately outside homie_sessions' frozen-identity and
// locator-immutability triggers.
//
// This text is frozen. A later v3 -> v4 step is a new constant, never an edit
// to this one: it must keep describing the v2 spines it still has to convert.
const migrationV2ToV3 = `
ALTER TABLE homie_sessions ADD COLUMN current_launch_id TEXT
    CHECK (current_launch_id IS NULL OR
           (typeof(current_launch_id) = 'text' AND
            length(current_launch_id) = 16 AND
            length(CAST(current_launch_id AS BLOB)) = 16 AND
            current_launch_id NOT GLOB '*[^0-9a-f]*'));

ALTER TABLE homie_sessions ADD COLUMN current_launch_mode TEXT
    CHECK (current_launch_mode IS NULL OR
           current_launch_mode IN ('fresh', 'native', 'rows'))
    CHECK ((current_launch_id IS NULL) = (current_launch_mode IS NULL));

ALTER TABLE homie_sessions ADD COLUMN current_prime_through_seq INTEGER
    CHECK (current_prime_through_seq IS NULL OR
           (typeof(current_prime_through_seq) = 'integer' AND
            current_prime_through_seq >= 0))
    CHECK ((current_launch_mode IS 'rows') =
           (current_prime_through_seq IS NOT NULL));

ALTER TABLE homie_sessions ADD COLUMN current_prime_row_count INTEGER
    CHECK (current_prime_row_count IS NULL OR
           (typeof(current_prime_row_count) = 'integer' AND
            current_prime_row_count >= 0))
    CHECK ((current_prime_through_seq IS NULL) =
           (current_prime_row_count IS NULL))
    CHECK (current_prime_row_count IS NULL OR
           ((current_prime_through_seq = 0) =
            (current_prime_row_count = 0)));

ALTER TABLE homie_sessions ADD COLUMN current_container_id TEXT
    CHECK (current_container_id IS NULL OR
           (typeof(current_container_id) = 'text' AND
            length(current_container_id) = 64 AND
            length(CAST(current_container_id AS BLOB)) = 64 AND
            current_container_id NOT GLOB '*[^0-9a-f]*'));

ALTER TABLE homie_sessions ADD COLUMN launch_bound_at TEXT
    CHECK ((current_container_id IS NULL) = (launch_bound_at IS NULL))
    CHECK (launch_bound_at IS NULL OR current_launch_id IS NOT NULL);

ALTER TABLE homie_sessions ADD COLUMN launch_started_at TEXT
    CHECK (launch_started_at IS NULL OR launch_bound_at IS NOT NULL);

ALTER TABLE homie_sessions ADD COLUMN resume_owed INTEGER NOT NULL DEFAULT 0
    CHECK (resume_owed IN (0, 1))
    CHECK (resume_owed = 0 OR current_launch_id IS NULL);

ALTER TABLE homie_sessions ADD COLUMN resume_mode TEXT
    CHECK (resume_mode IS NULL OR resume_mode IN ('native', 'rows'))
    CHECK ((resume_owed = 1) = (resume_mode IS NOT NULL));

ALTER TABLE homie_sessions ADD COLUMN resume_prime_through_seq INTEGER
    CHECK (resume_prime_through_seq IS NULL OR
           (typeof(resume_prime_through_seq) = 'integer' AND
            resume_prime_through_seq >= 0))
    CHECK ((resume_mode IS 'rows') =
           (resume_prime_through_seq IS NOT NULL));

ALTER TABLE homie_sessions ADD COLUMN resume_prime_row_count INTEGER
    CHECK (resume_prime_row_count IS NULL OR
           (typeof(resume_prime_row_count) = 'integer' AND
            resume_prime_row_count >= 0))
    CHECK ((resume_prime_through_seq IS NULL) =
           (resume_prime_row_count IS NULL))
    CHECK (resume_prime_row_count IS NULL OR
           ((resume_prime_through_seq = 0) =
            (resume_prime_row_count = 0)));
`

// migrationV3ToV4 closes the BLOB hole in ADR-016 Decision 2's replay-key
// fences. The v2 hex CHECKs (length + byte-length + GLOB) hold only for TEXT:
// a BLOB bypasses affinity conversion, length(blob) counts bytes, and GLOB
// reads it only to the first NUL — so a BLOB forgery stores as a distinct
// UNIQUE value whose own receipt lookup can never find it, and the replay
// fence fails open. Those columns shipped in the frozen v1 -> v2 step and a
// column CHECK cannot be added afterwards, so the fence is a pair of INSERT
// triggers (activity is append-only; outbox_content_immutable already refuses
// any UPDATE that changes the key, including NULL -> BLOB).
//
// This text is frozen. A later v4 -> v5 step is a new constant, never an edit
// to this one: it must keep describing the v3 spines it still has to convert.
const migrationV3ToV4 = `
CREATE TRIGGER activity_receipt_keys_are_text
BEFORE INSERT ON activity
WHEN (NEW.dispatch_key IS NOT NULL AND typeof(NEW.dispatch_key) != 'text')
  OR (NEW.dispatch_request_id IS NOT NULL AND typeof(NEW.dispatch_request_id) != 'text')
  OR (NEW.dispatch_result IS NOT NULL AND typeof(NEW.dispatch_result) != 'text')
BEGIN
    SELECT RAISE(ABORT, 'dispatch receipt columns must be TEXT; a BLOB bypasses the hex fences (ADR-016 D2)');
END;

CREATE TRIGGER outbox_event_destination_key_is_text
BEFORE INSERT ON outbox
WHEN NEW.event_destination_key IS NOT NULL AND typeof(NEW.event_destination_key) != 'text'
BEGIN
    SELECT RAISE(ABORT, 'outbox.event_destination_key must be TEXT; a BLOB bypasses the hex fence (ADR-016 D2)');
END;
`

// migrationV4ToV5 adds recovery evidence for the resident-owned first task
// skeleton.  It intentionally stores only the returned filesystem identity:
// the mutable absolute host path is reconstructed from the registered
// Worksource and re-attested by the setup action, never persisted in the
// spine.
const migrationV4ToV5 = `
CREATE TABLE task_setup_receipts (
    run_id          TEXT PRIMARY KEY REFERENCES runs(id),
    task_id         INTEGER NOT NULL REFERENCES tasks(id),
    root_device     TEXT NOT NULL
                   CHECK (typeof(root_device) = 'text' AND
                          root_device GLOB '[0-9]*' AND
                          root_device NOT GLOB '*[^0-9]*'),
    root_inode      TEXT NOT NULL
                   CHECK (typeof(root_inode) = 'text' AND
                          root_inode GLOB '[0-9]*' AND
                          root_inode NOT GLOB '*[^0-9]*'),
    root_owner_uid  INTEGER NOT NULL
                   CHECK (typeof(root_owner_uid) = 'integer' AND root_owner_uid >= 0),
    registered_at   TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK (length(root_device) <= 20 AND length(root_inode) <= 20)
);

CREATE TRIGGER task_setup_receipts_immutable
BEFORE UPDATE ON task_setup_receipts
BEGIN
    SELECT RAISE(ABORT, 'task setup receipt is immutable; retry must match its registered identity (ADR-016 D5)');
END;

CREATE TRIGGER task_setup_receipts_no_delete
BEFORE DELETE ON task_setup_receipts
BEGIN
    SELECT RAISE(ABORT, 'task setup receipts are durable recovery evidence (ADR-016 D5)');
END;
`

// migrationV5ToV6 records the first-task closure assignment (ADR-016 D5): the
// pinned base/target SHA, object format, sole branch, deterministic path-free
// task-root key, local repository UUID, and closure digest. It is keyed by
// task, not run, because D5's operative invariant is that a *retry reuses that
// assignment rather than rebasing to a moved target* — and a retry is a new
// run id, so keying by the entity that outlives the run (the task) structurally
// forbids the rebase. The row is durable retry evidence: immutable and
// undeletable, with the same typeof fences the D2 replay keys use so a BLOB
// forgery cannot bypass the hex/format GLOBs. base_sha's length is checked
// against the row's own object_format so a sha1/sha256 mismatch fails closed.
//
// This text is frozen. A later step is a new constant, never an edit to this
// one.
const migrationV5ToV6 = `
CREATE TABLE task_assignments (
    task_id          INTEGER PRIMARY KEY REFERENCES tasks(id),
    target_ref       TEXT NOT NULL
                     CHECK (typeof(target_ref) = 'text' AND length(target_ref) BETWEEN 1 AND 512),
    branch           TEXT NOT NULL
                     CHECK (typeof(branch) = 'text' AND length(branch) BETWEEN 1 AND 512),
    task_root_key    TEXT NOT NULL
                     CHECK (typeof(task_root_key) = 'text' AND length(task_root_key) BETWEEN 1 AND 512),
    object_format    TEXT NOT NULL
                     CHECK (typeof(object_format) = 'text' AND object_format IN ('sha1', 'sha256')),
    base_sha         TEXT NOT NULL
                     CHECK (typeof(base_sha) = 'text' AND
                            base_sha GLOB '[0-9a-f]*' AND base_sha NOT GLOB '*[^0-9a-f]*' AND
                            ((object_format = 'sha1' AND length(base_sha) = 40) OR
                             (object_format = 'sha256' AND length(base_sha) = 64))),
    local_repo_uuid  TEXT NOT NULL
                     CHECK (typeof(local_repo_uuid) = 'text' AND
                            local_repo_uuid GLOB '[0-9a-f-]*' AND local_repo_uuid NOT GLOB '*[^0-9a-f-]*' AND
                            length(local_repo_uuid) BETWEEN 1 AND 64),
    closure_digest   TEXT NOT NULL
                     CHECK (typeof(closure_digest) = 'text' AND length(closure_digest) = 64 AND
                            closure_digest GLOB '[0-9a-f]*' AND closure_digest NOT GLOB '*[^0-9a-f]*'),
    assigned_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TRIGGER task_assignments_immutable
BEFORE UPDATE ON task_assignments
BEGIN
    SELECT RAISE(ABORT, 'task assignment is immutable; a first-task retry reuses its recorded assignment, never rebases (ADR-016 D5)');
END;

CREATE TRIGGER task_assignments_no_delete
BEFORE DELETE ON task_assignments
BEGIN
    SELECT RAISE(ABORT, 'task assignments are durable retry evidence (ADR-016 D5)');
END;
`

// migrationV6ToV7 establishes the durable state machine for a Worker
// completion seal. Published is not authority: only accepted may rebuild a
// canonical task store. Cleanup is a recorded terminal instead of a delete so
// response loss cannot make a prior seal indistinguishable from no seal.
const migrationV6ToV7 = `
CREATE TABLE completion_seals (
    run_id                TEXT PRIMARY KEY REFERENCES runs(id),
    task_id               INTEGER NOT NULL REFERENCES tasks(id),
    completion_request_id TEXT NOT NULL UNIQUE CHECK (typeof(completion_request_id)='text' AND length(completion_request_id)=16 AND completion_request_id GLOB '[0-9a-f]*' AND completion_request_id NOT GLOB '*[^0-9a-f]*'),
    object_format         TEXT NOT NULL CHECK (typeof(object_format)='text' AND object_format IN ('sha1','sha256')),
    sealed_sha            TEXT NOT NULL CHECK (typeof(sealed_sha)='text' AND sealed_sha GLOB '[0-9a-f]*' AND sealed_sha NOT GLOB '*[^0-9a-f]*' AND ((object_format='sha1' AND length(sealed_sha)=40) OR (object_format='sha256' AND length(sealed_sha)=64))),
    closure_digest        TEXT NOT NULL CHECK (typeof(closure_digest)='text' AND length(closure_digest)=64 AND closure_digest GLOB '[0-9a-f]*' AND closure_digest NOT GLOB '*[^0-9a-f]*'),
    seal_device           TEXT NOT NULL CHECK (typeof(seal_device)='text' AND seal_device GLOB '[0-9]*' AND seal_device NOT GLOB '*[^0-9]*'),
    seal_inode            TEXT NOT NULL CHECK (typeof(seal_inode)='text' AND seal_inode GLOB '[0-9]*' AND seal_inode NOT GLOB '*[^0-9]*'),
    seal_owner_uid        INTEGER NOT NULL CHECK (typeof(seal_owner_uid)='integer' AND seal_owner_uid>=0),
    state                 TEXT NOT NULL DEFAULT 'published' CHECK (state IN ('published','accepted','cleanup_pending','removed')),
    published_at          TEXT NOT NULL DEFAULT (datetime('now')),
    accepted_at           TEXT,
    removed_at            TEXT,
    CHECK ((state='published' AND accepted_at IS NULL AND removed_at IS NULL) OR (state='accepted' AND accepted_at IS NOT NULL AND removed_at IS NULL) OR (state='cleanup_pending' AND accepted_at IS NULL AND removed_at IS NULL) OR (state='removed' AND accepted_at IS NULL AND removed_at IS NOT NULL)),
    CHECK (length(seal_device)<=20 AND length(seal_inode)<=20)
);
CREATE TRIGGER completion_seals_transition_only BEFORE UPDATE ON completion_seals
WHEN NEW.run_id IS NOT OLD.run_id OR NEW.task_id IS NOT OLD.task_id OR NEW.completion_request_id IS NOT OLD.completion_request_id OR NEW.object_format IS NOT OLD.object_format OR NEW.sealed_sha IS NOT OLD.sealed_sha OR NEW.closure_digest IS NOT OLD.closure_digest OR NEW.seal_device IS NOT OLD.seal_device OR NEW.seal_inode IS NOT OLD.seal_inode OR NEW.seal_owner_uid IS NOT OLD.seal_owner_uid OR NEW.published_at IS NOT OLD.published_at OR NOT ((OLD.state='published' AND NEW.state IN ('accepted','cleanup_pending')) OR (OLD.state='cleanup_pending' AND NEW.state='removed'))
BEGIN SELECT RAISE(ABORT, 'completion seal content is immutable and state transitions are fenced (ADR-016 D6)'); END;
CREATE TRIGGER completion_seals_no_delete BEFORE DELETE ON completion_seals BEGIN SELECT RAISE(ABORT, 'completion seal history is durable (ADR-016 D6)'); END;
`

// migrationV7ToV8 adds the digest of the immutable completion manifest.  A
// v7 row legitimately has no such evidence, so additive SQLite migration leaves
// historical rows NULL; the new INSERT trigger makes the field mandatory for
// every v8 publication and consumers refuse legacy NULL rows rather than
// guessing a manifest from mutable filesystem bytes.
const migrationV7ToV8 = `
ALTER TABLE completion_seals ADD COLUMN manifest_digest TEXT;
CREATE TRIGGER completion_seals_manifest_required BEFORE INSERT ON completion_seals
WHEN typeof(NEW.manifest_digest) != 'text' OR length(NEW.manifest_digest) != 64 OR NEW.manifest_digest GLOB '*[^0-9a-f]*'
BEGIN SELECT RAISE(ABORT, 'completion seal manifest digest must be canonical TEXT sha256 (ADR-016 D6)'); END;
CREATE TRIGGER completion_seals_manifest_immutable BEFORE UPDATE ON completion_seals
WHEN NEW.manifest_digest IS NOT OLD.manifest_digest
BEGIN SELECT RAISE(ABORT, 'completion seal manifest digest is immutable (ADR-016 D6)'); END;
`

// migrationV8ToV9 records the exact accepted Worker completion receipt on its
// task. A task can cycle through seeded/worked more than once, so selecting a
// seal by timestamp would be ambiguous; the acceptance transaction advances
// this pair atomically with the stage transition instead.
const migrationV8ToV9 = `
ALTER TABLE tasks ADD COLUMN accepted_completion_run_id TEXT;
ALTER TABLE tasks ADD COLUMN accepted_completion_request_id TEXT;
CREATE TRIGGER tasks_accepted_completion_pair BEFORE UPDATE ON tasks
WHEN (NEW.accepted_completion_run_id IS NULL) != (NEW.accepted_completion_request_id IS NULL)
BEGIN SELECT RAISE(ABORT, 'task accepted completion identity must be paired (ADR-016 D6)'); END;
CREATE TRIGGER tasks_accepted_completion_fenced BEFORE UPDATE ON tasks
WHEN NEW.accepted_completion_run_id IS NOT OLD.accepted_completion_run_id
  AND (NEW.status != 'worked' OR NOT EXISTS (
    SELECT 1 FROM completion_seals s
    WHERE s.run_id=NEW.accepted_completion_run_id
      AND s.completion_request_id=NEW.accepted_completion_request_id
      AND s.task_id=NEW.id AND s.state='accepted'))
BEGIN SELECT RAISE(ABORT, 'task accepted completion identity must name its accepted worked seal (ADR-016 D6)'); END;
`

const migrationV9ToV10 = `
CREATE TABLE accepted_seal_rebuild_receipts (
    run_id TEXT PRIMARY KEY REFERENCES runs(id), task_id INTEGER NOT NULL REFERENCES tasks(id),
    completion_run_id TEXT NOT NULL REFERENCES runs(id),
    completion_request_id TEXT NOT NULL CHECK (typeof(completion_request_id)='text' AND length(completion_request_id)=16 AND completion_request_id GLOB '[0-9a-f]*' AND completion_request_id NOT GLOB '*[^0-9a-f]*'),
    object_format TEXT NOT NULL CHECK (typeof(object_format)='text' AND object_format IN ('sha1','sha256')),
    sealed_sha TEXT NOT NULL CHECK (typeof(sealed_sha)='text' AND sealed_sha GLOB '[0-9a-f]*' AND sealed_sha NOT GLOB '*[^0-9a-f]*' AND ((object_format='sha1' AND length(sealed_sha)=40) OR (object_format='sha256' AND length(sealed_sha)=64))),
    closure_digest TEXT NOT NULL CHECK (typeof(closure_digest)='text' AND length(closure_digest)=64 AND closure_digest GLOB '[0-9a-f]*' AND closure_digest NOT GLOB '*[^0-9a-f]*'),
    manifest_digest TEXT NOT NULL CHECK (typeof(manifest_digest)='text' AND length(manifest_digest)=64 AND manifest_digest GLOB '[0-9a-f]*' AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    root_device TEXT NOT NULL CHECK (typeof(root_device)='text' AND root_device GLOB '[0-9]*' AND root_device NOT GLOB '*[^0-9]*' AND length(root_device)<=20),
    root_inode TEXT NOT NULL CHECK (typeof(root_inode)='text' AND root_inode GLOB '[0-9]*' AND root_inode NOT GLOB '*[^0-9]*' AND length(root_inode)<=20),
    root_owner_uid INTEGER NOT NULL CHECK (typeof(root_owner_uid)='integer' AND root_owner_uid>=0),
    local_repo_uuid TEXT NOT NULL CHECK (typeof(local_repo_uuid)='text' AND length(local_repo_uuid)=36 AND local_repo_uuid GLOB '[0-9a-f]*-[0-9a-f]*-[0-9a-f]*-[0-9a-f]*-[0-9a-f]*' AND local_repo_uuid NOT GLOB '*[^0-9a-f-]*'),
    object_count INTEGER NOT NULL CHECK (typeof(object_count)='integer' AND object_count>=1),
    fsck_clean INTEGER NOT NULL CHECK (typeof(fsck_clean)='integer' AND fsck_clean=1),
    rebuilt_at TEXT NOT NULL DEFAULT (datetime('now')), CHECK (run_id <> completion_run_id)
);
CREATE TRIGGER accepted_seal_rebuild_receipts_fenced_insert BEFORE INSERT ON accepted_seal_rebuild_receipts
WHEN NOT EXISTS (
    SELECT 1 FROM completion_seals s JOIN tasks t ON t.id=s.task_id JOIN runs r ON r.id=NEW.run_id JOIN lock l ON l.id=1
    WHERE s.run_id=NEW.completion_run_id AND s.completion_request_id=NEW.completion_request_id AND s.task_id=NEW.task_id AND s.state='accepted'
      AND s.object_format=NEW.object_format AND s.sealed_sha=NEW.sealed_sha AND s.closure_digest=NEW.closure_digest AND s.manifest_digest=NEW.manifest_digest
      AND t.status='worked' AND t.accepted_completion_run_id=NEW.completion_run_id AND t.accepted_completion_request_id=NEW.completion_request_id
      AND r.tier='pipeline' AND r.role='verifier' AND r.subject=NEW.task_id AND r.ended_at IS NULL AND l.run_id=NEW.run_id AND l.subject=NEW.task_id
      AND EXISTS (SELECT 1 FROM task_setup_receipts tr WHERE tr.task_id=NEW.task_id AND tr.root_device=NEW.root_device AND tr.root_inode=NEW.root_inode AND tr.root_owner_uid=NEW.root_owner_uid)
)
BEGIN SELECT RAISE(ABORT, 'accepted seal rebuild receipt is not fenced to the live verifier, accepted seal, and registered task root (ADR-016 D6)'); END;
CREATE TRIGGER accepted_seal_rebuild_receipts_immutable BEFORE UPDATE ON accepted_seal_rebuild_receipts
BEGIN SELECT RAISE(ABORT, 'accepted seal rebuild receipt is immutable durable recovery evidence (ADR-016 D6)'); END;
CREATE TRIGGER accepted_seal_rebuild_receipts_no_delete BEFORE DELETE ON accepted_seal_rebuild_receipts
BEGIN SELECT RAISE(ABORT, 'accepted seal rebuild receipts are durable recovery evidence (ADR-016 D6)'); END;
`

// Migrate brings an existing spine up to CurrentSchemaVersion, reporting
// whether it changed anything. It is the "present with an older schema →
// migrate" arm of §16.4; the caller owns the "absent on a non-empty volume →
// abort loudly" arm.
//
// Every step runs in one transaction: SQLite rolls DDL back with everything
// else, so a spine is either fully converted or untouched — never a half-
// migrated shape that no version number describes. An unknown version is
// fail-closed: a spine written by a newer build is refused, never guessed at.
// Migrate is idempotent by version, so onboarding may call it unconditionally.
func Migrate(db *sql.DB) (bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("migrate spine: %w", err)
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRow(`SELECT schema_version FROM meta WHERE id = 1`).Scan(&version); err != nil {
		return false, fmt.Errorf("read spine schema version: %w", err)
	}
	if err := validateDispatchScalarAdmission(tx); err != nil {
		return false, err
	}

	steps := map[int]string{1: migrationV1ToV2, 2: migrationV2ToV3, 3: migrationV3ToV4, 4: migrationV4ToV5, 5: migrationV5ToV6, 6: migrationV6ToV7, 7: migrationV7ToV8, 8: migrationV8ToV9, 9: migrationV9ToV10}
	changed := false
	for version < CurrentSchemaVersion {
		step, ok := steps[version]
		if !ok {
			return false, fmt.Errorf(
				"spine schema_version %d has no defined migration to %d; refusing (§16.4)",
				version, CurrentSchemaVersion)
		}
		if _, err := tx.Exec(step); err != nil {
			return false, fmt.Errorf("migrate spine schema_version %d: %w", version, err)
		}
		version++
		changed = true
	}
	if version != CurrentSchemaVersion {
		return false, fmt.Errorf(
			"spine schema_version %d is newer than this build's %d; refusing (§16.4)",
			version, CurrentSchemaVersion)
	}
	if !changed {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE meta SET schema_version = ? WHERE id = 1`, CurrentSchemaVersion); err != nil {
		return false, fmt.Errorf("stamp spine schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit spine migration: %w", err)
	}
	return true, nil
}

func validateDispatchScalarAdmission(tx *sql.Tx) error {
	checks := []struct {
		name  string
		query string
	}{
		{"meta.deployment_uuid", `SELECT typeof(deployment_uuid), CAST(deployment_uuid AS TEXT) FROM meta WHERE id = 1`},
		{"lock.console_tz", `SELECT typeof(console_tz), CAST(console_tz AS TEXT) FROM lock WHERE id = 1`},
	}
	for _, check := range checks {
		var storage, value string
		if err := tx.QueryRow(check.query).Scan(&storage, &value); err != nil {
			return fmt.Errorf("validate %s admission: %w", check.name, err)
		}
		if storage != "text" || !validDispatchScalar(value) {
			return fmt.Errorf("%s is not an admitted ADR-016 D2 scalar", check.name)
		}
	}
	rows, err := tx.Query(`SELECT id, typeof(title), CAST(title AS TEXT) FROM tasks`)
	if err != nil {
		return fmt.Errorf("validate tasks.title admission: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var storage, value string
		if err := rows.Scan(&id, &storage, &value); err != nil {
			return fmt.Errorf("validate tasks.title admission: %w", err)
		}
		if storage != "text" || !validDispatchScalar(value) {
			return fmt.Errorf("tasks.title for task %d is not an admitted ADR-016 D2 scalar", id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate tasks.title admission: %w", err)
	}
	if err := ValidateDispatchMountProjection(context.Background(), tx); err != nil {
		return err
	}
	return nil
}

func validDispatchScalar(value string) bool {
	if value == "" || len(value) > 4*1024 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
