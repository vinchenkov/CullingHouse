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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"mc/dispatch"
	"mc/refusal"
	"mc/routing"
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

// ---------------------------------------------------------------------------
// ADR-016 D1 — the command frame: prepare → attest → commit.
//
// One external `mc dispatch` composes three steps. Prepare runs under the
// process flock and one BEGIN IMMEDIATE transaction against spine state only;
// a branch whose entire consequence is lock-domain-owned commits there, with
// its D2 request receipt, and the command is done. A spawn instead returns a
// bounded candidate and preparation token. Attest runs with both released —
// it alone reads host files (routing.md today; the mount plan next). Commit
// reacquires, re-reads, requires the recomputed token to equal the prepared
// one byte-for-byte, re-decides, and applies exactly the reselected
// candidate's consequence — a spawn, or a classified refusal routed through
// the D4 consequence router with a dispatch_key that is finally DERIVED
// (token + canonical action) rather than taken on faith. It never falls
// through to another candidate.
//
// This is the native single-process form D1 pins for Linux ("the resident
// calls the same prepare/attest/commit functions locally in one process,
// deliberately releasing the transaction/flock across attest I/O"). The
// Darwin broker/helper self-delegation split — private __dispatch-prepare/
// __dispatch-commit CLI frames over the one-shot control descriptor — is a
// later slice over these same functions (deviation logged 2026-07-16).
// ---------------------------------------------------------------------------

// preparedDispatch is what one prepare invocation produced: either a final
// result (lock-domain consequence or receipt replay) or a candidate that owes
// attest and commit.
type preparedDispatch struct {
	requestID string
	final     map[string]any
	candidate *preparedCandidate
}

type preparedCandidate struct {
	spawn *dispatch.Spawn
	runID string
	tun   tunables
	token string
}

// attestedDispatch is the attest step's host projection: the resolved route
// and the digest binding it, or a classified refusal — never both.
type attestedDispatch struct {
	route         routing.Route
	routingDigest string
	refusal       *refusal.Refusal
}

// dispatchPrepare is the helper's first invocation (ADR-016 D1 step 1). Order
// is load-bearing: the deployment precondition before anything on every
// branch, then the receipt fence BEFORE reading selection state — a lost
// response looked up after selection could reap-then-claim in one command,
// exactly what D2 forbids (ADR-016:255-261).
func dispatchPrepare(ctx context.Context, q Q, uuid, requestID string) (preparedDispatch, error) {
	if err := requireDeploymentUUID(ctx, q, uuid); err != nil {
		return preparedDispatch{}, err
	}
	if !validLowercaseHex(requestID, 16) {
		return preparedDispatch{}, Domainf("dispatch request id must be exactly 16 lowercase hex characters (ADR-016 D2)")
	}
	if replay, found, err := lookupDispatchReceipt(ctx, q, requestID); err != nil {
		return preparedDispatch{}, err
	} else if found {
		return preparedDispatch{requestID: requestID, final: replay}, nil
	}

	sel, err := selectFromSpine(ctx, q)
	if err != nil {
		return preparedDispatch{}, err
	}

	if sel.action.Kind == dispatch.KindSpawn {
		// Spawn needs native authority; allocate the candidate identity now
		// (ADR-016:114-124 — it becomes canonical only at commit) and freeze
		// the projection under the token.
		runID, err := newRunID()
		if err != nil {
			return preparedDispatch{}, err
		}
		canonical, err := buildCanonicalPrepare(uuid, requestID, sel.rec, sel.lk, sel.tun, sel.homies,
			spawnCandidateProjection(runID, sel.action.Spawn)).bytes()
		if err != nil {
			return preparedDispatch{}, err
		}
		return preparedDispatch{requestID: requestID, candidate: &preparedCandidate{
			spawn: sel.action.Spawn,
			runID: runID,
			tun:   sel.tun,
			token: preparationToken(canonical),
		}}, nil
	}

	effect, err := applyAction(ctx, q, sel.now, sel.action, sel.tun)
	if err != nil {
		return preparedDispatch{}, err
	}
	switch sel.action.Kind {
	case dispatch.KindReap, dispatch.KindReenter:
		// Mutating lock-domain branches insert their receipt atomically with
		// the consequence. Idle and the Phase-2 land effect mutate nothing —
		// a non-mutating result needs no receipt and a lost-response retry
		// may re-evaluate once (ADR-016:261-263).
		if err := writeDispatchReceipt(ctx, q, requestID, effect); err != nil {
			return preparedDispatch{}, err
		}
	}
	return preparedDispatch{requestID: requestID, final: effect}, nil
}

