package agent

// loop.go defines the shared agent loop that both GUI and TUI use.
// The loop is parameterized by callback interfaces so it doesn't depend
// on any gui/ types directly.
//
// This is the mechanism that eliminates the duplicated RunAgentLoop in TUI.
// GUI and TUI each provide their own implementations of the callbacks,
// but the loop logic (LLM call → tool execution → repeat) is written once.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// TurnRouter is an optional LoopCallbacks extension for per-turn model routing.
// When implemented, RunLoop calls RouteTurn once at loop start and uses the
// returned config for all LLM rounds in that loop.
type TurnRouter interface {
	// RouteTurn returns a (possibly overridden) LLM config and a RouteDecision.
	// applied=false means keep GetLLMConfig() / default primary routing.
	RouteTurn(userText string) (cfg corelib.MaclawLLMConfig, decision RouteDecision, applied bool)
}

// LoopCallbacks defines the capabilities the agent loop needs from its host.
// GUI provides a full implementation; TUI provides a simpler one.
type LoopCallbacks interface {
	// GetLLMConfig returns the current LLM configuration.
	GetLLMConfig() corelib.MaclawLLMConfig

	// GetMaxIterations returns the maximum number of loop iterations.
	// Implementations MUST use config.EffectiveMaxIterations(configuredValue)
	// to ensure consistent behavior across all hosts. Example:
	//
	//   func (c *myCallbacks) GetMaxIterations() int {
	//       return config.EffectiveMaxIterations(c.cfg.MaclawAgentMaxIterations)
	//   }
	//
	// This ensures the same default (300), minimum (30), and maximum (300)
	// are applied everywhere.
	GetMaxIterations() int

	// BuildSystemPrompt constructs the system prompt for the LLM.
	BuildSystemPrompt(userText string, isFirstTurn bool) string

	// BuildTools returns the tool definitions to send to the LLM.
	BuildTools(userText string) []map[string]interface{}

	// ExecuteTool executes a tool call and returns the result string.
	ExecuteTool(name, argsJSON string) string

	// OnToken is called with each streaming text delta (may be nil).
	OnToken(delta string)

	// OnProgress is called with progress updates (may be nil).
	OnProgress(text string)

	// OnToolCall is called before a tool is executed (for UI updates).
	OnToolCall(name string)

	// OnToolResult is called after a tool is executed (for UI updates).
	OnToolResult(name string)

	// ShouldStop returns true if the loop should be terminated early.
	ShouldStop() bool
}

type ToolExecutionOutcome string

const (
	ToolExecutionOutcomeOK      ToolExecutionOutcome = "ok"
	ToolExecutionOutcomeTimeout ToolExecutionOutcome = "timeout"
	ToolExecutionOutcomeError   ToolExecutionOutcome = "error"
)

type ToolExecutionResult struct {
	Result  string
	Outcome ToolExecutionOutcome
	// ModelImages are screenshots (or other images) that should be shown to a
	// vision-capable chat model on the next LLM round. They are never treated as
	// user-facing outbound artifacts.
	ModelImages []ToolModelImage
}

// ToolModelImage is one image attached to a tool result for a vision model.
type ToolModelImage struct {
	MIME   string // e.g. image/png
	Base64 string
}

type StructuredToolExecutor interface {
	ExecuteToolStructured(name, argsJSON string) ToolExecutionResult
}

// ToolCallExecutor is the host-protocol aware execution boundary.  Unlike the
// older name/arguments callbacks, it preserves the model/provider tool-call
// identifier so a host can durably correlate retries and reconnects without
// reinterpreting a call as a new operation.
//
// Implementations that perform externally visible or otherwise non-replayable
// work should prefer this interface and reject an empty CallID rather than
// inventing one from a function name.
type ToolCallExecutor interface {
	ExecuteToolCall(name, argsJSON, callID string) ToolExecutionResult
}

// ToolCallExecutionContext is trusted dispatch metadata attached by RunLoop to
// a tool call that came from one concrete model request. It is not derived
// from tool arguments and must never be filled by a model/provider response.
//
// SurfaceEpoch identifies the exact tool surface that the request observed.
// Hosts with mutable/semantic surfaces use it to reject a delayed response
// after a replacement rather than resolving an old function name against the
// currently visible grant.
type ToolCallExecutionContext struct {
	SurfaceEpoch string
	// Protocol and ConnectionID are host/transport-owned correlation values.
	// Dynamic hosts use them with ResponseID and SurfaceEpoch to construct a
	// durable HostCallIdentity; none of the fields may come from tool arguments
	// or model-visible prompt text.
	Protocol     string
	ConnectionID string
	// ResponseID is the provider-issued identifier for the concrete response
	// that contained this call. RunLoop copies it from parsed provider metadata,
	// never from a function argument or a locally generated call ID. Hosts that
	// require durable dynamic aliases must reject a missing correlation.
	ResponseID string
	// ResponseBindingError is set only by a host-owned response-surface binder.
	// It lets a dynamic dispatcher fail closed for a response whose provider
	// correlation could not be durably bound, without changing stateless-tool
	// behavior for other hosts.
	ResponseBindingError string
}

// ToolCallContextExecutor is the epoch-aware form of ToolCallExecutor. The
// older interface remains for ordinary stateless tools and compatibility
// hosts. RunLoop prefers this form whenever it is implemented.
type ToolCallContextExecutor interface {
	ExecuteToolCallWithContext(name, argsJSON, callID string, execution ToolCallExecutionContext) ToolExecutionResult
}

// ToolSurfaceEpochProvider creates an opaque, server-owned correlation token
// immediately before each model request. The returned token is held only by
// RunLoop and passed back through ToolCallExecutionContext for tool calls in
// that response. It is intentionally absent from model-visible function
// parameters, so a model cannot nominate another surface epoch.
type ToolSurfaceEpochProvider interface {
	BeginToolSurfaceEpoch(iteration int) string
}

// ToolSurfaceExecutionContextProvider augments the epoch created at a concrete
// request boundary with trusted transport identity. It is deliberately
// separate from ToolSurfaceEpochProvider for compatibility with static hosts.
// A dynamic implementation must never synthesize these values from a loop ID,
// call ID, function name, or model response text.
type ToolSurfaceExecutionContextProvider interface {
	ToolSurfaceExecutionContext(iteration int, epoch string) ToolCallExecutionContext
}

// ToolSurfaceResponseBinder receives provider metadata before any tool call in
// that response is dispatched. Durable dynamic hosts use it to bind a request
// surface to the provider-issued response ID. An error is carried into the
// context and must be rejected by that host's dynamic dispatcher.
type ToolSurfaceResponseBinder interface {
	BindToolSurfaceResponse(execution ToolCallExecutionContext) error
}

// ToolSurfaceRequestChannel is a host-owned, single-attempt transport
// reservation for a tool-enabled model request. It exists so a future semantic
// Coding adapter can reserve one real channel *before* definitions are
// rendered, publish its request surface against that channel, send exactly one
// request, then return the correlated provider response through the same
// ownership boundary.
//
// A channel implementation must not transparently retry, redirect, reconnect,
// or fall back to another request. RunLoop owns successor attempts and will
// reserve a fresh channel before rendering each successor surface. The channel
// metadata must come from the live transport instance, never from a URL,
// model/config name, task text, loop/request ID, or tool call.
type ToolSurfaceRequestChannel interface {
	// ExecutionContext exposes the transport-owned protocol/connection tuple
	// for this exact reservation. SurfaceEpoch is assigned by RunLoop and is
	// ignored from this value.
	ExecutionContext() ToolCallExecutionContext
	// Do sends exactly one request over this reservation. stream selects the
	// loop's requested presentation mode; it must not change attempt ownership.
	Do(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (*llm.Response, error)
	// Close releases the reservation once its single attempt is terminal. cause
	// is diagnostic-only and must not be used as a replay or identity input.
	Close(cause error)
}

// VerifiedToolSurfaceDispatch is the immutable evidence returned by the same
// channel operation that sent (or attempted to send) a model request. It keeps
// payload verification and handoff state request-owned; callers must not look
// them up later from a cache, URL, task, alias, or transport-global map.
type VerifiedToolSurfaceDispatch struct {
	Response *llm.Response
	Receipt  ToolSurfaceReceipt
}

// VerifiedToolSurfaceRequestChannel is required for a correlation-bound
// request surface. A plain ToolSurfaceRequestChannel is intentionally not
// enough: it could internally verify a payload yet return a response without
// giving RunLoop proof that the response belongs to that verified dispatch.
type VerifiedToolSurfaceRequestChannel interface {
	ToolSurfaceRequestChannel
	DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (VerifiedToolSurfaceDispatch, error)
}

// ToolSurfacePublicationProofRequirement marks a reservation whose executable
// surface is durable and correlation-bound. Such a channel must not accept the
// legacy renderer's bare []definition result: it requires the renderer to
// explicitly prove that the exact reservation-owned surface was published.
//
// The marker is owned by the live channel/holder, not inferred from a config,
// tool name, URL, task, alias, or whether the returned definitions happen to
// be empty. Compatibility channels intentionally do not implement it, so an
// explicit empty replacement remains a valid ordinary lifecycle fixture.
type ToolSurfacePublicationProofRequirement interface {
	RequiresPublishedBoundToolSurface() bool
}

// toolSurfaceInvocationPolicyForConfig selects only the request protocol
// envelope that this loop is about to construct. It never derives a policy
// from a URL, model, task, alias, or tool name; choice/parallel are explicit
// provider defaults until a host-owned request policy is threaded here.
func toolSurfaceInvocationPolicyForConfig(cfg corelib.MaclawLLMConfig) ToolSurfaceInvocationPolicy {
	switch {
	case cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket():
		return DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeResponses)
	case strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic"):
		return DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeAnthropic)
	default:
		return DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat)
	}
}

// ToolSurfaceInvocationPolicyProvider supplies the host-owned invocation
// controls for one outbound request. It is deliberately separate from tool
// rendering: neither a mutable ExtraBody map nor provider/model heuristics may
// decide whether a rendered function is auto-selected, required, named, or
// parallel-callable.
//
// The returned policy must use the envelope that RunLoop is about to send. A
// provider may return the explicit provider-default policy when it has no
// stronger control to express.
type ToolSurfaceInvocationPolicyProvider interface {
	ToolSurfaceInvocationPolicy(iteration int) (ToolSurfaceInvocationPolicy, error)
}

func toolSurfaceInvocationPolicyForRequest(cb LoopCallbacks, cfg corelib.MaclawLLMConfig, iteration int) (ToolSurfaceInvocationPolicy, error) {
	expected := toolSurfaceInvocationPolicyForConfig(cfg)
	provider, ok := cb.(ToolSurfaceInvocationPolicyProvider)
	if !ok {
		return expected, nil
	}
	policy, err := provider.ToolSurfaceInvocationPolicy(iteration)
	if err != nil {
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("tool surface invocation policy: %w", err)
	}
	policy, err = normalizeToolSurfaceInvocationPolicy(policy)
	if err != nil {
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("tool surface invocation policy: %w", err)
	}
	if policy.Envelope != expected.Envelope {
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("tool surface invocation policy envelope %q does not match request envelope %q", policy.Envelope, expected.Envelope)
	}
	// Anthropic's Messages API does not currently have a reviewed native
	// projection for these controls. Do not accept an apparently meaningful
	// policy only to have the builder silently omit it; that would make the
	// manifest authorize a different surface than the provider observed.
	if policy.Envelope == ToolSurfaceEnvelopeAnthropic &&
		(policy.ToolChoice.Mode != ToolSurfaceToolChoiceProviderDefault || policy.ParallelToolCalls.Present) {
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("tool surface invocation policy is not qualified for anthropic envelope")
	}
	return policy, nil
}

func toolSurfacePolicyRequestOptions(policy ToolSurfaceInvocationPolicy) (interface{}, *bool) {
	var choice interface{}
	switch policy.ToolChoice.Mode {
	case ToolSurfaceToolChoiceAuto, ToolSurfaceToolChoiceRequired, ToolSurfaceToolChoiceNone:
		choice = policy.ToolChoice.Mode
	case ToolSurfaceToolChoiceSpecific:
		if policy.Envelope == ToolSurfaceEnvelopeResponses {
			choice = map[string]interface{}{"type": "function", "name": policy.ToolChoice.Name}
		} else {
			choice = map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": policy.ToolChoice.Name}}
		}
	}
	if !policy.ParallelToolCalls.Present {
		return choice, nil
	}
	parallel := policy.ParallelToolCalls.Value
	return choice, &parallel
}

// ToolSurfaceRequestChannelProvider is the S1-C transport seam. It is optional
// so existing HTTP/SSE compatibility callbacks keep their deliberately weaker
// S0.5 behavior. A provider returning a channel accepts the stricter invariant
// that the reserved channel owns the request which observes the rendered tool
// surface.
type ToolSurfaceRequestChannelProvider interface {
	ReserveToolSurfaceRequestChannel(ctx context.Context, cfg corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error)
}

// ToolSurfaceDisposition is the semantic terminal outcome of one reserved
// request channel. It is deliberately distinct from ToolSurfaceRequestChannel
// Close: closing a socket only releases transport resources and cannot prove
// whether the response was bound, accepted, or durably settled.
//
// RunLoop emits exactly one disposition for every non-nil reserved channel. A
// host must use it only to retire or settle the surface already identified by
// the supplied transport-owned context; it must not derive a replacement
// identity, connection, response, or grant from the disposition value.
type ToolSurfaceDisposition string

const (
	// ToolSurfaceTransportFailure means the single channel attempt did not
	// return a response that the loop can consume.
	ToolSurfaceTransportFailure ToolSurfaceDisposition = "transport_failure"
	// ToolSurfaceResponseAbandoned means a response was returned but did not
	// become a usable, fully settled tool surface (for example bind failure,
	// empty/invalid response, interactive interruption, or a rejected batch).
	ToolSurfaceResponseAbandoned ToolSurfaceDisposition = "response_abandoned"
	// ToolSurfaceResponseSettled means a tool-free response was accepted into
	// the loop's final/history path.
	ToolSurfaceResponseSettled ToolSurfaceDisposition = "response_settled"
	// ToolSurfaceToolBatchSettled means every call in the response was paired
	// and the complete batch passed its optional durability commit.
	ToolSurfaceToolBatchSettled ToolSurfaceDisposition = "tool_batch_settled"
	// ToolSurfaceSteered means host-owned live steering discarded the current
	// request/response before it could become the next model turn.
	ToolSurfaceSteered ToolSurfaceDisposition = "steered"
	// ToolSurfaceRuntimeTerminal means the loop stopped before it could settle
	// the current response (for example cancellation or a hard terminal path).
	ToolSurfaceRuntimeTerminal ToolSurfaceDisposition = "runtime_terminal"
	// ToolSurfaceIntegrityFailure means the channel did not supply a verified,
	// explicit replacement receipt for the rendered request. It is terminal and
	// never permits binding or a successor to reuse the old surface.
	ToolSurfaceIntegrityFailure ToolSurfaceDisposition = "surface_integrity_failure"
)

// ToolSurfaceDispositionObserver receives the one semantic terminal outcome
// for a reserved request channel. This is intentionally a separate optional
// callback from ToolSurfaceResponseBinder: a successful bind does not by
// itself establish that the response survived steering, completed its tool
// batch, or was committed as final text.
type ToolSurfaceDispositionObserver interface {
	OnToolSurfaceDisposition(execution ToolCallExecutionContext, disposition ToolSurfaceDisposition)
}

// ToolSurfaceDeliveryState records only what the host observed about one model
// request attempt. It is intentionally weaker than provider correlation: it
// neither identifies a connection/response/tool call nor authorizes replay.
// A request that was started but did not yield a normally consumed response is
// ambiguous by default. An adapter may report NotSent only when it can prove
// that no request bytes left the host.
type ToolSurfaceDeliveryState string

const (
	ToolSurfaceNotSent           ToolSurfaceDeliveryState = "not_sent"
	ToolSurfaceResponseConsumed  ToolSurfaceDeliveryState = "response_consumed"
	ToolSurfaceAmbiguousDelivery ToolSurfaceDeliveryState = "ambiguous_delivery"
)

// ToolSurfaceAttemptObserver observes host-side request attempts. Its input is
// built exclusively at the request boundary; implementations must not derive
// delivery state from model text, tool arguments, loop/request IDs, or provider
// configuration labels.
type ToolSurfaceAttemptObserver interface {
	OnToolSurfaceAttemptStarted(execution ToolCallExecutionContext)
	OnToolSurfaceAttemptFinished(execution ToolCallExecutionContext, delivery ToolSurfaceDeliveryState)
}

// ToolSurfaceAmbiguousDeliveryContainment opts a compatibility host into the
// conservative S0.5 rule: after an already-started request becomes ambiguous,
// RunLoop must not automatically issue a fallback or retry with a successor
// executable tool surface. This is not a substitute for a correlation-bound
// adapter, grant, or journal.
type ToolSurfaceAmbiguousDeliveryContainment interface {
	ContainToolSurfaceAmbiguousDelivery() bool
}

// ModelRequestToolSurfaceRenderer renders the exact definitions for one actual
// model request. It is intentionally separate from BuildTools: a request-local
// renderer can mint opaque invocation aliases only after the loop is about to
// send the request, instead of caching provider/resource selectors across
// iterations. The returned list is a complete replacement surface.
type ModelRequestToolSurfaceRenderer interface {
	BuildToolsForModelRequest(userText string, iteration int) []map[string]interface{}
}

// BoundModelRequestToolSurfaceRenderer is the channel-aware form of the
// request renderer. It is reserved for correlation-capable hosts: RunLoop
// supplies the exact transport reservation and server-owned surface epoch
// before the host publishes a durable ModelRequestSurface. Existing renderers
// keep ModelRequestToolSurfaceRenderer and therefore retain the compatibility
// ordering where static rendering precedes its local epoch fence.
type BoundModelRequestToolSurfaceRenderer interface {
	BuildToolsForBoundModelRequest(userText string, iteration int, execution ToolCallExecutionContext) []map[string]interface{}
}

// BoundToolSurfaceRender distinguishes an explicitly published empty
// replacement from a renderer that could not publish its reservation-owned
// surface. Definitions alone cannot carry that distinction: nil and [] are
// both valid Go representations of an explicit empty replacement.
//
// Published is host-owned publication evidence, not an identity, grant,
// alias, or execution authorization. Failure is diagnostic-only and must not
// be used to select a successor, transport, or capability.
type BoundToolSurfaceRender struct {
	Definitions []map[string]interface{}
	Published   bool
	Failure     string
}

// PublishedBoundModelRequestToolSurfaceRenderer is the proof-carrying form
// of BoundModelRequestToolSurfaceRenderer. Correlation-bound dynamic hosts
// should implement it so RunLoop can fail closed when durable publication
// fails rather than mistaking that failure for tools: []. The legacy renderer
// remains for compatibility hosts whose explicitly empty request surface is
// valid and whose interface cannot otherwise distinguish the two states.
type PublishedBoundModelRequestToolSurfaceRenderer interface {
	RenderPublishedBoundToolSurface(userText string, iteration int, execution ToolCallExecutionContext) BoundToolSurfaceRender
}

