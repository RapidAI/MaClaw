package tool

import "strings"

// CleanToolArguments sanitizes LLM-returned tool argument JSON strings.
// Many smaller models (DeepSeek, Qwen, etc.) return malformed JSON with
// code fences, over-escaped quotes, or single-quote wrappers. This function
// normalizes these common issues before json.Unmarshal.
//
// Inspired by goskills/runner.go cleanToolArguments().
func CleanToolArguments(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return args
	}

	// 1. Remove code fence wrappers: ```json ... ``` or ``` ... ```
	// Only strip when both opening and closing fences are present to avoid
	// mangling valid JSON that coincidentally contains backticks.
	for _, fence := range []string{"```json", "```JSON", "```"} {
		if strings.HasPrefix(args, fence) && strings.HasSuffix(strings.TrimRight(args, "\n\r\t "), "```") {
			args = strings.TrimPrefix(args, fence)
			args = strings.TrimLeft(args, "\n\r\t ")
			// Now strip the closing ```
			if idx := strings.LastIndex(args, "```"); idx >= 0 {
				args = args[:idx]
			}
			args = strings.TrimRight(args, "\n\r\t ")
			break // only strip once
		}
	}

	// 2. Remove single-quote wrapper: '{"key": "value"}' → {"key": "value"}
	if len(args) >= 2 && args[0] == '\'' && args[len(args)-1] == '\'' {
		inner := args[1 : len(args)-1]
		// Only unwrap if the inner content looks like JSON.
		trimmed := strings.TrimSpace(inner)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			args = inner
		}
	}

	// 3. Fix over-escaped quotes: {\"key\": \"value\"} → {"key": "value"}
	// Detect by checking if the JSON structure quotes are escaped.
	if strings.HasPrefix(args, `{\"`) || strings.HasPrefix(args, `[\"`) {
		args = strings.ReplaceAll(args, `\"`, `"`)
	}

	// 4. Fix unnecessary single-quote escapes: \' → '
	args = strings.ReplaceAll(args, `\'`, `'`)

	return strings.TrimSpace(args)
}
