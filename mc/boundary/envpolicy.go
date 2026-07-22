package boundary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	maxEnvPolicyEntries    = 128
	maxEnvNameBytes        = 255
	maxEnvValueBytes       = 4096
	maxAdditionalEnvGuards = 128
)

// envGuardFloor is the spec §16.3 shipped forbidden-env floor. Like the
// blocked-pattern floor it is private and always evaluated, including by the
// zero-value EnvGuard, so no configuration shape can shrink it (ADR-022 D7).
var envGuardFloor = [...]string{"CODEX_API_KEY", "ANTHROPIC_API_KEY"}

// EnvDelivery mirrors sandbox_profiles.runtime_auth_delivery (schema v12).
type EnvDelivery string

const (
	// DeliveryProjection is an OAuth binding: the projected credential file
	// is the only credential material, so provider refresh tokens and
	// provider API keys are forbidden env (ADR-022 D7).
	DeliveryProjection EnvDelivery = "projection"
	// DeliveryMaterialized is a declared static-key downgrade (ADR-022 D5):
	// the single declared key is permitted in its own binding's plane alone.
	DeliveryMaterialized EnvDelivery = "materialized"
)

// EnvBinding is the credential context an env plane is validated under.
type EnvBinding struct {
	Delivery EnvDelivery
	// ProviderCredentialKeys are the binding's own provider credential env
	// names from the binding catalog (for example OPENAI_API_KEY for a Codex
	// binding). For a projection binding every one of them is forbidden.
	ProviderCredentialKeys []string
	// DeclaredStaticKey is the one materialized env name of a D5 binding;
	// empty for projection bindings. It never overrides the shipped floor.
	DeclaredStaticKey string
}

// EnvGuard contains only validated operator additions. The non-removable
// shipped floor is compiled into Forbids rather than stored on the value, so
// even a zero-value guard cannot omit it.
type EnvGuard struct {
	additions []string
}

// NewEnvGuard validates the additive operator extension. It has no API for
// replacement, deletion, or negation of the shipped floor.
func NewEnvGuard(additions []string) (EnvGuard, error) {
	if len(additions) > maxAdditionalEnvGuards {
		return EnvGuard{}, fmt.Errorf("env guard: %d additions exceeds %d-name limit", len(additions), maxAdditionalEnvGuards)
	}
	validated := make([]string, len(additions))
	for i, addition := range additions {
		if err := validateEnvName(addition); err != nil {
			return EnvGuard{}, fmt.Errorf("env guard addition[%d]: %w", i, err)
		}
		validated[i] = addition
	}
	return EnvGuard{additions: validated}, nil
}

// Forbids reports whether a name is in the effective forbidden guard: the
// immutable shipped floor plus the validated operator additions.
func (g EnvGuard) Forbids(name string) bool {
	for _, floor := range envGuardFloor {
		if name == floor {
			return true
		}
	}
	for _, addition := range g.additions {
		if name == addition {
			return true
		}
	}
	return false
}

// EnvEntry is one declared environment assignment.
type EnvEntry struct {
	Name  string
	Value string
}

// EnvPlan is the validated environment for one plane (harness or tool),
// built from the declared policy alone — never from ambient environment.
type EnvPlan struct {
	entries      []EnvEntry
	apiKeyShaped []string
}

// Entries returns the declared assignments sorted by name.
func (p EnvPlan) Entries() []EnvEntry {
	return append([]EnvEntry(nil), p.entries...)
}

// APIKeyShaped returns every *_API_KEY-shaped declared name, sorted. The
// wildcard scan is classification for the guard and doctor; shape alone never
// rejects, because §5 permits operator-managed tool secrets.
func (p EnvPlan) APIKeyShaped() []string {
	return append([]string(nil), p.apiKeyShaped...)
}

// EnvPolicyErrorKind partitions builder refusals into the two D4 candidate
// env codes: env.invalid and env.forbidden.
type EnvPolicyErrorKind string

const (
	EnvPolicyInvalid   EnvPolicyErrorKind = "invalid"
	EnvPolicyForbidden EnvPolicyErrorKind = "forbidden"
)

// EnvPolicyError names the offending entry without ever carrying its value.
type EnvPolicyError struct {
	Kind   EnvPolicyErrorKind
	Name   string
	Reason string
}

