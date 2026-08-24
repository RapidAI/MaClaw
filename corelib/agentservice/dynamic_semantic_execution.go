package agentservice

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// DynamicSemanticCatalog is a host-side projection of a verified dynamic
// inventory. It deliberately contains no LLM-visible function names: callers
// publish Providers through ToolCatalog, plan them, then use Definitions only
// through CatalogRenderer with signed InvocationGrants.
//
// The catalog also owns the immutable runtime binding selected during
// projection. InvokeSelection revalidates that binding through the appropriate
// bridge immediately before transport, closing the discovery-to-execution
// time-of-check/time-of-use gap without reintroducing a provider-name gateway.
type DynamicSemanticCatalog struct {
	Providers   []coretool.ProviderSpec
	Definitions map[string]map[string]interface{}
	schemas     map[string]map[string]interface{}
	bindings    map[string]dynamicSemanticRuntimeBinding
}

// CanonicalizeSelectionArguments is the pre-admission half of dynamic
// execution. A coordinator must know the stable request digest before it can
// acquire the host-call journal, but its callers must not inspect the
// catalog's private schemas or bindings to obtain it. The selected binding is
// checked here as well, so a stale catalog becomes a deterministic rejection
// rather than an opportunity to look up a similarly named provider.
func (c *DynamicSemanticCatalog) CanonicalizeSelectionArguments(selection coretool.PlannedSelection, argsJSON string) (coretool.CanonicalRequest, error) {
	if c == nil {
		return coretool.CanonicalRequest{}, fmt.Errorf("dynamic_semantic_catalog_unavailable")
	}
	adapter := strings.TrimSpace(selection.AdapterName)
	binding, ok := c.bindings[adapter]
	if !ok || !dynamicSemanticProviderMatches(selection.Provider, binding.provider) {
		return coretool.CanonicalRequest{}, fmt.Errorf("dynamic_binding_stale")
	}
	schema, ok := c.schemas[adapter]
	if !ok {
		return coretool.CanonicalRequest{}, fmt.Errorf("dynamic_binding_stale")
	}
	return coretool.CanonicalizeAuthorizedInvocationArguments(argsJSON, schema, selection.ParameterAuthorization)
}

// DynamicCatalogLifecycle is the bounded lifecycle result associated with a
// dynamic inventory read. It does not name a provider or make one routable;
// it only distinguishes a complete ready/quarantined inventory from a refresh
// that has not yet established coverage for this request scope.
type DynamicCatalogLifecycle struct {
	// Kind is the provider implementation class whose inventory was observed.
	// It is set by the host boundary, rather than trusted from a provider's
	// discovery metadata.
	Kind     string
	Coverage coretool.CatalogCoverage
}

func CompleteDynamicCatalogLifecycle() DynamicCatalogLifecycle {
	return DynamicCatalogLifecycle{Coverage: coretool.CatalogCoverage{State: coretool.CatalogCoverageComplete}}
}

// IncompleteDynamicCatalogLifecycle requires a bounded reason such as
// catalog_incomplete or provider_not_ready. It is deliberately unsuitable for
// copying raw server errors or discovery metadata into a model tool surface.
func IncompleteDynamicCatalogLifecycle(reasonCode string) DynamicCatalogLifecycle {
	return DynamicCatalogLifecycle{Coverage: coretool.CatalogCoverage{State: coretool.CatalogCoverageIncomplete, ReasonCode: strings.TrimSpace(reasonCode)}}
}

// StaleDynamicCatalogLifecycle declares a bounded stale-while-revalidate
// interval. The planner will use it only for read-only selections.
func StaleDynamicCatalogLifecycle(until time.Time) DynamicCatalogLifecycle {
	return DynamicCatalogLifecycle{Coverage: coretool.CatalogCoverage{State: coretool.CatalogCoverageStale, ReasonCode: coretool.CatalogCoverageReasonStale, StaleUntil: until.UTC()}}
}

