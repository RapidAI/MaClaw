package agent

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/steering"
)

// SystemPromptConfig holds the configuration values needed to build the system prompt.
type SystemPromptConfig struct {
	RoleName          string
	RoleDescription   string
	IsProMode         bool
	Nickname          string
	TrialReflect      bool
	HasCodingSessions bool
}

// SystemPromptDeps holds the dependencies needed to build the system prompt.
// All fields are optional; nil fields disable the corresponding prompt sections.
type SystemPromptDeps struct {
	Config           SystemPromptConfig
	MemoryStore      *memory.Store
	SkipMemoryRecall bool

	SkillLister      func() []SkillInfo
	MCPServerLister  func() []MCPServerInfo
	SteeringResolver func(userMessage string, contextTokens int) []steering.File

	// CodingProviderInfo is retained for legacy callers but intentionally ignored
	// by prompt construction. Coding flow is described through the internal path.
	CodingProviderInfo func() string

	SSHHostLister       func() []corelib.SSHHostEntry
	UserProfileSection  func() string
	HasKnowledgeBase    bool
	KnowledgeAutoRecall func(b *strings.Builder, userMsg string)

	PostCorePrinciples func(b *strings.Builder)
	PostCodingWorkflow func(b *strings.Builder)
	PostSSHRules       func(b *strings.Builder)
	Epilogue           func(b *strings.Builder)
}

// SkillInfo describes an active skill for the system prompt.
type SkillInfo struct {
	Name        string
	Description string
	Publisher   string
}

// MCPServerInfo describes a registered MCP server for the system prompt.
type MCPServerInfo struct {
	ID    string
	Name  string
	Tools []string
}

// BuildSystemPrompt constructs the full system prompt from cache-friendly segments.
func BuildSystemPrompt(deps SystemPromptDeps, userMessage string, isFirstTurn bool) string {
	return BuildPromptBundle(deps, userMessage, isFirstTurn).String()
}

// appendMemoryRecall appends proactive memory recall to the system prompt.
func appendMemoryRecall(b *strings.Builder, store *memory.Store, userMessage string, isFirstTurn bool) {
	b.WriteString(store.UserFactSummaryForPrompt(memory.UserInfoPromptOptions("\n\n" + memory.PromptSectionUserMemory)))

	promptContext, _ := store.ProactiveContextForPrompt(userMessage, memory.CoreAgentProactivePromptOptions())
	b.WriteString(promptContext)

	b.WriteString(store.StaticMemorySectionForPrompt(memory.RecallHintAndGuidePromptOptions(isFirstTurn, memory.BuildTUIProactiveMemoryPrompt())))
}

// SteeringFileKey builds a per-user key for steering context file tracking.
func SteeringFileKey(userID, path string) string {
	return userID + "\x00" + path
}

// LooksLikeFilePath returns true if the string looks like a filesystem path
// rather than a URL, hostname, or other string value.
func LooksLikeFilePath(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "ftp://") {
		return false
	}
	hasSep := strings.ContainsAny(s, "/\\")
	hasDot := strings.Contains(s, ".")
	if hasDot && !hasSep {
		return false
	}
	return hasSep || hasDot
}

// ExtractSteeringRefs extracts #name references from user message text for
// manual steering file inclusion.
func ExtractSteeringRefs(text string) []string {
	if text == "" {
		return nil
	}
	var refs []string
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		if runes[i] != '#' {
			i++
			continue
		}
		j := i + 1
		for j < len(runes) {
			r := runes[j]
			if r >= 0x4e00 && r <= 0x9fff {
				j++
			} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				j++
			} else {
				break
			}
		}
		if j > i+1 {
			name := string(runes[i+1 : j])
			allDigits := true
			for _, r := range name {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if !allDigits {
				refs = append(refs, name)
			}
		}
		i = j
	}
	return refs
}

// FilePathParamNames are common parameter names that contain file paths.
var FilePathParamNames = []string{"path", "file_path", "file", "local_path", "source", "destination"}
