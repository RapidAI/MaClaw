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
