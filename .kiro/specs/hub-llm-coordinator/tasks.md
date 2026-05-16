# 实现计划：Hub LLM 协调器 + 群聊基线优化

## 概述

基于设计文档中定义的组件和接口，将 Hub LLM 协调器和基线优化分解为增量实现步骤。每个步骤构建在前一步骤之上。先实现基础设施（熔断器、配置、设备画像），再实现核心协调层（规则引擎、意图分类、讨论编排、回复合并），最后实现基线优化和 Admin UI。

## 任务

- [x] 1. 基础设施：熔断器 + LLM 配置
  - [x] 1.1 创建 `hub/internal/im/circuit_breaker.go`，实现 CircuitBreaker
    - 实现 Allow()：检查是否允许 LLM 调用（未熔断或冷却期已过）
    - 实现 RecordSuccess()：重置连续失败计数
    - 实现 RecordFailure()：递增失败计数，达到阈值（3）时设置 openUntil = now + 5min
    - 实现 IsOpen()、Status() 查询方法
    - _需求: 9.1, 9.2, 9.3, 9.4, 9.5_

  - [x] 1.2 编写单元测试：CircuitBreaker
    - 测试正常状态下 Allow() 返回 true
    - 测试连续 3 次 RecordFailure 后 Allow() 返回 false
    - 测试冷却期过后 Allow() 恢复为 true（半开状态）
    - 测试 RecordSuccess 重置计数器
    - **Property 5: 熔断器状态机正确性**

  - [x] 1.3 创建 `hub/internal/im/hub_llm_config.go`，实现 HubLLMConfig
    - 定义 HubLLMConfig 结构体（Enabled, APIURL, APIKey, Model, Protocol, SmartRouteSingleDevice）
    - 实现 ToMaclawLLMConfig() 转换方法，复用 corelib.MaclawLLMConfig
    - 实现 MaskAPIKey() 脱敏方法（显示前 4 位 + 掩码 + 后 4 位）
    - _需求: 1.1, 1.2, 1.6_

  - [x] 1.4 新增 Hub LLM 配置 Admin API
    - 在 `hub/internal/httpapi/router.go` 新增路由：GET/PUT `/api/admin/hub_llm_config`、POST `/api/admin/hub_llm_test`
    - GET：从 system_settings 读取配置，APIKey 脱敏返回
    - PUT：保存配置到 system_settings（key: `hub_llm_config`）
    - POST test：调用 DoSimpleLLMRequest 发送简单 prompt 验证连通性，返回成功/失败 + 耗时
    - _需求: 1.1, 1.2, 1.4, 1.5, 1.6_

- [x] 2. 设备画像缓存
  - [x] 2.1 创建 `hub/internal/im/device_profile.go`，实现 DeviceProfileCache
    - 定义 DeviceProfile 结构体（MachineID, Name, LLMConfigured, ProjectPath, Language, Framework, ActiveSessions）
    - 实现 Update(userID, profile)：更新或新增设备画像
    - 实现 Remove(userID, machineID)：移除设备画像
    - 实现 GetAll(userID) []DeviceProfile：获取用户所有在线设备画像
    - 使用 sync.RWMutex 保护并发访问，内存存储不持久化
    - _需求: 2.1, 2.2, 2.4, 2.5_

  - [x] 2.2 编写单元测试：DeviceProfileCache
    - 测试 Update + GetAll 基本流程
    - 测试 Remove 后 GetAll 不包含已移除设备
    - 测试并发安全性
    - **Property 7: 设备画像生命周期**

  - [x] 2.3 扩展 WebSocket Gateway 接收 DeviceProfile
    - 在 `hub/internal/ws/handlers_machine.go` 中处理 `device.profile_update` 消息类型
    - 通过 DeviceProfileUpdater 回调转发到 Coordinator.HandleDeviceProfileUpdate
    - bootstrap.go 中完成 wiring
    - _需求: 2.1, 2.2, 2.3_

