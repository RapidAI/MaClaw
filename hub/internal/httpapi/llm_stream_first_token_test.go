package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamFirstTokenGuardFailsOverSilentProvider(t *testing.T) {
	t.Setenv("MACLAW_LLM_FIRST_TOKEN_TIMEOUT_SEC", "0.3")
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, ": ping\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer silent.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": hi\ndata: {\"x\":1}\n\ndata: [DONE]\n\n")
	}))
	defer fast.Close()

	base := httptest.NewRequest(http.MethodPost, "/", nil)
	open := func(r *http.Request, url string) *http.Response {
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	start := time.Now()
	req, g := newStreamFirstTokenGuard(base, true)
	defer g.cancel()
	if err := g.awaitFirstData(open(req, silent.URL), "slow"); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("silent provider not cut: err=%v after %s", err, time.Since(start))
	}
	req2, g2 := newStreamFirstTokenGuard(base, true)
	defer g2.cancel()
	resp := open(req2, fast.URL)
	if err := g2.awaitFirstData(resp, "fast"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // past the limit: a started stream is not cut
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), ": hi\ndata: {\"x\":1}") || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("body %q", body)
	}
	_, g3 := newStreamFirstTokenGuard(base, false)
	defer g3.cancel()
	if g3.timer != nil {
		t.Fatal("last provider must not get a deadline")
	}
}
