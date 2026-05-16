# 需求文档：飞书自然语言远程工具控制

## 简介

本功能将 MaClaw Hub 的 IM 机器人升级为 Agent 透传模式。用户通过 IM 平台（飞书、QBot 等）发送的自然语言消息，经 Hub 透传到 MaClaw 客户端的 Agent（LLM），由 Agent 自行理解意图、调用本地工具、生成回复，再经 Hub 回传到 IM 平台。Hub 不做意图解析和命令映射，只负责消息路由、身份映射和速率限制。

这种架构的核心优势：Agent 拥有完整的本地上下文（文件系统、工具状态、会话列表、项目结构），能做出比 Hub 侧规则引擎更好的决策；新增能力只需更新 Agent 的 system prompt 和工具定义，无需改 Hub 代码。

架构上基于 OpenClaw IM 插件抽象体系构建 IM 适配层，通过统一的消息模型、能力抽象和身份抽象屏蔽平台差异，飞书作为首个 IM 接入，QBot 作为第二个计划接入平台。MCP Server 注册、Skill 定义和执行都在 MaClaw 客户端侧实现，Agent 可直接调用。

## 术语表

- **Agent**: MaClaw 客户端侧的 LLM Agent，负责理解用户自然语言、调用本地工具、生成回复
- **Agent_Passthrough**: Hub 的消息透传模式，IM 消息不经意图解析直接路由到 MaClaw Agent
- **Message_Router**: Hub 侧的消息路由器，负责将 IM 消息透传到用户绑定设备的 Agent，并将 Agent 回复路由回 IM
- **MCP_Registry**: MCP 注册中心（MaClaw 客户端侧），管理外部 MCP Server 的注册、发现和调用
- **Skill**: 可复用的操作单元，封装一组特定的工具调用序列或自动化流程（MaClaw 客户端侧）
- **Skill_Executor**: Skill 执行器（MaClaw 客户端侧），负责解析和执行 Skill 定义的操作序列
- **Skill_Crystallizer**: 技能沉淀器（MaClaw 客户端侧），从用户交互历史中识别可复用的操作模式
- **Feishu_Plugin**: 飞书 IM 插件，IM_Adapter 的首个实现，负责接收飞书消息和发送回复
- **GenericResponse**: 通用响应模型，Agent 回复经 Hub 转换为 IM 平台特定格式
- **Remote_Session_Manager**: 远程会话管理器（MaClaw 客户端侧），Agent 可调用的工具之一
- **Tool_Catalog**: 远程工具目录（MaClaw 客户端侧），Agent 可查询的工具元数据
- **IM_Adapter**: IM 适配层（Hub 侧），抽象 IM 平台差异的统一接口层
- **IM_Plugin**: IM 插件，具体 IM 平台实现
- **Settings_Panel**: 设置面板，前端 App.tsx 中的设置界面容器
- **Skills_Management_Panel**: Skills 管理面板，通过 Wails 绑定操作客户端侧的 Skill_Executor
- **MCP_Management_Panel**: MCP 管理面板，通过 Wails 绑定操作客户端侧的 MCP_Registry

## 需求

### 需求 1：Agent 透传消息路由

**用户故事：** 作为开发者，我希望通过 IM 平台直接用自然语言描述我想做的事情，由 MaClaw Agent 自行理解和处理，而不是经过 Hub 侧的硬编码意图映射。

#### 验收标准

1. WHEN 用户通过 IM 平台发送文本消息, THE Hub SHALL 将消息透传到用户绑定设备上的 MaClaw Agent，不做意图解析
2. THE Hub SHALL 通过 WebSocket 协议新增 `im.user_message` 消息类型，将 IM 消息路由到 MaClaw 客户端
3. THE MaClaw Agent SHALL 通过 WebSocket 协议新增 `im.agent_response` 消息类型，将回复路由回 Hub
4. WHEN Agent 回复到达 Hub, THE Hub SHALL 将其转换为 GenericResponse 并通过 IM_Plugin 发送到用户
5. THE Hub SHALL 为每个透传请求生成唯一 request_id，用于关联请求和回复
6. IF Agent 在 120 秒内未回复, THEN THE Hub SHALL 向用户返回超时提示消息
7. WHEN 用户绑定的设备不在线, THE Hub SHALL 直接返回 "您的设备不在线" 提示
8. WHEN 设备在线但 LLM 未配置, THE Hub SHALL 直接返回 "Agent 未就绪，请在 MaClaw 中配置 LLM" 提示

### 需求 2：MaClaw Agent 自然语言处理

**用户故事：** 作为开发者，我希望 MaClaw Agent 能理解我的自然语言指令并调用合适的工具完成操作。

#### 验收标准

1. WHEN MaClaw 客户端收到 `im.user_message`, THE Agent SHALL 使用配置的 LLM（OpenAI-compatible API）理解用户意图
2. THE Agent SHALL 在 system prompt 中注入当前设备状态、活跃会话列表、可用工具列表等上下文信息
3. THE Agent SHALL 支持以下工具调用：list_sessions、create_session、send_input、interrupt_session、kill_session、screenshot、list_mcp_tools、call_mcp_tool、run_skill
4. WHEN LLM 返回 tool_call, THE Agent SHALL 执行对应工具并将结果作为 AgentResponse 回传
5. WHEN LLM 返回纯文本回复, THE Agent SHALL 直接将文本作为 AgentResponse.Text 回传
6. THE Agent SHALL 使用中文回复，关键技术术语保留英文原文
7. IF 工具调用失败, THEN THE Agent SHALL 在回复中包含错误原因和建议操作

### 需求 3：远程会话管理（Agent 工具）

**用户故事：** 作为开发者，我希望通过自然语言描述来管理远程开发工具会话。

#### 验收标准

