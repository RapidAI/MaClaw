package main

import (
	"context"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
)

// Mirror of hub/internal/survey IM event codes (hub internal packages are not
// importable from gui). Drive session hints off these, never off reply text.
const (
	surveyEventResponseSubmitted = "response_submitted"
	surveyEventSessionEnded      = "session_ended"
	surveyEventSessionActive     = "session_active"
)

// surveyIMLang resolves the gateway machine's interface language for survey
// IM texts (zh/en today; falls back to zh via i18n.NormalizeLang).
func (a *App) surveyIMLang() string {
	lang := a.CurrentLanguage
	if strings.TrimSpace(lang) == "" {
		if cfg, err := a.LoadConfig(); err == nil {
			lang = cfg.Language
		}
	}
	return i18n.NormalizeLang(lang)
}

// surveyScopedUserID scopes a user id by group so rate limits / session hints
// match the Hub's group-scoped session key for group chats.
func surveyScopedUserID(groupID, userID string) string {
	userID = strings.TrimSpace(userID)
	if gid := strings.TrimSpace(groupID); gid != "" {
		return gid + ":" + userID
	}
	return userID
}

// surveyHintAction maps a Hub IM event code to a session-hint mutation.
// Legacy Hubs send no event: non-command handled replies still mark (old behavior).
func surveyHintAction(ev string, isCmd bool) (clear, mark bool) {
	switch ev {
	case surveyEventResponseSubmitted, surveyEventSessionEnded:
		return true, false
	case surveyEventSessionActive:
		return false, true
	default:
		return false, !isCmd
	}
}

// surveyEnabled reports whether IM survey intercept is on (default true).
func (a *App) surveyEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	if cfg.SurveyEnabled == nil {
		return true
	}
	return *cfg.SurveyEnabled
}

// shouldAttemptSurveyIM is the pure pre-claim decision (kill-switch + prefilter).
// When false, the gateway must not claim the message for survey (leave to agent/Hub LLM).
func shouldAttemptSurveyIM(enabled bool, strippedText string) bool {
	if !enabled {
		return false
	}
	if looksLikeSurveyCommand(strippedText) {
		return true
	}
	return couldBeSurveySessionReply(strippedText)
}

// tryHandleSurveyMessage intercepts Lansenger messages for survey Q&A.
// Must run after mention gate and before passthrough / agent / Hub forward.
// Returns true if handled (caller must not continue normal agent routing).
func (m *lansengerGatewayManager) tryHandleSurveyMessage(msg lansenger.IncomingMessage) bool {
	if m == nil || m.app == nil {
		return false
	}
	text := stripLansengerBotMentions(msg)
	if text == "" {
		text = strings.TrimSpace(msg.Text)
	}
	if !shouldAttemptSurveyIM(m.app.surveyEnabled(), text) {
		return false
	}

	// Init under gateway mutex so concurrent first messages do not race.
	m.mu.Lock()
	if m.surveyRate == nil {
		m.surveyRate = newSurveyUserRateLimit()
	}
	if m.surveyHints == nil {
		m.surveyHints = newSurveySessionHint()
	}
	rate := m.surveyRate
	hints := m.surveyHints
	m.mu.Unlock()

	lang := m.app.surveyIMLang()
	userID := strings.TrimSpace(msg.FromUserID)
	if userID == "" {
		// Hub requires user_id; avoid a confusing 400→"服务出错" path for commands.
		if looksLikeSurveyCommand(text) {
			_ = m.replySurveyText(msg, i18n.T(i18n.MsgSurveyIMUnknownSender, lang))
			return true
		}
		return false
	}
	// Group-scope the rate/hint key so one user's sessions in different groups
	// (matching the Hub group-scoped session key) do not share limits or hints.
	rk := surveyRateKey("lansenger", surveyScopedUserID(msg.GroupID, userID))
	now := time.Now()
	isCmd := looksLikeSurveyCommand(text)

	// Free-text (not pure choice/control) only probes Hub when we believe a session is active.
	// Avoids "你好"/"嗯" hammering im/handle when no one is filling a survey.
	if !isCmd && isFreeTextSurveyCandidate(text) && !hints.active(rk, now) {
		return false
	}

	// Design §9: ~2 msg/s. Only *claim* the message on throttle for explicit commands.
	if isCmd {
		if !rate.allow(rk, now) {
			_ = m.replySurveyText(msg, i18n.T(i18n.MsgSurveyIMRateLimited, lang))
			return true
		}
	} else if !rate.wouldAllow(rk, now) {
		return false
	}

	client, err := m.app.newSurveyHubClient()
	if err != nil {
		if isCmd {
			_ = m.replySurveyText(msg, i18n.T(i18n.MsgSurveyIMHubUnavailable, lang))
			return true
		}
		return false
	}

	chatType := strings.TrimSpace(msg.ChatType)
	if chatType == "" && strings.TrimSpace(msg.GroupID) != "" {
		chatType = "group"
	}

	body := map[string]any{
		"platform":  "lansenger",
		"user_id":   userID,
		"user_name": strings.TrimSpace(msg.SenderName),
		"chat_type": strings.ToLower(chatType),
		"group_id":  strings.TrimSpace(msg.GroupID),
		"text":      text,
		"is_at_me":  msg.IsAtMe,
		"raw_text":  msg.Text,
		"lang":      lang,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := client.IMHandle(ctx, body)
	if err != nil {
		log.Printf("[survey-im] im/handle error: %v", err)
		if isCmd {
			_ = m.replySurveyText(msg, i18n.T(i18n.MsgSurveyIMServiceError, lang))
			return true
		}
		return false
	}
	handled, _ := out["handled"].(bool)
	if !handled {
		return false
	}
	if !isCmd {
		rate.record(rk, now)
	}

	ev, _ := out["event"].(string)
	reply, _ := out["reply_text"].(string)
	// Session lifecycle hints follow Hub event codes (never reply text, which is
	// localized). Legacy Hubs without events keep the old probing behavior.
	if clear, mark := surveyHintAction(ev, isCmd); clear {
		hints.clear(rk)
	} else if mark {
		hints.mark(rk, now, 30*time.Minute)
	}

	if strings.TrimSpace(reply) != "" {
		if err := m.replySurveyText(msg, reply); err != nil {
			log.Printf("[survey-im] SendText failed: %v", err)
		}
	}
	// Only emit when Hub set survey_id (currently response submit). Do not invent
	// event type — empty means "response_submitted" for UI refresh compatibility.
	if sid, _ := out["survey_id"].(string); strings.TrimSpace(sid) != "" {
		if strings.TrimSpace(ev) == "" {
			ev = surveyEventResponseSubmitted
		}
		m.app.emitEvent(EventSurveyUpdated, map[string]any{
			"survey_id": strings.TrimSpace(sid),
			"event":     ev,
		})
	}
	return true
}

func (m *lansengerGatewayManager) replySurveyText(msg lansenger.IncomingMessage, text string) error {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return nil
	}
	return gw.SendText(context.Background(), m.surveyOutgoingText(msg, text))
}

