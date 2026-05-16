# 技术设计文档：Swarm Kernel Unification（蜂群内核统一）

## 概述

本设计将 GUI 端 `gui/swarm_*.go` 中的 SwarmOrchestrator 核心逻辑提取到 `corelib/swarm/` 包，通过接口抽象会话管理、应用上下文和 LLM 调用依赖，使 GUI 和 TUI 共享同一个编排内核。

### 现状分析

当前代码存在以下重复和耦合问题：

1. **类型完全重复**：`gui/swarm_types.go` 和 `corelib/swarm/types.go` 定义了完全相同的类型（SwarmRun、SwarmAgent、SubTask 等），两份代码逐行一致
2. **Notifier 重复**：`gui/swarm_notifier.go` 和 `corelib/swarm/notifier.go` 各有一套 Notifier 接口和 DefaultNotifier 实现，逻辑几乎相同
3. **WorktreeManager 重复**：`gui/swarm_worktree.go` 和 `corelib/swarm/worktree.go` 各有一份 WorktreeManager，逻辑一致但 git helper 函数名不同（`runGit` vs `swarmRunGit`）
4. **ConflictDetector 重复**：`gui/swarm_conflict.go` 和 `corelib/swarm/conflict.go` 各有一份，GUI 版多了 `*ProjectScanner` 依赖
5. **编排逻辑仅在 GUI**：SwarmOrchestrator、pipeline、agent scheduler、feedback loop、task splitter、reporter、prompts、doc generator、LLM caller 全部在 `gui/` 包中
6. **TUI 使用简化版**：`tui/commands/swarm.go` 使用 `corelib/misc.TaskOrchestrator`，无 worktree、无冲突检测、无反馈循环

### 设计决策

1. **corelib/swarm/ 为权威来源**：所有类型、接口、核心逻辑统一到 `corelib/swarm/`，GUI 通过类型别名或直接引用
2. **接口注入而非具体类型**：SwarmOrchestrator 构造函数接受 `SwarmSessionManager`、`SwarmAppContext`、`SwarmLLMCaller`、`Notifier` 接口
3. **GUI 变为薄适配层**：`gui/` 保留 Wails 绑定和适配器代码，编排逻辑全部委托给 `corelib/swarm.SwarmOrchestrator`
4. **TUI 获得完整能力**：TUI 通过实现相同接口接入编排内核，替换 `corelib/misc.TaskOrchestrator`
5. **渐进式迁移**：先迁移类型和接口，再迁移组件，最后切换调用方，每步可独立编译验证

## 架构

### 统一后的系统架构

```
┌─────────────────────────────────────────────────────────────────┐
│                     corelib/swarm/ (内核)                        │
│                                                                 │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐  │
│  │ SwarmOrchestrator │  │ Greenfield       │  │ Maintenance  │  │
│  │ (核心调度器)       │  │ Pipeline         │  │ Pipeline     │  │
│  └──────┬───────────┘  └──────────────────┘  └──────────────┘  │
│         │                                                       │
│  ┌──────┴───────────────────────────────────────────────────┐   │
│  │ 子组件：TaskSplitter, MergeController, FeedbackLoop,     │   │
│  │ SwarmReporter, TaskVerifier, SwarmDocGenerator,           │   │
│  │ SwarmPrompts, ToolSelector(委托 corelib/tool.Selector)   │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 已有组件（保持不变）：                                      │   │
│  │ types.go, notifier.go, worktree.go, conflict.go          │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 接口定义：                                                 │   │
│  │ SwarmSessionManager, SwarmSession, SwarmAppContext,       │   │
│  │ SwarmLLMCaller                                            │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         ▲                                    ▲
         │ 实现接口                            │ 实现接口
┌────────┴──────────┐              ┌──────────┴──────────┐
│   gui/ (适配层)    │              │   tui/ (适配层)      │
│                    │              │                      │
│ GUISessionAdapter  │              │ TUISessionAdapter    │
│ GUIAppContext      │              │ TUIAppContext         │
│ GUILLMCaller       │              │ TUILLMCaller          │
│ Wails Bindings     │              │ CLI Commands          │
│ (不变)             │              │ (重写)                │
└────────────────────┘              └──────────────────────┘
```

