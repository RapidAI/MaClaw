package freeproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestFindWasmInitFunction searches for the _.Ay() init function in module 72660
// to understand what initialization the WASM needs before get_sign works.
func TestFindWasmInitFunction(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// Find module 72660 definition
	idx := strings.Index(content, "72660")
	if idx < 0 {
		t.Fatal("module 72660 not found")
	}

	// Search around module 72660 for its exports
	// Module definitions look like: 72660:(e,t,n)=>{...}
	// or 72660:function(e,t,n){...}
	patterns := []string{
		"72660:", "72660 :",
	}
	for _, p := range patterns {
		i := strings.Index(content, p)
		if i >= 0 {
			end := i + 2000
			if end > len(content) {
				end = len(content)
			}
			t.Logf("=== Module 72660 at %d ===\n%s", i, content[i:end])
		}
	}

	// Also search for where _.Ay and _.No are defined/exported
	// In the interceptor: _ = n(72660)
	// _.Ay() is the init function, _.No() is get_sign
	// Look for the module's export pattern
	searchPatterns := []string{
		"Ay:", "No:", "n.d(t,{",
	}

	// Find all occurrences of n(72660) to see where it's imported
	importIdx := strings.Index(content, "n(72660)")
	if importIdx >= 0 {
		start := importIdx - 200
		if start < 0 {
			start = 0
		}
		end := importIdx + 200
		if end > len(content) {
			end = len(content)
		}
		t.Logf("=== Import of 72660 at %d ===\n%s", importIdx, content[start:end])
	}

	// The module 72660 is likely in a separate chunk. Let's find it.
	// Search for the chunk that contains the WASM loading code
	wasmPatterns := []string{
		"sign_bg", "get_sign", "wasm-bindgen",
		"__wbg_", "__wbindgen_",
	}
	for _, p := range wasmPatterns {
		i := strings.Index(content, p)
		if i >= 0 {
			start := i - 100
			if start < 0 {
				start = 0
			}
			end := i + 200
			if end > len(content) {
				end = len(content)
			}
			t.Logf("=== %q at %d ===\n%s", p, i, content[start:end])
			break // just need one to confirm it's in this file
		}
	}

	// If not in _app, check the chunk that module 72660 is in
	// Module 72660 might be in a separate chunk file
	t.Log("Searching for module 72660 in other chunks...")
	chunks := []string{
		"96f49dd3-b9ec81b4944f95c0.js",
		"6527.9f92f59eeb60fcbc.js",
	}
	for _, chunk := range chunks {
		url := "https://ai.dangbei.com/_next/static/chunks/" + chunk
		resp, err := http.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c := string(body)

		for _, p := range searchPatterns {
			if strings.Contains(c, p) {
				// Find the Ay and No exports
				for _, exp := range []string{"Ay:", "No:", "Ay,", "No,"} {
					idx := strings.Index(c, exp)
					if idx >= 0 {
						start := idx - 100
						if start < 0 {
							start = 0
						}
						end := idx + 200
						if end > len(c) {
							end = len(c)
						}
						t.Logf("=== %s in %s at %d ===\n%s", exp, chunk, idx, c[start:end])
					}
				}
			}
		}

		// Check for WASM-related code
		if strings.Contains(c, "sign_bg") || strings.Contains(c, "__wbg_") {
			t.Logf("=== WASM code found in %s ===", chunk)
			// Find the init/default export
			for _, p := range []string{"default:", "init(", "async function"} {
				idx := strings.Index(c, p)
				if idx >= 0 {
					start := idx - 50
					if start < 0 {
						start = 0
					}
					end := idx + 500
					if end > len(c) {
						end = len(c)
					}
					t.Logf("  %q at %d:\n%s", p, idx, c[start:end])
				}
			}
		}
	}
}
