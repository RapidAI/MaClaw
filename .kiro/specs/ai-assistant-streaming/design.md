# AI 助手 Streaming 响应 — 技术设计

## 架构概览

```
前端 (React/Wails)                    Go 后端
─────────────────                    ────────
sendMessage(text)
  ├─ 添加 user 消息
  ├─ 添加空 assistant 消息 (streaming placeholder)
  ├─ 监听 "ai-assistant-token" 事件 ──────── EventsEmit("ai-assistant-token", delta)
  │   └─ 逐步追加 content                      ↑ 来自 doLLMRequestStream 的 onToken 回调
  ├─ 监听 "ai-assistant-new-round" 事件 ───── EventsEmit("ai-assistant-new-round")
  │   └─ 新建 assistant 消息气泡                ↑ agent loop 新一轮 LLM 调用开始
  └─ await SendAIAssistantMessage(text) ────── HandleIMMessageWithProgress()
      └─ 用完整 IMAgentResponse 替换/确认        └─ runAgentLoop() → doLLMRequestStream()
         最后一条消息（处理 files/actions 等）
```

## 一、Go 后端改动

### 1.1 新增 streaming LLM 请求方法

在 `gui/im_message_handler.go` 中新增：

```go
// TokenCallback 每收到一段 LLM 文本 delta 时调用
type TokenCallback func(delta string)

// doLLMRequestStream 发送 streaming LLM 请求，通过 onToken 实时推送文本 delta。
// 返回完整的 llmResponse（包含拼接好的 content 和 tool_calls）。
// 如果 streaming 失败（provider 不支持等），自动 fallback 到非 streaming 模式。
func (h *IMMessageHandler) doLLMRequestStream(
    cfg MaclawLLMConfig,
    messages []interface{},
    tools []map[string]interface{},
    httpClient *http.Client,
    onToken TokenCallback,
) (*llmResponse, error)
```

内部逻辑：
- 根据 `cfg.Protocol` 分发到 `doOpenAILLMRequestStream` 或 `doAnthropicLLMRequestStream`
- 请求体加 `"stream": true`
- 逐行读取 SSE response body
- 文本 delta → 调用 `onToken(delta)`
- tool_calls delta → 内部拼接 function name 和 arguments
- 读完后组装完整的 `llmResponse` 返回

### 1.2 OpenAI SSE 解析

OpenAI streaming 格式：
```
data: {"choices":[{"delta":{"content":"你好"},"index":0}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_xxx","function":{"name":"bash","arguments":""}}]},"index":0}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]},"index":0}]}
data: [DONE]
```

解析规则：
- `delta.content` 非空 → `onToken(delta.content)`
- `delta.tool_calls` → 按 index 拼接到 `toolCallAccumulators[index]`
- `data: [DONE]` → 结束，组装 `llmResponse`

### 1.3 Anthropic SSE 解析

Anthropic streaming 格式：
```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_xxx","name":"bash","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":"}}

event: message_stop
```

解析规则：
- `content_block_delta` + `text_delta` → `onToken(delta.text)`
- `content_block_start` + `tool_use` → 记录 tool call id/name
- `content_block_delta` + `input_json_delta` → 拼接 arguments
- `message_stop` → 结束，组装 `llmResponse`

### 1.4 Fallback 机制

```go
func (h *IMMessageHandler) doOpenAILLMRequestStream(...) (*llmResponse, error) {
    // 发送 stream: true 请求
    resp, err := httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    
    contentType := resp.Header.Get("Content-Type")
    if !strings.Contains(contentType, "text/event-stream") {
        // Provider 不支持 streaming，fallback 到一次性读取
        return parseNonStreamResponse(resp)
    }
    
    // SSE 解析...
}
```

### 1.5 runAgentLoop 集成

在 `runAgentLoop` 中，将 `doLLMRequest` 替换为 `doLLMRequestStream`：

```go
// 每轮 LLM 调用前，通知前端新一轮开始
if onToken != nil && iteration > 0 {
    onNewRound()  // → EventsEmit("ai-assistant-new-round")
}

resp, err := h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
```