func dynamicCatalogLifecycleForKind(kind string, lifecycle DynamicCatalogLifecycle) DynamicCatalogLifecycle {
	lifecycle.Kind = strings.ToLower(strings.TrimSpace(kind))
	return lifecycle
}

type dynamicSemanticRuntimeBinding struct {
	provider coretool.ProviderBinding
	mcp      *MCPToolBinding
	skill    *SkillBinding
	host     *hostOwnedRuntimeBinding
}

// DynamicEffectReceiptState is the trusted outcome observed by an external
// operation coordinator. It is intentionally independent from MCP, Skill or
// any provider-specific transport status.
type DynamicEffectReceiptState string

const (
	DynamicEffectReceiptAccepted DynamicEffectReceiptState = "accepted"
	DynamicEffectReceiptAwaiting DynamicEffectReceiptState = "awaiting_receipt"
	DynamicEffectReceiptFailed   DynamicEffectReceiptState = "failed"
	DynamicEffectReceiptUnknown  DynamicEffectReceiptState = "unknown"
)

// DynamicEffectReceipt is the bounded evidence returned by a trusted
// coordinator. OperationID is coordinator-owned and must be durable: model
// tokens, provider result text and a function name cannot stand in for it.
type DynamicEffectReceipt struct {
	OperationID string
	State       DynamicEffectReceiptState
	ReasonCode  string
	// Reconciled authorizes a trusted coordinator to report an accepted
	// operation it had previously dispatched and durably reconciled, without
	// calling this process's dispatch closure again. It is never model input.
	Reconciled bool
}

// DynamicExternalEffectInvocation contains the immutable semantic identity of
// one dispatch. It deliberately has no discovery description, tool name, or
// model-provided provider selector.
type DynamicExternalEffectInvocation struct {
	Scope     coretool.InvocationScope
	Principal Principal
	Selection coretool.PlannedSelection
	Arguments map[string]interface{}
}

// DynamicExternalEffectCoordinator is the common receipt boundary for every
// dynamic selection whose declared effect includes external_effect or
// sensitive. The coordinator must durably create/reconcile OperationID and
// invoke dispatch at most once, synchronously before it returns. Returning
// accepted requires a trusted receipt; returning awaiting_receipt preserves a
// locally prepared operation without satisfying the plan DAG. A coordinator
// must never retain or invoke the callback after it returns: that would cross
// the request-owned terminal fence without an active caller to classify the
// outcome.
//
// The callback is the only way this interface can dispatch the already-bound
// provider. It therefore cannot select another Skill/MCP implementation from
// a provider name or model argument.
type DynamicExternalEffectCoordinator interface {
	CoordinateDynamicExternalEffect(context.Context, DynamicExternalEffectInvocation, func() (string, error)) (DynamicEffectReceipt, error)
}

// UnifiedSemanticEffectCoordinator marks an effect coordinator which commits
// the external operation, plan execution and host-call result through the
// shared semantic execution database. Hosts use this only to avoid issuing a
// second generic completion after the coordinator has already committed it.
// It does not expose settlement or provider dispatch authority.
type UnifiedSemanticEffectCoordinator interface {
	UsesSemanticExecutionCoordinator() bool
}

// DynamicSemanticAdmission is the already-durable host-call admission for a
// selected dynamic provider. It is carried only in process-local execution
// context between the semantic surface and its trusted effect coordinator;
// it is never derived from model input or serialized to a provider.
func WithDynamicSemanticAdmission(ctx context.Context, admission coretool.SemanticExecutionAdmission) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, dynamicSemanticAdmissionContextKey{}, admission)
}

func DynamicSemanticAdmissionFromContext(ctx context.Context) (coretool.SemanticExecutionAdmission, bool) {
	if ctx == nil {
		return coretool.SemanticExecutionAdmission{}, false
	}
	admission, ok := ctx.Value(dynamicSemanticAdmissionContextKey{}).(coretool.SemanticExecutionAdmission)
	return admission, ok
}

