# MaClaw 编程能力架构改进——SubAgent 纯净上下文方案

## 1. 问题分析：当前架构为什么处理不了大项目

### 1.1 Context 构成实测（来自 morio 超级玛利游戏任务日志）

| 组成部分 | 估算 Token | 占比 |
|---------|-----------|------|
| System Prompt（角色/规则/工作流/工具说明） | ~8,000-12,000 | 8-10% |
| 工具定义（40+ 工具的 JSON Schema） | ~8,000-15,000 | 8-12% |
| Steering 规则注入 | ~2,000-3,000 | 2% |
| 记忆召回（proactive recall + entity recall） | ~2,000-4,000 | 2-3% |
| Skill 列表 + MCP Server 列表 | ~1,000-3,000 | 1-2% |
| 对话历史（初始） | ~5,000-10,000 | 5-8% |
| **初始总计** | **~40,000** | **~30%** |
| 对话历史（129 轮迭代后） | **~95,000** | **~70%** |
| **最终总计** | **~135,000** | **100%** |

**关键发现**：
- 初始 context 就占了 40K token（30%），留给实际编码工作的空间只有 ~62K（128K × 80% - 40K）
- 每轮迭代追加 assistant 消息 + tool results，平均 ~700 token/轮
- 129 轮 × 700 = ~90K token 的对话历史
- `trimConversation` 因 token 估算偏低 30% 未触发裁剪（已在 #74 修复）

### 1.2 根本矛盾

MaClaw 的 agent loop 是一个**全能型单体 agent**——同一个 context 里塞了：

1. **角色人设**（"尽心尽责的软件开发管家"）
2. **IM 通道管理**（飞书/微信/QQ 文件发送规则）
3. **工作流引擎**（三阶段确认、NeedsConfirm gate）
4. **40+ 工具定义**（SSH、browser、screenshot、memory、skill、office...）
5. **编码规范**（UTF-8 编码、大文件分块写入）
6. **安全防火墙规则**
7. **Skill 市场搜索策略**
8. **记忆系统**（proactive recall、entity recall）
9. **实际的编码对话历史**

对于编码任务，其中 1-4、6-8 都是**噪音**——它们消耗 context 但不贡献编码质量。更糟的是，这些噪音会**干扰**模型的编码行为（如 Browser: 前缀幻觉就是工具定义污染 context 的典型案例）。

### 1.3 与 Claude Code / Kiro 的对比

| 维度 | Claude Code / Kiro | MaClaw 当前 |
|------|-------------------|------------|
| 编码 context | 纯净——只有编码相关的 system prompt + 文件内容 + 编码对话 | 污染——混入 IM 规则、40+ 工具、记忆、Skill 等 |
| 工具集 | 精简——read_file、write_file、bash、search、list 约 5-8 个 | 臃肿——40+ 工具定义占 8-15K token |
| 对话历史 | 独立——每个编码会话有自己的 context | 共享——编码对话和非编码对话混在一起 |
| Context 利用率 | ~90% 用于编码 | ~30% 初始开销，~70% 用于编码（但含大量噪音） |

## 2. SubAgent 方案设计

### 2.1 核心思路

**不是"启动一个子进程"，而是"启动一个纯净的 LLM 对话"**。

SubAgent 不需要是独立进程。它是同一个 Go 进程内的一个**独立的 conversation + tool set**，与主 agent 共享 HTTP client 和 LLM config，但有自己的：
- 精简的 system prompt（只有编码相关规则）
- 精简的工具集（只有 read_file、write_file、edit_file、bash、list_directory、search_files）
- 独立的 conversation history（不被 IM 规则、记忆召回等污染）
- 独立的 token budget 管理

### 2.2 架构对比

```
当前架构（单体 Agent）：
┌─────────────────────────────────────────────┐
│ Agent Loop (128K context)                    │
│ ┌─────────────────────────────────────────┐ │
│ │ System Prompt (12K)                     │ │
│ │ - 角色人设、IM 规则、工作流、安全...     │ │
│ ├─────────────────────────────────────────┤ │
│ │ Tool Definitions (15K)                  │ │
│ │ - 40+ 工具的 JSON Schema               │ │
│ ├─────────────────────────────────────────┤ │
│ │ Memory + Steering (5K)                  │ │
│ ├─────────────────────────────────────────┤ │
│ │ Conversation History (95K)              │ │
│ │ - 编码 + 非编码混合                     │ │
│ └─────────────────────────────────────────┘ │
│ 可用编码空间: ~0K (已爆)                     │
└─────────────────────────────────────────────┘

SubAgent 架构（主从分离）：
┌──────────────────────┐    ┌──────────────────────┐
│ 主 Agent (128K)       │    │ 编码 SubAgent (128K)  │
│ ┌──────────────────┐ │    │ ┌──────────────────┐ │
│ │ System Prompt 12K│ │    │ │ Coding Prompt 2K │ │
│ │ Tools 15K        │ │    │ │ Task Context 3K  │ │
│ │ Memory 5K        │ │    │ │ Tools 2K (6个)   │ │
│ │ History 20K      │ │    │ │ History 80K      │ │
│ │ (工作流管理)      │ │    │ │ (纯编码对话)      │ │
│ └──────────────────┘ │    │ └──────────────────┘ │
│ 职责: 需求/设计/调度  │───→│ 职责: 逐任务编码     │
│ 可用空间: ~76K        │    │ 可用编码空间: ~121K   │
└──────────────────────┘    └──────────────────────┘
```

