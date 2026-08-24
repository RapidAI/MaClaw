package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingBoundDynamicRequestAdapter is the test-only S1-C composition root for
// one already-reserved request channel. It intentionally is not embedded in a
// CodingSubAgent or RemoteCodingSubAgent callback: production wiring remains
// disabled until this holder's lifecycle and failure closure are qualified.
//
// The holder owns the only allowed path from a channel reservation to a
// dynamic model surface and back to the fixed G4 bridge. In particular, it
// contains no match map or name-addressed provider dispatcher.
type codingBoundDynamicRequestAdapter struct {
	mu sync.Mutex

	handler  *IMMessageHandler
	identity *trustedCodingInvocationIdentity
	prepared codingDynamicPlanPreparation
	dynamic  codingDynamicCatalogSnapshot

	protocol   string
	connection string
	epoch      string
	surface    *codingDurableDynamicSurface
	terminal   bool
	// terminalDurabilityErr records a failed durable retirement after this
	// process has already fenced the holder. It must never be silently treated
	// as a normal stale surface: restart recovery would otherwise see the old
	// active request presentation with no durable terminal fact.
	terminalDurabilityErr error
	channel               agent.ToolSurfaceRequestChannel
	// channelClosed is a holder-local resource fence. RunLoop's compatibility
	// cleanup can follow a holder-owned error retirement, and lifecycle closure
	// can race either path. Only the holder that owns this reservation may
	// release the underlying transport, and it does so once after its semantic
	// terminal state has been selected.
	channelClosed bool
	// invocationPolicy is the immutable manifest-authorized callable control
	// for this reservation. Retaining it at the holder prevents a direct
	// holder dispatch (or a future wrapper) from relying on a lower transport's
	// accidental provider-default behavior.
	invocationPolicy    agent.ToolSurfaceInvocationPolicy
	invocationPolicySet bool
	// preparationInFlight serializes the one lower-channel operation which
	// freezes audit evidence and invocation policy together. dispatchPreparing
	// tracks DoVerified's broader handoff phase; it is not sufficient here
	// because an external RunLoop preparer may call this method before dispatch
	// begins. Without this separate fence, two callers could both observe an
	// unset policy and race to configure the lower channel.
	preparationInFlight bool
	dispatchAttempted   bool
	dispatchPreparing   bool
	// executionCtx is cancelled before durable route retirement. It is passed to
	// the fixed bridge so a close/steer race cannot continue provider I/O merely
	// because it crossed alias admission just before Close acquired the mutex.
	executionCtx    context.Context
	cancelExecution context.CancelFunc
}

// codingBoundDynamicToolCall is a request-local execution ticket. It contains
// only references frozen at the holder's execution-admission linearization
// point; it is not a grant, alias, retry token, or reusable capability.
// Lifecycle cancellation remains authoritative through executionCtx after the
// ticket is issued.
type codingBoundDynamicToolCall struct {
	surface      *codingDurableDynamicSurface
	identity     *trustedCodingInvocationIdentity
	dynamic      codingDynamicCatalogSnapshot
	handler      *IMMessageHandler
	executionCtx context.Context
	execution    agent.ToolCallExecutionContext
	name         string
	argsJSON     string
	callID       string
}

var _ agent.ToolSurfacePublicationProofRequirement = (*codingBoundDynamicRequestAdapter)(nil)

// codingBoundDynamicRequestTerminalReason is host lifecycle evidence only. It
// is never derived from a model result, tool name, task text, runtime ID, or
// transport configuration. Keeping the reason separate makes future callback
// lifecycle wiring use the same durable closure path for steering, nested
// worker exit, and runtime terminal state.
type codingBoundDynamicRequestTerminalReason string

const (
	codingBoundDynamicRequestSteered    codingBoundDynamicRequestTerminalReason = "steered"
	codingBoundDynamicRequestNestedExit codingBoundDynamicRequestTerminalReason = "nested_exit"
	// RouteSuperseded is a host-owned replacement fact: a successor route has
	// become current before this reservation could settle. It is distinct from a
	// transport error, but closes through the same cancellation transaction.
	codingBoundDynamicRequestRouteSuperseded   codingBoundDynamicRequestTerminalReason = "route_superseded"
	codingBoundDynamicRequestRuntimeClosed     codingBoundDynamicRequestTerminalReason = "runtime_terminal"
	codingBoundDynamicRequestTransportFail     codingBoundDynamicRequestTerminalReason = "transport_failure"
	codingBoundDynamicRequestResponseAbandoned codingBoundDynamicRequestTerminalReason = "response_abandoned"
	codingBoundDynamicRequestResponseSettled   codingBoundDynamicRequestTerminalReason = "response_settled"
	codingBoundDynamicRequestToolBatchSettled  codingBoundDynamicRequestTerminalReason = "tool_batch_settled"
)

