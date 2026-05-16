# 任务清单：MaClaw 内核拆分三层架构

## 阶段 1：基础框架搭建

### 任务 1.1：创建 CoreLib 核心接口文件
- [x] 创建 `corelib/logger.go`：定义 `Logger` 接口（Debug/Info/Warn/Error）和 `DefaultLogger` 实现（输出到 stderr）
- [x] 创建 `corelib/event_emitter.go`：定义 `EventEmitter` 接口（Emit/Subscribe）、`EventHandler` 类型、`ChannelEmitter` 默认实现（带缓冲 channel 异步分发、panic 捕获）、`NoopEmitter` 空实现
- [x] 创建 `corelib/platform.go`：定义 `PlatformCapabilities` 结构体（HasDisplay/HasClipboard/HasNotify）和 `DetectPlatform()` 函数（Linux 检查 DISPLAY/WAYLAND_DISPLAY，Windows/macOS 默认有 GUI）
- [x] 创建 `corelib/kernel_options.go`：定义 `KernelOptions` 结构体（DataDir、HubURL、HubToken、MachineID、Logger、EventEmitter、PlatformOverride、ConfigPath、AgentMaxIterations、ToolLauncher）
- [x] 创建 `corelib/kernel.go`：定义 `Kernel` 结构体骨架、`NewKernel()` 工厂函数、`Shutdown()` 方法、`IsHeadless()` 方法、`OnEvent()` 方法
- [x] 验证：`go build ./corelib/...` 编译通过

### 任务 1.2：创建 CoreLib 子包骨架
- [x] 创建以下子包目录，每个包含一个占位 `doc.go` 文件（仅 package 声明和简要注释）：`corelib/session/`、`corelib/agent/`、`corelib/tool/`、`corelib/memory/`、`corelib/security/`、`corelib/config/`、`corelib/swarm/`、`corelib/mcp/`、`corelib/clawnet/`、`corelib/scheduler/`、`corelib/remote/`、`corelib/skill/`、`corelib/misc/`
- [x] 验证：`go build ./corelib/...` 编译通过（所有子包）

### 任务 1.3：创建 ToolLauncher 接口和 Kernel.Run() 骨架
- [x] 在 `corelib/tool/launch.go` 中定义 `ToolLaunchMode` 枚举（Interactive/Headless）、`LaunchOptions` 结构体、`ToolLauncher` 接口（Launch/SupportsMode）
- [x] 在 `corelib/kernel.go` 中添加 `Run(ctx context.Context) error` 方法骨架（使用 errgroup，当前仅 `<-ctx.Done()` 阻塞占位）
- [x] 验证：`go build ./corelib/...` 编译通过

## 阶段 2：CoreLib 子系统提取

> 每个任务迁移一组相关文件。迁移后确保 `go build ./...` 通过。
> 迁移顺序按依赖关系从底层到上层。测试文件跟随源文件一起迁移。

### 任务 2.1：提取公共类型定义
- [x] `common.go` 中的公共类型 → `corelib/types.go`（ModelConfig、ProjectConfig、ToolConfig、AppConfig 等）+ `corelib/app_config.go`
- [x] `remote_types.go` → `corelib/remote/types.go`；`remote_event_types.go` → `corelib/remote/event_types.go`
- [x] `swarm_types.go` + test → `corelib/swarm/types.go`
- [x] `remote_codex_types.go` + test → `corelib/remote/codex_types.go`
- [x] `remote_sdk_types.go` + test → `corelib/remote/sdk_types.go`
- [x] 更新根目录中引用这些类型的 import 路径
- [x] 验证：`go build ./...` 编译通过

