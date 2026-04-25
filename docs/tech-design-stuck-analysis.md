# 技术设计阶段卡住——机制层面分析报告

**日期**: 2026-04-26
**来源**: maclaw.log + trajectory 文件分析

## 一、现象描述

用户在桌面 AI 助手面板发送"在d:\workprj\steave2 下开发一个c++的警察抓小偷游戏。图形界面，画面精美，cmake管理，有音效。"后，编码工作流启动，但在技术设计阶段"卡住"——用户感知到长时间无响应。

## 二、时间线重建（从日志和 trajectory 文件）

| 时间 | 事件 | 耗时 |
|------|------|------|
| 05:40:41 | 用户发送编码任务 → UIC 分类为 coding (conf=0.90) → cross-type replacement 取消旧 presentation_design → StartWorkflow(coding) | 2.38s |
| 05:40:41 | `agent_loop=2.380381s first_token=0s` — 响应返回，**无 LLM 调用** | — |
| 05:43:54 | 用户发送"继续"（等了 3 分钟后手动触发） | — |
| 05:43:58 | 第一次 LLM 调用 (input=12980, output=42) — 短回复 | 4s |
| 05:44:26 | 第二次 LLM 调用 (input=13197, output=1429) — 生成需求文档 | 28s |
| 05:44:26 | `NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue` | — |
| 05:44:26 | post-loop doc capture: requirements (4860 chars) | — |
| 05:44:34 | 用户发送"确认" → UIC 分类为 continuation (conf=0.88) | — |
| 05:44:34 | AgentLoop start task="确认" | — |
| 05:44:38 | SteeringWorkflow detector NOT activated | — |
| **05:44:38 → 05:50:10** | **LLM 调用 (input=16095, output=4775) — 生成技术设计文档** | **5 分 32 秒** |
| 05:50:10 | `NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue` | — |
| 05:50:10 | trajectory saved (3 entries) | — |
| 05:50:11 | 用户发送"继续" → AgentLoop start | — |
| 05:50:41 | LLM 调用 (input=21243, output=1174) — 技术设计文档补充 | 30s |
| 05:50:41 | post-loop doc capture: tech_design (4162 chars) | — |
| 05:51:55 | 应用关闭 | — |

## 三、机制层面根因分析

### 问题 1 (P0): 工作流启动后未自动触发需求文档生成

**时间**: 05:40:41

**日志证据**:
- `[WorkflowInterception] cross-type replacement: cancelling active workflow and re-routing`
- `[WorkflowInterception] UIC fusion determined workflow: coding`
- `agent_loop=2.380381s first_token=0s` — 无 LLM 调用
- **缺失**: `[WorkflowInterception] StartWorkflow succeeded, re-routing to handleActiveWorkflow` 日志不存在

**根因**: 运行的 gui.exe 二进制文件编译自旧版源码，不包含 #77 的修复。

旧代码路径（`handleNeedsUnderstanding` 中 `StartWorkflow` 成功后）:
```go
// 旧代码 — 直接返回概览文本，不触发 agent loop
return &IMAgentResponse{Text: overview}
```

新代码路径（#77 修复后，当前源码）:
```go
// 新代码 — 发送概览文本后，re-route 到 handleActiveWorkflow 触发 agent loop
if cb := engine.GetCallbacks(); cb != nil {
    _ = cb.SendTextToUser(userID, overview)
}
return h.handleActiveWorkflow(engine, userID, text)
```

**机制性问题**: `handleNeedsUnderstanding` 中 `StartWorkflow` 创建了工作流状态（phase 0），但直接返回了 `&IMAgentResponse{Text: overview}`。这个非 nil 响应被 `handleWorkflowInterception` 返回给 `handleIMMessageWithLoop`，后者在 line 3948 `return wfResp` 直接返回给前端。`workflowAgentLoopMarker` 从未被设置，agent loop 从未运行。

**影响**: 用户看到"🚀 工作流已启动"后面板就停了，需要手动发送"继续"才能触发需求文档生成。

**修复状态**: 当前源码已修复（`handlePostStartWorkflow` 函数），但运行的二进制文件是旧版本。**需要重新编译**。

### 问题 2 (P1): LLM 响应极慢——技术设计生成耗时 5 分 32 秒

**时间**: 05:44:38 → 05:50:10

**日志证据**:
```
05:44:38 [SteeringWorkflow] detector NOT activated
（5 分 32 秒无任何日志，只有 PingMaclawLLM 心跳）
05:50:10 [LLM] usage main_round provider="智谱编程" input=16095 output=4775
```

