package computeruse

import (
	"fmt"
	"strings"
)

// BrowserNoPixel is the refusal for action=computer on a browser window.
// The next step is another structure read, or handing the step to the person.
const BrowserNoPixel = "再读一次结构，或把这一步交给人。不点浏览器窗口"

// ProbeFirst is the refusal for a coordinate computer batch on a desktop
// window that has no element id from app_probe yet.
const ProbeFirst = "先 app_probe，用返回的 id"

// PixelRefusal reports why this computer batch must not call xdotool.
// A browser window never takes a pixel batch. A known desktop window needs
// an element id on every driving step. A captcha, QR login, or payment
// confirmation stays a handoff, including when the step names an id.
// An empty window class is not a refusal: the caller could not read it.
// A batch whose driving steps all name an id is allowed. This function does
// not refuse a coordinate click merely because an earlier probe had refs.
func PixelRefusal(windowClass, pageNeed string, steps []Step) error {
	driving := false
	bare := false
	for _, step := range steps {
		if !stepDrives(step) {
			continue
		}
		driving = true
		if step.ID == 0 {
			bare = true
		}
	}
	if !driving {
		return nil
	}
	if handoffPage(pageNeed) || WindowIsBrowser(windowClass) {
		return fmt.Errorf("%s", BrowserNoPixel)
	}
	if strings.TrimSpace(windowClassName(windowClass)) != "" && bare {
		return fmt.Errorf("%s", ProbeFirst)
	}
	return nil
}

// WindowIsBrowser reports a WM_CLASS that holds a browser window.
func WindowIsBrowser(class string) bool {
	switch strings.ToLower(windowClassName(class)) {
	case "chromium", "chromium-browser", "chrome", "google-chrome", "google-chrome-stable",
		"firefox", "firefox-esr", "navigator", "epiphany", "microsoft-edge", "msedge":
		return true
	default:
		return false
	}
}

func windowClassName(class string) string {
	class = strings.TrimSpace(class)
	if i := strings.IndexAny(class, "\r\n"); i >= 0 {
		class = strings.TrimSpace(class[:i])
	}
	return class
}

func handoffPage(pageNeed string) bool {
	switch strings.TrimSpace(pageNeed) {
	case "captcha_widget", "login_qr", "payment_confirm":
		return true
	default:
		return false
	}
}

// stepDrives is a step that can click, type, or press. A screenshot, a zoom,
// a cursor read, and a wait do not.
func stepDrives(step Step) bool {
	if step.ID != 0 {
		return true
	}
	switch step.Kind {
	case "screenshot", "zoom", "cursor_position", "wait":
		return false
	default:
		return true
	}
}
