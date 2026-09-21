package tool

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// PermissionDualEvalStats is a process-local (+ durable) snapshot of
// legacy-vs-snapshot permission dual-run divergences: total mismatches,
// per-gate counts, and the last mismatch summary. It is the measurement
// base for the "对拍零差异跑稳 2 周" flip gate — counters are bounded
// cardinality and carry no rule reasons or user text.
type PermissionDualEvalStats struct {
	// Total counts all recorded mismatches across gates.
	Total int64 `json:"total"`
	// Gates maps gate name → mismatch count, bounded to
	// permissionDualEvalMaxGates distinct keys; overflow gates are
	// aggregated under Gates["(other)"].
	Gates map[string]int64 `json:"gates,omitempty"`
	// Last* summarize the most recent mismatch (no rule reasons/user text).
	LastGate   string `json:"last_gate,omitempty"`
	LastTool   string `json:"last_tool,omitempty"`
	LastLegacy string `json:"last_legacy,omitempty"`
	LastNew    string `json:"last_new,omitempty"`
	LastAt     int64  `json:"last_at,omitempty"` // unix seconds
}

type permissionDualEvalStatsCounters struct {
	total atomic.Int64

	mu              sync.Mutex
	gates           map[string]int64
	lastGate        string
	lastTool        string
	lastLegacy      string
	lastNew         string
	lastAt          int64
	persistDirty    bool
	persistFailures int
	loaded          bool
	persistTimer    *time.Timer
}

const permissionDualEvalStatsPersistDebounce = 400 * time.Millisecond

// permissionDualEvalMaxPersistFailures bounds the debounced persist retry
// loop, mirroring routingStatsMaxPersistFailures.
const permissionDualEvalMaxPersistFailures = 5

// permissionDualEvalMaxGates bounds the per-gate map cardinality. Gates
// beyond this many distinct keys aggregate under "(other)".
const permissionDualEvalMaxGates = 16

// permissionDualEvalOtherGate is the overflow bucket key for gates beyond
// permissionDualEvalMaxGates distinct names.
const permissionDualEvalOtherGate = "(other)"

var processPermissionDualEvalStats permissionDualEvalStatsCounters

// RecordPermissionDualEvalMismatch records one legacy-vs-snapshot divergence
// for a dual-run gate. gate/tool/effects are the only fields kept — rule
// reasons and user text never enter the counters. Safe for concurrent loops.
func RecordPermissionDualEvalMismatch(gate, tool, legacyEffect, newEffect string) {
	ensurePermissionDualEvalStatsLoaded()
	processPermissionDualEvalStats.total.Add(1)
	processPermissionDualEvalStats.mu.Lock()
	defer processPermissionDualEvalStats.mu.Unlock()
	if processPermissionDualEvalStats.gates == nil {
		processPermissionDualEvalStats.gates = make(map[string]int64)
	}
	key := gate
	if key == "" {
		key = "(unknown)"
	}
	if _, ok := processPermissionDualEvalStats.gates[key]; !ok {
		// New gate key: only admit it while under the bound; otherwise
		// fold into the overflow bucket.
		if len(processPermissionDualEvalStats.gates) >= permissionDualEvalMaxGates {
			key = permissionDualEvalOtherGate
		}
	}
	processPermissionDualEvalStats.gates[key]++
	processPermissionDualEvalStats.lastGate = gate
	processPermissionDualEvalStats.lastTool = tool
	processPermissionDualEvalStats.lastLegacy = legacyEffect
	processPermissionDualEvalStats.lastNew = newEffect
	processPermissionDualEvalStats.lastAt = time.Now().Unix()
	processPermissionDualEvalStats.persistDirty = true
	schedulePermissionDualEvalStatsPersistLocked()
}

func schedulePermissionDualEvalStatsPersistLocked() {
	if processPermissionDualEvalStats.persistTimer != nil {
		return
	}
	processPermissionDualEvalStats.persistTimer = time.AfterFunc(permissionDualEvalStatsPersistDebounce, func() {
		processPermissionDualEvalStats.mu.Lock()
		processPermissionDualEvalStats.persistTimer = nil
		processPermissionDualEvalStats.mu.Unlock()
		_ = persistPermissionDualEvalStats()
	})
}

