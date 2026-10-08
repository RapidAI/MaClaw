package guiapp

import (
	"context"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// The five assertions below deliberately live together. A qualified Coding
// callback must delegate the whole request lifecycle to one relay: reserving a
// live channel, rendering the corresponding durable surface, binding the
// provider response, executing the bound call, and receiving its semantic
// disposition are one composition contract, not five independently enabled
// conveniences.
var (
	_ agent.ToolSurfaceRequestChannelProvider             = (*codingSubAgentCallbacks)(nil)
	_ agent.BoundModelRequestToolSurfaceRenderer          = (*codingSubAgentCallbacks)(nil)
	_ agent.PublishedBoundModelRequestToolSurfaceRenderer = (*codingSubAgentCallbacks)(nil)
	_ agent.ToolSurfaceAuditEvidenceProvider              = (*codingSubAgentCallbacks)(nil)
	_ agent.ToolSurfaceResponseBinder                     = (*codingSubAgentCallbacks)(nil)
	_ agent.ToolCallContextExecutor                       = (*codingSubAgentCallbacks)(nil)
	_ agent.ToolSurfaceDispositionObserver                = (*codingSubAgentCallbacks)(nil)
	_ agent.LLMReplanAware                                = (*codingSubAgentCallbacks)(nil)
	_ agent.LLMFinalizationGuard                          = (*codingSubAgentCallbacks)(nil)

	_ agent.ToolSurfaceRequestChannelProvider             = (*remoteCodingCallbacks)(nil)
	_ agent.BoundModelRequestToolSurfaceRenderer          = (*remoteCodingCallbacks)(nil)
	_ agent.PublishedBoundModelRequestToolSurfaceRenderer = (*remoteCodingCallbacks)(nil)
	_ agent.ToolSurfaceAuditEvidenceProvider              = (*remoteCodingCallbacks)(nil)
	_ agent.ToolSurfaceResponseBinder                     = (*remoteCodingCallbacks)(nil)
	_ agent.ToolCallContextExecutor                       = (*remoteCodingCallbacks)(nil)
	_ agent.ToolSurfaceDispositionObserver                = (*remoteCodingCallbacks)(nil)
	_ agent.LLMReplanAware                                = (*remoteCodingCallbacks)(nil)
	_ agent.LLMFinalizationGuard                          = (*remoteCodingCallbacks)(nil)
)

// dynamicLifecycleRelaySnapshot returns only the callback-owned composition
// relay. It never creates a relay, recovers one from task/transport text, or
// turns a configuration row into execution authority. A nil result preserves
// the existing S0.5 path as one unit.
func (c *codingSubAgentCallbacks) dynamicLifecycleRelaySnapshot() *codingBoundDynamicRequestLifecycleRelay {
	if c == nil {
		return nil
	}
	return c.dynamicLifecycleRelay
}

func (c *remoteCodingCallbacks) dynamicLifecycleRelaySnapshot() *codingBoundDynamicRequestLifecycleRelay {
	if c == nil {
		return nil
	}
	return c.dynamicLifecycleRelay
}

// ReserveToolSurfaceRequestChannel is deliberately the first dynamic
// composition entry point. A nil relay reports nil,nil, which keeps RunLoop on
// the legacy compatibility path; it must not fabricate a channel or partially
// activate the other four interfaces.
func (c *codingSubAgentCallbacks) ReserveToolSurfaceRequestChannel(ctx context.Context, cfg corelib.MaclawLLMConfig) (agent.ToolSurfaceRequestChannel, error) {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		return relay.ReserveToolSurfaceRequestChannel(ctx, cfg)
	}
	return nil, nil
}

func (c *remoteCodingCallbacks) ReserveToolSurfaceRequestChannel(ctx context.Context, cfg corelib.MaclawLLMConfig) (agent.ToolSurfaceRequestChannel, error) {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		return relay.ReserveToolSurfaceRequestChannel(ctx, cfg)
	}
	return nil, nil
}

func (c *codingSubAgentCallbacks) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil && !codingDynamicKillSwitchClosed() {
		return relay.BuildToolsForBoundModelRequest(userText, iteration, execution)
	}
	return nil
}

func (c *remoteCodingCallbacks) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil && !codingDynamicKillSwitchClosed() {
		return relay.BuildToolsForBoundModelRequest(userText, iteration, execution)
	}
	return nil
}

func (c *codingSubAgentCallbacks) RenderPublishedBoundToolSurface(userText string, iteration int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil && !codingDynamicKillSwitchClosed() {
		return relay.RenderPublishedBoundToolSurface(userText, iteration, execution)
	}
	return agent.BoundToolSurfaceRender{Failure: "coding dynamic lifecycle relay is unavailable"}
}

