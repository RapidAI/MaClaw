// Package lansengerwatch implements local "watch person" rules for Lansenger IM:
// record watched speakers to text, keyword triggers with static reply and/or CLI.
package lansengerwatch

import "time"

const (
	// ConfigFileName is stored under <maclawBase>/lansenger_watch/config.json
	ConfigFileName = "config.json"
	// RosterDirName holds per-group learned member maps.
	RosterDirName = "roster"
	// LogsDirName holds append-only transcript text files.
	LogsDirName = "logs"

	DefaultCLITimeoutSec = 15
	MaxCLITimeoutSec     = 120

	// KeywordScopeTargets: keyword rules only match messages from TargetStaffIDs.
	KeywordScopeTargets = "targets"
	// KeywordScopeAnyone: keyword rules match any speaker in the bound group.
	KeywordScopeAnyone = "anyone"

	// Forward channel keys = the owner's own online IM pathways (not a third person).
	ChannelWeixin    = "weixin"
	ChannelLansenger = "lansenger"
	ChannelTelegram  = "telegram"
	ChannelQQ        = "qq"
	ChannelHub       = "hub" // Hub proactive → user's last active bound IM
)

// Config is the on-disk root document.
type Config struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// Job is one watch configuration (usually bound to one group).
type Job struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// BotProfileID scopes this job to one Lansenger bot. Empty values belong to
	// the migrated default bot for backward compatibility with existing jobs.
	BotProfileID string `json:"bot_profile_id,omitempty"`
	GroupID      string `json:"group_id"`
	GroupName    string `json:"group_name,omitempty"`

	// TargetStaffIDs are Lansenger staffIds to watch (multi-select). Empty means
	// "no one" — never match all members by accident.
	TargetStaffIDs []string          `json:"target_staff_ids"`
	TargetNames    map[string]string `json:"target_names,omitempty"` // staffId -> display name

	// RecordAll appends every message from targets to the daily transcript.
	RecordAll bool `json:"record_all"`

	// KeywordScope: "targets" (default) = only watched people; "anyone" = whole group.
	KeywordScope string `json:"keyword_scope,omitempty"`

	// ForwardOnTargetSpeech: when a watched target speaks, push a packaged copy
	// to the owner's selected IM channels (ForwardChannels).
	ForwardOnTargetSpeech bool `json:"forward_on_target_speech,omitempty"`

	// ForwardChannels are the owner's online IM pathways to notify, e.g.
	// weixin / lansenger / telegram / qq / hub. Not other people — the same
	// channels where "I" chat with the bot.
	ForwardChannels []string `json:"forward_channels,omitempty"`

	// ForwardStaffIDs is deprecated (was mistaken for people). Kept for config
	// compatibility; ignored by the engine.
	ForwardStaffIDs []string `json:"forward_staff_ids,omitempty"`

	// Keywords are optional trigger rules (OR within a rule's Keywords list).
	Keywords []KeywordRule `json:"keywords,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KeywordRule fires when any keyword matches the message text.
type KeywordRule struct {
	ID string `json:"id"`
	// Keywords: case-insensitive substring match (trimmed, non-empty).
	Keywords []string `json:"keywords"`
	// CaseSensitive defaults false.
	CaseSensitive bool `json:"case_sensitive,omitempty"`

	// RecordOnMatch also writes the line to the keyword-hits log (and all log if RecordAll).
	RecordOnMatch bool `json:"record_on_match"`

	// ReplyText is sent to the group/user when set and CLI does not produce stdout.
	ReplyText string `json:"reply_text,omitempty"`

	// CLICommand is a shell command line. Supports placeholders:
	//   {{date}} {{content}} {{speaker_id}} {{speaker_name}}
	//   {{group_id}} {{group_name}} {{keyword}} {{message_id}}
	// If no placeholders are present, standard flags are appended:
	//   --date --content --speaker-id --group-id --keyword
	// Env vars LANXIN_WATCH_* are always set.
	CLICommand string `json:"cli_command,omitempty"`

	// CLITimeoutSec bounds process runtime (default 15, max 120).
	CLITimeoutSec int `json:"cli_timeout_sec,omitempty"`

	// ReplyWithCLIStdout when true (default true if CLICommand set) sends CLI
	// stdout as the IM reply when the process succeeds and stdout is non-empty.
	// When false, CLI still runs (side effects) but only ReplyText is used for IM.
	ReplyWithCLIStdout *bool `json:"reply_with_cli_stdout,omitempty"`

	// ForwardOnMatch: package this hit and push to job.ForwardChannels.
	ForwardOnMatch bool `json:"forward_on_match,omitempty"`
}

// Member is a learned or manually added group participant.
type Member struct {
	StaffID    string    `json:"staff_id"`
	Name       string    `json:"name,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
	Source     string    `json:"source,omitempty"` // "message" | "manual"
}

// GroupRoster is persisted per group_id.
type GroupRoster struct {
	GroupID   string    `json:"group_id"`
	GroupName string    `json:"group_name,omitempty"`
	Members   []Member  `json:"members"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Incoming is the minimal message surface needed by the engine.
type Incoming struct {
	GroupID     string
	GroupName   string
	SpeakerID   string
	SpeakerName string
	Text        string
	MessageID   string
	ReceivedAt  time.Time
	IsGroup     bool
}

// ActionResult describes side effects for one processed message.
type ActionResult struct {
	MatchedJobIDs []string
	RecordedAll   bool
	KeywordHits   []KeywordHit
	Replies       []string         // texts to send back to the source group (may be multiple)
	CLILogs       []string         // diagnostic lines (not sent to IM)
	Forwards      []ForwardRequest // private DM packages to deliver
}

// ForwardRequest is a push to one of the owner's IM channels.
type ForwardRequest struct {
	JobID   string
	Channel string // weixin | lansenger | telegram | qq | hub
	Text    string
	Reason  string // "target_speech" | "keyword"
	Keyword string
}

// KeywordHit is one matched keyword rule.
type KeywordHit struct {
	JobID        string
	RuleID       string
	Keyword      string
	ReplyText    string
	CLICommand   string
	CLIStdout    string
	CLIStderr    string
	CLIError     string
	UsedCLIReply bool
	Forwarded    bool
}
