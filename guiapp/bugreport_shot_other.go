//go:build !windows

package guiapp

import "fmt"

func captureBugReportWindowPNG() ([]byte, error) {
	return nil, fmt.Errorf("main window screenshot is only available on Windows")
}
