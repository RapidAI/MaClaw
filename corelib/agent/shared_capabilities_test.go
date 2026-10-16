package agent

import "testing"

func TestExtraSharedHostCapabilityNamesStaysEmpty(t *testing.T) {
	if names := ExtraSharedHostCapabilityNames(); len(names) != 0 {
		t.Fatalf("ExtraSharedHost must stay empty after RegisterCoreTools migration, got %v", names)
	}
}

func TestHostPrivateCapabilityNamesDoNotOverlapCore(t *testing.T) {
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})
	core := map[string]bool{}
	for _, name := range r.Names() {
		core[name] = true
	}
	desktop := DesktopOnlyCapabilityNames()
	for name := range HostPrivateCapabilityNames() {
		if core[name] {
			t.Errorf("host-private %q leaked into RegisterCoreTools", name)
		}
		if desktop[name] {
			t.Errorf("host-private %q is already DesktopOnly; keep one list", name)
		}
	}
}
