package verbs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// SealTaskCompletion creates the immutable filesystem half of a Worker
// completion. Its caller supplies only the fixed task and run-keyed seal
// roots; the returned path-free publication must still pass PublishCompletionSeal
// before the completion can be accepted.
func SealTaskCompletion(taskRoot, sealDir, runID, requestID string, taskID int64) (CompletionSealPublication, error) {
	if taskID < 1 || runID == "" || len(requestID) != 16 || !assignmentHex.MatchString(requestID) {
		return CompletionSealPublication{}, Domainf("completion seal identity is malformed")
	}
	// Docker binds the exact run-keyed host directory at the fixed private
	// destination. A mount point cannot be atomically replaced, so an existing
	// empty directory uses the manifest-as-commit-marker form below. The model
	// cannot traverse /mc/private, and no consumer is admitted before the
	// resulting receipt is accepted, so partial staging is never an authority.
	if info, err := os.Lstat(sealDir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return CompletionSealPublication{}, Domainf("completion seal destination is not an empty directory")
		}
		entries, err := os.ReadDir(sealDir)
		if err != nil || len(entries) != 0 {
			return CompletionSealPublication{}, Domainf("completion seal destination is not empty")
		}
		return sealMountedTaskCompletion(taskRoot, sealDir, runID, requestID, taskID)
	} else if !os.IsNotExist(err) {
		return CompletionSealPublication{}, Domainf("completion seal destination is unreadable")
	}
	if filepath.Base(sealDir) != runID {
		return CompletionSealPublication{}, Domainf("completion seal path does not use the producer run key")
	}
	parent := filepath.Dir(sealDir)
	staging, err := os.MkdirTemp(parent, ".mc-seal-")
	if err != nil {
		return CompletionSealPublication{}, Domainf("create completion seal staging: %v", err)
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return CompletionSealPublication{}, Domainf("secure completion seal staging: %v", err)
	}
	publication, err := buildCompletionSealStaging(taskRoot, staging, runID, requestID, taskID)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	if err := syncDir(staging); err != nil {
		return CompletionSealPublication{}, err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return CompletionSealPublication{}, Domainf("read completion seal staging: %v", err)
	}
	for _, entry := range entries {
		if err := os.Chmod(filepath.Join(staging, entry.Name()), 0o444); err != nil {
			return CompletionSealPublication{}, Domainf("make completion seal immutable: %v", err)
		}
	}
	if err := os.Chmod(staging, 0o555); err != nil {
		return CompletionSealPublication{}, Domainf("make completion seal immutable: %v", err)
	}
	if err := syncDir(staging); err != nil {
		return CompletionSealPublication{}, err
	}
	if err := os.Rename(staging, sealDir); err != nil {
		return CompletionSealPublication{}, Domainf("publish completion seal: %v", err)
	}
	if err := syncDir(parent); err != nil {
		return CompletionSealPublication{}, err
	}
	info, err := os.Lstat(sealDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return CompletionSealPublication{}, Domainf("published completion seal is not a directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return CompletionSealPublication{}, Domainf("published completion seal lacks filesystem identity")
	}
	publication.Device, publication.Inode, publication.OwnerUID = strconv.FormatUint(uint64(stat.Dev), 10), strconv.FormatUint(stat.Ino, 10), int64(stat.Uid)
	return publication, nil
}

// sealMountedTaskCompletion publishes into a pre-existing empty bind root.
// Pack/index files become durable before manifest.json, which is the sole
// visibility marker; only an accepted spine receipt permits a later setup
// consumer to inspect this root.
func sealMountedTaskCompletion(taskRoot, sealRoot, runID, requestID string, taskID int64) (CompletionSealPublication, error) {
	staging, err := os.MkdirTemp(sealRoot, ".mc-seal-")
	if err != nil {
		return CompletionSealPublication{}, Domainf("create completion seal staging: %v", err)
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return CompletionSealPublication{}, Domainf("secure completion seal staging: %v", err)
	}
	publication, err := buildCompletionSealStaging(taskRoot, staging, runID, requestID, taskID)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	files, err := stagedSealPackNames(staging)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	for _, name := range files {
		if err := os.Chmod(filepath.Join(staging, name), 0o444); err != nil {
			return CompletionSealPublication{}, Domainf("make completion seal immutable: %v", err)
		}
	}
	if err := os.Chmod(filepath.Join(staging, "manifest.json"), 0o444); err != nil {
		return CompletionSealPublication{}, Domainf("make completion seal immutable: %v", err)
	}
	if err := syncSealFiles(staging); err != nil {
		return CompletionSealPublication{}, err
	}
	for _, name := range files {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(sealRoot, name)); err != nil {
			return CompletionSealPublication{}, Domainf("publish completion seal entry: %v", err)
		}
	}
	if err := syncDir(sealRoot); err != nil {
		return CompletionSealPublication{}, err
	}
	// The manifest binds every preceding immutable entry. It moves last, so a
	// crashed publisher leaves no consumable completion seal.
	if err := os.Rename(filepath.Join(staging, "manifest.json"), filepath.Join(sealRoot, "manifest.json")); err != nil {
		return CompletionSealPublication{}, Domainf("publish completion seal manifest: %v", err)
	}
	if err := os.Chmod(sealRoot, 0o555); err != nil {
		return CompletionSealPublication{}, Domainf("make completion seal immutable: %v", err)
	}
	if err := syncDir(sealRoot); err != nil {
		return CompletionSealPublication{}, err
	}
	info, err := os.Lstat(sealRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return CompletionSealPublication{}, Domainf("published completion seal is not a directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return CompletionSealPublication{}, Domainf("published completion seal lacks filesystem identity")
	}
	publication.Device, publication.Inode, publication.OwnerUID = strconv.FormatUint(uint64(stat.Dev), 10), strconv.FormatUint(stat.Ino, 10), int64(stat.Uid)
	return publication, nil
}

