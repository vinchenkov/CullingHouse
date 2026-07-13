package verbs

import (
	_ "embed"
	"fmt"
	"strings"

	"mc/dispatch"
)

// These are product-owned, frozen directives (Inv. 20), embedded into the mc
// binary so installation cannot accidentally omit or locally reinterpret
// them. Changing one is a reviewed product change, not operator config.

//go:embed directives/strategist-propose.md
var strategistProposeDirective string

//go:embed directives/editor.md
var editorDirective string

//go:embed directives/worker.md
var workerDirective string

//go:embed directives/verifier.md
var verifierDirective string

//go:embed directives/packager.md
var packagerDirective string

//go:embed directives/refiner.md
var refinerDirective string

//go:embed directives/strategist-initiative.md
var strategistInitiativeDirective string

//go:embed directives/strategist-console.md
var strategistConsoleDirective string

func directiveForRole(role dispatch.Role) (string, error) {
	directives := map[dispatch.Role]string{
		dispatch.RoleStrategistPropose:    strategistProposeDirective,
		dispatch.RoleEditor:               editorDirective,
		dispatch.RoleWorker:               workerDirective,
		dispatch.RoleVerifier:             verifierDirective,
		dispatch.RolePackager:             packagerDirective,
		dispatch.RoleRefiner:              refinerDirective,
		dispatch.RoleStrategistInitiative: strategistInitiativeDirective,
		dispatch.RoleStrategistConsole:    strategistConsoleDirective,
	}
	directive, ok := directives[role]
	if !ok {
		return "", fmt.Errorf("no frozen directive for role %q (Inv. 20)", role)
	}
	return strings.TrimSpace(directive), nil
}
