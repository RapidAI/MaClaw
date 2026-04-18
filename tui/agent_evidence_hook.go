package main

// Evidence collection hook for the TUI agent loop.
// After each agent turn, launches a goroutine to analyze the user message
// for profile signals. Flushes the batch queue every 10 turns or at session end.
// All analysis runs asynchronously and MUST NOT block the agent loop response.
//
// Requirements: 7.7, 11.2

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/user"
)

// tuiEvidenceTurnCounter tracks turn counts for batch flush scheduling.
var tuiEvidenceTurnCounter struct {
	count atomic.Int64
	mu    sync.Mutex
}

// getEvidenceCollector lazily creates a user.Collector bound to the user model.
// Returns nil if the user model is not available.
func (h *TUIAgentHandler) getEvidenceCollector() *user.Collector {
	// Ensure the user model is initialized first.
	model := h.getUserModel()
	if model == nil {
		return nil
	}

	// Lazily create the evidence collector.
	h.evidenceCollectorMu.Do(func() {
		h.evidenceCollector = user.NewCollector(model)
	})

	return h.evidenceCollector
}

// runEvidenceCollectionTUI launches an async goroutine to analyze the user message
// for profile signals. It also tracks turn count and flushes the batch queue
// every 10 turns. This function MUST NOT block — all work happens in goroutines.
func (h *TUIAgentHandler) runEvidenceCollectionTUI(userMessage string) {
	if userMessage == "" {
		return
	}

	collector := h.getEvidenceCollector()
	if collector == nil {
		return
	}

	// Increment turn counter.
	turns := tuiEvidenceTurnCounter.count.Add(1)

	// Launch async analysis — must not block the agent loop response.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[evidence_hook] panic in Analyze: %v\n", r)
			}
		}()
		collector.Analyze(userMessage)

		// Save the model after analysis to persist any updates.
		if model := h.getUserModel(); model != nil {
			_ = model.Save()
		}
	}()

	// Flush batch every 10 turns.
	if turns%10 == 0 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[evidence_hook] panic in FlushBatch: %v\n", r)
				}
			}()
			// Use a no-op summarize function for now — batch LLM analysis
			// requires an LLM callback that will be wired in a future task.
			_ = collector.FlushBatch(func(text string) (string, error) {
				return "", nil
			})
		}()
	}
}
