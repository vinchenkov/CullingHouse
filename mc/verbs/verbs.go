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
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"modernc.org/sqlite"

	"mc/domain"
)

// DomainError is a domain rejection: exit 1 (contract §2). It is
// mc/domain's coded error, re-exported so existing signatures survive the
// Phase 2 layering (phase2-contract §1.1).
type DomainError = domain.DomainError

// Domainf builds an uncoded DomainError (CLI-plane validation); domain-layer
// rejections carry their stable Code slug (contract §1.1).
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

// Q is the query surface verbs run against inside a transaction — moved to
// mc/domain and re-exported (contract §1.1).
type Q = domain.Q

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
//
// A driver-level fault is NOT a domain rejection and must not be reported as
// one. Reporting SQLite's own errors under `domain-rejection` tells an agent —
// and an operator reading a log — that the spine refused the request on the
// merits, when in fact the storage layer failed. That laundering cost a real
// diagnosis cycle on 2026-07-19: SQLite's `locking protocol (15)`
// (SQLITE_PROTOCOL, raised by a wal-index handshake failure) was read as a
// Mission Control guard and chased as a design blocker. These faults keep the
// driver's message but carry their own code so they are legible as
// infrastructure.
func classify(err error) error {
	switch err.(type) {
	case *DomainError, *UsageError:
		return err
	}
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return &DomainError{Code: domain.CodeSpineFault, Msg: err.Error()}
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
	RunID         string   `json:"run_id"`
	Tier          string   `json:"tier"`
	Role          string   `json:"role"`
	PoolIDs       []int    `json:"pool_ids"`
	VerbAllowlist []string `json:"verb_allowlist"`
}

// RequireHostScope fences host-effect and resident-only verbs before they
// open or mutate the spine (ADR-001 D6). Scope is derived only from run.json
// presence; a flag can never counterfeit it.
func RequireHostScope(id *RunIdentity, verb string) error {
	if id != nil {
		return Domainf("%s is host scope only; run.json callers are refused (ADR-001 D6)", verb)
	}
	return nil
}

// RequireOperatorVerb admits the host and a Homie only when the verb was
// frozen into that session's allowlist. Pipeline identities are categorically
// denied operator provenance (spec §18 deny rule 1).
func RequireOperatorVerb(id *RunIdentity, verb string) error {
	if id == nil {
		return nil
	}
	if id.Tier == "homie" {
		for _, allowed := range id.VerbAllowlist {
			if allowed == verb {
				return nil
			}
		}
		return Domainf("Homie session is not allowed operator verb %s (§15.3)", verb)
	}
	return Domainf("%s is an operator verb, denied to pipeline runs (§18 deny rule 1)", verb)
}

// RunJSONPath resolves the fixed in-container identity mount. Direct host and
// CLI-test invocations retain MC_RUN_JSON as a test seam; a privileged image
// invocation structurally ignores it and consumes only /mc/run.json (§11.5).
func RunJSONPath() string {
	return runJSONPathForCredentials(runtime.GOOS, os.Getuid(), os.Geteuid(), os.Getenv("MC_RUN_JSON"))
}

// runJSONPathForCredentials makes the production setuid boundary explicit
// and independently testable. A privileged image invocation must consume the
// immutable nested mount; an agent-controlled environment can never redirect
// or suppress its identity. Direct host/test invocations retain the override
// seam because their real and effective uids are equal.
func runJSONPathForCredentials(goos string, ruid, euid int, override string) string {
	if goos != "windows" && ruid != euid {
		return "/mc/run.json"
	}
	if override != "" {
		return override
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

// requirePipeline checks identity presence and tier (ADR-001 D2), for the
// verbs whose role expectation is subject-dependent or role-neutral.
func requirePipeline(id *RunIdentity) error {
	if id == nil {
		return Domainf("this verb requires a pipeline run identity (run.json); host scope is refused (§18)")
	}
	if id.Tier != "pipeline" {
		return Domainf("this verb is pipeline-tier only; run.json tier is %q", id.Tier)
	}
	return nil
}

// roleMismatch is the ADR-001 D2 refusal, coded (contract §1.1).
func roleMismatch(id *RunIdentity, want string) error {
	return &DomainError{Code: domain.CodeRoleMismatch,
		Msg: fmt.Sprintf("role mismatch: run.json role is %q, verb requires %q (ADR-001 D2)", id.Role, want)}
}

// requireRole checks the pipeline-role scope (ADR-001 D2): identity present,
// tier pipeline, and run.json role (base-role) matching want.
func requireRole(id *RunIdentity, want string) error {
	if err := requirePipeline(id); err != nil {
		return err
	}
	if got := baseRole(id.Role); got != want {
		return roleMismatch(id, want)
	}
	return nil
}

// requireExactRole preserves Strategist's explicit mode as part of the
// capability. Lock.owner is flat, so run.json is the only place that can
// prevent propose/initiative/console terminals from crossing (§3, ADR-001 D4).
func requireExactRole(id *RunIdentity, want string) error {
	if err := requirePipeline(id); err != nil {
		return err
	}
	if id.Role != want {
		return roleMismatch(id, want)
	}
	return nil
}

// requireOwnRun binds a role terminal's caller-supplied fencing token to the
// immutable identity in run.json before the token is checked against the live
// lease (§18 deny rule 2). Lease fencing alone is insufficient: an old
// same-role container must never act as a newer holder merely by supplying
// that holder's run id.
func requireOwnRun(id *RunIdentity, runID string) error {
	if err := requirePipeline(id); err != nil {
		return err
	}
	if id.RunID == "" || id.RunID != runID {
		return &DomainError{Code: domain.CodeStaleRun,
			Msg: fmt.Sprintf("stale run / caller run mismatch: run.json identifies %q, --run supplies %q (§18 deny rule 2)", id.RunID, runID)}
	}
	return nil
}

// fenceRun verifies the --run fencing token against the live lease (§10,
// §18 deny rule 2) — the domain's lease.Fence.
func fenceRun(ctx context.Context, q Q, runID string) (*int64, error) {
	return domain.Fence(ctx, q, runID)
}

// releaseLease is the ADR-001 D3 terminal boilerplate, lease half — the
// domain's fenced lease.Release.
func releaseLease(ctx context.Context, q Q, runID string) error {
	return domain.Release(ctx, q, runID)
}

// endRun is the D3 terminal boilerplate, runs half: stamp ended_at/outcome.
func endRun(ctx context.Context, q Q, runID, outcome string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE runs SET ended_at = datetime('now'), outcome = ? WHERE id = ?`,
		outcome, runID)
	return err
}
