package main

import (
	"context"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
)

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
	if looksLikeSurveyTraffic(strippedText) {
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
	// Group messages already passed mention gate when we get here from onIncomingMessage.
	// For p2p, allow without @.
	text := stripLansengerBotMentions(msg)
	if text == "" {
		text = strings.TrimSpace(msg.Text)
	}
	if !shouldAttemptSurveyIM(m.app.surveyEnabled(), text) {
		return false
	}

	// Design §9: ~2 msg/s per user — claim with throttle text so traffic never reaches LLM.
	if m.surveyRate == nil {
		m.surveyRate = newSurveyUserRateLimit()
	}
	rk := surveyRateKey("lansenger", msg.FromUserID)
	if !m.surveyRate.allow(rk, time.Now()) {
		_ = m.replySurveyText(msg, "操作过快，请稍后再试")
		return true
	}

	client, err := m.app.newSurveyHubClient()
	if err != nil {
		// Hub offline: if it looks like a survey command, claim and apologize.
		if looksLikeSurveyCommand(text) {
			_ = m.replySurveyText(msg, "问卷服务暂不可用（Hub 未连接），请稍后重试。")
			return true
		}
		return false
	}

	body := map[string]any{
		"platform":  "lansenger",
		"user_id":   msg.FromUserID,
		"user_name": msg.SenderName,
		"chat_type": msg.ChatType,
		"group_id":  msg.GroupID,
		"text":      text,
		"is_at_me":  msg.IsAtMe,
		"raw_text":  msg.Text,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := client.IMHandle(ctx, body)
	if err != nil {
		log.Printf("[survey-im] im/handle error: %v", err)
		if looksLikeSurveyCommand(text) {
			_ = m.replySurveyText(msg, "问卷服务暂时出错，请稍后重试。")
			return true
		}
		return false
	}
	handled, _ := out["handled"].(bool)
	if !handled {
		return false
	}
	reply, _ := out["reply_text"].(string)
	if strings.TrimSpace(reply) != "" {
		if err := m.replySurveyText(msg, reply); err != nil {
			log.Printf("[survey-im] SendText failed: %v", err)
		}
	}
	// Notify desktop UI when a response was submitted (design: survey-updated).
	if sid, _ := out["survey_id"].(string); strings.TrimSpace(sid) != "" {
		ev, _ := out["event"].(string)
		if ev == "" {
			ev = "response_submitted"
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
	to := lansengerReplyTarget(msg)
	isGroup := isLansengerGroupMessage(msg)
	text = m.groupReplyText(msg, text)
	return gw.SendText(context.Background(), lansenger.OutgoingText{
		ToUserID: to,
		Text:     text,
		IsGroup:  isGroup,
	})
}

// stripLansengerBotMentions removes leading @Bot tokens and known MentionedBots names.
func stripLansengerBotMentions(msg lansenger.IncomingMessage) string {
	text := strings.TrimSpace(msg.Text)
	// strip leading @tokens
	for {
		if !strings.HasPrefix(text, "@") {
			break
		}
		// find end of mention token
		i := 1
		for i < len(text) {
			r := rune(text[i])
			if unicode.IsSpace(r) || text[i] == '@' {
				break
			}
			// UTF-8 safe walk
			if text[i] >= 0x80 {
				// break on multibyte roughly by space only — use Fields-based approach
				break
			}
			i++
		}
		// better: Fields
		fields := strings.Fields(text)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "@") {
			break
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	}
	// strip bot display names if present as prefix "@Name"
	for _, b := range msg.MentionedBots {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			continue
		}
		for _, p := range []string{"@" + name, name} {
			if strings.HasPrefix(text, p) {
				text = strings.TrimSpace(text[len(p):])
			}
		}
	}
	return strings.TrimSpace(text)
}

func looksLikeSurveyCommand(text string) bool {
	t := strings.TrimSpace(text)
	low := strings.ToLower(t)
	if strings.HasPrefix(low, "/survey") {
		return true
	}
	if strings.HasPrefix(t, "问卷 ") || strings.HasPrefix(t, "调查 ") {
		return true
	}
	// 问卷CODE without space is invalid per design; require space
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
	switch t {
	case "取消", "修改", "上一题", "cancel", "Cancel":
		return true
	}
	// Numeric / choice tokens (1, 1,3, A, yes) — short answers only.
	// Avoid sending arbitrary chat to Hub; runtime still rejects when no session.
	runes := []rune(t)
	if len(runes) > 32 {
		return false
	}
	// pure number or comma/space separated choice tokens
	for _, r := range runes {
		if unicode.IsDigit(r) || r == ',' || r == '，' || r == '、' || unicode.IsSpace(r) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		// allow short CJK free-text answers (rating/text questions)
		if unicode.In(r, unicode.Han) {
			continue
		}
		return false
	}
	return true
}
