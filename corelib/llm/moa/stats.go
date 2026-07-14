package moa

// Lightweight MoA runtime counters (process + durable snapshot under data/stats/moa.json).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// Stats is a snapshot of MoA fan-out activity.
type Stats struct {
	Fanouts      int64             `json:"fanouts"`
	RefOK        int64             `json:"ref_ok"`
	RefFail      int64             `json:"ref_fail"`
	TotalRefMS   int64             `json:"total_ref_ms"`
	ByPreset     map[string]int64  `json:"by_preset,omitempty"`
	LastPreset   string            `json:"last_preset,omitempty"`
	LastMS       int64             `json:"last_ms,omitempty"`
	LastRefOK    int               `json:"last_ref_ok,omitempty"`
	LastRefFail  int               `json:"last_ref_fail,omitempty"`
	UpdatedAt    string            `json:"updated_at,omitempty"`
	Path         string            `json:"path,omitempty"`
}

// StatsPath is the durable MoA stats file.
func StatsPath() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "moa.json")
}

type moaCounters struct {
	mu         sync.Mutex
	fanouts    int64
	refOK      int64
	refFail    int64
	totalRefMS int64
	byPreset   map[string]int64
	lastPreset string
	lastMS     int64
	lastOK     int
	lastFail   int
	loaded     bool
	dirty      bool
	timer      *time.Timer
}

var globalMoAStats = &moaCounters{byPreset: make(map[string]int64)}

const moaStatsDebounce = 500 * time.Millisecond

// RecordFanOut records one reference fan-out wave.
func RecordFanOut(preset string, refOK, refFail int, duration time.Duration) {
	ms := duration.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	globalMoAStats.mu.Lock()
	defer globalMoAStats.mu.Unlock()
	globalMoAStats.ensureLoadedLocked()
	if globalMoAStats.byPreset == nil {
		globalMoAStats.byPreset = make(map[string]int64)
	}
	globalMoAStats.fanouts++
	globalMoAStats.refOK += int64(refOK)
	globalMoAStats.refFail += int64(refFail)
	globalMoAStats.totalRefMS += ms
	if preset == "" {
		preset = "default"
	}
	globalMoAStats.byPreset[preset]++
	globalMoAStats.lastPreset = preset
	globalMoAStats.lastMS = ms
	globalMoAStats.lastOK = refOK
	globalMoAStats.lastFail = refFail
	globalMoAStats.dirty = true
	globalMoAStats.schedulePersistLocked()
}

// LoadStats returns a copy of current counters.
func LoadStats() Stats {
	globalMoAStats.mu.Lock()
	defer globalMoAStats.mu.Unlock()
	globalMoAStats.ensureLoadedLocked()
	return globalMoAStats.snapshotLocked()
}

// FormatStatsLine is a one-line doctor/CLI summary; empty if no fan-outs.
func FormatStatsLine() string {
	st := LoadStats()
	if st.Fanouts == 0 {
		return ""
	}
	avg := int64(0)
	if st.Fanouts > 0 {
		avg = st.TotalRefMS / st.Fanouts
	}
	return fmt.Sprintf("moa fanouts=%d ref_ok=%d ref_fail=%d avg_ms=%d last=%s/%dms",
		st.Fanouts, st.RefOK, st.RefFail, avg, st.LastPreset, st.LastMS)
}

// ResetStatsForTest clears counters (tests only).
func ResetStatsForTest() {
	globalMoAStats.mu.Lock()
	defer globalMoAStats.mu.Unlock()
	if globalMoAStats.timer != nil {
		globalMoAStats.timer.Stop()
		globalMoAStats.timer = nil
	}
	globalMoAStats.fanouts = 0
	globalMoAStats.refOK = 0
	globalMoAStats.refFail = 0
	globalMoAStats.totalRefMS = 0
	globalMoAStats.byPreset = make(map[string]int64)
	globalMoAStats.lastPreset = ""
	globalMoAStats.lastMS = 0
	globalMoAStats.lastOK = 0
	globalMoAStats.lastFail = 0
	globalMoAStats.loaded = true
	globalMoAStats.dirty = false
}

// FlushStats writes dirty counters immediately.
func FlushStats() error {
	globalMoAStats.mu.Lock()
	defer globalMoAStats.mu.Unlock()
	return globalMoAStats.persistLocked()
}

func (c *moaCounters) ensureLoadedLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	data, err := os.ReadFile(StatsPath())
	if err != nil || len(data) == 0 {
		return
	}
	var st Stats
	if json.Unmarshal(data, &st) != nil {
		return
	}
	// Only reuse same calendar day to avoid unbounded growth without rotation.
	today := time.Now().Format("2006-01-02")
	if st.UpdatedAt != "" && len(st.UpdatedAt) >= 10 && st.UpdatedAt[:10] != today {
		return
	}
	c.fanouts = st.Fanouts
	c.refOK = st.RefOK
	c.refFail = st.RefFail
	c.totalRefMS = st.TotalRefMS
	c.byPreset = st.ByPreset
	if c.byPreset == nil {
		c.byPreset = make(map[string]int64)
	}
	c.lastPreset = st.LastPreset
	c.lastMS = st.LastMS
	c.lastOK = st.LastRefOK
	c.lastFail = st.LastRefFail
}

func (c *moaCounters) snapshotLocked() Stats {
	by := make(map[string]int64, len(c.byPreset))
	for k, v := range c.byPreset {
		by[k] = v
	}
	return Stats{
		Fanouts:     c.fanouts,
		RefOK:       c.refOK,
		RefFail:     c.refFail,
		TotalRefMS:  c.totalRefMS,
		ByPreset:    by,
		LastPreset:  c.lastPreset,
		LastMS:      c.lastMS,
		LastRefOK:   c.lastOK,
		LastRefFail: c.lastFail,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		Path:        StatsPath(),
	}
}

func (c *moaCounters) schedulePersistLocked() {
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(moaStatsDebounce, func() {
		globalMoAStats.mu.Lock()
		defer globalMoAStats.mu.Unlock()
		_ = globalMoAStats.persistLocked()
	})
}

func (c *moaCounters) persistLocked() error {
	if !c.dirty {
		return nil
	}
	st := c.snapshotLocked()
	dir := filepath.Dir(st.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, st.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	c.dirty = false
	return nil
}
