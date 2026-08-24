package llmservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestApplyOfficialForwardMetaCopiesWorkflowHints(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://hc.example/api/llm/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithOfficialForwardMeta(context.Background(), OfficialForwardMeta{
		WorkloadClass: llmpool.WorkloadFallbackBalanced,
		ClassSource:   llmpool.ClassSourceFallback,
		WorkflowType:  "coding",
		PhaseKind:     "execution",
		TaskType:      "reasoning",
	})
	applyOfficialForwardMeta(req, ctx)
	if got := req.Header.Get(llmpool.WorkloadClassHeader); got != "" {
		t.Fatalf("balanced must not be sent as P0 class, got %q", got)
	}
	if got := req.Header.Get(llmpool.WorkflowTypeHeader); got != "coding" {
		t.Fatalf("workflow type = %q", got)
	}
	if got := req.Header.Get(llmpool.PhaseKindHeader); got != "execution" {
		t.Fatalf("phase kind = %q", got)
	}
	if got := req.Header.Get(llmpool.TaskTypeHeader); got != "reasoning" {
		t.Fatalf("task type = %q", got)
	}
}

func TestApplyOfficialForwardMetaSendsFrozenClass(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://hc.example/api/llm/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithOfficialForwardMeta(context.Background(), OfficialForwardMeta{
		WorkloadClass: llmpool.WorkloadClassCode,
		ResolvedModel: llmpool.OfficialTierMid,
	})
	applyOfficialForwardMeta(req, ctx)
	if got := req.Header.Get(llmpool.WorkloadClassHeader); got != llmpool.WorkloadClassCode {
		t.Fatalf("class = %q", got)
	}
	if got := req.Header.Get(llmpool.ResolvedModelHeader); got != llmpool.OfficialTierMid {
		t.Fatalf("resolved = %q", got)
	}
}

func TestMaClawProviderClientForwardFailsOverToNextHubCenter(t *testing.T) {
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer failed.Close()
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("X-MaClaw-Service-Group-ID") != "redeem" {
			t.Fatalf("service group missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer working.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: failed.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{failed.URL, working.URL})
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto","messages":[]}`), "tenant_default", "redeem")
	if err != nil || status != http.StatusOK {
		t.Fatalf("Forward() status=%d err=%v body=%s", status, err, body)
	}
	if got := client.CurrentHubCenterURL(); got != working.URL {
		t.Fatalf("bound URL=%s want %s", got, working.URL)
	}
}

func TestMaClawProviderClientForwardDoesNotReplayJSONProviderError(t *testing.T) {
	calledFallback := false
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"provider unavailable"}}`))
	}))
	defer failed.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calledFallback = true }))
	defer fallback.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: failed.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{failed.URL, fallback.URL})
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto","messages":[]}`), "tenant_default", "redeem")
	if err != nil || status != http.StatusBadGateway {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if calledFallback {
		t.Fatal("replayed a JSON HubCenter/provider error on the fallback node")
	}
	if !strings.Contains(string(body), "provider unavailable") {
		t.Fatalf("body=%s", body)
	}
}

func TestMaClawProviderClientForwardStreamFailsOverBeforeReturningStream(t *testing.T) {
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer failed.Close()
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer working.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: failed.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{failed.URL, working.URL})
	resp, err := client.ForwardStream(context.Background(), []byte(`{"model":"auto","messages":[]}`), "tenant_default", "redeem")
	if err != nil {
		t.Fatalf("ForwardStream() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := client.CurrentHubCenterURL(); got != working.URL {
		t.Fatalf("bound URL=%s want %s", got, working.URL)
	}
}

