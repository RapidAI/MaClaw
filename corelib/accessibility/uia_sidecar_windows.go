//go:build windows

package accessibility

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// uiaSidecar is a long-lived UIA process (prefer C# binary, fallback PowerShell).
// JSON line protocol: request -> response.
type uiaSidecar struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	ready   bool
	idle    *time.Timer
	backend string // "csharp" | "powershell"
}

var globalUIASidecar uiaSidecar

const uiaIdleUnload = 5 * time.Minute

// Embedded as EncodedCommand (UTF-16LE base64) at start time.
const uiaSidecarScript = `
$ErrorActionPreference = 'Stop'
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
    if ($depth -gt 1) {
        $kids = $el.FindAll([System.Windows.Automation.TreeScope]::Children,
            [System.Windows.Automation.Condition]::TrueCondition)
        if ($kids.Count -gt 0) {
            $node.children = @()
            foreach ($k in $kids) {
                $child = Get-UITree $k ($depth - 1)
                if ($child) { $node.children += $child }
            }
        }
    }
    return $node
}

function Enum-Windows {
    param($window, $depth)
    if ($depth -lt 1) { $depth = 1 }
    if ($depth -gt 5) { $depth = 5 }
    $root = [System.Windows.Automation.AutomationElement]::RootElement
    if ([string]::IsNullOrEmpty($window)) {
        $wins = $root.FindAll([System.Windows.Automation.TreeScope]::Children,
            [System.Windows.Automation.Condition]::TrueCondition)
        $list = @()
        foreach ($w in $wins) {
            $name = $w.Current.Name
            if ([string]::IsNullOrEmpty($name)) { continue }
            $rect = $w.Current.BoundingRectangle
            $list += @{
                role = 'Window'; name = $name; value = ''
                x = [int]$rect.X; y = [int]$rect.Y
                width = [int]$rect.Width; height = [int]$rect.Height
            }
        }
        return $list
    }
    $all = $root.FindAll([System.Windows.Automation.TreeScope]::Children,
        [System.Windows.Automation.Condition]::TrueCondition)
    $win = $null
    foreach ($w in $all) {
        if ($w.Current.Name -like ("*" + $window + "*")) { $win = $w; break }
    }
    if (-not $win) { return @() }
    $tree = Get-UITree $win $depth
    if ($tree) { return @($tree) }
    return @()
}

function Find-El {
    param($window, $role, $name)
    $root = [System.Windows.Automation.AutomationElement]::RootElement
    $all = $root.FindAll([System.Windows.Automation.TreeScope]::Children,
        [System.Windows.Automation.Condition]::TrueCondition)
    $win = $null
    foreach ($w in $all) {
        if ($w.Current.Name -like ("*" + $window + "*")) { $win = $w; break }
    }
    if (-not $win) { return $null }
    $ctMap = @{
        'button'=[System.Windows.Automation.ControlType]::Button
        'edit'=[System.Windows.Automation.ControlType]::Edit
        'textfield'=[System.Windows.Automation.ControlType]::Edit
        'text'=[System.Windows.Automation.ControlType]::Text
        'checkbox'=[System.Windows.Automation.ControlType]::CheckBox
        'combobox'=[System.Windows.Automation.ControlType]::ComboBox
        'list'=[System.Windows.Automation.ControlType]::List
        'listitem'=[System.Windows.Automation.ControlType]::ListItem
        'menu'=[System.Windows.Automation.ControlType]::Menu
        'menuitem'=[System.Windows.Automation.ControlType]::MenuItem
        'tab'=[System.Windows.Automation.ControlType]::Tab
        'tabitem'=[System.Windows.Automation.ControlType]::TabItem
        'window'=[System.Windows.Automation.ControlType]::Window
        'radiobutton'=[System.Windows.Automation.ControlType]::RadioButton
        'hyperlink'=[System.Windows.Automation.ControlType]::Hyperlink
    }
    $roleLower = $role.ToLower()
    $nameCond = New-Object System.Windows.Automation.PropertyCondition(
        [System.Windows.Automation.AutomationElement]::NameProperty, $name)
    $cond = $nameCond
    if ($ctMap.ContainsKey($roleLower)) {
        $typeCond = New-Object System.Windows.Automation.PropertyCondition(
            [System.Windows.Automation.AutomationElement]::ControlTypeProperty, $ctMap[$roleLower])
        $cond = New-Object System.Windows.Automation.AndCondition($typeCond, $nameCond)
    }
    $el = $win.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
    if (-not $el) { return $null }
    $rect = $el.Current.BoundingRectangle
    $val = ''
    try {
        $vp = $el.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
        if ($vp) { $val = $vp.Current.Value }
    } catch {}
    return @{
        role = $el.Current.ControlType.ProgrammaticName -replace 'ControlType\.', ''
        name = $el.Current.Name; value = $val
        x = [int]$rect.X; y = [int]$rect.Y
        width = [int]$rect.Width; height = [int]$rect.Height
    }
}

while ($true) {
    $line = [Console]::In.ReadLine()
    if ($null -eq $line) { break }
    if ($line.Trim() -eq '') { continue }
    try {
        $req = $line | ConvertFrom-Json
        $op = [string]$req.op
        switch ($op) {
            'ping' {
                @{ ok = $true; pong = $true } | ConvertTo-Json -Compress
            }
            'enum' {
                $depth = 3
                if ($req.depth) { $depth = [int]$req.depth }
                $els = Enum-Windows ([string]$req.window) $depth
                @{ ok = $true; elements = @($els) } | ConvertTo-Json -Depth 12 -Compress
            }
            'find' {
                $el = Find-El ([string]$req.window) ([string]$req.role) ([string]$req.name)
                if ($el) {
                    @{ ok = $true; element = $el } | ConvertTo-Json -Compress
                } else {
                    @{ ok = $true; element = $null } | ConvertTo-Json -Compress
                }
            }
            default {
                @{ ok = $false; error = ("unknown op: " + $op) } | ConvertTo-Json -Compress
            }
        }
    } catch {
        (@{ ok = $false; error = $_.Exception.Message } | ConvertTo-Json -Compress)
    }
}
`

