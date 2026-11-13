package main

import (
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestTrimDesktopTabsKeepsAttachedAndNewest(t *testing.T) {
	var closed []string
	old := desktopCloseTab
	desktopCloseTab = func(_, id string) error { closed = append(closed, id); return nil }
	defer func() { desktopCloseTab = old }()
	var ts []browser.TargetInfo
	for i := 0; i < 10; i++ {
		id := fmt.Sprint("p", i)
		noteDesktopOwnedTab("x", id)
		ts = append(ts, browser.TargetInfo{ID: id, Type: "page", URL: "https://a.example/" + id})
	}
	got := trimDesktopTabs("x", "p9", ts)
	if len(got) != 7 || len(closed) != 3 || got[len(got)-1].ID != "p9" {
		t.Fatalf("kept %d closed %v", len(got), closed)
	}
}

func TestTrimDesktopTabsLeavesAnotherBotsPage(t *testing.T) {
	var closed []string
	old := desktopCloseTab
	desktopCloseTab = func(_, id string) error { closed = append(closed, id); return nil }
	defer func() { desktopCloseTab = old }()
	ts := []browser.TargetInfo{
		{ID: "foreign-note", Type: "page", Title: "write and publish a note", URL: "https://www.xiaohongshu.com/explore"},
		{ID: "mine", Type: "page", Title: "this bot", URL: "https://example.com/mine"},
	}
	noteDesktopOwnedTab("bot-a", "mine")
	got := trimDesktopTabs("bot-a", "mine", ts)
	if len(got) != 2 || len(closed) != 0 {
		t.Fatalf("kept %d closed %v", len(got), closed)
	}
	if !desktopTabOwned("bot-a", "mine", "mine") || desktopTabOwned("bot-a", "foreign-note", "mine") {
		t.Fatal("foreign page was treated as this bot's")
	}
}
