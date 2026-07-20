package verbs

import (
	"os"
	"strings"
	"testing"
)

// inspectSpineReadOnly is the codebase's ONE deliberate bypass of
// substrate.Open: it must not WAL-mutate a foreign database, so it builds its
// own read-only DSN and calls sql.Open directly. That makes it the one spine
// opener whose lock-domain guard is not inherited, and therefore the one that
// can lose it silently.
//
// Nothing behavioral can catch that loss here. The fast lane runs on darwin,
// where substrate's platform mount source is inert, so deleting the guard call
// turns no test red; and the guard's test seam is unexported inside substrate,
// so this package cannot drive it. A source-level assertion is the honest
// remaining option — the same shape as boundary's ADR-017 derivation guard,
// and for the same reason: a check that cannot be executed here is still worth
// pinning against drift.
func TestOnboardInspectionKeepsItsLockDomainGuard(t *testing.T) {
	raw, err := os.ReadFile("onboard.go")
	if err != nil {
		t.Fatalf("read onboard.go: %v", err)
	}
	src := string(raw)

	const marker = "func inspectSpineReadOnly("
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("inspectSpineReadOnly is gone; this guard needs rewriting against whatever replaced it")
	}
	body := src[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}

	guard := strings.Index(body, "substrate.GuardLockDomain(")
	open := strings.Index(body, "sql.Open(")
	if guard < 0 {
		t.Fatalf("inspectSpineReadOnly no longer calls substrate.GuardLockDomain — Inv. 24's guard is bypassed on the read-only path")
	}
	if open < 0 {
		t.Fatalf("inspectSpineReadOnly no longer calls sql.Open; if the bypass is gone this test should be deleted, not weakened")
	}
	if guard > open {
		t.Fatalf("substrate.GuardLockDomain is called AFTER sql.Open — the guard must refuse before the database is opened")
	}
}
