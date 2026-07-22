package verbs

import (
	"errors"

	"mc/boundary"
	"mc/refusal"
)

// attestCandidateEnvPolicy validates both declared env planes before any
// pipeline lease claim (contract §2.2, §3 "Forbidden env"; ADR-022 D7). The
// zero-value guard is the non-shrinkable §16.3 floor — the operator extension
// surface arrives with its config loader, and until then additions are inert
// exactly like the blocked-pattern policy's. Provider credential keys and
// foreign static keys come from the binding catalog, which does not exist
// yet; the floor and the refresh-token fence carry the row until it does.
// The refusal names only the enumerated field identifier, never a value.
func attestCandidateEnvPolicy(selected PrivateDispatchWorksource) *refusal.Refusal {
	binding := boundary.EnvBinding{Delivery: boundary.EnvDelivery(selected.RuntimeAuthDelivery)}
	for _, policy := range []string{selected.HarnessEnvPolicy, selected.ToolEnvPolicy} {
		if _, err := boundary.BuildEnvPlan(policy, boundary.EnvGuard{}, binding, nil); err != nil {
			code, summary := refusal.CodeEnvInvalid, refusal.SummaryUnparsable
			var policyErr *boundary.EnvPolicyError
			if errors.As(err, &policyErr) && policyErr.Kind == boundary.EnvPolicyForbidden {
				code, summary = refusal.CodeEnvForbidden, refusal.SummaryForbidden
			}
			// Authority stays empty: the env codes are ClassCandidate by
			// their own table entry, so authority is not one of their
			// dimensions (refusal.go D4 note).
			return &refusal.Refusal{Code: code, Field: refusal.FieldEnvName, Summary: summary}
		}
	}
	return nil
}
