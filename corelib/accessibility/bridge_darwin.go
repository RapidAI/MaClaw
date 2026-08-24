//go:build darwin

package accessibility

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// darwinBridge implements Bridge using python3 + pyobjc Accessibility APIs.
// This pragmatic approach avoids complex CGo with Accessibility framework
// while still leveraging the full macOS Accessibility API.
type darwinBridge struct{}

// NewBridge creates a macOS Bridge backed by Accessibility API via python3/pyobjc.
func NewBridge() Bridge {
	return &darwinBridge{}
}

// pyElement is the JSON shape returned by our python3 scripts.
type pyElement struct {
	Role     string      `json:"role"`
	Name     string      `json:"name"`
	Value    string      `json:"value"`
	X        int         `json:"x"`
	Y        int         `json:"y"`
	Width    int         `json:"width"`
	Height   int         `json:"height"`
	Children []pyElement `json:"children,omitempty"`
}

func (e *pyElement) toElement() Element {
	el := Element{
		Role:  e.Role,
		Name:  e.Name,
		Value: e.Value,
		Bounds: Rect{
			X:      e.X,
			Y:      e.Y,
			Width:  e.Width,
			Height: e.Height,
		},
	}
	for _, c := range e.Children {
		el.Children = append(el.Children, c.toElement())
	}
	return el
}

// runPython executes a python3 snippet and returns stdout.
// Returns ("", nil) if the command fails — graceful degradation.
func runPython(script string) (string, error) {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		// Check if this is a permission error.
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if classifyAccessibilityErrorMessage(stderr) == accessibilityErrorPermissionDenied {
				return "", fmt.Errorf("%s", accessibilityPermissionDeniedMessage)
			}
		}
		// Graceful degradation: app may not expose accessibility info.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func runPythonStrict(script string) error {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if classifyAccessibilityErrorMessage(stderr) == accessibilityErrorPermissionDenied {
				return fmt.Errorf("%s", accessibilityPermissionDeniedMessage)
			}
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("accessibility action failed: %w", err)
		}
		return fmt.Errorf("accessibility action failed: %w (%s)", err, msg)
	}
	return nil
}

// EnumElements returns the accessibility tree for the window matching the given title.
// If the window is not found or has no accessibility info, returns (nil, nil).
func (b *darwinBridge) EnumElements(windowTitle string) ([]Element, error) {
	// Python script that uses AppKit + Accessibility API to enumerate the element tree.
	// We limit depth to 3 levels to avoid huge trees.
	safeTitle := strings.ReplaceAll(windowTitle, `"`, `\"`)
	script := fmt.Sprintf(`
import json, sys
try:
    from AppKit import NSWorkspace
    from ApplicationServices import (
        AXUIElementCreateApplication,
        AXUIElementCopyAttributeValue,
        AXUIElementCopyAttributeNames,
    )
    from CoreFoundation import CFRange
except ImportError:
    print("[]")
    sys.exit(0)

def get_attr(el, attr):
    err, val = AXUIElementCopyAttributeValue(el, attr, None)
    if err == 0 and val is not None:
        return val
    return None

def get_str(el, attr):
    v = get_attr(el, attr)
    if v is not None:
        return str(v)
    return ""

def get_position(el):
    pos = get_attr(el, "AXPosition")
    if pos is not None:
        return int(pos.x), int(pos.y)
    return 0, 0

def get_size(el):
    sz = get_attr(el, "AXSize")
    if sz is not None:
        return int(sz.width), int(sz.height)
    return 0, 0

def enum_tree(el, depth):
    if depth <= 0:
        return None
    role = get_str(el, "AXRole").replace("AX", "")
    name = get_str(el, "AXTitle")
    if not name:
        name = get_str(el, "AXDescription")
    value = get_str(el, "AXValue")
    x, y = get_position(el)
    w, h = get_size(el)
    node = {"role": role, "name": name, "value": value, "x": x, "y": y, "width": w, "height": h}
    children_ref = get_attr(el, "AXChildren")
    if children_ref and len(children_ref) > 0:
        kids = []
        for child in children_ref:
            c = enum_tree(child, depth - 1)
            if c:
                kids.append(c)
        if kids:
            node["children"] = kids
    return node

# Find the app by window title
target_title = "%s"
apps = NSWorkspace.sharedWorkspace().runningApplications()
found_pid = None
for app in apps:
    pid = app.processIdentifier()
    ax_app = AXUIElementCreateApplication(pid)
    windows = get_attr(ax_app, "AXWindows")
    if windows:
        for win in windows:
            title = get_str(win, "AXTitle")
            if title == target_title:
                found_pid = pid
                break
    if found_pid:
        break

if not found_pid:
    print("[]")
    sys.exit(0)

ax_app = AXUIElementCreateApplication(found_pid)
windows = get_attr(ax_app, "AXWindows")
if not windows:
    print("[]")
    sys.exit(0)

result = []
for win in windows:
    title = get_str(win, "AXTitle")
    if title == target_title:
        tree = enum_tree(win, 3)
        if tree:
            result.append(tree)
        break

print(json.dumps(result))
`, safeTitle)

	out, err := runPython(script)
	if err != nil {
		// Permission error — propagate it.
		if isAccessibilityPermissionDenied(err) {
			return nil, err
		}
		return nil, nil
	}
	if out == "" || out == "[]" {
		return nil, nil
	}

	var parsed []pyElement
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		// Malformed output — degrade gracefully.
		return nil, nil
	}

	result := make([]Element, len(parsed))
	for i, p := range parsed {
		result[i] = p.toElement()
	}
	return result, nil
}

