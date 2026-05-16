# 需求文档：Swarm Kernel Unification（蜂群内核统一）

## 简介

当前 Swarm 编排器存在两套完全独立的实现：GUI 端拥有完整的 `SwarmOrchestrator`（位于 `gui/swarm_*.go`），包含 Greenfield/Maintenance 双模式 pipeline、Agent 调度、冲突检测、反馈循环、TDD 验证等全部能力；TUI 端仅使用 `corelib/misc/TaskOrchestrator` 做简单的计划执行，无 worktree 管理、无真实 Agent 会话、无冲突检测。

本特性将 GUI 端的 SwarmOrchestrator 核心逻辑提取到 `corelib/swarm/` 包，通过接口抽象会话管理和应用上下文依赖，使 TUI 和 GUI 共享同一个编排内核。GUI 端变为薄适配层，TUI 端获得与 GUI 完全一致的蜂群能力。

本特性基于已完成的 `swarm-orchestrator` spec（`.kiro/specs/swarm-orchestrator/`），聚焦于内核提取与双端适配工作。

## 术语表

- **Swarm_Kernel**: 蜂群编排内核，位于 `corelib/swarm/` 包中的核心编排逻辑，不依赖任何 GUI 或 TUI 特定类型
- **SwarmSessionManager**: 会话管理抽象接口，定义 Create/Get/Kill/WriteInput/ListSessions 等方法，由 GUI 和 TUI 各自实现
- **SwarmSession**: 会话抽象接口，定义 SessionID/Status/Summary/Output 等方法，屏蔽 GUI `RemoteSession` 和 TUI `TUISession` 的差异
- **SwarmAppContext**: 应用上下文抽象接口，提供 ListInstalledTools 等编排器需要但与具体 UI 框架绑定的能力
- **GUI_Adapter**: GUI 端适配层，将 `RemoteSessionManager`/`RemoteSession` 包装为 `SwarmSessionManager`/`SwarmSession` 接口实现
- **TUI_Adapter**: TUI 端适配层，将 `TUISessionManager`/`TUISession` 包装为 `SwarmSessionManager`/`SwarmSession` 接口实现
- **Orchestrator**: `corelib/swarm/` 中的 `SwarmOrchestrator` 结构体，通过接口依赖注入会话管理和应用上下文
- **Pipeline**: 编排器的阶段推进逻辑，包括 Greenfield pipeline 和 Maintenance pipeline
- **TaskOrchestrator**: `corelib/misc/TaskOrchestrator`，TUI 当前使用的轻量级计划执行器，统一后将被替换
- **LaunchSpec**: `corelib/remote/` 中定义的会话启动参数结构体，GUI 和 TUI 共用
- **SessionStatus**: `corelib/remote/` 中定义的会话状态枚举（starting/running/exited/error 等），GUI 和 TUI 共用

## 需求

### 需求 1：会话管理接口抽象

**用户故事：** 作为 corelib/swarm 包的使用者，我希望编排器通过接口访问会话管理能力，以便 GUI 和 TUI 能各自提供不同的会话实现。

#### 验收标准

1. THE Swarm_Kernel SHALL 在 `corelib/swarm/` 包中定义 `SwarmSessionManager` 接口，包含以下方法：`Create(spec SwarmLaunchSpec) (SwarmSession, error)`、`Get(sessionID string) (SwarmSession, bool)`、`Kill(sessionID string) error`、`WriteInput(sessionID string, text string) error`
2. THE Swarm_Kernel SHALL 在 `corelib/swarm/` 包中定义 `SwarmSession` 接口，包含以下方法：`SessionID() string`、`SessionStatus() SessionStatus`、`SessionSummary() SwarmSessionSummary`、`SessionOutput() string`
3. THE Swarm_Kernel SHALL 定义 `SwarmLaunchSpec` 结构体，包含编排器创建会话所需的字段：Tool、ProjectPath、Env（map[string]string）、LaunchSource，不依赖 GUI 的 `LaunchSpec` 或 TUI 的具体类型
4. THE Swarm_Kernel SHALL 定义 `SwarmSessionSummary` 结构体，包含编排器监控会话所需的字段：Status、ProgressSummary、LastResult、WaitingForUser、UpdatedAt
5. THE Swarm_Kernel SHALL 定义 `SessionStatus` 类型别名或重新导出 `corelib/remote.SessionStatus`，使编排器代码无需直接导入 `corelib/remote` 包中的会话状态常量

