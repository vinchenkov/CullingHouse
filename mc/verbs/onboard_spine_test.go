package verbs

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/substrate"
)

func absentMirrorRequest() OnboardSpineRequest {
	return OnboardSpineRequest{
		ProtocolVersion: 1,
		SchemaVersion:   substrate.CurrentSchemaVersion,
		MirrorState:     "absent",
	}
}

func TestOnboardSpinePrivateStateMatrix(t *testing.T) {
	t.Run("empty volume and absent mirror initialize exactly once", func(t *testing.T) {
		spine := filepath.Join(t.TempDir(), "spine.db")
		got, err := OnboardSpine(spine, absentMirrorRequest())
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "initialized" || got.DeploymentUUID == "" || got.SchemaVersion != substrate.CurrentSchemaVersion {
			t.Fatalf("result = %#v", got)
		}
		inspection, err := inspectSpineReadOnly(spine)
		if err != nil {
			t.Fatal(err)
		}
		if !inspection.hasMeta || inspection.uuid != got.DeploymentUUID {
			t.Fatalf("inspection = %#v, result = %#v", inspection, got)
		}
	})

	t.Run("committed meta with absent mirror repairs only the host mirror", func(t *testing.T) {
		spine := filepath.Join(t.TempDir(), "spine.db")
		first, err := OnboardSpine(spine, absentMirrorRequest())
		if err != nil {
			t.Fatal(err)
		}
		got, err := OnboardSpine(spine, absentMirrorRequest())
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "repair-mirror" || got.DeploymentUUID != first.DeploymentUUID {
			t.Fatalf("repair = %#v, first = %#v", got, first)
		}
	})

	t.Run("matching mirror is idempotent and mismatch refuses", func(t *testing.T) {
		spine := filepath.Join(t.TempDir(), "spine.db")
		first, err := OnboardSpine(spine, absentMirrorRequest())
		if err != nil {
			t.Fatal(err)
		}
		match := absentMirrorRequest()
		match.MirrorState = "present"
		match.MirrorUUID = first.DeploymentUUID
		got, err := OnboardSpine(spine, match)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "ok" {
			t.Fatalf("matching replay = %#v", got)
		}
		match.MirrorUUID = strings.Repeat("f", 32)
		if _, err := OnboardSpine(spine, match); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
			t.Fatalf("mismatch error = %v", err)
		}
	})

	t.Run("existing mirror plus empty volume is spine loss", func(t *testing.T) {
		spine := filepath.Join(t.TempDir(), "spine.db")
		req := absentMirrorRequest()
		req.MirrorState = "present"
		req.MirrorUUID = strings.Repeat("a", 32)
		if _, err := OnboardSpine(spine, req); err == nil || !strings.Contains(err.Error(), "spine lost") {
			t.Fatalf("spine-loss error = %v", err)
		}
	})

	t.Run("non-empty volume without meta is never adopted", func(t *testing.T) {
		spine := filepath.Join(t.TempDir(), "spine.db")
		db, err := sql.Open("sqlite", spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE foreign_table (id INTEGER PRIMARY KEY)`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := OnboardSpine(spine, absentMirrorRequest()); err == nil || !strings.Contains(err.Error(), "no meta identity") {
			t.Fatalf("foreign-spine error = %v", err)
		}
	})

	t.Run("recognized old schema migrates and newer schema refuses", func(t *testing.T) {
		spine := filepath.Join(t.TempDir(), "spine.db")
		db, err := sql.Open("sqlite", spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(schemaV1ForOnboard(t)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO meta (id, deployment_uuid, schema_version)
			VALUES (1, 'old-deployment', 1)`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := OnboardSpine(spine, absentMirrorRequest())
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "migrated-repair-mirror" || got.SchemaVersion != substrate.CurrentSchemaVersion {
			t.Fatalf("migration result = %#v", got)
		}
		db, err = sql.Open("sqlite", spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE meta SET schema_version = ? WHERE id = 1`, substrate.CurrentSchemaVersion+1); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err := OnboardSpine(spine, absentMirrorRequest()); err == nil || !strings.Contains(err.Error(), "newer") {
			t.Fatalf("newer-schema error = %v", err)
		}
	})
}

func TestOnboardSpineFrameCarriesNoPathOrConfigField(t *testing.T) {
	b, err := json.Marshal(absentMirrorRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"path", "home", "config", "credential", "routing", "worksource"} {
		if strings.Contains(strings.ToLower(string(b)), forbidden) {
			t.Fatalf("private frame carries %q: %s", forbidden, b)
		}
	}
}

func TestProductionHomeHostHalfPublishesOnlyReturnedMirror(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mc-home")
	t.Setenv("MC_HOME", home)
	req, err := PrepareOnboardHome()
	if err != nil {
		t.Fatal(err)
	}
	if req.MirrorState != "absent" || req.MirrorUUID != "" {
		t.Fatalf("fresh request = %#v", req)
	}
	uuid := strings.Repeat("a", 32)
	status, _, err := FinalizeOnboardHome(OnboardSpineResult{
		ProtocolVersion: 1, Status: "initialized", DeploymentUUID: uuid,
		SchemaVersion: substrate.CurrentSchemaVersion,
	})
	if err != nil || status != "done" {
		t.Fatalf("finalize status=%q err=%v", status, err)
	}
	b, err := os.ReadFile(filepath.Join(home, deploymentUUIDFilename))
	if err != nil || string(b) != uuid+"\n" {
		t.Fatalf("mirror = %q err=%v", b, err)
	}
	if _, err := os.Stat(filepath.Join(home, "spine.db")); !os.IsNotExist(err) {
		t.Fatalf("host half created/opened a spine: %v", err)
	}
	req, err = PrepareOnboardHome()
	if err != nil || req.MirrorState != "present" || req.MirrorUUID != uuid {
		t.Fatalf("repeat request = %#v err=%v", req, err)
	}
}

func TestProductionHomeHostHalfRejectsUntrustedHelperResponse(t *testing.T) {
	t.Setenv("MC_HOME", filepath.Join(t.TempDir(), "mc-home"))
	base := OnboardSpineResult{
		ProtocolVersion: 1, Status: "initialized", DeploymentUUID: strings.Repeat("a", 32),
		SchemaVersion: substrate.CurrentSchemaVersion,
	}
	bad := base
	bad.DeploymentUUID = "not-a-uuid"
	if _, _, err := FinalizeOnboardHome(bad); err == nil {
		t.Fatal("invalid helper UUID was accepted")
	}
	bad = base
	bad.Status = "invented"
	if _, _, err := FinalizeOnboardHome(bad); err == nil {
		t.Fatal("invalid helper status was accepted")
	}
	if _, err := os.Stat(os.Getenv("MC_HOME")); !os.IsNotExist(err) {
		t.Fatalf("rejected helper response wrote MC_HOME: %v", err)
	}
}
