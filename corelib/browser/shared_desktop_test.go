package browser

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestStalledPageDoesNotBlockTheLoggedInPage(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			time.Sleep(3 * time.Second)
			_ = conn.Close()
		}
	}()
	start := time.Now()
	if pageIsVisible("ws://" + ln.Addr().String() + "/devtools/page/stalled") {
		t.Fatal("a stalled page was treated as the one the person is using")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("visibility probe blocked for %s", elapsed)
	}
}

func TestLoggedInPageAnswersAfterTheShortProbe(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(700 * time.Millisecond)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			_ = conn.WriteJSON(map[string]any{
				"id":     msg.ID,
				"result": map[string]any{"result": map[string]any{"type": "string", "value": "visible"}},
			})
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	if pageIsVisible(wsURL) {
		t.Fatal("short probe treated a slow desktop page as visible")
	}
	if !pageIsVisibleWithin(wsURL, 1500*time.Millisecond) {
		t.Fatal("the page the person logged into was skipped")
	}
}

func TestVisibleDesktopPageFindsTheSlowLoggedInTab(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	answer := func(value string, delay time.Duration) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if delay > 0 {
				time.Sleep(delay)
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg struct {
					ID int64 `json:"id"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
					continue
				}
				_ = conn.WriteJSON(map[string]any{
					"id":     msg.ID,
					"result": map[string]any{"result": map[string]any{"type": "string", "value": value}},
				})
			}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/old", answer("hidden", 0))
	mux.HandleFunc("/devtools/page/login", answer("visible", 700*time.Millisecond))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			time.Sleep(3 * time.Second)
			_ = conn.Close()
		}
	}()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	targets := []TargetInfo{
		{ID: "old", Type: "page", URL: "https://mail.example/old", WebSocketDebugURL: base + "/devtools/page/old"},
		{ID: "stalled", Type: "page", URL: "https://mail.example/other", WebSocketDebugURL: "ws://" + ln.Addr().String() + "/devtools/page/stalled"},
		{ID: "login", Type: "page", URL: "https://mail.example/inbox", WebSocketDebugURL: base + "/devtools/page/login"},
	}
	start := time.Now()
	if got := visibleDesktopPage("old", targets); got != "login" {
		t.Fatalf("page=%q, want the tab the person just logged into", got)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("finding the logged-in tab held the turn for %s", elapsed)
	}
}

func TestVisibleDesktopPageUsesTheFocusedLoginWindow(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	answer := func(value string, delay time.Duration) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if delay > 0 {
				time.Sleep(delay)
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg struct {
					ID int64 `json:"id"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
					continue
				}
				_ = conn.WriteJSON(map[string]any{
					"id":     msg.ID,
					"result": map[string]any{"result": map[string]any{"type": "string", "value": value}},
				})
			}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/old", answer(`{"v":"visible","f":false}`, 0))
	mux.HandleFunc("/devtools/page/login", answer(`{"v":"visible","f":true}`, 400*time.Millisecond))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			time.Sleep(3 * time.Second)
			_ = conn.Close()
		}
	}()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	targets := []TargetInfo{
		{ID: "old", Type: "page", URL: "https://mail.example/old", WebSocketDebugURL: base + "/devtools/page/old"},
		{ID: "stalled", Type: "page", URL: "https://mail.example/other", WebSocketDebugURL: "ws://" + ln.Addr().String() + "/devtools/page/stalled"},
		{ID: "login", Type: "page", URL: "https://accounts.mail.example/login", WebSocketDebugURL: base + "/devtools/page/login"},
	}
	start := time.Now()
	if got := visibleDesktopPage("old", targets); got != "login" {
		t.Fatalf("page=%q, want the window the person is typing in", got)
	}
	if elapsed := time.Since(start); elapsed > 1200*time.Millisecond {
		t.Fatalf("the focused login window was held for %s", elapsed)
	}
}

