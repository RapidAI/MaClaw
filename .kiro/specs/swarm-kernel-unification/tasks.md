# 实施任务：Swarm Kernel Unification

## 任务总览

分为 4 个阶段：接口定义 → 核心逻辑迁移 → 适配层实现 → 清理与验证。每个阶段结束后全项目编译通过。

---

## 阶段 1：接口定义与类型统一

### Task 1: 定义核心接口 (`corelib/swarm/interfaces.go`)
- [x] 创建 `corelib/swarm/interfaces.go`
- [x] 定义 `SwarmSessionManager` 接口（Create/Get/Kill/WriteInput）
- [x] 定义 `SwarmSession` 接口（SessionID/SessionStatus/SessionSummary/SessionOutput）
- [x] 定义 `SwarmAppContext` 接口（ListInstalledTools）
- [x] 定义 `SwarmLLMCaller` 接口（CallLLM）
- [x] 定义 `SwarmLaunchSpec` 结构体
- [x] 定义 `SwarmSessionSummary` 结构体
- [x] 定义 `InstalledToolInfo` 结构体
- [x] 定义 `SessionStatus` 类型别名（重新导出 `remote.SessionStatus` 及常量）
- [x] 确保 `go build ./corelib/swarm/...` 通过

**Requirements: 1, 2, 5, 8**

### Task 2: GUI 类型别名切换
- [x] 在 `gui/corelib_aliases.go` 中添加所有 swarm 类型别名（SwarmRun、SwarmAgent、SubTask、SwarmRunRequest、SwarmRunSummary、SwarmReport 等全部类型、常量）
- [x] 删除 `gui/swarm_types.go`（所有类型定义已在 `corelib/swarm/types.go`）
- [x] 修复 `gui/` 包中因类型来源变更导致的编译错误（如有）
- [x] 确保 `go build ./gui/...` 通过
- [x] 确保 `go test ./gui/...` 通过（现有测试不回归）

**Requirements: 7**

---

## 阶段 2：核心逻辑迁移

### Task 3: 迁移 TaskSplitter
- [x] 将 `gui/swarm_task_splitter.go` 逻辑迁移到 `corelib/swarm/task_splitter.go`
- [x] 将构造函数改为 `NewTaskSplitter(caller SwarmLLMCaller)`
- [x] 将 `callLLM` 方法改为调用 `s.caller.CallLLM`
- [x] 当 `caller` 为 nil 时返回描述性 error
- [x] 迁移 `extractJSON` 辅助函数到 `corelib/swarm/json_helpers.go`
- [x] 迁移 `gui/swarm_task_splitter_test.go` 到 `corelib/swarm/task_splitter_test.go`（调整 package 和 mock）
- [x] 删除 `gui/swarm_task_splitter.go`，GUI 通过 corelib/swarm 引用
- [x] 确保 `go build ./...` 通过

**Requirements: 3, 8**

### Task 4: 迁移 FeedbackLoop
- [x] 将 `gui/swarm_feedback.go` 逻辑迁移到 `corelib/swarm/feedback.go`
- [x] 将构造函数改为 `NewFeedbackLoop(caller SwarmLLMCaller, maxRounds int)`
- [x] 当 `caller` 为 nil 时 `ClassifyFailures` 返回描述性 error
- [x] 迁移 `gui/swarm_feedback_test.go` 到 `corelib/swarm/feedback_test.go`
- [x] 删除 `gui/swarm_feedback.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3, 8**

### Task 5: 迁移 TaskVerifier
- [x] 将 `gui/swarm_task_verifier.go` 逻辑迁移到 `corelib/swarm/task_verifier.go`
- [x] 将 `TaskVerdict` 类型迁移到 `corelib/swarm/types.go`
- [x] 将构造函数改为 `NewTaskVerifier(caller SwarmLLMCaller)`
- [x] 迁移 `extractJSONObject`、`truncateForPrompt` 辅助函数
- [x] 迁移 `VerifyByTest` 及其 shell 执行辅助函数（`runTestShellCommand` 等）
- [x] 迁移 `gui/swarm_task_verifier_test.go`
- [x] 删除 `gui/swarm_task_verifier.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3, 8**

