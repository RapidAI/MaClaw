# 会话打断轻量化重设计——从"弹框确认"到"意图感知自动调度"

## 1. 问题本质

当前打断机制有两个层面的问题：

### 1.1 调度器信号退化——Embedding 未接线

`im_interrupt_handler.go` 的 `TryInterrupt` 中有一段 TODO：

```go
// TODO: compute message embedding via embedder when wired.
// For now, relevance stays at -1 (unavailable).
_ = taskEmbed
```

`Relevance` 始终为 -1，`Schedule()` 退化为 domain match + structure only。三信号决策矩阵变成了两信号，准确率大幅下降：

- "颜色改红色"（当前任务: 开发游戏）→ `domainMatch=true`（都是 coding）→ Merge - "帮我查天气"（当前任务: 开发游戏）→ `domainMatch=false` + `IsShort=true` → StatusQuery （应该是 Insert）
- "用 C++ 不要 Python"（当前任务: 开发游戏）→ `HasNegation=true` → Replace （应该是 Merge，高相关的修改请求）

没有 Relevance 信号，调度器无法区分"与当前任务相关的修改"和"无关的新任务"。否定结构一刀切 Replace，把"不要 Python 改 C++"这种任务调整也当成了取消。

### 1.2 Insert/Enqueue 未实现——fall through 到等锁

`TryInterrupt` 的 switch 只处理了 Replace、StatusQuery、Merge 三种 action。Insert 和 Enqueue 走 default 分支返回 `InterruptResult{}`（`Handled: false`），消息 fall through 到 `chatLoopMu.Lock()` 等锁。

桌面面板通过 Buffer Queue 缓解了这个问题（用户可以编辑/排队/发射），但 IM 通道完全没有缓冲区 UI，消息卡在锁上直到当前 agent loop 完成。

### 1.3 Merge 注入强度不足——LLM 可能忽略

当前 Merge 注入格式：`[用户补充] 颜色改成红色`。这是一条 system message，LLM 可能当作参考信息忽略，尤其在 context 很长（100K+ token）时。用户说"别用 Python 了改 C++"，期望 LLM 立即调整方案，但 LLM 可能继续用 Python 写完当前文件再看到这条补充。

### 1.4 "太重"的体感来源

用户说"太重了，会弹出 ask_user 界面"——这不是打断机制本身的问题。`ask_user` 是编码工作流阶段确认用的工具，不是打断的产物。但两者在用户体感上混在一起：agent 工作中 → 用户发消息 → 消息卡住等锁 → agent loop 结束 → 弹 ask_user 确认 → 用户以为是打断触发的确认。

根因是 Insert/Enqueue 未实现，消息无法在 agent 工作期间被处理，全部堆积到 agent loop 结束后。

## 2. 机制性分析

### 2.1 已有基础设施盘点

| 组件 | 状态 | 位置 |
|------|------|------|
| `Schedule()` 三信号决策矩阵 | 已实现 | `corelib/progress/scheduler.go` |
| `StructureSignal` + `DetectNegation` | 已实现 | `corelib/progress/structure.go` |
| `MilestoneBuffer` + `taskEmbed` 存储 | 已实现 | `corelib/progress/milestone.go` |
| `InterruptHandler` 接口 + 5 通道接入 | 已实现 | `corelib/progress/interrupt.go` + 各 gateway |
| `imInterruptHandler.TryInterrupt` | 已实现（3/5 action） | `gui/im_interrupt_handler.go` |
| `pendingInjection` Merge 注入 | 已实现 | `gui/im_message_handler.go` |
| `AgentProgressTracker` + `RecordToolCall` | 已实现 | `corelib/progress/agent_integration.go` |
| Embedder（memory store / tool router） | 可用 | `gui/app.go` 中 `a.memoryStore.Embedder()` |
| `CosineSimilarity` | 已实现 | `corelib/progress/scheduler.go` |
| Embedding 接线到 TryInterrupt | TODO | `gui/im_interrupt_handler.go:59` |
| Insert action 实现 | 未实现 | `TryInterrupt` default 分支 |
| Enqueue action 实现 | 未实现 | `TryInterrupt` default 分支 |
| Merge 注入强度分级 | 未实现 | 固定 `[用户补充]` 前缀 |

