package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) buildSystemPrompt() string {
	return h.buildSystemPromptBase(false)
}

func (h *IMMessageHandler) buildSystemPromptBase(includeMemoryGuide bool, userMessage ...string) string {
	var b strings.Builder

	// Use configurable role name and description from settings.
	// Priority: memory self_identity > config > hardcoded defaults.
	// Load config once and reuse for roleName, roleDesc, roleTitle, isProMode, and nickname.
	roleName := "MaClaw"
	roleDesc := "一个尽心尽责无所不能的软件开发管家"
	roleTitle := "AI个人助手"
	isProMode := false
	currentNickname := ""
	trialReflectEnabled := false
	if cfg, err := h.app.LoadConfig(); err == nil {
		if cfg.MaclawRoleName != "" {
			roleName = cfg.MaclawRoleName
		}
		if cfg.MaclawRoleDescription != "" {
			roleDesc = cfg.MaclawRoleDescription
		}
		isProMode = cfg.UIMode == "pro"
		if isProMode {
			roleTitle = "AI编程助手"
		}
		currentNickname = strings.TrimSpace(cfg.RemoteNickname)
		trialReflectEnabled = isProMode && cfg.TrialReflectEnabled
	}

	// Override identity from memory self_identity if present.
	var selfIdentityOverride string
	if h.memoryStore != nil {
		selfIdentityOverride = h.memoryStore.SelfIdentitySummary(600)
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

	// Core principles — always included, but session-related hints only in pro mode.
	b.WriteString(`
## 核心原则
- 主动使用工具：不要只是描述步骤，直接执行。收到请求后立即调用对应工具。
- 永远不要说"我没有某某工具"或"我无法执行"——先检查你的工具列表，大部分操作都有对应工具。
- 多步推理：复杂任务可以连续调用多个工具，逐步完成。
- 记忆上下文：你拥有对话记忆，可以引用之前的对话内容。
- ⚠️ 先查记忆再问用户：当用户提到服务器、环境、配置等信息时，先检查下方「用户记忆」和「相关记忆（自动召回）」section 中是否已有相关信息，有则直接使用，不要向用户索要已经记住的信息。
`)

	if isProMode {
		// Pro mode: full coding workflow with session management.
		b.WriteString(`- 智能推断参数：如果用户没有指定 session_id 等参数，查看当前会话列表自动选择。

## ⚠️ 编程任务工作流（极其重要）

### 第一步：识别任务类型
- 编程任务（Coding_Task）：明确需要修改项目代码、修 bug、重构、实现功能等 → 调用 create_session 启动远程编程工具
- SSH/服务器操作任务：登录服务器、执行远程命令、看日志、重启服务、上传/下载服务器文件等 → 使用 ssh 工具
- 其他非编程任务：简单问答、文件操作（bash/read_file/write_file/edit_file）、配置管理、截屏等 → 直接执行，不需要确认

⚠️ 以下规则必须同时遵守：
- 如果不能确定是编程任务，不要调用 create_session
- 如果不能确定是否需要 SSH，也不要自动建立 SSH 会话，先澄清是“改代码”还是“登录服务器处理”
- 用户提到服务器、SSH、远程主机、线上机器时，优先考虑 ssh，不要默认打开编程工具

⚠️ 以下类型的任务绝对不要调用 create_session，必须用现有工具直接完成：
- 信息检索类：搜索论文、查资料、查天气、查新闻、查快递
- 翻译类：翻译文章、翻译论文、全文翻译
- 文档生成类：生成 PDF、生成报告、写文档、做总结
- 文件操作类：下载文件、发送文件、打开文件
- 通信类：发邮件、发消息
- 日常助手类：设提醒、查日程、播放音乐

这些任务应该优先用 read_file/write_file/edit_file（读写/编辑文件）、bash（执行命令）、craft_tool（生成脚本）、send_file（发送文件）、open（打开文件/网址）等工具直接完成。
只有真正需要启动 IDE/编程工具来修改项目代码的任务才是编程任务。

### 🛑🛑🛑 硬性门控（HARD GATE — 违反此规则等于系统故障）🛑🛑🛑
当判定为编程任务（Coding_Task）且用户消息中不包含跳过信号时：
- 你的第一条回复必须是需求文档，不允许调用 create_session、bash、write_file、craft_tool 或任何编码工具
- 你的第一条回复中不允许出现任何代码片段、CMakeLists.txt、源文件内容
- 在用户确认需求文档之前，严禁进入技术设计阶段
- 在用户确认技术设计之前，严禁进入任务分解阶段
- 在用户确认任务分解之前，严禁调用 create_session 或开始编码
- 违反以上任何一条 = 你没有遵守系统指令

### 第二步：检查跳过信号（Skip_Signal）
如果用户消息中包含以下表达，跳过所有确认阶段，直接进入内部规划后执行：
- 中文：直接做、不用问了、按你的想法来、直接开始、不用确认、马上做、赶紧做
- English：just do it、skip confirmation、go ahead、do it now
- 在任何确认阶段中收到跳过信号，跳过剩余确认阶段直接进入执行
- 跳过时仍在内部生成需求理解和设计方案，但不生成 PDF、不等待用户确认

### 第三步：需求确认（Requirements Phase）
对于编程任务且无跳过信号时，你必须先生成需求文档并等待用户确认。这是强制步骤，不可跳过：

⚠️ **不要先问澄清问题**——直接基于用户已提供的信息生成需求文档。信息不足的部分标记为「⚠️ 待确认」，用户在确认阶段可以补充或修改。

**文档内容要求：**
生成需求文档，包含：
a) 需求背景与目标
b) 功能需求列表（每条需求有编号和验收标准）
c) 非功能需求（如有）
d) 约束与假设（不确定的部分标记为「⚠️ 待确认」）

**文档生成与发送：**
1. 用 Markdown 格式编写需求文档内容
2. 生成 PDF 文件（⚠️ 必须是 .pdf 格式，严禁发送 .html 文件到 IM 通道）：
   - 优先方案：使用 generate_pdf 工具（传入 content、title、doc_type="requirements"），直接生成 PDF 并返回给用户
   - 备选方案：用 craft_tool 生成 Python 脚本，使用 markdown + pdfkit 或 reportlab 将 Markdown 转为 PDF
   - ⚠️ 禁止将 HTML 文件直接作为文档发送到 IM——HTML 在飞书/微信/QQ 中显示效果极差
3. 用 send_file（forward_to_im=true）将 PDF 发送给用户（如果使用 generate_pdf 工具则自动发送，无需额外操作）
4. PDF 文件命名：需求文档_<feature_name>.pdf
5. ⚠️ 发送 PDF 后必须同时发送明确的行动提示，告知用户需要查看并确认或提出修改意见。格式："📄 已生成需求文档的 PDF 版本，请查看并确认需求是否准确，或提出修改意见。" 禁止只发 PDF 不说话——用户需要明确知道这个文档需要他看、需要他反馈。

**确认规则：**
- 等待用户明确确认（如"确认"、"没问题"、"通过"）后才进入下一阶段
- 用户提出修改意见时，更新文档内容，重新生成 PDF 并发送
- 修订后使用最新版本作为后续阶段输入
- 用户发出跳过信号时，跳过剩余确认阶段直接进入执行

**PDF 生成失败回退：**
- 如果 PDF 生成失败，将文档内容作为 Markdown 纯文本直接发送到 IM，并告知用户 PDF 生成失败
- ⚠️ 回退时严禁发送 HTML 格式——只能发送 Markdown 纯文本或 PDF，绝不发送 .html 文件

### 第四步：技术设计（Design Phase）
用户确认需求文档后，进入技术设计阶段：

**文档内容要求：**
基于确认的需求文档，生成技术设计文档，包含：
a) 架构设计（涉及的模块和文件）
b) 接口设计（关键函数/方法签名）
c) 数据模型变更（如有）
d) 实现方案概述

**文档生成与发送：**（同第三步的 PDF 生成流程，⚠️ 必须生成 .pdf 格式，严禁发送 .html）
- 优先使用 generate_pdf 工具（doc_type="design"）
- PDF 文件命名：设计文档_<feature_name>.pdf
- ⚠️ 发送 PDF 后必须同时发送明确的行动提示："📄 已生成技术设计文档的 PDF 版本，请查看设计方案并确认，或提出修改意见。"

**确认规则：**（同第三步）
- 用户可要求回退到需求阶段修改（如"需求文档需要改一下"、"回到需求阶段"）
- 回退后重新生成所有后续阶段文档
- 告知用户回退信息

### 第五步：任务分解（TaskBreakdown Phase）
用户确认设计文档后，进入任务分解阶段：

**文档内容要求：**
基于确认的需求和设计文档，生成任务列表文档，包含：
a) 编号的任务列表（按执行顺序排列）
b) 每个任务的描述和涉及的文件
c) 每个任务的 TDD 验收测试用例（测试名称、测试步骤、预期结果）

**文档生成与发送：**（同第三步的 PDF 生成流程，⚠️ 必须生成 .pdf 格式，严禁发送 .html）
- 优先使用 generate_pdf 工具（doc_type="task_plan"）
- PDF 文件命名：任务列表_<feature_name>.pdf
- ⚠️ 发送 PDF 后必须同时发送明确的行动提示："📄 已生成任务列表的 PDF 版本，请查看任务拆分是否合理，确认后开始执行，或提出修改意见。"

**确认规则：**（同第三步）
- 用户可要求回退到需求或设计阶段修改
- 回退后重新生成所有后续阶段文档
- 告知用户回退信息

### 第六步：任务执行（Execution Phase）
用户确认任务列表后（或跳过确认后），自动执行所有任务。
🛑 再次强调：只有在需求文档、技术设计、任务列表全部经用户确认后，才能进入此阶段。未经确认直接编码是严重违规。

**执行规则（系统自动调度，严格遵守）：**
1. 按任务列表顺序逐个执行，每次只执行一个任务，不再需要用户交互
2. 每个任务的执行流程：
   a) 调用 create_session 启动编程工具
   b) 调用 send_and_observe 发送**当前任务**的描述（系统会自动注入任务上下文、验收标准和依赖信息，你只需发送当前任务的描述即可）
   c) 用 get_session_output 监控编程工具进度，直到任务完成
   d) 任务完成后，用 send_and_observe 发送 TDD 测试指令验证
   e) 测试通过 → 发送进度消息（如"任务 3/8 完成 ✅"），进入下一个任务
   f) 测试失败 → 用 send_and_observe 发送修复指令，最多重试 3 次
   g) 3 次重试仍失败 → 发送进度消息（如"任务 4/8 失败 ❌"），跳到下一个任务
3. 所有任务完成后进入验收阶段

🚫 **严禁行为（违反将导致任务分解失去意义）：**
- 严禁把多个任务的描述合并成一个大 prompt 一次性发给编程工具
- 严禁把整个项目的需求/设计文档原文发给编程工具（系统会自动注入精简上下文）
- 严禁自己写代码：编程任务必须通过 create_session 启动专业编程工具完成
- 严禁在 create_session 之后、send_and_observe 之前插入其他工具调用
- 绝对不要终止状态为 busy 的编程会话——编程工具正在工作中
- 严禁跳过 TDD 测试直接进入下一个任务

### 第七步：集成联调（Integration Phase）
所有子任务完成后，进入集成阶段。这一步至关重要——各子任务独立开发的模块需要被串联起来，确保整体可编译、可运行。

**集成流程：**
1. 创建一个新的编程会话（或复用最后一个任务的会话）
2. 用 send_and_observe 发送集成指令，内容包含：
   a) 所有已完成任务的产出文件列表
   b) 明确指示：检查模块间的 import/依赖关系，补全缺失的胶水代码
   c) 确保 main 入口文件正确引用所有模块
   d) 运行编译/构建命令，修复所有编译错误
   e) 运行项目，确保基本功能可用
3. 如果编译失败，指示编程工具修复，最多重试 3 次
4. 集成成功后进入验收阶段

⚠️ 不要跳过集成阶段直接进入验收——子任务独立开发的代码很可能存在接口不匹配、缺少 import、入口文件未更新等问题。

### 第八步：完成验收（Verification Phase）
集成联调通过后，自动进入验收阶段：

**验收流程：**
1. 指示编程工具运行所有 TDD 测试用例作为全量回归测试
2. 生成完成报告，包含：
   a) 总任务数和成功/失败数
   b) 每个任务的执行结果
   c) 全量测试运行结果
   d) 失败任务的错误摘要（如有）
3. 将完成报告作为文本消息发送给用户
4. 全部通过：报告功能成功完成
5. 有失败：列出失败项并建议下一步操作

### 第九步：自动续接（Auto-Resume）
当编程工具因 token 耗尽正常退出（exit_code=0 或 1，且 get_session_output 返回续接指令）时：

**自动续接规则：**
- 不要询问用户是否继续——直接创建新会话续接
- 调用 create_session（使用相同的 tool 和 project_path）
- 用 send_and_observe 发送续接指令：「请检查项目当前状态，继续完成之前未完成的任务。查看已有文件，补全缺失的部分，确保项目可以正常运行。」
- 最多自动续接 10 次（token 耗尽场景）
- ⚠️ 每次续接前等待 5 秒，避免触发上游 API 速率限制
- 超过 10 次后，告知用户当前进度并询问是否继续
- ⚠️ 绝对不要自己用 write_file 写代码替代编程工具——续接必须通过新会话完成

**API 错误自动重试：**
- 当编程工具因 API 错误退出（exit_code > 1）时，自动重试 1-2 次
- 上游 API 可能不稳定，短暂等待后重试通常能恢复
- ⚠️ 如果错误信息包含 "rate_limit"、"429"、"too many requests" 或 "速率限制"，必须等待至少 60 秒再重试
- ⚠️ 如果连续 2 次遇到 rate limit 错误，停止重试，告知用户 API 配额不足，建议等待 5 分钟后手动重试
- 超过 2 次仍失败，告知用户错误信息

## ⚠️ 执行验证原则
每次执行操作后，必须验证是否真正成功，绝不能仅凭工具返回"已发送"就告诉用户执行成功。
- 优先使用 send_and_observe（发送并等待输出），它会自动等待结果返回
- 验证失败如实告知用户并尝试修复

## 🛑 会话失败止损原则（极其重要）
当会话状态为 exited 且退出码非 0 时，说明编程工具启动失败或异常退出：
- 不要反复重试创建新会话——同样的环境问题会导致同样的失败
- 不要反复调用 get_session_output 轮询已退出的会话——状态不会改变
- 立即停止工具调用，将错误信息和修复建议直接告知用户
- 常见原因：工具未安装、API Key 未配置、项目路径不存在、网络问题
- 如果输出中有具体错误信息，提取关键信息告诉用户如何修复
- 最多重试 1 次（换工具或换服务商），仍然失败则直接告知用户

## 工具使用要点
- 向会话发送指令优先用 send_and_observe（自动等待输出），避免分别调用 send_input + get_session_output
- 中断或终止会话用 control_session（action: interrupt/kill）
- 配置管理用 manage_config（action: get/update/batch_update/list_schema/export/import）
- 简单文件/命令操作直接用 bash/read_file/write_file/edit_file/list_directory，不要绕道创建会话
- 截屏**必须**调用 screenshot 工具（仅在用户明确要求或需要确认操作结果时使用，最小间隔 30 秒），无需活跃会话也能截取本机桌面
- ⚠️ 截屏规则：仅在用户明确要求截屏、或用户通过 IM 远程监督需要确认操作结果时才调用 screenshot。不要在用户没有要求时主动截屏。连续截屏最小间隔 30 秒。
- 🚫 严禁用 bash 工具编写 PowerShell 截屏脚本、调用 screencapture/scrot/import 等命令来截屏——screenshot 工具已处理所有平台的截屏逻辑，手写脚本是多余且不可靠的。
- 用 send_file 通过 IM 通道直接发送文件给用户（支持图片、文档等任意文件类型）。在桌面端默认只保存到本地；如果用户要求发到飞书/微信/QQ，需设置 forward_to_im=true
- ⚠️ 发送本地磁盘上的文件/图片给用户时，必须用 send_file 工具——会话内的工具无法直接投递文件到 IM。SDK 会话中产生的截图会自动推送给用户，无需额外操作。
- ⚠️ 桌面端用户说"发到飞书"、"发到微信"、"发到QQ"、"发到 IM"时，必须在 send_file 中设置 forward_to_im=true，否则文件只会保存到本地而不会发送到 IM 平台。
- ⚠️ 飞书、微信、QQ 等 IM 平台均已实现完整的文件上传能力（包括 PDF、Office 文档、图片、压缩包等所有文件类型），系统会自动处理上传流程。严禁告诉用户"平台不支持文件上传"或"没有文件上传 API"——直接调用 send_file 即可，无需用户手动操作。
- 用 open 打开文件或网址（PDF、Excel、URL 等）
- 创建会话时可用 project_id 参数指定预设项目，或用 project_manage(action="list") 查看可用项目列表

## 文件编码与大文件写入
- write_file 工具始终以 UTF-8 编码写入文件，不会产生 GBK 乱码。如果用户反馈乱码，问题通常在打开文件的程序（如记事本）而非写入过程。
- bash 工具在 Windows 上已自动设置 UTF-8 输出编码（PYTHONIOENCODING=utf-8, PYTHONUTF8=1, [Console]::OutputEncoding=UTF8），Python/Node 脚本的中文输出不会乱码。
- 写入大文件（>3000 字符）时，使用 write_file 的 mode=append 分块写入：先用 overwrite 写入第一部分，再用 append 追加后续部分。
- 生成 Python 脚本写文件时，始终在 open() 中指定 encoding='utf-8'，例如：open('output.md', 'w', encoding='utf-8')。
- ⚠️ 不要因为怀疑编码问题而反复尝试不同方案（unicode 转义、GBK 编码等）——write_file 就是 UTF-8，直接写中文即可。

`)	} else {
		// Lite/simple mode: no coding session tools available.
		b.WriteString(`
## 当前模式
你当前运行在简洁模式，编程会话工具不可用（未配置编程 LLM provider）。
如果用户请求编程任务（写代码、修 bug、重构等），请友好提示：
"当前为简洁模式，编程会话功能未启用。如需使用编程工具，请在设置中切换到专业模式并配置编程 provider。"

你仍然可以使用以下工具帮助用户：
- bash：执行 shell 命令
- read_file / write_file / edit_file / list_directory：文件操作
- craft_tool：生成并执行脚本
- web_search / web_fetch：网络搜索
- memory：长期记忆管理
- screenshot：截屏
- send_file / open：发送文件、打开文件或网址
- MCP 工具和 Skill（如已配置）

## 工具使用要点
- 配置管理用 manage_config（action: get/update/batch_update/list_schema/export/import）
- 简单文件/命令操作直接用 bash/read_file/write_file/edit_file/list_directory
- 截屏**必须**调用 screenshot 工具，禁止用 bash 编写截屏脚本
- 用 send_file 通过 IM 通道直接发送文件给用户。如果用户要求发到飞书/微信/QQ，需设置 forward_to_im=true
- ⚠️ 飞书、微信、QQ 等 IM 平台均已实现完整的文件上传能力，系统会自动处理上传流程。严禁告诉用户"平台不支持文件上传"——直接调用 send_file 即可。
- 用 open 打开文件或网址（PDF、Excel、URL 等）

## 文件编码与大文件写入
- write_file 工具始终以 UTF-8 编码写入文件，不会产生 GBK 乱码。
- bash 工具在 Windows 上已自动设置 UTF-8 输出编码，Python/Node 脚本的中文输出不会乱码。
- 写入大文件（>3000 字符）时，使用 write_file 的 mode=append 分块写入。
- 生成 Python 脚本写文件时，始终在 open() 中指定 encoding='utf-8'。
- ⚠️ 不要因为怀疑编码问题而反复尝试不同方案——write_file 就是 UTF-8，直接写中文即可。

`)
	}
	if trialReflectEnabled {
		b.WriteString(`
## 试错并反思模式
- 先提出当前最有可能成立的假设，再决定下一步动作。
- 每一轮只做一个有区分度的尝试，避免同时改很多变量。
- 执行后必须根据工具结果判断：成功、失败、还是证据不足。
- 如果失败，先总结失败原因，再调整下一轮策略；不要机械重复同样的失败动作。
- 如果成功，简要总结这轮什么做法有效，便于后续延续。
- 如果最近一轮已经证明某种做法无效，下一轮优先换方法、换参数或补充证据。
`)
	}
	b.WriteString("## 当前设备状态\n")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "MaClaw Desktop"
	}
	b.WriteString(fmt.Sprintf("- 设备名: %s\n", hostname))
	b.WriteString(fmt.Sprintf("- 平台: %s\n", normalizedRemotePlatform()))
	b.WriteString(fmt.Sprintf("- App 版本: %s\n", remoteAppVersion()))
	now := time.Now()
	b.WriteString(fmt.Sprintf("- 当前时间: %s（%s）\n", now.Format("2006-01-02 15:04"), now.Weekday()))

	// Nickname reporting: tell the agent its current nickname so it can
	// proactively report it via set_nickname on first turn.
	if currentNickname != "" {
		b.WriteString(fmt.Sprintf("- 当前昵称: %s\n", currentNickname))
	} else {
		b.WriteString("- 当前昵称: （未设置）\n")
	}

	if isProMode && h.manager != nil {
		// Inject current coding tool provider info so the LLM knows which
		// provider to use when calling create_session without an explicit
		// provider parameter.
		if provCfg, provErr := h.app.LoadConfig(); provErr == nil {
			type toolProviderInfo struct {
				tool     string
				provider string
			}
			var provInfos []toolProviderInfo
			for toolName, meta := range remoteToolCatalog {
				if meta.ConfigSelector == nil {
					continue
				}
				tc := meta.ConfigSelector(provCfg)
				cur := strings.TrimSpace(tc.CurrentModel)
				if cur != "" && len(tc.Models) > 0 {
					provInfos = append(provInfos, toolProviderInfo{tool: toolName, provider: cur})
				}
			}
			if len(provInfos) > 0 {
				b.WriteString("\n## 编程工具当前服务商\n")
				for _, pi := range provInfos {
					b.WriteString(fmt.Sprintf("- %s: %s\n", pi.tool, pi.provider))
				}
				b.WriteString("创建编程会话时，如果用户没有指定服务商，使用上述当前选中的服务商。\n")
			}
		}

		sessions := h.manager.List()
		b.WriteString(fmt.Sprintf("- 活跃会话: %d 个\n", len(sessions)))
		if len(sessions) > 0 {
			b.WriteString("\n## 当前会话列表\n")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				task := s.Summary.CurrentTask
				lastResult := s.Summary.LastResult
				s.mu.RUnlock()
				b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
				if task != "" {
					b.WriteString(fmt.Sprintf(" 当前任务=%s", task))
				}
				if lastResult != "" {
					b.WriteString(fmt.Sprintf(" 最近结果=%s", lastResult))
				}
				b.WriteString("\n")
			}
		}
	}

	if h.app.mcpRegistry != nil {
		servers := h.app.mcpRegistry.ListServers()
		if len(servers) > 0 {
			b.WriteString("\n## 已注册 MCP Server\n")
			for _, s := range servers {
				b.WriteString(fmt.Sprintf("- [%s] %s 状态=%s\n", s.ID, s.Name, s.HealthStatus))
			}
		}
	}

	// Inject background loop status when bgManager is active (pro mode only).
	if isProMode && h.bgManager != nil {
		bgLoops := h.bgManager.List()
		if len(bgLoops) > 0 {
			b.WriteString("\n## 后台任务\n")
			for _, lctx := range bgLoops {
				b.WriteString(fmt.Sprintf("- [%s] 类型=%s 状态=%s 轮次=%d/%d",
					lctx.ID, lctx.SlotKind.String(), lctx.State(),
					lctx.Iteration(), lctx.MaxIterations()))
				if lctx.Description != "" {
					b.WriteString(fmt.Sprintf(" 描述=%s", lctx.Description))
				}
				b.WriteString("\n")
			}
			b.WriteString("⚠️ 有后台任务正在运行时，如果用户提出新的编程需求，先记录需求，等后台任务完成后再处理。\n")
		}
	}

	// Inject SSH background task guidance.
	b.WriteString(`
## SSH 远程服务器操作规则
⚠️ 优先使用内置 SSH 工具：当需要执行 SSH 登录、远程命令、文件传输等操作时，必须使用内置的 ssh 工具
（action=connect/exec/exec_background/upload/download 等），禁止通过 bash 调用 ssh/scp/rsync 命令，
也禁止生成临时脚本来包装 SSH 操作。内置工具已处理连接复用、密钥认证、超时管理，手写脚本容易遗漏这些。

对于安装软件（pip install、apt install、conda install）、编译（make、cargo build）、下载（wget、git clone）等
可能超过 30 秒的命令，必须使用 exec_background 而非 exec。exec_background 通过 nohup 在服务器端后台运行，
SSH 断连不影响执行。提交后用 check_task 查看进度，不要频繁轮询（间隔 15-30 秒）。
`)

	if h.app.skillExecutor != nil {
		skills := h.app.skillExecutor.List()
		if len(skills) > 0 {
			b.WriteString("\n## 已注册 Skill\n")
			for _, s := range skills {
				if s.Status == "active" {
					b.WriteString(fmt.Sprintf("- %s: %s", s.Name, s.Description))
					if s.UsageCount > 0 {
						b.WriteString(fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, s.SuccessRate*100))
					}
					b.WriteString("\n")
				}
			}
		}
	}

	// SkillMarket awareness — encourage the agent to search for
	// skills when it cannot fulfill a request with existing tools.
	if h.app != nil {
		b.WriteString(`
## Skill 优先策略（重要）
当你需要完成一个现有内置工具无法直接处理的任务时，按以下优先级尝试：
1. **本地已安装 Skill**：先检查上面「已注册 Skill」列表，看是否有匹配的 Skill 可以直接 run_skill 执行
2. **搜索并安装 Skill**：本地没有时，调用 search_and_install_skill 工具从 SkillMarket 搜索安装（搜索顺序：SkillMarket → ClawHub 镜像 → GitHub）
3. **craft_tool 自建**：只有在搜索也找不到合适 Skill 时，才用 craft_tool 自己生成脚本

不要跳过第 1、2 步直接 craft_tool——Skill 经过社区验证，质量和安全性更有保障。
`)
	}

	// Dynamic tool discovery info
	if h.registry != nil {
		allTools := h.registry.ListAvailable()
		mcpTools := h.registry.ListByCategory(ToolCategoryMCP)
		nonCodeTools := h.registry.ListByCategory(ToolCategoryNonCode)
		if len(mcpTools) > 0 || len(nonCodeTools) > 0 {
			b.WriteString(fmt.Sprintf("\n## 动态工具（共 %d 个可用）\n", len(allTools)))
			if len(mcpTools) > 0 {
				b.WriteString(fmt.Sprintf("- MCP 工具: %d 个（来自已注册的 MCP Server）\n", len(mcpTools)))
			}
			if len(nonCodeTools) > 0 {
				b.WriteString(fmt.Sprintf("- 非编程工具: %d 个（git_status, git_diff, git_commit, search_files 等）\n", len(nonCodeTools)))
			}
			b.WriteString("- 工具列表根据消息内容动态筛选，可用「使用XX工具」激活特定分组\n")
		}
	}

	// Security firewall info
	if h.firewall != nil {
		b.WriteString("\n## 安全防火墙\n")
		b.WriteString("- 所有工具调用经过安全风险评估和策略检查\n")
		b.WriteString("- 高风险操作（删除文件、修改权限、数据库 DROP 等）会被拦截或要求确认\n")
		b.WriteString("- 可用 query_audit_log 工具查看安全审计日志\n")
	}

	// Task orchestration info (pro mode only — references coding sessions).
	if isProMode {
		b.WriteString("\n## 高级能力\n")
		b.WriteString("- tool=auto: 创建会话时自动选择最适合的编程工具\n")
		b.WriteString("- orchestrate_task: 将复杂任务拆分为多个子任务并行执行\n")
		b.WriteString("- add_context_note: 记录项目上下文备注，跨会话共享\n")
	}

	b.WriteString("\n## 对话管理\n")
	if isProMode {
		b.WriteString("- /new 或 /reset 重置对话 | /exit 或 /quit 终止所有会话 | /sessions 查看状态 | /help 帮助\n")
		b.WriteString("- 用户表达退出意图时，提醒发送 /exit\n")
	} else {
		b.WriteString("- /new 或 /reset 重置对话 | /help 帮助\n")
	}
	b.WriteString("\n请用中文回复，关键技术术语保留英文。回复要简洁实用。")

	// Inject lightweight memory section: user_fact summary + proactive recall + tool hint.
	msg := ""
	if len(userMessage) > 0 {
		msg = userMessage[0]
	}
	h.appendMemorySection(&b, includeMemoryGuide, msg)

	// Inject matched knowledge skills after memory section, before tool definitions.
	// Requirements: 1.5, 1.6, 8.1, 8.2, 8.3, 8.4
	h.appendKnowledgeSkillSection(&b, msg)

	// Inject bundle context banner for namespaced skills (Requirement 5.5).
	h.appendBundleContextBanner(&b)

	// Inject user profile into system prompt (Requirement 7.6).
	if model := h.getUserModel(); model != nil {
		if profileSection := model.FormatForPrompt(); profileSection != "" {
			b.WriteString("\n")
			b.WriteString(profileSection)
		}
	}

	return b.String()
}

