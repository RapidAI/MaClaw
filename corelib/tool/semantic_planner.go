package tool

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// NeedPolarity prevents lexical matches from being treated as requests. A
// mention such as “don't take a screenshot” is Avoid, not Require.
type NeedPolarity string

const (
	NeedRequire  NeedPolarity = "require"
	NeedAvoid    NeedPolarity = "avoid"
	NeedInquire  NeedPolarity = "inquire"
	NeedSimulate NeedPolarity = "simulate"
)

type CapabilityNeed struct {
	ID          string
	Capability  CapabilityID
	Qualifiers  map[string]string
	Polarity    NeedPolarity
	Required    bool
	Confidence  float64
	EvidenceIDs []string
}

type FactAuthority string

const (
	AuthorityUser    FactAuthority = "user_assertion"
	AuthorityChannel FactAuthority = "channel_adapter"
	AuthorityPolicy  FactAuthority = "policy"
	AuthorityRuntime FactAuthority = "runtime"
)

type RoutingFact struct {
	ID         string
	Kind       string
	Attributes map[string]string
	// Artifact binds an artifact_available fact to its complete, host-issued
	// provenance. Attributes remain useful diagnostic metadata, but are never
	// sufficient to authorize content consumption.
	Artifact   *ArtifactBinding
	Authority  FactAuthority
	ValidUntil time.Time
}

// RoutingConstraint only narrows what planning may do. It cannot name a tool
// to force it into the model surface.
type RoutingConstraint struct {
	ID         string
	Capability CapabilityID
	Effect     string // deny, require_confirmation, require_channel
	Attributes map[string]string
	Authority  FactAuthority
	ValidUntil time.Time
}

// PlanningBudget bounds how many required selections the planner may keep.
// Zero MaxSelections means unlimited. Required selections stay wave-closed:
// the limiter keeps a closed prefix or reports budget_exceeded /
// planning_budget_exceeded. Optional leftovers are filled one selection at a
// time and omitted, not unmet.
type PlanningBudget struct {
	MaxSelections   int
	MaxSchemaTokens int
}

type RouteRequest struct {
	RootTaskID   string
	SessionID    string
	TurnID       string
	ChannelScope string
	Snapshot     ToolCatalogSnapshot
	Needs        []CapabilityNeed
	Facts        []RoutingFact
	Constraints  []RoutingConstraint
	Budget       PlanningBudget
	Now          time.Time
}

type FitProof struct {
	NeedID             string
	ProviderBindingID  string
	MatchedCapability  CapabilityID
	QualifierBindings  map[string]string
	SnapshotGeneration uint64
	Digest             string
}

type PlannedSelection struct {
	ID                     string
	NeedID                 string
	AdapterName            string
	Provider               ProviderBinding
	ParameterAuthorization ParameterAuthorization
	Effects                []EffectClass
	FitProof               FitProof
	// RequiresConfirm is retained only as a compatibility projection for older
	// hosts. New execution code must use the explicit confirmation requirement
	// in Requires, which can be satisfied only by a trusted confirmation fact.
	RequiresConfirm bool
	ConfirmationID  string
	Phase           PlanPhase
	Requires        []string
	Consumes        []ArtifactContract
	Produces        []ArtifactContract
	// ArtifactDependencies binds every required consumed artifact to the
	// producing selection chosen by this immutable plan. Requires controls DAG
	// readiness; this separate edge prevents an executor from treating "any
	// newest matching artifact" in the invocation scope as the dependency.
	ArtifactDependencies []ArtifactDependency
}

// IsLightPromptSafeSelection reports whether an already-planned selection may
// execute under a lightweight prompt profile. This is deliberately based on
// the immutable plan contract, never on an adapter name or opaque grant token.
// Any confirmation dependency or non-read-only declared effect requires the
// full policy path. Empty/unknown effect sets are rejected conservatively.
func IsLightPromptSafeSelection(selection PlannedSelection) bool {
	if selection.RequiresConfirm || strings.TrimSpace(selection.ConfirmationID) != "" {
		return false
	}
	for _, requirement := range selection.Requires {
		if strings.HasPrefix(strings.TrimSpace(requirement), "confirmation:") {
			return false
		}
	}
	return effectsAreReadOnly(selection.Effects)
}

// ArtifactDependency is a typed, immutable producer-to-consumer plan edge.
// ProducerSelection is empty only when the planner has accepted an already
// available trusted artifact fact. Such a fact still requires an exact
// ArtifactRef at execution time; it never authorizes a newest-wins lookup.
type ArtifactDependency struct {
	ProducerSelection string
	// ArtifactID is a trusted RouteState-projected reference for a reused
	// artifact. It is never supplied by the model. Exactly one of
	// ProducerSelection or ArtifactID is set.
	ArtifactID string
	// Artifact is required when ArtifactID names a pre-existing artifact. It
	// commits the plan to the source scope, producer and integrity digest, so a
	// consumer cannot turn an ID into a same-scope lookup primitive.
	Artifact ArtifactBinding
	Contract ArtifactContract
}

// ArtifactBinding is the immutable, payload-free provenance needed to consume
// an already-published artifact. It intentionally omits CreatedAt: publication
// time is diagnostic metadata and must not make a retried plan drift.
type ArtifactBinding struct {
	ID                string
	Kind              string
	MIMEType          string
	IntegrityDigest   string
	ProducerSelection string
	Scope             InvocationScope
}

func ArtifactBindingFromRef(ref ArtifactRef) ArtifactBinding {
	return ArtifactBinding{ID: ref.ID, Kind: ref.Kind, MIMEType: ref.MIMEType, IntegrityDigest: ref.IntegrityDigest, ProducerSelection: ref.ProducerSelection, Scope: ref.Scope}
}

func (binding ArtifactBinding) ArtifactRef() ArtifactRef {
	return ArtifactRef{ID: binding.ID, Kind: binding.Kind, MIMEType: binding.MIMEType, IntegrityDigest: binding.IntegrityDigest, ProducerSelection: binding.ProducerSelection, Scope: binding.Scope}
}

// PlanPhase separates the complete capability plan from the smaller current
// model surface. Delivery is never materialized before its ArtifactRef and
// confirmation dependencies are satisfied.
type PlanPhase string

const (
	PlanPhaseExecution PlanPhase = "execution"
	PlanPhaseDelivery  PlanPhase = "delivery"
)

type UnmetNeed struct {
	NeedID     string
	ReasonCode string
}

// unmetIsAuthoringFault separates a rule that names something wrong from an
// environment that cannot serve something named correctly.
//
// The distinction only matters for optional needs. An optional need exists to
// say "take this capability if the host has it", so an absent provider or a
// policy denial is the answer working as intended and must not fail the turn.
// A capability the registry does not declare, or a need the descriptor
// rejects, is neither of those: it is a rule that could never be served on any
// host, and letting Required:false swallow it would turn optionality into a
// place for authoring mistakes to hide.
func unmetIsAuthoringFault(reasonCode string) bool {
	switch reasonCode {
	case "unknown_capability", "invalid_capability_need":
		return true
	default:
		return false
	}
}

const (
	TraceStageSemantics       = "TP0"
	TraceStageFeasibility     = "TP1"
	TraceStageDependency      = "TP2"
	TraceStageOptimization    = "TP3"
	TraceStageMaterialization = "TP4"
	TraceStageBinding         = "TP5"
	TraceStageRecovery        = "TP6"
	TraceStageCatalog         = "TP7"
	TraceStageArtifact        = "TP8"
	TraceStageRendering       = "TP9"
)

