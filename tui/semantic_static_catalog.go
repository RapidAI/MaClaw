package main

// TUI static read-only semantic catalog — Phase 3 (full semantic routing)
// slice 1. It mirrors the reviewed guiapp/coding_static_catalog.go S1-A
// pattern: a hand-reviewed need set and provider set, deliberately NOT
// inferred from user text, plus explicit posture constraints so a later
// provider append can never silently widen the turn.
//
// Slice-1 discipline:
//   - Capabilities are limited to the shared builtin ontology
//     (tool.RegisterBuiltinCapabilityOntology): fs.read.local and
//     information.fetch.web. TUI's registry has no VCS tool today, so
//     repo.inspect.vcs is NOT claimed here; claiming it would turn every
//     planned read turn into an Unmet (fail-closed) instead of a surface.
//   - Providers project the real CoreToolRegistry definitions by canonical
//     tool name, so the model-visible schema and the executed handler can
//     never drift: execution stays on the legacy chain
//     (tuiCallbacks.ExecuteTool -> toolRegistry.ExecuteCtx), exactly like the
//     tool_dispatcher_pilot precedence work.
//   - One provider per capability in this slice. Adding a second provider for
//     an already-served capability (e.g. list_directory under fs.read.local)
//     is a slice-2 planner-interaction decision, not a data append.
//   - Nothing consumes this catalog at runtime yet (slice 2 wires UIC +
//     planner + MaterializeReadySurface into tuiCallbacks.BuildTools behind a
//     kill switch). The tests below are the current consumer; they pin the
//     provider/registry contract so slice 2 cannot drift it silently.

import (
	"encoding/json"
	"fmt"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	tuiSemanticCatalogVersion = "tui-semantic-v1"
	tuiSemanticChannelScope   = "tui"

	// tuiStaticCatalogNeedEvidence marks every static need as a reviewed host
	// declaration, not a per-turn inference. Slice 2 replaces the static need
	// set with UIC-derived needs; these IDs must never be attached to text
	// evidence.
	tuiStaticCatalogNeedEvidence = "evidence:tui-static-catalog:v1"
)

// tuiSemanticCapabilityRegistry returns the sealed capability registry for the
// TUI host. Slice 1 registers the shared builtin ontology only; TUI has no
// product-domain capabilities of its own yet.
func tuiSemanticCapabilityRegistry() (*coretool.CapabilityRegistry, error) {
	registry := coretool.NewCapabilityRegistry(tuiSemanticCatalogVersion)
	if err := coretool.RegisterBuiltinCapabilityOntology(registry); err != nil {
		return nil, fmt.Errorf("register builtin capability ontology: %w", err)
	}
	if err := registry.Seal(); err != nil {
		return nil, fmt.Errorf("seal TUI semantic capability registry: %w", err)
	}
	return registry, nil
}

// tuiStaticReadOnlyCapabilityNeeds is a reviewed, host policy. It is not a
// translation of the user text and must never infer needs from it. Slice 1
// deliberately covers only read-only filesystem and web fetch work; shell,
// write, build, remote and control-plane capabilities stay out until their
// separate contracts exist.
func tuiStaticReadOnlyCapabilityNeeds() []coretool.CapabilityNeed {
	return []coretool.CapabilityNeed{
		{
			ID:          "need:tui-static:fs.read.local:0001",
			Capability:  coretool.CapabilityFSReadLocal,
			Polarity:    coretool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{tuiStaticCatalogNeedEvidence},
		},
		{
			ID:          "need:tui-static:information.fetch.web:0001",
			Capability:  coretool.CapabilityInformationFetchWeb,
			Polarity:    coretool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{tuiStaticCatalogNeedEvidence},
		},
	}
}

// tuiStaticPostureConstraints records the host posture as planner input. The
// slice-1 inventory exposes no mutation provider at all; retaining these
// denials makes that decision explicit and prevents a later provider append
// from silently making a read turn writable, shell-capable or remote-reaching.
func tuiStaticPostureConstraints() []coretool.RoutingConstraint {
	denied := []coretool.CapabilityID{
		coretool.CapabilityFSWriteLocal,
		coretool.CapabilityShellExecuteLocal,
		coretool.CapabilityBuildVerifyLocal,
		coretool.CapabilityShellExecuteRemoteHost,
		coretool.CapabilityBrowserControlWeb,
		coretool.CapabilityComputerControlDesktop,
	}
	constraints := make([]coretool.RoutingConstraint, 0, len(denied))
	for _, capability := range denied {
		constraints = append(constraints, coretool.RoutingConstraint{
			ID:         "tui-static:deny-" + string(capability),
			Capability: capability,
			Effect:     "deny",
			Authority:  coretool.AuthorityPolicy,
		})
	}
	return constraints
}

// tuiReadOnlyProviderPlan lists the slice-1 capability projections. The
// adapter name is the canonical CoreToolRegistry tool name so a granted
// selection dispatches through the unchanged legacy execution chain.
var tuiReadOnlyProviderPlan = []struct {
	adapter    string
	capability coretool.CapabilityID
	quality    float64
}{
	{adapter: "read_file", capability: coretool.CapabilityFSReadLocal, quality: 2},
	{adapter: "web_fetch", capability: coretool.CapabilityInformationFetchWeb, quality: 2},
}

