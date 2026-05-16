# 需求文档

## 简介

通过 Claude Code 的 hook 系统（PreToolUse / PostToolUse / Stop），将 maclaw 现有的安全护栏体系（Firewall、RiskAnalyzer、PolicyEngine、AuditLog、LLMReview）注入到 Claude Code 的工具执行链路中，使 maclaw 成为 Claude Code 的安全网关。具体实现方式为：在 `maclaw-tool` CLI 中新增 `security-check` 和 `audit-record` 两个纯本地子命令，由 Claude Code 的 PreToolUse/PostToolUse hook 调用；同时在 onboarding 阶段自动注入 hook 配置文件到 `~/.claude/hooks/`。

## 术语表

- **Claude_Code_Hook**: Claude Code 的扩展机制，支持在工具执行前（PreToolUse）、执行后（PostToolUse）、退出时（Stop）插入自定义脚本，通过 stdin 接收 JSON 输入，通过 exit code 和 stdout/stderr 控制行为
- **PreToolUse_Hook**: Claude Code 在执行任何工具前触发的 hook，exit 0 表示放行（stdout 内容作为提示注入），exit 2 表示阻止工具执行（stderr 告知原因）
- **PostToolUse_Hook**: Claude Code 在工具执行完毕后触发的 hook，用于事后审计和状态更新
- **Hook_Input**: Claude Code 通过 stdin 传递给 hook 脚本的 JSON 数据，包含 hook_type、tool_name、tool_input、session_id 等字段
- **Security_Check_Command**: `maclaw-tool security-check` 子命令，纯本地执行，从 stdin 读取安全检查请求，调用 Firewall.Check() 进行安全评估
- **Audit_Record_Command**: `maclaw-tool audit-record` 子命令，纯本地执行，从 stdin 读取审计记录请求，写入 AuditLog
- **Security_Check_Request**: security-check 命令的输入格式，是对 Claude Code Hook_Input 的抽象封装，包含 tool_name、tool_input、session_id、source 等字段，不绑定特定工具的 JSON schema
- **Audit_Record_Request**: audit-record 命令的输入格式，包含 tool_name、tool_input、session_id、result、output_snippet 等字段
- **Session_State_File**: 存储在 `/tmp/maclaw-session-{session_id}.json` 的临时文件，用于跨多次 hook 调用持久化会话安全上下文（累积风险计数、安全模式升级状态等）
- **Firewall（防火墙）**: maclaw 的统一安全检查入口，整合 RiskAnalyzer、PolicyEngine、AuditLog 进行工具执行前的安全拦截
- **RiskAnalyzer（风险分析器）**: 基于正则模式的风险评估引擎，包含 15+ 个内置风险模式
- **PolicyEngine（策略引擎）**: 支持 standard/strict/relaxed 三种安全模式的策略规则评估器
- **AuditLog（审计日志）**: 按日期分割、支持大小轮转和 30 天保留的审计日志系统
- **PolicyAsk_Action**: PolicyEngine 评估结果为 "ask"（需要用户确认）的动作，在无交互式确认通道的 hook 场景下需要特殊处理
- **Hook_Config_File**: 存放在 `~/.claude/hooks/` 目录下的 JSON 文件，定义 Claude Code 的 hook 配置，多个文件会合并执行

## 需求

### 需求 1：PreToolUse 前置安全拦截

**用户故事：** 作为使用 Claude Code 的开发者，我希望在 Claude Code 执行任何工具前，maclaw 的安全护栏能自动进行风险评估和拦截，以便防止危险操作被执行。

#### 验收标准

