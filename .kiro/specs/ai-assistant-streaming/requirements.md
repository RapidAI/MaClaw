# AI 助手 Streaming 响应优化

## 背景

MaClaw GUI 的 AI 助手当前采用同步请求-响应模式：前端 `await SendAIAssistantMessage(text)` 阻塞等待整个 agent loop（可能包含多轮 LLM 调用 + 工具执行）完成后才返回结果。用户在等待期间只能看到"正在思考..."，体感延迟严重。

## 需求

### 1. 全轮次 LLM Streaming

- agent loop 中每一轮 LLM 调用都应支持 SSE streaming
- LLM 生成的文本 token 应实时推送到前端，用户可以看到文字逐步出现
- 支持 OpenAI 协议（`/chat/completions` with `stream: true`）和 Anthropic 协议（`/v1/messages` with `stream: true`）
- tool_calls 的 streaming delta 需要在后端拼接完整后再执行工具

### 2. 前端实时渲染

- 前端监听 token 事件，实时追加到当前 assistant 消息
- 每轮 LLM 生成时创建新的 assistant 消息气泡，token 追加到该气泡
- 工具执行阶段显示工具状态（已有 progress 事件机制）
- 最终 `SendAIAssistantMessage` 返回后，用完整响应替换/确认最后一条消息（处理 fields/actions/files 等结构化数据）

### 3. Fallback 兼容

- 如果 LLM provider 不支持 streaming（返回非 SSE 格式或报错），自动 fallback 到当前的一次性读取模式
- 不影响现有的 IM 平台（飞书/QQ/Telegram）消息处理 — streaming 仅用于 desktop 平台

### 4. 附带优化

- `ensureRemoteInfra()` 使用 `sync/atomic` 做快速路径检查，减少每次消息的锁开销
