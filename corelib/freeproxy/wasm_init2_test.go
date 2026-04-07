package freeproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestExtractModule72660 extracts the complete module 72660 code.
func TestExtractModule72660(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// Module 72660 starts at "72660:(e,t,n)=>"
	idx := strings.Index(content, "72660:(e,t,n)=>")
	if idx < 0 {
		t.Fatal("module 72660 not found")
	}

	// Extract a large chunk to get the full module including the init function j
	end := idx + 8000
	if end > len(content) {
		end = len(content)
	}
	t.Logf("=== Module 72660 (from %d, 8000 chars) ===\n%s", idx, content[idx:end])
}
