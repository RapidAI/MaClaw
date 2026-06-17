package browser

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPolicyFromArgsParsesDomainLists(t *testing.T) {
	policy := policyFromArgs(map[string]interface{}{
		"allowed_domains":               []interface{}{"example.com", "sub.example.com"},
		"blocked_domains":               []interface{}{"forbidden.com"},
		"allow_cross_origin_navigation": false,
	})
	if len(policy.AllowedDomains) != 2 {
		t.Fatalf("AllowedDomains len = %d", len(policy.AllowedDomains))
	}
	if len(policy.BlockedDomains) != 1 || policy.BlockedDomains[0] != "forbidden.com" {
		t.Fatalf("BlockedDomains = %#v", policy.BlockedDomains)
	}
	if policy.AllowCrossOriginNavigation {
		t.Fatal("AllowCrossOriginNavigation = true, want false")
	}
}

func TestMarshalBrowserResultIncludesDisplay(t *testing.T) {
	result := marshalBrowserResult(true, "ok", map[string]interface{}{"session_id": "abc"})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if payload["display"] != "ok" {
		t.Fatalf("display = %#v", payload["display"])
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data missing: %#v", payload)
	}
	if data["session_id"] != "abc" {
		t.Fatalf("session_id = %#v", data["session_id"])
	}
}

func TestRegisterToolsContainsBrowserSessionMVP(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{
		"browser_session_start",
		"browser_session_stop",
		"browser_observe",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_wait",
		"browser_back",
		"browser_refresh",
		"browser_extract",
	} {
		toolDef, ok := reg.Get(name)
		if !ok {
			t.Fatalf("tool %q not registered", name)
		}
		if toolDef == nil {
			t.Fatalf("tool %q definition is nil", name)
		}
	}
}

func TestBrowserTypeSchemaExposesMarkdownContentFormat(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_type")
	if !ok || toolDef == nil {
		t.Fatal("browser_type not registered")
	}
	if _, ok := toolDef.InputSchema["content_format"]; !ok {
		t.Fatalf("browser_type schema missing content_format: %#v", toolDef.InputSchema)
	}
}

func TestBrowserTaskRunSchemaExposesMarkdownContentFormat(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTaskTools(reg, NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil), nil)
	toolDef, ok := reg.Get("browser_task_run")
	if !ok || toolDef == nil {
		t.Fatal("browser_task_run not registered")
	}
	if _, ok := toolDef.InputSchema["content_format"]; !ok {
		t.Fatalf("browser_task_run schema missing content_format: %#v", toolDef.InputSchema)
	}
}

func TestApplyDefaultContentFormatToTypeSteps(t *testing.T) {
	steps := []StepSpec{
		{Action: "type", Params: map[string]string{"selector": "#body"}},
		{Action: "click", Params: map[string]string{"selector": "button"}},
		{Action: "type", Params: map[string]string{"selector": "#title", "content_format": "plain"}},
	}
	applyDefaultContentFormatToTypeSteps(steps, "markdown")
	if steps[0].Params["content_format"] != BrowserContentFormatMarkdown {
		t.Fatalf("first type content_format = %q", steps[0].Params["content_format"])
	}
	if _, ok := steps[1].Params["content_format"]; ok {
		t.Fatalf("click step got content_format: %#v", steps[1].Params)
	}
	if steps[2].Params["content_format"] != "plain" {
		t.Fatalf("explicit content_format overwritten: %#v", steps[2].Params)
	}
}

func TestBrowserSessionToolsRequireSessionID(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{
		"browser_session_stop",
		"browser_observe",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_wait",
		"browser_back",
		"browser_refresh",
		"browser_extract",
		"browser_scroll",
		"browser_select",
		"browser_list_pages",
		"browser_switch_page",
		"browser_close",
		"browser_set_files",
		"browser_info",
	} {
		toolDef, ok := reg.Get(name)
		if !ok || toolDef == nil {
			t.Fatalf("tool %q not registered", name)
		}
		found := false
		for _, req := range toolDef.Required {
			if req == "session_id" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tool %q required = %#v, want session_id", name, toolDef.Required)
		}
		if _, ok := toolDef.InputSchema["session_id"]; !ok {
			t.Fatalf("tool %q missing session_id in input schema", name)
		}
	}
}

