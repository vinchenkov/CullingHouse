package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// CompletionSealPublication is the path-free receipt the privileged completion
// wrapper submits only after it has atomically published and re-attested the
// immutable seal directory. Host paths never enter the spine.
type CompletionSealPublication struct {
	RunID             string
	TaskID            int64
	CompletionRequest string
	ObjectFormat      string
	SealedSHA         string
	ClosureDigest     string
	ManifestDigest    string
	Device            string
	Inode             string
	OwnerUID          int64
}

func validateCompletionSealPublication(p CompletionSealPublication) error {
	if p.RunID == "" || p.TaskID < 1 || len(p.CompletionRequest) != 16 || !assignmentHex.MatchString(p.CompletionRequest) {
		return Domainf("completion seal publication identity is malformed")
	}
	if err := validateSetupObjectFormat(p.ObjectFormat); err != nil {
		return err
	}
	if len(p.SealedSHA) != oidLen(p.ObjectFormat) || !assignmentHex.MatchString(p.SealedSHA) ||
		len(p.ClosureDigest) != 64 || !assignmentHex.MatchString(p.ClosureDigest) ||
		len(p.ManifestDigest) != 64 || !assignmentHex.MatchString(p.ManifestDigest) ||
		!decimalIdentity.MatchString(p.Device) || !decimalIdentity.MatchString(p.Inode) || p.OwnerUID < 0 {
		return Domainf("completion seal publication does not carry canonical immutable evidence")
	}
	return nil
}

// PublishCompletionSeal creates the durable published receipt under the exact
// live Worker lease. An exact replay is inert; a same request with changed
// immutable facts refuses rather than letting a response retry rewrite seal
// authority. The privileged wrapper owns the preceding filesystem publication.
func PublishCompletionSeal(db *sql.DB, p CompletionSealPublication) error {
	if err := validateCompletionSealPublication(p); err != nil {
		return err
	}
	return inTx(db, func(ctx context.Context, q Q) error {
		var existing CompletionSealPublication
		err := q.QueryRowContext(ctx, `
			SELECT run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,
			       COALESCE(manifest_digest,''),seal_device,seal_inode,seal_owner_uid
			FROM completion_seals WHERE run_id=? AND completion_request_id=?`, p.RunID, p.CompletionRequest).
			Scan(&existing.RunID, &existing.TaskID, &existing.CompletionRequest, &existing.ObjectFormat,
				&existing.SealedSHA, &existing.ClosureDigest, &existing.ManifestDigest,
				&existing.Device, &existing.Inode, &existing.OwnerUID)
		if err == nil {
			if existing == p {
				return nil
			}
			return Domainf("completion seal publication conflicts with its immutable receipt")
		}
		if err != sql.ErrNoRows {
			return err
		}
		var tier, role, taskState string
		var subject sql.NullInt64
		var endedAt sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT tier,role,subject,ended_at FROM runs WHERE id=?`, p.RunID).
			Scan(&tier, &role, &subject, &endedAt); err != nil {
			return Domainf("completion seal producer run is absent")
		}
		if tier != "pipeline" || role != "worker" || !subject.Valid || subject.Int64 != p.TaskID || endedAt.Valid {
			return Domainf("completion seal publication is not a live task Worker")
		}
		if err := q.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, p.TaskID).Scan(&taskState); err != nil || taskState != "seeded" {
			return Domainf("completion seal publication is not for a seeded task")
		}
		if _, err := fenceRun(ctx, q, p.RunID); err != nil {
			return Domainf("completion seal publication lost its Worker lease fence")
		}
		_, err = q.ExecContext(ctx, `
			INSERT INTO completion_seals
			(run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			p.RunID, p.TaskID, p.CompletionRequest, p.ObjectFormat, p.SealedSHA, p.ClosureDigest,
			p.ManifestDigest, p.Device, p.Inode, p.OwnerUID)
		return err
	})
}

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
		result, err = q.ExecContext(ctx, `UPDATE tasks
			SET accepted_completion_run_id=?, accepted_completion_request_id=?
			WHERE id=? AND status='worked'`, runID, requestID, taskID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return Domainf("completion seal acceptance lost its task receipt fence")
		}
		if err := endRun(ctx, q, runID, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, runID)
	})
}

// CompleteSealedWorker is the privileged wrapper's terminal half. Filesystem
// publication happens before this call; this method binds that immutable,
// path-free receipt to the caller's exact Worker identity and performs the
// one allowed seeded -> worked terminal transition through acceptance.
func CompleteSealedWorker(db *sql.DB, id *RunIdentity, p CompletionSealPublication) (map[string]any, error) {
	if err := validateCompletionSealPublication(p); err != nil {
		return nil, err
	}
	if err := RequireSealedWorkerIdentity(id, p.RunID); err != nil {
		return nil, err
	}
	if err := PublishCompletionSeal(db, p); err != nil {
		return nil, err
	}
	if err := AcceptCompletionSeal(db, p.RunID, p.CompletionRequest); err != nil {
		return nil, err
	}
	return map[string]any{"task_id": p.TaskID, "status": "worked", "completion_request_id": p.CompletionRequest}, nil
}

// RequireSealedWorkerIdentity rejects an unprivileged or stale caller before
// the wrapper performs any filesystem publication into its gated seal root.
func RequireSealedWorkerIdentity(id *RunIdentity, runID string) error {
	if err := requireExactRole(id, "worker"); err != nil {
		return err
	}
	return requireOwnRun(id, runID)
}
