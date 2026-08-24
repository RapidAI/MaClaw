package browser

import "testing"

func TestCompleteActionMarksUnchanged(t *testing.T) {
	snap := BrowserSnapshot{
		SnapshotID: "snap-2",
		URL:        "https://example.com/buy",
		Title:      "Buy",
		Refs:       []BrowserElementRef{{Role: "button", Name: "Buy"}},
	}
	s := &BrowserAgentSession{
		ID:              "sess-1",
		lastFingerprint: snapshotFingerprint(snap),
	}
	got := s.completeAction("browser_click", "clicked @e1", "@e1", &BrowserObservation{Snapshot: snap}, map[string]interface{}{
		"target": "@e1",
	}, true)
	if got.Status != "unchanged" {
		t.Fatalf("status=%q", got.Status)
	}
	if _, ok := got.Data["delta"]; !ok {
		t.Fatal("missing delta")
	}
}

func TestCompleteActionAllowsUnchangedWhenNotRequired(t *testing.T) {
	snap := BrowserSnapshot{URL: "https://example.com", Title: "Home"}
	s := &BrowserAgentSession{lastFingerprint: snapshotFingerprint(snap)}
	got := s.completeAction("browser_type", "typed 3 chars", "activeElement", &BrowserObservation{Snapshot: snap}, nil, false)
	if got.Status != "ok" {
		t.Fatalf("status=%q", got.Status)
	}
}

func TestActivatingRefSkipsEditable(t *testing.T) {
	if activatingRef(&BrowserElementRef{Role: "textbox"}) {
		t.Fatal("textbox click should not require a page change")
	}
	if activatingRef(&BrowserElementRef{Tag: "textarea"}) {
		t.Fatal("textarea click should not require a page change")
	}
	if activatingRef(&BrowserElementRef{Tag: "input"}) {
		t.Fatal("text input click should not require a page change")
	}
	if activatingRef(&BrowserElementRef{Tag: "input", InputType: "email"}) {
		t.Fatal("email input click should not require a page change")
	}
	if !activatingRef(&BrowserElementRef{Role: "button"}) {
		t.Fatal("button click should require a page change")
	}
	if !activatingRef(&BrowserElementRef{Tag: "input", InputType: "submit"}) {
		t.Fatal("submit input click should require a page change")
	}
	if !activatingRef(&BrowserElementRef{Tag: "input", InputType: "checkbox"}) {
		t.Fatal("checkbox click should require a page change")
	}
	if !activatingRef(&BrowserElementRef{Role: "combobox"}) {
		t.Fatal("combobox click should require a page change")
	}
}

func TestSnapshotFingerprintIgnoresExcerpt(t *testing.T) {
	a := snapshotFingerprint(BrowserSnapshot{URL: "https://ex.com", Title: "T", PageTextExcerpt: "12:00", Refs: []BrowserElementRef{{Role: "button", Name: "Buy"}}})
	b := snapshotFingerprint(BrowserSnapshot{URL: "https://ex.com", Title: "T", PageTextExcerpt: "12:01", Refs: []BrowserElementRef{{Role: "button", Name: "Buy"}}})
	if a != b {
		t.Fatal("clock excerpt should not change fingerprint")
	}
}

func TestSnapshotFingerprintIncludesDisabled(t *testing.T) {
	enabled := snapshotFingerprint(BrowserSnapshot{URL: "https://ex.com", Title: "T", Refs: []BrowserElementRef{{Role: "button", Name: "Submit"}}})
	disabled := snapshotFingerprint(BrowserSnapshot{URL: "https://ex.com", Title: "T", Refs: []BrowserElementRef{{Role: "button", Name: "Submit", Disabled: true}}})
	if enabled == disabled {
		t.Fatal("enabled vs disabled should change fingerprint")
	}
}

func TestSnapshotFingerprintIncludesCheckedAndSelectValue(t *testing.T) {
	base := BrowserSnapshot{URL: "https://ex.com", Title: "T", Refs: []BrowserElementRef{{Role: "checkbox", Name: "Agree"}}}
	checked := BrowserSnapshot{URL: "https://ex.com", Title: "T", Refs: []BrowserElementRef{{Role: "checkbox", Name: "Agree", Checked: true}}}
	if snapshotFingerprint(base) == snapshotFingerprint(checked) {
		t.Fatal("checkbox checked should change fingerprint")
	}
	before := BrowserSnapshot{URL: "https://ex.com", Title: "T", Refs: []BrowserElementRef{{Tag: "select", Name: "Country", Value: "US"}}}
	after := BrowserSnapshot{URL: "https://ex.com", Title: "T", Refs: []BrowserElementRef{{Tag: "select", Name: "Country", Value: "China"}}}
	if snapshotFingerprint(before) == snapshotFingerprint(after) {
		t.Fatal("select value should change fingerprint")
	}
}
