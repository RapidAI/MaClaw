package im

import (
	"encoding/json"
	"log"
)

// WSAgentResponder adapts MessageRouter to the ws.IMAgentResponseHandler
// interface, parsing the raw JSON payload into an AgentResponse.
type WSAgentResponder struct {
	Router *MessageRouter
}

// HandleAgentResponse parses the raw JSON payload and delegates to the
// MessageRouter.
func (w *WSAgentResponder) HandleAgentResponse(requestID string, raw json.RawMessage) {
	if w == nil || w.Router == nil {
		return
	}
	// json.Unmarshal accepts unknown fields, so decoding a direct AgentResponse
	// into the old wrapper struct succeeds with an empty Response. Inspect the
	// wrapper key explicitly before choosing the wire shape.
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		log.Printf("[WSAgentResponder] failed to parse agent response for request_id=%s: %v", requestID, err)
		return
	}
	responseRaw, wrapped := object["response"]
	if !wrapped {
		responseRaw = raw
	}
	var resp AgentResponse
	if len(responseRaw) == 0 || string(responseRaw) == "null" {
		log.Printf("[WSAgentResponder] empty agent response for request_id=%s", requestID)
		return
	}
	if err := json.Unmarshal(responseRaw, &resp); err != nil {
		log.Printf("[WSAgentResponder] failed to parse agent response body for request_id=%s: %v", requestID, err)
		return
	}
	w.Router.HandleAgentResponse(requestID, &resp)
}

// HandleAgentVoicePart parses one bounded GUI -> Hub voice frame and stores it
// on the pending request. The final response commits the assembled stream.
func (w *WSAgentResponder) HandleAgentVoicePart(requestID string, raw json.RawMessage) {
	if w == nil || w.Router == nil {
		return
	}
	var frame AgentVoicePart
	if err := json.Unmarshal(raw, &frame); err != nil {
		log.Printf("[WSAgentResponder] failed to parse agent voice part for request_id=%s: %v", requestID, err)
		return
	}
	w.Router.HandleAgentVoicePart(requestID, frame)
}

// HandleAgentProgress delegates progress updates to the MessageRouter,
// which resets the timeout and optionally delivers the text to the user.
func (w *WSAgentResponder) HandleAgentProgress(requestID string, text string) {
	w.Router.HandleAgentProgress(requestID, text)
}
