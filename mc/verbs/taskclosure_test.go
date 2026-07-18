package verbs

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// tcSkeleton creates the exact resident-precreated first-task state for task
// id 7 under a fresh workspace root: empty writable source/ and git/ children
// beneath the mode-0555 task root. It returns (workspaceRoot, taskRoot).
func tcSkeleton(t *testing.T) (string, string) {
	t.Helper()
	ws := grWorkspace(t)
	root := filepath.Join(ws, ".mission-control", "tasks", "task-7")
	for _, dir := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	return ws, root
}

// tcRegistered arms the spine with the live standalone Worker run/lease for
// task 7 and registers the durable receipt from the real root identity.
func tcRegistered(t *testing.T, db *sql.DB, root string) TaskSetupReceipt {
	t.Helper()
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	receipt := TaskSetupReceipt{RunID: "setup-run", TaskID: 7, Root: TaskSetupIdentity{
		Device:   strconv.FormatUint(uint64(st.Dev), 10),
		Inode:    strconv.FormatUint(st.Ino, 10),
		OwnerUID: int(st.Uid),
	}}
	if _, err := RegisterFirstTaskSetup(db, receipt); err != nil {
		t.Fatalf("register receipt: %v", err)
	}
	return receipt
}

func tcStem() string { return strings.Repeat("a", 40) }

func tcClosure() FirstTaskClosure {
	files := []FirstTaskClosureFile{
		{Name: "pack-" + tcStem() + ".pack", Body: []byte("pack")},
		{Name: "pack-" + tcStem() + ".idx", Body: []byte("idx")},
	}
	return FirstTaskClosure{Digest: firstTaskClosureDigest(files), Files: files}
}

func tcAssertEmptyChildren(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"source", "git"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("refused closure write left residue in %s: %v", dir, entries)
		}
	}
}

func TestFirstTaskClosureDigestMatchesItsGoldenVector(t *testing.T) {
	files := []FirstTaskClosureFile{
		{Name: "pack-" + tcStem() + ".pack", Body: []byte("pack")},
		{Name: "pack-" + tcStem() + ".idx", Body: []byte("idx")},
	}
	const want = "b715a259058571cbc403b793b1f5b7f9f36aac6610edc602cba24c13ec9a2e08"
	if got := firstTaskClosureDigest(files); got != want {
		t.Fatalf("digest = %q, want golden %q", got, want)
	}
	reversed := []FirstTaskClosureFile{files[1], files[0]}
	if got := firstTaskClosureDigest(reversed); got != want {
		t.Fatalf("digest is input-order dependent: %q != %q", got, want)
	}
}

func TestWriteFirstTaskSetupClosurePopulatesThePinnedTableAndInspects(t *testing.T) {
	db := dvSpine(t)
	ws, root := tcSkeleton(t)
	receipt := tcRegistered(t, db, root)

	got, rows, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, tcClosure())
	if err != nil {
		t.Fatalf("write closure: %v", err)
	}
	if got.Receipt != receipt || got.Canonical != root {
		t.Fatalf("attested root = %+v, want receipt %+v at %q", got, receipt, root)
	}
	if len(rows) != len(taskPlanRows(7)) {
		t.Fatalf("inspected rows = %d, want %d", len(rows), len(taskPlanRows(7)))
	}

	for _, row := range taskPlanRows(7) {
		if row.Rel == "" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(row.Rel))
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("row %s: %v", row.Rel, err)
		}
		if info.IsDir() != row.IsDir {
			t.Fatalf("row %s: dirness = %v, want %v", row.Rel, info.IsDir(), row.IsDir)
		}
		if row.WantBytes != nil {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, row.WantBytes) {
				t.Fatalf("row %s: body %q, want pinned %q", row.Rel, body, row.WantBytes)
			}
			if info.Mode().Perm() != 0o444 {
				t.Fatalf("row %s: mode %o, want generated 0444 cover", row.Rel, info.Mode().Perm())
			}
		}
		if row.MustBeEmptyDir && info.Mode().Perm() != 0o555 {
			t.Fatalf("row %s: mode %o, want generated 0555 empty cover", row.Rel, info.Mode().Perm())
		}
	}
	for _, rel := range []string{"git/objects", "git/worktrees", "git/worktrees/mc-task-7", "git/objects/pack"} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
			t.Fatalf("interior dir %s = (%v, %v), want a mode-0755 directory", rel, info, err)
		}
	}
	for _, f := range tcClosure().Files {
		path := filepath.Join(root, "git", "objects", "pack", f.Name)
		body, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(body, f.Body) {
			t.Fatalf("closure file %s = (%q, %v), want exact pinned bytes %q", f.Name, body, err, f.Body)
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != 0o444 {
			t.Fatalf("closure file %s mode = (%v, %v), want 0444", f.Name, info.Mode(), err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "git", "objects", "pack"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("pack store = (%v, %v), want exactly the two pinned closure files", entries, err)
	}

	if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, tcClosure()); err == nil {
		t.Fatal("a second closure write over its own residue was accepted")
	}
}

func TestWriteFirstTaskSetupClosureRefusesADigestMismatch(t *testing.T) {
	db := dvSpine(t)
	ws, root := tcSkeleton(t)
	tcRegistered(t, db, root)

	closure := tcClosure()
	closure.Digest = strings.Repeat("0", 64)
	if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, closure); err == nil {
		t.Fatal("a closure that does not reproduce its pinned digest was accepted")
	}
	tcAssertEmptyChildren(t, root)
}

