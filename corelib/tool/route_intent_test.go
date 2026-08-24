package tool

import (
	"fmt"
	"testing"
)

func TestShouldAttemptRouteIntentRewrite(t *testing.T) {
	if !ShouldAttemptRouteIntentRewrite("会议录音") {
		t.Fatal("short meeting-record message should rewrite")
	}
	if !ShouldAttemptRouteIntentRewrite("帮我录一下") {
		t.Fatal("short record request should rewrite")
	}
	if !ShouldAttemptRouteIntentRewrite("弄一下") {
		t.Fatal("vague short request should rewrite")
	}
	if ShouldAttemptRouteIntentRewrite("") {
		t.Fatal("empty should not rewrite")
	}
	for _, trivial := range []string{"好的", "嗯", "ok", "确认", "1", "继续"} {
		if ShouldAttemptRouteIntentRewrite(trivial) {
			t.Fatalf("trivial ack %q should not rewrite", trivial)
		}
	}
	// Long unrelated message
	long := ""
	for i := 0; i < 40; i++ {
		long += "详细说明一下项目架构与模块划分 "
	}
	if ShouldAttemptRouteIntentRewrite(long) {
		t.Fatal("long unrelated message should not force rewrite")
	}
}

func TestParseRouteIntentJSON(t *testing.T) {
	raw := "```json\n{\"intent\":\"start_recording\",\"query_for_route\":\"打开桌面长时会议录音\",\"tool_families\":[\"recording\"],\"must_include\":[\"record_audio\"],\"must_exclude\":[],\"confidence\":0.9}\n```"
	intent := ParseRouteIntentJSON(raw)
	if intent == nil {
		t.Fatal("expected parsed intent")
	}
	if intent.Intent != "start_recording" {
		t.Fatalf("intent = %q", intent.Intent)
	}
	if intent.QueryForRoute == "" {
		t.Fatal("missing query_for_route")
	}
	if !intent.Usable() {
		t.Fatal("should be usable")
	}
}

func TestParseRouteIntentJSON_LowConfidenceIgnoredByUsableWhenOnlyConfidence(t *testing.T) {
	intent := ParseRouteIntentJSON(`{"intent":"other","query_for_route":"x","confidence":0.1}`)
	if intent == nil {
		// normalize keeps it if query present
		t.Fatal("parse should succeed")
	}
	if intent.Usable() {
		t.Fatal("low confidence should not be usable")
	}
}

func TestParseRouteIntentJSON_MissingConfidenceDefaultsUsable(t *testing.T) {
	intent := ParseRouteIntentJSON(`{"intent":"start_recording","query_for_route":"start mic","must_include":["record_audio"]}`)
	if intent == nil || !intent.Usable() {
		t.Fatalf("missing confidence with payload should default usable, got %+v", intent)
	}
	if intent.Confidence < MinRouteIntentConfidence {
		t.Fatalf("confidence = %v, want default >= %.2f", intent.Confidence, MinRouteIntentConfidence)
	}
}

func TestHasStrongLocalRouteSignalNeverAuthorizesFreeFormText(t *testing.T) {
	for _, msg := range []string{"会议录音", "take a screenshot", "弄一下"} {
		if HasStrongLocalRouteSignal(msg) {
			t.Fatalf("free-form wording %q must not authorize routing", msg)
		}
	}
}

func TestRouteWithOptions_UsesRewrittenQuery(t *testing.T) {
	router := NewRouter(nil)
	// Keep the core set small so candidate slots remain (a full core set would
	// consume MaxToolBudget and skip candidate scoring entirely).
	var tools []map[string]interface{}
	for _, name := range []string{"bash", "read_file", "write_file", "edit_file", "memory"} {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("session_search", "search past chat sessions and conversation history"))
	// Non-core, non-conditional candidate whose description only matches the
	// rewritten query. (Conditional tools like office stay UIC-gated: a rewrite
	// query alone must not force-include them — that is the planner's job.)
	tools = append(tools, makeToolDef("deck_maker", "create powerpoint slides presentation deck"))
	// A second partial match keeps min-max normalization well-defined (a
	// single-element score map normalizes to zero by design).
	tools = append(tools, makeToolDef("slide_viewer", "view existing slides"))
	for i := 0; i < 10; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("noise_%d", i), "qqqzzz nonmatching gibberish"))
	}

	// Original message is useless for retrieval; rewrite points at deck_maker.
	intent := &RouteIntent{
		Intent:        "office",
		QueryForRoute: "create powerpoint slides presentation deck",
		Confidence:    0.9,
	}
	result := router.RouteWithOptions("弄一下", tools, RouteOptions{Intent: intent})
	if !routedToolNames(result)["deck_maker"] {
		t.Fatalf("expected deck_maker via rewritten intent, got %v", routedToolNames(result))
	}
}

func TestRouteDoesNotPadZeroScoreCandidates(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	// Unrelated candidates with no lexical overlap → score ~0
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("zz_noise_%d", i), "qqqzzz nonmatching gibberish"))
	}
	result := router.Route("hello", tools)
	for _, tdef := range result {
		name := ExtractToolName(tdef)
		if len(name) > 8 && name[:8] == "zz_noise" {
			t.Fatalf("zero-score noise tool should not pad budget: %s in %v", name, routedToolNames(result))
		}
	}
}