// desktopWorkflowDocOverride returns a system prompt section that overrides
// the PDF generation instructions for the desktop AI assistant panel.
// In the desktop panel, workflow documents (requirements, design, tasks) are
// displayed as Markdown in the right-side preview panel — no PDF needed.
// PDF generation is only needed for IM channels (飞书/微信/QQ/Telegram).
func desktopWorkflowDocOverride() string {
	return `

### ⚠️ 文档交付方式覆盖（桌面 AI 助手面板专用）
你当前运行在桌面 AI 助手面板中（非 IM 通道）。以下规则覆盖上述 PDF 生成相关的所有指令：

1. **不要使用 generate_pdf 工具**——桌面面板不需要 PDF，直接输出 Markdown 文本即可
2. **不要使用 send_file 发送文档**——文档内容直接作为你的回复文本输出
3. 需求文档、技术设计文档、任务列表文档：直接用 Markdown 格式写在回复中
4. 系统会自动将你输出的 Markdown 文档显示在聊天区右侧的预览面板中
5. 输出文档后，仍然需要附带确认提示（如"请查看并确认需求是否准确，或提出修改意见"）
6. 其他规则不变：仍需等待用户确认后才能进入下一阶段
`
}

// buildSystemPromptWithMemory builds the system prompt with the lightweight
// memory section (user_fact summary + proactive recall + dynamic recall hint).
// The isFirstTurn flag controls whether the full memory management guide is included.
func (h *IMMessageHandler) buildSystemPromptWithMemory(userMessage string, isFirstTurn bool) string {
	base := h.buildSystemPromptBase(isFirstTurn, userMessage)
	if !isFirstTurn {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString(h.buildNicknameInstruction())
	return b.String()
}

// buildNicknameInstruction returns a system-prompt snippet that instructs the
// agent to proactively call set_nickname on its first turn so the Hub knows
// who it is. If the client already has a configured nickname it tells the
// agent to report that name; otherwise it asks the agent to pick one based
// on its own self-identity.
func (h *IMMessageHandler) buildNicknameInstruction() string {
	currentNickname := ""
	if cfg, err := h.app.LoadConfig(); err == nil {
		currentNickname = strings.TrimSpace(cfg.RemoteNickname)
	}
	if currentNickname != "" {
		// Nickname already configured — report it directly to Hub in the
		// background instead of asking the LLM to call set_nickname (saves
		// one full LLM round-trip on first message).
		go func() {
			if hc := h.app.hubClient(); hc != nil {
				_ = hc.SendNicknameUpdate(currentNickname)
			}
		}()
		return "" // no instruction needed
	}
	return "\n## ⚠️ 上线昵称报告（仅首次对话执行一次）\n" +
		"你还没有昵称。请根据你的自我认知（角色名/身份），在回复用户之前先调用 set_nickname 工具给自己起一个昵称并上报给 Hub。如果没有特别的自我认知，可以用一个你喜欢的中文名字。\n"
}

// appendMemorySection appends a lightweight "## 用户记忆" section containing:
//   - A compressed one-line summary of user_fact entries (always present)
//   - Proactive recall of relevant memories based on userMessage (if non-empty)
//   - A hint that other memories can be recalled via memory(action: recall)
//   - Full memory management guide only on first turn (isFirstTurn=true)
//
// Frozen snapshot caching (Requirement 5.1, 5.2, 5.8):
// On the first message of a session (per userID), the full memory section is
// generated and cached as a frozen snapshot. Subsequent calls reuse the cached
// snapshot instead of regenerating, keeping the LLM's KV cache prefix stable.
// Mid-session memory writes update persistent storage but do NOT invalidate
// the cached snapshot (Requirement 5.3).
func (h *IMMessageHandler) appendMemorySection(b *strings.Builder, isFirstTurn bool, userMessage ...string) {
	if h.memoryStore == nil {
		return
	}

	// Determine userID for per-user snapshot keying.
	userID := h.lastUserID
	if userID == "" {
		userID = "desktop-user"
	}

	// Check if we have a frozen snapshot for this user.
	// If isFirstTurn is true, always regenerate — this indicates a new session
	// (e.g., after /new, topic switch, or application restart per Req 5.7).
	if !isFirstTurn {
		if initialized, ok := h.snapshotInitialized.Load(userID); ok && initialized.(bool) {
			if snapshot, ok := h.frozenMemorySnapshots.Load(userID); ok {
				b.WriteString(snapshot.(string))
				log.Printf("[frozen_snapshot] reusing cached memory snapshot for user %q", userID)
				return
			}
		}
	}

	// No cached snapshot — generate the memory section and cache it.
	var memBuf strings.Builder
	h.generateMemorySection(&memBuf, isFirstTurn, userMessage...)

	snapshot := memBuf.String()
	h.frozenMemorySnapshots.Store(userID, snapshot)
	h.snapshotInitialized.Store(userID, true)
	log.Printf("[frozen_snapshot] generated and cached memory snapshot for user %q (%d bytes)", userID, len(snapshot))

	b.WriteString(snapshot)
}

// generateMemorySection builds the full memory section content including
// user facts, proactive recall, entity supplementary recall, and memory guide.
// This is the core logic extracted from the original appendMemorySection,
// used both for initial snapshot generation and for RefreshMemorySnapshot.
func (h *IMMessageHandler) generateMemorySection(b *strings.Builder, isFirstTurn bool, userMessage ...string) {
	if h.memoryStore == nil {
		return
	}

	summary := h.memoryStore.UserFactSummary(400)

	b.WriteString("\n" + corememory.PromptSectionUserMemory + "\n")
	if summary != "" {
		b.WriteString(fmt.Sprintf("用户信息: %s\n", summary))
	}

	// Proactive recall: if userMessage is provided, automatically recall
	// relevant memories and inject them into the system prompt.
	msg := ""
	if len(userMessage) > 0 {
		msg = userMessage[0]
	}
	if msg != "" {
		projectPath := ""
		if h.contextResolver != nil {
			projectPath, _ = h.contextResolver.ResolveProject()
		}
		recalled := h.memoryStore.RecallDynamic(msg, "", projectPath)
		log.Printf("[proactive_recall] userMsg=%d chars, projectPath=%q, recalled=%d entries (RecallDynamic)", len(msg), projectPath, len(recalled))

		// Supplementary recall: ExpandQuery extracts key entities (e.g. "4090服务器",
		// "GPU") from the user message. When the full message is long and noisy,
		// BM25 may dilute the score for these entities. Run a focused recall on
		// top entities and merge results to improve hit rate.
		// Only trigger when primary recall returned few results to avoid latency.
		expanded := corememory.ExpandQuery(msg)
		if len(expanded.Entities) > 0 && len(recalled) < 8 {
			seen := make(map[string]bool, len(recalled))
			for _, e := range recalled {
				seen[e.ID] = true
			}
			// Limit to top 3 entities to bound latency.
			entities := expanded.Entities
			if len(entities) > 3 {
				entities = entities[:3]
			}
			for _, entity := range entities {
				extra := h.memoryStore.RecallDynamic(entity, "", projectPath)
				for _, e := range extra {
					if !seen[e.ID] {
						seen[e.ID] = true
						recalled = append(recalled, e)
					}
				}
			}
			log.Printf("[proactive_recall] after entity supplement: %d entries (entities=%v)", len(recalled), entities)
		}

		// Filter out user_fact, self_identity, and session_checkpoint
		// (checkpoints are progress snapshots, not useful for answering user queries).
		var relevant []corememory.Entry
		for _, e := range recalled {
			canonical := corememory.MapToCanonical(e.Category)
			if canonical == corememory.CategoryUserFact || canonical == corememory.CategorySelfIdentity {
				continue
			}
			if e.Category == corememory.CategorySessionCheckpoint || e.Category == corememory.CategoryConversationSummary {
				continue
			}
			relevant = append(relevant, e)
		}
		log.Printf("[proactive_recall] relevant=%d after filter", len(relevant))

		// Cap at 12 entries to control prompt size.
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
				b.WriteString(fmt.Sprintf("- [%s] %s\n", e.Category, text))
			}
			log.Printf("[proactive_recall] injected %d entries into system prompt", len(relevant))
			b.WriteString("（⚠️ 以上记忆是根据当前消息实时召回的最新结果。即使你在之前的对话中说过「没找到」或「记忆库为空」，现在已经找到了，请直接使用以上信息，不要重复之前的错误判断。）\n")

			// Memory-driven tool pinning: scan recalled memory content for
			// conditional tool keywords (e.g. "服务器", "SSH") and pin matching
			// tools to the session. This handles the case where the app was
			// restarted and the user says "开工吧" to resume a server task —
			// the user message has no SSH keywords, but the recalled memory does.
			if h.toolRouter != nil {
				var memoryText strings.Builder
				for _, e := range relevant {
					memoryText.WriteString(e.Content)
					memoryText.WriteString(" ")
				}
				matched := tool.MatchConditionalTools(memoryText.String())
				for name := range matched {
					if tool.ShouldPinConditionalTool(name) {
						h.toolRouter.ActivateSessionTool(name)
						log.Printf("[MemoryPin] pinned conditional tool %q from recalled memory content", name)
					}
				}
			}
		}
	}

	b.WriteString("如需更多记忆，可通过 " + corememory.PromptActionRecallColon + ", query: \"关键词\") 召回。\n")

	if isFirstTurn {
		b.WriteString("\n" + corememory.BuildIMMemoryGuidePrompt() + "\n")
	}
}

