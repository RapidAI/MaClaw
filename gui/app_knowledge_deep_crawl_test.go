package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestKnowledgeDeepCrawlDoesNotCancelActiveCrawl(t *testing.T) {
	cancelled := false
	app := &App{
		deepCrawlCancel: func() { cancelled = true },
		deepCrawlMode:   "crawl",
	}

	_, err := app.KnowledgeDeepCrawl(knowledge.DeepCrawlRequest{SeedURL: "https://example.com/docs", MaxDepth: 1})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected active crawl error, got %v", err)
	}
	if cancelled {
		t.Fatal("second crawl must not cancel an active crawl")
	}
	if app.deepCrawlMode != "crawl" || app.deepCrawlCancel == nil {
		t.Fatalf("active crawl ownership changed unexpectedly: mode=%q cancel_nil=%v", app.deepCrawlMode, app.deepCrawlCancel == nil)
	}
}

func TestKnowledgeDeepCrawlPreviewDoesNotCancelActiveCrawl(t *testing.T) {
	cancelled := false
	app := &App{
		deepCrawlCancel: func() { cancelled = true },
		deepCrawlMode:   "crawl",
	}

	_, err := app.KnowledgeDeepCrawlPreview(knowledge.DeepCrawlRequest{SeedURL: "https://example.com/docs", MaxDepth: 1})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected active crawl error, got %v", err)
	}
	if cancelled {
		t.Fatal("preview must not cancel an active crawl")
	}
	if app.deepCrawlMode != "crawl" || app.deepCrawlCancel == nil {
		t.Fatalf("active crawl ownership changed unexpectedly: mode=%q cancel_nil=%v", app.deepCrawlMode, app.deepCrawlCancel == nil)
	}
}

func TestKnowledgeDeepCrawlCancelClearsActiveOwnership(t *testing.T) {
	cancelled := false
	app := &App{
		deepCrawlCancel: func() { cancelled = true },
		deepCrawlMode:   "preview",
	}

	if err := app.KnowledgeDeepCrawlCancel(); err != nil {
		t.Fatalf("KnowledgeDeepCrawlCancel: %v", err)
	}
	if !cancelled {
		t.Fatal("expected active cancel func to be called")
	}
	if app.deepCrawlCancel != nil || app.deepCrawlCtx != nil || app.deepCrawlMode != "" {
		t.Fatalf("cancel should clear ownership: mode=%q cancel_nil=%v ctx_nil=%v", app.deepCrawlMode, app.deepCrawlCancel == nil, app.deepCrawlCtx == nil)
	}
}
