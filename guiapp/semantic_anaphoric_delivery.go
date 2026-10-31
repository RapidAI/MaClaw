package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// A follow-up can point at a recipient without naming the channel: "把重庆的
// 天气也发给他", "send it to the same address", or the same thought in any
// other language. The intent classifier reads only that sentence, so it plans
// the default delivery for the artifact (this chat) and the mail tool is never
// rendered. Once this session has delivered through a configured mailbox, a
// follow-up may continue that channel. The decision is semantic, by the
// lightweight model, not by a phrase list: the wording rules differ between
// languages and a list would only cover the ones someone remembered to add.
//
// This method only runs when the structural evidence is already present: a
// trusted managed classification, a fully mapped rule set, and a recent
// mailbox send with an address in this session's history. The model then
// answers one question: is this new artifact going to that earlier recipient,
// or to the current chat / something else. A classifier failure means no
// rewrite; the model's own petition path remains.
//
// The recipient itself stays in conversation history; the model fills it from
// there, and the send still needs its confirmation.

const (
	// Count user turns, not raw entries: one tool-heavy turn can add dozens
	// of tool rows between the send and the follow-up that says "also send it
	// to him". Six turns is far enough for a continuation and short enough that
	// an unrelated old send does not keep re-arming the classifier.
	anaphoricDeliveryUserTurns       = 6
	anaphoricDeliveryClassifyTimeout = 2 * time.Second
	anaphoricDeliveryReason          = "anaphoric mailbox delivery"
)

const anaphoricDeliveryClassifyPrompt = `An earlier turn in this conversation delivered an artifact by email to a recipient. A new message just arrived. Decide where the new artifact of this turn should be delivered. Reply with one JSON object and nothing else: {"deliver":"mailbox"} or {"deliver":"current_chat"}.

mailbox: the message asks to deliver the new result to that earlier email recipient, or to the same address, in any language and any phrasing, including an implicit continuation of that delivery.
current_chat: the message asks for anything else: delivery to the speaker or to this chat, another channel, a status question about an earlier send, a negation or a correction, or a request that is unclear. If you are unsure, reply current_chat.

Do not explain.`

var (
	emailAddressPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// The managed surface renders the trusted adapter name; the legacy
	// catalog and the petition directory use mailbox_send. Both are the same
	// send, and history may record either.
	mailboxSendToolNames = []string{"mailbox_send", "semantic_send_trusted_mailbox"}
)

// mailboxSendAllowed reports this turn's channel policy for the mail tool.
// A Lansenger group chat allows only its explicit tool list, and the mailbox
// is not on it. Adding the label there would plan a capability the group
// filter removes, so the caller skips the rewrite.
func (c *LoopContext) mailboxSendAllowed() bool {
	if c == nil || c.LansengerGroupPermissions == nil {
		return true
	}
	return c.LansengerGroupPermissions.allowsTool("mailbox_send")
}

// semanticAnaphoricMailboxFollowUp appends mailbox_send to a continuation that
// sends the new artifact to the recipient of an earlier mailbox delivery.
// Nil means the caller must leave the classification alone.
func (h *IMMessageHandler) semanticAnaphoricMailboxFollowUp(current *intent.ClassificationResult, sessionKey, userText string, history []agent.ConversationEntry) *intent.ClassificationResult {
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
	if strings.TrimSpace(userText) == "" {
		return nil
	}
	// A pending mailbox draft already owns the next email send for this
	// session. The turn should resolve that draft (confirm or cancel), not arm
	// a second one; a new draft would replace the pending question.
	if h.peekMailboxDraftID(sessionKey) != "" {
		return nil
	}
	if !historyCarriesMailboxDelivery(history, anaphoricDeliveryUserTurns) {
		return nil
	}
	delivery, err := h.classifyAnaphoricDelivery(sessionKey, userText)
	if err != nil {
		log.Printf("[anaphoric-delivery] verdict unavailable user=%q err=%v", sessionKey, err)
		return nil
	}
	if delivery != "mailbox" {
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

func (h *IMMessageHandler) classifyAnaphoricDelivery(sessionKey, text string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("handler not initialized")
	}
	if h.anaphoricDeliveryClassifier != nil {
		raw, err := h.anaphoricDeliveryClassifier(text)
		if err != nil {
			return "", err
		}
		return parseAnaphoricDelivery(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), anaphoricDeliveryClassifyTimeout)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "anaphoric-delivery", OwnerID: sessionKey})
	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      anaphoricDeliveryClassifyPrompt,
		UserMessage:       truncateRunes(strings.TrimSpace(text), 500),
		TimeoutSec:        2,
		Tag:               "anaphoric-delivery",
		Role:              maclawLLMRoleForeground,
		PreferLightweight: true,
		NoThinking:        true,
		MaxOutputTokens:   30,
	})
	if err != nil {
		return "", err
	}
	if result == nil || strings.TrimSpace(result.Text) == "" {
		return "", fmt.Errorf("empty anaphoric delivery classification")
	}
	return parseAnaphoricDelivery(result.Text)
}

func parseAnaphoricDelivery(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty anaphoric delivery")
	}
	var payload struct {
		Deliver string `json:"deliver"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start >= 0 && end > start {
			_ = json.Unmarshal([]byte(text[start:end+1]), &payload)
		}
	}
	deliver := strings.ToLower(strings.TrimSpace(payload.Deliver))
	if deliver == "" {
		// A non-reasoning model sometimes answers with the bare word, with or
		// without a trailing period.
		deliver = strings.Trim(strings.ToLower(text), " \t\r\n.。!！")
	}
	switch deliver {
	case "mailbox":
		return "mailbox", nil
	case "current_chat", "chat", "none", "current":
		return "current_chat", nil
	default:
		return "", fmt.Errorf("unrecognized anaphoric delivery %q", truncateRunes(text, 80))
	}
}

// historyCarriesMailboxDelivery reports a recent mailbox send and an address
// in the same window. The address is what an anaphoric recipient can resolve
// to; a mailbox call without one cannot name a recipient for this turn.
func historyCarriesMailboxDelivery(history []agent.ConversationEntry, userTurns int) bool {
	if len(history) == 0 {
		return false
	}
	sent := false
	addressed := false
	seenUsers := 0
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		role := strings.ToLower(strings.TrimSpace(entry.Role))
		if role == "user" && !entry.HostContinuation {
			seenUsers++
			if seenUsers > userTurns {
				break
			}
		}
		if conversationEntrySentMail(entry) {
			sent = true
		}
		// Only the person's own words and the model's calls can name the
		// recipient. A web result can contain an address that is not one.
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
