# 实现计划：浏览器回放接入后台任务管理

## 概述

将 `browser_task_replay` 从同步阻塞模式改为异步后台执行，通过 `BackgroundLoopManager` 管理回放任务的生命周期，支持 GUI 进度展示、暂停/取消控制、完成通知和定时回放。实现语言为 Go。

## 任务列表

- [x] 1. 新增 TaskStatusPaused/TaskStatusCancelled 状态与 Pause/Resume 方法
  - [x] 1.1 在 `corelib/browser/task_types.go` 中新增 `TaskStatusCancelled` 状态常量（TaskStatusPaused 已存在）
  - [x] 1.2 在 `taskEntry` 结构体中新增 `pauseC chan struct{}`、`resumeC chan struct{}` 字段
  - [x] 1.3 实现 `BrowserTaskSupervisor.Pause(taskID string) error` 方法
  - [x] 1.4 实现 `BrowserTaskSupervisor.Resume(taskID string) error` 方法
  - [x] 1.5 修改 `Execute()` 步骤循环，在每个步骤完成后检查 `pauseC` 信号
  - [ ]* 1.6 编写属性测试：Pause/Resume 保持任务连续性

- [x] 2. 检查点 - 暂停/恢复机制编译通过

- [x] 3. 新增 `replay_background.go` 核心后台回放逻辑
  - [x] 3.1 定义 `ActivityUpdater` 和 `LoopManager` 接口
  - [x] 3.2 实现 `RunReplayInBackground()` 函数
  - [x] 3.3 实现 `notifyReplayComplete()` 通知函数
  - [x] 3.4 定义 `ScheduledReplayAction` 结构体
  - [ ]* 3.5 编写属性测试：异步提交返回任务 ID
  - [ ]* 3.6 编写属性测试：完成通知包含必要信息

- [x] 4. 改造 `browser_task_replay` Handler 为异步模式
  - [x] 4.1 修改 `RegisterRecorderTools` 签名，新增 loopMgr/activityStore/statusC/logger 参数
  - [x] 4.2 改造 handler：slot 可用时后台执行，slot 满时排队
  - [x] 4.3 更新 `gui/tools_browser.go` 调用点
  - [x] 4.4 新增 `bgLoopManagerAdapter` 适配器
  - [ ]* 4.5 编写属性测试：Slot 满时排队

- [x] 5. 检查点 - 后台回放核心流程编译通过

- [x] 6. AgentActivityStore 扩展与 GUI 集成
  - [x] 6.1 在 `gui/agent_activity.go` 中为 `"browser_replay"` source 添加 `"浏览器回放"` 标签
  - [x] 6.2 在 `gui/tools_browser.go` 中实现 `replayActivityAdapter`（满足 `ActivityUpdater` 接口）
  - [ ]* 6.3 编写属性测试：AgentActivityStore 状态同步
  - [ ]* 6.4 编写属性测试：ListViews 包含浏览器回放任务

- [x] 7. 取消控制集成
  - [x] 7.1 在 `RunReplayInBackground` 中监听 `loopCtx.CancelC`，传递到 supervisor.Cancel()
  - [ ]* 7.2 编写属性测试：Cancel 释放 Slot 并标记取消
  - [ ]* 7.3 编写属性测试：Slot 释放后自动调度队列

- [x] 8. 检查点 - 取消和 GUI 集成编译通过

- [x] 9. browser_task_status 工具扩展
  - [x] 9.1 扩展 handler 支持查询后台回放任务（BackgroundLoopManager 查询）
  - [x] 9.2 支持空 task_id 列出所有浏览器后台任务
  - [x] 9.3 更新 `RegisterTaskTools` 签名新增 loopMgr 参数
  - [ ]* 9.4 编写属性测试：状态查询返回正确信息

- [x] 10. 定时回放桥接
  - [x] 10.1 定义 `ScheduledReplayAction` 结构体（在 replay_background.go）
  - [x] 10.2 新建 `gui/browser_replay_scheduler.go`，实现 executor 回调桥接
  - [x] 10.3 实现 `wrapExecutorWithReplay()` 包装函数
  - [ ]* 10.4 编写属性测试：定时回放任务创建格式正确
  - [ ]* 10.5 编写属性测试：定时任务持久化 Round Trip
  - [ ]* 10.6 编写属性测试：定时回放执行结果更新

- [x] 11. 最终检查点 - 所有代码编译通过（go vet）

## 备注

- 标记 `*` 的子任务为可选属性测试，可跳过以加速 MVP 交付
- StatusEvent 新增 Extra map[string]string 字段用于携带截图等元数据
- Cancel 方法已扩展支持 paused 状态的任务取消
