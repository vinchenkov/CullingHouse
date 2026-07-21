package verbs

// ADR-016 D7's landing identity. Kept beside the dispatch seam's other
// derivations (dispatchseam.go) and following the same idiom — a closed
// canonical struct, a domain separator, SHA-256, hex — but with its own domain
// so a landing digest can never collide with a prepare token, a dispatch key,
// or a plan digest computed over the same bytes.
//
// Derived at PREPARE, not in the dispatch table and not at attest. An earlier
// draft sited this attest-side on availability alone; ADR-016:371 is the
// stronger argument, because it names the deterministic id as a member of the
// candidate TUPLE, and a tuple member must be inside the preparation token or
// commit cannot detect its drift. All four inputs are in prepare's scope. The
// dispatch table stays out of it either way: the `Land` effect payload carries
// none of these inputs and has no other use for a landing id.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// landingIDDomain separates ADR-016 D7's landing identity from every other
// dispatch-seam digest.
const landingIDDomain = "MC-DISPATCH-LANDING-V1\x00"

// landingIDHexLen is the ADR-016 D7 width: the first 16 lowercase hex of the
// digest. `mc-landing-<id>` is a container name, and landsealedrun.go refuses
// any other width because the abort path matches the id in MERGE_MSG.
const landingIDHexLen = 16

// canonicalLandingIdentity is the closed encoding of ADR-016 D7's three
// inputs: deployment, subject, and "the exact approved packet/run identity".
//
// That last phrase appears exactly once in the design corpus (ADR-016:833,
// echoed as "approved-run identity" at :846) and is never expanded, so its
// referent is resolved here rather than re-derived at every call site:
//
//   - The PACKET contributes nothing. `review_packets.task_id` is both primary
//     key and foreign key (schema.sql:453-454) — one packet per task for life,
//     with no id of its own — so "packet identity" is numerically the subject,
//     which the digest already names separately.
//   - The approved RUN is the Worker run whose seal was accepted:
//     `tasks.accepted_completion_run_id` / `accepted_completion_request_id`
//     (schema.sql:116-120), "the exact downstream authority: the currently
//     accepted Worker seal for this task". Approval itself is an operator write
//     with no run (spec §5), so there is no other run to mean. The pair, not
//     the run alone, is what makes it "exact".
//
// Every input is stable across landing attempts, and that is load-bearing
// rather than incidental. ADR-017:753-756 requires a retry to match "its exact
// action trailer" before adopting a merge, and this lane's trailer is
// `MC-Landing-Id: <id>` — so a per-attempt id would leave a crashed attempt's
// merge state unrecognizable to its own successor, which is the one state
// `abortOwnConflictedMerge` exists to resolve. Deliberately NOT inputs: the
// dispatch request id and dispatch key, which ADR-016:110-111 marks "not
// canonical work state" and regenerates every tick.
type canonicalLandingIdentity struct {
	DeploymentUUID    string `json:"deployment_uuid"`
	SubjectID         int64  `json:"subject_id"`
	ApprovedRunID     string `json:"approved_run_id"`
	ApprovedRequestID string `json:"approved_request_id"`
}

func (c canonicalLandingIdentity) bytes() ([]byte, error) {
	return json.Marshal(c)
}

// deriveLandingID computes ADR-016 D7's landing id: the first 16 lowercase hex
// of the domain-separated digest over the canonical identity bytes.
func deriveLandingID(id canonicalLandingIdentity) (string, error) {
	body, err := id.bytes()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(landingIDDomain))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))[:landingIDHexLen], nil
}
