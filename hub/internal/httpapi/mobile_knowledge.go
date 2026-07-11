package httpapi

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

var (
	mobileKnowledgeOnce  sync.Once
	mobileKnowledgeStore *knowledge.SQLiteStore
	mobileKnowledgeErr   error
	mobileKnowledgeMode  string // "fts" | "vector+fts"
)

// mobileInitKnowledgeStore opens a process-level knowledge DB under the mobile
// agent data root and attaches it to the shared CoreAgentExecutor (same pattern
// as MaClawSrv). Embedding uses Gemma when model file is present, else FTS-only.
func mobileInitKnowledgeStore(dataRoot string, exec *agentservice.CoreAgentExecutor) {
	if exec == nil {
		return
	}
	mobileKnowledgeOnce.Do(func() {
		dbPath := filepath.Join(dataRoot, "knowledge", "knowledge.db")
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			mobileKnowledgeErr = err
			log.Printf("[mobile-core-agent] knowledge mkdir failed: %v", err)
			return
		}
		store, err := knowledge.NewSQLiteStore(dbPath)
		if err != nil {
			mobileKnowledgeErr = err
			log.Printf("[mobile-core-agent] knowledge store open failed: %v", err)
			return
		}
		emb, mode := mobileKnowledgeEmbedder()
		store.SetEmbedder(emb)
		mobileKnowledgeStore = store
		mobileKnowledgeMode = mode
		log.Printf("[mobile-core-agent] knowledge store ready path=%s mode=%s", dbPath, mode)
	})
	if mobileKnowledgeStore != nil {
		exec.SetKnowledgeStore(mobileKnowledgeStore)
	}
}

func mobileKnowledgeModeMessage() (mode, message string) {
	if mobileKnowledgeStore == nil {
		return "unavailable", "knowledge store is not initialized"
	}
	mode = mobileKnowledgeMode
	if mode == "" {
		mode = "fts"
	}
	if mode == "vector+fts" {
		return mode, "knowledge store ready (vector + FTS)"
	}
	return "fts", "knowledge store ready (FTS-only embedding)"
}

func mobileKnowledgeEmbedder() (embedding.Embedder, string) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_DISABLED")), "true") ||
		strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_DISABLED")) == "1" {
		return embedding.NewNoopEmbedder(), "fts"
	}
	modelPath := strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_MODEL_PATH"))
	if modelPath == "" {
		modelPath = embedding.DefaultModelPath()
	}
	emb := embedding.NewDefaultEmbedder(modelPath)
	if embedding.IsNoop(emb) {
		return emb, "fts"
	}
	return emb, "vector+fts"
}
