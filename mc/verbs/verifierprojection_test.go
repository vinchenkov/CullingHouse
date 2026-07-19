package verbs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeVerifierDisposableSourceUsesOnlyTheSealedTree(t *testing.T) {
	source, _, format := buildSourceRepo(t)
	task := t.TempDir()
	for _, child := range []string{"source", "git"} {
		if err := os.Mkdir(filepath.Join(task, child), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := MaterializeFirstTaskStore(source, task, FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format, LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"})
	if err != nil {
		t.Fatal(err)
	}
	projection := filepath.Join(t.TempDir(), "projection")
	if err := os.Mkdir(projection, 0o755); err != nil {
		t.Fatal(err)
	}
	seal := AcceptedCompletionSeal{TaskID: 7, ObjectFormat: format, SealedSHA: result.BaseSHA}
	if err := MaterializeVerifierDisposableSource(task, projection, seal); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projection, "README.md")); err != nil {
		t.Fatalf("sealed tree missing file: %v", err)
	}
	pointer, err := os.ReadFile(filepath.Join(projection, ".git"))
	if err != nil || string(pointer) != "gitdir: ../git/worktrees/mc-task-7\n" {
		t.Fatalf("projection .git pointer=(%q,%v), want fixed relative task control", pointer, err)
	}
	if _, err := os.Stat(filepath.Join(projection, ".mc-verifier-index")); !os.IsNotExist(err) {
		t.Fatalf("projection retained setup-only Git index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(task, "source", "late.txt"), []byte("late"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MaterializeVerifierDisposableSource(task, other, seal); err == nil {
		t.Fatal("dirty canonical source materialized a disposable projection")
	}
}
