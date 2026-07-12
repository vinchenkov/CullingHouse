// Package verbs implements the mc verb surface pinned by the Phase 1b
// contract (docs/phase1b-contract.md §2) as testable functions over an open
// spine connection. cmd/mc is the thin CLI layer over this package.
//
// Common contract (contract §2): each verb returns a single JSON-marshalable
// value (the stdout object); rejections are typed — DomainError is exit 1
// (fencing mismatch, validation, illegal transition; substrate aborts
// surface here), UsageError is exit 2 (usage/environment). Every mutation
// runs under BEGIN IMMEDIATE on the S5 connection discipline
// (substrate.Open); timestamps are written with datetime('now') inside the
// transaction — host processes never pass timestamps in (§10 clock
// discipline).
package verbs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// DomainError is a domain rejection: exit 1 (contract §2).
type DomainError struct{ Msg string }

func (e *DomainError) Error() string { return e.Msg }

// Domainf builds a DomainError.
func Domainf(format string, a ...any) error {
	return &DomainError{Msg: fmt.Sprintf(format, a...)}
}

// UsageError is a usage/environment rejection: exit 2 (contract §2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usagef builds a UsageError.
func Usagef(format string, a ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, a...)}
}

// Q is the query surface verbs run against inside a transaction.
type Q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// inTx runs fn under BEGIN IMMEDIATE on a single connection (§10 storage
// discipline). Any error from fn rolls the transaction back and is returned;
// raw SQL errors (the substrate's trigger/CHECK aborts among them) are
// wrapped as DomainError unless already typed.
func inTx(db *sql.DB, fn func(ctx context.Context, q Q) error) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return Usagef("acquire spine connection: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return Usagef("begin immediate: %v", err)
	}
	if err := fn(ctx, conn); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return classify(err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return classify(err)
	}
	return nil
}

// classify maps untyped errors (substrate aborts, constraint failures) to
// DomainError; typed errors pass through.
func classify(err error) error {
	switch err.(type) {
	case *DomainError, *UsageError:
		return err
	}
	return &DomainError{Msg: err.Error()}
}

// newRunID allocates a run id: 16 hex chars, strict container-name-safe
// charset (§11.6; container name mc-run-<run_id>).
func newRunID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", Usagef("allocate run id: %v", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// spineTime is the datetime('now') storage format.
const spineTime = "2006-01-02 15:04:05"

func parseSpineTime(s string) (time.Time, error) {
	t, err := time.Parse(spineTime, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse spine timestamp %q: %w", s, err)
	}
	return t, nil
}

// baseRole strips the Strategist mode suffix: "strategist(propose)" →
// "strategist". Lock.owner and runs.role are flat and untyped (§10); mode
// lives in the run brief.
func baseRole(role string) string {
	if i := strings.IndexByte(role, '('); i >= 0 {
		return role[:i]
	}
	return role
}

// RunIdentity is the read-only launch envelope identity (§11.5, ADR-001 D2):
// tier and role are read only from run.json, never from arguments.
type RunIdentity struct {
	RunID   string `json:"run_id"`
	Tier    string `json:"tier"`
	Role    string `json:"role"`
	PoolIDs []int  `json:"pool_ids"`
}

// RunJSONPath resolves the run.json location: the fixed in-container mount
// /mc/run.json (§11.5), overridable via MC_RUN_JSON for the CLI test tier
// (within-container scope separation is best-effort by decision, §11.5;
// see deviation note D-mc-3).
func RunJSONPath() string {
	if p := os.Getenv("MC_RUN_JSON"); p != "" {
		return p
	}
	return "/mc/run.json"
}

// LoadIdentity reads run.json if present. (nil, nil) = host scope.
func LoadIdentity() (*RunIdentity, error) {
	b, err := os.ReadFile(RunJSONPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // host scope
		}
		return nil, Usagef("read run.json: %v", err)
	}
	var id RunIdentity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, Usagef("parse run.json: %v", err)
	}
	return &id, nil
}

// requireRole checks the pipeline-role scope (ADR-001 D2): identity present,
// tier pipeline, and run.json role (base-role) matching want.
func requireRole(id *RunIdentity, want string) error {
	if id == nil {
		return Domainf("this verb requires a pipeline run identity (run.json); host scope is refused (§18)")
	}
	if id.Tier != "pipeline" {
		return Domainf("this verb is pipeline-tier only; run.json tier is %q", id.Tier)
	}
	if got := baseRole(id.Role); got != want {
		return Domainf("role mismatch: run.json role is %q, verb requires %q (ADR-001 D2)", id.Role, want)
	}
	return nil
}

// fenceRun verifies the --run fencing token against the live lease (§10,
// §18 deny rule 2): a call whose run_id no longer matches is rejected, never
// double-applied. Returns the lease's subject (nil for subjectless runs).
func fenceRun(ctx context.Context, q Q, runID string) (*int64, error) {
	var lockRun sql.NullString
	var subject sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id = 1`).
		Scan(&lockRun, &subject)
	if err != nil {
		return nil, err
	}
	if !lockRun.Valid || lockRun.String != runID {
		return nil, Domainf("stale run: %q does not hold the live lease (§10 fencing)", runID)
	}
	if subject.Valid {
		s := subject.Int64
		return &s, nil
	}
	return nil, nil
}

// releaseLease is the ADR-001 D3 terminal boilerplate, lease half: NULL every
// claim column (a free lock carries no run residue — substrate CHECKs).
func releaseLease(ctx context.Context, q Q, runID string) error {
	res, err := q.ExecContext(ctx, `
		UPDATE lock SET run_id = NULL, worksource = NULL, subject = NULL,
			owner = NULL, acquired_at = NULL, last_heartbeat_at = NULL,
			hard_deadline_at = NULL
		WHERE id = 1 AND run_id = ?`, runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return Domainf("stale run: %q does not hold the live lease (§10 fencing)", runID)
	}
	return nil
}

// endRun is the D3 terminal boilerplate, runs half: stamp ended_at/outcome.
func endRun(ctx context.Context, q Q, runID, outcome string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE runs SET ended_at = datetime('now'), outcome = ? WHERE id = ?`,
		outcome, runID)
	return err
}
