# Implementation Plan: 虚拟员工系统（Virtual Employee System）

## Overview

本实现计划将虚拟员工系统的设计方案转化为可执行的编码任务。核心策略：先构建 Hub 侧基础设施（配额存储、注册表、在线状态、授权处理），再实现 API 层和 Admin Panel，然后构建 Maclaw Client 侧的设置/列表/Tab 系统，最后集成 A2A 通讯和群聊功能。每个任务聚焦单一模块，按依赖顺序排列。

## Tasks

- [x] 1. Hub VE Quota Store — AES-256-GCM 加密配额存储
  - [x] 1.1 创建 `hub/internal/ve/quota_store.go`，定义 `QuotaStore` 结构体（privateKey, filePath, sync.RWMutex），定义 `encryptedQuota`（Ciphertext, Nonce, Version）和 `quotaPayload`（Quota, HubID, Timestamp, MAC）数据结构
  - [x] 1.2 实现 `NewQuotaStore(privateKey []byte, filePath string)` 构造函数
  - [x] 1.3 实现 `SaveQuota(quota int, hubID string) error`：构造 payload → HMAC-SHA256 MAC → JSON 序列化 → AES-256-GCM 加密（key=SHA-256(privateKey), nonce=crypto/rand 12 bytes）→ 写盘
  - [x] 1.4 实现 `LoadQuota(hubID string) (int, error)`：读盘 → AES-256-GCM 解密 → 验证 MAC → 验证 HubID 匹配 → 验证 timestamp ≤ 24h → 返回 quota 值
  - [x] 1.5 实现 `GetEffectiveQuota(hubID string) int`：LoadQuota 失败时返回 0 + log security warning
  - [x] 1.6 实现错误降级逻辑：文件缺失/损坏/解密失败/MAC 不匹配/HubID 不匹配/timestamp 过期 → quota=0
  - [x] 1.7 编写单元测试 `hub/internal/ve/quota_store_test.go`：加密/解密 round-trip、MAC 验证失败、HubID 不匹配、timestamp 过期、文件损坏、明文不泄漏
    - _Requirements: 1.3, 1.4, 1.5, 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

- [x] 2. HubCenter Enrollment 扩展 — 配额字段传递
  - [x] 2.1 在 `corelib/remote/enrollment.go` 的 `EnrollResult` 结构体中新增 `VEQuota int` 字段（`json:"ve_quota,omitempty"`）
  - [x] 2.2 在 Hub 侧 enrollment 响应解析逻辑中，提取 `ve_quota` 字段并验证范围（0-10000），无效/缺失时默认为 0
  - [x] 2.3 Enrollment 成功后调用 `QuotaStore.SaveQuota()` 加密持久化配额
  - [x] 2.4 实现配额推送接收逻辑：监听 Hub-HubCenter WebSocket 通道的 quota update 消息，收到更新后 5s 内重新加密持久化
  - [x] 2.5 实现推送失败重试：60s 间隔，最多 5 次
  - [x] 2.6 编写单元测试：enrollment 响应解析、配额范围验证、推送接收与重试
    - _Requirements: 1.1, 1.2, 1.6, 1.7, 1.8, 1.9, 1.10_

- [x] 3. Hub VE Registry — 注册、审批、状态管理
  - [x] 3.1 创建 `hub/internal/ve/registry.go`，定义 `Registry` 结构体（quotaStore, mu sync.RWMutex, employees map），定义 `VirtualEmployee`、`AccessPolicy`、`VEStatus` 数据模型
  - [x] 3.2 实现 `Register(req VERegistrationRequest) (*VirtualEmployee, error)`：检查配额（active 数量 ≥ quota → quota_exceeded）→ 创建 pending 记录 → 2s 内返回
  - [x] 3.3 实现 `Approve(veID string) error`：状态变更 pending→active → 触发 WebSocket 通知
  - [x] 3.4 实现 `Reject(veID, reason string) error`：状态变更 pending→rejected → 触发 WebSocket 通知
  - [x] 3.5 实现 `Disable(veID string) error`：状态变更 active→disabled → 从 discoverable 列表移除 → 触发通知
  - [x] 3.6 实现 `ListDiscoverable(requesterID string) []VirtualEmployee`：按 AccessPolicy 过滤（public 全可见、whitelist 仅白名单、blacklist 排除黑名单、per_request 全可见带标记）
  - [x] 3.7 实现数据持久化（JSON 文件或 SQLite，与 Hub 现有存储方式一致）
  - [x] 3.8 编写单元测试：注册/审批/拒绝/禁用状态机、配额检查、AccessPolicy 过滤逻辑
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 3.1, 3.2, 3.3, 3.4, 4.4, 4.5, 4.6, 4.7_

