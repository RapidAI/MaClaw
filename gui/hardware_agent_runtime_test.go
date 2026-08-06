package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestHardwareAgentRuntimeRegistryIsolatesPerClientState(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	registry := newHardwareAgentRuntimeRegistry(app, nil, nil)

	alpha, err := registry.handler("pet-alpha")
	if err != nil {
		t.Fatalf("create alpha runtime: %v", err)
	}
	alphaAgain, err := registry.handler("PET-alpha")
	if err != nil {
		t.Fatalf("look up alpha runtime: %v", err)
	}
	beta, err := registry.handler("pet-beta")
	if err != nil {
		t.Fatalf("create beta runtime: %v", err)
	}

	if alpha != alphaAgain {
		t.Fatal("the same hardware client must retain its runtime")
	}
	if alpha == beta {
		t.Fatal("different hardware clients must not share an agent handler")
	}
	if alpha.memory == beta.memory || alpha.memoryStore == beta.memoryStore || alpha.confirmationStore == beta.confirmationStore {
		t.Fatal("hardware clients must not share conversation, long-term memory, or confirmations")
	}
	if alpha.client == beta.client || alpha.taskClient == beta.taskClient {
		t.Fatal("hardware clients must not share HTTP clients")
	}
	if alpha.client.Transport == beta.client.Transport || alpha.taskClient.Transport == beta.taskClient.Transport {
		t.Fatal("hardware clients must not share HTTP transports")
	}
	if alpha.unifiedClassifier == nil || beta.unifiedClassifier == nil || alpha.unifiedClassifier == beta.unifiedClassifier {
		t.Fatal("hardware clients must not share intent-classifier cache state")
	}
	if got := registry.count(); got != 2 {
		t.Fatalf("runtime count = %d, want 2", got)
	}
	for _, clientID := range []string{"pet-alpha", "pet-beta"} {
		if _, err := os.Stat(filepath.Dir(hardwareConversationMemoryPath(app, clientID))); err != nil {
			t.Fatalf("hardware data directory for %s: %v", clientID, err)
		}
	}

	registry.remove("pet-alpha")
	if got := registry.count(); got != 1 {
		t.Fatalf("runtime count after remove = %d, want 1", got)
	}
	betaAgain, err := registry.handler("pet-beta")
	if err != nil || betaAgain != beta {
		t.Fatalf("remaining runtime was affected by another device removal: handler=%p err=%v", betaAgain, err)
	}
	registry.stopAll()
}

func TestHardwareToolDefinitionGeneratorCloneIsolatesDeferredState(t *testing.T) {
	source := NewToolDefinitionGenerator(nil, []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "hardware_private_tool",
				"description": "original description",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"mode": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	})
	source.SetDeferredTools([]string{"hardware_private_tool"})

	alpha := cloneHardwareToolDefinitionGenerator(source)
	beta := cloneHardwareToolDefinitionGenerator(source)
	if alpha == nil || beta == nil || alpha == beta || alpha == source || beta == source {
		t.Fatal("each hardware runtime must receive a distinct tool definition generator")
	}
	if !alpha.ActivateDeferredTool("hardware_private_tool") {
		t.Fatal("alpha did not activate its deferred tool")
	}
	if beta.IsDeferredToolActivated("hardware_private_tool") || source.IsDeferredToolActivated("hardware_private_tool") {
		t.Fatal("a deferred-tool activation leaked outside its hardware runtime")
	}

	source.builtinDefs[0]["function"].(map[string]interface{})["description"] = "mutated source description"
	if got := alpha.builtinDefs[0]["function"].(map[string]interface{})["description"]; got != "original description" {
		t.Fatalf("hardware generator shared builtin definition state: got %q", got)
	}
}

func TestHardwareAgentRuntimeRegistryCreatesDifferentClientsConcurrently(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var configured sync.WaitGroup
	configured.Add(2)
	registry := newHardwareAgentRuntimeRegistry(app, nil, func(h *IMMessageHandler) {
		started <- struct{}{}
		configured.Done()
		<-release
	})
	defer registry.stopAll()

	errs := make(chan error, 2)
	go func() { _, err := registry.handler("pet-alpha"); errs <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("alpha runtime did not begin initialization")
	}
	go func() { _, err := registry.handler("pet-beta"); errs <- err }()
	select {
	case <-started:
		// Beta reached its configurator while alpha remains blocked, proving its
		// creation did not wait on alpha under the registry mutex.
	case <-time.After(time.Second):
		t.Fatal("beta runtime was blocked by alpha initialization")
	}
	close(release)
	configured.Wait()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("create hardware runtime: %v", err)
		}
	}
}

