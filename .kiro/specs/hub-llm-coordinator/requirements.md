# 需求文档：Hub LLM 协调器（群聊智能化）

## 简介

为 Hub 服务端引入可选的 LLM 协调层，将 Hub 从"哑管道"升级为"智能中枢"。协调器在 IM 消息路由到客户端 Agent 之前介入，提供智能路由、讨论编排、回复合并等能力。核心设计原则：LLM 是可选的增强层，未配置时完全降级为现有行为；已配置时仅在规则引擎无法处理的场景才调用 LLM，绝不在单消息热路径上增加延迟。

## 术语表

- **Hub_LLM**: Hub 服务端配置的 LLM 提供商（默认 DeepSeek V3，OpenAI 兼容 API），用于协调层的智能决策。LLM 请求复用 `corelib/agent.DoSimpleLLMRequest`，自带 `User-Agent: OpenClaw/1.0`，兼容 OpenAI/Anthropic 协议，可直接对接龙虾等 OpenClaw 兼容的包月套餐服务
- **Coordinator**: 协调器，Hub 侧的消息处理中间层，位于 IM Adapter 和 MessageRouter 之间，负责智能路由、讨论编排、回复合并
- **Rule_Engine**: 规则引擎，基于确定性规则（命令匹配、设备数量、@指定）做路由决策，零延迟，不依赖 LLM
- **Smart_Router**: 智能路由器，Intent_Classifier 的路由执行层，根据分类结果将消息发送到目标设备
- **Intent_Classifier**: 意图分类器，LLM 全面接管的核心组件，对每条非命令消息进行意图分类（route_single / broadcast / discuss / need_clarification），是无感智能模式的 primary 决策者
- **Discussion_Conductor**: 讨论指挥官，替代现有的机械式轮次讨论，由 LLM 决定每轮谁发言、问什么问题、何时总结收敛
- **Reply_Merger**: 回复合并器，当多设备同时回复时，由 LLM 去重、合并、提取关键差异，生成一份整合回复
- **Device_Profile**: 设备画像，Hub 维护的每台在线设备的上下文摘要（项目语言、框架、当前 session 状态），供路由决策使用
- **Passthrough_Mode**: 直通模式，LLM 未配置或不可用时的降级行为，等同于现有的 MessageRouter 逻辑
- **MessageRouter**: 现有的消息路由器（`hub/internal/im/router.go`），负责将 IM 消息转发到客户端 Agent
- **IM_Adapter**: 现有的 IM 适配器（`hub/internal/im/core.go`），管理 IM 插件注册、身份映射、限流

## 需求

### 需求 1：Hub LLM 配置管理

**用户故事：** 作为 Hub 管理员，我希望能在 Admin 页面配置 Hub 的 LLM 提供商，以便启用群聊智能化功能。同时，未配置 LLM 的 Hub 实例必须保持与当前完全一致的行为。

#### 验收标准

1. THE Hub SHALL 支持通过 Admin API 配置 Hub_LLM，配置项包括：API Base URL、API Key、Model Name、是否启用（enabled 开关）、单设备智能路由开关（`smart_route_single_device`，默认 false）
2. THE Hub SHALL 将 Hub_LLM 配置持久化到 system_settings 表（key: `hub_llm_config`），格式为 JSON
3. WHEN Hub_LLM 未配置或 enabled 为 false 时，THE Coordinator SHALL 运行在 Passthrough_Mode，所有消息直接走现有 MessageRouter 逻辑，行为与当前完全一致
4. WHEN Hub_LLM 配置变更时，THE Coordinator SHALL 在下一次消息处理时使用新配置，无需重启 Hub
5. THE Admin API SHALL 提供 Hub_LLM 连通性测试端点，发送一条简单 prompt 验证 API 可达性和 Key 有效性
6. THE Hub SHALL 在 API Key 存储时进行脱敏处理，Admin API 返回的配置中 Key 字段显示为掩码格式
7. WHEN `smart_route_single_device` 为 false（默认）时，单设备用户的消息 SHALL 直接转发，不经过 LLM 意图分类；WHEN 为 true 时，单设备用户的消息也 SHALL 经过 LLM 意图分类（用于体验测试）