1. WHEN Claude Code 的 PreToolUse hook 触发时，THE Security_Check_Command SHALL 从 stdin 读取 JSON 格式的 Security_Check_Request，解析出 tool_name、tool_input 和 session_id 字段
2. WHEN Security_Check_Request 解析成功，THE Security_Check_Command SHALL 调用 Firewall.Check() 方法，传入 tool_name、tool_input（作为 args map）和包含 session_id 的 CallContext
3. WHEN Firewall.Check() 返回放行结果（allowed=true），THE Security_Check_Command SHALL 以 exit code 0 退出，并在 stdout 输出安全检查摘要信息（包含风险等级和匹配的风险模式）
4. WHEN Firewall.Check() 返回拦截结果（allowed=false），THE Security_Check_Command SHALL 以 exit code 2 退出，并在 stderr 输出拦截原因（包含风险等级、触发的策略规则和拦截说明）
5. IF Security_Check_Request 的 JSON 解析失败或缺少必要字段（tool_name），THEN THE Security_Check_Command SHALL 以 exit code 0 退出并在 stderr 输出解析错误信息，避免因格式错误阻塞 Claude Code 的正常工作流
6. THE Security_Check_Command SHALL 在 10 毫秒量级内完成安全检查（不含 LLM 审查），确保对 Claude Code 工具执行链路的延迟影响可忽略
7. WHEN Security_Check_Request 包含 source 字段，THE Security_Check_Command SHALL 将该字段记录到审计日志中，用于区分不同调用来源（如 "claude-code"、"codex" 等）

### 需求 2：PostToolUse 事后审计记录

**用户故事：** 作为安全运维人员，我希望 Claude Code 每次工具执行完毕后都能记录审计日志，以便事后追溯和分析工具调用行为。

#### 验收标准

1. WHEN Claude Code 的 PostToolUse hook 触发时，THE Audit_Record_Command SHALL 从 stdin 读取 JSON 格式的 Audit_Record_Request，解析出 tool_name、tool_input、session_id 和 result 字段
2. WHEN Audit_Record_Request 解析成功，THE Audit_Record_Command SHALL 调用 AuditLog.Log() 方法写入一条审计条目，包含工具名称、参数、会话 ID、执行结果和时间戳
3. WHEN Audit_Record_Request 包含 output_snippet 字段，THE Audit_Record_Command SHALL 对输出片段进行敏感信息检测（检查是否包含 API key、密码、私钥等模式），并在审计条目中标记检测结果
4. THE Audit_Record_Command SHALL 以 exit code 0 退出，确保审计记录失败不会阻塞 Claude Code 的工具执行链路
5. IF Audit_Record_Request 的 JSON 解析失败，THEN THE Audit_Record_Command SHALL 以 exit code 0 退出并在 stderr 输出解析错误信息
6. WHEN Audit_Record_Request 包含 session_id，THE Audit_Record_Command SHALL 更新 Session_State_File 中的累积风险计数

### 需求 3：security-check CLI 子命令

**用户故事：** 作为开发者，我希望 `maclaw-tool` 提供一个纯本地执行的 `security-check` 子命令，以便 Claude Code hook 和其他工具能通过标准 CLI 接口调用 maclaw 的安全检查能力。

#### 验收标准

1. THE Security_Check_Command SHALL 作为 `maclaw-tool security-check` 子命令注册，不需要 `--token` 参数，不连接 Hub，纯本地执行
2. THE Security_Check_Command SHALL 支持 `--mode` 参数，可选值为 standard、strict、relaxed，默认为 standard，用于指定 PolicyEngine 的安全模式
3. THE Security_Check_Command SHALL 支持 `--project` 参数，指定项目路径，用于加载项目级安全策略文件（`.maclaw/security-policy.json`）
4. WHEN `--project` 参数指定的路径下存在 `.maclaw/security-policy.json` 文件，THE Security_Check_Command SHALL 加载该文件中的策略规则覆盖默认规则
5. THE Security_Check_Request 输入格式 SHALL 包含以下字段：`tool_name`（必填）、`tool_input`（可选，map 类型）、`session_id`（可选）、`source`（可选，标识调用来源）
6. THE Security_Check_Command SHALL 在无任何 stdin 输入时（EOF），以 exit code 0 退出并在 stderr 输出使用说明

### 需求 4：audit-record CLI 子命令

