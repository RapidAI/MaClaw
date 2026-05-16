# 设计文档：MaClaw 内核拆分三层架构

## 概述

本设计将 MaClaw 从当前根目录 `package main` 的单体结构拆分为三层架构。核心思路是：将 `App` 结构体中与 UI 无关的子系统全部提取到 `corelib/` 包中，形成独立的 `Kernel` 结构体；GUI 层和 TUI 层分别作为 Kernel 的消费者，通过统一的 Kernel API 和 Event Emitter 接口与内核交互。根目录不再包含任何 `.go` 源文件，仅保留编译脚本、配置和文档。

## 架构总览

```
┌─────────────────────────────────────────────────────┐
│                    项目根目录                         │
│  go.mod / Makefile / build scripts / docs           │
│  （无 .go 源文件）                                    │
├─────────┬──────────┬──────────┬──────────┬──────────┤
│ corelib/│  gui/    │  tui/    │  hub/    │ cmd/     │
│ (内核库) │ (Wails) │(BubbleTea)│(不变)   │maclaw-tool│
└────┬────┴────┬─────┴────┬─────┴──────────┴──────────┘
     │         │          │
     │    ┌────┴────┐ ┌───┴────┐
     │    │Wails    │ │Bubble  │
     │    │Bindings │ │Tea UI  │
     │    └────┬────┘ └───┬────┘
     │         │          │
     ▼         ▼          ▼
┌─────────────────────────────────────┐
│         Kernel API (接口层)          │
│  NewKernel() / Shutdown()           │
│  SessionManager / ToolSystem / ...  │
├─────────────────────────────────────┤
│         Event Emitter (事件层)       │
│  Emit() / Subscribe() / OnEvent()   │
├─────────────────────────────────────┤
│         CoreLib 内部子系统            │
│  session / agent / tool / memory    │
│  security / config / swarm / mcp    │
│  clawnet / scheduler / platform     │
└─────────────────────────────────────┘
```

## 目录结构设计

### 最终目录布局