// ToolDecision is one planner choice with a stable reason code. It is
// diagnostic metadata and is not part of SnapshotDigest / plan identity.
type ToolDecision struct {
	Stage      string
	Subject    string
	Event      string
	ReasonCode string
	NeedID     string
}

// TraceEvent is the ExplainTrace row for one TP0–TP4 observation.
type TraceEvent struct {
	Stage      string
	Subject    string
	Event      string
	ReasonCode string
}

// ExplainTrace records desensitised planner decisions for the current plan.
// It must not contain user text, paths, parameters, or provider secrets.
type ExplainTrace struct {
	PlanID         string
	SnapshotDigest string
	Events         []TraceEvent
}

// ToolPlan is a deterministic decision artifact. Rendering/execution only use
// its selections; they do not re-run name matching against a mutable catalog.
type ToolPlan struct {
	RootTaskID string
	ID         string
	// SnapshotDigest freezes every planner input that can influence the
	// decision. It is the compare-and-publish guard for RouteState revisions;
	// CatalogGeneration alone is intentionally insufficient.
	SnapshotDigest    string
	CatalogGeneration uint64
	Selections        []PlannedSelection
	Unmet             []UnmetNeed
	// Omitted records optional needs the host could not serve. They are kept
	// out of Unmet because they must not fail the plan, and kept out of
	// silence because "the model was never offered this capability" is
	// something review and audit have to be able to see after the fact.
	Omitted   []UnmetNeed
	Decisions []ToolDecision
	Trace     ExplainTrace
}

