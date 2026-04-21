package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// initTUIWorkflowEngine initializes the workflow engine for TUI.
func (a *TUIApp) initTUIWorkflowEngine() {
	dataDir := commands.ResolveDataDir()
	dbPath := filepath.Join(dataDir, "workflow.db")

	store, err := workflow.NewSQLiteStore(dbPath)
	if err != nil {
		log.Printf("[TUIWorkflow] SQLite store failed: %v, using in-memory mode", err)
		a.initTUIWorkflowEngineWithStore(workflow.NullStore{})
		return
	}
	a.initTUIWorkflowEngineWithStore(store)
	log.Printf("[TUIWorkflow] initialized with SQLite store at %s", dbPath)
}

func (a *TUIApp) initTUIWorkflowEngineWithStore(store workflow.PersistenceStore) {
	registry := workflow.NewWorkflowRegistry()
	llmCaller := &tuiWorkflowLLMCaller{}
	understanding := workflow.NewIntentUnderstandingManager(store, llmCaller, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, store, nil)

	adapter := NewTUIWorkflowAdapter(nil, engine)
	engine.SetCallbacks(adapter)

	if err := engine.RestoreFromStore(); err != nil {
		log.Printf("[TUIWorkflow] restore from store: %v", err)
	}

	a.workflowEngine = engine

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			engine.CleanupExpired()
			understanding.CleanupExpired()
		}
	}()
}

// tuiWorkflowLLMCaller adapts TUI's LLM infrastructure to workflow.LLMCaller.
type tuiWorkflowLLMCaller struct{}

func (c *tuiWorkflowLLMCaller) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	cfg, err := commands.LoadLLMConfig()
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := agent.DoSimpleLLMRequest(cfg, messages, client, timeout)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// initTUISteeringStore initializes the steering file store for TUI.
// User-level: ~/.maclaw/steering/
// Project-level: <cwd>/.maclaw/steering/ (if exists)
func (a *TUIApp) initTUISteeringStore() {
	dataDir := commands.ResolveDataDir()
	userDir := filepath.Join(dataDir, "steering")

	// Ensure default steering files exist.
	if err := steering.EnsureDefaults(userDir); err != nil {
		log.Printf("[steering] TUI EnsureDefaults: %v", err)
	}

	// Project-level: check current working directory.
	projectDir := ""
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, ".maclaw", "steering")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			projectDir = candidate
		}
	}

	a.steeringStore = steering.NewStore(userDir, projectDir)
	if err := a.steeringStore.Load(); err != nil {
		log.Printf("[steering] TUI initial load: %v", err)
	}

	log.Printf("[steering] TUI initialized (user=%s, project=%s, files=%d)",
		userDir, projectDir, a.steeringStore.FileCount())
}
