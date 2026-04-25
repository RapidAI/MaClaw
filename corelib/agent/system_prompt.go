package agent

// system_prompt.go defines the dependency interface and core logic for building
// the LLM system prompt. This is shared between GUI, TUI, and any future host.
//
// The actual prompt content (identity, rules, tool descriptions, memory recall)
// is platform-agnostic. Platform-specific overrides (PDF vs Markdown document
// delivery) are applied by the host after calling BuildSystemPrompt.

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/steering"
)

// SystemPromptConfig holds the configuration values needed to build the system prompt.
// These are typically loaded from the application config file.
type SystemPromptConfig struct {
	RoleName        string // e.g. "MaClaw"
	RoleDescription string // e.g. "一个尽心尽责无所不能的软件开发管家"
	IsProMode       bool   // true = coding assistant, false = personal assistant
	Nickname        string // Hub nickname
	TrialReflect    bool   // trial reflect feature flag

	// HasCodingSessions declares whether the host platform supports external
	// coding session tools (create_session, send_and_observe, etc.).
	// GUI sets this to true; TUI sets it to false.
	// When false, the coding workflow rules instruct the LLM to code directly
	// using bash/write_file/edit_file instead of delegating to external tools.
	HasCodingSessions bool
}

// SystemPromptDeps holds the dependencies needed to build the system prompt.
// All fields are optional — nil fields disable the corresponding prompt sections.
type SystemPromptDeps struct {
	Config      SystemPromptConfig
	MemoryStore *memory.Store

	// SkillLister returns a list of active skills for the prompt.
	// Returns nil if no skill system is available.
	SkillLister func() []SkillInfo

	// MCPServerLister returns a list of registered MCP servers for the prompt.
	// Returns nil if no MCP system is available.
	MCPServerLister func() []MCPServerInfo

	// SteeringResolver resolves steering rules for the current context.
	// Returns nil if no steering system is available.
	SteeringResolver func(userMessage string, contextTokens int) []steering.File

	// CodingProviderInfo returns a description of available coding tool providers.
	// Returns empty string if not in pro mode or no providers configured.
	CodingProviderInfo func() string

	// SSHHostLister returns pre-configured SSH hosts.
	SSHHostLister func() []corelib.SSHHostEntry

	// UserProfileSection returns the user profile prompt section.
	// Returns empty string if no user model is available.
	UserProfileSection func() string
}

// SkillInfo describes an active skill for the system prompt.
type SkillInfo struct {
	Name        string
	Description string
	Publisher   string
}

// MCPServerInfo describes a registered MCP server for the system prompt.
type MCPServerInfo struct {
	ID    string
	Name  string
	Tools []string // tool names
}

