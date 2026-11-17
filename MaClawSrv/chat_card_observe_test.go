package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/chatcard"
)

func TestChatCardObserverUsesTheBoundPage(t *testing.T) {
	chatCardPages.mu.Lock()
	chatCardPages.items = nil
	chatCardPages.mu.Unlock()
	card := chatcard.Card{
		TenantID: "tenant-a", UserID: "alice", RunID: "run-1", TaskID: "task-1",
		Question: "请在这台电脑上完成登录", LeaseID: "lease-a",
		Target: &chatcard.Target{Origin: "shop.example", URL: "https://shop.example/pay", Frame: "pay-frame"},
		Lines:  []chatcard.Line{{Text: "https://shop.example/pay"}},
	}
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	noteChatCardBoundPage(boundChatCardPage{
		TenantID: "tenant-a", UserID: "alice", RunID: "run-1", LeaseID: "lease-a",
		Frame: "pay-frame", Origin: "shop.example", Source: "page", Text: card.Question, At: time.Now().Add(-time.Minute),
	})
	if _, err := observeChatCardBoundPage(p, card); err == nil {
		t.Fatal("the card question was accepted as evidence")
	}
	noteChatCardBoundPage(boundChatCardPage{
		TenantID: "tenant-a", UserID: "alice", RunID: "run-1", LeaseID: "other-lease",
		Frame: "pay-frame", Origin: "shop.example", Source: "page", Text: "别的电脑上的页面", At: time.Now(),
	})
	if _, err := observeChatCardBoundPage(p, card); err == nil {
		t.Fatal("another lease was accepted")
	}
	noteChatCardBoundPage(boundChatCardPage{
		TenantID: "tenant-a", UserID: "alice", RunID: "run-1", LeaseID: "lease-a",
		Frame: "other-frame", Origin: "shop.example", Source: "tool", Text: "别的框里的结果", At: time.Now(),
	})
	if _, err := observeChatCardBoundPage(p, card); err == nil {
		t.Fatal("another frame was accepted")
	}
	noteChatCardPageFromScope(agentruntime.Scope{TenantID: "tenant-a", UserID: "alice", RunID: "run-1", SessionID: "task-1"}, "https://shop.example/pay", "", "当前页面：登录框仍在", "pay-frame")
	// The scope note uses the desktop user as the lease. The card lease is lease-a, so it must not match.
	if _, err := observeChatCardBoundPage(p, card); err == nil {
		t.Fatal("a page without this card's lease was accepted")
	}
	noteChatCardBoundPage(boundChatCardPage{
		TenantID: "tenant-a", UserID: "alice", RunID: "run-1", LeaseID: "lease-a",
		Frame: "pay-frame", Origin: "shop.example", Source: "page", Text: "当前页面：登录框仍在", At: time.Now(),
	})
	got, err := observeChatCardBoundPage(p, card)
	if err != nil || got.Text != "当前页面：登录框仍在" || got.Source != "page" {
		t.Fatalf("observation = %#v %v", got, err)
	}
	other := agentservice.Principal{TenantID: "tenant-a", UserID: "bob"}
	if _, err := observeChatCardBoundPage(other, card); err == nil {
		t.Fatal("another owner read this page")
	}
}