func TestHardwareAgentRuntimeRegistryStopWaitsForInitializingRuntimeCleanup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	started := make(chan struct{})
	release := make(chan struct{})
	registry := newHardwareAgentRuntimeRegistry(app, nil, func(*IMMessageHandler) {
		close(started)
		<-release
	})

	created := make(chan error, 1)
	go func() { _, err := registry.handler("pet-alpha"); created <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runtime did not enter initialization")
	}

	stopped := make(chan struct{})
	go func() {
		registry.stopAll()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("stopAll returned before the initializing runtime was cleaned up")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stopAll did not finish after initialization released")
	}
	if err := <-created; err == nil {
		t.Fatal("an initialization that races with stopAll must not publish a runtime")
	}
	if got := registry.count(); got != 0 {
		t.Fatalf("stopped registry retained %d runtime(s)", got)
	}
}

func TestHardwareAgentRuntimeRegistryConcurrentStopsShareTeardownBoundary(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	started := make(chan struct{})
	release := make(chan struct{})
	registry := newHardwareAgentRuntimeRegistry(app, nil, func(*IMMessageHandler) {
		close(started)
		<-release
	})

	created := make(chan error, 1)
	go func() { _, err := registry.handler("pet-alpha"); created <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runtime did not enter initialization")
	}

	firstStopped := make(chan struct{})
	secondStopped := make(chan struct{})
	go func() { registry.stopAll(); close(firstStopped) }()
	time.Sleep(10 * time.Millisecond)
	go func() { registry.stopAll(); close(secondStopped) }()
	select {
	case <-secondStopped:
		t.Fatal("second stopAll returned before the shared teardown completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	for label, done := range map[string]chan struct{}{"first": firstStopped, "second": secondStopped} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s stopAll did not finish", label)
		}
	}
	if err := <-created; err == nil {
		t.Fatal("initialization should fail after concurrent stopAll")
	}
}

func TestThirdPartyLocalRuntimeDispatchesToolsToOwningDevice(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	manager := newThirdPartyGatewayManager(app)
	// This test exercises the transport binding only. Avoid the full App
	// configurator because its shared desktop services intentionally own their
	// own lifecycle and are outside this isolated runtime's resource scope.
	manager.localHandlers = newHardwareAgentRuntimeRegistry(app, nil, nil)
	handler, err := manager.ensureLocalHandler("pet-alpha")
	if err != nil {
		t.Fatalf("create local hardware runtime: %v", err)
	}
	defer manager.resetLocalHandler()

	err = handler.clientToolDispatcher(context.Background(), agent.ClientToolContext{
		ClientID: "pet-alpha", ConversationID: "main", ReplyToMessageID: "message-1",
	}, agent.ClientToolDefinition{Name: "alarm_list", InputSchema: map[string]any{"type": "object"}}, "call-1", map[string]any{})
	if err != nil {
		t.Fatalf("dispatch client tool: %v", err)
	}
	alphaMessages, _, _ := manager.messagesAfter("pet-alpha", 0, 10)
	if len(alphaMessages) != 1 || alphaMessages[0].Type != "tool_call" || alphaMessages[0].ToolCall == nil || alphaMessages[0].ToolCall.Name != "alarm_list" {
		t.Fatalf("alpha queue = %#v", alphaMessages)
	}
	betaMessages, _, _ := manager.messagesAfter("pet-beta", 0, 10)
	if len(betaMessages) != 0 {
		t.Fatalf("tool call leaked to another device: %#v", betaMessages)
	}
}

