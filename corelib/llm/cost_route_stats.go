package llm

// Durable cost-route tier counters under ~/.maclaw/stats/cost_route.json.
// Multi-process safe at the instance level: each host-pid writes its own slot;
// LoadCostRouteStats sums instances for today (fleet view for CLI/doctor/Hub).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// CostRouteStatsPath is the durable cost-route stats file.
func CostRouteStatsPath() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "cost_route.json")
}

// CostRouteStats is the aggregated (fleet) or process snapshot of tier recommendations.
type CostRouteStats struct {
	// ByTier counts decisions by c0|c1|c2|c3.
	ByTier map[string]int64 `json:"by_tier,omitempty"`
	// Decisions is total recorded (shadow+on only).
	Decisions int64 `json:"decisions"`
	// Applied is mode=on decisions where ApplyCostTierConfig ran with applied=true.
	Applied int64 `json:"applied"`
	// Shadow is mode=shadow observations.
	Shadow int64 `json:"shadow"`
	// ByThinking counts thinking policy recommendations.
	ByThinking map[string]int64 `json:"by_thinking,omitempty"`
	LastTier   string           `json:"last_tier,omitempty"`
	LastMode   string           `json:"last_mode,omitempty"`
	LastThink  string           `json:"last_thinking,omitempty"`
	UpdatedAt  string           `json:"updated_at,omitempty"`
	Path       string           `json:"path,omitempty"`
	// Instances is optional count of host-pid slots when this is a fleet sum.
	Instances int `json:"instances,omitempty"`
}

// CostRouteInstanceSnap is one process's contribution for a calendar day.
type CostRouteInstanceSnap struct {
	ByTier     map[string]int64 `json:"by_tier,omitempty"`
	ByThinking map[string]int64 `json:"by_thinking,omitempty"`
	Decisions  int64            `json:"decisions"`
	Applied    int64            `json:"applied"`
	Shadow     int64            `json:"shadow"`
	LastTier   string           `json:"last_tier,omitempty"`
	LastMode   string           `json:"last_mode,omitempty"`
	LastThink  string           `json:"last_thinking,omitempty"`
	UpdatedAt  string           `json:"updated_at,omitempty"`
}

// CostRouteDiskFile is the on-disk multi-process shape.
type CostRouteDiskFile struct {
	Date      string                            `json:"date"` // YYYY-MM-DD
	Instances map[string]CostRouteInstanceSnap  `json:"instances,omitempty"`
	UpdatedAt string                            `json:"updated_at,omitempty"`
	// Legacy flat fields (pre multi-instance). Migrated on first write.
	ByTier     map[string]int64 `json:"by_tier,omitempty"`
	ByThinking map[string]int64 `json:"by_thinking,omitempty"`
	Decisions  int64            `json:"decisions,omitempty"`
	Applied    int64            `json:"applied,omitempty"`
	Shadow     int64            `json:"shadow,omitempty"`
	LastTier   string           `json:"last_tier,omitempty"`
	LastMode   string           `json:"last_mode,omitempty"`
	LastThink  string           `json:"last_thinking,omitempty"`
}

const costRoutePersistDebounce = 800 * time.Millisecond

type costRouteCounters struct {
	mu           sync.Mutex
	byTier       map[string]int64
	byThinking   map[string]int64
	decisions    int64
	applied      int64
	shadow       int64
	lastTier     string
	lastMode     string
	lastThink    string
	dirty        bool
	loaded       bool
	persistTimer *time.Timer
}

var (
	globalCostRouteStats = &costRouteCounters{
		byTier:     make(map[string]int64),
		byThinking: make(map[string]int64),
	}
	costRouteFileMu sync.Mutex
)

