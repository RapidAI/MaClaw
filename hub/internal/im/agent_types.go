package im

// AgentResponse is the structured reply from the MaClaw Agent (LLM).
// It is sent from the MaClaw client to Hub via the "im.agent_response"
// WebSocket message, then converted to GenericResponse for IM delivery.
type AgentResponse struct {
	Text         string           `json:"text"`                     // Main reply text
	Fields       []ResponseField  `json:"fields,omitempty"`         // Structured fields (optional)
	Actions      []ResponseAction `json:"actions,omitempty"`        // Suggested actions (optional)
	ImageKey     string           `json:"image_key,omitempty"`      // Image key (optional)
	FileData     string           `json:"file_data,omitempty"`      // Base64-encoded file data (optional)
	FileName     string           `json:"file_name,omitempty"`      // File display name (optional)
	FileMimeType string           `json:"file_mime_type,omitempty"` // File MIME type (optional)
	Error        string           `json:"error,omitempty"`          // Error message (optional)
	Deferred     bool             `json:"deferred,omitempty"`       // true = media buffered, Hub should not reply to user
}

// IMUserMessage is sent from Hub to MaClaw client via WebSocket
// when a user sends a message through an IM platform.
type IMUserMessage struct {
	Type        string              `json:"type"`                  // "im.user_message"
	RequestID   string              `json:"request_id"`            // Correlates with the agent response
	UserID      string              `json:"user_id"`
	Platform    string              `json:"platform"`              // "feishu", "qbot", "openclaw"
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
			StatusIcon: "❌",
			Title:      "Agent 错误",
			Body:       r.Error,
		}
	}

	resp := &GenericResponse{
		StatusCode:   200,
		StatusIcon:   "🤖",
		Title:        "",
		Body:         r.Text,
		Fields:       r.Fields,
		Actions:      r.Actions,
		ImageKey:     r.ImageKey,
		FileData:     r.FileData,
		FileName:     r.FileName,
		FileMimeType: r.FileMimeType,
	}

	return resp
}