// ToolResultProjector optionally lets a host build the model-facing view of a
// tool result. RunLoop passes the complete execution result so runtime/UI/audit
// consumers can retain raw output while the model receives a compact preview
// with a lossless read-back handle.
type ToolResultProjector interface {
	ProjectToolResult(name string, result ToolExecutionResult) string
}

// ToolExecutionEscalator lets a host replace a lightweight turn route after a
// successfully completed tool execution. RunLoop invokes it only after an
// actual successful dispatch, never for an argument, policy, failed, or
// interactive-pause result.
// The loop checks GetLLMConfig again before the next LLM round, so an
// escalation here also updates that round's document and tool budgets.
type ToolExecutionEscalator interface {
	EscalateAfterToolExecution(name string)
}

// ToolExecutionSurfaceRefresher lets a host refresh the model-visible prompt
// and tools after a successful tool execution. Typical cases are a light→reasoning
// route change and a consumed semantic grant whose opaque name must disappear
// before the next LLM round. It is separate from ToolExecutionEscalator so a
// grant retirement can rebuild tools without promoting the model.
type ToolExecutionSurfaceRefresher interface {
	RefreshAfterToolExecution(name string) bool
}

// ToolCallPetitioner lets a governed host rescue a model tool call that names
// a real cataloged tool absent from the current rendered surface. Without it
// such a call is a hard denial with no same-turn recovery, so a planner that
// under-rendered the surface (for example a degraded intent classification)
// strands the whole turn.
type ToolCallPetitioner interface {
	// PetitionToolCall is consulted exactly once per name when the model calls
	// a function that was not rendered in the current request surface and has
	// not already succeeded in this loop. Granted means the host expanded the
	// governed surface through its trusted planner; message then replaces the
	// denial and tells the model to re-issue the call. The execution outcome
	// remains an error either way, preserving retry/drift semantics; the
	// widened surface is observed by the next iteration's request rebuild.
	PetitionToolCall(name string) (granted bool, message string)
}

// LLMRequestContextProvider lets hosts wrap each LLM round with their own
// scheduling, tracing, and cancellation boundary without making corelib/agent
// depend on GUI/runtime packages.
type LLMRequestContextProvider interface {
	LLMRequestContext(iteration int) (context.Context, func(error), error)
}

// LLMReplanAware is implemented by hosts that can cancel only the current LLM
// operation to inject fresh user steering. It is deliberately distinct from
// ShouldStop: a replan continues the loop, while cancellation ends it.
type LLMReplanAware interface {
	LLMReplanRequested() bool
}

// LLMRoundNotifier is an optional host callback invoked immediately before
// every LLM request, including a live-steer replacement. The first request
// also notifies: hosts use it to mark the assistant round as actively
// streaming (e.g. auto-expanding the thinking panel), and the first round
// streams tokens exactly like later ones.
type LLMRoundNotifier interface {
	OnLLMNewRound()
}

// LLMFinalizationGuard lets a host atomically arbitrate final response commit
// against live steering acceptance. False means a steer won the race and the
// response must be discarded and regenerated from the transformed conversation.
type LLMFinalizationGuard interface {
	TryFinalizeLLMResponse() bool
}

const (
	maxFreeReplansPerLoop  = 64
	maxLLMRetries          = 5
	initialLLMRetryBackoff = 2 * time.Second
	maxLLMRetryBackoff     = 32 * time.Second
)

// llmRetryBackoff returns the delay before a retry after a transient LLM
// failure. retryAttempt is one-based, so the five delays are 2, 4, 8, 16,
// and 32 seconds.
func llmRetryBackoff(retryAttempt int) time.Duration {
	if retryAttempt <= 0 {
		return 0
	}
	delay := initialLLMRetryBackoff
	for attempt := 1; attempt < retryAttempt && delay < maxLLMRetryBackoff; attempt++ {
		delay *= 2
		if delay > maxLLMRetryBackoff {
			return maxLLMRetryBackoff
		}
	}
	return delay
}

// ToolAuthorizer is an optional host callback implemented when tool execution
// must be constrained by an outer policy, such as a workflow phase.
type ToolAuthorizer interface {
	IsToolAllowed(name string) bool
}

// ToolDenialPresenter is optional. When ToolAuthorizer rejects a name, the
// host may replace the generic policy text. A governed light lookup uses this
// to tell the model to answer from evidence; coding/workflow hosts must not.
type ToolDenialPresenter interface {
	ToolDenialMessage(name string) string
}

// ToolCallAuthorizer is an optional stronger execution boundary for hosts that
// need to validate tool arguments, not just the tool name.
type ToolCallAuthorizer interface {
	IsToolCallAllowed(name, argsJSON string) (bool, string)
}

// PromptProfileProvider is an optional host callback that reports the adaptive
// system-prompt profile for the current turn. When light, RunLoop denies
// non-allowlisted tools (with misroute metrics) even if the model invents a call.
type PromptProfileProvider interface {
	CurrentPromptProfile() PromptProfile
}

// PromptProfileToolAuthorizer is an optional host extension for tool surfaces
// whose model-facing names are not stable policy identifiers. In particular,
// a semantic CatalogRenderer emits opaque invocation grants; the host must
// resolve such a name back to its immutable planned selection and decide from
// that selection's capability/effects/confirmation contract.
//
// The name is only a transport lookup key. Implementations must not infer a
// policy from its spelling, provider display name, or model supplied arguments.
// Returning false is fail-closed. Hosts that do not implement this extension
// retain the static light-profile policy below.
type PromptProfileToolAuthorizer interface {
	IsToolAllowedForPromptProfile(name string, profile PromptProfile) bool
}

// LightProfileUpgrader is an optional host callback. When a light turn blocks a
// non-allowlisted tool, RunLoop may call this once, rebuild tools/system prompt,
// and re-authorize the tool so the turn can recover without user re-ask.
// A managed semantic turn must not implement a successful upgrade: Phase C
// forbids BuildTools rebuild-and-retry of the same name after a light deny.
type LightProfileUpgrader interface {
	// UpgradeLightPromptToFull switches the turn to full prompt + tools.
	// Returns true when the host successfully upgraded (profile is now full).
	UpgradeLightPromptToFull(reason string) bool
}

// ManagedSemanticTurn is an optional host callback. When true, the loop must
// not treat a light tool deny as permission to rebuild a larger tool surface.
type ManagedSemanticTurn interface {
	ManagedSemanticTurn() bool
}

// EarlyStopper is an optional host callback for non-cancel stops (e.g. daily
// LLM budget). Distinct from ShouldStop(), which maps to "cancelled".
type EarlyStopper interface {
	// EarlyStop returns stop=true to end the loop. A non-empty errCode is an
	// error stop (Error=errCode, Text=userText, HardExit). An empty errCode is
	// a clean completion: the turn's goal is already reached (e.g. artifact
	// delivered), the loop ends without Error, and an empty userText falls
	// back to the last non-empty assistant content.
	EarlyStop() (stop bool, errCode, userText string)
}

// LLMUsageRecorder is an optional host callback invoked after each LLM round
// with provider-reported tokens so hosts can charge CostTracker mid-loop.
type LLMUsageRecorder interface {
	OnLLMUsage(model string, inputTokens, outputTokens int)
}

// LoopHooks provides optional extension points for the agent loop.
// Hosts that don't need these features can embed DefaultLoopHooks.
type LoopHooks interface {
	// OnToolExecuted is called after a tool is executed with its result.
	// Used for session pinning, outcome recording, etc.
	OnToolExecuted(name, argsJSON, result string, success bool)

	// OnEmptyResponse is called when the LLM returns an empty response.
	// Returns true to continue the loop (retry), false to exit.
	OnEmptyResponse(iteration int) bool

	// TransformConversation is called before each LLM request with the current
	// conversation. It may return a modified conversation (e.g., compacted).
	// Return nil to keep the conversation unchanged.
	// This enables mid-loop compaction without exposing conversation internals.
	TransformConversation(conversation []interface{}) []interface{}
}

// ToolBatchMetadata describes a complete assistant tool-call batch after every
// call has a paired result in HistoryDelta. It deliberately excludes arguments
// and raw results: persistence hosts already receive the safe conversation
// entries and must not infer permission to replay a tool call.
type ToolBatchMetadata struct {
	Sequence        uint64
	LastToolName    string
	SideEffectState string
}

// ToolBatchCommitter is an optional durability hook. It is intentionally
// separate from LoopHooks for compatibility with existing hosts; RunLoop stops
// before the next model/tool step when it returns an error.
type ToolBatchCommitter interface {
	OnToolBatchCommitted(delta []ConversationEntry, meta ToolBatchMetadata) error
}

// ToolBatchStarter is an optional pre-execution durability hook. Its delta
// contains the assistant's complete tool-call declaration but no results, so
// the delta is diagnostic evidence rather than a provider-valid history group.
// Hosts that persist it must retain only their last valid history prefix and
// store tool identity in metadata. A crash while a tool is executing can then
// be recovered as uncertain work without replaying it. Returning an error
// prevents the first tool in the batch from running.
type ToolBatchStarter interface {
	OnToolBatchStarting(delta []ConversationEntry, meta ToolBatchMetadata) error
}

// ToolBatchAbandoner is an optional notification hook for an interactive pause
// discovered while processing a pre-committed tool batch. Hosts must retire
// the temporary checkpoint only when they durably commit the paired
// interactive result; sibling calls in this batch were intentionally not run.
type ToolBatchAbandoner interface {
	OnToolBatchAbandoned(meta ToolBatchMetadata)
}

// DefaultLoopHooks provides no-op implementations of all optional hooks.
type DefaultLoopHooks struct{}

func (DefaultLoopHooks) OnToolExecuted(string, string, string, bool)       {}
func (DefaultLoopHooks) OnEmptyResponse(int) bool                          { return false }
func (DefaultLoopHooks) TransformConversation([]interface{}) []interface{} { return nil }

// LoopResult is the output of RunLoop.
type LoopResult struct {
	Text       string
	Error      string
	Iterations int
	ToolCalls  int
	AskUser    *AskUserRequest
	// RecordAudio is set when record_audio opened an interactive session and
	// the loop paused for the host UI (desktop waveform card). Hosts must
	// open the recording UI and resume after the user stops/cancels.
	RecordAudio *RecordAudioRequest
	// PauseToolCallID is the tool_call id that triggered AskUser/RecordAudio
	// early-stop. Hosts need it to pair the tool result (do not use the last
	// id in a multi-tool batch — record_audio may not be last).
	PauseToolCallID string
	HardExit        bool // true when loop exited abnormally (consecutive empty responses, same-tool hard stop, etc.)

	// Usage aggregates LLM token/cost accounting for this loop when the host
	// (or future loop instrumentation) records it. Zero means unknown.
	Usage TurnUsage
	// Route is the model-routing decision for this loop when known.
	Route RouteDecision
	// LightUpgraded is true when this loop recovered from a light-profile tool
	// deny by upgrading to full prompt+tools mid-turn.
	LightUpgraded bool
	// HistoryDelta is the new conversation entries produced by this loop
	// (user turn + assistant/tool messages), excluding the system prompt and
	// any prior history. Hosts can append this to durable session history.
	HistoryDelta []ConversationEntry
	// Reasoning is the concatenation of provider-supplied, display-safe
	// reasoning summaries across all LLM rounds in this loop. It is separate
	// from HistoryDelta because a tool-using turn can have several assistant
	// rounds and only the final round used to reach the host response.
	Reasoning string
	// WorkingState is the task-turn workspace when this loop earned or resumed one.
	// Hosts may ignore it; AskUser/RecordAudio carriers persist it across RunLoop calls.
	WorkingState *WorkingState
}

// RunLoop executes the core agent loop: LLM call → tool execution → repeat.
// This is the single implementation shared by GUI and TUI.
//
// hooks is optional — pass nil to use DefaultLoopHooks.
func RunLoop(cb LoopCallbacks, userText string, history []ConversationEntry, httpClient *http.Client, hooks ...LoopHooks) LoopResult {
	return RunLoopWithUserContent(cb, userText, userText, history, httpClient, hooks...)
}