func TestHardwareRuntimeStopCancelsOnlyItsSessions(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	registry := newHardwareAgentRuntimeRegistry(app, nil, nil)
	alpha, err := registry.handler("pet-alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := registry.handler("pet-beta")
	if err != nil {
		t.Fatal(err)
	}
	defer registry.stopAll()

	alphaLoop := NewLoopContext("alpha task", 1, nil)
	betaLoop := NewLoopContext("beta task", 1, nil)
	alpha.setSessionLoopCtx("thirdparty:pet-alpha:default", alphaLoop)
	beta.setSessionLoopCtx("thirdparty:pet-beta:default", betaLoop)
	alpha.taskOrchestratorRegistry.GetOrCreate("thirdparty:pet-alpha:default")
	beta.taskOrchestratorRegistry.GetOrCreate("thirdparty:pet-beta:default")
	alpha.agentActivity.Update(&AgentActivity{Source: "im", OwnerID: "thirdparty:pet-alpha:default", Task: "alpha task"})
	beta.agentActivity.Update(&AgentActivity{Source: "im", OwnerID: "thirdparty:pet-beta:default", Task: "beta task"})
	alpha.workflowV2Adapters.Store("thirdparty:pet-alpha:default", &GUIWorkflowAdapter{})
	beta.workflowV2Adapters.Store("thirdparty:pet-beta:default", &GUIWorkflowAdapter{})
	alpha.frozenMemorySnapshots.Store("thirdparty:pet-alpha:default", "alpha memory")
	beta.frozenMemorySnapshots.Store("thirdparty:pet-beta:default", "beta memory")

	registry.remove("pet-alpha")
	if !alphaLoop.IsCancelled() {
		t.Fatal("removed hardware runtime did not cancel its active loop")
	}
	if betaLoop.IsCancelled() {
		t.Fatal("removing one hardware runtime cancelled another device loop")
	}
	if alpha.taskOrchestratorRegistry.Get("thirdparty:pet-alpha:default") != nil {
		t.Fatal("removed hardware runtime retained its workflow state")
	}
	if beta.taskOrchestratorRegistry.Get("thirdparty:pet-beta:default") == nil {
		t.Fatal("removing one hardware runtime cleared another device workflow state")
	}
	if _, ok := alpha.workflowV2Adapters.Load("thirdparty:pet-alpha:default"); ok {
		t.Fatal("removed hardware runtime retained its V2 workflow adapter")
	}
	if _, ok := beta.workflowV2Adapters.Load("thirdparty:pet-beta:default"); !ok {
		t.Fatal("removing one hardware runtime cleared another device V2 workflow adapter")
	}
	if _, ok := alpha.frozenMemorySnapshots.Load("thirdparty:pet-alpha:default"); ok {
		t.Fatal("removed hardware runtime retained its frozen memory snapshot")
	}
	if _, ok := beta.frozenMemorySnapshots.Load("thirdparty:pet-beta:default"); !ok {
		t.Fatal("removing one hardware runtime cleared another device frozen memory snapshot")
	}
	alpha.agentActivity.mu.Lock()
	alphaActivityCount := len(alpha.agentActivity.items)
	alpha.agentActivity.mu.Unlock()
	if alphaActivityCount != 0 {
		t.Fatal("removed hardware runtime retained its activity state")
	}
	beta.agentActivity.mu.Lock()
	betaActivityCount := len(beta.agentActivity.items)
	beta.agentActivity.mu.Unlock()
	if betaActivityCount != 1 {
		t.Fatal("removing one hardware runtime cleared another device activity state")
	}
}

func TestHardwareAgentRuntimeOwnsPrivateMemoryPaths(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	registry := newHardwareAgentRuntimeRegistry(app, nil, nil)
	handler, err := registry.handler("pet-a")
	if err != nil {
		t.Fatal(err)
	}
	defer registry.stopAll()

	wantPrefix := filepath.Join(app.GetDataDir(), "hardware_agents", safeFileToken("pet-a"))
	if got := handler.memoryStore.Path(); filepath.Dir(filepath.Dir(got)) != wantPrefix {
		t.Fatalf("memory path = %q, want it under %q", got, wantPrefix)
	}
	if handler.client == nil || handler.taskClient == nil {
		t.Fatal("private HTTP clients were not initialized")
	}
	if _, ok := handler.client.Transport.(*http.Transport); !ok {
		t.Fatalf("chat transport type = %T, want *http.Transport", handler.client.Transport)
	}
}
