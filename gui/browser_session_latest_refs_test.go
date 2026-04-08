package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestBrowserSessionViewCarriesLatestRefs(t *testing.T) {
	view := BrowserSessionView{
		LatestRefs: []browser.BrowserElementRef{{Ref: "@e1", Selector: "#submit"}, {Ref: "@e2", Selector: "input[name=q]"}},
	}
	if len(view.LatestRefs) != 2 {
		t.Fatalf("LatestRefs len = %d", len(view.LatestRefs))
	}
	if view.LatestRefs[0].Ref != "@e1" {
		t.Fatalf("first latest ref = %#v", view.LatestRefs[0])
	}
}
