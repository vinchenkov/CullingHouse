package verbs

// ADR-016 D7's landing identity. Kept beside the dispatch seam's other
// derivations (dispatchseam.go) and following the same idiom — a closed
// canonical struct, a domain separator, SHA-256, hex — but with its own domain
// so a landing digest can never collide with a prepare token, a dispatch key,
// or a plan digest computed over the same bytes.
//
// Derived ATTEST-side, not in the dispatch table: all three inputs are already
// in attest scope, whereas the `Land` effect payload carries none of them and
// the dispatch table has no other use for a landing id.

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
// inputs: deployment, subject, and the exact approved packet/run identity.
//
// Every input must be stable across landing attempts. The id is not a
// per-attempt nonce: `abortOwnConflictedMerge` identifies a merge this system
// wrote by matching `MC-Landing-Id: <id>`, so a retry that derived a fresh id
// could never undo the merge its predecessor left behind.
type canonicalLandingIdentity struct {
	DeploymentUUID string `json:"deployment_uuid"`
	SubjectID      int64  `json:"subject_id"`
	RunID          string `json:"run_id"`
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
