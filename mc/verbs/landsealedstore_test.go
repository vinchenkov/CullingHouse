package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Step 4b: the SEALED side of ADR-017:741-743 — "the sealed local branch,
// reviewed SHA, exact reachable closure/digest, safe local controls".
//
// This asks a different question from `inspectCompletableTaskStore`, which is
// why it is not that function. The completion-seal inspector proves a store is
// COMPLETABLE: its producer holds it RW and the identity facts are OUTPUTS to
// be read off and recorded. Landing proves a store is the EXACT SEALED ARTIFACT
// the immutable assignment already named: it holds the store RO, in a different
// effect class, and the identity facts are INPUTS to match against. The shapes
// rhyme; the direction of proof is opposite.

func buildSealedTaskStore(t *testing.T) (taskRoot string, pins landingStorePins) {
	t.Helper()
	src, _, format := buildSourceRepo(t)
	taskRoot = mkTaskChildren(t)
	seeded, err := MaterializeFirstTaskStore(src, taskRoot, FirstTaskSetupSpec{
		TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	})
	if err != nil {
		t.Fatal(err)
	}
	return taskRoot, landingStorePins{
		TaskID: 7, ObjectFormat: format,
		VerifiedSHA: seeded.BaseSHA, LocalRepoUUID: seeded.LocalRepoUUID,
	}
}

func TestRevalidateSealedTaskStoreAcceptsTheRealSealedStore(t *testing.T) {
	root, pins := buildSealedTaskStore(t)
	facts, err := revalidateSealedTaskStore(root, pins)
	if err != nil {
		t.Fatalf("the real sealed store was refused: %v", err)
	}
	if facts.Head != pins.VerifiedSHA {
		t.Fatalf("head = %q, want the verified SHA %q", facts.Head, pins.VerifiedSHA)
	}
	if len(facts.Tree) != oidLen(pins.ObjectFormat) {
		t.Fatalf("tree %q is not a canonical oid", facts.Tree)
	}
}

// Every frozen fact is a fence. These are the INPUTS landing matches against,
// so a drift in any one of them means the store is not the artifact the
// assignment named — the case that must never merge into the real repository.
func TestRevalidateSealedTaskStoreFencesEveryFrozenFact(t *testing.T) {
	drift := map[string]func(p *landingStorePins){
		"verified sha": func(p *landingStorePins) { p.VerifiedSHA = strings.Repeat("a", len(p.VerifiedSHA)) },
		"repo uuid":    func(p *landingStorePins) { p.LocalRepoUUID = "0f9c1e2a-3b4c-4d5e-8f60-112233445566" },
		"task id":      func(p *landingStorePins) { p.TaskID = 8 },
		"object format": func(p *landingStorePins) {
			if p.ObjectFormat == "sha1" {
				p.ObjectFormat = "sha256"
			} else {
				p.ObjectFormat = "sha1"
			}
		},
	}
	for name, mutate := range drift {
		t.Run(name, func(t *testing.T) {
			root, pins := buildSealedTaskStore(t)
			mutate(&pins)
			if _, err := revalidateSealedTaskStore(root, pins); err == nil {
				t.Fatalf("accepted a sealed store whose %s had drifted", name)
			}
		})
	}
}

