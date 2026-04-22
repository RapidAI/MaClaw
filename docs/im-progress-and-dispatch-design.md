# IM 通道进度机制 + 消息调度器 + 预输入队列改进 设计文档

## 1. 问题概述

### 1.1 IM 通道进度消息刷屏

**现状**：IM 通道（飞书/微信/QQ/Telegram）在 agent loop 运行期间，每 10 秒发送一条进度消息（"任务较复杂，正在耐心处理中"/"正在执行工具，请稍候"）。#37 做了分类节流（`progressMinInterval=10s` + 同类中间状态消息去重），但核心问题未解决——**不区分任务复杂度，所有任务都用同一套 nudge 策略，且消息没有信息量**。

用户查天气（5 秒完成），10 秒后收到"任务较复杂"——荒谬。用户开发游戏（5 分钟），每 10 秒收到一条空洞的"正在处理中"——刷屏且无用。

### 1.2 长程任务中用户失去控制权

**现状**：IM 通道的消息处理是同步阻塞的——`SendAIAssistantMessage` 在 agent loop 运行期间不接受新消息。用户发的"取消"/"补充信息"/"新任务"要等当前 loop 结束后才被处理。桌面面板有暂停键（context cancel），但 IM 没有等价机制。

### 1.3 桌面面板预输入队列功能不足

**现状**：预输入队列中的多条消息在 agent loop 结束后一次性处理。用户无法控制处理顺序，无法让某条消息优先处理，无法在等待过程中取消或补充当前任务。

### 1.4 核心矛盾

用户需要三件事：
1. **感知进度**——知道系统在干活、干到哪了
2. **不被打扰**——不想被无意义的状态消息刷屏
3. **保持控制权**——随时可以取消、补充、插入新任务

---

## 2. 设计原则

1. **机制性修复**：不用关键词列表做意图判断，不用硬编码规则做调度决策。所有分类和决策基于可计算的信号（embedding 相似度、意图融合结果、语法结构）
2. **复用已有基础设施**：embedding cosine（intent-fusion L2 通道）、意图分类（UnifiedIntentClassifier）、context cancel（桌面面板已有）
3. **桌面和 IM 统一后端**：调度决策逻辑共享，触发方式和 UI 交互各自实现
4. **事件驱动而非定时推送**：进度消息只在有新信息时发送，不按固定间隔


---

## 3. 事件驱动进度机制

### 3.1 核心转变

从"定时 nudge"变成"里程碑推送"。只在有新信息时才发消息。

### 3.2 MilestoneBuffer（共享数据源）

所有进度相关功能共享同一个里程碑缓冲区。MilestoneBuffer 是本设计中三个子系统（进度推送、消息调度、状态查询）的共享数据源。

```go
// corelib/progress/milestone.go

type Milestone struct {
    Time      time.Time
    Tool      string   // 工具名
    Summary   string   // 从工具 name+args 机械提取的摘要
    Phase     string   // "connecting"/"executing"/"generating"/...
    Completed bool     // 该步骤是否已完成
}

type MilestoneBuffer struct {
    mu         sync.RWMutex
    milestones []Milestone
    taskDesc   string       // 当前任务描述（调度器用）
    taskIntent string       // 当前任务意图标签（调度器用）
    taskEmbed  []float32    // 当前任务描述的 embedding 向量（调度器用）
    startTime  time.Time    // 任务开始时间
}

func (b *MilestoneBuffer) Record(m Milestone)           // 写入里程碑
func (b *MilestoneBuffer) Since(t time.Time) []Milestone // 读取指定时间后的里程碑
func (b *MilestoneBuffer) Latest() *Milestone            // 最新里程碑
func (b *MilestoneBuffer) ProgressSummary() string       // 当前进度摘要文本
func (b *MilestoneBuffer) Reset(taskDesc, taskIntent string, taskEmbed []float32) // 新任务重置
```

**消费者**：
- 进度推送器：从 buffer 读取里程碑，按事件驱动推送给用户
- 消息调度器：读取 `taskDesc`/`taskEmbed` 计算新消息与当前任务的相关性
- 状态查询：读取 `ProgressSummary()` 回复用户"做到哪了？"
- 取消处理：读取已完成里程碑，生成产出物摘要

