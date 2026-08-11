package guiautomation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// ── test doubles ──

type fakeBridge struct {
	elements map[string][]accessibility.Element // windowTitle -> elements
	values   map[string]string                  // "role::name" -> value
}

func (b *fakeBridge) EnumElements(windowTitle string) ([]accessibility.Element, error) {
	if els, ok := b.elements[windowTitle]; ok {
		return els, nil
	}
	return nil, nil
}

func (b *fakeBridge) FindElement(windowTitle, role, name string) (*accessibility.Element, error) {
	els, ok := b.elements[windowTitle]
	if !ok {
		return nil, nil
	}
	for i := range els {
		if matchElement(&els[i], role, name) {
			return &els[i], nil
		}
		for j := range els[i].Children {
			if matchElement(&els[i].Children[j], role, name) {
				return &els[i].Children[j], nil
			}
		}
	}
	return nil, nil
}

func matchElement(el *accessibility.Element, role, name string) bool {
	roleMatch := role == "" || strings.EqualFold(el.Role, role)
	nameMatch := name == "" || el.Name == name
	return roleMatch && nameMatch
}

func (b *fakeBridge) ClickElement(el *accessibility.Element) error                { return nil }
func (b *fakeBridge) TypeInElement(el *accessibility.Element, text string) error   { return nil }
func (b *fakeBridge) GetValue(el *accessibility.Element) (string, error) {
	key := el.Role + "::" + el.Name
	if v, ok := b.values[key]; ok {
		return v, nil
	}
	return el.Value, nil
}
func (b *fakeBridge) Close() {}

type fakeOCR struct {
	results []taskengine.OCRResult
	err     error
}

func (o *fakeOCR) Recognize(pngBase64 string) ([]taskengine.OCRResult, error) {
	return o.results, o.err
}
func (o *fakeOCR) IsAvailable() bool { return true }
func (o *fakeOCR) Close()            {}

// ── tests ──

func TestVerify_TextContains_Pass(t *testing.T) {
	ocr := &fakeOCR{results: []taskengine.OCRResult{
		{Text: "Welcome to the app", Confidence: 0.95},
		{Text: "Login successful", Confidence: 0.90},
	}}
	obs := NewGUIStateObserver(nil, ocr, fakeScreenshot, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "text_contains", Pattern: "Login successful"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got fail: %+v", result.Details)
	}
}

func TestVerify_TextContains_Fail(t *testing.T) {
	ocr := &fakeOCR{results: []taskengine.OCRResult{
		{Text: "Welcome to the app", Confidence: 0.95},
	}}
	obs := NewGUIStateObserver(nil, ocr, fakeScreenshot, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "text_contains", Pattern: "Logout"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail, got pass")
	}
	if !strings.Contains(result.Details[0].Error, "does not contain") {
		t.Errorf("expected 'does not contain' error, got: %s", result.Details[0].Error)
	}
}

func TestVerify_TextContains_NoOCR(t *testing.T) {
	obs := NewGUIStateObserver(nil, nil, fakeScreenshot, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "text_contains", Pattern: "anything"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail when OCR not available")
	}
	if !strings.Contains(result.Details[0].Error, "OCR not available") {
		t.Errorf("expected OCR error, got: %s", result.Details[0].Error)
	}
}

func TestVerify_OcrContains_BackwardCompat(t *testing.T) {
	ocr := &fakeOCR{results: []taskengine.OCRResult{
		{Text: "Hello World", Confidence: 0.99},
	}}
	obs := NewGUIStateObserver(nil, ocr, fakeScreenshot, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "ocr_contains", Pattern: "Hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for backward-compat ocr_contains")
	}
}

func TestVerify_ElementExists_Pass(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{
			"Notepad": {
				{Role: "Edit", Name: "TextEditor", Bounds: accessibility.Rect{X: 10, Y: 50, Width: 780, Height: 500}},
			},
		},
	}
	obs := NewGUIStateObserver(bridge, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "element_exists", Selector: "Edit::TextEditor", Window: "Notepad"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got: %+v", result.Details)
	}
}

func TestVerify_ElementExists_Fail(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{
			"Notepad": {
				{Role: "Edit", Name: "TextEditor"},
			},
		},
	}
	obs := NewGUIStateObserver(bridge, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "element_exists", Selector: "Button::Save", Window: "Notepad"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for non-existent element")
	}
}

func TestVerify_ElementExists_NoBridge(t *testing.T) {
	obs := NewGUIStateObserver(nil, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "element_exists", Selector: "Button::OK"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail when bridge not available")
	}
}

func TestVerify_ElementValue_Pass(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{
			"Login": {
				{Role: "Edit", Name: "Username", Value: "admin", Bounds: accessibility.Rect{X: 100, Y: 200, Width: 200, Height: 30}},
			},
		},
		values: map[string]string{
			"Edit::Username": "admin",
		},
	}
	obs := NewGUIStateObserver(bridge, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "element_value", Selector: "Edit::Username", Window: "Login", Pattern: "admin"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got: %+v", result.Details)
	}
}

func TestVerify_ElementValue_Mismatch(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{
			"Login": {
				{Role: "Edit", Name: "Username", Bounds: accessibility.Rect{X: 100, Y: 200, Width: 200, Height: 30}},
			},
		},
		values: map[string]string{
			"Edit::Username": "guest",
		},
	}
	obs := NewGUIStateObserver(bridge, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "element_value", Selector: "Edit::Username", Window: "Login", Pattern: "admin"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for value mismatch")
	}
	if !strings.Contains(result.Details[0].Error, "does not contain") {
		t.Errorf("expected value mismatch error, got: %s", result.Details[0].Error)
	}
}

