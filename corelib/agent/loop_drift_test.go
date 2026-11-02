package agent

import "testing"

func repeatToolCalls(name, args, result string, success, poll bool, n int) []toolCallRecord {
	tail := make([]toolCallRecord, n)
	for i := range tail {
		tail[i] = toolCallRecord{name: name, args: args, result: result, success: success, poll: poll}
	}
	return tail
}

func TestDriftFailureHintLeavesSuccessfulPollAlone(t *testing.T) {
	tail := repeatToolCalls("ssh", `{"command":"status"}`, "running", true, true, 4)
	if driftFailureHint(tail) {
		t.Fatal("a successful status poll must stay quiet")
	}
}

func TestDriftFailureHintOnIdenticalFailure(t *testing.T) {
	tail := repeatToolCalls("office", `{"path":"deck.pptx"}`, "[system rejected] write failed", false, false, 4)
	if !driftFailureHint(tail) {
		t.Fatal("an identical failed window must hint")
	}
}

func TestRepeatedCommitIgnoresAPollAndASingleWrite(t *testing.T) {
	polls := repeatToolCalls("ssh", `{"command":"status"}`, "running", true, true, 3)
	if _, seen := repeatedSuccessfulCommit(polls); seen != 0 {
		t.Fatalf("polls are not commits: seen=%d", seen)
	}
	once := repeatToolCalls("office", `{"path":"deck.pptx","theme":"clean"}`, "published", true, false, 1)
	if _, seen := repeatedSuccessfulCommit(once); seen != 1 {
		t.Fatalf("one commit is the work, not a repeat: seen=%d", seen)
	}
}

func TestRepeatedCommitCountsSamePayloadDespiteKeyOrder(t *testing.T) {
	published := "Office document artifact published. The host attaches it to this chat. This is not a send."
	calls := []toolCallRecord{
		{name: "office", args: `{"path":"deck.pptx","theme":"clean"}`, result: published, success: true},
		{name: "bash", args: `{"command":"ls"}`, result: "ok", success: true, poll: true},
		{name: "office", args: `{"theme":"clean","path":"deck.pptx"}`, result: published, success: true},
	}
	id, seen := repeatedSuccessfulCommit(calls)
	if seen != 2 || id == "" {
		t.Fatalf("key order must not hide the second commit: id=%q seen=%d", id, seen)
	}
	calls = append(calls, toolCallRecord{name: "office", args: `{"path":"deck.pptx","theme":"clean"}`, result: published, success: true})
	if _, seen = repeatedSuccessfulCommit(calls); seen != 3 {
		t.Fatalf("third identical commit seen=%d", seen)
	}
}

func TestCommitPayloadLandedIgnoresKeyOrder(t *testing.T) {
	key, ok := commitPayloadKey("office", `{"path":"deck.pptx","theme":"clean"}`)
	if !ok {
		t.Fatal("office arguments must have a payload key")
	}
	landed := map[string]struct{}{key: {}}
	if !commitPayloadLanded(landed, "office", `{"theme":"clean","path":"deck.pptx"}`) {
		t.Fatal("the same payload must count as already committed")
	}
	if commitPayloadLanded(landed, "office", `{"path":"other.pptx","theme":"clean"}`) {
		t.Fatal("a different path is a new commit")
	}
	if commitPayloadLanded(nil, "ssh", `{"command":"status"}`) {
		t.Fatal("nothing has landed")
	}
}

func TestRepeatedCommitResetsWhenThePayloadChanges(t *testing.T) {
	calls := repeatToolCalls("write_file", `{"path":"a.go"}`, "wrote a.go", true, false, 2)
	calls = append(calls, toolCallRecord{name: "write_file", args: `{"path":"b.go"}`, result: "wrote b.go", success: true})
	_, seen := repeatedSuccessfulCommit(calls)
	if seen != 1 {
		t.Fatalf("the latest different write is a new commit: seen=%d", seen)
	}
}
