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
	"testing"
)

func TestRebuildAcceptedCompletionSealUsesOnlyManifestVerifiedPack(t *testing.T) {
	src, _, format := buildSourceRepo(t)
	seed := mkTaskChildren(t)
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	seeded, err := MaterializeFirstTaskStore(src, seed, FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format, LocalRepoUUID: uuid})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := gitAtSource(filepath.Join(seed, "source"), "rev-parse", "--verify", seeded.BaseSHA+"^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	sealDir := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(seed, "git", "objects", "pack"))
	if err != nil {
		t.Fatal(err)
	}
	files := make([]CompletionSealFile, 0, len(entries))
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(seed, "git", "objects", "pack", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sealDir, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
		d := sha256.Sum256(b)
		files = append(files, CompletionSealFile{Name: e.Name(), Digest: hex.EncodeToString(d[:])})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	m := CompletionSealManifest{Version: 1, RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format, SealedSHA: seeded.BaseSHA, Tree: strings.TrimSpace(string(tree)), ObjectCount: seeded.ObjectCount, ClosureDigest: seeded.ClosureDigest, LocalRepoUUID: uuid, Files: files}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	d := sha256.Sum256(body)
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(sealDir)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	got, err := RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format, SealedSHA: seeded.BaseSHA, ClosureDigest: seeded.ClosureDigest, ManifestDigest: hex.EncodeToString(d[:]), Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int64(st.Uid)})
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseSHA != seeded.BaseSHA || got.ClosureDigest != seeded.ClosureDigest {
		t.Fatalf("rebuild=%+v seed=%+v", got, seeded)
	}
	// A complete receipt has one matching .pack/.idx basename, not merely two
	// pack-looking files. This fails before an attacker-controlled filename is
	// opened from the seal directory.
	mismatched := m
	mismatched.Files = append([]CompletionSealFile(nil), m.Files...)
	changedIndex := false
	for i := range mismatched.Files {
		if strings.HasSuffix(mismatched.Files[i].Name, ".idx") {
			mismatched.Files[i].Name = strings.TrimSuffix(mismatched.Files[i].Name, ".idx") + "-mismatch.idx"
			changedIndex = true
		}
	}
	if !changedIndex {
		t.Fatal("test fixture did not contain an index")
	}
	mismatchedBody, err := json.Marshal(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	mismatchedDigest := sha256.Sum256(mismatchedBody)
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), mismatchedBody, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format, SealedSHA: seeded.BaseSHA, ClosureDigest: seeded.ClosureDigest, ManifestDigest: hex.EncodeToString(mismatchedDigest[:]), Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int64(st.Uid)})
	if err == nil || !strings.Contains(err.Error(), "invalid pack entry") {
		t.Fatalf("mismatched pack pair error = %v", err)
	}
	badTree := m
	badTree.Tree = strings.Repeat("0", len(badTree.Tree))
	badTreeBody, err := json.Marshal(badTree)
	if err != nil {
		t.Fatal(err)
	}
	badTreeDigest := sha256.Sum256(badTreeBody)
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), badTreeBody, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format, SealedSHA: seeded.BaseSHA, ClosureDigest: seeded.ClosureDigest, ManifestDigest: hex.EncodeToString(badTreeDigest[:]), Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int64(st.Uid)})
	if err == nil || !strings.Contains(err.Error(), "tree does not match") {
		t.Fatalf("mismatched manifest tree error = %v", err)
	}
	badCount := m
	badCount.ObjectCount++
	badCountBody, err := json.Marshal(badCount)
	if err != nil {
		t.Fatal(err)
	}
	badCountDigest := sha256.Sum256(badCountBody)
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), badCountBody, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format, SealedSHA: seeded.BaseSHA, ClosureDigest: seeded.ClosureDigest, ManifestDigest: hex.EncodeToString(badCountDigest[:]), Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int64(st.Uid)})
	if err == nil || !strings.Contains(err.Error(), "object count does not match") {
		t.Fatalf("mismatched manifest count error = %v", err)
	}
	// A second document must not be silently ignored after a valid first one.
	multi := append(append([]byte(nil), body...), []byte("\n{}")...)
	multiDigest := sha256.Sum256(multi)
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), multi, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format, SealedSHA: seeded.BaseSHA, ClosureDigest: seeded.ClosureDigest, ManifestDigest: hex.EncodeToString(multiDigest[:]), Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int64(st.Uid)})
	if err == nil || !strings.Contains(err.Error(), "manifest is malformed") {
		t.Fatalf("multiple manifest documents error = %v", err)
	}
}

