# 需求文档：Hub 无感聊天（Seamless Chat）

## 简介

在 Hub LLM Coordinator 已实现智能路由、意图分类、讨论编排的基础上，进一步消除用户对"设备"概念的感知。核心目标：用户在任何 IM 平台上发消息，就像跟一个统一的 AI 团队对话，完全不感知背后有几台设备、当前在哪台设备上、对话历史在哪里。

本 spec 聚焦五个关键能力：
1. **Hub 侧对话上下文管理** — 对话状态从"绑定到设备"提升为"绑定到用户"
2. **Hub 直接应答** — 简单问题 Hub LLM 直接回答，不转发到客户端
3. **跨设备 Session 续接** — 设备切换时自动携带对话上下文
4. **空间模型（大厅/私聊/会议）** — 多设备交互的三种自然状态，替代显式模式切换
5. **讨论模式无感化** — 讨论从"命令驱动的模式"变为"共享上下文空间"，AI 参与者有角色意识和对话感

## 术语表

- **ConversationContext**: Hub 侧维护的用户级对话上下文，包含最近 N 轮对话摘要，不绑定特定设备
- **HubDirectAnswer**: Hub LLM 直接回答用户问题，不经过客户端 Agent，零设备延迟
- **SessionHandoff**: 当用户的消息被路由到与上一条不同的设备时，Hub 自动将对话上下文摘要注入到新设备的 prompt 中
- **ContextWindow**: Hub 维护的滑动窗口，保留最近 20 轮对话的摘要（每轮 = 用户消息 + Agent 回复的摘要）
- **IntentDirectAnswer**: IntentClassifier 新增的意图类型，表示 Hub 可以直接回答，无需转发到设备
- **空间状态（SpaceState）**: 用户与多设备交互时所处的状态，共三种：大厅（Lobby）、私聊（Private）、会议（Meeting）
- **大厅（Lobby）**: 默认空间状态，多设备在场，IntentClassifier 自动路由消息到最合适的设备
- **私聊（Private）**: 强隔离状态，用户只与一台指定设备交互，其他设备不可见，进入和退出必须通过显式命令
- **会议（Meeting）**: 共享上下文空间，所有参与设备共享讨论上下文，AI 参与者有角色意识和对话感
- **讨论上下文（DiscussionContext）**: 会议模式下维护的共享上下文，记录所有轮次的交互（包括 @某人的"小会"），所有参与者在下一轮都能看到

## 需求

### 需求 1：Hub 侧对话上下文管理

**用户故事：** 作为用户，我希望 Hub 能记住我最近的对话内容，这样当消息被路由到不同设备时，AI 仍然知道我之前在聊什么。

#### 验收标准

1. THE Hub SHALL 为每个用户维护一个 ConversationContext，包含最近 20 轮对话的摘要（用户消息原文 + Agent 回复的 LLM 生成摘要）
2. THE ConversationContext SHALL 使用内存存储（`map[string]*ConversationContext`），Hub 重启后清空（对话上下文是短期记忆，不需要持久化）
3. WHEN Agent 回复到达 Hub 时，THE Coordinator SHALL 异步调用 Hub LLM 生成回复摘要（不超过 100 字），追加到 ConversationContext
4. THE 摘要生成 SHALL 设置 3 秒超时，超时则使用回复文本的前 100 字符作为摘要（降级策略）
5. WHEN ConversationContext 超过 20 轮时，SHALL 淘汰最早的条目（FIFO）
6. THE ConversationContext SHALL 记录每轮的路由目标设备 ID，供 SessionHandoff 使用
7. WHEN Hub LLM 未配置时，THE ConversationContext SHALL 仍然维护（使用截断文本作为摘要），以便未来启用 LLM 时可用

### 需求 2：Hub 直接应答（HubDirectAnswer）

**用户故事：** 作为用户，我问一些通用问题（比如"Go 的 context 怎么用"、"帮我写个正则"）时，不需要等设备上的 Agent 响应，Hub 直接秒回。

#### 验收标准