### 迁移前后文件对照

| 迁移前 (gui/) | 迁移后 (corelib/swarm/) | 说明 |
|---|---|---|
| `gui/swarm_types.go` | `corelib/swarm/types.go` (已存在) | GUI 文件删除，改用 corelib 定义 |
| `gui/swarm_notifier.go` | `corelib/swarm/notifier.go` (已存在) | GUI 文件删除，改用 corelib 定义 |
| `gui/swarm_worktree.go` | `corelib/swarm/worktree.go` (已存在) | GUI 文件删除，改用 corelib 定义 |
| `gui/swarm_conflict.go` | `corelib/swarm/conflict.go` (已存在) | GUI 文件删除，corelib 版本已无 scanner 依赖 |
| `gui/swarm_orchestrator.go` | `corelib/swarm/orchestrator.go` (新建) | 核心调度器迁移 |
| `gui/swarm_pipeline_greenfield.go` | `corelib/swarm/pipeline_greenfield.go` (新建) | Greenfield pipeline 迁移 |
| `gui/swarm_pipeline.go` | `corelib/swarm/pipeline_maintenance.go` (新建) | Maintenance pipeline 迁移 |
| `gui/swarm_agent_scheduler.go` | `corelib/swarm/agent_scheduler.go` (新建) | Agent 调度迁移 |
| `gui/swarm_task_splitter.go` | `corelib/swarm/task_splitter.go` (新建) | 任务分解迁移 |
| `gui/swarm_merge.go` | `corelib/swarm/merge.go` (新建) | 合并控制器迁移 |
| `gui/swarm_feedback.go` | `corelib/swarm/feedback.go` (新建) | 反馈循环迁移 |
| `gui/swarm_reporter.go` | `corelib/swarm/reporter.go` (新建) | 报告生成器迁移 |
| `gui/swarm_task_verifier.go` | `corelib/swarm/task_verifier.go` (新建) | 任务验证器迁移 |
| `gui/swarm_doc_generator.go` | `corelib/swarm/doc_generator.go` (新建) | 文档生成器迁移 |
| `gui/swarm_prompts.go` | `corelib/swarm/prompts.go` (新建) | Prompt 模板迁移 |
| `gui/swarm_llm.go` | `corelib/swarm/interfaces.go` (新建) | LLM 调用抽象为接口 |
| `gui/tool_selector.go` | 保持不变（已委托 `corelib/tool.Selector`） | 编排器直接使用 `corelib/tool.Selector` |
| `gui/app_swarm_bindings.go` | 保持不变（修改内部委托目标） | Wails 绑定层 |
| — | `gui/swarm_adapters.go` (新建) | GUI 适配器实现 |
| — | `tui/swarm_adapters.go` (新建) | TUI 适配器实现 |

## 组件与接口

### 1. 核心接口定义 (`corelib/swarm/interfaces.go`)

```go
package swarm

import "time"

// SwarmSessionManager 抽象会话管理能力，由 GUI 和 TUI 各自实现。
type SwarmSessionManager interface {
    Create(spec SwarmLaunchSpec) (SwarmSession, error)
    Get(sessionID string) (SwarmSession, bool)
    Kill(sessionID string) error
    WriteInput(sessionID string, text string) error
}

// SwarmSession 抽象单个会话，屏蔽 GUI RemoteSession 和 TUI TUISession 的差异。
type SwarmSession interface {
    SessionID() string
    SessionStatus() SessionStatus
    SessionSummary() SwarmSessionSummary
    SessionOutput() string
}

// SwarmAppContext 抽象应用级能力（如已安装工具列表）。
type SwarmAppContext interface {
    ListInstalledTools() []InstalledToolInfo
}

// SwarmLLMCaller 抽象 LLM 调用能力。
type SwarmLLMCaller interface {
    CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error)
}

// SwarmLaunchSpec 编排器创建会话所需的参数，不依赖 GUI 或 TUI 特定类型。
type SwarmLaunchSpec struct {
    Tool         string
    ProjectPath  string
    Env          map[string]string
    LaunchSource string
}

// SwarmSessionSummary 编排器监控会话所需的摘要信息。
type SwarmSessionSummary struct {
    Status         string
    ProgressSummary string
    LastResult     string
    WaitingForUser bool
    UpdatedAt      time.Time
}

// InstalledToolInfo 描述一个已安装的编程工具。
type InstalledToolInfo struct {
    Name     string
    CanStart bool
}

// SessionStatus 类型别名，复用 corelib/remote 的定义。
// 编排器代码通过此别名访问状态常量，无需直接导入 corelib/remote。
type SessionStatus = remote.SessionStatus

// 重新导出常用状态常量。
const (
    SessionStarting     = remote.SessionStarting
    SessionRunning      = remote.SessionRunning
    SessionBusy         = remote.SessionBusy
    SessionWaitingInput = remote.SessionWaitingInput
    SessionError        = remote.SessionError
    SessionExited       = remote.SessionExited
)
```

