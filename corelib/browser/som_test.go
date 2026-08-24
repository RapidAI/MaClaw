package browser

import (
	"fmt"
	"testing"
)

func TestCompactElementRefsOmitInternalFields(t *testing.T) {
	refs := []BrowserElementRef{{
		Ref:                "@e1",
		Role:               "button",
		Name:               "Buy",
		Tag:                "button",
		Selector:           "button:nth-of-type(3)",
		SelectorCandidates: []string{"button:nth-of-type(3)", "#buy"},
		BackendNodeID:      42,
		BoundingBox:        BrowserBoundingBox{X: 1, Y: 2, Width: 3, Height: 4},
		Disabled:           false,
		FrameID:            "main",
	}}
	compact := compactElementRefs(refs)
	if len(compact) != 1 {
		t.Fatalf("len=%d", len(compact))
	}
	if compact[0].Ref != "@e1" || compact[0].Role != "button" || compact[0].Name != "Buy" || !compact[0].Enabled {
		t.Fatalf("compact=%#v", compact[0])
	}
	if compact[0].FrameID != "" {
		t.Fatal("main frame_id should be omitted from compact refs")
	}
}

func TestCompactSelectIncludesValueInName(t *testing.T) {
	got := compactElementRef(BrowserElementRef{
		Ref:   "@e1",
		Role:  "combobox",
		Tag:   "select",
		Name:  "Country",
		Value: "China",
	})
	if got.Name != "Country = China" {
		t.Fatalf("name=%q", got.Name)
	}
	if got.Checked != nil {
		t.Fatal("select should not set checked")
	}
}

func TestObserveDataFromSnapshotOmitsFrameTreeAndTabID(t *testing.T) {
	data := observeDataFromSnapshot(BrowserSnapshot{
		SnapshotID: "snap-1",
		TargetID:   "TARGET",
		FrameTree:  []BrowserFrameSnapshot{{FrameID: "C2A1B0F3E4D5", URL: "https://pay.example.com"}},
		Refs:       []BrowserElementRef{{Ref: "@e1", Name: "Pay"}},
	})
	if _, ok := data["frame_tree"]; ok {
		t.Fatal("compact observe leaked frame_tree")
	}
	if _, ok := data["tab_id"]; ok {
		t.Fatal("compact observe leaked tab_id")
	}
}

func TestCompactCheckboxIncludesUnchecked(t *testing.T) {
	got := compactElementRef(BrowserElementRef{
		Ref:       "@e1",
		Role:      "checkbox",
		Tag:       "input",
		InputType: "checkbox",
		Name:      "Agree",
		Checked:   false,
	})
	if got.Checked == nil {
		t.Fatal("unchecked checkbox must still expose checked=false")
	}
	if *got.Checked {
		t.Fatal("expected checked=false")
	}
	on := compactElementRef(BrowserElementRef{Ref: "@e2", Role: "checkbox", Checked: true})
	if on.Checked == nil || !*on.Checked {
		t.Fatal("checked checkbox must expose checked=true")
	}
}

func TestCompactElementRefOmitsCDPFrameID(t *testing.T) {
	got := compactElementRef(BrowserElementRef{
		Ref:     "@e2",
		Role:    "button",
		Name:    "Pay",
		FrameID: "C2A1B0F3E4D5",
	})
	if got.FrameID != "" {
		t.Fatalf("compact frame_id leaked CDP id %q", got.FrameID)
	}
}

func TestTruncateRefsCapsAt80(t *testing.T) {
	refs := make([]BrowserElementRef, 120)
	for i := range refs {
		refs[i] = BrowserElementRef{Ref: fmt.Sprintf("@e%d", i+1), Visible: true, InViewport: i < 10}
	}
	kept, truncated := truncateRefs(refs, compactRefLimit)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(kept) != compactRefLimit {
		t.Fatalf("kept=%d want %d", len(kept), compactRefLimit)
	}
	data := observeDataFromSnapshot(BrowserSnapshot{Refs: refs, RefsTruncated: true})
	compact, ok := data["refs"].([]CompactElementRef)
	if !ok {
		t.Fatalf("refs type %T", data["refs"])
	}
	if len(compact) != compactRefLimit {
		t.Fatalf("compact refs=%d want %d", len(compact), compactRefLimit)
	}
	if data["refs_truncated"] != true {
		t.Fatal("refs_truncated should be true")
	}
}

func TestObserveDataFromSnapshotHidesSelectorCandidates(t *testing.T) {
	data := observeDataFromSnapshot(BrowserSnapshot{
		SnapshotID: "snap-1",
		URL:        "https://example.com",
		Refs: []BrowserElementRef{{
			Ref:                "@e1",
			Name:               "OK",
			Selector:           "#ok",
			SelectorCandidates: []string{"#ok", "button"},
			BackendNodeID:      9,
		}},
	})
	compact, _ := data["refs"].([]CompactElementRef)
	if len(compact) != 1 || compact[0].Ref != "@e1" {
		t.Fatalf("compact=%#v", compact)
	}
	encoded := compactActionData(&BrowserObservation{Snapshot: BrowserSnapshot{
		Refs: []BrowserElementRef{{Ref: "@e1", SelectorCandidates: []string{"#ok"}}},
	}}, nil)
	if _, ok := encoded["selector_candidates"]; ok {
		t.Fatal("compact action data leaked selector_candidates")
	}
}

func TestCompactElementRefDoesNotUseSelectorAsName(t *testing.T) {
	got := compactElementRef(BrowserElementRef{
		Ref:      "@e1",
		Selector: "button:nth-of-type(3)",
		Tag:      "button",
	})
	if got.Name != "" {
		t.Fatalf("compact name leaked selector: %q", got.Name)
	}
}

func TestCompactActionDataStripsSelectorExtra(t *testing.T) {
	data := compactActionData(&BrowserObservation{Snapshot: BrowserSnapshot{
		SnapshotID: "snap-1",
		URL:        "https://example.com",
	}}, map[string]interface{}{
		"target":              "@e1",
		"selector":            "#buy",
		"selector_candidates": []string{"#buy"},
		"bounding_box":        BrowserBoundingBox{X: 1},
		"backend_node_id":     42,
	})
	for _, key := range []string{"selector", "selector_candidates", "bounding_box", "backend_node_id"} {
		if _, ok := data[key]; ok {
			t.Fatalf("compact action data leaked %s", key)
		}
	}
	if data["target"] != "@e1" {
		t.Fatalf("target=%v", data["target"])
	}
}
