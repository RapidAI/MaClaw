//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/UserExistsError/conpty"
)

func remotePTYCapability() (bool, string) {
	if conpty.IsConPtyAvailable() {
		return true, "ConPTY is available"
	}
	return false, "Windows ConPTY is not available on this system"
}

func remotePTYInteractiveSmokeProbe() (bool, string) {
	pty := NewWindowsPTYSession()
	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		return false, "ConPTY interactive probe failed to start: " + err.Error()
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	if err := pty.Write([]byte("echo maclaw-conpty-probe\r\n")); err != nil {
		return false, "ConPTY interactive probe failed to write: " + err.Error()
	}

	deadline := time.After(10 * time.Second)
	var output strings.Builder

	for {
		select {
		case chunk, ok := <-pty.Output():
			if !ok {
				return false, "ConPTY interactive probe output closed before echo completed"
			}
			output.Write(chunk)
			if strings.Contains(strings.ToLower(output.String()), "maclaw-conpty-probe") {
				return true, "ConPTY interactive probe succeeded"
			}
		case exit := <-pty.Exit():
			return false, fmt.Sprintf("ConPTY interactive probe exited early: %+v", exit)
		case <-deadline:
			return false, "ConPTY interactive probe timed out waiting for echo output"
		}
	}
}

func remoteClaudeLaunchSmokeProbe(cmd CommandSpec) (bool, string) {
	pty := NewWindowsPTYSession()
	_, err := pty.Start(cmd)
	if err != nil {
		return false, "Claude launch probe failed to start: " + err.Error()
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	successDeadline := time.After(4 * time.Second)
	var output strings.Builder

	for {
		select {
		case chunk, ok := <-pty.Output():
			if !ok {
				return false, "Claude launch probe output closed before startup completed"
			}
			output.Write(chunk)
			if output.Len() > 0 {
				return true, "Claude launch probe succeeded: process started under ConPTY and produced output"
			}
		case exit := <-pty.Exit():
			return false, fmt.Sprintf("Claude launch probe exited early: %+v", exit)
		case <-successDeadline:
			return true, "Claude launch probe succeeded: process started under ConPTY"
		}
	}
}
