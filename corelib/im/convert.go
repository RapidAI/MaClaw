package im

// This file provides conversion utilities between corelib/im types and
// hub/internal/im types. Since hub's im package has richer types (with
// UnifiedUserID, RawPayload, etc.), we provide functions to convert
// between the two without requiring hub to change its existing types.
//
// Usage in hub:
//   import cim "github.com/RapidAI/CodeClaw/corelib/im"
//   hubMsg := ConvertToHubFormat(corelibMsg)
//   corelibMsg := ConvertFromHubFormat(hubMsg)

// ToMap converts an IncomingMessage to a generic map for cross-package transfer.
func (m IncomingMessage) ToMap() map[string]interface{} {
	result := map[string]interface{}{
		"platform":     m.Platform,
		"platform_uid": m.PlatformUID,
		"user_name":    m.UserName,
		"message_id":   m.MessageID,
		"message_type": m.MessageType,
		"text":         m.Text,
		"lang":         m.Lang,
		"timestamp":    m.Timestamp,
	}
	if len(m.Attachments) > 0 {
		result["attachments"] = m.Attachments
	}
	return result
}

// IncomingFromMap creates an IncomingMessage from a generic map.
func IncomingFromMap(m map[string]interface{}) IncomingMessage {
	msg := IncomingMessage{}
	if v, ok := m["platform"].(string); ok {
		msg.Platform = v
	}
	if v, ok := m["platform_uid"].(string); ok {
		msg.PlatformUID = v
	}
	if v, ok := m["user_name"].(string); ok {
		msg.UserName = v
	}
	if v, ok := m["message_id"].(string); ok {
		msg.MessageID = v
	}
	if v, ok := m["message_type"].(string); ok {
		msg.MessageType = v
	}
	if v, ok := m["text"].(string); ok {
		msg.Text = v
	}
	if v, ok := m["lang"].(string); ok {
		msg.Lang = v
	}
	return msg
}
