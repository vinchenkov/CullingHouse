package deployment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"mc/routing"
)

const maxRuntimeAuthSourceBytes = 1 << 20

type RuntimeAuthSources struct {
	Bindings              []string
	CodexAuthFile         string
	ClaudeCredentialsFile string
	MinimaxTokenFile      string
	Environment           []string
}

type RuntimeAuthVerifier interface {
	Verify(binding, stagedGrantDir string) error
}

type RuntimeAuthVerifyFunc func(binding, stagedGrantDir string) error

func (f RuntimeAuthVerifyFunc) Verify(binding, stagedGrantDir string) error {
	return f(binding, stagedGrantDir)
}

type codexAuthSource struct {
	AuthMode     string  `json:"auth_mode"`
	OpenAIAPIKey *string `json:"OPENAI_API_KEY,omitempty"`
	Tokens       struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh,omitempty"`
}

type claudeCredentialsSource struct {
	ClaudeAIOAuth struct {
		AccessToken           string   `json:"accessToken"`
		RefreshToken          string   `json:"refreshToken"`
		ExpiresAt             int64    `json:"expiresAt"`
		RefreshTokenExpiresAt int64    `json:"refreshTokenExpiresAt,omitempty"`
		Scopes                []string `json:"scopes"`
		SubscriptionType      string   `json:"subscriptionType,omitempty"`
		RateLimitTier         string   `json:"rateLimitTier,omitempty"`
	} `json:"claudeAiOauth"`
}

