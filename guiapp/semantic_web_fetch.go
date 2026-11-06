package guiapp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	semanticTrustedWebFetchAdapter        = "semantic_fetch_trusted_web"
	semanticTrustedWebFetchImplementation = "trusted-web-fetch-v1"
	semanticTrustedWebFetchMaxChars       = 16384
	semanticTrustedWebFetchMaxBytes       = 2 * 1024 * 1024
	// A later offset of the same page reuses this extract instead of
	// downloading the body again. The slot is the principal, the URL, and
	// the network policy, so one user does not read another's page.
	// The lifetime starts at the fetch. A later offset does not extend it.
	// Ten minutes covers a serial read of a long catalog (the 164594-byte
	// CCF gist is about eleven pages) without keeping the snapshot for the
	// whole process.
	trustedWebCacheTTL = 10 * time.Minute
	trustedWebCacheCap = 8
)

// trustedWebCacheNow is the clock for the extract cache. Tests move it.
var trustedWebCacheNow = time.Now

func semanticUnpublishedLegacyWebFetchProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityInformationFetchWeb {
			return true
		}
	}
	return false
}

func semanticTrustedWebFetchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedWebFetchAdapter,
			"description": "Fetch one approved HTTP URL and extract page text. url is required. offset continues a truncated page; the page length is fixed.",
			"parameters":  semanticTrustedWebFetchInvocationSchema(),
		},
	}
}

func semanticTrustedWebFetchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
			// offset is the continuation cursor the fetch layer already
			// computes (NextOffset). It is not the legacy max_chars knob:
			// the page length stays host-fixed. The catalog renderer
			// replaces the function description, so this property
			// description is what the model reads. Do not put the page
			// length in it.
			// Production 2026-11-06: a 164594-byte CCF catalog was cut at
			// 16384 characters and the result said only "...(truncated)".
			// The kept prefix was still category 0. The model could not
			// ask for the rest of the same URL.
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "0-based character offset into the extracted text. Omit on the first read. When the result says Next: offset=N, call again with that offset. If that line includes url=, pass that url; otherwise repeat the url from this call. The page length is fixed.",
			},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func semanticTrustedWebFetchArgsAllowed(args map[string]interface{}) (string, int, error) {
	if len(args) == 0 || len(args) > 2 {
		return "", 0, fmt.Errorf("trusted_web_fetch_arguments_rejected")
	}
	var rawURL string
	var offset int
	var sawURL bool
	for key, raw := range args {
		switch key {
		case "url":
			value, ok := raw.(string)
			if !ok {
				return "", 0, fmt.Errorf("trusted_web_fetch_arguments_rejected")
			}
			rawURL = strings.TrimSpace(value)
			sawURL = true
		case "offset":
			parsed, ok := tool.NonNegativeSchemaInteger(raw)
			if !ok {
				return "", 0, fmt.Errorf("trusted_web_fetch_arguments_rejected")
			}
			offset = parsed
		default:
			return "", 0, fmt.Errorf("trusted_web_fetch_arguments_rejected")
		}
	}
	if !sawURL || rawURL == "" {
		return "", 0, fmt.Errorf("trusted_web_fetch_url_required")
	}
	if websearch.IsPlaceholderFetchHost(websearch.FetchURLHostname(rawURL)) {
		return "", 0, fmt.Errorf("trusted_web_fetch_url_host_rejected")
	}
	return rawURL, offset, nil
}

func (h *IMMessageHandler) fetchTrustedWeb(principalID, rawURL string, offset int, publicNetworkOnly bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_web_fetch_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_web_fetch_principal_required")
	}
	rawURL = websearch.TrimFetchURL(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("trusted_web_fetch_url_required")
	}
	if offset < 0 {
		return "", fmt.Errorf("trusted_web_fetch_arguments_rejected")
	}
	// The test hook replaces the whole fetch, including paging. Production
	// reads one host-sized window at offset.
	if h.semanticTrustedWebFetch != nil {
		return h.semanticTrustedWebFetch(principalID, rawURL)
	}
	full, err := h.trustedWebExtractedPage(principalID, rawURL, publicNetworkOnly)
	if err != nil {
		return "", err
	}
	page, err := trustedWebWindow(full, offset)
	if err != nil {
		return "", err
	}
	// rawURL is the cache key. A redirect stores the final address on
	// page.URL; the next call has to repeat rawURL to reuse that extract.
	return semanticTrustedWebFetchFittedProjection(page, rawURL), nil
}