func TestBrowserSetFilesSchemaAcceptsFilePathsAlias(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_set_files")
	if !ok || toolDef == nil {
		t.Fatal("browser_set_files not registered")
	}
	if _, ok := toolDef.InputSchema["file_paths"]; !ok {
		t.Fatal("browser_set_files missing file_paths alias")
	}
	files, ok := toolDef.InputSchema["files"].(map[string]interface{})
	if !ok || files["type"] != "array" {
		t.Fatalf("files schema = %#v, want array", toolDef.InputSchema["files"])
	}
	for _, req := range toolDef.Required {
		if req == "files" || req == "file_paths" {
			t.Fatalf("browser_set_files required = %#v, files/file_paths are alternatives and must not be individually required", toolDef.Required)
		}
	}
}

func TestBrowserSessionStartSchemaDefaultsToPersistent(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_session_start")
	if !ok || toolDef == nil {
		t.Fatal("browser_session_start not registered")
	}
	mode, ok := toolDef.InputSchema["mode"].(map[string]interface{})
	if !ok {
		t.Fatalf("mode schema = %#v", toolDef.InputSchema["mode"])
	}
	desc, _ := mode["description"].(string)
	if !strings.Contains(desc, "persistent") || !strings.Contains(desc, "cookie") || !strings.Contains(desc, "default") {
		t.Fatalf("mode description = %q, want persistent default with cookie/session retention", desc)
	}
}

func TestBrowserSessionStopDoesNotExposeCloseBrowser(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_session_stop")
	if !ok || toolDef == nil {
		t.Fatal("browser_session_stop not registered")
	}
	if _, ok := toolDef.InputSchema["close_browser"]; ok {
		t.Fatalf("browser_session_stop schema exposes close_browser: %#v", toolDef.InputSchema)
	}
	if strings.Contains(toolDef.Description, "close_browser") {
		t.Fatalf("browser_session_stop description exposes close_browser: %q", toolDef.Description)
	}
}

func TestBrowserClickSchemaIncludesTextFallback(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_click")
	if !ok || toolDef == nil {
		t.Fatal("browser_click not registered")
	}
	if _, ok := toolDef.InputSchema["text"]; !ok {
		t.Fatal("browser_click missing text fallback schema")
	}
	if !strings.Contains(toolDef.Description, "text") {
		t.Fatalf("browser_click description = %q, want text fallback", toolDef.Description)
	}
}

func TestBrowserObserveSchemaDefaultsScreenshotOff(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_observe")
	if !ok || toolDef == nil {
		t.Fatal("browser_observe not registered")
	}
	field, ok := toolDef.InputSchema["include_screenshot"].(map[string]interface{})
	if !ok {
		t.Fatalf("include_screenshot schema = %#v", toolDef.InputSchema["include_screenshot"])
	}
	desc, _ := field["description"].(string)
	if !strings.Contains(desc, "false") {
		t.Fatalf("include_screenshot description = %q, want default false", desc)
	}
}

func TestUnstableBrowserToolsNotRegistered(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{"browser_eval", "browser_get_text", "browser_get_html", "browser_click_at", "browser_screenshot"} {
		if toolDef, ok := reg.Get(name); ok || toolDef != nil {
			t.Fatalf("%s registered = %#v, want removed from stable browser surface", name, toolDef)
		}
	}
}

func TestStableToolSessionModeMapsAutoToPersistent(t *testing.T) {
	if got := stableToolSessionMode(map[string]interface{}{"mode": "auto"}); got != SessionModePersistent {
		t.Fatalf("stableToolSessionMode(auto) = %q, want persistent", got)
	}
	if got := stableToolSessionMode(map[string]interface{}{"mode": "isolated"}); got != SessionModeIsolated {
		t.Fatalf("stableToolSessionMode(isolated) = %q, want isolated", got)
	}
	if got := stableToolSessionMode(map[string]interface{}{"mode": "bad"}); got != SessionModePersistent {
		t.Fatalf("stableToolSessionMode(bad) = %q, want persistent", got)
	}
}