### 需求 2：设备画像收集

**用户故事：** 作为系统，我需要了解每台在线设备的项目上下文，以便智能路由能根据设备能力选择目标。

#### 验收标准

1. WHEN 客户端通过 WebSocket 连接到 Hub 时，THE Hub SHALL 接收并缓存客户端上报的 Device_Profile，包含：设备名称、当前打开的项目路径、项目主要语言、项目框架、活跃 session 列表、LLM 是否已配置
2. THE Hub SHALL 在客户端断开连接时清除对应的 Device_Profile 缓存
3. WHEN 客户端的项目上下文发生变化（切换项目、启动/关闭 session）时，THE 客户端 SHALL 主动推送更新的 Device_Profile 到 Hub
4. THE Device_Profile 缓存 SHALL 使用内存存储（不持久化），Hub 重启后由客户端重新上报
5. THE Coordinator SHALL 能通过 DeviceFinder 接口获取指定用户所有在线设备的 Device_Profile

### 需求 3：规则引擎优先路由

**用户故事：** 作为用户，我希望大部分消息能零延迟路由到正确的设备，只有真正需要智能判断的消息才走 LLM。

#### 验收标准

1. THE Rule_Engine SHALL 按以下优先级处理消息，命中任一规则即立即路由，不调用 LLM：
   - (a) 显式命令（`/call`、`/stop`、`/discuss`、`/help`、`/rounds`）→ 执行对应命令逻辑
   - (b) `@昵称` 前缀 → 路由到指定设备
   - (c) 用户只有一台在线设备且 `smart_route_single_device` 为 false → 直接转发
   - (d) 用户已通过 `/call <name>` 选定设备且未进入群聊模式 → 转发到选定设备
   - (e) 用户处于 `/call all` 广播模式 → 广播到所有设备
2. WHEN 以上规则均未命中时（多设备在线 + 未选定设备 + 非命令消息，或单设备 + `smart_route_single_device` 为 true），THE Rule_Engine SHALL 将消息交给 Intent_Classifier 处理
3. IF Hub_LLM 未配置，THEN THE Rule_Engine 在规则未命中时 SHALL 降级为现有行为：提示用户使用 `/call` 选择设备
4. THE Rule_Engine 的所有规则判断 SHALL 在内存中完成，不涉及任何 I/O 操作

### 需求 4：LLM 意图分类与智能路由

**用户故事：** 作为拥有多台设备的用户，我希望直接发消息就行，系统自动判断该发给谁、该不该群聊、该不该讨论，不需要我记任何命令。

#### 验收标准

1. WHEN LLM 已配置且规则引擎未命中时，THE Intent_Classifier SHALL 调用 Hub_LLM 对用户消息进行意图分类，输出为以下之一：
   - (a) `route_single`: 发给指定设备（附带目标设备 ID + 理由）
   - (b) `broadcast`: 广播到所有设备
   - (c) `discuss`: 发起多轮讨论（附带话题提取）
   - (d) `need_clarification`: 无法判断，需要用户补充信息
2. THE Intent_Classifier 的 LLM 调用 SHALL 输入为：用户消息文本 + 所有在线设备的 Device_Profile 摘要 + 最近 3 条消息的路由历史（提供上下文连续性）
3. THE Intent_Classifier 的 LLM 调用 SHALL 设置 5 秒超时，超时后降级为广播到所有设备
4. THE Intent_Classifier SHALL 在路由结果中附带简短说明（如 "已发送到 MacBook-Pro（检测到前端项目）"），通过 IM 回复告知用户
5. THE Intent_Classifier SHALL 缓存最近 10 条消息的分类决策，相似消息（同一用户、相同设备集合、相似意图）可复用缓存结果，避免重复 LLM 调用
6. THE Intent_Classifier 的 LLM 请求 SHALL 复用 `corelib/agent.DoSimpleLLMRequest`，自带 `User-Agent: OpenClaw/1.0`，兼容龙虾等 OpenClaw 包月套餐服务
7. WHEN Intent_Classifier 返回 `discuss` 意图时，THE Coordinator SHALL 自动启动讨论编排流程，无需用户手动输入 `/discuss`
8. WHEN Intent_Classifier 返回 `broadcast` 意图时，THE Coordinator SHALL 自动进入广播模式处理该消息，无需用户手动 `/call all`