### 任务 2.2：提取配置管理子系统
- [x] `config_manager.go` → `corelib/config/manager.go`，将 `*App` 依赖改为 `ConfigStore` 接口注入
- [x] 创建 `corelib/config/store.go`：定义 `ConfigStore` 接口（LoadConfig/SaveConfig）
- [x] 导出 `MaxAgentIterationsCap` 常量到 `corelib/config` 包
- [x] 更新根目录中引用 ConfigManager 的 import 路径
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.3：提取记忆子系统
- [x] `memory_store.go` → `corelib/memory/store.go`（自包含，无 `*App` 依赖）
- [x] `memory_compressor.go` → `corelib/memory/compressor.go`（通过 `LLMChatCaller` 接口解耦 `*App` 和 `doSimpleLLMRequest`）
- [x] `conversation_archiver.go` → `corelib/memory/archiver.go`（通过 `LLMSummarizer` 接口解耦）
- [x] 创建 `corelib/memory/types.go`：Entry、Category、BackupInfo、CompressResult、CompressorStatus
- [x] 更新根目录中引用这些类型的 import 路径
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.4：提取安全子系统
- [x] `security_firewall.go` → `corelib/security/firewall.go`
- [x] `security_risk_analyzer.go` → `corelib/security/risk_analyzer.go`
- [x] `risk_assessor.go` → `corelib/security/risk_assessor.go`
- [x] `policy_engine.go` → `corelib/security/policy_engine.go`
- [x] `audit_log.go` → `corelib/security/audit_log.go`
- [x] `llm_security_review.go` → `corelib/security/llm_review.go`（通过 `LLMSecurityCaller` 接口解耦）
- [x] 创建 `corelib/security/types.go`：RiskLevel、RiskContext、RiskAssessment、PolicyAction、PolicyRule、AuditEntry 等
- [ ] `remote_permission.go` → 留在 remote 子系统（与会话管理紧耦合）
- [x] 更新根目录中引用这些类型的 import 路径
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.5：提取工具子系统
- [x] `tool_registry.go` → `corelib/tool/registry.go` + `corelib/tool/types.go`
- [x] `tool_selector.go` → `corelib/tool/selector.go`
- [x] 已有 `corelib/tool/launch.go`（Task 1.3 创建的 ToolLauncher 接口）
- [x] `tool_router.go` → `corelib/tool/router.go`（通过 `SkillRecommender` 接口解耦 SkillHubClient，通过 `DefinitionGenerator` 解耦）
- [x] `tool_builder.go` → `corelib/tool/builder.go`（自包含，依赖 Registry 已在 corelib）
- [x] `tool_craft.go` 纯函数 → `corelib/tool/craft.go`（SaveScript/ExecuteScript/DetectScriptLanguage 等；toolCraftTool 方法留在根目录因依赖 *IMMessageHandler）
- [x] `tool_definition_generator.go` → `corelib/tool/definition.go`（通过 `MCPServerProvider`/`LocalMCPToolProvider` 接口解耦 MCPRegistry/LocalMCPManager）
- [ ] `tool_onboarding.go` + test → 留在根目录（重度 *App 依赖：读写配置文件、备份恢复）
- [ ] `remote_tool_catalog.go` → 已提取纯函数到 `corelib/remote/tool_catalog.go`；*App 方法留在根目录
- [x] `tools_non_code.go` 纯函数 → `corelib/tool/non_code.go`（RunGitCmd/SearchFilesInProject/CheckProjectHealth；registerNonCodeTools 留在根目录因依赖 *App）
- [x] 添加 `CapRequirement` 和 `AvailableTools(platform)` 方法（无头环境过滤 GUI 工具）
- [x] 更新根目录中引用这些类型的 import 路径
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.6：提取远程执行子系统
- [ ] `remote_hub_client.go` + test → 留在根目录（重度 *App 依赖：WebSocket 客户端、会话管理回调）
- [x] `remote_execution_helpers.go` 纯函数 → `corelib/remote/execution_helpers.go`（ResolveExecutablePath/BuildExecCmd/BuildEnvList/ProcessPipes/ReaderCoordinator）
- [x] `remote_execution_strategy.go` → 已在 `corelib/remote/types.go`（ExecutionHandle/ExecutionStrategy 接口）
- [x] 平台特定文件：`corelib/remote/execution_helpers_windows.go`（HideCommandWindow）、`corelib/remote/execution_helpers_other.go`
- [ ] `remote_execution_sdk.go` + test、`remote_execution_codex.go`、`remote_execution_gemini_acp.go`、`remote_execution_iflow.go` → 留在根目录（依赖 *App 的 PTY 会话管理和 buildExecCmd 等根函数）
- [x] `remote_output_pipeline.go` → 已提取到 `corelib/remote/output_pipeline.go`（通过接口解耦）
- [x] `remote_screenshot.go` + 纯函数 → 已提取到 `corelib/remote/screenshot.go`
- [x] `screenshot_native_*.go` → `corelib/remote/screenshot_native_windows.go` + `screenshot_native_other.go`
- [x] `remote_event_coalescer.go` → 已提取到 `corelib/remote/event_coalescer.go`
- [x] `remote_event_extractor.go` → 已提取到 `corelib/remote/event_extractor.go`
- [x] `remote_startup_responder.go` → 已提取到 `corelib/remote/startup_responder.go`（通过 StartupSessionWriter 接口解耦）
- [x] `remote_defaults.go` → 已提取到 `corelib/remote/defaults.go`
- [ ] `remote_status.go` + test → 留在根目录（*App 方法；类型已提取到 status_types.go）
- [ ] `remote_diagnostics.go` + test → 留在根目录（*App 方法；类型已提取到 diagnostics_types.go）
- [x] `remote_activation.go` 类型 → 已提取到 `corelib/remote/activation_types.go`（方法留在根目录）
- [x] `remote_workspace.go` → 已提取到 `corelib/remote/workspace.go`
- [x] `remote_image_helpers.go` → 已提取到 `corelib/remote/image_helpers.go`
- [x] `remote_machine_profile.go` → 已提取到 `corelib/remote/machine_profile.go`
- [x] `remote_mobile_launch.go` 类型 → 已提取到 `corelib/remote/mobile_launch_types.go`（方法留在根目录）
- [x] `remote_mode_hash.go` → 已提取到 `corelib/remote/mode_hash.go`
- [x] `remote_preview_buffer.go` → 已提取到 `corelib/remote/preview_buffer.go`
- [x] `remote_summary_reducer.go` → 已提取到 `corelib/remote/summary_reducer.go`
- [x] `remote_claude_onboarding.go` 纯函数 → 已提取到 `corelib/remote/onboarding_helpers.go`（ensureClaudeOnboardingComplete 方法留在根目录）
- [x] `remote_platform_name.go` → 已提取到 `corelib/remote/platform_name.go`
- [ ] `remote_platform_windows.go`/`remote_platform_other.go` → 留在根目录（依赖 ConPTY/CommandSpec/NewWindowsPTYSession）
- [ ] `remote_pty_windows.go` + test → 留在根目录（ConPTY 实现，依赖 conpty 库和根类型）
- [x] `remote_admin_*.go` → 已提取到 `corelib/remote/admin_windows.go` + `admin_other.go`
- [x] `remote_tool_catalog.go` 纯函数 → 已提取到 `corelib/remote/tool_catalog.go`
- [x] `provider_resolver.go` → 已提取到 `corelib/remote/provider_resolver.go`
- [x] `remote_output_normalize.go` → 已提取到 `corelib/remote/output_normalize.go`
- [ ] `remote_tool_claude.go` 等 provider adapter 文件 → 留在根目录（重度 *App 依赖）
- [ ] 解耦 `*App` 依赖：已通过接口解耦可提取部分；剩余 *App 方法留在根目录待 Phase 3
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.7：提取会话管理子系统
- [ ] `remote_session_manager.go` + 相关测试 → 留在 gui/（重度 *App 依赖：日志、状态发射、provider 工厂、checkpoint）；需引入 `SessionManagerDeps` 接口后才能提取
- [ ] `session_checkpoint.go` + test → 待提取（依赖 `*RemoteSession` 读取字段，需引入 `SessionSnapshot` 接口）
- [x] `session_monitor.go` → `corelib/remote/session_monitor.go`（通过 `SessionProvider` 接口解耦 `*RemoteSessionManager`）
- [x] `session_template.go` → `corelib/remote/session_template.go`（自包含，纯 JSON 持久化）
- [ ] `session_precheck.go`、`session_context_resolver.go` → 留在 gui/（依赖 `*App.LoadConfig()`，需引入 `ConfigProvider` 接口）
- [x] `session_startup_feedback.go` → 待提取（纯监控循环，但依赖 `*RemoteSessionManager` 和 `*SessionCheckpointer`）
- [x] `session_io_relay.go` → `corelib/remote/session_io_relay.go`（自包含，纯 channel 并发）
- [x] `session_stall_detector.go` → `corelib/remote/session_stall_detector.go`（logger 依赖注入，使用 `ExecutionHandle` 接口）
- [x] `session_completion_analyzer.go` → `corelib/remote/session_completion_analyzer.go`（纯语义分析，无 I/O）
- [ ] 解耦 `*App` 依赖：已通过接口解耦可提取部分；剩余 *App 方法留在 gui/
- [x] 验证：`go build ./corelib/...` 编译通过；`go build ./gui/` 编译通过