func TestBrowserExtractSchemaIncludesContinuationFields(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	toolDef, ok := reg.Get("browser_extract")
	if !ok || toolDef == nil {
		t.Fatal("browser_extract not registered")
	}
	if got := toolDef.Description; !strings.Contains(got, "offset/max_chars") {
		t.Fatalf("Description = %q", got)
	}
	for _, field := range []string{"offset", "max_chars"} {
		entry, ok := toolDef.InputSchema[field]
		if !ok {
			t.Fatalf("browser_extract missing schema field %q", field)
		}
		meta, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("schema[%q] = %#v", field, entry)
		}
		if meta["type"] != "integer" {
			t.Fatalf("schema[%q].type = %#v", field, meta["type"])
		}
	}
}

func TestBrowserTaskToolsExposeSessionID(t *testing.T) {
	reg := tool.NewRegistry()
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	RegisterTaskTools(reg, supervisor, nil)
	for _, name := range []string{"browser_task_run", "browser_task_status", "browser_task_verify"} {
		toolDef, ok := reg.Get(name)
		if !ok || toolDef == nil {
			t.Fatalf("tool %q not registered", name)
		}
		if _, ok := toolDef.InputSchema["session_id"]; !ok {
			t.Fatalf("tool %q missing session_id in input schema", name)
		}
		found := false
		for _, req := range toolDef.Required {
			if req == "session_id" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tool %q required = %#v, want session_id", name, toolDef.Required)
		}
	}
}

func TestBrowserOCRToolNotRegistered(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterOCRTool(reg, nil, func() (*Session, error) { return nil, nil })
	if toolDef, ok := reg.Get("browser_ocr"); ok || toolDef != nil {
		t.Fatalf("browser_ocr registered = %#v, want disabled", toolDef)
	}
}

func TestTaskSupervisorRejectsEvalStep(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	err := supervisor.doStep(nil, StepSpec{Action: "eval", Params: map[string]string{"expression": "document.cookie"}})
	if err == nil || !strings.Contains(err.Error(), "eval step is disabled") {
		t.Fatalf("doStep eval error = %v, want disabled", err)
	}
}

func TestTaskSupervisorRejectsClickAtStep(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	err := supervisor.doStep(nil, StepSpec{Action: "click_at", Params: map[string]string{"selector": "button"}})
	if err == nil || !strings.Contains(err.Error(), "click_at step is disabled") {
		t.Fatalf("doStep click_at error = %v, want disabled", err)
	}
}

func TestAgentTaskTypeStepAllowsFocusedEditableWithoutSelector(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	err := supervisor.doAgentStep(&BrowserAgentSession{}, StepSpec{Action: "type", Params: map[string]string{"text": "Title"}})
	if err == nil {
		t.Fatal("expected browser session error from nil session")
	}
	if strings.Contains(err.Error(), "missing selector/ref") {
		t.Fatalf("type step should use active editable fallback, got %v", err)
	}
}

func TestTaskVerifierRejectsOCRContainsWithoutScreenshot(t *testing.T) {
	verifier := NewTaskVerifier(nil, func() (*Session, error) { return nil, nil })
	result := verifier.checkOne(nil, CriterionSpec{Type: "ocr_contains", Pattern: "ok"})
	if result.Passed || !strings.Contains(result.Error, "ocr_contains is disabled") {
		t.Fatalf("ocr_contains result = %#v, want disabled", result)
	}
}

