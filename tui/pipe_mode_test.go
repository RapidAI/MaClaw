package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func capturePipeStderrForTest(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll stderr: %v", err)
	}
	_ = r.Close()
	return string(out)
}

func TestSilencePipeModeDiagnosticsSuppressesStandardLog(t *testing.T) {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	got := capturePipeStderrForTest(t, func() {
		restore := silencePipeModeDiagnostics()
		defer restore()
		log.Print("[LLM-stream] noisy request log")
		fmt.Fprintln(os.Stderr, "[steering] noisy stderr log")
	})
	if got != "" {
		t.Fatalf("pipe diagnostics wrote standard log to stderr: %q", got)
	}
}

func TestPipeCallbacksQuietSuppressesProgressAndToolLines(t *testing.T) {
	cb := &pipeCallbacks{
		app:   &TUIApp{toolRegistry: agent.NewCoreToolRegistry()},
		quiet: true,
	}

	got := capturePipeStderrForTest(t, func() {
		cb.OnProgress("[LLM-stream] noisy progress")
		_ = cb.ExecuteTool("missing_tool", "{}")
	})
	if got != "" {
		t.Fatalf("quiet pipe callbacks wrote stderr: %q", got)
	}
}

func TestPipeCallbacksVerboseStillReportsProgressAndToolLines(t *testing.T) {
	cb := &pipeCallbacks{
		app: &TUIApp{toolRegistry: agent.NewCoreToolRegistry()},
	}

	got := capturePipeStderrForTest(t, func() {
		cb.OnProgress("visible progress")
		_ = cb.ExecuteTool("missing_tool", "{}")
	})
	if got == "" {
		t.Fatal("verbose pipe callbacks should write progress/tool lines")
	}
}
