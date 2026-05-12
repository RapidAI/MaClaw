# 定时任务调度器彻底修复设计

## 问题根因

### GUI 桌面版

`scheduler.Manager` 的初始化和 executor 接线分离在两个不同的生命周期阶段：

1. `ensureScheduledTaskManager()`（Layer 2, `initOnDemandInfra`）：创建 Manager + 调用 `Start()`（启动 30s ticker）
2. `createAndWireHubClient()`（Hub 连接建立后）：调用 `SetExecutor()`

**问题 1**：`Start()` 时 executor 为 nil，catch-up runs 无法执行。
**问题 2**：`createAndWireHubClient()` 依赖远程账号配置（`RemoteMachineID + RemoteMachineToken + RemoteHubURL`）。纯桌面用户（不连 Hub）永远不会调用 `createAndWireHubClient()`，executor 永远为 nil。ticker 正常运行，任务到期时 `fireByID` 返回 "no executor configured"。

`tick()` 每 30 秒动态读取 `m.executor`（在 RLock 下），所以一旦 `SetExecutor` 被调用，后续 tick 能正常工作。但纯桌面用户永远走不到 `SetExecutor`。

### TUI

- `scheduler.Manager` 完全没有初始化（无 `Start()`、无 `SetExecutor()`）
- `manage_schedule` 工具未注册到 `CoreToolRegistry`（不在 `ExtraHandlers` 中）
- TUI CLI 的 `schedule` 命令只做 CRUD（`openScheduleManager` 每次创建新 Manager 实例，不调用 `Start()`）

### MaClawSrv

- 完全没有调度器基础设施
- `agentservice.Service` 是无状态 REST API 服务器，没有后台 ticker

## 修复方案

### 核心原则

`scheduler.Manager` 的 `Start()` 必须在 `SetExecutor()` 之后调用。这是机制性修复——不是"先 Start 再等 executor 设置"，而是"executor 就绪后才 Start"。

### 修复 1: `corelib/scheduler/task.go` — `StartWithExecutor()` 新增方法

新增 `StartWithExecutor(fn TaskExecutor)` 方法，原子地设置 executor 并启动 ticker。这是推荐的初始化方式，消除 Start/SetExecutor 时序问题。

```go
// StartWithExecutor atomically sets the executor and starts the background
// scheduler. This is the recommended initialization method — it eliminates
// the race between Start() and SetExecutor().
func (m *Manager) StartWithExecutor(fn TaskExecutor) {
    m.mu.Lock()
    m.executor = fn
    m.mu.Unlock()
    m.Start()
}
```

`Start()` 保留向后兼容（catch-up runs 使用当时的 executor 快照，可能为 nil）。

### 修复 2: GUI — 延迟 Start 到 executor 就绪

**方案**：`ensureScheduledTaskManager()` 只创建 Manager（不调用 `Start()`）。在两个路径中调用 `StartWithExecutor()`：

1. **Hub 路径**（`createAndWireHubClient`）：现有的 `SetExecutor` 改为 `StartWithExecutor`
2. **本地路径**（新增）：`ensureScheduledTaskManager()` 末尾检查 `hubClient` 是否已就绪。如果已就绪，立即 `StartWithExecutor`。如果未就绪，注册一个 deferred start callback。

更简洁的方案：`ensureScheduledTaskManager()` 不调用 `Start()`。在 `createAndWireHubClient()` 中 `SetExecutor` 后调用 `Start()`。同时新增一个 **本地 executor 路径**——当 Hub 不可用时，使用本地的 `IMMessageHandler` 执行任务。

**本地 executor 实现**：

```go
func (a *App) buildLocalScheduledTaskExecutor() scheduler.TaskExecutor {
    return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
        handler := a.ensureLocalIMHandler()
        if handler == nil {
            return "", fmt.Errorf("agent not initialized")
        }
        actionText := fmt.Sprintf("[自动执行定时任务] 这是系统自动触发的定时任务，必须在一次执行中完成，不会有用户交互。请直接执行以下操作并返回结果：\n%s", task.Action)
        resp := handler.HandleIMMessageWithProgressAndStream(IMUserMessage{
            UserID:        "scheduled_task",
            Platform:      "scheduler",
            Text:          actionText,
            MinIterations: 50,
            IsBackground:  true,
            CancelCtx:     ctx,
        }, nil, nil, nil, nil)
        if resp == nil {
            return "", fmt.Errorf("nil response from agent")
        }
        if resp.Error != "" {
            return resp.Text, fmt.Errorf("%s", resp.Error)
        }
        return resp.Text, nil
    }
}
```

**初始化时序**（修复后）：

```
ensureScheduledTaskManager()
  → NewManager(path)  // 只创建，不 Start
  → a.scheduledTaskManager = stm
  → 立即调用 startSchedulerWithLocalExecutor()

startSchedulerWithLocalExecutor()
  → executor = a.buildLocalScheduledTaskExecutor()
  → a.scheduledTaskManager.StartWithExecutor(executor)
```

Hub 路径中的 `SetExecutor` 改为覆盖 executor（升级为 Hub 感知版本，增加 IM 推送能力）：

