package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPolicyFromArgsDefaultsDenyUploadDownloadPopup(t *testing.T) {
	policy := policyFromArgs(map[string]interface{}{})
	if policy.AllowUpload || policy.AllowDownload || policy.AllowPopup {
		t.Fatalf("policy defaults should deny upload/download/popup: %#v", policy)
	}
	if err := validateUploadPolicy(policy); err == nil {
		t.Fatal("expected upload blocked")
	}
	if err := validateDownloadPolicy(policy); err == nil {
		t.Fatal("expected download blocked")
	}
	if err := validatePopupPolicy(policy); err == nil {
		t.Fatal("expected popup blocked")
	}
	if !shouldDenyManagedDownloads(policy, SessionModePersistent) {
		t.Fatal("managed sessions should deny downloads by default")
	}
	if shouldDenyManagedDownloads(policy, SessionModeConnectUser) {
		t.Fatal("user-chrome sessions must not set browser-wide download deny")
	}
	policy.AllowDownload = true
	if shouldDenyManagedDownloads(policy, SessionModeIsolated) {
		t.Fatal("allow_download=true should not deny")
	}
	policy.AllowUpload = true
	if err := validateUploadPolicy(policy); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterToolsContainsHoverPressDialog(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{"browser_hover", "browser_press", "browser_dialog"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("tool %q not registered", name)
		}
	}
}

func TestLLMSafeBrowserErrorStripsSelector(t *testing.T) {
	got := llmSafeBrowserError(fmt.Errorf("element not found: button.buy-now:nth-of-type(1)"))
	if strings.Contains(got, "nth-of-type") || strings.Contains(got, "buy-now") {
		t.Fatalf("leaked selector: %q", got)
	}
	got = llmSafeBrowserError(fmt.Errorf(`ref @e1 is stale; run observe again to get fresh refs: element not found: #buy`))
	if strings.Contains(got, "#buy") {
		t.Fatalf("leaked selector in stale wrap: %q", got)
	}
	if !strings.Contains(got, "@e1") {
		t.Fatalf("dropped ref: %q", got)
	}
	raw := marshalActionResult(nil, nil, fmt.Errorf("element not found: #checkout form button"), ExpectSpec{})
	if strings.Contains(raw, "#checkout") {
		t.Fatalf("marshal leaked selector: %s", raw)
	}
}

func TestMarshalActionResultUnchangedOmitsRefs(t *testing.T) {
	snap := BrowserSnapshot{
		SnapshotID: "snap-2",
		URL:        "https://example.com/buy",
		Title:      "Buy",
		Refs:       []BrowserElementRef{{Ref: "@e1", Role: "button", Name: "Buy"}},
	}
	s := &BrowserAgentSession{ID: "sess-1", lastFingerprint: snapshotFingerprint(snap)}
	result := s.completeAction("browser_click", "clicked @e1", "@e1", &BrowserObservation{Snapshot: snap}, map[string]interface{}{"target": "@e1"}, true)
	raw := marshalActionResult(s, result, nil, ExpectSpec{})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != false {
		t.Fatalf("ok=%v", payload["ok"])
	}
	data, _ := payload["data"].(map[string]interface{})
	if data == nil {
		t.Fatal("missing data")
	}
	if _, ok := data["refs"]; ok {
		t.Fatal("unchanged result leaked refs")
	}
	if data["target"] != "@e1" {
		t.Fatalf("target=%v", data["target"])
	}
	if _, ok := data["delta"]; !ok {
		t.Fatal("missing delta")
	}
}

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

func TestCompactVerifyFailureOmitsSelector(t *testing.T) {
	got := compactVerifyFailure(&VerifyResult{
		Passed: false,
		Details: []CriterionResult{{
			Criterion: CriterionSpec{Type: "dom_exists", Selector: "#secret"},
			Error:     "element not found",
		}},
	})
	if strings.Contains(got, "#secret") {
		t.Fatalf("task_run verify failure leaked selector: %s", got)
	}
	if !strings.Contains(got, "success criteria not met:") {
		t.Fatalf("got=%s", got)
	}
}

