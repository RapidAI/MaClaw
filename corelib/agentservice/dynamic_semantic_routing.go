package agentservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// DynamicCapabilityNeedRequest is the bounded input to a trusted semantic
// need resolver. In particular it contains neither a provider inventory nor a
// tool, Skill, MCP server, or MCP tool name. A resolver may use a governed
// intent/embedding/structured-understanding service, but must return declared
// capability needs rather than implementation identities.
type DynamicCapabilityNeedRequest struct {
	Principal  Principal
	UserText   string
	RootTaskID string
	SessionID  string
	TurnID     string
	// TaskRelation is resolved by the trusted ingress before semantic planning.
	// Text similarity, user identity, destination, model history and tool names
	// may contribute evidence, but cannot by themselves set Continue/Refine.
	// A continuation handle is an opaque host-issued proof, never a model field.
	TaskRelation TaskRelationDecision
	ChannelScope string
	// WorkflowPolicy and MutationScope are trusted host context expressed as
	// policy states, not legacy function names. A policy adapter maps them only
	// to capability constraints before planning.
	WorkflowPolicy string
	MutationScope  string
	// DestinationID is an optional host-trusted destination (for example
	// group:<id>). It is never parsed from user text. Empty on Core Agent
	// turns that have no channel destination.
	DestinationID string
}

// TaskRelationKind classifies the new user turn against a host-owned logical
// task. The resolver is intentionally conservative: ambiguity never inherits
// a mutation need or a previous invocation authority.
type TaskRelationKind string

const (
	TaskRelationNewTask               TaskRelationKind = "new_task"
	TaskRelationContinue              TaskRelationKind = "continue"
	TaskRelationRefine                TaskRelationKind = "refine"
	TaskRelationClarificationRequired TaskRelationKind = "clarification_required"
)

// TaskRelationDecision is validated by the host before it reaches a dynamic
// resolver. ContinuationHandle proves the selected task belongs to the
// authenticated principal/session; RootTaskID must be the same root that the
// host validated. AmendmentDigest is required by the future refine publisher,
// but deliberately has no authority to mutate a route by itself.
type TaskRelationDecision struct {
	Kind               TaskRelationKind
	RootTaskID         string
	ContinuationHandle string
	// AmendmentCommandID is an opaque, server-issued command selected through
	// an explicit refine action. It is meaningful only with the package-private
	// ingress verification marker below; client/model transport cannot attach a
	// TaskRelationDecision to ExecuteRequest.
	AmendmentCommandID string
	AmendmentDigest    string
	AmendmentRevision  uint64
	AmendmentFencing   uint64
	EvidenceIDs        []string
	// These bindings are copied from the authenticated ingress after handle
	// verification. They prevent a handle selected in one tenant, principal or
	// session from being attached to another request by a later call layer.
	TenantID    string
	PrincipalID string
	SessionID   string
	// verifiedByTrustedIngress is intentionally unexported. A non-empty opaque
	// string supplied by an API client, model, or embedding caller is evidence at
	// most; it is not a verified continuation. The authenticated ingress stamps
	// this only after it has validated the handle against the authoritative task
	// record and current route lineage.
	verifiedByTrustedIngress bool
}

func (d TaskRelationDecision) permitsContinuation(request DynamicCapabilityNeedRequest) bool {
	if d.Kind != TaskRelationContinue && d.Kind != TaskRelationRefine {
		return false
	}
	valid := d.verifiedByTrustedIngress &&
		strings.TrimSpace(d.ContinuationHandle) != "" &&
		strings.TrimSpace(d.RootTaskID) != "" &&
		strings.TrimSpace(d.RootTaskID) == strings.TrimSpace(request.RootTaskID) &&
		strings.TrimSpace(d.TenantID) == strings.TrimSpace(request.Principal.TenantID) &&
		strings.TrimSpace(d.PrincipalID) == memoryOwnerIDForPrincipal(request.Principal) &&
		strings.TrimSpace(d.SessionID) == strings.TrimSpace(request.SessionID)
	if !valid {
		return false
	}
	if d.Kind != TaskRelationRefine {
		return true
	}
	return strings.TrimSpace(d.AmendmentCommandID) != "" && strings.TrimSpace(d.AmendmentDigest) != "" && d.AmendmentRevision > 0 && d.AmendmentFencing > 0
}

// verifiedTaskRelationDecision is the package-private handoff from an
// authenticated ingress to semantic planning. It deliberately takes the
// authenticated principal and trusted session as arguments rather than
// trusting scope fields carried by a caller's decision. The caller must already
// have verified that ContinuationHandle selects this RootTaskID and that its
// route revision is current; this helper does not perform that persistence
// lookup itself.
func verifiedTaskRelationDecision(decision TaskRelationDecision, principal Principal, sessionID string) TaskRelationDecision {
	decision.ContinuationHandle = strings.TrimSpace(decision.ContinuationHandle)
	decision.RootTaskID = strings.TrimSpace(decision.RootTaskID)
	decision.AmendmentCommandID = strings.TrimSpace(decision.AmendmentCommandID)
	decision.AmendmentDigest = strings.TrimSpace(decision.AmendmentDigest)
	decision.TenantID = strings.TrimSpace(principal.TenantID)
	decision.PrincipalID = memoryOwnerIDForPrincipal(principal)
	decision.SessionID = strings.TrimSpace(sessionID)
	decision.EvidenceIDs = append([]string(nil), decision.EvidenceIDs...)
	decision.verifiedByTrustedIngress = true
	return decision
}

func cloneTaskRelationDecision(value *TaskRelationDecision) TaskRelationDecision {
	if value == nil {
		return TaskRelationDecision{Kind: TaskRelationNewTask}
	}
	result := *value
	result.EvidenceIDs = append([]string(nil), value.EvidenceIDs...)
	return result
}

// DynamicCapabilityNeedResolution says whether this request belongs to a
// dynamic capability family governed by the semantic router. Managed requests
// never fall back to the legacy bulk Skill/MCP surface: an empty or unmet
// semantic plan is an explicit unavailability condition, not permission to
// expose every discovered provider.
type DynamicCapabilityNeedResolution struct {
	Managed     bool
	Needs       []coretool.CapabilityNeed
	Facts       []coretool.RoutingFact
	Constraints []coretool.RoutingConstraint
}

// DynamicInventoryProvider optionally projects lifecycle coverage alongside a
// provider inventory. The base MCPToolProvider/SkillToolProvider interfaces
// remain compatible, but semantic routing treats their legacy list-only form
// as incomplete: an empty list cannot safely prove that related tools are not
// still loading. Production bridges implement this interface after a trusted
// ready/quarantine pass; test/static providers may return Complete explicitly.
type DynamicInventoryProvider interface {
	DynamicCatalogLifecycle(context.Context, Principal) DynamicCatalogLifecycle
}

// DynamicMCPInventoryProvider returns the routable MCP inventory and its
// lifecycle watermark from one host-owned observation. Semantic routing uses
// this stronger boundary when available: reading a list and health state
// separately can combine tools from one instant with readiness from another,
// producing a plan that never existed in the provider runtime.
//
// The legacy MCPToolProvider + DynamicInventoryProvider split remains only as
// a compatibility path for integrations that have not yet implemented an
// atomic inventory snapshot. Such integrations stay conservatively covered by
// their existing lifecycle declaration.
type DynamicMCPInventoryProvider interface {
	DynamicMCPInventory(context.Context, Principal) ([]MCPToolEntry, DynamicCatalogLifecycle)
}

// DynamicSkillInventoryProvider is the Skill counterpart to
// DynamicMCPInventoryProvider. The entries and completeness bit must describe
// the same principal-scoped installed-skill observation.
type DynamicSkillInventoryProvider interface {
	DynamicSkillInventory(context.Context, Principal) ([]SkillToolEntry, DynamicCatalogLifecycle)
}

// DynamicCapabilityNeedResolver is the only request-to-capability extension
// point for Core Agent dynamic providers. Implementations are a control-plane
// concern and must not consult ToolNames, provider names/descriptions, or
// dynamically discovered schemas as routing authority.
type DynamicCapabilityNeedResolver interface {
	ResolveDynamicCapabilityNeeds(context.Context, DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error)
}

// IntentClassificationSource is deliberately smaller than the concrete UIC.
// It makes the semantic source explicit and keeps the resolver independent of
// legacy ToolAffinity/ToolNames APIs.
type IntentClassificationSource interface {
	Classify(intent.MessageContext) intent.ClassificationResult
}

// PrincipalIntentClassificationSource is the host-facing counterpart for
// multi-tenant agents. It receives the complete trusted principal and request
// context, allowing a host to select that principal's already-authorized
// semantic classifier configuration without ever consulting provider
// inventory or tool-affinity output.
type PrincipalIntentClassificationSource interface {
	ClassifyDynamicIntent(context.Context, Principal, string) (intent.ClassificationResult, error)
}

// IntentCapabilityNeedTemplate is a control-plane mapping from a governed
// intent meaning to a governed outcome. It is not a mapping to a tool,
// provider, Skill, MCP server, or provider description.
type IntentCapabilityNeedTemplate struct {
	Capability coretool.CapabilityID
	Qualifiers map[string]string
	Polarity   coretool.NeedPolarity
	Required   bool
	// MaxInvocations declares how many times the turn may exercise this
	// outcome. It exists for iterative meanings — reading several files while
	// editing one of them — where a single invocation cannot express the
	// intent. Silence means one, so a rule that does not mention repeats
	// plans exactly as it did before this field existed.
	//
	// The budget is spent as plan nodes, not as a counter: the resolver emits
	// one sibling need per permitted invocation, each of which is planned,
	// granted, and journaled on its own. Raising it therefore widens what
	// review can see in the plan, never what a caller can do at runtime.
	// Only the first sibling inherits Required; later siblings are an
	// exposure ceiling (RepeatSiblingRequired).
	MaxInvocations int
}

// IntentLabelCapabilityNeedResolver turns semantic UIC labels into typed
// needs. It is reusable for any dynamic capability family, but deliberately
// requires an owner-published mapping and an already-governed capability
// registry. Dynamic inventories cannot add mappings at runtime.
type IntentLabelCapabilityNeedResolver struct {
	Classifier        IntentClassificationSource
	Registry          *coretool.CapabilityRegistry
	Rules             map[intent.IntentLabel][]IntentCapabilityNeedTemplate
	MinimumConfidence float64
	SessionGoverned   *SessionGovernedTaskStore
	// AmbientRetrieval is the host switch for optional warehouse needs.
	// Even when on, applyAmbientRetrieval still consults
	// intent.WantsAmbientRetrieval(primary) so lookup/closed-effect turns
	// do not grow knowledge/memory tools. Tests that pin exact intent→need
	// maps leave this off.
	AmbientRetrieval bool
}

