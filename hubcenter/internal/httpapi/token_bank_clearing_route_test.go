package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// clearingSettingsStub is the smallest settings repository that can name a
// clearing node. It exists because the routing decision is read from the shared
// settings blob, and a test needs to set that blob without going through the
// admin endpoint (which would exercise a different code path than the one under
// test).
type clearingSettingsStub struct {
	mu      sync.Mutex
	raw     string
	listErr error
}

func (s *clearingSettingsStub) Set(_ context.Context, _, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = value
	return nil
}

func (s *clearingSettingsStub) Get(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw, nil
}

func (s *clearingSettingsStub) List(context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, s.listErr
}

func (s *clearingSettingsStub) put(t *testing.T, settings sqlite.TokenBankSettings) {
	t.Helper()
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := s.Set(context.Background(), sqlite.TokenBankSettingsKey, string(encoded)); err != nil {
		t.Fatalf("set settings: %v", err)
	}
}

// clearingPeerStub records what a forwarded request looked like when it landed.
type clearingPeerStub struct {
	server *httptest.Server

	// headers are written on every answer. Tests use them to assert the proxy
	// carries the peer's headers back: the landing page is served with a
	// Content-Security-Policy, and a proxied copy without it would be an XSS
	// hole introduced by nothing more than a routing decision.
	headers map[string]string

	mu       sync.Mutex
	requests int
	lastHop  string
	lastBody string
	lastAuth string
}

func newClearingPeerStub(t *testing.T, status int, body string) *clearingPeerStub {
	t.Helper()
	stub := &clearingPeerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		stub.mu.Lock()
		stub.requests++
		stub.lastHop = r.Header.Get(tokenBankPeerHopHeader)
		stub.lastBody = string(raw)
		stub.lastAuth = r.Header.Get("Authorization")
		headers := stub.headers
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *clearingPeerStub) snapshot() (int, string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, s.lastHop, s.lastBody, s.lastAuth
}

// TestTokenBankWithdrawRoutesToClearingNode is the core of the fix: a
// withdrawal that lands on the wrong node has to be executed on the right one,
// not answered locally.
//
// The danger being guarded against is subtle. Refusing when the clearing node is
// unreachable looks like a bug to an operator — the pull fails and nothing
// moved — but it is the only correct answer. Answering locally is precisely the
// over-issue this routing exists to prevent, just made to look like high
// availability.
func TestTokenBankWithdrawRoutesToClearingNode(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "clearing-user@example.test")
	env.seedCredits(t, user.ID, 10)

	peer := newClearingPeerStub(t, http.StatusOK, `{"status":"ok","created":true}`)
	settings := &clearingSettingsStub{}
	env.handlers.settings = settings
	env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
	settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

	// hc-test is this node; the clearing node is hc-clearing, so the pull must
	// travel.
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, map[string]any{
		"request_id": "clearing-wd", "amount_micro": 1_000_000, "manual": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["status"]; got != "ok" {
		t.Fatalf("status = %v, want the peer's answer to be copied back", got)
	}
	requests, hop, body, auth := peer.snapshot()
	if requests != 1 {
		t.Fatalf("peer requests = %d, want 1 (the withdrawal must be decided there)", requests)
	}
	if hop != "1" {
		t.Errorf("peer hop header = %q, want \"1\" so the peer executes instead of routing again", hop)
	}
	// The body has to survive the hop byte for byte: a field this build does not
	// know about is still the clearing node's to validate.
	if !strings.Contains(body, "clearing-wd") {
		t.Errorf("peer body = %q, want the original request body", body)
	}
	if auth == "" {
		t.Error("peer saw no Authorization header; the clearing node must re-authenticate the caller")
	}
}