// ReadySelections returns only the selections whose declared dependencies are
// already satisfied by trusted execution facts. It is intentionally independent
// from function-call order, allowing a host executor to enforce the plan DAG.
func (p ToolPlan) ReadySelections(satisfied map[string]bool) []PlannedSelection {
	ready := make([]PlannedSelection, 0, len(p.Selections))
	for _, selection := range p.Selections {
		allSatisfied := true
		for _, requirement := range selection.Requires {
			if !satisfied[requirement] {
				allSatisfied = false
				break
			}
		}
		if allSatisfied {
			ready = append(ready, clonePlannedSelection(selection))
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
	return ready
}

// ConfirmationRequirementID returns the non-model-controlled DAG dependency
// for a selection's approval. It is scoped by ToolPlan.RootTaskID when a
// trusted confirmation fact is evaluated; the plain identifier is never an
// authorization token and may not be supplied as a tool argument.
func ConfirmationRequirementID(needID string) string {
	return "confirmation:" + strings.TrimSpace(needID)
}

// TrustedSatisfiedDependencies projects only trusted runtime/policy/channel
// facts into plan dependencies. User/LLM text and provider output cannot
// satisfy a confirmation requirement. The host must bind every confirmation
// fact to this root task and to the exact requirement ID before this method is
// called; facts without that binding are deliberately ignored.
func (p ToolPlan) TrustedSatisfiedDependencies(facts []RoutingFact, now time.Time) map[string]bool {
	satisfied := make(map[string]bool)
	if strings.TrimSpace(p.RootTaskID) == "" {
		return satisfied
	}
	// A trusted confirmation for a different logical requirement must not be
	// carried into this plan's dependency map. Besides avoiding misleading
	// traces, this prevents an unrelated confirmation fact from becoming a
	// future ready-node prerequisite after a replan or plan merge.
	requiredConfirmations := make(map[string]bool)
	for _, selection := range p.Selections {
		for _, requirement := range selection.Requires {
			if strings.HasPrefix(requirement, "confirmation:") {
				requiredConfirmations[requirement] = true
			}
		}
	}
	for _, fact := range facts {
		if fact.Authority != AuthorityRuntime && fact.Authority != AuthorityPolicy && fact.Authority != AuthorityChannel {
			continue
		}
		if !fact.ValidUntil.IsZero() && now.After(fact.ValidUntil) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(fact.Kind), "confirmation_granted") {
			continue
		}
		if strings.TrimSpace(fact.Attributes["root_task_id"]) != p.RootTaskID {
			continue
		}
		requirement := strings.TrimSpace(fact.Attributes["confirmation_requirement"])
		if !requiredConfirmations[requirement] {
			continue
		}
		satisfied[requirement] = true
	}
	return satisfied
}

// ToolPlanner selects providers from governed capability contracts. Semantic
// extraction is deliberately outside this type: callers pass typed needs.
type ToolPlanner struct {
	registry *CapabilityRegistry
}

func NewToolPlanner(registry *CapabilityRegistry) *ToolPlanner {
	return &ToolPlanner{registry: registry}
}

func (p *ToolPlanner) Plan(req RouteRequest) (ToolPlan, error) {
	if p == nil || p.registry == nil {
		return ToolPlan{}, fmt.Errorf("tool planner requires a capability registry")
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if strings.TrimSpace(req.RootTaskID) == "" {
		return ToolPlan{}, fmt.Errorf("root task id is required")
	}
	if req.Snapshot.RegistryVersion != p.registry.Version() {
		return ToolPlan{}, fmt.Errorf("catalog registry version %q does not match planner registry %q", req.Snapshot.RegistryVersion, p.registry.Version())
	}
	plan := ToolPlan{RootTaskID: req.RootTaskID, CatalogGeneration: req.Snapshot.Generation}
	plan.SnapshotDigest = semanticRouteSnapshotDigest(req)
	plan.ID = "plan:" + plan.SnapshotDigest[:24]
	needs := append([]CapabilityNeed(nil), req.Needs...)
	sort.SliceStable(needs, func(i, j int) bool { return needs[i].ID < needs[j].ID })
	seenNeedIDs := make(map[string]struct{}, len(needs))
	for i := range needs {
		needs[i].ID = strings.TrimSpace(needs[i].ID)
		need := needs[i]
		if need.ID == "" {
			return ToolPlan{}, fmt.Errorf("capability need id is required")
		}
		if _, duplicate := seenNeedIDs[need.ID]; duplicate {
			return ToolPlan{}, fmt.Errorf("duplicate capability need id %q", need.ID)
		}
		seenNeedIDs[need.ID] = struct{}{}
		if need.Polarity == "" {
			need.Polarity = NeedRequire
			needs[i].Polarity = NeedRequire
		}
		if need.Polarity == NeedInquire {
			plan.Unmet = append(plan.Unmet, UnmetNeed{NeedID: need.ID, ReasonCode: "clarification_required"})
			continue
		}
		if need.Polarity != NeedRequire {
			continue
		}
		// An optional need is planned exactly like a required one and differs
		// only in what an unservable outcome means. Skipping it here instead
		// — which is what Required:false used to do — made "optional" and
		// "absent" the same thing, so the only expressible rule was one where
		// every capability must be present on every host. That is why a family
		// whose providers are conditionally attached could not be migrated: a
		// single withheld provider collapsed the whole turn.
		record := func(reasonCode string) {
			unmet := UnmetNeed{NeedID: need.ID, ReasonCode: reasonCode}
			if need.Required || unmetIsAuthoringFault(reasonCode) {
				plan.Unmet = append(plan.Unmet, unmet)
				return
			}
			plan.Omitted = append(plan.Omitted, unmet)
		}
		descriptor, exists := p.registry.Lookup(need.Capability)
		if !exists || descriptor.Deprecated {
			record("unknown_capability")
			continue
		}
		if err := validateNeed(descriptor, need); err != nil {
			record("invalid_capability_need")
			continue
		}
		if constraintDeniesNeed(req.Constraints, need, req.Now) {
			record("policy_denied")
			continue
		}
		candidate, proof, requiresConfirm, ok := p.bestProvider(req, descriptor, need)
		if !ok {
			// No candidate from an incomplete/stale lifecycle snapshot does not
			// prove that this capability is infeasible. Preserve the distinction
			// so a caller can wait for its bounded refresh or ask for
			// clarification; it must never compensate by exposing a free
			// Skill/MCP gateway.
			if reason := req.Snapshot.Coverage.UnavailabilityReason(); reason != "" {
				record(reason)
				continue
			}
			record("no_feasible_provider")
			continue
		}
		selection := PlannedSelection{
			ID:                     "selection:" + need.ID,
			NeedID:                 need.ID,
			AdapterName:            candidate.AdapterName,
			Provider:               candidate.Binding,
			ParameterAuthorization: candidate.ParameterAuthorization,
			Effects:                append([]EffectClass(nil), candidate.Effects...),
			FitProof:               proof,
			RequiresConfirm:        requiresConfirm,
			Phase:                  planPhaseForCapability(need.Capability),
			Consumes:               append([]ArtifactContract(nil), candidate.Consumes...),
			Produces:               append([]ArtifactContract(nil), candidate.Produces...),
		}
		if requiresConfirm {
			selection.ConfirmationID = ConfirmationRequirementID(need.ID)
			selection.Requires = append(selection.Requires, selection.ConfirmationID)
		}
		plan.Selections = append(plan.Selections, selection)
	}
	attachArtifactDependencies(&plan, req.Facts, req.Now)
	attachLookupGenerateDependencies(&plan, needs)
	attachScheduleDispatchDependencies(&plan)
	attachGenerateCurrentDeliverDependencies(&plan)
	attachRenderCurrentVoiceDeliverDependencies(&plan)
	attachCaptureCurrentImageDeliverDependencies(&plan)
	attachLiveDataVisualDependencies(&plan, needs)
	applyPlanningBudget(&plan, req.Budget, needs)
	recordExplainTrace(&plan, needs)
	return plan, nil
}

// IsWebLookupCapability reports whether a need fetches public web evidence.
// Conversation reuse and tool-result provenance use this, not the clock:
// a prior web_search must not drop a required current_time leg.
func IsWebLookupCapability(capability CapabilityID) bool {
	id := strings.TrimSpace(string(capability))
	return strings.HasPrefix(id, "information.search.") || id == string(CapabilityInformationFetchWeb)
}

// IsLookupCapability reports whether a need produces evidence another
// selection may wait for. Search, fetch, and the clock are the after-edge
// lookup family: generate_pdf and live-data render bind to one successful
// invocation, not to the whole repeat ceiling.
func IsLookupCapability(capability CapabilityID) bool {
	id := strings.TrimSpace(string(capability))
	return IsWebLookupCapability(capability) || id == "information.current_time"
}

func isDocumentGenerateFile(capability CapabilityID) bool {
	return strings.TrimSpace(string(capability)) == "document.generate.file"
}

func isScheduleAdministerLocal(capability CapabilityID) bool {
	return strings.TrimSpace(string(capability)) == string(CapabilityScheduleAdministerLocal)
}

func isScheduleDispatchChannel(capability CapabilityID) bool {
	return strings.TrimSpace(string(capability)) == string(CapabilityScheduleDispatchChannel)
}

func isCurrentChannelFileDeliver(capability CapabilityID, qualifiers map[string]string) bool {
	if strings.TrimSpace(string(capability)) != "artifact.deliver.current_channel" {
		return false
	}
	format := strings.TrimSpace(qualifiers["format"])
	return format == "file" || format == ""
}

func isAudioRenderSpeech(capability CapabilityID) bool {
	return strings.TrimSpace(string(capability)) == "audio.render.speech"
}

func isCurrentChannelVoiceDeliver(capability CapabilityID, qualifiers map[string]string) bool {
	if strings.TrimSpace(string(capability)) != "artifact.deliver.current_channel" {
		return false
	}
	return strings.TrimSpace(qualifiers["format"]) == "voice"
}

func isVisualCaptureDesktop(capability CapabilityID) bool {
	return strings.TrimSpace(string(capability)) == "visual.capture.desktop"
}

func isVisualRenderLiveData(capability CapabilityID) bool {
	return strings.TrimSpace(string(capability)) == "visual.render.live_data"
}

func isCurrentChannelImageDeliver(capability CapabilityID, qualifiers map[string]string) bool {
	if strings.TrimSpace(string(capability)) != "artifact.deliver.current_channel" {
		return false
	}
	return strings.TrimSpace(qualifiers["format"]) == "image"
}

// familyBaseSelectionIDs returns one selection ID per repeat family among
// matching selections: the earliest remaining sibling. Ceiling siblings
// (id#02…) are an exposure budget, not extra after-edge obligations; if
// the historical base was omitted, the lowest remaining ID still unlocks
// the producer after one successful invocation.
func familyBaseSelectionIDs(selections []PlannedSelection, match func(PlannedSelection) bool) []string {
	best := make(map[string]string)
	for _, selection := range selections {
		if !match(selection) {
			continue
		}
		family := RepeatFamilyID(selection.NeedID)
		if family == "" {
			family = RepeatFamilyID(selection.ID)
		}
		if current, exists := best[family]; exists && current <= selection.ID {
			continue
		}
		best[family] = selection.ID
	}
	ids := make([]string, 0, len(best))
	for _, id := range best {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func requiredLookupBaseSelectionIDs(plan *ToolPlan, needs []CapabilityNeed) []string {
	if plan == nil {
		return nil
	}
	requiredFamilies := make(map[string]bool, len(needs))
	for _, need := range needs {
		if need.Required && IsLookupCapability(need.Capability) {
			requiredFamilies[RepeatFamilyID(need.ID)] = true
		}
	}
	return familyBaseSelectionIDs(plan.Selections, func(selection PlannedSelection) bool {
		return IsLookupCapability(selection.FitProof.MatchedCapability) && requiredFamilies[RepeatFamilyID(selection.NeedID)]
	})
}

func requireSelectionIDs(plan *ToolPlan, dependant func(PlannedSelection) bool, required []string) {
	if plan == nil || len(required) == 0 {
		return
	}
	for i := range plan.Selections {
		if !dependant(plan.Selections[i]) {
			continue
		}
		plan.Selections[i].Requires = appendUniqueRequirements(plan.Selections[i].Requires, required)
	}
}

// attachGenerateCurrentDeliverDependencies records a capability-level after
// edge: PDF generate must complete before current-channel file deliver so
// deliver consumes the published ArtifactRef. Echo-only turns have no
// generate selection and are unchanged. Repeat generate siblings are a
// ceiling; deliver waits for the family base, then consumes the latest
// artifact of that family.
func attachGenerateCurrentDeliverDependencies(plan *ToolPlan) {
	if plan == nil {
		return
	}
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isCurrentChannelFileDeliver(selection.FitProof.MatchedCapability, selection.FitProof.QualifierBindings)
	}, familyBaseSelectionIDs(plan.Selections, func(selection PlannedSelection) bool {
		return isDocumentGenerateFile(selection.FitProof.MatchedCapability)
	}))
}

// attachRenderCurrentVoiceDeliverDependencies records a capability-level
// after edge: speech render must complete before current-channel voice
// deliver so deliver consumes the published ArtifactRef. Echo-only voice
// turns have no render selection and are unchanged. Image/file deliver
// are not wired to this edge.
func attachRenderCurrentVoiceDeliverDependencies(plan *ToolPlan) {
	if plan == nil {
		return
	}
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isCurrentChannelVoiceDeliver(selection.FitProof.MatchedCapability, selection.FitProof.QualifierBindings)
	}, familyBaseSelectionIDs(plan.Selections, func(selection PlannedSelection) bool {
		return isAudioRenderSpeech(selection.FitProof.MatchedCapability)
	}))
}

