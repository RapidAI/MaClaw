package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// DynamicEffectEventKind is the redacted telemetry vocabulary for external
// effects. It is deliberately separate from tool-surface lifecycle events:
// those events cannot establish whether a provider accepted an effect.
type DynamicEffectEventKind string

const DynamicEffectEventUnknown DynamicEffectEventKind = "effect_unknown"

// DynamicEffectEvent is diagnostic-only. It must not contain an operation key,
// selection/binding identity, arguments, provider endpoint, or receipt body;
// those values could become a replay or correlation oracle. ReasonCode is a
// bounded local classification such as dynamic_effect_dispatch_unknown.
type DynamicEffectEvent struct {
	Kind       DynamicEffectEventKind
	ReasonCode string
}

// DynamicEffectEventObserver receives redacted external-effect lifecycle
// metrics. It never participates in admission, dispatch, settlement, retry, or
// manual resolution decisions.
type DynamicEffectEventObserver interface {
	OnDynamicEffectEvent(event DynamicEffectEvent)
}

func emitDynamicEffectUnknown(observer DynamicEffectEventObserver, reasonCode string) {
	if observer != nil {
		observer.OnDynamicEffectEvent(DynamicEffectEvent{Kind: DynamicEffectEventUnknown, ReasonCode: strings.TrimSpace(reasonCode)})
	}
}

// dynamicEffectObservedReason is the receipt recorded when a synchronous
// MCP or Skill response is itself the observation. It is not a channel
// acceptance and it is not a later integration's receipt.
const dynamicEffectObservedReason = "dynamic_effect_observed"

// dynamicMCPToolErrorReason is a completed tools/call failure. The transport
// delivered the tool's own error, so the outcome is failed rather than unknown.
const dynamicMCPToolErrorReason = "mcp_tool_error"

// LedgerDynamicExternalEffectCoordinator is the default durable coordinator
// for a receipt-bound dynamic effect. A provider return is not by itself a
// remote receipt: a newly dispatched external operation remains
// awaiting_receipt until a trusted integration settles it.
//
// One case is different. An installed MCP or Skill call selected as the
// host-issued x:mcp address, or as the open information.lookup family, has
// no later receipt source. The tools/call or skill response is the
// observation. On the unified coordinator a successful response is settled
// as the receipt and returned to the model. A completed tools/call error is
// recorded failed for every MCP selection, including a reviewed mutation,
// and the model sees that error. A lost transport stays unknown. The legacy
// ledger path does not settle successes this way: a host that registers a
// receipt source still settles those operations itself. A reviewed mutation's
// successful response stays awaiting on both paths, including when
// the planner addressed that same binding through its x:mcp grant, because
// its provider text is not evidence the system of record changed. A channel
// delivery stays awaiting for the same reason.
//
// This is provider-neutral. The selected immutable binding and canonical
// arguments derive the logical operation identity; neither a model tool-call
// ID nor the short-lived rendered function token participates in it.
type LedgerDynamicExternalEffectCoordinator struct {
	// SemanticCoordinator is the preferred owner for governed semantic
	// Skill/MCP effects. When present, operation admission, host-call outcome,
	// selection execution, receipt evidence and RouteState projection share the
	// same SQLite transaction domain as builtin and channel selections.
	SemanticCoordinator *coretool.SQLiteSemanticExecutionCoordinator
	Ledger              DynamicOperationLedger
	// ReceiptStore is optional but required for an accepted settlement. It
	// keeps the coordinator's receipt evidence separate from model-facing text
	// and binds the remote receipt digest to the durable operation key.
	ReceiptStore DynamicEffectReceiptStore
	// EventObserver receives redacted effect_unknown telemetry only. It is
	// intentionally excluded from the durable operation record and all control
	// decisions.
	EventObserver DynamicEffectEventObserver
	Now           func() time.Time
}

// UsesSemanticExecutionCoordinator identifies the production unified path
// without requiring hosts to type-assert this coordinator's concrete value or
// pointer form.
func (c LedgerDynamicExternalEffectCoordinator) UsesSemanticExecutionCoordinator() bool {
	return c.SemanticCoordinator != nil
}

