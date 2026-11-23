package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type challengeCDP struct {
	srv *httptest.Server

	mu          sync.Mutex
	identity    documentIdentity
	states      []PageReadyState
	history     []string
	current     int
	readyReads  int
	reloads     int
	navigations int
	backCalls   int
}

func newChallengeCDP(t *testing.T, states ...PageReadyState) (*Session, *challengeCDP) {
	t.Helper()
	fake := &challengeCDP{
		identity: documentIdentity{ID: "loader-1", URL: "https://search.jd.com/Search?keyword=water"},
		states:   states,
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var req fakeCDPRequest
			if conn.ReadJSON(&req) != nil {
				return
			}
			result := map[string]interface{}{}
			switch req.Method {
			case "Page.getFrameTree":
				fake.mu.Lock()
				identity := fake.identity
				fake.mu.Unlock()
				result["frameTree"] = map[string]interface{}{
					"frame": map[string]interface{}{"id": "frame-1", "loaderId": identity.ID, "url": identity.URL},
				}
			case "Page.createIsolatedWorld":
				result["executionContextId"] = 42
			case "Page.getNavigationHistory":
				fake.mu.Lock()
				entries := make([]map[string]interface{}, 0, len(fake.history))
				for _, entry := range fake.history {
					entries = append(entries, map[string]interface{}{"url": entry})
				}
				result["currentIndex"] = fake.current
				result["entries"] = entries
				fake.mu.Unlock()
			case "Runtime.evaluate":
				var params struct {
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal(req.Params, &params)
				if strings.Contains(params.Expression, "history.back()") {
					fake.mu.Lock()
					fake.backCalls++
					fake.mu.Unlock()
					_ = conn.WriteJSON(map[string]interface{}{"id": req.ID, "result": map[string]interface{}{"result": map[string]interface{}{"type": "undefined"}}})
					continue
				}
				fake.mu.Lock()
				index := fake.readyReads
				if index >= len(fake.states) {
					index = len(fake.states) - 1
				}
				fake.readyReads++
				state := fake.states[index]
				fake.mu.Unlock()
				encoded, _ := json.Marshal(state)
				result["result"] = map[string]interface{}{"type": "string", "value": string(encoded)}
			case "Page.reload":
				fake.mu.Lock()
				fake.reloads++
				fake.mu.Unlock()
			case "Page.navigate":
				fake.mu.Lock()
				fake.navigations++
				fake.mu.Unlock()
			}
			_ = conn.WriteJSON(map[string]interface{}{"id": req.ID, "result": result})
		}
	})
	fake.srv = httptest.NewServer(mux)
	t.Cleanup(fake.srv.Close)
	client, err := ConnectCDP("ws" + strings.TrimPrefix(fake.srv.URL, "http") + "/devtools/page/1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Session{client: client}, fake
}

func (f *challengeCDP) setDocument(identity documentIdentity, states ...PageReadyState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identity = identity
	f.states = states
	f.readyReads = 0
}

func (f *challengeCDP) setHistory(entries []string, current int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history = entries
	f.current = current
}

func (f *challengeCDP) counts() (reloads, navigations int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloads, f.navigations
}

func (f *challengeCDP) backCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.backCalls
}

func useShortChallengeTimers(t *testing.T, timeout, budget time.Duration) {
	t.Helper()
	seconds := strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64)
	t.Setenv(navReadyTimeoutEnv, seconds)
	t.Setenv(navBlankWallEnv, seconds)
	oldPoll := navReadyPoll
	oldBudget := documentChallengeWaitBudget
	navReadyPoll = time.Millisecond
	documentChallengeWaitBudget = budget
	t.Cleanup(func() {
		navReadyPoll = oldPoll
		documentChallengeWaitBudget = oldBudget
	})
}

func TestWaitPageReadyNeverReloadsRemoteBlank(t *testing.T) {
	useShortChallengeTimers(t, 4*time.Millisecond, time.Second)
	url := "https://item.jd.com/100.html"
	session, fake := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: url})
	report, ok := session.waitPageReady()
	if !ok || report.Ready || report.Wall != "blank" {
		t.Fatalf("report=%#v ok=%t", report, ok)
	}
	reloads, navigations := fake.counts()
	if reloads != 0 || navigations != 0 {
		t.Fatalf("readiness sent requests: reloads=%d navigations=%d", reloads, navigations)
	}
}

