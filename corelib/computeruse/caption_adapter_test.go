package computeruse

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestInferElementTypeFromRoleAndGeometry(t *testing.T) {
	if got := InferElementType(taskengine.UIElement{Type: "Button"}, "OK"); got != "button" {
		t.Fatalf("role: %s", got)
	}
	if got := InferElementType(taskengine.UIElement{Type: "interactable", BBox: [4]int{0, 0, 24, 24}}, ""); got != "icon" {
		t.Fatalf("icon: %s", got)
	}
	if got := InferElementType(taskengine.UIElement{Type: "interactable", Patterns: []string{"value"}}, "搜索"); got != "edit" {
		t.Fatalf("edit: %s", got)
	}
	if got := InferElementType(taskengine.UIElement{Type: "interactable"}, "Save"); got != "button" {
		t.Fatalf("named: %s", got)
	}
}

func TestMatchAdapterFamilies(t *testing.T) {
	if h := MatchAdapter([]string{"Document.docx - Word"}, ""); h.Kind != AdapterOffice {
		t.Fatalf("office: %+v", h)
	}
	if h := MatchAdapter(nil, "微信"); h.Kind != AdapterIM {
		t.Fatalf("im: %+v", h)
	}
	if h := MatchAdapter([]string{"Google Chrome"}, ""); h.Kind != AdapterBrowser {
		t.Fatalf("browser: %+v", h)
	}
	if h := MatchAdapter([]string{"File Explorer"}, ""); h.Kind != AdapterShell {
		t.Fatalf("shell: %+v", h)
	}
}

func TestNeedsCaptionAndParseApply(t *testing.T) {
	if !NeedsCaption("") || !NeedsCaption("element_3") {
		t.Fatal("synthetic/empty should need caption")
	}
	if NeedsCaption("Save") {
		t.Fatal("real label should not need caption")
	}
	marks := []MarkedElement{
		{Name: "OK", BBox: [4]int{0, 0, 40, 20}},
		{Name: "", Source: "accessibility", BBox: [4]int{10, 10, 24, 24}},
		{Name: "element_1", Source: "yolo", BBox: [4]int{40, 40, 20, 20}},
		{Name: "", BBox: [4]int{0, 0, 0, 0}},
	}
	got := UnlabeledCaptionIndices(marks, 8)
	if len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("indices=%v want yolo then a11y", got)
	}
	cap := ParseCaptionResponse("```json\n{\"name\":\"Search\",\"type\":\"edit\"}\n```")
	if cap.Name != "Search" || cap.Type != "edit" {
		t.Fatalf("parse json: %+v", cap)
	}
	wrapped := ParseCaptionResponse("Sure, here you go: {\"name\":\"Save\",\"type\":\"other\"}")
	if wrapped.Name != "Save" {
		t.Fatalf("parse wrapped json: %+v", wrapped)
	}
	el := MarkedElement{Type: "interactable", BBox: [4]int{0, 0, 80, 24}}
	if !ApplyCaption(&el, wrapped) || el.Name != "Save" || el.Type != "button" {
		t.Fatalf("apply other type: %+v", el)
	}
	plain := ParseCaptionResponse("Back")
	if plain.Name != "Back" {
		t.Fatalf("plain: %+v", plain)
	}
	synth := ParseCaptionResponse(`{"name":"element_3","type":"icon"}`)
	if synth.Name != "" || synth.Type != "icon" {
		t.Fatalf("synthetic json name: %+v", synth)
	}
	if got := ParseCaptionResponse(`{"type":"other"}`); got != (Caption{}) {
		t.Fatalf("other without a name should be empty: %+v", got)
	}
	unchanged := MarkedElement{Name: "OK", Type: "button", BBox: [4]int{0, 0, 40, 20}}
	if ApplyCaption(&unchanged, Caption{Name: "element_1"}) {
		t.Fatalf("synthetic caption should not count as applied: %+v", unchanged)
	}
	yolo := MarkedElement{Name: "element_1", Type: "interactable", BBox: [4]int{40, 40, 20, 20}, Source: "yolo"}
	if ApplyCaption(&yolo, Caption{Type: "other"}) {
		t.Fatalf("type other without a real name should not relabel synthetic marks: %+v", yolo)
	}
	if yolo.Name != "element_1" || yolo.Type != "interactable" {
		t.Fatalf("synthetic mark mutated: %+v", yolo)
	}
	a11y := MarkedElement{Name: "", Type: "checkbox", Source: "accessibility", BBox: [4]int{0, 0, 20, 20}}
	if !ApplyCaption(&a11y, Caption{Name: "Remember me", Type: "button"}) {
		t.Fatal("a11y caption should still apply the visible name")
	}
	if a11y.Name != "Remember me" || a11y.Type != "checkbox" {
		t.Fatalf("a11y closed type clobbered: %+v", a11y)
	}
	geom := MarkedElement{Name: "element_1", Type: "icon", Source: "yolo", BBox: [4]int{0, 0, 24, 24}}
	if !ApplyCaption(&geom, Caption{Name: "Close", Type: "button"}) {
		t.Fatal("yolo geometry icon should yield to caption type")
	}
	if geom.Name != "Close" || geom.Type != "button" {
		t.Fatalf("yolo geometry icon not replaced: %+v", geom)
	}
}

func TestIsComputerUseToolSelectAndDrag(t *testing.T) {
	for _, name := range []string{"computer_select", "computer_scroll_into_view", "computer_drag"} {
		if !IsComputerUseTool(name) {
			t.Fatalf("expected %s", name)
		}
	}
}
