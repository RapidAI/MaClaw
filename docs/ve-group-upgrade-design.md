# VE 对话升级群聊 + 本机 maclaw 参与设计

## 概述

在与数字员工 1v1 对话时，用户可以通过 tab 右键菜单：
1. 邀请其他数字员工加入，将对话升级为群聊
2. 将本机 maclaw（AI 助手）加入会话，作为"执行者"参与

本机 maclaw 是群聊中唯一有完整工具能力的参与者，负责执行本地操作（写文件、运行命令等）。远程数字员工提供知识和建议。人类用户提出问题，轻度参与。

## 角色模型

| 角色 | role_code | 工具能力 | 职责 |
|------|-----------|---------|------|
| 人类用户 | `initiator` | 无 | 提出问题、确认方向 |
| 本机 maclaw | `executor` | 完整（bash/write_file/ssh/...） | 执行本地操作、生成文档 |
| 远程数字员工 | `speak` | 只读（read_file/list_directory） | 提供知识、建议、分析 |

## 数据流

```
┌─────────────────────────────────────────────────────────┐
│                    本机 Maclaw Client                      │
│                                                           │
│  ┌──────────┐    ┌──────────────┐    ┌────────────────┐  │
│  │ Tab Bar  │    │ Group Chat   │    │ IMMessageHandler│  │
│  │ (右键菜单)│───▶│ View         │    │ (executor 角色) │  │
│  └──────────┘    └──────┬───────┘    └───────▲────────┘  │
│                         │                     │           │
│                         │ 用户消息             │ 群聊消息   │
│                         ▼                     │           │
│                  ┌──────────────┐             │           │
│                  │ GroupChat    │─────────────┘           │
│                  │ Dispatcher   │                         │
│                  └──────┬───────┘                         │
│                         │                                 │
└─────────────────────────┼─────────────────────────────────┘
                          │ A2A (via Hub)
                          ▼
┌─────────────────────────────────────────────────────────┐
│                         Hub                               │
│  ┌──────────────────────────────────────────────────┐    │
│  │ GroupDiscussionService                            │    │
│  │ - AddParticipant(sessionID, participant)          │    │
│  │ - BroadcastMessage(sessionID, msg)               │    │
│  │ - max_group_participants 限制                     │    │
│  └──────────────────────────────────────────────────┘    │
└─────────────────────────┬────────────────────────────────┘
                          │ A2A
                          ▼
┌─────────────────────────────────────────────────────────┐
│              远程 Maclaw Client (VE 所有者)                │
│  ┌──────────────────┐                                    │
│  │ VEMessageHandler │ ← 收到群聊消息，用 veAgentCallbacks  │
│  │ (speak 角色)      │   回复（只读工具）                   │
│  └──────────────────┘                                    │
└─────────────────────────────────────────────────────────┘
```

## 核心机制

### 1. Tab 升级（ve → group）

当用户从 1v1 VE tab 添加参与者时，tab 类型从 `"ve"` 升级为 `"group"`：

```typescript
// useAITabManager.ts
upgradeVETabToGroup(tabId: string, newParticipants: string[]): AITab | null {
    const prev = tabStateRef.current;
    const tab = prev.tabs.find(t => t.id === tabId);
    if (!tab || tab.type !== "ve") return null;

    const upgraded: AITab = {
        ...tab,
        type: "group",
        participants: [tab.veId!, ...newParticipants],
        discussionId: getTabState(tabId)?.sessionId,
    };
    // 保留 tab ID 不变，避免丢失对话历史
    updateTabState(() => ({
        ...prev,
        tabs: prev.tabs.map(t => t.id === tabId ? upgraded : t),
    }));
    return upgraded;
}
```

### 2. 本机 maclaw 参与机制（GroupChat Dispatcher）

本机 maclaw 加入群聊后，需要一个 **dispatcher** 将群聊消息路由到主 agent：

```go
// gui/ve_group_dispatcher.go

// GroupChatDispatcher routes group chat messages to the local maclaw agent
// when it's participating as an executor in a VE group discussion.
type GroupChatDispatcher struct {
    app            *App
    activeSessions sync.Map // sessionID → *groupExecutorSession
}

type groupExecutorSession struct {
    SessionID string
    Cancel    context.CancelFunc
}

// HandleGroupMessage is called when a message arrives in a group where
// local maclaw is an executor participant. It routes the message through
// IMMessageHandler with platform="ve_group_executor".
func (d *GroupChatDispatcher) HandleGroupMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
    // Skip own messages (from this machine)
    if msg.FromID == d.app.getMachineID() {
        return
    }
    // Skip stream chunks (wait for complete messages)
    if msg.Kind == a2a.MessageStreamChunk || msg.Kind == a2a.MessageStreamEnd {
        return
    }

    // Route to main agent as a group context message
    hubClient := d.app.hubClient()
    if hubClient == nil {
        return
    }
    handler := hubClient.ensureIMHandler()
    if handler == nil {
        return
    }

    // Construct message with group context
    imMsg := IMUserMessage{
        UserID:   fmt.Sprintf("ve-group:%s", sessionID),
        Platform: "ve_group_executor",
        Text:     msg.Content,
        Lang:     "zh",
    }

    // Run async to not block the WebSocket event loop
    go func() {
        resp := handler.HandleIMMessageWithProgressAndStream(imMsg, nil, func(chunk string) {
            // Stream response back to group via A2A
            d.sendToGroup(sessionID, a2a.GroupDiscussionMessage{
                Kind:    a2a.MessageStreamChunk,
                Content: chunk,
            })
        }, nil, nil)

        if resp != nil && resp.Text != "" {
            d.sendToGroup(sessionID, a2a.GroupDiscussionMessage{
                Kind:    a2a.MessageStreamEnd,
                Content: "",
            })
        }
    }()
}
```

