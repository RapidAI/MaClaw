package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func TestCostRouteStats_RecordAndLoad(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ResetCostRouteStatsForTest()

	t.Setenv(CostRouteEnvKey, "shadow")
	d := DecideCostRoute(TaskFast, ClassifyHints{}, "test")
	if d.Tier != CostTierC0 {
		t.Fatalf("tier=%s", d.Tier)
	}
	stats := LoadCostRouteStats()
	if stats.Decisions < 1 || stats.Shadow < 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if stats.ByTier["c0"] < 1 {
		t.Fatalf("by_tier=%v", stats.ByTier)
	}
	if line := FormatCostRouteStatsLine(); line == "" {
		t.Fatal("expected summary line")
	}

	primary := corelib.MaclawLLMConfig{URL: "http://p", Model: "primary", Key: "k"}
	aux := corelib.AuxiliaryLLMConfig{URL: "http://a", Model: "aux", Key: "k"}
	_, applied, _, _ := ApplyCostTierConfig(nil, primary, aux, CostTierC0, CostRouteOn)
	if !applied {
		t.Fatal("expected applied")
	}
	stats = LoadCostRouteStats()
	if stats.Applied < 1 {
		t.Fatalf("applied=%d", stats.Applied)
	}
	if err := FlushCostRouteStats(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := os.Stat(CostRouteStatsPath()); err != nil {
		t.Fatalf("path: %v", err)
	}
	// Disk should be multi-instance shape.
	raw, err := os.ReadFile(CostRouteStatsPath())
	if err != nil {
		t.Fatal(err)
	}
	var disk CostRouteDiskFile
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Instances) < 1 {
		t.Fatalf("expected instances map, got %+v", disk)
	}
	if hb := CostOpsHeartbeatStat(); hb == nil || hb.RouteDecisions < 1 {
		t.Fatalf("heartbeat=%+v", hb)
	}
}

func TestCostRouteStats_MultiInstanceFleetSum(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ResetCostRouteStatsForTest()

	today := timeNowDate()
	path := CostRouteStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file := CostRouteDiskFile{
		Date: today,
		Instances: map[string]CostRouteInstanceSnap{
			"other-1": {Decisions: 5, Applied: 2, Shadow: 3, ByTier: map[string]int64{"c0": 5}},
			"other-2": {Decisions: 3, Applied: 1, Shadow: 2, ByTier: map[string]int64{"c2": 3}},
		},
	}
	raw, _ := json.Marshal(file)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	// Fresh process memory (0) but fleet load sums disk slots.
	// loaded=false would re-read; Reset set loaded=true — force reload.
	globalCostRouteStats.mu.Lock()
	globalCostRouteStats.loaded = false
	globalCostRouteStats.mu.Unlock()

	sum := LoadCostRouteStats()
	if sum.Decisions != 8 || sum.Applied != 3 || sum.Shadow != 5 {
		t.Fatalf("sum=%+v", sum)
	}
	if sum.ByTier["c0"] != 5 || sum.ByTier["c2"] != 3 {
		t.Fatalf("by_tier=%v", sum.ByTier)
	}
	if sum.Instances != 2 {
		t.Fatalf("instances=%d", sum.Instances)
	}
}

func TestCostRouteStats_WriteDoesNotClobberOtherSlot(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ResetCostRouteStatsForTest()

	today := timeNowDate()
	path := CostRouteStatsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	file := CostRouteDiskFile{
		Date: today,
		Instances: map[string]CostRouteInstanceSnap{
			"sibling-pid": {Decisions: 10, Applied: 4, Shadow: 6, ByTier: map[string]int64{"c1": 10}},
		},
	}
	raw, _ := json.Marshal(file)
	_ = os.WriteFile(path, raw, 0o644)

	globalCostRouteStats.mu.Lock()
	globalCostRouteStats.loaded = false
	globalCostRouteStats.mu.Unlock()

	t.Setenv(CostRouteEnvKey, "on")
	_ = DecideCostRoute(TaskFast, ClassifyHints{}, "me")
	if err := FlushCostRouteStats(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk CostRouteDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Instances["sibling-pid"].Decisions != 10 {
		t.Fatalf("sibling clobbered: %+v", disk.Instances)
	}
	if len(disk.Instances) < 2 {
		t.Fatalf("expected our slot + sibling: %+v", disk.Instances)
	}
}

func timeNowDate() string {
	return time.Now().Format("2006-01-02")
}

func TestCostRouteStats_LegacyFlatMigratesWithoutDate(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ResetCostRouteStatsForTest()

	path := CostRouteStatsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	// Pre-multi-instance shape without date field.
	legacy := map[string]any{
		"decisions": 7,
		"applied":   2,
		"shadow":    5,
		"by_tier":   map[string]int64{"c0": 7},
	}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	globalCostRouteStats.mu.Lock()
	globalCostRouteStats.loaded = false
	globalCostRouteStats.mu.Unlock()

	t.Setenv(CostRouteEnvKey, "shadow")
	_ = DecideCostRoute(TaskFast, ClassifyHints{}, "x")
	if err := FlushCostRouteStats(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk CostRouteDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Instances["legacy"].Decisions != 7 {
		t.Fatalf("legacy lost: %+v", disk.Instances)
	}
	if len(disk.Instances) < 2 {
		t.Fatalf("expected legacy + this process: %+v", disk.Instances)
	}
	sum := LoadCostRouteStats()
	if sum.Decisions < 8 { // 7 legacy + at least 1 new
		t.Fatalf("sum=%+v", sum)
	}
}
