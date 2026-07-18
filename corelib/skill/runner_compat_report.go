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
	Runner              string   `json:"runner"`
	Runnable            bool     `json:"runnable"`
	StepCount           int      `json:"step_count"`
	SupportedSteps      int      `json:"supported_steps"`
	UnsupportedActions  []string `json:"unsupported_actions,omitempty"`
	CraftToolOnly       bool     `json:"craft_tool_only,omitempty"`
	HasBash             bool     `json:"has_bash,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
	SuggestedAlt        string   `json:"suggested_alt,omitempty"`
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
	}
	if !report.Runnable {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("unsupported step actions for %s runner: %s",
				report.Runner, strings.Join(report.UnsupportedActions, ", ")))
	}
	if LooksLikeGenericDownloadSkill(entry.Name, entry.Description) {
		report.SuggestedAlt = "download_file (or web_fetch with save_path) for simple HTTP/PDF downloads"
		if !report.Runnable || report.CraftToolOnly {
			report.Warnings = append(report.Warnings,
				"for simple downloads prefer built-in download_file instead of this Hub skill")
		}
	}
	return report
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
	if strings.TrimSpace(entry.Type) == "instruction" || strings.TrimSpace(entry.Type) == "knowledge" {
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