func TestDocumentChallengeClearsWithoutReload(t *testing.T) {
	useShortChallengeTimers(t, 50*time.Millisecond, time.Second)
	url := "https://search.jd.com/Search?keyword=water"
	session, fake := newChallengeCDP(t,
		PageReadyState{ReadyState: "complete", URL: url, Wall: "checking", Challenge: "generic"},
		PageReadyState{ReadyState: "complete", URL: url, Wall: "checking", Challenge: "generic"},
		PageReadyState{ReadyState: "complete", URL: url, Interactive: 8},
	)
	var report NavReadyReport
	ok := false
	for attempts := 0; attempts < 5 && !report.ChallengeCleared; attempts++ {
		report, ok = session.waitPageReady()
	}
	if !ok || !report.Ready || !report.ChallengeCleared || report.Wall != "" {
		t.Fatalf("report=%#v ok=%t", report, ok)
	}
	if !strings.Contains(report.Note(), "没有发送刷新") {
		t.Fatalf("note=%q", report.Note())
	}
	reloads, navigations := fake.counts()
	if reloads != 0 || navigations != 0 {
		t.Fatalf("challenge wait sent requests: reloads=%d navigations=%d", reloads, navigations)
	}
	if tracked := session.currentDocumentChallenge(); tracked != nil {
		t.Fatalf("cleared challenge is still tracked: %#v", tracked)
	}
}

func TestDocumentChallengeSharesOneBudgetAndStopsRefresh(t *testing.T) {
	useShortChallengeTimers(t, 4*time.Millisecond, 30*time.Millisecond)
	url := "https://search.jd.com/Search?keyword=water"
	checking := PageReadyState{ReadyState: "complete", URL: url, Wall: "checking", Challenge: "generic"}
	session, fake := newChallengeCDP(t, checking)
	first, _ := session.waitPageReady()
	if first.Wall != "checking" {
		t.Fatalf("first report=%#v", first)
	}
	firstSeen := session.challenge.FirstSeen
	second, _ := session.waitPageReady()
	if second.Wall != "checking" {
		t.Fatalf("a short read consumed the whole document budget: %#v", second)
	}
	expired := second
	for attempts := 0; attempts < 10 && expired.Wall == "checking"; attempts++ {
		expired, _ = session.waitPageReady()
	}
	if expired.Wall != "captcha" || expired.ChallengeCleared {
		t.Fatalf("expired report=%#v", expired)
	}
	third, _ := session.waitPageReady()
	if third.Wall != "captcha" || third.Waited > 20*time.Millisecond {
		t.Fatalf("a repeated read restarted the challenge budget: %#v", third)
	}
	if !session.challenge.FirstSeen.Equal(firstSeen) {
		t.Fatalf("challenge clock reset: first=%s now=%s", firstSeen, session.challenge.FirstSeen)
	}
	held := session.challengeForNavigation(url + "&_t=1795270081351")
	if held == nil || held.Kind != "captcha" {
		t.Fatalf("timestamp refresh bypassed the challenge: %#v", held)
	}
	if other := session.challengeForNavigation("https://search.jd.com/other"); other != nil {
		t.Fatalf("leaving the document was held: %#v", other)
	}
	agent := &BrowserAgentSession{ID: "s", session: session}
	if result := agent.personAskIfNeeded("press"); result == nil || result.Status != "ask" || result.Detail != "browser_challenge" {
		t.Fatalf("persistent challenge allowed press: %#v", result)
	}
	reloads, navigations := fake.counts()
	if reloads != 0 || navigations != 0 {
		t.Fatalf("challenge reads sent requests: reloads=%d navigations=%d", reloads, navigations)
	}
}

func TestDocumentChallengeBlocksMutationsWhileChecking(t *testing.T) {
	useShortChallengeTimers(t, 4*time.Millisecond, time.Second)
	url := "https://search.jd.com/Search?keyword=water"
	session, _ := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: url, Wall: "checking", Challenge: "generic"})
	report, _ := session.waitPageReady()
	if report.Wall != "checking" {
		t.Fatalf("report=%#v", report)
	}
	agent := &BrowserAgentSession{ID: "s", session: session}
	for _, action := range []string{"click", "type", "press", "select", "set_files", "dialog", "script"} {
		result := agent.challengeMutationResult(action)
		if result == nil || result.Status != "ok" || !result.batchStop || result.Detail != "browser_challenge_checking" {
			t.Fatalf("%s result=%#v", action, result)
		}
	}
}

