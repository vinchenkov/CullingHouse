// Package routing parses and validates MC_HOME/routing.md, the authoritative
// role-to-runtime map (spec §9.1, §16.2/§16.3).
package routing

import (
	"fmt"
	"strings"
)

var requiredRoles = []string{
	"strategist", "editor", "worker", "verifier", "packager", "refiner", "homie",
}

// Registry resolves a model binding id to its harness family. Phase 2 uses
// the shipped canonical registry; config.toml replaces this injected input
// when the onboarding/config layer lands.
type Registry map[string]string

// ProductionRegistry is the shipped §9.1 binding catalog. The fake family is
// deliberately absent from production.
func ProductionRegistry() Registry {
	registry := Registry{}
	for id, spec := range productionBindings {
		registry[id] = spec.Harness
	}
	return registry
}

// Route is one validated role row.
type Route struct {
	Role    string
	Harness string
	Binding string
}

// HistoricalBinding is the immutable harness/binding locator stored on Runs.
func (r Route) HistoricalBinding() string { return r.Harness + "/" + r.Binding }

// Table is a complete validated routing snapshot.
type Table struct {
	byRole map[string]Route
}

// Resolve returns the route for a base role (Strategist modes all use
// "strategist"). Parse guarantees every required role exists.
func (t Table) Resolve(role string) (Route, error) {
	r, ok := t.byRole[role]
	if !ok {
		return Route{}, fmt.Errorf("routing.md has no route for role %q", role)
	}
	return r, nil
}

// Parse accepts exactly one Markdown pipe table with columns
// role|harness|binding. Blank lines and # heading/comment lines are allowed;
// prose, extra tables/columns, duplicate/missing/unknown roles, unresolved
// bindings, and harness mismatches fail closed.
func Parse(data []byte, registry Registry, allowFakeDecorrelation bool) (Table, error) {
	table := Table{byRole: map[string]Route{}}
	state := 0 // 0 header, 1 separator, 2 data
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			return Table{}, fmt.Errorf("routing.md line %d: only the routing table, blank lines, and # comments are allowed", lineNo+1)
		}
		parts := strings.Split(strings.Trim(line, "|"), "|")
		if len(parts) != 3 {
			return Table{}, fmt.Errorf("routing.md line %d: want exactly role|harness|binding", lineNo+1)
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		switch state {
		case 0:
			if parts[0] != "role" || parts[1] != "harness" || parts[2] != "binding" {
				return Table{}, fmt.Errorf("routing.md line %d: first row must be | role | harness | binding |", lineNo+1)
			}
			state = 1
			continue
		case 1:
			for _, part := range parts {
				if strings.Trim(part, "- ") != "" || len(strings.TrimSpace(part)) < 3 {
					return Table{}, fmt.Errorf("routing.md line %d: malformed Markdown separator", lineNo+1)
				}
			}
			state = 2
			continue
		}

		role, harness, binding := parts[0], parts[1], parts[2]
		if !isRequiredRole(role) {
			return Table{}, fmt.Errorf("routing.md line %d: unknown role %q", lineNo+1, role)
		}
		if _, exists := table.byRole[role]; exists {
			return Table{}, fmt.Errorf("routing.md line %d: duplicate role %q", lineNo+1, role)
		}
		resolvedHarness, ok := registry[binding]
		if !ok {
			return Table{}, fmt.Errorf("routing.md line %d: unresolved binding %q", lineNo+1, binding)
		}
		if harness != resolvedHarness {
			return Table{}, fmt.Errorf("routing.md line %d: binding %q resolves to harness %q, not %q", lineNo+1, binding, resolvedHarness, harness)
		}
		table.byRole[role] = Route{Role: role, Harness: harness, Binding: binding}
	}
	if state != 2 {
		return Table{}, fmt.Errorf("routing.md: missing role|harness|binding table")
	}
	for _, role := range requiredRoles {
		if _, ok := table.byRole[role]; !ok {
			return Table{}, fmt.Errorf("routing.md: missing role %q", role)
		}
	}
	for _, pair := range [][2]string{{"strategist", "editor"}, {"worker", "verifier"}} {
		a, b := table.byRole[pair[0]], table.byRole[pair[1]]
		if a.Harness == b.Harness && !(allowFakeDecorrelation && a.Harness == "fake") {
			return Table{}, fmt.Errorf("routing.md: %s↔%s must use decorrelated harness families (both resolve to %q, Inv. 9)", pair[0], pair[1], a.Harness)
		}
	}
	return table, nil
}

func isRequiredRole(got string) bool {
	for _, role := range requiredRoles {
		if got == role {
			return true
		}
	}
	return false
}