### 2.2 核心不变量

**调度决策 = f(relevance, domainMatch, structure)**

这个抽象是正确的。问题不在决策矩阵的设计，而在：
1. Relevance 信号缺失（-1），矩阵退化
2. 决策产出后，2/5 的 action 没有执行器

### 2.3 修复策略

不改决策矩阵，不改信号定义，不改接口。只做三件事：
1. 接线 Embedding 信号（让矩阵恢复三信号工作）
2. 实现 Insert/Enqueue 执行器（让所有决策都有执行路径）
3. Merge 注入分级（让注入的消息被 LLM 正确对待）

## 3. 修复方案

### 3.1 Embedding 信号接线（P0）

**根因**：`imInterruptHandler` 没有 embedder 引用，无法计算新消息的 embedding。

**修复**：

#### `gui/im_interrupt_handler.go`

```go
type imInterruptHandler struct {
    handler           *IMMessageHandler
    milestoneTrackers sync.Map
    embedder          embedding.Embedder  // 新增
}

func newIMInterruptHandler(h *IMMessageHandler) *imInterruptHandler {
    return &imInterruptHandler{handler: h}
}

// SetEmbedder configures the embedder for relevance computation.
// Called from app.go after embedding model is loaded.
func (ih *imInterruptHandler) SetEmbedder(emb embedding.Embedder) {
    ih.embedder = emb
}
```

`TryInterrupt` 中的 TODO 替换为：

```go
var relevance float64 = -1
if tracker != nil {
    taskEmbed := tracker.Buffer().TaskEmbed()
    if taskEmbed != nil && ih.embedder != nil && !embedding.IsNoop(ih.embedder) {
        msgEmbed, err := ih.embedder.Embed(messageText)
        if err == nil && len(msgEmbed) > 0 {
            relevance = progress.CosineSimilarity(taskEmbed, msgEmbed)
        }
    }
}
```

#### `gui/app.go` 接线

在 `activateEmbedderAsync` 或 handler 初始化后，将 embedder 传递给 interrupt handler：

```go
if a.memoryStore != nil {
    emb := a.memoryStore.Embedder()
    if emb != nil && !embedding.IsNoop(emb) {
        handler.interruptHandler.SetEmbedder(emb)
    }
}
```

#### `runAgentLoop` 中 taskEmbed 初始化

当前 `AgentProgressTracker` 创建时 `taskEmbed` 传 nil。需要在 agent loop 开始时计算 task embedding：

```go
var taskEmbed []float32
if ih := h.interruptHandler; ih != nil && ih.embedder != nil && !embedding.IsNoop(ih.embedder) {
    if vec, err := ih.embedder.Embed(userText); err == nil {
        taskEmbed = vec
    }
}
loopProgressTracker = progress.NewAgentProgressTracker(onProgress, userText, intentLabel, taskEmbed)
```

**效果**：`Schedule()` 从两信号退化模式恢复为三信号完整模式。"用 C++ 不要 Python" 的 relevance 与 "开发游戏" 高相关（>0.6），即使有否定结构也会被判定为 Merge 而非 Replace。

### 3.2 Insert 实现——轻量插入（P1）

**设计原则**：不做完整的"暂停→恢复"（太重，需要序列化整个 agent loop 状态）。Insert/Enqueue 的消息不被消费，让它继续走 gateway 的正常排队路径（等 chatLoopMu 释放后自然处理）。

**为什么不用 goroutine 自动处理**：

初版设计用 `pendingInsert sync.Map` 存消息，在 `handleIMMessageWithLoop` 返回前用 goroutine 调用 `HandleIMMessageWithProgress`。审视发现三个机制性问题：

1. **响应投递断裂**：goroutine 中 `HandleIMMessageWithProgress` 的返回值没有消费者。Hub 模式靠 `sendIMAgentResponse` 投递，IM 通道靠 gateway 的 `handler(incoming)` 回调投递，桌面面板靠 `onToken`/`onProgress` 回调投递——goroutine 里都没有这些。用户看不到响应。