func TestWriteFirstTaskSetupClosureRefusesAMalformedClosureSet(t *testing.T) {
	db := dvSpine(t)
	ws, root := tcSkeleton(t)
	tcRegistered(t, db, root)

	pack := func(name string, body []byte) FirstTaskClosureFile {
		return FirstTaskClosureFile{Name: name, Body: body}
	}
	stem, other := tcStem(), strings.Repeat("b", 40)
	cases := map[string][]FirstTaskClosureFile{
		"empty set":        {},
		"pack without idx": {pack("pack-"+stem+".pack", []byte("pack"))},
		"mismatched stems": {pack("pack-"+stem+".pack", []byte("pack")), pack("pack-"+other+".idx", []byte("idx"))},
		"uppercase stem":   {pack("pack-"+strings.ToUpper(stem)+".pack", []byte("pack")), pack("pack-"+strings.ToUpper(stem)+".idx", []byte("idx"))},
		"traversal name":   {pack("pack-../"+stem+".pack", []byte("pack")), pack("pack-"+stem+".idx", []byte("idx"))},
		"empty body":       {pack("pack-"+stem+".pack", []byte{}), pack("pack-"+stem+".idx", []byte("idx"))},
		"duplicate names":  {pack("pack-"+stem+".pack", []byte("pack")), pack("pack-"+stem+".pack", []byte("pack"))},
		"third file":       {pack("pack-"+stem+".pack", []byte("pack")), pack("pack-"+stem+".idx", []byte("idx")), pack("pack-"+other+".pack", []byte("pack"))},
	}
	for name, files := range cases {
		closure := FirstTaskClosure{Digest: firstTaskClosureDigest(files), Files: files}
		if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, closure); err == nil {
			t.Fatalf("malformed closure set %q was accepted", name)
		}
	}
	tcAssertEmptyChildren(t, root)
}

func TestWriteFirstTaskSetupClosureBoundsAdmittedBytesBeforeHashing(t *testing.T) {
	db := dvSpine(t)
	ws, root := tcSkeleton(t)
	tcRegistered(t, db, root)

	files := []FirstTaskClosureFile{
		{Name: "pack-" + tcStem() + ".pack", Body: make([]byte, maxFirstTaskClosureBytes)},
		{Name: "pack-" + tcStem() + ".idx", Body: []byte("idx")},
	}
	closure := FirstTaskClosure{Digest: strings.Repeat("0", 64), Files: files}
	if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, closure); err == nil {
		t.Fatal("a closure beyond the admission bound was accepted")
	}
	tcAssertEmptyChildren(t, root)
}

func TestWriteFirstTaskSetupClosureRefusesResidueWithoutCleanup(t *testing.T) {
	db := dvSpine(t)
	ws, root := tcSkeleton(t)
	tcRegistered(t, db, root)

	residue := filepath.Join(root, "git", "config")
	if err := os.WriteFile(residue, []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, tcClosure()); err == nil {
		t.Fatal("a non-empty resident skeleton was accepted for closure materialization")
	}
	body, err := os.ReadFile(residue)
	if err != nil || string(body) != "residue" {
		t.Fatalf("refused closure write disturbed residue: (%q, %v)", body, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "source"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("refused closure write touched source/: (%v, %v)", entries, err)
	}
}

func TestWriteFirstTaskSetupClosureRequiresTheLiveReceiptGate(t *testing.T) {
	db := dvSpine(t)
	ws, root := tcSkeleton(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)

	if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, tcClosure()); err == nil {
		t.Fatal("closure write without a registered receipt was accepted")
	}
	tcAssertEmptyChildren(t, root)

	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	receipt := TaskSetupReceipt{RunID: "setup-run", TaskID: 7, Root: TaskSetupIdentity{
		Device:   strconv.FormatUint(uint64(st.Dev), 10),
		Inode:    strconv.FormatUint(st.Ino, 10),
		OwnerUID: int(st.Uid),
	}}
	if _, err := RegisterFirstTaskSetup(db, receipt); err != nil {
		t.Fatalf("register receipt: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteFirstTaskSetupClosure(db, "setup-run", ws, tcClosure()); err == nil {
		t.Fatal("closure write over a drifted root identity was accepted")
	}
}