type oauthRuntimeGrant struct {
	Binding      string `json:"binding"`
	Channel      string `json:"channel"`
	TokenURL     string `json:"token_url"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type staticRuntimeGrant struct {
	Binding string `json:"binding"`
	Channel string `json:"channel"`
	EnvName string `json:"env_name"`
	Secret  string `json:"secret"`
}

func statOwner(info os.FileInfo) (uint32, uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, uint64(st.Nlink), true
}

func requireOwnerOnlyDirectory(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s %q: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %q must not be a symlink", label, path)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s %q must be an owner-only directory with mode 0700", label, path)
	}
	uid, _, ok := statOwner(info)
	if !ok || int(uid) != os.Getuid() {
		return fmt.Errorf("%s %q must be owned by the current operator", label, path)
	}
	return nil
}

func readSecretSource(path, label string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("%s source file is required", label)
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s source %q: %w", label, path, err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s source %q must not be a symlink", label, path)
	}
	if !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%s source %q must be a regular file with mode 0600", label, path)
	}
	uid, links, ok := statOwner(before)
	if !ok || int(uid) != os.Getuid() || links != 1 {
		return nil, fmt.Errorf("%s source %q must be singly linked and owned by the current operator", label, path)
	}
	if before.Size() <= 0 || before.Size() > maxRuntimeAuthSourceBytes {
		return nil, fmt.Errorf("%s source %q has invalid size", label, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s source %q: %w", label, path, err)
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("%s source %q changed during inspection", label, path)
	}
	body, err := io.ReadAll(io.LimitReader(f, maxRuntimeAuthSourceBytes+1))
	if err != nil || len(body) > maxRuntimeAuthSourceBytes {
		return nil, fmt.Errorf("read %s source %q: invalid bounded content", label, path)
	}
	return body, nil
}

func normalizeIsolatedSourcePath(home, path, label string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s source file is required", label)
	}
	root := filepath.Join(home, "runtime-auth-sources")
	if err := requireOwnerOnlyDirectory(root, "runtime-auth source root"); err != nil {
		return "", err
	}
	parent, err := CanonicalHome(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("resolve %s source parent: %w", label, err)
	}
	normalized := filepath.Join(parent, filepath.Base(path))
	rel, err := filepath.Rel(root, normalized)
	if err != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s source must live in the isolated MC_HOME/runtime-auth-sources tree", label)
	}
	for dir := filepath.Dir(normalized); dir != filepath.Dir(root); dir = filepath.Dir(dir) {
		if err := requireOwnerOnlyDirectory(dir, "runtime-auth source directory"); err != nil {
			return "", err
		}
		if dir == root {
			break
		}
	}
	return normalized, nil
}

func decodeRuntimeAuthJSON(body []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func validateRuntimeBindings(bindings []string) ([]string, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("at least one runtime binding is required")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if _, ok := routing.ProductionBinding(binding); !ok {
			return nil, fmt.Errorf("unknown runtime binding %q", binding)
		}
		if seen[binding] {
			return nil, fmt.Errorf("duplicate runtime binding %q", binding)
		}
		seen[binding] = true
		out = append(out, binding)
	}
	sort.Strings(out)
	return out, nil
}

func validateRuntimeAuthEnvironment(environment []string) error {
	known := map[string]bool{
		"ANTHROPIC_API_KEY": true, "ANTHROPIC_AUTH_TOKEN": true,
		"CLAUDE_CODE_OAUTH_TOKEN": true, "CODEX_ACCESS_TOKEN": true,
		"CODEX_API_KEY": true, "OPENAI_API_KEY": true,
	}
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		if known[name] {
			return fmt.Errorf("forbidden ambient runtime credential %s is set; use an owner-only source file", name)
		}
	}
	return nil
}

func buildRuntimeGrant(binding string, sources RuntimeAuthSources) (any, error) {
	spec, _ := routing.ProductionBinding(binding)
	switch binding {
	case "chatgpt":
		body, err := readSecretSource(sources.CodexAuthFile, "Codex auth")
		if err != nil {
			return nil, err
		}
		var source codexAuthSource
		if err := decodeRuntimeAuthJSON(body, &source); err != nil {
			return nil, fmt.Errorf("parse Codex auth source: %w", err)
		}
		if source.AuthMode != "chatgpt" || source.OpenAIAPIKey != nil && *source.OpenAIAPIKey != "" ||
			source.Tokens.AccessToken == "" || source.Tokens.IDToken == "" || source.Tokens.RefreshToken == "" || source.Tokens.AccountID == "" {
			return nil, fmt.Errorf("Codex auth source must be ChatGPT OAuth with access/id/refresh/account evidence and no OPENAI_API_KEY")
		}
		return oauthRuntimeGrant{Binding: binding, Channel: spec.Channel, TokenURL: spec.TokenURL,
			ClientID: spec.ClientID, RefreshToken: source.Tokens.RefreshToken, AccountID: source.Tokens.AccountID}, nil
	case "claude":
		body, err := readSecretSource(sources.ClaudeCredentialsFile, "Claude credentials")
		if err != nil {
			return nil, err
		}
		var source claudeCredentialsSource
		if err := decodeRuntimeAuthJSON(body, &source); err != nil {
			return nil, fmt.Errorf("parse Claude credentials source: %w", err)
		}
		oauth := source.ClaudeAIOAuth
		hasInference := false
		for _, scope := range oauth.Scopes {
			if scope == "user:inference" {
				hasInference = true
			}
		}
		if oauth.AccessToken == "" || oauth.RefreshToken == "" || oauth.ExpiresAt <= 0 || !hasInference {
			return nil, fmt.Errorf("Claude credentials source needs access/refresh/expiry evidence and user:inference scope")
		}
		return oauthRuntimeGrant{Binding: binding, Channel: spec.Channel, TokenURL: spec.TokenURL,
			ClientID: spec.ClientID, RefreshToken: oauth.RefreshToken, Scope: strings.Join(oauth.Scopes, " ")}, nil
	case "minimax":
		body, err := readSecretSource(sources.MinimaxTokenFile, "MiniMax token")
		if err != nil {
			return nil, err
		}
		secret := strings.TrimSpace(string(body))
		if secret == "" || len(secret) > 16*1024 || !utf8.ValidString(secret) || strings.IndexFunc(secret, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
			return nil, fmt.Errorf("MiniMax token source must contain one non-empty token without whitespace or controls")
		}
		return staticRuntimeGrant{Binding: binding, Channel: spec.Channel, EnvName: spec.DeclaredStaticKey, Secret: secret}, nil
	}
	return nil, fmt.Errorf("unknown runtime binding %q", binding)
}

func writeRuntimeGrant(dir, binding string, grant any) error {
	body, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	path := filepath.Join(dir, binding+".json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func validateStagedRuntimeGrants(dir string, bindings []string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != len(bindings) {
		return fmt.Errorf("staged grant set changed shape during live verification")
	}
	for i, binding := range bindings {
		entry := entries[i]
		if entry.Name() != binding+".json" || entry.IsDir() {
			return fmt.Errorf("staged grant set contains unexpected entry %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
			return fmt.Errorf("staged grant %s lost its mode-0600 regular-file boundary", binding)
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		spec, _ := routing.ProductionBinding(binding)
		if spec.Delivery == routing.CredentialMaterialized {
			var grant staticRuntimeGrant
			if err := decodeRuntimeAuthJSON(body, &grant); err != nil || grant.Binding != binding ||
				grant.Channel != spec.Channel || grant.EnvName != spec.DeclaredStaticKey || grant.Secret == "" {
				return fmt.Errorf("staged grant %s is no longer the closed static shape", binding)
			}
			continue
		}
		var grant oauthRuntimeGrant
		if err := decodeRuntimeAuthJSON(body, &grant); err != nil || grant.Binding != binding ||
			grant.Channel != spec.Channel || grant.TokenURL != spec.TokenURL || grant.ClientID != spec.ClientID || grant.RefreshToken == "" {
			return fmt.Errorf("staged grant %s is no longer the closed OAuth shape", binding)
		}
		if binding == "chatgpt" && grant.AccountID == "" {
			return fmt.Errorf("staged Codex grant lost its account identity")
		}
		if binding == "claude" && !strings.Contains(" "+grant.Scope+" ", " user:inference ") {
			return fmt.Errorf("staged Claude grant lost user:inference scope")
		}
	}
	return nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func directoriesEqual(left, right string) (bool, error) {
	leftEntries, err := os.ReadDir(left)
	if err != nil {
		return false, err
	}
	rightEntries, err := os.ReadDir(right)
	if err != nil {
		return false, err
	}
	if len(leftEntries) != len(rightEntries) {
		return false, nil
	}
	for i := range leftEntries {
		if leftEntries[i].Name() != rightEntries[i].Name() || leftEntries[i].IsDir() || rightEntries[i].IsDir() {
			return false, nil
		}
		leftBody, err := os.ReadFile(filepath.Join(left, leftEntries[i].Name()))
		if err != nil {
			return false, err
		}
		rightBody, err := os.ReadFile(filepath.Join(right, rightEntries[i].Name()))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(leftBody, rightBody) {
			return false, nil
		}
	}
	return true, nil
}

// ImportRuntimeAuth validates every selected provider-native source, builds
// the resident's closed grant union in a private directory, runs every live
// gate against those staged bytes, and only then publishes the whole store.
func ImportRuntimeAuth(home string, sources RuntimeAuthSources, verifier RuntimeAuthVerifier) (string, error) {
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		return "", fmt.Errorf("resolve runtime-auth MC_HOME: %w", err)
	}
	home = canonicalHome
	if err := requireOwnerOnlyDirectory(home, "MC_HOME"); err != nil {
		return "", err
	}
	bindings, err := validateRuntimeBindings(sources.Bindings)
	if err != nil {
		return "", err
	}
	if err := validateRuntimeAuthEnvironment(sources.Environment); err != nil {
		return "", err
	}
	if verifier == nil {
		return "", fmt.Errorf("runtime-auth live verifier is unavailable")
	}
	enabled := map[string]bool{}
	for _, binding := range bindings {
		enabled[binding] = true
	}
	if enabled["chatgpt"] {
		sources.CodexAuthFile, err = normalizeIsolatedSourcePath(home, sources.CodexAuthFile, "Codex auth")
		if err != nil {
			return "", err
		}
	}
	if enabled["claude"] {
		sources.ClaudeCredentialsFile, err = normalizeIsolatedSourcePath(home, sources.ClaudeCredentialsFile, "Claude credentials")
		if err != nil {
			return "", err
		}
	}
	if enabled["minimax"] {
		sources.MinimaxTokenFile, err = normalizeIsolatedSourcePath(home, sources.MinimaxTokenFile, "MiniMax token")
		if err != nil {
			return "", err
		}
	}
	stage, err := os.MkdirTemp(home, ".runtime-auth-import-")
	if err != nil {
		return "", fmt.Errorf("create runtime-auth staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	for _, binding := range bindings {
		grant, err := buildRuntimeGrant(binding, sources)
		if err != nil {
			return "", fmt.Errorf("runtime binding %s: %w", binding, err)
		}
		if err := writeRuntimeGrant(stage, binding, grant); err != nil {
			return "", fmt.Errorf("stage runtime binding %s: %w", binding, err)
		}
	}
	if err := syncDirectory(stage); err != nil {
		return "", fmt.Errorf("sync runtime-auth staging directory: %w", err)
	}
	for _, binding := range bindings {
		if err := verifier.Verify(binding, stage); err != nil {
			return "", fmt.Errorf("runtime binding %s live no-op: %w", binding, err)
		}
	}
	if err := validateStagedRuntimeGrants(stage, bindings); err != nil {
		return "", fmt.Errorf("revalidate live-verified runtime grants: %w", err)
	}
	if err := syncDirectory(stage); err != nil {
		return "", fmt.Errorf("sync live-verified runtime grants: %w", err)
	}
	canonical := filepath.Join(home, "refresh-grants")
	if err := requireOwnerOnlyDirectory(canonical, "runtime grant store"); err == nil {
		equal, err := directoriesEqual(stage, canonical)
		if err != nil {
			return "", fmt.Errorf("compare runtime grant store: %w", err)
		}
		if equal {
			return "ok", nil
		}
		if err := atomicExchangeDirectories(stage, canonical); err != nil {
			return "", fmt.Errorf("atomically exchange runtime grant store: %w", err)
		}
		if err := syncDirectory(home); err != nil {
			return "", fmt.Errorf("sync MC_HOME after runtime grant exchange: %w", err)
		}
		if err := os.RemoveAll(stage); err != nil {
			return "", fmt.Errorf("remove replaced runtime grant store: %w", err)
		}
		if err := syncDirectory(home); err != nil {
			return "", fmt.Errorf("sync MC_HOME after old grant removal: %w", err)
		}
		return "done", nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(stage, canonical); err != nil {
		return "", fmt.Errorf("publish runtime grant store: %w", err)
	}
	if err := syncDirectory(home); err != nil {
		return "", fmt.Errorf("sync MC_HOME after runtime grant publication: %w", err)
	}
	return "done", nil
}
