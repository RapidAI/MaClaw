package main

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestResolveExperienceProviderForAttributionDoesNotUseLastIMUserID(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	registry := workflow.NewWorkflowRegistry()
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, nil, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, workflow.NullStore{}, &mockEngineCallbacksGUI{})
	remoteOwnerID := "project-tab:remote-workflow"
	if _, err := engine.StartWorkflow(remoteOwnerID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "remote build"}); err != nil {
		t.Fatalf("StartWorkflow remote failed: %v", err)
	}

	app := &App{memoryStore: store, workflowEngine: engine, imHandler: &IMMessageHandler{}}
	app.imHandler.globalLoopMu.Lock()
	app.imHandler.lastUserID = remoteOwnerID
	app.imHandler.globalLoopMu.Unlock()

	provider := app.resolveExperienceProviderForAttribution()
	composite, ok := provider.(lifecycle.CompositeProvider)
	if !ok {
		t.Fatalf("provider = %T, want lifecycle.CompositeProvider", provider)
	}
	if got := len(composite.Providers); got != 1 {
		t.Fatalf("provider count = %d, want only memory provider; last IM workflow must not be attributed globally", got)
	}
}
