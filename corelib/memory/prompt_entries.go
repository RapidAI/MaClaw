package memory

import (
	"context"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// RecallEntriesPromptOptions controls shared prompt rendering for automatic
// memory recall sections.
type RecallEntriesPromptOptions struct {
	Header   string
	Intro    string
	Footer   string
	MaxRunes int
}

// FormatRecallEntriesForPrompt renders recalled entries for prompt injection in
// a stable format shared by GUI, TUI, and server agents.
func FormatRecallEntriesForPrompt(entries []Entry, opts RecallEntriesPromptOptions) string {
	if len(entries) == 0 {
		return ""
	}
	maxRunes := opts.MaxRunes
	if maxRunes <= 0 {
		maxRunes = 200
	}
	var b strings.Builder
	writePromptLine := func(value string) {
		if value == "" {
			return
		}
		b.WriteString(value)
		if !strings.HasSuffix(value, "\n") {
			b.WriteByte('\n')
		}
	}
	writePromptLine(opts.Header)
	writePromptLine(opts.Intro)
	for _, entry := range entries {
		b.WriteString(FormatRecallEntryForPrompt(entry, maxRunes))
		b.WriteByte('\n')
	}
	writePromptLine(opts.Footer)
	return b.String()
}

func FormatExperienceCandidatesForPrompt(candidates []lifecycle.Candidate, opts RecallEntriesPromptOptions) string {
	if len(candidates) == 0 {
		return ""
	}
	maxRunes := opts.MaxRunes
	if maxRunes <= 0 {
		maxRunes = 200
	}
	var b strings.Builder
	writePromptLine := func(value string) {
		if value == "" {
			return
		}
		b.WriteString(value)
		if !strings.HasSuffix(value, "\n") {
			b.WriteByte('\n')
		}
	}
	writePromptLine(opts.Header)
	writePromptLine(opts.Intro)
	for _, candidate := range candidates {
		b.WriteString(FormatExperienceCandidateForPrompt(candidate, maxRunes))
		b.WriteByte('\n')
	}
	writePromptLine(opts.Footer)
	return b.String()
}

func FormatExperienceCandidateForPrompt(candidate lifecycle.Candidate, maxRunes int) string {
	entry := candidate.Entry
	text := firstNonEmptyString(entry.Content, entry.WhenToUse)
	if maxRunes > 0 {
		runes := []rune(text)
		if len(runes) > maxRunes {
			text = string(runes[:maxRunes]) + "..."
		}
	}
	entryType := string(entry.EntryType)
	if entryType == "" {
		entryType = "experience"
	}
	line := "- [" + entryType + "] " + text
	if entry.WhenToUse != "" && entry.WhenToUse != text {
		line += " (use: " + entry.WhenToUse + ")"
	}
	if entry.SourceURL == "" {
		return line
	}
	line += " (source: " + entry.SourceURL
	if LooksLikeFilePath(entry.SourceURL) {
		line += "; full: read_file"
	}
	line += ")"
	return line
}

// ProactivePromptOptions controls the complete dynamic memory context section
// injected into prompts by GUI, TUI, VE, and server agents.
type ProactivePromptOptions struct {
	Recall       ProactiveRecallOptions
	EventContext lifecycle.EventContext
	Policy       lifecycle.RetrievalPolicy
	TokenBudget  int

	IncludeMemoryIndex bool
	MemoryIndexLabel   string
	MemoryIndexUnit    string

	IncludeSceneIndex bool
	SceneIndexLabel   string
	SceneLimit        int
	MaxScenes         int
	MaxArtifacts      int

	RecallEntries RecallEntriesPromptOptions

	IncludeDerivedFacts bool
	DerivedFactLimit    int
}

// ProactiveContextForPrompt builds the shared dynamic memory prompt context and
// returns both the rendered section and the recalled entries for host logging.
func (s *Store) ProactiveContextForPrompt(query string, opts ProactivePromptOptions) (string, []Entry) {
	if s == nil {
		return "", nil
	}
	var b strings.Builder
	if opts.IncludeMemoryIndex {
		if index := s.MemoryIndexForPrompt(opts.Recall.StrictProject, opts.Recall.ProjectPath, opts.MemoryIndexUnit); index != "" {
			label := opts.MemoryIndexLabel
			if label == "" {
				label = "[Memory Index] "
			}
			b.WriteByte('\n')
			b.WriteString(label)
			b.WriteString(index)
			b.WriteByte('\n')
		}
	}

	if opts.IncludeSceneIndex {
		sceneLimit := opts.SceneLimit
		if sceneLimit <= 0 {
			sceneLimit = 5
		}
		maxScenes := opts.MaxScenes
		if maxScenes <= 0 {
			maxScenes = 3
		}
		maxArtifacts := opts.MaxArtifacts
		if maxArtifacts <= 0 {
			maxArtifacts = 2
		}
		if sceneNav := s.SceneIndexForPrompt(opts.Recall.StrictProject, opts.Recall.ProjectPath, sceneLimit, maxScenes, maxArtifacts); sceneNav != "" {
			label := opts.SceneIndexLabel
			if label == "" {
				label = "[Scene Index]"
			}
			b.WriteByte('\n')
			b.WriteString(label)
			if !strings.HasSuffix(label, "\n") {
				b.WriteByte('\n')
			}
			b.WriteString(sceneNav)
			b.WriteByte('\n')
		}
	}

	var recalled []Entry
	if strings.TrimSpace(query) != "" {
		opts.Recall.EventContext = opts.EventContext
		decision := decideProactivePromptRetrieval(query, opts)
		s.recordRetrievalDecisionEvent(decision, opts.EventContext)
		candidates := s.RecallProactiveCandidatesWithDecision(decision, opts.Recall)
		recalled = s.entriesForExperienceCandidates(candidates)
		recallSection := FormatExperienceCandidatesForPrompt(candidates, opts.RecallEntries)
		b.WriteString(recallSection)
		if recallSection != "" {
			s.recordCandidateExperienceEvent(lifecycle.EventExperienceInjected, "proactive_prompt:"+string(decision.Mode), decision.Query, candidates, EstimateTextTokens(recallSection), opts.EventContext)
		}
	}

	if opts.IncludeDerivedFacts {
		limit := opts.DerivedFactLimit
		if limit <= 0 {
			limit = 5
		}
		b.WriteString(FormatDerivedFactsForPrompt(s.LastDerivedFacts(), limit))
	}
	return b.String(), recalled
}

func decideProactivePromptRetrieval(query string, opts ProactivePromptOptions) lifecycle.RetrievalDecision {
	policy := opts.Policy
	if policy == nil {
		policy = lifecycle.DefaultRetrievalPolicy{}
	}
	return policy.Decide(context.Background(), lifecycle.RetrievalPolicyInput{
		TraceID:        opts.EventContext.TraceID,
		TaskID:         opts.EventContext.TaskID,
		CurrentGoal:    query,
		TokenBudget:    opts.TokenBudget,
		Boundary:       proactiveRecallBoundary(opts.Recall),
		MissingSignals: []string{"max_entries:" + strconv.Itoa(defaultPositive(opts.Recall.MaxEntries, 12))},
	})
}
