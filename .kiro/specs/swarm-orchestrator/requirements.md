# 需求文档：Swarm Orchestrator（蜂群编排器）

## 简介

Swarm Orchestrator 是 Maclaw 的 AI 军团调度系统，通过 git worktree 实现多个编程工具实例并行开发同一项目。系统支持两种工作模式：Greenfield Swarm（全新项目从零开始）和 Maintenance Swarm（Bug 修复/新功能迭代）。每个 Agent 本质上是一个带角色专属 system prompt 的 RemoteSession，在独立的 git worktree 中工作，通过冲突检测、依赖分析和反馈循环机制保证并行开发的正确性。系统复用现有的 RemoteSessionManager、SharedContextStore、ProjectScanner、飞书通知和 WebSocket 推送等基础设施。

## 术语表

- **Swarm_Orchestrator**: 蜂群编排器，管理整个并行开发流程的核心调度组件，负责模式选择、角色分配、阶段推进和反馈循环
- **Swarm_Run**: 一次蜂群执行实例，包含完整的从任务分解到报告生成的生命周期
- **Swarm_Agent**: 蜂群中的单个 Agent 实例，本质上是一个带角色专属 system prompt 的 RemoteSession
- **Agent_Role**: Agent 的角色类型，包括 Architect（架构师）、Designer（产品设计师）、Developer（开发者）、Compiler（编译官）、Tester（测试者）、Documenter（文档员）
- **Worktree_Manager**: Git Worktree 管理器，负责创建、管理和清理 git worktree 工作目录
- **Worktree**: Git worktree 工作副本，每个 Developer Agent 在独立的 worktree 中工作，位于项目同级目录 `../.maclaw-workers/`
- **Conflict_Detector**: 冲突检测器，分析文件依赖图，确保并行任务不操作相同文件
- **Task_Splitter**: 任务分解器，将产品需求或任务列表分解为可并行执行的子任务
- **Merge_Controller**: 合并控制器，按依赖拓扑序将各 worktree 的改动合并回主分支
- **Feedback_Loop**: 反馈循环机制，处理编译失败、测试失败等情况的自动修复流程
- **Swarm_Reporter**: 报告生成器，生成 report.md、report.json、timeline.md 等完整报告
- **Session_Manager**: 远程会话管理器（对应现有 `RemoteSessionManager`），管理编程工具会话的生命周期
- **Shared_Context**: 共享上下文存储（对应现有 `SharedContextStore`），用于 Agent 间上下文共享
- **Project_Scanner**: 项目扫描器（对应现有 `ProjectScanner`），分析代码库结构
- **Feishu_Notifier**: 飞书通知器（对应现有 `feishu.Notifier`），推送实时进度通知
- **WS_Gateway**: WebSocket 网关（对应现有 `ws.Gateway`），推送实时事件到前端
- **Greenfield_Mode**: 全新项目模式，从产品需求出发，经过架构规划、并行开发、编译、测试、文档的完整流程
- **Maintenance_Mode**: 维护模式，从 Bug/Feature 列表出发，经过冲突检测、并行修复、编译、测试、文档更新的流程
- **Task_Group**: 任务分组，冲突检测后将有文件依赖的任务归入同一组，组内串行执行，组间并行执行
- **Swarm_Round**: 蜂群轮次，一次完整的开发-编译-测试循环，反馈循环可能触发多个轮次

## 需求

### 需求 1：Greenfield Swarm 模式启动

**用户故事：** 作为开发者，我希望通过提供产品需求和技术栈描述来启动一个全新项目的并行开发，以便多个 AI Agent 能同时从零构建项目的不同模块。

#### 验收标准

