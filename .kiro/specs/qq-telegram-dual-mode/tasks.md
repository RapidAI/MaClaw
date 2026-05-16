# 实现任务：QQ Bot / Telegram 网关本地/Hub 双模式支持

## 任务 1：AppConfig 添加 QQ Bot 和 Telegram 本地模式配置字段

- [ ] 在 `corelib/app_config.go` 的 `AppConfig` 结构体中添加 `QQBotLocalMode *bool` 字段（JSON 标签 `qqbot_local_mode,omitempty`），紧跟 `QQBotAppSecret` 之后
- [ ] 在 `corelib/app_config.go` 的 `AppConfig` 结构体中添加 `TelegramLocalMode *bool` 字段（JSON 标签 `telegram_local_mode,omitempty`），紧跟 `TelegramBotToken` 之后
- [ ] 添加 `IsQQBotLocalMode() bool` 方法：nil 返回 `true`（默认本地模式），否则返回 `*QQBotLocalMode`
- [ ] 添加 `SetQQBotLocal(v bool)` 方法：设置 `QQBotLocalMode` 指针
- [ ] 添加 `IsTelegramLocalMode() bool` 方法：nil 返回 `true`（默认本地模式），否则返回 `*TelegramLocalMode`
- [ ] 添加 `SetTelegramLocal(v bool)` 方法：设置 `TelegramLocalMode` 指针
- [ ] 验证：`go build ./corelib/...` 编译通过

需求映射：需求 1（验收标准 1-3）、需求 2（验收标准 1-3）

## 任务 2：QQ Bot 网关添加本地模式消息路由和处理

- [ ] 在 `gui/qqbot_gateway.go` 的 `qqBotGatewayManager` 结构体中添加 `localHandler *IMMessageHandler` 字段
- [ ] 添加 `ensureLocalHandler() *IMMessageHandler` 方法，懒加载创建 IMMessageHandler 并注入所有子系统（参照 `weixinGatewayManager.ensureLocalHandler`）
- [ ] 添加 `resetLocalHandler()` 方法，销毁缓存的 localHandler（调用 `localHandler.memory.stop()`）
- [ ] 修改 `Stop()` 方法：在清理时也销毁 localHandler
- [ ] 修改 `onIncomingMessage`：读取 `cfg.IsQQBotLocalMode()`，本地模式调用 `handleLocalMessage`，Hub 模式保持现有 `forwardToHub` 逻辑
- [ ] 添加 `handleLocalMessage(msg qqbot.IncomingMessage)` 方法：检查 LLM 配置 → 调用 `ensureLocalHandler` → 构建 `IMUserMessage` → 调用 `HandleIMMessageWithProgress` → 发送响应
- [ ] 添加 `sendAgentResponse(gw *qqbot.Gateway, openID string, resp *IMAgentResponse)` 方法：处理文本/图片/文件/错误回复
- [ ] 将现有 Hub 转发逻辑提取为 `forwardToHub(msg qqbot.IncomingMessage)` 方法
- [ ] 修改 `onStatusChange`：本地模式下跳过 Gateway Claim（参照微信的 `onStatusChange`）
- [ ] 添加进度回调（progress callback）：每 5 秒最多发送一条中间状态消息
- [ ] 验证：`go build ./gui/...` 编译通过

需求映射：需求 3（验收标准 1-7）、需求 5（验收标准 1-5）、需求 6（验收标准 1）

## 任务 3：Telegram 网关添加本地模式消息路由和处理