func (r *IntentLabelCapabilityNeedResolver) ResolveDynamicCapabilityNeeds(_ context.Context, request DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
	if r == nil || r.Classifier == nil || r.Registry == nil {
		return DynamicCapabilityNeedResolution{}, fmt.Errorf("intent capability need resolver is unavailable")
	}
	classification := r.Classifier.Classify(intent.MessageContext{Text: request.UserText, UserID: request.Principal.UserID})
	if resolution, ok := r.SessionGoverned.ReplayContinuation(request, r.Rules, r.Registry, classification); ok {
		return applyAmbientRetrieval(r.Registry, r.AmbientRetrieval, classification.Primary, resolution), nil
	}
	resolution, err := resolveIntentLabelCapabilityNeeds(r.Registry, r.Rules, r.MinimumConfidence, classification)
	if err != nil {
		return resolution, err
	}
	return applyAmbientRetrieval(r.Registry, r.AmbientRetrieval, classification.Primary, resolution), nil
}

// PrincipalIntentLabelCapabilityNeedResolver applies the same reviewed
// intent-label templates as IntentLabelCapabilityNeedResolver, but asks a
// host-owned, principal-aware classifier for the semantic interpretation.
// It is suitable for service hosts whose LLM/embedding credentials are scoped
// to individual tenants. ToolNames and dynamic provider metadata remain
// outside both the input and the result.
type PrincipalIntentLabelCapabilityNeedResolver struct {
	Classifier        PrincipalIntentClassificationSource
	Registry          *coretool.CapabilityRegistry
	Rules             map[intent.IntentLabel][]IntentCapabilityNeedTemplate
	MinimumConfidence float64
	SessionGoverned   *SessionGovernedTaskStore
	AmbientRetrieval  bool
}

func (r *PrincipalIntentLabelCapabilityNeedResolver) ResolveDynamicCapabilityNeeds(ctx context.Context, request DynamicCapabilityNeedRequest) (DynamicCapabilityNeedResolution, error) {
	if r == nil || r.Classifier == nil || r.Registry == nil {
		return DynamicCapabilityNeedResolution{}, fmt.Errorf("principal intent capability need resolver is unavailable")
	}
	classification, err := r.Classifier.ClassifyDynamicIntent(ctx, request.Principal, request.UserText)
	if err != nil {
		return DynamicCapabilityNeedResolution{}, err
	}
	if resolution, ok := r.SessionGoverned.ReplayContinuation(request, r.Rules, r.Registry, classification); ok {
		return applyAmbientRetrieval(r.Registry, r.AmbientRetrieval, classification.Primary, resolution), nil
	}
	resolution, err := resolveIntentLabelCapabilityNeeds(r.Registry, r.Rules, r.MinimumConfidence, classification)
	if err != nil {
		return resolution, err
	}
	return applyAmbientRetrieval(r.Registry, r.AmbientRetrieval, classification.Primary, resolution), nil
}

func resolveIntentLabelCapabilityNeeds(registry *coretool.CapabilityRegistry, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, minimumConfidence float64, classification intent.ClassificationResult) (DynamicCapabilityNeedResolution, error) {
	if registry == nil {
		return DynamicCapabilityNeedResolution{}, fmt.Errorf("intent capability registry is unavailable")
	}
	threshold := minimumConfidence
	if threshold <= 0 {
		threshold = 0.78
	}
	if classification.Degraded || classification.Confidence < threshold {
		return DynamicCapabilityNeedResolution{}, nil
	}
	labels := classification.Labels()
	seen := make(map[string]bool)
	needs := make([]coretool.CapabilityNeed, 0)
	managed := false
	unmapped := false
	for _, label := range labels {
		templates := rules[label]
		if len(templates) == 0 {
			// Generic classifier states are not capability requests. Skipping
			// them keeps a non_coding Q&A on the legacy tool surface, and keeps
			// a governed primary (search/live_data) from being discarded when a
			// generic secondary is also present.
			if label.IsNonCapabilityLabel() {
				continue
			}
			// Any other confident label without an owner-published mapping is a
			// migration coverage gap. Scan the rest of the labels first so a
			// coding-only turn is unmanaged, while search+document_delivery
			// still fail closed instead of running only the migrated subset.
			unmapped = true
			continue
		}
		managed = true
		for _, template := range templates {
			if _, exists := registry.Lookup(template.Capability); !exists {
				return DynamicCapabilityNeedResolution{}, fmt.Errorf("intent capability rule %q references unknown capability %q", label, template.Capability)
			}
			polarity := template.Polarity
			if polarity == "" {
				polarity = coretool.NeedRequire
			}
			key := string(template.Capability) + "\x00" + string(polarity) + "\x00" + dynamicNeedQualifierKey(template.Qualifiers)
			if seen[key] {
				continue
			}
			seen[key] = true
			baseID := "need:" + string(template.Capability) + ":" + coretool.SchemaDigest([]byte(key))[:12]
			// MaxInvocations is an exposure ceiling, not an obligation.
			// Required on the template means "this meaning must happen at
			// least once" — only the first sibling inherits it. Later
			// siblings stay optional so after-edges (lookup→generate,
			// lookup→render) unlock after the declared invocation rather
			// than waiting for unused refinements. An optional template
			// stays optional as a whole, so a missing provider still
			// omits the family together.
			for index := 0; index < coretool.RepeatSiblingBudget(template.MaxInvocations); index++ {
				needs = append(needs, coretool.CapabilityNeed{
					ID:         coretool.RepeatSiblingNeedID(baseID, index),
					Capability: template.Capability, Qualifiers: cloneDynamicNeedQualifiers(template.Qualifiers), Polarity: polarity,
					Required: coretool.RepeatSiblingRequired(template.Required, index), Confidence: classification.Confidence, EvidenceIDs: []string{"intent:" + string(label)},
				})
			}
		}
	}
	if unmapped {
		if managed {
			return DynamicCapabilityNeedResolution{Managed: true}, nil
		}
		return DynamicCapabilityNeedResolution{}, nil
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].ID < needs[j].ID })
	return DynamicCapabilityNeedResolution{Managed: managed, Needs: needs}, nil
}

func dynamicNeedQualifierKey(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strings.TrimSpace(key)+"="+strings.TrimSpace(values[key]))
	}
	return strings.Join(parts, "\x1f")
}

func cloneDynamicNeedQualifiers(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// DynamicSemanticRouting supplies the shared semantic routing primitives for
// the Core Agent Skill/MCP migration. Production hosts must provide an issuer
// backed by a durable InvocationGrantStore and a durable ExecutionStore; the
// memory stores are suitable only for tests and explicit single-process
// development.
//
// Registry and Resolver are independently governed: a provider contract says
// what an implementation can do, while the resolver says what user outcome is
// requested. Neither declaration can create the other at runtime.
type DynamicSemanticRouting struct {
	Registry       *coretool.CapabilityRegistry
	Resolver       DynamicCapabilityNeedResolver
	Issuer         *coretool.InvocationIssuer
	ExecutionStore coretool.PlanExecutionStore
	RouteState     coretool.RouteStateStore
	HostCalls      coretool.HostCallJournal
	// Coordinator is the optional unified persistence owner. Production
	// resources provide it; memory/test configurations retain the component
	// interfaces while exercising the same capability-plan semantics.
	Coordinator *coretool.SQLiteSemanticExecutionCoordinator
	GrantTTL    time.Duration
	// EffectCoordinator owns receipt-backed dispatch for dynamic selections
	// declared external_effect or sensitive. Nil is allowed at configuration
	// time so a read-only family remains usable, but such a selected effect
	// fails closed instead of being promoted to plan success.
	EffectCoordinator DynamicExternalEffectCoordinator
	// PolicyAdapter translates the surrounding workflow/mutation policy into
	// capability constraints before planning. It must not inspect opaque grant
	// names or provider display metadata. Nil is accepted only for hosts whose
	// request has no restrictive legacy workflow policy.
	PolicyAdapter DynamicCapabilityPolicyAdapter
	// SessionGoverned stores planner-granted needs for continuation replay.
	// Nil disables session replay; production Routing always installs a store.
	SessionGoverned *SessionGovernedTaskStore
}

// SettleDynamicSemanticExternalEffect is the trusted receipt-reconciliation
// entry point for a selection previously recorded awaiting_receipt. It has no
// adapter/grant/model input, cannot invoke a provider, and only advances the
// immutable selection execution fact after the coordinator's operation ledger
// reports a matching durable terminal receipt.
func (r DynamicSemanticRouting) SettleDynamicSemanticExternalEffect(scope coretool.InvocationScope, principal Principal, selectionID, operationID string, state DynamicEffectReceiptState, reasonCode, receipt string) error {
	// Settlement is a data-plane reconciliation path, not a new route
	// request. It intentionally does not require a live need resolver, grant
	// issuer, or catalog inventory: those mutable control-plane components
	// must not decide the meaning of an already-published operation.
	if r.ExecutionStore == nil || r.RouteState == nil || r.EffectCoordinator == nil {
		return fmt.Errorf("dynamic semantic receipt settlement unavailable")
	}
	selectionID, operationID = strings.TrimSpace(selectionID), strings.TrimSpace(operationID)
	if selectionID == "" || operationID == "" {
		return fmt.Errorf("dynamic semantic receipt settlement identity is required")
	}
	if strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.UserID) == "" || scope.PrincipalID != memoryOwnerIDForPrincipal(principal) {
		return fmt.Errorf("dynamic semantic receipt principal is invalid")
	}
	reconciler, ok := r.EffectCoordinator.(DynamicExternalEffectReconciler)
	if !ok || reconciler == nil {
		return fmt.Errorf("dynamic semantic receipt settlement unavailable")
	}
	plan, err := r.RouteState.PublishedPlan(scope)
	if err != nil {
		return fmt.Errorf("load dynamic semantic settlement plan: %w", err)
	}
	selection, ok := dynamicSemanticSelectionByID(plan, selectionID)
	if !ok || !dynamicSelectionRequiresReceipt(selection) {
		return fmt.Errorf("dynamic semantic receipt selection is invalid")
	}
	trustedReceipt, err := reconciler.SettleDynamicExternalEffect(DynamicExternalEffectSettlement{Scope: scope, Principal: principal, Selection: selection, OperationID: operationID, State: state, ReasonCode: reasonCode, Receipt: receipt})
	if err != nil {
		return err
	}
	if unified, ok := r.EffectCoordinator.(UnifiedSemanticEffectCoordinator); ok && unified.UsesSemanticExecutionCoordinator() {
		// SettleExternalEffectReceipt already committed receipt evidence,
		// operation state, PlanExecution and RouteState completion together.
		// Replaying the legacy two-store projection afterwards would not make
		// the write safer; it would turn an idempotent receipt into a false
		// settlement conflict.
		switch trustedReceipt.State {
		case DynamicEffectReceiptAccepted, DynamicEffectReceiptFailed, DynamicEffectReceiptUnknown:
			return nil
		default:
			return fmt.Errorf("dynamic semantic receipt is not terminal")
		}
	}
	var executionState coretool.PlanExecutionState
	switch trustedReceipt.State {
	case DynamicEffectReceiptAccepted:
		executionState = coretool.PlanExecutionSucceeded
	case DynamicEffectReceiptFailed:
		executionState = coretool.PlanExecutionFailed
	case DynamicEffectReceiptUnknown:
		executionState = coretool.PlanExecutionUnknown
	default:
		return fmt.Errorf("dynamic semantic receipt is not terminal")
	}
	record, err := r.ExecutionStore.SettleAwaitingReceipt(scope, selectionID, executionState, coretool.SchemaDigest([]byte(operationID)), trustedReceipt.ReasonCode, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("settle dynamic semantic execution: %w", err)
	}
	if record.State != executionState {
		return fmt.Errorf("dynamic semantic execution settlement conflict")
	}
	if executionState == coretool.PlanExecutionSucceeded {
		if _, err := r.RouteState.RecordSelectionCompletion(scope, scope.PlanID, selectionID, time.Now().UTC()); err != nil {
			return fmt.Errorf("record settled dynamic semantic completion: %w", err)
		}
	}
	return nil
}

