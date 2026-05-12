package main

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/nudge"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type IMMessageHandler struct {
	app        *App
	manager    *RemoteSessionManager
	memory     *agent.ConversationMemory
	client     *http.Client // chat-priority HTTP client (optimised transport)
	taskClient *http.Client // background-task HTTP client (separate pool)

	// --- Extracted App dependencies (agent-unification Phase 1) ---
	// These fields are wired from App at construction time (GUI) or from
	// standalone components (TUI). Code should use h.getWorkflowEngine()
	// and h.getUnifiedClassifier() instead of h.app.XXX. The h.app field
	// is retained for not-yet-extracted deps.
	//
	// For GUI: these may be nil at construction (late-init goroutines) and
	// the accessor methods fall through to h.app.XXX as a bridge.
	// For TUI: h.app is nil, these fields are the sole source.

	workflowEngine    *workflow.WorkflowEngine
	unifiedClassifier *intent.UnifiedIntentClassifier
	steeringStore     *steering.Store

	// standaloneConfig holds the config from NewIMMessageHandlerStandalone.
	// nil when constructed via NewIMMessageHandler (GUI mode).
	standaloneConfig *StandaloneConfig

	// Unified tool registry and dynamic builder (Phase 1 upgrade).
	registry    *ToolRegistry
	toolBuilder *DynamicToolBuilder

	// Security firewall (Phase 2 upgrade).
	firewall *SecurityFirewall

	// Dynamic tool generation and routing (lazily initialized via setters).
	toolDefGen     *ToolDefinitionGenerator
	toolRouter     *ToolRouter
	usageTracker   *tool.UsageTracker
	taskStore      *task.Store
	cachedTools    []map[string]interface{}
	toolsCacheTime time.Time
	toolsMu        sync.RWMutex

	// Capability gap detection (lazily initialized via setter).
	capabilityGapDetector *CapabilityGapDetector

	// Long-term memory store (lazily initialized via setter).
	memoryStore *memory.Store

	// Session-start memory extractor: extracts knowledge from the previous
	// session's conversation history when a new session begins. Inspired by
	// Codex CLI's memories/phase1.rs which processes old rollouts at startup.
	sessionStartExtractor *memory.SessionStartExtractor

	// Pending confirmation store for pre-execution confirmation gating.
	confirmationStore *aiConfirmationStore

	// Session template manager (lazily initialized via setter).
	templateManager *remote.SessionTemplateManager

	// Scheduled task manager (lazily initialized via setter).
	scheduledTaskManager *scheduler.Manager

	traceService *AITraceService

	// Smart session startup components (lazily initialized via setters).
	contextResolver *SessionContextResolver
	sessionPrecheck *SessionPrecheck
	startupFeedback *SessionStartupFeedback

	// Configuration manager (lazily initialized via setter).
	configManager *ConfigManager

	// Dynamic loop limit 鈥?set by the "set_max_iterations" tool during an
	// active agent loop. Reset to 0 at the start of each runAgentLoop call.
	// A positive value overrides the configured maxIter for the current loop.
	// NOTE: This field is kept as a legacy bridge alongside currentLoopCtx.
	// Both are kept in sync by toolSetMaxIterations. Will be fully replaced
	// by per-loop LoopContext.MaxIterations once Task 5 routes background
	// loops through bgManager (eliminating shared handler state).
	loopMaxOverride int

	// currentLoopCtx points to the LoopContext of the currently executing
	// runAgentLoop. Used by tools (e.g. set_max_iterations) to interact
	// with the active loop. Set at the start of runAgentLoop, cleared at end.
	currentLoopCtx *LoopContext
	chatLoopMu     sync.Mutex // serializes chat loop execution; prevents overlapping loops

	// Background loop manager and session monitor (lazily initialized via setters).
	bgManager      *BackgroundLoopManager
	sessionMonitor *SessionMonitor

	// SSH session manager (lazily initialized on first SSH tool call).
	sshMgr    *remote.SSHSessionManager
	bgTaskMgr *remote.SSHBackgroundTaskManager

	// Local background task manager for long-running local processes.
	// Mirrors the SSH BackgroundTaskManager pattern: Submit/Check/Wait/Kill.
	localBgTaskMgr *tool.LocalBackgroundTaskManager

	// lastUserText stores the most recent user message text for the current
	// agent loop. Used by toolCreateSession to detect non-coding tasks and
	// prevent unnecessary session creation.
	lastUserText string

	// lastUserID stores the user ID for the current agent loop. Used by
	// context-aware guards (e.g. conversationHasCodingContext) to load the
	// correct conversation history shard.
	lastUserID string

	// imFileSender is an optional callback that forwards a file to the user's
	// IM channels (Feishu/WeChat/etc.) via the Hub WebSocket. Set by the
	// desktop GUI after connecting to the Hub. When nil, IM forwarding is
	// silently skipped.
	imFileSender func(b64Data, fileName, mimeType, message string) error

	// agentActivity is a process-local shared store that lets the GUI AI
	// assistant and IM channels see each other's active tasks.
	agentActivity *AgentActivityStore

	// lastScreenshotAt records the time of the last successful screenshot
	// to enforce a cooldown period and prevent accidental rapid-fire captures.
	lastScreenshotAt time.Time

	// topicDetector automatically detects topic switches and clears stale
	// conversation context so users don't need to manually /new.
	topicDetector *topicSwitchDetector

	// --- First-layer Harness modules (lazily initialized via setters) ---

	// goalAnchor periodically re-injects the original user goal into the
	// LLM context to prevent drift during long-running agent loops.
	goalAnchor *GoalAnchor

	// driftDetector analyzes recent tool_call sequences to detect loop
	// patterns and trigger re-planning when the agent is stuck.
	driftDetector *DriftDetector

	// sessionDriftReplanCount tracks the cumulative drift replan count
	// across agent loops for each user. When a loop exits due to drift
	// (NeedHumanHelp), the replan count is saved here so the next loop
	// inherits it 鈥?preventing the detector from re-walking the full
	// "first drift 鈫?recover 鈫?second drift 鈫?human help" cycle.
	// Keyed by userID, value is int.
	sessionDriftReplanCount sync.Map

	// sessionDriftTool tracks the tool name that caused the last drift
	// exit for each user. Injected into the next loop's system prompt
	// so the LLM knows not to repeat the same tool.
	// Keyed by userID, value is string.
	sessionDriftTool sync.Map

	// harnessProgressTracker maintains a structured task checklist that is
	// injected into the LLM context before each iteration.
	harnessProgressTracker *HarnessProgressTracker

	// adaptiveRetry classifies tool_call failures and decides retry
	// strategy, supplementing the existing isRetryableLLMError logic.
	adaptiveRetry *AdaptiveRetry

	trajectoryRecorderFactory func() *TrajectoryRecorder

	// stashedPhasePrompt holds the custom PhasePrompt from HandleInput
	// (e.g. modify requests) so the system-prompt builder can use it
	// instead of rebuilding a generic one. Keyed by userID.
	stashedPhasePrompt sync.Map

	// workflowOriginalRequest holds the user's original task request text
	// when a workflow starts via multi-round IUM. The message that triggers
	// StartWorkflow is the IUM completion message (e.g. "娌℃湁鍏跺畠淇℃伅浜?),
	// not the original request (e.g. "鏍规嵁 readme.md 鍋?PPT"). Without
	// this stash, the agent loop's userText would be the IUM completion
	// message, which carries no task semantics and causes the LLM to drift.
	// Consumed (LoadAndDelete) by runAgentLoop to replace msg.Text.
	workflowOriginalRequest sync.Map

	// workflowAgentLoopMarker is set by handleActiveWorkflow when the
	// workflow engine returns RunAgentLoop=true. Consumed (LoadAndDelete)
	// by handleIMMessageWithLoop to enable phase prompt injection and
	// doc capture when the agent loop is running on behalf of the workflow.
	//
	// This marker is set for ALL RunAgentLoop=true responses, including
	// DefaultInput=true (first phase execution). The phase prompt guides
	// the LLM to produce the phase deliverable, and doc capture saves it
	// so the workflow can advance via NeedsConfirm.
	workflowAgentLoopMarker sync.Map

	// workflowPendingConfirmOther is set by handlePendingConfirm when the
	// LLM classifies the user's message as "other" (unrelated to the active
	// workflow's pending confirmation). Consumed (LoadAndDelete) by the
	// agent loop to skip the NeedsConfirm gate 鈥?otherwise the unrelated
	// LLM output (e.g. weather query result) would be captured as a phase
	// document and emitted to the doc preview panel.
	workflowPendingConfirmOther sync.Map

	// pendingCriticalConfirm stores response channels for critical-risk
	// skill installation confirmations. Keyed by a unique confirmation ID
	// (string), value is chan criticalRiskConfirmResponse. Cleaned up after
	// use or timeout.
	pendingCriticalConfirm sync.Map

	// pendingCriticalConfirmIM maps "platform:userID" to the active
	// critical-risk confirmation ID. When an IM user responds with
	// "纭瀹夎" or "鎷掔粷瀹夎", handleIMMessageWithLoop checks this map
	// to route the answer to ResolveCriticalConfirm.
	pendingCriticalConfirmIM sync.Map

	// pendingAskUser tracks ask_user questions that are waiting for user
	// responses. When the agent loop returns early due to ask_user, the
	// question is stored here so the next user message can be identified
	// as a response (not a new request) and the context is preserved.
	// Keyed by userID, value is *pendingAskUserState.
	pendingAskUser sync.Map

	// pendingUserReply tracks plain-text assistant questions that expect the
	// next user message to continue the same task. Keyed by userID, value is
	// *pendingUserReplyState.
	pendingUserReply sync.Map

	// suppressPendingUserReplyUpdate preserves pendingUserReply for the current
	// request when intent classification was inconclusive. Keyed by userID.
	suppressPendingUserReplyUpdate sync.Map

	// Optional test hooks for pending-reply intent classification. Production
	// uses LLMClassify; tests inject deterministic classifiers without keyword
	// matching hidden in the implementation.
	pendingReplyPromptClassifier func(assistantText string) (bool, error)
	pendingReplyAnswerClassifier func(question, answer string) (bool, error)

	// pendingCapabilityGap stores the result of an async capability gap
	// resolution (skill search + install) that completed after the response
	// was already returned to the user. The result is injected into the
	// system prompt of the next conversation turn.
	// Keyed by userID, value is *pendingCapabilityGapResult.
	pendingCapabilityGap sync.Map

	// pendingSlotUserText stores the user's original task text when it was
	// intercepted by the unfinished-slot hint. When the user clicks a slot
	// action button (dismiss / start-new), the saved text replaces the
	// synthetic placeholder so the original task is executed after the
	// state change, instead of being silently dropped.
	// Keyed by userID, value is *pendingSlotText.
	pendingSlotUserText sync.Map

	// pendingContextCompression stores a compression request from the
	// compress_context tool. Applied by the agent loop after the tool
	// result is appended to conversation.
	// Keyed by userID, value is *contextCompressionRequest.
	pendingContextCompression sync.Map

	// compactionCount tracks how many times conversation compaction has
	// occurred for each user in the current session. Used to warn users
	// when quality may degrade due to repeated compaction (Codex CLI
	// pattern: "Long threads and multiple compactions can cause the model
	// to be less accurate").
	// Keyed by userID, value is int.
	compactionCount sync.Map

	// frozenMemorySnapshots caches the memory section of the system prompt
	// per user. On the first message of a session, the memory section is
	// generated via appendMemorySection and cached. Subsequent system prompt
	// constructions reuse the cached snapshot instead of regenerating,
	// keeping the LLM's KV cache prefix stable.
	// Keyed by userID, value is string (the cached memory section text).
	frozenMemorySnapshots sync.Map

	// snapshotInitialized tracks whether a frozen memory snapshot has been
	// generated for a given user in the current session.
	// Keyed by userID, value is bool.
	snapshotInitialized sync.Map

	// taskOrchestrator manages per-task execution during the coding
	// workflow's Execution Phase. When active, it injects per-task system
	// messages and constructs focused prompts for send_and_observe instead
	// of letting the LLM dump the entire project description at once.
	// Uses a per-user registry to isolate concurrent workflows in maclawsrv.
	taskOrchestratorRegistry *TaskOrchestratorRegistry

	// nudgeTracker manages the post-use skill nudge system per session.
	// It tracks cooldown timing, deduplication, and iteration thresholds
	// to inject low-priority system messages encouraging skill creation
	// or improvement after complex tasks, skill failures, or user corrections.
	// Lazily initialized on first use via ensureNudgeTracker().
	nudgeTracker *nudge.NudgeTracker

	// taskContextManager is the unified decision point for task switching.
	// It determines whether a new message continues the current task,
	// starts a new task, or recalls a past task 鈥?replacing the scattered
	// logic across looksLikeFreshTaskRequest, TopicSwitchDetector, and
	// shouldAutoClearIncompleteTaskContext.
	taskContextManager *agent.TaskContextManager

	// taskArchive stores completed/abandoned tasks for potential recall.
	taskArchive *agent.TaskArchive

	// steeringContextFiles accumulates file paths from tool calls
	// (read_file, write_file, edit_file, etc.) during the current
	// conversation. Used by fileMatch steering resolution.
	// Keyed by "userID\x00filePath" (string), value is bool.
	steeringContextFiles sync.Map

	// pendingInjection stores supplementary messages to inject into the
	// running agent loop. Set by the interrupt handler when a Merge decision
	// is made, consumed by the agent loop at the start of each iteration.
	// Keyed by userID, value is string.
	pendingInjection sync.Map

	// interruptHandler bridges IM gateways to the running agent loop's
	// cancel/merge/status mechanisms. Set during construction.
	interruptHandler *imInterruptHandler

	// activeBtwSubAgent holds the currently running /btw SubAgent (if any).
	// Used by /cancel to cancel a running side query. Stored/cleared
	// atomically by handleBtwCommand.
	activeBtwSubAgent atomic.Pointer[BtwSubAgent]
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	// Response-header timeout: how long to wait for the FIRST byte from the
	// LLM API after sending the request. This is NOT the total streaming
	// duration 鈥?once headers arrive, SSE streaming continues without this
	// limit. 120s is sufficient for even the slowest models (deepseek-reasoner
	// thinking phase). If no byte arrives in 120s, the API is down.
	//
	// This is a fixed value rather than reading from LLM config because the
	// transport outlives any single LLM provider configuration. The user may
	// switch providers mid-session; the transport should not carry a stale
	// timeout from the previous provider.
	responseHeaderTimeout := imResponseHeaderTimeout(app)
	// Optimised transport for interactive chat 鈥?larger connection pool
	// so concurrent requests don't queue behind each other.
	chatTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true, // Disable automatic gzip for streaming responses.
	}
	// Separate transport for background tasks (scheduled tasks, auto-picked
	// AgentNet tasks) so they never starve the chat connection pool.
	taskTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       10,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
	}

	chatClient := &http.Client{Transport: chatTransport}
	taskClient := &http.Client{Transport: taskTransport}

	h := &IMMessageHandler{
		app:               app,
		manager:           manager,
		memory:            app.ensureConversationMemory(),
		confirmationStore: app.ensureAIConfirmationStore(),
		client:            chatClient,
		taskClient:        taskClient,
		agentActivity:     NewAgentActivityStore(),
		workflowEngine:    app.workflowEngine,
		unifiedClassifier: app.unifiedClassifier,
		steeringStore:     app.steeringStore,
	}
	h.interruptHandler = newIMInterruptHandler(h)
	// Initialize ToolRegistry and register builtin tools.
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	// Register non-code tools (Git, file search, health check).
	registerNonCodeTools(h.registry, app)
	// Register browser automation tools (CDP-based).
	registerBrowserTools(h.registry, app)
	// Register current-Hub MaClaw group discussion tools.
	registerGroupDiscussionTools(h.registry, app, h)
	h.toolBuilder = NewDynamicToolBuilder(h.registry)

	// Initialize automatic topic switch detector.
	h.topicDetector = newTopicSwitchDetector(func() (*http.Client, corelib.MaclawLLMConfig) {
		return h.client, h.getMaclawLLMConfig()
	})

	// Initialize task execution orchestrator for per-task coding workflow.
	h.taskOrchestratorRegistry = NewTaskOrchestratorRegistry()
	if h.sessionPrecheck != nil {
		h.taskOrchestratorRegistry.SetExternalChecker(&sessionPrecheckAdapter{precheck: h.sessionPrecheck})
	}

	// Initialize the nudge tracker for post-use skill nudge system.
	h.nudgeTracker = nudge.NewNudgeTracker()

	return h
}

