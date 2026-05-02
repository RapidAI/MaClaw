package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
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
	if len(triggers) == 0 {
		triggers = append(triggers, parsed.triggers...)
	}
	if len(triggers) == 0 && strings.TrimSpace(parsed.name) != "" {
		triggers = []string{parsed.name}
	}
	if parsed.compatibility != "" && !containsString(triggers, "agent-skill") {
		triggers = append(triggers, "agent-skill")
	}
	producesArtifact := true // default: skills produce artifacts
	if opts.ProducesArtifact != nil {
		producesArtifact = *opts.ProducesArtifact
	} else if parsed.producesArtifact != nil {
		producesArtifact = *parsed.producesArtifact
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
	if len(platforms) == 0 {
		platforms = append(platforms, parsed.platforms...)
	}
	requiresGUI := false
	if opts.RequiresGUI != nil {
		requiresGUI = *opts.RequiresGUI
	} else if parsed.requiresGUI != nil {
		requiresGUI = *parsed.requiresGUI
	}
	return &corelib.NLSkillEntry{
		Name:                    parsed.name,
		Description:             parsed.description,
		Triggers:                triggers,
		Steps:                   []corelib.NLSkillStep{{Action: "craft_tool", Params: params}},
		Status:                  "active",
		CreatedAt:               time.Now().Format(time.RFC3339),
		Source:                  source,
		SourceProject:           sourceProject,
		TrustLevel:              strings.TrimSpace(opts.TrustLevel),
		SkillDir:                strings.TrimSpace(opts.SkillDir),
		Platforms:               platforms,
		RequiresGUI:             requiresGUI,
		Mode:                    parsed.mode,
		ExecMode:                parsed.execMode,
		GlobalTimeout:           parsed.timeout,
		ProducesArtifact:        producesArtifact,
		RequiredArgs:            parsed.requiredArgs,
		RequiredEnv:             parsed.requiredEnv,
		PreferredShell:          parsed.preferredShell,
		RequiresPython:          parsed.requiresPython,
		RequiresNode:            parsed.requiresNode,
		Operations:              parsed.operations,
		Params:                  parsed.params,
		Pipeline:                parsed.pipeline,
		RequiresTools:           parsed.requiresTools,
		FallbackForTools:        parsed.fallbackForTools,
		RequiresToolsets:        parsed.requiresToolsets,
		FallbackForToolsets:     parsed.fallbackForToolsets,
		RequiredCredentialFiles: parsed.requiredCredentialFiles,
		Stateful:                parsed.stateful,
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
	if len(triggers) == 0 {
		triggers = append(triggers, parsed.triggers...)
	}
	if len(triggers) == 0 && strings.TrimSpace(parsed.name) != "" {
		triggers = []string{parsed.name}
	}
	if parsed.compatibility != "" && !containsString(triggers, "agent-skill") {
		triggers = append(triggers, "agent-skill")
	}
	platforms := append([]string(nil), opts.Platforms...)
	if len(platforms) == 0 {
		platforms = append(platforms, parsed.platforms...)
	}
	requiresGUI := false
	if opts.RequiresGUI != nil {
		requiresGUI = *opts.RequiresGUI
	} else if parsed.requiresGUI != nil {
		requiresGUI = *parsed.requiresGUI
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

	// Extract capture directives (<!-- extract: VAR=regex -->) for each bash block.
	// This enables step-to-step variable passing for SKILL.md-defined skills.
	var captureDirectives []map[string]string
	if strings.TrimSpace(parsed.markdown) != "" {
		captureDirectives = extractCaptureDirectives(parsed.markdown)
	}

	if len(allBlocks) > 0 {
		log.Printf("[skill-parser] %s: found %d bash blocks in SKILL.md, %d scripts in scripts/",
			parsed.name, len(allBlocks), len(localScripts))
		blockIdx := 0 // tracks position in captureDirectives (which includes skipped blocks)
		for _, rawBlock := range allBlocks {
			currentBlockIdx := blockIdx
			blockIdx++

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
			// Attach capture directives from <!-- extract: VAR=regex --> comments
			// preceding this bash block, enabling step-to-step variable passing.
			var capture map[string]string
			if currentBlockIdx < len(captureDirectives) && captureDirectives[currentBlockIdx] != nil {
				capture = captureDirectives[currentBlockIdx]
				log.Printf("[skill-parser] %s: step %d has capture directives: %v", parsed.name, len(steps)+1, capture)
			}
			steps = append(steps, corelib.NLSkillStep{Action: "bash", Params: params, OnError: "stop", Capture: capture})
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
	} else if parsed.producesArtifact != nil {
		producesArtifact = *parsed.producesArtifact
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
		Name:                    parsed.name,
		Description:             parsed.description,
		Triggers:                triggers,
		Steps:                   steps,
		Status:                  "active",
		CreatedAt:               fileModTime(mdPath),
		Source:                  firstNonEmpty(strings.TrimSpace(opts.Source), "file"),
		SourceProject:           firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
		TrustLevel:              strings.TrimSpace(opts.TrustLevel),
		SkillDir:                firstNonEmpty(strings.TrimSpace(opts.SkillDir), skillDir),
		Platforms:               platforms,
		RequiresGUI:             requiresGUI,
		Mode:                    parsed.mode,
		ExecMode:                parsed.execMode,
		GlobalTimeout:           parsed.timeout,
		ProducesArtifact:        producesArtifact,
		RequiredArgs:            parsed.requiredArgs,
		RequiredEnv:             parsed.requiredEnv,
		PreferredShell:          parsed.preferredShell,
		RequiresPython:          parsed.requiresPython,
		RequiresNode:            parsed.requiresNode,
		Operations:              parsed.operations,
		Params:                  parsed.params,
		Pipeline:                parsed.pipeline,
		RequiresTools:           parsed.requiresTools,
		FallbackForTools:        parsed.fallbackForTools,
		RequiresToolsets:        parsed.requiresToolsets,
		FallbackForToolsets:     parsed.fallbackForToolsets,
		RequiredCredentialFiles: parsed.requiredCredentialFiles,
		Stateful:                parsed.stateful,
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
	requiredEnv    []string // from frontmatter required_env (alias: requires_env)
	preferredShell string   // from frontmatter shell (e.g. "bash", "cmd")
	execMode       string   // from frontmatter exec_mode: "all" (default), "first", "named"
	timeout        int      // from frontmatter timeout (seconds), 0 = use default
	// Extended fields from YAML frontmatter (list/bool/struct types)
	triggers                []string // from frontmatter triggers (YAML list)
	platforms               []string // from frontmatter platforms (YAML list)
	requiresGUI             *bool    // from frontmatter requires_gui (YAML bool)
	mode                    string   // from frontmatter mode (e.g. "sequential", "api_workflow")
	producesArtifact        *bool    // from frontmatter produces_artifact (YAML bool)
	requiresPython          []string // from frontmatter requires.python (YAML list)
	requiresNode            []string // from frontmatter requires.node (YAML list)
	operations              []corelib.NLSkillOperation
	params                  []corelib.NLSkillParam
	pipeline                []corelib.SkillPipelineStep
	requiresTools           []string
	fallbackForTools        []string
	requiresToolsets        []string
	fallbackForToolsets     []string
	requiredCredentialFiles []string
	stateful                bool
}

func parseSkillMarkdownDocument(content, nameFallback, descriptionFallback string) (*parsedSkillMarkdown, error) {
	skillMD := strings.TrimSpace(content)
	if skillMD == "" {
		return nil, fmt.Errorf("empty skill.md document")
	}
	// Parse YAML frontmatter — single source of truth for all typed fields.
	yamlFM, body := ParseMarkdownFrontmatterYAML(skillMD)

	// Name resolution: YAML frontmatter → first heading → fallback → "unnamed-skill"
	name := yamlString(yamlFM["name"])
	if name == "" {
		name = firstMarkdownHeading(body)
	}
	if name == "" {
		name = strings.TrimSpace(nameFallback)
	}
	if name == "" {
		name = "unnamed-skill"
	}

	// Description resolution: YAML frontmatter → fallback → first paragraph
	description := yamlString(yamlFM["description"])
	if description == "" {
		description = strings.TrimSpace(descriptionFallback)
	}
	if description == "" {
		description = firstMarkdownParagraph(body)
	}

	// Extract all typed metadata from the single YAML map.
	meta := extractSkillMetadata(yamlFM)

	// Build the backward-compatible string map for callers that still need it
	// (e.g. verification_mode, compatibility).
	simpleFM, _ := ParseMarkdownFrontmatter(skillMD)

	return &parsedSkillMarkdown{
		markdown:                skillMD,
		body:                    body,
		name:                    name,
		description:             description,
		compatibility:           strings.TrimSpace(yamlString(yamlFM["compatibility"])),
		frontmatter:             simpleFM,
		requiredArgs:            meta.requiredArgs,
		requiredEnv:             meta.requiredEnv,
		preferredShell:          meta.preferredShell,
		execMode:                meta.execMode,
		timeout:                 meta.timeout,
		triggers:                meta.triggers,
		platforms:               meta.platforms,
		requiresGUI:             meta.requiresGUI,
		mode:                    meta.mode,
		producesArtifact:        meta.producesArtifact,
		requiresPython:          meta.requiresPython,
		requiresNode:            meta.requiresNode,
		operations:              meta.operations,
		params:                  meta.params,
		pipeline:                meta.pipeline,
		requiresTools:           meta.requiresTools,
		fallbackForTools:        meta.fallbackForTools,
		requiresToolsets:        meta.requiresToolsets,
		fallbackForToolsets:     meta.fallbackForToolsets,
		requiredCredentialFiles: meta.requiredCredentialFiles,
		stateful:                meta.stateful,
	}, nil
}

// frontmatterKeyAliases maps non-canonical frontmatter key names to their
// canonical equivalents. This is applied once at parse time by
// ParseMarkdownFrontmatterYAML so that all downstream code sees only
// canonical keys. Add new aliases here — not in every consumer.
var frontmatterKeyAliases = map[string]string{
	"requires_env": "required_env", // SKILL.md historical convention → skill.yaml canonical
}

// extractFrontmatterBlock splits a Markdown document into the raw YAML
// frontmatter block and the remaining body. Returns ("", content) if no
// frontmatter delimiters are found.
func extractFrontmatterBlock(content string) (fmBlock, body string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", content
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}
	return rest[:idx], strings.TrimSpace(rest[idx+4:])
}

func ParseMarkdownFrontmatter(content string) (map[string]string, string) {
	fm := make(map[string]string)
	fmBlock, body := extractFrontmatterBlock(content)
	if fmBlock == "" {
		return fm, body
	}
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
		if canonical, ok := frontmatterKeyAliases[key]; ok {
			key = canonical
		}
		fm[key] = val
	}
	return fm, body
}

// ParseMarkdownFrontmatterYAML parses the YAML frontmatter block from a
// Markdown document and returns the raw YAML map plus the remaining body.
// Unlike ParseMarkdownFrontmatter (which returns map[string]string), this
// preserves YAML types: lists become []interface{}, bools become bool, nested
// maps become map[string]interface{}, etc.
//
// Key aliases (e.g. requires_env → required_env) are normalized at parse time
// so downstream code only sees canonical key names.
//
// Falls back to ParseMarkdownFrontmatter-style line parsing if YAML
// unmarshalling fails (backward compatibility).
func ParseMarkdownFrontmatterYAML(content string) (map[string]interface{}, string) {
	fmBlock, body := extractFrontmatterBlock(content)
	if fmBlock == "" {
		return nil, body
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(fmBlock), &raw); err != nil {
		// YAML parse failed — fall back to simple key:value parsing and
		// wrap each value as string in the interface{} map.
		simple, _ := ParseMarkdownFrontmatter(content)
		result := make(map[string]interface{}, len(simple))
		for k, v := range simple {
			result[k] = v
		}
		return result, body
	}
	// Normalize key aliases so downstream code sees only canonical names.
	for alias, canonical := range frontmatterKeyAliases {
		if v, ok := raw[alias]; ok {
			if _, exists := raw[canonical]; !exists {
				raw[canonical] = v
			}
			delete(raw, alias)
		}
	}
	return raw, body
}

// skillFrontmatterMetadata holds all typed metadata extracted from a YAML
// frontmatter map. This is the single extraction point — both
// parseSkillMarkdownDocument and buildCraftToolFallback call
// extractSkillMetadata instead of duplicating field extraction logic.
type skillFrontmatterMetadata struct {
	requiredArgs            []string
	requiredEnv             []string
	preferredShell          string
	execMode                string
	timeout                 int
	triggers                []string
	platforms               []string
	requiresGUI             *bool
	mode                    string
	producesArtifact        *bool
	requiresPython          []string
	requiresNode            []string
	operations              []corelib.NLSkillOperation
	params                  []corelib.NLSkillParam
	pipeline                []corelib.SkillPipelineStep
	requiresTools           []string
	fallbackForTools        []string
	requiresToolsets        []string
	fallbackForToolsets     []string
	requiredCredentialFiles []string
	stateful                bool
}

// extractSkillMetadata extracts all typed skill metadata from a YAML
// frontmatter map. Key aliases have already been normalized by
// ParseMarkdownFrontmatterYAML, so this function only uses canonical names.
func extractSkillMetadata(yamlFM map[string]interface{}) skillFrontmatterMetadata {
	if yamlFM == nil {
		return skillFrontmatterMetadata{}
	}
	normalized := normalizeSkillYAMLRaw(yamlFM)
	var sf SkillYAMLFile
	if data, err := yaml.Marshal(normalized); err == nil {
		_ = yaml.Unmarshal(data, &sf)
	}
	var m skillFrontmatterMetadata
	m.requiredArgs = firstNonEmptyStringList(sf.RequiredArgs, yamlStringList(normalized["required_args"]))
	m.requiredEnv = firstNonEmptyStringList(sf.RequiredEnv, yamlStringList(normalized["required_env"]))
	m.preferredShell = firstNonEmpty(strings.TrimSpace(sf.PreferredShell), yamlString(normalized["shell"]))
	m.execMode = firstNonEmpty(strings.TrimSpace(sf.ExecMode), yamlString(normalized["exec_mode"]))
	m.timeout = sf.GlobalTimeout
	if m.timeout == 0 {
		m.timeout = yamlInt(firstNonNil(normalized["timeout"], normalized["global_timeout"]))
	}
	m.triggers = firstNonEmptyStringList(sf.Triggers, yamlStringList(normalized["triggers"]))
	m.platforms = firstNonEmptyStringList(sf.Platforms, yamlStringList(normalized["platforms"]))
	if sf.RequiresGUI {
		requiresGUI := true
		m.requiresGUI = &requiresGUI
	} else {
		m.requiresGUI = yamlBool(normalized["requires_gui"])
	}
	m.mode = firstNonEmpty(strings.TrimSpace(sf.Mode), yamlString(normalized["mode"]))
	m.producesArtifact = yamlBool(normalized["produces_artifact"])
	m.requiresPython = requiresPythonFromYAML(sf.Requires)
	m.requiresNode = requiresNodeFromYAML(sf.Requires)
	m.operations = convertSkillYAMLOperations(sf.Operations)
	m.params = convertSkillYAMLParams(sf.Params)
	m.pipeline = convertPipelineSteps(sf.Pipeline)
	m.requiresTools = sf.RequiresTools
	m.fallbackForTools = sf.FallbackForTools
	m.requiresToolsets = sf.RequiresToolsets
	m.fallbackForToolsets = sf.FallbackForToolsets
	m.requiredCredentialFiles = sf.RequiredCredentialFiles
	m.stateful = sf.Stateful
	return m
}

func firstNonEmptyStringList(primary, fallback []string) []string {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}

// yamlStringList extracts a []string from a YAML value that may be:
//   - a []interface{} (YAML list like [a, b, c])
//   - a string (CSV like "a, b, c")
//   - nil
func yamlStringList(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []interface{}:
		var result []string
		for _, item := range val {
			s := fmt.Sprintf("%v", item)
			s = strings.TrimSpace(s)
			if s != "" {
				result = append(result, s)
			}
		}
		return result
	case string:
		return splitCSV(val)
	}
	return nil
}

// yamlString extracts a string from a YAML value.
func yamlString(v interface{}) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// yamlBool extracts a *bool from a YAML value. Returns nil if not present.
func yamlBool(v interface{}) *bool {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case bool:
		return &val
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err != nil {
			return nil
		}
		return &b
	}
	return nil
}

