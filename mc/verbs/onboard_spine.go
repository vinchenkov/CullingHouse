package verbs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mc/substrate"
)

// OnboardSpineRequest is the bounded, path-free host→helper frame used during
// production Home provisioning. The caller's mirror observation is evidence;
// the helper computes every spine fact itself from its fixed /mc/spine scope.
type OnboardSpineRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	SchemaVersion   int    `json:"schema_version"`
	MirrorState     string `json:"mirror_state"`
	MirrorUUID      string `json:"mirror_uuid,omitempty"`
}

type OnboardSpineResult struct {
	ProtocolVersion int    `json:"protocol_version"`
	Status          string `json:"status"`
	DeploymentUUID  string `json:"deployment_uuid"`
	SchemaVersion   int    `json:"schema_version"`
}

// PrepareOnboardHome performs the host half of the production Home crossing.
// It validates the canonical home before any write and sends only the
// deployment mirror observation to the helper; it never receives a spine
// path and therefore cannot open the runtime-local database.
func PrepareOnboardHome() (OnboardSpineRequest, error) {
	if _, _, err := onboardPreflight(); err != nil {
		return OnboardSpineRequest{}, err
	}
	home, err := mcHomeDir()
	if err != nil {
		return OnboardSpineRequest{}, err
	}
	uuid, exists, err := readDeploymentMirror(home)
	if err != nil {
		return OnboardSpineRequest{}, err
	}
	req := OnboardSpineRequest{
		ProtocolVersion: 1,
		SchemaVersion:   substrate.CurrentSchemaVersion,
		MirrorState:     "absent",
	}
	if exists {
		req.MirrorState = "present"
		req.MirrorUUID = uuid
	}
	return req, nil
}

// FinalizeOnboardHome is the host half after the helper has committed and
// re-read spine identity. It scaffolds MC_HOME and atomically publishes only
// the returned UUID mirror; no database bytes cross kernels.
func FinalizeOnboardHome(result OnboardSpineResult) (string, string, error) {
	if result.ProtocolVersion != 1 || result.SchemaVersion != substrate.CurrentSchemaVersion {
		return "", "", Domainf("helper onboard-spine response has mismatched protocol/schema identity")
	}
	if len(result.DeploymentUUID) != 32 {
		return "", "", Domainf("helper onboard-spine response has invalid deployment UUID")
	}
	if _, err := hex.DecodeString(result.DeploymentUUID); err != nil || strings.ToLower(result.DeploymentUUID) != result.DeploymentUUID {
		return "", "", Domainf("helper onboard-spine response has invalid deployment UUID")
	}
	validStatus := map[string]bool{
		"ok": true, "initialized": true, "repair-mirror": true,
		"migrated": true, "migrated-repair-mirror": true,
	}
	if !validStatus[result.Status] {
		return "", "", Domainf("helper onboard-spine response has invalid status %q", result.Status)
	}
	home, err := mcHomeDir()
	if err != nil {
		return "", "", err
	}
	mirrored, mirrorExists, err := readDeploymentMirror(home)
	if err != nil {
		return "", "", err
	}
	if mirrorExists && mirrored != result.DeploymentUUID {
		return "", "", Domainf("deployment identity mismatch: MC_HOME has %s but helper proved spine %s — restore the matching backup (§16.4)", mirrored, result.DeploymentUUID)
	}
	changed, err := scaffoldOnboardHome(home)
	if err != nil {
		return "", "", err
	}
	if !mirrorExists {
		if err := writeDeploymentMirror(home, result.DeploymentUUID); err != nil {
			return "", "", err
		}
		changed = true
	}
	status := "ok"
	if changed || result.Status != "ok" {
		status = "done"
	}
	return status, fmt.Sprintf("deployment %s provisioned through helper (schema %d)", result.DeploymentUUID, result.SchemaVersion), nil
}