### 3.3 里程碑提取器（零 LLM 开销）

从工具调用的 name + args + result 中机械提取摘要，不需要 LLM：

```go
// corelib/progress/extractor.go

// toolSummaryRules 声明式映射，新增工具只需加一行
var toolSummaryRules = map[string]SummaryRule{
    "web_search":       {Verb: "搜索", ArgKey: "query", MaxLen: 30},
    "write_file":       {Verb: "生成", ArgKey: "path",  MaxLen: 50},
    "bash":             {Verb: "执行", ArgKey: "command", MaxLen: 30},
    "ssh":              {Verb: "服务器", ArgKey: "command", MaxLen: 30, Prefix: actionFromSSHAction},
    "generate_pdf":     {Verb: "生成 PDF", Static: true},
    "create_session":   {Verb: "启动编程环境", Static: true},
    "send_and_observe": {Verb: "编码中", ArgKey: "text", MaxLen: 40},
    "run_skill":        {Verb: "执行技能", ArgKey: "skill_name", MaxLen: 30},
}

// 静默工具——不产生里程碑，用户不关心这些内部操作
var silentTools = map[string]bool{
    "read_file": true, "memory": true, "discover_tool": true,
    "task": true, "ask_user": true, "manage_config": true,
}

func ExtractMilestone(toolName string, args map[string]any, result string) *Milestone
```

**设计要点**：
- 声明式规则表，新增工具只需加一行，不改逻辑
- 静默工具列表排除内部操作，减少噪音
- `MaxLen` 截断防止摘要过长
- SSH 工具根据 action（connect/exec/close）动态生成动词

### 3.4 三层进度推送策略

#### 第一层：即时确认（所有非轻量任务，仅一次）

用户发消息后 2-3 秒内，如果任务未完成，发一条确认：`"收到，正在处理 🔄"`。之后不再发同类消息。

**轻量任务判定**（不发即时确认）：消息 <80 字 + 意图融合结果非 coding/ssh/workflow + 非工作流阶段。轻量任务预期 <30 秒完成，结果本身就是最好的"确认"。

**动态升级**：轻量任务如果 30 秒内未完成，自动升级——补发即时确认 + 开始里程碑追踪。

#### 第二层：里程碑推送（事件驱动）

每次工具调用完成后，`ExtractMilestone` 提取摘要写入 `MilestoneBuffer`。推送器检查：

1. 距上次推送是否超过 **30 秒**（合并窗口）
2. 是否有新的非静默里程碑

满足条件则推送。合并窗口内的多个里程碑合并为一条：

```
1 个里程碑：直接推送原文
  → "已搜索到相关资料，正在整理..."

2-3 个：合并
  → "已完成: 搜索资料(3个来源) + 读取文件(2个)，正在生成报告..."

4+ 个：聚合
  → "已完成 6 个步骤，正在编写 game.js..."
```

#### 第三层：静默兜底（防"假死"感知）

如果超过 **90 秒** 没有任何里程碑推送（长时间单步操作），发一条心跳：

```
"仍在执行中，当前: 等待服务器编译完成（已耗时 2 分钟）..."
```

之后每 **120 秒** 一次心跳，最多 **3 次**。3 次心跳后发最终通知：

```
"任务耗时较长，完成后会立即通知你。"
```

然后彻底静默，直到任务完成。

### 3.5 效果对比

| 场景 | 现在 | 改进后 |
|------|------|--------|
| 查天气（5s） | 10s 后 "正在处理中" | 零 nudge，直接出结果 |
| 搜资料（40s） | 10s/20s/30s 三条 "正在处理中" | "收到 🔄" + "已搜索到 3 个来源，正在整理..." |
| 编码任务（5min） | 每 10s 一条，共 30 条刷屏 | "收到 🔄" + 3-4 条里程碑 + 完成通知 |
| SSH 部署（3min） | 交替 "正在处理"/"正在执行工具" | "收到 🔄" + "连接服务器..." + "git clone 完成" + "编译中..." |

消息数量从 **10-30 条** 降到 **2-5 条**，每条都有信息量。


---

## 4. 消息调度器（Message Scheduler）