type dynamicSemanticAdmissionContextKey struct{}

// DynamicExternalEffectReconciler is optional at configuration time. A
// receipt-bound selection can always be prepared safely, but only a
// coordinator that implements this interface may later settle it. Settlement
// receives the immutable scope and selected binding so an arbitrary operation
// ID cannot be attached to a different capability or principal.
type DynamicExternalEffectReconciler interface {
	SettleDynamicExternalEffect(DynamicExternalEffectSettlement) (DynamicEffectReceipt, error)
}

type DynamicExternalEffectSettlement struct {
	Scope       coretool.InvocationScope
	Principal   Principal
	Selection   coretool.PlannedSelection
	OperationID string
	State       DynamicEffectReceiptState
	ReasonCode  string
	// Receipt is supplied only by a trusted provider/channel reconciliation
	// integration. It is persisted as a digest and never model-visible.
	Receipt string
}

// DynamicEffectReceiptObservation is produced only by a host-configured,
// binding-specific provider/channel receipt source. It contains no adapter
// name, grant, model call ID, or dispatch function: an observation can settle
// existing work but can never create permission to invoke a provider.
type DynamicEffectReceiptObservation struct {
	OperationID string
	State       DynamicEffectReceiptState
	ReasonCode  string
	Receipt     string
}

// DynamicEffectReceiptSource is a host-owned reconciliation integration for
// exactly one immutable provider binding. Implementations may observe a
// provider's receipt endpoint or a local channel outbox, but must never use
// this interface to dispatch/retry an effect. The callback is intentionally
// supplied by the routing host so every observation is re-bound to the
// durable operation before it can change plan state.
type DynamicEffectReceiptSource interface {
	BindingID() string
	ObserveDynamicEffectReceipts(context.Context, func(DynamicEffectReceiptObservation) error) error
}

// DynamicExternalEffectOperationResolver exposes only the immutable
// operation-to-plan mapping required by reconciliation. It is implemented by
// the durable coordinator, never by an Agent/model-facing execution surface.
type DynamicExternalEffectOperationResolver interface {
	DynamicSemanticOperationBinding(operationID string) (DynamicSemanticOperationBinding, error)
}

// BuildDynamicSemanticCatalog quarantines invalid discovery entries and
// projects only bindings backed by a valid trusted contract. A catalog contains
// no selection decision; an unneeded capability remains unrendered until the
// common ToolPlanner selects it.
func BuildDynamicSemanticCatalog(mcpEntries []MCPToolEntry, skillEntries []SkillToolEntry) (DynamicSemanticCatalog, error) {
	result := DynamicSemanticCatalog{
		Definitions: make(map[string]map[string]interface{}),
		schemas:     make(map[string]map[string]interface{}),
		bindings:    make(map[string]dynamicSemanticRuntimeBinding),
	}
	for _, entry := range mcpEntries {
		provider, definition, binding, err := ProjectMCPDynamicProvider(entry)
		if err != nil {
			continue // Invalid/unpublished contract is quarantined, not inferred.
		}
		if err := result.add(provider, definition, dynamicSemanticRuntimeBinding{provider: provider.Binding, mcp: &binding}); err != nil {
			return DynamicSemanticCatalog{}, err
		}
	}
	for _, entry := range skillEntries {
		provider, definition, binding, err := ProjectSkillDynamicProvider(entry)
		if err != nil {
			continue // See MCP path: discovery alone cannot produce a capability.
		}
		if err := result.add(provider, definition, dynamicSemanticRuntimeBinding{provider: provider.Binding, skill: &binding}); err != nil {
			return DynamicSemanticCatalog{}, err
		}
	}
	return result, nil
}

