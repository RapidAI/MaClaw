# Implementation Plan: VE Workflow Runtime（审批工作流运行时）

## Overview

本实现计划将审批工作流运行时设计方案转化为可执行的编码任务。核心策略：先构建 Hub 侧基础设施（FormValidator、数据库 schema 扩展、ConfirmationStore），再实现 RuntimeAPI 和 NotificationDispatcher，然后构建 ConfirmationTracker 和 DirectoryService，接着实现 IM 快速发起和撤回功能，最后构建桌面端 WorkflowDirectory 面板和 Terminal Node 配置面板。每个任务聚焦单一模块，按依赖顺序排列。

## Tasks

- [x] 1. 数据库 Schema 扩展 — 新增表和字段
  - [x] 1.1 扩展 workflow_instances 表，新增 initiator_id、initiation_channel、form_data、workflow_name、withdrawn_at、withdrawn_by 字段和索引
    - 创建 migration SQL 文件 `hub/internal/workflow/migrations/003_runtime_extensions.sql`
    - 新增 `InstanceWithdrawn` 和 `InstanceCancelled` 状态常量到 `hub/internal/workflow/instance.go`
    - _Requirements: 1.4, 1.6, 4.1, 13.2_

  - [x] 1.2 创建 confirmations 表（id, instance_id, recipient_id, type, status, notes, timeout_hours, max_reminders, reminders_sent, reminder_interval_hours, last_reminder_at, confirmed_at, auto_closed_at, auto_close_reason, created_at）及索引
    - 创建 `hub/internal/workflow/confirmation_store.go`，实现 `ConfirmationStore` 接口（Create/Get/UpdateStatus/IncrementReminders/ListPending/ListByInstance/FindOverdue）
    - _Requirements: 7.1, 8.1, 14.4, 14.5_

  - [x] 1.3 创建 notifications 表（id, instance_id, type, recipient_id, channel, payload_json, delivered, delivered_at, failure_reason, created_at）及索引
    - 创建 `hub/internal/workflow/notification_store.go`，实现 NotificationStore 接口
    - _Requirements: 5.3, 6.3, 6.6_

  - [ ]* 1.4 编写数据库 schema 单元测试
    - 验证 migration 执行成功、字段约束、索引存在性
    - _Requirements: 4.1, 4.5_

- [x] 2. FormValidator — Schema 驱动的表单验证
  - [x] 2.1 实现 `hub/internal/workflow/form_validator.go`
    - 定义 `FormFieldSchema`（Name/Label/Type/Required/MaxLength/MinValue/MaxValue/Options/Pattern）
    - 定义 `FieldType` 常量（text/number/date/select/file/textarea/boolean）
    - 定义 `ValidationError` 结构体
    - 实现 `FormValidator.Validate(formData map[string]interface{}, schema []FormFieldSchema) []ValidationError`
    - 实现 `ExtractFormSchema(graph *WorkflowGraph) ([]FormFieldSchema, error)`
    - _Requirements: 1.2, 1.3, 3.2, 3.4_

  - [ ]* 2.2 编写 Property Test：表单验证正确性
    - **Property 1: Form validation correctness**
    - 使用 `pgregory.net/rapid` 生成随机 schema 和 form data
    - 验证：所有 required 字段存在且类型正确时 → 无 error；缺失 required 字段或类型不匹配时 → 有对应 error
    - **Validates: Requirements 1.2, 1.3, 3.2, 3.4**

  - [ ]* 2.3 编写单元测试：FormValidator 边界情况
    - 测试：空 schema、全 optional 字段、pattern 正则匹配、select 选项验证、number 范围验证、MaxLength 截断检测
    - _Requirements: 1.2, 1.3_

