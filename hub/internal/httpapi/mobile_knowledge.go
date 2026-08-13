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

// mobileKnowledgePurgeState serializes a user purge with Mobile writes that
// could otherwise recreate data after the purge.
// A viewer token can already be in flight when an administrator unbinds its
// user, so deleting sources alone is not sufficient: that request could commit
// a new private source immediately afterwards.  Keeping the tombstone in the
// process also prevents asynchronous document indexing from restoring data
// after the persistent mobile state has been removed.
var mobileKnowledgePurgeState = struct {
	sync.RWMutex
	purged map[string]struct{}
}{
	purged: make(map[string]struct{}),
}

func mobileKnowledgeOwnerKey(tenantID, ownerID string) string {
	return mobileMeetingRecordingTenantID(tenantID) + "\x00" + strings.TrimSpace(ownerID)
}

func mobileKnowledgeOwnerIsPurgedLocked(tenantID, ownerID string) bool {
	_, found := mobileKnowledgePurgeState.purged[mobileKnowledgeOwnerKey(tenantID, ownerID)]
	return found
}

func mobileKnowledgeOwnerIsPurged(tenantID, ownerID string) bool {
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	return mobileKnowledgeOwnerIsPurgedLocked(tenantID, ownerID)
}

// mobileOwnerWriteAllowed is deliberately cheap so every Mobile state writer
// can reject a stale, already-authenticated request after its account is
// unbound. It keeps per-user state from being resurrected before the deleted
// viewer token expires on a concurrent request.
func mobileOwnerWriteAllowed(tenantID, ownerID string) bool {
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	return mobileOwnerWriteAllowedLocked(tenantID, ownerID)
}

// mobileOwnerWriteAllowedLocked is for mutations which must hold the purge
// read lock until after their state-map update. Callers must already hold
// mobileKnowledgePurgeState.RLock.
func mobileOwnerWriteAllowedLocked(tenantID, ownerID string) bool {
	return strings.TrimSpace(ownerID) != "" && !mobileKnowledgeOwnerIsPurgedLocked(tenantID, ownerID)
}

// mobileMarkOwnersPurged must run before starting any durable cleanup. It
// closes the race with a request that authenticated before user tokens were
// revoked but has not yet stored Mobile-owned data.
func mobileMarkOwnersPurged(tenantID string, ownerIDs map[string]struct{}) {
	mobileKnowledgePurgeState.Lock()
	for ownerID := range ownerIDs {
		mobileKnowledgePurgeState.purged[mobileKnowledgeOwnerKey(tenantID, ownerID)] = struct{}{}
	}
	mobileKnowledgePurgeState.Unlock()
}

func mobileResetKnowledgePurgeStateForTest() {
	mobileKnowledgePurgeState.Lock()
	mobileKnowledgePurgeState.purged = make(map[string]struct{})
	mobileKnowledgePurgeState.Unlock()
}

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