func (h *IMMessageHandler) trustedWebExtractedPage(principalID, rawURL string, publicNetworkOnly bool) (*websearch.FetchResult, error) {
	if page, ok := trustedWebCacheGet(principalID, rawURL, publicNetworkOnly); ok {
		return page, nil
	}
	// Overlapping misses of the same principal, URL, and network policy
	// share one download. The cache lock is not held across that download.
	// A failure is not cached, so the next call tries again.
	key := trustedWebCacheKey(principalID, rawURL, publicNetworkOnly)
	flight, leader := trustedWebInflightJoin(key)
	if !leader {
		<-flight.done
		if flight.err != nil {
			return nil, flight.err
		}
		if flight.page == nil {
			return nil, fmt.Errorf("trusted_web_fetch_empty")
		}
		page := *flight.page
		return &page, nil
	}
	defer func() {
		close(flight.done)
		trustedWebInflightFinish(key, flight)
	}()
	if page, ok := trustedWebCacheGet(principalID, rawURL, publicNetworkOnly); ok {
		flight.page = page
		return page, nil
	}
	opts := &websearch.FetchOptions{
		// MaxChars 0 keeps the extracted text. The caller slices a page and
		// never returns this full text to the model.
		MaxChars:          0,
		MaxBytes:          semanticTrustedWebFetchMaxBytes,
		TimeoutS:          corelib.NormalizeAgentTimeoutSec(corelib.DefaultAgentTimeoutSec),
		PublicNetworkOnly: publicNetworkOnly,
	}
	if !publicNetworkOnly {
		if err := websearch.ApplyHubDownload(opts, h.getWebSearchStrategy()); err != nil {
			flight.err = err
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.TimeoutS)*time.Second)
	defer cancel()
	result, err := websearch.FetchCtx(ctx, rawURL, opts)
	if err != nil {
		flight.err = err
		return nil, err
	}
	if result == nil {
		err = fmt.Errorf("trusted_web_fetch_empty")
		flight.err = err
		return nil, err
	}
	trustedWebCachePut(principalID, rawURL, publicNetworkOnly, result)
	flight.page = result
	return result, nil
}

// trustedWebInflight is one download shared by overlapping cache misses.
// The leader publishes page or err, then closes done. Closing happens
// after the cache put, so a waiter either joins this download or reads
// the snapshot.
type trustedWebInflight struct {
	done chan struct{}
	page *websearch.FetchResult
	err  error
}

var trustedWebInflightState = struct {
	mu sync.Mutex
	m  map[string]*trustedWebInflight
}{
	m: map[string]*trustedWebInflight{},
}

func trustedWebInflightJoin(key string) (flight *trustedWebInflight, leader bool) {
	trustedWebInflightState.mu.Lock()
	defer trustedWebInflightState.mu.Unlock()
	if existing, ok := trustedWebInflightState.m[key]; ok {
		return existing, false
	}
	flight = &trustedWebInflight{done: make(chan struct{})}
	trustedWebInflightState.m[key] = flight
	return flight, true
}

func trustedWebInflightFinish(key string, flight *trustedWebInflight) {
	trustedWebInflightState.mu.Lock()
	defer trustedWebInflightState.mu.Unlock()
	if current, ok := trustedWebInflightState.m[key]; ok && current == flight {
		delete(trustedWebInflightState.m, key)
	}
}

type trustedWebCacheEntry struct {
	key  string
	at   time.Time
	page websearch.FetchResult
}

var trustedWebCacheState = struct {
	mu      sync.Mutex
	entries []trustedWebCacheEntry
}{}

func trustedWebCacheKey(principalID, rawURL string, publicNetworkOnly bool) string {
	flag := "0"
	if publicNetworkOnly {
		flag = "1"
	}
	return principalID + "\x00" + rawURL + "\x00" + flag
}

