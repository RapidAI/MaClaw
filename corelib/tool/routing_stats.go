package tool

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// RecordRoutingStats gates mutation of the process-global routing counters.
// It exists so offline evaluation harnesses (corelib/tool/surfaceeval) can
// replay the legacy router without polluting live telemetry or scheduling
// durable writes against the real routing.json. When false, the Record*
// functions return immediately and GetRoutingStats keeps reporting whatever
// counters are already live. It is not synchronized; hosts that flip it must
// do so before starting concurrent routing (or, like surfaceeval.Run, when no
// other routing is in flight).
var RecordRoutingStats = true

// RoutingStats is a process-local (+ durable) snapshot of legacy tool-routing
// decisions: text-router hits, conditional-tool activation, surface-admission
// denials, petition rescues, and discover_tool outcomes. It is the Phase 0
// measurement baseline for the tool-routing improvement plan
// (docs/design/tool-routing-improvement-plan-zh.md); counters are bounded
// cardinality and carry no user text.
type RoutingStats struct {
	// LegacyRouteCalls counts RouteWithOptions/RecommendWithOptions invocations.
	LegacyRouteCalls int64 `json:"legacy_route_calls"`
	// LegacyRouteSelectedTotal sums selected tool counts across routes.
	LegacyRouteSelectedTotal int64 `json:"legacy_route_selected_total"`
	// LegacyRouteMaxSelected / LegacyRouteLastSelected track the rendered
	// surface-size distribution (proxy for tool-surface token occupancy).
	LegacyRouteMaxSelected  int `json:"legacy_route_max_selected,omitempty"`
	LegacyRouteLastSelected int `json:"legacy_route_last_selected,omitempty"`
	// ConditionalActivated counts routes where the intent classifier
	// activated conditional (otherwise fail-closed) tools.
	ConditionalActivated int64 `json:"conditional_activated"`
	// UnrenderedDenials counts tool calls rejected by the rendered-surface
	// fence (names not frozen for the concrete outbound request).
	UnrenderedDenials int64 `json:"unrendered_denials"`
	// ConsumedGrantDenials counts denials for already-consumed one-shot
	// grants (subset of UnrenderedDenials with dedicated text).
	ConsumedGrantDenials int64 `json:"consumed_grant_denials"`
	// PetitionGrants counts unrendered calls rescued by a governed host's
	// PetitionToolCall for the next iteration.
	PetitionGrants int64 `json:"petition_grants"`
	// DiscoverToolCalls / DiscoverToolMisses count discover_tool outcomes
	// (recovery-path pressure from routing misses).
	DiscoverToolCalls  int64 `json:"discover_tool_calls"`
	DiscoverToolMisses int64 `json:"discover_tool_misses"`
}

type routingStatsCounters struct {
	legacyRouteCalls         atomic.Int64
	legacyRouteSelectedTotal atomic.Int64
	conditionalActivated     atomic.Int64
	unrenderedDenials        atomic.Int64
	consumedGrantDenials     atomic.Int64
	petitionGrants           atomic.Int64
	discoverToolCalls        atomic.Int64
	discoverToolMisses       atomic.Int64

	mu              sync.Mutex
	maxSelected     int
	lastSelected    int
	persistDirty    bool
	persistFailures int
	loaded          bool
	persistTimer    *time.Timer
}

const routingStatsPersistDebounce = 400 * time.Millisecond

// routingStatsMaxPersistFailures bounds the debounced persist retry loop: a
// persist attempt that keeps failing re-arms the timer up to this many
// consecutive times, then the dirty flag is dropped (with one log line).
// In-memory counters stay live; only the durable snapshot may lag or be
// lost, which is acceptable after a bounded retry.
const routingStatsMaxPersistFailures = 5

var processRoutingStats routingStatsCounters

