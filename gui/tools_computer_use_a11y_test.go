package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestFlattenA11yKeepsListAndTreeItems(t *testing.T) {
	tree := []accessibility.Element{
		{
			Role: "Window", Name: "Chat",
			Bounds: accessibility.Rect{X: 100, Y: 50, Width: 400, Height: 300},
			Children: []accessibility.Element{
				{Role: "ListItem", Name: "Alice", Bounds: accessibility.Rect{X: 110, Y: 60, Width: 80, Height: 24}},
				{Role: "TreeItem", Name: "Inbox", Bounds: accessibility.Rect{X: 110, Y: 90, Width: 80, Height: 24}},
				{Role: "Edit", Name: "Search", AutomationID: "searchBox", Patterns: []string{"value"},
					Bounds: accessibility.Rect{X: 110, Y: 120, Width: 200, Height: 28}},
			},
		},
	}
	meta := computeruse.ScreenMeta{ScaleFactor: 1, OriginX: 100, OriginY: 50}
	var out []taskengine.UIElement
	flattenA11y(&out, tree, 0, 5, meta)
	names := map[string]taskengine.UIElement{}
	for _, el := range out {
		names[el.Name] = el
	}
	for _, want := range []string{"Chat", "Alice", "Inbox", "Search"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing %q in %+v", want, out)
		}
	}
	search := names["Search"]
	if search.Handle != "searchBox" || search.BBox[0] != 10 || search.BBox[1] != 70 {
		t.Fatalf("search mapped=%+v", search)
	}
	if !a11yRoleInteractable("ListItem") || !a11yRoleInteractable("TreeItem") || !a11yRoleInteractable("TabItem") {
		t.Fatal("expected list/tree/tab to be interactable")
	}
}

func TestScreenshotMatchesDisplay(t *testing.T) {
	if !screenshotMatchesDisplay(1920, 1080, 1920, 1080) {
		t.Fatal("1x should match")
	}
	if !screenshotMatchesDisplay(2880, 1800, 1440, 900) {
		t.Fatal("retina 2x should match")
	}
	if screenshotMatchesDisplay(3840, 1080, 1920, 1080) {
		t.Fatal("stitched dual-width should not match a single display")
	}
}

func TestCuMarkSemantic(t *testing.T) {
	if cuMarkSemantic(&computeruse.MarkedElement{Source: "yolo"}) {
		t.Fatal("bare yolo mark should not be semantic")
	}
	if !cuMarkSemantic(&computeruse.MarkedElement{Source: "accessibility"}) {
		t.Fatal("a11y mark should be semantic")
	}
	if !cuMarkSemantic(&computeruse.MarkedElement{Source: "yolo", Handle: "id"}) {
		t.Fatal("handle should enable semantic")
	}
}
