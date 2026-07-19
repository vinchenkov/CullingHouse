package verbs

import (
	"os"
	"path/filepath"
	"strings"
)

// MaterializeVerifierDisposableSource creates the Verifier's execution-scoped
// writable source from the sealed commit in the canonical task-local store.
// It never copies the canonical worktree: that tree is first required clean,
// then Git writes the sealed tree directly into the otherwise-empty projection.
func MaterializeVerifierDisposableSource(taskRoot, projectionRoot string, seal AcceptedCompletionSeal) error {
	if seal.TaskID < 1 || len(seal.SealedSHA) != oidLen(seal.ObjectFormat) || !assignmentHex.MatchString(seal.SealedSHA) {
		return Domainf("verifier disposable projection has no canonical sealed commit")
	}
	if err := validateSetupObjectFormat(seal.ObjectFormat); err != nil {
		return err
	}
	canonical := filepath.Join(taskRoot, "source")
	gitDir := filepath.Join(taskRoot, "git")
	for _, path := range []string{canonical, gitDir, projectionRoot} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Domainf("verifier disposable projection path is not a non-symlink directory")
		}
	}
	entries, err := os.ReadDir(projectionRoot)
	if err != nil || len(entries) != 0 {
		return Domainf("verifier disposable projection root is not empty")
	}
	status, err := gitOutput(canonical, sourceGitEnv(), nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || len(strings.TrimSpace(string(status))) != 0 {
		return Domainf("verifier disposable projection refuses a dirty canonical task source")
	}
	got, err := gitOutput(canonical, sourceGitEnv(), nil, "rev-parse", "--verify", seal.SealedSHA+"^{commit}")
	if err != nil || strings.TrimSpace(string(got)) != seal.SealedSHA {
		return Domainf("verifier disposable projection sealed commit is absent from the canonical task store")
	}
	// GIT_DIR names only the task-local sanitized store; GIT_WORK_TREE is the
	// disposable root. No primary checkout path or object store enters here.
	// The canonical task store is mounted RO to this setup container. Git's
	// default index location is therefore unavailable even though read-tree and
	// checkout-index are otherwise read-only against that store. Keep the
	// short-lived index in the disposable projection and remove it before that
	// projection becomes an agent mount.
	projectionIndex := filepath.Join(projectionRoot, ".mc-verifier-index")
	defer os.Remove(projectionIndex)
	env := append(sourceGitEnv(), "GIT_DIR="+gitDir, "GIT_WORK_TREE="+projectionRoot, "GIT_INDEX_FILE="+projectionIndex)
	if _, err := gitOutput("", env, nil, "read-tree", seal.SealedSHA); err != nil {
		return Domainf("verifier disposable projection could not build its sealed index: %v", err)
	}
	if _, err := gitOutput("", env, nil, "checkout-index", "-a", "-f"); err != nil {
		return Domainf("verifier disposable projection could not materialize its sealed tree: %v", err)
	}
	// The later container overlays this pointer read-only. Its relative target
	// resolves through the simultaneously RO-mounted canonical task root, so
	// Git reads sealed task-local controls without a primary-repository alias.
	pointer := []byte("gitdir: ../git/worktrees/" + taskWorktreeName(seal.TaskID) + "\n")
	if err := os.WriteFile(filepath.Join(projectionRoot, ".git"), pointer, 0o644); err != nil {
		return Domainf("verifier disposable projection could not write its Git pointer: %v", err)
	}
	return nil
}