```
github.com/RapidAI/CodeClaw/
├── go.mod
├── go.sum
├── wails.json
├── Makefile                    # 新增：统一编译入口
├── build_installer.bat         # 保留
├── deploy_all.cmd              # 保留
├── README.md / docs/           # 保留
├── .gitignore
│
├── corelib/                    # 内核库（package corelib）
│   ├── kernel.go               # Kernel 结构体 + NewKernel() + Shutdown()
│   ├── kernel_options.go       # KernelOptions 配置
│   ├── event_emitter.go        # EventEmitter 接口 + 默认实现
│   ├── logger.go               # Logger 接口 + 默认实现
│   ├── platform.go             # PlatformCapabilities 接口 + 检测逻辑
│   ├── types.go                # 公共类型定义（AppConfig, ModelConfig 等）
│   ├── session/                # 会话管理
│   │   ├── manager.go          # SessionManager（从 remote_session_manager.go 提取）
│   │   ├── checkpoint.go       # SessionCheckpointer
│   │   ├── monitor.go          # SessionMonitor
│   │   ├── template.go         # SessionTemplateManager
│   │   ├── precheck.go         # SessionPrecheck
│   │   ├── context_resolver.go # SessionContextResolver
│   │   ├── startup_feedback.go # SessionStartupFeedback
│   │   ├── io_relay.go         # SessionIORelay
│   │   ├── stall_detector.go   # SessionStallDetector
│   │   └── completion.go       # SessionCompletionAnalyzer
│   ├── agent/                  # Agent 循环
│   │   ├── loop.go             # Agent Loop 核心（从 im_message_handler.go 提取）
│   │   ├── loop_context.go     # AgentLoopContext
│   │   ├── background.go       # BackgroundLoopManager
│   │   └── llm_helper.go       # LLM 请求辅助
│   ├── tool/                   # 工具系统
│   │   ├── registry.go         # ToolRegistry
│   │   ├── registry_builtin.go # 内置工具注册
│   │   ├── router.go           # ToolRouter
│   │   ├── selector.go         # ToolSelector
│   │   ├── builder.go          # ToolBuilder
│   │   ├── craft.go            # ToolCraft
│   │   ├── definition.go       # ToolDefinitionGenerator
│   │   ├── onboarding.go       # ToolOnboarding
│   │   ├── catalog.go          # RemoteToolCatalog
│   │   ├── non_code.go         # 非代码工具
│   │   └── manager.go          # ToolManager
│   ├── memory/                 # 记忆系统
│   │   ├── store.go            # MemoryStore
│   │   ├── compressor.go       # MemoryCompressor
│   │   └── archiver.go         # ConversationArchiver
│   ├── security/               # 安全系统
│   │   ├── firewall.go         # SecurityFirewall
│   │   ├── risk_analyzer.go    # SecurityRiskAnalyzer
│   │   ├── risk_assessor.go    # RiskAssessor
│   │   ├── policy_engine.go    # PolicyEngine
│   │   ├── audit_log.go        # AuditLog
│   │   ├── llm_review.go       # LLMSecurityReview
│   │   └── permission.go       # RemotePermission
│   ├── config/                 # 配置管理
│   │   └── manager.go          # ConfigManager
│   ├── swarm/                  # Swarm 编排
│   │   ├── orchestrator.go     # SwarmOrchestrator
│   │   ├── pipeline.go         # SwarmPipeline
│   │   ├── scheduler.go        # SwarmAgentScheduler
│   │   ├── splitter.go         # SwarmTaskSplitter
│   │   ├── verifier.go         # SwarmTaskVerifier
│   │   ├── conflict.go         # SwarmConflict
│   │   ├── worktree.go         # SwarmWorktree
│   │   ├── merge.go            # SwarmMerge
│   │   ├── feedback.go         # SwarmFeedback
│   │   ├── notifier.go         # SwarmNotifier
│   │   ├── reporter.go         # SwarmReporter
│   │   ├── doc_generator.go    # SwarmDocGenerator
│   │   ├── llm.go              # SwarmLLM
│   │   ├── prompts.go          # SwarmPrompts
│   │   └── types.go            # Swarm 类型定义
│   ├── mcp/                    # MCP 系统
│   │   ├── auto_discovery.go   # MCPAutoDiscovery
│   │   ├── registry.go         # MCPRegistry
│   │   └── local_client.go     # LocalMCPClient + LocalMCPManager
│   ├── clawnet/                # ClawNet 网络
│   │   ├── client.go           # ClawNetClient
│   │   ├── installer.go        # ClawNetInstaller
│   │   └── auto_picker.go      # ClawNetAutoTaskPicker
│   ├── scheduler/              # 定时任务
│   │   ├── task.go             # ScheduledTask
│   │   └── calendar.go         # ScheduledTaskCalendar
│   ├── remote/                 # 远程执行（与 UI 无关的部分）
│   │   ├── hub_client.go       # RemoteHubClient
│   │   ├── execution_sdk.go    # SDK 执行引擎
│   │   ├── execution_codex.go  # Codex 执行引擎
│   │   ├── execution_gemini.go # Gemini ACP 执行引擎
│   │   ├── execution_helpers.go
│   │   ├── execution_strategy.go
│   │   ├── output_pipeline.go  # RemoteOutputPipeline
│   │   ├── screenshot.go       # 截图（含 headless 降级）
│   │   ├── event_coalescer.go  # RemoteEventCoalescer
│   │   ├── event_extractor.go  # RemoteEventExtractor
│   │   ├── startup_responder.go
│   │   ├── status.go           # RemoteStatus
│   │   ├── types.go            # Remote 类型定义
│   │   ├── defaults.go
│   │   ├── diagnostics.go
│   │   ├── activation.go
│   │   ├── workspace.go
│   │   ├── image_helpers.go
│   │   ├── machine_profile.go
│   │   ├── mobile_launch.go
│   │   ├── mode_hash.go
│   │   ├── preview_buffer.go
│   │   ├── summary_reducer.go
│   │   ├── claude_onboarding.go
│   │   └── platform_name.go
│   ├── skill/                  # 技能系统
│   │   ├── hub_client.go       # SkillHubClient
│   │   ├── executor.go         # SkillExecutor
│   │   ├── backup.go           # SkillBackup
│   │   └── gap_detector.go     # CapabilityGapDetector
│   └── misc/                   # 杂项
│       ├── experience.go       # ExperienceExtractor
│       ├── shared_context.go   # SharedContextStore
│       ├── context_bridge.go   # ContextBridge
│       ├── orchestrator2.go    # TaskOrchestrator2
│       ├── mdns_scanner.go     # MDNSScanner
│       └── project_scanner.go  # ProjectScanner
│
├── gui/                        # Wails GUI 层
│   ├── main.go                 # Wails 入口（从根 main.go 迁移）
│   ├── app.go                  # GUI App 壳：持有 *corelib.Kernel + wails ctx
│   ├── bindings.go             # Wails 绑定适配层（桥接 Kernel API → Wails 方法）
│   ├── bindings_swarm.go       # Swarm 相关绑定
│   ├── bindings_nl.go          # NL MCP/Skills 绑定
│   ├── bindings_llm.go         # MaClaw LLM 绑定
│   ├── event_bridge.go         # EventEmitter → runtime.EventsEmit 桥接
│   ├── tray_darwin.go          # 系统托盘（macOS）
│   ├── tray_linux.go           # 系统托盘（Linux）
│   ├── tray_windows.go         # 系统托盘（Windows）
│   ├── platform_darwin.go      # 平台特定代码
│   ├── platform_linux.go
│   ├── platform_windows.go
│   ├── screen_dim.go           # 屏幕亮度调节
│   ├── screen_dim_darwin.go
│   ├── screen_dim_linux.go
│   ├── screen_dim_windows.go
│   ├── screen_permission_darwin.go
│   ├── screen_permission_other.go
│   ├── mac_compat_darwin.go
│   ├── mac_compat_other.go
│   ├── resources_darwin.go
│   ├── resources_linux.go
│   ├── resources_windows.go
│   ├── remote_smoke.go         # 远程冒烟测试
│   ├── android_pwa_shell.go    # Android PWA 生成
│   ├── env_check_api.go        # 环境检查 API
│   ├── internal/               # GUI 内部包
│   │   └── systray/            # 系统托盘原生实现（从根 internal/systray/ 迁入）
│   └── frontend/               # React 前端（从根 frontend/ 移入）
│       └── ...
│
├── tui/                        # TUI/CLI 层
│   ├── main.go                 # TUI/CLI 入口
│   ├── app.go                  # TUI App：持有 *corelib.Kernel
│   ├── event_bridge.go         # EventEmitter → Bubble Tea Msg 桥接
│   ├── logger.go               # TUI Logger（状态栏输出）
│   ├── views/                  # Bubble Tea 视图组件
│   │   ├── root.go             # 根 Model（Tab 切换）
│   │   ├── session_list.go     # 会话列表视图
│   │   ├── session_detail.go   # 会话详情（实时输出流）
│   │   ├── tool_status.go      # 工具状态视图
│   │   ├── config.go           # 配置管理视图
│   │   └── status_bar.go       # 底部状态栏
│   └── commands/               # CLI 子命令（--no-tui 模式）
│       ├── session.go          # session list/start/attach/kill
│       └── config.go           # config get/set
│
├── cmd/
│   └── maclaw-tool/            # 轻量级 Hub CLI（从 maclaw-cli 重命名）
│       └── main.go
│
├── hub/                        # Hub 服务器（保持不变）
├── hubcenter/                  # 管理中心（保持不变）
├── mobile/                     # 移动端资源：Android/iOS/PWA（保持不变，非 Go 代码）
├── openclaw-bridge/            # TypeScript 桥接服务（保持不变，独立 Node.js 项目）
├── conductor/                  # 产品规划与工作流文档（保持不变）
├── site/                       # 网站开发计划文档（保持不变）
├── docs/                       # 项目文档（保持不变）
├── build/                      # 构建脚本、安装包资源（保持不变，需更新内部路径引用）
├── testdata/                   # 测试数据（保持不变）
└── dist/                       # 编译产物（保持不变，.gitignore）
```


