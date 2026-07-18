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
	"mc/refusal"
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
		MountState: PrivateDispatchMountState{Worksources: []PrivateDispatchWorksource{}},
	}
	// The unmutated base must be ACCEPTED, or every subtest below rejects for
	// the wrong reason and the named validations go unproven (the takeover
	// review found exactly that: a missing MountState voided all five,
	// 2026-07-16).
	accepted := base
	if _, err := preparedFromCandidate(defaultDispatchProtocolIdentity,
		"deployment-test", dfRequestID, &accepted); err != nil {
		t.Fatalf("the unmutated base candidate must be accepted, got: %v", err)
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

// The helper never trusts the broker's plan shape: a route attestation must
// carry an explicit, bounded, evidence-complete plan with strictly ordered
// unique non-overlapping absolute destinations; a refusal carries none.
func TestValidatePrivateAttestationMountPlanRules(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	entry := PrivateDispatchMountEntry{
		Access: "rw", Destination: "/workspace/artifacts/art", Device: "42", Inode: "7",
		Kind: "dir", LogicalID: "artifact:art", Mode: 448, OwnerUID: 501, Source: "/srv/artifact",
	}
	route := func(plan *PrivateDispatchMountPlan) PrivateDispatchAttestation {
		return PrivateDispatchAttestation{
			RoutingDigest: digest, Harness: "claude-sdk", Binding: "minimax", MountPlan: plan,
		}
	}
	valid := func() *PrivateDispatchMountPlan {
		return &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{entry}}
	}
	if err := validatePrivateAttestation(route(valid())); err != nil {
		t.Fatalf("a valid route+plan attestation was rejected: %v", err)
	}
	if err := validatePrivateAttestation(route(&PrivateDispatchMountPlan{
		Version: 1, Entries: []PrivateDispatchMountEntry{},
	})); err != nil {
		t.Fatalf("an explicit empty plan was rejected: %v", err)
	}
	if err := validatePrivateAttestation(route(nil)); err == nil {
		t.Fatal("a route attestation without an explicit plan was accepted")
	}
	if err := validatePrivateAttestation(PrivateDispatchAttestation{
		MountPlan: valid(),
		Refusal:   &PrivateDispatchRefusal{Code: refusal.CodeStale, Field: "none", Summary: "mismatch"},
	}); err == nil {
		t.Fatal("a refusal attestation carrying a plan was accepted")
	}

	mutations := map[string]func(*PrivateDispatchMountPlan){
		"nil_entries":     func(p *PrivateDispatchMountPlan) { p.Entries = nil },
		"bad_version":     func(p *PrivateDispatchMountPlan) { p.Version = 2 },
		"bad_access":      func(p *PrivateDispatchMountPlan) { p.Entries[0].Access = "rwx" },
		"bad_kind":        func(p *PrivateDispatchMountPlan) { p.Entries[0].Kind = "socket" },
		"relative_source": func(p *PrivateDispatchMountPlan) { p.Entries[0].Source = "srv/artifact" },
		"colon_source":    func(p *PrivateDispatchMountPlan) { p.Entries[0].Source = "/srv/a:b" },
		"unclean_dest":    func(p *PrivateDispatchMountPlan) { p.Entries[0].Destination = "/workspace//art" },
		"hex_inode":       func(p *PrivateDispatchMountPlan) { p.Entries[0].Inode = "0x2a" },
		"empty_device":    func(p *PrivateDispatchMountPlan) { p.Entries[0].Device = "" },
		"mode_over_perm":  func(p *PrivateDispatchMountPlan) { p.Entries[0].Mode = 0o1000 },
		"negative_owner":  func(p *PrivateDispatchMountPlan) { p.Entries[0].OwnerUID = -1 },
		"unsorted_destinations": func(p *PrivateDispatchMountPlan) {
			second := entry
			second.Destination = "/workspace/artifacts/aaa"
			second.LogicalID = "artifact:aaa"
			p.Entries = append(p.Entries, second)
		},
		"overlapping_destinations": func(p *PrivateDispatchMountPlan) {
			second := entry
			second.Destination = entry.Destination + "/nested"
			second.LogicalID = "artifact:art/nested"
			p.Entries = append(p.Entries, second)
		},
		"duplicate_destinations": func(p *PrivateDispatchMountPlan) {
			p.Entries = append(p.Entries, entry)
		},
		"colon_destination": func(p *PrivateDispatchMountPlan) { p.Entries[0].Destination = "/workspace/a:b" },
		"destination_outside_workspace": func(p *PrivateDispatchMountPlan) {
			p.Entries[0].Destination = "/mc/session"
			p.Entries[0].LogicalID = "artifact:session"
		},
		"duplicate_logical_ids": func(p *PrivateDispatchMountPlan) {
			second := entry
			second.Destination = "/workspace/artifacts/other"
			p.Entries = append([]PrivateDispatchMountEntry{second}, p.Entries...)
		},
		"oversized_plan_bytes": func(p *PrivateDispatchMountPlan) {
			// Structurally valid and honestly sorted, but past the 32 KiB
			// byte budget: the helper's own bound must refuse it even though
			// the producer bound would never have emitted it.
			p.Entries = nil
			for i := 0; i < 9; i++ {
				huge := entry
				huge.Destination = fmt.Sprintf("/workspace/artifacts/bulk-%d", i)
				huge.LogicalID = fmt.Sprintf("artifact:bulk-%d:%s", i, strings.Repeat("x", 4000))
				p.Entries = append(p.Entries, huge)
			}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			plan := valid()
			mutate(plan)
			if err := validatePrivateAttestation(route(plan)); err == nil {
				t.Fatal("a malformed plan attestation was accepted")
			}
		})
	}
}

