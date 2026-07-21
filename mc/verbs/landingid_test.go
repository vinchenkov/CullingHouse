package verbs

import (
	"strings"
	"testing"
)

// ADR-016 D7: `landing_id` is "the first 16 lowercase hex of the
// domain-separated digest of deployment, subject, and exact approved
// packet/run identity". These tests pin the two properties the rest of the
// sealed lane leans on:
//
//   - DETERMINISM. `abortOwnConflictedMerge` (landsealed.go:891) identifies a
//     merge THIS system wrote by matching `MC-Landing-Id: <id>` in MERGE_MSG.
//     A retry after a crashed attempt must derive the identical id or it can
//     no longer recognize — and therefore never undo — its own merge.
//   - GRAMMAR. `mc-landing-<id>` is a container name (ADR-016 D7) and
//     `landsealedrun.go:125` refuses anything that is not 16 lowercase hex.

func landingIdentityFixture() canonicalLandingIdentity {
	return canonicalLandingIdentity{
		DeploymentUUID:    "0f9c1e2a-3b4c-4d5e-8f60-112233445566",
		SubjectID:         7,
		ApprovedRunID:     "a1b2c3d4e5f60718",
		ApprovedRequestID: "00112233445566aa",
	}
}

// The frozen canonical bytes. Same discipline as the dispatchseam vectors:
// declared field order, explicit zero values, no maps or floats. Changing this
// shape changes every landing id in flight, so it is an ADR-worthy event.
func landingIdentityGoldenJSON() string {
	return `{"deployment_uuid":"0f9c1e2a-3b4c-4d5e-8f60-112233445566","subject_id":7,` +
		`"approved_run_id":"a1b2c3d4e5f60718","approved_request_id":"00112233445566aa"}`
}

func TestLandingIdentityCanonicalGoldenVector(t *testing.T) {
	got, err := landingIdentityFixture().bytes()
	if err != nil {
		t.Fatalf("canonical landing identity bytes: %v", err)
	}
	if string(got) != landingIdentityGoldenJSON() {
		t.Fatalf("canonical landing identity bytes drifted from the frozen vector\n got: %s\nwant: %s",
			got, landingIdentityGoldenJSON())
	}
}

func TestDeriveLandingIDIsSixteenLowercaseHex(t *testing.T) {
	got, err := deriveLandingID(landingIdentityFixture())
	if err != nil {
		t.Fatalf("deriveLandingID: %v", err)
	}
	if !validLowercaseHex(got, 16) {
		t.Fatalf("landing id %q is not 16 lowercase hex; landsealedrun.go would refuse it", got)
	}
}

func TestDeriveLandingIDIsDeterministic(t *testing.T) {
	// The property the abort path depends on: a second landing attempt over
	// the same approved identity recognizes the merge the first one wrote.
	first, err := deriveLandingID(landingIdentityFixture())
	if err != nil {
		t.Fatalf("deriveLandingID: %v", err)
	}
	for i := 0; i < 8; i++ {
		again, err := deriveLandingID(landingIdentityFixture())
		if err != nil {
			t.Fatalf("deriveLandingID (attempt %d): %v", i, err)
		}
		if again != first {
			t.Fatalf("landing id is not stable across attempts: %q then %q; the abort path\n"+
				"could not identify its own merge after a retry", first, again)
		}
	}
}

func TestDeriveLandingIDSeparatesEveryInput(t *testing.T) {
	base, err := deriveLandingID(landingIdentityFixture())
	if err != nil {
		t.Fatalf("deriveLandingID: %v", err)
	}
	perturbed := map[string]func(*canonicalLandingIdentity){
		"deployment": func(c *canonicalLandingIdentity) { c.DeploymentUUID = "11111111-2222-4333-8444-555555555555" },
		"subject":    func(c *canonicalLandingIdentity) { c.SubjectID = 8 },
		"run":        func(c *canonicalLandingIdentity) { c.ApprovedRunID = "ffeeddccbbaa9988" },
		// The request id earns its place only if it separates on its own: the
		// accepted seal is the (run, request) pair, and a run that re-sealed
		// under a different request is a different approved authority.
		"request": func(c *canonicalLandingIdentity) { c.ApprovedRequestID = "99887766554433bb" },
	}
	for name, mutate := range perturbed {
		id := landingIdentityFixture()
		mutate(&id)
		got, err := deriveLandingID(id)
		if err != nil {
			t.Fatalf("%s: deriveLandingID: %v", name, err)
		}
		if got == base {
			t.Fatalf("%s: perturbing this input left the landing id unchanged (%q); two distinct\n"+
				"landings would share a container name and an abort trailer", name, got)
		}
	}
}

// The domain separator is what keeps a landing digest from ever colliding with
// a prepare token, a dispatch key, or a plan digest over the same bytes.
func TestLandingIDDomainIsDistinct(t *testing.T) {
	domains := []string{prepareTokenDomain, dispatchKeyDomain, dispatchPlanDomain}
	for _, d := range domains {
		if landingIDDomain == d {
			t.Fatalf("landing id shares domain separator %q with an existing dispatch digest", d)
		}
	}
	if !strings.HasSuffix(landingIDDomain, "\x00") {
		t.Fatalf("landing id domain %q lacks the NUL terminator the other seam domains use", landingIDDomain)
	}
}
