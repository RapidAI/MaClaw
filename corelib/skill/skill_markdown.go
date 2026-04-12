package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// bashBlockRe matches fenced bash code blocks in markdown.
// Captures the content between ```bash (or ```bash.norun) and ```.
var bashBlockRe = regexp.MustCompile("(?s)```bash(\\.norun)?\\s*\n(.*?)```")

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

// extractAllBashBlocksFromMarkdown returns all bash code blocks found in the
// markdown content, including those with {baseDir} placeholders. Unlike
// extractBashBlocksFromMarkdown, this does NOT filter out blocks with
// unresolved placeholders or Chinese paths — the caller is responsible for
// resolving {baseDir} and deciding which blocks are executable.
func extractAllBashBlocksFromMarkdown(content string) []string {
	matches := bashBlockRe.FindAllStringSubmatch(content, -1)
	var blocks []string
	for _, m := range matches {
		// m[1] = ".norun" or "" (the optional suffix capture group)
		// m[2] = block content
		if len(m) > 2 && m[1] == "" {
			trimmed := strings.TrimSpace(m[2])
			if trimmed != "" {
				blocks = append(blocks, trimmed)
			}
		}
	}
	return blocks
}

// resolveBaseDirInBlock replaces {baseDir} placeholders in a bash block
// with the actual skill directory path (using forward slashes).
func resolveBaseDirInBlock(block, skillDir string) string {
	if skillDir == "" {
		return block
	}
	slashDir := filepath.ToSlash(strings.TrimRight(skillDir, `/\`))
	result := strings.ReplaceAll(block, "{baseDir}", slashDir)
	result = strings.ReplaceAll(result, "{base_dir}", slashDir)
	return result
}

// isResolvedBlockExecutable checks if a bash block (after {baseDir} resolution)
// looks like an executable command. This is a lighter check than
// isExecutableBashBlock — it only rejects blocks that still have unresolved
// template placeholders like {{input}}.pdf or Chinese example paths that
// are clearly NOT the {baseDir}-resolved skill directory.
func isResolvedBlockExecutable(block, skillDir string) bool {
	slashDir := ""
	if skillDir != "" {
		slashDir = filepath.ToSlash(strings.TrimRight(skillDir, `/\`))
	}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Still has unresolved {baseDir} — not executable
		if strings.Contains(line, "{baseDir}") || strings.Contains(line, "{base_dir}") {
			return false
		}
		// Check for Chinese path segments, but SKIP paths that are inside
		// the resolved skill directory (those are real paths, not examples).
		if hasChinesePathSegments(line) {
			if slashDir == "" || !strings.Contains(line, slashDir) {
				return false
			}
			// The Chinese path is within the skill directory — it's a real path
		}
		// Check for remaining template placeholders that look like file paths
		if strings.Contains(line, "{{/") || strings.Contains(line, "{{\\") ||
			strings.Contains(line, "}}.pdf") || strings.Contains(line, "}}.md") {
			return false
		}
	}
	return true
}

// extractBashBlocksFromMarkdown returns executable bash code blocks from
// markdown, filtering out usage examples with unresolved placeholders.
// This is the legacy API used by callers that don't have skill directory context.
func extractBashBlocksFromMarkdown(content string) []string {
	all := extractAllBashBlocksFromMarkdown(content)
	var result []string
	for _, block := range all {
		if isExecutableBashBlock(block) {
			result = append(result, block)
		}
	}
	return result
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
		RequiredArgs:   parsed.requiredArgs,
		RequiredEnv:    parsed.requiredEnv,
		PreferredShell: parsed.preferredShell,
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

	// Build a set of script files that actually exist in the scripts/ directory.
	localScripts := make(map[string]bool)
	scriptEntries, _ := os.ReadDir(scriptsDir)
	for _, e := range scriptEntries {
		if !e.IsDir() && isScriptFileName(e.Name()) {
			localScripts[e.Name()] = true
		}
	}

	// Extract ALL bash blocks from markdown (including {baseDir} ones) and
	// process them in document order. This ensures step execution order matches
	// the SKILL.md definition order.
	var allBlocks []string
	if strings.TrimSpace(parsed.markdown) != "" {
		allBlocks = extractAllBashBlocksFromMarkdown(parsed.markdown)
	}

	if len(allBlocks) > 0 {
		log.Printf("[skill-parser] %s: found %d bash blocks in SKILL.md, %d scripts in scripts/",
			parsed.name, len(allBlocks), len(localScripts))
		for _, rawBlock := range allBlocks {
			// Resolve {baseDir} placeholders so we can check if the block
			// references real files in the skill directory.
			resolved := resolveBaseDirInBlock(rawBlock, skillDir)

			// Check if the resolved block is executable (not a usage example).
			if !isResolvedBlockExecutable(resolved, skillDir) {
				continue
			}

			// Check if this block references a local script in scripts/.
			// If so, use the script-based command (with proper quoting etc.)
			var localScriptPath string
			for _, line := range strings.Split(resolved, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				for _, field := range strings.Fields(line) {
					field = strings.Trim(field, "\"'`")
					base := filepath.Base(field)
					if isScriptFileName(base) && localScripts[base] {
						localScriptPath = filepath.Join(scriptsDir, base)
						break
					}
				}
				if localScriptPath != "" {
					break
				}
			}

			var command string
			if localScriptPath != "" {
				// Block references a local script — use the resolved command
				// from commandFromSkillMarkdown (handles quoting, {baseDir}, etc.)
				cmd, ok := scriptExecutionCommandFromMarkdown(localScriptPath, parsed.markdown, skillDir)
				if !ok {
					continue
				}
				command = cmd
				log.Printf("[skill-parser] %s: step from script ref: %s", parsed.name, filepath.Base(localScriptPath))
			} else {
				// Direct bash command block — use the resolved content.
				command = resolved
				snippet := resolved
				if len(snippet) > 60 {
					snippet = snippet[:60] + "..."
				}
				log.Printf("[skill-parser] %s: step from direct block: %s", parsed.name, snippet)
			}

			params := map[string]interface{}{"command": command}
			if strings.TrimSpace(skillDir) != "" {
				params["working_dir"] = skillDir
			}
			steps = append(steps, corelib.NLSkillStep{Action: "bash", Params: params, OnError: "stop"})
		}
	}

	// Fallback: if no bash blocks exist in markdown but scripts/ has files,
	// include all scripts as steps (legacy behavior for skills without
	// inline bash blocks in their SKILL.md).
	if len(steps) == 0 && len(scriptEntries) > 0 {
		log.Printf("[skill-parser] %s: no bash blocks produced steps, falling back to scripts/ scan (%d files)",
			parsed.name, len(scriptEntries))
		for _, e := range scriptEntries {
			if e.IsDir() {
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
	producesArtifact := true
	if opts.ProducesArtifact != nil {
		producesArtifact = *opts.ProducesArtifact
	}
	// Apply execMode: "first" — only keep the first bash step.
	if parsed.execMode == "first" && len(steps) > 1 {
		log.Printf("[skill-parser] %s: exec_mode=first, keeping only first step out of %d", parsed.name, len(steps))
		steps = steps[:1]
	}

	// Apply operation labels from <!-- operation: xxx --> comments.
	opBlocks := extractOperationLabeledBlocks(parsed.markdown)
	if len(opBlocks) > 0 {
		for i := range steps {
			if steps[i].Action != "bash" {
				continue
			}
			cmd, _ := steps[i].Params["command"].(string)
			if cmd == "" {
				continue
			}
			// Match step command to operation block by comparing the first
			// non-comment, non-empty line of each. This is more reliable than
			// substring matching which breaks when {baseDir} resolution changes
			// the path prefix.
			cmdFirstLine := firstSignificantLine(cmd)
			for label, block := range opBlocks {
				resolved := resolveBaseDirInBlock(block, skillDir)
				blockFirstLine := firstSignificantLine(resolved)
				if cmdFirstLine != "" && blockFirstLine != "" && cmdFirstLine == blockFirstLine {
					steps[i].Label = label
					log.Printf("[skill-parser] %s: step %d labeled as %q", parsed.name, i, label)
					break
				}
			}
		}
	}

	entry := &corelib.NLSkillEntry{
		Name:          parsed.name,
		Description:   parsed.description,
		Triggers:      triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     fileModTime(mdPath),
		Source:        firstNonEmpty(strings.TrimSpace(opts.Source), "file"),
		SourceProject: firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
		TrustLevel:    strings.TrimSpace(opts.TrustLevel),
		SkillDir:      firstNonEmpty(strings.TrimSpace(opts.SkillDir), skillDir),
		Platforms:     platforms,
		RequiresGUI:   requiresGUI,
		ExecMode:      parsed.execMode,
		GlobalTimeout: parsed.timeout,
		ProducesArtifact: producesArtifact,
		RequiredArgs:   parsed.requiredArgs,
		RequiredEnv:    parsed.requiredEnv,
		PreferredShell: parsed.preferredShell,
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

// normalizeImportedScriptCommand replaces well-known placeholder arguments
// in a SKILL.md command line with {{input}} / {{output}} template variables
// so that the runner can substitute actual file paths at execution time.
func normalizeImportedScriptCommand(command string) string {
	// Static replacements for common placeholder patterns.
	replacer := strings.NewReplacer(
		`"/绝对路径/输入.md"`, "{{input}}",
		`"/绝对路径/输出.pdf"`, "{{output}}",
		`"/绝对路径/输入文件"`, "{{input}}",
		`"/绝对路径/输出文件"`, "{{output}}",
		`"/path/in.md"`, "{{input}}",
		`"/path/out.pdf"`, "{{output}}",
		`"/path/input"`, "{{input}}",
		`"/path/output"`, "{{output}}",
		`"/path/to/input.md"`, "{{input}}",
		`"/path/to/output.pdf"`, "{{output}}",
		`'/绝对路径/输入.md'`, "{{input}}",
		`'/绝对路径/输出.pdf'`, "{{output}}",
		`'/绝对路径/输入文件'`, "{{input}}",
		`'/绝对路径/输出文件'`, "{{output}}",
		`'/path/in.md'`, "{{input}}",
		`'/path/out.pdf'`, "{{output}}",
		`'/path/input'`, "{{input}}",
		`'/path/output'`, "{{output}}",
		`'/path/to/input.md'`, "{{input}}",
		`'/path/to/output.pdf'`, "{{output}}",
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
	// If the path is already quoted, return as-is to prevent double-quoting
	// which causes cmd.exe/bash to treat quotes as literal path characters.
	trimmed := strings.TrimSpace(path)
	if (strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`)) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
		return path
	}
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
	requiredArgs   []string // from frontmatter required_args
	requiredEnv    []string // from frontmatter requires.env
	preferredShell string   // from frontmatter shell (e.g. "bash", "cmd")
	execMode       string   // from frontmatter exec_mode: "all" (default), "first", "named"
	timeout        int      // from frontmatter timeout (seconds), 0 = use default
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

	// Parse extended frontmatter fields for runner compatibility.
	requiredArgs := splitCSV(strings.TrimSpace(frontmatter["required_args"]))
	requiredEnv := splitCSV(strings.TrimSpace(frontmatter["requires_env"]))
	preferredShell := strings.TrimSpace(frontmatter["shell"])
	execMode := strings.TrimSpace(frontmatter["exec_mode"])
	timeout := parseIntFrontmatter(frontmatter["timeout"])

	return &parsedSkillMarkdown{
		markdown:       skillMD,
		body:           body,
		name:           name,
		description:    description,
		compatibility:  strings.TrimSpace(frontmatter["compatibility"]),
		frontmatter:    frontmatter,
		requiredArgs:   requiredArgs,
		requiredEnv:    requiredEnv,
		preferredShell: preferredShell,
		execMode:       execMode,
		timeout:        timeout,
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

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
// Handles both "a, b, c" and "a,b,c" formats.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseIntFrontmatter parses an integer from a frontmatter string value.
// Returns 0 if the value is empty or not a valid integer.
func parseIntFrontmatter(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0
	}
	return v
}

// operationCommentRe matches <!-- operation: xxx --> HTML comments in markdown.
// Used to tag bash blocks with operation labels for selective execution.
var operationCommentRe = regexp.MustCompile(`<!--\s*operation:\s*(\S+)\s*-->`)

// firstSignificantLine returns the first non-empty, non-comment line from a
// bash block. Used for matching operation-labeled blocks to parsed steps.
func firstSignificantLine(block string) string {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

// extractOperationLabeledBlocks parses SKILL.md content and returns a map of
// operation label → bash block content. An <!-- operation: xxx --> comment
// tags the next ```bash block that follows it.
func extractOperationLabeledBlocks(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var pendingLabel string
	inBashBlock := false
	var blockLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for operation comment
		if m := operationCommentRe.FindStringSubmatch(trimmed); len(m) > 1 {
			pendingLabel = m[1]
			continue
		}

		// Check for bash block start (exclude .norun)
		if strings.HasPrefix(trimmed, "```bash") && !strings.Contains(trimmed, ".norun") {
			inBashBlock = true
			blockLines = nil
			continue
		}

		// Check for block end
		if inBashBlock && strings.HasPrefix(trimmed, "```") {
			inBashBlock = false
			if pendingLabel != "" && len(blockLines) > 0 {
				result[pendingLabel] = strings.TrimSpace(strings.Join(blockLines, "\n"))
			}
			pendingLabel = ""
			continue
		}

		if inBashBlock {
			blockLines = append(blockLines, line)
		} else if pendingLabel != "" && trimmed != "" &&
			!strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "<!--") {
			// Non-empty, non-heading, non-comment line between operation comment
			// and bash block — clear the pending label to avoid mis-association.
			pendingLabel = ""
		}
	}
	return result
}
