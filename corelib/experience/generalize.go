package experience

import (
	"path/filepath"
	"strings"
)

const projectPathArg = "project_path"
const projectPathTemplate = "{{project_path}}"

// GeneralizePattern replaces source-session-specific values with template
// variables before quality/evidence checks and persistence. Today this handles
// the most important case: the current project path. Other absolute paths are
// still treated as one-off evidence and rejected by the quality gate.
func GeneralizePattern(p Pattern, projectPath string) Pattern {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return p
	}
	repls := projectPathReplacements(projectPath)
	if len(repls) == 0 {
		return p
	}

	p.Name = replaceKnownValues(p.Name, repls)
	p.Description = replaceKnownValues(p.Description, repls)
	for i := range p.Triggers {
		p.Triggers[i] = replaceKnownValues(p.Triggers[i], repls)
	}
	for i := range p.Steps {
		p.Steps[i].Action = replaceKnownValues(p.Steps[i].Action, repls)
		p.Steps[i].OnError = replaceKnownValues(p.Steps[i].OnError, repls)
		p.Steps[i].Params = generalizeParamMap(p.Steps[i].Params, repls)
	}
	return p
}

func projectPathReplacements(projectPath string) []string {
	candidates := []string{projectPath, filepath.Clean(projectPath), filepath.ToSlash(projectPath), filepath.ToSlash(filepath.Clean(projectPath))}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "." || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func generalizeParamMap(params map[string]interface{}, replacements []string) map[string]interface{} {
	if params == nil {
		return nil
	}
	out := make(map[string]interface{}, len(params))
	for key, value := range params {
		out[replaceKnownValues(key, replacements)] = generalizeValue(value, replacements)
	}
	return out
}

func generalizeValue(value interface{}, replacements []string) interface{} {
	switch v := value.(type) {
	case string:
		return replaceKnownValues(v, replacements)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = generalizeValue(item, replacements)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[replaceKnownValues(key, replacements)] = generalizeValue(item, replacements)
		}
		return out
	default:
		return value
	}
}

func replaceKnownValues(text string, replacements []string) string {
	for _, replacement := range replacements {
		text = strings.ReplaceAll(text, replacement, projectPathTemplate)
	}
	return text
}