// DynamicSemanticOperationBinding implements the trusted reconciliation
// resolver without exposing the ledger to provider/channel integrations.
func (c LedgerDynamicExternalEffectCoordinator) DynamicSemanticOperationBinding(operationID string) (DynamicSemanticOperationBinding, error) {
	if c.SemanticCoordinator != nil {
		operation, err := c.SemanticCoordinator.ExternalEffectOperation(operationID)
		if err != nil {
			return DynamicSemanticOperationBinding{}, fmt.Errorf("load dynamic external effect: %w", err)
		}
		return DynamicSemanticOperationBinding{
			Scope: operation.Scope, Principal: Principal{TenantID: operation.TenantID, UserID: operation.UserID},
			SelectionID: operation.SelectionID, SelectionDigest: operation.SelectionDigest,
		}, nil
	}
	return DynamicSemanticOperationBindingForOperation(c.Ledger, operationID)
}

// ResolveUnknownExternalEffect forwards an operator's out-of-band verdict to
// the durable coordinator. The legacy ledger has no unknown-only guard and no
// place to record who decided, so it does not get this door: a host still on
// that path leaves its unknown operations unknown rather than settling them
// with nothing written down.
func (c LedgerDynamicExternalEffectCoordinator) ResolveUnknownExternalEffect(scope coretool.InvocationScope, selectionID, selectionDigest, bindingID string, resolution coretool.SemanticExternalEffectResolution, now time.Time) (coretool.SemanticExternalEffectOperation, error) {
	if c.SemanticCoordinator == nil {
		return coretool.SemanticExternalEffectOperation{}, fmt.Errorf("dynamic semantic manual resolution unavailable")
	}
	return c.SemanticCoordinator.ResolveUnknownExternalEffect(scope, selectionID, selectionDigest, bindingID, resolution, now)
}

func (c LedgerDynamicExternalEffectCoordinator) CoordinateDynamicExternalEffect(ctx context.Context, invocation DynamicExternalEffectInvocation, dispatch func() (string, error)) (DynamicEffectReceipt, error) {
	if c.SemanticCoordinator != nil {
		return c.coordinateUnifiedSemanticExternalEffect(ctx, invocation, dispatch)
	}
	if c.Ledger == nil {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	if err := validateDynamicExternalEffectInvocation(invocation); err != nil {
		return DynamicEffectReceipt{}, err
	}
	if dispatch == nil {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect dispatch is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return DynamicEffectReceipt{}, ctx.Err()
	default:
	}
	now := c.now()
	bindingID := invocation.Selection.Provider.StableID()
	// NeedID is control-plane-owned purpose lineage. It keeps two distinct
	// semantic effects that happen to use the same binding and arguments from
	// collapsing into one operation, while remaining stable across a re-render
	// of the same logical selection.
	effectScope := strings.Join([]string{"semantic", strings.TrimSpace(invocation.Selection.Provider.Kind), strings.TrimSpace(invocation.Selection.NeedID)}, ":")
	record, execute, err := dynamicSemanticEffectOperationAdmission(c.Ledger, now, invocation, effectScope, bindingID)
	if err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("admit dynamic external effect: %w", err)
	}
	if !execute {
		return dynamicEffectReceiptFromOperation(record), nil
	}
	result, dispatchErr := dispatch()
	if dispatchErr != nil {
		if deterministic := dynamicSemanticDispatchErrorForExternal(invocation.Selection.Provider.Kind, dispatchErr); deterministic != nil {
			if _, err := c.Ledger.Complete(record.Key, DynamicOperationFailed, dynamicOperationDigest(deterministic.Result), deterministic.ReasonCode, c.now()); err != nil {
				return DynamicEffectReceipt{}, fmt.Errorf("record deterministic dynamic external effect failure: %w", err)
			}
			return DynamicEffectReceipt{OperationID: record.Key, State: DynamicEffectReceiptFailed, ReasonCode: deterministic.ReasonCode}, nil
		}
		if _, err := c.Ledger.Complete(record.Key, DynamicOperationUnknown, dynamicOperationDigest(result), "dynamic_effect_dispatch_unknown", c.now()); err != nil {
			return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect unknown: %w", err)
		}
		emitDynamicEffectUnknown(c.EventObserver, "dynamic_effect_dispatch_unknown")
		return DynamicEffectReceipt{OperationID: record.Key, State: DynamicEffectReceiptUnknown, ReasonCode: "dynamic_effect_dispatch_unknown"}, nil
	}
	if completedDynamicToolResultFailure(invocation.Selection, result) {
		if _, err := c.Ledger.Complete(record.Key, DynamicOperationFailed, dynamicOperationDigest(result), dynamicMCPToolErrorReason, c.now()); err != nil {
			return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect tool failure: %w", err)
		}
		return DynamicEffectReceipt{OperationID: record.Key, State: DynamicEffectReceiptFailed, ReasonCode: dynamicMCPToolErrorReason}, nil
	}
	if _, err := c.Ledger.Complete(record.Key, DynamicOperationAwaitingReceipt, dynamicOperationDigest(result), "dynamic_effect_awaiting_receipt", c.now()); err != nil {
		// A provider response was observed, but the operation record could not
		// be persisted. The caller must not infer success or retry dispatch.
		return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect awaiting receipt: %w", err)
	}
	return DynamicEffectReceipt{OperationID: record.Key, State: DynamicEffectReceiptAwaiting, ReasonCode: "dynamic_effect_awaiting_receipt"}, nil
}