1. THE IntentClassifier SHALL 新增 `direct_answer` 意图类型，当 LLM 判断问题不需要访问用户设备上的项目代码/文件/工具时返回此意图
2. WHEN IntentClassifier 返回 `direct_answer` 意图时，THE Coordinator SHALL 使用 Hub LLM 直接生成回答，不转发到任何客户端设备
3. THE Hub 直接应答 SHALL 将 ConversationContext 的最近 5 轮摘要注入 LLM prompt，保持对话连贯性
4. THE Hub 直接应答 SHALL 设置 15 秒超时，超时则降级为路由到设备（fallback 到 broadcast）
5. THE Hub 直接应答的 LLM prompt SHALL 包含系统提示："你是用户的 AI 编程助手，直接回答问题。如果问题需要访问用户的项目代码或文件，请回复 JSON `{\"need_device\": true}`，系统会自动转发到用户的设备。"
6. IF Hub LLM 的回答包含 `{"need_device": true}` 标记，THEN THE Coordinator SHALL 将消息降级为正常路由流程（走 IntentClassifier 的其他意图）
7. THE Hub 直接应答 SHALL 在回复中附带一个轻量标记（如回复末尾的 `— Hub AI`），让用户知道这是 Hub 直接回答的
8. WHEN Hub LLM 未配置时，THE `direct_answer` 意图 SHALL 不生效，所有消息走正常路由

### 需求 3：跨设备 Session 续接（SessionHandoff）

**用户故事：** 作为用户，我在 MacBook 上讨论了一个前端 bug，然后消息被路由到公司 PC（因为 PC 上有后端项目），我希望 PC 上的 Agent 知道我之前在 MacBook 上聊了什么。

#### 验收标准

1. WHEN Coordinator 将消息路由到设备 B，且上一轮消息路由到了设备 A（设备切换发生）时，THE Coordinator SHALL 在发送给设备 B 的 `im.user_message` 中注入 `handoff_context` 字段
2. THE `handoff_context` SHALL 包含：上一台设备的名称、最近 3 轮对话摘要（来自 ConversationContext）、切换原因（来自 IntentClassifier 的 reason）
3. THE 客户端 Agent SHALL 在收到带 `handoff_context` 的消息时，将上下文摘要作为系统消息注入到 LLM 对话历史的开头
4. THE `handoff_context` 的总文本长度 SHALL 不超过 500 字符，超过时截断最早的摘要条目
5. WHEN 用户连续发消息到同一台设备时（无设备切换），SHALL 不注入 `handoff_context`（避免冗余）
6. WHEN Hub LLM 未配置时，THE SessionHandoff SHALL 使用截断的原始文本作为上下文（降级但仍可用）

### 需求 4：对话上下文增强的意图分类

**用户故事：** 作为用户，我说"继续"或"上面那个再改改"时，系统应该知道我在说什么，自动路由到上次对话的设备。

#### 验收标准

1. THE IntentClassifier 的 LLM prompt SHALL 注入 ConversationContext 的最近 3 轮摘要，使 LLM 能理解指代和上下文延续
2. WHEN 用户消息包含指代词（"这个"、"上面的"、"继续"、"再改改"等）且 ConversationContext 有历史记录时，THE IntentClassifier SHALL 倾向于路由到上一轮的目标设备（上下文连续性优先）
3. THE IntentClassifier 的 LLM prompt SHALL 明确说明："如果用户消息是对上一轮对话的延续（包含指代词或省略主语），优先路由到上一轮的目标设备"
4. THE 路由历史（routeHistory）SHALL 与 ConversationContext 合并，避免维护两套重复的上下文数据

### 需求 5：单设备智能增强

**用户故事：** 作为只有一台设备的用户，如果 Hub LLM 已配置，我也希望享受 Hub 直接应答的能力，简单问题秒回，复杂问题才转发到设备。

#### 验收标准

1. WHEN 用户只有一台设备在线且 Hub LLM 已配置时，THE Coordinator SHALL 对消息进行轻量意图判断：是否为 `direct_answer`
2. IF 意图为 `direct_answer`，THEN Hub 直接回答（同需求 2）
3. IF 意图不为 `direct_answer`，THEN 直接转发到唯一设备（零额外延迟，不做完整意图分类）
4. THE 单设备轻量意图判断 SHALL 复用 IntentClassifier，但 prompt 简化为仅判断 `direct_answer` vs `route_single`，减少 LLM token 消耗
5. WHEN `smart_route_single_device` 为 false（默认）时，THE 单设备轻量意图判断 SHALL 不生效，消息直接转发（现有行为）
6. WHEN `smart_route_single_device` 为 true 时，THE 单设备轻量意图判断 SHALL 生效

