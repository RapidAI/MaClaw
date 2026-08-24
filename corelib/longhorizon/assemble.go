package longhorizon

import "strings"

func ManagerSystemPrompt() string {
	return strings.TrimSpace(`
You are the LongHorizon manager. You only schedule the next bounded unit of work.
You have no tools. Do not write code, click UI, or search a knowledge base.

Reply with labeled fields:
Next: cli | gui | browser | ask | blocked | done
Goal:
Acceptance:
Related:
Question:

Rules:
- One Next value. Fail closed: if unsure, use ask.
- CLI is repository file/shell work. GUI is desktop computer-use. Browser is web.
- done is only allowed after a real auditor report of complete + clean + aligned.
- Do not paste prior executor transcripts or chat memory.
`)
}

func CLIExecutorSystemPrompt() string {
	return strings.TrimSpace(`
You are a coding executor for one bounded task. Use only the listed tools.
Do not search a knowledge base. Do not call computer-use, MCP, skills, or spawn agents.
Do not continue after the current goal and acceptance criteria are met.
`)
}

func CLIAuditorSystemPrompt() string {
	return strings.TrimSpace(`
You are a read-only auditor. Treat executor output as a claim.
You have no action tools. Prefer the mechanical probe digest.
If there is no probe digest, integrity cannot be clean.

Reply with:
Status: complete | incomplete | blocked
Integrity: clean | suspect | violation
Alignment: aligned | drifted
Summary:
`)
}

func GUIExecutorSystemPrompt() string {
	return strings.TrimSpace(`
You are a desktop computer-use executor for one bounded task.
Use only computer_* tools. Do not use browser, shell, files, MCP, skills, or spawn.
computer_done reports a claim; it does not complete the outer long-horizon task.
Observe after every action. Stop when the current goal and acceptance are met.
`)
}

func GUIAuditorSystemPrompt() string {
	return strings.TrimSpace(`
You are a read-only desktop auditor. Treat executor output as a claim.
You have no action tools. Prefer the latest observe digest.
If there is no observe digest, integrity cannot be clean.
Do not call computer_focus.

Reply with:
Status: complete | incomplete | blocked
Integrity: clean | suspect | violation
Alignment: aligned | drifted
Summary:
`)
}

func BrowserExecutorSystemPrompt() string {
	return strings.TrimSpace(`
You are a browser executor for one bounded task.
Use only browser_* tools. Do not use computer-use, shell, files, MCP, skills, or spawn.
If the page shows a captcha or needs the user, stop and say so; do not guess.
Stop when the current goal and acceptance are met.
`)
}

func BrowserAuditorSystemPrompt() string {
	return strings.TrimSpace(`
You are a read-only browser auditor. Treat executor output as a claim.
You have no action tools. Prefer the mechanical page digest (url, title, text, flags).
If there is no page digest, integrity cannot be clean.

Reply with:
Status: complete | incomplete | blocked
Integrity: clean | suspect | violation
Alignment: aligned | drifted
Summary:
`)
}

