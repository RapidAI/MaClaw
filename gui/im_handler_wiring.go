package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/nudge"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type IMMessageHandler struct {
	app     *App
	manager *RemoteSessionManager
	memory  *agent.ConversationMemory
	// Populated only on a private Lansenger profile runtime; not sourced from
	// model tool arguments.
	lansengerBotProfileID string
	client                *http.Client // chat-priority HTTP client (optimised transport)
	taskClient            *http.Client // background-task HTTP client (separate pool)

	// --- Extracted App dependencies (agent-unification Phase 1) ---
	// These fields are wired from App at construction time (GUI) or from
	// standalone components (TUI). Code should use h.getWorkflowEngine()
	// and h.getUnifiedClassifier() instead of h.app.XXX. The h.app field
	// is retained for not-yet-extracted deps.
	//
	// For GUI: these may be nil at construction (late-init goroutines) and
	// the accessor methods fall through to h.app.XXX as a bridge.
	// For TUI: h.app is nil, these fields are the sole source.

	// workflowEngine removed — V2 is the sole workflow engine.
	unifiedClassifier *intent.UnifiedIntentClassifier
	steeringStore     *steering.Store

	// workflowV2Adapters owns durable document bridges for V2 workflows when
	// the host has not installed the legacy engine callback adapter. Adapters
	// retain workflow-instance state, so they must be scoped per owner rather
	// than shared across concurrent desktop/IM workflows.
	workflowV2Adapters sync.Map // map[string]*GUIWorkflowAdapter

	// standaloneConfig holds the config from NewIMMessageHandlerStandalone.
	// nil when constructed via NewIMMessageHandler (GUI mode).
	standaloneConfig *StandaloneConfig

	// Unified tool registry and dynamic builder (Phase 1 upgrade).
	registry    *ToolRegistry
	toolBuilder *DynamicToolBuilder
	// clientToolDispatcher delegates a per-message dynamic tool call to the
	// originating third-party client. The dispatcher must return quickly; the
	// authoritative result arrives asynchronously through tool-result.
	clientToolDispatcher func(context.Context, agent.ClientToolContext, agent.ClientToolDefinition, string, map[string]any) error

	// Injectable dependency for search_and_install_skill execution. The default
	// implementation uses SkillMarket; tests can replace the dependency without
	// hijacking the registry dispatch path.
	skillSearchInstallHandler func(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult

	// Security firewall (Phase 2 upgrade).
	firewall *SecurityFirewall

	// Dynamic tool generation and routing (lazily initialized via setters).
	toolDefGen       *ToolDefinitionGenerator
	toolRouter       *ToolRouter
	usageTracker     *tool.UsageTracker
	taskStore        *task.Store
	goalStore        *goal.Store
	cachedTools      []map[string]interface{}
	cachedToolDefGen *ToolDefinitionGenerator
	toolsCacheTime   time.Time
	toolsMu          sync.RWMutex

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
	scheduledTaskManagerMu sync.RWMutex
	scheduledTaskManager   *scheduler.Manager

	traceService *AITraceService

	// Smart session startup components (lazily initialized via setters).
	contextResolver *SessionContextResolver
	sessionPrecheck *SessionPrecheck
	startupFeedback *SessionStartupFeedback

	// Configuration manager (lazily initialized via setter).
	configManager *ConfigManager

	// moaSessions arms one-shot multi-model council (/moa) per user.
	moaSessions *moaSessionStore

	// --- Per-session loop state ---
	// Each userID (desktop-user, desktop-user:{path}, IM users) gets its own
	// mutex and loop context. This allows project tabs to run agent loops
	// concurrently with the local tab without data races.
	sessionLoops sync.Map // map[string]*sessionLoopState

	// loopMaxOverride is a legacy bridge — set by "set_max_iterations" tool.
	// Kept for backward compat; new code should use LoopContext.MaxIterations.
	loopMaxOverride int

	// currentLoopCtx is DEPRECATED — kept only for code paths that don't have
	// access to the userID (will be removed incrementally). When sessionLoops
	// is used, this points to the LAST started loop (non-deterministic under
	// concurrency). Prefer getSessionLoopCtx(userID) instead.
	//
	// Protected by globalLoopMu to prevent data races under concurrent loops.
	globalLoopMu   sync.RWMutex
	currentLoopCtx *LoopContext
	lastUserText   string
	lastUserID     string

	// Background loop manager and session monitor (lazily initialized via setters).
	bgManager      *BackgroundLoopManager
	sessionMonitor *SessionMonitor

	// SSH session manager (lazily initialized on first SSH tool call).
	sshMgrOnce sync.Once
	sshMgr     *remote.SSHSessionManager
	bgTaskMgr  *remote.SSHBackgroundTaskManager

	sshMirrorWatchMu sync.Mutex
	sshMirrorWatch   map[string]struct{}

	// proactiveRecallInFlight prevents repeated prompt-build recalls for the
	// same user from piling up after the front-end budget has already expired.
	proactiveRecallInFlight sync.Map // map[string]proactiveRecallState

	// Local background task manager for long-running local processes.
	// Mirrors the SSH BackgroundTaskManager pattern: Submit/Check/Wait/Kill.
	localBgTaskMgr *tool.LocalBackgroundTaskManager

	// imFileSender is an optional callback that forwards a file to the user's
	// IM channels (Feishu/WeChat/etc.) via the Hub WebSocket. Set by the
	// desktop GUI after connecting to the Hub. When nil, IM forwarding is
	// silently skipped.
	imFileSender           func(b64Data, fileName, mimeType, message string) error
	structuredIMFileSender func(agent.IMFileDeliveryRequest) error

	// agentActivity is a process-local shared store that lets the GUI AI
	// assistant and IM channels see each other's active tasks.
	agentActivity *AgentActivityStore

	// lastScreenshotAt records the time of the last successful screenshot
	// to enforce a cooldown period and prevent accidental rapid-fire captures.
	lastScreenshotAt time.Time
	// screenshotCooldowns scopes screenshot cooldowns by runtime owner/request so
	// desktop, IM, third-party, and ownerless isolated runtimes do not throttle
	// each other.
	screenshotCooldowns sync.Map // map[string]time.Time

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

	// pendingV2SubAgentExecution is set when the V2 workflow advances to an
	// execution phase (implementation). It signals executePreparedIMEntry to
	// run CodingSubAgent instead of the normal agent loop. Using a dedicated
	// map avoids conflicts with stashedPhasePrompt (which gets consumed by
	// the system prompt builder's LoadAndDelete).
	pendingV2SubAgentExecution sync.Map

	// pendingTemplateCodingProjectPath stores the project path after the
	// pure coding (coding_dev) is armed; the next agent loop runs
	// CodingSubAgent instead of the normal chat loop.
	pendingTemplateCodingProjectPath sync.Map

	// pendingTemplateRemoteCoding stores context after pure remote coding
	// (remote_coding_dev) arms SSH; the next agent loop runs RemoteCodingSubAgent.
	pendingTemplateRemoteCoding sync.Map

	// codingWorkflowRemoteCreds holds session-only SSH secrets for coding
	// workflow remote variant (password/key). Keyed by userID; never persisted.
	// Non-secret host/user/port/workdir live in phase FormData + sticky memory.
	codingWorkflowRemoteCreds sync.Map

	// codingExecCheckpoint stores last coding implementation results so the user
	// can 重试失败 / 继续执行 after cancel or partial failure. Keyed by userID.
	codingExecCheckpoint sync.Map

	// pendingCodingExecRetryAction is set when the user asks to retry failed or
	// resume incomplete coding tasks. Values: "failed" | "resume". Keyed by userID.
	pendingCodingExecRetryAction sync.Map

	// stickyCodingWorkbenchMemory holds multi-turn plan/result context for pure
	// coding environments (create-task local/remote) so follow-up messages share
	// prior summaries and touched files.
	stickyCodingWorkbenchMemory sync.Map

	// pendingWorkflowChoice stores the original message and route result while
	// waiting for the user to choose how to handle a detected workflow task
	// (enter full workflow / skip to normal agent). Keyed by userID.
	pendingWorkflowChoice sync.Map

	// workflowReviewExperienceContext carries the trace/task context of the
	// phase output currently awaiting review. Keyed by userID; consumed by
	// review-intent events so user feedback can update the same injected
	// experience candidates that shaped the phase output.
	workflowReviewExperienceContext sync.Map

	// workflowPendingConfirmOther is set by handlePendingConfirm when the
	// LLM classifies the user's message as "other" (unrelated to the active
	// workflow's pending confirmation). Consumed (LoadAndDelete) by the
	// agent loop to skip the NeedsConfirm gate 鈥?otherwise the unrelated
	// LLM output (e.g. weather query result) would be captured as a phase
	// document and emitted to the doc preview panel.
	workflowPendingConfirmOther sync.Map

	// pendingCancelExecuteRequest stores the original task request when user
	// cancels a workflow but wants direct execution (e.g. "取消，直接处理").
	// Consumed (LoadAndDelete) by the agent loop to replace the cancel message
	// with the original task text.
	pendingCancelExecuteRequest sync.Map

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

	// pendingRecordAudio tracks interactive record_audio sessions waiting for
	// the user to stop recording. The next user message carries the saved path
	// and duration summary and is treated as a continuation of the same task.
	// Keyed by userID, value is *pendingRecordAudioState.
	pendingRecordAudio sync.Map

	// pendingPostRecording tracks a completed recording that is waiting for the
	// user to pick a post-processing action via engine-injected GUI buttons
	// (minutes / transcribe / keep_only). Keyed by userID, value is
	// *pendingPostRecordingState.
	pendingPostRecording sync.Map

	// pendingSpeakerEstimates caches CAM++ speaker-count estimates for the open
	// post-recording choice (userID -> int). Kept separate from
	// pendingPostRecording so background pre-estimate never races with the
	// step-2 confirm state machine via copy-on-write Store.
	pendingSpeakerEstimates sync.Map

	// pendingUserReply tracks plain-text assistant questions that expect the
	// next user message to continue the same task. Keyed by userID, value is
	// *pendingUserReplyState.
	pendingUserReply sync.Map

	// suppressPendingUserReplyUpdate preserves pendingUserReply for the current
	// request when intent classification was inconclusive. Keyed by userID.
	suppressPendingUserReplyUpdate sync.Map

	// deferredSessionExtraction stores prepared messages for session-start
	// extraction. Set during preflight, consumed after agent loop completes.
	// This ensures the extraction LLM call never competes with the main
	// agent loop for API bandwidth. Keyed by userID, value is
	// []memory.ConversationMessage.
	deferredSessionExtraction sync.Map

	// backgroundLLMCancel is the legacy single-session fallback. New agent
	// loops use backgroundLLMCancelByUser so one tab never cancels another tab's
	// background extraction work.
	backgroundLLMMu     sync.Mutex
	backgroundLLMCancel context.CancelFunc
	// Keyed by userID, value is context.CancelFunc for owner-scoped background
	// LLM work (online extraction, session-start extraction, semantic dedup).
	backgroundLLMCancelByUser sync.Map

	// postConversationScheduler runs non-visible post-loop work outside the
	// foreground response path. It is owner-scoped: same owner serializes and can
	// be canceled/replaced, different owners run independently.
	postConversationSchedulerMu sync.Mutex
	postConversationScheduler   *postConversationScheduler

	// Optional test hooks for pending-reply intent classification. Production
	// uses LLMClassify; tests inject deterministic classifiers without keyword
	// matching hidden in the implementation.
	pendingReplyPromptClassifier func(assistantText string) (bool, error)
	pendingReplyAnswerClassifier func(question, answer string) (bool, error)
	noToolReplyClassifier        func(text string) (agentNoToolReplyIntent, error)

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

	// snapshotWarmInflight singleflights static snapshot builds per userID.
	// Keyed by userID, value is chan struct{} (closed when the builder finishes).
	snapshotWarmInflight sync.Map

	// snapshotEpoch invalidates in-flight builds after RefreshMemorySnapshot:
	// a builder that started under gen N discards its result if gen advanced.
	// Keyed by userID, value is *atomic.Uint64.
	snapshotEpoch sync.Map

	// taskOrchestrator manages per-task execution during the coding
	// workflow's Execution Phase. When active, it injects per-task system
	// messages and constructs focused prompts for the internal CodingSubAgent
	// instead of letting the LLM dump the entire project description at once.
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

	// pendingPreLoopGuide stores guide-launch text that arrived before the
	// agent loop started (during preflight/intent-classification). Unlike
	// pendingInjection (consumed as system role with replan instruction),
	// this is consumed at iteration 0 as a user-role supplement — because
	// there is no "current plan" to re-evaluate yet.
	// Keyed by userID, value is *preLoopGuideEntry.
	pendingPreLoopGuide sync.Map

	// acceptedGuideLaunchIDs makes GUI guide-launch RPC retries idempotent. A
	// call can be accepted by the loop even if its response is lost; retrying
	// the durable queue entry must not inject the interjection twice. Keyed by
	// "userID\x00launchID", value is *guideLaunchAcceptance.
	acceptedGuideLaunchIDs sync.Map

	// cancelledTaskBoundary records that a user explicitly cancelled the
	// current task. The next normal user message must start a new task instead
	// of being merged into or classified as a continuation of the cancelled
	// task's history.
	// Keyed by userID, value is time.Time.
	cancelledTaskBoundary sync.Map

	// interruptHandler bridges IM gateways to the running agent loop's
	// cancel/merge/status mechanisms. Set during construction.
	interruptHandler *imInterruptHandler

	// activeBtwSubAgents tracks running /btw SubAgents by owner. This is the
	// primary cancellation registry for concurrent channels.
	// Keyed by userID, value is *BtwSubAgent.
	activeBtwSubAgents sync.Map

	// activeBtwSubAgent holds the most recent /btw SubAgent for legacy callers.
	// New cancellation paths must use activeBtwSubAgents.
	activeBtwSubAgent atomic.Pointer[BtwSubAgent]

	// activeLoopCallbacksByOwner tracks running /loop callbacks by owner.
	// Keyed by userID, value is *guiLoopCommandCallbacks.
	activeLoopCallbacksByOwner sync.Map

	// activeLoopCallbacks holds the most recent /loop callbacks for legacy callers.
	// New cancellation paths must use activeLoopCallbacksByOwner.
	activeLoopCallbacks atomic.Pointer[guiLoopCommandCallbacks]

	// activeExperimentOrchestrator holds a running RemoteExperimentOrchestrator
	// for the paper_reproduction workflow's iterative_improvement phase.
	// Keyed by userID, value is *RemoteExperimentOrchestrator.
	activeExperimentOrchestrator sync.Map

	// pendingExperimentNotification stores notifications from a background
	// experiment orchestrator that should be delivered to the user on their
	// next interaction.
	// Keyed by userID, value is string.
	pendingExperimentNotification sync.Map
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	var conversationMemory *agent.ConversationMemory
	var confirmationStore *aiConfirmationStore
	if app != nil {
		conversationMemory = app.ensureConversationMemory()
		confirmationStore = app.ensureAIConfirmationStore()
	}
	return newIMMessageHandler(app, manager, conversationMemory, confirmationStore)
}