// RecordLegacyRoute records one legacy text-router invocation and its
// selected surface size. Safe for concurrent loops.
func RecordLegacyRoute(selectedCount int) {
	if !RecordRoutingStats {
		return
	}
	ensureRoutingStatsLoaded()
	if selectedCount < 0 {
		selectedCount = 0
	}
	processRoutingStats.legacyRouteCalls.Add(1)
	processRoutingStats.legacyRouteSelectedTotal.Add(int64(selectedCount))
	processRoutingStats.mu.Lock()
	processRoutingStats.lastSelected = selectedCount
	if selectedCount > processRoutingStats.maxSelected {
		processRoutingStats.maxSelected = selectedCount
	}
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
	processRoutingStats.mu.Unlock()
}

// RecordConditionalRouteActivation records a route where the intent
// classifier activated conditional tools (fail-closed path taken).
func RecordConditionalRouteActivation() {
	if !RecordRoutingStats {
		return
	}
	ensureRoutingStatsLoaded()
	processRoutingStats.conditionalActivated.Add(1)
	processRoutingStats.mu.Lock()
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
	processRoutingStats.mu.Unlock()
}

// RecordUnrenderedToolCallDenial records a rendered-surface fence rejection.
func RecordUnrenderedToolCallDenial() {
	if !RecordRoutingStats {
		return
	}
	ensureRoutingStatsLoaded()
	processRoutingStats.unrenderedDenials.Add(1)
	processRoutingStats.mu.Lock()
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
	processRoutingStats.mu.Unlock()
}

// RecordConsumedGrantDenial records a denial for an already-consumed grant.
func RecordConsumedGrantDenial() {
	if !RecordRoutingStats {
		return
	}
	ensureRoutingStatsLoaded()
	processRoutingStats.consumedGrantDenials.Add(1)
	processRoutingStats.mu.Lock()
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
	processRoutingStats.mu.Unlock()
}

// RecordPetitionGrant records a governed-host petition rescue.
func RecordPetitionGrant() {
	if !RecordRoutingStats {
		return
	}
	ensureRoutingStatsLoaded()
	processRoutingStats.petitionGrants.Add(1)
	processRoutingStats.mu.Lock()
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
	processRoutingStats.mu.Unlock()
}

// RecordDiscoverToolCall records one discover_tool outcome; matched=false is
// a routing-miss recovery that found nothing.
func RecordDiscoverToolCall(matched bool) {
	if !RecordRoutingStats {
		return
	}
	ensureRoutingStatsLoaded()
	processRoutingStats.discoverToolCalls.Add(1)
	if !matched {
		processRoutingStats.discoverToolMisses.Add(1)
	}
	processRoutingStats.mu.Lock()
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
	processRoutingStats.mu.Unlock()
}

func scheduleRoutingStatsPersistLocked() {
	if processRoutingStats.persistTimer != nil {
		return
	}
	processRoutingStats.persistTimer = time.AfterFunc(routingStatsPersistDebounce, func() {
		processRoutingStats.mu.Lock()
		processRoutingStats.persistTimer = nil
		processRoutingStats.mu.Unlock()
		_ = persistRoutingStats()
	})
}

// GetRoutingStats returns the current process-local routing counters.
func GetRoutingStats() RoutingStats {
	ensureRoutingStatsLoaded()
	st := RoutingStats{
		LegacyRouteCalls:         processRoutingStats.legacyRouteCalls.Load(),
		LegacyRouteSelectedTotal: processRoutingStats.legacyRouteSelectedTotal.Load(),
		ConditionalActivated:     processRoutingStats.conditionalActivated.Load(),
		UnrenderedDenials:        processRoutingStats.unrenderedDenials.Load(),
		ConsumedGrantDenials:     processRoutingStats.consumedGrantDenials.Load(),
		PetitionGrants:           processRoutingStats.petitionGrants.Load(),
		DiscoverToolCalls:        processRoutingStats.discoverToolCalls.Load(),
		DiscoverToolMisses:       processRoutingStats.discoverToolMisses.Load(),
	}
	processRoutingStats.mu.Lock()
	st.LegacyRouteMaxSelected = processRoutingStats.maxSelected
	st.LegacyRouteLastSelected = processRoutingStats.lastSelected
	processRoutingStats.mu.Unlock()
	return st
}

