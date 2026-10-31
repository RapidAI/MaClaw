package guiapp

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// A follow-up can point at a recipient without naming the channel: "把重庆的
// 天气也发给他", "send it to the same address". The classifier reads only that
// sentence, so it plans the default delivery for the artifact (this chat) and
// the mail tool is never rendered. Once this session has delivered through a
// configured mailbox, the deictic send continues that channel. The
// classification then carries mailbox_send, which is what puts the tool on the
// surface. The recipient itself stays in conversation history; the model fills
// it from there, and the send still needs its confirmation.
//
// This is deliberately narrow: an explicit channel in the sentence ("发邮件",
// "发微信", "email") is left to the classifier, and history must show both a
// mailbox send and an address. A deictic send with no such history is not an
// email.

const (
	anaphoricDeliveryHistoryWindow = 24
	anaphoricDeliveryReason        = "anaphoric mailbox delivery"
)

var (
	emailAddressPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// A send verb aimed at a person. The object is the artifact this session
	// just produced, so no channel noun is present. The cues keep the recipient
	// or the artifact: a bare "也发" also matches "也发现" and "也发生".
	anaphoricDeliveryCues = []string{
		"也发给他", "也发给她", "也发给他们", "也发给我", "也发一份", "也发过去",
		"再发一份", "再发一次", "再发给他", "再发给她", "再发给他们", "再发给我", "再发过去",
		"发给他", "发给她", "发给他们", "发给我", "发一份", "发过去",
		"给他发", "给她发", "给他们发", "给我发",
		"同样发", "照样发", "照这样发",
		"also send", "send it to him", "send it to her", "send it to them",
		"send it to me", "send the same", "same address", "same recipient",
	}
	// The sentence already names where it goes. The classifier can read that,
	// and this rewrite must not overwrite an explicit choice.
	explicitDeliveryChannelCues = []string{
		"邮箱", "邮件", "email", "e-mail", "mailbox",
		"微信", "wechat", "qq", "会话", "聊天", "群里", "群聊", "chat",
		"短信", "消息", "私信", "钉钉", "飞书", "telegram", "whatsapp",
	}
	// A negated send is the opposite request. "别发给他" contains the same
	// deictic phrase as "也发给他", and the plan must not email because of it.
	// Each entry keeps the object: a bare "别发" also matches "分别发给".
	anaphoricDeliveryNegations = []string{
		"别发给他", "别发给她", "别发给他们", "别发给我", "别发过去",
		"别给他发", "别给她发", "别给他们发", "别给我发",
		"不要发给他", "不要发给她", "不要发给他们", "不要发给我", "不要发过去",
		"不要给他发", "不要给她发", "不要给他们发", "不要给我发",
		"不用发给他", "不用发给她", "不用发给他们", "不用发给我", "不用发过去",
		"不用给他发", "不用给她发", "不用给他们发", "不用给我发",
		"不必发给他", "不必发给她", "不必发给他们", "不必发给我",
		"不必给他发", "不必给她发",
		"别再发给他", "不要再发给他", "不用再发给他", "不必再发给他",
		"先别发", "暂时别发", "先别给他发", "先别给她发", "先别给我发",
		"don't send", "do not send", "no need to send", "not to send",
	}
	// A status question about a send that already happened is not a new
	// delivery: "发给他了吗", "did you send it to him".
	anaphoricDeliveryStatusCues = []string{
		"发了吗", "发了没", "发给他了吗", "发给她了吗", "发给他们了吗",
		"发过去了吗", "发出去了吗", "发出去没", "有没有发", "是否发",
		"给他发了吗", "给她发了吗", "给他们发了吗", "给我发了吗",
		"did you send", "have you sent", "was it sent", "has it been sent",
		"did it get sent",
	}
	// The managed surface renders the trusted adapter name; the legacy
	// catalog and the petition directory use mailbox_send. Both are the same
	// send, and history may record either.
	mailboxSendToolNames = []string{"mailbox_send", "semantic_send_trusted_mailbox"}
)

