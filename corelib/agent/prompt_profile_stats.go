package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// PromptProfileStats is a process-local (+ optional durable) snapshot of
// adaptive prompt usage and estimated token savings.
type PromptProfileStats struct {
	LightTurns  int64  `json:"light_turns"`
	FullTurns   int64  `json:"full_turns"`
	LastProfile string `json:"last_profile,omitempty"`
	LastAt      string `json:"last_at,omitempty"`
	// LightPercent is light/(light+full)*100 when any turns recorded; else 0.
	LightPercent int `json:"light_percent"`
	// EstTokensSaved is cumulative (full-light) token estimate when light is chosen.
	EstTokensSaved int64 `json:"est_tokens_saved"`
	// LastFullTokens / LastLightTokens are the most recent dual estimate (light turns only).
	LastFullTokens  int `json:"last_full_tokens,omitempty"`
	LastLightTokens int `json:"last_light_tokens,omitempty"`
	// LastSavedTokens is max(0, last_full-last_light) for the last light turn.
	LastSavedTokens int `json:"last_saved_tokens,omitempty"`
	// ByTask counts turns by ClassifyTurn task type (fast/intent/summary/…).
	ByTask map[string]int64 `json:"by_task,omitempty"`
	// LastTask / LastReason are the most recent classify outcome (or env force).
	LastTask   string `json:"last_task,omitempty"`
	LastReason string `json:"last_reason,omitempty"`
	// LightToolDenies counts non-allowlisted tool calls blocked during light turns
	// (misroute signal — model still requested bash/files/etc.).
	LightToolDenies int64 `json:"light_tool_denies"`
	// ByDeniedTool breaks down which tools were blocked on light turns.
	ByDeniedTool map[string]int64 `json:"by_denied_tool,omitempty"`
	// LastDeniedTool is the most recently blocked tool name on a light turn.
	LastDeniedTool string `json:"last_denied_tool,omitempty"`
	// LightUpgrades counts preemptive light→full upgrades (soft cues / policy).
	LightUpgrades int64 `json:"light_upgrades"`
	// LastUpgradeReason is the most recent light→full upgrade reason.
	LastUpgradeReason string `json:"last_upgrade_reason,omitempty"`
	// AbEligibleLight is turns that classified as light (before A/B force-full).
	AbEligibleLight int64 `json:"ab_eligible_light"`
	// AbSampleFull is light-eligible turns forced to full for quality A/B.
	AbSampleFull int64 `json:"ab_sample_full"`
	// AbSamplePercent is the configured MACLAW_PROMPT_AB_PERCENT at read time (not durable).
	AbSamplePercent int `json:"ab_sample_percent,omitempty"`
	// UpgradeRatePercent ≈ light_upgrades / max(1, light_turns+full_turns) * 100.
	UpgradeRatePercent int `json:"upgrade_rate_percent,omitempty"`
	// DenyRatePercent ≈ light_tool_denies / max(1, light_turns) * 100.
	DenyRatePercent int `json:"deny_rate_percent,omitempty"`
}

// PromptProfileDecision is one adaptive-prompt routing outcome for stats.
type PromptProfileDecision struct {
	Profile     PromptProfile
	FullTokens  int
	LightTokens int
	// Task is ClassifyTurn task type (fast|intent|summary|reasoning|…).
	Task string
	// Reason is ClassifyTurn / env-override reason string.
	Reason string
}

type promptProfileCounters struct {
	light           atomic.Int64
	full            atomic.Int64
	estTokensSaved  atomic.Int64
	lightToolDenies atomic.Int64
	lightUpgrades   atomic.Int64
	abEligibleLight atomic.Int64
	abSampleFull    atomic.Int64

	mu                sync.Mutex
	last              string
	at                time.Time
	lastFullTokens    int
	lastLightTokens   int
	byTask            map[string]int64
	lastTask          string
	lastReason        string
	byDeniedTool      map[string]int64
	lastDeniedTool    string
	lastUpgradeReason string
	persistDirty      bool
	loaded            bool
	// persistTimer debounces disk writes under bursty concurrent turns.
	persistTimer *time.Timer
}

// promptProfilePersistDebounce batches durable JSON writes (in-memory counters
// stay immediately consistent; disk lags by at most this window).
const promptProfilePersistDebounce = 400 * time.Millisecond

var processPromptProfileStats promptProfileCounters

// RecordPromptProfile increments process-local adaptive prompt counters.
// Safe for concurrent agent loops.
func RecordPromptProfile(p PromptProfile) {
	RecordPromptProfileSavings(p, 0, 0)
}