// SettleDynamicExternalEffect records an integration-verified receipt after
// dispatch. It only advances an existing running/awaiting operation to a
// terminal state and never invokes a provider. Callers are trusted channel or
// provider reconciliation workers; model arguments must never reach it.
func (c LedgerDynamicExternalEffectCoordinator) SettleDynamicExternalEffect(settlement DynamicExternalEffectSettlement) (DynamicEffectReceipt, error) {
	if c.SemanticCoordinator != nil {
		return c.settleUnifiedSemanticExternalEffect(settlement)
	}
	if c.Ledger == nil {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	if err := validateDynamicExternalEffectSettlement(settlement); err != nil {
		return DynamicEffectReceipt{}, err
	}
	operationID := strings.TrimSpace(settlement.OperationID)
	if operationID == "" {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect operation id is required")
	}
	current, err := c.Ledger.Get(operationID)
	if err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("load dynamic external effect: %w", err)
	}
	if !dynamicOperationMatchesSettlement(current, settlement) {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect operation binding mismatch")
	}
	receiptDigest := ""
	if settlement.State == DynamicEffectReceiptAccepted {
		if c.ReceiptStore == nil {
			return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect receipt store is unavailable")
		}
		storedReceipt, err := c.ReceiptStore.Accept(settlement.OperationID, settlement.Receipt, c.now())
		if err != nil {
			return DynamicEffectReceipt{}, fmt.Errorf("record dynamic effect receipt: %w", err)
		}
		if storedReceipt.OperationID != operationID || strings.TrimSpace(storedReceipt.ReceiptDigest) != coretool.SchemaDigest([]byte(settlement.Receipt)) {
			// ReceiptStore owns the canonical receipt digest. Compare it against
			// the raw receipt bytes, not dynamicOperationDigest (which is the
			// logical-operation JSON digest profile).
			if storedReceipt.OperationID != operationID || strings.TrimSpace(storedReceipt.ReceiptDigest) == "" {
				return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect receipt persistence mismatch")
			}
		}
		receiptDigest = storedReceipt.ReceiptDigest
	}
	var terminal DynamicOperationState
	switch settlement.State {
	case DynamicEffectReceiptAccepted:
		terminal = DynamicOperationSucceeded
	case DynamicEffectReceiptFailed:
		terminal = DynamicOperationFailed
	case DynamicEffectReceiptUnknown:
		terminal = DynamicOperationUnknown
	default:
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect receipt settlement state is invalid")
	}
	record, err := c.Ledger.Settle(operationID, terminal, "", receiptDigest, strings.TrimSpace(settlement.ReasonCode), c.now())
	if err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("settle dynamic external effect: %w", err)
	}
	if settlement.State == DynamicEffectReceiptAccepted && (record.State != DynamicOperationSucceeded || record.ReceiptDigest != receiptDigest) {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect receipt settlement conflict")
	}
	if record.State == DynamicOperationUnknown {
		emitDynamicEffectUnknown(c.EventObserver, record.ReasonCode)
	}
	return dynamicEffectReceiptFromOperation(record), nil
}

