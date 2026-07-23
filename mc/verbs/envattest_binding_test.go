package verbs

import (
	"testing"

	"mc/refusal"
)

func TestCandidateEnvPolicyUsesSelectedBindingCatalog(t *testing.T) {
	cases := []struct {
		name, binding, policy string
		wantCode              string
	}{
		{name: "codex provider key", binding: "chatgpt", policy: `{"OPENAI_API_KEY":"metered"}`, wantCode: refusal.CodeEnvForbidden},
		{name: "codex agent identity", binding: "chatgpt", policy: `{"CODEX_ACCESS_TOKEN":"wrong-plane"}`, wantCode: refusal.CodeEnvForbidden},
		{name: "claude provider key", binding: "claude", policy: `{"ANTHROPIC_AUTH_TOKEN":"wrong-plane"}`, wantCode: refusal.CodeEnvForbidden},
		{name: "foreign static key", binding: "chatgpt", policy: `{"ANTHROPIC_AUTH_TOKEN":"foreign"}`, wantCode: refusal.CodeEnvForbidden},
		{name: "minimax own static key", binding: "minimax", policy: `{"ANTHROPIC_AUTH_TOKEN":"subscription"}`},
		{name: "shipped floor survives static binding", binding: "minimax", policy: `{"ANTHROPIC_API_KEY":"metered"}`, wantCode: refusal.CodeEnvForbidden},
		{name: "unknown binding", binding: "invented", policy: `{}`, wantCode: refusal.CodeAuthBindingInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selected := PrivateDispatchWorksource{
				HarnessEnvPolicy: tc.policy,
				// This stale profile field deliberately disagrees for MiniMax:
				// delivery is per binding, so profile state cannot weaken it.
				RuntimeAuthDelivery: "projection",
			}
			got := attestCandidateEnvPolicy(selected, tc.binding)
			if tc.wantCode == "" && got != nil {
				t.Fatalf("unexpected refusal: %+v", got)
			}
			if tc.wantCode != "" && (got == nil || got.Code != tc.wantCode) {
				t.Fatalf("refusal = %+v, want %s", got, tc.wantCode)
			}
		})
	}
}
