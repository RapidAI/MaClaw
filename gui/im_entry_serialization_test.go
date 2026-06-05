package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestIMMessageSerializationAllowsDifferentOwnersConcurrently(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: "desktop-user", Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()

	acquired := make(chan imMessageSerializationResult, 1)
	go func() {
		acquired <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: "desktop-user:C:/tasks/a", Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()

	select {
	case second := <-acquired:
		second.Unlock()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("different owner waited on another owner's serialization mutex")
	}
}

func TestIMMessageSerializationSerializesSameOwner(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "desktop-user:C:/tasks/same"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "one"}, nil, nil, nil, explicitTaskSlotDecision{})

	acquired := make(chan imMessageSerializationResult, 1)
	go func() {
		acquired <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: desktopPlatform, Text: "two"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()

	select {
	case second := <-acquired:
		second.Unlock()
		first.Unlock()
		t.Fatal("same owner acquired serialization mutex while first loop still held it")
	case <-time.After(100 * time.Millisecond):
	}

	first.Unlock()
	select {
	case second := <-acquired:
		second.Unlock()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("same owner did not acquire serialization mutex after first loop released it")
	}
}
