//go:build windows

package accessibility

import (
	"encoding/json"
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// windowsBridge implements Bridge using PowerShell + System.Windows.Automation.
// This pragmatic approach avoids complex COM interop from Go while still
// leveraging the full UI Automation framework on Windows.
type windowsBridge struct{}

// NewBridge creates a Windows Bridge backed by UI Automation via PowerShell.
func NewBridge() Bridge {
	return &windowsBridge{}
}

// psElement is the JSON shape returned by our PowerShell scripts.
type psElement struct {
	Role     string      `json:"role"`
	Name     string      `json:"name"`
	Value    string      `json:"value"`
	X        int         `json:"x"`
	Y        int         `json:"y"`
	Width    int         `json:"width"`
	Height   int         `json:"height"`
	Children []psElement `json:"children,omitempty"`
}

func (e *psElement) toElement() Element {
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

// runPS executes a PowerShell snippet and returns stdout.
// Returns ("", nil) if the command fails — graceful degradation.
func runPS(script string) (string, error) {
	cmd := coretool.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		// Graceful degradation: app may not expose accessibility info.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// EnumElements returns the accessibility tree for the window matching the given title.
// If the window is not found or has no accessibility info, returns (nil, nil).
func (b *windowsBridge) EnumElements(windowTitle string) ([]Element, error) {
	// PowerShell script that uses UI Automation to enumerate the element tree.
	// We limit depth to 3 levels to avoid huge trees.
	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

function Get-UITree {
    param($el, $depth)
    if ($depth -le 0) { return $null }
    $rect = $el.Current.BoundingRectangle
    $node = @{
        role   = $el.Current.ControlType.ProgrammaticName -replace 'ControlType\.', ''
        name   = $el.Current.Name
        value  = ''
        x      = [int]$rect.X
        y      = [int]$rect.Y
        width  = [int]$rect.Width
        height = [int]$rect.Height
    }
    try {
        $vp = $el.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
        if ($vp) { $node.value = $vp.Current.Value }
    } catch {}
    $kids = $el.FindAll([System.Windows.Automation.TreeScope]::Children,
        [System.Windows.Automation.Condition]::TrueCondition)
    if ($kids.Count -gt 0) {
        $node.children = @()
        foreach ($k in $kids) {
            $child = Get-UITree $k ($depth - 1)
            if ($child) { $node.children += $child }
        }
    }
    return $node
}

$root = [System.Windows.Automation.AutomationElement]::RootElement
$cond = New-Object System.Windows.Automation.PropertyCondition(
    [System.Windows.Automation.AutomationElement]::NameProperty, %q)
$win = $root.FindFirst([System.Windows.Automation.TreeScope]::Children, $cond)
if (-not $win) { Write-Output '[]'; exit 0 }

$tree = Get-UITree $win 3
if ($tree) {
    ConvertTo-Json @($tree) -Depth 10 -Compress
} else {
    Write-Output '[]'
}
`, windowTitle)

	out, err := runPS(script)
	if err != nil || out == "" || out == "[]" {
		return nil, nil
	}

	var parsed []psElement
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
func (b *windowsBridge) FindElement(windowTitle, role, name string) (*Element, error) {
	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$root = [System.Windows.Automation.AutomationElement]::RootElement
$winCond = New-Object System.Windows.Automation.PropertyCondition(
    [System.Windows.Automation.AutomationElement]::NameProperty, %q)
$win = $root.FindFirst([System.Windows.Automation.TreeScope]::Children, $winCond)
if (-not $win) { exit 0 }

$ctMap = @{
    'button'    = [System.Windows.Automation.ControlType]::Button
    'edit'      = [System.Windows.Automation.ControlType]::Edit
    'textfield' = [System.Windows.Automation.ControlType]::Edit
    'text'      = [System.Windows.Automation.ControlType]::Text
    'checkbox'  = [System.Windows.Automation.ControlType]::CheckBox
    'combobox'  = [System.Windows.Automation.ControlType]::ComboBox
    'list'      = [System.Windows.Automation.ControlType]::List
    'listitem'  = [System.Windows.Automation.ControlType]::ListItem
    'menu'      = [System.Windows.Automation.ControlType]::Menu
    'menuitem'  = [System.Windows.Automation.ControlType]::MenuItem
    'tab'       = [System.Windows.Automation.ControlType]::Tab
    'tabitem'   = [System.Windows.Automation.ControlType]::TabItem
    'tree'      = [System.Windows.Automation.ControlType]::Tree
    'treeitem'  = [System.Windows.Automation.ControlType]::TreeItem
    'window'    = [System.Windows.Automation.ControlType]::Window
    'radiobutton' = [System.Windows.Automation.ControlType]::RadioButton
    'slider'    = [System.Windows.Automation.ControlType]::Slider
    'hyperlink' = [System.Windows.Automation.ControlType]::Hyperlink
}

$roleLower = %q.ToLower()
$nameCond = New-Object System.Windows.Automation.PropertyCondition(
    [System.Windows.Automation.AutomationElement]::NameProperty, %q)

$cond = $nameCond
if ($ctMap.ContainsKey($roleLower)) {
    $typeCond = New-Object System.Windows.Automation.PropertyCondition(
        [System.Windows.Automation.AutomationElement]::ControlTypeProperty, $ctMap[$roleLower])
    $cond = New-Object System.Windows.Automation.AndCondition($typeCond, $nameCond)
}

$el = $win.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
if (-not $el) { exit 0 }

$rect = $el.Current.BoundingRectangle
$val = ''
try {
    $vp = $el.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
    if ($vp) { $val = $vp.Current.Value }
} catch {}

$obj = @{
    role   = $el.Current.ControlType.ProgrammaticName -replace 'ControlType\.', ''
    name   = $el.Current.Name
    value  = $val
    x      = [int]$rect.X
    y      = [int]$rect.Y
    width  = [int]$rect.Width
    height = [int]$rect.Height
}
ConvertTo-Json $obj -Compress
`, windowTitle, role, name)

	out, err := runPS(script)
	if err != nil || out == "" {
		return nil, nil
	}

	var p psElement
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return nil, nil
	}

	el := p.toElement()
	return &el, nil
}

// ClickElement performs a click on the element.
// First tries InvokePattern (for buttons), then falls back to simulating
// a mouse click at the element's center coordinates.
func (b *windowsBridge) ClickElement(el *Element) error {
	if el == nil {
		return nil
	}

	// Try InvokePattern first if we have bounds to identify the element,
	// otherwise fall back to coordinate-based click.
	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

# Try to find the element at the point and use InvokePattern
try {
    $pt = New-Object System.Windows.Point(%d, %d)
    $el = [System.Windows.Automation.AutomationElement]::FromPoint($pt)
    if ($el) {
        try {
            $ip = $el.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
            if ($ip) { $ip.Invoke(); exit 0 }
        } catch {}
    }
} catch {}

# Fallback: simulate mouse click via cursor position + SendInput
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)
Start-Sleep -Milliseconds 50

$sig = @'
[DllImport("user32.dll")] public static extern void mouse_event(int dwFlags, int dx, int dy, int dwData, int dwExtraInfo);
'@
$u = Add-Type -MemberDefinition $sig -Name WinAPI -Namespace ClickSim -PassThru
$u::mouse_event(0x0002, 0, 0, 0, 0)  # MOUSEEVENTF_LEFTDOWN
$u::mouse_event(0x0004, 0, 0, 0, 0)  # MOUSEEVENTF_LEFTUP
`, cx, cy, cx, cy)

	_, err := runPS(script)
	return err
}

// TypeInElement types text into the element.
// First tries ValuePattern.SetValue, then falls back to SendKeys.
func (b *windowsBridge) TypeInElement(el *Element, text string) error {
	if el == nil {
		return nil
	}

	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2

	// Escape text for PowerShell string embedding.
	safeText := strings.ReplaceAll(text, "'", "''")

	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

try {
    $pt = New-Object System.Windows.Point(%d, %d)
    $el = [System.Windows.Automation.AutomationElement]::FromPoint($pt)
    if ($el) {
        try {
            $vp = $el.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
            if ($vp) { $vp.SetValue('%s'); exit 0 }
        } catch {}
    }
} catch {}

# Fallback: click to focus then SendKeys
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d, %d)
Start-Sleep -Milliseconds 50
$sig = @'
[DllImport("user32.dll")] public static extern void mouse_event(int dwFlags, int dx, int dy, int dwData, int dwExtraInfo);
'@
$u = Add-Type -MemberDefinition $sig -Name WinAPI2 -Namespace TypeSim -PassThru
$u::mouse_event(0x0002, 0, 0, 0, 0)
$u::mouse_event(0x0004, 0, 0, 0, 0)
Start-Sleep -Milliseconds 100
[System.Windows.Forms.SendKeys]::SendWait('%s')
`, cx, cy, safeText, cx, cy, safeText)

	_, err := runPS(script)
	return err
}

// GetValue returns the current value of the element using ValuePattern.
// Returns ("", nil) if the element doesn't support ValuePattern.
func (b *windowsBridge) GetValue(el *Element) (string, error) {
	if el == nil {
		return "", nil
	}

	cx := el.Bounds.X + el.Bounds.Width/2
	cy := el.Bounds.Y + el.Bounds.Height/2

	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

try {
    $pt = New-Object System.Windows.Point(%d, %d)
    $el = [System.Windows.Automation.AutomationElement]::FromPoint($pt)
    if ($el) {
        try {
            $vp = $el.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
            if ($vp) { Write-Output $vp.Current.Value; exit 0 }
        } catch {}
    }
} catch {}
`, cx, cy)

	out, err := runPS(script)
	if err != nil {
		return "", nil
	}
	return out, nil
}

// Close releases resources. No persistent resources for the PowerShell approach.
func (b *windowsBridge) Close() {}