// codingBoundDynamicRequestAdapterFactory is deliberately injectable only by
// focused tests while the production qualification registry remains disabled.
// The factory receives already-resolved host identity and handler; it receives
// neither task text nor runtime IDs, provider matches, paths, or model input.
// A future production factory must be selected by an app-owned qualification
// result, never by an LLM configuration label alone.
type codingBoundDynamicRequestAdapterFactory func(context.Context, *IMMessageHandler, *trustedCodingInvocationIdentity, corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error)

// newCodingBoundDynamicRequestAdapterForChannel binds the holder to the one
// live transport reservation that will carry its rendered definitions. It is
// deliberately a test-only construction seam: no current Coding callback
// returns this object from ReserveToolSurfaceRequestChannel.
func newCodingBoundDynamicRequestAdapterForChannel(handler *IMMessageHandler, identity *trustedCodingInvocationIdentity, prepared codingDynamicPlanPreparation, dynamic codingDynamicCatalogSnapshot, channel agent.ToolSurfaceRequestChannel) (*codingBoundDynamicRequestAdapter, error) {
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, fmt.Errorf("coding dynamic request channel is required")
	}
	execution := channel.ExecutionContext()
	if strings.TrimSpace(execution.Protocol) == "" || strings.TrimSpace(execution.ConnectionID) == "" {
		return nil, fmt.Errorf("coding dynamic request channel correlation is required")
	}
	adapter.channel = channel
	return adapter, nil
}

// ExecutionContext and Do make the holder itself a single-use request channel.
// Thus the same object is suitable for the future RunLoop provider/renderer/
// binder/executor composition without allowing a second transport helper to
// send the definitions it just rendered.
func (a *codingBoundDynamicRequestAdapter) ExecutionContext() agent.ToolCallExecutionContext {
	if a == nil || a.channel == nil {
		return agent.ToolCallExecutionContext{}
	}
	return a.channel.ExecutionContext()
}

// RequiresPublishedBoundToolSurface marks this holder's live reservation as a
// durable dynamic surface. RunLoop must therefore require the proof-carrying
// renderer and may not downgrade a holder/relay publication failure to the
// legacy []definition renderer contract.
func (*codingBoundDynamicRequestAdapter) RequiresPublishedBoundToolSurface() bool { return true }

