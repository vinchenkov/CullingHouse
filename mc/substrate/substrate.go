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
const CurrentSchemaVersion = 2

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

	steps := map[int]string{1: migrationV1ToV2}
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