### 4.1 问题本质

用户在 agent loop 运行中发了一条消息。系统需要决定怎么处理它。

之前的思路是把这当作"意图分类问题"——给消息贴 cancel/supplement/new_task 标签。但这是 workaround，因为：

**调度决策不是消息本身的属性，是消息与当前执行上下文的关系属性。**

- "用红色" → 当前任务是"开发游戏" → 补充信息
- "用红色" → 当前任务是"查天气" → 新任务
- "取消订单" → 不是取消当前操作，是新任务
- "停一下" → 可能是取消，也可能是"让我看看进度"

把关系属性硬塞进意图分类的 label 体系里，在消息本身语义明确时能工作（"取消"→ cancel），但在需要上下文判断时失效。

### 4.2 正确抽象：基于上下文相关性的调度

```
Schedule(newMsg, currentCtx) → Decision

其中 Decision = f(relevance, intentMatch, structure)
```

调度决策由三个正交信号决定：

#### 信号 1：语义相关性（Relevance）

新消息和当前任务的语义相关程度。这不是意图分类，是 **embedding 语义相似度计算**——已有基础设施。

```go
// 复用 intent-fusion L2 通道的 embedding 能力
relevance = cosine(embed(newMsg.Text), milestoneBuffer.taskEmbed)
```

- 高相关（>0.6）= 大概率补充信息
- 低相关（<0.3）= 大概率新任务或控制指令
- 中间地带 = 需要更多信号

`taskEmbed` 在任务开始时计算一次，存入 `MilestoneBuffer`，后续所有新消息复用。

#### 信号 2：意图域匹配（Intent Domain Match）

新消息和当前任务是否属于同一个意图域。复用现有 12 个 IntentLabel 的分类结果，不新增 label：

```go
// 复用 UnifiedIntentClassifier 的并行融合结果
newMsgIntent = UIC.Classify(newMsg.Text)
domainMatch  = (newMsgIntent.Domain == milestoneBuffer.taskIntent.Domain)
```

- 同域（都是 Coding）= 大概率补充
- 异域（Coding vs Content）= 大概率新任务
- 新消息意图 = unknown/ambiguous + 极短 = 可能是控制指令

**关键**：这里用的是意图融合已有的 Domain 分组（Coding/Remote/Browser/Content/Special），不是新增的 Dispatch domain。意图融合回答"这条消息想干什么"，调度器用这个答案和当前任务做比较。两个系统职责分离。

#### 信号 3：消息结构特征（Structure）

不依赖关键词列表，而是检测中文/英文的 **语法结构模式**：

```go
// corelib/progress/structure.go

type StructureSignal struct {
    Length       int     // rune 计数
    IsShort      bool    // <5 rune
    IsMedium     bool    // 5-30 rune
    IsLong       bool    // >30 rune
    HasNegation  bool    // 否定句法模式
}

// 否定句法检测——语法结构，非关键词堆砌
// 中文否定句尾模式：不/别/没/算 + 了/吧/啦/呢
// 中文否定祈使：先不/暂时不/还是不/不要/不用/别
// 英文否定模式：don't/no longer/never mind/forget it/stop + 句末
func DetectNegation(text string) bool
```

**为什么否定句法不是关键词 workaround**：
- 关键词列表是开放域——"算了"、"不搞了"、"我改主意了"永远追不完
- 否定句法是封闭域——中文的否定表达有固定的句法结构（`不/别/没/算 + 了/吧/啦`），这是语法规则不是语义猜测
- 新的否定表达只要符合这个句法结构就自动覆盖，不需要维护列表

### 4.3 调度决策矩阵

三个信号组合产生五种调度决策：

```
                  高相关 + 同域意图    低相关 + 异域意图    低相关 + 无明确意图
否定结构           Replace             Replace             Replace
非否定 + 短消息    Merge               Enqueue             StatusQuery
非否定 + 中消息    Merge               Insert              Insert
非否定 + 长消息    Merge               Insert              Insert
```

### 4.4 五种调度动作

#### Replace（替换）——放弃当前任务，处理新消息

触发条件：否定结构（用户明确要放弃）

