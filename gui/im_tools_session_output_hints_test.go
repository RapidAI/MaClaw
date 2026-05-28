package main

import (
	"strings"
	"testing"
)

func TestAppendBusySessionHintUsesFacts(t *testing.T) {
	var b strings.Builder
	appendBusySessionHint(&b, sessionOutputHintFacts{
		Status:      SessionBusy,
		HasAPIRetry: true,
	})
	if got := b.String(); !strings.Contains(got, "API") || !strings.Contains(got, "自动重试") {
		t.Fatalf("busy API retry hint = %q, want API retry guidance", got)
	}
}

func TestAppendTerminalSessionExitHintIgnoresMissingOrZeroExitCode(t *testing.T) {
	for name, facts := range map[string]sessionOutputHintFacts{
		"missing": {
			Status:            SessionExited,
			StructuredSession: true,
		},
		"zero pty": {
			Status:   SessionExited,
			ExitCode: intPtr(0),
		},
	} {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			appendTerminalSessionExitHint(&b, facts)
			if got := b.String(); got != "" {
				t.Fatalf("exit hint = %q, want empty", got)
			}
		})
	}
}

func TestAppendStructuredSessionExitHintHandlesFatalAndRetryExhausted(t *testing.T) {
	for name, facts := range map[string]sessionOutputHintFacts{
		"fatal": {
			ExitCode:          intPtr(2),
			Tool:              "codex",
			StructuredSession: true,
			FatalSessionError: true,
		},
		"retry exhausted": {
			ExitCode:          intPtr(2),
			Tool:              "codex",
			StructuredSession: true,
			ResumeContext:     &SessionResumeContext{ResumeCount: 3},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			appendStructuredSessionExitHint(&b, facts)
			got := b.String()
			if !strings.Contains(got, "codex") || !strings.Contains(got, "2") {
				t.Fatalf("structured exit hint = %q, want tool and exit code", got)
			}
		})
	}
}

func TestAppendExitHintHelpersIgnoreMissingExitCode(t *testing.T) {
	var structured strings.Builder
	appendStructuredSessionExitHint(&structured, sessionOutputHintFacts{})
	if got := structured.String(); got != "" {
		t.Fatalf("structured helper hint = %q, want empty", got)
	}

	var pty strings.Builder
	appendPTYSessionExitHint(&pty, sessionOutputHintFacts{})
	if got := pty.String(); got != "" {
		t.Fatalf("pty helper hint = %q, want empty", got)
	}
}

func intPtr(v int) *int {
	return &v
}

func TestAppendWaitingInputSessionHintUsesFacts(t *testing.T) {
	var b strings.Builder
	appendWaitingInputSessionHint(&b, "s1", sessionOutputHintFacts{
		Status:            SessionWaitingInput,
		CompletionLevel:   CompletionIncomplete,
		StructuredSession: true,
	})
	if got := b.String(); !strings.Contains(got, "CodingSubAgent") || strings.Contains(got, "send_and_observe") {
		t.Fatalf("waiting incomplete hint = %q, want CodingSubAgent guidance without external continuation", got)
	}
}

func TestAppendTerminalSessionExitHintUsesFacts(t *testing.T) {
	var b strings.Builder
	exitCode := 2
	appendTerminalSessionExitHint(&b, sessionOutputHintFacts{
		Status:            SessionExited,
		ExitCode:          &exitCode,
		Tool:              "codex",
		StructuredSession: true,
	})
	if got := b.String(); !strings.Contains(got, "退出码 2") || !strings.Contains(got, "CodingSubAgent") || strings.Contains(got, "创建新会话") {
		t.Fatalf("structured exit hint = %q, want CodingSubAgent guidance without external retry", got)
	}
}
