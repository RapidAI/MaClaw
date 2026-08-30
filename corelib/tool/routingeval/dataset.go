// Package routingeval is the regression harness for unified semantic tool
// routing (design doc docs/design/semantic-tool-routing-design-zh.md, section
// 10.1). It evaluates the planner/need layer only: each sample declares typed
// needs, facts, constraints and a catalog snapshot, runs ToolPlanner.Plan and
// compares the resulting plan against the expected selections, unmet reason
// codes and structural invariants.
//
// Executor-level concerns (invocation grants, host protocol, crash recovery,
// delivery receipts, distributed timing) are intentionally NOT re-tested here;
// they are covered by the semantic_*_test.go unit tests. Samples for those
// categories assert the planner-level invariant that underpins them (for
// example plan identity binding or DAG gating) and say so in their notes.
//
// Dataset format (one JSON file per design-10.1 category under data/):
//
//	{
//	  "category": "high_risk_refusal",        // stable slug, used in reports
//	  "category_id": 6,                       // design 10.1 item number (1-40)
//	  "title": "...",                         // human readable category name
//	  "design_ref": "10.1 item 6",            // pointer into the design doc
//	  "format_note": "...",                   // optional per-file comment header
//	  "samples": [ Sample, ... ]
//	}
//
// Sample fields:
//
//	id / title / notes     identity; notes document the design mapping and any
//	                       TODO gaps where planner behaviour diverges from the
//	                       design's idealised expectation.
//	mode                   "plan" (default): needs are declared verbatim.
//	                       "needs": needs are derived from labels through the
//	                       deterministic label stub (needs_stub.go) and first
//	                       compared against expected.needs (precision/recall),
//	                       then optionally fed into the planner.
//	input_text             diagnostic only; the harness never runs a UIC.
//	labels                 stub-mode intent labels: {label, qualifiers,
//	                       polarity}. polarity defaults to "require".
//	context                {root_task_id, session_id, turn_id, channel, now,
//	                       principal_id}; defaults: task-eval / sess-eval /
//	                       turn-1 / lansenger / 2026-08-15T00:00:00Z / user-eval.
//	needs                  [{id, capability, qualifiers, polarity, required,
//	                       max_invocations}]; polarity defaults to require,
//	                       required defaults to true for require polarity.
//	                       max_invocations > 1 expands the need into that many
//	                       sibling needs (id, id#02, ...); only the first
//	                       sibling inherits Required, the rest are a ceiling.
//	facts                  [{id, kind, authority, attributes, valid_seconds,
//	                       artifact}]; valid_seconds is relative to context.now
//	                       (negative = already expired). artifact carries a full
//	                       ArtifactBinding with an explicit scope.
//	constraints            [{id, capability, effect, authority, valid_seconds}].
//	catalog                {registry_version_override, coverage, providers}.
//	                       coverage is optional; when absent the catalog is
//	                       published with scope-wide Complete coverage.
//	providers              [{adapter, kind, provider_id, implementation_id,
//	                       schema_salt, classification, ready, ready_seconds,
//	                       channel_scopes, effects, capabilities, consumes,
//	                       produces}]. kind/provider_id default to builtin/core,
//	                       implementation_id defaults to adapter, ready defaults
//	                       to true. Every provider gets a closed empty parameter
//	                       schema authorisation; schema_salt varies the digest.
//	expected               assertions, all optional unless noted:
//	  publish_error_contains   catalog publish must fail with this substring.
//	  plan_error_contains      planner.Plan must fail with this substring.
//	  needs                    needs-mode gold set [{capability, qualifiers,
//	                           polarity}] compared as a set.
//	  selections               [{need_id, capability, adapter, phase,
//	                           requires_confirmation, provider_binding_contains}];
//	                           every entry must be planned and every planned
//	                           selection must be expected (mis-exposure check).
//	  no_selections            shorthand asserting an empty selection set.
//	  unmet                    [{need_id, reason}]; compared as an exact set.
//	  artifact_edges           [{consumer_need, producer_need | artifact_id}].
//	  ready_without_facts      exact need-id set of plan.ReadySelections(nil).
//	  trusted_confirmations    exact set of confirmation requirement IDs that
//	                           plan.TrustedSatisfiedDependencies satisfies.
//	  plan_id_equals/differs   cross-sample plan identity invariants.
//	  equivalent_to            cross-sample selection-equivalence invariant.
package routingeval