// semanticAnaphoricMailboxFollowUp appends mailbox_send to a continuation that
// sends the new artifact to the recipient of an earlier mailbox delivery.
// Nil means the caller must leave the classification alone.
func semanticAnaphoricMailboxFollowUp(current *intent.ClassificationResult, userText string, history []agent.ConversationEntry) *intent.ClassificationResult {
	// A degraded or low-confidence verdict is not a managed plan at all; the
	// label would grant nothing, and the model's petition path remains. Do not
	// manufacture authority from a classification the host did not trust.
	if current == nil || current.Degraded || current.Confidence < imSemanticMinimumConfidence {
		return nil
	}
	// A label without a capability rule makes the whole managed plan fail
	// closed. Adding mailbox_send here must not turn a legacy turn into a
	// rejected one, so the rewrite only touches fully mapped classifications.
	if _, unmapped := imSemanticIntentCoverage(*current); unmapped != "" {
		return nil
	}
	// A workflow phase or a control-plane failure already owns this turn's
	// plan. A delivery label appended from history would fight that plan, so
	// leave those classifications alone.
	if current.WorkflowType != "" || current.ControlPlaneFailure {
		return nil
	}
	if current.HasLabel(intent.LabelMailboxSend) || current.Primary.IsNonCapabilityLabel() {
		return nil
	}
	// Goal and session-round continuations replay the original task sentence
	// inside a host prompt. The person did not type it this turn, so adding a
	// send here would re-run the delivery on every continuation round.
	if isGoalSystemContinuation(userText) || isSessionRoundContinuation(userText) {
		return nil
	}
	if !anaphoricRecipientDeliveryText(userText) {
		return nil
	}
	if !historyCarriesMailboxDelivery(history, anaphoricDeliveryHistoryWindow) {
		return nil
	}
	out := *current
	// Copy the label slice: appending into a shared backing array would leak
	// this label into another classification that aliases it.
	out.Secondary = append([]intent.IntentLabel(nil), current.Secondary...)
	appendIntentSecondary(&out, intent.LabelMailboxSend)
	if out.Reason == "" {
		out.Reason = anaphoricDeliveryReason
	} else if !strings.Contains(out.Reason, anaphoricDeliveryReason) {
		out.Reason = strings.TrimSpace(out.Reason + "; " + anaphoricDeliveryReason)
	}
	return &out
}

func anaphoricRecipientDeliveryText(text string) bool {
	raw := strings.ToLower(strings.TrimSpace(text))
	if raw == "" {
		return false
	}
	// Chinese has no word boundary, so "别发给他" also sits inside
	// "分别发给他", which is a send. Drop the benign 别-compounds before the
	// negation scan; the positive scan still sees the deictic send.
	negationScan := raw
	for _, benign := range []string{"分别", "特别", "区别", "类别"} {
		negationScan = strings.ReplaceAll(negationScan, benign, "")
	}
	if containsAnyMarker(negationScan, anaphoricDeliveryNegations) {
		return false
	}
	if containsAnyMarker(raw, anaphoricDeliveryStatusCues) {
		return false
	}
	if containsAnyMarker(raw, explicitDeliveryChannelCues) {
		return false
	}
	return containsAnyMarker(raw, anaphoricDeliveryCues)
}

// historyCarriesMailboxDelivery reports a recent mailbox send and an address
// in the same window. The address is what "他" can resolve to; a mailbox call
// without one cannot name a recipient for this turn.
func historyCarriesMailboxDelivery(history []agent.ConversationEntry, window int) bool {
	if len(history) == 0 {
		return false
	}
	start := len(history) - window
	if window <= 0 || start < 0 {
		start = 0
	}
	sent := false
	addressed := false
	for i := len(history) - 1; i >= start; i-- {
		entry := history[i]
		if conversationEntrySentMail(entry) {
			sent = true
		}
		// Only the person's own words and the model's calls can name the
		// recipient. A web result can contain an address that is not one.
		role := strings.ToLower(strings.TrimSpace(entry.Role))
		if !addressed && (role == "user" || role == "assistant") && emailAddressPattern.MatchString(conversationEntrySearchText(entry)) {
			addressed = true
		}
		if sent && addressed {
			return true
		}
	}
	return false
}

func conversationEntrySentMail(entry agent.ConversationEntry) bool {
	for _, name := range mailboxSendToolNames {
		if conversationEntryNamesTool(entry, name) {
			return true
		}
	}
	return false
}

func conversationEntryNamesTool(entry agent.ConversationEntry, name string) bool {
	if strings.EqualFold(strings.TrimSpace(entry.ToolName), name) {
		return true
	}
	for _, called := range conversationToolCallNames(entry.ToolCalls) {
		if strings.EqualFold(called, name) {
			return true
		}
	}
	if m, ok := entry.Content.(map[string]interface{}); ok {
		if strings.EqualFold(toolNameFromCallMap(m), name) {
			return true
		}
	}
	return false
}

// conversationToolCallNames reads the shapes a persisted history entry can
// hold: typed calls from a live turn, or decoded maps after a reload.
func conversationToolCallNames(raw interface{}) []string {
	var names []string
	add := func(name string) {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	switch calls := raw.(type) {
	case []llm.ToolCall:
		for _, call := range calls {
			add(call.Function.Name)
		}
	case []interface{}:
		for _, item := range calls {
			switch call := item.(type) {
			case llm.ToolCall:
				add(call.Function.Name)
			case map[string]interface{}:
				add(toolNameFromCallMap(call))
			case map[string]string:
				add(call["name"])
			}
		}
	case []map[string]interface{}:
		for _, call := range calls {
			add(toolNameFromCallMap(call))
		}
	case []map[string]string:
		for _, call := range calls {
			add(call["name"])
		}
	}
	return names
}

func conversationEntrySearchText(entry agent.ConversationEntry) string {
	var b strings.Builder
	b.WriteString(conversationEntryText(entry.Content))
	if entry.ToolCalls != nil {
		if data, err := json.Marshal(entry.ToolCalls); err == nil && len(data) > 0 {
			b.WriteByte('\n')
			b.Write(data)
		}
	}
	return b.String()
}
