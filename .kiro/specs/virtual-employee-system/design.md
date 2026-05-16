# Technical Design

## Overview

虚拟员工系统在现有 Hub/HubCenter 注册体系和 A2A 协议基础上构建，核心设计原则是**最大化复用现有基础设施**。系统通过扩展 HubCenter enrollment 响应传递配额、复用 Hub WebSocket 通道传递实时事件、复用 A2A `GroupProfile`/`GroupDiscussionMessage`/`GroupEnvelope` 类型实现通讯、复用 `AIAssistantPanel` 组件实现多 Tab 对话。

技术栈：Go 后端（Wails 框架）、React/TypeScript 前端、现有 Hub WebSocket 基础设施。

## Components and Interfaces

### System Components

```
┌─────────────────────────────────────────────────────────────────┐
│                         HubCenter                                │
│  - enrollment 响应新增 ve_quota 字段                              │
│  - 配额推送（通过 Hub-HubCenter 通道）                            │
└──────────────────────────┬──────────────────────────────────────┘
                           │ enrollment / quota push
┌──────────────────────────▼──────────────────────────────────────┐
│                           Hub                                    │
│  ┌─────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │ VE Quota Store  │  │ VE Registry      │  │ A2A Router    │  │
│  │ (AES-256-GCM)   │  │ (审批/状态管理)   │  │ (消息路由)     │  │
│  └─────────────────┘  └──────────────────┘  └───────────────┘  │
│  ┌─────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │ Admin Panel     │  │ WebSocket Hub    │  │ Auth Handler  │  │
│  │ (VE Tab)        │  │ (heartbeat/push) │  │ (per_request) │  │
│  └─────────────────┘  └──────────────────┘  └───────────────┘  │
└──────────────────────────┬──────────────────────────────────────┘
                           │ WebSocket (existing)
┌──────────────────────────▼──────────────────────────────────────┐
│                     Maclaw Client                                 │
│  ┌─────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │ VE Settings     │  │ VE Tab (React)   │  │ AI Panel Tabs │  │
│  │ (注册/策略配置)  │  │ (列表/状态展示)   │  │ (多对话管理)   │  │
│  └─────────────────┘  └──────────────────┘  └───────────────┘  │
│  ┌─────────────────┐  ┌──────────────────┐                     │
│  │ A2A HubClient   │  │ Auth Request UI  │                     │
│  │ (existing)       │  │ (授权弹窗)       │                     │
│  └─────────────────┘  └──────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow

**注册流程：**
1. HubCenter → Hub：enrollment 响应含 `ve_quota`
2. Hub：AES-256-GCM 加密存储配额
3. Maclaw Client → Hub：提交 VE 注册请求（WebSocket/HTTP）
4. Hub Admin：审批请求 → 状态变更通知（WebSocket push）
5. Hub → Maclaw Client：审批结果通知

**对话流程：**
1. 发起方 Maclaw → Hub：创建 A2A Session（复用 `HubClient.CreateConsultation`）
2. Hub：验证 Access_Policy → 路由消息
3. Hub → 目标 VE Maclaw：转发 A2A Message
4. 目标 VE Maclaw：本地 AI Agent 处理 → 生成响应
5. 目标 VE Maclaw → Hub → 发起方 Maclaw：流式响应回传

**在线状态流程：**
1. Maclaw Client：WebSocket 连接建立 → Hub 标记 VE online
2. Hub：15s heartbeat 检测 → 2 次 miss 后标记 offline
3. Hub → All Clients：状态变更 WebSocket push


## Detailed Design

### HubCenter 配额扩展

**集成点**：`corelib/remote/enrollment.go` 的 `EnrollResult`

扩展 enrollment 响应解析，新增 `VEQuota` 字段：

```go
// EnrollResult 扩展
type EnrollResult struct {
    // ... existing fields ...
    VEQuota    int    `json:"ve_quota,omitempty"`    // 虚拟员工配额，0-10000
}
```

Hub 侧接收到 enrollment 响应后，通过 `VEQuotaStore` 加密持久化。配额更新通过现有 Hub-HubCenter WebSocket 通道推送，复用 `heartbeat/notification` 机制。

### Hub VE Quota 加密存储模块

**新增文件**：`hub/internal/ve/quota_store.go`

```go
type QuotaStore struct {
    privateKey []byte          // Hub_Private_Key derived material
    filePath   string          // encrypted quota file path
    mu         sync.RWMutex
}

