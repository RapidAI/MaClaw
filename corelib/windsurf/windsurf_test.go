package windsurf

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTokenFromPasteReadsCallbackURL(t *testing.T) {
	got, err := TokenFromPaste("  \"sk-ws-pasted\"  ")
	if err != nil || got != "sk-ws-pasted" {
		t.Fatalf("raw = %q %v", got, err)
	}
	got, err = TokenFromPaste("http://127.0.0.1:9/auth?state=abc&code=devin-code-example")
	if err != nil || got != "devin-code-example" {
		t.Fatalf("query = %q %v", got, err)
	}
	got, err = TokenFromPaste("http://127.0.0.1:9/auth#access_token=eyJ.test.sig&state=abc")
	if err != nil || got != "eyJ.test.sig" {
		t.Fatalf("fragment = %q %v", got, err)
	}
	if _, err := TokenFromPaste("https://windsurf.com/devin/account/login?client_id=x"); err == nil {
		t.Fatal("login page was accepted as a token")
	}
}

func TestFragmentBridgeKeepsQueryState(t *testing.T) {
	if !strings.Contains(fragmentBridgeHTML, "location.search") {
		t.Fatal("fragment bridge drops the query state")
	}
	if !strings.Contains(fragmentBridgeHTML, "%2B") {
		t.Fatal("fragment bridge drops plus signs")
	}
	if !strings.Contains(fragmentBridgeHTML, `replace(/^\?/`) {
		t.Fatal("fragment bridge keeps a leading question mark")
	}
}