// attachCaptureCurrentImageDeliverDependencies records a capability-level
// after edge: desktop capture must complete before current-channel image
// deliver so deliver consumes the published ArtifactRef. Echo-only image
// turns have no capture selection and are unchanged. File/voice deliver
// are not wired to this edge.
func attachCaptureCurrentImageDeliverDependencies(plan *ToolPlan) {
	if plan == nil {
		return
	}
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isCurrentChannelImageDeliver(selection.FitProof.MatchedCapability, selection.FitProof.QualifierBindings)
	}, familyBaseSelectionIDs(plan.Selections, func(selection PlannedSelection) bool {
		return isVisualCaptureDesktop(selection.FitProof.MatchedCapability)
	}))
}

// attachLiveDataVisualDependencies closes the realtime image pipeline:
// trusted lookup facts -> host renderer -> current-channel image delivery.
// The renderer receives no model-controlled source, path, URL, or bytes; it
// consumes only host-recorded lookup evidence and publishes an ArtifactRef.
//
// Only the REQUIRED lookup family's base sibling gates the renderer,
// mirroring attachLookupGenerateDependencies. Optional offers and repeat
// ceiling siblings (declared MaxInvocations or a raised bundle budget)
// must not all complete before the renderer can run.
func attachLiveDataVisualDependencies(plan *ToolPlan, needs []CapabilityNeed) {
	if plan == nil {
		return
	}
	lookupIDs := requiredLookupBaseSelectionIDs(plan, needs)
	renderIDs := familyBaseSelectionIDs(plan.Selections, func(selection PlannedSelection) bool {
		return isVisualRenderLiveData(selection.FitProof.MatchedCapability)
	})
	if len(renderIDs) == 0 {
		return
	}
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isVisualRenderLiveData(selection.FitProof.MatchedCapability)
	}, lookupIDs)
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isCurrentChannelImageDeliver(selection.FitProof.MatchedCapability, selection.FitProof.QualifierBindings)
	}, renderIDs)
}

func appendUniqueRequirements(current, required []string) []string {
	seen := make(map[string]bool, len(current)+len(required))
	for _, requirement := range current {
		seen[requirement] = true
	}
	for _, requirement := range required {
		if !seen[requirement] {
			current = append(current, requirement)
			seen[requirement] = true
		}
	}
	sort.Strings(current)
	return current
}

// attachLookupGenerateDependencies wires a REQUIRED lookup as a prerequisite
// of document generation: a weather-style PDF must be rendered from fetched
// evidence, never from model memory. This is independent of artifact
// consume/produce matching. Hosts may omit the lookup need before Plan when
// the conversation already has same-topic facts; this edge then does not
// exist. An OPTIONAL lookup need (the deterministic composite companion a
// document-producing turn always carries) does not gate generation — it is an
// offer the model may use, and holding generate_pdf hostage to an uncalled
// optional search would dead-lock pure-content turns.
//
// Repeat siblings of a required lookup are an exposure ceiling, not extra
// prerequisites: the earliest remaining sibling of each required family is
// attached. Waiting for id#02…#05 dead-locks generate on unused refinements.
func attachLookupGenerateDependencies(plan *ToolPlan, needs []CapabilityNeed) {
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isDocumentGenerateFile(selection.FitProof.MatchedCapability)
	}, requiredLookupBaseSelectionIDs(plan, needs))
}

// attachScheduleDispatchDependencies records a capability-level after edge:
// local administer must complete before the independent dispatch selection
// registers a due-time delivery intent. Dispatch never writes Delivery onto
// the administer schema; the edge only orders host-owned work.
func attachScheduleDispatchDependencies(plan *ToolPlan) {
	if plan == nil {
		return
	}
	requireSelectionIDs(plan, func(selection PlannedSelection) bool {
		return isScheduleDispatchChannel(selection.FitProof.MatchedCapability)
	}, familyBaseSelectionIDs(plan.Selections, func(selection PlannedSelection) bool {
		return isScheduleAdministerLocal(selection.FitProof.MatchedCapability)
	}))
}

const reasonOptionalBudgetOmitted = "optional_budget_omitted"

// applyPlanningBudget keeps a closed required-wave prefix that fits
// MaxSelections, then fills leftover slots/tokens with optional selections
// one at a time. A later required wave that does not fit is budget_exceeded.
// If even the first required wave cannot fit, every remaining required
// selection is planning_budget_exceeded and optional is not filled. Unlimited
// budget still partitions so Selections start with required.
func applyPlanningBudget(plan *ToolPlan, budget PlanningBudget, needs []CapabilityNeed) {
	if plan == nil || len(plan.Selections) == 0 {
		return
	}
	required, optional := partitionRequiredOptionalSelections(plan.Selections, needs)
	if budget.MaxSelections <= 0 && budget.MaxSchemaTokens <= 0 {
		plan.Selections = append(required, optional...)
		return
	}
	kept := make([]PlannedSelection, 0, len(plan.Selections))
	if len(required) > 0 {
		waves := selectionDependencyWaves(required)
		if planningWaveExceedsBudget(nil, waves[0], budget) {
			plan.Selections = nil
			for _, wave := range waves {
				for _, selection := range wave {
					plan.Unmet = append(plan.Unmet, UnmetNeed{NeedID: selection.NeedID, ReasonCode: "planning_budget_exceeded"})
				}
			}
			omitOptionalBudgetOverflow(plan, optional)
			return
		}
		var dropped []PlannedSelection
		for _, wave := range waves {
			if planningWaveExceedsBudget(kept, wave, budget) {
				dropped = append(dropped, wave...)
				continue
			}
			kept = append(kept, wave...)
		}
		for _, selection := range dropped {
			plan.Unmet = append(plan.Unmet, UnmetNeed{NeedID: selection.NeedID, ReasonCode: "budget_exceeded"})
		}
	}
	keptIDs := make(map[string]bool, len(kept)+len(optional))
	for _, selection := range kept {
		keptIDs[selection.ID] = true
	}
	for _, wave := range selectionDependencyWaves(optional) {
		for _, selection := range wave {
			if optionalSelectionParentMissing(selection, keptIDs) || planningWaveExceedsBudget(kept, []PlannedSelection{selection}, budget) {
				plan.Omitted = append(plan.Omitted, UnmetNeed{NeedID: selection.NeedID, ReasonCode: reasonOptionalBudgetOmitted})
				continue
			}
			kept = append(kept, selection)
			keptIDs[selection.ID] = true
		}
	}
	plan.Selections = kept
}

