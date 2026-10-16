package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestHubCenterGatewayTimeoutIgnoresJSONApplicationErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "html 504", status: 504, body: "<html>Gateway Time-out</html>", want: true},
		{name: "empty 502", status: 502, body: "", want: true},
		{name: "html 503", status: 503, body: "<html>unavailable</html>", want: true},
		{name: "json 503 all providers", status: 503, body: `{"error":{"message":"all providers failed"}}`, want: false},
		{name: "json 502 provider", status: 502, body: `{"error":{"message":"provider unavailable"}}`, want: false},
		{name: "json 500", status: 500, body: `{"error":{"message":"billing reconciliation failed"}}`, want: false},
		{name: "transport has no status", status: 0, body: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hubCenterGatewayTimeout(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("hubCenterGatewayTimeout = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeOfficialBillingTrailersPromotesSnapshot(t *testing.T) {
	header := http.Header{}
	header.Set(llmpool.CreditMultiplierHeader, "1")
	trailer := http.Header{}
	trailer.Set(llmpool.CreditMultiplierHeader, "0.5")
	trailer.Set(llmpool.ProviderIDHeader, "deepseek")
	trailer.Set(llmpool.TokenPricingSnapshotHeader, "snap")
	got := mergeOfficialBillingTrailers(header, trailer)
	if got.Get(llmpool.CreditMultiplierHeader) != "0.5" || got.Get(llmpool.ProviderIDHeader) != "deepseek" || got.Get(llmpool.TokenPricingSnapshotHeader) != "snap" {
		t.Fatalf("merged = %#v", got)
	}
	if header.Get(llmpool.CreditMultiplierHeader) != "1" {
		t.Fatal("merge mutated the source header")
	}
}

type gatewayLeaseFixture struct {
	mu           sync.Mutex
	released     bool
	releaseCalls int
	releaseCode  int
}

func (f *gatewayLeaseFixture) serveSibling(t *testing.T, slowURL string, onChat func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/llm/v1/binding/release":
			f.mu.Lock()
			f.releaseCalls++
			f.released = f.releaseCode < 300
			code := f.releaseCode
			f.mu.Unlock()
			if code == 0 {
				code = http.StatusOK
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"released":1}`))
			return
		case "/api/llm/v1/chat/completions":
			f.mu.Lock()
			released := f.released
			f.mu.Unlock()
			if !released {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-2","redirect_url":"` + slowURL + `"}`))
				return
			}
			onChat(w, r)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestGatewayTimeoutCompletesOnSiblingWithoutExcludingOwner(t *testing.T) {
	slowHits := 0
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			t.Errorf("slow path = %s", r.URL.Path)
			return
		}
		slowHits++
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		if !json.Valid(body) {
			t.Errorf("slow body = %s", body)
		}
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if payload["stream"] != true {
			t.Fatalf("unquoted slow body stream = %#v, want true so headers can leave early", payload["stream"])
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>504 Gateway Time-out</html>"))
	}))
	defer slow.Close()

	fixture := &gatewayLeaseFixture{}
	sibling := httptest.NewServer(fixture.serveSibling(t, slow.URL, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer sibling.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: slow.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.SetHubCenterCandidates([]string{slow.URL, sibling.URL})

	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_default")
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v body=%s", status, err, body)
	}
	if !json.Valid(body) || string(body) == "" || !strings.Contains(string(body), `"content":"ok"`) || !strings.Contains(string(body), `"object":"chat.completion"`) {
		t.Fatalf("aggregated body = %s", body)
	}
	if slowHits != 1 {
		t.Fatalf("slow hits = %d, want 1", slowHits)
	}
	fixture.mu.Lock()
	calls := fixture.releaseCalls
	fixture.mu.Unlock()
	if calls != 1 {
		t.Fatalf("release calls = %d, want 1 (first gateway timeout, not the 3-strike path)", calls)
	}
	client.mu.RLock()
	_, excluded := client.ownerExcluded[normalizeHubCenterURLOne(slow.URL)]
	_, cooled := client.ownerGateway[normalizeHubCenterURLOne(slow.URL)]
	client.mu.RUnlock()
	if excluded {
		t.Fatal("gateway timeout must not exclude the owner")
	}
	if !cooled {
		t.Fatal("gateway timeout must cool the slow node")
	}
	for _, target := range client.orderedTargets("tenant_default") {
		if sameHubCenterURL(target, slow.URL) {
			t.Fatalf("cooled node still preferred: %v", client.orderedTargets("tenant_default"))
		}
	}
}