func (c *DynamicSemanticCatalog) add(provider coretool.ProviderSpec, definition map[string]interface{}, binding dynamicSemanticRuntimeBinding) error {
	if c == nil {
		return fmt.Errorf("dynamic semantic catalog is unavailable")
	}
	adapter := strings.TrimSpace(provider.AdapterName)
	if adapter == "" || binding.provider.StableID() != provider.Binding.StableID() {
		return fmt.Errorf("dynamic semantic provider binding is invalid")
	}
	function, ok := definition["function"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("dynamic semantic provider definition is invalid")
	}
	schema, ok := function["parameters"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("dynamic semantic provider schema is missing")
	}
	if _, exists := c.bindings[adapter]; exists {
		return fmt.Errorf("duplicate dynamic semantic adapter %q", adapter)
	}
	c.Providers = append(c.Providers, provider)
	c.Definitions[adapter] = definition
	c.schemas[adapter] = schema
	c.bindings[adapter] = binding
	return nil
}

// ExecuteSelection is intended to be supplied as the PlanExecutor callback.
// It validates model arguments against the same trusted closed schema that
// CatalogRenderer exposed, and dispatches only via the immutable selected
// binding. Provider transport failures are deliberately unknown unless the
// bridge proves a deterministic pre-dispatch failure (such as binding drift).
// ExecuteSelection preserves the catalog-only API for read-only callers. A
// selection that declares a receipt-bound effect fails closed here because a
// request scope and trusted coordinator are deliberately unavailable.
func (c *DynamicSemanticCatalog) ExecuteSelection(ctx context.Context, principal Principal, mcpProvider MCPToolProvider, skillProvider SkillToolProvider, selection coretool.PlannedSelection, argsJSON string) coretool.SelectionExecutionResult {
	return c.ExecuteSelectionWithEffects(ctx, coretool.InvocationScope{}, principal, mcpProvider, skillProvider, nil, selection, argsJSON)
}

// ExecuteSelectionWithEffects is the routed execution entry point. It adds the
// request scope and trusted external-effect coordinator without exposing either
// as a model argument or provider-owned extension point.
func (c *DynamicSemanticCatalog) ExecuteSelectionWithEffects(ctx context.Context, scope coretool.InvocationScope, principal Principal, mcpProvider MCPToolProvider, skillProvider SkillToolProvider, coordinator DynamicExternalEffectCoordinator, selection coretool.PlannedSelection, argsJSON string) coretool.SelectionExecutionResult {
	if c == nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_semantic_catalog_unavailable", ReasonCode: "dynamic_semantic_catalog_unavailable"}
	}
	// A request-owned surface cancels this context before it records its durable
	// terminal fact. Do not turn an already-observed cancellation into a bound
	// provider call just because the catalog was refreshed successfully a moment
	// earlier. The dispatch closure repeats this check after effect admission,
	// which is the last in-process point before an MCP/Skill/host bridge can
	// observe the binding.
	if ctx != nil && ctx.Err() != nil {
		return dynamicSemanticExecutionCancelledResult()
	}
	binding, ok := c.bindings[strings.TrimSpace(selection.AdapterName)]
	// Revalidation precedes schema lookup. A revoked or replaced binding may no
	// longer contribute a schema to the freshly observed catalog, but that does
	// not make the previously planned selection a malformed model call. It is a
	// deterministic binding-lifecycle rejection, and callers need that reason to
	// retire the old opaque grant and replan within the original constraints.
	if !ok || !dynamicSemanticProviderMatches(selection.Provider, binding.provider) {
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
	}
	canonical, err := c.CanonicalizeSelectionArguments(selection, argsJSON)
	if err != nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}
	}
	dispatch := func() (string, error) {
		if ctx != nil && ctx.Err() != nil {
			return "", fmt.Errorf("dynamic_execution_cancelled")
		}
		// Revalidate the immutable binding inside the receipt-bound operation
		// boundary. A stale catalog is a deterministic pre-I/O failure, but it
		// still needs the same host-call/execution completion transaction as
		// every other receipt-bound selection.
		if binding.host != nil {
			if binding.host.execute == nil {
				return "", fmt.Errorf("host_bound_execution_unavailable")
			}
			return binding.host.execute(ctx, principal, canonical.Values)
		}
		if binding.mcp != nil {
			return callBoundMCPTool(ctx, mcpProvider, principal, *binding.mcp, canonical.Values)
		}
		if binding.skill != nil {
			bound, ok := skillProvider.(boundSkillToolCaller)
			if !ok {
				return "", fmt.Errorf("skill_bound_execution_unavailable")
			}
			return bound.CallBoundSkill(ctx, principal, *binding.skill, canonical.Values)
		}
		return "", fmt.Errorf("dynamic_binding_stale")
	}
	if dynamicSelectionRequiresReceipt(selection) {
		return executeDynamicExternalEffect(ctx, coordinator, DynamicExternalEffectInvocation{
			Scope: scope, Principal: principal, Selection: selection, Arguments: canonical.Values,
		}, dispatch)
	}
	result, err := dispatch()
	if err != nil {
		return dynamicSemanticDispatchError(binding, err)
	}
	return coretool.SelectionExecutionResult{Result: result, Succeeded: true}
}

