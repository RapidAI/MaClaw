package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// checkSessionTaskGuard returns a non-empty hint string when the current
// user message should NOT create a coding session. Returns "" only for
// explicit coding tasks.
func (h *IMMessageHandler) checkSessionTaskGuard() string {
	userText, ownerID := h.currentRuntimeTaskTextOrLegacy()
	result := h.classifyTaskIntentForSessionGuard(userText)

	if result.Intent == intentCoding {
		return ""
	}

	if result.Intent == intentAmbiguous || result.Intent == intentUnknown {
		if gic := h.getGateIntentClassifier(); gic != nil {
			gResult := gic.Classify(userText, ownerID)
			switch gResult.Intent {
			case GateIntentNewProject, GateIntentBugFix, GateIntentMaintenance:
				return ""
			case GateIntentContinuation:
				return ""
			case GateIntentNonCoding:
				return nonCodingSessionHint(gResult)
			}
		}

		if h.app != nil && h.getAppToolRouter() != nil {
			if ic := h.getAppToolRouter().IntentClassifier(); ic != nil {
				icResult := ic.Classify(userText)
				switch icResult.Intent {
				case tool.IntentCoding:
					return ""
				case tool.IntentQuery:
					return "Task intent: semantic classification indicates a knowledge question, not an action. Do not create a coding session; answer the user directly."
				case tool.IntentSSH:
					result.Intent = intentSSH
				case tool.IntentContent:
					result.Intent = intentNonCoding
				}
			}
		}
	}

	return formatSemanticSessionGuardHint(result)
}

func formatSemanticSessionGuardHint(result taskIntentResult) string {
	switch result.Intent {
	case intentSSH:
		return fmt.Sprintf(`Task intent: semantic classification indicates an SSH/server operation (%s). Do not create a coding session.
Use the ssh tool instead:
- ssh(action="connect", ...): connect to the server
- ssh(action="exec", session_id="...", command="..."): run a short command
- ssh(action="exec_background", session_id="...", command="..."): run a long command, deployment, install, or build
- ssh(action="upload"/"download", ...): transfer files
Coding tasks should be routed through CodingSubAgent; do not create external coding sessions.`, formatIntentEvidence(result))
	case intentNonCoding:
		return fmt.Sprintf(`Task intent: semantic classification indicates this is not a coding task (%s). Do not create a coding session.
Use direct tools instead:
- bash: run local commands or scripts
- craft_tool: generate and execute a task-specific script
- read_file / write_file / edit_file: read, write, or edit local files
- send_file: send a file to the user
- open: open a file or URL
- memory: save or retrieve information
Coding tasks should be routed through CodingSubAgent; do not create external coding sessions.`, formatIntentEvidence(result))
	case intentUnknown, intentAmbiguous:
		return fmt.Sprintf(`Task intent is still ambiguous (%s). Do not create a coding session yet.
Clarify the goal first:
- If the user needs project code changes, bug fixes, or feature implementation, route it through CodingSubAgent after clarification.
- If the user needs server login, logs, service restart, upload, or download, use the ssh tool after clarification.
When semantic intent is unavailable or ambiguous, do not open coding tools automatically.`, formatIntentEvidence(result))
	default:
		return ""
	}
}

// conversationHasCodingContext checks whether the recent conversation history
// contains evidence of a coding task.
func (h *IMMessageHandler) conversationHasCodingContext() bool {
	if uic := h.getUnifiedClassifier(); uic != nil {
		return h.conversationHasCodingContextForOwnerUIC(uic, h.currentRuntimePolicyOwnerID())
	}
	return false
}

func (h *IMMessageHandler) conversationHasCodingContextUIC(uic *intent.UnifiedIntentClassifier) bool {
	return h.conversationHasCodingContextForOwnerUIC(uic, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) conversationHasCodingContextForOwner(ownerID string) bool {
	if uic := h.getUnifiedClassifier(); uic != nil {
		return h.conversationHasCodingContextForOwnerUIC(uic, ownerID)
	}
	return false
}

func (h *IMMessageHandler) conversationHasCodingContextForOwnerUIC(uic *intent.UnifiedIntentClassifier, ownerID string) bool {
	if h.memory == nil {
		return false
	}
	userID := strings.TrimSpace(ownerID)
	if userID == "" {
		return false
	}
	currentTaskText := h.runtimeTaskTextForOwner(userID)
	entries := h.memory.Load(userID)
	if len(entries) == 0 {
		return false
	}
	for i := len(entries) - 1; i >= 0; i-- {
		text, ok := entries[i].Content.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		if entries[i].Role != "user" {
			continue
		}
		if strings.TrimSpace(text) == strings.TrimSpace(currentTaskText) {
			continue
		}
		// Use embedding-only classification for history entries to avoid
		// triggering a full tree-channel LLM call per entry.
		// The full fusion pipeline is expensive for this check — we only
		// need a rough "is this coding-like?" signal, not precise workflow
		// type determination. Embedding alone is <100ms and sufficient.
		result := uic.ClassifyEmbeddingOnly(intent.MessageContext{Text: text})
		return result.IsCodingLike()
	}
	return false
}

// nonCodingSessionHint returns a user-facing hint message when the
// GateIntentClassifier determines the user's request is a non-coding task.
func nonCodingSessionHint(result GateIntentResult) string {
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = "non-coding task detected"
	}
	return fmt.Sprintf(`⚠️ 任务类型检测：当前请求看起来不是编程任务（%s），不需要创建编程会话。
请直接使用以下工具完成任务：
- bash：执行命令行操作（如 curl 下载、脚本执行）
- craft_tool：自动生成并执行脚本（适合数据处理、API 调用、文件转换）
- read_file / write_file / edit_file：读写和局部编辑本地文件
- send_file：将文件发送给用户
- open：打开文件或网址
- memory：保存/检索信息
如果确实需要编程任务，请改走内部 CodingSubAgent，不要创建外部编程会话。`, reason)
}