```
用户: "算了不做了，帮我查天气"
  → cancel agent loop context
  → 等待当前工具调用优雅退出（最多 5 秒）
  → 收集已完成产出物（从 MilestoneBuffer 读取）
  → 保存部分产出到对话历史
  → 回复: "已停止。之前已完成: game.js, style.css"
  → 处理新消息
  → 清理所有 pending 状态
```

#### Merge（合并）——注入当前 agent loop，不中断

触发条件：高相关 + 同域意图（补充信息）

```
用户: "颜色改成红色"（当前任务: 开发游戏）
  → 消息存入 pendingInjection sync.Map[userID]
  → agent loop 下一轮迭代开始时消费
  → 注入为 system message: "[用户补充] 颜色改成红色"
  → 回复: "收到，已纳入当前任务。"
```

#### Insert（插入）——暂停当前，处理新消息，恢复当前

触发条件：低相关 + 异域意图 + 非否定（新任务但不放弃当前）

```
用户: "帮我查下杭州天气"（当前任务: 开发游戏）
  → 暂停当前 agent loop（cancel context + 保存恢复点）
  → 处理插入的消息（独立 agent loop）
  → 插入消息完成后，自动恢复被暂停的任务
  → 回复: "先处理你的新消息，之后继续之前的任务。"
```

恢复点数据结构：

```go
type SuspendedTask struct {
    UserID               string
    OriginalMessage      string
    ConversationSnapshot []conversationEntry  // 对话历史快照
    MilestoneBuffer      []Milestone          // 已完成的里程碑
    PartialOutput        string               // 已生成的部分文本
    PendingStates        map[string]any       // pendingAskUser 等状态
    SuspendTime          time.Time
}
```

恢复时注入系统消息：`"[系统] 之前的任务被暂停处理了一个插入请求，现在恢复。已完成的进度: {里程碑摘要}。请继续。"`

#### Enqueue（排队）——等当前完成后处理

触发条件：低相关 + 异域意图 + 短消息（不紧急的新任务）

```
用户: "？"（当前任务: 开发游戏）
  → 存入 per-user 消息队列
  → agent loop 结束后自动 dequeue 处理
  → 回复: "当前有任务在执行，完成后立即处理。"
  → 队列上限 3 条，超出丢弃最旧的并通知用户
```

#### StatusQuery（状态查询）——返回进度，不干扰

触发条件：低相关 + 无明确意图 + 极短消息（"？"/"做到哪了"）

```
用户: "？"
  → 从 MilestoneBuffer 读取 ProgressSummary()
  → 回复: "正在编写 game.js（已完成 3/5 个文件），预计还需 2 分钟"
  → 不干扰 agent loop
```


### 4.5 统一中断层（InterruptAndProcess）

Replace 和 Insert 都需要中断当前 agent loop。抽象为统一接口：

```go
// corelib/progress/interrupt.go

type InterruptReason string

const (
    InterruptReplace    InterruptReason = "replace"     // 放弃当前 + 处理新的
    InterruptInsert     InterruptReason = "insert"      // 暂停当前 + 处理新的 + 恢复
    InterruptUserClick  InterruptReason = "user_click"  // 桌面面板用户点击发射按钮
)

type InterruptRequest struct {
    UserID     string
    NewMessage Message
    Reason     InterruptReason
}

type InterruptResult struct {
    PartialOutput    string       // 被中断任务的部分产出
    CompletedSteps   []Milestone  // 已完成的里程碑
    SuspendedTask    *SuspendedTask // Insert 模式下的恢复点（Replace 模式为 nil）
}

func (h *IMMessageHandler) InterruptAndProcess(req InterruptRequest) (*InterruptResult, error) {
    // 1. cancel 当前 agent loop 的 context
    // 2. 等待优雅退出（当前工具调用完成或超时 5 秒）
    // 3. 收集部分产出（从 MilestoneBuffer + conversation history）
    // 4. Insert 模式：保存恢复点
    // 5. Replace 模式：保存部分产出到对话历史，标记为已中断
    // 6. 清理 pending 状态（pendingAskUser、pendingCapabilityGap、drift state 等）
    // 7. 返回 InterruptResult
}
```