type encryptedQuota struct {
    Ciphertext []byte `json:"ciphertext"` // AES-256-GCM encrypted payload
    Nonce      []byte `json:"nonce"`      // 12-byte GCM nonce
}

type quotaPayload struct {
    Quota     int       `json:"quota"`
    HubID     string    `json:"hub_id"`
    Timestamp time.Time `json:"timestamp"`
    MAC       []byte    `json:"mac"`       // HMAC-SHA256 over quota+hub_id+timestamp
}
```

**加密流程**：
1. 构造 `quotaPayload`（quota + hub_id + UTC timestamp）
2. 计算 HMAC-SHA256 MAC（key = Hub_Private_Key, data = quota||hub_id||timestamp）
3. JSON 序列化 payload
4. AES-256-GCM 加密（key = SHA-256(Hub_Private_Key), nonce = crypto/rand 12 bytes）
5. 持久化 `encryptedQuota` 到磁盘

**解密验证流程**：
1. AES-256-GCM 解密
2. 验证 MAC 完整性
3. 验证 Hub ID 匹配当前 Hub
4. 验证 timestamp 不超过 24 小时
5. 任一验证失败 → quota = 0 + security warning log

### Hub VE Registry 模块

**新增文件**：`hub/internal/ve/registry.go`

复用 iWorkerCenter 的数据模型模式，新增 VE 专用实体：

```go
type VirtualEmployee struct {
    ID              string       `json:"id"`
    OwnerMachineID  string       `json:"owner_machine_id"`
    OwnerAgentID    string       `json:"owner_agent_id"`
    Name            string       `json:"name"`
    SkillDesc       string       `json:"skill_description"`
    AccessPolicy    AccessPolicy `json:"access_policy"`
    Status          VEStatus     `json:"status"`  // pending/active/disabled/rejected
    OnlineStatus    string       `json:"online_status"` // online/offline
    Whitelist       []string     `json:"whitelist,omitempty"`
    Blacklist       []string     `json:"blacklist,omitempty"`
    RegisteredAt    time.Time    `json:"registered_at"`
    ApprovedAt      time.Time    `json:"approved_at,omitempty"`
    LastHeartbeat   time.Time    `json:"last_heartbeat"`
}

type AccessPolicy string
const (
    PolicyPublic     AccessPolicy = "public"
    PolicyWhitelist  AccessPolicy = "whitelist"
    PolicyBlacklist  AccessPolicy = "blacklist"
    PolicyPerRequest AccessPolicy = "per_request"
)

type VEStatus string
const (
    VEStatusPending  VEStatus = "pending"
    VEStatusActive   VEStatus = "active"
    VEStatusDisabled VEStatus = "disabled"
    VEStatusRejected VEStatus = "rejected"
)
```

**Registry 核心方法**：
- `Register(req VERegistrationRequest) error` — 检查配额 → 创建 pending 记录
- `Approve(veID string) error` — 状态变更 + WebSocket 通知
- `Reject(veID, reason string) error` — 状态变更 + WebSocket 通知
- `Disable(veID string) error` — 状态变更 + 从 discoverable 列表移除
- `ListDiscoverable(requesterID string) []VirtualEmployee` — 按 Access_Policy 过滤
- `UpdateOnlineStatus(machineID string, online bool)` — heartbeat 驱动

### Hub Admin Panel VE Tab

**集成点**：Hub 后台管理面板（已有 iWorkerCenter 管理界面）

新增 "虚拟员工" Tab，复用现有 admin panel 的 React 组件模式：
- 待审批列表（pending）
- 已激活列表（active）
- 批量操作（approve/reject/disable）
- 群聊参与者上限配置（1-10，默认 5）

### Maclaw Client VE 注册设置

**集成点**：`gui/frontend/src/components/settings/` 目录

新增 `VirtualEmployeeSettingsPanel.tsx`：
- 条件渲染：仅当 `config.remote_machine_id` 非空（已注册到 Hub）时显示
- 表单字段：name（默认当前角色名）、skill_description、access_policy 选择器
- whitelist/blacklist 编辑器（access_policy 为对应模式时显示）
- 注册状态显示（pending/active/disabled）

**后端 Wails binding**：
```go
// gui/app_ve.go
func (a *App) RegisterVirtualEmployee(name, skillDesc, policy string) error
func (a *App) UpdateVEAccessPolicy(policy string, list []string) error
func (a *App) GetVERegistrationStatus() (*VEStatus, error)
```

通过现有 `HubClient` 发送注册请求到 Hub。

### VE Tab（虚拟员工列表）

**集成点**：`gui/frontend/src/components/ai/` 目录，"最近任务"区域

新增 `VirtualEmployeeTab.tsx` 组件：

```typescript
interface VirtualEmployeeEntry {
    id: string;
    name: string;
    skillDescription: string;
    accessPolicy: "public" | "whitelist" | "blacklist" | "per_request";
    onlineStatus: "online" | "offline";
}

