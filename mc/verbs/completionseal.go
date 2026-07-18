package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// AcceptCompletionSeal is the narrow host transaction shared by the eventual
// privileged publisher and Worker terminal. It refuses an orphaned/mismatched
// published receipt and turns it into authority only while its exact Worker
// still owns the singleton lease. The seal acceptance, ordinary Worker stage
// transition, terminal Run receipt, and lease release are one transaction: a
// later setup operation may consume only that resulting accepted record.
func AcceptCompletionSeal(db *sql.DB, runID, requestID string) error {
	return inTx(db, func(ctx context.Context, q Q) error {
		var tier, role, outcome string
		var subject sql.NullInt64
		var endedAt sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT tier, role, subject, ended_at, COALESCE(outcome, '')
			FROM runs WHERE id=?`, runID).
			Scan(&tier, &role, &subject, &endedAt, &outcome)
		if err != nil {
			return Domainf("completion seal producer run is absent")
		}
		var taskID int64
		var state string
		err = q.QueryRowContext(ctx, `SELECT task_id,state FROM completion_seals WHERE run_id=? AND completion_request_id=?`, runID, requestID).Scan(&taskID, &state)
		if err != nil {
			return Domainf("completion seal receipt is absent")
		}
		if tier != "pipeline" || role != "worker" || !subject.Valid || taskID != subject.Int64 {
			return Domainf("completion seal receipt is not a task Worker's published seal")
		}
		if state == "accepted" {
			if !endedAt.Valid || outcome != "completed" {
				return Domainf("accepted completion seal lacks its durable Worker terminal")
			}
			return nil // exact lost-response replay after the terminal released its lease.
		}
		if state != "published" || endedAt.Valid {
			return Domainf("completion seal receipt is not the live Worker published seal")
		}
		fencedSubject, err := fenceRun(ctx, q, runID)
		if err != nil {
			return err
		}
		if fencedSubject == nil || *fencedSubject != taskID {
			return Domainf("completion seal receipt lost its Worker task lease fence")
		}
		if err := domain.AdvanceStage(ctx, q, taskID, "worked"); err != nil {
			return err
		}
		result, err := q.ExecContext(ctx, `UPDATE completion_seals SET state='accepted', accepted_at=datetime('now') WHERE run_id=? AND completion_request_id=? AND state='published'`, runID, requestID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return Domainf("completion seal acceptance lost its published receipt fence")
		}
		if err := endRun(ctx, q, runID, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, runID)
	})
}
