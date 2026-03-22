package chat

import "time"

// ChannelType distinguishes direct (1:1) from group conversations.
type ChannelType int

const (
	ChannelDirect ChannelType = iota
	ChannelGroup
)

// Channel represents a chat channel.
type Channel struct {
	ID        string      `json:"id"`
	Type      ChannelType `json:"type"`
	Name      string      `json:"name,omitempty"`
	AvatarURL string      `json:"avatar_url,omitempty"`
	CreatedBy string      `json:"created_by"`
	CreatedAt time.Time   `json:"created_at"`
	LastSeq   int64       `json:"last_seq"`
}

// Member represents a channel member.
type Member struct {
	ChannelID string     `json:"channel_id"`
	UserID    string     `json:"user_id"`
	Role      MemberRole `json:"role"`
	Mute      bool       `json:"mute"`
	Nickname  string     `json:"nickname,omitempty"`
	JoinedAt  time.Time  `json:"joined_at"`
	ReadSeq   int64      `json:"read_seq"`
}

type MemberRole int

const (
	RoleMember MemberRole = iota
	RoleAdmin
	RoleOwner
)

// MessageType distinguishes text, image, voice note, and file messages.
type MessageType int

const (
	MsgText      MessageType = iota
	MsgImage
	MsgVoiceNote
	MsgFile
)

// Message represents a chat message.
type Message struct {
	ID          string      `json:"id"`
	ChannelID   string      `json:"channel_id"`
	Seq         int64       `json:"seq"`
	SenderID    string      `json:"sender_id"`
	Content     string      `json:"content"`
	MsgType     MessageType `json:"msg_type"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	ClientMsgID string      `json:"client_msg_id,omitempty"`
	Recalled    bool        `json:"recalled,omitempty"`
	EditedAt    *time.Time  `json:"edited_at,omitempty"`
}

// Attachment holds metadata for a file/image/voice attached to a message.
type Attachment struct {
	Type       string `json:"type"`                  // "image", "voice", "file"
	URL        string `json:"url"`
	ThumbURL   string `json:"thumb_url,omitempty"`
	Size       int64  `json:"size"`
	DurationMs int    `json:"duration_ms,omitempty"` // voice notes
}

// FileRecord tracks an uploaded file.
type FileRecord struct {
	ID         string    `json:"id"`
	UploaderID string    `json:"uploader_id"`
	ChannelID  string    `json:"channel_id"`
	Filename   string    `json:"filename"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	HasThumb   bool      `json:"has_thumb"`
	CreatedAt  time.Time `json:"created_at"`
}

// VoiceCall represents a voice call session.
type VoiceCall struct {
	ID        string     `json:"id"`
	ChannelID string     `json:"channel_id,omitempty"`
	CallerID  string     `json:"caller_id"`
	CallType  CallType   `json:"call_type"`
	Status    CallStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

type CallType int

const (
	CallOneToOne CallType = iota
	CallConference
)

type CallStatus int

const (
	CallRinging CallStatus = iota
	CallActive
	CallEnded
	CallMissed
	CallRejected
)

// CallParticipant tracks who joined a voice call.
type CallParticipant struct {
	CallID   string     `json:"call_id"`
	UserID   string     `json:"user_id"`
	JoinedAt *time.Time `json:"joined_at,omitempty"`
	LeftAt   *time.Time `json:"left_at,omitempty"`
}

// PushToken stores a device push token for notifications.
type PushToken struct {
	UserID    string    `json:"user_id"`
	Platform  string    `json:"platform"` // "apns", "fcm", "hms"
	Token     string    `json:"token"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReadReceipt is a batch read-position update.
type ReadReceipt struct {
	ChannelID string `json:"ch"`
	Seq       int64  `json:"seq"`
}

// WsHint is the minimal payload pushed over WebSocket.
type WsHint struct {
	Type      string `json:"t"`
	ChannelID string `json:"ch,omitempty"`
	Seq       int64  `json:"seq,omitempty"`
	UserID    string `json:"uid,omitempty"`
	MsgID     string `json:"msg_id,omitempty"`
	Expire    int    `json:"exp,omitempty"`
}
