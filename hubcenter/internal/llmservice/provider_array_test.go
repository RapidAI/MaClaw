package llmservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestExistingProvidersBecomeIndependentArrays(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "alpha", Name: "Alpha", APIURL: "https://alpha.example", CreditMultiplier: 2, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 4}},
			{ID: "beta", Name: "Beta", APIURL: "https://beta.example", CreditMultiplier: 5},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{
				Name: "auto",
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "alpha", Model: "m"},
					{ProviderID: "beta", Model: "m"},
				},
			}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.ProviderArrays) != 2 {
		t.Fatalf("arrays = %#v, want one array per existing provider", reg.ProviderArrays)
	}
	alpha := findProvider(reg, "alpha")
	beta := findProvider(reg, "beta")
	if alpha == nil || alpha.ArrayID != "alpha" || beta == nil || beta.ArrayID != "beta" {
		t.Fatalf("providers = %#v %#v, want independent arrays", alpha, beta)
	}
	if alpha.CreditMultiplier != 2 || beta.CreditMultiplier != 5 {
		t.Fatalf("multipliers = %v %v, want each provider to keep its own rate", alpha.CreditMultiplier, beta.CreditMultiplier)
	}
	if len(reg.ServiceGroups[0].Models[0].ProviderConfigs) != 2 {
		t.Fatalf("routes = %#v, independent providers must stay separate routes", reg.ServiceGroups[0].Models[0].ProviderConfigs)
	}
}

func TestProviderArraySharesBillingAndCollapsesServiceGroupRoute(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "primary", Name: "Primary", APIURL: "https://a.example", ArrayID: "primary", CreditMultiplier: 2, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 4}},
			{ID: "spare", Name: "Spare", APIURL: "https://b.example", ArrayID: "primary", CreditMultiplier: 9, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 8, OutputCreditsPer10K: 8}},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{
				Name: "auto",
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "primary", Model: "chat", CreditMultiplier: 1.5},
					{ProviderID: "spare", Model: "chat", CreditMultiplier: 4},
				},
			}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	primary := findProvider(reg, "primary")
	spare := findProvider(reg, "spare")
	if primary == nil || spare == nil {
		t.Fatal("missing providers")
	}
	if primary.ArrayID != "primary" || spare.ArrayID != "primary" {
		t.Fatalf("array ids = %s %s", primary.ArrayID, spare.ArrayID)
	}
	if primary.CreditMultiplier != 2 || spare.CreditMultiplier != 2 {
		t.Fatalf("multipliers = %v %v, want the array rate 2", primary.CreditMultiplier, spare.CreditMultiplier)
	}
	if spare.TokenPricing.InputCreditsPer10K != 3 || spare.TokenPricing.OutputCreditsPer10K != 4 {
		t.Fatalf("spare pricing = %#v, want the primary array price", spare.TokenPricing)
	}
	configs := reg.ServiceGroups[0].Models[0].ProviderConfigs
	if len(configs) != 1 || configs[0].ProviderID != "primary" || configs[0].CreditMultiplier != 1.5 {
		t.Fatalf("routes = %#v, want one logical array route keeping the first route rate", configs)
	}

	spare.CreditMultiplier = 4
	spare.TokenPricing = llmpool.TokenPricing{InputCreditsPer10K: 6, OutputCreditsPer10K: 7}
	if err := svc.UpdateProvider(context.Background(), *spare); err != nil {
		t.Fatalf("update spare: %v", err)
	}
	reg, err = svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	primary = findProvider(reg, "primary")
	spare = findProvider(reg, "spare")
	if primary.CreditMultiplier != 4 || spare.CreditMultiplier != 4 {
		t.Fatalf("multipliers after edit = %v %v, want 4", primary.CreditMultiplier, spare.CreditMultiplier)
	}
	if primary.TokenPricing.InputCreditsPer10K != 6 || spare.TokenPricing.OutputCreditsPer10K != 7 {
		t.Fatalf("pricing after edit primary=%#v spare=%#v", primary.TokenPricing, spare.TokenPricing)
	}
}

