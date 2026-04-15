package skill

// RunVarFallbackKeys lists top-level parameter names that the LLM may pass
// outside the nested "args" object when calling manage_skill(action=run).
// Both GUI (normalizeSkillRunVars) and TUI (normalizeRunSkillVars) use this
// list so they stay in sync automatically.
//
// When adding a new run-time parameter to the manage_skill tool definition,
// add it here — both GUI and TUI will pick it up.
var RunVarFallbackKeys = []string{
	"input", "output", "query", "url", "text", "file", "path", "format",
	"operation", "user_prompt",
}
