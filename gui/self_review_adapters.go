package main

import (
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ---------------------------------------------------------------------------
// Adapters for agent.SelfReviewLoop dependencies
// ---------------------------------------------------------------------------

// sessionStatsAdapter implements agent.SessionStatsProvider.
// Uses a simple counter that is incremented by the agent loop on completion.
type sessionStatsAdapter struct {
	mu         sync.Mutex
	timestamps []time.Time // completion timestamps for accurate since-based queries
}

func newSessionStatsAdapter() *sessionStatsAdapter {
	return &sessionStatsAdapter{}
}

func (a *sessionStatsAdapter) RecordCompletion() {
	a.mu.Lock()
	a.timestamps = append(a.timestamps, time.Now())
	// Keep only last 200 timestamps to bound memory.
	if len(a.timestamps) > 200 {
		a.timestamps = a.timestamps[len(a.timestamps)-200:]
	}
	a.mu.Unlock()
}

func (a *sessionStatsAdapter) CompletedSessionsSince(since time.Time) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, t := range a.timestamps {
		if !t.Before(since) {
			count++
		}
	}
	return count
}

// toolStatsAdapter implements agent.ToolStatsProvider using the ToolRegistry's
// built-in usage counters.
type toolStatsAdapter struct {
	app *App
}

func (a *toolStatsAdapter) TopToolsByUsage(n int) []agent.ToolUsageStat {
	if a.app.skillExecutor == nil {
		return nil
	}
	// No direct tool usage stats available in the registry; return empty.
	// Future: wire into ToolRegistry.UsageStats() when available.
	return nil
}

// skillHealthAdapter implements agent.SkillHealthProvider by reading
// UsageCount/SuccessCount from SkillExecutor.loadSkills().
type skillHealthAdapter struct {
	app *App
}

func (a *skillHealthAdapter) UnhealthySkills(minUsage int, maxSuccessRate float64) []agent.SkillHealthStat {
	if a.app.skillExecutor == nil {
		return nil
	}
	skills := a.app.skillExecutor.loadSkills()
	var result []agent.SkillHealthStat
	for _, s := range skills {
		if s.UsageCount < minUsage {
			continue
		}
		rate := 0.0
		if s.UsageCount > 0 {
			rate = float64(s.SuccessCount) / float64(s.UsageCount)
		}
		if rate >= maxSuccessRate {
			continue
		}
		result = append(result, agent.SkillHealthStat{
			Name:        s.Name,
			UsageCount:  s.UsageCount,
			SuccessRate: rate,
			LastError:   s.LastError,
		})
	}
	return result
}

// memorySaverAdapter implements agent.MemorySaver by writing to the
// App's MemoryStore.
type memorySaverAdapter struct {
	app *App
}

func (a *memorySaverAdapter) SaveInsight(content string, category string, tags []string) error {
	a.app.ensureMemoryStore()
	if a.app.memoryStore == nil {
		return nil // graceful degradation
	}
	entry := MemoryEntry{
		Content:  content,
		Category: MemoryCategory(category),
		Tags:     tags,
	}
	return a.app.memoryStore.Save(entry)
}