func TestHandleProxyRequestRotatesAndFailsOverInsideProviderArray(t *testing.T) {
	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		http.Error(w, `{"error":{"message":"slow down"}}`, http.StatusTooManyRequests)
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-spare"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-fo", Name: "Array", APIURL: primary.URL, ArrayID: "array-fo", CreditMultiplier: 2},
			{ID: "array-fo-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-fo", CreditMultiplier: 9},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-fo", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	resp, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}, &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "array-fo" {
		t.Fatalf("provider = %#v, want logical array id", resp)
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("hits = %#v, want primary then spare", hits)
	}
	if resp.CreditMultiplier != 2 {
		t.Fatalf("multiplier = %v, want the shared array rate 2", resp.CreditMultiplier)
	}
}

func TestHandleProxyRequestRoundRobinsProviderArray(t *testing.T) {
	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-primary"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-spare"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-rr", Name: "Array", APIURL: primary.URL, ArrayID: "array-rr"},
			{ID: "array-rr-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-rr"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-rr", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	first, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	second, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if first.ProviderID != "array-rr" || second.ProviderID != "array-rr" {
		t.Fatalf("providers = %s %s, want the logical array", first.ProviderID, second.ProviderID)
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("hits = %#v, want primary then spare", hits)
	}
}

func TestDetachPrimaryStaysIndependent(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "primary", Name: "Primary", APIURL: "https://a.example", ArrayID: "primary", CreditMultiplier: 2},
			{ID: "spare", Name: "Spare", APIURL: "https://b.example", ArrayID: "primary", CreditMultiplier: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "primary", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	for i := 0; i < 2; i++ {
		reg, err := svc.LoadRegistry(context.Background())
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
		primary := findProvider(reg, "primary")
		primary.ArrayID = ""
		primary.ArrayIndependent = true
		if err := svc.UpdateProvider(context.Background(), *primary); err != nil {
			t.Fatalf("detach %d: %v", i, err)
		}
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	primary := findProvider(reg, "primary")
	spare := findProvider(reg, "spare")
	if primary == nil || spare == nil {
		t.Fatal("missing providers")
	}
	if primary.ArrayID == "primary" || primary.ArrayID == "" {
		t.Fatalf("primary array = %q, a second independent save must not rejoin the shared array", primary.ArrayID)
	}
	if spare.ArrayID != "primary" {
		t.Fatalf("spare array = %q, want the original array id", spare.ArrayID)
	}
	configs := reg.ServiceGroups[0].Models[0].ProviderConfigs
	if len(configs) != 1 || configs[0].ProviderID != "primary" {
		t.Fatalf("routes = %#v, want the shared array to keep its route", configs)
	}
	if ids := providerArrayMemberIDs(reg, "primary"); len(ids) != 1 || ids[0] != "spare" {
		t.Fatalf("shared members = %#v, want only spare", ids)
	}
}

func TestCollapseKeepsExplicitRoutePrice(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "primary", Name: "Primary", APIURL: "https://a.example", ArrayID: "primary"},
			{ID: "spare", Name: "Spare", APIURL: "https://b.example", ArrayID: "primary"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{
				Name: "auto",
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "primary", Model: "chat", CreditMultiplier: 1.5},
					{ProviderID: "spare", Model: "chat", CreditMultiplier: 4, TokenPricingOverride: true, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 11, OutputCreditsPer10K: 12}},
				},
			}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	configs := reg.ServiceGroups[0].Models[0].ProviderConfigs
	if len(configs) != 1 || configs[0].ProviderID != "primary" {
		t.Fatalf("routes = %#v, want one logical route", configs)
	}
	if !configs[0].TokenPricingOverride || configs[0].TokenPricing.InputCreditsPer10K != 11 || configs[0].TokenPricing.OutputCreditsPer10K != 12 {
		t.Fatalf("route = %#v, want the explicit price override", configs[0])
	}
}