func TestValidatePrivateAttestationTaskPrecreateRules(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	valid := func() *PrivateDispatchMountPlan {
		return &PrivateDispatchMountPlan{
			Entries: []PrivateDispatchMountEntry{},
			TaskPrecreate: &PrivateDispatchTaskPrecreate{
				ChildMode: 0o700, TaskID: 7, WorkspaceRoot: "/srv/repo",
				Setup: &PrivateDispatchTaskSetup{Mode: "fresh", ObjectFormat: "sha1", TargetRef: "main"},
				TasksParent: PrivateDispatchPathIdentity{
					Canonical: "/srv/repo/.mission-control/tasks", Device: "8", Inode: "9", OwnerUID: 501,
				},
			},
			Version: 1,
		}
	}
	retrySetup := func() *PrivateDispatchTaskSetup {
		return &PrivateDispatchTaskSetup{
			Mode: "retry", ObjectFormat: "sha1",
			PinnedBaseSHA:       strings.Repeat("a", 40),
			PinnedClosureDigest: strings.Repeat("b", 64),
			PinnedLocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
		}
	}
	route := func(plan *PrivateDispatchMountPlan) PrivateDispatchAttestation {
		return PrivateDispatchAttestation{
			RoutingDigest: digest, Harness: "claude-sdk", Binding: "minimax", MountPlan: plan,
		}
	}
	if err := validatePrivateAttestation(route(valid())); err != nil {
		t.Fatalf("valid task precreate rejected: %v", err)
	}
	validRetry := valid()
	validRetry.TaskPrecreate.Setup = retrySetup()
	if err := validatePrivateAttestation(route(validRetry)); err != nil {
		t.Fatalf("valid retry task precreate rejected: %v", err)
	}
	mutations := map[string]func(*PrivateDispatchTaskPrecreate){
		"relative_workspace": func(s *PrivateDispatchTaskPrecreate) { s.WorkspaceRoot = "srv/repo" },
		"wrong_parent":       func(s *PrivateDispatchTaskPrecreate) { s.TasksParent.Canonical = "/srv/other" },
		"hex_device":         func(s *PrivateDispatchTaskPrecreate) { s.TasksParent.Device = "0x8" },
		"negative_owner":     func(s *PrivateDispatchTaskPrecreate) { s.TasksParent.OwnerUID = -1 },
		"zero_task":          func(s *PrivateDispatchTaskPrecreate) { s.TaskID = 0 },
		"unwritable_mode":    func(s *PrivateDispatchTaskPrecreate) { s.ChildMode = 0o500 },
		"widened_mode":       func(s *PrivateDispatchTaskPrecreate) { s.ChildMode = 0o777 },
		"no_setup":           func(s *PrivateDispatchTaskPrecreate) { s.Setup = nil },
		"recovery_wrong_root": func(s *PrivateDispatchTaskPrecreate) {
			s.RecoverRoot = &PrivateDispatchPathIdentity{Canonical: "/srv/repo/.mission-control/tasks/task-8", Device: "8", Inode: "10", OwnerUID: 501}
		},
		"recovery_bad_identity": func(s *PrivateDispatchTaskPrecreate) {
			s.RecoverRoot = &PrivateDispatchPathIdentity{Canonical: "/srv/repo/.mission-control/tasks/task-7", Device: "x", Inode: "10", OwnerUID: 501}
		},
		"unknown_mode":    func(s *PrivateDispatchTaskPrecreate) { s.Setup.Mode = "rebase" },
		"unknown_format":  func(s *PrivateDispatchTaskPrecreate) { s.Setup.ObjectFormat = "sha512" },
		"fresh_no_target": func(s *PrivateDispatchTaskPrecreate) { s.Setup.TargetRef = "" },
		"fresh_with_pin": func(s *PrivateDispatchTaskPrecreate) {
			s.Setup.PinnedBaseSHA = strings.Repeat("a", 40)
		},
		"retry_with_target": func(s *PrivateDispatchTaskPrecreate) {
			s.Setup = retrySetup()
			s.Setup.TargetRef = "main"
		},
		"retry_short_sha": func(s *PrivateDispatchTaskPrecreate) {
			s.Setup = retrySetup()
			s.Setup.PinnedBaseSHA = strings.Repeat("a", 39)
		},
		"retry_sha_format_mismatch": func(s *PrivateDispatchTaskPrecreate) {
			s.Setup = retrySetup()
			s.Setup.ObjectFormat = "sha256" // pins stay 40 hex: sha1-shaped
		},
		"retry_upper_digest": func(s *PrivateDispatchTaskPrecreate) {
			s.Setup = retrySetup()
			s.Setup.PinnedClosureDigest = strings.Repeat("B", 64)
		},
		"retry_bad_uuid": func(s *PrivateDispatchTaskPrecreate) {
			s.Setup = retrySetup()
			s.Setup.PinnedLocalRepoUUID = "not-a-uuid"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			plan := valid()
			mutate(plan.TaskPrecreate)
			if err := validatePrivateAttestation(route(plan)); err == nil {
				t.Fatal("malformed task precreate was accepted")
			}
		})
	}
	plan := valid()
	plan.Entries = []PrivateDispatchMountEntry{{
		Access: "ro", Destination: "/workspace", Device: "42", Inode: "7",
		Kind: "dir", LogicalID: "task-root", Mode: 0o555, OwnerUID: 501, Source: "/srv/repo/task-7",
	}}
	if err := validatePrivateAttestation(route(plan)); err == nil {
		t.Fatal("precreate plan fabricated an existing task-root bind")
	}
}

