package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func setLogDetailForTest(t *testing.T, enabled bool) {
	t.Helper()
	previous := corelib.IsLogDetailEnabled()
	corelib.SetLogDetailEnabled(enabled)
	t.Cleanup(func() { corelib.SetLogDetailEnabled(previous) })
}

func TestDetailAwareLogWriterCanDisableStderrMirror(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: nil}

	if _, err := writer.Write([]byte("warning: tui startup\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "warning: tui startup") {
		t.Fatalf("file log missing important line: %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr mirror should stay disabled, got %q", got)
	}
}

func TestDetailAwareLogWriterDropsUnimportantLinesWhenDetailDisabled(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: &stderr}

	if _, err := writer.Write([]byte("routine startup note\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); got != "" {
		t.Fatalf("file log should drop unimportant line, got %q", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr mirror should drop unimportant line, got %q", got)
	}
}

func TestDetailAwareLogWriterMirrorsStderrWhenConfigured(t *testing.T) {
	setLogDetailForTest(t, false)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: &stderr}

	if _, err := writer.Write([]byte("failed to start tui\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "failed to start tui") {
		t.Fatalf("file log missing important line: %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "failed to start tui") {
		t.Fatalf("stderr mirror missing important line: %q", got)
	}
}

func TestDetailAwareLogWriterWritesRoutineLinesWhenDetailEnabled(t *testing.T) {
	setLogDetailForTest(t, true)

	var file bytes.Buffer
	var stderr bytes.Buffer
	writer := &detailAwareLogWriter{file: &file, stderr: &stderr}

	if _, err := writer.Write([]byte("routine startup note\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := file.String(); !strings.Contains(got, "routine startup note") {
		t.Fatalf("file log missing detail line: %q", got)
	}
	if got := stderr.String(); !strings.Contains(got, "routine startup note") {
		t.Fatalf("stderr mirror missing detail line: %q", got)
	}
}

func TestDetailAwareLogWriterAllowsNilMirrorTargets(t *testing.T) {
	setLogDetailForTest(t, true)

	writer := &detailAwareLogWriter{}
	if n, err := writer.Write([]byte("detail line without sinks\n")); err != nil || n == 0 {
		t.Fatalf("Write() = (%d, %v), want successful discard", n, err)
	}
}

func TestSetLogFallbackSuppressesTUIStderr(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	var stderr bytes.Buffer
	log.SetOutput(&stderr)
	log.SetFlags(0)

	setLogFallback(false, &stderr)
	log.Print("hidden from terminal")
	if got := stderr.String(); got != "" {
		t.Fatalf("TUI fallback should suppress default stderr logging, got %q", got)
	}
}

func TestSetLogFallbackPreservesDesktopStderr(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	var stderr bytes.Buffer
	log.SetOutput(&stderr)
	log.SetFlags(0)

	setLogFallback(true, &stderr)
	log.Print("visible in desktop mode")
	if got := stderr.String(); !strings.Contains(got, "visible in desktop mode") {
		t.Fatalf("desktop fallback should keep stderr logging, got %q", got)
	}
}

func TestSetLogFallbackHandlesNilDesktopStderr(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	setLogFallback(true, nil)
	if log.Writer() == nil {
		t.Fatal("desktop fallback should install a non-nil writer")
	}
}

func TestIsTUISubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "standalone desktop", args: []string{"maclaw"}, want: false},
		{name: "tui", args: []string{"maclaw", "tui"}, want: true},
		{name: "ui alias", args: []string{"maclaw", "ui"}, want: true},
		{name: "later tui arg is not subcommand", args: []string{"maclaw", "remote-smoke", "tui"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTUISubcommand(tt.args); got != tt.want {
				t.Fatalf("isTUISubcommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
