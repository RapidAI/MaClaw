# Implementation Plan: Dynamic Notification System

## Overview

本计划实现 Hub/HubCenter 动态通知系统，覆盖后端通知包（类型、存储、服务、推送）、HTTP API、WebSocket 消息扩展、HubCenter 级联推送、GUI Go 绑定、React 前端组件、以及 Hub/HubCenter admin 管理面板。基础层先行，依赖层后接，最终通过集成测试验证端到端链路。

## Tasks

- [x] 1. Hub 后端：通知核心包（`hub/internal/notification/`）
  - [x] 1.1 创建 `hub/internal/notification/types.go`：定义 Category/Priority/AudienceType/Status 常量、Notification 结构体、CreateRequest/CascadeRequest/ClientNotification/NotificationPushPayload 类型
    - 按设计文档定义所有类型和 JSON 标签
    - _Requirements: FR-1, FR-2, NFR-1_
  - [x] 1.2 创建 `hub/internal/notification/store.go`：SQLite CRUD 操作
    - 实现 `InitSchema()` 创建 `admin_notifications` 和 `admin_notification_reads` 表（含索引）
    - 实现 `Create()`、`GetByID()`、`List()`（支持状态/分类筛选+分页）、`UpdateStatus()`
    - 实现 `MarkRead()`、`MarkAllRead()`、`GetUnreadForMachine()`（最多 10 条，排除已过期/已撤回）
    - 实现 `GetReadStats()`（总推送数、已读数、已读率）
    - 实现受众查询辅助：`AllActiveMachineIDs()`、`MachineIDsByTenantIDs()`、`MachineIDsByDepartmentIDs()`、`MachineIDsByUserIDs()`
    - _Requirements: FR-1, FR-5, FR-6, NFR-3_
  - [x] 1.3 创建 `hub/internal/notification/service.go`：业务逻辑层
    - 实现 `NewService(store, wsBroadcaster, imPusher)`
    - 实现 `CreateNotification()`：验证输入（标题≤100字符、内容≤2000字符、分类/受众合法性）、生成 UUID、持久化
    - 实现 `PublishNotification()`：设状态为 published、解析受众、通过 WSBroadcaster 推送 Envelope
    - 实现 `RevokeNotification()`：设状态为 revoked、广播 revoke 消息
    - 实现 `GetUnreadForMachine()`、`MarkRead()`、`MarkAllRead()`
    - 实现 `CreateFromCascade()`：HubCenter 级联入口，幂等处理（source+source_id 唯一）
    - 实现 `CheckExpired()`：定期检查并标记过期通知
    - _Requirements: FR-1, FR-3, FR-5, FR-6, NFR-2_
  - [x] 1.4 创建 `hub/internal/notification/pusher.go`：WebSocket 推送适配
    - 定义 `WSBroadcaster` 接口（BroadcastToMachines/BroadcastToAll）
    - 实现 `NewPusher(wsHub)` 适配现有 `ws.Hub` 的广播能力
    - 构建 Envelope（type=`notification.push`）并序列化为 JSON
    - _Requirements: FR-3, NFR-2, NFR-5_

- [x] 2. Checkpoint — 确保 Hub 通知核心包编译通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 3. Hub HTTP API handlers + 路由注册
  - [x] 3.1 创建 `hub/internal/httpapi/notification_handler.go`：管理员端点
    - `POST /api/v1/admin/notifications`：创建通知（requireAdmin 中间件）
    - `GET /api/v1/admin/notifications`：管理员列表含统计（requireAdmin）
    - `GET /api/v1/admin/notifications/{id}`：详情+送达统计（requireAdmin）
    - `POST /api/v1/admin/notifications/{id}/revoke`：撤回通知（requireAdmin）
    - 输入验证：标题/内容长度、分类/受众/优先级枚举合法性
    - _Requirements: FR-1, FR-5, NFR-4_
  - [x] 3.2 创建客户端端点（同文件或独立文件）
    - `GET /api/v1/notifications/unread`：客户端拉取未读通知（machine auth，limit=10）
    - `POST /api/v1/notifications/{id}/read`：客户端标记已读（machine auth）
    - `POST /api/v1/notifications/read-all`：客户端全部已读（machine auth）
    - _Requirements: FR-3, FR-4, NFR-4_
  - [x] 3.3 创建级联端点
    - `POST /api/v1/notifications/cascade`：HubCenter 级联推送入口（requireGlobalAdmin）
    - 幂等处理：相同 source+source_id 时更新而非重复创建
    - _Requirements: FR-2, FR-3, NFR-4_
  - [x] 3.4 在 Hub 路由注册处（如 `hub/internal/httpapi/router.go` 或类似文件）注册所有通知端点
    - 确保复用现有 `requireAdmin`/`requireGlobalAdmin`/machine auth 中间件
    - _Requirements: NFR-4, NFR-5_