func TestValidatePrivateTaskPrecreateCandidateRejectsHostilePairings(t *testing.T) {
	taskID := int64(7)
	step := &PrivateDispatchTaskPrecreate{
		ChildMode: 0o700, TaskID: taskID, WorkspaceRoot: "/srv/repo",
		Setup: &PrivateDispatchTaskSetup{Mode: "fresh", ObjectFormat: "sha1", TargetRef: "main"},
		TasksParent: PrivateDispatchPathIdentity{
			Canonical: "/srv/repo/.mission-control/tasks", Device: "8", Inode: "9", OwnerUID: 501,
		},
	}
	valid := func() *preparedCandidate {
		return &preparedCandidate{
			spawn: &dispatch.Spawn{Role: dispatch.RoleWorker, SubjectID: &taskID},
			mountState: PrivateDispatchMountState{
				SelectedWorksource: "repo",
				Worksources: []PrivateDispatchWorksource{{
					WorksourceID: "repo", Kind: "repo", WorkspaceRoot: "/srv/repo",
				}},
				SubjectTaskTargetRef: "main",
			},
		}
	}
	plan := &PrivateDispatchMountPlan{Entries: []PrivateDispatchMountEntry{}, TaskPrecreate: step, Version: 1}
	if err := validatePrivateTaskPrecreateCandidate(valid(), plan); err != nil {
		t.Fatalf("valid candidate pairing rejected: %v", err)
	}
	cases := map[string]func(*preparedCandidate){
		"non_worker": func(c *preparedCandidate) { c.spawn.Role = dispatch.RoleEditor },
		"wrong_task": func(c *preparedCandidate) {
			other := int64(8)
			c.spawn.SubjectID = &other
		},
		"initiative_child": func(c *preparedCandidate) {
			initiative := int64(9)
			c.mountState.SubjectInitiativeID = &initiative
		},
		"wrong_workspace": func(c *preparedCandidate) { c.mountState.Worksources[0].WorkspaceRoot = "/srv/other" },
		"non_repo":        func(c *preparedCandidate) { c.mountState.Worksources[0].Kind = "personal" },
		// The setup instruction must restate the frozen state, not invent it:
		// a fresh plan whose target differs from the frozen ref, or one that
		// ignores a frozen assignment row, is a forged instruction.
		"fresh_target_drift": func(c *preparedCandidate) { c.mountState.SubjectTaskTargetRef = "other" },
		"assignment_ignored_by_fresh_plan": func(c *preparedCandidate) {
			c.mountState.SubjectTaskAssignment = &PrivateDispatchTaskAssignment{
				BaseSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64),
				LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9", ObjectFormat: "sha1",
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cand := valid()
			mutate(cand)
			if err := validatePrivateTaskPrecreateCandidate(cand, plan); err == nil {
				t.Fatal("hostile task-precreate pairing was accepted")
			}
		})
	}
}

