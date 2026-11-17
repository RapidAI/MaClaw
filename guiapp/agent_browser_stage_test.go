package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestAgentBrowserStageShowsConfirmThenAttention(t *testing.T) {
	t.Cleanup(func() {
		browser.SetUserHoldsBrowser(false, "")
		_ = browser.ResolveCheckoutCard(false)
		liveAgentBrowser.mu.Lock()
		liveAgentBrowser.open = false
		liveAgentBrowser.attention = false
		liveAgentBrowser.askReason = ""
		liveAgentBrowser.clearConfirmLocked()
		liveAgentBrowser.mu.Unlock()
	})
	liveAgentBrowser.note(browser.BrowserStageNotice{
		Kind: "confirm", SessionID: "sess", Amount: "9.98", Item: "椰子水饮料", Address: "上地佳园", Host: "item.jd.com",
	})
	view := liveAgentBrowser.snapshot(false)
	if !view.Open || view.ConfirmAmount != "9.98" || view.ConfirmItem != "椰子水饮料" || view.Host != "item.jd.com" || view.Attention {
		t.Fatalf("confirm view %+v", view)
	}
	liveAgentBrowser.note(browser.BrowserStageNotice{
		Kind: "attention", Hold: true, Reason: "secret", Host: "item.jd.com", SessionID: "sess",
	})
	view = liveAgentBrowser.snapshot(false)
	if !view.Attention || view.ConfirmAmount != "" || view.AskReason != "secret" || view.Host != "item.jd.com" {
		t.Fatalf("attention view %+v", view)
	}
}

func TestConfirmClearDoesNotReopenPreview(t *testing.T) {
	t.Cleanup(func() {
		liveAgentBrowser.mu.Lock()
		liveAgentBrowser.open = false
		liveAgentBrowser.dismissed = false
		liveAgentBrowser.attention = false
		liveAgentBrowser.askReason = ""
		liveAgentBrowser.clearConfirmLocked()
		liveAgentBrowser.mu.Unlock()
	})
	liveAgentBrowser.note(browser.BrowserStageNotice{
		Kind: "confirm", SessionID: "sess", Amount: "9.98", Item: "椰子水",
	})
	liveAgentBrowser.dismiss()
	liveAgentBrowser.note(browser.BrowserStageNotice{Kind: "confirm_clear"})
	view := liveAgentBrowser.snapshot(false)
	if view.Open || view.ConfirmAmount != "" {
		t.Fatalf("clear reopened %+v", view)
	}
	liveAgentBrowser.note(browser.BrowserStageNotice{
		Kind: "confirm", SessionID: "sess", Amount: "9.98", Item: "椰子水",
	})
	liveAgentBrowser.note(browser.BrowserStageNotice{Kind: "confirm_clear"})
	view = liveAgentBrowser.snapshot(false)
	if !view.Open || view.ConfirmAmount != "" || view.AskReason != "" {
		t.Fatalf("clear kept the card %+v", view)
	}
}