func buildCompletionSealStaging(taskRoot, staging, runID, requestID string, taskID int64) (CompletionSealPublication, error) {
	source, gitDir := filepath.Join(taskRoot, "source"), filepath.Join(taskRoot, "git")
	format, uuid, head, tree, err := inspectCompletableTaskStore(source, gitDir, taskID)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	count, err := extractTaskClosurePack(gitDir, head, format, staging)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	if afterHead, afterTree, err := taskHeadAndTree(source, taskID, format); err != nil || afterHead != head || afterTree != tree {
		return CompletionSealPublication{}, Domainf("completion seal task branch changed during packing")
	}
	files, err := sealFiles(staging)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	closure, err := digestLandedPack(staging)
	if err != nil {
		return CompletionSealPublication{}, err
	}
	body, err := json.Marshal(CompletionSealManifest{Version: 1, RunID: runID, TaskID: taskID, CompletionRequest: requestID,
		ObjectFormat: format, SealedSHA: head, Tree: tree, ObjectCount: count, ClosureDigest: closure, LocalRepoUUID: uuid, Files: files})
	if err != nil {
		return CompletionSealPublication{}, Domainf("encode completion seal manifest: %v", err)
	}
	digest := sha256.Sum256(body)
	if err := writeSealFile(filepath.Join(staging, "manifest.json"), body); err != nil {
		return CompletionSealPublication{}, err
	}
	if err := syncSealFiles(staging); err != nil {
		return CompletionSealPublication{}, err
	}
	return CompletionSealPublication{RunID: runID, TaskID: taskID, CompletionRequest: requestID,
		ObjectFormat: format, SealedSHA: head, ClosureDigest: closure, ManifestDigest: hex.EncodeToString(digest[:])}, nil
}

func inspectCompletableTaskStore(source, gitDir string, taskID int64) (format, uuid, head, tree string, err error) {
	body, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil || validateTaskGitConfig(body) != nil {
		return "", "", "", "", Domainf("completion seal task config is not the fixed grammar")
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "localRepoUuid") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid = strings.TrimSpace(parts[1])
			}
		}
	}
	formatBytes, err := gitOutput(source, sourceGitEnv(), nil, "rev-parse", "--show-object-format")
	if err != nil {
		return "", "", "", "", Domainf("completion seal cannot read task object format: %v", err)
	}
	format = strings.TrimSpace(string(formatBytes))
	if err := validateSetupObjectFormat(format); err != nil || !assignmentUUID.MatchString(uuid) || string(body) != string(generatedTaskGitConfig(format, uuid)) {
		return "", "", "", "", Domainf("completion seal task config does not reproduce its local identity")
	}
	if fileNonEmpty(filepath.Join(gitDir, "objects", "info", "alternates")) {
		return "", "", "", "", Domainf("completion seal task store has object alternates")
	}
	status, err := gitOutput(source, sourceGitEnv(), nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || strings.TrimSpace(string(status)) != "" {
		return "", "", "", "", Domainf("completion seal task worktree is not clean")
	}
	head, tree, err = taskHeadAndTree(source, taskID, format)
	if err != nil {
		return "", "", "", "", err
	}
	refs, err := gitOutput(source, sourceGitEnv(), nil, "for-each-ref", "--format=%(refname)")
	if err != nil || strings.TrimSpace(string(refs)) != "refs/heads/"+taskAssignmentBranch(taskID) {
		return "", "", "", "", Domainf("completion seal task store does not have exactly its managed branch")
	}
	if err := fsckClean(gitDir); err != nil {
		return "", "", "", "", err
	}
	return format, uuid, head, tree, nil
}

