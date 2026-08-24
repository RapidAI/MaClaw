package browser

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func widgetSession(url string) *BrowserAgentSession {
	return &BrowserAgentSession{
		ID:             "sess-widget",
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				SnapshotID: "snap-1",
				URL:        url,
				Title:      "challenge",
				PageFlags:  BrowserPageFlags{CaptchaWidget: true},
			},
		},
	}
}

func TestCaptchaWidgetFromSignals(t *testing.T) {
	if !captchaWidgetFromSignals([]string{"https://www.google.com/recaptcha/api2/anchor"}, "", false) {
		t.Fatal("recaptcha iframe should be widget")
	}
	if !captchaWidgetFromSignals(nil, "请拖动滑块完成验证", false) {
		t.Fatal("slider text should be widget")
	}
	if captchaWidgetFromSignals(nil, "请输入验证码", true) {
		t.Fatal("class*=captcha plus OTP text must not be widget")
	}
	if captchaWidgetFromSignals(nil, "captcha-passed badge", true) {
		t.Fatal("class*=captcha alone must not be widget")
	}
	if !captchaWidgetFromSignals([]string{"https://cdn.example/captcha/frame.html"}, "", true) {
		t.Fatal("generic captcha iframe plus class should be widget")
	}
}

func TestShouldUseVisionSkipsCaptchaWidget(t *testing.T) {
	if shouldUseVision(BrowserPageFlags{CaptchaWidget: true, Captcha: true}, nil) {
		t.Fatal("captcha widget must not burn vision")
	}
	refs := []BrowserElementRef{{Ref: "@e1"}, {Ref: "@e2"}, {Ref: "@e3"}}
	if shouldUseVision(BrowserPageFlags{Captcha: true}, refs) {
		t.Fatal("text captcha flag must not force vision")
	}
	if !shouldUseVision(BrowserPageFlags{Canvas: true}, []BrowserElementRef{{Ref: "@e1"}}) {
		t.Fatal("canvas should still use vision")
	}
}

func TestClickAsksBeforeMutateWhenCaptchaWidget(t *testing.T) {
	s := widgetSession("http://127.0.0.1/fixture/captcha")
	got, err := s.Click("snap-1", "@e1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "ask" || got.AskUser == nil {
		t.Fatalf("got=%#v", got)
	}
	raw := marshalActionResult(s, got, nil, ExpectSpec{})
	if _, ok := agent.ParseAskUserResult(raw); !ok {
		t.Fatalf("click marshal = %q", raw)
	}
}

func TestOTPTypeDoesNotAskWithoutWidget(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", PageFlags: BrowserPageFlags{MFA: true, Captcha: true}},
		},
	}
	got, err := s.Type("snap-1", "@e1", "", "123456")
	if got != nil && got.Status == "ask" {
		t.Fatal("OTP type must not AskUser")
	}
	if err == nil {
		t.Fatal("expected session error after skipping captcha ask")
	}
}

func TestPressAndDialogAskOnWidgetPage(t *testing.T) {
	s := widgetSession("http://127.0.0.1/fixture/captcha")
	press, err := s.Press("Enter")
	if err != nil || press == nil || press.Status != "ask" {
		t.Fatalf("press=%#v err=%v", press, err)
	}
	dlg, err := s.HandleDialog(true, "")
	if err != nil || dlg == nil || dlg.Status != "ask" {
		t.Fatalf("dialog=%#v err=%v", dlg, err)
	}
}

func TestNavigateAllowedOnWidgetPage(t *testing.T) {
	if isCaptchaMutatingAction("navigate") || isCaptchaMutatingAction("hover") || isCaptchaMutatingAction("observe") {
		t.Fatal("navigate/hover/observe must be allowed on captcha pages")
	}
}