func imResponseHeaderTimeout(app *App) time.Duration {
	if app == nil {
		return time.Duration(corelib.DefaultLLMTimeoutSec) * time.Second
	}
	return time.Duration(app.GetMaclawLLMConfig().EffectiveTimeoutSec()) * time.Second
}

// SetToolRegistry replaces the tool registry (for testing or late reconfiguration).
func (h *IMMessageHandler) SetToolRegistry(r *ToolRegistry) {
	h.registry = r
	h.toolBuilder = NewDynamicToolBuilder(r)
}

// SetSecurityFirewall configures the security firewall for tool execution checks.
func (h *IMMessageHandler) SetSecurityFirewall(fw *SecurityFirewall) {
	h.firewall = fw
}

// SetToolDefGenerator configures the dynamic tool definition generator.
// When set, it replaces the hardcoded buildToolDefinitions() output.
func (h *IMMessageHandler) SetToolDefGenerator(gen *ToolDefinitionGenerator) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolDefGen = gen
	// Invalidate cache so next call regenerates.
	h.cachedTools = nil
	h.toolsCacheTime = time.Time{}
}

// SetCapabilityGapDetector configures the capability gap detector and wires
// the confirmCallback so that CapabilityGapDetector.Resolve uses the shared
// confirmCriticalRiskSkill mechanism for critical-risk user confirmation.
func (h *IMMessageHandler) SetCapabilityGapDetector(detector *CapabilityGapDetector) {
	h.capabilityGapDetector = detector
	if detector != nil {
		detector.SetConfirmCallback(func(skillName, riskDetails string) bool {
			// Determine the platform from the active loop context.
			platform := ""
			if h.currentLoopCtx != nil {
				platform = h.currentLoopCtx.Platform
			}
			// Extract factors from riskDetails for the shared confirmation function.
			// The riskDetails string is pre-formatted by the detector; pass it as a
			// single-element factors slice so buildCriticalRiskPrompt includes it.
			factors := []string{riskDetails}
			return h.confirmRiskSkillInstall(
				context.Background(), skillName, "capability_gap_auto", security.RiskHigh, factors, platform, h.lastUserID,
			)
		})
	}
}