## 核心接口设计

### 1. Kernel 结构体与工厂函数

```go
// corelib/kernel.go
package corelib

import "context"

// Kernel 是 MaClaw 内核的顶层入口，持有所有子系统的引用。
// GUI 和 TUI 各自创建一个 Kernel 实例来驱动业务逻辑。
type Kernel struct {
    opts     KernelOptions
    emitter  EventEmitter
    logger   Logger
    platform PlatformCapabilities

    // 子系统（全部导出，供上层直接访问）
    Sessions   *session.Manager
    Agent      *agent.Loop
    Tools      *tool.Registry
    Memory     *memory.Store
    Security   *security.Suite       // 聚合 Firewall + PolicyEngine + AuditLog + ...
    Config     *config.Manager
    Swarm      *swarm.Orchestrator
    MCP        *mcp.Registry
    ClawNet    *clawnet.Client
    Scheduler  *scheduler.Manager
    Remote     *remote.Manager       // 聚合 HubClient + ExecutionEngines + ...
    Skills     *skill.HubClient

    // 内部状态
    shutdownOnce sync.Once
}

// NewKernel 创建并初始化内核实例。
// 所有子系统在此函数中按依赖顺序初始化。
func NewKernel(opts KernelOptions) (*Kernel, error) {
    // 1. 初始化 Logger（未提供则使用 DefaultLogger）
    // 2. 初始化 EventEmitter（未提供则使用 NoopEmitter）
    // 3. 检测 PlatformCapabilities
    // 4. 按依赖顺序初始化各子系统
    // 5. 返回 Kernel 实例
}

// Shutdown 优雅关闭内核及所有子系统。
func (k *Kernel) Shutdown(ctx context.Context) error { ... }

// IsHeadless 返回当前是否运行在无头环境中。
func (k *Kernel) IsHeadless() bool {
    return !k.platform.HasDisplay()
}

// OnEvent 订阅内核事件。
func (k *Kernel) OnEvent(eventType string, handler EventHandler) {
    k.emitter.Subscribe(eventType, handler)
}
```

### 2. KernelOptions 配置

```go
// corelib/kernel_options.go
package corelib

type KernelOptions struct {
    // 数据目录（配置、数据库、日志等的根目录）
    DataDir     string

    // Hub 连接配置
    HubURL      string
    HubToken    string
    MachineID   string

    // 可选：外部注入的 Logger 实现
    Logger      Logger

    // 可选：外部注入的 EventEmitter 实现
    EventEmitter EventEmitter

    // 可选：覆盖自动检测的平台能力
    PlatformOverride *PlatformCapabilities

    // 配置文件路径（为空则使用 DataDir 下的默认路径）
    ConfigPath  string

    // Agent 最大迭代次数（0=默认12，-1=无限）
    AgentMaxIterations int
}
```

### 3. EventEmitter 接口

```go
// corelib/event_emitter.go
package corelib

// EventHandler 是事件回调函数类型。
type EventHandler func(payload interface{})

// EventEmitter 定义内核事件分发接口。
// GUI 层实现此接口将事件转发为 Wails runtime.EventsEmit 调用。
// TUI 层实现此接口将事件转换为 Bubble Tea 的 Msg 发送到 Program。
type EventEmitter interface {
    // Emit 触发一个事件，payload 为事件数据。
    // 实现必须保证不阻塞调用方（异步分发）。
    Emit(eventType string, payload interface{})

    // Subscribe 注册事件监听器。支持同一事件多个监听器。
    Subscribe(eventType string, handler EventHandler)
}

// ChannelEmitter 是基于 Go channel 的默认 EventEmitter 实现。
// 使用带缓冲的 channel 异步分发事件，监听器 panic 会被捕获并记录日志。
type ChannelEmitter struct { ... }

// NoopEmitter 是空实现，用于纯 CLI 批处理模式。
type NoopEmitter struct{}
func (NoopEmitter) Emit(string, interface{})              {}
func (NoopEmitter) Subscribe(string, EventHandler)        {}
```

### 4. Logger 接口

```go
// corelib/logger.go
package corelib

// Logger 定义内核日志接口。
type Logger interface {
    Debug(msg string, args ...interface{})
    Info(msg string, args ...interface{})
    Warn(msg string, args ...interface{})
    Error(msg string, args ...interface{})
}

// DefaultLogger 使用标准库 log 输出到 stderr。
type DefaultLogger struct{}
```

### 5. PlatformCapabilities 接口