- [x] 4. Hub WebSocket 消息类型扩展
  - [x] 4.1 在 `hub/internal/ws/envelope.go` 或相关类型文件中新增 `notification.push` 和 `notification.ack` 消息类型常量
    - _Requirements: NFR-5_
  - [x] 4.2 在 Hub WebSocket 消息分发处新增 `notification.ack` 的处理分支
    - 解析 `{notification_id, action: "read"}` payload
    - 调用 `NotificationService.MarkRead()`
    - _Requirements: FR-4, NFR-5_

- [x] 5. Checkpoint — 确保 Hub 端所有编译通过且可启动
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. HubCenter 后端：通知包（`hubcenter/internal/notification/`）
  - [x] 6.1 创建 `hubcenter/internal/notification/store.go`：存储 HubCenter 级通知记录
    - 表结构参考 Hub 端但受众字段为 Hub 级/租户级/全网
    - 新增 `cascade_results` 表记录每个 Hub 的推送状态（hub_id, status, pushed_at）
    - _Requirements: FR-2, NFR-3_
  - [x] 6.2 创建 `hubcenter/internal/notification/service.go`：业务逻辑
    - 实现 `CreateNotification()`：验证+持久化+触发级联
    - 实现受众解析（整个 Hub / 指定 Hub 下租户 / 全网广播）
    - 实现列表/详情/撤回（撤回时级联发送 revoke 到各 Hub）
    - _Requirements: FR-2, FR-5_
  - [x] 6.3 创建 `hubcenter/internal/notification/cascade.go`：HTTP POST 到目标 Hub
    - 实现 `DispatchToHubs()`：并发推送到目标 Hub 的 `/api/v1/notifications/cascade`
    - 使用 global admin token 认证
    - 记录每个 Hub 的推送结果（success/failed/timeout）
    - 30 秒超时、错误处理（401/403 不重试，5xx 记录支持手动重试）
    - _Requirements: FR-3, NFR-2, NFR-3_

- [x] 7. HubCenter HTTP API handlers
  - [x] 7.1 创建 HubCenter 通知管理端点
    - `POST /api/v1/admin/notifications`：创建跨 Hub 通知
    - `GET /api/v1/admin/notifications`：列表含级联状态
    - `GET /api/v1/admin/notifications/{id}`：详情+各 Hub 送达情况
    - `POST /api/v1/admin/notifications/{id}/revoke`：撤回（级联）
    - 复用 HubCenter 现有 admin 认证中间件
    - _Requirements: FR-2, FR-5, NFR-4_
  - [x] 7.2 注册路由到 HubCenter HTTP 路由器
    - _Requirements: NFR-4_

- [x] 8. Checkpoint — 确保 HubCenter 端编译通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. GUI Go 后端：通知绑定 + WebSocket 处理
  - [x] 9.1 创建 `gui/notification_binding.go`：Wails bindings
    - `GetUnreadNotifications() []ClientNotification`：返回内存缓存的未读列表
    - `GetUnreadCount() int`：返回未读计数
    - `MarkNotificationRead(notificationID string) error`：标记已读（调用 Hub API + 更新本地缓存）
    - `MarkAllNotificationsRead() error`：全部已读（调用 Hub API + 更新本地缓存）
    - `PullUnreadNotifications() error`：从 Hub 拉取未读（重连后调用）
    - 内存缓存 `notificationCache`：最多 10 条未读，LRU 淘汰
    - _Requirements: FR-3, FR-4, NFR-5_
  - [x] 9.2 创建 `gui/remote_hub_notification.go`：WebSocket 消息处理
    - `handleNotificationPush(payload json.RawMessage)`：处理 `notification.push` envelope
    - action=new：添加到缓存 + `EventsEmit("notification:new")` + urgent 时额外 `EventsEmit("notification:urgent-toast")`
    - action=revoke：从缓存移除 + `EventsEmit("notification:revoke")`
    - _Requirements: FR-3, FR-4_
  - [x] 9.3 在 `gui/remote_hub_message_type.go` 中新增 `hubInboundMessageNotificationPush` 常量并在 switch 分发中添加 case
    - _Requirements: NFR-5_
  - [x] 9.4 在 WebSocket 连接认证成功（auth.ok）后自动调用 `PullUnreadNotifications()` 同步未读
    - _Requirements: FR-3, NFR-3_