// RecordPromptProfileSavings records a turn and optional dual-build token
// estimates. When profile is light and fullTokens > lightTokens, the delta is
// added to EstTokensSaved (shadow estimate — no second LLM call).
func RecordPromptProfileSavings(p PromptProfile, fullTokens, lightTokens int) {
	RecordPromptProfileDecision(PromptProfileDecision{
		Profile:     p,
		FullTokens:  fullTokens,
		LightTokens: lightTokens,
	})
}

// RecordPromptProfileDecision records profile hit-rate, shadow savings, and
// optional classify task/reason breakdown for operator tuning.
func RecordPromptProfileDecision(d PromptProfileDecision) {
	ensurePromptProfileStatsLoaded()
	norm := NormalizePromptProfile(string(d.Profile))
	switch norm {
	case PromptProfileLight:
		processPromptProfileStats.light.Add(1)
		if d.FullTokens > 0 && d.LightTokens >= 0 && d.FullTokens > d.LightTokens {
			processPromptProfileStats.estTokensSaved.Add(int64(d.FullTokens - d.LightTokens))
		}
	default:
		processPromptProfileStats.full.Add(1)
		norm = PromptProfileFull
	}
	task := strings.TrimSpace(strings.ToLower(d.Task))
	reason := strings.TrimSpace(d.Reason)
	processPromptProfileStats.mu.Lock()
	processPromptProfileStats.last = string(norm)
	processPromptProfileStats.at = time.Now().UTC()
	if d.FullTokens > 0 || d.LightTokens > 0 {
		processPromptProfileStats.lastFullTokens = d.FullTokens
		processPromptProfileStats.lastLightTokens = d.LightTokens
	}
	if task != "" {
		if processPromptProfileStats.byTask == nil {
			processPromptProfileStats.byTask = make(map[string]int64)
		}
		processPromptProfileStats.byTask[task]++
		processPromptProfileStats.lastTask = task
	}
	if reason != "" {
		processPromptProfileStats.lastReason = reason
	}
	processPromptProfileStats.persistDirty = true
	schedulePersistPromptProfileStatsLocked()
	processPromptProfileStats.mu.Unlock()
}

// RecordLightToolDeny records a non-allowlisted tool blocked during a light turn.
// Safe for concurrent agent loops.
func RecordLightToolDeny(toolName string) {
	ensurePromptProfileStatsLoaded()
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "(unknown)"
	}
	processPromptProfileStats.lightToolDenies.Add(1)
	processPromptProfileStats.mu.Lock()
	if processPromptProfileStats.byDeniedTool == nil {
		processPromptProfileStats.byDeniedTool = make(map[string]int64)
	}
	processPromptProfileStats.byDeniedTool[name]++
	processPromptProfileStats.lastDeniedTool = name
	processPromptProfileStats.persistDirty = true
	schedulePersistPromptProfileStatsLocked()
	processPromptProfileStats.mu.Unlock()
}

// RecordLightUpgrade records a preemptive light→full profile upgrade.
func RecordLightUpgrade(reason string) {
	ensurePromptProfileStatsLoaded()
	processPromptProfileStats.lightUpgrades.Add(1)
	r := strings.TrimSpace(reason)
	processPromptProfileStats.mu.Lock()
	if r != "" {
		processPromptProfileStats.lastUpgradeReason = r
	}
	processPromptProfileStats.persistDirty = true
	schedulePersistPromptProfileStatsLocked()
	processPromptProfileStats.mu.Unlock()
}

// RecordABEligibleLight increments the A/B denominator (classified light).
func RecordABEligibleLight() {
	ensurePromptProfileStatsLoaded()
	processPromptProfileStats.abEligibleLight.Add(1)
	processPromptProfileStats.mu.Lock()
	processPromptProfileStats.persistDirty = true
	schedulePersistPromptProfileStatsLocked()
	processPromptProfileStats.mu.Unlock()
}

// RecordABSampleFull increments quality A/B force-full samples.
func RecordABSampleFull() {
	ensurePromptProfileStatsLoaded()
	processPromptProfileStats.abSampleFull.Add(1)
	processPromptProfileStats.mu.Lock()
	processPromptProfileStats.persistDirty = true
	schedulePersistPromptProfileStatsLocked()
	processPromptProfileStats.mu.Unlock()
}

