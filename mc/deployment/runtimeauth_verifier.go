package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mc/routing"
)

const (
	runtimeAuthNoopPrompt = "Reply with exactly MC_RUNTIME_AUTH_OK. Do not use tools."
	runtimeAuthImage      = "mc-prod"
	codexDummyRefresh     = "mc-projected-dummy-refresh"
)

type runtimeAccessCredential struct {
	AccessToken string
	IDToken     string
	AccountID   string
}

type runtimeAuthMintFunc func(binding, stagedGrantDir string) (runtimeAccessCredential, error)
type runtimeAuthRunFunc func(argv []string, stdin string) ([]byte, error)

// AdapterNoopVerifier spends one tiny provider turn through the same image,
// adapter source, credential channel, and native-session path used by a real
// run. RunnerSourceDir is an installed release asset, never a provider home.
type AdapterNoopVerifier struct {
	Image           string
	RunnerSourceDir string
	Mint            runtimeAuthMintFunc
	Run             runtimeAuthRunFunc
}

func NewAdapterNoopVerifier(home string) *AdapterNoopVerifier {
	return &AdapterNoopVerifier{
		Image:           runtimeAuthImage,
		RunnerSourceDir: filepath.Join(home, "release", "runner"),
	}
}

func defaultRuntimeAuthRun(argv []string, stdin string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", argv...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("adapter no-op exceeded two minutes")
	}
	return out, err
}