### 任务 2.8：提取 Agent 循环子系统
- [ ] `im_message_handler.go` + 相关测试 → 留在 gui/（重度 *App 依赖：config/LLM/工具注册/会话管理）；纯函数 `stripThinkingTags` 已提取为 `corelib/agent.StripThinkingTags`
- [x] `agent_loop_context.go` → `corelib/agent/loop_context.go` + `corelib/agent/types.go`（LoopKind、SlotKind、StatusEvent、LoopContext）
- [x] `background_loop_manager.go` → `corelib/agent/background_loop_manager.go`（纯 slot 并发管理）
- [x] `llm_request_helper.go` → `corelib/agent/llm_helper.go`（OpenAI/Anthropic 双协议，通过 `corelib.MaclawLLMConfig` 解耦）
- [ ] 将 `a.emitEvent(...)` 替换为 `k.emitter.Emit(...)` — 待 im_message_handler 提取后实现
- [ ] 在工具选择逻辑中集成 `AvailableTools(platform)` 过滤 — 待 im_message_handler 提取后实现
- [ ] 解耦 `*App` 和 `runtime.EventsEmit` 依赖 — 待 im_message_handler 提取后实现
- [x] 验证：`go build ./corelib/...` 编译通过；`go build ./gui/` 编译通过；Linux 交叉编译通过