### Task 6: 迁移 MergeController
- [x] 将 `gui/swarm_merge.go` 逻辑迁移到 `corelib/swarm/merge.go`
- [x] 将 `runGit`/`runGitOutput` 调用替换为 `swarmRunGit`/`swarmRunGitOutput`（已在 worktree.go 中）
- [x] 将 `runShellCommand` 迁移为 `corelib/swarm/` 包内函数
- [x] 移除 `hideCommandWindow` 依赖（该函数是 GUI 特有的 Windows 窗口隐藏，corelib 中不需要）
- [x] 迁移 `gui/swarm_merge_test.go`
- [x] 删除 `gui/swarm_merge.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

### Task 7: 迁移 SwarmReporter
- [x] 将 `gui/swarm_reporter.go` 逻辑迁移到 `corelib/swarm/reporter.go`
- [x] 迁移 `MarshalReport`/`UnmarshalReport` 函数
- [x] 迁移 `gui/swarm_reporter_test.go`
- [x] 删除 `gui/swarm_reporter.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

### Task 8: 迁移 SwarmDocGenerator
- [x] 将 `gui/swarm_doc_generator.go` 逻辑迁移到 `corelib/swarm/doc_generator.go`
- [x] 将 `DocType` 常量迁移到 `corelib/swarm/types.go`
- [x] 迁移 `gui/swarm_doc_generator_test.go`
- [x] 删除 `gui/swarm_doc_generator.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

### Task 9: 迁移 SwarmPrompts
- [x] 将 `gui/swarm_prompts.go` 逻辑迁移到 `corelib/swarm/prompts.go`
- [x] 迁移 `rolePromptTemplates`、`specPromptTemplates` 变量
- [x] 迁移 `RenderPrompt`、`RenderSpecPrompt` 函数
- [x] 迁移 `gui/swarm_prompts_test.go`
- [x] 删除 `gui/swarm_prompts.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

### Task 10: 迁移 Agent Scheduler
- [x] 将 `gui/swarm_agent_scheduler.go` 逻辑迁移到 `corelib/swarm/agent_scheduler.go`
- [x] 将 `o.manager.Create(LaunchSpec{...})` 替换为 `o.sessionMgr.Create(SwarmLaunchSpec{...})`
- [x] 将 `o.manager.Get(id)` + `s.mu.RLock()` 替换为 `o.sessionMgr.Get(id)` + `session.SessionStatus()`
- [x] 将 `o.manager.Kill(id)` 替换为 `o.sessionMgr.Kill(id)`
- [x] 将 `isActiveRemoteSessionStatus` 替换为基于 `SessionStatus` 常量的判断
- [x] 迁移 `gui/swarm_agent_scheduler_test.go`
- [x] 删除 `gui/swarm_agent_scheduler.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

### Task 11: 迁移 SwarmOrchestrator 核心
- [x] 将 `gui/swarm_orchestrator.go` 核心逻辑迁移到 `corelib/swarm/orchestrator.go`
- [x] 实现 `NewSwarmOrchestrator(sessionMgr, notifier, ...OrchestratorOption)` 构造函数
- [x] 实现 `OrchestratorOption` 函数选项（WithAppContext、WithLLMCaller、WithMaxRounds、WithMaxAgents）
- [x] 将 `installedToolNames()` 改为通过 `o.appCtx.ListInstalledTools()` 获取
- [x] 将 `selectToolForTask` 改为使用 `corelib/tool.Selector`
- [x] 保留 `SetIMDelivery`/`ClearIMDelivery` 方法（委托给 DefaultNotifier）
- [x] 迁移 `gui/swarm_orchestrator_test.go`
- [x] 删除 `gui/swarm_orchestrator.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

### Task 12: 迁移 Pipeline
- [x] 将 `gui/swarm_pipeline_greenfield.go` 迁移到 `corelib/swarm/pipeline_greenfield.go`
- [x] 将 `gui/swarm_pipeline.go`（maintenance pipeline）迁移到 `corelib/swarm/pipeline_maintenance.go`
- [x] 将 pipeline 中的 `o.manager` 调用替换为 `o.sessionMgr` 接口调用
- [x] 将 `swarmCallLLM` 调用替换为 `o.llmCaller.CallLLM`
- [x] 迁移 `gui/swarm_spec_pipeline_test.go` 和 `gui/swarm_tdd_test.go`
- [x] 删除 `gui/swarm_pipeline_greenfield.go` 和 `gui/swarm_pipeline.go`
- [x] 确保 `go build ./...` 通过

**Requirements: 3**

---

## 阶段 3：适配层实现

