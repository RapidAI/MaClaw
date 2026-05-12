package memory

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// htmlTagRe matches HTML tags for detection and stripping.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// extractJSONFromLLMResponse extracts and unmarshals JSON from an LLM response
// that may be wrapped in markdown code fences, prose text, or may even be an
// HTML error page from an API gateway.
//
// It handles the following cases:
//  1. Clean JSON (array or object)
//  2. JSON wrapped in ```json ... ``` code fences
//  3. JSON embedded in prose text ("Here are the results: [...]")
//  4. HTML error pages (returns descriptive error)
//  5. Empty responses (returns descriptive error)
//
// The target parameter must be a pointer to the desired type (e.g. *[]knowledgePoint).
func extractJSONFromLLMResponse(resp string, target interface{}) error {
	body := strings.TrimSpace(resp)

	// Case 5: Empty response.
	if body == "" {
		return fmt.Errorf("empty LLM response")
	}

	// Case 4: HTML response (API gateway error returned as 200, or proxy issue).
	// Detect by checking if the response starts with < or contains significant HTML.
	if looksLikeHTML(body) {
		// Strip tags to extract any meaningful error text.
		stripped := htmlTagRe.ReplaceAllString(body, " ")
		stripped = strings.Join(strings.Fields(stripped), " ")
		if len(stripped) > 200 {
			stripped = stripped[:200]
		}
		return fmt.Errorf("received HTML instead of JSON (likely API gateway error): %s", stripped)
	}

	// Case 2: Strip markdown code fences.
	body = stripMarkdownCodeFence(body)

	// Case 1: Try direct unmarshal first (most common path).
	if err := json.Unmarshal([]byte(body), target); err == nil {
		return nil
	}

	// Case 3: JSON embedded in prose text. Try to find the outermost JSON structure.
	if extracted := findEmbeddedJSON(body); extracted != "" {
		if err := json.Unmarshal([]byte(extracted), target); err == nil {
			return nil
		}
	}

	// All attempts failed. Return error with a preview of what we received.
	preview := body
	if len([]rune(preview)) > 100 {
		preview = string([]rune(preview)[:100]) + "..."
	}
	return fmt.Errorf("cannot parse LLM response as JSON: %s", preview)
}

// looksLikeHTML returns true if the text appears to be an HTML document or fragment.
func looksLikeHTML(s string) bool {
	lower := strings.ToLower(s)

	// Exclude reasoning model tags (<think>...</think>) which are not HTML.
	if strings.HasPrefix(lower, "<think>") || strings.HasPrefix(lower, "<thinking>") {
		return false
	}

	// Check common HTML document indicators.
	if strings.HasPrefix(lower, "<!doctype") ||
		strings.HasPrefix(lower, "<html") ||
		strings.HasPrefix(lower, "<head") ||
		strings.HasPrefix(lower, "<body") {
		return true
	}
	// Check if it starts with any HTML tag and contains multiple tags
	// (distinguishes HTML error pages from single XML-like tags).
	if strings.HasPrefix(s, "<") && strings.Count(lower, "<") > 2 {
		return true
	}
	return false
}

// stripMarkdownCodeFence removes ```json ... ``` or ``` ... ``` wrapping.
func stripMarkdownCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}

	// Find the end of the opening fence line.
	firstNewline := strings.Index(s, "\n")
	if firstNewline < 0 {
		// Single line starting with ``` — strip prefix/suffix.
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		return strings.TrimSpace(s)
	}

	// Look for a closing fence that is on its own line (possibly with leading whitespace).
	// Search backwards from the end to find "\n```" or "\n ```" pattern.
	// This avoids matching ``` that appears inside JSON string values.
	closingIdx := -1
	for i := len(s) - 3; i > firstNewline; i-- {
		if s[i] == '`' && i+2 < len(s) && s[i+1] == '`' && s[i+2] == '`' {
			// Check that this ``` is at the start of a line (preceded by \n and optional spaces).
			lineStart := i
			for lineStart > 0 && s[lineStart-1] == ' ' {
				lineStart--
			}
			if lineStart > 0 && s[lineStart-1] == '\n' {
				closingIdx = i
				break
			}
		}
	}

	if closingIdx > firstNewline {
		return strings.TrimSpace(s[firstNewline+1 : closingIdx])
	}

	// No proper closing fence found. Fallback: strip prefix/suffix.
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// findEmbeddedJSON attempts to extract a JSON array or object from text
// that may contain surrounding prose.
func findEmbeddedJSON(s string) string {
	// Try to find a JSON array.
	if idx := strings.Index(s, "["); idx >= 0 {
		if end := findMatchingBracket(s, idx, '[', ']'); end > idx {
			return s[idx : end+1]
		}
	}
	// Try to find a JSON object.
	if idx := strings.Index(s, "{"); idx >= 0 {
		if end := findMatchingBracket(s, idx, '{', '}'); end > idx {
			return s[idx : end+1]
		}
	}
	return ""
}

// findMatchingBracket finds the position of the matching closing bracket,
// respecting nesting and string literals.
func findMatchingBracket(s string, start int, open, close byte) int {
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		ch := s[i]

		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' && inString {
			escaped = true
			continue
		}

		if ch == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
