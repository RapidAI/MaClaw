package skill

import (
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ExtractScriptParams analyzes a script's source code and extracts parameter
// definitions. This enables craft_tool-generated skills to have a proper
// params schema for BindParams alias resolution and LLM context injection.
//
// Supported patterns:
//   - Python argparse: parser.add_argument("--name", ...)
//   - Python sys.argv: sys.argv[1], sys.argv[2]
//   - Node process.argv: process.argv[2], process.argv[3]
//   - Bash positional: $1, $2, ${1}, ${2}
//   - Bash getopts: getopts "f:o:" opt
//   - PowerShell param(...) and $args[0], $args[1]
func ExtractScriptParams(script string, language string) []corelib.NLSkillParam {
	switch strings.ToLower(language) {
	case "python", "python3", "py":
		return extractPythonParams(script)
	case "node", "nodejs", "javascript", "js":
		return extractNodeParams(script)
	case "bash", "sh":
		return extractBashParams(script)
	case "powershell", "pwsh", "ps1":
		return extractPowerShellParams(script)
	default:
		// Try all extractors and return the first non-empty result.
		if params := extractPythonParams(script); len(params) > 0 {
			return params
		}
		if params := extractNodeParams(script); len(params) > 0 {
			return params
		}
		if params := extractBashParams(script); len(params) > 0 {
			return params
		}
		return extractPowerShellParams(script)
	}
}

// ── Python ──────────────────────────────────────────────────────────────

// Matches: parser.add_argument("--name" or parser.add_argument("-n", "--name"
var pyArgparseLongOptionCallRe = regexp.MustCompile(`(?s)add_argument\(([^)]*["']--([a-zA-Z_][a-zA-Z0-9_-]*)["'][^)]*)\)`)

// Matches positional argparse arguments: parser.add_argument("input", ...)
var pyArgparsePositionalCallRe = regexp.MustCompile(`(?s)add_argument\(\s*["']([a-zA-Z_][a-zA-Z0-9_-]*)["']([^)]*)\)`)

var pyArgparseActionRe = regexp.MustCompile(`(?i)action\s*=\s*["']([^"']+)["']`)
var pyArgparseDefaultRe = regexp.MustCompile(`(?i)default\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s,)\]]+))`)
var pyArgparseNargsRe = regexp.MustCompile(`(?i)nargs\s*=\s*["']([^"']+)["']`)

// Matches Click decorators: @click.option("--name", ...), click.option("--name", ...)
var pyClickOptionCallRe = regexp.MustCompile(`(?s)(?:click\.)?option\(([^)]*["']--([a-zA-Z_][a-zA-Z0-9_-]*)["'][^)]*)\)`)

// Matches Click positional arguments: @click.argument("input", ...)
var pyClickArgumentCallRe = regexp.MustCompile(`(?s)(?:click\.)?argument\(\s*["']([a-zA-Z_][a-zA-Z0-9_-]*)["']([^)]*)\)`)

// Matches Typer signature defaults:
// input_file: str = typer.Argument(...)
// format: str = typer.Option("pdf", "--format")
var pyTyperDefaultParamRe = regexp.MustCompile(`(?s)([A-Za-z_][A-Za-z0-9_]*)\s*:\s*([^=,\n]+?)\s*=\s*typer\.(Option|Argument)\(([^)]*)\)`)

// Matches Annotated Typer signatures:
// input_file: Annotated[str, typer.Argument(...)]
var pyTyperAnnotatedParamRe = regexp.MustCompile(`(?s)([A-Za-z_][A-Za-z0-9_]*)\s*:\s*Annotated\s*\[\s*([^,\]]+)\s*,\s*typer\.(Option|Argument)\(([^)]*)\)\s*\]`)

var pyLongOptionInCallRe = regexp.MustCompile(`["']--([a-zA-Z_][a-zA-Z0-9_-]*)["']`)
var pyTyperRunFuncRe = regexp.MustCompile(`typer\.run\(\s*([A-Za-z_][A-Za-z0-9_]*)`)
var pySimpleDefSignatureRe = regexp.MustCompile(`(?s)def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^()]*)\)\s*:`)

// Matches: sys.argv[1], sys.argv[2], etc.
var pySysArgvRe = regexp.MustCompile(`sys\.argv\[(\d+)\]`)

func extractPythonParams(script string) []corelib.NLSkillParam {
	seen := make(map[string]bool)
	var params []corelib.NLSkillParam

	// Priority 1: argparse arguments (most informative).
	for _, m := range pyArgparseLongOptionCallRe.FindAllStringSubmatch(script, -1) {
		call := m[1]
		rawName := m[2]
		if argparseOptionTakesNoValue(call) {
			continue
		}
		name := strings.ReplaceAll(rawName, "-", "_")
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  argparseOptionRequired(call),
			Synthetic: true,
			CLIFlag:   "--" + rawName,
			Default:   argparseOptionDefault(call),
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	for _, m := range pyArgparsePositionalCallRe.FindAllStringSubmatch(script, -1) {
		rawName := m[1]
		call := m[2]
		name := strings.ReplaceAll(rawName, "-", "_")
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  argparsePositionalRequired(call),
			Synthetic: true,
			Default:   argparseOptionDefault(call),
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	for _, m := range pyClickOptionCallRe.FindAllStringSubmatch(script, -1) {
		call := m[1]
		rawName := m[2]
		if clickOptionTakesNoValue(call) {
			continue
		}
		name := strings.ReplaceAll(rawName, "-", "_")
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  clickOptionRequired(call),
			Synthetic: true,
			CLIFlag:   "--" + rawName,
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	for _, m := range pyClickArgumentCallRe.FindAllStringSubmatch(script, -1) {
		rawName := m[1]
		call := m[2]
		name := strings.ReplaceAll(rawName, "-", "_")
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  clickArgumentRequired(call),
			Synthetic: true,
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	appendTyperParams(script, seen, &params)
	if len(params) > 0 {
		return params
	}

	// Priority 2: sys.argv positional arguments.
	positionalNames := []string{"input", "output", "param3", "param4", "param5"}
	for _, m := range pySysArgvRe.FindAllStringSubmatch(script, -1) {
		idx := 0
		for _, c := range m[1] {
			idx = idx*10 + int(c-'0')
		}
		if idx < 1 || idx > 5 {
			continue
		}
		name := positionalNames[idx-1]
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  true,
			Synthetic: true,
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	return params
}

func argparseOptionRequired(call string) bool {
	compact := strings.ToLower(strings.ReplaceAll(call, " ", ""))
	compact = strings.ReplaceAll(compact, "\t", "")
	compact = strings.ReplaceAll(compact, "\n", "")
	return strings.Contains(compact, "required=true")
}

func argparseOptionDefault(call string) string {
	m := pyArgparseDefaultRe.FindStringSubmatch(call)
	if len(m) < 4 {
		return ""
	}
	for _, candidate := range m[1:] {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		switch strings.ToLower(candidate) {
		case "none":
			return ""
		default:
			return candidate
		}
	}
	return ""
}

func argparsePositionalRequired(call string) bool {
	if argparseOptionDefault(call) != "" {
		return false
	}
	m := pyArgparseNargsRe.FindStringSubmatch(call)
	if len(m) < 2 {
		return true
	}
	switch strings.TrimSpace(m[1]) {
	case "?", "*":
		return false
	default:
		return true
	}
}

func argparseOptionTakesNoValue(call string) bool {
	m := pyArgparseActionRe.FindStringSubmatch(call)
	if len(m) < 2 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(m[1])) {
	case "store_true", "store_false", "store_const", "append_const", "count", "help", "version":
		return true
	default:
		return false
	}
}

func clickOptionRequired(call string) bool {
	return compactPythonCallContains(call, "required=true")
}

func clickArgumentRequired(call string) bool {
	return !compactPythonCallContains(call, "required=false")
}

func clickOptionTakesNoValue(call string) bool {
	compact := compactPythonCall(call)
	return strings.Contains(compact, "is_flag=true") ||
		strings.Contains(compact, "flag_value=") ||
		strings.Contains(compact, "count=true")
}

func appendTyperParams(script string, seen map[string]bool, params *[]corelib.NLSkillParam) {
	for _, m := range pyTyperDefaultParamRe.FindAllStringSubmatch(script, -1) {
		rawName, annotation, kind, call := m[1], m[2], m[3], m[4]
		appendTyperParam(rawName, annotation, kind, call, seen, params)
	}
	for _, m := range pyTyperAnnotatedParamRe.FindAllStringSubmatch(script, -1) {
		rawName, annotation, kind, call := m[1], m[2], m[3], m[4]
		appendTyperParam(rawName, annotation, kind, call, seen, params)
	}
	appendTyperRunSignatureParams(script, seen, params)
}

func appendTyperParam(rawName, annotation, kind, call string, seen map[string]bool, params *[]corelib.NLSkillParam) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "option" && typerParamTakesNoValue(annotation, call) {
		return
	}
	required := false
	cliFlag := ""
	switch kind {
	case "argument":
		required = typerArgumentRequired(call)
	case "option":
		required = typerOptionRequired(call)
		cliFlag = typerOptionCLIFlag(rawName, call)
	default:
		return
	}
	appendPythonParam(rawName, required, cliFlag, seen, params)
}

func appendTyperRunSignatureParams(script string, seen map[string]bool, params *[]corelib.NLSkillParam) {
	if !looksLikeTyperCLI(script) {
		return
	}
	runTargets := map[string]bool{}
	for _, m := range pyTyperRunFuncRe.FindAllStringSubmatch(script, -1) {
		runTargets[m[1]] = true
	}
	for _, m := range pySimpleDefSignatureRe.FindAllStringSubmatch(script, -1) {
		funcName, signature := m[1], m[2]
		if len(runTargets) > 0 && !runTargets[funcName] {
			continue
		}
		if len(runTargets) == 0 && funcName != "main" && !signatureBelongsToTyperCommand(script, m[0]) {
			continue
		}
		for _, part := range splitCSV(signature) {
			part = strings.TrimSpace(part)
			if part == "" || strings.HasPrefix(part, "*") {
				continue
			}
			name, annotation, hasDefault, ok := parsePythonSignatureParam(part)
			if !ok || typerParamTakesNoValue(annotation, "") {
				continue
			}
			required := !hasDefault
			cliFlag := ""
			if hasDefault {
				cliFlag = typerOptionCLIFlag(name, "")
			}
			appendPythonParam(name, required, cliFlag, seen, params)
		}
	}
}

func looksLikeTyperCLI(script string) bool {
	return strings.Contains(script, "typer.run(") ||
		strings.Contains(script, "typer.Typer(") ||
		strings.Contains(script, "@app.command") ||
		strings.Contains(script, "@typer.")
}

func signatureBelongsToTyperCommand(script, defMatch string) bool {
	idx := strings.Index(script, defMatch)
	if idx < 0 {
		return false
	}
	start := idx - 256
	if start < 0 {
		start = 0
	}
	prefix := script[start:idx]
	return strings.Contains(prefix, "@app.command") || strings.Contains(prefix, "@typer.")
}

func parsePythonSignatureParam(part string) (name, annotation string, hasDefault, ok bool) {
	left, right, foundDefault := strings.Cut(part, "=")
	hasDefault = foundDefault
	left = strings.TrimSpace(left)
	if left == "" {
		return "", "", false, false
	}
	if rawName, rawAnnotation, foundAnnotation := strings.Cut(left, ":"); foundAnnotation {
		name = strings.TrimSpace(rawName)
		annotation = strings.TrimSpace(rawAnnotation)
	} else {
		name = strings.TrimSpace(left)
	}
	if name == "" || strings.ContainsAny(name, " \t") || right == "lambda" {
		return "", "", false, false
	}
	return name, annotation, hasDefault, true
}

func appendPythonParam(rawName string, required bool, cliFlag string, seen map[string]bool, params *[]corelib.NLSkillParam) {
	name := canonicalRunVarKey(rawName)
	if name == "" || seen[name] {
		return
	}
	seen[name] = true
	p := corelib.NLSkillParam{
		Name:      name,
		Required:  required,
		Synthetic: true,
		CLIFlag:   cliFlag,
	}
	if aliases, ok := commonParamAliases[name]; ok {
		p.Aliases = aliases
	}
	*params = append(*params, p)
}

func typerOptionRequired(call string) bool {
	trimmed := strings.TrimSpace(call)
	return strings.HasPrefix(trimmed, "...") || compactPythonCallContains(call, "default=...")
}

func typerArgumentRequired(call string) bool {
	trimmed := strings.TrimSpace(call)
	compact := compactPythonCall(call)
	if trimmed == "" || strings.HasPrefix(trimmed, "...") || strings.Contains(compact, "default=...") {
		return true
	}
	return !(strings.HasPrefix(strings.ToLower(trimmed), "none") || strings.Contains(compact, "default=none"))
}

func typerParamTakesNoValue(annotation, call string) bool {
	lowerAnnotation := strings.ToLower(strings.TrimSpace(annotation))
	compact := compactPythonCall(call)
	return lowerAnnotation == "bool" ||
		strings.HasSuffix(lowerAnnotation, ".bool") ||
		strings.Contains(compact, "is_flag=true") ||
		strings.Contains(compact, "count=true") ||
		strings.Contains(compact, "/--no-")
}

func typerOptionCLIFlag(rawName, call string) string {
	if m := pyLongOptionInCallRe.FindStringSubmatch(call); len(m) > 1 {
		return "--" + m[1]
	}
	name := canonicalRunVarKey(rawName)
	if name == "" {
		return ""
	}
	return "--" + strings.ReplaceAll(name, "_", "-")
}

func compactPythonCallContains(call, needle string) bool {
	return strings.Contains(compactPythonCall(call), strings.ToLower(strings.TrimSpace(needle)))
}

func compactPythonCall(call string) string {
	compact := strings.ToLower(strings.ReplaceAll(call, " ", ""))
	compact = strings.ReplaceAll(compact, "\t", "")
	compact = strings.ReplaceAll(compact, "\n", "")
	return compact
}

// ── Node.js ─────────────────────────────────────────────────────────────

// Matches: process.argv[2], process.argv[3], etc.
var nodeArgvRe = regexp.MustCompile(`process\.argv\[(\d+)\]`)

// Matches: const [input, output] = process.argv.slice(2)
var nodeArgvSliceDestructureRe = regexp.MustCompile(`(?m)(?:const|let|var)\s*\[\s*([A-Za-z_$][A-Za-z0-9_$]*(?:\s*,\s*[A-Za-z_$][A-Za-z0-9_$]*)*)\s*\]\s*=\s*process\.argv\.slice\(\s*2\s*\)`)

// Matches Commander options:
// program.requiredOption("--input <file>")
// program.option("--format <type>")
var nodeCommanderOptionCallRe = regexp.MustCompile(`(?s)\.(requiredOption|option)\(\s*["']([^"']+)["']([^)]*)\)`)

var nodeCommanderValueOptionRe = regexp.MustCompile(`--([A-Za-z_][A-Za-z0-9_-]*)(?:\s+|=)[<\[]`)

// Matches Yargs options:
// yargs.option("input", { demandOption: true, type: "string" })
var nodeYargsOptionCallRe = regexp.MustCompile(`(?s)\.option\(\s*["']([A-Za-z_][A-Za-z0-9_-]*)["']\s*,\s*\{([^}]*)\}\s*\)`)

func extractNodeParams(script string) []corelib.NLSkillParam {
	seen := make(map[string]bool)
	var params []corelib.NLSkillParam

	for _, m := range nodeCommanderOptionCallRe.FindAllStringSubmatch(script, -1) {
		method, spec := m[1], m[2]
		rawName, cliFlag, takesValue := commanderOptionParam(spec)
		if !takesValue {
			continue
		}
		appendNodeParam(rawName, method == "requiredOption", cliFlag, seen, &params)
	}
	for _, m := range nodeYargsOptionCallRe.FindAllStringSubmatch(script, -1) {
		rawName, body := m[1], m[2]
		if yargsOptionTakesNoValue(body) {
			continue
		}
		appendNodeParam(rawName, yargsOptionRequired(body), "--"+rawName, seen, &params)
	}
	if len(params) > 0 {
		return params
	}

	for _, m := range nodeArgvSliceDestructureRe.FindAllStringSubmatch(script, -1) {
		for _, raw := range strings.Split(m[1], ",") {
			name := canonicalRunVarKey(strings.TrimSpace(raw))
			if name == "" || name == "_" || seen[name] {
				continue
			}
			seen[name] = true
			p := corelib.NLSkillParam{
				Name:      name,
				Required:  true,
				Synthetic: true,
			}
			if aliases, ok := commonParamAliases[name]; ok {
				p.Aliases = aliases
			}
			params = append(params, p)
		}
	}
	if len(params) > 0 {
		return params
	}

	positionalNames := []string{"input", "output", "param3", "param4", "param5"}
	for _, m := range nodeArgvRe.FindAllStringSubmatch(script, -1) {
		idx := 0
		for _, c := range m[1] {
			idx = idx*10 + int(c-'0')
		}
		// Node: process.argv[0]=node, [1]=script, [2]=first arg
		if idx < 2 || idx > 6 {
			continue
		}
		name := positionalNames[idx-2]
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  true,
			Synthetic: true,
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	return params
}

func commanderOptionParam(spec string) (name, cliFlag string, takesValue bool) {
	m := nodeCommanderValueOptionRe.FindStringSubmatch(spec)
	if len(m) < 2 {
		return "", "", false
	}
	rawName := m[1]
	return rawName, "--" + rawName, true
}

func yargsOptionRequired(body string) bool {
	compact := compactJSObject(body)
	return strings.Contains(compact, "demandoption:true") ||
		strings.Contains(compact, "demand:true") ||
		strings.Contains(compact, "required:true")
}

func yargsOptionTakesNoValue(body string) bool {
	compact := compactJSObject(body)
	return strings.Contains(compact, "type:'boolean'") ||
		strings.Contains(compact, `type:"boolean"`) ||
		strings.Contains(compact, "boolean:true")
}

func compactJSObject(s string) string {
	compact := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	compact = strings.ReplaceAll(compact, "\t", "")
	compact = strings.ReplaceAll(compact, "\n", "")
	return compact
}

func appendNodeParam(rawName string, required bool, cliFlag string, seen map[string]bool, params *[]corelib.NLSkillParam) {
	name := canonicalRunVarKey(rawName)
	if name == "" || seen[name] {
		return
	}
	seen[name] = true
	p := corelib.NLSkillParam{
		Name:      name,
		Required:  required,
		Synthetic: true,
		CLIFlag:   cliFlag,
	}
	if aliases, ok := commonParamAliases[name]; ok {
		p.Aliases = aliases
	}
	*params = append(*params, p)
}

// ── Bash ────────────────────────────────────────────────────────────────

// Matches: $1, $2, ${1}, ${2}
var bashPositionalRe = regexp.MustCompile(`\$\{?(\d+)\}?`)

// Matches: getopts "f:o:h" opt
var bashGetoptsRe = regexp.MustCompile(`getopts\s+["']([^"']+)["']`)

func extractBashParams(script string) []corelib.NLSkillParam {
	seen := make(map[string]bool)
	var params []corelib.NLSkillParam

	// Priority 1: getopts flags.
	if m := bashGetoptsRe.FindStringSubmatch(script); len(m) > 1 {
		optStr := m[1]
		for i := 0; i < len(optStr); i++ {
			c := optStr[i]
			if c == ':' || c == 'h' { // skip colon (requires arg) and h (help)
				continue
			}
			requiresValue := i+1 < len(optStr) && optStr[i+1] == ':'
			if !requiresValue {
				continue
			}
			name := string(c)
			if seen[name] {
				continue
			}
			seen[name] = true
			params = append(params, corelib.NLSkillParam{
				Name:      name,
				Required:  true,
				Synthetic: true,
				CLIFlag:   "-" + name,
			})
		}
		if len(params) > 0 {
			return params
		}
	}

	// Priority 2: positional arguments.
	positionalNames := []string{"input", "output", "param3", "param4", "param5"}
	for _, m := range bashPositionalRe.FindAllStringSubmatch(script, -1) {
		idx := 0
		for _, c := range m[1] {
			idx = idx*10 + int(c-'0')
		}
		if idx < 1 || idx > 5 {
			continue
		}
		name := positionalNames[idx-1]
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  true,
			Synthetic: true,
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	return params
}

// --- PowerShell -----------------------------------------------------------

var psParamStartRe = regexp.MustCompile(`(?i)\bparam\s*\(`)
var psParamVarRe = regexp.MustCompile(`(?i)(?:\[[^\]]+\]\s*)?\$([A-Za-z_][A-Za-z0-9_]*)`)
var psArgsRe = regexp.MustCompile(`(?i)\$args\s*\[\s*(\d+)\s*\]`)

func extractPowerShellParams(script string) []corelib.NLSkillParam {
	seen := make(map[string]bool)
	var params []corelib.NLSkillParam
	if block, ok := powerShellParamBlock(script); ok {
		for _, match := range psParamVarRe.FindAllStringSubmatchIndex(block, -1) {
			rawName := block[match[2]:match[3]]
			name := canonicalRunVarKey(rawName)
			if name == "" || seen[name] || isPowerShellLiteralParam(rawName) {
				continue
			}
			prefix := powerShellParamPrefix(block, match[0])
			segment := prefix + block[match[0]:match[1]]
			if powerShellParamTakesNoValue(segment) {
				continue
			}
			seen[name] = true
			p := corelib.NLSkillParam{
				Name:      name,
				Required:  powerShellParamRequired(prefix),
				Synthetic: true,
				CLIFlag:   "-" + rawName,
			}
			if aliases, ok := commonParamAliases[name]; ok {
				p.Aliases = aliases
			}
			params = append(params, p)
		}
		if len(params) > 0 {
			return params
		}
	}

	positionalNames := []string{"input", "output", "param3", "param4", "param5"}
	for _, m := range psArgsRe.FindAllStringSubmatch(script, -1) {
		idx := 0
		for _, c := range m[1] {
			idx = idx*10 + int(c-'0')
		}
		if idx < 0 || idx >= len(positionalNames) {
			continue
		}
		name := positionalNames[idx]
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  true,
			Synthetic: true,
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
	return params
}

func powerShellParamBlock(script string) (string, bool) {
	loc := psParamStartRe.FindStringIndex(script)
	if loc == nil {
		return "", false
	}
	start := loc[1]
	depth := 1
	var quote byte
	for i := start; i < len(script); i++ {
		ch := script[i]
		if quote != 0 {
			if ch == '`' && i+1 < len(script) {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return script[start:i], true
			}
		}
	}
	return script[start:], true
}

func powerShellParamPrefix(block string, varStart int) string {
	if varStart <= 0 || varStart > len(block) {
		return ""
	}
	start := 0
	var quote byte
	bracketDepth := 0
	parenDepth := 0
	for i := 0; i < varStart; i++ {
		ch := block[i]
		if quote != 0 {
			if ch == '`' && i+1 < varStart {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ',':
			if bracketDepth == 0 && parenDepth == 0 {
				start = i + 1
			}
		}
	}
	return block[start:varStart]
}

func powerShellParamRequired(prefix string) bool {
	compact := strings.ToLower(strings.ReplaceAll(prefix, " ", ""))
	compact = strings.ReplaceAll(compact, "\t", "")
	compact = strings.ReplaceAll(compact, "\n", "")
	return strings.Contains(compact, "mandatory=$true") || strings.Contains(compact, "mandatory=true")
}

func powerShellParamTakesNoValue(prefix string) bool {
	return strings.Contains(strings.ToLower(prefix), "[switch]")
}

func isPowerShellLiteralParam(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "true", "false", "null":
		return true
	default:
		return false
	}
}
