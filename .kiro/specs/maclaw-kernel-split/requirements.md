# 需求文档：MaClaw 内核拆分三层架构

## 简介

将 MaClaw 从当前的单体 Wails 桌面应用拆分为三层架构：CoreLib（内核库）、GUI（Wails 桌面端）、TUI/CLI（终端交互端）。目标是让核心逻辑脱离 GUI 依赖，能够在无 UI 的 Linux 服务器和嵌入式系统上独立运行，同时保持现有 GUI 功能不变，并新增基于 Bubble Tea 的终端交互界面。

## 术语表

- **CoreLib**：MaClaw 内核库，包含所有与 UI 无关的核心业务逻辑，编译为独立的 Go package（`corelib/`），可被 GUI 和 TUI/CLI 共同引用
- **GUI**：基于 Wails v2 的桌面图形界面层，即当前的 MaClaw 桌面客户端
- **TUI**：基于 Bubble Tea 框架的终端交互界面，提供富文本终端 UI 体验
- **CLI**：纯命令行模式，适用于脚本自动化和嵌入式环境，无交互式 UI
- **App_Struct**：当前根目录 `package main` 中的 `App` 结构体，包含所有子系统的引用
- **Agent_Loop**：IM 消息处理与 LLM 交互的核心循环逻辑（当前在 `im_message_handler.go` 中）
- **Session_Manager**：远程编程会话的生命周期管理器（当前在 `remote_session_manager.go` 中）
- **Hub_Client**：与 Hub 服务器的 WebSocket 通信客户端（当前在 `remote_hub_client.go` 中）
- **Tool_System**：工具注册、路由、选择、定义生成等子系统的统称
- **Security_System**：安全防火墙、策略引擎、审计日志、风险评估等子系统的统称
- **Kernel_API**：CoreLib 对外暴露的公开接口层，GUI 和 TUI/CLI 通过该接口调用内核功能
- **Event_Emitter**：CoreLib 中的事件通知机制，替代当前对 Wails runtime.EventsEmit 的直接调用
- **Wails_Binding**：当前 `app_wails_bindings.go` 中通过 Wails 框架暴露给前端的方法

## 需求

### 需求 1：CoreLib 内核库提取

**用户故事：** 作为开发者，我想将核心业务逻辑提取为独立的 Go package，以便在无 GUI 环境下复用内核能力。

#### 验收标准

1. THE CoreLib SHALL 作为独立的 Go package 存在于 `corelib/` 目录下，拥有独立的 package 声明（非 `package main`）
2. THE CoreLib SHALL 包含以下子系统：Session_Manager、Hub_Client、Agent_Loop、Tool_System、Security_System、Memory 存储、配置管理、Swarm 编排、MCP 自动发现、ClawNet 客户端、定时任务管理
3. THE CoreLib SHALL 不依赖任何 GUI 框架（包括 `github.com/wailsapp/wails/v2` 和 `github.com/energye/systray`）
4. THE CoreLib SHALL 通过 Kernel_API 暴露公开接口，所有对外方法使用导出的函数签名（首字母大写）
5. THE CoreLib SHALL 通过 Event_Emitter 接口发送状态变更通知，替代当前对 `runtime.EventsEmit` 的直接调用
6. WHEN CoreLib 被 GUI 层引用时，THE CoreLib SHALL 与当前 `App` 结构体提供相同的核心功能
7. WHEN CoreLib 被 TUI/CLI 层引用时，THE CoreLib SHALL 在无图形环境下正常初始化和运行
8. THE CoreLib SHALL 使用 `go build ./corelib/...` 命令在 Linux amd64、Linux arm64、Darwin amd64、Darwin arm64 平台上成功编译，且不产生 GUI 相关的链接错误

### 需求 2：Kernel_API 接口设计

**用户故事：** 作为上层应用（GUI/TUI/CLI）开发者，我想通过统一的 API 调用内核功能，以便不同前端共享相同的业务逻辑。

#### 验收标准

