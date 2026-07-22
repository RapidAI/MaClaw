package main

import (
	"testing"
	"time"
)

func TestClearPerUserSessionStateClearsPendingConfirmation(t *testing.T) {
	userID := "u-session-reset"
	store := newAIConfirmationStore("")
	store.set(&pendingConfirmation{
		ID:           "confirm-session-reset",
		UserID:       userID,
		OriginalText: "run task",
		ResumeText:   "run task",
		Status:       confirmationStatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	h := &IMMessageHandler{confirmationStore: store}
	h.clearPerUserSessionState(userID)

	if got := store.get(userID); got != nil {
		t.Fatalf("pending confirmation survived session reset: %#v", got)
	}
}

func TestClearPerUserSessionStateClearsCompletedGuideAcceptancesForOnlyThatUser(t *testing.T) {
	completed := func(text string) *guideLaunchAcceptance {
		a := &guideLaunchAcceptance{done: make(chan struct{}), accepted: true, acceptedAt: time.Now(), text: text}
		close(a.done)
		return a
	}
	h := &IMMessageHandler{}
	h.acceptedGuideLaunchIDs.Store("u-reset\x00launch-1", completed("old steer"))
	h.acceptedGuideLaunchIDs.Store("u-other\x00launch-1", completed("other steer"))

	h.clearPerUserSessionState("u-reset")

	if _, ok := h.acceptedGuideLaunchIDs.Load("u-reset\x00launch-1"); ok {
		t.Fatal("reset session retained a completed guide acceptance")
	}
	if _, ok := h.acceptedGuideLaunchIDs.Load("u-other\x00launch-1"); !ok {
		t.Fatal("reset session cleared another user's guide acceptance")
	}
}

func TestClearPerUserSessionStateKeepsResolvingGuideAcceptance(t *testing.T) {
	h := &IMMessageHandler{}
	pending := &guideLaunchAcceptance{done: make(chan struct{}), text: "in flight"}
	h.acceptedGuideLaunchIDs.Store("u-reset\x00launch-pending", pending)

	h.clearPerUserSessionState("u-reset")

	if got, ok := h.acceptedGuideLaunchIDs.Load("u-reset\x00launch-pending"); !ok || got != pending {
		t.Fatal("reset must not open a duplicate-injection window for a resolving launch")
	}
}
