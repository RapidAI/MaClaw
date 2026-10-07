package httpapi

import (
	"strings"
	"testing"
)

func TestInjectCreditsDeductedSSE_StampsStopChunkAndKeepsTokens(t *testing.T) {
	raw := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	got := string(injectCreditsDeductedSSE(raw, 1.25))
	if !strings.Contains(got, `"credits_deducted":1.25`) && !strings.Contains(got, `"credits_deducted": 1.25`) {
		t.Fatalf("missing settled credits in %s", got)
	}
	if !strings.Contains(got, `"prompt_tokens":10`) || !strings.Contains(got, `[DONE]`) {
		t.Fatalf("stop chunk lost tokens or trailer: %s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "[DONE]") {
		// [DONE] stays after the stop event; the credit figure is on that event.
		idxCredits := strings.Index(got, "credits_deducted")
		idxDone := strings.Index(got, "[DONE]")
		if idxCredits < 0 || idxDone < 0 || idxCredits > idxDone {
			t.Fatalf("credits must be attached before [DONE]: %s", got)
		}
	}
}

func TestInjectCreditsDeductedSSE_MergesLaterUsageIntoStopChunk(t *testing.T) {
	raw := []byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n")
	got := string(injectCreditsDeductedSSE(raw, 0.5))
	stop := got
	if i := strings.Index(got, "\n\n"); i >= 0 {
		stop = got[:i]
	}
	if !strings.Contains(stop, `"credits_deducted":0.5`) || !strings.Contains(stop, `"prompt_tokens":10`) || !strings.Contains(stop, `"finish_reason":"stop"`) {
		t.Fatalf("stop chunk = %s\nfull = %s", stop, got)
	}
}

func TestInjectCreditsDeductedSSE_EmptyTailStillReportsDebit(t *testing.T) {
	got := string(injectCreditsDeductedSSE(nil, 0))
	if !strings.Contains(got, `"credits_deducted":0`) {
		t.Fatalf("got %s", got)
	}
}

func TestOpenAIStreamTailForwardsVisibleDeltaBeforeBilling(t *testing.T) {
	tail := &openAIStreamTail{}
	tail.markStarted()

	first := tail.retain([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"official \"}}]}\n\n"))
	if !strings.Contains(string(first), "official ") || tail.buf.Len() != 0 {
		t.Fatalf("content chunk should pass through, immediate=%s held=%s", first, tail.buf.Bytes())
	}

	stop := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"a<b\",\"reasoning_content\":\"think\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\n")
	immediate := tail.retain(stop)
	if !strings.Contains(string(immediate), "a<b") || !strings.Contains(string(immediate), "think") {
		t.Fatalf("last tokens were held: %s", immediate)
	}
	if strings.Contains(string(immediate), `"finish_reason":"stop"`) {
		t.Fatalf("forwarded delta still finishes the stream: %s", immediate)
	}
	held := tail.buf.String()
	if strings.Contains(held, "a<b") || strings.Contains(held, "think") || !strings.Contains(held, `"finish_reason":"stop"`) || !strings.Contains(held, `"prompt_tokens":2`) {
		t.Fatalf("held stop chunk = %s", held)
	}
	if got := tail.retain([]byte("data: [DONE]\n\n")); len(got) != 0 {
		t.Fatalf("[DONE] should wait with the stop chunk, got %s", got)
	}

	stamped := string(injectCreditsDeductedSSE(tail.buf.Bytes(), 1.25))
	stopEvent := stamped
	if i := strings.Index(stamped, "\n\n"); i >= 0 {
		stopEvent = stamped[:i]
	}
	if strings.Contains(stopEvent, "a<b") || strings.Contains(stopEvent, "think") || !strings.Contains(stopEvent, `"credits_deducted":1.25`) || !strings.Contains(stopEvent, `"finish_reason":"stop"`) || !strings.Contains(stopEvent, `"prompt_tokens":2`) {
		t.Fatalf("stamped stop = %s\nfull = %s", stopEvent, stamped)
	}
}

func TestOpenAIStreamTailDoesNotHoldToolFinish(t *testing.T) {
	tail := &openAIStreamTail{}
	raw := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"call\"},\"finish_reason\":\"tool_calls\"}]}\n\n")
	got := tail.retain(raw)
	if !strings.Contains(string(got), "call") || !strings.Contains(string(got), `"finish_reason":"tool_calls"`) || tail.buf.Len() != 0 {
		t.Fatalf("tool finish held: immediate=%s held=%s", got, tail.buf.Bytes())
	}
}

func TestInjectCreditsDeductedJSON_PreservesUsage(t *testing.T) {
	raw := []byte(`{"id":"c","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	got := string(injectCreditsDeductedJSON(raw, 2.5))
	if !strings.Contains(got, `"credits_deducted":2.5`) || !strings.Contains(got, `"prompt_tokens":3`) {
		t.Fatalf("got %s", got)
	}
}