func TestMarshalTaskStateOmitsFrameID(t *testing.T) {
	raw := marshalTaskState(&TaskState{
		ID:               "bt-1",
		Status:           TaskStatusPaused,
		CurrentStep:      2,
		TotalSteps:       4,
		RetryCount:       1,
		LastError:        "step verification failed",
		LastResultStatus: "ask",
		StepTraces:       []StepTrace{{FrameID: "CDP-FRAME-SECRET"}},
		Checkpoints:      []Checkpoint{{FrameID: "CDP-FRAME-SECRET", ScreenshotB64: "iVBOR"}},
	})
	if strings.Contains(raw, "CDP-FRAME-SECRET") || strings.Contains(raw, "iVBOR") || strings.Contains(raw, "frame_id") || strings.Contains(raw, "screenshot") {
		t.Fatalf("task_status leaked internals: %s", raw)
	}
	if !strings.Contains(raw, `"task_id":"bt-1"`) || !strings.Contains(raw, `"retries":1`) {
		t.Fatalf("got=%s", raw)
	}
}

func TestFormatStepVerifyFailureOmitsSelector(t *testing.T) {
	err := formatStepVerifyFailure(&VerifyResult{
		Passed: false,
		Details: []CriterionResult{{
			Criterion: CriterionSpec{Type: "dom_exists", Selector: "#secret"},
			Error:     "element not found",
		}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "#secret") {
		t.Fatalf("step verify failure leaked selector: %v", err)
	}
	if formatStepVerifyFailure(nil) == nil {
		t.Fatal("nil verify result should still fail")
	}
}

func TestStepOutcomeFailureGoalClassUnchanged(t *testing.T) {
	err := stepOutcomeFailure(stepOutcome{result: &BrowserActionResult{GoalClass: true, Status: "unchanged"}})
	if err == nil {
		t.Fatal("expected unchanged goal-class to fail the step")
	}
	if stepOutcomeFailure(stepOutcome{result: &BrowserActionResult{GoalClass: true, Status: "ok"}}) != nil {
		t.Fatal("ok goal-class should pass")
	}
	if stepOutcomeFailure(stepOutcome{result: &BrowserActionResult{GoalClass: false, Status: "unchanged"}}) != nil {
		t.Fatal("non-goal unchanged should not fail the task step")
	}
}

func TestForgetSubmitClickAllowsRetry(t *testing.T) {
	s := &BrowserAgentSession{recentSubmitClicks: map[string]time.Time{"k": time.Now()}}
	s.forgetSubmitClick("k")
	if err := s.guardSubmitClick("k"); err != nil {
		t.Fatalf("forget should allow retry: %v", err)
	}
}

func TestShouldAskCaptchaWidgetReconfirmsStaleSnapshot(t *testing.T) {
	if !shouldAskCaptchaWidget(true, true, false, false, nil) {
		t.Fatal("stale widget snapshot without peek must still ask")
	}
	if shouldAskCaptchaWidget(true, true, true, false, nil) {
		t.Fatal("peek showing widget gone must not ask again")
	}
	if !shouldAskCaptchaWidget(true, true, true, true, nil) {
		t.Fatal("peek confirming widget must ask")
	}
	if !shouldAskCaptchaWidget(true, true, true, false, fmt.Errorf("eval failed")) {
		t.Fatal("peek failure must fail closed")
	}
	if shouldAskCaptchaWidget(true, false, false, false, nil) {
		t.Fatal("trusted false snapshot must not ask")
	}
	if shouldAskCaptchaWidget(false, false, false, false, nil) {
		t.Fatal("no snapshot and no peek must not fake ask")
	}
	if !shouldAskCaptchaWidget(false, false, true, true, nil) {
		t.Fatal("no snapshot plus widget peek must ask")
	}
}

func TestMarshalTaskRunResultSanitizesSelector(t *testing.T) {
	raw := marshalTaskRunResult(nil, fmt.Errorf("element not found: #checkout form button"))
	if strings.Contains(raw, "#checkout") {
		t.Fatalf("task_run error leaked selector: %s", raw)
	}
	status := marshalTaskState(&TaskState{ID: "bt-1", Status: TaskStatusFailed, LastError: "element not found: #secret"})
	if strings.Contains(status, "#secret") {
		t.Fatalf("task_status last_error leaked selector: %s", status)
	}
}

func TestMarshalTaskRunAskIncludesResumeTaskID(t *testing.T) {
	raw := marshalTaskRunResult(&TaskState{
		ID:               "bt-9",
		Status:           TaskStatusPaused,
		LastResultStatus: "ask",
		AskUser:          captchaAskUserRequest("challenge https://example.com"),
	}, nil)
	if !strings.Contains(raw, "resume_task_id=bt-9") {
		t.Fatalf("ask payload missing resume_task_id: %s", raw)
	}
	if strings.Contains(raw, `"status":"paused"`) {
		t.Fatalf("must not wrap ask as paused json: %s", raw)
	}
	raw = marshalTaskRunResult(&TaskState{ID: "bt-9", Status: TaskStatusPaused, LastResultStatus: "ask"}, nil)
	if !strings.Contains(raw, "resume_task_id=bt-9") {
		t.Fatalf("nil ask_user missing resume_task_id: %s", raw)
	}
}

func TestTaskVerifierNilSessionFn(t *testing.T) {
	if _, err := (&TaskVerifier{}).Verify([]CriterionSpec{{Type: "dom_exists"}}); err == nil {
		t.Fatal("expected session error")
	}
	if err := (&TaskVerifier{}).WaitForStable(time.Second); err != nil {
		t.Fatalf("WaitForStable nil sessionFn = %v", err)
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
	result, err := supervisor.doAgentStep(&BrowserAgentSession{}, StepSpec{Action: "type", Params: map[string]string{"text": "Title"}})
	if err == nil && (result == nil || result.Status != "ask") {
		t.Fatal("expected browser session error from nil session")
	}
	if err != nil && strings.Contains(err.Error(), "missing selector/ref") {
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

func TestMarshalActionResultIncludesLastExpect(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/success", Title: "Done"},
		},
	}
	result := &BrowserActionResult{
		SnapshotID: "snap-1",
		Status:     "ok",
		Display:    "clicked",
		Data: map[string]interface{}{
			"url":   "https://example.com/success",
			"title": "Done",
		},
	}
	raw := marshalActionResult(s, result, nil, ExpectSpec{Type: "url_contains", Pattern: "/success"})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("ok=%v raw=%s", payload["ok"], raw)
	}
	data, _ := payload["data"].(map[string]interface{})
	excerpt, _ := data["last_expect"].(map[string]interface{})
	if excerpt["type"] != "url_contains" || excerpt["pattern"] != "/success" {
		t.Fatalf("last_expect=%v", data["last_expect"])
	}
}

func TestAttachLastExpectLedgerOnObserveData(t *testing.T) {
	s := &BrowserAgentSession{lastExpect: ExpectSpec{Type: "text", Pattern: "Welcome"}}
	data := attachLastExpectLedger(s, observeDataFromSnapshot(BrowserSnapshot{URL: "https://example.com"}))
	excerpt, _ := data["last_expect"].(map[string]string)
	if excerpt["type"] != "text" || excerpt["pattern"] != "Welcome" {
		t.Fatalf("last_expect=%v", data["last_expect"])
	}
}

func TestPolicyNavigateReturnsBlocked(t *testing.T) {
	s := &BrowserAgentSession{
		session: &Session{},
		Policy:  BrowserPolicy{BlockedDomains: []string{"evil.com"}},
	}
	got, err := s.Navigate("https://evil.com/phish")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got == nil || got.Status != "blocked" {
		t.Fatalf("got=%#v", got)
	}
	if got.Data["reason"] != "blocked" {
		t.Fatalf("data=%v", got.Data)
	}
	raw := marshalActionResult(s, got, err, ExpectSpec{})
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != false {
		t.Fatalf("ok=%v raw=%s", payload["ok"], raw)
	}
	data, _ := payload["data"].(map[string]interface{})
	if data["reason"] != "blocked" {
		t.Fatalf("data=%v", data)
	}
}

func TestPolicyUploadReturnsBlocked(t *testing.T) {
	s := &BrowserAgentSession{
		session:        &Session{},
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1"},
		},
	}
	got, err := s.SetFilesOn("snap-1", "@e1", "", []string{"a.txt"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got == nil || got.Status != "blocked" {
		t.Fatalf("got=%#v", got)
	}
}

func TestNavigateInvalidURLIsErrorNotBlocked(t *testing.T) {
	s := &BrowserAgentSession{session: &Session{}}
	got, err := s.Navigate("http://[")
	if err == nil {
		t.Fatal("expected invalid url error")
	}
	if got != nil {
		t.Fatalf("invalid url returned result=%#v", got)
	}
	if isPolicyDenied(err) {
		t.Fatalf("invalid url should not be policy denied: %v", err)
	}
}

func TestCurrentDomainFromSessionUsesSnapshot(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {URL: "https://example.com/page"},
		},
		session: &Session{},
	}
	if got := currentDomainFromSession(s); got != "example.com" {
		t.Fatalf("got=%q", got)
	}
}

func TestExecutePausesOnBlockedCSSNavigate(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) {
		return &Session{}, nil
	}, nil)
	state, err := supervisor.Execute(TaskSpec{
		Steps: []StepSpec{{Action: "navigate", Params: map[string]string{"url": "javascript:alert(1)"}}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if state == nil || state.Status != TaskStatusPaused || state.LastResultStatus != "blocked" {
		t.Fatalf("state=%#v", state)
	}
	if state.RetryCount != 0 {
		t.Fatalf("retries=%d", state.RetryCount)
	}
}

func TestExecutePausesOnBlockedNavigate(t *testing.T) {
	s := &BrowserAgentSession{
		session: &Session{},
		Policy:  BrowserPolicy{BlockedDomains: []string{"evil.com"}},
	}
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, nil, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	state, err := supervisor.Execute(TaskSpec{
		Steps: []StepSpec{{Action: "navigate", Params: map[string]string{"url": "https://evil.com/"}}},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if state == nil || state.Status != TaskStatusPaused || state.LastResultStatus != "blocked" {
		t.Fatalf("state=%#v", state)
	}
	if state.RetryCount != 0 {
		t.Fatalf("retries=%d", state.RetryCount)
	}
	raw := marshalTaskRunResult(state, nil)
	if strings.Contains(raw, "__ASK_USER__") {
		t.Fatalf("blocked must not be ask: %s", raw)
	}
	if !strings.Contains(raw, `"reason":"blocked"`) {
		t.Fatalf("got=%s", raw)
	}
}

func TestApplyExpectDoesNotRecordAskOrBlocked(t *testing.T) {
	s := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/done"},
		},
	}
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "ok"}, ExpectSpec{Type: "url_contains", Pattern: "/done"})
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "ask"}, ExpectSpec{Type: "url_contains", Pattern: "/other"})
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "blocked"}, ExpectSpec{Type: "text", Pattern: "nope"})
	if s.lastExpect.Type != "url_contains" || s.lastExpect.Pattern != "/done" {
		t.Fatalf("ask/blocked overwrote last_expect: %#v", s.lastExpect)
	}
}

