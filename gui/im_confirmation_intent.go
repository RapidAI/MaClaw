package main

import "strings"

type confirmationIntent string

const (
	confirmationIntentUnknown confirmationIntent = ""
	confirmationIntentConfirm confirmationIntent = "confirm"
	confirmationIntentCancel  confirmationIntent = "cancel"
	confirmationIntentModify  confirmationIntent = "modify"
)

func (i confirmationIntent) String() string {
	return string(i)
}

func classifyWorkflowConfirmationReply(text string) confirmationIntent {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	trimmed = strings.Trim(trimmed, " \t\r\n`\"''.,:;!?()[]{}")
	switch trimmed {
	case "confirm", "yes", "y", "ok", "start", "go", "continue", "approve", "确认", "是", "好", "好的", "开始", "继续", "同意":
		return confirmationIntentConfirm
	case "cancel", "no", "n", "stop", "abort", "reject", "取消", "否", "不", "停止", "先不", "不同意":
		return confirmationIntentCancel
	case "modify":
		return confirmationIntentModify
	default:
		return confirmationIntentModify
	}
}

func normalizeConfirmationIntent(text string) confirmationIntent {
	intent := strings.ToLower(strings.TrimSpace(text))
	intent = strings.Trim(intent, " \t\r\n`\"'.,:;!?()[]{}")
	switch intent {
	case confirmationIntentConfirm.String():
		return confirmationIntentConfirm
	case confirmationIntentCancel.String():
		return confirmationIntentCancel
	case confirmationIntentModify.String():
		return confirmationIntentModify
	default:
		return confirmationIntentUnknown
	}
}

func classifyConfirmationIntentFallback(text string) confirmationIntent {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	trimmed = strings.Trim(trimmed, " \t\r\n`\"'.,:;!?()[]{}")
	if trimmed == "" {
		return confirmationIntentUnknown
	}
	switch trimmed {
	case "confirm", "yes", "y", "ok", "okay", "start", "go", "go ahead", "continue", "proceed", "approve", "approved", "do it", "run it":
		return confirmationIntentConfirm
	case "cancel", "no", "n", "stop", "abort", "reject", "rejected":
		return confirmationIntentCancel
	case "modify", "change", "revise", "edit":
		return confirmationIntentModify
	}
	if strings.HasPrefix(trimmed, "go ahead") || strings.HasPrefix(trimmed, "please proceed") || strings.HasPrefix(trimmed, "let's go") {
		return confirmationIntentConfirm
	}
	return confirmationIntentUnknown
}
