package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestCheckoutGateStopsEveryActionUntilThePersonConfirms(t *testing.T) {
	prevPage := desktopReadCheckoutPage
	prevAt := desktopCheckoutElementAt
	t.Cleanup(func() {
		desktopReadCheckoutPage = prevPage
		desktopCheckoutElementAt = prevAt
		browser.ClearCheckoutConfirmation("tenant-a:alice")
		browser.ClearRunCart("run-pay")
	})
	desktopReadCheckoutPage = func(*browser.BrowserAgentSession) browser.CheckoutPage {
		return browser.CheckoutPage{
			URL: "https://shop.example/checkout", Context: "应付 ¥88.00\n收货地址",
			Focused: "提交订单", Selected: []string{"牛奶", "面包"},
		}
	}
	desktopCheckoutElementAt = func(context.Context, agentruntime.Scope, *browser.BrowserAgentSession, string, float64, float64) (string, string) {
		return "提交订单", "提交订单"
	}
	browser.NoteRunCartAdd("run-pay", "牛奶")
	scope := agentruntime.Scope{TenantID: "tenant-a", UserID: "alice", RunID: "run-pay", InstanceID: "inst-pay"}
	actions := []struct {
		name string
		args map[string]any
	}{
		{"click", map[string]any{"text": "提交订单"}},
		{"press", map[string]any{"key": "Return"}},
		{"type", map[string]any{"text": "\n"}},
		{"app_run", map[string]any{"steps": []any{map[string]any{"action": "click", "x": 10, "y": 20}}}},
		{"computer", map[string]any{"actions": []any{map[string]any{"type": "keypress", "keys": []any{"ENTER"}}}}},
		{"task_run", map[string]any{"steps": []any{map[string]any{"action": "click", "params": map[string]string{"text": "立即支付"}}}}},
		{"script", map[string]any{"script": `document.querySelector('button').innerText.includes('提交订单'); button.click()`}},
	}
	for _, action := range actions {
		ask, hold := desktopHoldCheckout(context.Background(), scope, nil, ":1", action.name, action.args)
		if !hold || !strings.Contains(ask, "确认支付") || !strings.Contains(ask, "面包") || strings.Contains(ask, "牛奶") {
			t.Fatalf("%s hold=%v ask=%q", action.name, hold, ask)
		}
	}
	addAsk, addHold := desktopHoldCheckout(context.Background(), scope, nil, ":1", "click", map[string]any{"text": "加入购物车"})
	if addHold || addAsk != "" {
		t.Fatalf("add to cart held: %q", addAsk)
	}
	if _, ok := browser.RecordCheckoutConfirmation("tenant-a:alice", "确认支付 ¥88"); !ok {
		t.Fatal("record")
	}
	if ask, hold := desktopHoldCheckout(context.Background(), scope, nil, ":1", "click", map[string]any{"text": "提交订单"}); hold || ask != "" {
		t.Fatalf("confirmed click still held: %q", ask)
	}
	if ask, hold := desktopHoldCheckout(context.Background(), scope, nil, ":1", "app_run", map[string]any{"steps": []any{map[string]any{"action": "click", "x": 3, "y": 4}}}); hold || ask != "" {
		t.Fatalf("confirmed app click still held: %q", ask)
	}
	if browser.CheckoutReady("tenant-a:alice", "应付 ¥88.00") {
		t.Fatal("app_run left the confirmation unused")
	}
}

func TestCheckoutGateLeavesOrdinaryClicksAlone(t *testing.T) {
	prev := desktopReadCheckoutPage
	t.Cleanup(func() { desktopReadCheckoutPage = prev })
	desktopReadCheckoutPage = func(*browser.BrowserAgentSession) browser.CheckoutPage {
		return browser.CheckoutPage{URL: "https://shop.example/item", Focused: "搜索", Context: "搜索商品"}
	}
	scope := agentruntime.Scope{TenantID: "tenant-a", UserID: "alice", RunID: "run-look"}
	if ask, hold := desktopHoldCheckout(context.Background(), scope, nil, ":1", "click", map[string]any{"text": "加入购物车"}); hold {
		t.Fatal(ask)
	}
	if ask, hold := desktopHoldCheckout(context.Background(), scope, nil, ":1", "press", map[string]any{"key": "Return"}); hold {
		t.Fatal(ask)
	}
	if ask, hold := desktopHoldCheckout(context.Background(), scope, nil, ":1", "probe", nil); hold {
		t.Fatal(ask)
	}
}
