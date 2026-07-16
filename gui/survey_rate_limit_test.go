package main

import (
	"testing"
	"time"
)

func TestSurveyUserRateLimit_TwoPerSecond(t *testing.T) {
	l := newSurveyUserRateLimit()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	key := surveyRateKey("lansenger", "u1")

	if !l.allow(key, now) {
		t.Fatal("1st should allow")
	}
	if !l.allow(key, now.Add(100*time.Millisecond)) {
		t.Fatal("2nd should allow")
	}
	if l.allow(key, now.Add(200*time.Millisecond)) {
		t.Fatal("3rd within 1s must deny")
	}
	if l.wouldAllow(key, now.Add(200*time.Millisecond)) {
		t.Fatal("wouldAllow should also deny")
	}
	// after window slides past first two
	if !l.allow(key, now.Add(1100*time.Millisecond)) {
		t.Fatal("after window should allow again")
	}
}

func TestSurveyUserRateLimit_SeparateUsers(t *testing.T) {
	l := newSurveyUserRateLimit()
	now := time.Now()
	if !l.allow(surveyRateKey("lansenger", "a"), now) {
		t.Fatal("a")
	}
	if !l.allow(surveyRateKey("lansenger", "a"), now) {
		t.Fatal("a2")
	}
	// user b independent
	if !l.allow(surveyRateKey("lansenger", "b"), now) {
		t.Fatal("b should allow")
	}
}

func TestSurveyUserRateLimit_WouldAllowDoesNotRecord(t *testing.T) {
	l := newSurveyUserRateLimit()
	now := time.Now()
	key := surveyRateKey("lansenger", "peek")
	if !l.wouldAllow(key, now) {
		t.Fatal("empty should allow")
	}
	if !l.wouldAllow(key, now) {
		t.Fatal("peek must not consume")
	}
	if !l.allow(key, now) {
		t.Fatal("first allow")
	}
	if !l.allow(key, now) {
		t.Fatal("second allow")
	}
	if l.wouldAllow(key, now) {
		t.Fatal("full window")
	}
}

func TestSurveyUserRateLimit_RecordAfterHandle(t *testing.T) {
	l := newSurveyUserRateLimit()
	now := time.Now()
	key := surveyRateKey("lansenger", "sess")
	// Speculative path: check without consume, then record after handled.
	if !l.wouldAllow(key, now) {
		t.Fatal("should allow")
	}
	l.record(key, now)
	l.record(key, now)
	if l.wouldAllow(key, now) {
		t.Fatal("two records fill window")
	}
}

func TestSurveyRateKey(t *testing.T) {
	if surveyRateKey("", "u") != "lansenger:u" {
		t.Fatal(surveyRateKey("", "u"))
	}
}