// schedulePersistPromptProfileStatsLocked coalesces disk writes. Caller holds mu.
func schedulePersistPromptProfileStatsLocked() {
	if processPromptProfileStats.persistTimer != nil {
		return // already armed; dirty flag will be flushed when it fires
	}
	processPromptProfileStats.persistTimer = time.AfterFunc(promptProfilePersistDebounce, func() {
		processPromptProfileStats.mu.Lock()
		processPromptProfileStats.persistTimer = nil
		processPromptProfileStats.mu.Unlock()
		_ = persistPromptProfileStats()
	})
}

// FlushPromptProfileStats forces any pending debounced durable write now.
// Used by export and reset so operators do not lose the latest counters.
func FlushPromptProfileStats() error {
	processPromptProfileStats.mu.Lock()
	if processPromptProfileStats.persistTimer != nil {
		processPromptProfileStats.persistTimer.Stop()
		processPromptProfileStats.persistTimer = nil
	}
	processPromptProfileStats.mu.Unlock()
	return persistPromptProfileStats()
}

// EstimatePromptProfileTokens builds light and full bundles and returns
// estimated totals. Used when light is chosen to measure savings without a
// second LLM call. deps.Config.PromptProfile is restored to its original value.
func EstimatePromptProfileTokens(deps SystemPromptDeps, userMessage string, isFirstTurn bool) (fullTokens, lightTokens int) {
	orig := deps.Config.PromptProfile
	deps.Config.PromptProfile = PromptProfileFull
	fullTokens = BuildPromptBundle(deps, userMessage, isFirstTurn).TokenStats().TotalTokens
	deps.Config.PromptProfile = PromptProfileLight
	lightTokens = BuildPromptBundle(deps, userMessage, isFirstTurn).TokenStats().TotalTokens
	deps.Config.PromptProfile = orig
	return fullTokens, lightTokens
}

// GetPromptProfileStats returns process-local adaptive prompt counters.
func GetPromptProfileStats() PromptProfileStats {
	ensurePromptProfileStatsLoaded()
	light := processPromptProfileStats.light.Load()
	full := processPromptProfileStats.full.Load()
	abEligible := processPromptProfileStats.abEligibleLight.Load()
	abSample := processPromptProfileStats.abSampleFull.Load()
	denies := processPromptProfileStats.lightToolDenies.Load()
	upgrades := processPromptProfileStats.lightUpgrades.Load()
	st := PromptProfileStats{
		LightTurns:      light,
		FullTurns:       full,
		EstTokensSaved:  processPromptProfileStats.estTokensSaved.Load(),
		LightToolDenies: denies,
		LightUpgrades:   upgrades,
		AbEligibleLight: abEligible,
		AbSampleFull:    abSample,
		AbSamplePercent: PromptABSamplePercent(),
	}
	if total := light + full; total > 0 {
		st.LightPercent = int((light * 100) / total)
		st.UpgradeRatePercent = int((upgrades * 100) / total)
	}
	if light > 0 {
		st.DenyRatePercent = int((denies * 100) / light)
	}
	processPromptProfileStats.mu.Lock()
	st.LastProfile = processPromptProfileStats.last
	if !processPromptProfileStats.at.IsZero() {
		st.LastAt = processPromptProfileStats.at.Format(time.RFC3339)
	}
	st.LastFullTokens = processPromptProfileStats.lastFullTokens
	st.LastLightTokens = processPromptProfileStats.lastLightTokens
	if st.LastFullTokens > st.LastLightTokens {
		st.LastSavedTokens = st.LastFullTokens - st.LastLightTokens
	}
	st.LastTask = processPromptProfileStats.lastTask
	st.LastReason = processPromptProfileStats.lastReason
	st.LastDeniedTool = processPromptProfileStats.lastDeniedTool
	st.LastUpgradeReason = processPromptProfileStats.lastUpgradeReason
	if len(processPromptProfileStats.byTask) > 0 {
		st.ByTask = make(map[string]int64, len(processPromptProfileStats.byTask))
		for k, v := range processPromptProfileStats.byTask {
			st.ByTask[k] = v
		}
	}
	if len(processPromptProfileStats.byDeniedTool) > 0 {
		st.ByDeniedTool = make(map[string]int64, len(processPromptProfileStats.byDeniedTool))
		for k, v := range processPromptProfileStats.byDeniedTool {
			st.ByDeniedTool[k] = v
		}
	}
	processPromptProfileStats.mu.Unlock()
	return st
}

// PromptProfileStatsPath returns the durable stats file path.
func PromptProfileStatsPath() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "prompt_profile.json")
}

