# 需求文档：QQ Bot / Telegram 网关本地/Hub 双模式支持

## 简介

为 QQ Bot 网关（`qqBotGatewayManager`）和 Telegram 网关（`telegramGatewayManager`）添加本地模式（单机）支持，使其与微信网关（`weixinGatewayManager`）具备相同的双模式架构。当前 QQ Bot 和 Telegram 网关仅支持 Hub 模式（多机），消息必须转发到 Hub 处理；添加本地模式后，消息可直接路由到本地 MaClaw LLM agent loop，无需 Hub 连接即可工作。

## 术语表

- **Gateway_Manager**：客户端侧的 IM 网关管理器（`qqBotGatewayManager` 或 `telegramGatewayManager`），负责启停底层网关、路由消息
- **Local_Mode**：本地/单机模式，消息直接由本地 MaClaw LLM agent loop 处理，不经过 Hub
- **Hub_Mode**：远程/多机模式，消息通过 `im.gateway_message` 转发到 Hub，Hub 通过 `im.gateway_reply` 返回回复
- **AppConfig**：应用配置结构体（`corelib.AppConfig`），持久化存储用户设置
- **IMMessageHandler**：本地 IM 消息处理器，封装 LLM agent loop、工具注册、对话记忆等能力
- **Hub_Client**：远程 Hub WebSocket 客户端（`RemoteHubClient`），负责与 Hub 通信
- **Gateway_Claim**：网关锁声明，客户端向 Hub 注册自己为某平台的网关持有者
- **Wails_Binding**：Wails 框架的前端绑定方法，允许前端 UI 调用 Go 后端函数

## 需求

### 需求 1：QQ Bot 网关本地模式配置

**用户故事：** 作为用户，我希望在 AppConfig 中配置 QQ Bot 网关的运行模式，以便选择消息走本地处理还是 Hub 转发。

#### 验收标准

1. THE AppConfig SHALL 包含 `QQBotLocalMode` 字段（类型 `*bool`，JSON 标签 `qqbot_local_mode`），语义与 `WeixinLocalMode` 一致
2. THE AppConfig SHALL 提供 `IsQQBotLocalMode()` 方法，WHEN `QQBotLocalMode` 为 nil 时返回 `true`（默认本地模式）
3. THE AppConfig SHALL 提供 `SetQQBotLocal(v bool)` 方法，设置 `QQBotLocalMode` 指针字段

### 需求 2：Telegram 网关本地模式配置

**用户故事：** 作为用户，我希望在 AppConfig 中配置 Telegram 网关的运行模式，以便选择消息走本地处理还是 Hub 转发。

#### 验收标准

1. THE AppConfig SHALL 包含 `TelegramLocalMode` 字段（类型 `*bool`，JSON 标签 `telegram_local_mode`），语义与 `WeixinLocalMode` 一致
2. THE AppConfig SHALL 提供 `IsTelegramLocalMode()` 方法，WHEN `TelegramLocalMode` 为 nil 时返回 `true`（默认本地模式）
3. THE AppConfig SHALL 提供 `SetTelegramLocal(v bool)` 方法，设置 `TelegramLocalMode` 指针字段

### 需求 3：QQ Bot 网关本地消息处理

**用户故事：** 作为用户，我希望 QQ Bot 网关在本地模式下能直接通过本地 LLM agent loop 处理消息并回复，无需 Hub 连接。

#### 验收标准

1. WHEN 收到 QQ 消息且 AppConfig 处于本地模式时，THE Gateway_Manager SHALL 将消息路由到本地 IMMessageHandler 处理
2. WHEN 收到 QQ 消息且 AppConfig 处于 Hub 模式时，THE Gateway_Manager SHALL 将消息转发到 Hub（保持现有行为）
3. WHEN 处于本地模式且本地 LLM 未配置时，THE Gateway_Manager SHALL 向用户发送提示消息"本地 LLM 未配置，请先在设置中配置 MaClaw LLM"
4. WHEN 处于本地模式且 Hub 未连接时，THE Gateway_Manager SHALL 正常处理消息（不依赖 Hub）
5. WHEN 处于 Hub 模式且 Hub 未连接时，THE Gateway_Manager SHALL 向用户发送提示消息"Hub 未连接"（保持现有行为）
6. THE Gateway_Manager SHALL 懒加载创建 `localHandler`（IMMessageHandler 实例），并注入与微信网关相同的子系统（toolRouter、memoryStore、configManager 等）
7. WHEN 用户切换模式时，THE Gateway_Manager SHALL 重置已缓存的 localHandler

### 需求 4：Telegram 网关本地消息处理

**用户故事：** 作为用户，我希望 Telegram 网关在本地模式下能直接通过本地 LLM agent loop 处理消息并回复，无需 Hub 连接。

#### 验收标准