// RoutingStatsPath returns the durable stats file path.
func RoutingStatsPath() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "routing.json")
}

type routingStatsDiskSnapshot struct {
	LegacyRouteCalls         int64 `json:"legacy_route_calls"`
	LegacyRouteSelectedTotal int64 `json:"legacy_route_selected_total"`
	LegacyRouteMaxSelected   int   `json:"legacy_route_max_selected,omitempty"`
	LegacyRouteLastSelected  int   `json:"legacy_route_last_selected,omitempty"`
	ConditionalActivated     int64 `json:"conditional_activated"`
	UnrenderedDenials        int64 `json:"unrendered_denials"`
	ConsumedGrantDenials     int64 `json:"consumed_grant_denials,omitempty"`
	PetitionGrants           int64 `json:"petition_grants,omitempty"`
	DiscoverToolCalls        int64 `json:"discover_tool_calls"`
	DiscoverToolMisses       int64 `json:"discover_tool_misses,omitempty"`
}

func ensureRoutingStatsLoaded() {
	processRoutingStats.mu.Lock()
	defer processRoutingStats.mu.Unlock()
	if processRoutingStats.loaded {
		return
	}
	processRoutingStats.loaded = true
	data, err := os.ReadFile(RoutingStatsPath())
	if err != nil || len(data) == 0 {
		return
	}
	var snap routingStatsDiskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	if processRoutingStats.legacyRouteCalls.Load() != 0 {
		return // only seed empty counters (first load after start)
	}
	processRoutingStats.legacyRouteCalls.Store(snap.LegacyRouteCalls)
	processRoutingStats.legacyRouteSelectedTotal.Store(snap.LegacyRouteSelectedTotal)
	processRoutingStats.conditionalActivated.Store(snap.ConditionalActivated)
	processRoutingStats.unrenderedDenials.Store(snap.UnrenderedDenials)
	processRoutingStats.consumedGrantDenials.Store(snap.ConsumedGrantDenials)
	processRoutingStats.petitionGrants.Store(snap.PetitionGrants)
	processRoutingStats.discoverToolCalls.Store(snap.DiscoverToolCalls)
	processRoutingStats.discoverToolMisses.Store(snap.DiscoverToolMisses)
	processRoutingStats.maxSelected = snap.LegacyRouteMaxSelected
	processRoutingStats.lastSelected = snap.LegacyRouteLastSelected
}

func persistRoutingStats() error {
	processRoutingStats.mu.Lock()
	if !processRoutingStats.persistDirty {
		processRoutingStats.mu.Unlock()
		return nil
	}
	processRoutingStats.persistDirty = false
	snap := routingStatsDiskSnapshot{
		LegacyRouteCalls:         processRoutingStats.legacyRouteCalls.Load(),
		LegacyRouteSelectedTotal: processRoutingStats.legacyRouteSelectedTotal.Load(),
		LegacyRouteMaxSelected:   processRoutingStats.maxSelected,
		LegacyRouteLastSelected:  processRoutingStats.lastSelected,
		ConditionalActivated:     processRoutingStats.conditionalActivated.Load(),
		UnrenderedDenials:        processRoutingStats.unrenderedDenials.Load(),
		ConsumedGrantDenials:     processRoutingStats.consumedGrantDenials.Load(),
		PetitionGrants:           processRoutingStats.petitionGrants.Load(),
		DiscoverToolCalls:        processRoutingStats.discoverToolCalls.Load(),
		DiscoverToolMisses:       processRoutingStats.discoverToolMisses.Load(),
	}
	processRoutingStats.mu.Unlock()

	path := RoutingStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		markRoutingStatsPersistDirty()
		return err
	}
	data, err := json.Marshal(snap)
	if err != nil {
		markRoutingStatsPersistDirty()
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		markRoutingStatsPersistDirty()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		markRoutingStatsPersistDirty()
		return err
	}
	processRoutingStats.mu.Lock()
	processRoutingStats.persistFailures = 0
	processRoutingStats.mu.Unlock()
	return nil
}