// RunLoopWithUserContent executes the core agent loop with a prebuilt user
// content payload. userText remains the plain text used for prompt/context
// construction, while userContent may be a multimodal content array.
func RunLoopWithUserContent(cb LoopCallbacks, userText string, userContent interface{}, history []ConversationEntry, httpClient *http.Client, hooks ...LoopHooks) LoopResult {
	var h LoopHooks = DefaultLoopHooks{}
	if len(hooks) > 0 && hooks[0] != nil {
		h = hooks[0]
	}

	cfg := cb.GetLLMConfig()
	route := PrimaryRouteDecision(cfg)
	// Optional host-side turn routing (e.g. GUI ModelRouter + ClassifyTurn).
	if tr, ok := cb.(TurnRouter); ok {
		if routed, decision, applied := tr.RouteTurn(userText); applied {
			cfg = routed
			route = decision
			route.Applied = true
		}
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return LoopResult{
			Error: "LLM not configured",
			Route: RouteDecision{Source: "primary", Reason: "LLM not configured", Applied: false},
		}
	}

	var usage TurnUsage
	usage.Model = cfg.Model
	usage.Provider = cfg.ProviderName
	// lightUpgraded: at most one light→full recovery per loop (tool-deny path).
	// Declared early so finish() can surface LightUpgraded on LoopResult.
	lightUpgraded := false
	// HistoryDelta tracks durable session entries for hosts (user + assistant + tools).
	// Store the same multimodal payload the model saw (string or content blocks).
	historyDelta := make([]ConversationEntry, 0, 8)
	historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: userContent})
	var displayReasoning strings.Builder
	workingState := loadInitialWorkingState(cb)
	executedTools := 0
	finish := func(r LoopResult) LoopResult {
		if r.Usage.Requests == 0 && usage.Requests > 0 {
			r.Usage = usage
		} else if usage.Requests > 0 {
			// Prefer accumulated loop usage when the caller left Usage zero.
			if r.Usage.InputTokens == 0 && r.Usage.OutputTokens == 0 {
				r.Usage = usage
			}
		} else if r.Usage.Model == "" {
			r.Usage.Model = cfg.Model
			r.Usage.Provider = cfg.ProviderName
		}
		if r.Route.Source == "" {
			r.Route = route
		}
		if lightUpgraded {
			r.LightUpgraded = true
		}
		if len(r.HistoryDelta) == 0 && len(historyDelta) > 0 {
			r.HistoryDelta = append([]ConversationEntry(nil), historyDelta...)
		}
		if r.Reasoning == "" && displayReasoning.Len() > 0 {
			r.Reasoning = displayReasoning.String()
		}
		finishWorkingState(cb, &r, workingState)
		return r
	}
	maxIter := cb.GetMaxIterations()
	if maxIter <= 0 {
		// This should never happen if GetMaxIterations() is implemented correctly
		// (using config.EffectiveMaxIterations). Log a warning to surface bugs.
		log.Printf("[agent-loop] WARNING: GetMaxIterations() returned %d, using fallback. This indicates a bug in the LoopCallbacks implementation.", maxIter)
		maxIter = config.EffectiveMaxIterations(0)
	}

	isFirstTurn := len(history) == 0
	systemPrompt := cb.BuildSystemPrompt(userText, isFirstTurn)
	// Tool definitions are request-owned state. Do not construct an initial
	// compatibility surface before a real model request exists: a static host may
	// return fresh inventory, policy or feature-gated tools on every BuildTools
	// call, and a pre-loop snapshot has no manifest, receipt or terminal owner.
	// `tools` is populated at the first request boundary below and then retained
	// only as local context for recovery text until that request is terminal.
	var tools []map[string]interface{}

	// Build conversation from history + current message.
	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.ToMessage())
	}
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})

	if httpClient == nil {
		// A request-scoped deadline is applied below after the final per-round
		// route (including any MoA aggregator) is known. Do not capture the
		// first route's ResponseHeaderTimeout here: a tool can promote a 32K
		// light route to a 200K/400K reasoning route with a longer timeout.
		httpClient = &http.Client{}
	}

	totalToolCalls := 0
	var lastNonEmptyContent string
	checkEarlyStop := func(iteration int) *LoopResult {
		es, ok := cb.(EarlyStopper)
		if !ok {
			return nil
		}
		stop, code, text := es.EarlyStop()
		if !stop {
			return nil
		}
		if strings.TrimSpace(code) == "" {
			// A code-less stop is a clean completion, not an error: the host
			// declared the turn's goal reached (for example the artifact was
			// delivered and the semantic plan is exhausted), so the closing
			// LLM round trip is skipped. That call only produces summary text
			// yet pays full latency and, on an upstream outage, strands an
			// already-delivered turn on retries (production 2026-08-26: PPT
			// delivered, then the closing call retried a 502 for ~65s).
			if strings.TrimSpace(text) == "" {
				text = lastNonEmptyContent
			}
			return &LoopResult{
				Text:       StripThinkingTags(text),
				Iterations: iteration,
				ToolCalls:  totalToolCalls,
			}
		}
		return &LoopResult{
			Text:       text,
			Error:      code,
			Iterations: iteration,
			ToolCalls:  totalToolCalls,
			HardExit:   true,
		}
	}
	consecutiveEmpty := 0
	const maxConsecutiveEmpty = 5
	var lastToolName string         // track last tool name for empty-response recovery
	var lastToolOutcome toolOutcome // structured outcome of last tool execution

	// Drift detection: track recent tool calls to detect loops.
	type toolCallRecord struct {
		name   string
		args   string
		result string
	}
	var recentCalls []toolCallRecord
	// succeededToolNames records tools that completed successfully at least
	// once in this loop. When a later request surface no longer renders such a
	// name (for example a consumed one-shot grant), the denial must say the
	// earlier success still stands; otherwise models conclude the action
	// failed and tell the user "delivery impossible" right after a successful
	// delivery (production 2026-08-25: send_file succeeded, the next request
	// dropped it, the retry was denied, and the assistant announced the PDF
	// could not be sent).
	succeededToolNames := map[string]struct{}{}
	const driftWindow = 4 // check last N calls for repetition
	consecutiveSame := 0

	// Consecutive same-tool failure detection: catches non-repeating failure
	// loops where the agent keeps trying different approaches with the same
	// tool but all fail (e.g. repeatedly editing a bat script then running
	// bash — each attempt has unique args so the exact-match drift detector
	// above won't trigger).
	//
	// Escalation: inject guidance at maxConsecutiveSameToolFailures (8), then
	// force-stop the loop at hardStopSameToolFailures (12) if the LLM ignores
	// the guidance and keeps failing.
	var lastFailedTool string
	consecutiveSameToolFailures := 0
	sameToolFailureGuidanceInjected := false
	const maxConsecutiveSameToolFailures = 8
	const hardStopSameToolFailures = 12

	// No-forward-progress detection: consecutive iterations in which NO tool
	// call succeeded. The same-tool detector above only counts repeats of one
	// tool, so a model that alternates failing/denied calls (or fence-denied
	// retries of an unrendered name) escapes it and dithers until maxIter —
	// production 2026-08-26: 12+ iterations, ~20 minutes of thinking, zero
	// successful calls after the first search. Petition/discovery rescues
	// need at most 1-2 no-success iterations, so 5 is far clear of legitimate
	// recovery.
	consecutiveNoProgressIterations := 0
	const hardStopNoProgressIterations = 5

	// MoA fan-out budget for this loop (K11).
	moaFanoutsRan := 0
	moaLastRefOK, moaLastRefFail, moaLastRefTotal := 0, 0, 0
	// References run concurrently. Receipt reporting is diagnostic-only, but a
	// host observer may use a simple append-only sink; serialize those callbacks
	// for this one loop without sharing any request identity or surface state.
	moaReceiptObserver := newSerializedToolSurfaceReceiptObserver(toolSurfaceReceiptObserverFor(cb))
	moaEventObserver := newSerializedToolSurfaceEventObserver(toolSurfaceEventObserverFor(cb))
	moaRunner := &moa.Runner{
		CallRef: func(ctx context.Context, refCFG corelib.MaclawLLMConfig, messages []interface{}) (*llm.Response, error) {
			// The fan-out runner adds its preset/per-reference cap to this context;
			// retain that cap while also enforcing the reference model's own
			// normalized timeout. This avoids an unbounded advisor request when a
			// caller invokes the runner outside its usual timeout wrapper.
			requestCtx, cancel := llmRequestContextWithTimeout(ctx, refCFG)
			defer cancel()
			// Advisors are independent outbound model requests. They are not
			// allowed to let an SDK issue a hidden compact/tool-less successor:
			// every successor must return to the owning loop so it gets a fresh
			// surface, receipt, attempt record, and delivery disposition.
			requestCtx = llm.WithTransparentRequestRetriesDisabled(requestCtx)
			// A reference call is still a real outbound model request. Its surface
			// is an explicit empty replacement and must be verified at the final
			// serialization boundary rather than silently omitting tools.
			lifecycle, err := newToolSurfaceReceiptHTTPClientWithLifecycleEvents(
				httpClient,
				nil,
				toolSurfaceInvocationPolicyForConfig(refCFG),
				ToolSurfacePlanEvidence{},
				lifecycleToolSurfaceReceiptObserver{receipts: moaReceiptObserver, events: moaEventObserver},
				moaEventObserver,
			)
			if err != nil {
				return nil, err
			}
			receiptClient := lifecycle.client
			// A reference request has no durable reservation holder, but it is still
			// an owner-visible static attempt. Close its metric lifecycle here rather
			// than letting the aggregator's later terminal event stand in for it.
			// This diagnostic event neither shares response identity nor grants any
			// authority to the advisor result.
			resp, requestErr := doLLMRequestWithTools(requestCtx, refCFG, messages, nil, toolSurfaceInvocationPolicyForConfig(refCFG), receiptClient)
			if requestErr == nil {
				requestErr = requireLLMDispatchResponse(resp)
			}
			emitToolSurfaceEvent(moaEventObserver, toolSurfaceTerminalEventFromManifest(lifecycle.manifest, moaReferenceToolSurfaceDisposition(resp, requestErr)))
			return resp, requestErr
		},
		MaxParallel: 3,
	}
	llmRequestAttempts := 0
	freeReplans := 0
	malformedReprompts := 0
	var toolBatchSequence uint64

	for iteration := 0; iteration < maxIter; iteration++ {
		if cb.ShouldStop() {
			return finish(LoopResult{Error: "cancelled", Iterations: iteration, ToolCalls: totalToolCalls})
		}
		// Non-cancel hard stops (budget, host policy) — after prior-round usage recorded.
		if stopped := checkEarlyStop(iteration); stopped != nil {
			return finish(*stopped)
		}

		// Refresh config each iteration so hosts can escalate models mid-loop
		// (e.g. light → reasoning after tools appear).
		if next := cb.GetLLMConfig(); strings.TrimSpace(next.URL) != "" && strings.TrimSpace(next.Model) != "" {
			cfg = next
			if usage.Model == "" {
				usage.Model = cfg.Model
				usage.Provider = cfg.ProviderName
			}
		}

		// Steer/replan: keep Goal and Settled, drop Live and unclosed Open.
		// Must run before TransformConversation — GUI consumes the replan
		// revision inside that hook, so a later splice check would miss it.
		if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
			ClearLiveAndOpen(workingState)
		}
		// Mid-loop conversation transformation (e.g., compaction).
		if transformed := h.TransformConversation(conversation); transformed != nil {
			conversation = transformed
		}
		conversation = FoldComputerUseObserves(conversation)
		workingState, conversation = spliceWorkingStateAtHead(cb, conversation, workingState, userText, executedTools)

		// Call LLM with tools via corelib/llm (streaming for real-time display).
		// Notify before every request, including the first: hosts flip the
		// assistant round into its streaming state here, and round one streams
		// tokens like any later round.
		if notifier, ok := cb.(LLMRoundNotifier); ok {
			notifier.OnLLMNewRound()
		}
		llmRequestAttempts++
		ctx, finishLLMRequest, ctxErr := llmRequestContextForLoop(cb, iteration)
		if ctxErr != nil {
			// The host declined to create the request context before any policy,
			// channel, manifest, or transport exists. Keep this failure visible as
			// one redacted pre-manifest lifecycle, without inventing a surface or
			// borrowing data from an earlier request.
			eventObserver := toolSurfaceEventObserverFor(cb)
			emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
			emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
			return finish(LoopResult{Error: fmt.Sprintf("surface_integrity_failure: LLM request context failed: %v", ctxErr), Iterations: iteration, ToolCalls: totalToolCalls})
		}
		// Optional MoA: reference fan-out on a request-only clone, then aggregator stream.
		// K9: loop re-checks env kill switch so a host cannot bypass MACLAW_MOA=off.
		reqConversation := conversation
		aggCFG := cfg
		if host, ok := cb.(MoAHost); ok && moa.EnvAllows() {
			toolsSeen := moa.ConversationHasToolResults(conversation)
			if active, preset, progress := host.PrepareMoA(iteration, toolsSeen, moaFanoutsRan); active {
				onlyBefore := preset.OnlyBeforeFirstTool
				dec := moa.ShouldFanOut(corelib.MoAConfig{
					FanoutMaxIterations: preset.FanoutMaxIterations,
					OnlyBeforeFirstTool: &onlyBefore,
				}, corelib.MoAPresetConfig{Enabled: preset.Enabled}, iteration, moaFanoutsRan, toolsSeen)
				thisFanOut := false
				usableRefs := moa.CountUsableRefs(preset.References)
				if dec.Allow && usableRefs > 0 {
					// Optional daily-budget precheck (PR3): estimate against usable advisors only.
					if gate, ok := cb.(MoABudgetGate); ok {
						if allow, reason := gate.AllowMoAFanOut(usableRefs); !allow {
							log.Printf("[agent-loop] moa fan-out skipped by budget gate: %s", reason)
							if reason != "" {
								cb.OnProgress(reason)
							}
							dec.Allow = false
						}
					}
				}
				// Fan-out only when at least one usable advisor is configured.
				// Error placeholders among a mixed set still appear in private advice.
				if dec.Allow && usableRefs > 0 {
					if progress != "" {
						cb.OnProgress(progress)
					} else {
						cb.OnProgress(fmt.Sprintf("consulting %d models…", usableRefs))
					}
					fan := moaRunner.RunReferences(ctx, preset, conversation)
					// Account reference usage before aggregator (even if aggregator fails later).
					for _, call := range fan.Calls {
						if call.Usage == nil {
							continue
						}
						round := TurnUsageFromLLM(call.Config, call.Usage)
						usage.Add(round)
						if rec, ok := cb.(LLMUsageRecorder); ok && (round.InputTokens > 0 || round.OutputTokens > 0) {
							model := call.Config.Model
							if model == "" {
								model = usage.Model
							}
							rec.OnLLMUsage(model, round.InputTokens, round.OutputTokens)
						}
					}
					if fan.Advice != "" {
						reqConversation = moa.InjectAdviceDeepCopy(conversation, fan.Advice)
					}
					moaFanoutsRan++
					thisFanOut = true
					moaLastRefOK = fan.RefOK
					moaLastRefFail = fan.RefFail
					moaLastRefTotal = len(preset.References)
					moa.RecordFanOut(preset.Name, fan.RefOK, fan.RefFail, fan.Duration)
					if fan.Progress != "" {
						cb.OnProgress(fan.Progress)
					}
					log.Printf("[agent-loop] moa fan-out preset=%s refs=%d ok=%d fail=%d ms=%d fanouts=%d reason=%s",
						preset.Name, len(preset.References), fan.RefOK, fan.RefFail, fan.Duration.Milliseconds(), moaFanoutsRan, dec.Reason)
				}
				// Aggregator config: use_primary follows GetLLMConfig (already in cfg); else fixed preset aggregator.
				if !preset.AggregatorUsePrimary {
					if strings.TrimSpace(preset.Aggregator.URL) != "" && strings.TrimSpace(preset.Aggregator.Model) != "" {
						aggCFG = preset.Aggregator
					}
				} else if preset.Raw.AggregatorMaxTokens > 0 {
					aggCFG.MaxOutputTokens = preset.Raw.AggregatorMaxTokens
				}
				route.Source = "moa"
				route.MoAPreset = preset.Name
				route.MoAFanouts = moaFanoutsRan
				route.MoAFanOut = thisFanOut
				// Prefer last completed fan-out counts for chip (ok/total); bare name when none yet.
				if moaLastRefTotal > 0 {
					route.MoAReferences = moaLastRefTotal
					route.MoARefOK = moaLastRefOK
					route.MoARefFailed = moaLastRefFail
				}
				if route.Reason == "" {
					route.Reason = "moa preset " + preset.Name
				} else if !strings.Contains(route.Reason, "moa=") {
					route.Reason = route.Reason + "; moa=" + preset.Name
				}
				route.Model = aggCFG.Model
				route.Provider = aggCFG.ProviderName
			}
		}

		// Apply the active route's timeout at the request boundary. This preserves
		// host-provided cancellation/tracing while making a tool-driven config
		// promotion effective immediately. It also covers the whole streaming
		// response rather than only the time spent waiting for response headers.
		requestBaseCtx := ctx
		requestCtx, cancelRequestTimeout := llmRequestContextWithTimeout(requestBaseCtx, aggCFG)
		finishRequest := func(requestErr error) {
			cancelRequestTimeout()
			finishLLMRequest(requestErr)
		}
		// Policy acquisition happens before a manifest can exist, but it is still
		// part of attempting to create this request-owned surface. Keep its
		// lifecycle observable as a redacted integrity failure rather than letting
		// telemetry make the failed request look as if it never reached the owner.
		eventObserver := toolSurfaceEventObserverFor(cb)
		invocationPolicy, policyErr := toolSurfaceInvocationPolicyForRequest(cb, aggCFG, iteration)
		if policyErr != nil {
			emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
			emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
			finishRequest(policyErr)
			return finish(LoopResult{Error: "surface_integrity_failure: " + policyErr.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
		}

		// A dynamic surface must be rendered at the concrete request boundary.
		// In particular, provider-bound aliases are request-local, so reuse of the
		// previous iteration's definitions would make a stale response executable.
		//
		// Correlation-capable hosts reserve their real transport channel before
		// rendering. That ordering is deliberately stricter than the legacy HTTP
		// compatibility path: a durable surface can then be published against the
		// exact live protocol/connection which will send it, rather than against a
		// config-derived approximation.
		executionContext := beginToolCallExecutionContext(cb, iteration)
		var requestChannel ToolSurfaceRequestChannel
		// A reservation becomes a lifecycle owner immediately, including failures
		// while its epoch/correlation, renderer or audit evidence is being checked.
		// Keep this closure ahead of those checks so every non-nil channel gets one
		// semantic terminal disposition rather than merely a transport Close.
		dispositionSent := false
		// Static S0.5 attempts have no reservation holder to receive a
		// disposition, but they still own a concrete receipt and must contribute
		// exactly one terminal metric. Reset this at every owner-visible static
		// send (initial request, stream fallback, or outer retry); no identity or
		// execution authority is derived from the flag.
		staticAttemptTerminalSent := false
		var staticAttemptManifest *ToolSurfaceEvent
		var boundSurfaceManifest *ToolSurfaceEvent
		disposeSurface := func(next ToolSurfaceDisposition) {
			if requestChannel == nil {
				if staticAttemptTerminalSent {
					return
				}
				staticAttemptTerminalSent = true
				if staticAttemptManifest != nil {
					emitToolSurfaceEvent(eventObserver, toolSurfaceTerminalEventFromManifest(*staticAttemptManifest, next))
					return
				}
				emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
				return
			}
			if dispositionSent {
				return
			}
			dispositionSent = true
			if observer, ok := cb.(ToolSurfaceDispositionObserver); ok {
				observer.OnToolSurfaceDisposition(executionContext, next)
			}
			if boundSurfaceManifest != nil {
				emitToolSurfaceEvent(eventObserver, toolSurfaceTerminalEventFromManifest(*boundSurfaceManifest, next))
				return
			}
			// A pre-manifest reservation failure has no rendered surface to
			// correlate. Its diagnostic receipt is deliberately explicit about
			// that fact, while the terminal metric remains redacted and must not
			// borrow a stale/partial digest from a prior request.
			emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: next, FailureKind: ToolSurfaceFailureIntegrity})
		}
		// A receipt observer is diagnostic-only, but a correlation-bound channel
		// may fail before it can return a dispatch receipt (for example missing
		// audit evidence or renderer/correlation validation). Keep a single
		// request-local reporter so every reserved attempt produces one audit
		// record without allowing observers to influence lifecycle authority.
		receiptReported := false
		reportReceipt := func(receipt ToolSurfaceReceipt) {
			if receiptReported {
				return
			}
			receiptReported = true
			if observer := toolSurfaceReceiptObserverFor(cb); observer != nil {
				observer.OnToolSurfaceReceipt(receipt)
			}
			emitToolSurfaceReceiptEvents(eventObserver, receipt)
		}
		integrityFailureReceipt := func(err error) ToolSurfaceReceipt {
			failure := "surface_integrity_failure"
			if err != nil && strings.TrimSpace(err.Error()) != "" {
				failure += ": " + err.Error()
			}
			return ToolSurfaceReceipt{ReplacementMode: "replace", Failure: failure, FailureKind: ToolSurfaceFailureIntegrity, Handoff: ToolSurfaceHandoffNotStarted}
		}
		retirePreDispatchIntegrityFailure := func(err error) {
			reportReceipt(integrityFailureReceipt(err))
			disposeSurface(ToolSurfaceIntegrityFailure)
			if requestChannel != nil {
				requestChannel.Close(err)
			}
		}
		if channelProvider, ok := cb.(ToolSurfaceRequestChannelProvider); ok {
			channel, reserveErr := channelProvider.ReserveToolSurfaceRequestChannel(requestCtx, aggCFG)
			if reserveErr != nil {
				// No live channel / manifest exists, so this failure cannot carry a
				// tuple or digest. It is nevertheless a request-surface creation
				// failure and must close the redacted lifecycle exactly once.
				emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
				emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
				finishRequest(reserveErr)
				return finish(LoopResult{Error: fmt.Sprintf("surface_integrity_failure: LLM request channel failed: %v", reserveErr), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			if channel != nil {
				// A callback may implement the full composition shape yet return
				// nil,nil while its qualification is disabled. That is explicitly
				// the S0.5 compatibility path: it has no correlation-bound channel
				// and therefore must not be required to mint a dynamic epoch. Require
				// the epoch only once a real reservation exists and will publish a
				// bound surface.
				if strings.TrimSpace(executionContext.SurfaceEpoch) == "" {
					err := fmt.Errorf("bound tool surface epoch is required")
					requestChannel = channel
					retirePreDispatchIntegrityFailure(err)
					finishRequest(err)
					return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
				}
				provided := channel.ExecutionContext()
				provided.Protocol = strings.TrimSpace(provided.Protocol)
				provided.ConnectionID = strings.TrimSpace(provided.ConnectionID)
				if provided.Protocol == "" || provided.ConnectionID == "" {
					reserveErr = fmt.Errorf("tool surface channel correlation is required")
					requestChannel = channel
					retirePreDispatchIntegrityFailure(reserveErr)
					finishRequest(reserveErr)
					return finish(LoopResult{Error: reserveErr.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
				}
				provided.SurfaceEpoch = executionContext.SurfaceEpoch
				executionContext = provided
				requestChannel = channel
				if renderer, ok := cb.(BoundModelRequestToolSurfaceRenderer); ok {
					// A bound renderer has already published a durable, reservation-owned
					// surface.  Do not reuse the static compatibility path's permissive
					// filtering here: silently sending a subset would make the wire
					// receipt agree with a payload that no longer matches that published
					// surface.  The authorizer remains an execution gate, but it must
					// accept every published definition before this reservation may send.
					requiresPublicationProof := false
					if requirement, ok := requestChannel.(ToolSurfacePublicationProofRequirement); ok {
						requiresPublicationProof = requirement.RequiresPublishedBoundToolSurface()
					}
					if publisher, ok := cb.(PublishedBoundModelRequestToolSurfaceRenderer); ok {
						rendered := publisher.RenderPublishedBoundToolSurface(userText, iteration, executionContext)
						if !rendered.Published {
							failure := strings.TrimSpace(rendered.Failure)
							if failure == "" {
								failure = "correlation-bound renderer did not publish a surface"
							}
							err := fmt.Errorf("surface_integrity_failure: %s", failure)
							retirePreDispatchIntegrityFailure(err)
							finishRequest(err)
							return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
						}
						tools = rendered.Definitions
					} else if requiresPublicationProof {
						err := fmt.Errorf("surface_integrity_failure: correlation-bound request channel requires published surface proof")
						retirePreDispatchIntegrityFailure(err)
						finishRequest(err)
						return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
					} else {
						tools = renderer.BuildToolsForBoundModelRequest(userText, iteration, executionContext)
					}
					if err := validateBoundToolSurfaceAuthorization(cb, tools); err != nil {
						retirePreDispatchIntegrityFailure(err)
						finishRequest(err)
						return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
					}
				} else {
					err := fmt.Errorf("bound tool surface renderer is required")
					retirePreDispatchIntegrityFailure(err)
					finishRequest(err)
					return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
				}
			} else {
				// A host can implement the provider for a subset of reviewed
				// transports. nil,nil means this configuration remains on its
				// existing compatibility path; it must not be used to materialize
				// a correlation-bound dynamic surface.
				tools = buildToolsForModelRequest(cb, userText, iteration)
			}
		} else {
			tools = buildToolsForModelRequest(cb, userText, iteration)
		}
		// The renderer result is still callback-owned memory at this point. Freeze
		// it before *any* observer can run: input-breakdown telemetry precedes the
		// lifecycle manifest on the static path, and either observer may be
		// synchronous. Bound publication already happened above; this snapshot is
		// solely the immutable model-facing presentation of that published surface.
		// A static request uses the same rule so its later manifest and final wire
		// body cannot be changed by a diagnostic callback in between.
		// The OpenAI compatibility builder may reduce unsupported JSON-Schema
		// keywords for the configured provider. Freeze that same provider-visible
		// projection before creating the receipt manifest; otherwise the final
		// verifier would compare the rendered schema with a deliberately reduced
		// wire schema and block a request before it reaches the provider.
		if requestChannel == nil {
			tools = prepareToolsForToolSurfaceReceipt(aggCFG, tools)
		}
		frozenTools, freezeErr := freezeToolSurfaceDefinitions(tools)
		if freezeErr != nil {
			failureErr := fmt.Errorf("surface_integrity_failure: %w", freezeErr)
			if requestChannel != nil {
				retirePreDispatchIntegrityFailure(failureErr)
			} else {
				// There is no static manifest yet to project. Close the diagnostic
				// lifecycle explicitly with no digest, just as a manifest-construction
				// failure does, rather than letting a later request appear to settle it.
				emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
				emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
			}
			finishRequest(failureErr)
			return finish(LoopResult{Error: failureErr.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
		}
		tools = frozenTools
		// A correlation-bound surface is never the static S0.5 compatibility
		// path: its renderer published a request-local dynamic surface against the
		// live reservation. It must therefore provide immutable *available* plan
		// evidence and a channel that accepts that exact record before any send.
		// Unavailable evidence is reserved exclusively for unbound static HTTP
		// requests; accepting it here would let a dynamic surface bypass its plan
		// / omission audit proof.
		var surfaceAuditEvidence ToolSurfacePlanEvidence
		if requestChannel != nil {
			provider, supported := cb.(ToolSurfaceAuditEvidenceProvider)
			if !supported {
				err := fmt.Errorf("surface_integrity_failure: correlation-bound request surface lacks audit evidence provider")
				retirePreDispatchIntegrityFailure(err)
				finishRequest(err)
				return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			surfaceAuditEvidence = provider.ToolSurfaceAuditEvidence(executionContext)
			if !surfaceAuditEvidence.Available {
				err := fmt.Errorf("surface_integrity_failure: correlation-bound request surface has unavailable audit evidence")
				retirePreDispatchIntegrityFailure(err)
				finishRequest(err)
				return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			// The provider may have assembled Omitted from a planner-owned slice.
			// Normalize and copy it before *any* manifest/event observer runs, then
			// use this one request-owned value for manifest, channel handoff and
			// post-dispatch receipt verification. Otherwise an observer could change
			// the provider-owned slice after the manifest is emitted and make the
			// same reservation disagree with its own final receipt.
			normalizedEvidence, normalizeErr := NormalizeToolSurfacePlanEvidence(surfaceAuditEvidence)
			if normalizeErr != nil {
				normalizeErr = fmt.Errorf("surface_integrity_failure: normalize correlation-bound audit evidence: %w", normalizeErr)
				retirePreDispatchIntegrityFailure(normalizeErr)
				finishRequest(normalizeErr)
				return finish(LoopResult{Error: normalizeErr.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			surfaceAuditEvidence = normalizedEvidence
			// The renderer has already published this durable, reservation-owned
			// surface and the holder just supplied its immutable plan evidence.
			// Emit the manifest before asking the channel to accept evidence so a
			// setter failure is still visible as “manifest created → integrity
			// failure → terminal”, rather than looking as if no surface existed.
			manifest, err := emitToolSurfaceManifestCreated(eventObserver, tools, invocationPolicy, surfaceAuditEvidence)
			if err != nil {
				retirePreDispatchIntegrityFailure(fmt.Errorf("surface_integrity_failure: manifest telemetry: %w", err))
				finishRequest(err)
				return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			boundSurfaceManifest = &manifest
			// A correlation-bound channel serializes its own final bytes. It must
			// atomically accept the manifest's audit evidence and host-owned policy
			// before it can write a frame. Separate setters allow an implementation
			// to observe a half-configured reservation between calls.
			preparationSetter, supported := requestChannel.(ToolSurfaceDispatchPreparationRequestChannel)
			if !supported {
				err := fmt.Errorf("surface_integrity_failure: correlation-bound request channel cannot atomically carry dispatch preparation")
				retirePreDispatchIntegrityFailure(err)
				finishRequest(err)
				return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			if err := preparationSetter.SetToolSurfaceDispatchPreparation(ToolSurfaceDispatchPreparation{AuditEvidence: surfaceAuditEvidence, InvocationPolicy: invocationPolicy}); err != nil {
				retirePreDispatchIntegrityFailure(err)
				finishRequest(err)
				return finish(LoopResult{Error: err.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
		}
		// Input accounting is an audit of the request about to leave the host, not
		// of the broad BuildTools compatibility inventory held before rendering.
		// Request-local renderers may remove legacy entries or mint governed
		// aliases, so sampling before this line reports a ghost surface and makes
		// routing/cost telemetry disagree with the model's actual definitions.
		observeLoopInputBreakdown(cb, reqConversation, tools)

		// Capture the epoch at the actual request boundary. A retry below is a
		// distinct model request and therefore receives a distinct epoch too.
		attemptObserver, observesAttempts := cb.(ToolSurfaceAttemptObserver)
		containAmbiguousDelivery := false
		if containment, ok := cb.(ToolSurfaceAmbiguousDeliveryContainment); ok {
			containAmbiguousDelivery = containment.ContainToolSurfaceAmbiguousDelivery()
		}
		attemptStarted := false
		attemptFinished := false
		// A reserved channel owns both a transport lifetime and a semantic
		// surface lifetime. The former ends at channel.Close; the latter must be
		// explicitly disposed after the loop decides what happened to the
		// response. Keep this per-request closure local so retries/successors
		// cannot accidentally settle a predecessor context.
		startAttempt := func() {
			if requestChannel == nil {
				staticAttemptTerminalSent = false
			}
			attemptStarted = true
			attemptFinished = false
			if observesAttempts {
				attemptObserver.OnToolSurfaceAttemptStarted(executionContext)
			}
		}
		finishAttempt := func(delivery ToolSurfaceDeliveryState) {
			if !attemptStarted || attemptFinished {
				return
			}
			attemptFinished = true
			if observesAttempts {
				attemptObserver.OnToolSurfaceAttemptFinished(executionContext, delivery)
			}
		}
		// Verify the exact final HTTP body at RoundTrip time. This is stricter
		// than observing the renderer output: provider adapters are free to
		// translate envelopes, but they may not silently drop, retain, append, or
		// rename definitions. The wrapper is request-local so no receipt can be
		// inherited by a retry or successor surface. Correlation-bound channels
		// must provide the same verification inside their single-request Do
		// ownership boundary; no current production callback materializes that
		// path while qualification remains disabled.
		var receiptClient *http.Client
		if requestChannel == nil {
			lifecycle, receiptErr := newToolSurfaceReceiptHTTPClientWithLifecycleEvents(httpClient, tools, invocationPolicy, ToolSurfacePlanEvidence{}, toolSurfaceLifecycleReceiptObserverFor(cb), eventObserver)
			if receiptErr != nil {
				finishRequest(receiptErr)
				return finish(LoopResult{Error: receiptErr.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			receiptClient = lifecycle.client
			staticAttemptManifest = &lifecycle.manifest
			// The lifecycle snapshot was frozen before its manifest was emitted.
			// Continue with that same request-owned surface so a synchronous event
			// observer cannot mutate callback-owned definitions between telemetry
			// creation and JSON serialization.
			tools = lifecycle.definitions
		}
		startAttempt()
		var resp *llm.Response
		var err error
		if requestChannel != nil {
			// A reserved channel is one request attempt. It owns no transparent
			// fallback/retry/reconnect successor; RunLoop must reserve a fresh
			// channel before rendering any successor request surface.
			// A correlation-bound channel owns its wire framing and is therefore
			// responsible for verifying the same manifest/receipt contract before
			// it writes request bytes. Current production qualification keeps that
			// path disabled; the compatibility path below is verified directly.
			verifiedChannel, verified := requestChannel.(VerifiedToolSurfaceRequestChannel)
			if !verified {
				err = fmt.Errorf("surface_integrity_failure: correlation-bound request channel must return a verified dispatch")
				reportReceipt(integrityFailureReceipt(err))
				disposeSurface(ToolSurfaceIntegrityFailure)
				requestChannel.Close(err)
			} else {
				// A channel owns serialization, and an SDK/adapter may mutate the
				// slice it receives while preparing its final frame. Keep RunLoop's
				// frozen rendered surface separate from that channel-owned input:
				// otherwise a channel could change definitions, create a matching
				// receipt for the changed payload, and have the post-dispatch verifier
				// accidentally hash those same changed maps. The retained `tools`
				// snapshot is the only baseline accepted for bind authority.
				channelTools, cloneErr := freezeToolSurfaceDefinitions(tools)
				if cloneErr != nil {
					err = fmt.Errorf("surface_integrity_failure: %w", cloneErr)
					reportReceipt(integrityFailureReceipt(err))
					disposeSurface(ToolSurfaceIntegrityFailure)
					requestChannel.Close(err)
				} else {
					dispatch, dispatchErr := verifiedChannel.DoVerified(requestCtx, reqConversation, channelTools, cb.OnToken, true)
					// DoVerified is the only qualified correlation-bound dispatch
					// seam, so even a broken channel must leave one explicit receipt
					// record.  Reporting a zero value here would make a reserved,
					// attempted dispatch indistinguishable from an observer omission;
					// it also violates the pre-dispatch integrity ledger's requirement
					// for a no-digest failure receipt.  A non-zero receipt remains
					// diagnostic evidence and is verified below before it can bind.
					if dispatch.Receipt == (ToolSurfaceReceipt{}) {
						// Without the required receipt we also lack the only
						// trustworthy handoff state.  Treat it as ambiguous rather
						// than claiming that bytes were not sent: DoVerified could
						// have failed after a write but before it assembled its
						// result. The integrity failure still retires the surface.
						missingReceipt := integrityFailureReceipt(fmt.Errorf("verified dispatch returned no receipt"))
						missingReceipt.Handoff = ToolSurfaceHandoffAmbiguous
						reportReceipt(missingReceipt)
					} else {
						reportReceipt(dispatch.Receipt)
					}
					if receiptErr := VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(tools, invocationPolicy, surfaceAuditEvidence, dispatch.Receipt); receiptErr != nil {
						err = receiptErr
						disposeSurface(ToolSurfaceIntegrityFailure)
						requestChannel.Close(err)
					} else if dispatch.Receipt.Handoff == ToolSurfaceHandoffAmbiguous {
						resp, err = dispatch.Response, dispatchErr
						if err == nil {
							err = fmt.Errorf("transport handoff is ambiguous without a transport error")
						}
						requestChannel.Close(err)
					} else if dispatch.Receipt.Handoff != ToolSurfaceHandoffStarted {
						err = fmt.Errorf("surface_integrity_failure: verified dispatch did not start transport handoff")
						disposeSurface(ToolSurfaceIntegrityFailure)
						requestChannel.Close(err)
					} else if dispatch.Response == nil && dispatchErr == nil {
						// A channel that claims a verified, started dispatch must return
						// either the response it owns or the transport/read error that
						// prevented one. Treat nil,nil as a broken dispatch contract,
						// never as a bindable response (or a later nil dereference).
						err = fmt.Errorf("surface_integrity_failure: verified dispatch started without response or error")
						disposeSurface(ToolSurfaceIntegrityFailure)
						requestChannel.Close(err)
					} else {
						resp, err = dispatch.Response, dispatchErr
						requestChannel.Close(err)
					}
				}
			}
		} else {
			resp, err = doLLMRequestWithToolsStreamWithBeforeFallback(requestCtx, aggCFG, reqConversation, tools, invocationPolicy, receiptClient, cb.OnToken, func() (toolSurfaceFallbackPreparation, error) {
				// The streaming attempt has begun but did not produce a consumable
				// response. A compatibility host must quarantine its static belt before
				// a fallback could render a same-named successor surface.
				finishAttempt(ToolSurfaceAmbiguousDelivery)
				// The stream request was an owner-visible attempt with its own
				// receipt. Retire its telemetry before constructing the fallback
				// surface; the fallback gets a new manifest, receipt, and terminal.
				disposeSurface(ToolSurfaceTransportFailure)
				if containAmbiguousDelivery {
					return toolSurfaceFallbackPreparation{}, errToolSurfaceFallbackSuppressed
				}
				// The predecessor is already terminal. From this point forward any
				// failure belongs to a prospective successor and must never reuse
				// the predecessor manifest or terminal projection.
				staticAttemptManifest = nil
				staticAttemptTerminalSent = false
				successorPolicy, successorPolicyErr := toolSurfaceInvocationPolicyForRequest(cb, aggCFG, iteration)
				if successorPolicyErr != nil {
					preparationErr := fmt.Errorf("surface_integrity_failure: successor invocation policy: %w", successorPolicyErr)
					emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
					emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
					staticAttemptTerminalSent = true
					return toolSurfaceFallbackPreparation{}, preparationErr
				}
				// A fallback is another real outbound request. Do not retain the
				// predecessor's policy snapshot: hosts may have changed an
				// explicit choice/parallel control while the stream was failing.
				invocationPolicy = successorPolicy
				// The fallback is a new outbound request. Advance its execution
				// epoch before rendering its replacement surface, exactly as the
				// initial request does. Rendering first leaves a window in which a
				// callback has installed successor definitions but still accepts a
				// delayed predecessor response under the old epoch.
				executionContext = beginToolCallExecutionContext(cb, iteration)
				tools = prepareToolsForToolSurfaceReceipt(aggCFG, buildToolsForModelRequest(cb, userText, iteration))
				lifecycle, rebuildErr := newToolSurfaceReceiptHTTPClientWithLifecycleEvents(httpClient, tools, invocationPolicy, ToolSurfacePlanEvidence{}, toolSurfaceLifecycleReceiptObserverFor(cb), eventObserver)
				if rebuildErr != nil {
					// The lifecycle emitted its own redacted pre-manifest integrity
					// terminal. Do not let the outer error path relabel it as a
					// transport failure or attach the predecessor manifest.
					staticAttemptTerminalSent = true
					return toolSurfaceFallbackPreparation{}, fmt.Errorf("surface_integrity_failure: successor lifecycle: %w", rebuildErr)
				}
				receiptClient = lifecycle.client
				staticAttemptManifest = &lifecycle.manifest
				tools = lifecycle.definitions
				// The non-stream fallback is a new request, even though it reuses the
				// same conversation. Account for the definitions that will actually be
				// sent on this fallback surface rather than leaving telemetry at the
				// predecessor stream attempt.
				observeLoopInputBreakdown(cb, reqConversation, tools)
				startAttempt()
				return toolSurfaceFallbackPreparation{
					Definitions:      tools,
					HTTPClient:       receiptClient,
					InvocationPolicy: invocationPolicy,
				}, nil
			})
		}
		if err != nil {
			finishAttempt(ToolSurfaceAmbiguousDelivery)
		}
		// A host may cancel just this request because new live user steering
		// arrived. Finish the operation before continuing; the next iteration's
		// TransformConversation call will append the steering message.
		if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
			disposeSurface(ToolSurfaceSteered)
			finishRequest(err)
			// Steering replaces this interrupted model attempt; it must not spend
			// one of the task's reasoning iterations (especially when maxIter=1).
			freeReplans++
			if freeReplans <= maxFreeReplansPerLoop {
				iteration--
			}
			continue
		}
		if err != nil {
			disposeSurface(ToolSurfaceTransportFailure)
			// Retry with exponential backoff for transient errors (503, timeout, network).
			// SubAgent tasks should be resilient to brief API outages.
			// A partial streamed response has already been rendered. Retrying it
			// would duplicate that visible output in the same assistant message.
			canRetry := requestChannel == nil && !containAmbiguousDelivery && resp == nil && requestBaseCtx.Err() == nil && requestCtx.Err() == nil
			for retryAttempt := 1; retryAttempt <= maxLLMRetries && canRetry && shouldRetrySimpleLLMError(err); retryAttempt++ {
				backoff := llmRetryBackoff(retryAttempt)
				log.Printf("[agent-loop] LLM error (attempt %d/%d), retrying in %s: %v", retryAttempt, maxLLMRetries, backoff, err)
				// A live steer can arrive while the loop is backing off from a
				// transient provider error. Poll the replan revision so the user does
				// not wait 2-6 seconds and then watch stale context retry first.
				deadline := time.NewTimer(backoff)
				ticker := time.NewTicker(50 * time.Millisecond)
				interruptedForReplan := false
			waitBackoff:
				for {
					select {
					case <-deadline.C:
						break waitBackoff
					case <-ticker.C:
						if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
							interruptedForReplan = true
							break waitBackoff
						}
						if cb.ShouldStop() {
							break waitBackoff
						}
					}
				}
				if !deadline.Stop() {
					select {
					case <-deadline.C:
					default:
					}
				}
				ticker.Stop()
				if interruptedForReplan {
					break
				}
				if cb.ShouldStop() {
					// The predecessor request was already retired before entering this
					// retry loop. Do not call disposeSurface here: it would either try
					// to settle that predecessor twice or obscure which request was
					// cancelled. No successor manifest or transport handoff exists.
					finishRequest(err)
					return finish(LoopResult{Error: "cancelled during LLM retry", Iterations: iteration, ToolCalls: totalToolCalls})
				}
				// A retry is a fresh HTTP operation, so it gets a fresh deadline from
				// the active route. In particular, do not reuse an already-expired
				// request context after a genuine route timeout.
				cancelRequestTimeout()
				requestCtx, cancelRequestTimeout = llmRequestContextWithTimeout(requestBaseCtx, aggCFG)
				// The predecessor was already retired before this retry loop. Clear
				// its projection before preparing the successor so a pre-send failure
				// cannot accidentally emit a second terminal event for the old
				// manifest.
				staticAttemptManifest = nil
				// Like the stream fallback, an outer retry owns a successor
				// request. Issue its epoch before the renderer replaces callback
				// state, so old responses become stale before any successor
				// definitions are visible to execution admission.
				executionContext = beginToolCallExecutionContext(cb, iteration)
				tools = prepareToolsForToolSurfaceReceipt(aggCFG, buildToolsForModelRequest(cb, userText, iteration))
				retryPolicy, retryPolicyErr := toolSurfaceInvocationPolicyForRequest(cb, aggCFG, iteration)
				if retryPolicyErr != nil {
					err = fmt.Errorf("surface_integrity_failure: %w", retryPolicyErr)
					emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
					emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
					staticAttemptTerminalSent = true
					break
				}
				// An outer retry is a fresh owner-visible request; its policy is
				// fetched and frozen independently of the retired predecessor.
				invocationPolicy = retryPolicy
				// This retry is a new outbound request. Freeze its renderer output
				// before input accounting can synchronously observe/mutate callback
				// state; the later manifest and wire payload must describe this exact
				// retry-owned snapshot.
				frozenRetryTools, freezeErr := freezeToolSurfaceDefinitions(tools)
				if freezeErr != nil {
					err = fmt.Errorf("surface_integrity_failure: %w", freezeErr)
					emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
					emitToolSurfaceEvent(eventObserver, ToolSurfaceEvent{Kind: ToolSurfaceEventTerminalReason, TerminalReason: ToolSurfaceIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
					// No retry request was started and no manifest exists. Mark this
					// request slot terminal so the common outer error path cannot add
					// a transport-failure terminal for a predecessor projection.
					staticAttemptTerminalSent = true
					break
				}
				tools = frozenRetryTools
				// Each outer retry is a separate model request and can receive a
				// different request-local surface. Record exactly that surface.
				observeLoopInputBreakdown(cb, reqConversation, tools)
				lifecycle, lifecycleErr := newToolSurfaceReceiptHTTPClientWithLifecycleEvents(httpClient, tools, invocationPolicy, ToolSurfacePlanEvidence{}, toolSurfaceLifecycleReceiptObserverFor(cb), eventObserver)
				if lifecycleErr != nil {
					err = lifecycleErr
					// lifecycle already emitted its pre-manifest integrity terminal;
					// no actual transport attempt started here.
					staticAttemptTerminalSent = true
				} else {
					receiptClient = lifecycle.client
					staticAttemptManifest = &lifecycle.manifest
					tools = lifecycle.definitions
					startAttempt()
				}
				if err == nil {
					resp, err = doLLMRequestWithTools(requestCtx, aggCFG, reqConversation, tools, invocationPolicy, receiptClient)
				}
				if err != nil {
					finishAttempt(ToolSurfaceAmbiguousDelivery)
				}
			}
			if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
				disposeSurface(ToolSurfaceSteered)
				finishRequest(err)
				freeReplans++
				if freeReplans <= maxFreeReplansPerLoop {
					iteration--
				}
				continue
			}
			finishRequest(err)
			if err != nil {
				disposeSurface(ToolSurfaceTransportFailure)
				return finish(LoopResult{Error: fmt.Sprintf("LLM call failed: %s", llm.UserFacingError(err)), Iterations: iteration, ToolCalls: totalToolCalls})
			}
		} else {
			// Every completed model-request path must provide either a consumable
			// response or an error. The correlation-bound channel enforces this at
			// its verified dispatch boundary; keep the same fail-closed invariant
			// here for static HTTP/SSE compatibility adapters as well.
			if responseErr := requireLLMDispatchResponse(resp); responseErr != nil {
				finishAttempt(ToolSurfaceAmbiguousDelivery)
				disposeSurface(ToolSurfaceIntegrityFailure)
				finishRequest(responseErr)
				return finish(LoopResult{Error: responseErr.Error(), Iterations: iteration, ToolCalls: totalToolCalls})
			}
			finishRequest(nil)
		}
		// Cover steering that landed after the HTTP request completed but before
		// its response was committed as the visible answer.
		if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
			disposeSurface(ToolSurfaceSteered)
			freeReplans++
			if freeReplans <= maxFreeReplansPerLoop {
				iteration--
			}
			continue
		}
		// Account aggregator usage with aggregator config (sticky model → force after).
		if resp != nil && resp.Usage != nil {
			round := TurnUsageFromLLM(aggCFG, resp.Usage)
			usage.Add(round)
			// Prefer aggregator as the turn's primary model label (sticky first-write).
			if usage.Model == "" || usage.Requests == 1 || strings.TrimSpace(aggCFG.Model) != "" {
				// After first Add, Model may be a reference model — overwrite to aggregator for chip.
				if strings.TrimSpace(aggCFG.Model) != "" {
					usage.Model = aggCFG.Model
				}
				if strings.TrimSpace(aggCFG.ProviderName) != "" {
					usage.Provider = aggCFG.ProviderName
				}
			}
			if rec, ok := cb.(LLMUsageRecorder); ok && (round.InputTokens > 0 || round.OutputTokens > 0) {
				model := aggCFG.Model
				if model == "" {
					model = usage.Model
				}
				rec.OnLLMUsage(model, round.InputTokens, round.OutputTokens)
			}
		}

		if len(resp.Choices) == 0 {
			// A parsed response with no choices was nevertheless consumed from this
			// concrete request. Record that fact before retiring its semantic
			// surface; otherwise attempt telemetry leaves a started request open and
			// can later be mistaken for an ambiguous delivery/successor candidate.
			finishAttempt(ToolSurfaceResponseConsumed)
			executionContext.ResponseID = strings.TrimSpace(resp.ResponseID)
			disposeSurface(ToolSurfaceResponseAbandoned)
			if h.OnEmptyResponse(iteration) {
				continue // hook says retry
			}
			return finish(LoopResult{Error: "LLM returned no choices", Iterations: iteration, ToolCalls: totalToolCalls})
		}
		// This response was normally returned to and consumed by the current loop
		// attempt. The state is still not provider correlation; it only lets a
		// compatibility observer distinguish this path from an ambiguous error.
		finishAttempt(ToolSurfaceResponseConsumed)

		// Bind every execution in this response to the provider correlation seen
		// at the request boundary. A dynamic host may fail closed when the
		// provider omitted it; ordinary stateless tools retain their existing
		// compatibility behavior.
		executionContext.ResponseID = strings.TrimSpace(resp.ResponseID)
		if binder, ok := cb.(ToolSurfaceResponseBinder); ok {
			if bindErr := binder.BindToolSurfaceResponse(executionContext); bindErr != nil {
				executionContext.ResponseBindingError = bindErr.Error()
				// A response whose trusted binding failed cannot authorize any tool
				// dispatch. Passing the error through to a dynamic executor is not
				// sufficient: a compatibility executor may not understand binding
				// metadata and could otherwise execute a call after this surface was
				// retired. Reject the whole response before any tool/history path.
				disposeSurface(ToolSurfaceResponseAbandoned)
				return finish(LoopResult{
					Error:      fmt.Sprintf("surface_integrity_failure: response binding failed: %v", bindErr),
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
				})
			}
		}

		choice := resp.Choices[0]
		reasoningContent := StripRolePrefixHallucinationLeading(choice.Message.ReasoningContent)
		appendLoopDisplayReasoning(&displayReasoning, reasoningContent)
		content := choice.Message.Content
		if content == "" && reasoningContent != "" {
			content = reasoningContent
		}
		content = StripRolePrefixHallucination(content)

		if len(choice.TruncatedToolNames) > 0 {
			disposeSurface(ToolSurfaceResponseAbandoned)
			truncatedList := strings.Join(choice.TruncatedToolNames, ", ")
			log.Printf("[agent-loop] truncated tool call recovery (iteration=%d tools=%s valid_tools=%d)", iteration, truncatedList, len(choice.Message.ToolCalls))

			// Best-effort partial write: if write_file was truncated and we have
			// the raw (incomplete) args, extract path+content and write to disk.
			// This converts a failed call into a partially successful one.
			var recoveryPrompt string
			if rawArgs := truncatedToolArgsLookup(choice.TruncatedToolArgs, "write_file"); rawArgs != "" {
				if pw := attemptLoopPartialWriteFile(rawArgs); pw != nil {
					recoveryPrompt = buildLoopPartialWriteRecovery(pw)
					// Made progress — reset drift/failure counters.
					consecutiveSame = 0
					consecutiveSameToolFailures = 0
					lastFailedTool = ""
					sameToolFailureGuidanceInjected = false
				} else {
					// Partial write not possible. If file already exists from a
					// previous partial write, instruct LLM to use mode=append.
					if absPath := resolvePartialWritePath(rawArgs); absPath != "" {
						if info, statErr := os.Stat(absPath); statErr == nil && info.Size() > 0 {
							recoveryPrompt = buildLoopPartialWriteAppendHint(absPath, info.Size())
						}
					}
				}
			}
			if recoveryPrompt == "" {
				recoveryPrompt = buildToolCallTruncationRecovery(choice.TruncatedToolNames, tools, userText)
			}

			if strings.TrimSpace(content) != "" || reasoningContent != "" {
				assistantMsg := map[string]interface{}{
					"role":    "assistant",
					"content": content,
				}
				if reasoningContent != "" {
					assistantMsg["reasoning_content"] = reasoningContent
				} else {
					assistantMsg["reasoning_content"] = ""
				}
				conversation = append(conversation, assistantMsg)
				historyDelta = append(historyDelta, ConversationEntry{
					Role:             "assistant",
					Content:          content,
					ReasoningContent: reasoningContent,
					FinishReason:     ResolveAssistantFinishReason(choice.FinishReason, false),
				})
			}
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": recoveryPrompt,
			})
			historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: recoveryPrompt})
			continue
		}

		if invalidToolNames := invalidLoopToolArgumentNames(choice.Message.ToolCalls); len(invalidToolNames) > 0 {
			disposeSurface(ToolSurfaceResponseAbandoned)
			invalidList := strings.Join(invalidToolNames, ", ")
			log.Printf("[agent-loop] invalid tool call argument recovery (iteration=%d tools=%s)", iteration, invalidList)
			recoveryPrompt := buildToolCallTruncationRecovery(invalidToolNames, tools, userText)
			if strings.TrimSpace(content) != "" || reasoningContent != "" {
				assistantMsg := map[string]interface{}{
					"role":    "assistant",
					"content": content,
				}
				if reasoningContent != "" {
					assistantMsg["reasoning_content"] = reasoningContent
				} else {
					assistantMsg["reasoning_content"] = ""
				}
				conversation = append(conversation, assistantMsg)
				historyDelta = append(historyDelta, ConversationEntry{
					Role:             "assistant",
					Content:          content,
					ReasoningContent: reasoningContent,
					FinishReason:     ResolveAssistantFinishReason(choice.FinishReason, false),
				})
			}
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": recoveryPrompt,
			})
			historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: recoveryPrompt})
			continue
		}

		// The model may have completed a tool decision just as live steering
		// arrived. Re-check before committing that assistant/tool-call turn or
		// executing any side effect; the user's newer direction must get a chance
		// to replace stale tool arguments.
		if len(choice.Message.ToolCalls) > 0 {
			if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
				disposeSurface(ToolSurfaceSteered)
				freeReplans++
				if freeReplans <= maxFreeReplansPerLoop {
					iteration--
				}
				continue
			}
		}

		// Track consecutive empty responses for hard exit.
		if strings.TrimSpace(content) == "" && len(choice.Message.ToolCalls) == 0 {
			// The provider response was consumed, but it did not produce a usable
			// assistant turn. Retire this exact surface before any backoff, steering
			// check, or recovery prompt can create the next request surface.
			disposeSurface(ToolSurfaceResponseAbandoned)
			consecutiveEmpty++
			snippetLen := len([]rune(lastToolOutcome.snippet))
			log.Printf("[agent-loop] empty response #%d (iteration=%d, lastTool=%s, outcome=%d, snippet_len=%d)",
				consecutiveEmpty, iteration, lastToolName, lastToolOutcome.kind, snippetLen)
			if consecutiveEmpty >= maxConsecutiveEmpty {
				log.Printf("[agent-loop] hard exit: %d consecutive empty responses", consecutiveEmpty)
				// Return the last non-empty content as a fallback.
				return finish(LoopResult{
					Text:       lastNonEmptyContent,
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
					HardExit:   true,
				})
			}

			// Brief pause before retry to avoid rapid-fire empty requests. Keep it
			// steer-aware so a user correction is not stuck behind a 1-5s sleep.
			emptyBackoff := time.NewTimer(time.Duration(consecutiveEmpty) * time.Second)
			emptyTicker := time.NewTicker(50 * time.Millisecond)
			emptyReplan := false
		emptyWait:
			for {
				select {
				case <-emptyBackoff.C:
					break emptyWait
				case <-emptyTicker.C:
					if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
						emptyReplan = true
						break emptyWait
					}
					if cb.ShouldStop() {
						break emptyWait
					}
				}
			}
			if !emptyBackoff.Stop() {
				select {
				case <-emptyBackoff.C:
				default:
				}
			}
			emptyTicker.Stop()
			if emptyReplan {
				disposeSurface(ToolSurfaceSteered)
				freeReplans++
				if freeReplans <= maxFreeReplansPerLoop {
					iteration--
				}
				continue
			}
			if cb.ShouldStop() {
				disposeSurface(ToolSurfaceRuntimeTerminal)
				return finish(LoopResult{Error: "cancelled", Iterations: iteration, ToolCalls: totalToolCalls})
			}

			// Build a context-aware recovery prompt.
			recoverPrompt := buildEmptyResponseRecovery(consecutiveEmpty, lastToolName, lastToolOutcome, userText)
			workingState, _ = applyWorkingStateEmpty(workingState, userText, lastToolName, consecutiveEmpty, executedTools, loopProjectedGoal(cb))
			if iteration+1 >= maxIter {
				continue
			}
			recoverPrompt = AppendNextHint(recoverPrompt, workingState)

			// Inject a recover prompt to nudge the LLM.
			conversation = append(conversation, map[string]interface{}{
				"role":              "assistant",
				"content":           content,
				"reasoning_content": "", // DeepSeek V4+: must exist on all assistant messages
			})
			historyDelta = append(historyDelta, ConversationEntry{
				Role:         "assistant",
				Content:      content,
				FinishReason: ResolveAssistantFinishReason(choice.FinishReason, false),
			})
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": recoverPrompt,
			})
			historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: recoverPrompt})
			continue
		}
		consecutiveEmpty = 0
		if strings.TrimSpace(content) != "" {
			lastNonEmptyContent = content
		}

		// Before committing a final text response, atomically close live-steer
		// acceptance. This covers the narrow gap after the earlier revision check:
		// an accepted interruption either forces regeneration or is rejected by
		// the host and remains queued for the next normal turn.
		hasToolCalls := len(choice.Message.ToolCalls) > 0
		if !hasToolCalls {
			if guard, ok := cb.(LLMFinalizationGuard); ok && !guard.TryFinalizeLLMResponse() {
				disposeSurface(ToolSurfaceSteered)
				freeReplans++
				if freeReplans <= maxFreeReplansPerLoop {
					iteration--
				}
				continue
			}
		}

		// Build assistant message for conversation history.
		batchDeltaStart := len(historyDelta)
		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": content,
		}
		if reasoningContent != "" {
			assistantMsg["reasoning_content"] = reasoningContent
		} else {
			// DeepSeek V4+ thinking mode: when tools are present in the
			// request, reasoning_content must exist on ALL assistant messages.
			// An empty string is accepted. For non-DeepSeek providers, the
			// field is simply ignored.
			assistantMsg["reasoning_content"] = ""
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)
		historyDelta = append(historyDelta, ConversationEntry{
			Role:             "assistant",
			Content:          content,
			ReasoningContent: reasoningContent,
			ToolCalls:        choice.Message.ToolCalls,
			FinishReason:     ResolveAssistantFinishReason(choice.FinishReason, hasToolCalls),
		})

		// No tool calls → final answer.
		if !hasToolCalls {
			// A malformed content tool-markup interception is a transport-level
			// artifact, not a user-meaningful answer: the model tried to emit a
			// tool call as raw markup and the llm layer replaced the content
			// with the interception notice. Re-ask once for a plain-text reply
			// instead of shipping that notice as the final text — otherwise it
			// overwrites an already-completed turn's good answer (e.g. after a
			// finish-nudge iteration).
			if strings.TrimSpace(content) == llm.MalformedContentToolCallErrorMsg && malformedReprompts == 0 && iteration+1 < maxIter {
				malformedReprompts++
				// Same lifecycle rule as the finish-nudge continue below: this
				// request's surface reservation must receive its one terminal
				// disposition before the next iteration re-requests.
				disposeSurface(ToolSurfaceResponseSettled)
				reprompt := "[系统] 上一条回复包含无法解析的工具调用标记，已被拦截。请直接用纯文本给出最终答复，不要输出任何工具调用标记。"
				conversation = append(conversation, map[string]interface{}{
					"role":    "user",
					"content": reprompt,
				})
				historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: reprompt})
				continue
			}
			attached := ShouldAttachWorkingState(loopPromptProfile(cb), WorkingStateDisabled(), workingState)
			// Do not spend the last iteration on a nudge — that turns a
			// deliverable answer into "max iterations reached".
			if ShouldBlockFinish(workingState, userText, attached) && iteration+1 < maxIter {
				disposeSurface(ToolSurfaceResponseSettled)
				workingState.FinishNudges++
				nudge := AppendNextHint(FinishNudgeMessage(), workingState)
				conversation = append(conversation, map[string]interface{}{
					"role":    "user",
					"content": nudge,
				})
				historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: nudge})
				continue
			}
			finalText := StripThinkingTags(content)
			// Note: we do NOT call cb.OnToken here. The final text is returned
			// via LoopResult.Text, and the caller (handleChatSend) sends it as
			// ChatResponseMsg. Calling OnToken would cause duplicate display.
			// Keep the last assistant entry's content as the cleaned final text.
			if n := len(historyDelta); n > 0 && historyDelta[n-1].Role == "assistant" {
				historyDelta[n-1].Content = finalText
			}
			disposeSurface(ToolSurfaceResponseSettled)
			return finish(LoopResult{Text: finalText, Iterations: iteration + 1, ToolCalls: totalToolCalls})
		}

		// Persist a valid conversation prefix before any tool begins. The result
		// of the imminent call is not yet known, so every such checkpoint is
		// deliberately classified as externally uncertain. Hosts must resume by
		// asking the model for a new forward decision, never by replaying it.
		toolBatchSequence++
		batchMeta := ToolBatchMetadata{
			Sequence:        toolBatchSequence,
			LastToolName:    strings.TrimSpace(choice.Message.ToolCalls[0].Function.Name),
			SideEffectState: "external_uncertain",
		}
		if starter, ok := h.(ToolBatchStarter); ok {
			batch := append([]ConversationEntry(nil), historyDelta[batchDeltaStart:]...)
			if err := starter.OnToolBatchStarting(batch, batchMeta); err != nil {
				disposeSurface(ToolSurfaceResponseAbandoned)
				return finish(LoopResult{
					Text:       "Unable to safely persist tool progress; automatic execution stopped.",
					Error:      "recovery_checkpoint_failed",
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
					HardExit:   true,
				})
			}
		}

		// Execute tool calls.
		var wsBatch workingStateBatch
		// A model response is an atomic tool-call batch: no next LLM request can
		// observe a replacement surface until every call has been paired and the
		// host has committed the batch.  In particular, semantic hosts may defer
		// publishing a dependant grant until OnToolBatchCommitted. Remember each
		// real execution here and refresh the request surface only after that
		// boundary. Keeping every name preserves the refresher contract for hosts
		// that distinguish different completed tools in one model response.
		executedToolNames := make([]string, 0, len(choice.Message.ToolCalls))
		batchHadSuccess := false
		for _, tc := range choice.Message.ToolCalls {
			if cb.ShouldStop() {
				_ = wsBatch.apply(workingState)
				if abandoner, ok := h.(ToolBatchAbandoner); ok {
					abandoner.OnToolBatchAbandoned(batchMeta)
				}
				disposeSurface(ToolSurfaceRuntimeTerminal)
				return finish(LoopResult{Error: "cancelled", Iterations: iteration + 1, ToolCalls: totalToolCalls})
			}
			totalToolCalls++
			argsJSON := normalizeLoopToolArguments(tc.Function.Arguments)
			// A function name returned by the provider is model-controlled data. It
			// may be dispatched only when it exactly matches one of the definitions
			// frozen for this concrete outbound request. Host authorizers are an
			// additional policy boundary, not a substitute for request-surface
			// ownership: authorizing a registry name must not revive an omitted,
			// predecessor-only, or hallucinated function through a name dispatcher.
			//
			// Keep this check in RunLoop rather than relying on individual callback
			// implementations. S0.5 hosts, generic callbacks, and future hosts then
			// share the same exact rendered-name admission rule before any executor
			// (including the epoch-less compatibility fallback) can run.
			if !toolCallNameWasRendered(tools, tc.Function.Name) {
				denial := unrenderedToolCallDeniedMessage(tc.Function.Name)
				if _, ok := succeededToolNames[strings.TrimSpace(tc.Function.Name)]; ok {
					// An already-consumed grant keeps its dedicated denial text; the
					// earlier success still stands and must not be reinterpreted.
					denial = consumedGrantToolCallDeniedMessage(tc.Function.Name)
				} else if petitioner, ok := cb.(ToolCallPetitioner); ok {
					// A governed host may rescue a call that names a real cataloged
					// tool the planner failed to render. The outcome stays an error;
					// the granted message replaces the denial so the model re-issues
					// the call against the widened surface of the next iteration.
					if granted, message := petitioner.PetitionToolCall(tc.Function.Name); granted && strings.TrimSpace(message) != "" {
						denial = message
					}
				}
				execResult := ToolExecutionResult{
					Result:  denial,
					Outcome: ToolExecutionOutcomeError,
				}
				result := execResult.Result
				lastToolName = tc.Function.Name
				lastToolOutcome = toolOutcomeFromExecutionResult(execResult)
				record := toolCallRecord{name: tc.Function.Name, args: argsJSON, result: result}
				recentCalls = append(recentCalls, record)
				if len(recentCalls) > driftWindow*2 {
					recentCalls = recentCalls[len(recentCalls)-driftWindow*2:]
				}
				conversation = append(conversation, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tc.ID,
					"content":      result,
				})
				historyDelta = append(historyDelta, ConversationEntry{
					Role:        "tool",
					Content:     result,
					ToolCallID:  tc.ID,
					ToolName:    tc.Function.Name,
					ToolOutcome: string(execResult.Outcome),
				})
				continue
			}
			// Hard-stop on oversized tool arguments (parity with GUI stream/exec gates).
			// Without this, complete non-stream LLM responses can deliver huge args that
			// never hit the streaming accumulator limit and thrash the loop until maxIter.
			if argSize := len(argsJSON); argSize > llm.MaxToolArgumentsBytes {
				toolName := strings.TrimSpace(tc.Function.Name)
				if toolName == "" {
					toolName = "unknown"
				}
				log.Printf("[agent-loop] rejected oversized tool arguments tool=%q args_len=%d limit=%d", toolName, argSize, llm.MaxToolArgumentsBytes)
				_ = wsBatch.apply(workingState)
				if abandoner, ok := h.(ToolBatchAbandoner); ok {
					abandoner.OnToolBatchAbandoned(batchMeta)
				}
				disposeSurface(ToolSurfaceResponseAbandoned)
				return finish(LoopResult{
					Error:      fmt.Sprintf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, argSize, llm.MaxToolArgumentsBytes),
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
					HardExit:   true,
				})
			}
			execResult, syntheticFailure := validateLoopToolArguments(tc.Function.Name, argsJSON)
			toolExecuted := false
			replanSkip := false
			policyRejected := false
			// Once live steering has invalidated this assistant tool batch, preserve
			// protocol pairing for every remaining tool_call but do not execute its
			// stale side effect. The next outer iteration injects the new user turn.
			if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
				execResult = ToolExecutionResult{
					Result:  "Tool call skipped because newer user guidance arrived before execution.",
					Outcome: ToolExecutionOutcomeError,
				}
				syntheticFailure = true
				replanSkip = true
			}
			if !syntheticFailure {
				execResult, policyRejected = authorizeLoopTool(cb, tc.Function.Name, argsJSON)
				// Light misroute recovery only changes this request's prompt profile;
				// it must not make an unrendered provider function executable. The
				// rendered-name fence above runs first, so a profile upgrade can never
				// reinterpret an omitted/hallucinated call as part of this surface.
				if policyRejected && !lightUpgraded && isLightToolDenyResult(execResult) {
					if tryLightProfileToolRetry(cb, userText, isFirstTurn, tc.Function.Name, &tools, conversation) {
						lightUpgraded = true
						execResult, policyRejected = authorizeLoopTool(cb, tc.Function.Name, argsJSON)
					}
				}
				if !policyRejected {
					cb.OnToolCall(tc.Function.Name)
					execResult = executeAuthorizedLoopToolCallWithContext(cb, tc.Function.Name, argsJSON, tc.ID, executionContext)
					cb.OnToolResult(tc.Function.Name)
					toolExecuted = true
				}
			}
			if toolExecuted && execResult.Outcome == ToolExecutionOutcomeOK {
				succeededToolNames[strings.TrimSpace(tc.Function.Name)] = struct{}{}
			}
			result := execResult.Result

			// Track last tool for empty-response recovery context.
			lastToolName = tc.Function.Name
			lastToolOutcome = toolOutcomeFromExecutionResult(execResult)

			if toolExecuted {
				executedTools++
				if !WorkingStateDisabled() && !syntheticFailure {
					workingState = EnsureWorkingState(workingState, userText, executedTools, loopProjectedGoal(cb))
				}
			}
			if askReq, ok := ParseAskUserResult(result); ok {
				workingState = applyThenSeekUser(workingState, &wsBatch, userText, executedTools, loopProjectedGoal(cb))
				if abandoner, ok := h.(ToolBatchAbandoner); ok {
					abandoner.OnToolBatchAbandoned(batchMeta)
				}
				disposeSurface(ToolSurfaceResponseAbandoned)
				return finish(LoopResult{
					Text:            FormatAskUserForDisplay(askReq),
					AskUser:         askReq,
					PauseToolCallID: tc.ID,
					Iterations:      iteration + 1,
					ToolCalls:       totalToolCalls,
				})
			}
			// Interactive long-form recording: pause for host UI (same shape as ask_user).
			// Hosts that reject (non-desktop IM) should rewrite the tool result before
			// returning the marker so the model can continue with guidance text.
			if recReq, ok := ParseRecordAudioResult(result); ok {
				workingState = applyThenSeekUser(workingState, &wsBatch, userText, executedTools, loopProjectedGoal(cb))
				if abandoner, ok := h.(ToolBatchAbandoner); ok {
					abandoner.OnToolBatchAbandoned(batchMeta)
				}
				disposeSurface(ToolSurfaceResponseAbandoned)
				return finish(LoopResult{
					Text:            FormatRecordAudioForDisplay(recReq),
					RecordAudio:     recReq,
					PauseToolCallID: tc.ID,
					Iterations:      iteration + 1,
					ToolCalls:       totalToolCalls,
				})
			}

			// Determine success for outcome tracking.
			toolSuccess := lastToolOutcome.kind == toolOutcomeOK
			batchHadSuccess = batchHadSuccess || toolSuccess
			// Record policy denies and invalid args once a workspace exists.
			// Replan-skipped stale calls must not look like a new fail.
			// Host policy denials are fail-closed routing guardrails, not
			// diagnosable tool failures: recording them as WorkingState opens
			// leaves a stale "unresolved" item that later triggers a spurious
			// finish-nudge even after the goal was completed through the tools
			// this turn allows. The denial text itself already tells the model
			// to reroute, and the unrendered-name fence above likewise skips
			// the ledger.
			if !WorkingStateDisabled() && !replanSkip && !policyRejected && workingState != nil {
				wsBatch.note(workingState, tc.Function.Name, argsJSON, execResult.Outcome)
			}
			// OnToolExecuted is an execution lifecycle callback, not a generic
			// tool-call observation. Calling it for invalid arguments, a policy
			// denial, or a stale replan skip lets hosts mistake a rejected request
			// for completed work and promote their route or retire state. Those
			// outcomes remain in HistoryDelta and WorkingState above, but only an
			// actual dispatcher invocation earns execution-side hooks.
			if toolExecuted {
				h.OnToolExecuted(tc.Function.Name, argsJSON, result, toolSuccess)
				executedToolNames = append(executedToolNames, tc.Function.Name)
				// Escalation changes the execution budget used when projecting this
				// tool result (for example a document reader's read-back window), so
				// it remains per-execution. Only the model-visible surface refresh is
				// deferred to the committed batch boundary below.
				if toolSuccess {
					if escalator, ok := cb.(ToolExecutionEscalator); ok {
						escalator.EscalateAfterToolExecution(tc.Function.Name)
					}
				}
			}

			// Consecutive same-tool failure tracking.
			if !toolSuccess {
				if tc.Function.Name == lastFailedTool || lastFailedTool == "" {
					lastFailedTool = tc.Function.Name
					consecutiveSameToolFailures++
				}
				// Different tool failing: don't reset — it's likely noise
				// (e.g. an intermittent write_file permission error between
				// bash failures). Only the dominant failing tool matters.

				if consecutiveSameToolFailures >= hardStopSameToolFailures {
					// Hard stop: LLM ignored the guidance and keeps failing.
					log.Printf("[agent-loop] hard stop: tool=%q failed %d consecutive times, force-exiting loop", lastFailedTool, consecutiveSameToolFailures)
					_ = wsBatch.apply(workingState)
					if abandoner, ok := h.(ToolBatchAbandoner); ok {
						abandoner.OnToolBatchAbandoned(batchMeta)
					}
					disposeSurface(ToolSurfaceResponseAbandoned)
					return finish(LoopResult{
						Text:       fmt.Sprintf("工具 %s 连续失败 %d 次，已停止执行。请检查环境或换一种方式完成任务。", lastFailedTool, consecutiveSameToolFailures),
						Iterations: iteration + 1,
						ToolCalls:  totalToolCalls,
						HardExit:   true,
					})
				}
			} else {
				// Success of the previously-failing tool resets the counter.
				if tc.Function.Name == lastFailedTool {
					consecutiveSameToolFailures = 0
					lastFailedTool = ""
					sameToolFailureGuidanceInjected = false
				}
			}

			// Drift detection deliberately tracks the raw result. Projection is a
			// model-context concern and must not make distinct runtime outputs look
			// identical to the loop detector.
			record := toolCallRecord{name: tc.Function.Name, args: argsJSON, result: result}
			recentCalls = append(recentCalls, record)
			if len(recentCalls) > driftWindow*2 {
				recentCalls = recentCalls[len(recentCalls)-driftWindow*2:]
			}

			// Project exactly once, after every runtime consumer has observed the
			// complete result and immediately before model/history commit. The
			// projection carries a lossless handle to the persisted raw payload.
			result = projectLoopToolResult(cb, tc.Function.Name, execResult)

			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      result,
			})
			conversation = appendComputerUseVisionImages(conversation, cfg, execResult.ModelImages)
			historyDelta = append(historyDelta, ConversationEntry{
				Role:        "tool",
				Content:     result,
				ToolCallID:  tc.ID,
				ToolName:    tc.Function.Name,
				ToolOutcome: string(execResult.Outcome),
			})
		}

		// Record this batch before commit/steer can skip it. File successes
		// already settled in note(); apply() still records the last failure.
		inject := wsBatch.apply(workingState)

		// No-forward-progress circuit breaker. A batch with zero successful
		// calls (all failed, denied, or fence-rejected) moves the turn no
		// closer to its goal; enough of those in a row means the model is
		// dithering, not recovering.
		if batchHadSuccess {
			consecutiveNoProgressIterations = 0
		} else {
			consecutiveNoProgressIterations++
			if consecutiveNoProgressIterations >= hardStopNoProgressIterations {
				log.Printf("[agent-loop] hard stop: no successful tool call in %d consecutive iterations, force-exiting loop", consecutiveNoProgressIterations)
				if abandoner, ok := h.(ToolBatchAbandoner); ok {
					abandoner.OnToolBatchAbandoned(batchMeta)
				}
				disposeSurface(ToolSurfaceResponseAbandoned)
				text := strings.TrimSpace(StripThinkingTags(lastNonEmptyContent))
				if text == "" {
					text = "多次尝试均未取得进展，已停止执行。请换一种方式描述任务，或稍后再试。"
				}
				return finish(LoopResult{
					Text:       text,
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
					HardExit:   true,
				})
			}
		}

		// Commit only after the entire parallel tool batch is fully paired in
		// HistoryDelta. OnToolExecuted is intentionally earlier and remains for
		// telemetry/model routing, not durable recovery.
		if committer, ok := h.(ToolBatchCommitter); ok {
			batch := append([]ConversationEntry(nil), historyDelta[batchDeltaStart:]...)
			batchMeta = ToolBatchMetadata{
				Sequence:        toolBatchSequence,
				LastToolName:    lastToolName,
				SideEffectState: sideEffectStateForToolBatch(choice.Message.ToolCalls),
			}
			if err := committer.OnToolBatchCommitted(batch, batchMeta); err != nil {
				disposeSurface(ToolSurfaceResponseAbandoned)
				return finish(LoopResult{
					Text:       "Unable to safely persist tool progress; automatic execution stopped.",
					Error:      "recovery_checkpoint_failed",
					Iterations: iteration + 1,
					ToolCalls:  totalToolCalls,
					HardExit:   true,
				})
			}
		}
		// The batch committer can publish dependant grants only after every tool
		// result is paired. Refreshing earlier snapshots the pre-commit surface
		// into this loop's private `tools` slice, so the next LLM request sees no
		// tool even though the host has correctly issued one. One refresh after a
		// settled batch gives the next request the complete committed surface.
		if len(executedToolNames) > 0 {
			beforeToolEscalation := cfg
			refreshSurface := false
			if refresher, ok := cb.(ToolExecutionSurfaceRefresher); ok {
				for _, name := range executedToolNames {
					// Preserve one callback per actual execution, but do not render an
					// intermediate surface: semantic hosts may publish dependants only
					// in the preceding batch commit.
					refreshSurface = refresher.RefreshAfterToolExecution(name) || refreshSurface
				}
			}
			if refreshSurface {
				// RefreshAfterToolExecution may update host-owned policy, grant, or
				// route state, but it is not an outbound model-request boundary.
				// Rendering definitions here would create a ghost executable surface
				// with no epoch, manifest, receipt, handoff, or terminal owner. Keep
				// the current request's frozen tools intact until it is settled; the
				// next real request will construct its complete replacement through
				// buildToolsForModelRequest after its epoch has advanced.
				//
				// The system prompt is non-executable conversation state and must be
				// refreshed now so that the successor request observes the updated
				// policy posture. It does not grant dispatch authority by itself.
				replaceConversationSystemPrompt(conversation, cb.BuildSystemPrompt(userText, isFirstTurn))
			}
			// Record a tool-driven route upgrade before the next iteration. This
			// keeps the final route/cost metadata aligned when an endpoint stays
			// the same but only ContextLength changes. A consumed semantic grant
			// can refresh tools without promoting the model.
			if next := cb.GetLLMConfig(); strings.TrimSpace(next.URL) != "" && strings.TrimSpace(next.Model) != "" {
				cfg = next
			}
			if loopLLMConfigChanged(beforeToolEscalation, cfg) {
				route.TaskType = string(llm.TaskReasoning)
				route.Model = cfg.Model
				route.Provider = cfg.ProviderName
				route.Source = "escalate"
				route.Reason = "tools requested after light turn"
				route.Applied = true
				usage.Model = cfg.Model
				usage.Provider = cfg.ProviderName
			}
		}
		if replanner, ok := cb.(LLMReplanAware); ok && replanner.LLMReplanRequested() {
			disposeSurface(ToolSurfaceSteered)
			freeReplans++
			if freeReplans <= maxFreeReplansPerLoop {
				iteration--
			}
			continue
		}
		disposeSurface(ToolSurfaceToolBatchSettled)

		// Same as done-check: do not spend the last iteration on an inject
		// the model will never see (it becomes "max iterations reached").
		if inject != "" && iteration+1 < maxIter {
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": inject,
			})
			historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: inject})
		}

		// Consecutive same-tool failure guidance: inject AFTER all tool results
		// are appended (injecting between tool results creates invalid message
		// ordering that some LLM APIs reject).
		// Use >= threshold with a one-shot flag to handle cases where the
		// counter jumps past the exact threshold within a single multi-tool-call
		// iteration.
		if consecutiveSameToolFailures >= maxConsecutiveSameToolFailures && !sameToolFailureGuidanceInjected {
			sameToolFailureGuidanceInjected = true
			log.Printf("[agent-loop] consecutive same-tool failures: tool=%q count=%d, injecting stop guidance", lastFailedTool, consecutiveSameToolFailures)
			guidance := fmt.Sprintf("[系统] 工具 %s 已连续失败 %d 次（每次方法不同但均未成功）。请停止继续尝试该工具，改用其他方式完成任务，或向用户说明当前遇到的具体问题和限制。", lastFailedTool, consecutiveSameToolFailures)
			conversation = append(conversation, map[string]interface{}{
				"role":    "user",
				"content": guidance,
			})
			historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: guidance})
		}

		// Drift detection: check if the last N calls are the same tool+args+result.
		// Same input + same output = dead loop. Same input + different output = polling (OK).
		if len(recentCalls) >= driftWindow {
			tail := recentCalls[len(recentCalls)-driftWindow:]
			allSame := true
			allSameResult := true
			for i := 1; i < len(tail); i++ {
				if tail[i].name != tail[0].name || tail[i].args != tail[0].args {
					allSame = false
					break
				}
				if tail[i].result != tail[0].result {
					allSameResult = false
				}
			}
			if allSame && allSameResult {
				consecutiveSame++
				if consecutiveSame >= 2 {
					driftTool := tail[0].name
					log.Printf("[agent-loop] drift detected: tool %q called %d times with same args+result, stopping", driftTool, driftWindow*consecutiveSame)
					// Inject a message telling the LLM to stop and explain.
					driftMsg := fmt.Sprintf("[系统] 检测到重复调用 %s 且结果相同，请停止重试并直接告诉用户当前的限制或问题。", driftTool)
					conversation = append(conversation, map[string]interface{}{
						"role":    "user",
						"content": driftMsg,
					})
					historyDelta = append(historyDelta, ConversationEntry{Role: "user", Content: driftMsg})
				}
			} else {
				consecutiveSame = 0
			}
		}
	}

	log.Printf("[agent-loop] max iterations (%d) reached", maxIter)
	return finish(LoopResult{Error: "max iterations reached", Iterations: maxIter, ToolCalls: totalToolCalls})
}

