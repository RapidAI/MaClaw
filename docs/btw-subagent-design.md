# /btw 侧查询 SubAgent 设计文档

## 概述

实现 `/btw <message>` 命令——用户在 AI 助手面板中输入 `/btw 最新的 AI 安全进展` 时，系统启动一个独立的轻量 agent loop（BtwSubAgent），在不污染主对话历史的前提下完成信息查询，将结果输出给用户。

类似 Claude Code 的 `/btw` 功能：side query，不打断主任务上下文。

## 机制性设计

### 核心原则

1. **独立 context**：`/btw` 在独立的 agent loop 中运行，不消耗主对话的 token 预算
2. **不污染主历史**：中间的工具调用（web_search、read_file 等）不进入主对话历史，只有最终结果作为单条 assistant 消息追加
3. **精简工具集**：只提供信息查询相关工具（web_search、web_fetch、read_file、memory recall），不提供编码/SSH/浏览器等工具
4. **流式输出**：复用主 agent loop 的 `onToken` 回调，实时显示查询进度
5. **可取消**：用户可以取消 `/btw` 查询，不影响主对话

### 机制性约束（非 prompt 级别）

| 约束 | 实施层 | 说明 |
|------|--------|------|
| 工具白名单 | `ExecuteTool` | 只有 `btwToolNames` 中的 4 个工具可执行，其他返回错误 |
| memory 只读 | `ExecuteTool` | `action != "recall"` 时返回错误，不依赖 prompt 指令 |
| 历史隔离 | `agent.RunLoop` | 传入 `nil` history，SubAgent 无法看到主对话 |
| 原子历史追加 | `memory.Append` | 使用原子 Append 而非 Load→append→Save，避免与并发主 loop 竞态 |
| 取消传播 | `atomic.Int32` | `Cancel()` 设置原子标记，`ShouldStop()` 读取，不依赖 LoopContext |

### 架构

```
用户: "/btw 最新的 AI 安全进展"
  ↓
[handleIMMessageWithLoop — slash command 拦截（chatLoopMu 之前）]
  ↓ 提取 "/btw " 后的 message
  ↓
[BtwSubAgent.Execute(message)]
  ├─ System Prompt: ~1K token（精简的查询助手角色）
  ├─ Tools: web_search, web_fetch, read_file, memory (4 个, ~500 token)
  ├─ Max iterations: 30（MinAgentIterations 下限）
  ├─ 独立 conversation history（nil）
  └─ 复用主 handler 的工具实现（零重复代码）
  ↓
[RunLoop — corelib/agent.RunLoop]
  ├─ LLM 生成搜索策略
  ├─ 执行 web_search / web_fetch
  ├─ 整理结果
  └─ 返回 LoopResult
  ↓
[结果处理]
  ├─ memory.Append 原子追加到主对话历史（单条消息）
  ├─ 前缀 "🔍 /btw 查询结果" 标识
  └─ 返回 IMAgentResponse
```

### 并发安全分析

GUI 的 `/btw` 在 `chatLoopMu.Lock()` **之前**拦截——这是设计意图，side query 不应阻塞在主 loop 的互斥锁上。但这意味着 `/btw` 可以与主 agent loop 并发运行。

**历史写入竞态**：主 loop 使用 `Load → modify → Save`（全量替换），`/btw` 如果也用这个模式，两个 Save 会互相覆盖。

**修复**：新增 `ConversationMemory.Append()` 方法——在单次锁获取内完成 read-modify-write，与 `Save()` 的锁是同一个 shard mutex，保证原子性。

TUI 无此问题——Bubble Tea 事件循环是单线程的，`/btw` 的 goroutine 完成后才处理下一条消息。

### 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `gui/btw_subagent.go` | 新增 | BtwSubAgent + btwCallbacks + system prompt + tool definitions |
| `gui/im_message_handler.go` | 修改 | `/btw` 命令拦截 + `handleBtwCommand` + `/help` 更新 + `activeBtwSubAgent` 字段 + `/cancel` 联动 |
| `gui/app_wails_bindings.go` | 修改 | 新增 `SendBtwQuery` Wails binding（独立于 `SendAIAssistantMessage`） |
| `gui/frontend/src/components/ai/useAIAssistant.ts` | 修改 | 新增 `sendBtwMessage`（独立发送通道，不经过 activeRound guard） |
| `gui/frontend/src/components/ai/AIAssistantPanel.tsx` | 修改 | `handleSend` 检测 `/btw` 前缀，绕过 buffer queue 直接调用 `sendBtwMessage` |
| `gui/frontend/src/App.tsx` | 修改 | 传递 `sendBtwMessage` prop |
| `gui/frontend/wailsjs/go/main/App.js` | 修改 | 新增 `SendBtwQuery` JS binding |
| `gui/frontend/wailsjs/go/main/App.d.ts` | 修改 | 新增 `SendBtwQuery` TS 声明 |
| `tui/app.go` | 修改 | `/btw` 路由 + `tuiBtwCallbacks` + `/help` 更新 + `cancellable` 接口 |
| `corelib/agent/conversation_memory.go` | 修改 | 新增 `Append()` 原子追加方法 |
| `docs/btw-subagent-design.md` | 新增 | 本设计文档 |