桌面面板和 IM 通道共享这个接口，区别只在触发方式：
- 桌面：用户点击 [⏎] 按钮 → `InterruptAndProcess(UserClick, msg)`
- IM：调度器判定 Replace → `InterruptAndProcess(Replace, msg)`
- IM：调度器判定 Insert → `InterruptAndProcess(Insert, msg)`

### 4.6 IM 通道异步化

当前 IM 消息处理是同步阻塞的。需要改为异步，才能在 agent loop 运行中接收新消息：

```go
// hub/internal/im/async_dispatcher.go

type AsyncDispatcher struct {
    scheduler    *MessageScheduler       // 调度决策
    cancelFuncs  sync.Map                // per-user cancel context
    msgQueues    sync.Map                // per-user 消息队列
    handler      *IMMessageHandler       // 后端处理器
}

// 消息到达时立即调用，不阻塞
func (d *AsyncDispatcher) OnMessage(userID string, msg Message) {
    if !d.isAgentLoopRunning(userID) {
        // 空闲状态，直接处理
        go d.processMessage(userID, msg)
        return
    }
    
    // agent loop 运行中，做调度决策
    decision := d.scheduler.Schedule(msg, d.getMilestoneBuffer(userID))
    
    switch decision.Action {
    case ActionReplace:
        d.handler.InterruptAndProcess(InterruptRequest{
            UserID: userID, NewMessage: msg, Reason: InterruptReplace,
        })
        go d.processMessage(userID, msg)
        
    case ActionMerge:
        d.handler.InjectMessage(userID, msg)
        d.replyIM(userID, "收到，已纳入当前任务。")
        
    case ActionInsert:
        result, _ := d.handler.InterruptAndProcess(InterruptRequest{
            UserID: userID, NewMessage: msg, Reason: InterruptInsert,
        })
        go func() {
            d.processMessage(userID, msg)
            if result.SuspendedTask != nil {
                d.resumeTask(userID, result.SuspendedTask)
            }
        }()
        
    case ActionEnqueue:
        d.enqueue(userID, msg)
        d.replyIM(userID, "当前有任务在执行，完成后立即处理。")
        
    case ActionStatusQuery:
        summary := d.getMilestoneBuffer(userID).ProgressSummary()
        d.replyIM(userID, summary)
    }
}
```

**延迟考量**：
- L1 keyword + 否定句法 + 消息长度：<1ms，覆盖大多数明确场景（"停"/"算了"）
- L2 embedding relevance：~5ms，覆盖相关性判断
- 意图融合完整流程（L2+L3 并行）：2-8s，仅在 L1+L2 不确定时触发
- IM 策略：L1 CLEAR → 立即执行；L1 不确定 → 先回复"收到" → 等 L2 → 决策；极少数需要 L3

---

## 5. 桌面面板预输入队列改进

### 5.1 逐条处理

**现状**：队列中多条消息在 agent loop 结束后一次性处理。

**改进**：逐条处理，每条独立 agent loop。处理完第 1 条 → 再处理第 2 条 → 再处理第 3 条。

```typescript
// 前端伪代码
async function processQueue() {
    while (queue.hasNext()) {
        const msg = queue.peek()
        msg.status = 'active'
        
        try {
            const resp = await sendMessage(msg.text)
            msg.status = 'completed'
            queue.dequeue()
        } catch (e) {
            if (e.type === 'interrupted') {
                msg.status = 'interrupted'
                // 不 dequeue，保持在队列中让用户决定
            } else {
                msg.status = 'failed'
            }
        }
    }
}
```

### 5.2 引导发射（Guided Fire）

队列中每条等待的消息旁边有 [⏎] 按钮。点击后不是直接中断，而是先做调度决策——判断这条消息和当前正在处理的消息的关系。

#### 预分类（零延迟交互）

消息入队时异步触发调度决策计算（`Schedule(msg, currentCtx)`），结果缓存。UI 上直接显示推荐动作图标：

```
┌─────────────────────────────────────────────────────┐
│  当前: "开发贪吃蛇游戏"  [处理中 🔄]                  │
│                                                       │
│  ① "颜色改红色"     [↗ 合并]   ← relevance=0.72      │
│  ② "查下天气"       [⏩ 插入]   ← relevance=0.08      │
│  ③ "算了不做了"     [🔄 替换]   ← negation + low rel  │
└─────────────────────────────────────────────────────┘
```