1. THE Kernel_API SHALL 定义为 Go interface 类型，包含会话管理、工具执行、内存查询、配置读写、安全审计等方法分组
2. THE Kernel_API SHALL 提供一个工厂函数 `NewKernel(opts KernelOptions) (*Kernel, error)` 用于初始化内核实例
3. WHEN 调用方传入 KernelOptions 时，THE Kernel_API SHALL 支持通过选项配置数据目录、日志输出、Hub 地址、事件回调等参数
4. THE Kernel_API SHALL 提供 `Shutdown(ctx context.Context) error` 方法用于优雅关闭内核及所有子系统
5. THE Kernel_API SHALL 提供事件订阅方法 `OnEvent(eventType string, handler func(payload interface{}))` 用于上层监听内核状态变更
6. IF 调用方在内核未初始化时调用 Kernel_API 方法，THEN THE Kernel_API SHALL 返回明确的错误信息，包含 "kernel not initialized" 字样

### 需求 3：GUI 层适配

**用户故事：** 作为桌面端用户，我想在架构拆分后继续使用现有的 MaClaw GUI，功能和体验保持不变。

#### 验收标准

1. THE GUI SHALL 位于 `gui/` 目录下，包含 Wails 入口（`main.go`）、Wails 绑定层和前端资源
2. THE GUI SHALL 通过引用 CoreLib 的 Kernel_API 实现所有业务功能，自身不包含核心业务逻辑
3. THE GUI SHALL 保留当前所有 Wails_Binding 方法的函数签名和返回值类型，确保前端 TypeScript 代码无需修改
4. THE GUI SHALL 将 CoreLib 的 Event_Emitter 事件转发为 Wails 的 `runtime.EventsEmit` 调用
5. THE GUI SHALL 保留系统托盘（systray）功能，托盘相关代码仅存在于 GUI 层
6. THE GUI SHALL 保留平台特定代码（`platform_*.go`、`screen_dim_*.go`、`mac_compat_darwin.go`）在 GUI 层
7. WHEN 用户通过 GUI 执行任何现有操作时，THE GUI SHALL 产生与拆分前相同的结果

### 需求 4：TUI 终端交互界面

**用户故事：** 作为在终端环境工作的开发者，我想通过富文本终端界面与 MaClaw 交互，以便在没有桌面环境的服务器上使用完整功能。

#### 验收标准

1. THE TUI SHALL 位于 `tui/` 目录下，使用 Bubble Tea 框架实现终端交互界面
2. THE TUI SHALL 通过引用 CoreLib 的 Kernel_API 实现所有业务功能
3. THE TUI SHALL 提供以下核心视图：会话列表、会话详情（实时输出流）、工具状态、配置管理
4. THE TUI SHALL 支持键盘快捷键导航（如 Tab 切换面板、q 退出、Enter 确认）
5. WHEN 用户在 TUI 中发起编程会话时，THE TUI SHALL 实时显示 Agent_Loop 的输出流
6. WHEN CoreLib 通过 Event_Emitter 发送状态变更时，THE TUI SHALL 在界面中实时更新对应的视图组件
7. THE TUI SHALL 支持通过 `--no-tui` 参数退化为纯 CLI 模式（无交互式界面，仅标准输入输出）

### 需求 5：CLI 纯命令行模式

**用户故事：** 作为运维工程师或自动化脚本编写者，我想通过纯命令行方式调用 MaClaw 功能，以便集成到 CI/CD 流水线和嵌入式系统中。

#### 验收标准

1. THE CLI SHALL 作为 TUI 二进制的子命令模式存在，通过 `maclaw-tui --no-tui <command>` 或 `maclaw-tui <command> --batch` 触发
2. THE CLI SHALL 支持以下子命令：`session list`、`session start`、`session attach`、`session kill`、`config get`、`config set`
3. THE CLI SHALL 所有输出支持 JSON 格式（通过 `--json` 参数），便于脚本解析
4. THE CLI SHALL 通过标准退出码表示执行结果（0 表示成功，1 表示一般错误，2 表示参数错误）
5. WHEN CLI 在无 TTY 环境下运行时，THE CLI SHALL 自动禁用颜色输出和交互式提示
6. THE CLI SHALL 支持通过环境变量 `MACLAW_HUB_URL` 和 `MACLAW_TOKEN` 配置连接参数，替代交互式输入
7. IF CLI 无法连接到 Hub 或 CoreLib 初始化失败，THEN THE CLI SHALL 输出包含错误原因的诊断信息到标准错误流，并以非零退出码退出