- [x] 4. Hub VE Presence Manager — 在线状态心跳管理
  - [x] 4.1 创建 `hub/internal/ve/presence.go`，定义 `PresenceManager` 结构体（mu sync.RWMutex, veStatus map, heartbeatInterval=15s, missThreshold=2）
  - [x] 4.2 实现 `RecordHeartbeat(veID, machineID string)` 和 `StartMonitor(ctx context.Context)` goroutine（每 15s 扫描，连续 2 次 miss → 移除 machineID）
  - [x] 4.3 实现多实例支持：同一 VE 多个 machineID，任一在线→VE online，全部离线→VE offline
  - [x] 4.4 实现 `OnWebSocketConnect/Disconnect`：状态变更后 5s 内 push `ve:status_change` 事件到所有 Client
  - [x] 4.5 编写单元测试：heartbeat 超时检测、多实例在线/离线逻辑、状态变更事件触发
    - _Requirements: 9.1, 9.2, 9.5, 9.6_

- [x] 5. Hub VE Auth Handler — Per-Request 授权流程
  - [x] 5.1 创建 `hub/internal/ve/auth_handler.go`，定义 `AuthHandler` 结构体（pendingRequests sync.Map, timeout=60s），定义 `AuthorizationRequest` 和 `AuthorizationResponse` 结构体
  - [x] 5.2 实现 `HandleSessionInitiation(requesterID, veID string) error`：检查 AccessPolicy → per_request 时生成 AuthorizationRequest → WebSocket push 到 VE 所有者
  - [x] 5.3 实现 `HandleAuthResponse(resp AuthorizationResponse) error`：allow → 建立 A2A session；deny → 通知发起方 access_denied
  - [x] 5.4 实现 60s 超时逻辑：goroutine 定时检查 → 超时后通知发起方 timeout → 清理 pending 记录
  - [x] 5.5 确保授权原子性：allow/deny 决策与 session 建立/拒绝是原子操作
  - [x] 5.6 编写单元测试：per_request 流程、60s 超时、allow/deny 路由、并发授权请求
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7_

- [x] 6. Hub VE API Endpoints — HTTP 路由注册
  - [x] 6.1 注册 Admin API 路由：`GET /api/ve/list`、`POST /api/ve/{id}/approve`、`POST /api/ve/{id}/reject`、`POST /api/ve/{id}/disable`、`PUT /api/ve/config`
  - [x] 6.2 注册 Client API 路由：`POST /api/ve/register`、`GET /api/ve/status`、`PUT /api/ve/settings`、`GET /api/ve/discoverable`、`POST /api/ve/{id}/initiate`、`POST /api/ve/auth/respond`
  - [x] 6.3 实现 handler 层：输入验证（name ≤50 chars, skill_desc ≤500 chars, 消息 ≤32000 code points）→ 委托给 Registry/AuthHandler
  - [x] 6.4 `GET /api/ve/discoverable` 实现 AccessPolicy 过滤（从请求中提取 requester machine_id）
  - [x] 6.5 `POST /api/ve/{id}/initiate` 实现：public/whitelist/blacklist → 直接创建 session；per_request → 委托 AuthHandler
  - [x] 6.6 编写 API 集成测试：完整注册→审批→查询流程、AccessPolicy 过滤、配额超限拒绝
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 3.1, 3.2, 3.3, 4.2, 5.1_

- [x] 7. Hub Admin Panel VE Tab — 后台管理界面
  - [x] 7.1 在 Hub Admin Panel 中新增"虚拟员工"Tab（复用现有 admin panel 组件模式）
  - [x] 7.2 实现待审批列表视图（pending）和已激活列表视图（active），显示 name/skill_description/access_policy/online_status/registered_at
  - [x] 7.3 实现审批/拒绝/禁用操作按钮，调用对应 Admin API
  - [x] 7.4 实现群聊参与者上限配置（数字输入框 1-10，默认 5），变更后 push `ve:group_config` 事件
  - [x] 7.5 编写前端组件测试：列表渲染、操作按钮回调、配置表单验证
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 8.8, 8.9_