func TestApplyExpectDoesNotRecordTrivialExpect(t *testing.T) {
	s := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/done"},
		},
	}
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "ok"}, ExpectSpec{Type: "url_contains", Pattern: "/done"})
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "ok"}, ExpectSpec{Type: "url_contains", Pattern: "/"})
	if s.lastExpect.Pattern != "/done" {
		t.Fatalf("trivial expect overwrote last_expect: %#v", s.lastExpect)
	}
}

func TestValidateNavigationPolicyBlocksSchemes(t *testing.T) {
	if err := validateNavigationPolicy(BrowserPolicy{}, "javascript:alert(1)", ""); !isPolicyDenied(err) {
		t.Fatalf("javascript: err=%v", err)
	}
	if err := validateNavigationPolicy(BrowserPolicy{}, "data:text/html,hi", ""); !isPolicyDenied(err) {
		t.Fatalf("data: err=%v", err)
	}
	if err := validateNavigationPolicy(BrowserPolicy{}, "file:///etc/passwd", ""); !isPolicyDenied(err) {
		t.Fatalf("file: err=%v", err)
	}
	if err := validateNavigationPolicy(BrowserPolicy{}, "about:blank", ""); err != nil {
		t.Fatalf("about:blank err=%v", err)
	}
	if err := validateNavigationPolicy(BrowserPolicy{}, "https://example.com/", ""); err != nil {
		t.Fatalf("https err=%v", err)
	}
	if err := validateNavigationPolicy(BrowserPolicy{}, "http://", ""); err == nil || isPolicyDenied(err) {
		t.Fatalf("missing host should be invalid url, got %v", err)
	}
}

