package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseRPCModeFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"no args", []string{"maclaw-tui"}, false},
		{"--mode rpc", []string{"maclaw-tui", "--mode", "rpc"}, true},
		{"--mode=rpc", []string{"maclaw-tui", "--mode=rpc"}, true},
		{"--mode json (not rpc)", []string{"maclaw-tui", "--mode", "json"}, false},
		{"-p prompt (not rpc)", []string{"maclaw-tui", "-p", "hello"}, false},
		{"rpc without --mode", []string{"maclaw-tui", "rpc"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = tc.args
			got := parseRPCModeFlag()
			if got != tc.want {
				t.Errorf("parseRPCModeFlag(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestRPCRequestUnmarshal(t *testing.T) {
	cases := []struct {
		input string
		want  RPCRequest
	}{
		{`{"type":"prompt","id":"req-1","text":"hello"}`, RPCRequest{Type: "prompt", ID: "req-1", Text: "hello"}},
		{`{"type":"abort","id":"req-2"}`, RPCRequest{Type: "abort", ID: "req-2"}},
		{`{"type":"shutdown"}`, RPCRequest{Type: "shutdown"}},
	}

	for _, tc := range cases {
		var got RPCRequest
		if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("unmarshal %q = %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestRPCEventMarshal(t *testing.T) {
	event := RPCEvent{
		Type:      "done",
		RequestID: "req-1",
		Text:      "hello world",
		Usage:     &RPCUsage{InputTokens: 100, OutputTokens: 50},
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundtrip RPCEvent
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundtrip.Type != "done" || roundtrip.RequestID != "req-1" || roundtrip.Text != "hello world" {
		t.Errorf("roundtrip mismatch: %+v", roundtrip)
	}
	if roundtrip.Usage == nil || roundtrip.Usage.InputTokens != 100 || roundtrip.Usage.OutputTokens != 50 {
		t.Errorf("usage mismatch: %+v", roundtrip.Usage)
	}
}

func TestEmitRPCEvent_DoesNotPanic(t *testing.T) {
	// Verify emitRPCEvent doesn't panic even with nil fields.
	emitRPCEvent(RPCEvent{Type: "ready"})
	emitRPCEvent(RPCEvent{Type: "error", Message: "test error"})
	emitRPCEvent(RPCEvent{Type: "text_delta", RequestID: "x", Delta: ""})
}
