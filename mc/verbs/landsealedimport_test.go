package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Step 4d: the import (ADR-017:743-747).
//
//	"It imports only that closure into the real object store without a
//	 hardlink, alternate, or speculative deletion, then fsyncs and verifies the
//	 imported objects. A crash here may leave unreachable but valid imported
//	 objects; retry reuses them by hash and never guesses them safe to delete."
//
// Every clause of that is a separate fence below. The fixture is built so the
// reviewed objects genuinely do NOT exist in the real repository beforehand —
// landingPair's reviewed commit is created in the repo itself, which would make
// every assertion here pass without importing anything.

// importPair builds a real repository and a SEPARATE sealed store holding a
// reviewed commit the real repository has never seen.
func importPair(t *testing.T) (repo, sealed, base, verified string) {
	t.Helper()
	repo = t.TempDir()
	srcGit(t, repo, "init", "-q", "-b", "main")
	srcGit(t, repo, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(repo, "README.md"), "operator\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "base")
	base = srcGit(t, repo, "rev-parse", "HEAD")

	sealed = filepath.Join(t.TempDir(), "sealed")
	// --no-hardlinks so the two stores share no inodes to begin with; the
	// hardlink fence below would otherwise be measuring the clone, not the
	// import.
	srcGit(t, filepath.Dir(sealed), "clone", "-q", "--no-hardlinks", repo, sealed)
	srcGit(t, sealed, "config", "commit.gpgsign", "false")
	srcGit(t, sealed, "checkout", "-q", "-b", "mc/task-7")
	if err := os.MkdirAll(filepath.Join(sealed, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sealed, "README.md"), "operator\nreviewed\n")
	writeFile(t, filepath.Join(sealed, "lib/new.go"), "package lib\n")
	srcGit(t, sealed, "add", "-A")
	srcGit(t, sealed, "commit", "-qm", "reviewed change")
	verified = srcGit(t, sealed, "rev-parse", "HEAD")

	// Precondition: the real repository must not already have it.
	if err := objectPresent(repo, verified); err == nil {
		t.Fatal("fixture is invalid: the real repository already has the reviewed commit")
	}
	return repo, sealed, base, verified
}

func objectPresent(repo, oid string) error {
	_, err := (landingGit{dir: repo}).out("cat-file", "-e", oid+"^{commit}")
	return err
}

func TestImportSealedClosureLandsTheReviewedObjects(t *testing.T) {
	repo, sealed, base, verified := importPair(t)
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := objectPresent(repo, verified); err != nil {
		t.Fatalf("the reviewed commit is not in the real store after import: %v", err)
	}
	// The whole closure, not just the tip: every object reachable from the
	// reviewed commit and not from the base must be readable in the real store.
	out, err := (landingGit{dir: repo}).out("rev-list", "--objects", verified, "--not", base)
	if err != nil {
		t.Fatalf("the imported closure is not traversable in the real store: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the imported closure is empty")
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, err := (landingGit{dir: repo}).out("cat-file", "-e", fields[0]); err != nil {
			t.Fatalf("closure object %s is missing from the real store", fields[0])
		}
	}
}

// "without a hardlink": no object file in the real store may share an inode
// with the sealed store. A hardlinked import would make the sealed root's
// bytes mutable through the real repository, defeating the RO mount.
func TestImportSealedClosureSharesNoInodeWithTheSealedStore(t *testing.T) {
	repo, sealed, base, verified := importPair(t)
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("import: %v", err)
	}
	sealedInodes := map[uint64]string{}
	collectInodes(t, filepath.Join(sealed, ".git", "objects"), sealedInodes)
	realInodes := map[uint64]string{}
	collectInodes(t, filepath.Join(repo, ".git", "objects"), realInodes)
	for ino, path := range realInodes {
		if other, ok := sealedInodes[ino]; ok {
			t.Fatalf("real store %q shares inode %d with sealed store %q", path, ino, other)
		}
	}
}