func TestOpenURLBlockedReturnsError(t *testing.T) {
	s := &BrowserAgentSession{
		session: &Session{},
		Policy:  BrowserPolicy{BlockedDomains: []string{"evil.com"}},
	}
	err := s.OpenURL("https://evil.com/phish")
	if err == nil {
		t.Fatal("expected blocked start_url error")
	}
	if !strings.Contains(err.Error(), "evil.com") {
		t.Fatalf("err=%v", err)
	}
	if err := s.OpenURL(""); err != nil {
		t.Fatalf("empty url err=%v", err)
	}
}

func TestActionErrorTreatsNonOKStatuses(t *testing.T) {
	if err := actionError(&BrowserActionResult{Status: "ask", Display: "solve captcha"}, nil); err == nil || !strings.Contains(err.Error(), "solve captcha") {
		t.Fatal("ask should be an OpenURL error")
	}
	if err := actionError(&BrowserActionResult{Status: "unchanged", Display: "same page"}, nil); err != nil {
		t.Fatalf("unchanged err=%v", err)
	}
	if err := actionError(&BrowserActionResult{Status: "ok"}, nil); err != nil {
		t.Fatalf("ok err=%v", err)
	}
	if err := actionError(nil, nil); err == nil {
		t.Fatal("nil result should be an error")
	}
	blockedErr := actionError(&BrowserActionResult{Status: "blocked", Display: "browser policy blocked domain: evil.com"}, nil)
	if !isPolicyDenied(blockedErr) {
		t.Fatalf("blocked OpenURL error should be policyDenied: %v", blockedErr)
	}
}

