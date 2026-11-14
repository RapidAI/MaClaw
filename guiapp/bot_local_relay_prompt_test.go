package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/localrelay"
)

func TestSettleLocalRelayPromptsUnblocksTheQuestion(t *testing.T) {
	ch := make(chan localrelay.Answer, 1)
	localRelayState.mu.Lock()
	if localRelayState.pending == nil {
		localRelayState.pending = map[string]localRelayPending{}
	}
	localRelayState.pending["prompt-full"] = localRelayPending{answer: ch}
	localRelayState.mu.Unlock()
	t.Cleanup(func() {
		localRelayState.mu.Lock()
		delete(localRelayState.pending, "prompt-full")
		localRelayState.mu.Unlock()
	})

	// The waiter locks the same mutex while finishing, as askLocalRelay does.
	got := make(chan localrelay.Answer, 1)
	go func() {
		ans := <-ch
		localRelayState.mu.Lock()
		delete(localRelayState.pending, "prompt-full")
		localRelayState.mu.Unlock()
		got <- ans
	}()
	settleLocalRelayPrompts(localrelay.AnswerOnce)
	select {
	case ans := <-got:
		if ans != localrelay.AnswerOnce {
			t.Fatalf("answer %q", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("the question stayed open")
	}
}