```go
// corelib/platform.go
package corelib

// PlatformCapabilities 描述当前运行环境的能力。
// 在 Kernel 初始化时自动检测，也可通过 KernelOptions.PlatformOverride 覆盖。
type PlatformCapabilities struct {
    hasDisplay    bool   // 是否有显示服务器（X11/Wayland/macOS/Windows）
    hasClipboard  bool   // 是否支持剪贴板
    hasNotify     bool   // 是否支持系统通知
    displayInfo   string // 检测失败时的诊断信息
    osName        string // "linux", "darwin", "windows"
    arch          string // "amd64", "arm64", ...
}

func (p *PlatformCapabilities) HasDisplay() bool    { return p.hasDisplay }
func (p *PlatformCapabilities) HasClipboard() bool  { return p.hasClipboard }
func (p *PlatformCapabilities) HasNotify() bool     { return p.hasNotify }

// DetectPlatform 自动检测当前环境能力。
// Linux 下检查 DISPLAY 和 WAYLAND_DISPLAY 环境变量。
// Windows 和 macOS 默认认为有完整 GUI 能力。
func DetectPlatform() PlatformCapabilities { ... }
```

## GUI 层适配设计

### GUI App 壳

```go
// gui/app.go
package main

import (
    "context"
    "github.com/RapidAI/CodeClaw/corelib"
    "github.com/wailsapp/wails/v2/pkg/runtime"
)

// GUIApp 是 GUI 层的顶层结构体。
// 持有 Kernel 实例和 Wails context，作为 Wails Bind 的目标对象。
type GUIApp struct {
    ctx    context.Context   // Wails context
    kernel *corelib.Kernel
    bridge *WailsEventBridge

    // GUI 专有状态
    IsInitMode  bool
    IsAutoStart bool
    // ... 其他 GUI 专有字段（窗口管理、托盘等）
}

func NewGUIApp() *GUIApp { ... }

func (a *GUIApp) startup(ctx context.Context) {
    a.ctx = ctx
    // 初始化 Kernel
    kernel, err := corelib.NewKernel(corelib.KernelOptions{
        DataDir:      a.getDataDir(),
        Logger:       NewFileLogger(a.getLogPath()),
        EventEmitter: NewWailsEventBridge(ctx),
    })
    if err != nil { ... }
    a.kernel = kernel
}

func (a *GUIApp) shutdown(ctx context.Context) {
    a.kernel.Shutdown(ctx)
}
```

### Wails Event Bridge

```go
// gui/event_bridge.go
package main

// WailsEventBridge 将 CoreLib 的 EventEmitter 事件转发为 Wails runtime.EventsEmit 调用。
// 这样前端 TypeScript 代码无需任何修改，继续通过 EventsOn 监听事件。
type WailsEventBridge struct {
    ctx context.Context
}

func (b *WailsEventBridge) Emit(eventType string, payload interface{}) {
    runtime.EventsEmit(b.ctx, eventType, payload)
}

func (b *WailsEventBridge) Subscribe(eventType string, handler corelib.EventHandler) {
    // GUI 层一般不需要 Go 侧订阅，事件直接推送到前端
    // 如有需要，可维护一个本地 handler 列表
}
```

### Wails 绑定适配层

```go
// gui/bindings.go
package main

// 所有 Wails 绑定方法保持原有签名不变，内部委托给 Kernel API。
// 前端 TypeScript 代码零修改。

func (a *GUIApp) ListMemories(category, keyword string) []corelib.MemoryEntry {
    return a.kernel.Memory.List(category, keyword)
}

func (a *GUIApp) SaveMemory(content, category string, tags []string) error {
    return a.kernel.Memory.Save(content, category, tags)
}

// ... 其余绑定方法同理，全部委托给 kernel 的对应子系统
```

## TUI 层设计

### TUI 入口

```go
// tui/main.go
package main

import (
    "fmt"
    "os"

    "github.com/RapidAI/CodeClaw/corelib"
    tea "github.com/charmbracelet/bubbletea"
)

func main() {
    noTUI := hasFlag("--no-tui") || hasFlag("--batch")

    opts := corelib.KernelOptions{
        DataDir:  getDataDir(),
        HubURL:   getEnvOrFlag("MACLAW_HUB_URL", "--hub-url"),
        HubToken: getEnvOrFlag("MACLAW_TOKEN", "--token"),
    }

    if noTUI {
        // 纯 CLI 模式：NoopEmitter，stderr logger
        opts.EventEmitter = corelib.NoopEmitter{}
        opts.Logger = corelib.DefaultLogger{}
        kernel, err := corelib.NewKernel(opts)
        if err != nil {
            fmt.Fprintf(os.Stderr, "error: %v\n", err)
            os.Exit(1)
        }
        defer kernel.Shutdown(context.Background())
        os.Exit(runCLICommand(kernel, os.Args))
    }

    // TUI 模式：Bubble Tea
    tuiApp := NewTUIApp(opts)
    p := tea.NewProgram(tuiApp, tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}
```

### TUI Event Bridge

```go
// tui/event_bridge.go
package main

import tea "github.com/charmbracelet/bubbletea"

// BubbleTeaEventBridge 将 CoreLib 事件转换为 Bubble Tea 的 Msg，
// 通过 Program.Send() 推送到 TUI 的 Update 循环中。
type BubbleTeaEventBridge struct {
    program *tea.Program
}

// KernelEventMsg 是包装内核事件的 Bubble Tea Msg 类型。
type KernelEventMsg struct {
    EventType string
    Payload   interface{}
}

func (b *BubbleTeaEventBridge) Emit(eventType string, payload interface{}) {
    if b.program != nil {
        b.program.Send(KernelEventMsg{EventType: eventType, Payload: payload})
    }
}

func (b *BubbleTeaEventBridge) Subscribe(eventType string, handler corelib.EventHandler) {
    // TUI 层通过 Bubble Tea 的 Update 方法统一处理事件，
    // 不需要单独的 Subscribe 机制
}
```