推荐动作是预计算的，点击即执行，零延迟。

#### 用户可覆盖

点击 [⏎] 默认执行推荐动作。长按或右键展开菜单让用户覆盖：

```
[↗ 合并到当前任务]    ← 系统推荐（高亮）
[⏩ 插入优先处理]
[🔄 替换当前任务]
```

### 5.3 队列状态机

每条消息有 6 种状态：

```
pending     → 排队中，显示推荐动作图标 + [⏎] 按钮
active      → 处理中，显示 🔄 动画
completed   → 已完成，显示 ✅（几秒后折叠）
interrupted → 被中断（Insert 模式暂停），显示 ⏸️ + [↻ 恢复] [✕ 放弃]
abandoned   → 被放弃（Replace 模式），显示 ⚠️ 已放弃
failed      → 处理失败，显示 [↻ 重试] [✕ 删除]
merged      → 已合并到当前任务，显示 ↗ 已合并
```

状态转换：

```
pending → active → completed
                 → interrupted → resumed (自动恢复) → active → completed
                               → abandoned (用户点击放弃)
                 → abandoned (被 Replace)
                 → failed → active (用户点击重试)

pending → merged (被 Merge 合并到当前任务)
pending → active (被 [⏎] 按钮提升为当前任务)
```

### 5.4 被中断消息的处理

Insert 模式下，当前任务被暂停。插入消息完成后：

- **自动恢复**：系统自动恢复被暂停的任务，注入恢复上下文
- 用户也可以在 `interrupted` 状态下点击 [✕ 放弃] 手动放弃

Replace 模式下，当前任务被放弃：

- 标记为 `abandoned`，部分产出保留在对话历史中
- 用户可以看到之前的工作成果，但不会自动恢复


---

## 6. 与意图融合机制的关系

### 6.1 职责分离

```
意图融合（UIC）：消息 → 意图（coding/ssh/browser/search/...）
消息调度器：    (消息, 上下文) → 调度决策（merge/insert/replace/enqueue/status）
```

两个系统正交。意图融合回答"这条消息想干什么"，调度器用这个答案和当前任务做比较，决定"怎么处理这条消息"。

### 6.2 调度器消费意图融合的方式

调度器不新增 IntentLabel，而是消费现有分类结果的两个维度：

1. **Domain 匹配**：`newMsg.Domain == currentTask.Domain` → 同域信号
2. **Label 匹配**：`newMsg.Label == currentTask.Label` → 强同域信号

这些信息从 `UnifiedIntentClassifier.Classify()` 的 `ClassificationResult` 中直接获取，不需要额外调用。

### 6.3 信号融合而非意图分类

调度决策是三个信号的函数，不是一个分类问题：

```
Decision = f(
    relevance,     // embedding cosine: 连续值 [0,1]
    intentMatch,   // domain/label 匹配: 布尔值
    structure,     // 长度 + 否定句法: 结构化特征
)
```

这三个信号独立计算、正交组合。决策矩阵是声明式的，新增调度策略只需修改矩阵，不需要改信号计算逻辑。

---

## 7. 完整架构图

```
                    桌面面板                              IM 通道
                       │                                    │
              预输入队列 + 预分类                     AsyncDispatcher
              (入队时异步 Schedule)                  (消息到达时 Schedule)
                       │                                    │
                  调度决策（缓存）                      调度决策（实时）
              (点击 [⏎] 读缓存, 0ms)              (L1 <1ms / L2 5ms / L3 2-8s)
                       │                                    │
              ┌────────┼────────┐              ┌────────────┼────────────┐
              ↓        ↓        ↓              ↓            ↓            ↓
           Replace  Merge  Insert           Replace      Merge       Insert
           Enqueue  Status                  Enqueue      Status
              │        │        │              │            │            │
              └────────┼────────┘              └────────────┼────────────┘
                       ↓                                    ↓
                InterruptAndProcess（统一中断层）
                       │
              ┌────────┼────────┐
              ↓        ↓        ↓
           Replace   Merge    Insert
              │        │        │
           放弃当前  注入当前   暂停+插入+恢复
           处理新的  loop      
                       │
                       ↓
                MilestoneBuffer（共享数据源）
                 ├→ 事件驱动进度推送（IM 通道）
                 ├→ 语义相关性计算（调度器）
                 ├→ 状态查询回复（StatusQuery）
                 └→ 取消时产出物摘要（Replace）
```

