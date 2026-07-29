package main

import (
	"fmt"

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
		if uic := h.getUnifiedClassifier(); uic != nil {
			uicResult := uic.Classify(intent.MessageContext{Text: userText, UserID: ownerID})
			if uicResult.IsCodingLike() {
				return ""
			}
			// Continuation phrases ("开工"/"继续"/"let's go") should allow session
			// creation — they indicate the user wants to continue a prior coding task.
			if uicResult.Primary == intent.LabelContinuation {
				return ""
			}
			if uicResult.IsNonCodingLike() {
				return "Task intent: semantic classification indicates a non-coding task. Do not create a coding session; use direct tools instead."
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
