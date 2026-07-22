package verbs

import (
	"strings"
	"testing"

	"mc/dispatch"
)

func TestFrozenRoleDirectivesCoverEverySpawnRole(t *testing.T) {
	roles := []dispatch.Role{
		dispatch.RoleStrategistPropose,
		dispatch.RoleEditor,
		dispatch.RoleEditorPlanReview,
		dispatch.RoleWorker,
		dispatch.RoleVerifier,
		dispatch.RolePackager,
		dispatch.RoleRefiner,
		dispatch.RoleStrategistInitiative,
		dispatch.RoleStrategistConsole,
	}
	seen := map[string]dispatch.Role{}
	for _, role := range roles {
		directive, err := directiveForRole(role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		for _, required := range []string{
			"Orchestrate by default.", "read-only", "depth-1", "exactly one",
		} {
			if !strings.Contains(directive, required) {
				t.Errorf("%s directive missing %q", role, required)
			}
		}
		if prior, duplicate := seen[directive]; duplicate {
			t.Errorf("%s and %s share an undifferentiated directive", prior, role)
		}
		seen[directive] = role
	}
}

func TestUnknownSpawnRoleHasNoFallbackDirective(t *testing.T) {
	if _, err := directiveForRole(dispatch.Role("unknown")); err == nil {
		t.Fatal("unknown role received a fallback directive")
	}
}

// TestDirectivesEncodeTheSelfOrchestrationContract pins the spec §9.2 frozen
// content: every directive names the same four-pattern menu and carries the
// escape valve verbatim; harness-family phrasing follows the §9.1 default
// policy (producing roles on claude-sdk use the exact trigger term "dynamic
// workflow"; judging roles on codex select a named pattern in bounded
// rounds); the Verifier's directive states the criterion-driven/N-A gate
// rule so Inv. 12 holds (spec §7).
func TestDirectivesEncodeTheSelfOrchestrationContract(t *testing.T) {
	patterns := []string{
		"Fanout-And-Synthesize",
		"Adversarial Verification",
		"Generate-And-Filter",
		"Tournament",
	}
	escapeValve := "if you must hold the whole thing in your head to take the next step, do not spawn"
	claudeSDK := []dispatch.Role{
		dispatch.RoleStrategistPropose,
		dispatch.RoleStrategistInitiative,
		dispatch.RoleStrategistConsole,
		dispatch.RoleWorker,
		dispatch.RolePackager,
	}
	codex := []dispatch.Role{
		dispatch.RoleEditor,
		dispatch.RoleEditorPlanReview,
		dispatch.RoleVerifier,
		dispatch.RoleRefiner,
	}
	// Directives are wrapped prose; compare with whitespace collapsed.
	flat := func(role dispatch.Role) string {
		directive, err := directiveForRole(role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		return strings.Join(strings.Fields(directive), " ")
	}
	for _, role := range append(append([]dispatch.Role{}, claudeSDK...), codex...) {
		directive := flat(role)
		for _, p := range patterns {
			if !strings.Contains(directive, p) {
				t.Errorf("%s directive does not name the %s pattern", role, p)
			}
		}
		if !strings.Contains(directive, escapeValve) {
			t.Errorf("%s directive is missing the escape valve clause", role)
		}
		if strings.Contains(directive, "as you see fit") {
			t.Errorf("%s directive phrases discretion as %q", role, "as you see fit")
		}
		for _, banned := range []string{"Classify-And-Act", "Loop-Until-Done"} {
			if strings.Contains(directive, banned) {
				t.Errorf("%s directive offers the excluded %s pattern", role, banned)
			}
		}
	}
	for _, role := range claudeSDK {
		if !strings.Contains(flat(role), "dynamic workflow") {
			t.Errorf("%s (claude-sdk) directive lacks the exact trigger term %q", role, "dynamic workflow")
		}
	}
	for _, role := range codex {
		directive := flat(role)
		if !strings.Contains(directive, "bounded rounds") {
			t.Errorf("%s (codex) directive lacks the bounded-rounds instruction", role)
		}
		if strings.Contains(directive, "dynamic workflow") {
			t.Errorf("%s (codex) directive uses the claude-sdk trigger term", role)
		}
	}
	if !strings.Contains(flat(dispatch.RoleVerifier), "N/A") {
		t.Error("verifier directive is missing the criterion-driven N/A gate rule")
	}
}