1. WHEN 用户提供产品需求文本和技术栈描述时，THE Swarm_Orchestrator SHALL 创建一个 Greenfield 模式的 Swarm_Run，并按以下阶段顺序推进：需求分解 → 架构规划 → 并行开发 → 编译合并 → 测试 → 文档生成
2. WHEN Swarm_Run 进入需求分解阶段时，THE Task_Splitter SHALL 调用配置的 LLM 将产品需求分解为独立的开发子任务，每个子任务包含任务描述、预期输出文件列表和依赖关系
3. WHEN Swarm_Run 进入架构规划阶段时，THE Swarm_Orchestrator SHALL 创建一个 Architect 角色的 Swarm_Agent，由该 Agent 生成项目目录结构、模块划分和接口定义
4. WHEN Swarm_Run 进入并行开发阶段时，THE Swarm_Orchestrator SHALL 为每个独立子任务创建一个 Developer 角色的 Swarm_Agent，每个 Agent 在独立的 Worktree 中工作
5. THE Swarm_Orchestrator SHALL 在 Greenfield 模式中支持以下角色：Architect、Designer、Developer（多个）、Compiler、Tester、Documenter
6. WHEN 用户未指定技术栈时，THE Swarm_Orchestrator SHALL 由 Architect Agent 根据需求内容推荐技术栈，并等待用户确认后继续

### 需求 2：Maintenance Swarm 模式启动

**用户故事：** 作为开发者，我希望通过提供 Bug/Feature 列表来启动并行修复流程，以便多个 AI Agent 能同时处理不同的 Bug 或功能需求。

#### 验收标准

1. WHEN 用户提供任务列表（手动输入、GitHub Issues URL、飞书多维表格 URL 或 Jira URL）时，THE Swarm_Orchestrator SHALL 创建一个 Maintenance 模式的 Swarm_Run
2. WHEN 任务列表来源为 GitHub Issues URL 时，THE Task_Splitter SHALL 通过 GitHub API 拉取 Issue 列表，解析标题、描述和标签作为任务输入
3. WHEN 任务列表来源为手动输入时，THE Task_Splitter SHALL 解析用户提供的文本，将每一条独立描述识别为一个任务
4. THE Swarm_Orchestrator SHALL 在 Maintenance 模式中按以下阶段顺序推进：任务解析 → 冲突检测 → 并行开发 → 编译合并 → 测试 → 文档更新
5. THE Swarm_Orchestrator SHALL 在 Maintenance 模式中支持以下角色：Developer（多个）、Compiler、Tester、Documenter

### 需求 3：Git Worktree 生命周期管理

**用户故事：** 作为开发者，我希望蜂群编排器能自动管理 git worktree 的创建和清理，以便并行开发不影响我的原始工作目录。

#### 验收标准

1. WHEN Swarm_Run 启动且项目目录已有 git 仓库时，THE Worktree_Manager SHALL 先执行 `git stash` 保存未提交的改动，然后基于 HEAD 为每个 Developer Agent 创建独立的 git worktree
2. WHEN Swarm_Run 启动且项目目录没有 git 仓库（无 `.git` 目录）时，THE Worktree_Manager SHALL 自动执行 `git init` 和初始 commit，然后创建 worktree
3. WHEN Swarm_Run 启动且项目目录没有任何 commit 历史时，THE Worktree_Manager SHALL 自动创建一个包含当前所有文件的初始 commit，然后创建 worktree
4. THE Worktree_Manager SHALL 将所有 worktree 创建在项目同级目录的 `.maclaw-workers/{run_id}/` 路径下
5. THE Worktree_Manager SHALL 为每个 worktree 创建独立的 git 分支，分支命名格式为 `swarm/{run_id}/{agent_role}-{task_index}`
6. WHEN Swarm_Run 的所有任务完成并合并后，THE Worktree_Manager SHALL 清理所有 worktree 目录和对应的 git 分支
7. WHEN Swarm_Run 完成后且原始工作目录有 stash 记录时，THE Worktree_Manager SHALL 执行 `git stash pop` 恢复用户之前未提交的改动
8. THE Worktree_Manager SHALL 仅执行本地 git 操作，不依赖远程仓库

### 需求 4：文件依赖分析与冲突检测

**用户故事：** 作为开发者，我希望蜂群编排器能自动检测任务间的文件冲突，以便避免多个 Agent 同时修改同一文件导致合并失败。

#### 验收标准