type promptProfileDiskSnapshot struct {
	LightTurns        int64            `json:"light_turns"`
	FullTurns         int64            `json:"full_turns"`
	EstTokensSaved    int64            `json:"est_tokens_saved"`
	LastProfile       string           `json:"last_profile,omitempty"`
	LastAt            string           `json:"last_at,omitempty"`
	LastFullTokens    int              `json:"last_full_tokens,omitempty"`
	LastLightTokens   int              `json:"last_light_tokens,omitempty"`
	ByTask            map[string]int64 `json:"by_task,omitempty"`
	LastTask          string           `json:"last_task,omitempty"`
	LastReason        string           `json:"last_reason,omitempty"`
	LightToolDenies   int64            `json:"light_tool_denies,omitempty"`
	ByDeniedTool      map[string]int64 `json:"by_denied_tool,omitempty"`
	LastDeniedTool    string           `json:"last_denied_tool,omitempty"`
	LightUpgrades     int64            `json:"light_upgrades,omitempty"`
	LastUpgradeReason string           `json:"last_upgrade_reason,omitempty"`
	AbEligibleLight   int64            `json:"ab_eligible_light,omitempty"`
	AbSampleFull      int64            `json:"ab_sample_full,omitempty"`
}

func ensurePromptProfileStatsLoaded() {
	processPromptProfileStats.mu.Lock()
	defer processPromptProfileStats.mu.Unlock()
	if processPromptProfileStats.loaded {
		return
	}
	processPromptProfileStats.loaded = true
	path := PromptProfileStatsPath()
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	var snap promptProfileDiskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	// Only seed empty process counters (first load after start).
	if processPromptProfileStats.light.Load() == 0 && processPromptProfileStats.full.Load() == 0 {
		processPromptProfileStats.light.Store(snap.LightTurns)
		processPromptProfileStats.full.Store(snap.FullTurns)
		processPromptProfileStats.estTokensSaved.Store(snap.EstTokensSaved)
		processPromptProfileStats.lightToolDenies.Store(snap.LightToolDenies)
		processPromptProfileStats.lightUpgrades.Store(snap.LightUpgrades)
		processPromptProfileStats.abEligibleLight.Store(snap.AbEligibleLight)
		processPromptProfileStats.abSampleFull.Store(snap.AbSampleFull)
		processPromptProfileStats.last = snap.LastProfile
		if snap.LastAt != "" {
			if t, err := time.Parse(time.RFC3339, snap.LastAt); err == nil {
				processPromptProfileStats.at = t
			}
		}
		processPromptProfileStats.lastFullTokens = snap.LastFullTokens
		processPromptProfileStats.lastLightTokens = snap.LastLightTokens
		if len(snap.ByTask) > 0 {
			processPromptProfileStats.byTask = make(map[string]int64, len(snap.ByTask))
			for k, v := range snap.ByTask {
				processPromptProfileStats.byTask[k] = v
			}
		}
		processPromptProfileStats.lastTask = snap.LastTask
		processPromptProfileStats.lastReason = snap.LastReason
		if len(snap.ByDeniedTool) > 0 {
			processPromptProfileStats.byDeniedTool = make(map[string]int64, len(snap.ByDeniedTool))
			for k, v := range snap.ByDeniedTool {
				processPromptProfileStats.byDeniedTool[k] = v
			}
		}
		processPromptProfileStats.lastDeniedTool = snap.LastDeniedTool
		processPromptProfileStats.lastUpgradeReason = snap.LastUpgradeReason
	}
}

