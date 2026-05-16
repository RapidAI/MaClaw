# 需求文档

## 简介

在 MaClaw 桌面应用主界面左侧导航栏（远程图标下方）新增一个"AI 助手"图标入口，图标采用有趣的龙虾 AI 形象。点击后弹出一个全屏终端风格的对话界面，接入 maClaw Agent（即 IMMessageHandler），将原本在飞书/IM 上的自然语言交互能力搬到桌面端，让用户无需手机即可直接与 maClaw 对话、下达指令、查看执行结果。

## 术语表

- **AI_Assistant_Panel**: AI 助手面板，桌面端的 maClaw 对话界面组件
- **AI_Assistant_Icon**: 左侧导航栏中的 AI 助手图标按钮
- **Sidebar**: 主界面左侧 60px 宽的图标导航栏
- **IMMessageHandler**: 后端 Go 模块，负责处理 IM 用户消息并调用 LLM Agent 生成回复
- **Terminal_Console**: 终端风格的全屏对话界面，参考 RemoteSessionConsole 的视觉风格
- **Conversation_Memory**: IMMessageHandler 中的对话记忆机制，维护多轮对话上下文
- **Wails_Binding**: Go 后端通过 Wails 框架暴露给前端的函数接口
- **Agent_Response**: IMAgentResponse 结构体，包含文本、字段、操作按钮、图片等

## 需求

### 需求 1：AI 助手图标入口

**用户故事：** 作为桌面端用户，我希望在左侧导航栏看到一个 AI 助手图标，以便快速进入 AI 对话界面。

#### 验收标准

1. THE Sidebar SHALL 在远程（📡）图标下方显示一个 AI_Assistant_Icon
2. THE AI_Assistant_Icon SHALL 使用龙虾 AI 主题的 SVG 或 PNG 图标，尺寸与其他导航图标一致（约 1.2rem）
3. THE AI_Assistant_Icon SHALL 在图标下方显示"AI 助手"（中文）或"AI Asst"（英文）标签文字，字号与其他导航项一致（0.65rem）
4. WHEN 用户点击 AI_Assistant_Icon 时，THE AI_Assistant_Panel SHALL 以全屏覆盖方式弹出
5. WHEN AI_Assistant_Icon 处于选中状态时，THE Sidebar SHALL 在该图标右侧显示 3px 宽的主色调高亮边框，与其他导航项的选中样式一致
6. THE AI_Assistant_Icon SHALL 支持国际化，根据当前语言设置显示对应的标签文字和 tooltip

### 需求 2：AI 助手对话界面

**用户故事：** 作为桌面端用户，我希望有一个类似终端的对话界面与 maClaw 交互，以便像在 IM 上一样下达指令和查看结果。

#### 验收标准

1. THE AI_Assistant_Panel SHALL 以全屏覆盖层（fixed overlay）方式展示，视觉风格参考 RemoteSessionConsole 的深色终端主题
2. THE AI_Assistant_Panel SHALL 包含顶部标题栏，显示"AI 助手"标题、连接状态指示和关闭按钮
3. THE AI_Assistant_Panel SHALL 包含中间滚动区域，用于显示对话历史（用户输入和 Agent 回复）
4. THE AI_Assistant_Panel SHALL 包含底部输入栏，包含文本输入框和发送按钮
5. WHEN 用户在输入框中输入文本并按回车键或点击发送按钮时，THE AI_Assistant_Panel SHALL 将消息发送给 IMMessageHandler 并显示用户输入
6. WHEN IMMessageHandler 返回 Agent_Response 时，THE AI_Assistant_Panel SHALL 在对话区域渲染回复文本，支持 Markdown 格式（代码块、加粗、列表等）
7. WHILE Agent 正在处理消息时，THE AI_Assistant_Panel SHALL 显示加载指示器（如"正在思考..."动画）
8. WHEN 用户按下 Escape 键或点击关闭按钮时，THE AI_Assistant_Panel SHALL 关闭覆盖层并返回主界面
9. THE AI_Assistant_Panel SHALL 在关闭后保留对话历史，再次打开时恢复之前的对话内容

### 需求 3：后端消息通道

**用户故事：** 作为桌面端用户，我希望桌面端的 AI 对话能复用 maClaw 的 Agent 能力，以便获得与 IM 端一致的智能交互体验。

#### 验收标准

1. THE Wails_Binding SHALL 暴露一个 SendAIAssistantMessage(text string) 函数，接收用户输入文本并返回 Agent_Response
2. THE SendAIAssistantMessage 函数 SHALL 调用 IMMessageHandler.HandleIMMessage，传入 platform 为 "desktop" 的 IMUserMessage
3. THE Conversation_Memory SHALL 为桌面端对话维护独立的会话上下文，user_id 使用固定标识（如 "desktop-user"）
4. IF IMMessageHandler 处理消息时发生错误，THEN THE Wails_Binding SHALL 返回包含 error 字段的 Agent_Response，前端据此显示错误提示
5. THE Wails_Binding SHALL 暴露一个 ClearAIAssistantHistory() 函数，用于清空桌面端的对话记忆

### 需求 4：Agent 回复渲染

**用户故事：** 作为桌面端用户，我希望 Agent 的回复能以结构化方式展示，以便清晰地查看执行结果和操作建议。

#### 验收标准

1. WHEN Agent_Response 包含 text 字段时，THE AI_Assistant_Panel SHALL 以 Markdown 格式渲染文本内容
2. WHEN Agent_Response 包含 fields 数组时，THE AI_Assistant_Panel SHALL 以键值对卡片形式展示每个 field 的 label 和 value
3. WHEN Agent_Response 包含 actions 数组时，THE AI_Assistant_Panel SHALL 为每个 action 渲染一个可点击按钮，点击后将 action.command 作为新消息发送
4. WHEN Agent_Response 包含 error 字段时，THE AI_Assistant_Panel SHALL 以红色警告样式显示错误信息
5. THE AI_Assistant_Panel SHALL 在新消息到达时自动滚动到底部，除非用户已手动向上滚动查看历史

### 需求 5：进度反馈

**用户故事：** 作为桌面端用户，我希望在 Agent 执行长时间任务时能看到进度更新，以便了解当前执行状态。

#### 验收标准

1. THE Wails_Binding SHALL 支持通过 Wails 事件机制（EventsEmit）向前端推送 Agent 的中间进度消息
2. WHEN IMMessageHandler 通过 onProgress 回调发送进度更新时，THE AI_Assistant_Panel SHALL 在对话区域实时显示进度文本（如"正在执行 bash 命令…"）
3. WHILE Agent 正在处理且有进度更新时，THE AI_Assistant_Panel SHALL 将进度消息以浅色样式追加在当前回复区域下方
4. WHEN Agent 处理完成后，THE AI_Assistant_Panel SHALL 将最终回复替换或追加在进度消息之后