### 需求 2：应用上下文接口抽象

**用户故事：** 作为 corelib/swarm 包的使用者，我希望编排器通过接口访问应用级能力（如已安装工具列表），以便不同 UI 端能各自提供实现。

#### 验收标准

1. THE Swarm_Kernel SHALL 在 `corelib/swarm/` 包中定义 `SwarmAppContext` 接口，包含方法：`ListInstalledTools() []InstalledToolInfo`
2. THE Swarm_Kernel SHALL 定义 `InstalledToolInfo` 结构体，包含 Name（string）和 CanStart（bool）字段
3. WHEN SwarmAppContext 为 nil 时，THE Orchestrator SHALL 回退到默认行为（使用配置中指定的工具或 "claude"），不产生 panic

### 需求 3：编排器核心逻辑迁移

**用户故事：** 作为开发者，我希望 SwarmOrchestrator 的核心逻辑位于 `corelib/swarm/` 包中，以便 GUI 和 TUI 共享同一份代码。

#### 验收标准

1. THE Swarm_Kernel SHALL 将以下组件从 `gui/` 迁移到 `corelib/swarm/`：SwarmOrchestrator（核心调度器）、Greenfield pipeline、Maintenance pipeline、Agent scheduler（createAgent/waitForAgent/runDeveloperAgents）
2. THE Swarm_Kernel SHALL 将以下子组件从 `gui/` 迁移到 `corelib/swarm/`：MergeController、FeedbackLoop、SwarmReporter、TaskSplitter、TaskVerifier、ToolSelector、SwarmDocGenerator、SwarmPrompts（prompt 模板与渲染）、swarmCallLLM（LLM 调用辅助函数）
3. THE Swarm_Kernel SHALL 将 `gui/swarm_types.go` 中的所有类型定义统一到 `corelib/swarm/types.go`，消除 GUI 包中的重复类型定义
4. THE Swarm_Kernel SHALL 将 `gui/swarm_notifier.go` 中的 `DefaultSwarmNotifier` 统一到 `corelib/swarm/notifier.go`，消除 GUI 包中的重复通知器实现
5. THE Swarm_Kernel SHALL 将 `gui/swarm_worktree.go` 中的 `WorktreeManager` 统一到 `corelib/swarm/worktree.go`，消除 GUI 包中的重复 worktree 管理实现
6. WHEN 迁移完成后，`corelib/swarm/` 包 SHALL 不导入 `gui/` 包或 `tui/` 包中的任何类型
7. THE Swarm_Kernel 中的 `NewSwarmOrchestrator` 构造函数 SHALL 接受接口参数（SwarmSessionManager、SwarmAppContext、Notifier）而非具体类型（*App、*RemoteSessionManager）

### 需求 4：GUI 适配层

**用户故事：** 作为 GUI 端开发者，我希望通过薄适配层将现有的 RemoteSessionManager 接入 corelib 编排内核，以便 GUI 继续正常工作。

#### 验收标准

1. THE GUI_Adapter SHALL 实现 `SwarmSessionManager` 接口，内部委托给现有的 `RemoteSessionManager`
2. THE GUI_Adapter SHALL 实现 `SwarmSession` 接口，内部包装现有的 `RemoteSession`，将 `RemoteSession` 的 `mu`-保护字段通过接口方法安全暴露
3. THE GUI_Adapter SHALL 实现 `SwarmAppContext` 接口，内部委托给 `App.ListRemoteToolMetadata()` 方法
4. WHEN GUI 端调用 `StartSwarmRun` 时，THE GUI_Adapter SHALL 将 `SwarmLaunchSpec` 转换为 GUI 的 `LaunchSpec`，保留所有必要字段（Tool、ProjectPath、Env、LaunchSource）
5. THE GUI_Adapter SHALL 保持现有的 Wails 前端绑定（`app_swarm_bindings.go`）不变，仅修改内部委托目标为 `corelib/swarm.SwarmOrchestrator`
6. WHEN GUI_Adapter 的 SwarmSession.SessionStatus() 被调用时，THE GUI_Adapter SHALL 在持有 `RemoteSession.mu` 读锁的情况下返回状态值

