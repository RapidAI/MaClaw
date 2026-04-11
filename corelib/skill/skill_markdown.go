package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// bashBlockRe matches fenced bash code blocks in markdown.
// Captures the content between ```bash and ```.
var bashBlockRe = regexp.MustCompile("(?s)```bash\\s*\n(.*?)```")

// hasUnresolvedPlaceholders checks if a line contains template-like
// placeholders such as {{input}}, {baseDir}, etc. that were not resolved.
func hasUnresolvedPlaceholders(line string) bool {
	if strings.Contains(line, "{baseDir}") || strings.Contains(line, "{base_dir}") {
		return true
	}
	// Only flag {{...}} patterns that look like file paths (contain / or .)
	// as these are typically usage examples, not runtime template variables.
	if strings.Contains(line, "{{/") || strings.Contains(line, "{{\\") ||
		strings.Contains(line, "}}.pdf") || strings.Contains(line, "}}.md") {
		return true
	}
	return false
}

// hasChinesePathSegments detects Chinese characters that appear in what
// looks like a file path argument (quoted string containing Chinese).
// These are typically example placeholders, not executable commands.
func hasChinesePathSegments(line string) bool {
	// Check for quoted strings containing CJK characters — typical of
	// example usage like "/绝对路径/输入.md"
	inQuote := false
	quoteChar := byte(0)
	var segment strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if (c == '"' || c == '\'') && (i == 0 || line[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quoteChar = c
				segment.Reset()
			} else if c == quoteChar {
				inQuote = false
				// Check if the quoted segment contains CJK and looks like a path
				s := segment.String()
				if containsCJK(s) && (strings.Contains(s, "/") || strings.Contains(s, `\`)) {
					return true
				}
			}
		} else if inQuote {
			segment.WriteByte(c)
		}
	}
	return false
}

// containsCJK reports whether s contains any CJK Unified Ideographs.
func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// isExecutableBashBlock checks if a bash code block looks like an actual
// executable command rather than a usage example with placeholders.
func isExecutableBashBlock(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if hasUnresolvedPlaceholders(line) {
			return false
		}
		if hasChinesePathSegments(line) {
			return false
		}
	}
	return true
}

// extractBashBlocksFromMarkdown returns all bash code blocks found in the
// given markdown content. This is used to determine the number of executable
// steps for skills that define steps inline in skill.md.
func extractBashBlocksFromMarkdown(content string) []string {
	matches := bashBlockRe.FindAllStringSubmatch(content, -1)
	var blocks []string
	for _, m := range matches {
		if len(m) > 1 {
			trimmed := strings.TrimSpace(m[1])
			if trimmed == "" {
				continue
			}
			// Skip blocks that look like usage examples with unresolved
			// placeholders or Chinese path segments.
			if !isExecutableBashBlock(trimmed) {
				continue
			}
			blocks = append(blocks, trimmed)
		}
	}
	return blocks
}

type MarkdownSkillOptions struct {
	NameFallback        string
	DescriptionFallback string
	Source              string
	SourceProject       string
	TrustLevel          string
	SkillDir            string
	Triggers            []string
	Platforms           []string
	RequiresGUI         *bool
	ProducesArtifact    *bool // false = diagnostic/instruction skill, no file output expected
}

func ParseMarkdownSkill(content string, opts MarkdownSkillOptions) (*corelib.NLSkillEntry, error) {
	parsed, err := parseSkillMarkdownDocument(content, opts.NameFallback, opts.DescriptionFallback)
	if err != nil {
		return nil, err
	}
	triggers := append([]string(nil), opts.Triggers...)
	if len(triggers) == 0 && strings.TrimSpace(parsed.name) != "" {
		triggers = []string{parsed.name}
	}
	if parsed.compatibility != "" && !containsString(triggers, "agent-skill") {
		triggers = append(triggers, "agent-skill")
	}
	producesArtifact := true // default: skills produce artifacts
	if opts.ProducesArtifact != nil && !*opts.ProducesArtifact {
		producesArtifact = false
	}
	verificationMode := "artifact_required"
	if !producesArtifact {
		verificationMode = "artifact_optional"
	}
	if v := parsed.frontmatter["verification_mode"]; v != "" {
		verificationMode = v
	}
	params := map[string]interface{}{
		"instructions":      parsed.markdown,
		"verification_mode": verificationMode,
		"register_policy":   "manual",
	}
	if skillDir := strings.TrimSpace(opts.SkillDir); skillDir != "" {
		params["working_dir"] = skillDir
	}
	sourceProject := strings.TrimSpace(opts.SourceProject)
	if sourceProject == "" {
		sourceProject = parsed.compatibility
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "file"
	}
	platforms := append([]string(nil), opts.Platforms...)
	requiresGUI := false
	if opts.RequiresGUI != nil {
		requiresGUI = *opts.RequiresGUI
	}
	return &corelib.NLSkillEntry{
		Name:          parsed.name,
		Description:   parsed.description,
		Triggers:      triggers,
		Steps:         []corelib.NLSkillStep{{Action: "craft_tool", Params: params}},
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        source,
		SourceProject: sourceProject,
		TrustLevel:    strings.TrimSpace(opts.TrustLevel),
		SkillDir:      strings.TrimSpace(opts.SkillDir),
		Platforms:     platforms,
		RequiresGUI:   requiresGUI,
		ProducesArtifact: producesArtifact,
	}, nil
}

func ImportMarkdownSkillDir(skillDir string, opts MarkdownSkillOptions) (*corelib.NLSkillEntry, error) {
	mdPath, err := skillMarkdownPath(skillDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取 %s: %v", filepath.Base(mdPath), err)
	}
	parsed, err := parseSkillMarkdownDocument(string(data), opts.NameFallback, opts.DescriptionFallback)
	if err != nil {
		return nil, err
	}
	triggers := append([]string(nil), opts.Triggers...)
	if len(triggers) == 0 && strings.TrimSpace(parsed.name) != "" {
		triggers = []string{parsed.name}
	}
	if parsed.compatibility != "" && !containsString(triggers, "agent-skill") {
		triggers = append(triggers, "agent-skill")
	}
	platforms := append([]string(nil), opts.Platforms...)
	requiresGUI := false
	if opts.RequiresGUI != nil {
		requiresGUI = *opts.RequiresGUI
	}
	steps := make([]corelib.NLSkillStep, 0)
	scriptsDir := filepath.Join(skillDir, "scripts")
	// Collect script file names referenced in markdown bash blocks.
	// Only scripts explicitly mentioned in the markdown are turned into steps,
	// preventing stale scripts in the scripts/ directory from becoming phantom steps.
	referencedScripts := make(map[string]bool)
	if strings.TrimSpace(parsed.markdown) != "" {
		for _, block := range extractBashBlocksFromMarkdown(parsed.markdown) {
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				// Extract script file references from the command line.
				for _, field := range strings.Fields(line) {
					field = strings.Trim(field, "\"'`")
					base := filepath.Base(field)
					if isScriptFileName(base) {
						referencedScripts[base] = true
					}
				}
			}
		}
	}
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			// If markdown references exist, only include scripts mentioned there.
			// Otherwise, fall back to including all scripts (legacy behavior).
			if len(referencedScripts) > 0 && !referencedScripts[e.Name()] {
				continue
			}
			scriptPath := filepath.Join(scriptsDir, e.Name())
			command, ok := scriptExecutionCommandFromMarkdown(scriptPath, parsed.markdown, skillDir)
			if !ok {
				continue
			}
			params := map[string]interface{}{"command": command}
			if strings.TrimSpace(skillDir) != "" {
				params["working_dir"] = skillDir
			}
			steps = append(steps, corelib.NLSkillStep{Action: "bash", Params: params, OnError: "stop"})
		}
	}

	// [Bug #2 fix] If no script-based steps were found, check for direct
	// executable bash code blocks in the markdown. Blocks marked as ```bash
	// that contain valid commands (no unresolved placeholders, no script file
	// references) should be executed directly via bash, not delegated to
	// craft_tool which cannot run shell commands.
	if len(steps) == 0 && strings.TrimSpace(parsed.markdown) != "" {
		for _, block := range extractBashBlocksFromMarkdown(parsed.markdown) {
			// Skip blocks that reference script files — those should have been
			// handled above via the scripts/ directory scan.
			hasScriptRef := false
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				for _, field := range strings.Fields(line) {
					field = strings.Trim(field, "\"'`")
					if isScriptFileName(filepath.Base(field)) {
						hasScriptRef = true
						break
					}
				}
				if hasScriptRef {
					break
				}
			}
			if hasScriptRef {
				continue
			}
			// This is a direct bash command block — create a bash step for it.
			params := map[string]interface{}{"command": block}
			if strings.TrimSpace(skillDir) != "" {
				params["working_dir"] = skillDir
			}
			steps = append(steps, corelib.NLSkillStep{Action: "bash", Params: params, OnError: "stop"})
		}
	}

	if len(steps) == 0 {
		entry, err := ParseMarkdownSkill(parsed.markdown, MarkdownSkillOptions{
			NameFallback:        parsed.name,
			DescriptionFallback: parsed.description,
			Source:              opts.Source,
			SourceProject:       firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
			TrustLevel:          opts.TrustLevel,
			SkillDir:            firstNonEmpty(strings.TrimSpace(opts.SkillDir), skillDir),
			Triggers:            triggers,
			Platforms:           platforms,
			RequiresGUI:         &requiresGUI,
			ProducesArtifact:    opts.ProducesArtifact,
		})
		if err != nil {
			return nil, err
		}
		entry.CreatedAt = fileModTime(mdPath)
		return entry, nil
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "file"
	}
	producesArtifact := true
	if opts.ProducesArtifact != nil {
		producesArtifact = *opts.ProducesArtifact
	}
	entry := &corelib.NLSkillEntry{
		Name:          parsed.name,
		Description:   parsed.description,
		Triggers:      triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     fileModTime(mdPath),
		Source:        source,
		SourceProject: firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
		TrustLevel:    strings.TrimSpace(opts.TrustLevel),
		SkillDir:      firstNonEmpty(strings.TrimSpace(opts.SkillDir), skillDir),
		Platforms:     platforms,
		RequiresGUI:   requiresGUI,
		ProducesArtifact: producesArtifact,
	}
	return entry, nil
}

