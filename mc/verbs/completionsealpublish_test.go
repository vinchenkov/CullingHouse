package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealTaskCompletionPublishesOneImmutableVerifiedPack(t *testing.T) {
	src, _, format := buildSourceRepo(t)
	root := mkTaskChildren(t)
	seeded, err := MaterializeFirstTaskStore(src, root, FirstTaskSetupSpec{
		TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	sealDir := filepath.Join(parent, "worker")
	publication, err := SealTaskCompletion(root, sealDir, "worker", "0011223344556677", 7)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sealDir, 0o700)
		entries, _ := os.ReadDir(sealDir)
		for _, entry := range entries {
			_ = os.Chmod(filepath.Join(sealDir, entry.Name()), 0o600)
		}
	})
	if publication.SealedSHA != seeded.BaseSHA || publication.ClosureDigest != seeded.ClosureDigest || publication.ManifestDigest == "" {
		t.Fatalf("publication=%+v seed=%+v", publication, seeded)
	}
	body, err := os.ReadFile(filepath.Join(sealDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest CompletionSealManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Tree == "" || manifest.ObjectCount < 1 || len(manifest.Files) != 2 || manifest.LocalRepoUUID != seeded.LocalRepoUUID {
		t.Fatalf("manifest=%+v", manifest)
	}
	for _, name := range append([]string{"manifest.json"}, manifest.Files[0].Name, manifest.Files[1].Name) {
		info, err := os.Stat(filepath.Join(sealDir, name))
		if err != nil || info.Mode().Perm() != 0o444 {
			t.Fatalf("seal entry %q mode=%v err=%v, want 0444", name, info.Mode(), err)
		}
	}
	if _, err := RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{
		RunID: publication.RunID, TaskID: publication.TaskID, CompletionRequest: publication.CompletionRequest,
		ObjectFormat: publication.ObjectFormat, SealedSHA: publication.SealedSHA,
		ClosureDigest: publication.ClosureDigest, ManifestDigest: publication.ManifestDigest,
		Device: publication.Device, Inode: publication.Inode, OwnerUID: publication.OwnerUID,
	}); err != nil {
		t.Fatalf("published seal did not rebuild: %v", err)
	}
	if _, err := SealTaskCompletion(root, sealDir, "worker", "0011223344556677", 7); err == nil {
		t.Fatal("existing seal destination was overwritten")
	}
}

func TestSealTaskCompletionRefusesDirtyTaskStore(t *testing.T) {
	src, _, format := buildSourceRepo(t)
	root := mkTaskChildren(t)
	if _, err := MaterializeFirstTaskStore(src, root, FirstTaskSetupSpec{
		TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "untracked.txt"), []byte("no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := SealTaskCompletion(root, filepath.Join(t.TempDir(), "worker"), "worker", "0011223344556677", 7)
	if err == nil || !strings.Contains(err.Error(), "not clean") {
		t.Fatalf("dirty task seal error=%v", err)
	}
}

func TestSealTaskCompletionPublishesIntoAnExistingMountedRunRoot(t *testing.T) {
	src, _, format := buildSourceRepo(t)
	root := mkTaskChildren(t)
	if _, err := MaterializeFirstTaskStore(src, root, FirstTaskSetupSpec{
		TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	}); err != nil {
		t.Fatal(err)
	}
	// The production mount source is pre-created by the resident. It is a
	// mount point, so the publisher must not require a parent-visible rename.
	sealRoot := t.TempDir()
	publication, err := SealTaskCompletion(root, sealRoot, "worker", "0011223344556677", 7)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sealRoot, 0o700)
		entries, _ := os.ReadDir(sealRoot)
		for _, entry := range entries {
			_ = os.Chmod(filepath.Join(sealRoot, entry.Name()), 0o600)
		}
	})
	if info, err := os.Stat(sealRoot); err != nil || info.Mode().Perm() != 0o555 {
		t.Fatalf("mounted seal root mode=%v err=%v, want 0555", info.Mode(), err)
	}
	if _, err := RebuildAcceptedCompletionSeal(sealRoot, mkTaskChildren(t), AcceptedCompletionSeal{
		RunID: publication.RunID, TaskID: publication.TaskID, CompletionRequest: publication.CompletionRequest,
		ObjectFormat: publication.ObjectFormat, SealedSHA: publication.SealedSHA,
		ClosureDigest: publication.ClosureDigest, ManifestDigest: publication.ManifestDigest,
		Device: publication.Device, Inode: publication.Inode, OwnerUID: publication.OwnerUID,
	}); err != nil {
		t.Fatalf("mounted seal did not rebuild: %v", err)
	}
}
