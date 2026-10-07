package browser

import (
	"strings"
	"testing"
	"time"
)

func TestProbeScriptSkipsFullPageText(t *testing.T) {
	if !strings.Contains(browserObserveScript, "const __maclawProbe = false;") {
		t.Fatal("observe script missing probe flag")
	}
	if !strings.Contains(browserProbeScript, "const __maclawProbe = true;") {
		t.Fatal("probe script did not enable probe mode")
	}
	if !strings.Contains(browserProbeScript, "document.body.innerText") {
		t.Fatal("probe pageText should keep a short main-document sample for flags")
	}
	if !strings.Contains(browserObserveScript, "function pageText()") {
		t.Fatal("full pageText walk must remain on the observe script")
	}
}

func TestProbeSnapshotOmitsPageText(t *testing.T) {
	data := observeDataFromSnapshot(BrowserSnapshot{
		Probe:      true,
		SnapshotID: "snap-probe",
		URL:        "https://example.com/form",
		Refs: []BrowserElementRef{{
			Ref:   "@e1",
			Role:  "textbox",
			Name:  "Email",
			Tag:   "input",
			Value: "a@b.c",
		}},
	})
	if _, ok := data["page_text_excerpt"]; ok {
		t.Fatalf("probe data leaked page text: %#v", data)
	}
	if _, ok := data["console_summary"]; ok {
		t.Fatal("probe data leaked console")
	}
	if data["probe"] != true {
		t.Fatalf("probe flag = %#v", data["probe"])
	}
	refs, _ := data["refs"].([]CompactElementRef)
	if len(refs) != 1 || refs[0].Value != "a@b.c" {
		t.Fatalf("probe refs = %#v", data["refs"])
	}
}

func TestViewChangedIgnoresCounts(t *testing.T) {
	if viewChanged("Inbox (3)", "Inbox (4)", "Orders", "Orders") {
		t.Fatal("a changing count is not a new view")
	}
	if !viewChanged("Settings", "Inbox", "", "") {
		t.Fatal("a new title is a view change when there is no heading")
	}
	if viewChanged("Inbox", "(1) Inbox", "Orders", "Orders") {
		t.Fatal("a title badge is not a view change while the heading is stable")
	}
	if viewChanged("Inbox (3)", "Inbox", "", "") {
		t.Fatal("adding a count to the title is not a view change")
	}
	if !viewChanged("Inbox", "Inbox", "Orders", "Settings") {
		t.Fatal("a new heading is a view change")
	}
	if viewChanged("", "Inbox", "", "Settings") {
		t.Fatal("a title that was missing at the start is not a view change")
	}
}

func TestSameDocumentURLIgnoresDefaultPortAndHostCase(t *testing.T) {
	if !sameDocumentURL("https://EX.com:443/a#section", "https://ex.com/a") {
		t.Fatal("default port, host case, and hash are the same document")
	}
	if !sameDocumentURL("http://ex.com:80", "http://ex.com/") {
		t.Fatal("origin with and without a slash are the same document")
	}
	if !sameDocumentURL("https://ex.com/a", "https://ex.com/a/") || !sameDocumentURL("https://ex.com/a", "https://ex.com/a///") {
		t.Fatal("trailing slashes are the same document")
	}
	if sameDocumentURL("https://ex.com/a?x=1", "https://ex.com/a?x=2") {
		t.Fatal("query changes are a different URL")
	}
}

func TestStepBudgetOutlivesWait(t *testing.T) {
	step := StepSpec{Action: "wait", Timeout: 10 * time.Second, Params: map[string]string{"duration_ms": "10000"}}
	if got := stepBudget(step, 30*time.Second); got != 12*time.Second {
		t.Fatalf("budget = %s", got)
	}
	click := StepSpec{Action: "click", Timeout: 10 * time.Second}
	if got := stepBudget(click, 30*time.Second); got != 10*time.Second {
		t.Fatalf("click budget = %s", got)
	}
	if got := stepBudget(StepSpec{Action: "wait"}, 30*time.Second); got != 30*time.Second {
		t.Fatalf("default wait budget = %s", got)
	}
}

