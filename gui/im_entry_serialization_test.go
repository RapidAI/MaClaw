package main

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestIMMessageSerializationCancelsWhileWaitingForOwner(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	owner := "lansenger:profile:group:user"
	first := h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: "lansenger_local", Text: "first"}, nil, nil, nil, explicitTaskSlotDecision{})
	defer first.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultC := make(chan imMessageSerializationResult, 1)
	go func() {
		resultC <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: owner, Platform: "lansenger_local", Text: "second", CancelCtx: ctx}, nil, nil, nil, explicitTaskSlotDecision{})
	}()
	cancel()
	select {
	case result := <-resultC:
		if !result.Handled || result.Response == nil || result.Response.Error != context.Canceled.Error() {
			t.Fatalf("canceled wait result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled owner wait did not return promptly")
	}
}

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
