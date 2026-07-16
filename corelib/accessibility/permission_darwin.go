//go:build darwin

package accessibility

import (
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// ProbeDesktopPermissions reports macOS TCC-related readiness for Computer Use.
// Accessibility is required for a11y trees; Screen Recording is required for capture.
func ProbeDesktopPermissions() map[string]interface{} {
	start := time.Now()
	out := map[string]interface{}{
		"platform":              "darwin",
		"ok":                    false,
		"accessibility_trusted": false,
		"screen_recording":      nil,
		"error":                 "",
		"hint":                  "System Settings → Privacy & Security → Accessibility (and Screen Recording for screenshots)",
		"ms":                    0,
	}

	// Prefer ApplicationServices.AXIsProcessTrusted + optional CGPreflightScreenCaptureAccess.
	script := `
import json
out = {"trusted": False, "screen": None, "error": ""}
try:
    from ApplicationServices import AXIsProcessTrusted
    out["trusted"] = bool(AXIsProcessTrusted())
except Exception as e:
    out["error"] = "AXIsProcessTrusted: %s" % e
try:
    import Quartz
    if hasattr(Quartz, "CGPreflightScreenCaptureAccess"):
        out["screen"] = bool(Quartz.CGPreflightScreenCaptureAccess())
except Exception:
    pass
print(json.dumps(out), flush=True)
`
	cmd := exec.Command("python3", "-c", script)
	raw, err := cmd.Output()
	out["ms"] = time.Since(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			msg = strings.TrimSpace(string(exitErr.Stderr))
		}
		out["error"] = msg
		// Still try a coarse enum to see if Accessibility works without the probe.
		if coarseTrusted() {
			out["accessibility_trusted"] = true
			out["ok"] = true
			out["error"] = ""
			out["note"] = "AXIsProcessTrusted probe failed; EnumElements succeeded"
		}
		return out
	}

	var parsed struct {
		Trusted bool    `json:"trusted"`
		Screen  *bool   `json:"screen"`
		Error   string  `json:"error"`
	}
	if jerr := json.Unmarshal(raw, &parsed); jerr != nil {
		out["error"] = "parse probe: " + jerr.Error()
		return out
	}
	out["accessibility_trusted"] = parsed.Trusted
	if parsed.Screen != nil {
		out["screen_recording"] = *parsed.Screen
	}
	if parsed.Error != "" && !parsed.Trusted {
		out["error"] = parsed.Error
	}
	// Accessibility trusted is the main gate for a11y trees; screen recording
	// is best-effort (nil when API missing). YOLO/OCR need capture.
	out["ok"] = parsed.Trusted
	if parsed.Screen != nil && !*parsed.Screen {
		out["ok"] = false
		if out["error"] == "" {
			out["error"] = "screen recording not granted"
		}
	}
	return out
}

func coarseTrusted() bool {
	// Cheap: list frontmost app AX tree depth 1; success implies trust.
	b := &darwinBridge{}
	_, err := b.EnumElements("")
	return err == nil || !isAccessibilityPermissionDenied(err)
}
