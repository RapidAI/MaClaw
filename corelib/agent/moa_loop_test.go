package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// moaMockCallbacks embeds mockCallbacks and implements MoAHost.
type moaMockCallbacks struct {
	*mockCallbacks
	preset     moa.ResolvedPreset
	progresses []string
	usageCalls int
	breakdowns []LoopInputBreakdown
	receipts   []ToolSurfaceReceipt
	events     []ToolSurfaceEvent
}

func (m *moaMockCallbacks) PrepareMoA(iteration int, toolsSeen bool, fanoutsRan int) (bool, moa.ResolvedPreset, string) {
	_ = iteration
	_ = toolsSeen
	_ = fanoutsRan
	return true, m.preset, "consulting test models…"
}

func (m *moaMockCallbacks) OnProgress(text string) {
	m.progresses = append(m.progresses, text)
}

func (m *moaMockCallbacks) OnLLMUsage(model string, in, out int) {
	_ = model
	_ = in
	_ = out
	m.usageCalls++
}

func (m *moaMockCallbacks) OnLoopInputBreakdown(breakdown LoopInputBreakdown) {
	m.breakdowns = append(m.breakdowns, breakdown)
}

func (m *moaMockCallbacks) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	m.receipts = append(m.receipts, receipt)
}

func (m *moaMockCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func TestRunLoop_MoAEnvKillSwitchDisablesHost(t *testing.T) {
	t.Setenv("MACLAW_MOA", "off")
	var refHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if !strings.Contains(string(body), `"tools"`) {
			refHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "single model only"}},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 3},
		})
	}))
	defer srv.Close()
	primary := corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "m", Protocol: "openai", WireAPI: "chat"}
	cb := &moaMockCallbacks{
		mockCallbacks: &mockCallbacks{
			config: primary, maxIter: 1, sysPrompt: "t",
			tools: []map[string]interface{}{
				{"type": "function", "function": map[string]interface{}{"name": "echo", "parameters": map[string]interface{}{"type": "object"}}},
			},
		},
		preset: moa.ResolvedPreset{
			Name: "review", Enabled: true, FanoutMaxIterations: 1,
			AggregatorUsePrimary: true, Aggregator: primary,
			References: []moa.ResolvedRef{{Label: "a", Config: primary}},
			Raw:        corelib.MoAPresetConfig{Enabled: true},
		},
	}
	result := RunLoop(cb, "hi", nil, srv.Client())
	if result.Error != "" {
		t.Fatalf("%s", result.Error)
	}
	if refHits.Load() != 0 {
		t.Fatalf("env=off must skip references, hits=%d", refHits.Load())
	}
	if result.Route.MoAPreset != "" {
		t.Fatalf("route should not mark moa under kill switch: %+v", result.Route)
	}
}