func taskHeadAndTree(source string, taskID int64, format string) (string, string, error) {
	branch, err := gitOutput(source, sourceGitEnv(), nil, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || strings.TrimSpace(string(branch)) != "refs/heads/"+taskAssignmentBranch(taskID) {
		return "", "", Domainf("completion seal task HEAD is not its managed branch")
	}
	head, err := gitOutput(source, sourceGitEnv(), nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || len(strings.TrimSpace(string(head))) != oidLen(format) || !assignmentHex.MatchString(strings.TrimSpace(string(head))) {
		return "", "", Domainf("completion seal task HEAD is not a canonical commit")
	}
	tree, err := gitOutput(source, sourceGitEnv(), nil, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil || len(strings.TrimSpace(string(tree))) != oidLen(format) || !assignmentHex.MatchString(strings.TrimSpace(string(tree))) {
		return "", "", Domainf("completion seal task tree is not canonical")
	}
	return strings.TrimSpace(string(head)), strings.TrimSpace(string(tree)), nil
}

func extractTaskClosurePack(gitDir, head, format, packDir string) (int, error) {
	if err := os.MkdirAll(packDir, 0o700); err != nil {
		return 0, Domainf("create completion seal pack: %v", err)
	}
	syn, env, err := syntheticGitContext(format, filepath.Join(gitDir, "objects"))
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(syn)
	revOut, err := gitOutput("", env, nil, "rev-list", "--objects", head)
	if err != nil {
		return 0, Domainf("completion seal cannot enumerate closure: %v", err)
	}
	oids := firstTokens(revOut)
	if len(oids) == 0 {
		return 0, Domainf("completion seal closure is empty")
	}
	if _, err := gitOutput("", env, []byte(strings.Join(oids, "\n")+"\n"), "-c", "pack.writeReverseIndex=false", "pack-objects", filepath.Join(packDir, "pack")); err != nil {
		return 0, Domainf("completion seal cannot pack closure: %v", err)
	}
	idx, err := singlePackIdx(packDir)
	if err != nil {
		return 0, Domainf("completion seal did not produce one pack index")
	}
	verified, err := gitOutput("", env, nil, "verify-pack", "-v", idx)
	if err != nil || !sameOIDSet(oids, packObjectOIDs(verified)) {
		return 0, Domainf("completion seal pack does not reproduce the reachable closure")
	}
	return len(oids), nil
}

func sealFiles(dir string) ([]CompletionSealFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, Domainf("read completion seal pack: %v", err)
	}
	files := make([]CompletionSealFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "pack-") || (!strings.HasSuffix(entry.Name(), ".pack") && !strings.HasSuffix(entry.Name(), ".idx")) {
			return nil, Domainf("completion seal pack has an unexpected entry")
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, Domainf("read completion seal pack: %v", err)
		}
		sum := sha256.Sum256(body)
		files = append(files, CompletionSealFile{Name: entry.Name(), Digest: hex.EncodeToString(sum[:])})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	if len(files) != 2 || strings.TrimSuffix(files[0].Name, ".idx") != strings.TrimSuffix(files[1].Name, ".pack") {
		return nil, Domainf("completion seal pack does not contain one matching pair")
	}
	return files, nil
}

func stagedSealPackNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, Domainf("read completion seal pack: %v", err)
	}
	names := make([]string, 0, 2)
	for _, entry := range entries {
		if entry.Name() == "manifest.json" {
			continue
		}
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "pack-") || (!strings.HasSuffix(entry.Name(), ".pack") && !strings.HasSuffix(entry.Name(), ".idx")) {
			return nil, Domainf("completion seal pack has an unexpected entry")
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) != 2 || strings.TrimSuffix(names[0], ".idx") != strings.TrimSuffix(names[1], ".pack") {
		return nil, Domainf("completion seal pack does not contain one matching pair")
	}
	return names, nil
}

func writeSealFile(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Domainf("write completion seal manifest: %v", err)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return Domainf("write completion seal manifest: %v", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Domainf("sync completion seal manifest: %v", err)
	}
	if err := f.Close(); err != nil {
		return Domainf("close completion seal manifest: %v", err)
	}
	return nil
}

func syncSealFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Domainf("read completion seal staging: %v", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return Domainf("completion seal staging holds a non-regular entry")
		}
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return Domainf("open completion seal entry: %v", err)
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return Domainf("sync completion seal entry: %v", err)
		}
	}
	return nil
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return Domainf("open completion seal directory: %v", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return Domainf("sync completion seal directory: %v", err)
	}
	return nil
}