// yamlInt extracts an int from a YAML value. Returns 0 if not present.
func yamlInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		return parseIntFrontmatter(val)
	}
	return 0
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

// extractCommentRe matches <!-- extract: VAR=regex --> HTML comments in markdown.
// Used to define output variable capture rules for the preceding or following bash block.
// Example: <!-- extract: SESSION_ID=sessionId[":]\s*([a-f0-9-]+) -->
var extractCommentRe = regexp.MustCompile(`<!--\s*extract:\s*(\w+)\s*=\s*(.+?)\s*-->`)

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

// extractCaptureDirectives parses SKILL.md content and returns a slice of
// capture maps, one per bash block (in document order). Each map contains
// varName → regex entries from <!-- extract: VAR=regex --> comments that
// precede the bash block.
//
// Example SKILL.md:
//
//	<!-- extract: SESSION_ID=sessionId[":]\s*([a-f0-9-]+) -->
//	```bash
//	python3 create_session.py "hello"
//	```
//
// This returns [{SESSION_ID: `sessionId[":]\s*([a-f0-9-]+)`}] for the first
// bash block, enabling the runner to capture SESSION_ID from its output.
func extractCaptureDirectives(content string) []map[string]string {
	var result []map[string]string
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	pendingCaptures := make(map[string]string)
	inBashBlock := false
	var blockContent strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for extract comment — accumulate captures for the next bash block
		if m := extractCommentRe.FindStringSubmatch(trimmed); len(m) > 2 {
			pendingCaptures[m[1]] = m[2]
			continue
		}

		// Check for bash block start (exclude .norun)
		if strings.HasPrefix(trimmed, "```bash") && !strings.Contains(trimmed, ".norun") {
			inBashBlock = true
			blockContent.Reset()
			continue
		}

		// .norun blocks: consume them without emitting, and clear pending captures
		if strings.HasPrefix(trimmed, "```bash") && strings.Contains(trimmed, ".norun") {
			pendingCaptures = make(map[string]string)
			continue
		}

		// Check for block end
		if inBashBlock && strings.HasPrefix(trimmed, "```") {
			inBashBlock = false
			// Only emit an entry if the block has non-empty content,
			// matching extractAllBashBlocksFromMarkdown which skips empty blocks.
			if strings.TrimSpace(blockContent.String()) != "" {
				if len(pendingCaptures) > 0 {
					cp := make(map[string]string, len(pendingCaptures))
					for k, v := range pendingCaptures {
						cp[k] = v
					}
					result = append(result, cp)
				} else {
					result = append(result, nil)
				}
			}
			pendingCaptures = make(map[string]string)
			continue
		}

		if inBashBlock {
			blockContent.WriteString(line)
			blockContent.WriteByte('\n')
		}

		// Non-comment, non-block line clears pending captures to avoid
		// mis-association with a distant bash block.
		if !inBashBlock && len(pendingCaptures) > 0 && trimmed != "" &&
			!strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "<!--") {
			pendingCaptures = make(map[string]string)
		}
	}
	return result
}