func dynamicSemanticExecutionCancelledResult() coretool.SelectionExecutionResult {
	return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_execution_cancelled", ReasonCode: "dynamic_execution_cancelled"}
}

func dynamicSelectionRequiresReceipt(selection coretool.PlannedSelection) bool {
	if dynamicHostLocalMutationSelection(selection) {
		return false
	}
	if dynamicHostObservedExternalSelection(selection) {
		return false
	}
	for _, effect := range selection.Effects {
		if effect == coretool.EffectExternalEffect || effect == coretool.EffectSensitive {
			return true
		}
	}
	return false
}

// dynamicHostObservedExternalSelection identifies a host-owned ssh / browser /
// CU / repo.mutate adapter. The same process waits on the bound session or
// observes HEAD, so the handler result is the observation receipt. This is
// not a channel send and must not enter the IM delivery coordinator.
// Schedule dispatch and message.send.im stay on the coordinator path.
func dynamicHostObservedExternalSelection(selection coretool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), reviewedHostProviderKind) {
		return false
	}
	external := false
	for _, effect := range selection.Effects {
		if effect == coretool.EffectExternalEffect {
			external = true
		}
	}
	if !external {
		return false
	}
	switch strings.TrimSpace(selection.AdapterName) {
	case reviewedHostSSHAdapterName, reviewedHostBrowserAdapterName, reviewedHostComputerUseAdapterName, reviewedHostRepoMutateAdapterName:
		return true
	default:
		return false
	}
}

// dynamicHostLocalMutationSelection identifies a host-owned provider whose
// declared effects are local mutations with no external effect. The same
// process performs and observes the write, so the handler result is the
// authoritative local completion receipt. This boundary is unavailable to
// Skill/MCP or any selection that also declares EffectExternalEffect.
func dynamicHostLocalMutationSelection(selection coretool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), reviewedHostProviderKind) {
		return false
	}
	local := false
	for _, effect := range selection.Effects {
		switch effect {
		case coretool.EffectSensitive, coretool.EffectLocalMutation:
			local = true
		case coretool.EffectExternalEffect:
			return false
		}
	}
	return local
}