### TUI 视图结构

```go
// tui/views/root.go
package views

import tea "github.com/charmbracelet/bubbletea"

// RootModel 是 TUI 的顶层 Bubble Tea Model。
// 管理 Tab 切换和子视图路由。
type RootModel struct {
    kernel    *corelib.Kernel
    activeTab int                // 0=会话列表, 1=工具状态, 2=配置
    tabs      []tea.Model        // 子视图
    statusBar StatusBarModel     // 底部状态栏（显示连接状态、日志等）
    width     int
    height    int
}

// 键盘导航：
// Tab / Shift+Tab: 切换面板
// q / Ctrl+C: 退出
// Enter: 确认/进入详情
// Esc: 返回上级
```

## 平台能力适配设计

### 工具平台依赖标注

```go
// corelib/tool/registry.go

// ToolCapRequirement 描述工具的平台依赖。
type ToolCapRequirement struct {
    RequiresDisplay   bool // 需要显示服务器（截图、屏幕亮度等）
    RequiresClipboard bool // 需要剪贴板
    RequiresNetwork   bool // 需要网络连接
}

// RegisterTool 注册工具时附带平台依赖声明。
func (r *Registry) RegisterTool(name string, handler ToolHandler, caps ToolCapRequirement) {
    r.tools[name] = &registeredTool{
        handler: handler,
        caps:    caps,
    }
}

// AvailableTools 返回当前环境下可用的工具列表。
// 在无头环境下自动过滤掉 RequiresDisplay=true 的工具。
func (r *Registry) AvailableTools(platform *PlatformCapabilities) []ToolInfo {
    var result []ToolInfo
    for _, t := range r.tools {
        if t.caps.RequiresDisplay && !platform.HasDisplay() {
            continue
        }
        if t.caps.RequiresClipboard && !platform.HasClipboard() {
            continue
        }
        result = append(result, t.Info())
    }
    return result
}
```

### 截图工具降级示例

```go
// corelib/remote/screenshot.go

func (m *Manager) CaptureScreenshot(sessionID string) error {
    if !m.kernel.Platform().HasDisplay() {
        return fmt.Errorf("screenshot unavailable: %s", m.kernel.Platform().DisplayInfo())
    }
    // 正常截图逻辑...
}
```

### 剪贴板降级方案

```go
// corelib/platform.go

// ClipboardGet 在有 GUI 时使用系统剪贴板，无头环境下读取临时文件。
func (k *Kernel) ClipboardGet() (string, error) {
    if k.platform.HasClipboard() {
        return systemClipboardGet()
    }
    // 降级：从 DataDir/clipboard.txt 读取
    return os.ReadFile(filepath.Join(k.opts.DataDir, "clipboard.txt"))
}

// ClipboardSet 在有 GUI 时写入系统剪贴板，无头环境下写入临时文件。
func (k *Kernel) ClipboardSet(text string) error {
    if k.platform.HasClipboard() {
        return systemClipboardSet(text)
    }
    return os.WriteFile(filepath.Join(k.opts.DataDir, "clipboard.txt"), []byte(text), 0644)
}
```

## Kernel 长驻运行模型

### 核心概念

Kernel 本身是一个长驻的"机器人引擎"。GUI、TUI、CLI 都只是它的 UI 壳。daemon 模式是"无 UI"的 Kernel。

```
┌──────────────────────────────────────────────┐
│              Kernel.Run() 事件循环             │
│  ┌─────────┐ ┌──────────┐ ┌───────────────┐  │
│  │Hub WS   │ │定时任务   │ │ClawNet 自动拾取│  │
│  │心跳+派发 │ │调度器     │ │               │  │
│  └─────────┘ └──────────┘ └───────────────┘  │
│  ┌─────────┐ ┌──────────┐ ┌───────────────┐  │
│  │MCP 发现  │ │会话监控   │ │后台 Agent Loop│  │
│  └─────────┘ └──────────┘ └───────────────┘  │
├──────────────────────────────────────────────┤
│  UI 层（可选，并行运行）                       │
│  ┌──────┐  ┌──────┐  ┌────────┐              │
│  │Wails │  │Bubble│  │daemon  │              │
│  │GUI   │  │Tea   │  │(无 UI) │              │
│  └──────┘  └──────┘  └────────┘              │
└──────────────────────────────────────────────┘
```

### Kernel.Run() 方法

```go
// corelib/kernel.go

// Run 启动内核事件循环，阻塞直到 ctx 被取消。
// 内部启动所有后台子系统：Hub WebSocket、定时任务、ClawNet、MCP 发现等。
// GUI 和 TUI 在各自的 goroutine 中调用此方法。
// daemon 模式直接在 main goroutine 中调用。
func (k *Kernel) Run(ctx context.Context) error {
    g, gctx := errgroup.WithContext(ctx)

    // Hub WebSocket 连接 + 心跳 + 任务派发
    g.Go(func() error { return k.Remote.RunEventLoop(gctx) })

    // 定时任务调度器
    g.Go(func() error { return k.Scheduler.Run(gctx) })

    // ClawNet 自动任务拾取
    if k.opts.ClawNetEnabled {
        g.Go(func() error { return k.ClawNet.RunAutoPicker(gctx) })
    }

    // MCP 自动发现
    g.Go(func() error { return k.MCP.RunDiscovery(gctx) })

    // 会话监控
    g.Go(func() error { return k.Sessions.RunMonitor(gctx) })

    return g.Wait()
}
```

### 各模式的启动流程

