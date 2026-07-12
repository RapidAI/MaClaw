# 借鉴 Codex CLI 的记忆管理改进方案

## 调查来源

- Codex CLI 开源仓库: https://github.com/openai/codex (`codex-rs/core/src/`)
- OpenAI 官方博客: "Unrolling the Codex agent loop" (2026-04-22)
- 第三方分析: kangwooklee.com 的 compact API 逆向分析
- 第三方对比: justin3go.com 的三大 CLI Agent 压缩策略对比

## 一、Codex 记忆架构全景

### 1.1 三层记忆管理

```
┌─────────────────────────────────────────────────────────────┐
│              Context Compaction (对话内压缩)                   │
│  compact.rs + compact_remote.rs                              │
│  触发: context 接近模型窗口上限时自动触发 / 用户手动 /compact    │
│  策略: Handoff Summary (交接摘要) 替换全部历史                  │
│  特点: 保留所有用户消息原文 (上限 20K token)                    │
│  恢复: Summary Prefix prompt 告知 LLM "另一个模型已做了这些"    │
└──────────────────────┬──────────────────────────────────────┘
                       │ rollout 文件持久化
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              Memories (跨会话记忆提取)                         │
│  memories/phase1.rs + memories/phase2.rs                     │
│  触发: 新会话启动时异步处理旧 rollout                          │
│  Phase 1: LLM 提取 raw_memory + rollout_summary + slug       │
│  Phase 2: 语义整合 + 索引构建                                 │
│  特点: 并行处理 + job claim/lease 防并发 + secret 脱敏         │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              Thread Rollout Truncation (线程截断)              │
│  thread_rollout_truncation.rs                                │
│  策略: 基于 user turn boundary 的精确截断                     │
│  支持: ThreadRolledBack 回滚标记                              │
│  支持: fork-turn boundary (子 agent 触发点)                   │
└─────────────────────────────────────────────────────────────┘
```

### 1.2 Compaction 核心流程 (compact.rs, 578 行)

```
触发 auto-compact
  │
  ├─ 1. 构建压缩 prompt (templates/compact/prompt.md)
  │     "You are performing a CONTEXT CHECKPOINT COMPACTION..."
  │
  ├─ 2. 发送给 LLM, 流式接收摘要
  │     drain_to_completed() 处理 SSE 流
  │
  ├─ 3. 提取最后一条 assistant 消息作为 summary 后缀
  │     get_last_assistant_message_from_turn()
  │
  ├─ 4. 收集所有用户消息原文
  │     collect_user_messages() — 过滤掉 summary 消息本身
  │
  ├─ 5. 构建压缩后的历史
  │     build_compacted_history():
  │       - 用户消息从最近的开始倒序保留, 上限 20K token
  │       - 超长消息用 truncate_text 截断
  │       - 末尾追加 summary_prefix + LLM 生成的摘要
  │
  ├─ 6. 注入 initial context (系统指令)
  │     insert_initial_context_before_last_real_user_or_summary()
  │     放在最后一条真实用户消息之前
  │
  ├─ 7. 保留 ghost snapshots (用于 /undo)
  │
  ├─ 8. 替换历史 + 重算 token
  │     sess.replace_compacted_history()
  │     sess.recompute_token_usage()
  │
  └─ 9. 发出警告
        "Long threads and multiple compactions can cause the model
         to be less accurate. Start a new thread when possible."
```

### 1.3 Compaction 的容错机制

```rust
// compact.rs — ContextWindowExceeded 回退
Err(CodexErr::ContextWindowExceeded) => {
    if turn_input_len > 1 {
        // 从头部移除最旧的历史条目, 保留尾部 (prefix cache 友好)
        history.remove_first_item();
        truncated_count += 1;
        retries = 0;  // 重置重试计数
        continue;     // 重试压缩
    }
    // 只剩 1 条也超限, 报错
}
```

关键设计: 压缩本身也可能超限 (历史太长, 压缩 prompt + 历史 > context window)。
Codex 的解决方案是渐进式头部截断 + 重试, 而不是直接报错。

### 1.4 Memories Phase 1 (phase1.rs, 620 行)

```
新会话启动
  │
  ├─ 1. claim_startup_jobs()
  │     从 state_db 中认领未处理的旧 rollout
  │     参数: scan_limit, max_claimed, max_age_days,
  │           min_rollout_idle_hours, lease_seconds
  │
  ├─ 2. build_request_context()
  │     构建 LLM 请求上下文 (model, reasoning_effort 等)
  │
  ├─ 3. run_jobs() — 并行处理
  │     futures::stream::iter().buffer_unordered(CONCURRENCY_LIMIT)
  │     每个 job:
  │       a. 加载 rollout 文件
  │       b. 过滤: 移除 developer 消息, memory-excluded 片段
  │       c. 序列化为 JSON
  │       d. redact_secrets() 脱敏
  │       e. 发送给 LLM, 要求输出结构化 JSON:
  │          { raw_memory: string,      // 详细 Markdown 记忆
  │            rollout_summary: string,  // 紧凑摘要行
  │            rollout_slug: string }    // 文件名 slug
  │       f. 结果再次 redact_secrets()
  │       g. 写入 state_db
  │
  └─ 4. emit_metrics()
        记录 claimed/succeeded/failed/token_usage
```

### 1.5 Thread Rollout Truncation (170 行)

核心概念: **user turn boundary** — 每条用户消息标记一个 turn 边界。

