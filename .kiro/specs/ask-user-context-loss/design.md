# Ask-User Context Loss Bugfix Design

## Overview

`ask_user` 工具返回时 agent loop 提前退出，但未保存 conversation history，导致下一条消息丢失上下文。修复分两层：
1. **History 持久化**：ask_user 返回前保存完整 history
2. **Pending 状态追踪**：标记 ask_user 等待状态，下一条消息注入上下文提示

## Glossary

- **ask_user return path**: `gui/im_message_handler.go` 中 `ParseAskUserResult(result)` 成功后的 `return resp` 路径
- **conversationMemory**: `h.memory`，按 userID 存储对话历史的内存结构
- **pending ask_user**: ask_user 返回后、用户回答前的中间状态
- **history**: `runAgentLoop` 中的 `[]conversationEntry` 局部变量，包含本轮累积的完整对话

## Bug Details

### Root Cause

`gui/im_message_handler.go` 的 `runAgentLoop` 函数中，ask_user 检测到 `__ASK_USER__` 标记后直接 `return resp`（约第 4608 行），跳过了所有 `saveConversationHistoryTimed` 调用点。对比其他返回路径（正常结束、max iterations、capability gap 等），每个路径都在 return 前调用了 `saveConversationHistoryTimed`。

```go
// 当前代码（缺陷）
if askReq, ok := ParseAskUserResult(result); ok {
    // ... 构造 resp ...
    return resp  // ← 没有 saveConversationHistoryTimed
}
```

### Impact

1. 需求文档（可能数千字）在 ask_user 返回后从 memory 中消失
2. 用户的任何非按钮回复都会被当作新请求
3. 编程工作流的三阶段流程在需求确认环节断裂

## Fix Implementation

### Change 1: ask_user 返回前保存 history

**File**: `gui/im_message_handler.go`

在 ask_user 的 `return resp` 前，添加 history 保存。需要将 LLM 的 assistant 消息和 ask_user 的 tool result 都追加到 history 中再保存：

```go
if askReq, ok := ParseAskUserResult(result); ok {
    displayText := FormatAskUserForDisplay(askReq)
    toolResults = append(toolResults, fmt.Sprintf("用户被提问: %s（等待回答）", askReq.Question))
    recordToolResult(tc.ID, toolResults[len(toolResults)-1])

    // --- FIX: 保存 history，包含本轮 LLM 输出和 ask_user 工具结果 ---
    // 追加当前 assistant 消息（含 tool_calls）到 history
    // 注意：assistant 消息可能已在前面的迭代中追加，需要检查
    // 追加 tool result 到 history
    conversation = append(conversation, map[string]interface{}{
        "role":         "tool",
        "tool_call_id": tc.ID,
        "content":      toolResults[len(toolResults)-1],
    })
    history = append(history, conversationEntry{
        Role: "tool", Content: toolResults[len(toolResults)-1], ToolCallID: tc.ID,
    })
    h.saveConversationHistoryTimed(userID, history, nil)
    // --- END FIX ---

    resp := &IMAgentResponse{
        Text:           displayText,
        ResponseSource: "ask_user",
    }
    // ... actions ...
    return resp
}
```

### Change 2: Pending ask_user 状态追踪

**File**: `gui/im_message_handler.go`

在 `IMMessageHandler` 结构体中新增 `pendingAskUser sync.Map`（key: userID, value: `*pendingAskUserState`）。

```go
type pendingAskUserState struct {
    Question  string
    Options   []string
    InputType string
    Timestamp time.Time
}
```

在 ask_user 返回前设置 pending 状态：
```go
h.pendingAskUser.Store(userID, &pendingAskUserState{
    Question:  askReq.Question,
    Options:   askReq.Options,
    InputType: askReq.InputType,
    Timestamp: time.Now(),
})
```

### Change 3: 下一条消息处理时消费 pending 状态

**File**: `gui/im_message_handler.go`

在 `handleIMMessageWithLoop` 的 topic detection 之前，检查并消费 pending ask_user 状态：

```go
// --- ask_user 回答检测 ---
var askUserContext string
if raw, ok := h.pendingAskUser.LoadAndDelete(msg.UserID); ok {
    pending := raw.(*pendingAskUserState)
    // 超时保护：超过 30 分钟的 pending 状态视为过期
    if time.Since(pending.Timestamp) < 30*time.Minute {
        askUserContext = fmt.Sprintf(
            "【上下文提示】用户正在回答之前的确认问题。\n问题：%s\n用户的回答：%s\n请将此视为对当前任务的补充或修改意见，而非全新的请求。",
            pending.Question, trimmed,
        )
    }
}
```

### Change 4: 跳过 topic detection

当存在 `askUserContext` 时，跳过 topic switch detection：

```go
if !msg.IsBackground && h.topicDetector != nil && len(EntriesBeforeClear) > 0 &&
    !hasIncompleteTaskMarker(EntriesBeforeClear) &&
    decision.ResumeSlotID == "" && !decision.StartNewTask &&
    askUserContext == "" {  // ← 新增条件：有 pending ask_user 时跳过
    if h.topicDetector.detect(trimmed, msg.UserID, h.memory) == TopicNew {
        // ...
    }
}
```

### Change 5: 注入 ask_user 上下文到 system prompt

在 system prompt 构建后、agent loop 启动前，注入 ask_user 上下文：

```go
if askUserContext != "" {
    systemPrompt += "\n\n" + askUserContext
}
```

### Change 6: Pending 状态过期清理

在 `handleExitCommand`、`cancelWorkflowForUser`、`/new` 命令处理中清除 pending 状态：

```go
h.pendingAskUser.Delete(userID)
```

## Testing Strategy

### Unit Tests

1. **History 保存验证**：mock `conversationMemory`，调用 ask_user 路径后验证 `save` 被调用且 history 包含需求文档和 ask_user 结果
2. **Pending 状态设置/消费**：验证 ask_user 返回后 pending 状态被设置，下一条消息处理后被清除
3. **Topic detection 跳过**：验证有 pending ask_user 时 topic detector 不被调用
4. **System prompt 注入**：验证 askUserContext 被正确注入到 system prompt
5. **超时过期**：验证超过 30 分钟的 pending 状态被忽略
6. **多用户隔离**：验证不同 userID 的 pending 状态互不干扰

### Integration Tests

1. 模拟完整流程：需求文档生成 → ask_user → 用户输入补充需求 → 验证 LLM 收到完整上下文
2. 模拟按钮点击路径不受影响
3. 模拟 /new 命令清除 pending 状态

## Affected Files

- `gui/im_message_handler.go` — 主要修改文件（history 保存、pending 状态、topic detection 跳过、system prompt 注入）
- `gui/im_tool_ask_user.go` — 可能需要导出 `AskUserRequest` 的字段供 pending 状态使用（已导出）
