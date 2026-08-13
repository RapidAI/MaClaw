# 工作流引擎 V2 架构设计

## 设计原则

1. **一条路径**：用户消息进来 → 一个决策点 → 工作流或 agent loop。不要六七层拦截器。
2. **状态机驱动**：工作流是一个有限状态机，状态转换有明确的触发条件，不靠 flag 和 sync.Map 传递。
3. **路径即数据**：projectPath 在工作流创建时确定，写入状态机，不再从 config/全局变量/ownerID 多路推断。
4. **测试隔离**：状态机不写生产 DB。测试用内存 store，生产用 SQLite，两者互不干扰。

---

## 核心架构

```
用户消息
    │
    ▼
┌─────────────────────┐
│  WorkflowRouter     │  ← 唯一决策点
│  (取代 6 层拦截器)   │
└─────────────────────┘
    │                 │
    ▼                 ▼
┌──────────┐    ┌──────────────┐
│ 普通      │    │ 工作流        │
│ AgentLoop │    │ StateMachine  │
└──────────┘    └──────────────┘
                      │
            ┌─────────┼─────────┐
            ▼         ▼         ▼
        Phase 1   Phase 2   Phase N
        (需求)    (设计)    (执行)
                              │
                              ▼
                    ┌──────────────┐
                    │ TaskRunner    │
                    │ (SubAgent)   │
                    └──────────────┘
```

---

## 模块职责（5 个模块，替代现有 30+ 文件）

### 1. WorkflowRouter（替代 QuickFilter + UIC + IUM + GateIntentClassifier + SteeringDetector）

**职责**：判断一条消息是否应该进入/继续工作流。

**决策逻辑**（优先级从高到低）：
1. 已有活跃工作流 → 路由到 StateMachine.HandleInput
2. 用户明确跳过（"直接做"/"不用文档"）→ 路由到 AgentLoop
3. 消息匹配编码意图（关键词 + LLM 确认）→ 创建工作流
4. 其他 → 路由到 AgentLoop

**实现**：
- 一个函数 `Route(msg) → (workflow | agentloop)`
- 关键词匹配做快速预筛（<1ms）
- 需要 LLM 确认时调用一次轻量 LLM（不是三次）
- 不维护任何跨请求状态（状态全在 StateMachine 里）

### 2. StateMachine（替代 WorkflowEngine + NeedsConfirm gate + WorkflowAdapter）

**职责**：管理工作流的生命周期和阶段流转。

**状态**：
```go
type WorkflowState struct {
    ID           string
    UserID       string
    Type         WorkflowType        // coding, ppt, product_design...
    ProjectPath  string              // 创建时确定，不可变
    Phases       []Phase             // 从模板复制
    CurrentPhase int                 // 当前阶段索引
    PhaseOutputs map[string]string   // 阶段产出物
    Status       WorkflowStatus      // active, paused, completed, cancelled
    CreatedAt    time.Time
}

type Phase struct {
    ID            string
    Name          string
    NeedsConfirm  bool
    ToolPolicy    ToolPolicy  // doc_only | full
    Output        string      // 当前阶段的产出物
    Status        PhaseStatus // pending, running, waiting_confirm, completed
}
```

**状态转换**：
```
pending → running       : StateMachine.Start() 或 用户确认上一阶段
running → waiting_confirm : LLM 产出实质性文档（>200 rune）
waiting_confirm → completed : 用户确认
completed → (next phase pending → running)
```

**关键约束**：
- `ProjectPath` 在 `Create()` 时设置，之后只读
- `PhaseOutputs` 只在 `waiting_confirm → completed` 转换时写入
- 没有 `PendingReviewPhaseID`、`PendingReviewRevisionRequested`、`PhaseFormSubmitted` 等复杂子状态

### 3. PhaseExecutor（替代 agent loop 中的 coding-gate + NeedsConfirm 逻辑）

**职责**：执行单个阶段的 agent loop。

**行为**：
- 注入当前阶段的 system prompt（阶段指令 + 前序阶段摘要）
- 按 `ToolPolicy` 过滤工具列表（doc_only 阶段只保留读取/搜索工具）
- LLM 产出实质性文本 → 设置阶段状态为 `waiting_confirm` → force-return
- 不需要 `gateConfig.active`、`needsConfirmFromSteering`、`needsConfirmFromEngine` 三个独立判断

**与主 AgentLoop 的关系**：
- PhaseExecutor 就是一个配置了特定 system prompt + 工具过滤的 AgentLoop
- 不是独立的执行器，复用现有 `runAgentLoop` 基础设施
- 通过 `LoopContext.WorkflowPhase *Phase` 传递阶段信息（1 个字段，不是 6 个 sync.Map）

### 4. TaskRunner（替代 TaskExecutionOrchestrator + SubAgentTaskRunner + CodingSubAgent）

**职责**：在执行阶段逐任务运行代码。

