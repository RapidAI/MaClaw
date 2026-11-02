package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/windsurf"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestProviderTestConfigKeepsDevinIdentityForTokenBank(t *testing.T) {
	provider := &llmpool.ProviderConfig{
		Name:     "Token Bank · Devin · swe-2-high",
		APIURL:   windsurf.SelfServeAPIServer,
		Protocol: "openai",
	}
	cfg := providerTestConfig(corelib.MaclawLLMConfig{
		URL:      provider.APIURL,
		Model:    "swe-2-high",
		Protocol: "openai",
	}, provider)
	if cfg.ProviderName != provider.Name || cfg.URL != windsurf.SelfServeAPIServer {
		t.Fatalf("test config = %+v", cfg)
	}
	client := llmservice.DevinHTTPClient(&http.Client{}, cfg.ProviderName, cfg.URL)
	if _, ok := client.Transport.(*windsurf.Transport); !ok {
		t.Fatal("status check would POST OpenAI JSON at the Devin host")
	}

	plain := providerTestConfig(corelib.MaclawLLMConfig{
		URL:      "https://api.deepseek.com/v1",
		Protocol: "openai",
	}, &llmpool.ProviderConfig{Name: "DeepSeek", APIURL: "https://api.deepseek.com/v1"})
	if plain.ProviderName != "" {
		t.Fatalf("non-Devin test config took a provider name: %+v", plain)
	}

	named := providerTestConfig(corelib.MaclawLLMConfig{
		URL: "https://example.test/v1",
	}, &llmpool.ProviderConfig{Name: "Devin", APIURL: "https://example.test/v1"})
	if named.ProviderName != "Devin" {
		t.Fatalf("named Devin test config = %+v", named)
	}
	namedClient := llmservice.DevinHTTPClient(&http.Client{}, named.ProviderName, named.URL)
	if _, ok := namedClient.Transport.(*windsurf.Transport); !ok {
		t.Fatal("status check missed a provider that is Devin by name")
	}
}

func TestRunLLMProviderChatTestSpeaksDevinCascade(t *testing.T) {
	var mu sync.Mutex
	var sawOpenAI, sawCascade bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(r.URL.Path, "chat/completions") {
			sawOpenAI = true
			http.NotFound(w, r)
			return
		}
		sawCascade = true
		if strings.Contains(r.URL.Path, "GetUserJwt") {
			_, _ = w.Write(devinStatusEncodeString(1, devinStatusJWT()))
			return
		}
		frame, err := devinStatusFrame(devinStatusEncodeString(3, "pong"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	reply, errMsg, _, _ := runLLMProviderChatTest(context.Background(), corelib.MaclawLLMConfig{
		URL:          srv.URL,
		Key:          "sk-ws-status",
		Model:        "swe-2-high",
		Protocol:     "openai",
		ProviderName: "Devin",
	})
	if errMsg != "" {
		t.Fatalf("status check error = %s", errMsg)
	}
	if strings.TrimSpace(reply) != "pong" {
		t.Fatalf("reply = %q", reply)
	}
	mu.Lock()
	openai, cascade := sawOpenAI, sawCascade
	mu.Unlock()
	if openai || !cascade {
		t.Fatalf("openai=%v cascade=%v", openai, cascade)
	}
}

func devinStatusJWT() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":9999999999}`))
	return header + "." + payload + ".sig"
}

func devinStatusEncodeString(field int, s string) []byte {
	body := []byte(s)
	tag := devinStatusVarint(uint64(field<<3 | 2))
	return append(append(tag, devinStatusVarint(uint64(len(body)))...), body...)
}

func devinStatusVarint(v uint64) []byte {
	var out []byte
	for v > 0x7f {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func devinStatusFrame(body []byte) ([]byte, error) {
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[1:], uint32(len(body)))
	return append(header, body...), nil
}
