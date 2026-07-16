package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/dispatch"
	"mc/substrate"
)

func privateRequest(uuid string) PrivateDispatchPrepareRequest {
	return PrivateDispatchPrepareRequest{
		Version:             1,
		ReleaseBuildID:      "development",
		ControlVersion:      1,
		SpineSchemaVersion:  substrate.CurrentSchemaVersion,
		ConfigSchemaVersion: 1,
		DeploymentUUID:      uuid,
		DispatchRequestID:   dfRequestID,
	}
}

func TestPrivateDispatchFramesRoundTripCandidateWithoutHostReadsInPrepare(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	req := privateRequest(dfUUID(t, db))

	var prepared PrivateDispatchPrepareResponse
	err := inTx(db, func(ctx context.Context, q Q) error {
		var e error
		prepared, e = DispatchPreparePrivate(ctx, q, req)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Kind != "candidate" || prepared.Candidate == nil || prepared.Final != nil {
		t.Fatalf("prepared frame = %+v, want candidate only", prepared)
	}
	if prepared.Candidate.Token == "" || prepared.Candidate.RunID == "" || prepared.Candidate.Role != "editor" {
		t.Fatalf("prepared candidate omitted bounded identity: %+v", prepared.Candidate)
	}

	commit, err := DispatchAttestPrivate(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if commit.Candidate.Token != prepared.Candidate.Token || commit.Attestation.RoutingDigest == "" {
		t.Fatalf("commit frame did not bind prepare + host attestation: %+v", commit)
	}

	var result PrivateDispatchResult
	err = inTx(db, func(ctx context.Context, q Q) error {
		var e error
		result, e = DispatchCommitPrivate(ctx, q, commit)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	var effect map[string]any
	if err := json.Unmarshal(result.Result, &effect); err != nil {
		t.Fatal(err)
	}
	if effect["action"] != "spawn" || effect["run_id"] != prepared.Candidate.RunID {
		t.Fatalf("private commit result = %v, want prepared spawn", effect)
	}
}

func TestPrivateDispatchCandidateFreezesSelectedWorksourceAndEveryProfile(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	dvExec(t, db, `UPDATE sandbox_profiles SET artifact_roots=?, readonly_mounts=?, denied_paths=?,
		tool_home_dir=?, runtime_control_dir=? WHERE id='default'`,
		`["/tmp/artifact-b","/tmp/artifact-a"]`, `[
  "/tmp/reference"
]`, `["/tmp/denied"]`, `/tmp/tool-home`, `/tmp/runtime-control`)
	dvExec(t, db, `INSERT INTO sandbox_profiles (id, workspace_root, artifact_roots, readonly_mounts,
		denied_paths, egress_policy) VALUES ('other-profile','/tmp/other','[]','[]','[]','none')`)
	dvExec(t, db, `INSERT INTO worksources (id,title,kind,sandbox_profile,status)
		VALUES ('other','Other','repo','other-profile','paused')`)

	prepare := func(requestID string) PrivateDispatchPrepareResponse {
		req := privateRequest(dfUUID(t, db))
		req.DispatchRequestID = requestID
		var out PrivateDispatchPrepareResponse
		if err := inTx(db, func(ctx context.Context, q Q) error {
			var err error
			out, err = DispatchPreparePrivate(ctx, q, req)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}

	first := prepare("0123456789abcdea")
	if first.Candidate == nil {
		t.Fatal("prepare returned no candidate")
	}
	state := first.Candidate.MountState
	if state.SelectedWorksource != "ws-test" || len(state.Worksources) != 2 {
		t.Fatalf("mount state = %+v, want selected ws-test and every Worksource", state)
	}
	if got := state.Worksources[1]; got.WorksourceID != "ws-test" || got.ProfileID != "default" ||
		len(got.ArtifactRoots) != 2 || got.ArtifactRoots[0] != "/tmp/artifact-a" ||
		len(got.ReadonlyMounts) != 1 || got.DeniedPaths[0] != "/tmp/denied" ||
		got.ToolHomeDir != "/tmp/tool-home" || got.RuntimeControlDir != "/tmp/runtime-control" {
		t.Fatalf("normalized selected profile = %+v", got)
	}

	dvExec(t, db, `UPDATE sandbox_profiles SET readonly_mounts='["/tmp/reference-2"]' WHERE id='default'`)
	second := prepare("0123456789abcdeb")
	if second.Candidate.Token == first.Candidate.Token {
		t.Fatal("profile drift did not change the preparation token")
	}
}

func TestWorksourceArchiveRejectsProjectionBeyondAggregateBudget(t *testing.T) {
	db := dvSpine(t)
	roots := make([]string, 0, 64)
	for i := 0; i < 63; i++ {
		prefix := fmt.Sprintf("/%02d", i)
		roots = append(roots, prefix+strings.Repeat("x", 4096-len(prefix)))
	}
	roots = append(roots, "/z")
	row := substrate.DispatchWorksource{
		WorksourceID: "ws-test", Kind: "personal", Status: "active", ProfilePresent: true,
		ProfileID: "default", WorkspaceRoot: "/tmp/ws-test", ArtifactRoots: roots,
		ReadonlyMounts: []string{}, DeniedPaths: []string{},
	}
	body, err := json.Marshal([]substrate.DispatchWorksource{row})
	if err != nil {
		t.Fatal(err)
	}
	delta := substrate.MaxDispatchMountProjectionBytes - len(body)
	if delta < 0 || delta > 4094 {
		t.Fatalf("exact-budget fixture needs delta %d outside 0..4094", delta)
	}
	roots[len(roots)-1] += strings.Repeat("x", delta)
	artifactJSON, err := json.Marshal(roots)
	if err != nil {
		t.Fatal(err)
	}
	dvExec(t, db, `UPDATE sandbox_profiles SET artifact_roots=? WHERE id='default'`, string(artifactJSON))
	if err := substrate.ValidateDispatchMountProjection(context.Background(), db); err != nil {
		t.Fatalf("exact aggregate budget rejected: %v", err)
	}

	if _, err := WorksourceSetStatus(db, nil, "ws-test", "archived"); err == nil {
		t.Fatal("archive admitted a Worksource projection beyond the aggregate budget")
	}
	if got := dfStr(t, db, `SELECT status FROM worksources WHERE id='ws-test'`); got != "active" {
		t.Fatalf("rejected archive left status %q, want transactional rollback to active", got)
	}
}

func TestPrivateDispatchPrepareReturnsFinalWithoutAttest(t *testing.T) {
	db := dvSpine(t)
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('fresh-run', 'pipeline', 'worker', 'ws-test', NULL)`)
	dvExec(t, db, `UPDATE lock SET run_id='fresh-run', worksource='ws-test', owner='worker', acquired_at=?, hard_deadline_at=? WHERE id=1`,
		dvFuture.Format(spineTime), dvFuture.AddDate(0, 0, 1).Format(spineTime))
	prepared, err := func() (PrivateDispatchPrepareResponse, error) {
		var out PrivateDispatchPrepareResponse
		err := inTx(db, func(ctx context.Context, q Q) error {
			var e error
			out, e = DispatchPreparePrivate(ctx, q, privateRequest(dfUUID(t, db)))
			return e
		})
		return out, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Kind != "final" || prepared.Candidate != nil || prepared.Final == nil {
		t.Fatalf("prepared frame = %+v, want final only", prepared)
	}
	var effect map[string]any
	if err := json.Unmarshal(*prepared.Final, &effect); err != nil {
		t.Fatal(err)
	}
	if effect["action"] != "idle" {
		t.Fatalf("unexpected final effect %v, want idle", effect)
	}
}

func TestPrivateDispatchPrepareDoesNotReadHostRouting(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	if err := os.Remove(filepath.Join(os.Getenv("MC_HOME"), "routing.md")); err != nil {
		t.Fatal(err)
	}
	var prepared PrivateDispatchPrepareResponse
	err := inTx(db, func(ctx context.Context, q Q) error {
		var e error
		prepared, e = DispatchPreparePrivate(ctx, q, privateRequest(dfUUID(t, db)))
		return e
	})
	if err != nil {
		t.Fatalf("lock-domain prepare touched missing host routing: %v", err)
	}
	if prepared.Kind != "candidate" || prepared.Candidate == nil {
		t.Fatalf("prepare = %+v, want candidate independent of routing.md", prepared)
	}
	attested, err := DispatchAttestPrivate(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if attested.Attestation.Refusal == nil || attested.Attestation.Refusal.Code != "health.routing_invalid" {
		t.Fatalf("host attest = %+v, want routing health refusal", attested.Attestation)
	}
}

func TestPrivateDispatchRechecksHostFilesImmediatelyBeforeCommit(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	prepared := func() PrivateDispatchPrepareResponse {
		var out PrivateDispatchPrepareResponse
		if err := inTx(db, func(ctx context.Context, q Q) error {
			var err error
			out, err = DispatchPreparePrivate(ctx, q, privateRequest(dfUUID(t, db)))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}()
	home := os.Getenv("MC_HOME")
	commit, err := DispatchAttestPrivate(home, prepared)
	if err != nil {
		t.Fatal(err)
	}
	routingPath := filepath.Join(home, "routing.md")
	raw, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routingPath, append(raw, []byte("\n# concurrent operator edit\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	commit = DispatchRecheckPrivate(home, prepared, commit)
	if commit.Attestation.Refusal == nil || commit.Attestation.Refusal.Code != "preflight.stale" {
		t.Fatalf("recheck = %+v, want stale refusal", commit.Attestation)
	}
	var result PrivateDispatchResult
	if err := inTx(db, func(ctx context.Context, q Q) error {
		var err error
		result, err = DispatchCommitPrivate(ctx, q, commit)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var effect map[string]any
	if err := json.Unmarshal(result.Result, &effect); err != nil {
		t.Fatal(err)
	}
	if effect["action"] != "refused" || effect["consequence"] != "none" {
		t.Fatalf("stale recheck effect = %v", effect)
	}
	if got := dfInt(t, db, `SELECT COUNT(*) FROM runs`); got != 0 {
		t.Fatalf("stale host-file recheck opened %d runs", got)
	}
}

func TestPrivateCandidateStructuralBounds(t *testing.T) {
	base := PrivateDispatchCandidate{
		RunID: "0123456789abcdef", Role: "worker", ProposedPool: []int64{}, Wave: []int64{},
		DedupeTitles: []string{}, Token: strings.Repeat("a", 64),
		TimeoutMinutes: 120, GraceMinutes: 30, HeartbeatIntervalS: 30,
		SpawnGraceS: 300, HardDeadlineMinutes: 480, ConsoleHour: 9,
		ConsoleMinute: 30, ConsoleTZ: "America/Los_Angeles",
	}
	tests := map[string]func(*PrivateDispatchCandidate){
		"role_control":  func(c *PrivateDispatchCandidate) { c.Role = "worker\n" },
		"duplicate_ids": func(c *PrivateDispatchCandidate) { c.Wave = []int64{1, 1} },
		"title_control": func(c *PrivateDispatchCandidate) { c.DedupeTitles = []string{"secret\nnext"} },
		"invalid_utf8":  func(c *PrivateDispatchCandidate) { c.ConsoleTZ = string([]byte{0xff}) },
		"scalar_oversize": func(c *PrivateDispatchCandidate) {
			c.ConsoleTZ = strings.Repeat("x", maxPrivateScalarBytes+1)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if _, err := preparedFromCandidate(defaultDispatchProtocolIdentity,
				"deployment-test", dfRequestID, &candidate); err == nil {
				t.Fatal("malformed candidate projection was accepted")
			}
		})
	}
}