**注意**：这里复用主 agent 的 `IMMessageHandler` 是合理的——因为本机 maclaw 作为 executor 需要完整工具能力，且它运行在本机（不是远程），安全边界与桌面用户相同。用 `platform="ve_group_executor"` 标识来源，但不做工具过滤。

### 3. 回复协调（Turn-Taking）

多个 AI 同时回复会导致消息混乱。协调策略：

**方案：@mention 触发**
- 群聊中，只有被 @mention 的参与者回复
- 人类发消息时默认 @所有人（或 @最近活跃的参与者）
- AI 参与者回复时可以 @其他参与者请求协助
- 本机 maclaw 在收到 @executor 或需要执行操作时才回复

**实现**：在 `GroupDiscussionMessage` 中已有 `mentions` 字段（或通过 content 中的 `@name` 解析）。Dispatcher 检查消息是否 mention 了本机 maclaw，只有被 mention 时才触发 agent loop。

```go
func (d *GroupChatDispatcher) shouldRespond(msg a2a.GroupDiscussionMessage) bool {
    // Always respond if explicitly mentioned
    if containsMention(msg.Content, d.app.getLocalExecutorName()) {
        return true
    }
    // Respond if message contains action keywords (需要执行/帮我做/生成文件/...)
    if containsActionKeywords(msg.Content) {
        return true
    }
    // Don't respond to pure discussion between other VEs
    return false
}
```

### 4. Hub API：动态添加参与者

```
POST /runtime/a2a/sessions/{sessionID}/participants
Body: { "id": "<machine_id>", "role_code": "executor", "name": "本机 AI 助手" }
Response: { "success": true, "participant_count": 3 }

约束：participant_count <= max_group_participants (Hub 配置)
```

如果 Hub 已有此 API（`GroupDiscussionAddParticipant`），直接复用。

### 5. 前端：Tab 右键菜单

```typescript
// AITabBar.tsx 中 tab 右键菜单
{tab.type === "ve" && (
    <>
        <MenuItem onClick={() => onInviteVE(tab)}>
            邀请数字员工
        </MenuItem>
        <MenuItem onClick={() => onAddLocalMaclaw(tab)}>
            添加本机 AI 助手
        </MenuItem>
    </>
)}
{tab.type === "group" && !tab.readOnly && (
    <MenuItem onClick={() => onInviteVE(tab)}>
        邀请数字员工
    </MenuItem>
)}
```

### 6. System Prompt 注入（executor 模式）

当本机 maclaw 以 executor 身份参与群聊时，system prompt 需要额外注入群聊上下文：

```
你正在参与一个数字员工群聊讨论。
你的角色是「执行者」——负责执行本地操作（读写文件、运行命令、生成文档等）。
其他参与者是远程数字员工，他们只能提供建议和知识，不能执行操作。
人类用户是问题的提出者。

当前参与者：
- 用户（发起者）
- 安娜（远程数字员工，专长：法律研究）
- 本机 AI 助手（你，执行者）

协作规则：
- 当需要执行操作时（写文件、运行命令等），由你负责
- 当需要专业知识时，等待远程数字员工的建议
- 不要重复其他参与者已经说过的内容
- 如果被 @mention 或消息中包含操作请求，才回复
```

## 实现阶段

### Phase 1: Tab 右键菜单 + 邀请 VE（前端）
- `AITabBar.tsx` 新增 tab 右键菜单
- 邀请 VE 弹出 `ParticipantSelector`（已有组件）
- 调用 Hub API 添加参与者
- Tab 类型升级 ve → group

### Phase 2: 本机 maclaw 加入（前端 + 后端）
- "添加本机 AI 助手" 菜单项
- 后端 `GroupChatDispatcher` 实现
- 注册到 Hub session 作为 executor 参与者
- 群聊消息路由到主 agent

### Phase 3: 回复协调（后端）
- @mention 检测
- Action keyword 检测
- 避免多 AI 同时回复的冲突

### Phase 4: System Prompt 群聊上下文注入（后端）
- `platform="ve_group_executor"` 的专用 system prompt 段
- 参与者列表注入
- 协作规则注入

## 约束与边界

- 最大参与者数：Hub 配置的 `max_group_participants`（默认 5，最大 10）
- 本机 maclaw 只能加入一次（不能重复添加）
- 升级为群聊后不能降级回 1v1
- 只读历史 tab 不能添加参与者
- 本机 maclaw 作为 executor 使用主 agent 的完整工具集（不做安全过滤——它运行在本机，安全边界与桌面用户相同）

## 与现有机制的关系

| 机制 | 关系 |
|------|------|
| `VEMessageHandler` | 不变——处理远程 VE 收到的消息 |
| `IMMessageHandler` | 新增 `ve_group_executor` platform 支持 |
| `VEGroupChatView` | 已有群聊 UI，复用 |
| `ParticipantSelector` | 已有组件，复用 |
| `useAITabManager` | 新增 `upgradeVETabToGroup` 方法 |
| `AITabBar` | 新增 tab 右键菜单 |
| Hub `GroupDiscussionService` | 复用 `AddParticipant` API |
