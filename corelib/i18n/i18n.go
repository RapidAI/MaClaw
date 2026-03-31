// Package i18n provides lightweight internationalisation for user-facing
// progress and status messages across IM channels (WeChat, Telegram, QQ, Feishu).
//
// Usage:
//
//	i18n.T(i18n.MsgAckProcessing, "en")       // English
//	i18n.T(i18n.MsgAckProcessing, "")         // fallback → zh
//	i18n.Tf(i18n.MsgAgentRoundOf, "en", 2, 5) // formatted
package i18n

import "fmt"

// ---------------------------------------------------------------------------
// Translation key constants
// ---------------------------------------------------------------------------

const (
	// im_message_handler.go – progress messages
	MsgAckProcessing = "msg.ack_processing"
	MsgTaskComplex   = "msg.task_complex"
	MsgAgentRoundOf  = "msg.agent_round_of"  // with max: %d/%d
	MsgAgentRound    = "msg.agent_round"      // without max: %d
	MsgRoundsExhaust = "msg.rounds_exhausted" // rounds used up
	MsgMaxRounds     = "msg.max_rounds"       // max rounds reached hint

	// im_message_handler.go – inferFileDeliveryMessage
	MsgFileRequirements = "msg.file_requirements"
	MsgFileDesign       = "msg.file_design"
	MsgFileTaskList     = "msg.file_task_list"
	MsgFileGeneric      = "msg.file_generic" // %s filename

	// gateway – LLM / Hub status
	MsgLLMNotConfigured = "msg.llm_not_configured"
	MsgHubUnavailable   = "msg.hub_unavailable"
	MsgProgressPrefix   = "msg.progress_prefix"

	// im_pending_media.go
	MsgMediaSingle   = "msg.media_single"
	MsgMediaMultiple = "msg.media_multiple" // %d count

	// corelib/weixin/gateway.go
	MsgMessageQueued = "msg.message_queued"

	// hub/internal/im/router.go
	MsgNoOnlineDevices   = "msg.no_online_devices"
	MsgLLMConcurrencyFull = "msg.llm_concurrency_full"
	MsgMultiDeviceReply  = "msg.multi_device_reply"
	MsgGroupChatReply    = "msg.group_chat_reply"

	// tui/agent_tools.go
	MsgStallSuspected = "msg.stall_suspected"
	MsgStallStuck     = "msg.stall_stuck"
	MsgToolWorking    = "msg.tool_working"
	MsgWaitingInput   = "msg.waiting_input"
	MsgSessionExited  = "msg.session_exited"
	MsgSessionError   = "msg.session_error"
)

// defaultLang is the fallback language when lang is empty or unknown.
const defaultLang = "zh"

// ---------------------------------------------------------------------------
// Translation tables
// ---------------------------------------------------------------------------

