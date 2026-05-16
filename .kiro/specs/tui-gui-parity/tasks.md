# Implementation Plan: TUI/CLI 与 GUI 功能对齐

## Overview

将 TUI Agent 从 6 个工具扩展到 40+ 个工具，补全 CLI 子命令缺失功能，集成安全防火墙和智能路由。所有新增代码仅作为 corelib 模块的薄包装层。实现语言：Go。构建验证：`go build ./tui/...`

## Tasks

- [ ] 1. 扩展 agent_handler.go 结构体与基础设施
  - [ ] 1.1 扩展 TUIAgentHandler 结构体，新增 firewall、defGenerator、router、selector、configMgr、memoryStore、schedulerMgr、clawnetClient、auditLog、maxIterations 字段
    - 修改 `tui/agent_handler.go` 中的 `TUIAgentHandler` struct 定义
    - 更新 `NewTUIAgentHandler()` 构造函数，接受新的依赖参数
    - 从配置读取 maxIterations，未配置则使用默认值 20
    - _Requirements: 11.1, 11.5, 20.3, 21.1_

  - [ ] 1.2 在 executeTool() 入口集成 Firewall 安全检查
    - 在 `executeTool()` 开头调用 `Firewall.Check(toolName, args, ctx)`
    - 拒绝时将拒绝原因作为 tool result 返回给 LLM
    - 所有工具调用记录到 AuditLog
    - Firewall 初始化失败时以无防火墙模式继续运行并记录警告
    - _Requirements: 11.1, 11.2, 11.4, 11.5_

  - [ ] 1.3 实现 onAsk 回调（终端用户确认）
    - TUI 模式下通过 stdin 读取 y/n 实现用户确认
    - 调用 `Firewall.SetOnAsk()` 注册回调
    - _Requirements: 11.3_

  - [ ] 1.4 集成 DefinitionGenerator 和 Router 替换硬编码工具定义
    - 将 `buildToolDefinitions()` 改为使用 `DefinitionGenerator.Generate()` 动态合并 builtin + MCP 工具
    - 在 `RunAgentLoop()` 中，当工具总数 > MaxToolBudget(28) 时调用 `Router.Route(userMessage, allTools)` 裁剪
    - 工具总数 ≤ 28 时直接发送所有工具定义
    - _Requirements: 21.1, 21.2, 21.3_

  - [ ]* 1.5 为 Firewall 集成编写单元测试
    - 测试 Firewall.Check() 拒绝时返回正确原因
    - 测试 onAsk 回调被正确调用
    - 测试无防火墙模式的降级行为
    - _Requirements: 11.1, 11.2, 11.3, 11.5_

- [ ] 2. Checkpoint — 确保基础设施编译通过
  - 运行 `go build ./tui/...` 确保编译通过，如有问题请询问用户。

- [ ] 3. 扩展 Agent 会话管理工具（需求 1）
  - [ ] 3.1 实现 create_session、list_sessions（增强）、get_session_output、get_session_events 工具
    - 在 `tui/agent_handler.go` 的 `buildToolDefinitions()` 中添加工具定义
    - 在 `executeTool()` 中添加对应 case，复用 TUISessionManager
    - create_session 接受 tool、project_path、template_name 参数
    - get_session_output 接受 session_id 和可选 tail_lines 参数
    - _Requirements: 1.1, 1.2, 1.3_

  - [ ] 3.2 实现 interrupt_session、kill_session、send_and_observe、control_session 工具
    - send_and_observe 发送输入后等待 wait_seconds 秒并返回新输出
    - control_session 接受 action（pause/resume/restart）参数
    - _Requirements: 1.4, 1.5, 1.6, 1.7_

- [ ] 4. 扩展 Agent 配置与模板工具（需求 2、3）
  - [ ] 4.1 实现 get_config、update_config、batch_update_config、list_config_schema、export_config、import_config 工具
    - 复用 `corelib/config/Manager` 或 `commands.FileConfigStore`
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

  - [ ] 4.2 实现 create_template、list_templates、launch_template 工具
    - 复用现有模板文件存储逻辑
    - _Requirements: 3.1, 3.2, 3.3_