func TestRotateProviderArrayKeepsCursorInRange(t *testing.T) {
	members := []*llmpool.ProviderConfig{{ID: "a"}, {ID: "b"}}
	providerArrayCursor.Lock()
	providerArrayCursor.next["cursor-bound"] = -5
	providerArrayCursor.Unlock()
	first := rotateProviderArray("cursor-bound", members)
	if len(first) != 2 || first[0].ID != "a" {
		t.Fatalf("negative cursor order = %#v", first)
	}
	providerArrayCursor.Lock()
	stored := providerArrayCursor.next["cursor-bound"]
	providerArrayCursor.Unlock()
	if stored < 0 || stored >= len(members) {
		t.Fatalf("cursor = %d, want 0 or 1", stored)
	}
	providerArrayCursor.Lock()
	providerArrayCursor.next["cursor-bound"] = 5
	providerArrayCursor.Unlock()
	got := rotateProviderArray("cursor-bound", members)
	if len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("wrapped cursor order = %#v, want b then a", got)
	}
}

func TestDetachedPrimaryLeavesSharedArray(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "primary", Name: "Primary", APIURL: "https://a.example", ArrayID: "primary", CreditMultiplier: 2},
			{ID: "spare", Name: "Spare", APIURL: "https://b.example", ArrayID: "primary", CreditMultiplier: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "primary", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	primary := findProvider(reg, "primary")
	primary.ArrayID = ""
	primary.ArrayIndependent = true
	if err := svc.UpdateProvider(context.Background(), *primary); err != nil {
		t.Fatalf("detach: %v", err)
	}
	reg, err = svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	primary = findProvider(reg, "primary")
	primary.CreditMultiplier = 9
	if err := svc.UpdateProvider(context.Background(), *primary); err != nil {
		t.Fatalf("update detached billing: %v", err)
	}
	reg, err = svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload billing: %v", err)
	}
	logical, profile, members := lookupProviderArray(reg, "primary", nil)
	if logical != "primary" || profile == nil || profile.ID != "spare" || profile.CreditMultiplier != 2 {
		t.Fatalf("shared profile = %s %#v, want spare at rate 2", logical, profile)
	}
	if len(members) != 1 || members[0].ID != "spare" {
		t.Fatalf("shared members = %#v", members)
	}
	if got := findProvider(reg, "primary"); got == nil || got.CreditMultiplier != 9 {
		t.Fatalf("detached billing = %#v, want multiplier 9", got)
	}

	refs, err := svc.ProviderReferences(context.Background(), "primary")
	if err != nil {
		t.Fatalf("detached refs: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("detached refs = %#v, the shared route belongs to the remaining array", refs)
	}
	pruned, err := svc.DeleteProvider(context.Background(), "primary", true)
	if err != nil {
		t.Fatalf("delete detached: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %#v, deleting the detached provider must keep the shared route", pruned)
	}
	reg, err = svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if findProvider(reg, "primary") != nil || findProvider(reg, "spare") == nil {
		t.Fatalf("providers = %#v", reg.Providers)
	}
	configs := reg.ServiceGroups[0].Models[0].ProviderConfigs
	if len(configs) != 1 || configs[0].ProviderID != "primary" {
		t.Fatalf("routes = %#v, want the shared array route", configs)
	}

	refs, err = svc.ProviderReferences(context.Background(), "spare")
	if err != nil {
		t.Fatalf("spare refs: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "pool" {
		t.Fatalf("spare refs = %#v, the last member still owns the array route", refs)
	}
	if _, err := svc.DeleteProvider(context.Background(), "spare", false); !errors.Is(err, ErrProviderInUse) {
		t.Fatalf("delete last member without prune: %v", err)
	}
	pruned, err = svc.DeleteProvider(context.Background(), "spare", true)
	if err != nil {
		t.Fatalf("delete last member: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != "Pool" {
		t.Fatalf("pruned = %#v", pruned)
	}
	reg, err = svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	if len(reg.ServiceGroups[0].Models[0].ProviderConfigs) != 0 || len(reg.ServiceGroups[0].Models[0].ProviderIDs) != 0 {
		t.Fatalf("routes = %#v, the last member should remove the array route", reg.ServiceGroups[0].Models[0])
	}
}

func TestQuoteUsesLogicalArrayAndDoesNotRotate(t *testing.T) {
	var hits []string
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "a")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-a"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "b")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-b"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer second.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "quote-a", Name: "A", APIURL: first.URL, ArrayID: "quote-pool"},
			{ID: "quote-b", Name: "B", APIURL: second.URL, ArrayID: "quote-pool"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "quote-pool", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	providerArrayCursor.Lock()
	delete(providerArrayCursor.next, "quote-pool")
	providerArrayCursor.Unlock()

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}}
	quote, err := prepareProxyQuoteDispatch(context.Background(), cfg, req, false)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if proxyDispatchLogicalID(quote) != "quote-pool" {
		t.Fatalf("quote provider = %s, want the logical array", proxyDispatchLogicalID(quote))
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.ProviderID != "quote-pool" {
		t.Fatalf("response provider = %s, want the logical array", resp.ProviderID)
	}
	if len(hits) != 1 || hits[0] != "a" {
		t.Fatalf("hits = %#v, a quote must not consume the array rotation", hits)
	}
}

