package main

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	foregroundAgentIdlePoll = 250 * time.Millisecond

	// foregroundAgentBackgroundQuietPeriod is the per-owner quiet period after
	// a foreground loop finishes. It prevents the owner's own post-conversation
	// background processing from starting immediately and competing with a
	// rapid follow-up foreground message from the same owner.
	//
	// It is scoped per-ownerID: tab A's quiet period does not affect tab B's
	// background processing, so multiple concurrent tabs do not starve each
	// other's background tasks.
	foregroundAgentBackgroundQuietPeriod = 5 * time.Second
)

var foregroundAgentWork atomic.Int64

// foregroundAgentOwners tracks per-owner foreground loop nesting counts and
// the nanosecond timestamp of the most recent loop completion.
var foregroundAgentOwners = struct {
	mu         sync.Mutex
	counts     map[string]int
	lastDoneNs map[string]int64 // per-owner last foreground-done UnixNano
}{
	counts:     make(map[string]int),
	lastDoneNs: make(map[string]int64),
}

func (a *App) beginForegroundAgentLoop(ownerID, requestID, loopID string) func() {
	if a == nil {
		return func() {}
	}
	active := a.foregroundAgentLoops.Add(1)
	globalActive, ownerFirst := beginForegroundAgentOwner(ownerID)
	log.Printf("[agent-qos] foreground_start owner=%q request_id=%q loop=%q active=%d global_active=%d owner_first=%v", ownerID, requestID, loopID, active, globalActive, ownerFirst)
	// Cancel only background LLM requests belonging to the SAME owner (tab).
	// Cancelling all background LLM when any tab starts a foreground loop would
	// starve background processing in other tabs during multi-tab sessions.
	// Memory pipeline and per-owner background LLM are still cancelled so this
	// owner's own foreground loop gets exclusive LLM access for its tab.
	if a.cancelMemoryPipelineRun("foreground-agent-start") {
		a.triggerMemoryPipelineSoon(a.projectIndexMemoryDebounce())
	}
	if h := a.imHandler; h != nil {
		h.cancelBackgroundLLMForOwner(ownerID)
	}
	// Only cancel global background LLM leases when the scheduler is in
	// fg-degraded mode — i.e. when a provider 429/overload reduced the
	// foreground slot limit from 4 to 1 and background LLM would compete
	// for that sole remaining slot. In healthy mode (fgLimit=4) or when only
	// background slots are paused (bgPaused), there is no slot shortage for
	// foreground, so background work in other tabs should not be disrupted.
	if globalLLMScheduler.IsForegroundDegraded() {
		globalLLMScheduler.CancelActiveBackground("foreground-agent-start-degraded")
	}
	cleanupOnce := sync.Once{}
	return func() {
		cleanupOnce.Do(func() {
			active := a.foregroundAgentLoops.Add(-1)
			if active < 0 {
				a.foregroundAgentLoops.Store(0)
				active = 0
			}
			globalActive, ownerDone := endForegroundAgentOwner(ownerID)
			globalLLMScheduler.Dispatch()
			log.Printf("[agent-qos] foreground_done owner=%q request_id=%q loop=%q active=%d global_active=%d owner_done=%v", ownerID, requestID, loopID, active, globalActive, ownerDone)
		})
	}
}

func beginForegroundAgentOwner(ownerID string) (int64, bool) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		ownerID = "unknown"
	}
	foregroundAgentOwners.mu.Lock()
	defer foregroundAgentOwners.mu.Unlock()
	count := foregroundAgentOwners.counts[ownerID]
	foregroundAgentOwners.counts[ownerID] = count + 1
	if count > 0 {
		return foregroundAgentWork.Load(), false
	}
	return foregroundAgentWork.Add(1), true
}

func endForegroundAgentOwner(ownerID string) (int64, bool) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		ownerID = "unknown"
	}
	foregroundAgentOwners.mu.Lock()
	defer foregroundAgentOwners.mu.Unlock()
	count := foregroundAgentOwners.counts[ownerID]
	if count <= 1 {
		delete(foregroundAgentOwners.counts, ownerID)
		// Record per-owner completion time for the quiet-period check.
		foregroundAgentOwners.lastDoneNs[ownerID] = time.Now().UnixNano()
		active := foregroundAgentWork.Add(-1)
		if active < 0 {
			foregroundAgentWork.Store(0)
			active = 0
		}
		return active, true
	}
	foregroundAgentOwners.counts[ownerID] = count - 1
	return foregroundAgentWork.Load(), false
}

func activeForegroundAgentWork() int64 {
	return foregroundAgentWork.Load()
}