func TestVisibleDesktopPageLeavesAFocusedNetworkError(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	answer := func(value string, delay time.Duration) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if delay > 0 {
				time.Sleep(delay)
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg struct {
					ID int64 `json:"id"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
					continue
				}
				_ = conn.WriteJSON(map[string]any{
					"id":     msg.ID,
					"result": map[string]any{"result": map[string]any{"type": "string", "value": value}},
				})
			}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/error", answer(`{"v":"visible","f":true,"e":true}`, 0))
	mux.HandleFunc("/devtools/page/login", answer(`{"v":"visible","f":false,"e":false}`, 250*time.Millisecond))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	targets := []TargetInfo{
		{ID: "error", Type: "page", URL: "https://www.facebook.com/", WebSocketDebugURL: base + "/devtools/page/error"},
		{ID: "login", Type: "page", URL: "https://www.youtube.com/watch?v=cNw8C41UPbU", WebSocketDebugURL: base + "/devtools/page/login"},
	}
	if got := visibleDesktopPage("error", targets); got != "login" {
		t.Fatalf("page=%q, want the loaded window behind the network error", got)
	}
}

func TestVisibleDesktopPageKeepsTheOnlyNetworkError(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			_ = conn.WriteJSON(map[string]any{
				"id":     msg.ID,
				"result": map[string]any{"result": map[string]any{"type": "string", "value": `{"v":"visible","f":true,"e":true}`}},
			})
		}
	}))
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	targets := []TargetInfo{
		{ID: "error", Type: "page", URL: "https://www.facebook.com/", WebSocketDebugURL: base + "/devtools/page/error"},
	}
	if got := visibleDesktopPage("", targets); got != "error" {
		t.Fatalf("page=%q, want the only window even when it failed to load", got)
	}
}

func TestAttentionFromValueReadsANetworkError(t *testing.T) {
	got := attentionFromValue(`{"v":"visible","f":true,"e":true}`)
	if !got.visible || !got.focused || !got.failed {
		t.Fatalf("attention=%+v", got)
	}
	plain := attentionFromValue(`{"v":"visible","f":false}`)
	if !plain.visible || plain.focused || plain.failed {
		t.Fatalf("attention=%+v", plain)
	}
}

func TestDesktopConnectionRequiresALiveBrowser(t *testing.T) {
	sess := &BrowserAgentSession{session: &Session{}}
	if !sess.IsTargetAlive() {
		t.Fatal("target channel was already closed")
	}
	if sess.DesktopConnected() {
		t.Fatal("a desktop session without a browser connection was reused")
	}
}

func TestSharedDesktopDropsADeadConnection(t *testing.T) {
	dead := &BrowserAgentSession{
		ID:           "browser-session-dead",
		OwnerID:      "owner-dead",
		Addr:         "http://127.0.0.1:1",
		Mode:         SessionModePersistent,
		session:      &Session{},
		targetGoneCh: make(chan struct{}),
		stopCh:       make(chan struct{}),
	}
	browserAgentMu.Lock()
	browserAgentSessions[dead.ID] = dead
	browserAgentMu.Unlock()
	t.Cleanup(func() {
		browserAgentMu.Lock()
		delete(browserAgentSessions, dead.ID)
		browserAgentMu.Unlock()
	})
	if got := liveSharedDesktopSession(dead.OwnerID, dead.Addr, SessionModePersistent, BrowserPolicy{}); got != nil {
		t.Fatal("dead desktop connection was reused")
	}
	browserAgentMu.Lock()
	_, still := browserAgentSessions[dead.ID]
	browserAgentMu.Unlock()
	if still {
		t.Fatal("dead desktop connection stayed registered")
	}
}

func TestSharedDesktopUsesTheVisiblePage(t *testing.T) {
	if _, err := StartSharedDesktopSession("owner", ""); err == nil {
		t.Fatal("missing desktop address was accepted")
	}
	var mu sync.Mutex
	var methods []string
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]map[string]string{{
			"id":                   "page-1",
			"type":                 "page",
			"title":                "desktop",
			"url":                  "about:blank",
			"webSocketDebuggerUrl": wsURL,
		}})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			mu.Lock()
			methods = append(methods, msg.Method)
			mu.Unlock()
			if err := conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"id":%d,"result":{}}`, msg.ID))); err != nil {
				return
			}
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	sess, err := StartSharedDesktopSession("desktop-owner", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = StopAgentSession(sess.ID, false) }()
	if sess.TargetID != "page-1" {
		t.Fatalf("target=%q, want the visible page", sess.TargetID)
	}
	mu.Lock()
	seen := append([]string(nil), methods...)
	mu.Unlock()
	if !containsMethod(seen, "Target.activateTarget") {
		t.Fatalf("visible page was not brought forward: %v", seen)
	}
	if containsMethod(seen, "Target.createTarget") {
		t.Fatalf("shared desktop opened a second page: %v", seen)
	}
	again, err := StartSharedDesktopSession("desktop-owner", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != sess.ID || again.TargetID != "page-1" {
		t.Fatalf("second attach id=%s target=%s, want the same window", again.ID, again.TargetID)
	}
}