// tuiSemanticReadOnlyProviders projects the host registry definitions onto the
// slice-1 capabilities. A missing definition is an error, not a skipped entry:
// the catalog must never publish a provider the host cannot execute. The
// returned defsByName/schemas maps are deliberately unused in slice 1: they
// are the slice-2 wiring surface, so granted selections dispatch with the
// exact same definition bytes this catalog digested.
func tuiSemanticReadOnlyProviders(defs []map[string]interface{}) ([]coretool.ProviderSpec, map[string]map[string]interface{}, map[string]map[string]interface{}, error) {
	byName := make(map[string]map[string]interface{}, len(defs))
	for _, def := range defs {
		function, ok := def["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := function["name"].(string)
		if name != "" {
			byName[name] = def
		}
	}

	providers := make([]coretool.ProviderSpec, 0, len(tuiReadOnlyProviderPlan))
	defsByName := make(map[string]map[string]interface{}, len(tuiReadOnlyProviderPlan))
	schemas := make(map[string]map[string]interface{}, len(tuiReadOnlyProviderPlan))
	for _, plan := range tuiReadOnlyProviderPlan {
		definition, ok := byName[plan.adapter]
		if !ok {
			return nil, nil, nil, fmt.Errorf("tui semantic catalog: registry has no definition for adapter %q", plan.adapter)
		}
		function, _ := definition["function"].(map[string]interface{})
		schema, _ := function["parameters"].(map[string]interface{})
		if schema == nil {
			return nil, nil, nil, fmt.Errorf("tui semantic catalog: adapter %q has no parameters schema", plan.adapter)
		}
		schema, err := tuiNormalizedSchema(schema)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("tui semantic catalog: normalize %q schema: %w", plan.adapter, err)
		}
		authorization, err := coretool.NewParameterAuthorization(schema)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("tui semantic catalog: authorize %q schema: %w", plan.adapter, err)
		}
		implementation := plan.adapter + "-v1"
		providers = append(providers, coretool.ProviderSpec{
			AdapterName: plan.adapter,
			Binding: coretool.ProviderBinding{
				Kind:             "builtin",
				ProviderID:       tuiSemanticChannelScope,
				ImplementationID: implementation,
				SchemaDigest:     coretool.SchemaDigest(canonicalTUIDefinitionBytes(schema)),
			},
			ParameterAuthorization: authorization,
			Provides:               []coretool.CapabilityProvision{{Capability: plan.capability, Quality: plan.quality}},
			Effects:                []coretool.EffectClass{coretool.EffectReadOnly},
			Ready:                  true,
			ChannelScopes:          []string{tuiSemanticChannelScope},
		})
		defsByName[plan.adapter] = definition
		schemas[plan.adapter] = schema
	}
	return providers, defsByName, schemas, nil
}

// tuiStaticCatalogSnapshot publishes the slice-1 snapshot. Completeness is
// legitimate here: the inventory is a static registry projection whose full
// provider list is known at build time, unlike a dynamic lifecycle owner.
func tuiStaticCatalogSnapshot(defs []map[string]interface{}) (coretool.ToolCatalogSnapshot, *coretool.CapabilityRegistry, error) {
	registry, err := tuiSemanticCapabilityRegistry()
	if err != nil {
		return coretool.ToolCatalogSnapshot{}, nil, err
	}
	providers, _, _, err := tuiSemanticReadOnlyProviders(defs)
	if err != nil {
		return coretool.ToolCatalogSnapshot{}, nil, err
	}
	catalog := coretool.NewToolCatalog(registry)
	now := time.Now().UTC()
	snapshot, err := catalog.PublishWithCoverage(providers, coretool.CatalogCoverage{
		State:      coretool.CatalogCoverageComplete,
		ObservedAt: now,
	}, now)
	if err != nil {
		return coretool.ToolCatalogSnapshot{}, nil, err
	}
	return snapshot, registry, nil
}

// tuiNormalizedSchema round-trips the schema through JSON. Registry entries
// type their property specs as map[string]string in-process; the parameter
// authorizer requires map[string]interface{}. A JSON round-trip is exact here
// — the schema is JSON by definition — and normalizes every nested map type in
// one step.
func tuiNormalizedSchema(schema map[string]interface{}) (map[string]interface{}, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var normalized map[string]interface{}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}
	if normalized == nil {
		return nil, fmt.Errorf("schema normalized to nil")
	}
	return normalized, nil
}

// canonicalTUIDefinitionBytes mirrors the guiapp canonical digest: encoding/json
// sorts map keys, so the digest stays stable across map iteration orders and
// catalog refreshes with the same trusted definition.
func canonicalTUIDefinitionBytes(definition map[string]interface{}) []byte {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return []byte("invalid_definition")
	}
	return encoded
}
