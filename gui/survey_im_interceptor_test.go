package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
)

func TestLansengerProfileBotDoesNotUseUnscopedSurveyHubProtocol(t *testing.T) {
	legacy := newLansengerGatewayManager(nil)
	if !legacy.supportsSurveyInterception() {
		t.Fatal("legacy singleton should retain survey support")
	}

	profile := newLansengerGatewayManagerForProfile(nil, corelib.LansengerBotProfile{ID: "support"})
	if profile.supportsSurveyInterception() {
		t.Fatal("profile bot must not use the unscoped survey Hub protocol")
	}
	msg := lansenger.IncomingMessage{ChatType: "group", GroupID: "g1", FromUserID: "u1", Text: "/survey abc"}
	if profile.surveyCandidateBypassesMention(msg) {
		t.Fatal("profile bot must not bypass @mention for survey")
	}
	if profile.tryHandleSurveyMessage(msg) {
		t.Fatal("profile bot must not claim survey traffic")
	}
}

func TestStripLansengerBotMentions(t *testing.T) {
	msg := lansenger.IncomingMessage{
		Text: "@Bot /survey A3F9K2",
		MentionedBots: []lansenger.MentionedBot{
			{ID: "b1", Name: "Bot"},
		},
	}
	got := stripLansengerBotMentions(msg)
	if got != "/survey A3F9K2" {
		t.Fatalf("got %q", got)
	}
}

func TestLooksLikeSurveyCommand(t *testing.T) {
	if !looksLikeSurveyCommand("/survey A3F9K2") {
		t.Fatal("expected command")
	}
	if !looksLikeSurveyCommand("/survey") {
		t.Fatal("bare /survey")
	}
	if !looksLikeSurveyCommand("问卷 A3F9K2") {
		t.Fatal("expected chinese start")
	}
	if looksLikeSurveyCommand("问卷写好了吗") {
		t.Fatal("bare keyword must not match")
	}
	if looksLikeSurveyCommand("hello") {
		t.Fatal("normal chat")
	}
	// Must not claim unrelated slash commands that share the prefix.
	if looksLikeSurveyCommand("/surveys") {
		t.Fatal("/surveys must not match")
	}
	if looksLikeSurveyCommand("/surveyhelp") {
		t.Fatal("/surveyhelp must not match")
	}
}

func TestCouldBeSurveySessionReply(t *testing.T) {
	if !couldBeSurveySessionReply("取消") {
		t.Fatal("cancel")
	}
	if !couldBeSurveySessionReply("2") {
		t.Fatal("answer token")
	}
	if !couldBeSurveySessionReply("1,2") {
		t.Fatal("multi index")
	}
	if !couldBeSurveySessionReply("修改") {
		t.Fatal("modify")
	}
	if !couldBeSurveySessionReply("prev") {
		t.Fatal("prev control")
	}
	if !couldBeSurveySessionReply("还可以") {
		t.Fatal("free text candidate")
	}
	// empty
	if couldBeSurveySessionReply("") {
		t.Fatal("empty")
	}
}

func TestStrictChoiceTokenIsNumericOnly(t *testing.T) {
	if !isStrictChoiceToken("3") {
		t.Fatal("rating/index digit")
	}
	if !isStrictChoiceToken("1,2") {
		t.Fatal("multi")
	}
	// Latin chat must not be treated as safe pre-session probes.
	if isStrictChoiceToken("hi") {
		t.Fatal("latin word must not be strict")
	}
	if isStrictChoiceToken("ok") {
		t.Fatal("ok must not be strict")
	}
	if isStrictChoiceToken("opt_yes") {
		t.Fatal("option id is free-text path")
	}
}

func TestLooksLikeSurveyCommandFullwidthSpace(t *testing.T) {
	if !looksLikeSurveyCommand("问卷\u3000A3F9K2") {
		t.Fatal("fullwidth space after 问卷")
	}
}

func TestShouldAttemptSurveyIM_KillSwitchAndCommands(t *testing.T) {
	// kill-switch: survey_enabled=false must never claim, even for /survey
	if shouldAttemptSurveyIM(false, "/survey A3F9K2") {
		t.Fatal("disabled must not attempt")
	}
	if shouldAttemptSurveyIM(false, "问卷 A3F9K2") {
		t.Fatal("disabled chinese start")
	}
	// enabled + command
	if !shouldAttemptSurveyIM(true, "/survey A3F9K2") {
		t.Fatal("enabled command should attempt")
	}
	if !shouldAttemptSurveyIM(true, "问卷 A3F9K2") {
		t.Fatal("enabled chinese start should attempt")
	}
	// bare 问卷 must not (looksLikeSurveyCommand false and not a short answer token pattern alone - actually 问卷写好了吗 has CJK)
	// 问卷写好了吗 is not a command (no space+code); couldBeSurveySessionReply may be true due to Han chars.
	// Non-@ free chat that is clearly not survey should ideally not attempt — document current: Han short text may attempt, Hub returns handled=false.
	if shouldAttemptSurveyIM(true, "") {
		t.Fatal("empty")
	}
}
