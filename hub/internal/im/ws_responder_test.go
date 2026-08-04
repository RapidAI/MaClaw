package im

import (
	"encoding/json"
	"testing"
)

func TestWSAgentResponderAcceptsWrappedResponse(t *testing.T) {
	router, pending := pendingVoiceTestRouter("wrapped")
	responder := &WSAgentResponder{Router: router}
	responder.HandleAgentResponse("wrapped", json.RawMessage(`{"response":{"text":"wrapped result"}}`))

	resp := <-pending.ResponseCh
	if resp.Text != "wrapped result" {
		t.Fatalf("wrapped response=%#v", resp)
	}
}

func TestWSAgentResponderAcceptsDirectResponse(t *testing.T) {
	router, pending := pendingVoiceTestRouter("direct")
	responder := &WSAgentResponder{Router: router}
	responder.HandleAgentResponse("direct", json.RawMessage(`{"text":"direct result"}`))

	resp := <-pending.ResponseCh
	if resp.Text != "direct result" {
		t.Fatalf("direct response=%#v", resp)
	}
}

func TestWSAgentResponderRejectsNullWrappedResponse(t *testing.T) {
	router, pending := pendingVoiceTestRouter("null")
	responder := &WSAgentResponder{Router: router}
	responder.HandleAgentResponse("null", json.RawMessage(`{"response":null}`))

	select {
	case resp := <-pending.ResponseCh:
		t.Fatalf("null wrapper must not commit an empty result: %#v", resp)
	default:
	}
}