func TestMaClawProviderClientForwardRedirectsToBoundNode(t *testing.T) {
	wrongHits := 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("X-Tenant-ID") != "tenant_a" {
			t.Fatalf("tenant = %q", r.Header.Get("X-Tenant-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer owner.Close()
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wrongHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"` + owner.URL + `","error":{"message":"tenant bound to node hc-3, please redirect","code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"` + owner.URL + `"}}`))
	}))
	defer wrong.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: wrong.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{wrong.URL})
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto","messages":[]}`), "tenant_a", "redeem")
	if err != nil || status != http.StatusOK {
		t.Fatalf("Forward() status=%d err=%v body=%s", status, err, body)
	}
	if wrongHits != 1 {
		t.Fatalf("wrong node hits = %d, want 1", wrongHits)
	}
	if got := client.TenantHubCenterURL("tenant_a"); got != owner.URL {
		t.Fatalf("tenant pin = %s want %s", got, owner.URL)
	}
}

func TestMaClawProviderClientTenantPinSurvivesPreferredURLChange(t *testing.T) {
	var ownerHits, otherHits int
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ownerHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"owner"}}]}`))
	}))
	defer owner.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"other"}}]}`))
	}))
	defer other.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: owner.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{owner.URL, other.URL})
	if _, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a"); err != nil {
		t.Fatalf("first forward: %v", err)
	}
	client.SetBoundURL(other.URL)
	if _, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a"); err != nil || status != http.StatusOK {
		t.Fatalf("pinned forward status=%d err=%v", status, err)
	}
	if ownerHits != 2 || otherHits != 0 {
		t.Fatalf("hits owner=%d other=%d, want owner=2 other=0", ownerHits, otherHits)
	}
	if _, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_b"); err != nil || status != http.StatusOK {
		t.Fatalf("unpinned forward status=%d err=%v", status, err)
	}
	if otherHits != 1 {
		t.Fatalf("unpinned tenant should use preferred node, otherHits=%d", otherHits)
	}
}

func TestMaClawProviderClientForwardStreamRedirectsToBoundNode(t *testing.T) {
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer owner.Close()
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(officialRedirectNodeHeader, "hc-3")
		w.Header().Set(officialRedirectURLHeader, owner.URL)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"message":"tenant bound to node hc-3, please redirect","code":"TENANT_BOUND_TO_NODE"}}`))
	}))
	defer wrong.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: wrong.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{wrong.URL})
	resp, err := client.ForwardStream(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err != nil {
		t.Fatalf("ForwardStream() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := client.TenantHubCenterURL("tenant_a"); got != owner.URL {
		t.Fatalf("tenant pin = %s want %s", got, owner.URL)
	}
}

func TestParseHubCenterBindingRedirect(t *testing.T) {
	got, ok := parseHubCenterBindingRedirect(http.StatusConflict, []byte(`{"error":{"message":"tenant bound to node hc-3, please redirect","code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"https://hubs2.maclaw.top"}}`), nil)
	if !ok || got.NodeID != "hc-3" || got.RedirectURL != "https://hubs2.maclaw.top" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if _, ok := parseHubCenterBindingRedirect(http.StatusConflict, []byte(`{"code":"HUB_NOT_READY_ON_NODE","message":"Hub metadata is not available on this node yet."}`), nil); ok {
		t.Fatal("HUB_NOT_READY_ON_NODE must not look like a tenant binding redirect")
	}
	header := http.Header{"Location": []string{"https://evil.example/api/llm/v1/chat/completions"}}
	if _, ok := parseHubCenterBindingRedirect(http.StatusConflict, []byte(`{"code":"HUB_NOT_READY_ON_NODE","message":"Hub metadata is not available on this node yet."}`), header); ok {
		t.Fatal("Location on an unrelated 409 must not look like a tenant binding redirect")
	}
	got, ok = parseHubCenterBindingRedirect(http.StatusConflict, []byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"javascript:alert(1)"}`), nil)
	if !ok || got.NodeID != "hc-3" || got.RedirectURL != "" {
		t.Fatalf("non-http redirect_url should be ignored, got=%+v ok=%v", got, ok)
	}
	got, ok = parseHubCenterBindingRedirect(http.StatusConflict, []byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"https://"}`), nil)
	if !ok || got.NodeID != "hc-3" || got.RedirectURL != "" {
		t.Fatalf("hostless redirect_url should be ignored, got=%+v ok=%v", got, ok)
	}
	got, ok = parseHubCenterBindingRedirect(http.StatusConflict, []byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"https://evil:pass@hubs2.maclaw.top"}`), nil)
	if !ok || got.NodeID != "hc-3" || got.RedirectURL != "" {
		t.Fatalf("redirect_url with userinfo should be ignored, got=%+v ok=%v", got, ok)
	}
}

func TestMaClawProviderClientDoesNotReturnBareConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"HUB_NOT_READY_ON_NODE","message":"Hub metadata is not available on this node yet."}`))
	}))
	defer srv.Close()
	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: srv.URL, HubID: "hub1", MachineToken: "secret"})
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err == nil || status != 0 || len(body) != 0 {
		t.Fatalf("status=%d err=%v body=%q, want empty result plus error", status, err, body)
	}
	if strings.Contains(err.Error(), "please redirect") {
		t.Fatalf("must not leak binding copy: %v", err)
	}
}

