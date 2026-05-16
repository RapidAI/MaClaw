# IM Progress i18n 任务列表

## Task 1: 创建 i18n 包和翻译表
- [x] 创建 `corelib/i18n/i18n.go`
- [x] 实现 `T(key, lang string) string` 函数，lang 为空时 fallback 到 "zh"
- [x] 实现 `Tf(key, lang string, args ...interface{}) string` 用于带参数的格式化翻译
- [x] 定义翻译 key 常量（如 `MsgAckProcessing`、`MsgTaskComplex`、`MsgAgentRound` 等）
- [x] 填充 zh-CN 和 en 翻译表
- [x] 编写单元测试：验证 key 查找、fallback、格式化

涉及文件：`corelib/i18n/i18n.go`、`corelib/i18n/i18n_test.go`

## Task 2: 语言检测与传递机制
- [x] 在 `IMUserMessage` 结构体中添加 `Lang string` 字段
- [x] 微信/飞书/QQ gateway：默认设置 `Lang = "zh"`
- [x] Telegram gateway：从 `msg.From.LanguageCode` 提取语言，映射到 "zh"/"en"
- [x] `HandleIMMessageWithProgress` 将 lang 传递给 onProgress 回调
- [x] Hub 模式：在 gateway_message payload 中传递 lang 字段

涉及文件：`gui/im_message_handler.go`、`gui/weixin_gateway.go`、`gui/telegram_gateway.go`、`gui/qqbot_gateway.go`

## Task 3: 替换 im_message_handler.go 中的硬编码字符串
- [x] ack 消息：`"⏳ 需要一点时间处理，请稍候..."` → `i18n.T("msg.ack_processing", lang)`
- [x] 任务复杂：`"⏳ 任务较复杂，仍在处理中，请稍候..."` → `i18n.T("msg.task_complex", lang)`
- [x] 推理轮次：`"🔄 Agent 推理中（第 %d/%d 轮）…"` → `i18n.Tf("msg.agent_round_of", lang, iteration+1, effectiveMax)`
- [x] 推理轮次（无上限）：`"🔄 Agent 推理中（第 %d 轮）…"` → `i18n.Tf("msg.agent_round", lang, iteration+1)`
- [x] 轮次用完：`"⏳ 推理轮次已用完..."` → `i18n.T("msg.rounds_exhausted", lang)`
- [x] `inferFileDeliveryMessage` 中的 PDF 交付提示

涉及文件：`gui/im_message_handler.go`

## Task 4: 替换 gateway 文件中的硬编码字符串
- [x] `gui/weixin_gateway.go`：Hub 不可用提示、LLM 未配置提示、progress 前缀
- [x] `gui/telegram_gateway.go`：同上
- [x] `gui/qqbot_gateway.go`：同上
- [x] `gui/im_pending_media.go`：收到文件/图片提示
- [x] `corelib/weixin/gateway.go`：排队提示

涉及文件：各 gateway 文件

## Task 5: 替换 Hub 端和 TUI 端的硬编码字符串
- [x] `hub/internal/im/router.go`：多设备回复标题、并发已满提示
- [x] `tui/agent_tools.go`：编程工具停滞/卡住提示

涉及文件：`hub/internal/im/router.go`、`tui/agent_tools.go`

## Task 6: 更新测试
- [x] 更新 `hub/internal/im/router_progress_test.go` 中的 progress 文本匹配
- [x] 确保所有现有测试通过（文案变更不应破坏逻辑测试）
- [x] 验证 `go build ./...` 编译通过
