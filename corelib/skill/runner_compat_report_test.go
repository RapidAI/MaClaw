package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestAssessRunnerCompatibility_ShellToolBecomesBash(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "Wget Tool",
		Steps: []corelib.NLSkillStep{{
			Action: "shell_tool",
			Params: map[string]interface{}{"command": "wget -O o.pdf u"},
		}},
	}
	report := AssessRunnerCompatibility(entry, RunnerBackendGUI)
	if !report.Runnable {
		t.Fatalf("shell_tool should normalize to bash: %#v", report)
	}
	if !report.HasBash || report.SupportedSteps != 1 {
		t.Fatalf("report=%#v", report)
	}
	if report.SuggestedAlt == "" {
		t.Fatal("download-like skill should suggest download_file")
	}
}

func TestAssessRunnerCompatibility_UnsupportedAction(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:  "broken",
		Steps: []corelib.NLSkillStep{{Action: "totally_unknown", Params: map[string]interface{}{}}},
	}
	report := AssessRunnerCompatibility(entry, RunnerBackendGUI)
	if report.Runnable {
		t.Fatal("expected not runnable")
	}
	if len(report.UnsupportedActions) == 0 {
		t.Fatal("expected unsupported actions")
	}
}

func TestLooksLikeDownloadSearchQuery(t *testing.T) {
	if !LooksLikeDownloadSearchQuery("wget tool") {
		t.Fatal("expected wget query match")
	}
	if !LooksLikeDownloadSearchQuery("下载文件") {
		t.Fatal("expected Chinese download query match")
	}
	if LooksLikeDownloadSearchQuery("翻译论文 skill") {
		t.Fatal("literature translate should not force download caution alone")
	}
}

func TestLooksLikeGenericDownloadSkill(t *testing.T) {
	yes := [][2]string{
		{"Wget Tool", "download files with wget"},
		{"Curl Tool", "http download helper"},
		{"Paper Fetch", "fetch pdf from arxiv"},
		{"download tool", ""},
	}
	for _, tc := range yes {
		if !LooksLikeGenericDownloadSkill(tc[0], tc[1]) {
			t.Fatalf("expected generic download: name=%q desc=%q", tc[0], tc[1])
		}
	}
	no := [][2]string{
		{"paper_pdf_translator", "translate academic PDFs offline"},
		{"sheet-analysis", "analyze excel sheets"},
		{"fetch weather", "domain weather fetch api"}, // not a download helper name pattern
	}
	for _, tc := range no {
		if LooksLikeGenericDownloadSkill(tc[0], tc[1]) {
			t.Fatalf("expected NOT generic download: name=%q desc=%q", tc[0], tc[1])
		}
	}
}

func TestAssessRunnerCompatibility_CraftToolOnlyDownload(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:        "Curl Tool",
		Description: "download files with curl",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "make curl tool"},
		}},
	}
	report := AssessRunnerCompatibility(entry, RunnerBackendGUI)
	if !report.Runnable {
		t.Fatal("craft_tool is GUI-supported")
	}
	if !report.CraftToolOnly {
		t.Fatal("expected craft_tool_only")
	}
	text := FormatRunnerCompatReport(report)
	if !strings.Contains(text, "download_file") {
		t.Fatalf("format missing download_file hint: %s", text)
	}
}