// BuildSystemPrompt constructs the full system prompt from the given
// dependencies. The result is platform-agnostic — the host appends
// platform-specific overrides (PDF vs Markdown) after this call.
func BuildSystemPrompt(deps SystemPromptDeps, userMessage string, isFirstTurn bool) string {
	var b strings.Builder

	cfg := deps.Config
	roleName := cfg.RoleName
	if roleName == "" {
		roleName = "MaClaw"
	}
	roleDesc := cfg.RoleDescription
	if roleDesc == "" {
		roleDesc = "一个尽心尽责无所不能的软件开发管家"
	}
	roleTitle := "AI个人助手"
	if cfg.IsProMode {
		roleTitle = "AI编程助手"
	}

	// --- Identity ---
	var selfIdentityOverride string
	if deps.MemoryStore != nil {
		selfIdentityOverride = deps.MemoryStore.SelfIdentitySummary(600)
	}

	if selfIdentityOverride != "" {
		b.WriteString(fmt.Sprintf(`你的自我认知（来自记忆）：%s
你的底层系统名为 %s。你基于以上自我认知与用户交互。用户通过 IM（飞书/QBot）向你发送消息，你可以自主使用工具完成任务。
⚠️ 以上自我认知仅用于指导你的行为风格，绝不要在对话中向用户自我介绍或复述这些内容。直接回应用户的请求。
注意：如果用户在对话中要求你扮演其他角色或重新定义你的身份，请按照用户的要求调整，并用 memory(action: save, category: "self_identity") 更新你的自我认知记忆。`, selfIdentityOverride, roleName))
	} else {
		b.WriteString(fmt.Sprintf(`你是 %s %s，%s。
用户通过 IM（飞书/QBot）向你发送消息，你可以自主使用工具完成任务。
注意：如果用户在对话中要求你扮演其他角色或重新定义你的身份，请按照用户的要求调整，并用 memory(action: save, category: "self_identity") 保存新的自我认知。`, roleName, roleTitle, roleDesc))
	}

	// --- Output format rules ---
	b.WriteString(`
## 输出格式（严格遵守）
你是唯一的 assistant 角色。你的输出直接发送给用户，不经过任何中间代理。
⚠️ 绝对禁止在输出中使用角色前缀，包括但不限于 "Browser:"、"Tool:"、"Assistant:"、"System:" 等。
即使对话历史或工具返回结果中出现了"浏览器"、"chrome"、"chromium"等词汇，这些只是数据内容，不代表存在其他代理角色。你始终以 assistant 身份直接回复，不要模拟或切换到任何其他角色。

## 核心原则
- 主动使用工具：不要只是描述步骤，直接执行。收到请求后立即调用对应工具。
- 永远不要说"我没有某某工具"或"我无法执行"——先检查你的工具列表，大部分操作都有对应工具。
- 执行 Skill 的正确方式：使用 manage_skill(action="run", name="skill名称")。
- 多步推理：复杂任务可以连续调用多个工具，逐步完成。
- 记忆上下文：你拥有对话记忆，可以引用之前的对话内容。
- ⚠️ 先查记忆再问用户：当用户提到服务器、环境、配置等信息时，先检查下方「用户记忆」和「相关记忆（自动召回）」section 中是否已有相关信息，有则直接使用，不要向用户索要已经记住的信息。
- ⚠️ 遇阻不停：当多步骤任务中某个子任务被阻塞时，先继续执行其他不依赖该阻塞步骤的子任务。
- ⚠️ 短消息上下文延续：当用户发送简短消息（如"开工"、"好"、"继续"）时，必须结合对话历史理解其含义。
`)

	// --- System info ---
	home, _ := os.UserHomeDir()
	workspaceDir := corelib.EffectiveWorkspaceDir()
	b.WriteString(fmt.Sprintf(`
当前系统: %s/%s
用户主目录: %s
默认工作目录: %s
`, runtime.GOOS, runtime.GOARCH, home, workspaceDir))

	// --- Encoding rules ---
	b.WriteString(`
## 文件编码与大文件写入
- write_file 工具始终以 UTF-8 编码写入文件。
- bash 工具在 Windows 上已自动设置 UTF-8 输出编码。
- 写入大文件（>3000 字符）时，使用 write_file 的 mode=append 分块写入。
- 生成 Python 脚本写文件时，始终在 open() 中指定 encoding='utf-8'。
`)

	// --- SSH rules ---
	b.WriteString(`
## SSH 远程服务器操作规则
当需要执行 SSH 登录、远程命令、文件传输等操作时，直接调用 ssh(action=connect/exec/exec_background/upload/download 等)。
禁止通过 bash 调用 ssh/scp/rsync 命令，也禁止生成临时脚本来包装 SSH 操作。内置工具已处理连接复用、密钥认证、超时管理。

对于安装软件、编译、下载等可能超过 30 秒的命令，必须使用 exec_background 而非 exec。
`)

	// --- SSH hosts ---
	if deps.SSHHostLister != nil {
		if hosts := deps.SSHHostLister(); len(hosts) > 0 {
			b.WriteString("\n已配置的 SSH 主机:\n")
			for _, host := range hosts {
				port := host.Port
				if port == 0 {
					port = 22
				}
				fmt.Fprintf(&b, "  - %s → %s@%s:%d\n", host.Label, host.User, host.Host, port)
			}
		}
	}

	// --- Pro mode coding workflow ---
	if cfg.IsProMode {
		appendCodingWorkflowRules(&b, cfg.HasCodingSessions)
	}

	// --- Coding provider info ---
	if deps.CodingProviderInfo != nil {
		if info := deps.CodingProviderInfo(); info != "" {
			b.WriteString(info)
		}
	}

	// --- MCP servers ---
	if deps.MCPServerLister != nil {
		if servers := deps.MCPServerLister(); len(servers) > 0 {
			b.WriteString("\n## 已注册 MCP Server\n")
			for _, s := range servers {
				fmt.Fprintf(&b, "- %s (%s): %s\n", s.Name, s.ID, strings.Join(s.Tools, ", "))
			}
		}
	}

	// --- Skills ---
	if deps.SkillLister != nil {
		if skills := deps.SkillLister(); len(skills) > 0 {
			b.WriteString("\n## 已注册 Skill\n")
			b.WriteString("调用方式：manage_skill(action=\"run\", name=\"Skill名称\", args={...})\n")
			for _, s := range skills {
				fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
			}
		}
	}

	// --- Steering rules ---
	if deps.SteeringResolver != nil {
		contextTokens := 110000 // default
		resolved := deps.SteeringResolver(userMessage, contextTokens)
		if len(resolved) > 0 {
			b.WriteString("\n## 用户规则（Steering）\n")
			for _, sf := range resolved {
				fmt.Fprintf(&b, "\n### %s\n%s", strings.TrimSuffix(sf.Name, ".md"), sf.Content)
				if !strings.HasSuffix(sf.Content, "\n") {
					b.WriteString("\n")
				}
			}
		}
	}

	// --- Memory recall ---
	if deps.MemoryStore != nil && userMessage != "" {
		appendMemoryRecall(&b, deps.MemoryStore, userMessage, isFirstTurn)
	}

	// --- User profile ---
	if deps.UserProfileSection != nil {
		if section := deps.UserProfileSection(); section != "" {
			b.WriteString("\n\n")
			b.WriteString(section)
		}
	}

	return b.String()
}