func trustedWebCacheGet(principalID, rawURL string, publicNetworkOnly bool) (*websearch.FetchResult, bool) {
	key := trustedWebCacheKey(principalID, rawURL, publicNetworkOnly)
	trustedWebCacheState.mu.Lock()
	defer trustedWebCacheState.mu.Unlock()
	now := trustedWebCacheNow()
	kept := make([]trustedWebCacheEntry, 0, len(trustedWebCacheState.entries))
	var hit trustedWebCacheEntry
	found := false
	for _, entry := range trustedWebCacheState.entries {
		if now.Sub(entry.at) > trustedWebCacheTTL {
			continue
		}
		if entry.key == key {
			hit = entry
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if found {
		kept = append(kept, hit)
	}
	trustedWebCacheState.entries = kept
	if !found {
		return nil, false
	}
	page := hit.page
	return &page, true
}

func trustedWebCachePut(principalID, rawURL string, publicNetworkOnly bool, page *websearch.FetchResult) {
	if page == nil {
		return
	}
	key := trustedWebCacheKey(principalID, rawURL, publicNetworkOnly)
	stored := *page
	trustedWebCacheState.mu.Lock()
	defer trustedWebCacheState.mu.Unlock()
	now := trustedWebCacheNow()
	kept := make([]trustedWebCacheEntry, 0, len(trustedWebCacheState.entries)+1)
	for _, entry := range trustedWebCacheState.entries {
		if now.Sub(entry.at) > trustedWebCacheTTL || entry.key == key {
			continue
		}
		kept = append(kept, entry)
	}
	kept = append(kept, trustedWebCacheEntry{key: key, at: now, page: stored})
	if len(kept) > trustedWebCacheCap {
		kept = kept[len(kept)-trustedWebCacheCap:]
	}
	trustedWebCacheState.entries = kept
}

func trustedWebWindow(full *websearch.FetchResult, offset int) (*websearch.FetchResult, error) {
	if full == nil {
		return nil, fmt.Errorf("trusted_web_fetch_empty")
	}
	if semanticTrustedWebFetchBinary(full.Content) {
		if offset > 0 {
			return nil, fmt.Errorf("trusted_web_fetch_offset_past_end")
		}
		page := *full
		return &page, nil
	}
	if offset < 0 {
		offset = 0
	}
	total := full.TotalChars
	if total > 0 && offset >= total && offset > 0 {
		return nil, fmt.Errorf("trusted_web_fetch_offset_past_end")
	}
	pageText, shown, end, exhausted := trustedWebPageSlice(full.Content, offset, semanticTrustedWebFetchMaxChars)
	if total <= 0 {
		if exhausted {
			total = end
		} else {
			total = utf8.RuneCountInString(full.Content)
		}
	} else if exhausted && end < total {
		// The cached text ended before the recorded total. Advertising
		// HasMore here would repeat this same offset on an empty slice.
		total = end
	}
	if offset > 0 && (shown == 0 || offset >= total) {
		return nil, fmt.Errorf("trusted_web_fetch_offset_past_end")
	}
	page := *full
	page.Content = pageText
	page.TotalChars = total
	page.NextOffset = end
	more := end < total
	page.Truncated = more
	page.HasMore = more
	return &page, nil
}

// trustedWebPageSlice returns runes [offset, offset+maxChars) without
// allocating a rune for every character of the cached document.
// exhausted is true when the window reaches the end of content.
func trustedWebPageSlice(content string, offset, maxChars int) (page string, shown, end int, exhausted bool) {
	if offset < 0 {
		offset = 0
	}
	if maxChars < 1 {
		maxChars = 1
	}
	i := 0
	for i < len(content) && end < offset {
		_, size := utf8.DecodeRuneInString(content[i:])
		if size < 1 {
			break
		}
		i += size
		end++
	}
	if end < offset {
		return "", 0, end, true
	}
	start := i
	for i < len(content) && shown < maxChars {
		_, size := utf8.DecodeRuneInString(content[i:])
		if size < 1 {
			break
		}
		i += size
		shown++
	}
	return content[start:i], shown, end + shown, i >= len(content)
}

func semanticTrustedWebFetchBinary(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "[二进制")
}

func semanticTrustedWebFetchFittedProjection(result *websearch.FetchResult, requested string) string {
	if result == nil {
		return ""
	}
	if semanticTrustedWebFetchBinary(result.Content) {
		return semanticTrustedWebFetchProjection(result)
	}
	page := *result
	return websearch.FitFetchPage(&page, agent.WebFetchMaxToolResult, func(page *websearch.FetchResult) string {
		return semanticTrustedWebFetchProjectionRequested(page, requested)
	})
}

func semanticTrustedWebFetchProjection(result *websearch.FetchResult) string {
	return semanticTrustedWebFetchProjectionRequested(result, "")
}

func semanticTrustedWebFetchProjectionRequested(result *websearch.FetchResult, requested string) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Fetched web evidence.\n")
	if title := strings.TrimSpace(result.Title); title != "" {
		fmt.Fprintf(&b, "Title: %s\n", title)
	}
	if pageURL := strings.TrimSpace(result.URL); pageURL != "" {
		fmt.Fprintf(&b, "URL: %s\n", pageURL)
	}
	fmt.Fprintf(&b, "Type: %s | Size: %d bytes\n\n", result.ContentType, result.BytesRead)
	if semanticTrustedWebFetchBinary(result.Content) {
		// The fetch layer's binary advisory is written for the legacy surface
		// ("请使用 save_path 参数") — this adapter does not accept save_path,
		// so that advice is literally uncallable here, and it never names
		// the real petitionable capability. Production 2026-08-26 (ragdoll
		// birthday deck): the model fetched three images, could not act on the
		// advisory, concluded "无法直接下载图片" and shipped a text-only PPT
		// while download_file sat unused in the petition whitelist. Replace the
		// legacy advisory with guidance that is actionable on THIS surface.
		fmt.Fprintf(&b, "[binary content: %s, %d bytes — web_fetch extracts text only. To save the file to disk, call download_file with {\"url\": %q}; the destination is host-bound. To place images in a presentation, call office with slides[].images. Native charts go in slides[].charts.]", result.ContentType, result.BytesRead, strings.TrimSpace(result.URL))
		if result.ResponseClipped {
			b.WriteString("\n" + websearch.ResponseClippedNote)
		}
	} else {
		if note := semanticTrustedWebFetchOpenedRange(result); note != "" {
			b.WriteString(note)
			b.WriteByte('\n')
		}
		b.WriteString(result.Content)
		if note := semanticTrustedWebFetchNextOffset(result, requested); note != "" {
			b.WriteString(note)
		} else if result.ResponseClipped {
			b.WriteString("\n" + websearch.ResponseClippedNote)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// semanticTrustedWebFetchWindow reports the inclusive character span of the
// page the fetch layer already sliced. NextOffset is the exclusive end.
func semanticTrustedWebFetchWindow(result *websearch.FetchResult) (start, end int, ok bool) {
	if result == nil || result.TotalChars <= 0 {
		return 0, 0, false
	}
	shown := utf8.RuneCountInString(result.Content)
	if shown == 0 {
		return 0, 0, false
	}
	end = result.NextOffset
	if end < shown {
		end = shown
	}
	start = end - shown
	if start < 0 {
		start = 0
	}
	return start, end, true
}

func semanticTrustedWebFetchOpenedRange(result *websearch.FetchResult) string {
	start, end, ok := semanticTrustedWebFetchWindow(result)
	if !ok || start <= 0 || result.Truncated || result.HasMore {
		return ""
	}
	return fmt.Sprintf("(chars %d-%d of %d)", start, end-1, result.TotalChars)
}

func semanticTrustedWebFetchNextOffset(result *websearch.FetchResult, requested string) string {
	start, end, ok := semanticTrustedWebFetchWindow(result)
	if !ok || (!result.Truncated && !result.HasMore) {
		return ""
	}
	next := result.NextOffset
	if next < end {
		next = end
	}
	// Keep this on one line. The preview treats a one-line Next footer as
	// the trailer, and the offset parser stops at the first non-digit.
	// The url is outside the closing parenthesis, so a copy of the line
	// does not include that parenthesis in the address.
	if continueURL := websearch.ContinueFetchURL(requested, result.URL); continueURL != "" {
		return fmt.Sprintf("\n... (total %d chars, showing %d-%d. Next: offset=%d) url=%s", result.TotalChars, start, end-1, next, continueURL)
	}
	return fmt.Sprintf("\n... (total %d chars, showing %d-%d. Next: offset=%d)", result.TotalChars, start, end-1, next)
}

func semanticTrustedWebFetchResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_web_fetch_delivery_token")
	}
	// No tool-name token scan here. Fetched content cannot grant capabilities —
	// the semantic router renders the tool list and gates every call — while a
	// literal scan rejected legitimate pages and even the fetch layer's own
	// binary advisory ("[二进制内容 … 请使用 save_path 参数下载]") with an opaque
	// trusted_web_fetch_legacy_name, burning the fetch grant on a host-made
	// message (production 2026-08-26, ragdoll birthday deck turn).
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_web_fetch_empty")
	}
	return text, nil
}
