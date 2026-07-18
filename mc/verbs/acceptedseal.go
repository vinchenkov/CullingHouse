package verbs

import (
	"context"
	"database/sql"
)

// AcceptedCompletionSeal is the path-free immutable authority a later setup
// operation names before it opens the corresponding sealed filesystem object.
// It deliberately carries no host path: the resident derives that from run id
// and re-attests the stored device/inode at the file-plane boundary.
type AcceptedCompletionSeal struct {
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

// LoadAcceptedCompletionSeal returns only an exact immutable accepted Worker
// completion whose producer has the matching durable completed terminal. This
// is the spine half of D6's accepted-seal rebuild fence; filesystem identity
// and producer-absence rechecks remain at the setup/resident boundary.
func LoadAcceptedCompletionSeal(db *sql.DB, runID, requestID string) (AcceptedCompletionSeal, error) {
	var out AcceptedCompletionSeal
	err := inTx(db, func(ctx context.Context, q Q) error {
		var tier, role, outcome string
		var ended sql.NullString
		var state string
		var manifest sql.NullString
		err := q.QueryRowContext(ctx, `SELECT s.run_id,s.task_id,s.completion_request_id,s.object_format,s.sealed_sha,s.closure_digest,s.manifest_digest,s.seal_device,s.seal_inode,s.seal_owner_uid,s.state,r.tier,r.role,r.ended_at,COALESCE(r.outcome,'') FROM completion_seals s JOIN runs r ON r.id=s.run_id WHERE s.run_id=? AND s.completion_request_id=?`, runID, requestID).Scan(&out.RunID, &out.TaskID, &out.CompletionRequest, &out.ObjectFormat, &out.SealedSHA, &out.ClosureDigest, &manifest, &out.Device, &out.Inode, &out.OwnerUID, &state, &tier, &role, &ended, &outcome)
		if err != nil {
			return Domainf("accepted completion seal receipt is absent")
		}
		if state != "accepted" || tier != "pipeline" || role != "worker" || !ended.Valid || outcome != "completed" {
			return Domainf("completion seal is not an accepted completed Worker receipt")
		}
		if !manifest.Valid || len(manifest.String) != 64 || !assignmentHex.MatchString(manifest.String) {
			return Domainf("accepted completion seal has no canonical manifest digest")
		}
		out.ManifestDigest = manifest.String
		return nil
	})
	return out, err
}