```rust
// 扫描 rollout 找到所有用户消息位置
fn user_message_positions_in_rollout(items) -> Vec<usize> {
    // 遍历 items, 找 ResponseItem::Message 且 parse 为 UserMessage 的
    // 遇到 ThreadRolledBack 标记时, 回退 user_positions
}

// 从头部截断: 保留第 N 个用户消息之后的内容
fn truncate_rollout_before_nth_user_message_from_start(items, n) -> Vec

// 从尾部保留: 只保留最后 N 个 fork turn
fn truncate_rollout_to_last_n_fork_turns(items, n) -> Vec
```

fork-turn boundary = 用户消息 + 子 agent 的 trigger_turn 消息。
这比 MacLaw 的 turn boundary 更精细——它区分了"用户发起的 turn"和"子 agent 发起的 turn"。


## 二、MacLaw vs Codex 差异分析 (只列有收益的差异)

### 差异 1 (P0): 压缩后无 Recovery Prompt — LLM 不知道历史被压缩过

**Codex 做法**:

压缩后的历史末尾注入 `summary_prefix.md`:
```
Another language model started to solve this problem and produced a summary
of its thinking process. You also have access to the state of the tools that
were used by that language model. Use this to build on the work that has
already been done and avoid duplicating work. Here is the summary produced
by the other language model, use the information in this summary to assist
with your own analysis:
```

这告诉 LLM 三件事:
1. 有人已经做了一部分工作
2. 工具状态 (文件系统) 反映了已完成的工作
3. 不要重复已做过的事

**MacLaw 现状**:

