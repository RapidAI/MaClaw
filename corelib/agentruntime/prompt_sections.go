package agentruntime

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// LightPromptRequest contains only transport-neutral inputs needed for the
// bounded/light Agent prompt. GUI and headless hosts may add their own final
// safety fence, but the identity, low-complexity rules and execution metadata
// are rendered once here.
type LightPromptRequest struct {
	RoleName            string
	RoleDescription     string
	UserText            string
	ExecutionLayer      string
	ExecutionTask       string
	ExecutionConfidence float64
	ExecutionReason     string
	CurrentTime         time.Time
	ClientCapabilities  *agent.ClientCapabilities
	ClientTools         []agent.ClientToolDefinition
	ClientID            string
	AssistantBinding    *agent.AssistantBinding
}

// BuildLightPrompt builds the shared low-complexity prompt used by every host.
// It deliberately does not inspect host/UI state; host-only restrictions can
// be appended by a PromptContributor or adapter after this stable section.
func BuildLightPrompt(request LightPromptRequest) string {
	roleName := strings.TrimSpace(request.RoleName)
	if roleName == "" {
		roleName = "MaClaw"
	}
	roleDescription := strings.TrimSpace(request.RoleDescription)
	if roleDescription == "" {
		roleDescription = "a careful personal assistant for low-complexity lookup tasks"
	}
	prompt := agent.BuildSystemPrompt(agent.SystemPromptDeps{
		Config: agent.SystemPromptConfig{
			RoleName:        roleName,
			RoleDescription: roleDescription,
			PromptProfile:   agent.PromptProfileLight,
		},
	}, request.UserText, true)

	now := request.CurrentTime
	if now.IsZero() {
		now = time.Now()
	}
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n")
	b.WriteString("Use the smallest sufficient action. Prefer one listed lookup tool when live data is needed, then answer immediately.\n")
	b.WriteString("Do not inspect local files, run shell commands, manage projects, start group discussions, change memory, create tasks, or generate files.\n")
	b.WriteString("Do not ask the user to re-authorize tools. If a listed lookup already returned evidence, answer from that evidence.\n")
	b.WriteString("If the request needs code, files, project context, or multi-step planning, say those tools are not in this turn instead of improvising.\n")
	b.WriteString(fmt.Sprintf("Current local time: %s\n", now.Format("2006-01-02 15:04:05 -0700")))
	b.WriteString(fmt.Sprintf("Execution profile: layer=%s task=%s confidence=%.2f reason=%s\n", strings.TrimSpace(request.ExecutionLayer), strings.TrimSpace(request.ExecutionTask), request.ExecutionConfidence, strings.TrimSpace(request.ExecutionReason)))
	if clientContext := agent.BuildClientCapabilityPrompt(request.ClientCapabilities); clientContext != "" {
		b.WriteString(clientContext)
		b.WriteByte('\n')
	}
	if len(request.ClientTools) > 0 {
		names := make([]string, 0, len(request.ClientTools))
		for _, definition := range request.ClientTools {
			names = append(names, definition.Name)
		}
		if clientToolContext := BuildClientToolPrompt(ClientToolPromptRequest{ToolNames: names, ClientID: request.ClientID}); clientToolContext != "" {
			b.WriteString(clientToolContext)
			b.WriteByte('\n')
		}
	}
	if bindingPrompt := BuildAssistantBindingPrompt(request.AssistantBinding); bindingPrompt != "" {
		b.WriteString(bindingPrompt)
		b.WriteByte('\n')
	}
	return b.String()
}

// DesktopWorkflowDocumentDeliveryPrompt and IMWorkflowDocumentDeliveryPrompt
// are the shared, host-neutral workflow document delivery contracts. The host
// chooses one based on its transport profile; the Agent rules themselves do
// not live in a GUI-only package.
func DesktopWorkflowDocumentDeliveryPrompt() string {
	return `

### 文档交付方式覆盖（桌面 AI 助手面板 · 仅工作流阶段文档）
你当前运行在桌面 AI 助手面板中（非 IM 通道）。以下规则**仅适用于**编程/产品等工作流阶段产出（需求文档、技术设计、任务列表等），**不覆盖**会议录音后处理：

**工作流阶段文档（requirements / design / tasks 等）：**
1. **不要使用 office(action="generate_pdf") 或 generate_pdf 工具**——桌面面板阶段文档不需要 PDF，直接输出 Markdown 文本即可
2. **不要使用 send_file 发送上述工作流阶段文档**——文档内容直接作为你的回复文本输出
3. 需求文档、技术设计文档、任务列表文档：直接用 Markdown 格式写在回复中
4. 系统会自动将你输出的 Markdown 文档显示在聊天区右侧的预览面板中
5. 输出文档后，仍然需要附带确认提示（如"请查看并确认需求是否准确，或提出修改意见"）
6. 其他规则不变：仍需等待用户确认后才能进入下一阶段

**例外（必须遵守，优先级高于上列 1–2）：**
- 会议/长时录音后处理（转写并生成会议纪要、仅转写文字、音频存档等）：必须按任务指令使用 write_file 落盘 .md、generate_pdf 生成 PDF、send_file 投递 md/pdf/mp3（及 transcript 相关文件）。不要因为上列工作流规则而省略落盘或投递。
`
}