- [x] 3. RuntimeAPI — HTTP 路由和 Initiation 处理
  - [x] 3.1 创建 `hub/internal/workflow/api_runtime.go`，实现 `RuntimeAPI` 结构体和 `RegisterRoutes`
    - 注册路由：POST /api/v1/workflows/{id}/initiate、POST /api/v1/instances/{id}/withdraw、POST /api/v1/confirmations/{id}/confirm、GET /api/v1/confirmations/pending、GET /api/v1/directory/initiated、GET /api/v1/directory/pending-action、GET /api/v1/directory/pending-confirmation、GET /api/v1/directory/completed
    - _Requirements: 1.1, 3.1_

  - [x] 3.2 实现 `handleInitiateWorkflow` handler
    - 解析 `InitiateWorkflowRequest`（form_data, channel）
    - 调用 `FormValidator.Validate` 验证表单数据
    - 验证通过后调用 `WorkflowExecutor.StartInstance` 创建实例
    - 持久化完整 Form_Data（含 initiator_id, submission_timestamp UTC ms, version_id, channel）
    - 返回 `InitiateWorkflowResponse`（instance_id, status, created_at, version_id）
    - _Requirements: 1.4, 1.5, 1.6, 2.6, 3.2, 3.5_

  - [x] 3.3 实现 API 认证和速率限制
    - Bearer token 认证（复用 Hub 现有 auth middleware）
    - 速率限制：100 requests/minute per authenticated client（token bucket）
    - 返回 HTTP 401/429 对应错误
    - _Requirements: 3.1, 3.3, 3.6_

  - [ ]* 3.4 编写 Property Test：实例创建数据完整性
    - **Property 2: Instance creation data completeness**
    - 使用 rapid 生成随机 form_data 和 channel
    - 验证：成功创建的实例记录包含所有必需字段（form_data, initiator_id, timestamp UTC ms, version_id, channel）
    - **Validates: Requirements 1.4, 1.6, 2.6**

  - [ ]* 3.5 编写 API 集成测试
    - 测试完整 initiation 流程：valid request → 201、invalid form → 400、bad auth → 401、rate limit → 429
    - _Requirements: 1.1, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 4. Checkpoint — 确保基础设施测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. NotificationDispatcher — 双通道通知分发
  - [x] 5.1 创建 `hub/internal/workflow/notification_dispatcher.go`
    - 定义 `NotificationDispatcher` 结构体（hubNotifier, imPusher, auditStore）
    - 定义 `HubInAppNotifier` 和 `IMPushNotifier` 接口
    - 定义 `NotificationType` 常量（result_executor/notifier/withdrawal/reminder/escalation）
    - 定义 `WorkflowNotification` 结构体
    - _Requirements: 5.1, 5.3, 6.1, 6.3_

  - [x] 5.2 实现 `Dispatch` 和 `DispatchBatch` 方法
    - 同时尝试 Hub in-app 和 IM push 两个通道
    - IM 通道未连接时仅通过 Hub in-app 发送，记录 IM 失败到 Instance_Timeline
    - 记录 delivery 状态到 notifications 表
    - 60 秒内完成通知发送
    - _Requirements: 5.1, 5.3, 5.6, 6.1, 6.3_

  - [ ]* 5.3 编写 Property Test：通知投递完整性
    - **Property 5: Terminal node notification delivery**
    - 使用 rapid 生成随机 executor/notifier 配置
    - 验证：每个配置的 recipient 都收到通知，两个通道都被尝试
    - **Validates: Requirements 5.1, 5.3, 6.1, 6.3**

  - [ ]* 5.4 编写 Property Test：通知内容完整性
    - **Property 6: Notification content completeness by type**
    - 验证：executor 通知包含 workflow_name/result/form_data_summary/initiator/url；notifier 通知包含 workflow_name/result/form_data_summary/url
    - **Validates: Requirements 5.2, 6.2, 6.6**

