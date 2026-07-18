package verbs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The first-task setup executor: ADR-016 D5 / ADR-017:437-478's sanitized
// closure extraction and full task-local store materialization. It runs inside
// the resident's short-lived network=none setup container (no spine, no
// credentials); the host records the durable assignment from the SetupResult it
// returns and re-verifies it against the landed bytes. Darwin never runs this:
// the closure walk shells to the container's pinned git, so "Mission Control
// never invokes operator-installed host Git" holds by placement. Fast-lane
// tests drive the same Go against host git over a temp repo.
//
// firstTaskGitBin is the git binary; a package var only so a test could inject
// a fake, never an operator/env knob (production is the container's pinned git
// on PATH).
var firstTaskGitBin = "git"

// oidLen is the hex length of an object name in the given format.
func oidLen(objectFormat string) int {
	if objectFormat == "sha256" {
		return 64
	}
	return 40
}

func validateSetupObjectFormat(objectFormat string) error {
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return Domainf("first-task setup object format %q is outside the closed set", objectFormat)
	}
	return nil
}

// gitOutput runs git with an explicit environment and optional stdin, returning
// captured stdout. A non-zero exit is a domain error carrying stderr.
func gitOutput(dir string, env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command(firstTaskGitBin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = env
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// sourceGitEnv is the environment for reading the real source repository: its
// own repo config is read (that is where alternates/promisor live) but the
// operator's global and system git config are isolated out so they cannot mask
// a forbidden state or inject one.
func sourceGitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"HOME=" + os.TempDir(),
		"GIT_TERMINAL_PROMPT=0",
	}
}

func gitAtSource(dir string, args ...string) ([]byte, error) {
	return gitOutput(dir, sourceGitEnv(), nil, args...)
}

func sourceCommonDirAbs(sourceRepo string) (string, error) {
	out, err := gitAtSource(sourceRepo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", Domainf("first-task setup could not resolve the source git common dir: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func fileNonEmpty(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// checkSourceExtractable refuses a source whose object graph a plain closure
// walk cannot faithfully reproduce: alternates, grafts, and replace refs
// silently rewrite the reachable set, while shallow and partial/promisor
// sources have genuinely missing objects (ADR-017:449-461). Each is refused
// before any extraction, on the real source.
func checkSourceExtractable(sourceRepo string) error {
	gcd, err := sourceCommonDirAbs(sourceRepo)
	if err != nil {
		return err
	}
	if fileNonEmpty(filepath.Join(gcd, "objects", "info", "alternates")) {
		return Domainf("first-task setup refuses a source with object alternates")
	}
	if fileNonEmpty(filepath.Join(gcd, "info", "grafts")) {
		return Domainf("first-task setup refuses a source with commit grafts")
	}
	if rep, err := gitAtSource(sourceRepo, "for-each-ref", "--format=%(refname)", "refs/replace/"); err == nil &&
		len(strings.TrimSpace(string(rep))) > 0 {
		return Domainf("first-task setup refuses a source with replace refs")
	}
	if sh, err := gitAtSource(sourceRepo, "rev-parse", "--is-shallow-repository"); err == nil &&
		strings.TrimSpace(string(sh)) == "true" {
		return Domainf("first-task setup refuses a shallow source")
	}
	if _, err := gitAtSource(sourceRepo, "config", "--get", "extensions.partialClone"); err == nil {
		return Domainf("first-task setup refuses a partial-clone source")
	}
	if promisor, err := gitAtSource(sourceRepo, "config", "--get-regexp", `^remote\..*\.promisor$`); err == nil &&
		len(strings.TrimSpace(string(promisor))) > 0 {
		return Domainf("first-task setup refuses a promisor-remote source")
	}
	if hits, _ := filepath.Glob(filepath.Join(gcd, "objects", "pack", "*.promisor")); len(hits) > 0 {
		return Domainf("first-task setup refuses a source with promisor packs")
	}
	return nil
}

// syntheticGitContext creates a config/ref-free git dir whose only settings are
// the object format, and returns it plus the environment that reads the real
// source object directory through it with replace disabled and the operator's
// git config fully isolated (ADR-017:452-456). The caller must remove the dir.
func syntheticGitContext(objectFormat, sourceObjectDir string) (string, []string, error) {
	syn, err := os.MkdirTemp("", "mc-setup-syn-")
	if err != nil {
		return "", nil, Domainf("first-task setup could not create a synthetic git context: %v", err)
	}
	for _, d := range []string{"objects", "refs"} {
		if err := os.Mkdir(filepath.Join(syn, d), 0o700); err != nil {
			os.RemoveAll(syn)
			return "", nil, Domainf("first-task setup could not create a synthetic git context: %v", err)
		}
	}
	config := "[core]\n\trepositoryformatversion = 1\n"
	if objectFormat == "sha256" {
		config += "[extensions]\n\tobjectFormat = sha256\n"
	}
	if err := os.WriteFile(filepath.Join(syn, "config"), []byte(config), 0o600); err != nil {
		os.RemoveAll(syn)
		return "", nil, Domainf("first-task setup could not write the synthetic git context: %v", err)
	}
	if err := os.WriteFile(filepath.Join(syn, "HEAD"), []byte("ref: refs/heads/none\n"), 0o600); err != nil {
		os.RemoveAll(syn)
		return "", nil, Domainf("first-task setup could not write the synthetic git context: %v", err)
	}
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_DIR=" + syn,
		"GIT_OBJECT_DIRECTORY=" + sourceObjectDir,
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"HOME=" + syn,
		"GIT_TERMINAL_PROMPT=0",
	}
	return syn, env, nil
}

func firstTokens(out []byte) []string {
	var toks []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			toks = append(toks, f[0])
		}
	}
	return toks
}