// dispatchAttest is the host-authority step, run with the flock and
// transaction released. Today it attests routing.md — reading, digesting,
// parsing, and resolving the candidate's role — and classifies any failure as
// the D4 deployment-health refusal instead of erroring the command: routing
// brokenness is the deployment's fault, never the candidate task's, and the
// consequence (one dispatch.health action, no charge, no block, no claim)
// belongs to the commit transaction (phase3-contract row 174). The mount-plan
// attestation joins here once a candidate carries mount requests.
func dispatchAttest(home string, prepared preparedDispatch) (attestedDispatch, error) {
	cand := prepared.candidate
	if cand == nil {
		return attestedDispatch{}, Domainf("dispatch: attest requires a prepared candidate")
	}
	path := filepath.Join(home, "routing.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return routingRefusal(refusal.SummaryMissing, err), nil
	}
	sum := sha256.Sum256(data)
	registry, allowFakeDecorrelation := routing.ActiveRegistry()
	table, err := routing.Parse(data, registry, allowFakeDecorrelation)
	if err != nil {
		return routingRefusal(refusal.SummaryUnparsable, err), nil
	}
	route, err := table.Resolve(baseRole(string(cand.spawn.Role)))
	if err != nil {
		return routingRefusal(refusal.SummaryUnresolved, err), nil
	}
	return attestedDispatch{route: route, routingDigest: hex.EncodeToString(sum[:])}, nil
}

// routingRefusal classifies one routing failure. The raw error text rides
// only in Message, which DetailFor drops — routing.md bytes are operator
// material and never reach a stored detail (ADR-016 D1).
func routingRefusal(summary refusal.Summary, err error) attestedDispatch {
	return attestedDispatch{refusal: &refusal.Refusal{
		Code:    refusal.CodeRoutingInvalid,
		Field:   refusal.FieldRouting,
		Summary: summary,
		Message: err.Error(),
	}}
}

// dispatchCommit is the helper's second invocation (ADR-016 D1 step 3): under
// a fresh flock and transaction it reloads and re-decides lock-domain truth,
// requires the recomputed canonical projection to reproduce the preparation
// token byte-for-byte, and applies exactly the reselected candidate's
// consequence. Ordinary drift is preflight.stale; a decision that no longer
// reselects the prepared candidate is preflight.candidate_mismatch — both
// stale-class, both inert (ADR-016:266-273).
func dispatchCommit(ctx context.Context, q Q, uuid string, prepared preparedDispatch, attested attestedDispatch) (map[string]any, error) {
	cand := prepared.candidate
	if cand == nil {
		return nil, Domainf("dispatch: commit requires a prepared candidate")
	}
	if err := requireDeploymentUUID(ctx, q, uuid); err != nil {
		return nil, err
	}
	sel, err := selectFromSpine(ctx, q)
	if err != nil {
		return nil, err
	}
	rcand := refusalCandidateFor(cand.spawn)

	proj := spawnCandidateProjection(cand.runID, cand.spawn)
	canonical, err := buildCanonicalPrepare(uuid, prepared.requestID, sel.rec, sel.lk, sel.tun, sel.homies, proj).bytes()
	if err != nil {
		return nil, err
	}
	if preparationToken(canonical) != cand.token {
		return commitInertRefusal(ctx, q, prepared, rcand, refusal.CodeStale)
	}

	// Re-decide with commit's own clock. The helper computes its own truth:
	// byte-identical state must still reselect this candidate, so a doctored
	// frame or a time-flipped decision refuses here rather than committing a
	// consequence nothing selected.
	if sel.action.Kind != dispatch.KindSpawn {
		return commitInertRefusal(ctx, q, prepared, rcand, refusal.CodeCandidateMismatch)
	}
	reselected, err := json.Marshal(spawnCandidateProjection(cand.runID, sel.action.Spawn))
	if err != nil {
		return nil, err
	}
	preparedProj, err := json.Marshal(proj)
	if err != nil {
		return nil, err
	}
	if string(reselected) != string(preparedProj) {
		return commitInertRefusal(ctx, q, prepared, rcand, refusal.CodeCandidateMismatch)
	}

	if attested.refusal != nil {
		key, err := refusalDispatchKey(prepared, *attested.refusal)
		if err != nil {
			return nil, err
		}
		return applyRefusal(ctx, q, rcand, *attested.refusal, key)
	}

	action := canonicalAction{
		Version:       1,
		RequestID:     prepared.requestID,
		Consequence:   "spawn",
		RunID:         cand.runID,
		Role:          string(cand.spawn.Role),
		SubjectID:     cand.spawn.SubjectID,
		RoutingDigest: attested.routingDigest,
		Harness:       attested.route.Harness,
		Binding:       attested.route.Binding,
	}
	key, err := deriveDispatchKey(cand.token, action)
	if err != nil {
		return nil, err
	}
	effect, err := applySpawn(ctx, q, sel.now, sel.action.Spawn, sel.tun, attested.route, cand.runID)
	if err != nil {
		return nil, err
	}
	if err := writeSpawnReceipt(ctx, q, cand.runID, key); err != nil {
		return nil, err
	}
	return effect, nil
}