`trimHistoryWithSummary` (#66) 生成的 separator 格式:
```
[对话摘要] {LLM 生成的摘要}
```

LLM 看到这个时, 不知道:
- 这是压缩后的恢复上下文还是普通的系统消息
- 文件系统中已经有了之前工作的产出
- 应该避免重复已完成的工作

**实际影响**: LLM 可能重新执行已完成的步骤 (重新读取已读过的文件、重新生成已生成的代码)。

**收益**: 避免压缩后的重复工作, 提升长会话的连贯性。

---

### 差异 2 (P0): 压缩 Prompt 不够结构化 — 摘要质量不稳定

**Codex 做法**:

压缩 prompt (`templates/compact/prompt.md`) 明确要求 4 个 section:
```
You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff
summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly
continue the work.
```

关键词: "CONTEXT CHECKPOINT COMPACTION" + "handoff summary" + "another LLM that will resume"。
这不是在写会议纪要, 是在写**交接文档**。

**MacLaw 现状**:

`makeSummarizer` 的 prompt (如果有的话) 是通用的"请总结以下对话"。
没有明确的结构化要求, 没有"交接"的语义框架。

**收益**: 结构化的压缩 prompt 产出更稳定、更有用的摘要。

---

### 差异 3 (P0): 压缩时不保留用户消息原文 — 用户意图在压缩后丢失

**Codex 做法**:

`build_compacted_history` 的核心逻辑:
```rust
// 1. 收集所有用户消息 (过滤掉 summary 消息本身)
let user_messages = collect_user_messages(history_items);

// 2. 从最近的开始倒序保留, 上限 20K token
let mut selected_messages: Vec<String> = Vec::new();
let mut remaining = max_tokens; // 20,000
for message in user_messages.iter().rev() {
    if remaining == 0 { break; }
    let tokens = approx_token_count(message);
    if tokens <= remaining {
        selected_messages.push(message.clone());
        remaining -= tokens;
    } else {
        // 超长消息截断后保留
        let truncated = truncate_text(message, TruncationPolicy::Tokens(remaining));
        selected_messages.push(truncated);
        break;
    }
}

// 3. 压缩后的历史 = 用户消息原文 + summary
// 用户消息作为独立的 user role 消息保留
// summary 作为最后一条 user role 消息追加
```

**MacLaw 现状**:

`trimHistoryWithSummary` (#66 + #56) 的策略:
- Tier-1: 保留 10 个 turn boundary (用户请求首条 + LLM 首条响应)
- Tier-2: 保留最近 30 条
- 中间: LLM 摘要或静态占位符

问题:
- turn boundary 只保留每个 turn 的**首条**用户消息
- 如果用户在一个 turn 中发了补充需求 ("加上音效"、"用 C++ cmake")，这些补充消息不是 turn boundary, 会被截断
- 用户的约束条件 ("保持向后兼容") 如果不在 turn 首条, 也会丢失

**收益**: 保留用户消息原文确保用户意图不丢失, 这是压缩后 LLM 能否正确继续工作的关键。

---

### 差异 4 (P1): 压缩超限时无渐进式回退 — 直接报错

**Codex 做法**:

```rust
// compact.rs — 压缩本身超限时的处理
Err(CodexErr::ContextWindowExceeded) => {
    if turn_input_len > 1 {
        // 从头部移除最旧的历史条目
        history.remove_first_item();
        truncated_count += 1;
        retries = 0;  // 重置重试计数, 因为输入变了
        continue;     // 重试
    }
    // 只剩 1 条也超限, 才报错
}
```

同时有指数退避重试 (非 ContextWindowExceeded 的其他错误):
```rust
Err(e) => {
    if retries < max_retries {
        retries += 1;
        let delay = backoff(retries);
        tokio::time::sleep(delay).await;
        continue;
    }
}
```

**MacLaw 现状**:

#74 加了 token 校准 (用 API 返回的实际 token 数校准估算值), 但:
- 校准只在 `trimConversation` 阶段生效
- 如果校准后仍然超限 (极端情况), LLM 调用直接返回错误
- 没有"压缩本身超限"的处理 (因为 MacLaw 的压缩是在 trimHistory 中做的, 不是独立的 LLM 调用)

**收益**: 渐进式回退确保压缩永远不会因为"历史太长连压缩都放不下"而失败。

---

### 差异 5 (P1): 新会话启动时不提取旧对话记忆 — 记忆提取延迟

**Codex 做法**:

`memories/phase1.rs` 在新会话启动时:
1. 扫描 state_db 中未处理的旧 rollout
2. 用 job claim/lease 机制认领 (防止多个会话并发处理同一个 rollout)
3. 并行调用 LLM 提取记忆 (`buffer_unordered(CONCURRENCY_LIMIT)`)
4. 输出结构化 JSON: `{ raw_memory, rollout_summary, rollout_slug }`
5. 写入 state_db, 供后续会话召回

关键参数:
- `max_rollouts_per_startup`: 每次启动最多处理的 rollout 数
- `max_rollout_age_days`: rollout 最大年龄
- `min_rollout_idle_hours`: rollout 最小空闲时间 (避免处理正在进行的会话)
- `JOB_LEASE_SECONDS`: job 租约时间 (防止死锁)
- `JOB_RETRY_DELAY_SECONDS`: 失败后重试延迟

**MacLaw 现状**:

- `KnowledgeExtractor`: 1h 冷却期, 且只在 `ConversationArchiver.Archive()` 内部调用
- `Archive()`: 只在会话 TTL 过期时触发
- 用户连续工作数小时不"过期"时, 中间的对话内容不会被提取为长期记忆
- Phase 1 (#62) 的 `SavePhaseOutput` 沉淀只覆盖工作流阶段产出物, 不覆盖普通对话

**收益**: 新会话启动时提取旧对话记忆, 确保跨会话的知识不丢失。

---

### 差异 6 (P1): 记忆提取无 Secret 脱敏 — 敏感信息可能被持久化

**Codex 做法**:

`phase1.rs` 中两处脱敏:
```rust
// 1. 输入脱敏: rollout 内容序列化后脱敏
let serialized = serde_json::to_string(&filtered)?;
Ok(redact_secrets(serialized))

// 2. 输出脱敏: LLM 提取的记忆脱敏
output.raw_memory = redact_secrets(output.raw_memory);
output.rollout_summary = redact_secrets(output.rollout_summary);
output.rollout_slug = output.rollout_slug.map(redact_secrets);
```

**MacLaw 现状**:

- `memory.Store.Save()`: 无脱敏步骤
- `KnowledgeExtractor.Extract()`: 无脱敏步骤
- `ConversationArchiver.Archive()`: 无脱敏步骤
- 如果用户在对话中处理了 SSH 密码、API key、数据库连接字符串, 这些可能被原样保存到 `memories.json`

**收益**: 防止敏感信息泄漏到持久化记忆中, 这是安全问题。

---

### 差异 7 (P1): 压缩后不重算 Token 使用量 — Token 统计不准确

**Codex 做法**:

```rust
// compact.rs — 压缩完成后
sess.replace_compacted_history(new_history, reference_context_item, compacted_item).await;
client_session.reset_websocket_session();  // 重置 WebSocket 会话
sess.recompute_token_usage(&turn_context).await;  // 重算 token 使用量
```

压缩后立即重算 token, 确保状态栏显示的 "context left" 百分比准确。

**MacLaw 现状**:

`trimHistory` 截断后, `estimateConversationTokens` 会在下一次 LLM 调用前重新估算。
但 #74 发现这个估算可能低估 30%, 导致 `trimConversation` 不触发。
压缩后没有强制重算的步骤。

**收益**: 压缩后立即重算确保后续的 token 预算决策基于准确数据。

---

### 差异 8 (P1): 无压缩质量警告 — 用户不知道多次压缩会降低质量

**Codex 做法**:

每次压缩后发出警告:
```rust
let warning = EventMsg::Warning(WarningEvent {
    message: "Heads up: Long threads and multiple compactions can cause
    the model to be less accurate. Start a new thread when possible to
    keep threads small and targeted.".to_string(),
});
sess.send_event(&turn_context, warning).await;
```

**MacLaw 现状**: 压缩 (trimHistory) 是静默的, 用户完全不知道发生了压缩, 也不知道多次压缩会降低质量。

**收益**: 让用户知道何时应该开始新会话, 避免在质量已经下降的会话中继续工作。

---

### 差异 9 (P2): 压缩后不保留 Initial Context 注入点 — 系统指令位置不稳定

**Codex 做法**:

`insert_initial_context_before_last_real_user_or_summary` 有精确的注入规则:
```
优先: 放在最后一条真实用户消息之前
回退 1: 放在最后一条 summary 消息之前
回退 2: 放在最后一条 compaction item 之前
回退 3: 追加到末尾
```

这确保 initial context (系统指令、工具定义) 在压缩后的历史中位置稳定,
模型训练时期望看到的 "compaction summary 是历史中的最后一项" 不被破坏。

**MacLaw 现状**:

MacLaw 的 system prompt 每次 LLM 调用都重新构建, 不存在"注入到历史中"的问题。
但 steering 规则和 proactive recall 的内容在压缩前后可能不一致。

**收益**: 中等。MacLaw 的架构不同, 但可以借鉴"压缩后确保关键上下文位置稳定"的思路。

---

### 差异 10 (P2): 无压缩分析事件 — 无法量化压缩效果

**Codex 做法**:

完整的分析事件 (`CompactionAnalyticsAttempt`):
```rust
CodexCompactionEvent {
    thread_id,
    turn_id,
    trigger: Auto | Manual,
    reason: UserRequested | ContextWindowExceeded | ...,
    implementation: Responses | Remote,
    phase: StandaloneTurn | MidTurn,
    strategy: Memento,
    status: Completed | Interrupted | Failed,
    error: Option<String>,
    active_context_tokens_before: i64,
    active_context_tokens_after: i64,
    started_at: u64,
    completed_at: u64,
    duration_ms: Option<u64>,
}
```

**MacLaw 现状**: 压缩 (trimHistory) 没有任何分析事件。无法知道:
- 压缩发生了多少次
- 每次压缩释放了多少 token
- 压缩耗时多久
- 压缩后 LLM 的表现是否下降

**收益**: 量化压缩效果, 为后续优化提供数据基础。

---

### 差异 11 (P2): fork-turn boundary 不区分用户 turn 和子 agent turn

**Codex 做法**:

`thread_rollout_truncation.rs` 区分两种 turn boundary:
- **user turn boundary**: 用户发送的消息
- **fork-turn boundary**: 用户消息 + 子 agent 的 `trigger_turn` 消息

截断时可以选择:
- `truncate_rollout_before_nth_user_message_from_start`: 按用户 turn 截断
- `truncate_rollout_to_last_n_fork_turns`: 按 fork turn 截断 (保留子 agent 上下文)

**MacLaw 现状**:

#56 的 turn boundary 只识别 `user -> assistant` 的角色切换, 不区分:
- 用户主动发送的消息
- 系统注入的消息 (recover prompt, steering 注入等)
- SubAgent (#75) 的任务委派消息

**收益**: 更精确的截断, 特别是在使用 SubAgent 时保留正确的上下文。


## 三、完整改进方案 (11 项)

### 改进 1: Handoff Recovery Prompt — 压缩后注入恢复上下文

**借鉴**: Codex `summary_prefix.md`

**根因**: MacLaw 压缩后的 `[对话摘要]` 标记没有告诉 LLM 这是压缩恢复上下文。LLM 可能把摘要当作普通系统消息, 不知道文件系统中已有之前工作的产出, 导致重复执行已完成的步骤。

**修复**:

`gui/im_conversation_trim.go` — `trimHistoryWithSummary` 的 separator 格式改为:

```go
const compactionRecoveryPrefix = `[上下文恢复] 之前的对话因为长度限制被压缩。以下是压缩摘要。

另一个语言模型已经开始处理这个任务并产出了工作摘要。你可以访问该模型使用过的工具的当前状态（文件系统、代码等反映了已完成的工作）。请基于已完成的工作继续，避免重复已做过的事情。

以下是之前模型产出的摘要，请利用其中的信息辅助你的分析：

`

// trimHistoryWithSummary 中:
if summary != "" {
    separator = compactionRecoveryPrefix + summary
} else {
    separator = "[...中间的工具调用和执行细节已省略...]"
}
```

**修改文件**:
- `gui/im_conversation_trim.go`: separator 格式 + `compactionRecoveryPrefix` 常量

---

### 改进 2: 结构化压缩 Prompt — 从"请总结"到"写交接文档"

**借鉴**: Codex `templates/compact/prompt.md`

**根因**: MacLaw 的 `makeSummarizer` prompt 是通用的"请总结以下对话", 没有结构化要求。LLM 生成的摘要质量不稳定——有时遗漏关键决策, 有时包含无用的执行细节。

**修复**:

`gui/im_conversation_trim.go` — `makeSummarizer` 的 prompt 改为:

```go
const compactionPrompt = `你正在执行上下文检查点压缩。为将要继续此任务的另一个 LLM 生成一份交接摘要。

包含以下内容:
- 当前进度和已做的关键决策
- 重要的上下文、约束条件和用户偏好
- 剩余待完成的工作（明确的下一步）
- 继续工作所需的关键数据、示例或引用

要求:
- 简洁、结构化
- 聚焦于帮助下一个 LLM 无缝继续工作
- 直接引用关键短语而非转述（防止语义漂移）
- 不要包含工具调用的原始输出（文件内容、命令输出等）

以下是需要压缩的对话内容:
`
```

**修改文件**:
- `gui/im_conversation_trim.go`: `makeSummarizer` 的 prompt

---

### 改进 3: 压缩时保留用户消息原文 — 用户意图不丢失

**借鉴**: Codex `collect_user_messages` + `build_compacted_history`

**根因**: MacLaw 的 turn boundary 只保留每个 turn 的首条用户消息。用户的补充需求、约束条件、修改要求如果不在 turn 首条, 会在压缩时丢失。

**修复**:

`gui/im_conversation_trim.go` — `trimHistoryWithSummary` 新增用户消息保留逻辑:

```go
const maxPreservedUserTokens = 8000 // Codex 用 20K, MacLaw 保守一些

// 从被截断的 entries 中提取用户消息, 从最近的开始倒序保留
func collectPreservedUserMessages(droppedEntries []agent.ConversationEntry, maxTokens int) []agent.ConversationEntry {
    var result []agent.ConversationEntry
    remaining := maxTokens
    for i := len(droppedEntries) - 1; i >= 0; i-- {
        e := droppedEntries[i]
        if e.Role != "user" { continue }
        text, ok := e.Content.(string)
        if !ok || text == "" { continue }
        tokens := estimateBytesToTokens(len(text))
        if tokens > remaining {
            // 超长消息截断后保留
            if remaining > 200 {
                truncated := truncateRunesSmart(text, remaining*3) // 粗略 token->rune
                result = append([]agent.ConversationEntry{{
                    Role: "user", Content: truncated,
                }}, result...)
            }
            break
        }
        result = append([]agent.ConversationEntry{e}, result...)
        remaining -= tokens
    }
    return result
}

// trimHistoryWithSummary 中, 构建 result 时:
// result = tier1 + preservedUserMessages + separator(recovery+summary) + recentWindow
preservedUsers := collectPreservedUserMessages(droppedEntries, maxPreservedUserTokens)
// 在 tier1 和 separator 之间插入 preservedUsers
```

**修改文件**:
- `gui/im_conversation_trim.go`: `collectPreservedUserMessages` + `trimHistoryWithSummary` 集成

---

### 改进 4: 渐进式头部截断回退 — 压缩/LLM调用超限时不直接报错

**借鉴**: Codex `compact.rs` 的 `ContextWindowExceeded` 处理

**根因**: MacLaw #74 加了 token 校准, 但极端情况下 (校准比率异常、模型 context window 突然变小) 仍可能超限。当前超限时直接返回错误, 用户看到"API 服务端错误"。

**修复**:

`gui/im_message_handler.go` — agent loop 的 LLM 调用处新增渐进式回退:

```go
const maxContextTrimRetries = 5

// 在 LLM 调用处
for contextTrimRetry := 0; contextTrimRetry < maxContextTrimRetries; contextTrimRetry++ {
    resp, err := callLLM(conversation, tools, ...)
    if !isContextWindowExceeded(err) {
        break // 成功或其他错误, 退出重试
    }
    // 从头部移除最旧的非 system 条目 (保留尾部, prefix cache 友好)
    removed := removeOldestNonSystemEntry(&conversation)
    if !removed {
        break // 没有可移除的条目了
    }
    log.Printf("[agent-loop] context window exceeded, removed oldest entry, retry %d/%d", contextTrimRetry+1, maxContextTrimRetries)
}
```

`isContextWindowExceeded` 检测:
```go
func isContextWindowExceeded(err error) bool {
    if err == nil { return false }
    msg := strings.ToLower(err.Error())
    return strings.Contains(msg, "context window") ||
           strings.Contains(msg, "context_length_exceeded") ||
           strings.Contains(msg, "maximum context length") ||
           strings.Contains(msg, "prompt is too long")
}
```

**修改文件**:
- `gui/im_message_handler.go`: LLM 调用处新增重试逻辑
- `gui/llm_errors.go` (或 `corelib/llm/types.go`): `isContextWindowExceeded` 函数

---

### 改进 5: 新会话启动时异步提取旧对话记忆

**借鉴**: Codex `memories/phase1.rs`

**根因**: MacLaw 的 `KnowledgeExtractor` 有 1h 冷却期且只在会话过期时触发。用户连续工作数小时时, 中间的对话内容不会被提取为长期记忆。Phase 1 (#62) 的 `SavePhaseOutput` 只覆盖工作流阶段产出物, 不覆盖普通对话中的知识。

**修复**:

新增 `corelib/memory/session_start_extractor.go`:

```go
// SessionStartExtractor 在新会话启动时异步提取旧对话的记忆
type SessionStartExtractor struct {
    store           *Store
    conversationDir string
    llmClient       LLMClient // 接口, 由 GUI/TUI 注入
    mu              sync.Mutex
    processing      map[string]bool // 防止并发处理同一个会话文件
}

// ExtractFromPreviousSession 异步提取上一个会话的记忆
func (e *SessionStartExtractor) ExtractFromPreviousSession(userID string) {
    go func() {
        // 1. 扫描 conversation 持久化目录, 找到该用户最近的已完成会话
        sessionFile := e.findLatestCompletedSession(userID)
        if sessionFile == "" { return }

        // 2. 检查是否已处理过 (防止重复提取)
        e.mu.Lock()
        if e.processing[sessionFile] {
            e.mu.Unlock()
            return
        }
        e.processing[sessionFile] = true
        e.mu.Unlock()
        defer func() {
            e.mu.Lock()
            delete(e.processing, sessionFile)
            e.mu.Unlock()
        }()

        // 3. 加载会话历史
        entries := loadSessionEntries(sessionFile)
        if len(entries) < 5 { return } // 太短的会话不值得提取

        // 4. 构建提取 prompt (借鉴 Codex phase1)
        prompt := buildExtractionPrompt(entries)

        // 5. 调用 LLM 提取结构化记忆
        result, err := e.llmClient.Generate(prompt, extractionOutputSchema())
        if err != nil { return }

        // 6. 脱敏 + 保存
        result.RawMemory = redactSecrets(result.RawMemory)
        result.Summary = redactSecrets(result.Summary)

        if result.RawMemory != "" {
            e.store.Save(Entry{
                Content:    result.RawMemory,
                Category:   CategoryTaskArtifact,
                Tags:       []string{"session_extraction", "auto"},
                Scope:      ScopeProject,
                SourceType: "session_start_extraction",
            })
        }
    }()
}

// 提取 prompt — 借鉴 Codex phase1 的结构化输出
func buildExtractionPrompt(entries []ConversationEntry) string {
    // 序列化对话历史 (过滤掉 system 消息和过长的 tool result)
    // 要求 LLM 输出:
    // { "raw_memory": "详细的 Markdown 记忆",
    //   "rollout_summary": "一行紧凑摘要" }
}
```

**调用点**: `gui/im_message_handler.go` — `handleIMMessageWithLoop` 开头:

```go
// 新会话的第一条消息时触发
if h.sessionStartExtractor != nil && isFirstMessageInSession(userID) {
    h.sessionStartExtractor.ExtractFromPreviousSession(userID)
}
```

**修改文件**:
- `corelib/memory/session_start_extractor.go`: 新文件
- `gui/im_message_handler.go`: 调用点
- `gui/app.go`: 初始化 `SessionStartExtractor`

---

### 改进 6: 记忆写入时 Secret 脱敏

**借鉴**: Codex `redact_secrets`

**根因**: MacLaw 的 `memory.Store.Save()` 没有脱敏步骤。SSH 密码、API key、数据库连接字符串可能被原样保存到 `memories.json`。

**修复**:

新增 `corelib/memory/redact.go`:

```go
package memory

import "regexp"

var secretPatterns = []*regexp.Regexp{
    // API keys
    regexp.MustCompile(`(?i)(api[_-]?key|apikey|api[_-]?token)\s*[:=]\s*['"]?([a-zA-Z0-9_\-]{20,})['"]?`),
    // Passwords
    regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*['"]?([^\s'"]{4,})['"]?`),
    // Bearer tokens
    regexp.MustCompile(`(?i)bearer\s+([a-zA-Z0-9_\-\.]{20,})`),
    // Private keys
    regexp.MustCompile(`(?i)-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----`),
    // Connection strings
    regexp.MustCompile(`(?i)(mongodb|postgres|mysql|redis)://[^\s]+@[^\s]+`),
    // AWS keys
    regexp.MustCompile(`(?i)(AKIA|ASIA)[A-Z0-9]{16}`),
    // Generic secrets
    regexp.MustCompile(`(?i)(secret|token|credential)\s*[:=]\s*['"]?([a-zA-Z0-9_\-]{16,})['"]?`),
}

func RedactSecrets(text string) string {
    result := text
    for _, pattern := range secretPatterns {
        result = pattern.ReplaceAllStringFunc(result, func(match string) string {
            // 保留 key 名, 替换 value
            // 例: "password=abc123" -> "password=[REDACTED]"
            loc := pattern.FindStringSubmatchIndex(match)
            if len(loc) >= 6 {
                return match[:loc[4]] + "[REDACTED]"
            }
            return "[REDACTED]"
        })
    }
    return result
}
```

**注入点**: `corelib/memory/store.go` — `Save()` 和 `SaveWithContext()`:

```go
func (s *Store) Save(entry Entry) error {
    // 脱敏
    entry.Content = RedactSecrets(entry.Content)
    // ... 现有逻辑 ...
}
```

**修改文件**:
- `corelib/memory/redact.go`: 新文件
- `corelib/memory/redact_test.go`: 新文件
- `corelib/memory/store.go`: `Save` 和 `SaveWithContext` 调用 `RedactSecrets`

---

### 改进 7: 压缩后强制重算 Token 使用量

**借鉴**: Codex `sess.recompute_token_usage()`

**根因**: MacLaw #74 发现 `estimateConversationTokens` 可能低估 30%。压缩后如果不重算, 下一次 `trimConversation` 的 token 预算决策基于不准确的数据。

**修复**:

`gui/im_message_handler.go` — `saveConversationHistoryTimed` 中, `trimHistoryWithSummary` 执行后:

```go
// 压缩后强制重置 token 校准状态
if trimmed {
    // 重置 lastLLMInputTokens, 强制下一次 LLM 调用后重新校准
    h.resetTokenCalibration(userID)
}
```

`resetTokenCalibration` 将 `lastLLMInputTokens` 设为 0, 使得下一次 LLM 调用后的校准基于压缩后的实际 token 数。

**修改文件**:
- `gui/im_message_handler.go`: 压缩后重置校准状态

---

### 改进 8: 压缩质量警告 — 告知用户多次压缩会降低质量

**借鉴**: Codex 的 `WarningEvent`

**根因**: 用户不知道对话已被压缩, 也不知道多次压缩会导致 LLM 准确性下降。用户可能在一个已经压缩多次的会话中继续工作, 而不知道应该开始新会话。

**修复**:

`gui/im_message_handler.go` — agent loop 中, 检测到压缩发生后:

```go
// 追踪压缩次数
compactionCount := h.getCompactionCount(userID)
if compactionCount > 0 && compactionCount % 2 == 0 { // 每 2 次压缩提醒一次
    warningMsg := fmt.Sprintf(
        "提示: 当前对话已经过 %d 次上下文压缩。长对话和多次压缩可能导致模型准确性下降。"+
        "建议在合适的时候开始新对话 (/new), 保持对话简短和聚焦。",
        compactionCount,
    )
    // 桌面面板: 显示为系统提示
    // IM 通道: 发送为普通消息
    h.sendSystemNotification(userID, warningMsg)
}
```

**修改文件**:
- `gui/im_message_handler.go`: 压缩计数 + 警告逻辑
- `gui/im_session_state.go`: `compactionCount` 持久化 (sync.Map)

---

### 改进 9: 压缩分析事件 — 量化压缩效果

**借鉴**: Codex `CompactionAnalyticsAttempt`

**根因**: 无法量化压缩效果, 无法知道压缩发生了多少次、释放了多少 token、耗时多久。

**修复**:

`gui/im_conversation_trim.go` — `trimHistoryWithSummary` 返回压缩统计:

```go
type CompactionStats struct {
    Trigger         string    // "auto" | "manual"
    EntriesBefore   int       // 压缩前条目数
    EntriesAfter    int       // 压缩后条目数
    TokensBefore    int       // 压缩前估算 token 数
    TokensAfter     int       // 压缩后估算 token 数
    UserMsgsPreserved int     // 保留的用户消息数
    SummaryGenerated  bool    // 是否生成了 LLM 摘要
    SummaryTokens     int     // 摘要的 token 数
    DurationMs        int64   // 压缩耗时 (含 LLM 调用)
    StartedAt         time.Time
}
```

记录到日志:
```go
log.Printf("[compaction] trigger=%s entries=%d->%d tokens=%d->%d user_msgs_preserved=%d summary=%v duration=%dms",
    stats.Trigger, stats.EntriesBefore, stats.EntriesAfter,
    stats.TokensBefore, stats.TokensAfter,
    stats.UserMsgsPreserved, stats.SummaryGenerated, stats.DurationMs)
```

**修改文件**:
- `gui/im_conversation_trim.go`: `CompactionStats` + `trimHistoryWithSummary` 返回统计
- `gui/im_message_handler.go`: 记录统计到日志

---

### 改进 10: fork-turn boundary — 区分用户 turn 和 SubAgent turn

**借鉴**: Codex `fork_turn_positions_in_rollout`

**根因**: MacLaw #75 引入了 CodingSubAgent, #18 引入了 delegate_task。这些子 agent 的消息在对话历史中与用户消息混在一起。`trimHistory` 的 turn boundary 不区分用户发起的 turn 和子 agent 发起的 turn, 可能在截断时丢失子 agent 的关键上下文。

**修复**:

`gui/im_conversation_trim.go` — turn boundary 检测增强:

```go
// 现有: 只检测 role 切换
func isTurnBoundary(prev, curr ConversationEntry) bool {
    return prev.Role != "user" && curr.Role == "user"
}

// 增强: 区分用户 turn 和 SubAgent turn
type turnBoundaryType int
const (
    turnBoundaryUser     turnBoundaryType = iota // 用户发起的 turn
    turnBoundarySubAgent                          // SubAgent 发起的 turn
)

func classifyTurnBoundary(prev, curr ConversationEntry) (bool, turnBoundaryType) {
    if prev.Role != "user" && curr.Role == "user" {
        // 检查是否是 SubAgent 的 trigger turn
        text, ok := curr.Content.(string)
        if ok && isSubAgentTrigger(text) {
            return true, turnBoundarySubAgent
        }
        return true, turnBoundaryUser
    }
    return false, 0
}

func isSubAgentTrigger(text string) bool {
    // 检测 SubAgent 委派标记
    return strings.Contains(text, "__SUBAGENT_CONTEXT__") ||
           strings.Contains(text, "[SubAgent Task]")
}
```

截断策略: SubAgent turn boundary 的优先级低于用户 turn boundary。
当需要截断时, 优先保留用户 turn boundary, SubAgent turn boundary 可以被压缩到摘要中。

**修改文件**:
- `gui/im_conversation_trim.go`: `classifyTurnBoundary` + 截断优先级

---

### 改进 11: 记忆提取时过滤 developer/system 消息

**借鉴**: Codex `sanitize_response_item_for_memories`

**根因**: MacLaw 的 `KnowledgeExtractor` 和 `ConversationArchiver` 将整个对话历史 (含 system prompt、steering 注入、recover prompt 等) 发送给 LLM 提取记忆。这些系统消息不包含用户知识, 但占用 LLM 的 context 和注意力, 降低提取质量。

**修复**:

`corelib/memory/knowledge_extractor.go` — `Extract` 中过滤系统消息:

```go
func filterForMemoryExtraction(entries []ConversationEntry) []ConversationEntry {
    var filtered []ConversationEntry
    for _, e := range entries {
        // 跳过 system/developer 消息
        if e.Role == "system" || e.Role == "developer" { continue }

        // 跳过 recover prompt 注入
        if e.Role == "user" {
            text, ok := e.Content.(string)
            if ok && isMemoryExcludedContent(text) { continue }
        }

        // 跳过过长的 tool result (只保留摘要)
        if e.Role == "tool" {
            text, ok := e.Content.(string)
            if ok && len([]rune(text)) > 2000 {
                e.Content = truncateRunesSmart(text, 500) + "\n[...tool output truncated...]"
            }
        }

        filtered = append(filtered, e)
    }
    return filtered
}

func isMemoryExcludedContent(text string) bool {
    excludePrefixes := []string{
        "[上下文恢复]",
        "[对话摘要]",
        "[系统通知]",
        compactionRecoveryPrefix[:30], // 改进 1 的 recovery prompt
    }
    for _, prefix := range excludePrefixes {
        if strings.HasPrefix(text, prefix) { return true }
    }
    return false
}
```

**修改文件**:
- `corelib/memory/knowledge_extractor.go`: `filterForMemoryExtraction`
- `gui/conversation_archiver.go`: 同步使用 `filterForMemoryExtraction`


## 四、与现有改进记录的关系

| 本方案改进 | 对应已有改进 | 关系 |
|-----------|------------|------|
| 改进 1 (Recovery Prompt) | #66 Phase 7 智能压缩 | **增强**: #66 实现了 LLM 摘要, 本次加 recovery prompt |
| 改进 2 (结构化压缩 Prompt) | #66 Phase 7 智能压缩 | **增强**: 改进压缩 prompt 质量 |
| 改进 3 (保留用户消息原文) | #56 turn boundary | **增强**: #56 保留 turn boundary, 本次扩展为保留所有用户消息 |
| 改进 4 (渐进式头部截断) | #74 token 校准 | **补充**: #74 做预防, 本次加 fallback |
| 改进 5 (新会话启动提取记忆) | #62 Phase 1 产出物沉淀 | **补充**: #62 覆盖工作流产出物, 本次覆盖普通对话 |
| 改进 6 (Secret 脱敏) | 无 | **新增** |
| 改进 7 (压缩后重算 Token) | #74 token 校准 | **补充**: 确保校准数据在压缩后准确 |
| 改进 8 (压缩质量警告) | 无 | **新增** |
| 改进 9 (压缩分析事件) | 无 | **新增** |
| 改进 10 (fork-turn boundary) | #56 turn boundary, #75 SubAgent | **增强**: 区分用户 turn 和 SubAgent turn |
| 改进 11 (记忆提取过滤) | #62 Phase 1 产出物沉淀 | **补充**: 提升记忆提取质量 |

## 五、优先级和实施顺序

### 第一批 (P0, 1-2 天): 压缩质量核心改进

| 改进 | 工作量 | 修改文件数 | 风险 |
|------|--------|-----------|------|
| 1. Recovery Prompt | 0.5 天 | 1 | 低 (纯文本变更) |
| 2. 结构化压缩 Prompt | 0.5 天 | 1 | 低 (纯文本变更) |
| 3. 保留用户消息原文 | 1 天 | 1 | 中 (改变截断逻辑) |

这三个改进只改 `gui/im_conversation_trim.go` 一个文件, 是最高 ROI 的改动。
改进 1+2 改变压缩的"输入"(prompt) 和"输出"(separator 格式)。
改进 3 改变压缩后保留的内容。

### 第二批 (P1, 1-2 天): 安全 + 容错

| 改进 | 工作量 | 修改文件数 | 风险 |
|------|--------|-----------|------|
| 6. Secret 脱敏 | 0.5 天 | 3 (新文件+store+test) | 低 (增量, 不改已有逻辑) |
| 4. 渐进式头部截断 | 0.5 天 | 1-2 | 低 (增量, fallback 逻辑) |
| 7. 压缩后重算 Token | 0.5 天 | 1 | 低 (增量) |

### 第三批 (P1, 2-3 天): 跨会话记忆提取

| 改进 | 工作量 | 修改文件数 | 风险 |
|------|--------|-----------|------|
| 5. 新会话启动提取记忆 | 2 天 | 3 (新文件+handler+app) | 中 (新增异步逻辑) |
| 11. 记忆提取过滤 | 0.5 天 | 2 | 低 (增量) |

### 第四批 (P2, 1-2 天): 可观测性 + 精细化

| 改进 | 工作量 | 修改文件数 | 风险 |
|------|--------|-----------|------|
| 8. 压缩质量警告 | 0.5 天 | 2 | 低 |
| 9. 压缩分析事件 | 0.5 天 | 2 | 低 |
| 10. fork-turn boundary | 1 天 | 1 | 中 (改变截断逻辑) |

## 六、验收标准

### 改进 1+2 (Recovery Prompt + 结构化压缩 Prompt)
- 压缩后的 separator 以 `[上下文恢复]` 开头, 包含 recovery prompt
- LLM 生成的摘要包含 4 个 section (进度/上下文/待办/关键数据)
- 压缩后 LLM 不重复执行已完成的步骤 (通过 trajectory 验证)

### 改进 3 (保留用户消息原文)
- 压缩后的历史中包含所有用户消息原文 (上限 8K token)
- 用户的补充需求 ("加上音效"、"用 C++ cmake") 在压缩后仍可见
- 总 token 预算不超过 `MaxConversationTurns` 对应的 token 数

### 改进 4 (渐进式头部截断)
- `ContextWindowExceeded` 错误不直接返回给用户
- 最多重试 5 次, 每次从头部移除 1 条
- 重试成功后正常继续, 重试失败后返回友好错误

### 改进 5 (新会话启动提取记忆)
- 新会话的第一条消息触发异步提取 (不阻塞响应)
- 提取结果存入 `memory.Store` 的 `task_artifact` 类别
- 下一条消息的 proactive recall 能召回提取的记忆
- 太短的会话 (<5 条) 不触发提取

### 改进 6 (Secret 脱敏)
- `password=abc123` 保存后变为 `password=[REDACTED]`
- `AKIA1234567890ABCDEF` 保存后变为 `[REDACTED]`
- `Bearer eyJhbGci...` 保存后变为 `Bearer [REDACTED]`
- 正常内容不受影响

### 改进 7 (压缩后重算 Token)
- 压缩后 `lastLLMInputTokens` 被重置为 0
- 下一次 LLM 调用后重新校准

### 改进 8 (压缩质量警告)
- 第 2 次压缩后显示警告
- 第 4 次压缩后再次显示
- 警告建议用户开始新对话

### 改进 9 (压缩分析事件)
- 日志中记录 `[compaction]` 事件
- 包含 entries before/after, tokens before/after, duration

### 改进 10 (fork-turn boundary)
- SubAgent 的 trigger turn 被正确识别
- 截断时优先保留用户 turn boundary
- SubAgent turn boundary 可以被压缩到摘要中

### 改进 11 (记忆提取过滤)
- system/developer 消息不被发送给 LLM 提取
- recovery prompt 不被提取为记忆
- 过长的 tool result 被截断到 500 字符
