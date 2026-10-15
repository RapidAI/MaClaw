package windsurf

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

func TestListModelsAndChat(t *testing.T) {
	resetCaches()
	jwt := testJWT(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/exa.auth_pb.AuthService/GetUserJwt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(encodeString(1, jwt))
	})
	mux.HandleFunc("/exa.api_server_pb.ApiServerService/GetCascadeModelConfigs", func(w http.ResponseWriter, r *http.Request) {
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

func TestCanonicalAPIServer(t *testing.T) {
	if got := CanonicalAPIServer("devin-session-token$x", ""); got != SelfServeAPIServer {
		t.Fatalf("session server = %s", got)
	}
	if got := CanonicalAPIServer("sk-ws-x", "https://evil.example"); got != DefaultAPIServer {
		t.Fatalf("rejected foreign host = %s", got)
	}
	if got := CanonicalAPIServer("sk-ws-x", "https://server.codeium.com"); got != DefaultAPIServer {
		t.Fatalf("kept = %s", got)
	}
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
