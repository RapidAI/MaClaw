package llm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func TestCostOpsExportMerge(t *testing.T) {
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir("") })
	ResetCostRouteStatsForTest()

	t.Setenv(CostRouteEnvKey, "shadow")
	_ = DecideCostRoute(TaskFast, ClassifyHints{}, "exp")
	ct := NewCostTracker(0)
	ct.Record("gpt-4o-mini", 1_000_000, 0)
	// BuildCostOpsExport flushes debounced writers.

	exp := BuildCostOpsExport()
	if exp.CostRoute.Decisions < 1 {
		t.Fatalf("export route=%+v", exp.CostRoute)
	}
	if exp.DailyFleet.Calls < 1 {
		t.Fatalf("export fleet=%+v", exp.DailyFleet)
	}
	path := filepath.Join(dir, "e1.json")
	if err := WriteCostOpsExport(path, exp); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCostOpsExport(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CostRoute.Decisions != exp.CostRoute.Decisions {
		t.Fatalf("loaded=%+v", loaded)
	}

	// Second synthetic export
	exp2 := CostOpsExport{
		SchemaVersion: CostOpsExportSchemaVersion,
		Host:          "other",
		CostRoute:     CostRouteStats{Decisions: 3, Applied: 1, Shadow: 2, ByTier: map[string]int64{"c2": 3}},
		DailyFleet:    CostDailyFleetView{CostUSD: 1.0, Calls: 4, Instances: 1},
	}
	path2 := filepath.Join(dir, "e2.json")
	if err := WriteCostOpsExport(path2, exp2); err != nil {
		t.Fatal(err)
	}
	merged := MergeCostOpsExports([]CostOpsExport{loaded, exp2})
	if merged.SourceCount != 2 {
		t.Fatalf("count=%d", merged.SourceCount)
	}
	if merged.RouteDecisions < 4 {
		t.Fatalf("decisions=%d", merged.RouteDecisions)
	}
	if merged.DailyCalls < 5 {
		t.Fatalf("calls=%d", merged.DailyCalls)
	}
	if merged.Summary == "" {
		t.Fatal("empty summary")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
