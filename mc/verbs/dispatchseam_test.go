package verbs

// ADR-016 D1/D2 — the dispatch seam's pure primitives. The canonical prepare
// projection, the preparation token, and the commit-side dispatch_key are a
// frozen cross-harness wire contract exactly like homieCandidateState: the
// golden vectors below pin the bytes, and the derivation tests pin the
// domain-separated hashing with the separators written as literals (never the
// production constants), so a drifted separator or field order fails here
// before it orphans a real deployment's replay fences.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mc/dispatch"
)

func i64p(v int64) *int64   { return &v }
func strp(s string) *string { return &s }

// dsGoldenPrepare is a fully-populated canonical prepare projection: one task
// with every nullable field set, one with every nullable field null, a held
// lock, one Homie candidate state, and a spawn candidate. Values are synthetic
// — the vector freezes the wire shape, not a reachable spine state.
func dsGoldenPrepare() canonicalPrepare {
	return canonicalPrepare{
		Version:             1,
		ReleaseBuildID:      "development",
		ControlVersion:      1,
		SpineSchemaVersion:  4,
		ConfigSchemaVersion: 1,
		DeploymentUUID:      "de41cafe-0000-4000-8000-000000000001",
		RequestID:           "00112233445566ff",
		Tasks: []canonicalTask{
			{
				ID: 7, Title: "fix the gate", Scope: "task", InitiativeID: i64p(3),
				Priority: 1, CreatedAt: "2026-01-02 03:04:05", Status: "seeded",
				Blocked: false, PlanReviewed: true, DispatchRetries: 2,
				Decision: "approved", DecidedAt: strp("2026-01-02 04:05:06"),
				Archived: false, Worksource: "ws-test", WorksourceStatus: "active",
				Branch: "mc/task-7", VerifiedSHA: "abc123", TargetRef: "refs/heads/main",
			},
			{
				ID: 9, Title: "t", Scope: "task", InitiativeID: nil,
				Priority: 0, CreatedAt: "2026-01-03 00:00:00", Status: "backlog",
				Blocked: true, PlanReviewed: false, DispatchRetries: 0,
				Decision: "", DecidedAt: nil,
				Archived: false, Worksource: "ws-test", WorksourceStatus: "active",
				Branch: "", VerifiedSHA: "", TargetRef: "",
			},
		},
		Packets: []canonicalPacket{
			{TaskID: 7, CreatedAt: "2026-01-02 05:00:00", Saturated: false, Archived: false},
		},
		LastBriefingAt: strp("2026-01-01 00:00:00"),
		Lock: canonicalLock{
			Held: true, RunID: "aaaabbbbccccdddd", Owner: "worker", SubjectID: i64p(7),
			AcquiredAt: strp("2026-01-02 06:00:00"), LastHeartbeatAt: nil,
			HardDeadlineAt: strp("2026-01-02 08:00:00"),
		},
		Tunables: canonicalTunables{
			TimeoutMinutes: 120, GraceMinutes: 30, HeartbeatIntervalS: 30,
			SpawnGraceS: 300, HardDeadlineMinutes: 480,
			ConsoleHour: 9, ConsoleMinute: 30, ConsoleTZ: "America/Los_Angeles",
		},
		Homies: []homieCandidateState{
			{
				SessionID: "sess-1", LaunchID: strp("aaaaaaaaaaaaaaaa"),
				LaunchMode: strp("rows"), PrimeSeq: i64p(7), PrimeCount: i64p(3),
				ResumeOwed: 0, ResumeMode: nil, ResumeSeq: nil, ResumeCount: nil,
				Binding: "claude", InputSeq: i64p(4),
			},
		},
		MountState: PrivateDispatchMountState{
			SelectedWorksource: "ws-test",
			Worksources: []PrivateDispatchWorksource{{
				WorksourceID: "ws-test", Kind: "repo", Status: "active",
				ProfilePresent: true, ProfileID: "default", WorkspaceRoot: "/srv/ws-test",
				ArtifactRoots: []string{"/srv/artifacts"}, ReadonlyMounts: []string{"/srv/reference"},
				DeniedPaths: []string{"/srv/ws-test/private"}, ToolHomeDir: "/srv/tool-home",
				RuntimeControlDir: "/srv/runtime-control",
			}},
		},
		Candidate: canonicalCandidate{
			Kind: "spawn", RunID: "0123456789abcdef", Role: "worker", SubjectID: i64p(7),
			ProposedPool: []int64{}, Wave: []int64{}, DedupeTitles: []string{},
		},
	}
}

