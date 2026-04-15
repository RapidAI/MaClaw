package workflow

import (
	"fmt"
	"strings"
)

// BuildPhaseSystemPrompt builds the system prompt for a workflow phase.
// Includes: phase name/description, LLM instructions, StructuredIntent summary,
// previous phase outputs summary, Checklist items.
func BuildPhaseSystemPrompt(state *WorkflowState, phase *PhaseTemplate, registry *WorkflowRegistry) string {
	if state == nil || phase == nil {
		return ""
	}

	var b strings.Builder

	// 1. Current phase name and description
	fmt.Fprintf(&b, "## 当前阶段：%s\n\n", phase.Name)
	if phase.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", phase.Description)
	}

	// 2. Phase LLM instruction
	if phase.Prompt != "" {
		fmt.Fprintf(&b, "## 阶段指令\n\n%s\n\n", phase.Prompt)
	}

	// 2.5 Input document guidance (for document-driven workflows)
	// When the template requires an input document, inject contextual
	// guidance into the phase prompt. This is handled uniformly by the
	// engine rather than hardcoded in individual template Prompts.
	if registry != nil {
		tmpl := registry.Match(state.Type)
		if tmpl != nil && tmpl.NeedsInputDocument() {
			req := tmpl.RequiresInput
			if state.IsWaitingForInput(tmpl) {
				// Input not yet received — tell LLM to ask user for upload
				b.WriteString("## ⚠️ 需要用户提供输入文档\n\n")
				fmt.Fprintf(&b, "%s\n\n", req.Description)
				if len(req.FileTypes) > 0 {
					fmt.Fprintf(&b, "支持的文件格式：%s\n", strings.Join(req.FileTypes, "、"))
				}
				if req.AcceptText {
					b.WriteString("用户也可以直接粘贴文档内容文本，或提供网址由系统自动抓取。\n")
				}
				b.WriteString("\n请提示用户上传文档后再开始分析。在收到文档之前，不要生成本阶段的产出物。\n\n")
			} else if state.PhaseIndex == 0 && req.AnalysisHint != "" {
				// Input received, first phase — inject analysis guidance
				b.WriteString("## 输入文档分析指引\n\n")
				fmt.Fprintf(&b, "%s\n\n", req.AnalysisHint)
			}
		}
	}

	// 3. StructuredIntent summary
	b.WriteString("## 用户意图摘要\n\n")
	if state.Intent.Category != "" {
		fmt.Fprintf(&b, "- 类别：%s\n", string(state.Intent.Category))
	}
	if state.Intent.Summary != "" {
		fmt.Fprintf(&b, "- 摘要：%s\n", state.Intent.Summary)
	}
	if len(state.Intent.Goals) > 0 {
		b.WriteString("- 目标：\n")
		for _, g := range state.Intent.Goals {
			fmt.Fprintf(&b, "  - %s\n", g)
		}
	}
	if len(state.Intent.Constraints) > 0 {
		b.WriteString("- 约束：\n")
		for _, c := range state.Intent.Constraints {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
	}
	b.WriteString("\n")

	// 4. Previous phase outputs summary
	if state.PhaseIndex > 0 && registry != nil {
		tmpl := registry.Match(state.Type)
		if tmpl != nil && len(tmpl.Phases) > 0 {
			var prevOutputs []string
			for i := 0; i < state.PhaseIndex && i < len(tmpl.Phases); i++ {
				pid := tmpl.Phases[i].ID
				if output, ok := state.PhaseOutputs[pid]; ok && output != "" {
					prevOutputs = append(prevOutputs, fmt.Sprintf("### %s\n\n%s", tmpl.Phases[i].Name, output))
				}
			}
			if len(prevOutputs) > 0 {
				b.WriteString("## 前序阶段产出物\n\n")
				b.WriteString(strings.Join(prevOutputs, "\n\n"))
				b.WriteString("\n\n")
			}
		}
	}

	// 5. Checklist items
	if len(phase.Checklist) > 0 {
		b.WriteString("## 质量检查清单\n\n")
		for _, item := range phase.Checklist {
			fmt.Fprintf(&b, "- [ ] %s\n", item)
		}
		b.WriteString("\n")
	}

	// 6. NeedsConfirm behavior constraint
	if phase.NeedsConfirm {
		b.WriteString("## ⚠️ 重要：等待用户确认\n\n")
		b.WriteString("本阶段需要用户确认后才能进入下一阶段。请严格遵守以下规则：\n")
		b.WriteString("1. 输出本阶段的产出物后，**立即停止**，不要继续生成下一阶段的内容\n")
		b.WriteString("2. 在产出物末尾明确提示用户：\"请确认以上内容，或提出修改意见。确认后我将进入下一阶段。\"\n")
		b.WriteString("3. **绝对不要**在同一次回复中既输出产出物又开始下一阶段的工作\n")
		b.WriteString("4. 如果用户的需求信息不足，先追问澄清，不要假设默认值直接生成完整文档\n\n")
	}

	return b.String()
}

// BuildQualityGatePrompt builds the prompt for quality gate checking.
// Asks the LLM to check the output against the phase's checklist items
// and return a structured assessment.
func BuildQualityGatePrompt(phase *PhaseTemplate, output string) string {
	if phase == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString("请对以下阶段产出物进行质量门禁检查。\n\n")
	fmt.Fprintf(&b, "阶段：%s\n\n", phase.Name)
	b.WriteString("产出物内容：\n\n")
	b.WriteString(output)
	b.WriteString("\n\n")

	b.WriteString("请逐项检查以下清单，对每一项给出通过/未通过的判断和简要说明：\n\n")
	for i, item := range phase.Checklist {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}

	b.WriteString("\n请以 JSON 数组格式返回检查结果，每项包含 description、passed（bool）、note 字段。示例：\n")
	b.WriteString(`[{"description":"检查项描述","passed":true,"note":"通过说明"}]`)
	b.WriteString("\n")

	return b.String()
}

// GetToolFilterForPhase returns the tool filtering policy for a phase.
func GetToolFilterForPhase(phase *PhaseTemplate) ToolFilterPolicy {
	if phase == nil {
		return ToolFilterNone
	}
	return phase.ToolPolicy
}
