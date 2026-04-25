package skill

import (
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

	consumed := make(map[string]bool)

	// Phase 1: Alias resolution + default application.
	for _, p := range params {
		allNames := append([]string{p.Name}, p.Aliases...)

		var matched string
		for _, name := range allNames {
			if v, ok := vars[name]; ok && v != "" {
				matched = v
				break
			}
		}

		if matched != "" {
			result.ResolvedVars[p.Name] = matched
			for _, n := range allNames {
				consumed[n] = true
			}
		} else if p.Default != "" {
			result.ResolvedVars[p.Name] = p.Default
		}
	}

	// Phase 2: CLI args construction (explicit schema only).
	// Values are stored unquoted — shell quoting is the responsibility of
	// the execution layer (resolveSkillStep), not the binding layer.
	for _, p := range params {
		v := result.ResolvedVars[p.Name]
		if v == "" || p.CLIFlag == "" || p.Synthetic {
			continue
		}
		result.CLIArgs = append(result.CLIArgs, p.CLIFlag, v)
	}

	// Phase 3: Undeclared parameter detection.
	var declaredNames string // lazy-computed, shared across warnings
	for key, value := range vars {
		if consumed[key] || value == "" {
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
		if p.Required && result.ResolvedVars[p.Name] == "" {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"必需参数 %q 未提供", p.Name))
		}
	}

	return result
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
		name := p.Name
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
