//go:build windows

package accessibility

import "time"

// ProbeDesktopPermissions reports Windows readiness for Computer Use a11y.
// There is no macOS-style Accessibility TCC; UIA runs in-process/sidecar.
func ProbeDesktopPermissions() map[string]interface{} {
	start := time.Now()
	alive := UIASidecarAlive()
	backend := UIASidecarBackend()
	// If sidecar not yet started, a lightweight enum warms it (cheap when already warm).
	if !alive {
		_ = WarmupUIA()
		alive = UIASidecarAlive()
		backend = UIASidecarBackend()
	}
	out := map[string]interface{}{
		"platform":              "windows",
		"ok":                    true, // UIA path available via C#/PS sidecar on first use
		"accessibility_trusted": true,
		"screen_recording":      true,
		"uia_alive":             alive,
		"uia_backend":           backend,
		"note":                  "Windows UIA does not require Accessibility TCC; enterprise policy may still block automation",
		"ms":                    time.Since(start).Milliseconds(),
	}
	if !alive && backend == "" {
		out["note"] = "UIA sidecar not yet running; will start on first computer_observe (C# preferred, PowerShell fallback)"
	}
	return out
}