// ReconcileDynamicEffectReceiptSource consumes trusted provider/channel
// observations for one immutable binding. The source is never given a
// dispatcher, grant, adapter name, or selection chosen by a caller. For every
// observation we first recover the admission-time scope/principal/selection
// mapping from the operation ledger, then load the published immutable plan
// and compare its selection digest before invoking the normal settlement
// path. This keeps restart recovery and live receipt polling on exactly the
// same validation boundary.
func (r DynamicSemanticRouting) ReconcileDynamicEffectReceiptSource(ctx context.Context, source DynamicEffectReceiptSource) error {
	if source == nil {
		return fmt.Errorf("dynamic effect receipt source is required")
	}
	if r.EffectCoordinator == nil {
		return fmt.Errorf("dynamic semantic receipt settlement unavailable")
	}
	resolver, ok := r.EffectCoordinator.(DynamicExternalEffectOperationResolver)
	if !ok || resolver == nil {
		return fmt.Errorf("dynamic semantic operation binding resolver unavailable")
	}
	bindingID := strings.TrimSpace(source.BindingID())
	if bindingID == "" {
		return fmt.Errorf("dynamic effect receipt source binding is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return source.ObserveDynamicEffectReceipts(ctx, func(observation DynamicEffectReceiptObservation) error {
		operationID := strings.TrimSpace(observation.OperationID)
		if operationID == "" {
			return fmt.Errorf("dynamic effect receipt observation operation id is required")
		}
		binding, err := resolver.DynamicSemanticOperationBinding(operationID)
		if err != nil {
			return err
		}
		plan, err := r.RouteState.PublishedPlan(binding.Scope)
		if err != nil {
			return fmt.Errorf("load dynamic semantic reconciliation plan: %w", err)
		}
		selection, ok := dynamicSemanticSelectionByID(plan, binding.SelectionID)
		if !ok || !dynamicSelectionRequiresReceipt(selection) || dynamicSemanticSelectionDigest(selection) != binding.SelectionDigest {
			return fmt.Errorf("dynamic effect operation selection binding mismatch")
		}
		if selection.Provider.StableID() != bindingID {
			return fmt.Errorf("dynamic effect receipt source binding mismatch")
		}
		return r.SettleDynamicSemanticExternalEffect(binding.Scope, binding.Principal, binding.SelectionID, operationID, observation.State, observation.ReasonCode, observation.Receipt)
	})
}

// DynamicSemanticManualResolution is an operator's out-of-band verdict on one
// operation that ended unknown. It carries no scope, selection, provider or
// plan: every one of those is recovered from the operation ledger, exactly as
// the receipt path does, so an operator cannot aim a verdict at an operation
// other than the one they name.
type DynamicSemanticManualResolution struct {
	OperationID string
	// Succeeded records that the effect demonstrably took place. False
	// records that it demonstrably did not. There is no third value: an
	// operator who does not know should not be resolving anything.
	Succeeded  bool
	Evidence   string
	ResolvedBy string
	ReasonCode string
}

// DynamicSemanticUnknownResolver is the coordinator capability behind the
// out-of-band exit. It is a separate interface from settlement so a host that
// has no way to authenticate an operator simply never satisfies it.
type DynamicSemanticUnknownResolver interface {
	ResolveUnknownExternalEffect(scope coretool.InvocationScope, selectionID, selectionDigest, bindingID string, resolution coretool.SemanticExternalEffectResolution, now time.Time) (coretool.SemanticExternalEffectOperation, error)
}

// ResolveUnknownDynamicSemanticExternalEffect is the exit for an operation
// that ended unknown. Everything before this point tries hard to avoid
// claiming an outcome it cannot observe, which is right, but it leaves
// operations parked in unknown with nothing that can ever move them. A person
// who has checked the channel is the only remaining source of truth, and this
// is the one door they get.
//
// It is deliberately not reachable from a model, an adapter, or a grant. It
// takes no dispatcher and can invoke no provider: it can only write down what
// somebody found.
func (r DynamicSemanticRouting) ResolveUnknownDynamicSemanticExternalEffect(resolution DynamicSemanticManualResolution) error {
	if r.RouteState == nil || r.EffectCoordinator == nil {
		return fmt.Errorf("dynamic semantic manual resolution unavailable")
	}
	resolver, ok := r.EffectCoordinator.(DynamicExternalEffectOperationResolver)
	if !ok || resolver == nil {
		return fmt.Errorf("dynamic semantic operation binding resolver unavailable")
	}
	settler, ok := r.EffectCoordinator.(DynamicSemanticUnknownResolver)
	if !ok || settler == nil {
		return fmt.Errorf("dynamic semantic manual resolution unavailable")
	}
	operationID := strings.TrimSpace(resolution.OperationID)
	if operationID == "" {
		return fmt.Errorf("dynamic semantic manual resolution operation id is required")
	}
	if strings.TrimSpace(resolution.Evidence) == "" {
		return fmt.Errorf("dynamic semantic manual resolution evidence is required")
	}
	if strings.TrimSpace(resolution.ResolvedBy) == "" {
		return fmt.Errorf("dynamic semantic manual resolution operator is required")
	}
	binding, err := resolver.DynamicSemanticOperationBinding(operationID)
	if err != nil {
		return err
	}
	plan, err := r.RouteState.PublishedPlan(binding.Scope)
	if err != nil {
		return fmt.Errorf("load dynamic semantic resolution plan: %w", err)
	}
	selection, ok := dynamicSemanticSelectionByID(plan, binding.SelectionID)
	if !ok || !dynamicSelectionRequiresReceipt(selection) || dynamicSemanticSelectionDigest(selection) != binding.SelectionDigest {
		return fmt.Errorf("dynamic semantic manual resolution selection binding mismatch")
	}
	outcome := coretool.SemanticExternalEffectFailed
	if resolution.Succeeded {
		outcome = coretool.SemanticExternalEffectSucceeded
	}
	_, err = settler.ResolveUnknownExternalEffect(binding.Scope, binding.SelectionID, binding.SelectionDigest, selection.Provider.StableID(), coretool.SemanticExternalEffectResolution{
		OperationKey: operationID, Outcome: outcome,
		Evidence: resolution.Evidence, ResolvedBy: resolution.ResolvedBy, ReasonCode: resolution.ReasonCode,
	}, time.Now().UTC())
	return err
}

// DynamicCapabilityPolicyAdapter is the migration bridge for policies that
// are still expressed in legacy tool-name terms. It receives no provider
// identity and can only narrow the semantic request through standard facts and
// constraints. A restrictive request without this adapter fails closed.
type DynamicCapabilityPolicyAdapter interface {
	DynamicCapabilityConstraints(DynamicCapabilityNeedRequest) ([]coretool.RoutingFact, []coretool.RoutingConstraint, error)
}

// CapabilityPolicyRule is a declarative capability-level policy rule. It is
// intentionally unable to reference an AdapterName, Skill name, MCP server or
// function token. Multiple matching rules compose monotonically: they can only
// add facts/constraints, never remove an earlier restriction.
type CapabilityPolicyRule struct {
	WorkflowPolicy string
	MutationScope  string
	Facts          []coretool.RoutingFact
	Constraints    []coretool.RoutingConstraint
}

// StaticCapabilityPolicyAdapter is the generic bridge for hosts whose
// workflow engine has not yet natively emitted RoutingConstraint. Product
// configuration publishes rules against capability IDs and trusted workflow
// states; it does not maintain a second tool-name allowlist.
type StaticCapabilityPolicyAdapter struct {
	Rules []CapabilityPolicyRule
}