func (c LedgerDynamicExternalEffectCoordinator) coordinateUnifiedSemanticExternalEffect(ctx context.Context, invocation DynamicExternalEffectInvocation, dispatch func() (string, error)) (DynamicEffectReceipt, error) {
	if err := validateDynamicExternalEffectInvocation(invocation); err != nil {
		return DynamicEffectReceipt{}, err
	}
	if dispatch == nil {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect dispatch is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return DynamicEffectReceipt{}, ctx.Err()
	default:
	}
	operation, err := DynamicSemanticExternalEffectOperationFromInvocation(invocation)
	if err != nil {
		return DynamicEffectReceipt{}, err
	}
	// The matching host call was admitted by the semantic surface immediately
	// before provider I/O. The coordinator uses this operation boundary only to
	// acquire the stable external-effect identity; it never admits a grant.
	admission, ok := DynamicSemanticAdmissionFromContext(ctx)
	if !ok {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic semantic execution admission unavailable")
	}
	prepared, execute, err := c.SemanticCoordinator.PrepareExternalEffect(admission, operation)
	if err != nil {
		return DynamicEffectReceipt{}, err
	}
	if !execute {
		return dynamicEffectReceiptFromUnifiedOperation(prepared), nil
	}
	result, dispatchErr := dispatch()
	if dispatchErr != nil {
		if deterministic := dynamicSemanticDispatchErrorForExternal(invocation.Selection.Provider.Kind, dispatchErr); deterministic != nil {
			if _, err := c.SemanticCoordinator.CompleteExternalEffectDispatch(admission, prepared.OperationKey, coretool.SemanticExternalEffectFailed, deterministic.Result, deterministic.ReasonCode, c.now()); err != nil {
				return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect failure: %w", err)
			}
			return DynamicEffectReceipt{OperationID: prepared.OperationKey, State: DynamicEffectReceiptFailed, ReasonCode: deterministic.ReasonCode}, nil
		}
		if _, err := c.SemanticCoordinator.CompleteExternalEffectDispatch(admission, prepared.OperationKey, coretool.SemanticExternalEffectUnknown, result, "dynamic_effect_dispatch_unknown", c.now()); err != nil {
			return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect unknown: %w", err)
		}
		emitDynamicEffectUnknown(c.EventObserver, "dynamic_effect_dispatch_unknown")
		return DynamicEffectReceipt{OperationID: prepared.OperationKey, State: DynamicEffectReceiptUnknown, ReasonCode: "dynamic_effect_dispatch_unknown"}, nil
	}
	if completedDynamicToolResultFailure(invocation.Selection, result) {
		// The headless bridge encodes a finished tools/call error as an
		// "Error:" string with a nil Go error. That includes a reviewed
		// mutation, whose successful body still waits for a receipt. Settling
		// the error as awaiting would hide it and leave dependants blocked on
		// a call the tool already refused.
		if _, err := c.SemanticCoordinator.CompleteExternalEffectDispatch(admission, prepared.OperationKey, coretool.SemanticExternalEffectFailed, result, dynamicMCPToolErrorReason, c.now()); err != nil {
			return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect tool failure: %w", err)
		}
		return DynamicEffectReceipt{OperationID: prepared.OperationKey, State: DynamicEffectReceiptFailed, ReasonCode: dynamicMCPToolErrorReason}, nil
	}
	if synchronousDynamicEffectObservation(invocation.Selection) {
		return c.observeSynchronousDynamicExternalEffect(admission, prepared.OperationKey, invocation, result)
	}
	if _, err := c.SemanticCoordinator.CompleteExternalEffectDispatch(admission, prepared.OperationKey, coretool.SemanticExternalEffectAwaitingReceipt, result, "dynamic_effect_awaiting_receipt", c.now()); err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect awaiting receipt: %w", err)
	}
	return DynamicEffectReceipt{OperationID: prepared.OperationKey, State: DynamicEffectReceiptAwaiting, ReasonCode: "dynamic_effect_awaiting_receipt"}, nil
}