**GUI 模式：**
```go
// gui/main.go
func (a *GUIApp) startup(ctx context.Context) {
    kernel, _ := corelib.NewKernel(opts)
    a.kernel = kernel
    // Kernel 在后台 goroutine 长驻运行
    go kernel.Run(ctx)
}
// Wails 的 UI 事件循环在主 goroutine 运行
wails.Run(appOptions)
```

**TUI 模式：**
```go
// tui/main.go
func main() {
    kernel, _ := corelib.NewKernel(opts)
    // Kernel 在后台 goroutine 长驻运行
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()
    go kernel.Run(ctx)
    // Bubble Tea UI 在主 goroutine 运行
    p := tea.NewProgram(NewTUIApp(kernel), tea.WithAltScreen())
    p.Run()
    kernel.Shutdown(context.Background())
}
```

**Daemon 模式（无 UI）：**
```go
// tui/main.go — "daemon" 子命令
func runDaemon(kernel *corelib.Kernel) {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    if pidFile != "" {
        os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
        defer os.Remove(pidFile)
    }

    kernel.Logger().Info("maclaw daemon started, pid=%d", os.Getpid())

    // 直接在主 goroutine 阻塞运行 Kernel
    if err := kernel.Run(ctx); err != nil && err != context.Canceled {
        kernel.Logger().Error("daemon error: %v", err)
        os.Exit(1)
    }

    kernel.Shutdown(context.Background())
    kernel.Logger().Info("maclaw daemon stopped")
}
```

**CLI 单次命令模式：**
```go
// tui/commands/session.go
func runSessionStart(kernel *corelib.Kernel, args []string) int {
    // 不调用 kernel.Run()，只调用具体的 API 方法
    err := kernel.Sessions.Start(context.Background(), opts)
    if err != nil { return 1 }
    return 0
}
```

### systemd 示例

```ini
# deploy/maclaw-daemon.service
[Unit]
Description=MaClaw Daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/maclaw-tui daemon --log-file /var/log/maclaw/daemon.log --pid-file /run/maclaw.pid
Restart=on-failure
RestartSec=5
User=maclaw
Environment=MACLAW_HUB_URL=wss://hub.example.com/ws
Environment=MACLAW_TOKEN=xxx

[Install]
WantedBy=multi-user.target
```

## TUI 模式下的工具启动策略

### 问题背景

当前 GUI 模式通过 `exec.Command` 弹出新终端窗口（Windows Terminal / Terminal.app / xterm）来启动 Claude Code 等 CLI/TUI 工具。但在 TUI 模式下用户已经在终端里，再弹窗口不合理；无头 Linux 上更没有终端模拟器可弹。

### 三种启动模式

| 运行环境 | 启动方式 | 说明 |
|---|---|---|
| GUI（Wails 桌面端） | 弹出新终端窗口 | 行为不变，与当前一致 |
| TUI 交互模式 | 暂挂 TUI → 前台 exec → 恢复 TUI | 工具直接接管当前终端 |
| CLI 批处理 / 无 TTY | Headless 模式（`--print` 等） | 输出到 stdout，无交互 |

### CoreLib 层：ToolLaunchMode 抽象

```go
// corelib/tool/launch.go

// ToolLaunchMode 定义工具启动模式。
type ToolLaunchMode int

const (
    // LaunchInteractive 交互模式：工具接管终端（TUI 前台 exec）或弹出新窗口（GUI）。
    LaunchInteractive ToolLaunchMode = iota
    // LaunchHeadless 无头模式：工具以非交互方式运行（--print / --output-format json）。
    LaunchHeadless
)

// LaunchOptions 工具启动选项。
type LaunchOptions struct {
    ProjectDir  string
    Tool        string            // "claude", "codex", "gemini", ...
    Mode        ToolLaunchMode
    Env         map[string]string // 额外环境变量
    Args        []string          // 额外命令行参数
    YoloMode    bool
    AdminMode   bool
}

// ToolLauncher 是工具启动的抽象接口。
// GUI 和 TUI 各自提供不同的实现。
type ToolLauncher interface {
    // Launch 启动指定工具。
    // 返回的 error 为 nil 表示工具正常退出。
    Launch(ctx context.Context, opts LaunchOptions) error

    // SupportsMode 检查当前环境是否支持指定的启动模式。
    SupportsMode(mode ToolLaunchMode) bool
}
```

CoreLib 的 `Kernel` 持有一个 `ToolLauncher` 接口，由上层（GUI/TUI）在初始化时注入：

```go
// corelib/kernel_options.go
type KernelOptions struct {
    // ... 其他字段 ...

    // ToolLauncher 工具启动器，由上层注入。
    // GUI 注入弹窗口的实现，TUI 注入前台 exec 的实现。
    ToolLauncher tool.ToolLauncher
}
```

### GUI 层实现：弹出终端窗口

```go
// gui/tool_launcher.go

// GUIToolLauncher 通过弹出新终端窗口启动工具（与当前行为一致）。
type GUIToolLauncher struct {
    ctx context.Context // Wails context
}

func (l *GUIToolLauncher) Launch(ctx context.Context, opts tool.LaunchOptions) error {
    // 复用当前 App.LaunchTool 的逻辑：
    // Windows: 启动 cmd.exe / Windows Terminal
    // macOS: 启动 Terminal.app / osascript
    // Linux: 启动 xterm / gnome-terminal
}

func (l *GUIToolLauncher) SupportsMode(mode tool.ToolLaunchMode) bool {
    return true // GUI 支持所有模式
}
```

### TUI 层实现：暂挂 + 前台 exec

