package memory

import (
	"fmt"
	"strings"
)

// FormatMemoryIndexForPrompt renders a compact store table-of-contents for
// prompt context. It intentionally summarizes the whole eligible store instead
// of only the entries recalled for the current query.
func FormatMemoryIndexForPrompt(stats []CategoryStat, countUnit string) string {
	if len(stats) == 0 {
		return ""
	}
	var parts []string
	for _, stat := range stats {
		part := fmt.Sprintf("%s: %s", stat.Category.DisplayName(), formatMemoryIndexCount(stat.Count, countUnit))
		if len(stat.Tags) > 0 {
			part += "(" + strings.Join(stat.Tags, ", ") + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " | ")
}

// MemoryIndexForPrompt builds the shared memory index layer used by GUI, TUI,
// and server prompts.
func (s *Store) MemoryIndexForPrompt(strictProject bool, projectPath string, countUnit string) string {
	if s == nil {
		return ""
	}
	if strictProject && strings.TrimSpace(projectPath) != "" {
		return FormatMemoryIndexForPrompt(s.CategoryStatsForProject(projectPath), countUnit)
	}
	return FormatMemoryIndexForPrompt(s.CategoryStats(), countUnit)
}

// SceneIndexForPrompt builds and formats the shared scene navigation layer.
func (s *Store) SceneIndexForPrompt(strictProject bool, projectPath string, sceneLimit int, maxScenes int, maxArtifacts int) string {
	if s == nil {
		return ""
	}
	scenes := s.SceneIndex(sceneLimit)
	if strictProject && strings.TrimSpace(projectPath) != "" {
		projectLower := semanticNormalizeProjectPath(projectPath)
		filtered := scenes[:0]
		for _, scene := range scenes {
			if semanticNormalizeProjectPath(scene.ProjectPath) == projectLower {
				filtered = append(filtered, scene)
			}
		}
		scenes = filtered
	}
	return FormatSceneIndexForPrompt(scenes, maxScenes, maxArtifacts)
}

// UserFactSummaryPromptOptions controls how a compact user_fact summary is
// rendered into host prompts.
type UserFactSummaryPromptOptions struct {
	// MaxRunes is passed to Store.UserFactSummary. When zero, 400 is used.
	MaxRunes int
	// Template, when set, is formatted with the summary as its only argument.
	// It is useful for host-local localized section templates.
	Template string
	Header   string
	Prefix   string
	Suffix   string
	// OwnerID and StrictOwner apply the same opt-in exact-owner boundary used by
	// isolated group prompts. Zero values retain the historical shared summary.
	OwnerID     string
	StrictOwner bool
}

// UserFactSummaryForPrompt builds the shared user profile prompt section used
// by GUI, TUI, VE, and server prompts.
func (s *Store) UserFactSummaryForPrompt(opts UserFactSummaryPromptOptions) string {
	if s == nil {
		return ""
	}
	maxRunes := opts.MaxRunes
	if maxRunes <= 0 {
		maxRunes = 400
	}
	return FormatUserFactSummaryForPrompt(s.userFactSummaryForPrompt(maxRunes, opts.OwnerID, opts.StrictOwner), opts)
}

func (s *Store) userFactSummaryForPrompt(maxRunes int, ownerID string, strictOwner bool) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return s.UserFactSummary(maxRunes)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	parts := make([]string, 0)
	for _, entry := range s.entries {
		if entry.Category != CategoryUserFact {
			continue
		}
		if strictOwner && entry.OwnerID != ownerID {
			continue
		}
		if !strictOwner && entry.OwnerID != "" && entry.OwnerID != ownerID {
			continue
		}
		if entry.Boundary != nil && entry.Boundary.OwnerID != "" && entry.Boundary.OwnerID != ownerID {
			continue
		}
		parts = append(parts, firstNonEmptyString(entry.CompactForm, entry.Content))
	}
	summary := strings.Join(parts, " | ")
	if runes := []rune(summary); maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return summary
}

