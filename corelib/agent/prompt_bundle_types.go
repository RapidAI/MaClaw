package agent

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PromptBundle splits the system prompt into segments for prompt-caching strategies.
// StableSystemPrompt rarely changes (role, rules, tool descriptions).
// SessionContext changes per session (system info, SSH hosts, MCP tools, coding state).
// RetrievedContext changes per message (skills, steering, memory, knowledge, profile).
type PromptBundle struct {
	StableSystemPrompt string
	SessionContext     string
	RetrievedContext   string
}

// String concatenates all non-empty segments with newline separators,
// producing the full system prompt text.
func (p PromptBundle) String() string {
	var parts []string
	if s := strings.TrimSpace(p.StableSystemPrompt); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(p.SessionContext); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(p.RetrievedContext); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// PromptBundleTokenStats holds estimated token counts per segment.
type PromptBundleTokenStats struct {
	StableSystemPromptTokens int
	SessionContextTokens     int
	RetrievedContextTokens   int
	TotalTokens              int
}

// TokenStats returns estimated token counts for each segment.
func (p PromptBundle) TokenStats() PromptBundleTokenStats {
	stable := EstimateBytesToTokens([]byte(p.StableSystemPrompt))
	session := EstimateBytesToTokens([]byte(p.SessionContext))
	retrieved := EstimateBytesToTokens([]byte(p.RetrievedContext))
	return PromptBundleTokenStats{
		StableSystemPromptTokens: stable,
		SessionContextTokens:     session,
		RetrievedContextTokens:   retrieved,
		TotalTokens:              stable + session + retrieved,
	}
}

// BuildPromptBundle builds the system prompt as cache-friendly segments.
func BuildPromptBundle(deps SystemPromptDeps, userMessage string, isFirstTurn bool) PromptBundle {
	var stable strings.Builder
	var session strings.Builder
	var retrieved strings.Builder

	cfg := deps.Config
	roleName := strings.TrimSpace(cfg.RoleName)
	if roleName == "" {
		roleName = "MaClaw"
	}
	roleDesc := strings.TrimSpace(cfg.RoleDescription)
	if roleDesc == "" {
		roleDesc = "a careful, capable software and personal assistant"
	}
	roleTitle := "AI personal assistant"
	if cfg.IsProMode {
		roleTitle = "AI coding assistant"
	}

	fmt.Fprintf(&stable, "You are %s, %s: %s. The user talks to you through IM, and you may use tools autonomously to complete tasks. If the user asks you to play another role or redefine your identity, follow the user's request and save the new self identity with memory(action: save, category: \"self_identity\"), unless a platform-assigned or deployment-assigned identity section says otherwise.\n", roleName, roleTitle, roleDesc)
	stable.WriteString(PromptOutputFormatRules)
	stable.WriteString(PromptCorePrinciples)
	stable.WriteString(PromptEvidenceBoundFactualRules)
	if deps.HasKnowledgeBase {
		stable.WriteString(PromptKnowledgeBaseRules)
	}
	if deps.PostCorePrinciples != nil {
		deps.PostCorePrinciples(&stable)
	}
	stable.WriteString(PromptEncodingRules)
	stable.WriteString(PromptSSHRules)
	if cfg.IsProMode {
		appendInternalCodingWorkflowRules(&stable)
	}

	home, _ := os.UserHomeDir()
	workspaceDir := corelib.EffectiveWorkspaceDir()
	fmt.Fprintf(&session, "\nCurrent system: %s/%s\nUser home: %s\nDefault workspace: %s\n", runtime.GOOS, runtime.GOARCH, home, workspaceDir)
	if deps.SSHHostLister != nil {
		if hosts := deps.SSHHostLister(); len(hosts) > 0 {
			session.WriteString("\nConfigured SSH hosts:\n")
			for _, host := range hosts {
				port := host.Port
				if port == 0 {
					port = 22
				}
				fmt.Fprintf(&session, "  - %s -> %s@%s:%d\n", host.Label, host.User, host.Host, port)
			}
		}
	}
	if deps.PostSSHRules != nil {
		deps.PostSSHRules(&session)
	}
	if deps.PostCodingWorkflow != nil {
		deps.PostCodingWorkflow(&session)
	}
	if deps.MCPServerLister != nil {
		if servers := deps.MCPServerLister(); len(servers) > 0 {
			session.WriteString("\n## Registered MCP Servers\n")
			for _, s := range servers {
				fmt.Fprintf(&session, "- %s (%s): %s\n", s.Name, s.ID, strings.Join(s.Tools, ", "))
			}
		}
	}

	if deps.MemoryStore != nil {
		if selfIdentityOverride := deps.MemoryStore.SelfIdentitySummary(600); selfIdentityOverride != "" {
			fmt.Fprintf(&retrieved, "\nSelf identity memory for %s:\n%s\nUse this only to guide behavior; do not recite it to the user unless asked.\n", roleName, selfIdentityOverride)
		}
	}
	if deps.SkillLister != nil {
		if skills := deps.SkillLister(); len(skills) > 0 {
			retrieved.WriteString("\n## Registered Skills\n")
			retrieved.WriteString("Call with manage_skill(action=\"run\", name=\"SkillName\", args={...}).\n")
			for _, s := range skills {
				fmt.Fprintf(&retrieved, "- %s: %s\n", s.Name, s.Description)
			}
		}
	}
	if deps.SteeringResolver != nil {
		resolved := deps.SteeringResolver(userMessage, 110000)
		if len(resolved) > 0 {
			retrieved.WriteString("\n## User Rules (Steering)\n")
			for _, sf := range resolved {
				fmt.Fprintf(&retrieved, "\n### %s\n%s", strings.TrimSuffix(sf.Name, ".md"), sf.Content)
				if !strings.HasSuffix(sf.Content, "\n") {
					retrieved.WriteString("\n")
				}
			}
		}
	}
	if deps.MemoryStore != nil && userMessage != "" && !deps.SkipMemoryRecall {
		appendMemoryRecall(&retrieved, deps.MemoryStore, userMessage, isFirstTurn)
	}
	if deps.KnowledgeAutoRecall != nil && userMessage != "" {
		deps.KnowledgeAutoRecall(&retrieved, userMessage)
	}
	if deps.UserProfileSection != nil {
		if section := strings.TrimSpace(deps.UserProfileSection()); section != "" {
			retrieved.WriteString("\n\n")
			retrieved.WriteString(section)
		}
	}
	if deps.Epilogue != nil {
		deps.Epilogue(&retrieved)
	}

	return PromptBundle{
		StableSystemPrompt: stable.String(),
		SessionContext:     session.String(),
		RetrievedContext:   retrieved.String(),
	}
}

func appendInternalCodingWorkflowRules(b *strings.Builder) {
	b.WriteString(`
## Coding task workflow
### Step 1: classify the task
- Coding_Task: requests that modify project code, fix bugs, refactor, or implement features use the internal CodingSubAgent path.
- SSH/server operation tasks: log in to servers, run remote commands, inspect logs, or restart services use SSH tools.
- Other non-coding tasks: answer, inspect files, manage configuration, or produce documents directly.

Do not enter CodingSubAgent for research, translation, document generation, file transfer, reminders, schedule lookup, or other ordinary assistant tasks.

### Coding execution
When the task is a coding task, work through the internal CodingSubAgent flow and follow the available filesystem, search, edit, shell, and test tools for this runtime. Prefer incremental edits over whole-file rewrites, compile or lint touched files when practical, and run focused tests before finishing.

### Confirmation gate
When a message is a coding task and the user did not include a skip signal:
- First response must be a requirements document; do not start coding yet.
- Do not enter design before the user confirms the requirements document.
- Do not enter task decomposition before the user confirms the design document.
- Do not start CodingSubAgent coding before the user confirms the task list.

### Skip signals
If the user message contains one of these expressions, skip all confirmation stages:
- Chinese: 直接做, 不用问了, 按你的想法来, 直接开始
- English: just do it, skip confirmation, go ahead
`)
}