### 任务 2.9：提取 Swarm 编排子系统
- [x] `swarm_types.go` → 已提取到 `corelib/swarm/types.go`（Task 2.1）
- [x] `swarm_worktree.go` → `corelib/swarm/worktree.go`（WorktreeManager，本地 git helpers）
- [x] `swarm_conflict.go` → `corelib/swarm/conflict.go`（ConflictDetector，Union-Find 算法）
- [x] `swarm_notifier.go` → `corelib/swarm/notifier.go`（Notifier 接口，DefaultNotifier，NoopNotifier）
- [ ] 其余 swarm 文件（orchestrator、pipeline、pipeline_greenfield、scheduler、splitter、verifier、merge、feedback、reporter、doc_generator、llm、prompts）→ 留在根目录（重度 *App/*RemoteSession 依赖）
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.10：提取 MCP、ClawNet、Scheduler、Skill、Misc 子系统
- [ ] MCP：`mcp_auto_discovery.go` + test → 留在根目录（重度 *App 依赖）；`local_mcp_client.go` + `local_mcp_manager.go` → 留在根目录（依赖 *MCPRegistry 根类型）；MCPRegistry → 留在根目录
- [x] ClawNet：`clawnet_client.go` → `corelib/clawnet/client.go`（含所有类型和方法）；`clawnet_installer.go` → `corelib/clawnet/installer.go`；`clawnet_auto_task_picker.go` → `corelib/clawnet/auto_picker.go`；平台特定 `hide_window_windows.go` + `hide_window_other.go`
- [x] Scheduler：`scheduled_task.go` → `corelib/scheduler/task.go`（含本地 generateID）；`scheduled_task_calendar.go` → `corelib/scheduler/calendar.go`
- [ ] Skill：`skillhub_client.go` → 留在根目录（重度 *App 依赖）；`capability_gap_detector.go` → 留在根目录（重度 *App 依赖）
- [x] Misc（已提取）：`shared_context.go` → `corelib/misc/shared_context.go`；`context_bridge.go` → `corelib/misc/context_bridge.go`（引用 corelib/remote.ImportantEvent）；`task_orchestrator.go` → `corelib/misc/task_orchestrator.go`（通过 SubTaskExecutor 函数类型解耦 *RemoteSessionManager/*ToolSelector）
- [ ] Misc（留在根目录）：`experience_extractor.go`（*App 依赖）、`mdns_scanner.go`（*MCPRegistry 依赖）、`project_scanner.go`（*MCPRegistry 依赖）
- [x] 验证：`go build ./corelib/...` 编译通过；`go build .` 编译通过

### 任务 2.11：完成 Kernel 结构体接线
- [x] 在 `NewKernel()` 中初始化可安全导入的子系统（ToolRegistry、ToolRouter、Scheduler、ClawNet Client、AutoPicker），赋值给 Kernel 导出字段
- [x] 实现 `Kernel.Run()`：errgroup 启动定时任务调度器、ClawNet 自动拾取后台 goroutine
- [x] 实现 `Kernel.Shutdown()`：按逆序关闭 AutoPicker → Scheduler
- [x] 添加 "kernel not initialized" 错误检查
- [x] 验证：`go build ./corelib/...` 通过；`CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 和 `darwin/arm64` 交叉编译通过
- [x] 修复 `screenshot_command_windows.go` 缺少 Darwin/Linux 函数桩导致的跨平台编译问题
- [ ] **已知限制（import cycle）**：`corelib/config`、`corelib/misc` 因循环依赖无法被 kernel.go 导入，需由上层（GUI/TUI）管理。Hub WebSocket、MCP 发现、会话监控等依赖 *App 的后台任务也由上层启动。

## 阶段 3：GUI 层分离

### 任务 3.1：创建 GUI 目录结构和 GUIApp 壳
- [x] 创建 `gui/` 目录结构
- [ ] 创建 `gui/event_bridge.go`：`WailsEventBridge`（EventEmitter → runtime.EventsEmit）— 待从 App 中解耦后实现
- [ ] 创建 `gui/tool_launcher.go`：`GUIToolLauncher`（弹出终端窗口）— 待从 App 中解耦后实现
- [ ] 将 `gui/app.go` 中的 `App` 结构体重构为 `GUIApp`（持有 `*corelib.Kernel` + Wails ctx）— 待 corelib 子系统完全提取后实现

### 任务 3.2：迁移 Wails 入口和绑定层
- [x] 根目录所有 226 个 `.go` 文件（含测试）批量迁移到 `gui/`（保持 `package main`）
- [ ] 后续：将绑定方法从 `App` 委托改为 `GUIApp` + `kernel.XXX` 委托 — 待 corelib 子系统完全提取后实现
- [x] 验证：`go build ./gui/` 编译通过

### 任务 3.3：迁移 GUI 专有文件
- [x] 所有 GUI 专有文件已随批量迁移一起移入 `gui/`（tray_*.go、platform_*.go、screen_dim*.go、screen_permission_*.go、mac_compat_*.go、resources_*.go、remote_smoke.go、android_pwa_shell.go、env_check_api.go）
- [x] `internal/systray/` → `gui/internal/systray/`（系统托盘原生实现）
- [x] 更新 `go.mod` 中 `replace github.com/energye/systray` 路径为 `./gui/internal/systray`
- [x] 删除空的根目录 `internal/` 目录
- [x] 验证：`go build ./gui/` 编译通过

### 任务 3.4：迁移前端资源和清理根目录
- [x] `frontend/` → `gui/frontend/`
- [x] 更新 `wails.json` 中的 frontend 路径（`cd gui/frontend && npm install/build`）
- [x] `gui/main.go` 中的 `//go:embed all:frontend/dist` 路径无需修改（frontend 已在 gui/ 下）
- [x] 复制 `build/` 中被 `//go:embed` 引用的资源到 `gui/build/`（icon.ico、appicon.png、bootstrap.html.tmpl）
- [x] 确认根目录无任何 `.go` 源文件（0 个）
- [x] 验证以下目录保持原位不变：`hub/`、`hubcenter/`、`mobile/`、`openclaw-bridge/`、`conductor/`、`site/`、`docs/`、`build/`、`testdata/`、`dist/`
- [x] 验证：`go build ./...` 通过；`go build ./corelib/...` 通过；Linux 交叉编译通过
- [ ] 验证：`cd gui && wails build` 通过 — 需要 Node.js 环境，待手动验证
- [x] 更新 `build/` 目录中引用根目录 `.go` 文件或 `frontend/` 路径的构建脚本

## 阶段 4：TUI/CLI/Daemon 层实现 + maclaw-tool 重命名

### 任务 4.1：创建 TUI 入口和基础框架
- [x] `go.mod` 添加 Bubble Tea 依赖（bubbletea、lipgloss、bubbles）
- [x] 创建 `tui/main.go`：子命令路由（默认=TUI、`daemon`=守护进程、`session`/`config`=CLI、`--no-tui`=批处理）
- [x] 创建 `tui/app.go`：`TUIApp` 结构体，实现 Bubble Tea Init/Update/View
- [x] 创建 `tui/event_bridge.go`：`BubbleTeaEventBridge`（EventEmitter → tea.Program.Send）
- [x] 创建 `tui/logger.go`：TUI Logger（状态栏输出）
- [x] 创建 `tui/tool_launcher.go`：`TUIToolLauncher`（交互模式 `tea.ExecProcess` 暂挂前台 exec；headless 模式 `--print` 等参数）
- [x] 验证：`go build ./tui/...` 编译通过

### 任务 4.2：实现 TUI 视图组件
- [x] `tui/views/root.go`：根 Model，Tab 切换 + 键盘导航
- [x] `tui/views/session_list.go`：会话列表视图
- [x] `tui/views/session_detail.go`：会话详情（实时输出流）
- [x] `tui/views/tool_status.go`：工具状态视图
- [x] `tui/views/config.go`：配置管理视图
- [x] `tui/views/status_bar.go`：底部状态栏（Hub 连接状态、日志、快捷键提示）
- [x] 验证：TUI 可启动并显示基本界面

### 任务 4.3：实现 Daemon 守护进程模式
- [x] `tui/main.go` 中实现 `daemon` 子命令：NewKernel → 写 PID 文件 → `kernel.Run(ctx)` 阻塞 → 信号处理优雅退出
- [x] 支持 `--pid-file` 和 `--log-file` 参数
- [x] 创建 `deploy/maclaw-daemon.service` systemd unit 文件示例
- [x] 验证：`maclaw-tui daemon` 可启动并保持运行，SIGTERM 优雅退出

### 任务 4.4：实现 CLI 子命令
- [x] `tui/commands/session.go`：`session list`（支持 `--json`）、`session start`、`session attach`、`session kill`
- [x] `tui/commands/config.go`：`config get`、`config set`
- [x] 所有命令支持 `--json` 输出、标准退出码（0/1/2）、无 TTY 自动禁用颜色
- [x] 支持 `MACLAW_HUB_URL` 和 `MACLAW_TOKEN` 环境变量
- [x] 验证：CLI 命令正常执行并返回正确退出码

### 任务 4.5：重命名 maclaw-cli 为 maclaw-tool
- [x] `cmd/maclaw-cli/` → `cmd/maclaw-tool/`
- [x] 更新 usage 信息、编译脚本和文档中的 `maclaw-cli` 引用
- [x] 验证：`go build ./cmd/maclaw-tool/...` 编译通过

## 阶段 5：集成验证和收尾

### 任务 5.1：创建 Makefile 统一编译入口
- [x] 创建根目录 `Makefile`：`check-corelib-deps`（grep 检查无 wails/systray 依赖）、`build-corelib-headless`（CGO_ENABLED=0 交叉编译）、`build-tui`、`build-gui`、`build-tool`、`build-all`、`test`
- [x] 验证：`make build-all` 全部通过

### 任务 5.2：端到端验证
- [ ] GUI 模式：`cd gui && wails dev`，确认所有现有功能正常
- [ ] TUI 模式：`./bin/maclaw-tui`，确认界面可交互、工具可启动（暂挂+前台 exec）
- [ ] Daemon 模式：`./bin/maclaw-tui daemon`，确认 Hub 连接、定时任务、ClawNet 正常长驻运行
- [ ] CLI 模式：`./bin/maclaw-tui --no-tui session list --json`，确认 JSON 输出正确
- [x] Headless 编译：`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./tui/...` 通过
- [ ] maclaw-tool：`./bin/maclaw-tool session list` 功能正常
- [x] 依赖隔离：`make check-corelib-deps` 通过
