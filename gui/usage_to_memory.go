package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// UsagePatternBridge periodically extracts usage patterns from UsageTracker
// and writes them to the Memory Store as project_knowledge entries.
type UsagePatternBridge struct {
	tracker *tool.UsageTracker
	memory  *memory.Store
	stopCh  chan struct{}
	once    sync.Once
}

// NewUsagePatternBridge creates a new bridge between UsageTracker and Memory Store.
func NewUsagePatternBridge(tracker *tool.UsageTracker, mem *memory.Store) *UsagePatternBridge {
	return &UsagePatternBridge{
		tracker: tracker,
		memory:  mem,
		stopCh:  make(chan struct{}),
	}
}

// Start begins the 24-hour periodic extraction loop.
func (b *UsagePatternBridge) Start() {
	go b.loop()
}

// Stop halts the periodic loop.
func (b *UsagePatternBridge) Stop() {
	b.once.Do(func() {
		close(b.stopCh)
	})
}

func (b *UsagePatternBridge) loop() {
	// Run once on startup after a short delay.
	select {
	case <-b.stopCh:
		return
	case <-time.After(30 * time.Second):
	}
	b.RunOnce()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.RunOnce()
		}
	}
}

// RunOnce executes a single extraction + write cycle.
func (b *UsagePatternBridge) RunOnce() {
	if b.tracker == nil || b.memory == nil {
		log.Printf("[usage-to-memory] tracker or memory store not available, skipping")
		return
	}

	patterns := b.tracker.ExtractPatterns(7)
	if len(patterns) == 0 {
		return
	}

	written := 0
	updated := 0
	for _, p := range patterns {
		// Search for existing usage_pattern entry for this tool using tag-based filtering.
		// List all project_knowledge entries and filter by tags manually for precision.
		allEntries := b.memory.List(memory.CategoryProjectKnowledge, "")
		found := false
		for _, e := range allEntries {
			if !hasTag(e.Tags, "usage_pattern") || !hasTag(e.Tags, p.ToolName) {
				continue
			}
			// Found existing entry for this tool.
			if strings.TrimSpace(e.Content) == strings.TrimSpace(p.Description) {
				// Content identical — just touch access count.
				b.memory.TouchAccess([]string{e.ID})
			} else {
				// Content changed — update.
				tags := mergeTags(e.Tags, []string{"usage_pattern", p.ToolName})
				_ = b.memory.Update(e.ID, p.Description, memory.CategoryProjectKnowledge, tags)
				updated++
			}
			found = true
			break
		}
		if !found {
			entry := memory.Entry{
				Content:  p.Description,
				Category: memory.CategoryProjectKnowledge,
				Tags:     []string{"usage_pattern", p.ToolName},
			}
			_ = b.memory.Save(entry)
			written++
		}
	}

	if written > 0 || updated > 0 {
		log.Printf("[usage-to-memory] extracted %d patterns: %d new, %d updated",
			len(patterns), written, updated)
	}
}

func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

func mergeTags(existing, additional []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, t := range existing {
		seen[t] = true
	}
	result := make([]string, len(existing))
	copy(result, existing)
	for _, t := range additional {
		if !seen[t] {
			result = append(result, t)
			seen[t] = true
		}
	}
	return result
}