func TestCustomArrayIDUsesMemberBillingAndLoadBalance(t *testing.T) {
	var hits []string
	serve := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			hits = append(hits, name)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
		}
	}
	memberA := httptest.NewServer(serve("a"))
	defer memberA.Close()
	memberB := httptest.NewServer(serve("b"))
	defer memberB.Close()
	peer := httptest.NewServer(serve("peer"))
	defer peer.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "band-a", Name: "A", APIURL: memberA.URL, ArrayID: "band-pool", CreditMultiplier: 1},
			{ID: "band-b", Name: "B", APIURL: memberB.URL, ArrayID: "band-pool", CreditMultiplier: 1},
			{ID: "band-peer", Name: "Peer", APIURL: peer.URL, CreditMultiplier: 1},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "band-group", Name: "Band", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{
				Name: "auto",
				ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "band-pool", Model: "chat"},
					{ProviderID: "band-peer", Model: "chat"},
				},
			}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, profile := proxyDispatchMeta(reg, "band-pool")
	if profile == nil || profile.CreditMultiplier != 1 || (profile.ID != "band-a" && profile.ID != "band-b") {
		t.Fatalf("array meta = %#v, want a member of band-pool", profile)
	}
	if proxyLogicalRouteSkipWRR(nil, reg, "band-pool", nil) {
		t.Fatal("a live array must stay in the load-balance rotation")
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	req := &ProxyRequest{ServiceGroupID: "band-group", Body: map[string]any{"model": "auto"}}
	for i := 0; i < 2; i++ {
		resp, err := HandleProxyRequest(context.Background(), cfg, req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if resp.ProviderID != "band-pool" && resp.ProviderID != "band-peer" {
			t.Fatalf("provider = %s", resp.ProviderID)
		}
	}
	seen := map[string]bool{}
	for _, hit := range hits {
		seen[hit] = true
	}
	if !seen["peer"] || (!seen["a"] && !seen["b"]) {
		t.Fatalf("hits = %#v, the array must share rotation with the other provider", hits)
	}
}

func TestFailedArrayMemberIsSkippedDuringPause(t *testing.T) {
	prev := arrayMemberFailurePause
	arrayMemberFailurePause = time.Hour
	t.Cleanup(func() {
		arrayMemberFailurePause = prev
		clearArrayMemberPause("array-pause")
		clearArrayMemberPause("array-pause-b")
	})

	var hits []string
	fail := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			hits = append(hits, name)
			http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
		}
	}
	primary := httptest.NewServer(fail("primary"))
	defer primary.Close()
	spare := httptest.NewServer(fail("spare"))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-pause", Name: "Array", APIURL: primary.URL, ArrayID: "array-pause"},
			{ID: "array-pause-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-pause"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-pause", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}}
	if _, err := HandleProxyRequest(context.Background(), cfg, req); err == nil {
		t.Fatal("first request should fail after both members answer")
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("first hits = %#v, want both members once", hits)
	}

	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !proxyLogicalRouteSkipWRR(cfg, reg, "array-pause", nil) {
		t.Fatal("an array whose members are all cooling must leave the load-balance rotation")
	}

	before := len(hits)
	_, err = HandleProxyRequest(context.Background(), cfg, req)
	if err == nil || !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("second request error = %v, want cooling down", err)
	}
	if len(hits) != before {
		t.Fatalf("second hits = %#v, cooling members must not be called", hits)
	}

	providerArrayFailurePause.Lock()
	for _, id := range []string{"array-pause", "array-pause-b"} {
		state := providerArrayFailurePause.states[providerIDKey(id)]
		if state == nil {
			state = &arrayMemberPauseState{}
			providerArrayFailurePause.states[providerIDKey(id)] = state
		}
		state.until = time.Now().Add(-time.Second)
		state.probe = false
	}
	providerArrayFailurePause.Unlock()
	if _, err := HandleProxyRequest(context.Background(), cfg, req); err == nil {
		t.Fatal("request after the pause should reach the members again")
	}
	if len(hits) != before+2 {
		t.Fatalf("hits after pause = %#v, want both members tried again", hits)
	}
}

