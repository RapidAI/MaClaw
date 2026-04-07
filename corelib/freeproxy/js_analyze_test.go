package freeproxy

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestAnalyzeJSBundle downloads the _app JS bundle and searches for chat API patterns.
func TestAnalyzeJSBundle(t *testing.T) {
	requireIntegrationTest(t)
	urls := []string{
		"https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js",
		"https://ai.dangbei.com/_next/static/chunks/6527.9f92f59eeb60fcbc.js",
	}

	patterns := []string{
		"chatApi", "v2/chat", "v1/chat", "/chat",
		"botCode", "AI_SEARCH",
		"dangbei.net", "ai-api", "ai-search",
		"modelCode", "modelId", "model_code",
		"conversationId",
		"question",
		"stream",
		"completion",
	}

	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			t.Logf("Failed to fetch %s: %v", url, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		content := string(body)
		t.Logf("=== %s (len=%d) ===", url, len(content))

		for _, p := range patterns {
			re := regexp.MustCompile(regexp.QuoteMeta(p))
			locs := re.FindAllStringIndex(content, -1)
			if len(locs) > 0 {
				t.Logf("  Pattern %q: %d matches", p, len(locs))
				for i, loc := range locs {
					if i >= 3 {
						break
					}
					start := loc[0] - 120
					if start < 0 {
						start = 0
					}
					end := loc[1] + 120
					if end > len(content) {
						end = len(content)
					}
					ctx := content[start:end]
					// Replace newlines for readability
					ctx = strings.ReplaceAll(ctx, "\n", " ")
					t.Logf("    [%d] ...%s...", i, ctx)
				}
			}
		}
	}
}

// TestAnalyzeAllChunks searches ALL JS chunks for the chat API endpoint.
func TestAnalyzeAllChunks(t *testing.T) {
	requireIntegrationTest(t)
	chunks := []string{
		"4062-35991c397ba9845f.js",
		"6603-a3cd13bd6e4e6652.js",
		"7586-31963b10538dd6ba.js",
		"8856-c10b609ebf8048fc.js",
		"4979-adf286ee00e1f343.js",
		"6609-a74d03d0a83f8e45.js",
		"2437-b4518419850b0a9e.js",
		"9974-ddab145ae3871df6.js",
		"2216-57cd310cb3cf2441.js",
		"2806-e5e80b5c63bc655d.js",
		"8100-eaaee8a2289aea87.js",
		"8428-588717c658a18a9d.js",
		"pages/chat-56b75a527ca55cdb.js",
		"pages/_app-3da91045335ded21.js",
		"96f49dd3-b9ec81b4944f95c0.js",
		"6527.9f92f59eeb60fcbc.js",
	}

	// Key patterns that would indicate the chat API call
	keyPatterns := []string{
		"chatApi", "v2/chat", "v1/chat",
		"botCode", "AI_SEARCH",
		"dangbei.net", "ai-api", "ai-search",
		"modelCode", "agentId",
	}

	for _, chunk := range chunks {
		url := "https://ai.dangbei.com/_next/static/chunks/" + chunk
		resp, err := http.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		content := string(body)

		found := false
		for _, p := range keyPatterns {
			if strings.Contains(content, p) {
				if !found {
					t.Logf("=== %s (len=%d) ===", chunk, len(content))
					found = true
				}
				idx := strings.Index(content, p)
				start := idx - 150
				if start < 0 {
					start = 0
				}
				end := idx + len(p) + 150
				if end > len(content) {
					end = len(content)
				}
				t.Logf("  %q: ...%s...", p, content[start:end])
			}
		}
	}

	// Also check if there are dynamically loaded chunks we're missing
	// by looking at the webpack manifest
	t.Log("Checking _buildManifest.js for additional chunks...")
	resp, err := http.Get("https://ai.dangbei.com/_next/static/oGDUZX5UdXWWzB--ZIgJx/_buildManifest.js")
	if err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		content := string(body)
		t.Logf("buildManifest (len=%d):", len(content))

		// Extract chunk filenames
		re := regexp.MustCompile(`"([^"]*\.js)"`)
		matches := re.FindAllStringSubmatch(content, -1)
		seen := make(map[string]bool)
		for _, m := range matches {
			if !seen[m[1]] {
				seen[m[1]] = true
				fmt.Printf("  chunk: %s\n", m[1])
			}
		}
	}
}