2. **锁竞态**：goroutine 在 `defer chatLoopMu.Unlock()` 执行前启动，会阻塞在 Lock 上。如果在 Unlock 和 goroutine 获得 Lock 之间有新消息到达并先获得锁，pendingInsert 的消息会排在新消息后面。

3. **多条覆盖**：`sync.Map.Store` 覆盖旧值，多条 Insert 消息只保留最后一条，但每条都回复了"收到"。

**正确的机制**：`InterruptResult` 新增 `Queued` 字段。Insert/Enqueue 返回 `Handled: false, Queued: true`——gateway 发送 Reply 作为即时反馈，但不消费消息，让它继续走正常排队路径。消息在 chatLoopMu 释放后被 gateway 的正常处理流程处理，使用 gateway 自己的响应投递机制。

```go
// corelib/progress/interrupt.go
type InterruptResult struct {
    Handled bool           // true if the message was fully processed (don't queue it)
    Action  ScheduleAction // which action was taken
    Reply   string         // optional text to send back to the user immediately
    Queued  bool           // message acknowledged but NOT consumed — let it queue
}
```

**效果**：
- 用户发"帮我查天气" → 立即收到"收到，当前任务完成后立即处理" → 当前 loop 结束 → 消息被正常处理 → 用户收到天气结果
- 多条 Insert 消息不会互相覆盖——每条都在 gateway 的队列里等着
- 响应投递走 gateway 的正常路径，不需要额外的投递机制

### 3.4 Merge 注入分级（P2）

**根因**：所有 Merge 消息都用 `[用户补充]` 前缀，LLM 对待补充信息和强制修改指令的优先级相同。

**修复**：根据 `StructureSignal` 和 `Relevance` 分级注入。

#### `gui/im_interrupt_handler.go` — Merge 分支增强

```go
case progress.ActionMerge:
    // 根据信号强度分级注入
    injection := classifyMergeInjection(messageText, decision, input)
    ih.handler.pendingInjection.Store(userID, injection)
    return progress.InterruptResult{
        Handled: true,
        Action:  progress.ActionMerge,
        Reply:   "收到，已纳入当前任务。",
    }
```

#### `gui/im_interrupt_handler.go` — 新增 `classifyMergeInjection`

```go
// classifyMergeInjection determines the injection format based on signal strength.
// Three tiers:
//   - Directive: negation + high relevance → user wants to CHANGE something
//   - Supplement: high relevance, no negation → user adds information
//   - Note: medium relevance → informational, LLM may consider
func classifyMergeInjection(text string, decision progress.ScheduleDecision, input progress.ScheduleInput) string {
    s := input.Structure

    if s.HasNegation {
        // "不要 Python 改 C++"、"别用那个库"
        // 否定 + 高相关 = 用户要求修改当前方案
        return "[用户要求修改——必须立即执行] " + text
    }

    if decision.Confidence >= 0.80 {
        // 高置信度 Merge = 明确的补充需求
        return "[用户补充需求——请在当前任务中纳入] " + text
    }

    // 中等置信度 = 可能相关的信息
    return "[用户补充] " + text
}
```

#### `gui/im_message_handler.go` — 消费端不变

注入消费逻辑不变（`pendingInjection.LoadAndDelete` → 追加为 system message）。分级体现在注入文本的前缀中，LLM 通过前缀理解优先级。

**为什么前缀分级不是 workaround**：

LLM 对 system message 的遵从度与措辞强度正相关。"必须立即执行"比"请纳入"比"补充"有更高的遵从率。这不是关键词 hack，而是利用 LLM 的指令遵从特性。三个前缀对应三种语义强度，由可计算的信号（negation + confidence）决定，不依赖关键词列表。

### 3.5 封装改进——EmbedText 访问器（审视修正）

初版实现中 `im_message_handler.go` 直接访问 `h.interruptHandler.embedder`（未导出字段）。虽然同包合法，但跨了封装边界。

**修正**：`imInterruptHandler` 新增 `EmbedText(text string) []float32` 方法，封装 nil 检查 + IsNoop 检查 + Embed 调用 + 错误处理。`im_message_handler.go` 通过此方法获取 taskEmbed，不再直接访问 embedder 字段。