func TestMarkOwnerUnreachableMovesPreferredURL(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	live := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer dead.Close()
	defer live.Close()
	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: dead.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{dead.URL, live.URL})
	client.SetBoundURL(dead.URL)
	client.markOwnerUnreachable(dead.URL)
	if got := client.CurrentHubCenterURL(); got != live.URL {
		t.Fatalf("CurrentHubCenterURL = %q, want live sibling %q", got, live.URL)
	}
}

func TestMaClawProviderClientStopsWhenOwnerAlreadyTried(t *testing.T) {
	otherHits := 0
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		otherHits++
	}))
	defer other.Close()
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	ownerURL := owner.URL
	owner.Close()
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"` + ownerURL + `"}`))
	}))
	defer wrong.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: wrong.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{wrong.URL, ownerURL, other.URL})
	_, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err == nil || !errors.Is(err, corellm.ErrOfficialOwnerUnreachable) || !strings.Contains(err.Error(), "hc-3") {
		t.Fatalf("error = %v, want owner hc-3 unreachable", err)
	}
	if otherHits != 0 {
		t.Fatalf("other node hits = %d, want 0 after owner already failed", otherHits)
	}
	if got := client.TenantHubCenterURL("tenant_a"); got != "" {
		t.Fatalf("TenantHubCenterURL = %q, want empty after owner failure", got)
	}
}

func TestMaClawProviderClientSkipsOwnerOnCooldown(t *testing.T) {
	ownerHits := 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ownerHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"owner"}}]}`))
	}))
	defer owner.Close()
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"` + owner.URL + `"}`))
	}))
	defer wrong.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: wrong.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{wrong.URL, owner.URL})
	client.markOwnerUnreachable(owner.URL)
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err == nil || !strings.Contains(err.Error(), "hc-3") || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v, want owner cooldown unreachable", err)
	}
	if status != 0 || len(body) != 0 {
		t.Fatalf("status=%d body=%q, want empty result so Hub does not replay sibling 409", status, body)
	}
	if ownerHits != 0 {
		t.Fatalf("owner hits = %d, want 0 during cooldown", ownerHits)
	}
}

func TestMaClawProviderClientSkipsCoolingPreferredURL(t *testing.T) {
	ownerHits := 0
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		ownerHits++
	}))
	defer owner.Close()
	siblingHits := 0
	sibling := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		siblingHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"sibling"}}]}`))
	}))
	defer sibling.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: owner.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{owner.URL, sibling.URL})
	client.SetBoundURL(owner.URL)
	client.markOwnerUnreachable(owner.URL)
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err != nil || status != http.StatusOK {
		t.Fatalf("Forward() status=%d err=%v body=%s", status, err, body)
	}
	if ownerHits != 0 {
		t.Fatalf("owner hits = %d, want 0 while preferred URL is cooling", ownerHits)
	}
	if siblingHits != 1 {
		t.Fatalf("sibling hits = %d, want 1", siblingHits)
	}
}