func TestParseTaskStepsAcceptsOneObject(t *testing.T) {
	steps, err := parseTaskSteps(`{"action":"type","ref":"@e1","value":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Action != "type" || steps[0].Params["ref"] != "@e1" || steps[0].Params["value"] != "hello" {
		t.Fatalf("steps = %#v", steps)
	}
	steps, err = parseTaskSteps(`[{"action":"click","params":{"ref":"@e2"}}]`)
	if err != nil || len(steps) != 1 || steps[0].Params["ref"] != "@e2" {
		t.Fatalf("array steps = %#v err=%v", steps, err)
	}
	if _, err = parseTaskSteps(`{"steps":[{"action":"click"}]}`); err == nil {
		t.Fatal("a wrapper object is not one step")
	}
	steps, err = parseTaskSteps(`{"action":"click","ref":"@e1","timeout":10}`)
	if err != nil || steps[0].Timeout != 10*time.Second {
		t.Fatalf("timeout 10 = %s err=%v", steps[0].Timeout, err)
	}
	steps, err = parseTaskSteps(`[{"action":"wait","timeout":5000}]`)
	if err != nil || steps[0].Timeout != 5*time.Second {
		t.Fatalf("timeout 5000 = %s err=%v", steps[0].Timeout, err)
	}
	steps, err = parseTaskSteps(`[{"action":"click","timeout":"30s"}]`)
	if err != nil || steps[0].Timeout != 30*time.Second {
		t.Fatalf("timeout 30s = %s err=%v", steps[0].Timeout, err)
	}
	steps, err = parseTaskSteps(`{"action":"wait","timeout":3}`)
	if err != nil || steps[0].Timeout != 3*time.Second || steps[0].Params["duration_ms"] != "3000" {
		t.Fatalf("wait timeout = %s params=%#v err=%v", steps[0].Timeout, steps[0].Params, err)
	}
	steps, err = parseTaskSteps(`{"action":"wait","timeout":10,"duration_ms":500}`)
	if err != nil || steps[0].Params["duration_ms"] != "500" {
		t.Fatalf("explicit wait = %#v err=%v", steps[0].Params, err)
	}
	steps, err = parseTaskSteps(`{"action":"scroll","params":{"dy":-240,"dx":15}}`)
	if err != nil || steps[0].Params["dy"] != "-240" || steps[0].Params["dx"] != "15" {
		t.Fatalf("numeric params = %#v err=%v", steps[0].Params, err)
	}
	steps, err = parseTaskSteps(`{"action":"dialog","params":{"accept":false}}`)
	if err != nil || steps[0].Params["accept"] != "false" {
		t.Fatalf("bool param = %#v err=%v", steps[0].Params, err)
	}
	steps, err = parseTaskSteps(`{"action":"set_files","params":{"files":["a.png","report, final.png"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	files := splitStepList(steps[0].Params["files"])
	if len(files) != 2 || files[0] != "a.png" || files[1] != "report, final.png" {
		t.Fatalf("file list = %#v parsed=%#v", steps[0].Params, files)
	}
	steps, err = parseTaskSteps(`{"action":"set_files","files":["c.png"]}`)
	if err != nil {
		t.Fatal(err)
	}
	files = splitStepList(steps[0].Params["files"])
	if len(files) != 1 || files[0] != "c.png" {
		t.Fatalf("top-level file list = %#v parsed=%#v", steps[0].Params, files)
	}
	if got := splitStepList("a.png, b.png"); len(got) != 2 || got[1] != "b.png" {
		t.Fatalf("comma list = %#v", got)
	}
	if got := firstStepParam(map[string]string{"file": "a.png"}, "files", "file", "path", "paths"); got != "a.png" {
		t.Fatalf("file alias = %q", got)
	}
}

func TestFirstStepParamAcceptsTypeAndPressAliases(t *testing.T) {
	if got := firstStepParam(map[string]string{"value": "hello"}, "text", "value"); got != "hello" {
		t.Fatalf("type alias = %q", got)
	}
	if got := firstStepParam(map[string]string{"text": "keep", "value": "drop"}, "text", "value"); got != "keep" {
		t.Fatalf("explicit text = %q", got)
	}
	if got := firstStepParam(map[string]string{"text": "Enter"}, "key", "text"); got != "Enter" {
		t.Fatalf("press alias = %q", got)
	}
	if got := firstStepParam(nil, "text"); got != "" {
		t.Fatalf("nil params = %q", got)
	}
}

func TestStepScrollDeltaAcceptsShortNames(t *testing.T) {
	dx, dy := stepScrollDelta(map[string]string{"dy": "-240"})
	if dx != 0 || dy != -240 {
		t.Fatalf("dy alias = %d,%d", dx, dy)
	}
	dx, dy = stepScrollDelta(map[string]string{"delta_y": "10", "dy": "99", "dx": "3"})
	if dx != 3 || dy != 10 {
		t.Fatalf("explicit delta = %d,%d", dx, dy)
	}
	if _, dy = stepScrollDelta(nil); dy != 500 {
		t.Fatalf("default dy = %d", dy)
	}
	if got := firstStepParam(map[string]string{"label": "Open"}, "value", "label", "text"); got != "Open" {
		t.Fatalf("select alias = %q", got)
	}
}

func TestDoAgentStepAcceptsActionCase(t *testing.T) {
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, nil, nil)
	_, err := supervisor.doAgentStep(&BrowserAgentSession{}, StepSpec{Action: " Click "})
	if err == nil || strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("err = %v", err)
	}
}

func TestBatchURLWatchDecision(t *testing.T) {
	changed, done := batchURLWatchDecision("https://ex/a", "https://ex/a#section", "complete", false, 0, batchLinkURLWait, batchURLUnchangedSettle)
	if changed || done {
		t.Fatal("hash-only updates stay on the same document")
	}
	changed, done = batchURLWatchDecision("https://ex/a", "https://ex/b", "loading", false, 0, batchLinkURLWait, batchURLUnchangedSettle)
	if !changed || !done {
		t.Fatal("url change should stop immediately")
	}
	changed, done = batchURLWatchDecision("https://ex/a", "https://ex/a", "complete", false, 50*time.Millisecond, batchLinkURLWait, batchURLUnchangedSettle)
	if changed || done {
		t.Fatal("a still-settling complete document should keep polling")
	}
	changed, done = batchURLWatchDecision("https://ex/a", "https://ex/a", "complete", false, batchURLUnchangedSettle, batchLinkURLWait, batchURLUnchangedSettle)
	if changed || !done {
		t.Fatal("unchanged complete document should release the batch")
	}
	changed, done = batchURLWatchDecision("https://ex/a", "https://ex/a", "loading", false, 500*time.Millisecond, batchLinkURLWait, batchURLUnchangedSettle)
	if changed || done {
		t.Fatal("loading document should keep waiting for the navigation")
	}
	changed, done = batchURLWatchDecision("https://ex/a", "https://ex/a", "loading", false, batchLinkURLWait, batchLinkURLWait, batchURLUnchangedSettle)
	if changed || !done {
		t.Fatal("loading document should give up at the cap")
	}
	changed, done = batchURLWatchDecision("https://ex/a", "https://ex/a", "loading", true, 0, batchLinkURLWait, 0)
	if changed || !done {
		t.Fatal("a page that was already loading should not stall a same-page click")
	}
}

func TestBatchURLWaitCoversSlowNavigation(t *testing.T) {
	if batchLinkURLWait < time.Second {
		t.Fatalf("link url wait = %s, want at least 1s so a click can navigate", batchLinkURLWait)
	}
	if batchURLPollInterval > 100*time.Millisecond {
		t.Fatalf("poll = %s, url changes should be noticed quickly", batchURLPollInterval)
	}
	link := &BrowserElementRef{Role: "link", Tag: "a"}
	if wait, settle := batchClickURLWatch(link, ""); wait != batchLinkURLWait || settle != batchURLUnchangedSettle {
		t.Fatalf("link watch = %s %s", wait, settle)
	}
	button := &BrowserElementRef{Role: "button", Tag: "button"}
	if wait, settle := batchClickURLWatch(button, ""); wait != batchLinkURLWait || settle != batchURLPollInterval {
		t.Fatalf("button watch = %s %s", wait, settle)
	}
	if _, settle := batchClickURLWatch(nil, "a.nav"); settle != batchURLUnchangedSettle {
		t.Fatal("anchor selectors should keep the link settle")
	}
	for _, sel := range []string{"article", "aside .card", "address", "[aria-label=Save]", "role=linkedin"} {
		if refMayNavigate(nil, sel) {
			t.Fatalf("%q must not be treated as a link", sel)
		}
	}
	if !refMayNavigate(nil, `[role="link"]`) {
		t.Fatal("role=link selectors should watch the URL")
	}
	changed, done := batchURLWatchDecision("https://ex/a", "https://ex/a", "complete", false, 0, batchLinkURLWait, 0)
	if changed || !done {
		t.Fatal("a stable button click should release on the first sample")
	}
}

func TestWatchBatchURLStopsWhenTargetGone(t *testing.T) {
	gone := make(chan struct{})
	close(gone)
	s := &BrowserAgentSession{
		fastBatch:    true,
		fastBatchURL: "https://ex/a",
		targetGoneCh: gone,
		session:      &Session{},
	}
	stopped, why := s.watchBatchURL(2*time.Second, 0)
	if !stopped || why != "target_gone" {
		t.Fatalf("destroyed target stop=%v reason=%q", stopped, why)
	}
}

func TestMarshalStoppedBatchIsNotCompleted(t *testing.T) {
	raw := marshalTaskRunResult(&TaskState{
		ID:            "bt-1",
		Status:        TaskStatusStopped,
		CurrentStep:   2,
		TotalSteps:    5,
		StoppedReason: "url_changed",
		Observation:   "probed page",
	}, nil)
	if !strings.Contains(raw, `"status":"stopped"`) || !strings.Contains(raw, `"stopped_reason":"url_changed"`) {
		t.Fatalf("raw=%s", raw)
	}
	if strings.Contains(raw, `"status":"completed"`) {
		t.Fatalf("partial batch must not look finished: %s", raw)
	}
}

func TestFollowActionStopsWhenObserveFailsAfterTargetGone(t *testing.T) {
	gone := make(chan struct{})
	close(gone)
	s := &BrowserAgentSession{
		fastBatch:    true,
		fastBatchURL: "https://ex/a",
		targetGoneCh: gone,
		session:      &Session{},
	}
	followed := s.followAction("browser_click", time.Second, time.Millisecond, false, time.Second, 0, "")
	if followed.err != nil || !followed.stop || followed.reason != "target_gone" || followed.observeErr == "" {
		t.Fatalf("follow=%#v", followed)
	}
	result := followed.stamp(s.completeAction("browser_click", "clicked @e1", "@e1", nil, nil, true))
	if result == nil || !result.batchStop || result.batchStopReason != "target_gone" || result.batchObserveErr == "" || result.Status != "ok" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFollowActionStopsWhenSubmitObserveFails(t *testing.T) {
	gone := make(chan struct{})
	close(gone)
	s := &BrowserAgentSession{
		fastBatch:    true,
		fastBatchURL: "https://ex/form",
		targetGoneCh: gone,
		session:      &Session{},
	}
	followed := s.followAction("browser_click", time.Second, time.Millisecond, true, 0, 0, "submit")
	if followed.err != nil || !followed.stop || followed.reason != "submit" || followed.observeErr == "" {
		t.Fatalf("follow=%#v", followed)
	}
	single := &BrowserAgentSession{targetGoneCh: gone, session: &Session{}}
	followed = single.followAction("browser_click", time.Second, time.Millisecond, true, 0, 0, "submit")
	if followed.err == nil || followed.stop {
		t.Fatalf("single-step read failure=%#v", followed)
	}
}

func TestFastBatchUnchangedDisplayDoesNotSayRetry(t *testing.T) {
	snap := BrowserSnapshot{URL: "https://ex/form", Title: "Form", SnapshotID: "s"}
	s := &BrowserAgentSession{fastBatch: true, lastFingerprint: snapshotFingerprint(snap)}
	result := s.completeAction("browser_click", "clicked submit", "submit", &BrowserObservation{Snapshot: snap}, nil, true)
	if result.Status != "unchanged" {
		t.Fatalf("status %s", result.Status)
	}
	if strings.Contains(result.Display, "retrying") || !strings.Contains(result.Display, "probe before another submit") {
		t.Fatalf("display %s", result.Display)
	}
}

func TestUnchangedSubmitIsRemembered(t *testing.T) {
	s := &BrowserAgentSession{}
	key := "https://ex/form|发布"
	s.rememberSubmitClickIfOK(key, &BrowserActionResult{Status: "unchanged"})
	if err := s.guardSubmitClick(key); err == nil {
		t.Fatal("unchanged submit should block an immediate second click")
	}
	if err := s.guardSubmitClick(key); err != nil && !strings.Contains(err.Error(), "probe the page") {
		t.Fatalf("guard error = %v", err)
	}
}

func TestUnchangedSubmitInBatchDoesNotFail(t *testing.T) {
	err := stepOutcomeFailure(stepOutcome{result: &BrowserActionResult{
		GoalClass: true,
		Status:    "unchanged",
		batchStop: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = stepOutcomeFailure(stepOutcome{result: &BrowserActionResult{
		GoalClass: true,
		Status:    "unchanged",
	}})
	if err == nil {
		t.Fatal("single-step unchanged submit should still fail")
	}
}

func TestFastBatchFinalObserveFailureStops(t *testing.T) {
	s := &BrowserAgentSession{}
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, nil, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	state, err := supervisor.Execute(TaskSpec{FastBatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != TaskStatusStopped || state.StoppedReason != "observe_failed" {
		t.Fatalf("state=%#v", state)
	}
	raw := marshalTaskRunResult(state, nil)
	if strings.Contains(raw, `"status":"completed"`) || !strings.Contains(raw, `"stopped_reason":"observe_failed"`) {
		t.Fatalf("raw=%s", raw)
	}
}

func TestBindLatestSnapshotRefsSkipsUnknown(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap",
		snapshots: map[string]*BrowserSnapshot{
			"snap": {SnapshotID: "snap", Refs: []BrowserElementRef{{Ref: "@e1", Selector: "#ok"}}},
		},
	}
	steps := []StepSpec{
		{Action: "click", Params: map[string]string{"ref": "@e1"}},
		{Action: "click", Params: map[string]string{"ref": "@e404"}},
		{Action: "click", Params: map[string]string{"ref": "@e1", "snapshot_id": "other"}},
	}
	s.bindLatestSnapshotRefs(steps)
	if steps[0].Params["snapshot_id"] != "snap" {
		t.Fatalf("known ref snapshot_id = %q", steps[0].Params["snapshot_id"])
	}
	if steps[1].Params["snapshot_id"] != "" {
		t.Fatalf("unknown ref should stay unbound: %#v", steps[1].Params)
	}
	if steps[2].Params["snapshot_id"] != "other" {
		t.Fatalf("explicit snapshot_id changed: %q", steps[2].Params["snapshot_id"])
	}
}

func TestFastBatchSkipsPerStepObserve(t *testing.T) {
	s := &BrowserAgentSession{}
	s.beginFastBatch()
	defer s.endFastBatch()
	followed := s.followAction("browser_type", 1, 1, false, 0, 0, "")
	if followed.err != nil || followed.obs != nil || followed.stop || followed.blocked != nil {
		t.Fatalf("batch type should not observe: %#v", followed)
	}
	single := (&BrowserAgentSession{}).followAction("browser_type", 1, 1, false, 0, 0, "")
	if single.err == nil {
		t.Fatal("single-step type must still observe")
	}
}

func TestPreflightBatchRefsFailsBeforeAction(t *testing.T) {
	s := &BrowserAgentSession{
		snapshots: map[string]*BrowserSnapshot{
			"snap": {SnapshotID: "snap", Refs: []BrowserElementRef{{Ref: "@e1", Selector: "#ok"}}},
		},
	}
	if err := s.preflightBatchRefs([]StepSpec{
		{Action: "type", Params: map[string]string{"text": "hi"}},
		{Action: "click", Params: map[string]string{"snapshot_id": "snap", "ref": "@e1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.preflightBatchRefs([]StepSpec{
		{Action: "click", Params: map[string]string{"snapshot_id": "snap", "ref": "@e404"}},
		{Action: "click", Params: map[string]string{"snapshot_id": "snap", "ref": "@e1"}},
	}); err == nil {
		t.Fatal("missing ref should fail preflight")
	}
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, nil, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	state, err := supervisor.Execute(TaskSpec{
		FastBatch: true,
		Steps: []StepSpec{
			{Action: "click", Params: map[string]string{"snapshot_id": "snap", "ref": "@e404"}},
			{Action: "click", Params: map[string]string{"snapshot_id": "snap", "ref": "@e1"}},
		},
	})
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if state == nil || state.Status != TaskStatusFailed || state.CurrentStep != 0 {
		t.Fatalf("state=%#v", state)
	}
	if state.RetryCount != 0 {
		t.Fatalf("retries=%d", state.RetryCount)
	}
}

func TestFastBatchStopsBeforeTypingOnALoginPage(t *testing.T) {
	peeked := 0
	s := &BrowserAgentSession{ID: "login", peekFlags: func() (BrowserPageFlags, error) {
		peeked++
		return BrowserPageFlags{LoginWall: true}, nil
	}}
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	state, err := supervisor.Execute(TaskSpec{
		FastBatch:      true,
		PauseForPerson: true,
		Steps: []StepSpec{
			{Action: "type", Params: map[string]string{"text": "secret", "ref": "e1"}},
			{Action: "press", Params: map[string]string{"key": "Enter"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != TaskStatusPaused || state.CurrentStep != 1 || !strings.Contains(state.Observation, "page flags: login_wall") {
		t.Fatalf("state=%#v", state)
	}
	if peeked == 0 {
		t.Fatal("login page was not checked")
	}
	plain := &BrowserAgentSession{ID: "plain", peekFlags: func() (BrowserPageFlags, error) {
		t.Fatal("local batch must not stop for a login page")
		return BrowserPageFlags{}, nil
	}}
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return plain, nil }
	skipped, err := supervisor.Execute(TaskSpec{
		FastBatch: true,
		Steps:     []StepSpec{{Action: "type", Params: map[string]string{"text": "secret", "ref": "e1"}}},
	})
	if err == nil || skipped == nil || skipped.Status == TaskStatusPaused {
		t.Fatalf("local batch paused: err=%v state=%#v", err, skipped)
	}
}

func TestFastBatchCaptchaStillAsks(t *testing.T) {
	s := widgetSession("http://127.0.0.1/fixture/captcha")
	supervisor := NewBrowserTaskSupervisor(nil, nil, nil, func() (*Session, error) { return nil, nil }, nil)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) { return s, nil }
	state, err := supervisor.Execute(TaskSpec{
		FastBatch: true,
		Steps:     []StepSpec{{Action: "click", Params: map[string]string{"ref": "@e1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != TaskStatusPaused || state.AskUser == nil || state.RetryCount != 0 {
		t.Fatalf("state=%#v", state)
	}
}

func TestPlaybookPrefersProbeThenBatch(t *testing.T) {
	p := Playbook()
	for _, marker := range []string{`action="probe"`, `action="task_run"`, "snapshot_id", "one observation"} {
		if !strings.Contains(p, marker) {
			t.Fatalf("playbook missing %q", marker)
		}
	}
}
