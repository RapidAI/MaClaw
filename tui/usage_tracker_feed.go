package main

// usage_tracker_feed.go wires TUI agent loops into the shared tool usage
// tracker, closing the routing quality loop: real tool-execution outcomes
// from corelib/agent.RunLoop are recorded into the same
// <MaclawBaseDir>/data/tool_usage.json that the desktop/IM routers consume
// for experience-aware scoring. Hosts embed usageTrackerFeed to implement
// agent.UsageTrackerProvider; when no tracker can be opened the feed is a
// no-op and the loop is otherwise unaffected.

import (
	"log"
	"sync"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

var (
	tuiUsageTrackerOnce sync.Once
	tuiUsageTracker     *coretool.UsageTracker
)

// defaultTUIUsageTracker lazily opens (or creates) the shared usage tracker
// at the product default path. A nil result means the feed stays disabled.
func defaultTUIUsageTracker() *coretool.UsageTracker {
	tuiUsageTrackerOnce.Do(func() {
		path := coretool.DefaultUsageTrackerPath()
		if path == "" {
			return
		}
		tracker, err := coretool.NewUsageTracker(path)
		if err != nil {
			log.Printf("[tui] usage tracker unavailable, outcome feed disabled: %v", err)
			return
		}
		tuiUsageTracker = tracker
	})
	return tuiUsageTracker
}

// usageTrackerFeed is embedded into TUI LoopCallbacks implementations to
// satisfy agent.UsageTrackerProvider with zero other behavior.
type usageTrackerFeed struct{}

func (usageTrackerFeed) UsageTracker() *coretool.UsageTracker {
	return defaultTUIUsageTracker()
}
