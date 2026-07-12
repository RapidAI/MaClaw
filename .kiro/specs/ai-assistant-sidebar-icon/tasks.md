# 实现计划：AI 助手侧边栏图标

## 概述

在 MaClaw 桌面应用左侧导航栏新增 AI 助手图标入口，点击后弹出全屏终端风格对话界面，复用后端 `IMMessageHandler` 的 Agent 能力。实现分为后端 Wails Binding、前端 Hook/组件、侧边栏集成三个阶段，逐步构建并在每个阶段验证功能。

## 任务

- [x] 1. 后端 Wails Binding 实现
  - [x] 1.1 实现 `SendAIAssistantMessage` 和 `ClearAIAssistantHistory` Wails Binding
    - 在 `app_wails_bindings.go` 中新增两个方法
    - `SendAIAssistantMessage(text string) (*IMAgentResponse, error)`：构造 `IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: text}`，调用 `hubClient().imHandler.HandleIMMessageWithProgress(msg, onProgress)`
    - `ClearAIAssistantHistory() error`：调用 `hubClient().imHandler.memory.clear("desktop-user")`
    - `onProgress` 回调通过 `runtime.EventsEmit(a.ctx, "ai-assistant-progress", progressText)` 推送进度
    - 处理 `hubClient()` 或 `imHandler` 为 nil 的错误情况
    - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 5.1_

  - [x]* 1.2 编写后端属性测试：消息构造与平台标识
    - **Property 4: 后端消息构造与平台标识**
    - 验证 `SendAIAssistantMessage` 构造的 `IMUserMessage` 中 `UserID == "desktop-user"` 且 `Platform == "desktop"`
    - **验证: 需求 3.1, 3.2**

  - [x]* 1.3 编写后端属性测试：桌面端对话记忆隔离
    - **Property 5: 桌面端对话记忆隔离**
    - 验证桌面端（key="desktop-user"）和 IM 端用户的对话记忆互不干扰
    - **验证: 需求 3.3**

  - [x]* 1.4 编写后端属性测试：错误响应传播
    - **Property 6: 错误响应传播**
    - 验证当 LLM 未配置时，返回的 `IMAgentResponse.Error` 非空
    - **验证: 需求 3.4**

  - [x]* 1.5 编写后端属性测试：清空历史清除记忆
    - **Property 7: 清空历史清除记忆**
    - 验证调用 `ClearAIAssistantHistory()` 后，`memory.load("desktop-user")` 返回空切片
    - **验证: 需求 3.5**

- [x] 2. 检查点 - 后端功能验证
  - 确保所有测试通过，如有问题请询问用户。

- [x] 3. 前端 useAIAssistant Hook 实现
  - [x] 3.1 创建 `frontend/src/components/ai/useAIAssistant.ts` Hook
    - 定义 `ChatMessage` 接口（id, role, content, fields, actions, timestamp）
    - 实现 `messages` 状态管理（前端维护对话历史用于 UI 渲染）
    - 实现 `sending` 状态标记
    - 实现 `sendMessage(text)` 方法：添加用户消息 → 调用 Wails Binding `SendAIAssistantMessage` → 添加 assistant 回复
    - 实现 `clearHistory()` 方法：调用 `ClearAIAssistantHistory` 并清空前端消息列表
    - 实现 `executeAction(command)` 方法：将 action.command 作为新消息发送
    - 通过 `EventsOn("ai-assistant-progress", ...)` 监听进度事件，添加 `role: 'progress'` 消息
    - 在 `useEffect` cleanup 中调用 `EventsOff` 避免内存泄漏
    - 空消息（`text.trim() === ""`）直接返回，不调用后端
    - _需求: 2.5, 2.7, 2.9, 4.3, 5.2, 5.3, 5.4_

  - [x]* 3.2 编写前端属性测试：发送消息增长消息列表
    - **Property 2: 发送消息增长消息列表**
    - 验证 `sendMessage(text)` 后消息列表长度至少增加 1，且包含 `role === 'user'` 的消息
    - **验证: 需求 2.5**

  - [x]* 3.3 编写前端属性测试：关闭/重新打开保留对话历史
    - **Property 3: 关闭/重新打开保留对话历史**
    - 验证关闭面板再重新打开后消息列表不变
    - **验证: 需求 2.9**

  - [x]* 3.4 编写前端属性测试：进度事件传递
    - **Property 9: 进度事件传递**
    - 验证 `onProgress` 回调的文本通过事件传递后，消息列表中出现 `role === 'progress'` 的消息
    - **验证: 需求 5.1, 5.2**

  - [x]* 3.5 编写前端属性测试：最终回复在进度消息之后
    - **Property 10: 最终回复在进度消息之后**
    - 验证 `role === 'assistant'` 消息的索引大于所有对应 `role === 'progress'` 消息的索引
    - **验证: 需求 5.4**