// toolCallNameWasRendered is the final generic request-surface admission check.
// The renderer's maps are trusted host-owned definitions; the requested name is
// not normalized because accepting aliases/case variants would let a response
// escape the exact model-visible surface that the wire receipt verified.
func toolCallNameWasRendered(tools []map[string]interface{}, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, definition := range tools {
		if tooldef.Name(definition) == name {
			return true
		}
	}
	return false
}

func unrenderedToolCallDeniedMessage(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown)"
	}
	return fmt.Sprintf("Error: tool %q was not available in this request's rendered tool surface. Do not retry %q and do not ask the user to re-authorize tools; continue with the tools rendered in this request or answer from what you already have.", name, name)
}

// consumedGrantToolCallDeniedMessage is the denial for a name that already
// completed successfully earlier in this loop but is absent from the current
// request surface (for example a one-shot grant whose sibling budget was
// consumed by that success). The model must not reinterpret the missing grant
// as the action having failed.
func consumedGrantToolCallDeniedMessage(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown)"
	}
	return fmt.Sprintf("Error: tool %q is not in this request's rendered tool surface because it already ran successfully earlier in this turn and has reached this turn's usage limit. That earlier result still stands (e.g. an artifact was already generated or delivered); do not retry %q, do not ask the user to re-authorize tools, and do not report that action as failed, consumed, or undelivered.", name, name)
}

