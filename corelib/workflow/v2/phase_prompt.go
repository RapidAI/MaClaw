package v2

import (
	"fmt"
	"strings"
)

// BuildPhasePrompt constructs the system prompt injection for the current phase.
func BuildPhasePrompt(state *WorkflowState) string {
	if state == nil {
		return ""
	}
	phase := state.ActivePhase()
	if phase == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## 当前任务\n\n你正在执行「%s」工作流的「%s」阶段。\n\n", state.Type, phase.Name))
	sb.WriteString(fmt.Sprintf("用户需求：%s\n\n", state.Summary))

	if state.ProjectPath != "" {
		sb.WriteString(fmt.Sprintf("项目路径：%s\n\n", state.ProjectPath))
	}

	// Previous phase outputs (truncated)
	prevOutputs := state.PreviousOutputs(500)
	if len(prevOutputs) > 0 {
		sb.WriteString("## 前序阶段产出物（摘要）\n\n")
		for i := 0; i < state.CurrentPhase && i < len(state.Phases); i++ {
			p := state.Phases[i]
			if output, ok := prevOutputs[p.ID]; ok {
				sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", p.Name, output))
			}
		}
	}

	// Phase-specific instructions
	sb.WriteString(phaseInstruction(phase.ID))

	return sb.String()
}

func phaseInstruction(phaseID string) string {
	switch phaseID {
	case "requirements":
		return `## 阶段指令

生成需求文档（Markdown 格式），包含：
- 功能需求
- 非功能需求  
- 边界情况
- 验收标准

信息不足的部分标记为「⚠️ 待确认」。直接生成文档，不要先问澄清问题。

## 重要约束（违反将导致错误）
- 只生成一份需求文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】在文档后自己说"好的"或模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
	case "design":
		return `## 阶段指令

基于已确认的需求，生成技术设计文档（Markdown 格式），包含：
- 架构设计
- 技术选型
- 模块划分
- 接口设计
- 数据结构

## 重要约束（违反将导致错误）
- 只生成一份技术设计文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
	case "tasks":
		return `## 阶段指令

基于已确认的设计，生成任务拆分文档。使用以下格式（不要使用表格）：

### T1: 任务标题
- **描述**：具体要做什么
- **涉及文件**：file1.cpp, file2.h
- **依赖**：无 / 依赖 T0
- **优先级**：P0/P1/P2
- **工作量**：预估说明

每个任务必须包含描述、涉及文件、依赖、优先级、工作量五个字段。

## 重要约束（违反将导致错误）
- 只生成一份任务拆分文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】在同一次回复中开始写代码或执行任何任务。
- 【严禁】自己模拟用户确认。
- 你只负责任务拆分这一步，后续编码由系统自动调度。
`
	case "implementation":
		return "" // TaskRunner/SubAgent handles this

	case "verification":
		return `## 阶段指令

编码执行已完成。现在进行最终验收：

1. 编译整个项目（确认编译通过无错误）
2. 运行程序（确认启动不崩溃）
3. 检查需求文档中的验收标准是否满足
4. 生成验收报告，列出：
   - 编译结果（通过/失败+错误信息）
   - 运行结果（正常启动/崩溃+错误信息）
   - 各验收标准的通过情况
   - 如有问题，给出修复建议

## 重要约束
- 必须实际执行编译和运行命令，不要只描述。
- 如果编译失败，尝试修复后重新编译（最多3次）。
- 生成完验收报告后停止输出。
`

	case "audience_goal":
		return `## 阶段指令

分析 PPT 的目标受众和演讲目标：
- 受众是谁？（职位、知识水平、关注点）
- 演讲目标是什么？（说服、教育、汇报）
- 核心信息是什么？（一句话总结）
- 期望的行动是什么？

输出完毕后等待用户确认。
`
	case "outline":
		return `## 阶段指令

基于受众和目标，设计 PPT 内容大纲：
- 每页的标题和核心要点
- 逻辑流（开场→问题→方案→证据→结论→行动）
- 预估总页数

输出完毕后等待用户确认。
`
	case "slide_scripting":
		return `## 阶段指令

基于大纲，为每一页编写详细脚本：
- 页面标题
- 要展示的文字内容
- 视觉建议（图表/图片/动画）
- 演讲备注

输出完毕后等待用户确认。
`
	case "ppt_generation":
		return "" // Tool execution phase

	case "problem_discovery":
		return `## 阶段指令

分析产品要解决的核心问题：
- 目标用户是谁？
- 他们面临什么痛点？
- 现有解决方案的不足？
- 机会在哪里？

输出完毕后等待用户确认。
`
	case "user_research":
		return `## 阶段指令

基于问题发现，深入用户研究：
- 用户画像（demographics, behaviors, goals）
- 使用场景
- 关键需求优先级
- 竞品分析

输出完毕后等待用户确认。
`
	case "solution_design":
		return `## 阶段指令

基于用户研究，设计产品方案：
- 核心功能列表
- 信息架构
- 用户旅程
- MVP 定义

输出完毕后等待用户确认。
`
	case "prototype":
		return `## 阶段指令

基于方案，描述原型设计：
- 关键页面线框图描述
- 交互流程
- 视觉风格建议

输出完毕后等待用户确认。
`
	default:
		// Generic instruction for doc-only phases without specific prompts.
		return `## 阶段指令

请基于前序阶段的产出物和用户需求，生成本阶段的完整文档内容（Markdown 格式）。
内容要详实、结构清晰、有可操作性。

## 重要约束（违反将导致错误）
- 只生成一份文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
	}
}
