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
	foregroundAgentIdlePoll              = 250 * time.Millisecond
	foregroundAgentBackgroundQuietPeriod = 10 * time.Second
)

var foregroundAgentWork atomic.Int64
var foregroundAgentLastDoneUnixNano atomic.Int64

var foregroundAgentOwners = struct {
	mu     sync.Mutex
	counts map[string]int
}{counts: make(map[string]int)}

func (a *App) beginForegroundAgentLoop(ownerID, requestID, loopID string) func() {
	if a == nil {
		return func() {}
	}
	active := a.foregroundAgentLoops.Add(1)
	globalActive, ownerFirst := beginForegroundAgentOwner(ownerID)
	log.Printf("[agent-qos] foreground_start owner=%q request_id=%q loop=%q active=%d global_active=%d owner_first=%v", ownerID, requestID, loopID, active, globalActive, ownerFirst)
	globalLLMScheduler.CancelActiveBackground("foreground-agent-start")
	if a.cancelMemoryPipelineRun("foreground-agent-start") {
		a.triggerMemoryPipelineSoon(a.projectIndexMemoryDebounce())
	}
	if h := a.imHandler; h != nil {
		h.cancelBackgroundLLMForOwner(ownerID)
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
			foregroundAgentLastDoneUnixNano.Store(time.Now().UnixNano())
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

func foregroundAgentQuietUntil(ownerID string) time.Time {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		return time.Time{}
	}
	last := foregroundAgentLastDoneUnixNano.Load()
	if last <= 0 {
		return time.Time{}
	}
	return time.Unix(0, last).Add(foregroundAgentBackgroundQuietPeriod)
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