func TestSessionNavigateBlocksSchemesWithoutPanic(t *testing.T) {
	s := &Session{}
	if _, err := s.Navigate("javascript:alert(1)"); err == nil {
		t.Fatal("expected javascript: to be blocked")
	}
	if _, err := s.Navigate("https://example.com/"); err == nil {
		t.Fatal("disconnected session should error instead of panicking")
	}
}

func TestSwitchPageNilSession(t *testing.T) {
	if err := (*Session)(nil).SwitchPage("t1"); err == nil {
		t.Fatal("expected disconnected session error")
	}
	if err := (&Session{}).SwitchPage(""); err == nil {
		t.Fatal("expected missing target id")
	}
}

func TestWaitForStableNilClient(t *testing.T) {
	if err := (&Session{}).WaitForStable(time.Second, 50*time.Millisecond); err == nil {
		t.Fatal("expected disconnected session error")
	}
}

func TestBackNilClient(t *testing.T) {
	if err := (&Session{}).Back(); err == nil {
		t.Fatal("expected disconnected session error")
	}
}

func TestEvalNilClient(t *testing.T) {
	if _, err := (*Session)(nil).Eval("1"); err == nil {
		t.Fatal("expected disconnected session error")
	}
	if _, err := (&Session{}).Eval("1"); err == nil {
		t.Fatal("expected disconnected session error")
	}
}

func TestGetHTMLNilClient(t *testing.T) {
	if _, err := (*Session)(nil).GetHTML(""); err == nil {
		t.Fatal("expected disconnected session error")
	}
	if _, err := (&Session{}).GetHTML(""); err == nil {
		t.Fatal("expected disconnected session error")
	}
}

func TestScreenshotNilClient(t *testing.T) {
	if _, err := (*Session)(nil).Screenshot(false); err == nil {
		t.Fatal("expected disconnected session error")
	}
	if _, err := (&Session{}).Screenshot(false); err == nil {
		t.Fatal("expected disconnected session error")
	}
}

func TestPruneDuplicatePagesNilClient(t *testing.T) {
	if n := (*Session)(nil).PruneDuplicatePages(); n != 0 {
		t.Fatalf("closed=%d", n)
	}
	if n := (&Session{}).PruneDuplicatePages(); n != 0 {
		t.Fatalf("closed=%d", n)
	}
}

func TestWaitForLoadOnNilClient(t *testing.T) {
	if waitForLoadOn(nil, time.Second, 0) != "" {
		t.Fatal("nil client should not wait")
	}
}

func TestObserveAfterActionGenericErrorNotBlocked(t *testing.T) {
	obs, blocked, err := (&BrowserAgentSession{ID: "s1"}).observeAfterAction("browser_click")
	if obs != nil || blocked != nil {
		t.Fatalf("obs=%v blocked=%v", obs, blocked)
	}
	if err == nil {
		t.Fatal("expected disconnected observe error")
	}
	if isPolicyDenied(err) {
		t.Fatalf("generic observe error should not be policy denied: %v", err)
	}
}

func TestClickPopupPolicyDeniedIsBlocked(t *testing.T) {
	s := &BrowserAgentSession{ID: "s1"}
	got, err := policyBlockResult(s, "browser_click", policyDenied("browser policy blocked popup; pass allow_popup=true on session_start"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "blocked" || got.Action != "browser_click" {
		t.Fatalf("got=%#v", got)
	}
	raw := marshalActionResult(s, got, err, ExpectSpec{})
	if strings.Contains(raw, `"ok":true`) || strings.Contains(raw, "ASK_USER") {
		t.Fatalf("blocked click should be compact ok=false: %s", raw)
	}
}