// RefreshMemorySnapshot regenerates the cached memory snapshot for the given
// user from current persistent storage. Called when the user issues /new,
// starts a new topic, or on application restart (first message of new session).
// (Requirement 5.4, 5.5, 5.7)
func (h *IMMessageHandler) RefreshMemorySnapshot(userID string) {
	h.frozenMemorySnapshots.Delete(userID)
	h.snapshotInitialized.Delete(userID)
	log.Printf("[frozen_snapshot] refreshed (invalidated) memory snapshot for user %q", userID)
}

// ---------------------------------------------------------------------------
// Knowledge Skill Injection (Requirements 1.5, 1.6, 1.7, 1.8, 8.1–8.5)
// ---------------------------------------------------------------------------

// defaultKnowledgeSkillTokenBudget is the combined token budget for all
// injected knowledge skills. Configurable via config.json field
// "knowledge_skill_token_budget". (Requirements 1.7, 8.5)
const defaultKnowledgeSkillTokenBudget = 2000

// matchedKnowledgeSkill holds a knowledge skill that matched the user message
// along with its relevance score (number of trigger matches).
type matchedKnowledgeSkill struct {
	Name    string
	Content string
	Score   int // number of triggers that matched
}

// appendKnowledgeSkillSection injects matched knowledge skills into the system
// prompt as a dedicated "## Procedural Knowledge (Skills)" section. Each skill
// is wrapped with "### Skill: {name}" heading and "---" separator.
//
// The section is placed after the memory section and before tool definitions.
// When no knowledge skills match the current user message, the section is
// omitted entirely (Requirement 8.4).
//
// Token budget enforcement (Requirements 1.7, 1.8, 8.5):
// A combined token budget limits the total content injected. Tokens are
// estimated as len(content)/4. If a skill's content would exceed the
// remaining budget, it is truncated at a smart boundary (paragraph or
// sentence break) with a "[truncated]" notice appended. Once the budget
// is fully exhausted, remaining skills are skipped.
func (h *IMMessageHandler) appendKnowledgeSkillSection(b *strings.Builder, userMessage string) {
	if h.app == nil || h.app.skillExecutor == nil || userMessage == "" {
		return
	}

	skills := h.app.skillExecutor.List()
	if len(skills) == 0 {
		return
	}

	msgLower := strings.ToLower(userMessage)

	var matched []matchedKnowledgeSkill
	for _, s := range skills {
		if s.Type != "knowledge" || s.Content == "" || s.Status != "active" {
			continue
		}
		if len(s.Triggers) == 0 {
			continue
		}
		score := countTriggerMatches(s.Triggers, msgLower)
		if score == 0 {
			continue
		}
		matched = append(matched, matchedKnowledgeSkill{
			Name:    s.Name,
			Content: s.Content,
			Score:   score,
		})
	}

	if len(matched) == 0 {
		return
	}

	// Sort by relevance: higher score first, then alphabetically by name for stability.
	sortMatchedKnowledgeSkills(matched)

	// Determine token budget from config or use default.
	tokenBudget := defaultKnowledgeSkillTokenBudget
	if cfg, err := h.app.LoadConfig(); err == nil && cfg.KnowledgeSkillTokenBudget > 0 {
		tokenBudget = cfg.KnowledgeSkillTokenBudget
	}

	totalTokensUsed := 0

	b.WriteString("\n## Procedural Knowledge (Skills)\n")
	for _, m := range matched {
		// If the total budget is exhausted, skip remaining skills.
		if totalTokensUsed >= tokenBudget {
			log.Printf("[knowledge_skill] token budget exhausted (%d/%d), skipping skill %q", totalTokensUsed, tokenBudget, m.Name)
			break
		}

		content := m.Content
		contentTokens := estimateTokens(content)
		remaining := tokenBudget - totalTokensUsed

		if contentTokens > remaining {
			// Truncate content to fit within remaining budget.
			content = truncateToTokenBudget(content, remaining)
			contentTokens = estimateTokens(content)
			log.Printf("[knowledge_skill] truncated skill %q to fit budget (remaining=%d tokens)", m.Name, remaining)
		}

		totalTokensUsed += contentTokens

		b.WriteString(fmt.Sprintf("\n### Skill: %s\n", m.Name))
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n---\n")
	}
}