// TestTokenBankWithdrawRunsLocallyWhenThisNodeIsClearing pins the other three
// cases in which routing must NOT happen: this node is the clearing node, no
// clearing node is configured, and the request already arrived as a trusted hop.
func TestTokenBankWithdrawRunsLocallyWhenThisNodeIsClearing(t *testing.T) {
	cases := []struct {
		name     string
		clearing string
		hop      bool
		wantPeer int
	}{
		// Empty is the shipped default: an unconfigured cluster keeps behaving
		// exactly as it does today rather than failing every pull.
		{name: "unconfigured", clearing: "", wantPeer: 0},
		{name: "this node is the clearing node", clearing: "hc-test", wantPeer: 0},
		// A trusted hop means a peer already routed it; routing again would loop.
		{name: "trusted hop", clearing: "hc-clearing", hop: true, wantPeer: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTokenBankTestEnv(t)
			user, token := env.createUser(t, "local-user@example.test")
			env.seedCredits(t, user.ID, 10)

			peer := newClearingPeerStub(t, http.StatusOK, `{"status":"ok"}`)
			settings := &clearingSettingsStub{}
			env.handlers.settings = settings
			env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
			settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: tc.clearing})

			headers := map[string]string{}
			if tc.hop {
				headers[tokenBankPeerHopHeader] = "1"
				headers[tokenBankPeerSecretHeader] = "peer-secret"
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/token-bank/credits/withdraw",
				strings.NewReader(`{"request_id":"local-wd","amount_micro":1000000,"manual":true}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			env.handlers.TokenBankWithdrawCredits(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("withdraw status = %d, body %s", rec.Code, rec.Body.String())
			}
			if requests, _, _, _ := peer.snapshot(); requests != tc.wantPeer {
				t.Fatalf("peer requests = %d, want %d", requests, tc.wantPeer)
			}
		})
	}
}

// TestTokenBankWithdrawRefusesWhenClearingUnreachable is the assertion that
// matters most. An unreachable clearing node must produce a refusal, never a
// local answer — a local answer is the over-issue bug wearing a success status.
func TestTokenBankWithdrawRefusesWhenClearingUnreachable(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "unreachable-user@example.test")
	env.seedCredits(t, user.ID, 10)

	settings := &clearingSettingsStub{}
	env.handlers.settings = settings
	// The directory knows the peer but reports it as down.
	env.handlers.SetTokenBankOrigin(&claimOriginStub{url: "http://127.0.0.1:1", reachable: false, secret: "peer-secret"})
	settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, map[string]any{
		"request_id": "unreachable-wd", "amount_micro": 1_000_000, "manual": true,
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("withdraw status = %d, want %d: an unreachable clearing node must refuse, not answer locally",
			rec.Code, http.StatusServiceUnavailable)
	}

	// And nothing may have been debited. This is the whole point: the pull
	// failed, and the balance is untouched.
	balance, err := env.repo.Balance(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.WithdrawnMicro != 0 {
		t.Fatalf("withdrawn = %d, want 0 (a refused pull must not debit)", balance.WithdrawnMicro)
	}
}

// TestTokenBankGiftLandingRoutesToClearingAndKeepsSecurityHeaders is the
// recipient's first contact with a shared link, and the reason "route the
// writes" turned out to be only half the job: the link row exists solely on the
// clearing node, so the landing page 404s on every other node — the user reads
// that as a dead link and never reaches the claim button.
//
// It also pins the header passthrough. The landing page is HTML served with a
// Content-Security-Policy; a proxy that only copied the body would hand back a
// page stripped of its XSS defence.
func TestTokenBankGiftLandingRoutesToClearingAndKeepsSecurityHeaders(t *testing.T) {
	env := newTokenBankTestEnv(t)
	peer := newClearingPeerStub(t, http.StatusOK, `<html><body>landing</body></html>`)
	peer.mu.Lock()
	peer.headers = map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "default-src 'none'",
		"Cache-Control":           "no-store",
		"X-Content-Type-Options":  "nosniff",
	}
	peer.mu.Unlock()
	settings := &clearingSettingsStub{}
	env.handlers.settings = settings
	env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
	settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

	req := httptest.NewRequest(http.MethodGet, "/c/ABCDEFG234", nil)
	rec := httptest.NewRecorder()
	// Routed through a mux so {code} is populated; calling the handler
	// directly would leave PathValue empty and never reach the routing.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /c/{code}", env.handlers.TokenBankGiftLanding)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("landing status = %d, body %s", rec.Code, rec.Body.String())
	}
	if requests, _, _, _ := peer.snapshot(); requests != 1 {
		t.Fatalf("peer requests = %d, want 1 (the link is only on the clearing node)", requests)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("Content-Security-Policy = %q, want the peer's policy: dropping it serves the page without its XSS defence", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: a proxied landing page must not become cacheable", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the peer's HTML content type", got)
	}
}

// TestTokenBankGiftReadsRouteToClearing covers the same half for the rest of
// the surface: a list, a preview and a revoke all read or write rows that exist
// only where the link was created. Routing only the create would leave them
// answering "empty" or "not found" from everywhere else.
func TestTokenBankGiftReadsRouteToClearing(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "list gift links", method: http.MethodGet, path: "/api/v1/credits/share-links"},
		{name: "revoke gift link", method: http.MethodPost, path: "/api/v1/credits/share-links/link-1/revoke"},
		{name: "preview gift link", method: http.MethodGet, path: "/api/v1/credits/share-links/ABCDEFG234/preview"},
		{name: "list withdrawals", method: http.MethodGet, path: "/api/v1/token-bank/credits/withdrawals"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTokenBankTestEnv(t)
			_, token := env.createUser(t, "reader@example.test")
			peer := newClearingPeerStub(t, http.StatusOK, `{"items":[]}`)
			settings := &clearingSettingsStub{}
			env.handlers.settings = settings
			env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
			settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

			mux := http.NewServeMux()
			h := env.handlers
			mux.HandleFunc("GET /api/v1/credits/share-links", h.TokenBankListGiftLinks)
			mux.HandleFunc("POST /api/v1/credits/share-links/{id}/revoke", h.TokenBankRevokeGiftLink)
			mux.HandleFunc("GET /api/v1/credits/share-links/{code}/preview", h.TokenBankPreviewGiftLink)
			mux.HandleFunc("GET /api/v1/token-bank/credits/withdrawals", h.TokenBankListWithdrawals)

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(""))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if requests, _, _, _ := peer.snapshot(); requests != 1 {
				t.Fatalf("peer requests = %d, want 1: %s reads rows that only exist on the clearing node (status %d, body %s)",
					requests, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestTokenBankHubBindGrantRoutesToClearing guards the last hop of a
// withdrawal. The hub comes back to record the grant it created; if that
// arrives on a node with no withdrawal row, the bind answers not-found and the
// grant id is gone for good — which is what makes a later re-issue impossible.
func TestTokenBankHubBindGrantRoutesToClearing(t *testing.T) {
	env := newTokenBankTestEnv(t)
	peer := newClearingPeerStub(t, http.StatusOK, `{"status":"ok"}`)
	settings := &clearingSettingsStub{}
	env.handlers.settings = settings
	env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
	settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

	rec := env.hubPull(t, tokenBankHubAuthStub{}, "/api/hubs/hub-1/token-bank/grants", map[string]any{
		"request_id": "grant-wd",
		"grant_id":   "grant-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("bind grant status = %d, body %s", rec.Code, rec.Body.String())
	}
	if requests, _, _, _ := peer.snapshot(); requests != 1 {
		t.Fatalf("peer requests = %d, want 1: binding the grant id anywhere but the clearing node loses it silently", requests)
	}
}

// TestTokenBankClearingProxyRejectsOversizedRequest pins the request half of
// the proxy's body discipline. The proxy forwards the request byte for byte;
// a body larger than the limit must be refused here rather than forwarded
// truncated, because a truncated JSON would surface on the clearing node as a
// misleading 400 instead of the real "too large" cause.
func TestTokenBankClearingProxyRejectsOversizedRequest(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "big-body@example.test")
	env.seedCredits(t, user.ID, 10)

	peer := newClearingPeerStub(t, http.StatusOK, `{"status":"ok"}`)
	settings := &clearingSettingsStub{}
	env.handlers.settings = settings
	env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
	settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, map[string]any{
		"request_id":   "big-body-wd",
		"amount_micro": 1_000_000,
		// The route decision runs before the body is read, so padding is
		// enough to push the marshaled JSON past the proxy limit.
		"pad": strings.Repeat("a", tokenBankClearingProxyBodyLimit),
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; forwarding a truncated body would come back as a misleading 400 (body %s)", rec.Code, rec.Body.String())
	}
	if requests, _, _, _ := peer.snapshot(); requests != 0 {
		t.Fatalf("peer requests = %d, want 0: an oversized body must not reach the clearing node", requests)
	}
}

// TestTokenBankClearingProxyRejectsTruncatedResponse guards the list half.
// The routed surface includes list endpoints capped at 500 rows of fat JSON —
// well past the request limit — so the response limit has to be its own, and
// an overflowing response must fail loudly instead of arriving as a 200 with
// half a JSON document.
func TestTokenBankClearingProxyRejectsTruncatedResponse(t *testing.T) {
	env := newTokenBankTestEnv(t)
	_, token := env.createUser(t, "big-response@example.test")

	huge := strings.Repeat("a", tokenBankClearingProxyResponseLimit+1)
	peer := newClearingPeerStub(t, http.StatusOK, huge)
	settings := &clearingSettingsStub{}
	env.handlers.settings = settings
	env.handlers.SetTokenBankOrigin(&claimOriginStub{url: peer.server.URL, reachable: true, secret: "peer-secret"})
	settings.put(t, sqlite.TokenBankSettings{ClearingNodeID: "hc-clearing"})

	rec := env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals", token, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; a truncated 200 tells the caller it succeeded while the body is garbage (len %d)", rec.Code, rec.Body.Len())
	}
	if strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Error("response leaked the peer's truncated payload as a success")
	}
}