### 需求 6：对话上下文管理 API

**用户故事：** 作为用户，我希望能通过命令查看和清除 Hub 记住的对话上下文。

#### 验收标准

1. THE Adapter SHALL 支持 `/context` 命令，显示当前 ConversationContext 的摘要（最近 5 轮的简要信息：时间、消息摘要、路由目标）
2. THE Adapter SHALL 支持 `/context clear` 命令，清除当前用户的 ConversationContext
3. THE Admin API SHALL 提供 GET `/api/admin/conversation_stats` 端点，返回当前活跃的 ConversationContext 数量和总轮次数（供监控）


### 需求 7：空间模型 — 大厅 / 私聊 / 会议

**用户故事：** 作为多设备用户，我希望跟多台设备的交互像在一个办公室里一样自然——默认在大厅里大家都在，需要时可以拉一个人私聊，也可以开会让大家一起讨论。

#### 7.1 空间状态定义

##### 验收标准

1. THE Coordinator SHALL 为每个多设备用户维护一个 SpaceState，取值为 `lobby`（大厅）、`private`（私聊）、`meeting`（会议）三种之一
2. THE SpaceState 默认值 SHALL 为 `lobby`
3. THE SpaceState SHALL 仅在多设备在线（≥2 台）时生效；单设备在线时 SHALL 不做任何空间状态管理和意图分类，消息直接透传到唯一设备（与现有行为一致）
4. THE SpaceState SHALL 使用内存存储，Hub 重启后重置为 `lobby`

#### 7.2 状态转换规则

##### 验收标准

1. 大厅 → 私聊：SHALL 仅通过 `/call <设备名>` 命令触发，IntentClassifier SHALL NOT 自动触发私聊切换
2. 私聊 → 大厅：SHALL 仅通过 `/call all` 命令触发，IntentClassifier SHALL NOT 自动退出私聊
3. 大厅 → 会议：SHALL 通过 `/discuss <话题>` 或 `/discuss @安妮 @小明 <话题>` 命令触发，或 IntentClassifier 以高置信度识别到讨论意图时自动触发。未指定参与者时默认拉入所有在线且 LLM 已配置的设备；指定参与者时仅拉入指定设备（类似"拉小群"）
4. 会议 → 大厅：SHALL 通过 `/stop` 命令触发、LLM Conductor 判断讨论收敛自动结束、或讨论超时（20 分钟）自动结束
5. 私聊 → 会议：SHALL NOT 允许直接转换，用户必须先 `/call all` 返回大厅再发起会议
6. 会议 → 私聊：SHALL NOT 允许直接转换，用户必须先 `/stop` 结束会议再进入私聊。会议中需要跟某台设备（含非参与者）单独交互时，使用 `/ask <设备名> <消息>` 一次性命令，不切换状态
7. THE 状态转换 SHALL 遵循严格的显式信号原则：私聊的进入和退出必须是显式命令，防止 IntentClassifier 误判导致状态混乱
8. WHEN 用户在会议中执行 `/call` 时，SHALL 拒绝并提示："会议进行中，无法切换私聊。使用 `/ask <设备名> <消息>` 临时交互，或 `/stop` 结束会议。"
9. WHEN 用户在私聊中执行 `/discuss` 时，SHALL 拒绝并提示："私聊模式中，无法发起会议。发送 `/call all` 返回大厅后再发起。"

#### 7.3 状态标记与用户感知

##### 验收标准

1. WHEN 用户进入私聊时，SHALL 发送醒目的状态变更通知，包含分隔线、图标、私聊设备名、退出命令提示
2. WHEN 用户在私聊中连续发送 5 条消息后，SHALL 插入一条轻量提醒（如 `当前仍在与安妮的私聊中`），防止用户忘记当前状态
3. WHEN 用户退出私聊或会议返回大厅时，SHALL 发送状态变更通知，包含 图标和当前在线设备列表（名称），让用户立刻知道可以跟谁交互
4. WHEN 用户进入会议时，SHALL 发送状态变更通知，包含分隔线、图标、话题、实际参与者列表（而非所有在线设备）、退出命令提示。若为指定参与者的"小群会议"，SHALL 明确标注哪些设备参与、哪些设备未参与
5. WHEN 会议结束时，SHALL 发送总结和状态变更通知，明确提示已返回大厅
6. THE 大厅模式 SHALL 保持现有的轻量路由提示（如 `已发送到 安妮（检测到前端项目）`），不添加额外状态标记