func (a StaticCapabilityPolicyAdapter) DynamicCapabilityConstraints(request DynamicCapabilityNeedRequest) ([]coretool.RoutingFact, []coretool.RoutingConstraint, error) {
	facts := make([]coretool.RoutingFact, 0)
	constraints := make([]coretool.RoutingConstraint, 0)
	seenFacts := make(map[string]struct{})
	seenConstraints := make(map[string]struct{})
	for _, rule := range a.Rules {
		if policy := strings.TrimSpace(rule.WorkflowPolicy); policy != "" && !strings.EqualFold(policy, strings.TrimSpace(request.WorkflowPolicy)) {
			continue
		}
		if scope := strings.TrimSpace(rule.MutationScope); scope != "" && !strings.EqualFold(scope, strings.TrimSpace(request.MutationScope)) {
			continue
		}
		for _, fact := range rule.Facts {
			if strings.TrimSpace(fact.ID) == "" || strings.TrimSpace(fact.Kind) == "" {
				return nil, nil, fmt.Errorf("dynamic capability policy fact is invalid")
			}
			if fact.Authority != coretool.AuthorityPolicy && fact.Authority != coretool.AuthorityRuntime {
				return nil, nil, fmt.Errorf("dynamic capability policy fact %q has untrusted authority", fact.ID)
			}
			if _, exists := seenFacts[fact.ID]; exists {
				return nil, nil, fmt.Errorf("duplicate dynamic capability policy fact %q", fact.ID)
			}
			seenFacts[fact.ID] = struct{}{}
			facts = append(facts, cloneDynamicRoutingFact(fact))
		}
		for _, constraint := range rule.Constraints {
			if strings.TrimSpace(constraint.ID) == "" || strings.TrimSpace(string(constraint.Capability)) == "" || !dynamicCapabilityConstraintEffectAllowed(constraint.Effect) {
				return nil, nil, fmt.Errorf("dynamic capability policy constraint is invalid")
			}
			if constraint.Authority != coretool.AuthorityPolicy && constraint.Authority != coretool.AuthorityRuntime {
				return nil, nil, fmt.Errorf("dynamic capability policy constraint %q has untrusted authority", constraint.ID)
			}
			if _, exists := seenConstraints[constraint.ID]; exists {
				return nil, nil, fmt.Errorf("duplicate dynamic capability policy constraint %q", constraint.ID)
			}
			seenConstraints[constraint.ID] = struct{}{}
			constraints = append(constraints, cloneDynamicRoutingConstraint(constraint))
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	sort.Slice(constraints, func(i, j int) bool { return constraints[i].ID < constraints[j].ID })
	return facts, constraints, nil
}

func dynamicCapabilityConstraintEffectAllowed(effect string) bool {
	switch strings.TrimSpace(effect) {
	case "deny", "require_confirmation":
		return true
	default:
		return false
	}
}

func cloneDynamicRoutingFact(fact coretool.RoutingFact) coretool.RoutingFact {
	fact.Attributes = cloneDynamicNeedQualifiers(fact.Attributes)
	return fact
}

func cloneDynamicRoutingConstraint(constraint coretool.RoutingConstraint) coretool.RoutingConstraint {
	constraint.Attributes = cloneDynamicNeedQualifiers(constraint.Attributes)
	return constraint
}

func (r DynamicSemanticRouting) validate() error {
	if r.Registry == nil {
		return fmt.Errorf("dynamic semantic routing capability registry is required")
	}
	if r.Resolver == nil {
		return fmt.Errorf("dynamic semantic routing need resolver is required")
	}
	if r.Issuer == nil {
		return fmt.Errorf("dynamic semantic routing invocation issuer is required")
	}
	if r.ExecutionStore == nil {
		return fmt.Errorf("dynamic semantic routing execution store is required")
	}
	if r.RouteState == nil {
		return fmt.Errorf("dynamic semantic routing route-state store is required")
	}
	if r.HostCalls == nil {
		return fmt.Errorf("dynamic semantic routing host-call journal is required")
	}
	if r.GrantTTL <= 0 {
		return fmt.Errorf("dynamic semantic routing grant ttl must be positive")
	}
	return nil
}

type coreDynamicSemanticReplanInput struct {
	Needs         []coretool.CapabilityNeed
	Facts         []coretool.RoutingFact
	Constraints   []coretool.RoutingConstraint
	RootTaskID    string
	SessionID     string
	ChannelScope  string
	DestinationID string
	Attempts      uint8
}

type coreDynamicSemanticSurface struct {
	routing        DynamicSemanticRouting
	registry       *coretool.CapabilityRegistry
	catalog        DynamicSemanticCatalog
	plan           coretool.ToolPlan
	scope          coretool.InvocationScope
	executor       *coretool.PlanExecutor
	routeState     coretool.RouteStateStore
	hostCalls      coretool.HostCallJournal
	grants         map[string]coretool.InvocationGrant
	retiredGrants  map[string]coretool.InvocationGrant
	issued         map[string]bool
	completed      map[string]bool
	trustedFacts   map[string]bool
	refreshPending bool
	definitions    map[string]map[string]interface{}
	replan         *coreDynamicSemanticReplanInput
	tenantID       string
	hostServices   reviewedHostOwnedServices
}

func newCoreDynamicSemanticSurface(routing DynamicSemanticRouting, catalog DynamicSemanticCatalog, plan coretool.ToolPlan, scope coretool.InvocationScope, routingFacts ...[]coretool.RoutingFact) (*coreDynamicSemanticSurface, error) {
	return newCoreDynamicSemanticSurfaceForTenant(routing, catalog, plan, scope, "", routingFacts...)
}

func newCoreDynamicSemanticSurfaceForTenant(routing DynamicSemanticRouting, catalog DynamicSemanticCatalog, plan coretool.ToolPlan, scope coretool.InvocationScope, tenantID string, routingFacts ...[]coretool.RoutingFact) (*coreDynamicSemanticSurface, error) {
	var parent *coretool.RouteRevisionRef
	if current, currentErr := routing.RouteState.CurrentRevision(scope); currentErr == nil {
		parent = &current
	} else if currentErr.Error() != "route_revision_not_found" {
		return nil, fmt.Errorf("load dynamic semantic route revision: %w", currentErr)
	}
	return newCoreDynamicSemanticSurfaceForTenantWithParent(routing, catalog, plan, scope, tenantID, parent, routingFacts...)
}

// newCoreDynamicSemanticSurfaceForTenantWithParent is the single publisher of
// a model surface. Supplying ExpectedParent lets recovery retain the lineage
// snapshot it checked before replanning, so another request cannot silently
// become the parent while this replan is being built.
func newCoreDynamicSemanticSurfaceForTenantWithParent(routing DynamicSemanticRouting, catalog DynamicSemanticCatalog, plan coretool.ToolPlan, scope coretool.InvocationScope, tenantID string, expectedParent *coretool.RouteRevisionRef, routingFacts ...[]coretool.RoutingFact) (*coreDynamicSemanticSurface, error) {
	return newCoreDynamicSemanticSurfaceForTenantWithParentAndAmendment(routing, catalog, plan, scope, tenantID, expectedParent, nil, routingFacts...)
}

func newCoreDynamicSemanticSurfaceForTenantWithParentAndAmendment(routing DynamicSemanticRouting, catalog DynamicSemanticCatalog, plan coretool.ToolPlan, scope coretool.InvocationScope, tenantID string, expectedParent *coretool.RouteRevisionRef, amendment *coretool.RouteAmendmentRef, routingFacts ...[]coretool.RoutingFact) (*coreDynamicSemanticSurface, error) {
	tenantID = strings.TrimSpace(tenantID)
	executor, err := coretool.NewPlanExecutorWithRouteState(routing.Issuer, routing.ExecutionStore, routing.RouteState)
	if err != nil {
		return nil, err
	}
	var parent *coretool.RouteRevisionRef
	if expectedParent != nil {
		copy := *expectedParent
		parent = &copy
	}
	publishRequest := coretool.RouteRevisionPublishRequest{Scope: scope, Plan: plan, ExpectedParent: parent, SnapshotDigest: plan.SnapshotDigest, Amendment: amendment}
	var state coretool.RouteState
	var initialGrants []coretool.InvocationGrant
	if routing.Coordinator != nil {
		state, initialGrants, err = routing.Coordinator.PublishSurface(coretool.SurfacePublishRequest{Revision: publishRequest, TenantID: tenantID, Issuer: routing.Issuer, GrantTTL: routing.GrantTTL, Now: time.Now().UTC()})
	} else {
		state, err = routing.RouteState.PublishRevision(publishRequest, time.Now().UTC())
	}
	if err != nil {
		return nil, fmt.Errorf("open dynamic semantic route state: %w", err)
	}
	surface := &coreDynamicSemanticSurface{
		routing: routing, registry: routing.Registry, catalog: catalog, plan: plan, scope: scope, executor: executor,
		routeState: routing.RouteState, hostCalls: routing.HostCalls,
		grants: make(map[string]coretool.InvocationGrant), retiredGrants: make(map[string]coretool.InvocationGrant), issued: make(map[string]bool), completed: make(map[string]bool), trustedFacts: make(map[string]bool),
		definitions: make(map[string]map[string]interface{}),
		tenantID:    tenantID,
	}
	if len(routingFacts) > 0 {
		now := time.Now().UTC()
		surface.trustedFacts = plan.TrustedSatisfiedDependencies(routingFacts[0], now)
		for _, fact := range routingFacts[0] {
			if !strings.EqualFold(strings.TrimSpace(fact.Kind), "confirmation_granted") {
				continue
			}
			if _, err := routing.RouteState.RecordConfirmation(scope, plan.ID, fact, now); err != nil {
				return nil, fmt.Errorf("record dynamic semantic confirmation: %w", err)
			}
		}
	}
	confirmed, err := routing.RouteState.ConfirmedRequirements(scope, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("recover dynamic semantic confirmation: %w", err)
	}
	for requirement := range confirmed {
		surface.trustedFacts[requirement] = true
	}
	for _, materialization := range state.Materializations {
		surface.issued[materialization.Grant.SelectionID] = true
		name := coretool.RenderedSemanticFunctionName(materialization.Grant.AdapterName, materialization.Grant.Token)
		if materialization.State == coretool.RouteMaterializationExposed {
			// A terminal execution has consumed its one-time grant. Do not
			// re-render it after a restart: receipt reconciliation owns awaiting
			// effects, while failed/unknown selections require an explicit new
			// route revision under the original capability constraints.
			execution, executionErr := executor.Execution(scope, materialization.Grant.SelectionID)
			if executionErr == nil && dynamicSemanticExecutionConsumesModelGrant(execution.State) {
				surface.retiredGrants[name] = materialization.Grant
				if _, err := routing.RouteState.RetireMaterialization(scope, plan.ID, materialization.Grant.Token, time.Now().UTC()); err != nil {
					return nil, fmt.Errorf("retire terminal dynamic semantic materialization: %w", err)
				}
				continue
			}
			if executionErr != nil && !errors.Is(executionErr, coretool.ErrPlanExecutionNotFound) {
				return nil, fmt.Errorf("recover dynamic semantic execution state: %w", executionErr)
			}
			if existing, exists := surface.grants[name]; exists && existing.Token != materialization.Grant.Token {
				return nil, fmt.Errorf("function-name collision for grant %q", materialization.Grant.SelectionID)
			}
			surface.grants[name] = materialization.Grant
		} else {
			surface.retiredGrants[name] = materialization.Grant
		}
	}
	for _, grant := range initialGrants {
		name := coretool.RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
		surface.issued[grant.SelectionID] = true
		surface.grants[name] = grant
	}
	completed, err := executor.Completed(scope)
	if err != nil {
		return nil, fmt.Errorf("recover dynamic semantic completion: %w", err)
	}
	projected, err := routing.RouteState.CompletedSelections(scope)
	if err != nil {
		return nil, fmt.Errorf("recover projected dynamic semantic completion: %w", err)
	}
	for selectionID := range projected {
		completed[selectionID] = true
	}
	surface.completed = completed
	return surface, nil
}

// nextExposedSelections states this host's exposure closure in the shared
// vocabulary. The choice itself lives in coretool so the two hosts cannot
// diverge on it: a need declaring an invocation budget is planned as sibling
// selections, and a host that granted them all at once would hand the model
// its whole allowance in one round.
func (s *coreDynamicSemanticSurface) nextExposedSelections(ready []coretool.PlannedSelection, completed map[string]bool) map[string]bool {
	live := make(map[string]bool, len(s.grants))
	for _, grant := range s.grants {
		live[grant.SelectionID] = true
	}
	return coretool.NextRepeatSelections(coretool.RepeatExposure{
		Ready:     ready,
		Completed: completed,
		Granted:   s.issued,
		Live:      live,
		Unsettled: s.selectionIsUnsettled,
	})
}

// selectionIsUnsettled reads the durable execution record for a spent
// selection. A failed attempt is settled — it cost its budget and the family
// moves on — while awaiting-receipt, running, or lost outcomes are not, and a
// family must not step past them.
func (s *coreDynamicSemanticSurface) selectionIsUnsettled(selectionID string) bool {
	if s == nil || s.executor == nil {
		return false
	}
	record, err := s.executor.Execution(s.scope, selectionID)
	if err != nil {
		return false
	}
	switch record.State {
	case coretool.PlanExecutionAwaitingReceipt, coretool.PlanExecutionUnknown, coretool.PlanExecutionRunning:
		return true
	default:
		return false
	}
}

// Definitions materializes precisely the current DAG exposure closure. It
// does not re-plan, re-discover, or mint replacement grants for a selection
// that has already been exposed.
func (s *coreDynamicSemanticSurface) Definitions() ([]map[string]interface{}, error) {
	if s == nil {
		return nil, fmt.Errorf("dynamic semantic call surface is unavailable")
	}
	completed, err := s.executor.Completed(s.scope)
	if err != nil {
		return nil, fmt.Errorf("load dynamic semantic completion: %w", err)
	}
	projected, err := s.routeState.CompletedSelections(s.scope)
	if err != nil {
		return nil, fmt.Errorf("load projected dynamic semantic completion: %w", err)
	}
	for selectionID := range projected {
		completed[selectionID] = true
	}
	s.completed = completed
	ready := s.plan.ReadySelections(s.withTrustedFacts(completed))
	needed := s.nextExposedSelections(ready, completed)
	if len(needed) > 0 {
		partial := dynamicSemanticPlanSelections(s.plan, needed)
		var grants []coretool.InvocationGrant
		var err error
		if s.routing.Coordinator != nil {
			_, grants, err = s.routing.Coordinator.MaterializeReadySurface(s.scope, s.routing.Issuer, s.routing.GrantTTL, s.withTrustedFacts(completed), needed, time.Now().UTC())
		} else {
			grants, err = s.routing.Issuer.IssueReady(partial, s.scope, s.routing.GrantTTL, s.withTrustedFacts(completed))
		}
		if err != nil {
			return nil, fmt.Errorf("issue dynamic semantic grants: %w", err)
		}
		for _, grant := range grants {
			name := coretool.RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
			if name == "" {
				return nil, fmt.Errorf("dynamic semantic grant %q has no model function name", grant.SelectionID)
			}
			if existing, exists := s.grants[name]; exists && existing.Token != grant.Token {
				return nil, fmt.Errorf("function-name collision for grant %q", grant.SelectionID)
			}
			if s.routing.Coordinator == nil {
				if _, err := s.routeState.RecordMaterialization(s.scope, s.plan.ID, coretool.RouteMaterialization{
					FunctionName: grant.Token, Grant: grant, State: coretool.RouteMaterializationExposed,
				}, time.Now().UTC()); err != nil {
					return nil, fmt.Errorf("record dynamic semantic materialization: %w", err)
				}
			}
			s.grants[name] = grant
			s.issued[grant.SelectionID] = true
		}
	}

	visible := make(map[string]bool, len(ready))
	grants := make([]coretool.InvocationGrant, 0, len(ready))
	for _, selection := range ready {
		if completed[selection.ID] {
			continue
		}
		for _, grant := range s.grants {
			if grant.SelectionID == selection.ID {
				visible[selection.ID] = true
				grants = append(grants, grant)
				break
			}
		}
	}
	if len(visible) == 0 {
		return nil, nil
	}
	rendered, err := coretool.NewCatalogRenderer(s.registry).RenderReady(dynamicSemanticPlanSelections(s.plan, visible), grants, s.catalog.Definitions, s.withTrustedFacts(completed))
	if err != nil {
		return nil, fmt.Errorf("render dynamic semantic surface: %w", err)
	}
	out := make([]map[string]interface{}, 0, len(rendered))
	s.definitions = make(map[string]map[string]interface{}, len(rendered))
	for _, item := range rendered {
		s.definitions[item.FunctionName] = item.Definition
		out = append(out, item.Definition)
	}
	return out, nil
}

// Execute records the trusted host call before it consumes the grant. A model
// tool-call ID is therefore a recovery correlation key, never an instruction
// to run a dynamic provider again. Core Agent's legacy direct callback has no
// such identity and is rejected for a semantic selection rather than inventing
// one from the opaque function name.
func (s *coreDynamicSemanticSurface) Execute(ctx context.Context, principal Principal, mcpProvider MCPToolProvider, skillProvider SkillToolProvider, name, argsJSON, callID string) (coretool.SelectionExecutionResult, bool) {
	if s == nil {
		return coretool.SelectionExecutionResult{}, false
	}
	functionName := strings.TrimSpace(name)
	grant, ok := s.grants[functionName]
	if !ok {
		grant, ok = s.retiredGrants[functionName]
	}
	if !ok {
		return coretool.SelectionExecutionResult{}, false
	}
	if strings.TrimSpace(callID) == "" || s.hostCalls == nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_identity_required", ReasonCode: "host_call_identity_required"}, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	selection, ok := dynamicSemanticSelectionByID(s.plan, grant.SelectionID)
	if !ok {
		return coretool.SelectionExecutionResult{Result: "[system rejected] invocation_grant_selection_not_found", ReasonCode: "invocation_grant_selection_not_found"}, true
	}
	requestDigest := "invalid:" + coretool.SchemaDigest([]byte(argsJSON))
	canonical, canonicalErr := coretool.CanonicalizeAuthorizedInvocationArguments(argsJSON, s.catalog.schemas[selection.AdapterName], selection.ParameterAuthorization)
	if canonicalErr == nil {
		requestDigest = canonical.Digest
	}
	identity := coretool.HostCallIdentity{
		Protocol: "core-agent-loop/v1", ConnectionID: s.scope.SessionID + "\x00" + s.scope.TurnID, CallID: strings.TrimSpace(callID),
	}
	fingerprint := coretool.InvocationGrantFingerprint(grant)
	if s.routing.Coordinator != nil {
		return s.executeCoordinated(ctx, principal, mcpProvider, skillProvider, grant, selection, canonical, canonicalErr, identity, requestDigest)
	}
	record, action, err := s.hostCalls.Acquire(identity, fingerprint, requestDigest, time.Now().UTC())
	if err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
	}
	acquired := action
	action = coretool.ResolveHostCallAcquireAction(action, record, requestDigest)
	switch action {
	case coretool.HostCallAcquireReplay:
		if acquired == coretool.HostCallAcquireConflict {
			return dynamicSemanticRecordedResultFallback(record.Result), true
		}
		return s.dynamicSemanticReplayedResult(selection.ID, record.Result), true
	case coretool.HostCallAcquireConflict:
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_conflict", ReasonCode: "host_call_conflict"}, true
	case coretool.HostCallAcquireInProgress:
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_in_progress", ReasonCode: "host_call_in_progress"}, true
	case coretool.HostCallAcquireUnknown:
		// The journal recorded that this call may have reached its provider.
		// Reporting a definite failure here would invite a retry of an effect
		// that might already hold, so the uncertainty is carried outward.
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_unknown", Unknown: true, ReasonCode: "host_call_unknown"}, true
	case coretool.HostCallAcquireAdmit:
		// Continue below.
	default:
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_unavailable", ReasonCode: "host_call_unavailable"}, true
	}
	if _, err := s.hostCalls.MarkAdmitted(identity, fingerprint, requestDigest, time.Now().UTC()); err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
	}
	result, selected, err := s.executor.Execute(grant, s.scope, s.plan, s.trustedFacts, func(selected coretool.PlannedSelection) coretool.SelectionExecutionResult {
		return s.catalog.ExecuteSelectionWithEffects(ctx, s.scope, principal, mcpProvider, skillProvider, s.routing.EffectCoordinator, selected, argsJSON)
	})
	if err != nil {
		_, _ = s.hostCalls.MarkUnknown(identity, fingerprint, requestDigest, time.Now().UTC())
		_ = s.retireAfterAttempt(grant)
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
	}
	if result.Unknown {
		// An unknown outcome is not a completed one. Storing it as completed
		// would let a later replay re-derive a definite verdict from the
		// recorded text and invite a second attempt at an externally
		// observable effect that might already hold.
		if _, err := s.hostCalls.MarkUnknown(identity, fingerprint, requestDigest, time.Now().UTC()); err != nil {
			_ = s.retireAfterAttempt(grant)
			return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_record_failed", Unknown: true, ReasonCode: "host_call_record_failed"}, true
		}
	} else if _, err := s.hostCalls.Complete(identity, fingerprint, requestDigest, result.Result, time.Now().UTC()); err != nil {
		// The provider may have run; the journal is the recovery authority.
		// Preserve that uncertainty instead of presenting an uncorrelated result.
		_, _ = s.hostCalls.MarkUnknown(identity, fingerprint, requestDigest, time.Now().UTC())
		_ = s.retireAfterAttempt(grant)
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_record_failed", Unknown: true, ReasonCode: "host_call_record_failed"}, true
	}
	if result.Succeeded {
		s.completed[selected.ID] = true
	}
	// A grant is consumed before provider execution. Whether the provider
	// reports success, failure, or unknown, this particular model function
	// name must never be rendered again. A retry requires a new plan revision
	// and its own explicit recovery policy, not an accidental replay.
	if err := s.retireAfterAttempt(grant); err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] route_state_retire_failed", ReasonCode: "route_state_retire_failed"}, true
	}
	return result, true
}

