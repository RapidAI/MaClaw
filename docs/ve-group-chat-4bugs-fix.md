# VE 群聊 4 个 Bug 机制性修复方案

## 问题总览

| # | 问题 | 根因 | 影响 |
|---|------|------|------|
| 1 | 添加本机AI后历史对话消失 | Tab 类型切换导致不同组件类型渲染，React reconciliation 卸载 VEConversationView | 用户丢失对话上下文 |
| 2 | 消息没有标注回复者 | `VEMessage` 数据模型缺少 `senderName` 字段，`MessageBubble` 不渲染发送者 | 群聊中无法区分谁在说话 |
| 3 | 输入 @ 后没有参与者列表 | `mentionableParticipants` 错误过滤掉 `local-maclaw`；1:1 VE tab 无 participants prop | 无法 @mention 指定参与者 |
| 4 | @本机AI 后本机AI没反应 | `SendVEMessage` 无 @mention 路由——消息只发给远程VE或只发给本机AI，无定向能力 | 用户无法定向对话 |

## 机制性根因分析

### 核心架构缺陷：群聊的数据模型和路由模型仍然是 1:1 对话的简单扩展

4 个 bug 的共同根因是缺少群聊必需的三个机制：
1. **消息归属**（谁发的）——VEMessage 缺少 senderName/senderId
2. **消息寻址**（发给谁）——SendVEMessage 缺少 @mention 路由
3. **状态连续性**（tab 类型切换时保持状态）——不同组件类型导致 React 卸载

---

## 修复方案

### Fix 1: 统一组件类型——React reconciliation 不卸载 VEConversationView

**根因**：`AssistantActiveTabContent` 中 VE tab 渲染 `<VETabWrapper>`，group tab 渲染 `<LiveGroupTabWrapper>`。React reconciliation 规则：**相同 key + 不同组件类型 = 卸载旧 + 挂载新**。Tab type 从 "ve" 变为 "group" 时，VEConversationView 必然被卸载重挂载。

**Review Round 2 发现的深层问题**：即使统一为一个组件，如果 render 返回值的**根元素类型**在 `isGroupMode` 切换时变化（bare `<VEConversationView>` vs `<div>` wrapper），React 仍然会卸载。

**最终修复**：`UnifiedVEGroupWrapper` 始终返回**相同的 DOM 结构**：
```
<div flex-row>
  <div flex-1>
    <VEConversationView ... />   ← 位置永远不变
  </div>
  {isGroupMode && <GroupParticipantPanel />}  ← 条件渲染，不影响 VEConversationView 位置
</div>
```

VEConversationView 在 React 树中的位置（第一个 div → 第一个 div → 第一个子元素）永远不变。无论 `isGroupMode` 如何切换，React 都不会卸载它。

### Fix 2: VEMessage 新增 senderName + MessageBubble 渲染发送者标签

**修复**：
- `VEMessage` 新增 `senderName?: string` 和 `senderId?: string` 字段
- `MessageBubble` 在 assistant 消息有 `senderName` 时，在气泡上方显示灰色小字标签
- 流式输出期间也显示发送者名称
- `VEConversationState` 新增 `_streamSenderName` / `_streamSenderId` 内部字段
- 后端 `emitStreamToFrontend` 事件 payload 新增 `sender_name: "本机AI"` 和 `sender_id: "local-maclaw"`

### Fix 3: @mention 参与者列表——所有参与者可被 mention

**根因**：`mentionableParticipants` 过滤掉 `"local-maclaw"`，理由是"不能 @mention 自己"。但在群聊中，用户（人类操作员）不是 `local-maclaw`——`local-maclaw` 是本机AI 参与者，应该可以被 @mention。

**修复**：`mentionableParticipants` 直接等于 `participants`，不做任何过滤。所有参与者（安娜 + 本机AI）都可被 @mention。

### Fix 4: SendVEGroupMessage @mention 路由 + 单响应者不变量

**根因**：`SendVEMessage` 无 @mention 路由。

**Review Round 3 发现的深层问题**：如果 broadcast 路径同时发给本机AI和远程VE，两者都会响应，用户收到重复/矛盾的回复。原始 `SendVEMessage` 的设计是"单响应者"——`tryLocalExecutorDispatch` 是短路机制，成功则不发给 Hub。

**最终修复**：新增 `SendVEGroupMessage(sessionID, content, mentionedIds)` binding，路由语义：

| 场景 | 路由 | 响应者 |
|------|------|--------|
| 无 @mention | 优先本机AI，fallback 远程VE | 只有一个（单响应者不变量） |
| @本机AI | 只发给本机 dispatcher，不发 Hub | 本机AI |
| @远程VE | 只发给 Hub，不触发本机 dispatcher | 远程VE |
| @两者 | 同"无 @mention"（优先本机AI） | 只有一个 |

---

## 修改文件清单

| 文件 | 修改内容 |
|------|---------|
| `gui/frontend/src/components/ai/AssistantActiveTabContent.tsx` | 删除 `VETabWrapper` + `LiveGroupTabWrapper`，统一为 `UnifiedVEGroupWrapper`。始终返回相同 DOM 结构，VEConversationView 位置不变 |
| `gui/frontend/src/components/ai/VEConversationView.tsx` | VEMessage 新增 senderName/senderId；MessageBubble 渲染发送者标签；mentionableParticipants 不过滤；doSendMessage 提取 mentionedIds 调用 SendVEGroupMessage；stream 事件捕获 sender_name |
| `gui/frontend/src/components/ai/useAITabManager.ts` | upgradeVETabToGroup 注释更新 |
| `gui/app_ve.go` | 新增 SendVEGroupMessage binding，@mention 路由 + 单响应者不变量 |
| `gui/ve_group_dispatcher.go` | emitStreamToFrontend 事件 payload 新增 sender_name/sender_id |

---

## Review 过程中发现并修复的机制性问题

| 轮次 | 问题 | 根因 | 修复 |
|------|------|------|------|
| R1 | 初版用两个不同组件类型 | React: 相同 key + 不同类型 = 卸载 | 统一为 UnifiedVEGroupWrapper |
| R2 | 统一组件内条件返回不同根元素 | React: 根元素类型变化 = 卸载子树 | 始终返回相同 DOM 结构，panel 条件渲染 |
| R3 | broadcast 同时发给两者 | 打破单响应者不变量 | 恢复优先级短路语义 |
| R3 | @本机AI 仍发 Hub | Hub 转发给远程VE导致双响应 | @本机AI 不发 Hub |
| R4 | `useMemo(() => participants, [participants])` 无意义 | 返回输入本身的 memo 是 no-op | 简化为 `const x = participants` |

---

## 验收标准

1. **历史不丢失**：添加本机AI后，之前的对话消息仍然可见（VEConversationView 不卸载）
2. **发送者标签**：群聊中每条 assistant 消息上方显示发送者名称（"安娜" / "本机AI"）
3. **@mention 列表**：输入 @ 后弹出参与者选择列表，包含所有参与者（安娜 + 本机AI）
4. **@mention 路由**：@本机AI 时只有本机AI响应；@安娜 时只有安娜响应；无 @ 时按优先级只有一个响应
5. **向后兼容**：1:1 VE 对话（未添加本机AI）行为不变
6. **单响应者不变量**：每条消息只有一个 AI 响应，用户不会收到重复/矛盾的回复
