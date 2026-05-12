package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// knowledgeStoreManager manages the process-level knowledge store instance.
// Shared by all HTTP handlers and the agent executor.
type knowledgeStoreManager struct {
	store    *knowledge.SQLiteStore
	embedder embedding.Embedder
	mu       sync.RWMutex
	dbPath   string
	closed   bool
}

// newKnowledgeStoreManager initializes the knowledge store and embedding model.
// The store is created at $MACLAW_DATA_ROOT/knowledge/knowledge.db.
// The embedding model is loaded from $MACLAW_DATA_ROOT/models/ or a custom path.
// If the model is unavailable, vector search degrades to pure FTS (no error).
func newKnowledgeStoreManager(dataRoot string) (*knowledgeStoreManager, error) {
	dbPath := filepath.Join(dataRoot, "knowledge", "knowledge.db")
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, err
	}

	var emb embedding.Embedder
	if isEmbeddingDisabled() {
		emb = embedding.NewNoopEmbedder()
		log.Printf("[knowledge] embedding explicitly disabled via MACLAW_EMBEDDING_DISABLED, using FTS-only mode")
	} else {
		modelPath := resolveEmbeddingModelPath(dataRoot)
		emb = embedding.NewDefaultEmbedder(modelPath)
		if embedding.IsNoop(emb) {
			log.Printf("[knowledge] embedding model not available at %s, using FTS-only mode", modelPath)
		} else {
			log.Printf("[knowledge] embedding model loaded from %s", modelPath)
		}
	}
	store.SetEmbedder(emb)

	return &knowledgeStoreManager{
		store:    store,
		embedder: emb,
		dbPath:   dbPath,
	}, nil
}

// Store returns the shared knowledge SQLiteStore.
func (m *knowledgeStoreManager) Store() *knowledge.SQLiteStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

// Close releases the knowledge store and embedding model resources.
func (m *knowledgeStoreManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	if m.store != nil {
		_ = m.store.Close()
	}
	if m.embedder != nil {
		m.embedder.Close()
	}
	log.Printf("[knowledge] store closed")
}

// resolveEmbeddingModelPath determines the embedding model file path.
// Priority: MACLAW_EMBEDDING_MODEL_PATH env > $MACLAW_DATA_ROOT/models/ > ~/.maclaw/models/
func resolveEmbeddingModelPath(dataRoot string) string {
	if custom := strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_MODEL_PATH")); custom != "" {
		return custom
	}
	// Check in data root first
	candidate := filepath.Join(dataRoot, "models", embedding.DefaultModelFilename)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Fall back to default path (~/.maclaw/models/)
	return embedding.DefaultModelPath()
}

// isEmbeddingDisabled checks if embedding is explicitly disabled via env.
func isEmbeddingDisabled() bool {
	v := strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_DISABLED"))
	return v == "1" || v == "true" || v == "TRUE" || v == "yes"
}