```
createAndWireHubClient()
  → a.scheduledTaskManager.SetExecutor(hubAwareExecutor)  // 覆盖本地 executor
```

这样：
- 纯桌面用户：本地 executor 生效，定时任务正常触发
- Hub 用户：Hub executor 覆盖本地 executor，增加 IM 推送能力
- catch-up runs：在 `StartWithExecutor` 中执行，executor 已就绪

### 修复 3: TUI — 注册 manage_schedule 工具 + 启动调度器

1. `TUIApp` 新增 `scheduledTaskManager *scheduler.Manager` 字段
2. `runTUIWithOptions()` 中初始化 Manager + `StartWithExecutor(tuiExecutor)`
3. `ExtraHandlers` 新增 `"manage_schedule": newManageScheduleHandler(app)`
4. TUI executor 使用 `agent.RunLoop` 执行任务

**TUI executor 实现**：

```go
func (app *TUIApp) buildScheduledTaskExecutor() scheduler.TaskExecutor {
    return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
        actionText := fmt.Sprintf("[自动执行定时任务] ...\n%s", task.Action)
        history := app.history.Load("scheduled_task")
        cb := newTuiSchedulerCallbacks(app)
        result := agent.RunLoop(cb, actionText, history, nil)
        if result.Error != "" {
            return result.Text, fmt.Errorf("%s", result.Error)
        }
        return result.Text, nil
    }
}
```

**TUI manage_schedule handler**：

```go
func newManageScheduleHandler(app *TUIApp) agent.ToolHandler {
    return func(args map[string]interface{}) string {
        action := stringVal(args, "action")
        switch action {
        case "create": return app.toolCreateScheduledTask(args)
        case "list":   return app.toolListScheduledTasks()
        case "delete": return app.toolDeleteScheduledTask(args)
        case "update": return app.toolUpdateScheduledTask(args)
        default:       return fmt.Sprintf("未知 action: %s", action)
        }
    }
}
```

CRUD 逻辑直接委托给 `app.scheduledTaskManager` 的 Add/List/Delete/Update 方法。

### 修复 4: MaClawSrv — 可选的调度器支持

MaClawSrv 是多租户 REST API 服务器。调度器需要按租户/用户隔离。

**方案**：通过环境变量 `MACLAW_ENABLE_SCHEDULER=true` 启用。启用后：

1. `runServer()` 中创建 `scheduler.Manager`（持久化到 `{dataRoot}/scheduled_tasks.json`）
2. executor 使用 `agentservice.Service.SendMessage()` 执行任务
3. 通过 HTTP API 暴露 CRUD 端点（`/api/v1/scheduled-tasks`）

**简化方案**（推荐）：MaClawSrv 当前是无状态 API 服务器，调度器是有状态的后台服务。建议 MaClawSrv 暂不内置调度器，而是通过外部 cron/systemd timer 调用 API 实现定时任务。在 API 文档中说明推荐做法。

如果确实需要内置，实现如下：

```go
// runServer() 中：
if enableScheduler {
    schPath := filepath.Join(dataRoot, "scheduled_tasks.json")
    schMgr, err := scheduler.NewManager(schPath)
    if err == nil {
        schMgr.StartWithExecutor(func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
            // 使用 service 的 SendMessage 执行
            // 需要一个 "system" principal
            ...
        })
        defer schMgr.Stop()
    }
}
```

## 修改文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/scheduler/task.go` | 修改 | 新增 `StartWithExecutor()` 方法 |
| `gui/app.go` | 修改 | `ensureScheduledTaskManager()` 不调用 `Start()`，改为 `StartWithExecutor(localExecutor)` |
| `gui/app.go` | 修改 | `createAndWireHubClient()` 中 `SetExecutor` 覆盖为 Hub 感知版本（不再调用 Start） |
| `gui/app_scheduled_task_executor.go` | 新增 | `buildLocalScheduledTaskExecutor()` + `buildHubScheduledTaskExecutor()` |
| `tui/app.go` | 修改 | `TUIApp` 新增 `scheduledTaskManager` 字段；初始化 + `StartWithExecutor` |
| `tui/app.go` | 修改 | `ExtraHandlers` 新增 `"manage_schedule"` |
| `tui/agent_tools_schedule.go` | 新增 | TUI 侧 manage_schedule handler + CRUD 实现 + executor |
| `MaClawSrv/main.go` | 修改 | 可选调度器支持（`MACLAW_ENABLE_SCHEDULER`） |
| `MaClawSrv/scheduler.go` | 新增 | MaClawSrv 调度器初始化 + HTTP 端点 |

## 验收标准

- GUI 纯桌面用户（不连 Hub）：定时任务正常触发执行
- GUI Hub 用户：定时任务触发 + 结果推送到 IM
- GUI catch-up runs：进程重启后，错过的 process 类型任务补执行
- TUI：`manage_schedule` 工具可用，定时任务正常触发
- TUI CLI：`maclaw-tui schedule list/create/delete` 行为不变
- MaClawSrv：通过环境变量启用调度器后，定时任务正常触发
- 所有现有 scheduler 测试通过