interface VETabProps {
    onStartConversation: (ve: VirtualEmployeeEntry) => void;
    onAddToGroup: (ve: VirtualEmployeeEntry, tabId: string) => void;
}
```

**数据获取**：
- 初始加载：Wails binding `ListVirtualEmployees()` → Hub HTTP API
- 实时更新：复用现有 WebSocket 事件监听，新增 `ve:list_update` 事件类型
- 节流：500ms throttle，防止高频 push 导致渲染抖动

**交互**：
- 双击 / 右键菜单"对话" → 触发 `onStartConversation`
- 右键菜单"添加到群聊" → 触发 `onAddToGroup`

### Per-Request 授权流程

**集成点**：Hub WebSocket 通道 + Maclaw Client 事件系统

```
发起方 Maclaw                Hub                    VE 所有者 Maclaw
     │                        │                           │
     │── initiate_session ──▶│                           │
     │                        │── auth_request ─────────▶│
     │                        │                           │── 显示授权弹窗
     │                        │◀── auth_response ────────│
     │◀── session_created ───│  (allow/deny)             │
     │   or access_denied     │                           │
```

**Hub 侧**：
- `ve/auth_handler.go`：拦截 per_request VE 的 session 创建请求
- 生成 `AuthorizationRequest`，通过 WebSocket push 到 VE 所有者
- 60s 超时 → 通知发起方 timeout

**Client 侧**：
- 监听 `ve:auth_request` WebSocket 事件
- VE Tab 右上角闪烁指示器
- 授权弹窗组件 `VEAuthorizationDialog.tsx`
- 响应通过 WebSocket 回传 Hub

### AI Assistant Panel Tab 系统

**集成点**：`gui/frontend/src/components/ai/AIAssistantPanel.tsx`

将现有单一对话视图重构为 Tab 容器：

```typescript
interface AITab {
    id: string;
    type: "local" | "ve_conversation" | "ve_group";
    title: string;
    veId?: string;           // VE conversation target
    participants?: string[]; // group chat participants
    closable: boolean;
}