// StripBashCommentLines removes lines starting with # from a bash command
// string. This is needed when executing via cmd.exe on Windows, where #
// is not a valid comment character and causes "'#' is not recognized" errors.
// SplitSkillDocSections splits SKILL.md content into CORE and REFERENCE
// sections based on HTML comment markers:
//
//	<!-- CORE: description --> marks the start of core instructions
//	<!-- REFERENCE: description --> marks the start of reference documentation
//
// Markers must be on their own line (the entire line is consumed).
// Content after --> on the same line is discarded.
//
// When no markers are present, the entire content is treated as CORE
// (backward compatible). The CORE section is always injected fully into
// LLM context; the REFERENCE section is subject to token budget truncation.
//
// This implements the 3-layer knowledge architecture from the "5 Skill
// Architecture Patterns" article: Layer 1 (frontmatter ~100 tokens),
// Layer 2 (CORE ~2-5K tokens), Layer 3 (REFERENCE, loaded on demand).
func SplitSkillDocSections(content string) (core string, reference string) {
	const corePrefix = "<!-- CORE"
	const refPrefix = "<!-- REFERENCE"

	coreIdx := strings.Index(content, corePrefix)
	refIdx := strings.Index(content, refPrefix)

	// No markers at all → entire document is core
	if coreIdx < 0 && refIdx < 0 {
		return content, ""
	}

	// Helper: skip past the end of the HTML comment line
	skipMarkerLine := func(s string, markerStart int) int {
		rest := s[markerStart:]
		nl := strings.Index(rest, "\n")
		if nl < 0 {
			return len(s) // marker is the last line
		}
		return markerStart + nl + 1
	}

	// Only REFERENCE marker (no CORE marker) → everything before REFERENCE is core
	if coreIdx < 0 {
		corePart := strings.TrimSpace(content[:refIdx])
		refStart := skipMarkerLine(content, refIdx)
		refPart := ""
		if refStart < len(content) {
			refPart = strings.TrimSpace(content[refStart:])
		}
		return corePart, refPart
	}

	// Only CORE marker (no REFERENCE marker) → everything after CORE marker is core
	if refIdx < 0 {
		// Content before CORE marker is preamble (included in core)
		coreStart := skipMarkerLine(content, coreIdx)
		preamble := strings.TrimSpace(content[:coreIdx])
		coreBody := ""
		if coreStart < len(content) {
			coreBody = strings.TrimSpace(content[coreStart:])
		}
		if preamble != "" && coreBody != "" {
			return preamble + "\n\n" + coreBody, ""
		}
		if coreBody != "" {
			return coreBody, ""
		}
		return preamble, ""
	}

	// Both markers present — CORE must come before REFERENCE
	if coreIdx > refIdx {
		// Markers in wrong order → treat entire content as core (safe fallback)
		return content, ""
	}

	coreStart := skipMarkerLine(content, coreIdx)
	corePart := ""
	if coreStart < refIdx {
		corePart = strings.TrimSpace(content[coreStart:refIdx])
	}
	// Include any preamble before CORE marker
	preamble := strings.TrimSpace(content[:coreIdx])
	if preamble != "" && corePart != "" {
		corePart = preamble + "\n\n" + corePart
	} else if preamble != "" {
		corePart = preamble
	}

	refStart := skipMarkerLine(content, refIdx)
	refPart := ""
	if refStart < len(content) {
		refPart = strings.TrimSpace(content[refStart:])
	}

	return corePart, refPart
}

func StripBashCommentLines(command string) string {
	lines := strings.Split(command, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}