func TestRecorderAndReplayMutationToolsNotRegistered(t *testing.T) {
	reg := tool.NewRegistry()
	recorder := NewBrowserRecorder(func() (*Session, error) { return nil, nil }, nil)
	replayer := NewFlowReplayer(NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil), nil, nil)
	RegisterRecorderTools(reg, recorder, replayer, nil, nil, nil, nil)
	for _, name := range []string{"browser_record_start", "browser_record_stop", "browser_task_replay"} {
		if toolDef, ok := reg.Get(name); ok || toolDef != nil {
			t.Fatalf("tool %q registered = %#v, want removed from stable browser surface", name, toolDef)
		}
	}
	if toolDef, ok := reg.Get("browser_list_flows"); !ok || toolDef == nil || toolDef.Handler == nil {
		t.Fatal("browser_list_flows should remain registered as read-only inspection")
	}
}

func TestTaskSupervisorForArgsRejectsMissingSessionID(t *testing.T) {
	base := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	_, err := taskSupervisorForArgs(base, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "missing session_id") {
		t.Fatalf("taskSupervisorForArgs error = %v, want missing session_id", err)
	}
}

func TestTaskSupervisorForArgsRejectsNilBase(t *testing.T) {
	if _, err := taskSupervisorForArgs(nil, map[string]interface{}{}); err == nil {
		t.Fatal("expected error for nil supervisor")
	}
}

func TestBrowserJSONArgAcceptsStructuredSteps(t *testing.T) {
	got, err := browserJSONArg(map[string]interface{}{
		"steps": []interface{}{
			map[string]interface{}{"action": "navigate", "params": map[string]interface{}{"url": "https://example.com"}},
		},
	}, "steps")
	if err != nil {
		t.Fatalf("browserJSONArg error = %v", err)
	}
	var steps []StepSpec
	if err := json.Unmarshal([]byte(got), &steps); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", got, err)
	}
	if len(steps) != 1 || steps[0].Action != "navigate" || steps[0].Params["url"] != "https://example.com" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestBrowserJSONArgKeepsJSONStrings(t *testing.T) {
	input := `[{"action":"wait","params":{"duration_ms":"100"}}]`
	got, err := browserJSONArg(map[string]interface{}{"steps": input}, "steps")
	if err != nil {
		t.Fatalf("browserJSONArg error = %v", err)
	}
	if got != input {
		t.Fatalf("browserJSONArg = %q, want %q", got, input)
	}
}

