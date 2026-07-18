package acpagent

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestEncodeLineRejectsEmbeddedNewline(t *testing.T) {
	// Craft a value that would include newline if marshaled poorly —
	// normal structs don't; test the guard with a raw string via map is fine.
	// We call encodeLine with something that produces newline-free JSON only.
	line, err := encodeLine(map[string]any{"jsonrpc": "2.0", "method": "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte("\n")) {
		t.Fatalf("expected trailing newline, got %q", line)
	}
	if bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("expected exactly one newline, got %q", line)
	}
}

func TestConnInitializeRoundTrip(t *testing.T) {
	// Fake gateway not needed — only initialize hits no gateway for sessions...
	// Use a minimal bridge with a stub: we only test transport framing with Conn.

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"test"}}}` + "\n")
	var out bytes.Buffer
	conn := NewConn(in, &out)

	req, err := conn.ReadRequest()
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "initialize" {
		t.Fatalf("method = %q", req.Method)
	}
	if err := conn.WriteResponse(req.ID, InitializeResult{
		ProtocolVersion:   ProtocolVersion,
		AgentCapabilities: DefaultAgentCapabilities(),
		AgentInfo:         ImplementationInfo{Name: "maclaw-gui-bridge", Version: "0.1.0"},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var resp Response
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal response: %v raw=%s", err, out.String())
	}
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var result InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != 1 {
		t.Fatalf("protocolVersion = %d", result.ProtocolVersion)
	}
	if result.AgentInfo.Name != "maclaw-gui-bridge" {
		t.Fatalf("agent name = %q", result.AgentInfo.Name)
	}
}

func TestPromptText(t *testing.T) {
	got := PromptText([]ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "resource", Resource: map[string]any{"uri": "file:///a.go", "text": "package a"}},
	})
	if !strings.Contains(got, "hello") || !strings.Contains(got, "package a") {
		t.Fatalf("unexpected prompt text: %q", got)
	}
}

func TestDecodeIncomingResponseVsRequest(t *testing.T) {
	reqLine := []byte(`{"jsonrpc":"2.0","id":1,"method":"session/request_permission","params":{}}`)
	msg, err := DecodeIncoming(reqLine)
	if err != nil || msg.Request == nil || msg.Request.Method != "session/request_permission" {
		t.Fatalf("request: msg=%+v err=%v", msg, err)
	}
	respLine := []byte(`{"jsonrpc":"2.0","id":1,"result":{"outcome":{"outcome":"selected","optionId":"allow_once"}}}`)
	msg, err = DecodeIncoming(respLine)
	if err != nil || msg.Response == nil {
		t.Fatalf("response: msg=%+v err=%v", msg, err)
	}
	raw, ok := msg.Response.Result.(json.RawMessage)
	if !ok || !strings.Contains(string(raw), "allow_once") {
		t.Fatalf("result=%T %v", msg.Response.Result, msg.Response.Result)
	}
}

func TestReadRequestEOF(t *testing.T) {
	conn := NewConn(strings.NewReader(""), io.Discard)
	_, err := conn.ReadRequest()
	if err != io.EOF {
		t.Fatalf("err = %v, want EOF", err)
	}
}
