# Fix #76: Agent Loop 失控——三层缓解机制

## 来源

用户让 maclaw 开发 C++ 超级玛利游戏，agent loop 跑了 95 次迭代，context 膨胀到 130K token，最终因连续 3 次空响应被 hard exit 终止。

## 根因分析

三个独立问题叠加导致 agent loop 失控：

### 根因 1 (P0): SubAgent 未触发——两条激活路径都不匹配

**Path 1** 条件：`workflowAgentLoop && ws.CurrentPhase == PhaseCodingImplementation`
- 活跃工作流是 `presentation_design`（PPT 设计），不是 `coding`
- 用户消息"继续"通过 `handlePendingConfirm` 被分类为 `"other"` → `workflowAgentLoop=false`
- 三个条件全部不满足

**Path 2** 条件：`preGate.active && !preGate.skipSignal`
- `preGate` 从 `msg.Text="继续"` 重新计算
- "继续"不包含编码关键词 → `preGate.active=false`
- 但 agent loop 内部的 `gateConfig`（从对话历史+用户消息综合判断）`active=true`
- **信号丢失**：SubAgent 拦截点在 `runAgentLoop` 之前，用的是原始 `msg.Text`，而 `runAgentLoop` 内部的 gate 用的是更丰富的上下文

### 根因 2 (P0): ContextLength 配置为 180K，trimConversation 不触发

- `maclaw_llm_context_length = 180000`，智谱编程 provider `context_length = 180000`
- `EffectiveContextTokens = 180000 * 80% = 144000`
- `msgBudget = 144000 - toolsTokenBudget ≈ 139000`
- API 报告最高 130880 input tokens < 139000 → trimConversation 正确判断不需要裁剪
- 但 glm-5.1 的**实际可用** context window 远小于 180K——在 120K+ 时就开始返回空响应
- **标称 vs 实际的差距**：配置的 context_length 是 API 接受的最大值，不是模型能有效处理的最大值

### 根因 3 (P1): HasPhaseOutput 始终 false——NeedsConfirm gate 永远不拦截

- 日志显示 95 次迭代中每次都打印 `NeedsConfirm tool branch: first execution (hasOutput=false), skipping engine gate`
- `SavePhaseOutput` 从未被调用——agent loop 中没有代码路径在编码执行期间调用它
- NeedsConfirm gate 的设计意图是"第一次执行放行，产出物生成后拦截"，但产出物永远不被记录
- 这个问题在 SubAgent 路径中不存在（SubAgent 完成后会调用 `SavePhaseOutput`），但主 Agent 路径缺失

## 修复方案

### 修复 1: SubAgent Path 2 复用 agent loop 的 gate 信号

**问题**：Path 2 用 `msg.Text`（"继续"）重新计算 gate config，丢失了对话历史中的编码意图信号。

**修复**：当 `msg.Text` 是短动作指令（"继续"/"开工"/"开干"等）且对话历史中有编码上下文时，Path 2 应该从对话历史中推断编码意图，而不是仅从 `msg.Text` 计算。

具体实现：在 Path 2 的 `preGate` 计算后，如果 `preGate.active=false` 且 `msg.Text` 匹配 `codingActionPhrases`（已有列表），扫描对话历史（`h.memory.Load(userID)` 最近 10 条）查找编码关键词。如果找到，视为编码延续信号，激活 orchestrator。

这与 #21 的 `checkSessionTaskGuard` 上下文感知修复是同一个模式——短动作指令 + 历史编码上下文 = 编码延续。

### 修复 2: 主 Agent 编码执行迭代预算

**问题**：主 Agent 在编码执行阶段没有迭代上限（除了全局的 `max_rounds=300`），95 轮迭代把 context 撑爆。

**修复**：在 `runAgentLoop` 中新增编码执行阶段的迭代预算。

检测条件：连续 N 次迭代中，编码工具（write_file/edit_file/bash）调用占比 > 80%。

触发行为：
- 达到 `codingIterationBudget=50` 时，注入系统消息提醒 LLM 保存进度并汇报
- 达到 `codingIterationHardLimit=60` 时，强制返回当前结果，附带进度摘要
- 返回的响应包含已完成的文件列表和未完成的任务描述，用户可以说"继续"恢复

实现位置：在 `runAgentLoop` 的迭代循环开始处（`trimConversation` 之后），新增编码迭代计数器。每次迭代检查工具调用类型，更新计数器。

### 修复 3: 基于 API 实际 token 的 context 硬上限