func TestRebuildAcceptedCompletionSealRejectsWrongRootIdentityBeforeManifestRead(t *testing.T) {
	sealDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RebuildAcceptedCompletionSeal(sealDir, mkTaskChildren(t), AcceptedCompletionSeal{
		Device: "0", Inode: "0", OwnerUID: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "different filesystem object") {
		t.Fatalf("wrong identity error = %v", err)
	}
}

// buildSealFrom freezes a populated task store into an accepted-seal directory
// (pack/index + manifest) and returns its root plus manifest digest.
func buildSealFrom(t *testing.T, store string, spec FirstTaskSetupSpec, seeded SetupResult) (string, string) {
	t.Helper()
	tree, err := gitAtSource(filepath.Join(store, "source"), "rev-parse", "--verify", seeded.BaseSHA+"^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	sealDir := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(store, "git", "objects", "pack"))
	if err != nil {
		t.Fatal(err)
	}
	files := make([]CompletionSealFile, 0, len(entries))
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(store, "git", "objects", "pack", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sealDir, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
		d := sha256.Sum256(b)
		files = append(files, CompletionSealFile{Name: e.Name(), Digest: hex.EncodeToString(d[:])})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	m := CompletionSealManifest{Version: 1, RunID: "worker", TaskID: spec.TaskID, CompletionRequest: "0011223344556677",
		ObjectFormat: spec.ObjectFormat, SealedSHA: seeded.BaseSHA, Tree: strings.TrimSpace(string(tree)),
		ObjectCount: seeded.ObjectCount, ClosureDigest: seeded.ClosureDigest, LocalRepoUUID: spec.LocalRepoUUID, Files: files}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	d := sha256.Sum256(body)
	return sealDir, hex.EncodeToString(d[:])
}

// The accepted-seal rebuild exists precisely to launder a POPULATED canonical
// store the Worker may have damaged after it sealed (ADR-017:533-537 "discarding
// late branch moves and dangling objects even if the Worker damaged its local
// repository after completion"; ADR-016:758-760 "despite late loose residue").
// It must therefore replace the existing store in place, not refuse it — the
// empty-child precondition is scoped to "the first setup action" (ADR-017:439).
func TestRebuildAcceptedCompletionSealReplacesADamagedCanonicalStore(t *testing.T) {
	src, _, format := buildSourceRepo(t)
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	spec := FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format, LocalRepoUUID: uuid}
	store := mkTaskChildren(t)
	seeded, err := MaterializeFirstTaskStore(src, store, spec)
	if err != nil {
		t.Fatal(err)
	}
	sealDir, manifestDigest := buildSealFrom(t, store, spec, seeded)

	// Damage the canonical store the way a post-seal Worker can: drop loose
	// residue in the worktree and clobber the sole branch to a bogus SHA.
	junk := filepath.Join(store, "source", "late-residue.txt")
	if err := os.WriteFile(junk, []byte("written after the seal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	branch := filepath.Join(store, "git", "refs", "heads", "mc", "task-7")
	if err := os.WriteFile(branch, []byte(strings.Repeat("0", len(seeded.BaseSHA))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(sealDir)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	got, err := RebuildAcceptedCompletionSeal(sealDir, store, AcceptedCompletionSeal{
		RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: format,
		SealedSHA: seeded.BaseSHA, ClosureDigest: seeded.ClosureDigest, ManifestDigest: manifestDigest,
		Device: strconv.FormatUint(uint64(st.Dev), 10), Inode: strconv.FormatUint(st.Ino, 10), OwnerUID: int64(st.Uid)})
	if err != nil {
		t.Fatalf("rebuild over a populated canonical store: %v", err)
	}
	if got.BaseSHA != seeded.BaseSHA || got.ClosureDigest != seeded.ClosureDigest {
		t.Fatalf("rebuild=%+v seed=%+v", got, seeded)
	}
	// The late residue is gone and the branch is back at the sealed SHA: the
	// rebuild started from the seal, never from a later filesystem observation.
	if _, err := os.Lstat(junk); !os.IsNotExist(err) {
		t.Fatalf("late residue survived the rebuild: %v", err)
	}
	ref, err := os.ReadFile(branch)
	if err != nil || strings.TrimSpace(string(ref)) != seeded.BaseSHA {
		t.Fatalf("sole branch = (%q, %v), want the sealed SHA %s", strings.TrimSpace(string(ref)), err, seeded.BaseSHA)
	}
}

// First-task creation keeps its empty precondition: only the rebuild launders.
func TestMaterializeFirstTaskStoreStillRefusesResidueOnFirstCreate(t *testing.T) {
	src, _, format := buildSourceRepo(t)
	store := mkTaskChildren(t)
	if err := os.WriteFile(filepath.Join(store, "source", "residue"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := MaterializeFirstTaskStore(src, store, FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD",
		ObjectFormat: format, LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"})
	if err == nil || !strings.Contains(err.Error(), "already holds residue") {
		t.Fatalf("first create over residue = %v, want a residue refusal", err)
	}
}