## 4. 完整的打断场景清单

修复后，所有场景都有明确的执行路径：

| # | 场景 | 用户消息示例 | 调度决策 | 执行路径 |
|---|------|-------------|---------|---------|
| 1 | 放弃当前任务 | "算了"、"不做了"、"停" | Replace | Cancel loop → 保存产出 → 回复"已停止" |
| 2 | 调整当前任务（否定式） | "别用 Python 了改 C++" | Merge（高相关+否定） | 注入 `[用户要求修改——必须立即执行]` |
| 3 | 补充当前任务 | "加个排行榜"、"颜色改红色" | Merge（高相关） | 注入 `[用户补充需求——请纳入]` |
| 4 | 查询进度 | "？"、"到哪了" | StatusQuery | 返回 `ProgressSummary()` |
| 5 | 插入紧急任务 | "先帮我查个天气" | Insert | 存入队列 → loop 结束后自动处理 |
| 6 | 排队等完成 | "做完这个再帮我翻译" | Enqueue | 存入队列 → loop 结束后自动处理 |
| 7 | 否定式新任务 | "取消服务器上的定时任务" | Replace（低相关+否定）→ 实际应为 Insert | **Embedding 修复后**：低相关+否定 → Replace （用户确实要放弃当前任务去做新的） |

### 场景 7 的特殊分析

"取消服务器上的定时任务"——包含"取消"但不是取消当前 agent 任务。

- 当前任务："开发游戏"
- Relevance：低（"取消定时任务"与"开发游戏"语义不相关）
- HasNegation：true（"取消"匹配否定模式）
- 调度矩阵：否定 + 低相关 → Replace

这个决策是**正确的**。用户在 agent 开发游戏时说"取消服务器上的定时任务"，语义是"放弃当前的游戏开发，去处理服务器的事"。Replace 放弃当前任务 + 处理新消息，符合用户意图。

如果用户想在不放弃游戏开发的情况下处理服务器任务，他会说"帮我顺便取消服务器上的定时任务"——"顺便"使消息更长（IsMedium），且 Relevance 仍然低，矩阵判定为 Insert。

**关键**：Embedding 接线后，否定结构不再一刀切 Replace。高相关+否定 → Merge（"别用 Python"），低相关+否定 → Replace（"取消定时任务"）。这是三信号矩阵的设计意图，之前因为 Relevance=-1 退化了。

## 5. 修改文件清单

### Phase 1: Embedding 接线（P0）

- `gui/im_interrupt_handler.go`：
  - `imInterruptHandler` 新增 `embedder` 字段 + `SetEmbedder()` 方法 + `EmbedText()` 访问器
  - `TryInterrupt` 中 TODO 替换为实际 embedding 计算
- `gui/app.go`：
  - handler 初始化后接线 `interruptHandler.SetEmbedder(emb)`
- `gui/app_embedding.go`：
  - `activateEmbedderAsync` 中接线 interrupt handler 的 embedder
- `gui/weixin_gateway.go`：
  - 微信本地 handler 初始化时接线 interrupt handler 的 embedder
- `gui/im_message_handler.go`：
  - `runAgentLoop` 中通过 `EmbedText()` 计算 taskEmbed 传入 `AgentProgressTracker`

### Phase 2: Insert/Enqueue 执行器 + InterruptResult.Queued（P1）

- `corelib/progress/interrupt.go`：
  - `InterruptResult` 新增 `Queued bool` 字段
- `gui/im_interrupt_handler.go`：
  - `TryInterrupt` 新增 `ActionInsert` 和 `ActionEnqueue` 分支，返回 `Handled: false, Queued: true`
- `corelib/weixin/gateway.go`：
  - interrupt 处理支持 `Queued`——发送 Reply 但不 return，消息继续排队
- `corelib/telegram/gateway.go`：
  - 同上，Queued 时通过 `SendText` 发送即时反馈
- `corelib/qqbot/gateway.go`：
  - 同上
