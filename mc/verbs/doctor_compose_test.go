package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/substrate"
)

func doctorTestRequest() DoctorRuntimeRequest {
	return DoctorRuntimeRequest{
		ProtocolVersion: 1, ReleaseBuildID: "development", ControlVersion: 1,
		SpineSchemaVersion: substrate.CurrentSchemaVersion, ConfigSchemaVersion: 1,
	}
}

func doctorHostFixture() []DoctorFinding {
	return []DoctorFinding{
		{Check: "mc-home", Status: "ok", Detail: "home", OnboardSection: "home"},
		{Check: "routing", Status: "ok", Detail: "routing", OnboardSection: "routing"},
		{Check: "runtime-auth", Status: "deferred", Detail: "auth", OnboardSection: "runtime-auth"},
		{Check: "supervision", Status: "deferred", Detail: "supervision", OnboardSection: "supervision"},
	}
}

func doctorRuntimeFixture(uuid string) DoctorRuntimeReport {
	report := DoctorRuntimeUnavailable(doctorTestRequest(), "fixture")
	report.SpineUUID = uuid
	for i := range report.Findings {
		report.Findings[i].Status = "ok"
		report.Findings[i].Detail = "ok"
	}
	return report
}

func TestComposeDoctorKeepsAuthorityOrderAndComparesOnlyUUIDMirror(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MC_HOME", home)
	uuid := strings.Repeat("a", 32)
	if err := os.WriteFile(filepath.Join(home, deploymentUUIDFilename), []byte(uuid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ComposeDoctor(doctorHostFixture(), doctorRuntimeFixture(uuid))
	if err != nil {
		t.Fatal(err)
	}
	report := res.(map[string]any)
	findings := report["findings"].([]DoctorFinding)
	want := []string{"mc-home", "spine", "deployment-identity", "worksources", "surfaces", "routing", "container-runtime", "runtime-auth", "supervision"}
	for i, finding := range findings {
		if finding.Check != want[i] {
			t.Fatalf("finding %d = %q, want %q", i, finding.Check, want[i])
		}
	}
	if findings[2].Status != "ok" || report["ok"] != true {
		t.Fatalf("composed report = %#v", report)
	}

	mismatch := doctorRuntimeFixture(strings.Repeat("b", 32))
	res, err = ComposeDoctor(doctorHostFixture(), mismatch)
	if err != nil {
		t.Fatal(err)
	}
	findings = res.(map[string]any)["findings"].([]DoctorFinding)
	if findings[2].Status != "fail" || !strings.Contains(findings[2].Detail, "does not match") {
		t.Fatalf("mismatch identity = %#v", findings[2])
	}
}

func TestComposeDoctorRejectsOpenEndedHelperFindings(t *testing.T) {
	t.Setenv("MC_HOME", t.TempDir())
	uuid := strings.Repeat("a", 32)
	base := doctorRuntimeFixture(uuid)
	bad := base
	bad.Findings = append([]DoctorFinding{}, base.Findings...)
	bad.Findings[0].Check = "invented"
	if _, err := ComposeDoctor(doctorHostFixture(), bad); err == nil {
		t.Fatal("unknown helper finding was accepted")
	}
	bad = base
	bad.Findings = append([]DoctorFinding{}, base.Findings...)
	bad.Findings[0].Status = "maybe"
	if _, err := ComposeDoctor(doctorHostFixture(), bad); err == nil {
		t.Fatal("invalid helper status was accepted")
	}
	bad = base
	bad.Findings = bad.Findings[:3]
	if _, err := ComposeDoctor(doctorHostFixture(), bad); err == nil {
		t.Fatal("missing helper finding was accepted")
	}
}

func TestDoctorRuntimeFrameCarriesNoHostFact(t *testing.T) {
	b, err := json.Marshal(doctorTestRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"home", "path", "routing", "mirror", "credential", "worksource"} {
		if strings.Contains(strings.ToLower(string(b)), forbidden) {
			t.Fatalf("runtime doctor frame carries %q: %s", forbidden, b)
		}
	}
}
