//go:build darwin

package guiautomation

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

// darwinInputSimulator uses a long-lived python3 process with Quartz loaded once.
// Falls back to one-shot python -c if the sidecar is unavailable.
type darwinInputSimulator struct{}

func NewInputSimulator() InputSimulator { return &darwinInputSimulator{} }

// ── persistent python sidecar ──────────────────────────────────────────

type darwinInputSidecar struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	ready   bool
	idle    *time.Timer
}

var globalDarwinInput darwinInputSidecar

const darwinInputIdle = 5 * time.Minute

const darwinInputServerPy = `
import json, sys, time
try:
    import Quartz
except Exception as e:
    sys.stderr.write("quartz import failed: %s\n" % e)
    sys.exit(1)

KEYMAP = {
    "return": 36, "enter": 36, "tab": 48, "space": 49, "backspace": 51, "delete": 51,
    "escape": 53, "esc": 53,
    "command": 55, "cmd": 55, "shift": 56, "option": 58, "alt": 58, "control": 59, "ctrl": 59,
    "f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
    "f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
    "home": 115, "end": 119, "pageup": 116, "pagedown": 121,
    "left": 123, "right": 124, "down": 125, "up": 126,
}
for i, ch in enumerate("abcdefghijklmnopqrstuvwxyz"):
    # approximate QWERTY map used by prior one-shot code
    pass
# full letter map from previous implementation
KEYMAP.update({
    "a": 0, "b": 11, "c": 8, "d": 2, "e": 14, "f": 3, "g": 5, "h": 4,
    "i": 34, "j": 38, "k": 40, "l": 37, "m": 46, "n": 45, "o": 31,
    "p": 35, "q": 12, "r": 15, "s": 1, "t": 17, "u": 32, "v": 9,
    "w": 13, "x": 7, "y": 16, "z": 6,
    "0": 29, "1": 18, "2": 19, "3": 20, "4": 21, "5": 23, "6": 22,
    "7": 26, "8": 28, "9": 25,
})

def ok(msg=None):
    o = {"ok": True}
    if msg:
        o["msg"] = msg
    print(json.dumps(o), flush=True)

def err(e):
    print(json.dumps({"ok": False, "error": str(e)}), flush=True)

def click(x, y, button="left", count=1):
    p = Quartz.CGPointMake(x, y)
    if button == "right":
        down, up, btn = Quartz.kCGEventRightMouseDown, Quartz.kCGEventRightMouseUp, Quartz.kCGMouseButtonRight
    else:
        down, up, btn = Quartz.kCGEventLeftMouseDown, Quartz.kCGEventLeftMouseUp, Quartz.kCGMouseButtonLeft
    for i in range(count):
        e = Quartz.CGEventCreateMouseEvent(None, down, p, btn)
        if count == 2:
            Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, i + 1)
        Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
        e = Quartz.CGEventCreateMouseEvent(None, up, p, btn)
        if count == 2:
            Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, i + 1)
        Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
        if count == 2 and i == 0:
            time.sleep(0.04)

def type_text(text):
    for ch in text:
        e = Quartz.CGEventCreateKeyboardEvent(None, 0, True)
        Quartz.CGEventKeyboardSetUnicodeString(e, len(ch), ch)
        Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
        e = Quartz.CGEventCreateKeyboardEvent(None, 0, False)
        Quartz.CGEventKeyboardSetUnicodeString(e, len(ch), ch)
        Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
        time.sleep(0.005)

def key_combo(keys):
    codes = []
    for k in keys:
        code = KEYMAP.get(str(k).lower())
        if code is None:
            raise ValueError("unknown key: %s" % k)
        codes.append(code)
    for c in codes:
        e = Quartz.CGEventCreateKeyboardEvent(None, c, True)
        Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    for c in reversed(codes):
        e = Quartz.CGEventCreateKeyboardEvent(None, c, False)
        Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)

def scroll(x, y, dx, dy):
    p = Quartz.CGPointMake(x, y)
    move = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventMouseMoved, p, Quartz.kCGMouseButtonLeft)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, move)
    sc = Quartz.CGEventCreateScrollWheelEvent(None, Quartz.kCGScrollEventUnitLine, 2, int(dy), int(dx))
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, sc)

def drag(fx, fy, tx, ty):
    src = Quartz.CGPointMake(fx, fy)
    dst = Quartz.CGPointMake(tx, ty)
    e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, src, Quartz.kCGMouseButtonLeft)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    time.sleep(0.05)
    e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDragged, dst, Quartz.kCGMouseButtonLeft)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    time.sleep(0.05)
    e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, dst, Quartz.kCGMouseButtonLeft)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
        op = req.get("op")
        if op == "ping":
            ok("pong")
        elif op == "click":
            click(int(req.get("x", 0)), int(req.get("y", 0)), req.get("button", "left"), int(req.get("count", 1)))
            ok()
        elif op == "type":
            type_text(req.get("text") or "")
            ok()
        elif op == "key":
            key_combo(req.get("keys") or [])
            ok()
        elif op == "scroll":
            scroll(int(req.get("x", 0)), int(req.get("y", 0)), int(req.get("dx", 0)), int(req.get("dy", 0)))
            ok()
        elif op == "drag":
            drag(int(req.get("fx", 0)), int(req.get("fy", 0)), int(req.get("tx", 0)), int(req.get("ty", 0)))
            ok()
        else:
            err("unknown op: %s" % op)
    except Exception as e:
        err(e)
`