// SetToolRouter configures the tool router for context-aware tool filtering.
func (h *IMMessageHandler) SetToolRouter(router *ToolRouter) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolRouter = router
	// Wire the registry into the router so it can dynamically resolve
	// builtin tool names and use tags for TF-IDF scoring.
	if router != nil && h.registry != nil {
		router.SetRegistry(h.registry)
	}
}

// SetContextResolver configures the session context resolver for auto-detecting
// project paths and recommending tools.
func (h *IMMessageHandler) SetContextResolver(resolver *SessionContextResolver) {
	h.contextResolver = resolver
}

// SetUsageTracker configures the tool usage tracker for outcome recording.
func (h *IMMessageHandler) SetUsageTracker(tracker *tool.UsageTracker) {
	h.usageTracker = tracker
}

// SetSessionPrecheck configures the session precheck for environment validation.
func (h *IMMessageHandler) SetSessionPrecheck(precheck *SessionPrecheck) {
	h.sessionPrecheck = precheck
	// Keep orchestrator's external tool checker in sync.
	if h.taskOrchestratorRegistry != nil && precheck != nil {
		h.taskOrchestratorRegistry.SetExternalChecker(&sessionPrecheckAdapter{precheck: precheck})
	}
}

// SetStartupFeedback configures the startup feedback monitor.
func (h *IMMessageHandler) SetStartupFeedback(feedback *SessionStartupFeedback) {
	h.startupFeedback = feedback
}

