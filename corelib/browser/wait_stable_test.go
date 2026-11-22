package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTextLenWithinTolerance(t *testing.T) {
	tests := []struct {
		name     string
		current  int
		previous int
		tol      float64
		want     bool
	}{
		{"both zero", 0, 0, 0.05, true},
		{"previous zero current nonzero", 100, 0, 0.05, false},
		{"exact match", 1000, 1000, 0.05, true},
		{"within 5%", 1040, 1000, 0.05, true},
		{"at boundary", 1050, 1000, 0.05, true},
		{"over 5%", 1060, 1000, 0.05, false},
		{"decrease within", 960, 1000, 0.05, true},
		{"decrease over", 940, 1000, 0.05, false},
		{"small page minor change", 50, 48, 0.05, true},   // 2/48 = 4.2%
		{"small page larger change", 45, 48, 0.05, false}, // 3/48 = 6.3%
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := textLenWithinTolerance(tc.current, tc.previous, tc.tol)
			if got != tc.want {
				t.Fatalf("textLenWithinTolerance(%d, %d, %f) = %v, want %v",
					tc.current, tc.previous, tc.tol, got, tc.want)
			}
		})
	}
}

func TestParseStabilitySignature(t *testing.T) {
	tests := []struct {
		name      string
		sig       string
		wantReady string
		wantURL   string
		wantText  int
		wantMut   int
	}{
		{
			"complete page",
			`{"ready":"complete","url":"https://example.com/app","text":4521,"mut":3}`,
			"complete", "https://example.com/app", 4521, 3,
		},
		{
			"interactive page",
			`{"ready":"interactive","url":"https://spa.test/","text":120}`,
			"interactive", "https://spa.test/", 120, 0,
		},
		{
			"loading page",
			`{"ready":"loading","url":"about:blank","text":0}`,
			"loading", "about:blank", 0, 0,
		},
		{
			"invalid json",
			`not json`,
			"", "", 0, 0,
		},
		{
			"empty string",
			``,
			"", "", 0, 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready, url, textLen, mut := parseStabilitySignature(tc.sig)
			if ready != tc.wantReady || url != tc.wantURL || textLen != tc.wantText || mut != tc.wantMut {
				t.Fatalf("parseStabilitySignature(%q) = (%q, %q, %d, %d), want (%q, %q, %d, %d)",
					tc.sig, ready, url, textLen, mut, tc.wantReady, tc.wantURL, tc.wantText, tc.wantMut)
			}
		})
	}
}

func TestStabilityProbeWalksIframes(t *testing.T) {
	for _, marker := range []string{"__maclawMut", "globalThis", "contentDocument", "iframe", "readyState", "queryIframes", "shadowRoot"} {
		if !strings.Contains(stabilityProbeJS, marker) {
			t.Fatalf("stability probe missing %q", marker)
		}
	}
	if strings.Contains(stabilityProbeJS, "innerText") || strings.Contains(stabilityProbeJS, "textContent") {
		t.Fatal("stability probe must not use body text length")
	}
	if strings.Contains(stabilityProbeJS, "defaultView") || strings.Contains(stabilityProbeJS, "window.__maclawMut") {
		t.Fatal("stability probe assigns the counter on the page window")
	}
	if strings.Contains(stabilityReadyJS, "__maclawMut") || strings.Contains(stabilityReadyJS, "MutationObserver") {
		t.Fatal("ready fallback installs a counter")
	}
}

type stabilityCDPLog struct {
	mu          sync.Mutex
	contexts    []int
	creates     int
	bare        int
	bareExpr    []string
	methods     []string
	badFrame    bool
	granted     bool
	named       bool
	scriptFault string
}