// newIMMessageHandler builds an IM handler with caller-owned state stores.
// The public constructor supplies the App-wide stores. Hardware runtimes pass
// their private stores instead, so construction never briefly attaches a
// device runtime to desktop conversation or confirmation state.
func newIMMessageHandler(app *App, manager *RemoteSessionManager, conversationMemory *agent.ConversationMemory, confirmationStore *aiConfirmationStore) *IMMessageHandler {
	handlerStart := time.Now()
	// Response-header timeout: how long to wait for the FIRST byte from the
	// LLM API after sending the request. This is NOT the total streaming
	// duration 鈥?once headers arrive, SSE streaming continues without this
	// limit. The value follows the configured LLM timeout (default 600s,
	// clamped to 240-600s).
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
	// tasks) so they never starve the chat connection pool.
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

	if conversationMemory == nil {
		conversationMemory = agent.NewConversationMemory()
	}
	h := &IMMessageHandler{
		app:               app,
		manager:           manager,
		memory:            conversationMemory,
		confirmationStore: confirmationStore,
		client:            chatClient,
		taskClient:        taskClient,
		agentActivity:     NewAgentActivityStore(),
		unifiedClassifier: app.unifiedClassifier,
		steeringStore:     app.steeringStore,
	}
	// Construction is intentionally side-effect free with respect to
	// app.imHandler. Several paths create short-lived handlers (for example a
	// manual browser-tool invocation or a local gateway); publishing each one
	// here could replace the handler that owns an active task and disconnect its
	// interrupt tracker. The long-lived Hub lifecycle publishes its handler
	// explicitly after creation.
	h.interruptHandler = newIMInterruptHandler(h)
	// A handler can be created after the embedding model was activated (for
	// example after a Hub reconnect). Inherit the already-loaded shared runtime
	// immediately instead of waiting for a future activation event that may never
	// occur during this process lifetime.
	if emb := app.activeInterruptEmbedder(); emb != nil {
		h.interruptHandler.SetEmbedder(emb)
	}
	// Initialize ToolRegistry and register builtin tools.
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	// Register non-code tools (Git, file search, health check).
	registerNonCodeTools(h.registry, app)
	// Register browser automation tools (CDP-based).
	registerBrowserTools(h.registry, app)
	// Register the native OCR recognition tool.
	registerOCRTools(h.registry, app)
	// Register current-Hub MaClaw group discussion tools.
	registerGroupDiscussionTools(h.registry, app, h)
	// Knowledge retrieval is part of the baseline IM capability set. Local
	// gateways (including Lansenger group chat) create their own handlers and
	// may receive a message before the later desktop/Hub registration pass.
	// Register it here so the group permission policy can expose the explicitly
	// authorised knowledge_search tool instead of finding no registered tool.
	registerKnowledgeTools(h.registry, app)
	h.toolBuilder = NewDynamicToolBuilder(h.registry)
	// Handlers may be recreated after full embedding activation (for example a
	// Hub reconnect). In that case the activation callback will not run again,
	// so restore tool routing here as well. Intent-only embedding deliberately
	// does not enable tool vector search.
	if app != nil && app.embeddingActivated.Load() {
		if emb := app.activeInterruptEmbedder(); emb != nil {
			h.toolBuilder.SetEmbedder(emb)
		}
	}

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

	log.Printf("[NewIMMessageHandler] constructed in %v", time.Since(handlerStart))
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
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.registry = r
	h.toolBuilder = NewDynamicToolBuilder(r)
	h.cachedTools = nil
	h.cachedToolDefGen = nil
	h.toolsCacheTime = time.Time{}
}

