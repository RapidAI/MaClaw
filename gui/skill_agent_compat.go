package main

import (
	"context"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// ImportAgentSkill reads an Anthropic Agent Skills directory (containing
// SKILL.md and optional scripts/) and converts it to an NLSkillEntry.
func ImportAgentSkill(skillDir string) (*corelib.NLSkillEntry, error) {
	entry, err := cskill.ImportMarkdownSkillDir(skillDir, cskill.MarkdownSkillOptions{
		NameFallback: filepath.Base(skillDir),
		Source:       string(skillEntrySourceAgent),
		SkillDir:     skillDir,
	})
	if err != nil {
		return nil, err
	}
	for i := range entry.Steps {
		if !classifySkillStepAction(entry.Steps[i].Action).IsBash() {
			continue
		}
		cmd, _ := entry.Steps[i].Params["command"].(string)
		if cmd == "" {
			continue
		}
		entry.Steps[i].Params["command"] = normalizeImportedAgentSkillCommand(cmd)
	}
	entry.Source = string(skillEntrySourceAgent)
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	return entry, nil
}

func normalizeImportedAgentSkillCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range []string{"bash ", "node "} {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if len(rest) >= 2 && strings.HasPrefix(rest, "\"") && strings.HasSuffix(rest, "\"") {
			pathPart := strings.TrimSuffix(strings.TrimPrefix(rest, "\""), "\"")
			if runtime.GOOS == "windows" {
				pathPart = filepath.Clean(filepath.FromSlash(pathPart))
			}
			pathPart = strings.ReplaceAll(pathPart, "\"", `\\"`)
			return prefix + `"` + pathPart + `"`
		}
	}
	return cmd
}

// ExportAgentSkill converts an NLSkillEntry to Anthropic Agent Skills format,
// writing SKILL.md and scripts/ to outputDir.
func ExportAgentSkill(entry corelib.NLSkillEntry, outputDir string, appOpt ...*App) error {
	if err := scanAgentSkillExport(entry, appOpt...); err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("閸掓稑缂撴潏鎾冲毉閻╊喖缍嶆径杈Е: %v", err)
	}

	// Build YAML frontmatter.
	var fm strings.Builder
	fm.WriteString("---\n")
	fm.WriteString(fmt.Sprintf("name: %s\n", sanitizeAgentSkillName(entry.Name)))
	fm.WriteString(fmt.Sprintf("description: %s\n", singleLine(entry.Description)))
	fm.WriteString("compatibility: maclaw\n")
	fm.WriteString("---\n\n")

	// Build markdown body from steps.
	var body strings.Builder
	body.WriteString("# " + entry.Name + "\n\n")
	body.WriteString(entry.Description + "\n\n")

	// Write scripts.
	hasScripts := false
	scriptIdx := 0
	scriptsDir := filepath.Join(outputDir, "scripts")
	scriptsDirCreated := false
	for _, step := range entry.Steps {
		if classifySkillStepAction(step.Action).IsBash() {
			cmd, _ := step.Params["command"].(string)
			if cmd == "" {
				continue
			}
			if !scriptsDirCreated {
				if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
					return err
				}
				scriptsDirCreated = true
			}
			hasScripts = true
			scriptIdx++
			scriptName := fmt.Sprintf("step_%02d.sh", scriptIdx)

			scriptPath := filepath.Join(scriptsDir, scriptName)
			if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\n"+cmd+"\n"), 0o755); err != nil {
				return err
			}

			body.WriteString(fmt.Sprintf("## Step %d\n\n", scriptIdx))
			body.WriteString(fmt.Sprintf("Run `scripts/%s`\n\n", scriptName))
		} else {
			body.WriteString(fmt.Sprintf("## Step: %s\n\n", step.Action))
			body.WriteString(fmt.Sprintf("Action: `%s`\n\n", step.Action))
		}
	}

	if !hasScripts {
		body.WriteString("This skill contains no executable scripts.\n")
	}

	// Write SKILL.md.
	content := fm.String() + body.String()
	mdPath := filepath.Join(outputDir, "SKILL.md")
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("閸愭瑥鍙?SKILL.md 婢惰精瑙? %v", err)
	}

	return nil
}

func scanAgentSkillExport(entry corelib.NLSkillEntry, appOpt ...*App) error {
	if app := firstSkillLifecycleApp(appOpt...); app != nil && app.isRiskGuardrailOffMode() {
		return nil
	}
	report := cskill.NewSecurityScanner(nil).ScanInstallStaged(context.Background(), &entry, entry.SkillDir, nil)
	if report == nil {
		if app := firstSkillLifecycleApp(appOpt...); app != nil && !app.skillInstallMissingScanShouldBlock() {
			return nil
		}
		return fmt.Errorf("agent skill export security scan produced no report")
	}
	if app := firstSkillLifecycleApp(appOpt...); app != nil {
		if app.skillInstallScanShouldBlock(report) {
			return fmt.Errorf("agent skill export blocked by security scan: level=%s summary=%s", report.FinalLevel, report.Summary)
		}
		return nil
	}
	return nil
}

// parseFrontmatter splits a SKILL.md into YAML frontmatter key-value pairs
// and the remaining markdown body. Handles both \n and \r\n line endings.
func parseFrontmatter(content string) (map[string]string, string) {
	return cskill.ParseMarkdownFrontmatter(content)
}

// sanitizeAgentSkillName converts a skill name to the Agent Skills naming
// convention: lowercase, hyphens, 1-64 chars, no consecutive hyphens.
func sanitizeAgentSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")

	// Remove invalid characters.
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	name = b.String()

	// Collapse consecutive hyphens.
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")

	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		name = "unnamed-skill"
	}
	return name
}

// singleLine collapses a multi-line string to a single line for YAML frontmatter.
func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}