- [x] 10. Checkpoint — 确保 GUI Go 后端编译通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. GUI React 前端：通知组件
  - [x] 11.1 创建 `gui/frontend/src/components/ai/useNotifications.ts`：状态管理 Hook
    - 监听 `notification:new`、`notification:revoke`、`notification:sync`、`notification:urgent-toast` 事件
    - 管理 notifications 列表、unreadCount、panelOpen、categoryFilter 状态
    - 初始化时调用 `GetUnreadNotifications()` 加载
    - displayCount = min(unreadCount, 10)，shouldAnimate = unreadCount > 0
    - _Requirements: FR-4_
  - [x] 11.2 创建 `gui/frontend/src/components/ai/NotificationBell.tsx`：铃铛图标组件
    - 铃铛图标 + CSS 闪烁动画（unreadCount > 0 时）
    - 未读计数 badge（红点，最大显示 10+）
    - 点击触发 panelOpen toggle
    - 位置：AI 助手面板顶部标题栏区域（`AssistantTitleBar.tsx`）
    - _Requirements: FR-4_
  - [x] 11.3 创建 `gui/frontend/src/components/ai/NotificationPanel.tsx`：下拉通知列表面板
    - 下拉面板 UI（fixed/absolute 定位）
    - 顶部：分类筛选（Tabs/Pills：全部 / 系统公告 / 功能更新 / 安全告警 / 运维通知 / 自定义）
    - 底部：「全部已读」按钮
    - 列表渲染 NotificationItem 组件
    - 空状态提示
    - _Requirements: FR-4_
  - [x] 11.4 创建 `gui/frontend/src/components/ai/NotificationItem.tsx`：单条通知卡片
    - 显示：分类标签（彩色 pill）、标题、时间（相对时间格式）、优先级标识（urgent 红色/important 橙色）
    - 已读/未读视觉区分（未读加粗/背景色）
    - 点击展开详情（NotificationDetail）或标记已读
    - _Requirements: FR-4_
  - [x] 11.5 创建 `gui/frontend/src/components/ai/NotificationDetail.tsx`：通知详情
    - Markdown 渲染通知 content（使用现有 Markdown 渲染库）
    - XSS 防护：使用 DOMPurify 或等效库 sanitize HTML 输出
    - 返回按钮回到列表
    - _Requirements: FR-4, NFR-4_
  - [x] 11.6 创建 `gui/frontend/src/components/ai/NotificationToast.tsx`：urgent 通知 Toast 提醒
    - 类似新版本提醒的 UI 模式
    - 自动消失（5-10 秒）或手动关闭
    - 显示标题 + 简短内容预览
    - 点击跳转到通知详情
    - _Requirements: FR-4_
  - [x] 11.7 在 `AssistantTitleBar.tsx`（或对应标题栏组件）中集成 NotificationBell
    - 引入 useNotifications hook
    - 渲染 NotificationBell + NotificationPanel
    - _Requirements: FR-4_

- [x] 12. Checkpoint — 确保 GUI 前端编译通过且组件可渲染
  - Ensure all tests pass, ask the user if questions arise.

- [x] 13. Hub Admin Panel：通知管理 Tab
  - [x] 13.1 创建 `hub/web/admin/notification-tab.js`：通知管理页面
    - Tab 导航集成（在现有 admin 面板的 tab 列表中新增"通知管理"）
    - 通知列表视图：卡片列表（标题、分类标签、状态标签、创建时间、送达率）
    - 筛选栏：状态（全部/已发布/已过期/已撤回/草稿）、分类
    - 操作按钮：创建新通知
    - _Requirements: FR-5, NFR-5_
  - [x] 13.2 实现创建通知表单
    - 标题输入（maxlength=100）
    - Markdown 编辑器（内容，maxlength=2000，带预览）
    - 分类选择下拉（系统公告/功能更新/安全告警/运维通知/自定义）
    - 受众选择器：所有用户（默认）/ 指定租户（多选）/ 指定部门（树形选择）/ 指定用户（搜索）
    - 优先级 radio（normal/important/urgent）
    - IM 推送开关（仅 urgent 时可见）
    - 生效时间：立即 / 定时（日期时间选择器）
    - 过期时间（可选日期时间选择器）
    - 操作：保存草稿 / 立即发布
    - 调用 `POST /api/v1/admin/notifications` API
    - _Requirements: FR-1, NFR-5_
  - [x] 13.3 实现通知详情 + 统计视图
    - 通知内容 Markdown 预览
    - 送达统计：总推送数、已读数、已读率
    - 撤回操作（仅 published 且未过期时可用）
    - _Requirements: FR-5_