1. WHEN Swarm_Run 进入冲突检测阶段时，THE Conflict_Detector SHALL 分析每个任务预期修改的文件列表，构建文件依赖图
2. WHEN 两个或多个任务的预期修改文件列表存在交集时，THE Conflict_Detector SHALL 将这些任务归入同一个 Task_Group
3. THE Conflict_Detector SHALL 确保同一 Task_Group 内的任务串行执行，不同 Task_Group 的任务可以并行执行
4. WHEN Conflict_Detector 完成分组后，THE Swarm_Orchestrator SHALL 向用户展示分组结果，包括每组的任务列表和冲突文件
5. THE Conflict_Detector SHALL 复用现有 Project_Scanner 分析代码库的导入关系和模块依赖，将间接依赖也纳入冲突检测范围

### 需求 5：Agent 会话管理

**用户故事：** 作为开发者，我希望每个蜂群 Agent 都是一个完整的编程工具会话，以便复用现有的会话管理基础设施。

#### 验收标准

1. THE Swarm_Orchestrator SHALL 通过现有 Session_Manager 的 Create 方法为每个 Swarm_Agent 创建 RemoteSession，LaunchSpec 的 ProjectPath 指向对应的 worktree 路径
2. WHEN 创建 Swarm_Agent 的 RemoteSession 时，THE Swarm_Orchestrator SHALL 在 LaunchSpec 中注入角色专属的 system prompt，包含角色职责描述、任务上下文和输出格式要求
3. THE Swarm_Orchestrator SHALL 通过 Session_Manager 的 WriteInput 方法向 Swarm_Agent 发送任务指令
4. THE Swarm_Orchestrator SHALL 通过监听 Swarm_Agent 的 RemoteSession 状态变化（Summary、Events、Preview）来跟踪任务执行进度
5. WHEN Swarm_Agent 的 RemoteSession 进入 "waiting_input" 状态时，THE Swarm_Orchestrator SHALL 根据角色类型和当前阶段自动生成下一步指令
6. IF Swarm_Agent 的 RemoteSession 进入 "error" 状态，THEN THE Swarm_Orchestrator SHALL 记录错误信息并尝试重新创建该 Agent（最多重试 2 次）

### 需求 6：编译合并流程

**用户故事：** 作为开发者，我希望蜂群编排器能自动将各 Agent 的开发成果合并并编译验证，以便及时发现集成问题。

#### 验收标准

1. WHEN 所有 Developer Agent 完成各自任务后，THE Merge_Controller SHALL 按依赖拓扑序将各 worktree 分支合并到主分支
2. WHEN 合并过程中出现 git 冲突时，THE Merge_Controller SHALL 创建一个 Compiler 角色的 Swarm_Agent 来解决冲突
3. WHEN 所有分支合并完成后，THE Swarm_Orchestrator SHALL 创建一个 Compiler 角色的 Swarm_Agent 在合并后的代码上执行编译命令
4. WHEN 编译成功时，THE Swarm_Orchestrator SHALL 推进到测试阶段
5. WHEN 编译失败时，THE Merge_Controller SHALL 分析编译错误日志，识别导致错误的分支，回退该分支的合并，并通知对应的 Developer Agent 修复
6. THE Merge_Controller SHALL 在编译失败后触发一个轮次内小循环：Developer 修复 → 重新合并 → 重新编译，直到编译成功或达到最大重试次数

### 需求 7：反馈循环机制

**用户故事：** 作为开发者，我希望蜂群编排器能自动处理测试失败的情况，以便根据失败类型采取不同的修复策略。

#### 验收标准

