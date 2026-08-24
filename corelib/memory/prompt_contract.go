package memory

import "strings"

const (
	PromptSectionUserMemory        = "## 用户记忆"
	PromptSectionMemoryGuide       = "## 记忆管理指引"
	PromptSectionProactiveMemory   = "## 主动记忆"
	PromptActionRecallColon        = "memory(action: recall"
	PromptActionSaveColon          = "memory(action: save)"
	PromptActionSaveEquals         = "memory(action=save)"
	PromptCategoryProjectKnowledge = "project_knowledge"
	PromptCategoryInstruction      = "instruction"
	PromptTagProactive             = "proactive"
	PromptProactiveAck             = "已主动记录"
	PromptSaveCategorySummary      = "user_fact | 偏好 → preference | 项目知识 → " + PromptCategoryProjectKnowledge + " | 指令 → " + PromptCategoryInstruction
)

// CatalogOnlyWorkingSetFooter is the IM/Core instruction when the prompt
// carries a memory catalog but no recalled warehouse text.
func CatalogOnlyWorkingSetFooter() string {
	return "（当前任务工作集为空。上方索引只是目录指针，不是已确认事实。仅当本轮工具列表里有记忆检索或知识库检索时才调用它们拉取正文；没有这些工具时不要把目录当作答案，也不要先搜知识库。）"
}

func BuildIMMemoryGuidePrompt() string {
	return strings.Join([]string{
		PromptSectionMemoryGuide,
		"需要已保存的经验或资料时，仅当本轮工具列表里有记忆检索或知识库检索时才调用它们；没有这些工具时不要先搜知识库，也不要把记忆索引或训练数据当作已确认事实。",
		"识别到有价值的信息时，主动调用 " + PromptActionSaveColon + " 保存：",
		"- " + PromptSaveCategorySummary,
		"- tags: 提供 3-5 个具体实体名（主机名、工具名、项目名），不要用泛词",
		"- 同一主题的信息合并为一条保存，不要拆分为多条",
		"",
		"更新已有记忆时，使用 memory(action: replace, old_text: \"已有内容的唯一子串\", content: \"新内容\")。",
		"删除过时记忆时，使用 memory(action: delete, old_text: \"已有内容的唯一子串\")。",
		"无需记住完整内容或 ID，只需提供能唯一定位的片段。",
	}, "\n")
}

func BuildTUIProactiveMemoryPrompt() string {
	return strings.Join([]string{
		PromptSectionProactiveMemory,
		"当你在会话中发现以下类型的非显而易见信息时，应主动使用 " + PromptActionSaveEquals + " 保存：",
		"- 调试过程中发现的 workaround 或未文档化行为",
		"- 配置细节、环境特殊性（将同一服务器/工具的所有信息合并为一条）",
		"- 用户项目的架构决策或约定",
		"- 重要的错误原因和解决方案",
		"",
		"保存规则：",
		"- category: " + PromptCategoryProjectKnowledge + " 或 " + PromptCategoryInstruction,
		"- tags: 必须提供 3-5 个具体实体名（主机名、工具名、项目名等专有名词），用于后续搜索召回。不要用泛词如\"服务器\"\"配置\"",
		"- 同一主题的信息合并为一条（如同一服务器的 hostname+port+services = 一条），不要拆分",
		"- 每次会话最多主动保存 5 条",
		"",
		"保存后在回复中简要提示：" + PromptProactiveAck + ": <摘要>",
	}, "\n")
}