func markRoutingStatsPersistDirty() {
	processRoutingStats.mu.Lock()
	defer processRoutingStats.mu.Unlock()
	processRoutingStats.persistFailures++
	if processRoutingStats.persistFailures >= routingStatsMaxPersistFailures {
		log.Printf("[routing-stats] dropping dirty flag after %d consecutive persist failures; in-memory counters stay live",
			processRoutingStats.persistFailures)
		processRoutingStats.persistDirty = false
		return
	}
	processRoutingStats.persistDirty = true
	scheduleRoutingStatsPersistLocked()
}

// FlushRoutingStats forces any pending debounced durable write now.
func FlushRoutingStats() error {
	processRoutingStats.mu.Lock()
	if processRoutingStats.persistTimer != nil {
		processRoutingStats.persistTimer.Stop()
		processRoutingStats.persistTimer = nil
	}
	processRoutingStats.mu.Unlock()
	return persistRoutingStats()
}

// ResetRoutingStats clears in-memory counters and the durable stats file.
func ResetRoutingStats() error {
	processRoutingStats.legacyRouteCalls.Store(0)
	processRoutingStats.legacyRouteSelectedTotal.Store(0)
	processRoutingStats.conditionalActivated.Store(0)
	processRoutingStats.unrenderedDenials.Store(0)
	processRoutingStats.consumedGrantDenials.Store(0)
	processRoutingStats.petitionGrants.Store(0)
	processRoutingStats.discoverToolCalls.Store(0)
	processRoutingStats.discoverToolMisses.Store(0)
	processRoutingStats.mu.Lock()
	if processRoutingStats.persistTimer != nil {
		processRoutingStats.persistTimer.Stop()
		processRoutingStats.persistTimer = nil
	}
	processRoutingStats.maxSelected = 0
	processRoutingStats.lastSelected = 0
	processRoutingStats.persistDirty = false
	processRoutingStats.persistFailures = 0
	processRoutingStats.loaded = true
	processRoutingStats.mu.Unlock()
	if err := os.Remove(RoutingStatsPath()); err != nil && !os.IsNotExist(err) {
		markRoutingStatsPersistDirty()
		return persistRoutingStats()
	}
	return nil
}

// FormatRoutingLine renders RoutingStats as a compact operator line for
// TUI /status and CLI shared-loop.
func FormatRoutingLine() string {
	st := GetRoutingStats()
	if st.LegacyRouteCalls == 0 && st.UnrenderedDenials == 0 && st.DiscoverToolCalls == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "routing: legacy=%d", st.LegacyRouteCalls)
	if st.LegacyRouteCalls > 0 {
		avg := float64(st.LegacyRouteSelectedTotal) / float64(st.LegacyRouteCalls)
		fmt.Fprintf(&b, " sel_avg=%.1f max=%d", avg, st.LegacyRouteMaxSelected)
	}
	if st.ConditionalActivated > 0 {
		fmt.Fprintf(&b, " cond=%d", st.ConditionalActivated)
	}
	if st.UnrenderedDenials > 0 {
		fmt.Fprintf(&b, " unrendered=%d", st.UnrenderedDenials)
	}
	if st.ConsumedGrantDenials > 0 {
		fmt.Fprintf(&b, " consumed=%d", st.ConsumedGrantDenials)
	}
	if st.PetitionGrants > 0 {
		fmt.Fprintf(&b, " petition=%d", st.PetitionGrants)
	}
	if st.DiscoverToolCalls > 0 {
		fmt.Fprintf(&b, " discover=%d", st.DiscoverToolCalls)
		if st.DiscoverToolMisses > 0 {
			fmt.Fprintf(&b, "(miss %d)", st.DiscoverToolMisses)
		}
	}
	return b.String()
}