func TestRunLoop_MoAFanOutInjectsAdviceBeforeAggregator(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on") // ensure not polluted by other tests
	var refHits, aggHits atomic.Int32
	// Fake OpenAI-compatible server: reference has an explicit empty replacement;
	// aggregator has its rendered tools schema in body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		s := string(body)
		hasExplicitEmptyTools := strings.Contains(s, `"tools":[]`)
		hasAdvice := strings.Contains(s, "Private multi-model council advice")
		w.Header().Set("Content-Type", "application/json")
		if hasExplicitEmptyTools {
			refHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "ref advice: check risks"}},
				},
				"usage": map[string]interface{}{"prompt_tokens": 11, "completion_tokens": 7},
			})
			return
		}
		aggHits.Add(1)
		if !hasAdvice {
			// Aggregator must see injected advice on the request-only clone.
			t.Errorf("aggregator request missing private advice; body_len=%d", len(s))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "final answer from aggregator"}},
			},
			"usage": map[string]interface{}{"prompt_tokens": 40, "completion_tokens": 12},
		})
	}))
	defer srv.Close()

	primary := corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "k", Model: "agg-model", Protocol: "openai", WireAPI: "chat",
	}
	refCFG := corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "k", Model: "ref-model", Protocol: "openai", WireAPI: "chat",
	}
	cb := &moaMockCallbacks{
		mockCallbacks: &mockCallbacks{
			config:    primary,
			maxIter:   2,
			sysPrompt: "you are test",
			tools: []map[string]interface{}{
				{"type": "function", "function": map[string]interface{}{"name": "echo", "parameters": map[string]interface{}{"type": "object"}}},
			},
			toolResult: "ok",
		},
		preset: moa.ResolvedPreset{
			Name:                 "review",
			Enabled:              true,
			FanoutMaxIterations:  1,
			OnlyBeforeFirstTool:  true,
			ReferenceTimeoutSec:  10,
			AggregatorUsePrimary: true,
			Aggregator:           primary,
			References: []moa.ResolvedRef{
				{Label: "ref-a", Config: refCFG},
			},
			Raw: corelib.MoAPresetConfig{Enabled: true},
		},
	}

	client := srv.Client()
	result := RunLoop(cb, "review this plan", nil, client)
	if result.Error != "" {
		t.Fatalf("loop error: %s", result.Error)
	}
	if !strings.Contains(result.Text, "final answer") {
		t.Fatalf("text=%q", result.Text)
	}
	if refHits.Load() < 1 {
		t.Fatalf("expected reference call, hits=%d", refHits.Load())
	}
	if aggHits.Load() < 1 {
		t.Fatalf("expected aggregator call, hits=%d", aggHits.Load())
	}
	if result.Route.MoAPreset != "review" {
		t.Fatalf("expected MoAPreset=review, route=%+v", result.Route)
	}
	if result.Route.MoAReferences < 1 || result.Route.MoARefOK < 1 {
		t.Fatalf("expected ref counts on route: refs=%d ok=%d", result.Route.MoAReferences, result.Route.MoARefOK)
	}
	if result.Route.MoAFanouts < 1 || !result.Route.MoAFanOut {
		t.Fatalf("expected MoAFanOut wave recorded: fanouts=%d fanOut=%v", result.Route.MoAFanouts, result.Route.MoAFanOut)
	}
	meta := FormatTurnMetaOpts(TurnMetaOptions{Route: result.Route, Usage: result.Usage})
	if !strings.Contains(meta, "moa=review") {
		t.Fatalf("turn meta missing moa tag: %q", meta)
	}
	if result.Usage.Requests < 2 {
		t.Fatalf("expected >=2 usage requests (ref+agg), got %d", result.Usage.Requests)
	}
	if len(cb.progresses) == 0 {
		t.Fatal("expected progress callback during fan-out")
	}
	if len(cb.receipts) < 2 {
		t.Fatalf("expected receipts for reference and aggregator, got %#v", cb.receipts)
	}
	var sawEmpty, sawToolBearing bool
	for _, receipt := range cb.receipts {
		if !receipt.Verified {
			t.Fatalf("unverified MoA receipt: %#v", receipt)
		}
		if receipt.WireToolCount == 0 {
			sawEmpty = true
		} else {
			sawToolBearing = true
		}
	}
	if !sawEmpty || !sawToolBearing {
		t.Fatalf("receipts must cover explicit-empty reference and tool-bearing aggregator: %#v", cb.receipts)
	}
	if len(cb.breakdowns) != 1 {
		t.Fatalf("breakdowns=%#v, want one aggregator request", cb.breakdowns)
	}
	base := EstimateLoopInputBreakdown([]interface{}{
		map[string]string{"role": "system", "content": "you are test"},
		map[string]interface{}{"role": "user", "content": "review this plan"},
	}, cb.tools)
	if cb.breakdowns[0].HistoryTokens <= base.HistoryTokens {
		t.Fatalf("aggregator history tokens=%d, want injected MoA advice above base=%d", cb.breakdowns[0].HistoryTokens, base.HistoryTokens)
	}
	// Durable history user content must not include private advice.
	for _, e := range result.HistoryDelta {
		if e.Role == "user" {
			if s, ok := e.Content.(string); ok && strings.Contains(s, "Private multi-model") {
				t.Fatal("advice leaked into HistoryDelta user content")
			}
		}
	}
	// Stats should record the fan-out wave.
	st := moa.LoadStats()
	if st.Fanouts < 1 {
		t.Fatalf("expected moa stats fanouts>=1, got %+v", st)
	}
}

