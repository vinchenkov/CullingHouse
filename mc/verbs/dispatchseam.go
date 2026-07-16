package verbs

// ADR-016 D1/D2 — the dispatch seam's pure primitives: the canonical prepare
// projection, the preparation token, and the commit-side dispatch_key.
//
// The canonical structs below are a frozen cross-harness wire contract, the
// same discipline as homieCandidateState: json.Marshal over closed structs in
// declared field order, UTF-8 strings, decimal integers, explicit zero
// values, pointers for null, no maps or floats (ADR-016:151-156). Semantically
// unordered collections are sorted by their declared key before encoding;
// nil slices normalize to empty ones so "absent" and "empty" never encode
// differently. Times are spine strings ("2006-01-02 15:04:05"). Changing any
// shape here is an ADR-worthy event — the golden vectors in
// dispatchseam_test.go exist to make that impossible to do by accident.
//
// Wall-clock time is deliberately NOT part of the projection: commit
// re-decides with its own fresh clock, so second-granularity drift between
// the two transactions can change the selected action (caught by the
// re-decide equality check) but never falsely stales byte-identical state.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"mc/dispatch"
)

// Domain separators, exactly ADR-016:130-131 and :238-240. The preparation
// token participates in dispatch_key derivation in its hex ASCII form.
const (
	prepareTokenDomain = "MC-DISPATCH-PREPARE-V1\x00"
	dispatchKeyDomain  = "MC-DISPATCH-ACTION-V1\x00"
)

type canonicalTask struct {
	ID               int64   `json:"id"`
	Title            string  `json:"title"`
	Scope            string  `json:"scope"`
	InitiativeID     *int64  `json:"initiative_id"`
	Priority         int     `json:"priority"`
	CreatedAt        string  `json:"created_at"`
	Status           string  `json:"status"`
	Blocked          bool    `json:"blocked"`
	PlanReviewed     bool    `json:"plan_reviewed"`
	DispatchRetries  int     `json:"dispatch_retries"`
	Decision         string  `json:"decision"`
	DecidedAt        *string `json:"decided_at"`
	Archived         bool    `json:"archived"`
	Worksource       string  `json:"worksource"`
	WorksourceStatus string  `json:"worksource_status"`
	Branch           string  `json:"branch"`
	VerifiedSHA      string  `json:"verified_sha"`
	TargetRef        string  `json:"target_ref"`
}

type canonicalPacket struct {
	TaskID    int64  `json:"task_id"`
	CreatedAt string `json:"created_at"`
	Saturated bool   `json:"saturated"`
	Archived  bool   `json:"archived"`
}

type canonicalLock struct {
	Held            bool    `json:"held"`
	RunID           string  `json:"run_id"`
	Owner           string  `json:"owner"`
	SubjectID       *int64  `json:"subject_id"`
	AcquiredAt      *string `json:"acquired_at"`
	LastHeartbeatAt *string `json:"last_heartbeat_at"`
	HardDeadlineAt  *string `json:"hard_deadline_at"`
}

type canonicalTunables struct {
	TimeoutMinutes      int    `json:"timeout_minutes"`
	GraceMinutes        int    `json:"grace_minutes"`
	HeartbeatIntervalS  int    `json:"heartbeat_interval_s"`
	SpawnGraceS         int    `json:"spawn_grace_s"`
	HardDeadlineMinutes int    `json:"hard_deadline_minutes"`
	ConsoleHour         int    `json:"console_hour"`
	ConsoleMinute       int    `json:"console_minute"`
	ConsoleTZ           string `json:"console_tz"`
}

// canonicalCandidate is the bounded logical candidate a prepare returns for
// the steps that need native authority (ADR-016 D1 step 1). The run id is
// allocated at prepare and becomes canonical only at commit (ADR-016:121-124).
type canonicalCandidate struct {
	Kind         string   `json:"kind"`
	RunID        string   `json:"run_id"`
	Role         string   `json:"role"`
	SubjectID    *int64   `json:"subject_id"`
	ProposedPool []int64  `json:"proposed_pool"`
	Wave         []int64  `json:"wave"`
	DedupeTitles []string `json:"dedupe_titles"`
}

