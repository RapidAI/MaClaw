package im

import "strings"

// AgentResponse is the structured reply from the MaClaw Agent (LLM).
// It is sent from the MaClaw client to Hub via the "im.agent_response"
// WebSocket message, then converted to GenericResponse for IM delivery.
type AgentResponse struct {
	Text              string           `json:"text"`                          // Main reply text
	Fields            []ResponseField  `json:"fields,omitempty"`              // Structured fields (optional)
	Actions           []ResponseAction `json:"actions,omitempty"`             // Suggested actions (optional)
	ImageKey          string           `json:"image_key,omitempty"`           // Image key (optional)
	FileData          string           `json:"file_data,omitempty"`           // Base64-encoded file data (optional)
	FileName          string           `json:"file_name,omitempty"`           // File display name (optional)
	FileMimeType      string           `json:"file_mime_type,omitempty"`      // File MIME type (optional)
	VoiceData         string           `json:"voice_data,omitempty"`          // Base64-encoded voice audio (optional, OGG Opus or WAV)
	VoiceFileName     string           `json:"voice_file_name,omitempty"`     // e.g. "voice.ogg"
	VoiceMimeType     string           `json:"voice_mime_type,omitempty"`     // e.g. "audio/ogg"
	VoiceParts        []VoicePart      `json:"voice_parts,omitempty"`         // Ordered bounded audio segments for hardware clients
	PendingVoiceParts int              `json:"pending_voice_parts,omitempty"` // Deferred parts that follow the terminal result
	Error             string           `json:"error,omitempty"`               // Error message (optional)
	Deferred          bool             `json:"deferred,omitempty"`            // true = media buffered, Hub should not reply to user
}

// IMUserMessage is sent from Hub to MaClaw client via WebSocket
// when a user sends a message through an IM platform.
type IMUserMessage struct {
	Type        string              `json:"type"`       // "im.user_message"
	RequestID   string              `json:"request_id"` // Correlates with the agent response
	UserID      string              `json:"user_id"`
	Platform    string              `json:"platform"` // "feishu", "qbot", "openclaw"
	Text        string              `json:"text"`
	Lang        string              `json:"lang,omitempty"`        // User language ("zh", "en"); empty defaults to "zh"
	Attachments []MessageAttachment `json:"attachments,omitempty"` // File/image attachments from user
	Timestamp   int64               `json:"ts"`
}

// IMAgentResponseMsg is sent from MaClaw client to Hub via WebSocket
// as the Agent's reply to an im.user_message.
type IMAgentResponseMsg struct {
	Type      string        `json:"type"`       // "im.agent_response"
	RequestID string        `json:"request_id"` // Correlates with the original request
	Response  AgentResponse `json:"response"`
}

// ToGenericResponse converts an AgentResponse to a GenericResponse
// suitable for delivery through any IM plugin.
func (r *AgentResponse) ToGenericResponse() *GenericResponse {
	if r.Error != "" {
		return &GenericResponse{
			StatusCode: 500,
			StatusIcon: "error",
			Title:      "Agent 错误",
			Body:       r.Error,
		}
	}

	resp := &GenericResponse{
		StatusCode:        200,
		StatusIcon:        "info",
		Title:             "",
		Body:              r.Text,
		Fields:            filterOutInternalLLMTelemetryFields(r.Fields),
		Actions:           r.Actions,
		ImageKey:          r.ImageKey,
		FileData:          r.FileData,
		FileName:          r.FileName,
		FileMimeType:      r.FileMimeType,
		VoiceData:         r.VoiceData,
		VoiceFileName:     r.VoiceFileName,
		VoiceMimeType:     r.VoiceMimeType,
		VoiceParts:        append([]VoicePart(nil), r.VoiceParts...),
		PendingVoiceParts: r.PendingVoiceParts,
	}

	return resp
}

// filterOutInternalLLMTelemetryFields removes internal LLM telemetry from the
// response field list. It remains available in MaClaw's trace data, but clients
// flatten fields into the reply body and would expose model routing/settings
// after the assistant's actual answer.
func filterOutInternalLLMTelemetryFields(fields []ResponseField) []ResponseField {
	if len(fields) == 0 {
		return fields
	}
	filtered := make([]ResponseField, 0, len(fields))
	for _, f := range fields {
		label := strings.ToLower(strings.TrimSpace(f.Label))
		if f.Internal || isLegacyInternalLLMTelemetryLabel(label) {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}

// isLegacyInternalLLMTelemetryLabel keeps replies clean while older GUI
// versions that predate ResponseField.Internal are still connected to Hub.
// Match only labels emitted by MaClaw itself so similarly named business
// fields are not accidentally hidden.
func isLegacyInternalLLMTelemetryLabel(label string) bool {
	switch label {
	case "input tokens",
		"output tokens",
		"total tokens",
		"cache read tokens",
		"cache write tokens",
		"session_tokens",
		"session_est_cost_rmb",
		"turn",
		"route task",
		"route source",
		"route model",
		"route escalated",
		"route reason",
		"cost tier",
		"thinking":
		return true
	default:
		return false
	}
}
