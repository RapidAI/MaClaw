package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// moaMockCallbacks embeds mockCallbacks and implements MoAHost.
type moaMockCallbacks struct {
	*mockCallbacks
	preset     moa.ResolvedPreset
	progresses []string
	usageCalls int
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
	// Fake OpenAI-compatible server: ref path has no tools; aggregator has tools schema in body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		s := string(body)
		hasTools := strings.Contains(s, `"tools"`) && strings.Contains(s, "echo")
		hasAdvice := strings.Contains(s, "Private multi-model council advice")
		w.Header().Set("Content-Type", "application/json")
		if !hasTools {
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
			Name:                "review",
			Enabled:             true,
			FanoutMaxIterations: 1,
			OnlyBeforeFirstTool: true,
			ReferenceTimeoutSec: 10,
			AggregatorUsePrimary: true,
			Aggregator:          primary,
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
