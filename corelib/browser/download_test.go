package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeCDPServer emulates just enough of Chrome's DevTools endpoints to drive
// downloadViaBrowserAt end to end: /json/version, /json, a browser-level
// websocket (setDownloadBehavior, createTarget/closeTarget, download events)
// and a page-level websocket (Page/Fetch commands + requestPaused events).
type fakeCDPServer struct {
	srv *httptest.Server

	mu           sync.Mutex
	browserConn  *websocket.Conn
	downloadPath string

	fileContent   []byte
	suggestedName string
	// emitOnNavigate controls whether Page.navigate triggers download events.
	emitOnNavigate bool
	// navigateAborts makes Page.navigate return errorText net::ERR_ABORTED
	// in its result, which is how real Chrome reports a navigation that
	// turned into a download.
	navigateAborts bool
	// navigateErrorText, when non-empty (and navigateAborts false), makes
	// Page.navigate return that errorText (e.g. DNS failure).
	navigateErrorText string
	// emitDecoy makes the server emit an unrelated download (different host)
	// completing before ours; the client must not mistake it for ours.
	emitDecoy bool

	// interceptedDisposition records the Content-Disposition the client sent
	// back in Fetch.continueResponse for the intercepted inline-PDF response.
	interceptedDisposition chan string

	// gorilla/websocket permits one concurrent writer; responses (from the
	// read loop) and pushed events (from emitDownloadEvents) run on different
	// goroutines, so each connection gets a dedicated write mutex.
	browserWriteMu sync.Mutex
	pageWriteMu    sync.Mutex
}

func newFakeCDPServer(t *testing.T, fileContent []byte, suggestedName string) *fakeCDPServer {
	t.Helper()
	f := &fakeCDPServer{
		fileContent:            fileContent,
		suggestedName:          suggestedName,
		emitOnNavigate:         true,
		interceptedDisposition: make(chan string, 1),
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
		f.serveBrowserWS(conn)
	})
	mux.HandleFunc("/devtools/page/page-1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.servePageWS(conn)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type fakeCDPRequest struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (f *fakeCDPServer) reply(conn *websocket.Conn, mu *sync.Mutex, id int64, result interface{}) {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.WriteJSON(map[string]interface{}{"id": id, "result": result})
}