- [x] 6. ConfirmationTracker — 确认追踪、提醒和升级
  - [x] 6.1 创建 `hub/internal/workflow/confirmation_tracker.go`
    - 定义 `ConfirmationTracker` 结构体（store, notifDispatcher, auditStore, ticker 5min）
    - 定义 `ConfirmationType`（executor/notifier）、`ConfirmationStatus`（pending/confirmed/auto_closed）
    - 定义 `Confirmation` 结构体
    - _Requirements: 7.1, 7.3, 8.1, 8.2_

  - [x] 6.2 实现 `StartTracking` 方法
    - 从 TerminalNodeConfig 中提取 executor/notifier 配置
    - 为每个 executor 和 notifier 创建 Confirmation 记录（pending 状态）
    - 设置 timeout_hours、max_reminders、reminder_interval_hours
    - _Requirements: 7.1, 8.1, 14.4, 14.5_

  - [x] 6.3 实现 `Confirm` 方法
    - 验证 confirmationID 存在且 recipientID 匹配
    - 更新状态为 confirmed，记录 notes（executor 最多 2000 字符）
    - 记录 audit trail 事件
    - _Requirements: 7.1, 7.2, 8.1_

  - [x] 6.4 实现 `RunReminderLoop` 后台 goroutine
    - 每 5 分钟检查 overdue confirmations
    - 逻辑：reminders_sent < max_reminders AND 距上次提醒 >= reminder_interval_hours → 发送提醒
    - Executor 提醒耗尽 → 通知 Initiator（escalation_triggered 事件）
    - Notifier 提醒耗尽 → auto-close（auto_closed 事件，reason=notifier_timeout）
    - _Requirements: 7.3, 7.4, 7.5, 8.2, 8.3, 8.4_

  - [ ]* 6.5 编写 Property Test：提醒频率和上限
    - **Property 8: Reminder frequency and cap invariant**
    - 使用 rapid 生成随机 timeout/max_reminders/interval 配置
    - 模拟时间推进，验证：提醒间隔正确、总数不超过 max_reminders、timeout 前不发送
    - **Validates: Requirements 7.3, 7.4, 8.2, 8.3**

  - [ ]* 6.6 编写 Property Test：Executor 升级触发
    - **Property 9: Escalation on executor reminder exhaustion**
    - 验证：reminders_sent >= max_reminders 且未确认 → 通知 Initiator + 记录 escalation_triggered
    - **Validates: Requirements 7.5**

  - [ ]* 6.7 编写 Property Test：Notifier 自动关闭
    - **Property 10: Auto-close on notifier reminder exhaustion**
    - 验证：reminders_sent >= max_reminders 且未确认 → status=auto_closed + reason=notifier_timeout + 记录事件
    - **Validates: Requirements 8.4**

  - [ ]* 6.8 编写 Property Test：确认记录完整性
    - **Property 7: Confirmation recording completeness**
    - 验证：每条确认记录包含 recipient_id/timestamp/type；executor 确认包含 notes（≤2000 chars）
    - **Validates: Requirements 7.1, 8.1**

- [x] 7. DirectoryService — 4 个分类视图
  - [x] 7.1 创建 `hub/internal/workflow/directory.go`
    - 定义 `DirectoryService` 结构体（instanceStore, confirmStore, nodeExecStore）
    - 定义 `DirectoryFilter`（Status/DateFrom/DateTo/WorkflowType/Role/Result/Page/PageSize）
    - 定义 `DirectoryItem` 结构体
    - _Requirements: 9.1, 10.1, 11.1, 12.1_

  - [x] 7.2 实现 `MyInitiated` 方法
    - 查询 initiator_id == userID 的实例
    - 支持 status/date_range/workflow_type 过滤
    - 默认按 initiation date 降序，分页 20 条/页
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [x] 7.3 实现 `PendingMyAction` 方法
    - 查询有 pending approval node 分配给 userID 的实例
    - 计算 urgency（normal/approaching_timeout/overdue）
    - 按 urgency 降序 + submission date 升序排序
    - _Requirements: 10.1, 10.2, 10.3_

  - [x] 7.4 实现 `PendingMyConfirmation` 方法
    - 查询 confirmations 表中 recipient_id == userID 且 status == pending 的记录
    - 计算 time_remaining
    - 按 time_remaining 升序排序
    - _Requirements: 11.1, 11.2, 11.3_

  - [x] 7.5 实现 `Completed` 方法
    - 查询 terminal 状态实例中 userID 参与过的（initiator/approver/executor/notifier）
    - 支持 date_range/workflow_type/result/role 过滤
    - 默认按 completion date 降序，分页 20 条/页
    - _Requirements: 12.1, 12.2, 12.3, 12.4_

  - [x] 7.6 实现 Directory API handlers
    - `handleMyInitiated`、`handlePendingMyAction`、`handlePendingMyConfirmation`、`handleCompleted`
    - 解析 query params 为 DirectoryFilter，调用 DirectoryService，返回 JSON
    - _Requirements: 9.5, 10.4, 11.4_

  - [ ]* 7.7 编写 Property Test：Directory 视图查询正确性
    - **Property 11: Directory view query correctness**
    - 使用 rapid 生成随机实例集合（不同 initiator/approver/executor/notifier）
    - 验证：每个视图返回正确的子集
    - **Validates: Requirements 9.1, 10.1, 11.1, 12.1**

  - [ ]* 7.8 编写 Property Test：Directory 过滤正确性
    - **Property 12: Directory filter correctness**
    - 使用 rapid 生成随机过滤条件组合
    - 验证：返回的每条记录都满足所有 active filter
    - **Validates: Requirements 9.3, 12.3**

  - [ ]* 7.9 编写 Property Test：Directory 排序正确性
    - **Property 13: Directory sort ordering**
    - 验证：我发起的/已完成的按日期降序；待我处理的按 urgency+date 升序；待我确认的按 time_remaining 升序
    - **Validates: Requirements 9.4, 10.3, 11.3, 12.4**