---

## 8. 实现计划

### Phase 1: MilestoneBuffer + 里程碑提取器

**目标**：建立共享数据源，替换现有的无信息量进度消息。

**文件变更**：
- `corelib/progress/milestone.go`：`Milestone`、`MilestoneBuffer` 数据结构
- `corelib/progress/extractor.go`：`ExtractMilestone()`、`toolSummaryRules`、`silentTools`
- `corelib/progress/milestone_test.go`：单元测试

**接入点**：
- `gui/im_tool_execution.go`：`executeTool()` 完成后调用 `ExtractMilestone` 写入 buffer
- `gui/im_message_handler.go`：agent loop 开始时 `Reset()` buffer

### Phase 2: 事件驱动进度推送

**目标**：替换现有的定时 nudge 机制。

**文件变更**：
- `corelib/progress/pusher.go`：`ProgressPusher` 实现（三层策略 + 合并窗口 + 动态升级）
- `hub/internal/im/router.go`：替换 `progressMinInterval` 逻辑，改为消费 `ProgressPusher` 的事件
- `gui/im_message_handler.go`：移除"任务较复杂，正在耐心处理中"等硬编码消息

### Phase 3: 消息调度器核心

**目标**：实现 `Schedule()` 函数和三信号决策矩阵。

**文件变更**：
- `corelib/progress/scheduler.go`：`MessageScheduler`、`ScheduleDecision`、决策矩阵
- `corelib/progress/structure.go`：`DetectNegation()`、`StructureSignal`
- `corelib/progress/scheduler_test.go`：决策矩阵覆盖测试

### Phase 4: InterruptAndProcess 统一中断层

**目标**：实现中断 + 部分产出保留 + 恢复点。

**文件变更**：
- `corelib/progress/interrupt.go`：`InterruptRequest`、`InterruptResult`、`SuspendedTask`
- `gui/im_message_handler.go`：`InterruptAndProcess()` 方法
- `gui/im_message_handler.go`：per-user cancel context 管理

### Phase 5: IM 通道异步化

**目标**：IM 消息接收与 agent loop 解耦。

**文件变更**：
- `hub/internal/im/async_dispatcher.go`：`AsyncDispatcher` 实现
- `hub/internal/im/router.go`：消息接收改为异步，调用 `AsyncDispatcher.OnMessage()`

### Phase 6: 桌面面板预输入队列改进

**目标**：逐条处理 + 引导发射 + 预分类。

**文件变更**：
- `gui/frontend/src/components/ai/PreInputQueue.tsx`：队列 UI 重构（状态机 + [⏎] 按钮 + 菜单）
- `gui/frontend/src/components/ai/usePreInputQueue.ts`：逐条处理逻辑 + 预分类缓存
- `gui/app.go`：新增 `SchedulePreInput()` Wails binding，前端调用做预分类

---

## 9. 边界情况

### 9.1 取消时正在执行 SSH 命令

不发 Ctrl+C 到远程服务器（可能中断危险操作如数据库迁移）。只取消 maclaw 侧的 agent loop。回复："已停止 maclaw 任务。注意：服务器上正在执行的命令可能仍在运行。"

### 9.2 取消时正在等用户确认（ask_user pending）

取消整个任务。清除 pendingAskUser 状态。如果用户只是想说"不确认"，他会点按钮或说"不"/"修改"。

### 9.3 Insert 模式恢复时上下文过期

SuspendedTask 超过 **10 分钟** 未恢复，视为过期，自动转为 abandoned。避免恢复一个已经不相关的任务。

### 9.4 连续取消

用户发"停"，系统回复"已停止"，用户又发"停"——第二次是空操作，回复"当前没有正在执行的任务。"

### 9.5 Merge 注入时机

`pendingInjection` 在 agent loop 每次迭代开始时检查并消费。如果当前迭代正在执行一个长时间工具调用（如 SSH 命令），注入要等到下一轮迭代才生效。这是可接受的——补充信息不需要立即中断当前操作。