- [ ] 5. 扩展 Agent 定时任务与记忆工具（需求 4、5）
  - [ ] 5.1 实现 create_scheduled_task、list_scheduled_tasks、delete_scheduled_task、update_scheduled_task 工具
    - 复用 `corelib/scheduler/Manager`
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [ ] 5.2 实现 memory 工具（save/list/search/delete 四种 action）
    - 复用 `corelib/memory/Store`
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

- [ ] 6. 扩展 Agent MCP 与技能工具（需求 6、7）
  - [ ] 6.1 实现 list_mcp_tools、call_mcp_tool 工具
    - list_mcp_tools 返回所有已配置 MCP 服务器及其工具列表
    - call_mcp_tool 接受 server_id、tool_name、arguments 参数
    - _Requirements: 6.1, 6.2_

  - [ ] 6.2 实现 list_skills、search_skill_hub、install_skill_hub、run_skill 工具
    - 复用本地技能列表和 SkillHub HTTP API
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [ ] 7. 扩展 Agent ClawNet、审计、实用工具（需求 8、9、10）
  - [ ] 7.1 实现 clawnet_search、clawnet_publish 工具
    - 复用 `clawnet.Client.SearchKnowledge()` 和 `PublishKnowledge()`
    - _Requirements: 8.1, 8.2_

  - [ ] 7.2 实现 query_audit_log 工具
    - 复用 `security.AuditLog.Query()`，接受 tool_name、risk_level、start_date、end_date 过滤参数
    - _Requirements: 9.1_

  - [ ] 7.3 实现 send_file、parallel_execute、switch_llm_provider、set_max_iterations、recommend_tool、screenshot 工具
    - send_file 读取文件内容发送到指定会话
    - parallel_execute 使用 goroutine fan-out 并发执行
    - switch_llm_provider 复用本地配置切换
    - set_max_iterations 更新 Agent 循环上限
    - recommend_tool 复用 `tool.Selector.Recommend()`
    - screenshot 复用 `corelib/remote/screenshot.go`
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

- [ ] 8. Checkpoint — Agent 工具扩展完成
  - 运行 `go build ./tui/...` 确保编译通过，如有问题请询问用户。

- [ ] 9. 扩展 app.go 初始化（需求 11、21、22）
  - [ ] 9.1 在 initKernel() 中初始化 Firewall、SessionMonitor、DefinitionGenerator、Router
    - 创建 RiskAnalyzer、PolicyEngine、AuditLog 实例并组合为 Firewall
    - 创建 SessionMonitor（statusCh + 20s 轮询间隔）
    - 创建 DefinitionGenerator（builtinDefs + mcpProvider）和 Router
    - 将新组件传递给 TUIAgentHandler
    - _Requirements: 11.1, 11.5, 21.1, 22.1_

  - [ ] 9.2 集成 SessionMonitor 状态通知到 TUI
    - 启动 goroutine 监听 statusCh，将 StatusEvent 转发为 Bubble Tea 消息
    - 会话创建后调用 SessionMonitor.StartWatching()
    - 会话终止/关闭时调用 StopWatching()
    - 退出时调用 SessionMonitor.Close()
    - _Requirements: 22.1, 22.2, 22.3, 22.4_

- [ ] 10. 更新 status_bar.go 显示会话监控通知
  - 在 `tui/views/status_bar.go` 中添加 SessionMonitor 状态变更消息的显示
  - 当会话从 busy 变为 waiting_input 或 exited 时在状态栏显示通知
  - _Requirements: 22.2_

- [ ] 11. Checkpoint — TUI 核心集成完成
  - 运行 `go build ./tui/...` 确保编译通过，如有问题请询问用户。

- [x] 12. CLI ClawNet 任务生命周期扩展（需求 12）
  - [x] 12.1 在 clawnet.go 中扩展 tasks 子命令，新增 bid、assign、claim、submit、approve、reject、cancel 操作
    - 修改 `tui/commands/clawnet.go` 中的 `clawnetTasks()` 函数
    - 每个操作复用 `clawnet.Client` 对应方法
    - _Requirements: 12.1, 12.2, 12.3_

  - [x] 12.2 新增 tasks board、tasks submissions、tasks pick-winner 子命令
    - board 调用 `GetTaskBoard()` 显示任务看板
    - submissions 调用 `GetTaskSubmissions()` 显示提交列表
    - pick-winner 调用 `PickTaskWinner()` 选择获胜者
    - _Requirements: 12.4, 12.5, 12.6_