func (a *codingBoundDynamicRequestAdapter) Do(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (*llm.Response, error) {
	dispatch, err := a.DoVerified(ctx, conversation, tools, onToken, stream)
	return dispatch.Response, err
}

// DoVerified preserves the holder's single request ownership while forwarding
// the receipt produced by its concrete transport. A future qualified Coding
// path cannot fall back to the legacy Do-only channel seam.
func (a *codingBoundDynamicRequestAdapter) DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (agent.VerifiedToolSurfaceDispatch, error) {
	if a == nil || a.channel == nil {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("coding dynamic request channel is unavailable")
	}
	a.mu.Lock()
	if a.dispatchAttempted || a.dispatchPreparing {
		a.mu.Unlock()
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("coding dynamic request channel already used")
	}
	ready := !a.terminal && a.surface != nil
	policySet := a.invocationPolicySet
	a.dispatchPreparing = true
	a.mu.Unlock()
	if !ready {
		a.mu.Lock()
		a.dispatchPreparing = false
		a.mu.Unlock()
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("coding dynamic request surface is not prepared")
	}
	// The lifecycle owner cancels this request-local context before it writes a
	// terminal durable fact.  Use the same context for transport handoff, so a
	// close that wins while audit/policy preparation is in flight cannot leave
	// DoVerified sending through an otherwise independent caller context.
	a.mu.Lock()
	executionCtx := a.executionCtx
	a.mu.Unlock()
	if executionCtx == nil || executionCtx.Err() != nil {
		a.mu.Lock()
		a.dispatchPreparing = false
		a.mu.Unlock()
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("stale_surface")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := mergeCodingDynamicRequestContexts(ctx, executionCtx)
	defer cancel()
	if !policySet {
		a.mu.Lock()
		a.dispatchPreparing = false
		a.mu.Unlock()
		err := fmt.Errorf("surface_integrity_failure: coding dynamic invocation policy was not set")
		a.Close(err)
		return agent.VerifiedToolSurfaceDispatch{}, err
	}
	verified, ok := a.channel.(agent.VerifiedToolSurfaceRequestChannel)
	if !ok {
		err := fmt.Errorf("surface_integrity_failure: coding dynamic transport does not return a verified dispatch")
		a.Close(err)
		return agent.VerifiedToolSurfaceDispatch{}, err
	}
	// The adapter, rather than RunLoop's callback wrapper, owns the only
	// executable channel beneath this request-local surface. Require that
	// channel to accept the exact plan evidence it can independently derive;
	// otherwise a future wrapper could substitute a receipt for another plan.
	evidence := a.ToolSurfaceAuditEvidence(a.ExecutionContextWithEpoch())
	if !evidence.Available {
		err := fmt.Errorf("surface_integrity_failure: coding dynamic audit evidence is unavailable")
		a.Close(err)
		return agent.VerifiedToolSurfaceDispatch{}, err
	}
	// RunLoop normally installed the complete setup before entering DoVerified.
	// Direct holder callers still use the same atomic path; neither audit-only
	// nor policy-only transport preparation is accepted here.
	a.mu.Lock()
	policy := a.invocationPolicy
	a.mu.Unlock()
	if err := a.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: evidence, InvocationPolicy: policy}); err != nil && !isToolSurfaceDispatchPreparationAlreadySet(err) {
		a.mu.Lock()
		a.dispatchPreparing = false
		a.mu.Unlock()
		a.Close(err)
		return agent.VerifiedToolSurfaceDispatch{}, err
	}
	// Setting dispatch preparation only freezes the lower transport's immutable
	// frame.  It is not itself permission to begin I/O: a lifecycle terminal can
	// win while that external setter is running.  Re-check the holder fence at
	// the last in-process linearization point before handing the context to the
	// transport.  Without this check, a channel that does not independently
	// reject an already-cancelled context could be called after the route was
	// durably retired.
	a.mu.Lock()
	if a.terminal || a.surface == nil || a.executionCtx == nil || a.executionCtx.Err() != nil {
		a.dispatchPreparing = false
		a.mu.Unlock()
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("stale_surface")
	}
	a.dispatchPreparing = false
	a.dispatchAttempted = true
	a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("stale_surface: request lifecycle cancelled: %w", err)
	}
	dispatch, err := verified.DoVerified(ctx, conversation, tools, onToken, stream)
	if err != nil {
		a.Close(err)
	}
	return dispatch, err
}

