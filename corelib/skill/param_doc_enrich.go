package skill

import (
	"os"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// paramDocHint is a lightweight description extracted from SKILL.md / README.
type paramDocHint struct {
	Description string
	Type        string
	Required    bool
	HasRequired bool
}

var (
	// - `input`: path to file
	// - **format** (required): output format
	// * name - description
	paramDocBulletRe = regexp.MustCompile(`(?i)^[\s>*+-]*` +
		`(?:\*\*|__|\x60)?` +
		`([A-Za-z_][A-Za-z0-9_-]*)` +
		`(?:\*\*|__|\x60)?` +
		`(?:\s*[\(\[]\s*(required|optional|必需|可选)\s*[\)\]])?` +
		`\s*[:：\-–—]\s*(.+?)\s*$`)

	// | input | path to file | yes |
	// | `format` | output format |
	paramDocTableRe = regexp.MustCompile(`(?i)^\s*\|` +
		`\s*(?:\*\*|__|\x60)?` +
		`([A-Za-z_][A-Za-z0-9_-]*)` +
		`(?:\*\*|__|\x60)?\s*\|` +
		`\s*([^|]+?)\s*\|` +
		`(?:\s*([^|]*?)\s*\|)?`)

	// --input PATH  Input file path
	// --format, -f  Output format
	paramDocCLIRe = regexp.MustCompile(`(?i)^\s*(?:[-*]+\s+)?` +
		`(?:--([A-Za-z_][A-Za-z0-9_-]*))` +
		`(?:\s*,\s*-[A-Za-z])?` +
		`(?:\s+[A-Z][A-Z0-9_]*)?` +
		`\s{2,}(.+?)\s*$`)

	// input (string, required): description
	paramDocTypedBulletRe = regexp.MustCompile(`(?i)^[\s>*+-]*` +
		`(?:\*\*|__|\x60)?` +
		`([A-Za-z_][A-Za-z0-9_-]*)` +
		`(?:\*\*|__|\x60)?` +
		`\s*\(\s*([a-z]+)` +
		`(?:\s*,\s*(required|optional|必需|可选))?\s*\)` +
		`\s*[:：\-–—]\s*(.+?)\s*$`)
)

// ExtractParamHintsFromDoc parses skill documentation for parameter hints.
// It prefers sections titled Parameters / Arguments / 参数 / 入参 when present,
// otherwise scans the full document with conservative patterns. CLI-style
// `--flag` lines are also scanned from the whole document (Usage sections).
func ExtractParamHintsFromDoc(doc string) map[string]paramDocHint {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return nil
	}
	section := extractParamDocSection(doc)
	if section == "" {
		section = doc
	}
	hints := map[string]paramDocHint{}
	parseParamDocLines(section, hints, true)
	// CLI usage often lives outside the Parameters section.
	if section != doc {
		parseParamDocLines(doc, hints, false)
	}
	if len(hints) == 0 {
		return nil
	}
	return hints
}

// parseParamDocLines fills hints from markdown lines.
// When full=true, accepts bullets/tables/typed/CLI. When false, only CLI lines
// (to harvest Usage without re-scanning noisy freeform bullets).
func parseParamDocLines(text string, hints map[string]paramDocHint, full bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip markdown table separators.
		if strings.HasPrefix(line, "|") && strings.Contains(line, "---") {
			continue
		}
		if full {
			if m := paramDocTypedBulletRe.FindStringSubmatch(line); len(m) == 5 {
				mergeParamDocHint(hints, m[1], m[4], m[2], m[3])
				continue
			}
			if m := paramDocBulletRe.FindStringSubmatch(line); len(m) == 4 {
				mergeParamDocHint(hints, m[1], m[3], "", m[2])
				continue
			}
			if m := paramDocTableRe.FindStringSubmatch(line); len(m) >= 3 {
				reqMark := ""
				if len(m) >= 4 {
					reqMark = m[3]
				}
				// Avoid treating header row "Name | Description" as a param.
				name := strings.ToLower(strings.TrimSpace(m[1]))
				if name == "name" || name == "param" || name == "parameter" || name == "arg" || name == "argument" || name == "参数" || name == "字段" {
					continue
				}
				mergeParamDocHint(hints, m[1], m[2], "", reqMark)
				continue
			}
		}
		if m := paramDocCLIRe.FindStringSubmatch(line); len(m) == 3 {
			mergeParamDocHint(hints, m[1], m[2], "", "")
			continue
		}
	}
}

