package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

// bashBlockRe matches fenced shell code blocks in markdown.
// Captures the fence language, optional fence attributes, and block content.
var (
	bashBlockRe        = regexp.MustCompile("(?ims)^```[ \t]*(bash|sh|shell|zsh|cmd|bat|powershell|pwsh)([^\n`]*)\n(.*?)^```[ \t]*$")
	anglePlaceholderRe = regexp.MustCompile(`<[^>\s]+>`)
	asciiPlaceholderRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
)

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
	if anglePlaceholderRe.MatchString(line) {
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
		// m[1] = fence language, m[2] = attributes, m[3] = block content.
		if len(m) > 3 && !markdownShellFenceNoRun(m[1], m[2]) {
			trimmed := strings.TrimSpace(m[3])
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
		if hasUnsafeAnglePlaceholder(line) {
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

type markdownBashBlockContext struct {
	Block    string
	Headings []string
	Shell    string
}

func extractBashBlockContexts(content string) []markdownBashBlockContext {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	matches := bashBlockRe.FindAllStringSubmatchIndex(content, -1)
	contexts := make([]markdownBashBlockContext, 0, len(matches))
	for _, m := range matches {
		if len(m) < 8 || m[2] < 0 || m[3] < 0 || m[4] < 0 || m[5] < 0 || m[6] < 0 || m[7] < 0 {
			continue
		}
		lang := content[m[2]:m[3]]
		attrs := content[m[4]:m[5]]
		if markdownShellFenceNoRun(lang, attrs) {
			continue
		}
		block := strings.TrimSpace(content[m[6]:m[7]])
		if block == "" {
			continue
		}
		contexts = append(contexts, markdownBashBlockContext{
			Block:    block,
			Headings: markdownHeadingStackBefore(content[:m[0]]),
			Shell:    markdownShellFencePreferredShell(lang),
		})
	}
	return contexts
}

func markdownShellFenceNoRun(lang, attrs string) bool {
	text := strings.ToLower(strings.TrimSpace(lang + " " + attrs))
	return strings.Contains(text, ".norun") || strings.Contains(text, " norun") || strings.Contains(text, " no-run")
}

func markdownShellFencePreferredShell(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "cmd", "bat":
		return "cmd"
	case "powershell", "pwsh":
		return "powershell"
	default:
		return ""
	}
}

func parseMarkdownShellFenceStart(line string) (string, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "````") {
		return "", false, false
	}
	info := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
	if info == "" {
		return "", false, false
	}
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return "", false, false
	}
	rawLang := strings.ToLower(strings.Trim(fields[0], " \t.,;:()[]{}"))
	norun := strings.Contains(rawLang, ".norun") || strings.Contains(rawLang, ".no-run")
	rawLang = strings.TrimSuffix(strings.TrimSuffix(rawLang, ".norun"), ".no-run")
	rawLang = strings.Trim(rawLang, ".")
	if !isMarkdownShellFenceLanguage(rawLang) {
		return "", false, false
	}
	for _, field := range fields[1:] {
		field = strings.ToLower(strings.Trim(field, " \t.,;:()[]{}"))
		if field == "norun" || field == "no-run" || field == ".norun" || field == ".no-run" {
			norun = true
			break
		}
	}
	return rawLang, norun, true
}

