//go:build !windows

package accessibility

// WarmupUIA is a no-op outside Windows (no UIA sidecar).
func WarmupUIA() map[string]interface{} {
	return map[string]interface{}{
		"ok":      true,
		"backend": "",
		"windows": 0,
		"ms":      0,
		"skipped": true,
		"reason":  "UIA sidecar is Windows-only",
	}
}

// SelfCheckUIA reports that a11y sidecar checks apply to Windows only.
func SelfCheckUIA() map[string]interface{} {
	return map[string]interface{}{
		"platform": "other",
		"ok":       true,
		"skipped":  true,
		"reason":   "UIA sidecar is Windows-only; macOS uses Accessibility + input sidecar",
	}
}