// canonicalPrepare is D2's closed prepare projection: everything selection
// read, plus the candidate it selected. The Homie entries are the launch
// generations the D4 fence compares, in the same frozen homieCandidateState
// shape the preflight marker key uses.
type canonicalPrepare struct {
	Version        int                   `json:"version"`
	DeploymentUUID string                `json:"deployment_uuid"`
	RequestID      string                `json:"request_id"`
	Tasks          []canonicalTask       `json:"tasks"`
	Packets        []canonicalPacket     `json:"packets"`
	LastBriefingAt *string               `json:"last_briefing_at"`
	Lock           canonicalLock         `json:"lock"`
	Tunables       canonicalTunables     `json:"tunables"`
	Homies         []homieCandidateState `json:"homies"`
	Candidate      canonicalCandidate    `json:"candidate"`
}

func (p canonicalPrepare) bytes() ([]byte, error) {
	return json.Marshal(p)
}

// canonicalRefusal carries only mc/refusal's closed enums — never Message,
// which is hostile by D4's definition.
type canonicalRefusal struct {
	Code      string `json:"code"`
	Authority string `json:"authority"`
	Field     string `json:"field"`
	Summary   string `json:"summary"`
	ItemIndex *int   `json:"item_index"`
}

// canonicalAction is the commit-side action encoding under dispatch_key. It
// binds the attested host projection (today: the routing.md digest and the
// resolved route) to the prepared candidate and its consequence.
type canonicalAction struct {
	Version       int               `json:"version"`
	RequestID     string            `json:"request_id"`
	Consequence   string            `json:"consequence"` // "spawn" | "refusal"
	RunID         string            `json:"run_id"`
	Role          string            `json:"role"`
	SubjectID     *int64            `json:"subject_id"`
	RoutingDigest string            `json:"routing_digest"`
	Harness       string            `json:"harness"`
	Binding       string            `json:"binding"`
	Refusal       *canonicalRefusal `json:"refusal"`
}

func (a canonicalAction) bytes() ([]byte, error) {
	return json.Marshal(a)
}

// preparationToken derives ADR-016 D2's prepare token: SHA-256 over the
// domain-separated canonical prepare bytes, hex-encoded.
func preparationToken(canonical []byte) string {
	h := sha256.New()
	h.Write([]byte(prepareTokenDomain))
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}

// deriveDispatchKey computes the commit-side idempotency fence
// (ADR-016:238-240). The helper computes it at commit from bytes it
// reconstructs itself; it is never a caller-supplied hash.
func deriveDispatchKey(token string, action canonicalAction) (string, error) {
	body, err := action.bytes()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(dispatchKeyDomain))
	h.Write([]byte(token))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// newDispatchRequestID allocates D2's command-scoped request id: exactly 16
// lowercase hex, minted once per external command and reused across transport
// retries of that command (ADR-016:107-110).
func newDispatchRequestID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", Usagef("allocate dispatch request id: %v", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func spineTimeString(t time.Time) string {
	return t.Format(spineTime)
}

func spineTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := spineTimeString(*t)
	return &s
}

// spawnCandidateProjection projects the decided spawn into the bounded
// candidate frame, normalizing nil slices.
func spawnCandidateProjection(runID string, sp *dispatch.Spawn) canonicalCandidate {
	c := canonicalCandidate{
		Kind:         "spawn",
		RunID:        runID,
		Role:         string(sp.Role),
		SubjectID:    sp.SubjectID,
		ProposedPool: sp.ProposedPool,
		Wave:         sp.Wave,
		DedupeTitles: sp.DedupeTitles,
	}
	if c.ProposedPool == nil {
		c.ProposedPool = []int64{}
	}
	if c.Wave == nil {
		c.Wave = []int64{}
	}
	if c.DedupeTitles == nil {
		c.DedupeTitles = []string{}
	}
	return c
}

