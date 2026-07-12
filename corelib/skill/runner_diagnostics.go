package skill

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// BuildArgsExample renders a compact JSON-like args object for missing params.
func BuildArgsExample(keys []string) string {
	keys = canonicalArgKeys(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%q: \"<%s value>\"", key, key))
	}
	return strings.Join(parts, ", ")
}

func FormatMissingRequiredArgsMessage(skillName string, missing []string, description string) string {
	return FormatMissingRequiredArgsMessageWithParams(skillName, missing, description, nil)
}

// FormatMissingRequiredArgsMessageWithParams is the full missing-args report,
// optionally appending the completed parameter schema so the agent can see
// the full contract without a separate inspect call.
func FormatMissingRequiredArgsMessageWithParams(skillName string, missing []string, description string, params []corelib.NLSkillParam) string {
	missing = canonicalArgKeys(missing)
	if len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "skill %q missing required parameter(s): %s. Pass them in args, e.g. args={%s}.", displaySkillName(skillName), strings.Join(missing, ", "), BuildArgsExample(missing))
	appendDescription(&b, description)
	appendParamContract(&b, params)
	b.WriteString("\n[action: provide_args]")
	return b.String()
}

func FormatImplicitRequiredArgsMessage(skillName string, missing []string, description string) string {
	return FormatImplicitRequiredArgsMessageWithParams(skillName, missing, description, nil)
}

func FormatImplicitRequiredArgsMessageWithParams(skillName string, missing []string, description string, params []corelib.NLSkillParam) string {
	missing = canonicalArgKeys(missing)
	if len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "skill %q command template references missing parameter(s): %s. Pass them in args, e.g. args={%s}.", displaySkillName(skillName), strings.Join(missing, ", "), BuildArgsExample(missing))
	appendDescription(&b, description)
	appendParamContract(&b, params)
	b.WriteString("\n[action: provide_args]")
	return b.String()
}

func appendParamContract(b *strings.Builder, params []corelib.NLSkillParam) {
	if b == nil || len(params) == 0 {
		return
	}
	if schema := FormatParamSchema(params); schema != "" {
		b.WriteString("\n\n## Parameter contract\n")
		b.WriteString(schema)
	}
	if js := FormatParamSchemaJSON(params); js != "" {
		b.WriteString("JSON Schema: ")
		b.WriteString(js)
		b.WriteString("\n")
	}
}

func FormatNoExecutableStepsMessage(skillName string, entry *corelib.NLSkillEntry, runner string) string {
	name := displaySkillName(skillName)
	if name == "" && entry != nil {
		name = displaySkillName(entry.Name)
	}
	runner = normalizeRunnerBackend(runner)
	if runner == "" {
		runner = "unknown"
	}

	var b strings.Builder
	if entry != nil && IsKnowledgeSkillType(entry.Type) {
		fmt.Fprintf(&b, "skill %q is a knowledge skill, so it is not directly executable by the %s runner.", name, runner)
		appendDescription(&b, entry.Description)
		if strings.TrimSpace(entry.SkillDir) != "" {
			fmt.Fprintf(&b, "\nSkill directory: %s", strings.TrimSpace(entry.SkillDir))
		}
		fmt.Fprintf(&b, "\nNext action: use it as reference material, or add executable steps/command to the skill definition. [action: inspect_skill]")
		return b.String()
	}
	fmt.Fprintf(&b, "skill %q has no executable steps for the %s runner.", name, runner)
	if entry != nil {
		if len(entry.RequiredArgs) > 0 {
			fmt.Fprintf(&b, " Required parameter(s): %s.", strings.Join(canonicalArgKeys(entry.RequiredArgs), ", "))
		}
		appendDescription(&b, entry.Description)
		if strings.TrimSpace(entry.SkillDir) != "" {
			fmt.Fprintf(&b, "\nSkill directory: %s", strings.TrimSpace(entry.SkillDir))
		}
	}
	if actions := SupportedStepActions(runner); len(actions) > 0 {
		fmt.Fprintf(&b, "\nSupported step actions for %s: %s.", runner, strings.Join(actions, ", "))
	}
	if runner == RunnerBackendTUI {
		b.WriteString("\n[action: open_gui] Run this skill in the GUI runner, or add a bash-compatible step to skill.yaml/SKILL.md.")
	} else {
		b.WriteString("\n[action: inspect_skill] Add an executable step such as action: bash/run, or provide SKILL.md command blocks that the runner can import.")
	}
	return b.String()
}

func FormatRunnerWarnings(requirementWarnings []Violation, fileWarnings []string) []string {
	var warnings []string
	for _, warning := range requirementWarnings {
		message := strings.TrimSpace(FormatViolation(warning))
		if message != "" {
			warnings = append(warnings, message)
		}
	}
	for _, warning := range fileWarnings {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}

func PrefixOutputWithWarnings(output string, warnings []string) string {
	var warningLines []string
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			warningLines = append(warningLines, "[Warning] "+warning)
		}
	}
	if len(warningLines) == 0 {
		return output
	}
	warningOutput := strings.Join(warningLines, "\n")
	if strings.TrimSpace(output) == "" {
		return warningOutput
	}
	return warningOutput + "\n" + output
}

func canonicalArgKeys(keys []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		key = canonicalRunVarKey(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func appendDescription(b *strings.Builder, description string) {
	description = strings.TrimSpace(description)
	if description == "" {
		return
	}
	if len([]rune(description)) > 160 {
		description = string([]rune(description)[:160]) + "..."
	}
	fmt.Fprintf(b, "\nDescription: %s", description)
}

func displaySkillName(name string) string {
	return strings.TrimSpace(name)
}
