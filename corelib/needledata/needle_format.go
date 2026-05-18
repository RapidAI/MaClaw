package needledata

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// NeedleExample is the JSONL shape consumed by cactus-compute/needle:
// query plus JSON-encoded tool and answer arrays.
type NeedleExample struct {
	Query   string `json:"query"`
	Tools   string `json:"tools"`
	Answers string `json:"answers"`
}

func TrainingRecordToNeedleExample(rec TrainingRecord) (NeedleExample, error) {
	tools := needleToolsForRecord(rec)
	if len(tools) == 0 {
		return NeedleExample{}, fmt.Errorf("record %s has no needle tools", rec.ID)
	}
	answers := []map[string]any{{
		"name":      rec.Expected.Name,
		"arguments": rec.Expected.Arguments,
	}}
	if answers[0]["arguments"] == nil {
		answers[0]["arguments"] = map[string]any{}
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return NeedleExample{}, err
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return NeedleExample{}, err
	}
	return NeedleExample{Query: needleQueryForRecord(rec), Tools: string(toolsJSON), Answers: string(answersJSON)}, nil
}

func TrainingRecordsToNeedleExamples(records []TrainingRecord) ([]NeedleExample, error) {
	out := make([]NeedleExample, 0, len(records))
	for _, rec := range records {
		ex, err := TrainingRecordToNeedleExample(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, nil
}

func needleQueryForRecord(rec TrainingRecord) string {
	var system, user string
	for _, msg := range rec.Messages {
		switch msg.Role {
		case "system":
			if system == "" {
				system = strings.TrimSpace(msg.Content)
			}
		case "user":
			if user == "" {
				user = strings.TrimSpace(msg.Content)
			}
		}
	}
	if system == "" {
		return user
	}
	return strings.TrimSpace("Task: " + rec.Task + "\nInstruction: " + system + "\nUser: " + user)
}

func needleToolsForRecord(rec TrainingRecord) []map[string]any {
	switch rec.Task {
	case EventIntentGate:
		return namedNeedleTools(map[string]string{
			"route_ssh":        "Route to remote shell or server operation.",
			"route_browser":    "Route to browser automation.",
			"route_web_search": "Route to web search or online research.",
			"route_office":     "Route to document, spreadsheet, or presentation work.",
			"route_coding":     "Route to local coding, debugging, or maintenance work.",
			"route_workflow":   "Route to a structured MacLaw workflow.",
			"no_call":          "Do not route to a tool or workflow.",
		})
	case EventWorkflowReview:
		return namedNeedleTools(map[string]string{
			"confirm":     "Approve the current workflow phase and continue.",
			"supplement":  "Add corrections, requirements, or extra context for the current phase.",
			"skip":        "Skip the current phase when allowed.",
			"cancel":      "Cancel the current workflow.",
			"switch_task": "Cancel this workflow and start a different task.",
			"other":       "Keep waiting because the reply is unrelated or ambiguous.",
		})
	case EventMemoryExtractGate:
		return namedNeedleTools(map[string]string{
			"extract_memory": "Extract durable user or project knowledge into memory.",
			"no_extract":     "Do not extract a memory from this message.",
		})
	case EventSmartApproval:
		return namedNeedleTools(map[string]string{
			"safe":    "The operation is safe enough to proceed under current policy.",
			"unsafe":  "The operation should be blocked or require explicit confirmation.",
			"unknown": "There is not enough confidence to decide locally.",
		})
	default:
		out := make([]map[string]any, 0, len(rec.Tools))
		for _, tool := range rec.Tools {
			out = append(out, needleToolSpec(tool.Name, tool.Description, tool.Parameters))
		}
		return out
	}
}

func namedNeedleTools(descriptions map[string]string) []map[string]any {
	order := []string{"route_ssh", "route_browser", "route_web_search", "route_office", "route_coding", "route_workflow", "no_call", "confirm", "supplement", "skip", "cancel", "switch_task", "other", "extract_memory", "no_extract", "safe", "unsafe", "unknown"}
	out := make([]map[string]any, 0, len(descriptions))
	seen := map[string]bool{}
	for _, name := range order {
		if desc, ok := descriptions[name]; ok {
			out = append(out, needleToolSpec(name, desc, nil))
			seen[name] = true
		}
	}
	var rest []string
	for name := range descriptions {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		out = append(out, needleToolSpec(name, descriptions[name], nil))
	}
	return out
}

func needleToolSpec(name, description string, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	return map[string]any{"name": name, "description": description, "parameters": params}
}

func ValidateNeedleExample(ex NeedleExample) error {
	if strings.TrimSpace(ex.Query) == "" {
		return fmt.Errorf("empty query")
	}
	if !utf8.ValidString(ex.Tools) || !utf8.ValidString(ex.Answers) {
		return fmt.Errorf("tools or answers are not valid utf-8")
	}
	var tools []map[string]any
	if err := json.Unmarshal([]byte(ex.Tools), &tools); err != nil {
		return fmt.Errorf("invalid tools JSON: %w", err)
	}
	var answers []map[string]any
	if err := json.Unmarshal([]byte(ex.Answers), &answers); err != nil {
		return fmt.Errorf("invalid answers JSON: %w", err)
	}
	if len(tools) == 0 || len(answers) == 0 {
		return fmt.Errorf("tools and answers must be non-empty")
	}
	return nil
}
