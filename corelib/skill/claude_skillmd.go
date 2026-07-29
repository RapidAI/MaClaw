package skill

// Claude SKILL.md format compatibility.
//
// Claude skills use SKILL.md with YAML frontmatter containing:
//   - name, description, version, author, license
//   - allowed-tools: list of tool names the skill may use
//   - model: preferred model
//   - tools: array of custom tool definitions with script paths and parameters
//
// The body after the frontmatter is the skill's instruction markdown.
// Scripts live in a scripts/ subdirectory.
//
// This module parses Claude-format SKILL.md and converts it to NLSkillEntry
// so that skills from awesome-claude-skills and similar repos work out of the box.

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

// claudeSkillMeta corresponds to the YAML frontmatter of a Claude SKILL.md.
type claudeSkillMeta struct {
	Name         string          `yaml:"name"`
	Description  string          `yaml:"description"`
	AllowedTools []string        `yaml:"allowed-tools"`
	Model        string          `yaml:"model"`
	Author       string          `yaml:"author"`
	Version      string          `yaml:"version"`
	License      string          `yaml:"license"`
	Tools        []claudeToolDef `yaml:"tools"`
}

// claudeToolDef describes a custom tool defined in Claude SKILL.md frontmatter.
type claudeToolDef struct {
	Name        string                     `yaml:"name"`
	Script      string                     `yaml:"script"`
	Description string                     `yaml:"description"`
	Parameters  map[string]claudeToolParam `yaml:"parameters"`
}

// claudeToolParam describes a parameter of a Claude tool definition.
type claudeToolParam struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// IsClaudeSKILLMD checks whether the given markdown content is a Claude-format
// SKILL.md (has YAML frontmatter with at least a "name" field and uses the
// Claude-specific "allowed-tools" or "tools" keys).
func IsClaudeSKILLMD(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return false
	}
	rest := trimmed[3:]
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		return false
	}
	fmBlock := rest[:idx]
	// Quick heuristic: Claude SKILL.md uses "allowed-tools" or "tools" in frontmatter.
	return bytes.Contains(fmBlock, []byte("allowed-tools")) ||
		bytes.Contains(fmBlock, []byte("tools:"))
}

// ParseClaudeSKILLMD parses a Claude-format SKILL.md and returns an NLSkillEntry.
// skillDir is the directory containing the SKILL.md file.
func ParseClaudeSKILLMD(skillDir string, data []byte) (*corelib.NLSkillEntry, error) {
	marker := []byte("---")
	parts := bytes.SplitN(data, marker, 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("no YAML frontmatter found in Claude SKILL.md")
	}

	var meta claudeSkillMeta
	if err := yaml.Unmarshal(parts[1], &meta); err != nil {
		return nil, fmt.Errorf("failed to parse Claude SKILL.md frontmatter: %w", err)
	}

	body := strings.TrimSpace(string(parts[2]))

	if meta.Name == "" {
		// Fallback: use directory name
		meta.Name = filepath.Base(skillDir)
		meta.Name = strings.ReplaceAll(meta.Name, "-", " ")
		meta.Name = strings.ReplaceAll(meta.Name, "_", " ")
	}

	if meta.Description == "" {
		meta.Description = firstMarkdownParagraph(body)
	}

	// Build steps from tools definitions and scripts/ directory.
	steps := buildClaudeSkillSteps(skillDir, meta)

	// If no explicit tools, try auto-discovering scripts/ directory.
	if len(steps) == 0 {
		steps = autoDiscoverScripts(skillDir)
	}

	// If still no steps, extract bash blocks from the markdown body.
	if len(steps) == 0 {
		allBlocks := extractAllBashBlocksFromMarkdown(body)
		for _, block := range allBlocks {
			resolved := resolveBaseDirInBlock(block, skillDir)
			if !isResolvedBlockExecutable(resolved, skillDir) {
				continue
			}
			params := map[string]interface{}{"command": resolved}
			if skillDir != "" {
				params["working_dir"] = skillDir
			}
			steps = append(steps, corelib.NLSkillStep{
				Action: "bash", Params: params, OnError: "stop",
			})
		}
	}

	log.Printf("[claude-compat] parsed Claude SKILL.md: name=%q tools=%d steps=%d",
		meta.Name, len(meta.Tools), len(steps))

	return &corelib.NLSkillEntry{
		Name:        meta.Name,
		Description: meta.Description,
		Steps:       steps,
		Status:      "active",
		Source:      "file",
		SkillDir:    skillDir,
		Triggers:    []string{meta.Name},
	}, nil
}

// buildClaudeSkillSteps converts Claude tool definitions to NLSkillStep entries.
func buildClaudeSkillSteps(skillDir string, meta claudeSkillMeta) []corelib.NLSkillStep {
	var steps []corelib.NLSkillStep

	for _, t := range meta.Tools {
		scriptPath := resolveClaudeScriptPath(skillDir, t)
		if scriptPath == "" {
			continue
		}

		cmd := buildScriptCommand(scriptPath)
		params := map[string]interface{}{"command": cmd}
		if skillDir != "" {
			params["working_dir"] = skillDir
		}

		steps = append(steps, corelib.NLSkillStep{
			Action:  "bash",
			Name:    t.Name,
			Params:  params,
			OnError: "stop",
		})
	}

	return steps
}

// resolveClaudeScriptPath finds the actual script file for a Claude tool definition.
func resolveClaudeScriptPath(skillDir string, t claudeToolDef) string {
	// 1. Explicit script path in tool definition.
	if t.Script != "" {
		full := filepath.Join(skillDir, t.Script)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}

	// 2. Infer from tool name: replace underscores with hyphens, try common extensions.
	scriptName := strings.ReplaceAll(t.Name, "_", "-")
	for _, ext := range []string{".py", ".ts", ".js", ".sh"} {
		candidate := filepath.Join(skillDir, "scripts", scriptName+ext)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// 3. Try exact tool name with extensions.
	for _, ext := range []string{".py", ".ts", ".js", ".sh"} {
		candidate := filepath.Join(skillDir, "scripts", t.Name+ext)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// buildScriptCommand returns the shell command to execute a script file
// based on its extension. Delegates to scriptExecutionCommand which already
// handles .mjs, .cjs, .ps1, .cmd, .bat etc.
func buildScriptCommand(scriptPath string) string {
	if cmd, ok := scriptExecutionCommand(scriptPath); ok {
		return cmd
	}
	// Fallback for unknown extensions.
	quoted := quoteScriptPath(scriptPath)
	if runtime.GOOS == "windows" {
		return "powershell -NoProfile -ExecutionPolicy Bypass -File " + quoted
	}
	return "bash " + quoted
}

// autoDiscoverScripts scans the scripts/ subdirectory and creates bash steps
// for each script file found. This enables Claude skills that rely on
// scripts/ without explicit tool definitions in the frontmatter.
func autoDiscoverScripts(skillDir string) []corelib.NLSkillStep {
	scriptsDir := filepath.Join(skillDir, "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return nil
	}

	var steps []corelib.NLSkillStep
	for _, e := range entries {
		if e.IsDir() || !isScriptFileName(e.Name()) {
			continue
		}
		scriptPath := filepath.Join(scriptsDir, e.Name())
		cmd := buildScriptCommand(scriptPath)
		params := map[string]interface{}{"command": cmd}
		if skillDir != "" {
			params["working_dir"] = skillDir
		}
		steps = append(steps, corelib.NLSkillStep{
			Action:  "bash",
			Name:    strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			Params:  params,
			OnError: "stop",
		})
	}

	return steps
}
