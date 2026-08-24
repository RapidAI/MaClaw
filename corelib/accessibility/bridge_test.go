package accessibility

import "testing"

func TestNewBridgeReturnsNonNil(t *testing.T) {
	b := NewBridge()
	if b == nil {
		t.Fatal("NewBridge() returned nil")
	}
}

func TestNoopBridgeEnumElements(t *testing.T) {
	b := &noopBridge{}
	elems, err := b.EnumElements("any window")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elems != nil {
		t.Fatalf("expected nil elements, got %v", elems)
	}
}

func TestNoopBridgeFindElement(t *testing.T) {
	b := &noopBridge{}
	el, err := b.FindElement("win", "button", "OK")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if el != nil {
		t.Fatalf("expected nil element, got %v", el)
	}
}

func TestNoopBridgeClickElement(t *testing.T) {
	b := &noopBridge{}
	if err := b.ClickElement(&Element{}); err == nil {
		t.Fatal("expected unsupported-platform error")
	}
}

func TestNoopBridgeTypeInElement(t *testing.T) {
	b := &noopBridge{}
	if err := b.TypeInElement(&Element{}, "hello"); err == nil {
		t.Fatal("expected unsupported-platform error")
	}
}

func TestNoopBridgeGetValue(t *testing.T) {
	b := &noopBridge{}
	val, err := b.GetValue(&Element{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty string, got %q", val)
	}
}

func TestNoopBridgeClose(t *testing.T) {
	b := &noopBridge{}
	b.Close() // should not panic
}