func isMarkdownShellFenceLanguage(lang string) bool {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "bash", "sh", "shell", "zsh", "cmd", "bat", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func markdownHeadingStackBefore(prefix string) []string {
	var stack [6]string
	for _, line := range strings.Split(prefix, "\n") {
		level, text, ok := parseMarkdownHeading(line)
		if !ok {
			continue
		}
		stack[level-1] = text
		for i := level; i < len(stack); i++ {
			stack[i] = ""
		}
	}
	result := make([]string, 0, len(stack))
	for _, heading := range stack {
		if strings.TrimSpace(heading) != "" {
			result = append(result, heading)
		}
	}
	return result
}

func parseMarkdownHeading(line string) (int, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) {
		return 0, "", false
	}
	if line[level] != ' ' && line[level] != '\t' {
		return 0, "", false
	}
	text := strings.TrimSpace(line[level:])
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func shouldAutoSelectSingleMarkdownAlternative(execMode, mode string, operations []corelib.NLSkillOperation, opBlocks map[string]string, steps []corelib.NLSkillStep, contexts []markdownBashBlockContext) bool {
	if len(steps) <= 1 || len(contexts) < len(steps) {
		return false
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "api_workflow" || len(operations) > 0 || len(opBlocks) > 0 {
		return false
	}
	execMode = strings.ToLower(strings.TrimSpace(execMode))
	if execMode != "" && execMode != "auto" {
		return false
	}

	usageContext := false
	var commonTarget string
	for i, step := range steps {
		if NormalizeStepActionName(step.Action) != "bash" || len(step.Capture) > 0 {
			return false
		}
		cmd, _ := step.Params["command"].(string)
		if countSignificantCommandLines(cmd) != 1 {
			return false
		}
		target, ok := markdownCommandExecutionTarget(cmd)
		if !ok || target == "" {
			return false
		}
		if commonTarget == "" {
			commonTarget = target
		} else if commonTarget != target {
			return false
		}

		if markdownHeadingsHaveWorkflowMarker(contexts[i].Headings) {
			return false
		}
		if markdownHeadingsHaveUsageMarker(contexts[i].Headings) {
			usageContext = true
		}
	}
	return usageContext
}

func preferredMarkdownAlternativeStepIndex(steps []corelib.NLSkillStep, requiredArgs []string) int {
	bestIdx := 0
	bestMissingRequired := int(^uint(0) >> 1)
	bestPlaceholderCount := int(^uint(0) >> 1)
	for i, step := range steps {
		cmd, _ := step.Params["command"].(string)
		placeholders := markdownCommandPlaceholderSet(cmd)
		missingRequired := markdownMissingRequiredPlaceholderCount(placeholders, requiredArgs)
		count := len(placeholders)
		if missingRequired < bestMissingRequired || (missingRequired == bestMissingRequired && count < bestPlaceholderCount) {
			bestIdx = i
			bestMissingRequired = missingRequired
			bestPlaceholderCount = count
		}
	}
	return bestIdx
}

func markdownCommandPlaceholderCount(command string) int {
	return len(markdownCommandPlaceholderSet(command))
}

func markdownCommandPlaceholderSet(command string) map[string]bool {
	seen := make(map[string]bool)
	for _, key := range ExtractPlaceholderKeys(firstSignificantLine(command)) {
		canonical := canonicalRunVarKey(key)
		if canonical != "" {
			seen[canonical] = true
		}
	}
	return seen
}

func markdownMissingRequiredPlaceholderCount(placeholders map[string]bool, requiredArgs []string) int {
	missing := 0
	for _, arg := range requiredArgs {
		canonical := canonicalRunVarKey(arg)
		if canonical != "" && !placeholders[canonical] {
			missing++
		}
	}
	return missing
}

func parameterizeMarkdownUsageCommand(block string, context markdownBashBlockContext) (string, bool) {
	if !markdownHeadingsHaveUsageMarker(context.Headings) || markdownHeadingsHaveWorkflowMarker(context.Headings) {
		return "", false
	}
	line := stripInlineShellComment(firstSignificantLine(block))
	if line == "" || len(ExtractPlaceholderKeys(line)) > 0 {
		return "", false
	}
	fields := splitMarkdownCommandFields(line)
	if len(fields) < 2 {
		return "", false
	}
	for i := 1; i < len(fields); i++ {
		field := strings.TrimSpace(fields[i])
		if field == "" {
			continue
		}
		if replacement, ok := parameterizeMarkdownInputFlagAssignment(field); ok {
			fields[i] = replacement
			return strings.Join(fields, " "), true
		}
		if strings.HasPrefix(field, "-") || isNonInputMarkdownCommandOptionValue(fields, i) {
			continue
		}
		if !looksLikeMarkdownSampleInputPath(field) {
			continue
		}
		fields[i] = "{{input}}"
		return strings.Join(fields, " "), true
	}
	return "", false
}

func parameterizeMarkdownInputFlagAssignment(field string) (string, bool) {
	flag, value, ok := strings.Cut(strings.TrimSpace(field), "=")
	if !ok || !strings.HasPrefix(flag, "-") || !isInputOptionFlag(flag) {
		return "", false
	}
	if !looksLikeMarkdownSampleInputPath(value) {
		return "", false
	}
	return flag + "={{input}}", true
}

func isNonInputMarkdownCommandOptionValue(fields []string, idx int) bool {
	if idx <= 0 || idx >= len(fields) {
		return false
	}
	prev := strings.TrimSpace(fields[idx-1])
	if prev == "" || !strings.HasPrefix(prev, "-") {
		return false
	}
	return !strings.Contains(prev, "=") && !isInputOptionFlag(prev)
}

func isInputOptionFlag(flag string) bool {
	flag = strings.TrimLeft(strings.ToLower(strings.TrimSpace(flag)), "-")
	flag = strings.ReplaceAll(flag, "_", "-")
	switch flag {
	case "input", "in", "i", "file", "f", "path", "source", "src", "document", "pdf", "image":
		return true
	default:
		return false
	}
}

func looksLikeMarkdownSampleInputPath(raw string) bool {
	value := strings.Trim(strings.TrimSpace(raw), "\"'`")
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		return looksLikeMarkdownSampleInputName(strings.Trim(value, "<>"))
	}
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, "{}") {
		return false
	}
	if strings.ContainsAny(value, "<>") {
		matches := anglePlaceholderRe.FindAllString(value, -1)
		if len(matches) != 1 {
			return false
		}
		expanded := strings.Replace(value, matches[0], strings.Trim(matches[0], "<>"), 1)
		if strings.ContainsAny(expanded, "<>") {
			return false
		}
		return looksLikeMarkdownSampleInputName(expanded) || looksLikeMarkdownSampleInputName(matches[0])
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(value, `\`, `/`)))
	if base == "" || strings.HasPrefix(base, "-") {
		return false
	}
	if looksLikeMarkdownSampleInputName(base) {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".pdf", ".jpg", ".jpeg", ".png", ".bmp", ".tiff", ".tif", ".webp", ".md", ".txt", ".csv", ".json", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx":
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		return looksLikeMarkdownSampleInputName(stem)
	default:
		return false
	}
}

func looksLikeMarkdownSampleInputName(raw string) bool {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.Trim(name, "\"'`<>")
	if name == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, `/`)))
	if base == "" || strings.HasPrefix(base, "-") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, ext)
	normalized := strings.NewReplacer("_", "-", " ", "-", ".", "-").Replace(stem)
	switch normalized {
	case "input", "file", "document", "doc", "report", "sample", "example", "photo", "image",
		"pdf", "markdown", "text", "csv", "json", "your-file", "your-input", "your-document",
		"your-pdf", "input-file", "input-document", "input-pdf", "input-image", "source-file",
		"source-document", "source-pdf", "pdf-file", "image-file", "document-file",
		"doc-file", "xls-file", "ppt-file", "word-file", "excel-file", "presentation-file":
		return true
	default:
		return false
	}
}