// moaReferenceToolSurfaceDisposition converts only the locally observable
// outcome of one static advisor request into a diagnostic terminal metric. It
// deliberately does not assert provider acknowledgement, transport identity,
// or execution authority. RunReferences applies the same content acceptance
// rule before it includes advice in the aggregator prompt.
func moaReferenceToolSurfaceDisposition(resp *llm.Response, err error) ToolSurfaceDisposition {
	if err != nil {
		if errors.Is(err, errLLMDispatchMissingResponse) {
			return ToolSurfaceIntegrityFailure
		}
		return ToolSurfaceTransportFailure
	}
	if resp == nil || len(resp.Choices) == 0 {
		return ToolSurfaceResponseAbandoned
	}
	message := resp.Choices[0].Message
	if strings.TrimSpace(message.Content) == "" && strings.TrimSpace(message.ReasoningContent) == "" {
		return ToolSurfaceResponseAbandoned
	}
	return ToolSurfaceResponseSettled
}

// loopLLMConfigChanged compares the request-shaping fields that can change
// when a host promotes a light turn after a tool call. In particular, model
// and URL equality must not hide a ContextLength-only promotion.
func loopLLMConfigChanged(a, b corelib.MaclawLLMConfig) bool {
	return a.URL != b.URL || a.Key != b.Key || a.Model != b.Model ||
		a.Protocol != b.Protocol || a.ProviderName != b.ProviderName ||
		a.ContextLength != b.ContextLength || a.TimeoutSec != b.TimeoutSec ||
		a.MaxOutputTokens != b.MaxOutputTokens || a.ThinkingMode != b.ThinkingMode ||
		a.ReasoningEffort != b.ReasoningEffort
}

