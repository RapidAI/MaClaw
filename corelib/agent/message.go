package agent

// UserMessage is the input to the agent handler. It represents a message
// from any platform (desktop, IM, TUI).
//
// This is the corelib equivalent of gui.IMUserMessage. The gui package
// defines IMUserMessage as an alias or wrapper around this type.
type UserMessage struct {
	UserID   string `json:"user_id"`
	Platform string `json:"platform"` // "desktop", "feishu", "wechat", "qq", "telegram", "tui"
	Text     string `json:"text"`
	Lang     string `json:"lang,omitempty"`

	// Attachments holds images/files attached to the message.
	Attachments []MessageAttachment `json:"attachments,omitempty"`

	// IsBackground indicates this is a background task (scheduled, auto-picked).
	IsBackground bool `json:"is_background,omitempty"`

	// StartNewTask forces a new conversation context.
	StartNewTask bool `json:"start_new_task,omitempty"`

	// MinIterations overrides the default max iterations for this message.
	MinIterations int `json:"min_iterations,omitempty"`

	// BackgroundSlotKind categorizes the background task type.
	BackgroundSlotKind string `json:"background_slot_kind,omitempty"`

	// ResumeSlotID resumes a previously unfinished task slot.
	ResumeSlotID string `json:"resume_slot_id,omitempty"`

	// DismissSlotID dismisses a previously unfinished task slot.
	DismissSlotID string `json:"dismiss_slot_id,omitempty"`

	// ResumeRecoverableSessionID resumes a recoverable coding session.
	ResumeRecoverableSessionID string `json:"resume_recoverable_session_id,omitempty"`

	// DismissRecoverableSessionID dismisses a recoverable coding session.
	DismissRecoverableSessionID string `json:"dismiss_recoverable_session_id,omitempty"`

	// SlashCommand is set by Hub when it parses a slash command and forwards
	// it to the device for handling (e.g. "workflow:status", "workflow:cancel").
	SlashCommand string `json:"slash_command,omitempty"`
}

// MessageAttachment represents an image or file attached to a message.
type MessageAttachment struct {
	Type     string `json:"type"`                // "image", "file", "audio", "video"
	FileName string `json:"file_name,omitempty"` // original filename
	MimeType string `json:"mime_type,omitempty"` // e.g. "image/png"
	Data     string `json:"data,omitempty"`      // Base64-encoded content
	Size     int64  `json:"size,omitempty"`      // Original size in bytes
}

// Response is the output from the agent handler.
//
// This is the corelib equivalent of gui.IMAgentResponse.
type Response struct {
	Text     string `json:"text,omitempty"`
	Error    string `json:"error,omitempty"`
	ImageKey string `json:"image_key,omitempty"` // base64 screenshot

	// HardExit indicates the loop was terminated due to consecutive empty
	// responses (not a normal completion). Post-loop doc capture should
	// be skipped.
	HardExit bool `json:"hard_exit,omitempty"`
}

// TokenCallback receives streaming text deltas from the LLM.
type TokenCallback func(delta string)

// NewRoundCallback is called when the agent loop starts a new LLM round
// (after tool execution). Used by the desktop frontend to insert visual
// separators between rounds.
type NewRoundCallback func()

// StreamDoneCallback is called when the LLM stream for the current round
// is complete (all tokens received, before tool execution).
type StreamDoneCallback func()