// observeSynchronousDynamicExternalEffect records a completed MCP or Skill
// response as the receipt of that same call. The dispatch is first closed as
// awaiting_receipt, which is the only state SettleExternalEffectReceipt
// accepts, and then settled with a digest of the returned text. Reconciled
// stays false so the caller returns that text instead of a reconciliation
// placeholder. A dispatch that did not return is never observed here.
func (c LedgerDynamicExternalEffectCoordinator) observeSynchronousDynamicExternalEffect(admission coretool.SemanticExecutionAdmission, operationKey string, invocation DynamicExternalEffectInvocation, result string) (DynamicEffectReceipt, error) {
	now := c.now()
	if _, err := c.SemanticCoordinator.CompleteExternalEffectDispatch(admission, operationKey, coretool.SemanticExternalEffectAwaitingReceipt, result, dynamicEffectObservedReason, now); err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("record dynamic external effect observation: %w", err)
	}
	settled, err := c.SemanticCoordinator.SettleExternalEffectReceipt(invocation.Scope, invocation.Selection.ID, dynamicSemanticSelectionDigest(invocation.Selection), invocation.Selection.Provider.StableID(), operationKey, coretool.SemanticExternalEffectSucceeded, coretool.SchemaDigest([]byte(result)), dynamicEffectObservedReason, now)
	if err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("settle dynamic external effect observation: %w", err)
	}
	if settled.State != coretool.SemanticExternalEffectSucceeded || settled.ReceiptDigest != coretool.SchemaDigest([]byte(result)) {
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic external effect observation conflict")
	}
	return DynamicEffectReceipt{OperationID: operationKey, State: DynamicEffectReceiptAccepted, ReasonCode: dynamicEffectObservedReason}, nil
}

// synchronousDynamicEffectObservation reports a request/response MCP or Skill
// call whose returned text is the effect. The host-issued per-tool address
// and the open lookup family are that shape. A reviewed mutation, a delivery,
// or any other external capability still waits for its own receipt. The
// x:mcp grant is only an address: a reviewed mutation selected through it
// still waits.
func synchronousDynamicEffectObservation(selection coretool.PlannedSelection) bool {
	switch strings.ToLower(strings.TrimSpace(selection.Provider.Kind)) {
	case "mcp", "skill":
	default:
		return false
	}
	if reviewedMCPEffectDefersObservation(selection.Provider.ImplementationID) {
		return false
	}
	capability := strings.TrimSpace(string(selection.FitProof.MatchedCapability))
	if capability == string(CapabilityInformationLookup) {
		return true
	}
	return strings.HasPrefix(capability, autoMCPCapabilityPrefix)
}

// completedDynamicToolResultFailure reports a dispatch body the host already
// classifies as a hard failure. Every MCP selection is included: the headless
// bridge encodes a finished tools/call error as an "Error:" string and a nil
// Go error, and a reviewed mutation must not park that error in
// awaiting_receipt. A skill uses the same classification only when its own
// response is the observation. A normal provider body stays on the receipt path.
func completedDynamicToolResultFailure(selection coretool.PlannedSelection, result string) bool {
	if !agentruntime.SelectionFailed(result) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(selection.Provider.Kind)) {
	case "mcp":
		return true
	case "skill":
		return synchronousDynamicEffectObservation(selection)
	default:
		return false
	}
}

// reviewedMCPEffectDefersObservation reports a code-reviewed MCP tool whose
// contract is an external effect and not a read. work_log_update is that
// shape, so its provider text cannot close the receipt. work_log_query is a
// read, and unannotated tools are not in the reviewed map, so both stay on
// the observation path.
func reviewedMCPEffectDefersObservation(implementationID string) bool {
	contract, ok := ReviewedMCPCapabilityContracts()[strings.TrimSpace(implementationID)]
	if !ok {
		return false
	}
	readOnly := false
	external := false
	for _, effect := range contract.Effects {
		switch effect {
		case coretool.EffectReadOnly:
			readOnly = true
		case coretool.EffectExternalEffect, coretool.EffectSensitive:
			external = true
		}
	}
	return external && !readOnly
}