// TestRunLoop_MoAMultiRefSoak runs 3 parallel advisors + aggregator and asserts
// partial failure still produces an aggregator answer (K8).
func TestRunLoop_MoAMultiRefSoak(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on")
	var refHits, aggHits, failHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		s := string(body)
		hasTools := strings.Contains(s, `"tools"`) && strings.Contains(s, "echo")
		w.Header().Set("Content-Type", "application/json")
		if !hasTools {
			// Fail one advisor (empty content path via 500 on model label in body).
			if strings.Contains(s, "ref-fail") {
				failHits.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"ref down"}`))
				return
			}
			refHits.Add(1)
			label := "ok"
			if strings.Contains(s, "ref-b") {
				label = "b"
			} else if strings.Contains(s, "ref-c") {
				label = "c"
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "advice-" + label}},
				},
				"usage": map[string]interface{}{"prompt_tokens": 8, "completion_tokens": 4},
			})
			return
		}
		aggHits.Add(1)
		if !strings.Contains(s, "Private multi-model council advice") {
			t.Errorf("aggregator missing advice block")
		}
		// At least one successful ref should appear in advice.
		if !strings.Contains(s, "advice-") {
			t.Errorf("aggregator missing any ref content")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "soak final"}},
			},
			"usage": map[string]interface{}{"prompt_tokens": 50, "completion_tokens": 10},
		})
	}))
	defer srv.Close()

	primary := corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "k", Model: "agg", Protocol: "openai", WireAPI: "chat",
	}
	mkRef := func(model string) corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{
			URL: srv.URL, Key: "k", Model: model, Protocol: "openai", WireAPI: "chat",
		}
	}
	cb := &moaMockCallbacks{
		mockCallbacks: &mockCallbacks{
			config:    primary,
			maxIter:   2,
			sysPrompt: "soak",
			tools: []map[string]interface{}{
				{"type": "function", "function": map[string]interface{}{"name": "echo", "parameters": map[string]interface{}{"type": "object"}}},
			},
			toolResult: "ok",
		},
		preset: moa.ResolvedPreset{
			Name:                 "soak",
			Enabled:              true,
			FanoutMaxIterations:  1,
			OnlyBeforeFirstTool:  true,
			ReferenceTimeoutSec:  10,
			AggregatorUsePrimary: true,
			Aggregator:           primary,
			References: []moa.ResolvedRef{
				{Label: "a", Config: mkRef("ref-a")},
				{Label: "b", Config: mkRef("ref-b")},
				{Label: "fail", Config: mkRef("ref-fail")},
			},
			Raw: corelib.MoAPresetConfig{Enabled: true},
		},
	}

	result := RunLoop(cb, "multi-ref soak question", nil, srv.Client())
	if result.Error != "" {
		t.Fatalf("loop error: %s", result.Error)
	}
	if !strings.Contains(result.Text, "soak final") {
		t.Fatalf("text=%q", result.Text)
	}
	if refHits.Load() < 2 {
		t.Fatalf("expected >=2 successful refs, got ok=%d fail=%d", refHits.Load(), failHits.Load())
	}
	if failHits.Load() < 1 {
		t.Fatalf("expected 1 failed ref, got %d", failHits.Load())
	}
	if aggHits.Load() < 1 {
		t.Fatal("expected aggregator call")
	}
	if result.Route.MoAReferences != 3 {
		t.Fatalf("MoAReferences=%d want 3", result.Route.MoAReferences)
	}
	if result.Route.MoARefOK < 2 || result.Route.MoARefFailed < 1 {
		t.Fatalf("ok=%d fail=%d", result.Route.MoARefOK, result.Route.MoARefFailed)
	}
	meta := FormatTurnMetaOpts(TurnMetaOptions{Route: result.Route, Usage: result.Usage})
	if !strings.Contains(meta, "moa=soak") || !strings.Contains(meta, "/") {
		t.Fatalf("chip: %q", meta)
	}
	// Usage: 2 ok refs + 1 agg (failed ref may not report usage).
	if result.Usage.Requests < 3 {
		t.Fatalf("usage requests=%d want >=3", result.Usage.Requests)
	}
}

func TestRunLoop_MoAParallelReferenceReceiptsAreSerialized(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on")
	var receiptActive, receiptMaxActive atomic.Int32
	var eventActive, eventMaxActive atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var request struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "primary" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{{"message": map[string]interface{}{"role": "assistant", "content": "advice"}}},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]interface{}{"role": "assistant", "content": "final"}}},
		})
	}))
	defer srv.Close()

	primary := corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "primary", Protocol: "openai", WireAPI: "chat"}
	cb := &concurrencyCheckingMoACallbacks{moaMockCallbacks: &moaMockCallbacks{
		mockCallbacks: &mockCallbacks{config: primary, maxIter: 1, sysPrompt: "sys"},
		preset: moa.ResolvedPreset{
			Name: "parallel-receipts", Enabled: true, FanoutMaxIterations: 1,
			References: []moa.ResolvedRef{
				{Label: "a", Config: corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "a", Protocol: "openai", WireAPI: "chat"}},
				{Label: "b", Config: corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "b", Protocol: "openai", WireAPI: "chat"}},
				{Label: "c", Config: corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "c", Protocol: "openai", WireAPI: "chat"}},
			},
		},
	}}
	cb.onReceipt = func() {
		current := receiptActive.Add(1)
		for {
			seen := receiptMaxActive.Load()
			if current <= seen || receiptMaxActive.CompareAndSwap(seen, current) {
				break
			}
		}
		defer receiptActive.Add(-1)
		// Keep the observer occupied long enough for independently completed
		// advisor RoundTrips to overlap if the fan-out fails to serialize it.
		for i := 0; i < 1000; i++ {
			runtime.Gosched()
		}
	}
	cb.onEvent = func() {
		current := eventActive.Add(1)
		for {
			seen := eventMaxActive.Load()
			if current <= seen || eventMaxActive.CompareAndSwap(seen, current) {
				break
			}
		}
		defer eventActive.Add(-1)
		// Manifest and receipt lifecycle events originate in independently
		// completed advisor requests, so a non-serialized observer would race.
		for i := 0; i < 1000; i++ {
			runtime.Gosched()
		}
	}

	result := RunLoop(cb, "review", nil, srv.Client())
	if result.Error != "" || result.Text != "final" {
		t.Fatalf("result=%+v", result)
	}
	if receiptMaxActive.Load() != 1 {
		t.Fatalf("receipt observer concurrency=%d, want 1", receiptMaxActive.Load())
	}
	if len(cb.receipts) < 4 {
		t.Fatalf("receipts=%#v, want three advisors plus aggregator", cb.receipts)
	}
	if eventMaxActive.Load() != 1 {
		t.Fatalf("event observer concurrency=%d, want 1", eventMaxActive.Load())
	}
	manifestEvents, verifiedEvents, terminalEvents := 0, 0, 0
	for _, event := range cb.events {
		switch event.Kind {
		case ToolSurfaceEventManifestCreated:
			manifestEvents++
		case ToolSurfaceEventPayloadVerified:
			verifiedEvents++
		case ToolSurfaceEventTerminalReason:
			terminalEvents++
			if event.PayloadDigest == "" || event.AuditDigest == "" || event.ReplacementMode != "replace" || event.ExpectedToolCount != 0 {
				t.Fatalf("terminal event lacks the advisor/aggregator surface summary: %+v", event)
			}
		}
	}
	if manifestEvents < 4 || verifiedEvents < 4 {
		t.Fatalf("lifecycle events=%#v, want manifest and verified events for three advisors plus aggregator", cb.events)
	}
	if terminalEvents != 4 {
		t.Fatalf("terminal events=%#v, want one per three advisors plus aggregator", cb.events)
	}
}

func TestMoAReferenceToolSurfaceDisposition(t *testing.T) {
	tests := []struct {
		name string
		resp *llm.Response
		err  error
		want ToolSurfaceDisposition
	}{
		{name: "transport error", err: io.ErrUnexpectedEOF, want: ToolSurfaceTransportFailure},
		{name: "nil response", err: requireLLMDispatchResponse(nil), want: ToolSurfaceIntegrityFailure},
		{name: "empty choices", resp: &llm.Response{}, want: ToolSurfaceResponseAbandoned},
		{name: "empty message", resp: &llm.Response{Choices: []llm.Choice{{}}}, want: ToolSurfaceResponseAbandoned},
		{name: "reasoning accepted", resp: &llm.Response{Choices: []llm.Choice{{Message: llm.Message{ReasoningContent: "analysis"}}}}, want: ToolSurfaceResponseSettled},
		{name: "content accepted", resp: &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: "advice"}}}}, want: ToolSurfaceResponseSettled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := moaReferenceToolSurfaceDisposition(test.resp, test.err); got != test.want {
				t.Fatalf("disposition=%q, want %q", got, test.want)
			}
		})
	}
}

func TestRunLoop_MoAReferenceEmptyResponseEmitsAbandonedTerminal(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if request.Model == "advisor" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]interface{}{"role": "assistant", "content": "final"}}},
		})
	}))
	defer srv.Close()

	primary := corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "primary", Protocol: "openai", WireAPI: "chat"}
	cb := &moaMockCallbacks{
		mockCallbacks: &mockCallbacks{config: primary, maxIter: 1, sysPrompt: "sys"},
		preset: moa.ResolvedPreset{
			Name: "empty-reference", Enabled: true, FanoutMaxIterations: 1,
			References: []moa.ResolvedRef{{Label: "advisor", Config: corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "advisor", Protocol: "openai", WireAPI: "chat"}}},
		},
	}

	result := RunLoop(cb, "review", nil, srv.Client())
	if result.Error != "" || result.Text != "final" {
		t.Fatalf("result=%+v", result)
	}
	var terminals []ToolSurfaceDisposition
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			if event.PayloadDigest == "" || event.AuditDigest == "" || event.ReplacementMode != "replace" || event.ExpectedToolCount != 0 {
				t.Fatalf("terminal event lacks static surface summary: %+v", event)
			}
			terminals = append(terminals, event.TerminalReason)
		}
	}
	if want := []ToolSurfaceDisposition{ToolSurfaceResponseAbandoned, ToolSurfaceResponseSettled}; !slices.Equal(terminals, want) {
		t.Fatalf("terminals=%v, want %v", terminals, want)
	}
}

func TestRunLoop_MoAAnthropicReferenceUsesExplicitEmptySurfaceReceipt(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on")
	var refHits, aggHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		model, _ := body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		if model == "advisor" {
			tools, present := body["tools"]
			if !present {
				t.Fatalf("Anthropic advisor omitted explicit empty tools replacement: %#v", body)
			}
			list, ok := tools.([]interface{})
			if !ok || len(list) != 0 {
				t.Fatalf("Anthropic advisor tools=%#v, want []", tools)
			}
			refHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "advisor-response", "type": "message", "role": "assistant", "content": []map[string]interface{}{{"type": "text", "text": "advice"}},
			})
			return
		}
		aggHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "primary-response", "type": "message", "role": "assistant", "content": []map[string]interface{}{{"type": "text", "text": "final"}},
		})
	}))
	defer srv.Close()

	primary := corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "primary", Protocol: "anthropic"}
	cb := &moaMockCallbacks{
		mockCallbacks: &mockCallbacks{config: primary, maxIter: 1, sysPrompt: "sys"},
		preset: moa.ResolvedPreset{
			Name: "anthropic-reference", Enabled: true, FanoutMaxIterations: 1,
			References: []moa.ResolvedRef{{Label: "advisor", Config: corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "advisor", Protocol: "anthropic"}}},
		},
	}

	result := RunLoop(cb, "review", nil, srv.Client())
	if result.Error != "" || result.Text != "final" {
		t.Fatalf("result=%+v", result)
	}
	if refHits.Load() != 1 || aggHits.Load() != 1 {
		t.Fatalf("reference/aggregator hits=%d/%d", refHits.Load(), aggHits.Load())
	}
	var sawExplicitEmpty bool
	for _, receipt := range cb.receipts {
		if receipt.Verified && receipt.WireToolCount == 0 && receipt.ReplacementMode == "replace" {
			sawExplicitEmpty = true
		}
	}
	if !sawExplicitEmpty {
		t.Fatalf("missing verified Anthropic explicit-empty receipt: %#v", cb.receipts)
	}
}

type concurrencyCheckingMoACallbacks struct {
	*moaMockCallbacks
	mu        sync.Mutex
	onReceipt func()
	onEvent   func()
}

func (m *concurrencyCheckingMoACallbacks) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	m.mu.Lock()
	onReceipt := m.onReceipt
	m.mu.Unlock()
	if onReceipt != nil {
		onReceipt()
	}
	m.moaMockCallbacks.OnToolSurfaceReceipt(receipt)
}

func (m *concurrencyCheckingMoACallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.mu.Lock()
	onEvent := m.onEvent
	m.mu.Unlock()
	if onEvent != nil {
		onEvent()
	}
	m.moaMockCallbacks.OnToolSurfaceEvent(event)
}
