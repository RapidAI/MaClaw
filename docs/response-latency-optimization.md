# 响应延迟优化——关键路径瘦身

## 问题

用户发送简单消息（如"驱网服务器信息"）到看到第一个 token，耗时 60-90 秒。
根因不是模型慢——主 LLM 调用本身只需 14 秒。是关键路径上堆了太多串行的辅助 LLM 调用和分类逻辑。

## 日志实证（"驱网服务器信息" 17:32:12 → 17:33:36，共 84 秒）

```
17:32:12  pending-reply-answer LLM (deepseek-reasoner)     2.7s   ← 阻塞
17:32:12  Skill scanner（全量扫描）                          ~0.1s
17:32:12  后台 LLM ×2（memory/online_extraction）           ~9s（异步但占 API 并发）
17:32:43  session-start-extractor (deepseek-reasoner)       35.7s（异步但占 API 并发）
17:32:43  proactive_recall + knowledge_auto_recall          ~0.1s
17:32:43  Skill scanner（第二次全量扫描）                    ~0.1s
17:32:58  UIC fusion (tree channel, deepseek-reasoner)      15s（超时+JSON解析失败）← 阻塞
17:33:01  GateIntentClassifier LLM (deepseek-reasoner)      3s（超时）← 阻塞
17:33:10  第二次 UIC fusion                                 8.9s  ← 阻塞
17:33:10  ═══ 主 LLM 调用开始 ═══
17:33:24  主 LLM iteration 1 (ssh tool call)                ~14s
17:33:29  主 LLM iteration 2 (最终响应)                     ~5s
17:33:29  ═══ 响应返回给用户 ═══
17:33:29  compaction LLM                                    6.5s  ← post-response 但阻塞返回
17:33:36  pending-reply-prompt LLM                          2.6s  ← post-response 但阻塞返回
```

**关键路径上的阻塞开销：~30 秒**（pending-reply 2.7s + UIC tree 15s + Gate 3s + 第二次 UIC 8.9s）
**post-response 阻塞开销：~9 秒**（compaction 6.5s + pending-reply-prompt 2.6s）

## 优化方案（5 个独立改动，按收益排序）

### 改动 1 (P0): UIC tree channel 超时从 15s 降到 5s + 降级容忍

**收益**：关键路径减少 10-12 秒

**根因**：`buildUICLLMFunc` 的 timeout 是 15s，但 deepseek-reasoner 的 thinking phase 经常超过 15s，导致每次都等到超时才放弃。tree channel 失败后 UIC 降级到 embedding-only 模式（已有机制），结果完全可用。

**修复**：
- `gui/app_embedding.go`：`buildUICLLMFunc` 的 `doSimpleLLMRequest` timeout 从 15s 降到 5s
- `corelib/intent/classifier.go`：`classifyWithFusion` 中 tree channel 使用 `select` + 5s deadline，超时直接用 embedding-only 结果，不等 tree
- 对 deepseek-reasoner 这类推理模型，tree channel 基本不可能在 5s 内返回有效 JSON（thinking phase 就要 3-8s），所以实际效果是：**推理模型下 tree channel 自动禁用，零延迟降级到 embedding-only**

**文件**：
- `gui/app_embedding.go`：timeout 15s → 5s
- `corelib/intent/classifier.go`：`classifyWithFusion` 新增 tree channel select deadline

---

### 改动 2 (P0): GateIntentClassifier 不 escalate 到 LLM（已有 UIC 结果时）

**收益**：关键路径减少 3 秒

**根因**：`GateIntentClassifier.classifyGateIntentWithLLM` 在 UIC 返回 `degraded=true` 或 `conf < threshold` 时 escalate 到独立的 LLM 调用（3s timeout）。但 UIC 的 embedding channel 已经给出了 `ssh(0.698)` 的结果——这对 gate 来说已经足够判断"不是编码任务"。

**修复**：
- `gui/gate_intent_classifier.go`：当 UIC 已返回结果（即使 degraded）且 intent 不是 `coding`/`unknown` 时，直接使用 UIC 结果，不 escalate 到 LLM
- 只有当 UIC 完全不可用（nil）或 intent 是 `coding`/`unknown`（需要精确判断是否走三阶段）时才 escalate

**文件**：
- `gui/gate_intent_classifier.go`：`classify` 方法的 escalation 条件收紧

---

### 改动 3 (P1): pending-reply-answer 改为先用启发式判断，LLM 作为 fallback

**收益**：关键路径减少 2-3 秒（大多数情况）

**根因**：`classifyPendingUserReplyAnswer` 每次都调用 LLM（2.7s）判断"用户消息是否是对上一个问题的回答"。但大多数情况下，启发式规则就能判断：
- 用户消息 < 20 字 + 上一条 assistant 以 `?` 或 `？` 结尾 → 大概率是回答
- 用户消息包含明确的新任务关键词（"帮我"、"开发"、"查询"等）→ 大概率不是回答
- 只有模糊情况才需要 LLM

**修复**：
- `gui/im_pending_reply.go`：`classifyPendingUserReplyAnswer` 新增启发式前置判断
  - 短回复（<20 字）+ 上文是问题 → 直接返回 `(true, true)` 不调 LLM
  - 包含新任务关键词 → 直接返回 `(false, true)` 不调 LLM
  - 模糊情况 → 调 LLM（保持现有行为）

**文件**：
- `gui/im_pending_reply.go`

---