func (s *coreDynamicSemanticSurface) executeCoordinated(ctx context.Context, principal Principal, mcpProvider MCPToolProvider, skillProvider SkillToolProvider, grant coretool.InvocationGrant, selection coretool.PlannedSelection, canonical coretool.CanonicalRequest, canonicalErr error, identity coretool.HostCallIdentity, requestDigest string) (coretool.SelectionExecutionResult, bool) {
	if s == nil || s.routing.Coordinator == nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] semantic_execution_coordinator_unavailable", ReasonCode: "semantic_execution_coordinator_unavailable"}, true
	}
	if canonicalErr != nil {
		result := "[system rejected] parameter_schema_invalid"
		if _, err := s.routing.Issuer.Validate(grant, s.scope, s.plan, s.withTrustedFacts(s.completed)); err != nil {
			return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
		}
		admission := coretool.SemanticExecutionAdmission{Identity: identity, Grant: grant, RequestDigest: requestDigest, Scope: s.scope, Selection: selection, Now: time.Now().UTC()}
		record, action, err := s.routing.Coordinator.Reject(admission, result, "parameter_schema_invalid")
		if err != nil {
			return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
		}
		acquired := action
		action = coretool.ResolveHostCallAcquireAction(action, record, requestDigest)
		switch action {
		case coretool.HostCallAcquireReplay:
			if acquired == coretool.HostCallAcquireConflict {
				return dynamicSemanticRecordedResultFallback(record.Result), true
			}
			return s.dynamicSemanticReplayedResult(selection.ID, record.Result), true
		case coretool.HostCallAcquireConflict:
			return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_conflict", ReasonCode: "host_call_conflict"}, true
		case coretool.HostCallAcquireInProgress:
			return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_in_progress", ReasonCode: "host_call_in_progress"}, true
		case coretool.HostCallAcquireUnknown:
			return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_unknown", Unknown: true, ReasonCode: "host_call_unknown"}, true
		}
		if err := s.retireAfterAttempt(grant); err != nil {
			return coretool.SelectionExecutionResult{Result: "[system rejected] route_state_retire_failed", ReasonCode: "route_state_retire_failed"}, true
		}
		return coretool.SelectionExecutionResult{Result: result, ReasonCode: "parameter_schema_invalid"}, true
	}
	if _, err := s.routing.Issuer.Validate(grant, s.scope, s.plan, s.withTrustedFacts(s.completed)); err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
	}
	admission := coretool.SemanticExecutionAdmission{Identity: identity, Grant: grant, RequestDigest: canonical.Digest, Scope: s.scope, Selection: selection, Now: time.Now().UTC()}
	record, action, err := s.routing.Coordinator.Admit(admission)
	if err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
	}
	acquired := action
	action = coretool.ResolveHostCallAcquireAction(action, record, admission.RequestDigest)
	switch action {
	case coretool.HostCallAcquireReplay:
		if acquired == coretool.HostCallAcquireConflict {
			return dynamicSemanticRecordedResultFallback(record.Result), true
		}
		return s.dynamicSemanticReplayedResult(selection.ID, record.Result), true
	case coretool.HostCallAcquireConflict:
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_conflict", ReasonCode: "host_call_conflict"}, true
	case coretool.HostCallAcquireInProgress:
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_in_progress", ReasonCode: "host_call_in_progress"}, true
	case coretool.HostCallAcquireUnknown:
		return coretool.SelectionExecutionResult{Result: "[system rejected] host_call_unknown", Unknown: true, ReasonCode: "host_call_unknown"}, true
	}
	executionContext := WithDynamicSemanticAdmission(ctx, admission)
	result := s.catalog.ExecuteSelectionWithEffects(executionContext, s.scope, principal, mcpProvider, skillProvider, s.routing.EffectCoordinator, selection, string(canonical.CanonicalJSON))
	if unified, ok := s.routing.EffectCoordinator.(UnifiedSemanticEffectCoordinator); ok && unified.UsesSemanticExecutionCoordinator() && dynamicSelectionRequiresReceipt(selection) && result.ReasonCode != "parameter_schema_invalid" {
		// The unified effect coordinator already atomically finalized the
		// operation, execution and host-call record. Do not issue a second
		// completion transition through the generic path.
		if result.Succeeded {
			s.completed[selection.ID] = true
		}
		if err := s.retireAfterAttempt(grant); err != nil {
			return coretool.SelectionExecutionResult{Result: "[system rejected] route_state_retire_failed", ReasonCode: "route_state_retire_failed"}, true
		}
		return result, true
	}
	state := coretool.PlanExecutionSucceeded
	if result.Unknown {
		state, result.Succeeded = coretool.PlanExecutionUnknown, false
	} else if result.AwaitingReceipt {
		state, result.Succeeded = coretool.PlanExecutionAwaitingReceipt, false
	} else if !result.Succeeded {
		state = coretool.PlanExecutionFailed
	}
	if _, err := s.routing.Coordinator.Complete(admission, state, result.Result, result.ReasonCode, time.Now().UTC()); err != nil {
		_ = s.retireAfterAttempt(grant)
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}, true
	}
	if result.Succeeded {
		s.completed[selection.ID] = true
	}
	if err := s.retireAfterAttempt(grant); err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] route_state_retire_failed", ReasonCode: "route_state_retire_failed"}, true
	}
	return result, true
}

