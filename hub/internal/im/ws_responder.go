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
	var envelope struct {
		Response AgentResponse `json:"response"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// Try parsing as a direct AgentResponse (no wrapper).
		var direct AgentResponse
		if err2 := json.Unmarshal(raw, &direct); err2 != nil {
			log.Printf("[WSAgentResponder] failed to parse agent response for request_id=%s: %v", requestID, err)
			return
		}
		w.Router.HandleAgentResponse(requestID, &direct)
		return
	}
	resp := envelope.Response
	w.Router.HandleAgentResponse(requestID, &resp)
}

// HandleAgentProgress delegates progress updates to the MessageRouter,
// which resets the timeout and optionally delivers the text to the user.
func (w *WSAgentResponder) HandleAgentProgress(requestID string, text string) {
	w.Router.HandleAgentProgress(requestID, text)
}
