package main

// Evidence collection hook for the GUI agent loop.
// After each agent turn, launches a goroutine to analyze the user message
// for profile signals. Flushes the batch queue every 10 turns or at session end.
// All analysis runs asynchronously and MUST NOT block the agent loop response.

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/user"
)

// evidenceTurnCounter tracks per-user turn counts for batch flush scheduling.
// Keyed by userID, value is *atomic.Int64.
var evidenceTurnCounters sync.Map

// getEvidenceCollector lazily creates a user.Collector bound to the user model.
// Returns nil if the user model is not available.
func (h *IMMessageHandler) getEvidenceCollector() *user.Collector {
	if h.app == nil {
		return nil
	}

	// Ensure the user model is initialized first.
	h.app.userModelMu.Do(func() {
		modelPath := h.app.userModelPath()
		model, err := user.NewModel(modelPath)
		if err != nil {
			fmt.Printf("[evidence_hook] failed to load user model: %v\n", err)
			return
		}
		h.app.userModel = model
	})

	if h.app.userModel == nil {
		return nil
	}

	// Lazily create the evidence collector (stored on App).
	h.app.evidenceCollectorMu.Do(func() {
		h.app.evidenceCollector = user.NewCollector(h.app.userModel)
	})

	return h.app.evidenceCollector
}

// runEvidenceCollection launches an async goroutine to analyze the user message
// for profile signals. It also tracks turn count and flushes the batch queue
// every 10 turns. This function MUST NOT block — all work happens in goroutines.
func (h *IMMessageHandler) runEvidenceCollection(userID, userMessage string) {
	if userMessage == "" {
		return
	}

	collector := h.getEvidenceCollector()
	if collector == nil {
		return
	}

	// Increment turn counter for this user.
	counterVal, _ := evidenceTurnCounters.LoadOrStore(userID, &atomic.Int64{})
	counter := counterVal.(*atomic.Int64)
	turns := counter.Add(1)

	// Launch async analysis — must not block the agent loop response.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[evidence_hook] panic in Analyze: %v\n", r)
			}
		}()
		collector.Analyze(userMessage)
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
				// TODO: wire to actual LLM summarization callback
				return "", nil
			})
		}()
	}
}

// flushEvidenceOnSessionEnd flushes the evidence batch queue and resets the
// session state. Called on /new, /reset, /exit commands. Runs in a goroutine
// to avoid blocking the command response.
func (h *IMMessageHandler) flushEvidenceOnSessionEnd(userID string) {
	collector := h.getEvidenceCollector()
	if collector == nil {
		return
	}

	// Reset turn counter for this user.
	evidenceTurnCounters.Delete(userID)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[evidence_hook] panic in session-end flush: %v\n", r)
			}
		}()
		_ = collector.FlushBatch(func(text string) (string, error) {
			// TODO: wire to actual LLM summarization callback
			return "", nil
		})
		collector.ResetSession()
	}()
}