func TestOwnerUnreachableErrorOmitsOwnerURL(t *testing.T) {
	err := ownerUnreachableError("hc-3", "https://hubs2.maclaw.top", errors.New(`Get "https://hubs2.maclaw.top/api/client/quality": EOF`))
	if !errors.Is(err, corellm.ErrOfficialOwnerUnreachable) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "hubs2") || strings.Contains(err.Error(), "http") {
		t.Fatalf("public error leaked owner URL: %v", err)
	}
	wrapped := bindingLoopFailure("hc-3", "https://hubs2.maclaw.top", fmt.Errorf(`maclaw official: forward failed: Get "https://hubs2.maclaw.top/api/llm/v1/chat/completions": EOF`))
	if !errors.Is(wrapped, corellm.ErrOfficialOwnerUnreachable) {
		t.Fatalf("wrapped = %v", wrapped)
	}
	if strings.Contains(wrapped.Error(), "hubs2") || strings.Contains(wrapped.Error(), "http") {
		t.Fatalf("loop failure leaked owner URL: %v", wrapped)
	}
}

func TestAdminHTTPClientUsesShortTimeout(t *testing.T) {
	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://hubs.maclaw.top", TimeoutSec: 600})
	got := client.adminHTTPClient()
	if got.Timeout != officialAdminRequestTimeout {
		t.Fatalf("admin timeout = %s, want %s", got.Timeout, officialAdminRequestTimeout)
	}
	if client.httpClient().Timeout <= officialAdminRequestTimeout {
		t.Fatal("official LLM client should keep the long timeout")
	}
}

func TestFailRequiredOwnerUnlessCanceledDoesNotCoolOwner(t *testing.T) {
	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://hubs.maclaw.top", HubID: "hub1", MachineToken: "secret"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.failRequiredOwnerUnlessCanceled(ctx, "hc-3", "https://hubs2.maclaw.top", context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	client.mu.Lock()
	_, cooling := client.ownerCooldown["https://hubs2.maclaw.top"]
	client.mu.Unlock()
	if cooling {
		t.Fatal("canceled request must not cool the bound owner")
	}
}