// The frozen canonical bytes of dsGoldenPrepare. Assembled from per-struct
// segments for reviewability; the assertion is on the joined whole.
func dsGoldenPrepareJSON() string {
	return strings.Join([]string{
		`{"version":1`,
		`"release_build_id":"development"`,
		`"gateway_control_version":1`,
		`"spine_schema_version":4`,
		`"config_schema_version":1`,
		`"deployment_uuid":"de41cafe-0000-4000-8000-000000000001"`,
		`"request_id":"00112233445566ff"`,
		`"tasks":[` +
			`{"id":7,"title":"fix the gate","scope":"task","initiative_id":3,` +
			`"priority":1,"created_at":"2026-01-02 03:04:05","status":"seeded",` +
			`"blocked":false,"plan_reviewed":true,"dispatch_retries":2,` +
			`"decision":"approved","decided_at":"2026-01-02 04:05:06",` +
			`"archived":false,"worksource":"ws-test","worksource_status":"active",` +
			`"branch":"mc/task-7","verified_sha":"abc123","target_ref":"refs/heads/main"},` +
			`{"id":9,"title":"t","scope":"task","initiative_id":null,` +
			`"priority":0,"created_at":"2026-01-03 00:00:00","status":"backlog",` +
			`"blocked":true,"plan_reviewed":false,"dispatch_retries":0,` +
			`"decision":"","decided_at":null,` +
			`"archived":false,"worksource":"ws-test","worksource_status":"active",` +
			`"branch":"","verified_sha":"","target_ref":""}]`,
		`"packets":[{"task_id":7,"created_at":"2026-01-02 05:00:00","saturated":false,"archived":false}]`,
		`"last_briefing_at":"2026-01-01 00:00:00"`,
		`"lock":{"held":true,"run_id":"aaaabbbbccccdddd","owner":"worker","subject_id":7,` +
			`"acquired_at":"2026-01-02 06:00:00","last_heartbeat_at":null,` +
			`"hard_deadline_at":"2026-01-02 08:00:00"}`,
		`"tunables":{"timeout_minutes":120,"grace_minutes":30,"heartbeat_interval_s":30,` +
			`"spawn_grace_s":300,"hard_deadline_minutes":480,` +
			`"console_hour":9,"console_minute":30,"console_tz":"America/Los_Angeles"}`,
		`"homies":[{"session_id":"sess-1","launch_id":"aaaaaaaaaaaaaaaa","launch_mode":"rows",` +
			`"prime_through_seq":7,"prime_row_count":3,"resume_owed":0,"resume_mode":null,` +
			`"resume_prime_through_seq":null,"resume_prime_row_count":null,` +
			`"binding":"claude","input_seq":4}]`,
		`"mount_state":{"selected_worksource":"ws-test","subject_initiative_id":null,"worksources":[{` +
			`"worksource_id":"ws-test","kind":"repo","status":"active",` +
			`"profile_present":true,"profile_id":"default","workspace_root":"/srv/ws-test",` +
			`"artifact_roots":["/srv/artifacts"],"readonly_mounts":["/srv/reference"],` +
			`"denied_paths":["/srv/ws-test/private"],"tool_home_dir":"/srv/tool-home",` +
			`"runtime_control_dir":"/srv/runtime-control"}]}`,
		`"candidate":{"kind":"spawn","run_id":"0123456789abcdef","role":"worker",` +
			`"subject_id":7,"proposed_pool":[],"wave":[],"dedupe_titles":[]}}`,
	}, ",")
}

