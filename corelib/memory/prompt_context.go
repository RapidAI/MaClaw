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
	return FormatUserFactSummaryForPrompt(s.UserFactSummary(maxRunes), opts)
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
		b.WriteString(s.UserFactSummaryForPrompt(opts.UserFacts))
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
	return "\u5982\u9700\u66f4\u591a\u8bb0\u5fc6\uff0c\u53ef\u901a\u8fc7 " + PromptActionRecallColon + ", query: \"\u5173\u952e\u8bcd\") \u53ec\u56de\u3002"
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
