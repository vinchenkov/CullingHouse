package verbs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

// FirstTaskSetupSpec is the closed instruction the in-container executor
// materializes: the task id (which fixes the branch mc/task-<id> and worktree
// mc-task-<id>), fresh-vs-retry mode, the target ref to resolve (fresh) or the
// exact pinned base OID to reuse (retry), the expected object format, and the
// local repository UUID (fresh-generated or retry-pinned).
type FirstTaskSetupSpec struct {
	TaskID        int64
	Mode          string
	TargetRef     string
	PinnedBaseSHA string
	ObjectFormat  string
	LocalRepoUUID string
}

// SetupResult is the git-derived evidence the in-container executor emits and
// the host records as the task assignment and cross-checks against the landed
// store. Its JSON is /mc/setup-result.json's contract.
type SetupResult struct {
	BaseSHA       string `json:"base_sha"`
	ObjectFormat  string `json:"object_format"`
	LocalRepoUUID string `json:"local_repo_uuid"`
	ClosureDigest string `json:"closure_digest"`
	ObjectCount   int    `json:"object_count"`
	FsckClean     bool   `json:"fsck_clean"`
}

// generatedTaskGitConfig is the closed-key-set RO config of a task-local store
// (ADR-017:463-467): object format, relative linked worktrees, and MC identity.
// No remote/credential/include/alternate/graft/replace/promisor/hook/fsmonitor/
// maintenance/filter key is present. `extensions.relativeWorktrees` (repo-format
// v1) is git's actual relative-path mechanism; ADR-017:466's
// `worktree.useRelativePaths` is only the `git worktree add` trigger that git
// persists AS this extension (deviation logged 2026-07-17).
func generatedTaskGitConfig(objectFormat, uuid string) []byte {
	var b strings.Builder
	b.WriteString("[core]\n\trepositoryformatversion = 1\n\tbare = true\n")
	b.WriteString("[extensions]\n\trelativeWorktrees = true\n")
	if objectFormat == "sha256" {
		b.WriteString("\tobjectFormat = sha256\n")
	}
	b.WriteString("[mc]\n\tlocalRepoUuid = " + uuid + "\n")
	return []byte(b.String())
}

const maxTaskGitConfigBytes = 4096

var taskConfigAllowed = map[string]bool{
	"core.repositoryformatversion": true,
	"core.bare":                    true,
	"extensions.relativeworktrees": true,
	"extensions.objectformat":      true,
	"mc.localrepouuid":             true,
}

// validateTaskGitConfig is the closed-grammar check the spine-free
// dispatch-attest resolver uses on git/config: it admits only the exact key set
// generatedTaskGitConfig writes and rejects any other section or key (so no
// remote/credential/hook/alternate/filter can ride in), plus the format-
// critical values. The exact object_format/uuid match against the recorded
// assignment is the spine-present RecordFirstTaskSetupClosure's job, not this
// resolver's.
func validateTaskGitConfig(body []byte) error {
	if len(body) > maxTaskGitConfigBytes {
		return Domainf("task git config exceeds its content bound")
	}
	section := ""
	seen := map[string]string{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return Domainf("task git config has a malformed section header")
			}
			inner := strings.TrimSpace(line[1 : len(line)-1])
			if inner == "" || strings.ContainsAny(inner, " \t\"'") {
				return Domainf("task git config uses a subsection, which the closed grammar forbids")
			}
			section = strings.ToLower(inner)
			continue
		}
		if section == "" {
			return Domainf("task git config has a key outside any section")
		}
		key := line
		if i := strings.IndexByte(line, '='); i >= 0 {
			key = strings.TrimSpace(line[:i])
			seen[section+"."+strings.ToLower(key)] = strings.TrimSpace(line[i+1:])
		} else {
			seen[section+"."+strings.ToLower(key)] = ""
		}
		if !taskConfigAllowed[section+"."+strings.ToLower(key)] {
			return Domainf("task git config carries %q, outside the closed grammar", section+"."+strings.ToLower(key))
		}
	}
	if seen["core.repositoryformatversion"] != "1" {
		return Domainf("task git config repositoryformatversion is not 1")
	}
	if strings.ToLower(seen["core.bare"]) != "true" {
		return Domainf("task git config is not marked bare")
	}
	if strings.ToLower(seen["extensions.relativeworktrees"]) != "true" {
		return Domainf("task git config does not enable relative worktrees")
	}
	if v, ok := seen["extensions.objectformat"]; ok && v != "sha256" {
		return Domainf("task git config object format is not sha256")
	}
	if uuid, ok := seen["mc.localrepouuid"]; !ok || !assignmentUUID.MatchString(uuid) {
		return Domainf("task git config carries no valid MC identity")
	}
	return nil
}

