package main

import "strings"

type misAgentViewIDKind int

const (
	misAgentViewIDUnknown misAgentViewIDKind = iota
	misAgentViewIDSkillRun
	misAgentViewIDSkillStatus
	misAgentViewIDToolApproval
	misAgentViewIDToolRun
	misAgentViewIDMCPCall
	misAgentViewIDChooseIntent
	misAgentViewIDResumeTransaction
	misAgentViewIDIntent
	misAgentViewIDCommit
)

type misAgentViewID struct {
	Kind misAgentViewIDKind
	Arg  string
}

func classifyMISAgentViewID(viewID string) misAgentViewID {
	trimmed := strings.TrimSpace(viewID)
	switch {
	case trimmed == "skill:status":
		return misAgentViewID{Kind: misAgentViewIDSkillStatus}
	case trimmed == "tool:approval":
		return misAgentViewID{Kind: misAgentViewIDToolApproval}
	case trimmed == "mcp:call":
		return misAgentViewID{Kind: misAgentViewIDMCPCall}
	case trimmed == "mis:choose-intent":
		return misAgentViewID{Kind: misAgentViewIDChooseIntent}
	case trimmed == "mis:resume-transaction":
		return misAgentViewID{Kind: misAgentViewIDResumeTransaction}
	}
	if arg, ok := strings.CutPrefix(trimmed, "skill:run:"); ok {
		return misAgentViewID{Kind: misAgentViewIDSkillRun, Arg: strings.TrimSpace(arg)}
	}
	if arg, ok := strings.CutPrefix(trimmed, "tool:run:"); ok {
		return misAgentViewID{Kind: misAgentViewIDToolRun, Arg: strings.TrimSpace(arg)}
	}
	if arg, ok := strings.CutPrefix(trimmed, "mis:intent:"); ok {
		return misAgentViewID{Kind: misAgentViewIDIntent, Arg: strings.TrimSpace(arg)}
	}
	if arg, ok := strings.CutPrefix(trimmed, "mis:commit:"); ok {
		return misAgentViewID{Kind: misAgentViewIDCommit, Arg: strings.TrimSpace(arg)}
	}
	return misAgentViewID{Kind: misAgentViewIDUnknown}
}
