package mailbox

import "strings"

// ExternalMutationKnown reports a mailbox_send or mailbox_delete result whose
// outcome is already known. Empty text stays unknown: the tool may have been
// interrupted after a server had already accepted the change. A sentence that
// says the outcome is still unknown stays unknown even when a refusal prefix
// is also present.
func ExternalMutationKnown(result string) bool {
	text := strings.TrimSpace(result)
	if text == "" || externalMutationOutcomeUnknown(text) {
		return false
	}
	// The mailbox executor marks a definite non-delivery with this prefix and
	// an unknown server outcome with [system unknown]. A host refusal before
	// dial or IMAP is a known non-mutation.
	if strings.Contains(text, "[system rejected]") {
		return true
	}
	for _, known := range externalMutationKnownFragments {
		if strings.Contains(text, known) {
			return true
		}
	}
	return false
}

// ExternalMutationUncertain reports a mailbox_send or mailbox_delete result
// that may already have reached the server. Empty text stays in this class:
// the tool may have been interrupted after the server accepted the change.
// A confirmation card has not dialed, so trimming it must not stick the
// review latch. Check those markers before the uncertain sentences.
func ExternalMutationUncertain(result string) bool {
	text := strings.TrimSpace(result)
	if text == "" {
		return true
	}
	if ExternalMutationConfirmation(text) {
		return false
	}
	if strings.Contains(text, MsgUncertain) || strings.Contains(text, MsgDeleteUncertain) {
		return true
	}
	return strings.Contains(text, "[system unknown]")
}

// ExternalMutationConfirmation reports a mailbox_send or mailbox_delete card
// that is still waiting for the user. SMTP or IMAP has not been attempted.
// Dropping the card must not be treated as a delivery that may have landed.
func ExternalMutationConfirmation(result string) bool {
	text := strings.TrimSpace(result)
	if text == "" {
		return false
	}
	return strings.Contains(text, "awaiting_receipt") ||
		strings.Contains(text, "mailbox_send_awaiting_confirmation") ||
		strings.Contains(text, "mailbox_delete_awaiting_confirmation") ||
		strings.Contains(text, "__ASK_USER__")
}

func externalMutationOutcomeUnknown(text string) bool {
	for _, sentence := range []string{
		MsgUncertain,
		MsgDeleteUncertain,
		MsgExternalReview,
		MsgDeleteExternalReview,
	} {
		if strings.Contains(text, sentence) {
			return true
		}
	}
	// An open receipt is not a finished server outcome. The confirmation card
	// and these reason codes must keep the provisional latch.
	return ExternalMutationConfirmation(text)
}

// Fragments are full product sentences or argument refusals. Shorter pieces
// such as "不会再次发送" also appear inside the uncertain sentences and must
// not be listed here.
var externalMutationKnownFragments = []string{
	MsgNotConfigured,
	MsgNoDefault,
	"没有名为「",
	"」已停用",
	"没有配置收件",
	"没有配置发件",
	MsgNoSecret,
	MsgKeyring,
	MsgNoHost,
	MsgAuth,
	MsgTLS,
	MsgConnect,
	MsgRejected,
	MsgCancelled,
	MsgDeleteCancelled,
	MsgExpired,
	MsgDeleteExpired,
	MsgPending,
	MsgDeletePending,
	MsgSendBudget,
	MsgDeleteBudget,
	MsgQueryBudget,
	MsgOAuthReauth,
	MsgFolder,
	MsgEmpty,
	MsgNotFound,
	"邮件已发送",
	MsgDeleteMoved,
	MsgDeletePurged,
	MsgDeleteNone,
	MsgDeleteAborted,
	MsgDeleteUIDChanged,
	"不能在参数里提供",
	"不支持的发信操作",
	"不支持的删除操作",
	"不支持的邮箱操作",
	"需要主题和正文",
	"收件人需要",
	"只有发送新邮件可以带附件",
	"附件必须位于任务工作区",
	"邮件编号无效",
	"需要指定要删除的邮件",
	"一次最多指定",
	"范围只能是",
}
