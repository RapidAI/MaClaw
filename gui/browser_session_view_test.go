package main

import "testing"

func TestBrowserSessionViewCarriesSnapshotFields(t *testing.T) {
	view := BrowserSessionView{
		ID:             "browser-1",
		CurrentURL:     "https://example.com",
		CurrentTitle:   "Example",
		ReadyState:     "complete",
		LastSnapshotID: "snap-1",
	}
	if view.CurrentURL == "" || view.CurrentTitle == "" || view.LastSnapshotID == "" {
		t.Fatalf("view fields missing: %#v", view)
	}
}

func TestBrowserSessionViewKeepsUpdatedSnapshotFields(t *testing.T) {
	view := BrowserSessionView{}
	view.CurrentURL = "https://example.com/a"
	view.CurrentTitle = "Page A"
	view.LastSnapshotID = "snap-a"
	view.CurrentURL = "https://example.com/b"
	view.CurrentTitle = "Page B"
	view.LastSnapshotID = "snap-b"
	if view.CurrentURL != "https://example.com/b" || view.CurrentTitle != "Page B" || view.LastSnapshotID != "snap-b" {
		t.Fatalf("updated view fields = %#v", view)
	}
}
