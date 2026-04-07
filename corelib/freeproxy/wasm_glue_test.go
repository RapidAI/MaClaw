package freeproxy

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestExtractWasmGlue downloads the JS bundle and extracts the wasm-bindgen glue
// code for get_sign to understand the exact calling convention.
func TestExtractWasmGlue(t *testing.T) {
	requireIntegrationTest(t)
	// The _app bundle contains the WASM glue code in module 72660
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// Find module 72660 which has the WASM signing
	idx := strings.Index(content, "72660:")
	if idx < 0 {
		// Try to find get_sign directly
		idx = strings.Index(content, "get_sign")
		if idx < 0 {
			t.Fatal("get_sign not found in bundle")
		}
	}

	// Extract a large chunk around it
	start := idx - 200
	if start < 0 {
		start = 0
	}
	end := idx + 8000
	if end > len(content) {
		end = len(content)
	}
	chunk := content[start:end]

	// Find the get_sign function definition
	gsIdx := strings.Index(chunk, "get_sign")
	if gsIdx >= 0 {
		gsStart := gsIdx - 200
		if gsStart < 0 {
			gsStart = 0
		}
		gsEnd := gsIdx + 600
		if gsEnd > len(chunk) {
			gsEnd = len(chunk)
		}
		t.Logf("=== get_sign context ===\n%s", chunk[gsStart:gsEnd])
	}

	// Find the function v(e,t) which is the JS wrapper for get_sign
	// Pattern: function v(e,t){...get_sign...}
	re := regexp.MustCompile(`function\s+\w\((\w,\w)\)\s*\{[^}]*get_sign[^}]*\}`)
	match := re.FindString(chunk)
	if match != "" {
		t.Logf("=== get_sign wrapper ===\n%s", match)
	}

	// Also look for the init/load function pattern
	for _, pattern := range []string{"sign_bg", "wasm_bg", ".wasm", "Ay:", "No:"} {
		pIdx := strings.Index(chunk, pattern)
		if pIdx >= 0 {
			pStart := pIdx - 100
			if pStart < 0 {
				pStart = 0
			}
			pEnd := pIdx + 300
			if pEnd > len(chunk) {
				pEnd = len(chunk)
			}
			t.Logf("=== %s context ===\n%s", pattern, chunk[pStart:pEnd])
		}
	}
}
