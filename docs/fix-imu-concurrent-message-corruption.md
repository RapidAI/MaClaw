# Fix: 并发消息处理导致 Agent Loop 状态破坏

## 根因

`handleIMMessageWithLoop` 中，`chatLoopMu.Lock()` 放在了工作流拦截、任务上下文决策、确认门控、系统 prompt 构建**之后**。这些代码包含阻塞式 LLM 调用（10-35s）和破坏性操作（清空对话历史），但在 `chatLoopMu` 的保护范围之外并发执行。

用户快速连发两条消息时，两条消息并发进入 IUM 的阻塞式 LLM 调用，后返回的消息在 fall through 时看到空历史（前一条消息的 agent loop 还没保存），触发 `TaskNew` 清空对话历史，破坏正在运行的 agent loop 的上下文。

## 修复

将 interrupt handler + `chatLoopMu.Lock()` 从原位置（系统 prompt 构建之后）移到工作流拦截之前。

## Review/Fix/Optimize

### Review 发现 #1：后台任务阻塞（已修复）

初版缺少 `!msg.IsBackground` 条件。后台任务有自己的 `bgManager.SpawnOrQueue` 串行化，不应获取 `chatLoopMu`。

### Review 发现 #2：`EntriesBeforeClear` 过早加载（已修复）

`EntriesBeforeClear` 在锁之前加载。当消息 B 在锁上等待消息 A 的 agent loop 完成时，快照不包含消息 A 的结果。`resolveTaskContext` 看到空历史 → `TaskNew` → 清空历史。

**修复**：在 `chatLoopMu.Lock()` 之后立即重新加载 `EntriesBeforeClear` 和 `unfinishedSlot`。此时前一个 loop 已完成并保存了历史，重新加载得到的是最新状态。

### 不需要修复的项

- **`decision` 不需要重新计算**：`resolveExplicitTaskSlotDecision` 读取 `msg` 字段（per-message，非共享状态）。`DismissSlotID`/`ResumeSlotID` 来自前端按钮点击，是幂等操作。
- **`ConsumeInFlightTask` 可以在锁外**：一次性消费，内部有锁，两条并发消息只有一条能消费成功。
- **`sessionStartExtractor.MaybeExtractAsync` 可以在锁外**：异步操作，不影响后续逻辑。

## 最终修改

`gui/im_message_handler.go`：
1. 在工作流拦截之前插入 interrupt handler + `chatLoopMu.Lock()`，条件 `providedLoopCtx == nil && !msg.IsBackground`
2. 锁获取后立即重新加载 `EntriesBeforeClear` 和 `unfinishedSlot`
3. 删除原位置的 interrupt handler + lock 块，替换为注释