func scriptExecutionCommandFromMarkdown(scriptPath, markdown, skillDir string) (string, bool) {
	if command, ok := commandFromSkillMarkdown(scriptPath, markdown, skillDir); ok {
		return command, true
	}
	return scriptExecutionCommand(scriptPath)
}

func commandFromSkillMarkdown(scriptPath, markdown, skillDir string) (string, bool) {
	if strings.TrimSpace(markdown) == "" {
		return "", false
	}
	slashPath := filepath.ToSlash(scriptPath)
	slashSkillDir := filepath.ToSlash(strings.TrimRight(skillDir, `/\\`))
	baseDirMarker := "{baseDir}"
	for _, rawLine := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			continue
		}
		normalized := strings.ReplaceAll(line, `\"`, `"`)
		normalized = strings.ReplaceAll(normalized, `"{baseDir}/`, `"`+slashSkillDir+`/`)
		normalized = strings.ReplaceAll(normalized, `{baseDir}/`, slashSkillDir+`/`)
		normalized = strings.ReplaceAll(normalized, baseDirMarker, slashSkillDir)
		if !strings.Contains(normalized, filepath.ToSlash(filepath.Base(scriptPath))) {
			continue
		}
		if strings.Contains(normalized, slashPath) || strings.Contains(normalized, filepath.ToSlash(filepath.Join(slashSkillDir, "scripts", filepath.Base(scriptPath)))) {
			return normalizeImportedScriptCommand(normalized), true
		}
	}
	return "", false
}

