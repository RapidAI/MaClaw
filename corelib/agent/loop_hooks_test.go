package agent

import "testing"

// recordingHook records what it saw so a test can prove it ran.
type recordingHook struct {
	name       string
	transforms int
	toolCalls  int
	emptyVote  bool
}

func (h *recordingHook) TransformConversation(conversation []interface{}) []interface{} {
	h.transforms++
	return append(conversation, map[string]string{"role": "system", "content": h.name})
}

func (h *recordingHook) OnToolExecuted(string, string, string, bool) { h.toolCalls++ }

func (h *recordingHook) OnEmptyResponse(int) bool { return h.emptyVote }

func TestResolveLoopHooksAppliesEveryHook(t *testing.T) {
	first := &recordingHook{name: "first", emptyVote: true}
	second := &recordingHook{name: "second", emptyVote: true}

	h := resolveLoopHooks([]LoopHooks{first, second})

	out := h.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "hi"}})
	if len(out) != 3 {
		t.Fatalf("expected both hooks to append, got %d messages: %v", len(out), out)
	}
	if first.transforms != 1 || second.transforms != 1 {
		t.Fatalf("each hook should transform once, got first=%d second=%d", first.transforms, second.transforms)
	}
}

// TestResolveLoopHooksThreadsOutput checks the second hook sees what the first
// produced. Dropping all but the first hook — the previous behaviour — would
// silently disable any hook passed after it.
func TestResolveLoopHooksThreadsOutput(t *testing.T) {
	first := &recordingHook{name: "one", emptyVote: true}
	seen := -1
	second := &countingHook{onTransform: func(conversation []interface{}) {
		seen = len(conversation)
	}}

	h := resolveLoopHooks([]LoopHooks{first, second})
	h.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "hi"}})

	if seen != 2 {
		t.Fatalf("second hook should see the first hook's output (2 messages), saw %d", seen)
	}
}

type countingHook struct {
	onTransform func(conversation []interface{})
}

func (h *countingHook) TransformConversation(conversation []interface{}) []interface{} {
	h.onTransform(conversation)
	return nil
}
func (h *countingHook) OnToolExecuted(string, string, string, bool) {}
func (h *countingHook) OnEmptyResponse(int) bool                    { return true }

func TestResolveLoopHooksSkipsNil(t *testing.T) {
	only := &recordingHook{name: "only", emptyVote: true}
	h := resolveLoopHooks([]LoopHooks{nil, only, nil})

	// A single live hook must keep behaving exactly as it did before the chain
	// existed: one call, its own return value.
	out := h.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "hi"}})
	if len(out) != 2 || only.transforms != 1 {
		t.Fatalf("single hook should be passed through unchanged, got %d messages %d transforms", len(out), only.transforms)
	}
}

func TestResolveLoopHooksEmptyUsesDefault(t *testing.T) {
	h := resolveLoopHooks(nil)
	if out := h.TransformConversation([]interface{}{}); out != nil {
		t.Fatal("DefaultLoopHooks must return nil")
	}
	if h.OnEmptyResponse(0) {
		t.Fatal("DefaultLoopHooks must decline to retry an empty response")
	}
}

// TestLoopHooksChainEmptyResponseOneVoteToExit keeps an empty model reply
// terminal: a hook voting to continue must not override one voting to exit.
func TestLoopHooksChainEmptyResponseOneVoteToExit(t *testing.T) {
	h := resolveLoopHooks([]LoopHooks{
		&recordingHook{emptyVote: true},
		&recordingHook{emptyVote: false},
	})
	if h.OnEmptyResponse(1) {
		t.Fatal("a hook voting to exit must win")
	}
}

func TestLoopHooksChainFansOutToolEvents(t *testing.T) {
	first := &recordingHook{emptyVote: true}
	second := &recordingHook{emptyVote: true}
	h := resolveLoopHooks([]LoopHooks{first, second})

	h.OnToolExecuted("shell", "{}", "ok", true)
	if first.toolCalls != 1 || second.toolCalls != 1 {
		t.Fatalf("every hook should see the tool event, got first=%d second=%d", first.toolCalls, second.toolCalls)
	}
}