### Task 13: GUI 适配层
- [x] 创建 `gui/swarm_adapters.go`
- [x] 实现 `GUISessionAdapter`（SwarmSessionManager 接口，委托 RemoteSessionManager）
- [x] 实现 `GUISessionWrapper`（SwarmSession 接口，包装 RemoteSession，持有 mu 读锁返回状态）
- [x] 实现 `GUIAppContext`（SwarmAppContext 接口，委托 App.ListRemoteToolMetadata）
- [x] 实现 `GUILLMCaller`（SwarmLLMCaller 接口，使用 MaclawLLMConfig + doSimpleLLMRequest）
- [x] 编写 `gui/swarm_adapters_test.go`（LaunchSpec 转换、SessionStatus 读锁安全）
- [x] 确保 `go build ./gui/...` 和 `go test ./gui/...` 通过

**Requirements: 4**

### Task 14: 修改 GUI Wails 绑定
- [x] 修改 `gui/app_swarm_bindings.go` 中的 `ensureSwarmOrchestrator`，使用适配器 + `swarm.NewSwarmOrchestrator`
- [x] 修改 `wireSwarmIMDelivery`，通过 `swarm.DefaultNotifier.SetIMDelivery` 注入
- [x] 确保所有 Wails 绑定方法签名不变（StartSwarmRun、PauseSwarmRun 等）
- [x] 确保前端事件名称不变（swarm:phase_change、swarm:agent_complete 等）
- [x] 确保 `go build ./gui/...` 和 `go test ./gui/...` 通过

**Requirements: 4, 9**

### Task 15: TUI 适配层
- [x] 创建 `tui/swarm_adapters.go`
- [x] 实现 `TUISwarmSessionAdapter`（SwarmSessionManager 接口，委托 TUISessionManager）
- [x] 实现 `TUISwarmSessionWrapper`（SwarmSession 接口，包装 TUISession）
- [x] 实现 `TUIAppContext`（SwarmAppContext 接口，扫描本地工具二进制）
- [x] 实现 `TUILLMCaller`（SwarmLLMCaller 接口，使用 TUI 端 LLM 配置）
- [x] 实现 `TUINotifier`（Notifier 接口，格式化输出到终端）
- [x] 编写 `tui/swarm_adapters_test.go`
- [x] 确保 `go build ./tui/...` 和 `go test ./tui/...` 通过

**Requirements: 5, 10**

### Task 16: TUI swarm 命令重写
- [x] 重写 `tui/commands/swarm.go`，使用 `swarm.SwarmOrchestrator` 替代 `misc.TaskOrchestrator`
- [x] 实现 `swarmCreate`：支持 `--mode greenfield --requirements <file>` 和 `--mode maintenance --tasks <file>`
- [x] 实现 `swarmStatus`：通过 `GetSwarmRun` 展示阶段、Agent 状态、轮次信息
- [x] 实现 `swarmCancel`：通过 `CancelSwarmRun` 取消并清理
- [x] 实现 `swarmList`：通过 `ListSwarmRuns` 列出历史
- [x] 移除对 `corelib/misc.TaskOrchestrator` 的 swarm 相关依赖
- [x] 确保 `go build ./tui/...` 通过

**Requirements: 6**

---

## 阶段 4：清理与验证

### Task 17: 删除 GUI 重复文件
- [x] 删除 `gui/swarm_notifier.go`（已由 `corelib/swarm/notifier.go` 替代）
- [x] 删除 `gui/swarm_worktree.go`（已由 `corelib/swarm/worktree.go` 替代）
- [x] 删除 `gui/swarm_conflict.go`（已由 `corelib/swarm/conflict.go` 替代）
- [x] 删除 `gui/swarm_llm.go`（LLM 调用已通过 SwarmLLMCaller 接口抽象）
- [x] 删除对应的 GUI 测试文件（`gui/swarm_notifier_test.go`、`gui/swarm_worktree_test.go`、`gui/swarm_conflict_test.go`）
- [x] 修复删除后的编译错误（如有 GUI 代码仍引用已删除的函数，改为引用 corelib/swarm）
- [x] 确保 `go build ./...` 通过

**Requirements: 7, 10**

### Task 18: 全项目编译与测试验证
- [x] `go build ./...` 全项目编译通过
- [x] `go test ./corelib/swarm/...` 所有 swarm 包测试通过
- [x] `go test ./gui/...` 所有 GUI 测试通过
- [x] `go test ./tui/...` 所有 TUI 测试通过
- [x] 验证 `corelib/swarm/` 包不导入 `gui/` 或 `tui/`（`go list -f '{{.Imports}}' ./corelib/swarm/` 检查）
- [x] 验证报告文件路径（`.maclaw-swarm/{run_id}/`）和格式（report.md、report.json、timeline.md）不变
- [x] 验证 worktree 路径（`../.maclaw-workers/{run_id}/`）和分支命名（`swarm/{run_id}/{role}-{task_index}`）不变

**Requirements: 9**