// dynamicSemanticReplayedResult reconstructs the outcome of a host call that
// was already recorded.
//
// The stored text is not the authority. A result string only reliably
// separates a rejection from everything else, so deriving the verdict from it
// reports a pending operation and an unobserved one as definite successes —
// the same class of defect as §11.52, arriving one layer later. PlanExecution
// is the durable verdict, and both the coordinated and uncoordinated paths
// already maintain it correctly, so replay reads that instead of guessing.
//
// Reading it at replay time is also more accurate than remembering the first
// attempt: a trusted receipt may have settled an awaiting_receipt or unknown
// selection in the meantime, and the execution row carries that resolution
// while the frozen text never could.
func (s *coreDynamicSemanticSurface) dynamicSemanticReplayedResult(selectionID, result string) coretool.SelectionExecutionResult {
	if s != nil && s.routing.ExecutionStore != nil {
		if record, err := s.routing.ExecutionStore.Execution(s.scope, selectionID); err == nil {
			switch record.State {
			case coretool.PlanExecutionSucceeded:
				return coretool.SelectionExecutionResult{Result: result, Succeeded: true, ReasonCode: record.ReasonCode}
			case coretool.PlanExecutionFailed:
				return coretool.SelectionExecutionResult{Result: result, ReasonCode: record.ReasonCode}
			case coretool.PlanExecutionUnknown:
				return coretool.SelectionExecutionResult{Result: result, Unknown: true, ReasonCode: record.ReasonCode}
			case coretool.PlanExecutionAwaitingReceipt:
				return coretool.SelectionExecutionResult{Result: result, AwaitingReceipt: true, ReasonCode: record.ReasonCode}
			}
		}
	}
	return dynamicSemanticRecordedResultFallback(result)
}

// dynamicSemanticRecordedResultFallback is the last resort for a recorded host
// call whose durable verdict cannot be read: no execution store, a lost row,
// or a state that is not terminal. Text says far less than the execution row,
// so this only recognizes the shapes that are certainly not success — the two
// failure prefixes the executor uses, and the marker trusted adapters emit
// when they could not observe their own effect.
func dynamicSemanticRecordedResultFallback(result string) coretool.SelectionExecutionResult {
	trimmed := strings.TrimSpace(result)
	switch {
	case strings.HasPrefix(trimmed, "[system unknown]"):
		return coretool.SelectionExecutionResult{Result: result, Unknown: true}
	case strings.HasPrefix(trimmed, "[system rejected]"), strings.HasPrefix(trimmed, "error:"):
		return coretool.SelectionExecutionResult{Result: result}
	default:
		return coretool.SelectionExecutionResult{Result: result, Succeeded: true}
	}
}

func (s *coreDynamicSemanticSurface) retireGrant(grant coretool.InvocationGrant) error {
	if s == nil || s.routeState == nil {
		return fmt.Errorf("route_state_unavailable")
	}
	// A consumed invocation is never shown again by this host, even when the
	// durable retirement write reports an error. Re-exposing it would invite a
	// stale model retry; recovery remains fail-closed because a later process
	// will have to reconcile the durable state rather than dispatching again.
	name := coretool.RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
	_, err := s.routeState.RetireMaterialization(s.scope, s.plan.ID, grant.Token, time.Now().UTC())
	delete(s.grants, name)
	s.retiredGrants[name] = grant
	return err
}

// retireAfterAttempt requests a new host surface even when the provider or
// durable route-state operation fails. The invocation grant was already
// admitted and may have been consumed, so retaining its function definition
// in the current LLM turn would create a stale retry path.
func (s *coreDynamicSemanticSurface) retireAfterAttempt(grant coretool.InvocationGrant) error {
	if s != nil {
		s.refreshPending = true
	}
	return s.retireGrant(grant)
}

func dynamicSemanticExecutionConsumesModelGrant(state coretool.PlanExecutionState) bool {
	switch state {
	case coretool.PlanExecutionAwaitingReceipt, coretool.PlanExecutionFailed, coretool.PlanExecutionUnknown:
		return true
	default:
		return false
	}
}

func dynamicSemanticSelectionByID(plan coretool.ToolPlan, selectionID string) (coretool.PlannedSelection, bool) {
	for _, selection := range plan.Selections {
		if selection.ID == selectionID {
			return selection, true
		}
	}
	return coretool.PlannedSelection{}, false
}

func (s *coreDynamicSemanticSurface) HasGrant(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.grants[strings.TrimSpace(name)]
	return ok
}

func (s *coreDynamicSemanticSurface) HasKnownGrant(name string) bool {
	if s == nil {
		return false
	}
	if s.HasGrant(name) {
		return true
	}
	_, ok := s.retiredGrants[strings.TrimSpace(name)]
	return ok
}

func (s *coreDynamicSemanticSurface) ConsumeRefreshPending() bool {
	if s == nil || !s.refreshPending {
		return false
	}
	s.refreshPending = false
	return true
}

