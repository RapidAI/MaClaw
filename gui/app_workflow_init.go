package main

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// initWorkflowEngine initializes the workflow engine with all dependencies.
// Called during app startup.
func (a *App) initWorkflowEngine() {
	dbPath := filepath.Join(a.getMaclawBaseDir(), "workflow.db")

	// 1. Create SQLiteStore (fallback to NullStore on failure).
	store, err := workflow.NewSQLiteStore(dbPath)
	if err != nil {
		log.Printf("[WorkflowEngine] failed to create SQLite store at %s: %v, using in-memory mode", dbPath, err)
		a.initWorkflowEngineWithStore(workflow.NullStore{})
		return
	}
	a.initWorkflowEngineWithStore(store)
	log.Printf("[WorkflowEngine] initialized with SQLite store at %s", dbPath)
}

// initWorkflowEngineWithStore creates the engine with the given persistence store.
func (a *App) initWorkflowEngineWithStore(store workflow.PersistenceStore) {
	// 2. Create registry (auto-registers built-in templates).
	registry := workflow.NewWorkflowRegistry()

	// 3. Create LLM caller adapter.
	llmCaller := &workflowLLMCaller{
		app:    a,
		client: &http.Client{Timeout: 35 * time.Second}, // slightly above max LLM timeout to let context cancel first
	}

	// 4. Create IntentUnderstandingManager.
	understanding := workflow.NewIntentUnderstandingManager(store, llmCaller, registry)

	// 5. Create WorkflowEngine (callbacks=nil initially, set below).
	engine := workflow.NewWorkflowEngine(registry, understanding, store, nil)

	// 6. Create GUIWorkflowAdapter and set as callbacks.
	adapter := NewGUIWorkflowAdapter(a, engine)
	engine.SetCallbacks(adapter)

	// 7. Restore active workflows from store.
	if err := engine.RestoreFromStore(); err != nil {
		log.Printf("[WorkflowEngine] restore from store: %v", err)
	}

	// 8. Store engine reference.
	a.workflowEngine = engine

	// 8.1 Wire artifact saver: persist phase outputs to long-term memory
	// so they survive conversation history truncation.
	// memoryStore may not be initialized yet (lazy init), so we use a
	// deferred adapter that calls ensureMemoryStore on first use.
	a.workflowArtifactSaver = &deferredArtifactSaver{app: a}
	engine.SetArtifactSaver(a.workflowArtifactSaver)

	// 9. Start periodic cleanup goroutine.
	go workflowCleanupLoop(engine, understanding)
}

// workflowCleanupLoop runs CleanupExpired on both the engine and understanding
// manager every hour. It runs until the process exits.
func workflowCleanupLoop(engine *workflow.WorkflowEngine, understanding *workflow.IntentUnderstandingManager) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if err := engine.CleanupExpired(); err != nil {
			log.Printf("[WorkflowEngine] cleanup expired: %v", err)
		}
		if understanding != nil {
			understanding.CleanupExpired()
		}
	}
}

// workflowLLMCaller adapts the App's LLM infrastructure to the
// workflow.LLMCaller interface used by IntentUnderstandingManager.
type workflowLLMCaller struct {
	app    *App
	client *http.Client
}

func (c *workflowLLMCaller) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	cfg := c.app.GetMaclawLLMConfig()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := doSimpleLLMRequest(ctx, cfg, messages, c.client, timeout)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}
