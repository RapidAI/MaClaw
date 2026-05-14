package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// SubAgentSpec defines a specialized sub-agent with its own system prompt.
type SubAgentSpec struct {
	Name        string // unique identifier
	Description string // what this sub-agent does
	Prompt      string // system prompt for the sub-agent
}

// builtinSubAgents defines the available sub-agent specializations.
// The main agent can delegate tasks to these via the delegate_task tool.
// NOTE: The AgentRegistry (corelib/agent/definition.go) is checked first;
// these are kept as fallback for when the registry is not initialized.
var builtinSubAgents = map[string]SubAgentSpec{
	"coding_workflow": {
		Name:        "coding_workflow",
		Description: "编码工作流专家：引导完成需求分析→技术设计→任务拆分的完整流程",
		Prompt: `你是编码工作流专家。你的职责是引导用户完成编程任务的前期规划。

严格按以下三个阶段执行：

## 阶段一：需求文档
1. 不要先问澄清问题——直接基于用户已提供的信息生成需求文档，不确定的部分标记为「⚠️ 待确认」
2. 用简洁的语言复述你对需求的理解
3. 生成需求文档（Markdown 格式），包含：功能需求、非功能需求、边界情况、验收标准
4. 在文档末尾用文字提示用户确认或提出修改意见（如"请查看并确认需求是否准确，或提出修改意见。"）

## 阶段二：技术设计文档
1. 基于确认的需求，生成技术设计文档，包含：架构设计、技术选型、模块划分、接口设计
2. 在文档末尾用文字提示用户确认或提出修改意见

## 阶段三：任务拆分
1. 基于确认的设计，生成任务列表，包含：任务描述、优先级、依赖关系
2. 使用 task 工具创建所有任务（含依赖关系）
3. 在文档末尾用文字提示用户确认或提出修改意见

每个阶段必须等待用户确认后才能进入下一阶段。`,
	},
	"help": {
		Name:        "help",
		Description: "使用帮助专家：回答关于 MaClaw 自身功能、配置、工具使用的问题",
		Prompt: `你是 MaClaw 使用帮助专家。回答用户关于 MaClaw 的功能、配置、工具使用等问题。

你可以使用 discover_tool 工具查找相关工具的详细信息。
回答要简洁实用，给出具体的操作步骤。
如果问题超出 MaClaw 范围，礼貌地说明并建议其他途径。`,
	},
}

// toolDelegateTask handles the delegate_task tool call.
// It checks the AgentRegistry first (user-defined YAML agents), then falls
// back to builtinSubAgents. This allows users to create custom agents in
// ~/.maclaw/agents/*.yaml without modifying code.
func (h *IMMessageHandler) toolDelegateTask(args map[string]interface{}) string {
	agentName, _ := args["agent"].(string)
	if agentName == "" {
		return h.listSubAgents()
	}

	userRequest, _ := args["request"].(string)
	if userRequest == "" {
		return fmt.Sprintf("错误: 缺少 request 参数。请描述要委派给 %s 的任务。", agentName)
	}

	// Priority 1: Check AgentRegistry (user-defined + project-defined YAML agents)
	registry := h.getAgentRegistry()
	if registry != nil {
		if def := registry.Get(agentName); def != nil {
			return fmt.Sprintf("__SUBAGENT_CONTEXT__\n[%s 子 Agent 已激活]\n\n专业领域: %s\n\n指导原则:\n%s\n\n用户请求: %s\n\n请按照上述指导原则处理用户请求。",
				def.Name, def.Description, def.SystemPrompt, userRequest)
		}
	}

	// Priority 2: Fallback to hardcoded builtinSubAgents
	spec, ok := builtinSubAgents[agentName]
	if !ok {
		return fmt.Sprintf("未知子 Agent: %s\n\n%s", agentName, h.listSubAgents())
	}

	return fmt.Sprintf("__SUBAGENT_CONTEXT__\n[%s 子 Agent 已激活]\n\n专业领域: %s\n\n指导原则:\n%s\n\n用户请求: %s\n\n请按照上述指导原则处理用户请求。",
		spec.Name, spec.Description, spec.Prompt, userRequest)
}

// getAgentRegistry returns the lazily-initialized agent registry.
// Scans ~/.maclaw/agents/ and <project>/.maclaw/agents/ for YAML definitions.
func (h *IMMessageHandler) getAgentRegistry() *agent.AgentRegistry {
	if h.app == nil {
		return nil
	}
	h.app.agentRegistryOnce.Do(func() {
		userDir := filepath.Join(corelib.MaclawBaseDir(), "agents")
		dirs := []string{userDir}
		if wd := corelib.EffectiveWorkspaceDir(); wd != "" {
			projectDir := filepath.Join(wd, ".maclaw", "agents")
			if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
				dirs = append(dirs, projectDir)
			}
		}
		h.app.agentRegistry = agent.NewAgentRegistry(dirs...)
		_ = h.app.agentRegistry.Load()
	})
	return h.app.agentRegistry
}

func (h *IMMessageHandler) listSubAgents() string {
	var b strings.Builder
	b.WriteString("可用的子 Agent:\n")

	// List from registry first
	if registry := h.getAgentRegistry(); registry != nil {
		for _, def := range registry.List() {
			b.WriteString(fmt.Sprintf("\n- **%s**: %s", def.Name, def.Description))
		}
	} else {
		// Fallback to builtins
		for name, spec := range builtinSubAgents {
			b.WriteString(fmt.Sprintf("\n- **%s**: %s", name, spec.Description))
		}
	}
	b.WriteString("\n\n使用方式: delegate_task(agent=\"coding_workflow\", request=\"用户的需求描述\")")
	return b.String()
}

// IsSubAgentContext checks if a tool result contains sub-agent context injection.
func IsSubAgentContext(result string) bool {
	return strings.HasPrefix(result, "__SUBAGENT_CONTEXT__")
}

// ExtractSubAgentContext extracts the context from a sub-agent result.
func ExtractSubAgentContext(result string) string {
	return strings.TrimPrefix(result, "__SUBAGENT_CONTEXT__\n")
}
