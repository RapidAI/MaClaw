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

func TestSurveyRateKey(t *testing.T) {
	if surveyRateKey("", "u") != "lansenger:u" {
		t.Fatal(surveyRateKey("", "u"))
	}
}
