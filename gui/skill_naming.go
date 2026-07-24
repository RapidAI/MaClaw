package main

import (
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// skillNamingLLM is the minimal LLM surface needed for semantic skill naming.
// SkillEvolutionLLMAdapter already implements it.
type skillNamingLLM interface {
	ChatCall(messages []map[string]string) (string, error)
	IsConfigured() bool
}

// maxLLMSkillNameLen caps the LLM-generated base name (without craft_ prefix).
const maxLLMSkillNameLen = 40

// skillNamingSystemPrompt instructs the model to produce a short, meaningful
// snake_case skill name. Kept inline per project convention (no template files).
const skillNamingSystemPrompt = `You name reusable skills for an AI agent.
Given a task description and the tool sequence that accomplished it, output a short snake_case English skill name that describes what the skill does (e.g. fetch_weather_data, export_excel_report).
Rules:
- 2 to 6 lowercase words joined by underscores, at most 40 characters total
- letters, digits and underscores only; no craft_ prefix, no quotes, no explanation
- output the name on a single line and nothing else
- if existing names are listed, do not repeat any of them`

// GenerateSkillNameWithLLM asks the LLM for a semantic skill name based on the
// task description and drafted steps. Any failure (LLM unconfigured, call
// error, unusable output) falls back to the heuristic tool.GenerateSkillName,
// so the pipeline is never blocked by naming. The returned name always carries
// the craft_ prefix so scanner learned-skill classification keeps working.
//
// existingNames, when non-nil, is used to avoid collisions by appending a
// short numeric suffix (_2, _3, ...). For full protection the set should
// contain each existing skill's raw name and its toKebabCase directory form;
// matching is done against both.
//
// The second return value reports whether the LLM-produced name was used
// (false means heuristic fallback).
func GenerateSkillNameWithLLM(
	llm skillNamingLLM,
	description string,
	steps []skill.SkillYAMLStep,
	existingNames map[string]bool,
) (string, bool) {
	if llm == nil || !llm.IsConfigured() {
		return tool.GenerateSkillName(description), false
	}

	userMsg := buildSkillNamingUserMessage(description, steps, existingNames)
	resp, err := llm.ChatCall([]map[string]string{
		{"role": "system", "content": skillNamingSystemPrompt},
		{"role": "user", "content": userMsg},
	})
	if err != nil {
		log.Printf("[skill-naming] LLM call failed, falling back to heuristic name: %v", err)
		return tool.GenerateSkillName(description), false
	}

	base, ok := sanitizeLLMSkillName(resp)
	if !ok {
		log.Printf("[skill-naming] unusable LLM name %q, falling back to heuristic", truncateNameForLog(resp))
		return tool.GenerateSkillName(description), false
	}

	name := "craft_" + base
	if existingNames != nil {
		// Match on both the raw name and its toKebabCase form: the on-disk
		// directory is derived via toKebabCase, so a name that only differs
		// by _ vs - would still collide with an existing skill directory.
		for i := 2; existingNames[name] || existingNames[toKebabCase(name)]; i++ {
			name = "craft_" + base + "_" + strconv.Itoa(i)
		}
	}
	return name, true
}

// buildSkillNamingUserMessage assembles the user message for the naming call:
// the (truncated) task description, the deduplicated tool actions, and the
// existing skill names to avoid.
func buildSkillNamingUserMessage(description string, steps []skill.SkillYAMLStep, existingNames map[string]bool) string {
	var b strings.Builder
	b.WriteString("Task description: ")
	b.WriteString(truncateSkillDescription(strings.TrimSpace(description), 300))

	if actions := dedupeStepActions(steps); len(actions) > 0 {
		b.WriteString("\nTool sequence: ")
		b.WriteString(strings.Join(actions, " -> "))
	}

	if len(existingNames) > 0 {
		// Only learned (craft_*) names matter for collision-avoidance, and a
		// cap keeps the prompt small when many skills are registered.
		const maxNamesInPrompt = 50
		names := make([]string, 0, maxNamesInPrompt)
		for n := range existingNames {
			if !strings.HasPrefix(n, "craft_") {
				continue
			}
			names = append(names, n)
		}
		sort.Strings(names) // deterministic prompt for reproducible logs/tests
		if len(names) > maxNamesInPrompt {
			names = names[:maxNamesInPrompt]
		}
		if len(names) > 0 {
			b.WriteString("\nExisting names to avoid: ")
			b.WriteString(strings.Join(names, ", "))
		}
	}
	return b.String()
}

// dedupeStepActions returns step action names in order, duplicates removed,
// capped at 10 entries to keep the prompt small.
func dedupeStepActions(steps []skill.SkillYAMLStep) []string {
	seen := make(map[string]bool)
	var actions []string
	for _, s := range steps {
		a := strings.TrimSpace(s.Action)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		actions = append(actions, a)
		if len(actions) >= 10 {
			break
		}
	}
	return actions
}

// sanitizeLLMSkillName normalizes raw LLM output into a valid skill base name
// (no craft_ prefix). Returns ok=false when nothing usable remains.
func sanitizeLLMSkillName(raw string) (string, bool) {
	// First line only; models sometimes add explanations below.
	line := raw
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "\"'`")
	line = strings.TrimSpace(line)
	line = strings.ToLower(line)
	// Normalize to snake_case: the skill directory name is derived via
	// toKebabCase (underscores become hyphens), so accepting dashes here
	// would let craft_foo-bar and craft_foo_bar collide on disk.
	line = strings.ReplaceAll(line, "-", "_")
	// Strip a craft_ prefix the model may have added despite instructions
	// (dash normalization above also turns a craft- prefix into craft_).
	line = strings.TrimPrefix(line, "craft_")

	// Reject input with no ASCII letters/digits at all (e.g. Chinese output,
	// pure punctuation): SanitizeFilename would degrade it to a task_<hash>
	// fallback, which is no better than the heuristic name.
	if !hasASCIIAlnum(line) {
		return "", false
	}
	name := tool.SanitizeFilename(line)
	if len(name) > maxLLMSkillNameLen {
		name = strings.TrimRight(name[:maxLLMSkillNameLen], "_-")
	}
	name = strings.Trim(name, "_-")
	if name == "" {
		return "", false
	}
	return name, true
}

// hasASCIIAlnum reports whether s contains at least one ASCII letter or digit.
func hasASCIIAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return true
		}
	}
	return false
}

// truncateNameForLog bounds LLM output written to logs. Truncates on rune
// boundaries so multi-byte characters are not split.
func truncateNameForLog(s string) string {
	const maxRunes = 80
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return s
}