func TestFailedArrayMemberPauseUsesSibling(t *testing.T) {
	prev := arrayMemberFailurePause
	arrayMemberFailurePause = time.Hour
	t.Cleanup(func() {
		arrayMemberFailurePause = prev
		clearArrayMemberPause("array-live")
		clearArrayMemberPause("array-live-b")
	})

	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-spare"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-live", Name: "Array", APIURL: primary.URL, ArrayID: "array-live"},
			{ID: "array-live-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-live"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-live", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}}
	first, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if first.ProviderID != "array-live" {
		t.Fatalf("provider = %s, want the logical array", first.ProviderID)
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("first hits = %#v, want the failed member once then its sibling", hits)
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, req); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if len(hits) != 3 || hits[2] != "spare" {
		t.Fatalf("second hits = %#v, the paused member must not be called", hits)
	}
}

func TestCoolingArrayMemberStaysSkippedWhenSiblingIsPaused(t *testing.T) {
	prev := arrayMemberFailurePause
	arrayMemberFailurePause = time.Hour
	t.Cleanup(func() {
		arrayMemberFailurePause = prev
		clearArrayMemberPause("array-one")
		clearArrayMemberPause("array-one-b")
	})

	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		http.Error(w, `{"error":{"message":"should not be called"}}`, http.StatusBadGateway)
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-one", Name: "Array", APIURL: primary.URL, ArrayID: "array-one"},
			{ID: "array-one-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-one", Paused: true},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-one", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}}
	if _, err := HandleProxyRequest(context.Background(), cfg, req); err == nil {
		t.Fatal("first request should fail")
	}
	if len(hits) != 1 || hits[0] != "primary" {
		t.Fatalf("first hits = %#v, want the live member once", hits)
	}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil || !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("second request error = %v, want cooling down", err)
	}
	if len(hits) != 1 {
		t.Fatalf("second hits = %#v, the paused member must stay skipped", hits)
	}
}

