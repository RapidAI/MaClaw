package main

import "testing"

func TestBrowserSessionViewActionFieldsRemainStable(t *testing.T) {
	view := BrowserSessionView{
		CurrentURL:     "https://example.com/page-a",
		CurrentTitle:   "Page A",
		LastSnapshotID: "snap-a",
	}
	view.CurrentURL = "https://example.com/page-b"
	view.CurrentTitle = "Page B"
	view.LastSnapshotID = "snap-b"
	if view.CurrentURL != "https://example.com/page-b" {
		t.Fatalf("CurrentURL = %q", view.CurrentURL)
	}
	if view.CurrentTitle != "Page B" {
		t.Fatalf("CurrentTitle = %q", view.CurrentTitle)
	}
	if view.LastSnapshotID != "snap-b" {
		t.Fatalf("LastSnapshotID = %q", view.LastSnapshotID)
	}
}

func TestBrowserSessionViewPreviewCanCarryRefs(t *testing.T) {
	view := BrowserSessionView{}
	view.Preview = SessionPreview{PreviewLines: []string{"--- refs ---", "@e1 Submit", "@e2 Search"}}
	if len(view.Preview.PreviewLines) != 3 {
		t.Fatalf("PreviewLines len = %d", len(view.Preview.PreviewLines))
	}
	if view.Preview.PreviewLines[1] != "@e1 Submit" {
		t.Fatalf("first ref preview line = %q", view.Preview.PreviewLines[1])
	}
}