`onToken` 和 `onNewRound` 回调由 `SendAIAssistantMessage` 注入：

```go
func (a *App) SendAIAssistantMessage(text string) (*IMAgentResponse, error) {
    // ...
    onToken := func(delta string) {
        runtime.EventsEmit(a.ctx, "ai-assistant-token", delta)
    }
    onNewRound := func() {
        runtime.EventsEmit(a.ctx, "ai-assistant-new-round")
    }
    // ...
}
```

### 1.6 HandleIMMessageWithProgress 签名扩展

```go
type TokenCallback func(delta string)
type NewRoundCallback func()

func (h *IMMessageHandler) HandleIMMessageWithProgressAndStream(
    msg IMUserMessage,
    onProgress ProgressCallback,
    onToken TokenCallback,
    onNewRound NewRoundCallback,
) *IMAgentResponse
```

保留原 `HandleIMMessageWithProgress` 不变（IM 平台继续用），新方法供 desktop 调用。

## 二、前端改动

### 2.1 useAIAssistant.ts

```typescript
// 新增事件类型
const STREAM_TOKEN_EVENT = "ai-assistant-token";
const NEW_ROUND_EVENT = "ai-assistant-new-round";

export function useAIAssistant() {
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [sending, setSending] = useState(false);
    const sendingRef = useRef(false);
    // 追踪当前 streaming 消息的 ID
    const streamingMsgIdRef = useRef<string | null>(null);

    // 监听 token 事件：追加到当前 streaming 消息
    useEffect(() => {
        const tokenHandler = (delta: string) => {
            const msgId = streamingMsgIdRef.current;
            if (!msgId) return;
            setMessages(prev => prev.map(msg =>
                msg.id === msgId
                    ? { ...msg, content: msg.content + delta }
                    : msg
            ));
        };
        
        const newRoundHandler = () => {
            // 新一轮 LLM 调用：创建新的 assistant 消息气泡
            const newId = nextId();
            streamingMsgIdRef.current = newId;
            const newMsg: ChatMessage = {
                id: newId,
                role: 'assistant',
                content: '',
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, newMsg]);
        };
        
        EventsOn(STREAM_TOKEN_EVENT, tokenHandler);
        EventsOn(NEW_ROUND_EVENT, newRoundHandler);
        return () => {
            EventsOff(STREAM_TOKEN_EVENT);
            EventsOff(NEW_ROUND_EVENT);
        };
    }, []);

    const sendMessage = useCallback(async (text: string) => {
        if (text.trim() === "" || sendingRef.current) return;
        sendingRef.current = true;
        setSending(true);

        // 1. 添加 user 消息
        const userMsg = { id: nextId(), role: 'user', content: text, timestamp: Date.now() };
        
        // 2. 添加空 assistant 消息作为 streaming placeholder
        const streamId = nextId();
        streamingMsgIdRef.current = streamId;
        const placeholderMsg = { id: streamId, role: 'assistant', content: '', timestamp: Date.now() };
        
        setMessages(prev => [...prev, userMsg, placeholderMsg]);

        try {
            const response = await SendAIAssistantMessage(text);
            streamingMsgIdRef.current = null;
            
            // 3. 用完整响应替换最后一条 assistant 消息
            //    如果有 fields/actions/files 等结构化数据，需要更新
            if (response.error) {
                setMessages(prev => {
                    // 找到最后一条 assistant 消息，替换为 error
                    const last = [...prev];
                    const errorMsg = { id: nextId(), role: 'error', content: response.error, timestamp: Date.now() };
                    // 如果 streaming placeholder 是空的，替换它；否则追加 error
                    const lastAssistantIdx = last.findLastIndex(m => m.role === 'assistant');
                    if (lastAssistantIdx >= 0 && last[lastAssistantIdx].content === '') {
                        last[lastAssistantIdx] = errorMsg;
                    } else {
                        last.push(errorMsg);
                    }
                    return last;
                });
            } else {
                // 用完整响应更新最后一条 assistant 消息的结构化字段
                setMessages(prev => {
                    const last = [...prev];
                    const lastAssistantIdx = last.findLastIndex(m => m.role === 'assistant');
                    if (lastAssistantIdx >= 0) {
                        last[lastAssistantIdx] = {
                            ...last[lastAssistantIdx],
                            // 保留 streaming 积累的 content（如果完整响应有 content 则用完整的）
                            content: response.text || last[lastAssistantIdx].content,
                            fields: response.fields,
                            actions: response.actions,
                            localFilePath: response.local_file_path,
                            localFilePaths: response.local_file_paths,
                            thumbnailBase64: response.thumbnail_base64,
                        };
                    }
                    return last;
                });
            }
        } catch (err: any) {
            streamingMsgIdRef.current = null;
            setMessages(prev => [...prev, {
                id: nextId(), role: 'error', content: err?.message || String(err), timestamp: Date.now(),
            }]);
        } finally {
            sendingRef.current = false;
            setSending(false);
        }
    }, []);

    // ... clearHistory, executeAction 不变
}
```