```go
// tui/tool_launcher.go

// TUIToolLauncher 在 TUI 交互模式下暂挂 Bubble Tea，
// 让工具进程直接接管当前终端。
type TUIToolLauncher struct {
    program *tea.Program
    hasTTY  bool
}

func (l *TUIToolLauncher) Launch(ctx context.Context, opts tool.LaunchOptions) error {
    if opts.Mode == tool.LaunchHeadless || !l.hasTTY {
        return l.launchHeadless(ctx, opts)
    }
    // 交互模式：使用 tea.ExecProcess 暂挂 TUI
    cmd := buildToolCommand(opts)
    return l.program.Exec(tea.ExecProcess(cmd, func(err error) tea.Msg {
        return toolExitMsg{err: err}
    }))
}

func (l *TUIToolLauncher) launchHeadless(ctx context.Context, opts tool.LaunchOptions) error {
    // 添加 headless 参数（如 claude --print）
    cmd := buildHeadlessCommand(opts)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

func (l *TUIToolLauncher) SupportsMode(mode tool.ToolLaunchMode) bool {
    if mode == tool.LaunchInteractive {
        return l.hasTTY
    }
    return true
}
```

`tea.ExecProcess` 是 Bubble Tea 内置的机制，会自动处理 `ReleaseTerminal()` → 运行子进程 → `RestoreTerminal()` 的完整流程，包括信号处理（Ctrl+C 等）。

### Headless 模式参数映射

| 工具 | 交互模式命令 | Headless 模式参数 |
|---|---|---|
| Claude Code | `claude` | `claude --print` |
| Codex CLI | `codex` | `codex --quiet` |
| Gemini CLI | `gemini` | `gemini --output-format json` |
| 其他工具 | 直接执行 | 尝试 `--non-interactive` 或 `--batch` |

## 依赖隔离策略

### 编译隔离

| 层 | 允许的依赖 | 禁止的依赖 |
|---|---|---|
| `corelib/` | 标准库、`gorilla/websocket`、`modernc.org/sqlite`、`fsnotify`、`charmbracelet/lipgloss`（仅 TUI 格式化） | `wailsapp/wails`、`energye/systray`、任何 CGO GUI 库 |
| `gui/` | `corelib/`、`wailsapp/wails`、`energye/systray`、平台 GUI 库 | 直接引用 `corelib/` 内部未导出的符号 |
| `tui/` | `corelib/`、`charmbracelet/bubbletea`、`charmbracelet/lipgloss`、`charmbracelet/bubbles` | `wailsapp/wails`、`energye/systray` |

### CI 验证

```makefile
# Makefile

.PHONY: check-corelib-deps
check-corelib-deps:
	@echo "Checking corelib has no GUI dependencies..."
	@! grep -r "wailsapp/wails" corelib/ || (echo "FAIL: corelib imports wails" && exit 1)
	@! grep -r "energye/systray" corelib/ || (echo "FAIL: corelib imports systray" && exit 1)
	@echo "OK"

.PHONY: build-corelib-headless
build-corelib-headless:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./corelib/...

.PHONY: build-tui
build-tui:
	CGO_ENABLED=0 go build -o bin/maclaw-tui ./tui/

.PHONY: build-gui
build-gui:
	cd gui && wails build

.PHONY: build-all
build-all: check-corelib-deps build-corelib-headless build-tui build-gui
```

## 源文件迁移映射

以下是根目录 `.go` 文件到新位置的映射关系（关键文件）：

| 原文件 | 目标位置 | 说明 |
|---|---|---|
| `app.go` | `corelib/kernel.go` + `gui/app.go` | App 结构体拆分：核心字段→Kernel，GUI 字段→GUIApp |
| `main.go` | `gui/main.go` | Wails 入口迁移 |
| `app_wails_bindings.go` | `gui/bindings.go` | 绑定层迁移，改为委托 Kernel |
| `app_swarm_bindings.go` | `gui/bindings_swarm.go` | 同上 |
| `app_nl_mcp.go` | `gui/bindings_nl.go` | 同上 |
| `app_nl_skills.go` | `gui/bindings_nl.go` | 合并到 NL 绑定 |
| `app_maclaw_llm.go` | `gui/bindings_llm.go` | 同上 |
| `app_clawnet.go` | `gui/bindings.go` (部分) + `corelib/clawnet/` | ClawNet 逻辑→corelib，绑定→gui |
| `im_message_handler.go` | `corelib/agent/loop.go` | Agent 循环核心 |
| `agent_loop_context.go` | `corelib/agent/loop_context.go` | |
| `background_loop_manager.go` | `corelib/agent/background.go` | |
| `remote_session_manager.go` | `corelib/session/manager.go` | |
| `remote_hub_client.go` | `corelib/remote/hub_client.go` | |
| `remote_screenshot.go` | `corelib/remote/screenshot.go` | 增加 headless 降级 |
| `tool_registry.go` | `corelib/tool/registry.go` | |
| `tool_router.go` | `corelib/tool/router.go` | |
| `tool_selector.go` | `corelib/tool/selector.go` | |
| `config_manager.go` | `corelib/config/manager.go` | |
| `memory_store.go` | `corelib/memory/store.go` | |
| `security_firewall.go` | `corelib/security/firewall.go` | |
| `policy_engine.go` | `corelib/security/policy_engine.go` | |
| `audit_log.go` | `corelib/security/audit_log.go` | |
| `swarm_orchestrator.go` | `corelib/swarm/orchestrator.go` | |
| `swarm_*.go` | `corelib/swarm/` 对应文件 | |
| `mcp_auto_discovery.go` | `corelib/mcp/auto_discovery.go` | |
| `local_mcp_client.go` | `corelib/mcp/local_client.go` | |
| `clawnet_client.go` | `corelib/clawnet/client.go` | |
| `scheduled_task.go` | `corelib/scheduler/task.go` | |
| `skillhub_client.go` | `corelib/skill/hub_client.go` | |
| `tray_*.go` | `gui/tray_*.go` | GUI 专有 |
| `platform_*.go` | `gui/platform_*.go` | GUI 专有 |
| `screen_dim*.go` | `gui/screen_dim*.go` | GUI 专有 |
| `mac_compat_*.go` | `gui/mac_compat_*.go` | GUI 专有 |
| `resources_*.go` | `gui/resources_*.go` | GUI 专有 |
| `remote_smoke.go` | `gui/remote_smoke.go` | GUI 专有（需要 Wails App） |
| `android_pwa_shell.go` | `gui/android_pwa_shell.go` | GUI 专有 |
| `env_check_api.go` | `gui/env_check_api.go` | GUI 专有 |
| `internal/systray/` | `gui/internal/systray/` | GUI 专有（系统托盘原生实现） |
| `common.go` | `corelib/types.go` | 公共类型定义 |
| `remote_types.go` | `corelib/remote/types.go` | |
| `swarm_types.go` | `corelib/swarm/types.go` | |
| `cmd/maclaw-cli/main.go` | `cmd/maclaw-tool/main.go` | 重命名 |

