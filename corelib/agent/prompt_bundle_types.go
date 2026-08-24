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
// When Config.PromptProfile is light, bulk policy (coding gate, SSH, MCP,
// skill catalog, long evidence rules) is omitted to cut token cost on simple turns.
func BuildPromptBundle(deps SystemPromptDeps, userMessage string, isFirstTurn bool) PromptBundle {
	var stable strings.Builder
	var session strings.Builder
	var retrieved strings.Builder

	cfg := deps.Config
	light := NormalizePromptProfile(string(cfg.PromptProfile)).IsLight()
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

	// Light wins over ManagedSemantic: a weather-only grant must keep
	// "don't write files / don't generate documents". Hosts that planned a
	// mutating capability (document.generate, deliver) must force full first.
	if light {
		fmt.Fprintf(&stable, "You are %s, %s: %s. Handle this as a low-complexity turn; prefer a concise answer and only use tools when necessary.\n", roleName, roleTitle, roleDesc)
		stable.WriteString(PromptOutputFormatRules)
		stable.WriteString(PromptCorePrinciplesLight)
		if deps.HasKnowledgeBase {
			// Keep a one-liner instead of full knowledge-base policy.
			stable.WriteString("\nIf knowledge-base context appears below, prefer it over guessing.\n")
		}
	} else if cfg.ManagedSemantic {
		fmt.Fprintf(&stable, "You are %s, %s: %s. Handle this turn with the tools currently listed; do not invent a larger catalog.\n", roleName, roleTitle, roleDesc)
		stable.WriteString(PromptOutputFormatRules)
		stable.WriteString(PromptCorePrinciplesManaged)
		if deps.HasKnowledgeBase {
			stable.WriteString("\nIf knowledge-base context appears below, prefer it over guessing.\n")
		}
		if deps.PostCorePrinciples != nil {
			deps.PostCorePrinciples(&stable)
		}
	} else {
		fmt.Fprintf(&stable, "You are %s, %s: %s. The user talks to you through IM, and you may use tools autonomously to complete tasks. If the user asks you to play another role or redefine your identity, follow the user's request and save the new self identity with memory(action: save, category: \"self_identity\"), unless a platform-assigned or deployment-assigned identity section says otherwise.\n", roleName, roleTitle, roleDesc)
		stable.WriteString(PromptOutputFormatRules)
		stable.WriteString(PromptCorePrinciples)
		stable.WriteString(PromptWorkingStateContract)
		stable.WriteString(PromptEvidenceBoundFactualRules)
		if deps.HasKnowledgeBase {
			stable.WriteString(PromptKnowledgeBaseRules)
		}
		if deps.PostCorePrinciples != nil {
			deps.PostCorePrinciples(&stable)
		}
		stable.WriteString(PromptEncodingRules)
		stable.WriteString(PromptSSHRules)
		if cfg.IsProMode && !cfg.SuppressCodingGateRules {
			appendInternalCodingWorkflowRules(&stable)
		}
	}

	home, _ := os.UserHomeDir()
	// Resolve the effective project directory — the single source of truth
	// matching what resolveToolWorkDir("") and resolveFileToolPath() actually
	// use at tool execution time.
	projectDir := corelib.EffectiveWorkspaceDir()
	if deps.EffectiveProjectDir != nil {
		if dir := deps.EffectiveProjectDir(); dir != "" {
			projectDir = dir
		}
	}
	scratchDir := os.TempDir()
	if deps.ScratchDir != nil {
		if dir := deps.ScratchDir(); dir != "" {
			scratchDir = dir
		}
	}
	fmt.Fprintf(&session, "\nCurrent system: %s/%s\nUser home: %s\nProject directory: %s\nTemp directory: %s\n", runtime.GOOS, runtime.GOARCH, home, projectDir, scratchDir)
	if light {
		session.WriteString("Relative tool paths resolve against Project directory. Prefer answering without tools unless live data is needed.\n")
		session.WriteString(fmt.Sprintf("Prompt profile: light (adaptive)\n"))
	} else {
		if cfg.ManagedSemantic {
			session.WriteString("Relative tool paths resolve against Project directory. Use Temp directory for scratch files.\n")
		} else {
			session.WriteString("All relative paths in tools (read_file, write_file, edit_file, ripgrep, Glob) resolve against Project directory. bash cwd = Project directory unless working_dir is specified. Use Temp directory for scratch files.\n")
		}
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
		if !cfg.ManagedSemantic && deps.PostCodingWorkflow != nil {
			deps.PostCodingWorkflow(&session)
		}
		if !cfg.ManagedSemantic && deps.MCPServerLister != nil {
			if servers := deps.MCPServerLister(); len(servers) > 0 {
				session.WriteString("\n## Registered MCP Servers\n")
				for _, s := range servers {
					fmt.Fprintf(&session, "- %s (%s): %s\n", s.Name, s.ID, strings.Join(s.Tools, ", "))
				}
			}
		}
	}

	if deps.MemoryStore != nil {
		// Light turns only need a short self-identity cue if present.
		limit := 600
		if light {
			limit = 200
		}
		if selfIdentityOverride := deps.MemoryStore.SelfIdentitySummary(limit); selfIdentityOverride != "" {
			fmt.Fprintf(&retrieved, "\nSelf identity memory for %s:\n%s\nUse this only to guide behavior; do not recite it to the user unless asked.\n", roleName, selfIdentityOverride)
		}
	}
	if light && !cfg.ManagedSemantic && deps.SkillLister != nil {
		// Keep the catalog out of the cost-sensitive light prompt, but do not
		// hide the capability itself.  Otherwise a short follow-up such as
		// "run it again" loses both the skill list and manage_skill tool.
		// Managed turns have no manage_skill grant; teaching the name is a
		// guaranteed refuse.
		retrieved.WriteString("\n## Installed Skills\n")
		retrieved.WriteString("Installed Skills are available. When the user names, asks for, or wants to run a skill, use manage_skill(action=\"list\", \"search\", or \"run\") to find or execute it.\n")
	}
	if !light && !cfg.ManagedSemantic && deps.SkillLister != nil {
		if skills := deps.SkillLister(); len(skills) > 0 {
			retrieved.WriteString("\n## Registered Skills\n")
			retrieved.WriteString("Call with manage_skill(action=\"run\", name=\"SkillName\", args={...}).\n")
			retrieved.WriteString("Do NOT pre-check dependencies (Python/Node/etc.) with bash before running a Skill. The Skill Runner has built-in dependency checks and returns actionable error messages if something is missing.\n")
			for _, s := range skills {
				fmt.Fprintf(&retrieved, "- %s: %s\n", s.Name, s.Description)
			}
		}
	}
	// Steering user rules still apply on light turns (usually small and high priority).
	if deps.SteeringResolver != nil {
		budget := 110000
		if light {
			budget = 8000
		}
		resolved := deps.SteeringResolver(userMessage, budget)
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
	// Memory/knowledge auto-recall: skip on light turns to avoid stuffing context.
	if !light {
		if deps.MemoryStore != nil && userMessage != "" && !deps.SkipMemoryRecall {
			appendMemoryRecall(&retrieved, deps.MemoryStore, userMessage, isFirstTurn)
		}
	}
	if deps.UserProfileSection != nil {
		if section := strings.TrimSpace(deps.UserProfileSection()); section != "" {
			if !light || len(section) < 800 {
				retrieved.WriteString("\n\n")
				retrieved.WriteString(section)
			}
		}
	}
	if !light && deps.Epilogue != nil {
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