1. WHEN 测试阶段发现失败的测试用例时，THE Feedback_Loop SHALL 调用配置的 LLM 对每个失败用例进行分类，分为三种类型：Bug（代码缺陷）、Feature_Gap（功能缺失）、Requirement_Deviation（需求偏差）
2. WHEN 失败类型为 Bug 时，THE Feedback_Loop SHALL 自动启动一个 Maintenance 模式的 Swarm_Round，将 Bug 描述作为任务输入进行修复
3. WHEN 失败类型为 Feature_Gap 时，THE Feedback_Loop SHALL 启动一个 Mini-Greenfield 流程：由 Architect Agent 评估缺失功能 → Developer Agent 开发 → Compiler Agent 编译 → Tester Agent 测试
4. WHEN 失败类型为 Requirement_Deviation 时，THE Feedback_Loop SHALL 暂停 Swarm_Run 并通知用户确认需求，等待用户输入后继续
5. THE Feedback_Loop SHALL 维护一个最大轮次计数器，WHEN 累计轮次达到配置的上限（默认 5 轮）时，THE Feedback_Loop SHALL 终止 Swarm_Run 并生成当前状态的报告
6. WHEN 每个 Swarm_Round 开始时，THE Feedback_Loop SHALL 递增轮次计数器并在报告中记录该轮次的触发原因

### 需求 8：实时进度通知

**用户故事：** 作为开发者，我希望在蜂群执行过程中收到实时进度通知，以便随时了解各 Agent 的工作状态。

#### 验收标准

1. WHEN Swarm_Run 的阶段发生变化时，THE Swarm_Orchestrator SHALL 通过 Feishu_Notifier 和 WS_Gateway 推送阶段变更通知，包含当前阶段名称、已完成任务数和总任务数
2. WHEN 任一 Swarm_Agent 完成其任务时，THE Swarm_Orchestrator SHALL 推送该 Agent 的完成通知，包含 Agent 角色、任务摘要和耗时
3. WHEN 编译失败或测试失败时，THE Swarm_Orchestrator SHALL 推送失败通知，包含错误摘要和即将采取的修复策略
4. WHEN Feedback_Loop 暂停等待用户确认时，THE Swarm_Orchestrator SHALL 推送等待确认通知，包含需要确认的内容和操作指引
5. THE Swarm_Orchestrator SHALL 通过 WS_Gateway 推送每个 Swarm_Agent 的实时输出预览（复用现有 preview_delta 机制）
6. THE Swarm_Orchestrator SHALL 在每个关键节点推送通知：Swarm_Run 启动、各阶段完成、编译结果、测试结果、Swarm_Run 结束

### 需求 9：执行报告生成

**用户故事：** 作为开发者，我希望蜂群执行完成后能获得完整的执行报告，以便回顾整个开发过程和评估结果。

#### 验收标准

1. WHEN Swarm_Run 结束时（正常完成或达到最大轮次），THE Swarm_Reporter SHALL 生成以下报告文件：`report.md`（人类可读摘要）、`report.json`（机器可读数据）、`timeline.md`（完整时间线）
2. THE Swarm_Reporter SHALL 在 report.md 中包含：需求完成度统计、各轮次详情、代码变更统计（新增/修改/删除行数）、各 Agent 工作量统计、遗留问题列表
3. THE Swarm_Reporter SHALL 在 report.json 中包含：Swarm_Run 的完整元数据、每个 Agent 的执行记录、每个轮次的触发原因和结果、测试报告数据
4. THE Swarm_Reporter SHALL 在 timeline.md 中按时间顺序记录所有关键事件：Agent 创建/完成、编译结果、测试结果、反馈循环触发、用户交互
5. THE Swarm_Reporter SHALL 为每个 Developer Agent 生成独立的 diff 文件，记录该 Agent 在其 worktree 中的所有代码变更
6. THE Swarm_Reporter SHALL 将所有报告文件存储在项目目录的 `.maclaw-swarm/{run_id}/` 路径下

### 需求 10：报告序列化与反序列化

**用户故事：** 作为开发者，我希望蜂群报告的 JSON 格式是稳定且可验证的，以便其他工具能可靠地解析和使用报告数据。

#### 验收标准

