//go:build windows

package accessibility

import (
	"fmt"
	"time"
)

// WarmupUIA pre-starts the preferred UIA sidecar (C# if available) and performs
// a cheap top-level window enum so the first Computer Use observe is faster.
// Safe to call repeatedly; concurrent calls share the global sidecar.
func WarmupUIA() map[string]interface{} {
	start := time.Now()
	result := map[string]interface{}{
		"ok":      false,
		"backend": "",
		"windows": 0,
		"ms":      0,
		"error":   "",
		"exe":     uiaCSharpSidecarPath(), // may compile
	}
	// Force start + ping via enum of top-level windows.
	els, err := globalUIASidecar.enum("", 1)
	result["ms"] = time.Since(start).Milliseconds()
	result["backend"] = UIABackend()
	result["alive"] = UIASidecarAlive()
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	result["ok"] = true
	result["windows"] = len(els)
	return result
}

// SelfCheckUIA returns a structured diagnostics snapshot for Computer Use a11y.
func SelfCheckUIA() map[string]interface{} {
	out := map[string]interface{}{
		"platform":       "windows",
		"csc_found":      findCSC() != "",
		"csharp_exe":     findExistingUIACsharp(),
		"csharp_path":    "",
		"warmup":         nil,
		"backend_after":  "",
		"alive_after":    false,
		"ok":             false,
		"error":          "",
	}
	// Prefer prebuilt; allow compile for check.
	ResetUIACsharpCache()
	p := uiaCSharpSidecarPath()
	out["csharp_path"] = p
	if p == "" && findCSC() == "" {
		out["error"] = "no prebuilt csharp sidecar and csc.exe not found; will use powershell fallback"
	}
	w := WarmupUIA()
	out["warmup"] = w
	out["backend_after"] = UIABackend()
	out["alive_after"] = UIASidecarAlive()
	if ok, _ := w["ok"].(bool); ok {
		out["ok"] = true
	} else if err, _ := w["error"].(string); err != "" {
		out["error"] = err
	} else {
		out["error"] = fmt.Sprintf("%v", w["error"])
	}
	// PowerShell fallback still counts as ok for Computer Use.
	if UIASidecarAlive() {
		out["ok"] = true
	}
	return out
}
