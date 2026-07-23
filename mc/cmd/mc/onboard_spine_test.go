package main_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"mc/substrate"
)

func onboardSpineFrame(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"protocol_version": 1,
		"schema_version":   substrate.CurrentSchemaVersion,
		"mirror_state":     "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPrivateOnboardSpineCommandUsesOnlyFixedHelperScope(t *testing.T) {
	spine := filepath.Join(t.TempDir(), "spine.db")
	frame := onboardSpineFrame(t)

	withoutScope := runMC(t, nil, frame, "__onboard-spine")
	if withoutScope.code != 1 || !strings.Contains(withoutScope.stderr, "helper's fixed spine scope") {
		t.Fatalf("without scope: code=%d stderr=%q json=%v", withoutScope.code, withoutScope.stderr, withoutScope.json)
	}

	got := runMC(t, spineEnv(spine), frame, "__onboard-spine")
	if got.code != 0 || got.json["status"] != "initialized" || got.json["deployment_uuid"] == "" {
		t.Fatalf("private onboard: code=%d stderr=%q json=%v", got.code, got.stderr, got.json)
	}

	unknown := strings.TrimSuffix(frame, "}") + `,"home":"/host"}`
	refused := runMC(t, spineEnv(spine), unknown, "__onboard-spine")
	if refused.code != 2 || !strings.Contains(refused.stderr, "unknown field") {
		t.Fatalf("unknown frame field: code=%d stderr=%q json=%v", refused.code, refused.stderr, refused.json)
	}
}