1. THE Swarm_Reporter SHALL 将 Swarm_Run 的报告数据序列化为 JSON 格式，包含所有字段（run_id、mode、status、rounds、agents、timeline、statistics）
2. THE Swarm_Reporter SHALL 将 JSON 格式的报告数据反序列化为 SwarmReport 结构体
3. FOR ALL 有效的 SwarmReport 对象，序列化后再反序列化 SHALL 产生与原始对象等价的结果（round-trip 属性）
4. IF 反序列化的 JSON 缺少必填字段（run_id 或 mode），THEN THE Swarm_Reporter SHALL 返回描述性错误信息

### 需求 11：Swarm_Run 生命周期管理

**用户故事：** 作为开发者，我希望能查看、暂停和取消正在执行的蜂群任务，以便在需要时控制执行流程。

#### 验收标准

1. THE Swarm_Orchestrator SHALL 为每个 Swarm_Run 分配唯一 ID，并维护其状态（pending、running、paused、completed、failed、cancelled）
2. WHEN 用户请求暂停一个 running 状态的 Swarm_Run 时，THE Swarm_Orchestrator SHALL 暂停创建新的 Swarm_Agent，等待当前活跃 Agent 完成后进入 paused 状态
3. WHEN 用户请求恢复一个 paused 状态的 Swarm_Run 时，THE Swarm_Orchestrator SHALL 从暂停点继续执行后续阶段
4. WHEN 用户请求取消一个 Swarm_Run 时，THE Swarm_Orchestrator SHALL 终止所有活跃的 Swarm_Agent 会话，清理 worktree，并生成截止当前状态的报告
5. THE Swarm_Orchestrator SHALL 提供列出所有 Swarm_Run（含历史记录）的接口，返回每个 Run 的 ID、模式、状态、创建时间和任务摘要
6. THE Swarm_Orchestrator SHALL 限制同时运行的 Swarm_Run 数量为 1 个，WHEN 已有 running 状态的 Run 时，新的启动请求 SHALL 返回错误

### 需求 12：Agent 角色 System Prompt 管理

**用户故事：** 作为开发者，我希望每个 Agent 角色都有精心设计的 system prompt，以便不同角色能准确执行各自的职责。

#### 验收标准

1. THE Swarm_Orchestrator SHALL 为每种 Agent_Role 维护一个 system prompt 模板，模板支持变量替换（项目名称、技术栈、任务描述、上下文信息）
2. THE Swarm_Orchestrator SHALL 为 Architect 角色的 prompt 包含：项目需求全文、技术栈约束、输出格式要求（目录结构、模块划分、接口定义）
3. THE Swarm_Orchestrator SHALL 为 Developer 角色的 prompt 包含：分配的子任务描述、架构师的设计文档、相关模块的接口定义、代码规范要求
4. THE Swarm_Orchestrator SHALL 为 Compiler 角色的 prompt 包含：编译命令、错误日志、需要修复的文件列表
5. THE Swarm_Orchestrator SHALL 为 Tester 角色的 prompt 包含：测试命令、需求文档、已实现功能列表
6. THE Swarm_Orchestrator SHALL 为 Documenter 角色的 prompt 包含：项目结构、API 列表、变更日志、文档模板

### 需求 13：并发控制与资源限制

**用户故事：** 作为开发者，我希望蜂群编排器能合理控制并发数量，以便不会因为同时启动过多 Agent 而耗尽系统资源。

#### 验收标准

1. THE Swarm_Orchestrator SHALL 限制单个 Swarm_Run 中同时活跃的 Developer Agent 数量，默认上限为 5 个
2. WHEN 待执行任务数量超过并发上限时，THE Swarm_Orchestrator SHALL 将多余的任务放入等待队列，当有 Agent 完成任务后自动调度下一个任务
3. THE Swarm_Orchestrator SHALL 提供配置接口允许用户调整最大并发数（范围 1-10）
4. WHEN 系统内存使用率超过 85% 时，THE Swarm_Orchestrator SHALL 暂停创建新的 Swarm_Agent，直到内存使用率降至 75% 以下
5. THE Swarm_Orchestrator SHALL 为每个 Swarm_Agent 设置单任务超时时间（默认 30 分钟），WHEN Agent 超时时，THE Swarm_Orchestrator SHALL 终止该 Agent 并将任务标记为失败

