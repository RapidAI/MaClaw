# 实施计划：Claude Hook Security Gateway

## 概述

将 maclaw 安全护栏体系通过 Claude Code hook 机制注入工具执行链路。实现 `maclaw-tool` 的 `security-check` 和 `audit-record` 两个纯本地子命令，以及 hook 配置自动注入。使用 Go 语言，基于现有 `corelib/security` 包扩展。

## 任务

- [ ] 1. 创建敏感信息检测模块
  - [x] 1.1 创建 `corelib/security/sensitive_detector.go`，实现 SensitiveDetector
    - 定义 SensitiveMatch 结构体（Category, Pattern 字段）
    - 定义内部 sensitivePattern 结构体（name, category, compiled regex）
    - 实现 NewSensitiveDetector()，内置四类检测模式：API key（`sk-`/`AKIA` 前缀）、私钥标记（`-----BEGIN.*PRIVATE KEY-----`）、密码赋值（`password=`/`passwd:`）、JWT token
    - 实现 Detect(text string) []SensitiveMatch 方法，返回所有匹配的敏感信息类别
    - 实现 Redact(text string) string 方法，将匹配内容替换为 `[REDACTED]`
    - _需求: 8.1, 8.2, 8.3, 8.4_

  - [ ]* 1.2 编写 Property 5 的属性测试：敏感信息检测准确性
    - **Property 5: 敏感信息检测准确性**
    - 对包含已知敏感模式的字符串，Detect() 应返回非空结果且包含正确类别；对不包含敏感模式的字符串，应返回空结果
    - **验证: 需求 8.1, 8.2, 8.3**

  - [ ]* 1.3 编写 Property 6 的属性测试：脱敏处理消除所有敏感模式
    - **Property 6: 脱敏处理消除所有敏感模式**
    - 对任意字符串，Detect(Redact(text)) 应返回空结果
    - **验证: 需求 8.4**

  - [ ]* 1.4 编写 SensitiveDetector 单元测试
    - 测试已知 API key 格式（`sk-abc123...`、`AKIA...`）检测
    - 测试 JWT token 格式检测
    - 测试私钥标记检测
    - 测试空字符串、不含敏感信息的普通文本
    - 测试脱敏后内容不再包含原始敏感信息
    - _需求: 8.1, 8.2, 8.3, 8.4_

- [ ] 2. 创建会话状态持久化模块
  - [x] 2.1 创建 `corelib/security/session_state.go`，实现 SessionStateManager
    - 定义 SessionState 结构体（SessionID, ToolCallCount, HighRiskCount, LastCheckTime, SecurityMode, ModeUpgraded, HighRiskTimestamps）
    - 实现 LoadSessionState(sessionID string) (*SessionState, error)，从 `/tmp/maclaw-session-{id}.json` 加载状态
    - 实现 Save() error 方法，使用文件锁（flock）防止并发写入
    - 实现 IncrementToolCall() 方法
    - 实现 IncrementHighRisk() bool 方法，包含 5 分钟窗口内 ≥3 次 high/critical 触发自动升级为 strict 模式的逻辑
    - 状态文件损坏时创建新的初始状态并继续执行
    - _需求: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [ ]* 2.2 编写 Property 8 的属性测试：会话状态持久化正确性
    - **Property 8: 会话状态持久化正确性**
    - 对包含 session_id 的请求序列，处理后 tool_call_count 应等于序列长度，high_risk_count 应等于 high/critical 次数
    - **验证: 需求 6.1, 6.2, 2.6**

  - [ ]* 2.3 编写 Property 9 的属性测试：累积高风险自动升级安全模式
    - **Property 9: 累积高风险自动升级安全模式**
    - 5 分钟窗口内 ≥3 次 high/critical → security_mode 升级为 strict 且 mode_upgraded=true；<3 次或超过 5 分钟窗口 → 不触发升级
    - **验证: 需求 6.3, 6.4**

  - [ ]* 2.4 编写 SessionStateManager 单元测试
    - 测试新会话创建初始状态
    - 测试 3 次高风险触发模式升级
    - 测试状态文件损坏时的恢复行为
    - _需求: 6.1, 6.2, 6.3, 6.6_

- [x] 3. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户确认。