- [x] 14. HubCenter Admin Panel：通知管理
  - [x] 14.1 在 `hubcenter/web/admin/` 中新增通知管理模块
    - 复用 Hub admin 的 UI 结构
    - 受众选择器差异：整个 Hub（多选 Hub 实例列表）/ 指定 Hub 下的租户 / 全网广播
    - 额外展示：级联推送状态表格（Hub 名称、推送时间、状态 success/failed/pending）
    - 调用 HubCenter 通知 API
    - _Requirements: FR-2, FR-5_

- [x] 15. Checkpoint — 确保 Admin 面板功能完整
  - Ensure all tests pass, ask the user if questions arise.

- [x] 16. Property-Based Tests（使用 `pgregory.net/rapid`）
  - [x] 16.1 Property 1: Badge count display invariant
    - **Property 1: Badge count display invariant**
    - 生成随机 unread count (0-1000)，验证 displayCount == min(N, 10) && shouldAnimate == (N > 0)
    - **Validates: Requirements FR-4**
  - [x] 16.2 Property 2: Client-server unread synchronization on reconnect
    - **Property 2: Client-server unread synchronization on reconnect**
    - 生成随机通知集合（不同状态/过期/撤回），模拟 GetUnreadForMachine，验证返回集合为已发布+未过期+未撤回+未读的子集且 ≤ 10 条
    - **Validates: Requirements FR-3, FR-4**
  - [x] 16.3 Property 3: Revoked notification invisibility
    - **Property 3: Revoked notification invisibility**
    - 生成随机通知序列（含 revoke 操作），验证 revoked 通知不出现在任何 GetUnreadForMachine/push 结果中
    - **Validates: Requirements FR-5, FR-6**
  - [x] 16.4 Property 4: Notification input validation completeness
    - **Property 4: Notification input validation completeness**
    - 生成随机 CreateRequest（含无效值：标题>100字符、内容>2000字符、非法分类/受众），验证验证逻辑正确拒绝所有无效输入
    - **Validates: Requirements FR-1**
  - [x] 16.5 Property 5: Notification lifecycle state machine validity
    - **Property 5: Notification lifecycle state machine validity**
    - 生成随机状态转换序列，验证只有 draft→published→{expired,revoked} 合法，其他转换被拒绝
    - **Validates: Requirements FR-6**

- [x] 17. Integration Tests
  - [x] 17.1 Hub admin API 端到端测试
    - 创建通知 → 发布 → 拉取未读 → 标记已读 → 撤回 → 验证客户端不可见
    - _Requirements: FR-1, FR-3, FR-4, FR-5, FR-6_
  - [x] 17.2 HubCenter 级联端到端测试
    - HubCenter 创建通知 → 级联 POST 到 Hub → Hub 存储 → 客户端拉取验证
    - _Requirements: FR-2, FR-3_
  - [x] 17.3 WebSocket 推送验证测试
    - 创建通知 → 验证在线客户端收到 notification.push envelope（action=new）
    - 撤回通知 → 验证在线客户端收到 notification.push envelope（action=revoke）
    - _Requirements: FR-3, NFR-2_
  - [x] 17.4 客户端断线重连同步测试
    - 创建通知 → 模拟客户端断线 → 重连 → Pull 验证未读通知存在
    - _Requirements: FR-3, NFR-3_

- [x] 18. Final Checkpoint — 确保全部测试通过
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using Go's `rapid` library (already in project)
- Unit tests validate specific examples and edge cases
- Hub WebSocket 复用现有 Envelope 格式（`{type, request_id, ts, machine_id, payload}`），仅新增 notification 专用 type
- Admin 面板复用现有 UI 风格（tab 导航 + 卡片式布局）
- 客户端不区分通知来源（Hub/HubCenter），统一展示
- IM 通道推送（飞书/微信/QQ）属于 urgent 级别的可选功能，复用现有 IM 推送基础设施

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["1.3", "1.4"] },
    { "id": 2, "tasks": ["3.1", "3.2", "3.3", "3.4", "4.1", "4.2"] },
    { "id": 3, "tasks": ["6.1", "6.2", "6.3"] },
    { "id": 4, "tasks": ["7.1", "7.2", "9.1", "9.2", "9.3", "9.4"] },
    { "id": 5, "tasks": ["11.1", "11.2", "11.3", "11.4", "11.5", "11.6", "11.7"] },
    { "id": 6, "tasks": ["13.1", "13.2", "13.3", "14.1"] },
    { "id": 7, "tasks": ["16.1", "16.2", "16.3", "16.4", "16.5"] },
    { "id": 8, "tasks": ["17.1", "17.2", "17.3", "17.4"] }
  ]
}
```
