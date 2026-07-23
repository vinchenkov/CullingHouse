package deployment

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeSecretFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validRuntimeAuthSources(t *testing.T, home string) RuntimeAuthSources {
	t.Helper()
	sourceRoot := filepath.Join(home, "runtime-auth-sources")
	if err := os.Mkdir(sourceRoot, 0o700); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	sources, err := os.MkdirTemp(sourceRoot, "provider-homes-")
	if err != nil {
		t.Fatal(err)
	}
	return RuntimeAuthSources{
		Bindings: []string{"chatgpt", "claude", "minimax"},
		CodexAuthFile: writeSecretFile(t, sources, "auth.json", `{
  "auth_mode":"chatgpt",
  "OPENAI_API_KEY":null,
  "tokens":{"access_token":"codex-access","id_token":"codex-id","refresh_token":"codex-refresh","account_id":"acct-1"},
  "last_refresh":"2026-07-22T00:00:00Z"
}`),
		ClaudeCredentialsFile: writeSecretFile(t, sources, ".credentials.json", `{
  "claudeAiOauth":{"accessToken":"claude-access","refreshToken":"claude-refresh","expiresAt":9999999999999,"refreshTokenExpiresAt":9999999999999,"scopes":["user:inference"],"subscriptionType":"max"}
}`),
		MinimaxTokenFile: writeSecretFile(t, sources, "minimax-token", "minimax-secret\n"),
	}
}

func readGrantMap(t *testing.T, dir string) map[string]map[string]any {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			t.Fatalf("unexpected runtime grant entry %q", entry.Name())
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var grant map[string]any
		if err := json.Unmarshal(body, &grant); err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(entry.Name(), ".json")] = grant
		if info, err := entry.Info(); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("grant %s mode = %v, err %v", entry.Name(), info.Mode().Perm(), err)
		}
	}
	return out
}

func TestImportRuntimeAuthPublishesAllBindingsInOneExchange(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(home, "refresh-grants")
	if err := os.Mkdir(canonical, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "old.json"), []byte("old-owner\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	seen := []string{}
	verifier := RuntimeAuthVerifyFunc(func(binding, stagedDir string) error {
		seen = append(seen, binding)
		if body, err := os.ReadFile(filepath.Join(canonical, "old.json")); err != nil || string(body) != "old-owner\n" {
			t.Fatalf("canonical store changed before all gates: body=%q err=%v", body, err)
		}
		if _, err := os.Stat(filepath.Join(stagedDir, binding+".json")); err != nil {
			t.Fatalf("verifier cannot inspect staged %s grant: %v", binding, err)
		}
		return nil
	})
	status, err := ImportRuntimeAuth(home, validRuntimeAuthSources(t, home), verifier)
	if err != nil || status != "done" {
		t.Fatalf("import = status %q err %v", status, err)
	}
	if !reflect.DeepEqual(seen, []string{"chatgpt", "claude", "minimax"}) {
		t.Fatalf("verification order = %v", seen)
	}
	grants := readGrantMap(t, canonical)
	if got := grants["chatgpt"]; got["channel"] != "codex" || got["refresh_token"] != "codex-refresh" || got["account_id"] != "acct-1" {
		t.Fatalf("codex grant = %#v", got)
	}
	if got := grants["claude"]; got["channel"] != "claude" || got["refresh_token"] != "claude-refresh" || got["scope"] != "user:inference" {
		t.Fatalf("claude grant = %#v", got)
	}
	if got := grants["minimax"]; got["channel"] != "static" || got["env_name"] != "ANTHROPIC_AUTH_TOKEN" || got["secret"] != "minimax-secret" {
		t.Fatalf("minimax grant = %#v", got)
	}
	if _, exists := grants["old"]; exists {
		t.Fatal("old grant survived atomic replacement")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"refresh-grants", "runtime-auth-sources"}) {
		t.Fatalf("secret staging residue under MC_HOME: %v", names)
	}
}

