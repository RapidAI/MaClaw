package skill

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// BindResult holds the outcome of parameter binding.
type BindResult struct {
	// ResolvedVars contains the final variable map after alias resolution
	// and default application. Keys are canonical param names.
	ResolvedVars map[string]string

	// CLIArgs contains CLI flag+value pairs to append to the command.
	// Only populated for params with explicit CLIFlag declarations.
	// Format: [flag, value, flag, value, ...]
	CLIArgs []string

	// Errors are hard failures that should prevent execution (e.g.,
	// required parameter missing).
	Errors []string

	// Warnings are soft issues that are logged but don't prevent execution
	// (e.g., caller provided an undeclared parameter).
	Warnings []string
}

// BindParams resolves LLM-provided vars against the skill's parameter schema.
// This is the single binding path for all skills — both explicitly-declared
// and auto-synthesized schemas flow through here.
//
// The binding process:
//  1. Alias resolution: for each declared param, check vars for the canonical
//     name and all aliases. First match wins.
//  2. Default application: if no match found and param has a default, use it.
//  3. CLI args construction: for params with CLIFlag (explicit schema only),
//     build flag+value pairs for command-line appending.
//  4. Undeclared parameter detection: vars keys not consumed by any param
//     generate warnings.
//  5. Required parameter validation: params marked Required that have no
//     value after steps 1-2 generate errors.
func BindParams(params []corelib.NLSkillParam, vars map[string]string) BindResult {
	result := BindResult{
		ResolvedVars: make(map[string]string),
	}

	if len(params) == 0 {
		// No schema — pass through all vars unchanged.
		for k, v := range vars {
			result.ResolvedVars[k] = v
		}
		return result
	}

	primaryNames := schemaPrimaryParamNames(params)
	consumed := make(map[string]bool)

	// Phase 1: Alias resolution + default application.
	for _, p := range params {
		p.Name = canonicalRunVarKey(p.Name)
		allNames := paramBindingNamesForSchema(p, primaryNames)

		var matched string
		for _, name := range allNames {
			if v, ok := lookupCanonicalVar(vars, name); ok && v != "" {
				matched = v
				break
			}
		}

		if matched != "" {
			for _, name := range allNames {
				result.ResolvedVars[name] = matched
			}
			for _, n := range allNames {
				consumed[n] = true
			}
		} else if p.Default != "" {
			for _, name := range allNames {
				result.ResolvedVars[name] = p.Default
			}
		}
	}

	// Phase 2: CLI args construction (explicit schema only).
	// Values are stored unquoted — shell quoting is the responsibility of
	// the execution layer (resolveSkillStep), not the binding layer.
	for _, p := range params {
		p.Name = canonicalRunVarKey(p.Name)
		v := result.ResolvedVars[p.Name]
		if v == "" || p.CLIFlag == "" || p.Synthetic {
			continue
		}
		result.CLIArgs = append(result.CLIArgs, p.CLIFlag, v)
	}

	// Phase 3: Undeclared parameter detection.
	var declaredNames string // lazy-computed, shared across warnings
	for key, value := range vars {
		if consumed[canonicalRunVarKey(key)] || value == "" || isUndeclaredRunCarrierKey(key) {
			continue
		}
		if declaredNames == "" {
			declaredNames = formatParamNames(params)
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"参数 %q 未被 Skill 声明（已声明: %s）",
			key, declaredNames))
	}

	// Phase 4: Required parameter validation.
	for _, p := range params {
		p.Name = canonicalRunVarKey(p.Name)
		if p.Required && result.ResolvedVars[p.Name] == "" {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"必需参数 %q 未提供", p.Name))
		}
	}

	return result
}

func isUndeclaredRunCarrierKey(key string) bool {
	switch canonicalRunVarKey(key) {
	case "input", "user_prompt",
		"app_id", "app_kind", "app_name", "maclaw_app",
		"fields", "file", "files", "file_name", "file_path", "file_paths", "file_text",
		"input_file_path", "local_file_path", "uploaded_file_path",
		"input_mode", "output", "output_mode", "output_format",
		"params", "prompt":
		return true
	default:
		return false
	}
}

