package device

import (
	"fmt"
	"runtime"
	"strings"

	"go.bug.st/serial"
)

// HostAccess describes only local prerequisites. It never downloads or
// installs drivers, changes group membership, or attempts elevation.
type HostAccess struct {
	Platform     string `json:"platform"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Guide        string `json:"guide"`
	Port         string `json:"port,omitempty"`
	DriverNeeded bool   `json:"driverNeeded"`
	AccessNeeded bool   `json:"accessNeeded"`
}

// DiagnoseAccess performs a short open/close check only. It deliberately
// avoids DTR/RTS changes, writes, erases, and ROM reset traffic, so it can be
// shown before users start a probe or flash job.
func DiagnoseAccess(port string) HostAccess {
	result := HostAccess{Platform: runtime.GOOS, Port: port, Status: "unknown", Message: "Select a serial device to check local access."}
	if strings.TrimSpace(port) == "" {
		return result
	}
	if !validSerialPort(port) {
		result.Status = "unsupported"
		result.Message = "The selected path is not a supported serial endpoint."
		return result
	}
	serialPort, err := serial.Open(port, &serial.Mode{BaudRate: 115200})
	if err == nil {
		_ = serialPort.Close()
		result.Status = "ready"
		result.Message = "The operating system allowed a read-only serial open."
		return result
	}
	result.Status = "blocked"
	result.Message = "The serial endpoint could not be opened. Check the cable, whether another application is using it, and the platform access guidance."
	result.Guide, result.DriverNeeded, result.AccessNeeded = accessGuide(runtime.GOOS, err)
	return result
}

func accessGuide(platform string, err error) (guide string, driverNeeded, accessNeeded bool) {
	message := strings.ToLower(err.Error())
	switch platform {
	case "windows":
		guide = "Windows: reconnect the device with a data-capable cable. For CP210x, CH34x, or FTDI adapters, install the vendor-signed driver through Windows Update or the adapter vendor. Close Device Manager, serial terminals, and IDEs that may hold the COM port."
		driverNeeded = strings.Contains(message, "not found") || strings.Contains(message, "cannot find") || strings.Contains(message, "does not exist")
		accessNeeded = strings.Contains(message, "access") || strings.Contains(message, "denied") || strings.Contains(message, "busy")
	case "darwin":
		guide = "macOS: use a data-capable cable and close serial terminals. Prefer the built-in USB CDC endpoint; if a USB-UART adapter requires a driver, install only the vendor-notarized driver and approve it in Privacy & Security."
		driverNeeded = strings.Contains(message, "no such") || strings.Contains(message, "not found")
		accessNeeded = strings.Contains(message, "permission") || strings.Contains(message, "resource busy") || strings.Contains(message, "busy")
	default:
		guide = "Linux: reconnect the device and close serial terminals. Ensure your user belongs to the distribution's serial group (commonly dialout or uucp), then sign out and back in. If a USB-UART adapter is missing, install the distribution-supported driver or udev rule; do not run the flasher as root."
		driverNeeded = strings.Contains(message, "no such") || strings.Contains(message, "not found")
		accessNeeded = strings.Contains(message, "permission") || strings.Contains(message, "denied") || strings.Contains(message, "busy")
	}
	return guide, driverNeeded, accessNeeded
}

func (h HostAccess) Error() error {
	if h.Status == "ready" || h.Status == "unknown" {
		return nil
	}
	return fmt.Errorf("serial host access is %s: %s", h.Status, h.Message)
}
