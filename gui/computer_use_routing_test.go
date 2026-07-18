package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestComputerUseEnabledFromConfig(t *testing.T) {
	if !computerUseEnabledFromConfig(nil) {
		t.Fatal("nil cfg default true")
	}
	f := false
	if computerUseEnabledFromConfig(&corelib.AppConfig{ComputerUseEnabled: &f}) {
		t.Fatal("want false")
	}
	tr := true
	if !computerUseEnabledFromConfig(&corelib.AppConfig{ComputerUseEnabled: &tr}) {
		t.Fatal("want true")
	}
}

func TestEnsureComputerUseTools(t *testing.T) {
	all := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		{"type": "function", "function": map[string]interface{}{"name": "computer_observe"}},
		{"type": "function", "function": map[string]interface{}{"name": "computer_click"}},
		{"type": "function", "function": map[string]interface{}{"name": "gui_click"}},
		{"type": "function", "function": map[string]interface{}{"name": "gui_type"}},
	}
	routed := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		{"type": "function", "function": map[string]interface{}{"name": "gui_click"}},
	}
	out := ensureComputerUseTools(routed, all, true)
	names := map[string]bool{}
	for _, tdef := range out {
		names[extractToolName(tdef)] = true
	}
	if !names["computer_observe"] || !names["computer_click"] {
		t.Fatalf("missing CU tools: %v", names)
	}
	if names["gui_click"] || names["gui_type"] {
		t.Fatalf("legacy GUI tools should be demoted: %v", names)
	}
	if names["bash"] != true {
		t.Fatal("bash should remain")
	}
	// inactive: no change
	out2 := ensureComputerUseTools(routed, all, false)
	if len(out2) != len(routed) {
		t.Fatalf("inactive should pass through, got %d", len(out2))
	}
}

func TestComputerUsePlaybookSection(t *testing.T) {
	if computerUsePlaybookSection(false) != "" {
		t.Fatal("inactive empty")
	}
	s := computerUsePlaybookSection(true)
	if s == "" || !strings.Contains(s, "Computer Use") || !strings.Contains(s, "computer_observe") {
		t.Fatalf("playbook section incomplete: %q", s)
	}
	_ = computeruse.Playbook()
}

// cuGateStubEmbedder maps texts containing a marker phrase to one axis and
// everything else to the orthogonal axis, so the computer_use anchor (which
// contains the marker) wins for marked queries and loses otherwise.
type cuGateStubEmbedder struct{ hit string }

func (s *cuGateStubEmbedder) Embed(text string) ([]float32, error) {
	if strings.Contains(text, s.hit) {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func (s *cuGateStubEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, txt := range texts {
		v, err := s.Embed(txt)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (s *cuGateStubEmbedder) Dim() int { return 2 }
func (s *cuGateStubEmbedder) Close()   {}

func resetComputerUseSessionForTest(t *testing.T) {
	t.Helper()
	globalComputerUse.mu.Lock()
	globalComputerUse.activated = false
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.activated = false
		globalComputerUse.mu.Unlock()
	})
}

func waitUICReady(t *testing.T, uic *intent.UnifiedIntentClassifier) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !uic.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("UIC anchor warmup timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShouldActivateComputerUseSemanticGate(t *testing.T) {
	resetComputerUseSessionForTest(t)

	// Only the computer_use anchor exemplar contains this marker phrase.
	uic := intent.New(intent.Config{Embedder: &cuGateStubEmbedder{hit: "打开word程序"}})
	waitUICReady(t, uic)
	h := &IMMessageHandler{unifiedClassifier: uic}

	if !h.shouldActivateComputerUse("打开word程序，编写一个你（maclaw）的简历。") {
		t.Fatal("semantic computer-use intent should activate")
	}
	if h.shouldActivateComputerUse("把昨天那个文件发给我") {
		t.Fatal("unrelated message must not activate")
	}
	if !h.shouldActivateComputerUse("@computer 帮我看看屏幕") {
		t.Fatal("explicit trigger should activate without classifier support")
	}
}

func TestShouldActivateComputerUseDegradedFailsClosed(t *testing.T) {
	resetComputerUseSessionForTest(t)

	h := &IMMessageHandler{unifiedClassifier: intent.New(intent.Config{Embedder: embedding.NewNoopEmbedder()})}
	if h.shouldActivateComputerUse("打开word程序写一份简历") {
		t.Fatal("degraded classifier must fail closed")
	}
	h2 := &IMMessageHandler{}
	if h2.shouldActivateComputerUse("打开word程序写一份简历") {
		t.Fatal("nil classifier must fail closed")
	}
}

func TestShouldActivateComputerUseStickySession(t *testing.T) {
	resetComputerUseSessionForTest(t)
	markComputerUseSessionActive()
	h := &IMMessageHandler{}
	if !h.shouldActivateComputerUse("随便聊聊") {
		t.Fatal("active CU session should keep the gate open")
	}
}

func TestComputerUseIntentActivated(t *testing.T) {
	cases := []struct {
		name string
		res  intent.ClassificationResult
		want bool
	}{
		{"cu above threshold", intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: 0.84}, true},
		{"cu at threshold", intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: 0.50}, true},
		{"cu below threshold", intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: 0.49}, false},
		{"other intent wins", intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.95}, false},
		{"degraded must fail closed", intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: 0.90, Degraded: true}, false},
		{"unknown degraded", intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true}, false},
	}
	for _, c := range cases {
		if got := computerUseIntentActivated(c.res); got != c.want {
			t.Errorf("%s: computerUseIntentActivated(%+v)=%v want %v", c.name, c.res, got, c.want)
		}
	}
}

// TestPrepareAgentLoopToolsComputerUseActivation covers the end-to-end path:
// semantic CU intent → gate opens → computer_* kept and legacy gui_* demoted
// in the final per-turn tool set.
func TestPrepareAgentLoopToolsComputerUseActivation(t *testing.T) {
	resetComputerUseSessionForTest(t)
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("computer_observe", "observe screen", nil, nil),
		toolDef("computer_click", "click element", nil, nil),
		toolDef("gui_click", "raw coordinate click", nil, nil),
		toolDef("gui_type", "raw coordinate type", nil, nil),
	}
	uic := intent.New(intent.Config{Embedder: &cuGateStubEmbedder{hit: "打开word程序"}})
	waitUICReady(t, uic)
	h := &IMMessageHandler{
		unifiedClassifier: uic,
		toolDefGen:        NewToolDefinitionGenerator(nil, defs),
	}

	active := h.prepareAgentLoopTools("u1", "打开word程序，写一份简历", nil, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(active.Tools)
	if !names["computer_observe"] || !names["computer_click"] {
		t.Fatalf("CU tools should be present when intent active: %#v", names)
	}
	if names["gui_click"] || names["gui_type"] {
		t.Fatalf("legacy gui tools should be demoted when CU active: %#v", names)
	}
	if !names["bash"] {
		t.Fatalf("unrelated core tools should remain: %#v", names)
	}

	inactive := h.prepareAgentLoopTools("u1", "把昨天的文件发给我", nil, agentLoopPhase{})
	inactiveNames := toolNameSetForWorkflowFilterTest(inactive.Tools)
	if !inactiveNames["gui_click"] {
		t.Fatalf("legacy gui tools should remain when CU inactive: %#v", inactiveNames)
	}
}
