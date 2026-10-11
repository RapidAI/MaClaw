package tool

import (
	"strings"
	"testing"
	"time"
)

func TestBuiltinRegisterRejectsExternalPrefix(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	err := reg.Register(CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "core",
		Effects: []EffectClass{EffectReadOnly},
	})
	if err == nil || !strings.Contains(err.Error(), "RegisterExternalCapability") {
		t.Fatalf("builtin Register must reject the x: prefix, got err=%v", err)
	}
}

func TestRegisterExternalCapabilityHappyPath(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	if err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "vendor",
		Summary: "Translate text with an external vendor service.",
		Effects: []EffectClass{EffectReadOnly, EffectExternalEffect},
	}); err != nil {
		t.Fatalf("register external capability: %v", err)
	}
	desc, ok := reg.Lookup("x:acme:translate")
	if !ok {
		t.Fatal("registered external capability not found in registry")
	}
	if desc.Owner != "vendor" {
		t.Fatalf("owner = %q, want vendor", desc.Owner)
	}
}

func TestRegisterExternalCapabilityRejectsNonVendorOwner(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "core",
		Effects: []EffectClass{EffectReadOnly},
	})
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("expected owner rejection, got err=%v", err)
	}
}

func TestRegisterExternalCapabilityRejectsEffectBeyondCeiling(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	// Default ceiling is {read_only, external_effect}; sensitive must be rejected.
	err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly, EffectSensitive},
	})
	if err == nil || !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("expected ceiling rejection, got err=%v", err)
	}
}

func TestRegisterExternalCapabilityRejectsWrongNamespace(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:other:translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly},
	})
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("expected namespace rejection, got err=%v", err)
	}
}

func TestRegisterExternalCapabilityRejectsMissingPrefix(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly},
	})
	if err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Fatalf("expected prefix rejection, got err=%v", err)
	}
}

func TestRegisterExternalCapabilityIdempotentAndConflicting(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	base := CapabilityDescriptor{ID: "x:acme:translate", Version: "v1", Owner: "vendor", Effects: []EffectClass{EffectReadOnly}}
	if err := reg.RegisterExternalCapability("acme", base); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Identical re-register is a no-op.
	if err := reg.RegisterExternalCapability("acme", base); err != nil {
		t.Fatalf("idempotent re-register: %v", err)
	}
	// Conflicting descriptor for the same ID is rejected.
	conflict := CapabilityDescriptor{ID: "x:acme:translate", Version: "v2", Owner: "vendor", Effects: []EffectClass{EffectReadOnly}}
	if err := reg.RegisterExternalCapability("acme", conflict); err == nil {
		t.Fatal("conflicting re-register must be rejected")
	}
}

func TestRegisterExternalCapabilityOnSealedRegistry(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	// Mimic production: the builtin ontology is present before sealing.
	if err := reg.Register(CapabilityDescriptor{ID: "test.builtin", Version: "v1", Effects: []EffectClass{EffectReadOnly}}); err != nil {
		t.Fatalf("seed builtin: %v", err)
	}
	if err := reg.Seal(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	// Builtin Register must now fail.
	if err := reg.Register(CapabilityDescriptor{ID: "shell.new", Version: "v1", Effects: []EffectClass{EffectReadOnly}}); err == nil {
		t.Fatal("builtin Register must fail on a sealed registry")
	}
	// External registration must still succeed: a freshly installed MCP/Skill
	// legitimately introduces a new x: capability the planner must name.
	if err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly},
	}); err != nil {
		t.Fatalf("external register on sealed registry: %v", err)
	}
}

func TestSetExternalCapabilityCeilingWidensRegistration(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	if err := reg.SetExternalCapabilityCeiling("acme", []EffectClass{EffectReadOnly, EffectExternalEffect, EffectSensitive}); err != nil {
		t.Fatalf("set ceiling: %v", err)
	}
	if err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly, EffectSensitive},
	}); err != nil {
		t.Fatalf("register with widened ceiling: %v", err)
	}
}

// --- Publish integration ---

func externalProviderForTest(adapter string, capID CapabilityID, effects []EffectClass) ProviderSpec {
	return ProviderSpec{
		AdapterName: adapter,
		Binding:     ProviderBinding{Kind: "mcp", ProviderID: "acme", ImplementationID: "translate", SchemaDigest: "sha256:abc"},
		Classification: ProviderClassProvision,
		ParameterAuthorization: ParameterAuthorization{
			Digest: "pa:1", CanonicalizerVer: "1", AllowedFields: []string{"text"},
		},
		Provides: []CapabilityProvision{{Capability: capID, Quality: 1}},
		Effects:  effects,
	}
}

func TestPublishAcceptsRegisteredExternalCapability(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	if err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly, EffectExternalEffect},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Seal(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	cat := &ToolCatalog{registry: reg}
	if _, err := cat.Publish([]ProviderSpec{
		externalProviderForTest("acme-translate", "x:acme:translate", []EffectClass{EffectExternalEffect}),
	}, time.Now().UTC()); err != nil {
		t.Fatalf("publish registered external capability: %v", err)
	}
}

func TestPublishRejectsUnregisteredExternalCapability(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	// Mimic production: the builtin ontology is present before sealing.
	if err := reg.Register(CapabilityDescriptor{ID: "test.builtin", Version: "v1", Effects: []EffectClass{EffectReadOnly}}); err != nil {
		t.Fatalf("seed builtin: %v", err)
	}
	if err := reg.Seal(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	cat := &ToolCatalog{registry: reg}
	_, err := cat.Publish([]ProviderSpec{
		externalProviderForTest("acme-translate", "x:acme:translate", []EffectClass{EffectExternalEffect}),
	}, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "unknown or deprecated") {
		t.Fatalf("expected unknown-capability rejection, got err=%v", err)
	}
}

func TestPublishRejectsExternalEffectBeyondDescriptor(t *testing.T) {
	reg := NewCapabilityRegistry("test-v1")
	// Registered descriptor permits only read_only; the namespace did not opt
	// into a wider ceiling, so a provider publishing external_effect must be
	// rejected rather than silently widening its authority.
	if err := reg.RegisterExternalCapability("acme", CapabilityDescriptor{
		ID:      "x:acme:translate",
		Version: "v1",
		Owner:   "vendor",
		Effects: []EffectClass{EffectReadOnly},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Seal(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	cat := &ToolCatalog{registry: reg}
	_, err := cat.Publish([]ProviderSpec{
		externalProviderForTest("acme-translate", "x:acme:translate", []EffectClass{EffectExternalEffect}),
	}, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "not permitted by its registered descriptor") {
		t.Fatalf("expected descriptor-bound rejection, got err=%v", err)
	}
}
