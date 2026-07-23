package deployment

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func envValue(environment []string, name string) string {
	for _, entry := range environment {
		if got, value, ok := strings.Cut(entry, "="); ok && got == name {
			return value
		}
	}
	return ""
}

func TestAcquireRuntimeAuthUsesOnlyIsolatedSubscriptionHomesAndCleansThem(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	type call struct {
		binary string
		args   []string
		env    []string
	}
	calls := []call{}
	login := func(binary string, args, environment []string, _ io.Reader, _ io.Writer) error {
		calls = append(calls, call{binary, append([]string(nil), args...), append([]string(nil), environment...)})
		switch binary {
		case "codex":
			path := filepath.Join(envValue(environment, "CODEX_HOME"), "auth.json")
			return os.WriteFile(path, []byte("codex-auth\n"), 0o600)
		case "claude":
			path := filepath.Join(envValue(environment, "CLAUDE_CONFIG_DIR"), ".credentials.json")
			return os.WriteFile(path, []byte("claude-auth\n"), 0o600)
		}
		return errors.New("unexpected binary")
	}
	acquisition, err := AcquireRuntimeAuth(home, []string{"claude", "chatgpt"}, nil, io.Discard,
		[]string{"PATH=/bin", "HOME=/personal", "ANTHROPIC_BASE_URL=https://redirect.invalid", "LANG=en_US.UTF-8"}, login)
	if err != nil {
		t.Fatal(err)
	}
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].binary != "codex" || calls[1].binary != "claude" ||
		!reflect.DeepEqual(calls[0].args, []string{"login"}) ||
		!reflect.DeepEqual(calls[1].args, []string{"auth", "login", "--claudeai"}) {
		t.Fatalf("login calls = %#v", calls)
	}
	for _, got := range calls {
		joined := strings.Join(got.env, "\n")
		if strings.Contains(joined, "/personal") || strings.Contains(joined, "ANTHROPIC_BASE_URL") {
			t.Fatalf("personal or redirecting environment reached %s: %v", got.binary, got.env)
		}
		if envValue(got.env, "HOME") == "" || !strings.HasPrefix(envValue(got.env, "HOME"), filepath.Join(canonicalHome, "runtime-auth-sources")) {
			t.Fatalf("%s HOME = %q", got.binary, envValue(got.env, "HOME"))
		}
	}
	flowRoot := acquisition.flowRoot
	if acquisition.CodexAuthFile == "" || acquisition.ClaudeCredentialsFile == "" {
		t.Fatalf("acquired paths = %#v", acquisition)
	}
	if err := acquisition.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flowRoot); !os.IsNotExist(err) {
		t.Fatalf("provider flow survived cleanup: %v", err)
	}
	if info, err := os.Stat(filepath.Join(home, "runtime-auth-sources")); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("source root = %v, %v", info, err)
	}
}

func TestAcquireRuntimeAuthRefusesAmbientCredentialBeforeWritingOrLogin(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := AcquireRuntimeAuth(home, []string{"chatgpt"}, nil, io.Discard,
		[]string{"OPENAI_API_KEY=metered"}, func(string, []string, []string, io.Reader, io.Writer) error {
			called = true
			return nil
		})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") || called {
		t.Fatalf("ambient refusal = %v, called=%v", err, called)
	}
	if _, statErr := os.Stat(filepath.Join(home, "runtime-auth-sources")); !os.IsNotExist(statErr) {
		t.Fatalf("ambient refusal wrote source root: %v", statErr)
	}
}

func TestAcquireRuntimeAuthCleansFailedProviderFlow(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := AcquireRuntimeAuth(home, []string{"chatgpt"}, nil, io.Discard, []string{"PATH=/bin"},
		func(string, []string, []string, io.Reader, io.Writer) error { return errors.New("consent refused") })
	if err == nil || !strings.Contains(err.Error(), "consent refused") {
		t.Fatalf("login failure = %v", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(home, "runtime-auth-sources"))
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("failed flow residue = %v, %v", entries, readErr)
	}
}

func TestAcquireRuntimeAuthSurfacesARefusedCleanupRace(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "do-not-remove")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := AcquireRuntimeAuth(home, []string{"chatgpt"}, nil, io.Discard, []string{"PATH=/bin"},
		func(_ string, _ []string, environment []string, _ io.Reader, _ io.Writer) error {
			flowRoot := filepath.Dir(filepath.Dir(envValue(environment, "CODEX_HOME")))
			if err := os.RemoveAll(flowRoot); err != nil {
				return err
			}
			if err := os.Symlink(target, flowRoot); err != nil {
				return err
			}
			return errors.New("login interrupted")
		})
	if err == nil || !strings.Contains(err.Error(), "cleanup also failed") || !strings.Contains(err.Error(), "changed flow root") {
		t.Fatalf("cleanup-race error = %v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("cleanup race touched foreign target: %v", statErr)
	}
}
