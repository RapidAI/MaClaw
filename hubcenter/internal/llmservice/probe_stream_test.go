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
	// The same shape the live Google parser accepts: one data line per event,
	// the functionCall repeated on the finishReason chunk, and [DONE] without
	// a blank line in front of it.
	googleRepeat := "" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"},{\"thought\":true,\"text\":\"why\"},{\"functionCall\":{\"name\":\"lookup\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig\"}]}}]}\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig\"}]},\"finishReason\":\"STOP\"}]}\n" +
		"data: [DONE]\n"
	repeated := ParseProbeStream([]byte(googleRepeat), "google")
	if !repeated.End || repeated.Reply != "hello" || len(repeated.Calls) != 1 || repeated.Calls[0].Name != "lookup" || repeated.Calls[0].Arguments != `{"q":"x"}` {
		t.Fatalf("google repeat=%#v", repeated)
	}
	googleTwo := ParseProbeStream([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"ping\",\"args\":{\"q\":\"a\"}}},{\"functionCall\":{\"name\":\"ping\",\"args\":{\"q\":\"b\"}}}]},\"finishReason\":\"STOP\"}]}\n\n"), "google")
	if !googleTwo.End || len(googleTwo.Calls) != 2 || googleTwo.Calls[0].Arguments != `{"q":"a"}` || googleTwo.Calls[1].Arguments != `{"q":"b"}` {
		t.Fatalf("google two=%#v", googleTwo)
	}
	tight := ParseProbeStream([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\ndata: {\"choices\":[{\"delta\":{\"content\":\"ng\"}}]}\ndata: [DONE]\n"), "openai")
	if !tight.End || tight.Reply != "pong" {
		t.Fatalf("tight openai=%#v", tight)
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
	// A non-stream JSON body is still not a stream, even with a legal finish_reason.
	finishedJSON := ParseProbeStream([]byte(`{"choices":[{"message":{"content":"pong"},"finish_reason":"stop"}]}`), "openai")
	if finishedJSON.End || finishedJSON.Err != "" || finishedJSON.Reply != "" || finishedJSON.Failure() != "stream ended before the end marker" {
		t.Fatalf("json completion with finish_reason=%#v", finishedJSON)
	}
}

// TestParseProbeStreamLiveFields locks the fields the live stream parsers
// already treat as text or a tool call. data: [DONE] ends an OpenAI stream.
// A legal finish_reason does too, because ParseProbeStream runs only after a
// clean read. An unknown reason, or text with no reason, stays unfinished.
func TestParseProbeStreamLiveFields(t *testing.T) {
	marker := "stream ended before the end marker"
	check := func(name, kind, body string, end bool, reply, failure string, calls int, callName, callArgs string) {
		t.Helper()
		got := ParseProbeStream([]byte(body), kind)
		if got.End != end || got.Reply != reply || got.Failure() != failure || len(got.Calls) != calls {
			t.Fatalf("%s=%#v", name, got)
		}
		if calls == 0 {
			return
		}
		if got.Calls[0].Name != callName || got.Calls[0].Arguments != callArgs {
			t.Fatalf("%s call=%#v", name, got.Calls[0])
		}
	}

	check("thinking", "openai",
		"data: {\"choices\":[{\"delta\":{\"thinking\":\"plan\"}}]}\n\ndata: [DONE]\n\n",
		true, "plan", "", 0, "", "")
	check("reasoning aliases", "openai",
		"data: {\"choices\":[{\"delta\":{\"reasoning\":\"First \"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"thinking\":\"second\"}}]}\n\n"+
			"data: [DONE]\n\n",
		true, "First second", "", 0, "", "")
	check("thinking stays behind content", "openai",
		"data: {\"choices\":[{\"delta\":{\"reasoning\":\"First \"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"thinking\":\"second\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"Done.\"}}]}\n\n"+
			"data: [DONE]\n\n",
		true, "Done.", "", 0, "", "")
	check("legacy function_call", "openai",
		"data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"bash\",\"arguments\":\"\"}}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\"{\\\"command\\\"\"}}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\":\\\"dir\\\"}\"}},\"finish_reason\":\"function_call\"}]}\n\n"+
			"data: [DONE]\n\n",
		true, "", "", 1, "bash", `{"command":"dir"}`)
	check("legacy without done", "openai",
		"data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}}]}\n\n",
		false, "", marker, 1, "bash", `{"command":"dir"}`)
	check("tool_calls wins", "openai",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"ping\",\"arguments\":\"{}\"}}],\"function_call\":{\"name\":\"bash\",\"arguments\":\"{}\"}}}]}\n\ndata: [DONE]\n\n",
		true, "", "", 1, "ping", "{}")
	check("finish_reason stop closes", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n",
		true, "pong", "", 0, "", "")
	check("finish_reason on the last chunk", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":null}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		true, "pong", "", 0, "", "")
	// Typhoon's documented chunks: a role delta with a null reason, then an
	// empty delta whose finish_reason is stop, and no data: [DONE]. A missing
	// blank line must not hide that stop chunk.
	check("typhoon stop without done", "openai",
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"pong\"},\"finish_reason\":null}]}\n"+
			"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n",
		true, "pong", "", 0, "", "")
	check("typhoon tool_calls fragments", "openai",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"ping\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n",
		true, "", "", 1, "ping", "{}")
	check("finish_reason tool_calls closes", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"ping\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
		true, "hello", "", 1, "ping", "{}")
	check("finish_reason length closes", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"length\"}]}\n\n",
		true, "pong", "", 0, "", "")
	check("finish_reason content_filter closes", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"content_filter\"}]}\n\n",
		true, "pong", "", 0, "", "")
	check("legacy finish_reason closes", "openai",
		"data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"bash\",\"arguments\":\"\"}}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\"{\\\"command\\\"\"}}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\":\\\"dir\\\"}\"}},\"finish_reason\":\"function_call\"}]}\n\n",
		true, "", "", 1, "bash", `{"command":"dir"}`)
	check("finish_reason null is not the end", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":null}]}\n\n",
		false, "pong", marker, 0, "", "")
	check("finish_reason error is not the end", "openai",
		"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"error\"}]}\n\n",
		false, "pong", marker, 0, "", "")
	check("finish_reason stop without content", "openai",
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		true, "", "model returned no completion content", 0, "", "")

	check("anthropic reasoning_delta", "anthropic",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"reasoning_delta\",\"text\":\"why\"}}\n\nevent: message_stop\n\n",
		true, "why", "", 0, "", "")
	check("anthropic reasoning_delta thinking wins", "anthropic",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"reasoning_delta\",\"thinking\":\"a\",\"text\":\"b\"}}\n\nevent: message_stop\n\n",
		true, "a", "", 0, "", "")
	check("anthropic thinking block text", "anthropic",
		"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"thinking\",\"text\":\"why\"}}\n\nevent: message_stop\n\n",
		true, "why", "", 0, "", "")

	check("responses done text", "responses",
		"data: {\"type\":\"response.output_text.done\",\"text\":\"pong\"}\n\ndata: {\"type\":\"response.completed\"}\n\n",
		true, "pong", "", 0, "", "")
	check("responses done does not double", "responses",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n\n"+
			"data: {\"type\":\"response.output_text.done\",\"text\":\"pong\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\"}]}]}}\n\n",
		true, "pong", "", 0, "", "")
	check("responses completed content field", "responses",
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"content\":\"pong\"}]}]}}\n\n",
		true, "pong", "", 0, "", "")
	check("responses done tool", "responses",
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"name\":\"ping\",\"arguments\":\"{}\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"arguments\":\"{}\"}\n\n"+
			"data: {\"type\":\"response.completed\"}\n\n",
		true, "", "", 1, "ping", "{}")
	check("responses completed adds the tool", "responses",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\"}]},{\"type\":\"function_call\",\"name\":\"ping\",\"arguments\":\"{}\"}]}}\n\n",
		true, "pong", "", 1, "ping", "{}")
	check("responses reasoning delta", "responses",
		"data: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"why\"}\n\nevent: response.completed\n\n",
		true, "why", "", 0, "", "")
	check("responses reasoning part does not double", "responses",
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"why\"}\n\n"+
			"data: {\"type\":\"response.reasoning_summary_part.added\",\"part\":{\"text\":\"why\"}}\n\n"+
			"data: {\"type\":\"response.completed\"}\n\n",
		true, "why", "", 0, "", "")
	check("responses done extends arguments", "responses",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"name\":\"ping\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{\"}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"arguments\":\"{\\\"a\\\":1}\"}\n\n"+
			"data: {\"type\":\"response.completed\"}\n\n",
		true, "", "", 1, "ping", `{"a":1}`)
	check("responses failed event", "responses",
		"event: response.reasoning_summary_text.delta\ndata: {\"delta\":\"Thinking about inputs.\"}\n\n"+
			"event: response.output_text.delta\ndata: {\"delta\":\"partial answer\"}\n\n"+
			"event: response.failed\ndata: {\"response\":{\"error\":{\"message\":\"provider overloaded\",\"code\":\"overloaded\"}}}\n\n",
		false, "partial answer", "model returned an error: provider overloaded", 0, "", "")
	check("responses completed null error", "responses",
		"data: {\"type\":\"response.completed\",\"response\":{\"error\":null,\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\"}]}]}}\n\n",
		true, "pong", "", 0, "", "")
	check("responses stale done keeps arguments", "responses",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"name\":\"ping\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{\\\"a\\\":1}\"}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"arguments\":\"{\"}\n\n"+
			"data: {\"type\":\"response.completed\"}\n\n",
		true, "", "", 1, "ping", `{"a":1}`)
}
