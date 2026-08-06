package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	llmcompat "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/session"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/user"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// App struct
type App struct {
	ctx             context.Context
	CurrentLanguage string
	watcher         *fsnotify.Watcher

	// configLastInternalWrite records the time of the most recent internal
	// config file write (SaveConfig/PatchConfigFields/patchConfig). The
	// fsnotify watcher uses this to suppress redundant config-updated events
	// triggered by our own writes (debounce window: 500ms).
	configLastInternalWrite atomic.Int64
	// pendingBugReportUpload keeps the most recent locally-created diagnostics
	// archive available after an upload failure. It is deliberately kept outside
	// config: the archive contains sensitive diagnostics and should not become a
	// permanent application setting.
	pendingBugReportUploadMu sync.Mutex
	pendingBugReportUpload   *pendingBugReportUpload
	// bugReportUploadMu serializes archive creation and retry uploads. Without
	// this, a second submission could remove the first pending archive while a
	// retry is still streaming it to HubCenter.
	bugReportUploadMu sync.Mutex
	// virtualRepositorySyncScheduleMu coalesces rapid local edits into one Hub
	// synchronization and keeps an offline edit queued for bounded retry.
	virtualRepositorySyncScheduleMu sync.Mutex
	virtualRepositorySyncStateMu    sync.Mutex
	virtualRepositorySyncTimer      *time.Timer
	// virtualRepositorySyncGeneration advances after every local virtual
	// repository mutation. A sync run compares it around its network phase so
	// it never applies an older snapshot over a newly saved local change.
	virtualRepositorySyncGeneration atomic.Uint64
	// virtualRepositoryBackgroundSyncs counts automatic sync work that should
	// block the manual Sync button: the short debounce queue and an active run.
	// Failed-retry waits deliberately do not increment this counter so a Hub or
	// SSH outage cannot leave the UI stuck on "syncing" for minutes.
	virtualRepositoryBackgroundSyncs atomic.Int32
	// Background-sync UI phase/message fields are guarded by
	// virtualRepositorySyncScheduleMu (same lock as the retry timer).
	virtualRepositorySyncPhase         string
	virtualRepositorySyncStatusMessage string
	// virtualRepositorySyncRepairRepositoryID identifies the remote repository
	// that caused a recoverable automatic-sync failure. It is exposed as
	// structured status metadata so the UI never has to parse an error string.
	virtualRepositorySyncRepairRepositoryID string
	virtualRepositorySyncAttempt            int
	virtualRepositorySyncNextRetryAt        time.Time
	virtualRepositorySyncTimerBlocksUI      bool

	// Managers to reduce struct complexity
	managers                           *AppManagers
	testHomeDir                        string // For testing purposes
	downloadCancelers                  map[string]context.CancelFunc
	downloadMutex                      sync.Mutex
	skillInstallConfirm                sync.Map
	IsInitMode                         bool
	IsAutoStart                        bool
	installingNode                     bool      // Flag to prevent concurrent Node.js installation
	installingGit                      bool      // Flag to prevent concurrent Git installation
	nodeInstallDone                    chan bool // Channel to signal Node.js installation completion
	installMutex                       sync.Mutex
	toolInstallLocks                   map[string]bool // Track which tools are currently being installed
	toolLockMutex                      sync.Mutex      // Mutex for toolInstallLocks map
	remoteSessions                     *RemoteSessionManager
	remoteActivationBackgroundDisabled bool // test-only: skip post-activation background connect
	browserSessions                    *BrowserAgentManager
	powerStateMutex                    sync.Mutex
	powerStateProcess                  *exec.Cmd
	screenDimCancel                    context.CancelFunc // cancels the screen-dim goroutine
	workstationCancel                  context.CancelFunc // cancels the workstation-mode anti-lock goroutine
	mcpRegistry                        *MCPRegistry
	localMCPManager                    *LocalMCPManager
	skillExecutor                      *SkillExecutor
	cachedSkillScanner                 *CachedSkillScanner
	skillRunner                        *SkillRunner
	maclawAppInstallMixedSkill         func(source, id, installRef string) error
	sessionStarter                     *CodingSessionStarter
	skillMarketClient                  *SkillMarketClient
	skillMarketAutoLoginRunning        atomic.Bool
	skillMarketAutoLoginNextAttempt    atomic.Value // stores time.Time; throttles failed machine-login retries
	// petStoreSessionRefreshMu prevents several initial Pet Store requests
	// (listings, account, and rankings) from all exchanging the same expired
	// session at once when the embedded market opens.
	petStoreSessionRefreshMu sync.Mutex
	skillLifecycle           *SkillLifecycleManager
	gossipClient             *GossipClient
	gossipAutoPublish        *AutoPublishTrigger
	evolutionPipeline        *skill.EvolutionPipeline
	// Maclaw capability evolution components
	riskAssessor      *RiskAssessor
	policyEngine      *PolicyEngine
	auditLog          *AuditLog
	llmSecurityReview *LLMSecurityReview
	mdnsScanner       *MDNSScanner
	projectScanner    *ProjectScanner
	toolDefGenerator  *ToolDefinitionGenerator
	toolRouter        *ToolRouter

	unifiedClassifier        *intent.UnifiedIntentClassifier // UIC: shared three-layer intent classifier
	classifierOnce           sync.Once                       // guards single creation of unifiedClassifier + gateIntentClassifier
	embeddingActivated       atomic.Bool                     // ensures activateEmbedderAsync runs at most once
	intentEmbeddingActive    atomic.Bool                     // ensures UIC-only embedding activation runs at most once
	embeddingMu              sync.Mutex
	intentEmbedder           embedding.Embedder // local model held for UIC and reused when vector search is enabled
	intentEmbedderPath       string             // absolute/loader-provided path for the shared local embedding runtime
	usageTracker             *tool.UsageTracker
	goalContinuation         *GoalContinuationEngine
	experienceEvents         *lifecycle.EventTrail
	experienceSink           lifecycle.EventSink
	experienceExtractor      *ExperienceExtractor
	llmEndpointFailuresOnce  sync.Once
	llmEndpointFailures      *llmEndpointFailureGate
	orchestrator             *Orchestrator
	sharedContext            *SharedContextStore
	toolSelector             *ToolSelector
	skillHubClient           *SkillHubClient
	capabilityGapDetector    *CapabilityGapDetector
	agentRegistry            *agent.AgentRegistry
	agentRegistryOnce        sync.Once
	ohModules                openhumanModules
	lastModelRouteMu         sync.RWMutex
	lastModelRoute           modelRouteDecision // last turn routing decision (debug/settings)
	stopHubTicker            chan struct{}      // signals the 24h recommendation refresh goroutine to stop
	hubUpdCache              *hubUpdateCache
	hubCenterCache           *remote.HubCenterSelectionCache // shared cache from corelib/remote
	hubCenterPersister       *guiHubCenterPersister          // persister for HubCenter URL config
	oauthMu                  sync.Mutex
	oauthCancel              context.CancelFunc
	oauthGeneration          uint64
	xaiOAuthSession          *oauth.XAIAuthSession
	xaiOAuthAuthorizationURL string
	credentialStore          *oauth.FileCredentialStore  // independent OAuth credential storage (credentials.json)
	anthropicOAuthParams     *oauth.AnthropicOAuthParams // in-progress Anthropic OAuth params
	copilotDeviceCode        string                      // in-progress GitHub Copilot device code
	copilotPollInterval      int                         // Copilot device code poll interval
	copilotPollCtx           context.Context             // Copilot device code polling context
	// Smart session components
	memoryStore                       *memory.Store
	memoryStoreMu                     sync.Mutex
	configManager                     *ConfigManager
	templateManager                   *remote.SessionTemplateManager
	contextResolver                   *SessionContextResolver
	sessionPrecheck                   *SessionPrecheck
	conversationArchiver              *ConversationArchiver
	sessionCheckpointer               *SessionCheckpointer
	startupFeedback                   *SessionStartupFeedback
	imHandler                         *IMMessageHandler
	ioRelay                           *SessionIORelay
	aiConversationMemory              *agent.ConversationMemory
	conversationMemoryMu              sync.Mutex
	aiConfirmationStore               *aiConfirmationStore
	confirmationStoreMu               sync.Mutex
	swarmOrchestrator                 *swarm.SwarmOrchestrator
	swarmInitOnce                     sync.Once // per-App; package-level Once would skip init for later test Apps
	memoryCompressor                  *MemoryCompressor
	memoryMaintenance                 *memory.Maintenance
	memPipeline                       *memory.Pipeline
	memoryPipelineDebounce            time.Duration
	disableBackgroundEmbeddingForTest bool
	memoryPipelineScheduleMu          sync.Mutex
	memoryPipelineTimer               *time.Timer
	memoryPipelineScheduleSeq         uint64
	memoryPipelineRunCancel           context.CancelFunc
	memoryPipelineRunSeq              uint64
	memoryPipelineRunActive           bool
	foregroundAgentLoops              atomic.Int64
	compressorMu                      sync.Mutex // guards lazy creation of memoryCompressor
	scheduledTaskManagerMu            sync.Mutex // guards lazy scheduler creation across concurrent IM gateways
	scheduledTaskManager              *scheduler.Manager
	// scheduleTargetCatalogs maps delivery channel → target list (groups/users).
	// Agent tools use channel as a parameter; new IM platforms only register here.
	scheduleTargetCatalogs            *scheduler.TargetCatalogRegistry
	scheduleTargetListCache           *scheduler.TargetListCache
	scheduleTargetCatalogsOnce        sync.Once
	deliveryStateStoreCached          *scheduler.DeliveryStateStore
	deliveryStateStoreOnce            sync.Once
	remoteInfraOnce                   sync.Once   // guards ensureRemoteInfra initialization
	remoteInfraReady                  atomic.Bool // fast-path check for ensureRemoteInfra
	backgroundUpdateOnce              sync.Once   // guards startBackgroundUpdateChecks single loop
	warmupDone                        atomic.Bool // true after WarmupTools + WarmupHTTPConn complete
	mcpAutoDiscovery                  *MCPAutoDiscovery
	llmPromptCache                    *corelib.LLMPromptResponseCache
	llmPromptCacheDir                 string
	llmPromptCacheMu                  sync.Mutex
	localCacheHitRateStates           sync.Map // provider string → *localLLMCacheHitRateState
	toolVersionCache                  *ToolVersionCache
	securityFirewall                  *SecurityFirewall
	securityRiskAnalyzer              *SecurityRiskAnalyzer
	hubSecurityCache                  hubSecurityCache
	digitalEmployeeAuthCache          digitalEmployeeAuthorizationCache
	hubViewerTokenRecoveryNextAttempt atomic.Value // stores time.Time; throttles failed viewer-token recovery
	capabilitySyncRunning             atomic.Bool
	capabilitySyncNextAttempt         atomic.Value // stores time.Time; throttles heartbeat-triggered managed sync retries and steady-state re-syncs
	hubMarketplaceUnsupported         atomic.Bool  // capability discovery: hub doesn't have marketplace API
	hubMarketplace404URL              atomic.Value // stores the hub URL (string) that returned 404
	managedDeploymentIDs              sync.Map     // capability_ref (string) -> true; cached from last sync
	contextBridge                     *ContextBridge
	aiTrace                           *AITraceService
	taskOrchestrator2                 *TaskOrchestrator2
	qqBotGateway                      *qqBotGatewayManager
	telegramGateway                   *telegramGatewayManager
	weixinGateway                     *weixinGatewayManager
	lansengerGateway                  *lansengerGatewayManager
	localStartMenu                    *localStartMenuService
	localStartMenuMu                  sync.Mutex
	lansengerWatch                    *lansengerWatchService
	lansengerWatchOnce                sync.Once
	thirdPartyGateway                 *thirdPartyGatewayManager
	acpHost                           *acpHost // Mode B: loopback ACP for VS Code programming agent
	acpHostMu                         sync.Mutex
	imGatewaySyncMu                   sync.Mutex
	passthroughRegistry               *PassthroughRegistry
	iworkerGoalWatch                  *IWorkerGoalWatchService
	iworkerGoalWatchMu                sync.Mutex
	// configMu serializes writers (load/mutate/publish). Hot-path readers do NOT
	// take configMu: they copy from configSnap (atomic.Pointer to an immutable
	// AppConfig). Writers always publish a fresh heap snapshot via
	// publishConfigLocked; invalidation stores nil.
	// Disk I/O uses configDiskMu and must not hold configMu.
	configMu   sync.Mutex
	configSnap atomic.Pointer[corelib.AppConfig]
	// nlSkillsSnap holds the skill table separately so config snaps and token
	// patches stay small. Always published together with configSnap.
	nlSkillsSnap atomic.Pointer[[]corelib.NLSkillEntry]
	// configCache / configCacheValid are write-side mirrors kept under configMu
	// for mutation helpers and for unit tests that seed App{} composites.
	// Prefer publishedConfig() / publishConfigLocked for all production paths.
	configCache      corelib.AppConfig
	configCacheValid bool
	configDiskMu     sync.Mutex // serializes config.json writes
	// pendingDataDirReset is set under configMu when the effective data dir
	// changes; drainPendingDataDirReset must run only after releasing configMu
	// so DB/memory teardown never blocks LoadConfig readers.
	pendingDataDirReset atomic.Bool
	// pendingConfigDiskWrite is set when loadConfigLocked produced an in-memory
	// config that must be written after releasing configMu (first-run bootstrap,
	// legacy migration materialize, pet-field migration). Unified drain is
	// runConfigPostUnlock → drainPendingConfigDiskWrite.
	pendingConfigDiskWrite     atomic.Bool
	effectiveBaseDir           atomic.Value // stores string
	tokenUsageMu               sync.Mutex   // guards AccumulateLLMTokenUsage + pending flush
	tokenUsagePending          map[string]*pendingLLMTokenDelta
	tokenUsageFlushScheduled   bool
	ssoPolling                 *ssoPollingSession // active embedded SSO polling session
	ssoPollingMu               sync.Mutex         // guards ssoPolling
	interactionInfraOnce       sync.Once          // guards ensureInteractionInfra initialization
	interactionInfraDone       atomic.Bool
	hubClientEnsureMu          sync.Mutex
	aiAssistantReadyAt         atomic.Int64
	aiAssistantFirstChatLogged atomic.Bool
	docGenerator               *swarm.SwarmDocGenerator        // cached PDF doc generator
	workflowEngine             *workflow.WorkflowEngine        // TEST ONLY: never instantiated in production. Always nil at runtime.
	workflowV2                 *workflowV2State                // V2 workflow engine (clean state machine)
	workflowArtifactSaver      *deferredArtifactSaver          // shared artifact saver for OwnerID injection
	workflowDisabled           atomic.Bool                     // true when user disables workflow in settings; checked by getWorkflowEngine()
	steeringStore              *steering.Store                 // declarative rule injection (corelib/steering)
	codeEventEmitter           *CodeEventEmitter               // emits code file events to frontend for code preview panel
	codingKnowledgeStore       *knowledge.CodingKnowledgeStore // independent coding experience store (coding_knowledge.db)
	deepCrawlMu                sync.Mutex                      // guards deepCrawlCancel
	deepCrawlCancel            context.CancelFunc              // cancels active deep crawl session
	deepCrawlCtx               context.Context                 // active deep crawl context (used to identify ownership)
	deepCrawlMode              string                          // active deep crawl owner: crawl or preview
	floatingAssistant          *FloatingAssistantManager
	floatingAssistantMu        sync.Mutex
	agentViewEmissionSeq       atomic.Int64
	agentViewOpenMu            sync.Mutex
	agentViewOpen              map[string]agentViewOpenRecord // view_id → open revision/schema

	// IM audit store (SQLite-backed IM message audit for review/export).
	imAuditStore   *IMAuditStore
	imAuditStoreMu sync.Once

	// Session search store (FTS5 full-text search across historical conversations).
	sessionSearchStore *session.Store
	sessionStoreMu     sync.Once

	// User model (dialectic user modeling with confidence-scored dimensions).
	userModel   *user.Model
	userModelMu sync.Once

	// Evidence collector for user modeling (lazily initialized).
	evidenceCollector   *user.Collector
	evidenceCollectorMu sync.Once

	// TTS manager (lazy-loaded, auto-unloading).
	ttsManager *tts.Manager

	// Project Tab session persistence (disk-backed session read/write + cleanup).
	projectTabSessionPersist *ProjectTabSessionPersist

	// tabProjectPaths caches tabID -> projectPath mappings in memory to avoid
	// unnecessary disk reads in SendMessageForTab. Populated in CreateProjectTabSession.
	tabProjectPaths sync.Map

	// tabWorkingDirOverrides caches projectPath -> user-specified working directory.
	// Written immediately when user clicks the directory switcher (SetTabWorkingDir).
	// Read by recentTaskExecutionProjectPath() BEFORE checking the persistent
	// workingDir tag in memory, ensuring the new directory takes effect on the
	// very next agent loop without waiting for memory flush + index rebuild.
	tabWorkingDirOverrides sync.Map

	// ACP conversations need a private owner even when they use one workspace.
	acpSessionWorkingDirs sync.Map // ACP ownerID -> normalized working directory

	// sessionEventScopeIDs caches userID -> event_scope_id (tab ID) mappings.
	// Populated when SendAIAssistantMessage receives a request with event_scope_id.
	// Used by workflow event emission to include the scope ID in all events.
	sessionEventScopeIDs sync.Map

	// Sticky VE session caches: (veID -> sessionID) and (sorted participant key -> sessionID).
	// Ensures conversations with the same VE/group always reuse the same session unless archived.
	veSessionCache           sync.Map // string -> string
	groupSessionCache        sync.Map // string -> string
	groupSessionReturnVEIDs  sync.Map // sessionID -> []string
	veDefaultResponder       sync.Map // sessionID -> participantID
	veSessionActiveCache     sync.Map // string -> time.Time
	veDetailRefreshCache     sync.Map // sessionID -> *veDetailRefreshState
	veDiscoverableCache      sync.Map // hubURL/token/localID -> veDiscoverableCacheEntry
	veDiscoverableCacheEpoch atomic.Uint64

	// VE session renewal tracking (closed session -> renewed session mapping).
	veSessionRenewalMap sync.Map // closedSessionID -> *veSessionRenewalEntry

	// Capability market sync: permanently skipped capabilities (with TTL).
	capabilitySyncPermanentSkips sync.Map // capability ID -> time.Time

	// Skill operation recorder: records tool calls and generates portable skills.
	skillRecorder *SkillOperationRecorder

	// Notification cache: in-memory LRU cache of up to 10 unread notifications.
	notifCache *notificationCache
}

type pendingBugReportUpload struct {
	zipPath       string
	includedFiles int
}

type pendingBugReportUploadMetadata struct {
	ZipPath       string `json:"zip_path"`
	IncludedFiles int    `json:"included_files"`
}

func (a *App) ensureExperienceLifecycleSink() lifecycle.EventSink {
	if a == nil {
		return lifecycle.NoopEventSink{}
	}
	if a.experienceEvents == nil {
		a.experienceEvents = lifecycle.NewEventTrail(512)
	}
	if a.experienceSink == nil {
		a.experienceSink = &lifecycle.AttributingEventSink{Sink: a.experienceEvents, Resolve: a.resolveExperienceProviderForAttribution}
	}
	if a.usageTracker != nil {
		a.usageTracker.SetExperienceEventSink(a.experienceSink)
	}
	if a.memoryStore != nil {
		a.memoryStore.SetExperienceEventSink(a.experienceSink)
	}
	return a.experienceSink
}

func (a *App) resolveExperienceProviderForAttribution() lifecycle.Provider {
	if a == nil || a.memoryStore == nil {
		return nil
	}
	providers := []lifecycle.Provider{memory.NewExperienceProvider(a.memoryStore)}
	if a.skillExecutor != nil {
		// loadSkills uses configMu; do not hold skillExecutor.mu across it.
		skills := a.skillExecutor.loadSkills()
		if len(skills) > 0 {
			providers = append(providers, skill.NewExperienceProvider(skills))
			providers = append(providers, skill.NewGovernanceDraftProvider(skills, skill.SkillMaintenancePlanOptions{MaxActions: 12}))
		}
	}
	// workflowEngine experience provider not used.
	return lifecycle.NewCompositeProvider(providers...)
}

// Safe no-op defaults so callers never need nil checks before tray is ready.
var OnConfigChanged func(corelib.AppConfig) = func(corelib.AppConfig) {}
var UpdateTrayMenu func(string) = func(string) {}
var UpdateTrayVisibility func(bool) = func(bool) {}

// UpdateComputerUseTray refreshes tray Computer Use submenu labels/enabled state.
// Set by platform tray setup (Windows); no-op elsewhere.
var UpdateComputerUseTray func() = func() {}

// ShowNotification displays a system tray balloon/toast notification.
// title is the notification title, message is the body text.
// iconFlag: 0=none, 1=info, 2=warning, 3=error
var ShowNotification func(title, message string, iconFlag uint32) = func(string, string, uint32) {}

// FlashAndBeep plays a notification sound and flashes the taskbar/dock icon.
// Set by platform-specific tray setup code.
var FlashAndBeep func() = func() {}

// NewApp creates a new App application struct
func NewApp() *App {
	bgCtx := context.Background()
	return &App{
		ctx:               nil,
		downloadCancelers: make(map[string]context.CancelFunc),
		nodeInstallDone:   make(chan bool, 1), // Buffered channel to signal Node.js installation completion
		toolInstallLocks:  make(map[string]bool),
		floatingAssistant: nil,
		managers:          NewAppManagers(bgCtx),
		hubUpdCache:       newHubUpdateCache(),
		hubCenterCache:    remote.NewHubCenterSelectionCache(60 * time.Second),
		toolVersionCache:  NewToolVersionCache(),
		notifCache:        newNotificationCache(),
	}
}

func bytesToMiB(v uint64) uint64 {
	return v / 1024 / 1024
}

func (a *App) logMemorySnapshot(stage string) {
	var stats goruntime.MemStats
	goruntime.ReadMemStats(&stats)
	log.Printf("[mem] %s heap=%dMiB sys=%dMiB stack=%dMiB objects=%d goroutines=%d gc=%d",
		stage,
		bytesToMiB(stats.HeapAlloc),
		bytesToMiB(stats.Sys),
		bytesToMiB(stats.StackInuse),
		stats.HeapObjects,
		goruntime.NumGoroutine(),
		stats.NumGC,
	)
}

// ensureRemoteInfra initializes remoteSessions, mcpRegistry, and skillExecutor
// if they haven't been created yet. Call this before any remote operation.
// Thread-safe: uses sync.Once-style check-lock-check to avoid races.
// Only initializes Layer 0 (core) components for minimal startup memory.
func (a *App) ensureRemoteInfra() {
	// Ultra-fast path: atomic load, no lock.
	if a.remoteInfraReady.Load() {
		return
	}
	a.remoteInfraOnce.Do(func() {
		t0 := time.Now()
		a.initCoreInfra()
		a.remoteInfraReady.Store(true)
		log.Printf("[ensureRemoteInfra] first-time init done in %v", time.Since(t0))
	})
}

// ---------------------------------------------------------------------------
// Layer 0 -Core infrastructure: minimal set needed for Hub connection and
// basic IM message routing. Initialized at startup.
// ---------------------------------------------------------------------------
func (a *App) initCoreInfra() {
	coreStart := time.Now()

	// Deploy bundled skills (pdf-word, doc-redact, etc.) to the active data directory.
	// on first run. Idempotent — skips if already deployed.
	if strings.TrimSpace(a.testHomeDir) == "" {
		_, _ = a.LoadConfig()
		if primaryDir, err := a.primarySkillsDir(); err == nil {
			deployBuiltinSkillsToDir(primaryDir)
		}
	}

	if a.remoteSessions == nil {
		a.remoteSessions = NewRemoteSessionManager(a)
	}
	if a.browserSessions == nil {
		a.browserSessions = NewBrowserAgentManager(a)
	}
	if a.mcpRegistry == nil {
		a.mcpRegistry = NewMCPRegistry(a)
	}
	if a.skillExecutor == nil {
		a.skillExecutor = NewSkillExecutor(a, a.mcpRegistry, a.remoteSessions)
	}
	if a.cachedSkillScanner == nil {
		a.cachedSkillScanner = &CachedSkillScanner{}
		cfg, err := a.LoadConfig()
		if err == nil {
			roots := a.skillScanRootsWithExternal(cfg.ExternalSkillDirs)
			a.cachedSkillScanner.Init(roots)
		} else {
			roots := a.skillScanRootsWithExternal(nil)
			a.cachedSkillScanner.Init(roots)
		}
	}
	if a.sessionStarter == nil {
		a.sessionStarter = NewCodingSessionStarter(a)
	}
	if a.riskAssessor == nil {
		a.riskAssessor = &RiskAssessor{}
	}
	if a.policyEngine == nil {
		mode := ""
		if cfg, err := a.LoadConfig(); err == nil {
			mode = cfg.SecurityPolicyMode
		}
		a.policyEngine = NewPolicyEngineWithMode(mode)
	}
	if a.toolDefGenerator == nil {
		tdgStart := time.Now()
		builtins := (&IMMessageHandler{}).buildToolDefinitions()
		a.toolDefGenerator = NewToolDefinitionGenerator(a.mcpRegistry, builtins)
		a.toolDefGenerator.SetLocalMCPManager(a.localMCPManager)
		// Progressive tool discovery: defer low-frequency tools so they don't
		// bloat the initial prompt. The LLM can discover them via discover_tool.
		a.toolDefGenerator.SetDeferredTools(DeferredToolNames)
		log.Printf("[initCoreInfra] toolDefGenerator built in %v", time.Since(tdgStart))
	}
	if a.toolRouter == nil {
		a.toolRouter = NewToolRouter(a.toolDefGenerator)
	}
	// Wire skill-aware routing: when skillExecutor is available, connect it
	// to the toolRouter so that Route() can match user messages against
	// installed skills and enrich the manage_skill tool description with
	// "Available Skill: xh-md-to-pdf" hints. Without this wiring, skillMatchScore
	// always returns 0 and the LLM never gets skill-specific routing hints.
	if a.toolRouter != nil && a.skillExecutor != nil {
		a.toolRouter.SetSkillProvider(&skillExecutorProvider{executor: a.skillExecutor})
	}
	if a.usageTracker == nil {
		trackerPath := tool.DefaultUsageTrackerPath()
		if trackerPath != "" {
			tracker, err := tool.NewUsageTracker(trackerPath)
			if err != nil {
				log.Printf("[App] failed to load usage tracker: %v", err)
			} else {
				a.usageTracker = tracker
				a.ensureExperienceLifecycleSink()
				a.toolRouter.SetUsageTracker(tracker)

				// Register fingerprint providers for invalidation detection.
				tracker.FingerprintProviders = []tool.FingerprintProvider{
					tool.NewConfigFingerprintProviderFromFields(func(toolName string) map[string]interface{} {
						// Fingerprint LLM config fields that affect tool reliability.
						cfg, err := a.LoadConfig()
						if err != nil {
							return nil
						}
						switch toolName {
						case "craft_tool", "delegate_task", "ask_user":
							return map[string]interface{}{
								"provider": cfg.MaclawLLMCurrentProvider,
							}
						}
						return nil
					}),
					tool.NewSSHFingerprintProviderFromStatic(func(toolName string) *tool.StaticSSHHostConfig {
						if toolName != "ssh" {
							return nil
						}
						cfg, err := a.LoadConfig()
						if err != nil || len(cfg.SSHHosts) == 0 {
							return nil
						}
						// Use the first (default) SSH host for fingerprint.
						h := cfg.SSHHosts[0]
						return &tool.StaticSSHHostConfig{
							Host:    h.Host,
							Port:    h.Port,
							User:    h.User,
							KeyPath: h.KeyPath,
						}
					}),
				}
			}
		}
	}
	if a.goalContinuation == nil {
		goalDataDir := filepath.Join(a.GetDataDir(), "goals")
		goalStore := goal.NewStore(goalDataDir)
		a.goalContinuation = NewGoalContinuationEngine(goalStore, a)
	}
	if a.sharedContext == nil {
		a.sharedContext = NewSharedContextStore()
	}
	if a.toolSelector == nil {
		a.toolSelector = NewToolSelector()
	}
	if a.configManager == nil {
		a.configManager = NewConfigManager(a)
	}
	// Register OEM extra tools into the built-in tool registry.
	{
		registry := make(map[string]bool, len(remoteToolCatalog))
		for name := range remoteToolCatalog {
			registry[name] = true
		}
		if err := brand.RegisterExtraTools(registry); err != nil {
			fmt.Printf("[initCoreInfra] WARNING: failed to register OEM extra tools: %v\n", err)
		}
	}
	log.Printf("[initCoreInfra] done in %v", time.Since(coreStart))
}

// ---------------------------------------------------------------------------
// Layer 1 -Interaction infrastructure: components needed when the user
// actually starts interacting (first IM message, first session launch, etc.).
// Deferred from startup to reduce idle memory.
// ---------------------------------------------------------------------------

func (a *App) ensureInteractionInfra() {
	a.ensureRemoteInfra()
	a.interactionInfraOnce.Do(func() {
		a.initInteractionInfra()
		a.interactionInfraDone.Store(true)
	})
}

func (a *App) interactionInfraReady() bool {
	return a.interactionInfraDone.Load()
}

func (a *App) markAIAssistantReady() {
	a.warmupDone.Store(true)
	a.aiAssistantReadyAt.Store(time.Now().UnixNano())
	a.aiAssistantFirstChatLogged.Store(false)
	// Best-effort: imHandler may already be wired with memoryStore.
	a.scheduleWarmFrozenMemorySnapshot()
}

// scheduleWarmFrozenMemorySnapshot precomputes the static system-prompt memory
// section for desktop chat so the first user message avoids a multi-second
// StaticMemorySectionForPrompt cost on the critical path.
func (a *App) scheduleWarmFrozenMemorySnapshot() {
	if a == nil {
		return
	}
	handler := a.imHandler
	if handler == nil && a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		handler = a.remoteSessions.hubClient.currentIMHandler()
	}
	if handler == nil {
		return
	}
	// Wire memory store if handler was created before ensureMemoryStore finished.
	if handler.memoryStore == nil && a.memoryStore != nil {
		handler.memoryStore = a.memoryStore
	}
	go handler.WarmFrozenMemorySnapshot(desktopUserID)
}

func (a *App) beginFirstAIAssistantChatTelemetry() (readyAt int64, shouldLog bool) {
	readyAt = a.aiAssistantReadyAt.Load()
	shouldLog = readyAt > 0 && a.aiAssistantFirstChatLogged.CompareAndSwap(false, true)
	return readyAt, shouldLog
}

func (a *App) initInteractionInfra() {
	t0 := time.Now()
	a.logMemorySnapshot("initInteractionInfra:start")

	// --- Critical path: components required for the first AI message ---
	if a.ioRelay == nil {
		a.ioRelay = NewSessionIORelay()
	}
	if a.aiTrace == nil {
		a.aiTrace = NewAITraceService()
	}

	log.Printf("[initInteractionInfra] critical path done in %v", time.Since(t0))
	a.logMemorySnapshot("initInteractionInfra:critical-done")

	// Non-critical interaction services now initialize on-demand via ensure* helpers.
}

// --- Fine-grained ensure helpers for Layer 2 components ---

func (a *App) ensureMemoryStore() {
	if a.memoryStore != nil {
		return
	}
	a.memoryStoreMu.Lock()
	if a.memoryStore != nil {
		a.memoryStoreMu.Unlock()
		return
	}
	a.ensureAITrace()
	a.logMemorySnapshot("ensureMemoryStore:start")
	baseDir := a.getMaclawBaseDir()
	memPath := filepath.Join(memory.DataDirStoreDir(baseDir), "memories.json")
	ms, err := memory.OpenDataDirStore(baseDir, memory.StoreModeSQLite)
	if err != nil {
		fmt.Printf("[ensureMemoryStore] WARNING: failed to load SQLite memory store from %s: %v\n", baseDir, err)
		backupPath := memPath + ".bad." + time.Now().Format("20060102_150405")
		_ = os.Rename(memPath, backupPath)
		fmt.Printf("[ensureMemoryStore] renamed problematic legacy file to %s, retrying JSON mode\n", backupPath)
		ms, err = memory.OpenDataDirStore(baseDir, memory.StoreModeJSON)
		if err != nil {
			fmt.Printf("[ensureMemoryStore] ERROR: memory store still failed after retry: %v\n", err)
		}
	}
	var compressorToConfigure *MemoryCompressor
	if ms != nil {
		a.memoryStore = ms
		a.ensureExperienceLifecycleSink()

		// Register ProjectIndex change callback. When any memory entry
		// updates the project index, notify the frontend and debounce memory
		// maintenance from the single ProjectIndex change point instead of
		// wiring per-writer events.
		if pi := ms.ProjectIndex(); pi != nil {
			pi.OnChanged = func(_ string) {
				if a.ctx != nil {
					a.emitEvent("task-list-changed")
				}
				a.triggerMemoryPipelineSoon(a.projectIndexMemoryDebounce())
			}
		}
		// Corelib owns the memory maintenance topology: compressor, pipeline,
		// synthesizer, TiMem consolidators, recall gating, and online extraction.
		maintenance := memory.NewMaintenance(ms, nil, guiMemoryEventEmitter{app: a})
		maintenance.InstallRuntime()
		a.memoryMaintenance = maintenance
		compressor := maintenance.Compressor()
		// Defer configureMemoryCompressor until after memoryStoreMu is released:
		// configure → LoadConfig → applyDataDirFromConfig may call
		// resetPathBoundStateForDataDirChange, which re-locks memoryStoreMu (deadlock).
		compressorToConfigure = compressor
		a.compressorMu.Lock()
		a.memoryCompressor = compressor
		a.compressorMu.Unlock()
		a.memPipeline = maintenance.Pipeline()
		a.triggerMemoryPipelineSoon(a.memoryPipelineStartupDelay())
		// refreshMemoryEvolutionLLM is deferred until after memoryStoreMu is
		// released, same as configureMemoryCompressor below: it goes through
		// LoadConfig, which may drain a pending data-dir reset that re-locks
		// memoryStoreMu (self-deadlock).
		// Static memory snapshot does not need embeddings — prewarm as soon as
		// the store is open so first chat message can reuse the cache.
		a.scheduleWarmFrozenMemorySnapshot()
		if a.disableBackgroundEmbeddingForTest {
			a.logMemorySnapshot("ensureMemoryStore:embedding-load-disabled-for-test")
			a.logMemorySnapshot("ensureMemoryStore:ready")
		} else {
			// Load embedding model asynchronously so it doesn't block the first
			// AI assistant message. Vector search will become available once
			// the model finishes loading in the background. Tool embedding
			// cache is also pre-warmed so the first routeTools() call is fast.
			go func() {
				cfg, err := a.LoadConfig()
				if err != nil {
					return
				}
				a.logMemorySnapshot("ensureMemoryStore:embedding-load")
				modelPath := embedding.DefaultModelPath()
				emb, err := a.sharedEmbeddingEmbedder(modelPath, func(path string) (embedding.Embedder, error) {
					return embedding.NewDefaultEmbedder(path), nil
				})
				if err != nil {
					log.Printf("[ensureMemoryStore] embedding model load failed: %v", err)
					return
				}
				if embedding.IsNoop(emb) {
					return // model not found, skip
				}
				if cfg.VectorSearchEnabled {
					a.activateEmbedderAsync(emb)
					log.Println("[ensureMemoryStore] embedding model loaded in background")
					return
				}
				a.activateIntentClassifierEmbedderAsync(emb)
				log.Println("[ensureMemoryStore] intent embedding model loaded in background")
			}()
			a.logMemorySnapshot("ensureMemoryStore:ready")
		}
	}
	// Unlock before configure: LoadConfig may re-enter path-bound state resets.
	a.memoryStoreMu.Unlock()
	if compressorToConfigure != nil {
		a.configureMemoryCompressor(compressorToConfigure)
		// Newly opened store: refresh the memory-evolution LLM outside the
		// store mutex (it loads config, see above).
		a.refreshMemoryEvolutionLLM()
	}
}

func (a *App) ensureMemoryPipeline() {
	a.ensureInteractionInfra()
	if a.memPipeline == nil {
		return
	}
	a.triggerMemoryPipelineSoon(a.projectIndexMemoryDebounce())
}

func (a *App) projectIndexMemoryDebounce() time.Duration {
	if a != nil && a.memoryPipelineDebounce > 0 {
		return a.memoryPipelineDebounce
	}
	return 10 * time.Minute
}

func (a *App) memoryPipelineStartupDelay() time.Duration {
	if a != nil && a.memoryPipelineDebounce > 0 {
		return a.memoryPipelineDebounce
	}
	return 10 * time.Minute
}

func (a *App) triggerMemoryPipelineSoon(delay time.Duration) {
	if a == nil || a.memPipeline == nil {
		return
	}
	if delay < 0 {
		delay = 0
	}
	if a.memoryPipelineDebounce <= 0 && delay < 10*time.Minute {
		delay = 10 * time.Minute
	}
	a.memoryPipelineScheduleMu.Lock()
	a.memoryPipelineScheduleSeq++
	seq := a.memoryPipelineScheduleSeq
	if a.memoryPipelineTimer != nil {
		a.memoryPipelineTimer.Stop()
	}
	a.memoryPipelineTimer = time.AfterFunc(delay, func() {
		a.runMemoryPipelineWhenIdle(seq)
	})
	a.memoryPipelineScheduleMu.Unlock()
	log.Printf("[agent-qos] schedule_memory_pipeline seq=%d delay=%s", seq, delay.Round(time.Millisecond))
}

func (a *App) runMemoryPipelineWhenIdle(seq uint64) {
	if a == nil || a.memPipeline == nil {
		return
	}
	if !a.isCurrentMemoryPipelineSchedule(seq) {
		log.Printf("[agent-qos] skip_stale_memory_pipeline seq=%d", seq)
		return
	}
	now := time.Now()
	active := a.activeForegroundAgentLoops()
	quietUntil := foregroundAgentQuietUntil("")
	if active > 0 || quietUntil.After(now) {
		retryDelay := 30 * time.Second
		if quietDelay := time.Until(quietUntil); quietDelay > retryDelay {
			retryDelay = quietDelay
		}
		log.Printf("[agent-qos] defer_memory_pipeline_until_idle seq=%d active=%d quiet_for=%s retry=%s", seq, active, time.Until(quietUntil).Round(time.Millisecond), retryDelay.Round(time.Millisecond))
		a.rescheduleMemoryPipelineRun(seq, retryDelay)
		return
	}
	if a.hasActiveMemoryPipelineRun() {
		retryDelay := 30 * time.Second
		log.Printf("[agent-qos] defer_memory_pipeline_run_active seq=%d retry=%s", seq, retryDelay.Round(time.Millisecond))
		a.rescheduleMemoryPipelineRun(seq, retryDelay)
		return
	}
	log.Printf("[agent-qos] run_memory_pipeline_idle seq=%d", seq)
	ctx, cancel := context.WithCancel(context.Background())
	a.memoryPipelineScheduleMu.Lock()
	if a.memoryPipelineScheduleSeq != seq {
		a.memoryPipelineScheduleMu.Unlock()
		cancel()
		log.Printf("[agent-qos] skip_stale_memory_pipeline_before_run seq=%d", seq)
		return
	}
	a.memoryPipelineRunCancel = cancel
	a.memoryPipelineRunSeq = seq
	a.memoryPipelineRunActive = true
	a.memoryPipelineScheduleMu.Unlock()
	runSucceeded := false
	defer func() {
		if r := recover(); r != nil {
			// Keep background memory work from killing the process; log stack for diagnosis.
			log.Printf("[agent-qos] memory_pipeline_panic seq=%d panic=%v\n%s", seq, r, string(debug.Stack()))
		}
		a.memoryPipelineScheduleMu.Lock()
		if a.memoryPipelineRunSeq == seq {
			a.memoryPipelineRunCancel = nil
			a.memoryPipelineRunSeq = 0
			a.memoryPipelineRunActive = false
		}
		a.memoryPipelineScheduleMu.Unlock()
		cancel()
		if !runSucceeded && a.isCurrentMemoryPipelineSchedule(seq) {
			a.triggerMemoryPipelineSoon(a.projectIndexMemoryDebounce())
		}
	}()
	a.memPipeline.RunOnce(ctx)
	runSucceeded = true
	if a.isCurrentMemoryPipelineSchedule(seq) {
		a.triggerMemoryPipelineSoon(6 * time.Hour)
	}
}

func (a *App) hasActiveMemoryPipelineRun() bool {
	if a == nil {
		return false
	}
	a.memoryPipelineScheduleMu.Lock()
	defer a.memoryPipelineScheduleMu.Unlock()
	return a.memoryPipelineRunActive
}

func (a *App) rescheduleMemoryPipelineRun(seq uint64, delay time.Duration) {
	if a == nil {
		return
	}
	if delay < 0 {
		delay = 0
	}
	a.memoryPipelineScheduleMu.Lock()
	if a.memoryPipelineScheduleSeq == seq {
		if a.memoryPipelineTimer != nil {
			a.memoryPipelineTimer.Stop()
		}
		a.memoryPipelineTimer = time.AfterFunc(delay, func() {
			a.runMemoryPipelineWhenIdle(seq)
		})
	}
	a.memoryPipelineScheduleMu.Unlock()
}

func (a *App) cancelMemoryPipelineRun(reason string) bool {
	if a == nil {
		return false
	}
	a.memoryPipelineScheduleMu.Lock()
	cancel := a.memoryPipelineRunCancel
	if cancel != nil {
		a.memoryPipelineRunCancel = nil
		a.memoryPipelineScheduleSeq++
	}
	a.memoryPipelineScheduleMu.Unlock()
	if cancel == nil {
		return false
	}
	log.Printf("[agent-qos] cancel_memory_pipeline_run reason=%s", strings.TrimSpace(reason))
	cancel()
	return true
}

func (a *App) stopMemoryPipelineSchedule(reason string) {
	if a == nil {
		return
	}
	a.memoryPipelineScheduleMu.Lock()
	cancel := a.memoryPipelineRunCancel
	a.memoryPipelineRunCancel = nil
	a.memoryPipelineRunSeq = 0
	a.memoryPipelineRunActive = false
	if a.memoryPipelineTimer != nil {
		a.memoryPipelineTimer.Stop()
		a.memoryPipelineTimer = nil
	}
	a.memoryPipelineScheduleSeq++
	seq := a.memoryPipelineScheduleSeq
	a.memoryPipelineScheduleMu.Unlock()
	if cancel != nil {
		log.Printf("[agent-qos] stop_memory_pipeline_schedule reason=%s seq=%d", strings.TrimSpace(reason), seq)
		cancel()
	}
}

func (a *App) isCurrentMemoryPipelineSchedule(seq uint64) bool {
	if a == nil {
		return false
	}
	a.memoryPipelineScheduleMu.Lock()
	defer a.memoryPipelineScheduleMu.Unlock()
	return a.memoryPipelineScheduleSeq == seq
}

func (a *App) ensureScheduledTaskManager() {
	a.scheduledTaskManagerMu.Lock()
	defer a.scheduledTaskManagerMu.Unlock()
	if a.scheduledTaskManager != nil {
		return
	}
	a.ensureRemoteInfra()
	baseDir := a.getMaclawBaseDir()
	stm, err := scheduler.NewManager(filepath.Join(baseDir, "scheduled_tasks.json"))
	if err == nil {
		a.scheduledTaskManager = stm
		// Notify frontend when task state changes (fire/expire/create/delete).
		a.scheduledTaskManager.SetOnChange(func() {
			a.emitEvent("scheduled-tasks-changed")
		})
		// Start the scheduler immediately with a local executor that doesn't
		// depend on Hub connectivity. This ensures scheduled tasks fire even
		// for desktop-only users who never connect to Hub.
		// If Hub connects later, createAndWireHubClient() upgrades the executor
		// to a Hub-aware version that also pushes results to IM channels.
		a.scheduledTaskManager.StartWithExecutor(a.buildLocalScheduledTaskExecutor())
	} else {
		fmt.Printf("[ensureScheduledTaskManager] WARNING: failed to init: %v\n", err)
	}
}

// scheduledTaskManagerForIMHandler initializes and returns the App-wide
// scheduler under the same lock that protects its lazy creation. Local gateway
// handlers can arrive concurrently during startup, so they must not race a
// first-time assignment while attaching to the shared scheduler.
func (a *App) scheduledTaskManagerForIMHandler() *scheduler.Manager {
	if a == nil {
		return nil
	}
	a.ensureScheduledTaskManager()
	a.scheduledTaskManagerMu.Lock()
	defer a.scheduledTaskManagerMu.Unlock()
	return a.scheduledTaskManager
}

func (a *App) ensureMCPAutoDiscovery() {
	if a.mcpAutoDiscovery != nil {
		return
	}
	a.ensureRemoteInfra()
	if a.mcpRegistry == nil {
		return
	}
	a.mcpAutoDiscovery = NewMCPAutoDiscovery(a, nil, a.mcpRegistry)
	if err := a.mcpAutoDiscovery.ScanGlobal(); err != nil {
		fmt.Printf("[ensureMCPAutoDiscovery] WARNING: global MCP scan failed: %v\n", err)
	}
	if cfg, err := a.LoadConfig(); err == nil {
		for _, p := range cfg.Projects {
			if p.Path != "" {
				_ = a.mcpAutoDiscovery.ScanProject(p.Path)
				_ = a.mcpAutoDiscovery.WatchProject(p.Path)
			}
		}
	}
}

func (a *App) ensureTemplateManager() {
	if a.templateManager != nil {
		return
	}
	baseDir := a.getMaclawBaseDir()
	tm, err := NewSessionTemplateManager(filepath.Join(baseDir, "templates.json"))
	if err == nil {
		a.templateManager = tm
	}
}

func (a *App) ensureMDNSScanner() {
	if a.mdnsScanner != nil {
		return
	}
	a.ensureRemoteInfra()
	if a.mcpRegistry != nil {
		a.mdnsScanner = NewMDNSScanner(a.mcpRegistry)
	}
	if a.projectScanner == nil && a.mcpRegistry != nil {
		a.projectScanner = NewProjectScanner(a.mcpRegistry)
	}
}

func (a *App) ensureSkillMarketClient() {
	if a.skillMarketClient != nil {
		return
	}
	a.skillMarketClient = NewSkillMarketClient(a)
}

func (a *App) ensureSkillLifecycleManager() {
	if a.skillLifecycle != nil {
		a.skillLifecycle.StartBackgroundProcessing(context.Background(), skillUploadQueueProcessInterval)
		return
	}
	a.skillLifecycle = NewSkillLifecycleManager(a)
	a.skillLifecycle.StartBackgroundProcessing(context.Background(), skillUploadQueueProcessInterval)
}

func (a *App) recoverSkillUploadQueueAfterStartup() {
	probe := NewSkillLifecycleManager(a)
	runnable, err := probe.HasRunnableUploadItems(time.Now())
	if err != nil {
		log.Printf("[skill-lifecycle] startup queue probe failed: %v", err)
		return
	}
	if !runnable {
		return
	}
	a.ensureSkillLifecycleManager()
}

func (a *App) ensureSkillRunner() {
	a.ensureInteractionInfra()
	if a.skillRunner != nil || a.skillExecutor == nil {
		return
	}
	a.skillRunner = NewSkillRunner(a.skillExecutor)
	a.skillRunner.packageFn = a.packageSkillForMarket

	// Wire evolution pipeline (async background self-evolution + self-repair).
	a.ensureEvolutionPipeline()
	a.skillRunner.evolutionPipeline = a.evolutionPipeline
	// Route self-repair through the pipeline so it shares delay/coalesce/throttle
	// with optimize/promote, and so SkillRunner does not spawn a parallel repair.
	if a.evolutionPipeline != nil && a.skillRunner != nil {
		runner := a.skillRunner
		a.evolutionPipeline.EnableRepair = true
		a.evolutionPipeline.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
			// entry is already a deep copy from the pipeline.
			if entry == nil || runner == nil {
				return
			}
			// Re-check eligibility with latest repairer availability.
			if !runner.canStartRepairSkill(entry) {
				runner.repairingSkills.Delete(entry.Name)
				return
			}
			// Ensure pending marker is set (may already be set by updateUsageStats).
			runner.markSelfRepairPending(entry.Name)
			// Runs on the evolution worker goroutine — do not spawn another.
			runner.maybeRepairSkill(entry)
		}
	}
}

func (a *App) ensureEvolutionPipeline() {
	if a.evolutionPipeline != nil {
		return
	}
	pipeline := skill.NewEvolutionPipeline()
	pipeline.UsageTracker = a.usageTracker
	pipeline.Versioner = &skill.Versioner{}
	if cfg, err := a.LoadConfig(); err == nil {
		pipeline.RepairCooldown = skill.RepairCooldownFromHours(cfg.SkillEvolutionRepairCooldownHours)
	}
	// RepairGate: sandbox-replay historical args before accepting repairs/optimizations.
	// Bash-only steps run via TempDirSandboxExecutor; non-bash steps soft-pass so
	// craft_tool-only skills are not false-rejected.
	pipeline.Gate = skill.NewRepairGate(skill.RepairGateConfig{}, skill.NewDefaultSandboxExecutor())
	pipeline.SkillLoader = func() []corelib.NLSkillEntry {
		if a.skillExecutor == nil {
			return nil
		}
		// loadSkills is self-synchronized (skillCacheMu + LoadConfig); avoid e.mu.
		return a.skillExecutor.loadSkills()
	}
	pipeline.SkillSaver = func(skills []corelib.NLSkillEntry) error {
		if a.skillExecutor == nil {
			return nil
		}
		// Serialize skill-table writes; saveSkills takes configMu via PatchConfig.
		return a.skillExecutor.withSkillListMutate(func() error {
			return a.skillExecutor.saveSkills(skills)
		})
	}
	// Wire LLM for optimization and promotion (lazy config  - picks up provider changes).
	pipeline.LLM = NewSkillEvolutionLLMAdapter(a.GetMaclawLLMConfig)
	pipeline.Optimizer = skill.NewSkillOptimizer(pipeline.LLM, pipeline.Gate, pipeline.Versioner)

	// Wire NudgePromoter: converts high-confidence tool-sequence patterns into real skills.
	// Auto-promotion uses the critical-only staging validator: candidates already
	// succeeded as local tool sequences, so community "high" (e.g. bash) is expected.
	if skillsDir, err := skill.PrimarySkillsDir(); err == nil {
		pipeline.Promoter = skill.NewNudgePromoter(
			pipeline.LLM,
			skill.NewAutoPromotionStagingValidator(),
			&skillExecutorRegistrar{app: a},
			skillsDir,
		)
	}

	// Wire EventEmitter: durable audit + frontend notify when skills evolve.
	pipeline.EventEmitter = func(event string, data map[string]string) {
		skill.RecordEvolutionEvent(event, data, "desktop")
		a.emitEvent(event, data)
	}

	// Wire UploadTrigger: after successful optimization/promotion, enqueue
	// upload through SkillLifecycleManager (which applies portability gate,
	// runtime proof, package completeness, and quality checks before submission).
	//
	// NOTE: We pre-ensure the lifecycle manager here (on the main goroutine)
	// rather than inside the closure, because ensureSkillLifecycleManager is
	// not goroutine-safe (no internal mutex).
	a.ensureSkillLifecycleManager()
	pipeline.UploadTrigger = func(skillName string, _ *skill.SkillExecutionResultCompat) {
		if cfg, err := a.LoadConfig(); err != nil || !cfg.IsSkillAutoUploadEnabled() {
			return
		}
		if a.skillLifecycle == nil {
			return
		}
		// Find skill dir for the enqueue call.
		var skillDir string
		if a.skillExecutor != nil {
			for _, s := range a.skillExecutor.loadSkills() {
				if s.Name == skillName {
					skillDir = s.SkillDir
					break
				}
			}
		}
		if _, err := a.skillLifecycle.EnqueueUpload(
			context.Background(),
			skillName,
			skillDir,
			"skill_evolution_auto",
			true, // requireRuntimeProof  - must have at least one successful run
			true, // processNow  - try to upload immediately
		); err != nil {
			log.Printf("[evolution-pipeline] upload enqueue failed for %s: %v", skillName, err)
		}
	}

	a.evolutionPipeline = pipeline

	// Wire MaintenanceScheduler: runs BuildSkillMaintenancePlan every 24h,
	// persists high-value results to long-term memory / experience learning.
	pipeline.Scheduler = skill.NewMaintenanceScheduler(
		pipeline.SkillLoader,
		func(content string, tags []string) error {
			if a.memoryStore == nil {
				return nil
			}
			_, err := a.memoryStore.UpsertTaskArtifact(memory.TaskArtifactUpsertOptions{
				Title:            "Skill maintenance plan",
				Content:          content,
				Tags:             tags,
				IdentityTagCount: 2, // ["skill_maintenance", "auto_scheduled"] form stable identity
				SourceType:       "skill_maintenance",
			})
			return err
		},
	)
	if a.usageTracker != nil {
		tracker := a.usageTracker
		pipeline.Scheduler.UsageIngester = func(skills []corelib.NLSkillEntry, opts skill.SkillMaintenancePlanOptions) int {
			return skill.IngestHighValueMaintenanceExperience(tracker, skills, opts)
		}
	}

	pipeline.Start()
}

func (a *App) ensureLLMSecurityReview() {
	a.ensureInteractionInfra()
	if a.llmSecurityReview != nil {
		return
	}
	cfg := a.GetMaclawLLMConfig()
	a.llmSecurityReview = NewLLMSecurityReview(cfg)
}

// refreshMemoryEvolutionLLM wires the current MaClaw LLM caller into every
// memory component that can evolve or filter long-term memory. This is kept
// independent from embedding activation: text-only memory extraction and
// reflection should work even when vector search is disabled or unavailable.
func (a *App) refreshMemoryEvolutionLLM() {
	if a.memoryStore == nil || !a.isMaclawLLMConfigured() {
		return
	}
	if a.memoryMaintenance == nil {
		a.memoryMaintenance = memory.NewMaintenance(a.memoryStore, nil, guiMemoryEventEmitter{app: a})
		a.memoryMaintenance.InstallRuntime()
		if a.memPipeline == nil {
			a.memPipeline = a.memoryMaintenance.Pipeline()
		}
		if compressor := a.memoryMaintenance.Compressor(); compressor != nil {
			a.configureMemoryCompressor(compressor)
			var oldCompressor *MemoryCompressor
			a.compressorMu.Lock()
			if a.memoryCompressor != compressor {
				oldCompressor = a.memoryCompressor
				a.memoryCompressor = compressor
			}
			a.compressorMu.Unlock()
			if oldCompressor != nil {
				oldCompressor.Stop()
			}
		}
	}
	a.memoryMaintenance.SetLLM(&archiverLLMCaller{app: a})
	a.triggerMemoryPipelineSoon(a.projectIndexMemoryDebounce())
}

func (a *App) ensureExperienceExtractor() {
	a.ensureInteractionInfra()
	if a.experienceExtractor != nil {
		return
	}
	cfg := a.GetMaclawLLMConfig()
	a.experienceExtractor = NewExperienceExtractor(a, a.skillExecutor, cfg)
}

func (a *App) ensureSkillHubClient() {
	a.ensureRemoteInfra()
	if a.skillHubClient != nil {
		return
	}
	a.ensureInteractionInfra()
	a.skillHubClient = NewSkillHubClient(a)
	// Async: don't block initialization on HubCenter reachability.
	go func(client *SkillHubClient) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = client.RefreshRecommendations(ctx)
	}(a.skillHubClient)
	if a.stopHubTicker == nil {
		a.stopHubTicker = make(chan struct{})
		go func(stop <-chan struct{}, client *SkillHubClient) {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_ = client.RefreshRecommendations(context.Background())
				case <-stop:
					return
				}
			}
		}(a.stopHubTicker, a.skillHubClient)
	}
	if a.toolRouter != nil {
		a.toolRouter.SetHubClient(a.skillHubClient)
	}
}

func (a *App) ensureCapabilityGapDetector() {
	a.ensureInteractionInfra()
	if a.capabilityGapDetector != nil {
		return
	}
	a.ensureSkillHubClient()
	cfg := a.GetMaclawLLMConfig()
	a.capabilityGapDetector = NewCapabilityGapDetector(
		a, a.skillHubClient, a.skillExecutor, a.riskAssessor, a.auditLog, cfg,
	)
}

func (a *App) ensureGossipClient() {
	if a.gossipClient != nil {
		return
	}
	a.ensureInteractionInfra()
	a.gossipClient = NewGossipClient(a)
}

func (a *App) ensureGossipAutoPublish() {
	a.ensureGossipClient()
	if a.gossipAutoPublish != nil || a.gossipClient == nil {
		return
	}
	a.gossipAutoPublish = NewAutoPublishTrigger(a.gossipClient, func() bool {
		if cfg, err := a.LoadConfig(); err == nil {
			return cfg.GossipAutoPublish
		}
		return true
	})
	a.gossipAutoPublish.SetLLMConfigFn(func() corelib.MaclawLLMConfig {
		return a.GetMaclawLLMConfig()
	})
	a.gossipAutoPublish.SetGossipAllowedFn(func() bool {
		return a.isGossipAllowed()
	})
}

func (a *App) ensureContextBridge() {
	a.ensureInteractionInfra()
	if a.contextBridge != nil {
		return
	}
	a.contextBridge = NewContextBridge()
}

func (a *App) buildTrajectoryRecorderFactory() func() *TrajectoryRecorder {
	// Return a factory that checks the config dynamically on each call,
	// so toggling the setting at runtime takes effect immediately.
	return func() *TrajectoryRecorder {
		cfg, err := a.LoadConfig()
		if err != nil || !cfg.LLMTrajectoryLogging {
			return nil
		}
		recorder := NewTrajectoryRecorderForBaseDir(a.getMaclawBaseDir())
		recorder.SetPipeline(a.buildSkillAutoSummaryPipeline())
		return recorder
	}
}

func (a *App) buildSkillAutoSummaryPipeline() *SkillAutoSummaryPipeline {
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		return nil
	}
	a.ensureSkillLifecycleManager()
	// Auto-summary runs after the user already executed these tools in-session.
	// DefaultSkillSecurityPolicy uses Ask for shell/network, but background
	// pipelines cannot surface a UI prompt — the old askFn always returned false
	// and dropped every learned skill that touched shell/network. Allow here;
	// execution-time tool guards still enforce policy when the skill is run.
	checker := NewSecurityPolicyChecker(SkillSecurityPolicy{
		NetworkAccess:    SecurityAllow,
		FileSystemAccess: SecurityAllow,
		ShellExec:        SecurityAllow,
		DatabaseAccess:   SecurityAllow,
	}, nil)
	return NewSkillAutoSummaryPipeline(
		NewTagGenerator(),
		checker,
		a.skillExecutor,
		nil,
	).WithNamingLLM(NewSkillEvolutionLLMAdapter(a.GetMaclawLLMConfig))
}

func (a *App) ensureAITrace() {
	if a.aiTrace != nil {
		return
	}
	a.aiTrace = NewAITraceService()
}

func (a *App) ensureSecurityRiskAnalyzer() {
	a.ensureInteractionInfra()
	if a.securityRiskAnalyzer != nil {
		return
	}
	a.securityRiskAnalyzer = NewSecurityRiskAnalyzer()
}

func (a *App) ensureSecurityFirewall() {
	a.ensureInteractionInfra()
	if a.securityFirewall != nil {
		return
	}
	a.ensureSecurityRiskAnalyzer()
	a.ensureAuditLog()
	if a.policyEngine != nil && a.securityRiskAnalyzer != nil && a.auditLog != nil {
		a.securityFirewall = NewSecurityFirewall(a.securityRiskAnalyzer, a.policyEngine, a.auditLog)
	}
}

func (a *App) ensureLocalMCPManager() {
	a.ensureInteractionInfra()
	if a.localMCPManager != nil {
		return
	}
	a.localMCPManager = NewLocalMCPManager(a.mcpRegistry)
	if a.toolDefGenerator != nil {
		a.toolDefGenerator.SetLocalMCPManager(a.localMCPManager)
	}
}

func (a *App) autoStartLocalMCPServers(entries []corelib.LocalMCPServerEntry) {
	for _, server := range entries {
		if !server.Disabled && server.AutoStart {
			a.ensureLocalMCPManager()
			if a.localMCPManager != nil {
				go a.localMCPManager.SyncFromConfig()
			}
			return
		}
	}
}

func (a *App) ensureOrchestrator() {
	a.ensureInteractionInfra()
	if a.orchestrator == nil && a.remoteSessions != nil && a.sharedContext != nil && a.toolSelector != nil {
		a.orchestrator = NewOrchestrator(a, a.remoteSessions, a.sharedContext, a.toolSelector)
	}
}

func (a *App) ensureTaskOrchestrator2() {
	a.ensureInteractionInfra()
	if a.taskOrchestrator2 == nil && a.remoteSessions != nil && a.toolSelector != nil {
		a.ensureContextBridge()
		a.taskOrchestrator2 = NewTaskOrchestrator2(a.remoteSessions, a.toolSelector, a.contextBridge)
	}
}

func (a *App) ensureContextResolver() {
	a.ensureInteractionInfra()
	if a.contextResolver == nil {
		a.contextResolver = NewSessionContextResolver(a)
	}
}

func (a *App) ensureSessionPrecheck() {
	a.ensureInteractionInfra()
	if a.sessionPrecheck == nil {
		a.sessionPrecheck = NewSessionPrecheck(a)
	}
}

func (a *App) ensureConversationArchiver() {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	a.ensureMemoryPipeline()
	if a.conversationArchiver == nil && a.memoryStore != nil {
		a.conversationArchiver = NewConversationArchiver(a.memoryStore, a)
	}
	if a.conversationArchiver != nil {
		a.conversationArchiver.SetSlotScopeResolver(func(userID string) *agent.UnfinishedTaskSlot {
			mem := a.ensureConversationMemory()
			if mem == nil {
				return nil
			}
			return mem.ActiveUnfinishedSlot(userID)
		})
	}
}

func (a *App) ensureSessionCheckpointer() {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	a.ensureContextBridge()
	if a.sessionCheckpointer == nil && a.memoryStore != nil {
		a.sessionCheckpointer = NewSessionCheckpointer(a.memoryStore, a.contextBridge)
	}
}

func (a *App) ensureStartupFeedback() {
	a.ensureInteractionInfra()
	if a.startupFeedback == nil && a.remoteSessions != nil {
		a.startupFeedback = NewSessionStartupFeedback(a.remoteSessions)
	}
	a.ensureSessionCheckpointer()
	if a.startupFeedback == nil || a.sessionCheckpointer == nil {
		return
	}
	a.startupFeedback.SetCheckpointer(a.sessionCheckpointer)
	a.startupFeedback.SetUnfinishedSlotResolver(func(projectPath string) *agent.UnfinishedTaskSlot {
		mem := a.ensureConversationMemory()
		if mem == nil {
			return nil
		}
		slot := mem.ActiveUnfinishedSlot(desktopUserID)
		if slot == nil {
			return nil
		}
		if strings.TrimSpace(slot.ProjectPath) != strings.TrimSpace(projectPath) {
			return nil
		}
		return slot
	})
}

func (a *App) ensureConversationMemory() *agent.ConversationMemory {
	// Do not hold the memory lock while bootstrapping remote infrastructure:
	// that bootstrap can construct handlers which also need this shared memory.
	if strings.TrimSpace(a.testHomeDir) == "" {
		a.ensureRemoteInfra()
	}
	a.conversationMemoryMu.Lock()
	defer a.conversationMemoryMu.Unlock()
	if a.aiConversationMemory != nil {
		return a.aiConversationMemory
	}
	if strings.TrimSpace(a.testHomeDir) != "" {
		storePath := filepath.Join(a.GetDataDir(), "ai_assistant_conversation.json")
		if _, err := os.Stat(storePath); err == nil {
			log.Printf("[paths] ai_conversation_memory mode=persistent path=%q", storePath)
			a.aiConversationMemory = agent.NewPersistentConversationMemory(storePath)
		} else {
			log.Printf("[paths] ai_conversation_memory mode=memory test_home=%q missing_path=%q", a.testHomeDir, storePath)
			a.aiConversationMemory = agent.NewConversationMemory()
		}
		return a.aiConversationMemory
	}
	storePath := filepath.Join(a.GetDataDir(), "ai_assistant_conversation.json")
	log.Printf("[paths] ai_conversation_memory mode=persistent path=%q", storePath)
	a.aiConversationMemory = agent.NewPersistentConversationMemory(storePath)
	return a.aiConversationMemory
}

// currentConversationMemory returns the currently published shared memory
// without creating one. UI/read-only paths use it so a data-directory reset
// cannot race with an unsynchronised pointer read.
func (a *App) currentConversationMemory() *agent.ConversationMemory {
	if a == nil {
		return nil
	}
	a.conversationMemoryMu.Lock()
	defer a.conversationMemoryMu.Unlock()
	return a.aiConversationMemory
}

func (a *App) ensureAIConfirmationStore() *aiConfirmationStore {
	a.ensureRemoteInfra()
	a.confirmationStoreMu.Lock()
	defer a.confirmationStoreMu.Unlock()
	if a.aiConfirmationStore == nil {
		storePath := filepath.Join(a.GetDataDir(), "ai_assistant_confirmation.json")
		log.Printf("[paths] ai_confirmation_store path=%q", storePath)
		a.aiConfirmationStore = newAIConfirmationStore(storePath)
	}
	return a.aiConfirmationStore
}

func (a *App) currentAIConfirmationStore() *aiConfirmationStore {
	if a == nil {
		return nil
	}
	a.confirmationStoreMu.Lock()
	defer a.confirmationStoreMu.Unlock()
	return a.aiConfirmationStore
}

func (a *App) ensureDocGenerator() {
	if a.docGenerator != nil {
		return
	}
	a.docGenerator = swarm.NewSwarmDocGenerator()
}

func (a *App) ensureAuditLog() {
	if a.auditLog != nil {
		return
	}
	dir := filepath.Join(a.GetDataDir(), "audit")
	al, err := NewAuditLog(dir)
	if err == nil {
		a.auditLog = al
	}
}

// createAndWireHubClient creates a new RemoteHubClient, wires all subsystem
// handlers into it, and connects. This consolidates the repeated hub-client
// setup code that was duplicated in startup() and LaunchTool().
func (a *App) createAndWireHubClient() *RemoteHubClient {
	cwStart := time.Now()
	a.logMemorySnapshot("createAndWireHubClient:start")
	a.ensureInteractionInfra()
	log.Printf("[createAndWireHubClient] ensureInteractionInfra done in %v", time.Since(cwStart))
	hubClient := NewRemoteHubClient(a, a.remoteSessions)
	a.remoteSessions.SetHubClient(hubClient)
	hubClient.configureIMHandler = func(handler *IMMessageHandler) {
		if a.capabilityGapDetector == nil {
			a.ensureCapabilityGapDetector()
		}
		if a.capabilityGapDetector != nil {
			handler.SetCapabilityGapDetector(a.capabilityGapDetector)
		}
		if a.toolDefGenerator != nil {
			a.ensureLocalMCPManager()
			handler.SetToolDefGenerator(a.toolDefGenerator)
		}
		if a.toolRouter != nil {
			handler.SetToolRouter(a.toolRouter)
		}
		if a.usageTracker != nil {
			handler.SetUsageTracker(a.usageTracker)
		}
		if a.memoryStore == nil {
			a.ensureMemoryStore()
		}
		if a.memoryStore != nil {
			handler.SetMemoryStore(a.memoryStore)
		}
		if store := a.ensureAIConfirmationStore(); store != nil {
			handler.SetConfirmationStore(store)
		}
		a.ensureAITrace()
		if a.aiTrace != nil {
			handler.SetTraceService(a.aiTrace)
		}
		handler.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
		if a.configManager != nil {
			handler.SetConfigManager(a.configManager)
		}
		if a.templateManager != nil {
			handler.SetTemplateManager(a.templateManager)
		}
		if a.scheduledTaskManager != nil {
			handler.SetScheduledTaskManager(a.scheduledTaskManager)
		}
		if a.contextResolver == nil {
			a.ensureContextResolver()
		}
		if a.contextResolver != nil {
			handler.SetContextResolver(a.contextResolver)
		}
		if a.sessionPrecheck == nil {
			a.ensureSessionPrecheck()
		}
		if a.sessionPrecheck != nil {
			handler.SetSessionPrecheck(a.sessionPrecheck)
		}
		a.ensureStartupFeedback()
		if a.startupFeedback != nil {
			handler.SetStartupFeedback(a.startupFeedback)
		}
		if a.securityFirewall == nil {
			a.ensureSecurityFirewall()
		}
		if a.securityFirewall != nil {
			handler.SetSecurityFirewall(a.securityFirewall)
		}
		// Wire IM file sender so the desktop AI assistant can forward files to
		// the user's Feishu/WeChat. Prefer local Weixin gateway; fall back to Hub.
		handler.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
			return a.forwardDesktopFileToIMRequest(hubClient, req)
		})
		// Initialize and wire BackgroundLoopManager + SessionMonitor.
		statusC := make(chan StatusEvent, 32)
		blm := NewBackgroundLoopManager(statusC)
		// Emit Wails event when background loop state changes.
		blm.OnChange = func() {
			if a.ctx != nil {
				a.emitEvent("background-loops-changed")
			}
		}
		handler.SetBackgroundLoopManager(blm)
		// Register GUI automation tools with async background replay support.
		registerGUIAutomationTools(handler.registry, blm, handler.agentActivity, statusC)
		// Text-primary Computer Use (OmniParser + OCR SoM for non-vision models).
		bindComputerUseYOLOGate(a)
		registerComputerUseTools(handler.registry)
		// Keep group discussion available after late tool registration rebuilds.
		registerGroupDiscussionTools(handler.registry, a, handler)
		// Rebuild the tool builder so it picks up the newly registered GUI tools.
		handler.toolBuilder = NewDynamicToolBuilder(handler.registry)
		// A rebuilt builder starts without its hybrid retriever. Restore it only
		// when full vector search is active; intent-only embedding must remain
		// limited to routing and interruption relevance.
		if a.embeddingActivated.Load() {
			if emb := a.activeInterruptEmbedder(); emb != nil {
				handler.toolBuilder.SetEmbedder(emb)
			}
		}
		// Re-sync the router's registry snapshot so late-registered tools
		// (computer_* and group discussion) get Priority/Tags scoring.
		handler.SetToolRouter(a.toolRouter)
		// Wire skill-aware routing to the tool builder.
		if a.skillExecutor != nil {
			handler.toolBuilder.SetSkillProvider(&skillExecutorProvider{executor: a.skillExecutor})
		}
		// Keep interruption relevance on the intent runtime even when the user
		// has disabled vector search. Tool vector retrieval was restored above
		// only when full activation is active, so do not infer that policy from
		// memoryStore's embedder here.
		if emb := a.activeInterruptEmbedder(); emb != nil && handler.interruptHandler != nil {
			handler.interruptHandler.SetEmbedder(emb)
		}
		// Wire the statusC into the chat loop's LoopContext so it can drain
		// background events. This is done lazily: the chat LoopContext gets
		// statusC assigned in HandleIMMessageWithProgress before runAgentLoop.

		sm := NewSessionMonitor(a.remoteSessions, statusC, 20*time.Second)
		handler.SetSessionMonitor(sm)

		a.ensureConversationArchiver()
		if a.conversationArchiver != nil {
			handler.memory.Archiver = a.conversationArchiver
		}
	}
	if a.ioRelay != nil {
		hubClient.SetIORelay(a.ioRelay)
	}
	// Wire the scheduled task executor so that due tasks are sent to the
	// agent loop via the IM handler, making scheduled tasks actually fire.
	// This upgrades the local executor (set in ensureScheduledTaskManager)
	// to a Hub-aware version that also pushes results to IM channels.
	if a.scheduledTaskManager != nil {
		a.scheduledTaskManager.SetExecutor(a.buildHubScheduledTaskExecutor(hubClient))
		a.scheduledTaskManager.SetOnChange(func() {
			a.emitEvent("scheduled-tasks-changed")
		})
	}
	// Pre-initialize the IM handler before exposing AI assistant readiness so the
	// first chat request does not pay handler construction/configuration cost.
	preInitStart := time.Now()
	hubClient.ensureIMHandler()
	log.Printf("[createAndWireHubClient] ensureIMHandler (pre-init) done in %v", time.Since(preInitStart))
	connectStart := time.Now()
	_ = hubClient.Connect()
	log.Printf("[createAndWireHubClient] Hub Connect done in %v", time.Since(connectStart))
	a.logMemorySnapshot("createAndWireHubClient:connected")
	a.markAIAssistantReady()
	log.Printf("[createAndWireHubClient] total=%v -AI assistant now ready", time.Since(cwStart))
	a.emitEvent("ai-assistant-init-progress", "ready")
	// Do not eagerly warm tools or HTTP connections after Hub login/connect.
	// That extra preload is not required for correctness and causes a large
	// idle memory jump right after first login.

	// Start configured client-side IM gateways.
	a.syncIMGatewaysFromConfig()

	return hubClient
}

// prepareHubClientSync performs the synchronous (fast, no network I/O) portion of
// Hub client setup: creates the client, configures the IM handler, and wires all
// subsystems. This is safe to call on the startup critical path because it only
// does in-memory initialization and local file reads.
func (a *App) prepareHubClientSync() {
	a.hubClientEnsureMu.Lock()
	defer a.hubClientEnsureMu.Unlock()
	if a.remoteSessions != nil {
		if existing := a.remoteSessions.GetHubClient(); existing != nil {
			log.Printf("[prepareHubClientSync] hubClient already prepared; skipping")
			return
		}
	}
	cwStart := time.Now()
	a.logMemorySnapshot("prepareHubClientSync:start")
	a.ensureInteractionInfra()
	log.Printf("[prepareHubClientSync] ensureInteractionInfra done in %v", time.Since(cwStart))
	hubClient := NewRemoteHubClient(a, a.remoteSessions)
	a.remoteSessions.SetHubClient(hubClient)
	hubClient.configureIMHandler = a.buildHubClientIMHandlerConfigurator(hubClient)
	if a.ioRelay != nil {
		hubClient.SetIORelay(a.ioRelay)
	}
	// Wire the scheduled task executor so that due tasks are sent to the
	// agent loop via the IM handler, making scheduled tasks actually fire.
	if a.scheduledTaskManager != nil {
		a.scheduledTaskManager.SetExecutor(a.buildHubScheduledTaskExecutor(hubClient))
		a.scheduledTaskManager.SetOnChange(func() {
			a.emitEvent("scheduled-tasks-changed")
		})
	}
	// Pre-initialize the IM handler so the first chat request does not pay
	// handler construction/configuration cost.
	preInitStart := time.Now()
	hubClient.ensureIMHandler()
	log.Printf("[prepareHubClientSync] ensureIMHandler (pre-init) done in %v", time.Since(preInitStart))
	a.logMemorySnapshot("prepareHubClientSync:done")
	log.Printf("[prepareHubClientSync] total=%v", time.Since(cwStart))
}

// asyncHubConnect performs Hub WebSocket connection, authentication, and post-connect
// setup in a background goroutine. This is the network I/O portion that was previously
// blocking startup(). After successful auth, it emits the "ready" event and starts
// IM gateways. sendMachineHelloLocked() runs in a separate goroutine so it doesn't
// delay the "ready" signal. On failure, the system operates in degraded mode (local
// features work).
func (a *App) asyncHubConnect() {
	connectStart := time.Now()
	log.Printf("[asyncHubConnect] starting Hub connection in background")

	hubClient := a.remoteSessions.GetHubClient()
	if hubClient == nil {
		log.Printf("[asyncHubConnect] no hub client available, skipping")
		return
	}

	// ConnectAuthOnly performs WebSocket dial + sendMachineAuth + read auth response
	// without calling sendMachineHelloLocked(). This allows us to emit "ready"
	// immediately after auth succeeds.
	err := hubClient.ConnectAuthOnly()
	if err != nil {
		log.Printf("[asyncHubConnect] Hub auth failed in %v: %v -operating in degraded mode", time.Since(connectStart), err)
		// Degraded mode: local LLM, local tools still work. Only Hub-dependent
		// features (IM relay, remote session sync, scheduled task push to IM
		// channels) are unavailable until reconnection succeeds.
		a.emitEvent("ai-assistant-init-progress", "degraded")
		return
	}

	log.Printf("[asyncHubConnect] Hub auth succeeded in %v", time.Since(connectStart))
	a.logMemorySnapshot("asyncHubConnect:connected")
	a.emitEvent("ai-assistant-init-progress", "ready")

	// sendMachineHelloLocked() runs in a separate goroutine so its network
	// latency (tool version checks, skill list sync) doesn't delay UI
	// interactivity or the "ready" signal.
	go func() {
		helloStart := time.Now()
		hubClient.mu.Lock()
		// Verify connection is still alive before sending hello.
		// Between auth completion and acquiring the lock, the connection
		// may have been closed by a concurrent disconnect/reconnect.
		if hubClient.conn == nil || !hubClient.connected {
			hubClient.mu.Unlock()
			log.Printf("[asyncHubConnect] sendMachineHelloLocked skipped: connection lost before hello")
			return
		}
		err := hubClient.sendMachineHelloLocked()
		hubClient.mu.Unlock()
		if err != nil {
			log.Printf("[asyncHubConnect] sendMachineHelloLocked failed in %v: %v", time.Since(helloStart), err)
			// Hello failure is non-fatal -connection is already established,
			// auth succeeded, and local features work. Hub will eventually
			// receive hello on next heartbeat or reconnect.
			return
		}
		log.Printf("[asyncHubConnect] sendMachineHelloLocked done in %v", time.Since(helloStart))
	}()

	// Start IM gateways after successful Hub connection.
	a.syncIMGatewaysFromConfig()
}

// buildHubClientIMHandlerConfigurator returns the configureIMHandler closure used by
// both createAndWireHubClient (legacy path) and prepareHubClientSync (new non-blocking path).
func (a *App) buildHubClientIMHandlerConfigurator(hubClient *RemoteHubClient) func(handler *IMMessageHandler) {
	return func(handler *IMMessageHandler) {
		if a.capabilityGapDetector == nil {
			a.ensureCapabilityGapDetector()
		}
		if a.capabilityGapDetector != nil {
			handler.SetCapabilityGapDetector(a.capabilityGapDetector)
		}
		if a.toolDefGenerator != nil {
			a.ensureLocalMCPManager()
			handler.SetToolDefGenerator(a.toolDefGenerator)
		}
		if a.toolRouter != nil {
			handler.SetToolRouter(a.toolRouter)
		}
		if a.usageTracker != nil {
			handler.SetUsageTracker(a.usageTracker)
		}
		if a.memoryStore == nil {
			a.ensureMemoryStore()
		}
		if a.memoryStore != nil {
			handler.SetMemoryStore(a.memoryStore)
		}
		if store := a.ensureAIConfirmationStore(); store != nil {
			handler.SetConfirmationStore(store)
		}
		a.ensureAITrace()
		if a.aiTrace != nil {
			handler.SetTraceService(a.aiTrace)
		}
		handler.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
		if a.configManager != nil {
			handler.SetConfigManager(a.configManager)
		}
		if a.templateManager != nil {
			handler.SetTemplateManager(a.templateManager)
		}
		if a.scheduledTaskManager != nil {
			handler.SetScheduledTaskManager(a.scheduledTaskManager)
		}
		if a.contextResolver == nil {
			a.ensureContextResolver()
		}
		if a.contextResolver != nil {
			handler.SetContextResolver(a.contextResolver)
		}
		if a.sessionPrecheck == nil {
			a.ensureSessionPrecheck()
		}
		if a.sessionPrecheck != nil {
			handler.SetSessionPrecheck(a.sessionPrecheck)
		}
		a.ensureStartupFeedback()
		if a.startupFeedback != nil {
			handler.SetStartupFeedback(a.startupFeedback)
		}
		if a.securityFirewall == nil {
			a.ensureSecurityFirewall()
		}
		if a.securityFirewall != nil {
			handler.SetSecurityFirewall(a.securityFirewall)
		}
		// Wire IM file sender so the desktop AI assistant can forward files to
		// the user's Feishu/WeChat. Prefer local Weixin gateway; fall back to Hub.
		handler.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
			return a.forwardDesktopFileToIMRequest(hubClient, req)
		})
		// Initialize and wire BackgroundLoopManager + SessionMonitor.
		statusC := make(chan StatusEvent, 32)
		blm := NewBackgroundLoopManager(statusC)
		// Emit Wails event when background loop state changes.
		blm.OnChange = func() {
			if a.ctx != nil {
				a.emitEvent("background-loops-changed")
			}
		}
		handler.SetBackgroundLoopManager(blm)
		// Register GUI automation tools with async background replay support.
		registerGUIAutomationTools(handler.registry, blm, handler.agentActivity, statusC)
		// Text-primary Computer Use (OmniParser + OCR SoM for non-vision models).
		bindComputerUseYOLOGate(a)
		registerComputerUseTools(handler.registry)
		// Keep group discussion available after late tool registration rebuilds.
		registerGroupDiscussionTools(handler.registry, a, handler)
		// Rebuild the tool builder so it picks up the newly registered GUI tools.
		handler.toolBuilder = NewDynamicToolBuilder(handler.registry)
		// Re-sync the router's registry snapshot so late-registered tools
		// (computer_* and group discussion) get Priority/Tags scoring.
		handler.SetToolRouter(a.toolRouter)
		// Wire skill-aware routing to the tool builder.
		if a.skillExecutor != nil {
			handler.toolBuilder.SetSkillProvider(&skillExecutorProvider{executor: a.skillExecutor})
		}
		// Interruption relevance is useful with intent-only embedding as well;
		// tool vector retrieval is restored above only for full activation.
		if emb := a.activeInterruptEmbedder(); emb != nil && handler.interruptHandler != nil {
			handler.interruptHandler.SetEmbedder(emb)
		}

		sm := NewSessionMonitor(a.remoteSessions, statusC, 20*time.Second)
		handler.SetSessionMonitor(sm)

		a.ensureConversationArchiver()
		if a.conversationArchiver != nil {
			handler.memory.Archiver = a.conversationArchiver
		}
	}
}

// tryLockTool attempts to acquire a lock for installing a specific tool
// Returns true if lock acquired, false if tool is already being installed
func (a *App) tryLockTool(toolName string) bool {
	a.toolLockMutex.Lock()
	defer a.toolLockMutex.Unlock()

	if a.toolInstallLocks == nil {
		a.toolInstallLocks = make(map[string]bool)
	}
	if a.toolInstallLocks[toolName] {
		return false // Already being installed
	}
	a.toolInstallLocks[toolName] = true
	return true
}

// unlockTool releases the lock for a specific tool
func (a *App) unlockTool(toolName string) {
	a.toolLockMutex.Lock()
	defer a.toolLockMutex.Unlock()
	delete(a.toolInstallLocks, toolName)
}

// isToolLocked checks if a tool is currently being installed
func (a *App) isToolLocked(toolName string) bool {
	a.toolLockMutex.Lock()
	defer a.toolLockMutex.Unlock()
	if a.toolInstallLocks == nil {
		return false
	}
	return a.toolInstallLocks[toolName]
}

// IsToolBeingInstalled checks if a tool is currently being installed (exported for frontend)
func (a *App) IsToolBeingInstalled(toolName string) bool {
	return a.isToolLocked(toolName)
}

func (a *App) syncIMGatewaysFromConfig() {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()

	a.ensureQQBotGateway()
	a.ensureTelegramGateway()
	a.ensureWeixinGateway()
	a.ensureLansengerGateway()
	a.ensureThirdPartyGatewayLocked()
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	startupBegin := time.Now()
	log.Printf("[startup] begin")
	a.ctx = ctx
	// Initialize agentView sequence with a time-based epoch so post-restart
	// events are never rejected as stale by a surviving frontend WebView.
	a.initAgentViewSeqEpoch()
	a.logStoragePaths("startup.begin", nil)
	// Initialize code event emitter for code preview panel.
	a.codeEventEmitter = NewCodeEventEmitter(a)
	// Ambient date is rendered locally by the hardware; GUI owns live weather
	// resolution and publishes compact snapshots through the authenticated Hub.
	a.startDeviceAmbientWeather(ctx)
	// Migrate legacy ~/.cceasy data to ~/.maclaw/data on first launch.
	a.MigrateDataDir()
	// One-time enable shared agent loop for pre-default installs.
	a.migrateSharedAgentLoopOnce()
	// Initialize independent credential store (OAuth tokens separated from config.json).
	a.credentialStore = oauth.NewFileCredentialStore(oauth.DefaultCredentialStorePath())
	// Migrate data to custom data_dir if configured and not yet migrated.
	a.migrateToCustomDataDir()
	// Platform specific initialization
	a.platformStartup()
	a.startConfigWatcher()
	log.Printf("[startup] platform+config_watcher done in %v", time.Since(startupBegin))
	// Pre-warm gse Chinese segmenter dictionary in background so BM25
	// tool routing doesn't block on first use.
	bm25.PrewarmDict()
	// Initialize CodeBuddy config in project directory
	if config, err := a.LoadConfig(); err == nil {
		// Apply user-configured working directory (or keep default).
		corelib.SetWorkspaceDir(config.WorkingDirectory)
		effectiveWD := corelib.EffectiveWorkspaceDir()
		skillTemp := filepath.Join(effectiveWD, ".maclaw-tmp")
		if err := os.MkdirAll(skillTemp, 0o755); err != nil {
			log.Printf("[startup] warn: create skill_temp failed path=%q err=%v", skillTemp, err)
		}
		// Always-important markers (err= keyword forces log filter pass) + sidecar file.
		// Keep this line ASCII-only so Windows log encoding cannot drop it.
		log.Printf("[startup] workdir ready err=none download_file=builtin effective_wd_set=%t skill_temp_set=%t elapsed=%v",
			strings.TrimSpace(effectiveWD) != "", strings.TrimSpace(skillTemp) != "", time.Since(startupBegin))
		writeWorkdirReadySidecar(config.WorkingDirectory, effectiveWD, skillTemp)
		a.logStoragePaths("startup.config_loaded", &config)
		// a.syncToCodeBuddySettings(config, ")
		if config.Language != "" {
			a.SetLanguage(config.Language)
		}

		// Synchronous embedding engine initialization: ensure the intent classifier
		// has Layer 2 (embedding) ready BEFORE the UI becomes interactive.
		// This prevents the cold-start window where BM25 noise can trigger wrong
		// workflows because the semantic veto layer isn't available yet.
		// Only blocks when the model file exists on disk (~1.5s load time).
		// If the file is missing (needs download), skip and let async path handle it.
		a.ensureEmbeddingEngineSync(config.VectorSearchEnabled)

		if config.RemoteMachineID != "" && config.RemoteMachineToken != "" && config.RemoteHubURL != "" {
			// Synchronous: prepare Hub client infrastructure (fast, no network I/O).
			hubPrepStart := time.Now()
			a.prepareHubClientSync()
			log.Printf("[startup] prepareHubClientSync done in %v (since startup begin: %v)", time.Since(hubPrepStart), time.Since(startupBegin))
			// Mark AI assistant ready BEFORE Hub connection -user can interact immediately.
			a.markAIAssistantReady()
			a.emitEvent("ai-assistant-init-progress", "ready")
			// Background: Hub WebSocket connect + auth + sendHello (network I/O).
			go a.asyncHubConnect()
			go a.scheduleVirtualRepositorySync()
		} else if config.RemoteEmail != "" && config.RemoteHubURL != "" {
			// No full credentials yet -mark ready immediately, auto-register in background.
			hubPrepStart := time.Now()
			a.prepareHubClientSync()
			log.Printf("[startup] prepareHubClientSync (local/degraded) done in %v (since startup begin: %v)", time.Since(hubPrepStart), time.Since(startupBegin))
			a.markAIAssistantReady()
			a.emitEvent("ai-assistant-init-progress", "degraded")
			go a.autoRegisterOnStartup(config)
		} else {
			// No Hub credentials at all -mark ready immediately without attempting connection.
			// Frontend shows "degraded" (Hub offline) state, user can register from settings.
			hubPrepStart := time.Now()
			a.prepareHubClientSync()
			log.Printf("[startup] prepareHubClientSync (local/degraded) done in %v (since startup begin: %v)", time.Since(hubPrepStart), time.Since(startupBegin))
			a.markAIAssistantReady()
			a.emitEvent("ai-assistant-init-progress", "degraded")
		}
		a.refreshPowerOptimizationStateFromConfig(config)
		a.refreshWorkstationMode(config)
		a.maybeCleanToolCacheOnStartup(config)
		// Auto-start memory compression service if enabled in config.
		if config.MemoryAutoCompress && a.memoryStore != nil {
			_ = a.getOrCreateCompressor()
			if a.memoryMaintenance != nil {
				_ = a.memoryMaintenance.StartCompressor()
			}
		}
		// Auto-start local MCP servers that are enabled for app launch.
		a.autoStartLocalMCPServers(config.LocalMCPServers)

		// Ensure local AI models are ready for features the user has not
		// explicitly disabled. Missing assets are downloaded and then enabled.
		go a.ensureConfiguredAIModels()
		// Computer Use: pre-start UIA sidecar / seed csharp exe in background.
		go a.backgroundWarmupComputerUse()

		// Keep the AI assistant in a lightweight "connecting" state until the
		// user actually opens or uses it; avoid pre-warming the full interaction
		// stack during startup because it eagerly loads memory-heavy services.
		// Auto-start IM gateways that were previously enabled.
		// If Hub is connected, createAndWireHubClient already started them;
		// only start here when Hub credentials are absent (pure local mode).
		if config.RemoteMachineID == "" || config.RemoteMachineToken == "" || config.RemoteHubURL == "" {
			go func() {
				a.syncIMGatewaysFromConfig()
			}()
		}
		// Mode B: loopback ACP host so VS Code can use GUI AI assistant as programming agent.
		go func() {
			// Defer slightly so hub/IM handler warmup can begin first.
			time.Sleep(2 * time.Second)
			a.ensureACPHost()
		}()
		// CodeGen SSO token validation on startup (qianxin brand only).
		// Claude/TigerClaw Code now uses CodeGen's remote Anthropic-compatible
		// base URL directly; the legacy local adapter remains disabled.
		go func() {
			if err := a.ensureCodeGenToken(); err != nil {
				log.Printf("[CodeGen] startup token check failed: %v", err)
			} else if err := a.ensureCodeGenConfiguredModelAvailable(); err != nil {
				log.Printf("[CodeGen] startup model availability check failed: %v", err)
			}
		}()
		// Migrate OAuth credentials from config.json to independent credential store.
		go a.migrateOAuthCredentialsOnStartup(config)
		go a.startIWorkerGoalWatchIfConfigured(config)
		go func() {
			time.Sleep(90 * time.Second)
			a.recoverSkillUploadQueueAfterStartup()
		}()
		if config.PetEnabled {
			go func(cfg corelib.AppConfig) {
				if fa := a.ensureFloatingAssistant(); fa != nil {
					fa.RefreshAppearance(cfg)
				}
			}(config)
		}

		// Legacy workflow engine init disabled — V2 is sole engine.
		// go a.initWorkflowEngine()
		// Initialize V2 workflow engine (clean state machine).
		a.workflowV2 = a.initWorkflowV2()
		// Initialize steering store (declarative rule injection from ~/.maclaw/steering/).
		go a.initSteeringStore()
		// Initialize TTS manager if assets are already present.
		go a.initTTSManager()
		a.startSkillArtifactRegistryMaintenance(ctx)

		// Initialize project tab session persistence and clean up stale sessions (>30 days).
		a.projectTabSessionPersist = NewProjectTabSessionPersistForBaseDir(a.getMaclawBaseDir())
		go func() {
			removed, err := a.projectTabSessionPersist.CleanupStale(30 * 24 * time.Hour)
			if err != nil {
				log.Printf("[startup] project tab session cleanup error: %v", err)
			} else if removed > 0 {
				log.Printf("[startup] project tab session cleanup: removed %d stale sessions", removed)
			}
		}()

		// Create shared intent classifiers synchronously. Until semantic classifiers are ready,
		// they return conservative unknown/ambiguous results instead of local keyword decisions.
		classifierStart := time.Now()
		a.initEarlyClassifier()
		log.Printf("[startup] initEarlyClassifier done in %v", time.Since(classifierStart))

		log.Printf("[startup] complete in %v", time.Since(startupBegin))
		return
	}
	a.setPowerOptimizationEnabled(false)
	log.Printf("[startup] complete (no config) in %v", time.Since(startupBegin))
}

// domReady is called after the frontend Dom has been loaded
func (a *App) domReady(ctx context.Context) {
	log.Printf("[domReady] frontend DOM loaded, warmup_done=%v interaction_infra_ready=%v", a.warmupDone.Load(), a.interactionInfraReady())
	// Trigger environment check on startup
	// IsInitMode and PauseEnvCheck logic is handled inside CheckEnvironment
	a.CheckEnvironment(false)
	// Always arm the loop: each attempt re-reads check_update_on_startup so
	// enabling/disabling mid-session takes effect without a restart.
	a.startBackgroundUpdateChecks()
	// Startup may begin maximised (low-res policy). Clamp after first paint so
	// Win10 frameless windows do not sit under the taskbar.
	a.scheduleClampMaximizedWindowToWorkArea(120 * time.Millisecond)
}

// GetUIZoomFactor returns the saved UI zoom factor.
// 0 means Auto (frontend applies a DPI/resolution-based recommendation).
// Manual values are in [0.5, 2.0].
func (a *App) GetUIZoomFactor() float64 {
	cfg, err := a.LoadConfig()
	if err != nil {
		return 0
	}
	if cfg.UIZoomFactor < 0 {
		return 0
	}
	if cfg.UIZoomFactor > 0 && cfg.UIZoomFactor < 0.5 {
		return 0.5
	}
	if cfg.UIZoomFactor > 2.0 {
		return 2.0
	}
	return cfg.UIZoomFactor
}

// SetUIZoomFactor persists the UI zoom factor.
// Pass 0 for Auto (DPI-adaptive). Manual range is [0.5, 2.0].
func (a *App) SetUIZoomFactor(factor float64) error {
	if factor < 0 {
		factor = 0
	}
	if factor > 0 && factor < 0.5 {
		factor = 0.5
	}
	if factor > 2.0 {
		factor = 2.0
	}
	_, err := a.PatchConfigFields(map[string]interface{}{"ui_zoom_factor": factor})
	return err
}

// GetChatFontSize returns the saved chat font size in pixels (default 14).
func (a *App) GetChatFontSize() int {
	cfg, err := a.LoadConfig()
	if err != nil || cfg.ChatFontSize <= 0 {
		return 14
	}
	if cfg.ChatFontSize < 12 {
		return 14
	}
	if cfg.ChatFontSize > 24 {
		return 24
	}
	return cfg.ChatFontSize
}

// SetChatFontSize persists the chat font size (clamped to 12-24).
func (a *App) SetChatFontSize(size int) error {
	if size < 12 {
		size = 12
	}
	if size > 24 {
		size = 24
	}
	_, err := a.PatchConfigFields(map[string]interface{}{"chat_font_size": size})
	return err
}

func (a *App) shutdown(ctx context.Context) {
	if a.screenDimCancel != nil {
		a.screenDimCancel()
	}
	a.maybeCleanToolCacheOnExit()
	// Clean up workstation mode (restore lock screen policy, etc.)
	a.setWorkstationMode(false, 0)
	a.stopIWorkerGoalWatch()
	if a.localMCPManager != nil {
		a.localMCPManager.StopAll()
	}
	a.conversationMemoryMu.Lock()
	conversationMemory := a.aiConversationMemory
	a.aiConversationMemory = nil
	a.conversationMemoryMu.Unlock()
	if conversationMemory != nil {
		// Force-flush dirty state to disk BEFORE stopping the persist loop.
		// When the process is killed by an updater (no graceful shutdown),
		// the OS-level signal handler may still reach this code path via
		// Wails OnShutdown. Without an explicit flush, the 150ms debounce
		// timer in persistLoop may not have fired yet, losing the latest
		// conversation history and unfinished task slots.
		if err := conversationMemory.FlushNow(); err != nil {
			log.Printf("[shutdown] conversation memory flush failed: %v", err)
		}
		conversationMemory.Stop()
	}
	// Pure-coding sticky (.coding_workbench.json): step-status updates use a
	// debounced disk write; flush all pending + in-memory sessions now.
	// Use existing hubClient only — do not ensure/create infrastructure at exit.
	if hub := a.hubClient(); hub != nil && hub.imHandler != nil {
		hub.imHandler.FlushAllStickyCodingWorkbenchMemory()
	} else {
		flushAllStickyCodingDebouncedPersist()
	}
	a.confirmationStoreMu.Lock()
	confirmationStore := a.aiConfirmationStore
	a.aiConfirmationStore = nil
	a.confirmationStoreMu.Unlock()
	if confirmationStore != nil {
		confirmationStore.stop()
	}
	if a.memoryMaintenance != nil {
		a.memoryMaintenance.Stop()
	} else {
		if a.memPipeline != nil {
			a.memPipeline.Stop()
		}
		a.compressorMu.Lock()
		memoryCompressor := a.memoryCompressor
		a.compressorMu.Unlock()
		if memoryCompressor != nil {
			memoryCompressor.Stop()
		}
	}
	a.stopMemoryPipelineSchedule("shutdown")
	if a.memoryStore != nil {
		a.memoryStore.Stop()
	}
	// Close V2 workflow SQLite store.
	if a.workflowV2 != nil {
		if closer, ok := a.workflowV2.store.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	// Close coding knowledge store.
	if a.codingKnowledgeStore != nil {
		_ = a.codingKnowledgeStore.Close()
		a.codingKnowledgeStore = nil
	}
	// OpenHuman modules shutdown
	if a.ohModules.toolMemory != nil {
		a.ohModules.toolMemory.Flush()
	}
	if a.ohModules.heartbeat != nil {
		a.ohModules.heartbeat.Stop()
	}
	if a.ohModules.subconsciousEngine != nil {
		a.ohModules.subconsciousEngine.Stop()
	}
	if a.ohModules.autoFetchEngine != nil {
		a.ohModules.autoFetchEngine.Stop()
	}
	if a.ohModules.eventBus != nil {
		a.ohModules.eventBus.Close()
	}
	if a.scheduledTaskManager != nil {
		a.scheduledTaskManager.Stop() // cancels ticker + all in-flight executor goroutines
	}
	if a.skillLifecycle != nil {
		a.skillLifecycle.StopBackgroundProcessing()
	}
	if a.evolutionPipeline != nil {
		a.evolutionPipeline.Stop()
	}
	if a.stopHubTicker != nil {
		close(a.stopHubTicker)
	}
	if a.mdnsScanner != nil {
		a.mdnsScanner.Stop()
	}
	if a.auditLog != nil {
		a.auditLog.Close()
	}
	if a.sessionSearchStore != nil {
		_ = a.sessionSearchStore.Close()
		a.sessionSearchStore = nil
	}
	if a.qqBotGateway != nil {
		a.qqBotGateway.Stop()
	}
	if a.telegramGateway != nil {
		a.telegramGateway.Stop()
	}
	if a.weixinGateway != nil {
		a.weixinGateway.Stop()
	}
	if a.lansengerGateway != nil {
		a.lansengerGateway.Stop()
	}
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.Stop()
	}
	a.platformShutdown()
}

func (a *App) refreshPowerOptimizationStateFromConfig(config corelib.AppConfig) {
	enabled := config.PowerOptimization && a.hasActiveRemoteTasks()
	a.setPowerOptimizationEnabled(enabled)
	// Workstation mode also owns the display-off timer: it prevents sleep/lock
	// while still allowing the screen to turn off after the configured idle time.
	a.updateScreenDimTimer(enabled || config.WorkstationMode, config.ScreenDimTimeoutMin)
}

func (a *App) refreshPowerOptimizationState() {
	config, err := a.LoadConfig()
	if err != nil {
		a.setPowerOptimizationEnabled(false)
		return
	}
	a.refreshPowerOptimizationStateFromConfig(config)
}

func (a *App) hasActiveRemoteTasks() bool {
	if a.remoteSessions == nil {
		return false
	}
	return a.remoteSessions.HasActiveSessions()
}

func (a *App) resolveProjectProxyURL(config corelib.AppConfig, projectDir string) string {
	var proxyHost, proxyPort, proxyUsername, proxyPassword string

	var targetProj *corelib.ProjectConfig
	for i := range config.Projects {
		if config.Projects[i].Path == projectDir {
			targetProj = &config.Projects[i]
			break
		}
	}
	if targetProj == nil {
		for i := range config.Projects {
			if config.Projects[i].Id == config.CurrentProject {
				targetProj = &config.Projects[i]
				break
			}
		}
	}

	if targetProj != nil {
		proxyHost = targetProj.ProxyHost
		proxyPort = targetProj.ProxyPort
		proxyUsername = targetProj.ProxyUsername
		proxyPassword = targetProj.ProxyPassword
	}

	if proxyHost == "" {
		proxyHost = config.DefaultProxyHost
		proxyPort = config.DefaultProxyPort
		proxyUsername = config.DefaultProxyUsername
		proxyPassword = config.DefaultProxyPassword
	}

	if proxyHost == "" || proxyPort == "" {
		return ""
	}

	scheme := config.DefaultProxyProtocol
	if scheme == "" {
		scheme = "http"
	}

	if proxyUsername != "" && proxyPassword != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%s", scheme, proxyUsername, proxyPassword, proxyHost, proxyPort)
	}
	return fmt.Sprintf("%s://%s:%s", scheme, proxyHost, proxyPort)
}

// Named flags for whether launch-env builders may rewrite each tool's native
// CLI config files (~/.codex, ~/.claude, …). Prefer these over bare booleans.
const (
	persistNativeToolConfig = true  // real LaunchTool / remote session start
	probeNativeToolConfig   = false // diagnostics, readiness, dry-run probes
)

func (a *App) buildClaudeLaunchEnv(
	config corelib.AppConfig,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	writeNativeConfig bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected claude model is nil")
	}

	env := map[string]string{}
	env["CLAUDE_CODE_USE_COLORS"] = "true"
	env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "128000"
	env["MAX_THINKING_TOKENS"] = "10000"

	wireAPI := effectiveToolWireAPI("claude", *selectedModel)
	if !selectedModel.IsBuiltin {
		if strings.TrimSpace(selectedModel.WireApi) != "" && !corelib.IsAnthropicWireAPI(wireAPI) {
			return nil, fmt.Errorf("claude provider %q must use anthropic wire_api", selectedModel.ModelName)
		}
		if selectedModel.ApiKey != "" {
			env["ANTHROPIC_AUTH_TOKEN"] = strings.TrimSpace(selectedModel.ApiKey)
		}
		if selectedModel.ModelUrl != "" {
			env["ANTHROPIC_BASE_URL"] = strings.TrimSpace(selectedModel.ModelUrl)
		}
		if selectedModel.ModelId != "" {
			modelID := strings.TrimSpace(selectedModel.ModelId)
			// CodeGen's model list includes both "auto" (OpenAI shorthand) and
			// "qax-codegen/Auto" (canonical name). Claude Code uses Anthropic
			// protocol which requires the canonical form. Normalize here so
			// users who pick "auto" from the model list don't get a 400.
			if corelib.IsCodeGenURL(selectedModel.ModelUrl) {
				modelID = corelib.NormalizeCodeGenModel(modelID)
			}
			env["ANTHROPIC_MODEL"] = modelID
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelID
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelID
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelID
		}
	}

	switch normalizeModelProviderKind(selectedModel.ModelName) {
	case modelProviderQianfan:
		modelID := selectedModel.ModelId
		if modelID == "" {
			modelID = "qianfan-code-latest"
		}
		env["ANTHROPIC_AUTH_TOKEN"] = selectedModel.ApiKey
		env["ANTHROPIC_BASE_URL"] = "https://qianfan.baidubce.com/anthropic/coding"
		env["ANTHROPIC_MODEL"] = modelID
		env["ANTHROPIC_SMALL_FAST_MODEL"] = modelID
	}

	// For all non-builtin (third-party) providers: disable nonessential traffic
	// and increase API timeout. Third-party Anthropic-compatible APIs
	// such as DeepSeek typically have stricter rate limits than Anthropic's own API.
	// Claude Code's internal agent loop sends requests very rapidly without human
	// interaction pauses, which easily triggers 429 rate limits on these providers.
	// CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 prevents background telemetry,
	// model availability checks, and other non-essential API calls.
	// API_TIMEOUT_MS=600000 (10 min) gives the provider's rate limiter time to
	// recover between retries instead of failing fast.
	if !selectedModel.IsBuiltin {
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		if _, ok := env["API_TIMEOUT_MS"]; !ok {
			env["API_TIMEOUT_MS"] = "600000"
		}
	}

	for _, proj := range config.Projects {
		if proj.Path == projectDir || proj.Id == config.CurrentProject {
			if proj.TeamMode {
				env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] = "1"
			}
			break
		}
	}

	a.injectProxyEnv(env, config, projectDir, useProxy)

	if !writeNativeConfig {
		return env, nil
	}

	if !selectedModel.IsBuiltin {
		// Surgical update: write only provider-specific env fields into
		// ~/.claude/settings.json, preserving all user state (conversations,
		// plugins, hooks, etc.).
		if err := configfile.WriteClaudeProviderSettings(selectedModel.ModelName, selectedModel.ApiKey, env["ANTHROPIC_BASE_URL"], env["ANTHROPIC_MODEL"]); err != nil {
			return nil, fmt.Errorf("write claude provider settings: %w", err)
		}
	} else {
		// Surgical cleanup: remove only third-party env fields from settings.json,
		// preserving all other user state (conversations, plugins, hooks, etc.).
		if err := configfile.ClearClaudeThirdPartySettings(); err != nil {
			log.Printf("[LaunchTool] Claude: ClearClaudeThirdPartySettings error: %v", err)
		}

		// Backward-compat migration: if a pre-fix backup directory exists
		// (created by the old clearClaudeConfig -backupToolNativeConfig flow),
		// restore it one last time so users don't lose their old state.
		backupDir := filepath.Join(a.configBackupDir("claude"), ".claude")
		if info, err := os.Stat(backupDir); err == nil && info.IsDir() {
			log.Printf("[LaunchTool] Claude: pre-fix backup found at %s -running one-time migration restore", backupDir)
			a.restoreToolNativeConfig("claude")
		}
	}
	return env, nil
}

func (a *App) buildClaudeLaunchSpec(
	config corelib.AppConfig,
	yoloMode bool,
	adminMode bool,
	pythonEnv string,
	projectDir string,
	useProxy bool,
) (LaunchSpec, error) {
	return a.buildRemoteLaunchSpec("claude", config, yoloMode, adminMode, pythonEnv, projectDir, useProxy, "", persistNativeToolConfig)
}

func (a *App) buildCodexLaunchEnv(
	config corelib.AppConfig,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	writeNativeConfig bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected codex model is nil")
	}

	env := map[string]string{}

	if !selectedModel.IsBuiltin {
		baseURL := normalizedOpenAICompatibleToolBaseURL(remoteToolNameCodex.String(), *selectedModel)
		if selectedModel.ApiKey != "" {
			env["OPENAI_API_KEY"] = selectedModel.ApiKey
		}
		if baseURL != "" {
			env["OPENAI_BASE_URL"] = baseURL
		}
		if writeNativeConfig {
			// Surgical update: persist provider config to ~/.codex/auth.json and
			// ~/.codex/config.toml so Codex subprocesses pick up the settings.
			// Preserves user's MCP servers, profiles, and other config.
			if err := configfile.WriteCodexConfigWithClientName(selectedModel.ApiKey, baseURL, selectedModel.ModelId, selectedModel.ModelName, effectiveToolWireAPI("codex", *selectedModel), codeGenClientNameForModelConfig(config, *selectedModel)); err != nil {
				return nil, fmt.Errorf("prepare codex provider switch: %w", err)
			}
		}
	} else if writeNativeConfig {
		// Surgical cleanup: remove only third-party auth/provider entries,
		// preserving all other user state (MCP servers, profiles, etc.).
		if err := configfile.ClearCodexThirdPartySettings(); err != nil {
			return nil, fmt.Errorf("prepare codex builtin switch: %w", err)
		}

		// Backward-compat migration: if a pre-fix backup directory exists
		// (created by the old clearCodexConfig -backupToolNativeConfig flow),
		// restore it one last time so users don't lose their old state.
		backupDir := filepath.Join(a.configBackupDir("codex"), ".codex")
		if info, err := os.Stat(backupDir); err == nil && info.IsDir() {
			log.Printf("[LaunchTool] Codex: pre-fix backup found at %s -running one-time migration restore", backupDir)
			a.restoreToolNativeConfig("codex")
		}
	}

	a.injectProxyEnv(env, config, projectDir, useProxy)

	return env, nil
}

func (a *App) buildOpencodeLaunchEnv(
	config corelib.AppConfig,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	writeNativeConfig bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected opencode model is nil")
	}

	env := map[string]string{}
	if !selectedModel.IsBuiltin {
		baseURL := normalizedOpenAICompatibleToolBaseURL(remoteToolNameOpencode.String(), *selectedModel)
		if selectedModel.ApiKey != "" {
			env["OPENCODE_API_KEY"] = selectedModel.ApiKey
		}
		if baseURL != "" {
			env["OPENCODE_BASE_URL"] = baseURL
		}
		if selectedModel.ModelId != "" {
			env["OPENCODE_MODEL"] = selectedModel.ModelId
		}
		if writeNativeConfig {
			a.backupToolNativeConfig("opencode")
			// Write ~/.config/opencode/opencode.json for persistence across subprocess restarts.
			// Env vars alone don't populate the model selector in OpenCode's UI.
			if err := configfile.WriteOpencodeConfig(selectedModel.ApiKey, baseURL, selectedModel.ModelId, selectedModel.ModelName); err != nil {
				log.Printf("[opencode-config] failed to write config: %v", err)
			}
		}
	} else {
		if writeNativeConfig {
			// Restore native config so Opencode can use its own auth.
			a.restoreToolNativeConfig("opencode")
		}
		if selectedModel.ModelId != "" {
			env["OPENCODE_MODEL"] = selectedModel.ModelId
		}
	}

	a.injectProxyEnv(env, config, projectDir, useProxy)

	return env, nil
}

func (a *App) buildIFlowLaunchEnv(
	config corelib.AppConfig,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	writeNativeConfig bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected iflow model is nil")
	}

	env := map[string]string{}
	if !selectedModel.IsBuiltin {
		baseURL := normalizedOpenAICompatibleToolBaseURL(remoteToolNameIFlow.String(), *selectedModel)
		if selectedModel.ApiKey != "" {
			env["OPENAI_API_KEY"] = selectedModel.ApiKey
			env["IFLOW_API_KEY"] = selectedModel.ApiKey
		}
		if baseURL != "" {
			env["OPENAI_BASE_URL"] = baseURL
			env["IFLOW_BASE_URL"] = baseURL
		}
		if selectedModel.ModelId != "" {
			env["IFLOW_MODEL"] = selectedModel.ModelId
		}
		if writeNativeConfig {
			a.backupToolNativeConfig("iflow")
			// Write ~/.iflow/settings.json for persistence across subprocess restarts.
			// Without this config file, iFlow CLI prompts the user to configure provider URL.
			if err := configfile.WriteIFlowConfig(selectedModel.ApiKey, baseURL, selectedModel.ModelId); err != nil {
				log.Printf("[iflow-config] failed to write config: %v", err)
			}
		}
	} else {
		if writeNativeConfig {
			// Restore native config so iFlow can use its own auth.
			a.restoreToolNativeConfig("iflow")
		}
		if selectedModel.ModelId != "" {
			env["IFLOW_MODEL"] = selectedModel.ModelId
		}
	}

	a.injectProxyEnv(env, config, projectDir, useProxy)

	return env, nil
}

func (a *App) buildKiloLaunchEnv(
	config corelib.AppConfig,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	writeNativeConfig bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected kilo model is nil")
	}

	env := map[string]string{}
	if !selectedModel.IsBuiltin {
		baseURL := normalizedOpenAICompatibleToolBaseURL(remoteToolNameKilo.String(), *selectedModel)
		if selectedModel.ApiKey != "" {
			env["OPENAI_API_KEY"] = selectedModel.ApiKey
			env["KILO_API_KEY"] = selectedModel.ApiKey
		}
		if baseURL != "" {
			env["OPENAI_BASE_URL"] = baseURL
			env["KILO_BASE_URL"] = baseURL
		}
		if selectedModel.ModelId != "" {
			env["KILO_MODEL"] = selectedModel.ModelId
		}
		if writeNativeConfig {
			a.backupToolNativeConfig("kilo")
			// Write ~/.kilocode/cli/config.json for persistence across subprocess restarts.
			// Env vars alone don't populate the model selector in Kilo Code's UI.
			if err := configfile.WriteKiloConfig(selectedModel.ApiKey, baseURL, selectedModel.ModelId); err != nil {
				log.Printf("[kilo-config] failed to write config: %v", err)
			}
		}
	} else {
		if writeNativeConfig {
			// Restore native config so Kilo can use its own auth.
			a.restoreToolNativeConfig("kilo")
		}
		if selectedModel.ModelId != "" {
			env["KILO_MODEL"] = selectedModel.ModelId
		}
	}

	a.injectProxyEnv(env, config, projectDir, useProxy)

	return env, nil
}

func (a *App) buildRemoteLaunchEnvForTool(
	toolName string,
	config corelib.AppConfig,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	writeNativeConfig bool,
) (map[string]string, error) {
	toolKind := normalizeRemoteToolNameKind(toolName)
	switch toolKind {
	case remoteToolNameClaude:
		return a.buildClaudeLaunchEnv(config, selectedModel, projectDir, useProxy, writeNativeConfig)
	case remoteToolNameCodex:
		return a.buildCodexLaunchEnv(config, selectedModel, projectDir, useProxy, writeNativeConfig)
	case remoteToolNameOpencode:
		return a.buildOpencodeLaunchEnv(config, selectedModel, projectDir, useProxy, writeNativeConfig)
	case remoteToolNameIFlow:
		return a.buildIFlowLaunchEnv(config, selectedModel, projectDir, useProxy, writeNativeConfig)
	case remoteToolNameKilo:
		return a.buildKiloLaunchEnv(config, selectedModel, projectDir, useProxy, writeNativeConfig)
	default:
		// Check OEM extra tools
		extraTool := findExtraTool(toolKind.String())
		if extraTool != nil {
			return a.buildExtraToolLaunchEnv(extraTool, selectedModel, projectDir, useProxy, config)
		}
		return nil, fmt.Errorf("remote launch is not supported for tool: %s", toolName)
	}
}

// findExtraTool looks up an OEM extra tool by name from brand.Current().ExtraTools.
// Returns nil if no matching extra tool is found.
func findExtraTool(toolName string) *brand.ExtraToolDef {
	for i, et := range brand.Current().ExtraTools {
		if et.Name == toolName {
			return &brand.Current().ExtraTools[i]
		}
	}
	return nil
}

// buildExtraToolLaunchEnv builds environment variables for an OEM extra tool.
// If the ExtraToolDef has a custom EnvBuilderFunc, it is used; otherwise a
// generic OpenAI-compatible env set is produced.
func (a *App) buildExtraToolLaunchEnv(
	et *brand.ExtraToolDef,
	selectedModel *corelib.ModelConfig,
	projectDir string,
	useProxy bool,
	config corelib.AppConfig,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected model is nil for extra tool %s", et.Name)
	}

	var env map[string]string
	if et.EnvBuilderFunc != nil {
		env = et.EnvBuilderFunc(nil, selectedModel, projectDir)
	} else {
		// Generic OpenAI-compatible environment variable builder
		env = map[string]string{}
		if selectedModel.ApiKey != "" {
			env["OPENAI_API_KEY"] = selectedModel.ApiKey
		}
		if selectedModel.ModelUrl != "" {
			env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
		}
		if selectedModel.ModelId != "" {
			env["OPENAI_MODEL"] = selectedModel.ModelId
		}
	}

	a.injectProxyEnv(env, config, projectDir, useProxy)

	return env, nil
}

func (a *App) buildRemoteLaunchSpec(
	toolName string,
	config corelib.AppConfig,
	yoloMode bool,
	adminMode bool,
	pythonEnv string,
	projectDir string,
	useProxy bool,
	providerOverride string,
	writeNativeConfig bool,
) (LaunchSpec, error) {
	tool := normalizeRemoteToolName(toolName)
	meta, err := getRemoteToolMetadata(tool)
	if err != nil {
		return LaunchSpec{}, err
	}
	if meta.ConfigSelector == nil {
		return LaunchSpec{}, fmt.Errorf("tool config unavailable for tool: %s", tool)
	}
	toolCfg := meta.ConfigSelector(config)

	targetProvider := toolCfg.CurrentModel
	if strings.TrimSpace(providerOverride) != "" {
		targetProvider = strings.TrimSpace(providerOverride)
	}

	var selectedModel *corelib.ModelConfig
	for _, m := range toolCfg.Models {
		if strings.EqualFold(m.ModelName, targetProvider) {
			model := m
			selectedModel = &model
			break
		}
	}
	if selectedModel == nil {
		if strings.TrimSpace(providerOverride) != "" {
			return LaunchSpec{}, fmt.Errorf("provider %q not found for tool %s", targetProvider, tool)
		}
		return LaunchSpec{}, fmt.Errorf("no %s provider selected", tool)
	}

	if !isValidProvider(*selectedModel) {
		return LaunchSpec{}, fmt.Errorf("provider %q has no API key configured", targetProvider)
	}

	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}
	projectDir = filepath.Clean(projectDir)

	env, err := a.buildRemoteLaunchEnvForTool(tool, config, selectedModel, projectDir, useProxy, writeNativeConfig)
	if err != nil {
		return LaunchSpec{}, err
	}

	title := filepath.Base(projectDir)
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = meta.DefaultTitle
	}

	teamMode := false
	if normalizeRemoteToolNameKind(tool).IsClaude() {
		for _, proj := range config.Projects {
			if proj.Path == projectDir || proj.Id == config.CurrentProject {
				teamMode = proj.TeamMode
				break
			}
		}
	}

	return LaunchSpec{
		Tool:         tool,
		ProjectPath:  projectDir,
		ModelName:    selectedModel.ModelName,
		ModelID:      selectedModel.ModelId,
		IsBuiltin:    selectedModel.IsBuiltin,
		BinaryName:   meta.BinaryName,
		Title:        title,
		LaunchSource: RemoteLaunchSourceDesktop,
		YoloMode:     a.enforceYoloModeQuiet(yoloMode),
		AdminMode:    adminMode,
		PythonEnv:    pythonEnv,
		UseProxy:     useProxy,
		TeamMode:     teamMode,
		Env:          env,
	}, nil
}

func (a *App) startConfigWatcher() {
	var err error
	a.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		a.log("Failed to create file watcher: " + err.Error())
		return
	}
	go func() {
		for {
			select {
			case event, ok := <-a.watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					a.configMu.Lock()
					a.invalidateConfigCacheLocked()
					a.configMu.Unlock()
					// Skip events triggered by our own writes (debounce 500ms).
					if time.Now().UnixMilli()-a.configLastInternalWrite.Load() < 500 {
						continue
					}
					a.log(a.tr("Config file modified: ") + event.Name)
					// External edit detected  - reload and notify frontend.
					config, err := a.LoadConfig()
					if err == nil {
						a.refreshPowerOptimizationStateFromConfig(config)
						a.refreshWorkstationMode(config)
						a.emitEvent("config-updated", config)
						// Re-sync IM gateways on config change, including gateways
						// newly enabled by direct config edits.
						a.syncIMGatewaysFromConfig()
						// Trigger tool outcome invalidation for externally-modified
						// SSH host or LLM provider fields. The fingerprint mechanism
						// catches this on the next RecordExperience call, but
						// immediate invalidation ensures ContextOutcomeScore is
						// accurate before that next call.
						a.invalidateOutcomesFromExternalConfigChange(config)
					}
				}
			case err, ok := <-a.watcher.Errors:
				if !ok {
					return
				}
				a.log("Watcher error: " + err.Error())
			}
		}
	}()
	configPath, err := a.getConfigPath()
	if err == nil {
		if err := a.watcher.Add(configPath); err != nil {
			a.log("Failed to watch config file: " + err.Error())
		} else {
			a.log("Watching config file: " + configPath)
		}
	}
}
func (a *App) SetLanguage(lang string) {
	a.CurrentLanguage = lang
	setAgentViewLang(lang)
	if UpdateTrayMenu != nil {
		UpdateTrayMenu(lang)
	}
}

// Greet returns a greeting for the given name
func (a *App) ResizeWindow(width, height int) {
	// The frontend calls this after the environment check completes. Preserve the
	// low-resolution startup policy here as well: on Windows, WindowSetSize can
	// otherwise change the geometry after the window was initially maximised.
	if shouldMaximiseMainWindowForPrimaryScreen() {
		runtime.WindowMaximise(a.ctx)
		// Win10 frameless maximise can cover the taskbar; clamp after the OS settles.
		a.scheduleClampMaximizedWindowToWorkArea(50 * time.Millisecond)
		return
	}
	runtime.WindowSetSize(a.ctx, width, height)
	runtime.WindowCenter(a.ctx)
}

// GetAdaptiveWindowSize returns the recommended window dimensions for the
// current screen, matching the logic used at startup. Frontend can call this
// instead of hardcoding resize dimensions.
func (a *App) GetAdaptiveWindowSize() map[string]int {
	w, h := adaptiveWindowSize()
	return map[string]int{"width": w, "height": h}
}

// RestoreWindowGeometry is no longer used -kept as no-op for binding compatibility.
func (a *App) RestoreWindowGeometry() {
}

// MaximiseAndSaveGeometry is no longer used -kept as no-op for binding compatibility.
func (a *App) MaximiseAndSaveGeometry() bool {
	return false
}

func (a *App) WindowHide() {
	runtime.WindowHide(a.ctx)
	if UpdateTrayVisibility != nil {
		UpdateTrayVisibility(false)
	}
	if fa := a.ensureFloatingAssistant(); fa != nil {
		fa.ShowFloatingButton()
	}
}
func (a *App) SetFullscreen(fullscreen bool) {
	if fullscreen {
		runtime.WindowMaximise(a.ctx)
		a.scheduleClampMaximizedWindowToWorkArea(50 * time.Millisecond)
	} else {
		runtime.WindowUnmaximise(a.ctx)
	}
}
func (a *App) SelectProjectDir() string {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Project Directory",
	})
	if err != nil {
		return ""
	}
	return selection
}

// SelectWorkingDir opens a native directory picker for the user to choose
// a default working directory. Returns the selected path or empty string
// if cancelled.
func (a *App) SelectWorkingDir() string {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Working Directory",
	})
	if err != nil {
		return ""
	}
	return selection
}

// SetWorkflowWorkingDir sets the working directory for the current coding
// workflow. This updates the project-scoped workflow when a current project is
// selected; otherwise it falls back to the desktop workflow owner.
func (a *App) SetWorkflowWorkingDir(dir string) {
	trimmed, _, err := normalizeWorkflowProjectPath(dir)
	if trimmed == "" || a == nil {
		return
	}
	if err != nil {
		log.Printf("[workflow-v2] invalid workflow working directory %s: %v", strings.TrimSpace(dir), err)
		return
	}
	// Single path with SetTabWorkingDir: keep phase prompts / adapter / V2 state aligned.
	a.syncActiveWorkflowWorkingDir(a.workflowOwnerIDForCurrentProject(), trimmed)
}

func (a *App) workflowOwnerIDForCurrentProject() string {
	if a == nil {
		return desktopUserID
	}
	projectOwnerID := projectSessionOwnerID(a.GetCurrentProjectPath())
	if projectOwnerID != desktopUserID {
		if a.workflowEngine != nil {
			if state := a.workflowEngine.GetActiveWorkflow(projectOwnerID); state != nil {
				return projectOwnerID
			}
		}
		if a.workflowV2 != nil && a.workflowV2.machine != nil && a.workflowV2.machine.GetActive(projectOwnerID) != nil {
			return projectOwnerID
		}
	}
	return desktopUserID
}

// GetWorkflowWorkingDir returns the current workflow working directory.
func (a *App) GetWorkflowWorkingDir() string {
	if a == nil {
		return ""
	}
	ownerID := a.workflowOwnerIDForCurrentProject()
	// V2: return the active workflow's project path if available.
	if a.workflowV2 != nil && a.workflowV2.machine != nil {
		if state := a.workflowV2.machine.GetActive(ownerID); state != nil {
			return state.ProjectPath
		}
	}
	if a.workflowEngine != nil {
		if state := a.workflowEngine.GetActiveWorkflow(ownerID); state != nil {
			return strings.TrimSpace(state.ProjectPath)
		}
	}
	return ""
}

// RefreshWorkflowV2StateForTab re-emits the full workflow state (phase_update + doc_update)
// for the given tab's project path. Called by the frontend on tab switch to ensure the
// preview panel shows the correct workflow state even if events were missed while the tab
// was inactive (background agent loops emit events that are rejected by the inactive tab's
// event filter — this refresh bridges that gap).
func (a *App) RefreshWorkflowV2StateForTab(projectPath string, tabID ...string) {
	if a.workflowV2 == nil {
		return
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return
	}
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if userID == "" {
		return
	}
	// Seed the event_scope_id if provided, ensuring refreshed events carry the tab scope
	// even when no user message has been sent yet (e.g., after app restart).
	if len(tabID) > 0 && strings.TrimSpace(tabID[0]) != "" {
		a.sessionEventScopeIDs.Store(userID, strings.TrimSpace(tabID[0]))
	}
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return
	}
	wf := handler.getWorkflowV2()
	if wf == nil {
		return
	}
	// Try active workflow first, then load from store (may be completed).
	state := wf.machine.GetActive(userID)
	if state == nil {
		state, _ = wf.store.Load(userID)
	}
	if state == nil {
		return
	}
	// Re-emit full progress (phases + phase_outputs + current_phase).
	// Emit phase_update directly instead of through emitWorkflowV2Progress to avoid
	// the suggest_maximize side effect — tab switch is not a new document event.
	handler.emitWorkflowV2ProgressPayloadOnly(userID, state)
	// Re-emit doc_update for all phases that have output, so the frontend
	// docUpdatePhaseIDsRef is correctly populated — this prevents subsequent
	// phase_update events from overwriting doc_update content during normal
	// workflow operation after a tab switch.
	// Emit directly instead of through emitDocUpdateV2 to avoid repeated store.Load.
	// Content is sanitized to remove any persisted role-prefix hallucinations.
	projectPath = workflowEventProjectPath(state)
	workflowID := state.ID
	for _, p := range state.Phases {
		if p.Output != "" {
			emitWorkflowV2Event(a, "workflow:doc_update", map[string]interface{}{
				"phase_id":       p.ID,
				"content":        stripRolePrefixHallucination(p.Output),
				"project_path":   projectPath,
				"workflow_id":    workflowID,
				"event_scope_id": a.getEventScopeID(userID),
			})
		}
	}
}

func (a *App) GetUserHomeDir() string {
	if a.testHomeDir != "" {
		return a.testHomeDir
	}
	home, _ := os.UserHomeDir()
	return home
}

// GetDataDir returns the persistent data directory that
// survives uninstalls and is easy to back up / transfer.
// When data_dir is configured, returns <data_dir>/data; otherwise ~/.maclaw/data.
func (a *App) GetDataDir() string {
	return filepath.Join(a.getMaclawBaseDir(), "data")
}

// getEventScopeID returns the event_scope_id (tab ID) for a given userID.
// Returns empty string if no scope has been registered for this session.
func (a *App) getEventScopeID(userID string) string {
	if v, ok := a.sessionEventScopeIDs.Load(userID); ok {
		return v.(string)
	}
	return ""
}

// sessionSearchDBPath returns the path to the session search FTS5 database.
func (a *App) sessionSearchDBPath() string {
	return filepath.Join(a.GetDataDir(), "session_search.db")
}

// userModelPath returns the path to the user model JSON file.
func (a *App) userModelPath() string {
	return filepath.Join(a.GetDataDir(), "user_model.json")
}

// GetTempDir returns the temporary directory for maclaw.
// When data_dir is configured, returns <data_dir>/temp; otherwise ~/.maclaw/temp.
func (a *App) GetTempDir() string {
	tmp := filepath.Join(a.getMaclawBaseDir(), "temp")
	_ = os.MkdirAll(tmp, 0o755)
	return tmp
}

// getMaclawBaseDir returns the effective maclaw base directory for this App instance.
// It tracks the data_dir config when present; testHomeDir is only the default fallback.
func (a *App) getMaclawBaseDir() string {
	if a != nil {
		if base, ok := a.effectiveBaseDir.Load().(string); ok && strings.TrimSpace(base) != "" {
			return base
		}
	}
	if a == nil {
		return corelib.MaclawBaseDir()
	}
	if a.testHomeDir != "" {
		return filepath.Join(a.testHomeDir, ".maclaw")
	}
	return corelib.MaclawBaseDir()
}

// BrandInfo is the JSON-friendly brand information exposed to the frontend.
type BrandInfo struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	DisplayNameCN   string `json:"displayNameCN"`
	Slogan          string `json:"slogan"`
	Author          string `json:"author"`
	BusinessContact string `json:"businessContact"`
	WebsiteURL      string `json:"websiteURL"`
	GitHubURL       string `json:"githubURL"`
	IconPath        string `json:"iconPath"`
}

// GetBrandInfo returns the current brand configuration for the frontend.
func (a *App) GetBrandInfo() BrandInfo {
	b := brand.Current()
	return BrandInfo{
		ID:              b.ID,
		DisplayName:     b.DisplayName,
		DisplayNameCN:   b.DisplayNameCN,
		Slogan:          b.Slogan,
		Author:          b.Author,
		BusinessContact: b.BusinessContact,
		WebsiteURL:      b.WebsiteURL,
		GitHubURL:       b.GitHubURL,
		IconPath:        b.IconPath,
	}
}

// MigrateDataDir moves legacy ~/.cceasy/* subdirectories into ~/.maclaw/data/
// on first launch. It is safe to call multiple times (no-op once migrated).
func (a *App) MigrateDataDir() {
	home := a.GetUserHomeDir()
	oldBase := filepath.Join(home, ".cceasy")
	newBase := a.GetDataDir()

	// If old directory doesn't exist, nothing to migrate.
	if _, err := os.Stat(oldBase); os.IsNotExist(err) {
		return
	}

	// Ensure new base exists.
	_ = os.MkdirAll(newBase, 0o755)

	// Subdirectories to migrate.
	subs := []string{"files", "screenshots", "im_files", "audit", "cache", "config_backup", "skills", "tools", "node"}
	for _, sub := range subs {
		src := filepath.Join(oldBase, sub)
		dst := filepath.Join(newBase, sub)
		if _, err := os.Stat(src); err != nil {
			continue // source doesn't exist
		}
		if _, err := os.Stat(dst); err == nil {
			continue // destination already exists, skip
		}
		if err := os.Rename(src, dst); err != nil {
			log.Printf("[MigrateDataDir] failed to move %s -%s: %v", src, dst, err)
		} else {
			log.Printf("[MigrateDataDir] migrated %s -%s", src, dst)
		}
	}

	// Fix .cmd/.bat shim files that contain hardcoded old paths.
	toolsDir := filepath.Join(newBase, "tools")
	if entries, err := os.ReadDir(toolsDir); err == nil {
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".cmd" && ext != ".bat" {
				continue
			}
			p := filepath.Join(toolsDir, e.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			content := string(data)
			if !strings.Contains(content, ".cceasy") {
				continue
			}
			// Replace both absolute path and %USERPROFILE% forms.
			fixed := content
			fixed = strings.ReplaceAll(fixed, oldBase, newBase)
			fixed = strings.ReplaceAll(fixed, `%USERPROFILE%\.cceasy\tools`, strings.ReplaceAll(filepath.Join(newBase, "tools"), home, `%USERPROFILE%`))
			fixed = strings.ReplaceAll(fixed, `%USERPROFILE%\.cceasy`, strings.ReplaceAll(newBase, home, `%USERPROFILE%`))
			if fixed != content {
				_ = os.WriteFile(p, []byte(fixed), 0644)
				log.Printf("[MigrateDataDir] fixed old path in %s", e.Name())
			}
		}
	}

	// Remove old .cceasy directory if empty.
	if entries, err := os.ReadDir(oldBase); err == nil && len(entries) == 0 {
		_ = os.Remove(oldBase)
		log.Printf("[MigrateDataDir] removed empty legacy directory %s", oldBase)
	}
}

func (a *App) GetLocalCacheDir() string {
	// Use shorter path to avoid Windows 260 character path limit
	// npm's _cacache directory structure can create very long paths
	return filepath.Join(a.GetDataDir(), "cache")
}
func (a *App) GetCurrentProjectPath() string {
	config, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	for _, p := range config.Projects {
		if p.Id == config.CurrentProject {
			return p.Path
		}
	}
	if len(config.Projects) > 0 {
		return config.Projects[0].Path
	}
	// No configured project. Do NOT use os.Getwd() — on Windows that is the
	// maclaw installation directory (e.g. C:\Program Files\MaClaw\), which is
	// not a valid project directory and causes search tools to scan system files.
	// Return empty and let the caller handle the fallback (EffectiveWorkspaceDir
	// or taskDir/workspace/).
	return ""
}
func (a *App) getClaudeConfigPaths(projectDir string, instanceID string) (string, string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "claude", instanceID)
		settings := filepath.Join(dir, "settings.json")
		legacy := filepath.Join(dir, "claude.json")
		return dir, settings, legacy
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude")
	settings := filepath.Join(dir, "settings.json")
	legacy := filepath.Join(home, ".claude.json")
	return dir, settings, legacy
}
func (a *App) getCodexConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "codex", instanceID)
		auth := filepath.Join(dir, "auth.json")
		return dir, auth
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".codex")
	auth := filepath.Join(dir, "auth.json")
	return dir, auth
}
func (a *App) getOpencodeConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "opencode", instanceID)
		config := filepath.Join(dir, "opencode.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "opencode")
	config := filepath.Join(dir, "opencode.json")
	return dir, config
}
func (a *App) getIFlowConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "iflow", instanceID)
		config := filepath.Join(dir, "settings.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".iflow")
	config := filepath.Join(dir, "settings.json")
	return dir, config
}

// ---------------------------------------------------------------------------
// Native config backup / restore
// ---------------------------------------------------------------------------
// When switching to a third-party provider we need to clear the tool's native
// config directory (e.g. ~/.claude, ~/.codex) so that env-var-based
// credentials take effect.  Instead of deleting the directory outright we move
// it to a backup location (~/.maclaw/data/config_backup/<tool>/) so that switching
// back to the original provider can restore it without forcing the user to
// re-authenticate.

// toolNativeConfigPaths returns the native config directory and any extra
// legacy files that belong to the tool's original-provider configuration.
func (a *App) toolNativeConfigPaths(tool string) (dir string, extras []string) {
	home := a.GetUserHomeDir()
	switch normalizeRemoteToolNameKind(tool) {
	case remoteToolNameClaude:
		return filepath.Join(home, ".claude"),
			[]string{
				filepath.Join(home, ".claude.json"),
				filepath.Join(home, ".claude.json.backup"),
			}
	case remoteToolNameCodex:
		return filepath.Join(home, ".codex"), nil
	case remoteToolNameOpencode:
		return filepath.Join(home, ".config", "opencode"), nil
	case remoteToolNameIFlow:
		return filepath.Join(home, ".iflow"), nil
	case remoteToolNameKilo:
		return filepath.Join(home, ".kilocode", "cli"), nil
	default:
		return filepath.Join(home, "."+strings.ToLower(tool)), nil
	}
}

// configBackupDir returns <active data dir>/config_backup/<tool>.
func (a *App) configBackupDir(tool string) string {
	return filepath.Join(a.GetDataDir(), "config_backup", strings.ToLower(tool))
}

// backupToolNativeConfig moves the tool's native config directory (and any
// legacy files) into the backup location.  If a backup already exists it is
// left untouched so we never overwrite a good backup with an empty directory.
func (a *App) backupToolNativeConfig(tool string) {
	srcDir, extras := a.toolNativeConfigPaths(tool)
	backupBase := a.configBackupDir(tool)

	// Only backup if the source directory actually exists and is non-empty.
	if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
		backupDirDst := filepath.Join(backupBase, filepath.Base(srcDir))
		// Don't overwrite an existing backup -it may contain a valid login.
		if _, err := os.Stat(backupDirDst); os.IsNotExist(err) {
			os.MkdirAll(backupBase, 0755)
			if err := os.Rename(srcDir, backupDirDst); err != nil {
				// Rename can fail across filesystems; fall back to copy.
				a.copyDir(srcDir, backupDirDst)
				os.RemoveAll(srcDir)
			}
			a.log(fmt.Sprintf("[config-backup] backed up %s -> %s", srcDir, backupDirDst))
		} else {
			// Backup already exists, just remove the source.
			os.RemoveAll(srcDir)
			a.log(fmt.Sprintf("[config-backup] backup already exists for %s, removed source", tool))
		}
	}

	// Handle legacy extra files the same way.
	for _, extra := range extras {
		if _, err := os.Stat(extra); err == nil {
			backupPath := filepath.Join(backupBase, filepath.Base(extra))
			if _, err := os.Stat(backupPath); os.IsNotExist(err) {
				os.MkdirAll(backupBase, 0755)
				os.Rename(extra, backupPath)
				a.log(fmt.Sprintf("[config-backup] backed up %s", extra))
			} else {
				os.Remove(extra)
			}
		}
	}
}

// restoreToolNativeConfig restores a previously backed-up native config
// directory (and legacy files) back to their original locations.
func (a *App) restoreToolNativeConfig(tool string) {
	srcDir, extras := a.toolNativeConfigPaths(tool)
	backupBase := a.configBackupDir(tool)

	backupDirSrc := filepath.Join(backupBase, filepath.Base(srcDir))
	if info, err := os.Stat(backupDirSrc); err == nil && info.IsDir() {
		// Remove any current config that might have been written by a
		// third-party provider so the restore is clean.
		os.RemoveAll(srcDir)
		if err := os.Rename(backupDirSrc, srcDir); err != nil {
			a.copyDir(backupDirSrc, srcDir)
			os.RemoveAll(backupDirSrc)
		}
		a.log(fmt.Sprintf("[config-restore] restored %s -> %s", backupDirSrc, srcDir))
	}

	// Restore legacy extra files.
	for _, extra := range extras {
		backupPath := filepath.Join(backupBase, filepath.Base(extra))
		if _, err := os.Stat(backupPath); err == nil {
			os.Remove(extra) // remove any stale version
			os.Rename(backupPath, extra)
			a.log(fmt.Sprintf("[config-restore] restored %s", extra))
		}
	}

	// Clean up the backup directory if it's now empty.
	if entries, err := os.ReadDir(backupBase); err == nil && len(entries) == 0 {
		os.Remove(backupBase)
	}
}

// copyDir recursively copies src to dst (best-effort, used as fallback when
// os.Rename fails across filesystems).
func (a *App) copyDir(src, dst string) {
	filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func (a *App) clearOpencodeConfig() {
	a.backupToolNativeConfig("opencode")
	a.log("Cleared Opencode configuration directory (backed up)")
}
func (a *App) clearIFlowConfig() {
	a.backupToolNativeConfig("iflow")
	a.log("Cleared iFlow configuration directory (backed up)")
}
func (a *App) getKiloConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "kilocode", "cli", instanceID)
		config := filepath.Join(dir, "config.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".kilocode", "cli")
	config := filepath.Join(dir, "config.json")
	return dir, config
}
func (a *App) clearKiloConfig() {
	a.backupToolNativeConfig("kilo")
	a.log("Cleared Kilo Code configuration file (backed up)")
}
func (a *App) clearEnvVars() {
	vars := []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "CLAUDE_CODE_MAX_OUTPUT_TOKENS",
		"MAX_THINKING_TOKENS", "API_TIMEOUT_MS",
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "WIRE_API",
		"OPENCODE_API_KEY", "OPENCODE_BASE_URL",
		"CODEBUDDY_API_KEY", "CODEBUDDY_BASE_URL", "CODEBUDDY_CODE_MAX_OUTPUT_TOKENS",
		"IFLOW_API_KEY", "IFLOW_BASE_URL",
		"KILO_API_KEY", "KILO_BASE_URL", "KILO_MODEL",
	}
	for _, v := range vars {
		os.Unsetenv(v)
	}
}

func effectiveToolWireAPI(toolName string, model corelib.ModelConfig) string {
	wireAPI := strings.TrimSpace(model.WireApi)
	if wireAPI != "" {
		return wireAPI
	}
	if strings.EqualFold(toolName, "claude") {
		trimmedURL := strings.TrimRight(strings.TrimSpace(model.ModelUrl), "/")
		if trimmedURL == "" || strings.HasSuffix(trimmedURL, "/anthropic") || strings.Contains(trimmedURL, "/anthropic/") {
			return "anthropic"
		}
		return "anthropic"
	}
	if strings.EqualFold(toolName, "codex") {
		return "responses"
	}
	return ""
}

func (a *App) syncToClaudeSettings(config corelib.AppConfig, projectDir string, instanceID string) error {
	var selectedModel *corelib.ModelConfig
	for _, m := range config.Claude.Models {
		if m.ModelName == config.Claude.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected model not found")
	}
	dir, settingsPath, legacyPath := a.getClaudeConfigPaths(projectDir, instanceID)
	if selectedModel.IsBuiltin {
		// Surgical cleanup: remove third-party env fields from settings.json,
		// preserving all other user state (conversations, plugins, hooks, etc.).
		if err := configfile.ClearClaudeThirdPartySettings(); err != nil {
			log.Printf("[syncToClaudeSettings] Claude: ClearClaudeThirdPartySettings error: %v", err)
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	settings := make(map[string]interface{})
	env := make(map[string]string)
	wireAPI := effectiveToolWireAPI("claude", *selectedModel)
	if strings.TrimSpace(selectedModel.WireApi) != "" && !corelib.IsAnthropicWireAPI(wireAPI) {
		return fmt.Errorf("claude provider %q must use anthropic wire_api", selectedModel.ModelName)
	}
	// Exclusively use AUTH_TOKEN for custom providers
	env["ANTHROPIC_AUTH_TOKEN"] = selectedModel.ApiKey
	env["CLAUDE_CODE_USE_COLORS"] = "true"
	env["MAX_THINKING_TOKENS"] = "31999"
	switch normalizeModelProviderKind(selectedModel.ModelName) {
	case modelProviderKimi:
		env["ANTHROPIC_BASE_URL"] = "https://api.kimi.com/coding"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
	case modelProviderGLM:
		env["ANTHROPIC_BASE_URL"] = "https://open.bigmodel.cn/api/anthropic"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
		settings["permissions"] = map[string]string{"defaultMode": "dontAsk"}
	case modelProviderDoubao:
		env["ANTHROPIC_BASE_URL"] = "https://ark.cn-beijing.volces.com/api/coding"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
	case modelProviderXFYun:
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "astron-code-latest"
		}
		env["ANTHROPIC_BASE_URL"] = "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case modelProviderMiniMax:
		env["ANTHROPIC_BASE_URL"] = "https://api.minimaxi.com/anthropic"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_SMALL_FAST_MODEL"] = selectedModel.ModelId
		env["API_TIMEOUT_MS"] = "3000000"
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	case modelProviderDeepSeek:
		env["ANTHROPIC_BASE_URL"] = "https://api.deepseek.com/anthropic"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "deepseek-chat"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case modelProviderGACCode:
		env["ANTHROPIC_BASE_URL"] = "https://gaccode.com/claudecode"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "sonnet"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case modelProviderTencent:
		env["ANTHROPIC_BASE_URL"] = "https://api.lkeap.cloud.tencent.com/coding/anthropic"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "glm-5"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case modelProviderAliyun:
		env["ANTHROPIC_BASE_URL"] = "https://coding.dashscope.aliyuncs.com/apps/anthropic"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "glm-5"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case modelProviderQianfan:
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "qianfan-code-latest"
		}
		env["ANTHROPIC_BASE_URL"] = "https://qianfan.baidubce.com/anthropic/coding"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
		env["ANTHROPIC_SMALL_FAST_MODEL"] = modelId
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		env["API_TIMEOUT_MS"] = "600000"
		settings["permissions"] = map[string][]string{
			"allow": {},
			"deny":  {},
		}
	case modelProviderCodegen:
		// CodeGen: use the configured Anthropic-compatible provider directly.
		// Normalize "auto" → "qax-codegen/Auto" since the Anthropic endpoint
		// requires the canonical model name.
		env["ANTHROPIC_BASE_URL"] = selectedModel.ModelUrl
		modelId := corelib.NormalizeCodeGenModel(selectedModel.ModelId)
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	default:
		env["ANTHROPIC_BASE_URL"] = selectedModel.ModelUrl
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
	}
	settings["env"] = env
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	// Check if settings file needs update
	if existingData, err := os.ReadFile(settingsPath); err == nil {
		if bytes.Equal(existingData, data) {
			// Settings file is already up to date, skip main settings write
			// But still need to update .claude.json for API key responses
			goto updateLegacyJson
		}
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return err
	}
	// 2. Sync to ~/.claude.json for customApiKeyResponses
updateLegacyJson:
	var claudeJson map[string]interface{}
	if jsonData, err := os.ReadFile(legacyPath); err == nil {
		json.Unmarshal(jsonData, &claudeJson)
	}
	if claudeJson == nil {
		claudeJson = make(map[string]interface{})
	}
	claudeJson["customApiKeyResponses"] = map[string]interface{}{
		"approved": []string{selectedModel.ApiKey},
		"rejected": []string{},
	}
	claudeJson["hasCompletedOnboarding"] = true
	data2, err := json.MarshalIndent(claudeJson, "", "  ")
	if err != nil {
		return err
	}
	// Check if legacy file needs update
	if existingData, err := os.ReadFile(legacyPath); err == nil {
		if bytes.Equal(existingData, data2) {
			return nil
		}
	}
	return os.WriteFile(legacyPath, data2, 0644)
}

func (a *App) syncToCodexSettings(config corelib.AppConfig, projectDir string, instanceID string) error {
	var selectedModel *corelib.ModelConfig
	for _, m := range config.Codex.Models {
		if m.ModelName == config.Codex.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected codex model not found")
	}
	dir, _ := a.getCodexConfigPaths(projectDir, instanceID)
	if selectedModel.IsBuiltin {
		// Surgical cleanup: remove third-party auth and provider config,
		// preserving MCP servers, profiles, and other user settings.
		if err := configfile.ClearCodexThirdPartySettingsAt(dir); err != nil {
			log.Printf("[syncToCodexSettings] Codex: ClearCodexThirdPartySettings error: %v", err)
			return err
		}
		return nil
	}
	return configfile.WriteCodexConfigAtWithClientName(
		dir,
		selectedModel.ApiKey,
		normalizedOpenAICompatibleToolBaseURL(remoteToolNameCodex.String(), *selectedModel),
		selectedModel.ModelId,
		selectedModel.ModelName,
		effectiveToolWireAPI("codex", *selectedModel),
		codeGenClientNameForModelConfig(config, *selectedModel),
	)
}

func codeGenClientNameForModelConfig(config corelib.AppConfig, selectedModel corelib.ModelConfig) string {
	if !corelib.IsCodeGenURL(selectedModel.ModelUrl) {
		return ""
	}
	if strings.TrimSpace(selectedModel.AgentType) != "" {
		return selectedModel.AgentType
	}
	modelURL := strings.TrimRight(strings.TrimSpace(selectedModel.ModelUrl), "/")
	modelName := strings.TrimSpace(selectedModel.ModelName)
	for _, provider := range config.MaclawLLMProviders {
		providerURL := strings.TrimRight(strings.TrimSpace(provider.URL), "/")
		providerName := strings.TrimSpace(provider.Name)
		if !corelib.IsCodeGenURL(providerURL) && !strings.EqualFold(providerName, codegenProviderName) {
			continue
		}
		if strings.EqualFold(providerURL, modelURL) || strings.EqualFold(providerName, modelName) {
			return provider.UserAgent()
		}
	}
	return corelib.CodeGenClientName
}

func openAICompatibleToolUserAgent(toolName string, model corelib.ModelConfig) string {
	if agent := strings.TrimSpace(model.AgentType); agent != "" {
		return agent
	}
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case remoteToolNameKilo.String():
		return "Kilo Code"
	case remoteToolNameOpencode.String(), remoteToolNameCodex.String(), remoteToolNameIFlow.String(), remoteToolNameCodeBuddy.String():
		return "OpenCode"
	default:
		return "OpenCode"
	}
}

func normalizedOpenAICompatibleToolBaseURL(toolName string, model corelib.ModelConfig) string {
	return strings.TrimRight(corelib.NormalizeGLMCodingPlanOpenAIBaseURL(strings.TrimSpace(model.ModelUrl), openAICompatibleToolUserAgent(toolName, model)), "/")
}

func (a *App) syncToOpencodeSettings(config corelib.AppConfig, projectDir string, instanceID string) error {
	var selectedModel *corelib.ModelConfig
	for _, m := range config.Opencode.Models {
		if m.ModelName == config.Opencode.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected opencode model not found")
	}
	dir, configPath := a.getOpencodeConfigPaths(projectDir, instanceID)
	if selectedModel.IsBuiltin {
		a.clearOpencodeConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	baseUrl := normalizedOpenAICompatibleToolBaseURL(remoteToolNameOpencode.String(), *selectedModel)
	modelId := selectedModel.ModelId
	providerKind := normalizeModelProviderKind(selectedModel.ModelName)
	providerName := strings.TrimSpace(selectedModel.ModelName)
	if providerName == "" && providerKind != modelProviderUnknown {
		providerName = string(providerKind)
	}
	// Fallback logic for Opencode (align with Codex providers)
	if modelId == "" {
		switch providerKind {
		case modelProviderDeepSeek:
			modelId = "deepseek-chat"
			if baseUrl == "" {
				baseUrl = "https://api.deepseek.com/v1"
			}
		case modelProviderGLM:
			modelId = "GLM-5.2"
			if baseUrl == "" {
				baseUrl = "https://open.bigmodel.cn/api/coding/paas/v4"
			}
		case modelProviderDoubao:
			modelId = "doubao-seed-code-preview-latest"
			if baseUrl == "" {
				baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
			}
		case modelProviderXFYun:
			modelId = "astron-code-latest"
			if baseUrl == "" {
				baseUrl = "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2"
			}
		case modelProviderKimi:
			modelId = "kimi-for-coding"
			if baseUrl == "" {
				baseUrl = "https://api.kimi.com/coding/v1"
			}
		case modelProviderMiniMax:
			modelId = "MiniMax-M2.1"
			if baseUrl == "" {
				baseUrl = "https://api.minimaxi.com/v1"
			}
		case modelProviderAliyun:
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://coding.dashscope.aliyuncs.com/apps/anthropic/v1"
			}
		case modelProviderTencent:
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://api.lkeap.cloud.tencent.com/coding/v3"
			}
		default:
			modelId = "opencode-1.0"
			if baseUrl == "" {
				baseUrl = "https://api.aicodemirror.com/api/opencode/v1"
			}
		}
	}
	// Build the JSON structure
	opencodeJson := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]interface{}{
			"myprovider": map[string]interface{}{
				"npm":  "@ai-sdk/openai-compatible",
				"name": providerName,
				"options": map[string]interface{}{
					"baseURL":   baseUrl,
					"apiKey":    selectedModel.ApiKey,
					"maxTokens": 8192,
				},
				"models": map[string]interface{}{
					modelId: map[string]interface{}{
						"name": modelId,
						"limit": map[string]interface{}{
							"context": 8192,
							"output":  8192,
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(opencodeJson, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
func (a *App) syncToIFlowSettings(config corelib.AppConfig, projectDir string, instanceID string) error {
	var selectedModel *corelib.ModelConfig
	for _, m := range config.IFlow.Models {
		if m.ModelName == config.IFlow.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected iflow model not found")
	}
	dir, configPath := a.getIFlowConfigPaths(projectDir, instanceID)
	if selectedModel.IsBuiltin {
		a.clearIFlowConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Prepare defaults
	baseUrl := normalizedOpenAICompatibleToolBaseURL(remoteToolNameIFlow.String(), *selectedModel)
	modelId := selectedModel.ModelId
	providerKind := normalizeModelProviderKind(selectedModel.ModelName)
	// Fallback logic for iFlow (align with Codex providers)
	if modelId == "" {
		switch providerKind {
		case modelProviderDeepSeek:
			modelId = "deepseek-chat"
			if baseUrl == "" {
				baseUrl = "https://api.deepseek.com/v1"
			}
		case modelProviderGLM:
			modelId = "GLM-5.2"
			if baseUrl == "" {
				baseUrl = "https://open.bigmodel.cn/api/coding/paas/v4"
			}
		case modelProviderDoubao:
			modelId = "doubao-seed-code-preview-latest"
			if baseUrl == "" {
				baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
			}
		case modelProviderKimi:
			modelId = "kimi-for-coding"
			if baseUrl == "" {
				baseUrl = "https://api.kimi.com/coding/v1"
			}
		case modelProviderMiniMax:
			modelId = "MiniMax-M2.1"
			if baseUrl == "" {
				baseUrl = "https://api.minimaxi.com/v1"
			}
		case modelProviderAliyun:
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://coding.dashscope.aliyuncs.com/apps/anthropic/v1"
			}
		case modelProviderTencent:
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://api.lkeap.cloud.tencent.com/coding/v3"
			}
		default:
			modelId = "gpt-4o"
		}
	}
	// Build the JSON structure for settings.json
	settings := map[string]string{
		"selectedAuthType": "openai-compatible",
		"apiKey":           selectedModel.ApiKey,
		"baseUrl":          baseUrl,
		"modelName":        modelId,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
func (a *App) syncToKiloSettings(config corelib.AppConfig, projectDir string, instanceID string) error {
	var selectedModel *corelib.ModelConfig
	for _, m := range config.Kilo.Models {
		if m.ModelName == config.Kilo.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected kilo model not found")
	}
	dir, configPath := a.getKiloConfigPaths(projectDir, instanceID)
	if selectedModel.IsBuiltin {
		a.clearKiloConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Read existing config if it exists
	var kiloConfig map[string]interface{}
	existingData, err := os.ReadFile(configPath)
	if err == nil {
		// File exists, parse it
		if err := json.Unmarshal(existingData, &kiloConfig); err != nil {
			// If parsing fails, create new config
			kiloConfig = make(map[string]interface{})
		}
	} else {
		// File doesn't exist, create new config
		kiloConfig = make(map[string]interface{})
	}
	// Prepare provider configuration
	baseUrl := normalizedOpenAICompatibleToolBaseURL(remoteToolNameKilo.String(), *selectedModel)
	modelId := selectedModel.ModelId
	providerKind := normalizeModelProviderKind(selectedModel.ModelName)
	// Fallback logic for common providers
	if modelId == "" {
		switch providerKind {
		case modelProviderDeepSeek:
			modelId = "deepseek-chat"
			if baseUrl == "" {
				baseUrl = "https://api.deepseek.com/v1"
			}
		case modelProviderGLM:
			modelId = "GLM-5.2"
			if baseUrl == "" {
				baseUrl = "https://open.bigmodel.cn/api/coding/paas/v4"
			}
		case modelProviderDoubao:
			modelId = "doubao-seed-code-preview-latest"
			if baseUrl == "" {
				baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
			}
		case modelProviderKimi:
			modelId = "kimi-for-coding"
			if baseUrl == "" {
				baseUrl = "https://api.kimi.com/coding/v1"
			}
		case modelProviderMiniMax:
			modelId = "MiniMax-M2.1"
			if baseUrl == "" {
				baseUrl = "https://api.minimaxi.com/v1"
			}
		case modelProviderXiaomi:
			modelId = "mimo-v2-flash"
			if baseUrl == "" {
				baseUrl = "https://api.xiaomimimo.com/v1"
			}
		case modelProviderAliyun:
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://coding.dashscope.aliyuncs.com/apps/anthropic/v1"
			}
		default:
			modelId = "gpt-4o"
		}
	}
	// Build provider object
	provider := map[string]interface{}{
		"id":            "default",
		"provider":      "openai",
		"openAiApiKey":  selectedModel.ApiKey,
		"openAiModelId": modelId,
		"openAiBaseUrl": baseUrl,
	}
	// Update providers array
	kiloConfig["providers"] = []interface{}{provider}
	// Write config file
	data, err := json.MarshalIndent(kiloConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func (a *App) syncToCodeBuddySettings(config corelib.AppConfig, projectPath string) error {
	if projectPath == "" {
		projectPath = a.GetCurrentProjectPath()
	}
	if projectPath == "" {
		return nil
	}
	cbDir := filepath.Join(projectPath, ".codebuddy")
	if err := os.MkdirAll(cbDir, 0755); err != nil {
		return err
	}
	cbFilePath := filepath.Join(cbDir, "models.json")
	var cbModels []corelib.CodeBuddyModel
	var availableModelIds []string
	for _, m := range config.CodeBuddy.Models {
		// Only sync the currently selected model
		if m.ModelName != config.CodeBuddy.CurrentModel {
			continue
		}
		if m.IsBuiltin {
			continue
		}
		vendor := strings.ToLower(m.ModelName)
		vendorKind := normalizeModelProviderKind(m.ModelName)
		idStr := m.ModelId
		if idStr == "" {
			switch vendorKind {
			case modelProviderDeepSeek:
				idStr = "deepseek-chat"
			case modelProviderGLM:
				idStr = "GLM-5.2"
			case modelProviderDoubao:
				idStr = "doubao-seed-code-preview-latest"
			case modelProviderKimi:
				idStr = "kimi-for-coding"
			case modelProviderMiniMax:
				idStr = "MiniMax-M2.1"
			default:
				idStr = vendor + "-model"
			}
		}
		modelIds := strings.Split(idStr, ",")
		modelUrl := strings.TrimSpace(m.ModelUrl)
		if modelUrl != "" {
			modelUrl = llmcompat.BuildOpenAIChatCompletionsEndpoint(corelib.NormalizeGLMCodingPlanOpenAIBaseURL(modelUrl, openAICompatibleToolUserAgent(remoteToolNameCodeBuddy.String(), m)))
		}
		for _, id := range modelIds {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			availableModelIds = append(availableModelIds, id)
			cbModels = append(cbModels, corelib.CodeBuddyModel{
				Id:               id,
				Name:             id,
				Vendor:           vendor,
				ApiKey:           m.ApiKey,
				MaxInputTokens:   200000,
				MaxOutputTokens:  8192,
				Url:              modelUrl,
				SupportsToolCall: true,
				SupportsImages:   true,
			})
		}
	}
	cbConfig := corelib.CodeBuddyFileConfig{
		Models:          cbModels,
		AvailableModels: availableModelIds,
	}
	data, err := json.MarshalIndent(cbConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cbFilePath, data, 0644)
}
func getBaseUrl(selectedModel *corelib.ModelConfig) string {
	// If user provided a URL for the selected model, always prefer it.
	if selectedModel.ModelUrl != "" {
		return selectedModel.ModelUrl
	}
	// Otherwise, fall back to hardcoded defaults for known providers that have them.
	baseUrl := "" // Default to empty string
	switch normalizeModelProviderKind(selectedModel.ModelName) {
	case modelProviderKimi:
		baseUrl = "https://api.kimi.com/coding"
	case modelProviderGLM:
		baseUrl = "https://open.bigmodel.cn/api/anthropic"
	case modelProviderDoubao:
		baseUrl = "https://ark.cn-beijing.volces.com/api/coding"
	case modelProviderXFYun:
		baseUrl = "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic"
	case modelProviderMiniMax:
		baseUrl = "https://api.minimaxi.com/anthropic"
	case modelProviderDeepSeek:
		baseUrl = "https://api.deepseek.com/anthropic"
	case modelProviderGACCode:
		baseUrl = "https://gaccode.com/claudecode"
	case modelProviderQianfan:
		baseUrl = "https://qianfan.baidubce.com/anthropic/coding"
	}
	return baseUrl
}
func (a *App) LaunchTool(toolName string, yoloMode bool, adminMode bool, pythonProject bool, pythonEnv string, projectDir string, useProxy bool) error {
	a.log(fmt.Sprintf("LaunchTool called: %s, yolo=%v, admin=%v, py=%v, pyenv=%s, dir=%s, proxy=%v",
		toolName, yoloMode, adminMode, pythonProject, pythonEnv, projectDir, useProxy))
	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}
	projectDir = normalizeProjectSessionPath(projectDir)
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(remoteLaunchPolicyOwnerIDForProject(RemoteLaunchSourceDesktop, projectDir), remoteSessionStartPolicyToolName, map[string]interface{}{"tool": toolName, "project_dir": projectDir, "launch_source": "desktop"}); err != nil {
		a.log(fmt.Sprintf("LaunchTool blocked by workflow policy: %v", err))
		return err
	}
	a.log(fmt.Sprintf("Launching %s...", toolName))

	// Generate unique instance ID for this launch (timestamp-based)
	instanceID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Only process Python environment if pythonProject is true
	if pythonProject && pythonEnv != "" && pythonEnv != "None (Default)" {
		a.log(fmt.Sprintf("Python project: using Python environment: %s", pythonEnv))
	} else {
		// Clear pythonEnv if not a Python project
		pythonEnv = ""
	}
	config, err := a.LoadConfig()
	if err != nil {
		a.log("Error loading config: " + err.Error())
		return err
	}
	launchToolKind := normalizeRemoteToolNameKind(toolName)
	var toolCfg corelib.ToolConfig
	var envKey, envBaseUrl string
	var binaryName string
	switch launchToolKind {
	case remoteToolNameClaude:
		toolCfg = config.Claude
		envKey = "ANTHROPIC_AUTH_TOKEN"
		envBaseUrl = "ANTHROPIC_BASE_URL"
		binaryName = "claude"
	case remoteToolNameCodex:
		toolCfg = config.Codex
		envKey = "OPENAI_API_KEY"
		envBaseUrl = "OPENAI_BASE_URL"
		binaryName = "codex"
	case remoteToolNameIFlow:
		toolCfg = config.IFlow
		envKey = "IFLOW_API_KEY"
		envBaseUrl = "IFLOW_BASE_URL"
		binaryName = "iflow"
	case remoteToolNameKilo:
		toolCfg = config.Kilo
		envKey = "KILO_API_KEY"
		envBaseUrl = "KILO_BASE_URL"
		binaryName = "kilo"
	case remoteToolNameOpencode:
		toolCfg = config.Opencode
		envKey = "OPENCODE_API_KEY"
		envBaseUrl = "OPENCODE_BASE_URL"
		binaryName = "opencode"
	case remoteToolNameCodeBuddy:
		toolCfg = config.CodeBuddy
		envKey = "CODEBUDDY_API_KEY"
		envBaseUrl = "CODEBUDDY_BASE_URL"
		binaryName = "codebuddy"
	default:
		// Check OEM extra tools from brand config
		extraTool := findExtraTool(launchToolKind.String())
		if extraTool == nil {
			return fmt.Errorf("unsupported tool: %s", toolName)
		}
		// Load tool config from ExtraToolConfigs map
		if config.ExtraToolConfigs != nil {
			if tc, ok := config.ExtraToolConfigs[extraTool.ConfigKey]; ok {
				toolCfg = tc
			}
		}
		envKey = "OPENAI_API_KEY"
		envBaseUrl = "OPENAI_BASE_URL"
		binaryName = extraTool.Name
	}
	var selectedModel *corelib.ModelConfig
	for _, m := range toolCfg.Models {
		if m.ModelName == toolCfg.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil || toolCfg.CurrentModel == "" {
		title := "\u63d0\u793a"
		message := "\u8bf7\u5148\u9009\u62e9\u670d\u52a1\u5546"
		if normalizeAppLanguageKind(a.CurrentLanguage).IsEnglish() {
			title = "Notice"
			message = "Please select a provider first."
		}
		a.ShowMessage(title, message)
		return fmt.Errorf("please select a provider first")
	}

	// Remote desktop launches write native configs exactly once inside
	// buildRemoteLaunchSpec. Handle them before the local env/write path so we
	// do not rewrite ~/.codex / ~/.claude twice.
	launchMode := normalizeLaunchModeKind(config.DefaultLaunchMode)
	remoteCapableTool := launchToolKind.IsDesktopRemoteLaunchCapableBuiltin() || findExtraTool(launchToolKind.String()) != nil
	if launchMode.IsRemote() && config.RemoteEnabled && remoteCapableTool {
		return a.launchToolRemoteSession(toolName, config, yoloMode, adminMode, pythonEnv, projectDir, useProxy, launchToolKind)
	}

	// Ensure ActiveTool is set correctly for syncToSystemEnv
	config.ActiveTool = launchToolKind.String()
	a.syncToSystemEnv(config)
	// Create env map for passing to batch script
	env := make(map[string]string)
	// Proxy settings
	if useProxy && goruntime.GOOS != "windows" {
		var proxyHost, proxyPort, proxyUsername, proxyPassword string
		// Get proxy configuration (matching project path > global default)
		var targetProj *corelib.ProjectConfig
		for i := range config.Projects {
			if config.Projects[i].Path == projectDir {
				targetProj = &config.Projects[i]
				break
			}
		}
		// Fallback to CurrentProject if path match not found
		if targetProj == nil {
			for i := range config.Projects {
				if config.Projects[i].Id == config.CurrentProject {
					targetProj = &config.Projects[i]
					break
				}
			}
		}
		if targetProj != nil {
			proxyHost = targetProj.ProxyHost
			proxyPort = targetProj.ProxyPort
			proxyUsername = targetProj.ProxyUsername
			proxyPassword = targetProj.ProxyPassword
		}
		// Use global default if project not configured
		if proxyHost == "" {
			proxyHost = config.DefaultProxyHost
			proxyPort = config.DefaultProxyPort
			proxyUsername = config.DefaultProxyUsername
			proxyPassword = config.DefaultProxyPassword
		}
		if proxyHost != "" && proxyPort != "" {
			var proxyURL string
			if proxyUsername != "" && proxyPassword != "" {
				proxyURL = fmt.Sprintf("http://%s:%s@%s:%s",
					proxyUsername, proxyPassword, proxyHost, proxyPort)
			} else {
				proxyURL = fmt.Sprintf("http://%s:%s", proxyHost, proxyPort)
			}
			// Set proxy environment variables (only in env map, not main process)
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
			a.log(fmt.Sprintf("Proxy enabled: %s:%s", proxyHost, proxyPort))
		}
	}
	// Shared launch-env builders own native config writes for the main tools so
	// local desktop launch matches remote launch (normalization, surgical
	// update/clear, CodeGen headers). CodeBuddy / OEM extras keep the legacy path.
	switch launchToolKind {
	case remoteToolNameClaude, remoteToolNameCodex, remoteToolNameOpencode, remoteToolNameIFlow, remoteToolNameKilo:
		built, err := a.buildRemoteLaunchEnvForTool(launchToolKind.String(), config, selectedModel, projectDir, useProxy, persistNativeToolConfig)
		if err != nil {
			a.log(fmt.Sprintf("prepare %s launch env failed: %v", toolName, err))
			return err
		}
		for k, v := range built {
			env[k] = v
		}
		if !selectedModel.IsBuiltin {
			// Local desktop extras not required by remote session adapters.
			if launchToolKind == remoteToolNameCodex {
				env["WIRE_API"] = "responses"
				if selectedModel.ModelId != "" {
					env["OPENAI_MODEL"] = selectedModel.ModelId
				}
			}
			// Instance-scoped config files for multi-window local launches.
			switch launchToolKind {
			case remoteToolNameOpencode:
				if err := a.syncToOpencodeSettings(config, projectDir, instanceID); err != nil {
					log.Printf("Opencode: sync instance settings failed: %v", err)
				}
			case remoteToolNameIFlow:
				if err := a.syncToIFlowSettings(config, projectDir, instanceID); err != nil {
					log.Printf("iFlow: sync instance settings failed: %v", err)
				}
			case remoteToolNameKilo:
				if err := a.syncToKiloSettings(config, projectDir, instanceID); err != nil {
					log.Printf("Kilo: sync instance settings failed: %v", err)
				}
			}
			a.log(fmt.Sprintf("%s: prepared provider env and native config for local launch", toolName))
		} else {
			a.log(fmt.Sprintf("Running %s in Original mode: native config restored.", toolName))
		}
	default:
		if !selectedModel.IsBuiltin {
			// --- OTHER PROVIDER MODE (CodeBuddy / OEM): SET ENV ONLY ---
			toolBaseURL := strings.TrimSpace(selectedModel.ModelUrl)
			if !launchToolKind.IsClaude() {
				toolBaseURL = normalizedOpenAICompatibleToolBaseURL(launchToolKind.String(), *selectedModel)
			}
			env[envKey] = selectedModel.ApiKey
			if toolBaseURL != "" && envBaseUrl != "" {
				env[envBaseUrl] = toolBaseURL
			}
			selectedModelProvider := normalizeModelProviderKind(selectedModel.ModelName)
			if selectedModelProvider.IsDeepSeek() {
				env["CODEBUDDY_CODE_MAX_OUTPUT_TOKENS"] = "8192"
			}
			if selectedModel.ModelId != "" {
				if findExtraTool(launchToolKind.String()) != nil {
					env["OPENAI_MODEL"] = selectedModel.ModelId
				}
			}
			if et := findExtraTool(launchToolKind.String()); et != nil && et.EnvBuilderFunc != nil {
				extraEnv := et.EnvBuilderFunc(nil, selectedModel, projectDir)
				for k, v := range extraEnv {
					env[k] = v
				}
			}
		} else {
			a.restoreToolNativeConfig(launchToolKind.String())
			a.log(fmt.Sprintf("Running %s in Original mode: native config restored.", toolName))
		}
	}

	// Ensure tool onboarding is complete for local launches so the user
	// doesn't have to confirm theme/trust/setup prompts every time.
	ensureToolOnboardingComplete(a, launchToolKind.String(), projectDir)

	// Enforce Hub YOLO mode override for local launches (Req 7.8).
	yoloMode = a.enforceYoloModeQuiet(yoloMode)
	if launchToolKind.IsClaude() {
		log.Printf("[claude-launch] local-launch-requested project=%q yolo=%v admin=%v", projectDir, yoloMode, adminMode)
	}

	// Platform specific launch
	return a.platformLaunch(binaryName, yoloMode, adminMode, pythonEnv, projectDir, env, selectedModel.ModelId)
}

// launchToolRemoteSession builds a remote launch spec (writing native configs
// once) and creates the session. Used by LaunchTool so remote mode never runs
// the local env/config write path first.
func (a *App) launchToolRemoteSession(
	toolName string,
	config corelib.AppConfig,
	yoloMode, adminMode bool,
	pythonEnv, projectDir string,
	useProxy bool,
	launchToolKind remoteToolNameKind,
) error {
	if launchToolKind.IsClaude() {
		log.Printf("[claude-launch] requested source=desktop-ui mode=remote remote_enabled=%v project=%q yolo=%v admin=%v",
			config.RemoteEnabled, projectDir, yoloMode, adminMode)
	}
	spec, err := a.buildRemoteLaunchSpec(toolName, config, yoloMode, adminMode, pythonEnv, projectDir, useProxy, "", persistNativeToolConfig)
	if err != nil {
		a.log("build remote launch spec failed: " + err.Error())
		return err
	}

	if a.remoteSessions == nil {
		a.createAndWireHubClient()
	}

	session, err := a.remoteSessions.CreateUserSession(spec)
	if err != nil {
		a.log("create remote session failed: " + err.Error())
		return err
	}
	if launchToolKind.IsClaude() && session != nil {
		log.Printf("[claude-launch] dispatched mode=remote session=%s pid=%d source=%s project=%q",
			session.ID, session.PID, session.LaunchSource, session.ProjectPath)
	}
	return nil
}
func (a *App) log(message string) {
	if a.IsInitMode {
		fmt.Println(message)
	}
	if a.ctx != nil {
		a.emitEvent("env-log", message)
	}
}

// writeWorkdirReadySidecar records download/workdir binding outside the filtered
// maclaw.log stream so operators and verify scripts always have a signal.
// It also appends one line directly to maclaw.log (bypassing detailAwareLogWriter)
// because some production builds were observed to drop the standard log.Printf
// for this exact startup section while still executing the following statements.
func writeWorkdirReadySidecar(configured, effective, skillTemp string) {
	dir := corelib.MaclawLogsDir()
	_ = os.MkdirAll(dir, 0o755)
	now := time.Now()
	body := fmt.Sprintf("%s download_file=builtin configured_wd=%q effective_wd=%q skill_temp=%q\n",
		now.Format(time.RFC3339),
		strings.TrimSpace(configured),
		strings.TrimSpace(effective),
		strings.TrimSpace(skillTemp),
	)
	path := filepath.Join(dir, "workdir_ready.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		log.Printf("[startup] warn: write workdir_ready sidecar failed path=%q err=%v", path, err)
	}
	// Unfiltered append — must stay visible even when log detail filter drops lines.
	logLine := fmt.Sprintf("%s workdir_ready.go:0: [startup] workdir ready err=none download_file=builtin configured_wd=%q effective_wd=%q skill_temp=%q\n",
		now.Format("2006/01/02 15:04:05"),
		strings.TrimSpace(configured),
		strings.TrimSpace(effective),
		strings.TrimSpace(skillTemp),
	)
	if f, err := os.OpenFile(filepath.Join(dir, "maclaw.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(logLine)
		_ = f.Close()
	}
}

func (a *App) logStoragePaths(stage string, config *corelib.AppConfig) {
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		cwd = "<error:" + cwdErr.Error() + ">"
	}
	exe, exeErr := os.Executable()
	if exeErr != nil {
		exe = "<error:" + exeErr.Error() + ">"
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		home = "<error:" + homeErr.Error() + ">"
	}
	configPath, configErr := a.getConfigPath()
	if configErr != nil {
		configPath = "<error:" + configErr.Error() + ">"
	}
	configuredDataDir := ""
	configuredWorkingDir := ""
	if config != nil {
		configuredDataDir = strings.TrimSpace(config.DataDir)
		configuredWorkingDir = strings.TrimSpace(config.WorkingDirectory)
	}
	baseDir := a.getMaclawBaseDir()
	dataDir := a.GetDataDir()
	conversationPath := filepath.Join(dataDir, "ai_assistant_conversation.json")
	confirmationPath := filepath.Join(dataDir, "ai_assistant_confirmation.json")
	uiStatePath := filepath.Join(dataDir, "ai_assistant_ui_state.json")
	log.Printf("[paths] stage=%s cwd=%q exe=%q home=%q config_path=%q base_dir=%q data_dir=%q ai_conversation=%q ai_confirmation=%q ai_ui_state=%q configured_data_dir=%q configured_working_dir=%q",
		stage, cwd, exe, home, configPath, baseDir, dataDir, conversationPath, confirmationPath, uiStatePath, configuredDataDir, configuredWorkingDir)
}

func (a *App) getConfigPath() (string, error) {
	if a.testHomeDir != "" {
		return filepath.Join(a.testHomeDir, ".maclaw", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".maclaw", "config.json"), nil
}

func (a *App) migrateLegacyConfigLocked(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	home := a.GetUserHomeDir()
	oldPath := filepath.Join(home, ".aicoder_config.json")
	data, err := os.ReadFile(oldPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data = sanitizeLegacyCodingToolSelection(data)
	return configfile.AtomicWrite(path, data)
}

func sanitizeLegacyCodingToolSelection(data []byte) []byte {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return data
	}
	changed := false
	if tool, ok := raw["active_tool"].(string); ok {
		if normalized, valid := normalizeCodingToolSelection(tool); valid {
			if normalized != tool {
				raw["active_tool"] = normalized
				changed = true
			}
		} else {
			raw["active_tool"] = "claude"
			changed = true
		}
	}
	if tool, ok := raw["default_tool"].(string); ok && strings.TrimSpace(tool) != "" {
		if normalized, valid := normalizeCodingToolSelection(tool); valid {
			if normalized != tool {
				raw["default_tool"] = normalized
				changed = true
			}
		} else {
			raw["default_tool"] = "claude"
			changed = true
		}
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return data
	}
	return out
}

// LoadConfig returns the current app config. All access goes through the
// unified config transaction protocol in config_txn.go (read snapshot +
// runConfigPostUnlock). Callers must not take configMu themselves for reads.
func (a *App) LoadConfig() (corelib.AppConfig, error) {
	return a.loadConfigSnapshot()
}

// getConfigForMutationLocked returns a working copy for in-memory mutation.
// Prefer the write-side configCache (full, including skills) under configMu so
// token patches do not reassemble skills from a separate snap incorrectly.
// The returned value owns its slices/maps (cloneAppConfigForMutation).
func (a *App) getConfigForMutationLocked() (corelib.AppConfig, error) {
	if a.configCacheValid {
		return cloneAppConfigForMutation(a.configCache), nil
	}
	cfg, err := a.loadConfigLocked()
	if err != nil {
		return corelib.AppConfig{}, err
	}
	return cloneAppConfigForMutation(cfg), nil
}

// LoadConfigForUI is the frontend-facing config load. Same as LoadConfig but
// omits NLSkills (UI uses ListNLSkills) to shrink Wails JSON payloads.
func (a *App) LoadConfigForUI() (corelib.AppConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return cfg, err
	}
	cfg.NLSkills = nil
	return cfg, nil
}

// commitConfigToDisk persists config after an in-memory mutation. Prefer the
// latest published snapshot so concurrent PatchConfig coalesces to last
// mutation; fall back to the caller snapshot if the cache was invalidated.
// Disk I/O is serialized inside saveToPath (configDiskMu).
func (a *App) commitConfigToDisk(path string, fallback corelib.AppConfig) error {
	toWrite := fallback
	if cfg, ok := a.publishedConfig(); ok {
		toWrite = cfg
	}
	err := a.saveToPath(path, toWrite)
	if err == nil {
		// Any successful full write satisfies deferred bootstrap/pet persists.
		a.pendingConfigDiskWrite.Store(false)
	}
	return err
}

// applyDataDirFromConfigLocked updates the effective data dir under configMu.
// When the path changes it only sets pendingDataDirReset — never tear down
// stores here. Call drainPendingDataDirReset after releasing configMu.
func (a *App) applyDataDirFromConfigLocked(config corelib.AppConfig) (dataDirChanged bool) {
	if a == nil {
		return false
	}
	oldBase := a.getMaclawBaseDir()
	var newBase string
	if strings.TrimSpace(a.testHomeDir) != "" && strings.TrimSpace(config.DataDir) == "" {
		newBase = filepath.Join(a.testHomeDir, ".maclaw")
		corelib.SetMaclawBaseDir(newBase)
	} else {
		corelib.SetMaclawBaseDirFromConfig(config.DataDir)
		newBase = corelib.MaclawBaseDir()
	}
	a.effectiveBaseDir.Store(newBase)
	changed := strings.TrimSpace(oldBase) != "" && filepath.Clean(oldBase) != filepath.Clean(newBase)
	if changed {
		a.pendingDataDirReset.Store(true)
	}
	return changed
}

// drainPendingDataDirReset runs path-bound store teardown if a prior locked
// apply flagged a data-dir change. Safe and idempotent; never call under configMu.
func (a *App) drainPendingDataDirReset() {
	if a == nil {
		return
	}
	if a.pendingDataDirReset.CompareAndSwap(true, false) {
		a.resetPathBoundStateForDataDirChange()
	}
}

// drainPendingConfigDiskWrite persists a config snapshot that was prepared
// under configMu (bootstrap / migration / pet sanitize) without holding the
// lock across AtomicWrite. Never call under configMu.
func (a *App) drainPendingConfigDiskWrite(cfg corelib.AppConfig) {
	if a == nil || !a.pendingConfigDiskWrite.CompareAndSwap(true, false) {
		return
	}
	path, err := a.getConfigPath()
	if err != nil {
		a.pendingConfigDiskWrite.Store(true)
		log.Printf("[config] deferred_disk_write get_path_failed err=%v", err)
		return
	}
	if err := a.commitConfigToDisk(path, cfg); err != nil {
		a.pendingConfigDiskWrite.Store(true)
		log.Printf("[config] deferred_disk_write failed err=%v", err)
		return
	}
	log.Printf("[config] deferred_disk_write ok variant=%q", cfg.PetVariant)
}

// sshHostEntries returns configured SSH hosts for form option injection.
// Never returns passwords to callers that only need profile metadata.
func (a *App) sshHostEntries() []corelib.SSHHostEntry {
	if a == nil {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	if len(cfg.SSHHosts) == 0 {
		return nil
	}
	out := make([]corelib.SSHHostEntry, len(cfg.SSHHosts))
	copy(out, cfg.SSHHosts)
	// Strip secrets so form helpers cannot accidentally persist them.
	for i := range out {
		out[i].Password = ""
		out[i].Passphrase = ""
	}
	return out
}

func (a *App) invalidateConfigCacheLocked() {
	a.configSnap.Store(nil)
	a.nlSkillsSnap.Store(nil)
	a.configCache = corelib.AppConfig{}
	a.configCacheValid = false
}

func (a *App) loadConfigLocked() (corelib.AppConfig, error) {
	start := time.Now()
	log.Printf("[config] LoadConfig:start")

	path, err := a.getConfigPath()
	if err != nil {
		log.Printf("[config] LoadConfig:get_path_failed after=%s err=%v", time.Since(start), err)
		return corelib.AppConfig{}, err
	}
	log.Printf("[config] LoadConfig:path=%q", path)
	if err := a.migrateLegacyConfigLocked(path); err != nil {
		log.Printf("[config] LoadConfig:migrate_failed after=%s err=%v", time.Since(start), err)
		return corelib.AppConfig{}, err
	}
	// Helper for default models
	defaultClaudeModels := []corelib.ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: "", IsBuiltin: true},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/anthropic", ApiKey: ""},
		{ModelName: "Kimi", ModelId: "kimi-k2-thinking", ModelUrl: "https://api.kimi.com/coding", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding", ApiKey: ""},
		{ModelName: "iFlytek", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic", ApiKey: "", HasSubscription: true},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/anthropic", ApiKey: ""},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/anthropic", ApiKey: ""},
		{ModelName: "Baidu Qianfan", ModelId: "qianfan-code-latest", ModelUrl: "https://qianfan.baidubce.com/anthropic/coding", ApiKey: "", HasSubscription: true},
		{ModelName: "Tencent Cloud", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/anthropic", ApiKey: "", HasSubscription: true},
		{ModelName: "Moore Threads", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net", ApiKey: "", HasSubscription: true},
		{ModelName: "Kuaishou", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/kat-coder-pro-v1/claude-code-proxy", ApiKey: "", HasSubscription: true},
		{ModelName: "Aliyun", ModelId: "glm-5", ModelUrl: "https://coding.dashscope.aliyuncs.com/apps/anthropic", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultCodexModels := []corelib.ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: "", IsBuiltin: true},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: "", WireApi: "responses"},
		{ModelName: "GLM", ModelId: "GLM-5.2", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: "", WireApi: "responses"},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: "", WireApi: "responses"},
		{ModelName: "iFlytek", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", WireApi: "responses", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: "", WireApi: "responses"},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: "", WireApi: "responses"},
		{ModelName: "Tencent Cloud", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", WireApi: "responses", HasSubscription: true},
		{ModelName: "Moore Threads", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", WireApi: "responses", HasSubscription: true},
		{ModelName: "Kuaishou", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", WireApi: "responses", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", WireApi: "responses", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", WireApi: "responses", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", WireApi: "responses", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", WireApi: "responses", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", WireApi: "responses", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", WireApi: "responses", IsCustom: true},
	}
	defaultOpencodeModels := []corelib.ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: "", IsBuiltin: true},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "GLM-5.2", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "iFlytek", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "Tencent Cloud", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "Moore Threads", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Kuaishou", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultIFlowModels := []corelib.ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: "", IsBuiltin: true},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "GLM-5.2", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "iFlytek", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "XiaoMi", ModelId: "mimo-v2-flash", ModelUrl: "https://api.xiaomimimo.com/v1", ApiKey: ""},
		{ModelName: "Tencent Cloud", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "Moore Threads", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Kuaishou", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultKiloModels := []corelib.ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: "", IsBuiltin: true},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "GLM-5.2", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "iFlytek", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "XiaoMi", ModelId: "mimo-v2-flash", ModelUrl: "https://api.xiaomimimo.com/v1", ApiKey: ""},
		{ModelName: "Tencent Cloud", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "Moore Threads", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Kuaishou", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Check for old config file for migration
		home, _ := os.UserHomeDir()
		oldPath := filepath.Join(home, ".claude_model_config.json")
		if _, err := os.Stat(oldPath); err == nil {
			// Migrate old config
			data, err := os.ReadFile(oldPath)
			if err == nil {
				var oldConfig struct {
					CurrentModel string                  `json:"current_model"`
					Models       []corelib.ModelConfig   `json:"models"`
					Projects     []corelib.ProjectConfig `json:"projects"`
					CurrentProj  string                  `json:"current_project"`
				}
				if err := json.Unmarshal(data, &oldConfig); err == nil {
					// Start from AppConfigDefaults so any new default-true fields
					// are automatically inherited — no manual sync required.
					config := corelib.AppConfigDefaults()
					config.Claude = corelib.ToolConfig{
						CurrentModel: oldConfig.CurrentModel,
						Models:       oldConfig.Models,
					}
					config.Codex = corelib.ToolConfig{
						CurrentModel: "Codex",
						Models:       defaultCodexModels,
					}
					config.Opencode = corelib.ToolConfig{
						CurrentModel: "Original",
						Models:       defaultOpencodeModels,
					}
					config.CodeBuddy = corelib.ToolConfig{
						CurrentModel: "Original",
						Models:       defaultOpencodeModels,
					}
					config.IFlow = corelib.ToolConfig{
						CurrentModel: "Original",
						Models:       defaultIFlowModels,
					}
					config.Kilo = corelib.ToolConfig{
						CurrentModel: "Original",
						Models:       defaultKiloModels,
					}
					config.Projects = oldConfig.Projects
					config.CurrentProject = oldConfig.CurrentProj
					config.ActiveTool = "claude"
					config.RemoteHubCenterURL = defaultRemoteHubCenterURL
					config.RemoteHeartbeatSec = corelib.DefaultRemoteHeartbeatSec
					config.SubAgentConcurrency = corelib.DefaultSubAgentConcurrency
					// New-format install path (legacy file migration): figurative pet.
					corelib.ApplyNewInstallPetDefaults(&config)

					// Publish in-memory only; disk write is deferred via
					// pendingConfigDiskWrite → runConfigPostUnlock (no configMu held).
					a.publishConfigLocked(config)
					a.pendingConfigDiskWrite.Store(true)
					// Optional: os.Remove(oldPath)
					return config, nil
				}
			}
		}
		// Create default config — start from AppConfigDefaults() so that any
		// new default-true field is automatically picked up.
		defaultConfig := corelib.AppConfigDefaults()
		defaultConfig.Claude = corelib.ToolConfig{
			CurrentModel: "GLM",
			Models:       defaultClaudeModels,
		}
		defaultConfig.Codex = corelib.ToolConfig{
			CurrentModel: "Codex",
			Models:       defaultCodexModels,
		}
		defaultConfig.Opencode = corelib.ToolConfig{
			CurrentModel: "AiCodeMirror",
			Models:       defaultOpencodeModels,
		}
		defaultConfig.CodeBuddy = corelib.ToolConfig{
			CurrentModel: "AiCodeMirror",
			Models:       defaultOpencodeModels,
		}
		defaultConfig.IFlow = corelib.ToolConfig{
			CurrentModel: "Original",
			Models:       defaultIFlowModels,
		}
		defaultConfig.Kilo = corelib.ToolConfig{
			CurrentModel: "Original",
			Models:       defaultKiloModels,
		}
		defaultConfig.ActiveTool = "claude"
		defaultConfig.EnvCheckInterval = 7
		defaultConfig.RemoteHubCenterURL = defaultRemoteHubCenterURL
		defaultConfig.RemoteHeartbeatSec = corelib.DefaultRemoteHeartbeatSec
		defaultConfig.ScreenDimTimeoutMin = 3
		defaultConfig.SubAgentConcurrency = corelib.DefaultSubAgentConcurrency
		defaultConfig.NetworkLevel = "full"
		defaultConfig.SandboxMode = "none"
		if _, err := a.ensureDefaultHardwareWelcome(&defaultConfig); err != nil {
			return corelib.AppConfig{}, err
		}
		// K18: first-run figurative pet (must not come from UnmarshalJSON seed).
		corelib.ApplyNewInstallPetDefaults(&defaultConfig)
		// Defer AtomicWrite until after configMu is released.
		a.publishConfigLocked(defaultConfig)
		a.pendingConfigDiskWrite.Store(true)
		return defaultConfig, nil
	}
	var config corelib.AppConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return config, err
	}
	if changed, err := a.ensureDefaultHardwareWelcome(&config); err != nil {
		return config, err
	} else if changed {
		a.pendingConfigDiskWrite.Store(true)
	}
	// Upgrade only the exact historic default. Any other value is a user-defined
	// role description and must remain untouched.
	if config.MaclawRoleDescription == "一个尽心尽责无所不能的软件开发管家" {
		config.MaclawRoleDescription = corelib.DefaultMaclawRoleDescription
	}

	// All bool/struct defaults are handled by AppConfig.UnmarshalJSON via
	// appConfigDefaults(). Only non-bool post-unmarshal fixups remain here.
	if config.NetworkLevel == "" {
		config.NetworkLevel = "full"
	}
	if config.SandboxMode == "" {
		config.SandboxMode = "none"
	}
	config.RemoteHeartbeatSec = corelib.NormalizeRemoteHeartbeatIntervalSec(config.RemoteHeartbeatSec)

	// Set default values for new fields if not present or invalid
	if config.EnvCheckInterval < 2 || config.EnvCheckInterval > 30 {
		config.EnvCheckInterval = 7 // Default to 7 days
	}
	// Ensure defaults for new fields
	if config.Claude.CurrentModel == "" && len(config.Claude.Models) > 0 {
		config.Claude.CurrentModel = config.Claude.Models[0].ModelName
	}
	// Helper to rename a model (migrate old name -new name), preserving user's API key and settings.
	// If newName already exists, the old entry is removed to avoid duplicates.
	renameModel := func(models *[]corelib.ModelConfig, oldName, newName string, currentModel *string) {
		newIdx := -1
		oldIdx := -1
		for i := range *models {
			if (*models)[i].ModelName == oldName {
				oldIdx = i
			}
			if strings.EqualFold((*models)[i].ModelName, newName) {
				newIdx = i
			}
		}
		if oldIdx == -1 {
			return // old name not found, nothing to migrate
		}
		if newIdx != -1 && newIdx != oldIdx {
			// Both old and new exist -merge: copy user settings from old to new if new has none, then remove old
			if (*models)[newIdx].ApiKey == "" && (*models)[oldIdx].ApiKey != "" {
				(*models)[newIdx].ApiKey = (*models)[oldIdx].ApiKey
			}
			if (*models)[newIdx].ModelId == "" && (*models)[oldIdx].ModelId != "" {
				(*models)[newIdx].ModelId = (*models)[oldIdx].ModelId
			}
			if (*models)[newIdx].ModelUrl == "" && (*models)[oldIdx].ModelUrl != "" {
				(*models)[newIdx].ModelUrl = (*models)[oldIdx].ModelUrl
			}
			if (*models)[newIdx].WireApi == "" && (*models)[oldIdx].WireApi != "" {
				(*models)[newIdx].WireApi = (*models)[oldIdx].WireApi
			}
			*models = append((*models)[:oldIdx], (*models)[oldIdx+1:]...)
		} else if newIdx == -1 {
			// Only old exists -just rename
			(*models)[oldIdx].ModelName = newName
		}
		// Update current_model reference
		if *currentModel == oldName {
			*currentModel = newName
		}
	}

	// Migrate Chinese provider names to English (legacy config compatibility).
	// Previous versions used Chinese names; current version uses English names.
	chineseToEnglish := map[string]string{
		"\u817e\u8baf\u4e91":       "Tencent Cloud",
		"\u6469\u5c14\u7ebf\u7a0b": "Moore Threads",
		"\u5feb\u624b":             "Kuaishou",
		"\u963f\u91cc\u4e91":       "Aliyun",
		"\u767e\u5ea6\u5343\u5e06": "Baidu Qianfan",
		"\u8baf\u98de\u661f\u8fb0": "iFlytek",
	}
	allToolCfgs := []*corelib.ToolConfig{
		&config.Claude, &config.Codex, &config.Opencode,
		&config.CodeBuddy, &config.IFlow, &config.Kilo,
	}
	for oldName, newName := range chineseToEnglish {
		for _, tc := range allToolCfgs {
			renameModel(&tc.Models, oldName, newName, &tc.CurrentModel)
		}
	}

	// Helper to ensure a model exists in the list
	ensureModel := func(models *[]corelib.ModelConfig, name, url, id, wireApi string, hasSubscription ...bool) {
		hasSub := false
		if len(hasSubscription) > 0 {
			hasSub = hasSubscription[0]
		}
		for i := range *models {
			if strings.EqualFold((*models)[i].ModelName, name) {
				(*models)[i].ModelName = name // Update to canonical casing
				if url != "" {
					(*models)[i].ModelUrl = url
				}
				// Only set ModelId if user hasn't customized it (empty means not set yet)
				if id != "" && (*models)[i].ModelId == "" {
					(*models)[i].ModelId = id
				}
				if wireApi != "" {
					(*models)[i].WireApi = wireApi
				}
				(*models)[i].HasSubscription = hasSub
				return
			}
		}
		*models = append(*models, corelib.ModelConfig{ModelName: name, ModelUrl: url, ModelId: id, WireApi: wireApi, ApiKey: "", HasSubscription: hasSub})
	}
	// Helper to remove a model from the list
	removeModel := func(models *[]corelib.ModelConfig, name string) {
		var newModels []corelib.ModelConfig
		for _, m := range *models {
			if !strings.EqualFold(m.ModelName, name) {
				newModels = append(newModels, m)
			}
		}
		*models = newModels
	}
	if config.Codex.Models == nil || len(config.Codex.Models) == 0 {
		config.Codex.Models = defaultCodexModels
		config.Codex.CurrentModel = "Original"
	}
	if config.Opencode.Models == nil || len(config.Opencode.Models) == 0 {
		config.Opencode.Models = defaultOpencodeModels
		config.Opencode.CurrentModel = "Original"
	}
	if config.CodeBuddy.Models == nil || len(config.CodeBuddy.Models) == 0 {
		config.CodeBuddy.Models = defaultOpencodeModels
		config.CodeBuddy.CurrentModel = "Original"
	}
	if config.IFlow.Models == nil || len(config.IFlow.Models) == 0 {
		config.IFlow.Models = defaultIFlowModels
		config.IFlow.CurrentModel = "Original"
	}
	if config.Kilo.Models == nil || len(config.Kilo.Models) == 0 {
		config.Kilo.Models = defaultKiloModels
		config.Kilo.CurrentModel = "Original"
	}
	ensureModel(&config.Claude.Models, "DeepSeek", "https://api.deepseek.com/anthropic", "deepseek-chat", "anthropic")
	ensureModel(&config.Claude.Models, "Kimi", "https://api.kimi.com/coding", "kimi-k2-thinking", "anthropic")
	ensureModel(&config.Claude.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding", "doubao-seed-code-preview-latest", "anthropic")
	ensureModel(&config.Claude.Models, "iFlytek", "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic", "astron-code-latest", "anthropic", true)
	ensureModel(&config.Claude.Models, "GLM", "https://open.bigmodel.cn/api/anthropic", "glm-4.7", "anthropic")
	ensureModel(&config.Claude.Models, "MiniMax", "https://api.minimaxi.com/anthropic", "MiniMax-M2.1", "anthropic")
	ensureModel(&config.Claude.Models, "Baidu Qianfan", "https://qianfan.baidubce.com/anthropic/coding", "qianfan-code-latest", "anthropic", true)
	ensureModel(&config.Claude.Models, "XiaoMi", "https://api.xiaomimimo.com/anthropic", "mimo-v2-flash", "anthropic")
	ensureModel(&config.Claude.Models, "Tencent Cloud", "https://api.lkeap.cloud.tencent.com/coding/anthropic", "glm-5", "anthropic", true)
	ensureModel(&config.Claude.Models, "Moore Threads", "https://coding-plan-endpoint.kuaecloud.net", "GLM-4.7", "anthropic", true)
	ensureModel(&config.Claude.Models, "Kuaishou", "https://wanqing.streamlakeapi.com/api/gateway/coding/kat-coder-pro-v1/claude-code-proxy", "kat-coder-pro-v1", "anthropic", true)
	ensureModel(&config.Claude.Models, "Aliyun", "https://coding.dashscope.aliyuncs.com/apps/anthropic", "glm-5", "anthropic", true)
	ensureModel(&config.Codex.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "responses")
	ensureModel(&config.Codex.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "GLM-5.2", "responses")
	ensureModel(&config.Codex.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "responses")
	ensureModel(&config.Codex.Models, "iFlytek", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "responses", true)
	ensureModel(&config.Codex.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "responses")
	ensureModel(&config.Codex.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "responses")
	ensureModel(&config.Codex.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "responses")
	ensureModel(&config.Codex.Models, "Tencent Cloud", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "responses", true)
	ensureModel(&config.Codex.Models, "Moore Threads", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "responses", true)
	ensureModel(&config.Codex.Models, "Kuaishou", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "responses", true)
	for i := range config.Codex.Models {
		if !config.Codex.Models[i].IsBuiltin && strings.TrimSpace(config.Codex.Models[i].WireApi) == "" {
			config.Codex.Models[i].WireApi = "responses"
		}
	}
	ensureModel(&config.Opencode.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.Opencode.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "GLM-5.2", "")
	ensureModel(&config.Opencode.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Opencode.Models, "iFlytek", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.Opencode.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.Opencode.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.Opencode.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.Opencode.Models, "Tencent Cloud", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.Opencode.Models, "Moore Threads", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.Opencode.Models, "Kuaishou", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.CodeBuddy.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.CodeBuddy.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "GLM-5.2", "")
	ensureModel(&config.CodeBuddy.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.CodeBuddy.Models, "iFlytek", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.CodeBuddy.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.CodeBuddy.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.CodeBuddy.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.CodeBuddy.Models, "Tencent Cloud", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.CodeBuddy.Models, "Moore Threads", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.CodeBuddy.Models, "Kuaishou", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.IFlow.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.IFlow.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "GLM-5.2", "")
	ensureModel(&config.IFlow.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.IFlow.Models, "iFlytek", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.IFlow.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.IFlow.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.IFlow.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.IFlow.Models, "Tencent Cloud", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.IFlow.Models, "Moore Threads", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.IFlow.Models, "Kuaishou", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.Kilo.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.Kilo.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "GLM-5.2", "")
	ensureModel(&config.Kilo.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Kilo.Models, "iFlytek", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.Kilo.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.Kilo.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.Kilo.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.Kilo.Models, "Tencent Cloud", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.Kilo.Models, "Moore Threads", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.Kilo.Models, "Kuaishou", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)

	// Purge Aliyun from other tools if it exists
	removeModel(&config.Codex.Models, "Aliyun")
	removeModel(&config.Opencode.Models, "Aliyun")
	removeModel(&config.CodeBuddy.Models, "Aliyun")
	removeModel(&config.Codex.Models, "Baidu Qianfan")
	removeModel(&config.Opencode.Models, "Baidu Qianfan")
	removeModel(&config.CodeBuddy.Models, "Baidu Qianfan")
	removeModel(&config.IFlow.Models, "Baidu Qianfan")
	removeModel(&config.Kilo.Models, "Baidu Qianfan")
	removeModel(&config.IFlow.Models, "Aliyun")
	removeModel(&config.Kilo.Models, "Aliyun")
	removeModel(&config.Codex.Models, "aliyun")
	removeModel(&config.Opencode.Models, "aliyun")
	removeModel(&config.CodeBuddy.Models, "aliyun")
	removeModel(&config.IFlow.Models, "aliyun")
	removeModel(&config.Kilo.Models, "aliyun")
	removeDisallowedCodingToolProviders(&config)

	// Ensure 'Original' is always present and first
	ensureOriginal := func(models *[]corelib.ModelConfig) {
		found := false
		for _, m := range *models {
			if m.ModelName == "Original" {
				found = true
				break
			}
		}
		if !found {
			*models = append([]corelib.ModelConfig{{ModelName: "Original", ModelUrl: "", ApiKey: "", IsBuiltin: true}}, *models...)
		}
	}
	// Opencode does NOT use common relay providers
	cleanOpencodeModels := func(models *[]corelib.ModelConfig) {
		var newModels []corelib.ModelConfig
		for _, m := range *models {
			name := strings.ToLower(m.ModelName)
			if name != "aigocode" && name != "aicodemirror" && name != "coderelay" && name != "chatfire" {
				newModels = append(newModels, m)
			}
		}
		*models = newModels
	}
	ensureOriginal(&config.Claude.Models)
	ensureOriginal(&config.Codex.Models)
	ensureOriginal(&config.Opencode.Models)
	ensureOriginal(&config.CodeBuddy.Models)
	ensureOriginal(&config.IFlow.Models)
	ensureOriginal(&config.Kilo.Models)
	cleanOpencodeModels(&config.Opencode.Models)
	cleanOpencodeModels(&config.CodeBuddy.Models)
	cleanOpencodeModels(&config.IFlow.Models)
	// Ensure at least 2 custom models are always present, and at most 6
	// Custom models are identified by IsCustom flag, not by name
	ensureCustom := func(models *[]corelib.ModelConfig) {
		customCount := 0
		for _, m := range *models {
			if m.IsCustom {
				customCount++
			}
		}
		// Ensure at least 2 custom models exist
		for customCount < 2 {
			customCount++
			name := "Custom"
			if customCount > 1 {
				name = fmt.Sprintf("Custom%d", customCount-1)
			}
			*models = append(*models, corelib.ModelConfig{ModelName: name, ModelUrl: "", ApiKey: "", IsCustom: true})
		}
		// Ensure at most 6 custom models exist
		if customCount > 6 {
			var newModels []corelib.ModelConfig
			customAdded := 0
			for _, m := range *models {
				if m.IsCustom {
					if customAdded < 6 {
						newModels = append(newModels, m)
						customAdded++
					}
				} else {
					newModels = append(newModels, m)
				}
			}
			*models = newModels
		}
	}
	ensureCustom(&config.Claude.Models)
	ensureCustom(&config.Codex.Models)
	ensureCustom(&config.Opencode.Models)
	ensureCustom(&config.CodeBuddy.Models)
	ensureCustom(&config.IFlow.Models)
	ensureCustom(&config.Kilo.Models)
	// Ensure custom models are always last for all tools
	// Custom models are identified by IsCustom flag, not by name
	moveCustomToLast := func(models *[]corelib.ModelConfig) {
		var customModels []corelib.ModelConfig
		var newModels []corelib.ModelConfig
		for _, m := range *models {
			if m.IsCustom {
				customModels = append(customModels, m)
			} else {
				newModels = append(newModels, m)
			}
		}
		// Append all custom models at the end
		*models = append(newModels, customModels...)
	}
	// Ensure 'Original' is always first for all tools
	ensureOriginalFirst := func(models *[]corelib.ModelConfig) {
		var originalModel *corelib.ModelConfig
		var newModels []corelib.ModelConfig
		for i := range *models {
			m := &(*models)[i]
			if m.ModelName == "Original" {
				m.IsBuiltin = true
				originalModel = m
			} else {
				newModels = append(newModels, *m)
			}
		}
		if originalModel != nil {
			*models = append([]corelib.ModelConfig{*originalModel}, newModels...)
		}
	}
	moveCustomToLast(&config.Claude.Models)
	moveCustomToLast(&config.Codex.Models)
	moveCustomToLast(&config.Opencode.Models)
	moveCustomToLast(&config.CodeBuddy.Models)
	moveCustomToLast(&config.IFlow.Models)
	moveCustomToLast(&config.Kilo.Models)
	ensureOriginalFirst(&config.Claude.Models)
	ensureOriginalFirst(&config.Codex.Models)
	ensureOriginalFirst(&config.Opencode.Models)
	ensureOriginalFirst(&config.CodeBuddy.Models)
	ensureOriginalFirst(&config.IFlow.Models)
	ensureOriginalFirst(&config.Kilo.Models)
	// Ensure CurrentModel is valid
	if config.Codex.CurrentModel == "" {
		config.Codex.CurrentModel = "Original"
	}
	if config.Opencode.CurrentModel == "" {
		config.Opencode.CurrentModel = "Original"
	}
	if config.CodeBuddy.CurrentModel == "" {
		config.CodeBuddy.CurrentModel = "Original"
	}
	if config.IFlow.CurrentModel == "" {
		config.IFlow.CurrentModel = "Original"
	}
	if config.Kilo.Models == nil || len(config.Kilo.Models) == 0 {
		config.Kilo.Models = defaultKiloModels
		config.Kilo.CurrentModel = "Original"
	}
	if config.Kilo.CurrentModel == "" {
		config.Kilo.CurrentModel = "Original"
	}
	if config.ActiveTool == "" {
		config.ActiveTool = "message"
	}
	// Normalize CurrentModel casing for all tools
	normalizeCurrentModel := func(toolCfg *corelib.ToolConfig) {
		matched := false
		for _, m := range toolCfg.Models {
			if strings.EqualFold(m.ModelName, toolCfg.CurrentModel) {
				toolCfg.CurrentModel = m.ModelName
				matched = true
				break
			}
		}
		if !matched && len(toolCfg.Models) > 0 {
			toolCfg.CurrentModel = toolCfg.Models[0].ModelName
		}
	}
	normalizeCurrentModel(&config.Claude)
	normalizeCurrentModel(&config.Codex)
	normalizeCurrentModel(&config.Opencode)
	normalizeCurrentModel(&config.CodeBuddy)
	normalizeCurrentModel(&config.IFlow)
	normalizeCurrentModel(&config.Kilo)
	sanitizeCodingToolSelection(&config)
	normalizeConfigTimeouts(&config)
	normalizeProjectNames(&config)
	EnsurePetPackRegistryScanned()
	// Snapshot for K18: empty pet_variant must resolve AND persist as classic for legacy installs.
	beforePetVariant := config.PetVariant
	beforePetMigrated := config.PetVariantMigrated
	beforePetSkin := config.PetSkin
	beforePetPrompt := config.PetFigurativeUpgradePromptPending
	sanitizePetConfig(&config)
	petConfigMutated := beforePetVariant != config.PetVariant ||
		beforePetMigrated != config.PetVariantMigrated ||
		beforePetSkin != config.PetSkin ||
		beforePetPrompt != config.PetFigurativeUpgradePromptPending
	config.LLMPromptCache = config.LLMPromptCache.WithDefaults()
	if err := migrateLLMPromptCacheDirIfNeeded(corelib.DefaultLLMPromptCacheConfig(), config.LLMPromptCache); err != nil {
		log.Printf("[config] LoadConfig:llm_cache_migrate_failed err=%v", err)
	}
	// Defer K18 pet migration disk write until after configMu is released so a
	// cold LoadConfig does not hold the exclusive lock across AtomicWrite.
	if petConfigMutated {
		a.pendingConfigDiskWrite.Store(true)
	}
	log.Printf("[config] LoadConfig:done total=%s config_path=%q configured_data_dir=%q configured_working_dir=%q effective_base_dir=%q effective_data_dir=%q ai_conversation=%q",
		time.Since(start), path, strings.TrimSpace(config.DataDir), strings.TrimSpace(config.WorkingDirectory), a.getMaclawBaseDir(), a.GetDataDir(), filepath.Join(a.GetDataDir(), "ai_assistant_conversation.json"))
	// Publish immutable snap + data-dir pointer; heavy reset deferred.
	a.publishConfigLocked(config)
	corelib.SetLogDetailEnabled(config.LogDetailEnabled)
	memory.SetMemoryRecallLogEnabled(config.MemoryRecallLogEnabled)
	return config, nil
}

// applyDataDirFromConfig updates the effective data dir and, when the path
// actually changes, resets path-bound stores. Safe to call without configMu.
func (a *App) applyDataDirFromConfig(config corelib.AppConfig) {
	a.applyDataDirFromConfigLocked(config)
	a.drainPendingDataDirReset()
}

func (a *App) resetPathBoundStateForDataDirChange() {
	a.conversationMemoryMu.Lock()
	conversationMemory := a.aiConversationMemory
	a.aiConversationMemory = nil
	a.conversationMemoryMu.Unlock()
	if conversationMemory != nil {
		if err := conversationMemory.FlushNow(); err != nil {
			log.Printf("[config] data_dir_change: conversation memory flush failed: %v", err)
		}
		conversationMemory.Stop()
	}
	a.confirmationStoreMu.Lock()
	confirmationStore := a.aiConfirmationStore
	a.aiConfirmationStore = nil
	a.confirmationStoreMu.Unlock()
	if confirmationStore != nil {
		confirmationStore.stop()
	}
	if a.auditLog != nil {
		_ = a.auditLog.Close()
		a.auditLog = nil
	}
	if a.sessionSearchStore != nil {
		_ = a.sessionSearchStore.Close()
		a.sessionSearchStore = nil
	}
	a.sessionStoreMu = sync.Once{}
	if a.imAuditStore != nil {
		_ = a.imAuditStore.Close()
		a.imAuditStore = nil
	}
	a.imAuditStoreMu = sync.Once{}
	if a.codingKnowledgeStore != nil {
		codingKnowledgeStoreMu.Lock()
		_ = a.codingKnowledgeStore.Close()
		a.codingKnowledgeStore = nil
		codingKnowledgeStoreMu.Unlock()
	}
	if a.userModel != nil {
		if err := a.userModel.Save(); err != nil {
			log.Printf("[config] data_dir_change: user model save failed: %v", err)
		}
		a.userModel = nil
	}
	a.userModelMu = sync.Once{}
	a.evidenceCollector = nil
	a.evidenceCollectorMu = sync.Once{}
	a.templateManager = nil
	a.projectTabSessionPersist = nil
	if a.memoryMaintenance != nil {
		a.memoryMaintenance.Stop()
		a.memoryMaintenance = nil
	}
	if a.memPipeline != nil {
		a.memPipeline.Stop()
		a.memPipeline = nil
	}
	a.stopMemoryPipelineSchedule("data_dir_change")
	a.compressorMu.Lock()
	if a.memoryCompressor != nil {
		a.memoryCompressor.Stop()
		a.memoryCompressor = nil
	}
	a.compressorMu.Unlock()
	a.memoryStoreMu.Lock()
	if a.memoryStore != nil {
		a.memoryStore.Stop()
		a.memoryStore = nil
	}
	a.memoryStoreMu.Unlock()
	a.conversationArchiver = nil
	a.sessionCheckpointer = nil
}

func normalizeConfigTimeouts(config *corelib.AppConfig) {
	config.MaclawLLMTimeoutSec = corelib.NormalizeAgentTimeoutSec(config.MaclawLLMTimeoutSec)
	config.AgentResponseTimeoutSec = corelib.NormalizeAgentTimeoutSec(config.AgentResponseTimeoutSec)
	config.SkillRunnerTimeoutSec = corelib.NormalizeSkillRunnerTimeoutSec(config.SkillRunnerTimeoutSec)
	for i := range config.MaclawLLMProviders {
		config.MaclawLLMProviders[i].TimeoutSec = corelib.NormalizeAgentTimeoutSec(config.MaclawLLMProviders[i].TimeoutSec)
	}
}

// normalizeProjectNames ensures every project has a non-empty Name.
// Projects created by older code paths or tests may have an empty Name field.
func normalizeProjectNames(config *corelib.AppConfig) {
	changed := false
	for i := range config.Projects {
		if strings.TrimSpace(config.Projects[i].Name) == "" {
			// Derive name from the last component of the path.
			name := filepath.Base(config.Projects[i].Path)
			if name == "" || name == "." || name == "/" || name == "\\" {
				name = "Project"
			}
			config.Projects[i].Name = name
			changed = true
		}
	}
	_ = changed // normalization is in-place; caller persists if needed
}

// getProviderModel gets the model for a specific provider name from a tool config
func getProviderModel(toolConfig *corelib.ToolConfig, providerName string) *corelib.ModelConfig {
	for i := range toolConfig.Models {
		if strings.EqualFold(toolConfig.Models[i].ModelName, providerName) {
			return &toolConfig.Models[i]
		}
	}
	return nil
}

// syncAllProviderApiKeys synchronizes apikeys of all providers (except 'Original' and 'Custom') across all tools
func syncAllProviderApiKeys(a *App, oldConfig, newConfig *corelib.AppConfig) {
	// Map of tools for easy access
	tools := map[string]*corelib.ToolConfig{
		"claude":    &newConfig.Claude,
		"codex":     &newConfig.Codex,
		"opencode":  &newConfig.Opencode,
		"codebuddy": &newConfig.CodeBuddy,
		"iflow":     &newConfig.IFlow,
		"kilo":      &newConfig.Kilo,
	}
	oldTools := map[string]*corelib.ToolConfig{
		"claude":    &oldConfig.Claude,
		"codex":     &oldConfig.Codex,
		"opencode":  &oldConfig.Opencode,
		"codebuddy": &oldConfig.CodeBuddy,
		"iflow":     &oldConfig.IFlow,
		"kilo":      &oldConfig.Kilo,
	}
	// providerName (lower) -> intended API key
	intentions := make(map[string]string)
	activeToolName := strings.ToLower(newConfig.ActiveTool)
	// 1. Detect Intent from Active Tool (Highest Priority)
	if activeTool, ok := tools[activeToolName]; ok {
		oldActive := oldTools[activeToolName]
		if oldActive != nil {
			for _, m := range activeTool.Models {
				if m.IsBuiltin || m.IsCustom {
					continue
				}
				oldM := getProviderModel(oldActive, m.ModelName)
				// If key changed or a new key was added where none existed
				if (oldM != nil && m.ApiKey != oldM.ApiKey) || (oldM == nil && m.ApiKey != "") {
					intentions[strings.ToLower(m.ModelName)] = m.ApiKey
					a.log(fmt.Sprintf("Sync: detected %s intent from active tool %s", m.ModelName, activeToolName))
				}
			}
		}
	}
	// 2. Detect Intent from other tools (if not already captured from active tool)
	for name, tool := range tools {
		if name == activeToolName {
			continue
		}
		oldTool := oldTools[name]
		if oldTool == nil {
			continue
		}
		for _, m := range tool.Models {
			if m.IsBuiltin || m.IsCustom {
				continue
			}
			lowerName := strings.ToLower(m.ModelName)
			if _, handled := intentions[lowerName]; handled {
				continue
			}
			oldM := getProviderModel(oldTool, m.ModelName)
			if (oldM != nil && m.ApiKey != oldM.ApiKey) || (oldM == nil && m.ApiKey != "") {
				intentions[lowerName] = m.ApiKey
				a.log(fmt.Sprintf("Sync: detected %s intent from tool %s", m.ModelName, name))
			}
		}
	}
	// 3. Propagate all intentions to ALL tools
	for providerLower, targetKey := range intentions {
		for _, tool := range tools {
			for i := range tool.Models {
				if strings.ToLower(tool.Models[i].ModelName) == providerLower {
					if tool.Models[i].ApiKey != targetKey {
						tool.Models[i].ApiKey = targetKey
					}
				}
			}
		}
	}
}
func (a *App) hubSecurityExplicitlyCentralizedFalse() bool {
	if a == nil {
		return false
	}
	policy := a.hubSecurityCache.get()
	return policy != nil && !policy.CentralizedSecurity
}

func sanitizeCodingToolSelection(config *corelib.AppConfig) {
	if tool, ok := normalizeCodingToolSelection(config.ActiveTool); ok {
		config.ActiveTool = tool
	} else {
		config.ActiveTool = "claude"
	}
	if strings.TrimSpace(config.DefaultTool) != "" {
		if tool, ok := normalizeCodingToolSelection(config.DefaultTool); ok {
			config.DefaultTool = tool
		} else {
			config.DefaultTool = "claude"
		}
	}
}

func removeDisallowedCodingToolProviders(config *corelib.AppConfig) {
	removeProvider := func(toolCfg *corelib.ToolConfig, name string) {
		models := toolCfg.Models[:0]
		for _, model := range toolCfg.Models {
			if !strings.EqualFold(model.ModelName, name) {
				models = append(models, model)
			}
		}
		toolCfg.Models = models
		if strings.EqualFold(toolCfg.CurrentModel, name) {
			if len(toolCfg.Models) > 0 {
				toolCfg.CurrentModel = toolCfg.Models[0].ModelName
			} else {
				toolCfg.CurrentModel = "Original"
			}
		}
	}
	for _, toolCfg := range []*corelib.ToolConfig{
		&config.Claude,
		&config.Codex,
		&config.Opencode,
		&config.CodeBuddy,
		&config.IFlow,
		&config.Kilo,
	} {
		removeProvider(toolCfg, "ChatFire")
	}
}

func normalizeCodingToolSelection(tool string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(tool))
	switch normalized {
	case "claude", "codex", "opencode", "codebuddy", "iflow", "kilo":
		return normalized, true
	default:
		return "", false
	}
}

func (a *App) shouldPreserveHubManagedSecurity(current corelib.AppConfig) bool {
	if !current.HubSecurityCentralized {
		return false
	}
	return !a.hubSecurityExplicitlyCentralizedFalse()
}

// preserveBackendOwnedFields prevents a stale frontend snapshot (from SaveConfig)
// from overwriting fields that backend goroutines manage concurrently.
//
// SaveConfig is called from the Model Settings panel save button. The frontend
// snapshot was loaded when the panel opened  - any backend changes since then
// (credentials, LLM provider, onboarding, hub state) would be silently reverted
// without this protection.
//
// Strategy: for a small, explicit set of "backend-owned" fields, always use the
// authoritative on-disk value regardless of what the frontend sent. The frontend
// must use PatchConfigFields to modify these fields (which runs atomically under
// configMu). All other fields pass through from the frontend snapshot unchanged.
//
// This is the inverse of a whitelist: we enumerate the fields the frontend must
// NOT overwrite (small, stable set managed by backend goroutines), rather than
// the fields it CAN overwrite (large, grows with every new setting).
func sameOptionalBool(left, right *bool) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func preserveBackendOwnedFields(incoming *corelib.AppConfig, ondisk *corelib.AppConfig) {
	var restored []string

	// ── Remote credentials (ActivateRemote, clearMachineCredentials, persistViewerToken) ──
	if incoming.RemoteMachineID != ondisk.RemoteMachineID {
		restored = append(restored, "remote_machine_id")
	}
	incoming.RemoteMachineID = ondisk.RemoteMachineID
	incoming.RemoteMachineToken = ondisk.RemoteMachineToken
	incoming.RemoteViewerToken = ondisk.RemoteViewerToken
	incoming.RemoteSN = ondisk.RemoteSN
	incoming.RemoteUserID = ondisk.RemoteUserID
	incoming.RemoteClientID = ondisk.RemoteClientID
	incoming.RemoteHubID = ondisk.RemoteHubID
	incoming.RemoteTenantID = ondisk.RemoteTenantID
	incoming.RemoteTenantName = ondisk.RemoteTenantName
	incoming.RemoteNickname = ondisk.RemoteNickname
	incoming.RemoteMachineName = ondisk.RemoteMachineName
	incoming.SkillMarketSessionToken = ondisk.SkillMarketSessionToken

	// Hardware enablement owns a multi-field transport invariant and must only
	// change through SetHardwareEnabled. Preserve the complete tuple so an older
	// full-config form cannot silently re-enable local mode or disable its gateway.
	if incoming.HardwareEnabled != ondisk.HardwareEnabled ||
		incoming.ThirdPartyGatewayEnabled != ondisk.ThirdPartyGatewayEnabled ||
		incoming.ThirdPartyGatewayToken != ondisk.ThirdPartyGatewayToken ||
		!sameOptionalBool(incoming.ThirdPartyGatewayLocalMode, ondisk.ThirdPartyGatewayLocalMode) {
		restored = append(restored, "hardware_gateway_transport")
	}
	incoming.HardwareEnabled = ondisk.HardwareEnabled
	incoming.ThirdPartyGatewayEnabled = ondisk.ThirdPartyGatewayEnabled
	incoming.ThirdPartyGatewayToken = ondisk.ThirdPartyGatewayToken
	incoming.ThirdPartyGatewayLocalMode = ondisk.ThirdPartyGatewayLocalMode

	// ── Local hardware labels (SetThirdPartyHardwareDeviceAlias) ──
	// Display labels are saved independently of the full settings form. Keep a
	// stale form snapshot from erasing a recent rename or restoring a label for
	// an already-unbound device.
	incoming.HardwareDeviceAliases = ondisk.HardwareDeviceAliases

	// ── MaClaw LLM provider state (SaveMaclawLLMProviders, syncHubLLMServiceStatusToConfig) ──
	if incoming.MaclawLLMCurrentProvider != ondisk.MaclawLLMCurrentProvider {
		restored = append(restored, "maclaw_llm_current_provider("+incoming.MaclawLLMCurrentProvider+"->"+ondisk.MaclawLLMCurrentProvider+")")
	}
	incoming.MaclawLLMCurrentProvider = ondisk.MaclawLLMCurrentProvider
	incoming.MaclawLLMUrl = ondisk.MaclawLLMUrl
	incoming.MaclawLLMKey = ondisk.MaclawLLMKey
	incoming.MaclawLLMModel = ondisk.MaclawLLMModel
	incoming.MaclawLLMProtocol = ondisk.MaclawLLMProtocol
	incoming.MaclawLLMTimeoutSec = ondisk.MaclawLLMTimeoutSec
	incoming.MaclawLLMContextLength = ondisk.MaclawLLMContextLength
	incoming.MaclawLLMProviders = ondisk.MaclawLLMProviders

	// ── Onboarding (PatchConfigFields from useRemotePanel) ──
	if incoming.OnboardingDone != ondisk.OnboardingDone {
		restored = append(restored, fmt.Sprintf("onboarding_done(%v->%v)", incoming.OnboardingDone, ondisk.OnboardingDone))
	}
	incoming.OnboardingDone = ondisk.OnboardingDone

	// ── HubCenter URLs (failover persistence) ──
	incoming.RemoteHubCenterURLs = ondisk.RemoteHubCenterURLs

	// ── LLM Token Usage (AccumulateLLMTokenUsageWithCache, ResetLLMTokenUsage) ──
	// This field is only modified by backend via PatchConfig. A stale frontend
	// snapshot must not overwrite accumulated token counts. Unconditionally
	// restore from on-disk, same as other backend-owned fields.
	if ondisk.LLMTokenUsage != nil && incoming.LLMTokenUsage != nil {
		// Both non-nil: check if incoming would lose data (lower totals).
		for k, ondiskStat := range ondisk.LLMTokenUsage {
			if inStat, ok := incoming.LLMTokenUsage[k]; !ok || inStat == nil || inStat.TotalTokens < ondiskStat.TotalTokens {
				restored = append(restored, "llm_token_usage")
				break
			}
		}
	}
	incoming.LLMTokenUsage = ondisk.LLMTokenUsage

	// ── Working directory (SetTabWorkingDir for local tab) ──
	// Only PatchConfigFields should modify this. A stale frontend snapshot
	// must not accidentally set it to a Project Tab's directory.
	if incoming.WorkingDirectory != ondisk.WorkingDirectory {
		restored = append(restored, fmt.Sprintf("working_directory(%s->%s)", incoming.WorkingDirectory, ondisk.WorkingDirectory))
	}
	incoming.WorkingDirectory = ondisk.WorkingDirectory

	if len(restored) > 0 {
		log.Printf("[config] SaveConfig:preserved_backend_fields=%v", restored)
	}
}

func (a *App) SaveConfig(config corelib.AppConfig) error {
	lockStart := time.Now()
	a.configMu.Lock()
	lockWait := time.Since(lockStart)
	if lockWait > 50*time.Millisecond {
		log.Printf("[config] SaveConfig:lock_wait=%s", lockWait)
	}
	start := time.Now()
	log.Printf("[config] SaveConfig:start")

	path, err := a.getConfigPath()
	if err != nil {
		a.unlockConfigAbort(config)
		log.Printf("[config] SaveConfig:get_path_failed after=%s err=%v", time.Since(start), err)
		return err
	}
	log.Printf("[config] SaveConfig:path=%q configured_data_dir=%q configured_working_dir=%q", path, strings.TrimSpace(config.DataDir), strings.TrimSpace(config.WorkingDirectory))
	if err := a.migrateLegacyConfigLocked(path); err != nil {
		a.invalidateConfigCacheLocked()
		a.unlockConfigAbort(config)
		log.Printf("[config] SaveConfig:migrate_failed after=%s err=%v", time.Since(start), err)
		return err
	}
	// Sanitize: Ensure Custom models have a name (prevent empty tab button)
	sanitizeCustomNames := func(models []corelib.ModelConfig) {
		for i := range models {
			if models[i].IsCustom && strings.TrimSpace(models[i].ModelName) == "" {
				models[i].ModelName = "Custom"
			}
		}
	}
	sanitizeCustomNames(config.Claude.Models)
	sanitizeCustomNames(config.Codex.Models)
	sanitizeCustomNames(config.Opencode.Models)
	sanitizeCustomNames(config.CodeBuddy.Models)
	sanitizeCustomNames(config.IFlow.Models)
	sanitizeCustomNames(config.Kilo.Models)
	removeDisallowedCodingToolProviders(&config)
	sanitizeCodingToolSelection(&config)
	config.SubAgentConcurrency = corelib.NormalizeSubAgentConcurrency(config.SubAgentConcurrency)
	config.RemoteHeartbeatSec = corelib.NormalizeRemoteHeartbeatIntervalSec(config.RemoteHeartbeatSec)
	normalizeConfigTimeouts(&config)
	sanitizePetConfig(&config)
	// Load old config to compare for sync logic.
	// Use the in-memory configCache (authoritative under configMu) rather than
	// re-reading from disk  - avoids Windows SHARING_VIOLATION if an antivirus
	// or sync tool has the file open, and avoids the edge case where the file
	// doesn't exist yet (first run).
	var oldConfig corelib.AppConfig
	oldConfigLoaded := false
	if a.configCacheValid {
		oldConfig = a.configCache
		oldConfigLoaded = true
	} else if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &oldConfig) == nil {
			oldConfigLoaded = true
		}
	}
	config.LLMPromptCache = config.LLMPromptCache.WithDefaults()
	oldCacheConfig := corelib.DefaultLLMPromptCacheConfig()
	if oldConfigLoaded {
		oldCacheConfig = oldConfig.LLMPromptCache
	}
	if err := migrateLLMPromptCacheDirIfNeeded(oldCacheConfig, config.LLMPromptCache); err != nil {
		a.invalidateConfigCacheLocked()
		a.unlockConfigAbort(config)
		log.Printf("[config] SaveConfig:llm_cache_migrate_failed after=%s err=%v", time.Since(start), err)
		return err
	}
	if strings.TrimSpace(oldConfig.DefaultLaunchMode) != "" && !userDataMigrationIsConfigRestore(a) {
		config.DefaultLaunchMode = oldConfig.DefaultLaunchMode
	}
	// ── Backend-owned field preservation ──────────────────────────────────
	// The frontend SaveConfig receives a full config snapshot that may be
	// stale by the time it arrives (user opened model settings, edited API
	// keys, then clicked Save  - meanwhile backend goroutines updated
	// credentials, LLM provider state, onboarding flags via PatchConfig).
	// Without this, the stale snapshot would overwrite concurrent updates.
	//
	// Strategy: unconditionally restore a small set of backend-owned fields
	// from the authoritative on-disk config. These fields are only modified
	// by backend goroutines through PatchConfig (which is atomic under
	// configMu). The frontend must use PatchConfigFields to modify them.
	if oldConfigLoaded && strings.TrimSpace(a.testHomeDir) == "" && !userDataMigrationIsConfigRestore(a) {
		preserveBackendOwnedFields(&config, &oldConfig)
	}
	if a.shouldPreserveHubManagedSecurity(oldConfig) {
		clientsecurity.PreserveHubManagedSecurityConfig(oldConfig, &config)
	} else if a.hubSecurityExplicitlyCentralizedFalse() {
		config.HubSecurityCentralized = false
	}
	// Sync all apikeys across all tools before saving
	syncStart := time.Now()
	if !userDataMigrationIsConfigRestore(a) {
		syncAllProviderApiKeys(a, &oldConfig, &config)
	}
	log.Printf("[config] SaveConfig:sync_api_keys=%s", time.Since(syncStart))
	corelib.SetLogDetailEnabled(config.LogDetailEnabled)
	memory.SetMemoryRecallLogEnabled(config.MemoryRecallLogEnabled)
	// Sync workflow enabled/disabled state to the atomic flag so that
	// getWorkflowEngine() returns nil when workflow is disabled. This is
	// the single enforcement point -all workflow consumers go through
	// getWorkflowEngine(), so no per-consumer guards are needed.
	a.workflowDisabled.Store(!config.IsWorkflowEnabled())
	policyModeChanged := a.policyEngine != nil && config.SecurityPolicyMode != oldConfig.SecurityPolicyMode
	floatingChanged := floatingAppearanceChanged(oldConfig, config)
	motionChanged := floatingMotionChanged(oldConfig, config)
	soundChanged := floatingSoundChanged(oldConfig, config)
	devicePetChanged := !oldConfigLoaded || devicePetProfileChanged(oldConfig, config)
	hardwareCustomPetsDisabled := oldConfig.HardwareAllowCustomPets && !config.HardwareAllowCustomPets
	hardwareWelcomeChanged := !oldConfigLoaded || hardwareWelcomeConfigChanged(oldConfig, config)
	hardwareVolumeChanged := !oldConfigLoaded || hardwareVolumeChanged(oldConfig, config)
	hubClient := (*RemoteHubClient)(nil)
	if a.remoteSessions != nil {
		hubClient = a.remoteSessions.hubClient
	}
	// Unified epilogue: publish cache → unlock → post-unlock drains → disk write.
	// See config_txn.go protocol.
	writeStart := time.Now()
	if err := a.unlockConfigAndFinish(config, path, true); err != nil {
		a.configMu.Lock()
		a.invalidateConfigCacheLocked()
		a.configMu.Unlock()
		log.Printf("[config] SaveConfig:save_to_path_failed after=%s err=%v", time.Since(writeStart), err)
		return err
	}
	log.Printf("[config] SaveConfig:save_to_path=%s", time.Since(writeStart))

	stepStart := time.Now()
	a.refreshPowerOptimizationStateFromConfig(config)
	log.Printf("[config] SaveConfig:refresh_power_optimization=%s", time.Since(stepStart))
	stepStart = time.Now()
	a.refreshWorkstationMode(config)
	log.Printf("[config] SaveConfig:refresh_workstation_mode=%s", time.Since(stepStart))
	// Apply user-configured working directory change immediately.
	corelib.SetWorkspaceDir(config.WorkingDirectory)
	if policyModeChanged {
		stepStart = time.Now()
		a.policyEngine.SetMode(config.SecurityPolicyMode)
		log.Printf("[config] SaveConfig:set_policy_mode=%s", time.Since(stepStart))
	}
	if OnConfigChanged != nil {
		go OnConfigChanged(config)
	}
	if floatingChanged {
		go func(cfg corelib.AppConfig) {
			if cfg.PetEnabled {
				if fa := a.ensureFloatingAssistant(); fa != nil {
					fa.RefreshAppearance(cfg)
				}
				return
			}
			if fa := a.existingFloatingAssistant(); fa != nil {
				fa.RefreshAppearance(cfg)
			}
		}(config)
	}
	if !floatingChanged && (soundChanged || motionChanged) {
		go func(cfg corelib.AppConfig) {
			if fa := a.existingFloatingAssistant(); fa != nil {
				// Keep the requested sound preference current before recalculating
				// quiet/reduced-motion policy for the live window.
				if soundChanged {
					fa.UpdateSoundConfig(cfg)
				}
				if motionChanged {
					fa.UpdateMotionConfig(cfg)
				}
			}
		}(config)
	}
	if (devicePetChanged && !config.HardwareAllowCustomPets || hardwareCustomPetsDisabled) && a.thirdPartyGateway != nil {
		// Local Gateway mode has no Hub relay; broadcast to paired devices
		// directly so they follow the same settings change in real time. Runs
		// outside the hubClient guard because local mode may have no Hub client.
		go func(cfg corelib.AppConfig) {
			a.thirdPartyGateway.broadcastPetProfile(a.devicePetProfileForConfig(cfg))
		}(config)
	}
	if hubClient != nil {
		if oldConfigLoaded && oldConfig.RemoteHeartbeatSec != config.RemoteHeartbeatSec {
			hubClient.InvalidateHeartbeatIntervalCache()
		}
		go func(client *RemoteHubClient, cfg corelib.AppConfig, syncPet, syncWelcome, syncVolume bool) {
			if client.IsConnected() {
				if syncPet {
					if err := client.SendDeviceGatewayPetProfile(cfg); err != nil {
						log.Printf("[device-pet] profile sync failed: %v", err)
					}
				}
				if syncWelcome {
					if err := a.SyncHardwareWelcome(); err != nil {
						log.Printf("[device-welcome] configuration sync failed: %v", err)
					}
				}
				if syncVolume {
					if err := a.SyncHardwareVolume(); err != nil {
						log.Printf("[device-volume] configuration sync failed: %v", err)
					}
				}
				client.SyncLaunchProjects()
			}
		}(hubClient, config, (devicePetChanged && !config.HardwareAllowCustomPets) || hardwareCustomPetsDisabled, hardwareWelcomeChanged, hardwareVolumeChanged)
	}
	log.Printf("[config] SaveConfig:done total=%s config_path=%q configured_data_dir=%q configured_working_dir=%q effective_base_dir=%q effective_data_dir=%q ai_conversation=%q",
		time.Since(start), path, strings.TrimSpace(config.DataDir), strings.TrimSpace(config.WorkingDirectory), a.getMaclawBaseDir(), a.GetDataDir(), filepath.Join(a.GetDataDir(), "ai_assistant_conversation.json"))
	return nil
}

// SkillEvolutionStatus is the Wails-facing snapshot of the desktop EvolutionPipeline.
type SkillEvolutionStatus struct {
	PipelineStarted        bool   `json:"pipeline_started"`
	PendingSkills          int    `json:"pending_skills"`
	CoalescedNotifications uint64 `json:"coalesced_notifications"`
	DroppedNotifications   uint64 `json:"dropped_notifications"`
	ProcessedRequests      int    `json:"processed_requests"`
	EnableRepair           bool   `json:"enable_repair"`
	EnableOptimizer        bool   `json:"enable_optimizer"`
	EnablePromoter         bool   `json:"enable_promoter"`
	RepairCooldown         string `json:"repair_cooldown"`
	HasRepairHook          bool   `json:"has_repair_hook"`
	HasOptimizer           bool   `json:"has_optimizer"`
	HasPromoter            bool   `json:"has_promoter"`
	RepairCooldownHours    int    `json:"repair_cooldown_hours"`
	// EnvDisabled is true when MACLAW_DISABLE_SKILL_EVOLUTION is set.
	EnvDisabled bool `json:"env_disabled"`
	// ConfigEnabled is the persisted skill_evolution_enabled setting (default true).
	ConfigEnabled bool `json:"config_enabled"`
	// ConfigDisabled is true when the user turned off evolution in settings.
	ConfigDisabled bool `json:"config_disabled"`
	// Disabled is true when evolution is fully suppressed (env and/or config).
	Disabled bool `json:"disabled"`
}

// ListSkillYAMLBackups returns Versioner backup versions for a named skill's SkillDir.
func (a *App) ListSkillYAMLBackups(skillName string) map[string]interface{} {
	out := map[string]interface{}{
		"ok":       true,
		"skill":    skillName,
		"versions": []int{},
		"latest":   0,
	}
	entry, err := a.findNLSkillEntry(skillName)
	if err != nil {
		out["ok"] = false
		out["error"] = err.Error()
		return out
	}
	dir := strings.TrimSpace(entry.SkillDir)
	if dir == "" {
		out["ok"] = false
		out["error"] = "skill has no skill_dir (not file-backed)"
		return out
	}
	v := &skill.Versioner{}
	versions := v.ListVersions(dir)
	out["skill_dir"] = dir
	out["versions"] = versions
	out["latest"] = v.LatestVersion(dir)
	return out
}

// RestoreSkillYAMLBackup restores skill.yaml from Versioner backup v{version}.
// version<=0 means latest. confirm must be true. Always pre-backs up current first.
func (a *App) RestoreSkillYAMLBackup(skillName string, version int, confirm bool) map[string]interface{} {
	out := map[string]interface{}{
		"ok":      false,
		"skill":   skillName,
		"version": version,
	}
	if !confirm {
		out["error"] = "confirm=true is required"
		return out
	}
	entry, err := a.findNLSkillEntry(skillName)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	dir := strings.TrimSpace(entry.SkillDir)
	if dir == "" {
		out["error"] = "skill has no skill_dir (not file-backed)"
		return out
	}
	v := &skill.Versioner{}
	var written string
	var restored, pre int
	if version <= 0 {
		written, restored, pre, err = v.RestoreLatest(dir, true)
	} else {
		written, pre, err = v.RestoreVersion(dir, version, true)
		restored = version
	}
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	// Best-effort: refresh indexes so loaders pick up YAML.
	if a.toolRouter != nil {
		a.toolRouter.RefreshSkillIndex()
	}
	skill.RecordEvolutionEvent("skill:yaml_restore", map[string]string{
		"skill":       skillName,
		"explanation": fmt.Sprintf("restored v%d path=%s pre_backup=v%d", restored, written, pre),
	}, "desktop")
	out["ok"] = true
	out["version"] = restored
	out["written_path"] = written
	out["pre_backup_version"] = pre
	out["message"] = fmt.Sprintf("restored skill.yaml from v%d (current saved as v%d)", restored, pre)
	return out
}

// findNLSkillEntry locates a skill by name in the desktop skill executor list.
func (a *App) findNLSkillEntry(skillName string) (*corelib.NLSkillEntry, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	a.ensureSkillRunner()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	// loadSkills is self-synchronized; do not hold skillExecutor.mu (configMu inversion).
	skills := a.skillExecutor.loadSkills()
	for i := range skills {
		if skills[i].MatchesName(skillName) || skills[i].Name == skillName {
			cp := skill.CloneNLSkillEntry(&skills[i])
			if cp == nil {
				e := skills[i]
				return &e, nil
			}
			return cp, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", skillName)
}

// ApplySkillMaintenanceAction applies one approved maintenance draft action.
// kind: improve_contract | merge_duplicate
// skill: primary skill name
// relatedSkill: required for merge_duplicate (the other skill in the pair)
// confirm: must be true for real apply (dry_run=false)
// allowDuplicateRetire: required for merge_duplicate real apply
func (a *App) ApplySkillMaintenanceAction(kind, skillName, relatedSkill string, confirm, allowDuplicateRetire bool) map[string]interface{} {
	out := map[string]interface{}{
		"ok":      false,
		"action":  kind,
		"skill":   skillName,
		"related": relatedSkill,
	}
	if a == nil {
		out["error"] = "app not initialized"
		return out
	}
	a.ensureSkillRunner()
	if a.skillExecutor == nil {
		out["error"] = "skill executor not initialized"
		return out
	}
	var updated []corelib.NLSkillEntry
	var res skill.TargetedMaintenanceApplyResult
	if err := a.skillExecutor.withSkillListMutate(func() error {
		skills := a.skillExecutor.loadSkills()
		var saveErr error
		updated, res = skill.ApplyTargetedMaintenanceAction(
			skills,
			kind,
			skillName,
			relatedSkill,
			false, // dry_run always false for this binding (UI already confirmed)
			confirm,
			allowDuplicateRetire,
		)
		if res.OK && res.Result.ExecutedCount > 0 {
			if err := a.skillExecutor.saveSkills(updated); err != nil {
				saveErr = err
			}
		}
		return saveErr
	}); err != nil {
		out["error"] = "save failed: " + err.Error()
		out["result"] = res
		return out
	}

	if res.OK && res.RequiresIndexRefresh && res.Result.ExecutedCount > 0 {
		if a.toolRouter != nil {
			a.toolRouter.RefreshSkillIndex()
		}
	}
	// Audit row for apply attempts.
	skill.RecordEvolutionEvent("skill:maintenance_apply", map[string]string{
		"skill":       skillName,
		"explanation": fmt.Sprintf("action=%s related=%s ok=%v msg=%s err=%s", kind, relatedSkill, res.OK, res.Message, res.Error),
	}, "desktop")

	out["ok"] = res.OK
	out["error"] = res.Error
	out["message"] = res.Message
	out["result"] = res.Result
	out["requires_index_refresh"] = res.RequiresIndexRefresh
	if res.BackupVersion > 0 {
		out["backup_version"] = res.BackupVersion
	}
	if res.WrittenPath != "" {
		out["written_path"] = res.WrittenPath
	}
	if res.PatchDraft != nil {
		out["patch_draft"] = res.PatchDraft
	}
	if res.MergeDraft != nil {
		out["merge_draft"] = res.MergeDraft
	}
	return out
}

// ListSkillMaintenanceDrafts returns read-only patch/merge review drafts from a
// dry-run of the local skill maintenance plan. Never mutates skills.
func (a *App) ListSkillMaintenanceDrafts() map[string]interface{} {
	out := map[string]interface{}{
		"ok":            true,
		"non_executing": true,
		"boundary":      "read-only maintenance review drafts; no skill was modified",
		"patch_drafts":  []interface{}{},
		"merge_drafts":  []interface{}{},
		"queued_repair": []interface{}{},
	}
	if a == nil {
		out["ok"] = false
		out["error"] = "app not initialized"
		return out
	}
	a.ensureSkillRunner()
	if a.skillExecutor == nil {
		out["ok"] = false
		out["error"] = "skill executor not initialized"
		return out
	}
	skills := a.skillExecutor.loadSkills()
	drafts := skill.CollectMaintenanceReviewDrafts(skills, skill.SkillMaintenancePlanOptions{
		Now:        time.Now(),
		MaxActions: 40,
	})
	out["generated_at"] = drafts.GeneratedAt
	out["plan_summary"] = drafts.PlanSummary
	out["plan_actions"] = drafts.PlanActions
	out["patch_drafts"] = drafts.PatchDrafts
	out["merge_drafts"] = drafts.MergeDrafts
	out["queued_repair"] = drafts.QueuedRepair
	out["patch_count"] = len(drafts.PatchDrafts)
	out["merge_count"] = len(drafts.MergeDrafts)
	return out
}

// ListSkillEvolutionAudit returns the newest durable evolution audit rows
// (JSONL under ~/.maclaw/skill_evolution/audit.jsonl). limit defaults to 50.
func (a *App) ListSkillEvolutionAudit(limit int) []map[string]interface{} {
	events, err := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), limit)
	if err != nil || len(events) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(events))
	for _, ev := range events {
		row := map[string]interface{}{
			"timestamp":   ev.Timestamp,
			"kind":        ev.Kind,
			"skill":       ev.Skill,
			"explanation": ev.Explanation,
			"source":      ev.Source,
		}
		// Optional fields (omitempty in the JSONL): only include when present
		// so older rows keep their original shape.
		if ev.Status != "" {
			row["status"] = ev.Status
		}
		if ev.Via != "" {
			row["via"] = ev.Via
		}
		out = append(out, row)
	}
	return out
}

// GetSkillEvolutionStatus returns desktop skill evolution pipeline diagnostics.
// Wails binding for Skills Management → Evolution settings.
func (a *App) GetSkillEvolutionStatus() SkillEvolutionStatus {
	out := SkillEvolutionStatus{}
	if a == nil {
		return out
	}
	out.EnvDisabled = skill.EvolutionEnvDisabled()
	out.ConfigEnabled = true
	if cfg, err := a.LoadConfig(); err == nil {
		out.RepairCooldownHours = cfg.SkillEvolutionRepairCooldownHours
		if out.RepairCooldownHours <= 0 {
			out.RepairCooldownHours = 1
		}
		out.ConfigEnabled = cfg.IsSkillEvolutionEnabled()
	}
	out.ConfigDisabled = !out.ConfigEnabled
	out.Disabled = out.EnvDisabled || out.ConfigDisabled
	// Ensure pipeline exists so status reflects live wiring when skills have run.
	// Still build status when kill-switched so operators can see the disabled flag.
	a.ensureEvolutionPipeline()
	if a.evolutionPipeline == nil {
		return out
	}
	st := a.evolutionPipeline.Status()
	out.PipelineStarted = true
	out.PendingSkills = st.PendingSkills
	out.CoalescedNotifications = st.CoalescedNotifications
	out.DroppedNotifications = st.DroppedNotifications
	out.ProcessedRequests = st.ProcessedRequests
	out.EnableRepair = st.EnableRepair && !out.Disabled
	out.EnableOptimizer = st.EnableOptimizer && !out.Disabled
	out.EnablePromoter = st.EnablePromoter && !out.Disabled
	out.RepairCooldown = st.RepairCooldown.String()
	out.HasRepairHook = st.HasRepairHook
	out.HasOptimizer = st.HasOptimizer
	out.HasPromoter = st.HasPromoter
	return out
}

// parseLansengerGroupIDList accepts []string / []any from the settings UI.
func parseLansengerGroupIDList(key string, value interface{}) ([]string, error) {
	switch raw := value.(type) {
	case nil:
		return nil, nil
	case []string:
		ids := make([]string, 0, len(raw))
		for _, id := range raw {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		return ids, nil
	case []any:
		ids := make([]string, 0, len(raw))
		for _, item := range raw {
			id, ok := item.(string)
			if !ok {
				continue
			}
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("%s must be a string array", key)
	}
}

// parseLansengerGroupPermissionList validates owner-configured group-bot
// permissions. Values are deduplicated after trimming so a hand-edited config
// cannot grow unbounded or accidentally grant the same item multiple times.
func parseLansengerGroupPermissionList(key string, value interface{}, maxItems int) ([]string, error) {
	values, err := parseLansengerGroupIDList(key, value)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if maxItems > 0 && len(result) > maxItems {
			return nil, fmt.Errorf("%s exceeds the maximum of %d entries", key, maxItems)
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// PatchConfigFields is a frontend-safe atomic config patch endpoint.
// It only accepts whitelisted small settings so stale frontend snapshots cannot
// overwrite unrelated config fields while a user toggles general settings.
func (a *App) PatchConfigFields(patch map[string]interface{}) (corelib.AppConfig, error) {
	if len(patch) == 0 {
		return a.LoadConfig()
	}

	boolField := func(key string, value interface{}) (bool, error) {
		v, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("config field %q must be boolean", key)
		}
		return v, nil
	}
	stringField := func(key string, value interface{}) (string, error) {
		v, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("config field %q must be string", key)
		}
		return v, nil
	}
	intField := func(key string, value interface{}) (int, error) {
		switch v := value.(type) {
		case int:
			return v, nil
		case float64:
			return int(v), nil
		case float32:
			return int(v), nil
		default:
			return 0, fmt.Errorf("config field %q must be number", key)
		}
	}
	floatField := func(key string, value interface{}) (float64, error) {
		switch v := value.(type) {
		case float64:
			return v, nil
		case float32:
			return float64(v), nil
		case int:
			return float64(v), nil
		default:
			return 0, fmt.Errorf("config field %q must be number", key)
		}
	}
	cacheField := func(key string, value interface{}) (corelib.LLMPromptCacheConfig, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return corelib.LLMPromptCacheConfig{}, fmt.Errorf("config field %q must be object: %w", key, err)
		}
		var cache corelib.LLMPromptCacheConfig
		if err := json.Unmarshal(data, &cache); err != nil {
			return corelib.LLMPromptCacheConfig{}, fmt.Errorf("config field %q must be object: %w", key, err)
		}
		return cache.WithDefaults(), nil
	}
	modelRoutesField := func(key string, value interface{}) (map[string]corelib.ModelRouteConfig, error) {
		if value == nil {
			return nil, nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config field %q must be object: %w", key, err)
		}
		// Empty object clears routes.
		if string(data) == "null" || string(data) == "{}" {
			return map[string]corelib.ModelRouteConfig{}, nil
		}
		var routes map[string]corelib.ModelRouteConfig
		if err := json.Unmarshal(data, &routes); err != nil {
			return nil, fmt.Errorf("config field %q must be model_routes object: %w", key, err)
		}
		// Normalize keys; drop empty model entries.
		out := make(map[string]corelib.ModelRouteConfig, len(routes))
		for k, v := range routes {
			k = strings.ToLower(strings.TrimSpace(k))
			v.Model = strings.TrimSpace(v.Model)
			if k == "" || v.Model == "" {
				continue
			}
			v.URL = strings.TrimSpace(v.URL)
			v.Key = strings.TrimSpace(v.Key)
			v.Protocol = strings.TrimSpace(v.Protocol)
			v.Provider = strings.TrimSpace(v.Provider)
			out[k] = v
		}
		return out, nil
	}
	toolCacheMaintenanceField := func(key string, value interface{}, base corelib.ToolCacheMaintenanceConfig) (corelib.ToolCacheMaintenanceConfig, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return corelib.ToolCacheMaintenanceConfig{}, fmt.Errorf("config field %q must be object: %w", key, err)
		}
		cfg := base.WithDefaults()
		if err := json.Unmarshal(data, &cfg); err != nil {
			return corelib.ToolCacheMaintenanceConfig{}, fmt.Errorf("config field %q must be object: %w", key, err)
		}
		return cfg.WithDefaults(), nil
	}
	projectsField := func(key string, value interface{}) ([]corelib.ProjectConfig, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config field %q must be array: %w", key, err)
		}
		var projects []corelib.ProjectConfig
		if err := json.Unmarshal(data, &projects); err != nil {
			return nil, fmt.Errorf("config field %q must be array: %w", key, err)
		}
		return projects, nil
	}
	stringSliceField := func(key string, value interface{}) ([]string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config field %q must be string array: %w", key, err)
		}
		var values []string
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("config field %q must be string array: %w", key, err)
		}
		return values, nil
	}
	stringMapField := func(key string, value interface{}) (map[string]string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config field %q must be string map: %w", key, err)
		}
		var values map[string]string
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("config field %q must be string map: %w", key, err)
		}
		return values, nil
	}
	toolCurrentModelField := func(key string, value interface{}) (string, string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return "", "", fmt.Errorf("config field %q must be object: %w", key, err)
		}
		var patch struct {
			Tool  string `json:"tool"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(data, &patch); err != nil {
			return "", "", fmt.Errorf("config field %q must be object: %w", key, err)
		}
		tool := strings.TrimSpace(strings.ToLower(patch.Tool))
		model := strings.TrimSpace(patch.Model)
		if _, ok := normalizeCodingToolSelection(tool); !ok {
			return "", "", fmt.Errorf("config field %q has invalid tool: %s", key, patch.Tool)
		}
		if model == "" {
			return "", "", fmt.Errorf("config field %q model cannot be empty", key)
		}
		return tool, model, nil
	}
	setToolCurrentModel := func(cfg *corelib.AppConfig, tool, model string) error {
		var toolCfg *corelib.ToolConfig
		switch tool {
		case "claude":
			toolCfg = &cfg.Claude
		case "codex":
			toolCfg = &cfg.Codex
		case "opencode":
			toolCfg = &cfg.Opencode
		case "codebuddy":
			toolCfg = &cfg.CodeBuddy
		case "iflow":
			toolCfg = &cfg.IFlow
		case "kilo":
			toolCfg = &cfg.Kilo
		default:
			return fmt.Errorf("invalid coding tool: %s", tool)
		}
		for _, candidate := range toolCfg.Models {
			if strings.EqualFold(candidate.ModelName, model) {
				toolCfg.CurrentModel = candidate.ModelName
				return nil
			}
		}
		return fmt.Errorf("model %q not found for tool %s", model, tool)
	}

	a.configMu.Lock()
	cfg, err := a.getConfigForMutationLocked()
	if err != nil {
		a.unlockConfigAbort(corelib.AppConfig{})
		return corelib.AppConfig{}, err
	}
	current := cfg
	// Unified abort: any early return before unlockConfigAndFinish runs post-unlock drains.
	configTxnCommitted := false
	defer func() {
		if !configTxnCommitted {
			a.unlockConfigAbort(cfg)
		}
	}()
	proxyChanged := false
	policyModeChanged := false
	imGatewayChanged := false
	acpHostChanged := false
	petChanged := false

	for key, value := range patch {
		switch key {
		case "language":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.Language = strings.TrimSpace(v)
		case "active_tool":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ActiveTool = strings.TrimSpace(v)
		case "current_project":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.CurrentProject = strings.TrimSpace(v)
		case "projects":
			v, err := projectsField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.Projects = v
		case "favorite_employees":
			v, err := stringSliceField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.FavoriteEmployees = v
		case "favorite_employee_names":
			v, err := stringMapField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.FavoriteEmployeeNames = v
		case "tool_current_model":
			tool, model, err := toolCurrentModelField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if err := setToolCurrentModel(&cfg, tool, model); err != nil {
				return corelib.AppConfig{}, err
			}
		case "remote_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.RemoteEnabled = v
		case "remote_hub_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.RemoteHubID = strings.TrimSpace(v)
		case "remote_hub_url":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.RemoteHubURL = strings.TrimSpace(v)
		case "remote_hubcenter_url":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			v = strings.TrimRight(strings.TrimSpace(v), "/")
			if v != "" && remote.IsLoopbackURL(v) {
				return corelib.AppConfig{}, fmt.Errorf("remote_hubcenter_url must not be a loopback address")
			}
			cfg.RemoteHubCenterURL = v
		case "remote_email":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.RemoteEmail = strings.TrimSpace(v)
		case "remote_mobile":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.RemoteMobile = strings.TrimSpace(v)
		case "onboarding_done":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.OnboardingDone = v
		case "prefer_beta_channel":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PreferBetaChannel = v
		case "default_launch_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			launchMode := normalizeLaunchModeKind(v)
			if launchMode == launchModeInvalid {
				return corelib.AppConfig{}, fmt.Errorf("invalid default launch mode: %s", v)
			}
			cfg.DefaultLaunchMode = string(launchMode)
		case "security_policy_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SecurityPolicyMode = strings.TrimSpace(v)
			policyModeChanged = cfg.SecurityPolicyMode != current.SecurityPolicyMode
		case "sandbox_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SandboxMode = strings.TrimSpace(v)
		case "network_level":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.NetworkLevel = strings.TrimSpace(v)
		case "network_allowlist":
			v, err := stringSliceField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.NetworkAllowlist = v
		case "yolo_mode_allowed":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.YoloModeAllowed = v
		case "smart_route_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SmartRouteEnabled = v
		case "gossip_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.GossipEnabled = v
		case "file_outbound_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.FileOutboundEnabled = v
		case "image_outbound_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ImageOutboundEnabled = v
		case "skill_sources_allowed":
			v, err := stringSliceField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SkillSourcesAllowed = v
		case "maclaw_role_name":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MaclawRoleName = strings.TrimSpace(v)
		case "maclaw_role_description":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MaclawRoleDescription = strings.TrimSpace(v)
		case "im_progress_nudge_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.IMProgressNudgeEnabled = &v
		case "knowledge_auto_recall_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.KnowledgeAutoRecallEnabled = &v
		case "knowledge_auto_recall_min_score":
			v, err := floatField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 {
				v = 0
			}
			if v > 10 {
				v = 10
			}
			cfg.KnowledgeAutoRecallMinScore = v
		case "tool_cache_maintenance":
			v, err := toolCacheMaintenanceField(key, value, cfg.ToolCacheMaintenance)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ToolCacheMaintenance = v
		case "qqbot_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.QQBotEnabled = v
			imGatewayChanged = true
		case "qqbot_app_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.QQBotAppID = strings.TrimSpace(v)
			imGatewayChanged = true
		case "qqbot_app_secret":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.QQBotAppSecret = v
			imGatewayChanged = true
		case "qqbot_owner_openid":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.QQBotOwnerOpenID = strings.TrimSpace(v)
			// Owner openid is for proactive push only; no gateway restart required.
		case "telegram_bot_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.TelegramBotEnabled = v
			imGatewayChanged = true
		case "telegram_bot_token":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.TelegramBotToken = strings.TrimSpace(v)
			imGatewayChanged = true
		case "telegram_owner_chat_id":
			// Accept string (preferred, full int64) or number from legacy UI.
			var id corelib.DecimalInt64String
			switch v := value.(type) {
			case string:
				if err := id.SetString(v); err != nil {
					return corelib.AppConfig{}, fmt.Errorf("config field %q: %w", key, err)
				}
			case float64:
				if v != float64(int64(v)) {
					return corelib.AppConfig{}, fmt.Errorf("config field %q must be integer", key)
				}
				id.SetInt64(int64(v))
			case int:
				id.SetInt64(int64(v))
			case int64:
				id.SetInt64(v)
			case nil:
				id = ""
			default:
				// coerce via string
				s := strings.TrimSpace(fmt.Sprint(value))
				if err := id.SetString(s); err != nil {
					return corelib.AppConfig{}, fmt.Errorf("config field %q must be chat id string/number", key)
				}
			}
			cfg.TelegramBotOwnerChatID = id
		case "lansenger_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerEnabled = v
			imGatewayChanged = true
		case "lansenger_app_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerAppID = strings.TrimSpace(v)
			imGatewayChanged = true
		case "lansenger_app_secret":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerAppSecret = v
			imGatewayChanged = true
		case "lansenger_gateway_url":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerGatewayURL = strings.TrimSpace(v)
			imGatewayChanged = true
		case "lansenger_wss_url":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerWSSURL = strings.TrimSpace(v)
			imGatewayChanged = true
		case "lansenger_group_policy":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			// Normalize to open|allowlist|disabled (empty → open via Effective helper).
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "", "open":
				cfg.LansengerGroupPolicy = "open"
			case "allowlist", "allow", "whitelist":
				cfg.LansengerGroupPolicy = "allowlist"
			case "disabled", "off", "none":
				cfg.LansengerGroupPolicy = "disabled"
			default:
				return corelib.AppConfig{}, fmt.Errorf("lansenger_group_policy must be open, allowlist, or disabled")
			}
		case "lansenger_require_mention":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerRequireMention = &v
		case "lansenger_respond_to_at_all":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerRespondToAtAll = v
		case "lansenger_auto_mention_reply":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerAutoMentionReply = v
		case "lansenger_auto_quote_reply":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerAutoQuoteReply = v
		case "lansenger_group_knowledge_source_ids":
			ids, err := parseLansengerGroupPermissionList(key, value, 500)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerGroupKnowledgeSourceIDs = ids
		case "lansenger_group_allow_web_search":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerGroupAllowWebSearch = v
		case "lansenger_group_allow_all_directories":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerGroupAllowAllDirectories = v
		case "lansenger_group_allowed_directories":
			dirs, err := parseLansengerGroupPermissionList(key, value, 100)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerGroupAllowedDirectories = dirs
		case "lansenger_allowed_group_ids":
			ids, err := parseLansengerGroupIDList(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerAllowedGroupIDs = ids
		case "lansenger_ignored_group_ids":
			ids, err := parseLansengerGroupIDList(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LansengerIgnoredGroupIDs = ids
		case "thirdparty_gateway_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ThirdPartyGatewayEnabled = v
			imGatewayChanged = true
		case "thirdparty_gateway_token":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ThirdPartyGatewayToken = strings.TrimSpace(v)
			imGatewayChanged = true
		case "thirdparty_gateway_host":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ThirdPartyGatewayHost = strings.TrimSpace(v)
			imGatewayChanged = true
		case "thirdparty_gateway_port":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 1 || v > 65535 {
				return corelib.AppConfig{}, fmt.Errorf("thirdparty_gateway_port must be between 1 and 65535")
			}
			cfg.ThirdPartyGatewayPort = v
			imGatewayChanged = true
		case "hardware_welcome_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.HardwareWelcomeEnabled = v
		case "hardware_welcome_text":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if len([]rune(strings.TrimSpace(v))) > 80 {
				return corelib.AppConfig{}, fmt.Errorf("hardware_welcome_text must be at most 80 characters")
			}
			cfg.HardwareWelcomeText = strings.TrimSpace(v)
		case "hardware_welcome_voice_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			voiceID, err := normalizeHardwareWelcomeVoiceID(v)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.HardwareWelcomeVoiceID = voiceID
		case "hardware_welcome_audio_path":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.HardwareWelcomeAudioPath = strings.TrimSpace(v)
		case "hardware_volume":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 || v > 100 {
				return corelib.AppConfig{}, fmt.Errorf("hardware_volume must be between 0 and 100")
			}
			cfg.HardwareVolume = v
		case "hardware_device_aliases":
			v, err := stringMapField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.HardwareDeviceAliases = v
		case "hardware_allow_custom_pets":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.HardwareAllowCustomPets = v
		case "acp_host_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SetAcpHostEnabled(v)
			acpHostChanged = true
		case "acp_host_port":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 || v > 65535 {
				return corelib.AppConfig{}, fmt.Errorf("acp_host_port must be between 0 and 65535 (0 = ephemeral)")
			}
			cfg.AcpHostPort = v
			acpHostChanged = true
		case "acp_host_mirror_ui":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SetAcpHostMirrorUI(v)
			// no restart required
		case "default_proxy_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyEnabled = v
			proxyChanged = true
		case "default_proxy_protocol":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyProtocol = strings.TrimSpace(v)
			proxyChanged = true
		case "default_proxy_host":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyHost = strings.TrimSpace(v)
			proxyChanged = true
		case "default_proxy_port":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyPort = strings.TrimSpace(v)
			proxyChanged = true
		case "default_proxy_username":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyUsername = v
			proxyChanged = true
		case "default_proxy_password":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyPassword = v
			proxyChanged = true
		case "default_proxy_bypass":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyBypass = strings.TrimSpace(v)
			proxyChanged = true
		case "default_proxy_scope_maclaw":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyScopeMaclaw = v
			proxyChanged = true
		case "default_proxy_scope_coding_tools":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyScopeCodingTools = v
			proxyChanged = true
		case "default_proxy_scope_agent":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DefaultProxyScopeAgent = v
			proxyChanged = true
		case "pause_env_check":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PauseEnvCheck = v
		case "env_check_done":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.EnvCheckDone = v
		case "use_windows_terminal":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.UseWindowsTerminal = v
		case "show_ai_trace_entry":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowAITraceEntry = v
		case "show_app_entry":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowAppEntry = &v
		case "show_workflow_entry":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowWorkflowEntry = &v
		case "show_utilities_entry":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowUtilitiesEntry = &v
		case "survey_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SurveyEnabled = &v
		case "show_coding_tool_entry":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowCodingToolEntry = v
		case "show_codex":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowCodex = v
		case "show_opencode":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowOpenCode = v
		case "show_codebuddy":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowCodeBuddy = v
		case "show_iflow":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowIFlow = v
		case "show_kilo":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowKilo = v
		case "workstation_mode":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.WorkstationMode = v
		case "screen_dim_timeout_min":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ScreenDimTimeoutMin = max(0, v)
		case "remote_heartbeat_sec":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.RemoteHeartbeatSec = corelib.NormalizeRemoteHeartbeatIntervalSec(v)
			if hub := a.hubClient(); hub != nil {
				hub.InvalidateHeartbeatIntervalCache()
			}
		case "agent_response_timeout_sec":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.AgentResponseTimeoutSec = v
		case "skill_runner_timeout_sec":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SkillRunnerTimeoutSec = v
		case "skill_evolution_repair_cooldown_hours":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 {
				v = 0
			}
			if v > 24*30 {
				v = 24 * 30
			}
			cfg.SkillEvolutionRepairCooldownHours = v
		case "skill_evolution_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SetSkillEvolutionEnabled(v)
		case "skill_auto_upload_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SetSkillAutoUploadEnabled(v)
		case "skill_auto_upload_min_successes":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 {
				v = 0
			}
			cfg.SkillAutoUploadMinSuccesses = v
		case "maclaw_llm_timeout_sec":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MaclawLLMTimeoutSec = v
		case "maclaw_llm_current_provider":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MaclawLLMCurrentProvider = strings.TrimSpace(v)
		case "audio_input_device_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.AudioInputDeviceID = strings.TrimSpace(v)
		case "audio_output_device_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.AudioOutputDeviceID = strings.TrimSpace(v)
		case "noise_floor_calibrated":
			v, err := floatField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.NoiseFloorCalibrated = max(0, v)
		case "speech_level_calibrated":
			v, err := floatField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SpeechLevelCalibrated = max(0, v)
		case "llm_prompt_cache":
			v, err := cacheField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LLMPromptCache = v
		case "model_routes":
			v, err := modelRoutesField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ModelRoutes = v
		case "coding_route_pref":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.CodingRoutePref = normalizeCodingRoutePref(v)
		case "coding_route_pref_mirror":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.CodingRoutePrefMirror = v
		case "coding_checkpoint_sidecar_max_mb":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 {
				v = 0
			}
			if v > 8192 {
				v = 8192
			}
			cfg.CodingCheckpointSidecarMaxMB = v
		case "gossip_auto_publish":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.GossipAutoPublish = v
		case "show_hub_ranking":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ShowHubRanking = &v
		case "llm_trajectory_logging":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LLMTrajectoryLogging = v
		case "bug_report_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.BugReportEnabled = v
		case "bug_report_previous_trajectory":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.BugReportPreviousTrajectory = v
		case "bug_report_previous_log_detail":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.BugReportPreviousLogDetail = v
		case "log_detail_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LogDetailEnabled = v
		case "memory_recall_log_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MemoryRecallLogEnabled = v
		case "working_directory":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.WorkingDirectory = strings.TrimSpace(v)
		case "ui_zoom_factor":
			v, err := floatField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			// 0 = Auto (DPI-adaptive on the frontend). Manual range [0.5, 2.0].
			if v < 0 {
				v = 0
			}
			if v > 0 && v < 0.5 {
				v = 0.5
			}
			if v > 2.0 {
				v = 2.0
			}
			cfg.UIZoomFactor = v
		case "chat_font_size":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 12 {
				v = 12
			}
			if v > 24 {
				v = 24
			}
			cfg.ChatFontSize = v
		case "env_check_interval":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 2 || v > 30 {
				return corelib.AppConfig{}, fmt.Errorf("env_check_interval must be between 2 and 30 days")
			}
			cfg.EnvCheckInterval = v
		case "last_env_check_time":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.LastEnvCheckTime = strings.TrimSpace(v)
		case "workflow_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SetWorkflowEnabled(v)
		case "vector_search_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.VectorSearchEnabled = v
		case "screen_parsing_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ScreenParsingEnabled = &v
		case "computer_use_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ComputerUseEnabled = &v
		case "computer_use_log_keep_newest":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 {
				v = 0
			}
			if v > 100 {
				v = 100
			}
			cfg.ComputerUseLogKeepNewest = &v
		case "computer_use_log_max_age_days":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			if v < 0 {
				v = 0
			}
			if v > 3650 {
				v = 3650
			}
			cfg.ComputerUseLogMaxAgeDays = &v
		case "computer_use_log_auto_prune":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ComputerUseLogAutoPrune = &v
		case "asr_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ASREnabled = v
		case "diarization_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.DiarizationEnabled = v
		case "asr_voice_correction_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.ASRVoiceCorrectionEnabled = v
		case "tts_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.TTSEnabled = v
		case "tts_voice_id":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.TTSVoiceID = normalizeTTSVoiceID(v)
		case "maclaw_agent_max_iterations":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MaclawAgentMaxIterations = config.EffectiveMaxIterations(v)
		case "maclaw_llm_thinking_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.MaclawLLMThinkingMode = normalizeThinkingModeSetting(v)
		case "subagent_concurrency":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SubAgentConcurrency = corelib.NormalizeSubAgentConcurrency(v)
		case "subagent_full_access":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.SubAgentFullAccess = v
		case "trusted_skill_package_key_fingerprints":
			v, err := stringSliceField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.TrustedSkillPackageKeyFingerprints = v
		case "trial_reflect_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.TrialReflectEnabled = v
		case "pet_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetEnabled = v
			petChanged = true
		case "pet_skin":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetSkin = strings.TrimSpace(v)
			petChanged = true
		case "pet_size":
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetSize = v
			petChanged = true
		case "pet_ambient_city":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetAmbientCity = strings.TrimSpace(v)
		case "pet_motion_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetMotionEnabled = &v
			petChanged = true
		case "pet_motion_sound_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetMotionSound = &v
			petChanged = true
		case "pet_motion_sound_preset":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetMotionSoundPreset = strings.TrimSpace(v)
			petChanged = true
		case "pet_text_interaction_enabled":
			// 暂无消费者，功能落地后再暴露 UI（字段保留）。
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetTextInteraction = &v
			petChanged = true
		case "pet_voice_input_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetVoiceInput = v
			petChanged = true
		case "pet_voice_readback_enabled":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetVoiceReadback = v
			petChanged = true
		case "pet_file_drop_enabled":
			// 暂无消费者，功能落地后再暴露 UI（字段保留）。
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetFileDropEnabled = &v
			petChanged = true
		case "pet_interaction_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetInteractionMode = strings.TrimSpace(v)
			petChanged = true
		case "pet_conversation_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetConversationMode = strings.TrimSpace(v)
			petChanged = true
		case "pet_readback_mode":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetReadbackMode = strings.TrimSpace(v)
			petChanged = true
		case "pet_auto_retry_on_no_hear":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetAutoRetryOnNoHear = v
			petChanged = true
		case "pet_continuous_timeout_sec":
			// 暂无消费者，功能落地后再暴露 UI（字段保留）。
			v, err := intField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetContinuousTimeout = v
			petChanged = true
		case "pet_quiet_mode":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetQuietMode = v
			petChanged = true
		case "pet_variant":
			v, err := stringField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetVariant = strings.TrimSpace(v)
			cfg.PetVariantMigrated = true
			if cfg.PetVariant == "default" || cfg.PetVariant == "figurative" {
				cfg.PetFigurativeUpgradePromptPending = false
			}
			petChanged = true
		case "pet_reduced_motion":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetReducedMotion = v
			petChanged = true
		case "pet_figurative_upgrade_prompt_pending":
			v, err := boolField(key, value)
			if err != nil {
				return corelib.AppConfig{}, err
			}
			cfg.PetFigurativeUpgradePromptPending = v
			petChanged = true
		case "claude", "codex", "opencode", "codebuddy", "iflow", "kilo":
			data, err := json.Marshal(value)
			if err != nil {
				return corelib.AppConfig{}, fmt.Errorf("config field %q must be object: %w", key, err)
			}
			var tc corelib.ToolConfig
			if err := json.Unmarshal(data, &tc); err != nil {
				return corelib.AppConfig{}, fmt.Errorf("config field %q must be ToolConfig object: %w", key, err)
			}
			// Sanitize custom model names
			for i := range tc.Models {
				if tc.Models[i].IsCustom && strings.TrimSpace(tc.Models[i].ModelName) == "" {
					tc.Models[i].ModelName = "Custom"
				}
			}
			switch key {
			case "claude":
				cfg.Claude = tc
			case "codex":
				cfg.Codex = tc
			case "opencode":
				cfg.Opencode = tc
			case "codebuddy":
				cfg.CodeBuddy = tc
			case "iflow":
				cfg.IFlow = tc
			case "kilo":
				cfg.Kilo = tc
			}
		default:
			return corelib.AppConfig{}, fmt.Errorf("unsupported config patch field: %s", key)
		}
	}
	if err := validateHardwareGatewayInvariant(cfg); err != nil {
		return corelib.AppConfig{}, err
	}

	if a.shouldPreserveHubManagedSecurity(current) {
		clientsecurity.PreserveHubManagedSecurityConfig(current, &cfg)
	} else if a.hubSecurityExplicitlyCentralizedFalse() {
		cfg.HubSecurityCentralized = false
	}
	sanitizeCodingToolSelection(&cfg)
	normalizeConfigTimeouts(&cfg)
	if petChanged {
		sanitizePetConfig(&cfg)
	}
	if _, ok := patch["llm_prompt_cache"]; ok {
		if err := migrateLLMPromptCacheDirIfNeeded(current.LLMPromptCache, cfg.LLMPromptCache); err != nil {
			a.invalidateConfigCacheLocked()
			return corelib.AppConfig{}, err
		}
	}

	path, err := a.getConfigPath()
	if err != nil {
		return corelib.AppConfig{}, err
	}
	a.workflowDisabled.Store(!cfg.IsWorkflowEnabled())
	workflowTurnedOff := !cfg.IsWorkflowEnabled() && current.IsWorkflowEnabled()
	floatingChanged := petChanged && floatingAppearanceChanged(current, cfg)
	motionChanged := petChanged && floatingMotionChanged(current, cfg)
	soundChanged := petChanged && floatingSoundChanged(current, cfg)
	devicePetChanged := devicePetProfileChanged(current, cfg)
	hardwareCustomPetsDisabled := current.HardwareAllowCustomPets && !cfg.HardwareAllowCustomPets
	hardwareWelcomeChanged := hardwareWelcomeConfigChanged(current, cfg)
	hardwareVolumeChanged := hardwareVolumeChanged(current, cfg)
	// Unified epilogue: publish → unlock → post-unlock drains → persist.
	// Mark committed before finish so defer does not double-unlock.
	configTxnCommitted = true
	if err := a.unlockConfigAndFinish(cfg, path, true); err != nil {
		a.configMu.Lock()
		a.invalidateConfigCacheLocked()
		a.configMu.Unlock()
		return corelib.AppConfig{}, err
	}
	if fullControl, ok := patch["subagent_full_access"].(bool); ok {
		recordSubAgentPermissionModeAudit(a, fullControl)
	}

	// When workflow is toggled off, dismiss the frontend workflow panel immediately.
	// Without this, the progress board stays visible until the next user message.
	if workflowTurnedOff {
		emitWorkflowV2Event(a, "workflow:phase_update", nil)
		emitWorkflowV2Event(a, "workflow:suggest_maximize_dismiss", nil)
	}

	// When LLM provider is switched, emit the same event that SaveMaclawLLMProviders
	// uses so the sidebar token usage display refreshes automatically.
	if _, ok := patch["maclaw_llm_current_provider"]; ok {
		a.clearMoAStickyForAllUsers()
		if a.ctx != nil {
			a.emitEvent("llm-token-usage-changed", cfg.MaclawLLMCurrentProvider)
			a.emitEvent("moa-session-changed", nil)
		}
	}
	// Hot-reload ModelRouter when pure-coding / task routes change.
	if _, ok := patch["model_routes"]; ok {
		a.reloadModelRouterFromConfig(cfg)
		if a.ctx != nil {
			a.emitEvent("model-routes-changed", nil)
		}
	}
	if _, ok := patch["coding_checkpoint_sidecar_max_mb"]; ok {
		setCodingCheckpointSidecarMaxMB(cfg.CodingCheckpointSidecarMaxMB)
	}

	// Invalidate tool outcome records when SSH host or LLM provider config changes.
	// This ensures OutcomeScore reflects current tool reliability rather than stale
	// historical performance under different conditions (Req 1.1, 1.3, 1.4).
	if tracker := a.usageTracker; tracker != nil {
		// SSH host field changes → invalidate "ssh" tool outcomes.
		for key := range patch {
			if isSSHConfigField(key) {
				tracker.InvalidateOutcomes("ssh", fmt.Sprintf("config_change:%s", key))
				break
			}
		}
		// LLM provider change → invalidate LLM-dependent tool outcomes.
		if _, changed := patch["maclaw_llm_current_provider"]; changed {
			for _, toolName := range []string{"craft_tool", "delegate_task", "ask_user"} {
				tracker.InvalidateOutcomes(toolName, "llm_provider_changed")
			}
		}
	}

	corelib.SetLogDetailEnabled(cfg.LogDetailEnabled)
	memory.SetMemoryRecallLogEnabled(cfg.MemoryRecallLogEnabled)
	// Data dir already applied under configMu before commit; only workspace path remains.
	corelib.SetWorkspaceDir(cfg.WorkingDirectory)
	a.refreshPowerOptimizationStateFromConfig(cfg)
	a.refreshWorkstationMode(cfg)
	if policyModeChanged && a.policyEngine != nil {
		a.policyEngine.SetMode(cfg.SecurityPolicyMode)
	}
	// Notify all frontend listeners of the config change unconditionally.
	// OnConfigChanged (tray callback) also emits this event on platforms with
	// tray support, but may be nil. Emitting directly ensures all UI components
	// stay in sync (e.g. workflow toggle in the AI assistant title bar).
	if a.ctx != nil {
		a.emitEvent("config-changed", cfg)
	}
	if OnConfigChanged != nil {
		go OnConfigChanged(cfg)
	}
	if imGatewayChanged && a.ctx != nil {
		go a.syncIMGatewaysFromConfig()
	}
	if acpHostChanged && a.ctx != nil {
		go a.syncACPHostFromConfig()
	}
	if floatingChanged {
		go func(cfg corelib.AppConfig) {
			if cfg.PetEnabled {
				if fa := a.ensureFloatingAssistant(); fa != nil {
					fa.RefreshAppearance(cfg)
				}
				return
			}
			if fa := a.existingFloatingAssistant(); fa != nil {
				fa.RefreshAppearance(cfg)
			}
		}(cfg)
	}
	if !floatingChanged && (soundChanged || motionChanged) {
		go func(cfg corelib.AppConfig) {
			if fa := a.existingFloatingAssistant(); fa != nil {
				if soundChanged {
					fa.UpdateSoundConfig(cfg)
				}
				if motionChanged {
					fa.UpdateMotionConfig(cfg)
				}
			}
		}(cfg)
	}
	if (devicePetChanged && !cfg.HardwareAllowCustomPets || hardwareCustomPetsDisabled) {
		// Propagate the active GUI pet immediately. Previously the Hub profile was
		// refreshed only when the GUI happened to send a gateway reply, so changing
		// packs while the ESP was idle had no observable effect. This is a
		// transport-side state transition, not a frontend event: app context can
		// be nil during startup/reconnect while the Hub socket is already live.
		// Gating it on a.ctx therefore silently dropped real pet selections.
		go func(cfg corelib.AppConfig) {
			if hub := a.hubClient(); hub != nil && hub.IsConnected() {
				if err := hub.SendDeviceGatewayPetProfile(cfg); err != nil {
					log.Printf("[device-pet] profile sync failed: %v", err)
				}
			}
			// Local Gateway mode has no Hub relay; broadcast to paired devices
			// directly so they follow the same settings change in real time.
			if a.thirdPartyGateway != nil {
				a.thirdPartyGateway.broadcastPetProfile(a.devicePetProfileForConfig(cfg))
			}
		}(cfg)
	}
	if hardwareWelcomeChanged {
		// PatchConfigFields is also used by non-settings callers. Sync here so
		// changing the switch or replacing the WAV cannot depend on the settings
		// panel remaining mounted.
		go func() {
			if err := a.SyncHardwareWelcome(); err != nil {
				log.Printf("[device-welcome] configuration sync failed: %v", err)
			}
		}()
	}
	if hardwareVolumeChanged {
		go func() {
			if err := a.SyncHardwareVolume(); err != nil {
				log.Printf("[device-volume] configuration sync failed: %v", err)
			}
		}()
	}
	if proxyChanged {
		a.applyAgentProxy()
	}
	// Live-update evolution pipeline cooldown when patched.
	if _, ok := patch["skill_evolution_repair_cooldown_hours"]; ok && a.evolutionPipeline != nil {
		a.evolutionPipeline.RepairCooldown = skill.RepairCooldownFromHours(cfg.SkillEvolutionRepairCooldownHours)
	}
	log.Printf("[config] PatchConfigFields:done fields=%d keys=%v", len(patch), configPatchKeys(patch))
	return cfg, nil
}

// configPatchKeys extracts sorted keys from a patch map for diagnostic logging.
func configPatchKeys(patch map[string]interface{}) []string {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	return keys
}

// isSSHConfigField returns true if the patch key relates to SSH host configuration.
// Matches fields containing ssh_host, ssh_port, ssh_user, ssh_key, or the
// aggregate ssh_hosts array field.
func isSSHConfigField(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "ssh_host") ||
		strings.Contains(k, "ssh_port") ||
		strings.Contains(k, "ssh_user") ||
		strings.Contains(k, "ssh_key")
}

// invalidateOutcomesFromExternalConfigChange triggers tool outcome invalidation
// when an external config edit (detected by file watcher) may have changed
// SSH host or LLM provider fields. This is a best-effort comparison against
// the previous config cache — if the cache is stale, the fingerprint mechanism
// catches the change on the next RecordExperience call.
func (a *App) invalidateOutcomesFromExternalConfigChange(newCfg corelib.AppConfig) {
	tracker := a.usageTracker
	if tracker == nil {
		return
	}
	// Compare LLM provider against the fingerprint's known state to avoid
	// redundant invalidation. We fire invalidation unconditionally here because
	// the file watcher cannot determine which fields changed (it only knows
	// the file was modified). The fingerprint mechanism will deduplicate if
	// the provider hasn't actually changed.
	// This is intentionally simple: invalidate both SSH and LLM tools on any
	// external config file edit. Cost is minimal (just updates timestamps),
	// and the decay formula's grace period (multiplier=1.0 at t=0) ensures
	// no immediate impact if the invalidation was spurious.
	if len(newCfg.SSHHosts) > 0 {
		tracker.InvalidateOutcomes("ssh", "external_config_edit")
	}
	if newCfg.MaclawLLMCurrentProvider != "" {
		for _, toolName := range []string{"craft_tool", "delegate_task", "ask_user"} {
			tracker.InvalidateOutcomes(toolName, "external_config_edit")
		}
	}
}

// PatchConfig performs an atomic read-modify-write on the config file.
// The patchFn receives the current config and may modify any fields.
// Mutation and cache publish run under configMu; disk I/O is serialized by
// configDiskMu outside configMu so concurrent LoadConfig readers are not
// blocked on AtomicWrite. This eliminates the TOCTOU race of LoadConfig-
// modify-SaveConfig while keeping the UI path responsive.
//
// Use PatchConfig when updating a small number of fields (credentials, flags)
// while other goroutines may be concurrently modifying the config. Use
// SaveConfig when you hold a complete, authoritative config snapshot (e.g.
// from the frontend settings panel) that needs the full sanitization and
// API-key sync pipeline.
//
// Note: PatchConfig intentionally skips sanitizeCustomNames, syncAllProvider-
// ApiKeys, and post-save side effects (refreshWorkstationMode, etc.) because
// those are only relevant when the corresponding fields change. Callers that
// modify model/API-key/workspace fields should use SaveConfig instead.
func (a *App) SetDefaultLaunchMode(mode string) error {
	launchMode := normalizeLaunchModeKind(mode)
	if launchMode == launchModeInvalid {
		return fmt.Errorf("invalid default launch mode: %s", mode)
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.DefaultLaunchMode = launchMode.String()
		if launchMode.IsRemote() {
			cfg.RemoteEnabled = true
		}
	})
}

func (a *App) SetAuthRequestSoundConfig(preset string, muted bool) error {
	normalizedPreset := strings.ToLower(strings.TrimSpace(preset))
	switch normalizedPreset {
	case "classic", "soft", "bright", "pulse", "urgent":
	default:
		normalizedPreset = "classic"
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.GroupDiscussion.AuthRequestSoundPreset = normalizedPreset
		cfg.GroupDiscussion.AuthRequestSoundMuted = muted
	})
}

func (a *App) PatchConfig(patchFn func(cfg *corelib.AppConfig)) error {
	return a.patchConfig(patchFn, false)
}

func (a *App) PatchConfigIfChanged(patchFn func(cfg *corelib.AppConfig) bool) (bool, error) {
	return a.patchConfigIfChanged(patchFn, false)
}

func configPatchCaller(skip int) string {
	for i := skip; i < skip+8; i++ {
		pc, file, line, ok := goruntime.Caller(i)
		if !ok {
			continue
		}
		fnName := ""
		if fn := goruntime.FuncForPC(pc); fn != nil {
			fnName = fn.Name()
			if idx := strings.LastIndex(fnName, "/"); idx >= 0 {
				fnName = fnName[idx+1:]
			}
		}
		base := filepath.Base(file)
		if base == "app.go" && (strings.Contains(fnName, ".patchConfig") || strings.Contains(fnName, ".PatchConfig")) {
			continue
		}
		if fnName == "" {
			return fmt.Sprintf("%s:%d", base, line)
		}
		return fmt.Sprintf("%s:%d %s", base, line, fnName)
	}
	return "unknown"
}

func (a *App) patchConfigIfChanged(patchFn func(cfg *corelib.AppConfig) bool, allowHubManagedSecurity bool) (bool, error) {
	// Unified write protocol — see config_txn.go.
	return a.mutateConfigMaybe(patchFn, configMutateOpts{
		allowHubManagedSecurity: allowHubManagedSecurity,
		caller:                  configPatchCaller(2),
	})
}

func (a *App) patchConfig(patchFn func(cfg *corelib.AppConfig), allowHubManagedSecurity bool) error {
	// Unified write protocol — see config_txn.go.
	return a.mutateConfig(patchFn, configMutateOpts{
		allowHubManagedSecurity: allowHubManagedSecurity,
		caller:                  configPatchCaller(2),
	})
}

// saveToPath atomically writes config.json. All config disk writers must use
// this helper so configDiskMu serializes AtomicWrite (including rare paths
// inside loadConfigLocked such as first-run defaults / pet migration).
// Do not call while holding configMu if the write can be deferred — holding
// both locks is only safe when configMu is already held for a cold load.
func (a *App) saveToPath(path string, config corelib.AppConfig) error {
	a.configDiskMu.Lock()
	defer a.configDiskMu.Unlock()
	err := configfile.AtomicWriteJSON(path, config)
	if err == nil {
		a.configLastInternalWrite.Store(time.Now().UnixMilli())
	}
	return err
}

type UpdateResult struct {
	HasUpdate           bool   `json:"has_update"`
	LatestVersion       string `json:"latest_version"`
	ReleaseUrl          string `json:"release_url"`
	TagName             string `json:"tag_name"`
	DownloadUrl         string `json:"download_url"`
	DownloadUnavailable bool   `json:"download_unavailable"` // true when new version exists but installer package is not yet available
	SHA256              string `json:"sha256,omitempty"`     // expected sha256 hash of the installer (from manifest)
	Channel             string `json:"channel,omitempty"`    // "stable" or "beta" — which channel this result came from
}

const (
	githubLatestManifestURL = "https://github.com/RapidAI/MaClaw/releases/latest/download/latest.json"
	r2LatestManifestURL     = "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest.json"
	r2PublicBaseURL         = "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev"
	cosLatestManifestURL    = "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest.json"
	cosPublicBaseURL        = "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com"

	// Beta channel manifest URLs
	r2BetaManifestURL  = "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/beta.json"
	cosBetaManifestURL = "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/beta.json"
)

type updateManifest struct {
	Version string                         `json:"version"`
	Tag     string                         `json:"tag"`
	Assets  map[string]updateManifestAsset `json:"assets"`
}

type updateManifestAsset struct {
	Name   string   `json:"name"`
	Size   int64    `json:"size"`
	URL    string   `json:"url"`
	URLs   []string `json:"urls"`
	SHA256 string   `json:"sha256,omitempty"`
}

func updateHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 6 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   6 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
}

func updateTargetFileName() string {
	return updateTargetFileNameFor(brand.Current().DisplayName, goruntime.GOOS)
}

func updateTargetFileNameFor(brandName, goos string) string {
	if strings.TrimSpace(brandName) == "" {
		brandName = "MaClaw"
	}
	if goos == "darwin" {
		return brandName + "-Universal.pkg"
	}
	return brandName + "-Setup.exe"
}

func r2ReleaseAssetURL(fileName string, isBeta bool) string {
	prefix := "latest"
	if isBeta {
		prefix = "beta"
	}
	return fmt.Sprintf("%s/%s/%s", r2PublicBaseURL, prefix, fileName)
}

func cosReleaseAssetURL(fileName string, isBeta bool) string {
	prefix := "latest"
	if isBeta {
		prefix = "beta"
	}
	return fmt.Sprintf("%s/%s/%s", cosPublicBaseURL, prefix, fileName)
}

func combineDownloadURLList(urls ...string) string {
	seen := make(map[string]bool, len(urls))
	cleaned := make([]string, 0, len(urls))
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		cleaned = append(cleaned, url)
	}
	return strings.Join(cleaned, "\n")
}

func combineDownloadURLs(primary, fallback string) string {
	return combineDownloadURLList(primary, fallback)
}

func manifestAssetDownloadURLs(manifest updateManifest, targetFileName, tagName string, isBeta bool) []string {
	urls := []string{}
	if asset, ok := manifest.Assets[targetFileName]; ok {
		urls = append(urls, asset.URLs...)
		urls = append(urls, asset.URL)
	}
	if tagName != "" {
		urls = append(urls, r2ReleaseAssetURL(targetFileName, isBeta))
		urls = append(urls, cosReleaseAssetURL(targetFileName, isBeta))
	}
	combined := combineDownloadURLList(urls...)
	if combined == "" {
		return nil
	}
	return strings.Split(combined, "\n")
}

func (a *App) CheckUpdate(currentVersion string) (UpdateResult, error) {
	release, source, err := a.fetchLatestReleaseFast()
	if err != nil {
		a.log(a.tr("CheckUpdate: all update sources failed: %v", err))
		return UpdateResult{LatestVersion: "fetch_failed", ReleaseUrl: ""}, err
	}
	a.log(a.tr("CheckUpdate: using update source: %s", source))
	return a.buildUpdateResult(currentVersion, release, false)
}

// buildUpdateResult is the shared version comparison and URL construction logic
// used by both CheckUpdate (stable) and CheckUpdateBeta (beta).
func (a *App) buildUpdateResult(currentVersion string, release latestReleaseInfo, isBeta bool) (UpdateResult, error) {
	targetFileName := updateTargetFileName()

	tagName := strings.TrimSpace(release.TagName)
	if tagName == "" {
		tagName = strings.TrimSpace(release.Name)
	}
	if tagName == "" {
		return UpdateResult{LatestVersion: "unknown", ReleaseUrl: ""}, fmt.Errorf("no version found in release")
	}

	githubDownloadUrl := strings.TrimSpace(release.GitHubDownloadURL)
	cosDownloadUrl := strings.TrimSpace(release.COSDownloadURL)
	if githubDownloadUrl == "" {
		githubDownloadUrl = fmt.Sprintf("https://github.com/RapidAI/MaClaw/releases/download/%s/%s", tagName, targetFileName)
	}
	if cosDownloadUrl == "" {
		betaTag := isBeta || strings.Contains(tagName, "-beta") || strings.Contains(tagName, "-alpha") || strings.Contains(tagName, "-rc")
		cosDownloadUrl = cosReleaseAssetURL(targetFileName, betaTag)
	}
	downloadUrl := strings.TrimSpace(release.DownloadURL)
	if downloadUrl == "" {
		downloadUrl = combineDownloadURLs(githubDownloadUrl, cosDownloadUrl)
	}

	displayVersion := strings.TrimSpace(tagName)
	if !strings.HasPrefix(strings.ToUpper(displayVersion), "V") {
		displayVersion = "V" + displayVersion
	}
	latestVersionForComparison := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(tagName)), "v")
	latestVersionForComparison = strings.Split(latestVersionForComparison, " ")[0]
	cleanCurrent := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(currentVersion)), "v")
	cleanCurrent = strings.Split(cleanCurrent, " ")[0]

	channel := "stable"
	if isBeta {
		channel = "beta"
	}
	a.log(a.tr("CheckUpdate[%s]: Latest=%s, Current=%s, Display=%s", channel, latestVersionForComparison, cleanCurrent, displayVersion))
	if compareVersions(latestVersionForComparison, cleanCurrent) > 0 {
		a.log(a.tr("CheckUpdate[%s]: Update available! %s > %s", channel, latestVersionForComparison, cleanCurrent))
		return UpdateResult{HasUpdate: true, LatestVersion: displayVersion, ReleaseUrl: release.ReleaseURL, TagName: tagName, DownloadUrl: downloadUrl, DownloadUnavailable: downloadUrl == "", SHA256: release.SHA256, Channel: channel}, nil
	}
	a.log(a.tr("CheckUpdate[%s]: Already on latest version", channel))
	return UpdateResult{HasUpdate: false, LatestVersion: displayVersion, ReleaseUrl: release.ReleaseURL, TagName: tagName, DownloadUrl: downloadUrl, SHA256: release.SHA256, Channel: channel}, nil
}

// CheckUpdateBeta checks for updates when the user opts into 尝鲜测试版.
// It fetches both the beta and stable channels (in parallel) and returns the
// newer one, so a newer formal release is never hidden behind an older beta build.
func (a *App) CheckUpdateBeta(currentVersion string) (UpdateResult, error) {
	type channelOutcome struct {
		result UpdateResult
		err    error
	}
	var beta, stable channelOutcome
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				beta.err = fmt.Errorf("beta check panic: %v", r)
			}
		}()
		release, source, err := a.fetchBetaReleaseFast()
		if err != nil {
			a.log(a.tr("CheckUpdateBeta: all beta sources failed: %v", err))
			beta.err = err
			return
		}
		a.log(a.tr("CheckUpdateBeta: using beta source: %s", source))
		beta.result, beta.err = a.buildUpdateResult(currentVersion, release, true)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				stable.err = fmt.Errorf("stable check panic: %v", r)
			}
		}()
		release, source, err := a.fetchLatestReleaseFast()
		if err != nil {
			a.log(a.tr("CheckUpdateBeta: stable check failed: %v", err))
			stable.err = err
			return
		}
		a.log(a.tr("CheckUpdateBeta: also checked stable source: %s", source))
		stable.result, stable.err = a.buildUpdateResult(currentVersion, release, false)
	}()
	wg.Wait()

	result, err := pickPreferredUpdateResult(beta.result, beta.err, stable.result, stable.err)
	if err != nil {
		a.log(a.tr("CheckUpdateBeta: all update sources failed: %v", err))
		return result, err
	}
	a.log(a.tr("CheckUpdateBeta: preferred channel=%s latest=%s has_update=%v", result.Channel, result.LatestVersion, result.HasUpdate))
	return result, nil
}

// updateResultVersionKey returns the best string for semver comparison.
// Prefer TagName (raw manifest tag) over LatestVersion (display, may have a V prefix).
func updateResultVersionKey(r UpdateResult) string {
	if tag := strings.TrimSpace(r.TagName); tag != "" {
		return tag
	}
	return strings.TrimSpace(r.LatestVersion)
}

// pickPreferredUpdateResult chooses the better of beta and stable update results.
// The user opted into beta, so equal versions prefer beta; a strictly newer
// stable release always wins so formal updates are not masked by the beta channel.
func pickPreferredUpdateResult(beta UpdateResult, betaErr error, stable UpdateResult, stableErr error) (UpdateResult, error) {
	if betaErr != nil && stableErr != nil {
		return UpdateResult{LatestVersion: "fetch_failed", ReleaseUrl: ""}, fmt.Errorf("beta: %v; stable: %v", betaErr, stableErr)
	}
	if betaErr != nil {
		return stable, nil
	}
	if stableErr != nil {
		return beta, nil
	}
	// Prefer whichever release is strictly newer; on tie keep beta (user opted in).
	if compareVersions(updateResultVersionKey(stable), updateResultVersionKey(beta)) > 0 {
		return stable, nil
	}
	return beta, nil
}

func (a *App) fetchBetaReleaseFast() (latestReleaseInfo, string, error) {
	var errors []string
	if release, err := a.fetchManifestLatestRelease("r2-beta", r2BetaManifestURL, 5*time.Second); err == nil {
		return release, "r2-beta", nil
	} else {
		errors = append(errors, fmt.Sprintf("r2-beta: %v", err))
		a.log(a.tr("CheckUpdateBeta: R2 beta check failed, trying COS: %v", err))
	}
	if release, err := a.fetchManifestLatestRelease("cos-beta", cosBetaManifestURL, 5*time.Second); err == nil {
		return release, "cos-beta", nil
	} else {
		errors = append(errors, fmt.Sprintf("cos-beta: %v", err))
		return latestReleaseInfo{}, "", fmt.Errorf("all beta manifest checks failed: %s", strings.Join(errors, "; "))
	}
}

type latestReleaseInfo struct {
	TagName           string
	Name              string
	ReleaseURL        string
	DownloadURL       string
	GitHubDownloadURL string
	COSDownloadURL    string
	SHA256            string
}

func (a *App) fetchLatestReleaseFast() (latestReleaseInfo, string, error) {
	var errors []string
	if release, err := a.fetchGitHubLatestRelease(4 * time.Second); err == nil {
		return release, "github", nil
	} else {
		errors = append(errors, fmt.Sprintf("github: %v", err))
		a.log(a.tr("CheckUpdate: GitHub latest check failed quickly, trying R2: %v", err))
	}
	if release, err := a.fetchR2LatestRelease(5 * time.Second); err == nil {
		return release, "r2", nil
	} else {
		errors = append(errors, fmt.Sprintf("r2: %v", err))
		a.log(a.tr("CheckUpdate: R2 latest check failed quickly, trying COS: %v", err))
	}
	if release, err := a.fetchCOSLatestRelease(5 * time.Second); err == nil {
		return release, "cos", nil
	} else {
		errors = append(errors, fmt.Sprintf("cos: %v", err))
		return latestReleaseInfo{}, "", fmt.Errorf("all latest manifest checks failed: %s", strings.Join(errors, "; "))
	}
}

func (a *App) fetchGitHubLatestRelease(timeout time.Duration) (latestReleaseInfo, error) {
	a.log(a.tr("CheckUpdate: Starting GitHub manifest check against %s", githubLatestManifestURL))
	return a.fetchManifestLatestRelease("github", githubLatestManifestURL, timeout)
}

func (a *App) fetchManifestLatestRelease(source, manifestURL string, timeout time.Duration) (latestReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return latestReleaseInfo{}, err
	}
	req.Header.Set("User-Agent", brand.Current().DisplayName)
	resp, err := updateHTTPClient(timeout).Do(req)
	if err != nil {
		return latestReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyText, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return latestReleaseInfo{}, fmt.Errorf("%s latest manifest returned status %d: %s", source, resp.StatusCode, string(bodyText))
	}
	var manifest updateManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return latestReleaseInfo{}, err
	}
	tagName := strings.TrimSpace(manifest.Tag)
	if tagName == "" {
		tagName = strings.TrimSpace(manifest.Version)
	}
	targetFileName := updateTargetFileName()
	isBeta := strings.Contains(tagName, "-beta") || strings.Contains(tagName, "-alpha") || strings.Contains(tagName, "-rc")
	mirrorURLs := manifestAssetDownloadURLs(manifest, targetFileName, tagName, isBeta)
	githubURL := ""
	if tagName != "" {
		githubURL = fmt.Sprintf("https://github.com/RapidAI/MaClaw/releases/download/%s/%s", tagName, targetFileName)
	}
	cosURL := ""
	if len(mirrorURLs) > 0 {
		cosURL = mirrorURLs[len(mirrorURLs)-1]
	}
	sha256 := ""
	if asset, ok := manifest.Assets[targetFileName]; ok {
		sha256 = strings.TrimSpace(asset.SHA256)
	}
	return latestReleaseInfo{TagName: tagName, Name: tagName, ReleaseURL: "https://github.com/RapidAI/MaClaw/releases/latest", DownloadURL: combineDownloadURLList(append([]string{githubURL}, mirrorURLs...)...), GitHubDownloadURL: githubURL, COSDownloadURL: cosURL, SHA256: sha256}, nil
}

func (a *App) fetchR2LatestRelease(timeout time.Duration) (latestReleaseInfo, error) {
	a.log(a.tr("CheckUpdate: Starting R2 check against %s", r2LatestManifestURL))
	return a.fetchManifestLatestRelease("r2", r2LatestManifestURL, timeout)
}

func (a *App) fetchCOSLatestRelease(timeout time.Duration) (latestReleaseInfo, error) {
	a.log(a.tr("CheckUpdate: Starting COS check against %s", cosLatestManifestURL))
	return a.fetchManifestLatestRelease("cos", cosLatestManifestURL, timeout)
}

// Helper function to get map keys
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

type DownloadProgress struct {
	Percentage float64                `json:"percentage"`
	Downloaded int64                  `json:"downloaded"`
	Total      int64                  `json:"total"`
	Status     downloadProgressStatus `json:"status"`
	Source     string                 `json:"source,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

func (a *App) DownloadUpdate(url string, fileName string) (string, error) {
	return a.DownloadUpdateWithSHA256(url, fileName, "")
}

func (a *App) DownloadUpdateWithSHA256(url string, fileName string, expectedSHA256 string) (string, error) {
	urls := splitDownloadURLs(url)
	if len(urls) == 0 {
		return "", fmt.Errorf("download url is empty")
	}
	a.log(fmt.Sprintf("DownloadUpdate: Starting download candidates: %v", urls))
	downloadsDir, err := a.GetDownloadsFolder()
	if err != nil {
		return "", fmt.Errorf("failed to get downloads folder: %w", err)
	}
	destPath := filepath.Join(downloadsDir, fileName)
	ctx, cancel := context.WithCancel(context.Background())
	downloadID := fileName
	a.downloadMutex.Lock()
	a.downloadCancelers[downloadID] = cancel
	a.downloadMutex.Unlock()
	defer func() {
		a.downloadMutex.Lock()
		delete(a.downloadCancelers, downloadID)
		a.downloadMutex.Unlock()
		cancel()
	}()

	var lastErr error
	for index, candidateURL := range urls {
		if _, err := os.Stat(destPath); err == nil {
			_ = os.Remove(destPath)
		}
		a.log(fmt.Sprintf("DownloadUpdate: Trying source %d/%d: %s", index+1, len(urls), candidateURL))
		a.emitEvent("download-progress", DownloadProgress{Status: downloadProgressStatusDownloading, Source: downloadSourceHost(candidateURL)})
		path, err := a.downloadUpdateFromURL(ctx, candidateURL, fileName, destPath)
		if err == nil {
			// SHA256 verification (if expected hash is provided from manifest)
			if expectedSHA256 != "" {
				a.emitEvent("download-progress", DownloadProgress{Percentage: 100, Status: downloadProgressStatusVerifying})
				if verifyErr := verifySHA256File(path, expectedSHA256); verifyErr != nil {
					a.log(fmt.Sprintf("DownloadUpdate: SHA256 verification failed for %s: %v", candidateURL, verifyErr))
					_ = os.Remove(path)
					lastErr = verifyErr
					continue
				}
				a.log("DownloadUpdate: SHA256 verification passed")
			}
			a.emitEvent("download-progress", DownloadProgress{Percentage: 100, Status: downloadProgressStatusCompleted})
			return path, nil
		}
		lastErr = err
		a.log(fmt.Sprintf("DownloadUpdate: Source failed, will try fallback if available: %v", err))
		if ctx.Err() == context.Canceled {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all download sources failed")
	}
	a.emitEvent("download-progress", DownloadProgress{Status: downloadProgressStatusError, Error: lastErr.Error()})
	return "", lastErr
}

// downloadSourceHost deliberately exposes only the host. Download URLs can contain
// short-lived signed query parameters and should never be forwarded to the UI.
func downloadSourceHost(downloadURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(downloadURL))
	if err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}
	return ""
}

func splitDownloadURLs(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '|'
	})
	urls := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		url := strings.TrimSpace(field)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		urls = append(urls, url)
	}
	return urls
}

// verifySHA256File verifies the SHA256 hash of a file against an expected hex digest.
// Returns nil if expected is empty (no verification requested) or if the hash matches.
func verifySHA256File(path, expected string) error {
	expected = strings.TrimSpace(strings.ToLower(expected))
	if expected == "" {
		return nil
	}
	if len(expected) != 64 {
		return fmt.Errorf("invalid sha256 digest length %d: %q", len(expected), expected)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("file integrity verification failed (expected %s..., got %s...)", expected[:8], actual[:8])
	}
	return nil
}

func (a *App) downloadUpdateFromURL(ctx context.Context, url string, fileName string, destPath string) (string, error) {
	sourceTimeout := 30 * time.Minute
	if strings.Contains(url, "github.com/") || strings.Contains(url, "githubusercontent.com/") {
		sourceTimeout = 90 * time.Second
	}
	sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(sourceCtx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MaClaw-App")
	client := updateHTTPClient(12 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("installer not found (HTTP 404)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	contentType := resp.Header.Get("Content-Type")
	a.log(fmt.Sprintf("DownloadUpdate: Content-Type: %s", contentType))
	if !strings.Contains(strings.ToLower(contentType), "application/octet-stream") &&
		!strings.Contains(strings.ToLower(contentType), "application/x-msdownload") &&
		!strings.Contains(strings.ToLower(contentType), "application/x-dosexec") &&
		!strings.Contains(strings.ToLower(contentType), "application/vnd.apple.installer+xml") {
		a.log(fmt.Sprintf("Warning: Unexpected Content-Type: %s", contentType))
	}
	if resp.ContentLength >= 0 && resp.ContentLength < 5*1024*1024 {
		return "", fmt.Errorf("file too small (%d bytes), possibly an error page", resp.ContentLength)
	}
	expectedExt := ".exe"
	if goruntime.GOOS == "darwin" {
		expectedExt = ".pkg"
	}
	if !strings.HasSuffix(strings.ToLower(fileName), expectedExt) {
		return "", fmt.Errorf("invalid file extension: %s (expected %s)", fileName, expectedExt)
	}
	size := resp.ContentLength
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	var downloaded int64
	buffer := make([]byte, 256*1024)
	lastReport := time.Now()
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := out.Write(buffer[:n]); writeErr != nil {
				return "", writeErr
			}
			downloaded += int64(n)
			if time.Since(lastReport) > 100*time.Millisecond {
				percentage := 0.0
				if size > 0 {
					percentage = float64(downloaded) / float64(size) * 100
				}
				a.emitEvent("download-progress", DownloadProgress{Percentage: percentage, Downloaded: downloaded, Total: size, Status: downloadProgressStatusDownloading})
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() == context.Canceled {
				a.emitEvent("download-progress", DownloadProgress{Status: downloadProgressStatusCancelled})
				return "", fmt.Errorf("download cancelled")
			}
			if sourceCtx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("download source timed out: %s", url)
			}
			return "", err
		}
	}
	a.emitEvent("download-progress", DownloadProgress{Percentage: 100, Downloaded: downloaded, Total: size, Status: downloadProgressStatusDownloading})
	return destPath, nil
}
func (a *App) CancelDownload(downloadID string) {
	a.downloadMutex.Lock()
	defer a.downloadMutex.Unlock()
	if cancel, ok := a.downloadCancelers[downloadID]; ok {
		cancel()
		delete(a.downloadCancelers, downloadID)
	}
}
func (a *App) RecoverCC() error {
	a.emitRecoverLog("Starting recovery process...")
	home := a.GetUserHomeDir()

	// Remove ~/.claude.json file
	claudeJsonPath := filepath.Join(home, ".claude.json")
	a.emitRecoverLog(fmt.Sprintf("Checking file: %s", claudeJsonPath))
	if _, err := os.Stat(claudeJsonPath); !os.IsNotExist(err) {
		a.emitRecoverLog("Found .claude.json file. Removing...")
		if err := os.Remove(claudeJsonPath); err != nil && !os.IsNotExist(err) {
			a.emitRecoverLog(fmt.Sprintf("Failed to remove .claude.json file: %v", err))
			return fmt.Errorf("failed to remove .claude.json file: %w", err)
		}
		a.emitRecoverLog("Successfully removed .claude.json file.")
	} else {
		a.emitRecoverLog(".claude.json file not found, skipping.")
	}

	// Remove ~/.claude/settings.json file
	settingsPath := configfile.ClaudeSettingsPath()
	if a.testHomeDir != "" {
		settingsPath = filepath.Join(home, ".claude", "settings.json")
	}
	a.emitRecoverLog(fmt.Sprintf("Checking file: %s", settingsPath))
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		a.emitRecoverLog("Found .claude/settings.json file. Removing...")
		if err := os.Remove(settingsPath); err != nil && !os.IsNotExist(err) {
			a.emitRecoverLog(fmt.Sprintf("Failed to remove .claude/settings.json file: %v", err))
			return fmt.Errorf("failed to remove .claude/settings.json file: %w", err)
		}
		a.emitRecoverLog("Successfully removed .claude/settings.json file.")
	} else {
		a.emitRecoverLog(".claude/settings.json file not found, skipping.")
	}

	// Remove ~/.claude/hooks/*.json files
	hooksDir := filepath.Join(home, ".claude", "hooks")
	a.emitRecoverLog(fmt.Sprintf("Checking hooks directory: %s", hooksDir))
	if entries, err := os.ReadDir(hooksDir); err == nil {
		foundHookConfig := false
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			foundHookConfig = true
			hookPath := filepath.Join(hooksDir, entry.Name())
			a.emitRecoverLog(fmt.Sprintf("Found hook config: %s. Removing...", hookPath))
			if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
				a.emitRecoverLog(fmt.Sprintf("Failed to remove hook config %s: %v", hookPath, err))
				return fmt.Errorf("failed to remove hook config %s: %w", hookPath, err)
			}
			a.emitRecoverLog(fmt.Sprintf("Successfully removed hook config: %s", hookPath))
		}
		if !foundHookConfig {
			a.emitRecoverLog("No .json hook config files found, skipping.")
		}
	} else if os.IsNotExist(err) {
		a.emitRecoverLog(".claude/hooks directory not found, skipping.")
	} else {
		a.emitRecoverLog(fmt.Sprintf("Failed to read hooks directory: %v", err))
		return fmt.Errorf("failed to read hooks directory: %w", err)
	}

	a.emitRecoverLog("Recovery process completed successfully.")
	return nil
}
func (a *App) emitRecoverLog(msg string) {
	a.emitEvent("recover-log", msg)
}
func (a *App) ShowMessage(title, message string) {
	a.emitEvent("show-message", map[string]string{
		"title":   title,
		"message": message,
	})
}

// ShowToast emits a lightweight toast notification to the frontend.
// typ can be "success", "error", "warning", or "info" (default).
func (a *App) ShowToast(message, typ string) {
	if typ == "" {
		typ = "info"
	}
	a.emitEvent("show-toast", map[string]interface{}{
		"message":  message,
		"type":     typ,
		"duration": 3500,
	})
}

// hasWailsEventsContext reports whether a.ctx is the real Wails lifecycle
// context that carries the frontend events bus. context.Background() and nil
// are NOT valid — calling runtime.EventsEmit with them log.Fatals and aborts
// the whole process (which used to fail go test ./gui/ with package-level FAIL
// and no --- FAIL: TestXxx after async AI assistant loops finished).
func (a *App) hasWailsEventsContext() bool {
	if a == nil || a.ctx == nil {
		return false
	}
	return a.ctx.Value("events") != nil
}

// emitEvent is the only safe way to push Wails events from App. It never
// calls runtime.EventsEmit unless the lifecycle context is present, so
// headless/test code cannot kill the process via log.Fatalf.
func (a *App) emitEvent(name string, data ...interface{}) {
	if !a.hasWailsEventsContext() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[wails] emitEvent skipped name=%s err=%v", name, r)
		}
	}()
	runtime.EventsEmit(a.ctx, name, data...)
}
func (a *App) ClipboardGetText() (string, error) {
	// Try Wails runtime first
	if a.ctx != nil {
		text, err := runtime.ClipboardGetText(a.ctx)
		if err == nil && text != "" {
			return text, nil
		}
	}
	// Fallback for macOS: use pbpaste command
	cmd := exec.Command("pbpaste")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		return out.String(), nil
	}
	return "", nil
}
func (a *App) fetchRemoteMarkdown(repo, file string) (string, error) {
	// Use GitHub API with timestamp to bypass all caches
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=main&t=%d", repo, file, time.Now().UnixNano())
	// Create a new transport to avoid connection reuse caching
	transport := &http.Transport{
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "Failed to create request: " + err.Error(), nil
	}
	// GitHub API headers - request raw content directly
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	req.Header.Set("User-Agent", "MaClaw-App")
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("Pragma", "no-cache")
	// Add GitHub token for authentication (helps avoid rate limiting).
	// Uses shared ResolveGitHubToken: env GITHUB_TOKEN > built-in default.
	token := skill.ResolveGitHubToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "Failed to fetch remote message: " + err.Error(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Remote content unavailable (Status: %d %s)", resp.StatusCode, resp.Status), nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "Error reading remote content: " + err.Error(), nil
	}
	return string(data), nil
}
func (a *App) ReadBBS() (string, error) {
	return a.fetchRemoteMarkdown("rapidaicoder/msg", "bbs.md")
}
func (a *App) ReadTutorial() (string, error) {
	return a.fetchRemoteMarkdown("rapidaicoder/msg", "tutorial.md")
}
func (a *App) ReadThanks() (string, error) {
	return a.fetchRemoteMarkdown("rapidaicoder/msg", "thanks.md")
}

// splitVersionPreRelease splits a version string into its numeric part and
// pre-release suffix. e.g. "1.3.0-beta.1" → ("1.3.0", "beta.1"),
// "1.3.0" → ("1.3.0", "").
func splitVersionPreRelease(version string) (string, string) {
	// Find the first hyphen that separates the numeric part from pre-release.
	// Must come after at least one digit (to avoid splitting negative numbers, though
	// versions don't have those).
	idx := strings.IndexByte(version, '-')
	if idx <= 0 {
		return version, ""
	}
	return version[:idx], version[idx+1:]
}

// preReleaseWeight returns an ordering weight for pre-release labels.
// Lower weight = earlier in release order. No pre-release (stable) = highest.
func preReleaseWeight(preRelease string) int {
	if preRelease == "" {
		return 100 // stable is always "newer" than any pre-release of same version
	}
	lower := strings.ToLower(preRelease)
	if strings.HasPrefix(lower, "alpha") {
		return 10
	}
	if strings.HasPrefix(lower, "beta") {
		return 20
	}
	if strings.HasPrefix(lower, "rc") {
		return 30
	}
	return 15 // unknown pre-release type between alpha and beta
}

// preReleaseNumber extracts the numeric suffix from a pre-release label.
// e.g. "beta.2" → 2, "rc1" → 1, "beta" → 0
func preReleaseNumber(preRelease string) int {
	// Try common separators: "beta.2", "beta-2", "rc1"
	num := 0
	for i := len(preRelease) - 1; i >= 0; i-- {
		if preRelease[i] >= '0' && preRelease[i] <= '9' {
			continue
		}
		if i < len(preRelease)-1 {
			fmt.Sscanf(preRelease[i+1:], "%d", &num)
		}
		break
	}
	return num
}

// compareVersions returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal.
// Supports semver pre-release suffixes: 1.3.0-beta.1 < 1.3.0-beta.2 < 1.3.0-rc.1 < 1.3.0 (stable).
func compareVersions(v1, v2 string) int {
	// Normalize: strip "v"/"V" prefix, lowercase, take first word
	clean := func(s string) string {
		s = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(s)), "v")
		return strings.Split(s, " ")[0]
	}
	v1 = clean(v1)
	v2 = clean(v2)

	numeric1, pre1 := splitVersionPreRelease(v1)
	numeric2, pre2 := splitVersionPreRelease(v2)

	// Compare numeric parts first
	parts1 := strings.Split(numeric1, ".")
	parts2 := strings.Split(numeric2, ".")
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for i := 0; i < maxLen; i++ {
		val1 := 0
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &val1)
		}
		val2 := 0
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &val2)
		}
		if val1 > val2 {
			return 1
		}
		if val1 < val2 {
			return -1
		}
	}

	// Numeric parts are equal — compare pre-release suffixes.
	// Stable (no pre-release) > any pre-release of the same version.
	w1 := preReleaseWeight(pre1)
	w2 := preReleaseWeight(pre2)
	if w1 != w2 {
		if w1 > w2 {
			return 1
		}
		return -1
	}
	// Same pre-release type — compare numeric suffix (beta.1 vs beta.2)
	n1 := preReleaseNumber(pre1)
	n2 := preReleaseNumber(pre2)
	if n1 > n2 {
		return 1
	}
	if n1 < n2 {
		return -1
	}
	return 0
}
func (a *App) getInstalledClaudeVersion(claudePath string) (string, error) {
	// Check if the path exists
	if _, err := os.Stat(claudePath); err != nil {
		// If explicit path fails, try finding it in PATH if it's just "claude"
		if claudePath == "claude" {
			path, err := exec.LookPath("claude")
			if err != nil {
				return "", err
			}
			claudePath = path
		} else {
			return "", err
		}
	}
	var cmd *exec.Cmd
	cmd = createVersionCmd(claudePath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output format example: claude-code/0.2.29 darwin-arm64 node-v22.12.0
	output := strings.TrimSpace(string(out))
	parts := strings.Split(output, " ")
	if len(parts) > 0 {
		// "claude-code/0.2.29"
		verParts := strings.Split(parts[0], "/")
		if len(verParts) == 2 {
			return verParts[1], nil
		}
		// If output is just the version (unlikely but possible)
		if len(parts) == 1 && strings.Contains(parts[0], ".") {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("unexpected output format: %s", output)
}
func (a *App) getLatestNpmVersion(npmPath string, packageName string) (string, error) {
	var cmd *exec.Cmd
	// Use npm view <package> version
	localCacheDir := a.GetLocalCacheDir()
	if err := os.MkdirAll(localCacheDir, 0755); err != nil {
		a.log(fmt.Sprintf("Warning: Failed to create local npm cache dir: %v", err))
	}
	args := []string{"view", packageName, "version", "--cache", localCacheDir}
	if normalizeAppLanguageKind(a.CurrentLanguage).IsChinese() {
		args = append(args, "--registry=https://registry.npmmirror.com")
	}
	cmd = createNpmInstallCmd(npmPath, args) // Using createNpmInstallCmd as it's a general npm command runner
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ListPythonEnvironments returns a list of all available Python environments
func (a *App) ListPythonEnvironments() []corelib.PythonEnvironment {
	envs := []corelib.PythonEnvironment{}
	// Add default "None" option
	envs = append(envs, corelib.PythonEnvironment{
		Name: "None (Default)",
		Path: "",
		Type: "system",
	})
	// Detect Conda environments
	condaEnvs := a.detectCondaEnvironments()
	envs = append(envs, condaEnvs...)
	// Could add detection for virtualenv, venv, etc. here
	return envs
}

// detectCondaEnvironments finds all Anaconda/Miniconda environments
func (a *App) detectCondaEnvironments() []corelib.PythonEnvironment {
	envs := []corelib.PythonEnvironment{}
	envMap := make(map[string]corelib.PythonEnvironment)
	// Helper to add env
	addEnv := func(name, path string) {
		if name == "" || path == "" {
			return
		}
		if _, exists := envMap[name]; !exists {
			a.log(a.tr("Found conda environment: %s at %s", name, path))
			envMap[name] = corelib.PythonEnvironment{
				Name: name,
				Path: path,
				Type: "conda",
			}
		}
	}
	// 1. Try 'conda env list'
	condaCmd := a.findCondaCommand()
	if condaCmd != "" {
		a.log(a.tr("Using conda command: ") + condaCmd)
		var cmd *exec.Cmd
		if goruntime.GOOS == "windows" {
			// Use platform-specific function to create command with hidden window
			cmd = createCondaEnvListCmd(condaCmd)
		} else {
			cmd = exec.Command(condaCmd, "env", "list")
		}
		output, err := cmd.CombinedOutput()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				// Skip comments and empty lines
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) == 0 {
					continue
				}
				var name, path string
				// Handle parsing
				// Case 1: "* /path" (unnamed, active)
				// Case 2: "/path" (unnamed)
				// Case 3: "name * /path" (named, active)
				// Case 4: "name /path" (named)
				firstIsPath := strings.Contains(parts[0], "/") || strings.Contains(parts[0], "\\") || (goruntime.GOOS == "windows" && strings.Contains(parts[0], ":"))
				if parts[0] == "*" {
					// Case 1
					if len(parts) > 1 {
						path = strings.Join(parts[1:], " ")
						name = filepath.Base(path)
					}
				} else if firstIsPath {
					// Case 2
					path = strings.Join(parts, " ")
					name = filepath.Base(path)
				} else {
					// Case 3 or 4
					name = parts[0]
					if len(parts) > 1 {
						startIdx := 1
						if parts[1] == "*" {
							startIdx = 2
						}
						if startIdx < len(parts) {
							path = strings.Join(parts[startIdx:], " ")
						}
					}
				}
				addEnv(name, path)
			}
		} else {
			// Only log as info, not error - conda command failed but this is not critical
			a.log(a.tr("Note: Unable to list conda environments via command (conda may not be fully initialized): ") + err.Error())
		}
	}
	// 2. Scan common env directories (Fallback/Augment)
	roots := []string{}
	// Conda installation root envs
	condaRoot := a.getCondaRoot()
	if condaRoot != "" {
		roots = append(roots, filepath.Join(condaRoot, "envs"))
		// Also add root environment (base)
		addEnv("base", condaRoot)
	}
	// User .conda envs
	home, _ := os.UserHomeDir()
	roots = append(roots, filepath.Join(home, ".conda", "envs"))
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					addEnv(entry.Name(), filepath.Join(root, entry.Name()))
				}
			}
		}
	}
	// Convert map to slice
	for _, env := range envMap {
		envs = append(envs, env)
	}
	// Only log if conda environments were found
	if len(envs) > 0 {
		a.log(a.tr("Total conda environments found: %d", len(envs)))
	}
	return envs
}

// findCondaCommand tries to locate the conda executable
func (a *App) findCondaCommand() string {
	// Try common conda command names (include .bat for Windows)
	condaCmds := []string{"conda.exe", "conda.bat", "conda"}
	// First check CONDA_EXE environment variable
	if condaExe := os.Getenv("CONDA_EXE"); condaExe != "" {
		if _, err := os.Stat(condaExe); err == nil {
			a.log(a.tr("Found conda from CONDA_EXE: ") + condaExe)
			return condaExe
		}
	}
	for _, cmd := range condaCmds {
		// Check if command exists in PATH
		if path, err := exec.LookPath(cmd); err == nil {
			a.log(a.tr("Found conda in PATH: ") + path)
			return path
		}
	}
	// Try common installation paths
	commonPaths := a.getCommonCondaPaths()
	a.log(a.tr("Searching for conda in %d common paths...", len(commonPaths)))
	for _, basePath := range commonPaths {
		// Check if the base path exists first
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			continue
		}
		for _, cmd := range condaCmds {
			fullPath := filepath.Join(basePath, cmd)
			if _, err := os.Stat(fullPath); err == nil {
				a.log(a.tr("Found conda at: ") + fullPath)
				return fullPath
			}
			// Also check in Scripts subdirectory (Windows)
			scriptsPath := filepath.Join(basePath, "Scripts", cmd)
			if _, err := os.Stat(scriptsPath); err == nil {
				a.log(a.tr("Found conda at: ") + scriptsPath)
				return scriptsPath
			}
			// Check in condabin subdirectory (newer Anaconda installations)
			condabinPath := filepath.Join(basePath, "condabin", cmd)
			if _, err := os.Stat(condabinPath); err == nil {
				a.log(a.tr("Found conda at: ") + condabinPath)
				return condabinPath
			}
			// Check in bin subdirectory (Linux/macOS)
			binPath := filepath.Join(basePath, "bin", cmd)
			if _, err := os.Stat(binPath); err == nil {
				a.log(a.tr("Found conda at: ") + binPath)
				return binPath
			}
		}
	}
	// No need to log if conda not found - it's normal if user doesn't use conda
	return ""
}

// getCommonCondaPaths returns platform-specific common conda installation paths
func (a *App) getCommonCondaPaths() []string {
	paths := []string{}
	homeDir := a.GetUserHomeDir()
	// Check CONDA_PREFIX environment variable first
	if condaPrefix := os.Getenv("CONDA_PREFIX"); condaPrefix != "" {
		paths = append(paths, condaPrefix)
	}
	// Check CONDA_EXE environment variable
	if condaExe := os.Getenv("CONDA_EXE"); condaExe != "" {
		// CONDA_EXE points to the conda executable, go up to get root
		condaDir := filepath.Dir(condaExe)
		if strings.HasSuffix(strings.ToLower(condaDir), "scripts") ||
			strings.HasSuffix(strings.ToLower(condaDir), "library\\bin") {
			paths = append(paths, filepath.Dir(condaDir))
		} else {
			paths = append(paths, condaDir)
		}
	}
	// User home directory paths
	paths = append(paths,
		filepath.Join(homeDir, "anaconda3"),
		filepath.Join(homeDir, "miniconda3"),
		filepath.Join(homeDir, "Anaconda3"),
		filepath.Join(homeDir, "Miniconda3"),
	)
	// macOS common paths
	if goruntime.GOOS == "darwin" {
		paths = append(paths,
			"/opt/anaconda3",
			"/opt/miniconda3",
			"/usr/local/anaconda3",
			"/usr/local/miniconda3",
			"/opt/homebrew/anaconda3",
			"/opt/homebrew/miniconda3",
			"/opt/homebrew/Caskroom/miniconda/base",
			"/opt/homebrew/Caskroom/anaconda/base",
			"/usr/local/Caskroom/miniconda/base",
			"/usr/local/Caskroom/anaconda/base",
		)
	}
	// AppData Local paths (Windows common location)
	appDataLocal := os.Getenv("LOCALAPPDATA")
	if appDataLocal != "" {
		paths = append(paths,
			filepath.Join(appDataLocal, "anaconda3"),
			filepath.Join(appDataLocal, "miniconda3"),
			filepath.Join(appDataLocal, "Continuum", "anaconda3"),
			filepath.Join(appDataLocal, "Continuum", "miniconda3"),
		)
	}
	// ProgramData paths (all users installation)
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = "C:\\ProgramData"
	}
	paths = append(paths,
		filepath.Join(programData, "anaconda3"),
		filepath.Join(programData, "miniconda3"),
		filepath.Join(programData, "Anaconda3"),
		filepath.Join(programData, "Miniconda3"),
	)
	// Common drive root installations
	for _, drive := range []string{"C:", "D:", "E:"} {
		root := drive + string(filepath.Separator)
		paths = append(paths,
			filepath.Join(root, "anaconda3"),
			filepath.Join(root, "miniconda3"),
			filepath.Join(root, "Anaconda3"),
			filepath.Join(root, "Miniconda3"),
			filepath.Join(root, "ProgramData", "anaconda3"),
			filepath.Join(root, "ProgramData", "miniconda3"),
		)
	}
	return paths
}

// getCondaRoot finds the conda installation root directory
func (a *App) getCondaRoot() string {
	// First try to get from conda command location
	condaCmd := a.findCondaCommand()
	if condaCmd != "" {
		// If conda is in PATH or found directly, parse its path
		// Conda executable is usually in [root]/Scripts/conda.exe or [root]/bin/conda
		condaDir := filepath.Dir(condaCmd)
		// Check if we're in Scripts or bin directory
		if strings.HasSuffix(strings.ToLower(condaDir), "scripts") ||
			strings.HasSuffix(strings.ToLower(condaDir), "bin") {
			// Go up one level to get the root
			return filepath.Dir(condaDir)
		}
		// Otherwise, condaDir itself might be the root
		return condaDir
	}
	// If not found, try common installation paths
	commonPaths := a.getCommonCondaPaths()
	for _, path := range commonPaths {
		// Check if activate.bat exists (Windows) or activate exists (Unix)
		activateScript := filepath.Join(path, "Scripts", "activate.bat")
		if _, err := os.Stat(activateScript); err == nil {
			return path
		}
		activateScript = filepath.Join(path, "bin", "activate")
		if _, err := os.Stat(activateScript); err == nil {
			return path
		}
	}
	return ""
}

type SystemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	OSVersion string `json:"os_version"`
}

func (a *App) GetSystemInfo() SystemInfo {
	return SystemInfo{
		OS:        goruntime.GOOS,
		Arch:      goruntime.GOARCH,
		OSVersion: a.getOSVersion(),
	}
}
func (a *App) getOSVersion() string {
	switch goruntime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	case "windows":
		// Use platform-specific function to hide window
		if ver := getWindowsVersionHidden(); ver != "" {
			return ver
		}
	case "linux":
		// Try /etc/os-release
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					return strings.Trim(line[12:], "\"")
				}
			}
		}
	}
	return "Unknown"
}
func (a *App) PackLog(logContent string) (string, error) {
	// Create a temp file for the zip
	timestamp := time.Now().Format("20060102_150405")
	fileName := fmt.Sprintf("maclaw_log_%s.zip", timestamp)
	tempDir := a.GetTempDir()
	zipPath := filepath.Join(tempDir, fileName)
	// Create the zip file
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()
	// Initialize zip writer
	archive := zip.NewWriter(zipFile)
	defer archive.Close()
	// Create file inside zip
	f, err := archive.Create("install_log.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create file in zip: %w", err)
	}
	// Write content
	_, err = f.Write([]byte(logContent))
	if err != nil {
		return "", fmt.Errorf("failed to write content to zip: %w", err)
	}
	return zipPath, nil
}

// SetBugReportEnabled starts or stops a bounded diagnostic collection session.
// Starting clears only logs and trajectories, then forces diagnostic settings.
// Stopping restores the values saved at the beginning of the session.
func (a *App) SetBugReportEnabled(enabled bool) (corelib.AppConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.AppConfig{}, err
	}
	if cfg.BugReportEnabled == enabled {
		return cfg, nil
	}
	if enabled {
		a.clearBugReportDiagnostics()
		return a.PatchConfigFields(map[string]interface{}{
			"bug_report_enabled":             true,
			"bug_report_previous_trajectory": cfg.LLMTrajectoryLogging,
			"bug_report_previous_log_detail": cfg.LogDetailEnabled,
			"llm_trajectory_logging":         true,
			"log_detail_enabled":             true,
		})
	} else {
		return a.PatchConfigFields(map[string]interface{}{
			"bug_report_enabled":     false,
			"llm_trajectory_logging": cfg.BugReportPreviousTrajectory,
			"log_detail_enabled":     cfg.BugReportPreviousLogDetail,
		})
	}
}

// clearBugReportDiagnostics empties, but does not remove, the diagnostic
// directories. Keeping their roots prevents logger reinitialization races;
// open files on Windows are truncated when removal is unavailable.
func (a *App) clearBugReportDiagnostics() {
	for _, name := range []string{"logs", "trajectories"} {
		dir := filepath.Join(a.getMaclawBaseDir(), name)
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			// Diagnostic roots must remain inside ~/.maclaw. Never follow a
			// user-created link here: successful report cleanup must not erase an
			// arbitrary external directory.
			log.Printf("[bug-report] skipping symbolic-link diagnostic root %s", dir)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("[bug-report] cannot create %s directory %s: %v", name, dir, err)
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("[bug-report] cannot read %s directory %s: %v", name, dir, err)
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				// Windows does not allow deleting an open diagnostic file. Clear its
				// contents instead; an inaccessible file must not block future logs.
				if !entry.IsDir() {
					if truncateErr := truncateBugReportDiagnosticFile(path); truncateErr == nil {
						continue
					}
				}
				log.Printf("[bug-report] keeping inaccessible %s entry %s: %v", name, path, err)
			}
		}
	}
}

// truncateBugReportDiagnosticFile clears a file without replacing its path.
// Keeping the path is important for loggers that already hold the file open.
func truncateBugReportDiagnosticFile(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	return file.Close()
}

func (a *App) SelectBugReportScreenshots() []string {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Select AI assistant screenshots",
		Filters: []runtime.FileFilter{{DisplayName: "Images (*.png;*.jpg;*.jpeg;*.webp;*.bmp)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.bmp"}},
	})
	if err != nil {
		return []string{}
	}
	if files == nil {
		return []string{}
	}
	return normalizeBugReportScreenshotPaths(files)
}

const maxBugReportScreenshots = 12

func normalizeBugReportScreenshotPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	selected := make([]string, 0, min(len(paths), maxBugReportScreenshots))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		key := cleanPath
		if goruntime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, cleanPath)
		if len(selected) == maxBugReportScreenshots {
			break
		}
	}
	return selected
}

// BugReportScreenshotPreviewDataURL creates a compact, browser-safe thumbnail
// for a user-selected diagnostic screenshot.  The frontend receives only image
// data, never a local file URL/path, and therefore cannot accidentally expose it.
func (a *App) BugReportScreenshotPreviewDataURL(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("screenshot path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Size() > 10*1024*1024 {
		return "", fmt.Errorf("screenshot must be an image file no larger than 10 MB")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return "", fmt.Errorf("inspect screenshot: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return "", fmt.Errorf("screenshot dimensions are not supported")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return "", fmt.Errorf("decode screenshot: %w", err)
	}
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return "", fmt.Errorf("screenshot has invalid dimensions")
	}
	const maxEdge = 240
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxEdge || height > maxEdge {
		if width >= height {
			height = max(1, height*maxEdge/width)
			width = maxEdge
		} else {
			width = max(1, width*maxEdge/height)
			height = maxEdge
		}
	}
	thumbnail := image.NewRGBA(image.Rect(0, 0, width, height))
	// Catmull-Rom preserves text and UI edges materially better than nearest
	// neighbour while still producing a compact preview for the report dialog.
	draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), source, bounds, draw.Over, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, thumbnail); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

// SubmitBugReport makes a diagnostics archive and posts it to HubCenter using
// the existing authenticated SkillMarket session. The local archive is retained
// for a retry if the request fails.
func (a *App) SubmitBugReport(osVersion, description string, screenshots []string) (map[string]interface{}, error) {
	a.bugReportUploadMu.Lock()
	defer a.bugReportUploadMu.Unlock()
	osVersion, description = strings.TrimSpace(osVersion), strings.TrimSpace(description)
	if osVersion == "" || description == "" {
		return nil, fmt.Errorf("operating system and problem description are required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RemoteHubCenterURL) == "" || strings.TrimSpace(cfg.SkillMarketSessionToken) == "" {
		return nil, fmt.Errorf("please sign in to HubCenter before submitting a report")
	}
	zipPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("maclaw-diagnostics-%d.zip", time.Now().UnixNano()))
	out, err := os.Create(zipPath)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(out)
	// Until it has become the persisted retry archive, every early exit must
	// close and remove the partial ZIP. Closing first is essential on Windows,
	// where an open file cannot be removed.
	keepArchive, archiveClosed, outputClosed := false, false, false
	defer func() {
		if !archiveClosed {
			_ = zw.Close()
		}
		if !outputClosed {
			_ = out.Close()
		}
		if !keepArchive {
			_ = os.Remove(zipPath)
		}
	}()
	var includedFiles int
	for _, root := range []string{"logs", "trajectories"} {
		base := filepath.Join(a.getMaclawBaseDir(), root)
		baseInfo, statErr := os.Lstat(base)
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				log.Printf("[bug-report] skipping unreadable %s directory %s: %v", root, base, statErr)
			}
			continue
		}
		if baseInfo.Mode()&os.ModeSymlink != 0 {
			log.Printf("[bug-report] skipping symbolic-link diagnostic root: %s", base)
			continue
		}
		if !baseInfo.IsDir() {
			log.Printf("[bug-report] skipping %s because it is not a directory: %s", root, base)
			continue
		}
		if err := filepath.Walk(base, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				if os.IsPermission(walkErr) {
					log.Printf("[bug-report] skipping inaccessible diagnostic path %s: %v", path, walkErr)
					return nil
				}
				return walkErr
			}
			if info == nil || info.IsDir() {
				return nil
			}
			// Diagnostics directories are user-writable. Do not follow symlinks
			// or read devices/FIFOs, which could leak data outside ~/.maclaw or
			// block archive creation indefinitely.
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(a.getMaclawBaseDir(), path)
			if err != nil {
				return err
			}
			in, err := os.Open(path)
			if err != nil {
				if os.IsPermission(err) {
					log.Printf("[bug-report] skipping unreadable diagnostic file %s: %v", path, err)
					return nil
				}
				return err
			}
			w, err := zw.Create(filepath.ToSlash(rel))
			if err != nil {
				_ = in.Close()
				return err
			}
			_, err = io.Copy(w, in)
			closeErr := in.Close()
			if err == nil {
				includedFiles++
			}
			if err == nil {
				err = closeErr
			}
			return err
		}); err != nil && !os.IsNotExist(err) && !os.IsPermission(err) {
			return nil, err
		}
	}
	if includedFiles == 0 {
		return nil, fmt.Errorf("no diagnostic files found; enable problem reporting and reproduce the issue before submitting")
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	archiveClosed = true
	if err := out.Close(); err != nil {
		return nil, err
	}
	outputClosed = true
	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return nil, err
	}
	if zipInfo.Size() > 100<<20 {
		return nil, fmt.Errorf("diagnostics archive exceeds the 100 MB upload limit")
	}
	a.setPendingBugReportUpload(zipPath, includedFiles)
	keepArchive = true
	return a.uploadBugReportArchive(zipPath, includedFiles, osVersion, description, screenshots)
}

// RetryBugReportUpload retries the latest failed submission without recreating
// the archive. This avoids losing the exact diagnostics captured when the
// problem occurred.
func (a *App) RetryBugReportUpload(osVersion, description string, screenshots []string) (map[string]interface{}, error) {
	a.bugReportUploadMu.Lock()
	defer a.bugReportUploadMu.Unlock()
	pending := a.getPendingBugReportUpload()
	if pending == nil || strings.TrimSpace(pending.zipPath) == "" {
		return nil, fmt.Errorf("no failed diagnostic upload is available to retry")
	}
	if _, err := os.Stat(pending.zipPath); err != nil {
		a.clearPendingBugReportUpload(pending.zipPath)
		return nil, fmt.Errorf("saved diagnostics archive is no longer available: %w", err)
	}
	return a.uploadBugReportArchive(pending.zipPath, pending.includedFiles, osVersion, description, screenshots)
}

// HasPendingBugReportUpload reports whether this running desktop client still
// has diagnostics retained from a failed submission.
func (a *App) HasPendingBugReportUpload() bool {
	return a.getPendingBugReportUpload() != nil
}

func (a *App) pendingBugReportUploadPath() string {
	return filepath.Join(a.GetTempDir(), "pending-bug-report-upload.json")
}

func (a *App) isPendingBugReportArchivePath(path string) bool {
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(filepath.Clean(a.GetTempDir()), cleanPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return strings.EqualFold(filepath.Ext(cleanPath), ".zip") && strings.HasPrefix(filepath.Base(cleanPath), "maclaw-diagnostics-")
}

func (a *App) getPendingBugReportUpload() *pendingBugReportUpload {
	a.pendingBugReportUploadMu.Lock()
	defer a.pendingBugReportUploadMu.Unlock()
	pending := a.pendingBugReportUpload
	if pending == nil {
		data, err := os.ReadFile(a.pendingBugReportUploadPath())
		if err == nil {
			var saved pendingBugReportUploadMetadata
			if json.Unmarshal(data, &saved) == nil && a.isPendingBugReportArchivePath(saved.ZipPath) {
				pending = &pendingBugReportUpload{zipPath: saved.ZipPath, includedFiles: saved.IncludedFiles}
				a.pendingBugReportUpload = pending
			}
		}
	}
	if pending == nil || strings.TrimSpace(pending.zipPath) == "" {
		return nil
	}
	info, err := os.Lstat(pending.zipPath)
	if !a.isPendingBugReportArchivePath(pending.zipPath) || err != nil || !info.Mode().IsRegular() {
		a.pendingBugReportUpload = nil
		_ = os.Remove(a.pendingBugReportUploadPath())
		return nil
	}
	return pending
}

func (a *App) setPendingBugReportUpload(zipPath string, includedFiles int) {
	a.pendingBugReportUploadMu.Lock()
	defer a.pendingBugReportUploadMu.Unlock()
	if !a.isPendingBugReportArchivePath(zipPath) {
		return
	}
	previous := a.pendingBugReportUpload
	a.pendingBugReportUpload = &pendingBugReportUpload{zipPath: zipPath, includedFiles: includedFiles}
	metadata, err := json.Marshal(pendingBugReportUploadMetadata{ZipPath: zipPath, IncludedFiles: includedFiles})
	if err == nil {
		metadataPath := a.pendingBugReportUploadPath()
		temporaryPath := metadataPath + ".tmp"
		if os.WriteFile(temporaryPath, metadata, 0o600) == nil {
			// Windows cannot replace an existing file with Rename. Removing the
			// previous small metadata file keeps the persisted retry state current.
			_ = os.Remove(metadataPath)
			if err := os.Rename(temporaryPath, metadataPath); err != nil {
				_ = os.Remove(temporaryPath)
			}
		}
	}
	if previous != nil && previous.zipPath != zipPath {
		_ = os.Remove(previous.zipPath)
	}
}

func (a *App) clearPendingBugReportUpload(zipPath string) {
	a.pendingBugReportUploadMu.Lock()
	defer a.pendingBugReportUploadMu.Unlock()
	if a.pendingBugReportUpload != nil && a.pendingBugReportUpload.zipPath == zipPath {
		a.pendingBugReportUpload = nil
		_ = os.Remove(a.pendingBugReportUploadPath())
	}
}

func (a *App) uploadBugReportArchive(zipPath string, includedFiles int, osVersion, description string, screenshots []string) (map[string]interface{}, error) {
	osVersion, description = strings.TrimSpace(osVersion), strings.TrimSpace(description)
	if osVersion == "" || description == "" {
		return nil, fmt.Errorf("operating system and problem description are required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RemoteHubCenterURL) == "" || strings.TrimSpace(cfg.SkillMarketSessionToken) == "" {
		return nil, fmt.Errorf("please sign in to HubCenter before submitting a report")
	}
	file, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	convertedScreenshots := make([]string, 0)
	defer func() {
		for _, path := range convertedScreenshots {
			_ = os.Remove(path)
		}
	}()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("os_version", osVersion)
	// Keep this field name aligned with HubCenter's ProblemReport.GUIVersion
	// (multipart key: gui_version). remoteAppVersion also supplies a stable
	// fallback for local/dev builds where the linker has not set main.version.
	_ = mw.WriteField("gui_version", remoteAppVersion())
	_ = mw.WriteField("description", description)
	part, err := mw.CreateFormFile("diagnostics", filepath.Base(zipPath))
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(part, file); err != nil {
		return nil, err
	}
	for _, path := range normalizeBugReportScreenshotPaths(screenshots) {
		info, statErr := os.Stat(path)
		ext := strings.ToLower(filepath.Ext(path))
		if statErr != nil || info.IsDir() || info.Size() > 10<<20 || (ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" && ext != ".bmp") {
			continue
		}
		if ext == ".bmp" {
			converted, convertErr := a.convertBugReportBMPToPNG(path)
			if convertErr != nil {
				return nil, fmt.Errorf("convert BMP screenshot %q: %w", filepath.Base(path), convertErr)
			}
			path, ext = converted, ".png"
			convertedScreenshots = append(convertedScreenshots, converted)
		}
		if err := validateBugReportScreenshot(path); err != nil {
			return nil, fmt.Errorf("invalid screenshot %q: %w", filepath.Base(path), err)
		}
		if err := copyBugReportScreenshotPart(mw, path); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.RemoteHubCenterURL, "/")+"/api/v1/problem-reports", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SkillMarketSessionToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HubCenter rejected report: %s", strings.TrimSpace(string(data)))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	reportID, hasReportID := result["id"].(string)
	if !hasReportID || strings.TrimSpace(reportID) == "" {
		// A 2xx response without a report identifier cannot be treated as a
		// completed submission: preserve the local archive for retry instead of
		// discarding diagnostics after a malformed proxy/server response.
		return nil, fmt.Errorf("HubCenter returned a successful response without a report identifier")
	}
	// Close before removing: Windows does not permit deletion of an archive that
	// this process still has open. The deferred Close remains harmless on the
	// error paths above.
	if err := file.Close(); err != nil {
		log.Printf("[bug-report] close uploaded diagnostics archive %s: %v", zipPath, err)
	}
	// A successful upload no longer needs a local copy. Failed requests return
	// before this point, intentionally preserving it for direct retry.
	if err := os.Remove(zipPath); err != nil {
		log.Printf("[bug-report] remove uploaded diagnostics archive %s: %v", zipPath, err)
	}
	a.clearPendingBugReportUpload(zipPath)
	// The HubCenter has accepted the archive. Clear exactly the local data that
	// was collected for this report so it is not silently included next time.
	// Cleanup is best-effort: a post-upload Windows file lock must not turn a
	// successful, already-persisted report into an apparent failed submission.
	a.clearBugReportDiagnostics()
	result["diagnostic_file_count"] = includedFiles
	return result, nil
}

// ListMyBugReports returns only reports owned by the signed-in HubCenter user.
func (a *App) ListMyBugReports() (map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RemoteHubCenterURL) == "" || strings.TrimSpace(cfg.SkillMarketSessionToken) == "" {
		return nil, fmt.Errorf("please sign in to HubCenter to view your reports")
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.RemoteHubCenterURL, "/")+"/api/v1/problem-reports/mine", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SkillMarketSessionToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HubCenter rejected report lookup: %s", strings.TrimSpace(string(data)))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) convertBugReportBMPToPNG(source string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	img, _, err := image.Decode(in)
	if err != nil {
		return "", err
	}
	outPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("bug-report-screenshot-%d.png", time.Now().UnixNano()))
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	err = png.Encode(out, img)
	closeErr := out.Close()
	if err != nil {
		return "", err
	}
	return outPath, closeErr
}

func validateBugReportScreenshot(path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	config, format, err := image.DecodeConfig(in)
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 50_000_000 {
		return fmt.Errorf("image dimensions exceed the limit")
	}
	if format != "png" && format != "jpeg" && format != "webp" && format != "bmp" {
		return fmt.Errorf("unsupported image format")
	}
	return nil
}

func copyBugReportScreenshotPart(mw *multipart.Writer, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	part, err := mw.CreateFormFile("screenshots", filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, in)
	return err
}

// normalizeOpenPath expands portable ~/… home paths and rejects empty results.
// Windows does not interpret "~", so open/stat must always expand first.
// Prefer normalizeOpenPathForApp when an *App is available so testHomeDir applies.
func normalizeOpenPath(path string) (string, error) {
	return normalizeOpenPathHome(path, "")
}

func normalizeOpenPathHome(path, home string) (string, error) {
	if strings.TrimSpace(home) != "" {
		path = filepath.Clean(corelib.ExpandHomePathWithHome(path, home))
	} else {
		path = filepath.Clean(corelib.ExpandHomePath(path))
	}
	if path == "" || path == "." {
		return "", fmt.Errorf("empty path")
	}
	return path, nil
}

func (a *App) normalizeOpenPathForApp(path string) (string, error) {
	home := ""
	if a != nil {
		home = a.GetUserHomeDir()
	}
	return normalizeOpenPathHome(path, home)
}

// startSystemOpen opens path with the OS default handler (non-blocking).
func startSystemOpen(path string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command(windowsSystemExecutable("rundll32.exe"), "url.dll,FileProtocolHandler", filepath.FromSlash(path))
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("unsupported platform")
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (a *App) ShowItemInFolder(path string) error {
	path, err := a.normalizeOpenPathForApp(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		path = filepath.FromSlash(path)
		cmd = exec.Command(windowsSystemExecutable("explorer.exe"), "/select,", path)
		hideCommandWindow(cmd)
	case "linux":
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	default:
		return fmt.Errorf("unsupported platform")
	}
	// Use Start instead of Run to avoid waiting for the process and ignoring exit codes (like 1 on Windows)
	return cmd.Start()
}

func (a *App) OpenFileOrShowInFolder(path string) error {
	path, err := a.normalizeOpenPathForApp(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		log.Printf("[app-open-file] path unavailable path=%q err=%v", path, err)
		return err
	}
	if err := startSystemOpen(path); err != nil {
		log.Printf("[app-open-file] system open failed path=%q err=%v; falling back to reveal", path, err)
		return a.ShowItemInFolder(path)
	}
	return nil
}

func windowsSystemExecutable(name string) string {
	if goruntime.GOOS != "windows" {
		return name
	}
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		root = strings.TrimSpace(os.Getenv("windir"))
	}
	if root == "" {
		root = `C:\Windows`
	}
	candidates := []string{
		filepath.Join(root, "System32", name),
		filepath.Join(root, name),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return name
}

func (a *App) OpenSkillRunArtifact(runID string, artifactID string) error {
	return a.openSkillRunArtifactByID("", runID, artifactID, false)
}

// OpenProjectDirectory opens a directory in the system file explorer.
// This is a Wails binding method called from the ProjectDirBar component.
func (a *App) OpenProjectDirectory(path string) error {
	path, err := a.normalizeOpenPathForApp(path)
	if err != nil {
		return err
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		if err != nil {
			return err
		}
		return fmt.Errorf("not a directory: %s", path)
	}
	if err := startSystemOpen(path); err != nil {
		log.Printf("[OpenProjectDirectory] system open failed path=%q err=%v", path, err)
		return err
	}
	return nil
}

func (a *App) RevealSkillRunArtifact(runID string, artifactID string) error {
	return a.openSkillRunArtifactByID("", runID, artifactID, true)
}

func (a *App) OpenSkillRunArtifactForOwner(ownerID string, runID string, artifactID string) error {
	return a.openSkillRunArtifactByID(ownerID, runID, artifactID, false)
}

func (a *App) RevealSkillRunArtifactForOwner(ownerID string, runID string, artifactID string) error {
	return a.openSkillRunArtifactByID(ownerID, runID, artifactID, true)
}

func (a *App) openSkillRunArtifactByID(ownerID string, runID string, artifactID string, reveal bool) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	ownerID = strings.TrimSpace(ownerID)
	runID = strings.TrimSpace(runID)
	artifactID = strings.TrimSpace(artifactID)
	path, err := a.lookupSkillArtifactPathForOwner(ownerID, runID, artifactID)
	if err != nil {
		if entry, downloadErr := a.DownloadSkillRunArtifactForOwner(ownerID, runID, artifactID); downloadErr == nil && entry != nil && entry.Available {
			path, err = a.lookupSkillArtifactPathForOwner(ownerID, runID, artifactID)
		}
	}
	if err != nil {
		if a.skillRunner == nil {
			return err
		}
		status, statusErr := a.skillRunner.GetRunStatus(runID)
		if statusErr != nil {
			return statusErr
		}
		path, err = resolveSkillRunArtifactPath(status, artifactID)
		if err != nil {
			return err
		}
		if ownerID != "" && strings.TrimSpace(status.OwnerID) != "" && ownerID != strings.TrimSpace(status.OwnerID) {
			return fmt.Errorf("artifact owner mismatch")
		}
		a.registerSkillRunArtifacts(status)
	}
	if reveal {
		return a.ShowItemInFolder(path)
	}
	return a.OpenFileOrShowInFolder(path)
}

func (a *App) GetSkillsDir(toolName string) string {
	baseDir := filepath.Join(a.GetDataDir(), "skills")
	storageDir := filepath.Join(baseDir, "storage")

	// Migration: If storage doesn't exist but claude does, rename claude to storage
	// This ensures existing skills are preserved and shared
	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		oldDir := filepath.Join(baseDir, "claude")
		if _, err := os.Stat(oldDir); err == nil {
			os.Rename(oldDir, storageDir)
		}
	}

	return storageDir
}
func (a *App) selectFile(title string, filters []runtime.FileFilter) string {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: filters,
	})
	if err != nil {
		return ""
	}
	return selection
}

func (a *App) SelectSkillFile() string {
	return a.selectFile("Select Skill Zip File (.zip, usually with skill.md)", []runtime.FileFilter{{DisplayName: "Skill Zip Files (*.zip)", Pattern: "*.zip"}})
}

func (a *App) SelectAIAssistantFile() string {
	return a.selectFile("Select File for AI Assistant", nil)
}

// SelectAIAssistantFiles opens a native multi-file selection dialog and returns the selected file paths.
// Returns empty slice if user cancels or an error occurs.
func (a *App) SelectAIAssistantFiles() []string {
	selections, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Files for AI Assistant",
	})
	if err != nil {
		return []string{} // Return empty slice instead of nil for consistency
	}
	return selections
}

// getInstalledSkillDirs returns a list of installed skill directory names for both user and project locations
func (a *App) getInstalledSkillDirs(toolName string, location string, projectPath string) []string {
	var installedDirs []string
	configDirName := getToolConfigDirName(toolName)
	locationKind := normalizeSkillInstallLocationKind(location)

	var skillsDir string
	if locationKind.IsUser() {
		home, err := os.UserHomeDir()
		if err != nil {
			return installedDirs
		}
		skillsDir = filepath.Join(home, configDirName, "skills")
	} else if locationKind.IsProject() {
		if projectPath == "" {
			return installedDirs
		}
		skillsDir = filepath.Join(projectPath, configDirName, "skills")
	} else {
		return installedDirs
	}

	// Check if skills directory exists
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		return installedDirs
	}

	// Read all entries in the skills directory
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return installedDirs
	}

	// Collect directory names
	for _, entry := range entries {
		if entry.IsDir() {
			installedDirs = append(installedDirs, entry.Name())
		}
	}

	return installedDirs
}

func (a *App) ListSkills(toolName string) []corelib.Skill {
	skillsDir := a.GetSkillsDir(toolName)
	metadataPath := filepath.Join(skillsDir, "metadata.json")

	var defaultSkills []corelib.Skill
	// Add default skills for all tools
	defaultSkills = append(defaultSkills, corelib.Skill{
		Name:        "Claude Official Documentation Skill Package",
		Description: "Claude Official Documentation Skill Package",
		Type:        "address",
		Value:       "document-skills@anthropic-agent-skills",
	})
	defaultSkills = append(defaultSkills, corelib.Skill{
		Name:        "Superpowers Marketplace",
		Description: "Superpowers marketplace skill source",
		Type:        "address",
		Value:       "superpowers@superpowers-marketplace",
	})

	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return defaultSkills
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return defaultSkills
	}
	var skills []corelib.Skill
	json.Unmarshal(data, &skills)

	// Filter out duplicates of default skills if they exist in JSON
	// AND filter out 'address' type skills for Codex
	requiresZipSkills := normalizeRemoteToolNameKind(toolName).RequiresZipSkills()

	filteredSkills := defaultSkills
	for _, s := range skills {
		if requiresZipSkills && normalizeSkillTypeKind(s.Type).IsAddress() {
			continue
		}

		isDefault := false
		for _, ds := range defaultSkills {
			if s.Name == ds.Name {
				isDefault = true
				break
			}
		}
		if !isDefault {
			filteredSkills = append(filteredSkills, s)
		}
	}
	return filteredSkills
}

// ListSkillsWithInstallStatus returns skills list with installed status marked
func (a *App) ListSkillsWithInstallStatus(toolName string, location string, projectPath string) []corelib.Skill {
	// Get all available skills
	allSkills := a.ListSkills(toolName)

	// Get installed skill directories
	installedDirs := a.getInstalledSkillDirs(toolName, location, projectPath)
	installedMap := make(map[string]bool)
	for _, dir := range installedDirs {
		installedMap[dir] = true
	}

	// Also check enabledPlugins in ~/.claude/settings.json for address-type skills
	enabledPlugins := make(map[string]bool)
	home, _ := os.UserHomeDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if data, err := os.ReadFile(settingsFile); err == nil {
		var settings map[string]interface{}
		if err := json.Unmarshal(data, &settings); err == nil {
			if plugins, ok := settings["enabledPlugins"].(map[string]interface{}); ok {
				for k, v := range plugins {
					if enabled, ok := v.(bool); ok && enabled {
						enabledPlugins[k] = true
					}
				}
			}
		}
	}

	// Mark skills as installed based on their type
	for i := range allSkills {
		skill := &allSkills[i]

		switch normalizeSkillTypeKind(skill.Type) {
		case skillTypeZip:
			// For zip skills, extract the skill directory name from the zip filename
			// The zip file should extract to a directory with the same base name
			zipName := filepath.Base(skill.Value)
			// Remove .zip extension
			dirName := strings.TrimSuffix(zipName, ".zip")
			dirName = strings.TrimSuffix(dirName, ".rar")

			// Check if this directory exists in installed dirs
			skill.Installed = installedMap[dirName]
		case skillTypeAddress:
			// For address skills, check enabledPlugins in settings.json
			skill.Installed = enabledPlugins[skill.Value]
			// Fallback: also check skill directories
			if !skill.Installed {
				parts := strings.Split(skill.Value, "@")
				if len(parts) > 0 {
					dirName := parts[0]
					skill.Installed = installedMap[dirName]
				}
			}
		}
	}

	return allSkills
}

func (a *App) validateSkillZip(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("invalid zip file: %v", err)
	}
	defer r.Close()
	if err := validateSkillZipResourceLimits(r.File); err != nil {
		return err
	}

	type zipSkillLayout struct {
		hasMarkdown   bool
		hasLegacyMD   bool
		hasLegacyMeta bool
	}

	layouts := make(map[string]*zipSkillLayout)
	rootFileCount := 0
	validRootMarkdown := false
	rootHasLegacyMD := false
	rootHasLegacyMeta := false
	for _, f := range r.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip contains unsupported symlink entry: %s", f.Name)
		}
		_, name, err := safeZipEntryTarget(os.TempDir(), f.Name)
		if err != nil {
			return err
		}
		parts := strings.Split(name, "/")
		if len(parts) > 0 && (strings.HasPrefix(parts[0], "__MACOSX") || strings.HasPrefix(parts[0], ".")) {
			continue
		}
		if len(parts) == 1 {
			if f.FileInfo().IsDir() {
				continue
			}
			rootFileCount++
			if isSkillMarkdownDocFileName(parts[0]) {
				validRootMarkdown = true
				if parts[0] == "SKILL.md" {
					rootHasLegacyMD = true
				}
			} else if parts[0] == "_meta.json" {
				rootHasLegacyMeta = true
			}
			continue
		}
		dir := parts[0]
		layout := layouts[dir]
		if layout == nil {
			layout = &zipSkillLayout{}
			layouts[dir] = layout
		}
		if len(parts) == 2 {
			if isSkillMarkdownDocFileName(parts[1]) {
				layout.hasMarkdown = true
				if parts[1] == "SKILL.md" {
					layout.hasLegacyMD = true
				}
			} else if parts[1] == "_meta.json" {
				layout.hasLegacyMeta = true
			}
		}
	}
	if validRootMarkdown && !rootHasLegacyMeta {
		return nil
	}
	if rootHasLegacyMD || rootHasLegacyMeta {
		return fmt.Errorf("legacy SKILL.md/_meta.json skill packages must be migrated to skill.yaml or skill.md")
	}
	if len(layouts) == 0 {
		if rootFileCount > 0 {
			return fmt.Errorf("skill package root must contain skill.md/SKILL.md or README.md in a common case variant")
		}
		return fmt.Errorf("skill package is empty or contains no valid directories")
	}
	for dir, layout := range layouts {
		if layout.hasLegacyMeta {
			return fmt.Errorf("directory '%s' uses legacy skill format (SKILL.md/_meta.json); please migrate to skill.yaml or skill.md", dir)
		}
		if !layout.hasMarkdown {
			return fmt.Errorf("directory '%s' is missing skill.md/SKILL.md or README.md in a common case variant", dir)
		}
	}
	return nil
}

const (
	maxSkillZipEntries            = 2048
	maxSkillZipFileBytes          = 64 << 20
	maxSkillZipTotalExpandedBytes = 256 << 20
)

func validateSkillZipResourceLimits(files []*zip.File) error {
	if len(files) > maxSkillZipEntries {
		return fmt.Errorf("zip contains too many entries: %d > %d", len(files), maxSkillZipEntries)
	}
	var total uint64
	for _, file := range files {
		if file == nil || file.FileInfo().IsDir() {
			continue
		}
		size := file.UncompressedSize64
		if size > maxSkillZipFileBytes {
			return fmt.Errorf("zip entry %q is too large after decompression: %d > %d bytes", file.Name, size, maxSkillZipFileBytes)
		}
		total += size
		if total > maxSkillZipTotalExpandedBytes {
			return fmt.Errorf("zip expands to too much data: %d > %d bytes", total, maxSkillZipTotalExpandedBytes)
		}
	}
	return nil
}

type skillZipInstallScanResult struct {
	HighestReport *skill.ScanReport
	ByName        map[string]*skill.ScanReport
	ByDirName     map[string]*skill.ScanReport
}

func (a *App) scanSkillZipBeforeInstall(path, displayName, description string) (*skill.ScanReport, error) {
	result, err := a.scanSkillZipBeforeInstallDetailed(path, displayName, description)
	if result == nil {
		return nil, err
	}
	return result.HighestReport, err
}

func (a *App) scanSkillZipBeforeInstallDetailed(path, displayName, description string) (*skillZipInstallScanResult, error) {
	tmpDir, err := os.MkdirTemp("", "maclaw-skill-scan-*")
	if err != nil {
		return nil, fmt.Errorf("create skill scan staging directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := a.unzip(path, tmpDir); err != nil {
		return nil, fmt.Errorf("extract skill for security scan: %w", err)
	}

	scanner := skill.NewSecurityScanner(nil)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	roots := candidateInstalledSkillDirs(tmpDir)
	if len(roots) == 0 {
		roots = []string{tmpDir}
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	a.emitSkillInstallProgress(name, "extract", "Extracted skill package for pre-install security scan.", nil)
	result := &skillZipInstallScanResult{
		ByName:    make(map[string]*skill.ScanReport),
		ByDirName: make(map[string]*skill.ScanReport),
	}
	skippedRiskScan := false
	for _, root := range roots {
		entry, err := loadImportedSkillEntry(root)
		if err != nil {
			entry = &corelib.NLSkillEntry{
				Name:        name,
				Description: description,
				SkillDir:    root,
				Source:      "zip",
				TrustLevel:  "community",
			}
		}
		dirName := filepath.Base(root)
		if strings.TrimSpace(entry.Name) == "" {
			entry.Name = name
		}
		if strings.TrimSpace(entry.Description) == "" {
			entry.Description = description
		}
		entry.SkillDir = root
		entry.Source = firstNonEmpty(entry.Source, "zip")
		entry.TrustLevel = firstNonEmpty(entry.TrustLevel, "community")
		if a.isRiskGuardrailOffMode() {
			skippedRiskScan = true
			if err := a.admitManualSkillInstall(ctx, entry, "manual zip", nil); err != nil {
				return result, err
			}
			continue
		}
		report := scanner.ScanInstallStaged(ctx, entry, root, func(status string) {
			if a != nil {
				a.log(status)
				a.emitSkillInstallProgress(entry.Name, "scanning", status, nil)
			}
		})
		result.HighestReport = higherSkillInstallScanReport(result.HighestReport, report)
		if report != nil {
			if key := strings.TrimSpace(entry.Name); key != "" {
				result.ByName[key] = report
			}
			if key := strings.TrimSpace(dirName); key != "" {
				result.ByDirName[key] = report
			}
		}
		if err := a.admitManualSkillInstall(ctx, entry, "manual zip", report); err != nil {
			return result, err
		}
	}
	if result.HighestReport == nil && !skippedRiskScan {
		entry := &corelib.NLSkillEntry{Name: name, Description: description, SkillDir: tmpDir, Source: "zip", TrustLevel: "community"}
		result.HighestReport = scanner.ScanInstallStaged(ctx, entry, tmpDir, func(status string) {
			if a != nil {
				a.log(status)
				a.emitSkillInstallProgress(name, "scanning", status, nil)
			}
		})
		if result.HighestReport != nil {
			result.ByName[name] = result.HighestReport
			result.ByDirName[filepath.Base(tmpDir)] = result.HighestReport
		}
		if err := a.admitManualSkillInstall(ctx, entry, "manual zip", result.HighestReport); err != nil {
			return result, err
		}
	}
	return result, nil
}

func higherSkillInstallScanReport(current, next *skill.ScanReport) *skill.ScanReport {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	if security.RiskLevelOrder[next.FinalLevel] > security.RiskLevelOrder[current.FinalLevel] {
		return next
	}
	return current
}

func getToolConfigDirName(tool string) string {
	return normalizeRemoteToolNameKind(tool).ConfigDirName()
}
func (a *App) AddSkill(name, description, skillType, value, toolName string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "install", "name": name, "type": skillType, "tool": toolName}); err != nil {
		return err
	}
	skillKind := normalizeSkillTypeKind(skillType)
	// Prevent address skills for Codex
	if normalizeRemoteToolNameKind(toolName).RequiresZipSkills() && skillKind.IsAddress() {
		return fmt.Errorf("codex only supports zip package skills")
	}
	// Validate zip if applicable
	if skillKind.IsZip() && strings.Contains(value, string(os.PathSeparator)) {
		if err := a.validateSkillZip(value); err != nil {
			return err
		}
	}
	skillsDir := a.GetSkillsDir(toolName)
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return err
	}
	metadataPath := filepath.Join(skillsDir, "metadata.json")
	// Load existing
	var skills []corelib.Skill
	if data, err := os.ReadFile(metadataPath); err == nil {
		json.Unmarshal(data, &skills)
	}
	// Check for duplicate name - update if exists
	found := false
	for i, s := range skills {
		if s.Name == name {
			finalValue := value
			if skillKind.IsZip() {
				// If value is a path (contains separator)", assume it's a new file to copy
				if strings.Contains(value, string(os.PathSeparator)) {
					srcFile, err := os.Open(value)
					if err != nil {
						return err
					}
					defer srcFile.Close()
					fileName := filepath.Base(value)
					destPath := filepath.Join(skillsDir, fileName)
					destFile, err := os.Create(destPath)
					if err != nil {
						return err
					}
					defer destFile.Close()
					_, err = io.Copy(destFile, srcFile)
					if err != nil {
						return err
					}
					finalValue = fileName
				}
			}
			skills[i] = corelib.Skill{
				Name:        name,
				Description: description,
				Type:        skillType,
				Value:       finalValue,
			}
			found = true
			break
		}
	}
	if !found {
		finalValue := value
		if skillKind.IsZip() {
			// Copy file
			srcFile, err := os.Open(value)
			if err != nil {
				return err
			}
			defer srcFile.Close()
			fileName := filepath.Base(value)
			destPath := filepath.Join(skillsDir, fileName)
			destFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer destFile.Close()
			_, err = io.Copy(destFile, srcFile)
			if err != nil {
				return err
			}
			finalValue = fileName
		}
		newSkill := corelib.Skill{
			Name:        name,
			Description: description,
			Type:        skillType,
			Value:       finalValue,
		}
		skills = append(skills, newSkill)
	}
	// Save
	data, err := json.MarshalIndent(skills, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return err
	}
	if a.skillExecutor != nil {
		a.skillExecutor.invalidateSkillCache()
	} else if a.cachedSkillScanner != nil {
		a.cachedSkillScanner.Invalidate()
	}
	return nil
}
func (a *App) InstallDefaultMarketplace() error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "install", "source": "default_marketplace"}); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %v", err)
	}

	// Read existing settings
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsFile); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Ensure extraKnownMarketplaces exists
	marketplaces, ok := settings["extraKnownMarketplaces"].(map[string]interface{})
	if !ok {
		marketplaces = make(map[string]interface{})
	}

	changed := false

	// Add anthropic-agent-skills marketplace (anthropics/skills repo)
	if _, exists := marketplaces["anthropic-agent-skills"]; !exists {
		marketplaces["anthropic-agent-skills"] = map[string]interface{}{
			"source": map[string]interface{}{
				"source": "github",
				"repo":   "anthropics/skills",
			},
		}
		changed = true
	}

	// Add superpowers-marketplace (obra/superpowers-marketplace repo)
	if _, exists := marketplaces["superpowers-marketplace"]; !exists {
		marketplaces["superpowers-marketplace"] = map[string]interface{}{
			"source": map[string]interface{}{
				"source": "github",
				"repo":   "obra/superpowers-marketplace",
			},
		}
		changed = true
	}

	if changed {
		settings["extraKnownMarketplaces"] = marketplaces
		data, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal settings: %v", err)
		}
		if err := os.WriteFile(settingsFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write settings: %v", err)
		}
		a.log("Default marketplaces added to ~/.claude/settings.json")
	}

	return nil
}
func (a *App) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := validateSkillZipResourceLimits(r.File); err != nil {
		return err
	}
	type zipEntry struct {
		file      *zip.File
		target    string
		cleanName string
	}
	entries := make([]zipEntry, 0, len(r.File))
	for _, f := range r.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip contains unsupported symlink entry: %s", f.Name)
		}
		target, cleanName, err := safeZipEntryTarget(dest, f.Name)
		if err != nil {
			return err
		}
		entries = append(entries, zipEntry{file: f, target: target, cleanName: cleanName})
	}
	// 1. Identify root directories to clean up
	rootDirs := make(map[string]bool)
	for _, entry := range entries {
		parts := strings.Split(entry.cleanName, "/")
		if len(parts) > 0 {
			rootDir := parts[0]
			if !strings.HasPrefix(rootDir, "__MACOSX") && !strings.HasPrefix(rootDir, ".") {
				rootDirs[rootDir] = true
			}
		}
	}
	// 2. Remove existing directories
	for dir := range rootDirs {
		destPath := filepath.Join(dest, dir)
		if err := os.RemoveAll(destPath); err != nil {
			return fmt.Errorf("failed to remove existing skill directory %s: %v", destPath, err)
		}
	}
	os.MkdirAll(dest, 0755)
	for _, entry := range entries {
		f := entry.file
		fpath := entry.target
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		written, err := io.Copy(outFile, io.LimitReader(rc, int64(maxSkillZipFileBytes)+1))
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
		if f.UncompressedSize64 > maxSkillZipFileBytes || written > maxSkillZipFileBytes {
			return fmt.Errorf("zip entry %q is too large after decompression", f.Name)
		}
	}
	return nil
}

func safeZipEntryTarget(dest, name string) (string, string, error) {
	name = strings.ToValidUTF8(name, "")
	slashName := strings.ReplaceAll(name, "\\", "/")
	if slashName == "" || strings.HasPrefix(slashName, "/") || zipEntryHasDrivePrefix(slashName) || zipEntryHasColonPathComponent(slashName) || filepath.IsAbs(name) || filepath.IsAbs(filepath.FromSlash(slashName)) || filepath.VolumeName(filepath.FromSlash(slashName)) != "" {
		return "", "", fmt.Errorf("illegal file path: %s", name)
	}
	cleanName := pathpkg.Clean(slashName)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", "", fmt.Errorf("illegal file path: %s", name)
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return "", "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(destAbs, filepath.FromSlash(cleanName)))
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(destAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("illegal file path: %s", name)
	}
	return targetAbs, cleanName, nil
}

func zipEntryHasDrivePrefix(name string) bool {
	return len(name) >= 2 && name[1] == ':' &&
		((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z'))
}

func zipEntryHasColonPathComponent(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.Contains(part, ":") {
			return true
		}
	}
	return false
}

func (a *App) InstallSkill(name, description, skillType, value, location, projectPath, toolName string) error {
	locationKind := normalizeSkillInstallLocationKind(location)
	projectPath = normalizeProjectSessionPath(projectPath)
	if locationKind.IsProject() && projectPath == "" {
		projectPath = normalizeProjectSessionPath(a.GetCurrentProjectPath())
	}
	if locationKind.IsProject() && projectPath == "" {
		return fmt.Errorf("project path required")
	}
	policyOwnerID := a.defaultManualPolicyOwnerID()
	if locationKind.IsProject() {
		policyOwnerID = projectSessionOwnerID(projectPath)
	}
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(policyOwnerID, "manage_skill", map[string]interface{}{"action": "install", "name": name, "type": skillType, "location": location, "project_path": projectPath, "tool": toolName}); err != nil {
		return err
	}
	skillKind := normalizeSkillTypeKind(skillType)
	// 1. Validate
	if locationKind.IsProject() && skillKind.IsAddress() {
		return fmt.Errorf("project installation only supports zip/rar files")
	}
	// For zip validation, we need to know if value is a path or filename
	var fullPath string
	var installScanReport *skill.ScanReport
	var installScanReports *skillZipInstallScanResult
	var zipSnapshotPath string
	if skillKind.IsZip() {
		if strings.Contains(value, string(os.PathSeparator)) {
			fullPath = value
		} else {
			fullPath = filepath.Join(a.GetSkillsDir(toolName), value)
		}
		snapshot, cleanup, err := snapshotSkillZipForInstall(fullPath)
		if err != nil {
			return err
		}
		defer cleanup()
		fullPath = snapshot
		zipSnapshotPath = snapshot
		if err := a.validateSkillZip(fullPath); err != nil {
			return err
		}
		report, err := a.scanSkillZipBeforeInstallDetailed(fullPath, name, description)
		if err != nil {
			return err
		}
		installScanReports = report
		if report != nil {
			installScanReport = report.HighestReport
		}
	}
	configDirName := getToolConfigDirName(toolName)
	var installedSkillDirs []string
	// 2. Install to Tool
	if locationKind.IsUser() {
		if skillKind.IsAddress() {
			// Skill ID installation
			if !normalizeRemoteToolNameKind(toolName).IsClaude() {
				return fmt.Errorf("skill ID installation is currently only supported for Claude")
			}
			// Ensure default marketplaces are registered
			if err := a.InstallDefaultMarketplace(); err != nil {
				a.log(fmt.Sprintf("Warning: failed to ensure marketplaces: %v", err))
			}
			// Enable plugin in ~/.claude/settings.json
			home, _ := os.UserHomeDir()
			settingsFile := filepath.Join(home, ".claude", "settings.json")
			var settings map[string]interface{}
			if data, err := os.ReadFile(settingsFile); err == nil {
				if err := json.Unmarshal(data, &settings); err != nil {
					settings = make(map[string]interface{})
				}
			} else {
				settings = make(map[string]interface{})
			}
			enabledPlugins, ok := settings["enabledPlugins"].(map[string]interface{})
			if !ok {
				enabledPlugins = make(map[string]interface{})
			}
			enabledPlugins[value] = true
			settings["enabledPlugins"] = enabledPlugins
			data, err := json.MarshalIndent(settings, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal settings: %v", err)
			}
			if err := os.WriteFile(settingsFile, data, 0644); err != nil {
				return fmt.Errorf("failed to write settings: %v", err)
			}
			a.log(fmt.Sprintf("Plugin %s enabled in settings.json", value))
		} else {
			// Unzip to ~/.<tool>/skills
			home, _ := os.UserHomeDir()
			destDir := filepath.Join(home, configDirName, "skills")
			a.emitSkillInstallProgress(name, "installing", "Installing approved skill package.", installScanReport)
			if err := a.unzip(fullPath, destDir); err != nil {
				return fmt.Errorf("unzip failed: %v", err)
			}
			installedSkillDirs = append(installedSkillDirs, candidateInstalledSkillDirs(destDir)...)
			if err := writeSkillScanCacheForInstalledZip(name, description, destDir, installScanReport, installScanReports); err != nil {
				cleanupImportedSkillDirs(installedSkillDirs)
				return fmt.Errorf("write skill scan cache: %w", err)
			}
		}
	} else if locationKind.IsProject() {
		destDir := filepath.Join(projectPath, configDirName, "skills")
		a.emitSkillInstallProgress(name, "installing", "Installing approved skill package.", installScanReport)
		if err := a.unzip(fullPath, destDir); err != nil {
			return fmt.Errorf("unzip failed: %v", err)
		}
		installedSkillDirs = append(installedSkillDirs, candidateInstalledSkillDirs(destDir)...)
		if err := writeSkillScanCacheForInstalledZip(name, description, destDir, installScanReport, installScanReports); err != nil {
			cleanupImportedSkillDirs(installedSkillDirs)
			return fmt.Errorf("write skill scan cache: %w", err)
		}
	}
	// 3. Add to App List
	addSkillValue := value
	if zipSnapshotPath != "" {
		addSkillValue = zipSnapshotPath
	}
	if err := a.AddSkill(name, description, skillType, addSkillValue, toolName); err != nil {
		cleanupImportedSkillDirs(installedSkillDirs)
		return err
	}
	a.emitSkillInstallProgress(name, "done", "Skill installed successfully.", installScanReport)
	return nil
}

func snapshotSkillZipForInstall(src string) (string, func(), error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return "", func() {}, fmt.Errorf("open skill zip: %w", err)
	}
	defer srcFile.Close()

	tmp, err := os.CreateTemp("", "maclaw-skill-install-*.zip")
	if err != nil {
		return "", func() {}, fmt.Errorf("create skill zip snapshot: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := io.Copy(tmp, srcFile); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("copy skill zip snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close skill zip snapshot: %w", err)
	}
	return tmpPath, cleanup, nil
}

func (a *App) DeleteSkill(name, toolName string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "uninstall", "name": name, "tool": toolName}); err != nil {
		return err
	}
	// Prevent deletion of the hardcoded skill
	if name == "Claude Official Documentation Skill Package" {
		return fmt.Errorf("cannot delete system skill package")
	}
	skillsDir := a.GetSkillsDir(toolName)
	metadataPath := filepath.Join(skillsDir, "metadata.json")
	var skills []corelib.Skill
	if data, err := os.ReadFile(metadataPath); err == nil {
		json.Unmarshal(data, &skills)
	}
	var newSkills []corelib.Skill
	for _, s := range skills {
		if s.Name == name {
			if normalizeSkillTypeKind(s.Type).IsZip() {
				os.Remove(filepath.Join(skillsDir, s.Value))
			}
		} else {
			newSkills = append(newSkills, s)
		}
	}
	data, err := json.MarshalIndent(newSkills, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0644)
}

// Translation logic
var translations = map[string]map[string]string{
	"Show AI assistant button": {
		"zh-Hans": "\u663e\u793a AI \u52a9\u624b\u6309\u94ae",
		"zh-Hant": "\u986f\u793a AI \u52a9\u624b\u6309\u9215",
	},
	"Service Redeem": {
		"zh-Hans": "\u670d\u52a1\u5151\u6362",
		"zh-Hant": "\u670d\u52d9\u5151\u63db",
	},
	"Security": {
		"zh-Hans": "\u5b89\u5168",
		"zh-Hant": "\u5b89\u5168",
	},
	"New version %s is available. Open About to download the update.": {
		"zh-Hans": "\u53d1\u73b0\u65b0\u7248\u672c %s\uff0c\u53ef\u524d\u5f80\u5173\u4e8e\u9875\u9762\u4e0b\u8f7d\u66f4\u65b0\u3002",
		"zh-Hant": "\u767c\u73fe\u65b0\u7248\u672c %s\uff0c\u53ef\u524d\u5f80\u95dc\u65bc\u9801\u9762\u4e0b\u8f09\u66f4\u65b0\u3002",
	},
}

func (a *App) tr(key string, args ...interface{}) string {
	lang := normalizeAppLanguageKind(a.CurrentLanguage).TranslationTag()
	var format string
	if dict, ok := translations[key]; ok {
		if val, ok := dict[lang]; ok {
			format = val
		}
	}
	if format == "" {
		format = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(format, args...)
	}
	return format
}
func (a *App) OpenSystemUrl(url string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		a.log("Opening URL on macOS: " + url)
		cmd = exec.Command("open", url)
	case "windows":
		a.log("Opening URL on Windows: " + url)
		// Escape & to ^& for cmd.exe
		escapedUrl := strings.ReplaceAll(url, "&", "^&")
		cmdExe := os.Getenv("ComSpec")
		if cmdExe == "" {
			if sysroot := os.Getenv("SystemRoot"); sysroot != "" {
				cmdExe = filepath.Join(sysroot, "System32", "cmd.exe")
			} else {
				cmdExe = `C:\Windows\System32\cmd.exe`
			}
		}
		cmd = exec.Command(cmdExe, "/c", "start", "", escapedUrl)
		hideCommandWindow(cmd)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

// PingSkillHub tests whether a SkillHUB URL is reachable.
// Returns a JSON-friendly struct with online status and latency.
func (a *App) PingSkillHub(url string) map[string]interface{} {
	result := map[string]interface{}{
		"url":    url,
		"online": false,
		"ms":     0,
		"error":  "",
	}
	if strings.TrimSpace(url) == "" {
		result["error"] = "empty URL"
		return result
	}
	target := strings.TrimRight(strings.TrimSpace(url), "/")
	start := time.Now()
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()
	result["ms"] = elapsed
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	defer resp.Body.Close()
	// Any response (2xx, 3xx, 4xx) means the server is reachable
	if resp.StatusCode < 500 {
		result["online"] = true
	} else {
		result["error"] = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

// ValidateSkillHub probes a SkillHub/ClawHub URL and reports the detected API flavor.
// Return shape: {"type": "standard"|"clawhub"|"clawhub_mirror"|"unsupported", "reason": "..."}.
func (a *App) ValidateSkillHub(rawURL string) map[string]interface{} {
	result := map[string]interface{}{
		"type":   "unsupported",
		"reason": "",
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		result["reason"] = "URL \u4e0d\u80fd\u4e3a\u7a7a"
		return result
	}

	base := strings.TrimRight(rawURL, "/")
	client := &http.Client{Timeout: 8 * time.Second}

	// Probe 1: ClawSkillHub / skillhub.space list API: /api/skills?search=test&limit=1
	if probeSkillHubSpace(client, base) {
		result["type"] = "skillhub_space"
		result["reason"] = "Connected to ClawSkillHub API (skillhub.space format)"
		return result
	}

	// Probe 2: standard Hub search API: /api/v1/skills/search?q=test
	if hubType := probeStandardHub(client, base); hubType {
		result["type"] = "standard"
		result["reason"] = "Connected to standard SkillHub API"
		return result
	}

	// Probe 3: ClawHub mirror stats API: /api/stats
	if hubType := probeClawHubMirror(client, base); hubType {
		result["type"] = "clawhub_mirror"
		result["reason"] = "Connected to ClawHub mirror API (topclawhubskills.com format)"
		return result
	}

	// Probe 4: ClawHub item list API: /api/v1/skills
	if hubType := probeClawHub(client, base); hubType {
		result["type"] = "clawhub"
		result["reason"] = "Connected to ClawHub API (clawhub.ai format)"
		return result
	}

	// Fallback: if the base URL itself is reachable, report it as a generic mirror.
	if resp, err := client.Get(base); err == nil {
		resp.Body.Close()
		if resp.StatusCode < 400 {
			result["type"] = "mirror"
			result["reason"] = "Site is reachable, but it was not identified as a standard SkillHub API"
			return result
		}
	}

	result["reason"] = "Not identified as a usable SkillHub or ClawHub API"
	return result
}

// probeSkillHubSpace checks the skillhub.space/clawskillhub.com API flavor.
// GET /api/skills?search=test&limit=1 returns a JSON array of skill summaries.
func probeSkillHubSpace(client *http.Client, base string) bool {
	endpoint := base + "/api/skills?search=test&limit=1"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// Expected response shape: [{"id":..., "slug":..., "owner":...}, ...].
	var items []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return false
	}
	return true
}

// probeStandardHub checks the standard Hub skill search API.
func probeStandardHub(client *http.Client, base string) bool {
	endpoint := base + "/api/v1/skills/search?q=test"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// Expected response shape: an object containing a "skills" field.
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	_, hasSkills := body["skills"]
	return hasSkills
}

// probeClawHubMirror checks the topclawhubskills.com mirror API.
func probeClawHubMirror(client *http.Client, base string) bool {
	endpoint := base + "/api/stats"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	// Expected response shape: {"ok": true, "total_skills": ...}.
	if ok, _ := body["ok"].(bool); ok {
		if _, has := body["total_skills"]; has {
			return true
		}
	}
	return false
}

// probeClawHub checks the clawhub.ai list API.
func probeClawHub(client *http.Client, base string) bool {
	endpoint := base + "/api/v1/skills"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	// Expected response shape: {"items": [...], "nextCursor": ...}.
	_, hasItems := body["items"]
	return hasItems
}
