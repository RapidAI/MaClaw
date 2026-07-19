package acpagent

// ProtocolVersion is the negotiated ACP major version.
const ProtocolVersion = 1

// Stop reasons per ACP prompt-turn.
const (
	StopEndTurn         = "end_turn"
	StopMaxTokens       = "max_tokens"
	StopMaxTurnRequests = "max_turn_requests"
	StopRefusal         = "refusal"
	StopCancelled       = "cancelled"
)

// ContentBlock is a minimal ACP content block (text-first for bridge MVP).
type ContentBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	MimeType string         `json:"mimeType,omitempty"`
	Data     string         `json:"data,omitempty"`
	URI      string         `json:"uri,omitempty"`
	Resource map[string]any `json:"resource,omitempty"`
	// Catch-all for forward-compat fields.
	Extra map[string]any `json:"-"`
}

// ImplementationInfo describes client or agent software.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// InitializeParams is the client → agent initialize request.
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities map[string]any     `json:"clientCapabilities,omitempty"`
	ClientInfo         ImplementationInfo `json:"clientInfo,omitempty"`
}

// InitializeResult is the agent → client initialize response.
type InitializeResult struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	AgentCapabilities  map[string]any     `json:"agentCapabilities"`
	AgentInfo          ImplementationInfo `json:"agentInfo"`
	AuthMethods        []map[string]any   `json:"authMethods,omitempty"`
}

// SessionNewParams creates a new ACP session.
type SessionNewParams struct {
	Cwd        string           `json:"cwd,omitempty"`
	MCPServers []map[string]any `json:"mcpServers,omitempty"`
}

// SessionNewResult is returned from session/new.
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// SessionPromptParams is a user prompt turn.
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResult completes a prompt turn.
type SessionPromptResult struct {
	StopReason string `json:"stopReason"`
}

// SessionCancelParams cancels an in-flight prompt (notification).
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionSteerParams injects guide-launch text into the session's running
// agent loop (MaClaw extension: mirrors the GUI input-buffer 引导发射).
type SessionSteerParams struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

// SessionSteerResult reports whether the running loop accepted the injection.
// accepted=false tells the client to fall back to queueing for the next turn.
type SessionSteerResult struct {
	Accepted bool `json:"accepted"`
}

// SessionUpdateParams is an agent → client streaming notification.
type SessionUpdateParams struct {
	SessionID string         `json:"sessionId"`
	Update    map[string]any `json:"update"`
}

// PromptText joins text content blocks (and simple resource text) into one string.
func PromptText(blocks []ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "", "text":
			if t := trimNonEmpty(b.Text); t != "" {
				parts = append(parts, t)
			}
		case "resource":
			if b.Resource != nil {
				if t, ok := b.Resource["text"].(string); ok {
					if tt := trimNonEmpty(t); tt != "" {
						uri, _ := b.Resource["uri"].(string)
						if uri != "" {
							parts = append(parts, "```\n// file: "+uri+"\n"+tt+"\n```")
						} else {
							parts = append(parts, tt)
						}
					}
				}
			}
		case "resource_link":
			if u := trimNonEmpty(b.URI); u != "" {
				parts = append(parts, u)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "\n\n" + parts[i]
	}
	return out
}

func trimNonEmpty(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// DefaultAgentCapabilities returns a conservative bridge capability set.
func DefaultAgentCapabilities() map[string]any {
	return map[string]any{
		"loadSession": false,
		"promptCapabilities": map[string]any{
			"image":           false,
			"audio":           false,
			"embeddedContext": true,
		},
		"mcpCapabilities": map[string]any{
			"http": false,
			"sse":  false,
		},
	}
}