// DatasetFile is the top-level document of one data/*.json file.
type DatasetFile struct {
	Category   string   `json:"category"`
	CategoryID int      `json:"category_id"`
	Title      string   `json:"title"`
	DesignRef  string   `json:"design_ref"`
	FormatNote string   `json:"format_note,omitempty"`
	Samples    []Sample `json:"samples"`
}

// Sample is one evaluation case. See the package doc for the full format.
type Sample struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Notes       string           `json:"notes,omitempty"`
	Mode        string           `json:"mode,omitempty"`
	InputText   string           `json:"input_text,omitempty"`
	Labels      []LabelSpec      `json:"labels,omitempty"`
	Context     ContextSpec      `json:"context,omitempty"`
	Needs       []NeedSpec       `json:"needs,omitempty"`
	Facts       []FactSpec       `json:"facts,omitempty"`
	Constraints []ConstraintSpec `json:"constraints,omitempty"`
	Catalog     CatalogSpec      `json:"catalog"`
	Expected    ExpectedSpec     `json:"expected"`
}

// LabelSpec is one stub-mode intent label. The label stub is a deterministic
// placeholder for the host UIC: it maps a preset label plus optional context
// qualifiers onto a typed CapabilityNeed.
type LabelSpec struct {
	Label      string            `json:"label"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	Polarity   string            `json:"polarity,omitempty"`
}

// ContextSpec freezes the route request envelope. Empty fields take the
// defaults documented in the package doc so samples stay short.
type ContextSpec struct {
	RootTaskID      string `json:"root_task_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	TurnID          string `json:"turn_id,omitempty"`
	Channel         string `json:"channel,omitempty"`
	Now             string `json:"now,omitempty"`
	PrincipalID     string `json:"principal_id,omitempty"`
	MaxSelections   int    `json:"max_selections,omitempty"`
	MaxSchemaTokens int    `json:"max_schema_tokens,omitempty"`
}

// NeedSpec declares one typed capability need in plan mode.
type NeedSpec struct {
	ID         string            `json:"id"`
	Capability string            `json:"capability"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	Polarity   string            `json:"polarity,omitempty"`
	Required   *bool             `json:"required,omitempty"`
	// MaxInvocations expands this need into that many sibling needs, matching
	// how the host resolver spends a repeat budget as plan nodes. Silence
	// means one, so existing samples are unaffected. Sibling IDs are minted by
	// tool.RepeatSiblingNeedID, so expectations name them id, id#02, id#03.
	MaxInvocations int `json:"max_invocations,omitempty"`
}

// FactSpec declares one routing fact. ValidSeconds is interpreted relative to
// the sample's frozen context time.
type FactSpec struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Authority    string            `json:"authority"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	ValidSeconds *int64            `json:"valid_seconds,omitempty"`
	Artifact     *ArtifactSpec     `json:"artifact,omitempty"`
}

// ArtifactSpec is the JSON form of a complete ArtifactBinding.
type ArtifactSpec struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	MIMEType          string    `json:"mime_type"`
	IntegrityDigest   string    `json:"integrity_digest"`
	ProducerSelection string    `json:"producer_selection"`
	Scope             ScopeSpec `json:"scope"`
}

// ScopeSpec is the JSON form of an InvocationScope.
type ScopeSpec struct {
	RootTaskID  string `json:"root_task_id"`
	PlanID      string `json:"plan_id"`
	SessionID   string `json:"session_id"`
	TurnID      string `json:"turn_id"`
	PrincipalID string `json:"principal_id"`
}

