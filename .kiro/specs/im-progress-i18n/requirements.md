# IM Progress 消息多语言本地化

## 背景

MaClaw 的 IM 通道（微信、飞书、Telegram、QQ）中，所有 progress/status 提示消息目前都是硬编码的中文字符串，没有多语言支持。对于非中文用户（如 Telegram 用户），这些消息不友好。

## 需求

### FR-1: 建立 i18n 基础设施
- 创建翻译表（至少支持 zh-CN 和 en）
- 提供 `T(key string, lang string) string` 查找函数
- 翻译表可以是简单的 Go map，不需要外部文件

### FR-2: 语言检测策略
- 微信/飞书：默认中文
- Telegram：根据用户 language_code 字段检测
- QQ：默认中文
- 支持用户通过配置覆盖

### FR-3: 提取硬编码字符串
涉及的文件和消息类型：
- `gui/im_message_handler.go`: ack 消息、任务复杂提示、推理轮次提示、推理轮次用完提示
- `gui/weixin_gateway.go`: progress 前缀、Hub 不可用提示、LLM 未配置提示
- `gui/telegram_gateway.go`: 同上
- `gui/qqbot_gateway.go`: 同上
- `gui/im_pending_media.go`: 收到文件/图片提示
- `gui/im_message_handler.go` (`inferFileDeliveryMessage`): PDF 交付提示
- `hub/internal/im/router.go`: 多设备回复标题
- `corelib/weixin/gateway.go`: 排队提示
- `tui/agent_tools.go`: 编程工具停滞提示

### FR-4: 不影响现有行为
- 默认语言为中文，现有用户体验不变
- 翻译缺失时 fallback 到中文
