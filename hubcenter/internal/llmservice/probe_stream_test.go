package llmservice

import "testing"

func TestParseProbeStreamProtocols(t *testing.T) {
	openai := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ng\",\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"ping\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	got := ParseProbeStream([]byte(openai), "openai")
	if !got.End || got.Err != "" || got.Reply != "pong" || len(got.Calls) != 1 || got.Calls[0].Name != "ping" || got.Calls[0].Arguments != "{}" {
		t.Fatalf("openai=%#v", got)
	}
	partial := ParseProbeStream([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n"), "openai")
	if partial.End || partial.Reply != "po" {
		t.Fatalf("partial=%#v", partial)
	}
	toolsOnly := ParseProbeStream([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"ping\",\"arguments\":\"{}\"}}]}}]}\n\ndata: [DONE]\n\n"), "")
	if !toolsOnly.End || toolsOnly.Reply != "" || len(toolsOnly.Calls) != 1 {
		t.Fatalf("tools only=%#v", toolsOnly)
	}

	anthropic := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"name\":\"ping\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{}\"}}\n\n" +
		"event: message_stop\n\n"
	got = ParseProbeStream([]byte(anthropic), "anthropic")
	if !got.End || got.Reply != "pong" || len(got.Calls) != 1 || got.Calls[0].Name != "ping" || got.Calls[0].Arguments != "{}" {
		t.Fatalf("anthropic=%#v", got)
	}
	typedStop := ParseProbeStream([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"), "anthropic")
	if !typedStop.End || typedStop.Reply != "pong" {
		t.Fatalf("typed stop=%#v", typedStop)
	}
	done := ParseProbeStream([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\ndata: [DONE]\n\n"), "anthropic")
	if !done.End || done.Reply != "pong" {
		t.Fatalf("anthropic done=%#v", done)
	}

	responses := "" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"name\":\"ping\"}}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"delta\":\"{}\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\"}\n\n"
	got = ParseProbeStream([]byte(responses), "responses")
	if !got.End || got.Reply != "pong" || len(got.Calls) != 1 || got.Calls[0].Name != "ping" || got.Calls[0].Arguments != "{}" {
		t.Fatalf("responses=%#v", got)
	}
	incomplete := ParseProbeStream([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"po\"}\n\nevent: response.incomplete\n\n"), "responses")
	if !incomplete.End || incomplete.Reply != "po" {
		t.Fatalf("incomplete=%#v", incomplete)
	}

	google := "data: " + `{"candidates":[{"content":{"parts":[{"text":"pong"},{"functionCall":{"name":"ping","args":{}}}]},"finishReason":"STOP"}]}` + "\n\n"
	got = ParseProbeStream([]byte(google), "google")
	if !got.End || got.Reply != "pong" || len(got.Calls) != 1 || got.Calls[0].Name != "ping" {
		t.Fatalf("google=%#v", got)
	}
	googleDone := ParseProbeStream([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pong\"}]}}]}\n\ndata: [DONE]\n\n"), "google")
	if !googleDone.End || googleDone.Reply != "pong" {
		t.Fatalf("google done=%#v", googleDone)
	}

	broken := ParseProbeStream([]byte("data: {\"error\":{\"message\":\"boom\"}}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"), "openai")
	if broken.End || broken.Err != "boom" || broken.Reply != "x" || broken.Failure() != "model returned an error: boom" {
		t.Fatalf("error=%#v", broken)
	}
	bare := ParseProbeStream([]byte(`{"error":{"message":"nope"}}`), "openai")
	if bare.End || bare.Err != "nope" || bare.Reply != "" || bare.Failure() != "model returned an error: nope" {
		t.Fatalf("bare json error=%#v", bare)
	}
	completion := ParseProbeStream([]byte(`{"choices":[{"message":{"content":"pong"}}]}`), "openai")
	if completion.End || completion.Err != "" || completion.Reply != "" || completion.Failure() != "stream ended before the end marker" {
		t.Fatalf("json completion=%#v", completion)
	}
}