func sideEffectStateForToolBatch(calls []llm.ToolCall) string {
	state := "none"
	for _, call := range calls {
		name := strings.ToLower(strings.TrimSpace(call.Function.Name))
		switch name {
		// These built-in tools only inspect local, already-available state.
		// Keep this allow-list explicit: tool names are not a reliable security
		// boundary, especially for per-client and MCP-provided tools.
		case "read_file", "read_files", "list_directory", "list_dir",
			"ripgrep", "grep", "glob", "search_files", "session_search":
			continue
		// These built-in tools change local state. A recovery may resume the
		// conversation, but must first tell the user to inspect the workspace.
		case "write_file", "edit_file", "apply_patch", "str_replace",
			"create_file", "delete_file", "remove_file", "bash",
			"run_terminal", "shell", "powershell":
			state = "local_committed"
		default:
			// Unknown names include dynamically declared client and MCP tools.
			// We cannot infer whether they made an external change from a string
			// name, so recovery must require explicit review rather than replay.
			return "external_uncertain"
		}
	}
	return state
}

func projectLoopToolResult(cb LoopCallbacks, name string, result ToolExecutionResult) string {
	if projector, ok := cb.(ToolResultProjector); ok {
		return projector.ProjectToolResult(name, result)
	}
	return TruncateToolResultForTool(name, result.Result)
}