func serveStabilityCDP(t *testing.T, failFirst, noWorld bool) (*CDPClient, *stabilityCDPLog) {
	t.Helper()
	log := &stabilityCDPLog{}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		world := 6
		evals := 0
		for {
			var req struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			log.mu.Lock()
			log.methods = append(log.methods, req.Method)
			log.mu.Unlock()
			result := map[string]interface{}{}
			switch req.Method {
			case "Page.getFrameTree":
				if noWorld {
					_ = conn.WriteJSON(map[string]interface{}{
						"id":    req.ID,
						"error": map[string]interface{}{"code": -32000, "message": "no frame"},
					})
					continue
				}
				result = map[string]interface{}{
					"frameTree": map[string]interface{}{
						"frame": map[string]interface{}{"id": "frame-1"},
					},
				}
			case "Page.createIsolatedWorld":
				var p struct {
					FrameID string `json:"frameId"`
					Grant   bool   `json:"grantUniveralAccess"`
					Name    string `json:"worldName"`
				}
				_ = json.Unmarshal(req.Params, &p)
				log.mu.Lock()
				log.creates++
				if p.FrameID != "frame-1" {
					log.badFrame = true
				}
				if p.Grant || strings.Contains(string(req.Params), "grantUniveralAccess") {
					log.granted = true
				}
				if p.Name != "" || strings.Contains(string(req.Params), "worldName") {
					log.named = true
				}
				world++
				id := world
				log.mu.Unlock()
				result = map[string]interface{}{"executionContextId": id}
			case "Runtime.evaluate":
				var p struct {
					ContextID  int    `json:"contextId"`
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal(req.Params, &p)
				log.mu.Lock()
				evals++
				n := evals
				fault := log.scriptFault
				if p.ContextID == 0 {
					log.bare++
					log.bareExpr = append(log.bareExpr, p.Expression)
				} else {
					log.contexts = append(log.contexts, p.ContextID)
				}
				log.mu.Unlock()
				value := `{"ready":"complete","url":"https://www.jd.com/","mut":1}`
				if p.Expression == "document.readyState" {
					value = "complete"
				}
				if failFirst && n == 1 {
					result = map[string]interface{}{
						"exceptionDetails": map[string]interface{}{"text": "Execution context was destroyed."},
					}
				} else if fault != "" && n == 1 {
					result = map[string]interface{}{
						"exceptionDetails": map[string]interface{}{"text": fault},
					}
				} else {
					result = map[string]interface{}{
						"result": map[string]interface{}{
							"type":  "string",
							"value": value,
						},
					}
				}
			}
			_ = conn.WriteJSON(map[string]interface{}{"id": req.ID, "result": result})
		}
	}))
	t.Cleanup(srv.Close)
	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client, log
}

func TestStabilityProbeUsesIsolatedWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	if err := (&Session{}).waitForStableOn(client, time.Second, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates != 1 {
		t.Fatalf("createIsolatedWorld calls = %d, want 1", log.creates)
	}
	if log.bare != 0 {
		t.Fatalf("main-world evaluates = %d", log.bare)
	}
	if log.badFrame || log.granted || log.named {
		t.Fatalf("isolated world frame=%v grant=%v named=%v", log.badFrame, log.granted, log.named)
	}
	if len(log.contexts) < 3 {
		t.Fatalf("isolated evaluates = %d", len(log.contexts))
	}
	for _, id := range log.contexts {
		if id != 7 {
			t.Fatalf("evaluate context = %d, want 7", id)
		}
	}
}

func TestStabilityProbeRecreatesDestroyedWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, true, false)
	if err := (&Session{}).waitForStableOn(client, time.Second, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates != 2 {
		t.Fatalf("createIsolatedWorld calls = %d, want 2", log.creates)
	}
	if log.bare != 0 {
		t.Fatalf("destroyed world fell back to the page window, evaluates=%d", log.bare)
	}
	if len(log.contexts) < 4 || log.contexts[0] != 7 {
		t.Fatalf("contexts = %v", log.contexts)
	}
	for _, id := range log.contexts[1:] {
		if id != 8 {
			t.Fatalf("recreated context = %d, want 8 in %v", id, log.contexts)
		}
	}
}

func TestStabilityProbeReadyFallbackDoesNotTouchPage(t *testing.T) {
	client, log := serveStabilityCDP(t, false, true)
	if err := (&Session{}).waitForStableOn(client, time.Second, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates != 0 || log.bare < 3 {
		t.Fatalf("creates=%d bare=%d", log.creates, log.bare)
	}
	for _, expr := range log.bareExpr {
		if expr != stabilityReadyJS || strings.Contains(expr, "MutationObserver") || strings.Contains(expr, "__maclawMut") {
			t.Fatalf("fallback expression installs a counter: %s", expr)
		}
	}
}

func TestPageReadyReadsInIsolatedWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	st, err := (&Session{client: client}).pageReady()
	if err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates < 1 || log.bare != 0 {
		t.Fatalf("creates=%d bare=%d url=%q", log.creates, log.bare, st.URL)
	}
	for _, id := range log.contexts {
		if id == 0 {
			t.Fatal("page ready used the page world")
		}
	}
}

func TestEvalIsolatedDoesNotReadThePageWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, false, true)
	_, err := (&Session{client: client}).evalIsolated("location.href", false, time.Second)
	if err == nil {
		t.Fatal("a missing isolated world still returned a value")
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.bare != 0 || log.creates != 0 {
		t.Fatalf("creates=%d bare=%d", log.creates, log.bare)
	}
}

func focusBefore(methods []string, input string) bool {
	focus := -1
	act := -1
	for i, m := range methods {
		if m == "Emulation.setFocusEmulationEnabled" && focus < 0 {
			focus = i
		}
		if m == input && act < 0 {
			act = i
		}
	}
	return focus >= 0 && act >= 0 && focus < act
}

func TestClickPointEmulatesFocusBeforeThePress(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	if err := (&Session{client: client}).clickPoint(566, 502); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if !focusBefore(log.methods, "Input.dispatchMouseEvent") {
		t.Fatalf("methods = %v", log.methods)
	}
}

