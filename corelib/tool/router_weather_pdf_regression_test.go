package tool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Regression tests for the semantic tool-routing miss observed in production
// (~/.maclaw/logs/tool_route.log, 2026-08-24): for the message
// "北京天气，输出 格式化pdf报告" the router ranked git_status #1 and exposed
// no tool capable of fetching weather or producing a PDF, so the task could
// not continue.
//
// Two root causes are pinned here:
//  1. Embedding texts included BodySummary (long templated parameter docs),
//     collapsing the vector space (median pairwise cosine ~0.8) and turning
//     cosine ranking into noise. buildEmbeddingText is now name+description.
//  2. All conditional tools failed closed when the classifier stayed silent.
//     Benign tools (web_search, generate_pdf, office) are now score-eligible;
//     sensitive ones (ssh, browser, screenshot) stay fail-closed.

// weatherPDFRouteTools builds a candidate set resembling the production mix:
// everyday core tools, the task-relevant tools, and the noise tools that used
// to crowd them out.
func weatherPDFRouteTools() []map[string]interface{} {
	defs := []struct{ name, desc string }{
		// bootstrap / fallback core
		{"task", "Manage background tasks"},
		{"async_wait", "Wait for async operations"},
		{"compress_context", "Compress conversation context"},
		{"bash", "Run shell commands"},
		{"read_file", "Read a file from disk"},
		{"edit_file", "Edit an existing file"},
		{"discover_tool", "Discover deferred tools"},
		// task-relevant tools
		{"web_fetch", "Fetch content from a web page URL"},
		{"web_search", "Search the web for current information"},
		{"generate_pdf", "Generate a formatted PDF document from markdown or HTML"},
		// noise tools
		{"git_status", "Show the working tree status and recent changes"},
		{"knowledge_search", "Search the local knowledge base"},
		{"computer_click", "Click on the screen at coordinates"},
		{"asr", "Speech to text transcription"},
		// sensitive conditional tools that must stay fail-closed
		{"ssh", "Execute commands on a remote server over SSH"},
		{"browser", "Automate a browser session"},
		{"screenshot", "Capture the screen"},
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		out = append(out, makeToolDef(d.name, d.desc))
	}
	return out
}

func routedNameOrder(result []map[string]interface{}) map[string]int {
	order := make(map[string]int, len(result))
	for i, t := range result {
		order[ExtractToolName(t)] = i
	}
	return order
}

// TestRouter_WeatherPDFTaskKeepsNeededTools is the no-model regression test:
// with the classifier skipped, benign conditional tools must compete on score
// while sensitive conditional tools stay hidden. Descriptions carry Chinese
// keywords like the production enrichment store does, since pure BM25 cannot
// bridge a Chinese query to English descriptions.
func TestRouter_WeatherPDFTaskKeepsNeededTools(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	tools := weatherPDFRouteTools()
	// Replace the two benign conditional tools with Chinese-enriched
	// descriptions so the BM25-only path (NoopEmbedder) can match them.
	for i, td := range tools {
		switch ExtractToolName(td) {
		case "web_search":
			tools[i] = makeToolDef("web_search", "网络搜索 查询天气、新闻、实时信息")
		case "generate_pdf":
			tools[i] = makeToolDef("generate_pdf", "生成格式化 PDF 报告文档")
		}
	}

	result := router.RouteWithOptions("北京天气，输出 格式化pdf报告", tools, RouteOptions{SkipUnifiedClassifier: true})
	names := routedToolNames(result)

	for _, want := range []string{"web_search", "generate_pdf"} {
		if !names[want] {
			t.Fatalf("task-needed tool %q missing from routed surface: %#v", want, names)
		}
	}
	for _, forbidden := range []string{"ssh", "browser", "screenshot"} {
		if names[forbidden] {
			t.Fatalf("sensitive conditional tool %q must stay fail-closed: %#v", forbidden, names)
		}
	}
}

// TestRouter_WeatherPDFFusedRanking is the model-gated regression test for the
// embedding channel: with the real embedding model, the fused ranking must
// place the task-relevant tools ahead of the former noise winner git_status.
func TestRouter_WeatherPDFFusedRanking(t *testing.T) {
	modelPath := os.Getenv("GEMMA_EMB_MODEL")
	if modelPath == "" {
		home, _ := os.UserHomeDir()
		modelPath = filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skip("no gemma embedding model found")
	}
	emb, err := embedding.NewGemmaEmbedder(modelPath, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatalf("load embedder: %v", err)
	}
	defer emb.Close()

	router := NewRouter(NewDefinitionGenerator(nil, nil))
	router.SetEmbedder(emb)
	tools := weatherPDFRouteTools()

	result := router.RouteWithOptions("北京天气，输出 格式化pdf报告", tools, RouteOptions{SkipUnifiedClassifier: true})
	names := routedToolNames(result)
	order := routedNameOrder(result)

	for _, want := range []string{"generate_pdf", "web_fetch", "web_search"} {
		if !names[want] {
			t.Fatalf("task-needed tool %q missing from routed surface: %#v", want, names)
		}
	}
	// git_status used to top the fused ranking for this query. The robust
	// regression signal is that it must not lead the candidate ranking and must
	// trail generate_pdf by a wide margin. (web_fetch/web_search beat it by a
	// much smaller cosine margin that can vary with model quantization, so no
	// pairwise order is asserted for them.)
	if names["git_status"] {
		if order["git_status"] < order["generate_pdf"] {
			t.Fatalf("git_status ranked #%d ahead of generate_pdf (#%d); embedding channel still noise-dominated",
				order["git_status"], order["generate_pdf"])
		}
	}
}
