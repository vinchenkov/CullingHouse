package routing

import "testing"

func TestProductionBindingCatalogIsClosedAndDrivesRegistry(t *testing.T) {
	catalog := ProductionBindings()
	if len(catalog) != 3 {
		t.Fatalf("catalog has %d bindings, want 3", len(catalog))
	}
	registry := ProductionRegistry()
	for _, id := range []string{"chatgpt", "claude", "minimax"} {
		spec, ok := catalog[id]
		if !ok || spec.ID != id || spec.Harness == "" || registry[id] != spec.Harness {
			t.Fatalf("binding %q is incoherent: %+v registry=%q", id, spec, registry[id])
		}
		if spec.Delivery == CredentialProjection && (spec.TokenURL == "" || spec.ClientID == "" || spec.DeclaredStaticKey != "") {
			t.Fatalf("OAuth binding %q is incomplete: %+v", id, spec)
		}
		if spec.Delivery == CredentialMaterialized && (spec.DeclaredStaticKey == "" || spec.TokenURL != "" || spec.ClientID != "") {
			t.Fatalf("static binding %q is incomplete: %+v", id, spec)
		}
	}
	delete(catalog, "chatgpt")
	if _, ok := ProductionBindings()["chatgpt"]; !ok {
		t.Fatal("caller mutation changed the production catalog")
	}
}