### 需求 6：目录结构重组

**用户故事：** 作为项目维护者，我想让代码目录结构清晰反映三层架构，以便新成员快速理解项目组织。

#### 验收标准

1. THE 项目 SHALL 采用以下顶层目录结构：`corelib/`（内核库）、`gui/`（Wails GUI）、`tui/`（TUI/CLI）、`hub/`（Hub 服务器，保持不变）、`hubcenter/`（管理中心，保持不变）、`frontend/`（移至 `gui/frontend/`）
2. THE `corelib/` 目录 SHALL 按功能模块组织子包：`session/`、`agent/`、`tool/`、`memory/`、`security/`、`config/`、`swarm/`、`mcp/`、`clawnet/`、`scheduler/`
3. THE `gui/` 目录 SHALL 包含：`main.go`（Wails 入口）、`bindings.go`（Wails 绑定适配层）、`frontend/`（React 前端资源）、平台特定文件
4. THE `tui/` 目录 SHALL 包含：`main.go`（TUI/CLI 入口）、`views/`（Bubble Tea 视图组件）、`commands/`（CLI 子命令）
5. THE Go module 路径 SHALL 保持为 `github.com/RapidAI/CodeClaw`，所有内部引用使用 `github.com/RapidAI/CodeClaw/corelib/...` 路径
6. WHEN 执行 `go build ./...` 时，THE 项目 SHALL 在根目录下成功编译所有子包（不含 GUI 的 CGO 依赖平台除外）
7. THE 项目根目录 SHALL 不包含任何程序源码文件（`.go` 源文件），仅保留编译脚本（如 `Makefile`、`build_installer.bat`、`deploy_all.cmd`）、项目配置文件（如 `go.mod`、`go.sum`、`wails.json`、`.gitignore`）和必要文档（如 `README.md`、`docs/`）
8. THE 当前根目录下的所有 `.go` 源文件 SHALL 全部迁移至对应的子目录中（核心逻辑迁入 `corelib/`，GUI 相关迁入 `gui/`），根目录不再作为任何 Go package 的源码目录
9. THE 以下已有顶层目录 SHALL 保持原位不变，不参与本次架构拆分：
   - `mobile/`（移动端资源：Android/iOS/PWA，非 Go 代码）
   - `openclaw-bridge/`（TypeScript 桥接服务，独立 Node.js 项目）
   - `conductor/`（产品规划与工作流文档）
   - `site/`（网站开发计划文档）
   - `docs/`（项目文档）
   - `build/`（构建脚本、安装包资源、平台签名文件）
   - `testdata/`（测试数据）
   - `dist/`（编译产物，已在 .gitignore 中）
10. THE `internal/systray/` 目录 SHALL 迁移至 `gui/internal/systray/`，因为 systray 是 GUI 层专属依赖
11. THE `build/` 目录中引用根目录 `.go` 文件或 `frontend/` 路径的脚本 SHALL 更新为迁移后的新路径（如 `gui/frontend/`）

### 需求 7：依赖隔离

**用户故事：** 作为开发者，我想确保 CoreLib 不引入 GUI 依赖，以便在嵌入式和无头服务器上轻量部署。

#### 验收标准

1. THE CoreLib SHALL 的 import 列表中不包含 `github.com/wailsapp/wails/v2` 的任何子包
2. THE CoreLib SHALL 的 import 列表中不包含 `github.com/energye/systray` 包
3. THE CoreLib SHALL 不使用任何需要 CGO 的 GUI 相关库（如 X11、Cocoa、Win32 GUI API）
4. THE CoreLib SHALL 在设置 `CGO_ENABLED=0` 的条件下成功编译（SQLite 使用 `modernc.org/sqlite` 纯 Go 实现）
5. WHEN 在 Linux arm64（嵌入式目标平台）上交叉编译 CoreLib 时，THE 编译过程 SHALL 成功完成且产出的二进制文件可正常运行
6. THE GUI 层 SHALL 是唯一引用 `github.com/wailsapp/wails/v2` 的模块