- [x] 8. Checkpoint — 确保核心服务测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. WithdrawalHandler — 撤回前置条件和原子状态变更
  - [x] 9.1 创建 `hub/internal/workflow/withdrawal.go`
    - 定义 `WithdrawalHandler` 结构体（instanceStore, auditStore, notifDispatcher, confirmTracker）
    - 定义 `ErrAlreadyCompleted` 和 `ErrNotInitiator` 错误
    - _Requirements: 13.1, 13.4_

  - [x] 9.2 实现 `Withdraw` 方法
    - 前置条件检查：instance status == running、未到达 terminal node、requester == initiator
    - 原子操作：设置 status=withdrawn、取消所有 pending approval nodes（status=skipped）、记录 withdrawal 事件
    - 60 秒内通知所有有 pending actions 的参与者
    - 通知内容：workflow_name, initiator_name, withdrawn_at, "无需进一步操作"
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6_

  - [x] 9.3 实现 `handleWithdrawInstance` API handler
    - 解析 instance_id 和 auth user_id
    - 调用 WithdrawalHandler.Withdraw
    - 返回 200 成功 / 403 非发起人 / 409 已完成或已撤回
    - _Requirements: 13.1, 13.4_

  - [ ]* 9.4 编写 Property Test：撤回前置条件
    - **Property 14: Withdrawal precondition enforcement**
    - 使用 rapid 生成不同状态的实例
    - 验证：running + 未到 terminal + initiator → 成功；其他情况 → 拒绝
    - **Validates: Requirements 13.1, 13.4**

  - [ ]* 9.5 编写 Property Test：撤回原子性和完整性
    - **Property 15: Withdrawal atomicity and completeness**
    - 验证：成功撤回后 status=withdrawn、pending nodes=skipped、audit 有 withdrawal 事件、所有参与者被通知
    - **Validates: Requirements 13.2, 13.3, 13.5, 13.6**

- [x] 10. WorkflowInitiationHandler — IM 快速发起
  - [x] 10.1 创建 `gui/im_message_handler_workflow_initiate.go`
    - 定义 `WorkflowInitiationHandler` 结构体（app, hubClient, sessions map）
    - 定义 `initiationSession`（UserID/WorkflowID/WorkflowName/Schema/ExtractedData/MissingFields/Confirmed/CreatedAt）
    - _Requirements: 2.1, 2.2_

  - [x] 10.2 实现 `HandleInitiationIntent` 方法
    - 匹配用户消息到已发布工作流（关键词匹配 workflow name + form field labels）
    - 使用 VE agent loop（NLU）从自然语言提取 Form_Data 字段
    - 提取成功 → 展示给用户确认
    - 缺失字段 → 追问用户
    - 无匹配工作流 → 告知用户并建议可用工作流
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [x] 10.3 实现确认和提交流程
    - 用户确认 → 调用 Hub API POST /api/v1/workflows/{id}/initiate 创建实例
    - 用户修改 → 更新 ExtractedData，重新展示
    - 创建成功 → 回复用户"✅ 审批已发起，单号：WF-{date}-{seq}"
    - _Requirements: 2.3, 2.6_

  - [ ]* 10.4 编写单元测试：IM 发起流程
    - 测试：关键词匹配、字段提取、缺失字段追问、确认提交、无匹配工作流提示
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

- [x] 11. TerminalNodeConfig — 图形编辑器中的 Executor/Notifier 配置
  - [x] 11.1 创建 `hub/internal/workflow/terminal_node.go`
    - 定义 `TerminalNodeConfig`（ResultExecutors []ExecutorConfig, Notifiers []NotifierConfig）
    - 定义 `ExecutorConfig`（UserID, TimeoutHours 1-720 default 48, MaxReminders 1-10 default 3, ReminderInterval default 24）
    - 定义 `NotifierConfig`（UserID, TimeoutHours 1-720 default 72, MaxReminders 1-10 default 2, ReminderInterval default 24）
    - 定义 `NodeTypeTerminal` 常量
    - _Requirements: 14.1, 14.4, 14.5_

  - [x] 11.2 实现 Terminal Node 配置验证
    - timeout_hours 范围 [1, 720]
    - max_reminders 范围 [1, 10]
    - 无 executor 时显示 warning（非 error）
    - _Requirements: 14.3, 14.4, 14.5_

  - [ ]* 11.3 编写 Property Test：Terminal Node 配置范围验证
    - **Property 16: Terminal node configuration range validation**
    - 使用 rapid 生成随机 timeout/max_reminders 值
    - 验证：[1,720] 和 [1,10] 范围内接受，范围外拒绝
    - **Validates: Requirements 14.4, 14.5**