// "without an alternate": the real store must not be made to borrow objects
// from the sealed root. An alternate would leave the real repository broken the
// moment the landing container exits and the mount disappears.
func TestImportSealedClosureCreatesNoAlternate(t *testing.T) {
	repo, sealed, base, verified := importPair(t)
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("import: %v", err)
	}
	alternates := filepath.Join(repo, ".git", "objects", "info", "alternates")
	if fileNonEmpty(alternates) {
		body, _ := os.ReadFile(alternates)
		t.Fatalf("the import created an object alternate: %q", string(body))
	}
	// And the objects must survive the sealed store disappearing entirely.
	if err := os.RemoveAll(sealed); err != nil {
		t.Fatal(err)
	}
	if err := objectPresent(repo, verified); err != nil {
		t.Fatalf("imported objects did not survive the sealed store going away: %v", err)
	}
}

// "retry reuses them by hash and never guesses them safe to delete": importing
// twice is inert, and a pre-existing unreachable object is left alone. Landing
// crashes between import and ref creation are expected, so the second attempt
// must be a no-op rather than a conflict.
func TestImportSealedClosureIsIdempotentAndDeletesNothing(t *testing.T) {
	repo, sealed, base, verified := importPair(t)

	// A dangling object the operator (or a prior crashed landing) left behind.
	dangling := strings.TrimSpace(srcGitStdin(t, repo, "unreachable operator blob\n", "hash-object", "-w", "--stdin"))
	if dangling == "" {
		t.Fatal("could not plant a dangling object")
	}
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("second import must be inert, got: %v", err)
	}
	if err := objectPresent(repo, verified); err != nil {
		t.Fatalf("reviewed commit missing after the second import: %v", err)
	}
	if _, err := (landingGit{dir: repo}).out("cat-file", "-e", dangling); err != nil {
		t.Fatal("the import speculatively deleted a pre-existing unreachable object")
	}
}

// The import must refuse a reviewed commit the sealed store does not actually
// contain: otherwise a caller could name any SHA and get a silently empty pack.
func TestImportSealedClosureRefusesAnAbsentReviewedCommit(t *testing.T) {
	repo, sealed, base, _ := importPair(t)
	absent := strings.Repeat("a", 40)
	if err := importSealedClosure(sealed, repo, base, absent); err == nil {
		t.Fatal("imported a closure for a commit the sealed store does not hold")
	}
}

// The import must not create any ref in the real repository. Ref creation is
// the NEXT stage's durable marker (ADR-017:748), and doing it here would make
// a crash mid-import indistinguishable from a completed import.
func TestImportSealedClosureCreatesNoRef(t *testing.T) {
	repo, sealed, base, verified := importPair(t)
	before := srcGit(t, repo, "for-each-ref", "--format=%(refname)")
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("import: %v", err)
	}
	after := srcGit(t, repo, "for-each-ref", "--format=%(refname)")
	if before != after {
		t.Fatalf("the import changed the ref set:\nbefore %q\nafter  %q", before, after)
	}
}

func collectInodes(t *testing.T, root string, into map[uint64]string) {
	t.Helper()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if ino, ok := inodeOf(info); ok {
			into[ino] = path
		}
		return nil
	})
}

func srcGitStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	out, err := gitOutput(dir, sourceGitEnv(), []byte(stdin), args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// A reviewed commit equal to the frozen base has an EMPTY closure. Importing
// nothing and then proceeding to create a ref and merge would land a no-op
// while reporting success. Reachable, and the reason the emptiness check reads
// the pack header rather than the byte length: an empty range still produces a
// 32-byte pack, so a length test can never fire.
func TestImportSealedClosureRefusesAnEmptyClosure(t *testing.T) {
	repo, sealed, base, _ := importPair(t)
	if err := importSealedClosure(sealed, repo, base, base); err == nil {
		t.Fatal("imported an empty closure")
	}
}

func TestPackObjectCountReadsTheHeader(t *testing.T) {
	if _, err := packObjectCount([]byte("not a pack")); err == nil {
		t.Fatal("accepted a non-pack")
	}
	empty := append([]byte("PACK"), 0, 0, 0, 2, 0, 0, 0, 0)
	if n, err := packObjectCount(empty); err != nil || n != 0 {
		t.Fatalf("empty pack header: n=%d err=%v", n, err)
	}
	three := append([]byte("PACK"), 0, 0, 0, 2, 0, 0, 0, 3)
	if n, err := packObjectCount(three); err != nil || n != 3 {
		t.Fatalf("three-object pack header: n=%d err=%v", n, err)
	}
}
