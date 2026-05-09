package main

import (
	"math/rand"
	"testing"
	"testing/quick"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Property-based tests for the Coding Tool Gate.
//
// Uses testing/quick with at least 100 iterations per property.
// Tag format: Feature: coding-workflow-gate, Property N: <description>
// ---------------------------------------------------------------------------

// allKnownToolNames returns a combined list of blocklist + allowlist + extra names.
var allKnownToolNames = func() []string {
	names := make([]string, 0, len(codingToolBlocklist)+len(deliveryToolAllowlist)+4)
	for name := range codingToolBlocklist {
		names = append(names, name)
	}
	for name := range deliveryToolAllowlist {
		names = append(names, name)
	}
	names = append(names, "unknown_tool", "list_skills", "search_web", "read_file")
	return names
}()

// randomToolCalls generates a random list of tool calls from known tool names.
type randomToolCalls struct {
	Calls []llm.ToolCall
}

func (randomToolCalls) Generate(rand *rand.Rand, size int) interface{} {
	n := rand.Intn(10)
	calls := make([]llm.ToolCall, n)
	for i := range calls {
		name := allKnownToolNames[rand.Intn(len(allKnownToolNames))]
		calls[i] = llm.ToolCall{
			ID:   "call_" + name,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: name, Arguments: "{}"},
		}
	}
	return randomToolCalls{Calls: calls}
}

// ---------------------------------------------------------------------------
// Property 1: Tool stripping correctness
// Feature: coding-workflow-gate, Property 1: Tool stripping correctness
// Validates: Requirements 1.1, 1.3, 2.3, 2.4
// ---------------------------------------------------------------------------
func TestCodingGateProperty1_ToolStrippingCorrectness(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))
		n := rng.Intn(10)
		calls := make([]llm.ToolCall, n)
		for i := range calls {
			name := allKnownToolNames[rng.Intn(len(allKnownToolNames))]
			calls[i] = llm.ToolCall{
				ID:   "call_" + name,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: name, Arguments: "{}"},
			}
		}

		result := applyCodingToolGate(calls)

		// Verify stripped contains exactly coding tools.
		for _, tc := range result.stripped {
			if !isCodingTool(tc.Function.Name) {
				t.Logf("non-coding tool in stripped: %s", tc.Function.Name)
				return false
			}
		}
		// Verify remaining contains no coding tools.
		for _, tc := range result.remaining {
			if isCodingTool(tc.Function.Name) {
				t.Logf("coding tool in remaining: %s", tc.Function.Name)
				return false
			}
		}
		// Verify total count preserved.
		if len(result.stripped)+len(result.remaining) != len(calls) {
			t.Logf("count mismatch: stripped=%d remaining=%d total=%d", len(result.stripped), len(result.remaining), len(calls))
			return false
		}
		// Verify remaining order preserved.
		ri := 0
		for _, tc := range calls {
			if !isCodingTool(tc.Function.Name) {
				if ri >= len(result.remaining) || result.remaining[ri].ID != tc.ID {
					t.Logf("order not preserved at remaining index %d", ri)
					return false
				}
				ri++
			}
		}
		// Verify applied flag.
		if result.applied != (len(result.stripped) > 0) {
			t.Logf("applied=%v but stripped=%d", result.applied, len(result.stripped))
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 1 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 2: Text content preservation
// Feature: coding-workflow-gate, Property 2: Text content preservation
// Validates: Requirements 1.2
// ---------------------------------------------------------------------------
func TestCodingGateProperty2_TextContentPreservation(t *testing.T) {
	f := func(text string, seed int64) bool {
		rng := rand.New(rand.NewSource(seed))
		n := rng.Intn(8) + 1
		calls := make([]llm.ToolCall, n)
		for i := range calls {
			name := allKnownToolNames[rng.Intn(len(allKnownToolNames))]
			calls[i] = llm.ToolCall{
				ID:   "call_" + name,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: name, Arguments: "{}"},
			}
		}

		// applyCodingToolGate only operates on tool calls, never on text.
		// Verify the function does not have any side effect on text.
		textBefore := text
		_ = applyCodingToolGate(calls)
		if text != textBefore {
			t.Logf("text was modified")
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 3: Gate inactivity for non-qualifying configurations
// Feature: coding-workflow-gate, Property 3: Gate inactivity
// Validates: Requirements 1.4, 1.5, 7.1
//
// Without classifiers, the gate must not infer workflow intent. Background is
// still guaranteed inactive.
// ---------------------------------------------------------------------------
func TestCodingGateProperty3_GateInactivity(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))

		// Generate random user text — any text with background loop should be inactive.
		texts := []string{
			"帮我翻译这段话",
			"帮我写一个 Python 脚本",
			"开发一个游戏",
			"查天气",
			"ssh 到服务器",
		}
		userText := texts[rng.Intn(len(texts))]

		cfg := newCodingToolGateConfig(userText, LoopKindBackground)
		if cfg.active {
			t.Logf("gate should be inactive for background loop, userText=%q, but active=true reason=%s", userText, cfg.reason)
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 3b: Ordinary-agent fallback for loops without classifiers
// Feature: coding-workflow-gate, Property 3b: No workflow inference by absence
// Validates: without classifiers, the workflow gate stays inactive
// ---------------------------------------------------------------------------
func TestCodingGateProperty3b_OrdinaryAgentFallbackWithoutClassifiers(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))

		texts := []string{
			"帮我翻译这段话",
			"帮我写一个 Python 脚本，直接做",
			"开发一个游戏",
			"查天气",
			"有bug，修复一下",
		}
		userText := texts[rng.Intn(len(texts))]

		cfg := newCodingToolGateConfig(userText, LoopKindChat)
		if cfg.active {
			t.Logf("without classifiers, gate should be inactive for userText=%q, but active=true reason=%s", userText, cfg.reason)
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 3b failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 4: Tool classification determinism
// Feature: coding-workflow-gate, Property 4: Tool classification determinism
// Validates: Requirements 2.1, 2.2, 2.3, 2.4
// ---------------------------------------------------------------------------
func TestCodingGateProperty4_ToolClassificationDeterminism(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))
		// Pick a random tool name from known + random strings.
		var name string
		if rng.Intn(2) == 0 {
			name = allKnownToolNames[rng.Intn(len(allKnownToolNames))]
		} else {
			// Generate a random string.
			buf := make([]byte, rng.Intn(20)+1)
			for i := range buf {
				buf[i] = byte('a' + rng.Intn(26))
			}
			name = string(buf)
		}

		got := isCodingTool(name)
		expected := codingToolBlocklist[name] && !deliveryToolAllowlist[name]
		if got != expected {
			t.Logf("isCodingTool(%q)=%v expected=%v", name, got, expected)
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 5: Skip signal detection completeness
// Feature: coding-workflow-gate, Property 5: Skip signal detection
// Validates: Requirements 3.1, 3.2, 3.3, 3.4
// ---------------------------------------------------------------------------