- [x] 8. A2A Protocol Extension — 流式消息与 VE Session 处理
  - [x] 8.1 在 `corelib/a2a/types.go` 中新增 `MessageKindStreamChunk` 和 `MessageKindStreamEnd` 常量
  - [x] 8.2 实现 VE 侧消息接收处理：收到 `GroupEnvelope`（Type=discussion_message）→ 提取 content → 调用本地 AI Agent（复用 IMMessageHandler 的 agent loop）
  - [x] 8.3 实现 VE 侧流式响应发送：AI Agent 每生成一个 chunk → 构造 `GroupDiscussionMessage`（kind=stream_chunk）→ 通过 Hub 回传；生成完毕 → 发送 kind=stream_end
  - [x] 8.4 实现 60s 首响应超时：VE 处理消息后 60s 内无 chunk 产出 → 发送 timeout error 消息
  - [x] 8.5 实现消息有序性保证：Hub 中继保证同一 session 内 FIFO 顺序
  - [x] 8.6 编写单元测试：stream_chunk/stream_end 消息构造与解析、session 生命周期、超时处理
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_

- [x] 9. Maclaw Client VE Settings — 设置面板与注册流程
  - [x] 9.1 创建 `gui/app_ve.go`，实现 Wails binding：`RegisterVirtualEmployee`、`UpdateVESettings`、`GetVEStatus`，通过 HubClient 调用 Hub API
  - [x] 9.2 创建 `gui/frontend/src/components/settings/VirtualEmployeeSettingsPanel.tsx`，条件渲染（仅当 remote_machine_id 非空时显示）
  - [x] 9.3 实现表单字段：name（默认当前角色名）、skill_description（textarea）、access_policy 选择器（4 选项）
  - [x] 9.4 实现 whitelist/blacklist 编辑器：policy 为 whitelist/blacklist 时显示，支持添加/移除 maclaw 用户标识
  - [x] 9.5 实现注册状态显示（pending/active/disabled/rejected）+ 审批通过通知（监听 `ve:approved` 事件）
  - [x] 9.6 编写前端组件测试：表单验证、policy 切换联动、状态显示
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

- [x] 10. Maclaw Client VE Tab — 虚拟员工列表组件
  - [x] 10.1 实现 Wails binding `ListVirtualEmployees()`：调用 Hub `GET /api/ve/discoverable`
  - [x] 10.2 创建 `gui/frontend/src/components/ai/VirtualEmployeeTab.tsx`，在"最近任务"区域新增"虚拟员工"Tab
  - [x] 10.3 实现列表渲染：name（截断 20 字符）、skill_description（截断 50 字符）、在线状态指示器（绿点/灰点）、access_policy 图标、per_request 显示"需授权"badge
  - [x] 10.4 实现加载/空状态：100ms 内显示 loading，5s 内渲染结果；Hub 不可用/无 VE 时显示对应空状态
  - [x] 10.5 实现实时更新：监听 `ve:list_update`/`ve:status_change` WebSocket 事件 → 500ms throttle 刷新
  - [x] 10.6 实现交互：双击/右键菜单"对话" → `onStartConversation`；右键菜单"添加到群聊" → `onAddToGroup`
  - [x] 10.7 编写前端组件测试：列表渲染、AccessPolicy 图标、在线状态、空状态、throttle 刷新
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10, 9.3, 9.4_

- [x] 11. AI Assistant Panel Tab System — 重构为 Tab 式多对话
  - [x] 11.1 定义 `AITab` interface（id, type, title, veId?, participants?, closable）和 `AIAssistantPanelState`（tabs, activeTabId, maxVETabs=8）
  - [x] 11.2 重构 `AIAssistantPanel.tsx`：从单一对话视图变为 Tab 容器 + 路由，将现有对话逻辑提取到 `LocalAIAssistantView.tsx`
  - [x] 11.3 创建 `AITabBar.tsx` + `AITabItem.tsx` 组件：水平 Tab 栏，第一个 Tab 固定"AI 助手"（不可关闭）
  - [x] 11.4 实现 Tab 创建：`onStartConversation(ve)` → 检查重复 → 有则激活，无则创建新 Tab
  - [x] 11.5 实现 Tab 关闭（× 按钮 → 结束 A2A session → 移除 Tab）和 Tab 切换（保存/恢复 state：history, scroll, input）
  - [x] 11.6 实现最大 Tab 限制：VE Tab 最多 8 个，超限显示错误提示
  - [x] 11.7 非活跃 Tab 条件渲染（不渲染 DOM，只保留 state）
  - [x] 11.8 编写前端组件测试：Tab 创建/切换/关闭、最大数量限制、状态隔离、重复 Tab 检测
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8, 6.9, 6.10, 6.11_

