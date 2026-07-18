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
