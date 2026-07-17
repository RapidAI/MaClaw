package commands

import (
	"fmt"
	"strings"
	"sync"
)

// captureMu serializes CaptureOutput so concurrent GUI installs do not race
// package stdio writers.
var captureMu sync.Mutex

// maxCaptureBytes caps chat-bound CLI output so a huge search dump cannot flood the UI.
const maxCaptureBytes = 64 << 10 // 64 KiB

// captureTruncationMarker is appended when output hits maxCaptureBytes.
const captureTruncationMarker = "\n... (output truncated)"

// cappedWriter keeps at most max bytes, then discards further writes while
// still accepting them so producers never block. Safe for concurrent Write
// (CLI may log from multiple goroutines while CaptureOutput is active).
type cappedWriter struct {
	mu        sync.Mutex
	buf       []byte
	max       int
	truncated bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buf) < c.max {
		remain := c.max - len(c.buf)
		if len(p) <= remain {
			c.buf = append(c.buf, p...)
		} else {
			c.buf = append(c.buf, p[:remain]...)
			c.truncated = true
		}
	} else {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedWriter) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := string(c.buf)
	if c.truncated {
		for len(s) > 0 && (s[len(s)-1]&0xC0) == 0x80 {
			s = s[:len(s)-1]
		}
		return s + captureTruncationMarker
	}
	return s
}

// CaptureOutput runs fn while capturing package CLI stdout/stderr (Stdout/Stderr).
// Used by GUI/AI assistant slash handlers that reuse the same CLI implementations.
//
// Unlike redirecting os.Stdout/os.Stderr, this only swaps package-level writers.
// Concurrent process logs (log.Print, other packages writing to os.Stdout) are
// NOT captured and never interleave into install chat output.
//
// Guarantees:
//   - package Stdout/Stderr always restored (including panic)
//   - concurrent captures are serialized
//   - flag.ContinueOnError usage text written to Stderr() is included
//   - output is truncated to maxCaptureBytes without unbounded buffering
func CaptureOutput(fn func() error) (out string, err error) {
	captureMu.Lock()
	defer captureMu.Unlock()

	outCap := &cappedWriter{buf: make([]byte, 0, 4096), max: maxCaptureBytes}
	errCap := &cappedWriter{buf: make([]byte, 0, 1024), max: maxCaptureBytes}
	restore := swapStdio(outCap, errCap)
	defer restore()

	// Recover panics so GUI process is not taken down by CLI code.
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("command panicked: %v", rec)
			}
		}()
		err = fn()
	}()

	// Prefer stdout; append stderr when present (flag usage, warnings).
	combinedBare, outTrunc := stripCaptureTruncation(outCap.String())
	seBare, errTrunc := stripCaptureTruncation(errCap.String())
	se := strings.TrimSpace(seBare)
	if se != "" {
		if strings.TrimSpace(combinedBare) == "" {
			combinedBare = se
		} else if !strings.Contains(combinedBare, se) {
			combinedBare = strings.TrimRight(combinedBare, "\n") + "\n" + se
		}
	}
	if len(combinedBare) > maxCaptureBytes {
		return truncateCapture(combinedBare), err
	}
	if outTrunc || errTrunc {
		return combinedBare + captureTruncationMarker, err
	}
	return combinedBare, err
}

func stripCaptureTruncation(s string) (body string, truncated bool) {
	if strings.HasSuffix(s, captureTruncationMarker) {
		return strings.TrimSuffix(s, captureTruncationMarker), true
	}
	return s, false
}

func truncateCapture(s string) string {
	if len(s) <= maxCaptureBytes {
		return s
	}
	cut := maxCaptureBytes
	for cut > 0 && cut < len(s) && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + captureTruncationMarker
}

// RunSkillCaptured executes skill subcommands and returns printed CLI output.
func RunSkillCaptured(args []string) (string, error) {
	return CaptureOutput(func() error { return RunSkill(args) })
}

// RunMCPCaptured executes mcp subcommands and returns printed CLI output.
func RunMCPCaptured(args []string) (string, error) {
	return CaptureOutput(func() error { return RunMCP(args) })
}

// RunPluginCaptured executes plugin subcommands and returns printed CLI output.
func RunPluginCaptured(args []string) (string, error) {
	return CaptureOutput(func() error { return RunPlugin(args) })
}