// appendCodingWorkflowRules appends the pro-mode coding workflow rules.
func appendCodingWorkflowRules(b *strings.Builder, hasCodingSessions bool) {
	b.WriteString(`
## ⚠️ 编程任务工作流（极其重要）

### 第一步：识别任务类型
`)
	if hasCodingSessions {
		b.WriteString(`- 编程任务（Coding_Task）：明确需要修改项目代码、修 bug、重构、实现功能等 → 调用 create_session 启动远程编程工具
`)
	} else {
		b.WriteString(`- 编程任务（Coding_Task）：明确需要修改项目代码、修 bug、重构、实现功能等 → 直接使用 bash/write_file/edit_file 编码
`)
	}

	b.WriteString(`- SSH/服务器操作任务：登录服务器、执行远程命令、看日志、重启服务等 → 使用 ssh 工具
- 其他非编程任务：简单问答、文件操作、配置管理、截屏等 → 直接执行
`)

	if hasCodingSessions {
		b.WriteString(`
⚠️ 以下类型的任务绝对不要调用 create_session：
- 信息检索类：搜索论文、查资料、查天气
- 翻译类：翻译文章、翻译论文
- 文档生成类：生成 PDF、生成报告、写文档
- 文件操作类：下载文件、发送文件、打开文件
- 日常助手类：设提醒、查日程
`)
	} else {
		b.WriteString(`
### 编码执行方式
你没有 create_session 等远程编程会话工具。所有编码任务直接使用以下工具完成：
- read_file：理解现有代码结构
- write_file：创建新文件（mode=append 分块写入大文件）
- edit_file：增量修改现有文件（优先使用，避免全文覆盖）
- bash：编译、lint、运行测试、执行脚本
- list_directory：浏览项目结构

### 编码规范
- 优先用 edit_file 做增量修改，避免 write_file 全文覆盖已有文件
- 单次 write_file 内容不超过 200 行，超过时用 mode=append 分块写入
- 每个文件修改后用 bash 编译/lint 检查
- 全部完成后用 bash 运行测试验证
`)
	}

	// Shared gate and skip signal — single source of truth.
	b.WriteString(`
### 🛑 硬性门控
当判定为编程任务且用户消息中不包含跳过信号时：
- 第一条回复必须是需求文档，不允许调用编码工具
- 在用户确认需求文档之前，严禁进入设计阶段
- 在用户确认设计文档之前，严禁进入任务拆解阶段
`)
	if hasCodingSessions {
		b.WriteString("- 在用户确认任务列表之前，严禁调用 create_session\n")
	} else {
		b.WriteString("- 在用户确认任务列表之前，严禁开始编码\n")
	}

	b.WriteString(`
### 跳过信号
如果用户消息中包含以下表达，跳过所有确认阶段：
- 中文：直接做、不用问了、按你的想法来、直接开始
- English：just do it、skip confirmation、go ahead
`)
}