### 2. SwarmOrchestrator 重构 (`corelib/swarm/orchestrator.go`)

```go
package swarm

// SwarmOrchestrator 蜂群编排内核，通过接口依赖注入。
type SwarmOrchestrator struct {
    sessionMgr   SwarmSessionManager
    appCtx       SwarmAppContext
    llmCaller    SwarmLLMCaller
    worktreeMgr  *WorktreeManager
    conflictDet  *ConflictDetector
    taskSplitter *TaskSplitter
    mergeCtrl    *MergeController
    feedbackLoop *FeedbackLoop
    reporter     *SwarmReporter
    notifier     Notifier
    taskVerifier *TaskVerifier
    docGenerator *SwarmDocGenerator
    toolSelector *tool.Selector  // 直接使用 corelib/tool.Selector

    mu              sync.RWMutex
    activeRun       *SwarmRun
    runHistory      []*SwarmRun
    cachedInstalled []string
    maxRounds       int
    maxAgents       int
}

// NewSwarmOrchestrator 通过接口参数创建编排器。
// sessionMgr 和 notifier 必须非 nil；appCtx 和 llmCaller 可为 nil（回退到默认行为）。
func NewSwarmOrchestrator(
    sessionMgr SwarmSessionManager,
    notifier   Notifier,
    opts       ...OrchestratorOption,
) *SwarmOrchestrator

// OrchestratorOption 配置选项（函数选项模式）。
type OrchestratorOption func(*SwarmOrchestrator)

func WithAppContext(ctx SwarmAppContext) OrchestratorOption
func WithLLMCaller(caller SwarmLLMCaller) OrchestratorOption
func WithMaxRounds(n int) OrchestratorOption
func WithMaxAgents(n int) OrchestratorOption
```

关键变更：
- 构造函数不再接受 `*App` 和 `*RemoteSessionManager`，改为接口参数
- `swarmCallLLM` 全局函数替换为 `o.llmCaller.CallLLM` 方法调用
- `o.app.ListRemoteToolMetadata()` 替换为 `o.appCtx.ListInstalledTools()`
- `o.manager.Create/Get/Kill` 替换为 `o.sessionMgr.Create/Get/Kill`
- 子组件（TaskSplitter、FeedbackLoop、TaskVerifier）接受 `SwarmLLMCaller` 接口而非 `MaclawLLMConfig`

### 3. 子组件接口化改造

所有依赖 LLM 的子组件改为接受 `SwarmLLMCaller` 接口：

```go
// TaskSplitter — 改造前：NewTaskSplitter(cfg MaclawLLMConfig)
// 改造后：
func NewTaskSplitter(caller SwarmLLMCaller) *TaskSplitter

// FeedbackLoop — 改造前：NewFeedbackLoop(cfg MaclawLLMConfig, maxRounds int)
// 改造后：
func NewFeedbackLoop(caller SwarmLLMCaller, maxRounds int) *FeedbackLoop

// TaskVerifier — 改造前：NewTaskVerifier(cfg MaclawLLMConfig)
// 改造后：
func NewTaskVerifier(caller SwarmLLMCaller) *TaskVerifier
```