func executeLoopTool(cb LoopCallbacks, name, argsJSON string) ToolExecutionResult {
	argsJSON = normalizeLoopToolArguments(argsJSON)
	if result, rejected := authorizeLoopTool(cb, name, argsJSON); rejected {
		return result
	}
	return executeAuthorizedLoopTool(cb, name, argsJSON)
}

func normalizeLoopToolArguments(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return "{}"
	}
	return argsJSON
}

func validateLoopToolArguments(name, argsJSON string) (ToolExecutionResult, bool) {
	args := strings.TrimSpace(argsJSON)
	if args == "" {
		return ToolExecutionResult{}, false
	}
	if loopToolArgumentsAreObject(args) {
		return ToolExecutionResult{}, false
	}
	msg := fmt.Sprintf("Error: tool call %q has invalid JSON object arguments. The tool was not executed. Regenerate the same tool call with complete valid JSON object arguments, including all required fields, and do not summarize or truncate file content.", name)
	log.Printf("[agent-loop] rejected invalid tool arguments tool=%q args_len=%d", strings.TrimSpace(name), len(args))
	return ToolExecutionResult{Result: msg, Outcome: ToolExecutionOutcomeError}, true
}

func loopToolArgumentsAreObject(args string) bool {
	var parsed map[string]interface{}
	return json.Unmarshal([]byte(args), &parsed) == nil && parsed != nil
}

func invalidLoopToolArgumentNames(calls []llm.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	var invalid []string
	for _, tc := range calls {
		args := normalizeLoopToolArguments(tc.Function.Arguments)
		if !loopToolArgumentsAreObject(args) {
			invalid = append(invalid, tc.Function.Name)
		}
	}
	return invalid
}

func authorizeLoopTool(cb LoopCallbacks, name, argsJSON string) (ToolExecutionResult, bool) {
	// Host grant/policy boundaries win over the adaptive light allowlist.
	// A governed lookup that denies write_file/web_fetch must not emit the
	// light-upgrade hint ("set PROFILE=full"); that text made the model ask
	// the user to re-authorize tools that this turn cannot run.
	if authorizer, ok := cb.(ToolAuthorizer); ok && !authorizer.IsToolAllowed(name) {
		return ToolExecutionResult{
			Result:  resolveToolAuthorizerDenyMessage(cb, name),
			Outcome: ToolExecutionOutcomeError,
		}, true
	}
	if pp, ok := cb.(PromptProfileProvider); ok && pp.CurrentPromptProfile().IsLight() {
		if !isToolAllowedForPromptProfile(cb, name, pp.CurrentPromptProfile()) {
			RecordLightToolDeny(name)
			return ToolExecutionResult{
				Result:  LightToolDenyMessage(name),
				Outcome: ToolExecutionOutcomeError,
			}, true
		}
	}
	if authorizer, ok := cb.(ToolCallAuthorizer); ok {
		if allowed, reason := authorizer.IsToolCallAllowed(name, argsJSON); !allowed {
			if strings.TrimSpace(reason) == "" {
				reason = fmt.Sprintf("tool call %q is not allowed by the current execution policy", name)
			}
			return ToolExecutionResult{
				Result:  "Error: " + reason,
				Outcome: ToolExecutionOutcomeError,
			}, true
		}
	}
	return ToolExecutionResult{}, false
}

func resolveToolAuthorizerDenyMessage(cb LoopCallbacks, name string) string {
	if presenter, ok := cb.(ToolDenialPresenter); ok {
		if custom := strings.TrimSpace(presenter.ToolDenialMessage(name)); custom != "" {
			return custom
		}
	}
	return toolAuthorizerDenyMessage(name)
}

func toolAuthorizerDenyMessage(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		n = "(unknown)"
	}
	return fmt.Sprintf(
		"Error: tool %q is not allowed by the current execution policy. Use a tool available in this turn; do not ask the user to re-authorize tools.",
		n,
	)
}

// IsToolAllowedForPromptProfile applies a host's immutable invocation policy
// when available, otherwise the legacy static allowlist. It is exported so a
// host can keep BuildTools exposure aligned with this execution boundary.
func IsToolAllowedForPromptProfile(cb LoopCallbacks, name string, profile PromptProfile) bool {
	return isToolAllowedForPromptProfile(cb, name, profile)
}

func isToolAllowedForPromptProfile(cb LoopCallbacks, name string, profile PromptProfile) bool {
	if authorizer, ok := cb.(PromptProfileToolAuthorizer); ok {
		return authorizer.IsToolAllowedForPromptProfile(name, profile)
	}
	if profile.IsLight() {
		return IsLightTurnToolAllowed(name)
	}
	return true
}

// isLightToolDenyResult detects authorizeLoopTool rejections from the light allowlist.
func isLightToolDenyResult(res ToolExecutionResult) bool {
	return res.Outcome == ToolExecutionOutcomeError &&
		strings.Contains(res.Result, "light prompt profile")
}

// tryLightProfileToolRetry upgrades light→full once and refreshes the system
// message for subsequent LLM rounds. It intentionally does not rebuild the
// current request's tool slice: a provider response has authority only over
// the exact definitions rendered and sent for that request. The next real
// outbound request performs the normal epoch → complete replacement render.
func tryLightProfileToolRetry(
	cb LoopCallbacks,
	userText string,
	isFirstTurn bool,
	deniedTool string,
	tools *[]map[string]interface{},
	conversation []interface{},
) bool {
	if !LightToolRetryEnabled() {
		return false
	}
	if managed, ok := cb.(ManagedSemanticTurn); ok && managed.ManagedSemanticTurn() {
		return false
	}
	upgrader, ok := cb.(LightProfileUpgrader)
	if !ok {
		return false
	}
	reason := "tool_deny_retry:" + strings.TrimSpace(deniedTool)
	if !upgrader.UpgradeLightPromptToFull(reason) {
		return false
	}
	// Host may still report light if upgrade is a no-op.
	if pp, ok := cb.(PromptProfileProvider); ok && pp.CurrentPromptProfile().IsLight() {
		return false
	}
	RecordLightUpgrade(reason)
	_ = tools
	// Replace system message so full policy is visible to the model.
	newPrompt := cb.BuildSystemPrompt(userText, isFirstTurn)
	replaceConversationSystemPrompt(conversation, newPrompt)
	log.Printf("[agent-loop] light→full upgrade after tool deny tool=%q tools=%d", deniedTool, len(*tools))
	return true
}

// replaceConversationSystemPrompt updates conversation[0] when it is the system turn.
func replaceConversationSystemPrompt(conversation []interface{}, newPrompt string) {
	if len(conversation) == 0 || strings.TrimSpace(newPrompt) == "" {
		return
	}
	switch msg := conversation[0].(type) {
	case map[string]string:
		if msg["role"] == "system" {
			msg["content"] = newPrompt
			conversation[0] = msg
		}
	case map[string]interface{}:
		if role, _ := msg["role"].(string); role == "system" {
			msg["content"] = newPrompt
			conversation[0] = msg
		}
	}
}

func executeAuthorizedLoopTool(cb LoopCallbacks, name, argsJSON string) ToolExecutionResult {
	return executeAuthorizedLoopToolCall(cb, name, argsJSON, "")
}

func executeAuthorizedLoopToolCall(cb LoopCallbacks, name, argsJSON, callID string) ToolExecutionResult {
	return executeAuthorizedLoopToolCallWithContext(cb, name, argsJSON, callID, ToolCallExecutionContext{})
}

func beginToolCallExecutionContext(cb LoopCallbacks, iteration int) ToolCallExecutionContext {
	var execution ToolCallExecutionContext
	if epochProvider, ok := cb.(ToolSurfaceEpochProvider); ok {
		execution.SurfaceEpoch = epochProvider.BeginToolSurfaceEpoch(iteration)
	}
	if provider, ok := cb.(ToolSurfaceExecutionContextProvider); ok {
		provided := provider.ToolSurfaceExecutionContext(iteration, execution.SurfaceEpoch)
		// The epoch is created by the existing request-boundary provider and is
		// not replaceable by a second callback. This avoids an accidental route
		// from host configuration into a model-controlled surface identity.
		provided.SurfaceEpoch = execution.SurfaceEpoch
		return provided
	}
	return execution
}

func buildToolsForModelRequest(cb LoopCallbacks, userText string, iteration int) []map[string]interface{} {
	if renderer, ok := cb.(ModelRequestToolSurfaceRenderer); ok {
		return FilterToolDefinitionsByAuthorizer(cb, renderer.BuildToolsForModelRequest(userText, iteration))
	}
	// Static compatibility callbacks do not have a reservation-bound renderer,
	// but they still own a distinct real outbound request on every iteration,
	// stream fallback and outer retry. Reusing the prior in-memory slice would
	// let an old static inventory survive a host policy/configuration change and
	// makes the next receipt prove only a stale presentation. Rebuild from the
	// callback at each request boundary. This helper deliberately accepts no
	// predecessor slice, so callers cannot accidentally restore cross-request
	// surface reuse by treating cached definitions as fallback authority.
	return FilterToolDefinitionsByAuthorizer(cb, cb.BuildTools(userText))
}

// prepareToolsForToolSurfaceReceipt applies the same provider-specific OpenAI
// projection used by Chat and Responses request serialization before the
// receipt manifest is frozen. Anthropic owns a distinct wire projection and
// retains the definitions supplied by its request builder.
func prepareToolsForToolSurfaceReceipt(cfg corelib.MaclawLLMConfig, tools []map[string]interface{}) []map[string]interface{} {
	// WireAPI selects the actual builder before Protocol in every RunLoop
	// dispatch path. Keep receipt projection ordered the same way: a legacy or
	// misconfigured anthropic Protocol label must not make a Responses payload
	// retain schema keys that its OpenAI-compatible builder removes.
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return llm.PrepareOpenAIChatToolsForWire(cfg, tools)
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return tools
	}
	return llm.PrepareOpenAIChatToolsForWire(cfg, tools)
}

func executeAuthorizedLoopToolCallWithContext(cb LoopCallbacks, name, argsJSON, callID string, execution ToolCallExecutionContext) ToolExecutionResult {
	if contextual, ok := cb.(ToolCallContextExecutor); ok {
		result := contextual.ExecuteToolCallWithContext(name, argsJSON, callID, execution)
		if result.Outcome == "" {
			outcome := classifyToolResult(result.Result)
			result.Outcome = executionOutcomeFromToolOutcome(outcome.kind)
		}
		return result
	}
	if contextual, ok := cb.(ToolCallExecutor); ok {
		result := contextual.ExecuteToolCall(name, argsJSON, callID)
		if result.Outcome == "" {
			outcome := classifyToolResult(result.Result)
			result.Outcome = executionOutcomeFromToolOutcome(outcome.kind)
		}
		return result
	}
	if structured, ok := cb.(StructuredToolExecutor); ok {
		result := structured.ExecuteToolStructured(name, argsJSON)
		if result.Outcome == "" {
			outcome := classifyToolResult(result.Result)
			result.Outcome = executionOutcomeFromToolOutcome(outcome.kind)
		}
		return result
	}
	result := cb.ExecuteTool(name, argsJSON)
	outcome := classifyToolResult(result)
	return ToolExecutionResult{Result: result, Outcome: executionOutcomeFromToolOutcome(outcome.kind)}
}

// FilterToolDefinitionsByAuthorizer removes LLM-facing tool definitions that
// the callback's ToolAuthorizer would reject at execution time. This keeps
// exposure and execution governed by the same mechanism.
func FilterToolDefinitionsByAuthorizer(cb LoopCallbacks, tools []map[string]interface{}) []map[string]interface{} {
	authorizer, ok := cb.(ToolAuthorizer)
	if !ok || len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if authorizer.IsToolAllowed(tooldef.Name(def)) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// validateBoundToolSurfaceAuthorization preserves a correlation-bound
// renderer's all-or-nothing ownership of its already-published definitions.
// Unlike the S0.5 compatibility filter above, it never returns a subset: a
// policy change or an inconsistent renderer/authorizer pair must retire the
// reservation before wire handoff, rather than making the provider observe a
// smaller surface than the durable holder published.
func validateBoundToolSurfaceAuthorization(cb LoopCallbacks, tools []map[string]interface{}) error {
	authorizer, ok := cb.(ToolAuthorizer)
	if !ok {
		return nil
	}
	for _, def := range tools {
		if !authorizer.IsToolAllowed(tooldef.Name(def)) {
			return fmt.Errorf("surface_integrity_failure: bound tool surface definition is rejected by current authorizer")
		}
	}
	return nil
}

func toolOutcomeFromExecutionResult(result ToolExecutionResult) toolOutcome {
	outcome := toolOutcome{kind: toolOutcomeOK, snippet: truncateRunesSuffix(result.Result, 300)}
	switch result.Outcome {
	case ToolExecutionOutcomeTimeout:
		outcome.kind = toolOutcomeTimeout
	case ToolExecutionOutcomeError:
		outcome.kind = toolOutcomeError
	default:
		outcome.kind = toolOutcomeOK
	}
	return outcome
}

func executionOutcomeFromToolOutcome(kind toolOutcomeKind) ToolExecutionOutcome {
	switch kind {
	case toolOutcomeTimeout:
		return ToolExecutionOutcomeTimeout
	case toolOutcomeError:
		return ToolExecutionOutcomeError
	default:
		return ToolExecutionOutcomeOK
	}
}

// doLLMRequestWithTools sends a chat completion request with tool definitions.
// Dispatches to the correct protocol based on cfg.Protocol:
//   - "anthropic" → Anthropic Messages API
//   - everything else → OpenAI-compatible chat completions
func doLLMRequestWithTools(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, httpClient *http.Client) (*llm.Response, error) {
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return doResponsesRequestWithTools(ctx, cfg, conversation, tools, policy, httpClient)
	}
	if cfg.Protocol == "anthropic" {
		return llm.DoAnthropicRequestWithOptions(ctx, cfg, conversation, tools, httpClient, llm.AnthropicMessagesRequestOptions{
			Tools: tools, ExplicitToolReplacement: true,
		})
	}
	choice, parallel := toolSurfacePolicyRequestOptions(policy)
	return llm.DoOpenAIRequestWithOptions(ctx, cfg, conversation, tools, httpClient, llm.OpenAIChatRequestOptions{
		Stream: false, Tools: tools, ExplicitToolReplacement: true, ToolChoice: choice, ParallelToolCalls: parallel,
	})
}