// FindElement searches for an element by role and name in the given window.
// Returns (nil, nil) if not found — allows callers to degrade to other strategies.
func (b *darwinBridge) FindElement(windowTitle, role, name string) (*Element, error) {
	safeTitle := strings.ReplaceAll(windowTitle, `"`, `\"`)
	safeName := strings.ReplaceAll(name, `"`, `\"`)
	safeRole := strings.ReplaceAll(role, `"`, `\"`)

	script := fmt.Sprintf(`
import json, sys
try:
    from AppKit import NSWorkspace
    from ApplicationServices import (
        AXUIElementCreateApplication,
        AXUIElementCopyAttributeValue,
    )
except ImportError:
    sys.exit(0)

def get_attr(el, attr):
    err, val = AXUIElementCopyAttributeValue(el, attr, None)
    if err == 0 and val is not None:
        return val
    return None

def get_str(el, attr):
    v = get_attr(el, attr)
    if v is not None:
        return str(v)
    return ""

def get_position(el):
    pos = get_attr(el, "AXPosition")
    if pos is not None:
        return int(pos.x), int(pos.y)
    return 0, 0

def get_size(el):
    sz = get_attr(el, "AXSize")
    if sz is not None:
        return int(sz.width), int(sz.height)
    return 0, 0

def find_element(el, target_role, target_name, depth):
    if depth <= 0:
        return None
    role = get_str(el, "AXRole").replace("AX", "").lower()
    title = get_str(el, "AXTitle")
    desc = get_str(el, "AXDescription")
    if role == target_role.lower() and (title == target_name or desc == target_name):
        value = get_str(el, "AXValue")
        x, y = get_position(el)
        w, h = get_size(el)
        return {"role": role, "name": title or desc, "value": value,
                "x": x, "y": y, "width": w, "height": h}
    children = get_attr(el, "AXChildren")
    if children:
        for child in children:
            result = find_element(child, target_role, target_name, depth - 1)
            if result:
                return result
    return None

target_title = "%s"
target_role = "%s"
target_name = "%s"

apps = NSWorkspace.sharedWorkspace().runningApplications()
for app in apps:
    pid = app.processIdentifier()
    ax_app = AXUIElementCreateApplication(pid)
    windows = get_attr(ax_app, "AXWindows")
    if windows:
        for win in windows:
            title = get_str(win, "AXTitle")
            if title == target_title:
                found = find_element(win, target_role, target_name, 10)
                if found:
                    print(json.dumps(found))
                    sys.exit(0)
                sys.exit(0)
`, safeTitle, safeRole, safeName)

	out, err := runPython(script)
	if err != nil {
		if isAccessibilityPermissionDenied(err) {
			return nil, err
		}
		return nil, nil
	}
	if out == "" {
		return nil, nil
	}

	var p pyElement
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return nil, nil
	}

	el := p.toElement()
	return &el, nil
}

// ClickElement tries AXPress on the element at the given bounds. It does not
// synthesize a Quartz mouse click — callers that want pixel fallback should
// do that themselves.
func (b *darwinBridge) ClickElement(el *Element) error {
	if el == nil {
		return nil
	}

	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2
	if ok, err := globalAXSidecar.actAt("press_at", cx, cy, ""); err == nil && ok {
		return nil
	}

	script := fmt.Sprintf(`
import sys
try:
    from ApplicationServices import (
        AXUIElementCreateSystemWide,
        AXUIElementCopyElementAtPosition,
        AXUIElementPerformAction,
    )
except ImportError:
    sys.exit(1)

sys_wide = AXUIElementCreateSystemWide()
err, target = AXUIElementCopyElementAtPosition(sys_wide, float(%d), float(%d), None)
if err != 0 or target is None:
    sys.exit(1)
r = AXUIElementPerformAction(target, "AXPress")
sys.exit(0 if r == 0 else 1)
`, cx, cy)

	return runPythonStrict(script)
}