- `gui/remote_hub_client.go`：
  - Hub 模式 Queued 时通过 `sendIMAgentResponse` 发送即时反馈
- `gui/im_message_handler.go`：
  - 桌面面板 authoritative check 中 Queued 消息 fall through 到 Lock

### Phase 3: Merge 注入分级（P2）

- `gui/im_interrupt_handler.go`：
  - 新增 `classifyMergeInjection()` 函数
  - `ActionMerge` 分支调用 `classifyMergeInjection` 替代直接存储原文

## 6. 验收标准

- Agent 开发游戏时用户说"颜色改红色" → Merge（高相关），注入 `[用户补充需求]`，LLM 在下一轮迭代中调整颜色
- Agent 开发游戏时用户说"别用 Python 改 C++" → Merge（高相关+否定），注入 `[用户要求修改——必须立即执行]`，LLM 立即切换语言
- Agent 开发游戏时用户说"算了" → Replace（低相关+否定），Cancel loop，回复"已停止"
- Agent 开发游戏时用户说"帮我查天气" → Insert（低相关+异域），回复"收到，完成后立即处理"，loop 结束后自动处理
- Agent 开发游戏时用户说"？" → StatusQuery，返回进度摘要
- Embedding 不可用时 → 退化为 domain match + structure（行为与当前一致，不 regress）
- 所有 16 个现有 scheduler 测试通过
- 所有 5 个现有 AgentProgressTracker 测试通过
- 新增 8 个 embedding-aware scheduler 测试（覆盖高相关+否定 → Merge 等关键路径）
- 新增 3 个 Insert/Enqueue 执行器测试
- 新增 3 个 Merge 注入分级测试

## 7. 与现有机制的关系

| 机制 | 关系 | 说明 |
|------|------|------|
| `ask_user` 工具 | 正交 | ask_user 是 LLM 主动向用户提问的工具，打断是用户主动向 agent 发消息。两者独立 |
| Buffer Queue（桌面） | 互补 | Buffer Queue 是前端 UI 层的排队机制，本修复是后端调度层的执行机制。前端排队 + 后端调度 = 完整体验 |
| `pendingAskUser` | 不冲突 | ask_user pending 状态在 Replace 时被清除（已有逻辑），Insert/Enqueue 不影响 |
| Coding Tool Gate | 不冲突 | Gate 在 agent loop 内部工作，打断在 agent loop 外部工作。Merge 注入的消息进入 loop 后受 gate 约束 |
| WorkflowEngine | 不冲突 | 工作流阶段确认是 loop 内部的 NeedsConfirm 机制，打断是 loop 外部的消息调度。Insert 的消息在 loop 结束后作为新 loop 处理，不影响当前工作流状态 |
| DriftDetector | 不冲突 | Merge 注入的消息可能帮助 LLM 跳出漂移（用户补充信息提供了新路径）|

## 8. 未来演进

### 8.1 并行 Agent Loop（多 Slot）

当前架构是单 chat loop + 多 background loop。如果未来支持多个并行 chat loop：
- Insert 升级为"在独立 slot 中并行处理"
- Enqueue 保持排队语义
- `Schedule()` 接口不变，只改执行器

### 8.2 Embedding 缓存

`TryInterrupt` 中每次调用 `embedder.Embed(messageText)` 有 ~5ms 延迟。如果成为瓶颈，可以复用 `QueryEmbeddingCache`（已有，`corelib/tool/hybrid.go`）做 LRU 缓存。当前不需要——5ms 对打断决策来说完全可接受。

### 8.3 优先级提升

用户说"这个先放放，先做那个更重要的"——本质是 Replace + Enqueue（放弃当前 + 把当前任务存为 UnfinishedSlot + 处理新任务）。当前 Replace 已经通过 `cancelledExitResponse` 保存历史，结合 #55 的 In-Flight Task Marker，重启后可以恢复。但"主动暂存当前任务"需要新增 `SuspendToSlot` 动作，作为后续迭代。

### 8.4 部分回滚

用户说"刚才那步不对，回退到上一步"——需要 checkpoint 机制（保存每个工具调用前的 conversation 快照）。这是一个独立的大特性，不在本次范围内。
