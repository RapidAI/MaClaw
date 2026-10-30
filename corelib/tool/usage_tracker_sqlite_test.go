package tool

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageTrackerSQLiteImportsJSONAndSearchesByTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_usage.json")
	legacy := []byte(`{"records":[{"tool_name":"web_search","query_tokens":["lanzhou","weather"],"success":true,"follow_up":"continue","timestamp":"2026-10-01T00:00:00Z","task_type":"weather"}]}`)
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	found := tracker.Search("web_search", "lanzhou", 5)
	if len(found) != 1 || !found[0].Success || found[0].TaskType != "weather" {
		t.Fatalf("imported search = %+v", found)
	}
	tracker.RecordExperience(ToolExperience{
		ToolName: "bash", QueryTokens: []string{"compile", "lan_zhou"}, Success: false,
		FollowUp: "retry", TaskType: "coding", Timestamp: time.Now().UTC(),
	})
	if err := tracker.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Search("bash", "lan_zhou", 5); len(got) != 1 || got[0].FollowUp != "retry" {
		t.Fatalf("reopened search = %+v", got)
	}
	if got := reopened.Search("web_search", "weather", 5); len(got) != 1 {
		t.Fatalf("legacy row missing after reopen: %+v", got)
	}
	if _, err := os.Stat(usageDBPath(path)); err != nil {
		t.Fatalf("usage db: %v", err)
	}
}

func TestUsageTrackerSQLiteSurvivesMissingOrBrokenLegacyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_usage.json")
	legacy := []byte(`{"records":[{"tool_name":"web_search","query_tokens":["lanzhou","weather"],"success":true,"timestamp":"2026-10-01T00:00:00Z","task_type":"weather"}]}`)
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.db.Exec(`DELETE FROM usage_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	kept, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := kept.Search("web_search", "lanzhou", 5); len(got) != 1 {
		t.Fatalf("missing json hid sqlite: %+v", got)
	}

	if _, err := kept.db.Exec(`DELETE FROM usage_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := broken.Search("web_search", "weather", 5); len(got) != 1 {
		t.Fatalf("broken json hid sqlite: %+v", got)
	}
}

func TestUsageTrackerSQLiteDropsLegacyFileAfterImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_usage.json")
	legacy := []byte(`{"records":[{"tool_name":"web_search","query_tokens":["lanzhou","weather"],"success":true,"timestamp":"2026-10-01T00:00:00Z","task_type":"weather"}]}`)
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy file remains: %v", err)
	}
	tracker.RecordExperience(ToolExperience{
		ToolName: "bash", QueryTokens: []string{"compile", "lan_zhou"}, Success: true,
		TaskType: "coding", Timestamp: time.Now().UTC(),
	})
	if err := tracker.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.db.Exec(`DELETE FROM usage_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Search("web_search", "lanzhou", 5); len(got) != 1 {
		t.Fatalf("legacy row = %+v", got)
	}
	if got := reopened.Search("bash", "lan_zhou", 5); len(got) != 1 {
		t.Fatalf("later row = %+v", got)
	}
}

func TestUsageTrackerSQLiteTrimDropsOldRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_usage.json")
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	tracker.maxItems = 2
	record := func(token string) {
		tracker.RecordExperience(ToolExperience{
			ToolName: "bash", QueryTokens: []string{token}, Success: true,
			FollowUp: "continue", Timestamp: time.Now().UTC(),
		})
	}
	record("one")
	record("two")
	if err := tracker.Save(); err != nil {
		t.Fatal(err)
	}
	record("three")
	if err := tracker.Save(); err != nil {
		t.Fatal(err)
	}
	n, err := usageRowCount(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
	reopened, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Search("bash", "three", 5); len(got) != 1 {
		t.Fatalf("three = %+v", got)
	}
	if got := reopened.Search("bash", "one", 5); len(got) != 0 {
		t.Fatalf("trimmed row still searchable: %+v", got)
	}
}

func TestUsageTrackerSearchUsesLiveRing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_usage.json")
	tracker, err := NewUsageTracker(path)
	if err != nil {
		t.Fatal(err)
	}
	tracker.maxItems = 2
	record := func(token string) {
		tracker.RecordExperience(ToolExperience{
			ToolName: "bash", QueryTokens: []string{"shared", token}, Success: true,
			FollowUp: "continue", Timestamp: time.Now().UTC(),
		})
	}
	record("one")
	record("two")
	if err := tracker.Save(); err != nil {
		t.Fatal(err)
	}
	record("three")
	found := tracker.Search("bash", "shared", 10)
	got := map[string]bool{}
	for _, rec := range found {
		for _, token := range rec.QueryTokens {
			got[token] = true
		}
	}
	if got["one"] || !got["two"] || !got["three"] {
		t.Fatalf("live ring search = %+v", found)
	}
}
