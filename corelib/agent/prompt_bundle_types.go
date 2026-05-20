package agent

import "strings"

// PromptBundle splits the system prompt into segments for prompt-caching strategies.
// StableSystemPrompt rarely changes (role, rules, tool descriptions).
// SessionContext changes per session (SSH hosts, active tools, coding state).
// RetrievedContext changes per message (skill index, MCP tools, memory).
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

// BuildPromptBundle builds the system prompt and splits it into a PromptBundle.
// The stable segment contains everything up to dynamic sections.
// SessionContext contains per-session dynamic state (SSH hosts, active tools, coding state).
// RetrievedContext contains per-message dynamic state (skill index, MCP tools).
func BuildPromptBundle(deps SystemPromptDeps, userMessage string, isDesktop bool) PromptBundle {
	// Build the full prompt using the existing function
	full := BuildSystemPrompt(deps, userMessage, isDesktop)

	// Split into segments based on dynamic section markers
	bundle := PromptBundle{}

	// Find session context boundary (SSH hosts, PostSSHRules, PostCodingWorkflow)
	sessionStart := -1

	// PostSSHRules and PostCodingWorkflow content goes into SessionContext
	if deps.PostSSHRules != nil {
		var probe strings.Builder
		deps.PostSSHRules(&probe)
		if marker := strings.TrimSpace(probe.String()); marker != "" {
			if idx := strings.Index(full, marker); idx >= 0 {
				if sessionStart < 0 || idx < sessionStart {
					sessionStart = idx
				}
			}
		}
	}
	if deps.PostCodingWorkflow != nil {
		var probe strings.Builder
		deps.PostCodingWorkflow(&probe)
		if marker := strings.TrimSpace(probe.String()); marker != "" {
			if idx := strings.Index(full, marker); idx >= 0 {
				if sessionStart < 0 || idx < sessionStart {
					sessionStart = idx
				}
			}
		}
	}

	// SSH hosts go into SessionContext
	if deps.SSHHostLister != nil {
		hosts := deps.SSHHostLister()
		if len(hosts) > 0 {
			if idx := strings.Index(full, hosts[0].Host); idx >= 0 {
				if sessionStart < 0 || idx < sessionStart {
					sessionStart = idx
				}
			}
		}
	}

	// Skill index and MCP tools go into RetrievedContext
	retrievedStart := -1
	if deps.SkillLister != nil {
		skills := deps.SkillLister()
		if len(skills) > 0 {
			if idx := strings.Index(full, skills[0].Name); idx >= 0 {
				retrievedStart = idx
			}
		}
	}
	if deps.MCPServerLister != nil {
		servers := deps.MCPServerLister()
		if len(servers) > 0 {
			if idx := strings.Index(full, servers[0].Name); idx >= 0 {
				if retrievedStart < 0 || idx < retrievedStart {
					retrievedStart = idx
				}
			}
		}
	}

	// Assign segments
	if sessionStart >= 0 && retrievedStart >= 0 {
		if sessionStart <= retrievedStart {
			bundle.StableSystemPrompt = full[:sessionStart]
			bundle.SessionContext = full[sessionStart:retrievedStart]
			bundle.RetrievedContext = full[retrievedStart:]
		} else {
			bundle.StableSystemPrompt = full[:retrievedStart]
			bundle.RetrievedContext = full[retrievedStart:sessionStart]
			bundle.SessionContext = full[sessionStart:]
		}
	} else if sessionStart >= 0 {
		bundle.StableSystemPrompt = full[:sessionStart]
		bundle.SessionContext = full[sessionStart:]
	} else if retrievedStart >= 0 {
		bundle.StableSystemPrompt = full[:retrievedStart]
		bundle.RetrievedContext = full[retrievedStart:]
	} else {
		bundle.StableSystemPrompt = full
	}

	return bundle
}
