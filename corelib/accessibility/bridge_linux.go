//go:build linux

package accessibility

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// linuxBridge implements Bridge using python3 + pyatspi2 (AT-SPI D-Bus interface).
// This pragmatic approach avoids complex D-Bus interop from Go while still
// leveraging the full AT-SPI accessibility framework on Linux.
type linuxBridge struct{}

// NewBridge creates a Linux Bridge backed by AT-SPI via python3/pyatspi2.
func NewBridge() Bridge {
	return &linuxBridge{}
}

// atspiElement is the JSON shape returned by our python3 scripts.
type atspiElement struct {
	Role     string         `json:"role"`
	Name     string         `json:"name"`
	Value    string         `json:"value"`
	X        int            `json:"x"`
	Y        int            `json:"y"`
	Width    int            `json:"width"`
	Height   int            `json:"height"`
	Children []atspiElement `json:"children,omitempty"`
}

func (e *atspiElement) toElement() Element {
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

// runPy executes a python3 snippet and returns stdout.
// Returns ("", nil) if the command fails — graceful degradation.
func runPy(script string) (string, error) {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		// Graceful degradation: app may not expose accessibility info,
		// or pyatspi2 may not be installed.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func runPyStrict(script string) error {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
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
func (b *linuxBridge) EnumElements(windowTitle string) ([]Element, error) {
	safeTitle := strings.ReplaceAll(windowTitle, `"`, `\"`)
	script := fmt.Sprintf(`
import json, sys
try:
    import pyatspi
except ImportError:
    print("[]")
    sys.exit(0)

def enum_tree(obj, depth):
    if depth <= 0:
        return None
    try:
        role = obj.getRoleName()
        name = obj.name or ""
        value = ""
        try:
            vi = obj.queryValue()
            value = str(vi.currentValue)
        except (NotImplementedError, AttributeError):
            pass
        try:
            ti = obj.queryText()
            if ti:
                value = ti.getText(0, ti.characterCount)
        except (NotImplementedError, AttributeError):
            pass
        comp = None
        x, y, w, h = 0, 0, 0, 0
        try:
            comp = obj.queryComponent()
            bb = comp.getExtents(pyatspi.DESKTOP_COORDS)
            x, y, w, h = bb.x, bb.y, bb.width, bb.height
        except (NotImplementedError, AttributeError):
            pass
        node = {"role": role, "name": name, "value": value,
                "x": x, "y": y, "width": w, "height": h}
        kids = []
        for i in range(obj.childCount):
            try:
                child = obj.getChildAtIndex(i)
                if child:
                    c = enum_tree(child, depth - 1)
                    if c:
                        kids.append(c)
            except Exception:
                pass
        if kids:
            node["children"] = kids
        return node
    except Exception:
        return None

desktop = pyatspi.Registry.getDesktop(0)
target_title = "%s"
result = []
for i in range(desktop.childCount):
    try:
        app = desktop.getChildAtIndex(i)
        if not app:
            continue
        for j in range(app.childCount):
            try:
                win = app.getChildAtIndex(j)
                if win and win.name == target_title:
                    tree = enum_tree(win, 3)
                    if tree:
                        result.append(tree)
                    break
            except Exception:
                pass
    except Exception:
        pass

print(json.dumps(result))
`, safeTitle)

	out, err := runPy(script)
	if err != nil || out == "" || out == "[]" {
		return nil, nil
	}

	var parsed []atspiElement
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
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
func (b *linuxBridge) FindElement(windowTitle, role, name string) (*Element, error) {
	safeTitle := strings.ReplaceAll(windowTitle, `"`, `\"`)
	safeName := strings.ReplaceAll(name, `"`, `\"`)
	safeRole := strings.ReplaceAll(role, `"`, `\"`)

	script := fmt.Sprintf(`
import json, sys
try:
    import pyatspi
except ImportError:
    sys.exit(0)

def find_element(obj, target_role, target_name, depth):
    if depth <= 0:
        return None
    try:
        role = obj.getRoleName().lower()
        el_name = obj.name or ""
        if role == target_role.lower() and el_name == target_name:
            value = ""
            try:
                vi = obj.queryValue()
                value = str(vi.currentValue)
            except (NotImplementedError, AttributeError):
                pass
            try:
                ti = obj.queryText()
                if ti:
                    value = ti.getText(0, ti.characterCount)
            except (NotImplementedError, AttributeError):
                pass
            x, y, w, h = 0, 0, 0, 0
            try:
                comp = obj.queryComponent()
                bb = comp.getExtents(pyatspi.DESKTOP_COORDS)
                x, y, w, h = bb.x, bb.y, bb.width, bb.height
            except (NotImplementedError, AttributeError):
                pass
            return {"role": role, "name": el_name, "value": value,
                    "x": x, "y": y, "width": w, "height": h}
        for i in range(obj.childCount):
            try:
                child = obj.getChildAtIndex(i)
                if child:
                    found = find_element(child, target_role, target_name, depth - 1)
                    if found:
                        return found
            except Exception:
                pass
    except Exception:
        pass
    return None

desktop = pyatspi.Registry.getDesktop(0)
target_title = "%s"
target_role = "%s"
target_name = "%s"

for i in range(desktop.childCount):
    try:
        app = desktop.getChildAtIndex(i)
        if not app:
            continue
        for j in range(app.childCount):
            try:
                win = app.getChildAtIndex(j)
                if win and win.name == target_title:
                    found = find_element(win, target_role, target_name, 10)
                    if found:
                        print(json.dumps(found))
                    sys.exit(0)
            except Exception:
                pass
    except Exception:
        pass
`, safeTitle, safeRole, safeName)

	out, err := runPy(script)
	if err != nil || out == "" {
		return nil, nil
	}

	var p atspiElement
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return nil, nil
	}

	el := p.toElement()
	return &el, nil
}

// ClickElement attempts an AT-SPI action. It does not synthesize xdotool
// clicks — callers that want pixel fallback should do that themselves.
func (b *linuxBridge) ClickElement(el *Element) error {
	if el == nil {
		return nil
	}
	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2
	script := fmt.Sprintf(`
import sys
try:
    import pyatspi
except ImportError:
    sys.exit(1)
try:
    desktop = pyatspi.Registry.getDesktop(0)
    target = None
    for app in desktop:
        try:
            comp = app.queryComponent()
            target = comp.getAccessibleAtPoint(%d, %d, pyatspi.component.XY_SCREEN)
        except Exception:
            continue
        if target is not None:
            break
    if target is None:
        sys.exit(1)
    try:
        action = target.queryAction()
        for i in range(action.nActions):
            name = (action.getName(i) or "").lower()
            if name in ("click", "press", "activate"):
                action.doAction(i)
                sys.exit(0)
        if action.nActions > 0:
            action.doAction(0)
            sys.exit(0)
    except Exception:
        pass
    sys.exit(1)
except Exception:
    sys.exit(1)
`, cx, cy)
	return runPyStrict(script)
}

// TypeInElement tries AT-SPI text/value set. It does not type via xdotool.
func (b *linuxBridge) TypeInElement(el *Element, text string) error {
	if el == nil {
		return nil
	}
	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2
	safeText := strings.ReplaceAll(text, `\`, `\\`)
	safeText = strings.ReplaceAll(safeText, `"`, `\"`)
	script := fmt.Sprintf(`
import sys
try:
    import pyatspi
except ImportError:
    sys.exit(1)
text = "%s"
try:
    desktop = pyatspi.Registry.getDesktop(0)
    target = None
    for app in desktop:
        try:
            comp = app.queryComponent()
            target = comp.getAccessibleAtPoint(%d, %d, pyatspi.component.XY_SCREEN)
        except Exception:
            continue
        if target is not None:
            break
    if target is None:
        sys.exit(1)
    try:
        txt = target.queryText()
        txt.setTextContents(text)
        sys.exit(0)
    except Exception:
        pass
    try:
        target.setValue(text)
        sys.exit(0)
    except Exception:
        pass
    sys.exit(1)
except Exception:
    sys.exit(1)
`, safeText, cx, cy)
	return runPyStrict(script)
}

// GetValue returns the current value of the element using pyatspi2.
// Returns ("", nil) if the element doesn't expose a value.
func (b *linuxBridge) GetValue(el *Element) (string, error) {
	if el == nil {
		return "", nil
	}

	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2

	script := fmt.Sprintf(`
import sys
try:
    import pyatspi
except ImportError:
    sys.exit(0)

def get_value_at(x, y):
    desktop = pyatspi.Registry.getDesktop(0)
    for i in range(desktop.childCount):
        try:
            app = desktop.getChildAtIndex(i)
            if not app:
                continue
            for j in range(app.childCount):
                try:
                    win = app.getChildAtIndex(j)
                    if not win:
                        continue
                    found = find_at_point(win, x, y)
                    if found:
                        return found
                except Exception:
                    pass
        except Exception:
            pass
    return ""

def find_at_point(obj, x, y):
    try:
        comp = obj.queryComponent()
        if comp and comp.contains(x, y, pyatspi.DESKTOP_COORDS):
            # Try text interface first
            try:
                ti = obj.queryText()
                if ti and ti.characterCount > 0:
                    return ti.getText(0, ti.characterCount)
            except (NotImplementedError, AttributeError):
                pass
            # Try value interface
            try:
                vi = obj.queryValue()
                return str(vi.currentValue)
            except (NotImplementedError, AttributeError):
                pass
            # Recurse into children
            for i in range(obj.childCount):
                try:
                    child = obj.getChildAtIndex(i)
                    if child:
                        val = find_at_point(child, x, y)
                        if val:
                            return val
                except Exception:
                    pass
    except (NotImplementedError, AttributeError):
        pass
    return ""

val = get_value_at(%d, %d)
if val:
    print(val)
`, cx, cy)

	out, err := runPy(script)
	if err != nil {
		return "", nil
	}
	return out, nil
}

func (b *linuxBridge) SelectElement(el *Element) error {
	return b.ClickElement(el)
}

func (b *linuxBridge) ExpandElement(el *Element) error {
	return b.ClickElement(el)
}

func (b *linuxBridge) ScrollElementIntoView(el *Element) error {
	if el == nil {
		return nil
	}
	cx, cy := elementCenter(el)
	return runPyStrict(fmt.Sprintf(`
import sys
try:
    import pyatspi
except ImportError:
    sys.exit(1)
try:
    desktop = pyatspi.Registry.getDesktop(0)
    target = None
    for app in desktop:
        try:
            comp = app.queryComponent()
            target = comp.getAccessibleAtPoint(%d, %d, pyatspi.component.XY_SCREEN)
        except Exception:
            continue
        if target is not None:
            break
    if target is None:
        sys.exit(1)
    try:
        comp = target.queryComponent()
        comp.scrollTo(pyatspi.component.SCROLL_ANYWHERE)
        sys.exit(0)
    except Exception:
        sys.exit(1)
except Exception:
    sys.exit(1)
`, cx, cy))
}

func (b *linuxBridge) FocusElement(el *Element) error {
	if el == nil {
		return nil
	}
	cx, cy := elementCenter(el)
	return runPyStrict(fmt.Sprintf(`
import sys
try:
    import pyatspi
except ImportError:
    sys.exit(1)
try:
    desktop = pyatspi.Registry.getDesktop(0)
    target = None
    for app in desktop:
        try:
            comp = app.queryComponent()
            target = comp.getAccessibleAtPoint(%d, %d, pyatspi.component.XY_SCREEN)
        except Exception:
            continue
        if target is not None:
            break
    if target is None:
        sys.exit(1)
    try:
        target.queryComponent().grabFocus()
        sys.exit(0)
    except Exception:
        sys.exit(1)
except Exception:
    sys.exit(1)
`, cx, cy))
}

// Close releases resources. No persistent resources for the python3 approach.
func (b *linuxBridge) Close() {}