// IsUndeclaredRunCarrierKey reports generic runner input carriers that may be
// used for inference even when not declared as concrete skill parameters.
func IsUndeclaredRunCarrierKey(key string) bool {
	return isUndeclaredRunCarrierKey(key)
}

// ParameterBindingKeySet returns every canonical input key BindParams may
// consume for the given schema, including common aliases.
func ParameterBindingKeySet(params []corelib.NLSkillParam) map[string]bool {
	primaryNames := schemaPrimaryParamNames(params)
	result := make(map[string]bool)
	for _, p := range params {
		for _, name := range paramBindingNamesForSchema(p, primaryNames) {
			if name != "" {
				result[name] = true
			}
		}
	}
	return result
}

func canonicalParamNames(names []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, name := range names {
		name = canonicalRunVarKey(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func paramBindingNames(param corelib.NLSkillParam) []string {
	return paramBindingNamesForSchema(param, nil)
}

func paramBindingNamesForSchema(param corelib.NLSkillParam, primaryNames map[string]bool) []string {
	names := append([]string{param.Name}, param.Aliases...)
	paramName := canonicalRunVarKey(param.Name)
	for _, alias := range commonParamAliases[paramName] {
		aliasKey := canonicalRunVarKey(alias)
		if primaryNames != nil && primaryNames[aliasKey] && aliasKey != paramName {
			continue
		}
		names = append(names, alias)
	}
	return canonicalParamNames(names)
}

func schemaPrimaryParamNames(params []corelib.NLSkillParam) map[string]bool {
	primaryNames := make(map[string]bool, len(params))
	for _, param := range params {
		if key := canonicalRunVarKey(param.Name); key != "" {
			primaryNames[key] = true
		}
	}
	return primaryNames
}

// HasErrors returns true if the bind result contains hard errors.
func (r BindResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// ErrorString returns all errors joined as a single string.
func (r BindResult) ErrorString() string {
	return strings.Join(r.Errors, "; ")
}

// formatParamNames builds a human-readable list of declared parameter names.
func formatParamNames(params []corelib.NLSkillParam) string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		name := canonicalRunVarKey(p.Name)
		if len(p.Aliases) > 0 {
			name += " (别名: " + strings.Join(p.Aliases, ", ") + ")"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// FormatParamSchema builds a human-readable parameter schema summary for
// injection into the LLM's system prompt. Distinguishes between explicitly
// declared params and auto-synthesized ones.
func FormatParamSchema(params []corelib.NLSkillParam) string {
	if len(params) == 0 {
		return ""
	}

	hasSynthetic := false
	for _, p := range params {
		if p.Synthetic {
			hasSynthetic = true
			break
		}
	}

	var b strings.Builder
	if hasSynthetic {
		b.WriteString("参数 (从命令模板推断):\n")
	} else {
		b.WriteString("参数:\n")
	}

	for _, p := range params {
		b.WriteString("  - ")
		b.WriteString(p.Name)
		if typ := strings.TrimSpace(p.Type); typ != "" {
			b.WriteString(" (")
			b.WriteString(typ)
			b.WriteString(")")
		}
		if len(p.Aliases) > 0 {
			b.WriteString(" (别名: ")
			b.WriteString(strings.Join(p.Aliases, ", "))
			b.WriteString(")")
		}
		if p.Description != "" {
			b.WriteString(": ")
			b.WriteString(p.Description)
		}
		if p.CLIFlag != "" {
			b.WriteString(" [")
			b.WriteString(p.CLIFlag)
			b.WriteString("]")
		}
		if p.Default != "" {
			b.WriteString(" (默认: ")
			b.WriteString(p.Default)
			b.WriteString(")")
		}
		if p.Required {
			b.WriteString(" *必需*")
		}
		if p.Synthetic {
			b.WriteString(" (从模板推断)")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// FormatCompactParamTags returns a single-line param summary for skill list
// lines, e.g. "params: input*, format". Required params are marked with '*'.
func FormatCompactParamTags(params []corelib.NLSkillParam) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	seen := map[string]bool{}
	for _, p := range params {
		name := canonicalRunVarKey(p.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if p.Required {
			parts = append(parts, name+"*")
		} else {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "params: " + strings.Join(parts, ", ")
}

// ParamSchemaJSONObject builds a JSON Schema (draft-style) object for skill
// args so agent UIs / MCP-style consumers can treat skill params as a real
// declarative schema rather than free-form prose.
func ParamSchemaJSONObject(params []corelib.NLSkillParam) map[string]interface{} {
	if len(params) == 0 {
		return nil
	}
	properties := make(map[string]interface{}, len(params))
	var required []string
	for _, p := range params {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		prop := map[string]interface{}{
			"type": paramJSONSchemaType(p.Type),
		}
		if desc := strings.TrimSpace(p.Description); desc != "" {
			prop["description"] = desc
		}
		if def := strings.TrimSpace(p.Default); def != "" {
			prop["default"] = def
		}
		if flag := strings.TrimSpace(p.CLIFlag); flag != "" {
			prop["x-cli-flag"] = flag
		}
		if len(p.Aliases) > 0 {
			aliases := make([]string, 0, len(p.Aliases))
			for _, a := range p.Aliases {
				a = strings.TrimSpace(a)
				if a != "" {
					aliases = append(aliases, a)
				}
			}
			if len(aliases) > 0 {
				prop["x-aliases"] = aliases
			}
		}
		if p.Synthetic {
			prop["x-synthetic"] = true
		}
		properties[name] = prop
		if p.Required {
			required = append(required, name)
		}
	}
	if len(properties) == 0 {
		return nil
	}
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// FormatParamSchemaJSON returns a compact JSON Schema string for params, or "".
func FormatParamSchemaJSON(params []corelib.NLSkillParam) string {
	schema := ParamSchemaJSONObject(params)
	if schema == nil {
		return ""
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return string(raw)
}

func paramJSONSchemaType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "number", "integer", "boolean", "array", "object", "string":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "string"
	}
}

// FormatSkillInspectReport builds a full param-contract report for
// manage_skill(action="info"). Completes declared + synthesized params and
// enriches missing descriptions from SKILL.md when available.
func FormatSkillInspectReport(entry *corelib.NLSkillEntry) string {
	if entry == nil {
		return "skill not found"
	}
	params := CompleteParamsForSkill(entry)
	var b strings.Builder
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(&b, "=== Skill: %s ===\n", name)
	if id := strings.TrimSpace(entry.SkillID); id != "" {
		fmt.Fprintf(&b, "skill_id: %s\n", id)
	}
	if desc := strings.TrimSpace(entry.Description); desc != "" {
		fmt.Fprintf(&b, "description: %s\n", desc)
	}
	if status := strings.TrimSpace(entry.Status); status != "" {
		fmt.Fprintf(&b, "status: %s\n", status)
	}
	if typ := strings.TrimSpace(entry.Type); typ != "" {
		fmt.Fprintf(&b, "type: %s\n", typ)
	}
	if mode := strings.TrimSpace(entry.Mode); mode != "" {
		fmt.Fprintf(&b, "mode: %s\n", mode)
	}
	if len(entry.RequiredArgs) > 0 {
		fmt.Fprintf(&b, "required_args: %s\n", strings.Join(entry.RequiredArgs, ", "))
	}
	if len(entry.RequiredEnv) > 0 {
		fmt.Fprintf(&b, "required_env: %s\n", strings.Join(entry.RequiredEnv, ", "))
	}
	b.WriteString("\n")
	if schema := FormatParamSchema(params); schema != "" {
		b.WriteString(schema)
	} else {
		b.WriteString("参数: (无声明 / 无模板占位符)\n")
	}
	if js := FormatParamSchemaJSON(params); js != "" {
		b.WriteString("\nJSON Schema (args):\n")
		b.WriteString(js)
		b.WriteString("\n")
	}
	b.WriteString("\n提示: manage_skill(action=\"run\", name=\"")
	b.WriteString(name)
	b.WriteString("\", args={...})\n")
	return b.String()
}
