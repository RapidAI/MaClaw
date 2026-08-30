package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

type appEmbeddingTestEmbedder struct {
	closed atomic.Bool
}

func (e *appEmbeddingTestEmbedder) Embed(string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (e *appEmbeddingTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (e *appEmbeddingTestEmbedder) Dim() int { return 3 }

func (e *appEmbeddingTestEmbedder) Close() { e.closed.Store(true) }

func TestFullEmbeddingActivationReusesIntentEmbedder(t *testing.T) {
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{VectorSearchEnabled: true},
	}

	intentEmb := &appEmbeddingTestEmbedder{}
	app.activateIntentClassifierEmbedderAsync(intentEmb)
	if app.intentEmbedder != intentEmb {
		t.Fatalf("intent embedder was not retained")
	}
	if !app.intentEmbeddingActive.Load() {
		t.Fatalf("intent embedding should be active")
	}

	fullEmb := &appEmbeddingTestEmbedder{}
	app.activateEmbedderAsync(fullEmb)
	if app.intentEmbedder != intentEmb {
		t.Fatalf("full activation should reuse intent embedder")
	}
	if !fullEmb.closed.Load() {
		t.Fatalf("redundant full embedder should be closed")
	}
	if !app.embeddingActivated.Load() {
		t.Fatalf("full embedding activation flag should be set")
	}
}

func TestSharedEmbeddingEmbedderSerializesConcurrentLoads(t *testing.T) {
	app := &App{}
	var loads atomic.Int32
	const callers = 8

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan embedding.Embedder, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			emb, err := app.sharedEmbeddingEmbedder("model.gguf", func(string) (embedding.Embedder, error) {
				loads.Add(1)
				return &appEmbeddingTestEmbedder{}, nil
			})
			if err != nil {
				t.Errorf("sharedEmbeddingEmbedder returned error: %v", err)
				return
			}
			results <- emb
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	if loads.Load() != 1 {
		t.Fatalf("expected one embedder load, got %d", loads.Load())
	}
	var first embedding.Embedder
	for emb := range results {
		if first == nil {
			first = emb
			continue
		}
		if !sameEmbedder(first, emb) {
			t.Fatalf("expected all callers to receive the same embedder")
		}
	}
}

func TestSharedEmbeddingEmbedderReloadsWhenPathChanges(t *testing.T) {
	app := &App{}
	first, err := app.sharedEmbeddingEmbedder("first.gguf", func(string) (embedding.Embedder, error) {
		return &appEmbeddingTestEmbedder{}, nil
	})
	if err != nil {
		t.Fatalf("first load failed: %v", err)
	}
	firstEmb := first.(*appEmbeddingTestEmbedder)

	again, err := app.sharedEmbeddingEmbedder("first.gguf", func(string) (embedding.Embedder, error) {
		t.Fatalf("same path should reuse existing embedder")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("same path load failed: %v", err)
	}
	if again != first {
		t.Fatalf("same path should reuse existing embedder")
	}

	second, err := app.sharedEmbeddingEmbedder("second.gguf", func(string) (embedding.Embedder, error) {
		return &appEmbeddingTestEmbedder{}, nil
	})
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	if second == first {
		t.Fatalf("different path should load a new embedder")
	}
	if !firstEmb.closed.Load() {
		t.Fatalf("old embedder should be closed after path change")
	}
	if app.intentEmbedderPath != "second.gguf" {
		t.Fatalf("intentEmbedderPath = %q, want second.gguf", app.intentEmbedderPath)
	}
}

func TestIntentActivationDoesNotCloseSharedEmbedder(t *testing.T) {
	app := &App{}
	emb := &appEmbeddingTestEmbedder{}
	app.intentEmbedder = emb

	app.activateIntentClassifierEmbedderAsync(emb)

	if emb.closed.Load() {
		t.Fatalf("shared embedder should not be closed during intent activation")
	}
	if app.intentEmbedder != emb {
		t.Fatalf("shared embedder should remain cached")
	}
	if !app.intentEmbeddingActive.Load() {
		t.Fatalf("intent embedding should be active")
	}
}

func TestBuildUICLLMContextFuncSendsStrictStructuredIntentContract(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"top\":[{\"skill\":\"live_data\",\"score\":0.95,\"workflow_type\":\"\"}]}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "test", Name: "Test", URL: server.URL, Key: "test-key", Model: "test-model",
		}},
		MaclawLLMCurrentProvider: "Test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	response, err := app.buildUICLLMContextFunc()(context.Background(), "classify", "北京天气，输出格式化PDF报告")
	if err != nil {
		t.Fatalf("classifier callback: %v", err)
	}
	if response == "" {
		t.Fatal("classifier callback returned empty response")
	}
	format, _ := body["response_format"].(map[string]interface{})
	if format["type"] != "json_schema" {
		t.Fatalf("response_format=%#v, want json_schema", body["response_format"])
	}
	schema, _ := format["json_schema"].(map[string]interface{})
	root, _ := schema["schema"].(map[string]interface{})
	properties, _ := root["properties"].(map[string]interface{})
	top, _ := properties["top"].(map[string]interface{})
	if top["minItems"] != float64(1) || top["maxItems"] != float64(3) {
		t.Fatalf("top schema=%#v, want 1..3 candidates", top)
	}
	assertIntentTreeSkillEnum(t, top)
}