当 `SwarmLLMCaller` 为 nil 时，这些组件返回描述性错误而非 panic：
```go
func (s *TaskSplitter) SplitRequirements(...) ([]SubTask, error) {
    if s.caller == nil {
        return nil, fmt.Errorf("LLM caller not configured, cannot split requirements")
    }
    // ...
}
```

### 4. MergeController 改造

MergeController 当前依赖 GUI 的 `runGit`/`runGitOutput` 和 `hideCommandWindow` 函数。迁移后使用 `corelib/swarm/worktree.go` 中已有的 `swarmRunGit`/`swarmRunGitOutput` 函数，并将 `runShellCommand` 迁移为包内函数。

```go
// corelib/swarm/merge.go
type MergeController struct {
    worktreeMgr *WorktreeManager
}

func NewMergeController(wm *WorktreeManager) *MergeController
func (m *MergeController) MergeAll(projectPath string, branches []BranchInfo, compileCmd string) (*MergeResult, error)
func (m *MergeController) RevertBranch(projectPath, branchName string) error
```

### 5. Agent Scheduler 改造

Agent scheduler 是编排器中最依赖会话管理的部分。改造要点：

```go
// 改造前（gui/swarm_agent_scheduler.go）：
func (o *SwarmOrchestrator) createAgent(...) {
    spec := LaunchSpec{...}                    // GUI 的 LaunchSpec
    session, err := o.manager.Create(spec)     // GUI 的 RemoteSessionManager
    // ...
    s.mu.RLock()                               // 直接访问 RemoteSession.mu
    status := s.Status
    s.mu.RUnlock()
}

// 改造后（corelib/swarm/agent_scheduler.go）：
func (o *SwarmOrchestrator) createAgent(...) {
    spec := SwarmLaunchSpec{...}               // corelib/swarm 的 SwarmLaunchSpec
    session, err := o.sessionMgr.Create(spec)  // SwarmSessionManager 接口
    // ...
    status := session.SessionStatus()          // SwarmSession 接口方法（内部处理锁）
}
```

### 6. GUI 适配层 (`gui/swarm_adapters.go`)

```go
package main

import (
    "github.com/RapidAI/CodeClaw/corelib/remote"
    "github.com/RapidAI/CodeClaw/corelib/swarm"
)

// --- SwarmSessionManager 适配 ---

// GUISessionAdapter 将 RemoteSessionManager 适配为 SwarmSessionManager。
type GUISessionAdapter struct {
    manager *RemoteSessionManager
}

func (a *GUISessionAdapter) Create(spec swarm.SwarmLaunchSpec) (swarm.SwarmSession, error) {
    launchSpec := remote.LaunchSpec{
        Tool:         spec.Tool,
        ProjectPath:  spec.ProjectPath,
        Env:          spec.Env,
        LaunchSource: remote.RemoteLaunchSource(spec.LaunchSource),
    }
    session, err := a.manager.Create(launchSpec)
    if err != nil {
        return nil, err
    }
    return &GUISessionWrapper{session: session}, nil
}

func (a *GUISessionAdapter) Get(sessionID string) (swarm.SwarmSession, bool) {
    s, ok := a.manager.Get(sessionID)
    if !ok {
        return nil, false
    }
    return &GUISessionWrapper{session: s}, true
}

func (a *GUISessionAdapter) Kill(sessionID string) error {
    return a.manager.Kill(sessionID)
}

func (a *GUISessionAdapter) WriteInput(sessionID, text string) error {
    return a.manager.WriteInput(sessionID, text)
}

// GUISessionWrapper 将 RemoteSession 适配为 SwarmSession。
type GUISessionWrapper struct {
    session *RemoteSession
}

func (w *GUISessionWrapper) SessionID() string {
    return w.session.ID
}

func (w *GUISessionWrapper) SessionStatus() swarm.SessionStatus {
    w.session.mu.RLock()
    defer w.session.mu.RUnlock()
    return w.session.Status
}

func (w *GUISessionWrapper) SessionSummary() swarm.SwarmSessionSummary {
    w.session.mu.RLock()
    defer w.session.mu.RUnlock()
    return swarm.SwarmSessionSummary{
        Status:     string(w.session.Status),
        LastResult: w.session.Summary.LastResult,
        UpdatedAt:  time.Unix(w.session.Summary.UpdatedAt, 0),
    }
}

func (w *GUISessionWrapper) SessionOutput() string {
    w.session.mu.RLock()
    defer w.session.mu.RUnlock()
    return w.session.Summary.LastResult
}

// --- SwarmAppContext 适配 ---

// GUIAppContext 将 App 适配为 SwarmAppContext。
type GUIAppContext struct {
    app *App
}

func (c *GUIAppContext) ListInstalledTools() []swarm.InstalledToolInfo {
    views := c.app.ListRemoteToolMetadata()
    var result []swarm.InstalledToolInfo
    for _, v := range views {
        if v.Installed && v.CanStart {
            result = append(result, swarm.InstalledToolInfo{
                Name: v.Name, CanStart: v.CanStart,
            })
        }
    }
    return result
}

// --- SwarmLLMCaller 适配 ---

// GUILLMCaller 将 GUI 的 MaclawLLMConfig + doSimpleLLMRequest 适配为 SwarmLLMCaller。
type GUILLMCaller struct {
    config MaclawLLMConfig
}

func (c *GUILLMCaller) CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error) {
    // 复用现有的 swarmCallLLM 逻辑
    return swarmCallLLM(c.config, prompt, temperature, timeout)
}
```

