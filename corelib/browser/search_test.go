package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeSearchCDP emulates the browser/page CDP endpoints SearchViaBrowser
// needs: /json/version, /json, Target.createTarget/closeTarget, and
// Runtime.evaluate returning a canned extraction payload per URL.
type fakeSearchCDP struct {
	srv *httptest.Server

	mu             sync.Mutex
	browserConn    *websocket.Conn
	browserWriteMu sync.Mutex
	pageWriteMu    sync.Mutex

	// hitsByURL maps a substring of the navigated URL to the JSON string the
	// extraction expression should "return".
	hitsByURL       map[string]string
	navigated       chan string
	verification    bool
	activated       bool
	ignoreDispose   bool
	activatedTarget chan string
}

func (f *fakeSearchCDP) reply(conn *websocket.Conn, mu *sync.Mutex, id int64, result interface{}) {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.WriteJSON(map[string]interface{}{"id": id, "result": result})
}

func newFakeSearchCDP(t *testing.T, hitsByURL map[string]string) *fakeSearchCDP {
	t.Helper()
	f := &fakeSearchCDP{
		hitsByURL:       hitsByURL,
		navigated:       make(chan string, 4),
		activatedTarget: make(chan string, 1),
	}
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/devtools/browser"
		_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": wsURL})
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/devtools/page/page-1"
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "page-1", "type": "page", "url": "about:blank", "webSocketDebuggerUrl": wsURL},
		})
	})
	mux.HandleFunc("/devtools/browser", func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.browserConn = conn
		f.mu.Unlock()
		for {
			var req fakeCDPRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			switch req.Method {
			case "Target.createBrowserContext":
				f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{"browserContextId": "isolated-1"})
			case "Target.createTarget":
				var p struct {
					URL string `json:"url"`
				}
				_ = json.Unmarshal(req.Params, &p)
				f.navigated <- p.URL
				f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{"targetId": "page-1"})
			case "Target.activateTarget":
				var p struct {
					TargetID string `json:"targetId"`
				}
				_ = json.Unmarshal(req.Params, &p)
				f.mu.Lock()
				f.activated = true
				f.mu.Unlock()
				f.activatedTarget <- p.TargetID
				f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{})
			case "Target.disposeBrowserContext":
				f.mu.Lock()
				ignore := f.ignoreDispose
				f.mu.Unlock()
				if !ignore {
					f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{})
				}
			default:
				f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{})
			}
		}
	})
	mux.HandleFunc("/devtools/page/page-1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		for {
			var req fakeCDPRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			if req.Method == "Runtime.evaluate" {
				var p struct {
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal(req.Params, &p)
				expr := p.Expression
				f.mu.Lock()
				verification := f.verification
				activated := f.activated
				f.mu.Unlock()
				if strings.Contains(expr, "verify you are human") {
					f.reply(conn, &f.pageWriteMu, req.ID, map[string]interface{}{
						"result": map[string]interface{}{"type": "boolean", "value": verification},
					})
					continue
				}
				payload := "[]"
				engine := "bing"
				if strings.Contains(expr, "MjjYud") {
					engine = "google"
				}
				if v, ok := f.hitsByURL[engine]; ok && (!verification || activated) {
					payload = v
				}
				// Runtime.evaluate result envelope: {"result": {"type": "string", "value": "<json string>"}}
				f.reply(conn, &f.pageWriteMu, req.ID, map[string]interface{}{
					"result": map[string]interface{}{"type": "string", "value": payload},
				})
				continue
			}
			f.reply(conn, &f.pageWriteMu, req.ID, map[string]interface{}{})
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestSearchEngineViaBrowserReturnsHits(t *testing.T) {
	fake := newFakeSearchCDP(t, map[string]string{
		"bing": `[{"title":"Go Dev","url":"https://go.dev/","snippet":"the Go site"}]`,
	})
	hits, err := searchEngineViaBrowser(context.Background(), fake.srv.URL, "bing",
		"https://cn.bing.com/search?q=golang&count=5", 5)
	if err != nil {
		t.Fatalf("searchEngineViaBrowser: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "Go Dev" || hits[0].URL != "https://go.dev/" || hits[0].Snippet != "the Go site" {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

func TestSearchEngineViaBrowserTimesOutWithoutHits(t *testing.T) {
	fake := newFakeSearchCDP(t, map[string]string{}) // extraction always "[]"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := searchEngineViaBrowser(ctx, fake.srv.URL, "bing", "https://cn.bing.com/search?q=x", 5)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPageWSURLForTargetStopsWhenCallerCancels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json" {
			http.NotFound(w, r)
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := pageWSURLForTargetCtx(ctx, srv.URL, "missing", 5*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancelled target discovery took %v", elapsed)
	}
}

func TestBrowserWebSocketURLStopsWhenCallerCancels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := browserWebSocketURLCtx(ctx, srv.URL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancelled browser discovery took %v", elapsed)
	}
}

func TestBrowserSearchCleanupStaysBoundedAfterCancellation(t *testing.T) {
	fake := newFakeSearchCDP(t, map[string]string{})
	fake.mu.Lock()
	fake.ignoreDispose = true
	fake.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := searchEngineViaBrowser(ctx, fake.srv.URL, "bing", "https://cn.bing.com/search?q=x", 5)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled browser search cleanup took %v", elapsed)
	}
}

func TestSearchViaBrowserQueuedCallerCanCancel(t *testing.T) {
	browserSearchGate <- struct{}{}
	defer func() { <-browserSearchGate }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := SearchViaBrowser(ctx, "bing_cn", "golang", 3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancelled queued search took %v", elapsed)
	}
}

func TestSearchViaBrowserRejectsUnsupportedEngineBeforeBrowserDiscovery(t *testing.T) {
	started := time.Now()
	_, err := SearchViaBrowser(context.Background(), "unsupported", "golang", 3)
	if err == nil || !strings.Contains(err.Error(), "unsupported browser search engine") {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("invalid engine validation took %v", elapsed)
	}
}

func TestParseSearchEvalResult(t *testing.T) {
	raw := json.RawMessage(`{"result":{"type":"string","value":"[{\"title\":\"A\",\"url\":\"https://a/\",\"snippet\":\"s\"},{\"title\":\"\",\"url\":\"https://bad/\",\"snippet\":\"\"},{\"title\":\"B\",\"url\":\"ftp://no/\",\"snippet\":\"\"}]"}}`)
	hits := parseSearchEvalResult(raw)
	if len(hits) != 1 || hits[0].Title != "A" {
		t.Fatalf("unexpected hits: %+v", hits)
	}
	if got := parseSearchEvalResult(json.RawMessage(`{"result":{"type":"string","value":"[]"}}`)); len(got) != 0 {
		t.Fatalf("empty payload should yield no hits: %+v", got)
	}
}

func TestSearchPageNeedsHumanVerification(t *testing.T) {
	fake := newFakeSearchCDP(t, map[string]string{})
	pageWS, err := pageWSURLForTarget(fake.srv.URL, "page-1", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pws, err := ConnectCDP(pageWS)
	if err != nil {
		t.Fatal(err)
	}
	defer pws.Close()
	// The fake endpoint returns a string payload, never a boolean verification
	// signal; this verifies malformed/irrelevant evaluate responses stay safe.
	if searchPageNeedsHumanVerification(context.Background(), pws) {
		t.Fatal("unexpected verification detection")
	}
}

func TestBrowserSearchCommandTimeoutUsesRemainingContextBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	timeout := browserSearchCommandTimeout(ctx, DefaultCmdTimeout)
	if timeout <= 0 || timeout > 50*time.Millisecond {
		t.Fatalf("timeout = %v, want remaining context budget", timeout)
	}
	if got := browserSearchCommandTimeout(context.Background(), 3*time.Second); got != 3*time.Second {
		t.Fatalf("timeout without deadline = %v", got)
	}
}

func TestSearchEngineViaBrowserForegroundsHumanVerificationAndResumes(t *testing.T) {
	fake := newFakeSearchCDP(t, map[string]string{
		"bing": `[{"title":"Verified result","url":"https://example.com/","snippet":"ok"}]`,
	})
	fake.verification = true

	hits, err := searchEngineViaBrowserWithOptions(context.Background(), fake.srv.URL, "bing",
		"https://cn.bing.com/search?q=verify", 5, SearchOptions{HumanAssist: true})
	if err != nil {
		t.Fatalf("searchEngineViaBrowserWithOptions: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "Verified result" {
		t.Fatalf("unexpected hits after verification: %+v", hits)
	}
	select {
	case targetID := <-fake.activatedTarget:
		if targetID != "page-1" {
			t.Fatalf("activated target = %q, want page-1", targetID)
		}
	default:
		t.Fatal("verification page was not brought to the foreground")
	}
}

// TestSearchExtractionJSLayered guards the layered extraction contract:
// engine-specific selectors first, structural/generic fallback last, so a
// markup redesign degrades instead of returning zero results.
func TestSearchExtractionJSLayered(t *testing.T) {
	bing := searchExtractionJS("bing", 7)
	for _, marker := range []string{
		"li.b_algo",                   // primary
		"ol#b_results",                // secondary structural layer
		"genericExtract",              // generic fallback
		"const MAX = 7;",              // maxResults embedded
		"bing\\.com\\/(search|images", // self-link exclusion in fallback
	} {
		if !strings.Contains(bing, marker) {
			t.Fatalf("bing extraction JS missing %q", marker)
		}
	}
	// Layer order: primary evaluated before generic fallback.
	if strings.Index(bing, "primary()") > strings.Index(bing, "genericExtract()") {
		t.Fatal("bing extraction must try primary before generic fallback")
	}

	google := searchExtractionJS("google", 5)
	for _, marker := range []string{
		"a h3",
		"genericExtract",
		"const MAX = 5;",
		"google\\.[a-z.]+",
	} {
		if !strings.Contains(google, marker) {
			t.Fatalf("google extraction JS missing %q", marker)
		}
	}
}

// TestGenericSearchExtractJSContract guards the fallback heuristics: nav
// areas excluded, short/anchor-less links skipped, engine self-links out.
func TestGenericSearchExtractJSContract(t *testing.T) {
	js := genericSearchExtractJS(`/example\.com\/search/`)
	for _, marker := range []string{
		"nav,header,footer",
		"title.length < 8",
		"a[href^=\"http\"]",
		"exclude.test(u)",
		"h1,h2,h3",
	} {
		if !strings.Contains(js, marker) {
			t.Fatalf("generic extraction JS missing %q", marker)
		}
	}
}