// SetSecurityFirewall configures the security firewall for tool execution checks.
func (h *IMMessageHandler) SetSecurityFirewall(fw *SecurityFirewall) {
	h.firewall = fw
}

// SetSkillSearchInstallHandler replaces the SkillMarket search/install executor.
// Passing nil restores the production SkillMarket-backed implementation.
func (h *IMMessageHandler) SetSkillSearchInstallHandler(fn func(map[string]interface{}, tool.ProgressCallback) searchAndInstallSkillResult) {
	h.skillSearchInstallHandler = fn
}

// SetToolDefGenerator configures the dynamic tool definition generator.
// When set, it replaces the hardcoded buildToolDefinitions() output.
func (h *IMMessageHandler) SetToolDefGenerator(gen *ToolDefinitionGenerator) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolDefGen = gen
	// Invalidate cache so next call regenerates.
	h.cachedTools = nil
	h.cachedToolDefGen = nil
	h.toolsCacheTime = time.Time{}
}

// SetCapabilityGapDetector configures the capability gap detector and wires
// the confirmCallback so that CapabilityGapDetector.Resolve uses the shared
// confirmCriticalRiskSkill mechanism for critical-risk user confirmation.
func (h *IMMessageHandler) SetCapabilityGapDetector(detector *CapabilityGapDetector) {
	h.capabilityGapDetector = detector
	if detector != nil {
		detector.SetConfirmCallbackWithContext(func(ctx context.Context, skillName, riskDetails string) bool {
			platform, userID := capabilityGapRuntimeFromContext(ctx)
			// Extract factors from riskDetails for the shared confirmation function.
			// The riskDetails string is pre-formatted by the detector; pass it as a
			// single-element factors slice so buildCriticalRiskPrompt includes it.
			factors := []string{riskDetails}
			return h.confirmRiskSkillInstall(
				context.Background(), skillName, "capability_gap_auto", security.RiskHigh, factors, platform, userID,
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
	if router != nil {
		if uic := h.getUnifiedClassifier(); uic != nil {
			router.SetUnifiedClassifier(uic)
		}
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
	if h.adaptiveRetry != nil {
		h.adaptiveRetry.SetMemoryStore(ms)
	}

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
	if h == nil {
		return
	}
	h.scheduledTaskManagerMu.Lock()
	defer h.scheduledTaskManagerMu.Unlock()
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

// SetStructuredIMFileSender configures exact-target-aware IM file delivery.
// SetIMFileSender remains as a source-compatible adapter for legacy hosts/tests.
func (h *IMMessageHandler) SetStructuredIMFileSender(fn func(agent.IMFileDeliveryRequest) error) {
	h.structuredIMFileSender = fn
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
	if h.adaptiveRetry != nil {
		h.adaptiveRetry.SetMemoryStore(h.memoryStore)
	}
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
		cachedGen := h.cachedToolDefGen
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		if cached != nil && cachedGen == nil && time.Since(cacheTime) < toolsCacheTTL {
			tools = cached
		} else {
			// Sync dynamic tools (SkillHub) only on cache rebuild, not every call.
			h.syncSkillHubTools()

			tools = h.toolBuilder.BuildAll()
			tools = h.filterInactiveDeferredTools(tools)

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.cachedToolDefGen = nil
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	} else {
		// --- Legacy path: ToolDefinitionGenerator or hardcoded ---
		h.toolsMu.RLock()
		gen := h.toolDefGen
		cached := h.cachedTools
		cachedGen := h.cachedToolDefGen
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		// Fallback: no generator configured 鈥?use hardcoded definitions.
		if gen == nil {
			tools = h.buildToolDefinitions()
		} else if cached != nil && cachedGen == gen && time.Since(cacheTime) < toolsCacheTTL {
			// Return cached tools if still fresh (within 5 seconds).
			tools = cached
		} else {
			// Regenerate from the generator.
			tools = gen.Generate()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.cachedToolDefGen = gen
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	}

	// Agent coding now runs through the internal CodingSubAgent. External
	// coding-session tools stay out of the agent tool list in every UI mode.
	tools = filterCodingTools(tools)
	tools = filterDisabledExternalCodingSessionToolDefs(tools)

	return tools
}

func (h *IMMessageHandler) filterInactiveDeferredTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 || h == nil || h.toolDefGen == nil {
		return tools
	}
	deferred := make(map[string]bool, len(DeferredToolNames))
	for _, name := range DeferredToolNames {
		deferred[name] = true
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, item := range tools {
		name := extractToolName(item)
		if deferred[name] && !h.toolDefGen.IsDeferredToolActivated(name) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// routeTools applies the ToolRouter to filter tools based on user message.
// It is retained for compatibility callers; interactive Agent turns should
// use routeToolsForUser with their owner ID so routing remains local-only.
// If no router is configured, conditional tools fail closed so high-cost or
// sensitive tools such as ssh and browser automation are not exposed by a
// missing router setup.
func (h *IMMessageHandler) routeTools(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	return h.routeToolsForUser("", userMessage, allTools, true)
}

// routeToolsWithOptions is like routeTools. The skipHeavySemantic argument is
// retained for API compatibility; interactive routing never makes an auxiliary
// LLM request before the main agent response.
func (h *IMMessageHandler) routeToolsWithOptions(userMessage string, allTools []map[string]interface{}, skipHeavySemantic bool) []map[string]interface{} {
	return h.routeToolsForUser("", userMessage, allTools, true)
}

func (h *IMMessageHandler) routeToolsForUser(userID, userMessage string, allTools []map[string]interface{}, skipHeavySemantic bool) []map[string]interface{} {
	h.toolsMu.RLock()
	router := h.toolRouter
	h.toolsMu.RUnlock()

	if router == nil {
		filtered := make([]map[string]interface{}, 0, len(allTools))
		for _, item := range allTools {
			name := extractToolName(item)
			if name == "set_nickname" {
				if isExplicitNicknameRequest(userMessage) {
					filtered = append(filtered, item)
				}
				continue
			}
			if tool.IsConditionalTool(name) {
				continue
			}
			filtered = append(filtered, item)
		}
		// The router can be unavailable briefly during startup. Explicit IM task
		// management must still work in that window; otherwise conditional tools
		// such as manage_schedule and im_message are silently hidden.
		if isIMManagementRequest(userMessage) {
			filtered = ensureIMManagementToolsRouted(filtered, allTools, userMessage)
		}
		return filtered
	}
	// IM task and message management are safe, shared-state tools. Keep them
	// available for the current request when its intent is explicit, so every
	// local IM gateway (蓝信、微信、Telegram、QQ、第三方) can use them. Do not
	// pin them on ToolRouter: App shares that router across IM handlers, and a
	// pin from one conversation must never leak into another conversation.
	if uic := h.getUnifiedClassifier(); uic != nil {
		router.SetUnifiedClassifier(uic)
	}
	// Tool selection is an affordance, not an authority. Never issue a second
	// LLM request just to rewrite a message before the main Agent can respond.
	// BM25 plus optional local embedding provides enough pruning; uncertain
	// conditional tools stay hidden and can be discovered explicitly later.
	// Keep the parameter for callers compiled against the older API.
	_ = skipHeavySemantic
	routeOpts := tool.RouteOptions{
		SkipUnifiedClassifier: false,
		PreferEmbeddingOnly:   true,
	}
	routed := router.RouteForSession(userID, userMessage, allTools, routeOpts)
	if isIMManagementRequest(userMessage) {
		routed = ensureIMManagementToolsRouted(routed, allTools, userMessage)
	}
	if isExplicitNicknameRequest(userMessage) {
		return routed
	}
	filtered := make([]map[string]interface{}, 0, len(routed))
	for _, item := range routed {
		if extractToolName(item) == "set_nickname" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func ensureIMManagementToolsRouted(routed, allTools []map[string]interface{}, userMessage string) []map[string]interface{} {
	if len(allTools) == 0 {
		return routed
	}
	requested := imManagementToolNames(userMessage)
	if len(requested) == 0 {
		return routed
	}
	available := make(map[string]map[string]interface{}, len(allTools))
	for _, item := range allTools {
		if name := extractToolName(item); name != "" {
			available[name] = item
		}
	}
	selected := make(map[string]bool, len(routed))
	for _, item := range routed {
		selected[extractToolName(item)] = true
	}
	missing := make([]map[string]interface{}, 0, len(requested))
	required := make(map[string]bool, len(requested))
	for _, name := range requested {
		required[name] = true
		item, ok := available[name]
		if !ok || selected[name] {
			continue
		}
		missing = append(missing, item)
	}
	if len(missing) == 0 {
		return routed
	}

	// Make room for all missing explicit tools before appending any of them.
	// Replacing the tail one-by-one can evict a management tool appended in the
	// previous iteration when several explicit tools are needed (for example,
	// scheduling a task that pushes its result to an IM channel).
	toRemove := len(routed) + len(missing) - maxToolBudget
	if toRemove > 0 {
		keptReversed := make([]map[string]interface{}, 0, len(routed)-toRemove)
		removed := 0
		for i := len(routed) - 1; i >= 0; i-- {
			name := extractToolName(routed[i])
			if removed < toRemove && !required[name] {
				removed++
				continue
			}
			keptReversed = append(keptReversed, routed[i])
		}
		routed = make([]map[string]interface{}, len(keptReversed))
		for i := range keptReversed {
			routed[len(keptReversed)-1-i] = keptReversed[i]
		}
	}
	return append(routed, missing...)
}

func imManagementToolNames(userMessage string) []string {
	s := strings.ToLower(strings.TrimSpace(userMessage))
	if s == "" {
		return nil
	}
	var names []string
	for _, marker := range []string{"定时", "日程", "提醒", "cron", "schedule", "timer", "任务管理", "执行任务", "暂停任务", "恢复任务"} {
		if strings.Contains(s, marker) {
			names = append(names, "manage_schedule")
			break
		}
	}
	for _, marker := range []string{"推送", "发到", "发送到", "im_message"} {
		if strings.Contains(s, marker) {
			names = append(names, "im_message")
			break
		}
	}
	// File delivery needs both target discovery and the artifact sender. Short
	// voice transcripts are often classified as light turns, so mark these tools
	// explicitly instead of relying only on semantic retrieval.
	fileIntent := false
	for _, marker := range []string{"文件", "报告", "文档", "附件", "表格", "图片", "照片", "录音", "音频", "pdf", "docx", "xlsx", "pptx", "file", "attachment", "report", "document"} {
		if strings.Contains(s, marker) {
			fileIntent = true
			break
		}
	}
	if fileIntent && len(names) > 0 {
		names = append(names, "send_to_im", "send_file")
	}
	return names
}

func isIMManagementRequest(userMessage string) bool {
	return len(imManagementToolNames(userMessage)) > 0
}