### 7. TUI 适配层 (`tui/swarm_adapters.go`)

```go
package main

import (
    "github.com/RapidAI/CodeClaw/corelib/remote"
    "github.com/RapidAI/CodeClaw/corelib/swarm"
)

// --- SwarmSessionManager 适配 ---

// TUISwarmSessionAdapter 将 TUISessionManager 适配为 SwarmSessionManager。
type TUISwarmSessionAdapter struct {
    manager *TUISessionManager
}

func (a *TUISwarmSessionAdapter) Create(spec swarm.SwarmLaunchSpec) (swarm.SwarmSession, error) {
    launchSpec := remote.LaunchSpec{
        Tool:        spec.Tool,
        ProjectPath: spec.ProjectPath,
        Env:         spec.Env,
    }
    session, err := a.manager.Create(launchSpec)
    if err != nil {
        return nil, err
    }
    return &TUISwarmSessionWrapper{session: session}, nil
}

func (a *TUISwarmSessionAdapter) Get(sessionID string) (swarm.SwarmSession, bool) {
    s, ok := a.manager.Get(sessionID)
    if !ok {
        return nil, false
    }
    return &TUISwarmSessionWrapper{session: s}, true
}

func (a *TUISwarmSessionAdapter) Kill(sessionID string) error {
    return a.manager.Kill(sessionID)
}

func (a *TUISwarmSessionAdapter) WriteInput(sessionID, text string) error {
    return a.manager.WriteInput(sessionID, text)
}

// TUISwarmSessionWrapper 将 TUISession 适配为 SwarmSession。
type TUISwarmSessionWrapper struct {
    session *TUISession
}

func (w *TUISwarmSessionWrapper) SessionID() string {
    return w.session.ID
}

func (w *TUISwarmSessionWrapper) SessionStatus() swarm.SessionStatus {
    w.session.mu.Lock()
    defer w.session.mu.Unlock()
    return w.session.Status
}

func (w *TUISwarmSessionWrapper) SessionSummary() swarm.SwarmSessionSummary {
    w.session.mu.Lock()
    defer w.session.mu.Unlock()
    return swarm.SwarmSessionSummary{
        Status:    string(w.session.Status),
        UpdatedAt: time.Unix(w.session.Summary.UpdatedAt, 0),
    }
}

func (w *TUISwarmSessionWrapper) SessionOutput() string {
    w.session.mu.Lock()
    defer w.session.mu.Unlock()
    return strings.Join(w.session.PreviewLines, "\n")
}

// --- SwarmAppContext 适配 ---

// TUIAppContext 通过扫描本地二进制文件返回工具列表。
type TUIAppContext struct{}

func (c *TUIAppContext) ListInstalledTools() []swarm.InstalledToolInfo {
    // 扫描 PATH 中已知的编程工具二进制
    tools := []string{"claude", "cursor", "codex", "gemini"}
    var result []swarm.InstalledToolInfo
    for _, t := range tools {
        _, err := exec.LookPath(t)
        result = append(result, swarm.InstalledToolInfo{
            Name: t, CanStart: err == nil,
        })
    }
    return result
}

// --- SwarmLLMCaller 适配 ---

// TUILLMCaller 使用 TUI 端的 LLM 配置。
type TUILLMCaller struct {
    config corelib.MaclawLLMConfig
}

func (c *TUILLMCaller) CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error) {
    // 使用 TUI 端已有的 LLM 调用逻辑
    // ...
}

// --- TUI Notifier ---

// TUINotifier 将 Swarm 通知格式化输出到终端。
type TUINotifier struct{}

func (n *TUINotifier) NotifyPhaseChange(run *swarm.SwarmRun, phase swarm.SwarmPhase) error {
    fmt.Printf("[Swarm %s] Phase → %s\n", run.ID, phase)
    return nil
}
// ... 其他 Notifier 方法类似，输出到 stdout
```