1. THE Agent SHALL 提供 create_session 工具，支持参数：tool（工具名）、project_path（项目路径）、prompt（任务描述）
2. THE Agent SHALL 提供 list_sessions 工具，返回当前所有活跃会话的 ID、工具名、标题和状态
3. THE Agent SHALL 提供 send_input 工具，向指定会话发送文本输入
4. THE Agent SHALL 提供 interrupt_session 和 kill_session 工具，用于中断和终止会话
5. THE Agent SHALL 提供 screenshot 工具，截取指定会话的屏幕
6. IF 用户请求的工具未安装, THEN THE Agent SHALL 返回可用工具列表并建议替代方案

### 需求 4：MCP Server 注册与调用（客户端侧）

**用户故事：** 作为开发者，我希望能在 MaClaw 客户端注册外部 MCP Server 并通过自然语言调用其工具。

#### 验收标准

1. THE MCP_Registry SHALL 在 MaClaw 客户端侧管理 MCP Server 的注册、发现和调用
2. THE Agent SHALL 提供 list_mcp_tools 和 call_mcp_tool 工具，供 LLM 调用
3. WHEN MCP_Registry 收到工具调用请求, THE MCP_Registry SHALL 按照 MCP 协议向目标 Server 发送请求
4. IF MCP Server 在 30 秒内未响应, THEN THE MCP_Registry SHALL 返回超时错误
5. THE MCP_Registry SHALL 维护已注册 MCP Server 的健康状态
6. WHEN 用户请求查看可用的 MCP 工具, THE Agent SHALL 通过 list_mcp_tools 返回所有已注册且健康的 Server 及其工具列表

### 需求 5：Skill 定义与执行（客户端侧）

**用户故事：** 作为开发者，我希望能定义和执行可复用的 Skill。

#### 验收标准

1. THE Skill_Executor SHALL 在 MaClaw 客户端侧支持以 YAML 格式定义 Skill
2. THE Agent SHALL 提供 run_skill 工具，供 LLM 根据用户意图调用
3. WHEN Skill_Executor 执行多步操作序列, THE Skill_Executor SHALL 按顺序执行每个步骤
4. IF Skill 执行过程中某个步骤失败, THEN THE Skill_Executor SHALL 停止执行并报告错误
5. THE Skill_Executor SHALL 支持在步骤中引用 MCP 工具和内置操作
6. THE Agent SHALL 提供 list_skills 工具，返回所有已注册 Skill 的名称和描述

### 需求 6：安全与权限控制

**用户故事：** 作为系统管理员，我希望消息路由受到安全控制。

#### 验收标准

1. THE Hub SHALL 在透传消息前，通过平台用户标识绑定验证用户身份，未绑定用户仅可执行绑定流程
2. THE Hub SHALL 对每个用户实施速率限制，单个用户每分钟操作请求不超过 30 次
3. WHEN 速率限制被触发, THE Hub SHALL 返回友好的限流提示消息
4. THE Agent SHALL 对 LLM 返回的 tool_call 参数做基本校验，防止异常调用
5. 邮箱绑定流程（email → open_id 映射）继续在 Hub 侧处理，不经过 Agent

### 需求 7：响应格式与用户体验

**用户故事：** 作为开发者，我希望 Agent 的回复在 IM 平台上清晰易读。

#### 验收标准

1. WHEN Agent 回复到达 Hub, THE Hub SHALL 将 AgentResponse 转换为 GenericResponse，再由 IM_Plugin 转换为平台特定格式
2. THE AgentResponse SHALL 支持结构化字段（fields）、操作建议（actions）和图片（image_key），IM_Plugin 根据平台能力自动降级
3. THE Hub SHALL 对超过 4000 字节的响应内容在行边界处截断
4. WHEN 目标 IM 平台不支持富文本元素, THE IM_Plugin SHALL 自动降级为纯文本
5. THE Agent SHALL 在回复中使用中文，关键技术术语保留英文

### 需求 8：IM 插件化适配层

**用户故事：** 作为平台开发者，我希望 IM 适配层支持快速接入新的 IM 平台。

#### 验收标准

1. THE IM_Adapter SHALL 定义统一的 IMPlugin 接口（ReceiveMessage、SendText、SendCard、SendImage、ResolveUser、Capabilities）
2. THE IM_Adapter SHALL 将所有 IM 平台的消息统一转换为 IncomingMessage 结构
3. THE IM_Adapter SHALL 根据 IM_Plugin 的能力声明自动选择最佳消息格式并处理降级
4. THE IM_Adapter SHALL 通过 Identity_Service 将平台用户标识映射为统一内部用户标识
5. WHEN 新的 IM_Plugin 注册时, THE IM_Adapter SHALL 自动将该平台的消息路由接入 Message_Router
6. THE Feishu_Plugin SHALL 作为首个 IM_Plugin 实现
7. THE IM_Adapter SHALL 为每个 IM_Plugin 维护独立的连接状态

### 需求 9：前端管理界面

**用户故事：** 作为开发者，我希望在本地前端设置面板中管理 Skills 和 MCP Server。

#### 验收标准

1. THE Settings_Panel SHALL 新增 "skills" 和 "mcp" 两个 tab
2. THE Skills_Management_Panel SHALL 通过 Wails 绑定操作客户端侧的 Skill_Executor，支持 Skill 的查看、创建、编辑、删除
3. THE MCP_Management_Panel SHALL 通过 Wails 绑定操作客户端侧的 MCP_Registry，支持 MCP Server 的注册、查看、编辑、删除和健康状态监控
4. THE Skills_Management_Panel SHALL 展示 Skill_Crystallizer 生成的候选 Skill，支持确认和忽略
5. THE MCP_Management_Panel SHALL 以颜色标识展示健康状态（绿/黄/红）