// FormatUserFactSummaryForPrompt renders an already-computed user_fact summary
// with consistent empty handling and newline boundaries.
func FormatUserFactSummaryForPrompt(summary string, opts UserFactSummaryPromptOptions) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	if opts.Template != "" {
		return fmt.Sprintf(opts.Template, summary)
	}

	var b strings.Builder
	if opts.Header != "" {
		b.WriteString(opts.Header)
		if !strings.HasSuffix(opts.Header, "\n") {
			b.WriteString("\n")
		}
	}
	if opts.Prefix != "" {
		b.WriteString(opts.Prefix)
	}
	b.WriteString(summary)
	if opts.Suffix != "" {
		b.WriteString(opts.Suffix)
	} else if !strings.HasSuffix(summary, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// StaticMemoryPromptOptions controls the stable memory section that can be
// cached across a session: user facts, recall hint, and optional save guide.
type StaticMemoryPromptOptions struct {
	UserFacts         UserFactSummaryPromptOptions
	IncludeRecallHint bool
	RecallHint        string
	IncludeGuide      bool
	Guide             string
	GuidePrefix       string
	GuideSuffix       string
}

// StaticMemorySectionForPrompt builds the shared static memory prompt section
// used by GUI and server prompts.
func (s *Store) StaticMemorySectionForPrompt(opts StaticMemoryPromptOptions) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	if opts.UserFacts != (UserFactSummaryPromptOptions{}) {
		facts := s.UserFactSummaryForPrompt(opts.UserFacts)
		if facts != "" {
			b.WriteString(facts)
		} else if opts.UserFacts.Header != "" && (opts.IncludeRecallHint || opts.IncludeGuide) {
			b.WriteString(opts.UserFacts.Header)
			if !strings.HasSuffix(opts.UserFacts.Header, "\n") {
				b.WriteByte('\n')
			}
		}
	}

	if opts.IncludeRecallHint {
		hint := opts.RecallHint
		if hint == "" {
			hint = DefaultRecallHintForPrompt()
		}
		writeMemoryPromptLine(&b, hint)
	}

	if opts.IncludeGuide {
		guide := opts.Guide
		if guide == "" {
			guide = BuildIMMemoryGuidePrompt()
		}
		if guide != "" {
			b.WriteString(opts.GuidePrefix)
			b.WriteString(guide)
			if opts.GuideSuffix != "" {
				b.WriteString(opts.GuideSuffix)
			} else if !strings.HasSuffix(guide, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// DefaultRecallHintForPrompt returns the shared instruction for explicit memory
// recall when automatically injected memory is not enough.
func DefaultRecallHintForPrompt() string {
	return "\u5f53\u524d\u4efb\u52a1\u4e0d\u9884\u88c5\u8bb0\u5fc6\u6216\u77e5\u8bc6\u5e93\u6b63\u6587\u3002\u5982\u9700\u76f8\u5173\u7ecf\u9a8c\u6216\u8d44\u6599\uff0c\u8c03\u7528\u5f53\u524d\u5de5\u5177\u5217\u8868\u4e2d\u7684\u8bb0\u5fc6\u68c0\u7d22\uff08" + PromptActionRecallColon + ", query: \"\u5173\u952e\u8bcd\")\uff09\u6216\u77e5\u8bc6\u5e93\u68c0\u7d22\uff1b\u4e0d\u8981\u628a\u8bb0\u5fc6\u7d22\u5f15\u5f53\u4f5c\u5df2\u786e\u8ba4\u4e8b\u5b9e\u3002"
}

func writeMemoryPromptLine(b *strings.Builder, value string) {
	if value == "" {
		return
	}
	b.WriteString(value)
	if !strings.HasSuffix(value, "\n") {
		b.WriteByte('\n')
	}
}

func formatMemoryIndexCount(count int, unit string) string {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		unit = "entries"
	}
	if isASCIIWord(unit) {
		return fmt.Sprintf("%d %s", count, unit)
	}
	return fmt.Sprintf("%d%s", count, unit)
}

func isASCIIWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'A' || (r > 'Z' && r < 'a') || r > 'z' {
			return false
		}
	}
	return true
}