// translations maps lang → key → translated string.
// Format verbs (e.g. %d, %s) are preserved for use with Tf.
var translations = map[string]map[string]string{
	"zh": {
		MsgAckProcessing:     "⏳ 需要一点时间处理，请稍候...",
		MsgTaskComplex:       "⏳ 任务较复杂，仍在处理中，请稍候...",
		MsgAgentRoundOf:      "🔄 Agent 推理中（第 %d/%d 轮）…",
		MsgAgentRound:        "🔄 Agent 推理中（第 %d 轮）…",
		MsgRoundsExhaust:     "⏳ 推理轮次已用完，但编程会话仍在运行，正在检查状态…",
		MsgMaxRounds:         "(已达到最大推理轮次，请继续发送消息以完成任务)",
		MsgFileRequirements:  "📋 需求文档已生成，请查看并确认需求是否准确，或提出修改意见。",
		MsgFileDesign:        "🏗️ 技术设计文档已生成，请查看设计方案并确认，或提出修改意见。",
		MsgFileTaskList:      "📝 任务列表已生成，请查看任务拆分是否合理，确认后开始执行。",
		MsgFileGeneric:       "📄 已生成文件 %s，请查看并确认，或提出修改意见。",
		MsgLLMNotConfigured:  "⚠️ 本地 LLM 未配置，请先在设置中配置 MaClaw LLM。",
		MsgHubUnavailable:    "⚠️ 当前为多机模式，但 Hub 未连接。消息已回退到本地处理。\n请检查 Hub 连接状态，或切换回单机模式。",
		MsgProgressPrefix:    "⏳ ",
		MsgMediaSingle:       "📎 收到文件/图片了，请告诉我你希望怎么处理",
		MsgMediaMultiple:     "📎 收到 %d 个文件/图片了，请告诉我你希望怎么处理",
		MsgMessageQueued:     "⏳ 上一条消息还在处理中，你的消息已排队，请稍候…",
		MsgNoOnlineDevices:   "📴 当前没有在线设备。",
		MsgLLMConcurrencyFull: "LLM 并发已满，请稍后重试",
		MsgMultiDeviceReply:  "多设备回复",
		MsgGroupChatReply:    "群聊回复",
		MsgStallSuspected:    "⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查",
		MsgStallStuck:        "⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话",
		MsgToolWorking:       "⏳ 编程工具正在工作中，请等待后再检查进度",
		MsgWaitingInput:      "⚠️ 会话正在等待用户输入",
		MsgSessionExited:     "会话已退出",
		MsgSessionError:      "⚠️ 会话出错",
	},
	"en": {
		MsgAckProcessing:     "⏳ Processing, please wait...",
		MsgTaskComplex:       "⏳ This task is complex, still working on it...",
		MsgAgentRoundOf:      "🔄 Agent reasoning (round %d/%d)…",
		MsgAgentRound:        "🔄 Agent reasoning (round %d)…",
		MsgRoundsExhaust:     "⏳ Reasoning rounds exhausted, but the coding session is still running, checking status…",
		MsgMaxRounds:         "(Max reasoning rounds reached, please send another message to continue)",
		MsgFileRequirements:  "📋 Requirements document generated. Please review and confirm, or suggest changes.",
		MsgFileDesign:        "🏗️ Technical design document generated. Please review and confirm, or suggest changes.",
		MsgFileTaskList:      "📝 Task list generated. Please review the task breakdown and confirm to start execution.",
		MsgFileGeneric:       "📄 File %s generated. Please review and confirm, or suggest changes.",
		MsgLLMNotConfigured:  "⚠️ Local LLM not configured. Please configure MaClaw LLM in settings first.",
		MsgHubUnavailable:    "⚠️ Multi-device mode is active but Hub is disconnected. Message has been processed locally.\nPlease check Hub connection or switch to standalone mode.",
		MsgProgressPrefix:    "⏳ ",
		MsgMediaSingle:       "📎 File/image received. How would you like me to handle it?",
		MsgMediaMultiple:     "📎 Received %d files/images. How would you like me to handle them?",
		MsgMessageQueued:     "⏳ Previous message is still being processed. Your message has been queued, please wait…",
		MsgNoOnlineDevices:   "📴 No devices are currently online.",
		MsgLLMConcurrencyFull: "LLM concurrency limit reached, please try again later",
		MsgMultiDeviceReply:  "Multi-device reply",
		MsgGroupChatReply:    "Group chat reply",
		MsgStallSuspected:    "⏳ Coding tool output paused, system is attempting recovery, please check back later",
		MsgStallStuck:        "⚠️ Coding tool may be stuck. Consider sending a specific command or terminating the session",
		MsgToolWorking:       "⏳ Coding tool is working, please wait before checking progress",
		MsgWaitingInput:      "⚠️ Session is waiting for user input",
		MsgSessionExited:     "Session exited",
		MsgSessionError:      "⚠️ Session error",
	},
}


// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// NormalizeLang maps common language codes to the keys used in the
// translations table. An empty or unrecognised code falls back to "zh".
func NormalizeLang(lang string) string {
	switch lang {
	case "zh", "zh-CN", "zh-Hans", "zh-TW", "zh-Hant":
		return "zh"
	case "en", "en-US", "en-GB":
		return "en"
	case "":
		return defaultLang
	}
	// Unknown language → fallback
	return defaultLang
}

// T returns the translated string for the given key and language.
// If lang is empty or unknown it falls back to "zh".
// If the key is not found the key itself is returned.
func T(key, lang string) string {
	lang = NormalizeLang(lang)
	if table, ok := translations[lang]; ok {
		if s, ok := table[key]; ok {
			return s
		}
	}
	// Fallback to default language if the requested lang table exists but
	// the key is missing.
	if lang != defaultLang {
		if table, ok := translations[defaultLang]; ok {
			if s, ok := table[key]; ok {
				return s
			}
		}
	}
	return key
}

// Tf is like T but applies fmt.Sprintf formatting with the provided args.
func Tf(key, lang string, args ...interface{}) string {
	return fmt.Sprintf(T(key, lang), args...)
}
