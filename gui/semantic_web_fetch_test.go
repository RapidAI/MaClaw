package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
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
	// The turn budget is five fetches: repeat siblings re-execute under the
	// same stable name, and the sixth call finds every grant consumed.
	for i := 2; i <= 5; i++ {
		if again := cb.ExecuteTool(name, `{"url":"https://example.com/page"}`); !strings.Contains(again, "Fetched web evidence.") {
			t.Fatalf("repeat fetch %d must re-execute through a sibling grant: %q", i, again)
		}
	}
	if exhausted := cb.ExecuteTool(name, `{"url":"https://example.com/page"}`); strings.Contains(exhausted, "Fetched web evidence.") {
		t.Fatalf("sixth fetch must be denied, budget exhausted: %q", exhausted)
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

// The fetch layer answers a binary URL with its own advisory containing
// "save_path" ("[二进制内容 … 如需下载请使用 save_path 参数]"). A literal
// tool-name scan rejected that host-made guidance as
// trusted_web_fetch_legacy_name and burned the fetch grant (production
// 2026-08-26). The projection must pass tool-name mentions through: the
// router, not a content scan, is the capability boundary.
func TestSemanticWebFetchProjectionPassesToolNameMentions(t *testing.T) {
	advisory := "[二进制内容: image/jpeg, 12345 字节。如需下载请使用 save_path 参数]"
	out, err := semanticTrustedWebFetchResultProjection(advisory)
	if err != nil || out != advisory {
		t.Fatalf("binary advisory rejected: out=%q err=%v", out, err)
	}
	page := "This tutorial compares web_fetch and download_file for archiving."
	if out, err = semanticTrustedWebFetchResultProjection(page); err != nil || out != page {
		t.Fatalf("page mentioning tools rejected: out=%q err=%v", out, err)
	}
	if _, err = semanticTrustedWebFetchResultProjection("[file_base64|image/png]AAAA"); err == nil {
		t.Fatal("delivery token must still be rejected")
	}
}

// A binary fetch must be projected into guidance that is actionable on THIS
// surface: the legacy advisory ("请使用 save_path 参数") is uncallable here
// (the schema is closed over url) and never names download_file, so the
// production model concluded images were undownloadable while download_file
// sat in the petition whitelist (2026-08-26 ragdoll birthday deck).
func TestSemanticWebFetchProjectionBinaryAdvisoryNamesDownloadFile(t *testing.T) {
	out := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		URL:         "https://images.example.com/cat.jpeg",
		ContentType: "image/jpeg",
		BytesRead:   94926,
		Content:     "[二进制内容: image/jpeg, 94926 字节。如需下载请使用 save_path 参数]",
	})
	if !strings.Contains(out, "download_file") || !strings.Contains(out, `"url"`) {
		t.Fatalf("binary advisory must name the closed download_file path: %q", out)
	}
	if strings.Contains(out, "请使用 save_path 参数") || strings.Contains(out, `"save_path"`) {
		t.Fatalf("uncallable save_path advisory must be replaced: %q", out)
	}
	if strings.Contains(out, "python-pptx") || strings.Contains(out, "bash") {
		t.Fatalf("binary advisory must not send the model to bash/python-pptx: %q", out)
	}
	if !strings.Contains(out, "slides[].images") || !strings.Contains(out, "slides[].charts") {
		t.Fatalf("advisory must name office image and chart fields: %q", out)
	}
	if !strings.Contains(out, "https://images.example.com/cat.jpeg") {
		t.Fatalf("advisory must carry the source URL: %q", out)
	}
}

func TestSemanticTrustedWebFetchArgsRejectPlaceholderHost(t *testing.T) {
	if _, err := semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.invalid/skip"}); err == nil || !strings.Contains(err.Error(), "url_host_rejected") {
		t.Fatal(err)
	}
	if _, err := semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "example.invalid/skip"}); err == nil || !strings.Contains(err.Error(), "url_host_rejected") {
		t.Fatal(err)
	}
	got, err := semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.com/page"})
	if err != nil || got != "https://example.com/page" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
