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
	if !isSurveyControlWord("取消") {
		t.Fatal("control")
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