func TestValidatePrivateTaskPrecreateCandidateRetryPins(t *testing.T) {
	taskID := int64(7)
	assignment := func() *PrivateDispatchTaskAssignment {
		return &PrivateDispatchTaskAssignment{
			BaseSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64),
			LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9", ObjectFormat: "sha1",
		}
	}
	step := &PrivateDispatchTaskPrecreate{
		ChildMode: 0o700, TaskID: taskID, WorkspaceRoot: "/srv/repo",
		Setup: &PrivateDispatchTaskSetup{
			Mode: "retry", ObjectFormat: "sha1",
			PinnedBaseSHA:       strings.Repeat("a", 40),
			PinnedClosureDigest: strings.Repeat("b", 64),
			PinnedLocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
		},
		TasksParent: PrivateDispatchPathIdentity{
			Canonical: "/srv/repo/.mission-control/tasks", Device: "8", Inode: "9", OwnerUID: 501,
		},
	}
	valid := func() *preparedCandidate {
		return &preparedCandidate{
			spawn: &dispatch.Spawn{Role: dispatch.RoleWorker, SubjectID: &taskID},
			mountState: PrivateDispatchMountState{
				SelectedWorksource: "repo",
				Worksources: []PrivateDispatchWorksource{{
					WorksourceID: "repo", Kind: "repo", WorkspaceRoot: "/srv/repo",
				}},
				SubjectTaskAssignment: assignment(),
				SubjectTaskTargetRef:  "main",
			},
		}
	}
	plan := &PrivateDispatchMountPlan{Entries: []PrivateDispatchMountEntry{}, TaskPrecreate: step, Version: 1}
	if err := validatePrivateTaskPrecreateCandidate(valid(), plan); err != nil {
		t.Fatalf("valid retry pairing rejected: %v", err)
	}
	cases := map[string]func(*preparedCandidate){
		// A retry plan with no frozen assignment fabricates a closure pin.
		"no_assignment": func(c *preparedCandidate) { c.mountState.SubjectTaskAssignment = nil },
		"base_sha_drift": func(c *preparedCandidate) {
			c.mountState.SubjectTaskAssignment.BaseSHA = strings.Repeat("c", 40)
		},
		"digest_drift": func(c *preparedCandidate) {
			c.mountState.SubjectTaskAssignment.ClosureDigest = strings.Repeat("c", 64)
		},
		"uuid_drift": func(c *preparedCandidate) {
			c.mountState.SubjectTaskAssignment.LocalRepoUUID = "ffffffff-4e5f-6071-8293-a4b5c6d7e8f9"
		},
		"format_drift": func(c *preparedCandidate) {
			c.mountState.SubjectTaskAssignment.ObjectFormat = "sha256"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cand := valid()
			mutate(cand)
			if err := validatePrivateTaskPrecreateCandidate(cand, plan); err == nil {
				t.Fatal("a retry pairing that diverges from the frozen assignment was accepted")
			}
		})
	}
}

