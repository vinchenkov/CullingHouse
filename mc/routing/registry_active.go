//go:build !test_fake_routing

package routing

// ActiveRegistry is the production routing catalog.
func ActiveRegistry() (Registry, bool) { return ProductionRegistry(), false }