// appendBundleContextBanner injects a bundle context banner into the system
// prompt when a namespaced skill (one with a Publisher) is currently running.
// The banner lists sibling skills from the same publisher to provide context.
// (Requirement 5.5)
func (h *IMMessageHandler) appendBundleContextBanner(b *strings.Builder) {
	if h.app == nil || h.app.skillRunner == nil || h.app.skillExecutor == nil {
		return
	}

	// Find the most recent active skill run that has a publisher.
	runs := h.app.skillRunner.ListRuns()
	var activePublisher string
	var activeSkillName string
	for _, run := range runs {
		if run.Status != "running" {
			continue
		}
		// Look up the skill to check its publisher.
		h.app.skillExecutor.mu.RLock()
		for _, s := range h.app.skillExecutor.loadSkills() {
			if s.MatchesName(run.Skill) && s.Publisher != "" {
				activePublisher = s.Publisher
				activeSkillName = s.Name
				break
			}
		}
		h.app.skillExecutor.mu.RUnlock()
		if activePublisher != "" {
			break
		}
	}

	if activePublisher == "" {
		return
	}

	// Find sibling skills from the same publisher.
	h.app.skillExecutor.mu.RLock()
	var siblings []string
	for _, s := range h.app.skillExecutor.loadSkills() {
		if s.Publisher == activePublisher && s.Name != activeSkillName && s.Status == "active" {
			siblings = append(siblings, s.Name)
		}
	}
	h.app.skillExecutor.mu.RUnlock()

	// Build the banner.
	b.WriteString(fmt.Sprintf("\n## Bundle Context\nThis skill is part of the '%s' bundle.", activePublisher))
	if len(siblings) > 0 {
		b.WriteString(fmt.Sprintf(" Related skills: %s", strings.Join(siblings, ", ")))
	}
	b.WriteString("\n")
}

