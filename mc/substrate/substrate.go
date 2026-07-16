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
const CurrentSchemaVersion = 4

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

	steps := map[int]string{1: migrationV1ToV2, 2: migrationV2ToV3, 3: migrationV3ToV4}
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
