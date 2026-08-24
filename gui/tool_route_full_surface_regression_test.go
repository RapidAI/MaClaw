package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestFullSurfaceWeatherPDFRouting is the production-faithful regression for
// the 2026-08-24 tool-miss incident: against the complete desktop host
// surface (~164 tools, not a reduced fixture), the fused route for a Chinese
// weather+PDF request must expose the tools the task needs. It guards the
// three fixes that resolved the incident:
//  1. embedding width — the 256-dim MRL truncation collapsed CJK
//     discrimination (web_search fell to rank 104/164); the production
//     DefaultEmbeddingDim (768) must be used here as in app_embedding.go.
//  2. web_search's description carries its canonical real-time scenarios
//     (天气/新闻/汇率), the keywords both retrieval channels bridge on.
//  3. legacy adapter catalog completeness — without provisions the closed
//     replacement empties the surface downstream (covered separately by
//     TestLegacyAdapterCatalogCoversFullHostSurface).
func TestFullSurfaceWeatherPDFRouting(t *testing.T) {
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

	app := &App{testHomeDir: t.TempDir()}
	handler := NewIMMessageHandler(app, nil)
	// Mirror the late desktop registration pass in app.go so the routed
	// surface matches production (GUI automation + computer use).
	statusC := make(chan StatusEvent, 32)
	blm := NewBackgroundLoopManager(statusC)
	registerGUIAutomationTools(handler.registry, blm, handler.agentActivity, statusC, app)
	registerComputerUseTools(handler.registry, app)
	registerGroupDiscussionTools(handler.registry, app, handler)
	handler.toolBuilder = NewDynamicToolBuilder(handler.registry)

	router := tool.NewRouter(tool.NewDefinitionGenerator(nil, nil))
	router.SetEmbedder(emb)
	definitions := handler.getTools()
	if len(definitions) < 100 {
		t.Fatalf("audit surface too small to be production-faithful: %d tools", len(definitions))
	}
	result := router.RouteWithOptions("南京天气，输出 格式化pdf报告", definitions, tool.RouteOptions{SkipUnifiedClassifier: true})

	selected := make(map[string]bool, len(result))
	for _, td := range result {
		selected[tool.ExtractToolName(td)] = true
	}
	for _, want := range []string{"web_search", "web_fetch", "generate_pdf"} {
		if !selected[want] {
			t.Fatalf("task-needed tool %q missing from full-surface route (%d tools selected of %d)", want, len(result), len(definitions))
		}
	}
}
