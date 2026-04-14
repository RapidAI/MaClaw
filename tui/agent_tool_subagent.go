package main

import (
	"fmt"
	"strings"
)

// subAgentSpec defines a specialized sub-agent for TUI.
type subAgentSpec struct {
	Name        string
	Description string
	Prompt      string
}

var tuiSubAgents = map[string]subAgentSpec{
	"coding_workflow": {
		Name:        "coding_workflow",
		Description: "编码工作流专家：引导完成需求分析→技术设计→任务拆分",
		Prompt:      "你是编码工作流专家。引导用户完成需求分析→技术设计→任务拆分。每个阶段在文档末尾用文字提示用户确认或修改。使用 task 工具创建任务列表。",
	},
	"help": {
		Name:        "help",
		Description: "MaClaw 使用帮助专家",
		Prompt:      "你是 MaClaw 使用帮助专家。回答关于功能、配置、工具使用的问题。使用 discover_tool 查找工具信息。",
	},
}

func (h *TUIAgentHandler) toolDelegateTask(args map[string]interface{}) string {
	agentName := stringArg(args, "agent")
	if agentName == "" {
		var b strings.Builder
		b.WriteString("可用的子 Agent:\n")
		for name, spec := range tuiSubAgents {
			b.WriteString(fmt.Sprintf("\n- %s: %s", name, spec.Description))
		}
		return b.String()
	}

	spec, ok := tuiSubAgents[agentName]
	if !ok {
		return fmt.Sprintf("未知子 Agent: %s", agentName)
	}

	request := stringArg(args, "request")
	if request == "" {
		return fmt.Sprintf("错误: 缺少 request 参数")
	}

	return fmt.Sprintf("__SUBAGENT_CONTEXT__\n[%s 子 Agent 已激活]\n专业领域: %s\n指导原则: %s\n用户请求: %s",
		spec.Name, spec.Description, spec.Prompt, request)
}