func TestLoginListenerSkipsSchemeWord(t *testing.T) {
	listener, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	req, err := http.NewRequest(http.MethodGet, listener.authURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	redirect := req.URL.Query().Get("redirect_uri")
	state := req.URL.Query().Get("state")
	resp, err := http.Get(redirect + "?token=bearer&state=" + state + "&code=devin-code-example-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	token, err := listener.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if token != "devin-code-example-0123456789" {
		t.Fatalf("token = %q", token)
	}
}

func TestLoginURLUsesDevinAccountPage(t *testing.T) {
	raw := loginURL(Website, "http://127.0.0.1:9/auth", "state-1")
	if !strings.Contains(raw, "https://windsurf.com/devin/account/login?") {
		t.Fatalf("url = %s", raw)
	}
	if !strings.Contains(raw, "client_id="+ClientID) {
		t.Fatalf("missing client id: %s", raw)
	}
	if !strings.Contains(raw, "redirect_uri=") || !strings.Contains(raw, "127.0.0.1") {
		t.Fatalf("missing redirect: %s", raw)
	}
}

func TestLoginListenerReturnsToken(t *testing.T) {
	listener, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	req, err := http.NewRequest(http.MethodGet, listener.authURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	redirect := req.URL.Query().Get("redirect_uri")
	state := req.URL.Query().Get("state")
	resp, err := http.Get(redirect + "?state=" + state + "&access_token=eyJ.test.sig")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	token, err := listener.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if token != "eyJ.test.sig" {
		t.Fatalf("token = %q", token)
	}
}

func TestLoginListenerKeepsWaitingForFragmentState(t *testing.T) {
	listener, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	req, err := http.NewRequest(http.MethodGet, listener.authURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	redirect := req.URL.Query().Get("redirect_uri")
	state := req.URL.Query().Get("state")
	resp, err := http.Get(redirect + "?code=devin-code-example-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp, err = http.Get(redirect + "?state=other&state=" + state + "&code=devin-code-example-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merged status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	token, err := listener.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if token != "devin-code-example-0123456789" {
		t.Fatalf("token = %q", token)
	}
}

func TestLoginListenerRejectsMismatchedState(t *testing.T) {
	listener, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	req, err := http.NewRequest(http.MethodGet, listener.authURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	redirect := req.URL.Query().Get("redirect_uri")
	mismatch := redirect + "?state=wrong&code=devin-code-example-0123456789"
	resp, err := http.Get(mismatch)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "retry=true") {
		t.Fatalf("first mismatch status=%d body=%s", resp.StatusCode, body)
	}
	resp, err = http.Get(mismatch + "&devin_checked=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := listener.Wait(ctx); err == nil || !strings.Contains(err.Error(), "状态不匹配") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveFirebaseAndAuth1AndAPIKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/exa.seat_management_pb.SeatManagementService/RegisterUser", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["firebase_id_token"] == "" {
			t.Errorf("missing firebase token")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"api_key": "sk-ws-from-register", "name": "Ada", "api_server_url": "https://server.codeium.com",
		})
	})
	mux.HandleFunc("/exa.seat_management_pb.SeatManagementService/WindsurfPostAuth", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Devin-Auth1-Token") == "" {
			t.Errorf("missing auth1 header")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"sessionToken": "devin-session-token$abc"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ep := serviceEndpoints{website: srv.URL, register: srv.URL, webBackend: srv.URL}

	got, err := resolveToken(context.Background(), ep, "eyJhbGciOiJub25lIn0.eyJleHAiOjF9.sig")
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "sk-ws-from-register" || got.Name != "Ada" || got.APIServerURL != DefaultAPIServer {
		t.Fatalf("firebase session = %+v", got)
	}
	got, err = resolveToken(context.Background(), ep, "auth1_example")
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "devin-session-token$abc" || got.APIServerURL != SelfServeAPIServer {
		t.Fatalf("auth1 session = %+v", got)
	}
	got, err = resolveToken(context.Background(), ep, "sk-ws-pasted")
	if err != nil || got.APIServerURL != DefaultAPIServer {
		t.Fatalf("api key session = %+v %v", got, err)
	}
}

func TestExchangeDevinCode(t *testing.T) {
	const code = "devin-code-example-0123456789abcdef"
	mux := http.NewServeMux()
	mux.HandleFunc("/exa.seat_management_pb.SeatManagementService/ExchangeDevinCode", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "" {
			t.Errorf("authorization code was sent as an API key")
		}
		raw, _ := io.ReadAll(r.Body)
		fields, err := iterFields(raw)
		if err != nil || fieldString(fields, 1) != code {
			t.Fatalf("code field err=%v", err)
		}
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(encodeString(1, "devin-session-token$fromcode"))
	})
	mux.HandleFunc("/exa.seat_management_pb.SeatManagementService/GetCurrentUserName", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "devin-session-token$fromcode" {
			t.Errorf("missing session auth header")
		}
		_, _ = w.Write(encodeString(1, "Ada"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ep := serviceEndpoints{apiServer: srv.URL}
	got, err := resolveToken(context.Background(), ep, code)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "devin-session-token$fromcode" || got.Name != "Ada" || got.APIServerURL != SelfServeAPIServer {
		t.Fatalf("session = %+v", got)
	}
	if _, err := resolveToken(context.Background(), ep, "short"); err == nil {
		t.Fatal("short token was accepted")
	}
}

func TestListModelsAndChat(t *testing.T) {
	resetCaches()
	jwt := testJWT(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/exa.auth_pb.AuthService/GetUserJwt", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sk-ws-test" {
			t.Errorf("jwt missing X-Api-Key")
		}
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(encodeString(1, jwt))
	})
	mux.HandleFunc("/exa.api_server_pb.ApiServerService/GetCascadeModelConfigs", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		fields, err := iterFields(raw)
		if err != nil {
			t.Errorf("catalog proto: %v", err)
		} else if meta, ok := bytesField(fields, 1); !ok {
			t.Errorf("catalog missing metadata")
		} else if sub, err := iterFields(meta); err != nil || fieldString(sub, 21) != jwt {
			t.Errorf("catalog jwt = %q", fieldString(sub, 21))
		}
		model := append(encodeString(1, "SWE 1.6"), encodeString(22, "swe-1-6")...)
		_, _ = w.Write(encodeBytes(1, model))
	})
	mux.HandleFunc("/exa.api_server_pb.ApiServerService/GetChatMessage", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) < 5 || raw[0]&0x01 == 0 {
			t.Errorf("expected gzip connect frame, flags=%d len=%d", raw[0], len(raw))
		}
		zr, err := gzip.NewReader(strings.NewReader(string(raw[5:])))
		if err != nil {
			t.Errorf("gunzip: %v", err)
		} else {
			payload, _ := io.ReadAll(zr)
			_ = zr.Close()
			fields, err := iterFields(payload)
			if err != nil {
				t.Errorf("request proto: %v", err)
			} else if fieldString(fields, 21) != "swe-1-6" {
				t.Errorf("model field = %q", fieldString(fields, 21))
			}
		}
		frame, _ := frameConnect(encodeString(3, "hello"), false)
		trailer, _ := frameConnect([]byte(`{}`), false)
		trailer[0] |= 0x02
		_, _ = w.Write(frame)
		_, _ = w.Write(trailer)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	models, err := ListModels(context.Background(), srv.URL, "sk-ws-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "swe-1-6" || models[0].Name != "SWE 1.6" {
		t.Fatalf("models = %+v", models)
	}

	body := []byte(`{"model":"swe-1-6","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-test", "", body, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Choices) != 1 || parsed.Choices[0].Message.Content != "hello" {
		t.Fatalf("body = %s", raw)
	}
}

func TestFieldTagPast15IsVarint(t *testing.T) {
	encoded := encodeString(21, "gpt")
	fields, err := iterFields(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if fieldString(fields, 21) != "gpt" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestListModelsFallsBackWhenAPIKeyRejected(t *testing.T) {
	resetCaches()
	var badHits, goodHits int
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badHits++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer bad.Close()
	jwt := testJWT(t)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodHits++
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		model := append(encodeString(1, "SWE 1.6"), encodeString(22, "swe-1-6")...)
		_, _ = w.Write(encodeBytes(1, model))
	}))
	defer good.Close()
	prev := devinFallbackHook
	devinFallbackHook = func(server string) (string, bool) {
		if server == bad.URL {
			return good.URL, true
		}
		return "", false
	}
	t.Cleanup(func() {
		devinFallbackHook = prev
		resetCaches()
	})

	models, err := ListModels(context.Background(), bad.URL, "sk-ws-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "swe-1-6" {
		t.Fatalf("models = %+v", models)
	}
	if badHits != 1 || goodHits != 2 {
		t.Fatalf("hits bad=%d good=%d", badHits, goodHits)
	}
	if NotedAPIServer("sk-ws-fallback") != "" {
		t.Fatal("test server was remembered")
	}
}

func TestListModelsDoesNotRetryTestServer(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer srv.Close()

	_, err := ListModels(context.Background(), srv.URL, "sk-ws-stay")
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("test server hits = %d", hits)
	}
}

func TestNotedDevinHostOverridesCodeium(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	RememberDevinAPIServer("sk-ws-01-noted", SelfServeAPIServer)
	if got := routeDevinAPIServer("sk-ws-01-noted", DefaultAPIServer); got != SelfServeAPIServer {
		t.Fatalf("routed = %s", got)
	}
	if got := CanonicalAPIServer("sk-ws-01-noted", DefaultAPIServer); got != SelfServeAPIServer {
		t.Fatalf("canonical = %s", got)
	}
	RememberDevinAPIServer("sk-ws-01-noted", "http://127.0.0.1:9")
	if got := NotedAPIServer("sk-ws-01-noted"); got != SelfServeAPIServer {
		t.Fatalf("unofficial host replaced memory: %s", got)
	}
	if got := routeDevinAPIServer("devin-session-token$x", DefaultAPIServer); got != SelfServeAPIServer {
		t.Fatalf("session token routed = %s", got)
	}
	if _, ok := officialDevinHostFallback("http://127.0.0.1:9"); ok {
		t.Fatal("test server has a fallback")
	}
	if got, ok := officialDevinHostFallback(DefaultAPIServer); !ok || got != SelfServeAPIServer {
		t.Fatalf("codeium fallback = %s %v", got, ok)
	}
}

func TestMintDevinSessionToken(t *testing.T) {
	const want = "devin-session-token$minted"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sk-ws-mint" {
			t.Errorf("mint missing X-Api-Key")
		}
		if !strings.HasSuffix(r.URL.Path, "/GetSelfDevinSessionToken") {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(encodeString(1, want))
	}))
	defer srv.Close()
	got, err := mintDevinSession(context.Background(), srv.URL, "sk-ws-mint")
	if err != nil || got != want {
		t.Fatalf("mint = %q %v", got, err)
	}
}

func TestResolveAccessUsesMintedSession(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	rememberMintedSession("sk-ws-minted", "devin-session-token$saved")
	server, key := resolveAccess(context.Background(), DefaultAPIServer, "sk-ws-minted")
	if server != SelfServeAPIServer || key != "devin-session-token$saved" {
		t.Fatalf("resolved %s %s", server, key)
	}
	server, key = resolveAccess(context.Background(), "http://127.0.0.1:9", "sk-ws-local")
	if server != "http://127.0.0.1:9" || key != "sk-ws-local" {
		t.Fatalf("test server resolved %s %s", server, key)
	}
}

func TestCanonicalAPIServer(t *testing.T) {
	if got := CanonicalAPIServer("devin-session-token$x", ""); got != SelfServeAPIServer {
		t.Fatalf("session server = %s", got)
	}
	if got := CanonicalAPIServer("devin-session-token$x", DefaultAPIServer); got != SelfServeAPIServer {
		t.Fatalf("session token kept codeium host %s", got)
	}
	if got := CanonicalAPIServer("sk-ws-x", "https://evil.example"); got != DefaultAPIServer {
		t.Fatalf("rejected foreign host = %s", got)
	}
	if got := CanonicalAPIServer("sk-ws-x", "https://server.codeium.com"); got != DefaultAPIServer {
		t.Fatalf("kept = %s", got)
	}
}

func TestChatRequestUsesPromptID(t *testing.T) {
	raw := buildChatRequest("sk", "", "swe-1-6", "cascade-1", "prompt-1", "session-1", 9, "trig", chatRequest{})
	fields, err := iterFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	if fieldString(fields, 17) != "prompt-1" {
		t.Fatalf("prompt id = %q", fieldString(fields, 17))
	}
	if fieldString(fields, 21) != "swe-1-6" {
		t.Fatalf("model = %q", fieldString(fields, 21))
	}
	if got := fieldString(fields, 22); got != "" {
		t.Fatalf("execution id = %q", got)
	}
}

func TestStreamCompletesWithoutEndFrame(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		frame, err := frameConnect(encodeString(3, "hello"), false)
		if err != nil {
			t.Errorf("frame: %v", err)
		}
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	body := []byte(`{"model":"swe-1-6","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-stream", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "hello") || !strings.Contains(string(raw), "data: [DONE]") {
		t.Fatalf("stream = %s", raw)
	}
}

func TestStreamToolArgsKeepCompletionID(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	callID := "call_888daab19b294bfb9d83d9e7#cef2ba2f278e47b2bf283ce48a888252"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		tool := encodeBytes(6, append(append(encodeString(1, callID), encodeString(2, "web_search")...), encodeString(3, `{"q":"北京天气"}`)...))
		frame, err := frameConnect(tool, false)
		if err != nil {
			t.Errorf("frame: %v", err)
		}
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	body := []byte(`{"model":"swe-2-high","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-tool", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	sawTool := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("chunk: %v %s", err, payload)
		}
		if chunk.ID != "chatcmpl-devin" {
			t.Fatalf("response id = %q payload = %s", chunk.ID, payload)
		}
		if strings.Contains(payload, callID) {
			sawTool = true
		}
	}
	if !sawTool || !strings.Contains(text, "web_search") || !strings.Contains(text, `"finish_reason":"tool_calls"`) {
		t.Fatalf("tool call missing: %s", text)
	}
}

func TestStreamCutToolIsNotFinishedCall(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	callID := "call_cut#tool"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		tool, err := frameConnect(encodeBytes(6, append(encodeString(1, callID), encodeString(2, "generate_pdf")...)), false)
		if err != nil {
			t.Errorf("frame: %v", err)
			return
		}
		trailer, err := frameConnect([]byte(`{"error":{"message":"stream broke"}}`), false)
		if err != nil {
			t.Errorf("trailer: %v", err)
			return
		}
		trailer[0] |= 0x02
		_, _ = w.Write(tool)
		_, _ = w.Write(trailer)
	}))
	defer srv.Close()

	body := []byte(`{"model":"swe-2-high","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-cut-tool", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if !strings.Contains(text, callID) || !strings.Contains(text, `"finish_reason":"length"`) || strings.Contains(text, `"finish_reason":"tool_calls"`) {
		t.Fatalf("stream = %s", text)
	}
}

func TestReadChatEventsStopsOnShortFrame(t *testing.T) {
	tool, err := frameConnect(encodeBytes(6, append(encodeString(1, "call-1"), encodeString(2, "web_search")...)), false)
	if err != nil {
		t.Fatal(err)
	}
	body := append(append([]byte{}, tool...), 0, 0, 0, 0, 8)
	var sawTool bool
	err = readChatEvents(bytes.NewReader(body), func(event chatEvent) error {
		if event.kind == "tool_start" {
			sawTool = true
		}
		return nil
	})
	if !sawTool || err == nil || !strings.Contains(err.Error(), "对话中断") {
		t.Fatalf("saw=%v err=%v", sawTool, err)
	}
}

func TestStreamSurfacesTrailerError(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		frame, err := frameConnect([]byte(`{"error":{"message":"model rejected"}}`), false)
		if err != nil {
			t.Errorf("frame: %v", err)
		}
		frame[0] |= 0x02
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	body := []byte(`{"model":"swe-1-6","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-trailer", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if !strings.Contains(text, "model rejected") || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream = %s", text)
	}
}

func TestCompletionBudgetIsMaxTokens(t *testing.T) {
	fields, err := iterFields(encodeCompletion(chatRequest{maxTokens: 4096}))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fieldVarint(fields, 2)
	if !ok || got != 4096 {
		t.Fatalf("max tokens = %d ok=%v", got, ok)
	}
	newlines, ok := fieldVarint(fields, 3)
	if !ok || newlines != maxChatNewlines || newlines == got {
		t.Fatalf("max newlines = %d tokens = %d", newlines, got)
	}
}

func TestStopReasonIgnoresIncomplete(t *testing.T) {
	if events := decodeChatFrame(encodeVarintField(5, 1)); len(events) != 0 {
		t.Fatalf("incomplete stop = %+v", events)
	}
	events := decodeChatFrame(encodeVarintField(5, 5))
	if len(events) != 1 || events[0].reason != "length" {
		t.Fatalf("max newlines = %+v", events)
	}
}

func TestStreamOpenFailureStillCompletes(t *testing.T) {
	body := []byte(`{"model":"","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), "http://127.0.0.1/chat/completions", "sk-ws-x", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if resp.StatusCode != http.StatusOK || !strings.Contains(text, "请先选择 Devin 模型") || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("status %d body %s", resp.StatusCode, text)
	}
}

func TestToolArgsStayOnSameCall(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		start, _ := frameConnect(encodeBytes(6, append(encodeString(1, "call-1"), encodeString(2, "read_file")...)), false)
		args, _ := frameConnect(encodeBytes(6, encodeString(3, `{"path":"a.go"}`)), false)
		_, _ = w.Write(start)
		_, _ = w.Write(args)
	}))
	defer srv.Close()

	body := []byte(`{"model":"swe-1-6","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-tool", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if strings.Contains(text, `"index":1`) || !strings.Contains(text, `"index":0`) || !strings.Contains(text, `a.go`) || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream = %s", text)
	}
}

func TestChatDisablesMCPAndMarksCustomTools(t *testing.T) {
	raw := buildChatRequest("sk", "", "swe-1-6", "cascade", "prompt", "session", 1, "trig", chatRequest{
		tools: []toolDef{
			{name: "read_file", description: "read", parameters: `{"type":"object"}`},
			{name: "mcp__srv__do", parameters: "{}"},
		},
	})
	fields, err := iterFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	var sawRead, sawMCP, sawDisable bool
	for _, field := range fields {
		if field.num == 9 && field.wire == 2 {
			exp, _ := field.value.([]byte)
			inner, err := iterFields(exp)
			if err != nil {
				t.Fatal(err)
			}
			packed, ok := bytesField(inner, 2)
			if !ok {
				t.Fatal("missing disabled experiments")
			}
			value, _, err := decodeVarint(packed, 0)
			if err != nil || value != cascadeEnableMCPTools {
				t.Fatalf("disabled experiment = %d %v", value, err)
			}
			sawDisable = true
		}
		if field.num == 10 && field.wire == 2 {
			tool, _ := field.value.([]byte)
			sub, err := iterFields(tool)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := fieldVarint(sub, 9); ok {
				t.Fatal("tool was marked as a custom tool")
			}
			switch fieldString(sub, 1) {
			case "read_file":
				sawRead = true
			case "mcp__srv__do":
				sawMCP = true
			}
		}
	}
	if !sawDisable || !sawRead || !sawMCP {
		t.Fatalf("disable=%v read=%v mcp=%v", sawDisable, sawRead, sawMCP)
	}
}

func TestStreamRetriesWithoutToolsOnMCPError(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	var chats int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		chats++
		raw, _ := io.ReadAll(r.Body)
		zr, err := gzip.NewReader(strings.NewReader(string(raw[5:])))
		if err != nil {
			t.Errorf("gunzip: %v", err)
			return
		}
		payload, _ := io.ReadAll(zr)
		_ = zr.Close()
		fields, err := iterFields(payload)
		if err != nil {
			t.Errorf("request: %v", err)
		}
		var tools int
		for _, field := range fields {
			if field.num == 10 {
				tools++
			}
		}
		if chats == 1 && tools == 0 {
			t.Errorf("first request dropped tools")
		}
		if chats == 2 && tools != 0 {
			t.Errorf("retry still sent %d tools", tools)
		}
		if chats == 1 {
			frame, _ := frameConnect([]byte(`{"error":{"message":"Unable to process request due to an MCP configuration issue."}}`), false)
			frame[0] |= 0x02
			_, _ = w.Write(frame)
			return
		}
		frame, _ := frameConnect(encodeString(3, "hello"), false)
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	body := []byte(`{"model":"swe-1-6","stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`)
	resp, err := Upstream{}.DoChatCompletions(context.Background(), srv.URL+"/chat/completions", "sk-ws-mcp", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "data: [DONE]") || strings.Contains(text, "MCP configuration") {
		t.Fatalf("chats=%d stream=%s", chats, text)
	}
}

func TestAgentToolChoiceAndInlineImage(t *testing.T) {
	body := []byte(`{
		"model":"swe-1-6",
		"parallel_tool_calls": false,
		"tool_choice": {"type":"function","function":{"name":"read_file"}},
		"tools": [
			{"type":"function","function":{"name":"read_file","description":"read","parameters":{"type":"object"}}},
			{"type":"function","function":{"name":"grep","description":"search","parameters":{"type":"object"}}}
		],
		"messages":[{"role":"user","content":[
			{"type":"text","text":"look"},
			{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}
		]}]
	}`)
	req, err := parseOpenAIChat(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.forcedTool != "read_file" || !req.disableParallel || len(req.tools) != 1 || req.tools[0].name != "read_file" {
		t.Fatalf("choice = %+v", req)
	}
	if len(req.messages) != 1 || len(req.messages[0].images) != 1 || req.messages[0].images[0].mime != "image/png" || req.messages[0].images[0].data != "aGVsbG8=" {
		t.Fatalf("message = %+v", req.messages)
	}
	raw := buildChatRequest("sk", "", req.model, "c", "p", "s", 1, "t", req)
	fields, err := iterFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := fieldVarint(fields, 11); !ok || got != 1 {
		t.Fatalf("disable parallel = %d ok=%v", got, ok)
	}
	choice, ok := fieldBytes(fields, 12)
	if !ok || fieldString(mustFields(t, choice), 2) != "read_file" {
		t.Fatalf("tool choice = %q", choice)
	}
	var toolNames []string
	var sawImage bool
	for _, field := range fields {
		if field.num == 10 {
			tool := fieldBytesOf(field)
			toolNames = append(toolNames, fieldString(mustFields(t, tool), 1))
		}
		if field.num != 3 {
			continue
		}
		prompt := mustFields(t, fieldBytesOf(field))
		image, ok := fieldBytes(prompt, 10)
		if !ok {
			continue
		}
		sawImage = true
		imageFields := mustFields(t, image)
		if fieldString(imageFields, 1) != "aGVsbG8=" || fieldString(imageFields, 2) != "image/png" {
			t.Fatalf("image = %#v", imageFields)
		}
	}
	if !sawImage {
		t.Fatal("prompt image missing")
	}
	if len(toolNames) != 1 || toolNames[0] != "read_file" {
		t.Fatalf("tools = %v", toolNames)
	}
}

func TestAgentToolChoiceIgnoresMissingTool(t *testing.T) {
	req, err := parseOpenAIChat([]byte(`{"model":"swe-1-6","tool_choice":{"type":"function","function":{"name":"missing"}},"tools":[{"type":"function","function":{"name":"read_file","parameters":{}}},{"type":"function","function":{"name":"grep","parameters":{}}}],"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.forcedTool != "" || len(req.tools) != 2 {
		t.Fatalf("forced=%q tools=%d", req.forcedTool, len(req.tools))
	}
	raw := buildChatRequest("sk", "", req.model, "c", "p", "s", 1, "t", chatRequest{forcedTool: "read_file"})
	fields, err := iterFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fieldBytes(fields, 12); ok {
		t.Fatal("tool choice sent without the tool")
	}
}

func TestStreamCancelAfterTextIsNotSuccess(t *testing.T) {
	resetCaches()
	t.Cleanup(resetCaches)
	jwt := testJWT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(encodeString(1, jwt))
			return
		}
		frame, err := frameConnect(encodeString(3, "hello"), false)
		if err != nil {
			t.Errorf("frame: %v", err)
			return
		}
		_, _ = w.Write(frame)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := []byte(`{"model":"swe-1-6","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(ctx, srv.URL+"/v1/chat/completions", "sk-ws-cancel-mid", "", body, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	time.AfterFunc(200*time.Millisecond, cancel)
	raw, err := io.ReadAll(resp.Body)
	if err == nil || !strings.Contains(string(raw), "hello") || strings.Contains(string(raw), "[DONE]") {
		t.Fatalf("err=%v body=%s", err, raw)
	}
}

func TestCanceledChatDoesNotBecomeAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := []byte(`{"model":"swe-1-6","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	resp, err := Upstream{}.DoChatCompletions(ctx, "https://server.codeium.com/v1/chat/completions", "sk-ws-cancel", "", body, "", true)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSplitSSETextKeepsRunes(t *testing.T) {
	text := strings.Repeat("中a", 20)
	parts := splitUTF8(text, 4)
	if strings.Join(parts, "") != text {
		t.Fatalf("joined = %q", strings.Join(parts, ""))
	}
	for _, part := range parts {
		if !utf8.ValidString(part) || len(part) > 4 {
			t.Fatalf("part = %q len=%d", part, len(part))
		}
	}
	desc := trimToolDesc(strings.Repeat("中", maxToolDesc))
	if !utf8.ValidString(desc) || len(desc) > maxToolDesc {
		t.Fatalf("desc len=%d valid=%v", len(desc), utf8.ValidString(desc))
	}
}

func TestDeveloperPromptAndRemoteImage(t *testing.T) {
	req, err := parseOpenAIChat([]byte(`{"model":"swe-1-6","messages":[{"role":"developer","content":"follow the tools"},{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.messages) != 2 || len(req.messages[1].images) != 0 {
		t.Fatalf("messages = %+v", req.messages)
	}
	raw := buildChatRequest("sk", "", req.model, "c", "p", "s", 1, "t", req)
	fields, err := iterFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, field := range fields {
		if field.num != 3 {
			continue
		}
		prompts = append(prompts, fieldString(mustFields(t, fieldBytesOf(field)), 3))
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "follow the tools") || !strings.Contains(prompts[0], "hi") {
		t.Fatalf("prompts = %#v", prompts)
	}
}

func TestInlineImageStripsBase64Whitespace(t *testing.T) {
	req, err := parseOpenAIChat([]byte(`{"model":"swe-1-6","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVs\nbG8="}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.messages) != 1 || len(req.messages[0].images) != 1 || req.messages[0].images[0].data != "aGVsbG8=" {
		t.Fatalf("images = %+v", req.messages)
	}
}

func TestAgentToolChoiceNoneDropsTools(t *testing.T) {
	req, err := parseOpenAIChat([]byte(`{"model":"swe-1-6","tool_choice":"none","tools":[{"type":"function","function":{"name":"read_file","parameters":{}}}],"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.tools) != 0 || req.forcedTool != "" {
		t.Fatalf("tools = %+v forced=%q", req.tools, req.forcedTool)
	}
}

func fieldBytes(fields []protoField, num int) ([]byte, bool) {
	for _, field := range fields {
		if field.num == num {
			raw, ok := field.value.([]byte)
			return raw, ok
		}
	}
	return nil, false
}

func fieldBytesOf(field protoField) []byte {
	raw, _ := field.value.([]byte)
	return raw
}

func mustFields(t *testing.T, raw []byte) []protoField {
	t.Helper()
	fields, err := iterFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func testJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":9999999999}`))
	token := header + "." + payload + ".signature"
	if !jwtPattern.MatchString(token) {
		t.Fatalf("test jwt rejected: %s", token)
	}
	return token
}