func TestBuildUICLLMContextFuncPreservesStructuredIntentContractForConservativeResponses(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"top\":[{\"skill\":\"live_data\",\"score\":0.95,\"workflow_type\":\"\"}]}"}]}]}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "test", Name: "Qwen", URL: server.URL, Key: "test-key", Model: "qwen-coder", Protocol: "openai", WireAPI: "responses",
		}},
		MaclawLLMCurrentProvider: "Qwen",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	response, err := app.buildUICLLMContextFunc()(context.Background(), "classify", "北京天气，输出格式化PDF报告")
	if err != nil || response == "" {
		t.Fatalf("classifier callback response=%q err=%v", response, err)
	}
	text, _ := body["text"].(map[string]interface{})
	format, _ := text["format"].(map[string]interface{})
	if format["type"] != "json_schema" || format["name"] != "intent_tree_candidates" || format["strict"] != true {
		t.Fatalf("Responses text.format=%#v, want preserved strict intent schema", format)
	}
	schema, _ := format["schema"].(map[string]interface{})
	properties, _ := schema["properties"].(map[string]interface{})
	top, _ := properties["top"].(map[string]interface{})
	assertIntentTreeSkillEnum(t, top)
}

func assertIntentTreeSkillEnum(t *testing.T, top map[string]interface{}) {
	t.Helper()
	items, _ := top["items"].(map[string]interface{})
	alternatives, _ := items["anyOf"].([]interface{})
	if len(alternatives) == 0 {
		t.Fatalf("items=%#v, want per-label schema alternatives", items)
	}
	values := make(map[string]bool, len(alternatives))
	for _, alternative := range alternatives {
		branch, ok := alternative.(map[string]interface{})
		if !ok {
			t.Fatalf("schema alternative=%#v, want object", alternative)
		}
		properties, _ := branch["properties"].(map[string]interface{})
		skill, _ := properties["skill"].(map[string]interface{})
		enum, _ := skill["enum"].([]interface{})
		if len(enum) != 1 {
			t.Fatalf("skill schema=%#v, want exactly one taxonomy label per alternative", skill)
		}
		label, ok := enum[0].(string)
		if !ok {
			t.Fatalf("skill enum=%#v, want strings", enum)
		}
		values[label] = true
	}
	for _, label := range []string{"live_data", "document_generate"} {
		if !values[label] {
			t.Fatalf("skill alternatives=%#v, missing %q", alternatives, label)
		}
	}
	if values["get_current_weather"] {
		t.Fatalf("skill alternatives=%#v, must not accept provider tool names", alternatives)
	}
}

func TestBuildUICLLMContextFuncOwnDeadlineDoesNotTripEndpointGate(t *testing.T) {
	// 2026-08-25 production chain: one tree call killed by our own 5s fusion
	// deadline was recorded as endpoint network failure, so the endpoint gate
	// skipped every classification for the next 30s and ambiguous turns kept
	// degrading to unknown. Our own deadline must not be endpoint evidence.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Outlive the caller's 100ms deadline, but do not rely on request-context
		// propagation to unblock: Server.Close would otherwise wait forever.
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "test", Name: "Test", URL: server.URL, Key: "test-key", Model: "test-model",
		}},
		MaclawLLMCurrentProvider: "Test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := app.buildUICLLMContextFunc()(ctx, "classify", "杭州天气，生成pdf报告"); err == nil {
		t.Fatal("want deadline error from the hanging server")
	}
	cfg := app.GetMaclawLLMConfig()
	if _, skip := app.shouldSkipLightweightLLM(cfg); skip {
		t.Fatal("own fusion deadline must not trip the endpoint failure gate")
	}

	// The gate itself must still record genuine network failures (the exact
	// dial error text is platform-dependent, so feed a canonical one directly).
	app.observeLLMEndpointResult(cfg, fmt.Errorf("dial tcp 127.0.0.1:1: connectex: connection refused"))
	if _, skip := app.shouldSkipLightweightLLM(cfg); !skip {
		t.Fatal("genuine network failure must trip the endpoint failure gate")
	}
}

func TestFullActivationTreatsCachedButInactiveEmbedderAsNotReused(t *testing.T) {
	app := &App{}
	emb := &appEmbeddingTestEmbedder{}
	app.intentEmbedder = emb

	claimed, reusedIntent := app.claimEmbeddingForFullActivation(emb)

	if claimed != emb {
		t.Fatalf("expected cached embedder to be claimed")
	}
	if reusedIntent {
		t.Fatalf("cached but inactive embedder should still be wired during full activation")
	}
	if emb.closed.Load() {
		t.Fatalf("cached embedder should not be closed")
	}
}

