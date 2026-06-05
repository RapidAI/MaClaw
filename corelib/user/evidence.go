package user

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// pendingObservation represents an ambiguous signal queued for batch LLM analysis.
type pendingObservation struct {
	Message   string
	Timestamp time.Time
}

// Collector analyzes user messages for profile signals.
type Collector struct {
	model        *Model
	batchQueue   []pendingObservation
	updateCounts map[string]int // dimension → updates this session
	mu           sync.Mutex
}

// NewCollector creates an evidence collector bound to a user model.
func NewCollector(model *Model) *Collector {
	return &Collector{
		model:        model,
		batchQueue:   nil,
		updateCounts: make(map[string]int),
	}
}

// Pattern definitions for common signals.
var (
	// Programming language mentions
	languagePatterns = regexp.MustCompile(`(?i)\b(golang|go\s+(?:code|program|project|module)|python|javascript|typescript|rust|java|c\+\+|cpp|ruby|swift|kotlin|scala|haskell|elixir|clojure|php|perl|lua|dart|zig|ocaml|erlang|r\b|c#|csharp|objective-c|shell|bash)\b`)

	// Tool preferences
	toolPatterns = regexp.MustCompile(`(?i)\b(vim|neovim|nvim|emacs|vscode|vs\s*code|visual\s*studio\s*code|intellij|goland|pycharm|webstorm|sublime|atom|docker|kubernetes|k8s|terraform|ansible|git|github|gitlab|jenkins|circleci|webpack|vite|tmux|zsh|fish|iterm|warp|copilot)\b`)

	// Expertise indicators
	expertisePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(i'?m\s+a\s+)?(senior|staff|principal|lead|junior|beginner|intermediate|expert|experienced|novice)\s*(developer|engineer|programmer|dev)?\b`),
		regexp.MustCompile(`(?i)\b(years?\s+of\s+experience|been\s+(coding|programming|developing)\s+for)\b`),
		regexp.MustCompile(`(?i)\b(new\s+to\s+(programming|coding|development))\b`),
	}
)

// Analyze processes a user message for profile signals.
// For clear signals, it updates the model directly (respecting rate limits).
// For ambiguous signals, it queues them for batch LLM analysis.
func (c *Collector) Analyze(userMessage string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return
	}

	// Check for programming language mentions
	if matches := languagePatterns.FindAllString(msg, -1); len(matches) > 0 {
		lang := normalizeLangName(matches[0])
		c.tryUpdate("preferred_languages", lang, Evidence{
			Observation: fmt.Sprintf("Mentioned programming language: %s", lang),
			Timestamp:   time.Now(),
			Source:      "pattern",
		})
	}

	// Check for tool preferences
	if matches := toolPatterns.FindAllString(msg, -1); len(matches) > 0 {
		tool := strings.TrimSpace(matches[0])
		c.tryUpdate("tool_preferences", tool, Evidence{
			Observation: fmt.Sprintf("Mentioned tool preference: %s", tool),
			Timestamp:   time.Now(),
			Source:      "pattern",
		})
	}

	// Check for expertise indicators
	for _, pat := range expertisePatterns {
		if match := pat.FindString(msg); match != "" {
			level := classifyExpertiseLevel(match)
			if level != "" {
				c.tryUpdate("technical_level", level, Evidence{
					Observation: fmt.Sprintf("Expertise indicator: %s", match),
					Timestamp:   time.Now(),
					Source:      "pattern",
				})
			}
			break
		}
	}

	// Queue ambiguous observations for batch LLM analysis.
	// Messages that contain potential signals but don't match clear patterns
	// are queued for later batch processing.
	if hasAmbiguousSignals(msg) {
		c.batchQueue = append(c.batchQueue, pendingObservation{
			Message:   msg,
			Timestamp: time.Now(),
		})
	}
}

// FlushBatch processes queued observations via LLM (called every 10 turns or session end).
func (c *Collector) FlushBatch(summarize func(string) (string, error)) error {
	c.mu.Lock()
	if len(c.batchQueue) == 0 {
		c.mu.Unlock()
		return nil
	}

	// Collect queued observations
	var sb strings.Builder
	for i, obs := range c.batchQueue {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(obs.Message)
	}
	queued := sb.String()

	// Clear the queue before releasing the lock
	c.batchQueue = nil
	c.mu.Unlock()

	// Call the summarize callback outside the lock
	result, err := summarize(queued)
	if err != nil {
		return fmt.Errorf("batch LLM analysis failed: %w", err)
	}

	// Parse and apply results
	c.applyBatchResults(result)
	return nil
}

