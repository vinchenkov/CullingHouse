package deployment

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RuntimeAuthLoginFunc func(binary string, args, environment []string, stdin io.Reader, log io.Writer) error

type RuntimeAuthAcquisition struct {
	CodexAuthFile         string
	ClaudeCredentialsFile string
	sourceRoot            string
	flowRoot              string
}

func defaultRuntimeAuthLogin(binary string, args, environment []string, stdin io.Reader, log io.Writer) error {
	path, err := exec.LookPath(binary)
	if err != nil {
		return fmt.Errorf("provider login executable %q is unavailable: %w", binary, err)
	}
	cmd := exec.Command(path, args...)
	cmd.Env = environment
	cmd.Stdin = stdin
	if log == nil {
		log = io.Discard
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("provider-owned %s login failed: %w", binary, err)
	}
	return nil
}

func isolatedLoginBaseEnvironment(base []string) []string {
	allowed := map[string]bool{
		"ALL_PROXY": true, "HTTP_PROXY": true, "HTTPS_PROXY": true,
		"NO_PROXY": true, "all_proxy": true, "http_proxy": true,
		"https_proxy": true, "no_proxy": true,
		"LANG": true, "LC_ALL": true, "LC_CTYPE": true,
		"NODE_EXTRA_CA_CERTS": true, "PATH": true,
		"SSL_CERT_DIR": true, "SSL_CERT_FILE": true,
		"TERM": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	}
	out := make([]string, 0, len(allowed)+2)
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if ok && (allowed[name] || strings.HasPrefix(name, "LC_")) {
			out = append(out, entry)
		}
	}
	return out
}

func appendLoginHome(base []string, home, configName, configPath string) []string {
	out := append([]string(nil), base...)
	out = append(out, "HOME="+home, configName+"="+configPath)
	return out
}

func (a *RuntimeAuthAcquisition) Cleanup() error {
	if a == nil || a.flowRoot == "" {
		return nil
	}
	parent := filepath.Dir(a.flowRoot)
	if parent != a.sourceRoot || !strings.HasPrefix(filepath.Base(a.flowRoot), ".provider-login-") {
		return fmt.Errorf("refuse unsafe runtime-auth acquisition cleanup target %q", a.flowRoot)
	}
	info, err := os.Lstat(a.flowRoot)
	if os.IsNotExist(err) {
		a.flowRoot = ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect runtime-auth acquisition cleanup target: %w", err)
	}
	uid, _, ok := statOwner(info)
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || int(uid) != os.Getuid() {
		return fmt.Errorf("refuse runtime-auth acquisition cleanup of a changed flow root")
	}
	if err := os.RemoveAll(a.flowRoot); err != nil {
		return fmt.Errorf("remove runtime-auth acquisition flow: %w", err)
	}
	if err := syncDirectory(a.sourceRoot); err != nil {
		return fmt.Errorf("sync runtime-auth source root after cleanup: %w", err)
	}
	a.flowRoot = ""
	return nil
}

// AcquireRuntimeAuth runs only provider-owned subscription login commands in
// disposable homes beneath MC_HOME. It never reads or copies ~/.codex,
// ~/.claude, API keys, or provider endpoint overrides.
func AcquireRuntimeAuth(home string, bindings []string, stdin io.Reader, log io.Writer, environment []string, login RuntimeAuthLoginFunc) (result *RuntimeAuthAcquisition, retErr error) {
	canonicalHome, err := CanonicalHome(home)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime-auth acquisition MC_HOME: %w", err)
	}
	if err := requireOwnerOnlyDirectory(canonicalHome, "MC_HOME"); err != nil {
		return nil, err
	}
	bindings, err = validateRuntimeBindings(bindings)
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeAuthEnvironment(environment); err != nil {
		return nil, err
	}
	wantsCodex, wantsClaude := false, false
	for _, binding := range bindings {
		wantsCodex = wantsCodex || binding == "chatgpt"
		wantsClaude = wantsClaude || binding == "claude"
	}
	if !wantsCodex && !wantsClaude {
		return nil, fmt.Errorf("runtime-auth acquisition requires at least one OAuth subscription binding")
	}
	sourceRoot := filepath.Join(canonicalHome, "runtime-auth-sources")
	if err := os.Mkdir(sourceRoot, 0o700); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("create runtime-auth source root: %w", err)
	}
	if err := requireOwnerOnlyDirectory(sourceRoot, "runtime-auth source root"); err != nil {
		return nil, err
	}
	flowRoot, err := os.MkdirTemp(sourceRoot, ".provider-login-")
	if err != nil {
		return nil, fmt.Errorf("create isolated provider-login flow: %w", err)
	}
	acquisition := &RuntimeAuthAcquisition{sourceRoot: sourceRoot, flowRoot: flowRoot}
	success := false
	defer func() {
		if !success {
			if cleanupErr := acquisition.Cleanup(); cleanupErr != nil {
				if retErr != nil {
					retErr = fmt.Errorf("%v; isolated provider-login cleanup also failed: %w", retErr, cleanupErr)
				} else {
					retErr = cleanupErr
				}
			}
		}
	}()
	base := isolatedLoginBaseEnvironment(environment)
	if login == nil {
		login = defaultRuntimeAuthLogin
	}
	if wantsCodex {
		flowHome := filepath.Join(flowRoot, "chatgpt", "home")
		config := filepath.Join(flowRoot, "chatgpt", "codex")
		if err := os.MkdirAll(flowHome, 0o700); err != nil {
			return nil, fmt.Errorf("create isolated Codex home: %w", err)
		}
		if err := os.Mkdir(config, 0o700); err != nil {
			return nil, fmt.Errorf("create isolated Codex config: %w", err)
		}
		if err := login("codex", []string{"login"},
			appendLoginHome(base, flowHome, "CODEX_HOME", config), stdin, log); err != nil {
			return nil, err
		}
		acquisition.CodexAuthFile = filepath.Join(config, "auth.json")
		if _, err := readSecretSource(acquisition.CodexAuthFile, "acquired Codex auth"); err != nil {
			return nil, err
		}
	}
	if wantsClaude {
		flowHome := filepath.Join(flowRoot, "claude", "home")
		config := filepath.Join(flowRoot, "claude", "config")
		if err := os.MkdirAll(flowHome, 0o700); err != nil {
			return nil, fmt.Errorf("create isolated Claude home: %w", err)
		}
		if err := os.Mkdir(config, 0o700); err != nil {
			return nil, fmt.Errorf("create isolated Claude config: %w", err)
		}
		if err := login("claude", []string{"auth", "login", "--claudeai"},
			appendLoginHome(base, flowHome, "CLAUDE_CONFIG_DIR", config), stdin, log); err != nil {
			return nil, err
		}
		acquisition.ClaudeCredentialsFile = filepath.Join(config, ".credentials.json")
		if _, err := readSecretSource(acquisition.ClaudeCredentialsFile, "acquired Claude credentials"); err != nil {
			return nil, err
		}
	}
	if err := syncDirectory(flowRoot); err != nil {
		return nil, fmt.Errorf("sync isolated provider-login flow: %w", err)
	}
	success = true
	return acquisition, nil
}