func TestMaClawProviderClientRedirectsWhenQualityProbeReturns5xx(t *testing.T) {
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			http.Error(w, "quality down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer owner.Close()
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"TENANT_BOUND_TO_NODE","node_id":"hc-3","redirect_url":"` + owner.URL + `"}`))
	}))
	defer wrong.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: wrong.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{wrong.URL})
	body, status, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a")
	if err != nil || status != http.StatusOK {
		t.Fatalf("Forward() status=%d err=%v body=%s", status, err, body)
	}
}

func TestMaClawProviderClientTenantPinExpires(t *testing.T) {
	var firstHits, secondHits int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"first"}}]}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"second"}}]}`))
	}))
	defer second.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: first.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{first.URL, second.URL})
	if _, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a"); err != nil {
		t.Fatalf("first forward: %v", err)
	}
	client.SetBoundURL(second.URL)
	client.mu.Lock()
	pin := client.tenantBound["tenant_a"]
	pin.PinnedAt = time.Now().Add(-officialTenantPinTTL - time.Second)
	client.tenantBound["tenant_a"] = pin
	client.mu.Unlock()
	if _, _, err := client.Forward(context.Background(), []byte(`{"model":"auto"}`), "tenant_a"); err != nil {
		t.Fatalf("expired pin forward: %v", err)
	}
	if firstHits != 1 || secondHits != 1 {
		t.Fatalf("hits first=%d second=%d, want first=1 second=1 after pin expiry", firstHits, secondHits)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type comparableRoundTripper struct {
	fn roundTripFunc
}

func (rt *comparableRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.fn(req)
}

func TestMaClawProviderClientForwardStreamUsesStreamingClientWithoutTotalTimeout(t *testing.T) {
	transport := &comparableRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			t.Fatalf("path = %q, want chat completions endpoint", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("Authorization = %q, want bearer secret", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Tenant-ID") != "tenant_acme" {
			t.Fatalf("X-Tenant-ID = %q, want tenant_acme", r.Header.Get("X-Tenant-ID"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	}}
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Timeout: 120 * time.Second, Transport: transport}

	streamClient := client.streamHTTPClient()
	if streamClient == client.HTTPClient {
		t.Fatal("streamHTTPClient() reused timeout client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", streamClient.Timeout)
	}
	if streamClient.Transport != transport {
		t.Fatal("streamHTTPClient() did not preserve transport")
	}
	if client.HTTPClient.Timeout != 120*time.Second {
		t.Fatalf("base client timeout = %s, want 120s", client.HTTPClient.Timeout)
	}

	resp, err := client.ForwardStream(context.Background(), []byte(`{"stream":true}`), "tenant_acme")
	if err != nil {
		t.Fatalf("ForwardStream() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMaClawProviderClientForwardStreamBoundsResponseHeaderWait(t *testing.T) {
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Timeout: 120 * time.Second}

	streamClient := client.streamHTTPClient()
	if streamClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", streamClient.Timeout)
	}
	transport, ok := streamClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("stream transport = %T, want *http.Transport", streamClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 120*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 120s", transport.ResponseHeaderTimeout)
	}
	if client.HTTPClient.Transport != nil {
		t.Fatal("base client transport was mutated")
	}
}

func TestMaClawProviderClientForwardStreamBoundsResponseHeaderWaitWithZeroTimeoutClient(t *testing.T) {
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{}

	streamClient := client.streamHTTPClient()
	if streamClient == client.HTTPClient {
		t.Fatal("streamHTTPClient() reused unbounded default client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", streamClient.Timeout)
	}
	transport, ok := streamClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("stream transport = %T, want *http.Transport", streamClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 120*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 120s", transport.ResponseHeaderTimeout)
	}
	if client.HTTPClient.Transport != nil {
		t.Fatal("base client transport was mutated")
	}
}

func TestQueryAuthorizationRefreshesCredentialsWhenMissing(t *testing.T) {
	refreshCalls := 0
	var gotHubID string
	var gotAuth string
	var gotTenantID string

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://hubcenter.example.com"})
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/llm/v1/authorization" {
			t.Fatalf("path = %q, want authorization endpoint", r.URL.Path)
		}
		gotHubID = r.URL.Query().Get("hub_id")
		gotTenantID = r.URL.Query().Get("tenant_id")
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("X-Hub-ID") != "hub_refreshed" {
			t.Fatalf("X-Hub-ID = %q, want hub_refreshed", r.Header.Get("X-Hub-ID"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub_refreshed","tenant_id":"tenant_acme","allow_external_providers":true}`)),
			Request:    r,
		}, nil
	})}

	client.SetRefreshCredentials(func() (string, string) {
		refreshCalls++
		return "hub_refreshed", "secret_refreshed"
	})

	status, err := client.QueryAuthorization(context.Background(), "tenant_acme")
	if err != nil {
		t.Fatalf("QueryAuthorization() error = %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d, want 1", refreshCalls)
	}
	if gotHubID != "hub_refreshed" {
		t.Fatalf("hub_id query = %q, want hub_refreshed", gotHubID)
	}
	if gotTenantID != "tenant_acme" {
		t.Fatalf("tenant_id query = %q, want tenant_acme", gotTenantID)
	}
	if gotAuth != "Bearer secret_refreshed" {
		t.Fatalf("Authorization = %q, want bearer secret", gotAuth)
	}
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("status.AllowExternalProviders = %v, want true", status)
	}
}

