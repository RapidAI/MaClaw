package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDesktopWaitPollKinds(t *testing.T) {
	cases := map[string]int{
		"sleep 85; tail -n 3 train.log":             2,
		"while [ ! -f done ]; do sleep 5; done":     2,
		"sleep 10 && tail -5 log.txt":               1,
		"sleep 20; ssh box 'tail -n2 /w/train.log'": 1,
		"sleep 2; tail log":                         0,
		"sleep 10":                                  0,
		"python train.py --epochs 3":                0,
		"sleep 5 && grep -c step log && nvidia-smi": 1,
	}
	for cmd, want := range cases {
		if got := desktopWaitPollKind(cmd); got != want {
			t.Errorf("%q: kind %d, want %d", cmd, got, want)
		}
	}
}

func TestDesktopWaitPollNoteAfterRepeatedShortPolls(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-waitpoll-short-" + t.Name()}
	cmd := "sleep 15; tail -n 2 train.log"
	if desktopWaitPollNote(scope, cmd) != "" || desktopWaitPollNote(scope, cmd) != "" {
		t.Fatal("two short polls are not yet a pattern")
	}
	n := desktopWaitPollNote(scope, cmd)
	if !strings.Contains(n, "background=true") {
		t.Fatalf("third short poll without a watched job must say how to hand it off, got %q", n)
	}
	if desktopWaitPollNote(scope, cmd) != "" {
		t.Fatal("the unwatched-process note comes once")
	}
}

func TestDesktopWaitPollNoteSkipsOrdinaryBash(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-waitpoll-plain-" + t.Name()}
	for i := 0; i < 5; i++ {
		if n := desktopWaitPollNote(scope, "ls -la && cat README"); n != "" {
			t.Fatalf("no sleep, no note: %q", n)
		}
	}
}
