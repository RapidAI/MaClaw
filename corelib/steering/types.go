// Package steering provides a declarative rule injection mechanism for MacLaw.
//
// Steering files are Markdown documents with optional YAML front-matter that
// declare how and when their content should be injected into the LLM system
// prompt. This replaces hardcoded rules in im_system_prompt.go with
// user-editable files in ~/.maclaw/steering/ (user-level) and
// <project>/.maclaw/steering/ (project-level).
//
// Inspired by Kiro's .kiro/steering/ mechanism, extended with contextMatch
// mode for MacLaw's IM-agent use case.
package steering

import "time"

// Scope indicates whether a steering file comes from user-level or project-level.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// InclusionMode determines when a steering file is injected into the system prompt.
type InclusionMode string

const (
	// InclusionAlways injects the file into every conversation.
	InclusionAlways InclusionMode = "always"

	// InclusionFileMatch injects when the conversation context contains
	// files matching the FileMatchPattern glob.
	InclusionFileMatch InclusionMode = "fileMatch"

	// InclusionContextMatch injects when the user message contains any of
	// the ContextKeywords. This is MacLaw-specific (Kiro doesn't have it),
	// corresponding to the conditionalKeepRules keyword matching mechanism
	// but for rules instead of tools.
	InclusionContextMatch InclusionMode = "contextMatch"

	// InclusionManual injects only when the user explicitly references the
	// file via #name in their IM message.
	InclusionManual InclusionMode = "manual"
)

// File represents a loaded steering rule file.
type File struct {
	// Name is the filename without directory path, used for same-name override
	// between user-level and project-level files.
	Name string

	// Scope indicates whether this file is user-level or project-level.
	Scope Scope

	// Inclusion determines when this file is injected.
	Inclusion InclusionMode

	// FileMatchPattern is a glob pattern for InclusionFileMatch mode.
	// Example: "*.go", "src/**/*.ts"
	FileMatchPattern string

	// ContextKeywords is a list of keywords for InclusionContextMatch mode.
	// Any keyword match (case-insensitive substring) triggers injection.
	ContextKeywords []string

	// Priority controls injection order. Lower values are injected first.
	// Default: 100.
	Priority int

	// Overridable indicates whether a project-level file with the same Name
	// can override this file. Default: true.
	Overridable bool

	// Content is the Markdown body (without YAML front-matter).
	Content string

	// SourcePath is the full filesystem path, for logging and debugging.
	SourcePath string

	// ModTime is the file modification time, for hot-reload detection.
	ModTime time.Time
}

// ResolveContext provides the information needed to determine which steering
// files should be injected for the current conversation turn.
type ResolveContext struct {
	// UserMessage is the current user message text.
	UserMessage string

	// ContextFiles are file paths read/edited in the current conversation,
	// used for InclusionFileMatch matching.
	ContextFiles []string

	// ManualRefs are steering file names explicitly referenced by the user
	// via #name syntax in their IM message.
	ManualRefs []string

	// EffectiveContextTokens is the usable context window size from
	// MaclawLLMConfig.EffectiveContextTokens(). Used to dynamically scale
	// the steering token budget for smaller context models.
	EffectiveContextTokens int
}
