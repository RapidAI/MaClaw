package guiautomation

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestMergeUIElementsPrefersAccessibilityHandle(t *testing.T) {
	els := []taskengine.UIElement{
		{Name: "element_0", Type: "interactable", BBox: [4]int{10, 10, 40, 20}, Confidence: 0.9, Source: "yolo", Interactable: true},
		{Name: "OK", Type: "Button", BBox: [4]int{11, 11, 38, 18}, Confidence: 1, Source: "accessibility", Handle: "OkButton", Patterns: []string{"invoke"}, Interactable: true},
	}
	out := MergeUIElements(els)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1", len(out))
	}
	if out[0].Source != "accessibility" || out[0].Handle != "OkButton" {
		t.Fatalf("merged=%+v", out[0])
	}
	if out[0].Name != "OK" {
		t.Fatalf("name=%q", out[0].Name)
	}
}

func TestCompositeScreenParserMergesStaticSources(t *testing.T) {
	yolo := &StaticScreenParser{Els: []taskengine.UIElement{
		{Name: "element_0", Type: "interactable", BBox: [4]int{10, 10, 40, 20}, Confidence: 0.9, Source: "yolo", Interactable: true},
	}}
	a11y := &StaticScreenParser{Els: []taskengine.UIElement{
		{Name: "OK", Type: "Button", BBox: [4]int{11, 11, 38, 18}, Confidence: 1, Source: "accessibility", Handle: "OkButton", Interactable: true},
	}}
	out, err := NewCompositeScreenParser(yolo, a11y).Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Handle != "OkButton" {
		t.Fatalf("composite=%+v", out)
	}
}
