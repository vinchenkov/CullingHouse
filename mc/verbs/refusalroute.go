package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"mc/domain"
	"mc/refusal"
)

// ---------------------------------------------------------------------------
// ADR-016 Decision 4's consequence router — the impure half of the
// invalid-plan/no-claim transaction.
//
// mc/refusal owns the pure half: which of D4's three classes a (code,
// authority) pair carries. This file owns the other half: applying that class's
// exact consequence to the spine, inside the caller's transaction.
//
// The split is the point. Deciding a class is a total function with no spine at
// all, so the whole table is proven by table-driven tests; applying it is a
// handful of writes with no policy in them. Neither half can drift into the
// other's job.
//
// D4's four-part invariant binds every arm: no new Run row, no claim on the
// lease, no spawn effect, and no fall-through to another candidate. A refusal
// is terminal for the tick — that is what "never claim/spawn" means.
// ---------------------------------------------------------------------------

// RefusalCandidateKind names the shape of the candidate a refusal was raised
// against. D4's candidate-policy consequence is subject-dependent, so the class
// alone does not determine what happens — this does.
type RefusalCandidateKind string

const (
	// RefusalSubjectTask is a pipeline candidate carrying a subject task: the
	// one shape that can be blamed for its own bad plan.
	RefusalSubjectTask RefusalCandidateKind = "subject_task"

	// RefusalSubjectlessPipeline is a pipeline candidate with no subject (the
	// Console, an initiative-less spawn). It has no task to blame, and D4 is
	// explicit that it must not invent one.
	RefusalSubjectlessPipeline RefusalCandidateKind = "subjectless_pipeline"

	// RefusalHomie is a Homie session candidate.
	RefusalHomie RefusalCandidateKind = "homie"
)

// RefusalCandidate identifies who a refusal is about.
type RefusalCandidate struct {
	Kind RefusalCandidateKind

	// TaskID is required for RefusalSubjectTask and ignored otherwise.
	TaskID *int64

	// SessionID is required for RefusalHomie and ignored otherwise.
	SessionID string

	// LaunchID is the session's current_launch_id as observed when the
	// Homie candidate was selected; empty means the row had no launch.
	// It is D3's generation fence on the end consequence: if the row has
	// moved to a different generation by commit time, the refusal judged
	// something that no longer exists and must not end the session.
	// Ignored for non-Homie candidates.
	LaunchID string
}

// applyRefusal routes one classified boundary refusal to its exact D4
// consequence and returns the tick's terminal effect.
//
// It runs inside the dispatch transaction, so every consequence commits atomically
// with the decision that produced it — a task is blocked, or a Homie is ended,
// or nothing happened at all. There is no window in which the refusal is known
// but unapplied.
//
// dispatchKey is D2's candidate/action replay fence, derived by the dispatch
// commit seam and passed in because this router owns consequences, not frame
// hashing. An empty key remains supported for the router's standalone tests;
// production attested consequences always supply the derived key. A malformed
// key is refused here rather than left to abort against the storage CHECK.
func applyRefusal(ctx context.Context, q Q, cand RefusalCandidate, r refusal.Refusal, dispatchKey string) (map[string]any, error) {
	return applyRefusalWithReceipt(ctx, q, cand, r, dispatchKey, "")
}