func partitionRequiredOptionalSelections(selections []PlannedSelection, needs []CapabilityNeed) (required, optional []PlannedSelection) {
	requiredByNeed := make(map[string]bool, len(needs))
	known := make(map[string]bool, len(needs))
	for _, need := range needs {
		requiredByNeed[need.ID] = need.Required
		known[need.ID] = true
	}
	for _, selection := range selections {
		if !known[selection.NeedID] || requiredByNeed[selection.NeedID] {
			required = append(required, selection)
			continue
		}
		optional = append(optional, selection)
	}
	return required, optional
}

func optionalSelectionParentMissing(selection PlannedSelection, keptIDs map[string]bool) bool {
	for _, requirement := range selection.Requires {
		if strings.HasPrefix(requirement, "selection:") && !keptIDs[requirement] {
			return true
		}
	}
	return false
}

func omitOptionalBudgetOverflow(plan *ToolPlan, optional []PlannedSelection) {
	if plan == nil {
		return
	}
	for _, selection := range optional {
		plan.Omitted = append(plan.Omitted, UnmetNeed{NeedID: selection.NeedID, ReasonCode: reasonOptionalBudgetOmitted})
	}
}

func planningWaveExceedsBudget(kept, wave []PlannedSelection, budget PlanningBudget) bool {
	if budget.MaxSelections > 0 && len(kept)+len(wave) > budget.MaxSelections {
		return true
	}
	if budget.MaxSchemaTokens > 0 && selectionSchemaTokenCost(kept)+selectionSchemaTokenCost(wave) > budget.MaxSchemaTokens {
		return true
	}
	return false
}

// selectionSchemaTokenCost is a conservative, deterministic estimate of the
// CatalogRenderer projection for a selection set. It uses the already-bound
// parameter authorization, never provider descriptions or model text.
func selectionSchemaTokenCost(selections []PlannedSelection) int {
	total := 0
	for _, selection := range selections {
		total += 24
		total += 8 * len(selection.ParameterAuthorization.AllowedFields)
		if strings.TrimSpace(selection.ParameterAuthorization.Digest) != "" {
			total += 4
		}
	}
	return total
}

func selectionDependencyWaves(selections []PlannedSelection) [][]PlannedSelection {
	remaining := make(map[string]PlannedSelection, len(selections))
	for _, selection := range selections {
		remaining[selection.ID] = selection
	}
	waves := make([][]PlannedSelection, 0, len(selections))
	for len(remaining) > 0 {
		wave := make([]PlannedSelection, 0, len(remaining))
		for _, selection := range remaining {
			ready := true
			for _, requirement := range selection.Requires {
				if _, blocked := remaining[requirement]; blocked {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, selection)
			}
		}
		if len(wave) == 0 {
			for _, selection := range remaining {
				wave = append(wave, selection)
			}
		}
		sort.Slice(wave, func(i, j int) bool { return wave[i].ID < wave[j].ID })
		waves = append(waves, wave)
		for _, selection := range wave {
			delete(remaining, selection.ID)
		}
	}
	return waves
}

func recordExplainTrace(plan *ToolPlan, needs []CapabilityNeed) {
	if plan == nil {
		return
	}
	decisions := make([]ToolDecision, 0, len(needs)+len(plan.Selections)+len(plan.Unmet))
	appendDecision := func(stage, subject, event, reason, needID string) {
		decisions = append(decisions, ToolDecision{
			Stage: stage, Subject: subject, Event: event, ReasonCode: reason, NeedID: needID,
		})
	}
	for _, need := range needs {
		polarity := need.Polarity
		if polarity == "" {
			polarity = NeedRequire
		}
		if polarity != NeedRequire {
			continue
		}
		reason := "need_required"
		if !need.Required {
			reason = "need_optional"
		}
		appendDecision(TraceStageSemantics, string(need.Capability), "recognized", reason, need.ID)
	}
	if plan.CatalogGeneration > 0 || strings.TrimSpace(plan.SnapshotDigest) != "" {
		appendDecision(TraceStageCatalog, "catalog", "frozen", "snapshot_bound", "")
	}
	for _, selection := range plan.Selections {
		appendDecision(TraceStageFeasibility, string(selection.FitProof.MatchedCapability), "selected", "provider_selected", selection.NeedID)
		appendDecision(TraceStageOptimization, string(selection.FitProof.MatchedCapability), "selected", "highest_quality_provider", selection.NeedID)
		appendDecision(TraceStageMaterialization, string(selection.FitProof.MatchedCapability), "selected", "selection_materialized", selection.NeedID)
		if strings.TrimSpace(selection.FitProof.ProviderBindingID) != "" || strings.TrimSpace(selection.Provider.Kind) != "" {
			appendDecision(TraceStageBinding, string(selection.FitProof.MatchedCapability), "bound", "fit_proof_bound", selection.NeedID)
		}
		if strings.TrimSpace(selection.ParameterAuthorization.Digest) != "" {
			appendDecision(TraceStageRendering, string(selection.FitProof.MatchedCapability), "projected", "canonical_schema_bound", selection.NeedID)
		}
		for _, requirement := range selection.Requires {
			appendDecision(TraceStageDependency, string(selection.FitProof.MatchedCapability), "depends", "after:"+requirement, selection.NeedID)
		}
		for range selection.ArtifactDependencies {
			appendDecision(TraceStageArtifact, string(selection.FitProof.MatchedCapability), "consumes", "artifact_bound", selection.NeedID)
		}
	}
	for _, unmet := range plan.Unmet {
		event := "unmet"
		if unmet.ReasonCode == "policy_denied" {
			event = "denied"
		}
		subject := unmet.NeedID
		for _, need := range needs {
			if need.ID == unmet.NeedID {
				subject = string(need.Capability)
				break
			}
		}
		appendDecision(TraceStageFeasibility, subject, event, unmet.ReasonCode, unmet.NeedID)
		appendDecision(TraceStageMaterialization, subject, event, unmet.ReasonCode, unmet.NeedID)
	}
	// Omitted optional needs get a feasibility row too. They did not fail the
	// plan, but "this host had nothing for that capability" is the reason the
	// model was never offered it, and that has to be answerable from the trace
	// rather than by re-deriving the host's wiring after the fact.
	for _, omitted := range plan.Omitted {
		subject := omitted.NeedID
		for _, need := range needs {
			if need.ID == omitted.NeedID {
				subject = string(need.Capability)
				break
			}
		}
		appendDecision(TraceStageFeasibility, subject, "omitted", omitted.ReasonCode, omitted.NeedID)
	}
	plan.Decisions = decisions
	events := make([]TraceEvent, 0, len(decisions))
	for _, decision := range decisions {
		events = append(events, TraceEvent{
			Stage: decision.Stage, Subject: decision.Subject, Event: decision.Event, ReasonCode: decision.ReasonCode,
		})
	}
	plan.Trace = ExplainTrace{PlanID: plan.ID, SnapshotDigest: plan.SnapshotDigest, Events: events}
}

func planPhaseForCapability(capability CapabilityID) PlanPhase {
	if strings.HasPrefix(string(capability), "artifact.deliver.") {
		return PlanPhaseDelivery
	}
	return PlanPhaseExecution
}

