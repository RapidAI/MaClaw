package skill

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RunnerCompatReport describes whether a skill can execute on a given runner
// after NormalizeStepForRunner. Used at install time and skill inspect to stop
// the agent from treating non-runnable Hub skills as download tools.
type RunnerCompatReport struct {
	Runner             string   `json:"runner"`
	Runnable           bool     `json:"runnable"`
	StepCount          int      `json:"step_count"`
	SupportedSteps     int      `json:"supported_steps"`
	UnsupportedActions []string `json:"unsupported_actions,omitempty"`
	CraftToolOnly      bool     `json:"craft_tool_only,omitempty"`
	// AgentGuidedOnly identifies imported Markdown workflows which describe an
	// agent-led project (for example research -> approval -> multi-agent
	// writing) rather than a single recipe the GUI runner can execute.
	AgentGuidedOnly bool     `json:"agent_guided_only,omitempty"`
	HasBash         bool     `json:"has_bash,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	SuggestedAlt    string   `json:"suggested_alt,omitempty"`
}

// AssessRunnerCompatibility normalizes a copy of the skill steps and checks
// them against SupportedStepActions for the given runner backend.
func AssessRunnerCompatibility(entry *corelib.NLSkillEntry, runner string) RunnerCompatReport {
	report := RunnerCompatReport{
		Runner:   normalizeRunnerBackend(runner),
		Runnable: true,
	}
	if entry == nil {
		report.Runnable = false
		report.Warnings = []string{"skill entry is nil"}
		return report
	}
	skillDir := strings.TrimSpace(entry.SkillDir)
	steps := entry.Steps
	report.StepCount = len(steps)
	if IsInstructionOnlySkillType(entry.Type) {
		report.Runnable = false
		report.Warnings = append(report.Warnings, "instruction-only app container is not directly executable")
		return report
	}
	if len(steps) == 0 {
		// Knowledge / instruction-only skills are "runnable" as documentation.
		if IsKnowledgeSkillType(entry.Type) || isInstructionOnlyEntry(entry) {
			report.Warnings = append(report.Warnings, "no executable steps (documentation/knowledge skill)")
			return report
		}
		report.Runnable = false
		report.Warnings = append(report.Warnings, "no executable steps")
		return report
	}

	var craftOnly = true
	unsupported := make(map[string]struct{})
	for _, raw := range steps {
		step := NormalizeStepForRunnerCopy(raw, skillDir)
		action := NormalizeStepActionName(step.Action)
		if action == "bash" {
			report.HasBash = true
			craftOnly = false
		} else if action != "craft_tool" {
			craftOnly = false
		}
		if err := EnsureStepActionSupported(report.Runner, action); err != nil {
			report.Runnable = false
			if action == "" {
				action = "<empty>"
			}
			unsupported[action] = struct{}{}
			continue
		}
		report.SupportedSteps++
	}
	for action := range unsupported {
		report.UnsupportedActions = append(report.UnsupportedActions, action)
	}
	report.CraftToolOnly = craftOnly && report.SupportedSteps > 0 && !report.HasBash
	if report.CraftToolOnly {
		report.Warnings = append(report.Warnings,
			"skill is craft_tool-only; requires a working Python runtime at run time")
		if IsAgentGuidedWorkflowSkill(entry) {
			report.AgentGuidedOnly = true
			report.Runnable = false
			report.Warnings = append(report.Warnings,
				"imported Markdown workflow requires interactive agent orchestration and cannot run as one GUI skill step")
			report.SuggestedAlt = "use this as an agent-guided project workflow; it is not directly runnable by the GUI skill runner"
		}
	}
	if !report.Runnable && len(report.UnsupportedActions) > 0 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("unsupported step actions for %s runner: %s",
				report.Runner, strings.Join(report.UnsupportedActions, ", ")))
	}
	if !report.AgentGuidedOnly && LooksLikeGenericDownloadSkill(entry.Name, entry.Description) {
		report.SuggestedAlt = "download_file (or web_fetch with save_path) for simple HTTP/PDF downloads"
		if !report.Runnable || report.CraftToolOnly {
			report.Warnings = append(report.Warnings,
				"for simple downloads prefer built-in download_file instead of this Hub skill")
		}
	}
	return report
}

// IsAgentGuidedWorkflowSkill reports whether an imported Markdown skill is a
// project workflow that needs an interactive agent rather than a single GUI
// runner invocation. It deliberately requires multiple independent signals so
// ordinary craft_tool skills that merely mention Node, Playwright, or a script
// continue to run normally.
func IsAgentGuidedWorkflowSkill(entry *corelib.NLSkillEntry) bool {
	if entry == nil || len(entry.Steps) != 1 {
		return false
	}
	// Only imported marketplace sources use the SKILL.md -> craft_tool adapter.
	// A manually-authored craft_tool can legitimately describe a richer task and
	// must not be silently reclassified merely because it uses similar words.
	if !isImportedMarkdownWorkflowSource(entry.Source) {
		return false
	}
	step := NormalizeStepForRunnerCopy(entry.Steps[0], entry.SkillDir)
	if NormalizeStepActionName(step.Action) != "craft_tool" {
		return false
	}
	instructions, _ := step.Params["instructions"].(string)
	text := strings.ToLower(strings.TrimSpace(instructions))
	if text == "" {
		return false
	}

	// One explicit multi-agent requirement plus two workflow-management signals
	// is intentionally conservative. This catches Book-PDF-style SKILL.md files
	// without treating a normal executable recipe as documentation-only.
	multiAgent := containsRunnerCompatAny(text,
		"multi-agent", "multi agent", "background agent", "background agents",
		"多agent", "多 agent", "多个agent", "多个 agent", "多智能体", "并行写作")
	if !multiAgent {
		return false
	}
	signals := 0
	if containsRunnerCompatAny(text, "research →", "research ->", "调研 →", "调研->", "阶段1", "阶段 1", "五个阶段", "multi-phase", "多阶段") {
		signals++
	}
	if containsRunnerCompatAny(text, "confirm with the user", "user confirmation", "ask the user", "与用户确认", "用户确认", "等待用户确认") {
		signals++
	}
	if containsRunnerCompatAny(text, "templates/", "scripts/", "references/", "项目初始化", "project initialization") {
		signals++
	}
	if containsRunnerCompatAny(text, "version.json", "changelog", "语义化版本", "semantic version") {
		signals++
	}
	return signals >= 2
}

func isImportedMarkdownWorkflowSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "hub", "skillhub", "skillmarket", "clawhub", "github", "agent_skill",
		"auto_hub", "auto_skillhub", "auto_skillmarket", "auto_clawhub", "auto_github":
		return true
	default:
		return false
	}
}

func containsRunnerCompatAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// LooksLikeGenericDownloadSkill reports whether a skill name/description is a
// generic HTTP download helper (wget/curl/fetch) rather than a domain pipeline.
// Used by Hub search formatting and install/run demotion hints.
//
// Intentionally avoids bare "download"/"fetch" alone — those match domain skills
// like paper_pdf_translator ("fetch PDF then translate") and would over-demote.
func LooksLikeGenericDownloadSkill(name, description string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	d := strings.ToLower(strings.TrimSpace(description))
	blob := n + " " + d
	if blob == " " || blob == "" {
		return false
	}
	// Domain pipelines that happen to mention download/fetch stay excluded.
	if strings.Contains(blob, "translat") || strings.Contains(blob, "summar") ||
		strings.Contains(blob, "ocr") || strings.Contains(blob, "pdf_translator") ||
		strings.Contains(n, "translator") {
		return false
	}
	for _, key := range []string{
		"wget", "curl", "paper fetch", "paper-fetch", "http get", "http-get",
		"download tool", "download file", "download files", "download url",
		"fetch url", "fetch file", "fetch pdf", "http download",
	} {
		if strings.Contains(blob, key) {
			return true
		}
	}
	// Exact / prefix name patterns for marketplace download helpers.
	switch {
	case n == "download", n == "download tool", n == "downloader",
		strings.HasPrefix(n, "wget"), strings.HasPrefix(n, "curl"),
		strings.HasPrefix(n, "paper fetch"), strings.HasPrefix(n, "paper-fetch"):
		return true
	}
	return false
}

// LooksLikeDownloadSearchQuery reports whether a hub search query is asking for
// a generic downloader — agent should prefer built-in download_file.
func LooksLikeDownloadSearchQuery(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	for _, key := range []string{"wget", "curl", "download", "下载", "fetch pdf", "paper fetch", "http get", "下载工具", "下载文件"} {
		if strings.Contains(q, key) {
			return true
		}
	}
	return false
}

func isInstructionOnlyEntry(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return false
	}
	// Mirror lightweight instruction-only detection used by runners.
	if IsInstructionOnlySkillType(entry.Type) || strings.TrimSpace(entry.Type) == "knowledge" {
		return true
	}
	return false
}

// FormatRunnerCompatReport returns a short agent-facing summary.
func FormatRunnerCompatReport(report RunnerCompatReport) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("runner_compat(%s): runnable=%v supported_steps=%d/%d",
		report.Runner, report.Runnable, report.SupportedSteps, report.StepCount))
	if report.CraftToolOnly {
		b.WriteString(" craft_tool_only=true")
	}
	if report.AgentGuidedOnly {
		b.WriteString(" agent_guided_only=true")
	}
	if len(report.UnsupportedActions) > 0 {
		b.WriteString(" unsupported=[" + strings.Join(report.UnsupportedActions, ",") + "]")
	}
	for _, w := range report.Warnings {
		b.WriteString("\n- warning: " + w)
	}
	if report.SuggestedAlt != "" {
		b.WriteString("\n- prefer: " + report.SuggestedAlt)
	}
	return b.String()
}
