//go:build !darwin && !windows

package accessibility

// ProbeDesktopPermissions is a best-effort stub on Linux and other platforms.
// Desktop Computer Use depends on distro a11y (AT-SPI) and display server access.
func ProbeDesktopPermissions() map[string]interface{} {
	return map[string]interface{}{
		"platform":              "other",
		"ok":                    true,
		"skipped":               true,
		"accessibility_trusted": nil,
		"screen_recording":      nil,
		"note":                  "Linux/other: ensure AT-SPI and display permissions for your DE; no TCC-style gate",
		"ms":                    0,
	}
}