- [ ] 4. 实现 security-check 子命令
  - [x] 4.1 创建 `cmd/maclaw-tool/security_check.go`，实现 SecurityCheckCommand
    - 定义 SecurityCheckRequest 结构体（ToolName, ToolInput, SessionID, Source, ProjectPath）
    - 定义 SecurityCheckResult 结构体（Allowed, RiskLevel, Reason, Factors, ModeUpgrade）
    - 实现 runSecurityCheck(mode, projectPath string) int 函数
    - 从 stdin 读取 JSON 格式的 SecurityCheckRequest
    - 创建 RiskAnalyzer + PolicyEngine（按 mode 参数）+ AuditLog
    - 如有 projectPath，通过 Firewall.LoadProjectPolicy() 加载项目级策略
    - 调用 Firewall.Check()，根据结果输出 stdout/stderr 并返回 exit code（0 放行 / 2 拦截）
    - 处理 PolicyAsk 按安全模式分级：strict→deny(exit 2)、standard→放行+提示(exit 0)、relaxed→静默放行(exit 0)
    - 加载/更新 SessionState，检查是否需要自动升级安全模式
    - 空 stdin / JSON 解析失败 / 缺少 tool_name 时以 exit 0 退出并在 stderr 输出错误信息
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 7.1, 7.2, 7.3, 7.4_

  - [ ]* 4.2 编写 Property 1 的属性测试：SecurityCheckRequest 序列化往返
    - **Property 1: SecurityCheckRequest 序列化往返**
    - 任意合法 SecurityCheckRequest 序列化为 JSON 后再反序列化，应产生等价对象
    - **验证: 需求 1.1, 3.5, 9.5**

  - [ ]* 4.3 编写 Property 3 的属性测试：Firewall 结果到 exit code 映射
    - **Property 3: Firewall 结果到 exit code 的映射**
    - allowed=true → exit 0，allowed=false → exit 2
    - **验证: 需求 1.3, 1.4**

  - [ ]* 4.4 编写 Property 4 的属性测试：PolicyAsk 按安全模式分级处理
    - **Property 4: PolicyAsk 按安全模式分级处理**
    - strict→exit 2，standard→exit 0 + stdout 风险提示，relaxed→exit 0 + 仅审计
    - **验证: 需求 7.1, 7.2, 7.3**

  - [ ]* 4.5 编写 Property 14 的属性测试：mode 参数正确配置 PolicyEngine
    - **Property 14: mode 参数正确配置 PolicyEngine**
    - 对 standard/strict/relaxed 三种 mode，创建的 PolicyEngine 应使用对应模式的策略规则集
    - **验证: 需求 3.2**

  - [ ]* 4.6 编写 Property 15 的属性测试：请求数据正确流转到 Firewall 和 AuditLog
    - **Property 15: 请求数据正确流转到 Firewall 和 AuditLog**
    - toolName == request.tool_name，args == request.tool_input，CallContext.SessionID == request.session_id，审计日志 source == request.source
    - **验证: 需求 1.2, 1.7, 2.2, 2.3**

  - [ ]* 4.7 编写 security-check 单元测试
    - 测试已知危险命令（`rm -rf /`）应被拦截
    - 测试安全的文件读取操作应被放行
    - 测试空 stdin、畸形 JSON、缺少 tool_name 的边界情况
    - _需求: 1.1, 1.3, 1.4, 1.5_

- [ ] 5. 实现 audit-record 子命令
  - [x] 5.1 创建 `cmd/maclaw-tool/audit_record.go`，实现 AuditRecordCommand
    - 定义 AuditRecordRequest 结构体（ToolName, ToolInput, SessionID, Result, OutputSnippet, Source）
    - 实现 runAuditRecord(auditDir string) int 函数
    - 从 stdin 读取 JSON 格式的 AuditRecordRequest
    - 创建 AuditLog（指向 auditDir，默认 `~/.maclaw/audit/`）
    - 对 output_snippet 调用 SensitiveDetector.Detect() 进行敏感信息检测
    - 检测到敏感信息时：在审计条目中标记 sensitive_detected=true 和类别列表，对 output_snippet 调用 Redact() 脱敏后再写入
    - 调用 AuditLog.Log() 写入审计条目
    - 更新 SessionState 的 tool_call_count
    - 始终以 exit code 0 退出（包括解析失败、空 stdin 等情况）
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 4.1, 4.2, 4.3, 4.4, 4.5, 8.1, 8.2, 8.3, 8.4, 8.5_

  - [ ]* 5.2 编写 Property 2 的属性测试：AuditRecordRequest 序列化往返
    - **Property 2: AuditRecordRequest 序列化往返**
    - 任意合法 AuditRecordRequest 序列化为 JSON 后再反序列化，应产生等价对象
    - **验证: 需求 2.1, 4.3**

  - [ ]* 5.3 编写 Property 10 的属性测试：audit-record 始终 exit 0
    - **Property 10: audit-record 始终以 exit code 0 退出**
    - 对任意输入（合法 JSON、非法 JSON、空输入），exit code 始终为 0
    - **验证: 需求 2.4, 2.5**

  - [ ]* 5.4 编写 audit-record 单元测试
    - 测试正常审计记录写入
    - 测试包含敏感信息的 output_snippet 被正确脱敏
    - 测试空 stdin、审计目录不存在时自动创建
    - _需求: 2.1, 2.2, 2.3, 2.4, 4.4_

- [x] 6. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户确认。

- [ ] 7. 修改 maclaw-tool main.go 添加子命令路由
  - [x] 7.1 修改 `cmd/maclaw-tool/main.go`，在 main() 函数中添加 security-check 和 audit-record 的路由
    - 在 flag.Parse() 之后、`--token` 校验之前，检查第一个参数是否为 `security-check` 或 `audit-record`
    - 如果是 `security-check`：解析 `--mode`（默认 standard）和 `--project` 参数，调用 runSecurityCheck()，os.Exit() 返回结果
    - 如果是 `audit-record`：解析 `--audit-dir`（默认 `~/.maclaw/audit/`）参数，调用 runAuditRecord()，os.Exit() 返回结果
    - 这两个子命令跳过 `--token` 校验和 Hub 连接，纯本地执行
    - 更新 usage() 函数，添加 security-check 和 audit-record 的使用说明
    - _需求: 3.1, 3.2, 3.3, 3.6, 4.1, 4.2, 4.5_

