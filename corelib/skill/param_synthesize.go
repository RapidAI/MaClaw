package skill

import (
	"github.com/RapidAI/CodeClaw/corelib"
)

// commonParamAliases maps canonical parameter names (extracted from command
// templates) to common alternative names the LLM might use. This closes the
// LLM-to-Skill parameter name gap: when a skill uses {{input}} but the LLM
// passes "file", BindParams resolves the alias automatically.
//
// Only add aliases for genuinely interchangeable names. Do NOT add aliases
// that change semantics (e.g., "url" is not an alias for "input").
var commonParamAliases = map[string][]string{
	"input":       {"file", "path", "source", "src", "input_file", "in"},
	"output":      {"dest", "destination", "target", "output_file", "out"},
	"text":        {"content", "message", "msg", "prompt", "input"},
	"content":     {"text", "message", "msg", "prompt", "input"},
	"message":     {"text", "content", "prompt", "msg", "input"},
	"prompt":      {"text", "content", "message", "input"},
	"description": {"content", "text", "message", "prompt", "input", "task"},
	"task":        {"content", "text", "message", "prompt", "input", "description"},
	"query":       {"q", "question", "prompt", "text"},
	"format":      {"type", "fmt", "output_format"},
}

// SynthesizeParams extracts placeholder keys from all step command templates
// and builds an implicit parameter schema. This ensures every skill has a
// params schema, either explicitly declared in YAML or auto-synthesized
// from command templates, so all skills flow through the same BindParams path.
//
// Parameters marked Synthetic=true indicate they were inferred from templates
// rather than explicitly declared. The context injection marks these
// as inferred from a template so callers know the name may be approximate.
//
// When a synthesized parameter name matches a key in commonParamAliases,
// the aliases are automatically attached so BindParams can resolve them.
//
// requiredArgs is the skill's RequiredArgs field; keys listed there are
// marked Required=true in the synthesized schema.
func SynthesizeParams(steps []corelib.NLSkillStep, requiredArgs []string) []corelib.NLSkillParam {
	requiredSet := make(map[string]bool, len(requiredArgs))
	for _, a := range requiredArgs {
		requiredSet[canonicalRunVarKey(a)] = true
	}

	seen := make(map[string]bool)
	var params []corelib.NLSkillParam

	for _, step := range steps {
		// Scan all string values in step.Params for placeholders.
		extractPlaceholdersFromParams(step.Params, func(key string) {
			key = canonicalRunVarKey(key)
			if key == "" || seen[key] {
				return
			}
			seen[key] = true
			p := corelib.NLSkillParam{
				Name:      key,
				Required:  requiredSet[key],
				Synthetic: true,
			}
			// Attach common aliases so BindParams can resolve alternative
			// names the LLM might use (e.g., "file" -> "input").
			if aliases, ok := commonParamAliases[key]; ok {
				p.Aliases = aliases
			}
			params = append(params, p)
		})
	}

	return params
}

// CompleteParamsForRunner merges author-declared params with params inferred
// from command templates. Many legacy skills declare only part of the schema in
// YAML while the command still contains additional placeholders; runner
// pre-checks, natural input inference, and ResolveStep all need the complete
// contract instead of switching synthesis off as soon as one explicit param is
// present.
func CompleteParamsForRunner(explicit []corelib.NLSkillParam, steps []corelib.NLSkillStep, requiredArgs []string) []corelib.NLSkillParam {
	synthesized := SynthesizeParams(steps, requiredArgs)
	if len(explicit) == 0 {
		return synthesized
	}

	requiredSet := make(map[string]bool, len(requiredArgs))
	for _, raw := range requiredArgs {
		if key := canonicalRunVarKey(raw); key != "" {
			requiredSet[key] = true
		}
	}

	result := make([]corelib.NLSkillParam, 0, len(explicit)+len(synthesized))
	covered := make(map[string]bool)
	for _, param := range explicit {
		copied := copySkillParam(param)
		if paramMatchesRequiredArg(copied, requiredSet) {
			copied.Required = true
		}
		result = append(result, copied)
		for _, key := range paramCoverageKeys(copied) {
			covered[key] = true
		}
	}

	for _, param := range synthesized {
		if covered[canonicalRunVarKey(param.Name)] {
			continue
		}
		result = append(result, copySkillParam(param))
		for _, key := range paramCoverageKeys(param) {
			covered[key] = true
		}
	}

	return result
}

func copySkillParam(param corelib.NLSkillParam) corelib.NLSkillParam {
	copied := param
	if param.Aliases != nil {
		copied.Aliases = append([]string(nil), param.Aliases...)
	}
	return copied
}

func paramMatchesRequiredArg(param corelib.NLSkillParam, requiredSet map[string]bool) bool {
	for _, key := range paramCoverageKeys(param) {
		if requiredSet[key] {
			return true
		}
	}
	return false
}

func paramCoverageKeys(param corelib.NLSkillParam) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(raw string) {
		key := canonicalRunVarKey(raw)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		keys = append(keys, key)
	}
	add(param.Name)
	for _, alias := range param.Aliases {
		add(alias)
	}
	for _, alias := range commonParamAliases[canonicalRunVarKey(param.Name)] {
		add(alias)
	}
	return keys
}

// extractPlaceholdersFromParams recursively scans a params map for placeholder
// keys in all string values. This mirrors the recursive structure of
// resolveSkillValue; any string that resolveSkillValue would process,
// this function also scans.
func extractPlaceholdersFromParams(value interface{}, callback func(key string)) {
	switch typed := value.(type) {
	case string:
		for _, key := range ExtractPlaceholderKeys(typed) {
			callback(key)
		}
	case map[string]interface{}:
		for _, item := range typed {
			extractPlaceholdersFromParams(item, callback)
		}
	case []interface{}:
		for _, item := range typed {
			extractPlaceholdersFromParams(item, callback)
		}
	}
}
