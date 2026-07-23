//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/deployment"
)

func runtimeAuthSourceFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runtimeAuthBrokerArgs(t *testing.T, home string) []string {
	t.Helper()
	sourceRoot := filepath.Join(home, "runtime-auth-sources")
	if err := os.Mkdir(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	sources, err := os.MkdirTemp(sourceRoot, "provider-homes-")
	if err != nil {
		t.Fatal(err)
	}
	codex := runtimeAuthSourceFile(t, sources, "auth.json", `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"access_token":"ca","id_token":"ci","refresh_token":"cr","account_id":"acct"}}`)
	claude := runtimeAuthSourceFile(t, sources, ".credentials.json", `{"claudeAiOauth":{"accessToken":"aa","refreshToken":"ar","expiresAt":9999999999999,"scopes":["user:inference"]}}`)
	minimax := runtimeAuthSourceFile(t, sources, "minimax", "minimax-subscription-secret\n")
	return []string{"onboard", "runtime-auth", "--codex-auth-file", codex,
		"--claude-credentials-file", claude, "--minimax-token-file", minimax}
}

func TestBrokerOnboardRuntimeAuthPublishesOnlyAfterAllLiveGates(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MC_HOME", home)
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CODEX_ACCESS_TOKEN", "CODEX_API_KEY", "MINIMAX_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(key, "")
	}
	original := productionRuntimeAuthVerifier
	t.Cleanup(func() { productionRuntimeAuthVerifier = original })
	seen := []string{}
	productionRuntimeAuthVerifier = deployment.RuntimeAuthVerifyFunc(func(binding, stage string) error {
		seen = append(seen, binding)
		if _, err := os.Stat(filepath.Join(stage, binding+".json")); err != nil {
			t.Fatalf("missing staged grant for %s: %v", binding, err)
		}
		return nil
	})
	var stdout, stderr bytes.Buffer
	if code := brokerOnboardRuntimeAuth(runtimeAuthBrokerArgs(t, home), &stdout, &stderr); code != 0 {
		t.Fatalf("broker exit %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Join(seen, ",") != "chatgpt,claude,minimax" {
		t.Fatalf("live gate order = %v", seen)
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["ok"] != true {
		t.Fatalf("result = %s, err %v", stdout.String(), err)
	}
	if _, err := os.Stat(filepath.Join(home, "refresh-grants", "minimax.json")); err != nil {
		t.Fatalf("canonical store not published: %v", err)
	}
}

func TestBrokerOnboardRuntimeAuthVerifierFailurePublishesNothing(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MC_HOME", home)
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CODEX_ACCESS_TOKEN", "CODEX_API_KEY", "MINIMAX_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(key, "")
	}
	original := productionRuntimeAuthVerifier
	t.Cleanup(func() { productionRuntimeAuthVerifier = original })
	productionRuntimeAuthVerifier = deployment.RuntimeAuthVerifyFunc(func(binding, _ string) error {
		if binding == "claude" {
			return errors.New("provider rejected no-op")
		}
		return nil
	})
	var stdout, stderr bytes.Buffer
	if code := brokerOnboardRuntimeAuth(runtimeAuthBrokerArgs(t, home), &stdout, &stderr); code != 1 {
		t.Fatalf("broker exit %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "provider rejected no-op") {
		t.Fatalf("missing gate refusal: %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, "refresh-grants")); !os.IsNotExist(err) {
		t.Fatalf("failed gate published grants: %v", err)
	}
}