func (c *remoteCodingCallbacks) RenderPublishedBoundToolSurface(userText string, iteration int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil && !codingDynamicKillSwitchClosed() {
		return relay.RenderPublishedBoundToolSurface(userText, iteration, execution)
	}
	return agent.BoundToolSurfaceRender{Failure: "coding dynamic lifecycle relay is unavailable"}
}

// ToolSurfaceAuditEvidence is intentionally unavailable on the static
// compatibility path. Only an active request-owned relay may provide the
// plan/omission facts for the exact reservation it rendered.
func (c *codingSubAgentCallbacks) ToolSurfaceAuditEvidence(execution agent.ToolCallExecutionContext) agent.ToolSurfacePlanEvidence {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		return relay.ToolSurfaceAuditEvidence(execution)
	}
	return agent.ToolSurfacePlanEvidence{}
}

func (c *remoteCodingCallbacks) ToolSurfaceAuditEvidence(execution agent.ToolCallExecutionContext) agent.ToolSurfacePlanEvidence {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		return relay.ToolSurfaceAuditEvidence(execution)
	}
	return agent.ToolSurfacePlanEvidence{}
}

func (c *codingSubAgentCallbacks) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		return relay.BindToolSurfaceResponse(execution)
	}
	return nil
}

func (c *remoteCodingCallbacks) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		return relay.BindToolSurfaceResponse(execution)
	}
	return nil
}

func (c *codingSubAgentCallbacks) OnToolSurfaceDisposition(execution agent.ToolCallExecutionContext, disposition agent.ToolSurfaceDisposition) {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		relay.OnToolSurfaceDisposition(execution, disposition)
	}
}

func (c *remoteCodingCallbacks) OnToolSurfaceDisposition(execution agent.ToolCallExecutionContext, disposition agent.ToolSurfaceDisposition) {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		relay.OnToolSurfaceDisposition(execution, disposition)
	}
}

// codingDynamicCallbackComposition is a test seam for D1. Production callers
// cannot select it: the qualified relay constructor remains host-gated and
// returns nil today. Keeping the composition itself explicit lets regressions
// prove that once it is present, all five loop extensions use the same relay
// and no legacy dispatcher becomes a fallback.
type codingDynamicCallbackComposition interface {
	agent.ToolSurfaceRequestChannelProvider
	agent.BoundModelRequestToolSurfaceRenderer
	agent.PublishedBoundModelRequestToolSurfaceRenderer
	agent.ToolSurfaceResponseBinder
	agent.ToolCallContextExecutor
	agent.ToolSurfaceDispositionObserver
}

func requireCodingDynamicCallbackComposition(candidate any) error {
	if candidate == nil {
		return fmt.Errorf("coding dynamic callback composition is required")
	}
	if _, ok := candidate.(codingDynamicCallbackComposition); !ok {
		return fmt.Errorf("coding dynamic callback composition is incomplete")
	}
	return nil
}

// codingDynamicLifecycleOwner belongs to exactly one CodingSubAgent or
// RemoteCodingSubAgent execution. It bridges host-owned cancellation and
// nested-exit facts to the same relay which owns request-channel disposition.
// It contains no semantic identity, task text, path, runtime ID, or provider
// selector: those inputs must never recreate a relay or a request surface.
//
// The owner is intentionally inert while qualification is disabled because no
// callback has a relay to install. Keeping the lifecycle bridge here makes the
// future D2 wiring atomic with D1 rather than scattering close calls across
// tool dispatchers and return branches.
//
// The state snapshot pointer is written only through sync/atomic helpers. A
// raw unsafe.Pointer (rather than an atomic.Pointer wrapper or a mutex) keeps
// whole-struct copies of CodingSubAgent / RemoteCodingSubAgent free of lock
// values while preserving the field-copy semantics a copied owner shares the
// current snapshot and still closes the shared relay without touching the
// original.
type codingDynamicLifecycleOwner struct {
	state unsafe.Pointer // *codingDynamicLifecycleOwnerState
}

type codingDynamicLifecycleOwnerState struct {
	relay *codingBoundDynamicRequestLifecycleRelay
	stop  context.CancelFunc
}

func (o *codingDynamicLifecycleOwner) loadState() *codingDynamicLifecycleOwnerState {
	if o == nil {
		return nil
	}
	return (*codingDynamicLifecycleOwnerState)(atomic.LoadPointer(&o.state))
}

