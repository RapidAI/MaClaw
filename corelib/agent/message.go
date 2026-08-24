package agent

import "context"

// AssistantBinding is trusted, local routing metadata attached by an IM
// transport. It is deliberately separate from UserID: a bot must not pretend
// to be a desktop expert session merely to select a persona.
type AssistantBinding struct {
	BotProfileID        string   `json:"bot_profile_id,omitempty"`
	Mode                string   `json:"mode,omitempty"`
	ExpertID            string   `json:"expert_id,omitempty"`
	InitialPrompt       string   `json:"initial_prompt,omitempty"`
	WorkingDirectory    string   `json:"working_directory,omitempty"`
	DocumentDirectories []string `json:"document_directories,omitempty"`
	AllowedDirectories  []string `json:"allowed_directories,omitempty"`
	AllowAllDirectories bool     `json:"allow_all_directories,omitempty"`
}

// DeliveryTarget is trusted transport context for an external artifact
// delivery. It is assigned by a channel adapter from the inbound conversation,
// never parsed from user/model content, and is intentionally omitted from
// wire serialization. The destination should include its address kind (for
// example "group:<id>" versus "user:<id>") whenever transports share IDs.
type DeliveryTarget struct {
	ChannelScope  string
	DestinationID string
}

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

	// CacheQuestion and CacheScope are trusted transport metadata used only by
	// local answer reuse. They keep a display/prompt-enriched message (such as
	// a group-chat context prefix) from weakening exact-question matching, while
	// preserving the conversation boundary that must not share cached answers.
	// They are never sent to external clients.
	CacheQuestion string `json:"-"`
	CacheScope    string `json:"-"`
	// CachePolicyScope carries non-secret, response-affecting transport policy
	// that is not represented by AssistantBinding. It prevents a cached answer
	// from crossing permission boundaries such as a group knowledge-source
	// allowlist. It is never sent to external clients.
	CachePolicyScope string `json:"-"`

	// AssistantBinding is transport-internal policy supplied by a configured
	// bot profile; it must never be inferred from untrusted message text.
	AssistantBinding *AssistantBinding `json:"-"`
	// CodingTaskIngressToken is a one-shot host capability attached only by a
	// trusted coding-task ingress. It is intentionally excluded from the wire
	// format: UserID, request text, model calls and transport metadata must not
	// be able to manufacture a semantic Coding task relation.
	CodingTaskIngressToken string `json:"-"`
	// DeliveryTarget is the server-owned current-channel destination available
	// to a planned delivery selection. It is not an agent tool argument.
	DeliveryTarget *DeliveryTarget `json:"-"`

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
	Type          string `json:"type"`                      // "image", "file", "audio", "video"
	FileName      string `json:"file_name,omitempty"`       // original filename
	MimeType      string `json:"mime_type,omitempty"`       // e.g. "image/png"
	Data          string `json:"data,omitempty"`            // Base64-encoded content
	Size          int64  `json:"size,omitempty"`            // Original size in bytes
	SourceMediaID string `json:"source_media_id,omitempty"` // transport-local authenticated media reference
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