// GetPermissionDualEvalStats returns the current process-local dual-eval
// divergence counters.
func GetPermissionDualEvalStats() PermissionDualEvalStats {
	ensurePermissionDualEvalStatsLoaded()
	st := PermissionDualEvalStats{
		Total: processPermissionDualEvalStats.total.Load(),
	}
	processPermissionDualEvalStats.mu.Lock()
	defer processPermissionDualEvalStats.mu.Unlock()
	if len(processPermissionDualEvalStats.gates) > 0 {
		st.Gates = make(map[string]int64, len(processPermissionDualEvalStats.gates))
		for k, v := range processPermissionDualEvalStats.gates {
			st.Gates[k] = v
		}
	}
	st.LastGate = processPermissionDualEvalStats.lastGate
	st.LastTool = processPermissionDualEvalStats.lastTool
	st.LastLegacy = processPermissionDualEvalStats.lastLegacy
	st.LastNew = processPermissionDualEvalStats.lastNew
	st.LastAt = processPermissionDualEvalStats.lastAt
	return st
}

// PermissionDualEvalStatsPath returns the durable stats file path.
func PermissionDualEvalStatsPath() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "permission_dual_eval.json")
}

type permissionDualEvalStatsDiskSnapshot struct {
	Total      int64            `json:"total"`
	Gates      map[string]int64 `json:"gates,omitempty"`
	LastGate   string           `json:"last_gate,omitempty"`
	LastTool   string           `json:"last_tool,omitempty"`
	LastLegacy string           `json:"last_legacy,omitempty"`
	LastNew    string           `json:"last_new,omitempty"`
	LastAt     int64            `json:"last_at,omitempty"`
}

func ensurePermissionDualEvalStatsLoaded() {
	processPermissionDualEvalStats.mu.Lock()
	defer processPermissionDualEvalStats.mu.Unlock()
	if processPermissionDualEvalStats.loaded {
		return
	}
	processPermissionDualEvalStats.loaded = true
	processPermissionDualEvalStats.gates = make(map[string]int64)
	data, err := os.ReadFile(PermissionDualEvalStatsPath())
	if err != nil || len(data) == 0 {
		return
	}
	var snap permissionDualEvalStatsDiskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	if processPermissionDualEvalStats.total.Load() != 0 {
		return // only seed empty counters (first load after start)
	}
	processPermissionDualEvalStats.total.Store(snap.Total)
	for k, v := range snap.Gates {
		processPermissionDualEvalStats.gates[k] = v
	}
	processPermissionDualEvalStats.lastGate = snap.LastGate
	processPermissionDualEvalStats.lastTool = snap.LastTool
	processPermissionDualEvalStats.lastLegacy = snap.LastLegacy
	processPermissionDualEvalStats.lastNew = snap.LastNew
	processPermissionDualEvalStats.lastAt = snap.LastAt
}

func persistPermissionDualEvalStats() error {
	processPermissionDualEvalStats.mu.Lock()
	if !processPermissionDualEvalStats.persistDirty {
		processPermissionDualEvalStats.mu.Unlock()
		return nil
	}
	processPermissionDualEvalStats.persistDirty = false
	snap := permissionDualEvalStatsDiskSnapshot{
		Total:      processPermissionDualEvalStats.total.Load(),
		Gates:      make(map[string]int64, len(processPermissionDualEvalStats.gates)),
		LastGate:   processPermissionDualEvalStats.lastGate,
		LastTool:   processPermissionDualEvalStats.lastTool,
		LastLegacy: processPermissionDualEvalStats.lastLegacy,
		LastNew:    processPermissionDualEvalStats.lastNew,
		LastAt:     processPermissionDualEvalStats.lastAt,
	}
	for k, v := range processPermissionDualEvalStats.gates {
		snap.Gates[k] = v
	}
	processPermissionDualEvalStats.mu.Unlock()

	path := PermissionDualEvalStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		markPermissionDualEvalStatsPersistDirty()
		return err
	}
	data, err := json.Marshal(snap)
	if err != nil {
		markPermissionDualEvalStatsPersistDirty()
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		markPermissionDualEvalStatsPersistDirty()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		markPermissionDualEvalStatsPersistDirty()
		return err
	}
	processPermissionDualEvalStats.mu.Lock()
	processPermissionDualEvalStats.persistFailures = 0
	processPermissionDualEvalStats.mu.Unlock()
	return nil
}

