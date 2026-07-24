package substrate_test

import (
	"context"
	"strings"
	"testing"

	"mc/substrate"
)

// An initiative setup receipt is durable cut evidence (ADR-025 D3): once
// recorded it is immutable and undeletable (a retry reuses its recorded cut and
// roots, never re-resolves main), its two root triples are decimal-GLOB and
// typeof-fenced like every other D2 identity, and cut_sha must be a git object
// hash (40 or 64 lowercase hex).
func TestInitiativeSetupReceiptsAreImmutableTypedAndClosed(t *testing.T) {
	db := openSpine(t)
	init := mkTask(t, db, "initiative", "seeded")
	other := mkTask(t, db, "initiative", "seeded")
	sha1hex := strings.Repeat("a", 40)
	sha256hex := strings.Repeat("a", 64)

	// A well-formed sha1 cut commits.
	mustExec(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '42', 501, '17', '99', 501, ?)`, init, sha1hex)

	// A well-formed sha256 cut commits for a different initiative.
	mustExec(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '43', 501, '17', '100', 501, ?)`, other, sha256hex)

	// Immutable and undeletable — the whole reuse discipline.
	wantAbort(t, db, `UPDATE initiative_setup_receipts SET cut_sha=? WHERE initiative_id=?`, strings.Repeat("c", 40), init)
	wantAbort(t, db, `UPDATE initiative_setup_receipts SET store_inode='777' WHERE initiative_id=?`, init)
	wantAbort(t, db, `DELETE FROM initiative_setup_receipts WHERE initiative_id=?`, init)

	// One receipt per initiative: the PK refuses a second cut.
	wantAbort(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '44', 501, '17', '101', 501, ?)`, init, sha1hex)

	third := mkTask(t, db, "initiative", "seeded")
	// cut_sha must be a git hash length (40 or 64), lowercase hex.
	wantAbort(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '45', 501, '17', '102', 501, ?)`, third, strings.Repeat("a", 39))
	wantAbort(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '45', 501, '17', '102', 501, 'ZZZZ')`, third)

	// A negative owner uid is refused (both triples).
	wantAbort(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '45', -1, '17', '102', 501, ?)`, third, sha1hex)

	// A non-decimal device is refused (both triples decimal-GLOB fenced).
	wantAbort(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '45', 501, 'x7', '102', 501, ?)`, third, sha1hex)

	// A BLOB cut_sha bypasses affinity conversion; the typeof fence refuses it.
	wantAbort(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '45', 501, '17', '102', 501,
		        x'61616161616161616161616161616161616161616161616161616161616161616161616161616161')`, third)
}

// A migrated spine must gain the initiative_setup_receipts table with exactly
// the same columns and durable-evidence triggers a fresh spine ships — otherwise
// a deployment upgraded in place could record a mutable or re-cuttable receipt
// the fresh schema refuses.
func TestMigrateAddsInitiativeSetupReceiptsMatchingFresh(t *testing.T) {
	migrated := migratedV1Spine(t)
	fresh := openSpine(t)
	if got, want := columnsOf(t, migrated, "initiative_setup_receipts"), columnsOf(t, fresh, "initiative_setup_receipts"); got != want {
		t.Errorf("initiative_setup_receipts columns after migration:\n  got  %s\n  want %s", got, want)
	}
	if got, want := triggersOf(t, migrated, "initiative_setup_receipts"), triggersOf(t, fresh, "initiative_setup_receipts"); got != want {
		t.Errorf("initiative_setup_receipts triggers after migration:\n  got  %s\n  want %s", got, want)
	}
	if !triggerExists(t, migrated, "initiative_setup_receipts_immutable") || !triggerExists(t, migrated, "initiative_setup_receipts_no_delete") {
		t.Fatal("migrated spine is missing the initiative_setup_receipts durable-evidence fences")
	}
}

// LoadSubjectInitiativeSetup projects the durable receipt for one initiative:
// both root identities and the recorded cut. An absent initiative is a nil,
// not an error — the normal pre-cut state that leaves a child health-refusing.
func TestLoadSubjectInitiativeSetupProjectsTheReceipt(t *testing.T) {
	db := openSpine(t)
	init := mkTask(t, db, "initiative", "seeded")
	cut := strings.Repeat("a", 40)
	mustExec(t, db, `INSERT INTO initiative_setup_receipts
		(initiative_id, store_device, store_inode, store_owner_uid,
		 worktree_device, worktree_inode, worktree_owner_uid, cut_sha)
		VALUES (?, '17', '42', 501, '17', '99', 502, ?)`, init, cut)

	got, err := substrate.LoadSubjectInitiativeSetup(context.Background(), db, init)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := &substrate.DispatchInitiativeSetup{
		StoreRoot:    substrate.DispatchTaskSetupIdentity{Device: "17", Inode: "42", OwnerUID: 501},
		WorktreeRoot: substrate.DispatchTaskSetupIdentity{Device: "17", Inode: "99", OwnerUID: 502},
		CutSHA:       cut,
	}
	if *got != *want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// No receipt row means the pre-cut state: an explicit nil, not an error.
	absent, err := substrate.LoadSubjectInitiativeSetup(context.Background(), db, init+999)
	if err != nil || absent != nil {
		t.Fatalf("no-receipt initiative = (%+v, %v), want nil", absent, err)
	}
}