func TestImportRuntimeAuthGateFailureLeavesCanonicalBytesUntouched(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	canonical := filepath.Join(home, "refresh-grants")
	if err := os.MkdirAll(canonical, 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte("existing-canonical-owner\n")
	if err := os.WriteFile(filepath.Join(canonical, "owner.json"), before, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ImportRuntimeAuth(home, validRuntimeAuthSources(t, home), RuntimeAuthVerifyFunc(func(binding, _ string) error {
		if binding == "claude" {
			return errors.New("live no-op refused")
		}
		return nil
	}))
	if err == nil || !strings.Contains(err.Error(), "claude") || !strings.Contains(err.Error(), "live no-op refused") {
		t.Fatalf("gate failure = %v", err)
	}
	after, readErr := os.ReadFile(filepath.Join(canonical, "owner.json"))
	if readErr != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("canonical changed after failed gate: %q err %v", after, readErr)
	}
	entries, readErr := os.ReadDir(home)
	if readErr != nil || len(entries) != 2 {
		t.Fatalf("failed import left staging residue: entries=%v err=%v", entries, readErr)
	}
}

func TestImportRuntimeAuthHealthyReplayRunsGatesWithoutReplacingTheStore(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	sources := validRuntimeAuthSources(t, home)
	verifications := 0
	verifier := RuntimeAuthVerifyFunc(func(string, string) error { verifications++; return nil })
	if status, err := ImportRuntimeAuth(home, sources, verifier); err != nil || status != "done" {
		t.Fatalf("first import = %q, %v", status, err)
	}
	canonical := filepath.Join(home, "refresh-grants")
	before, err := os.Stat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	sources = validRuntimeAuthSources(t, home)
	if status, err := ImportRuntimeAuth(home, sources, verifier); err != nil || status != "ok" {
		t.Fatalf("replay = %q, %v", status, err)
	}
	after, err := os.Stat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("healthy replay replaced the canonical grant directory")
	}
	if verifications != 6 {
		t.Fatalf("healthy replay skipped live gates: %d calls", verifications)
	}
}

func TestImportRuntimeAuthRejectsUnsafeSourcesAndBindingSets(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(t *testing.T, root string, sources *RuntimeAuthSources)
		want   string
	}{
		"duplicate binding": {func(_ *testing.T, _ string, s *RuntimeAuthSources) { s.Bindings = []string{"chatgpt", "chatgpt"} }, "duplicate runtime binding"},
		"unknown binding":   {func(_ *testing.T, _ string, s *RuntimeAuthSources) { s.Bindings = []string{"unknown"} }, "unknown runtime binding"},
		"metered codex fallback": {func(t *testing.T, _ string, s *RuntimeAuthSources) {
			s.CodexAuthFile = writeSecretFile(t, filepath.Dir(s.CodexAuthFile), "metered.json", `{"auth_mode":"chatgpt","OPENAI_API_KEY":"metered","tokens":{"access_token":"a","id_token":"i","refresh_token":"r","account_id":"x"}}`)
		}, "OPENAI_API_KEY"},
		"missing claude scope": {func(t *testing.T, _ string, s *RuntimeAuthSources) {
			s.ClaudeCredentialsFile = writeSecretFile(t, filepath.Dir(s.ClaudeCredentialsFile), "scopeless.json", `{"claudeAiOauth":{"accessToken":"a","refreshToken":"r","expiresAt":9999999999999,"scopes":["profile"]}}`)
		}, "user:inference"},
		"group-readable token": {func(t *testing.T, _ string, s *RuntimeAuthSources) {
			if err := os.Chmod(s.MinimaxTokenFile, 0o640); err != nil {
				t.Fatal(err)
			}
		}, "mode 0600"},
		"symlink source": {func(t *testing.T, root string, s *RuntimeAuthSources) {
			link := filepath.Join(filepath.Dir(s.CodexAuthFile), "codex-link")
			if err := os.Symlink(s.CodexAuthFile, link); err != nil {
				t.Fatal(err)
			}
			s.CodexAuthFile = link
		}, "symlink"},
		"ambient provider key": {func(_ *testing.T, _ string, s *RuntimeAuthSources) {
			s.Environment = []string{"PATH=/bin", "OPENAI_API_KEY=metered"}
		}, "OPENAI_API_KEY"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			sources := validRuntimeAuthSources(t, home)
			tc.mutate(t, root, &sources)
			_, err := ImportRuntimeAuth(home, sources, RuntimeAuthVerifyFunc(func(string, string) error { return nil }))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if _, statErr := os.Stat(filepath.Join(home, "refresh-grants")); !os.IsNotExist(statErr) {
				t.Fatalf("refusal published a canonical store: %v", statErr)
			}
		})
	}
}
