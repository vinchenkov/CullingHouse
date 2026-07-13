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