// ConstraintSpec declares one routing constraint.
type ConstraintSpec struct {
	ID           string            `json:"id"`
	Capability   string            `json:"capability"`
	Effect       string            `json:"effect"`
	Authority    string            `json:"authority"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	ValidSeconds *int64            `json:"valid_seconds,omitempty"`
}

// CatalogSpec describes the catalog publication for one sample.
type CatalogSpec struct {
	RegistryVersionOverride string         `json:"registry_version_override,omitempty"`
	Coverage                *CoverageSpec  `json:"coverage,omitempty"`
	Providers               []ProviderJSON `json:"providers,omitempty"`
}

// CoverageSpec is the JSON form of CatalogCoverage. StaleSeconds is relative
// to context.now.
type CoverageSpec struct {
	State        string               `json:"state"`
	ReasonCode   string               `json:"reason_code,omitempty"`
	StaleSeconds *int64               `json:"stale_seconds,omitempty"`
	Families     []CoverageFamilySpec `json:"families,omitempty"`
}

// CoverageFamilySpec is the JSON form of one CatalogCoverageFamily.
type CoverageFamilySpec struct {
	Kind         string `json:"kind"`
	State        string `json:"state"`
	ReasonCode   string `json:"reason_code,omitempty"`
	StaleSeconds *int64 `json:"stale_seconds,omitempty"`
}

// ProviderJSON describes one catalog provider entry.
type ProviderJSON struct {
	Adapter          string             `json:"adapter"`
	Kind             string             `json:"kind,omitempty"`
	ProviderID       string             `json:"provider_id,omitempty"`
	ImplementationID string             `json:"implementation_id,omitempty"`
	SchemaSalt       string             `json:"schema_salt,omitempty"`
	Classification   string             `json:"classification,omitempty"`
	Ready            *bool              `json:"ready,omitempty"`
	ReadySeconds     *int64             `json:"ready_seconds,omitempty"`
	ChannelScopes    []string           `json:"channel_scopes,omitempty"`
	Effects          []string           `json:"effects,omitempty"`
	Capabilities     []ProvisionSpec    `json:"capabilities,omitempty"`
	Consumes         []ArtifactContract `json:"consumes,omitempty"`
	Produces         []ArtifactContract `json:"produces,omitempty"`
}

// ProvisionSpec is one capability provision of a provider.
type ProvisionSpec struct {
	Capability string            `json:"capability"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	Quality    *float64          `json:"quality,omitempty"`
}

// ArtifactContractSpec is the JSON form of ArtifactContract.
type ArtifactContract struct {
	Kind     string `json:"kind"`
	MIMEType string `json:"mime_type,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// ExpectedSpec holds all assertions evaluated against the planner output.
type ExpectedSpec struct {
	PublishErrorContains string                 `json:"publish_error_contains,omitempty"`
	PlanErrorContains    string                 `json:"plan_error_contains,omitempty"`
	Needs                []ExpectedNeedSpec     `json:"needs,omitempty"`
	Selections           []SelectionExpectation `json:"selections,omitempty"`
	NoSelections         bool                   `json:"no_selections,omitempty"`
	Unmet                []UnmetExpectation     `json:"unmet,omitempty"`
	ArtifactEdges        []ArtifactEdgeSpec     `json:"artifact_edges,omitempty"`
	ReadyWithoutFacts    []string               `json:"ready_without_facts,omitempty"`
	TrustedConfirmations []string               `json:"trusted_confirmations,omitempty"`
	PlanIDEquals         string                 `json:"plan_id_equals,omitempty"`
	PlanIDDiffers        string                 `json:"plan_id_differs,omitempty"`
	EquivalentTo         string                 `json:"equivalent_to,omitempty"`
}

// ExpectedNeedSpec is one gold need for the needs-mode comparison.
type ExpectedNeedSpec struct {
	Capability string            `json:"capability"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	Polarity   string            `json:"polarity,omitempty"`
}

// SelectionExpectation asserts that one need was planned, with optional
// per-field checks. Empty fields are not asserted.
type SelectionExpectation struct {
	NeedID                 string `json:"need_id"`
	Capability             string `json:"capability,omitempty"`
	Adapter                string `json:"adapter,omitempty"`
	Phase                  string `json:"phase,omitempty"`
	RequiresConfirmation   *bool  `json:"requires_confirmation,omitempty"`
	ProviderBindingContain string `json:"provider_binding_contains,omitempty"`
}

// UnmetExpectation asserts the exact reason code of one unmet need.
type UnmetExpectation struct {
	NeedID string `json:"need_id"`
	Reason string `json:"reason"`
}

// ArtifactEdgeSpec asserts one artifact dependency edge of a planned
// selection: exactly one of producer_need (in-plan producer) or artifact_id
// (trusted pre-existing artifact fact) is set.
type ArtifactEdgeSpec struct {
	ConsumerNeed string `json:"consumer_need"`
	ProducerNeed string `json:"producer_need,omitempty"`
	ArtifactID   string `json:"artifact_id,omitempty"`
}
