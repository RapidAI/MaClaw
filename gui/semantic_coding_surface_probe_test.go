package main

import (
	"sort"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingSurfaceTrustedAdapters records, for each capability the static coding
// surface depends on, the trusted managed adapter that already implements it.
//
// Referencing the adapter constants is the point: it makes the claim "a managed
// implementation already exists" a compile-time fact rather than a comment.
//
// repo.mutate.vcs is deliberately absent. No coding tool declares it: the
// surface reaches git mutation through bash, under shell.execute.local. A
// managed coding plan that grants only the shell capability would therefore
// carry repo mutation with it, which the migration had to decide about rather
// than inherit.
//
// That decision has since been made, and it went the other way: the managed
// coding rule grants build.verify.local instead of shell.execute.local, so it
// carries neither arbitrary execution nor the repo mutation riding on it. This
// map still records shell.execute.local because it describes what the static
// belt reaches for, not what the managed rule grants.
var codingSurfaceTrustedAdapters = map[tool.CapabilityID]string{
	tool.CapabilityFSReadLocal:       semanticTrustedFileReadAdapter,
	tool.CapabilityFSWriteLocal:      semanticTrustedFileWriteAdapter,
	tool.CapabilityShellExecuteLocal: semanticTrustedShellAdapter,
	tool.CapabilityRepoInspectVCS:    semanticTrustedRepoInspectAdapter,
}

// codingSurfaceToolsOutsideHostRegistry lists coding tools the host registry
// never registers. buildCodingToolDefinitionsFromRegistry falls back to a
// private definition for these, so these legacy names exist only inside the
// subagent and cannot be annotated.
//
// Their outcomes are no longer out of reach, which is the part that decides
// whether the migration can proceed: fs.read.local now serves both content
// search and locating files by name through the trusted read adapter, so a
// managed coding plan can discover and grep the workspace without these two
// legacy entries ever being registered.
var codingSurfaceToolsOutsideHostRegistry = map[string]string{
	"Glob":    "subagent-only fallback definition; outcome served by fs.read.local file_pattern",
	"ripgrep": "subagent-only fallback definition; outcome served by fs.read.local query",
}

// TestCodingSurfaceCapabilityCoverageProbe is the read-only probe for the
// agentic half of the migration. It changes no routing, and it still describes
// the coding subagent's static belt, which is reached through its own entry
// points and never consults imSemanticIntentRuleSet. The shared-turn coding
// route is a separate surface and is pinned in semantic_coding_route_test.go.
//
// The question it answers is the go/no-go one. The coding subagent picks tools
// with host heuristics over the task text rather than from a capability plan,
// and before that can be replaced it has to be true that the catalog can
// express what the surface actually uses. This walks the static surface, reads
// each tool's registered capability provision, and checks that a trusted
// managed adapter already serves it.
//
// A failure here is a finding rather than a bug to paper over: it names a
// coding tool whose outcome the managed catalog cannot express, which is work
// the migration must do first.
func TestCodingSurfaceCapabilityCoverageProbe(t *testing.T) {
	registry := NewToolRegistry()
	h := &IMMessageHandler{registry: registry}
	registerBuiltinTools(registry, h)
	registerNonCodeTools(registry, &App{testHomeDir: t.TempDir()})

	var (
		unexpectedlyAbsent  []string
		unexpectedlyPresent []string
		unannotated         []string
		uncovered           []string
		covered             = make(map[tool.CapabilityID][]string)
	)
	for _, name := range codingSubAgentToolOrder {
		registered, ok := registry.Get(name)
		_, knownAbsent := codingSurfaceToolsOutsideHostRegistry[name]
		if !ok || registered == nil {
			if !knownAbsent {
				unexpectedlyAbsent = append(unexpectedlyAbsent, name)
			}
			continue
		}
		if knownAbsent {
			unexpectedlyPresent = append(unexpectedlyPresent, name)
		}
		if len(registered.CapabilityProvisions) == 0 {
			unannotated = append(unannotated, name)
			continue
		}
		for _, provision := range registered.CapabilityProvisions {
			if _, ok := codingSurfaceTrustedAdapters[provision.Capability]; !ok {
				uncovered = append(uncovered, name+" -> "+string(provision.Capability))
				continue
			}
			covered[provision.Capability] = append(covered[provision.Capability], name)
		}
	}

	sort.Strings(unexpectedlyAbsent)
	sort.Strings(unexpectedlyPresent)
	sort.Strings(unannotated)
	sort.Strings(uncovered)

	if len(unexpectedlyAbsent) > 0 {
		t.Errorf("coding tools absent from the host registry, so the catalog cannot see them: %v", unexpectedlyAbsent)
	}
	if len(unexpectedlyPresent) > 0 {
		t.Errorf("coding tools now registered on the host; drop their entry from codingSurfaceToolsOutsideHostRegistry: %v", unexpectedlyPresent)
	}
	if len(unannotated) > 0 {
		t.Errorf("registered coding tools with no capability provision, so no plan can ever select them: %v", unannotated)
	}
	if len(uncovered) > 0 {
		t.Errorf("coding tools whose capability has no trusted managed adapter: %v", uncovered)
	}
	if len(covered) == 0 {
		t.Fatal("probe resolved no capabilities at all; the registry or the surface list stopped being readable")
	}

	// Guard the coverage claim against quietly shrinking. Every adapter above is
	// claimed to serve the coding surface, so an entry no coding tool maps to is
	// a stale claim that overstates how ready the migration is.
	for capability := range codingSurfaceTrustedAdapters {
		if len(covered[capability]) == 0 {
			t.Errorf("capability %q is listed as coding-surface coverage but no coding tool provides it", capability)
		}
	}
}

// TestCodingSurfaceAnnotationsStayOutOfTheManagedCatalog is the safety half of
// the probe. Annotating a legacy tool with its capability is a catalog
// declaration, not an exposure decision, and the write/read/shell families are
// unpublished on managed turns in favour of their trusted adapters. Without
// this, annotating one more coding tool could quietly hand the managed surface
// a legacy multiplexer whose schema takes a model-written path.
func TestCodingSurfaceAnnotationsStayOutOfTheManagedCatalog(t *testing.T) {
	registry := NewToolRegistry()
	h := &IMMessageHandler{registry: registry}
	registerBuiltinTools(registry, h)
	registerNonCodeTools(registry, &App{testHomeDir: t.TempDir()})

	for _, name := range codingSubAgentToolOrder {
		registered, ok := registry.Get(name)
		if !ok || registered == nil || len(registered.CapabilityProvisions) == 0 {
			continue
		}
		for _, channel := range []string{"desktop", "lansenger"} {
			if !semanticUnpublishedManagedProvider(*registered, channel, "") {
				t.Errorf("legacy coding tool %q is published into the managed catalog on channel %q", name, channel)
			}
		}
	}
}
