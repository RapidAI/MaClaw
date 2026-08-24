package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func webFetchClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelWebFetch,
		Confidence: .98,
		ToolNames:  []string{"web_fetch", "web_search", "download_file"},
	}
}

func TestIMSemanticWebFetchUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		t.Fatalf("planning must not execute fetch user=%q url=%q", userID, url)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这个链接的内容", "lansenger", "root-fetch", "turn-fetch", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedWebFetchAdapter || selection.FitProof.MatchedCapability != tool.CapabilityInformationFetchWeb {
		t.Fatalf("selection=%+v", selection)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only web fetch must not require a receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	if name != "web_fetch" || definition["name"] != "web_fetch" {
		t.Fatalf("managed web fetch name=%q, want web_fetch", name)
	}
	if selection.AdapterName == "web_fetch" || selection.AdapterName == "web_search" || selection.AdapterName == "download_file" {
		t.Fatalf("managed web fetch leaked registry adapter %q", selection.AdapterName)
	}
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["url"]; !ok || len(properties) != 1 {
		t.Fatalf("web fetch schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"save_path", "output", "dest", "path", "filename", "offset", "max_chars",
		"render_js", "headers", "cookie", "use_browser_cookies", "via_browser",
		"timeout", "query", "channel", "destination", "group_name",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing web fetch schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedWebFetchAdapter, `{"url":"https://example.com"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"url":"https://example.com","save_path":"out.html"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged save_path=%q", got)
	}
}

func TestIMSemanticWebFetchExecutesURLWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	var seenUser, seenURL string
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		seenUser, seenURL = userID, url
		return "Fetched web evidence.\nTitle: Example\nURL: https://example.com", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "fetch the content of this URL", "lansenger", "root-fetch-exec", "turn-fetch-exec", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"url":"https://example.com/page"}`)
	if !strings.Contains(got, "Fetched web evidence.") || strings.Contains(got, "web_fetch") || strings.Contains(got, "save_path") {
		t.Fatalf("bound fetch=%q", got)
	}
	if seenUser != "user-1" || seenURL != "https://example.com/page" {
		t.Fatalf("dispatch user=%q url=%q", seenUser, seenURL)
	}
	if replay := cb.ExecuteTool(name, `{"url":"https://example.com/page"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticWebFetchRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	h.semanticTrustedWebFetch = func(string, string) (string, error) {
		return "[file_base64|text/html]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这个链接的内容", "lansenger", "root-fetch-token", "turn-fetch-token", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"url":"https://example.com","channel":"lansenger"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "trusted_web_fetch_arguments_rejected") {
		t.Fatalf("extra field=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这个链接的内容", "lansenger", "root-fetch-token-2", "turn-fetch-token-2", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"url":"https://example.com"}`); !strings.Contains(got, "trusted_web_fetch_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.fetchTrustedWeb("", "https://example.com", false); err == nil || !strings.Contains(err.Error(), "trusted_web_fetch_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.fetchTrustedWeb("user-1", "", false); err == nil || !strings.Contains(err.Error(), "trusted_web_fetch_url_required") {
		t.Fatalf("empty url err=%v", err)
	}
}

func TestIMSemanticWebFetchReadsURLWithoutSaving(t *testing.T) {
	body := `<html><head><title>Example</title></head><body><p>hello trusted fetch</p></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	out, err := h.fetchTrustedWeb("user-1", server.URL, false)
	if err != nil || !strings.Contains(out, "Fetched web evidence.") || !strings.Contains(out, "hello trusted fetch") {
		t.Fatalf("fetch=%q err=%v", out, err)
	}
	if strings.Contains(out, "web_fetch") || strings.Contains(out, "save_path") || strings.Contains(out, "download_file") || strings.Contains(out, "[file_base64") {
		t.Fatalf("fetch leaked legacy/delivery names: %q", out)
	}
	if _, err := h.fetchTrustedWeb("user-1", "http://127.0.0.1/", true); err == nil {
		t.Fatal("public-network fetch must reject loopback")
	}
}
