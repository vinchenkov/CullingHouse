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

// A migrated spine and a freshly initialized one must be indistinguishable —
// structurally and, more importantly, in what they refuse. SQLite cannot ALTER
// a UNIQUE column onto an existing table, so the obvious ALTER-only migration
// silently yields a spine whose D2 replay fences do not exist; every duplicate
// key it was supposed to reject is instead applied twice. Asserting only that
// inserts succeed would grade exactly that spine green.
func TestMigrateV1ToCurrentMatchesAFreshSpine(t *testing.T) {
	t.Run("migrated spine enforces every D2 fence a fresh spine does", func(t *testing.T) {
		db := migratedV1Spine(t)
		assertActivityReceiptBackstops(t, db)
		assertOutboxDestinationBackstops(t, db)
	})

	t.Run("migrated spine matches a fresh spine's columns and indexes", func(t *testing.T) {
		migrated := migratedV1Spine(t)
		fresh := openSpine(t)
		for _, table := range []string{"activity", "outbox"} {
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