func validateOnboardSpineRequest(req OnboardSpineRequest) error {
	if req.ProtocolVersion != 1 {
		return Usagef("private onboard-spine protocol_version %d is unsupported", req.ProtocolVersion)
	}
	if req.SchemaVersion != substrate.CurrentSchemaVersion {
		return Domainf("private onboard-spine schema identity %d does not match helper build %d",
			req.SchemaVersion, substrate.CurrentSchemaVersion)
	}
	switch req.MirrorState {
	case "absent":
		if req.MirrorUUID != "" {
			return Usagef("private onboard-spine absent mirror carries a UUID")
		}
	case "present":
		if req.MirrorUUID == "" {
			return Usagef("private onboard-spine present mirror needs its UUID")
		}
	default:
		return Usagef("private onboard-spine mirror_state must be absent or present")
	}
	return nil
}

// OnboardSpine performs only runtime-local spine work. It never reads MC_HOME,
// config, credentials, routing, or a host path; production calls it with the
// helper's fixed /mc/spine/spine.db path after private scope validation.
func OnboardSpine(spine string, req OnboardSpineRequest) (OnboardSpineResult, error) {
	if err := validateOnboardSpineRequest(req); err != nil {
		return OnboardSpineResult{}, err
	}
	inspection, err := inspectSpineReadOnly(spine)
	if err != nil {
		return OnboardSpineResult{}, err
	}
	if inspection.tables > 0 {
		if err := validateInspection(spine, inspection); err != nil {
			return OnboardSpineResult{}, err
		}
		if req.MirrorState == "present" && req.MirrorUUID != inspection.uuid {
			return OnboardSpineResult{}, Domainf(
				"deployment identity mismatch: host mirror has %s but spine has %s — restore the matching backup (§16.4)",
				req.MirrorUUID, inspection.uuid)
		}
		migrated, err := migrateSpine(spine)
		if err != nil {
			return OnboardSpineResult{}, err
		}
		status := "ok"
		if req.MirrorState == "absent" {
			status = "repair-mirror"
		}
		if migrated {
			status = "migrated"
			if req.MirrorState == "absent" {
				status = "migrated-repair-mirror"
			}
		}
		return OnboardSpineResult{
			ProtocolVersion: 1,
			Status:          status,
			DeploymentUUID:  inspection.uuid,
			SchemaVersion:   substrate.CurrentSchemaVersion,
		}, nil
	}
	if inspection.exists && inspection.bytes > 0 {
		return OnboardSpineResult{}, Domainf(
			"spine at %q is non-empty (%d bytes) but has no meta identity — no onboarding path may re-initialize it; restore from backup (§16.4)",
			spine, inspection.bytes)
	}
	if req.MirrorState == "present" {
		return OnboardSpineResult{}, Domainf(
			"spine lost — restore from backup: MC_HOME identity %s has no non-empty spine at %q (§16.4)",
			req.MirrorUUID, spine)
	}

	parent := filepath.Dir(spine)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return OnboardSpineResult{}, Usagef("create spine volume root: %v", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return OnboardSpineResult{}, Usagef("secure spine volume root: %v", err)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return OnboardSpineResult{}, Usagef("generate deployment uuid: %v", err)
	}
	uuid := hex.EncodeToString(raw[:])
	db, err := substrate.Open(spine)
	if err != nil {
		return OnboardSpineResult{}, Usagef("%v", err)
	}
	err = inTx(db, func(ctx context.Context, q Q) error {
		if _, err := q.ExecContext(ctx, substrate.Schema); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx,
			`INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, ?, ?)`,
			uuid, substrate.CurrentSchemaVersion)
		return err
	})
	closeErr := db.Close()
	if err != nil {
		removeFreshSpineFiles(spine)
		return OnboardSpineResult{}, Domainf("initialize spine: %v", err)
	}
	if closeErr != nil {
		return OnboardSpineResult{}, Domainf("close provisioned spine: %v", closeErr)
	}
	if err := os.Chmod(spine, 0o600); err != nil {
		return OnboardSpineResult{}, Domainf("secure provisioned spine: %v", err)
	}
	return OnboardSpineResult{
		ProtocolVersion: 1,
		Status:          "initialized",
		DeploymentUUID:  uuid,
		SchemaVersion:   substrate.CurrentSchemaVersion,
	}, nil
}

func (r OnboardSpineResult) String() string {
	return fmt.Sprintf("%s deployment %s schema %d", r.Status, r.DeploymentUUID, r.SchemaVersion)
}