### 2.2 AIAssistantPanel.tsx

基本不需要改动。已有的 `messages` 驱动渲染机制天然支持 streaming — 当 `messages` 中某条消息的 `content` 被 `setMessages` 更新时，React 会自动重新渲染该消息气泡。

唯一可能的小优化：streaming 期间空 content 的 assistant 消息不显示"空气泡"，可以在 `renderMessage` 中加一个判断。

## 三、ensureRemoteInfra atomic 优化

```go
// gui/app.go
type App struct {
    // ...
    remoteInfraReady atomic.Bool  // 新增
}

func (a *App) ensureRemoteInfra() {
    // 超快路径：atomic load，无锁
    if a.remoteInfraReady.Load() {
        return
    }
    // 原有的 check-lock-check 逻辑...
    // 初始化完成后：
    a.remoteInfraReady.Store(true)
}
```

## 四、不影响的部分

- IM 平台（飞书/QQ/Telegram）继续使用 `HandleIMMessageWithProgress`，不走 streaming
- 后台任务（`IsBackground`）不走 streaming
- `doLLMRequest`（非 streaming 版本）保留，供 `compactHistory` 的 summarizer 等内部调用使用
- `ai-assistant-progress` 事件机制保留，工具执行状态继续通过它推送

## 五、事件流时序图

```
用户输入 "帮我检查代码"
│
├─ 前端: 添加 user 消息 + 空 assistant 消息 (id=A1)
├─ 前端: streamingMsgIdRef = A1
├─ 前端: await SendAIAssistantMessage("帮我检查代码")
│
│  Go 后端: runAgentLoop 第 1 轮
│  ├─ doLLMRequestStream(onToken)
│  │  ├─ SSE: delta "我来" → EventsEmit("ai-assistant-token", "我来")
│  │  │  └─ 前端: A1.content = "我来"
│  │  ├─ SSE: delta "看看" → EventsEmit("ai-assistant-token", "看看")
│  │  │  └─ 前端: A1.content = "我来看看"
│  │  ├─ SSE: delta tool_call(bash, "ls -la")
│  │  └─ SSE: [DONE] → 返回 llmResponse (有 tool_calls)
│  ├─ progress: "⚙️ 正在执行: bash"
│  │  └─ 前端: 添加 progress 消息
│  ├─ executeTool("bash", ...)
│  │
│  Go 后端: runAgentLoop 第 2 轮
│  ├─ EventsEmit("ai-assistant-new-round")
│  │  └─ 前端: 新建 assistant 消息 (id=A2), streamingMsgIdRef = A2
│  ├─ doLLMRequestStream(onToken)
│  │  ├─ SSE: delta "检查完了" → token 事件
│  │  │  └─ 前端: A2.content = "检查完了"
│  │  ├─ SSE: delta "，发现..." → token 事件
│  │  │  └─ 前端: A2.content = "检查完了，发现..."
│  │  └─ SSE: [DONE] → 返回 llmResponse (无 tool_calls, 最终回复)
│  │
│  └─ 返回 IMAgentResponse{Text: "检查完了，发现..."}
│
├─ 前端: SendAIAssistantMessage 返回
├─ 前端: 用完整响应更新 A2 的 fields/actions 等
└─ 前端: sending = false
```