func TestVerify_WindowExists_Pass(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{
			"Untitled - Notepad": {
				{Role: "Window", Name: "Untitled - Notepad"},
			},
		},
	}
	obs := NewGUIStateObserver(bridge, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "window_exists", Pattern: "Untitled - Notepad"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got: %+v", result.Details)
	}
}

func TestVerify_WindowExists_Fail(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{},
	}
	obs := NewGUIStateObserver(bridge, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "window_exists", Pattern: "NonExistent"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for non-existent window")
	}
}

func TestVerify_MultipleCriteria_PartialFail(t *testing.T) {
	bridge := &fakeBridge{
		elements: map[string][]accessibility.Element{
			"Notepad": {
				{Role: "Edit", Name: "TextEditor"},
			},
		},
	}
	ocr := &fakeOCR{results: []taskengine.OCRResult{
		{Text: "Hello World"},
	}}
	obs := NewGUIStateObserver(bridge, ocr, fakeScreenshot, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "element_exists", Selector: "Edit::TextEditor", Window: "Notepad"},
		{Type: "text_contains", Pattern: "Goodbye"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected overall fail when one criterion fails")
	}
	if len(result.Details) != 2 {
		t.Fatalf("expected 2 details, got %d", len(result.Details))
	}
	if !result.Details[0].Passed {
		t.Error("first criterion (element_exists) should pass")
	}
	if result.Details[1].Passed {
		t.Error("second criterion (text_contains) should fail")
	}
}

func TestVerify_UnsupportedType(t *testing.T) {
	obs := NewGUIStateObserver(nil, nil, nil, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "magic_check", Pattern: "x"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for unsupported type")
	}
	if !strings.Contains(result.Details[0].Error, "unsupported") {
		t.Errorf("expected unsupported error, got: %s", result.Details[0].Error)
	}
}

func TestSnapshot_CollectsAvailableData(t *testing.T) {
	ocr := &fakeOCR{results: []taskengine.OCRResult{
		{Text: "Screen text"},
	}}
	obs := NewGUIStateObserver(nil, ocr, fakeScreenshot, nil)

	snap, err := obs.Snapshot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ScreenshotB64 == "" {
		t.Error("expected screenshot data")
	}
	if snap.OCRText == "" {
		t.Error("expected OCR text")
	}
}

func TestSnapshot_NoDependencies(t *testing.T) {
	obs := NewGUIStateObserver(nil, nil, nil, nil)

	snap, err := obs.Snapshot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap == nil {
		t.Fatal("expected non-nil snapshot even with no dependencies")
	}
}

func TestTakeCheckpoint(t *testing.T) {
	obs := NewGUIStateObserver(nil, nil, fakeScreenshot, nil)

	cp := obs.TakeCheckpoint(3)
	if cp.StepIndex != 3 {
		t.Errorf("expected step index 3, got %d", cp.StepIndex)
	}
	if cp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if cp.ScreenshotB64 == "" {
		t.Error("expected screenshot in checkpoint")
	}
}

func TestParseSelector(t *testing.T) {
	tests := []struct {
		selector     string
		fallbackName string
		wantRole     string
		wantName     string
	}{
		{"Button::OK", "", "Button", "OK"},
		{"Edit::Username", "", "Edit", "Username"},
		{"OK", "", "", "OK"},
		{"", "fallback", "", "fallback"},
		{"", "", "", ""},
	}
	for _, tt := range tests {
		role, name := parseSelector(tt.selector, tt.fallbackName)
		if role != tt.wantRole || name != tt.wantName {
			t.Errorf("parseSelector(%q, %q) = (%q, %q), want (%q, %q)",
				tt.selector, tt.fallbackName, role, name, tt.wantRole, tt.wantName)
		}
	}
}

func TestWaitForStable_NoScreenshot(t *testing.T) {
	obs := NewGUIStateObserver(nil, nil, nil, nil)
	err := obs.WaitForStable(100 * time.Millisecond)
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// ── helpers ──

func fakeScreenshot() (string, error) {
	return "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", nil
}

var _ = fmt.Sprintf

type countingOCR struct {
	calls   int
	results []taskengine.OCRResult
}

func (o *countingOCR) Recognize(string) ([]taskengine.OCRResult, error) {
	o.calls++
	return o.results, nil
}
func (o *countingOCR) IsAvailable() bool { return true }
func (o *countingOCR) Close()            {}

func TestVerify_MultipleTextContainsShareOneOCRPass(t *testing.T) {
	ocr := &countingOCR{results: []taskengine.OCRResult{
		{Text: "Welcome", Confidence: 0.9},
		{Text: "Login successful", Confidence: 0.9},
	}}
	obs := NewGUIStateObserver(nil, ocr, fakeScreenshot, nil)

	result, err := obs.Verify([]taskengine.CriterionSpec{
		{Type: "text_contains", Pattern: "Welcome"},
		{Type: "text_contains", Pattern: "Login successful"},
		{Type: "text_contains", Pattern: "Missing text"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Passed {
		t.Fatal("third criterion should fail")
	}
	if ocr.calls != 1 {
		t.Fatalf("Recognize called %d times for 3 criteria on one screenshot, want 1", ocr.calls)
	}
}
