package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// tuiSemanticTestDefinitions builds the definitions the slice-1 catalog
// projects, straight from a real CoreToolRegistry so the adapter-name and
// schema assertions exercise the same surface the runtime executes through.
func tuiSemanticTestDefinitions(t *testing.T) []map[string]interface{} {
	t.Helper()
	registry := agent.NewCoreToolRegistry()
	agent.RegisterCoreTools(registry, agent.CoreToolDeps{})
	defs := registry.BuildDefinitions()
	if len(defs) == 0 {
		t.Fatal("RegisterCoreTools produced no definitions")
	}
	return defs
}

func TestTUISemanticCapabilityRegistrySealed(t *testing.T) {
	registry, err := tuiSemanticCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Sealed() {
		t.Fatal("TUI semantic capability registry must be sealed")
	}
	for _, capability := range []coretool.CapabilityID{
		coretool.CapabilityFSReadLocal,
		coretool.CapabilityInformationFetchWeb,
	} {
		if _, ok := registry.Lookup(capability); !ok {
			t.Fatalf("builtin ontology must contain %s", capability)
		}
	}
}

func TestTUIStaticCatalogSnapshotPublishes(t *testing.T) {
	snapshot, registry, err := tuiStaticCatalogSnapshot(tuiSemanticTestDefinitions(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Providers) != len(tuiReadOnlyProviderPlan) {
		t.Fatalf("snapshot has %d providers, want %d", len(snapshot.Providers), len(tuiReadOnlyProviderPlan))
	}
	for _, provider := range snapshot.Providers {
		if provider.Binding.ProviderID != tuiSemanticChannelScope {
			t.Fatalf("provider %q bound to %q, want %q", provider.AdapterName, provider.Binding.ProviderID, tuiSemanticChannelScope)
		}
		for _, provision := range provider.Provides {
			if _, ok := registry.Lookup(provision.Capability); !ok {
				t.Fatalf("provider %q serves unregistered capability %s", provider.AdapterName, provision.Capability)
			}
			if provision.Capability != coretool.CapabilityFSReadLocal && provision.Capability != coretool.CapabilityInformationFetchWeb {
				t.Fatalf("provider %q claims capability %s outside the slice-1 read-only set", provider.AdapterName, provision.Capability)
			}
		}
		for _, effect := range provider.Effects {
			if effect != coretool.EffectReadOnly {
				t.Fatalf("provider %q declares effect %q, slice 1 is read-only", provider.AdapterName, effect)
			}
		}
	}
}

func TestTUIStaticCatalogFailsClosedOnMissingDefinition(t *testing.T) {
	// Drop web_fetch from the registry: the catalog must refuse to publish a
	// provider the host cannot execute, never skip it silently.
	registry := agent.NewCoreToolRegistry()
	agent.RegisterCoreTools(registry, agent.CoreToolDeps{})
	var partial []map[string]interface{}
	for _, def := range registry.BuildDefinitions() {
		function, _ := def["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		if name == "web_fetch" {
			continue
		}
		partial = append(partial, def)
	}
	if _, _, err := tuiStaticCatalogSnapshot(partial); err == nil {
		t.Fatal("catalog must fail closed when a planned adapter definition is missing")
	}
}

func TestTUIStaticProviderAdaptersExistInRegistry(t *testing.T) {
	defs := tuiSemanticTestDefinitions(t)
	providers, _, _, err := tuiSemanticReadOnlyProviders(defs)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]bool, len(defs))
	for _, def := range defs {
		function, _ := def["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		byName[name] = true
	}
	for _, provider := range providers {
		if !byName[provider.AdapterName] {
			t.Fatalf("adapter %q has no CoreToolRegistry definition; a granted selection would be unexecutable", provider.AdapterName)
		}
	}
}

func TestTUIStaticPostureConstraintsDenyMutation(t *testing.T) {
	denied := make(map[coretool.CapabilityID]bool)
	for _, constraint := range tuiStaticPostureConstraints() {
		if constraint.Effect != "deny" {
			t.Fatalf("constraint %q has effect %q, want deny", constraint.ID, constraint.Effect)
		}
		denied[constraint.Capability] = true
	}
	for _, capability := range []coretool.CapabilityID{
		coretool.CapabilityFSWriteLocal,
		coretool.CapabilityFSDeleteLocal,
		coretool.CapabilityShellExecuteLocal,
		coretool.CapabilityBuildVerifyLocal,
		coretool.CapabilityShellExecuteRemoteHost,
		coretool.CapabilityBrowserControlWeb,
		coretool.CapabilityComputerControlDesktop,
	} {
		if !denied[capability] {
			t.Fatalf("posture must deny %s: the slice-1 inventory has no mutation provider, the denial must stay explicit", capability)
		}
	}
	if denied[coretool.CapabilityFSReadLocal] || denied[coretool.CapabilityInformationFetchWeb] {
		t.Fatal("posture must not deny the capabilities the slice-1 inventory serves")
	}
}

func TestTUIStaticNeedsAreReviewedDeclarations(t *testing.T) {
	for _, need := range tuiStaticReadOnlyCapabilityNeeds() {
		if len(need.EvidenceIDs) != 1 || need.EvidenceIDs[0] != tuiStaticCatalogNeedEvidence {
			t.Fatalf("need %q must cite only the static-catalog evidence, never text-derived evidence", need.ID)
		}
		if need.Polarity != coretool.NeedRequire || !need.Required {
			t.Fatalf("need %q must be a required require-polarity need in slice 1", need.ID)
		}
	}
}
