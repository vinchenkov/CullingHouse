package verbs

import (
	"os"
	"path/filepath"
	"testing"
)

// ADR-022 repurposes the gateway-secret deny class: the resident token
// service's refresh-grant store is real on-disk credential material captured
// at onboarding, and it must be a deny-mount root so no candidate-authored
// mount can bind the real grants into a container.
func TestResolveGatewaySecretRootsDenyMountsTheRefreshGrantStore(t *testing.T) {
	home := t.TempDir()

	t.Run("an absent store registers nothing rather than false evidence", func(t *testing.T) {
		roots, err := resolveGatewaySecretRoots(home)
		if err != nil {
			t.Fatal(err)
		}
		if len(roots) != 0 {
			t.Fatalf("roots = %v, want none for an absent store", roots)
		}
	})

	t.Run("a present store is exactly the deny root", func(t *testing.T) {
		store := filepath.Join(home, "refresh-grants")
		if err := os.MkdirAll(store, 0o700); err != nil {
			t.Fatal(err)
		}
		roots, err := resolveGatewaySecretRoots(home)
		if err != nil {
			t.Fatal(err)
		}
		if len(roots) != 1 {
			t.Fatalf("roots = %v, want exactly the refresh-grant store", roots)
		}
		want := maID(t, store)
		if roots[0].Canonical != want.Canonical {
			t.Fatalf("root = %q, want %q", roots[0].Canonical, want.Canonical)
		}
	})
}