func executeDynamicExternalEffect(ctx context.Context, coordinator DynamicExternalEffectCoordinator, invocation DynamicExternalEffectInvocation, dispatch func() (string, error)) coretool.SelectionExecutionResult {
	if coordinator == nil {
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_coordinator_unavailable", ReasonCode: "dynamic_effect_coordinator_unavailable"}
	}
	var (
		mu                  sync.Mutex
		called              bool
		dispatchFinished    bool
		coordinatorReturned bool
		result              string
		dispatchErr         error
	)
	guardedDispatch := func() (string, error) {
		mu.Lock()
		if coordinatorReturned {
			mu.Unlock()
			return "", fmt.Errorf("dynamic_effect_dispatch_late")
		}
		if called {
			mu.Unlock()
			return "", fmt.Errorf("dynamic_effect_dispatch_replayed")
		}
		called = true
		mu.Unlock()
		value, err := dispatch()
		mu.Lock()
		result, dispatchErr, dispatchFinished = value, err, true
		mu.Unlock()
		return value, err
	}
	receipt, err := coordinator.CoordinateDynamicExternalEffect(ctx, invocation, guardedDispatch)
	mu.Lock()
	coordinatorReturned = true
	dispatched, finished, dispatchedResult, dispatchedErr := called, dispatchFinished, result, dispatchErr
	mu.Unlock()
	if err != nil {
		// A deterministic dispatch error proves that the bound bridge rejected
		// before provider I/O, even when the coordinator returns that callback
		// error directly instead of a receipt.
		if dispatched && finished && dispatchedErr != nil {
			if deterministic := dynamicSemanticDispatchErrorForExternal(invocation.Selection.Provider.Kind, dispatchedErr); deterministic != nil {
				return *deterministic
			}
		}
		// No dispatch callback ran and the request was cancelled. This is a
		// local terminal fact, not an external-effect uncertainty; no provider
		// bridge could have observed the binding.
		if !dispatched && ctx != nil && ctx.Err() != nil {
			return dynamicSemanticExecutionCancelledResult()
		}
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_execution_unknown", Unknown: true, ReasonCode: "dynamic_effect_execution_unknown"}
	}
	// An invalid asynchronous coordinator may have started the callback but
	// returned before it finished. Provider I/O might already be underway, so
	// fail closed rather than accepting its incomplete receipt/result.
	if dispatched && !finished {
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_execution_unknown", Unknown: true, ReasonCode: "dynamic_effect_execution_unknown"}
	}
	if dispatchedErr != nil {
		if deterministic := dynamicSemanticDispatchErrorForExternal(invocation.Selection.Provider.Kind, dispatchedErr); deterministic != nil {
			return *deterministic
		}
		// Binding drift and a missing bound bridge are proven before a
		// transport call; retain that deterministic rejection. Every other
		// dispatch error remains unknown because the provider might have
		// accepted the effect before the response was lost.
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_execution_unknown", Unknown: true, ReasonCode: "dynamic_effect_execution_unknown"}
	}
	reason := strings.TrimSpace(receipt.ReasonCode)
	switch receipt.State {
	case DynamicEffectReceiptAccepted:
		if strings.TrimSpace(receipt.OperationID) == "" {
			return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_receipt_invalid", ReasonCode: "dynamic_effect_receipt_invalid"}
		}
		if !dispatched && !receipt.Reconciled {
			return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_receipt_dispatch_missing", ReasonCode: "dynamic_effect_receipt_dispatch_missing"}
		}
		if receipt.Reconciled {
			return coretool.SelectionExecutionResult{Result: "[system accepted] dynamic_effect_reconciled", Succeeded: true, ReasonCode: firstDynamicEffectReason(reason, "dynamic_effect_reconciled")}
		}
		return coretool.SelectionExecutionResult{Result: dispatchedResult, Succeeded: true, ReasonCode: reason}
	case DynamicEffectReceiptAwaiting, DynamicEffectReceiptFailed, DynamicEffectReceiptUnknown:
		// A coordinator may discover an existing durable operation and settle
		// it without invoking this process's dispatch closure. The operation
		// identity is therefore required for every terminal/pending state.
		if strings.TrimSpace(receipt.OperationID) == "" {
			return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_receipt_invalid", ReasonCode: "dynamic_effect_receipt_invalid"}
		}
		if receipt.State == DynamicEffectReceiptAwaiting {
			return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_awaiting_receipt", AwaitingReceipt: true, ReasonCode: firstDynamicEffectReason(reason, "dynamic_effect_awaiting_receipt")}
		}
		if receipt.State == DynamicEffectReceiptFailed {
			return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_failed", ReasonCode: firstDynamicEffectReason(reason, "dynamic_effect_failed")}
		}
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_execution_unknown", Unknown: true, ReasonCode: firstDynamicEffectReason(reason, "dynamic_effect_execution_unknown")}
	default:
		return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_receipt_invalid", ReasonCode: "dynamic_effect_receipt_invalid"}
	}
}

