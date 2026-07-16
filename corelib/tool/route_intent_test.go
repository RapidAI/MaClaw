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

func TestExpandExcludes_ProtectsCoreFileTools(t *testing.T) {
	intent := &RouteIntent{
		Intent:        "search",
		QueryForRoute: "search web",
		MustExclude:   []string{"bash", "read_file", "record_audio"},
		Confidence:    0.9,
	}
	available := map[string]bool{"bash": true, "read_file": true, "record_audio": true, "web_search": true}
	ex := intent.ExpandExcludes(available)
	got := map[string]bool{}
	for _, n := range ex {
		got[n] = true
	}
	if got["bash"] || got["read_file"] {
		t.Fatalf("core file tools must not be excluded via rewrite: %v", ex)
	}
	if !got["record_audio"] {
		t.Fatalf("record_audio may be excluded when model asks: %v", ex)
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

func TestExpandPins_CaseInsensitiveToolNames(t *testing.T) {
	intent := &RouteIntent{
		Intent:        "coding",
		QueryForRoute: "search code",
		MustInclude:   []string{"glob", "RIPGREP"},
		Confidence:    0.9,
	}
	available := map[string]bool{"Glob": true, "ripgrep": true, "bash": true}
	pins := intent.ExpandPins(available)
	got := map[string]bool{}
	for _, p := range pins {
		got[p] = true
	}
	if !got["Glob"] {
		t.Fatalf("expected canonical Glob, got %v", pins)
	}
	if !got["ripgrep"] {
		t.Fatalf("expected ripgrep, got %v", pins)
	}
}

func TestHasStrongLocalRouteSignal(t *testing.T) {
	if !HasStrongLocalRouteSignal("会议录音") {
		t.Fatal("meeting recording should be strong local")
	}
	if !HasStrongLocalRouteSignal("take a screenshot") {
		t.Fatal("screenshot should be strong local")
	}
	if HasStrongLocalRouteSignal("弄一下") {
		t.Fatal("vague message should not be strong local")
	}
}

func TestRouteIntentExpandPins(t *testing.T) {
	intent := &RouteIntent{
		Intent:        "start_recording",
		QueryForRoute: "start desktop meeting recording now",
		ToolFamilies:  []string{"recording"},
		MustInclude:   []string{"record_audio"},
		Confidence:    0.9,
	}
	available := map[string]bool{"record_audio": true, "asr": true, "bash": true}
	pins := intent.ExpandPins(available)
	found := false
	for _, p := range pins {
		if p == "record_audio" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected record_audio pin, got %v", pins)
	}
}

func TestRouteWithOptions_PinsMustInclude(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	// Non-core tool that only gets in via pin.
	tools = append(tools, makeToolDef("office", "generate documents and presentations"))
	for i := 0; i < 40; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "noise tool unrelated"))
	}

	intent := &RouteIntent{
		Intent:        "office",
		QueryForRoute: "create a formal powerpoint presentation for product launch",
		MustInclude:   []string{"office"},
		Confidence:    0.95,
	}
	result := router.RouteWithOptions("弄一下", tools, RouteOptions{Intent: intent})
	if !routedToolNames(result)["office"] {
		t.Fatalf("pinned office tool missing from route result: %v", result)
	}
}

func TestRouteWithOptions_UsesRewrittenQuery(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("session_search", "search past chat sessions and conversation history"))
	// Non-core: specialized meeting recorder description so rewritten query hits it.
	tools = append(tools, makeToolDef("office", "create word excel powerpoint documents slides pdf"))

	// Original message is useless for retrieval; rewrite points at office.
	intent := &RouteIntent{
		Intent:        "office",
		QueryForRoute: "create powerpoint slides presentation deck",
		MustInclude:   []string{"office"},
		Confidence:    0.9,
	}
	result := router.RouteWithOptions("弄一下", tools, RouteOptions{Intent: intent})
	if !routedToolNames(result)["office"] {
		t.Fatalf("expected office via rewritten intent, got %v", routedToolNames(result))
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
