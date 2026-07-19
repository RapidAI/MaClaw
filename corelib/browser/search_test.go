package browser

import (
	"context"
	"encoding/json"
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
	hitsByURL map[string]string
	navigated chan string
}

func (f *fakeSearchCDP) reply(conn *websocket.Conn, mu *sync.Mutex, id int64, result interface{}) {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.WriteJSON(map[string]interface{}{"id": id, "result": result})
}

func newFakeSearchCDP(t *testing.T, hitsByURL map[string]string) *fakeSearchCDP {
	t.Helper()
	f := &fakeSearchCDP{hitsByURL: hitsByURL, navigated: make(chan string, 4)}
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
			case "Target.createTarget":
				var p struct {
					URL string `json:"url"`
				}
				_ = json.Unmarshal(req.Params, &p)
				f.navigated <- p.URL
				f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{"targetId": "page-1"})
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
				payload := "[]"
				expr := p.Expression
				engine := "bing"
				if strings.Contains(expr, "MjjYud") {
					engine = "google"
				}
				if v, ok := f.hitsByURL[engine]; ok {
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
