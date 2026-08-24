package main

import "testing"

func TestAIAssistantStreamEventEmitterSequencesInterleavedTraffic(t *testing.T) {
	emitter := newAIAssistantStreamEventEmitter(nil, "req-coding", "desktop-user:D:/project")
	thought := emitter.payload("\x01Inspect the existing implementation.")
	tool := emitter.payload("Coding Agent Event: {\"agent\":\"coding\",\"event\":\"tool_started\"}")
	secondThought := emitter.payload("\x01Apply the focused edit.")
	secondTool := emitter.payload("Coding Agent Event: {\"agent\":\"coding\",\"event\":\"tool_finished\"}")

	got := []uint64{thought.Sequence, tool.Sequence, secondThought.Sequence, secondTool.Sequence}
	want := []uint64{1, 2, 3, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sequence[%d] = %d, want %d (got=%v)", i, got[i], want[i], got)
		}
	}
	if tool.RequestID != "req-coding" || tool.SessionKey != "desktop-user:D:/project" {
		t.Fatalf("payload routing lost: %#v", tool)
	}
}