func TestDocumentChallengeDoesNotFollowLoaderChange(t *testing.T) {
	useShortChallengeTimers(t, 4*time.Millisecond, time.Second)
	url := "https://search.jd.com/Search?keyword=water"
	session, fake := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: url, Wall: "checking", Challenge: "generic"})
	report, _ := session.waitPageReady()
	if report.Wall != "checking" {
		t.Fatalf("report=%#v", report)
	}
	fake.setDocument(documentIdentity{ID: "loader-2", URL: url}, PageReadyState{ReadyState: "complete", URL: url, Interactive: 3})
	if tracked := session.currentDocumentChallenge(); tracked != nil {
		t.Fatalf("challenge followed the new document: %#v", tracked)
	}
}

func TestDocumentChallengeLoginTransitionClears(t *testing.T) {
	useShortChallengeTimers(t, 50*time.Millisecond, time.Second)
	url := "https://passport.jd.com/login"
	session, _ := newChallengeCDP(t,
		PageReadyState{ReadyState: "complete", URL: url, Wall: "checking", Challenge: "generic"},
		PageReadyState{ReadyState: "complete", URL: url, Wall: "login_form"},
	)
	report, _ := session.waitPageReady()
	if report.Wall != "login_form" {
		t.Fatalf("report=%#v", report)
	}
	if tracked := session.currentDocumentChallenge(); tracked != nil {
		t.Fatalf("login form retained the old challenge: %#v", tracked)
	}
}

func TestRateLimitChallengeIsReevaluatedBySnapshot(t *testing.T) {
	useShortChallengeTimers(t, 50*time.Millisecond, time.Second)
	url := "https://search.jd.com/Search?keyword=water"
	session, _ := newChallengeCDP(t,
		PageReadyState{ReadyState: "complete", URL: url, Wall: "rate_limit"},
		PageReadyState{ReadyState: "complete", URL: url, Interactive: 6},
	)
	report, _ := session.waitPageReady()
	if report.Wall != "rate_limit" {
		t.Fatalf("report=%#v", report)
	}
	if note := session.waitForChallengeSnapshot(); note != "" {
		t.Fatalf("recovered page kept the rate-limit note: %q", note)
	}
	if tracked := session.currentDocumentChallenge(); tracked != nil {
		t.Fatalf("recovered page retained the rate-limit challenge: %#v", tracked)
	}
}

func TestMainDocumentHTTP429BecomesRateLimit(t *testing.T) {
	url := "https://search.jd.com/Search?keyword=water"
	session, _ := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: url, Interactive: 4})
	agent := &BrowserAgentSession{ID: "s", session: session}
	agent.handleCDPEvent(CDPEvent{Method: "Network.responseReceived", Params: json.RawMessage(`{"type":"XHR","response":{"url":"https://search.jd.com/Search?keyword=water","status":429}}`)})
	if state, err := session.pageReady(); err != nil || state.Wall == "rate_limit" {
		t.Fatalf("subresource response became a document wall: %#v err=%v", state, err)
	}
	agent.handleCDPEvent(CDPEvent{Method: "Page.frameNavigated", Params: json.RawMessage(`{"frame":{"id":"frame-1"}}`)})
	agent.handleCDPEvent(CDPEvent{Method: "Network.responseReceived", Params: json.RawMessage(`{"type":"Document","frameId":"child-frame","response":{"url":"https://search.jd.com/Search?keyword=water","status":429}}`)})
	if state, err := session.pageReady(); err != nil || state.Wall == "rate_limit" {
		t.Fatalf("same-url child frame became the main document: %#v err=%v", state, err)
	}
	agent.handleCDPEvent(CDPEvent{Method: "Network.responseReceived", Params: json.RawMessage(`{"type":"Document","frameId":"frame-1","response":{"url":"https://search.jd.com/Search?keyword=water","status":429}}`)})
	if state, err := session.pageReady(); err != nil || state.Wall != "rate_limit" || state.Challenge != "http_429" {
		t.Fatalf("main document 429 was missed: %#v err=%v", state, err)
	}
	agent.handleCDPEvent(CDPEvent{Method: "Network.responseReceived", Params: json.RawMessage(`{"type":"Document","frameId":"frame-1","response":{"url":"https://search.jd.com/Search?keyword=water","status":200}}`)})
	if state, err := session.pageReady(); err != nil || state.Wall == "rate_limit" {
		t.Fatalf("successful document kept the old 429: %#v err=%v", state, err)
	}
}

func TestBackChecksHistoryTargetBeforeSending(t *testing.T) {
	current := "https://example.com/result"
	refused := "https://search.jd.com/Search?keyword=water"
	session, fake := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: current, Interactive: 2})
	fake.setHistory([]string{refused, current}, 1)
	agent := &BrowserAgentSession{
		ID:              "s",
		session:         session,
		frequencyHostAt: map[string]time.Time{"jd.com": time.Now()},
	}
	result, err := agent.Back()
	if err != nil || result == nil || result.Action != "browser_back" || !FrequencyRefusalStops(result.Display) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if fake.backCallCount() != 0 {
		t.Fatal("history.back bypassed the refused target")
	}
}

