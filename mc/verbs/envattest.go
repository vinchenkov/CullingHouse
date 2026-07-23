package verbs

import (
	"errors"

	"mc/boundary"
	"mc/refusal"
	"mc/routing"
)

// attestCandidateEnvPolicy validates both declared env planes before any
// pipeline lease claim (contract §2.2, §3 "Forbidden env"; ADR-022 D7). The
// zero-value guard is the non-shrinkable §16.3 floor — the operator extension
// surface arrives with its config loader, and until then additions are inert
// exactly like the blocked-pattern policy's. Provider credential keys and
// foreign static keys come from the binding catalog, which does not exist
// yet; the floor and the refresh-token fence carry the row until it does.
// The refusal names only the enumerated field identifier, never a value.
func attestCandidateEnvPolicy(selected PrivateDispatchWorksource, bindingID string) *refusal.Refusal {
	spec, ok := routing.ActiveBinding(bindingID)
	if !ok {
		return &refusal.Refusal{Code: refusal.CodeAuthBindingInvalid, Field: refusal.FieldNone, Summary: refusal.SummaryUnparsable}
	}
	foreignStaticKeys := []string{}
	for id, candidate := range routing.ActiveBindings() {
		if id != bindingID && candidate.DeclaredStaticKey != "" {
			foreignStaticKeys = append(foreignStaticKeys, candidate.DeclaredStaticKey)
		}
	}
	binding := boundary.EnvBinding{
		Delivery:               boundary.EnvDelivery(spec.Delivery),
		ProviderCredentialKeys: spec.ProviderCredentialKeys,
		DeclaredStaticKey:      spec.DeclaredStaticKey,
	}
	for _, policy := range []string{selected.HarnessEnvPolicy, selected.ToolEnvPolicy} {
		if _, err := boundary.BuildEnvPlan(policy, boundary.EnvGuard{}, binding, foreignStaticKeys); err != nil {
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
