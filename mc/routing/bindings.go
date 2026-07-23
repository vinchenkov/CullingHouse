package routing

// CredentialDelivery is the ADR-022 per-binding credential channel. It is
// intentionally catalog state, not sandbox-profile state: one Worksource can
// route different roles through OAuth and static bindings in the same run
// graph.
type CredentialDelivery string

const (
	CredentialProjection   CredentialDelivery = "projection"
	CredentialMaterialized CredentialDelivery = "materialized"
)

type BindingSpec struct {
	ID                     string
	Harness                string
	Channel                string
	Delivery               CredentialDelivery
	ProviderCredentialKeys []string
	DeclaredStaticKey      string
	BaseURL                string
	TokenURL               string
	ClientID               string
}

var productionBindings = map[string]BindingSpec{
	"chatgpt": {
		ID: "chatgpt", Harness: "codex", Channel: "codex", Delivery: CredentialProjection,
		ProviderCredentialKeys: []string{"CODEX_ACCESS_TOKEN", "CODEX_API_KEY", "OPENAI_API_KEY"},
		TokenURL:               "https://auth.openai.com/oauth/token", ClientID: "app_EMoamEEZ73f0CkXaXp7hrann",
	},
	"claude": {
		ID: "claude", Harness: "claude-sdk", Channel: "claude", Delivery: CredentialProjection,
		ProviderCredentialKeys: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
		TokenURL:               "https://platform.claude.com/v1/oauth/token", ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
	},
	"minimax": {
		ID: "minimax", Harness: "claude-sdk", Channel: "static", Delivery: CredentialMaterialized,
		ProviderCredentialKeys: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
		DeclaredStaticKey:      "ANTHROPIC_AUTH_TOKEN",
		BaseURL:                "https://api.minimax.io/anthropic",
	},
}

func cloneBindingSpec(spec BindingSpec) BindingSpec {
	spec.ProviderCredentialKeys = append([]string(nil), spec.ProviderCredentialKeys...)
	return spec
}

func ProductionBinding(id string) (BindingSpec, bool) {
	spec, ok := productionBindings[id]
	return cloneBindingSpec(spec), ok
}

func ProductionBindings() map[string]BindingSpec {
	out := make(map[string]BindingSpec, len(productionBindings))
	for id, spec := range productionBindings {
		out[id] = cloneBindingSpec(spec)
	}
	return out
}
