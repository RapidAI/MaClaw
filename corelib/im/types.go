// Package im defines the unified IM abstraction layer for corelib.
// It provides standardized message types and plugin interfaces that can be
// used by corelib consumers without importing hub/internal/im.
package im

import (
	"context"
	"encoding/json"
	"time"
)

// IncomingMessage represents a standardized inbound message from any IM platform.
type IncomingMessage struct {
	Platform    string          `json:"platform"`     // "feishu", "dingtalk", "wecom", "telegram", "weixin", "qqbot"
	PlatformUID string          `json:"platform_uid"` // Platform-specific user ID
	UserName    string          `json:"user_name"`    // Display name
	MessageID   string          `json:"message_id"`   // Platform message ID for dedup
	MessageType string          `json:"message_type"` // "text", "image", "file"
	Text        string          `json:"text"`
	Lang        string          `json:"lang,omitempty"`
	Attachments []Attachment    `json:"attachments,omitempty"`
	RawPayload  json.RawMessage `json:"raw_payload,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
}

// Attachment represents a file/image attachment in a message.
type Attachment struct {
	Type          string `json:"type"` // "image", "file", "audio", "video"
	FileName      string `json:"file_name"`
	MimeType      string `json:"mime_type"`
	Data          string `json:"data"` // base64-encoded
	Size          int64  `json:"size"`
	SourceMediaID string `json:"source_media_id,omitempty"` // transport-local authenticated media reference
}

// OutgoingMessage represents a standardized outbound message.
type OutgoingMessage struct {
	Text         string `json:"text"`
	Markdown     string `json:"markdown,omitempty"`
	Title        string `json:"title,omitempty"`
	FallbackText string `json:"fallback_text,omitempty"`
}

// UserTarget identifies the recipient for outbound messages.
type UserTarget struct {
	PlatformUID string `json:"platform_uid"`
	UserName    string `json:"user_name,omitempty"`
}

// Capabilities declares what an IM platform supports.
type Capabilities struct {
	SupportsMarkdown bool `json:"supports_markdown"`
	SupportsRichCard bool `json:"supports_rich_card"`
	SupportsImage    bool `json:"supports_image"`
	SupportsFile     bool `json:"supports_file"`
	MaxTextLength    int  `json:"max_text_length"`
}

// MessageHandler is the callback type for incoming messages.
type MessageHandler func(msg IncomingMessage)

// Plugin defines the standard interface for IM platform plugins.
// This is the corelib-level abstraction — lighter than hub's IMPlugin,
// focused on send/receive without user resolution or store dependencies.
type Plugin interface {
	// Name returns the plugin identifier (e.g. "feishu", "dingtalk", "wecom").
	Name() string
	// Start starts the plugin (connect, register webhooks, etc.).
	Start(ctx context.Context) error
	// Stop stops the plugin gracefully.
	Stop(ctx context.Context) error
	// OnMessage registers a handler for incoming messages.
	OnMessage(handler MessageHandler)
	// SendText sends a plain text message.
	SendText(ctx context.Context, target UserTarget, text string) error
	// SendMarkdown sends a markdown message (falls back to text if unsupported).
	SendMarkdown(ctx context.Context, target UserTarget, markdown string) error
	// Capabilities returns what this platform supports.
	Capabilities() Capabilities
}