- [ ] 在 `gui/telegram_gateway.go` 的 `telegramGatewayManager` 结构体中添加 `localHandler *IMMessageHandler` 字段
- [ ] 添加 `ensureLocalHandler() *IMMessageHandler` 方法（同 QQ Bot 模式）
- [ ] 添加 `resetLocalHandler()` 方法
- [ ] 修改 `Stop()` 方法：在清理时也销毁 localHandler
- [ ] 修改 `onIncomingMessage`：读取 `cfg.IsTelegramLocalMode()`，本地模式调用 `handleLocalMessage`，Hub 模式保持现有 `forwardToHub` 逻辑
- [ ] 添加 `handleLocalMessage(msg telegram.IncomingMessage)` 方法：检查 LLM 配置 → 调用 `ensureLocalHandler` → 构建 `IMUserMessage`（platform_uid 为 `strconv.FormatInt(msg.ChatID, 10)`）→ 调用 `HandleIMMessageWithProgress` → 发送响应
- [ ] 添加 `sendAgentResponse(gw *telegram.Gateway, chatID int64, resp *IMAgentResponse)` 方法：处理文本/图片/文件/错误回复
- [ ] 将现有 Hub 转发逻辑提取为 `forwardToHub(msg telegram.IncomingMessage)` 方法
- [ ] 修改 `onStatusChange`：本地模式下跳过 Gateway Claim（参照微信的 `onStatusChange`）
- [ ] 添加进度回调（progress callback）：每 5 秒最多发送一条中间状态消息
- [ ] 验证：`go build ./gui/...` 编译通过

需求映射：需求 4（验收标准 1-7）、需求 5（验收标准 1-5）、需求 6（验收标准 2）

## 任务 4：Hub Client syncIMGatewayClaims 添加本地模式检查

- [ ] 修改 `gui/remote_hub_client.go` 的 `syncIMGatewayClaims` 方法：QQ Bot claim 前检查 `cfg.IsQQBotLocalMode()`，仅 Hub 模式下发送
- [ ] 修改 `gui/remote_hub_client.go` 的 `syncIMGatewayClaims` 方法：Telegram claim 前检查 `cfg.IsTelegramLocalMode()`，仅 Hub 模式下发送
- [ ] 验证：`go build ./gui/...` 编译通过

需求映射：需求 6（验收标准 3）

## 任务 5：Wails 绑定方法 — 模式查询和切换

- [ ] 在 `gui/qqbot_gateway.go` 添加 `App.GetQQBotLocalMode() bool` 方法：读取 `cfg.IsQQBotLocalMode()`，默认返回 `true`
- [ ] 在 `gui/qqbot_gateway.go` 添加 `App.SetQQBotLocalMode(enabled bool) error` 方法：保存配置 → 重置 localHandler → 非本地模式时发送 Gateway Claim
- [ ] 在 `gui/telegram_gateway.go` 添加 `App.GetTelegramLocalMode() bool` 方法：读取 `cfg.IsTelegramLocalMode()`，默认返回 `true`
- [ ] 在 `gui/telegram_gateway.go` 添加 `App.SetTelegramLocalMode(enabled bool) error` 方法：保存配置 → 重置 localHandler → 非本地模式时发送 Gateway Claim
- [ ] 验证：`go build ./gui/...` 编译通过

需求映射：需求 7（验收标准 1-6）、需求 6（验收标准 4）

## 任务 6：前端 UI — QQ Bot 和 Telegram 模式切换开关

- [ ] 在 `gui/frontend/src/App.tsx` 添加 `qqBotLocalMode` 和 `telegramLocalMode` 状态变量
- [ ] 在 `gui/frontend/src/App.tsx` 的 import 中添加 `GetQQBotLocalMode, SetQQBotLocalMode, GetTelegramLocalMode, SetTelegramLocalMode`
- [ ] 在 useEffect 初始化中调用 `GetQQBotLocalMode().then(...)` 和 `GetTelegramLocalMode().then(...)` 获取初始状态
- [ ] 在 QQ Bot 设置区域（`imSubTab === 'qq'`）添加"单机/多机"模式切换按钮组（参照微信的模式切换 UI 代码）
- [ ] 在 Telegram 设置区域（`imSubTab === 'telegram'`）添加"单机/多机"模式切换按钮组
- [ ] 在 useEffect cleanup 中无需额外清理（状态变量随组件销毁）
- [ ] 验证：前端编译通过（`npm run build` 或 Wails 构建）

需求映射：需求 8（验收标准 1-4）