func normalizeImportedScriptCommand(command string) string {
	replacer := strings.NewReplacer(
		`"/绝对路径/输入.md"`, "{{input}}",
		`"/绝对路径/输出.pdf"`, "{{output}}",
		`"/path/in.md"`, "{{input}}",
		`"/path/out.pdf"`, "{{output}}",
		`'/绝对路径/输入.md'`, "{{input}}",
		`'/绝对路径/输出.pdf'`, "{{output}}",
		`'/path/in.md'`, "{{input}}",
		`'/path/out.pdf'`, "{{output}}",
	)
	return replacer.Replace(command)
}

func scriptExecutionCommand(scriptPath string) (string, bool) {
	quoted := quoteScriptPath(scriptPath)
	name := strings.ToLower(filepath.Base(scriptPath))
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mjs", ".cjs", ".js":
		return "node " + quoted, true
	case ".ps1":
		return "& " + quoted, true
	case ".cmd", ".bat":
		return "& " + quoted, true
	case ".sh":
		return "bash " + quoted, true
	case ".py":
		return "python " + quoted, true
	case "":
		if strings.EqualFold(name, "node") {
			return "node " + quoted, true
		}
		return "& " + quoted, true
	default:
		return "", false
	}
}

func quoteScriptPath(path string) string {
	// On Windows, convert backslashes to forward slashes to prevent
	// bash from interpreting them as escape characters.
	if strings.Contains(path, `\`) {
		path = filepath.ToSlash(path)
	}
	return "\"" + strings.ReplaceAll(path, "\"", `\"`) + "\""
}

func skillMarkdownPath(skillDir string) (string, error) {
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return "", fmt.Errorf("无法读取 skill.md: %v", err)
	}
	for _, candidate := range []string{"skill.md", "SKILL.md"} {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if entry.Name() == candidate {
				return filepath.Join(skillDir, candidate), nil
			}
		}
	}
	return "", fmt.Errorf("无法读取 skill.md: file not found")
}