### Review/Fix/Optimize 记录

#### 发现的机制性问题及修复

| # | 问题 | 根因 | 修复 |
|---|------|------|------|
| 1 | `EffectiveMaxIterations(15)` → 30 | `MinAgentIterations=30` 下限，15 被静默覆盖 | 改为 `EffectiveMaxIterations(MinAgentIterations)`，显式声明意图 |
| 2 | GUI 历史写入竞态 | `/btw` 在 `chatLoopMu` 之前运行，`Load→append→Save` 与主 loop 的 `Save` 竞态 | 新增 `ConversationMemory.Append()` 原子方法 |
| 3 | memory 工具未机制性限制 | prompt 说"只用 recall"，但 LLM 可调用 save/delete | `ExecuteTool` 中硬拒绝 `action != "recall"` |
| 4 | 取消机制断裂 | `LoopContext` 由 `handleBtwCommand` 创建但 `/cancel` 取消的是 `currentLoopCtx` | 改用 `atomic.Int32` 自包含取消 + `activeBtwSubAgent` 字段让 `/cancel` 可达 |
| 5 | TUI `/btw` 前缀匹配过宽 | `HasPrefix("/btw")` 匹配 `/btwxyz` | 改为 `== "/btw" \|\| HasPrefix("/btw ")` |
| 6 | 无用间接层 | `buildBtwToolDefinitions(_ *IMMessageHandler)` 忽略参数 | 删除参数，直接返回内联定义 |
| 7 | 重复辅助函数 | `truncateRunesForBtw` 与 `truncateRunesForSubAgent` 相同 | 复用 `truncateRunesForSubAgent` |
| 8 | `/btw` 进入预输入队列 | 前端 `handleSend` 在 `submitLocked=true` 时将所有消息入队，`/btw` 失去侧查询意义 | 新增 `SendBtwQuery` 独立 Wails binding + `sendBtwMessage` 独立发送通道，绕过 buffer queue 和 activeRound guard |
| 9 | `sendMessage` idle-phase guard 阻止并发 | `activeRoundRef.current.phase !== 'idle'` 时 `sendMessage` 直接 return | `/btw` 走独立的 `sendBtwMessage`，不经过 `sendMessage`，不影响 `activeRound` 状态 |
| 10 | `Append` 与 `Save` 竞态——Append 被 Save 覆盖 | 主 loop 的 `Load→modify→Save` 全量替换 history，`Append` 在 Load 和 Save 之间的写入被覆盖 | 不在 `handleBtwCommand` 中追加历史。`/btw` 结果只在前端 UI 显示，不进入后端 history。避免了竞态，也符合"侧查询不污染主上下文"的设计意图 |
| 11 | `sendBtwMessage("")` 静默返回 | 用户输入 `/btw` 无参数时，input 被清空但无任何反馈 | `sendBtwMessage` 空查询时显示用法帮助消息 |

## Context 效率

| 指标 | 主 Agent | BtwSubAgent |
|------|---------|-------------|
| System prompt | ~12,000 token | ~1,000 token |
| 工具定义 | ~15,000 token (40+) | ~500 token (4) |
| 初始开销 | ~40,000 token | ~1,500 token |
| 可用查询空间 | ~62,000 token | ~100,000 token |

## 验收标准

- `/btw 最新的 Go 1.23 有什么新特性` → 搜索网页 → 返回结构化结果
- `/btw` （无参数）→ 显示用法提示
- `/btwxyz` → 不匹配 /btw（前缀匹配修复）
- `/btw` 查询期间用户可以 `/cancel` 取消
- `/btw` 的中间工具调用不出现在主对话历史中
- `/btw` 的结果只在前端 UI 显示，不追加到后端 conversation history（避免与并发主 loop 竞态）
- `memory(action="save")` 在 /btw 中被机制性拒绝（不依赖 prompt）
- 流式输出正常工作（通过独立的 `ai-btw-token` 事件通道）
- 主 agent loop 运行期间输入 `/btw` 立即执行，不进入预输入队列
- 主对话上下文不受 `/btw` 影响
- GUI / TUI / corelib 编译通过
- 所有现有测试通过（2 个预先存在的 Property 6/7 失败不受影响）