1. WHEN 收到 Telegram 消息且 AppConfig 处于本地模式时，THE Gateway_Manager SHALL 将消息路由到本地 IMMessageHandler 处理
2. WHEN 收到 Telegram 消息且 AppConfig 处于 Hub 模式时，THE Gateway_Manager SHALL 将消息转发到 Hub（保持现有行为）
3. WHEN 处于本地模式且本地 LLM 未配置时，THE Gateway_Manager SHALL 向用户发送提示消息"本地 LLM 未配置，请先在设置中配置 MaClaw LLM"
4. WHEN 处于本地模式且 Hub 未连接时，THE Gateway_Manager SHALL 正常处理消息（不依赖 Hub）
5. WHEN 处于 Hub 模式且 Hub 未连接时，THE Gateway_Manager SHALL 向用户发送提示消息"Hub 未连接"（保持现有行为）
6. THE Gateway_Manager SHALL 懒加载创建 `localHandler`（IMMessageHandler 实例），并注入与微信网关相同的子系统
7. WHEN 用户切换模式时，THE Gateway_Manager SHALL 重置已缓存的 localHandler

### 需求 5：本地模式下的 Agent 响应发送

**用户故事：** 作为用户，我希望本地模式下 LLM agent 的回复（文本、图片、文件）能正确通过 QQ/Telegram API 发送给我。

#### 验收标准

1. WHEN 本地 agent 返回文本回复时，THE Gateway_Manager SHALL 通过对应平台 API 发送文本消息
2. WHEN 本地 agent 返回图片（base64 编码）时，THE Gateway_Manager SHALL 通过对应平台 API 发送图片消息
3. WHEN 本地 agent 返回文件时，THE Gateway_Manager SHALL 通过对应平台 API 发送文件消息
4. WHEN 本地 agent 返回错误且无文本时，THE Gateway_Manager SHALL 将错误信息作为文本消息发送
5. THE Gateway_Manager SHALL 支持进度回调（progress callback），在 agent 处理过程中向用户发送中间状态消息，频率限制为每 5 秒最多一条

### 需求 6：Gateway Claim 的本地模式检查

**用户故事：** 作为系统，我希望处于本地模式的网关不向 Hub 发送 Gateway Claim，避免不必要的锁竞争。

#### 验收标准

1. WHEN QQ Bot 网关处于本地模式且连接状态变为 "connected" 时，THE Gateway_Manager SHALL 跳过向 Hub 发送 Gateway_Claim
2. WHEN Telegram 网关处于本地模式且连接状态变为 "connected" 时，THE Gateway_Manager SHALL 跳过向 Hub 发送 Gateway_Claim
3. WHEN Hub_Client 重连并调用 `syncIMGatewayClaims` 时，THE Hub_Client SHALL 检查 QQ Bot 和 Telegram 的本地模式配置，仅在 Hub 模式下发送 claim
4. WHEN 用户从本地模式切换到 Hub 模式时，THE Wails_Binding SHALL 立即发送 Gateway_Claim 到 Hub（参照微信的 `SetWeixinLocalMode` 实现）

### 需求 7：Wails 绑定方法

**用户故事：** 作为前端开发者，我希望有 Wails 绑定方法来查询和切换 QQ Bot / Telegram 的运行模式，以便在 UI 中实现模式切换开关。

#### 验收标准

1. THE App SHALL 提供 `GetQQBotLocalMode() bool` 方法，返回当前 QQ Bot 本地模式状态
2. THE App SHALL 提供 `SetQQBotLocalMode(enabled bool) error` 方法，保存配置并触发必要的状态同步
3. THE App SHALL 提供 `GetTelegramLocalMode() bool` 方法，返回当前 Telegram 本地模式状态
4. THE App SHALL 提供 `SetTelegramLocalMode(enabled bool) error` 方法，保存配置并触发必要的状态同步
5. WHEN `SetQQBotLocalMode(false)` 被调用（切换到 Hub 模式）时，THE App SHALL 向 Hub 发送 `qqbot_remote` 平台的 Gateway_Claim
6. WHEN `SetTelegramLocalMode(false)` 被调用（切换到 Hub 模式）时，THE App SHALL 向 Hub 发送 `telegram` 平台的 Gateway_Claim

### 需求 8：前端 UI 模式切换

**用户故事：** 作为用户，我希望在前端界面中看到 QQ Bot 和 Telegram 的模式切换开关，以便方便地在本地模式和 Hub 模式之间切换。

#### 验收标准

1. THE 前端 SHALL 在 QQ Bot 设置区域显示"本地模式 / Hub 模式"切换开关
2. THE 前端 SHALL 在 Telegram 设置区域显示"本地模式 / Hub 模式"切换开关
3. WHEN 用户切换开关时，THE 前端 SHALL 调用对应的 Wails 绑定方法（`SetQQBotLocalMode` / `SetTelegramLocalMode`）
4. THE 前端 SHALL 显示当前模式状态，并在切换后更新显示