// The candidate's frozen mount state is helper-boundary input like any other:
// the two setup projections must arrive shaped exactly as the spine tables
// would have produced them.
func TestValidatePrivateMountStateSetupProjections(t *testing.T) {
	valid := func() PrivateDispatchMountState {
		return PrivateDispatchMountState{
			SelectedWorksource: "repo",
			Worksources: []PrivateDispatchWorksource{{
				WorksourceID: "repo", Kind: "repo", Status: "active",
				ArtifactRoots: []string{}, ReadonlyMounts: []string{}, DeniedPaths: []string{},
			}},
			SubjectTaskSetupRoots: []PrivateDispatchTaskSetupIdentity{},
			SubjectTaskAssignment: &PrivateDispatchTaskAssignment{
				BaseSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64),
				LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9", ObjectFormat: "sha1",
			},
			SubjectTaskTargetRef: "main",
		}
	}
	if err := validatePrivateMountState(valid()); err != nil {
		t.Fatalf("valid setup projections rejected: %v", err)
	}
	cases := map[string]func(*PrivateDispatchMountState){
		"assignment_bad_format": func(s *PrivateDispatchMountState) { s.SubjectTaskAssignment.ObjectFormat = "sha512" },
		"assignment_short_sha":  func(s *PrivateDispatchMountState) { s.SubjectTaskAssignment.BaseSHA = strings.Repeat("a", 39) },
		"assignment_bad_uuid":   func(s *PrivateDispatchMountState) { s.SubjectTaskAssignment.LocalRepoUUID = "nope" },
		"assignment_upper_digest": func(s *PrivateDispatchMountState) {
			s.SubjectTaskAssignment.ClosureDigest = strings.Repeat("B", 64)
		},
		"target_ref_control_bytes": func(s *PrivateDispatchMountState) { s.SubjectTaskTargetRef = "main\x00" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			state := valid()
			mutate(&state)
			if err := validatePrivateMountState(state); err == nil {
				t.Fatal("a malformed setup projection was accepted")
			}
		})
	}
}