func TestRevalidateSealedTaskStoreFencesTheStoreItself(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"a dirty tracked file in the sealed worktree": func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "source", "README.md"), "tampered\n")
		},
		"an untracked file in the sealed worktree": func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "source", "planted.txt"), "planted\n")
		},
		// The index-visibility evasion, ported from legacy mc-land.test.ts:128.
		// A tampered reviewed file plus --assume-unchanged makes `status` report
		// the store as pristine, defeating the dirt fence directly. The real
		// repository was already fenced against this (:198); the sealed side —
		// which is the Worker's own output, the attacker-shaped input here — was
		// not. The config's exact-byte reproduction cannot catch it, because
		// these flags live in the index rather than the config.
		"a tampered reviewed file hidden by --assume-unchanged": func(t *testing.T, root string) {
			src := filepath.Join(root, "source")
			srcGit(t, src, "update-index", "--assume-unchanged", "README.md")
			writeFile(t, filepath.Join(src, "README.md"), "tampered behind the flag\n")
		},
		"a reviewed path hidden by --skip-worktree": func(t *testing.T, root string) {
			src := filepath.Join(root, "source")
			srcGit(t, src, "update-index", "--skip-worktree", "README.md")
			writeFile(t, filepath.Join(src, "README.md"), "tampered behind the flag\n")
		},
		// A second ref is how a sealed store stops being sole-branch. The same
		// check is what keeps refs/replace/* out, which is the sharper attack:
		// a replacement could substitute different bytes for a reviewed object.
		"a second ref beside the managed branch": func(t *testing.T, root string) {
			srcGit(t, filepath.Join(root, "source"), "branch", "extra")
		},
		"a replace ref substituting reviewed bytes": func(t *testing.T, root string) {
			src := filepath.Join(root, "source")
			head := srcGit(t, src, "rev-parse", "HEAD")
			other := srcGit(t, src, "commit-tree", head+"^{tree}", "-m", "substitute")
			srcGit(t, src, "replace", "-f", head, other)
		},
		"object alternates borrowing another store": func(t *testing.T, root string) {
			info := filepath.Join(root, "git", "objects", "info")
			if err := os.MkdirAll(info, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(info, "alternates"), "/some/other/objects\n")
		},
		"a config outside the generated fixed grammar": func(t *testing.T, root string) {
			path := filepath.Join(root, "git", "config")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, string(body)+"[filter \"evil\"]\n\tclean = sh -c 'id'\n")
		},
		// Checking out a NEW branch would be caught by the sole-ref fence before
		// the HEAD fence ever ran (mutation proved it). Detaching moves HEAD
		// while leaving the ref set untouched, so this is the scenario in which
		// the HEAD fence is the only thing standing.
		"HEAD detached from the managed branch": func(t *testing.T, root string) {
			srcGit(t, filepath.Join(root, "source"), "checkout", "-q", "--detach")
		},
		"HEAD moved to a second branch": func(t *testing.T, root string) {
			srcGit(t, filepath.Join(root, "source"), "checkout", "-q", "-b", "elsewhere")
		},
		// fsck is the only fence that sees object-level corruption: a truncated
		// or bit-rotted loose object is structurally invalid while every ref,
		// config and status check above still passes.
		"a corrupt loose object in the store": func(t *testing.T, root string) {
			dir := filepath.Join(root, "git", "objects", "ab")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, strings.Repeat("c", 38)), "not zlib, not an object\n")
		},
		"the managed branch moved off the reviewed commit": func(t *testing.T, root string) {
			src := filepath.Join(root, "source")
			writeFile(t, filepath.Join(src, "extra.txt"), "post-seal\n")
			srcGit(t, src, "add", "-A")
			srcGit(t, src, "commit", "-qm", "post-seal commit")
		},
		"the store is absent entirely": func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join(root, "git")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			root, pins := buildSealedTaskStore(t)
			tamper(t, root)
			if _, err := revalidateSealedTaskStore(root, pins); err == nil {
				t.Fatalf("accepted a sealed store with %s", name)
			}
		})
	}
}

// The sealed store is inspected through the landing fences, not the weaker
// source-reading ones. A hook planted in the sealed store must not fire — the
// store is attacker-shaped input to this class, not our own scratch space.
func TestRevalidateSealedTaskStoreRunsUnderTheLandingFences(t *testing.T) {
	root, pins := buildSealedTaskStore(t)
	hooks := filepath.Join(root, "git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "sealed-hook-fired")
	for _, name := range []string{"post-checkout", "reference-transaction", "post-index-change"} {
		path := filepath.Join(hooks, name)
		writeFile(t, path, "#!/bin/sh\ntouch "+marker+"\n")
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := revalidateSealedTaskStore(root, pins); err != nil {
		t.Fatalf("the sealed store was refused: %v", err)
	}
	if _, err := os.Lstat(marker); err == nil {
		t.Fatal("a hook in the sealed store fired during landing revalidation")
	}
}
