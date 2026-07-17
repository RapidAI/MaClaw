package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
)

func TestSurveyHintAction(t *testing.T) {
	cases := []struct {
		ev        string
		isCmd     bool
		wantClear bool
		wantMark  bool
	}{
		{surveyEventResponseSubmitted, false, true, false},
		{surveyEventResponseSubmitted, true, true, false},
		{surveyEventSessionEnded, false, true, false},
		{surveyEventSessionEnded, true, true, false},
		{surveyEventSessionActive, false, false, true},
		{surveyEventSessionActive, true, false, true},
		// Legacy Hub without events: only non-command handled replies mark.
		{"", false, false, true},
		{"", true, false, false},
	}
	for _, c := range cases {
		clear, mark := surveyHintAction(c.ev, c.isCmd)
		if clear != c.wantClear || mark != c.wantMark {
			t.Fatalf("ev=%q isCmd=%v → clear=%v mark=%v, want %v/%v",
				c.ev, c.isCmd, clear, mark, c.wantClear, c.wantMark)
		}
	}
}

func TestSurveyScopedUserID(t *testing.T) {
	if got := surveyScopedUserID("", "u1"); got != "u1" {
		t.Fatalf("p2p scope=%q", got)
	}
	if got := surveyScopedUserID("g1", "u1"); got != "g1:u1" {
		t.Fatalf("group scope=%q", got)
	}
	// Different groups must not share rate/hint keys.
	if surveyScopedUserID("g1", "u1") == surveyScopedUserID("g2", "u1") {
		t.Fatal("groups must have distinct scoped ids")
	}
}

func TestSurveyIMLang(t *testing.T) {
	if got := (&App{CurrentLanguage: "zh-Hans"}).surveyIMLang(); got != "zh" {
		t.Fatalf("zh-Hans → %q", got)
	}
	if got := (&App{CurrentLanguage: "en-US"}).surveyIMLang(); got != "en" {
		t.Fatalf("en-US → %q", got)
	}
	// No CurrentLanguage and no config on disk → zh fallback.
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	if got := (&App{testHomeDir: tmpHome}).surveyIMLang(); got != "zh" {
		t.Fatalf("empty → %q", got)
	}
}

func newSurveyTestManager(t *testing.T, lang string) *lansengerGatewayManager {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	return &lansengerGatewayManager{app: &App{testHomeDir: tmpHome, CurrentLanguage: lang}}
}

func TestSurveyOutgoingTextGroupForcesDecorations(t *testing.T) {
	m := newSurveyTestManager(t, "en")
	msg := lansenger.IncomingMessage{
		FromUserID: "u1", SenderName: "Alice", GroupID: "g1", ChatType: "group",
		MessageID: "mid-1", Text: "1",
	}
	out := m.surveyOutgoingText(msg, "Submitted successfully. Thank you!")
	if !out.IsGroup {
		t.Fatal("group reply must keep IsGroup")
	}
	if !strings.HasPrefix(out.Text, "[Survey]\n") {
		t.Fatalf("group reply missing survey tag: %q", out.Text)
	}
	// Forced @voter even though AutoMentionReply is off in the default config.
	if out.Reminder == nil || len(out.Reminder.UserIDs) != 1 || out.Reminder.UserIDs[0] != "u1" {
		t.Fatalf("reminder=%+v", out.Reminder)
	}
	// Forced native quote of the inbound message.
	if out.RefMsgID != "mid-1" {
		t.Fatalf("refMsgID=%q", out.RefMsgID)
	}
}

func TestSurveyOutgoingTextGroupTagZh(t *testing.T) {
	m := newSurveyTestManager(t, "zh-Hans")
	msg := lansenger.IncomingMessage{
		FromUserID: "u1", GroupID: "g1", ChatType: "group", MessageID: "mid-1", Text: "1",
	}
	out := m.surveyOutgoingText(msg, "提交成功，感谢参与！")
	if !strings.HasPrefix(out.Text, "【问卷】\n") {
		t.Fatalf("zh group reply missing tag: %q", out.Text)
	}
}

func TestSurveyOutgoingTextP2PStaysPlain(t *testing.T) {
	m := newSurveyTestManager(t, "en")
	msg := lansenger.IncomingMessage{FromUserID: "u1", ChatType: "p2p", Text: "1"}
	out := m.surveyOutgoingText(msg, "Submitted successfully. Thank you!")
	if strings.HasPrefix(out.Text, "[Survey]") {
		t.Fatalf("p2p must not carry the group tag: %q", out.Text)
	}
	if out.Reminder != nil || out.RefMsgID != "" {
		t.Fatalf("p2p must not force decorations: %+v ref=%q", out.Reminder, out.RefMsgID)
	}
	if out.IsGroup {
		t.Fatal("p2p must not be marked group")
	}
}

func TestBuildSurveyAnnounceText(t *testing.T) {
	info := surveyAnnounceInfo{
		Title: "午餐调查", ShortCode: "A3F9K2",
		Deadline: "2026-07-20T12:00:00Z", TargetCount: 50, QuestionCount: 3,
	}
	zh := buildSurveyAnnounceText("zh", info)
	for _, needle := range []string{"【问卷】午餐调查", "短码：A3F9K2", "共 3 题", "截止：", "目标回收：50 份", "/survey A3F9K2"} {
		if !strings.Contains(zh, needle) {
			t.Fatalf("zh card missing %q:\n%s", needle, zh)
		}
	}
	en := buildSurveyAnnounceText("en", info)
	for _, needle := range []string{"[Survey] 午餐调查", "Code: A3F9K2", "3 questions", "Deadline:", "Target: 50 responses", "/survey A3F9K2"} {
		if !strings.Contains(en, needle) {
			t.Fatalf("en card missing %q:\n%s", needle, en)
		}
	}
	// Minimal card: no deadline/target/questions lines.
	min := buildSurveyAnnounceText("zh", surveyAnnounceInfo{Title: "T", ShortCode: "X12345"})
	if strings.Contains(min, "截止") || strings.Contains(min, "目标回收") || strings.Contains(min, "共 ") {
		t.Fatalf("minimal card must omit optional lines:\n%s", min)
	}
}