// SetConfigManager configures the configuration manager for config tools.
func (h *IMMessageHandler) SetConfigManager(cm *ConfigManager) {
	h.configManager = cm
}

// SetMemoryStore configures the long-term memory store.
func (h *IMMessageHandler) SetMemoryStore(ms *memory.Store) {
	h.memoryStore = ms

	// Initialize session-start memory extractor (Codex-inspired improvement #5).
	// Uses the same LLM adapter pattern as ConversationArchiver.
	if ms != nil && h.app != nil {
		llmAdapter := &sessionStartLLMCaller{app: h.app}
		h.sessionStartExtractor = memory.NewSessionStartExtractor(ms, llmAdapter)
	}
}

// SetConfirmationStore configures the pending confirmation store.
func (h *IMMessageHandler) SetConfirmationStore(store *aiConfirmationStore) {
	h.confirmationStore = store
}

func (h *IMMessageHandler) SetTraceService(trace *AITraceService) {
	h.traceService = trace
}

func (h *IMMessageHandler) traceContextResolver() *SessionContextResolver {
	if h.contextResolver != nil {
		return h.contextResolver
	}
	if h.app != nil {
		_ = h.getContextResolver() // ensure
		return h.getContextResolver()
	}
	return nil
}

// SetTemplateManager configures the session template manager.
func (h *IMMessageHandler) SetTemplateManager(tm *remote.SessionTemplateManager) {
	h.templateManager = tm
}

