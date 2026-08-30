package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func webSearchClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: .98,
		ToolNames:  []string{"web_search", "web_fetch", "download_file"},
	}
}

func TestIMSemanticWebSearchUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		t.Fatalf("planning must not execute search user=%q query=%q", userID, query)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "搜索 Go 并发文档", "lansenger", "root-search", "turn-search", webSearchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, semanticTrustedWebSearchCapability)
	if !ok || selection.AdapterName != semanticTrustedWebSearchAdapter {
		t.Fatalf("selection=%+v found=%v", selection, ok)
	}
	if selection.FitProof.QualifierBindings["freshness"] != "reference" {
		t.Fatalf("search freshness=%#v", selection.FitProof.QualifierBindings)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only web search must not require a receipt: %+v", selection.Effects)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	def := semanticDefForGrantName(defs, name)
	if name != "web_search" || def == nil {
		t.Fatalf("managed web search name=%q, want web_search", name)
	}
	definition := def["function"].(map[string]interface{})
	if definition["name"] != "web_search" {
		t.Fatalf("managed web search def name=%q, want web_search", definition["name"])
	}
	if selection.AdapterName == "web_search" || selection.AdapterName == "web_fetch" || selection.AdapterName == "download_file" {
		t.Fatalf("managed web search leaked registry adapter %q", selection.AdapterName)
	}
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["query"]; !ok || len(properties) != 1 {
		t.Fatalf("web search schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"max_results", "provider", "engine", "save_path", "url", "channel",
		"destination", "group_name", "freshness", "offset",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing web search schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedWebSearchAdapter, `{"query":"go"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"query":"go","max_results":20}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged max_results=%q", got)
	}
}

func TestIMSemanticWebSearchExecutesQueryWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	var seenUser, seenQuery string
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		seenUser, seenQuery = userID, query
		return "Public web results for \"channels\" (1):\n\n1. Go Channels\n   https://example.com/channels", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "find Go concurrency documentation", "lansenger", "root-search-exec", "turn-search-exec", webSearchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"query":"channels"}`)
	if !strings.Contains(got, "Public web results") || strings.Contains(got, "web_search") || strings.Contains(got, "max_results") {
		t.Fatalf("bound search=%q", got)
	}
	if seenUser != "user-1" || seenQuery != "channels" {
		t.Fatalf("dispatch user=%q query=%q", seenUser, seenQuery)
	}
	// The turn budget is five searches: repeat siblings re-execute under the
	// same stable name, and the sixth call finds every grant consumed.
	for i := 2; i <= 5; i++ {
		if again := cb.ExecuteTool(name, `{"query":"channels"}`); !strings.Contains(again, "Public web results") {
			t.Fatalf("repeat search %d must re-execute through a sibling grant: %q", i, again)
		}
	}
	if exhausted := cb.ExecuteTool(name, `{"query":"channels"}`); strings.Contains(exhausted, "Public web results") {
		t.Fatalf("sixth search must be denied, budget exhausted: %q", exhausted)
	}
}

func TestIMSemanticWebSearchRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	h.semanticTrustedWebSearch = func(string, string) (string, error) {
		return "[file_base64|text/plain]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "搜索 Go 并发文档", "lansenger", "root-search-token", "turn-search-token", webSearchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"query":"go","channel":"lansenger"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "trusted_web_search_arguments_rejected") {
		t.Fatalf("extra field=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "搜索 Go 并发文档", "lansenger", "root-search-token-2", "turn-search-token-2", webSearchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"query":"go"}`); !strings.Contains(got, "trusted_web_search_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.searchTrustedWeb("", "go", false); err == nil || !strings.Contains(err.Error(), "trusted_web_search_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.searchTrustedWeb("user-1", "", false); err == nil || !strings.Contains(err.Error(), "trusted_web_search_query_required") {
		t.Fatalf("empty query err=%v", err)
	}
}

func TestIMSemanticWebSearchProjectsResultsWithoutLegacyNames(t *testing.T) {
	out := semanticTrustedWebSearchProjection("golang", websearch.SearchResponse{
		Results: []websearch.SearchResult{{Title: "Go", URL: "https://go.dev", Snippet: "The Go language"}},
	})
	if !strings.Contains(out, "Public web results") || !strings.Contains(out, "https://go.dev") || !strings.Contains(out, "The Go language") {
		t.Fatalf("projection=%q", out)
	}
	if strings.Contains(out, "web_search") || strings.Contains(out, "web_fetch") || strings.Contains(out, "[file_base64") {
		t.Fatalf("projection leaked legacy/delivery names: %q", out)
	}
}

func TestIMSemanticLiveDataUsesHostSearchCurrentFreshness(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "今天北京天气", "lansenger", "root-live-search", "turn-live-search",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98, ToolNames: []string{"web_search"}},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, "information.search.web")
	if !ok || selection.AdapterName != semanticTrustedWebSearchAdapter || selection.FitProof.QualifierBindings["freshness"] != "current" {
		t.Fatalf("live data selection=%+v found=%v", selection, ok)
	}
}

// A denial after a consumed grant must acknowledge the earlier success. The
// generic policy denial reads as "the capability is unavailable this turn"
// and made the production model discard 8 valid search results (2026-08-26).
func TestIMSemanticWebSearchConsumedGrantDenialAcknowledgesSuccess(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "results", nil }
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "find Go concurrency documentation", "lansenger", "root-search-deny", "turn-search-deny", webSearchClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	for i := 0; i < 5; i++ {
		if got := cb.ExecuteTool("web_search", `{"query":"go"}`); !strings.Contains(got, "results") {
			t.Fatalf("search %d=%q", i+1, got)
		}
	}
	allowed, reason := cb.IsToolCallAllowed("web_search", `{"query":"go"}`)
	if allowed {
		t.Fatal("exhausted search budget must deny the sixth call")
	}
	if !strings.Contains(reason, "already ran successfully") || !strings.Contains(reason, "still stands") {
		t.Fatalf("consumed-grant denial must acknowledge the earlier success: %q", reason)
	}
}