### 需求 8：现有 CLI 客户端重命名与兼容

**用户故事：** 作为现有 `maclaw-cli` 的用户，我想在架构迁移后继续使用现有的 CLI 命令（重命名为 `maclaw-tool`），以便平滑过渡到新架构。

#### 验收标准

1. THE 项目 SHALL 将 `cmd/maclaw-cli/` 重命名为 `cmd/maclaw-tool/`，保留其现有功能作为轻量级 Hub 客户端
2. THE `cmd/maclaw-tool` SHALL 继续支持 `session list`、`session start`、`session attach`、`session kill` 命令
3. THE 新 TUI/CLI（`tui/`）SHALL 提供 `cmd/maclaw-tool` 的所有功能的超集
4. WHEN 用户从 `maclaw-tool` 迁移到新 TUI/CLI 时，THE 命令参数格式 SHALL 保持兼容（相同的 flag 名称和语义）

### 需求 9：Event_Emitter 事件机制

**用户故事：** 作为上层应用开发者，我想通过统一的事件机制接收内核状态变更，以便 GUI 和 TUI 各自用适合的方式展示状态。

#### 验收标准

1. THE Event_Emitter SHALL 定义为 Go interface，包含 `Emit(eventType string, payload interface{})` 方法
2. THE CoreLib SHALL 在以下场景触发事件：会话状态变更、工具执行进度、Hub 连接状态变化、安全告警、后台任务进度
3. THE Event_Emitter SHALL 支持多个监听器同时订阅同一事件类型
4. THE Event_Emitter SHALL 保证事件分发不阻塞内核主逻辑（使用异步分发或带缓冲的 channel）
5. IF 事件监听器执行时发生 panic，THEN THE Event_Emitter SHALL 捕获该 panic 并记录日志，不影响其他监听器和内核运行
6. THE Event_Emitter SHALL 提供默认的空实现（NoopEmitter），用于不需要事件通知的场景（如纯 CLI 批处理模式）

### 需求 10：日志抽象层

**用户故事：** 作为开发者，我想让 CoreLib 的日志输出可配置，以便 GUI 写入文件、TUI 显示在状态栏、CLI 输出到 stderr。

#### 验收标准

1. THE CoreLib SHALL 定义日志接口 `Logger`，包含 `Debug`、`Info`、`Warn`、`Error` 四个级别的方法
2. THE CoreLib SHALL 通过 KernelOptions 接受外部传入的 Logger 实现
3. IF 调用方未提供 Logger 实现，THEN THE CoreLib SHALL 使用默认的标准库 `log` 输出到 stderr
4. THE GUI SHALL 提供将日志写入文件并通过 Wails 事件推送到前端的 Logger 实现
5. THE TUI SHALL 提供将日志显示在 TUI 状态栏区域的 Logger 实现

### 需求 11：平台能力适配与无头环境降级

**用户故事：** 作为在无 GUI 的 Linux 服务器上运行 MaClaw 的用户，我想让依赖图形环境的功能自动降级或提供替代方案，以便系统在无头环境下稳定运行而不会因缺少显示服务器而崩溃。

#### 验收标准

1. THE CoreLib SHALL 定义 `PlatformCapabilities` 接口，用于检测当前运行环境的能力（是否有显示服务器、是否有剪贴板、是否支持系统通知等）
2. THE CoreLib SHALL 在初始化时自动检测运行环境能力，并将结果存储在 `PlatformCapabilities` 实例中
3. WHEN 截图工具（screenshot）在无显示服务器的 Linux 环境下被调用时，THE CoreLib SHALL 返回明确的错误信息说明该功能不可用，而非崩溃或 panic
4. WHEN 屏幕亮度调节（screen_dim）在无 GUI 环境下被调用时，THE CoreLib SHALL 静默跳过该操作并记录日志
5. WHEN 剪贴板操作在无 GUI 环境下被调用时，THE CoreLib SHALL 提供基于文件的降级方案（如写入临时文件）
6. THE Tool_System SHALL 在工具注册时标注每个工具的平台依赖（如 `requires_display`、`requires_clipboard`），并在无头环境下自动过滤不可用的工具
7. THE CoreLib SHALL 提供 `IsHeadless() bool` 方法，供上层应用判断当前是否运行在无头环境中
8. WHEN Agent_Loop 在无头环境下选择工具时，THE Agent_Loop SHALL 自动排除标记为需要图形环境的工具，避免执行必然失败的操作

