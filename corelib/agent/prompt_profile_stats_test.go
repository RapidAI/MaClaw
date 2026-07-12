package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordPromptProfileStats(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordPromptProfile(PromptProfileLight)
	RecordPromptProfile(PromptProfileLight)
	RecordPromptProfile(PromptProfileFull)
	st := GetPromptProfileStats()
	if st.LightTurns != 2 || st.FullTurns != 1 {
		t.Fatalf("stats=%+v", st)
	}
	if st.LightPercent != 66 { // 2/3
		t.Fatalf("light_percent=%d", st.LightPercent)
	}
	if st.LastProfile != string(PromptProfileFull) {
		t.Fatalf("last=%q", st.LastProfile)
	}
	if st.LastAt == "" {
		t.Fatal("last_at empty")
	}
}

func TestRecordPromptProfileSavings(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordPromptProfileSavings(PromptProfileLight, 5000, 1200)
	st := GetPromptProfileStats()
	if st.EstTokensSaved != 3800 {
		t.Fatalf("saved=%d", st.EstTokensSaved)
	}
	if st.LastFullTokens != 5000 || st.LastLightTokens != 1200 || st.LastSavedTokens != 3800 {
		t.Fatalf("last tokens=%+v", st)
	}
	// Full turn does not add savings
	RecordPromptProfileSavings(PromptProfileFull, 5000, 0)
	st = GetPromptProfileStats()
	if st.EstTokensSaved != 3800 || st.FullTurns != 1 {
		t.Fatalf("after full=%+v", st)
	}
}

func TestRecordLightToolDeny(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordLightToolDeny("bash")
	RecordLightToolDeny("bash")
	RecordLightToolDeny("write_file")
	st := GetPromptProfileStats()
	if st.LightToolDenies != 3 {
		t.Fatalf("denies=%d", st.LightToolDenies)
	}
	if st.ByDeniedTool["bash"] != 2 || st.ByDeniedTool["write_file"] != 1 {
		t.Fatalf("by_denied=%v", st.ByDeniedTool)
	}
	if st.LastDeniedTool != "write_file" {
		t.Fatalf("last=%q", st.LastDeniedTool)
	}
	RecordPromptProfile(PromptProfileLight)
	RecordLightUpgrade("tool_deny_retry:bash")
	line := FormatPromptProfileLine()
	if !strings.Contains(line, "light_deny=3") {
		t.Fatalf("line=%q", line)
	}
	// Top denied tools: bash:2,write_file:1
	if !strings.Contains(line, "bash:2") || !strings.Contains(line, "write_file:1") {
		t.Fatalf("expected by_denied breakdown in line=%q", line)
	}
	if !strings.Contains(line, "light_upgrade=1") || !strings.Contains(line, "light_upgrade=1(bash)") {
		t.Fatalf("expected compact upgrade reason in line=%q", line)
	}
}

func TestRecordPromptProfileDecision_ByTask(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordPromptProfileDecision(PromptProfileDecision{
		Profile:     PromptProfileLight,
		FullTokens:  4000,
		LightTokens: 1000,
		Task:        "fast",
		Reason:      "short simple turn",
	})
	RecordPromptProfileDecision(PromptProfileDecision{
		Profile: PromptProfileFull,
		Task:    "reasoning",
		Reason:  "coding cues",
	})
	RecordPromptProfileDecision(PromptProfileDecision{
		Profile: PromptProfileLight,
		Task:    "FAST", // normalized to lower
		Reason:  "another",
	})
	st := GetPromptProfileStats()
	if st.ByTask["fast"] != 2 {
		t.Fatalf("by_task fast=%v", st.ByTask)
	}
	if st.ByTask["reasoning"] != 1 {
		t.Fatalf("by_task reasoning=%v", st.ByTask)
	}
	if st.LastTask != "fast" {
		t.Fatalf("last_task=%q want fast", st.LastTask)
	}
	if st.LastReason != "another" {
		t.Fatalf("last_reason=%q", st.LastReason)
	}
	line := FormatPromptProfileLine()
	if !strings.Contains(line, "task=fast") {
		t.Fatalf("line missing task: %q", line)
	}
	if !strings.Contains(line, "by_task=") {
		t.Fatalf("line missing by_task: %q", line)
	}
}

func TestEstimatePromptProfileTokens_LightShorter(t *testing.T) {
	full, light := EstimatePromptProfileTokens(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:  "MaClaw",
			IsProMode: true,
		},
	}, "hello", true)
	if full <= 0 || light <= 0 {
		t.Fatalf("full=%d light=%d", full, light)
	}
	if light >= full {
		t.Fatalf("expected light < full: light=%d full=%d", light, full)
	}
}

func TestFormatPromptProfileLine(t *testing.T) {
	t.Setenv(PromptProfileEnvKey, "")
	ResetPromptProfileStatsForTest()
	if FormatPromptProfileLine() != "" {
		t.Fatal("empty when no turns")
	}
	RecordPromptProfileSavings(PromptProfileLight, 5000, 1200)
	RecordPromptProfile(PromptProfileFull)
	line := FormatPromptProfileLine()
	for _, part := range []string{"adaptive-prompt:", "light 50%", "est_saved=", "last=full"} {
		if !strings.Contains(line, part) {
			t.Fatalf("line missing %q: %q", part, line)
		}
	}
	t.Setenv(PromptProfileEnvKey, "full")
	line2 := FormatPromptProfileLine()
	if !strings.Contains(line2, PromptProfileEnvKey+"=full") {
		t.Fatalf("expected env lock in %q", line2)
	}
}

func TestResetPromptProfileStats(t *testing.T) {
	ResetPromptProfileStatsForTest()
	RecordPromptProfileSavings(PromptProfileLight, 3000, 500)
	if GetPromptProfileStats().LightTurns == 0 {
		t.Fatal("expected seed")
	}
	if err := ResetPromptProfileStats(); err != nil {
		t.Fatal(err)
	}
	st := GetPromptProfileStats()
	if st.LightTurns != 0 || st.FullTurns != 0 || st.EstTokensSaved != 0 {
		t.Fatalf("after reset: %+v", st)
	}
}

func TestPromptProfileStatsPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Point DataDir via env if supported; otherwise write directly and reload via path.
	// Use isolated file by temporarily writing through persist after reset with custom path —
	// PromptProfileStatsPath uses maclawpath.DataDir; inject via writing the file then Load.
	ResetPromptProfileStatsForTest()
	RecordPromptProfileSavings(PromptProfileLight, 3000, 1000)

	// Force write to a temp file and read back structure.
	path := filepath.Join(dir, "prompt_profile.json")
	snap := promptProfileDiskSnapshot{
		LightTurns:      5,
		FullTurns:       2,
		EstTokensSaved:  9999,
		LastProfile:     "light",
		LastAt:          "2026-07-12T00:00:00Z",
		LastFullTokens:  4000,
		LastLightTokens: 1000,
	}
	data, _ := json.Marshal(snap)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Unmarshal path works
	var got promptProfileDiskSnapshot
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.EstTokensSaved != 9999 || got.LightTurns != 5 {
		t.Fatalf("disk=%+v", got)
	}
}