- [x] 13. CLI ClawNet 身份、排行榜、auto-picker、daemon、binary、profile、nutshell 扩展（需求 13-16）
  - [x] 13.1 新增 identity 子命令（has-identity/export-identity/import-identity/backup-key/restore-key）
    - 复用 `clawnetIdentityKeyPath()` 和文件操作逻辑（参考 gui/app_clawnet.go 模式）
    - _Requirements: 13.1_

  - [x] 13.2 新增 leaderboard、transactions、credits-audit 子命令
    - 复用 `ClawNet_Client.GetLeaderboard()`、`GetCreditsTransactions()`、`GetCreditsAudit()`
    - _Requirements: 13.2, 13.3, 13.4_

  - [x] 13.3 新增 auto-picker 子命令（status/configure/trigger）
    - 复用 `clawnet.AutoTaskPicker`
    - status 调用 `GetStatus()`
    - configure 接受 --enabled、--poll-minutes、--min-reward、--tags 参数
    - trigger 接受 --task 参数调用 `PickAndExecuteTask()`
    - _Requirements: 14.1, 14.2, 14.3_

  - [x] 13.4 新增 daemon 子命令（ensure/stop/info）和 binary 子命令（install/update/path）
    - daemon ensure 调用 `EnsureDaemon()`
    - daemon stop 调用 `StopDaemon()`
    - daemon info 显示 PID 和运行状态
    - binary install 调用 `clawnet.Download()`
    - binary update 调用 `SelfUpdate()`
    - binary path 显示二进制路径
    - _Requirements: 15.1, 15.2, 15.3, 15.4, 15.5, 15.6_

  - [x] 13.5 新增 profile 子命令（get/update/set-motto）
    - 复用 `ClawNet_Client.GetProfile()`、`UpdateProfile()`、`SetMotto()`
    - _Requirements: 16.1, 16.2, 16.3_

- [x] 14. CLI MCP 完整功能扩展（需求 17）
  - [x] 14.1 在 mcp.go 中新增 health-check、tools、call-tool 子命令
    - health-check 检查所有已配置 MCP 服务器的健康状态
    - tools 列出所有 MCP 服务器提供的工具及参数描述
    - call-tool 接受 --server、--tool、--args 参数调用指定工具
    - _Requirements: 17.1, 17.2, 17.3_

- [x] 15. CLI NLSkill execute、SkillHub check-updates/update、LLM iterations 扩展（需求 18-20）
  - [x] 15.1 在 nlskill.go 中新增 execute 子命令
    - 查找匹配的 NL 技能并按步骤执行 Steps
    - 技能不存在或 disabled 时返回描述性错误
    - 步骤失败且 on_error 为 "stop" 时停止执行
    - _Requirements: 18.1, 18.2, 18.3_

  - [x] 15.2 在 skillhub.go 中新增 check-updates 和 update 子命令
    - check-updates 对比本地 Hub 技能版本与远程最新版本
    - update 从 SkillHub 下载最新版本并替换本地技能
    - _Requirements: 19.1, 19.2_

  - [x] 15.3 在 llm.go 中新增 set-max-iterations 和 get-max-iterations 子命令
    - set-max-iterations 将值保存到本地配置
    - get-max-iterations 从配置读取并显示
    - _Requirements: 20.1, 20.2_

- [x] 16. 更新 main.go 帮助文本
  - 更新 `tui/main.go` 中 `printUsage()` 的帮助文本，反映所有新增子命令
  - 确保 clawnet 帮助文本包含 identity、leaderboard、transactions、credits-audit、auto-picker、daemon、binary、profile
  - 确保 mcp 帮助文本包含 health-check、tools、call-tool
  - 确保 nlskill 帮助文本包含 execute
  - 确保 skillhub 帮助文本包含 check-updates、update
  - 确保 llm 帮助文本包含 set-max-iterations、get-max-iterations

- [x] 17. Final checkpoint — 全部编译通过
  - 运行 `go build ./tui/...` 确保编译通过，如有问题请询问用户。

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 所有新增工具仅作为 corelib 模块的薄包装层，禁止在 tui/ 中重复实现业务逻辑
- 每个 task 引用了具体的需求编号以确保可追溯性
- Checkpoints 确保增量验证，避免大量代码积累后才发现编译问题