#### 7.4 各空间状态下的消息路由行为

##### 验收标准

1. **大厅模式**：消息 SHALL 经过 RuleEngine → IntentClassifier 正常路由流程，IntentClassifier 决定发给哪台设备或广播
2. **私聊模式**：所有非命令消息 SHALL 直接发送到私聊目标设备，不经过 IntentClassifier，不记录到公共 ConversationContext（隔离）
3. **会议模式**：消息 SHALL 由 Coordinator 判断是注入讨论（InjectUserInput）还是会议中的 @参与者"小会"交互
4. WHEN 会议模式下用户 @一个或多个参与者设备时，SHALL 将消息发送给所有被 @ 的设备，被 @ 的设备同时回复，回复内容 SHALL 写入会议的 DiscussionContext，其他参与者在下一轮能看到
5. WHEN 私聊模式下目标设备离线时，SHALL 提示用户设备已离线并自动返回大厅（显示在线设备列表）
6. WHEN 会议模式下 Hub 向用户推送每条讨论回复时，SHALL 自动在消息前添加会议上下文前缀，格式为 `会议 | {话题} | 第{N}轮`，让用户始终知道当前处于会议状态、讨论什么话题、进行到第几轮。该前缀由 Hub 在 deliver 时自动拼接，不消耗额外 LLM token

#### 7.6 @ 语义在不同空间状态下的统一定义

##### 验收标准

1. **@ 解析规则**：从消息开头连续匹配 `@name` 序列（以空格分隔），直到遇到非 `@` 开头的文本为止，剩余部分为消息正文。支持单人和多人 @
2. **大厅模式下的 @**：`@name` 为定向发送，仅发给被 @ 的设备，其他设备透明无感，不切换空间状态。支持多人 @（如 `@安妮 @小明 帮我看看这个`），被 @ 的设备并行回复，回复合并后返回给用户
3. **会议模式下的 @**：`@name` 为"小会"交互，仅被 @ 的参与者回复，回复写入 DiscussionContext（其他参与者下一轮能看到）。支持多人 @。@ 非参与者 SHALL 拒绝并提示使用 `/ask`
4. **私聊模式下的 @**：`@` 无特殊路由语义，消息（含 @ 前缀）原样发送给私聊目标设备，当作普通文本处理

#### 7.5 会议参与者隔离

##### 验收标准

1. THE 会议状态 SHALL 维护一个明确的参与者列表（`participants []string`，存储 machineID），仅列表中的设备参与讨论
2. WHEN 用户通过 `/discuss @安妮 @小明 <话题>` 指定参与者时，SHALL 仅将指定设备加入参与者列表；未指定时默认拉入所有在线且 LLM 已配置的设备
3. THE 未参与会议的设备 SHALL 完全不受会议影响：不收到讨论 prompt、不出现在讨论上下文中、不感知会议的存在
4. WHEN 会议进行中用户 @ 的设备中包含非参与者时，SHALL 拒绝整条消息并提示用户："会议进行中，@的设备中包含非参与者。使用 /ask <设备名> <消息> 与非参与者临时交互，或 /stop 结束会议。"
5. WHEN IntentClassifier 自动触发会议时，SHALL 根据消息内容和设备画像智能选择参与者（如"安妮和小明讨论一下前端方案"→ 仅安妮和小明参与），而非默认全员

### 需求 8：讨论模式无感化与 AI 对话感

**用户故事：** 作为用户，我希望会议中的 AI 不是各说各话，而是像真人开会一样能感知彼此、回应对方的观点。

#### 8.1 讨论生命周期自动管理

##### 验收标准

1. WHEN 讨论 goroutine（`runDiscussion` 或 `runConductedDiscussion`）结束时，SHALL 自动从 `discussions` map 中删除该用户的条目（`delete(r.discussions, userID)`），彻底清理状态（修复当前 bug：讨论结束后 `IsInDiscussion` 永远返回 true）
2. THE `StopDiscussion` 方法 SHALL 在 `Running=false` 时直接删除 map 条目并返回（现有行为保留）
3. WHEN LLM 模式启用时，`core.go` 中的 step 3d（讨论模式消息拦截）SHALL 被跳过，所有非命令消息 SHALL 经过 Coordinator 统一处理
4. WHEN LLM 模式未启用时，step 3d SHALL 保持现有行为（向后兼容）
5. THE 讨论结束后 SHALL NOT 保留隐式的"追加话题"状态；用户想继续讨论可以重新发起（在大厅模式下 IntentClassifier 会识别 discuss 意图）