func (e *EnvPolicyError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("env policy %s: %s", e.Kind, e.Reason)
	}
	return fmt.Sprintf("env policy %s: %s: %s", e.Kind, e.Name, e.Reason)
}

func envInvalid(name, reason string) error {
	return &EnvPolicyError{Kind: EnvPolicyInvalid, Name: name, Reason: reason}
}

func envForbidden(name, reason string) error {
	return &EnvPolicyError{Kind: EnvPolicyForbidden, Name: name, Reason: reason}
}

// BuildEnvPlan validates one plane's declared env policy under the effective
// guard and the binding's credential context. The policy is a flat JSON
// object of name→value strings; an empty or absent policy is the safe base:
// no entries at all. foreignStaticKeys are the declared static keys of every
// OTHER binding — a materialized secret exists only in its own plane.
func BuildEnvPlan(policyJSON string, guard EnvGuard, binding EnvBinding, foreignStaticKeys []string) (EnvPlan, error) {
	declared := map[string]string{}
	if strings.TrimSpace(policyJSON) != "" {
		if err := json.Unmarshal([]byte(policyJSON), &declared); err != nil {
			return EnvPlan{}, envInvalid("", "policy must be a flat JSON object of name to string value")
		}
	}
	if len(declared) > maxEnvPolicyEntries {
		return EnvPlan{}, envInvalid("", fmt.Sprintf("%d entries exceeds %d-entry limit", len(declared), maxEnvPolicyEntries))
	}

	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)

	plan := EnvPlan{}
	for _, name := range names {
		if err := validateEnvName(name); err != nil {
			return EnvPlan{}, envInvalid(name, err.Error())
		}
		if err := validateEnvValue(declared[name]); err != nil {
			return EnvPlan{}, envInvalid(name, err.Error())
		}
		if isAPIKeyShaped(name) {
			plan.apiKeyShaped = append(plan.apiKeyShaped, name)
		}
		if guard.Forbids(name) {
			return EnvPlan{}, envForbidden(name, "name is in the effective forbidden guard (§16.3 floor plus operator additions)")
		}
		if err := checkBindingCredentialFence(name, binding, foreignStaticKeys); err != nil {
			return EnvPlan{}, err
		}
		plan.entries = append(plan.entries, EnvEntry{Name: name, Value: declared[name]})
	}
	return plan, nil
}

// checkBindingCredentialFence enforces ADR-022 D5/D7: refresh-token-shaped
// names never enter any plane, a binding's provider credential keys never
// enter a projection plane, and another binding's declared static key never
// crosses into this one. The binding's own declared static key is the single
// declared exception, permitted in its own plane alone.
func checkBindingCredentialFence(name string, binding EnvBinding, foreignStaticKeys []string) error {
	if name == binding.DeclaredStaticKey && binding.Delivery == DeliveryMaterialized {
		return nil
	}
	if strings.HasSuffix(name, "_REFRESH_TOKEN") {
		return envForbidden(name, "provider refresh material never enters a container (ADR-022 D7)")
	}
	for _, provider := range binding.ProviderCredentialKeys {
		if name == provider {
			return envForbidden(name, "the projected credential file is the binding's only credential material (ADR-022 D7)")
		}
	}
	for _, foreign := range foreignStaticKeys {
		if name == foreign {
			return envForbidden(name, "another binding's declared static key never crosses planes (ADR-022 D5)")
		}
	}
	return nil
}

func isAPIKeyShaped(name string) bool {
	return strings.HasSuffix(name, "_API_KEY")
}

func validateEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(name) > maxEnvNameBytes {
		return fmt.Errorf("name exceeds %d ASCII bytes", maxEnvNameBytes)
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		switch {
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b == '_':
		case b >= '0' && b <= '9':
			if i == 0 {
				return fmt.Errorf("name must not start with a digit")
			}
		default:
			return fmt.Errorf("name permits ASCII letters, digits, and underscore only")
		}
	}
	return nil
}

func validateEnvValue(value string) error {
	if len(value) > maxEnvValueBytes {
		return fmt.Errorf("value exceeds %d bytes", maxEnvValueBytes)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 && value[i] != '\t' {
			return fmt.Errorf("value must not contain control bytes")
		}
	}
	return nil
}
