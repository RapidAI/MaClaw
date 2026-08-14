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

func TestAssessRunnerCompatibility_AgentGuidedWorkflowIsNotRunnable(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:   "Book-PDF",
		Source: "clawhub",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": `
# Book project
阶段1：调研
启动多个background agent并行调研。
与用户确认大纲后进入写作。
多Agent并行写作，每个 agent 输出一个 HTML 片段。
从 templates/ 复制项目骨架，使用 scripts/init-project.sh。
维护 version.json 和 CHANGELOG。
`},
		}},
	}
	report := AssessRunnerCompatibility(entry, RunnerBackendGUI)
	if report.Runnable || !report.AgentGuidedOnly {
		t.Fatalf("report = %#v, want non-runnable agent-guided workflow", report)
	}
	if !strings.Contains(FormatRunnerCompatReport(report), "agent_guided_only=true") {
		t.Fatalf("formatted report missing agent-guided marker: %s", FormatRunnerCompatReport(report))
	}
	if strings.Contains(FormatRunnerCompatReport(report), "unsupported step actions") {
		t.Fatalf("agent-guided report must not claim unsupported actions: %s", FormatRunnerCompatReport(report))
	}
}

func TestAssessRunnerCompatibility_DoesNotRejectOrdinaryPlaywrightRecipe(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:   "render-pdf",
		Source: "clawhub",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "Use Node and Playwright to render the requested HTML to PDF."},
		}},
	}
	report := AssessRunnerCompatibility(entry, RunnerBackendGUI)
	if !report.Runnable || report.AgentGuidedOnly {
		t.Fatalf("ordinary recipe should remain runnable: %#v", report)
	}
}

func TestAssessRunnerCompatibility_DoesNotReclassifyManualCraftTool(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:   "manual-book-helper",
		Source: "manual",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "多Agent并行写作。阶段1 调研；与用户确认大纲；从 templates/ 复制项目；维护 version.json。"},
		}},
	}
	report := AssessRunnerCompatibility(entry, RunnerBackendGUI)
	if !report.Runnable || report.AgentGuidedOnly {
		t.Fatalf("manual craft_tool must not be reclassified: %#v", report)
	}
}

func TestAssessRunnerCompatibility_RecognizesSkillHubImportedWorkflow(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:   "Book-PDF",
		Source: "hub",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "阶段1 调研，启动多个background agent并行调研；与用户确认大纲；多Agent并行写作；从 templates/ 复制项目骨架；维护 version.json。"},
		}},
	}
	if report := AssessRunnerCompatibility(entry, RunnerBackendGUI); report.Runnable || !report.AgentGuidedOnly {
		t.Fatalf("hub imported workflow = %#v, want agent-guided incompatibility", report)
	}
}

func TestAssessRunnerCompatibility_RecognizesSkillMarketImportedWorkflow(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:   "Book-PDF",
		Source: "skillmarket",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "Phase 1 research with multiple background agents; confirm with the user; use templates/ and scripts/; maintain version.json."},
		}},
	}
	if report := AssessRunnerCompatibility(entry, RunnerBackendGUI); report.Runnable || !report.AgentGuidedOnly {
		t.Fatalf("skillmarket imported workflow = %#v, want agent-guided incompatibility", report)
	}
}