- [ ] 8. 实现 Hook 配置自动注入
  - [x] 8.1 创建 `corelib/configfile/claude_hook_injector.go`，实现 EnsureClaudeSecurityHook()
    - 实现 EnsureClaudeSecurityHook(home, maclawBinary, tag string, logFn func(string)) error
    - 在 `~/.claude/hooks/` 下创建 `maclaw-security.json`
    - 包含 PreToolUse hook（调用 `maclaw-tool security-check`）和 PostToolUse hook（调用 `maclaw-tool audit-record`）
    - 通过 `_comment` 字段包含 `"maclaw-security-gateway"` 标记实现幂等检测
    - 如果 maclawBinary 为空，通过 os.Executable() 推断路径，失败时回退到 "maclaw-tool"
    - 写入失败时记录警告日志但不返回错误
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.6, 5.7_

  - [x] 8.2 修改 `corelib/configfile/claude_onboarding.go` 的 EnsureClaudeOnboarding()，在 ensureClaudeStopHook() 调用之后添加 EnsureClaudeSecurityHook() 调用
    - 传入 home 目录、maclaw-tool 二进制路径、tag 和 logFn
    - 失败时记录警告日志但不阻塞 onboarding 流程
    - _需求: 5.1, 5.7_

  - [ ]* 8.3 编写 Property 7 的属性测试：Hook 配置注入幂等性
    - **Property 7: Hook 配置注入幂等性**
    - 连续两次调用 EnsureClaudeSecurityHook() 后，maclaw-security.json 内容应与第一次调用后完全相同
    - **验证: 需求 5.4**

  - [ ]* 8.4 编写 Property 11 的属性测试：Hook 配置包含正确的命令定义
    - **Property 11: Hook 配置包含正确的命令定义**
    - 生成的 maclaw-security.json 应包含 PreToolUse 调用 security-check 和 PostToolUse 调用 audit-record
    - **验证: 需求 5.2, 5.3**

  - [ ]* 8.5 编写 Property 12 的属性测试：Hook 注入不影响 Stop hook
    - **Property 12: Hook 注入不影响 Stop hook**
    - 已存在 stop.json 或 maclaw-stop.json 时，调用 EnsureClaudeSecurityHook() 后原有文件内容不变
    - **验证: 需求 5.5**

  - [ ]* 8.6 编写 HookConfigInjector 单元测试
    - 测试首次注入创建文件
    - 测试已存在标记时跳过写入
    - 测试 hooks 目录不存在时自动创建
    - _需求: 5.1, 5.4, 5.5_

- [ ] 9. 实现 Claude Code Hook_Input 格式转换
  - [x] 9.1 在 `cmd/maclaw-tool/security_check.go` 中添加 Claude Code Hook_Input 到 SecurityCheckRequest 的格式转换辅助函数
    - 实现 convertClaudeHookInput(raw json.RawMessage) (*SecurityCheckRequest, error)
    - 将 Claude Code 的 hook_type 忽略、tool_name/tool_input/session_id 直接透传
    - 在 runSecurityCheck 中，如果 stdin JSON 不符合 SecurityCheckRequest 格式，尝试作为 Claude Code Hook_Input 解析并转换
    - _需求: 9.1, 9.2, 9.3, 9.4_

  - [ ]* 9.2 编写 Property 13 的属性测试：格式转换正确性
    - **Property 13: Claude Code Hook_Input 到 SecurityCheckRequest 的格式转换**
    - 合法的 Claude Code Hook_Input 转换后 tool_name/tool_input/session_id 字段值与原始输入一致
    - **验证: 需求 9.2, 9.3**

- [ ] 10. 扩展 AuditEntry 支持新字段
  - [x] 10.1 在 `corelib/security/types.go` 的 AuditEntry 结构体中添加扩展字段
    - 添加 Source string `json:"source,omitempty"` 字段
    - 添加 SensitiveDetected bool `json:"sensitive_detected,omitempty"` 字段
    - 添加 SensitiveCategories []string `json:"sensitive_categories,omitempty"` 字段
    - 添加 OutputSnippet string `json:"output_snippet,omitempty"` 字段
    - 确保新字段使用 omitempty 标签，不影响现有审计日志的序列化/反序列化
    - _需求: 2.2, 2.3, 8.3_

- [x] 11. 最终检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户确认。

## 说明

- 标记 `*` 的任务为可选测试任务，可跳过以加速 MVP 交付
- 每个任务引用了对应的需求编号，确保需求全覆盖
- 属性测试验证设计文档中的 15 个正确性属性
- 检查点确保增量验证，避免问题累积
- 所有新文件均在现有包结构内，不引入新的外部依赖
