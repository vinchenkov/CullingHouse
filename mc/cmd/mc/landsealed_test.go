package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The landing verb's CLI surface. The lane itself is proven in mc/verbs against
// real repositories; what matters here is that the verb is REACHABLE, runs
// locally rather than self-delegating into the helper (which has no `/repo`
// bind), and is refused to any agent-scoped caller.
//
// This exists because a host-scope verb whose wiring is untested is exactly the
// rot 6657541 and 83ed9e9 both found: the implementation stays correct while
// nothing reaches it.

func writeLandingFile(t *testing.T, env map[string]any) string {
	t.Helper()
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "landing.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A well-formed landing envelope whose fixed destinations do not exist on the
// host: it must get PAST decode and validation and fail in the lane, which is
// what proves the verb is wired to the lane at all.
func landingEnvelopeJSON() map[string]any {
	return map[string]any{
		"schema_version": 1, "operation": "sealed-landing", "task_id": 7,
		"object_format": "sha1", "branch": "mc/task-7", "target_ref": "main",
		"source_repo": "/repo/source", "task_root": "/repo/task",
		"cover_root":   "/repo/source/.mission-control",
		"landing_id":   "0011223344556677",
		"verified_sha": strings.Repeat("a", 40), "pre_merge_sha": strings.Repeat("b", 40),
		"pinned_base_sha":        strings.Repeat("c", 40),
		"pinned_closure_digest":  strings.Repeat("d", 64),
		"pinned_local_repo_uuid": "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	}
}

func TestLandSealedVerbIsHostScopeOnly(t *testing.T) {
	path := writeLandingFile(t, landingEnvelopeJSON())

	// Host scope: reaches the lane and refuses there (the fixed container
	// destinations are absent on the host), NOT at usage or decode.
	res := runMC(t, nil, "", "__land-sealed", path)
	if res.code == 0 {
		t.Fatalf("landing succeeded against absent container destinations: %v", res.json)
	}
	if res.code == 2 {
		t.Fatalf("landing failed at usage rather than reaching the lane: stderr=%q", res.stderr)
	}

	// An agent-scoped caller is refused outright.
	agentRun := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(agentRun, []byte(`{"run_id":"r","tier":"pipeline","role":"worker"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	denied := runMC(t, []string{"MC_RUN_JSON=" + agentRun}, "", "__land-sealed", path)
	if denied.code == 0 {
		t.Fatalf("an agent-scoped caller ran the landing executor: %v", denied.json)
	}
}

func TestLandSealedVerbRequiresExactlyOneArgument(t *testing.T) {
	if res := runMC(t, nil, "", "__land-sealed"); res.code != 2 {
		t.Fatalf("bare verb exit = %d, want usage 2 (stderr=%q)", res.code, res.stderr)
	}
}

// A run id is the bleed direction that hides: every setup arm legitimately
// carries one, and landing holds no lease and opens no Run (§7).
func TestLandSealedVerbRefusesAnEnvelopeNamingARun(t *testing.T) {
	env := landingEnvelopeJSON()
	env["run_id"] = "run-1"
	res := runMC(t, nil, "", "__land-sealed", writeLandingFile(t, env))
	if res.code == 0 {
		t.Fatal("the landing verb accepted an envelope naming a run")
	}
}
