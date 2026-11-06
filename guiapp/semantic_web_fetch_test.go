package guiapp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	if _, ok := properties["url"]; !ok || len(properties) != 2 {
		t.Fatalf("web fetch schema=%#v", properties)
	}
	offsetField, _ := properties["offset"].(map[string]interface{})
	offsetDesc, _ := offsetField["description"].(string)
	if offsetField["type"] != "integer" || !strings.Contains(offsetDesc, "Next: offset=") {
		t.Fatalf("offset cursor=%#v", offsetField)
	}
	for _, forbidden := range []string{
		"save_path", "output", "dest", "path", "filename", "max_chars",
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
	// The published wave is five fetches. information.fetch.web stays
	// continuable after that wave is spent, so the next call of the same
	// task still runs. A truncated page's Next: offset= is that next call.
	// A count of calls is not the stop.
	for i := 2; i <= 6; i++ {
		if again := cb.ExecuteTool(name, `{"url":"https://example.com/page"}`); !strings.Contains(again, "Fetched web evidence.") {
			t.Fatalf("repeat fetch %d must re-execute through a sibling grant: %q", i, again)
		}
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
	if _, err := h.fetchTrustedWeb("", "https://example.com", 0, false); err == nil || !strings.Contains(err.Error(), "trusted_web_fetch_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
	if _, err := h.fetchTrustedWeb("user-1", "", 0, false); err == nil || !strings.Contains(err.Error(), "trusted_web_fetch_url_required") {
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
	out, err := h.fetchTrustedWeb("user-1", server.URL, 0, false)
	if err != nil || !strings.Contains(out, "Fetched web evidence.") || !strings.Contains(out, "hello trusted fetch") {
		t.Fatalf("fetch=%q err=%v", out, err)
	}
	if strings.Contains(out, "web_fetch") || strings.Contains(out, "save_path") || strings.Contains(out, "download_file") || strings.Contains(out, "[file_base64") {
		t.Fatalf("fetch leaked legacy/delivery names: %q", out)
	}
	if _, err := h.fetchTrustedWeb("user-1", "http://127.0.0.1/", 0, true); err == nil {
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
// (the schema does not accept save_path) and never names download_file, so the
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
	if _, _, err := semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.invalid/skip"}); err == nil || !strings.Contains(err.Error(), "url_host_rejected") {
		t.Fatal(err)
	}
	if _, _, err := semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "example.invalid/skip"}); err == nil || !strings.Contains(err.Error(), "url_host_rejected") {
		t.Fatal(err)
	}
	got, offset, err := semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.com/page"})
	if err != nil || got != "https://example.com/page" || offset != 0 {
		t.Fatalf("got=%q offset=%d err=%v", got, offset, err)
	}
	got, offset, err = semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.com/page", "offset": float64(16384)})
	if err != nil || got != "https://example.com/page" || offset != 16384 {
		t.Fatalf("paged got=%q offset=%d err=%v", got, offset, err)
	}
	if _, _, err = semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.com/page", "offset": float64(-1)}); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, _, err = semanticTrustedWebFetchArgsAllowed(map[string]interface{}{"url": "https://example.com/page", "max_chars": float64(100)}); err == nil {
		t.Fatal("max_chars accepted")
	}
}

