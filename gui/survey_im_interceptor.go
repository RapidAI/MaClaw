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

	rk := surveyRateKey("lansenger", msg.FromUserID)
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
			_ = m.replySurveyText(msg, "操作过快，请稍后再试")
			return true
		}
	} else if !rate.wouldAllow(rk, now) {
		return false
	}

	client, err := m.app.newSurveyHubClient()
	if err != nil {
		if isCmd {
			_ = m.replySurveyText(msg, "问卷服务暂不可用（Hub 未连接），请稍后重试。")
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
		"user_id":   strings.TrimSpace(msg.FromUserID),
		"user_name": strings.TrimSpace(msg.SenderName),
		"chat_type": strings.ToLower(chatType),
		"group_id":  strings.TrimSpace(msg.GroupID),
		"text":      text,
		"is_at_me":  msg.IsAtMe,
		"raw_text":  msg.Text,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := client.IMHandle(ctx, body)
	if err != nil {
		log.Printf("[survey-im] im/handle error: %v", err)
		if isCmd {
			_ = m.replySurveyText(msg, "问卷服务暂时出错，请稍后重试。")
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
	// Session lifecycle hints: only mark when user is actually mid-survey.
	// Avoid marking after /survey help|list|not-found so free-text chat stays local.
	switch {
	case ev == "response_submitted",
		strings.Contains(reply, "已取消"):
		hints.clear(rk)
	case !isCmd:
		// Session answer / control word that Hub accepted.
		hints.mark(rk, now, 30*time.Minute)
	case strings.Contains(reply, "开始填写"),
		strings.Contains(reply, "继续填写"),
		strings.Contains(reply, "回复「修改」"),
		strings.Contains(reply, "答案无效"),
		strings.Contains(reply, "正在填写"):
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
			ev = "response_submitted"
		}
		m.app.emitEvent(EventSurveyUpdated, map[string]any{
			"survey_id": strings.TrimSpace(sid),
			"event":     ev,
		})
		// Belt-and-suspenders: clear free-text hint after submit event.
		if ev == "response_submitted" {
			hints.clear(rk)
		}
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
	// Prefer Fields so multi-byte names are handled correctly.
	for {
		fields := strings.Fields(text)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "@") {
			break
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	}
	for _, b := range msg.MentionedBots {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			continue
		}
		for _, p := range []string{"@" + name + " ", "@" + name, name + " ", name} {
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