func persistPromptProfileStats() error {
	processPromptProfileStats.mu.Lock()
	if !processPromptProfileStats.persistDirty {
		processPromptProfileStats.mu.Unlock()
		return nil
	}
	processPromptProfileStats.persistDirty = false
	snap := promptProfileDiskSnapshot{
		LightTurns:        processPromptProfileStats.light.Load(),
		FullTurns:         processPromptProfileStats.full.Load(),
		EstTokensSaved:    processPromptProfileStats.estTokensSaved.Load(),
		LastProfile:       processPromptProfileStats.last,
		LastFullTokens:    processPromptProfileStats.lastFullTokens,
		LastLightTokens:   processPromptProfileStats.lastLightTokens,
		LastTask:          processPromptProfileStats.lastTask,
		LastReason:        processPromptProfileStats.lastReason,
		LightToolDenies:   processPromptProfileStats.lightToolDenies.Load(),
		LastDeniedTool:    processPromptProfileStats.lastDeniedTool,
		LightUpgrades:     processPromptProfileStats.lightUpgrades.Load(),
		LastUpgradeReason: processPromptProfileStats.lastUpgradeReason,
		AbEligibleLight:   processPromptProfileStats.abEligibleLight.Load(),
		AbSampleFull:      processPromptProfileStats.abSampleFull.Load(),
	}
	if len(processPromptProfileStats.byTask) > 0 {
		snap.ByTask = make(map[string]int64, len(processPromptProfileStats.byTask))
		for k, v := range processPromptProfileStats.byTask {
			snap.ByTask[k] = v
		}
	}
	if len(processPromptProfileStats.byDeniedTool) > 0 {
		snap.ByDeniedTool = make(map[string]int64, len(processPromptProfileStats.byDeniedTool))
		for k, v := range processPromptProfileStats.byDeniedTool {
			snap.ByDeniedTool[k] = v
		}
	}
	if !processPromptProfileStats.at.IsZero() {
		snap.LastAt = processPromptProfileStats.at.Format(time.RFC3339)
	}
	processPromptProfileStats.mu.Unlock()

	path := PromptProfileStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		markPromptProfilePersistDirty()
		return err
	}
	// Compact JSON: durable file is machine-readable; pretty is for exports.
	data, err := json.Marshal(snap)
	if err != nil {
		markPromptProfilePersistDirty()
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		markPromptProfilePersistDirty()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		markPromptProfilePersistDirty()
		return err
	}
	return nil
}

// markPromptProfilePersistDirty re-queues a durable write after a failed flush
// so counters are not lost until the next successful write.
func markPromptProfilePersistDirty() {
	processPromptProfileStats.mu.Lock()
	processPromptProfileStats.persistDirty = true
	schedulePersistPromptProfileStatsLocked()
	processPromptProfileStats.mu.Unlock()
}

// ResetPromptProfileStats clears in-memory counters and the durable stats file.
// Used by maclaw-cli shared-loop stats-reset and tests.
func ResetPromptProfileStats() error {
	processPromptProfileStats.light.Store(0)
	processPromptProfileStats.full.Store(0)
	processPromptProfileStats.estTokensSaved.Store(0)
	processPromptProfileStats.lightToolDenies.Store(0)
	processPromptProfileStats.lightUpgrades.Store(0)
	processPromptProfileStats.abEligibleLight.Store(0)
	processPromptProfileStats.abSampleFull.Store(0)
	processPromptProfileStats.mu.Lock()
	if processPromptProfileStats.persistTimer != nil {
		processPromptProfileStats.persistTimer.Stop()
		processPromptProfileStats.persistTimer = nil
	}
	processPromptProfileStats.last = ""
	processPromptProfileStats.at = time.Time{}
	processPromptProfileStats.lastFullTokens = 0
	processPromptProfileStats.lastLightTokens = 0
	processPromptProfileStats.byTask = nil
	processPromptProfileStats.lastTask = ""
	processPromptProfileStats.lastReason = ""
	processPromptProfileStats.byDeniedTool = nil
	processPromptProfileStats.lastDeniedTool = ""
	processPromptProfileStats.lastUpgradeReason = ""
	processPromptProfileStats.persistDirty = false
	processPromptProfileStats.loaded = true
	processPromptProfileStats.mu.Unlock()
	path := PromptProfileStatsPath()
	// Best-effort remove; missing file is OK.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		// Fall back to writing an empty snapshot so path stays valid.
		processPromptProfileStats.mu.Lock()
		processPromptProfileStats.persistDirty = true
		processPromptProfileStats.mu.Unlock()
		return persistPromptProfileStats()
	}
	return nil
}

// ResetPromptProfileStatsForTest clears counters without touching disk layout
// assumptions beyond ResetPromptProfileStats (tests).
func ResetPromptProfileStatsForTest() {
	_ = ResetPromptProfileStats()
}

// FormatPromptProfileLine returns a one-line status summary for TUI /status
// and CLI shared-loop. Empty when no adaptive turns have been recorded yet.
//
// Example: "adaptive-prompt: light 66% (2/3) · est_saved=3.8k tok · last=light(-3.0k)"
func FormatPromptProfileLine() string {
	st := GetPromptProfileStats()
	return st.FormatLine()
}