func markPermissionDualEvalStatsPersistDirty() {
	processPermissionDualEvalStats.mu.Lock()
	defer processPermissionDualEvalStats.mu.Unlock()
	processPermissionDualEvalStats.persistFailures++
	if processPermissionDualEvalStats.persistFailures >= permissionDualEvalMaxPersistFailures {
		log.Printf("[permission-dual-eval-stats] dropping dirty flag after %d consecutive persist failures; in-memory counters stay live",
			processPermissionDualEvalStats.persistFailures)
		processPermissionDualEvalStats.persistDirty = false
		return
	}
	processPermissionDualEvalStats.persistDirty = true
	schedulePermissionDualEvalStatsPersistLocked()
}

// FlushPermissionDualEvalStats forces any pending debounced durable write now.
func FlushPermissionDualEvalStats() error {
	processPermissionDualEvalStats.mu.Lock()
	if processPermissionDualEvalStats.persistTimer != nil {
		processPermissionDualEvalStats.persistTimer.Stop()
		processPermissionDualEvalStats.persistTimer = nil
	}
	processPermissionDualEvalStats.mu.Unlock()
	return persistPermissionDualEvalStats()
}

// ResetPermissionDualEvalStats clears in-memory counters and the durable
// stats file.
func ResetPermissionDualEvalStats() error {
	processPermissionDualEvalStats.total.Store(0)
	processPermissionDualEvalStats.mu.Lock()
	if processPermissionDualEvalStats.persistTimer != nil {
		processPermissionDualEvalStats.persistTimer.Stop()
		processPermissionDualEvalStats.persistTimer = nil
	}
	processPermissionDualEvalStats.gates = make(map[string]int64)
	processPermissionDualEvalStats.lastGate = ""
	processPermissionDualEvalStats.lastTool = ""
	processPermissionDualEvalStats.lastLegacy = ""
	processPermissionDualEvalStats.lastNew = ""
	processPermissionDualEvalStats.lastAt = 0
	processPermissionDualEvalStats.persistDirty = false
	processPermissionDualEvalStats.persistFailures = 0
	processPermissionDualEvalStats.loaded = true
	processPermissionDualEvalStats.mu.Unlock()
	if err := os.Remove(PermissionDualEvalStatsPath()); err != nil && !os.IsNotExist(err) {
		markPermissionDualEvalStatsPersistDirty()
		return persistPermissionDualEvalStats()
	}
	return nil
}

// FormatPermissionDualEvalLine renders PermissionDualEvalStats as a compact
// operator line for TUI /doctor, /status and CLI shared-loop. Empty when no
// mismatch has been recorded.
func FormatPermissionDualEvalLine() string {
	st := GetPermissionDualEvalStats()
	if st.Total == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "dual_eval: mismatches=%d", st.Total)
	keys := make([]string, 0, len(st.Gates))
	for k := range st.Gates {
		keys = append(keys, k)
	}
	// Stable output: "(other)" and "(unknown)" last, rest alphabetical.
	less := func(i, j int) bool {
		a, c := keys[i], keys[j]
		aOther := a == permissionDualEvalOtherGate || a == "(unknown)"
		bOther := c == permissionDualEvalOtherGate || c == "(unknown)"
		if aOther != bOther {
			return !aOther
		}
		return a < c
	}
	sort.Slice(keys, less)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%d", k, st.Gates[k])
	}
	if st.LastGate != "" {
		fmt.Fprintf(&b, " last=%s/%s %s->%s", st.LastGate, st.LastTool, st.LastLegacy, st.LastNew)
	}
	return b.String()
}
