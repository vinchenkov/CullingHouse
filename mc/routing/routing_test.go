package routing

import (
	"strings"
	"testing"
)

const canonical = `# Mission Control routing
| role | harness | binding |
| --- | --- | --- |
| strategist | claude-sdk | claude |
| editor | codex | chatgpt |
| worker | claude-sdk | minimax |
| verifier | codex | chatgpt |
| packager | claude-sdk | minimax |
| refiner | codex | chatgpt |
| homie | claude-sdk | claude |
`

func TestParseCanonicalRouting(t *testing.T) {
	table, err := Parse([]byte(canonical), ProductionRegistry(), false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := table.Resolve("worker")
	if err != nil {
		t.Fatal(err)
	}
	if r.Harness != "claude-sdk" || r.Binding != "minimax" || r.HistoricalBinding() != "claude-sdk/minimax" {
		t.Fatalf("worker route = %+v", r)
	}
}

func TestParseRoutingRejectsInvalidConfigurations(t *testing.T) {
	tests := []struct {
		name string
		body string
		msg  string
	}{
		{"prose", "not a table\n" + canonical, "only the routing table"},
		{"extra_column", strings.Replace(canonical, "| role | harness | binding |", "| role | harness | binding | model |", 1), "exactly"},
		{"missing_role", strings.Replace(canonical, "| refiner | codex | chatgpt |\n", "", 1), "missing role"},
		{"duplicate_role", canonical + "| worker | claude-sdk | minimax |\n", "duplicate role"},
		{"unknown_role", strings.Replace(canonical, "| refiner |", "| critic |", 1), "unknown role"},
		{"unknown_binding", strings.Replace(canonical, "| worker | claude-sdk | minimax |", "| worker | claude-sdk | missing |", 1), "unresolved binding"},
		{"harness_mismatch", strings.Replace(canonical, "| worker | claude-sdk | minimax |", "| worker | codex | minimax |", 1), "resolves to harness"},
		{"worker_verifier_correlation", strings.Replace(canonical, "| worker | claude-sdk | minimax |", "| worker | codex | chatgpt |", 1), "decorrelated"},
		{"strategist_editor_correlation", strings.Replace(canonical, "| strategist | claude-sdk | claude |", "| strategist | codex | chatgpt |", 1), "decorrelated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.body), ProductionRegistry(), false)
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("error = %v, want containing %q", err, tc.msg)
			}
		})
	}
}

func TestProductionRegistryRejectsFake(t *testing.T) {
	fake := strings.NewReplacer(
		"claude-sdk", "fake",
		"codex", "fake",
		"claude", "fake",
		"chatgpt", "fake",
		"minimax", "fake",
	).Replace(canonical)
	if _, err := Parse([]byte(fake), ProductionRegistry(), false); err == nil || !strings.Contains(err.Error(), "unresolved binding") {
		t.Fatalf("production fake error = %v", err)
	}
}
