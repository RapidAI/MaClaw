# 技术设计：Project Tab 多实例架构

## 核心问题

当前 `useAIAssistant` 是单例 hook——一个 `messages` 状态、一个 `activeRound`、一个事件监听器。所有 tab 共享这个实例。Project tab 的消息写入全局 `messages`，响应通过全局事件到达，切换 tab 后消息在错误的 tab 中显示。

## 设计原则

每个 project tab = 一个独立的 agent 实例。独立的：
- 对话历史（messages）
- 请求状态（sending/streaming/activeRound）
- 事件通道（token/progress/response 按 session 路由）
- 工具执行上下文（workDir = projectPath）

完成后经验沉淀到长期记忆，供跨项目复用。

## 架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│  AIAssistantPanel                                               │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  AITabBar                                                 │  │
│  │  [AI 助手] [北京天气 ×] [C++游戏 ×] [▼ 更多]             │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  Tab Content (条件渲染，只渲染 activeTab)                  │  │
│  │                                                           │  │
│  │  activeTab.type === "local":                              │  │
│  │    <LocalTabView instance={localInstance} />              │  │
│  │                                                           │  │
│  │  activeTab.type === "project":                            │  │
│  │    <ProjectTabView instance={projectInstances[tabId]} />  │  │
│  │                                                           │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  InputStack (共享 UI，绑定到 activeInstance)               │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## 关键设计决策

### 决策 1: 事件路由——后端事件携带 sessionKey

**问题**：后端通过全局事件（`ai-assistant-token`、`ai-assistant-response`）推送数据，前端无法区分属于哪个 tab。

**方案**：后端事件 payload 新增 `session_key` 字段。前端按 `session_key` 路由到对应实例。

```go
// 后端 emitAIAssistantToken / emitAIAssistantResponse
type streamEventPayload struct {
    SessionKey string `json:"session_key,omitempty"` // "" = local, "desktop-user:{path}" = project
    RequestID  string `json:"request_id"`
    Text       string `json:"text,omitempty"`
    // ... 其他字段
}
```

- `session_key == ""` 或 `session_key == "desktop-user"` → 路由到 local instance
- `session_key == "desktop-user:{projectPath}"` → 路由到对应 project instance

**后端改动**：`runAIAssistantMessageAsync` 中的所有 `emitEvent` 调用传入 `userID` 作为 `session_key`。

### 决策 2: 前端多实例管理——useAISession hook

将 `useAIAssistant`（3000+ 行）拆分为：

1. **`useAISession(sessionKey)`**：单个会话实例的核心逻辑
   - 拥有独立的 `messages`、`sending`、`streaming`、`activeRound`
   - 只监听 `session_key` 匹配的事件
   - `sendMessage` 自动注入 `project_path`（从 sessionKey 解析）

2. **`useAISessionManager()`**：管理多个 session 实例
   - `localSession`: 固定的 local tab session（sessionKey=""）
   - `projectSessions: Map<tabId, session>`：project tab sessions
   - `createSession(tabId, projectPath)` / `destroySession(tabId)`
   - `getActiveSession()`: 返回当前 tab 对应的 session

3. **`useAIAssistant()`**：保留为 facade，委托给 `useAISessionManager`
   - 返回 `activeSession` 的 messages/sending/streaming 等
   - 向后兼容现有的 `AIAssistantPanel` 消费方式

```typescript
// useAISession.ts — 单个会话实例
function useAISession(sessionKey: string) {
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [sending, setSending] = useState(false);
    const activeRoundRef = useRef<ActiveRound>(createIdleRound(0));

    // 只监听匹配 sessionKey 的事件
    useEffect(() => {
        const off = EventsOn("ai-assistant-token", (payload) => {
            const event = parseStreamEvent(payload);
            if (event.session_key !== sessionKey) return; // 路由过滤
            // ... 处理 token
        });
        return off;
    }, [sessionKey]);

    const sendMessage = useCallback(async (text: string) => {
        const projectPath = sessionKey ? sessionKey.replace("desktop-user:", "") : "";
        await SendAIAssistantMessage({ text, project_path: projectPath });
    }, [sessionKey]);

    return { messages, sending, streaming, sendMessage, ... };
}
```

### 决策 3: 实例生命周期

| 事件 | 动作 |
|------|------|
| 应用启动 | 创建 local session（始终存在） |
| 创建 project tab | `createSession(tabId, projectPath)` → 新 session 实例 |
| 切换 tab | 切换 `activeSession` 指针（不销毁实例，保持 streaming） |
| 关闭 project tab | `destroySession(tabId)` → 持久化历史 → 销毁实例 |
| 归档 | 沉淀经验 → 标记只读 → 销毁实例 |

**关键**：切换 tab 不中断 streaming。后台 tab 的 session 继续接收事件并更新 messages，切回时立即看到最新状态。

### 决策 4: 后端并发——chatLoopMu 改为 per-session

**问题**：`chatLoopMu` 是全局 mutex，project tab 的 agent loop 会阻塞 local tab。

**方案**：`chatLoopMu` 改为 per-userID 的 mutex map。