### 需求 5：讨论编排（LLM 指挥）

**用户故事：** 作为用户，我希望 `/discuss` 发起的多设备讨论能像真正的对话一样有来有回，而不是每轮所有设备回答同一个问题。

#### 验收标准

1. WHEN Hub_LLM 已配置且用户发起 `/discuss` 时，THE Discussion_Conductor SHALL 替代现有的机械式轮次逻辑，由 LLM 编排讨论流程
2. THE Discussion_Conductor SHALL 在每轮结束后调用 LLM 决定下一步动作，可选动作包括：
   - (a) 向指定设备追问具体问题
   - (b) 要求某设备对另一设备的观点进行评价
   - (c) 生成阶段性总结
   - (d) 结束讨论并生成最终总结
3. THE Discussion_Conductor 的每轮 LLM 调用 SHALL 包含：讨论主题、当前轮次、所有历史回复的摘要（非全文）、各设备的 Device_Profile
4. WHEN Hub_LLM 未配置时，THE `/discuss` 命令 SHALL 使用现有的机械式轮次逻辑，行为与当前完全一致
5. THE Discussion_Conductor SHALL 支持用户在讨论过程中插话，插话内容作为下一轮 LLM 编排的输入
6. THE Discussion_Conductor SHALL 设置讨论总轮次上限（默认 10 轮），达到上限后自动生成总结并结束

### 需求 6：多设备回复合并

**用户故事：** 作为用户，当我在广播模式下发消息时，我希望收到一份整合后的回复，而不是 N 份可能高度重复的独立回复。

#### 验收标准

1. WHEN 用户处于广播模式且 Hub_LLM 已配置时，THE Reply_Merger SHALL 在收集到所有设备回复后，调用 LLM 合并回复
2. THE Reply_Merger 的合并策略 SHALL 为：去除重复内容、保留各设备的独特观点、标注观点来源设备名称、在末尾列出各设备的关键差异
3. THE Reply_Merger SHALL 设置 10 秒超时等待所有设备回复，超时后仅合并已收到的回复
4. IF 只有一台设备回复（其他超时），THEN THE Reply_Merger SHALL 直接返回该设备的回复，不调用 LLM
5. IF 多台设备的回复内容高度相似（文本相似度 > 80%），THEN THE Reply_Merger SHALL 直接返回第一台设备的回复并附注"其他设备观点一致"，不调用 LLM
6. WHEN Hub_LLM 未配置时，THE 广播模式 SHALL 使用现有行为：逐个返回各设备的独立回复

### 需求 7：无感智能模式（LLM 全面接管）

**用户故事：** 作为用户，我希望接入多台 maclaw 后就像跟一个统一的 AI 团队对话，完全不需要记任何命令，系统自动判断一切。

#### 验收标准

1. WHEN Hub_LLM 已配置且用户有多台设备在线时，THE Coordinator SHALL 默认进入"无感智能模式"：所有非命令消息都先经过 Intent_Classifier，由 LLM 决定走单聊、广播还是讨论
2. THE 无感智能模式下，用户无需执行 `/call`、`/call all`、`/discuss` 等命令即可自然使用所有功能——LLM 意图分类是 primary，命令系统是 fallback
3. WHEN Intent_Classifier 返回 `discuss` 意图时，THE Coordinator SHALL 自动启动讨论编排，并向用户发送一条简短通知（如"检测到讨论意图，已自动发起多设备讨论"）
4. WHEN Intent_Classifier 返回 `broadcast` 意图时，THE Coordinator SHALL 自动广播到所有设备，并向用户发送一条简短通知（如"已广播到所有设备"）
5. THE `/call`、`/call all`、`/discuss`、`/stop` 等显式命令 SHALL 继续有效，作为用户手动覆盖智能路由的方式
6. WHEN Hub_LLM 未配置时，THE 无感智能模式 SHALL 不生效，多设备用户仍需使用 `/call` 命令选择设备（现有行为）
7. WHEN 用户首次进入无感智能模式时，THE Coordinator SHALL 发送一条欢迎消息，说明当前处于智能模式，可直接发消息，也可使用命令手动控制

