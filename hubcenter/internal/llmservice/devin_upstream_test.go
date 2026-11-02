package llmservice

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/windsurf"
)

func TestDevinMatchFollowsOfficialHostNotDisplayName(t *testing.T) {
	shared := &llmpool.ProviderConfig{
		Name:     "Token Bank · Devin · swe-2-high",
		APIURL:   windsurf.SelfServeAPIServer,
		Protocol: "openai",
	}
	if !devinMatches(shared) {
		t.Fatal("token bank member on the Devin API host was not recognized")
	}
	renamedOnly := &llmpool.ProviderConfig{
		Name:     "Token Bank · Devin · swe-2-high",
		APIURL:   "https://api.example.com/v1",
		Protocol: "openai",
	}
	if devinMatches(renamedOnly) {
		t.Fatal("display name alone must not select the Cascade transport")
	}
	plain := DevinHTTPClient(&http.Client{}, renamedOnly.Name, renamedOnly.APIURL)
	if _, ok := plain.Transport.(*windsurf.Transport); ok {
		t.Fatal("non-Devin client was wrapped")
	}
	wrapped := DevinHTTPClient(&http.Client{}, shared.Name, shared.APIURL)
	if _, ok := wrapped.Transport.(*windsurf.Transport); !ok {
		t.Fatal("Devin API host was not wrapped with the Cascade transport")
	}
}

func TestForwardTokenBankDevinSpeaksCascade(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if strings.Contains(r.URL.Path, "chat/completions") || strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		writeDevinCascade(t, w, r)
	}))
	defer srv.Close()
	joined := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(paths, " ")
	}

	provider := &llmpool.ProviderConfig{
		ID:       "tb-devin",
		Name:     "Devin",
		APIURL:   srv.URL,
		APIKey:   "sk-ws-token-bank",
		Protocol: "openai",
		Models:   []string{"swe-2-high"},
	}
	resp, err := forwardToProvider(context.Background(), srv.Client(), provider, map[string]any{
		"model":    "swe-2-high",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, "swe-2-high", "swe-2-high")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK || !bytes.Contains(resp.Body, []byte("pong")) {
		t.Fatalf("response = %#v", resp)
	}
	if got := joined(); !strings.Contains(got, "GetChatMessage") || strings.Contains(got, "chat/completions") {
		t.Fatalf("paths = %s, want the Cascade chat RPC", got)
	}

	dst := newLockedResponseRecorder()
	streamed, err := streamProviderToWriter(context.Background(), srv.Client(), provider, map[string]any{
		"model":    "swe-2-high",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, "swe-2-high", "swe-2-high", dst)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if streamed == nil || streamed.statusCode != http.StatusOK || !strings.Contains(dst.BodyString(), "pong") {
		t.Fatalf("stream status=%v body=%s", streamed, dst.BodyString())
	}

	models, err := ProbeProviderModels(context.Background(), srv.Client(), provider.Name, provider.APIURL, provider.APIKey, provider.Protocol, "")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(models) != 1 || models[0] != "swe-2-high" {
		t.Fatalf("models = %#v", models)
	}
	if got := joined(); strings.Contains(got, "/models") || !strings.Contains(got, "GetCascadeModelConfigs") {
		t.Fatalf("paths = %s, model probe did not use the Cascade catalog", got)
	}
}

func TestDevinStreamKeepsCascadeOutputBudget(t *testing.T) {
	var mu sync.Mutex
	var budgets []uint64
	var decodeErr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetChatMessage") {
			raw, _ := io.ReadAll(r.Body)
			n, err := devinChatBudget(raw)
			mu.Lock()
			if err != nil {
				decodeErr = err.Error()
			} else {
				budgets = append(budgets, n)
			}
			mu.Unlock()
		}
		writeDevinCascade(t, w, r)
	}))
	defer srv.Close()
	base := &llmpool.ProviderConfig{
		ID:       "devin-budget",
		Name:     "Devin",
		APIURL:   srv.URL,
		APIKey:   "sk-ws-budget",
		Protocol: "openai",
		Models:   []string{"swe-2-high"},
	}
	capped := *base
	capped.ID = TokenBankMemberID("share", "swe-2-high")
	capped.MaxOutputTokensPerRequest = 1000

	cases := []struct {
		name     string
		provider *llmpool.ProviderConfig
		body     map[string]any
		stream   bool
		want     uint64
	}{
		{name: "stream omitted", provider: base, stream: true, want: 128000},
		{name: "non-stream omitted", provider: base, want: 128000},
		{name: "caller max_tokens", provider: base, body: map[string]any{"max_tokens": 4096}, stream: true, want: 4096},
		{name: "caller max_completion_tokens", provider: base, body: map[string]any{"max_completion_tokens": 2048}, stream: true, want: 2048},
		{name: "token bank cap", provider: &capped, stream: true, want: 1000},
		{name: "caller under cap", provider: &capped, body: map[string]any{"max_tokens": 200}, stream: true, want: 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{
				"model":    "swe-2-high",
				"messages": []any{map[string]any{"role": "user", "content": "ping"}},
			}
			for k, v := range tc.body {
				body[k] = v
			}
			mu.Lock()
			before := len(budgets)
			mu.Unlock()
			if tc.stream {
				dst := newLockedResponseRecorder()
				streamed, err := streamProviderToWriter(context.Background(), srv.Client(), tc.provider, body, "swe-2-high", "swe-2-high", dst)
				if err != nil || streamed == nil || streamed.statusCode != http.StatusOK {
					t.Fatalf("stream err=%v result=%#v body=%s", err, streamed, dst.BodyString())
				}
			} else {
				resp, err := forwardToProvider(context.Background(), srv.Client(), tc.provider, body, "swe-2-high", "swe-2-high")
				if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
					t.Fatalf("forward err=%v resp=%#v", err, resp)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if decodeErr != "" {
				t.Fatal(decodeErr)
			}
			if len(budgets) != before+1 || budgets[before] != tc.want {
				t.Fatalf("budgets = %v, want %d at index %d", budgets, tc.want, before)
			}
		})
	}
}

func TestDevinStreamOpenFailureIsNotACompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(devinEncodeString(1, devinTestJWT(t)))
			return
		}
		if strings.Contains(r.URL.Path, "GetChatMessage") {
			http.Error(w, "invalid api key", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dst := newLockedResponseRecorder()
	streamed, err := streamProviderToWriter(context.Background(), srv.Client(), &llmpool.ProviderConfig{
		ID:       "tb-devin-stream-fail",
		Name:     "Devin",
		APIURL:   srv.URL,
		APIKey:   "sk-ws-rejected",
		Protocol: "openai",
		Models:   []string{"swe-2-high"},
	}, map[string]any{
		"model":    "swe-2-high",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, "swe-2-high", "swe-2-high", dst)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if streamed == nil || streamed.statusCode < 400 || streamed.wroteBusinessStream {
		t.Fatalf("result = %#v, want a failed attempt", streamed)
	}
	if body := dst.BodyString(); body != "" {
		t.Fatalf("client body = %q, want the failure kept off the stream", body)
	}
	if !bytes.Contains(streamed.errorBody, []byte("invalid api key")) {
		t.Fatalf("error body = %q", streamed.errorBody)
	}
}

func TestDevinStreamTrailerErrorIsNotACompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(devinEncodeString(1, devinTestJWT(t)))
			return
		}
		if strings.Contains(r.URL.Path, "GetChatMessage") {
			frame, err := devinFrameConnect([]byte(`{"error":{"message":"model rejected"}}`))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			frame[0] |= 0x02
			_, _ = w.Write(frame)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dst := newLockedResponseRecorder()
	streamed, err := streamProviderToWriter(context.Background(), srv.Client(), &llmpool.ProviderConfig{
		ID:       "tb-devin-trailer",
		Name:     "Devin",
		APIURL:   srv.URL,
		APIKey:   "sk-ws-trailer",
		Protocol: "openai",
		Models:   []string{"swe-2-high"},
	}, map[string]any{
		"model":    "swe-2-high",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, "swe-2-high", "swe-2-high", dst)
	if err == nil || !strings.Contains(err.Error(), "model rejected") {
		t.Fatalf("stream err = %v", err)
	}
	if streamed != nil && streamed.wroteBusinessStream {
		t.Fatal("trailer error was committed as a completion")
	}
	if body := dst.BodyString(); body != "" {
		t.Fatalf("client body = %q, want the failure kept off the stream", body)
	}
}

func TestForwardDevinDoesNotReplayFinishedFailure(t *testing.T) {
	var chats atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(devinEncodeString(1, devinTestJWT(t)))
			return
		}
		if strings.Contains(r.URL.Path, "GetChatMessage") {
			chats.Add(1)
			http.Error(w, "invalid api key", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	resp, err := forwardToProvider(context.Background(), srv.Client(), &llmpool.ProviderConfig{
		ID:       "tb-devin-fail",
		Name:     "Devin",
		APIURL:   srv.URL,
		APIKey:   "sk-ws-rejected",
		Protocol: "openai",
		Models:   []string{"swe-2-high"},
	}, map[string]any{
		"model":    "swe-2-high",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, "swe-2-high", "swe-2-high")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp == nil || resp.StatusCode < 400 {
		t.Fatalf("response = %#v, want the Cascade failure", resp)
	}
	if got := chats.Load(); got != 1 {
		t.Fatalf("cascade chats = %d, want 1", got)
	}
}

func writeDevinCascade(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	switch {
	case strings.Contains(r.URL.Path, "GetUserJwt"):
		_, _ = w.Write(devinEncodeString(1, devinTestJWT(t)))
	case strings.Contains(r.URL.Path, "GetCascadeModelConfigs"):
		entry := append(devinEncodeString(1, "SWE 2 High"), devinEncodeString(22, "swe-2-high")...)
		_, _ = w.Write(devinEncodeBytes(1, entry))
	default:
		frame, err := devinFrameConnect(devinEncodeString(3, "pong"))
		if err != nil {
			t.Errorf("frame: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(frame)
	}
}

func devinChatBudget(raw []byte) (uint64, error) {
	if len(raw) < 5 || raw[0]&0x01 == 0 {
		return 0, fmt.Errorf("expected gzip connect frame, flags=%d len=%d", byteAt(raw), len(raw))
	}
	n := binary.BigEndian.Uint32(raw[1:5])
	if uint32(len(raw)-5) < n {
		return 0, fmt.Errorf("short connect frame")
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw[5 : 5+n]))
	if err != nil {
		return 0, err
	}
	payload, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil {
		return 0, err
	}
	completion, ok := devinProtoBytes(payload, 8)
	if !ok {
		return 0, fmt.Errorf("chat request missing completion config")
	}
	budget, ok := devinProtoVarint(completion, 2)
	if !ok {
		return 0, fmt.Errorf("completion config missing max tokens")
	}
	return budget, nil
}

func byteAt(raw []byte) byte {
	if len(raw) == 0 {
		return 0
	}
	return raw[0]
}

func devinProtoBytes(buf []byte, num int) ([]byte, bool) {
	for len(buf) > 0 {
		tag, n := binary.Uvarint(buf)
		if n <= 0 {
			return nil, false
		}
		buf = buf[n:]
		field := int(tag >> 3)
		switch tag & 7 {
		case 0:
			_, n = binary.Uvarint(buf)
			if n <= 0 {
				return nil, false
			}
			buf = buf[n:]
		case 1:
			if len(buf) < 8 {
				return nil, false
			}
			buf = buf[8:]
		case 2:
			ln, n := binary.Uvarint(buf)
			if n <= 0 || uint64(len(buf[n:])) < ln {
				return nil, false
			}
			buf = buf[n:]
			chunk := buf[:ln]
			buf = buf[ln:]
			if field == num {
				return append([]byte(nil), chunk...), true
			}
		case 5:
			if len(buf) < 4 {
				return nil, false
			}
			buf = buf[4:]
		default:
			return nil, false
		}
	}
	return nil, false
}

func devinProtoVarint(buf []byte, num int) (uint64, bool) {
	for len(buf) > 0 {
		tag, n := binary.Uvarint(buf)
		if n <= 0 {
			return 0, false
		}
		buf = buf[n:]
		field := int(tag >> 3)
		switch tag & 7 {
		case 0:
			v, n := binary.Uvarint(buf)
			if n <= 0 {
				return 0, false
			}
			buf = buf[n:]
			if field == num {
				return v, true
			}
		case 1:
			if len(buf) < 8 {
				return 0, false
			}
			buf = buf[8:]
		case 2:
			ln, n := binary.Uvarint(buf)
			if n <= 0 || uint64(len(buf[n:])) < ln {
				return 0, false
			}
			buf = buf[n+int(ln):]
		case 5:
			if len(buf) < 4 {
				return 0, false
			}
			buf = buf[4:]
		default:
			return 0, false
		}
	}
	return 0, false
}

func devinTestJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":9999999999}`))
	return header + "." + payload + ".sig"
}

func devinEncodeString(field int, s string) []byte {
	return devinEncodeBytes(field, []byte(s))
}

func devinEncodeBytes(field int, body []byte) []byte {
	tag := devinEncodeVarint(uint64(field<<3 | 2))
	return append(append(tag, devinEncodeVarint(uint64(len(body)))...), body...)
}

func devinEncodeVarint(v uint64) []byte {
	var out []byte
	for v > 0x7f {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func devinFrameConnect(body []byte) ([]byte, error) {
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[1:], uint32(len(body)))
	return append(header, body...), nil
}