interface AIAssistantPanelState {
    tabs: AITab[];
    activeTabId: string;
    maxVETabs: number; // 8
}
```

**Tab 管理规则**：
- 第一个 Tab 固定为 "AI 助手"（`type: "local"`，`closable: false`）
- VE 对话 Tab 最多 8 个（总计 9 个含本地 AI）
- 每个 Tab 独立维护：conversation history、scroll position、input text、streaming state
- Tab 切换时保存/恢复状态（React state + ref）

**组件结构**：
```
AIAssistantPanel (Tab 容器)
├── AITabBar (Tab 栏)
│   ├── AITabItem (固定: AI 助手)
│   └── AITabItem[] (动态: VE 对话 Tabs)
├── LocalAIAssistantView (现有功能，提取为子组件)
└── VEConversationView (VE 对话视图，复用消息渲染组件)
```

**重构策略**：
1. 将 `AIAssistantPanel.tsx` 中的对话逻辑提取到 `LocalAIAssistantView.tsx`
2. `AIAssistantPanel.tsx` 变为纯 Tab 容器 + 路由
3. 新增 `VEConversationView.tsx`，复用 `MessageBubble`、`StreamingIndicator` 等现有组件
4. 消息发送路由：local Tab → 现有 `SendAIAssistantMessage` binding；VE Tab → A2A 协议

### A2A Chat 集成

**集成点**：`corelib/a2a/hub_client.go` 的 `HubClient`

VE 对话复用现有 A2A 协议类型和 Hub 路由：

| 操作 | 复用的类型/方法 | 说明 |
|------|---------------|------|
| 创建会话 | `HubClient.CreateConsultation()` | `GroupConsultationRequest.FromID` = 发起方 agent_id |
| 发送消息 | `HubClient.SendDiscussionMessage()` | `GroupDiscussionMessage.Kind = "statement"` |
| 接收消息 | WebSocket `GroupEnvelope` 事件 | `Type = GroupMessageDiscussionMessage` |
| 结束会话 | `HubClient.SetConsultationState("cancel")` | Tab 关闭时调用 |

**VE 侧消息处理**：
- 收到 `GroupEnvelope`（Type=discussion_message）→ 提取 content
- 调用本地 AI Agent（复用 `IMMessageHandler` 的 agent loop）
- 响应通过 `SendDiscussionMessage` 回传
- 流式响应：每个 chunk 作为独立 `GroupDiscussionMessage` 发送（`kind="stream_chunk"`）

**新增 MessageKind**：
```go
// corelib/a2a/types.go 扩展
const (
    MessageKindStreamChunk MessageKind = "stream_chunk"  // 流式响应片段
    MessageKindStreamEnd   MessageKind = "stream_end"    // 流式响应结束
)
```

### Group Chat 集成

**集成点**：`corelib/a2a/hub_group.go` 的 `GroupInvitation`/`GroupDiscussionMessage`

群聊完全复用现有 A2A Group Discussion 机制：

1. 用户在 VE 对话 Tab 点击 "+" → 选择要添加的 VE
2. 发送 `GroupInvitation`（`Role: GroupRoleSpeak`）到目标 VE
3. 目标 VE 的 `ShouldAutoAcceptGroupInvitation` 判断是否自动接受
4. 接受后，消息通过 `GroupDiscussionMessage`（`scope: "current_hub"`）广播到所有参与者
5. 每个参与者的响应带 `FromID` 前缀标签显示

**参与者上限**：
- Hub Admin Panel 配置 `max_group_participants`（1-10，默认 5）
- Hub 在处理 `GroupInvitation` 时检查当前参与者数量
- 超限时返回 `group_full` 错误

### Online Status 管理

**集成点**：现有 Hub WebSocket heartbeat 机制

```go
// hub/internal/ve/presence.go
type PresenceManager struct {
    mu              sync.RWMutex
    veStatus        map[string]*presenceState // key: ve_id
    heartbeatInterval time.Duration           // 15s
    missThreshold   int                       // 2 consecutive misses
}

type presenceState struct {
    machineIDs    map[string]time.Time // 多实例支持：machine_id → last_heartbeat
    onlineStatus  string
}
```

**心跳检测**：
- 复用现有 WebSocket heartbeat（`heartbeat_interval_sec` 在 enrollment 时配置为 15s）
- Hub 侧 goroutine 每 15s 扫描 `presenceState`
- 某 machine_id 连续 2 次 miss（30s）→ 从 `machineIDs` 移除
- 所有 machine_id 移除 → VE 标记 offline → push `ve:status_change` 事件

**多实例支持**：
- 同一 VE 可在多个 Maclaw 实例上激活
- 任一实例在线 → VE 在线
- 所有实例离线 → VE 离线


## Data Models

### VirtualEmployee（Hub 侧）

```go
type VirtualEmployee struct {
    ID              string       `json:"id" db:"id"`                // UUID
    OwnerMachineID  string       `json:"owner_machine_id" db:"owner_machine_id"`
    OwnerAgentID    string       `json:"owner_agent_id" db:"owner_agent_id"`
    Name            string       `json:"name" db:"name"`            // max 50 chars
    SkillDesc       string       `json:"skill_description" db:"skill_description"` // max 500 chars
    AccessPolicy    AccessPolicy `json:"access_policy" db:"access_policy"`
    Status          VEStatus     `json:"status" db:"status"`
    OnlineStatus    string       `json:"online_status" db:"online_status"`
    Whitelist       []string     `json:"whitelist,omitempty"`       // JSON array in DB
    Blacklist       []string     `json:"blacklist,omitempty"`       // JSON array in DB
    GroupMaxParticipants int     `json:"group_max_participants" db:"group_max_participants"` // Hub 级配置
    RegisteredAt    time.Time    `json:"registered_at" db:"registered_at"`
    ApprovedAt      time.Time    `json:"approved_at,omitempty" db:"approved_at"`
    DisabledAt      time.Time    `json:"disabled_at,omitempty" db:"disabled_at"`
    LastHeartbeat   time.Time    `json:"last_heartbeat" db:"last_heartbeat"`
}
```

### VERegistrationRequest（Client → Hub）

```go
type VERegistrationRequest struct {
    Name         string       `json:"name"`
    SkillDesc    string       `json:"skill_description"`
    AccessPolicy AccessPolicy `json:"access_policy"`
    Whitelist    []string     `json:"whitelist,omitempty"`
    Blacklist    []string     `json:"blacklist,omitempty"`
}
```

### AuthorizationRequest（Hub → VE Owner）

```go
type AuthorizationRequest struct {
    ID              string    `json:"id"`
    RequesterName   string    `json:"requester_name"`
    RequesterMachineID string `json:"requester_machine_id"`
    TargetVEID      string    `json:"target_ve_id"`
    TargetVEName    string    `json:"target_ve_name"`
    CreatedAt       time.Time `json:"created_at"`
    ExpiresAt       time.Time `json:"expires_at"` // created_at + 60s
}