func attachArtifactDependencies(plan *ToolPlan, facts []RoutingFact, now time.Time) {
	if plan == nil {
		return
	}
	blocked := make(map[string]bool)
	for i := range plan.Selections {
		selection := &plan.Selections[i]
		for _, consumed := range selection.Consumes {
			if !consumed.Required {
				continue
			}
			producerID := ""
			for _, candidate := range plan.Selections {
				if candidate.ID == selection.ID || !producesArtifact(candidate.Produces, consumed) {
					continue
				}
				if producerID == "" || candidate.ID < producerID {
					producerID = candidate.ID
				}
			}
			if producerID != "" {
				selection.Requires = append(selection.Requires, producerID)
				selection.ArtifactDependencies = append(selection.ArtifactDependencies, ArtifactDependency{ProducerSelection: producerID, Contract: consumed})
				continue
			}
			if source, ok := trustedArtifactDependency(facts, consumed, now); ok {
				selection.ArtifactDependencies = append(selection.ArtifactDependencies, source)
				continue
			}
			{
				blocked[selection.ID] = true
				plan.Unmet = append(plan.Unmet, UnmetNeed{NeedID: selection.NeedID, ReasonCode: "artifact_dependency_missing"})
			}
		}
		sort.Strings(selection.Requires)
		sort.Slice(selection.ArtifactDependencies, func(i, j int) bool {
			left, right := selection.ArtifactDependencies[i], selection.ArtifactDependencies[j]
			if left.ProducerSelection != right.ProducerSelection {
				return left.ProducerSelection < right.ProducerSelection
			}
			if left.ArtifactID != right.ArtifactID {
				return left.ArtifactID < right.ArtifactID
			}
			if left.Contract.Kind != right.Contract.Kind {
				return left.Contract.Kind < right.Contract.Kind
			}
			return left.Contract.MIMEType < right.Contract.MIMEType
		})
	}
	if len(blocked) == 0 {
		return
	}
	kept := plan.Selections[:0]
	for _, selection := range plan.Selections {
		if !blocked[selection.ID] {
			kept = append(kept, selection)
		}
	}
	plan.Selections = kept
}

func trustedArtifactDependency(facts []RoutingFact, consumed ArtifactContract, now time.Time) (ArtifactDependency, bool) {
	var source ArtifactDependency
	for _, fact := range facts {
		if fact.Authority != AuthorityRuntime && fact.Authority != AuthorityChannel {
			continue
		}
		if !fact.ValidUntil.IsZero() && now.After(fact.ValidUntil) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(fact.Kind), "artifact_available") {
			continue
		}
		binding := fact.Artifact
		if binding == nil {
			// Compatibility facts remain usable for planning tests and older
			// trusted callers, but their resulting dependency is intentionally
			// incomplete and therefore rejected by durable RouteState/execution.
			// A host that wants a materialized surface must publish full binding.
			legacy := ArtifactBinding{ID: strings.TrimSpace(fact.Attributes["artifact_id"]), Kind: strings.TrimSpace(fact.Attributes["kind"]), MIMEType: strings.TrimSpace(fact.Attributes["mime_type"])}
			if legacy.ID == "" {
				continue
			}
			binding = &legacy
		}
		if !strings.EqualFold(strings.TrimSpace(binding.Kind), strings.TrimSpace(consumed.Kind)) {
			continue
		}
		if mimeType := strings.TrimSpace(consumed.MIMEType); mimeType != "" && !strings.EqualFold(strings.TrimSpace(binding.MIMEType), mimeType) {
			continue
		}
		candidate := ArtifactDependency{ArtifactID: binding.ID, Artifact: *binding, Contract: consumed}
		if source.ArtifactID != "" && (source.ArtifactID != candidate.ArtifactID || source.Artifact != candidate.Artifact) {
			return ArtifactDependency{}, false
		}
		source = candidate
	}
	return source, source.ArtifactID != ""
}

func producesArtifact(produced []ArtifactContract, consumed ArtifactContract) bool {
	for _, candidate := range produced {
		if !strings.EqualFold(strings.TrimSpace(candidate.Kind), strings.TrimSpace(consumed.Kind)) {
			continue
		}
		if strings.TrimSpace(consumed.MIMEType) == "" || strings.EqualFold(strings.TrimSpace(candidate.MIMEType), strings.TrimSpace(consumed.MIMEType)) {
			return true
		}
	}
	return false
}

func sameArtifactContract(left, right ArtifactContract) bool {
	return strings.EqualFold(strings.TrimSpace(left.Kind), strings.TrimSpace(right.Kind)) &&
		strings.EqualFold(strings.TrimSpace(left.MIMEType), strings.TrimSpace(right.MIMEType)) &&
		left.Required == right.Required
}

func validArtifactBinding(binding ArtifactBinding) bool {
	if strings.TrimSpace(binding.ID) == "" || strings.TrimSpace(binding.Kind) == "" || strings.TrimSpace(binding.MIMEType) == "" || strings.TrimSpace(binding.IntegrityDigest) == "" || strings.TrimSpace(binding.ProducerSelection) == "" {
		return false
	}
	return ValidateArtifactScope(binding.Scope) == nil
}

func artifactBindingMatchesContract(binding ArtifactBinding, contract ArtifactContract) bool {
	return strings.EqualFold(strings.TrimSpace(binding.Kind), strings.TrimSpace(contract.Kind)) &&
		(strings.TrimSpace(contract.MIMEType) == "" || strings.EqualFold(strings.TrimSpace(binding.MIMEType), strings.TrimSpace(contract.MIMEType)))
}

func canonicalArtifactBinding(binding ArtifactBinding) string {
	return strings.Join([]string{
		strings.TrimSpace(binding.ID), strings.ToLower(strings.TrimSpace(binding.Kind)), strings.ToLower(strings.TrimSpace(binding.MIMEType)),
		strings.TrimSpace(binding.IntegrityDigest), strings.TrimSpace(binding.ProducerSelection), binding.Scope.RootTaskID,
		binding.Scope.PlanID, binding.Scope.SessionID, binding.Scope.TurnID, binding.Scope.PrincipalID,
	}, "\x1e")
}

// validateToolPlanArtifactDependencies makes ArtifactDependency a real plan
// invariant rather than advisory metadata. A required consumer needs exactly
// one source edge per contract; that source is either an in-plan producer or
// one trusted pre-existing ArtifactRef, never a scope-wide artifact search.
func validateToolPlanArtifactDependencies(plan ToolPlan) error {
	selectionIDs := make(map[string]bool, len(plan.Selections))
	for _, selection := range plan.Selections {
		selectionIDs[selection.ID] = true
	}
	for _, selection := range plan.Selections {
		for _, consumed := range selection.Consumes {
			if !consumed.Required {
				continue
			}
			matches := 0
			for _, dependency := range selection.ArtifactDependencies {
				if sameArtifactContract(dependency.Contract, consumed) {
					matches++
				}
			}
			if matches != 1 {
				return fmt.Errorf("artifact_dependency_invalid")
			}
		}
		seen := make(map[string]bool, len(selection.ArtifactDependencies))
		for _, dependency := range selection.ArtifactDependencies {
			producer, artifactID := strings.TrimSpace(dependency.ProducerSelection), strings.TrimSpace(dependency.ArtifactID)
			if (producer == "" && artifactID == "") || (producer != "" && artifactID != "") || !dependency.Contract.Required {
				return fmt.Errorf("artifact_dependency_invalid")
			}
			if artifactID != "" && dependency.Artifact != (ArtifactBinding{}) && (!validArtifactBinding(dependency.Artifact) || dependency.Artifact.ID != artifactID || !artifactBindingMatchesContract(dependency.Artifact, dependency.Contract)) {
				return fmt.Errorf("artifact_dependency_invalid")
			}
			if producer != "" && dependency.Artifact != (ArtifactBinding{}) {
				return fmt.Errorf("artifact_dependency_invalid")
			}
			if producer != "" && (!selectionIDs[producer] || producer == selection.ID) {
				return fmt.Errorf("artifact_dependency_invalid")
			}
			if producer != "" {
				producerFound, requireFound := false, false
				for _, candidate := range plan.Selections {
					if candidate.ID != producer {
						continue
					}
					producerFound = producesArtifact(candidate.Produces, dependency.Contract)
					break
				}
				for _, requirement := range selection.Requires {
					if requirement == producer {
						requireFound = true
						break
					}
				}
				if !producerFound || !requireFound {
					return fmt.Errorf("artifact_dependency_invalid")
				}
			}
			contractFound := false
			for _, consumed := range selection.Consumes {
				if consumed.Required && sameArtifactContract(dependency.Contract, consumed) {
					contractFound = true
					break
				}
			}
			if !contractFound {
				return fmt.Errorf("artifact_dependency_invalid")
			}
			key := producer + "\x00" + artifactID + "\x00" + canonicalArtifactBinding(dependency.Artifact) + "\x00" + canonicalArtifacts([]ArtifactContract{dependency.Contract})
			if seen[key] {
				return fmt.Errorf("artifact_dependency_invalid")
			}
			seen[key] = true
		}
	}
	return nil
}

