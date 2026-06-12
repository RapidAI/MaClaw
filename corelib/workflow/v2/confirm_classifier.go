package v2

import (
	"encoding/json"
	"strings"
)

// ConfirmClassifierSystemPrompt is the system prompt for the LLM-based confirm intent classifier.
const ConfirmClassifierSystemPrompt = `你是一个意图分类器。用户正在一个工作流中，当前阶段已经产出了文档，等待用户确认。

你需要判断用户的回复属于以下哪种意图：

1. "confirm" — 用户认可当前文档，同意推进到下一阶段。
   示例："确认"、"OK"、"好的"、"没问题"、"可以继续了"、"通过"、"LGTM"

2. "modify" — 用户对当前文档有修改意见，需要重新生成。
   示例："加一个登录功能"、"把技术栈换成 React"、"这里需要改一下..."、"继续完善，加入支付模块"

3. "cancel" — 用户不想继续这个工作流了，放弃整个任务。
   示例："取消"、"不做了"、"算了"、"放弃"

4. "cancel_execute" — 用户不想走工作流的多阶段流程，但仍然想要完成原始任务，希望跳过流程直接执行。
   示例："取消，直接处理"、"取消工作流，直接做"、"不要这个流程了，直接帮我搞"、"跳过这些步骤直接执行"、"别确认了直接干"

5. "unrelated" — 用户的消息与当前工作流无关。
   示例："今天天气怎么样"、"帮我查个东西"、"你好"

注意：
- "继续" 单独出现 = confirm（用户同意继续）
- "继续，加一个XX功能" = modify（用户有补充）
- "可以，但是需要改一下XX" = modify（有修改意见）
- "取消" 单独出现 = cancel（放弃任务）
- "取消，直接处理" / "取消，直接做" = cancel_execute（不要流程但要完成任务）
- 如果你不确定，返回 "modify"（保守策略：宁可让用户重新表述，不要误跳过）

只返回一个 JSON 对象：{"intent": "confirm|modify|cancel|cancel_execute|unrelated"}
`

// BuildConfirmClassifierUserPrompt builds the user prompt for classification.
func BuildConfirmClassifierUserPrompt(phaseContext, userText string) string {
	return "当前工作流上下文：" + phaseContext + "\n\n用户回复：" + userText
}

// ParseConfirmClassifierResponse extracts the intent from LLM response.
func ParseConfirmClassifierResponse(response string) string {
	response = strings.TrimSpace(response)
	// Try JSON parse
	var result struct {
		Intent string `json:"intent"`
	}
	// Strip markdown code fence if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		response = strings.Join(jsonLines, "\n")
	}
	if err := json.Unmarshal([]byte(response), &result); err == nil {
		switch result.Intent {
		case "confirm", "modify", "cancel", "cancel_execute", "unrelated":
			return result.Intent
		}
	}
	// Fallback: look for the intent word directly in response
	lower := strings.ToLower(response)
	// Check cancel_execute first (it contains "cancel" as substring)
	if strings.Contains(lower, "cancel_execute") {
		return "cancel_execute"
	}
	for _, intent := range []string{"confirm", "modify", "cancel", "unrelated"} {
		if strings.Contains(lower, intent) {
			return intent
		}
	}
	return "" // empty = use keyword fallback
}
