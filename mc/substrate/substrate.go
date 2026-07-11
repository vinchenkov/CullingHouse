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
