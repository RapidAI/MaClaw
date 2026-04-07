package freeproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestExtractFullGlue extracts the complete module 72660 to understand all host function usage.
func TestExtractFullGlue(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// Find module 72660
	idx := strings.Index(content, "72660:(e,t,n)=>")
	if idx < 0 {
		t.Fatal("Module 72660 not found")
	}

	// Extract a large chunk - the module is likely ~10-15KB
	end := idx + 15000
	if end > len(content) {
		end = len(content)
	}
	chunk := content[idx:end]

	// Find the end of module 72660 (next module starts with a number followed by colon)
	// Modules are separated by },NUMBER:
	modEnd := len(chunk)
	for i := 1000; i < len(chunk)-10; i++ {
		// Look for pattern like "},12345:" which marks next module
		if chunk[i] == '}' && chunk[i+1] == ',' {
			// Check if next chars are digits followed by ':'
			j := i + 2
			for j < len(chunk) && chunk[j] >= '0' && chunk[j] <= '9' {
				j++
			}
			if j > i+2 && j < len(chunk) && chunk[j] == ':' {
				modEnd = i + 1
				break
			}
		}
	}

	module := chunk[:modEnd]
	t.Logf("Module 72660 length: %d chars", len(module))

	// Print in sections
	sectionSize := 2000
	for i := 0; i < len(module); i += sectionSize {
		end := i + sectionSize
		if end > len(module) {
			end = len(module)
		}
		t.Logf("\n=== Section %d-%d ===\n%s", i, end, module[i:end])
	}
}