func hasUnsafeAnglePlaceholder(line string) bool {
	for _, match := range anglePlaceholderRe.FindAllString(line, -1) {
		if !looksLikeMarkdownSampleInputPath(match) {
			return true
		}
	}
	return false
}

func stripInlineShellComment(line string) string {
	line = strings.TrimSpace(line)
	var quote rune
	for i, r := range line {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
		case '#':
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return line
}

func markdownHeadingsHaveUsageMarker(headings []string) bool {
	markers := []string{
		"usage", "example", "examples", "quick start", "quickstart", "how to run",
		"commands", "invocation", "use case", "use cases", "recommended execution",
		"execution method", "execution methods", "run method", "run methods",
		"\u4f7f\u7528", "\u7528\u6cd5", "\u793a\u4f8b", "\u4f8b\u5b50", "\u8303\u4f8b",
		"\u5feb\u901f\u5f00\u59cb", "\u547d\u4ee4", "\u6267\u884c\u65b9\u5f0f",
		"\u63a8\u8350\u6267\u884c",
	}
	return markdownHeadingsContainAny(headings, markers)
}

func markdownHeadingsHaveWorkflowMarker(headings []string) bool {
	markers := []string{
		"step", "phase", "stage", "workflow", "pipeline", "sequence",
		"setup", "prepare", "cleanup", "teardown", "verify", "validate",
		"\u6b65\u9aa4", "\u9636\u6bb5", "\u6d41\u7a0b", "\u7b2c",
	}
	return markdownHeadingsContainAny(headings, markers)
}

func markdownHeadingsContainAny(headings, markers []string) bool {
	for _, heading := range headings {
		lower := strings.ToLower(strings.TrimSpace(heading))
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

func countSignificantCommandLines(block string) int {
	count := 0
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	return count
}

func markdownCommandExecutionTarget(command string) (string, bool) {
	fields := splitMarkdownCommandFields(firstSignificantLine(command))
	if len(fields) == 0 {
		return "", false
	}
	exe := normalizeCommandExecutable(fields[0])
	switch exe {
	case "python", "py":
		return interpreterCommandTarget("python", fields[1:])
	case "node", "deno", "bun":
		return interpreterCommandTarget(exe, fields[1:])
	case "bash", "sh", "pwsh", "powershell":
		return interpreterCommandTarget(exe, fields[1:])
	default:
		target := normalizeCommandTargetPath(fields[0])
		if target == "" {
			return "", false
		}
		if isScriptFileName(filepath.Base(target)) || strings.Contains(target, "/") {
			return target, true
		}
		return "", false
	}
}

func interpreterCommandTarget(exe string, args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == "-m" && i+1 < len(args) {
			module := strings.ToLower(strings.TrimSpace(args[i+1]))
			if module != "" {
				return exe + "|-m|" + module, true
			}
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		path := normalizeCommandTargetPath(arg)
		if path == "" {
			continue
		}
		base := filepath.Base(path)
		if isScriptFileName(base) || strings.Contains(path, "/") {
			return exe + "|" + path, true
		}
		return exe + "|" + path, true
	}
	return exe, exe != ""
}

func normalizeCommandExecutable(s string) string {
	s = normalizeCommandTargetPath(s)
	base := filepath.Base(s)
	switch base {
	case "python3", "python.exe", "python3.exe":
		return "python"
	case "py.exe":
		return "py"
	case "node.exe":
		return "node"
	case "powershell.exe":
		return "powershell"
	case "pwsh.exe":
		return "pwsh"
	default:
		return base
	}
}

func normalizeCommandTargetPath(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "\"'`")
	s = strings.ReplaceAll(s, `\`, "/")
	for strings.HasPrefix(s, "./") {
		s = strings.TrimPrefix(s, "./")
	}
	return strings.ToLower(s)
}

func splitMarkdownCommandFields(line string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	flush := func() {
		if current.Len() == 0 {
			return
		}
		fields = append(fields, current.String())
		current.Reset()
	}
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if quote != '\'' && r == '\\' && i+1 < len(runes) && isEscapableShellRune(runes[i+1]) {
				current.WriteRune(runes[i+1])
				i++
				continue
			}
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}
		if r == '\\' && i+1 < len(runes) && isEscapableShellRune(runes[i+1]) {
			current.WriteRune(runes[i+1])
			i++
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
		case ' ', '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return fields
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
	producesArtifact := false // default: markdown-only skills may be stdout/instructional
	if opts.ProducesArtifact != nil {
		producesArtifact = *opts.ProducesArtifact
	} else if parsed.producesArtifact != nil {
		producesArtifact = *parsed.producesArtifact
	}
	verificationMode := markdownVerificationMode(parsed.frontmatter["verification_mode"], producesArtifact)
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
	if isKnowledgeSkillType(parsed.skillType) {
		return buildMarkdownKnowledgeSkillEntry(parsed, opts, triggers, platforms, requiresGUI, time.Now().Format(time.RFC3339)), nil
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
		RequiresBins:            parsed.requiresBins,
		Operations:              parsed.operations,
		Params:                  parsed.params,
		Pipeline:                parsed.pipeline,
		Capabilities:            parsed.capabilities,
		RequiresTools:           parsed.requiresTools,
		FallbackForTools:        parsed.fallbackForTools,
		RequiresToolsets:        parsed.requiresToolsets,
		FallbackForToolsets:     parsed.fallbackForToolsets,
		RequiredCredentialFiles: parsed.requiredCredentialFiles,
		Stateful:                parsed.stateful,
	}, nil
}

func buildMarkdownKnowledgeSkillEntry(parsed *parsedSkillMarkdown, opts MarkdownSkillOptions, triggers, platforms []string, requiresGUI bool, createdAt string) *corelib.NLSkillEntry {
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().Format(time.RFC3339)
	}
	skillDir := strings.TrimSpace(opts.SkillDir)
	var references []corelib.SkillReference
	if skillDir != "" {
		references = scanReferences(skillDir)
	}
	return &corelib.NLSkillEntry{
		Name:                    parsed.name,
		Description:             parsed.description,
		Triggers:                triggers,
		Status:                  "active",
		CreatedAt:               createdAt,
		Source:                  firstNonEmpty(strings.TrimSpace(opts.Source), "file"),
		SourceProject:           firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
		TrustLevel:              strings.TrimSpace(opts.TrustLevel),
		SkillDir:                skillDir,
		Platforms:               platforms,
		RequiresGUI:             requiresGUI,
		Type:                    "knowledge",
		Content:                 parsed.markdown,
		Capabilities:            parsed.capabilities,
		RequiresTools:           parsed.requiresTools,
		FallbackForTools:        parsed.fallbackForTools,
		RequiresToolsets:        parsed.requiresToolsets,
		FallbackForToolsets:     parsed.fallbackForToolsets,
		RequiredCredentialFiles: parsed.requiredCredentialFiles,
		Stateful:                parsed.stateful,
		References:              references,
	}
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
	if isKnowledgeSkillType(parsed.skillType) {
		return buildMarkdownKnowledgeSkillEntry(parsed, opts, triggers, platforms, requiresGUI, fileModTime(mdPath)), nil
	}
	steps := make([]corelib.NLSkillStep, 0)
	scriptsDir := filepath.Join(skillDir, "scripts")

	// Build a lookup of bundled executable scripts that exist either in the
	// package root or in scripts/. Older skills often store runnable helpers
	// next to SKILL.md instead of under scripts/, and both layouts should be
	// treated as first-class local assets.
	localScripts := make(map[string]string)
	scriptEntries, _ := os.ReadDir(scriptsDir)
	for _, e := range scriptEntries {
		if !e.IsDir() && isScriptFileName(e.Name()) {
			localScripts[e.Name()] = filepath.Join(scriptsDir, e.Name())
		}
	}
	rootEntries, _ := os.ReadDir(skillDir)
	for _, e := range rootEntries {
		if !e.IsDir() && isScriptFileName(e.Name()) {
			if _, exists := localScripts[e.Name()]; !exists {
				localScripts[e.Name()] = filepath.Join(skillDir, e.Name())
			}
		}
	}

	// Extract ALL bash blocks from markdown (including {baseDir} ones) and
	// process them in document order. This ensures step execution order matches
	// the SKILL.md definition order.
	var allBlocks []string
	var blockContexts []markdownBashBlockContext
	if strings.TrimSpace(parsed.markdown) != "" {
		blockContexts = extractBashBlockContexts(parsed.markdown)
		allBlocks = make([]string, 0, len(blockContexts))
		for _, ctx := range blockContexts {
			allBlocks = append(allBlocks, ctx.Block)
		}
	}

	// Extract capture directives (<!-- extract: VAR=regex -->) for each bash block.
	// This enables step-to-step variable passing for SKILL.md-defined skills.
	var captureDirectives []map[string]string
	if strings.TrimSpace(parsed.markdown) != "" {
		captureDirectives = extractCaptureDirectives(parsed.markdown)
	}
	stepContexts := make([]markdownBashBlockContext, 0)
	inferredParams := make([]corelib.NLSkillParam, 0)

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

			// Check if this block references a local script in scripts/.
			// If so, use the script-based command (with proper quoting etc.)
			var localScriptPath string
			for _, line := range strings.Split(resolved, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				for _, field := range splitMarkdownCommandFields(line) {
					field = strings.Trim(field, "\"'`")
					base := filepath.Base(field)
					if isScriptFileName(base) && localScripts[base] != "" {
						localScriptPath = localScripts[base]
						break
					}
				}
				if localScriptPath != "" {
					break
				}
			}

			scriptParams := readLocalScriptParams(localScriptPath)
			normalizedLocalCommand := ""
			if localScriptPath != "" {
				normalizedLocalCommand = normalizeImportedScriptCommandWithParams(rewriteImportedLocalScriptCommand(resolved, localScriptPath), scriptParams)
			}

			// Check if the resolved block is executable (not a usage example).
			if !isResolvedBlockExecutable(resolved, skillDir) {
				if strings.TrimSpace(normalizedLocalCommand) == "" || !isParameterizedMarkdownCommandExecutable(normalizedLocalCommand, skillDir) {
					continue
				}
			}
			if currentBlockIdx < len(blockContexts) && isMarkdownDependencyInstallBlock(resolved, blockContexts[currentBlockIdx]) {
				log.Printf("[skill-parser] %s: skipping dependency/install example block", parsed.name)
				continue
			}

			var command string
			if localScriptPath != "" {
				// Use the current bash block, not the first matching line in the
				// whole document, so alternative invocations keep their own args.
				command = normalizedLocalCommand
				if strings.TrimSpace(command) == "" {
					continue
				}
				command = AppendRunParamPlaceholders(command, scriptParams)
				inferredParams = mergeUniqueSkillParams(inferredParams, scriptParams)
				log.Printf("[skill-parser] %s: step from script ref: %s", parsed.name, filepath.Base(localScriptPath))
			} else if parameterized, ok := parameterizeMarkdownUsageCommand(resolved, blockContexts[currentBlockIdx]); ok {
				command = parameterized
				log.Printf("[skill-parser] %s: step from parameterized usage example: %s", parsed.name, command)
			} else {
				// Direct bash command blocks may still document a canonical
				// --input/--output invocation. Normalize those sample arguments
				// before preserving the block so legacy Office examples do not
				// hard-code a fixture path into a reusable skill.
				command = parameterizeMarkdownSampleCommandArgs(resolved)
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
			if currentBlockIdx < len(blockContexts) && blockContexts[currentBlockIdx].Shell != "" {
				params["preferred_shell"] = blockContexts[currentBlockIdx].Shell
			}
			// Attach capture directives from <!-- extract: VAR=regex --> comments
			// preceding this bash block, enabling step-to-step variable passing.
			var capture map[string]string
			if currentBlockIdx < len(captureDirectives) && captureDirectives[currentBlockIdx] != nil {
				capture = captureDirectives[currentBlockIdx]
				log.Printf("[skill-parser] %s: step %d has capture directives: %v", parsed.name, len(steps)+1, capture)
			}
			steps = append(steps, corelib.NLSkillStep{Action: "bash", Params: params, OnError: "stop", Capture: capture})
			if currentBlockIdx < len(blockContexts) {
				stepContexts = append(stepContexts, blockContexts[currentBlockIdx])
			} else {
				stepContexts = append(stepContexts, markdownBashBlockContext{})
			}
		}
	}

	// Fallback: if no bash blocks exist in markdown but the package bundles
	// runnable scripts, include them as steps (legacy behavior for skills
	// without inline bash blocks in their SKILL.md).
	if len(steps) == 0 && len(localScripts) > 0 {
		log.Printf("[skill-parser] %s: no bash blocks produced steps, falling back to bundled script scan (%d files)",
			parsed.name, len(localScripts))
		scriptPaths := make([]string, 0, len(localScripts))
		for _, scriptPath := range localScripts {
			scriptPaths = append(scriptPaths, scriptPath)
		}
		slices.Sort(scriptPaths)
		for _, scriptPath := range scriptPaths {
			scriptParams := readLocalScriptParams(scriptPath)
			command, ok := scriptExecutionCommandFromMarkdown(scriptPath, parsed.markdown, skillDir)
			if !ok {
				continue
			}
			command = AppendRunParamPlaceholders(command, scriptParams)
			inferredParams = mergeUniqueSkillParams(inferredParams, scriptParams)
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
	producesArtifact := false
	if opts.ProducesArtifact != nil {
		producesArtifact = *opts.ProducesArtifact
	} else if parsed.producesArtifact != nil {
		producesArtifact = *parsed.producesArtifact
	}
	effectiveExecMode := parsed.execMode
	opBlocks := extractOperationLabeledBlocks(parsed.markdown)
	// Apply execMode: "first" when declared explicitly.
	if strings.EqualFold(strings.TrimSpace(parsed.execMode), "first") && len(steps) > 1 {
		log.Printf("[skill-parser] %s: exec_mode=first, keeping only first step out of %d", parsed.name, len(steps))
		steps = steps[:1]
		if len(stepContexts) > 1 {
			stepContexts = stepContexts[:1]
		}
		effectiveExecMode = "first"
	} else if shouldAutoSelectSingleMarkdownAlternative(parsed.execMode, parsed.mode, parsed.operations, opBlocks, steps, stepContexts) {
		selectedIdx := preferredMarkdownAlternativeStepIndex(steps, parsed.requiredArgs)
		log.Printf("[skill-parser] %s: usage-style alternatives detected, keeping step %d out of %d", parsed.name, selectedIdx+1, len(steps))
		steps = steps[selectedIdx : selectedIdx+1]
		stepContexts = stepContexts[selectedIdx : selectedIdx+1]
		effectiveExecMode = "first"
	}

	// Apply operation labels from <!-- operation: xxx --> comments.
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
		ExecMode:                effectiveExecMode,
		GlobalTimeout:           parsed.timeout,
		ProducesArtifact:        producesArtifact,
		RequiredArgs:            parsed.requiredArgs,
		RequiredEnv:             parsed.requiredEnv,
		PreferredShell:          parsed.preferredShell,
		RequiresPython:          parsed.requiresPython,
		RequiresNode:            parsed.requiresNode,
		RequiresBins:            parsed.requiresBins,
		Operations:              parsed.operations,
		Params:                  mergeUniqueSkillParams(parsed.params, inferredParams),
		Pipeline:                parsed.pipeline,
		Capabilities:            parsed.capabilities,
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
	return normalizeImportedScriptCommandWithParams(command, nil)
}

func normalizeImportedScriptCommandWithParams(command string, params []corelib.NLSkillParam) string {
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
	command = parameterizeMarkdownSampleCommandArgs(replacer.Replace(command))
	return parameterizeMarkdownDeclaredPlaceholders(command, params)
}

func rewriteImportedLocalScriptCommand(block, localScriptPath string) string {
	command := joinMarkdownShellContinuationLines(block)
	if command == "" {
		return ""
	}
	base := filepath.Base(localScriptPath)
	for _, field := range splitMarkdownCommandFields(command) {
		token := strings.Trim(field, "\"'`")
		if filepath.Base(strings.ReplaceAll(token, `\`, `/`)) != base {
			continue
		}
		return replaceMarkdownCommandTokenOnce(command, token, quoteScriptPath(localScriptPath))
	}
	return command
}

func parameterizeMarkdownDeclaredPlaceholders(command string, params []corelib.NLSkillParam) string {
	fields := strings.Fields(command)
	result := command
	for i, field := range fields {
		if replacement, ok := parameterizeMarkdownDeclaredFlagAssignment(field, params); ok {
			result = replaceCommandTokenOnce(result, field, replacement)
			continue
		}
		trimmed := strings.TrimSpace(field)
		if !strings.HasPrefix(trimmed, "-") {
			if name := markdownPlaceholderTokenName(trimmed); name != "" {
				result = replaceCommandTokenOnce(result, field, "{{"+name+"}}")
			}
			continue
		}
		if i+1 >= len(fields) {
			continue
		}
		name := markdownCLIFlagParamName(trimmed, params)
		if name == "" || !isMarkdownDocPlaceholderToken(fields[i+1]) {
			continue
		}
		result = replaceCommandTokenOnce(result, fields[i+1], "{{"+name+"}}")
	}
	return result
}

func parameterizeMarkdownDeclaredFlagAssignment(field string, params []corelib.NLSkillParam) (string, bool) {
	flag, value, ok := strings.Cut(strings.TrimSpace(field), "=")
	if !ok || !strings.HasPrefix(flag, "-") || !isMarkdownDocPlaceholderToken(value) {
		return "", false
	}
	name := markdownCLIFlagParamName(flag, params)
	if name == "" {
		return "", false
	}
	return flag + "={{" + name + "}}", true
}

func markdownCLIFlagParamName(flag string, params []corelib.NLSkillParam) string {
	flagKey := canonicalRunVarKey(strings.TrimLeft(strings.TrimSpace(flag), "-"))
	if flagKey == "" {
		return ""
	}
	for _, param := range params {
		if cliFlag := strings.TrimSpace(param.CLIFlag); cliFlag != "" && canonicalRunVarKey(strings.TrimLeft(cliFlag, "-")) == flagKey {
			return canonicalRunVarKey(param.Name)
		}
	}
	return flagKey
}

func markdownPlaceholderTokenName(token string) string {
	if !isMarkdownDocPlaceholderToken(token) {
		return ""
	}
	inner := strings.TrimSpace(strings.Trim(strings.TrimSpace(token), "<>[]"))
	if inner == "" {
		return ""
	}
	// Standalone positional placeholders should already look like a param
	// name. Flag-bound placeholders may be localized and are handled by the
	// preceding CLI flag instead.
	if !asciiPlaceholderRe.MatchString(inner) {
		return ""
	}
	return canonicalRunVarKey(inner)
}

func isMarkdownDocPlaceholderToken(token string) bool {
	token = strings.TrimSpace(token)
	if len(token) < 3 {
		return false
	}
	return (strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">")) ||
		(strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]"))
}

func replaceMarkdownCommandTokenOnce(command, token, replacement string) string {
	if token == "" || replacement == "" {
		return command
	}
	candidates := []string{
		`"` + token + `"`,
		`'` + token + `'`,
		"`" + token + "`",
		strings.ReplaceAll(token, " ", `\ `),
		token,
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(command, candidate) {
			return strings.Replace(command, candidate, replacement, 1)
		}
	}
	return command
}

func joinMarkdownShellContinuationLines(block string) string {
	var commands []string
	var current strings.Builder
	for _, rawLine := range strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		continued := strings.HasSuffix(line, `\`) || strings.HasSuffix(line, "`")
		if continued {
			line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, `\`), "`"))
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(line)
		if continued {
			continue
		}
		if strings.TrimSpace(current.String()) != "" {
			commands = append(commands, strings.TrimSpace(current.String()))
		}
		current.Reset()
	}
	if strings.TrimSpace(current.String()) != "" {
		commands = append(commands, strings.TrimSpace(current.String()))
	}
	return strings.Join(commands, "\n")
}

func parameterizeMarkdownSampleCommandArgs(command string) string {
	fields := strings.Fields(command)
	result := command
	inputParameterized := false
	for i, field := range fields {
		if replacement, ok := parameterizeMarkdownFlagAssignment(field); ok {
			result = replaceCommandTokenOnce(result, field, replacement)
			continue
		}
		trimmed := strings.TrimSpace(field)
		if !strings.HasPrefix(trimmed, "-") {
			// A direct usage block commonly has its input as the first positional
			// argument (for example `parser report.doc --output result.doc`).
			// Restrict this to known sample names so ordinary commands and arbitrary
			// positional arguments remain unchanged.
			if !inputParameterized && !isNonInputMarkdownCommandOptionValue(fields, i) && looksLikeMarkdownSampleInputPath(field) {
				result = replaceCommandTokenOnce(result, field, "{{input}}")
				inputParameterized = true
			}
			continue
		}
		if i+1 >= len(fields) {
			continue
		}
		next := fields[i+1]
		switch {
		case isInputOptionFlag(field) && looksLikeMarkdownSampleInputPath(next):
			result = replaceCommandTokenOnce(result, next, "{{input}}")
		case isOutputOptionFlag(field) && looksLikeMarkdownSampleOutputPath(next):
			result = replaceCommandTokenOnce(result, next, "{{output}}")
		}
	}
	return result
}

func parameterizeMarkdownFlagAssignment(field string) (string, bool) {
	flag, value, ok := strings.Cut(strings.TrimSpace(field), "=")
	if !ok || !strings.HasPrefix(flag, "-") {
		return "", false
	}
	switch {
	case isInputOptionFlag(flag) && looksLikeMarkdownSampleInputPath(value):
		return flag + "={{input}}", true
	case isOutputOptionFlag(flag) && looksLikeMarkdownSampleOutputPath(value):
		return flag + "={{output}}", true
	default:
		return "", false
	}
}

func replaceCommandTokenOnce(command, token, replacement string) string {
	if token == "" || replacement == "" {
		return command
	}
	return strings.Replace(command, token, replacement, 1)
}

func isParameterizedMarkdownCommandExecutable(block, skillDir string) bool {
	slashDir := ""
	if skillDir != "" {
		slashDir = filepath.ToSlash(strings.TrimRight(skillDir, `/\`))
	}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "{baseDir}") || strings.Contains(line, "{base_dir}") {
			return false
		}
		if hasChinesePathSegments(line) {
			if slashDir == "" || !strings.Contains(line, slashDir) {
				return false
			}
		}
		if hasUnsafeAnglePlaceholder(line) {
			return false
		}
	}
	return true
}

func readLocalScriptParams(scriptPath string) []corelib.NLSkillParam {
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return nil
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil
	}
	params := ExtractScriptParams(string(data), scriptLanguageForPath(scriptPath))
	for i := range params {
		// A bundled script's parser/signature is a concrete CLI contract, not
		// a loose template guess. Treat it like an imported schema so CLIFlag
		// appending and optional-parameter ownership work for legacy scripts.
		params[i].Synthetic = false
	}
	return params
}

func scriptLanguageForPath(scriptPath string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(scriptPath))) {
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "node"
	case ".sh":
		return "bash"
	case ".ps1":
		return "powershell"
	default:
		return ""
	}
}

func mergeUniqueSkillParams(base, extras []corelib.NLSkillParam) []corelib.NLSkillParam {
	if len(extras) == 0 {
		return append([]corelib.NLSkillParam(nil), base...)
	}
	result := make([]corelib.NLSkillParam, 0, len(base)+len(extras))
	seen := map[string]bool{}
	add := func(param corelib.NLSkillParam) {
		key := canonicalRunVarKey(param.Name)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		result = append(result, param)
	}
	for _, param := range base {
		add(param)
	}
	for _, param := range extras {
		add(param)
	}
	return result
}

func isOutputOptionFlag(flag string) bool {
	flag = strings.TrimLeft(strings.ToLower(strings.TrimSpace(flag)), "-")
	flag = strings.ReplaceAll(flag, "_", "-")
	switch flag {
	case "output", "out", "o", "dest", "destination", "target", "output-file", "output-path", "pdf-output", "save-as":
		return true
	default:
		return false
	}
}

func looksLikeMarkdownSampleOutputPath(raw string) bool {
	value := strings.Trim(strings.TrimSpace(raw), "\"'`")
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		return looksLikeMarkdownSampleOutputName(strings.Trim(value, "<>"))
	}
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, "{}") {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(value, `\`, `/`)))
	if base == "" || strings.HasPrefix(base, "-") {
		return false
	}
	if looksLikeMarkdownSampleOutputName(base) {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".pdf", ".md", ".txt", ".csv", ".json", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func looksLikeMarkdownSampleOutputName(raw string) bool {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.Trim(name, "\"'`<>")
	name = strings.TrimSuffix(name, filepath.Ext(name))
	normalized := strings.NewReplacer("_", "-", " ", "-", ".", "-").Replace(name)
	switch normalized {
	case "output", "out", "result", "target", "destination", "dest", "report", "document",
		"output-file", "output-document", "output-pdf", "result-file", "result-pdf",
		"your-output", "your-output-file", "your-pdf":
		return true
	default:
		return false
	}
}

func isMarkdownDependencyInstallBlock(block string, context markdownBashBlockContext) bool {
	if markdownHeadingsHaveUsageMarker(context.Headings) || markdownHeadingsHaveWorkflowMarker(context.Headings) {
		return false
	}
	if !markdownHeadingsHaveDependencyMarker(context.Headings) {
		return false
	}
	return isDependencyInstallCommand(firstSignificantLine(joinMarkdownShellContinuationLines(block)))
}

func markdownHeadingsHaveDependencyMarker(headings []string) bool {
	markers := []string{
		"install", "installation", "setup", "dependency", "dependencies", "requirement", "requirements",
		"compatibility", "environment", "prerequisite", "prerequisites",
		"\u5b89\u88c5", "\u4f9d\u8d56", "\u73af\u5883", "\u524d\u7f6e", "\u51c6\u5907",
	}
	for _, heading := range headings {
		heading = strings.ToLower(strings.TrimSpace(heading))
		for _, marker := range markers {
			if strings.Contains(heading, marker) {
				return true
			}
		}
	}
	return false
}

func isDependencyInstallCommand(line string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(line)))
	if len(fields) == 0 {
		return false
	}
	if len(fields) >= 3 && (fields[0] == "python" || fields[0] == "python3" || fields[0] == "py") && fields[1] == "-m" && fields[2] == "pip" {
		return len(fields) >= 4 && fields[3] == "install"
	}
	if len(fields) >= 2 {
		switch fields[0] {
		case "pip", "pip3":
			return fields[1] == "install"
		case "npm", "pnpm":
			return fields[1] == "install" || fields[1] == "add"
		case "yarn":
			return fields[1] == "add" || fields[1] == "install"
		case "uv":
			return len(fields) >= 3 && fields[1] == "pip" && fields[2] == "install"
		}
	}
	return false
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
	mdPath, err := findSkillMarkdownDocPath(skillDir)
	if err != nil {
		return "", fmt.Errorf("cannot read skill markdown: %w", err)
	}
	return mdPath, nil
}

type parsedSkillMarkdown struct {
	markdown       string
	body           string
	name           string
	description    string
	compatibility  string
	skillType      string
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
	requiresBins            []string // from frontmatter requires.bins (YAML list)
	operations              []corelib.NLSkillOperation
	params                  []corelib.NLSkillParam
	pipeline                []corelib.SkillPipelineStep
	capabilities            []string
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
		skillType:               meta.skillType,
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
		requiresBins:            meta.requiresBins,
		operations:              meta.operations,
		params:                  meta.params,
		pipeline:                meta.pipeline,
		capabilities:            meta.capabilities,
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
	requiresBins            []string
	operations              []corelib.NLSkillOperation
	params                  []corelib.NLSkillParam
	pipeline                []corelib.SkillPipelineStep
	requiresTools           []string
	fallbackForTools        []string
	requiresToolsets        []string
	fallbackForToolsets     []string
	requiredCredentialFiles []string
	stateful                bool
	skillType               string
	capabilities            []string
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
	m.skillType = firstNonEmpty(strings.TrimSpace(sf.Type), yamlString(normalized["type"]))
	m.producesArtifact = yamlBool(normalized["produces_artifact"])
	m.requiresPython = requiresPythonFromYAML(sf.Requires)
	m.requiresNode = requiresNodeFromYAML(sf.Requires)
	m.requiresBins = requiresBinsFromYAML(sf.Requires)
	m.operations = convertSkillYAMLOperations(sf.Operations)
	m.params = convertSkillYAMLParams(sf.Params)
	m.pipeline = convertPipelineSteps(sf.Pipeline)
	m.capabilities = sf.Capabilities
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
	case map[string]interface{}:
		result := make([]string, 0, len(val))
		for key := range val {
			if s := strings.TrimSpace(key); s != "" {
				result = append(result, s)
			}
		}
		slices.Sort(result)
		return result
	case map[string]string:
		result := make([]string, 0, len(val))
		for key := range val {
			if s := strings.TrimSpace(key); s != "" {
				result = append(result, s)
			}
		}
		slices.Sort(result)
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

func markdownVerificationMode(explicit string, producesArtifact bool) string {
	if mode := strings.TrimSpace(explicit); mode != "" {
		return mode
	}
	if producesArtifact {
		return "artifact_required"
	}
	return "artifact_optional"
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
//	python3 session_example.py "hello"
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

		// Check for executable shell block start (exclude .norun/no-run).
		if _, norun, ok := parseMarkdownShellFenceStart(trimmed); ok && !norun {
			inBashBlock = true
			blockContent.Reset()
			continue
		}

		// .norun/no-run shell blocks: consume them without emitting, and clear pending captures.
		if _, norun, ok := parseMarkdownShellFenceStart(trimmed); ok && norun {
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