- [x] 12. Checkpoint — 确保所有后端服务测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 13. WorkflowExecutor 扩展 — Terminal Node 处理
  - [x] 13.1 扩展 `hub/internal/workflow/executor.go` 的 node 执行逻辑
    - 新增 `NodeTypeTerminal` 分支处理
    - Terminal node 到达时：更新 instance status=completed、记录 instance_completed 事件
    - 调用 `NotificationDispatcher.DispatchBatch` 通知所有 executors 和 notifiers
    - 调用 `ConfirmationTracker.StartTracking` 创建确认记录
    - _Requirements: 5.1, 5.5, 6.1, 6.5_

  - [x] 13.2 实现 Node Execution 记录
    - 每个 node 完成时创建 node_execution 记录（instance_id, node_id, node_type, start_at, completed_at, status）
    - 记录 audit_trail 事件（node_completed）
    - _Requirements: 4.1, 4.2_

  - [ ]* 13.3 编写 Property Test：Node 执行记录不变量
    - **Property 3: Node execution recording invariant**
    - 验证：每个完成的 node 都有对应的 node_execution 记录，包含所有必需字段
    - **Validates: Requirements 4.1, 4.2**

  - [ ]* 13.4 编写 Property Test：Instance Timeline 时序
    - **Property 4: Instance timeline chronological ordering**
    - 使用 rapid 生成随机事件序列
    - 验证：查询返回的事件严格按 timestamp 非递减排序
    - **Validates: Requirements 4.3**

- [x] 14. 桌面端 WorkflowDirectory 面板
  - [x] 14.1 创建 `gui/app_workflow_directory.go`
    - 实现 Wails binding `GetWorkflowDirectory(view string, filter string) (*DirectoryResponse, error)`
    - view: "initiated" | "pending_action" | "pending_confirmation" | "completed"
    - filter: JSON-encoded DirectoryFilter
    - 通过 HubClient 调用 Hub Directory API
    - _Requirements: 9.5_

  - [x] 14.2 创建 `gui/frontend/src/components/workflow/WorkflowDirectoryPanel.tsx`
    - Tab 栏：我发起的 | 待我处理的 | 待我确认的 | 已完成的
    - 每个 Tab 渲染列表（workflow_name, status, date, urgency 等）
    - 点击跳转到 Hub 实例详情页
    - _Requirements: 9.5, 10.4, 11.4_

  - [x] 14.3 实现过滤和分页 UI
    - 状态过滤器、日期范围选择器、工作流类型下拉
    - 分页控件（20 条/页）
    - _Requirements: 9.3, 9.4, 12.3, 12.4_

  - [ ]* 14.4 编写前端组件测试
    - 测试：Tab 切换、列表渲染、过滤器交互、分页、空状态
    - _Requirements: 9.5, 10.4, 11.4_

- [x] 15. Hub 前端 Terminal Node 配置面板
  - [x] 15.1 实现 Terminal Node 配置面板（Hub React 组件）
    - Result Executor 用户搜索 + 添加（Hub user directory）
    - Notifier 用户搜索 + 添加
    - Per-executor: timeout_hours 输入（1-720, default 48）、max_reminders 输入（1-10, default 3）
    - Per-notifier: timeout_hours 输入（1-720, default 72）、max_reminders 输入（1-10, default 2）
    - 无 executor 时显示 warning（允许保存）
    - _Requirements: 14.1, 14.2, 14.3, 14.4, 14.5_

  - [ ]* 15.2 编写前端组件测试
    - 测试：用户搜索、添加/移除 executor/notifier、范围验证、warning 显示
    - _Requirements: 14.1, 14.2, 14.3_