func clonePlannedSelection(in PlannedSelection) PlannedSelection {
	out := in
	out.ConfirmationID = strings.TrimSpace(in.ConfirmationID)
	out.Effects = append([]EffectClass(nil), in.Effects...)
	out.Requires = append([]string(nil), in.Requires...)
	out.Consumes = append([]ArtifactContract(nil), in.Consumes...)
	out.Produces = append([]ArtifactContract(nil), in.Produces...)
	out.ArtifactDependencies = append([]ArtifactDependency(nil), in.ArtifactDependencies...)
	for i := range out.ArtifactDependencies {
		out.ArtifactDependencies[i].Artifact = in.ArtifactDependencies[i].Artifact
	}
	out.FitProof.QualifierBindings = cloneStringMap(in.FitProof.QualifierBindings)
	return out
}

// validateNeed makes qualifier validation symmetric with catalog publication.
// A semantic extractor can preserve unrecognised text as evidence, but it
// cannot use an undeclared or incomplete qualifier to influence feasibility.
func validateNeed(descriptor CapabilityDescriptor, need CapabilityNeed) error {
	if strings.TrimSpace(string(need.Capability)) == "" {
		return fmt.Errorf("capability is required")
	}
	for qualifier, value := range need.Qualifiers {
		rule, ok := descriptor.Qualifiers[qualifier]
		if !ok {
			return fmt.Errorf("capability %q does not declare qualifier %q", descriptor.ID, qualifier)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("capability %q qualifier %q is empty", descriptor.ID, qualifier)
		}
		if len(rule.Values) > 0 && !containsFold(rule.Values, value) {
			return fmt.Errorf("capability %q qualifier %q value %q is not allowed", descriptor.ID, qualifier, value)
		}
	}
	for qualifier, rule := range descriptor.Qualifiers {
		if rule.Required && strings.TrimSpace(need.Qualifiers[qualifier]) == "" {
			return fmt.Errorf("capability %q requires qualifier %q", descriptor.ID, qualifier)
		}
	}
	return nil
}

func (p *ToolPlanner) bestProvider(req RouteRequest, descriptor CapabilityDescriptor, need CapabilityNeed) (ProviderSpec, FitProof, bool, bool) {
	candidates := append([]ProviderSpec(nil), req.Snapshot.Providers...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Binding.StableID() < candidates[j].Binding.StableID() })
	var selected ProviderSpec
	selectedQuality := -1.0
	found := false
	for _, candidate := range candidates {
		if !candidate.Classification.plannable() {
			// Fixed control-plane and quarantined entries are catalog coverage
			// records only; they must never become planner candidates even if a
			// hand-built snapshot bypassed the Publish gate.
			continue
		}
		if !providerLifecycleEligible(req, candidate) {
			continue
		}
		if len(candidate.ChannelScopes) > 0 && !containsFold(candidate.ChannelScopes, req.ChannelScope) {
			continue
		}
		if !effectsWithinCapability(candidate.Effects, descriptor.Effects) {
			continue
		}
		for _, provision := range candidate.Provides {
			if provision.Capability != need.Capability || !qualifiersMatch(need.Qualifiers, provision.Qualifiers) {
				continue
			}
			// Candidates are pre-sorted by immutable binding identity. Therefore a
			// quality tie keeps that ordering deterministic while a better declared
			// implementation is selected regardless of discovery order.
			if !found || provision.Quality > selectedQuality {
				selected, selectedQuality, found = candidate, provision.Quality, true
			}
		}
	}
	if !found {
		return ProviderSpec{}, FitProof{}, false, false
	}
	proof := FitProof{
		NeedID:             need.ID,
		ProviderBindingID:  selected.Binding.StableID(),
		MatchedCapability:  need.Capability,
		QualifierBindings:  cloneStringMap(need.Qualifiers),
		SnapshotGeneration: req.Snapshot.Generation,
	}
	proof.Digest = fitProofDigest(proof)
	return selected, proof, constraintRequiresConfirm(req.Constraints, need.Capability, req.Now), true
}

// providerLifecycleEligible applies the same coverage policy to every provider
// kind. A partial inventory is never a basis for executing a candidate from
// that family. A stale catalog may retain a read-only candidate only inside its
// explicitly published stale-while-revalidate window; external and sensitive
// candidates always require complete, fresh coverage.
func providerLifecycleEligible(req RouteRequest, candidate ProviderSpec) bool {
	if !candidate.Ready {
		return false
	}
	coverage := req.Snapshot.Coverage.ForProviderKind(candidate.Binding.Kind)
	switch coverage.State {
	case CatalogCoverageComplete:
		return candidate.ReadyUntil.IsZero() || !req.Now.After(candidate.ReadyUntil)
	case CatalogCoverageStale:
		if !effectsAreReadOnly(candidate.Effects) || coverage.StaleUntil.IsZero() || req.Now.After(coverage.StaleUntil) {
			return false
		}
		// A stale coverage record is the trusted declaration that this exact
		// published entry can be used while refresh is in progress. ReadyUntil
		// may already have elapsed; the bounded StaleUntil is the only allowed
		// extension and is never available to mutating providers.
		return true
	default:
		return false
	}
}

func effectsAreReadOnly(effects []EffectClass) bool {
	return len(effects) == 1 && effects[0] == EffectReadOnly
}

func qualifiersMatch(need, provision map[string]string) bool {
	for name, value := range need {
		if !strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(provision[name])) {
			return false
		}
	}
	return true
}