func requireEmptyChild(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return Domainf("first-task setup child %s is unreadable: %v", filepath.Base(path), err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Domainf("first-task setup child %s is not a non-symlink directory", filepath.Base(path))
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Domainf("first-task setup child %s is unreadable: %v", filepath.Base(path), err)
	}
	if len(entries) != 0 {
		return Domainf("first-task setup child %s already holds residue; setup never overwrites", filepath.Base(path))
	}
	return nil
}

func resolveBaseOID(sourceRepo, targetRef, objectFormat string) (string, error) {
	if targetRef == "" {
		return "", Domainf("first-task setup fresh mode has no target ref to resolve")
	}
	out, err := gitAtSource(sourceRepo, "rev-parse", "--verify", "--end-of-options", targetRef+"^{commit}")
	if err != nil {
		return "", Domainf("first-task setup could not resolve target ref %q: %v", targetRef, err)
	}
	oid := strings.TrimSpace(string(out))
	if len(oid) != oidLen(objectFormat) || !assignmentHex.MatchString(oid) {
		return "", Domainf("first-task setup resolved a non-canonical base OID")
	}
	return oid, nil
}

// rejectReservedTreeComponent refuses a base tree whose top level contains a
// reserved MC/git cover name, which setup could otherwise never cover without
// colliding with the real content (ADR-017:433-434).
func rejectReservedTreeComponent(sourceRepo, baseSHA string) error {
	out, err := gitAtSource(sourceRepo, "ls-tree", "--name-only", baseSHA)
	if err != nil {
		return Domainf("first-task setup could not read the base tree: %v", err)
	}
	for _, name := range strings.Split(string(out), "\n") {
		switch strings.TrimSpace(name) {
		case ".mission-control", ".git":
			return Domainf("first-task setup refuses a base tree with a reserved %q root component", strings.TrimSpace(name))
		}
	}
	return nil
}

func digestLandedPack(packDir string) (string, error) {
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return "", Domainf("first-task setup closure store is unreadable: %v", err)
	}
	var files []FirstTaskClosureFile
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return "", Domainf("first-task setup closure store holds a non-regular entry")
		}
		body, err := os.ReadFile(filepath.Join(packDir, e.Name()))
		if err != nil {
			return "", Domainf("first-task setup closure store is unreadable: %v", err)
		}
		files = append(files, FirstTaskClosureFile{Name: e.Name(), Body: body})
	}
	return firstTaskClosureDigest(files), nil
}

// fsckClean proves the assembled store is object-complete and consistent
// (ADR-017:474). rc 0 with the emptyPackedRefsFile warning (a consequence of
// the ADR's empty packed-refs cover) is clean; any error/missing/broken/
// dangling line is not.
func fsckClean(gitDir string) error {
	out, err := gitOutput(gitDir, sourceGitEnv(), nil, "fsck", "--full", "--strict", "--no-dangling")
	if err != nil {
		return Domainf("first-task store failed fsck: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if l == "" || strings.HasPrefix(l, "warning") || strings.HasPrefix(l, "checking") {
			continue
		}
		if strings.HasPrefix(l, "error") || strings.HasPrefix(l, "missing") ||
			strings.HasPrefix(l, "broken") || strings.HasPrefix(l, "dangling") || strings.HasPrefix(l, "fatal") {
			return Domainf("first-task store fsck reported: %s", strings.TrimSpace(line))
		}
	}
	return nil
}

