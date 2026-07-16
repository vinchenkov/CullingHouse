package substrate_test

import (
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"mc/substrate"
)

// schemaV1 is the real Phase 1a spine, frozen from the last commit before
// ADR-016 Decision 2's receipt columns landed. A hand-rolled approximation
// would prove only that Migrate converts the approximation; the shape actually
// deployed is the only one worth converting.
//
//go:embed testdata/schema-v1.sql
var schemaV1 string

// schemaV2 is the spine as frozen at the last commit before ADR-016
// Decision 3's launch-fencing columns landed (556968c), for the same reason.
//
//go:embed testdata/schema-v2.sql
var schemaV2 string

// schemaV3 is the spine as frozen at the last commit before the v4 typeof
// fence triggers landed (b9bff07), for the same reason.
//
//go:embed testdata/schema-v3.sql
var schemaV3 string

// A migrated spine and a freshly initialized one must be indistinguishable —
// structurally and, more importantly, in what they refuse. SQLite cannot ALTER
// a UNIQUE column onto an existing table, so the obvious ALTER-only migration
// silently yields a spine whose D2 replay fences do not exist; every duplicate
// key it was supposed to reject is instead applied twice. Asserting only that
// inserts succeed would grade exactly that spine green.
func TestMigrateV1ToCurrentMatchesAFreshSpine(t *testing.T) {
	t.Run("migrated spine enforces every D2 and D3 fence a fresh spine does", func(t *testing.T) {
		db := migratedV1Spine(t)
		assertActivityReceiptBackstops(t, db)
		assertOutboxDestinationBackstops(t, db)
		assertHomieLaunchFenceBackstops(t, db)
	})

	t.Run("migrated spine matches a fresh spine's columns and indexes", func(t *testing.T) {
		migrated := migratedV1Spine(t)
		fresh := openSpine(t)
		for _, table := range []string{"activity", "outbox", "homie_sessions"} {
			if got, want := columnsOf(t, migrated, table), columnsOf(t, fresh, table); got != want {
				t.Errorf("%s columns after migration:\n  got  %s\n  want %s", table, got, want)
			}
			if got, want := indexesOf(t, migrated, table), indexesOf(t, fresh, table); got != want {
				t.Errorf("%s indexes after migration:\n  got  %s\n  want %s", table, got, want)
			}
		}
	})

	t.Run("migration stamps the version it actually wrote", func(t *testing.T) {
		db := migratedV1Spine(t)
		if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != substrate.CurrentSchemaVersion {
			t.Fatalf("schema_version = %d, want %d", got, substrate.CurrentSchemaVersion)
		}
	})

	t.Run("replaying migration on a current spine changes nothing", func(t *testing.T) {
		db := migratedV1Spine(t)
		changed, err := substrate.Migrate(db)
		if err != nil {
			t.Fatalf("Migrate replay: %v", err)
		}
		if changed {
			t.Fatal("Migrate replay reported another change; it must be idempotent by version")
		}
	})

	t.Run("a freshly initialized spine needs no migration", func(t *testing.T) {
		db := openSpine(t)
		mustExec(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'fresh', ?)`,
			substrate.CurrentSchemaVersion)
		changed, err := substrate.Migrate(db)
		if err != nil {
			t.Fatalf("Migrate fresh: %v", err)
		}
		if changed {
			t.Fatal("Migrate reported a change on a current spine")
		}
	})

	// §16.4: a spine is either fully converted or untouched. A half-migrated
	// shape that no version number describes is the one outcome that cannot be
	// recovered from, so the DDL and the version must move together.
	t.Run("failed DDL rolls back every prior statement and the version", func(t *testing.T) {
		db := legacyV1Spine(t)
		mustExec(t, db, `DROP TABLE outbox`) // migration reaches a missing outbox after altering activity
		if changed, err := substrate.Migrate(db); err == nil || changed {
			t.Fatalf("Migrate incomplete v1 = changed %v, err %v; want atomic refusal", changed, err)
		}
		if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != 1 {
			t.Fatalf("failed migration changed schema_version to %d", got)
		}
		if columnExists(t, db, "activity", "dispatch_key") {
			t.Fatal("failed migration retained activity.dispatch_key")
		}
	})

	// Fail closed on a spine written by a newer build: guessing at an unknown
	// shape is how a brain gets silently corrupted.
	t.Run("unknown version refuses without mutation", func(t *testing.T) {
		db := legacyV1Spine(t)
		mustExec(t, db, `UPDATE meta SET schema_version=99 WHERE id=1`)
		if changed, err := substrate.Migrate(db); err == nil || changed {
			t.Fatalf("Migrate unknown = changed %v, err %v; want refusal", changed, err)
		}
		if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != 99 {
			t.Fatalf("unknown-version refusal changed schema_version to %d", got)
		}
		if columnExists(t, db, "activity", "dispatch_key") {
			t.Fatal("unknown-version refusal changed the schema")
		}
	})

	t.Run("a version below every defined step refuses without mutation", func(t *testing.T) {
		db := legacyV1Spine(t)
		mustExec(t, db, `UPDATE meta SET schema_version=0 WHERE id=1`)
		if changed, err := substrate.Migrate(db); err == nil || changed {
			t.Fatalf("Migrate v0 = changed %v, err %v; want refusal", changed, err)
		}
		if columnExists(t, db, "activity", "dispatch_key") {
			t.Fatal("v0 refusal changed the schema")
		}
	})
}

func TestMigrateRejectsPrivateCarrierScalarDebt(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'fresh', ?)`,
		substrate.CurrentSchemaVersion)
	mustExec(t, db, `INSERT INTO tasks (title, worksource) VALUES (?, 'ws')`, strings.Repeat("x", 4097))

	changed, err := substrate.Migrate(db)
	if err == nil || changed || !strings.Contains(err.Error(), "tasks.title") {
		t.Fatalf("Migrate overlong dispatch scalar = changed %v, err %v; want admission refusal", changed, err)
	}
	if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != substrate.CurrentSchemaVersion {
		t.Fatalf("failed admission changed schema version to %d", got)
	}
}

// The v2 -> v3 step adds ADR-016 D3's launch fencing to homie_sessions. The
// hazard is the same as v1 -> v2: an ALTER-only step that silently fails to
// carry a pairing CHECK yields a spine that accepts a half-bound launch or a
// debt-plus-launch row a fresh spine refuses, and every generation fence
// downstream of it fails open.
func TestMigrateV2ToCurrentMatchesAFreshSpine(t *testing.T) {
	t.Run("migrated spine enforces every D3 launch fence a fresh spine does", func(t *testing.T) {
		assertHomieLaunchFenceBackstops(t, migratedV2Spine(t))
	})

	t.Run("migrated spine matches a fresh spine's columns and indexes", func(t *testing.T) {
		migrated := migratedV2Spine(t)
		fresh := openSpine(t)
		for _, table := range []string{"homie_sessions", "activity", "outbox"} {
			if got, want := columnsOf(t, migrated, table), columnsOf(t, fresh, table); got != want {
				t.Errorf("%s columns after migration:\n  got  %s\n  want %s", table, got, want)
			}
			if got, want := indexesOf(t, migrated, table), indexesOf(t, fresh, table); got != want {
				t.Errorf("%s indexes after migration:\n  got  %s\n  want %s", table, got, want)
			}
		}
	})

	// D3: "`homie start` initializes every launch/debt field empty/zero."
	// For a session that predates the columns, the migration's defaults are
	// that initialization.
	t.Run("a session predating launch fencing carries no launch and no debt", func(t *testing.T) {
		db := migratedV2Spine(t)
		if got := oneInt(t, db, `
			SELECT count(*) FROM homie_sessions WHERE id = 'v2-history'
			  AND current_launch_id IS NULL AND current_launch_mode IS NULL
			  AND current_prime_through_seq IS NULL AND current_prime_row_count IS NULL
			  AND current_container_id IS NULL
			  AND launch_bound_at IS NULL AND launch_started_at IS NULL
			  AND resume_owed = 0 AND resume_mode IS NULL
			  AND resume_prime_through_seq IS NULL AND resume_prime_row_count IS NULL`,
		); got != 1 {
			t.Fatal("migration must leave a pre-existing session with every launch/debt field empty/zero")
		}
	})

	// §16.4 atomicity, aimed at the middle of the step this time: the planted
	// column makes a LATER ALTER fail after several have already applied, so
	// only full rollback can leave no half-fenced shape behind.
	t.Run("failed DDL rolls back every prior statement and the version", func(t *testing.T) {
		db := legacyV2Spine(t)
		mustExec(t, db, `ALTER TABLE homie_sessions ADD COLUMN resume_owed TEXT`)
		if changed, err := substrate.Migrate(db); err == nil || changed {
			t.Fatalf("Migrate planted v2 = changed %v, err %v; want atomic refusal", changed, err)
		}
		if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != 2 {
			t.Fatalf("failed migration changed schema_version to %d", got)
		}
		if columnExists(t, db, "homie_sessions", "current_launch_id") {
			t.Fatal("failed migration retained homie_sessions.current_launch_id")
		}
	})
}

// The v3 -> v4 step adds ADR-016 D2's typeof fence triggers over the
// activity/outbox replay-key columns. The hazard: those columns' hex CHECKs
// hold only for TEXT — a BLOB bypasses affinity conversion, length() counts
// its bytes, and GLOB reads it only to the first NUL, so a BLOB forgery
// stores as a distinct UNIQUE value whose own receipt lookup cannot find it,
// and the replay fence fails open. The columns shipped in the frozen v1 -> v2
// migration, so the fence must be a trigger.
func TestMigrateV3ToCurrentMatchesAFreshSpine(t *testing.T) {
	t.Run("migrated spine enforces every fence a fresh spine does", func(t *testing.T) {
		db := migratedV3Spine(t)
		assertActivityReceiptBackstops(t, db)
		assertOutboxDestinationBackstops(t, db)
		assertHomieLaunchFenceBackstops(t, db)
	})

	t.Run("migrated spine matches a fresh spine's triggers", func(t *testing.T) {
		migrated := migratedV3Spine(t)
		fresh := openSpine(t)
		for _, table := range []string{"activity", "outbox", "homie_sessions"} {
			if got, want := triggersOf(t, migrated, table), triggersOf(t, fresh, table); got != want {
				t.Errorf("%s triggers after migration:\n  got  %s\n  want %s", table, got, want)
			}
		}
	})

	t.Run("migration stamps the version it actually wrote", func(t *testing.T) {
		db := migratedV3Spine(t)
		if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != substrate.CurrentSchemaVersion {
			t.Fatalf("schema_version = %d, want %d", got, substrate.CurrentSchemaVersion)
		}
	})

	t.Run("v3 history survives the conversion untouched", func(t *testing.T) {
		db := migratedV3Spine(t)
		if got := oneInt(t, db, `
			SELECT count(*) FROM homie_sessions WHERE id = 'v3-history'
			  AND current_launch_id = 'aaaaaaaaaaaaaaaa' AND current_launch_mode = 'fresh'`,
		); got != 1 {
			t.Fatal("migration must leave a v3 session's bound launch untouched")
		}
		if got := oneInt(t, db, `SELECT count(*) FROM activity WHERE dispatch_key IS NOT NULL`); got != 1 {
			t.Fatal("migration must preserve keyed dispatch history")
		}
	})

	// §16.4 atomicity: a pre-existing trigger name makes the SECOND CREATE
	// TRIGGER fail after the first applied; only full rollback leaves no
	// half-fenced shape behind.
	t.Run("failed DDL rolls back every prior statement and the version", func(t *testing.T) {
		db := legacyV3Spine(t)
		mustExec(t, db, `
			CREATE TRIGGER outbox_event_destination_key_is_text
			BEFORE INSERT ON outbox BEGIN SELECT 1; END`)
		if changed, err := substrate.Migrate(db); err == nil || changed {
			t.Fatalf("Migrate planted v3 = changed %v, err %v; want atomic refusal", changed, err)
		}
		if got := oneInt(t, db, `SELECT schema_version FROM meta WHERE id=1`); got != 3 {
			t.Fatalf("failed migration changed schema_version to %d", got)
		}
		if triggerExists(t, db, "activity_receipt_keys_are_text") {
			t.Fatal("failed migration retained the activity typeof fence")
		}
	})
}

// legacyV3Spine is a real v3 spine carrying data, as a live deployment would:
// a session with a bound launch generation and keyed dispatch history that
// must survive the conversion.
func legacyV3Spine(t *testing.T) *sql.DB {
	t.Helper()
	db, err := substrate.Open(filepath.Join(t.TempDir(), "legacy-v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, schemaV3)
	mustExec(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'legacy-deployment', 3)`)
	mkHomieSession(t, db, "v3-history")
	mustExec(t, db, `
		UPDATE homie_sessions SET current_launch_id = 'aaaaaaaaaaaaaaaa', current_launch_mode = 'fresh'
		WHERE id = 'v3-history'`)
	mustExec(t, db, `INSERT INTO activity (actor, kind, detail, dispatch_key)
		VALUES ('mc', 'dispatch.health', '{}', ?)`, strings.Repeat("f", 64))
	return db
}

func migratedV3Spine(t *testing.T) *sql.DB {
	t.Helper()
	db := legacyV3Spine(t)
	before := oneInt(t, db, `SELECT count(*) FROM activity`)
	changed, err := substrate.Migrate(db)
	if err != nil {
		t.Fatalf("Migrate v3: %v", err)
	}
	if !changed {
		t.Fatal("Migrate v3 reported no change")
	}
	// §16.4: no path may drop or re-initialize a spine containing data.
	if after := oneInt(t, db, `SELECT count(*) FROM activity`); after != before {
		t.Fatalf("migration lost history: %d activity rows before, %d after", before, after)
	}
	return db
}

// legacyV2Spine is a real v2 spine carrying data, as a live deployment would:
// a registered Homie session and receipt-fenced dispatch history that must
// survive the conversion.
func legacyV2Spine(t *testing.T) *sql.DB {
	t.Helper()
	db, err := substrate.Open(filepath.Join(t.TempDir(), "legacy-v2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, schemaV2)
	mustExec(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'legacy-deployment', 2)`)
	mkHomieSession(t, db, "v2-history")
	mustExec(t, db, `INSERT INTO activity (actor, kind, detail, dispatch_key)
		VALUES ('mc', 'dispatch.health', '{}', ?)`, strings.Repeat("f", 64))
	return db
}

func migratedV2Spine(t *testing.T) *sql.DB {
	t.Helper()
	db := legacyV2Spine(t)
	sessions := oneInt(t, db, `SELECT count(*) FROM homie_sessions`)
	changed, err := substrate.Migrate(db)
	if err != nil {
		t.Fatalf("Migrate v2: %v", err)
	}
	if !changed {
		t.Fatal("Migrate v2 reported no change")
	}
	// §16.4: no path may drop or re-initialize a spine containing data.
	if after := oneInt(t, db, `SELECT count(*) FROM homie_sessions`); after != sessions {
		t.Fatalf("migration lost history: %d sessions before, %d after", sessions, after)
	}
	return db
}

// legacyV1Spine is a real v1 spine carrying data, as a live deployment would.
func legacyV1Spine(t *testing.T) *sql.DB {
	t.Helper()
	db, err := substrate.Open(filepath.Join(t.TempDir(), "legacy-v1.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, schemaV1)
	mustExec(t, db, `INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, 'legacy-deployment', 1)`)
	// History predating the receipt columns must survive the conversion.
	mustExec(t, db, `INSERT INTO activity (actor, kind, detail) VALUES ('operator', 'task.added', '{}')`)
	return db
}

func migratedV1Spine(t *testing.T) *sql.DB {
	t.Helper()
	db := legacyV1Spine(t)
	before := oneInt(t, db, `SELECT count(*) FROM activity`)
	changed, err := substrate.Migrate(db)
	if err != nil {
		t.Fatalf("Migrate v1: %v", err)
	}
	if !changed {
		t.Fatal("Migrate v1 reported no change")
	}
	// §16.4: no path may drop or re-initialize a spine containing data.
	if after := oneInt(t, db, `SELECT count(*) FROM activity`); after != before {
		t.Fatalf("migration lost history: %d activity rows before, %d after", before, after)
	}
	return db
}

// triggersOf renders a table's trigger names as a comparable string.
func triggersOf(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'trigger' AND tbl_name = ? ORDER BY name`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, ", ")
}

func triggerExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	return oneInt(t, db, `
		SELECT count(*) FROM sqlite_master
		WHERE type = 'trigger' AND name = ?`, name) == 1
}

// columnsOf renders a table's column list as a comparable string.
func columnsOf(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT name, type FROM pragma_table_info(?) ORDER BY name`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			t.Fatal(err)
		}
		out = append(out, name+" "+kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, ", ")
}

// indexesOf renders a table's indexes, including uniqueness and key columns —
// the property an ALTER-only migration cannot reproduce.
func indexesOf(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT name, "unique" FROM pragma_index_list(?) ORDER BY name`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type idx struct {
		name   string
		unique int
	}
	var found []idx
	for rows.Next() {
		var e idx
		if err := rows.Scan(&e.name, &e.unique); err != nil {
			t.Fatal(err)
		}
		found = append(found, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range found {
		cols, err := db.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, e.name)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for cols.Next() {
			var name sql.NullString
			if err := cols.Scan(&name); err != nil {
				cols.Close()
				t.Fatal(err)
			}
			keys = append(keys, name.String)
		}
		cols.Close()
		out = append(out, fmt.Sprintf("%s(unique=%d)[%s]", e.name, e.unique, strings.Join(keys, "+")))
	}
	return strings.Join(out, ", ")
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	return oneInt(t, db, `SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column) > 0
}