func TestEmbeddingActivationWiresDesktopAndHubIMInterruptHandlers(t *testing.T) {
	app := &App{}
	desktopHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	hubHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	app.imHandler = desktopHandler
	app.remoteSessions = &RemoteSessionManager{hubClient: &RemoteHubClient{imHandler: hubHandler}}
	emb := &appEmbeddingTestEmbedder{}

	app.wireEmbedderToIMHandlers(emb, true)

	if desktopHandler.interruptHandler.currentEmbedder() != emb {
		t.Fatal("desktop IM interrupt handler did not receive the active embedder")
	}
	if hubHandler.interruptHandler.currentEmbedder() != emb {
		t.Fatal("Hub IM interrupt handler did not receive the active embedder")
	}
}

func TestEmbeddingActivationWiresLocalGatewayInterruptHandlers(t *testing.T) {
	app := &App{}
	weixinHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	telegramHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	qqHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	lansengerHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	thirdPartyHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	app.weixinGateway = &weixinGatewayManager{localHandler: weixinHandler}
	app.telegramGateway = &telegramGatewayManager{localHandler: telegramHandler}
	app.qqBotGateway = &qqBotGatewayManager{localHandler: qqHandler}
	app.lansengerGateway = &lansengerGatewayManager{localHandler: lansengerHandler}
	app.thirdPartyGateway = &thirdPartyGatewayManager{localHandler: thirdPartyHandler}
	emb := &appEmbeddingTestEmbedder{}

	app.wireEmbedderToIMHandlers(emb, false)

	for name, handler := range map[string]*IMMessageHandler{
		"weixin": weixinHandler, "telegram": telegramHandler, "qq": qqHandler,
		"lansenger": lansengerHandler, "third-party": thirdPartyHandler,
	} {
		if handler.interruptHandler.currentEmbedder() != emb {
			t.Fatalf("%s interrupt handler did not receive the active embedder", name)
		}
	}
}

func TestHubClientReusesExistingDesktopIMHandler(t *testing.T) {
	app := &App{}
	desktopHandler := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	app.imHandler = desktopHandler
	hub := &RemoteHubClient{app: app}

	if got := hub.ensureIMHandler(); got != desktopHandler {
		t.Fatal("Hub client should reuse the existing desktop IM handler")
	}
	if app.imHandler != desktopHandler {
		t.Fatal("Hub initialization must not replace the desktop IM handler")
	}
}

func TestNewIMMessageHandlerDoesNotReplaceActiveAppHandler(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	active := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	app.imHandler = active

	created := NewIMMessageHandler(app, nil)
	if created == active {
		t.Fatal("test requires a distinct temporary handler")
	}
	if app.imHandler != active {
		t.Fatal("temporary handler construction must not replace an active app handler")
	}
}

func TestResetLocalGatewayHandlerKeepsSharedConversationMemoryUsable(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	shared := app.ensureConversationMemory()
	manager := &weixinGatewayManager{app: app, localHandler: &IMMessageHandler{memory: shared}}

	manager.resetLocalHandler()
	if manager.currentLocalHandler() != nil {
		t.Fatal("local handler should be detached on reset")
	}
	if got := app.ensureConversationMemory(); got != shared {
		t.Fatal("gateway reset must retain the shared app conversation memory")
	}
}

func TestConcurrentHandlerDependenciesShareSingleAppInstances(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	const workers = 16
	memories := make(chan *agent.ConversationMemory, workers)
	confirmations := make(chan *aiConfirmationStore, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			memories <- app.ensureConversationMemory()
			confirmations <- app.ensureAIConfirmationStore()
		}()
	}
	wg.Wait()
	close(memories)
	close(confirmations)
	for memory := range memories {
		if memory != app.aiConversationMemory {
			t.Fatal("concurrent initialization created more than one conversation memory")
		}
	}
	for confirmation := range confirmations {
		if confirmation != app.aiConfirmationStore {
			t.Fatal("concurrent initialization created more than one confirmation store")
		}
	}
}

func TestInterruptRecoversTaskEmbeddingLoadedAfterTaskStart(t *testing.T) {
	const userID = "im:embedding-recovery"
	handler := &IMMessageHandler{}
	handler.setSessionLoopCtx(userID, NewLoopContext("active-task", 3, nil))
	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "检查现有项目的测试覆盖率", "unknown", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)
	interrupt.SetEmbedder(&appEmbeddingTestEmbedder{})

	_ = interrupt.TryInterrupt(userID, "补充：也检查失败用例")
	if got := tracker.Buffer().TaskEmbed(); len(got) == 0 {
		t.Fatal("interrupt should cache the task embedding when it becomes available after task start")
	}
}
