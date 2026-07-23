package verbs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mc/substrate"
)

const transferTestUUID = "0123456789abcdef0123456789abcdef"

func transferFixture(t *testing.T) (string, string, SpineTransferRequest) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, deploymentUUIDFilename), []byte(transferTestUUID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spineRoot := filepath.Join(root, "volume")
	if err := os.Mkdir(spineRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	spine := filepath.Join(spineRoot, "spine.db")
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(substrate.Schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, ?, ?)`, transferTestUUID, substrate.CurrentSchemaVersion); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE transfer_sentinel (id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO transfer_sentinel (id, value) VALUES (7, 'restore sentinel')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	req := SpineTransferRequest{
		ProtocolVersion: spineTransferProtocol, ReleaseBuildID: "release-test",
		ControlVersion: 1, SpineSchemaVersion: substrate.CurrentSchemaVersion,
		ConfigSchemaVersion: 1, DeploymentUUID: transferTestUUID,
	}
	return home, spine, req
}

func exportTransfer(t *testing.T, spine string, req SpineTransferRequest) []byte {
	t.Helper()
	var stream bytes.Buffer
	if err := ExportSpineBackup(spine, req, req.ReleaseBuildID, req.ControlVersion, req.ConfigSchemaVersion, &stream); err != nil {
		t.Fatal(err)
	}
	return stream.Bytes()
}

func TestPrepareSpineTransferBindsOwnerOnlyHomeAndMirror(t *testing.T) {
	home, _, want := transferFixture(t)
	t.Setenv("MC_HOME", home)
	got, gotHome, err := PrepareSpineTransfer(want.ReleaseBuildID, want.ControlVersion, want.ConfigSchemaVersion)
	if err != nil || got != want || gotHome != home {
		t.Fatalf("prepared request=%+v home=%q err=%v", got, gotHome, err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PrepareSpineTransfer(want.ReleaseBuildID, want.ControlVersion, want.ConfigSchemaVersion); err == nil || !strings.Contains(err.Error(), "owner-only") {
		t.Fatalf("widened MC_HOME accepted: %v", err)
	}
}

func TestSpineBackupCrossingPublishesClosedSnapshotAndRetention(t *testing.T) {
	home, spine, req := transferFixture(t)
	stream := exportTransfer(t, spine, req)
	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	first, size, err := PublishSpineBackup(home, req, bytes.NewReader(stream), base)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(first)
	if err != nil || info.Mode().Perm() != 0o600 || size != info.Size() {
		t.Fatalf("published snapshot shape info=%v size=%d err=%v", info, size, err)
	}
	inspection, err := snapshotInspection(first)
	if err != nil || inspection.uuid != transferTestUUID || inspection.version != substrate.CurrentSchemaVersion {
		t.Fatalf("published snapshot inspection=%+v err=%v", inspection, err)
	}
	unrelated := filepath.Join(home, "backups", "spine-operator-notes.db")
	if err := os.WriteFile(unrelated, []byte("do not prune"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < defaultBackupRetain+3; i++ {
		if _, _, err := PublishSpineBackup(home, req, bytes.NewReader(stream), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(home, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for _, entry := range entries {
		if backupSnapshotName.MatchString(entry.Name()) {
			valid++
		}
	}
	if valid != defaultBackupRetain {
		t.Fatalf("retention left %d snapshots, want %d", valid, defaultBackupRetain)
	}
	if body, err := os.ReadFile(unrelated); err != nil || string(body) != "do not prune" {
		t.Fatalf("retention touched unrelated file: %q %v", body, err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("oldest snapshot survived retention: %v", err)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatalf("source stopped accepting opens after backup: %v", err)
	}
	db.Close()
}

func TestSpineBackupRefusesSymlinkedOrWidenedBackupRoot(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, home string){
		"symlink": func(t *testing.T, home string) {
			target := filepath.Join(filepath.Dir(home), "target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(home, "backups")); err != nil {
				t.Fatal(err)
			}
		},
		"widened": func(t *testing.T, home string) {
			if err := os.Mkdir(filepath.Join(home, "backups"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			home, spine, req := transferFixture(t)
			prepare(t, home)
			stream := exportTransfer(t, spine, req)
			if _, _, err := PublishSpineBackup(home, req, bytes.NewReader(stream), time.Now()); err == nil || !strings.Contains(err.Error(), "owner-only") {
				t.Fatalf("unsafe backup root accepted: %v", err)
			}
		})
	}
}

func TestSpineBackupCrossingRejectsTruncationAndIdentityDriftWithoutResidue(t *testing.T) {
	home, spine, req := transferFixture(t)
	stream := exportTransfer(t, spine, req)
	for name, body := range map[string][]byte{
		"truncated": stream[:len(stream)-1],
		"trailing":  append(append([]byte(nil), stream...), 'x'),
	} {
		if _, _, err := PublishSpineBackup(home, req, bytes.NewReader(body), time.Now()); err == nil {
			t.Fatalf("%s stream accepted", name)
		}
	}
	drift := req
	drift.DeploymentUUID = strings.Repeat("a", 32)
	if _, _, err := PublishSpineBackup(home, drift, bytes.NewReader(stream), time.Now()); err == nil {
		t.Fatal("identity-drifted response accepted")
	}
	entries, err := os.ReadDir(filepath.Join(home, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed publications left residue: %v", entries)
	}
}

func TestSpineRestoreAdmitsNewestMatchingSnapshotOnlyIntoLostSlot(t *testing.T) {
	home, spine, req := transferFixture(t)
	stream := exportTransfer(t, spine, req)
	backup, _, err := PublishSpineBackup(home, req, bytes.NewReader(stream), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(spine); err != nil {
		t.Fatal(err)
	}
	for _, sibling := range []string{spine + "-wal", spine + "-shm"} {
		_ = os.Remove(sibling)
	}
	file, header, selected, err := OpenLatestSpineBackup(home, req)
	if err != nil {
		t.Fatal(err)
	}
	if selected != backup {
		t.Fatalf("selected %q, want %q", selected, backup)
	}
	framed, err := FrameSpineBackup(file, header)
	if err != nil {
		t.Fatal(err)
	}
	defer framed.Close()
	result, err := RestoreSpine(spine, req.ReleaseBuildID, req.ControlVersion, req.ConfigSchemaVersion, framed)
	if err != nil || result != header {
		t.Fatalf("restore result=%+v err=%v", result, err)
	}
	inspection, err := inspectSpineReadOnly(spine)
	if err != nil || inspection.uuid != transferTestUUID || inspection.version != substrate.CurrentSchemaVersion {
		t.Fatalf("restored spine inspection=%+v err=%v", inspection, err)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title string
	if err := db.QueryRow(`SELECT value FROM transfer_sentinel WHERE id = 7`).Scan(&title); err != nil || title != "restore sentinel" {
		t.Fatalf("restored sentinel title=%q err=%v", title, err)
	}
}

func TestSpineRestoreReplaysHealthyTargetAndRefusesBadStreamWithoutChangingIt(t *testing.T) {
	_, spine, req := transferFixture(t)
	stream := exportTransfer(t, spine, req)
	if _, err := RestoreSpine(spine, req.ReleaseBuildID, req.ControlVersion, req.ConfigSchemaVersion, bytes.NewReader(stream)); err != nil {
		t.Fatalf("healthy restore replay failed: %v", err)
	}
	inspection, err := inspectSpineReadOnly(spine)
	if err != nil || inspection.uuid != transferTestUUID {
		t.Fatalf("refusal changed target: %+v %v", inspection, err)
	}
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatal(err)
	}
	foreignUUID := strings.Repeat("f", 32)
	if _, err := db.Exec(`UPDATE meta SET deployment_uuid = ? WHERE id = 1`, foreignUUID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreSpine(spine, req.ReleaseBuildID, req.ControlVersion, req.ConfigSchemaVersion, bytes.NewReader(stream)); err == nil || !strings.Contains(err.Error(), "foreign") {
		t.Fatalf("foreign non-empty target accepted: %v", err)
	}
	inspection, err = inspectSpineReadOnly(spine)
	if err != nil || inspection.uuid != foreignUUID {
		t.Fatalf("foreign refusal changed target: %+v %v", inspection, err)
	}
	if err := os.Remove(spine); err != nil {
		t.Fatal(err)
	}
	for _, sibling := range []string{spine + "-wal", spine + "-shm"} {
		_ = os.Remove(sibling)
	}
	bad := stream[:len(stream)-1]
	if _, err := RestoreSpine(spine, req.ReleaseBuildID, req.ControlVersion, req.ConfigSchemaVersion, bytes.NewReader(bad)); err == nil {
		t.Fatal("truncated restore accepted")
	}
	if _, err := os.Stat(spine); !os.IsNotExist(err) {
		t.Fatalf("failed restore materialized target: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(spine))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".restore-") {
			t.Fatalf("failed restore left stage %q", entry.Name())
		}
	}
}