// FormatLine renders PromptProfileStats as a compact operator line.
func (st PromptProfileStats) FormatLine() string {
	total := st.LightTurns + st.FullTurns
	forced, forceOK := EnvPromptProfileOverride()
	if total <= 0 && !forceOK {
		return ""
	}
	var b strings.Builder
	if total <= 0 {
		fmt.Fprintf(&b, "adaptive-prompt: no turns yet")
	} else {
		fmt.Fprintf(&b, "adaptive-prompt: light %d%% (%d/%d)", st.LightPercent, st.LightTurns, total)
		if st.EstTokensSaved > 0 {
			fmt.Fprintf(&b, " · est_saved=%s tok", formatCompactTokenCount(int(st.EstTokensSaved)))
		}
		if p := strings.TrimSpace(st.LastProfile); p != "" {
			if st.LastSavedTokens > 0 && p == string(PromptProfileLight) {
				fmt.Fprintf(&b, " · last=%s(-%s)", p, formatCompactTokenCount(st.LastSavedTokens))
			} else {
				fmt.Fprintf(&b, " · last=%s", p)
			}
		}
		if task := strings.TrimSpace(st.LastTask); task != "" {
			fmt.Fprintf(&b, " · task=%s", task)
		}
		if by := formatByTaskSummary(st.ByTask); by != "" {
			fmt.Fprintf(&b, " · by_task=%s", by)
		}
		if st.LightToolDenies > 0 {
			fmt.Fprintf(&b, " · light_deny=%d", st.LightToolDenies)
			if byDeny := formatByDeniedToolSummary(st.ByDeniedTool); byDeny != "" {
				fmt.Fprintf(&b, "(%s)", byDeny)
			} else if d := strings.TrimSpace(st.LastDeniedTool); d != "" {
				fmt.Fprintf(&b, "(%s)", d)
			}
		}
		if st.LightUpgrades > 0 {
			fmt.Fprintf(&b, " · light_upgrade=%d", st.LightUpgrades)
			if r := strings.TrimSpace(st.LastUpgradeReason); r != "" {
				// Keep compact: tool_deny_retry:bash → bash
				if short := compactUpgradeReason(r); short != "" {
					fmt.Fprintf(&b, "(%s)", short)
				}
			}
		}
		if st.AbEligibleLight > 0 {
			fmt.Fprintf(&b, " · ab=%d/%d", st.AbSampleFull, st.AbEligibleLight)
		}
		if st.UpgradeRatePercent > 0 {
			fmt.Fprintf(&b, " · upgrade_rate=%d%%", st.UpgradeRatePercent)
		}
		if st.DenyRatePercent > 0 {
			fmt.Fprintf(&b, " · deny_rate=%d%%", st.DenyRatePercent)
		}
	}
	if forceOK {
		fmt.Fprintf(&b, " · env %s=%s", PromptProfileEnvKey, forced)
	}
	if pct := PromptABSamplePercent(); pct > 0 {
		fmt.Fprintf(&b, " · ab_pct=%d", pct)
	}
	return b.String()
}

// formatByDeniedToolSummary returns compact "bash:2,write_file:1" (max 3 keys).
func formatByDeniedToolSummary(by map[string]int64) string {
	return formatCountMapSummary(by, 3)
}

// compactUpgradeReason shortens upgrade reasons for status lines.
// tool_deny_retry:bash → bash; soft_full_agent_intent stays as-is (truncated).
func compactUpgradeReason(reason string) string {
	r := strings.TrimSpace(reason)
	if r == "" {
		return ""
	}
	const prefix = "tool_deny_retry:"
	if strings.HasPrefix(r, prefix) {
		tool := strings.TrimSpace(strings.TrimPrefix(r, prefix))
		if tool != "" {
			return tool
		}
	}
	if len(r) > 24 {
		return r[:24] + "…"
	}
	return r
}

// formatByTaskSummary returns a compact "fast:3,reasoning:1" breakdown (max 4 keys).
func formatByTaskSummary(by map[string]int64) string {
	return formatCountMapSummary(by, 4)
}

// formatCountMapSummary returns "k:v,k:v" for top-N keys by count then name.
func formatCountMapSummary(by map[string]int64, maxKeys int) string {
	if len(by) == 0 {
		return ""
	}
	if maxKeys <= 0 {
		maxKeys = 4
	}
	type kv struct {
		k string
		v int64
	}
	items := make([]kv, 0, len(by))
	for k, v := range by {
		if strings.TrimSpace(k) == "" || v <= 0 {
			continue
		}
		items = append(items, kv{k: k, v: v})
	}
	if len(items) == 0 {
		return ""
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	if len(items) > maxKeys {
		items = items[:maxKeys]
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s:%d", it.k, it.v))
	}
	return strings.Join(parts, ",")
}