// MaterializeFirstTaskStore builds the complete task-local store in place under
// taskRoot's empty source/ and git/ children (ADR-017:437-478). It runs inside
// the network=none setup container with no spine: the durable assignment is
// recorded by the host from the returned SetupResult, which the host also
// re-verifies against the landed bytes.
func MaterializeFirstTaskStore(sourceRepo, taskRoot string, spec FirstTaskSetupSpec) (SetupResult, error) {
	if spec.TaskID < 1 {
		return SetupResult{}, Domainf("first-task setup task id %d is not a canonical positive decimal", spec.TaskID)
	}
	if err := validateSetupObjectFormat(spec.ObjectFormat); err != nil {
		return SetupResult{}, err
	}
	if !assignmentUUID.MatchString(spec.LocalRepoUUID) {
		return SetupResult{}, Domainf("first-task setup local repository UUID is malformed")
	}

	sf, err := gitAtSource(sourceRepo, "rev-parse", "--show-object-format")
	if err != nil {
		return SetupResult{}, Domainf("first-task setup could not read the source object format: %v", err)
	}
	if got := strings.TrimSpace(string(sf)); got != spec.ObjectFormat {
		return SetupResult{}, Domainf("first-task setup source object format %q does not match the pinned %q", got, spec.ObjectFormat)
	}

	var baseSHA string
	switch spec.Mode {
	case "fresh":
		baseSHA, err = resolveBaseOID(sourceRepo, spec.TargetRef, spec.ObjectFormat)
		if err != nil {
			return SetupResult{}, err
		}
	case "retry":
		baseSHA = spec.PinnedBaseSHA
		if len(baseSHA) != oidLen(spec.ObjectFormat) || !assignmentHex.MatchString(baseSHA) {
			return SetupResult{}, Domainf("first-task setup retry base SHA is not a canonical object name")
		}
		if typ, err := gitAtSource(sourceRepo, "cat-file", "-t", baseSHA); err != nil ||
			strings.TrimSpace(string(typ)) != "commit" {
			return SetupResult{}, Domainf("first-task setup retry base %s is not a commit in the source", baseSHA)
		}
	default:
		return SetupResult{}, Domainf("first-task setup mode %q is neither fresh nor retry", spec.Mode)
	}

	if err := rejectReservedTreeComponent(sourceRepo, baseSHA); err != nil {
		return SetupResult{}, err
	}

	source := filepath.Join(taskRoot, "source")
	gitDir := filepath.Join(taskRoot, "git")
	for _, c := range []string{source, gitDir} {
		if err := requireEmptyChild(c); err != nil {
			return SetupResult{}, err
		}
	}

	wtName := taskWorktreeName(spec.TaskID)
	branch := taskAssignmentBranch(spec.TaskID)
	wt := filepath.Join(gitDir, "worktrees", wtName)
	for _, d := range []string{
		filepath.Join(source, ".mission-control"),
		filepath.Join(gitDir, "objects", "pack"),
		filepath.Join(gitDir, "objects", "info"),
		filepath.Join(gitDir, "info"),
		filepath.Join(gitDir, "hooks"),
		filepath.Join(gitDir, "refs", "heads", "mc"),
		wt,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return SetupResult{}, Domainf("first-task setup could not create %s: %v", d, err)
		}
	}

	refPath := filepath.Join(gitDir, "refs", "heads", "mc", "task-"+strconv.FormatInt(spec.TaskID, 10))
	headRef := "ref: refs/heads/" + branch + "\n"
	files := []struct{ path, body string }{
		{filepath.Join(gitDir, "config"), string(generatedTaskGitConfig(spec.ObjectFormat, spec.LocalRepoUUID))},
		{filepath.Join(gitDir, "HEAD"), headRef},
		{refPath, baseSHA + "\n"},
		{filepath.Join(gitDir, "packed-refs"), ""},
		{filepath.Join(gitDir, "shallow"), ""},
		{filepath.Join(wt, "commondir"), "../..\n"},
		{filepath.Join(wt, "gitdir"), "../../../source/.git\n"},
		{filepath.Join(wt, "HEAD"), headRef},
		{filepath.Join(wt, "config.worktree"), ""},
		{filepath.Join(source, ".git"), "gitdir: ../git/worktrees/" + wtName + "\n"},
	}
	for _, f := range files {
		if err := os.WriteFile(f.path, []byte(f.body), 0o644); err != nil {
			return SetupResult{}, Domainf("first-task setup could not write %s: %v", f.path, err)
		}
	}

	packDir := filepath.Join(gitDir, "objects", "pack")
	count, err := extractClosurePack(sourceRepo, baseSHA, spec.ObjectFormat, packDir)
	if err != nil {
		return SetupResult{}, err
	}

	storeEnv := sourceGitEnv()
	if _, err := gitOutput(source, storeEnv, nil, "read-tree", baseSHA); err != nil {
		return SetupResult{}, Domainf("first-task setup could not build the worktree index: %v", err)
	}
	if _, err := gitOutput(source, storeEnv, nil, "checkout-index", "-a", "-f"); err != nil {
		return SetupResult{}, Domainf("first-task setup could not materialize the worktree: %v", err)
	}
	if err := fsckClean(gitDir); err != nil {
		return SetupResult{}, err
	}

	digest, err := digestLandedPack(packDir)
	if err != nil {
		return SetupResult{}, err
	}
	return SetupResult{
		BaseSHA: baseSHA, ObjectFormat: spec.ObjectFormat, LocalRepoUUID: spec.LocalRepoUUID,
		ClosureDigest: digest, ObjectCount: count, FsckClean: true,
	}, nil
}