// commitInertRefusal routes a stale-class preflight refusal through the D4
// router: no durable mutation, terminal refused effect, next tick re-decides.
func commitInertRefusal(ctx context.Context, q Q, prepared preparedDispatch, rcand RefusalCandidate, code string) (map[string]any, error) {
	r := refusal.Refusal{Code: code, Field: refusal.FieldNone, Summary: refusal.SummaryMismatch}
	key, err := refusalDispatchKey(prepared, r)
	if err != nil {
		return nil, err
	}
	return applyRefusal(ctx, q, rcand, r, key)
}

// refusalDispatchKey derives the D2 fence for a refusal consequence from the
// preparation token and the canonical action naming the refusal — the
// derivation applyRefusal used to take on faith as an input.
func refusalDispatchKey(prepared preparedDispatch, r refusal.Refusal) (string, error) {
	cand := prepared.candidate
	return deriveDispatchKey(cand.token, canonicalAction{
		Version:     1,
		RequestID:   prepared.requestID,
		Consequence: "refusal",
		RunID:       cand.runID,
		Role:        string(cand.spawn.Role),
		SubjectID:   cand.spawn.SubjectID,
		Refusal: &canonicalRefusal{
			Code:      r.Code,
			Authority: string(r.Authority),
			Field:     string(r.Field),
			Summary:   string(r.Summary),
			ItemIndex: r.ItemIndex,
		},
	})
}

// refusalCandidateFor names who a pipeline candidate's refusal is about: the
// subject task when there is one to blame, the subjectless shape otherwise.
// Homie candidates arrive with the future wake selector.
func refusalCandidateFor(sp *dispatch.Spawn) RefusalCandidate {
	if sp.SubjectID != nil {
		return RefusalCandidate{Kind: RefusalSubjectTask, TaskID: sp.SubjectID}
	}
	return RefusalCandidate{Kind: RefusalSubjectlessPipeline}
}

// requireDeploymentUUID is D1's first inert precondition, checked before
// selection or mutation on every branch of both helper invocations.
func requireDeploymentUUID(ctx context.Context, q Q, uuid string) error {
	var stored string
	if err := q.QueryRowContext(ctx, `SELECT deployment_uuid FROM meta WHERE id = 1`).Scan(&stored); err != nil {
		return Domainf("read spine deployment identity: %v — restore from backup (§16.4)", err)
	}
	if stored != uuid {
		return Domainf("deployment identity mismatch: MC_HOME mirror %q does not name this spine's deployment %q (run: mc onboard home)", uuid, stored)
	}
	return nil
}

// readDeploymentMirrorStrict reads MC_HOME's identity mirror the way D1 pins
// for dispatch: a fixed non-symlink regular file, opened no-follow, bounded.
// onboard's readDeploymentMirror tolerates absence because provisioning is
// its job; dispatch never provisions, so an unonboarded or foreign MC_HOME
// refuses before any spine read.
func readDeploymentMirrorStrict(home string) (string, error) {
	path := filepath.Join(home, deploymentUUIDFilename)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", Domainf("read deployment identity mirror %q: %v (run: mc onboard home)", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", Domainf("read deployment identity mirror %q: %v (run: mc onboard home)", path, err)
	}
	if !fi.Mode().IsRegular() {
		return "", Domainf("deployment identity mirror %q must be a regular file (ADR-016 D1)", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", Domainf("read deployment identity mirror %q: %v", path, err)
	}
	uuid := strings.TrimSpace(string(b))
	if uuid == "" {
		return "", Domainf("deployment identity mirror %q is empty — restore from backup (§16.4)", path)
	}
	return uuid, nil
}

// lookupDispatchReceipt is the D2 replay fence's read half. An unreadable
// stored result is a protocol error, never a green light to re-execute.
func lookupDispatchReceipt(ctx context.Context, q Q, requestID string) (map[string]any, bool, error) {
	var stored string
	err := q.QueryRowContext(ctx,
		`SELECT dispatch_result FROM activity WHERE dispatch_request_id = ?`, requestID).Scan(&stored)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stored), &result); err != nil {
		return nil, false, Domainf("stored dispatch result for request %s is unreadable: %v — restore from backup (§16.4)", requestID, err)
	}
	return result, true, nil
}

func writeDispatchReceipt(ctx context.Context, q Q, requestID string, effect map[string]any) error {
	body, err := json.Marshal(effect)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
		INSERT INTO activity (actor, kind, subject, detail, dispatch_request_id, dispatch_result)
		VALUES ('dispatch', 'dispatch.result', NULL, NULL, ?, ?)`, requestID, string(body))
	return err
}

// writeSpawnReceipt records the attested commit's separate candidate/action
// digest fence (ADR-016:240-247): one activity row under the derived
// dispatch_key, whose UNIQUE index makes a same-action replay unstorable.
func writeSpawnReceipt(ctx context.Context, q Q, runID, key string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO activity (actor, kind, subject, dispatch_key)
		VALUES ('dispatch', 'dispatch.spawn', ?, ?)`, runID, key)
	return err
}