**根因**: 智谱编程 (glm-5.1) API 对 16K input + 4.7K output 的请求响应延迟极高（5.5 分钟）。这不是 maclaw 的 bug，是 API 服务商的性能问题。

**机制性分析**: maclaw 的 LLM 请求是同步阻塞的——`doOpenAILLMRequestStream` 发送 HTTP 请求后等待 SSE 流开始。在 SSE 第一个 token 到达之前，整个 agent loop 被阻塞。前端显示"正在思考..."但没有任何进度反馈。

**对比**: 需求文档生成（input=13197, output=1429）只用了 28 秒。技术设计（input=16095, output=4775）用了 5.5 分钟。input 增加了 22%，output 增加了 234%，但耗时增加了 1180%。这说明 glm-5.1 的延迟与 output token 数量呈超线性关系。

### 问题 3 (P2): NeedsConfirm gate 在 iter=0 时被 hasOutput=false 旁路

**日志证据**:
```
05:44:26 [agent-loop] NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue (iter=1)
05:50:10 [agent-loop] NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue (iter=0)
```

**机制分析**: 这实际上是**正确行为**，不是 bug。

`HasPhaseOutput(userID)` 检查 `ws.PhaseOutputs[ws.CurrentPhase]`。在 agent loop 内部，LLM 刚生成了文档文本，但 `SavePhaseOutput` 还没被调用（它在 post-loop doc capture 中调用）。所以 `HasPhaseOutput` 返回 false。

gate 的设计意图是：
- `hasOutput=false`（第一次执行）→ 允许 loop 继续，让 LLM 生成文档
- `hasOutput=true`（已有产出物）→ 拦截，等待用户确认

第一次执行时 `hasOutput=false` 是正确的——文档还没生成，不应该拦截。

但这里有一个微妙的时序问题：当 `advancePhase` 推进到 tech_design 阶段后，tech_design 阶段确实没有产出物（`hasOutput=false`）。所以 gate 允许 loop 继续。LLM 生成技术设计文档后，gate 在 `iter=0` 时检查 `hasOutput`——仍然是 false（因为 `SavePhaseOutput` 还没调用）。所以 gate 允许 loop 继续到 `iter=1`。

在 `iter=1` 时，如果 LLM 没有工具调用且输出了实质性文本，`isSubstantivePhaseDocument` 会返回 true，gate 会拦截。但如果 LLM 在 `iter=0` 就输出了完整文档且没有工具调用，loop 会在 no-tool stall 检测中结束，post-loop doc capture 保存文档。

**结论**: 这个行为是正确的。gate 的 `hasOutput=false` 旁路确保了第一次执行不被拦截。

### 问题 4 (P2): 技术设计文档被分成两次 LLM 调用

**日志证据**:
- 05:50:10: trajectory saved (3 entries) — 第一次 LLM 调用生成了技术设计文档的前半部分
- 05:50:41: trajectory saved — 第二次 LLM 调用（"继续"消息）生成了补充部分

**机制分析**: 第一次 LLM 调用（output=4775 tokens）可能因为 `finish_reason=length`（output token 限制）被截断。`filterTruncatedToolCalls` 只处理 tool call 截断，不处理纯文本截断。#74 的修复（`finish_reason=length` 纯文本续写）可能也未包含在运行的二进制中。

用户发送"继续"后，LLM 在新的 agent loop 中补充了技术设计文档的剩余部分。

## 四、修复建议

### 修复 1 (P0): 重新编译 gui.exe

当前源码已包含 #77 的修复（`handlePostStartWorkflow` 函数）。重新编译后：
- 工作流启动时自动触发 agent loop 生成需求文档
- 用户不需要手动发送"继续"

### 修复 2 (P1): LLM 响应慢的缓解

这不是 maclaw 的 bug，但可以改善用户体验：

**方案 A**: 在 LLM 请求期间显示更详细的进度提示
- 当前：只显示"正在思考..."
- 改进：显示"正在生成技术设计文档，预计需要 1-5 分钟..."（基于 phase 类型和历史延迟）

**方案 B**: 考虑切换到响应更快的 LLM 服务商

### 修复 3 (P2): finish_reason=length 纯文本续写

确认 #74 的修复是否包含在当前源码中。如果是，重新编译即可解决技术设计文档被截断的问题。

## 五、Review/Fix/Optimize

### Review 结论

1. **#77 修复已在当前源码中正确实现**。`handlePostStartWorkflow` 是所有工作流启动路径的单一后处理函数，三个调用点（UIC、IUM、keyword fallback）全部使用它。`go build ./gui/` 编译通过。

