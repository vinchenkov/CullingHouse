package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"mc/dispatch"
)

const spawnBriefSchema = "mc.spawn-brief.v1"

type briefTask struct {
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Scope           string `json:"scope"`
	InitiativeID    *int64 `json:"initiative_id"`
	Priority        int    `json:"priority"`
	Status          string `json:"status"`
	CorrectionCount int    `json:"correction_count"`
	Blocked         bool   `json:"blocked"`
	BlockedReason   string `json:"blocked_reason"`
	Worksource      string `json:"worksource"`
	Branch          string `json:"branch"`
	VerifiedSHA     string `json:"verified_sha"`
	TargetRef       string `json:"target_ref"`
	RefineNotes     string `json:"refine_notes"`
}

type briefVerdict struct {
	Outcome          string `json:"outcome"`
	EvidencePath     string `json:"evidence_path"`
	CorrectionPath   string `json:"correction_path"`
	Deepening        string `json:"deepening"`
	ExceptionLabeled bool   `json:"exception_labeled"`
}

type spawnBriefDocument struct {
	Schema           string        `json:"schema"`
	Role             string        `json:"role"`
	Subject          *briefTask    `json:"subject,omitempty"`
	ProposedPool     []briefTask   `json:"proposed_pool,omitempty"`
	DedupeTitles     []string      `json:"dedupe_titles,omitempty"`
	LatestCorrection *briefVerdict `json:"latest_correction,omitempty"`
	LatestVerdict    *briefVerdict `json:"latest_verdict,omitempty"`
	LatestOutputPath string        `json:"latest_output_path,omitempty"`
	ReviewQueue      []briefTask   `json:"review_queue,omitempty"`
	BlockedTasks     []briefTask   `json:"blocked_tasks,omitempty"`
}

// buildSpawnBrief materializes the role's immutable opening input from the
// same BEGIN IMMEDIATE snapshot that is about to claim the lease. The
// resident copies this string into run.json unchanged; it never re-reads the
// spine, so correction/refinement/evidence carriers cannot drift across the
// decision-to-effect gap (spec §9.2, §10, Inv. 10/12).
func buildSpawnBrief(ctx context.Context, q Q, sp *dispatch.Spawn) (string, error) {
	doc := spawnBriefDocument{Schema: spawnBriefSchema, Role: string(sp.Role)}

	if sp.SubjectID != nil {
		subject, err := loadBriefTask(ctx, q, *sp.SubjectID)
		if err != nil {
			return "", err
		}
		doc.Subject = &subject
		var output sql.NullString
		err = q.QueryRowContext(ctx, `
			SELECT output_path FROM runs
			WHERE subject = ? AND output_path IS NOT NULL
			ORDER BY created_at DESC, id DESC LIMIT 1`, *sp.SubjectID).Scan(&output)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}
		if output.Valid {
			doc.LatestOutputPath = output.String
		}
	}

	if sp.Role == dispatch.RoleEditor {
		for _, id := range sp.ProposedPool {
			task, err := loadBriefTask(ctx, q, id)
			if err != nil {
				return "", err
			}
			doc.ProposedPool = append(doc.ProposedPool, task)
		}
	}
	if sp.Role == dispatch.RoleStrategistPropose {
		doc.DedupeTitles = append([]string(nil), sp.DedupeTitles...)
	}
	if sp.SubjectID != nil && sp.Role == dispatch.RoleWorker {
		verdict, err := loadLatestVerdict(ctx, q, *sp.SubjectID, "correct")
		if err != nil {
			return "", err
		}
		doc.LatestCorrection = verdict
	}
	if sp.SubjectID != nil && sp.Role == dispatch.RolePackager {
		verdict, err := loadLatestVerdict(ctx, q, *sp.SubjectID, "")
		if err != nil {
			return "", err
		}
		doc.LatestVerdict = verdict
	}
	if sp.Role == dispatch.RoleStrategistConsole {
		var err error
		doc.ReviewQueue, err = loadBriefTaskSet(ctx, q, `
			SELECT t.id FROM tasks t
			JOIN review_packets p ON p.task_id = t.id
			WHERE t.archived = 0 AND t.decision IS NULL AND p.archived = 0
			ORDER BY p.created_at, t.id`)
		if err != nil {
			return "", err
		}
		doc.BlockedTasks, err = loadBriefTaskSet(ctx, q, `
			SELECT id FROM tasks
			WHERE archived = 0 AND blocked = 1
			ORDER BY priority, created_at, id`)
		if err != nil {
			return "", err
		}
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render spawn brief: %w", err)
	}
	return "Mission Control immutable run brief\n" + string(b), nil
}

func loadBriefTask(ctx context.Context, q Q, id int64) (briefTask, error) {
	var task briefTask
	var initiativeID sql.NullInt64
	var blocked int
	err := q.QueryRowContext(ctx, `
		SELECT id, title, COALESCE(description, ''), scope, initiative_id,
		       priority, status, correction_count, blocked,
		       COALESCE(blocked_reason, ''), worksource, COALESCE(branch, ''),
		       COALESCE(verified_sha, ''), COALESCE(target_ref, ''),
		       COALESCE(refine_notes, '')
		FROM tasks WHERE id = ?`, id).Scan(
		&task.ID, &task.Title, &task.Description, &task.Scope, &initiativeID,
		&task.Priority, &task.Status, &task.CorrectionCount, &blocked,
		&task.BlockedReason, &task.Worksource, &task.Branch, &task.VerifiedSHA,
		&task.TargetRef, &task.RefineNotes)
	if err != nil {
		return task, err
	}
	if initiativeID.Valid {
		v := initiativeID.Int64
		task.InitiativeID = &v
	}
	task.Blocked = blocked == 1
	return task, nil
}

func loadBriefTaskSet(ctx context.Context, q Q, query string) ([]briefTask, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tasks := make([]briefTask, 0, len(ids))
	for _, id := range ids {
		task, err := loadBriefTask(ctx, q, id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func loadLatestVerdict(ctx context.Context, q Q, taskID int64, outcome string) (*briefVerdict, error) {
	query := `
		SELECT verdict_outcome, COALESCE(evidence_path, ''),
		       COALESCE(correction_path, ''), COALESCE(deepening, '')
		FROM runs
		WHERE subject = ? AND verdict_outcome IS NOT NULL`
	args := []any{taskID}
	if outcome != "" {
		query += ` AND verdict_outcome = ?`
		args = append(args, outcome)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT 1`
	var v briefVerdict
	err := q.QueryRowContext(ctx, query, args...).Scan(
		&v.Outcome, &v.EvidencePath, &v.CorrectionPath, &v.Deepening)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.ExceptionLabeled = v.Outcome == "budget-spent"
	return &v, nil
}