**用户故事：** 作为开发者，我希望 `maclaw-tool` 提供一个纯本地执行的 `audit-record` 子命令，以便 Claude Code 的 PostToolUse hook 能通过标准 CLI 接口记录审计日志。

#### 验收标准

1. THE Audit_Record_Command SHALL 作为 `maclaw-tool audit-record` 子命令注册，不需要 `--token` 参数，不连接 Hub，纯本地执行
2. THE Audit_Record_Command SHALL 支持 `--audit-dir` 参数，指定审计日志存储目录，默认为 `~/.maclaw/audit/`
3. THE Audit_Record_Request 输入格式 SHALL 包含以下字段：`tool_name`（必填）、`tool_input`（可选）、`session_id`（可选）、`result`（可选，工具执行结果描述）、`output_snippet`（可选，工具输出片段）
4. WHEN 审计日志目录不存在，THE Audit_Record_Command SHALL 自动创建该目录
5. THE Audit_Record_Command SHALL 在无任何 stdin 输入时（EOF），以 exit code 0 退出并在 stderr 输出使用说明

### 需求 5：Hook 配置自动注入

**用户故事：** 作为使用 Claude Code 的开发者，我希望 maclaw 在 Claude Code 会话创建时自动注入安全 hook 配置，以便无需手动配置即可获得安全防护。

#### 验收标准

1. WHEN EnsureClaudeOnboarding 函数执行时，THE Onboarding_Logic SHALL 在 `~/.claude/hooks/` 目录下创建 `maclaw-security.json` 配置文件，包含 PreToolUse 和 PostToolUse 两个 hook 定义
2. THE PreToolUse hook 定义 SHALL 配置为调用 `maclaw-tool security-check` 命令，并通过 `--project` 参数传入当前项目路径
3. THE PostToolUse hook 定义 SHALL 配置为调用 `maclaw-tool audit-record` 命令
4. WHEN `~/.claude/hooks/maclaw-security.json` 已存在且包含 maclaw 标记注释（`_comment` 字段包含 "maclaw-security-gateway"），THE Onboarding_Logic SHALL 跳过写入，保持幂等性
5. THE maclaw-security.json 和现有的 maclaw-stop.json（或 stop.json）SHALL 作为独立文件存在，互不干扰，Claude Code 会合并执行所有 hook 文件
6. IF `maclaw-tool` 二进制文件不在 PATH 中，THEN THE Onboarding_Logic SHALL 使用完整的二进制文件路径（通过 `os.Executable()` 或已知安装路径推断）
7. WHEN hook 配置文件写入失败（如权限不足），THE Onboarding_Logic SHALL 记录警告日志但不阻塞会话创建流程

### 需求 6：会话安全状态持久化

**用户故事：** 作为安全系统，我希望跨多次 hook 调用保持会话的安全上下文（如累积风险计数），以便检测渐进式风险升级模式。

#### 验收标准

1. WHEN Security_Check_Command 或 Audit_Record_Command 接收到包含 session_id 的请求，THE Session_State_File SHALL 被创建或更新在 `/tmp/maclaw-session-{session_id}.json` 路径
2. THE Session_State_File SHALL 包含以下字段：session_id、tool_call_count（工具调用总次数）、high_risk_count（high/critical 风险触发次数）、last_check_time（最后检查时间戳）、security_mode（当前安全模式，可能因累积风险被升级）
3. WHEN 同一会话在 5 分钟内累积触发 3 次及以上 high 或 critical 风险评估，THE Security_Check_Command SHALL 自动将该会话的安全模式升级为 strict，并在 Session_State_File 中记录升级状态
4. WHEN 会话安全模式被自动升级为 strict，THE Security_Check_Command SHALL 在 stdout 输出安全模式升级通知，告知 Claude Code 当前会话已进入严格安全模式
5. THE Session_State_File SHALL 使用文件锁（flock 或等效机制）防止并发写入冲突
6. WHEN Session_State_File 读取失败（文件损坏或不存在），THE Security_Check_Command SHALL 创建新的初始状态文件并继续执行，确保不因状态文件问题阻塞安全检查