func IMWorkflowDocumentDeliveryPrompt() string {
	return `

### IM 通道文档交付规则（所有工作流通用）
你当前运行在 IM 通道中（飞书/微信/QQ/Telegram）。所有工作流（编码、PPT 设计、产品设计、商业计划等）的每个阶段产出文档，必须遵守以下规则：

1. **必须**使用 generate_pdf 工具将本阶段产出物生成 PDF 后发送给用户
2. **严禁**在 IM 聊天窗口中直接输出大段文档文本——IM 中长文本阅读体验极差，用户无法有效审阅
3. 发送 PDF 后必须附带提示："已生成 [阶段名称] 的 PDF 版本，请查看并确认，或提出修改意见。"
4. 短回复（确认提示、澄清问题、进度说明等）可以直接文本输出，不需要 PDF
5. 其他规则不变：仍需等待用户确认后才能进入下一阶段
`
}

const lightSemanticGrantPromptFence = "\nGoverned tools: the live tool list is the ground truth. Call a listed name only, one tool per response; every result may change the next list. If a needed capability is unlisted, query tools_search for its exact name instead of inventing a tool. Do not fetch a search-engine page.\n"

// EnsureLightSemanticGrantPromptFence appends the compact semantic-routing
// contract used by light turns. Full governed turns retain their richer
// petition/replan guidance; light turns need only the bounded list-state rule
// so the prompt stays below the low-latency token budget on every host.
func EnsureLightSemanticGrantPromptFence(prompt string) string {
	if strings.Contains(prompt, "Governed tools:") {
		return prompt
	}
	return prompt + lightSemanticGrantPromptFence
}

// SemanticGrantPromptFence is the full governed-turn contract. Light turns
// use EnsureLightSemanticGrantPromptFence; every host's full managed surface
// must append this same fence so petition/replan guidance cannot drift.
const SemanticGrantPromptFence = "\nGoverned tools: the live tool list is the ground truth — call any listed name, and call it again while it stays listed. After a successful call a name may briefly leave the list and reappear for a later step in this same turn; lookup tools (search, fetch) are expected to be used several times. Call ONE tool per response: the list is a state machine, not a static catalog — every result changes what is listed next, so a batched second call races the first call's outcome and is rejected. Unlisted capability? Query tools_search for the exact name; a name it marks 可请愿 (petitionable) — for example an unlisted web_search, web_fetch, bash, or office — may be called once even though unlisted: the host may authorize it on the spot. A name it marks planned or unavailable stays invalid. Later steps such as PDF or image render unlock after the current step succeeds, in this same reply; when a render or delivery tool is listed, call it immediately and do not say please wait; do not tell the user a tool is missing. An untried petitionable name is available: call it instead of explaining the tool list. Do not narrate tool limits or availability mechanics to the user; just do the work or state plainly what could not be done. Do not invent previous_turn_tool or reuse leftover invoke_* names. Do not fetch a search-engine page.\n"

// EnsureSemanticGrantPromptFence appends the full governed-turn fence when
// the prompt does not already contain one (light or full).
func EnsureSemanticGrantPromptFence(prompt string) string {
	if strings.Contains(prompt, "Governed tools:") {
		return prompt
	}
	return prompt + SemanticGrantPromptFence
}

// BuildAssistantBindingPrompt renders the transport-neutral portion of an
// assistant/bot binding. The binding is trusted metadata supplied by the host;
// keeping its projection in agentruntime prevents GUI and headless transports
// from diverging in how working/document scopes are communicated to the model.
//
// Host-specific prompt sections (Wails controls, IM delivery rules, device
// state) must remain PromptContributor modules and must not be added here.
func BuildAssistantBindingPrompt(binding *agent.AssistantBinding) string {
	if binding == nil || strings.TrimSpace(binding.BotProfileID) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("[机器人绑定运行上下文]\n")
	b.WriteString("- bot_profile_id: ")
	b.WriteString(strings.TrimSpace(binding.BotProfileID))
	b.WriteByte('\n')
	if mode := strings.TrimSpace(binding.Mode); mode != "" {
		b.WriteString("- 助手模式: ")
		b.WriteString(mode)
		b.WriteByte('\n')
	}
	if dir := strings.TrimSpace(binding.WorkingDirectory); dir != "" {
		b.WriteString("- 工作目录: ")
		b.WriteString(dir)
		b.WriteByte('\n')
	}
	if len(binding.DocumentDirectories) > 0 {
		b.WriteString("- 文档检索目录: ")
		b.WriteString(strings.Join(binding.DocumentDirectories, ", "))
		b.WriteByte('\n')
	}
	if prompt := strings.TrimSpace(binding.InitialPrompt); prompt != "" {
		b.WriteString("- 管理员补充要求: ")
		b.WriteString(prompt)
		b.WriteByte('\n')
	}
	b.WriteString("仅在已授权的工作目录、文档目录和知识库范围内检索或读取；不得声称读取了未实际检索到的资料。")
	return b.String()
}
