package main

import (
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