func TestStreamSkipsCoolingArrayMember(t *testing.T) {
	prev := arrayMemberFailurePause
	arrayMemberFailurePause = time.Hour
	t.Cleanup(func() {
		arrayMemberFailurePause = prev
		clearArrayMemberPause("array-stream")
		clearArrayMemberPause("array-stream-b")
	})

	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-stream", Name: "Array", APIURL: primary.URL, ArrayID: "array-stream"},
			{ID: "array-stream-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-stream"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official", AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-stream", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto", "stream": true}}
	writer := newLockedResponseRecorder()
	if err := HandleProxyStreamRequest(context.Background(), cfg, req, writer); err != nil {
		t.Fatalf("first stream: %v", err)
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("first hits = %#v, want the failed member once then its sibling", hits)
	}
	if err := HandleProxyStreamRequest(context.Background(), cfg, req, newLockedResponseRecorder()); err != nil {
		t.Fatalf("second stream: %v", err)
	}
	if len(hits) != 3 || hits[2] != "spare" {
		t.Fatalf("second hits = %#v, the paused member must not be called", hits)
	}
}

func TestArrayMemberProbeIsSingleFlight(t *testing.T) {
	clearArrayMemberPause("probe-member")
	t.Cleanup(func() { clearArrayMemberPause("probe-member") })
	pauseArrayMember("probe-member", time.Hour)
	if skip, claimed := arrayMemberDialGate("probe-member"); !skip || claimed {
		t.Fatalf("during pause skip=%v claimed=%v", skip, claimed)
	}
	providerArrayFailurePause.Lock()
	providerArrayFailurePause.states[providerIDKey("probe-member")].until = time.Now().Add(-time.Second)
	providerArrayFailurePause.Unlock()

	skip, claimed := arrayMemberDialGate("probe-member")
	if skip || !claimed {
		t.Fatalf("first probe skip=%v claimed=%v", skip, claimed)
	}
	if skip, claimed := arrayMemberDialGate("probe-member"); !skip || claimed {
		t.Fatalf("second caller skip=%v claimed=%v, want it to wait out the probe", skip, claimed)
	}
	releaseArrayMemberProbe("probe-member")
	skip, claimed = arrayMemberDialGate("probe-member")
	if skip || !claimed {
		t.Fatalf("after release skip=%v claimed=%v, want the next caller to be the only probe", skip, claimed)
	}
	if skip, claimed := arrayMemberDialGate("probe-member"); !skip || claimed {
		t.Fatalf("while probing skip=%v claimed=%v, want everyone else to skip", skip, claimed)
	}
}

func TestStreamRoundRobinDoesNotReplayTheSameMember(t *testing.T) {
	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"b\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-srr", Name: "Array", APIURL: primary.URL, ArrayID: "array-srr"},
			{ID: "array-srr-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-srr"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official", AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-srr", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto", "stream": true}}
	for i := 0; i < 2; i++ {
		if err := HandleProxyStreamRequest(context.Background(), cfg, req, newLockedResponseRecorder()); err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("hits = %#v, want each member once", hits)
	}
}

func TestRecoveringArrayMemberYieldsToHealthySibling(t *testing.T) {
	prev := arrayMemberFailurePause
	arrayMemberFailurePause = time.Hour
	t.Cleanup(func() {
		arrayMemberFailurePause = prev
		clearArrayMemberPause("array-yield")
		clearArrayMemberPause("array-yield-b")
	})

	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "primary")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer primary.Close()
	spare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "spare")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"from-spare"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer spare.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "array-yield", Name: "Array", APIURL: primary.URL, ArrayID: "array-yield"},
			{ID: "array-yield-b", Name: "Array B", APIURL: spare.URL, ArrayID: "array-yield"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "pool", Name: "Pool", AgentID: "maclaw_official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "array-yield", Model: "chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	req := &ProxyRequest{ServiceGroupID: "pool", Body: map[string]any{"model": "auto"}}
	if _, err := HandleProxyRequest(context.Background(), cfg, req); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if len(hits) != 2 || hits[0] != "primary" || hits[1] != "spare" {
		t.Fatalf("first hits = %#v", hits)
	}

	providerArrayFailurePause.Lock()
	state := providerArrayFailurePause.states[providerIDKey("array-yield")]
	if state == nil {
		t.Fatal("failed member was not paused")
	}
	state.until = time.Now().Add(-time.Second)
	state.probe = false
	providerArrayFailurePause.Unlock()

	if _, err := HandleProxyRequest(context.Background(), cfg, req); err != nil {
		t.Fatalf("request during recovery grace: %v", err)
	}
	if len(hits) != 3 || hits[2] != "spare" {
		t.Fatalf("hits during grace = %#v, want the healthy sibling and not the recovering member", hits)
	}

	providerArrayFailurePause.Lock()
	if state = providerArrayFailurePause.states[providerIDKey("array-yield")]; state != nil {
		state.until = time.Now().Add(-2 * arrayMemberFailurePause)
		state.probe = false
	}
	providerArrayFailurePause.Unlock()
	before := len(hits)
	seenPrimary := false
	for i := 0; i < 2 && !seenPrimary; i++ {
		if _, err := HandleProxyRequest(context.Background(), cfg, req); err != nil {
			t.Fatalf("request after grace: %v", err)
		}
		for _, hit := range hits[before:] {
			if hit == "primary" {
				seenPrimary = true
			}
		}
	}
	if !seenPrimary {
		t.Fatalf("hits after grace = %#v, want the recovering member tried again", hits)
	}
}