2. **Cross-type replacement 路径正确**。当用户在活跃工作流期间发送不同类型的任务时，`handleActiveWorkflow` → `CancelWorkflow` → `handleWorkflowInterception`（递归）→ `handleNeedsUnderstanding` → `StartWorkflow` → `handlePostStartWorkflow` → `handleActiveWorkflow`（第二次）→ `HandleInput` → `RunAgentLoop=true` → 设置 markers → 返回 nil → agent loop 运行。整个链路正确。

3. **`finish_reason=length` 纯文本续写已实现**（#74）。当 LLM 输出被 output token 限制截断时，agent loop 注入续写 prompt，最多续写 3 次。但 **Review 发现一个机制性 bug**：续写后 `resp.Text` 只包含最后一个 chunk，`SavePhaseOutput` 保存的是不完整的阶段产出物。已修复（见 Fix 2）。

4. **NeedsConfirm gate 的 `hasOutput=false` 旁路是正确设计**。第一次执行时阶段没有产出物，gate 允许 loop 继续让 LLM 生成文档。文档生成后 post-loop doc capture 保存产出物。下次进入该阶段时 `hasOutput=true`，gate 正常拦截。

5. **潜在性能问题（非 bug）**: cross-type replacement 路径中 UIC 被调用 3 次（handleActiveWorkflow 的 cross-type 检测 + handleNeedsUnderstanding 的 UIC 分类 + 第二次 handleActiveWorkflow 的 cross-type 检测）。每次 UIC 调用耗时 2+ 秒。但这是一次性成本（仅在 cross-type replacement 时发生），不值得为此改变函数签名传递缓存结果。

### Fix

**两个修复**：

#### Fix 1: 重新编译 gui.exe
当前源码已包含所有必要的修复：
- #77: `handlePostStartWorkflow` 确保工作流启动后自动触发 agent loop
- #74: `finish_reason=length` 纯文本续写防止文档截断
- #33: 前序阶段产出物截断防止 LLM 重复输出

#### Fix 2 (新发现): `finish_reason=length` 续写后 `resp.Text` 只包含最后一个 chunk

**根因**: `finish_reason=length` 续写机制在 loop 内 `continue` 时，下一次迭代的 `msgContent` 只包含新 chunk 的内容。当 loop 最终结束时，`resp.Text = stripThinkingTags(msgContent)` 只包含最后一个 chunk，丢失了前面所有 chunk 的内容。

**影响**:
- `SavePhaseOutput` 只保存最后一个 chunk 作为阶段产出物
- 用户确认后 `advancePhase`，下一阶段的 `BuildPhaseSystemPrompt` 注入的"前序阶段产出物"是不完整的
- doc preview 面板只显示最后一个 chunk

**修复**: 新增 `lengthContinuationBuf` 累积器，在 `finish_reason=length` 续写时累积所有 chunk。所有构造 `IMAgentResponse` 的路径（no-tool 分支、NeedsConfirm gate、hard cap、tool branch NeedsConfirm）都使用累积后的完整文本。

**修改文件**: `gui/im_message_handler.go`
- 在 iteration loop 前声明 `var lengthContinuationBuf strings.Builder`
- `finish_reason=length` 续写时 `lengthContinuationBuf.WriteString(msgContent)` 累积当前 chunk
- 4 个 `IMAgentResponse` 构造点：检查 `lengthContinuationBuf.Len() > 0`，有累积内容时用 `lengthContinuationBuf.String() + msgContent` 拼接完整文本（不写入 buffer，避免 fall-through 路径重复追加）

### Optimize

**LLM 响应慢不是 maclaw 的 bug**，但可以改善用户体验：
- 智谱编程 glm-5.1 对 16K input + 4.7K output 的请求耗时 5.5 分钟
- 建议用户考虑切换到响应更快的 LLM 服务商（如 Anthropic Claude、OpenAI GPT-4o）
- 或者在 maclaw 的 LLM 配置中调整 `max_output_tokens` 参数，限制单次输出长度，让 `finish_reason=length` 续写机制分多次生成

## 六、验证步骤

1. 重新编译 gui.exe：`go build -o gui.exe ./gui/`（或使用项目的 build 脚本）
2. 启动新的编码工作流
3. 验证：发送编码任务后，面板自动显示需求文档（不需要手动发送"继续"）
4. 验证：确认需求后，自动进入技术设计阶段并生成文档
5. 验证：技术设计文档完整（不被截断，`finish_reason=length` 续写生效）
6. 验证：确认技术设计后，自动进入任务分解阶段
