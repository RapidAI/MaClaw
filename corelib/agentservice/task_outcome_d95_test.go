package agentservice

import (
	"strings"
	"testing"
)

// A done task whose reply denies a post the host verified gets the
// correction in front of the reply (run_5ecb9d6d said 「本次没有发布」 next
// to a confirmed CSDN post).
func TestApplyTaskOutcomeDoneCarriesNotice(t *testing.T) {
	run := &Run{}
	msg := &Message{Content: "本次没有发布或验证。", Metadata: map[string]string{
		TaskOutcomeKey:       TaskOutcomeDone,
		TaskOutcomeNoticeKey: "更正：已发布并确认 —— blog.csdn.net https://blog.csdn.net/x/article/details/1",
	}}
	if got := applyTaskOutcome(run, msg); got != TaskOutcomeDone {
		t.Fatalf("outcome %q", got)
	}
	if run.WaitingForUser {
		t.Fatal("a done task does not wait for the user")
	}
	if !strings.HasPrefix(msg.Content, "更正：已发布并确认") || !strings.Contains(msg.Content, "本次没有发布") {
		t.Fatalf("content %q", msg.Content)
	}
	// Applied once only.
	applyTaskOutcome(run, msg)
	if strings.Count(msg.Content, "更正") != 1 {
		t.Fatalf("notice twice: %q", msg.Content)
	}
}