// mergeCodingDynamicRequestContexts preserves the caller's deadline/value
// domain while making the holder's lifecycle fence an equally authoritative
// cancellation source. It does not create a new timeout or retry authority.
func mergeCodingDynamicRequestContexts(caller, lifecycle context.Context) (context.Context, context.CancelFunc) {
	if lifecycle == nil {
		return context.WithCancel(caller)
	}
	ctx, cancel := context.WithCancel(caller)
	stop := context.AfterFunc(lifecycle, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func isToolSurfaceDispatchPreparationAlreadySet(err error) bool {
	// A second holder DoVerified is never permitted to send. Treat an already
	// consumed one-shot channel as proof that the first attempt owned setup, then
	// let DoVerified return its canonical already-used failure.
	return err != nil && (strings.Contains(err.Error(), "dispatch preparation set after channel use") || strings.Contains(err.Error(), "dispatch preparation set after dispatch attempt"))
}

// ExecutionContextWithEpoch is the immutable reservation tuple after the
// bound renderer published its surface. It is intentionally private to the
// holder; callers receive only ExecutionContext from the real channel and
// RunLoop supplies the epoch for each request.
func (a *codingBoundDynamicRequestAdapter) ExecutionContextWithEpoch() agent.ToolCallExecutionContext {
	if a == nil {
		return agent.ToolCallExecutionContext{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return agent.ToolCallExecutionContext{Protocol: a.protocol, ConnectionID: a.connection, SurfaceEpoch: a.epoch}
}

// SetToolSurfaceDispatchPreparation is the holder's atomic handoff boundary.
// It validates that audit evidence belongs to this durable reservation and
// then forwards the exact normalized pair to the one concrete transport.
func (a *codingBoundDynamicRequestAdapter) SetToolSurfaceDispatchPreparation(preparation agent.ToolSurfaceDispatchPreparation) error {
	if a == nil || a.channel == nil {
		return fmt.Errorf("surface_integrity_failure: coding dynamic request channel is unavailable")
	}
	evidence, err := agent.NormalizeToolSurfacePlanEvidence(preparation.AuditEvidence)
	if err != nil {
		return fmt.Errorf("surface_integrity_failure: invalid coding dynamic audit evidence: %w", err)
	}
	policy, err := agent.NormalizeToolSurfaceInvocationPolicy(preparation.InvocationPolicy)
	if err != nil {
		return fmt.Errorf("surface_integrity_failure: invalid coding dynamic invocation policy: %w", err)
	}
	a.mu.Lock()
	if a.terminal || a.surface == nil || a.dispatchAttempted || a.preparationInFlight || (a.dispatchPreparing && !a.invocationPolicySet) {
		a.mu.Unlock()
		return fmt.Errorf("surface_integrity_failure: coding dynamic dispatch preparation is unavailable")
	}
	expected := a.toolSurfaceAuditEvidenceLocked()
	if !expected.Available || !sameToolSurfacePlanEvidence(expected, evidence) {
		a.mu.Unlock()
		return fmt.Errorf("surface_integrity_failure: coding dynamic audit evidence does not match current reservation")
	}
	if a.invocationPolicySet && a.invocationPolicy != policy {
		a.mu.Unlock()
		return fmt.Errorf("surface_integrity_failure: coding dynamic invocation policy changed")
	}
	if a.invocationPolicySet {
		// The exact pair has already crossed the lower channel's atomic setup
		// boundary. Do not issue a second setter call merely because a direct
		// holder caller repeated an idempotent preparation request.
		a.mu.Unlock()
		return nil
	}
	a.preparationInFlight = true
	a.mu.Unlock()
	setter, ok := a.channel.(agent.ToolSurfaceDispatchPreparationRequestChannel)
	if !ok {
		a.mu.Lock()
		a.preparationInFlight = false
		a.mu.Unlock()
		return a.failDispatchPreparation(fmt.Errorf("surface_integrity_failure: coding dynamic transport cannot atomically carry dispatch preparation"))
	}
	if err := setter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: expected, InvocationPolicy: policy}); err != nil {
		a.mu.Lock()
		a.preparationInFlight = false
		a.mu.Unlock()
		// A lower setter error is not proof that it left no state behind: a
		// transport may have frozen either portion of the immutable frame before
		// discovering its own failure. Retrying this request could therefore send
		// a partially configured or ambiguously owned surface. Retire the exact
		// holder before exposing the error so a fresh request is the only possible
		// recovery path.
		return a.failDispatchPreparation(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.preparationInFlight = false
	if a.terminal || a.surface == nil || a.dispatchAttempted {
		return fmt.Errorf("surface_integrity_failure: coding dynamic dispatch preparation changed during setup")
	}
	a.invocationPolicy, a.invocationPolicySet = policy, true
	return nil
}

// failDispatchPreparation closes the request authority after a failure at the
// atomic lower-channel setup boundary. The returned error retains any durable
// retirement failure, but callers never regain this holder merely because the
// concrete channel did not explain whether it changed its internal frame.
func (a *codingBoundDynamicRequestAdapter) failDispatchPreparation(cause error) error {
	if cause == nil {
		cause = fmt.Errorf("surface_integrity_failure: coding dynamic dispatch preparation failed")
	}
	if retireErr := a.closeAndRetire(cause); retireErr != nil {
		return fmt.Errorf("%w; durable coding dynamic route retirement failed: %v", cause, retireErr)
	}
	return cause
}

func sameToolSurfacePlanEvidence(left, right agent.ToolSurfacePlanEvidence) bool {
	left, leftErr := agent.NormalizeToolSurfacePlanEvidence(left)
	right, rightErr := agent.NormalizeToolSurfacePlanEvidence(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if left.Available != right.Available || left.PlanID != right.PlanID || left.PlanSnapshotDigest != right.PlanSnapshotDigest || left.CatalogGeneration != right.CatalogGeneration || len(left.Omitted) != len(right.Omitted) {
		return false
	}
	for i := range left.Omitted {
		if left.Omitted[i] != right.Omitted[i] {
			return false
		}
	}
	return true
}

// ToolSurfaceAuditEvidence returns only immutable planning facts for the
// already-rendered request reservation. It never exposes definitions, aliases,
// grants, transport identity, or any predecessor state.
func (a *codingBoundDynamicRequestAdapter) ToolSurfaceAuditEvidence(execution agent.ToolCallExecutionContext) agent.ToolSurfacePlanEvidence {
	if a == nil {
		return agent.ToolSurfacePlanEvidence{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.surface == nil || !a.matchesReservationLocked(execution) {
		return agent.ToolSurfacePlanEvidence{}
	}
	return a.toolSurfaceAuditEvidenceLocked()
}

func (a *codingBoundDynamicRequestAdapter) toolSurfaceAuditEvidenceLocked() agent.ToolSurfacePlanEvidence {
	omitted := make([]agent.ToolSurfaceOmission, 0, len(a.prepared.Plan.Omitted))
	for _, item := range a.prepared.Plan.Omitted {
		omitted = append(omitted, agent.ToolSurfaceOmission{NeedID: item.NeedID, ReasonCode: item.ReasonCode})
	}
	return agent.ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             a.prepared.Plan.ID,
		PlanSnapshotDigest: a.prepared.Plan.SnapshotDigest,
		CatalogGeneration:  a.prepared.Plan.CatalogGeneration,
		Omitted:            omitted,
	}
}

func newCodingBoundDynamicRequestAdapter(handler *IMMessageHandler, identity *trustedCodingInvocationIdentity, prepared codingDynamicPlanPreparation, dynamic codingDynamicCatalogSnapshot) (*codingBoundDynamicRequestAdapter, error) {
	if handler == nil || handler.app == nil || identity == nil || !identity.complete() || !dynamic.complete() {
		return nil, fmt.Errorf("coding dynamic adapter prerequisites are incomplete")
	}
	if strings.TrimSpace(prepared.Plan.ID) == "" || len(prepared.Plan.Unmet) != 0 || len(prepared.Plan.Selections) == 0 {
		return nil, fmt.Errorf("coding dynamic adapter plan is incomplete")
	}
	executionCtx, cancelExecution := context.WithCancel(context.Background())
	return &codingBoundDynamicRequestAdapter{
		handler: handler, identity: identity, prepared: prepared, dynamic: dynamic,
		executionCtx: executionCtx, cancelExecution: cancelExecution,
	}, nil
}

// RenderPublishedBoundToolSurface publishes a prepared durable surface only
// against the exact channel reservation passed by RunLoop. Its Published bit
// explicitly distinguishes a legitimate empty replacement from a failed
// durable publication. Alias resolution remains impossible until
// BindToolSurfaceResponse succeeds.
func (a *codingBoundDynamicRequestAdapter) RenderPublishedBoundToolSurface(_ string, _ int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if a == nil {
		return agent.BoundToolSurfaceRender{Failure: "coding dynamic request adapter is unavailable"}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.surface != nil || !a.acceptReservationLocked(execution) {
		return agent.BoundToolSurfaceRender{Failure: "coding dynamic request reservation is stale"}
	}
	surface, err := a.handler.app.publishCodingDurableDynamicSurfaceForEpoch(a.identity, a.prepared, a.dynamic, a.protocol, a.connection, a.epoch, time.Now().UTC())
	if err != nil {
		// A publisher error is not proof that no durable route/surface was ever
		// written: PublishSurface can commit before later presentation work fails.
		// The publisher attempts coordinator-owned cancellation on that path, but
		// the holder cannot independently verify its outcome without reopening a
		// second authority channel. Latch the failure so the relay denies a
		// successor until the process is restarted/reconciled.
		// Publication has crossed the durable coordinator boundary, so its
		// failure is terminally ambiguous. Revoke every in-process execution
		// ticket before exposing that terminal state to RunLoop; a later
		// disposition must only complete cleanup, never be the first lifecycle
		// cancellation fence.
		if a.cancelExecution != nil {
			a.cancelExecution()
		}
		a.terminal = true
		a.terminalDurabilityErr = fmt.Errorf("publish coding dynamic request surface: %w", err)
		return agent.BoundToolSurfaceRender{Failure: fmt.Sprintf("publish coding dynamic request surface: %v", err)}
	}
	definitions, err := cloneCodingDynamicDefinitions(surface.definitions)
	if err != nil {
		// The route was durably published before the presentation clone.  Its
		// cancellation is therefore part of this failure's terminal contract, not
		// best-effort cleanup.  If it cannot be committed, latch the error so the
		// relay closes successor admission instead of leaving an active durable
		// request surface behind after RunLoop's pre-dispatch failure.
		// The clone failure happens after PublishSurface has made route authority
		// visible. Cancel the request execution context before attempting its
		// durable retirement, matching every other terminal path.
		if a.cancelExecution != nil {
			a.cancelExecution()
		}
		retireErr := surface.Cancel(time.Now().UTC())
		a.terminal = true
		if retireErr != nil {
			a.terminalDurabilityErr = fmt.Errorf("copy coding dynamic request definitions: %v; durable route cancellation failed: %w", err, retireErr)
		}
		return agent.BoundToolSurfaceRender{Failure: fmt.Sprintf("copy coding dynamic request definitions: %v", err)}
	}
	a.surface = surface
	return agent.BoundToolSurfaceRender{Definitions: definitions, Published: true}
}

// BuildToolsForBoundModelRequest preserves the older renderer seam for direct
// callers. RunLoop uses RenderPublishedBoundToolSurface when available, so it
// never infers publication success from len(definitions).
func (a *codingBoundDynamicRequestAdapter) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	return a.RenderPublishedBoundToolSurface(userText, iteration, execution).Definitions
}

// BindToolSurfaceResponse binds the provider-issued response ID before any
// dynamic call may enter alias resolution or admission. A failed bind retires
// the route surface immediately so no executable alias remains in memory or
// durable state.
func (a *codingBoundDynamicRequestAdapter) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	if a == nil {
		return fmt.Errorf("stale_surface")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.surface == nil {
		if a.terminalDurabilityErr != nil {
			return fmt.Errorf("surface_integrity_failure: coding dynamic surface retirement was not durable: %w", a.terminalDurabilityErr)
		}
		return fmt.Errorf("stale_surface")
	}
	if !a.matchesReservationLocked(execution) {
		if retireErr := a.retireLocked(); retireErr != nil {
			return fmt.Errorf("surface_integrity_failure: retire stale coding dynamic surface: %w", retireErr)
		}
		return fmt.Errorf("stale_surface")
	}
	if strings.TrimSpace(execution.ResponseID) == "" {
		if retireErr := a.retireLocked(); retireErr != nil {
			return fmt.Errorf("surface_integrity_failure: retire unbound coding dynamic surface: %w", retireErr)
		}
		return fmt.Errorf("stale_surface")
	}
	if err := a.surface.BindResponse(execution.ResponseID, time.Now().UTC()); err != nil {
		if retireErr := a.retireLocked(); retireErr != nil {
			return fmt.Errorf("surface_integrity_failure: retire failed coding dynamic binding: %w", retireErr)
		}
		return fmt.Errorf("stale_surface")
	}
	return nil
}

// ExecuteToolCallWithContext is the fixed G4 dispatch boundary for this one
// reservation. A mismatch must fail before observing the provider catalog or
// consuming a grant.
func (a *codingBoundDynamicRequestAdapter) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	call, ok := a.beginBoundToolCall(name, argsJSON, callID, execution)
	if !ok {
		return rejectedCodingDynamicAdapterResult("stale_surface")
	}
	return executeCodingBoundToolCall(call)
}

// beginBoundToolCall is the holder-local admission half of G4. The relay calls
// it while holding its lifecycle mutex, which makes it the linearization point
// between a new tool-call attempt and a terminal transition. It deliberately
// performs no catalog observation, coordinator transaction, or provider I/O
// while either mutex is held.
func (a *codingBoundDynamicRequestAdapter) beginBoundToolCall(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) (codingBoundDynamicToolCall, bool) {
	if a == nil {
		return codingBoundDynamicToolCall{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.surface == nil || strings.TrimSpace(execution.ResponseBindingError) != "" || strings.TrimSpace(callID) == "" || !a.matchesBoundExecutionLocked(execution) {
		return codingBoundDynamicToolCall{}, false
	}
	if a.executionCtx == nil || a.executionCtx.Err() != nil {
		return codingBoundDynamicToolCall{}, false
	}
	return codingBoundDynamicToolCall{
		surface: a.surface, identity: a.identity, dynamic: a.dynamic, handler: a.handler, executionCtx: a.executionCtx,
		execution: execution, name: name, argsJSON: argsJSON, callID: callID,
	}, true
}

func executeCodingBoundToolCall(call codingBoundDynamicToolCall) agent.ToolExecutionResult {
	if call.surface == nil || call.executionCtx == nil || call.executionCtx.Err() != nil {
		return rejectedCodingDynamicAdapterResult("stale_surface")
	}
	result := call.surface.ExecuteBoundSelection(call.executionCtx, call.identity, call.dynamic, call.handler, call.execution.Protocol, call.execution.ConnectionID, call.execution.ResponseID, call.callID, call.name, call.argsJSON, time.Now().UTC())
	return codingDynamicAdapterResult(result)
}

// Close is called by the request owner on terminal transport outcomes. A nil
// cause means the provider response was returned to RunLoop and must remain
// bindable; a non-nil cause retires the prepared/active surface. The channel
// itself may release its socket after a successful Do, but that is not a
// semantic cancellation of the already received response.
func (a *codingBoundDynamicRequestAdapter) Close(cause error) {
	if a == nil {
		return
	}
	if cause == nil {
		// A successful transport handoff is not terminal authority. In
		// particular, do not let the channel's post-dispatch cleanup race a
		// lifecycle close and independently close a socket while the relay still
		// owns the durable terminal transition. The final disposition will close
		// it after Finish/Cancel has been linearized.
		return
	}
	_ = a.closeAndRetire(cause)
}

// closeAndRetire is the error-carrying half of Close. ToolSurfaceRequestChannel
// keeps Close diagnostic-only for compatibility, but lifecycle owners use this
// method so durable failure cannot be collapsed into an ordinary terminal.
func (a *codingBoundDynamicRequestAdapter) closeAndRetire(cause error) error {
	if a == nil {
		return nil
	}
	// Direct lifecycle callers use this path rather than Close, so preserve the
	// same ordering: an admitted bridge must observe cancellation before the
	// durable route-retirement transaction begins.
	if a.cancelExecution != nil {
		a.cancelExecution()
	}
	a.mu.Lock()
	retireErr := a.retireLocked()
	channel := a.closeChannelLocked()
	a.mu.Unlock()
	if retireErr != nil {
		cause = fmt.Errorf("%w; durable coding dynamic route retirement failed: %v", cause, retireErr)
	}
	if channel != nil {
		channel.Close(cause)
	}
	return retireErr
}

// CloseForLifecycle is the only lifecycle-to-adapter transition intended for
// future Coding callback wiring. Its explicit, closed reason set prevents a
// callback from treating arbitrary task or transport text as cancellation
// authority. A durably settled response is not a route cancellation: it
// retires only this request presentation, preserving the current route for a
// correctly prepared successor. All other lifecycle reasons fail closed by
// cancelling the complete route authority.
func (a *codingBoundDynamicRequestAdapter) CloseForLifecycle(reason codingBoundDynamicRequestTerminalReason) error {
	switch reason {
	case codingBoundDynamicRequestResponseSettled,
		codingBoundDynamicRequestToolBatchSettled:
		return a.finishSettledRequest()
	case codingBoundDynamicRequestSteered,
		codingBoundDynamicRequestNestedExit,
		codingBoundDynamicRequestRouteSuperseded,
		codingBoundDynamicRequestRuntimeClosed,
		codingBoundDynamicRequestTransportFail,
		codingBoundDynamicRequestResponseAbandoned:
		return a.closeAndRetire(fmt.Errorf("coding dynamic request terminal: %s", reason))
	default:
		// Unknown lifecycle input is fail-closed. It has no semantic effect
		// beyond retiring this holder's already-published route surface.
		return a.closeAndRetire(fmt.Errorf("coding dynamic request terminal: unknown"))
	}
}

// terminalDurabilityError returns a previously latched persistence failure
// without changing request state. It is used by the relay when RunLoop sends
// its one disposition after a renderer or channel path already fenced the
// holder. A second generic close must never overwrite that evidence.
func (a *codingBoundDynamicRequestAdapter) terminalDurabilityError() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.terminalDurabilityErr
}

// finishSettledRequest marks the in-memory holder terminal before releasing
// its channel, then durably retires just the request surface. If that durable
// retirement cannot be written, cancellation is the only safe fallback: a
// process restart must not recover a still-active alias from a response that
// the loop already declared settled.
func (a *codingBoundDynamicRequestAdapter) finishSettledRequest() error {
	if a == nil {
		return nil
	}
	if a.cancelExecution != nil {
		a.cancelExecution()
	}
	a.mu.Lock()
	if a.terminal {
		retireErr := a.terminalDurabilityErr
		a.mu.Unlock()
		return retireErr
	}
	a.terminal = true
	var retireErr error
	if a.surface != nil {
		retireErr = a.surface.Finish(time.Now().UTC())
		if retireErr != nil {
			// Do not leave a durable active surface when we failed to mark it
			// finished. Cancellation also revokes any unspent route authority.
			if cancelErr := a.surface.Cancel(time.Now().UTC()); cancelErr != nil {
				retireErr = fmt.Errorf("finish request surface: %v; durable route cancellation fallback: %w", retireErr, cancelErr)
			}
			a.terminalDurabilityErr = retireErr
		}
	}
	channel := a.closeChannelLocked()
	a.mu.Unlock()
	if channel != nil {
		channel.Close(retireErr)
	}
	return retireErr
}

func (a *codingBoundDynamicRequestAdapter) acceptReservationLocked(execution agent.ToolCallExecutionContext) bool {
	if strings.TrimSpace(execution.Protocol) == "" || strings.TrimSpace(execution.ConnectionID) == "" || strings.TrimSpace(execution.SurfaceEpoch) == "" {
		return false
	}
	if a.protocol != "" || a.connection != "" || a.epoch != "" {
		return false
	}
	if a.channel != nil {
		reserved := a.channel.ExecutionContext()
		if execution.Protocol != reserved.Protocol || execution.ConnectionID != reserved.ConnectionID {
			return false
		}
	}
	a.protocol, a.connection, a.epoch = execution.Protocol, execution.ConnectionID, execution.SurfaceEpoch
	return true
}

func (a *codingBoundDynamicRequestAdapter) matchesBoundExecutionLocked(execution agent.ToolCallExecutionContext) bool {
	return a.matchesReservationLocked(execution) && strings.TrimSpace(execution.ResponseID) != ""
}

func (a *codingBoundDynamicRequestAdapter) matchesReservationLocked(execution agent.ToolCallExecutionContext) bool {
	return strings.TrimSpace(execution.Protocol) != "" && strings.TrimSpace(execution.ConnectionID) != "" && strings.TrimSpace(execution.SurfaceEpoch) != "" &&
		execution.Protocol == a.protocol && execution.ConnectionID == a.connection && execution.SurfaceEpoch == a.epoch
}

func (a *codingBoundDynamicRequestAdapter) retireLocked() error {
	// Every cancellation-style terminal path converges here, including bind
	// failures and future holder-local failure branches. Keep the lifecycle
	// fence adjacent to the terminal transition instead of relying on each
	// caller to remember it; a durable route cancellation must never begin
	// while an issued execution ticket can still regard its context as live.
	// Callers hold a.mu. context cancellation is idempotent and does not acquire
	// this mutex, so it is safe to establish this fence before the durable I/O.
	if a.cancelExecution != nil {
		a.cancelExecution()
	}
	if a.terminal {
		return a.terminalDurabilityErr
	}
	a.terminal = true
	if a.surface != nil {
		if err := a.surface.Cancel(time.Now().UTC()); err != nil {
			a.terminalDurabilityErr = err
			return err
		}
	}
	return nil
}

// closeChannelLocked transfers the one underlying transport cleanup right to
// this terminal path. Callers must hold a.mu. A terminal can be observed by
// several owners (dispatch error, RunLoop cleanup, and lifecycle disposition),
// but channel resource release is not a second semantic terminal authority and
// must not be invoked more than once.
func (a *codingBoundDynamicRequestAdapter) closeChannelLocked() agent.ToolSurfaceRequestChannel {
	if a.channelClosed || a.channel == nil {
		return nil
	}
	a.channelClosed = true
	return a.channel
}

func rejectedCodingDynamicAdapterResult(reason string) agent.ToolExecutionResult {
	return agent.ToolExecutionResult{Result: "[system rejected] " + reason, Outcome: agent.ToolExecutionOutcomeError}
}

func codingDynamicAdapterResult(result tool.SelectionExecutionResult) agent.ToolExecutionResult {
	outcome := agent.ToolExecutionOutcomeError
	if result.Succeeded {
		outcome = agent.ToolExecutionOutcomeOK
	}
	return agent.ToolExecutionResult{Result: result.Result, Outcome: outcome}
}