func (s *coreDynamicSemanticSurface) withTrustedFacts(completed map[string]bool) map[string]bool {
	merged := make(map[string]bool, len(completed)+len(s.trustedFacts))
	for id, satisfied := range completed {
		if satisfied {
			merged[id] = true
		}
	}
	for id, satisfied := range s.trustedFacts {
		if satisfied {
			merged[id] = true
		}
	}
	return merged
}

func closedManagedSemanticDefinitions(defs []map[string]interface{}) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(tooldef.Name(def))
		if name == "" || coretool.IsLegacyDynamicGatewayName(name) {
			continue
		}
		out = append(out, def)
	}
	return out
}

func dynamicSemanticPlanSelections(plan coretool.ToolPlan, allowed map[string]bool) coretool.ToolPlan {
	filtered := plan
	filtered.Selections = make([]coretool.PlannedSelection, 0, len(allowed))
	for _, selection := range plan.Selections {
		if allowed[selection.ID] {
			filtered.Selections = append(filtered.Selections, selection)
		}
	}
	return filtered
}

// ensureDynamicSemanticInitialized plans the request-scoped surface once.
// It does not issue grants: grant TTL and materialization stay on
// Definitions / BuildTools. The loop builds the prompt first, so this must
// set dynamicSemanticManaged on every fail-closed path; a return-value-only
// close is lost on the second call and would restore the legacy inventory.
func (c *coreAgentCallbacks) ensureDynamicSemanticInitialized() bool {
	if c == nil {
		return false
	}
	if c.dynamicSemanticInitialized {
		return c.dynamicSemanticManaged
	}
	if c.dynamicSemanticRouting == nil {
		return false
	}
	c.dynamicSemanticInitialized = true
	routing := *c.dynamicSemanticRouting
	request := DynamicCapabilityNeedRequest{
		Principal: c.principal, UserText: c.userText, RootTaskID: c.dynamicTaskRootID(),
		SessionID: c.loopID, TurnID: c.dynamicOperationScope, TaskRelation: c.taskRelation, ChannelScope: "core-agent",
		WorkflowPolicy: string(c.toolPolicy), MutationScope: string(c.mutationScope),
		DestinationID: strings.TrimSpace(c.trustedDestinationID),
	}
	resolution, err := routing.Resolver.ResolveDynamicCapabilityNeeds(c.ctx, request)
	if err != nil {
		// A configured semantic control plane failing is not permission to
		// restore the legacy bulk inventory surface.
		c.dynamicSemanticManaged = true
		return true
	}
	if !resolution.Managed {
		return false
	}
	c.dynamicSemanticManaged = true
	resolution, policyErr := applyDynamicSemanticPolicy(routing, request, resolution, string(c.toolPolicy), string(c.mutationScope))
	if policyErr {
		return true
	}
	if len(resolution.Needs) == 0 {
		return true
	}
	c.reviewedHostGenerate = reviewedHostGenerateNeedPresent(resolution.Needs)
	c.reviewedHostAudioRender = reviewedHostAudioRenderNeedPresent(resolution.Needs)
	c.reviewedHostVisualCapture = reviewedHostVisualCaptureNeedPresent(resolution.Needs)
	if reviewedHostDocumentNeedPresent(resolution.Needs) {
		ownerID := memoryOwnerIDForPrincipal(c.principal)
		documentInputs, docErr := reviewedHostDocumentInputsForTurn(request.RootTaskID, request.TurnID, ownerID, c.attachments)
		imageInputs, imgErr := reviewedHostImageInputsForTurn(request.RootTaskID, request.TurnID, ownerID, c.attachments)
		voiceInputs, voiceErr := reviewedHostVoiceInputsForTurn(request.RootTaskID, request.TurnID, ownerID, c.attachments)
		var boundDoc *reviewedHostDocumentInput
		var boundImg *reviewedHostImageInput
		var boundVoice *reviewedHostVoiceInput
		resolution.Needs, boundDoc, boundImg, boundVoice, err = bindReviewedHostDeliverableTurn(resolution.Needs, reviewedHostDeliverableTurnInputs{
			Documents: documentInputs, DocumentErr: docErr,
			Images: imageInputs, ImageErr: imgErr,
			Voices: voiceInputs, VoiceErr: voiceErr,
		})
		if err != nil {
			return true
		}
		c.reviewedHostDocument = boundDoc
		c.reviewedHostImage = boundImg
		c.reviewedHostVoice = boundVoice
		if boundDoc != nil {
			resolution.Facts = append(resolution.Facts, reviewedHostDocumentFacts([]reviewedHostDocumentInput{*boundDoc})...)
		}
	}
	if reviewedHostAudioNeedPresent(resolution.Needs) {
		audioInputs, audioErr := reviewedHostAudioInputsForTurn(request.RootTaskID, request.TurnID, memoryOwnerIDForPrincipal(c.principal), c.attachments)
		resolution.Needs, err = bindReviewedHostAudioTurn(resolution.Needs, audioInputs, audioErr)
		if err != nil {
			return true
		}
		if !reviewedHostSpeechReady(c.speechTranscriber) {
			return true
		}
		c.reviewedHostAudio = &audioInputs[0]
	}
	hostServices := c.reviewedHostOwnedServices()
	if c.reviewedHostDocument != nil {
		hostServices.DocumentRead = c
	}
	if c.reviewedHostAudio != nil {
		hostServices.AudioTranscribe = c
	}
	mcpEntries, skillEntries, lifecycle, err := observeDynamicSemanticInventory(c.ctx, c.principal, c.mcpProvider, c.skillProvider)
	if err != nil {
		mcpEntries, skillEntries, lifecycle = nil, nil, DynamicCatalogLifecycle{}
	}
	catalogProjection, lifecycle, err := prepareReviewedDynamicSemanticCatalog(routing.Registry, mcpEntries, skillEntries, lifecycle, hostServices)
	if err != nil {
		return true
	}
	catalog := coretool.NewToolCatalog(routing.Registry)
	snapshot, err := catalog.PublishWithCoverage(catalogProjection.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		return true
	}
	rootTaskID := c.dynamicTaskRootID()
	if rootTaskID == "" {
		return true
	}
	planTurnID := rootTaskID
	if c.taskRelation.Kind == TaskRelationRefine {
		// A refine is an immutable child decision even if its resulting needs
		// happen to match the parent. Reusing the parent's turn/plan key would
		// make an amendment collide with the old route instead of publishing a
		// fenced replacement surface.
		planTurnID = "refine:" + coretool.SchemaDigest([]byte(c.dynamicOperationScope + "\x00" + c.taskRelation.AmendmentCommandID))[:24]
	}
	plan, err := coretool.NewToolPlanner(routing.Registry).Plan(coretool.RouteRequest{
		RootTaskID: rootTaskID, SessionID: c.loopID, TurnID: planTurnID, ChannelScope: "core-agent",
		Snapshot: snapshot, Needs: resolution.Needs, Facts: resolution.Facts, Constraints: resolution.Constraints,
	})
	if err != nil || len(plan.Unmet) > 0 {
		return true
	}
	coretool.LogExplainTrace(plan.Trace)
	if routing.SessionGoverned != nil {
		routing.SessionGoverned.PersistGrantedPlan(request, routing.Registry, plan)
	}
	var expectedParent *coretool.RouteRevisionRef
	var amendment *coretool.RouteAmendmentRef
	if c.taskRelation.Kind == TaskRelationRefine {
		if routing.Coordinator == nil || !c.taskRelation.permitsContinuation(request) {
			return true
		}
		parent, parentErr := routing.RouteState.CurrentRevision(coretool.InvocationScope{
			RootTaskID: rootTaskID, SessionID: c.loopID, PrincipalID: memoryOwnerIDForPrincipal(c.principal),
		})
		if parentErr != nil || parent.Revision != c.taskRelation.AmendmentRevision || c.taskRelation.AmendmentFencing == 0 {
			return true
		}
		expectedParent = &parent
		amendment = &coretool.RouteAmendmentRef{
			CommandID: c.taskRelation.AmendmentCommandID, Digest: c.taskRelation.AmendmentDigest,
			ParentRevision: c.taskRelation.AmendmentRevision, ParentFencingToken: c.taskRelation.AmendmentFencing,
		}
	}
	surface, err := newCoreDynamicSemanticSurfaceForTenantWithParentAndAmendment(routing, catalogProjection, plan, coretool.InvocationScope{
		RootTaskID: rootTaskID, PlanID: plan.ID, SessionID: c.loopID, TurnID: planTurnID,
		PrincipalID: memoryOwnerIDForPrincipal(c.principal),
	}, c.principal.TenantID, expectedParent, amendment, resolution.Facts)
	if err != nil {
		return true
	}
	surface.hostServices = c.reviewedHostOwnedServices()
	surface.replan = &coreDynamicSemanticReplanInput{
		Needs: cloneDynamicCapabilityNeeds(resolution.Needs), Facts: cloneDynamicRoutingFacts(resolution.Facts),
		Constraints: cloneDynamicRoutingConstraints(resolution.Constraints), RootTaskID: rootTaskID,
		SessionID: c.loopID, ChannelScope: "core-agent", DestinationID: strings.TrimSpace(c.trustedDestinationID),
	}
	c.dynamicSemanticSurface = surface
	return true
}

// dynamicSemanticToolDefinitions initializes the request-scoped dynamic
// surface once, then advances only its existing plan. A resolver's Managed bit
// is intentionally evaluated before reading a dynamic inventory: whether a
// user asks for an outcome cannot depend on which Skill/MCP happens to be
// installed or on its untrusted metadata.
func (c *coreAgentCallbacks) dynamicSemanticToolDefinitions() ([]map[string]interface{}, bool) {
	if !c.ensureDynamicSemanticInitialized() {
		return nil, false
	}
	if c.dynamicSemanticSurface == nil {
		return nil, true
	}
	defs, err := c.dynamicSemanticSurface.Definitions()
	if err != nil {
		return nil, true
	}
	return defs, true
}

func applyDynamicSemanticPolicy(routing DynamicSemanticRouting, request DynamicCapabilityNeedRequest, resolution DynamicCapabilityNeedResolution, toolPolicy, mutationScope string) (DynamicCapabilityNeedResolution, bool) {
	if routing.PolicyAdapter != nil {
		facts, constraints, err := routing.PolicyAdapter.DynamicCapabilityConstraints(request)
		if err != nil {
			return resolution, true
		}
		resolution.Facts = append(resolution.Facts, facts...)
		resolution.Constraints = append(resolution.Constraints, constraints...)
		return resolution, false
	}
	if (toolPolicy != "" && toolPolicy != "none" && toolPolicy != "full") || (mutationScope != "" && mutationScope != "unknown" && mutationScope != "project") {
		return resolution, true
	}
	return resolution, false
}