// surveyOutgoingText builds the decorated survey reply. In groups the bot is
// always visible: force @voter + quote of the inbound message and prepend a
// survey identity tag, regardless of the agent-reply decoration toggles.
func (m *lansengerGatewayManager) surveyOutgoingText(msg lansenger.IncomingMessage, text string) lansenger.OutgoingText {
	opts := m.currentGroupOpts()
	if isLansengerGroupMessage(msg) {
		opts.AutoMentionReply = true
		opts.AutoQuoteReply = true
		text = i18n.T(i18n.MsgSurveyGroupTag, m.app.surveyIMLang()) + "\n" + text
	}
	// Same decoration path as agent replies: optional native @ / refMsgId / text quote.
	return buildLansengerOutgoingText(msg, text, opts)
}

// stripLansengerBotMentions is a thin gui wrapper around the shared corelib
// helper so survey / gateway / watch call sites stay short.
func stripLansengerBotMentions(msg lansenger.IncomingMessage) string {
	return lansenger.StripBotMentions(msg)
}

func looksLikeSurveyCommand(text string) bool {
	t := strings.TrimSpace(text)
	low := strings.ToLower(t)
	// Require end or whitespace after "/survey" so "/surveys" is not claimed.
	if strings.HasPrefix(low, "/survey") {
		if len(t) == 7 {
			return true
		}
		if len(t) > 7 {
			c := t[7]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				return true
			}
		}
		return false
	}
	if strings.HasPrefix(t, "问卷 ") || strings.HasPrefix(t, "调查 ") {
		return true
	}
	// fullwidth space after keyword
	if strings.HasPrefix(t, "问卷\u3000") || strings.HasPrefix(t, "调查\u3000") {
		return true
	}
	return false
}

func looksLikeSurveyTraffic(text string) bool {
	return looksLikeSurveyCommand(text)
}

func couldBeSurveySessionReply(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if isSurveyControlWord(t) {
		return true
	}
	if isStrictChoiceToken(t) {
		return true
	}
	// Free-text answers (text questions) — gated by session hint at call site.
	runes := []rune(t)
	if len(runes) == 0 || len(runes) > 2000 {
		return false
	}
	return isFreeTextSurveyCandidate(t)
}

func isSurveyControlWord(t string) bool {
	t = strings.TrimSpace(t)
	switch t {
	case "取消", "修改", "上一题":
		return true
	}
	switch strings.ToLower(t) {
	case "cancel", "prev", "back", "modify":
		return true
	default:
		return false
	}
}

// isStrictChoiceToken is option indexes / multi "1,2" / ratings — safe to probe without session hint.
// Short latin words ("hi", "ok") are NOT strict: they would hammer Hub without a session.
func isStrictChoiceToken(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	runes := []rune(t)
	if len(runes) > 32 {
		return false
	}
	hasDigit := false
	for _, r := range runes {
		if unicode.IsDigit(r) {
			hasDigit = true
			continue
		}
		if r == ',' || r == '，' || r == '、' || unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return hasDigit
}

// isFreeTextSurveyCandidate is non-control, non-pure-choice content (e.g. CJK text answers).
func isFreeTextSurveyCandidate(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" || isSurveyControlWord(t) || isStrictChoiceToken(t) {
		return false
	}
	return true
}