func TestBackDoesNotReloadTheCurrentChallenge(t *testing.T) {
	challengeURL := "https://search.jd.com/Search?keyword=water"
	current := challengeURL + "&_t=2"
	session, fake := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: current, Interactive: 2})
	fake.setHistory([]string{challengeURL, current}, 1)
	session.challenge = &documentChallenge{DocumentID: "loader-1", URL: challengeURL, Kind: "checking", FirstSeen: time.Now()}
	agent := &BrowserAgentSession{ID: "s", session: session}
	result, err := agent.Back()
	if err != nil || result == nil || result.Detail != "browser_challenge_checking" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if fake.backCallCount() != 0 {
		t.Fatal("history.back reloaded the challenged document")
	}
}

func TestIsolatedWorldDoesNotRetakeSessionMutex(t *testing.T) {
	session, _ := newChallengeCDP(t, PageReadyState{ReadyState: "complete", URL: "https://example.com", Interactive: 1})
	session.mu.Lock()
	done := make(chan string, 1)
	go func() { done <- session.pageVisibilityLocked() }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("isolated world initialization retook Session.mu")
	}
	session.mu.Unlock()
}

func TestBrowserOperationQueueSerializesOneSession(t *testing.T) {
	session := &BrowserAgentSession{}
	release := session.beginBrowserOperation()
	acquired := make(chan func(), 1)
	go func() {
		acquired <- session.beginBrowserOperation()
	}()
	select {
	case releaseSecond := <-acquired:
		releaseSecond()
		release()
		t.Fatal("a second operation entered while the first still owned the session")
	case <-time.After(10 * time.Millisecond):
	}
	release()
	select {
	case releaseSecond := <-acquired:
		releaseSecond()
	case <-time.After(time.Second):
		t.Fatal("the queued operation did not enter after the release")
	}
}

func TestDocumentChallengeResultIsNotAnExpectFailure(t *testing.T) {
	session := &Session{challenge: &documentChallenge{DocumentID: "loader-1", URL: "https://search.jd.com/Search", Kind: "checking"}}
	agent := &BrowserAgentSession{ID: "s", session: session}
	result := agent.challengeMutationResult("click")
	raw := marshalActionResult(agent, result, nil, ExpectSpec{Type: "text", Pattern: "unexpected"})
	if !strings.Contains(raw, `"ok":true`) || !strings.Contains(raw, "这次操作未发送") {
		t.Fatalf("result=%s", raw)
	}
}

func TestNavigationChallengeHoldIsNotAnExpectFailure(t *testing.T) {
	agent := &BrowserAgentSession{ID: "s"}
	result := &BrowserActionResult{
		SessionID: "s",
		Action:    "browser_navigate",
		Status:    "ok",
		Display:   "navigation not sent; current document challenge=checking",
		Data:      map[string]interface{}{"navigation_sent": false},
	}
	raw := marshalActionResult(agent, result, nil, ExpectSpec{Type: "text", Pattern: "unexpected"})
	if !strings.Contains(raw, `"ok":true`) || !strings.Contains(raw, "navigation not sent") {
		t.Fatalf("result=%s", raw)
	}
}

func TestDocumentChallengeRequestAliasCannotBeReopened(t *testing.T) {
	session := &Session{challenge: &documentChallenge{
		DocumentID: "loader-1",
		URL:        "https://search.jd.com/unhuman",
		Kind:       "captcha",
	}}
	session.noteChallengeRequest("https://search.jd.com/Search?keyword=water")
	if held := session.challengeForNavigation("https://search.jd.com/Search?keyword=water&_t=1"); held == nil {
		t.Fatal("the original request URL bypassed the redirected challenge")
	}
	if held := session.challengeForNavigation("https://search.jd.com/other"); held != nil {
		t.Fatalf("a different document was held: %#v", held)
	}
}

func TestRefreshLabelDoesNotClaimChallengeHoldWasSent(t *testing.T) {
	result := labelRefreshResult(&BrowserActionResult{
		Status:  "ok",
		Display: "navigation not sent; current document challenge=checking",
		Data:    map[string]interface{}{"navigation_sent": false},
	}, "https://search.jd.com/Search?keyword=water")
	if result == nil || result.Action != "browser_refresh" || strings.Contains(result.Display, "refreshed") || browserNavigationSent(result) {
		t.Fatalf("result=%#v", result)
	}
}
