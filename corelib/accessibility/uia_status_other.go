//go:build !windows

package accessibility

// UIASidecarAlive is only implemented on Windows (persistent UIA helper).
func UIASidecarAlive() bool { return false }

// UIASidecarBackend is only implemented on Windows ("csharp" | "powershell").
func UIASidecarBackend() string { return "" }
