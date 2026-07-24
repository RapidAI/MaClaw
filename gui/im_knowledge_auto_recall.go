package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ---------------------------------------------------------------------------
// Knowledge Auto-Recall: automatically search the knowledge base for every
// user message and inject relevant snippets into the system prompt.
//
// Design: no rule-based filtering. FTS score is the only signal.
// See docs/knowledge-auto-recall-design.md for full rationale.
// ---------------------------------------------------------------------------

var (
	knowledgeSourceCountCache int64 // atomic: cached source count
	knowledgeSourceCountTime  int64 // atomic: unix seconds of last check

	// Reusable store for auto-recall to avoid open/close per message.
	knowledgeAutoRecallStore   *knowledge.SQLiteStore
	knowledgeAutoRecallDBPath  string
	knowledgeAutoRecallStoreMu sync.Mutex

	// One-time FTS rebuild flag for gse segmentation migration.
	knowledgeFTSRebuilt   bool
	knowledgeFTSRebuildMu sync.Mutex
)

// appendKnowledgeAutoRecall searches the knowledge base using the user message
// and injects top results into the system prompt if they exceed the score threshold.
func (h *IMMessageHandler) appendKnowledgeAutoRecall(b *strings.Builder, msg string, priorUserMessages []string) {
	if msg == "" {
		return
	}
	minScore := agent.KnowledgeAutoRecallScoreThreshold
	if h != nil && h.app != nil {
		if cfg, err := h.app.LoadConfig(); err == nil {
			if !cfg.IsKnowledgeAutoRecallEnabled() {
				return
			}
			minScore = cfg.EffectiveKnowledgeAutoRecallMinScore()
		}
	}
	if !h.hasKnowledgeSources() {
		return
	}

	// Multi-turn: blend prior user turns; expand also enforces MaxQueryRunes.
	query := agent.ExpandKnowledgeAutoRecallQuery(msg, priorUserMessages)

	store, cleanupStore := h.getAutoRecallStoreForUse()
	defer cleanupStore()
	if store == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	queryStart := time.Now()
	// Search without ProjectPath restriction so personal-scope and project-scope
	// sources are both included. The knowledge base is small enough that cross-scope
	// search is not a performance concern.
	// Search hybrids FTS + embedding when the store has an embedder (attached via
	// openKnowledgeStore / activateEmbedderAsync). PreferEmbedding is left false so
	// embedding runs automatically when FTS is empty or low-confidence.
	results, err := store.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: agent.KnowledgeAutoRecallSearchLimit,
	})
	queryDuration := time.Since(queryStart)
	hasEmbedder := store.HasEmbedder()

	if err != nil {
		log.Printf("[knowledge_auto_recall] search error: %v (took %s)", err, queryDuration)
		return
	}
	if len(results) == 0 {
		log.Printf("[knowledge_auto_recall] no results for query=%d chars embedder=%v (took %s)", len(msg), hasEmbedder, queryDuration)
		// hasKnowledgeSources() pre-check above guarantees the KB is non-empty,
		// but FTS (+ optional embedding hybrid) found zero matches. Hint tools.
		b.WriteString(agent.KnowledgeAutoRecallNoMatchHint)
		return
	}

	// Dynamic threshold + injection count based on top score.
	// Uses shared constants from corelib/agent/prompt_blocks.go (+ optional config min score).
	topScore := results[0].Score
	maxInject := agent.KnowledgeAutoRecallMaxInjectWithMin(topScore, minScore)
	if maxInject == 0 {
		log.Printf("[knowledge_auto_recall] below threshold: topScore=%.2f min=%.2f, results=%d, query=%d chars (took %s)",
			topScore, minScore, len(results), len(msg), queryDuration)
		b.WriteString(agent.KnowledgeAutoRecallNoMatchHint)
		return
	}

	b.WriteString(agent.KnowledgeAutoRecallHeader)

	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < minScore {
			break
		}
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := knowledgeAutoRecallSnippet(r)
		if text == "" {
			continue
		}
		if len([]rune(text)) > agent.KnowledgeAutoRecallSnippetMaxRunes {
			text = string([]rune(text)[:agent.KnowledgeAutoRecallSnippetMaxRunes]) + "..."
		}
		b.WriteString(fmt.Sprintf("- [%s] %s\n", source, text))
		log.Printf("[knowledge_auto_recall] injecting #%d: score=%.2f type=%s source=%q snippet=%d chars",
			injected+1, r.Score, r.ResultType, source, len([]rune(text)))
		injected++
	}

	if injected > 0 {
		log.Printf("[knowledge_auto_recall] done: query=%d chars, injected=%d/%d, topScore=%.2f, took=%s",
			len(msg), injected, len(results), topScore, queryDuration)
	}
}

