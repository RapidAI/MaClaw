//go:build windows

package main

import (
	"strings"
	"testing"
	"time"
)

// fakeExecForResponder implements ExecutionHandle with a write recorder.
type fakeExecForResponder struct {
	written []byte
}

func (f *fakeExecForResponder) PID() int                { return 1 }
func (f *fakeExecForResponder) Write(data []byte) error  { f.written = append(f.written, data...); return nil }
func (f *fakeExecForResponder) Interrupt() error         { return nil }
func (f *fakeExecForResponder) Kill() error              { return nil }
func (f *fakeExecForResponder) Resize(c, r int) error    { return nil }
func (f *fakeExecForResponder) Close() error             { return nil }
func (f *fakeExecForResponder) Output() <-chan []byte     { return nil }
func (f *fakeExecForResponder) Exit() <-chan PTYExit      { return nil }

func TestStartupResponderThemeSelection(t *testing.T) {
	app := &App{}
	exec := &fakeExecForResponder{}
	session := &RemoteSession{
		ID:        "test-theme",
		CreatedAt: time.Now(),
		Exec:      exec,
	}
	r := newStartupAutoResponder(app, session)
	r.done = false // Enable the responder for testing

	// Simulate Claude Code theme selection output
	r.feed([]string{
		"Welcome to Claude Code!",
		"Choose a theme:",
		"1. Dark (default)",
		"2. Light",
		"3. Solarized Dark",
	})

	// Give the goroutine time to send
	time.Sleep(1 * time.Second)

	got := string(exec.written)
	if !strings.Contains(got, "1") {
		t.Errorf("expected '1' in written bytes, got %q", got)
	}
	if !strings.Contains(got, "\r") {
		t.Errorf("expected '\\r' in written bytes, got %q", got)
	}
}

func TestStartupResponderNumberedMenu(t *testing.T) {
	app := &App{}
	exec := &fakeExecForResponder{}
	session := &RemoteSession{
		ID:        "test-menu",
		CreatedAt: time.Now(),
		Exec:      exec,
	}
	r := newStartupAutoResponder(app, session)
	r.done = false // Enable the responder for testing

	r.feed([]string{
		"Please select an option:",
		"1. Use existing config",
		"2. Create new config",
		"3. Skip setup",
	})

	time.Sleep(1 * time.Second)

	got := string(exec.written)
	if !strings.Contains(got, "1") {
		t.Errorf("expected '1' in written bytes, got %q", got)
	}
}

func TestStartupResponderDoesNotFireAfterWindow(t *testing.T) {
	app := &App{}
	exec := &fakeExecForResponder{}
	session := &RemoteSession{
		ID:        "test-expired",
		CreatedAt: time.Now().Add(-60 * time.Second), // started 60s ago
		Exec:      exec,
	}
	r := newStartupAutoResponder(app, session)

	r.feed([]string{
		"Choose a theme:",
		"1. Dark",
		"2. Light",
	})

	time.Sleep(200 * time.Millisecond)

	if len(exec.written) > 0 {
		t.Errorf("expected no writes after window expired, got %q", string(exec.written))
	}
}

func TestStartupResponderDetectsNormalMode(t *testing.T) {
	app := &App{}
	exec := &fakeExecForResponder{}
	session := &RemoteSession{
		ID:        "test-normal",
		CreatedAt: time.Now(),
		Exec:      exec,
	}
	r := newStartupAutoResponder(app, session)
	r.done = false // Enable the responder for testing

	// First feed normal mode indicator
	r.feed([]string{"How can I help you today?"})

	// Then feed a theme prompt — should NOT trigger because we're in normal mode
	r.feed([]string{
		"Choose a theme:",
		"1. Dark",
	})

	time.Sleep(200 * time.Millisecond)

	if len(exec.written) > 0 {
		t.Errorf("expected no writes after normal mode detected, got %q", string(exec.written))
	}
}

func TestStartupResponderDoesNotRepeat(t *testing.T) {
	app := &App{}
	exec := &fakeExecForResponder{}
	session := &RemoteSession{
		ID:        "test-no-repeat",
		CreatedAt: time.Now(),
		Exec:      exec,
	}
	r := newStartupAutoResponder(app, session)
	r.done = false // Enable the responder for testing

	r.feed([]string{
		"Choose a theme:",
		"1. Dark",
		"2. Light",
		"3. Solarized",
	})

	time.Sleep(1 * time.Second)
	firstLen := len(exec.written)

	// Feed the same prompt again
	r.feed([]string{
		"Choose a theme:",
		"1. Dark",
		"2. Light",
		"3. Solarized",
	})

	time.Sleep(500 * time.Millisecond)

	if len(exec.written) != firstLen {
		t.Errorf("expected no additional writes on repeat, first=%d, now=%d", firstLen, len(exec.written))
	}
}