// The commit-side mount-state drift fence (the reflect.DeepEqual reload in
// dispatchCommit) — the token structurally cannot catch profile drift because
// it is rebuilt from the PREPARED state, so this fence alone stands between a
// stale mount authorization and a claim (takeover-review finding, 2026-07-16).
func TestPrivateDispatchCommitStalesOnMountStateDrift(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	var prepared PrivateDispatchPrepareResponse
	if err := inTx(db, func(ctx context.Context, q Q) error {
		var err error
		prepared, err = DispatchPreparePrivate(ctx, q, privateRequest(dfUUID(t, db)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if prepared.Kind != "candidate" {
		t.Fatalf("prepare = %q, want a candidate", prepared.Kind)
	}
	// A concurrent operator edit to the selected profile between prepare and
	// commit: the reloaded projection must stale the prepared candidate.
	dvExec(t, db, `UPDATE sandbox_profiles SET readonly_mounts='["/srv/added-after-prepare"]' WHERE id='default'`)

	commit, err := DispatchAttestPrivate(os.Getenv("MC_HOME"), prepared)
	if err != nil {
		t.Fatal(err)
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
	if effect["action"] != "refused" || effect["code"] != refusal.CodeStale || effect["consequence"] != "none" {
		t.Fatalf("mount-state drift effect = %v, want inert preflight.stale", effect)
	}
	if got := dfInt(t, db, `SELECT COUNT(*) FROM runs`); got != 0 {
		t.Fatalf("mount-state drift opened %d runs", got)
	}
}

// ADR-016 D5's before-commit repeat, end to end through the private frames: a
// host chmod on an authorized source between attest and commit changes the
// plan's mode evidence, so the recheck's canonical attestation differs and
// the candidate stales — no claim from drifted host authority.
func TestPrivateDispatchRecheckStalesOnMountEvidenceDrift(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(1, dispatch.ScopeTask, dispatch.StatusProposed, 2))
	dvExec(t, db, `UPDATE worksources SET kind='personal' WHERE id='ws-test'`)
	root := t.TempDir()
	reference := maMkdir(t, root, "reference")
	dvExec(t, db, `UPDATE sandbox_profiles SET readonly_mounts=? WHERE id='default'`, fmt.Sprintf(`[%q]`, reference))
	home := os.Getenv("MC_HOME")
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	allowlist := fmt.Sprintf("version = 1\n\n[[allow]]\npath = %q\ntarget = \"reference\"\naccess = \"ro\"\n", reference)
	if err := os.WriteFile(filepath.Join(home, "mount-allowlist"), []byte(allowlist), 0o600); err != nil {
		t.Fatal(err)
	}
	maStubSnapshot(t, root, "ws-test")

	var prepared PrivateDispatchPrepareResponse
	if err := inTx(db, func(ctx context.Context, q Q) error {
		var err error
		prepared, err = DispatchPreparePrivate(ctx, q, privateRequest(dfUUID(t, db)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	commit, err := DispatchAttestPrivate(home, prepared)
	if err != nil {
		t.Fatal(err)
	}
	plan := commit.Attestation.MountPlan
	if plan == nil || len(plan.Entries) != 1 || plan.Entries[0].Mode != 0o700 {
		t.Fatalf("attested plan = %+v, want the reference entry with its mode evidence", plan)
	}

	// Host drift inside the released-lock window: same path, same inode, new
	// mode grant. Docker inspect could never see this; the evidence does.
	if err := os.Chmod(reference, 0o755); err != nil {
		t.Fatal(err)
	}
	commit = DispatchRecheckPrivate(home, prepared, commit)
	if commit.Attestation.Refusal == nil || commit.Attestation.Refusal.Code != refusal.CodeStale {
		t.Fatalf("evidence-drift recheck = %+v, want stale refusal", commit.Attestation)
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
		t.Fatalf("evidence-drift effect = %v", effect)
	}
	if got := dfInt(t, db, `SELECT COUNT(*) FROM runs`); got != 0 {
		t.Fatalf("evidence drift opened %d runs", got)
	}
}