// SetScheduledTaskManager configures the scheduled task manager.
func (h *IMMessageHandler) SetScheduledTaskManager(stm *scheduler.Manager) {
	h.scheduledTaskManager = stm
}

// SetBackgroundLoopManager configures the background loop manager.
func (h *IMMessageHandler) SetBackgroundLoopManager(blm *BackgroundLoopManager) {
	h.bgManager = blm
}

// SetSessionMonitor configures the session monitor.
func (h *IMMessageHandler) SetSessionMonitor(sm *SessionMonitor) {
	h.sessionMonitor = sm
}

// SetIMFileSender configures the callback used to forward files to the user's
// IM channels (Feishu/WeChat/etc.) when the agent is running on the desktop.
func (h *IMMessageHandler) SetIMFileSender(fn func(b64Data, fileName, mimeType, message string) error) {
	h.imFileSender = fn
}

// SetGoalAnchor configures the goal anchoring module for the agent loop.
func (h *IMMessageHandler) SetGoalAnchor(ga *GoalAnchor) {
	h.goalAnchor = ga
}

// SetDriftDetector configures the drift detection module for the agent loop.
func (h *IMMessageHandler) SetDriftDetector(dd *DriftDetector) {
	h.driftDetector = dd
}

// SetHarnessProgressTracker configures the progress tracking module for the agent loop.
func (h *IMMessageHandler) SetHarnessProgressTracker(pt *HarnessProgressTracker) {
	h.harnessProgressTracker = pt
}

