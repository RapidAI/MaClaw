package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func TestProjectHostWebFetchNamesAClippedBody(t *testing.T) {
	done := &websearch.FetchResult{
		URL:             "https://example.com/a",
		ContentType:     "text/plain",
		Content:         "done",
		TotalChars:      4,
		NextOffset:      4,
		ResponseClipped: true,
	}
	out := projectHostWebFetch(done, 0)
	if !strings.Contains(out, websearch.ResponseClippedNote) || strings.Contains(out, "Continuation") {
		t.Fatalf("last page: %s", out)
	}
	if !done.ResponseClipped || done.Content != "done" {
		t.Fatal("clipped page was rewritten")
	}
	cont := &websearch.FetchResult{
		URL:             "https://example.com/a",
		ContentType:     "text/plain",
		Content:         "part",
		TotalChars:      10,
		Truncated:       true,
		HasMore:         true,
		NextOffset:      4,
		ResponseClipped: true,
	}
	out = projectHostWebFetch(cont, 0)
	if strings.Contains(out, websearch.ResponseClippedNote) || !strings.Contains(out, "Pass offset=4") {
		t.Fatalf("continued page: %s", out)
	}
}

func TestProjectHostWebFetchNamesTheRequestedURL(t *testing.T) {
	result := &websearch.FetchResult{
		URL:         "https://example.com/final",
		ContentType: "text/plain",
		Content:     "part",
		TotalChars:  10,
		Truncated:   true,
		HasMore:     true,
		NextOffset:  4,
	}
	out := projectHostWebFetchRequested(result, 0, "https://example.com/start")
	if !strings.Contains(out, "Pass offset=4") || !strings.Contains(out, "\nurl=https://example.com/start\n") {
		t.Fatalf("hint: %s", out)
	}
	if strings.Contains(out, "url=https://example.com/start to") {
		t.Fatal("url is inside the continuation sentence")
	}
	same := projectHostWebFetchRequested(result, 0, "https://example.com/final")
	plain := projectHostWebFetch(result, 0)
	if same != plain || strings.Contains(plain, "url=") {
		t.Fatalf("matching url changed the page")
	}
}

func TestProjectHostWebFetchKeepsShortPageFormat(t *testing.T) {
	result := &websearch.FetchResult{
		URL:         "https://example.com/a",
		ContentType: "text/plain",
		BytesRead:   10,
		Content:     "CDEF",
		TotalChars:  10,
		Truncated:   true,
		HasMore:     true,
		NextOffset:  6,
	}
	out := projectHostWebFetch(result, 2)
	if !strings.Contains(out, "Read: 2-6 / 10 chars") {
		t.Fatalf("range: %s", out)
	}
	if !strings.Contains(out, "truncated: true | has_more: true | next_offset: 6") || !strings.Contains(out, "Pass offset=6") {
		t.Fatalf("cursor: %s", out)
	}
	if result.Content != "CDEF" || result.NextOffset != 6 {
		t.Fatal("short page was rewritten")
	}
}

func TestProjectHostWebFetchClampsWideRunes(t *testing.T) {
	body := strings.Repeat("中", 16384)
	result := &websearch.FetchResult{
		URL:         "https://example.com/catalog",
		Title:       "Catalog",
		ContentType: "text/plain",
		BytesRead:   len(body),
		Content:     body,
		TotalChars:  16384,
		NextOffset:  16384,
	}
	out := projectHostWebFetch(result, 0)
	if len(out) > agent.WebFetchMaxToolResult {
		t.Fatalf("len=%d", len(out))
	}
	if result.NextOffset != 16384 || result.Content != body || result.HasMore {
		t.Fatal("caller result was mutated")
	}
	visible, next := hostWebFetchVisible(t, out)
	if next <= 0 || next >= 16384 {
		t.Fatalf("next=%d", next)
	}
	if visible != string([]rune(body)[:next]) {
		t.Fatalf("visible runes=%d next=%d", len([]rune(visible)), next)
	}
}

func hostWebFetchVisible(t *testing.T, page string) (string, int) {
	t.Helper()
	const lead = "\n\n--- Continuation ---\n"
	i := strings.LastIndex(page, lead)
	if i < 0 {
		t.Fatalf("missing continuation: %s", page[max(0, len(page)-180):])
	}
	footer := page[i+len(lead):]
	body := page[:i]
	if j := strings.Index(body, "\n\n"); j >= 0 {
		body = body[j+2:]
	}
	const key = "Pass offset="
	k := strings.LastIndex(footer, key)
	if k < 0 {
		t.Fatalf("footer=%q", footer)
	}
	n := 0
	for _, r := range footer[k+len(key):] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return body, n
}
