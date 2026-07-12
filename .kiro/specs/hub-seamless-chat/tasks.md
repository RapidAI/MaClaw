# 实现任务：Hub 无感聊天（Seamless Chat）

## 任务概览

基于 requirements.md 和 design.md，按依赖顺序分解为以下实现任务。

## 任务列表

### Task 1: 基础组件 — mention_parser.go
- [x] 新建 `hub/internal/im/mention_parser.go`
- [x] 实现 `ParseMentions(text string) (names []string, body string)`
- [x] 从消息开头连续匹配 `@name` 序列，直到遇到非 `@` 开头的文本

### Task 2: 基础组件 — space_state.go
- [x] 新建 `hub/internal/im/space_state.go`
- [x] 实现 SpaceStateType、SpaceState、spaceStateStore
- [x] 实现 GetOrCreate、EnterPrivate、ExitPrivate、EnterMeeting、ExitMeeting、RemoveParticipant、Reset
- [x] 状态转换矩阵：lobby↔private、lobby↔meeting，private↔meeting 不可直达

### Task 3: 基础组件 — conversation_context.go
- [x] 新建 `hub/internal/im/conversation_context.go`
- [x] 实现 ConversationContext、ConversationRound、conversationContextStore
- [x] 实现 GetOrCreate、RecordRound（异步摘要生成，3s 超时降级）、GetRecentSummaries、BuildHandoffContext（≤500 字符）、Clear、FormatDisplay
- [x] 摘要生成：LLM 可用时调用，否则截断前 100 字符

### Task 4: 讨论 bug 修复 — discussion.go + discussion_conductor.go
- [x] `discussion.go` runDiscussion defer 中新增 `delete(r.discussions, userID)`
- [x] `discussion_conductor.go` runConductedDiscussion defer 中新增 `delete(dc.router.discussions, userID)`

### Task 5: IntentClassifier 扩展 — intent_classifier.go
- [x] 新增 `IntentDirectAnswer` 意图类型
- [x] prompt 注入 ConversationContext 摘要（最近 3 轮）
- [x] 新增 direct_answer 判断规则
- [x] 上下文延续性指令（指代词优先路由到上一轮设备）

### Task 6: Coordinator 扩展 — coordinator.go
- [x] 集成 spaceStateStore 和 conversationContextStore
- [x] Coordinate 方法按空间状态分发：lobby/private/meeting
- [x] 新增 handlePrivateMessage（直接发给目标，不记录公共上下文，每 5 条提醒）
- [x] 新增 handleMeetingMessage（ParseMentions → @小会 / InjectUserInput）
- [x] 新增 handleLobbyMessage（现有 classifyAndRoute + @ 多人定向 + direct_answer + discuss 自动触发）
- [x] 新增 hubDirectAnswer（Hub LLM 直接回答，15s 超时降级，need_device 降级）
- [x] SessionHandoff：routeToTarget 检测设备切换，注入 handoff_context
- [x] RecordRound 异步记录到 ConversationContext

### Task 7: RuleEngine 扩展 — rule_engine.go
- [x] Rule 1 使用 ParseMentions 处理多人 @
- [x] 新增 ActionRouteToMultiple 动作类型
- [x] 多人 @ 返回 targets 列表

### Task 8: core.go 命令扩展
- [x] 新增 /ask 命令（一次性跨空间交互）
- [x] 新增 /context 和 /context clear 命令
- [x] /call 增加 SpaceState 校验（会议中拒绝，私聊中 /call all 退出私聊）
- [x] /discuss 增加 SpaceState 校验（私聊中拒绝）+ 参与者解析（@name）
- [x] /stop 扩展：退出会议回大厅
- [x] step 3d LLM 模式跳过

### Task 9: 讨论增强 — discussion_conductor.go
- [x] askDevices 注入 discussion_context（role、participants、round、topic、instruction）
- [x] deliverRoundReplies 添加会议上下文前缀 `会议 | {话题} | 第{N}轮`
- [x] 小会写入 DiscussionContext

### Task 10: router.go 扩展
- [x] routeToSingleMachine 支持 handoff_context 注入到 payload
- [x] 新增 routeToMultiple 方法（并行发送，合并回复）

### Task 11: help.go 更新
- [x] BuildHelpMessage 增加空间模型命令说明（/ask、/context、空间状态提示）

### Task 12: HTTP API + Bootstrap
- [x] hub_llm_handlers.go 新增 GET /api/admin/conversation_stats 端点
- [x] bootstrap.go 初始化 ConversationContext 和 SpaceState 存储，注入 Coordinator