// DynamicSemanticExternalEffectOperationFromInvocation exposes the stable
// operation identity used by the unified coordinator to the semantic host. It
// contains only digests and immutable binding identity, never raw parameters,
// credentials or a provider endpoint.
func DynamicSemanticExternalEffectOperationFromInvocation(invocation DynamicExternalEffectInvocation) (coretool.SemanticExternalEffectOperation, error) {
	if err := validateDynamicExternalEffectInvocation(invocation); err != nil {
		return coretool.SemanticExternalEffectOperation{}, err
	}
	requestDigest := dynamicOperationDigest(invocation.Arguments)
	bindingID := invocation.Selection.Provider.StableID()
	effectScope := strings.Join([]string{"semantic", strings.TrimSpace(invocation.Selection.Provider.Kind), strings.TrimSpace(invocation.Selection.NeedID)}, ":")
	return coretool.SemanticExternalEffectOperation{
		OperationKey:    dynamicOperationKey(invocation.Principal.TenantID, invocation.Principal.UserID, invocation.Scope.RootTaskID, effectScope, bindingID, requestDigest),
		Scope:           invocation.Scope,
		TenantID:        invocation.Principal.TenantID,
		UserID:          invocation.Principal.UserID,
		SelectionID:     invocation.Selection.ID,
		SelectionDigest: dynamicSemanticSelectionDigest(invocation.Selection),
		BindingID:       bindingID,
		RequestDigest:   requestDigest,
	}, nil
}

func (c LedgerDynamicExternalEffectCoordinator) settleUnifiedSemanticExternalEffect(settlement DynamicExternalEffectSettlement) (DynamicEffectReceipt, error) {
	if err := validateDynamicExternalEffectSettlement(settlement); err != nil {
		return DynamicEffectReceipt{}, err
	}
	var outcome coretool.SemanticExternalEffectState
	switch settlement.State {
	case DynamicEffectReceiptAccepted:
		outcome = coretool.SemanticExternalEffectSucceeded
	case DynamicEffectReceiptFailed:
		outcome = coretool.SemanticExternalEffectFailed
	case DynamicEffectReceiptUnknown:
		outcome = coretool.SemanticExternalEffectUnknown
	default:
		return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect receipt settlement state is invalid")
	}
	receiptDigest := ""
	if settlement.State == DynamicEffectReceiptAccepted {
		if strings.TrimSpace(settlement.Receipt) == "" {
			return DynamicEffectReceipt{}, fmt.Errorf("dynamic effect receipt is required")
		}
		receiptDigest = coretool.SchemaDigest([]byte(settlement.Receipt))
	}
	record, err := c.SemanticCoordinator.SettleExternalEffectReceipt(settlement.Scope, settlement.Selection.ID, dynamicSemanticSelectionDigest(settlement.Selection), settlement.Selection.Provider.StableID(), settlement.OperationID, outcome, receiptDigest, settlement.ReasonCode, c.now())
	if err != nil {
		return DynamicEffectReceipt{}, fmt.Errorf("settle dynamic external effect: %w", err)
	}
	if record.State == coretool.SemanticExternalEffectUnknown {
		emitDynamicEffectUnknown(c.EventObserver, record.ReasonCode)
	}
	return dynamicEffectReceiptFromUnifiedOperation(record), nil
}