```go
// 从全局 mutex:
chatLoopMu sync.Mutex

// 改为 per-session mutex:
chatLoopMus sync.Map // map[string]*sync.Mutex, key = userID
```

`enterIMMessageSerializationBoundary` 获取 `msg.UserID` 对应的 mutex，不同 session 的 agent loop 可以并发执行。

### 决策 5: 后端事件发射——注入 session_key

所有事件发射点（token/progress/response/new-round/stream-done）注入 `session_key`：

```go
// gui/app_wails_bindings.go
func (a *App) emitSessionEvent(eventName, sessionKey string, payload interface{}) {
    // payload 中注入 session_key
    if m, ok := payload.(map[string]interface{}); ok {
        m["session_key"] = sessionKey
    }
    runtime.EventsEmit(a.ctx, eventName, payload)
}
```

`runAIAssistantMessageAsync` 中的 `onToken`、`onProgress`、`emitAIAssistantResponse` 全部使用 `emitSessionEvent`，传入 `userID` 作为 `session_key`。

### 决策 6: 前端事件分发器

全局事件监听器 → 按 `session_key` 分发到对应 session 实例：

```typescript
// useAIEventDispatcher.ts
function useAIEventDispatcher(sessions: Map<string, SessionInstance>) {
    useEffect(() => {
        const offToken = EventsOn("ai-assistant-token", (raw) => {
            const { session_key, ...event } = parseEvent(raw);
            const target = sessions.get(session_key || "local");
            target?.handleToken(event);
        });
        const offResponse = EventsOn("ai-assistant-response", (raw) => {
            const { session_key, ...event } = parseEvent(raw);
            const target = sessions.get(session_key || "local");
            target?.handleResponse(event);
        });
        // ... progress, new-round, stream-done
        return () => { offToken(); offResponse(); ... };
    }, [sessions]);
}
```

### 决策 7: 经验沉淀（归档）

归档时从 project session 的对话历史中提取经验：

```
用户右键 → 归档
  → 后端 ArchiveProject(projectPath):
    1. 收集该 projectPath 的所有 task_artifact + project_knowledge
    2. LLM 生成结构化经验摘要
    3. 保存为 ScopeGlobal 的 project_knowledge（跨项目可召回）
    4. ProjectIndex 标记 archived=true
  → 前端: 关闭 tab + 刷新列表
```

## 实现分阶段

### Phase 1: 后端事件路由（最小改动，解决核心问题）

1. 所有事件 payload 新增 `session_key` 字段
2. `runAIAssistantMessageAsync` 的 `onToken`/`onProgress`/`emitResponse` 注入 `userID`
3. 前端事件监听器按 `session_key` 过滤（不匹配则忽略）
4. `chatLoopMu` 改为 per-userID mutex map

**效果**：project tab 的事件不再污染 local tab。两个 tab 可以并发执行 agent loop。

### Phase 2: 前端多实例

1. 提取 `useAISession` hook（从 `useAIAssistant` 中拆分核心逻辑）
2. `useAISessionManager` 管理多个实例
3. `AIAssistantPanel` 根据 activeTab 切换 activeSession
4. InputStack 绑定到 activeSession 的 sendMessage

**效果**：每个 tab 有独立的 messages/sending/streaming 状态。

### Phase 3: 持久化 + 归档

1. Project session 对话历史持久化到磁盘
2. 重启后恢复 session 状态
3. 归档 → LLM 经验提取 → 长期记忆沉淀

## 修改文件清单

### Phase 1（后端事件路由）

| 文件 | 变更 |
|------|------|
| `gui/app_wails_bindings.go` | `emitSessionEvent` 辅助函数；所有事件注入 `session_key` |
| `gui/im_handler_wiring.go` | `chatLoopMu` → `chatLoopMus sync.Map` |
| `gui/im_entry_serialization.go` | 获取 per-userID mutex |
| `gui/im_message_handler.go` | `onToken`/`onProgress` 回调注入 `session_key` |

### Phase 2（前端多实例）

| 文件 | 变更 |
|------|------|
| `useAISession.ts` | 新文件：单个会话实例 hook |
| `useAISessionManager.ts` | 新文件：多实例管理 |
| `useAIEventDispatcher.ts` | 新文件：全局事件 → per-session 分发 |
| `useAIAssistant.ts` | 重构为 facade，委托给 sessionManager |
| `AIAssistantPanel.tsx` | 移除 baseline/ID-set hack，使用 activeSession |

### Phase 3（持久化 + 归档）

| 文件 | 变更 |
|------|------|
| `gui/project_tab_session_persist.go` | session 持久化 |
| `gui/project_tab_archive.go` | 归档 + LLM 经验提取 |
| `corelib/memory/store.go` | `RecallDynamic` strictProject 模式 |

## 向后兼容

- local tab 行为完全不变（`useAISession("")` = 现有行为）
- IM 通道不受影响（不经过 tab 系统）
- 旧版前端（不发 `session_key`）→ 后端默认路由到 local session
- 事件 payload 中 `session_key` 为 `omitempty`，旧前端忽略该字段