**简化设计**：
- 从任务分解文档解析任务列表
- 逐任务调用 SubAgent（纯净 context，5 工具）
- **projectPath 直接用 WorkflowState.ProjectPath**（不再推断）
- **沙箱改为宽松模式**：只禁止明确危险的操作（rm -rf /），不限制路径必须在 projectPath 内
  - 原因：用户可能指定 `d:\game2` 但代码需要访问 `d:\game2\build\` 等子目录，或需要读取系统头文件
  - 保留 write_file 的路径限制（只能写到 projectPath 内），bash 不限制

### 5. WorkflowStore（替代 SQLiteStore + NullStore 混用）

**职责**：持久化工作流状态。

**接口**：
```go
type WorkflowStore interface {
    Save(state *WorkflowState) error
    Load(userID string) (*WorkflowState, error)
    Delete(userID string) error
}
```

**实现**：
- 生产：SQLite（`~/.maclaw/workflow_v2.db`）——新文件，不复用旧 DB
- 测试：`MemoryStore`——永远不碰文件系统

---

## 砍掉的复杂度

| 现有模块 | 处置 | 原因 |
|---------|------|------|
| QuickFilter | 删除 | 合并到 WorkflowRouter 的关键词预筛 |
| UnifiedIntentClassifier（工作流路由部分）| 删除 | Router 只需一次 LLM 调用 |
| IntentUnderstandingManager | 删除 | 不再多轮追问，不确定就不进工作流 |
| GateIntentClassifier | 删除 | Router 一个决策点替代 |
| SteeringWorkflowDetector | 删除 | PhaseExecutor 统一处理 |
| CodingToolGate | 删除 | PhaseExecutor 的 ToolPolicy 替代 |
| needsConfirmFromSteering | 删除 | StateMachine 的 Phase.NeedsConfirm 统一 |
| needsConfirmFromEngine | 删除 | 同上 |
| workflowAgentLoopMarker (sync.Map) | 删除 | LoopContext.WorkflowPhase 替代 |
| stashedPhasePrompt (sync.Map) | 删除 | PhaseExecutor 直接读 StateMachine |
| workflowPendingConfirmOther (sync.Map) | 删除 | Router 直接判断 |
| SkipNeedsConfirmGate | 删除 | Router 判断"与工作流无关"后直接走 AgentLoop |
| HasPhaseOutput 检查 | 删除 | 阶段状态机自带 running/waiting_confirm 区分 |
| backfillExecutionOrchestratorActivation | 删除 | TaskRunner 在执行阶段自动激活 |
| repairInvalidCodingTaskBreakdown | 删除 | 格式不对就用 LLM 重试一次，不搞复杂修复循环 |
| workflowStartProjectPathForOwner/ForIntent | 删除 | Create 时确定，之后不推断 |
| SubAgent 路径沙箱 | 大幅简化 | 只限制 write_file 在项目内，不限制 bash |

---

## 数据流（一条消息的完整生命周期）

### Case 1: 新编码任务

```
用户: "在 d:\game2 下开发贪吃蛇 C++"
    │
    ▼
WorkflowRouter.Route(msg)
    ├─ 关键词匹配: "开发" → 候选编码任务
    ├─ 提取 projectPath: "d:\game2"（从消息文本中提取）
    ├─ LLM 确认: "是编码任务吗？" → yes
    │
    ▼
StateMachine.Create(type=coding, projectPath="d:\game2")
    → Phase 0: requirements (running)
    │
    ▼
PhaseExecutor.Run(phase=requirements)
    ├─ system prompt: "生成需求文档..."
    ├─ tools: doc_only (read_file, web_search, memory)
    ├─ LLM 输出需求文档
    ├─ len > 200 rune → phase.Status = waiting_confirm
    │
    ▼
返回给用户: "需求文档已生成，请确认或修改"
```

### Case 2: 用户确认

```
用户: "确认"
    │
    ▼
WorkflowRouter.Route(msg)
    ├─ 有活跃工作流 → 路由到 StateMachine
    │
    ▼
StateMachine.HandleInput("确认")
    ├─ 当前阶段 waiting_confirm → completed
    ├─ Phase 1: tech_design → running
    │
    ▼
PhaseExecutor.Run(phase=tech_design)
    ... (同上)
```

### Case 3: 执行阶段

```
用户确认任务列表后:
    │
    ▼
StateMachine: Phase 3 (implementation) → running
    │
    ▼
TaskRunner.Start(tasks, projectPath="d:\game2")
    ├─ Task 1: SubAgent(system="实现蛇移动...", tools=[bash,write_file,...], projectPath="d:\game2")
    ├─ Task 2: SubAgent(system="实现食物生成...", ...)
    ├─ ...
    │
    ▼
TaskRunner 完成 → Phase 3 → completed → 工作流结束
```

### Case 4: 工作流活跃期间的无关消息

```
用户: "查一下杭州天气"
    │
    ▼
WorkflowRouter.Route(msg)
    ├─ 有活跃工作流
    ├─ 但消息明显与工作流无关（无编码关键词，有天气关键词）
    ├─ → 路由到普通 AgentLoop（完整工具集）
    │
    ▼
普通 AgentLoop 执行天气查询
```

---

## 实施计划

### Phase 1: 新建 V2 模块（不删旧代码）
- `corelib/workflow/v2/` 目录
- 实现 StateMachine + WorkflowStore + PhaseExecutor
- 单元测试覆盖状态转换

### Phase 2: WorkflowRouter + 接入 agent loop
- 替换 `handleWorkflowInterception` 为 `WorkflowRouter.Route`
- 替换 agent loop 中的 6 个门控为 `LoopContext.WorkflowPhase`
- 灰度切换：config 中 `workflow_engine_version: "v2"` 启用

### Phase 3: TaskRunner
- 简化 SubAgent 沙箱
- 接入 StateMachine 的执行阶段

### Phase 4: 删除旧代码
- 确认 V2 稳定后，删除 V1 的 30+ 文件

---

## 与现有代码的兼容性

- `workflow.db`（V1）保留不动，V2 使用 `workflow_v2.db`
- V1 的工作流模板定义（`templates.go`）可以复用，只需要简化 Phase 结构
- 前端面板（WorkflowDocPreview、phaseLabels）接口不变，只是后端数据源切换
- 工作流仅由用户显式从工作流面板或命令启动；普通消息不会自动触发。