type AuthorizationResponse struct {
    RequestID string `json:"request_id"`
    Decision  string `json:"decision"` // "allow" | "deny"
}
```

### EncryptedQuotaPayload（Hub 本地存储）

```go
type EncryptedQuotaFile struct {
    Ciphertext []byte `json:"ciphertext"`
    Nonce      []byte `json:"nonce"`
    Version    int    `json:"version"` // schema version for future migration
}

type QuotaPayload struct {
    Quota     int       `json:"quota"`      // 0-10000
    HubID     string    `json:"hub_id"`
    Timestamp time.Time `json:"timestamp"`  // UTC, millisecond precision
    MAC       []byte    `json:"mac"`        // HMAC-SHA256
}
```

### WebSocket 事件类型（Hub → Client push）

```go
// 复用现有 WebSocket 事件框架，新增 VE 相关事件类型
const (
    WSEventVEListUpdate    = "ve:list_update"     // VE 列表变更
    WSEventVEStatusChange  = "ve:status_change"   // VE 在线状态变更
    WSEventVEAuthRequest   = "ve:auth_request"    // 授权请求（发给 VE 所有者）
    WSEventVEApproved      = "ve:approved"        // 注册审批通过
    WSEventVERejected      = "ve:rejected"        // 注册审批拒绝
    WSEventVEDisabled      = "ve:disabled"        // VE 被禁用
    WSEventVEGroupConfig   = "ve:group_config"    // 群聊配置变更
)
```

## API Design

### Hub VE Management API（Admin Panel 使用）

```
GET    /api/ve/list                    # 列出所有 VE（含 pending）
POST   /api/ve/{id}/approve            # 审批通过
POST   /api/ve/{id}/reject             # 审批拒绝（body: {reason})
POST   /api/ve/{id}/disable            # 禁用
PUT    /api/ve/config                  # 更新 Hub 级 VE 配置（群聊上限等）
```

### Hub VE Client API（Maclaw Client 使用）

```
POST   /api/ve/register                # 提交注册请求
GET    /api/ve/status                  # 查询自己的 VE 注册状态
PUT    /api/ve/settings                # 更新 VE 设置（name/skill/policy）
GET    /api/ve/discoverable            # 获取可见的 VE 列表（按 access_policy 过滤）
POST   /api/ve/{id}/initiate           # 发起对话（触发 per_request 授权流程）
POST   /api/ve/auth/respond            # 响应授权请求（allow/deny）
```

### A2A 消息路由（复用现有 endpoint）

```
POST   /api/a2a/consultations          # 创建 VE 对话 session（复用）
POST   /api/a2a/consultations/{id}/messages  # 发送消息（复用）
POST   /api/a2a/consultations/{id}/invites   # 群聊邀请（复用）
GET    /api/a2a/consultations/{id}/detail    # 获取对话详情（复用）
```

### Wails Bindings（Go → TypeScript）

```go
// gui/app_ve.go — 新增 Wails binding 方法
func (a *App) RegisterVirtualEmployee(name, skillDesc, policy string, list []string) error
func (a *App) UpdateVESettings(name, skillDesc, policy string, list []string) error
func (a *App) GetVEStatus() (*VEStatusResponse, error)
func (a *App) ListVirtualEmployees() ([]VirtualEmployeeEntry, error)
func (a *App) InitiateVEConversation(veID string) (*VESessionInfo, error)
func (a *App) SendVEMessage(sessionID, content string) error
func (a *App) CloseVESession(sessionID string) error
func (a *App) AddVEToGroup(sessionID, veID string) error
func (a *App) RespondAuthRequest(requestID, decision string) error
```

## Security Considerations

### 配额加密存储

- **算法**：AES-256-GCM（authenticated encryption）+ HMAC-SHA256（MAC）
- **密钥派生**：`SHA-256(Hub_Private_Key)` 作为 AES key
- **Nonce**：每次加密使用 `crypto/rand` 生成 12 字节随机 nonce
- **防篡改**：MAC 覆盖 quota + hub_id + timestamp，任何字段被修改都会导致验证失败
- **防重放**：timestamp 验证不超过 24 小时
- **防跨 Hub 复制**：hub_id 验证匹配当前 Hub identity
- **明文禁止**：配额值不出现在任何 config 文件、log 文件或 API 响应中

### Access Policy 执行

- **whitelist/blacklist**：Hub 侧在 `ListDiscoverable` 和 session 创建时双重检查
- **per_request**：Hub 侧拦截 session 创建，等待所有者授权后才建立连接
- **60s 超时**：防止授权请求无限挂起

### A2A 通讯安全

- 复用现有 Hub WebSocket TLS 加密通道
- 消息路由在 Hub 侧完成，Client 之间不直接通讯
- Session 生命周期由 Hub 管理，Tab 关闭时显式终止

### 消息内容限制

- 单条消息最大 32,000 Unicode code points
- Hub 侧验证消息大小，超限拒绝转发

## Performance Considerations

### VE 列表查询

- Hub 侧维护内存缓存（`sync.RWMutex` 保护的 `[]VirtualEmployee`）
- 列表变更时 invalidate 缓存 + WebSocket push
- Client 侧 500ms throttle 防止高频刷新

### 在线状态检测

- 复用现有 WebSocket heartbeat，不增加额外网络开销
- Hub 侧 goroutine 每 15s 扫描一次（O(n)，n = active VE 数量，通常 < 100）
- 状态变更 push 使用 WebSocket broadcast，不轮询

### 消息流式传输

- 每个 stream chunk 作为独立 `GroupDiscussionMessage` 发送
- chunk 大小由 AI Agent 的 token 生成速度决定（通常 50-200ms/chunk）
- Client 侧 200ms 内渲染收到的 chunk

### Tab 状态管理

- 每个 Tab 独立维护 conversation state（React state）
- 非活跃 Tab 不渲染 DOM（条件渲染），只保留 state
- 最多 9 个 Tab（1 local + 8 VE），内存开销可控

### 配额检查

- Hub 侧配额检查为内存操作（解密后缓存 quota 值）
- 注册请求的配额检查在 2s 内完成（AC 要求）

## Testing Strategy

### 单元测试

| 模块 | 测试重点 |
|------|---------|
| `QuotaStore` | 加密/解密 round-trip、MAC 验证失败、Hub ID 不匹配、timestamp 过期、文件损坏 |
| `VE Registry` | 注册/审批/拒绝/禁用状态机、配额检查、Access Policy 过滤 |
| `PresenceManager` | heartbeat 超时检测、多实例在线/离线逻辑 |
| `AuthHandler` | per_request 流程、60s 超时、allow/deny 路由 |
| `A2A 消息路由` | VE 对话 session 创建、消息转发、流式 chunk 传递 |

### 集成测试

| 场景 | 验证点 |
|------|--------|
| 完整注册流程 | HubCenter enrollment → Hub 配额存储 → Client 注册 → Admin 审批 → Client 通知 |
| 对话流程 | Client A → Hub → VE Client B → AI 处理 → 响应回传 → Client A 显示 |
| 群聊流程 | 创建对话 → 添加 VE → GroupInvitation → 多方消息广播 |
| 授权流程 | per_request VE → 授权请求 → 所有者响应 → session 建立/拒绝 |
| 在线状态 | WebSocket 断开 → 30s 后 offline → 重连 → online |

### 前端测试

| 组件 | 测试重点 |
|------|---------|
| `VirtualEmployeeTab` | 列表渲染、Access Policy 图标、在线状态指示器、空状态 |
| `AITabBar` | Tab 创建/切换/关闭、最大数量限制、状态保持 |
| `VEConversationView` | 消息发送/接收、流式渲染、错误状态显示 |
| `VEAuthorizationDialog` | 弹窗显示/隐藏、allow/deny 回调 |
| `VirtualEmployeeSettingsPanel` | 表单验证、policy 切换、whitelist/blacklist 编辑 |

### Property-Based Tests

- `QuotaStore` 加密 round-trip：任意 quota 值（0-10000）+ 任意 Hub ID → 加密 → 解密 → 值不变
- Access Policy 过滤：任意 VE 列表 + 任意 requester ID → `ListDiscoverable` 结果满足 policy 约束
- Tab 状态隔离：任意 Tab 切换序列 → 每个 Tab 的 conversation state 独立不串扰

## Architecture

整体架构分为三层：HubCenter（配额管控）→ Hub（注册审批 + 消息路由 + 状态管理）→ Maclaw Client（UI + A2A 通讯）。

通讯路径：Maclaw Client A ↔ Hub（WebSocket/HTTP）↔ Maclaw Client B（VE 所有者）。所有 VE 对话消息通过 Hub 中继，Client 之间不直接通讯。

## Error Handling

### 网络错误

- Hub WebSocket 断开：Client 自动重连（指数退避 2s→30s，最多 5 次），重连后重新订阅 VE 事件
- A2A 消息发送失败：10s 超时后显示发送失败标记，不自动重试（用户可手动重发）
- Hub HTTP API 超时：5s 超时，返回友好错误提示

### 业务错误

- 配额超限：返回 `quota_exceeded` 错误码 + 中文提示"虚拟员工配额已满"
- 访问被拒：返回 `access_denied` 错误码 + 中文提示"访问被拒绝"
- VE 离线：返回 `ve_offline` 错误码 + 中文提示"该虚拟员工当前不在线"
- 授权超时：60s 后自动拒绝 + 通知发起方"授权请求超时"

### 数据完整性

- 配额文件损坏/缺失：quota 降级为 0，拒绝所有新注册，log security warning
- MAC 验证失败：quota 降级为 0，log security warning（可能被篡改）
- Hub ID 不匹配：quota 降级为 0，log security warning（可能被跨 Hub 复制）

## Correctness Properties

### Property 1: 配额不变量
任何时刻，active 状态的 VE 数量 ≤ VE_Quota（配额降低时允许暂时超出，但不允许新增）
**Validates: Requirement 1.6, 1.10**

### Property 2: Access Policy 一致性
VE 列表可见性和 session 创建权限检查使用同一份 policy 数据，不存在"看得到但连不上"或"看不到但能连上"的不一致
**Validates: Requirement 4.4, 4.5, 4.6, 4.7**

### Property 3: Tab 状态隔离
任意 Tab 的操作（发送消息、关闭、切换）不影响其他 Tab 的 conversation state
**Validates: Requirement 6.10**

### Property 4: 在线状态最终一致
VE 实际连接状态变更后，所有 Client 的 VE 列表在 5s 内收敛到正确状态
**Validates: Requirement 9.5**

### Property 5: 授权原子性
per_request 授权的 allow/deny 决策是原子的——不存在"已授权但 session 未建立"或"已拒绝但 session 已建立"的中间状态
**Validates: Requirement 5.5, 5.6**

### Property 6: 消息有序性
同一 A2A Session 内的消息按发送顺序到达（Hub 中继保证 FIFO）
**Validates: Requirement 7.9**

### Property 7: 群聊参与者上限
任何时刻，单个 Group Chat 的参与者数量 ≤ Hub 配置的 max_group_participants
**Validates: Requirement 8.8**
