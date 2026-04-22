package main

// im_handler_standalone.go provides a standalone constructor for IMMessageHandler
// that does not depend on *App. This enables TUI (and future non-GUI hosts) to
// use the same agent loop as the desktop and IM channels.
//
// See docs/agent-unification-design.md Phase 1.

import (
	"net"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/nudge"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// StandaloneConfig holds the components needed to construct an IMMessageHandler
// without a *App. All fields are optional — nil fields disable the corresponding
// functionality gracefully (the accessor methods in im_app_accessors.go return
// nil/zero when both the direct field and h.app are nil).
type StandaloneConfig struct {
	// WorkflowEngine is the corelib workflow engine (19 templates, phase management).
	WorkflowEngine *workflow.WorkflowEngine

	// UnifiedClassifier is the three-layer intent classifier (UIC).
	UnifiedClassifier *intent.UnifiedIntentClassifier

	// SteeringStore provides declarative rule injection from ~/.maclaw/steering/.
	SteeringStore *steering.Store

	// MemoryStore is the long-term memory store (BM25 + vector recall).
	MemoryStore *memory.Store

	// ToolRouter handles dynamic tool selection via BM25/embedding scoring.
	ToolRouter *tool.Router

	// UsageTracker records tool usage for outcome learning.
	UsageTracker *tool.UsageTracker

	// SSHManager manages SSH sessions (connect/exec/background tasks).
	SSHManager *remote.SSHSessionManager

	// LLMConfigFunc returns the current LLM configuration.
	// Required — without this the agent cannot make LLM calls.
	LLMConfigFunc func() MaclawLLMConfig

	// MaxIterationsFunc returns the max agent loop iterations.
	// Defaults to 30 if nil.
	MaxIterationsFunc func() int

	// IsProMode controls whether coding session tools are available.
	// Defaults to true if nil.
	IsProMode *bool

	// ResponseHeaderTimeout for the HTTP client. Defaults to 120s.
	ResponseHeaderTimeout time.Duration

	// ConversationStorePath is the file path for persisting conversation history.
	// Empty string uses in-memory only.
	ConversationStorePath string

	// ConfirmationStorePath is the file path for persisting confirmation state.
	// Empty string uses in-memory only.
	ConfirmationStorePath string
}

// NewIMMessageHandlerStandalone creates an IMMessageHandler without a *App.
// The handler uses the provided components directly instead of going through
// App's lazy-init infrastructure.
//
// This is the TUI entry point. The returned handler supports all agent
// functionality that the provided components enable. Missing components
// (nil fields) gracefully disable the corresponding features.
func NewIMMessageHandlerStandalone(cfg StandaloneConfig) *IMMessageHandler {
	timeout := cfg.ResponseHeaderTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	chatTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
	}

	chatClient := &http.Client{Transport: chatTransport}

	// Conversation memory: persistent if path provided, in-memory otherwise.
	var mem *conversationMemory
	if cfg.ConversationStorePath != "" {
		mem = newPersistentConversationMemory(cfg.ConversationStorePath)
	} else {
		mem = newConversationMemory()
	}

	// Confirmation store: persistent if path provided, in-memory otherwise.
	var confirmStore *aiConfirmationStore
	if cfg.ConfirmationStorePath != "" {
		confirmStore = newAIConfirmationStore(cfg.ConfirmationStorePath)
	} else {
		confirmStore = newAIConfirmationStore("")
	}

	h := &IMMessageHandler{
		// app is nil — all access goes through direct fields + accessors.
		app:               nil,
		manager:           nil, // TUI doesn't use RemoteSessionManager
		memory:            mem,
		confirmationStore: confirmStore,
		client:            chatClient,
		taskClient:        chatClient, // TUI shares one pool
		agentActivity:     NewAgentActivityStore(),
		workflowEngine:    cfg.WorkflowEngine,
		unifiedClassifier: cfg.UnifiedClassifier,
		steeringStore:     cfg.SteeringStore,
	}

	// Wire the tool router if provided.
	if cfg.ToolRouter != nil {
		h.toolRouter = &ToolRouter{inner: cfg.ToolRouter}
	}
	if cfg.UsageTracker != nil {
		h.usageTracker = cfg.UsageTracker
	}
	if cfg.SSHManager != nil {
		h.sshMgr = cfg.SSHManager
	}
	if cfg.MemoryStore != nil {
		h.memoryStore = cfg.MemoryStore
	}

	// Store standalone config for accessor methods.
	h.standaloneConfig = &cfg

	// Initialize ToolRegistry and register builtin tools.
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	// Non-code tools (Git, browser) are skipped — they need *App.
	// TUI can register additional tools after construction if needed.
	h.toolBuilder = NewDynamicToolBuilder(h.registry)

	// Initialize topic switch detector.
	h.topicDetector = newTopicSwitchDetector(func() (*http.Client, MaclawLLMConfig) {
		return h.client, h.getMaclawLLMConfig()
	})

	// Initialize task execution orchestrator.
	h.taskOrchestrator = NewTaskExecutionOrchestrator()

	// Initialize nudge tracker.
	h.nudgeTracker = nudge.NewNudgeTracker()

	return h
}