func TestPressEmulatesFocusBeforeTheKey(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	if err := (&Session{client: client}).Press("Enter"); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if !focusBefore(log.methods, "Input.dispatchKeyEvent") {
		t.Fatalf("methods = %v", log.methods)
	}
}

func waitForMethod(log *stabilityCDPLog, method string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		log.mu.Lock()
		for _, m := range log.methods {
			if m == method {
				log.mu.Unlock()
				return true
			}
		}
		log.mu.Unlock()
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWaitForLoadReadsInIsolatedWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	if got := (&Session{client: client}).waitForLoadOn(client, time.Second, 0); got != "complete" {
		t.Fatalf("readyState = %q", got)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates < 1 || log.bare != 0 {
		t.Fatalf("creates=%d bare=%d", log.creates, log.bare)
	}
}

func TestPageInfoReadsInIsolatedWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	info, err := (&Session{client: client}).Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "https://www.jd.com/" {
		t.Fatalf("url = %q", info.URL)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates < 1 || log.bare != 0 {
		t.Fatalf("creates=%d bare=%d", log.creates, log.bare)
	}
}

func TestHoverMissDoesNotReadThePageWorld(t *testing.T) {
	client, log := serveStabilityCDP(t, false, true)
	if err := (&Session{client: client}).Hover("#q"); err == nil {
		t.Fatal("missing isolated world still hovered")
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.bare != 0 {
		t.Fatalf("hover reread the page world: %v", log.bareExpr)
	}
	for _, m := range log.methods {
		if m == "Input.dispatchMouseEvent" {
			t.Fatalf("hover sent input after the read failed: %v", log.methods)
		}
	}
}

func TestHoverCoordReadStaysIsolated(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	if err := (&Session{client: client}).Hover("#q"); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.bare != 0 || !focusBefore(log.methods, "Input.dispatchMouseEvent") {
		t.Fatalf("bare=%d methods=%v", log.bare, log.methods)
	}
}

func TestEvalIsolatedDoesNotRetryAScriptError(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	log.mu.Lock()
	log.scriptFault = "element not found"
	log.mu.Unlock()
	_, err := (&Session{client: client}).evalIsolated("location.href", false, time.Second)
	if err == nil {
		t.Fatal("script error was ignored")
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates != 1 || log.bare != 0 || len(log.contexts) != 1 {
		t.Fatalf("creates=%d bare=%d contexts=%v", log.creates, log.bare, log.contexts)
	}
}

func TestEvalIsolatedRecreatesDestroyedContext(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	log.mu.Lock()
	log.scriptFault = "Execution context was destroyed."
	log.mu.Unlock()
	if _, err := (&Session{client: client}).evalIsolated("location.href", false, time.Second); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.creates != 2 || log.bare != 0 {
		t.Fatalf("creates=%d bare=%d contexts=%v", log.creates, log.bare, log.contexts)
	}
}

func TestTopFrameNavigationRestoresFocus(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	agent := &BrowserAgentSession{session: &Session{client: client}}
	agent.handleCDPEvent(CDPEvent{
		Method: "Page.frameNavigated",
		Params: json.RawMessage(`{"frame":{"id":"frame-1","url":"https://www.jd.com/"}}`),
	})
	if !waitForMethod(log, "Emulation.setFocusEmulationEnabled", time.Second) {
		log.mu.Lock()
		defer log.mu.Unlock()
		t.Fatalf("methods = %v", log.methods)
	}
}

func TestChildFrameNavigationDoesNotRefocus(t *testing.T) {
	client, log := serveStabilityCDP(t, false, false)
	agent := &BrowserAgentSession{session: &Session{client: client}}
	agent.handleCDPEvent(CDPEvent{
		Method: "Page.frameNavigated",
		Params: json.RawMessage(`{"frame":{"id":"child","parentId":"frame-1","url":"https://x"}}`),
	})
	time.Sleep(200 * time.Millisecond)
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, m := range log.methods {
		if m == "Emulation.setFocusEmulationEnabled" {
			t.Fatalf("child frame restored focus: %v", log.methods)
		}
	}
}

func TestWaitTimeoutSec(t *testing.T) {
	if got := waitTimeoutSec(0); got != 10 {
		t.Fatalf("default timeout = %d, want 10", got)
	}
	if got := waitTimeoutSec(500); got != 10 {
		t.Fatalf("sub-second timeout = %d, want 10", got)
	}
	if got := waitTimeoutSec(1000); got != 1 {
		t.Fatalf("1s timeout = %d, want 1", got)
	}
	if got := waitTimeoutSec(2500); got != 3 {
		t.Fatalf("2500ms timeout = %d, want 3", got)
	}
	if got := waitTimeoutSec(10000); got != 10 {
		t.Fatalf("10s timeout = %d, want 10", got)
	}
}