### 需求 12：TUI 模式下的 CLI 工具启动策略

**用户故事：** 作为在终端中使用 MaClaw TUI 的开发者，我想在 TUI 中启动 Claude Code 等本身就是 CLI/TUI 的编程工具时，工具能直接在当前终端中运行，而不是尝试弹出新的终端窗口。

#### 验收标准

1. WHEN 用户在 TUI 交互模式下启动 CLI/TUI 类工具（如 Claude Code、Codex CLI、Gemini CLI）时，THE TUI SHALL 暂挂 Bubble Tea 界面（释放终端控制权），在当前终端前台执行工具进程，工具退出后恢复 TUI 界面
2. THE TUI SHALL 使用 Bubble Tea 的 `ReleaseTerminal()` / `RestoreTerminal()`（或等效的 `tea.ExecProcess`）机制实现终端控制权的交接，确保工具进程能正常接收键盘输入和渲染自身 TUI
3. WHEN 用户在 CLI 批处理模式（`--no-tui`）下启动工具时，THE CLI SHALL 使用工具的 headless 模式（如 Claude Code 的 `--print` 参数），将工具输出流式传输到标准输出
4. WHEN 运行环境无 TTY（如 CI/CD 管道、cron 任务）时，THE CoreLib SHALL 仅支持 headless 模式启动工具，并在工具不支持 headless 模式时返回明确的错误信息
5. THE CoreLib SHALL 定义 `ToolLaunchMode` 枚举（`Interactive`、`Headless`），并在 Kernel API 中提供 `LaunchTool(name string, mode ToolLaunchMode, opts LaunchOptions) error` 方法
6. THE GUI 层 SHALL 继续使用当前的终端窗口弹出方式启动工具（行为不变）
7. WHEN 工具进程在 TUI 前台执行期间被用户中断（Ctrl+C）时，THE TUI SHALL 正确恢复界面状态，不出现终端渲染异常

### 需求 13：Daemon 守护进程模式

**用户故事：** 作为在无头 Linux 服务器上部署 MaClaw 的运维工程师，我想让 Kernel 以守护进程方式长驻运行，持续接收 Hub 任务派发、执行定时任务、参与 ClawNet 网络，无需任何 UI 界面。

#### 验收标准

1. THE Kernel SHALL 提供 `Run(ctx context.Context) error` 方法，该方法阻塞运行内核事件循环（Hub WebSocket 连接、定时任务调度、ClawNet 自动拾取等），直到 ctx 被取消或收到终止信号
2. THE TUI 二进制 SHALL 支持 `maclaw-tui daemon` 子命令，启动纯 Kernel 守护进程（不启动 Bubble Tea 界面），日志输出到 stdout/stderr 或指定文件
3. WHEN daemon 进程收到 SIGTERM 或 SIGINT 信号时，THE Kernel SHALL 调用 `Shutdown()` 优雅关闭所有子系统（完成进行中的会话、断开 Hub 连接、保存状态）
4. THE daemon 模式 SHALL 支持通过 `--pid-file <path>` 参数写入 PID 文件，便于 systemd / supervisord 管理进程生命周期
5. THE daemon 模式 SHALL 支持通过 `--log-file <path>` 参数将日志重定向到文件（默认输出到 stderr）
6. WHEN GUI 模式运行时，THE Kernel 的 `Run()` SHALL 在后台 goroutine 中执行，与 Wails 的 UI 事件循环并行运行
7. WHEN TUI 模式运行时，THE Kernel 的 `Run()` SHALL 在后台 goroutine 中执行，与 Bubble Tea 的 UI 事件循环并行运行
8. THE 项目 SHALL 提供示例 systemd unit 文件（`deploy/maclaw-daemon.service`），包含正确的 ExecStart、PIDFile、Restart 配置