func firstDynamicEffectReason(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func dynamicSemanticDispatchError(binding dynamicSemanticRuntimeBinding, err error) coretool.SelectionExecutionResult {
	if err != nil && strings.TrimSpace(err.Error()) == "dynamic_execution_cancelled" {
		return dynamicSemanticExecutionCancelledResult()
	}
	if binding.host != nil {
		code := strings.TrimSpace(err.Error())
		if code == "" {
			code = "host_clock_failed"
		}
		if code == "host_delegate_timeout" || code == "host_delegate_started_is_not_complete" || code == "host_message_send_unknown" || code == "host_file_deliver_unknown" || dynamicHostObservedExternalUnknown(code) {
			return coretool.SelectionExecutionResult{Result: "[system rejected] " + code, Unknown: true, ReasonCode: code}
		}
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + code, ReasonCode: code}
	}
	if binding.mcp != nil {
		return dynamicSemanticExecutionError("mcp", err)
	}
	if binding.skill != nil {
		return dynamicSemanticExecutionError("skill", err)
	}
	return coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
}

func dynamicHostObservedExternalUnknown(code string) bool {
	switch strings.TrimSpace(code) {
	case "host_ssh_timeout", "host_ssh_session_disconnected", "host_ssh_session_unavailable", "host_ssh_outcome_unobserved",
		"host_browser_timeout", "host_browser_session_disconnected", "host_browser_session_unavailable", "host_browser_outcome_unobserved",
		"host_computer_use_timeout", "host_computer_use_runtime_unavailable",
		"host_repo_mutate_push_receipt_unknown", "host_repo_mutate_head_unobserved":
		return true
	default:
		return false
	}
}

func dynamicSemanticDispatchErrorForExternal(kind string, err error) *coretool.SelectionExecutionResult {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if err != nil && strings.TrimSpace(err.Error()) == "dynamic_execution_cancelled" {
		result := dynamicSemanticExecutionCancelledResult()
		return &result
	}
	code := strings.TrimSpace(err.Error())
	if code == "dynamic_binding_stale" || code == kind+"_binding_stale" || code == kind+" bound execution is unavailable" || code == kind+"_bound_execution_unavailable" {
		if code == "dynamic_binding_stale" {
			result := coretool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
			return &result
		}
		result := dynamicSemanticExecutionError(kind, err)
		return &result
	}
	return nil
}

func dynamicSemanticProviderMatches(selected, projected coretool.ProviderBinding) bool {
	return selected.Kind == projected.Kind && selected.ProviderID == projected.ProviderID && selected.ImplementationID == projected.ImplementationID && selected.SchemaDigest == projected.SchemaDigest
}

func dynamicSemanticExecutionError(kind string, err error) coretool.SelectionExecutionResult {
	code := strings.TrimSpace(err.Error())
	if code == kind+"_binding_stale" || code == kind+" bound execution is unavailable" {
		return coretool.SelectionExecutionResult{Result: "[system rejected] " + code, ReasonCode: code}
	}
	// Bound transports cannot establish whether a request reached a dynamic
	// provider. Preserve that uncertainty in the common execution ledger.
	return coretool.SelectionExecutionResult{Result: "[system rejected] " + kind + "_execution_unknown", Unknown: true, ReasonCode: kind + "_execution_unknown"}
}