func extractParamDocSection(doc string) string {
	lines := strings.Split(doc, "\n")
	start := -1
	baseLevel := 2
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
		heading = strings.TrimSpace(strings.Trim(heading, "*:："))
		if !isParamDocHeading(heading) {
			continue
		}
		start = i + 1
		baseLevel = markdownHeadingLevel(line)
		break
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			continue
		}
		if markdownHeadingLevel(lines[i]) <= baseLevel {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func isParamDocHeading(heading string) bool {
	switch heading {
	case "parameters", "parameter", "params", "arguments", "argument", "args",
		"inputs", "input", "options", "usage",
		"参数", "参数说明", "入参", "输入", "输入参数", "参数列表", "命令参数":
		return true
	default:
		return false
	}
}

func markdownHeadingLevel(line string) int {
	trimmed := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 {
		return 99
	}
	return n
}

func mergeParamDocHint(dst map[string]paramDocHint, name, description, typ, requiredMark string) {
	key := canonicalRunVarKey(name)
	if key == "" {
		return
	}
	// Skip meta keys that appear as placeholders but are not user args.
	switch key {
	case "basedir", "base_dir", "skilldir", "skill_dir", "workdir", "work_dir":
		return
	}
	description = cleanParamDocDescription(description)
	if description == "" && typ == "" && requiredMark == "" {
		return
	}
	cur := dst[key]
	if cur.Description == "" && description != "" {
		cur.Description = description
	}
	if cur.Type == "" && strings.TrimSpace(typ) != "" {
		cur.Type = paramJSONSchemaType(typ)
	}
	if mark := strings.ToLower(strings.TrimSpace(requiredMark)); mark != "" {
		switch mark {
		case "required", "yes", "y", "true", "必需", "必填", "必须":
			cur.Required = true
			cur.HasRequired = true
		case "optional", "no", "n", "false", "可选", "选填":
			cur.Required = false
			cur.HasRequired = true
		}
	}
	// Description may itself say "required".
	if !cur.HasRequired {
		lower := strings.ToLower(description)
		if strings.Contains(lower, "(required)") || strings.Contains(description, "必需") || strings.Contains(description, "必填") {
			cur.Required = true
			cur.HasRequired = true
		}
	}
	dst[key] = cur
}

func cleanParamDocDescription(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "`\"'")
	s = strings.TrimSpace(s)
	// Drop trailing table junk / badges.
	if i := strings.Index(s, "|"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	// Cap length to keep prompt/tool responses compact.
	runes := []rune(s)
	if len(runes) > 160 {
		s = string(runes[:157]) + "..."
	}
	return s
}

// commonParamDescriptions provides last-resort human descriptions for widely
// used skill argument names when neither skill.yaml nor SKILL.md documents them.
// Keys must be canonicalRunVarKey form. Keep entries generic and safe.
var commonParamDescriptions = map[string]string{
	"input":        "Input file path, URL, or primary content for this skill",
	"input_file":   "Path to the input file",
	"input_path":   "Path to the input file or directory",
	"output":       "Output file path or destination for the result",
	"output_file":  "Path where the result should be written",
	"output_path":  "Path where the result should be written",
	"file":         "Path to the file to process",
	"path":         "Filesystem path used as input",
	"source":       "Source file path or input location",
	"src":          "Source file path or input location",
	"dest":         "Destination path for the output",
	"destination":  "Destination path for the output",
	"target":       "Target path or resource to operate on",
	"text":         "Plain text content to process",
	"content":      "Text or document content to process",
	"message":      "Message text to process or send",
	"msg":          "Message text to process or send",
	"prompt":       "Instruction or prompt text for generation",
	"description":  "Natural-language description of the task or artifact",
	"task":         "Task description or instruction",
	"query":        "Search or question query text",
	"q":            "Search or question query text",
	"question":     "Question text to answer",
	"format":       "Output format (for example pdf, png, json, markdown)",
	"fmt":          "Output format (for example pdf, png, json, markdown)",
	"output_format": "Output format (for example pdf, png, json, markdown)",
	"type":         "Type or format selector for this skill",
	"action":       "Named operation or mode to run",
	"mode":         "Execution mode or named operation",
	"operation":    "Named operation to execute",
	"url":          "HTTP(S) URL to fetch or open",
	"uri":          "Resource URI to use as input",
	"link":         "Link/URL to process",
	"city":         "City name",
	"lang":         "Language code or name (for example en, zh)",
	"language":     "Language code or name (for example en, zh)",
	"locale":       "Locale identifier (for example en-US, zh-CN)",
	"timeout":      "Timeout in seconds",
	"limit":        "Maximum number of items to process or return",
	"count":        "Count or quantity",
	"max":          "Maximum value or upper bound",
	"min":          "Minimum value or lower bound",
	"name":         "Name of the target resource",
	"title":        "Title text",
	"id":           "Identifier of the target resource",
	"session_id":   "Session identifier to reuse or attach to",
	"project":      "Project name or path",
	"project_dir":  "Project directory path",
	"workdir":      "Working directory for execution",
	"work_dir":     "Working directory for execution",
	"cwd":          "Working directory for execution",
	"dir":          "Directory path",
	"directory":    "Directory path",
	"config":       "Configuration file path or config value",
	"config_path":  "Path to a configuration file",
	"template":     "Template content or template file path",
	"model":        "Model name or identifier",
	"api_key":      "API key credential (prefer env when possible)",
	"token":        "Access token credential (prefer env when possible)",
	"username":     "Username for authentication",
	"password":     "Password credential (prefer env when possible)",
	"email":        "Email address",
	"host":         "Hostname or host:port",
	"port":         "Network port number",
	"dry_run":      "When true, preview without making changes",
	"force":        "When true, force the operation despite warnings",
	"verbose":      "When true, enable verbose logging",
	"debug":        "When true, enable debug output",
	"recursive":    "When true, process recursively",
	"overwrite":    "When true, overwrite existing outputs",
	"encoding":     "Text encoding (for example utf-8)",
	"charset":      "Character set / encoding",
	"theme":        "Theme or style name",
	"style":        "Style name or style options",
	"width":        "Width in pixels or layout units",
	"height":       "Height in pixels or layout units",
	"quality":      "Quality level for conversion or export",
	"page":         "Page number or page range",
	"pages":        "Page numbers or page range",
	"sheet":        "Spreadsheet sheet name or index",
	"sheet_name":   "Spreadsheet sheet name",
	"table":        "Table name",
	"sql":          "SQL statement to execute",
	"data":         "Input data payload",
	"json":         "JSON data or JSON file path",
	"payload":      "Request or job payload",
	"headers":      "HTTP headers or header map",
	"method":       "HTTP method (GET, POST, ...)",
	"body":         "Request body content",
	"user_prompt":  "Original user request text for this run",
}

// commonParamTypes provides optional JSON Schema type fallbacks for common names.
var commonParamTypes = map[string]string{
	"timeout":  "number",
	"limit":    "integer",
	"count":    "integer",
	"max":      "integer",
	"min":      "integer",
	"port":     "integer",
	"width":    "integer",
	"height":   "integer",
	"page":     "integer",
	"quality":  "number",
	"dry_run":  "boolean",
	"force":    "boolean",
	"verbose":  "boolean",
	"debug":    "boolean",
	"recursive": "boolean",
	"overwrite": "boolean",
}

// ApplyCommonParamDescriptionFallbacks fills empty Description (and Type when
// empty) using well-known parameter name heuristics. Never overwrites explicit
// or doc-derived fields. Safe for synthetic template params.
func ApplyCommonParamDescriptionFallbacks(params []corelib.NLSkillParam) []corelib.NLSkillParam {
	if len(params) == 0 {
		return params
	}
	out := make([]corelib.NLSkillParam, len(params))
	copy(out, params)
	for i := range out {
		key := canonicalRunVarKey(out[i].Name)
		if key == "" {
			continue
		}
		if strings.TrimSpace(out[i].Description) == "" {
			if desc := commonParamDescriptionFor(key, out[i].Aliases); desc != "" {
				out[i].Description = desc
			}
		}
		if strings.TrimSpace(out[i].Type) == "" {
			if typ := commonParamTypeFor(key, out[i].Aliases); typ != "" {
				out[i].Type = typ
			}
		}
	}
	return out
}

func commonParamDescriptionFor(name string, aliases []string) string {
	if desc, ok := commonParamDescriptions[name]; ok {
		return desc
	}
	for _, alias := range aliases {
		if desc, ok := commonParamDescriptions[canonicalRunVarKey(alias)]; ok {
			return desc
		}
	}
	// Light compound heuristics: *_file / *_path / *_dir / *_url
	switch {
	case strings.HasSuffix(name, "_file"):
		return "Path to a file used as " + strings.TrimSuffix(name, "_file")
	case strings.HasSuffix(name, "_path"):
		return "Filesystem path for " + strings.TrimSuffix(name, "_path")
	case strings.HasSuffix(name, "_dir") || strings.HasSuffix(name, "_directory"):
		base := strings.TrimSuffix(strings.TrimSuffix(name, "_directory"), "_dir")
		return "Directory path for " + base
	case strings.HasSuffix(name, "_url") || strings.HasSuffix(name, "_uri"):
		return "URL for " + strings.TrimSuffix(strings.TrimSuffix(name, "_url"), "_uri")
	case strings.HasSuffix(name, "_id"):
		return "Identifier for " + strings.TrimSuffix(name, "_id")
	case strings.HasPrefix(name, "is_") || strings.HasPrefix(name, "has_") || strings.HasPrefix(name, "enable_"):
		return "Boolean flag: " + name
	}
	return ""
}

func commonParamTypeFor(name string, aliases []string) string {
	if typ, ok := commonParamTypes[name]; ok {
		return typ
	}
	for _, alias := range aliases {
		if typ, ok := commonParamTypes[canonicalRunVarKey(alias)]; ok {
			return typ
		}
	}
	switch {
	case strings.HasPrefix(name, "is_") || strings.HasPrefix(name, "has_") || strings.HasPrefix(name, "enable_"):
		return "boolean"
	case strings.HasSuffix(name, "_count") || strings.HasSuffix(name, "_limit") || strings.HasSuffix(name, "_port"):
		return "integer"
	}
	return ""
}

// EnrichParamsFromDoc fills empty Description/Type (and optional Required) on
// params using hints parsed from skill documentation. Explicit non-empty fields
// are never overwritten.
func EnrichParamsFromDoc(params []corelib.NLSkillParam, doc string) []corelib.NLSkillParam {
	if len(params) == 0 {
		return params
	}
	hints := ExtractParamHintsFromDoc(doc)
	if len(hints) == 0 {
		return params
	}
	out := make([]corelib.NLSkillParam, len(params))
	copy(out, params)
	for i := range out {
		key := canonicalRunVarKey(out[i].Name)
		hint, ok := hints[key]
		if !ok {
			// Try alias match.
			for _, alias := range out[i].Aliases {
				if h, ok2 := hints[canonicalRunVarKey(alias)]; ok2 {
					hint = h
					ok = true
					break
				}
			}
		}
		if !ok {
			continue
		}
		if strings.TrimSpace(out[i].Description) == "" && hint.Description != "" {
			out[i].Description = hint.Description
		}
		if strings.TrimSpace(out[i].Type) == "" && hint.Type != "" && hint.Type != "string" {
			out[i].Type = hint.Type
		} else if strings.TrimSpace(out[i].Type) == "" && hint.Type != "" {
			out[i].Type = hint.Type
		}
		if hint.HasRequired && !out[i].Required {
			// Only promote to required from docs; never demote explicit required.
			out[i].Required = hint.Required
		}
	}
	return out
}

// EnrichParamsFromSkillDir reads SKILL.md / skill.md / README from skillDir and
// enriches params. Missing docs are a no-op.
func EnrichParamsFromSkillDir(params []corelib.NLSkillParam, skillDir string) []corelib.NLSkillParam {
	if len(params) == 0 {
		return params
	}
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return params
	}
	path, err := findSkillMarkdownDocPath(skillDir)
	if err != nil {
		return params
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return params
	}
	return EnrichParamsFromDoc(params, string(data))
}

// CompleteParamsForSkill returns the runner-complete param contract and, when
// possible, enriches missing descriptions from SkillDir / inline Content.
func CompleteParamsForSkill(entry *corelib.NLSkillEntry) []corelib.NLSkillParam {
	if entry == nil {
		return nil
	}
	params := CompleteParamsForRunner(entry.Params, entry.Steps, entry.RequiredArgs)
	return enrichParamsForDiagnostics(entry, params)
}

// enrichParamsForDiagnostics attaches doc-derived descriptions/types to an
// already-completed param list (e.g. step-scoped precheck params) without
// re-synthesizing placeholders. Common-name fallbacks run last so YAML/docs win.
func enrichParamsForDiagnostics(entry *corelib.NLSkillEntry, params []corelib.NLSkillParam) []corelib.NLSkillParam {
	if len(params) == 0 {
		return params
	}
	if entry != nil {
		if dir := strings.TrimSpace(entry.SkillDir); dir != "" {
			params = EnrichParamsFromSkillDir(params, dir)
		}
		if content := strings.TrimSpace(entry.Content); content != "" {
			params = EnrichParamsFromDoc(params, content)
		}
	}
	return ApplyCommonParamDescriptionFallbacks(params)
}