// buildCanonicalPrepare assembles the closed projection from what the prepare
// transaction read. Records arrive in SQL order, which is not part of the
// wire contract, so tasks sort by id and packets by (task_id, created_at).
func buildCanonicalPrepare(uuid, requestID string, rec dispatch.Records, lk dispatch.Lock, tun tunables, homies []homieCandidateState, cand canonicalCandidate) canonicalPrepare {
	tasks := make([]canonicalTask, 0, len(rec.Tasks))
	for _, t := range rec.Tasks {
		tasks = append(tasks, canonicalTask{
			ID:               t.ID,
			Title:            t.Title,
			Scope:            string(t.Scope),
			InitiativeID:     t.InitiativeID,
			Priority:         t.Priority,
			CreatedAt:        spineTimeString(t.CreatedAt),
			Status:           string(t.Status),
			Blocked:          t.Blocked,
			PlanReviewed:     t.PlanReviewed,
			DispatchRetries:  t.DispatchRetries,
			Decision:         string(t.Decision),
			DecidedAt:        spineTimePtr(t.DecidedAt),
			Archived:         t.Archived,
			Worksource:       t.Worksource,
			WorksourceStatus: t.WorksourceStatus,
			Branch:           t.Branch,
			VerifiedSHA:      t.VerifiedSHA,
			TargetRef:        t.TargetRef,
		})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })

	packets := make([]canonicalPacket, 0, len(rec.Packets))
	for _, p := range rec.Packets {
		packets = append(packets, canonicalPacket{
			TaskID:    p.TaskID,
			CreatedAt: spineTimeString(p.CreatedAt),
			Saturated: p.Saturated,
			Archived:  p.Archived,
		})
	}
	sort.Slice(packets, func(i, j int) bool {
		if packets[i].TaskID != packets[j].TaskID {
			return packets[i].TaskID < packets[j].TaskID
		}
		return packets[i].CreatedAt < packets[j].CreatedAt
	})

	lock := canonicalLock{Held: lk.Held}
	if lk.Held {
		lock.RunID = lk.RunID
		lock.Owner = lk.Owner
		lock.SubjectID = lk.SubjectID
		acquired := spineTimeString(lk.AcquiredAt)
		lock.AcquiredAt = &acquired
		lock.LastHeartbeatAt = spineTimePtr(lk.LastHeartbeatAt)
		deadline := spineTimeString(lk.HardDeadlineAt)
		lock.HardDeadlineAt = &deadline
	}

	if homies == nil {
		homies = []homieCandidateState{}
	}

	return canonicalPrepare{
		Version:        1,
		DeploymentUUID: uuid,
		RequestID:      requestID,
		Tasks:          tasks,
		Packets:        packets,
		LastBriefingAt: spineTimePtr(rec.LastBriefingAt),
		Lock:           lock,
		Tunables: canonicalTunables{
			TimeoutMinutes:      tun.timeoutMinutes,
			GraceMinutes:        tun.graceMinutes,
			HeartbeatIntervalS:  tun.heartbeatIntervalS,
			SpawnGraceS:         tun.spawnGraceS,
			HardDeadlineMinutes: tun.hardDeadlineMinutes,
			ConsoleHour:         tun.consoleHour,
			ConsoleMinute:       tun.consoleMinute,
			ConsoleTZ:           tun.consoleTZ,
		},
		Homies:    homies,
		Candidate: cand,
	}
}

// loadHomieProjection reads every active session's frozen candidate state,
// ordered by session id — the launch-generation observation (ADR-016 D3) the
// prepare projection binds and the D4 Homie-end fence later compares. The
// column list and InputSeq subquery must stay semantically identical to
// homiePrePrepareState's; TestDispatchSeamHomieProjection holds the two
// together by key equality.
func loadHomieProjection(ctx context.Context, q Q) ([]homieCandidateState, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.id, s.binding,
		       s.current_launch_id, s.current_launch_mode,
		       s.current_prime_through_seq, s.current_prime_row_count,
		       s.resume_owed, s.resume_mode,
		       s.resume_prime_through_seq, s.resume_prime_row_count,
		       (SELECT MAX(m.seq) FROM conversation_messages m
		        WHERE m.session_id = s.id
		          AND m.direction = 'inbound' AND m.completed_at IS NULL)
		FROM homie_sessions s WHERE s.status = 'active' ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []homieCandidateState{}
	for rows.Next() {
		var st homieCandidateState
		var launchID, launchMode, resumeMode sql.NullString
		var primeSeq, primeCount, resumeSeq, resumeCount, inputSeq sql.NullInt64
		if err := rows.Scan(
			&st.SessionID, &st.Binding,
			&launchID, &launchMode, &primeSeq, &primeCount,
			&st.ResumeOwed, &resumeMode, &resumeSeq, &resumeCount,
			&inputSeq,
		); err != nil {
			return nil, err
		}
		st.LaunchID = nullStringPtr(launchID)
		st.LaunchMode = nullStringPtr(launchMode)
		st.PrimeSeq = nullInt64Ptr(primeSeq)
		st.PrimeCount = nullInt64Ptr(primeCount)
		st.ResumeMode = nullStringPtr(resumeMode)
		st.ResumeSeq = nullInt64Ptr(resumeSeq)
		st.ResumeCount = nullInt64Ptr(resumeCount)
		st.InputSeq = nullInt64Ptr(inputSeq)
		out = append(out, st)
	}
	return out, rows.Err()
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