func (c *costRouteCounters) ensureLoaded() {
	if c.loaded {
		return
	}
	c.loaded = true
	today := time.Now().Format("2006-01-02")
	data, err := os.ReadFile(CostRouteStatsPath())
	if err != nil || len(data) == 0 {
		return
	}
	var file CostRouteDiskFile
	if json.Unmarshal(data, &file) != nil {
		return
	}
	// Multi-instance (current).
	if len(file.Instances) > 0 {
		if file.Date != today {
			return // new day → process starts at 0
		}
		inst, ok := file.Instances[costProcessInstanceID()]
		if !ok {
			return
		}
		c.applyInstanceSnap(inst)
		return
	}
	// Legacy flat file: seed this process so history is not zeroed on upgrade.
	if file.Decisions > 0 || file.Applied > 0 || file.Shadow > 0 {
		c.byTier = copyInt64Map(file.ByTier)
		if c.byTier == nil {
			c.byTier = make(map[string]int64)
		}
		c.byThinking = copyInt64Map(file.ByThinking)
		if c.byThinking == nil {
			c.byThinking = make(map[string]int64)
		}
		c.decisions = file.Decisions
		c.applied = file.Applied
		c.shadow = file.Shadow
		c.lastTier = file.LastTier
		c.lastMode = file.LastMode
		c.lastThink = file.LastThink
	}
}

func (c *costRouteCounters) applyInstanceSnap(inst CostRouteInstanceSnap) {
	c.byTier = copyInt64Map(inst.ByTier)
	if c.byTier == nil {
		c.byTier = make(map[string]int64)
	}
	c.byThinking = copyInt64Map(inst.ByThinking)
	if c.byThinking == nil {
		c.byThinking = make(map[string]int64)
	}
	c.decisions = inst.Decisions
	c.applied = inst.Applied
	c.shadow = inst.Shadow
	c.lastTier = inst.LastTier
	c.lastMode = inst.LastMode
	c.lastThink = inst.LastThink
}