- [x] 3. 规则引擎
  - [x] 3.1 创建 `hub/internal/im/rule_engine.go`，实现 RuleEngine
    - 定义 RouteAction 常量：ActionRouteToTarget, ActionBroadcast, ActionNeedClassification, ActionPassthrough
    - 定义 RouteDecision 结构体
    - 实现 Evaluate() 方法，按优先级评估：
      - @昵称 前缀 → route_to_target
      - 单设备 + smart_route_single_device=false → route_to_target
      - 已选定设备（非广播模式）→ route_to_target
      - 广播模式 → broadcast
      - 以上均未命中 + LLM 已配置 → need_classification
      - 以上均未命中 + 无 LLM → passthrough
    - 纯内存操作，零 I/O
    - _需求: 3.1, 3.2, 3.3, 3.4_

  - [x] 3.2 编写单元测试：RuleEngine
    - 测试每条规则的命中场景
    - 测试规则优先级（高优先级规则命中时不触发低优先级）
    - 测试 smart_route_single_device 开关行为
    - 测试无 LLM 时降级为 passthrough
    - **Property 2: 规则引擎零 I/O**
    - **Property 3: 规则引擎优先级正确性**
    - **Property 8: 单设备开关行为**

- [x] 4. 检查点 - 确保所有测试通过
  - 运行 `go test ./hub/internal/im/...` 确保所有测试通过

- [x] 5. LLM 意图分类器
  - [x] 5.1 创建 `hub/internal/im/intent_classifier.go`，实现 IntentClassifier
    - 定义 IntentType 常量：IntentRouteSingle, IntentBroadcast, IntentDiscuss, IntentNeedClarification
    - 定义 IntentResult 结构体
    - 实现 Classify() 方法：
      - 构建 LLM prompt（设备画像 + 最近 3 条路由历史 + 用户消息）
      - 调用 agent.DoSimpleLLMRequest（复用 OpenClaw UA）
      - 解析 JSON 响应为 IntentResult
      - 5 秒超时，超时降级为 broadcast
      - LLM 错误时触发 CircuitBreaker.RecordFailure()
    - 实现意图缓存（最近 10 条，基于 userID + machineSet + textHash）
    - _需求: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8_

  - [x] 5.2 编写单元测试：IntentClassifier
    - 使用 mock LLM 测试各种意图分类结果
    - 测试超时降级为 broadcast
    - 测试 JSON 解析失败降级
    - 测试缓存命中和缓存失效
    - **Property 4: 意图分类超时降级**
    - **Property 6: 意图分类缓存一致性**

- [x] 6. Coordinator 核心调度
  - [x] 6.1 创建 `hub/internal/im/coordinator.go`，实现 Coordinator
    - 组合 MessageRouter、DeviceFinder、RuleEngine、IntentClassifier、CircuitBreaker、configProvider
    - 实现 Coordinate() 方法：
      - 获取 LLM 配置（configProvider 动态读取）
      - 调用 RuleEngine.Evaluate()
      - 根据 RouteAction 分发：
        - route_to_target → MessageRouter.routeToSingleMachine()
        - broadcast → MessageRouter.routeBroadcast()（经 ReplyMerger）
        - need_classification → IntentClassifier.Classify() → 根据 IntentResult 分发
        - passthrough → MessageRouter.RouteToAgent()（现有逻辑）
      - IntentResult.discuss → 启动 DiscussionConductor
      - IntentResult.need_clarification → 返回提示消息
    - 实现 IsLLMEnabled()、GetLLMStatus()
    - 实现 UpdateDeviceProfile()、RemoveDeviceProfile()
    - _需求: 1.3, 3.1, 3.2, 4.7, 4.8, 7.1, 7.2, 7.3, 7.4_

  - [x] 6.2 编写单元测试：Coordinator
    - 测试无 LLM 配置时完全 passthrough
    - 测试规则命中时不调用 LLM
    - 测试 LLM 意图分类各种结果的分发
    - 测试熔断状态下降级为 passthrough
    - **Property 1: Passthrough 等价性**
    - **Property 9: 命令系统始终有效**

  - [x] 6.3 修改 `hub/internal/im/core.go`，集成 Coordinator
    - Adapter 新增 `coordinator *Coordinator` 字段和 `SetCoordinator()` 方法
    - HandleMessage 中非命令消息改为调用 `coordinator.Coordinate()` 替代 `messageRouter.RouteToAgent()`
    - 如果 coordinator 为 nil（未注入），保持现有行为不变
    - _需求: 7.1, 7.2, 7.5, 7.6_

  - [x] 6.4 修改 `hub/internal/app/bootstrap.go`，注入 Coordinator
    - 创建 llmConfigProvider 闭包（从 system_settings 读取 hub_llm_config）
    - 创建 Coordinator 实例
    - 调用 imAdapter.SetCoordinator(coordinator)
    - _需求: 1.3, 1.4_

