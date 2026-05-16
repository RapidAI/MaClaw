# 实现计划：Swarm Orchestrator（蜂群编排器）

## 概述

基于设计文档中定义的组件和接口，将蜂群编排器分解为增量实现步骤。每个步骤构建在前一步骤之上，最终将所有组件连接到 SwarmOrchestrator 核心调度器中。使用 Go 语言实现，属性测试使用 [rapid](https://github.com/flyingmutant/rapid) 库。

## 任务

- [x] 1. 定义数据模型和核心类型
  - [x] 1.1 创建 `swarm_types.go`，定义所有数据模型
    - 定义 SwarmMode、SwarmStatus、SwarmPhase、AgentRole 等常量类型
    - 定义 SwarmRun、SwarmAgent、SubTask、TaskGroup、WorktreeInfo、ProjectState 结构体
    - 定义 SwarmRound、BranchInfo、MergeResult 结构体
    - 定义 TestFailure、ClassifiedFailure、FailureType 类型
    - 定义 SwarmReport、ReportStatistics、AgentRecord、TimelineEvent 结构体
    - 定义 SwarmRunRequest、TaskListInput、SwarmRunSummary 结构体
    - 定义 PromptTemplate、PromptContext 结构体
    - _需求: 1.1, 1.5, 2.1, 2.5, 7.1, 10.1, 11.1_

  - [x]* 1.2 编写属性测试：Run ID 唯一性
    - **Property 25: Run ID 唯一性**
    - **验证: 需求 11.1**

- [x] 2. 实现 WorktreeManager（Git Worktree 管理器）
  - [x] 2.1 创建 `swarm_worktree.go`，实现 WorktreeManager
    - 实现 PrepareProject：检测 git 仓库、自动 init/commit、stash 未提交改动
    - 实现 CreateWorktree：在 `../.maclaw-workers/{run_id}/` 下创建 worktree 和分支
    - 实现 RemoveWorktree：删除指定 worktree 和分支
    - 实现 CleanupRun：清理整个 Run 的所有 worktree
    - 实现 RestoreProject：执行 stash pop 恢复用户改动
    - 所有操作仅使用本地 git 命令，不依赖远程仓库
    - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_

  - [x]* 2.2 编写属性测试：Worktree 命名规范
    - **Property 5: Worktree 命名规范**
    - **验证: 需求 3.4, 3.5**

  - [x]* 2.3 编写属性测试：Worktree 清理完整性
    - **Property 6: Worktree 清理完整性**
    - **验证: 需求 3.6**

  - [x]* 2.4 编写属性测试：Stash 往返恢复
    - **Property 7: Stash 往返恢复**
    - **验证: 需求 3.7**

- [x] 3. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 4. 实现 ConflictDetector（冲突检测器）
  - [x] 4.1 创建 `swarm_conflict.go`，实现 ConflictDetector
    - 实现 DetectConflicts：分析任务列表的 ExpectedFiles 交集，返回 TaskGroup 分组
    - 实现 BuildDependencyGraph：构建文件依赖图
    - 复用现有 ProjectScanner 分析代码库的导入关系和模块依赖
    - _需求: 4.1, 4.2, 4.3, 4.4, 4.5_

  - [x]* 4.2 编写属性测试：冲突分组正确性
    - **Property 8: 冲突分组正确性**
    - **验证: 需求 4.2**

  - [x]* 4.3 编写属性测试：组间无文件冲突
    - **Property 9: 组间无文件冲突**
    - **验证: 需求 4.3**

- [x] 5. 实现 TaskSplitter（任务分解器）
  - [x] 5.1 创建 `swarm_task_splitter.go`，实现 TaskSplitter
    - 实现 SplitRequirements：调用 LLM 将产品需求分解为 SubTask 列表（Greenfield 模式）
    - 实现 ParseTaskList：解析手动输入文本、GitHub Issues URL 等来源的任务列表（Maintenance 模式）
    - 每个 SubTask 包含 Description、ExpectedFiles、Dependencies
    - _需求: 1.2, 2.2, 2.3_

  - [x]* 5.2 编写属性测试：子任务结构完整性
    - **Property 2: 子任务结构完整性**
    - **验证: 需求 1.2**

  - [x]* 5.3 编写属性测试：手动输入解析完整性
    - **Property 3: 手动输入解析完整性**
    - **验证: 需求 2.3**

  - [x]* 5.4 编写属性测试：Maintenance 模式创建正确性
    - **Property 4: Maintenance 模式创建正确性**
    - **验证: 需求 2.1**

- [x] 6. 实现 MergeController（合并控制器）
  - [x] 6.1 创建 `swarm_merge.go`，实现 MergeController
    - 实现 MergeAll：按拓扑序逐个合并分支，每合并一个执行编译验证
    - 实现 RevertBranch：回退指定分支的合并
    - 编译失败时回退失败分支，通知 Developer 修复
    - _需求: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [x]* 6.2 编写属性测试：拓扑序合并
    - **Property 13: 拓扑序合并**
    - **验证: 需求 6.1**

- [x] 7. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 8. 实现 FeedbackLoop（反馈循环）
  - [x] 8.1 创建 `swarm_feedback.go`，实现 FeedbackLoop
    - 实现 ClassifyFailures：调用 LLM 对测试失败进行分类（Bug/FeatureGap/RequirementDeviation）
    - 实现 ShouldContinue：检查是否达到最大轮次
    - 实现 NextRound：递增轮次计数器并记录原因
    - _需求: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_

  - [x]* 8.2 编写属性测试：轮次计数器单调递增与终止
    - **Property 15: 轮次计数器单调递增与终止**
    - **验证: 需求 7.5, 7.6**

  - [x]* 8.3 编写属性测试：失败类型路由正确性
    - **Property 14: 失败类型路由正确性**
    - **验证: 需求 7.2, 7.3, 7.4**

- [x] 9. 实现 SwarmReporter（报告生成器）
  - [x] 9.1 创建 `swarm_reporter.go`，实现 SwarmReporter
    - 实现 GenerateReport：生成 report.md、report.json、timeline.md
    - 实现 WriteReportFiles：将报告写入 `.maclaw-swarm/{run_id}/` 路径
    - 实现 MarshalReport / UnmarshalReport：JSON 序列化/反序列化，缺少必填字段时返回错误
    - 为每个 Developer Agent 生成独立的 diff 文件
    - _需求: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 10.1, 10.2, 10.3, 10.4_

  - [x]* 9.2 编写属性测试：报告序列化往返
    - **Property 16: 报告序列化往返**
    - **验证: 需求 10.1, 10.2, 10.3**

  - [x]* 9.3 编写属性测试：报告反序列化错误处理
    - **Property 17: 报告反序列化错误处理**
    - **验证: 需求 10.4**

  - [x]* 9.4 编写属性测试：报告文件完整性
    - **Property 22: 报告文件完整性**
    - **验证: 需求 9.1, 9.6**

  - [x]* 9.5 编写属性测试：时间线事件有序性
    - **Property 23: 时间线事件有序性**
    - **验证: 需求 9.4**

  - [x]* 9.6 编写属性测试：Developer Diff 文件完整性
    - **Property 24: Developer Diff 文件完整性**
    - **验证: 需求 9.5**

- [x] 10. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 11. 实现 SwarmNotifier（通知接口）
  - [x] 11.1 创建 `swarm_notifier.go`，定义 SwarmNotifier 接口并实现默认通知器
    - 定义 SwarmNotifier 接口：NotifyPhaseChange、NotifyAgentComplete、NotifyFailure、NotifyWaitingUser、NotifyRunComplete
    - 实现 DefaultSwarmNotifier：整合 feishu.Notifier 和 ws.Gateway 推送
    - 实现各阶段通知的消息格式化
    - _需求: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

- [x] 12. 实现 Agent 角色 System Prompt 模板
  - [x] 12.1 创建 `swarm_prompts.go`，实现角色 prompt 模板系统
    - 定义各角色的 Go text/template 模板
    - 实现 RenderPrompt 函数：根据 AgentRole 和 PromptContext 渲染 system prompt
    - Architect prompt：包含需求全文、技术栈约束、输出格式要求
    - Developer prompt：包含子任务描述、架构设计、接口定义
    - Compiler prompt：包含编译命令、错误日志、修复文件列表
    - Tester prompt：包含测试命令、需求文档、已实现功能列表
    - Documenter prompt：包含项目结构、API 列表、变更日志
    - _需求: 12.1, 12.2, 12.3, 12.4, 12.5, 12.6_

  - [x]* 12.2 编写属性测试：角色 Prompt 包含必要内容
    - **Property 11: 角色 Prompt 包含必要内容**
    - **验证: 需求 5.2, 12.1, 12.2, 12.3, 12.4, 12.5, 12.6**

- [x] 13. 实现 SwarmOrchestrator 核心调度器
  - [x] 13.1 创建 `swarm_orchestrator.go`，实现 SwarmOrchestrator 核心
    - 实现 NewSwarmOrchestrator：初始化所有子组件依赖
    - 实现 StartSwarmRun：验证前置条件、创建 SwarmRun、启动状态机
    - 实现 PauseSwarmRun / ResumeSwarmRun / CancelSwarmRun：生命周期管理
    - 实现 ListSwarmRuns / GetSwarmRun：查询接口
    - 实现 ProvideUserInput：用户输入通道（需求偏差确认）
    - 实现单 Run 限制：已有 running Run 时拒绝新请求
    - _需求: 1.1, 1.3, 1.4, 1.5, 1.6, 11.1, 11.2, 11.3, 11.4, 11.5, 11.6_

  - [x]* 13.2 编写属性测试：阶段顺序正确性
    - **Property 1: 阶段顺序正确性**
    - **验证: 需求 1.1, 2.4**

  - [x]* 13.3 编写属性测试：单 Run 限制
    - **Property 18: 单 Run 限制**
    - **验证: 需求 11.6**

- [x] 14. 实现 Agent 会话管理与并发控制
  - [x] 14.1 在 SwarmOrchestrator 中实现 Agent 创建和调度逻辑
    - 通过 RemoteSessionManager.Create 为每个 SwarmAgent 创建 RemoteSession
    - LaunchSpec.ProjectPath 指向对应 worktree 路径
    - 注入角色专属 system prompt
    - 实现并发控制：限制同时活跃 Developer Agent 数量（默认 5，范围 1-10）
    - 实现等待队列：超出并发上限的任务排队等待
    - 实现 Agent 超时机制：默认 30 分钟超时，超时后终止并标记失败
    - 实现 Agent 重试机制：error 状态最多重试 2 次
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 13.1, 13.2, 13.3, 13.5_

  - [x]* 14.2 编写属性测试：Agent Session 指向 Worktree
    - **Property 10: Agent Session 指向 Worktree**
    - **验证: 需求 5.1, 1.4**

  - [x]* 14.3 编写属性测试：Agent 重试上限
    - **Property 12: Agent 重试上限**
    - **验证: 需求 5.6**

  - [x]* 14.4 编写属性测试：Agent 并发上限
    - **Property 19: Agent 并发上限**
    - **验证: 需求 13.1, 13.2**

  - [x]* 14.5 编写属性测试：并发配置范围验证
    - **Property 20: 并发配置范围验证**
    - **验证: 需求 13.3**

  - [x]* 14.6 编写属性测试：Agent 超时终止
    - **Property 21: Agent 超时终止**
    - **验证: 需求 13.5**

- [x] 15. 检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

- [x] 16. 实现 Greenfield 模式 Pipeline
  - [x] 16.1 在 SwarmOrchestrator 中实现 Greenfield 模式的完整流程
    - 实现 runGreenfield 方法：按阶段顺序驱动 task_split → architecture → development → merge → compile → test → document → report
    - 实现 Architect Agent 创建和输出解析
    - 实现并行 Developer Agent 创建和监控
    - 实现编译合并流程：调用 MergeController.MergeAll
    - 实现测试阶段：创建 Tester Agent 运行测试
    - 实现文档阶段：创建 Documenter Agent 生成文档
    - 集成 FeedbackLoop：测试失败时分类并触发修复策略
    - 集成 SwarmNotifier：各阶段推送通知
    - 集成 SwarmReporter：生成最终报告
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.5, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 7.1, 7.2, 7.3, 7.4, 8.1, 8.6_

- [x] 17. 实现 Maintenance 模式 Pipeline
  - [x] 17.1 在 SwarmOrchestrator 中实现 Maintenance 模式的完整流程
    - 实现 runMaintenance 方法：按阶段顺序驱动 task_split → conflict_detect → development → merge → compile → test → document → report
    - 实现冲突检测阶段：调用 ConflictDetector.DetectConflicts
    - 按 TaskGroup 分组调度 Developer Agent（组内串行、组间并行）
    - 复用编译合并、测试、文档、反馈循环逻辑
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 4.1, 4.2, 4.3, 4.4_

- [x] 18. 集成到 App 主结构
  - [x] 18.1 在 `app.go` 中注册 SwarmOrchestrator
    - 在 App 结构体中添加 swarmOrchestrator 字段
    - 在 ensureRemoteInfra 中初始化 SwarmOrchestrator 及其依赖
    - _需求: 1.1, 2.1_

  - [x] 18.2 创建 `app_swarm_bindings.go`，暴露 Wails 前端绑定方法
    - 实现 StartSwarmRun / PauseSwarmRun / ResumeSwarmRun / CancelSwarmRun 绑定
    - 实现 ListSwarmRuns / GetSwarmRun / ProvideUserInput 绑定
    - _需求: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6_

- [x] 19. 最终检查点 - 确保所有测试通过
  - 确保所有测试通过，如有问题请询问用户。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 开发
- 每个任务引用了具体的需求编号以确保可追溯性
- 检查点确保增量验证
- 属性测试使用 [rapid](https://github.com/flyingmutant/rapid) 库验证通用正确性属性
- 单元测试验证具体示例和边界情况
- 所有 25 个正确性属性均已分配到对应的实现任务中
