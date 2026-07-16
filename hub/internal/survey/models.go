package survey

import (
	"encoding/json"
	"time"
)

const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusClosed    = "closed"
	StatusArchived  = "archived"

	PlatformLansenger = "lansenger"

	PhaseAnswering     = "answering"
	PhaseConfirmUpdate = "confirm_update"

	SessionTTL = 30 * time.Minute

	CrockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	ShortCodeLen      = 6
	ShortCodeRetries  = 8
)

// Settings is persisted in settings_json (includes server-only salt).
type Settings struct {
	Anonymous     bool       `json:"anonymous"`
	AllowUpdate   bool       `json:"allow_update"`
	AllowP2P      bool       `json:"allow_p2p"`
	Deadline      *time.Time `json:"deadline,omitempty"`
	TargetCount   int        `json:"target_count"`
	AnonymitySalt string     `json:"anonymity_salt,omitempty"`
}

// SettingsIn is client-supplied settings (no salt).
type SettingsIn struct {
	Anonymous   bool       `json:"anonymous"`
	AllowUpdate bool       `json:"allow_update"`
	AllowP2P    bool       `json:"allow_p2p"`
	Deadline    *time.Time `json:"deadline,omitempty"`
	TargetCount int        `json:"target_count"`
}

type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Question struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"` // single_choice|multi_choice|text|rating
	Title     string   `json:"title"`
	Required  bool     `json:"required"`
	Position  int      `json:"position"`
	Options   []Option `json:"options,omitempty"`
	Min       *int     `json:"min,omitempty"`
	Max       *int     `json:"max,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
}

type Binding struct {
	Platform  string    `json:"platform"`
	GroupID   string    `json:"group_id"`
	GroupName string    `json:"group_name"`
	BoundAt   time.Time `json:"bound_at"`
}

type Survey struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	ShortCode     string     `json:"short_code"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Settings      Settings   `json:"settings"`
	Questions     []Question `json:"questions,omitempty"`
	Bindings      []Binding  `json:"bindings,omitempty"`
	BindingCount  int        `json:"binding_count,omitempty"`
	QuestionCount int        `json:"question_count,omitempty"`
	ResponseCount int        `json:"response_count,omitempty"`
	CreatedBy     string     `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
}

// Redact clears server-only fields for API responses.
func (s *Survey) Redact() {
	if s == nil {
		return
	}
	s.Settings.AnonymitySalt = ""
}

type CreateInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Questions   []Question `json:"questions"`
	Settings    SettingsIn `json:"settings"`
}

type UpdateInput struct {
	Title       *string     `json:"title,omitempty"`
	Description *string     `json:"description,omitempty"`
	Questions   *[]Question `json:"questions,omitempty"`
	Settings    *SettingsIn `json:"settings,omitempty"`
}

type PublishOptions struct {
	Announce bool `json:"announce"`
}

type Response struct {
	ID             string          `json:"id"`
	SurveyID       string          `json:"survey_id"`
	TenantID       string          `json:"tenant_id"`
	Platform       string          `json:"platform"`
	RespondentKey  string          `json:"respondent_key"`
	RespondentName string          `json:"respondent_name"`
	GroupID        string          `json:"group_id"`
	Answers        json.RawMessage `json:"answers"`
	SubmittedAt    time.Time       `json:"submitted_at"`
}

type Session struct {
	SessionKey string
	TenantID   string
	SurveyID   string
	Platform   string
	UserID     string
	UserName   string
	GroupID    string
	Phase      string
	Cursor     int
	Answers    map[string]any
	ExpiresAt  time.Time
	UpdatedAt  time.Time
}

// IMHandleRequest is the body of POST /api/v1/surveys/im/handle.
type IMHandleRequest struct {
	Platform  string `json:"platform"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	ChatType  string `json:"chat_type"` // p2p|group
	GroupID   string `json:"group_id"`
	Text      string `json:"text"` // already stripped of @mentions when possible
	IsAtMe    bool   `json:"is_at_me"`
	RawText   string `json:"raw_text,omitempty"`
}

// IMHandleResponse is returned to the gateway machine.
type IMHandleResponse struct {
	Handled   bool   `json:"handled"`
	ReplyText string `json:"reply_text,omitempty"`
	// SurveyID is set when Event is non-empty so the desktop can refresh results.
	SurveyID string `json:"survey_id,omitempty"`
	// Event is a lightweight signal for the gateway (e.g. "response_submitted").
	Event string `json:"event,omitempty"`
}

type Stats struct {
	SurveyID      string            `json:"survey_id"`
	ResponseCount int               `json:"response_count"`
	TargetCount   int               `json:"target_count,omitempty"`
	ByQuestion    []QuestionStats   `json:"by_question"`
}

type QuestionStats struct {
	QuestionID string         `json:"question_id"`
	Title      string         `json:"title"`
	Type       string         `json:"type"`
	Options    []OptionCount  `json:"options,omitempty"`
	TextCount  int            `json:"text_count,omitempty"`
	RatingAvg  float64        `json:"rating_avg,omitempty"`
	RatingN    int            `json:"rating_n,omitempty"`
}

type OptionCount struct {
	OptionID string `json:"option_id"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Percent  float64 `json:"percent"`
}