### 2.3 SubAgent 的 Context 构成

| 组成部分 | 估算 Token | 说明 |
|---------|-----------|------|
| Coding System Prompt | ~1,500-2,000 | 只含编码规范、TDD 规则、文件操作指南 |
| Task Context | ~2,000-3,000 | 当前任务描述 + 涉及文件 + 验收标准 + 精简的需求/设计摘要 |
| Tool Definitions | ~1,500-2,000 | 6 个工具：read_file、write_file、edit_file、bash、list_directory、search_files |
| Coding History | 剩余全部 | 纯编码对话——文件内容、编译输出、测试结果 |
| **总初始开销** | **~7,000** | **仅占 5%** |
| **可用编码空间** | **~95,000** | **比当前多 ~33K（+53%）** |

### 2.4 主 Agent 与 SubAgent 的职责分工

| 职责 | 主 Agent | 编码 SubAgent |
|------|---------|--------------|
| 需求理解 | ✅ | ❌ |
| 技术设计 | ✅ | ❌ |
| 任务拆分 | ✅ | ❌ |
| 逐任务编码 | ❌（调度） | ✅（执行） |
| 文件读写 | ❌ | ✅ |
| 编译/测试 | ❌ | ✅ |
| 集成联调 | ❌（调度） | ✅（执行） |
| IM 交互 | ✅ | ❌ |
| 记忆管理 | ✅ | ❌ |
| SSH/Browser | ✅ | ❌ |
| 进度报告 | ✅（接收并转发） | ✅（产出） |

### 2.5 通信协议

主 Agent 和 SubAgent 之间通过**结构化消息**通信，不共享 conversation：

```
主 Agent → SubAgent:
{
  "task": "T3: 实现玩家角色控制",
  "description": "实现 Player 类，包含移动、跳跃、碰撞检测",
  "files": ["src/player.h", "src/player.cpp"],
  "acceptance_criteria": ["玩家可以左右移动", "玩家可以跳跃", "碰撞检测正常"],
  "context_summary": "C++ SFML 3.x 项目，CMake 构建，项目根目录 D:\\workprj\\morio",
  "previous_task_outputs": ["src/game.h (已完成)", "src/level.h (已完成)"]
}

SubAgent → 主 Agent:
{
  "status": "completed",
  "files_modified": ["src/player.h", "src/player.cpp", "CMakeLists.txt"],
  "test_result": "3/3 passed",
  "summary": "Player 类实现完成，支持 WASD 移动和空格跳跃，AABB 碰撞检测",
  "iterations_used": 15,
  "tokens_used": 45000
}
```

### 2.6 实现路径

#### Phase 1: 内部 SubAgent（最小改动）

复用已有的 `corelib/agent/loop.go` 的 `RunLoop`，但传入不同的配置：

```go
type CodingSubAgent struct {
    cfg       corelib.MaclawLLMConfig
    httpClient *http.Client
    projectPath string
}

func (s *CodingSubAgent) ExecuteTask(task TaskItem) (*TaskResult, error) {
    // 1. 构建精简的 system prompt（只有编码规则）
    systemPrompt := buildCodingOnlyPrompt(task, s.projectPath)
    
    // 2. 构建精简的工具集（只有 6 个编码工具）
    tools := buildCodingToolSet()
    
    // 3. 构建初始 conversation（system + task context）
    conversation := buildTaskConversation(systemPrompt, task)
    
    // 4. 运行独立的 agent loop
    result := agent.RunLoop(agent.LoopConfig{
        LLMConfig:    s.cfg,
        HTTPClient:   s.httpClient,
        Messages:     conversation,
        Tools:        tools,
        MaxIterations: 50,  // 单任务上限
        Callbacks:    s,    // 进度回调
    })
    
    return parseTaskResult(result), nil
}
```

#### Phase 2: 与 TaskExecutionOrchestrator 集成

`TaskExecutionOrchestrator` 已经有任务调度逻辑（#22），但它的 `Activate()` 从未被调用。Phase 2 将 orchestrator 与 SubAgent 连接：

```go
// orchestrator 调度 → SubAgent 执行
func (o *TaskExecutionOrchestrator) ExecuteNextTask() {
    task := o.CurrentTask()
    result := o.subAgent.ExecuteTask(task)
    
    if result.TestsPassed {
        o.AdvanceToNext()
    } else {
        o.IncrementRetry()
    }
    
    // 向主 Agent 报告进度
    o.reportProgress(task, result)
}
```

#### Phase 3: 外部编程工具降级