- [x] 7. 检查点 - 核心协调层集成测试
  - 运行 `go test ./hub/internal/im/...` 确保所有测试通过
  - 验证无 LLM 配置时行为与当前完全一致

- [x] 8. LLM 讨论编排器
  - [x] 8.1 创建 `hub/internal/im/discussion_conductor.go`，实现 DiscussionConductor
    - 定义 ConductedDiscussionState、ConductedRound 结构体
    - 实现 StartConductedDiscussion()：启动 LLM 编排的讨论
    - 实现 runConductedDiscussion()：后台讨论循环
      - 每轮结束后调用 LLM 决定下一步动作（ask_specific / cross_review / summarize / conclude）
      - 支持用户插话（UserInputCh）
      - 最大 10 轮上限
      - LLM 编排失败时降级为现有机械式轮次逻辑
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [x] 8.2 编写单元测试：DiscussionConductor
    - 使用 mock LLM 测试讨论编排流程
    - 测试用户插话注入
    - 测试轮次上限自动结束
    - 测试 LLM 失败降级为机械式轮次

  - [x] 8.3 修改 `hub/internal/im/discussion.go`，集成 DiscussionConductor
    - StartDiscussion 中检查 LLM 是否可用
    - LLM 可用时委托给 DiscussionConductor
    - LLM 不可用时保持现有机械式轮次逻辑
    - ✅ 已完成：conductor 通过 MessageRouter.SetConductor 注入，StartDiscussion 自动委托
    - _需求: 5.1, 5.4_

- [x] 9. LLM 回复合并器
  - [x] 9.1 创建 `hub/internal/im/reply_merger.go`，实现 ReplyMerger
    - 定义 DeviceReply 结构体
    - 实现 MergeReplies() 方法：
      - 只有 1 个回复 → 直接返回
      - 多个回复前 100 字符相同 → 返回第一个 + "其他设备观点一致"
      - 多个不同回复 + LLM 可用 → 调用 LLM 合并（去重、保留独特观点、标注来源）
      - LLM 不可用 → 使用 BroadcastFormatter 结构化格式
    - 10 秒超时等待所有设备回复
    - _需求: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [x] 9.2 编写单元测试：ReplyMerger
    - 测试单回复直接返回
    - 测试相似回复去重
    - 测试 LLM 合并（mock）
    - 测试 LLM 不可用时降级

- [x] 10. 讨论记忆与上下文
  - [x] 10.1 在 Coordinator 中实现讨论历史存储和检索
    - 讨论结束时调用 LLM 提取关键结论，生成 DiscussionSummaryEntry
    - 存储到 system_settings（key: `discussion_history_{userID}`），最多 20 条
    - 新讨论开始时检索相关历史摘要注入 LLM 上下文
    - _需求: 8.1, 8.2, 8.3, 8.4, 8.5_

  - [x] 10.2 编写单元测试：讨论历史
    - 测试存储和检索
    - 测试 20 条上限淘汰
    - **Property 12: 讨论历史上限**

- [x] 11. 检查点 - LLM 功能完整性测试
  - 运行 `go test ./hub/internal/im/...` 确保所有测试通过

- [x] 12. 基线优化：命令帮助
  - [x] 12.1 创建 `hub/internal/im/help.go`，实现 BuildHelpMessage
    - 根据用户状态动态生成帮助消息
    - 单设备用户：不显示 /call all 和 /discuss
    - 广播模式：提示 /call <name> 切回单聊
    - LLM 已配置：提示无感智能模式已启用
    - 包含使用示例
    - _需求: 14.1, 14.2, 14.4_

  - [x] 12.2 修改 `hub/internal/im/core.go`，新增命令处理
    - 新增 `/help` 命令处理
    - 新增 `/rounds N` 命令处理（动态调整讨论轮数）
    - 无法识别的 `/` 命令返回友好错误 + 建议 `/help`
    - _需求: 14.1, 14.3, 13.3_