### 9.6 IM 通道 L2 embedding 不可用

L2 embedding 模型未加载时，relevance 信号缺失。降级策略：
- 仅用 intent domain match + structure 信号做决策
- 高相关/低相关的判断退化为同域/异域的二分
- 否定结构仍然有效（不依赖 embedding）

---

## 10. 校准与测试

### 10.1 调度决策校准数据

从真实场景提取标注用例，覆盖决策矩阵的所有格子：

```go
// corelib/progress/scheduler_calibration_test.go

var schedulerCases = []struct {
    CurrentTask string
    NewMessage  string
    Expected    Action
    Note        string
}{
    // Replace（否定 + 低相关）
    {"开发贪吃蛇游戏", "算了不做了", ActionReplace, "明确取消"},
    {"开发贪吃蛇游戏", "先不弄了吧", ActionReplace, "犹豫式取消"},
    {"开发贪吃蛇游戏", "我改主意了", ActionReplace, "间接取消"},
    
    // Merge（高相关 + 同域）
    {"开发贪吃蛇游戏", "颜色改成红色", ActionMerge, "补充视觉需求"},
    {"开发贪吃蛇游戏", "用C++不要Python", ActionMerge, "修改技术选型"},
    {"开发贪吃蛇游戏", "加个排行榜", ActionMerge, "追加功能需求"},
    
    // Insert（低相关 + 异域 + 非否定）
    {"开发贪吃蛇游戏", "帮我查下杭州天气", ActionInsert, "无关新任务"},
    {"开发贪吃蛇游戏", "翻译一下这段英文", ActionInsert, "异域任务"},
    
    // 易混淆
    {"开发贪吃蛇游戏", "取消服务器上的定时任务", ActionInsert, "含'取消'但是新任务"},
    {"开发贪吃蛇游戏", "帮我把那个订单取消了", ActionInsert, "含'取消'但是新任务"},
    {"查天气", "用红色", ActionInsert, "低相关的短消息"},
}
```

### 10.2 进度推送测试

验证三层策略的行为：

```go
// corelib/progress/pusher_test.go

// 轻量任务（<30s）：零 nudge
// 中等任务（30s-3min）：即时确认 + 里程碑推送
// 重度任务（>3min）：即时确认 + 里程碑推送 + 静默兜底
// 动态升级：轻量任务 30s 未完成 → 补发确认 + 开始里程碑
// 合并窗口：30s 内多个里程碑合并为一条
// 心跳上限：3 次心跳后彻底静默
```

---

## 11. 预期收益

| 指标 | 现在 | 改进后 |
|------|------|--------|
| 查天气进度消息数 | 1 条（无意义） | 0 条 |
| 编码任务进度消息数 | ~30 条（刷屏） | 3-5 条（有信息量） |
| 用户取消响应延迟 | 等 agent loop 结束（分钟级） | <1s（L1 快速路径）/ <5ms（L2 路径） |
| 补充信息生效延迟 | 等 agent loop 结束 | 下一轮迭代（秒级） |
| 预输入队列控制力 | 无（一次性处理） | 逐条 + 合并/插入/替换 |
| 进度消息信息量 | "正在处理中" | "已搜索到 3 个来源，正在整理..." |

## 12. 风险与缓解

1. **IM 异步化改动范围大**：IM router 从同步改异步是架构级变更。缓解：Phase 5 可以先做"取消快速路径"（仅 L1 keyword + 否定句法检测，在现有同步架构上加 goroutine 监听），完整异步化作为后续迭代
2. **Insert 模式恢复复杂**：保存和恢复 agent loop 状态涉及对话历史、pending 状态、工具上下文。缓解：Phase 4 先实现 Replace（简单丢弃），Insert 恢复作为后续迭代
3. **embedding 相关性阈值需要校准**：0.6/0.3 的高/低相关阈值是初始值。缓解：用 10.1 的标注数据做 grid search 校准
4. **否定句法检测的覆盖率**：中文否定表达虽然有固定句法结构，但口语化表达可能不完全符合。缓解：校准数据中包含口语化用例，持续补充