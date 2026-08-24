package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingBoundDynamicRequestLifecycleRelay is the callback-facing ownership
// seam for a future qualified adapter. It centralizes all four shared-loop
// extensions so a callback cannot reserve via one object but bind or dispatch
// through another. It is deliberately inert while production qualification is
// disabled: Reserve returns nil and RunLoop stays on S0.5 compatibility.
type codingBoundDynamicRequestLifecycleRelay struct {
	mu       sync.Mutex
	handler  *IMMessageHandler
	identity *trustedCodingInvocationIdentity
	factory  codingBoundDynamicRequestAdapterFactory
	active   *codingBoundDynamicRequestAdapter
	// terminating fences successor admission while the active holder writes its
	// durable terminal fact. The holder remains active until that write returns,
	// so no second request can be published over an unresolved predecessor.
	terminating bool
	// terminalErr latches a failed durable terminal write. It is diagnostic
	// evidence, but also an admission fence: restart recovery can still observe
	// the predecessor as active, so publishing a successor would be unsafe.
	terminalErr error
}

func newCodingBoundDynamicRequestLifecycleRelay(handler *IMMessageHandler, identity *trustedCodingInvocationIdentity, factory codingBoundDynamicRequestAdapterFactory) *codingBoundDynamicRequestLifecycleRelay {
	if handler == nil || identity == nil || !identity.complete() || factory == nil {
		return nil
	}
	copy := *identity
	return &codingBoundDynamicRequestLifecycleRelay{handler: handler, identity: &copy, factory: factory}
}

func (r *codingBoundDynamicRequestLifecycleRelay) ReserveToolSurfaceRequestChannel(ctx context.Context, cfg corelib.MaclawLLMConfig) (agent.ToolSurfaceRequestChannel, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminating {
		return nil, fmt.Errorf("surface_integrity_failure: coding dynamic request terminal transition in progress")
	}
	if r.terminalErr != nil {
		return nil, fmt.Errorf("surface_integrity_failure: coding dynamic request terminal state was not durable: %w", r.terminalErr)
	}
	// A callback must not silently replace a holder that might still own a
	// response-bound surface. Future refresh/replan wiring must explicitly
	// retire it through CloseForLifecycle first.
	if r.active != nil {
		return nil, fmt.Errorf("coding dynamic request lifecycle already active")
	}
	adapter, err := r.factory(ctx, r.handler, r.identity, cfg)
	if err != nil || adapter == nil {
		return nil, err
	}
	execution := adapter.ExecutionContext()
	if strings.TrimSpace(execution.Protocol) == "" || strings.TrimSpace(execution.ConnectionID) == "" {
		adapter.Close(fmt.Errorf("coding dynamic request channel correlation is required"))
		return nil, fmt.Errorf("coding dynamic request channel correlation is required")
	}
	r.active = adapter
	return adapter, nil
}

func (r *codingBoundDynamicRequestLifecycleRelay) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	return r.RenderPublishedBoundToolSurface(userText, iteration, execution).Definitions
}