// ResetSession resets per-session rate limits and clears the batch queue.
func (c *Collector) ResetSession() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.updateCounts = make(map[string]int)
	c.batchQueue = nil
}

// tryUpdate attempts to update a dimension, respecting the rate limit
// of at most one update per dimension per session.
func (c *Collector) tryUpdate(dimension, value string, evidence Evidence) {
	if c.updateCounts[dimension] > 0 {
		// Rate limited: already updated this dimension this session
		return
	}

	err := c.model.UpdateDimension(dimension, value, evidence)
	if err == nil {
		c.updateCounts[dimension]++
	}
}

// applyBatchResults parses the LLM summarization result and applies updates.
// Expected format: one update per line as "dimension:value"
func (c *Collector) applyBatchResults(result string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	lines := strings.Split(result, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		dimension := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if dimension == "" || value == "" {
			continue
		}

		c.tryUpdate(dimension, value, Evidence{
			Observation: fmt.Sprintf("Batch LLM analysis: %s", value),
			Timestamp:   time.Now(),
			Source:      "llm",
		})
	}
}

// normalizeLangName normalizes a language name match to a canonical form.
func normalizeLangName(match string) string {
	lower := strings.ToLower(strings.TrimSpace(match))
	switch {
	case lower == "golang" || strings.HasPrefix(lower, "go "):
		return "Go"
	case lower == "javascript":
		return "JavaScript"
	case lower == "typescript":
		return "TypeScript"
	case lower == "python":
		return "Python"
	case lower == "rust":
		return "Rust"
	case lower == "java":
		return "Java"
	case lower == "c++" || lower == "cpp":
		return "C++"
	case lower == "ruby":
		return "Ruby"
	case lower == "swift":
		return "Swift"
	case lower == "kotlin":
		return "Kotlin"
	case lower == "scala":
		return "Scala"
	case lower == "haskell":
		return "Haskell"
	case lower == "elixir":
		return "Elixir"
	case lower == "clojure":
		return "Clojure"
	case lower == "php":
		return "PHP"
	case lower == "perl":
		return "Perl"
	case lower == "lua":
		return "Lua"
	case lower == "dart":
		return "Dart"
	case lower == "zig":
		return "Zig"
	case lower == "ocaml":
		return "OCaml"
	case lower == "erlang":
		return "Erlang"
	case lower == "c#" || lower == "csharp":
		return "C#"
	case lower == "objective-c":
		return "Objective-C"
	case lower == "shell" || lower == "bash":
		return "Shell"
	case lower == "r":
		return "R"
	default:
		return strings.TrimSpace(match)
	}
}

// classifyExpertiseLevel extracts an expertise level from a matched string.
func classifyExpertiseLevel(match string) string {
	lower := strings.ToLower(match)
	switch {
	case strings.Contains(lower, "senior") || strings.Contains(lower, "staff") || strings.Contains(lower, "principal") || strings.Contains(lower, "lead"):
		return "senior"
	case strings.Contains(lower, "expert") || strings.Contains(lower, "experienced"):
		return "expert"
	case strings.Contains(lower, "intermediate"):
		return "intermediate"
	case strings.Contains(lower, "junior") || strings.Contains(lower, "beginner") || strings.Contains(lower, "novice") || strings.Contains(lower, "new to"):
		return "beginner"
	default:
		return ""
	}
}

// hasAmbiguousSignals checks if a message contains potential profile signals
// that don't match the clear patterns but might be useful for batch LLM analysis.
func hasAmbiguousSignals(msg string) bool {
	lower := strings.ToLower(msg)

	// Signals that suggest communication style preferences
	ambiguousIndicators := []string{
		"i prefer",
		"i like to",
		"i usually",
		"i always",
		"i tend to",
		"my workflow",
		"my setup",
		"my stack",
		"i work on",
		"i work with",
		"my team",
		"our codebase",
		"in my experience",
		"i've been working",
	}

	for _, indicator := range ambiguousIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}