### 8. GUI Wails 绑定层改造 (`gui/app_swarm_bindings.go`)

绑定层保持函数签名不变，仅修改内部构造逻辑：

```go
// 改造前：
func (a *App) ensureSwarmOrchestrator() {
    swarmInitOnce.Do(func() {
        notifier := NewDefaultSwarmNotifier(a)
        a.swarmOrchestrator = NewSwarmOrchestrator(
            a,                    // *App
            a.remoteSessions,     // *RemoteSessionManager
            a.sharedContext,
            a.projectScanner,
            notifier,
            llmCfg,
        )
    })
}

// 改造后：
func (a *App) ensureSwarmOrchestrator() {
    swarmInitOnce.Do(func() {
        sessionAdapter := &GUISessionAdapter{manager: a.remoteSessions}
        appCtx := &GUIAppContext{app: a}
        llmCaller := &GUILLMCaller{config: a.GetMaclawLLMConfig()}
        notifier := swarm.NewDefaultNotifier(func(name string, data ...interface{}) {
            a.emitEvent(name, data...)
        })
        a.swarmOrchestrator = swarm.NewSwarmOrchestrator(
            sessionAdapter,
            notifier,
            swarm.WithAppContext(appCtx),
            swarm.WithLLMCaller(llmCaller),
        )
    })
}
```

所有 Wails 绑定方法（StartSwarmRun、PauseSwarmRun 等）的签名和返回类型保持不变。类型通过别名引用 `corelib/swarm`：

```go
// gui/corelib_aliases.go 新增：
type SwarmRun = swarm.SwarmRun
type SwarmRunRequest = swarm.SwarmRunRequest
type SwarmRunSummary = swarm.SwarmRunSummary
type SwarmReport = swarm.SwarmReport
// ... 其他 swarm 类型别名
```

### 9. TUI swarm 命令重写 (`tui/commands/swarm.go`)

```go
// 改造前：使用 misc.TaskOrchestrator
func getOrchestrator() *misc.TaskOrchestrator {
    return misc.NewTaskOrchestratorWithPersist(nil, persistPath)
}

// 改造后：使用 swarm.SwarmOrchestrator
func getSwarmOrchestrator() *swarm.SwarmOrchestrator {
    sessionMgr := &TUISwarmSessionAdapter{manager: getSessionManager()}
    notifier := &TUINotifier{}
    appCtx := &TUIAppContext{}
    llmCaller := &TUILLMCaller{config: loadLLMConfig()}
    return swarm.NewSwarmOrchestrator(
        sessionMgr, notifier,
        swarm.WithAppContext(appCtx),
        swarm.WithLLMCaller(llmCaller),
    )
}
```

新的 CLI 接口：
```
maclaw-tui swarm create --mode greenfield --requirements <file> [--tool claude] [--max-agents 5]
maclaw-tui swarm create --mode maintenance --tasks <file> [--tool claude]
maclaw-tui swarm status <run_id>
maclaw-tui swarm cancel <run_id>
maclaw-tui swarm list
```

