//go:build !windows

package guiapp

import (
	"fmt"
	"os"
)

func platformAcceptanceSession() (consoleAcceptanceSession, string) {
	return nil, acceptanceReasonNoPlatform
}

func runConsoleBufferCommand(args []string) int {
	fmt.Fprintln(os.Stderr, acceptanceReasonNoPlatform)
	return 1
}