// applyRefusalWithReceipt is dispatchCommit's production entry: requestID is
// empty for the router's standalone tests/legacy callers, or the command-scoped
// replay key for an attested consequence. Health owns its activity insert, so
// it must place the exact result there at creation time (activity is append-only).
func applyRefusalWithReceipt(ctx context.Context, q Q, cand RefusalCandidate, r refusal.Refusal, dispatchKey, requestID string) (map[string]any, error) {
	// Classify first, and let an underivable class abort before any write.
	// Fail-closed: a refusal that does not classify applies NO consequence,
	// where a guessed one could block a task for the deployment's mistake.
	class, err := refusal.Classify(r)
	if err != nil {
		return nil, Domainf("%v", err)
	}
	// DetailFor re-runs Classify and additionally closes the enums. It is also
	// D4's sanitizer: it drops the producer's raw Message, which is the only
	// member that could carry a path, env value, credential, or nonce.
	detail, err := refusal.DetailFor(r)
	if err != nil {
		return nil, Domainf("%v", err)
	}
	if dispatchKey != "" && !validDispatchKey(dispatchKey) {
		return nil, Domainf("dispatch_key must be exactly 64 lowercase hex characters (ADR-016 D2)")
	}
	if err := validateRefusalCandidate(cand); err != nil {
		return nil, err
	}

	effect := map[string]any{"action": "refused", "class": string(class), "code": r.Code}

	switch class {
	case refusal.ClassStale:
		// D4's stale/protocol row: error/retry, no durable mutation or effect.
		// An ordinary concurrent operator edit lands here, which is exactly why
		// it can never punish a task, write health, or end a conversation. The
		// next tick re-reads the records and decides again.
		effect["consequence"] = "none"
		return effect, nil

	case refusal.ClassHealth:
		// The deployment's fault. One health action; no claim, no task
		// charge/block. If the crossing is down, the first recovered tick
		// records it.
		effect["consequence"] = "health"
		if err := recordDispatchHealth(ctx, q, cand, detail, dispatchKey, requestID, effect); err != nil {
			return nil, err
		}
		return effect, nil

	case refusal.ClassCandidate:
		return applyCandidatePolicy(ctx, q, cand, r, detail, dispatchKey, requestID, effect)
	}
	return nil, Domainf("refusal class %q has no consequence", class)
}

// applyCandidatePolicy is D4's candidate-policy row, whose consequence is
// subject-dependent.
func applyCandidatePolicy(ctx context.Context, q Q, cand RefusalCandidate, r refusal.Refusal, detail refusal.Detail, dispatchKey, requestID string, effect map[string]any) (map[string]any, error) {
	switch cand.Kind {
	case RefusalSubjectTask:
		// Atomically block with the code. The reason is the stable code and
		// nothing else: the producer's text never reaches the column.
		if err := domain.Block(ctx, q, *cand.TaskID, confinementReason(r.Code)); err != nil {
			return nil, err
		}
		effect["consequence"] = "task_blocked"
		effect["task_id"] = *cand.TaskID
		return effect, nil

	case RefusalSubjectlessPipeline:
		// No subject to blame, so this is health. D4 accepts that such a
		// candidate may recur every tick until its global configuration is
		// repaired — there is nothing to block, and inventing a task to carry
		// the blame would be worse than recurring.
		effect["consequence"] = "health"
		if err := recordDispatchHealth(ctx, q, cand, detail, dispatchKey, requestID, effect); err != nil {
			return nil, err
		}
		return effect, nil

	case RefusalHomie:
		// Ended in this same transaction, so it cannot remain the repeatedly
		// selected oldest active row and starve pipeline work (D4).
		//
		// D4 calls this a *launch-fenced* end: it applies only to the launch
		// generation the candidate was selected against (D3). A row that has
		// moved on — a launch bound after selection, or the observed one
		// superseded — is no longer the thing the refusal judged, so the end
		// applies NO consequence, the stale class's posture. No starvation
		// follows: the current generation is attested on the next tick and
		// earns its own verdict.
		held, err := homieLaunchFenceHolds(ctx, q, cand.SessionID, cand.LaunchID)
		if err != nil {
			return nil, err
		}
		if !held {
			effect["consequence"] = "none"
			effect["session_id"] = cand.SessionID
			effect["launch_superseded"] = true
			return effect, nil
		}
		ended, status, err := homieEndTx(ctx, q, "dispatch", cand.SessionID, confinementReason(r.Code))
		if err != nil {
			return nil, err
		}
		effect["consequence"] = "homie_ended"
		effect["session_id"] = cand.SessionID
		effect["ended"] = ended
		effect["status"] = status
		return effect, nil
	}
	return nil, Domainf("dispatch: unknown refusal candidate kind %q", cand.Kind)
}