func (h *IMMessageHandler) getAutoRecallStoreForUse() (*knowledge.SQLiteStore, func()) {
	if h == nil || h.app == nil {
		return nil, func() {}
	}
	return getAutoRecallStoreForAppUse(h.app, true)
}

func getAutoRecallStoreForAppUse(app *App, rebuildFTS bool) (*knowledge.SQLiteStore, func()) {
	if app == nil {
		return nil, func() {}
	}
	if strings.TrimSpace(app.testHomeDir) == "" {
		return getAutoRecallStoreForApp(app, rebuildFTS), func() {}
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		log.Printf("[knowledge_auto_recall] getAutoRecallStoreForUse: open failed: %v", err)
		return nil, func() {}
	}
	return store, func() { _ = store.Close() }
}

// getAutoRecallStore returns a reusable knowledge store for auto-recall.
// The store is kept open across messages to avoid repeated open/close overhead (~5ms each).
// It is lazily created. Invalidation is handled by CloseAutoRecallStore() which is called
// by KnowledgeClearAll and app shutdown.
func (h *IMMessageHandler) getAutoRecallStore() *knowledge.SQLiteStore {
	if h == nil || h.app == nil {
		return nil
	}
	return getAutoRecallStoreForApp(h.app, true)
}

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

// CloseAutoRecallStore closes the cached auto-recall store.
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

// invalidateKnowledgeSourceCountCache forces the next hasKnowledgeSources call
// to re-query the DB. Called after knowledge writes so a fresh import is
// discoverable by auto-recall immediately instead of up to 30s later.
func invalidateKnowledgeSourceCountCache() {
	atomic.StoreInt64(&knowledgeSourceCountTime, 0)
}

// hasKnowledgeSources checks if the knowledge base has any content.
// Uses a 30-second cache to avoid querying the DB on every message.
func (h *IMMessageHandler) hasKnowledgeSources() bool {
	if h.app == nil {
		return false
	}
	now := time.Now().Unix()
	lastCheck := atomic.LoadInt64(&knowledgeSourceCountTime)
	if now-lastCheck < 30 {
		return atomic.LoadInt64(&knowledgeSourceCountCache) > 0
	}
	// Cache miss — lightweight single-query check via the reusable store
	store, cleanupStore := h.getAutoRecallStoreForUse()
	defer cleanupStore()
	if store == nil {
		return false
	}
	stats, err := store.Stats(context.Background())
	if err != nil {
		log.Printf("[knowledge_auto_recall] hasKnowledgeSources: Stats failed: %v", err)
		return false
	}
	atomic.StoreInt64(&knowledgeSourceCountCache, int64(stats.Sources))
	atomic.StoreInt64(&knowledgeSourceCountTime, now)
	log.Printf("[knowledge_auto_recall] hasKnowledgeSources: cache refreshed, sources=%d", stats.Sources)
	return stats.Sources > 0
}

// knowledgeAutoRecallSnippet extracts the best display text from a search result.
// Delegates to the shared knowledge.BestContentText for consistent priority across all platforms.
func knowledgeAutoRecallSnippet(r knowledge.SearchResult) string {
	return knowledge.BestContentText(r)
}