func (o *codingDynamicLifecycleOwner) install(relay *codingBoundDynamicRequestLifecycleRelay, executionCtx context.Context, loopCtx *LoopContext) {
	if o == nil || relay == nil {
		return
	}
	var previous *codingDynamicLifecycleOwnerState
	var watchCtx context.Context
	var stop context.CancelFunc
	for {
		previous = o.loadState()
		if previous != nil && previous.relay == relay {
			return
		}
		watchCtx, stop = context.WithCancel(context.Background())
		// Lost the race: drop our watcher and retry so a concurrent install of
		// a newer relay is never clobbered.
		if !o.compareAndSwapState(previous, &codingDynamicLifecycleOwnerState{relay: relay, stop: stop}) {
			stop()
			continue
		}
		break
	}
	if previous != nil {
		if previous.stop != nil {
			previous.stop()
		}
		if previous.relay != nil {
			previous.relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		}
	}

	// Both sources are host-owned terminal facts. A detached child is stopped
	// by executionCtx while a normal task is stopped by LoopContext; neither is
	// inferred from model output or a transport label.
	var loopDone <-chan struct{}
	if loopCtx != nil {
		loopDone = loopCtx.CancelC
	}
	var executionDone <-chan struct{}
	if executionCtx != nil {
		executionDone = executionCtx.Done()
	}
	go func() {
		select {
		case <-watchCtx.Done():
			return
		case <-loopDone:
		case <-executionDone:
		}
		relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
	}()
}

func (o *codingDynamicLifecycleOwner) close(reason codingBoundDynamicRequestTerminalReason) {
	if o == nil {
		return
	}
	state := (*codingDynamicLifecycleOwnerState)(atomic.SwapPointer(&o.state, nil))
	if state == nil {
		return
	}
	if state.stop != nil {
		state.stop()
	}
	if state.relay != nil {
		state.relay.CloseForLifecycle(reason)
	}
}

func (o *codingDynamicLifecycleOwner) clear(relay *codingBoundDynamicRequestLifecycleRelay, reason codingBoundDynamicRequestTerminalReason) {
	if o == nil || relay == nil {
		return
	}
	for {
		state := o.loadState()
		if state == nil || state.relay != relay {
			return
		}
		if !o.compareAndSwapState(state, nil) {
			continue
		}
		if state.stop != nil {
			state.stop()
		}
		relay.CloseForLifecycle(reason)
		return
	}
}

func (o *codingDynamicLifecycleOwner) compareAndSwapState(old, new *codingDynamicLifecycleOwnerState) bool {
	return atomic.CompareAndSwapPointer(&o.state, unsafe.Pointer(old), unsafe.Pointer(new))
}

func (c *codingSubAgentCallbacks) registerDynamicLifecycleOwner() {
	if c == nil || c.subagent == nil {
		return
	}
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		c.subagent.dynamicLifecycleOwner.install(relay, c.subagent.executionCtx, c.subagent.loopCtx)
	}
}

func (c *remoteCodingCallbacks) registerDynamicLifecycleOwner() {
	if c == nil || c.agent == nil {
		return
	}
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		c.agent.dynamicLifecycleOwner.install(relay, c.agent.executionCtx, c.agent.loopCtx)
	}
}

func (c *codingSubAgentCallbacks) closeDynamicLifecycleOwner(reason codingBoundDynamicRequestTerminalReason) {
	if c == nil || c.subagent == nil {
		return
	}
	c.subagent.dynamicLifecycleOwner.clear(c.dynamicLifecycleRelaySnapshot(), reason)
}

func (c *remoteCodingCallbacks) closeDynamicLifecycleOwner(reason codingBoundDynamicRequestTerminalReason) {
	if c == nil || c.agent == nil {
		return
	}
	c.agent.dynamicLifecycleOwner.clear(c.dynamicLifecycleRelaySnapshot(), reason)
}

// closeCodingSubAgentDynamicLifecycle closes the execution-scoped owner, not
// merely the callback field. It is used by host terminal paths where the
// callback may already be unwinding. The owner performs an exact relay match
// so an old task cannot close a successor's reservation.
func (s *CodingSubAgent) closeCodingSubAgentDynamicLifecycle(reason codingBoundDynamicRequestTerminalReason) {
	if s != nil {
		s.dynamicLifecycleOwner.close(reason)
	}
}

func (r *RemoteCodingSubAgent) closeCodingSubAgentDynamicLifecycle(reason codingBoundDynamicRequestTerminalReason) {
	if r != nil {
		r.dynamicLifecycleOwner.close(reason)
	}
}
