package goal

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ApplyMissingAction fills action when the model sent a goal body and left
// the discriminator out. qfmodel does this on large plan calls: the schema
// lists action as a plain string beside the long content fields, and the
// arguments arrive with understanding, steps, and acceptance_criteria only.
// An explicit action is left unchanged. An empty call stays empty so get is
// not invented.
func ApplyMissingAction(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	if action, _ := args["action"].(string); strings.TrimSpace(action) != "" {
		return strings.TrimSpace(strings.ToLower(action))
	}
	inferred := inferGoalAction(args)
	if inferred != "" {
		args["action"] = inferred
	}
	return inferred
}

func inferGoalAction(args map[string]interface{}) string {
	if goalArgPresent(args, "steps") || goalArgPresent(args, "understanding") {
		return "plan"
	}
	if _, ok := args["index"]; ok {
		return "step"
	}
	if goalArgPresent(args, "objective") {
		return "create"
	}
	if goalArgPresent(args, "summary") {
		return "complete"
	}
	if goalArgPresent(args, "reason") {
		return "fail"
	}
	return ""
}

// IndexArg reads a step index. Models send the host step number as a JSON
// number or as a string. A missing or unparsable value is not an index, so
// the caller can bind the report to the current step instead of skipping it.
func IndexArg(args map[string]interface{}, key string) (int, bool) {
	if args == nil {
		return 0, false
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		n := int(v)
		if v != float64(n) {
			return 0, false
		}
		return n, true
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func goalArgPresent(args map[string]interface{}, key string) bool {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []interface{}:
		return len(v) > 0
	case []string:
		return len(v) > 0
	default:
		return true
	}
}
