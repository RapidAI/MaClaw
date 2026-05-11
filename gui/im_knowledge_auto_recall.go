package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

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
)

// appendKnowledgeAutoRecall searches the knowledge base using the user message
// and injects top results into the system prompt if they exceed the score threshold.
func (h *IMMessageHandler) appendKnowledgeAutoRecall(b *strings.Builder, msg string) {
	if msg == "" {
		return
	}
	if !h.hasKnowledgeSources() {
		log.Printf("[knowledge_auto_recall] skipped: no sources in knowledge base")
		return
	}

	store, err := h.app.openKnowledgeStore()
	if err != nil {
		log.Printf("[knowledge_auto_recall] skipped: openKnowledgeStore failed: %v", err)
		return
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	queryStart := time.Now()
	results, err := store.Search(ctx, knowledge.SearchOptions{
		Query:       msg,
		Limit:       5,
		ProjectPath: h.getCurrentProjectPath(),
	})
	queryDuration := time.Since(queryStart)

	if err != nil {
		log.Printf("[knowledge_auto_recall] search error: %v (took %s)", err, queryDuration)
		return
	}
	if len(results) == 0 {
		log.Printf("[knowledge_auto_recall] no results for query=%d chars (took %s)", len(msg), queryDuration)
		return
	}

	// Dynamic threshold + injection count based on top score
	topScore := results[0].Score
	var maxInject int
	switch {
	case topScore >= 3.0:
		maxInject = 3
	case topScore >= 1.0:
		maxInject = 1
	default:
		log.Printf("[knowledge_auto_recall] below threshold: topScore=%.2f, results=%d, query=%d chars (took %s)",
			topScore, len(results), len(msg), queryDuration)
		return // No sufficiently relevant results
	}

	b.WriteString("\n## 知识库参考（自动检索）\n")
	b.WriteString("以下内容来自你的知识库，与当前问题可能相关。请自然引用相关内容；不相关则忽略。\n")
	b.WriteString("如需更多信息，可调用 knowledge_search 或 knowledge_context_pack 深入检索。\n\n")

	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < 1.0 {
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
		if len([]rune(text)) > 200 {
			text = string([]rune(text)[:200]) + "..."
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

// hasKnowledgeSources checks if the knowledge base has any content.
// Uses a 30-second cache to avoid opening the DB on every message.
func (h *IMMessageHandler) hasKnowledgeSources() bool {
	if h.app == nil {
		return false
	}
	now := time.Now().Unix()
	lastCheck := atomic.LoadInt64(&knowledgeSourceCountTime)
	if now-lastCheck < 30 {
		return atomic.LoadInt64(&knowledgeSourceCountCache) > 0
	}
	// Cache miss — use Stats which gives us source count
	store, err := h.app.openKnowledgeStore()
	if err != nil {
		log.Printf("[knowledge_auto_recall] hasKnowledgeSources: openStore failed: %v", err)
		return false
	}
	defer store.Close()
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
func knowledgeAutoRecallSnippet(r knowledge.SearchResult) string {
	if r.Snippet != "" {
		return r.Snippet
	}
	if r.Summary != "" {
		return r.Summary
	}
	if r.Claim != "" {
		return r.Claim
	}
	if r.Subject != "" && r.Predicate != "" {
		return r.Subject + " " + r.Predicate + " " + r.Object
	}
	return ""
}