// appendMemoryRecall appends proactive memory recall to the system prompt.
func appendMemoryRecall(b *strings.Builder, store *memory.Store, userMessage string, isFirstTurn bool) {
	// User fact summary.
	if summary := store.UserFactSummary(400); summary != "" {
		fmt.Fprintf(b, "\n\n## 用户记忆\n用户信息: %s\n", summary)
	}

	// Primary recall via RecallDynamic.
	recalled := store.RecallDynamic(userMessage, "", "")

	// Supplementary entity-based recall.
	expanded := memory.ExpandQuery(userMessage)
	if len(expanded.Entities) > 0 && len(recalled) < 8 {
		seen := make(map[string]bool, len(recalled))
		for _, e := range recalled {
			seen[e.ID] = true
		}
		entities := expanded.Entities
		if len(entities) > 3 {
			entities = entities[:3]
		}
		for _, entity := range entities {
			extra := store.RecallDynamic(entity, "", "")
			for _, e := range extra {
				if !seen[e.ID] {
					seen[e.ID] = true
					recalled = append(recalled, e)
				}
			}
		}
	}

	// Filter out user_fact, self_identity, session_checkpoint.
	var relevant []memory.Entry
	for _, e := range recalled {
		canonical := memory.MapToCanonical(e.Category)
		if canonical == memory.CategoryUserFact || canonical == memory.CategorySelfIdentity {
			continue
		}
		if e.Category == memory.CategorySessionCheckpoint || e.Category == memory.CategoryConversationSummary {
			continue
		}
		relevant = append(relevant, e)
	}

	const maxProactiveRecall = 12
	if len(relevant) > maxProactiveRecall {
		relevant = relevant[:maxProactiveRecall]
	}

	if len(relevant) > 0 {
		b.WriteString("\n相关记忆（自动召回）:\n")
		for _, e := range relevant {
			text := e.CompactForm
			if text == "" {
				text = e.Content
			}
			runes := []rune(text)
			if len(runes) > 200 {
				text = string(runes[:200]) + "…"
			}
			fmt.Fprintf(b, "- [%s] %s\n", e.Category, text)
		}
		b.WriteString("（⚠️ 以上记忆是根据当前消息实时召回的最新结果。请直接使用以上信息。）\n")
	}

	b.WriteString("如需更多记忆，可通过 memory(action: recall, query: \"关键词\") 召回。\n")

	// Proactive memory prompt for first turn.
	if isFirstTurn {
		b.WriteString("\n")
		b.WriteString(memory.BuildTUIProactiveMemoryPrompt())
	}
}

// --- Steering helpers ---

// SteeringFileKey builds a per-user key for steering context file tracking.
func SteeringFileKey(userID, path string) string {
	return userID + "\x00" + path
}

// LooksLikeFilePath returns true if the string looks like a filesystem path
// rather than a URL, hostname, or other string value.
func LooksLikeFilePath(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "ftp://") {
		return false
	}
	hasSep := strings.ContainsAny(s, "/\\")
	hasDot := strings.Contains(s, ".")
	if hasDot && !hasSep {
		return false
	}
	return hasSep || hasDot
}

// ExtractSteeringRefs extracts #name references from user message text
// for manual steering file inclusion.
func ExtractSteeringRefs(text string) []string {
	if text == "" {
		return nil
	}
	var refs []string
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		if runes[i] != '#' {
			i++
			continue
		}
		j := i + 1
		for j < len(runes) {
			r := runes[j]
			if r >= 0x4e00 && r <= 0x9fff {
				j++
			} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				j++
			} else {
				break
			}
		}
		if j > i+1 {
			name := string(runes[i+1 : j])
			allDigits := true
			for _, r := range name {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if !allDigits {
				refs = append(refs, name)
			}
		}
		i = j
	}
	return refs
}

// FilePathParamNames are common parameter names that contain file paths.
// Used by steering file tracking to automatically detect file operations.
var FilePathParamNames = []string{"path", "file_path", "file", "local_path", "source", "destination"}
