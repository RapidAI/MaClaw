package agent

import "context"

// UserMessage is the input to the agent handler. It represents a message
// from any platform (desktop, IM, TUI).
//
// This is the corelib equivalent of gui.IMUserMessage. The gui package
// defines IMUserMessage as an alias or wrapper around this type.
type UserMessage struct {
	RequestID   string `json:"request_id,omitempty"`
	UserID      string `json:"user_id"`
	Platform    string `json:"platform"`               // "desktop", "feishu", "wechat", "qq", "telegram", "tui"
	MessageType string `json:"message_type,omitempty"` // "text", "voice", "image", "file", "audio", "video"
	Text        string `json:"text"`
	Lang        string `json:"lang,omitempty"`

	// Attachments holds images/files attached to the message.
	Attachments []MessageAttachment `json:"attachments,omitempty"`

	// SkipUserAudit is local-only. Gateways set it after persisting the inbound
	// user record directly (for example, before group mention/policy filtering).
	SkipUserAudit bool `json:"-"`

	// ClientCapabilities describes the concrete originating surface. Agent
	// replies must be consumable by this client, not merely by the gateway
	// platform used to transport the message.
	ClientCapabilities *ClientCapabilities `json:"client_capabilities,omitempty"`

	// ClientTools is the per-client tool catalog declared by the originating
	// third-party client. These tools are executed by that client rather than by
	// the MaClaw host and therefore must stay scoped to this message/session.
	ClientTools []ClientToolDefinition `json:"client_tools,omitempty"`
	// ClientToolContext identifies the client-side execution target. It is
	// transport-neutral routing data and contains no credentials.
	ClientToolContext *ClientToolContext `json:"client_tool_context,omitempty"`

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

	// UIAction marks the message as an explicit UI button click (dismiss,
	// start-new). When true and StartNewTask is also true, the backend
	// performs the requested state change and returns immediately — the
	// synthetic placeholder text is NOT sent through the LLM processing
	// pipeline (IUM, agent loop, etc.). This is a structural signal from
	// the frontend button protocol, not a text-based heuristic.
	UIAction bool `json:"ui_action,omitempty"`

	// SlashCommand is set by Hub when it parses a slash command and forwards
	// it to the device for handling (e.g. "workflow:status", "workflow:cancel").
	SlashCommand string `json:"slash_command,omitempty"`

	// StartMenu is supplied only for a confirmed Hub /startmenu launch. Desktop
	// clients create a dedicated task/tab from it before processing the message.
	StartMenu *StartMenuLaunch `json:"start_menu,omitempty"`

	// CancelCtx is an optional external cancellation context. When set,
	// background task handlers will monitor it and cancel the agent loop
	// when the context is done (e.g. scheduler timeout or shutdown).
	// Not serialized — only used for in-process signaling.
	CancelCtx context.Context `json:"-"`
}

// ClientToolDefinition describes an untrusted tool implemented by a connected
// client. Hosts validate and expose it only for messages from that client.
type ClientToolDefinition struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	InputSchema      map[string]any    `json:"inputSchema,omitempty"`
	OutputSchema     map[string]any    `json:"outputSchema,omitempty"`
	Risk             string            `json:"risk,omitempty"`
	RequiresApproval bool              `json:"requiresApproval,omitempty"`
	TimeoutMs        int64             `json:"timeoutMs,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// ClientToolContext routes a client tool call back to its declaring client.
type ClientToolContext struct {
	ClientID       string `json:"client_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	// ReplyToMessageID binds tool lifecycle messages to the command that owns
	// the current hardware turn. It is optional for non-hardware clients.
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
}

// StartMenuLaunch contains non-sensitive task metadata carried with a
// confirmed /startmenu prompt. SSH passwords are intentionally excluded.
type StartMenuLaunch struct {
	Title      string `json:"title"`
	TaskText   string `json:"task_text"`
	AgentMode  string `json:"agent_mode,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	RemoteHost string `json:"remote_host,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	RemoteUser string `json:"remote_user,omitempty"`
	RemoteDir  string `json:"remote_dir,omitempty"`
	Platform   string `json:"platform,omitempty"`
	TargetUID  string `json:"target_uid,omitempty"`
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
