package deployment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verifierRunnerSource(t *testing.T, root string) string {
	t.Helper()
	source := filepath.Join(root, "release", "runner")
	if err := os.MkdirAll(filepath.Join(source, "agent-runner", "adapters"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex.ts", "claude.ts"} {
		if err := os.WriteFile(filepath.Join(source, "agent-runner", "adapters", name), []byte("// fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func stagedVerifierGrant(t *testing.T, stage, binding string) {
	t.Helper()
	var grant any
	switch binding {
	case "chatgpt":
		grant = oauthRuntimeGrant{Binding: binding, Channel: "codex", TokenURL: "https://auth.openai.com/oauth/token",
			ClientID: "app_EMoamEEZ73f0CkXaXp7hrann", RefreshToken: "refresh", AccountID: "account"}
	case "claude":
		grant = oauthRuntimeGrant{Binding: binding, Channel: "claude", TokenURL: "https://platform.claude.com/v1/oauth/token",
			ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e", RefreshToken: "refresh", Scope: "user:inference"}
	case "minimax":
		grant = staticRuntimeGrant{Binding: binding, Channel: "static", EnvName: "ANTHROPIC_AUTH_TOKEN", Secret: "minimax-secret"}
	}
	if err := writeRuntimeGrant(stage, binding, grant); err != nil {
		t.Fatal(err)
	}
}

func mountSource(argv []string, target string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] != "-v" {
			continue
		}
		if source, destination, ok := strings.Cut(argv[i+1], ":"); ok && strings.TrimSuffix(destination, ":ro") == target {
			return source
		}
	}
	return ""
}

func TestAdapterNoopVerifierLaunchesEveryClosedBindingThroughProductionAdapter(t *testing.T) {
	root := t.TempDir()
	runner := verifierRunnerSource(t, root)
	stage := filepath.Join(root, "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []string{"chatgpt", "claude", "minimax"} {
		stagedVerifierGrant(t, stage, binding)
		t.Run(binding, func(t *testing.T) {
			var gotArgv []string
			verifier := &AdapterNoopVerifier{
				Image: runtimeAuthImage, RunnerSourceDir: runner,
				Mint: func(got, _ string) (runtimeAccessCredential, error) {
					if got != binding {
						t.Fatalf("mint binding = %q", got)
					}
					return runtimeAccessCredential{AccessToken: "access", IDToken: "id", AccountID: "account"}, nil
				},
				Run: func(argv []string, stdin string) ([]byte, error) {
					gotArgv = append([]string(nil), argv...)
					if stdin != runtimeAuthNoopPrompt {
						t.Fatalf("prompt = %q", stdin)
					}
					session := mountSource(argv, "/mc/session")
					if err := os.WriteFile(filepath.Join(session, "native.jsonl"), []byte("{}\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					return []byte(`{"event":"session-start","session_id":"native","native_file":"native.jsonl"}` + "\n"), nil
				},
			}
			if err := verifier.Verify(binding, stage); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(gotArgv, " ")
			if !strings.Contains(joined, "mc-tier=runtime-auth-verify") || !strings.Contains(joined, runner+":/app/src:ro") ||
				!strings.Contains(joined, "--security-opt seccomp=unconfined") {
				t.Fatalf("launch is missing production boundary: %v", gotArgv)
			}
			switch binding {
			case "chatgpt":
				if !strings.Contains(joined, "adapters/codex.ts") || !strings.Contains(joined, ":/mc/codex/sessions") ||
					!strings.Contains(joined, "CODEX_REFRESH_TOKEN_URL_OVERRIDE=") {
					t.Fatalf("Codex launch = %v", gotArgv)
				}
			case "claude":
				if !strings.Contains(joined, "adapters/claude.ts") || !strings.Contains(joined, "CLAUDE_CODE_OAUTH_TOKEN=access") {
					t.Fatalf("Claude launch = %v", gotArgv)
				}
			case "minimax":
				if !strings.Contains(joined, "--binding minimax") || !strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN=minimax-secret") {
					t.Fatalf("MiniMax launch = %v", gotArgv)
				}
			}
		})
	}
}

func TestAdapterNoopVerifierRequiresTheNativeTraceItRegisters(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	stagedVerifierGrant(t, stage, "minimax")
	verifier := &AdapterNoopVerifier{
		RunnerSourceDir: verifierRunnerSource(t, root),
		Run: func([]string, string) ([]byte, error) {
			return []byte(`{"event":"session-start","session_id":"native","native_file":"missing.jsonl"}` + "\n"), nil
		},
	}
	if err := verifier.Verify("minimax", stage); err == nil || !strings.Contains(err.Error(), "did not persist") {
		t.Fatalf("missing native trace error = %v", err)
	}
}

func TestMintRuntimeAccessDurablyAdoptsProviderRotationInTheStage(t *testing.T) {
	var request map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"rotated-refresh"}`))
	}))
	defer server.Close()
	stage := t.TempDir()
	grant := oauthRuntimeGrant{Binding: "claude", Channel: "claude", TokenURL: server.URL,
		ClientID: "client", RefreshToken: "old-refresh", Scope: "user:inference"}
	if err := writeRuntimeGrant(stage, "claude", grant); err != nil {
		t.Fatal(err)
	}
	credential, err := mintRuntimeAccess("claude", stage)
	if err != nil || credential.AccessToken != "fresh-access" {
		t.Fatalf("mint = %#v, %v", credential, err)
	}
	if request["refresh_token"] != "old-refresh" || request["scope"] != "user:inference" {
		t.Fatalf("request = %#v", request)
	}
	body, err := os.ReadFile(filepath.Join(stage, "claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rotated oauthRuntimeGrant
	if err := decodeRuntimeAuthJSON(body, &rotated); err != nil || rotated.RefreshToken != "rotated-refresh" {
		t.Fatalf("rotated grant = %#v, %v", rotated, err)
	}
}

func TestImportRuntimeAuthRejectsVerifierMutationOutsideTheClosedGrantSet(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	sources := validRuntimeAuthSources(t, home)
	_, err := ImportRuntimeAuth(home, sources, RuntimeAuthVerifyFunc(func(binding, stage string) error {
		if binding == "minimax" {
			return os.WriteFile(filepath.Join(stage, "smuggled.json"), []byte("{}\n"), 0o600)
		}
		return nil
	}))
	if err == nil || !strings.Contains(err.Error(), "changed shape") {
		t.Fatalf("mutation error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "refresh-grants")); !os.IsNotExist(statErr) {
		t.Fatalf("mutated stage was published: %v", statErr)
	}
}