func dynamicEffectReceiptFromUnifiedOperation(record coretool.SemanticExternalEffectOperation) DynamicEffectReceipt {
	receipt := DynamicEffectReceipt{OperationID: record.OperationKey, ReasonCode: strings.TrimSpace(record.ReasonCode)}
	switch record.State {
	case coretool.SemanticExternalEffectSucceeded:
		if strings.TrimSpace(record.ReceiptDigest) == "" {
			receipt.State, receipt.ReasonCode = DynamicEffectReceiptUnknown, "dynamic_effect_receipt_missing"
			return receipt
		}
		receipt.State, receipt.Reconciled = DynamicEffectReceiptAccepted, true
	case coretool.SemanticExternalEffectFailed:
		receipt.State = DynamicEffectReceiptFailed
	case coretool.SemanticExternalEffectUnknown:
		receipt.State = DynamicEffectReceiptUnknown
	default:
		receipt.State = DynamicEffectReceiptAwaiting
		if receipt.ReasonCode == "" {
			receipt.ReasonCode = "dynamic_effect_awaiting_receipt"
		}
	}
	return receipt
}

func (c LedgerDynamicExternalEffectCoordinator) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func validateDynamicExternalEffectInvocation(invocation DynamicExternalEffectInvocation) error {
	if strings.TrimSpace(invocation.Scope.RootTaskID) == "" || strings.TrimSpace(invocation.Scope.PrincipalID) == "" {
		return fmt.Errorf("dynamic effect invocation scope is invalid")
	}
	if strings.TrimSpace(invocation.Principal.TenantID) == "" || strings.TrimSpace(invocation.Principal.UserID) == "" {
		return fmt.Errorf("dynamic effect invocation principal is invalid")
	}
	if strings.TrimSpace(invocation.Selection.ID) == "" || strings.TrimSpace(invocation.Selection.Provider.StableID()) == "" {
		return fmt.Errorf("dynamic effect invocation selection is invalid")
	}
	if !dynamicSelectionRequiresReceipt(invocation.Selection) {
		return fmt.Errorf("dynamic effect invocation is not receipt-bound")
	}
	return nil
}

func validateDynamicExternalEffectSettlement(settlement DynamicExternalEffectSettlement) error {
	if err := validateDynamicExternalEffectInvocation(DynamicExternalEffectInvocation{Scope: settlement.Scope, Principal: settlement.Principal, Selection: settlement.Selection}); err != nil {
		return fmt.Errorf("dynamic effect settlement: %w", err)
	}
	if strings.TrimSpace(settlement.OperationID) == "" {
		return fmt.Errorf("dynamic effect settlement operation id is required")
	}
	return nil
}

func dynamicOperationMatchesSettlement(record DynamicOperationRecord, settlement DynamicExternalEffectSettlement) bool {
	if record.TenantID != settlement.Principal.TenantID || record.UserID != settlement.Principal.UserID || record.SessionID != settlement.Scope.RootTaskID {
		return false
	}
	// Receipt-bound semantic operations persist the exact plan-selection
	// identity at admission. Requiring it here prevents two selections that
	// happen to share a provider and need lineage from settling each other's
	// external operation. Legacy rows without this immutable mapping fail
	// closed; they may remain unknown/manual-resolution but cannot be promoted.
	if record.InvocationPlanID != settlement.Scope.PlanID || record.InvocationSessionID != settlement.Scope.SessionID || record.InvocationTurnID != settlement.Scope.TurnID || record.InvocationPrincipalID != settlement.Scope.PrincipalID || record.SelectionID != settlement.Selection.ID || record.SelectionDigest != dynamicSemanticSelectionDigest(settlement.Selection) {
		return false
	}
	wantBinding := settlement.Selection.Provider.StableID()
	wantKind := strings.Join([]string{"semantic", strings.TrimSpace(settlement.Selection.Provider.Kind), strings.TrimSpace(settlement.Selection.NeedID)}, ":")
	return record.BindingID == wantBinding && record.AdapterKind == wantKind
}

// DynamicSemanticOperationBinding is the host-owned identity a trusted
// reconciliation worker needs to settle an operation. It has no function
// token, provider display name, model call ID, or dispatch closure.
type DynamicSemanticOperationBinding struct {
	Scope           coretool.InvocationScope
	Principal       Principal
	SelectionID     string
	SelectionDigest string
}