func TestDispatchSeamCanonicalPrepareGoldenVector(t *testing.T) {
	got, err := dsGoldenPrepare().bytes()
	if err != nil {
		t.Fatalf("canonical bytes: %v", err)
	}
	if string(got) != dsGoldenPrepareJSON() {
		t.Fatalf("canonical prepare bytes drifted from the frozen vector\n got: %s\nwant: %s", got, dsGoldenPrepareJSON())
	}
}

// The preparation token is SHA-256 over "MC-DISPATCH-PREPARE-V1\x00" plus the
// canonical bytes (ADR-016:130-131), hex-encoded. The separator is written as
// a literal here on purpose.
func TestDispatchSeamPreparationTokenDerivation(t *testing.T) {
	canonical := []byte(dsGoldenPrepareJSON())
	sum := sha256.Sum256(append([]byte("MC-DISPATCH-PREPARE-V1\x00"), canonical...))
	want := hex.EncodeToString(sum[:])
	if got := preparationToken(canonical); got != want {
		t.Fatalf("preparationToken = %q, want %q", got, want)
	}
	if !validDispatchKey(want) {
		t.Fatalf("token %q is not 64 lowercase hex", want)
	}
}

// dispatch_key = SHA256("MC-DISPATCH-ACTION-V1\x00" || preparation_token ||
// canonical_action) (ADR-016:238-240). The token participates in its hex
// ASCII form — the frozen convention, pinned here.
func TestDispatchSeamDispatchKeyDerivation(t *testing.T) {
	spawnAction := canonicalAction{
		Version: 1, RequestID: "00112233445566ff", Consequence: "spawn",
		RunID: "0123456789abcdef", Role: "worker", SubjectID: i64p(7),
		RoutingDigest: strings.Repeat("ab", 32), Harness: "claude-sdk", Binding: "minimax",
	}
	wantSpawnJSON := `{"version":1,"request_id":"00112233445566ff","consequence":"spawn",` +
		`"run_id":"0123456789abcdef","role":"worker","subject_id":7,` +
		`"routing_digest":"` + strings.Repeat("ab", 32) + `",` +
		`"harness":"claude-sdk","binding":"minimax","plan_digest":"","refusal":null}`
	gotJSON, err := spawnAction.bytes()
	if err != nil {
		t.Fatalf("canonical action bytes: %v", err)
	}
	if string(gotJSON) != wantSpawnJSON {
		t.Fatalf("canonical action bytes drifted\n got: %s\nwant: %s", gotJSON, wantSpawnJSON)
	}

	token := preparationToken([]byte(dsGoldenPrepareJSON()))
	sum := sha256.Sum256(append(append([]byte("MC-DISPATCH-ACTION-V1\x00"), []byte(token)...), gotJSON...))
	want := hex.EncodeToString(sum[:])
	got, err := deriveDispatchKey(token, spawnAction)
	if err != nil {
		t.Fatalf("deriveDispatchKey: %v", err)
	}
	if got != want {
		t.Fatalf("deriveDispatchKey = %q, want %q", got, want)
	}
	if !validDispatchKey(got) {
		t.Fatalf("dispatch key %q is not 64 lowercase hex", got)
	}
}

