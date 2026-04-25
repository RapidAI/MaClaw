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
func ExtractScriptParams(script string, language string) []corelib.NLSkillParam {
	switch strings.ToLower(language) {
	case "python", "python3":
		return extractPythonParams(script)
	case "node", "nodejs", "javascript":
		return extractNodeParams(script)
	case "bash", "sh":
		return extractBashParams(script)
	default:
		// Try all extractors and return the first non-empty result.
		if params := extractPythonParams(script); len(params) > 0 {
			return params
		}
		if params := extractNodeParams(script); len(params) > 0 {
			return params
		}
		return extractBashParams(script)
	}
}

// ── Python ──────────────────────────────────────────────────────────────

// Matches: parser.add_argument("--name" or parser.add_argument("-n", "--name"
var pyArgparseRe = regexp.MustCompile(`add_argument\([^)]*["']--([a-zA-Z_][a-zA-Z0-9_-]*)["']`)

// Matches: sys.argv[1], sys.argv[2], etc.
var pySysArgvRe = regexp.MustCompile(`sys\.argv\[(\d+)\]`)

func extractPythonParams(script string) []corelib.NLSkillParam {
	seen := make(map[string]bool)
	var params []corelib.NLSkillParam

	// Priority 1: argparse arguments (most informative).
	for _, m := range pyArgparseRe.FindAllStringSubmatch(script, -1) {
		name := strings.ReplaceAll(m[1], "-", "_")
		if seen[name] {
			continue
		}
		seen[name] = true
		p := corelib.NLSkillParam{
			Name:      name,
			Required:  true,
			Synthetic: true,
			CLIFlag:   "--" + m[1],
		}
		if aliases, ok := commonParamAliases[name]; ok {
			p.Aliases = aliases
		}
		params = append(params, p)
	}
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

// ── Node.js ─────────────────────────────────────────────────────────────

// Matches: process.argv[2], process.argv[3], etc.
var nodeArgvRe = regexp.MustCompile(`process\.argv\[(\d+)\]`)

func extractNodeParams(script string) []corelib.NLSkillParam {
	seen := make(map[string]bool)
	var params []corelib.NLSkillParam

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
			name := string(c)
			if seen[name] {
				continue
			}
			seen[name] = true
			params = append(params, corelib.NLSkillParam{
				Name:      name,
				Required:  i+1 < len(optStr) && optStr[i+1] == ':', // has colon = requires value
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