func TestSharedDesktopAttachesToTheVisibleLogin(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	var srv *httptest.Server
	page := func(id, url string) map[string]string {
		return map[string]string{
			"id":                   id,
			"type":                 "page",
			"url":                  url,
			"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/" + id,
		}
	}
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{
			page("old", "https://other.example/start"),
			page("login", "https://mail.example/inbox"),
		})
	})
	mux.HandleFunc("/devtools/page/", func(w http.ResponseWriter, r *http.Request) {
		visible := "hidden"
		if strings.HasSuffix(r.URL.Path, "/login") {
			visible = "visible"
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			body := `{"id":%d,"result":{}}`
			if msg.Method == "Runtime.evaluate" {
				body = `{"id":%d,"result":{"result":{"type":"string","value":"` + visible + `"}}}`
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(body, msg.ID))); err != nil {
				return
			}
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	sess, err := StartSharedDesktopSession("desktop-owner-login", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = StopAgentSession(sess.ID, false) }()
	if sess.TargetID != "login" {
		t.Fatalf("target=%q, want the visible logged-in page", sess.TargetID)
	}
}

func TestLoginTabKeepsDesktopEvents(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var mu sync.Mutex
	visibleID := "old"
	var loginMethods []string
	mux := http.NewServeMux()
	var srv *httptest.Server
	page := func(id, url string) map[string]string {
		return map[string]string{
			"id":                   id,
			"type":                 "page",
			"url":                  url,
			"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/" + id,
		}
	}
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{
			page("old", "https://other.example/start"),
			page("login", "https://mail.example/inbox"),
		})
	})
	mux.HandleFunc("/devtools/page/", func(w http.ResponseWriter, r *http.Request) {
		id := "old"
		if strings.HasSuffix(r.URL.Path, "/login") {
			id = "login"
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			body := `{"id":%d,"result":{}}`
			if msg.Method == "Runtime.evaluate" {
				mu.Lock()
				state := "hidden"
				if id == visibleID {
					state = "visible"
				}
				mu.Unlock()
				body = `{"id":%d,"result":{"result":{"type":"string","value":"` + state + `"}}}`
			}
			if id == "login" {
				mu.Lock()
				loginMethods = append(loginMethods, msg.Method)
				mu.Unlock()
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(body, msg.ID))); err != nil {
				return
			}
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	sess, err := StartSharedDesktopSession("desktop-owner-events", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = StopAgentSession(sess.ID, false) }()
	if sess.TargetID != "old" {
		t.Fatalf("target=%q, want the page that was visible first", sess.TargetID)
	}
	sess.mu.Lock()
	sess.signalTargetGone()
	sess.mu.Unlock()
	if sess.IsTargetAlive() {
		t.Fatal("the old page was still marked alive")
	}
	mu.Lock()
	visibleID = "login"
	mu.Unlock()
	if err := focusAgentDesktopPage(sess); err != nil {
		t.Fatal(err)
	}
	if sess.TargetID != "login" {
		t.Fatalf("target=%q, want the logged-in page", sess.TargetID)
	}
	if !sess.IsTargetAlive() {
		t.Fatal("the logged-in page was treated as gone")
	}
	if sess.eventPumpClient == nil || sess.eventPumpClient != sess.cdpClient() {
		t.Fatal("moving to the logged-in page stopped browser events")
	}
	mu.Lock()
	seen := append([]string(nil), loginMethods...)
	mu.Unlock()
	if !containsMethod(seen, "Target.setAutoAttach") || !containsMethod(seen, "Target.setDiscoverTargets") {
		t.Fatalf("the logged-in page does not watch new windows: %v", seen)
	}
}

func TestSharedDesktopFollowsThePageThePersonUsed(t *testing.T) {
	login := TargetInfo{ID: "login", Type: "page", URL: "https://app.example/login"}
	home := TargetInfo{ID: "home", Type: "page", URL: "https://app.example/home"}
	if got := chooseDesktopPageAfterPerson("", []TargetInfo{login, home}); got != "home" {
		t.Fatalf("page=%s, want the later signed-in page", got)
	}
	if got := chooseDesktopPageAfterPerson("login", []TargetInfo{login, home}); got != "login" {
		t.Fatalf("page=%s, want the page the person is looking at", got)
	}
	if got := chooseDesktopPageAfterPerson("", []TargetInfo{{ID: "only", Type: "page", URL: "about:blank"}}); got != "only" {
		t.Fatalf("page=%s, want the only page", got)
	}
}

func TestSharedDesktopKeepsTheLoggedInPage(t *testing.T) {
	blank := TargetInfo{ID: "blank", Type: "page", URL: "about:blank"}
	loggedIn := TargetInfo{ID: "site", Type: "page", URL: "https://app.example/home"}
	if got := chooseSharedDesktopPage("blank", []TargetInfo{blank, loggedIn}); got != "site" {
		t.Fatalf("page=%s, want the logged-in site", got)
	}
	if got := chooseSharedDesktopPage("site", []TargetInfo{loggedIn, blank}); got != "site" {
		t.Fatalf("page=%s, want to stay on the logged-in site", got)
	}
	if got := chooseSharedDesktopPage("blank", []TargetInfo{blank}); got != "blank" {
		t.Fatalf("page=%s, want the only page", got)
	}
	empty := TargetInfo{ID: "empty", Type: "page", URL: "data:,"}
	if got := chooseSharedDesktopPage("empty", []TargetInfo{empty, loggedIn}); got != "site" {
		t.Fatalf("page=%s, want the logged-in site instead of the empty data tab", got)
	}
	if got := chooseDesktopPageAfterPerson("empty", []TargetInfo{empty, loggedIn}); got != "site" {
		t.Fatalf("page=%s, want the logged-in site instead of the empty data tab", got)
	}
}

func TestReconnectUsesTheVisibleLoggedInTab(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	answer := func(value string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg struct {
					ID int64 `json:"id"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
					continue
				}
				_ = conn.WriteJSON(map[string]any{
					"id":     msg.ID,
					"result": map[string]any{"result": map[string]any{"type": "string", "value": value}},
				})
			}
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/old", answer("hidden"))
	mux.HandleFunc("/devtools/page/login", answer("visible"))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	targets := []TargetInfo{
		{ID: "old", Type: "page", URL: "https://mail.example/old", WebSocketDebugURL: base + "/devtools/page/old"},
		{ID: "login", Type: "page", URL: "https://mail.example/inbox", WebSocketDebugURL: base + "/devtools/page/login"},
	}
	if got := desktopReconnectPage("old", "old", targets); got != "old" {
		t.Fatalf("page=%s, want the previously attached tab when visibility is ignored", got)
	}
	if got := desktopPageAfterReconnect("old", "old", targets); got != "login" {
		t.Fatalf("page=%s, want the tab the person is viewing", got)
	}
}

func TestReconnectKeepsTheLoggedInTab(t *testing.T) {
	blank := TargetInfo{ID: "blank", Type: "page", URL: "about:blank"}
	loggedIn := TargetInfo{ID: "login", Type: "page", URL: "https://mail.example/inbox"}
	targets := []TargetInfo{blank, loggedIn}
	if got := desktopReconnectPage("login", "blank", targets); got != "login" {
		t.Fatalf("page=%s, want the logged-in tab", got)
	}
	left, err := chooseRecoveryPageTarget(nil, BrowserPolicy{}, "login", targets)
	if err != nil {
		t.Fatal(err)
	}
	if left == "login" {
		t.Fatal("local recovery no longer leaves the logged-in tab")
	}
}

func TestSharedDesktopNavigatesTheLoggedInPage(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	session := &Session{addr: srv.URL, stayOnCurrentPage: true, activeTabID: "login"}
	if got := session.reuseExistingPage("https://example.com/home"); got != "" {
		t.Fatalf("left the logged-in page: %s", got)
	}
	if hits != 0 {
		t.Fatalf("looked for another tab while the person is logged in: %d", hits)
	}
}

func TestLoggedInPageIsNotReloaded(t *testing.T) {
	if !desktopPageAlreadyOpen("https://mail.example/inbox/", "https://mail.example/inbox") {
		t.Fatal("the logged-in page was treated as a different page")
	}
	if desktopPageAlreadyOpen("https://mail.example/inbox", "https://mail.example/other") {
		t.Fatal("a different page was left unloaded")
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var mu sync.Mutex
	var methods []string
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			mu.Lock()
			methods = append(methods, msg.Method)
			mu.Unlock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
				`{"id":%d,"result":{"result":{"type":"string","value":"https://mail.example/inbox"}}}`, msg.ID)))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/1")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session := &Session{client: client, stayOnCurrentPage: true}
	if _, err := session.Navigate("https://mail.example/inbox"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	seen := append([]string(nil), methods...)
	mu.Unlock()
	if containsMethod(seen, "Page.navigate") {
		t.Fatalf("reloaded the logged-in page: %v", seen)
	}
}

func TestNavigateDoesNotReplaceTheLoggedInDocument(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var mu sync.Mutex
	var methods []string
	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			mu.Lock()
			methods = append(methods, msg.Method)
			mu.Unlock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
				`{"id":%d,"result":{"result":{"type":"string","value":"https://mail.example/inbox"}}}`, msg.ID)))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/1")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session := &Session{client: client, stayOnCurrentPage: true, stayOnLoggedInDocument: true}
	if session.blocksLeavingLoggedInDocument("https://mail.example/reports") {
		t.Fatal("the agent cannot continue to another page of the logged-in site")
	}
	if session.blocksLeavingLoggedInDocument("https://accounts.mail.example/session") {
		t.Fatal("the agent cannot continue on the login site's other host")
	}
	agent := &BrowserAgentSession{session: session}
	if err := validateNavigationPolicy(agent.loggedInNavigationPolicy("https://accounts.mail.example/session"), "https://accounts.mail.example/session", "mail.example"); err != nil {
		t.Fatalf("the logged-in site's other host was blocked: %v", err)
	}
	if err := validateNavigationPolicy(agent.loggedInNavigationPolicy("https://other.example/start"), "https://other.example/start", "mail.example"); err == nil {
		t.Fatal("another site was allowed after login")
	}
	if !session.blocksLeavingLoggedInDocument("https://other.example/start") {
		t.Fatal("navigation left the logged-in site")
	}
	if sameLoggedInSite("https://app.example.co.uk/home", "https://login.example.co.uk/session") != true {
		t.Fatal("a country-suffix login host was treated as another site")
	}
	if sameLoggedInSite("https://app.example.co.uk/home", "https://evil.co.uk/") {
		t.Fatal("a different site under the same public suffix was kept")
	}
	result, err := agent.Navigate("https://other.example/start")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.batchStop || result.batchStopReason != "logged_in_page" {
		t.Fatalf("navigation left the logged-in site: %+v", result)
	}
	if !strings.Contains(result.Display, "logged-in page is already open") || !strings.Contains(result.Display, "https://mail.example/inbox") || !strings.Contains(result.Display, "this site") {
		t.Fatalf("the logged-in site was not kept: %s", result.Display)
	}
	mu.Lock()
	seen := append([]string(nil), methods...)
	mu.Unlock()
	if containsMethod(seen, "Page.navigate") || containsMethod(seen, "Target.createTarget") {
		t.Fatalf("opened another site and left the website login: %v", seen)
	}
}

func TestBlankNavigationDoesNotCoverTheLogin(t *testing.T) {
	session := &Session{stayOnCurrentPage: true}
	if _, err := session.Navigate("about:blank"); err != nil {
		t.Fatal(err)
	}
	if !session.keepsLoggedInPage("chrome://newtab") {
		t.Fatal("a chrome page would cover the login")
	}
	local := &Session{}
	if _, err := local.Navigate("about:blank"); err == nil {
		t.Fatal("a local browser skipped navigation")
	}
}

func TestLoggedInPopupStaysOnTheSameSite(t *testing.T) {
	if followLoggedInPopupPage("https://mail.example/inbox", "about:blank") {
		t.Fatal("a blank window pulled the agent off the logged-in page")
	}
	if !followLoggedInPopupPage("https://mail.example/inbox", "https://accounts.mail.example/next") {
		t.Fatal("the next page of the logged-in site was left behind")
	}
	if followLoggedInPopupPage("https://mail.example/inbox", "https://other.example/ad") {
		t.Fatal("a window from another site took the logged-in page")
	}
	if followLoggedInPopupPage("", "https://other.example/ad") {
		t.Fatal("an unknown window took the desktop")
	}
}

func TestFollowsTheLiveLoggedInPageNotTheOldProbe(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	var srv *httptest.Server
	answer := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
				`{"id":%d,"result":{"result":{"type":"string","value":"https://mail.example/inbox"}}}`, msg.ID)))
		}
	}
	mux.HandleFunc("/devtools/page/", answer)
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		base := "ws" + strings.TrimPrefix(srv.URL, "http")
		_ = json.NewEncoder(w).Encode([]map[string]string{{
			"id": "popup", "type": "page", "url": "https://accounts.mail.example/next",
			"webSocketDebuggerUrl": base + "/devtools/page/popup",
		}})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/login")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session := &Session{addr: srv.URL, client: client, stayOnLoggedInDocument: true, activeTabID: "login"}
	agent := &BrowserAgentSession{
		session:        session,
		TargetID:       "login",
		lastSnapshotID: "old",
		snapshots: map[string]*BrowserSnapshot{
			"old": {SnapshotID: "old", URL: "https://other.example/ad"},
		},
	}
	agent.followLoggedInPopup("popup", "https://accounts.mail.example/next")
	agent.mu.RLock()
	got := agent.TargetID
	agent.mu.RUnlock()
	if got != "popup" {
		t.Fatalf("stayed on the old probe instead of the logged-in site: %s", got)
	}
}

func TestFollowsTheLoggedInWindowOnceItIsListed(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	var srv *httptest.Server
	var hits int
	var hitMu sync.Mutex
	answer := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
				`{"id":%d,"result":{"result":{"type":"string","value":"https://mail.example/inbox"}}}`, msg.ID)))
		}
	}
	mux.HandleFunc("/devtools/page/", answer)
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		hitMu.Lock()
		hits++
		ready := hits >= 3
		hitMu.Unlock()
		base := "ws" + strings.TrimPrefix(srv.URL, "http")
		pages := []map[string]string{{
			"id": "login", "type": "page", "url": "https://mail.example/inbox",
			"webSocketDebuggerUrl": base + "/devtools/page/login",
		}}
		if ready {
			pages = append(pages, map[string]string{
				"id": "popup", "type": "page", "url": "https://accounts.mail.example/next",
				"webSocketDebuggerUrl": base + "/devtools/page/popup",
			})
		}
		_ = json.NewEncoder(w).Encode(pages)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/login")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session := &Session{addr: srv.URL, client: client, stayOnLoggedInDocument: true, activeTabID: "login"}
	agent := &BrowserAgentSession{session: session, TargetID: "login"}
	agent.followLoggedInPopup("popup", "https://accounts.mail.example/next")
	agent.mu.RLock()
	got := agent.TargetID
	agent.mu.RUnlock()
	if got != "popup" {
		t.Fatalf("stayed on the old page instead of the logged-in window: %s", got)
	}
}

func TestLoginPopupStaysOnTheSharedDesktop(t *testing.T) {
	if shouldCloseUnsolicitedPopup(true, fmt.Errorf("popup blocked")) {
		t.Fatal("the login window on the shared desktop was closed")
	}
	if !shouldCloseUnsolicitedPopup(false, fmt.Errorf("popup blocked")) {
		t.Fatal("a local browser popup was left open")
	}
	if shouldCloseUnsolicitedPopup(false, nil) {
		t.Fatal("an allowed popup was closed")
	}
}

func TestActionRecoveryStaysOnTheLoggedInPage(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		base := "ws" + strings.TrimPrefix(srv.URL, "http")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "blank", "type": "page", "url": "about:blank", "webSocketDebuggerUrl": base + "/devtools/page/blank"},
			{"id": "login", "type": "page", "url": "https://mail.example/inbox", "webSocketDebuggerUrl": base + "/devtools/page/login"},
		})
	})
	mux.HandleFunc("/devtools/page/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.ID == 0 {
				continue
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"id":%d,"result":{}}`, msg.ID)))
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	session := &Session{addr: srv.URL, stayOnCurrentPage: true, activeTabID: "blank"}
	if err := reattachAfterTargetGone(session, BrowserPolicy{}, "login"); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	active := session.activeTabID
	session.mu.Unlock()
	if active != "login" {
		t.Fatalf("page=%s, want the logged-in tab", active)
	}
}

func containsMethod(methods []string, want string) bool {
	for _, method := range methods {
		if method == want {
			return true
		}
	}
	return false
}