// plan_digest = SHA256("MC-DISPATCH-PLAN-V1\x00" || canonical_plan), hex —
// the canonical plan bytes are pinned here because the effect's D2 replay
// path round-trips maps alphabetically, so the declared field order must be
// alphabetical and must never drift.
func TestDispatchSeamMountPlanDigestDerivation(t *testing.T) {
	plan := &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{{
		Access: "rw", Destination: "/workspace/artifacts/art", Device: "42", Inode: "7",
		Kind: "dir", LogicalID: "artifact:art", Mode: 448, OwnerUID: 501, Source: "/srv/artifact",
	}}}
	wantJSON := `{"entries":[{"access":"rw","destination":"/workspace/artifacts/art",` +
		`"device":"42","inode":"7","kind":"dir","logical_id":"artifact:art",` +
		`"mode":448,"owner_uid":501,"source":"/srv/artifact"}],"version":1}`
	gotJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("canonical plan bytes: %v", err)
	}
	if string(gotJSON) != wantJSON {
		t.Fatalf("canonical plan bytes drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
	sum := sha256.Sum256(append([]byte("MC-DISPATCH-PLAN-V1\x00"), gotJSON...))
	want := hex.EncodeToString(sum[:])
	got, err := mountPlanDigest(plan)
	if err != nil {
		t.Fatalf("mountPlanDigest: %v", err)
	}
	if got != want {
		t.Fatalf("mountPlanDigest = %q, want %q", got, want)
	}
	if empty, err := mountPlanDigest(nil); err != nil || empty != "" {
		t.Fatalf("nil-plan digest = (%q, %v), want the explicit empty string", empty, err)
	}
}

func TestDispatchSeamCanonicalActionRefusalVariant(t *testing.T) {
	idx := 2
	action := canonicalAction{
		Version: 1, RequestID: "00112233445566ff", Consequence: "refusal",
		RunID: "0123456789abcdef", Role: "worker", SubjectID: nil,
		Refusal: &canonicalRefusal{
			Code: "health.routing_invalid", Authority: "", Field: "routing",
			Summary: "unparsable", ItemIndex: &idx,
		},
	}
	want := `{"version":1,"request_id":"00112233445566ff","consequence":"refusal",` +
		`"run_id":"0123456789abcdef","role":"worker","subject_id":null,` +
		`"routing_digest":"","harness":"","binding":"","plan_digest":"",` +
		`"refusal":{"code":"health.routing_invalid","authority":"","field":"routing",` +
		`"summary":"unparsable","item_index":2}}`
	got, err := action.bytes()
	if err != nil {
		t.Fatalf("canonical action bytes: %v", err)
	}
	if string(got) != want {
		t.Fatalf("refusal action bytes drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestDispatchSeamRequestIDShape(t *testing.T) {
	a, err := newDispatchRequestID()
	if err != nil {
		t.Fatalf("newDispatchRequestID: %v", err)
	}
	b, err := newDispatchRequestID()
	if err != nil {
		t.Fatalf("newDispatchRequestID: %v", err)
	}
	if !validLowercaseHex(a, 16) || !validLowercaseHex(b, 16) {
		t.Fatalf("request ids %q, %q are not 16 lowercase hex", a, b)
	}
	if a == b {
		t.Fatalf("two request ids collided: %q", a)
	}
}

// buildCanonicalPrepare must sort tasks by id and packets by (task_id,
// created_at) — records arrive in SQL order, which is not part of the wire
// contract — and normalize nil slices to empty ones so the encoding never
// distinguishes "absent" from "empty".
func TestDispatchSeamBuildCanonicalPrepareNormalizes(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rec := dispatch.Records{
		Tasks: []dispatch.Task{
			{ID: 9, Title: "b", Scope: "task", CreatedAt: created, Status: "backlog", Worksource: "ws-test", WorksourceStatus: "active"},
			{ID: 7, Title: "a", Scope: "task", CreatedAt: created, Status: "seeded", Worksource: "ws-test", WorksourceStatus: "active"},
		},
		Packets: []dispatch.Packet{
			{TaskID: 9, CreatedAt: created.Add(time.Hour)},
			{TaskID: 9, CreatedAt: created},
			{TaskID: 7, CreatedAt: created},
		},
	}
	sp := &dispatch.Spawn{Role: dispatch.Role("worker")}
	got := buildCanonicalPrepare("uuid-x", "00112233445566ff", rec, dispatch.Lock{}, tunables{}, nil, PrivateDispatchMountState{}, spawnCandidateProjection("0123456789abcdef", sp))
	if got.Tasks[0].ID != 7 || got.Tasks[1].ID != 9 {
		t.Fatalf("tasks not sorted by id: %+v", got.Tasks)
	}
	if got.Packets[0].TaskID != 7 || got.Packets[1].TaskID != 9 || got.Packets[1].CreatedAt >= got.Packets[2].CreatedAt {
		t.Fatalf("packets not sorted by (task_id, created_at): %+v", got.Packets)
	}
	if got.Homies == nil {
		t.Fatalf("nil homies must normalize to an empty slice")
	}
	if got.Candidate.ProposedPool == nil || got.Candidate.Wave == nil || got.Candidate.DedupeTitles == nil {
		t.Fatalf("nil candidate slices must normalize to empty slices: %+v", got.Candidate)
	}
	if got.Lock.Held || got.Lock.SubjectID != nil || got.Lock.AcquiredAt != nil {
		t.Fatalf("free lock must project as unheld with null times: %+v", got.Lock)
	}
	b1, err := got.bytes()
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	b2, err := buildCanonicalPrepare("uuid-x", "00112233445566ff", rec, dispatch.Lock{}, tunables{}, nil, PrivateDispatchMountState{}, spawnCandidateProjection("0123456789abcdef", sp)).bytes()
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	if string(b1) != string(b2) {
		t.Fatalf("canonical bytes are not deterministic:\n%s\n%s", b1, b2)
	}
}

// loadHomieProjection is the launch-generation observation the D4 fence
// compares (ADR-016 D3): every active session's frozen homieCandidateState,
// ordered by session id. It must agree byte-for-byte with the single-session
// homiePrePrepareState — the marker-key comparison in the future selector
// depends on the two never drifting.
func TestDispatchSeamHomieProjection(t *testing.T) {
	db := dvSpine(t)
	dvExec(t, db, `
		INSERT INTO homie_sessions (id, container_name, verb_allowlist, session_path, binding)
		VALUES ('h-bbbbbbbbbbbbbbbb', 'mc-homie-h-bbbbbbbbbbbbbbbb', '[]', 'sessions/h-b', 'claude')`)
	dvExec(t, db, `
		INSERT INTO homie_sessions (id, container_name, verb_allowlist, session_path, binding)
		VALUES ('h-aaaaaaaaaaaaaaaa', 'mc-homie-h-aaaaaaaaaaaaaaaa', '[]', 'sessions/h-a', 'claude')`)
	dvExec(t, db, `
		UPDATE homie_sessions
		SET current_launch_id = 'aaaaaaaaaaaaaaaa', current_launch_mode = 'rows',
		    current_prime_through_seq = 7, current_prime_row_count = 3
		WHERE id = 'h-aaaaaaaaaaaaaaaa'`)
	dvExec(t, db, `
		INSERT INTO homie_sessions (id, container_name, verb_allowlist, session_path, binding, status)
		VALUES ('h-cccccccccccccccc', 'mc-homie-h-cccccccccccccccc', '[]', 'sessions/h-c', 'claude', 'ended')`)

	var got []homieCandidateState
	err := inTx(db, func(ctx context.Context, q Q) error {
		var e error
		got, e = loadHomieProjection(ctx, q)
		return e
	})
	if err != nil {
		t.Fatalf("loadHomieProjection: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("projection covers active sessions only, got %d entries: %+v", len(got), got)
	}
	if got[0].SessionID != "h-aaaaaaaaaaaaaaaa" || got[1].SessionID != "h-bbbbbbbbbbbbbbbb" {
		t.Fatalf("projection not ordered by session id: %q, %q", got[0].SessionID, got[1].SessionID)
	}
	if got[0].LaunchID == nil || *got[0].LaunchID != "aaaaaaaaaaaaaaaa" || got[0].LaunchMode == nil || *got[0].LaunchMode != "rows" {
		t.Fatalf("launch generation not observed: %+v", got[0])
	}
	if got[1].LaunchID != nil {
		t.Fatalf("no-launch session must project a null launch id: %+v", got[1])
	}

	// Drift guard: the projection row equals homiePrePrepareState's.
	for _, want := range got {
		var single homieCandidateState
		err := inTx(db, func(ctx context.Context, q Q) error {
			var e error
			single, _, e = homiePrePrepareState(ctx, q, want.SessionID)
			return e
		})
		if err != nil {
			t.Fatalf("homiePrePrepareState(%q): %v", want.SessionID, err)
		}
		pb, err := single.key()
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		wb, err := want.key()
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		if pb != wb {
			t.Fatalf("projection drifted from homiePrePrepareState for %q:\n%+v\n%+v", want.SessionID, want, single)
		}
	}
}