func (c *costRouteCounters) toInstanceSnap() CostRouteInstanceSnap {
	return CostRouteInstanceSnap{
		ByTier:     copyInt64Map(c.byTier),
		ByThinking: copyInt64Map(c.byThinking),
		Decisions:  c.decisions,
		Applied:    c.applied,
		Shadow:     c.shadow,
		LastTier:   c.lastTier,
		LastMode:   c.lastMode,
		LastThink:  c.lastThink,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
}

// RecordCostRouteDecision records a recommend decision when mode is shadow or on.
func RecordCostRouteDecision(d CostRouteDecision) {
	if !CostRouteSurfaces(d.Mode) {
		return
	}
	c := globalCostRouteStats
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureLoaded()
	tier := string(d.Tier)
	if tier == "" {
		tier = "unknown"
	}
	if c.byTier == nil {
		c.byTier = make(map[string]int64)
	}
	c.byTier[tier]++
	c.decisions++
	if d.Mode == CostRouteShadow {
		c.shadow++
	}
	think := string(d.Thinking)
	if think != "" {
		if c.byThinking == nil {
			c.byThinking = make(map[string]int64)
		}
		c.byThinking[think]++
	}
	c.lastTier = tier
	c.lastMode = string(d.Mode)
	c.lastThink = think
	c.dirty = true
	c.schedulePersistLocked()
}

// RecordCostRouteApplied increments the applied counter when mode=on actually
// selected a model/thinking policy.
func RecordCostRouteApplied(applied bool) {
	if !applied {
		return
	}
	c := globalCostRouteStats
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureLoaded()
	c.applied++
	c.dirty = true
	c.schedulePersistLocked()
}

// schedulePersistLocked coalesces disk writes under bursty routing. Caller holds mu.
func (c *costRouteCounters) schedulePersistLocked() {
	if c.persistTimer != nil {
		return
	}
	c.persistTimer = time.AfterFunc(costRoutePersistDebounce, func() {
		_ = FlushCostRouteStats()
	})
}

// FlushCostRouteStats forces any pending debounced durable write now.
// Holds costRouteFileMu across snap→write so concurrent flushes serialize.
func FlushCostRouteStats() error {
	// Lock order: file → counters (Record only takes counters mu).
	costRouteFileMu.Lock()
	defer costRouteFileMu.Unlock()

	c := globalCostRouteStats
	c.mu.Lock()
	if c.persistTimer != nil {
		c.persistTimer.Stop()
		c.persistTimer = nil
	}
	if !c.dirty {
		c.mu.Unlock()
		return nil
	}
	snap := c.toInstanceSnap()
	c.dirty = false
	c.mu.Unlock()

	if err := writeCostRouteInstanceLocked(snap); err != nil {
		c.reDirtyAndReschedule(err)
		return err
	}
	return nil
}

// writeCostRouteInstanceLocked performs RMW of this process slot. Caller holds costRouteFileMu.
func writeCostRouteInstanceLocked(snap CostRouteInstanceSnap) error {
	path := CostRouteStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")
	file := CostRouteDiskFile{
		Date:      today,
		Instances: map[string]CostRouteInstanceSnap{},
	}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		var existing CostRouteDiskFile
		if json.Unmarshal(data, &existing) == nil {
			sameDay := existing.Date == today || existing.Date == ""
			if sameDay && len(existing.Instances) > 0 {
				file = existing
				if file.Instances == nil {
					file.Instances = make(map[string]CostRouteInstanceSnap)
				}
			} else if sameDay && (existing.Decisions > 0 || existing.Applied > 0 || existing.Shadow > 0) {
				// Migrate legacy flat (with or without date) → "legacy" slot.
				file.Instances = map[string]CostRouteInstanceSnap{
					"legacy": {
						ByTier:     copyInt64Map(existing.ByTier),
						ByThinking: copyInt64Map(existing.ByThinking),
						Decisions:  existing.Decisions,
						Applied:    existing.Applied,
						Shadow:     existing.Shadow,
						LastTier:   existing.LastTier,
						LastMode:   existing.LastMode,
						LastThink:  existing.LastThink,
						UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
					},
				}
			}
		}
	}
	// Strip legacy top-level counters from written shape.
	file.Date = today
	file.ByTier = nil
	file.ByThinking = nil
	file.Decisions = 0
	file.Applied = 0
	file.Shadow = 0
	file.LastTier = ""
	file.LastMode = ""
	file.LastThink = ""
	file.Instances[costProcessInstanceID()] = snap
	file.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// reDirtyAndReschedule marks stats dirty again and arms debounce after a failed write.
func (c *costRouteCounters) reDirtyAndReschedule(err error) {
	_ = err
	c.mu.Lock()
	c.dirty = true
	c.schedulePersistLocked()
	c.mu.Unlock()
}

func copyInt64Map(src map[string]int64) map[string]int64 {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]int64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func sumCostRouteInstances(instances map[string]CostRouteInstanceSnap) CostRouteStats {
	out := CostRouteStats{
		ByTier:     make(map[string]int64),
		ByThinking: make(map[string]int64),
		Path:       CostRouteStatsPath(),
	}
	var lastAt string
	for _, inst := range instances {
		out.Decisions += inst.Decisions
		out.Applied += inst.Applied
		out.Shadow += inst.Shadow
		out.Instances++
		for k, v := range inst.ByTier {
			out.ByTier[k] += v
		}
		for k, v := range inst.ByThinking {
			out.ByThinking[k] += v
		}
		// Prefer newest last_* by UpdatedAt when available.
		if inst.UpdatedAt >= lastAt && (inst.LastTier != "" || inst.LastMode != "") {
			lastAt = inst.UpdatedAt
			out.LastTier = inst.LastTier
			out.LastMode = inst.LastMode
			out.LastThink = inst.LastThink
		}
	}
	if len(out.ByTier) == 0 {
		out.ByTier = nil
	}
	if len(out.ByThinking) == 0 {
		out.ByThinking = nil
	}
	return out
}

// LoadCostRouteStats returns today's fleet sum across host-pid slots.
// Overlays this process's in-memory counters (including unflushed dirty).
func LoadCostRouteStats() CostRouteStats {
	today := time.Now().Format("2006-01-02")
	instances := map[string]CostRouteInstanceSnap{}

	if data, err := os.ReadFile(CostRouteStatsPath()); err == nil && len(data) > 0 {
		var file CostRouteDiskFile
		if json.Unmarshal(data, &file) == nil {
			if len(file.Instances) > 0 && file.Date == today {
				for k, v := range file.Instances {
					instances[k] = v
				}
			} else if file.Date == today || file.Date == "" {
				// Legacy flat → single synthetic slot for read path.
				if file.Decisions > 0 || file.Applied > 0 || file.Shadow > 0 {
					instances["legacy"] = CostRouteInstanceSnap{
						ByTier:     copyInt64Map(file.ByTier),
						ByThinking: copyInt64Map(file.ByThinking),
						Decisions:  file.Decisions,
						Applied:    file.Applied,
						Shadow:     file.Shadow,
						LastTier:   file.LastTier,
						LastMode:   file.LastMode,
						LastThink:  file.LastThink,
					}
				}
			}
		}
	}

	// Overlay this process memory (authoritative for our host-pid).
	c := globalCostRouteStats
	c.mu.Lock()
	c.ensureLoaded()
	id := costProcessInstanceID()
	if c.decisions > 0 || c.applied > 0 || c.shadow > 0 || c.dirty {
		instances[id] = c.toInstanceSnap()
	}
	c.mu.Unlock()

	out := sumCostRouteInstances(instances)
	return out
}

// FormatCostRouteStatsLine is a one-line operator summary.
func FormatCostRouteStatsLine() string {
	s := LoadCostRouteStats()
	if s.Decisions <= 0 {
		return ""
	}
	line := fmt.Sprintf("cost-route decisions=%d applied=%d shadow=%d last=%s/%s",
		s.Decisions, s.Applied, s.Shadow, s.LastTier, s.LastMode)
	if s.Instances > 1 {
		line += fmt.Sprintf(" instances=%d", s.Instances)
	}
	return line
}

// ResetCostRouteStatsForTest clears process counters (tests only).
func ResetCostRouteStatsForTest() {
	c := globalCostRouteStats
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.persistTimer != nil {
		c.persistTimer.Stop()
		c.persistTimer = nil
	}
	c.byTier = make(map[string]int64)
	c.byThinking = make(map[string]int64)
	c.decisions = 0
	c.applied = 0
	c.shadow = 0
	c.lastTier = ""
	c.lastMode = ""
	c.lastThink = ""
	c.dirty = false
	c.loaded = true // skip disk so tests stay isolated
}

// CostOpsHeartbeatStat returns a compact corelib snapshot for Hub
// machine.heartbeat (cost-route counters + local daily fleet). Nil when empty.
func CostOpsHeartbeatStat() *corelib.CostOpsStat {
	rs := LoadCostRouteStats()
	fleet := LoadCostDailyFleet()
	mode := ResolveCostRouteMode()
	var routeSummary, dailySummary string
	if rs.Decisions > 0 {
		routeSummary = FormatCostRouteStatsLine()
	}
	if fleet.Calls > 0 || fleet.CostUSD > 0 {
		dailySummary = fmt.Sprintf("llm-cost today=$%.4f calls=%d instances=%d",
			fleet.CostUSD, fleet.Calls, fleet.Instances)
	}
	out := corelib.CostOpsStat{
		RouteDecisions: rs.Decisions,
		RouteApplied:   rs.Applied,
		RouteShadow:    rs.Shadow,
		LastTier:       rs.LastTier,
		LastMode:       rs.LastMode,
		RouteSummary:   routeSummary,
		DailyCostUSD:   fleet.CostUSD,
		DailyCalls:     fleet.Calls,
		DailyInstances: fleet.Instances,
		DailySummary:   dailySummary,
		CostRouteMode:  string(mode),
	}
	if out.Empty() {
		return nil
	}
	return &out
}