### 需求 7：PolicyAsk 动作的 Hook 场景处理

**用户故事：** 作为安全系统设计者，我希望在无交互式确认通道的 Claude Code hook 场景下，PolicyAsk 动作能被合理处理，以便在安全性和可用性之间取得平衡。

#### 验收标准

1. WHEN PolicyEngine 评估结果为 PolicyAsk 且当前安全模式为 strict，THE Security_Check_Command SHALL 将 PolicyAsk 视为 PolicyDeny，以 exit code 2 退出并在 stderr 说明原因
2. WHEN PolicyEngine 评估结果为 PolicyAsk 且当前安全模式为 standard，THE Security_Check_Command SHALL 以 exit code 0 退出，并在 stdout 输出风险提示信息（包含风险等级、原因和建议），作为提示注入给 Claude Code 让其自行判断
3. WHEN PolicyEngine 评估结果为 PolicyAsk 且当前安全模式为 relaxed，THE Security_Check_Command SHALL 以 exit code 0 退出并放行，仅在审计日志中记录该事件
4. THE Security_Check_Command 的 stdout 风险提示信息 SHALL 使用结构化的自然语言格式，包含风险等级、触发原因和安全建议，便于 Claude Code 理解并做出合理决策

### 需求 8：敏感信息输出检测

**用户故事：** 作为安全运维人员，我希望 PostToolUse 审计阶段能检测工具输出中的敏感信息，以便及时发现潜在的数据泄露。

#### 验收标准

1. WHEN Audit_Record_Request 包含 output_snippet 字段，THE Audit_Record_Command SHALL 对该字段内容进行敏感信息模式匹配
2. THE Audit_Record_Command SHALL 检测以下敏感信息模式：API key 格式（如 `sk-`、`AKIA` 前缀）、私钥标记（如 `-----BEGIN.*PRIVATE KEY-----`）、密码赋值模式（如 `password=`、`passwd:`）、JWT token 格式
3. WHEN 检测到敏感信息模式，THE Audit_Record_Command SHALL 在审计条目中添加 `sensitive_detected: true` 标记和匹配的模式类别列表
4. THE Audit_Record_Command SHALL 对 output_snippet 中匹配到的敏感信息进行脱敏处理（替换为 `[REDACTED]`）后再写入审计日志，防止审计日志本身成为敏感信息泄露源
5. WHEN 检测到敏感信息且风险等级为 high 或 critical，THE Audit_Record_Command SHALL 在 stdout 输出警告信息，提示 Claude Code 注意输出中包含敏感内容

### 需求 9：Security_Check_Request 输入格式抽象

**用户故事：** 作为平台开发者，我希望 security-check 的输入格式不绑定 Claude Code 的特定 JSON schema，以便其他 AI 编程工具（如 Codex、Cursor 等）也能复用 maclaw 的安全检查能力。

#### 验收标准

1. THE Security_Check_Request JSON 格式 SHALL 定义为独立的通用格式，包含 `tool_name`（string，必填）、`tool_input`（object，可选）、`session_id`（string，可选）、`source`（string，可选）、`project_path`（string，可选）字段
2. THE PreToolUse hook 脚本 SHALL 负责将 Claude Code 的 Hook_Input JSON 格式转换为 Security_Check_Request 格式，再传递给 security-check 命令
3. THE hook 脚本中的格式转换逻辑 SHALL 将 Claude Code 的 `hook_type` 映射忽略（由 hook 配置决定调用哪个命令）、`tool_name` 直接透传、`tool_input` 直接透传、`session_id` 直接透传
4. THE Security_Check_Request 格式 SHALL 通过 JSON Schema 或 Go struct tag 进行文档化，便于第三方工具集成
5. FOR ALL 合法的 Security_Check_Request JSON 输入，序列化后再反序列化 SHALL 产生等价的请求对象（round-trip 属性）