func doResponsesRequestWithTools(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, httpClient *http.Client) (*llm.Response, error) {
	httpClient = llm.HTTPClientForRequestContext(ctx, httpClient)
	choice, parallel := toolSurfacePolicyRequestOptions(policy)
	req, _, endpoint, err := llm.NewResponsesAPIRequest(ctx, cfg, conversation, llm.ResponsesAPIRequestOptions{
		Stream:                  false,
		Tools:                   tools,
		ExplicitToolReplacement: true,
		ToolChoice:              choice,
		ParallelToolCalls:       parallel,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[agent-loop] POST %s model=%s configured_model=%s protocol=%s wire_api=%s (stream=false)", endpoint, cfg.UpstreamModel(), cfg.Model, cfg.Protocol, cfg.WireAPI)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil && len(body) == 0 {
		return nil, readErr
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &llm.HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}
	parsed, err := llm.ParseNonStreamResponsesAPIBody(body)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

// buildEmptyResponseRecovery constructs a context-aware recovery prompt when
// the LLM returns an empty response (no content, no tool calls). The prompt
// includes information about the last tool execution to help the LLM resume
// its task, especially after tool timeouts or errors.
func buildEmptyResponseRecovery(emptyCount int, lastToolName string, outcome toolOutcome, userGoal string) string {
	var sb strings.Builder

	// Escalating urgency based on consecutive empty count.
	if emptyCount <= 2 {
		sb.WriteString("[系统] 你的上一条回复为空。")
	} else {
		sb.WriteString(fmt.Sprintf("[系统] 警告：你已经连续 %d 次返回空回复。你必须立即回复内容或调用工具，否则任务将被终止。", emptyCount))
	}

	// Include last tool context if available.
	// The outcome kind is determined structurally by classifyToolResult,
	// not by keyword matching on arbitrary output.
	if lastToolName != "" {
		switch outcome.kind {
		case toolOutcomeTimeout:
			sb.WriteString(fmt.Sprintf("\n上一个工具 %s 执行超时。请不要放弃——你应该：", lastToolName))
			sb.WriteString("\n1. 检查操作是否仍在后台运行（如适用）")
			sb.WriteString("\n2. 尝试用更短的超时或不同的方式重试")
			sb.WriteString("\n3. 如果无法继续，向用户说明当前进度和遇到的问题")
		case toolOutcomeError:
			sb.WriteString(fmt.Sprintf("\n上一个工具 %s 返回了错误。请分析错误原因并尝试其他方法继续完成任务。", lastToolName))
		default:
			sb.WriteString(fmt.Sprintf("\n上一个工具调用是 %s。请根据其结果继续执行任务。", lastToolName))
		}
	}

	// Remind the LLM of the original goal on later retries.
	if emptyCount >= 2 && userGoal != "" {
		goalSnippet := truncateRunesPrefix(userGoal, 200)
		sb.WriteString(fmt.Sprintf("\n\n用户的原始目标：%s", goalSnippet))
		sb.WriteString("\n请继续完成这个任务，或者告诉用户当前的进展和遇到的问题。")
	}

	return sb.String()
}

func buildToolCallTruncationRecovery(names []string, tools []map[string]interface{}, userGoal string) string {
	available := map[string]bool{}
	for _, item := range tools {
		if name := tooldef.Name(item); name != "" {
			available[name] = true
		}
	}
	var sb strings.Builder
	sb.WriteString("[system] Previous tool call arguments were incomplete or invalid JSON, likely because output was truncated. Do not repeat the same oversized call. Regenerate complete valid JSON.")
	if len(names) > 0 {
		sb.WriteString("\nTruncated tools: ")
		sb.WriteString(strings.Join(names, ", "))
	}
	if containsToolName(names, "write_file") && available["write_file"] {
		sb.WriteString("\nFor write_file: no per-call content length limit. For very large content (>6000 chars), split into overwrite + append chunks to avoid model output truncation. Prefer edit_file or edit_lines for existing files.")
	}
	if containsToolName(names, "bash") && available["bash"] {
		sb.WriteString("\nFor bash: keep command <= 4000 characters. Do not embed generated file bodies in shell heredocs; use write_file chunks or targeted edits.")
	}
	if (available["edit_file"] || available["edit_lines"]) && containsToolName(names, "write_file") {
		sb.WriteString("\nFor existing files, use targeted edits instead of rewriting whole files.")
	}
	if strings.TrimSpace(userGoal) != "" {
		sb.WriteString("\nOriginal user goal: ")
		sb.WriteString(truncateRunesPrefix(userGoal, 240))
	}
	return sb.String()
}

func containsToolName(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func llmRequestContextForLoop(cb LoopCallbacks, iteration int) (context.Context, func(error), error) {
	if provider, ok := cb.(LLMRequestContextProvider); ok {
		ctx, finish, err := provider.LLMRequestContext(iteration)
		if err != nil {
			return nil, nil, err
		}
		if ctx == nil {
			ctx = context.Background()
		}
		if finish == nil {
			finish = func(error) {}
		}
		// RunLoop owns the selection → render → transport lifecycle for every
		// request it starts. SDK compatibility retries would otherwise create a
		// successor body after this owner has already frozen the tool surface and
		// receipt. Preserve the host's context values, cancellation and deadline;
		// this only disables those hidden successor requests.
		return llm.WithTransparentRequestRetriesDisabled(ctx), finish, nil
	}
	return llm.WithTransparentRequestRetriesDisabled(context.Background()), func(error) {}, nil
}

var errLLMDispatchMissingResponse = errors.New("LLM dispatch completed without response or error")

// requireLLMDispatchResponse keeps a completed request distinct from a valid
// provider response with zero choices. A nil response has no response identity
// to bind and is therefore an adapter contract failure, not an empty answer.
func requireLLMDispatchResponse(resp *llm.Response) error {
	if resp != nil {
		return nil
	}
	return fmt.Errorf("surface_integrity_failure: %w", errLLMDispatchMissingResponse)
}

// llmRequestContextWithTimeout applies the current request route's deadline
// without replacing the host context. context.WithTimeout retains any earlier
// host deadline, cancellation signal, trace values, and scheduler ownership.
func llmRequestContextWithTimeout(ctx context.Context, cfg corelib.MaclawLLMConfig) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, time.Duration(cfg.EffectiveTimeoutSec())*time.Second)
}

// ---------------------------------------------------------------------------
// toolOutcome — structured classification of tool execution results.
//
// All classification logic lives in classifyToolResult, which inspects the
// well-known prefixes produced by our own tool implementations (e.g.
// "[错误] 命令超时", "工具执行异常"). This is NOT keyword matching on
// arbitrary LLM output — these are structured markers we control.
// ---------------------------------------------------------------------------

type toolOutcomeKind int

const (
	toolOutcomeOK      toolOutcomeKind = iota // tool executed successfully
	toolOutcomeTimeout                        // tool hit a deadline / timeout
	toolOutcomeError                          // tool returned a known error
)

type toolOutcome struct {
	kind    toolOutcomeKind
	snippet string // last ~300 runes of the result for logging
}

// classifyToolResult inspects the tool result string and returns a structured
// outcome. The markers checked here are produced by our own tool code:
//
//   - "[错误] 命令超时"  → tools_local.go, im_tools_local.go
//   - "[错误] 退出码"    → tools_local.go, im_tools_local.go
//   - "[错误] 命令启动失败" → tools_local.go, im_tools_local.go
//   - "[错误] ..."       → im_tool_async_wait.go
//   - "工具执行异常"     → im_tool_execution.go (panic recovery)
//   - "未知工具"         → im_tool_execution.go, subagent callbacks
//   - "参数解析失败"     → im_tool_execution.go, tui callbacks
//   - "错误:"           → various tool handlers
//   - "Error:"          → various tool handlers
func classifyToolResult(result string) toolOutcome {
	snippet := truncateRunesSuffix(result, 300)

	// Timeout: our bash/ssh tools append "[错误] 命令超时（N 秒）".
	if strings.Contains(result, "[错误] 命令超时") {
		return toolOutcome{kind: toolOutcomeTimeout, snippet: snippet}
	}

	// Structured error markers from our tool implementations.
	errorPrefixes := []string{
		"[错误]",   // tools_local, im_tools_local, im_tool_async_wait
		"工具执行异常", // im_tool_execution.go panic recovery
		"未知工具",   // im_tool_execution.go, subagent callbacks
		"参数解析失败", // im_tool_execution.go, tui callbacks
		"错误:",    // various tool handlers
		"Error:", // various tool handlers
	}
	trimmed := strings.TrimSpace(result)
	for _, prefix := range errorPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return toolOutcome{kind: toolOutcomeError, snippet: snippet}
		}
	}
	// Also check for [错误] appearing mid-result (e.g. bash output + error).
	if strings.Contains(result, "\n[错误]") {
		return toolOutcome{kind: toolOutcomeError, snippet: snippet}
	}

	return toolOutcome{kind: toolOutcomeOK, snippet: snippet}
}

// truncateRunesSuffix returns the last n runes of s (UTF-8 safe).
// If s has fewer than n runes, returns s unchanged.
func truncateRunesSuffix(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// truncateRunesPrefix returns the first n runes of s with "..." appended (UTF-8 safe).
// If s has fewer than n runes, returns s unchanged.
func truncateRunesPrefix(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// appendLoopDisplayReasoning preserves every provider-supplied, display-safe
// reasoning summary from a multi-round tool loop while suppressing the common
// final-summary repetition. It intentionally does not infer or manufacture
// reasoning content.
func appendLoopDisplayReasoning(buf *strings.Builder, reasoning string) {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return
	}
	existing := buf.String()
	if existing == "" {
		buf.WriteString(reasoning)
		return
	}
	if strings.Contains(existing, reasoning) {
		return
	}
	if strings.HasPrefix(reasoning, existing) {
		buf.Reset()
		buf.WriteString(reasoning)
		return
	}
	buf.WriteString("\n\n")
	buf.WriteString(reasoning)
}

func reasoningSentinelCallback(rawOnToken llm.TokenCallback) llm.TokenCallback {
	return func(delta string) {
		if rawOnToken != nil && delta != "" {
			rawOnToken("\x01" + delta)
		}
	}
}

// doLLMRequestWithToolsStream sends a streaming LLM request, calling onToken
// for each text delta. Falls back to non-streaming if the streaming request fails.
func doLLMRequestWithToolsStream(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, httpClient *http.Client, onToken llm.TokenCallback) (*llm.Response, error) {
	return doLLMRequestWithToolsStreamWithBeforeFallback(ctx, cfg, conversation, tools, toolSurfaceInvocationPolicyForConfig(cfg), httpClient, onToken, nil)
}

// toolSurfaceFallbackPreparation returns a complete successor request surface.
// A fallback is an independent outbound request, so its definitions, client
// and invocation policy must originate together from the successor owner.
// The callback is deliberately unable to return a partial update such as only
// a new definitions slice: that shape invites a predecessor policy/client to
// leak into the successor wire request.
type toolSurfaceFallbackPreparation struct {
	Definitions      []map[string]interface{}
	HTTPClient       *http.Client
	InvocationPolicy ToolSurfaceInvocationPolicy
}

// errToolSurfaceFallbackSuppressed says the owner intentionally declined to
// create a successor (for example because the predecessor delivery is
// ambiguous). It is distinct from preparation failure so the caller can retain
// the original transport error instead of mislabeling a non-existent successor
// as an integrity failure.
var errToolSurfaceFallbackSuppressed = errors.New("tool surface fallback suppressed")

// doLLMRequestWithToolsStreamWithBeforeFallback invokes beforeFallback at the
// boundary where a failed streaming attempt is replaced by a non-streaming
// HTTP request. The callback returns a complete successor preparation: a fresh
// receipt must verify the fallback bytes, never the predecessor stream's
// manifest or policy projection. A non-nil callback error is a failed successor
// preparation; the original streaming error is not safe to retain because it
// would hide a distinct surface-integrity failure. Callers that correlate tool
// calls to requests use it to mint a fresh server-owned surface epoch too.
func doLLMRequestWithToolsStreamWithBeforeFallback(ctx context.Context, cfg corelib.MaclawLLMConfig, conversation []interface{}, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, httpClient *http.Client, onToken llm.TokenCallback, beforeFallback func() (toolSurfaceFallbackPreparation, error)) (*llm.Response, error) {
	rawOnToken := onToken
	var rolePrefixFilter *rolePrefixStreamFilter
	if onToken != nil {
		rolePrefixFilter = newRolePrefixStreamFilter(onToken)
		onToken = rolePrefixFilter.Write
	}
	flushRolePrefixFilter := func() {
		if rolePrefixFilter != nil {
			rolePrefixFilter.Flush()
		}
	}

	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		// Responses reasoning summaries need to arrive while the turn is busy so
		// the UI can keep its thinking panel open. The sentinel distinguishes
		// display-safe reasoning from ordinary assistant text in the shared host
		// callback protocol.
		choice, parallel := toolSurfacePolicyRequestOptions(policy)
		resp, err := llm.DoResponsesAPIRequestStreamWithOptions(ctx, cfg, conversation, tools, httpClient, onToken, reasoningSentinelCallback(rawOnToken), llm.ResponsesAPIRequestOptions{Stream: true, Tools: tools, ExplicitToolReplacement: true, ToolChoice: choice, ParallelToolCalls: parallel})
		flushRolePrefixFilter()
		if err != nil {
			if ctx.Err() != nil {
				return resp, err
			}
			// Keep transient failures in the outer retry loop. Besides applying
			// one consistent retry budget, that loop can interrupt its backoff
			// when live steering arrives. Falling back immediately to a
			// non-streaming request here would send the stale conversation before
			// the host has a chance to inject that steering.
			if shouldRetrySimpleLLMError(err) {
				return resp, err
			}
			// Once any delta reached the host, retrying the same request as a
			// non-stream response would append a second copy to the visible turn
			// and can repeat a provider-side tool decision. Return the terminal
			// error instead; the caller's normal retry policy can start a clean
			// round when appropriate.
			if resp != nil {
				// Deliver the small role-prefix buffer before returning a partial
				// stream error; otherwise a short first token would remain hidden.
				if rolePrefixFilter != nil {
					rolePrefixFilter.Flush()
				}
				return resp, err
			}
			log.Printf("[agent-loop] Responses streaming failed, falling back to non-stream: %v", err)
			if beforeFallback != nil {
				preparation, preparationErr := beforeFallback()
				if preparationErr != nil {
					if errors.Is(preparationErr, errToolSurfaceFallbackSuppressed) {
						return resp, err
					}
					return resp, preparationErr
				}
				if preparation.HTTPClient == nil {
					return resp, fmt.Errorf("surface_integrity_failure: successor fallback preparation has no receipt client")
				}
				if preparation.Definitions == nil {
					// nil definitions are a valid explicit empty replacement. The
					// receipt client will serialize tools:[] at the final boundary.
					preparation.Definitions = []map[string]interface{}{}
				}
				tools = preparation.Definitions
				httpClient = preparation.HTTPClient
				policy = preparation.InvocationPolicy
			}
			return doResponsesRequestWithTools(ctx, cfg, conversation, tools, policy, httpClient)
		}
		return resp, err
	}

	if cfg.Protocol == "anthropic" {
		// Anthropic thinking arrives as thinking_delta, not reasoning_content.
		resp, err := llm.DoAnthropicRequestStreamWithReasoning(ctx, cfg, conversation, tools, httpClient, onToken, reasoningSentinelCallback(rawOnToken))
		flushRolePrefixFilter()
		if err != nil {
			// A live-steer replan deliberately cancels this operation. Do not
			// immediately issue a fallback request with the same cancelled context;
			// the outer loop will inject the steering and start a fresh request.
			if ctx.Err() != nil {
				return resp, err
			}
			// Once thinking or answer text reached the host, a non-stream retry
			// would duplicate the visible turn. Keep the assembled prefix.
			if resp != nil {
				return resp, err
			}
			// Fallback to non-streaming only while this request remains live.
			// A route deadline or host cancellation must reach the outer loop as-is.
			log.Printf("[agent-loop] streaming failed, falling back to non-stream: %v", err)
			if beforeFallback != nil {
				preparation, preparationErr := beforeFallback()
				if preparationErr != nil {
					if errors.Is(preparationErr, errToolSurfaceFallbackSuppressed) {
						return resp, err
					}
					return resp, preparationErr
				}
				if preparation.HTTPClient == nil {
					return resp, fmt.Errorf("surface_integrity_failure: successor fallback preparation has no receipt client")
				}
				if preparation.Definitions == nil {
					preparation.Definitions = []map[string]interface{}{}
				}
				tools = preparation.Definitions
				httpClient = preparation.HTTPClient
				policy = preparation.InvocationPolicy
			}
			return llm.DoAnthropicRequestWithOptions(ctx, cfg, conversation, tools, httpClient, llm.AnthropicMessagesRequestOptions{
				Tools: tools, ExplicitToolReplacement: true,
			})
		}
		return resp, nil
	}
	// Chat Completions providers (including DeepSeek) expose reasoning via
	// delta.reasoning_content. Forward it immediately with the same sentinel
	// used by Responses so hosts can show the thinking panel while busy.
	choice, parallel := toolSurfacePolicyRequestOptions(policy)
	resp, err := llm.DoOpenAIRequestStreamWithOptions(ctx, cfg, conversation, tools, httpClient, onToken, reasoningSentinelCallback(rawOnToken), llm.OpenAIChatRequestOptions{Stream: true, Tools: tools, ExplicitToolReplacement: true, ToolChoice: choice, ParallelToolCalls: parallel})
	flushRolePrefixFilter()
	if err != nil {
		if ctx.Err() != nil {
			return resp, err
		}
		if resp != nil {
			return resp, err
		}
		log.Printf("[agent-loop] streaming failed, falling back to non-stream: %v", err)
		if beforeFallback != nil {
			preparation, preparationErr := beforeFallback()
			if preparationErr != nil {
				if errors.Is(preparationErr, errToolSurfaceFallbackSuppressed) {
					return resp, err
				}
				return resp, preparationErr
			}
			if preparation.HTTPClient == nil {
				return resp, fmt.Errorf("surface_integrity_failure: successor fallback preparation has no receipt client")
			}
			if preparation.Definitions == nil {
				preparation.Definitions = []map[string]interface{}{}
			}
			tools = preparation.Definitions
			httpClient = preparation.HTTPClient
			policy = preparation.InvocationPolicy
		}
		// Fallback is a distinct wire attempt. Its policy may differ from the
		// failed streaming predecessor, so recompute the provider-native Chat
		// projection after beforeFallback returned the successor snapshot.
		choice, parallel = toolSurfacePolicyRequestOptions(policy)
		return llm.DoOpenAIRequestWithOptions(ctx, cfg, conversation, tools, httpClient, llm.OpenAIChatRequestOptions{Stream: false, Tools: tools, ExplicitToolReplacement: true, ToolChoice: choice, ParallelToolCalls: parallel})
	}
	return resp, nil
}