// confinementReason is D4's exact reason grammar for a candidate-policy
// consequence: the stable code, prefixed.
//
// It is deliberately the whole reason. tasks.blocked_reason and activity.detail
// are both free-text columns, so nothing at the storage layer stops a raw
// producer string landing in them — the discipline has to hold here. The prefix
// also keeps a confinement block clear of the substrate's auto-unblock trigger,
// which fires only on the 'blocked child #%' shape: a confined task stays
// blocked until an operator or a repair unblocks it, never silently.
func confinementReason(code string) string { return "confinement:" + code }

// recordDispatchHealth appends D4's one health action.
//
// The detail is the closed canonical {code,field,item_index,summary} object,
// capped at 512 bytes and built only from enumerated identifiers, so it cannot
// carry a supplied path, env value, credential, or nonce even if the producer
// tried. The subject is the candidate's own identifier — already spine state,
// never producer input.
func recordDispatchHealth(ctx context.Context, q Q, cand RefusalCandidate, d refusal.Detail, dispatchKey, requestID string, effect map[string]any) error {
	payload, err := d.Canonical()
	if err != nil {
		return Domainf("%v", err)
	}
	var subject any // NULL for a subjectless candidate
	switch cand.Kind {
	case RefusalSubjectTask:
		subject = strconv.FormatInt(*cand.TaskID, 10)
	case RefusalHomie:
		subject = cand.SessionID
	}
	var key any // NULL until the prepare slice derives one
	if dispatchKey != "" {
		key = dispatchKey
	}
	if requestID == "" {
		_, err = q.ExecContext(ctx, `
			INSERT INTO activity (actor, kind, subject, detail, dispatch_key)
			VALUES ('dispatch', 'dispatch.health', ?, ?, ?)`, subject, string(payload), key)
		return err
	}
	result, err := json.Marshal(effect)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
		INSERT INTO activity
			(actor, kind, subject, detail, dispatch_key, dispatch_request_id, dispatch_result)
		VALUES ('dispatch', 'dispatch.health', ?, ?, ?, ?, ?)`,
		subject, string(payload), key, requestID, string(result))
	return err
}

// validateRefusalCandidate refuses a candidate the router cannot act on,
// before any write. An unknown kind or a missing identifier is a protocol
// error, never a default into some consequence.
func validateRefusalCandidate(c RefusalCandidate) error {
	switch c.Kind {
	case RefusalSubjectTask:
		if c.TaskID == nil {
			return Domainf("a subject-task refusal candidate needs a task id; D4 never invents a subject to blame")
		}
	case RefusalSubjectlessPipeline:
	case RefusalHomie:
		if c.SessionID == "" {
			return Domainf("a Homie refusal candidate needs a session id")
		}
		if c.LaunchID != "" && !validLowercaseHex(c.LaunchID, 16) {
			return Domainf("a Homie refusal candidate's observed launch id must be empty or exactly 16 lowercase hex characters (ADR-016 D3)")
		}
	default:
		return Domainf("dispatch: unknown refusal candidate kind %q", c.Kind)
	}
	return nil
}

// homieLaunchFenceHolds is D3's generation comparison for the launch-fenced
// end: the row's current_launch_id must still be the one the candidate
// observed at selection (empty = no launch). An unknown session holds the
// fence so homieEndTx states the canonical unknown-session refusal — the
// fence never invents its own copy of that error.
func homieLaunchFenceHolds(ctx context.Context, q Q, sessionID, observed string) (bool, error) {
	var current sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT current_launch_id FROM homie_sessions WHERE id = ?`, sessionID).Scan(&current)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return current.String == observed, nil
}

// validDispatchKey mirrors the substrate's ADR-016 D2 shape check: exactly 64
// lowercase hex characters. The storage CHECK pins the same shape with a
// dual-length NUL fence; this is the honest-error copy, not the authority.
func validDispatchKey(s string) bool { return validLowercaseHex(s, 64) }

func validLowercaseHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
