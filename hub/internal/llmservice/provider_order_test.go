package llmservice

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestOrderProvidersForRequestUsesProviderScopedParams(t *testing.T) {
	model := &AuthorizedModel{
		Name:             "auto",
		ProviderIDs:      []string{"provider-doc", "provider-tools"},
		CapabilityTags:   []string{"document", "tools"},
		Priority:         10,
		ResolutionTier:   1,
		CreditMultiplier: 1,
		ProviderCapabilityTags: map[string][]string{
			"provider-doc":   {"document"},
			"provider-tools": {"tools"},
		},
		ProviderPriorities: map[string]int{
			"provider-doc":   20,
			"provider-tools": 60,
		},
		ProviderResolutionTiers: map[string]int{
			"provider-doc":   2,
			"provider-tools": 1,
		},
		ProviderCreditMultipliers: map[string]float64{
			"provider-doc":   1.2,
			"provider-tools": 1,
		},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Use tools to search and fetch the answer."}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	ordered := OrderProvidersForRequest(body, model)
	if len(ordered) != 2 {
		t.Fatalf("ordered providers = %#v", ordered)
	}
	if ordered[0] != "provider-tools" || ordered[1] != "provider-doc" {
		t.Fatalf("unexpected provider order: %#v", ordered)
	}
}

func TestOrderProvidersForRequestLoadBalancesEqualBand(t *testing.T) {
	requestProviderWRR.Reset()
	model := &AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{"aisi", "oc1"},
	}
	first := OrderProvidersForRequest(nil, model)
	second := OrderProvidersForRequest(nil, model)
	if len(first) != 2 || first[0] != "aisi" || first[1] != "oc1" {
		t.Fatalf("first = %#v, want aisi then oc1", first)
	}
	if len(second) != 2 || second[0] != "oc1" || second[1] != "aisi" {
		t.Fatalf("second = %#v, want oc1 then aisi", second)
	}
}

func TestPeekProvidersForRequestDoesNotRotateWRR(t *testing.T) {
	requestProviderWRR.Reset()
	model := &AuthorizedModel{Name: "auto", ProviderIDs: []string{"aisi", "oc1"}}
	if got := PeekProvidersForRequest(nil, model); len(got) != 2 || got[0] != "aisi" {
		t.Fatalf("peek = %#v, want aisi first", got)
	}
	if got := OrderProvidersForRequest(nil, model); len(got) != 2 || got[0] != "aisi" {
		t.Fatalf("first rotate = %#v, want aisi first after peek", got)
	}
}

func TestOrderProvidersForRequestWithMetaSkipsCircuitOpenFromWRR(t *testing.T) {
	requestProviderWRR.Reset()
	model := &AuthorizedModel{Name: "auto", ProviderIDs: []string{"open", "live-a", "live-b"}}
	metas := map[string]llmpool.ProviderDispatchMeta{
		"open":   {ID: "open", Sequence: 1, MaxConcurrency: 10, SkipWRR: true},
		"live-a": {ID: "live-a", Sequence: 2, MaxConcurrency: 10},
		"live-b": {ID: "live-b", Sequence: 3, MaxConcurrency: 10},
	}
	first := OrderProvidersForRequestWithMeta(nil, model, metas, time.Time{})
	second := OrderProvidersForRequestWithMeta(nil, model, metas, time.Time{})
	if len(first) != 3 || first[0].Route.ProviderID != "live-a" {
		t.Fatalf("first = %#v, want live-a (circuit-open excluded from WRR)", first)
	}
	if first[1].Route.ProviderID != "open" || first[2].Route.ProviderID != "live-b" {
		t.Fatalf("first failover = %s,%s want open,live-b", first[1].Route.ProviderID, first[2].Route.ProviderID)
	}
	if second[0].Route.ProviderID != "live-b" {
		t.Fatalf("second = %s, want live-b", second[0].Route.ProviderID)
	}
}

func TestOrderProvidersForRequestWithMetaKeepsDifferentRouteMarkupsApart(t *testing.T) {
	requestProviderWRR.Reset()
	model := &AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{"plain", "marked-up"},
		ProviderCreditMultipliers: map[string]float64{
			"plain":     1,
			"marked-up": 2,
		},
	}
	metas := map[string]llmpool.ProviderDispatchMeta{
		"plain":     {ID: "plain", Sequence: 1, MaxConcurrency: 10, Billing: llmpool.ProviderBillingPolicy{CreditMultiplier: 1}},
		"marked-up": {ID: "marked-up", Sequence: 2, MaxConcurrency: 10, Billing: llmpool.ProviderBillingPolicy{CreditMultiplier: 1}},
	}
	got := OrderProvidersForRequestWithMeta(nil, model, metas, time.Time{})
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Route.ProviderID != "plain" || got[1].Route.ProviderID != "marked-up" {
		t.Fatalf("order = %s,%s want cheap route first", got[0].Route.ProviderID, got[1].Route.ProviderID)
	}
	if got[0].BandKey == got[1].BandKey {
		t.Fatalf("band keys = %s,%s want vendor x route groups kept apart", got[0].BandKey, got[1].BandKey)
	}
}

func TestOrderProvidersForRequestWithMetaInPoolDoesNotResetDefaultPool(t *testing.T) {
	requestProviderWRR.Reset()
	model := &AuthorizedModel{Name: "auto", ProviderIDs: []string{"chat-a", "resp", "chat-b"}}
	all := map[string]llmpool.ProviderDispatchMeta{
		"chat-a": {ID: "chat-a", Sequence: 1, MaxConcurrency: 10},
		"resp":   {ID: "resp", Sequence: 2, MaxConcurrency: 10},
		"chat-b": {ID: "chat-b", Sequence: 3, MaxConcurrency: 10},
	}
	stream := map[string]llmpool.ProviderDispatchMeta{
		"chat-a": {ID: "chat-a", Sequence: 1, MaxConcurrency: 10},
		"resp":   {ID: "resp", Sequence: 2, MaxConcurrency: 10, SkipWRR: true},
		"chat-b": {ID: "chat-b", Sequence: 3, MaxConcurrency: 10},
	}
	if got := OrderProvidersForRequestWithMeta(nil, model, all, time.Time{}); len(got) < 1 || got[0].Route.ProviderID != "chat-a" {
		t.Fatalf("default first = %#v, want chat-a", got)
	}
	if got := OrderProvidersForRequestWithMetaInPool(nil, model, stream, time.Time{}, "auto\x1estream"); len(got) < 1 || got[0].Route.ProviderID != "chat-a" {
		t.Fatalf("stream first = %#v, want chat-a", got)
	}
	if got := OrderProvidersForRequestWithMeta(nil, model, all, time.Time{}); len(got) < 1 || got[0].Route.ProviderID != "resp" {
		t.Fatalf("default second = %s, want resp (stream pool must not reset default WRR)", got[0].Route.ProviderID)
	}
	if got := OrderProvidersForRequestWithMetaInPool(nil, model, stream, time.Time{}, "auto\x1estream"); len(got) < 1 || got[0].Route.ProviderID != "chat-b" {
		t.Fatalf("stream second = %s, want chat-b", got[0].Route.ProviderID)
	}
}
