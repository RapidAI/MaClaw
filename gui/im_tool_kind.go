package main

import "strings"

type agentToolKind int

const (
	agentToolKindUnknown agentToolKind = iota
	agentToolKindBash
	agentToolKindReadFile
	agentToolKindEditFile
	agentToolKindWriteFile
	agentToolKindListDirectory
	agentToolKindGeneratePDF
	agentToolKindOffice
	agentToolKindCraftTool
	agentToolKindRunSkill
	agentToolKindGetSkillRun
	agentToolKindListSkills
	agentToolKindSearchSkillHub
	agentToolKindInstallSkillHub
	agentToolKindSearchAndInstallSkill
	agentToolKindManageSkill
	agentToolKindWebFetch
	agentToolKindWebSearch
	agentToolKindSendFile
	agentToolKindSSH
	agentToolKindMemory
	agentToolKindDelegateTask
	agentToolKindCreateSession
)

func classifyAgentToolKind(name string) agentToolKind {
	switch strings.TrimSpace(name) {
	case "bash":
		return agentToolKindBash
	case "read_file":
		return agentToolKindReadFile
	case "edit_file", "edit_lines":
		return agentToolKindEditFile
	case "write_file":
		return agentToolKindWriteFile
	case "list_directory":
		return agentToolKindListDirectory
	case "generate_pdf":
		return agentToolKindGeneratePDF
	case "office":
		return agentToolKindOffice
	case "craft_tool":
		return agentToolKindCraftTool
	case "run_skill":
		return agentToolKindRunSkill
	case "get_skill_run":
		return agentToolKindGetSkillRun
	case "list_skills":
		return agentToolKindListSkills
	case "search_skill_hub":
		return agentToolKindSearchSkillHub
	case "install_skill_hub":
		return agentToolKindInstallSkillHub
	case "search_and_install_skill":
		return agentToolKindSearchAndInstallSkill
	case "manage_skill":
		return agentToolKindManageSkill
	case "web_fetch":
		return agentToolKindWebFetch
	case "web_search":
		return agentToolKindWebSearch
	case "send_file", "send_to_im", "im_message":
		return agentToolKindSendFile
	case "ssh":
		return agentToolKindSSH
	case "memory":
		return agentToolKindMemory
	case "delegate_task":
		return agentToolKindDelegateTask
	case "create_session":
		return agentToolKindCreateSession
	default:
		return agentToolKindUnknown
	}
}

func (k agentToolKind) IsSkillTool() bool {
	return k.IsSkillProgressTool() || k.IsSkillSearchTool()
}

func (k agentToolKind) IsSkillSearchTool() bool {
	switch k {
	case agentToolKindSearchAndInstallSkill, agentToolKindSearchSkillHub, agentToolKindInstallSkillHub:
		return true
	default:
		return false
	}
}

func (k agentToolKind) IsSkillProgressTool() bool {
	switch k {
	case agentToolKindRunSkill, agentToolKindGetSkillRun, agentToolKindListSkills, agentToolKindManageSkill:
		return true
	default:
		return false
	}
}

func (k agentToolKind) IsBlockedBySkillPreference() bool {
	switch k {
	case agentToolKindCraftTool, agentToolKindBash, agentToolKindCreateSession:
		return true
	default:
		return false
	}
}

func (k agentToolKind) IsCodingIterationTool() bool {
	switch k {
	case agentToolKindBash, agentToolKindEditFile, agentToolKindWriteFile:
		return true
	default:
		return false
	}
}

func (k agentToolKind) PreserveAfterTruncation() bool {
	switch k {
	case agentToolKindBash, agentToolKindReadFile, agentToolKindListDirectory, agentToolKindWriteFile, agentToolKindDelegateTask:
		return true
	default:
		return false
	}
}

func (k agentToolKind) TraceCategory(execResult toolExecutionResult) traceEvidenceCategory {
	if execResult.IsFailure() {
		return traceEvidenceCategoryError
	}
	switch k {
	case agentToolKindWriteFile, agentToolKindGeneratePDF, agentToolKindOffice, agentToolKindSendFile:
		return traceEvidenceCategoryFile
	default:
		return traceEvidenceCategoryEvent
	}
}
