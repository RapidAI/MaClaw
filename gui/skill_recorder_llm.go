package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

// skillRecordingMetaSystemPrompt instructs the model to summarize a recorded
// operation sequence into professional skill metadata in a single JSON object.
// Kept inline per project convention (no template files).
const skillRecordingMetaSystemPrompt = `You summarize a recorded sequence of tool operations into metadata for a reusable skill of an AI agent.
Output a single JSON object on one line, and nothing else:
{"name":"...","description":"...","steps":["...", "..."]}

Rules for "name":
- 2 to 6 lowercase English words joined with hyphens, at most 50 characters
- describe what the skill accomplishes (e.g. export-excel-report, batch-convert-images)
- letters, digits and hyphens only; no quotes, no explanation
- do not repeat any of the existing names listed in the input

Rules for "description":
- one concise Chinese sentence: what the skill does and when to use it
- do not mention machine-specific paths; refer to inputs/outputs generically

Rules for "steps":
- one short Chinese title per operation, in the same order as the input list
- at most 12 characters each, e.g. "安装依赖", "生成报表", "运行导出脚本"
- the array length must equal the number of input operations`

// recordingMetadata is the JSON payload expected from the LLM.
type recordingMetadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
}

// SuggestRecordingMetadataWithLLM asks the LLM for a professional skill name,
// description, and per-step titles for a recorded operation sequence.
// Any failure (LLM unconfigured, call error, unusable output) returns ok=false
// so the caller falls back to the heuristic suggestions — recording is never
// blocked by metadata generation.
//
// Operation details are scrubbed of machine-specific absolute paths before
// being sent to the model.
func SuggestRecordingMetadataWithLLM(
	llm skillNamingLLM,
	entries []RecordedOp,
	workDir string,
	existingNames map[string]bool,
) (name, description string, stepTitles []string, ok bool) {
	if llm == nil || !llm.IsConfigured() || len(entries) == 0 {
		return "", "", nil, false
	}

	userMsg := buildRecordingMetaUserMessage(entries, workDir, existingNames)
	resp, err := llm.ChatCall([]map[string]string{
		{"role": "system", "content": skillRecordingMetaSystemPrompt},
		{"role": "user", "content": userMsg},
	})
	if err != nil {
		log.Printf("[skill-recorder] LLM metadata call failed, using heuristics: %v", err)
		return "", "", nil, false
	}

	meta, ok := parseRecordingMetadata(resp)
	if !ok {
		log.Printf("[skill-recorder] unusable LLM metadata %q, using heuristics", truncateNameForLog(resp))
		return "", "", nil, false
	}
	return meta.Name, meta.Description, meta.Steps, true
}

// parseRecordingMetadata extracts and validates the JSON metadata from raw
// LLM output. Tolerates markdown fences and surrounding prose.
func parseRecordingMetadata(raw string) (recordingMetadata, bool) {
	var meta recordingMetadata

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return meta, false
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &meta); err != nil {
		return recordingMetadata{}, false
	}

	// Validate and normalize the name (kebab-case for skill directories).
	name := strings.TrimSpace(meta.Name)
	name = strings.Trim(name, "\"'`")
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	if !hasASCIIAlnum(name) {
		return recordingMetadata{}, false
	}
	name = sanitizeSkillDirName(name)
	if name == "" || name == "skill" {
		return recordingMetadata{}, false
	}
	meta.Name = name

	meta.Description = strings.TrimSpace(meta.Description)
	// Only the name is mandatory; an empty description or missing step titles
	// just means the caller keeps its heuristic values for those fields.

	// Normalize step titles: trim and cap length. Empty entries are kept as
	// placeholders so titles stay index-aligned with the operation list;
	// generateSkillYAML skips empty titles when writing steps.
	titles := make([]string, 0, len(meta.Steps))
	for _, s := range meta.Steps {
		s = strings.TrimSpace(s)
		runes := []rune(s)
		if len(runes) > 30 {
			s = string(runes[:30])
		}
		titles = append(titles, s)
	}
	meta.Steps = titles

	return meta, true
}

// buildRecordingMetaUserMessage assembles the user message for the metadata
// call: the scrubbed operation list plus existing skill names to avoid.
func buildRecordingMetaUserMessage(entries []RecordedOp, workDir string, existingNames map[string]bool) string {
	var b strings.Builder
	b.WriteString("Recorded operations (in order):\n")

	count := 0
	for _, op := range entries {
		if !op.Success {
			continue
		}
		line := scrubbedOpLine(op, workDir)
		if line == "" {
			continue
		}
		count++
		fmt.Fprintf(&b, "%d. %s\n", count, line)
		if count >= 20 {
			break
		}
	}

	if len(existingNames) > 0 {
		const maxNamesInPrompt = 50
		names := make([]string, 0, maxNamesInPrompt)
		for n := range existingNames {
			names = append(names, n)
		}
		sort.Strings(names) // deterministic prompt for reproducible logs/tests
		if len(names) > maxNamesInPrompt {
			names = names[:maxNamesInPrompt]
		}
		b.WriteString("Existing names to avoid: ")
		b.WriteString(strings.Join(names, ", "))
	}
	return b.String()
}

// scrubbedOpLine renders one operation for the LLM prompt with all
// machine-specific absolute paths replaced by placeholders.
func scrubbedOpLine(op RecordedOp, workDir string) string {
	switch op.ToolName {
	case "bash":
		cmd, _ := op.Args["command"].(string)
		cmd = portabilizeCommand(cmd, workDir)
		cmd = strings.Join(strings.Fields(cmd), " ")
		return "bash: " + truncateString(cmd, 200)
	case "write_file":
		path, _ := op.Args["path"].(string)
		return "write_file: " + portabilizePath(path, workDir)
	case "edit_file":
		path, _ := op.Args["path"].(string)
		return "edit_file: " + portabilizePath(path, workDir)
	default:
		return op.ToolName
	}
}
