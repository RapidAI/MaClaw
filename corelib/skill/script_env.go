package skill

import (
	"regexp"
	"sort"
	"strings"
)

var (
	pythonOSEnvironIndexRe        = regexp.MustCompile(`\bos\.environ\s*\[\s*["']([A-Z][A-Z0-9_]{2,})["']\s*\]`)
	pythonOSEnvironGetNoDefaultRe = regexp.MustCompile(`\bos\.environ\.get\(\s*["']([A-Z][A-Z0-9_]{2,})["']\s*\)`)
	pythonOSGetenvNoDefaultRe     = regexp.MustCompile(`\bos\.getenv\(\s*["']([A-Z][A-Z0-9_]{2,})["']\s*\)`)
	nodeProcessEnvRe              = regexp.MustCompile(`\bprocess\.env(?:\.([A-Z][A-Z0-9_]{2,})|\[\s*["']([A-Z][A-Z0-9_]{2,})["']\s*\])`)
	powerShellEnvRe               = regexp.MustCompile(`(?i)\$env:([A-Z_][A-Z0-9_]{2,})`)
	bashRequiredEnvRe             = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]{2,})\s*:\?|\[\s+-z\s+["']?\$([A-Z_][A-Z0-9_]{2,})["']?\s+\]`)
)

// ExtractScriptRequiredEnv infers environment variables referenced directly by
// crafted script source so persisted skills can fail early with the shared
// runner requirement diagnostics instead of crashing inside the script.
func ExtractScriptRequiredEnv(script, language string) []string {
	language = strings.ToLower(strings.TrimSpace(language))
	seen := map[string]bool{}
	addMatches := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(script, -1) {
			for i := 1; i < len(m); i++ {
				addRequiredEnvName(seen, m[i])
			}
		}
	}
	switch language {
	case "python", "python3", "py":
		addMatches(pythonOSEnvironIndexRe)
		addMatches(pythonOSEnvironGetNoDefaultRe)
		addMatches(pythonOSGetenvNoDefaultRe)
	case "node", "nodejs", "javascript", "js":
		addMatches(nodeProcessEnvRe)
	case "powershell", "pwsh", "ps1":
		addMatches(powerShellEnvRe)
	case "bash", "sh":
		addMatches(bashRequiredEnvRe)
	default:
		addMatches(pythonOSEnvironIndexRe)
		addMatches(pythonOSEnvironGetNoDefaultRe)
		addMatches(pythonOSGetenvNoDefaultRe)
		addMatches(nodeProcessEnvRe)
		addMatches(powerShellEnvRe)
		addMatches(bashRequiredEnvRe)
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func addRequiredEnvName(seen map[string]bool, name string) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" || scriptEnvAllowlist[name] {
		return
	}
	seen[name] = true
}

var scriptEnvAllowlist = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "USERNAME": true, "TEMP": true,
	"TMP": true, "PWD": true, "SHELL": true, "COMSPEC": true, "OS": true,
	"LANG": true, "LC_ALL": true, "PYTHONPATH": true, "NODE_PATH": true,
	"USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	"PROGRAMDATA": true, "SYSTEMROOT": true, "WINDIR": true,
	"NODE_ENV": true, "CI": true, "TERM": true,
}