- [x] 12. VE Conversation View — 虚拟员工对话视图
  - [x] 12.1 创建 `gui/frontend/src/components/ai/VEConversationView.tsx`，复用现有 MessageBubble、StreamingIndicator 组件
  - [x] 12.2 实现 Wails binding `InitiateVEConversation(veID)`、`SendVEMessage(sessionID, content)`、`CloseVESession(sessionID)`
  - [x] 12.3 实现消息发送（A2A 协议路由）和流式响应显示（监听 stream_chunk → 200ms 内渲染 → stream_end 完成）
  - [x] 12.4 实现 A2A session 创建（5s 超时）和错误状态显示（Hub 连接中断/VE 离线/发送失败标记）
  - [x] 12.5 实现断线重连：指数退避（2s→30s，最多 5 次）→ 重连后恢复 session → 按序投递排队消息
  - [x] 12.6 编写前端组件测试：消息发送/接收、流式渲染、错误状态、断线重连
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9_

- [x] 13. Per-Request Authorization UI — 授权请求界面
  - [x] 13.1 创建 `gui/frontend/src/components/ai/VEAuthorizationDialog.tsx`
  - [x] 13.2 实现闪烁指示器：VE Tab 右上角，收到 `ve:auth_request` 事件时开始闪烁，所有请求处理完后停止
  - [x] 13.3 实现授权弹窗：显示 requester name/machine_id/target VE name，提供"允许"/"拒绝"按钮
  - [x] 13.4 实现 Wails binding `RespondAuthRequest(requestID, decision string) error`
  - [x] 13.5 编写前端组件测试：弹窗显示/隐藏、allow/deny 回调、闪烁指示器
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7_

- [x] 14. Group Chat Integration — 群聊功能
  - [x] 14.1 在 VE 对话 Tab 中添加"+"按钮，实现参与者选择器（按 AccessPolicy 过滤，排除已在群中的）
  - [x] 14.2 实现 Wails binding `AddVEToGroup(sessionID, veID string) error`：发送 GroupInvitation → 等待接受
  - [x] 14.3 实现群聊 Tab 标题更新（参与者名称，超长截断）和消息广播（GroupDiscussionMessage, scope="current_hub"）
  - [x] 14.4 实现参与者响应标签（每条响应前显示参与者名称）和参与者离线通知
  - [x] 14.5 实现参与者上限检查：超过 Hub 配置的 max_group_participants → 显示"群聊人数已满"
  - [x] 14.6 监听 `ve:group_config` 事件更新本地参与者上限配置
  - [x] 14.7 编写前端组件测试：参与者选择器、上限检查、消息广播渲染、离线通知
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8, 8.9, 8.10_

- [x] 15. WebSocket Event Integration — VE 相关实时事件
  - [x] 15.1 定义 WebSocket 事件常量：`ve:list_update`、`ve:status_change`、`ve:auth_request`、`ve:approved`、`ve:rejected`、`ve:disabled`、`ve:group_config`
  - [x] 15.2 Hub 侧实现事件 push 逻辑：Registry/PresenceManager/AuthHandler 状态变更时 broadcast 到所有连接的 Client
  - [x] 15.3 Client 侧实现事件监听注册：WebSocket 连接建立后订阅 VE 相关事件，重连后重新订阅
  - [x] 15.4 编写集成测试：事件 push/receive 全链路、throttle 行为、重连后重新订阅
    - _Requirements: 4.8, 9.1, 9.2, 9.5_