func observeDynamicSemanticInventory(ctx context.Context, principal Principal, mcp MCPToolProvider, skill SkillToolProvider) ([]MCPToolEntry, []SkillToolEntry, DynamicCatalogLifecycle, error) {
	if mcp == nil && skill == nil {
		return nil, nil, DynamicCatalogLifecycle{}, fmt.Errorf("dynamic semantic inventory unavailable")
	}
	lifecycles := make([]DynamicCatalogLifecycle, 0, 2)
	var mcpEntries []MCPToolEntry
	if mcp != nil {
		if provider, ok := mcp.(DynamicMCPInventoryProvider); ok {
			var lifecycle DynamicCatalogLifecycle
			mcpEntries, lifecycle = provider.DynamicMCPInventory(ctx, principal)
			lifecycles = append(lifecycles, dynamicCatalogLifecycleForKind("mcp", lifecycle))
		} else {
			mcpEntries = mcp.ListAvailableTools(ctx, principal)
			if provider, ok := mcp.(DynamicInventoryProvider); ok {
				lifecycles = append(lifecycles, dynamicCatalogLifecycleForKind("mcp", provider.DynamicCatalogLifecycle(ctx, principal)))
			} else {
				lifecycles = append(lifecycles, dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonIncomplete)))
			}
		}
	}
	var skillEntries []SkillToolEntry
	if skill != nil {
		if provider, ok := skill.(DynamicSkillInventoryProvider); ok {
			var lifecycle DynamicCatalogLifecycle
			skillEntries, lifecycle = provider.DynamicSkillInventory(ctx, principal)
			lifecycles = append(lifecycles, dynamicCatalogLifecycleForKind("skill", lifecycle))
		} else {
			skillEntries = skill.ListSkills(ctx, principal)
			if provider, ok := skill.(DynamicInventoryProvider); ok {
				lifecycles = append(lifecycles, dynamicCatalogLifecycleForKind("skill", provider.DynamicCatalogLifecycle(ctx, principal)))
			} else {
				lifecycles = append(lifecycles, dynamicCatalogLifecycleForKind("skill", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonIncomplete)))
			}
		}
	}
	return mcpEntries, skillEntries, mergeDynamicCatalogLifecycles(lifecycles), nil
}

func cloneDynamicCapabilityNeeds(needs []coretool.CapabilityNeed) []coretool.CapabilityNeed {
	if len(needs) == 0 {
		return nil
	}
	out := make([]coretool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		cloned := need
		cloned.Qualifiers = cloneDynamicNeedQualifiers(need.Qualifiers)
		out = append(out, cloned)
	}
	return out
}

func cloneDynamicRoutingFacts(facts []coretool.RoutingFact) []coretool.RoutingFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]coretool.RoutingFact, 0, len(facts))
	for _, fact := range facts {
		out = append(out, cloneDynamicRoutingFact(fact))
	}
	return out
}

func cloneDynamicRoutingConstraints(constraints []coretool.RoutingConstraint) []coretool.RoutingConstraint {
	if len(constraints) == 0 {
		return nil
	}
	out := make([]coretool.RoutingConstraint, 0, len(constraints))
	for _, constraint := range constraints {
		out = append(out, cloneDynamicRoutingConstraint(constraint))
	}
	return out
}

func (s *coreDynamicSemanticSurface) ReplanAfterBindingFailure(ctx context.Context, principal Principal, mcp MCPToolProvider, skill SkillToolProvider, reasonCode string) (*coreDynamicSemanticSurface, error) {
	if s == nil || s.replan == nil || s.routeState == nil || s.routing.Registry == nil {
		return nil, fmt.Errorf("dynamic semantic replan state is unavailable")
	}
	if !coretool.ReplanFailureEligible(reasonCode) {
		return nil, fmt.Errorf("semantic replan reason is not eligible")
	}
	if s.replan.Attempts >= 1 {
		return nil, fmt.Errorf("semantic replan attempt exhausted")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	// Capture the lineage before fetching mutable inventory and replanning. The
	// final PublishSurface uses this exact ref as ExpectedParent, so any route
	// that advances while a provider refresh is in flight fences this recovery
	// rather than letting it attach to a newer task revision it did not inspect.
	parent, err := s.routeState.CurrentRevision(s.scope)
	if err != nil {
		return nil, fmt.Errorf("load dynamic semantic replan parent: %w", err)
	}
	mcpEntries, skillEntries, lifecycle, err := observeDynamicSemanticInventory(ctx, principal, mcp, skill)
	if err != nil {
		return nil, err
	}
	catalogProjection, lifecycle, err := prepareReviewedDynamicSemanticCatalog(s.routing.Registry, mcpEntries, skillEntries, lifecycle, s.hostServices)
	if err != nil {
		return nil, fmt.Errorf("refresh dynamic semantic catalog: %w", err)
	}
	snapshot, err := coretool.NewToolCatalog(s.routing.Registry).PublishWithCoverage(catalogProjection.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("publish dynamic semantic replan catalog: %w", err)
	}
	turnID := "replan:" + coretool.SchemaDigest([]byte(strings.TrimSpace(s.scope.TurnID) + fmt.Sprintf(":%d", s.replan.Attempts+1)))[:24]
	plan, err := coretool.NewToolPlanner(s.routing.Registry).Plan(coretool.RouteRequest{
		RootTaskID: s.replan.RootTaskID, SessionID: s.replan.SessionID, TurnID: turnID, ChannelScope: s.replan.ChannelScope,
		Snapshot: snapshot, Needs: s.replan.Needs, Facts: s.replan.Facts, Constraints: s.replan.Constraints,
	})
	if err != nil || len(plan.Unmet) > 0 {
		return nil, fmt.Errorf("semantic replan no governed candidate")
	}
	if err := coretool.ValidateReplanSubset(s.plan, plan); err != nil && !coretool.ReplanIsBindingOnlyReplacement(s.plan, plan) {
		return nil, err
	}
	return s.replanAfterPrepared(plan, catalogProjection, parent, reasonCode)
}

// replanAfterPrepared is the small publish seam kept separate from inventory
// observation so tests can advance the lineage between planning and publish.
// Production ReplanAfterBindingFailure calls it only after a complete catalog
// and governed replacement plan have been constructed.
func (s *coreDynamicSemanticSurface) replanAfterPrepared(plan coretool.ToolPlan, catalog DynamicSemanticCatalog, parent coretool.RouteRevisionRef, reasonCode string) (*coreDynamicSemanticSurface, error) {
	if s == nil || s.replan == nil || s.routeState == nil {
		return nil, fmt.Errorf("dynamic semantic replan state is unavailable")
	}
	childScope := s.scope
	childScope.PlanID = plan.ID
	childScope.TurnID = "replan:" + coretool.SchemaDigest([]byte(strings.TrimSpace(s.scope.TurnID) + fmt.Sprintf(":%d", s.replan.Attempts+1)))[:24]
	plan.Trace.Events = append(plan.Trace.Events, coretool.TraceEvent{
		Stage: coretool.TraceStageRecovery, Subject: "replan", Event: "child_published", ReasonCode: strings.TrimSpace(reasonCode),
	})
	child, err := newCoreDynamicSemanticSurfaceForTenantWithParent(s.routing, catalog, plan, childScope, s.tenantID, &parent, s.replan.Facts)
	if err != nil {
		return nil, err
	}
	input := *s.replan
	input.Attempts++
	child.replan = &input
	child.hostServices = s.hostServices
	child.refreshPending = true
	return child, nil
}

func mergeDynamicCatalogLifecycles(values []DynamicCatalogLifecycle) DynamicCatalogLifecycle {
	if len(values) == 0 {
		return IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonIncomplete)
	}
	// Preserve every participating family. Collapsing a complete Skill family
	// with a loading MCP family into one incomplete bit used to hide ready Skill
	// candidates. Conversely, a missing candidate remains conservative because
	// the common coverage reports the incomplete family to the planner.
	families := make([]coretool.CatalogCoverageFamily, 0, len(values))
	seenKinds := make(map[string]bool, len(values))
	merged := coretool.CatalogCoverage{State: coretool.CatalogCoverageComplete}
	for _, candidate := range values {
		kind := strings.ToLower(strings.TrimSpace(candidate.Kind))
		if kind == "" {
			return IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonIncomplete)
		}
		if seenKinds[kind] {
			return IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonIncomplete)
		}
		seenKinds[kind] = true
		coverage := candidate.Coverage
		families = append(families, coretool.CatalogCoverageFamily{
			Kind: kind, State: coverage.State, ReasonCode: coverage.ReasonCode,
			ObservedAt: coverage.ObservedAt, StaleUntil: coverage.StaleUntil,
		})
		if coverage.State == coretool.CatalogCoverageIncomplete {
			merged.State = coretool.CatalogCoverageIncomplete
			merged.ReasonCode = strongerDynamicCoverageReason(merged.ReasonCode, coverage.ReasonCode)
			merged.StaleUntil = time.Time{}
		} else if coverage.State == coretool.CatalogCoverageStale && merged.State == coretool.CatalogCoverageComplete {
			merged.State = coretool.CatalogCoverageStale
			merged.ReasonCode = coretool.CatalogCoverageReasonStale
			merged.StaleUntil = coverage.StaleUntil
		}
	}
	sort.Slice(families, func(i, j int) bool { return families[i].Kind < families[j].Kind })
	merged.Families = families
	return DynamicCatalogLifecycle{Coverage: merged}
}

func firstNonEmptyDynamicReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return coretool.CatalogCoverageReasonIncomplete
}

func strongerDynamicCoverageReason(current, candidate string) string {
	if strings.TrimSpace(current) == coretool.CatalogCoverageReasonNotReady || strings.TrimSpace(candidate) == coretool.CatalogCoverageReasonNotReady {
		return coretool.CatalogCoverageReasonNotReady
	}
	return firstNonEmptyDynamicReason(current, candidate, coretool.CatalogCoverageReasonIncomplete)
}

// RefreshAfterToolExecution is the core-loop hook that advances a semantic
// ToolPlan DAG after a consumed grant. The next LLM round receives only newly
// ready selections; old opaque names are never re-materialized.
func (c *coreAgentCallbacks) RefreshAfterToolExecution(_ string) bool {
	return c != nil && c.dynamicSemanticSurface != nil && c.dynamicSemanticSurface.ConsumeRefreshPending()
}
