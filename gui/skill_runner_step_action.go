package main

import cskill "github.com/RapidAI/CodeClaw/corelib/skill"

type skillStepActionKind string

const (
	skillStepActionUnknown        skillStepActionKind = ""
	skillStepActionCreateSession  skillStepActionKind = "create_session"
	skillStepActionSendInput      skillStepActionKind = "send_input"
	skillStepActionSendAndObserve skillStepActionKind = "send_and_observe"
	skillStepActionControlSession skillStepActionKind = "control_session"
	skillStepActionCallMCPTool    skillStepActionKind = "call_mcp_tool"
	skillStepActionSSH            skillStepActionKind = "ssh"
	skillStepActionBash           skillStepActionKind = "bash"
	skillStepActionCraftTool      skillStepActionKind = "craft_tool"
	skillStepActionPoll           skillStepActionKind = "poll"
	skillStepActionSSHBash        skillStepActionKind = "ssh_bash"
	skillStepActionSSHListDir     skillStepActionKind = "ssh_list_dir"
	skillStepActionSSHReadFile    skillStepActionKind = "ssh_read_file"
	skillStepActionTodoWrite      skillStepActionKind = "todo_write"
)

func classifySkillStepAction(action string) skillStepActionKind {
	switch skillStepActionKind(cskill.NormalizeStepActionName(action)) {
	case skillStepActionCreateSession:
		return skillStepActionCreateSession
	case skillStepActionSendInput:
		return skillStepActionSendInput
	case skillStepActionSendAndObserve:
		return skillStepActionSendAndObserve
	case skillStepActionControlSession:
		return skillStepActionControlSession
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
	case skillStepActionSSHBash:
		return skillStepActionSSHBash
	case skillStepActionSSHListDir:
		return skillStepActionSSHListDir
	case skillStepActionSSHReadFile:
		return skillStepActionSSHReadFile
	case skillStepActionTodoWrite:
		return skillStepActionTodoWrite
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

func (k skillStepActionKind) UsesLegacySessionProcessEnv() bool {
	switch k {
	case skillStepActionCreateSession, skillStepActionSendInput, skillStepActionSendAndObserve, skillStepActionControlSession:
		return true
	default:
		return false
	}
}

func (k skillStepActionKind) UsesManagedProcessEnv() bool {
	switch k {
	case skillStepActionBash, skillStepActionCraftTool, skillStepActionPoll:
		// bash/craft_tool inject env per-subprocess via cmd.Env (BuildCommandEnv).
		// poll launches bash subprocesses internally and likewise receives env
		// through cmd.Env once its params carry the env, so it must NOT pin the
		// global os.Setenv mutex across its (up to minutes-long) poll loop.
		return false
	case skillStepActionCallMCPTool:
		// MCP calls do not launch a per-step child process here. Local MCP servers
		// have their own owner-scoped sessions, and remote MCP calls receive args
		// directly, so holding the process-env mutex would only serialize otherwise
		// independent agent instances.
		return false
	case skillStepActionCreateSession, skillStepActionSendInput, skillStepActionSendAndObserve, skillStepActionControlSession:
		// Legacy external coding-session actions are rejected before execution.
		return false
	default:
		return true
	}
}