func (f *fakeCDPServer) serveBrowserWS(conn *websocket.Conn) {
	for {
		var req fakeCDPRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		switch req.Method {
		case "Browser.setDownloadBehavior":
			var p struct {
				Behavior     string `json:"behavior"`
				DownloadPath string `json:"downloadPath"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.DownloadPath != "" {
				f.mu.Lock()
				f.downloadPath = p.DownloadPath
				f.mu.Unlock()
			}
			f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{})
		case "Target.createTarget":
			f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{"targetId": "page-1"})
		case "Target.closeTarget", "Browser.getVersion":
			f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{})
		default:
			f.reply(conn, &f.browserWriteMu, req.ID, map[string]interface{}{})
		}
	}
}

// emitDownloadEvents simulates Chrome's side of a completed download: the
// file lands in the downloadPath captured earlier, then willBegin/progress
// events are pushed on the browser-level connection.
func (f *fakeCDPServer) emitDownloadEvents() {
	f.mu.Lock()
	dir := f.downloadPath
	conn := f.browserConn
	f.mu.Unlock()
	if dir == "" || conn == nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	f.mu.Lock()
	decoy := f.emitDecoy
	f.mu.Unlock()
	if decoy {
		// An unrelated download on another host finishing first — this must
		// not complete the client's wait.
		_ = os.WriteFile(filepath.Join(dir, "other.zip"), []byte("decoy"), 0o644)
		for _, ev := range []map[string]interface{}{
			{"method": "Browser.downloadWillBegin", "params": map[string]interface{}{
				"guid": "g-0", "url": "http://other-host.invalid/x.zip", "suggestedFilename": "other.zip",
			}},
			{"method": "Browser.downloadProgress", "params": map[string]interface{}{
				"guid": "g-0", "state": "completed", "receivedBytes": 5, "totalBytes": 5,
			}},
		} {
			f.browserWriteMu.Lock()
			_ = conn.WriteJSON(ev)
			f.browserWriteMu.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, f.suggestedName), f.fileContent, 0o644)
	events := []map[string]interface{}{
		{"method": "Browser.downloadWillBegin", "params": map[string]interface{}{
			"guid": "g-1", "url": "http://example.com/paper.pdf", "suggestedFilename": f.suggestedName,
		}},
		{"method": "Browser.downloadProgress", "params": map[string]interface{}{
			"guid": "g-1", "state": "inProgress", "receivedBytes": len(f.fileContent) / 2, "totalBytes": len(f.fileContent),
		}},
		{"method": "Browser.downloadProgress", "params": map[string]interface{}{
			"guid": "g-1", "state": "completed", "receivedBytes": len(f.fileContent), "totalBytes": len(f.fileContent),
		}},
	}
	for _, ev := range events {
		f.browserWriteMu.Lock()
		_ = conn.WriteJSON(ev)
		f.browserWriteMu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fakeCDPServer) servePageWS(conn *websocket.Conn) {
	writeMu := &f.pageWriteMu
	for {
		var req fakeCDPRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		switch req.Method {
		case "Fetch.enable":
			f.reply(conn, writeMu, req.ID, map[string]interface{}{})
			// Immediately pause an inline-PDF document response: the client
			// must rewrite it to Content-Disposition: attachment.
			writeMu.Lock()
			_ = conn.WriteJSON(map[string]interface{}{
				"method": "Fetch.requestPaused",
				"params": map[string]interface{}{
					"requestId":          "req-1",
					"resourceType":       "Document",
					"responseStatusCode": 200,
					"responseHeaders": []map[string]string{
						{"name": "Content-Type", "value": "application/pdf"},
					},
					"request": map[string]string{"url": "http://example.com/paper.pdf"},
				},
			})
			writeMu.Unlock()
		case "Fetch.continueResponse":
			var p struct {
				ResponseHeaders []cdpHeader `json:"responseHeaders"`
			}
			_ = json.Unmarshal(req.Params, &p)
			f.interceptedDisposition <- headerValue(p.ResponseHeaders, "Content-Disposition")
			f.reply(conn, writeMu, req.ID, map[string]interface{}{})
		case "Page.navigate":
			f.mu.Lock()
			abort := f.navigateAborts
			navErr := f.navigateErrorText
			emit := f.emitOnNavigate
			f.mu.Unlock()
			switch {
			case abort:
				f.reply(conn, writeMu, req.ID, map[string]interface{}{
					"frameId": "frame-1", "errorText": "net::ERR_ABORTED at http://example.com/paper.pdf",
				})
			case navErr != "":
				f.reply(conn, writeMu, req.ID, map[string]interface{}{
					"errorText": navErr,
				})
			default:
				f.reply(conn, writeMu, req.ID, map[string]interface{}{"frameId": "frame-1"})
			}
			if emit {
				go f.emitDownloadEvents()
			}
		default:
			f.reply(conn, writeMu, req.ID, map[string]interface{}{})
		}
	}
}

func TestDownloadViaBrowserEndToEnd(t *testing.T) {
	content := []byte("%PDF-1.7 fake-browser-download-body")
	fake := newFakeCDPServer(t, content, "paper.pdf")

	dest := filepath.Join(t.TempDir(), "out", "saved.pdf")
	res, err := downloadViaBrowserAt(context.Background(), fake.srv.URL,
		"http://example.com/paper.pdf", dest, 30)
	if err != nil {
		t.Fatalf("downloadViaBrowserAt: %v", err)
	}
	if res.SavedTo != dest {
		t.Fatalf("SavedTo=%q want %q", res.SavedTo, dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: %q", got)
	}
	if res.Bytes != int64(len(content)) {
		t.Fatalf("Bytes=%d want %d", res.Bytes, len(content))
	}

	// The inline PDF response must have been rewritten to an attachment.
	select {
	case cd := <-fake.interceptedDisposition:
		if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "paper.pdf") {
			t.Fatalf("unexpected Content-Disposition: %q", cd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client never answered Fetch.requestPaused with continueResponse")
	}
}

func TestDownloadViaBrowserTimesOutWithoutDownload(t *testing.T) {
	fake := newFakeCDPServer(t, nil, "x.bin")
	fake.emitOnNavigate = false // no download events → must time out
	dest := filepath.Join(t.TempDir(), "x.bin")
	_, err := downloadViaBrowserAt(context.Background(), fake.srv.URL,
		"http://example.com/no-download", dest, 1)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("no file should exist on timeout, stat err=%v", statErr)
	}
}

func TestDownloadViaBrowserToleratesNavigateAborted(t *testing.T) {
	// Real Chrome answers Page.navigate with net::ERR_ABORTED when the
	// navigation immediately becomes a download — the happy path for
	// attachment responses.
	content := []byte("attachment-body")
	fake := newFakeCDPServer(t, content, "direct.bin")
	fake.navigateAborts = true
	dest := filepath.Join(t.TempDir(), "direct.bin")
	res, err := downloadViaBrowserAt(context.Background(), fake.srv.URL,
		"http://example.com/direct.bin", dest, 30)
	if err != nil {
		t.Fatalf("downloadViaBrowserAt: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(content) {
		t.Fatalf("content mismatch: %q", got)
	}
	_ = res
}

func TestDownloadViaBrowserIgnoresUnrelatedDownload(t *testing.T) {
	content := []byte("our-pdf-body")
	fake := newFakeCDPServer(t, content, "paper.pdf")
	fake.emitDecoy = true // unrelated download completes first on another host
	dest := filepath.Join(t.TempDir(), "paper.pdf")
	res, err := downloadViaBrowserAt(context.Background(), fake.srv.URL,
		"http://example.com/paper.pdf", dest, 30)
	if err != nil {
		t.Fatalf("downloadViaBrowserAt: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(content) {
		t.Fatalf("picked the wrong download: %q", got)
	}
	if res.FileName != "paper.pdf" {
		t.Fatalf("FileName=%q want paper.pdf", res.FileName)
	}
}

func TestDownloadViaBrowserNavigateHardFailure(t *testing.T) {
	fake := newFakeCDPServer(t, nil, "x.bin")
	fake.navigateErrorText = "net::ERR_NAME_NOT_RESOLVED"
	fake.emitOnNavigate = false
	dest := filepath.Join(t.TempDir(), "x.bin")
	_, err := downloadViaBrowserAt(context.Background(), fake.srv.URL,
		"http://nonexistent.invalid/x.bin", dest, 30)
	if err == nil {
		t.Fatal("expected navigation failure")
	}
	if !strings.Contains(err.Error(), "ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("error should surface the CDP errorText, got: %v", err)
	}
	// Must fail fast, not wait out the full timeout.
}

func TestMoveFileReplacesExistingDest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	_ = os.WriteFile(src, []byte("new-content"), 0o644)
	_ = os.WriteFile(dst, []byte("old-content"), 0o644)
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new-content" {
		t.Fatalf("dst not replaced: %q", got)
	}
}

func TestShouldForceAttachment(t *testing.T) {
	cases := []struct {
		name string
		rt   string
		code int
		ct   string
		cd   string
		want bool
	}{
		{"inline pdf document", "Document", 200, "application/pdf", "", true},
		{"octet stream", "Document", 200, "application/octet-stream", "", true},
		{"already attachment", "Document", 200, "application/pdf", `attachment; filename="a.pdf"`, false},
		{"html page", "Document", 200, "text/html; charset=utf-8", "", false},
		{"json api", "Document", 200, "application/json", "", false},
		{"subresource ignored", "Image", 200, "application/pdf", "", false},
		{"redirect ignored", "Document", 302, "application/pdf", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := shouldForceAttachment(tc.rt, tc.code, tc.ct, tc.cd, "http://x/f.pdf", "http://x/f.pdf")
			if got != tc.want {
				t.Fatalf("shouldForceAttachment = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestDownloadFilenameFromCD(t *testing.T) {
	if got := downloadFilenameFromCD(`inline; filename="report 2026.pdf"`, "http://x/a.pdf", ""); got != "report 2026.pdf" {
		t.Fatalf("from disposition: %q", got)
	}
	if got := downloadFilenameFromCD("", "http://x/papers/b.pdf?dl=1", ""); got != "b.pdf" {
		t.Fatalf("from url: %q", got)
	}
	if got := downloadFilenameFromCD("", "http://x/", ""); got != "download.bin" {
		t.Fatalf("fallback: %q", got)
	}
	// Path traversal attempts must collapse to a plain base name.
	if got := downloadFilenameFromCD(`attachment; filename="../../evil.exe"`, "http://x/", ""); got != "evil.exe" {
		t.Fatalf("sanitized: %q", got)
	}
}

func TestSetCDPHeader(t *testing.T) {
	headers := []cdpHeader{{Name: "Content-Type", Value: "application/pdf"}}
	out := setCDPHeader(headers, "Content-Disposition", "attachment")
	if v := headerValue(out, "content-disposition"); v != "attachment" {
		t.Fatalf("insert: %q", v)
	}
	out = setCDPHeader(out, "Content-DISPOSITION", "attachment; filename=\"a.pdf\"")
	if v := headerValue(out, "Content-Disposition"); v != `attachment; filename="a.pdf"` {
		t.Fatalf("replace: %q", v)
	}
	if n := 0; len(out) != 2 {
		t.Fatalf("should replace not duplicate: %v (n=%d)", out, n)
	}
}

func TestSanitizeFilenameWindowsReserved(t *testing.T) {
	cases := map[string]string{
		"CON":        "_CON",
		"con.pdf":    "_con.pdf",
		"nul":        "_nul",
		"COM1.txt":   "_COM1.txt",
		"lpt9":       "_lpt9",
		"normal.pdf": "normal.pdf",
		"file.":      "file",
		"  ":         "download.bin",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