### 10. Notifier 统一

corelib/swarm/notifier.go 已有 `Notifier` 接口、`DefaultNotifier` 和 `NoopNotifier`。统一后：

- GUI 删除 `gui/swarm_notifier.go`，使用 `swarm.NewDefaultNotifier(emitFn)` 并注入 `App.emitEvent`
- TUI 实现 `TUINotifier`，将通知格式化输出到终端
- `DefaultNotifier.SetIMDelivery` 方法保持不变，GUI 通过 `wireSwarmIMDelivery` 注入

## 数据模型

### 类型统一策略

`corelib/swarm/types.go` 已包含所有核心类型定义，与 `gui/swarm_types.go` 完全一致。统一后：

1. `gui/swarm_types.go` 删除，在 `gui/corelib_aliases.go` 中添加类型别名
2. `corelib/swarm/types.go` 成为唯一权威来源
3. `SwarmRun.UserInputCh` 字段保持 `json:"-"` 标签

新增类型（在 `corelib/swarm/interfaces.go`）：
- `SwarmLaunchSpec` — 编排器创建会话的参数
- `SwarmSessionSummary` — 编排器监控会话的摘要
- `InstalledToolInfo` — 已安装工具信息
- `TaskVerdict` — 从 `gui/swarm_task_verifier.go` 迁移
- `DocType` — 从 `gui/swarm_doc_generator.go` 迁移

### git helper 函数统一

`gui/swarm_worktree.go` 中的 git helper（`runGit`、`gitHasCommits` 等）依赖 `gui/remote_workspace.go` 中的 `runGit`/`runGitOutput`。`corelib/swarm/worktree.go` 有独立的 `swarmRunGit`/`swarmRunGitOutput`。

统一后：`corelib/swarm/worktree.go` 中的 `swarmRunGit` 系列函数成为唯一实现。`MergeController` 迁移后也使用这些函数。GUI 的 `runGit`（来自 `remote_workspace.go`）不受影响，仅 swarm 相关代码使用 `swarmRunGit`。

## 正确性属性

*继承自 swarm-orchestrator spec 的所有 25 个正确性属性，加上以下内核统一特有的属性：*

### Property 26: 接口隔离

*For any* `corelib/swarm/` 包中的 `.go` 文件，其 import 列表 SHALL NOT 包含 `gui/` 或 `tui/` 包路径。

**Validates: Requirements 3.6**

### Property 27: GUI 行为等价

*For any* 有效的 `SwarmRunRequest`，通过 GUI 适配层调用 `corelib/swarm.SwarmOrchestrator.StartSwarmRun` 的行为（阶段序列、事件名称、报告格式）SHALL 与迁移前 `gui.SwarmOrchestrator.StartSwarmRun` 的行为等价。

**Validates: Requirements 9.1, 9.2, 9.3, 9.4, 9.5**

### Property 28: TUI 能力对等

*For any* 有效的 `SwarmRunRequest`，通过 TUI 适配层调用 `corelib/swarm.SwarmOrchestrator.StartSwarmRun` SHALL 执行与 GUI 相同的 pipeline 阶段序列。

**Validates: Requirements 6.1, 6.2, 6.3, 6.4**

### Property 29: Nil 安全

*For any* `SwarmOrchestrator` 实例，当 `SwarmAppContext` 为 nil 时，`installedToolNames()` SHALL 返回 nil 而非 panic。当 `SwarmLLMCaller` 为 nil 时，依赖 LLM 的组件 SHALL 返回描述性 error 而非 panic。

**Validates: Requirements 2.3, 8.3**

### Property 30: 类型别名等价

*For any* 在 `gui/corelib_aliases.go` 中定义的 swarm 类型别名，其底层类型 SHALL 与 `corelib/swarm/types.go` 中的定义完全一致（Go 类型别名保证）。

**Validates: Requirements 7.1, 7.5**

## 错误处理

### 迁移特有的错误场景

