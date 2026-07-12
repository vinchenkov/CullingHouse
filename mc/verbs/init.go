package verbs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"mc/substrate"
)

// InitArgs carries mc init's flags (contract §2, Ambiguity A1: skeleton-only
// provisioning, expected to be absorbed by mc onboard in Phase 5).
type InitArgs struct {
	Spine         string
	Worksource    string
	WorkspaceRoot string
	// Lease tunables; zero = keep the schema default.
	TimeoutMinutes      int
	GraceMinutes        int
	HeartbeatIntervalS  int
	SpawnGraceS         int
	HardDeadlineMinutes int
}

// Init provisions a fresh spine: applies substrate.Schema, seeds meta, one
// sandbox_profiles + worksources row, and the lock tunable columns. It is
// not idempotent by design (substrate.Init): re-initializing a non-empty
// spine fails loudly.
func Init(a InitArgs) (any, error) {
	if a.Spine == "" || a.Worksource == "" || a.WorkspaceRoot == "" {
		return nil, Usagef("mc init requires --spine, --worksource, and --workspace-root")
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return nil, Usagef("%v", err)
	}
	defer db.Close()
	if err := substrate.Init(db); err != nil {
		return nil, Domainf("%v", err)
	}
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return nil, Usagef("generate deployment uuid: %v", err)
	}
	deployment := hex.EncodeToString(uuid[:])
	err = inTx(db, func(ctx context.Context, q Q) error {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, ?, 1)`,
			deployment); err != nil {
			return err
		}
		// One sandbox profile: the fake family is deterministic and
		// token-free, so the strictest legal egress policy applies
		// (contract §1: --network none is the fake family's egress_policy).
		if _, err := q.ExecContext(ctx, `
			INSERT INTO sandbox_profiles (id, workspace_root, egress_policy)
			VALUES ('default', ?, 'none')`, a.WorkspaceRoot); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO worksources (id, title, kind, sandbox_profile)
			VALUES (?, ?, 'repo', 'default')`, a.Worksource, a.Worksource); err != nil {
			return err
		}
		return applyTunables(ctx, q, a)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"spine":           a.Spine,
		"worksource":      a.Worksource,
		"deployment_uuid": deployment,
	}, nil
}

func applyTunables(ctx context.Context, q Q, a InitArgs) error {
	set := func(col string, v int) error {
		if v <= 0 {
			return nil
		}
		_, err := q.ExecContext(ctx,
			fmt.Sprintf(`UPDATE lock SET %s = ? WHERE id = 1`, col), v)
		return err
	}
	for col, v := range map[string]int{
		"timeout_minutes":       a.TimeoutMinutes,
		"grace_minutes":         a.GraceMinutes,
		"heartbeat_interval_s":  a.HeartbeatIntervalS,
		"spawn_grace_s":         a.SpawnGraceS,
		"hard_deadline_minutes": a.HardDeadlineMinutes,
	} {
		if err := set(col, v); err != nil {
			return err
		}
	}
	return nil
}

// OpenSpine opens an existing spine for the non-init verbs.
func OpenSpine(path string) (*sql.DB, error) {
	if path == "" {
		return nil, Usagef("no spine: set MC_SPINE (or MC_HELPER to delegate)")
	}
	db, err := substrate.Open(path)
	if err != nil {
		return nil, Usagef("%v", err)
	}
	return db, nil
}
