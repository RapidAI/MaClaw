package codegenproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetUpstreamExtraHeaders(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-XAI-Token-Auth")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	srv := NewServer(":0")
	srv.SetUpstreamExtraHeaders(map[string]string{"X-XAI-Token-Auth": "xai-grok-cli"})
	req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if gotAuth != "xai-grok-cli" {
		t.Fatalf("X-XAI-Token-Auth = %q", gotAuth)
	}

	srv.SetUpstreamExtraHeaders(nil)
	gotAuth = ""
	resp, err = srv.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if gotAuth != "" {
		t.Fatalf("cleared extra header still sent: %q", gotAuth)
	}
}
