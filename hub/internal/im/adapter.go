// Package im defines the IM adapter layer core interfaces and data models.
// It provides a unified abstraction for IM platform plugins, standardized
// message types, and capability declarations to support multi-platform
// messaging with automatic format negotiation and graceful degradation.
package im

import (
	"context"
	"encoding/json"
	"time"
)

// IMPlugin defines the standard interface for IM platform plugins.
// Each IM platform (Feishu, QBot, Slack, etc.) implements this interface
// to integrate with the IM adapter layer.
type IMPlugin interface {
	// Name returns the plugin name (e.g. "feishu", "qbot").
	Name() string
	// ReceiveMessage registers a callback for incoming messages.
	ReceiveMessage(handler func(msg IncomingMessage))
	// SendText sends a plain text message to the target user.
	SendText(ctx context.Context, target UserTarget, text string) error
	// SendCard sends a rich card message to the target user.
	SendCard(ctx context.Context, target UserTarget, card OutgoingMessage) error
	// SendImage sends an image message to the target user.
	SendImage(ctx context.Context, target UserTarget, imageKey string, caption string) error
	// SendFile sends a file to the target user. fileData is base64-encoded,
	// fileName is the display name, and mimeType hints the content type.
	SendFile(ctx context.Context, target UserTarget, fileData, fileName, mimeType string) error
	// ResolveUser maps a platform-specific user identifier to a unified internal user ID.
	ResolveUser(ctx context.Context, platformUID string) (string, error)
	// Capabilities returns the platform's capability declaration.
	Capabilities() CapabilityDeclaration
	// Start starts the plugin (establish connections, register webhooks, etc.).
	Start(ctx context.Context) error
	// Stop stops the plugin gracefully.
	Stop(ctx context.Context) error
}

// UrgentSender is an optional interface that IM plugins can implement to
// support urgent/buzz notifications (e.g. Feishu in-app urgent, phone call).
// Plugins that don't support urgent notifications simply don't implement this.
type UrgentSender interface {
	// SendUrgentText sends a text message with platform-specific urgent notification.
	SendUrgentText(ctx context.Context, target UserTarget, text string) error
}

// CapabilityDeclaration declares the message types supported by an IM platform.
type CapabilityDeclaration struct {
	SupportsRichCard    bool // Supports rich text cards
	SupportsMarkdown    bool // Supports Markdown formatting
	SupportsImage       bool // Supports image messages
	SupportsFile        bool // Supports file messages
	SupportsButton      bool // Supports button interactions
	SupportsMessageEdit bool // Supports message editing/updating
	MaxTextLength       int  // Maximum text length per message (0 = unlimited)
}

// MessageAttachment represents a file/image/audio attachment in an inbound message.
type MessageAttachment struct {
	Type     string `json:"type"`      // "image", "file", "audio", "video"
	FileName string `json:"file_name"` // Display name (e.g. "report.docx")
	MimeType string `json:"mime_type"` // MIME type (e.g. "image/png", "application/pdf")
	Data     string `json:"data"`      // Base64-encoded file content
	Size     int64  `json:"size"`      // Original file size in bytes (before base64)
}

// MaxAttachmentSize is the maximum allowed size for a single attachment (10 MB).
const MaxAttachmentSize = 10 * 1024 * 1024

// IncomingMessage represents a standardized inbound message from any IM platform.
type IncomingMessage struct {
	PlatformName  string              `json:"platform_name"`       // IM platform name (e.g. "feishu", "qbot")
	PlatformUID   string              `json:"platform_uid"`        // Platform-specific user ID (e.g. Feishu open_id)
	UnifiedUserID string              `json:"unified_user_id"`     // Unified internal user ID (populated by IM Adapter)
	MessageID     string              `json:"message_id,omitempty"` // Platform message ID for dedup (optional)
	MessageType   string              `json:"message_type"`        // "text", "image", "file", "audio", "interactive"
	Text          string              `json:"text"`                // Text content
	Lang          string              `json:"lang,omitempty"`      // User language ("zh", "en"); empty defaults to "zh"
	Attachments   []MessageAttachment `json:"attachments,omitempty"` // File/image attachments
	RawPayload    json.RawMessage     `json:"raw_payload"`         // Raw platform message for plugin-specific handling
	Timestamp     time.Time           `json:"timestamp"`
}

// OutgoingMessage represents a standardized outbound message, converted from GenericResponse.
type OutgoingMessage struct {
	Title        string          `json:"title"`
	Body         string          `json:"body"`
	Fields       []MessageField  `json:"fields,omitempty"`
	Actions      []MessageAction `json:"actions,omitempty"`
	StatusCode   int             `json:"status_code"`
	StatusIcon   string          `json:"status_icon"`
	FallbackText string          `json:"fallback_text"` // Plain text fallback
	Urgent       bool            `json:"urgent,omitempty"` // When true, send with platform-specific urgent/buzz notification
}

// UserTarget identifies the target user for outbound messages.
type UserTarget struct {
	PlatformUID   string `json:"platform_uid"`
	UnifiedUserID string `json:"unified_user_id"`
}

// MessageField represents a structured key-value field in an outgoing message.
type MessageField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// MessageAction represents an interactive action button in an outgoing message.
type MessageAction struct {
	Label   string `json:"label"`   // Button text
	Command string `json:"command"` // Corresponding command (e.g. "/use 1")
	Style   string `json:"style"`   // "primary", "danger", "default"
}
