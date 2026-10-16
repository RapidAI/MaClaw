package windsurf

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBearerTokenKeepsKeyPrefix(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, DefaultAPIServer+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "BEARER bearer-looking-key")
	if got := bearerToken(req); got != "bearer-looking-key" {
		t.Fatalf("token = %q", got)
	}
}

func TestMatchesProvider(t *testing.T) {
	if !MatchesProvider("Devin", "") {
		t.Fatal("name Devin should match")
	}
	if !MatchesProvider("custom", DefaultAPIServer) {
		t.Fatal("official host should match")
	}
	if MatchesProvider("OpenAI", "https://api.openai.com/v1") {
		t.Fatal("openai host should not match")
	}
}

func TestTransportChatDoesNotLeaveTheProcess(t *testing.T) {
	var called bool
	client := WrapClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, io.ErrUnexpectedEOF
	})})
	req, err := http.NewRequest(http.MethodPost, DefaultAPIServer+"/v1/chat/completions", strings.NewReader(`{"model":"","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if called {
		t.Fatal("Devin chat reached the inner transport")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "请先完成 Devin 登录") || !strings.Contains(string(raw), "[DONE]") {
		t.Fatalf("body = %s", raw)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
