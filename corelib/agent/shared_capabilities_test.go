package agent

import "testing"

func TestSharedCoreCapabilityNamesComeFromRegistry(t *testing.T) {
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})
	desktop := DesktopOnlyCapabilityNames()
	shared := map[string]bool{}
	for _, name := range SharedCoreCapabilityNames() {
		shared[name] = true
		if desktop[name] {
			t.Fatalf("desktop-only %s leaked into shared catalog", name)
		}
		if !r.Has(name) {
			t.Fatalf("shared name %s is not registered by RegisterCoreTools", name)
		}
	}
	for _, name := range r.Names() {
		if desktop[name] {
			continue
		}
		if !shared[name] {
			t.Fatalf("RegisterCoreTools name %s missing from SharedCoreCapabilityNames", name)
		}
	}
}