- [x] 16. End-to-End Integration Testing — 全流程集成测试
  - [x] 16.1 编写完整注册流程测试：HubCenter enrollment（含 ve_quota）→ Hub 配额加密存储 → Client 注册 → Admin 审批 → Client 通知
  - [x] 16.2 编写对话流程测试：Client A 发起对话 → Hub 路由 → VE Client B 接收 → AI 处理 → 流式响应回传 → Client A 显示
  - [x] 16.3 编写群聊流程测试：创建对话 → 添加 VE → GroupInvitation → 多方消息广播 → 参与者响应
  - [x] 16.4 编写授权流程测试：per_request VE → 授权请求 push → 所有者 allow/deny → session 建立/拒绝 → 60s 超时
  - [x] 16.5 编写在线状态测试：WebSocket 连接 → online → 断开 → 30s 后 offline → 重连 → online
  - [x] 16.6 编写安全性测试：配额明文不泄漏、MAC 篡改检测、跨 Hub 复制检测、AccessPolicy 一致性
    - _Requirements: 1.1-1.10, 2.1-2.6, 3.1-3.7, 4.1-4.10, 5.1-5.7, 6.1-6.11, 7.1-7.9, 8.1-8.10, 9.1-9.6, 10.1-10.6, 11.1-11.11_

- [x] 17. Conversation Attachment Support — 对话附件支持（文本/图片/文件）
  - [x] 17.1 Hub 文件中继端点实现
    - 创建 `hub/internal/ve/file_relay.go`，实现 `POST /api/ve/files/upload` 和 `GET /api/ve/files/{id}` 端点
    - 上传端点：接收 multipart/form-data，验证文件大小（文本≤500KB、图片≤10MB、文档≤20MB）和 MIME 类型
    - 存储到 Hub 本地临时目录，生成唯一 file_id，设置 24 小时 TTL
    - 下载端点：验证 session_id + participant_id 授权，返回文件内容
    - 实现 TTL 清理 goroutine：每小时扫描，删除超过 24 小时的文件
    - _Requirements: 11.4, 11.5, 11.9_
  - [x] 17.2 A2A Message 附件字段扩展
    - 在 `corelib/a2a/types.go` 中新增 `TextAttachment`（Content base64, Filename, MimeType）、`ImageAttachment`（FileURL, Filename, MimeType, Width, Height）、`FileAttachment`（FileURL, Filename, MimeType, SizeBytes）结构体
    - 在 `GroupDiscussionMessage` 中新增 `TextAttachments []TextAttachment`、`ImageAttachments []ImageAttachment`、`FileAttachments []FileAttachment` 字段（`json:",omitempty"`）
    - 编写序列化/反序列化单元测试
    - _Requirements: 11.3, 11.4, 11.5_
  - [x] 17.3 Client 侧附件发送逻辑（Wails binding）
    - 创建 `gui/app_ve_attachment.go`，实现 `SendVEMessageWithAttachments(sessionID string, content string, filePaths []string) error`
    - 实现文件类型检测（按扩展名分类：text/image/document）和大小验证
    - 文本文件（≤500KB）：读取内容 → base64 编码 → 填充 `TextAttachment` 内联到 A2A Message
    - 图片文件（≤10MB）：上传到 Hub file relay → 获取 file_url → 填充 `ImageAttachment`
    - 文档文件（≤20MB）：上传到 Hub file relay → 获取 file_url → 填充 `FileAttachment`
    - 上传失败时返回具体错误（网络错误/大小超限/类型不支持），保留消息文本供重试
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.10_
  - [x] 17.4 VE 侧附件接收与 AI Agent 传递
    - 在 VE 侧消息接收处理（Task 8.2 的 `GroupEnvelope` handler）中，检测 A2A Message 的附件字段
    - `TextAttachment`：base64 解码后将文本内容拼接到 AI Agent 输入（作为 context）
    - `ImageAttachment`/`FileAttachment`：通过 file_url 下载文件内容，传递给 AI Agent（图片作为 vision input，文档提取文本后作为 context）
    - 下载失败时记录错误日志，仍处理消息文本部分
    - _Requirements: 11.6_
  - [x] 17.5 前端附件 UI 组件
    - 在 `VEConversationView.tsx` 的输入区域添加附件按钮（📎），点击触发文件选择器
    - 文件选择器过滤支持的类型：`.txt,.md,.csv,.json,.xml,.yaml,.log,.go,.py,.js,.ts,.html,.css,.png,.jpg,.jpeg,.gif,.webp,.bmp,.pdf,.docx`
    - 选择文件后在输入框上方显示附件预览条（文件名 + 大小 + 移除按钮）
    - 发送时调用 `SendVEMessageWithAttachments` Wails binding
    - 错误处理：上传失败显示 toast 错误提示，保留输入框文本
    - _Requirements: 11.1, 11.2, 11.10_
  - [x] 17.6 前端附件显示组件
    - 在 `VEConversationView.tsx` 的消息气泡中实现附件渲染
    - 图片附件：内联缩略图预览（max-width: 300px），点击展开全尺寸查看
    - 文本/文档附件：显示为文件 chip（📄 图标 + 文件名 + 大小），点击下载
    - VE 响应中的附件（AI 生成的文件）：同样渲染为 chip 或内联图片
    - _Requirements: 11.7, 11.8_
  - [x] 17.7 群聊附件广播
    - 在群聊消息广播逻辑（Task 14.3）中，确保 `GroupDiscussionMessage` 的附件字段被完整转发到所有参与者
    - Hub file relay 端点的授权验证扩展：群聊中所有参与者（通过 session_id 关联）均可访问附件文件
    - _Requirements: 11.11_
  - [x] 17.8 编写附件功能测试
    - Hub file relay 单元测试：上传/下载/TTL 清理/授权验证/大小超限拒绝/类型不支持拒绝
    - A2A 附件序列化测试：TextAttachment/ImageAttachment/FileAttachment 的 JSON round-trip
    - Client 侧集成测试：文件类型检测、大小验证、上传成功/失败路径
    - 前端组件测试：附件按钮交互、预览条渲染、消息气泡中附件显示、错误提示
    - _Requirements: 11.1-11.11_

