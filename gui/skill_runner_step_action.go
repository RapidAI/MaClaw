package main

import cskill "github.com/RapidAI/CodeClaw/corelib/skill"

type skillStepActionKind string

const (
	skillStepActionUnknown        skillStepActionKind = ""
	skillStepActionCreateSession  skillStepActionKind = "create_session"
	skillStepActionSendInput      skillStepActionKind = "send_input"
	skillStepActionSendAndObserve skillStepActionKind = "send_and_observe"
	skillStepActionCallMCPTool    skillStepActionKind = "call_mcp_tool"
	skillStepActionSSH            skillStepActionKind = "ssh"
	skillStepActionBash           skillStepActionKind = "bash"
	skillStepActionCraftTool      skillStepActionKind = "craft_tool"
	skillStepActionPoll           skillStepActionKind = "poll"
)

func classifySkillStepAction(action string) skillStepActionKind {
	switch skillStepActionKind(cskill.NormalizeStepActionName(action)) {
	case skillStepActionCreateSession:
		return skillStepActionCreateSession
	case skillStepActionSendInput:
		return skillStepActionSendInput
	case skillStepActionSendAndObserve:
		return skillStepActionSendAndObserve
	case skillStepActionCallMCPTool:
		return skillStepActionCallMCPTool
	case skillStepActionSSH:
		return skillStepActionSSH
	case skillStepActionBash:
		return skillStepActionBash
	case skillStepActionCraftTool:
		return skillStepActionCraftTool
	case skillStepActionPoll:
		return skillStepActionPoll
	default:
		return skillStepActionUnknown
	}
}

func (k skillStepActionKind) IsBash() bool {
	return k == skillStepActionBash
}

func (k skillStepActionKind) IsCraftTool() bool {
	return k == skillStepActionCraftTool
}

func (k skillStepActionKind) UsesManagedProcessEnv() bool {
	switch k {
	case skillStepActionBash, skillStepActionCraftTool:
		return false
	default:
		return true
	}
}