func TestQueryAuthorizationFailsOverToNextCandidate(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer down.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/authorization" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub1","tenant_id":"tenant_a","allow_external_providers":true}`))
	}))
	defer up.Close()

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: down.URL, HubID: "hub1", MachineToken: "secret"})
	client.SetHubCenterCandidates([]string{down.URL, up.URL})
	status, err := client.QueryAuthorization(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("QueryAuthorization() error = %v", err)
	}
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("status = %+v", status)
	}
}

func TestTenantLLMAccessControlCachesAuthorizationTenantAliases(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_acme", &TenantAuthorizationStatus{
		HubID:                  "hub1",
		TenantID:               "tenant_acme",
		AllowExternalProviders: true,
	})

	status := ac.GetAuthorizationStatus(context.Background(), "acme")
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("alias status = %#v, want allowed", status)
	}
}

func TestTenantLLMAccessControlFetchCachesAuthorizationTenantAliases(t *testing.T) {
	requests := 0
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if got := r.URL.Query().Get("tenant_id"); got != "acme" {
			t.Fatalf("tenant_id query = %q, want acme", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub1","tenant_id":"tenant_acme","allow_external_providers":true}`)),
			Request:    r,
		}, nil
	})}
	ac := NewTenantLLMAccessControl(client)

	status := ac.GetAuthorizationStatus(context.Background(), "acme")
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("status = %#v, want allowed", status)
	}
	alias := ac.GetAuthorizationStatus(context.Background(), "tenant_acme")
	if alias == nil || !alias.AllowExternalProviders {
		t.Fatalf("alias status = %#v, want allowed", alias)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestEnsureBuiltinProviderDefaultsOfficialGroupToGrantRequired(t *testing.T) {
	reg := &Registry{}

	if !EnsureBuiltinProvider(reg) {
		t.Fatal("EnsureBuiltinProvider changed = false, want true")
	}

	group := reg.FindModelServiceGroup(MaClawOfficialServiceGroupID)
	if group == nil {
		t.Fatal("missing official service group")
	}
	if group.AccessPolicy != AccessPolicyGrantRequired {
		t.Fatalf("access policy = %q, want %q", group.AccessPolicy, AccessPolicyGrantRequired)
	}
}

func TestEnsureBuiltinProviderRepairsLegacyFreeOfficialGroup(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:           MaClawOfficialServiceGroupID,
		Name:         MaClawOfficialServiceGroupName,
		Description:  "MaClaw 官方 LLM 服务，通过 HubCenter 提供算力",
		AccessPolicy: AccessPolicyFree,
		Models: []ModelServiceModel{{
			Name:        "auto",
			ProviderIDs: []string{MaClawOfficialProviderID},
		}},
	}}}

	if !EnsureBuiltinProvider(reg) {
		t.Fatal("EnsureBuiltinProvider changed = false, want true")
	}
	if got := reg.ModelServiceGroups[0].AccessPolicy; got != AccessPolicyGrantRequired {
		t.Fatalf("access policy = %q, want %q", got, AccessPolicyGrantRequired)
	}
}

func TestEnsureBuiltinProviderDoesNotOverrideCustomizedOfficialGroupPolicy(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:           MaClawOfficialServiceGroupID,
		Name:         MaClawOfficialServiceGroupName,
		Description:  "Custom tenant policy",
		AccessPolicy: AccessPolicyFree,
		Models: []ModelServiceModel{{
			Name:        "auto",
			ProviderIDs: []string{MaClawOfficialProviderID},
		}},
	}}}

	if EnsureBuiltinProvider(reg) {
		t.Fatal("EnsureBuiltinProvider changed customized group")
	}
	if got := reg.ModelServiceGroups[0].AccessPolicy; got != AccessPolicyFree {
		t.Fatalf("access policy = %q, want %q", got, AccessPolicyFree)
	}
}

func TestOfficialForwardBillingPrefersTrailers(t *testing.T) {
	resp := &http.Response{
		Header:  make(http.Header),
		Trailer: make(http.Header),
	}
	resp.Header.Set(llmpool.CreditMultiplierHeader, "1")
	resp.Header.Set(llmpool.ProviderIDHeader, "openai")
	resp.Trailer.Set(llmpool.CreditMultiplierHeader, "0.5")
	resp.Trailer.Set(llmpool.ProviderIDHeader, "deepseek")
	multiplier, providerID := officialForwardBilling(resp)
	if multiplier != 0.5 || providerID != "deepseek" {
		t.Fatalf("billing = %v %q, want trailer 0.5 deepseek", multiplier, providerID)
	}
}