func TestDoAgentStepReturnsAskWithoutRetry(t *testing.T) {
	s := widgetSession("http://127.0.0.1/fixture/captcha")
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	result, err := supervisor.doAgentStep(s, StepSpec{Action: "click", Params: map[string]string{"ref": "@e1"}})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != "ask" {
		t.Fatalf("result=%#v", result)
	}
	state, err := supervisor.Execute(TaskSpec{Steps: []StepSpec{{Action: "click", Params: map[string]string{"ref": "@e1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != TaskStatusPaused || state.RetryCount != 0 || state.AskUser == nil {
		t.Fatalf("state=%#v", state)
	}
	raw := marshalTaskRunResult(state, nil)
	if _, ok := agent.ParseAskUserResult(raw); !ok {
		t.Fatalf("task_run marshal = %q", raw)
	}
	if strings.Contains(raw, `"status":"paused"`) {
		t.Fatalf("must not wrap ask as paused json: %s", raw)
	}
}

func TestDiscardAllClearsPausedTask(t *testing.T) {
	s := widgetSession("http://127.0.0.1/fixture/captcha")
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	state, err := supervisor.Execute(TaskSpec{Steps: []StepSpec{{Action: "click", Params: map[string]string{"ref": "@e1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := supervisor.GetState(state.ID); !ok {
		t.Fatal("paused task should remain until recycle")
	}
	supervisor.DiscardAll()
	if _, ok := supervisor.GetState(state.ID); ok {
		t.Fatal("DiscardAll should drop paused task")
	}
}

func TestGoalClassMissingExpect(t *testing.T) {
	s := &BrowserAgentSession{}
	result := &BrowserActionResult{Status: "ok", GoalClass: true, Display: "clicked", Detail: "submit"}
	got := s.applyGoalClassContract(result, ExpectSpec{})
	if got.Status != "expect_failed" || got.Data["reason"] != "missing_expect" {
		t.Fatalf("got=%#v", got)
	}
	if strings.Contains(got.Display, "computer_") {
		t.Fatalf("must not suggest computer_*: %s", got.Display)
	}
	got = s.applyGoalClassContract(&BrowserActionResult{Status: "ok", GoalClass: true, Display: "clicked", Detail: "submit"}, ExpectSpec{})
	if !strings.Contains(got.Display, "Do not use computer_*") {
		t.Fatalf("second missing_expect should nudge once: %s", got.Display)
	}
	link := s.applyGoalClassContract(&BrowserActionResult{Status: "ok", GoalClass: false}, ExpectSpec{})
	if link.Status != "ok" {
		t.Fatalf("link click without expect should stay ok, got %s", link.Status)
	}
}

func TestTrivialExpectRejected(t *testing.T) {
	if validExpect(ExpectSpec{Type: "url_contains", Pattern: "/"}) {
		t.Fatal("url_contains:/ must be rejected")
	}
	if validExpect(ExpectSpec{Type: "url_contains", Pattern: "http"}) {
		t.Fatal("url_contains:http must be rejected")
	}
	s := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/"}},
	}
	got := s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "unchanged", Display: "clicked"}, ExpectSpec{Type: "url_contains", Pattern: "/"})
	if got.Status != "expect_failed" {
		t.Fatalf("trivial expect upgraded? status=%s", got.Status)
	}
}

func TestAskTakesPriorityOverMissingExpect(t *testing.T) {
	s := &BrowserAgentSession{}
	got := s.applyGoalClassContract(&BrowserActionResult{Status: "ask", GoalClass: true, AskUser: &agent.AskUserRequest{Question: "q"}}, ExpectSpec{})
	if got.Status != "ask" {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestMarshalVerifyResultOmitsSelector(t *testing.T) {
	raw := marshalVerifyResult(&VerifyResult{
		Passed: false,
		Details: []CriterionResult{{
			Criterion: CriterionSpec{Type: "dom_exists", Selector: "#secret"},
			Error:     "not found",
		}},
	})
	if strings.Contains(raw, "#secret") {
		t.Fatalf("compact verify leaked selector: %s", raw)
	}
	if !strings.Contains(raw, `"ok":false`) {
		t.Fatalf("raw=%s", raw)
	}
	got := (&TaskVerifier{}).checkDOMExists(nil, CriterionSpec{Type: "dom_exists", Selector: "#secret"})
	if strings.Contains(got.Error, "#secret") {
		t.Fatalf("dom_exists error leaked selector: %s", got.Error)
	}
}

func TestPlaybookDoesNotSuggestComputerOnMissingExpect(t *testing.T) {
	p := Playbook()
	if !strings.Contains(p, "never use computer_*") {
		t.Fatal(p)
	}
}

func TestPageFlagsScriptIsFlagsOnly(t *testing.T) {
	if strings.Contains(browserPageFlagsScript, "selectorCandidatesFor") {
		t.Fatal("flags peek must not run SoM selectorCandidatesFor")
	}
	if strings.Contains(browserPageFlagsScript, "collectFromRoot") {
		t.Fatal("flags peek must not collect refs")
	}
	for _, marker := range []string{"recaptcha", "hcaptcha", "funcaptcha", "拖动滑块", "captcha_widget"} {
		if !strings.Contains(browserPageFlagsCollectJS, marker) {
			t.Fatalf("shared flags collect JS missing %q", marker)
		}
		if !strings.Contains(browserPageFlagsScript, marker) {
			t.Fatalf("flags script missing %q", marker)
		}
		if !strings.Contains(browserObserveScript, marker) {
			t.Fatalf("observe script missing shared flag marker %q", marker)
		}
	}
}

func TestCaptchaAskTrustsLastSnapshotFalse(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", PageFlags: BrowserPageFlags{CaptchaWidget: false}},
		},
	}
	if got := s.captchaAskIfNeeded("click"); got != nil {
		t.Fatalf("trusted false snapshot still asked: %#v", got)
	}
}

func TestCaptchaAskDoesNotFakeAskWithoutSession(t *testing.T) {
	s := &BrowserAgentSession{}
	if got := s.captchaAskIfNeeded("click"); got != nil {
		t.Fatalf("disconnected session without snapshot must not fake captcha ask: %#v", got)
	}
}

func TestExecuteWithoutAgentSessionUsesDoStep(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) {
		return nil, fmt.Errorf("no cdp")
	}, nil)
	state, err := supervisor.Execute(TaskSpec{Steps: []StepSpec{{Action: "click", Params: map[string]string{"selector": "a"}}}})
	if err == nil {
		t.Fatal("expected session error")
	}
	msg := err.Error()
	if state != nil && state.LastError != "" {
		msg = state.LastError
	}
	if strings.Contains(msg, "CSS doStep is disabled") {
		t.Fatalf("replay/legacy path must still use doStep: %s", msg)
	}
	if !strings.Contains(msg, "no cdp") {
		t.Fatalf("err=%v state=%#v", err, state)
	}
}

func TestApplyGoalClassContractNilSession(t *testing.T) {
	var s *BrowserAgentSession
	got := s.applyGoalClassContract(&BrowserActionResult{Status: "ok", GoalClass: true, Display: "clicked"}, ExpectSpec{})
	if got == nil || got.Status != "expect_failed" {
		t.Fatalf("got=%#v", got)
	}
}

func TestRememberSubmitClickAfterExpect(t *testing.T) {
	s := &BrowserAgentSession{recentSubmitClicks: map[string]time.Time{}}
	missing := &BrowserActionResult{
		Status:            "ok",
		GoalClass:         true,
		Display:           "clicked",
		Detail:            "submit",
		submitRememberKey: "page|submit",
	}
	raw := marshalActionResult(s, missing, nil, ExpectSpec{})
	if strings.Contains(raw, `"ok":true`) {
		t.Fatalf("missing expect should not be ok: %s", raw)
	}
	s.mu.Lock()
	_, armed := s.recentSubmitClicks["page|submit"]
	s.mu.Unlock()
	if armed {
		t.Fatal("missing_expect must not arm submit window")
	}

	s.snapshots = map[string]*BrowserSnapshot{
		"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/success"},
	}
	okResult := &BrowserActionResult{
		Status:            "ok",
		GoalClass:         true,
		SnapshotID:        "snap-1",
		Display:           "clicked",
		submitRememberKey: "page|submit",
	}
	raw = marshalActionResult(s, okResult, nil, ExpectSpec{Type: "url_contains", Pattern: "/success"})
	if !strings.Contains(raw, `"ok":true`) {
		t.Fatalf("valid expect should be ok: %s", raw)
	}
	s.mu.Lock()
	_, armed = s.recentSubmitClicks["page|submit"]
	s.mu.Unlock()
	if !armed {
		t.Fatal("ok submit with valid expect should arm window")
	}
}

func TestResumeSpecRequiresPaused(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, fmt.Errorf("no cdp") }, nil)
	state, err := supervisor.Execute(TaskSpec{Steps: []StepSpec{{Action: "click", Params: map[string]string{"selector": "a"}}}})
	if err == nil {
		t.Fatal("expected failed task")
	}
	if _, _, ok := supervisor.ResumeSpec(state.ID); ok {
		t.Fatal("failed/completed task must not resume")
	}
}

