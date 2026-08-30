package main

import (
	"fmt"
	"strings"
)

const cliHelpText = `MaClaw desktop GUI

Usage:
  maclaw [command] [options]

Commands and flags:
  --help, -h       Show this help and exit
  --version        Print the build version and exit
  remote-smoke     Remote-session smoke tool (see remote-smoke -h)
  tui | ui         Start the terminal UI
  init             First-run / init mode
  autostart        Launched from OS login autostart

Linux AppImage: pick the Ubuntu ABI that matches the host.
  Ubuntu 22.04           -> *-u2204.AppImage (WebKit 4.0)
  Ubuntu 24.04 / 26.04   -> *-u2404.AppImage (WebKit 4.1)
CI AppImages bundle WebKitGTK so a typical remote host can run the
matching artifact without installing libwebkit2gtk.
`

// handleCLIInfo prints --help / --version and returns true when main should
// exit before desktop or TUI startup. The process still loads CGO WebView
// libraries, which is what the CI smoke uses to prove the artifact links.
func handleCLIInfo(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch strings.TrimSpace(args[1]) {
	case "--version", "-version", "version":
		fmt.Println(version)
		return true
	case "--help", "-h":
		fmt.Print(cliHelpText)
		return true
	default:
		return false
	}
}
