package main

import (
	"strings"
	"testing"
)

func TestIsAIExternalCodingSessionLaunch(t *testing.T) {
	cases := []struct {
		source RemoteLaunchSource
		tool   string
		want   bool
	}{
		{RemoteLaunchSourceAI, "claude", true},
		{RemoteLaunchSourceAI, "Claude", true},
		{RemoteLaunchSourceAI, "codex", true},
		{RemoteLaunchSourceAI, "opencode", true},
		{RemoteLaunchSourceAI, "", true}, // empty normalizes to claude elsewhere
		{RemoteLaunchSourceAI, "browser", false},
		{RemoteLaunchSourceAI, "ai-assistant", false},
		{RemoteLaunchSourceDesktop, "claude", false},
		{RemoteLaunchSourceMobile, "claude", false},
		{RemoteLaunchSourceHandoff, "claude", false},
		{"", "claude", false}, // empty source normalizes to desktop
	}
	for _, tc := range cases {
		if got := isAIExternalCodingSessionLaunch(tc.source, tc.tool); got != tc.want {
			t.Fatalf("isAIExternalCodingSessionLaunch(%q, %q) = %v, want %v", tc.source, tc.tool, got, tc.want)
		}
	}
}

func TestRejectAIExternalCodingSessionLaunch(t *testing.T) {
	err := rejectAIExternalCodingSessionLaunch(LaunchSpec{
		Tool:         "claude",
		LaunchSource: RemoteLaunchSourceAI,
	})
	if err == nil {
		t.Fatal("expected error for AI+claude launch")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %q, want disabled message", err.Error())
	}

	if err := rejectAIExternalCodingSessionLaunch(LaunchSpec{
		Tool:         "claude",
		LaunchSource: RemoteLaunchSourceDesktop,
	}); err != nil {
		t.Fatalf("desktop claude launch should be allowed, got %v", err)
	}
}

func TestRemoteSessionManagerCreateUserSessionRejectsAI(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	mgr := NewRemoteSessionManager(app)
	for _, tool := range []string{"claude", "codex", "", "browser", "ai-assistant"} {
		session, err := mgr.CreateUserSession(LaunchSpec{
			Tool:         tool,
			ProjectPath:  t.TempDir(),
			LaunchSource: RemoteLaunchSourceAI,
		})
		if err == nil {
			t.Fatalf("expected CreateUserSession to reject AI launch for tool %q", tool)
		}
		if session != nil {
			t.Fatalf("expected nil session on reject for tool %q, got %#v", tool, session)
		}
		if !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("tool %q: error = %q, want disabled", tool, err.Error())
		}
	}
	if got := len(mgr.List()); got != 0 {
		t.Fatalf("sessions stored = %d, want 0", got)
	}

	// The narrower external-CLI guard remains useful at App API boundaries;
	// CreateUserSession applies the stricter user-only boundary above.
	if err := rejectAIExternalCodingSessionLaunch(LaunchSpec{
		Tool:         "ai-assistant",
		LaunchSource: RemoteLaunchSourceAI,
	}); err != nil {
		t.Fatalf("ai-assistant AI launch should not hit external-CLI guard: %v", err)
	}
}

func TestRemoteSessionAppEntrypointsRejectAIExternalLaunchBeforeSetup(t *testing.T) {
	app := &App{}
	for _, start := range []struct {
		name string
		fn   func() error
	}{
		{
			name: "remote",
			fn: func() error {
				_, err := app.StartRemoteSession("claude", "", false, "", RemoteLaunchSourceAI)
				return err
			},
		},
		{
			name: "handoff",
			fn: func() error {
				_, err := app.StartRemoteHandoffSession("claude", "", false, "", RemoteLaunchSourceAI)
				return err
			},
		},
	} {
		err := start.fn()
		if err == nil {
			t.Fatalf("%s entrypoint unexpectedly accepted an AI launch source", start.name)
		}
		if app.remoteSessions != nil {
			t.Fatalf("%s entrypoint initialized RemoteSessionManager before rejecting AI launch", start.name)
		}
	}
}