| 场景 | 处理方式 |
|------|---------|
| SwarmSessionManager 为 nil | `NewSwarmOrchestrator` 返回 error（必需依赖） |
| Notifier 为 nil | `NewSwarmOrchestrator` 返回 error（必需依赖） |
| SwarmAppContext 为 nil | `installedToolNames()` 返回 nil，ToolSelector 使用默认工具 "claude" |
| SwarmLLMCaller 为 nil | TaskSplitter/FeedbackLoop/TaskVerifier 返回 `fmt.Errorf("LLM caller not configured: ...")` |
| GUI 适配器 LaunchSpec 转换失败 | 透传底层 RemoteSessionManager 的 error |
| TUI 适配器 PTY 创建失败 | 透传底层 TUISessionManager 的 error |
| 类型别名编译冲突 | 删除 GUI 重复定义后，所有引用改为 corelib/swarm 限定名或别名 |

### 继承的错误处理策略

编排器内部的错误处理策略（Agent 重试、合并回退、反馈循环轮次限制等）从 swarm-orchestrator spec 继承，迁移过程中保持不变。

## 测试策略

### 迁移验证测试

迁移过程中的核心验证手段：

1. **编译验证**：每个迁移步骤后 `go build ./...` 确保全项目编译通过
2. **现有测试通过**：`go test ./gui/... ./corelib/swarm/... ./tui/...` 确保现有测试不回归
3. **接口契约测试**：为 `SwarmSessionManager`、`SwarmSession`、`SwarmAppContext`、`SwarmLLMCaller` 编写 mock 实现，验证编排器通过接口正确调用

### 新增测试

| 测试文件 | 覆盖内容 |
|---------|---------|
| `corelib/swarm/orchestrator_test.go` | 编排器通过 mock 接口的生命周期测试（start/pause/resume/cancel） |
| `corelib/swarm/interfaces_test.go` | 接口 nil 安全测试（AppContext nil、LLMCaller nil） |
| `gui/swarm_adapters_test.go` | GUI 适配器的 LaunchSpec 转换、SessionStatus 读锁安全 |
| `tui/swarm_adapters_test.go` | TUI 适配器的 LaunchSpec 转换、工具扫描 |

### 现有测试迁移

以下 GUI 测试文件需要迁移到 `corelib/swarm/`（因为被测代码已迁移）：

| GUI 测试文件 | 迁移目标 |
|---|---|
| `gui/swarm_types_test.go` | `corelib/swarm/types_test.go`（如不存在则新建） |
| `gui/swarm_notifier_test.go` | `corelib/swarm/notifier_test.go`（如不存在则新建） |
| `gui/swarm_worktree_test.go` | `corelib/swarm/worktree_test.go`（如不存在则新建） |
| `gui/swarm_conflict_test.go` | `corelib/swarm/conflict_test.go`（如不存在则新建） |
| `gui/swarm_orchestrator_test.go` | `corelib/swarm/orchestrator_test.go` |
| `gui/swarm_task_splitter_test.go` | `corelib/swarm/task_splitter_test.go` |
| `gui/swarm_feedback_test.go` | `corelib/swarm/feedback_test.go` |
| `gui/swarm_merge_test.go` | `corelib/swarm/merge_test.go` |
| `gui/swarm_reporter_test.go` | `corelib/swarm/reporter_test.go` |
| `gui/swarm_task_verifier_test.go` | `corelib/swarm/task_verifier_test.go` |
| `gui/swarm_prompts_test.go` | `corelib/swarm/prompts_test.go` |
| `gui/swarm_doc_generator_test.go` | `corelib/swarm/doc_generator_test.go` |
| `gui/swarm_tdd_test.go` | `corelib/swarm/tdd_test.go` |
| `gui/swarm_spec_pipeline_test.go` | `corelib/swarm/spec_pipeline_test.go` |
| `gui/swarm_agent_scheduler_test.go` | `corelib/swarm/agent_scheduler_test.go` |

测试迁移时需将 `package main` 改为 `package swarm` 或 `package swarm_test`，并将 GUI 特定的 mock（如 `*App`、`*RemoteSessionManager`）替换为接口 mock。