func (s *darwinInputSidecar) stop() {
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

func (s *darwinInputSidecar) resetIdle() {
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(darwinInputIdle, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.ready {
			s.stop()
		}
	})
}

func (s *darwinInputSidecar) ensure() error {
	if s.ready {
		return nil
	}
	return s.start()
}

func (s *darwinInputSidecar) start() error {
	s.stop()
	cmd := exec.Command("python3", "-c", darwinInputServerPy)
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
	s.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if err := s.writeAndReadLocked(map[string]interface{}{"op": "ping"}); err != nil {
		s.stop()
		return err
	}
	s.ready = true
	s.resetIdle()
	return nil
}

func (s *darwinInputSidecar) writeAndReadLocked(req map[string]interface{}) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdin, "%s\n", body); err != nil {
		return err
	}
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("darwin input sidecar closed")
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(s.scanner.Bytes(), &resp); err != nil {
		return fmt.Errorf("decode: %w (%q)", err, s.scanner.Text())
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (s *darwinInputSidecar) call(req map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	if err := s.writeAndReadLocked(req); err != nil {
		s.stop()
		// one retry after restart
		if err2 := s.ensure(); err2 != nil {
			return err
		}
		if err2 := s.writeAndReadLocked(req); err2 != nil {
			s.stop()
			return err2
		}
	}
	s.resetIdle()
	return nil
}

func (d *darwinInputSimulator) callOrFallback(req map[string]interface{}, fallback func() error) error {
	if err := globalDarwinInput.call(req); err == nil {
		return nil
	}
	// Sidecar unavailable (no python3 / no Quartz) — last resort one-shot.
	return fallback()
}

// runPython one-shot fallback (cold start).
func runPython(script string) error {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("input simulation failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *darwinInputSimulator) Click(x, y int) error {
	return d.callOrFallback(map[string]interface{}{"op": "click", "x": x, "y": y, "button": "left", "count": 1}, func() error {
		script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, x, y)
		return runPython(script)
	})
}

func (d *darwinInputSimulator) RightClick(x, y int) error {
	return d.callOrFallback(map[string]interface{}{"op": "click", "x": x, "y": y, "button": "right", "count": 1}, func() error {
		script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventRightMouseDown, p, Quartz.kCGMouseButtonRight)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventRightMouseUp, p, Quartz.kCGMouseButtonRight)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, x, y)
		return runPython(script)
	})
}

func (d *darwinInputSimulator) DoubleClick(x, y int) error {
	return d.callOrFallback(map[string]interface{}{"op": "click", "x": x, "y": y, "button": "left", "count": 2}, func() error {
		script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 1)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 1)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 2)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 2)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, x, y)
		return runPython(script)
	})
}

func (d *darwinInputSimulator) Type(text string) error {
	return d.callOrFallback(map[string]interface{}{"op": "type", "text": text}, func() error {
		escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(text)
		script := fmt.Sprintf(`
import Quartz, time
text = '%s'
for ch in text:
    e = Quartz.CGEventCreateKeyboardEvent(None, 0, True)
    Quartz.CGEventKeyboardSetUnicodeString(e, len(ch), ch)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    e = Quartz.CGEventCreateKeyboardEvent(None, 0, False)
    Quartz.CGEventKeyboardSetUnicodeString(e, len(ch), ch)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    time.sleep(0.01)
`, escaped)
		return runPython(script)
	})
}

func (d *darwinInputSimulator) KeyCombo(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	arr := make([]interface{}, len(keys))
	for i, k := range keys {
		arr[i] = k
	}
	return d.callOrFallback(map[string]interface{}{"op": "key", "keys": arr}, func() error {
		// minimal fallback: only common keys via one-shot (reuse click-style map)
		script := "import Quartz\n"
		// use previous resolve logic inline is complex; require sidecar for full key set
		return fmt.Errorf("key combo requires python3+Quartz sidecar (keys=%v)", keys)
	})
}

func (d *darwinInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
	if deltaX == 0 && deltaY == 0 {
		return nil
	}
	return d.callOrFallback(map[string]interface{}{"op": "scroll", "x": x, "y": y, "dx": deltaX, "dy": deltaY}, func() error {
		script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
move = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventMouseMoved, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, move)
scroll = Quartz.CGEventCreateScrollWheelEvent(None, Quartz.kCGScrollEventUnitLine, 2, %d, %d)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, scroll)
`, x, y, deltaY, deltaX)
		return runPython(script)
	})
}

func (d *darwinInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	return d.callOrFallback(map[string]interface{}{
		"op": "drag", "fx": fromX, "fy": fromY, "tx": toX, "ty": toY,
	}, func() error {
		script := fmt.Sprintf(`
import Quartz, time
src = Quartz.CGPointMake(%d, %d)
dst = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, src, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
time.sleep(0.05)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDragged, dst, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
time.sleep(0.05)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, dst, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, fromX, fromY, toX, toY)
		return runPython(script)
	})
}
