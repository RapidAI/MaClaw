package main

import (
	"testing"
	"time"
)

func TestSurveySessionHint_MarkActiveClear(t *testing.T) {
	h := newSurveySessionHint()
	now := time.Now()
	key := surveyRateKey("lansenger", "u1")
	if h.active(key, now) {
		t.Fatal("empty")
	}
	h.mark(key, now, time.Minute)
	if !h.active(key, now) {
		t.Fatal("marked")
	}
	if h.active(key, now.Add(2*time.Minute)) {
		t.Fatal("expired")
	}
	h.mark(key, now, time.Minute)
	h.clear(key)
	if h.active(key, now) {
		t.Fatal("cleared")
	}
}

func TestIsFreeTextVsChoice(t *testing.T) {
	if !isStrictChoiceToken("1,2") {
		t.Fatal("choice")
	}
	if !isStrictChoiceToken("１") {
		t.Fatal("fullwidth digit should be choice")
	}
	if !isStrictChoiceToken("1。") || !isStrictChoiceToken("1、") {
		t.Fatal("trailing mobile punctuation should still be choice")
	}
	if !isSurveyControlWord("取消") {
		t.Fatal("control")
	}
	if !isSurveyControlWord("跳过") || !isSurveyControlWord("skip") {
		t.Fatal("skip tokens must be control words for mention-bypass")
	}
	if isFreeTextSurveyCandidate("跳过") {
		t.Fatal("skip is control, not free text")
	}
	if !isFreeTextSurveyCandidate("还可以吧") {
		t.Fatal("free text")
	}
	if isFreeTextSurveyCandidate("2") {
		t.Fatal("2 is choice not free text")
	}
	// long free text still a session candidate at prefilter level
	long := ""
	for i := 0; i < 100; i++ {
		long += "测"
	}
	if !couldBeSurveySessionReply(long) {
		t.Fatal("long text should be session candidate")
	}
}

func TestSurveyShouldBypassMention(t *testing.T) {
	// Bare choice answer (the common multi-question path) must bypass @ requirement.
	if !surveyShouldBypassMention(true, "1", false) {
		t.Fatal("choice token should bypass even without session hint")
	}
	if !surveyShouldBypassMention(true, "1,2", false) {
		t.Fatal("multi-choice token should bypass")
	}
	if !surveyShouldBypassMention(true, "取消", false) {
		t.Fatal("control word should bypass")
	}
	if !surveyShouldBypassMention(true, "跳过", false) {
		t.Fatal("optional skip must bypass without session hint")
	}
	if !surveyShouldBypassMention(true, "skip", false) {
		t.Fatal("en skip must bypass")
	}
	if !surveyShouldBypassMention(true, "/survey YF0QMN", false) {
		t.Fatal("survey command should bypass")
	}
	// Free text without active session must not steal normal group chat.
	if surveyShouldBypassMention(true, "你好", false) {
		t.Fatal("free text without hint must not bypass")
	}
	if !surveyShouldBypassMention(true, "还可以", true) {
		t.Fatal("free text with session hint should bypass")
	}
	if surveyShouldBypassMention(false, "1", false) {
		t.Fatal("disabled survey must not bypass")
	}
	if surveyShouldBypassMention(true, "   ", true) {
		t.Fatal("blank text must not bypass")
	}
}
