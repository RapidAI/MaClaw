//go:build darwin

package accessibility

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type axSidecar struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	ready   bool
	idle    *time.Timer
}

var globalAXSidecar axSidecar

const axIdleUnload = 5 * time.Minute

const axSidecarPy = `
import json, sys
try:
    from ApplicationServices import (
        AXUIElementCreateSystemWide,
        AXUIElementCopyElementAtPosition,
        AXUIElementPerformAction,
        AXUIElementSetAttributeValue,
    )
except Exception as e:
    sys.stderr.write("ax import failed: %s\n" % e)
    sys.exit(1)

def ok(extra=None):
    o = {"ok": True}
    if extra:
        o.update(extra)
    print(json.dumps(o), flush=True)

def err(msg):
    print(json.dumps({"ok": False, "error": str(msg)}), flush=True)

def target_at(x, y):
    sys_wide = AXUIElementCreateSystemWide()
    e, t = AXUIElementCopyElementAtPosition(sys_wide, float(x), float(y), None)
    if e != 0 or t is None:
        return None
    return t

def press_at(x, y):
    t = target_at(x, y)
    if t is None:
        return False, "none"
    r = AXUIElementPerformAction(t, "AXPress")
    return r == 0, "press"

def set_value_at(x, y, text):
    t = target_at(x, y)
    if t is None:
        return False, "none"
    r = AXUIElementSetAttributeValue(t, "AXValue", text or "")
    return r == 0, "set_value"

def focus_at(x, y):
    t = target_at(x, y)
    if t is None:
        return False, "none"
    r = AXUIElementSetAttributeValue(t, "AXFocused", True)
    return r == 0, "focus"

def scroll_at(x, y):
    t = target_at(x, y)
    if t is None:
        return False, "none"
    r = AXUIElementPerformAction(t, "AXScrollToVisible")
    return r == 0, "scroll"

while True:
    line = sys.stdin.readline()
    if not line:
        break
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
        op = req.get("op") or ""
        x = int(req.get("x") or 0)
        y = int(req.get("y") or 0)
        if op == "ping":
            ok({"pong": True})
        elif op == "press_at":
            good, st = press_at(x, y)
            ok({"invoked": good, "strategy": st})
        elif op == "set_value_at":
            good, st = set_value_at(x, y, req.get("text") or "")
            ok({"set": good, "strategy": st})
        elif op == "focus_at":
            good, st = focus_at(x, y)
            ok({"invoked": good, "strategy": st})
        elif op == "scroll_into_view_at":
            good, st = scroll_at(x, y)
            ok({"invoked": good, "strategy": st})
        else:
            err("unknown op: " + op)
    except Exception as e:
        err(e)
`

type axResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	Invoked  bool   `json:"invoked"`
	Set      bool   `json:"set"`
	Strategy string `json:"strategy"`
}

func (s *axSidecar) ensureLocked() error {
	if s.ready && s.cmd != nil && s.cmd.Process != nil {
		return nil
	}
	s.stopLocked()
	cmd := exec.Command("python3", "-u", "-c", axSidecarPy)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.scanner = bufio.NewScanner(stdout)
	buf := make([]byte, 0, 64*1024)
	s.scanner.Buffer(buf, 1024*1024)
	s.ready = true
	return nil
}

func (s *axSidecar) stopLocked() {
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
		_, _ = s.cmd.Process.Wait()
	}
	s.cmd = nil
	s.scanner = nil
	s.ready = false
}

func (s *axSidecar) call(req map[string]interface{}) (axResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return axResponse{}, err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return axResponse{}, err
	}
	if _, err := io.WriteString(s.stdin, string(raw)+"\n"); err != nil {
		s.stopLocked()
		return axResponse{}, err
	}
	if !s.scanner.Scan() {
		s.stopLocked()
		return axResponse{}, fmt.Errorf("ax sidecar closed")
	}
	var resp axResponse
	if err := json.Unmarshal(s.scanner.Bytes(), &resp); err != nil {
		return axResponse{}, err
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(axIdleUnload, func() {
		s.mu.Lock()
		s.stopLocked()
		s.mu.Unlock()
	})
	if !resp.OK {
		return resp, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp, nil
}

func (s *axSidecar) actAt(op string, x, y int, text string) (bool, error) {
	req := map[string]interface{}{"op": op, "x": x, "y": y}
	if text != "" {
		req["text"] = text
	}
	resp, err := s.call(req)
	if err != nil {
		s.mu.Lock()
		s.stopLocked()
		s.mu.Unlock()
		resp, err = s.call(req)
		if err != nil {
			return false, err
		}
	}
	return resp.Invoked || resp.Set, nil
}