### 需求 5：TUI 适配层

**用户故事：** 作为 TUI 端开发者，我希望通过适配层将 TUISessionManager 接入 corelib 编排内核，以便 TUI 获得与 GUI 一致的蜂群能力。

#### 验收标准

1. THE TUI_Adapter SHALL 实现 `SwarmSessionManager` 接口，内部委托给现有的 `TUISessionManager`
2. THE TUI_Adapter SHALL 实现 `SwarmSession` 接口，内部包装现有的 `TUISession`，将 `TUISession` 的 `mu`-保护字段通过接口方法安全暴露
3. THE TUI_Adapter SHALL 实现 `SwarmAppContext` 接口，通过扫描本地已安装的编程工具二进制文件来返回工具列表
4. WHEN TUI 端调用 `SwarmSessionManager.Create` 时，THE TUI_Adapter SHALL 将 `SwarmLaunchSpec` 转换为 `remote.LaunchSpec`，传递给 `TUISessionManager.Create`

### 需求 6：TUI swarm 命令重写

**用户故事：** 作为 TUI 用户，我希望 `maclaw-tui swarm` 命令使用真正的 SwarmOrchestrator，以便获得与 GUI 一致的蜂群能力（worktree 管理、冲突检测、反馈循环等）。

#### 验收标准

1. WHEN 用户执行 `maclaw-tui swarm create` 时，THE TUI SHALL 使用 `corelib/swarm.SwarmOrchestrator` 创建并执行 Swarm_Run，替代当前的 `corelib/misc.TaskOrchestrator`
2. WHEN 用户执行 `maclaw-tui swarm status` 时，THE TUI SHALL 通过 `corelib/swarm.SwarmOrchestrator.GetSwarmRun` 获取 Run 详情，展示阶段、Agent 状态、轮次信息
3. WHEN 用户执行 `maclaw-tui swarm cancel` 时，THE TUI SHALL 通过 `corelib/swarm.SwarmOrchestrator.CancelSwarmRun` 取消 Run，触发 worktree 清理和报告生成
4. WHEN 用户执行 `maclaw-tui swarm list` 时，THE TUI SHALL 通过 `corelib/swarm.SwarmOrchestrator.ListSwarmRuns` 列出所有 Run 历史
5. THE TUI SHALL 支持 `maclaw-tui swarm create --mode greenfield --requirements <file>` 和 `maclaw-tui swarm create --mode maintenance --tasks <file>` 两种模式
6. THE TUI SHALL 在 swarm 执行过程中将进度通知输出到终端（通过 Notifier 接口的 TUI 实现）
7. WHEN TUI swarm 命令重写完成后，THE TUI SHALL 不再依赖 `corelib/misc.TaskOrchestrator` 进行 swarm 操作

### 需求 7：类型统一与去重

**用户故事：** 作为代码维护者，我希望 Swarm 相关类型只有一份权威定义，以便避免 GUI 和 corelib 之间的类型重复和不一致。

#### 验收标准

1. WHEN 迁移完成后，`gui/swarm_types.go` SHALL 被删除或替换为类型别名文件（类似现有的 `gui/corelib_aliases.go` 模式），所有类型定义以 `corelib/swarm/types.go` 为权威来源
2. WHEN 迁移完成后，`gui/swarm_notifier.go` 中的 `DefaultSwarmNotifier` SHALL 被删除，GUI 改用 `corelib/swarm.DefaultNotifier` 并通过构造函数注入 EventEmitter
3. WHEN 迁移完成后，`gui/swarm_worktree.go` SHALL 被删除，GUI 改用 `corelib/swarm.WorktreeManager`
4. THE Swarm_Kernel SHALL 确保 `corelib/swarm/types.go` 中的 `SwarmRun.UserInputCh` 字段（原 `userInputCh`）保持 `json:"-"` 标签，不参与序列化
5. FOR ALL 在 `gui/swarm_types.go` 和 `corelib/swarm/types.go` 中重复定义的类型，迁移后 SHALL 只保留 `corelib/swarm/types.go` 中的定义

