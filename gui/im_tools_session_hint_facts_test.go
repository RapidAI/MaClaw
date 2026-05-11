package main

import "testing"

func TestCollectSessionOutputHintFacts(t *testing.T) {
	exitCode := 1
	resumeContext := &SessionResumeContext{ResumeCount: 2}
	session := &RemoteSession{
		Status:            SessionBusy,
		Tool:              "codex",
		ExitCode:          &exitCode,
		StallState:        StallStateSuspected,
		CompletionLevel:   CompletionIncomplete,
		AutoContinueCount: 3,
		Exec:              &CodexSDKExecutionHandle{},
		ResumeContext:     resumeContext,
	}
	rawLines := []string{"working", "API retry scheduled"}

	facts := collectSessionOutputHintFacts(session, SessionBusy, rawLines)
	if !facts.Status.IsBusy() {
		t.Fatalf("Status = %q, want busy", facts.Status)
	}
	if !facts.HasAPIRetry || !facts.HasRecentTransientAPI {
		t.Fatalf("API marker facts = retry:%v transient:%v, want both true", facts.HasAPIRetry, facts.HasRecentTransientAPI)
	}
	if facts.StallState != StallStateSuspected {
		t.Fatalf("StallState = %v, want %v", facts.StallState, StallStateSuspected)
	}
	if facts.CompletionLevel != CompletionIncomplete {
		t.Fatalf("CompletionLevel = %v, want %v", facts.CompletionLevel, CompletionIncomplete)
	}
	if facts.AutoContinueCount != 3 {
		t.Fatalf("AutoContinueCount = %d, want 3", facts.AutoContinueCount)
	}
	if !facts.StructuredSession {
		t.Fatal("StructuredSession = false, want true for Codex execution mode")
	}
	if facts.ExitCode == nil || *facts.ExitCode != 1 {
		t.Fatalf("ExitCode = %#v, want 1", facts.ExitCode)
	}
	if facts.Tool != "codex" {
		t.Fatalf("Tool = %q, want codex", facts.Tool)
	}
	if facts.ResumeContext != resumeContext || facts.ResumeCount() != 2 {
		t.Fatalf("resume facts = %#v count=%d, want provided context count 2", facts.ResumeContext, facts.ResumeCount())
	}
}

func TestCollectSessionOutputHintFactsNilSession(t *testing.T) {
	facts := collectSessionOutputHintFacts(nil, SessionWaitingInput, nil)
	if !facts.Status.IsWaitingInput() {
		t.Fatalf("Status = %q, want waiting_input", facts.Status)
	}
	if facts.HasAPIRetry || facts.HasRecentTransientAPI || facts.StructuredSession {
		t.Fatalf("nil session should only expose normalized status, got %#v", facts)
	}
}

func TestSessionOutputHintFactsExitPredicates(t *testing.T) {
	exitCode := 1
	facts := sessionOutputHintFacts{
		Status:            SessionExited,
		ExitCode:          &exitCode,
		StructuredSession: true,
	}
	if !facts.HasNonZeroTerminalExit() {
		t.Fatal("non-zero terminal exit should be true")
	}
	if facts.HasStructuredNormalExitWithPossibleUnfinishedWork() {
		t.Fatal("non-zero exit should not be normal structured exit")
	}

	exitCode = 0
	if facts.HasNonZeroTerminalExit() {
		t.Fatal("zero exit should not be non-zero terminal exit")
	}
	if !facts.HasStructuredNormalExitWithPossibleUnfinishedWork() {
		t.Fatal("zero structured exited session should be possible unfinished normal exit")
	}
}

func TestCollectSessionOutputHintFactsOnlyScansFatalErrorsForNonZeroTerminalExit(t *testing.T) {
	rawLines := []string{"authentication failed"}
	running := &RemoteSession{Status: SessionBusy}
	runningFacts := collectSessionOutputHintFacts(running, SessionBusy, rawLines)
	if runningFacts.FatalSessionError {
		t.Fatal("running session should not scan fatal error hints")
	}

	exitCode := 2
	exited := &RemoteSession{Status: SessionExited, ExitCode: &exitCode}
	exitedFacts := collectSessionOutputHintFacts(exited, SessionExited, rawLines)
	if !exitedFacts.FatalSessionError {
		t.Fatal("non-zero terminal exit should scan fatal error hints")
	}
}