func activeForegroundAgentOwnerCount(ownerID string) int {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return int(activeForegroundAgentWork())
	}
	foregroundAgentOwners.mu.Lock()
	defer foregroundAgentOwners.mu.Unlock()
	return foregroundAgentOwners.counts[ownerID]
}

func foregroundAgentOwnersSnapshot() string {
	foregroundAgentOwners.mu.Lock()
	defer foregroundAgentOwners.mu.Unlock()
	if len(foregroundAgentOwners.counts) == 0 {
		return ""
	}
	owners := make([]string, 0, len(foregroundAgentOwners.counts))
	for owner := range foregroundAgentOwners.counts {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	parts := make([]string, 0, len(owners))
	for _, owner := range owners {
		parts = append(parts, owner+":"+strconv.Itoa(foregroundAgentOwners.counts[owner]))
	}
	return strings.Join(parts, ",")
}

func (a *App) activeForegroundAgentLoops() int64 {
	if a == nil {
		return 0
	}
	return a.foregroundAgentLoops.Load()
}

func (a *App) waitForForegroundAgentIdle(ctx context.Context, purpose, ownerID string) bool {
	if a == nil {
		return true
	}
	ownerID = strings.TrimSpace(ownerID)
	if ctx == nil {
		ctx = context.Background()
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = "background"
	}
	active := a.activeForegroundAgentLoopsForWait(ownerID)
	quietUntil := foregroundAgentQuietUntil(ownerID)
	if active == 0 && !quietUntil.After(time.Now()) {
		return true
	}
	startedAt := time.Now()
	log.Printf("[agent-qos] background_wait_start purpose=%q owner=%q active=%d quiet_for=%s owners=%q", purpose, ownerID, active, time.Until(quietUntil).Round(time.Millisecond), foregroundAgentOwnersSnapshot())
	ticker := time.NewTicker(foregroundAgentIdlePoll)
	defer ticker.Stop()
	for {
		quietUntil = foregroundAgentQuietUntil(ownerID)
		if a.activeForegroundAgentLoopsForWait(ownerID) == 0 && !quietUntil.After(time.Now()) {
			log.Printf("[agent-qos] background_wait_done purpose=%q owner=%q waited=%s", purpose, ownerID, time.Since(startedAt).Round(time.Millisecond))
			return true
		}
		select {
		case <-ctx.Done():
			log.Printf("[agent-qos] background_wait_cancel purpose=%q owner=%q waited=%s err=%v", purpose, ownerID, time.Since(startedAt).Round(time.Millisecond), ctx.Err())
			return false
		case <-ticker.C:
		}
	}
}

// foregroundAgentQuietUntil returns the time until which background processing
// for the given owner should stay idle after its most recent foreground loop
// completed. This prevents the owner's own post-conversation background tasks
// from starting immediately and competing with a rapid follow-up foreground
// message from the same tab.
//
// The quiet period is scoped per-ownerID: tab A finishing does not affect tab
// B's background quiet period. When ownerID is empty (global background wait),
// we use the most recent completion of ANY currently-active-or-recently-active
// owner. Owners whose last completion is older than 2x the quiet period are
// pruned from the map to prevent unbounded growth.
func foregroundAgentQuietUntil(ownerID string) time.Time {
	ownerID = strings.TrimSpace(ownerID)
	now := time.Now()
	pruneThreshold := now.Add(-2 * foregroundAgentBackgroundQuietPeriod)

	foregroundAgentOwners.mu.Lock()
	defer foregroundAgentOwners.mu.Unlock()

	if ownerID != "" {
		ns := foregroundAgentOwners.lastDoneNs[ownerID]
		if ns <= 0 {
			return time.Time{}
		}
		t := time.Unix(0, ns)
		if t.Before(pruneThreshold) {
			// Expired — prune and return zero.
			delete(foregroundAgentOwners.lastDoneNs, ownerID)
			return time.Time{}
		}
		return t.Add(foregroundAgentBackgroundQuietPeriod)
	}

	// Global wait: use the most recent completion, but only among owners that
	// are still active (have a non-zero count) OR finished recently enough to
	// be within the quiet period. Prune stale entries while iterating.
	var latest int64
	for owner, ns := range foregroundAgentOwners.lastDoneNs {
		t := time.Unix(0, ns)
		if t.Before(pruneThreshold) {
			delete(foregroundAgentOwners.lastDoneNs, owner)
			continue
		}
		if ns > latest {
			latest = ns
		}
	}
	if latest <= 0 {
		return time.Time{}
	}
	return time.Unix(0, latest).Add(foregroundAgentBackgroundQuietPeriod)
}

func (a *App) activeForegroundAgentLoopsForWait(ownerID string) int64 {
	if a == nil {
		return 0
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return a.activeForegroundAgentLoops()
	}
	return int64(activeForegroundAgentOwnerCount(ownerID))
}