## Notes

- 所有通讯通过 Hub 中转，Client 之间不直接通讯（解决网络不通问题）
- A2A 协议复用现有 `corelib/a2a/` 包的 Session/Message/GroupDiscussion 类型
- Hub 侧 VE 模块放在 `hub/internal/ve/` 目录，与现有 iWorkerCenter 模块并列
- 前端 VE 组件放在 `gui/frontend/src/components/ai/` 目录，与现有 AI 助手面板组件同级
- 群聊参与者上限由 Hub Admin 配置（最大 10，默认 5），通过 WebSocket push 同步到所有 Client
- 配额加密使用 Hub 已有的私钥，不需要额外的密钥管理基础设施
- 附件文件通过 Hub file relay 中转（24 小时 TTL），文本文件内联 base64，图片/文档通过 file_url 引用
- 附件大小限制：文本≤500KB、图片≤10MB、文档≤20MB，超限时客户端侧拒绝并提示用户

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7"] },
    { "id": 1, "tasks": ["2.1", "2.2", "2.3", "2.4", "2.5", "2.6", "4.1", "4.2", "4.3", "4.4", "4.5"] },
    { "id": 2, "tasks": ["3.1", "3.2", "3.3", "3.4", "3.5", "3.6", "3.7", "3.8", "5.1", "5.2", "5.3", "5.4", "5.5", "5.6"] },
    { "id": 3, "tasks": ["6.1", "6.2", "6.3", "6.4", "6.5", "6.6", "7.1", "7.2", "7.3", "7.4", "7.5", "8.1", "8.2", "8.3", "8.4", "8.5", "8.6"] },
    { "id": 4, "tasks": ["9.1", "9.2", "9.3", "9.4", "9.5", "9.6", "10.1", "10.2", "10.3", "10.4", "10.5", "10.6", "10.7", "15.1", "15.2", "15.3", "15.4", "17.1", "17.2"] },
    { "id": 5, "tasks": ["11.1", "11.2", "11.3", "11.4", "11.5", "11.6", "11.7", "11.8", "17.3"] },
    { "id": 6, "tasks": ["12.1", "12.2", "12.3", "12.4", "12.5", "12.6", "13.1", "13.2", "13.3", "13.4", "13.5", "17.4", "17.5", "17.6"] },
    { "id": 7, "tasks": ["14.1", "14.2", "14.3", "14.4", "14.5", "14.6", "14.7", "17.7"] },
    { "id": 8, "tasks": ["16.1", "16.2", "16.3", "16.4", "16.5", "16.6", "17.8"] }
  ]
}
```