func TestSemanticWebFetchProjectionNamesNextOffset(t *testing.T) {
	page := strings.Repeat("a", semanticTrustedWebFetchMaxChars)
	out := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		URL:         "https://example.com/catalog",
		ContentType: "text/plain",
		BytesRead:   semanticTrustedWebFetchMaxChars + len("TAIL"),
		Content:     page,
		Truncated:   true,
		HasMore:     true,
		TotalChars:  semanticTrustedWebFetchMaxChars + len("TAIL"),
		NextOffset:  semanticTrustedWebFetchMaxChars,
	})
	if strings.Contains(out, "TAIL") {
		t.Fatal("first page leaked the unread tail")
	}
	want := fmt.Sprintf("Next: offset=%d", semanticTrustedWebFetchMaxChars)
	if !strings.Contains(out, want) || strings.HasSuffix(strings.TrimSpace(out), "...(truncated)") {
		t.Fatalf("truncated page missing continuation cursor: %s", out[len(out)-180:])
	}

	tail := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		URL:         "https://example.com/catalog",
		ContentType: "text/plain",
		Content:     "TAIL",
		TotalChars:  semanticTrustedWebFetchMaxChars + len("TAIL"),
		NextOffset:  semanticTrustedWebFetchMaxChars + len("TAIL"),
	})
	if strings.Contains(tail, "Next:") || !strings.Contains(tail, "TAIL") {
		t.Fatalf("finished page=%q", tail)
	}
	if !strings.Contains(tail, fmt.Sprintf("(chars %d-%d of %d)", semanticTrustedWebFetchMaxChars, semanticTrustedWebFetchMaxChars+3, semanticTrustedWebFetchMaxChars+4)) {
		t.Fatalf("finished range=%q", tail)
	}

	whole := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		ContentType: "text/plain",
		Content:     "short page",
		TotalChars:  len("short page"),
		NextOffset:  len("short page"),
	})
	if strings.Contains(whole, "Next:") || strings.Contains(whole, "(chars ") {
		t.Fatalf("a page that fits must not advertise a cursor: %q", whole)
	}

	broken := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		ContentType: "text/plain",
		Content:     "abcd",
		TotalChars:  10,
		Truncated:   true,
		HasMore:     true,
		NextOffset:  0,
	})
	if !strings.Contains(broken, "Next: offset=4") {
		t.Fatalf("a zero cursor must name the first unread rune: %q", broken)
	}
}

func TestSemanticWebFetchProjectionNamesAClippedBody(t *testing.T) {
	done := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		ContentType:     "text/plain",
		URL:             "https://example.com/long",
		Content:         "tail",
		TotalChars:      4,
		NextOffset:      4,
		ResponseClipped: true,
	})
	if !strings.Contains(done, websearch.ResponseClippedNote) || strings.Contains(done, "Next:") {
		t.Fatalf("last page: %s", done)
	}
	more := semanticTrustedWebFetchProjection(&websearch.FetchResult{
		ContentType:     "text/plain",
		Content:         "head",
		TotalChars:      100,
		Truncated:       true,
		HasMore:         true,
		NextOffset:      4,
		ResponseClipped: true,
	})
	if strings.Contains(more, websearch.ResponseClippedNote) || !strings.Contains(more, "Next: offset=4") {
		t.Fatalf("continued page: %s", more)
	}
}