当 SubAgent 可用时，不再需要 `create_session` 启动外部编程工具（Claude Code 等）。SubAgent 直接在本地执行编码，消除了：
- 外部进程管理的复杂性
- 编程工具的 API 费用（SubAgent 复用主 Agent 的 LLM provider）
- 编程工具的 rate limit 问题（#20）
- 编程工具的 stdin/stdout pipe 管理

### 2.7 与现有机制的关系

| 现有机制 | SubAgent 方案下的变化 |
|---------|-------------------|
| Coding Tool Gate | 保留——仍然在主 Agent 中拦截编码工具，但不再是为了"强制生成需求文档"，而是为了"路由到 SubAgent" |
| TaskExecutionOrchestrator | 激活——从未被调用的 `Activate()` 终于有了消费者 |
| NeedsConfirm Gate | 保留——仍然在需求/设计/任务分解阶段强制确认 |
| trimConversation | 两套独立运行——主 Agent 和 SubAgent 各自管理自己的 context |
| 漂移检测 | SubAgent 内部独立运行——不受主 Agent 的漂移状态影响 |
| finish_reason=length 续写 | SubAgent 内部独立处理 |

## 3. 收益分析

### 3.1 Context 效率

| 指标 | 当前 | SubAgent 方案 | 改善 |
|------|------|-------------|------|
| 编码可用 context | ~62K (初始) → 0K (129轮后) | ~95K (初始) → ~50K (50轮后) | +53% 初始，不会归零 |
| 单任务最大迭代 | 受主 Agent 累积历史限制 | 独立 context，每个任务从 7K 开始 | 每个任务都有完整的 context 空间 |
| 工具定义开销 | 15K (40+ 工具) | 2K (6 工具) | -87% |
| System prompt 开销 | 12K | 2K | -83% |

### 3.2 编码质量

- **无噪音干扰**：SubAgent 不会看到 IM 规则、Browser 工具、SSH 工具等无关信息，不会产生 `Browser:` 前缀幻觉
- **聚焦单任务**：每次只处理一个任务，不会被其他任务的上下文干扰
- **独立 TDD**：每个任务独立测试，失败不影响其他任务的 context

### 3.3 可靠性

- **不会因 context 膨胀停止**：每个任务的 SubAgent 从 7K 开始，50 轮迭代后约 42K，远低于 128K 上限
- **任务级隔离**：一个任务的 SubAgent 崩溃不影响其他任务
- **可恢复**：任务失败后可以启动新的 SubAgent 重试，不需要恢复整个对话历史

## 4. 风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| SubAgent 不了解项目全局上下文 | Task Context 中注入精简的需求/设计摘要 + 前置任务的产出文件列表 |
| SubAgent 与主 Agent 的 LLM 调用量翻倍 | 单任务 50 轮上限 + 主 Agent 在编码阶段只做调度（极少 LLM 调用） |
| 工具实现需要从 GUI 层提取到 corelib | 渐进迁移——先在 GUI 层实现 SubAgent，后续再提取 |
| 与现有外部编程工具（Claude Code）的兼容 | SubAgent 作为 `tool=internal` 选项，与 `tool=claude-code` 并存 |

## 5. 实施优先级

1. **P0: 激活 TaskExecutionOrchestrator**——当前 `Activate()` 从未被调用，先让它工作起来
2. **P0: 实现 CodingSubAgent**——复用 `corelib/agent/loop.go`，传入精简配置
3. **P1: 主 Agent 编码阶段路由到 SubAgent**——替代当前的"主 Agent 直接编码"模式
4. **P1: SubAgent 进度回调**——实时向主 Agent 报告进度，主 Agent 转发给用户
5. **P2: 与外部编程工具并存**——`tool=internal` (SubAgent) vs `tool=claude-code` (外部)
6. **P2: SubAgent context 管理**——独立的 trimConversation + 漂移检测

## 6. 结论

**有必要启动 SubAgent 进行纯净上下文的编程**。

核心论据：
1. **数据证明**：当前架构下，初始 context 开销 40K（30%），129 轮后膨胀到 134K 导致模型返回空响应。即使 #74 的 token 校准修复了 trimConversation 不触发的问题，裁剪后的 context 仍然包含大量非编码噪音。
2. **机制性问题**：单体 Agent 的 context 是"编码 + 非编码"的混合体，trimConversation 无法区分"编码相关的重要历史"和"IM 规则注入的噪音"——它只能按时间顺序裁剪。
3. **对标行业实践**：Claude Code、Kiro、Cursor 等编程工具都使用独立的编码 context，不混入非编码信息。MaClaw 的 `create_session` 启动外部编程工具本质上就是这个思路，但外部进程管理带来了额外的复杂性（#20 rate limit、#46 SSH 重连、进程管理等）。
4. **基础设施已就绪**：`corelib/agent/loop.go` 的 `RunLoop` 已经是一个通用的 agent loop 实现，`TaskExecutionOrchestrator` 已经有任务调度逻辑。SubAgent 方案是把这些已有组件连接起来，不是从零开始。

SubAgent 方案不是 workaround——它是从架构层面解决"编码 context 被非编码信息污染"这个根本问题。