// TypeInElement sets AXValue on the focused element after pointing at bounds.
// It does not type via CGEvent — callers that want keyboard fallback should
// do that themselves.
func (b *darwinBridge) TypeInElement(el *Element, text string) error {
	if el == nil {
		return nil
	}

	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2
	if ok, err := globalAXSidecar.actAt("set_value_at", cx, cy, text); err == nil && ok {
		return nil
	}

	safeText := strings.ReplaceAll(text, `\`, `\\`)
	safeText = strings.ReplaceAll(safeText, `"`, `\"`)

	script := fmt.Sprintf(`
import sys
try:
    from ApplicationServices import (
        AXUIElementCreateSystemWide,
        AXUIElementCopyElementAtPosition,
        AXUIElementSetAttributeValue,
    )
except ImportError:
    sys.exit(1)

text = "%s"
sys_wide = AXUIElementCreateSystemWide()
err, target = AXUIElementCopyElementAtPosition(sys_wide, float(%d), float(%d), None)
if err != 0 or target is None:
    sys.exit(1)
r = AXUIElementSetAttributeValue(target, "AXValue", text)
sys.exit(0 if r == 0 else 1)
`, safeText, cx, cy)

	return runPythonStrict(script)
}

// GetValue returns the current value of the element using AXValue attribute.
// Returns ("", nil) if the element doesn't expose AXValue.
func (b *darwinBridge) GetValue(el *Element) (string, error) {
	if el == nil {
		return "", nil
	}

	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2

	script := fmt.Sprintf(`
import sys
try:
    from ApplicationServices import (
        AXUIElementCreateSystemWide, AXUIElementCopyAttributeValue,
        AXUIElementCreateApplication,
    )
    from AppKit import NSWorkspace
    from Quartz import (
        CGEventCreateMouseEvent, CGEventPost,
        kCGEventLeftMouseDown, kCGEventLeftMouseUp,
        kCGMouseButtonLeft, kCGHIDEventTap,
    )
    from CoreFoundation import CGPointMake
except ImportError:
    sys.exit(0)

# Use system-wide element to find element at point.
sys_wide = AXUIElementCreateSystemWide()
err, el = AXUIElementCopyAttributeValue(sys_wide, "AXFocusedUIElement", None)
if err != 0 or el is None:
    # Try clicking to focus first.
    point = CGPointMake(%d, %d)
    down = CGEventCreateMouseEvent(None, kCGEventLeftMouseDown, point, kCGMouseButtonLeft)
    up = CGEventCreateMouseEvent(None, kCGEventLeftMouseUp, point, kCGMouseButtonLeft)
    CGEventPost(kCGHIDEventTap, down)
    CGEventPost(kCGHIDEventTap, up)
    import time; time.sleep(0.1)
    err, el = AXUIElementCopyAttributeValue(sys_wide, "AXFocusedUIElement", None)

if err == 0 and el is not None:
    err2, val = AXUIElementCopyAttributeValue(el, "AXValue", None)
    if err2 == 0 and val is not None:
        print(str(val))
`, cx, cy)

	out, err := runPython(script)
	if err != nil {
		return "", nil
	}
	return out, nil
}

func (b *darwinBridge) SelectElement(el *Element) error {
	return b.ClickElement(el)
}

func (b *darwinBridge) ExpandElement(el *Element) error {
	return b.ClickElement(el)
}

func (b *darwinBridge) ScrollElementIntoView(el *Element) error {
	if el == nil {
		return nil
	}
	cx, cy := elementCenter(el)
	if ok, err := globalAXSidecar.actAt("scroll_into_view_at", cx, cy, ""); err == nil && ok {
		return nil
	}
	return runPythonStrict(fmt.Sprintf(`
import sys
try:
    from ApplicationServices import (
        AXUIElementCreateSystemWide,
        AXUIElementCopyElementAtPosition,
        AXUIElementPerformAction,
    )
except ImportError:
    sys.exit(1)
sys_wide = AXUIElementCreateSystemWide()
err, target = AXUIElementCopyElementAtPosition(sys_wide, float(%d), float(%d), None)
if err != 0 or target is None:
    sys.exit(1)
r = AXUIElementPerformAction(target, "AXScrollToVisible")
sys.exit(0 if r == 0 else 1)
`, cx, cy))
}

func (b *darwinBridge) FocusElement(el *Element) error {
	if el == nil {
		return nil
	}
	cx, cy := elementCenter(el)
	if ok, err := globalAXSidecar.actAt("focus_at", cx, cy, ""); err == nil && ok {
		return nil
	}
	return runPythonStrict(fmt.Sprintf(`
import sys
try:
    from ApplicationServices import (
        AXUIElementCreateSystemWide,
        AXUIElementCopyElementAtPosition,
        AXUIElementSetAttributeValue,
    )
except ImportError:
    sys.exit(1)
sys_wide = AXUIElementCreateSystemWide()
err, target = AXUIElementCopyElementAtPosition(sys_wide, float(%d), float(%d), None)
if err != 0 or target is None:
    sys.exit(1)
r = AXUIElementSetAttributeValue(target, "AXFocused", True)
sys.exit(0 if r == 0 else 1)
`, cx, cy))
}

// Close releases resources. No persistent resources for the python3 approach.
func (b *darwinBridge) Close() {}
