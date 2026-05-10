package main

import "strings"

type agentViewControlMessageKind int

const (
	agentViewControlMessageUnknown agentViewControlMessageKind = iota
	agentViewControlMessageSubmit
	agentViewControlMessageDismiss
)

const (
	agentViewControlSubmitPrefix  = "__agent_view_submit__ "
	agentViewControlDismissPrefix = "__agent_view_dismiss__ "
)

type agentViewControlMessage struct {
	Kind agentViewControlMessageKind
	Raw  string
}

func classifyAgentViewControlMessage(text string) agentViewControlMessage {
	trimmed := strings.TrimSpace(text)
	if raw, ok := strings.CutPrefix(trimmed, agentViewControlDismissPrefix); ok {
		return agentViewControlMessage{Kind: agentViewControlMessageDismiss, Raw: strings.TrimSpace(raw)}
	}
	if raw, ok := strings.CutPrefix(trimmed, agentViewControlSubmitPrefix); ok {
		return agentViewControlMessage{Kind: agentViewControlMessageSubmit, Raw: strings.TrimSpace(raw)}
	}
	return agentViewControlMessage{Kind: agentViewControlMessageUnknown}
}