- [x] 13. 基线优化：广播回复格式化
  - [x] 13.1 创建 `hub/internal/im/broadcast_formatter.go`，实现 FormatBroadcastReply
    - 结构化分隔线 + 设备名称标题
    - 相似回复去重（前 100 字符比较）
    - 末尾摘要统计（参与设备数、成功数、失败数）
    - 错误信息集中在末尾"异常"区域
    - _需求: 12.1, 12.2, 12.3, 12.4_

  - [x] 13.2 编写单元测试：BroadcastFormatter
    - 测试正常多设备回复格式
    - 测试相似回复去重
    - 测试包含错误的回复格式
    - **Property 10: 广播回复格式完整性**

  - [x] 13.3 修改 `hub/internal/im/router.go`，集成 BroadcastFormatter
    - routeBroadcast 使用 FormatBroadcastReply 替代现有的简单拼接
    - ✅ 已完成：routeBroadcast 收集 DeviceReply 后调用 FormatBroadcastReply
    - _需求: 12.1_

- [x] 14. 基线优化：讨论格式优化
  - [x] 14.1 创建 `hub/internal/im/discussion_formatter.go`，实现讨论格式化
    - FormatRoundSummary：每轮简要小结（各设备观点一句话概括）
    - FormatDiscussionSummary：结构化总结（共识点、分歧点、待定事项）
    - _需求: 13.1, 13.2, 13.4_

  - [x] 14.2 修改 `hub/internal/im/discussion.go`，集成讨论格式优化
    - 每轮开始时发送 prompt 摘要
    - 每轮结束后发送 FormatRoundSummary
    - 总结使用 FormatDiscussionSummary
    - 超时设备跳过而非传递超时信息
    - ✅ 已完成：runDiscussion 每轮后调用 FormatRoundSummary，总结使用 FormatDiscussionSummary
    - _需求: 13.1, 13.2, 13.4, 13.5_

- [x] 15. 基线优化：设备状态通知
  - [x] 15.1 创建 `hub/internal/im/device_notifier.go`，实现 DeviceNotifier
    - 实现 NotifyDeviceOnline / NotifyDeviceOffline（30 秒防抖）
    - 实现 MarkUserActive（只通知有过 IM 交互的用户）
    - 当前选定设备离线时额外提示切换
    - _需求: 15.1, 15.2, 15.3, 15.4_

  - [x] 15.2 编写单元测试：DeviceNotifier
    - 测试正常上下线通知
    - 测试 30 秒防抖（快速上下线只发最终状态）
    - 测试只通知活跃用户
    - **Property 11: 设备通知防抖**

  - [x] 15.3 修改 `hub/internal/ws/handlers_machine.go`，集成 DeviceNotifier
    - handleMachineHello 中调用 DeviceNotifyFunc.OnConnect
    - cleanupConnection 中调用 DeviceNotifyFunc.OnDisconnect
    - bootstrap.go 中通过 SetDeviceNotifyHook 完成 wiring
    - _需求: 15.1_

- [x] 16. 检查点 - 基线优化完整性测试
  - 运行 `go test ./hub/internal/im/...` 确保所有测试通过

- [x] 17. Admin 页面 LLM 配置 UI
  - [x] 17.1 修改 `hub/web/admin/index.html`，新增 Hub LLM 配置卡片
    - 启用开关、API Base URL 输入框、API Key 输入框（密码模式）、Model Name 输入框
    - Protocol 选择（OpenAI / Anthropic）
    - SmartRouteSingleDevice 开关
    - "测试连接"按钮（调用 /api/admin/hub_llm_test）
    - LLM 状态指示器（🟢 正常 / 🟡 熔断中 / ⚪ 未配置）
    - 新增 GET `/api/admin/hub_llm_status` 端点返回 Coordinator.GetLLMStatus()
    - _需求: 10.1, 10.2, 10.3, 10.4_

- [x] 18. 无感智能模式欢迎消息
  - [x] 18.1 在 Coordinator 中实现首次进入无感模式的欢迎消息
    - 用户首次发消息且 LLM 已配置 + 多设备在线时，发送欢迎消息
    - 说明当前处于智能模式，可直接发消息，也可使用命令手动控制
    - 使用 per-user 标记避免重复发送（内存，Hub 重启后重新发送一次无妨）
    - _需求: 7.7, 11.5_

- [x] 19. 最终集成测试
  - ✅ `go test ./hub/...` 全部通过
  - 手动验证：无 LLM 配置时行为与当前完全一致
  - 手动验证：配置 LLM 后无感智能模式正常工作