// DynamicSemanticOperationBindingForOperation resolves the immutable
// scope/principal/selection binding recorded at dispatch admission. It is a
// read-only trusted-host API; callers cannot manufacture an operation mapping
// by passing their own selection or provider name.
func DynamicSemanticOperationBindingForOperation(ledger DynamicOperationLedger, operationID string) (DynamicSemanticOperationBinding, error) {
	if ledger == nil {
		return DynamicSemanticOperationBinding{}, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	record, err := ledger.Get(strings.TrimSpace(operationID))
	if err != nil {
		return DynamicSemanticOperationBinding{}, fmt.Errorf("load dynamic external effect: %w", err)
	}
	if strings.TrimSpace(record.InvocationPlanID) == "" || strings.TrimSpace(record.InvocationSessionID) == "" || strings.TrimSpace(record.InvocationTurnID) == "" || strings.TrimSpace(record.InvocationPrincipalID) == "" || strings.TrimSpace(record.SelectionID) == "" || strings.TrimSpace(record.SelectionDigest) == "" {
		return DynamicSemanticOperationBinding{}, fmt.Errorf("dynamic effect operation reconciliation binding unavailable")
	}
	return DynamicSemanticOperationBinding{
		Scope:     coretool.InvocationScope{RootTaskID: record.SessionID, PlanID: record.InvocationPlanID, SessionID: record.InvocationSessionID, TurnID: record.InvocationTurnID, PrincipalID: record.InvocationPrincipalID},
		Principal: Principal{TenantID: record.TenantID, UserID: record.UserID}, SelectionID: record.SelectionID, SelectionDigest: record.SelectionDigest,
	}, nil
}

func dynamicSemanticEffectOperationAdmission(ledger DynamicOperationLedger, now time.Time, invocation DynamicExternalEffectInvocation, kind, bindingID string) (DynamicOperationRecord, bool, error) {
	if ledger == nil {
		return DynamicOperationRecord{}, false, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	requestDigest := dynamicOperationDigest(invocation.Arguments)
	record := DynamicOperationRecord{
		Key:      dynamicOperationKey(invocation.Principal.TenantID, invocation.Principal.UserID, invocation.Scope.RootTaskID, kind, bindingID, requestDigest),
		TenantID: invocation.Principal.TenantID, UserID: invocation.Principal.UserID, SessionID: invocation.Scope.RootTaskID,
		AdapterKind: kind, BindingID: bindingID, RequestDigest: requestDigest,
		InvocationPlanID: invocation.Scope.PlanID, InvocationSessionID: invocation.Scope.SessionID, InvocationTurnID: invocation.Scope.TurnID, InvocationPrincipalID: invocation.Scope.PrincipalID,
		SelectionID: invocation.Selection.ID, SelectionDigest: dynamicSemanticSelectionDigest(invocation.Selection), CreatedAt: now.UTC(),
	}
	return ledger.Acquire(record)
}

func dynamicSemanticSelectionDigest(selection coretool.PlannedSelection) string {
	// Marshal provides a stable digest over the complete immutable selection
	// contract (binding, effects, need lineage, artifact/confirmation edges),
	// not merely its mutable presentation or short-lived adapter token.
	return dynamicOperationDigest(selection)
}

func dynamicEffectReceiptFromOperation(record DynamicOperationRecord) DynamicEffectReceipt {
	receipt := DynamicEffectReceipt{OperationID: record.Key, ReasonCode: strings.TrimSpace(record.ReasonCode)}
	switch record.State {
	case DynamicOperationSucceeded:
		if strings.TrimSpace(record.ReceiptDigest) == "" {
			// A legacy/corrupt success row without receipt evidence is never
			// eligible to promote a semantic selection to succeeded.
			receipt.State = DynamicEffectReceiptUnknown
			receipt.ReasonCode = "dynamic_effect_receipt_missing"
			return receipt
		}
		receipt.State, receipt.Reconciled = DynamicEffectReceiptAccepted, true
	case DynamicOperationFailed:
		receipt.State = DynamicEffectReceiptFailed
	case DynamicOperationUnknown:
		receipt.State = DynamicEffectReceiptUnknown
	default:
		// running and awaiting_receipt are both non-replayable pending work.
		receipt.State = DynamicEffectReceiptAwaiting
		if receipt.ReasonCode == "" {
			receipt.ReasonCode = "dynamic_effect_awaiting_receipt"
		}
	}
	return receipt
}