- [x] 4. 检查点 - Hook 功能验证
  - 确保所有测试通过，如有问题请询问用户。

- [x] 5. AIAssistantPanel 组件实现
  - [x] 5.1 创建 `frontend/src/components/ai/AIAssistantPanel.tsx` 组件
    - 实现全屏覆盖层（fixed overlay），深色终端主题，视觉风格参考 `RemoteSessionConsole`
    - 实现顶部标题栏：traffic lights 风格关闭按钮 + "AI 助手"标题 + 清空历史按钮
    - 实现中间滚动对话区域：渲染 `ChatMessage[]`，支持 Markdown（代码块、加粗、列表等），复用 `RemoteSessionConsole` 的 `renderMarkdownLine` / `renderInlineMarkdown` 逻辑
    - 实现底部输入栏：`❯` 提示符 + 文本输入框 + 发送按钮
    - 回车键发送消息，Escape 键关闭面板
    - Agent 处理中显示"正在思考..."加载指示器
    - 新消息到达时自动滚动到底部（除非用户已手动向上滚动）
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.7, 2.8, 4.5_

  - [x] 5.2 实现 Agent 回复结构化渲染
    - Markdown 文本渲染（`response.text`）
    - 键值对卡片渲染（`response.fields` 数组 → label/value 卡片）
    - 操作按钮渲染（`response.actions` 数组 → 可点击按钮，点击后发送 `action.command`）
    - 错误信息红色警告样式渲染（`response.error`）
    - 进度消息浅色样式渲染（`role === 'progress'`）
    - _需求: 4.1, 4.2, 4.3, 4.4, 5.3_

  - [x]* 5.3 编写前端属性测试：响应渲染完整性
    - **Property 8: 响应渲染完整性**
    - 验证 fields 数组的每个 label/value 都被渲染，actions 数量与按钮数量一致，error 以错误样式显示
    - **验证: 需求 4.2, 4.3, 4.4**

- [x] 6. 检查点 - 面板组件验证
  - 确保所有测试通过，如有问题请询问用户。

- [x] 7. 侧边栏图标集成与国际化
  - [x] 7.1 在 `App.tsx` 侧边栏中添加 AI 助手图标入口
    - 在远程（）图标 `<div>` 之后插入新的 sidebar-item
    - 使用龙虾 emoji（）作为图标，尺寸 1.2rem
    - 图标下方显示标签文字：中文"AI 助手" / 英文"AI Asst"
    - 点击时设置 `showAIPanel = true`，弹出 `AIAssistantPanel`
    - 选中状态显示 3px 宽主色调右边框高亮
    - 添加 `showAIPanel` 布尔状态（独立于 `navTab`，因为 AI 面板是全屏覆盖层）
    - 支持国际化 tooltip（zh-Hans / zh-Hant / en）
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [x] 7.2 在 `App.tsx` 中集成 `AIAssistantPanel` 渲染
    - 导入 `AIAssistantPanel` 组件
    - 当 `showAIPanel === true` 时渲染 `<AIAssistantPanel onClose={() => setShowAIPanel(false)} />`
    - 确保面板关闭后对话历史保留（`useAIAssistant` 的 messages 状态在组件外层维护或通过 ref 持久化）
    - _需求: 1.4, 2.8, 2.9_

  - [x]* 7.3 编写前端属性测试：国际化标签正确性
    - **Property 1: 国际化标签正确性**
    - 验证 zh-Hans / zh-Hant / en 三种语言下图标标签和 tooltip 与预期本地化字符串匹配
    - **验证: 需求 1.3, 1.6**

- [x] 8. 最终检查点 - 全功能验证
  - 确保所有测试通过，如有问题请询问用户。

## 备注

- 标记 `*` 的任务为可选测试任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保可追溯性
- 检查点确保增量验证，避免问题累积
- 属性测试验证通用正确性属性，单元测试验证具体示例和边界情况
- 后端使用 Go `testing/quick` 或 `pgregory.net/rapid`，前端使用 `fast-check`
