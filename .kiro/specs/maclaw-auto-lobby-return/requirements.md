# 需求文档：设备掉线自动返回大厅

## 简介

当多个 Maclaw 客户端接入同一个 Hub 帐号时，用户可以通过 `/call <昵称>` 进入与某台设备的私聊模式。当前，如果该设备掉线，系统仅提示用户手动执行 `/call all` 返回大厅，用户体验不佳。本需求要求系统在检测到当前聊天对象（选定设备）掉线时，自动将用户状态切回大厅模式，并发送简洁的通知。

## 术语表

- **Device_Notifier**: 设备上下线通知器，负责在设备连接状态变化时向用户发送 IM 通知，包含 30 秒防抖机制（对应 `DeviceNotifier`）
- **Space_State_Store**: 用户空间状态管理器，维护每个用户当前所处的交互空间（大厅/私聊/会议），内存存储（对应 `spaceStateStore`）
- **Message_Router**: 消息路由器，管理用户的设备选择（`selectedMachine`）并将 IM 消息路由到目标 Maclaw 客户端（对应 `MessageRouter`）
- **Coordinator**: 消息协调器，在 IM Adapter 和 Message_Router 之间的中间件，实现智能路由和空间状态调度（对应 `Coordinator`）
- **Space_Lobby**: 大厅模式，用户未选定特定设备，消息根据规则引擎或 LLM 分类器路由
- **Space_Private**: 私聊模式，用户通过 `/call <昵称>` 选定了某台设备，所有消息直接发送到该设备
- **Space_Meeting**: 会议模式，用户通过 `/discuss` 发起多设备讨论

## 需求

### 需求 1：私聊目标掉线时自动返回大厅

**用户故事：** 作为开发者，我希望当我正在私聊的设备掉线时，系统自动将我切回大厅模式，以便我无需手动执行命令即可继续与其他在线设备交互。

#### 验收标准

1. WHEN Device_Notifier 检测到某设备离线且该设备是用户当前的私聊目标时，THE Device_Notifier SHALL 调用 Space_State_Store 将用户状态从 Space_Private 切换为 Space_Lobby
2. WHEN Device_Notifier 将用户状态切回 Space_Lobby 时，THE Device_Notifier SHALL 同时清除 Message_Router 中该用户的 selectedMachine 记录
3. WHEN 私聊目标掉线触发自动返回大厅后，THE Device_Notifier SHALL 向用户发送一条简洁的通知消息，格式为："📴 <设备名> 已离线，已自动返回大厅。"
4. THE Device_Notifier SHALL 在自动返回大厅的通知中不再包含手动切换设备的指引（移除原有的 "/call <昵称>" 提示）

### 需求 2：会议参与者掉线时的空间状态维护

**用户故事：** 作为开发者，我希望当会议中的某台设备掉线时，系统能正确维护会议状态，以便会议不会因单台设备掉线而异常。

#### 验收标准

1. WHEN Device_Notifier 检测到某设备离线且用户当前处于 Space_Meeting 时，THE Device_Notifier SHALL 调用 Space_State_Store 将该设备从会议参与者列表中移除
2. WHEN 会议参与者列表中仅剩一台设备时，THE Device_Notifier SHALL 向用户发送提示："📴 <设备名> 已离线，会议仅剩 1 台设备参与。"
3. IF 会议参与者列表变为空（所有参与设备均离线），THEN THE Device_Notifier SHALL 自动结束会议并将用户状态切回 Space_Lobby，通知用户："📴 所有会议设备已离线，会议已结束，已返回大厅。"

### 需求 3：非选定设备掉线时的通知简化

**用户故事：** 作为开发者，我希望非当前交互设备掉线时收到的通知足够简洁，以便不干扰我的工作流。

#### 验收标准

1. WHEN Device_Notifier 检测到某设备离线且该设备不是用户当前的私聊目标或会议参与者时，THE Device_Notifier SHALL 仅发送简洁的离线通知："📴 <设备名> 已离线"
2. THE Device_Notifier SHALL 保持现有的 30 秒防抖机制，避免网络抖动导致的通知风暴
