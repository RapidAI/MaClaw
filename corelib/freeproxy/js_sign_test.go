package freeproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAnalyzeV2Signing analyzes the v2 signing mechanism from the JS bundle.
func TestAnalyzeV2Signing(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// Find module 72660 which has the WASM signing
	idx := strings.Index(content, "72660:(e,t,n)=>")
	if idx < 0 {
		t.Fatal("Module 72660 not found")
	}

	// Extract ~6000 chars of the module
	end := idx + 6000
	if end > len(content) {
		end = len(content)
	}
	module := content[idx:end]

	// Find "var j" or "j=" or "function j"
	t.Log("=== Looking for j (Ay export) in module 72660 ===")

	// The module structure: exports {Ay:()=>j, No:()=>v}
	// v is defined as: function v(e,t) { ... r.get_sign(body, len, url, len) }
	// j must be the WASM init function

	// Let's find j by looking for the pattern after the w function
	wIdx := strings.Index(module, "function w(")
	if wIdx > 0 {
		afterW := module[wIdx:]
		// j is likely defined after w
		jIdx := strings.Index(afterW, "function j(")
		if jIdx < 0 {
			jIdx = strings.Index(afterW, "var j=")
		}
		if jIdx < 0 {
			jIdx = strings.Index(afterW, "j=function")
		}
		if jIdx >= 0 {
			end := jIdx + 500
			if end > len(afterW) {
				end = len(afterW)
			}
			t.Logf("j found at offset %d from w:\n%s", jIdx, afterW[jIdx:end])
		}
	}

	// Now let's understand the interceptor more carefully
	// The v1 signing uses: MD5(timestamp + body + nonce)
	// The v2 signing uses: WASM get_sign(body, url)
	// Both set headers: timestamp, nonce, sign, token, appType, lang, client-ver, appVersion

	// Let's find the v1 signing code to understand the nonce generation
	t.Log("\n=== V1 signing analysis ===")
	akIdx := strings.Index(content, "67297")
	if akIdx > 0 {
		// Find the Ak export
		searchFrom := akIdx
		for i := 0; i < 5; i++ {
			nextIdx := strings.Index(content[searchFrom:], "Ak")
			if nextIdx >= 0 {
				pos := searchFrom + nextIdx
				start := pos - 100
				if start < 0 {
					start = 0
				}
				end := pos + 200
				if end > len(content) {
					end = len(content)
				}
				t.Logf("Ak at %d: ...%s...", pos, content[start:end])
				searchFrom = pos + 1
			}
		}
	}

	// Let's also check if v1 endpoints (like create, getUserInfo) work
	// because they use simpler MD5 signing
	t.Log("\n=== Key finding ===")
	t.Log("The v2/chat endpoint requires WASM-based request signing.")
	t.Log("Without correct sign/timestamp/nonce headers, the server silently drops the request.")
	t.Log("v1 endpoints use MD5(timestamp + body + nonce) which we can implement.")
	t.Log("But v2 endpoints use a WASM binary for signing which is much harder to replicate.")

	// Check if there's a v1 chat endpoint
	t.Log("\n=== Checking for v1 chat endpoint ===")
	if strings.Contains(content, "v1/chat") {
		idx := strings.Index(content, "v1/chat")
		start := idx - 100
		if start < 0 {
			start = 0
		}
		end := idx + 200
		if end > len(content) {
			end = len(content)
		}
		t.Logf("v1/chat found: %s", content[start:end])
	} else {
		t.Log("No v1/chat endpoint found")
	}

	// Check for alternative chat endpoints
	for _, ep := range []string{"chatApi/v1", "chatApi/v3", "agentChat", "agentApi/v1/agentChat"} {
		if strings.Contains(content, ep) {
			idx := strings.Index(content, ep)
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + 150
			if end > len(content) {
				end = len(content)
			}
			t.Logf("Found %s: %s", ep, content[start:end])
		}
	}
}