### 改动 4 (P1): compaction 和 pending-reply-prompt 完全异步化（不阻塞响应返回）

**收益**：响应返回加速 ~9 秒

**根因**：`saveConversationHistoryTimed` 在 agent loop 返回后同步执行，包含 LLM summarizer 调用（6.5s）和 `updatePendingUserReplyFromHistory`（调用 `classifyPendingUserReplyPrompt` 2.6s）。这些操作在响应已经生成后执行，但仍然阻塞 `SendAIAssistantMessage` 的返回。

**修复**：
- `gui/im_history_persistence.go`：`saveConversationHistoryTimed` 中的 LLM summarizer 调用改为 goroutine
  - 先同步执行 `trimHistoryWithSummary(history, nil, memorySink, ...)` （无 summarizer，纯截断，<1ms）
  - 同步 `h.memory.Save(userID, trimmed)` 保存截断后的历史
  - 异步 goroutine 中执行 LLM summarizer + 更新 separator
- `gui/im_history_persistence.go`：`updatePendingUserReplyFromHistory` 改为 goroutine
  - 不阻塞响应返回
  - 结果存入 `pendingUserReply` sync.Map（下次消息时消费）

**文件**：
- `gui/im_history_persistence.go`

---

### 改动 5 (P2): UIC + GateIntentClassifier + proactive_recall 并行执行

**收益**：关键路径减少 ~5 秒（当 UIC 和 Gate 都需要时）

**根因**：当前流程是串行的：
1. `resolveIMEntryContext` → `routeWorkflowIMMessage` → `handleWorkflowInterception` → UIC Classify（~15s）
2. `prepareAgentLoopStartState` → `prepareAgentLoopCodingGate` → GateIntentClassifier（~3s）
3. `buildIMEntrySystemPrompt` → `appendProactiveRecall`（~5-20ms）

UIC 和 GateIntentClassifier 都是独立的分类任务，可以并行。proactive_recall 只依赖用户消息文本，也可以提前启动。

**修复**：
- 在 `resolveIMEntryContext` 之前，启动 UIC Classify 的 goroutine
- 在 `executePreparedIMEntry` 中，proactive_recall 结果从预计算的 channel 读取
- GateIntentClassifier 复用 UIC 的结果（改动 2 已覆盖大部分场景）

**文件**：
- `gui/im_entry_context.go`
- `gui/im_entry_execution.go`
- `gui/im_system_prompt.go`

---

## 预期效果

| 场景 | 修复前 | 修复后 | 改善 |
|------|--------|--------|------|
| 简单操作（"驱网服务器信息"）| ~84s | ~25-30s | -65% |
| 其中关键路径（到第一个 token）| ~50s | ~20-22s | -56% |
| 其中 post-response 阻塞 | ~9s | ~0s | -100% |

**注意**：主 LLM 调用本身（deepseek-reasoner thinking + 生成）的 14-19s 无法优化——那是模型本身的延迟。优化的是模型调用之前和之后的辅助开销。

## 实施顺序（已全部完成）

| # | 改动 | 省时 | 机制 |
|---|------|------|------|
| 1 | UIC tree deadline 3s | ~12s | 推理模型下 tree 自动降级 |
| 2 | GateIntentClassifier 接受 degraded | ~3s | 不 escalate 到 LLM |
| 3 | pending-reply-answer 启发式 | ~2.7s | 短回复不调 LLM |
| 4 | pending-reply-prompt 异步 | ~2.6s | 不阻塞返回 |
| 5 | 历史分类用 embedding-only | ~9s | 不触发 tree LLM |
| 6 | session-start-extractor 延迟 30s | ~2-5s | 不抢 API 带宽 |
| 7 | shouldBypassWorkflowForIntent 快速路径 | ~3s | 非编码意图 embedding-only 直接 bypass |
| 8 | compaction LLM 异步化 | ~6.5s | 两阶段：sync 快速截断 + async LLM 摘要 |
| **总计** | | **~41-46s** | |

## 关于流式响应

deepseek-reasoner 确实走了流式路径（日志 `[LLM Stream] POST`），但用户看不到流式输出的原因是：
- thinking phase 期间只产出 `reasoning_content`（不显示给用户）
- 只有 thinking 结束后的 `content` 才流式显示
- 这是 deepseek-reasoner 模型的固有特性，无法通过代码优化

如果换成 `deepseek-chat`，流式响应会立即可见（无 thinking phase）。但这不是本次优化的重点——本次优化的是"主 LLM 调用之前的 30+ 秒辅助开销"。

## 修改的文件

| 文件 | 改动 |
|------|------|
| `gui/app_embedding.go` | UIC LLM timeout 15s→5s |
| `corelib/intent/classifier.go` | tree channel 3s deadline + `ClassifyEmbeddingOnly()` 方法 |
| `gui/gate_intent_classifier.go` | `shouldAcceptGateResult` 接受 degraded 非编码结果 |
| `gui/gate_intent_classifier_test.go` | 更新测试用例 |
| `gui/im_history_persistence.go` | pending-reply-answer 启发式 + pending-reply-prompt 异步 + session-start-extractor 延迟 + compaction 两阶段异步 |
| `gui/im_message_handler_workflow.go` | `shouldBypassWorkflowForIntent` embedding-only 快速路径 + `recentContextResolvesToNonWorkflow` embedding-only |
| `gui/im_tools_session_guard.go` | `conversationHasCodingContextUIC` embedding-only |
