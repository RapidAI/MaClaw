package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopFileBashAndHTTPStayInTheContainer(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	var gotPath, gotAction, gotCommand, gotURL string
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/desktops/file":
			var in struct {
				Action string `json:"action"`
				Path   string `json:"path"`
			}
			_ = json.Unmarshal(raw, &in)
			gotAction, gotPath = in.Action, in.Path
			_ = json.NewEncoder(w).Encode(map[string]string{"output": "Wrote " + in.Path})
		case "/v1/desktops/bash":
			var in struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(raw, &in)
			gotCommand = in.Command
			_ = json.NewEncoder(w).Encode(map[string]string{"output": "hi"})
		case "/v1/desktops/http":
			var in struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(raw, &in)
			gotURL = in.URL
			_ = json.NewEncoder(w).Encode(map[string]any{"body": "page", "status": 200, "content_type": "text/html"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(docker.Close)
	pool := desktopPoolFake(t, newDesktopSettingsMem(), docker.URL)

	call := func(handler http.Handler, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer desktop-api-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	file := call(PostDesktopFileHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","action":"write","path":"~/Desktop/a.c","content":"int main(){}"}`)
	if file.Code != http.StatusOK || gotAction != "write" || gotPath != "/home/desktop/Desktop/a.c" {
		t.Fatalf("file status=%d path=%q action=%q body=%s", file.Code, gotPath, gotAction, file.Body.String())
	}
	pdf := call(PostDesktopFileHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","action":"bytes","path":"~/Desktop/北京天气.pdf"}`)
	if pdf.Code != http.StatusOK || gotAction != "bytes" || gotPath != "/home/desktop/Desktop/北京天气.pdf" {
		t.Fatalf("bytes status=%d path=%q action=%q body=%s", pdf.Code, gotPath, gotAction, pdf.Body.String())
	}
	rejected := call(PostDesktopFileHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","action":"steal","path":"~/Desktop/a.c"}`)
	if rejected.Code == http.StatusOK {
		t.Fatalf("unknown file action was accepted: %s", rejected.Body.String())
	}
	bash := call(PostDesktopBashHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","command":"echo hi"}`)
	if bash.Code != http.StatusOK || gotCommand != "echo hi" {
		t.Fatalf("bash status=%d command=%q body=%s", bash.Code, gotCommand, bash.Body.String())
	}
	page := call(PostDesktopHTTPHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","url":"https://example.com/a"}`)
	if page.Code != http.StatusOK || gotURL != "https://example.com/a" || !strings.Contains(page.Body.String(), `"status":200`) {
		t.Fatalf("http status=%d url=%q body=%s", page.Code, gotURL, page.Body.String())
	}
}