### 需求 8：LLM 调用抽象

**用户故事：** 作为 corelib/swarm 包的使用者，我希望编排器通过接口访问 LLM 调用能力，以便不同端能提供各自的 LLM 配置和调用实现。

#### 验收标准

1. THE Swarm_Kernel SHALL 定义 `SwarmLLMCaller` 接口，包含方法：`CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error)`
2. THE Swarm_Kernel SHALL 将 `gui/swarm_llm.go` 中的 `swarmCallLLM` 函数逻辑迁移为 `SwarmLLMCaller` 接口的默认实现
3. WHEN SwarmLLMCaller 为 nil 时，THE Orchestrator 中依赖 LLM 的组件（TaskSplitter、FeedbackLoop、TaskVerifier）SHALL 返回描述性错误而非 panic
4. THE GUI_Adapter 和 TUI_Adapter SHALL 各自提供 `SwarmLLMCaller` 实现，使用各端已有的 LLM 配置（GUI 使用 `MaclawLLMConfig`，TUI 使用 TUI 端的 LLM 配置）

### 需求 9：向后兼容性

**用户故事：** 作为现有用户，我希望内核统一后 GUI 端的蜂群功能行为不变，以便升级过程无感知。

#### 验收标准

1. WHEN 内核统一完成后，GUI 端通过 Wails 绑定调用的所有 Swarm API（StartSwarmRun、PauseSwarmRun、ResumeSwarmRun、CancelSwarmRun、ListSwarmRuns、GetSwarmRun、ProvideSwarmUserInput）SHALL 保持相同的函数签名和返回类型
2. WHEN 内核统一完成后，GUI 前端推送的所有 Swarm 事件名称（swarm:phase_change、swarm:agent_complete、swarm:failure、swarm:waiting_user、swarm:run_complete、swarm:document_review）SHALL 保持不变
3. WHEN 内核统一完成后，Swarm 报告文件的存储路径（`.maclaw-swarm/{run_id}/`）和文件格式（report.md、report.json、timeline.md）SHALL 保持不变
4. WHEN 内核统一完成后，Worktree 的存储路径（`../.maclaw-workers/{run_id}/`）和分支命名格式（`swarm/{run_id}/{role}-{task_index}`）SHALL 保持不变
5. FOR ALL 有效的 SwarmReport JSON 数据，内核统一前后的序列化/反序列化结果 SHALL 等价（round-trip 属性保持）

### 需求 10：Notifier 接口统一

**用户故事：** 作为 corelib/swarm 包的使用者，我希望通知器接口在 corelib 中有唯一定义，以便 GUI 和 TUI 各自提供不同的通知实现。

#### 验收标准

1. THE Swarm_Kernel SHALL 在 `corelib/swarm/notifier.go` 中保留唯一的 `Notifier` 接口定义，GUI 和 TUI 各自实现该接口
2. THE Swarm_Kernel SHALL 在 `corelib/swarm/notifier.go` 中保留 `DefaultNotifier`（基于 EventEmitter 回调）和 `NoopNotifier`（静默丢弃）两个默认实现
3. WHEN GUI 端使用 Notifier 时，THE GUI_Adapter SHALL 构造 `DefaultNotifier` 并注入 `App.emitEvent` 作为 EventEmitter
4. WHEN TUI 端使用 Notifier 时，THE TUI_Adapter SHALL 构造一个 TUI 专用 Notifier 实现，将通知格式化输出到终端
5. THE Swarm_Kernel 中的 `DefaultNotifier` SHALL 支持 `SetIMDelivery` 方法，保持 IM 文件/文本投递回调能力