- [x] 16. Hub 前端 Confirmation UI
  - [x] 16.1 实现实例详情页确认按钮
    - Result_Executor 视角：显示完整 Form_Data + 所有审批决策 + "确认已操作" 按钮 + notes 输入框（≤2000 chars）
    - Notifier 视角：显示审批结果摘要 + "确认已知会" 按钮
    - 确认状态显示：pending/confirmed/auto-closed + timestamp + notes
    - _Requirements: 5.4, 6.4, 7.1, 7.2, 7.6, 8.1, 8.5_

  - [ ]* 16.2 编写前端组件测试
    - 测试：确认按钮交互、notes 输入验证、状态显示切换
    - _Requirements: 7.1, 7.6, 8.5_

- [x] 17. Hub 前端 Form Initiation 页面
  - [x] 17.1 实现工作流发起表单页面
    - 根据 Form_Node schema 动态渲染表单字段（text/number/date/select/file/textarea/boolean）
    - 客户端 inline validation（required/type/maxLength/pattern/options）
    - 提交后调用 POST /api/v1/workflows/{id}/initiate
    - 成功后 2 秒内跳转到实例详情页
    - _Requirements: 1.1, 1.2, 1.3, 1.5_

  - [ ]* 17.2 编写前端组件测试
    - 测试：动态表单渲染、inline validation、提交成功跳转、提交失败错误显示
    - _Requirements: 1.1, 1.2, 1.3, 1.5_

- [x] 18. Checkpoint — 确保前后端集成测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 19. 端到端集成测试
  - [x] 19.1 编写 Hub 页面发起全流程测试
    - 表单提交 → 验证 → 实例创建 → 节点执行 → terminal node → 通知发送 → 确认追踪
    - _Requirements: 1.1, 1.2, 1.4, 1.5, 1.6, 4.1, 4.2, 5.1_

  - [x] 19.2 编写 IM 发起全流程测试
    - 自然语言消息 → VE 提取 → 用户确认 → API 调用 → 实例创建 → 通知
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

  - [x] 19.3 编写撤回全流程测试
    - running 实例 → 发起人撤回 → pending nodes 取消 → 参与者通知 → audit trail
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6_

  - [x] 19.4 编写确认提醒和升级全流程测试
    - 实例完成 → 确认记录创建 → 超时 → 提醒发送 → 提醒耗尽 → executor 升级 / notifier 自动关闭
    - _Requirements: 7.3, 7.4, 7.5, 8.2, 8.3, 8.4_

  - [x] 19.5 编写 Directory 视图全流程测试
    - 创建多个不同状态的实例 → 查询各视图 → 验证返回正确子集和排序
    - _Requirements: 9.1, 10.1, 11.1, 12.1_

- [x] 20. Final Checkpoint — 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Hub 侧运行时模块放在 `hub/internal/workflow/` 目录，扩展现有 WorkflowExecutor
- 桌面端 Wails binding 放在 `gui/app_workflow_directory.go`
- 前端组件放在 `gui/frontend/src/components/workflow/` 目录
- IM 快速发起复用现有 `VEMessageHandler` + `IMMessageHandler` pipeline
- 通知通过现有 Hub in-app notification + IM push 双通道发送
- 确认追踪是独立于工作流图执行的 post-completion 子系统
- Property tests 使用 `pgregory.net/rapid`，每个 property 至少 100 次迭代
- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases


## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3", "1.4"] },
    { "id": 1, "tasks": ["2.1", "2.2", "2.3", "11.1", "11.2", "11.3"] },
    { "id": 2, "tasks": ["3.1", "3.2", "3.3", "3.4", "3.5", "5.1"] },
    { "id": 3, "tasks": ["5.2", "5.3", "5.4", "9.1"] },
    { "id": 4, "tasks": ["6.1", "6.2", "6.3", "6.4", "6.5", "6.6", "6.7", "6.8", "9.2", "9.3", "9.4", "9.5"] },
    { "id": 5, "tasks": ["7.1", "7.2", "7.3", "7.4", "7.5", "7.6", "7.7", "7.8", "7.9", "13.1", "13.2", "13.3", "13.4"] },
    { "id": 6, "tasks": ["10.1", "10.2", "10.3", "10.4", "14.1", "14.2", "14.3", "14.4"] },
    { "id": 7, "tasks": ["15.1", "15.2", "16.1", "16.2", "17.1", "17.2"] },
    { "id": 8, "tasks": ["19.1", "19.2", "19.3", "19.4", "19.5"] }
  ]
}
```