func mintRuntimeAccess(binding, stagedGrantDir string) (runtimeAccessCredential, error) {
	path := filepath.Join(stagedGrantDir, binding+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		return runtimeAccessCredential{}, fmt.Errorf("read staged grant: %w", err)
	}
	var grant oauthRuntimeGrant
	if err := decodeRuntimeAuthJSON(body, &grant); err != nil {
		return runtimeAccessCredential{}, fmt.Errorf("parse staged OAuth grant: %w", err)
	}
	if grant.Binding != binding || grant.RefreshToken == "" {
		return runtimeAccessCredential{}, fmt.Errorf("staged OAuth grant identity mismatch")
	}
	request := map[string]string{
		"grant_type": "refresh_token", "client_id": grant.ClientID,
		"refresh_token": grant.RefreshToken,
	}
	if grant.Scope != "" {
		request["scope"] = grant.Scope
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return runtimeAccessCredential{}, err
	}
	req, err := http.NewRequest(http.MethodPost, grant.TokenURL, bytes.NewReader(requestBody))
	if err != nil {
		return runtimeAccessCredential{}, err
	}
	req.Header.Set("content-type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return runtimeAccessCredential{}, fmt.Errorf("token endpoint: %w", err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, maxRuntimeAuthSourceBytes+1))
	if err != nil || len(responseBody) > maxRuntimeAuthSourceBytes {
		return runtimeAccessCredential{}, fmt.Errorf("token endpoint returned invalid bounded content")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return runtimeAccessCredential{}, fmt.Errorf("token endpoint refused with HTTP %d", res.StatusCode)
	}
	var response struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil || response.AccessToken == "" {
		return runtimeAccessCredential{}, fmt.Errorf("token endpoint returned no access token")
	}
	if binding == "chatgpt" && response.IDToken == "" {
		return runtimeAccessCredential{}, fmt.Errorf("Codex token endpoint returned no id token")
	}
	if response.RefreshToken != "" && response.RefreshToken != grant.RefreshToken {
		grant.RefreshToken = response.RefreshToken
		if err := replaceRuntimeGrant(path, grant); err != nil {
			return runtimeAccessCredential{}, fmt.Errorf("persist rotated staged grant: %w", err)
		}
	}
	return runtimeAccessCredential{AccessToken: response.AccessToken, IDToken: response.IDToken, AccountID: grant.AccountID}, nil
}

func replaceRuntimeGrant(path string, grant any) error {
	body, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	next := path + ".next"
	f, err := os.OpenFile(next, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(next)
		return err
	}
	if err := os.Rename(next, path); err != nil {
		_ = os.Remove(next)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func codexProjectedAuth(credential runtimeAccessCredential) ([]byte, error) {
	if credential.AccessToken == "" || credential.IDToken == "" || credential.AccountID == "" {
		return nil, fmt.Errorf("Codex projection requires access/id/account evidence")
	}
	return json.MarshalIndent(map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"access_token": credential.AccessToken, "id_token": credential.IDToken,
			"refresh_token": codexDummyRefresh, "account_id": credential.AccountID,
		},
		"last_refresh": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
}

func sessionStartLocator(output []byte) (string, bool) {
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		var event struct {
			Event      string `json:"event"`
			SessionID  string `json:"session_id"`
			NativeFile string `json:"native_file"`
		}
		if json.Unmarshal(line, &event) == nil && event.Event == "session-start" && event.SessionID != "" && event.NativeFile != "" {
			if filepath.IsAbs(event.NativeFile) || strings.Contains(event.NativeFile, "\\") {
				return "", false
			}
			clean := filepath.Clean(event.NativeFile)
			if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean != event.NativeFile {
				return "", false
			}
			return clean, true
		}
	}
	return "", false
}

func (v *AdapterNoopVerifier) Verify(binding, stagedGrantDir string) error {
	spec, ok := routingProductionBinding(binding)
	if !ok {
		return fmt.Errorf("unknown production binding %q", binding)
	}
	if err := requireOwnerOnlyDirectory(v.RunnerSourceDir, "installed runner source"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(v.RunnerSourceDir, "agent-runner", "adapters", "codex.ts")); err != nil {
		return fmt.Errorf("installed runner source is incomplete: %w", err)
	}
	root, err := os.MkdirTemp(filepath.Dir(stagedGrantDir), ".runtime-auth-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	sessionDir, workspaceDir := filepath.Join(root, "session"), filepath.Join(root, "workspace")
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(workspaceDir, 0o700); err != nil {
		return err
	}
	image := v.Image
	if image == "" {
		image = runtimeAuthImage
	}
	run := v.Run
	if run == nil {
		run = defaultRuntimeAuthRun
	}
	argv := []string{
		"run", "--rm", "-i", "--label", "mc-managed=true", "--label", "mc-tier=runtime-auth-verify",
		"--security-opt", "seccomp=unconfined",
		"-v", sessionDir + ":/mc/session", "-v", workspaceDir + ":/workspace/source",
		"-v", v.RunnerSourceDir + ":/app/src:ro",
	}
	adapter := "claude.ts"
	adapterArgs := []string{"--session-dir", "/mc/session", "--workspace", "/workspace/source", "--binding", binding}
	if spec.Channel == "static" {
		body, err := os.ReadFile(filepath.Join(stagedGrantDir, binding+".json"))
		if err != nil {
			return err
		}
		var grant staticRuntimeGrant
		if err := decodeRuntimeAuthJSON(body, &grant); err != nil || grant.Binding != binding || grant.EnvName != spec.DeclaredStaticKey || grant.Secret == "" {
			return fmt.Errorf("staged static grant identity mismatch")
		}
		argv = append(argv, "-e", grant.EnvName+"="+grant.Secret)
	} else {
		mint := v.Mint
		if mint == nil {
			mint = mintRuntimeAccess
		}
		credential, err := mint(binding, stagedGrantDir)
		if err != nil {
			return err
		}
		if binding == "chatgpt" {
			adapter = "codex.ts"
			adapterArgs = []string{"--session-dir", "/mc/session", "--workspace", "/workspace/source", "--mode", "fresh"}
			auth, err := codexProjectedAuth(credential)
			if err != nil {
				return err
			}
			authPath := filepath.Join(root, "codex-auth.json")
			if err := os.WriteFile(authPath, append(auth, '\n'), 0o600); err != nil {
				return err
			}
			argv = append(argv,
				"--add-host", "host.docker.internal:host-gateway",
				"-v", authPath+":/mc/codex/auth.json",
				"-v", sessionDir+":/mc/codex/sessions",
				"-e", "CODEX_HOME=/mc/codex",
				"-e", "CODEX_REFRESH_TOKEN_URL_OVERRIDE=http://host.docker.internal:9/oauth/token")
		} else {
			argv = append(argv,
				"-e", "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=1",
				"-e", "CLAUDE_CODE_OAUTH_TOKEN="+credential.AccessToken)
		}
	}
	argv = append(argv, image, "bun", "/app/src/agent-runner/adapters/"+adapter)
	argv = append(argv, adapterArgs...)
	out, err := run(argv, runtimeAuthNoopPrompt)
	if err != nil {
		return fmt.Errorf("production adapter exited nonzero")
	}
	locator, ok := sessionStartLocator(out)
	if !ok {
		return fmt.Errorf("production adapter returned no durable session-start")
	}
	info, err := os.Lstat(filepath.Join(sessionDir, locator))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("production adapter did not persist its registered native trace")
	}
	return nil
}

// Isolated for tests and to keep routing's mutable clone semantics at the
// verifier boundary without exporting its internal map.
func routingProductionBinding(binding string) (struct {
	Channel           string
	DeclaredStaticKey string
}, bool) {
	spec, ok := routing.ProductionBinding(binding)
	return struct {
		Channel           string
		DeclaredStaticKey string
	}{spec.Channel, spec.DeclaredStaticKey}, ok
}