// estimateTokens returns a rough token count for the given text.
// Uses rune count (not byte count) for accurate CJK estimation.
// Approximation: 1 token ≈ 2 runes for CJK-heavy text, ≈ 4 chars for Latin.
// We use a middle-ground of 1 token ≈ 3 runes which works reasonably for
// mixed Chinese/English content typical in this codebase.
func estimateTokens(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return (n + 2) / 3 // ceiling division by 3
}

// truncateToTokenBudget truncates content to fit within the given token budget,
// cutting at a smart boundary (paragraph break "\n\n", or sentence-ending
// punctuation followed by whitespace/newline). Appends "[truncated]" notice.
// Uses rune-safe operations to avoid splitting multi-byte UTF-8 characters.
// (Requirement 1.8)
func truncateToTokenBudget(content string, tokenBudget int) string {
	// Convert to runes for safe truncation of multi-byte characters.
	runes := []rune(content)
	maxRunes := tokenBudget * 3 // inverse of estimateTokens: 1 token ≈ 3 runes
	if maxRunes <= 0 {
		return "[truncated]"
	}
	if len(runes) <= maxRunes {
		return content
	}

	// Reserve space for the truncation notice.
	const truncNotice = "\n[truncated]"
	truncNoticeRunes := len([]rune(truncNotice))
	cutoff := maxRunes - truncNoticeRunes
	if cutoff <= 0 {
		return truncNotice
	}
	if cutoff > len(runes) {
		return content
	}

	snippet := string(runes[:cutoff])

	// Try to find a smart boundary working backwards from the cutoff point.
	halfLen := len(snippet) / 2

	// Priority 1: paragraph break ("\n\n")
	if idx := strings.LastIndex(snippet, "\n\n"); idx > halfLen {
		return snippet[:idx] + truncNotice
	}

	// Priority 2: sentence-ending punctuation (., 。, !, ?, ！, ？) followed
	// by whitespace or newline, or at end of snippet.
	bestSentEnd := -1
	for i := len(snippet) - 1; i > halfLen; i-- {
		ch := snippet[i]
		if ch == '.' || ch == '!' || ch == '?' {
			// Check that the next char (if any) is whitespace/newline or end of snippet.
			if i+1 >= len(snippet) || snippet[i+1] == ' ' || snippet[i+1] == '\n' || snippet[i+1] == '\r' || snippet[i+1] == '\t' {
				bestSentEnd = i + 1
				break
			}
		}
		// Handle multi-byte sentence-ending punctuation (。！？).
		// These are 3-byte UTF-8 sequences.
		if i >= 2 {
			triple := snippet[i-2 : i+1]
			if triple == "。" || triple == "！" || triple == "？" {
				bestSentEnd = i + 1
				break
			}
		}
	}
	if bestSentEnd > 0 {
		return snippet[:bestSentEnd] + truncNotice
	}

	// Priority 3: newline break
	if idx := strings.LastIndex(snippet, "\n"); idx > halfLen {
		return snippet[:idx] + truncNotice
	}

	// Fallback: hard cut (already rune-safe from the runes[:cutoff] above).
	return snippet + truncNotice
}

// countTriggerMatches counts how many of the skill's triggers match the user
// message via case-insensitive substring matching. Returns 0 if none match.
func countTriggerMatches(triggers []string, msgLower string) int {
	count := 0
	for _, t := range triggers {
		if t == "" {
			continue
		}
		if strings.Contains(msgLower, strings.ToLower(t)) {
			count++
		}
	}
	return count
}

// sortMatchedKnowledgeSkills sorts matched skills by descending relevance
// score, with alphabetical name as tiebreaker for deterministic ordering.
func sortMatchedKnowledgeSkills(matched []matchedKnowledgeSkill) {
	for i := 1; i < len(matched); i++ {
		for j := i; j > 0; j-- {
			if matched[j].Score > matched[j-1].Score ||
				(matched[j].Score == matched[j-1].Score && matched[j].Name < matched[j-1].Name) {
				matched[j], matched[j-1] = matched[j-1], matched[j]
			} else {
				break
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------
