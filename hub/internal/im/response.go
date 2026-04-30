package im

import (
	"fmt"
	"strings"
)

// GenericResponse is the universal response model for all operations.
// It encapsulates operation results in a platform-agnostic format that
// can be converted to OutgoingMessage for any IM plugin, or degraded
// to plain text for platforms with limited capabilities.
type GenericResponse struct {
	StatusCode   int              // Status code: 200 success, 400 bad request, 404 not found, 500 internal error
	StatusIcon   string           // Status emoji icon (e.g. "✅", "❌", "⚠️")
	Title        string           // Response title
	Body         string           // Response body (supports Markdown)
	Fields       []ResponseField  // Structured field list
	Actions      []ResponseAction // Action button definitions
	FallbackText string           // Explicit plain text fallback (optional override)
	ImageKey     string           // Base64 image data or image key for IM delivery (optional)
	ImageCaption string           // Caption for the image (optional)
	FileData     string           // Base64-encoded file data for IM delivery (optional)
	FileName     string           // File display name (optional)
	FileMimeType string           // File MIME type (optional)
	VoiceData     string          // Base64-encoded voice audio for IM delivery (optional, OGG Opus or WAV)
	VoiceFileName string          // Voice file name, e.g. "voice.ogg" (optional)
	VoiceMimeType string          // Voice MIME type, e.g. "audio/ogg" (optional)
}

// ResponseField represents a structured key-value field in a response.
type ResponseField struct {
	Label    string // Field label
	Value    string // Field value (plain text)
	RichText string // Rich text representation (optional, for platforms that support it)
}

// ResponseAction represents an interactive action button in a response.
type ResponseAction struct {
	Label   string // Button display text
	Command string // Corresponding command (e.g. "/use 1")
	Style   string // "primary", "danger", "default"
}

// ToOutgoingMessage converts a GenericResponse to an OutgoingMessage
// suitable for delivery through any IM plugin.
func (r *GenericResponse) ToOutgoingMessage() OutgoingMessage {
	fields := make([]MessageField, len(r.Fields))
	for i, f := range r.Fields {
		fields[i] = MessageField{
			Label: f.Label,
			Value: f.Value,
		}
	}

	actions := make([]MessageAction, len(r.Actions))
	for i, a := range r.Actions {
		actions[i] = MessageAction{
			Label:   a.Label,
			Command: a.Command,
			Style:   a.Style,
		}
	}

	return OutgoingMessage{
		Title:        r.Title,
		Body:         r.Body,
		Fields:       fields,
		Actions:      actions,
		StatusCode:   r.StatusCode,
		StatusIcon:   r.StatusIcon,
		FallbackText: r.ToFallbackText(),
	}
}

// ToFallbackText generates a plain text representation of the response.
// This is used when the target IM platform does not support rich text,
// or as the FallbackText field in OutgoingMessage.
// If FallbackText is explicitly set, it is returned directly.
func (r *GenericResponse) ToFallbackText() string {
	if r.FallbackText != "" {
		return r.FallbackText
	}

	var b strings.Builder

	// Status icon + title line
	if r.StatusIcon != "" || r.Title != "" {
		if r.StatusIcon != "" {
			b.WriteString(r.StatusIcon)
			if r.Title != "" {
				b.WriteString(" ")
			}
		}
		if r.Title != "" {
			b.WriteString(r.Title)
		}
		b.WriteString("\n")
	}

	// Body
	if r.Body != "" {
		b.WriteString(r.Body)
		b.WriteString("\n")
	}

	// Fields as "Label: Value" lines
	for _, f := range r.Fields {
		b.WriteString(fmt.Sprintf("%s: %s\n", f.Label, f.Value))
	}

	return strings.TrimRight(b.String(), "\n")
}