func TestLastExpectIgnoresEmptyObserve(t *testing.T) {
	s := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/done"},
		},
	}
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "ok"}, ExpectSpec{Type: "url_contains", Pattern: "/done"})
	s.applyExpect(&BrowserActionResult{SnapshotID: "snap-1", Status: "ok"}, ExpectSpec{})
	s.mu.RLock()
	got := s.lastExpect
	s.mu.RUnlock()
	if got.Type != "url_contains" || got.Pattern != "/done" {
		t.Fatalf("empty expect wiped ledger: %#v", got)
	}
}

func TestGoalClassUnchangedDoesNotUpgradeToOK(t *testing.T) {
	s := &BrowserAgentSession{
		recentSubmitClicks: map[string]time.Time{},
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {SnapshotID: "snap-1", URL: "https://example.com/success"},
		},
	}
	result := &BrowserActionResult{
		SnapshotID:        "snap-1",
		Status:            "unchanged",
		GoalClass:         true,
		Display:           "clicked",
		submitRememberKey: "page|submit",
	}
	raw := marshalActionResult(s, result, nil, ExpectSpec{Type: "url_contains", Pattern: "/success"})
	if strings.Contains(raw, `"ok":true`) {
		t.Fatalf("goal-class unchanged must not become ok: %s", raw)
	}
	s.mu.Lock()
	_, armed := s.recentSubmitClicks["page|submit"]
	s.mu.Unlock()
	if armed {
		t.Fatal("goal-class unchanged must not arm submit window")
	}
}