#### 8.2 AI 角色意识与对话感

##### 验收标准

1. WHEN 向设备发送讨论消息时，`im.user_message` 的 payload SHALL 包含 `discussion_context` 字段，内容包括：该设备的角色身份（设备名）、所有参与者列表、当前轮次、话题
2. THE 讨论 prompt SHALL 明确告知每台设备："你是讨论参与者「{设备名}」，其他参与者有 {其他设备名列表}。请以你的角色身份参与讨论。"
3. THE 第 2 轮及之后的 prompt SHALL 包含上一轮所有参与者的发言，并要求 AI 直接回应其他参与者的具体观点（如："小明认为应该用微前端，请针对他的观点进行回应、补充或反驳"）
4. THE 讨论 prompt SHALL 鼓励 AI 之间的交互而非独立陈述，包含指令："不要重复自己之前的观点，重点回应其他参与者的新观点"
5. WHEN 用户在会议中 @一个或多个设备发言时，该发言和所有被 @ 设备的回复 SHALL 作为"小会"记录写入 DiscussionContext，下一轮所有参与者的 prompt 中 SHALL 包含这段"小会"内容（如："主持人与安妮、小明的小会：..."）

#### 8.3 会议中的消息路由

##### 验收标准

1. WHEN 会议进行中（`IsDiscussionRunning` 返回 true）且用户发送非命令消息时，THE Coordinator SHALL 判断消息类型：
   - 以一个或多个 `@参与者设备名` 开头 → 发送给被 @ 的参与者（"小会"），仅被 @ 的设备回复，回复写入 DiscussionContext，其他会议参与者下一轮能看到
   - 支持多人 @：`@安妮 @小明 你们觉得这个方案怎么样？` → 安妮和小明同时收到消息并回复，其他参与者不回复但下一轮能看到内容
   - 解析规则：从消息开头连续匹配 `@name` 序列（以空格分隔），直到遇到非 `@` 开头的文本为止，剩余部分为消息正文
   - 不带 @ 的消息 → 作为人类插话注入讨论（`InjectUserInput`）
2. THE Adapter SHALL 支持 `/ask <设备名> <消息>` 命令，用于在会议或私聊进行中向任意设备（包括非参与者）发送一次性消息。该消息 SHALL 不影响当前空间状态，回复 SHALL 不写入 DiscussionContext，仅直接返回给用户。这解决了会议中临时需要非参与者执行操作的场景（如 `/ask 工作站 跑一下后端测试`）
3. WHEN 会议已结束但 SpaceState 仍为 `meeting` 时（不应发生，但作为防御），SHALL 自动将 SpaceState 重置为 `lobby`
4. THE 会议中的 @参与者"小会"交互 SHALL 不中断正在进行的讨论轮次，回复异步写入 DiscussionContext

#### 8.4 空间状态边界情况

##### 验收标准

1. WHEN 会议进行中某参与者设备离线时，SHALL 将该设备从参与者列表中移除并通知用户（如："安妮已离线，退出会议"）；会议 SHALL 继续进行（2 人也是会议）；仅当所有参与者都离线时 SHALL 自动结束会议并通知用户
2. WHEN 私聊目标设备离线时，SHALL 提示用户设备已离线并自动返回大厅（显示在线设备列表），因为私聊对象已不存在
3. WHEN 所有设备离线时，SHALL 提示用户"所有设备已离线"，SpaceState 重置为 `lobby`，后续消息返回 503（现有行为）
4. WHEN 会议结束时，会议总结 SHALL 写入公共 ConversationContext，使后续大厅模式下的 IntentClassifier 能感知刚才的讨论内容
5. WHEN 会议结束后用户说"继续刚才的讨论"时，IntentClassifier SHALL 识别为 discuss 意图，并 SHALL 根据 ConversationContext 中记录的上次会议参与者自动拉入相同的设备（如果仍在线），用户无需重新指定