func effectsWithinCapability(provider, declared []EffectClass) bool {
	for _, effect := range provider {
		found := false
		for _, allowed := range declared {
			if effect == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func constraintDenies(constraints []RoutingConstraint, capability CapabilityID, now time.Time) bool {
	return constraintDeniesNeed(constraints, CapabilityNeed{Capability: capability}, now)
}

func constraintDeniesNeed(constraints []RoutingConstraint, need CapabilityNeed, now time.Time) bool {
	for _, constraint := range constraints {
		if constraint.Capability != need.Capability || constraint.Effect != "deny" || !trustedConstraintAuthority(constraint.Authority) {
			continue
		}
		if !constraint.ValidUntil.IsZero() && now.After(constraint.ValidUntil) {
			continue
		}
		if !constraintAttributesMatchNeed(constraint.Attributes, need.Qualifiers) {
			continue
		}
		return true
	}
	return false
}

// constraintAttributesMatchNeed treats empty attributes as a capability-wide
// deny. Non-empty attributes are qualifier filters, so format=file does not
// deny format=image on the same capability.
func constraintAttributesMatchNeed(attrs, qualifiers map[string]string) bool {
	if len(attrs) == 0 {
		return true
	}
	for key, value := range attrs {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if strings.TrimSpace(qualifiers[key]) != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func constraintRequiresConfirm(constraints []RoutingConstraint, capability CapabilityID, now time.Time) bool {
	for _, constraint := range constraints {
		if constraint.Capability == capability && constraint.Effect == "require_confirmation" && trustedConstraintAuthority(constraint.Authority) && (constraint.ValidUntil.IsZero() || !now.After(constraint.ValidUntil)) {
			return true
		}
	}
	return false
}

func trustedConstraintAuthority(authority FactAuthority) bool {
	return authority == AuthorityPolicy || authority == AuthorityRuntime || authority == AuthorityChannel
}

func semanticPlanID(req RouteRequest) string {
	return "plan:" + semanticRouteSnapshotDigest(req)[:24]
}

// semanticRouteSnapshotDigest covers the full planner input vector. It is
// kept separate from the short plan ID so durable revision publication can
// compare the complete digest rather than a truncated display identity.
func semanticRouteSnapshotDigest(req RouteRequest) string {
	parts := []string{req.RootTaskID, req.SessionID, req.ChannelScope, req.Snapshot.RegistryVersion, fmt.Sprintf("%d", req.Snapshot.Generation), req.TurnID,
		"coverage", string(req.Snapshot.Coverage.State), req.Snapshot.Coverage.ReasonCode, req.Snapshot.Coverage.StaleUntil.UTC().Format(time.RFC3339Nano),
	}
	families := append([]CatalogCoverageFamily(nil), req.Snapshot.Coverage.Families...)
	sort.Slice(families, func(i, j int) bool { return families[i].Kind < families[j].Kind })
	for _, family := range families {
		// ObservedAt is intentionally excluded: it is diagnostic metadata, not
		// a change in the route's semantic meaning. The state/reason/window are
		// authorization-relevant and therefore part of the immutable identity.
		parts = append(parts, "coverage_family", family.Kind, string(family.State), family.ReasonCode, family.StaleUntil.UTC().Format(time.RFC3339Nano))
	}
	providers := append([]ProviderSpec(nil), req.Snapshot.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].Binding.StableID() < providers[j].Binding.StableID() })
	for _, provider := range providers {
		parts = append(parts,
			"provider", provider.AdapterName, provider.Binding.StableID(), fmt.Sprintf("%t", provider.Ready),
			provider.ReadyUntil.UTC().Format(time.RFC3339Nano), strings.Join(provider.ChannelScopes, "\x1f"),
			provider.ParameterAuthorization.Digest, provider.ParameterAuthorization.CanonicalizerVer, strings.Join(provider.ParameterAuthorization.AllowedFields, "\x1f"),
			canonicalEffects(provider.Effects), canonicalProvisions(provider.Provides), canonicalArtifacts(provider.Consumes), canonicalArtifacts(provider.Produces),
		)
	}
	needs := append([]CapabilityNeed(nil), req.Needs...)
	sort.Slice(needs, func(i, j int) bool { return needs[i].ID < needs[j].ID })
	for _, need := range needs {
		parts = append(parts, "need", need.ID, string(need.Capability), string(need.Polarity), fmt.Sprintf("%t", need.Required), canonicalStringMap(need.Qualifiers))
	}
	constraints := append([]RoutingConstraint(nil), req.Constraints...)
	sort.Slice(constraints, func(i, j int) bool { return constraints[i].ID < constraints[j].ID })
	for _, constraint := range constraints {
		parts = append(parts, "constraint", constraint.ID, string(constraint.Capability), constraint.Effect, string(constraint.Authority), constraint.ValidUntil.UTC().Format(time.RFC3339Nano), canonicalStringMap(constraint.Attributes))
	}
	if req.Budget.MaxSelections > 0 {
		parts = append(parts, "budget", fmt.Sprintf("%d", req.Budget.MaxSelections))
	}
	if req.Budget.MaxSchemaTokens > 0 {
		parts = append(parts, "schema_budget", fmt.Sprintf("%d", req.Budget.MaxSchemaTokens))
	}
	facts := append([]RoutingFact(nil), req.Facts...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	for _, fact := range facts {
		artifact := ""
		if fact.Artifact != nil {
			artifact = canonicalArtifactBinding(*fact.Artifact)
		}
		parts = append(parts, "fact", fact.ID, fact.Kind, string(fact.Authority), fact.ValidUntil.UTC().Format(time.RFC3339Nano), canonicalStringMap(fact.Attributes), artifact)
	}
	return SchemaDigest([]byte(strings.Join(parts, "\x00")))
}

func canonicalEffects(effects []EffectClass) string {
	values := make([]string, 0, len(effects))
	for _, effect := range effects {
		values = append(values, string(effect))
	}
	sort.Strings(values)
	return strings.Join(values, "\x1f")
}

func canonicalProvisions(provisions []CapabilityProvision) string {
	values := make([]string, 0, len(provisions))
	for _, provision := range provisions {
		values = append(values, strings.Join([]string{string(provision.Capability), canonicalStringMap(provision.Qualifiers), fmt.Sprintf("%.9g", provision.Quality)}, "\x1e"))
	}
	sort.Strings(values)
	return strings.Join(values, "\x1f")
}

func canonicalArtifacts(contracts []ArtifactContract) string {
	values := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		values = append(values, strings.Join([]string{strings.ToLower(strings.TrimSpace(contract.Kind)), strings.ToLower(strings.TrimSpace(contract.MIMEType)), fmt.Sprintf("%t", contract.Required)}, "\x1e"))
	}
	sort.Strings(values)
	return strings.Join(values, "\x1f")
}

func canonicalArtifactDependencies(dependencies []ArtifactDependency) string {
	values := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		values = append(values, strings.Join([]string{
			strings.TrimSpace(dependency.ProducerSelection),
			strings.TrimSpace(dependency.ArtifactID),
			canonicalArtifactBinding(dependency.Artifact),
			strings.ToLower(strings.TrimSpace(dependency.Contract.Kind)),
			strings.ToLower(strings.TrimSpace(dependency.Contract.MIMEType)),
			fmt.Sprintf("%t", dependency.Contract.Required),
		}, "\x1e"))
	}
	sort.Strings(values)
	return strings.Join(values, "\x1f")
}

func canonicalStringMap(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, "\x1f")
}

func fitProofDigest(proof FitProof) string {
	keys := make([]string, 0, len(proof.QualifierBindings))
	for key := range proof.QualifierBindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{proof.NeedID, proof.ProviderBindingID, string(proof.MatchedCapability), fmt.Sprintf("%d", proof.SnapshotGeneration)}
	for _, key := range keys {
		parts = append(parts, key+"="+proof.QualifierBindings[key])
	}
	return SchemaDigest([]byte(strings.Join(parts, "\x00")))
}
