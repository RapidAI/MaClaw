package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ---------------------------------------------------------------------------
// Knowledge store cache used by tool retrieval (knowledge_search and coding
// subagents). Prompt-body auto-recall is gone; hosts pull warehouse text
// with tools. See docs/design/clean-working-set-on-demand-retrieval.md.
// ---------------------------------------------------------------------------

var (
	// Reusable store for knowledge tools to avoid open/close per message.
	knowledgeAutoRecallStore   *knowledge.SQLiteStore
	knowledgeAutoRecallDBPath  string
	knowledgeAutoRecallStoreMu sync.Mutex

	// One-time FTS rebuild flag for gse segmentation migration.
	knowledgeFTSRebuilt   bool
	knowledgeFTSRebuildMu sync.Mutex
)

// getAutoRecallStore returns a reusable knowledge store for tool retrieval.
// The store is kept open across messages to avoid repeated open/close overhead (~5ms each).
// It is lazily created. Invalidation is handled by CloseAutoRecallStore() which is called
// by KnowledgeClearAll and app shutdown.
func getAutoRecallStoreForApp(app *App, rebuildFTS bool) *knowledge.SQLiteStore {
	if app == nil {
		return nil
	}
	dbPath := app.knowledgeDBPath()
	knowledgeAutoRecallStoreMu.Lock()
	defer knowledgeAutoRecallStoreMu.Unlock()

	if knowledgeAutoRecallStore != nil {
		if knowledgeAutoRecallDBPath == dbPath {
			return knowledgeAutoRecallStore
		}
		_ = knowledgeAutoRecallStore.Close()
		knowledgeAutoRecallStore = nil
		knowledgeAutoRecallDBPath = ""
		log.Printf("[knowledge_auto_recall] closed cached store for stale db path")
	}

	store, err := app.openKnowledgeStore()
	if err != nil {
		log.Printf("[knowledge_auto_recall] getAutoRecallStore: open failed: %v", err)
		return nil
	}
	// Ensure embedder is attached even if open raced before embedding activation.
	app.attachKnowledgeEmbedder(store)
	knowledgeAutoRecallStore = store
	knowledgeAutoRecallDBPath = dbPath
	// Trigger FTS rebuild in background — does not block the current search.
	// The current search will use LIKE fallback if FTS fails; once rebuild
	// completes, subsequent searches will use the segmented FTS index.
	if rebuildFTS {
		go rebuildAutoRecallFTSInBackground(store)
	}
	return store
}

// rebuildFTSInBackground rebuilds the FTS index with gse segmentation.
// Runs in a background goroutine so it doesn't block user requests.
// Uses a persistent marker to avoid redundant rebuilds across restarts.
func rebuildAutoRecallFTSInBackground(store *knowledge.SQLiteStore) {
	knowledgeFTSRebuildMu.Lock()
	defer knowledgeFTSRebuildMu.Unlock()
	if knowledgeFTSRebuilt {
		return
	}
	knowledgeFTSRebuilt = true
	if store.HasFTSSegmentationMarker() {
		return
	}
	// PrewarmDict ensures gse dictionary loading has started.
	// bm25.Tokenize (called by RebuildFTSIndex) will block until loading completes.
	bm25.PrewarmDict()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(corelib.DefaultAgentTimeoutSec)*time.Second)
	defer cancel()
	if err := store.RebuildFTSIndex(ctx); err != nil {
		log.Printf("[knowledge_auto_recall] FTS rebuild failed: %v", err)
		// Reset flag so next getAutoRecallStore call retries
		knowledgeFTSRebuilt = false
	} else {
		store.SetFTSSegmentationMarker()
		log.Printf("[knowledge_auto_recall] FTS index rebuilt with gse segmentation")
	}
}

// CloseAutoRecallStore closes the cached knowledge store.
// Called on app shutdown or after KnowledgeClearAll.
func CloseAutoRecallStore() {
	knowledgeAutoRecallStoreMu.Lock()
	defer knowledgeAutoRecallStoreMu.Unlock()
	if knowledgeAutoRecallStore != nil {
		knowledgeAutoRecallStore.Close()
		knowledgeAutoRecallStore = nil
		knowledgeAutoRecallDBPath = ""
	}
}
