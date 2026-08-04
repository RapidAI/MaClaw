package agent

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const imFileTargetPayloadPrefix = "target64:"

// IMFileDeliveryTarget identifies an optional exact IM destination for a
// send_file/send_to_im artifact. An empty target preserves the legacy behavior
// of delivering to the user's bound/active IM channels.
type IMFileDeliveryTarget struct {
	Channel   string `json:"channel,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// Active reports whether the request asks for an exact destination instead of
// the legacy active-channel/broadcast route.
func (t IMFileDeliveryTarget) Active() bool {
	return strings.TrimSpace(t.Channel) != "" ||
		strings.TrimSpace(t.GroupID) != "" ||
		strings.TrimSpace(t.GroupName) != "" ||
		strings.TrimSpace(t.UserID) != ""
}

// Normalize trims target fields. Channel aliases are intentionally left to the
// host's IM router, which owns the set of available platforms.
func (t IMFileDeliveryTarget) Normalize() IMFileDeliveryTarget {
	t.Channel = strings.TrimSpace(t.Channel)
	t.GroupID = strings.TrimSpace(t.GroupID)
	t.GroupName = strings.TrimSpace(t.GroupName)
	t.UserID = strings.TrimSpace(t.UserID)
	return t
}

// IMFileDeliveryRequest is the structured callback contract used after a file
// payload has been materialized. Data is standard base64.
type IMFileDeliveryRequest struct {
	Data     string               `json:"file_data"`
	FileName string               `json:"file_name"`
	MIMEType string               `json:"mime_type"`
	Message  string               `json:"message,omitempty"`
	Target   IMFileDeliveryTarget `json:"target,omitempty"`
}

// IMFileDeliveryTargetFromArgs extracts exact-target fields from tool args.
// destination remains the desktop-vs-IM switch; a platform-valued destination
// is also accepted as a channel alias for backward compatibility.
func IMFileDeliveryTargetFromArgs(args map[string]interface{}) IMFileDeliveryTarget {
	if args == nil {
		return IMFileDeliveryTarget{}
	}
	read := func(key string) string {
		v, _ := args[key].(string)
		return strings.TrimSpace(v)
	}
	t := IMFileDeliveryTarget{
		Channel:   read("channel"),
		GroupID:   read("group_id"),
		GroupName: read("group_name"),
		UserID:    read("user_id"),
	}
	if t.Channel == "" {
		destination := read("destination")
		switch strings.ToLower(destination) {
		case "", "im", "chat", "desktop", "local", "ui", "assistant":
		default:
			t.Channel = destination
		}
	}
	return t.Normalize()
}

// EncodeIMFileDeliveryTargetFlag serializes exact-target metadata into one
// delimiter-safe file payload flag. Empty targets produce an empty flag.
func EncodeIMFileDeliveryTargetFlag(args map[string]interface{}) string {
	target := IMFileDeliveryTargetFromArgs(args)
	if !target.Active() {
		return ""
	}
	b, err := json.Marshal(target)
	if err != nil {
		return ""
	}
	return imFileTargetPayloadPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeIMFileDeliveryTargetFlag decodes a target64 payload flag.
func DecodeIMFileDeliveryTargetFlag(flag string) (IMFileDeliveryTarget, bool) {
	flag = strings.TrimSpace(flag)
	if !strings.HasPrefix(flag, imFileTargetPayloadPrefix) {
		return IMFileDeliveryTarget{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(flag, imFileTargetPayloadPrefix))
	if err != nil {
		return IMFileDeliveryTarget{}, false
	}
	var target IMFileDeliveryTarget
	if err := json.Unmarshal(b, &target); err != nil {
		return IMFileDeliveryTarget{}, false
	}
	target = target.Normalize()
	return target, target.Active()
}
