//go:build test_fake_routing

package routing

// ActiveRegistry adds the deterministic fake family only to explicitly
// tagged test binaries. Production builds cannot resolve it.
func ActiveRegistry() (Registry, bool) {
	r := ProductionRegistry()
	r["fake"] = "fake"
	return r, true
}