type parsedSkillMarkdown struct {
	markdown       string
	body           string
	name           string
	description    string
	compatibility  string
	frontmatter    map[string]string
}

func parseSkillMarkdownDocument(content, nameFallback, descriptionFallback string) (*parsedSkillMarkdown, error) {
	skillMD := strings.TrimSpace(content)
	if skillMD == "" {
		return nil, fmt.Errorf("empty skill.md document")
	}
	frontmatter, body := ParseMarkdownFrontmatter(skillMD)
	name := strings.TrimSpace(frontmatter["name"])
	if name == "" {
		name = firstMarkdownHeading(body)
	}
	if name == "" {
		name = strings.TrimSpace(nameFallback)
	}
	if name == "" {
		name = "unnamed-skill"
	}
	description := strings.TrimSpace(frontmatter["description"])
	if description == "" {
		description = strings.TrimSpace(descriptionFallback)
	}
	if description == "" {
		description = firstMarkdownParagraph(body)
	}
	return &parsedSkillMarkdown{
		markdown:      skillMD,
		body:          body,
		name:          name,
		description:   description,
		compatibility: strings.TrimSpace(frontmatter["compatibility"]),
	}, nil
}

func ParseMarkdownFrontmatter(content string) (map[string]string, string) {
	fm := make(map[string]string)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return fm, content
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return fm, content
	}
	fmBlock := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:])
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.Trim(strings.TrimSpace(line[colonIdx+1:]), `"'`)
		fm[key] = val
	}
	return fm, body
}

func firstMarkdownHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if heading != "" {
			return heading
		}
	}
	return ""
}

func firstMarkdownParagraph(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var triggerCleanupRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func normalizeTrigger(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "-")
	value = triggerCleanupRe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	return value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// isScriptFileName checks if a file name looks like an executable script.
func isScriptFileName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mjs", ".cjs", ".js", ".sh", ".py", ".ps1", ".bat", ".cmd", ".rb", ".pl":
		return true
	}
	return false
}