## 迁移策略

### 分阶段执行

迁移分为 4 个阶段，每个阶段结束后项目必须可编译通过：

**阶段 1：基础框架搭建**
- 创建 `corelib/` 目录结构和核心接口（Kernel、EventEmitter、Logger、PlatformCapabilities）
- 创建空的子包骨架（每个子包一个占位文件）
- 此阶段不移动任何现有代码，仅新增文件

**阶段 2：CoreLib 子系统提取**
- 按子系统逐个将根目录 `.go` 文件迁移到 `corelib/` 对应子包
- 每迁移一个子系统，同步修改 `package` 声明和 import 路径
- 根目录的 `App` 结构体暂时保留，通过 import corelib 子包来引用已迁移的类型
- 迁移顺序（按依赖关系从底层到上层）：
  1. `types.go`（公共类型）→ `corelib/types.go`
  2. `config/` → `memory/` → `security/` → `tool/` → `remote/` → `session/` → `agent/` → `swarm/` → `mcp/` → `clawnet/` → `scheduler/` → `skill/` → `misc/`

**阶段 3：GUI 层分离**
- 创建 `gui/` 目录
- 将 `main.go`、`app_wails_bindings.go`、`tray_*.go`、`platform_*.go`、`screen_dim*.go` 等 GUI 专有文件迁入 `gui/`
- 创建 `GUIApp` 壳结构体，持有 `*corelib.Kernel`
- 将 `frontend/` 移至 `gui/frontend/`
- 适配 `wails.json` 中的路径配置
- 确保根目录不再有任何 `.go` 文件

**阶段 4：TUI 层实现 + maclaw-tool 重命名**
- 创建 `tui/` 目录，实现 Bubble Tea 界面
- 实现 CLI 子命令模式
- 将 `cmd/maclaw-cli/` 重命名为 `cmd/maclaw-tool/`
- 添加 Makefile 统一编译入口

### 关键迁移原则

1. **逐文件迁移，每步可编译**：不做大规模一次性移动，每次迁移一个子系统后确保 `go build ./...` 通过
2. **先提取接口，后移动实现**：先在 `corelib/` 定义接口，再将实现从根目录迁入
3. **App → Kernel 字段映射**：`App` 结构体中的子系统字段逐步替换为 `Kernel` 中的对应字段
4. **测试文件跟随源文件**：`*_test.go` 文件与对应的源文件一起迁移到同一目录
5. **`emitEvent` 调用替换**：所有 `a.emitEvent(...)` 调用替换为 `k.emitter.Emit(...)`，GUI 层的 `WailsEventBridge` 负责转发到 `runtime.EventsEmit`
6. **`runtime.*` 调用隔离**：所有 `runtime.EventsEmit`、`runtime.WindowShow` 等 Wails runtime 调用仅存在于 `gui/` 目录中

## 与现有需求的对应关系

| 需求 | 设计覆盖 |
|---|---|
| 需求 1：CoreLib 提取 | Kernel 结构体 + corelib/ 子包结构 |
| 需求 2：Kernel API | NewKernel() + KernelOptions + 子系统导出字段 |
| 需求 3：GUI 适配 | gui/ 目录 + GUIApp 壳 + WailsEventBridge + 绑定适配层 |
| 需求 4：TUI | tui/ 目录 + Bubble Tea 视图 + BubbleTeaEventBridge |
| 需求 5：CLI | tui/commands/ + --no-tui 模式 |
| 需求 6：目录重组 | 完整目录布局 + 根目录无 .go 源文件 |
| 需求 7：依赖隔离 | 依赖隔离表 + CI 验证 Makefile |
| 需求 8：maclaw-tool | cmd/maclaw-tool/ 重命名 |
| 需求 9：Event Emitter | EventEmitter 接口 + ChannelEmitter + NoopEmitter |
| 需求 10：日志抽象 | Logger 接口 + DefaultLogger |
| 需求 11：平台能力适配 | PlatformCapabilities + ToolCapRequirement + 降级方案 |
| 需求 12：TUI 工具启动策略 | ToolLaunchMode + ToolLauncher 接口 + TUI 暂挂前台 exec + Headless 降级 |
| 需求 13：Daemon 守护进程 | Kernel.Run() 事件循环 + daemon 子命令 + systemd unit 文件 |
