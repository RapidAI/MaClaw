package im

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// GenericResponse is the universal response model for all operations.
// It encapsulates operation results in a platform-agnostic format that
// can be converted to OutgoingMessage for any IM plugin, or degraded
// to plain text for platforms with limited capabilities.
type GenericResponse struct {
	StatusCode   int              // Status code: 200 success, 400 bad request, 404 not found, 500 internal error
	StatusIcon   string           // Semantic status token: ok|error|warning|busy|info|offline (never emoji)
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

// FormatStatusIconMark maps semantic StatusIcon tokens to short ASCII marks
// for plain-text IM delivery. Never emits emoji.
func FormatStatusIconMark(icon string) string {
	switch strings.ToLower(strings.TrimSpace(icon)) {
	case "ok", "success":
		return "[OK]"
	case "error", "fail", "failed":
		return "[ERR]"
	case "warning", "warn", "alert":
		return "[!!]"
	case "busy", "pending", "running", "processing":
		return "[..]"
	case "info":
		return "[i]"
	case "offline":
		return "[--]"
	default:
		// Allow already-formatted bracket marks such as [OK] / [..].
		trimmed := strings.TrimSpace(icon)
		if len(trimmed) >= 3 && len(trimmed) <= 8 && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			return trimmed
		}
		return ""
	}
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

// displayNormalizedParts holds Title/Body/fields after display-policy stripping.
// Used so ToOutgoingMessage and ToFallbackText strip each string at most once.
type displayNormalizedParts struct {
	title  string
	body   string
	fields []MessageField
}

func (r *GenericResponse) displayNormalized() displayNormalizedParts {
	if r == nil {
		return displayNormalizedParts{}
	}
	fields := make([]MessageField, len(r.Fields))
	for i, f := range r.Fields {
		fields[i] = MessageField{
			Label: f.Label,
			Value: textutil.PrepareChatBodyForDisplay(f.Value),
		}
	}
	return displayNormalizedParts{
		title:  textutil.PrepareChatBodyForDisplay(r.Title),
		body:   textutil.PrepareChatBodyForDisplay(r.Body),
		fields: fields,
	}
}

// formatFallbackFromParts builds plain-text fallback from already-normalized parts.
// Status marks are ASCII only (never raw semantic tokens or emoji).
func formatFallbackFromParts(statusIcon, title, body string, fields []MessageField) string {
	mark := FormatStatusIconMark(statusIcon)

	// Pre-size: avoids repeated growth on typical multi-field responses.
	n := len(mark) + len(title) + len(body) + 8
	for _, f := range fields {
		n += len(f.Label) + len(f.Value) + 4
	}
	var b strings.Builder
	b.Grow(n)

	if mark != "" || title != "" {
		if mark != "" {
			b.WriteString(mark)
			if title != "" {
				b.WriteByte(' ')
			}
		}
		if title != "" {
			b.WriteString(title)
		}
		b.WriteByte('\n')
	}

	if body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}

	for _, f := range fields {
		b.WriteString(f.Label)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteByte('\n')
	}

	return strings.TrimRight(b.String(), "\n")
}

// ToOutgoingMessage converts a GenericResponse to an OutgoingMessage
// suitable for delivery through any IM plugin.
// Title/Body/field values are display-normalized (line-leading pictographs stripped)
// so every channel plugin sees consistent text whether it uses FallbackText or Body.
func (r *GenericResponse) ToOutgoingMessage() OutgoingMessage {
	if r == nil {
		return OutgoingMessage{}
	}

	parts := r.displayNormalized()

	actions := make([]MessageAction, len(r.Actions))
	for i, a := range r.Actions {
		actions[i] = MessageAction{
			Label:   a.Label,
			Command: a.Command,
			Style:   a.Style,
		}
	}

	fallback := r.FallbackText
	if fallback != "" {
		// Explicit override still gets line-leading pictograph strip (display policy).
		fallback = textutil.PrepareChatBodyForDisplay(fallback)
	} else {
		// Build from the same normalized parts — no second Prepare pass.
		fallback = formatFallbackFromParts(r.StatusIcon, parts.title, parts.body, parts.fields)
	}

	return OutgoingMessage{
		Title:        parts.title,
		Body:         parts.body,
		Fields:       parts.fields,
		Actions:      actions,
		StatusCode:   r.StatusCode,
		StatusIcon:   r.StatusIcon, // keep semantic token for machine consumers
		FallbackText: fallback,
	}
}

// ToFallbackText generates a plain text representation of the response.
// This is used when the target IM platform does not support rich text,
// or as the FallbackText field in OutgoingMessage.
// If FallbackText is explicitly set, it is returned after display normalization.
func (r *GenericResponse) ToFallbackText() string {
	if r == nil {
		return ""
	}
	if r.FallbackText != "" {
		return textutil.PrepareChatBodyForDisplay(r.FallbackText)
	}
	parts := r.displayNormalized()
	return formatFallbackFromParts(r.StatusIcon, parts.title, parts.body, parts.fields)
}

// FormatCardFallback builds plain text for plugins when OutgoingMessage.FallbackText
// is empty. Mirrors ToFallbackText formatting (ASCII status marks, no emoji).
// When FallbackText is empty, parts are re-normalized (idempotent) for safety
// if the card was constructed without going through ToOutgoingMessage.
func FormatCardFallback(card OutgoingMessage) string {
	if card.FallbackText != "" {
		return textutil.PrepareChatBodyForDisplay(card.FallbackText)
	}
	return formatFallbackFromParts(
		card.StatusIcon,
		textutil.PrepareChatBodyForDisplay(card.Title),
		textutil.PrepareChatBodyForDisplay(card.Body),
		normalizeMessageFields(card.Fields),
	)
}

func normalizeMessageFields(fields []MessageField) []MessageField {
	if len(fields) == 0 {
		return fields
	}
	out := make([]MessageField, len(fields))
	for i, f := range fields {
		out[i] = MessageField{
			Label: f.Label,
			Value: textutil.PrepareChatBodyForDisplay(f.Value),
		}
	}
	return out
}
