package memory

import "strings"

const (
	PromptSectionUserMemory         = "## 用户记忆"
	PromptSectionMemoryGuide        = "## 记忆管理指引"
	PromptSectionProactiveMemory    = "## 主动记忆"
	PromptActionRecallColon         = "memory(action: recall"
	PromptActionSaveColon           = "memory(action: save)"
	PromptActionSaveEquals          = "memory(action=save)"
	PromptCategoryProjectKnowledge  = "project_knowledge"
	PromptCategoryInstruction       = "instruction"
	PromptTagProactive              = "proactive"
	PromptProactiveAck              = "💾 已主动记录"
	PromptSaveCategorySummary       = "user_fact | 偏好 → preference | 项目知识 → " + PromptCategoryProjectKnowledge + " | 指令 → " + PromptCategoryInstruction
)

func BuildIMMemoryGuidePrompt() string {
	return strings.Join([]string{
		PromptSectionMemoryGuide,
		"识别到有价值的信息时，主动调用 " + PromptActionSaveColon + " 保存：",
		"- " + PromptSaveCategorySummary,
	}, "\n")
}

func BuildTUIProactiveMemoryPrompt() string {
	return strings.Join([]string{
		PromptSectionProactiveMemory,
		"当你在会话中发现以下类型的非显而易见信息时，应主动使用 " + PromptActionSaveEquals + " 保存：",
		"- 调试过程中发现的 workaround 或未文档化行为",
		"- 配置细节、环境特殊性",
		"- 用户项目的架构决策或约定",
		"- 重要的错误原因和解决方案",
		"",
		"保存时使用 category=" + PromptCategoryProjectKnowledge + " 或 " + PromptCategoryInstruction + "，并添加 tag \"" + PromptTagProactive + "\"。",
		"每次会话最多主动保存 5 条。保存后在回复中简要提示：" + PromptProactiveAck + ": <摘要>",
	}, "\n")
}