### 需求 8：群聊记忆与上下文

**用户故事：** 作为用户，我希望讨论的结论和重要信息能被记住，下次相关话题时自动召回。

#### 验收标准

1. WHEN 一次讨论结束时，THE Coordinator SHALL 调用 LLM 提取讨论中的关键结论和决策，生成结构化摘要
2. THE Coordinator SHALL 将讨论摘要存储到 Hub 的 system_settings 表（key: `discussion_history_{userID}`），最多保留最近 20 条
3. WHEN 用户发起新讨论时，THE Discussion_Conductor SHALL 检索历史讨论摘要中与当前话题相关的条目，注入到 LLM 的上下文中
4. THE 讨论历史 SHALL 包含：话题、参与设备、关键结论、时间戳
5. WHEN Hub_LLM 未配置时，THE 讨论历史功能 SHALL 不生效，不存储也不检索

### 需求 9：LLM 调用容错与降级

**用户故事：** 作为系统，我需要确保 LLM 不可用时不影响核心消息路由功能。

#### 验收标准

1. WHEN Hub_LLM API 调用失败（网络错误、超时、API 错误）时，THE Coordinator SHALL 立即降级为 Passthrough_Mode 处理当前消息，不重试
2. THE Coordinator SHALL 记录 LLM 调用失败的日志（包含错误类型、耗时），但不向用户暴露 LLM 内部错误
3. WHEN LLM 连续失败 3 次时，THE Coordinator SHALL 自动进入 Passthrough_Mode 并保持 5 分钟，期间不尝试 LLM 调用（熔断机制）
4. WHEN 熔断恢复后，THE Coordinator SHALL 用下一条消息尝试 LLM 调用，成功则恢复正常模式
5. THE Coordinator SHALL 在 Admin API 中暴露 LLM 健康状态（正常/熔断中/未配置），供管理员监控

### 需求 10：Admin 页面 LLM 配置 UI

**用户故事：** 作为 Hub 管理员，我希望在 Admin 页面上有一个简洁的 LLM 配置区域，能配置、测试和监控 Hub LLM。

#### 验收标准

1. THE Admin 页面 SHALL 在设置区域新增"Hub LLM"配置卡片，包含：启用开关、API Base URL 输入框、API Key 输入框（密码模式）、Model Name 输入框
2. THE 配置卡片 SHALL 提供"测试连接"按钮，点击后调用测试端点并显示结果（成功/失败 + 耗时）
3. THE 配置卡片 SHALL 显示当前 LLM 状态指示器：正常 / 熔断中 / ⚪ 未配置
4. THE 配置变更 SHALL 通过 Admin API 保存，保存成功后页面显示确认提示

---

## 基线体验优化（无 LLM 场景）

以下需求针对未配置 Hub LLM 的场景，优化现有多设备交互的基线体验。

### 需求 11：智能设备自动选择

**用户故事：** 作为只有一台设备在线的用户，我不想每次都被提示"请选择设备"；作为多设备用户，我希望系统能记住我上次使用的设备。

#### 验收标准

1. WHEN 用户只有一台在线设备时，THE MessageRouter SHALL 自动选择该设备并直接转发消息，不显示设备选择提示（当前已实现）
2. WHEN 用户有多台设备在线且之前已选择过设备时，THE MessageRouter SHALL 记住上次选择的设备（sticky selection），用户下次发消息时自动使用该设备，无需重新 `/call`
3. WHEN sticky selection 的设备离线时，THE MessageRouter SHALL 清除该选择，并在下次消息时：如果只剩一台设备则自动选择，否则提示用户重新选择
4. THE 设备选择提示 SHALL 包含每台设备的简要上下文信息（设备名称、LLM 状态），帮助用户做出选择
5. WHEN 用户首次连接且有多台设备在线时，THE MessageRouter SHALL 发送一条友好的欢迎引导消息，说明如何使用 `/call`、`/call all`、`/discuss` 等命令