func TestGatewayStreamTimeoutCompletesOnSibling(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>504 Gateway Time-out</html>"))
	}))
	defer slow.Close()
	fixture := &gatewayLeaseFixture{}
	sibling := httptest.NewServer(fixture.serveSibling(t, slow.URL, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer sibling.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: slow.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.SetHubCenterCandidates([]string{slow.URL, sibling.URL})
	resp, err := client.ForwardStream(context.Background(), []byte(`{"model":"auto","stream":true}`), "tenant_default")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	fixture.mu.Lock()
	calls := fixture.releaseCalls
	fixture.mu.Unlock()
	if calls != 1 {
		t.Fatalf("release calls = %d, want 1", calls)
	}
}

func TestGatewayTimeoutWhenReleaseDoesNotStickNamesNode(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>504 Gateway Time-out</html>"))
	}))
	defer slow.Close()
	fixture := &gatewayLeaseFixture{releaseCode: http.StatusInternalServerError}
	sibling := httptest.NewServer(fixture.serveSibling(t, slow.URL, func(http.ResponseWriter, *http.Request) {
		t.Fatal("sibling chat must not run when the lease release does not stick")
	}))
	defer sibling.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: slow.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.SetHubCenterCandidates([]string{slow.URL, sibling.URL})
	_, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_default")
	if !errors.Is(err, corellm.ErrOfficialGatewayTimeout) {
		t.Fatalf("err = %v, want gateway timeout", err)
	}
	if errors.Is(err, corellm.ErrOfficialOwnerUnreachable) {
		t.Fatalf("gateway timeout reported as unreachable: %v", err)
	}
	got := corellm.UserFacingError(err)
	if got != "官方模型节点 hc-2 响应超时，请稍后重试" {
		t.Fatalf("user message = %q", got)
	}
	if strings.Contains(got, slow.URL) || strings.Contains(got, "http") || strings.Contains(got, sibling.URL) {
		t.Fatalf("user message leaked a URL: %q", got)
	}
	client.mu.RLock()
	_, excluded := client.ownerExcluded[normalizeHubCenterURLOne(slow.URL)]
	client.mu.RUnlock()
	if excluded {
		t.Fatal("failed lease release must not exclude the owner")
	}
}

func TestGatewayTimeoutAloneDoesNotSayUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv.Close()
	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: srv.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	_, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_default")
	if !errors.Is(err, corellm.ErrOfficialGatewayTimeout) || errors.Is(err, corellm.ErrOfficialOwnerUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if got := corellm.UserFacingError(err); got != "官方模型响应超时，请稍后重试" && got != "官方模型暂时没有返回响应，请稍后重试" {
		t.Fatalf("user message = %q", got)
	}
	client.mu.RLock()
	_, excluded := client.ownerExcluded[normalizeHubCenterURLOne(srv.URL)]
	client.mu.RUnlock()
	if excluded {
		t.Fatal("single gateway timeout must not exclude the node")
	}
}

func TestQuotedGatewayTimeoutRetriesUnquotedWithoutMutatingQuote(t *testing.T) {
	var slowBody []byte
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slowBody, _ = io.ReadAll(io.LimitReader(r.Body, 4096))
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>504 Gateway Time-out</html>"))
	}))
	defer slow.Close()
	var healthyBody []byte
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyBody, _ = io.ReadAll(io.LimitReader(r.Body, 4096))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer healthy.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: slow.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.SetHubCenterCandidates([]string{slow.URL, healthy.URL})
	quote := OfficialPricingQuote{Token: "qt", ExpiresAt: time.Now().Add(time.Minute)}
	quote.targetURL = slow.URL
	original := []byte(`{"model":"auto"}`)
	result, err := client.ForwardDetailedWithQuote(context.Background(), quote, original, "tenant_q")
	if err != nil || result.StatusCode != http.StatusOK {
		t.Fatalf("status=%d err=%v body=%s", result.StatusCode, err, result.Body)
	}
	if string(slowBody) != string(original) {
		t.Fatalf("quoted body = %s, want unchanged %s", slowBody, original)
	}
	var payload map[string]any
	if err := json.Unmarshal(healthyBody, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stream"] != true {
		t.Fatalf("unquoted retry stream = %#v, want true", payload["stream"])
	}
}

func TestExcludedNodeStaysExcludedWhenChatHeadersFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/llm/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte("<html>504 Gateway Time-out</html>"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: srv.URL, HubID: "hub1", MachineToken: "secret"})
	t.Cleanup(func() { _ = client.Close() })
	client.markOwnerUnreachable(srv.URL)
	client.probeExcludedOwners()

	client.mu.RLock()
	_, excluded := client.ownerExcluded[normalizeHubCenterURLOne(srv.URL)]
	client.mu.RUnlock()
	if !excluded {
		t.Fatal("quality 200 with chat 504 must keep the node excluded")
	}
}
