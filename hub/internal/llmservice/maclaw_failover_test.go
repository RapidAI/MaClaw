package llmservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var errTransportFailed = errors.New("dial tcp: i/o timeout")

func TestMarkOwnerPoolUnhealthyDeprioritizesAndRecovers(t *testing.T) {
	oldCooldown := officialOwnerCooldown
	officialOwnerCooldown = 30 * time.Second
	t.Cleanup(func() { officialOwnerCooldown = oldCooldown })

	c := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://a.example.com"})
	c.SetHubCenterCandidates([]string{"https://a.example.com", "https://b.example.com", "https://c.example.com"})

	targets := c.orderedTargets("tenant-1")
	if len(targets) != 3 || targets[0] != "https://a.example.com" {
		t.Fatalf("initial targets = %v, want a first", targets)
	}

	// A pool-wide failure on b must skip b without touching pins or binding.
	c.markOwnerPoolUnhealthy("https://b.example.com")
	targets = c.orderedTargets("tenant-1")
	if len(targets) != 2 || targets[0] != "https://a.example.com" || targets[1] != "https://c.example.com" {
		t.Fatalf("after pool-unhealthy targets = %v, want [a c]", targets)
	}

	// A success on b restores it as the preferred target.
	c.rememberSuccessfulTarget("tenant-1", "https://b.example.com")
	targets = c.orderedTargets("tenant-1")
	if len(targets) != 3 || targets[0] != "https://b.example.com" {
		t.Fatalf("after recovery targets = %v, want b first", targets)
	}
}

func TestShouldFailoverHubCenterAllProvidersFailed(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "json 503 all providers failed fails over",
			status: 503,
			body:   `{"error":{"message":"all providers failed, last error: llmpool: provider opencode-3 circuit probe in flight (awaiting half-open probe)"}}`,
			want:   true,
		},
		{
			name:   "json 503 all stream providers failed fails over",
			status: 503,
			body:   `{"error":{"message":"all stream providers failed, last error: llmpool: provider opencode-3 circuit open (cooldown 10s)"}}`,
			want:   true,
		},
		{
			name:   "json 500 other application error stays",
			status: 500,
			body:   `{"error":{"message":"billing reconciliation failed"}}`,
			want:   false,
		},
		{
			name:   "html 502 reverse proxy error fails over",
			status: 502,
			body:   `<html><body>bad gateway</body></html>`,
			want:   true,
		},
		{
			name:   "transport error fails over",
			status: 0,
			body:   "",
			want:   true,
		},
		{
			name:   "json 400 stays",
			status: 400,
			body:   `{"error":{"message":"all providers failed"}}`,
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.name == "transport error fails over" {
				err = errTransportFailed
			}
			if got := shouldFailoverHubCenter(tc.status, []byte(tc.body), err); got != tc.want {
				t.Fatalf("shouldFailoverHubCenter(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func sickPoolServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"all providers failed, last error: llmpool: provider p circuit open (cooldown 10s)"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestForwardDetailedWithQuoteMarksSickPoolAndClearsPin(t *testing.T) {
	server := sickPoolServer(t)
	c := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: server.URL, HubID: "hub-x", MachineToken: "token-x"})
	c.SetHubCenterCandidates([]string{server.URL, "https://healthy.example.com"})

	c.rememberSuccessfulTarget("tenant-q", server.URL)
	if got := c.orderedTargets("tenant-q"); len(got) == 0 || got[0] != server.URL {
		t.Fatalf("precondition: pinned targets = %v, want %s first", got, server.URL)
	}

	quote := OfficialPricingQuote{Token: "qt", ExpiresAt: time.Now().Add(time.Minute)}
	quote.targetURL = server.URL
	result, err := c.ForwardDetailedWithQuote(context.Background(), quote, []byte(`{"model":"auto"}`), "tenant-q")
	if err != nil {
		t.Fatalf("ForwardDetailedWithQuote: %v", err)
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", result.StatusCode)
	}

	targets := c.orderedTargets("tenant-q")
	if len(targets) != 1 || targets[0] != "https://healthy.example.com" {
		t.Fatalf("after sick quote: targets = %v, want [https://healthy.example.com]", targets)
	}
}

func TestForwardStreamWithQuoteMarksSickPoolAndPreservesBody(t *testing.T) {
	server := sickPoolServer(t)
	c := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: server.URL, HubID: "hub-x", MachineToken: "token-x"})
	c.SetHubCenterCandidates([]string{server.URL, "https://healthy.example.com"})

	quote := OfficialPricingQuote{Token: "qt", ExpiresAt: time.Now().Add(time.Minute)}
	quote.targetURL = server.URL
	resp, err := c.ForwardStreamWithQuote(context.Background(), quote, []byte(`{"model":"auto"}`), "tenant-s")
	if err != nil {
		t.Fatalf("ForwardStreamWithQuote: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !hubCenterAllProvidersFailed(body) {
		t.Fatalf("error body must be preserved for the caller, got %q", body)
	}
	targets := c.orderedTargets("tenant-s")
	if len(targets) != 1 || targets[0] != "https://healthy.example.com" {
		t.Fatalf("after sick stream quote: targets = %v, want [https://healthy.example.com]", targets)
	}
}

func TestForwardDetailedWithQuoteMarksUnreachableNodeOnTransportError(t *testing.T) {
	server := sickPoolServer(t)
	closedURL := server.URL
	server.Close() // connection refused from now on

	c := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: closedURL, HubID: "hub-x", MachineToken: "token-x"})
	c.SetHubCenterCandidates([]string{closedURL, "https://healthy.example.com"})
	c.rememberSuccessfulTarget("tenant-t", closedURL)

	quote := OfficialPricingQuote{Token: "qt", ExpiresAt: time.Now().Add(time.Minute)}
	quote.targetURL = closedURL
	_, err := c.ForwardDetailedWithQuote(context.Background(), quote, []byte(`{"model":"auto"}`), "tenant-t")
	if err == nil {
		t.Fatal("expected transport error")
	}

	targets := c.orderedTargets("tenant-t")
	if len(targets) != 1 || targets[0] != "https://healthy.example.com" {
		t.Fatalf("after transport failure: targets = %v, want [https://healthy.example.com]", targets)
	}
}