// packObjectOIDs parses `verify-pack -v` output: object lines begin with an OID
// then a git type; trailing histogram/summary lines do not.
func packObjectOIDs(out []byte) []string {
	var oids []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[1] {
		case "commit", "tree", "blob", "tag":
			oids = append(oids, f[0])
		}
	}
	return oids
}

func sameOIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func singlePackIdx(packDir string) (string, error) {
	idxs, err := filepath.Glob(filepath.Join(packDir, "pack-*.idx"))
	if err != nil || len(idxs) != 1 {
		return "", Domainf("first-task closure did not produce exactly one pack index")
	}
	return idxs[0], nil
}

// extractClosurePack streams exactly the reachable object closure of baseSHA
// from the source repository's real object directory into ONE new pack under
// packDir, using a synthetic config/ref-free git context. It never clones,
// hardlinks, or persists an alternate; it proves the landed pack's object set
// equals the reachable closure and returns that object count.
func extractClosurePack(sourceRepo, baseSHA, objectFormat, packDir string) (int, error) {
	if err := validateSetupObjectFormat(objectFormat); err != nil {
		return 0, err
	}
	if len(baseSHA) != oidLen(objectFormat) || !assignmentHex.MatchString(baseSHA) {
		return 0, Domainf("first-task closure base SHA is not a canonical %s object name", objectFormat)
	}
	if err := checkSourceExtractable(sourceRepo); err != nil {
		return 0, err
	}
	gcd, err := sourceCommonDirAbs(sourceRepo)
	if err != nil {
		return 0, err
	}
	syn, env, err := syntheticGitContext(objectFormat, filepath.Join(gcd, "objects"))
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(syn)

	if typ, err := gitOutput("", env, nil, "cat-file", "-t", baseSHA); err != nil ||
		strings.TrimSpace(string(typ)) != "commit" {
		return 0, Domainf("first-task closure base %s is not a commit in the source object store", baseSHA)
	}
	revOut, err := gitOutput("", env, nil, "rev-list", "--objects", baseSHA)
	if err != nil {
		return 0, Domainf("first-task closure could not enumerate the reachable set: %v", err)
	}
	oids := firstTokens(revOut)
	if len(oids) == 0 {
		return 0, Domainf("first-task closure is empty")
	}
	stdin := []byte(strings.Join(oids, "\n") + "\n")
	if _, err := gitOutput("", env, stdin, "-c", "pack.writeReverseIndex=false",
		"pack-objects", filepath.Join(packDir, "pack")); err != nil {
		return 0, Domainf("first-task closure could not pack the reachable objects: %v", err)
	}
	idx, err := singlePackIdx(packDir)
	if err != nil {
		return 0, err
	}
	vpOut, err := gitOutput("", env, nil, "verify-pack", "-v", idx)
	if err != nil {
		return 0, Domainf("first-task closure pack failed verification: %v", err)
	}
	if !sameOIDSet(oids, packObjectOIDs(vpOut)) {
		return 0, Domainf("first-task closure pack object set does not equal the reachable closure")
	}
	return len(oids), nil
}