func TestBrowserFilesArgAcceptsArrayAndAlias(t *testing.T) {
	got := browserFilesArg(map[string]interface{}{"file_paths": []interface{}{"C:/tmp/a.txt", " C:/tmp/b.txt "}})
	if len(got) != 2 || got[0] != "C:/tmp/a.txt" || got[1] != "C:/tmp/b.txt" {
		t.Fatalf("browserFilesArg alias array = %#v", got)
	}
	got = browserFilesArg(map[string]interface{}{"files": "C:/tmp/a.txt, C:/tmp/b.txt"})
	if len(got) != 2 || got[0] != "C:/tmp/a.txt" || got[1] != "C:/tmp/b.txt" {
		t.Fatalf("browserFilesArg string = %#v", got)
	}
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestNormalizeStepParams_FlatFormatMovedToParams(t *testing.T) {
	// LLM often outputs flat step format: {"action":"click","ref":"@e19"}
	// instead of nested: {"action":"click","params":{"ref":"@e19"}}
	stepsJSON := `[{"action":"click","ref":"@e19"},{"action":"wait","duration_ms":1500},{"action":"navigate","url":"https://www.zhihu.com/"},{"action":"type","text":"hello","selector":".input"}]`
	var steps []StepSpec
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeStepParams(stepsJSON, steps)

	// click step: ref should be in Params
	if steps[0].Params["ref"] != "@e19" {
		t.Errorf("click step: Params[ref] = %q, want @e19", steps[0].Params["ref"])
	}
	// wait step: duration_ms should be in Params as string "1500"
	if steps[1].Params["duration_ms"] != "1500" {
		t.Errorf("wait step: Params[duration_ms] = %q, want 1500", steps[1].Params["duration_ms"])
	}
	// navigate step: url should be in Params
	if steps[2].Params["url"] != "https://www.zhihu.com/" {
		t.Errorf("navigate step: Params[url] = %q, want https://www.zhihu.com/", steps[2].Params["url"])
	}
	// type step: text and selector should be in Params
	if steps[3].Params["text"] != "hello" {
		t.Errorf("type step: Params[text] = %q, want hello", steps[3].Params["text"])
	}
	if steps[3].Params["selector"] != ".input" {
		t.Errorf("type step: Params[selector] = %q, want .input", steps[3].Params["selector"])
	}
}

func TestNormalizeStepParams_NestedFormatPreserved(t *testing.T) {
	// When LLM correctly uses nested format, normalizeStepParams should not interfere.
	stepsJSON := `[{"action":"click","params":{"ref":"@e19"}},{"action":"navigate","params":{"url":"https://example.com"}}]`
	var steps []StepSpec
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeStepParams(stepsJSON, steps)

	if steps[0].Params["ref"] != "@e19" {
		t.Errorf("click step: Params[ref] = %q, want @e19", steps[0].Params["ref"])
	}
	if steps[1].Params["url"] != "https://example.com" {
		t.Errorf("navigate step: Params[url] = %q, want https://example.com", steps[1].Params["url"])
	}
}

func TestNormalizeStepParams_MixedFormat(t *testing.T) {
	// Some steps nested, some flat — both should work.
	stepsJSON := `[{"action":"click","params":{"ref":"@e1"}},{"action":"wait","duration_ms":2000},{"action":"navigate","url":"https://test.com"}]`
	var steps []StepSpec
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeStepParams(stepsJSON, steps)

	if steps[0].Params["ref"] != "@e1" {
		t.Errorf("step 0: Params[ref] = %q, want @e1", steps[0].Params["ref"])
	}
	if steps[1].Params["duration_ms"] != "2000" {
		t.Errorf("step 1: Params[duration_ms] = %q, want 2000", steps[1].Params["duration_ms"])
	}
	if steps[2].Params["url"] != "https://test.com" {
		t.Errorf("step 2: Params[url] = %q, want https://test.com", steps[2].Params["url"])
	}
}

func TestNormalizeStepParams_HybridParamsAndTopLevel(t *testing.T) {
	// LLM puts some keys in params and some at top level.
	// Top-level extras should be merged into Params without overwriting existing.
	stepsJSON := `[{"action":"click","ref":"@e19","params":{"snapshot_id":"snap-1"}},{"action":"type","text":"hello","params":{"content_format":"markdown"}}]`
	var steps []StepSpec
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeStepParams(stepsJSON, steps)

	// click step: ref merged from top-level, snapshot_id preserved from nested params
	if steps[0].Params["ref"] != "@e19" {
		t.Errorf("click: Params[ref] = %q, want @e19", steps[0].Params["ref"])
	}
	if steps[0].Params["snapshot_id"] != "snap-1" {
		t.Errorf("click: Params[snapshot_id] = %q, want snap-1", steps[0].Params["snapshot_id"])
	}
	// type step: text merged from top-level, content_format preserved from nested params
	if steps[1].Params["text"] != "hello" {
		t.Errorf("type: Params[text] = %q, want hello", steps[1].Params["text"])
	}
	if steps[1].Params["content_format"] != "markdown" {
		t.Errorf("type: Params[content_format] = %q, want markdown", steps[1].Params["content_format"])
	}
}

func TestNormalizeStepParams_NestedTakesPrecedence(t *testing.T) {
	// If same key exists in both nested params and top-level, nested wins.
	stepsJSON := `[{"action":"click","ref":"@top","params":{"ref":"@nested"}}]`
	var steps []StepSpec
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeStepParams(stepsJSON, steps)

	// Nested params should take precedence.
	if steps[0].Params["ref"] != "@nested" {
		t.Errorf("Params[ref] = %q, want @nested (nested takes precedence)", steps[0].Params["ref"])
	}
}