**问题**：`ContextLength=180K` 是 API 接受的最大值，但模型在 120K+ 时就失效。token 校准只在估算偏差 >15% 时触发，当估算准确时不触发。

**修复**：新增 `apiReportedTokenHardLimit`——当 API 报告的 `input_tokens` 超过 `EffectiveContextTokens * 90%` 时，强制触发 trimConversation，不依赖估算。

```go
// 在 lastLLMInputTokens 更新后、下一次 trimConversation 之前
if lastLLMInputTokens > 0 {
    hardCeiling := cfg.EffectiveContextTokens() * 90 / 100  // 90% of effective
    if lastLLMInputTokens > hardCeiling {
        // API reports we're dangerously close to the limit.
        // Force trim by reducing effectiveTokenLimit to 70% of effective.
        forcedLimit := cfg.EffectiveContextTokens() * 70 / 100
        if forcedLimit < effectiveTokenLimit {
            log.Printf("[trim-hardlimit] API reported %d tokens > 90%% ceiling %d, forcing limit to %d",
                lastLLMInputTokens, hardCeiling, forcedLimit)
            effectiveTokenLimit = forcedLimit
        }
    }
}
```

这个硬上限不依赖估算准确性——直接用 API 报告的实际 token 数做判断。即使 `ContextLength=180K`，当 API 报告 130K 时（> 144K * 90% = 129.6K），强制裁剪到 144K * 70% = 100.8K。

### 修复 4: 空响应时主动裁剪（而非仅注入 Recover）

**问题**：空响应（`output=0, usage_nil=true`）几乎总是因为 context 过大。当前只注入 Recover 系统消息（进一步膨胀 context），不裁剪。

**修复**：空响应时，在注入 Recover 消息之前，先强制裁剪 conversation 到 `EffectiveContextTokens * 60%`。

```go
if output == 0 && usageNil {
    // Empty response — likely context too large. Force aggressive trim.
    aggressiveLimit := cfg.EffectiveContextTokens() * 60 / 100
    if aggressiveLimit < effectiveTokenLimit {
        log.Printf("[empty-response-trim] forcing aggressive trim from %d to %d", effectiveTokenLimit, aggressiveLimit)
        effectiveTokenLimit = aggressiveLimit
        conversation = trimConversation(conversation, effectiveTokenLimit, toolsTokenBudget, summarizer)
    }
}
```

## 四个修复的关系

| 修复 | 层面 | 触发条件 | 效果 |
|------|------|---------|------|
| 修复 1 | SubAgent 激活 | 短动作指令 + 历史编码上下文 | 根本解决——编码在纯净 context 中执行 |
| 修复 2 | 迭代预算 | 连续 50+ 轮编码工具调用 | 安全网——主 Agent 编码不会无限跑 |
| 修复 3 | Token 硬上限 | API 报告 > 90% effective | 防止 context 膨胀到模型失效区 |
| 修复 4 | 空响应裁剪 | output=0 | 最后防线——空响应时主动缩减 context |

修复 1 是根本解决（SubAgent 纯净 context），修复 2-4 是纵深防御（即使 SubAgent 不触发，主 Agent 也不会失控）。

## 验收标准

- 用户在 PPT 工作流期间说"开发超级玛利游戏"→"继续" → SubAgent 被激活（Path 2 上下文感知）
- 主 Agent 编码执行 50 轮后收到进度提醒，65 轮后强制返回
- API 报告 input tokens > 85% effective 时 trimConversation 被强制触发
- 空响应后下一轮 conversation 被主动裁剪到 60% effective
- 所有现有 CodingGate / SubAgent / DriftDetector 测试通过
- GUI / TUI / corelib 编译通过 
## 修改文件

| 文件 | 修改 |
|------|------|
| `gui/im_message_handler.go` | 修复 1: SubAgent Path 2 上下文感知（`hasCodingActionPhrase` + `conversationHasCodingContext` fallback）|
| `gui/im_message_handler.go` | 修复 2: 编码迭代预算（`codingIterBudgetSoft=50` / `codingIterBudgetHard=65`）|
| `gui/im_message_handler.go` | 修复 3: API token 硬上限（`lastLLMInputTokens > 85% effective` → 强制裁剪到 65%）|
| `gui/im_message_handler.go` | 修复 4: 空响应主动裁剪（`lastLLMOutputTokens == 0` → 裁剪到 60% effective）|
| `docs/fix-76-agent-loop-runaway.md` | 本文档 |