func AssembleEpisodeContext(role string, plan ManagerPlan, state *TaskState, evidence string, policy PolicySnapshot) EpisodeContext {
	ep := EpisodeContext{
		Role:          role,
		ToolSurface:   DefaultSurfaceForRole(role),
		Policy:        policy,
		Budget:        EpisodeBudget{MaxIterations: CLIMaxIterations},
		RelatedAudits: clipAuditList(plan.RelatedAudits),
		Evidence:      clipRunes(evidence, VerifiedContextCap),
	}
	switch role {
	case RoleManager:
		ep.SystemPrompt = ManagerSystemPrompt()
		ep.Goal = clipRunes(firstNonEmpty(stateUserGoal(state), plan.Goal), GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.History = clipRunes(managerHistory(state), ManagerHistoryCap)
		ep.ToolSurface = nil
		ep.Budget.MaxIterations = 1
	case RoleCLIExecutor:
		ep.SystemPrompt = CLIExecutorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
	case RoleGUIExecutor:
		ep.SystemPrompt = GUIExecutorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.Budget.MaxIterations = GUIMaxIterations
	case RoleBrowserExecutor:
		ep.SystemPrompt = BrowserExecutorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.Budget.MaxIterations = BrowserMaxIterations
	case RoleCLIAuditor:
		ep.SystemPrompt = CLIAuditorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.ToolSurface = nil
		ep.Budget.MaxIterations = 1
	case RoleGUIAuditor:
		ep.SystemPrompt = GUIAuditorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.ToolSurface = nil
		ep.Budget.MaxIterations = 1
	case RoleBrowserAuditor:
		ep.SystemPrompt = BrowserAuditorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.ToolSurface = nil
		ep.Budget.MaxIterations = 1
	default:
		ep.SystemPrompt = CLIExecutorSystemPrompt()
		ep.Goal = clipRunes(plan.Goal, GoalCap)
		ep.Acceptance = clipRunes(plan.Acceptance, AcceptanceCap)
		ep.ToolSurface = nil
	}
	ep.Goal = clipRunes(ep.Goal, GoalCap)
	total := utf8Len(ep.Goal) + utf8Len(ep.Acceptance) + utf8Len(ep.Evidence) + utf8Len(strings.Join(ep.RelatedAudits, "\n"))
	if total > VerifiedContextCap {
		ep.Evidence = clipRunes(ep.Evidence, max(0, VerifiedContextCap/4))
		ep.RelatedAudits = clipAuditList(ep.RelatedAudits)
	}
	return ep
}

func AssembleManagerContext(plan ManagerPlan, state *TaskState, evidence string, policy PolicySnapshot) (EpisodeContext, bool) {
	ep := AssembleEpisodeContext(RoleManager, plan, state, evidence, policy)
	if AssembleIsClean(ep) {
		return ep, true
	}
	if strings.TrimSpace(evidence) == "" {
		return ep, false
	}
	ep = AssembleEpisodeContext(RoleManager, plan, state, "", policy)
	return ep, AssembleIsClean(ep)
}

func AssembleIsClean(ep EpisodeContext) bool {
	blob := strings.Join([]string{ep.SystemPrompt, ep.Goal, ep.Acceptance, ep.Evidence, ep.History, strings.Join(ep.RelatedAudits, "\n")}, "\n")
	if ContainsForbiddenPromptForRole(ep.Role, blob) {
		return false
	}
	if SurfaceViolatesRole(ep.Role, ep.ToolSurface) {
		return false
	}
	if ep.Role == RoleManager || IsAuditorRole(ep.Role) {
		if len(ep.ToolSurface) != 0 {
			return false
		}
	}
	return true
}

func stateUserGoal(state *TaskState) string {
	if state == nil {
		return ""
	}
	return state.UserGoal
}

func managerHistory(state *TaskState) string {
	if state == nil {
		return ""
	}
	var b strings.Builder
	for _, round := range state.Rounds {
		b.WriteString(clipRunes(round.Goal, 400))
		b.WriteByte('\n')
		if round.Audit != nil {
			b.WriteString(clipRunes(round.Audit.Summary, RelatedAuditEachCap))
			b.WriteByte('\n')
		}
	}
	for _, item := range state.Carryover {
		b.WriteString(clipRunes(item, 400))
		b.WriteByte('\n')
	}
	return clipRunes(b.String(), ManagerHistoryCap)
}

func clipAuditList(items []string) []string {
	out := make([]string, 0, RelatedAuditMax)
	for _, item := range items {
		item = clipRunes(item, RelatedAuditEachCap)
		if item == "" {
			continue
		}
		out = append(out, item)
		if len(out) >= RelatedAuditMax {
			break
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func utf8Len(s string) int {
	return len([]rune(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
