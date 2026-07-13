package verbs

import (
	"context"
	"database/sql"
)

type WorksourceAddArgs struct {
	ID             string
	Title          string
	Kind           string
	Jurisdiction   string
	SandboxProfile string
	Directive      string
	SeedingMode    string
}

func WorksourceAdd(db *sql.DB, id *RunIdentity, a WorksourceAddArgs) (any, error) {
	if err := RequireOperatorVerb(id, "worksource.add"); err != nil {
		return nil, err
	}
	if a.ID == "" || a.Title == "" {
		return nil, Usagef("mc worksource add requires id and --title")
	}
	if a.Kind != "repo" && a.Kind != "personal" && a.Kind != "transient" {
		return nil, Usagef("mc worksource add --kind must be repo|personal|transient")
	}
	if a.SeedingMode == "" {
		a.SeedingMode = "propose-only"
	}
	if a.SeedingMode != "propose-only" && a.SeedingMode != "auto" {
		return nil, Usagef("mc worksource add --seeding-mode must be propose-only|auto")
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireOperatorVerbTx(ctx, q, id, "worksource.add"); err != nil {
			return err
		}
		if a.SandboxProfile != "" {
			var exists int
			if err := q.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM sandbox_profiles WHERE id = ?`, a.SandboxProfile,
			).Scan(&exists); err != nil {
				return err
			}
			if exists != 1 {
				return Domainf("unknown sandbox profile %q", a.SandboxProfile)
			}
		}
		_, err := q.ExecContext(ctx, `
			INSERT INTO worksources
				(id, title, kind, jurisdiction, sandbox_profile, directive, seeding_mode)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.Title, a.Kind, nullIfEmpty(a.Jurisdiction),
			nullIfEmpty(a.SandboxProfile), nullIfEmpty(a.Directive), a.SeedingMode)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"worksource_id": a.ID, "status": "active"}, nil
}

func WorksourceSetStatus(db *sql.DB, id *RunIdentity, worksource, target string) (any, error) {
	// The frozen §15.3 allowlist names the verbs (worksource.pause/.archive),
	// not the status values they write.
	verbs := map[string]string{"paused": "worksource.pause", "archived": "worksource.archive"}
	verb, ok := verbs[target]
	if !ok {
		return nil, Usagef("worksource status target must be paused|archived")
	}
	if err := RequireOperatorVerb(id, verb); err != nil {
		return nil, err
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireOperatorVerbTx(ctx, q, id, verb); err != nil {
			return err
		}
		var current string
		err := q.QueryRowContext(ctx,
			`SELECT status FROM worksources WHERE id = ?`, worksource).Scan(&current)
		if err == sql.ErrNoRows {
			return Domainf("unknown worksource %q", worksource)
		}
		if err != nil {
			return err
		}
		if current == target {
			return nil // operator/surface retry is idempotent
		}
		if current == "archived" {
			return Domainf("archived worksource %q is terminal", worksource)
		}
		_, err = q.ExecContext(ctx,
			`UPDATE worksources SET status = ? WHERE id = ?`, target, worksource)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"worksource_id": worksource, "status": target}, nil
}

func WorksourceList(db *sql.DB) (any, error) {
	rows, err := db.Query(`
		SELECT id, title, kind, jurisdiction, sandbox_profile, directive,
		       seeding_mode, status
		FROM worksources ORDER BY id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out, err := rowsToMaps(rows)
	if err != nil {
		return nil, classify(err)
	}
	return map[string]any{"worksources": out}, nil
}
