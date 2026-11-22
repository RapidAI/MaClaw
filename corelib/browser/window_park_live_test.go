//go:build windows

package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestLiveParkKeepsPageVisibleOffScreen launches a temporary headed Chrome.
// It is not part of the default suite: MACLAW_PARK_LIVE=1 go test ./corelib/browser -run TestLiveParkKeepsPageVisibleOffScreen
func TestLiveParkKeepsPageVisibleOffScreen(t *testing.T) {
	if os.Getenv("MACLAW_PARK_LIVE") == "" {
		t.Skip("set MACLAW_PARK_LIVE=1 to launch Chrome")
	}
	bi := detectBrowser()
	if bi == nil {
		t.Fatal("chrome or edge not found")
	}
	dir := t.TempDir()
	port, err := findFreeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--remote-allow-origins=http://127.0.0.1:%d,http://localhost:%d", port, port),
		"--user-data-dir=" + dir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=" + nativeOcclusionFeature,
		"--window-size=1280,800",
		"--new-window",
		"data:text/html,<title>park</title><body style='margin:0;background:red'>park</body>",
	}
	cmd := exec.Command(bi.path, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pids, _ := profileProcessIDs(dir)
		for pid := range pids {
			if proc, err := os.FindProcess(int(pid)); err == nil {
				_ = proc.Kill()
			}
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(12 * time.Second)
	var hwnds []uintptr
	for time.Now().Before(deadline) {
		pids, safe := profileProcessIDs(dir)
		if safe && len(pids) > 0 {
			hwnds = chromeWindowsForPIDs(pids)
			if len(hwnds) > 0 {
				parkProfileWindows(dir)
				hwnds = chromeWindowsForPIDs(pids)
				onScreen := false
				for _, hwnd := range hwnds {
					if windowNeedsPark(hwnd) {
						onScreen = true
					}
				}
				if len(hwnds) > 0 && !onScreen {
					break
				}
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(hwnds) == 0 {
		t.Fatal("headed window did not appear")
	}
	for _, hwnd := range hwnds {
		if windowNeedsPark(hwnd) {
			t.Fatal("window still meets a monitor")
		}
		if !windowIsTool(hwnd) {
			t.Fatal("window still has a taskbar button")
		}
	}
	// A maximized or minimized window must come back off the screen. Placement
	// alone is pulled onto a monitor; the park has to finish with SetWindowPos.
	for _, cmd := range []uintptr{3, 6} { // SW_MAXIMIZE, SW_MINIMIZE
		pids, _ := profileProcessIDs(dir)
		targets := chromeWindowsForPIDs(pids)
		for _, hwnd := range targets {
			procShowWindowPark.Call(hwnd, cmd)
		}
		time.Sleep(200 * time.Millisecond)
		changed := false
		for _, hwnd := range targets {
			if windowIconicOrZoomed(hwnd) {
				changed = true
			}
		}
		if !changed {
			t.Fatalf("show %d did not minimize or maximize the window", cmd)
		}
		deadline = time.Now().Add(3 * time.Second)
		for {
			parkProfileWindows(dir)
			pids, _ = profileProcessIDs(dir)
			hwnds = chromeWindowsForPIDs(pids)
			pending := false
			for _, hwnd := range hwnds {
				if windowNeedsPark(hwnd) {
					pending = true
				}
			}
			if len(hwnds) > 0 && !pending {
				break
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("show %d left the window on a monitor", cmd)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}

	deadline = time.Now().Add(8 * time.Second)
	var wsURL string
	for time.Now().Before(deadline) {
		targets, err := DiscoverTargets(addr)
		if err == nil {
			for _, target := range targets {
				if target.Type == "page" && target.WebSocketDebugURL != "" {
					wsURL = target.WebSocketDebugURL
					break
				}
			}
		}
		if wsURL != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if wsURL == "" {
		t.Fatal("cdp page did not appear")
	}
	client, err := ConnectCDP(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	raw, err := client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    "document.visibilityState",
		"returnByValue": true,
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var evaluated struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &evaluated); err != nil {
		t.Fatal(err)
	}
	if evaluated.Result.Value != "visible" {
		t.Fatalf("visibilityState=%q", evaluated.Result.Value)
	}
	shot, err := client.Send("Page.captureScreenshot", map[string]interface{}{
		"format":  "jpeg",
		"quality": 30,
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(shot) < 1000 {
		t.Fatalf("screenshot too small: %d", len(shot))
	}
}
