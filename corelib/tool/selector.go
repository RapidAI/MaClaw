package tool

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// Profile describes a programming tool's capability profile.
type Profile struct {
	Name       string   `json:"name"`
	Languages  []string `json:"languages"`
	Frameworks []string `json:"frameworks"`
	TaskTypes  []string `json:"task_types"`
	Score      float64  `json:"score"`
}

// Selector recommends the best programming tool for a given task.
type Selector struct {
	profiles     map[string]Profile
	profileOrder []string
	bm25Index    *bm25.Index // cached index over profile capability texts
}

// NewSelector creates a Selector pre-loaded with default capability profiles.
func NewSelector() *Selector {
	profiles := map[string]Profile{
		"claude": {Name: "claude",
			Languages:  []string{"python", "javascript", "typescript", "go", "rust", "java", "c", "cpp", "ruby", "php", "swift", "kotlin"},
			Frameworks: []string{"react", "vue", "django", "flask", "express", "nextjs", "fastapi"},
			TaskTypes:  []string{"refactor", "review", "debug", "explain", "generate", "test", "document", "design", "architecture"},
			Score:      0.9},
		"codex": {Name: "codex",
			Languages:  []string{"python", "javascript", "typescript", "go", "java", "rust", "shell", "bash"},
			Frameworks: []string{"react", "express", "django", "flask", "nextjs"},
			TaskTypes:  []string{"generate", "complete", "edit", "fix", "shell", "command", "automate", "script"},
			Score:      0.85},
		"gemini": {Name: "gemini",
			Languages:  []string{"python", "javascript", "typescript", "go", "java", "kotlin", "dart", "swift"},
			Frameworks: []string{"flutter", "angular", "react", "firebase", "android", "tensorflow"},
			TaskTypes:  []string{"generate", "analyze", "explain", "multimodal", "review", "test", "document"},
			Score:      0.85},
		"cursor": {Name: "cursor",
			Languages:  []string{"python", "javascript", "typescript", "go", "rust", "java", "cpp", "ruby"},
			Frameworks: []string{"react", "vue", "svelte", "nextjs", "tailwind", "express"},
			TaskTypes:  []string{"edit", "generate", "refactor", "fix", "complete", "navigate", "search"},
			Score:      0.8},
		"opencode": {Name: "opencode",
			Languages:  []string{"python", "javascript", "typescript", "go", "java", "rust"},
			Frameworks: []string{"react", "express", "django", "spring"},
			TaskTypes:  []string{"generate", "edit", "fix", "complete", "refactor"},
			Score:      0.75},
		"iflow": {Name: "iflow",
			Languages:  []string{"python", "javascript", "typescript", "java", "go"},
			Frameworks: []string{"react", "vue", "spring", "express", "fastapi"},
			TaskTypes:  []string{"workflow", "automate", "pipeline", "integrate", "deploy", "generate", "edit"},
			Score:      0.75},
		"kilo": {Name: "kilo",
			Languages:  []string{"python", "javascript", "typescript", "go", "rust", "java"},
			Frameworks: []string{"react", "vue", "express", "django", "flask"},
			TaskTypes:  []string{"generate", "edit", "fix", "complete", "refactor", "test"},
			Score:      0.75},
	}
	order := []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"}

	s := &Selector{
		profiles:     profiles,
		profileOrder: order,
		bm25Index:    bm25.New(),
	}
	s.rebuildIndex()
	return s
}

// rebuildIndex builds the BM25 index from profile capability texts.
func (s *Selector) rebuildIndex() {
	docs := make([]bm25.Doc, 0, len(s.profiles))
	for _, name := range s.profileOrder {
		p := s.profiles[name]
		text := strings.Join(p.Languages, " ") + " " +
			strings.Join(p.Frameworks, " ") + " " +
			strings.Join(p.TaskTypes, " ")
		docs = append(docs, bm25.Doc{ID: name, Text: text})
	}
	s.bm25Index.Rebuild(docs)
}

// Recommend returns the best tool name and a reason for the recommendation.
func (s *Selector) Recommend(taskDescription string, installed []string) (string, string) {
	desc := strings.ToLower(taskDescription)
	words := strings.Fields(desc)

	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[strings.ToLower(name)] = true
	}

	// BM25 scoring against profile capability texts.
	bm25Scores := s.bm25Index.Score(taskDescription)

	bestName := ""
	bestScore := -1.0
	var bestMatches []string

	for _, pName := range s.profileOrder {
		profile := s.profiles[pName]
		score := profile.Score + bm25Scores[pName]

		// Collect matches for the reason string.
		var matches []string
		for _, lang := range profile.Languages {
			if selectorContainsWord(words, lang) {
				matches = append(matches, lang)
			}
		}
		for _, fw := range profile.Frameworks {
			if selectorContainsWord(words, fw) {
				matches = append(matches, fw)
			}
		}
		for _, tt := range profile.TaskTypes {
			if selectorContainsWord(words, tt) {
				matches = append(matches, tt)
			}
		}

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

func selectorContainsWord(words []string, target string) bool {
	t := strings.ToLower(target)
	for _, w := range words {
		if w == t {
			return true
		}
	}
	return false
}

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