func (r *codingBoundDynamicRequestLifecycleRelay) RenderPublishedBoundToolSurface(userText string, iteration int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if r == nil {
		return agent.BoundToolSurfaceRender{Failure: "coding dynamic request relay is unavailable"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.active
	terminating := r.terminating
	if terminating {
		return agent.BoundToolSurfaceRender{Failure: "surface_integrity_failure: coding dynamic request terminal transition in progress"}
	}
	if active == nil {
		return agent.BoundToolSurfaceRender{Failure: "coding dynamic request relay has no active reservation"}
	}
	return active.RenderPublishedBoundToolSurface(userText, iteration, execution)
}

// ToolSurfaceAuditEvidence forwards only request-local plan evidence from the
// active holder. A nil relay deliberately reports unavailable evidence so the
// static S0.5 callback path never fabricates a ToolPlan.
func (r *codingBoundDynamicRequestLifecycleRelay) ToolSurfaceAuditEvidence(execution agent.ToolCallExecutionContext) agent.ToolSurfacePlanEvidence {
	if r == nil {
		return agent.ToolSurfacePlanEvidence{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.active
	terminating := r.terminating
	if terminating || active == nil {
		return agent.ToolSurfacePlanEvidence{}
	}
	// Evidence is read-only, but it is still an input to the transport's
	// immutable handoff frame. Keep it inside the relay fence so terminal cannot
	// begin between selecting the holder and reading its plan facts. The holder
	// performs no I/O here, so this does not extend the terminal critical path
	// across a durable operation.
	return active.ToolSurfaceAuditEvidence(execution)
}

func (r *codingBoundDynamicRequestLifecycleRelay) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.active
	terminating := r.terminating
	if terminating {
		return fmt.Errorf("surface_integrity_failure: coding dynamic request terminal transition in progress")
	}
	if active == nil {
		return fmt.Errorf("stale_surface")
	}
	return active.BindToolSurfaceResponse(execution)
}

func (r *codingBoundDynamicRequestLifecycleRelay) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	r.mu.Lock()
	active := r.active
	terminating := r.terminating
	if terminating {
		r.mu.Unlock()
		return rejectedCodingDynamicAdapterResult("surface_integrity_failure")
	}
	if active == nil {
		r.mu.Unlock()
		return rejectedCodingDynamicAdapterResult("stale_surface")
	}
	// Freeze a request-local execution ticket while terminal cannot yet win the
	// relay fence. The actual durable admission/provider work happens only after
	// this function returns and therefore must observe the ticket context, which
	// lifecycle cancellation closes before its durable terminal write begins.
	call, ok := active.beginBoundToolCall(name, argsJSON, callID, execution)
	if !ok {
		r.mu.Unlock()
		return rejectedCodingDynamicAdapterResult("stale_surface")
	}
	// Do not retain r.mu during catalog/provider work.
	r.mu.Unlock()
	return executeCodingBoundToolCall(call)
}

func (r *codingBoundDynamicRequestLifecycleRelay) CloseForLifecycle(reason codingBoundDynamicRequestTerminalReason) {
	if r == nil {
		return
	}
	r.mu.Lock()
	active := r.active
	if active == nil || r.terminating {
		r.mu.Unlock()
		return
	}
	r.terminating = true
	r.mu.Unlock()
	r.finishTerminal(active, reason)
}

// OnToolSurfaceDisposition is the semantic, once-only completion callback for
// the reservation which this relay exposed to RunLoop. It deliberately does
// not treat channel.Close(nil) as success: the loop tells us whether the
// response was abandoned, settled as text, or durably committed as a complete
// tool batch. A mismatched/unknown disposition is fail-closed.
func (r *codingBoundDynamicRequestLifecycleRelay) OnToolSurfaceDisposition(execution agent.ToolCallExecutionContext, disposition agent.ToolSurfaceDisposition) {
	if r == nil {
		return
	}
	r.mu.Lock()
	active := r.active
	// A binder or channel error may already have retired the holder before the
	// loop emits its one disposition. The relay must still forget that exact
	// reservation; otherwise the terminal pointer would reject every successor
	// reservation. Exact tuple comparison remains mandatory, but terminal state
	// is intentionally not part of this ownership check.
	if active == nil || r.terminating || !codingBoundDynamicRequestReservationMatchesExecution(active, execution) {
		r.mu.Unlock()
		return
	}
	r.terminating = true
	r.mu.Unlock()
	r.finishTerminal(active, codingBoundDynamicRequestReasonForDisposition(disposition))
}

// finishTerminal is the relay's linearization barrier between one request and
// its successor. It intentionally performs the durable operation outside the
// relay lock, but leaves active and terminating set until the result is known.
// A durable failure permanently closes successor admission for this relay.
func (r *codingBoundDynamicRequestLifecycleRelay) finishTerminal(active *codingBoundDynamicRequestAdapter, reason codingBoundDynamicRequestTerminalReason) {
	err := active.CloseForLifecycle(reason)
	if err == nil {
		// A renderer/pre-dispatch failure may have terminally fenced the holder
		// before RunLoop delivers its single disposition. CloseForLifecycle is
		// deliberately idempotent in that state, so preserve the original
		// durability uncertainty rather than clearing successor admission.
		err = active.terminalDurabilityError()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == active {
		r.active = nil
	}
	r.terminating = false
	if err != nil && r.terminalErr == nil {
		r.terminalErr = err
	}
}

// TerminalDurabilityError exposes the latched terminal durability failure for
// lifecycle diagnostics and host health reporting. The error is never retry,
// route or alias authority; it only closes further request admission.
func (r *codingBoundDynamicRequestLifecycleRelay) TerminalDurabilityError() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.terminalErr
}

func codingBoundDynamicRequestMatchesExecution(adapter *codingBoundDynamicRequestAdapter, execution agent.ToolCallExecutionContext) bool {
	if adapter == nil {
		return false
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return !adapter.terminal && adapter.matchesReservationLocked(execution)
}

func codingBoundDynamicRequestReservationMatchesExecution(adapter *codingBoundDynamicRequestAdapter, execution agent.ToolCallExecutionContext) bool {
	if adapter == nil {
		return false
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return adapter.matchesReservationLocked(execution)
}

func codingBoundDynamicRequestReasonForDisposition(disposition agent.ToolSurfaceDisposition) codingBoundDynamicRequestTerminalReason {
	switch disposition {
	case agent.ToolSurfaceSteered:
		return codingBoundDynamicRequestSteered
	case agent.ToolSurfaceRuntimeTerminal:
		return codingBoundDynamicRequestRuntimeClosed
	case agent.ToolSurfaceIntegrityFailure:
		return codingBoundDynamicRequestTransportFail
	case agent.ToolSurfaceTransportFailure:
		return codingBoundDynamicRequestTransportFail
	case agent.ToolSurfaceResponseAbandoned:
		return codingBoundDynamicRequestResponseAbandoned
	case agent.ToolSurfaceResponseSettled:
		return codingBoundDynamicRequestResponseSettled
	case agent.ToolSurfaceToolBatchSettled:
		return codingBoundDynamicRequestToolBatchSettled
	default:
		return "unknown_surface_disposition"
	}
}

// newQualifiedCodingBoundDynamicRequestLifecycleRelay intentionally creates no
// relay unless the app-owned production qualification says the complete
// callback contract is wired and enabled. Current rows return nil without a
// WebSocket dial, catalog read, plan publication, or alias materialization.
func newQualifiedCodingBoundDynamicRequestLifecycleRelay(handler *IMMessageHandler, identity *trustedCodingInvocationIdentity, cfg corelib.MaclawLLMConfig) *codingBoundDynamicRequestLifecycleRelay {
	qualification := codingDynamicProductionAdapterForConfig(cfg)
	if !qualification.eligible() {
		return nil
	}
	return newCodingBoundDynamicRequestLifecycleRelay(handler, identity, reserveCodingBoundDynamicRequestAdapter)
}

// reserveCodingBoundDynamicRequestAdapter is the sole future callback
// construction path. It is deliberately not called while qualification is
// disabled. The order is intentional: reserve the live channel first, then
// prepare a complete host-policy plan, then let RunLoop render/publish the
// surface using the exact reservation tuple and loop-assigned epoch.
func reserveCodingBoundDynamicRequestAdapter(ctx context.Context, handler *IMMessageHandler, identity *trustedCodingInvocationIdentity, cfg corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
	qualification := codingDynamicProductionAdapterForConfig(cfg)
	if !qualification.eligible() {
		return nil, nil
	}
	if handler == nil || handler.app == nil || identity == nil || !identity.complete() {
		return nil, fmt.Errorf("coding dynamic production adapter prerequisites are incomplete")
	}
	channel, err := reserveCodingResponsesWSRequestChannel(ctx, handler, cfg, nil)
	if err != nil || channel == nil {
		return nil, err
	}
	if err := validateCodingDynamicQualifiedRequestChannel(qualification, cfg, channel); err != nil {
		channel.Close(err)
		return nil, err
	}
	dynamic, err := handler.codingDynamicCatalogForIdentity(ctx, identity)
	if err != nil {
		channel.Close(err)
		return nil, err
	}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, codingDynamicCapabilityNeeds(), nil, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		channel.Close(err)
		return nil, err
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		channel.Close(err)
		return nil, err
	}
	return adapter, nil
}

// validateCodingDynamicQualifiedRequestChannel binds a structural production
// qualification to the live reservation it is about to authorize. A
// certificate that only matches a configuration row is insufficient: the
// actual channel could expose another protocol or lack the verified-dispatch
// / audit-evidence / invocation-policy contract that the certificate
// presupposes.
//
// This factory is intentionally Responses-WS specific. cfg only selects that
// fixed, reviewed envelope; it never contributes transport identity or
// authority. Protocol and ConnectionID are read exclusively from the live
// channel and must match the host-reviewed certificate exactly.
func validateCodingDynamicQualifiedRequestChannel(qualification codingDynamicProductionAdapterQualification, cfg corelib.MaclawLLMConfig, channel agent.ToolSurfaceRequestChannel) error {
	if !qualification.eligible() {
		return fmt.Errorf("coding dynamic production qualification is not eligible")
	}
	certificate := qualification.ReplacementSemantics
	if !cfg.IsResponsesWebSocket() || certificate == nil || certificate.Envelope != agent.ToolSurfaceEnvelopeResponses {
		return fmt.Errorf("coding dynamic qualification does not authorize the responses websocket envelope")
	}
	if _, ok := channel.(agent.VerifiedToolSurfaceRequestChannel); !ok {
		return fmt.Errorf("coding dynamic qualified channel must return a verified dispatch")
	}
	// The final serializer must atomically accept the receipt's audit evidence
	// and invocation policy. Separate setters leave a half-configured channel
	// boundary that a future implementation could observe or send through.
	if _, ok := channel.(agent.ToolSurfaceDispatchPreparationRequestChannel); !ok {
		return fmt.Errorf("coding dynamic qualified channel must atomically carry dispatch preparation")
	}
	execution := channel.ExecutionContext()
	if strings.TrimSpace(execution.Protocol) == "" || strings.TrimSpace(execution.ConnectionID) == "" {
		return fmt.Errorf("coding dynamic qualified channel correlation is required")
	}
	if execution.Protocol != certificate.Protocol {
		return fmt.Errorf("coding dynamic qualified channel protocol does not match replacement certificate")
	}
	return nil
}
