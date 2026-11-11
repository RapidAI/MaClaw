package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/botfiles"
)

func botFilesTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "files-test-token")
	old := botFiles
	botFiles = &botfiles.Store{Dir: t.TempDir()}
	t.Cleanup(func() { botFiles = old })
	ident := convAuth{"alice-tok": "alice", "bob-tok": "bob"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/desktop-services/files", PostBotFileCreateHandler(nil))
	mux.HandleFunc("PUT /api/v1/desktop-services/files/{id}/data", PutBotFileDataHandler())
	mux.HandleFunc("GET /api/v1/desktop-services/files/{id}/status", GetBotFileStatusHandler())
	mux.HandleFunc("POST /api/v1/desktop-services/files/{id}/link", PostBotFileLinkHandler())
	mux.HandleFunc("GET /api/v1/bot-files/{id}", GetBotFileHandler(ident))
	mux.HandleFunc("POST /api/v1/bot-files/{id}/link", PostBotFileRefreshHandler(ident))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, method, url string, headers map[string]string, body io.Reader) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestBotFileUploadAndOwnerDownloadWithRange(t *testing.T) {
	srv := botFilesTestServer(t)
	data := bytes.Repeat([]byte("0123456789"), 300_000) // 3 MB
	api := map[string]string{"Authorization": "Bearer files-test-token"}
	raw, _ := json.Marshal(map[string]any{"tenant_id": "tenant_default", "user_id": "alice::desk::bot-a", "instance_id": "bot-a", "name": "big.pdf", "size": len(data)})
	code, out := doJSON(t, "POST", srv.URL+"/api/v1/desktop-services/files", api, bytes.NewReader(raw))
	if code != 200 || out["ok"] != true {
		t.Fatalf("create %d %v", code, out)
	}
	if code, _ := doJSON(t, "POST", srv.URL+"/api/v1/desktop-services/files", nil, bytes.NewReader(raw)); code != http.StatusUnauthorized {
		t.Fatalf("create without token: %d", code)
	}
	id, tok := out["id"].(string), out["upload_token"].(string)
	up := map[string]string{"X-Upload-Token": tok}
	code, out = doJSON(t, "PUT", srv.URL+"/api/v1/desktop-services/files/"+id+"/data?offset=0", up, bytes.NewReader(data[:1_000_000]))
	if code != 200 || out["received"].(float64) != 1_000_000 {
		t.Fatalf("part 1 %d %v", code, out)
	}
	// A retry of the same part is refused with where to resume.
	code, out = doJSON(t, "PUT", srv.URL+"/api/v1/desktop-services/files/"+id+"/data?offset=0", up, bytes.NewReader(data[:10]))
	if code != http.StatusConflict || out["received"].(float64) != 1_000_000 {
		t.Fatalf("stale part %d %v", code, out)
	}
	code, out = doJSON(t, "PUT", srv.URL+"/api/v1/desktop-services/files/"+id+"/data?offset=1000000", up, bytes.NewReader(data[1_000_000:]))
	if code != 200 || out["complete"] != true {
		t.Fatalf("part 2 %d %v", code, out)
	}
	raw, _ = json.Marshal(map[string]any{"tenant_id": "tenant_default", "user_id": "alice"})
	code, out = doJSON(t, "POST", srv.URL+"/api/v1/desktop-services/files/"+id+"/link", api, bytes.NewReader(raw))
	if code != 200 {
		t.Fatalf("link %d %v", code, out)
	}
	link := out["url"].(string)

	get := func(token, path, rng string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Machine-ID", "m1")
		}
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, body
	}
	if resp, _ := get("", link, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no credential: %d", resp.StatusCode)
	}
	if resp, _ := get("bob-tok", link, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other person: %d", resp.StatusCode)
	}
	if resp, _ := get("alice-tok", "/api/v1/bot-files/"+id, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unsigned: %d", resp.StatusCode)
	}
	resp, body := get("alice-tok", link, "bytes=2000000-")
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, data[2_000_000:]) {
		t.Fatalf("range: %d len %d", resp.StatusCode, len(body))
	}
	resp, body = get("alice-tok", link, "")
	if resp.StatusCode != 200 || !bytes.Equal(body, data) || resp.Header.Get("Content-Length") != strconv.Itoa(len(data)) {
		t.Fatalf("full: %d", resp.StatusCode)
	}
	code, out = doJSON(t, "POST", srv.URL+"/api/v1/bot-files/"+id+"/link", map[string]string{"Authorization": "Bearer alice-tok", "X-Machine-ID": "m1"}, nil)
	if code != 200 || out["url"] == "" || out["bot_id"] != "bot-a" {
		t.Fatalf("refresh %d %v", code, out)
	}
}
