# AI 助手 Streaming 响应 — 实现任务

## Task 1: OpenAI 协议 Streaming 解析
#[[file:gui/llm_stream.go]]

- [x] 新增 `TokenCallback` 和 `NewRoundCallback` 类型定义
- [x] 实现 `doOpenAILLMRequestStream` 方法：
  - 请求体加 `"stream": true`
  - 逐行读取 SSE `data: {...}` 
  - 解析 `delta.content` → 调用 `onToken`
  - 解析 `delta.tool_calls` → 按 index 拼接 function name 和 arguments
  - `data: [DONE]` 时组装完整 `llmResponse` 返回
  - Content-Type 不是 `text/event-stream` 时 fallback 到非 streaming 解析
- [x] 实现 `doLLMRequestStream` 分发方法

## Task 2: Anthropic 协议 Streaming 解析
#[[file:gui/llm_stream.go]]

- [x] 实现 `doAnthropicLLMRequestStream` 方法：
  - 请求体加 `"stream": true`
  - 解析 `content_block_start` (text / tool_use)
  - 解析 `content_block_delta` (text_delta → onToken, input_json_delta → 拼接 arguments)
  - `message_stop` 时组装完整 `llmResponse` 返回
  - Fallback 同 Task 1
- [x] 在 `doLLMRequestStream` 中接入 Anthropic 分支

## Task 3: runAgentLoop 集成 Streaming
#[[file:gui/im_message_handler.go]]

- [x] 新增 `HandleIMMessageWithProgressAndStream` 方法
- [x] `HandleIMMessageWithProgress` 改为调用新方法并传 nil onToken/onNewRound
- [x] `runAgentLoop` 签名增加 `onToken TokenCallback` 和 `onNewRound NewRoundCallback`
- [x] 每轮 LLM 调用前（iteration > 0）调用 `onNewRound()`
- [x] 将 `doLLMRequest` 替换为 `doLLMRequestStream`
- [x] 后台任务路径传 nil（不 stream）

## Task 4: Wails Binding 层接入
#[[file:gui/app_wails_bindings.go]]

- [x] `SendAIAssistantMessage` 构造 onToken/onNewRound 回调
- [x] 调用 `HandleIMMessageWithProgressAndStream`

## Task 5: 前端 useAIAssistant Streaming 支持
#[[file:gui/frontend/src/components/ai/useAIAssistant.ts]]

- [x] 新增 `streamingMsgIdRef` 追踪当前 streaming 消息
- [x] 监听 `ai-assistant-token` 事件：追加 delta
- [x] 监听 `ai-assistant-new-round` 事件：新建 assistant 消息气泡
- [x] `sendMessage` 发送时创建空 assistant placeholder
- [x] `await` 返回后用完整响应更新结构化字段
- [x] `clearHistory` 重置 streamingMsgIdRef

## Task 6: ensureRemoteInfra atomic 优化
#[[file:gui/app.go]]

- [x] 新增 `remoteInfraReady atomic.Bool`
- [x] `ensureRemoteInfra` 开头 atomic 快速路径
- [x] 初始化完成后 `Store(true)`

## Task 7: 前端空气泡处理
#[[file:gui/frontend/src/components/ai/AIAssistantPanel.tsx]]

- [x] 空 content assistant 消息显示闪烁光标 `▍`
- [x] 注入 `@keyframes blink` CSS
- [x] 自动滚动改为监听 `messages` 整体变化（支持 streaming 内容更新时滚动）