// SetAdaptiveRetry configures the adaptive retry module for the agent loop.
func (h *IMMessageHandler) SetAdaptiveRetry(ar *AdaptiveRetry) {
	h.adaptiveRetry = ar
}

func (h *IMMessageHandler) SetTrajectoryRecorderFactory(factory func() *TrajectoryRecorder) {
	h.trajectoryRecorderFactory = factory
}

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
	var tools []map[string]interface{}

	// --- Phase 1 upgrade: prefer DynamicToolBuilder from ToolRegistry ---
	// Note: We use BuildAll() here intentionally 鈥?context-aware filtering
	// is handled downstream by routeTools() / ToolRouter which uses TF-IDF.
	// DynamicToolBuilder.Build(msg) is an alternative path for simpler setups
	// without ToolRouter.
	if h.toolBuilder != nil && h.registry != nil {
		h.toolsMu.RLock()
		cached := h.cachedTools
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
			tools = cached
		} else {
			// Sync dynamic tools (AgentNet, SkillHub) only on cache rebuild, not every call.
			h.syncAgentNetTools()
			h.syncSkillHubTools()

			tools = h.toolBuilder.BuildAll()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	} else {
		// --- Legacy path: ToolDefinitionGenerator or hardcoded ---
		h.toolsMu.RLock()
		gen := h.toolDefGen
		cached := h.cachedTools
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		// Fallback: no generator configured 鈥?use hardcoded definitions.
		if gen == nil {
			tools = h.buildToolDefinitions()
		} else if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
			// Return cached tools if still fresh (within 5 seconds).
			tools = cached
		} else {
			// Regenerate from the generator.
			tools = gen.Generate()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	}

	// In lite/simple mode (UIMode != "pro"), filter out coding session tools
	// since the user has not configured coding LLM providers. This removes
	// the tool definitions entirely so they are never sent to the LLM,
	// saving tokens and preventing the agent from attempting coding sessions.
	if !h.isProMode() {
		tools = filterCodingTools(tools)
	}

	return tools
}

// routeTools applies the ToolRouter to filter tools based on user message.
// If no router is configured, returns allTools unchanged.
//
// Tool selection (including conditional activation of ssh, browser, etc.)
// is fully handled by Route() via conditionalKeepRules + sessionTools.
// This function does not apply any additional per-tool filtering.
func (h *IMMessageHandler) routeTools(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	h.toolsMu.RLock()
	router := h.toolRouter
	h.toolsMu.RUnlock()

	if router == nil {
		return allTools
	}
	return router.Route(userMessage, allTools)
}
