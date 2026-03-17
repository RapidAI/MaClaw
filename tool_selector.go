package main

import (
	"strings"
)

// ToolProfile describes a programming tool's capability profile used for
// intelligent tool selection. Each profile captures the languages, frameworks,
// and task types the tool excels at, along with a base quality score.
type ToolProfile struct {
	Name       string   `json:"name"`
	Languages  []string `json:"languages"`
	Frameworks []string `json:"frameworks"`
	TaskTypes  []string `json:"task_types"`
	Score      float64  `json:"score"`
}

// ToolSelector recommends the best programming tool for a given task based on
// keyword matching between the task description and each tool's capability
// profile. Installed (healthy) tools receive a scoring bonus.
type ToolSelector struct {
	profiles map[string]ToolProfile
}

// NewToolSelector creates a ToolSelector pre-loaded with default capability
// profiles for the known tools.
func NewToolSelector() *ToolSelector {
	profiles := map[string]ToolProfile{
		"claude": {
			Name:       "claude",
			Languages:  []string{"python", "javascript", "typescript", "go", "rust", "java", "c", "cpp", "ruby", "php", "swift", "kotlin"},
			Frameworks: []string{"react", "vue", "django", "flask", "express", "nextjs", "fastapi"},
			TaskTypes:  []string{"refactor", "review", "debug", "explain", "generate", "test", "document", "design", "architecture"},
			Score:      0.9,
		},
		"codex": {
			Name:       "codex",
			Languages:  []string{"python", "javascript", "typescript", "go", "java", "rust", "shell", "bash"},
			Frameworks: []string{"react", "express", "django", "flask", "nextjs"},
			TaskTypes:  []string{"generate", "complete", "edit", "fix", "shell", "command", "automate", "script"},
			Score:      0.85,
		},
		"gemini": {
			Name:       "gemini",
			Languages:  []string{"python", "javascript", "typescript", "go", "java", "kotlin", "dart", "swift"},
			Frameworks: []string{"flutter", "angular", "react", "firebase", "android", "tensorflow"},
			TaskTypes:  []string{"generate", "analyze", "explain", "multimodal", "review", "test", "document"},
			Score:      0.85,
		},
		"cursor": {
			Name:       "cursor",
			Languages:  []string{"python", "javascript", "typescript", "go", "rust", "java", "cpp", "ruby"},
			Frameworks: []string{"react", "vue", "svelte", "nextjs", "tailwind", "express"},
			TaskTypes:  []string{"edit", "generate", "refactor", "fix", "complete", "navigate", "search"},
			Score:      0.8,
		},
		"opencode": {
			Name:       "opencode",
			Languages:  []string{"python", "javascript", "typescript", "go", "java", "rust"},
			Frameworks: []string{"react", "express", "django", "spring"},
			TaskTypes:  []string{"generate", "edit", "fix", "complete", "refactor"},
			Score:      0.75,
		},
		"iflow": {
			Name:       "iflow",
			Languages:  []string{"python", "javascript", "typescript", "java", "go"},
			Frameworks: []string{"react", "vue", "spring", "express", "fastapi"},
			TaskTypes:  []string{"workflow", "automate", "pipeline", "integrate", "deploy", "generate", "edit"},
			Score:      0.75,
		},
		"kilo": {
			Name:       "kilo",
			Languages:  []string{"python", "javascript", "typescript", "go", "rust", "java"},
			Frameworks: []string{"react", "vue", "express", "django", "flask"},
			TaskTypes:  []string{"generate", "edit", "fix", "complete", "refactor", "test"},
			Score:      0.75,
		},
	}
	return &ToolSelector{profiles: profiles}
}

// Recommend returns the name of the best tool for the given task description
// and a human-readable reason for the recommendation. Tools present in the
// installed slice receive a bonus so that available tools are preferred.
//
// If no tool scores above zero the function falls back to "claude" as the
// most general-purpose default.
func (s *ToolSelector) Recommend(taskDescription string, installed []string) (string, string) {
	desc := strings.ToLower(taskDescription)
	words := strings.Fields(desc)

	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[strings.ToLower(name)] = true
	}

	bestName := ""
	bestScore := -1.0
	bestMatches := []string{}

	// Iterate in deterministic order so ties are resolved consistently.
	profileOrder := []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"}
	for _, pName := range profileOrder {
		profile, ok := s.profiles[pName]
		if !ok {
			continue
		}
		score := profile.Score
		var matches []string

		for _, lang := range profile.Languages {
			if containsWord(words, lang) || strings.Contains(desc, lang) {
				score += 1.0
				matches = append(matches, lang)
			}
		}

		for _, fw := range profile.Frameworks {
			if containsWord(words, fw) || strings.Contains(desc, fw) {
				score += 1.5
				matches = append(matches, fw)
			}
		}

		for _, tt := range profile.TaskTypes {
			if containsWord(words, tt) || strings.Contains(desc, tt) {
				score += 1.0
				matches = append(matches, tt)
			}
		}

		// Bonus for installed & healthy tools.
		if installedSet[profile.Name] {
			score += 2.0
		}

		if score > bestScore {
			bestScore = score
			bestName = profile.Name
			bestMatches = matches
		}
	}

	if bestName == "" {
		bestName = "claude"
	}

	reason := buildRecommendReason(bestName, bestMatches, installedSet[bestName])
	return bestName, reason
}

// containsWord checks whether any element in words equals target (case-insensitive).
func containsWord(words []string, target string) bool {
	t := strings.ToLower(target)
	for _, w := range words {
		if w == t {
			return true
		}
	}
	return false
}

// buildRecommendReason produces a short human-readable explanation for the recommendation.
func buildRecommendReason(name string, matches []string, isInstalled bool) string {
	var parts []string

	if len(matches) > 0 {
		unique := uniqueStrings(matches)
		parts = append(parts, "matches capabilities: "+strings.Join(unique, ", "))
	}

	if isInstalled {
		parts = append(parts, "tool is installed and available")
	}

	if len(parts) == 0 {
		return name + " is recommended as a general-purpose default"
	}

	return name + " is recommended because " + strings.Join(parts, "; ")
}

// uniqueStrings removes duplicates while preserving order.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