### 需求 12：广播回复格式优化

**用户故事：** 作为使用 `/call all` 广播模式的用户，我希望收到的多设备回复格式清晰、易读，而不是简单地拼接 N 份独立回复。

#### 验收标准

1. THE routeBroadcast SHALL 在合并文本回复时使用结构化格式：每台设备的回复用清晰的分隔线和设备名称标题分隔，而非仅用 `[name]` 前缀
2. WHEN 多台设备的回复内容高度相似时（简单文本比较，如前 100 字符相同），THE routeBroadcast SHALL 只显示第一台设备的完整回复，并附注"其他 N 台设备观点一致"
3. THE routeBroadcast SHALL 在回复末尾附加一行摘要统计：参与设备数、成功回复数、超时/失败数
4. WHEN 有设备超时或失败时，THE routeBroadcast SHALL 将错误信息集中在回复末尾的"异常"区域，而非穿插在正常回复中

### 需求 13：讨论模式体验优化

**用户故事：** 作为使用 `/discuss` 的用户，我希望讨论过程更有条理，每轮之间有清晰的结构，而不是一堆消息刷屏。

#### 验收标准

1. THE runDiscussion SHALL 在每轮开始时发送包含轮次号和当前 prompt 摘要的进度消息（当前已实现轮次号，需增加 prompt 摘要）
2. THE runDiscussion SHALL 在每轮结束后发送该轮的简要小结（各设备观点的一句话概括），而非仅逐条转发每台设备的完整回复
3. THE runDiscussion SHALL 支持用户通过 `/rounds N` 命令动态调整剩余讨论轮数（默认 3 轮可能不够或太多）
4. THE 讨论总结 SHALL 使用更结构化的格式：分为"共识点"、"分歧点"、"待定事项"三个部分（由总结设备的 prompt 引导）
5. WHEN 讨论中某台设备超时未回复时，THE runDiscussion SHALL 跳过该设备继续下一轮，而非将超时信息作为该设备的"观点"传递给下一轮

### 需求 14：命令帮助与引导

**用户故事：** 作为新用户，我希望能快速了解 Hub IM 支持哪些命令，以及每个命令的用法。

#### 验收标准

1. WHEN 用户发送 `/help` 时，THE Adapter SHALL 返回所有可用命令的列表和简要说明
2. THE `/help` 回复 SHALL 根据用户当前状态动态调整：单设备用户不显示 `/call all` 和 `/discuss`；已在广播模式时提示 `/call <name>` 可切回单聊
3. WHEN 用户发送无法识别的 `/` 命令时，THE Adapter SHALL 返回友好的错误提示并建议使用 `/help`
4. THE 命令帮助 SHALL 包含简短的使用示例（如 `/call MacBook-Pro`、`/discuss 如何优化性能`）

### 需求 15：设备状态变更通知

**用户故事：** 作为多设备用户，我希望在设备上线/离线时收到通知，这样我知道当前有哪些设备可用。

#### 验收标准

1. WHEN 用户的某台设备上线或离线时，THE Hub SHALL 通过 IM 向该用户发送一条简短的状态通知（如"MacBook-Pro 已上线"或"MacBook-Pro 已离线"）
2. THE 状态通知 SHALL 仅在用户有活跃的 IM 会话时发送（即该用户在某个 IM 平台上有过消息交互），避免打扰未使用 IM 的用户
3. WHEN 用户当前选定的设备离线时，THE 通知 SHALL 额外提示用户切换设备或等待重连
4. THE 状态通知 SHALL 有防抖机制：同一设备在 30 秒内的多次上下线只发送最终状态，避免网络抖动导致消息轰炸
