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
		ts = append(ts, browser.TargetInfo{ID: fmt.Sprint("p", i), Type: "page", URL: "https://a.example/" + fmt.Sprint(i)})
	}
	got := trimDesktopTabs("x", "p9", ts)
	if len(got) != 7 || len(closed) != 3 || got[len(got)-1].ID != "p9" {
		t.Fatalf("kept %d closed %v", len(got), closed)
	}
}