type uiaResponse struct {
	OK       bool        `json:"ok"`
	Error    string      `json:"error"`
	Elements []psElement `json:"elements"`
	Element  *psElement  `json:"element"`
	Pong     bool        `json:"pong"`
}

func encodePowerShellCommand(script string) string {
	// -EncodedCommand expects UTF-16LE base64 (including BOM-less little-endian).
	u16 := utf16.Encode([]rune(script))
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func (s *uiaSidecar) stop() {
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
		s.stdin = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		s.cmd = nil
	}
	s.scanner = nil
	s.ready = false
}

func (s *uiaSidecar) resetIdle() {
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(uiaIdleUnload, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.ready {
			s.stop()
		}
	})
}

func (s *uiaSidecar) ensure() error {
	if s.ready {
		return nil
	}
	return s.start()
}

func (s *uiaSidecar) start() error {
	s.stop()
	// Prefer compiled C# helper when available (auto-built via Framework csc).
	if err := s.startProcess(uiaCSharpSidecarPath(), nil, "csharp"); err == nil {
		return nil
	}
	// Fallback: long-lived PowerShell with UIAutomation loaded once.
	psExe, err := coretool.ResolveWindowsPowerShell()
	if err != nil {
		return err
	}
	encoded := encodePowerShellCommand(uiaSidecarScript)
	return s.startProcess(psExe, []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", encoded}, "powershell")
}

func (s *uiaSidecar) startProcess(exe string, args []string, backend string) error {
	if strings.TrimSpace(exe) == "" {
		return fmt.Errorf("empty exe")
	}
	var cmd *exec.Cmd
	if len(args) == 0 {
		cmd = coretool.Command(exe)
	} else {
		cmd = coretool.Command(exe, args...)
	}
	coretool.HideCommandWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.scanner = bufio.NewScanner(stdout)
	s.scanner.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)

	req, _ := json.Marshal(map[string]string{"op": "ping"})
	if _, err := fmt.Fprintf(stdin, "%s\n", req); err != nil {
		s.stop()
		return fmt.Errorf("uia sidecar write: %w", err)
	}
	if !s.scanner.Scan() {
		s.stop()
		return fmt.Errorf("uia sidecar no ping response (%s)", backend)
	}
	var resp uiaResponse
	if err := json.Unmarshal(s.scanner.Bytes(), &resp); err != nil || !resp.OK {
		s.stop()
		return fmt.Errorf("uia sidecar bad ping (%s): %v %s", backend, err, strings.TrimSpace(string(s.scanner.Bytes())))
	}
	s.ready = true
	s.backend = backend
	s.resetIdle()
	return nil
}

func (s *uiaSidecar) call(req map[string]interface{}) (*uiaResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(s.stdin, "%s\n", body); err != nil {
		s.stop()
		return nil, err
	}
	if !s.scanner.Scan() {
		err := s.scanner.Err()
		s.stop()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("uia sidecar closed")
	}
	var resp uiaResponse
	if err := json.Unmarshal(s.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("uia sidecar decode: %w body=%q", err, s.scanner.Text())
	}
	if !resp.OK {
		return &resp, fmt.Errorf("uia sidecar: %s", resp.Error)
	}
	s.resetIdle()
	return &resp, nil
}

func (s *uiaSidecar) enum(window string, depth int) ([]Element, error) {
	resp, err := s.call(map[string]interface{}{
		"op":     "enum",
		"window": window,
		"depth":  depth,
	})
	if err != nil {
		// Restart once on transport failure, then surface error for fallback.
		s.mu.Lock()
		s.stop()
		s.mu.Unlock()
		resp, err = s.call(map[string]interface{}{
			"op":     "enum",
			"window": window,
			"depth":  depth,
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]Element, len(resp.Elements))
	for i, p := range resp.Elements {
		out[i] = p.toElement()
	}
	return out, nil
}

func (s *uiaSidecar) find(window, role, name string) (*Element, error) {
	resp, err := s.call(map[string]interface{}{
		"op":     "find",
		"window": window,
		"role":   role,
		"name":   name,
	})
	if err != nil {
		s.mu.Lock()
		s.stop()
		s.mu.Unlock()
		resp, err = s.call(map[string]interface{}{
			"op":     "find",
			"window": window,
			"role":   role,
			"name":   name,
		})
		if err != nil {
			return nil, err
		}
	}
	if resp.Element == nil {
		return nil, nil
	}
	el := resp.Element.toElement()
	return &el, nil
}

// UIASidecarAlive reports whether the long-lived accessibility process is running.
// Used by diagnostics / GetComputerUseStatus.
func UIASidecarAlive() bool {
	globalUIASidecar.mu.Lock()
	defer globalUIASidecar.mu.Unlock()
	return globalUIASidecar.ready
}

// UIASidecarBackend returns "csharp", "powershell", or "" when not running.
func UIASidecarBackend() string {
	return UIABackend()
}
