package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptProfileExport(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordPromptProfileDecision(PromptProfileDecision{
		Profile:     PromptProfileLight,
		FullTokens:  4000,
		LightTokens: 1000,
		Task:        "fast",
	})
	RecordLightToolDeny("bash")
	exp := BuildPromptProfileExport()
	if exp.SchemaVersion != PromptProfileExportSchemaVersion {
		t.Fatalf("schema=%d", exp.SchemaVersion)
	}
	if exp.ExportedAt == "" {
		t.Fatal("exported_at empty")
	}
	if exp.Stats.LightTurns != 1 || exp.Stats.EstTokensSaved != 3000 {
		t.Fatalf("stats=%+v", exp.Stats)
	}
	if exp.Stats.LightToolDenies != 1 {
		t.Fatalf("denies=%d", exp.Stats.LightToolDenies)
	}
	if exp.Summary == "" {
		t.Fatal("summary empty")
	}
	hb := AdaptivePromptHeartbeatStat()
	if hb == nil || hb.LightTurns != 1 || hb.EstTokensSaved != 3000 {
		t.Fatalf("heartbeat stat=%#v", hb)
	}
}

func TestAdaptivePromptHeartbeatStat_Empty(t *testing.T) {
	ResetPromptProfileStatsForTest()
	if AdaptivePromptHeartbeatStat() != nil {
		t.Fatal("expected nil when no stats")
	}
}

func TestWriteLoadPromptProfileExport(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordPromptProfile(PromptProfileLight)
	dir := t.TempDir()
	path := filepath.Join(dir, "exp.json")
	exp := BuildPromptProfileExport()
	if err := WritePromptProfileExport(path, exp); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPromptProfileExport(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Stats.LightTurns != 1 {
		t.Fatalf("loaded=%+v", loaded.Stats)
	}
	if loaded.SourcePath != path {
		t.Fatalf("source=%q", loaded.SourcePath)
	}
}

func TestMergePromptProfileExports(t *testing.T) {
	a := PromptProfileExport{
		SchemaVersion: 1,
		ExportedAt:    "2026-01-01T00:00:00Z",
		Host:          "host-a",
		SourcePath:    "a.json",
		Stats: PromptProfileStats{
			LightTurns:      2,
			FullTurns:       1,
			EstTokensSaved:  1000,
			ByTask:          map[string]int64{"fast": 2, "reasoning": 1},
			LightToolDenies: 1,
			LastDeniedTool:  "bash",
		},
	}
	b := PromptProfileExport{
		SchemaVersion: 1,
		ExportedAt:    "2026-01-02T00:00:00Z",
		Host:          "host-b",
		SourcePath:    "b.json",
		Stats: PromptProfileStats{
			LightTurns:        1,
			FullTurns:         3,
			EstTokensSaved:    500,
			ByTask:            map[string]int64{"fast": 1, "summary": 1},
			LightUpgrades:     2,
			LastUpgradeReason: "tool_deny_retry:bash",
			LastProfile:       "full",
			LastTask:          "reasoning",
		},
	}
	merged := MergePromptProfileExports([]PromptProfileExport{a, b})
	if merged.SourceCount != 2 {
		t.Fatalf("count=%d", merged.SourceCount)
	}
	if merged.Stats.LightTurns != 3 || merged.Stats.FullTurns != 4 {
		t.Fatalf("turns=%+v", merged.Stats)
	}
	if merged.Stats.EstTokensSaved != 1500 {
		t.Fatalf("saved=%d", merged.Stats.EstTokensSaved)
	}
	if merged.Stats.ByTask["fast"] != 3 || merged.Stats.ByTask["summary"] != 1 {
		t.Fatalf("by_task=%v", merged.Stats.ByTask)
	}
	if merged.Stats.LightToolDenies != 1 || merged.Stats.LightUpgrades != 2 {
		t.Fatalf("deny/upgrade=%+v", merged.Stats)
	}
	// Newest export is b (2026-01-02)
	if merged.Stats.LastUpgradeReason != "tool_deny_retry:bash" {
		t.Fatalf("last upgrade=%q", merged.Stats.LastUpgradeReason)
	}
	if merged.Stats.LightPercent != 42 { // 3/7
		t.Fatalf("light%%=%d", merged.Stats.LightPercent)
	}
	if len(merged.Hosts) != 2 {
		t.Fatalf("hosts=%v", merged.Hosts)
	}
	if !strings.Contains(merged.Summary, "light") {
		t.Fatalf("summary=%q", merged.Summary)
	}
}

func TestLoadPromptProfileExport_RawStatsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.json")
	raw := PromptProfileStats{LightTurns: 5, FullTurns: 5, EstTokensSaved: 100}
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	exp, err := LoadPromptProfileExport(path)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Stats.LightTurns != 5 {
		t.Fatalf("stats=%+v", exp.Stats)
	}
}