func TestTrustedWebWindowKeepsResponseClipped(t *testing.T) {
	page, err := trustedWebWindow(&websearch.FetchResult{
		ContentType:     "text/plain",
		URL:             "https://example.com/long",
		Content:         "ABCDEFGHIJ",
		TotalChars:      10,
		ResponseClipped: true,
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	out := semanticTrustedWebFetchFittedProjection(page, "")
	if !strings.Contains(out, websearch.ResponseClippedNote) || strings.Contains(out, "Next:") {
		t.Fatalf("window: %s", out)
	}
	if !strings.Contains(out, "IJ") || !strings.Contains(out, "(chars 8-9 of 10)") {
		t.Fatalf("range: %s", out)
	}
}

func TestSemanticWebFetchContinueURLStaysOutsideTheParen(t *testing.T) {
	page := &websearch.FetchResult{
		URL:         "https://example.com/final",
		ContentType: "text/plain",
		Content:     "abcd",
		TotalChars:  8,
		Truncated:   true,
		HasMore:     true,
		NextOffset:  4,
	}
	out := semanticTrustedWebFetchProjectionRequested(page, "https://example.com/start")
	if !strings.HasSuffix(out, "Next: offset=4) url=https://example.com/start") {
		t.Fatalf("footer: %s", out)
	}
	same := semanticTrustedWebFetchProjectionRequested(page, "https://example.com/final")
	if strings.Contains(same, "url=") || !strings.HasSuffix(same, "Next: offset=4)") {
		t.Fatalf("same url: %s", same)
	}
}

func TestLegacyWebFetchNamesTheRequestedURL(t *testing.T) {
	result := &websearch.FetchResult{
		URL:         "https://example.com/final",
		ContentType: "text/plain",
		Content:     "part",
		TotalChars:  10,
		Truncated:   true,
		HasMore:     true,
		NextOffset:  4,
	}
	out := formatLegacyWebFetch(result, 0, "https://example.com/start")
	if !strings.Contains(out, "继续读取时请传入 offset=4") || !strings.Contains(out, "\nurl=https://example.com/start\n") {
		t.Fatalf("hint: %s", out)
	}
	same := formatLegacyWebFetch(result, 0, "https://example.com/final")
	plain := formatLegacyWebFetch(result, 0, "")
	if same != plain || strings.Contains(plain, "url=") {
		t.Fatal("matching url changed the legacy page")
	}
}

func TestLegacyWebFetchNamesAClippedBody(t *testing.T) {
	out := formatLegacyWebFetch(&websearch.FetchResult{
		URL:             "https://example.com/a",
		ContentType:     "text/plain",
		Content:         "done",
		TotalChars:      4,
		NextOffset:      4,
		ResponseClipped: true,
	}, 0, "")
	if !strings.Contains(out, legacyWebFetchClippedNote) || strings.Contains(out, "完整性信号") {
		t.Fatalf("last page: %s", out)
	}
}

func TestIMSemanticWebFetchPagesWithOffset(t *testing.T) {
	const tail = "AI-JOURNAL-MARKER"
	body := strings.Repeat("a", semanticTrustedWebFetchMaxChars) + tail
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	head, err := h.fetchTrustedWeb("user-1", server.URL, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(head, tail) {
		t.Fatal("first window included the tail")
	}
	next := fmt.Sprintf("Next: offset=%d", semanticTrustedWebFetchMaxChars)
	if !strings.Contains(head, next) {
		t.Fatalf("head missing %s: %s", next, head[len(head)-200:])
	}
	rest, err := h.fetchTrustedWeb("user-1", server.URL, semanticTrustedWebFetchMaxChars, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rest, tail) || strings.Contains(rest, "Next:") {
		t.Fatalf("continued page=%q", rest)
	}
	if _, err := h.fetchTrustedWeb("user-1", server.URL, semanticTrustedWebFetchMaxChars+len(tail), false); err == nil || !strings.Contains(err.Error(), "trusted_web_fetch_offset_past_end") {
		t.Fatalf("past end err=%v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads=%d", hits.Load())
	}
}

func TestIMSemanticWebFetchRedirectKeepsTheRequestedURL(t *testing.T) {
	body := strings.Repeat("a", semanticTrustedWebFetchMaxChars) + "TAIL"
	var hits atomic.Int32
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	start := server.URL + "/start"
	final := server.URL + "/final"
	h := &IMMessageHandler{}
	head, err := h.fetchTrustedWeb("user-redirect", start, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads=%d", hits.Load())
	}
	if !strings.Contains(head, "URL: "+final) || strings.Contains(head, "TAIL") {
		t.Fatalf("head: %s", head)
	}
	if !strings.Contains(head, ") url="+start) || strings.Contains(head, "url="+start+")") {
		footer := head
		if len(footer) > 240 {
			footer = footer[len(footer)-240:]
		}
		t.Fatalf("footer: %s", footer)
	}
	_, next, more := semanticWebFetchVisible(t, head)
	if !more || next <= 0 {
		t.Fatalf("cursor more=%t next=%d", more, next)
	}
	rest, err := h.fetchTrustedWeb("user-redirect", start, next, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rest, "TAIL") || hits.Load() != 1 {
		t.Fatalf("continued downloads=%d page=%s", hits.Load(), rest)
	}
	again, err := h.fetchTrustedWeb("user-redirect", final, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || strings.Contains(again, "url=") {
		tail := again
		if len(tail) > 240 {
			tail = tail[len(tail)-240:]
		}
		t.Fatalf("final-url downloads=%d page=%s", hits.Load(), tail)
	}
}

func TestIMSemanticWebFetchWideRunesStayInsideToolResult(t *testing.T) {
	const marker = "尾标"
	body := strings.Repeat("中", semanticTrustedWebFetchMaxChars) + marker
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	var got strings.Builder
	offset := 0
	more := true
	for step := 0; step < 12 && more; step++ {
		page, err := h.fetchTrustedWeb("user-wide", server.URL, offset, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > agent.WebFetchMaxToolResult {
			t.Fatalf("page %d len=%d", step, len(page))
		}
		visible, next, continues := semanticWebFetchVisible(t, page)
		got.WriteString(visible)
		if !continues {
			more = false
			break
		}
		if next <= offset {
			t.Fatalf("cursor did not advance: %d -> %d", offset, next)
		}
		offset = next
	}
	if more {
		t.Fatal("pages did not cover the document")
	}
	if got.String() != body {
		t.Fatalf("covered %d runes, body %d", len([]rune(got.String())), len([]rune(body)))
	}
	if hits.Load() != 1 {
		t.Fatalf("downloads=%d", hits.Load())
	}
}

func TestIMSemanticWebFetchCacheIsPrincipalScoped(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello from " + r.URL.Path))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	if _, err := h.fetchTrustedWeb("user-a", server.URL, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.fetchTrustedWeb("user-b", server.URL, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.fetchTrustedWeb("user-a", server.URL, 0, false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("downloads=%d", hits.Load())
	}
}

func TestIMSemanticWebFetchPagesHTMLText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Long Page</title></head><body><main>ABCDEFGHIJ</main></body></html>`))
	}))
	defer server.Close()

	out, err := (&IMMessageHandler{}).fetchTrustedWeb("user-html", server.URL, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Title: Long Page") || !strings.Contains(out, "CDEFGHIJ") {
		t.Fatalf("extracted window: %s", out)
	}
	if strings.Contains(out, "ABCDEFGHIJ") || strings.Contains(out, "<main>") || strings.Contains(out, "Next:") {
		t.Fatalf("html page was not the extracted tail: %s", out)
	}
	if !strings.Contains(out, "(chars 2-9 of 10)") {
		t.Fatalf("range: %s", out)
	}
}

func TestIMSemanticWebFetchOverlappingMissSharesOneDownload(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	stop := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			close(started)
			<-release
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("shared-body"))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(stop)

	h := &IMMessageHandler{}
	errCh := make(chan error, 2)
	fetch := func() {
		out, err := h.fetchTrustedWeb("user-flight", server.URL, 0, false)
		if err != nil {
			errCh <- err
			return
		}
		if !strings.Contains(out, "shared-body") {
			errCh <- fmt.Errorf("missing body: %s", out)
			return
		}
		errCh <- nil
	}
	go fetch()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first download did not start")
	}
	go fetch()
	time.Sleep(200 * time.Millisecond)
	if hits.Load() != 1 {
		t.Fatalf("overlapping miss downloaded %d times", hits.Load())
	}

	otherDone := make(chan error, 1)
	go func() {
		out, err := h.fetchTrustedWeb("user-other", server.URL, 0, false)
		if err != nil {
			otherDone <- err
			return
		}
		if !strings.Contains(out, "shared-body") {
			otherDone <- fmt.Errorf("other principal missing body: %s", out)
			return
		}
		otherDone <- nil
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("different principal waited on the in-flight download")
	}
	if hits.Load() != 2 {
		t.Fatalf("downloads=%d", hits.Load())
	}
	stop()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("downloads=%d", hits.Load())
	}
}

func TestIMSemanticWebFetchErrorIsNotCached(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok-body"))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	if _, err := h.fetchTrustedWeb("user-err", server.URL, 0, false); err == nil {
		t.Fatal("expected an error from the failed download")
	}
	out, err := h.fetchTrustedWeb("user-err", server.URL, 0, false)
	if err != nil || !strings.Contains(out, "ok-body") {
		t.Fatalf("retry err=%v out=%s", err, out)
	}
	if hits.Load() != 2 {
		t.Fatalf("downloads=%d", hits.Load())
	}
}

func TestTrustedWebCacheExpiresFromFetchTime(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	now := start
	trustedWebCacheNow = func() time.Time { return now }
	t.Cleanup(func() { trustedWebCacheNow = time.Now })

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("cached-body"))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	if _, err := h.fetchTrustedWeb("user-expire", server.URL, 0, false); err != nil {
		t.Fatal(err)
	}
	now = start.Add(90 * time.Second)
	if _, err := h.fetchTrustedWeb("user-expire", server.URL, 0, false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("read inside the lifetime downloaded again: %d", hits.Load())
	}
	now = start.Add(trustedWebCacheTTL + time.Second)
	if _, err := h.fetchTrustedWeb("user-expire", server.URL, 0, false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("read after the fetch lifetime reused the snapshot: %d", hits.Load())
	}
}

func TestLegacyWebFetchWideRunesKeepCursor(t *testing.T) {
	body := strings.Repeat("中", 16384)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	out := (&IMMessageHandler{}).toolWebFetch(map[string]interface{}{"url": server.URL})
	if len(out) > agent.WebFetchMaxToolResult {
		t.Fatalf("len=%d", len(out))
	}
	const lead = "\n\n--- 完整性信号 ---\n"
	i := strings.LastIndex(out, lead)
	if i < 0 {
		t.Fatalf("missing cursor: %s", out[max(0, len(out)-200):])
	}
	footer := out[i+len(lead):]
	visible := out[:i]
	if j := strings.Index(visible, "\n\n"); j >= 0 {
		visible = visible[j+2:]
	}
	const key = "继续读取时请传入 offset="
	k := strings.LastIndex(footer, key)
	if k < 0 {
		t.Fatalf("footer=%q", footer)
	}
	next := 0
	for _, r := range footer[k+len(key):] {
		if r < '0' || r > '9' {
			break
		}
		next = next*10 + int(r-'0')
	}
	if next <= 0 || next >= 16384 {
		t.Fatalf("next=%d", next)
	}
	if visible != string([]rune(body)[:next]) {
		t.Fatalf("visible runes=%d next=%d", len([]rune(visible)), next)
	}
}

func semanticWebFetchVisible(t *testing.T, page string) (string, int, bool) {
	t.Helper()
	const footerLead = "\n... (total "
	next := 0
	more := false
	if i := strings.LastIndex(page, footerLead); i >= 0 && strings.Contains(page[i:], "Next: offset=") && strings.Count(page[i:], "\n") == 1 {
		footer := page[i+1:]
		page = page[:i]
		more = true
		const key = "Next: offset="
		j := strings.LastIndex(footer, key)
		if j < 0 {
			t.Fatalf("footer=%q", footer)
		}
		for _, r := range footer[j+len(key):] {
			if r < '0' || r > '9' {
				break
			}
			next = next*10 + int(r-'0')
		}
	}
	if i := strings.Index(page, "\n\n"); i >= 0 {
		page = page[i+2:]
	}
	if strings.HasPrefix(page, "(chars ") {
		if nl := strings.IndexByte(page, '\n'); nl >= 0 {
			page = page[nl+1:]
		} else {
			page = ""
		}
	}
	return page, next, more
}

func TestIMSemanticWebFetchAcceptsOffsetArgument(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		return "Fetched web evidence.\nURL: " + url, nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "fetch the content of this URL", "lansenger", "root-fetch-offset", "turn-fetch-offset", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"url":"https://example.com/page","offset":16384}`)
	if strings.Contains(got, "parameter_unknown_field") || strings.Contains(got, "trusted_web_fetch_arguments_rejected") || !strings.Contains(got, "Fetched web evidence.") {
		t.Fatalf("offset rejected: %q", got)
	}
	got = cb.ExecuteTool(name, `{"url":"https://example.com/page","max_chars":100}`)
	if !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "trusted_web_fetch_arguments_rejected") {
		t.Fatalf("max_chars accepted: %q", got)
	}
}
