// Package chatcard is the server-owned record of one chat interaction.
// The GUI renders a card and sends the person's choice back. It does not
// decide which task resumes, and a model, tool, or page cannot approve.
package chatcard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	KindConfirm       = "confirm"
	KindChoice        = "choice"
	KindMulti         = "multi_choice"
	KindText          = "text"
	KindNativeHandoff = "native_handoff"

	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDeclined   = "declined"
	StatusCancelled  = "cancelled"
	StatusExpired    = "expired"
	StatusSuperseded = "superseded"
	StatusDone       = "done"

	ActorUser  = "user"
	ActorTool  = "tool"
	ActorModel = "model"
	ActorPage  = "page"

	ChoiceApprove   = "approve"
	ChoiceDecline   = "decline"
	ChoiceCancel    = "cancel"
	ChoiceCompleted = "completed"

	// HandoffFullPage is the only handoff surface this slice opens.
	// A region spotlight is not painted here.
	HandoffFullPage = "full_page"
	AttachWatch     = "watch"
)

// Consequential operations. A generic confirm does not grant these.
var consequentialOps = map[string]bool{
	"purchase": true,
	"pay":      true,
	"send":     true,
	"publish":  true,
}

var (
	ErrInvalid      = errors.New("invalid interaction")
	ErrNotFound     = errors.New("interaction not found")
	ErrUnauthorized = errors.New("interaction requires an authenticated user")
	ErrForbidden    = errors.New("interaction is for a different user or task")
	ErrConflict     = errors.New("interaction decision conflicts with the recorded one")
	ErrExpired      = errors.New("interaction expired")
	ErrStale        = errors.New("interaction target or version is stale")
)

// Choice is one button the person can press. The id is what the server
// accepts. The label is only what the card shows.
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Field is one non-secret blank. Password, card, and OTP names are rejected.
type Field struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Operation is the exact displayed action. Version and Hash bind the payload
// the person saw. This slice does not grant purchase, payment, send, or
// publish; those stay on their existing checked paths.
type Operation struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Version int             `json:"version"`
	Hash    string          `json:"hash"`
}

// Evidence is a fact taken from a tool or a page. The card does not invent it.
type Evidence struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Excerpt string `json:"excerpt"`
	URL     string `json:"url,omitempty"`
	Origin  string `json:"origin,omitempty"`
}

// Line is one row on the card. A price, address, or recipient must cite evidence.
type Line struct {
	Text       string `json:"text"`
	EvidenceID string `json:"evidence_id,omitempty"`
}

// Target is the site the person is looking at. Empty means it was not identified.
type Target struct {
	URL    string `json:"url,omitempty"`
	Origin string `json:"origin,omitempty"`
	Frame  string `json:"frame,omitempty"`
	Title  string `json:"title,omitempty"`
}

// Shot is a screenshot the host already captured for this task.
// The card stores the bytes only so the owner can read them back.
// The public card carries the id, time, and desktop lease, not a new picture.
type Shot struct {
	ID          string    `json:"id"`
	CapturedAt  time.Time `json:"captured_at"`
	BotID       string    `json:"bot_id"`
	TaskID      string    `json:"task_id"`
	LeaseID     string    `json:"lease_id"`
	DesktopMode string    `json:"desktop_mode"`
	MIME        string    `json:"mime"`
	Digest      string    `json:"digest"`
}

// Card is one pending question bound to a tenant, user, bot, task, and run.
type Card struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	UserID         string     `json:"user_id"`
	BotID          string     `json:"bot_id"`
	TaskID         string     `json:"task_id"`
	RunID          string     `json:"run_id"`
	Kind           string     `json:"kind"`
	Question       string     `json:"question"`
	Choices        []Choice   `json:"choices,omitempty"`
	Fields         []Field    `json:"fields,omitempty"`
	Operation      Operation  `json:"operation"`
	Evidence       []Evidence `json:"evidence,omitempty"`
	Lines          []Line     `json:"lines,omitempty"`
	Target         *Target    `json:"target,omitempty"`
	Handoff        string     `json:"handoff,omitempty"`
	Spotlight      bool       `json:"spotlight"`
	Shot           *Shot      `json:"shot,omitempty"`
	LeaseID        string     `json:"lease_id,omitempty"`
	DesktopMode    string     `json:"desktop_mode,omitempty"`
	ConsentHash    string     `json:"consent_hash"`
	ConsentVersion int        `json:"consent_version"`
	Status         string     `json:"status"`
	ExpiresAt      time.Time  `json:"expires_at"`
	InputPaused    bool       `json:"input_paused,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Decision       *Decision  `json:"decision,omitempty"`
	Result         *Result    `json:"result,omitempty"`
	ResumeCount    int        `json:"resume_count"`
}

// Decision is the person's recorded choice. It is not proof the action finished.
type Decision struct {
	ChoiceIDs        []string          `json:"choice_ids,omitempty"`
	Fields           map[string]string `json:"fields,omitempty"`
	OperationVersion int               `json:"operation_version"`
	PayloadHash      string            `json:"payload_hash"`
	Decline          bool              `json:"decline,omitempty"`
	Mode             string            `json:"mode,omitempty"`
	At               time.Time         `json:"at"`
}

// Result is a later, evidence-backed outcome. A click cannot write it.
type Result struct {
	Status   string    `json:"status"`
	Evidence string    `json:"evidence,omitempty"`
	At       time.Time `json:"at"`
}

// Actor is who submitted the decision. Only an authenticated user counts.
type Actor struct {
	Kind          string
	TenantID      string
	UserID        string
	Authenticated bool
}

// Submission is the body the GUI transmits. User and tenant come from the
// authenticated caller, never from this body.
type Submission struct {
	RequestID        string
	TaskID           string
	BotID            string
	RunID            string
	ChoiceIDs        []string
	Fields           map[string]string
	OperationVersion int
	PayloadHash      string
	ConsentHash      string
	Decline          bool
	// Target fields are what the card showed. A different frame is rejected.
	TargetOrigin string
	TargetURL    string
	TargetFrame  string
	ShotID       string
	CapturedAt   time.Time
	LeaseID      string
	// InstanceID is server routing copied from the run. It is not part of
	// the consent hash and is not taken from the client body.
	InstanceID string
}

// Outcome is the server's answer. Resume is true only on the transition that
// continues the same task. Done is never set here.
type Outcome struct {
	Status    string `json:"status"`
	Resume    bool   `json:"resume"`
	Already   bool   `json:"already,omitempty"`
	Mode      string `json:"mode,omitempty"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	RequestID string `json:"request_id"`
	BotID     string `json:"bot_id"`
	ResumeID  string `json:"resume_id,omitempty"`
	Done      bool   `json:"done"`
}

// DesktopBinding is where a screenshot click may attach.
// Wake is always false: this slice never starts a different desktop.
type DesktopBinding struct {
	BotID       string    `json:"bot_id"`
	TaskID      string    `json:"task_id"`
	RunID       string    `json:"run_id"`
	LeaseID     string    `json:"lease_id"`
	DesktopMode string    `json:"desktop_mode"`
	CapturedAt  time.Time `json:"captured_at,omitempty"`
	ShotID      string    `json:"shot_id,omitempty"`
	Attach      string    `json:"attach"`
	Wake        bool      `json:"wake"`
}

// OpenInput is the host's request to publish a card.
type OpenInput struct {
	TenantID    string
	UserID      string
	BotID       string
	TaskID      string
	RunID       string
	Kind        string
	Question    string
	Choices     []Choice
	Fields      []Field
	Operation   Operation
	Evidence    []Evidence
	Lines       []Line
	Target      *Target
	Shot        *Shot
	ShotBytes   []byte
	LeaseID     string
	DesktopMode string
	ExpiresAt   time.Time
	InputPaused bool
}

// AskInput is an ask_user the host already produced.
type AskInput struct {
	Question    string
	InputType   string
	Options     []string
	Context     string
	SecretName  string
	BotID       string
	TaskID      string
	RunID       string
	TenantID    string
	UserID      string
	LeaseID     string
	DesktopMode string
	Shot        *Shot
	ShotBytes   []byte
	Evidence    []Evidence
}

func canonicalPayload(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(bytesTrim(raw)) == 0 {
		raw = []byte(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, "", err
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}

func bytesTrim(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func secretField(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, n)
	switch n {
	case "password", "passwd", "pwd", "otp", "cvv", "cvc", "pan", "cardnumber",
		"card", "secret", "token", "pin", "paymentpassword", "paypassword":
		return true
	}
	return strings.Contains(n, "password") || strings.Contains(n, "otp") || strings.Contains(n, "cvv")
}

func consequentialLine(text string) bool {
	t := strings.ToLower(text)
	for _, needle := range []string{"¥", "$", "￥", "元", "价格", "price", "地址", "address", "@"} {
		if strings.Contains(t, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func evidenceByID(items []Evidence) map[string]Evidence {
	out := make(map[string]Evidence, len(items))
	for _, item := range items {
		out[strings.TrimSpace(item.ID)] = item
	}
	return out
}

func validEvidence(item Evidence) bool {
	source := strings.TrimSpace(item.Source)
	return (source == "tool" || source == "page") && strings.TrimSpace(item.Excerpt) != ""
}

// Grants is false for this slice. A chat card does not authorize purchase,
// payment, send, or publish. Those stay on their existing checked paths.
func Grants(Card, string) bool { return false }

// SafeField reports an id this slice will store. It is an allowlist, not a
// scanner that claims to recognize every secret.
func SafeField(id string) bool {
	switch strings.TrimSpace(id) {
	case "info", "note", "detail":
		return true
	default:
		return false
	}
}

func fingerprint(sub Submission) string {
	ids := append([]string(nil), sub.ChoiceIDs...)
	raw, _ := json.Marshal(struct {
		IDs     []string          `json:"ids"`
		Fields  map[string]string `json:"fields,omitempty"`
		Version int               `json:"version"`
		Hash    string            `json:"hash"`
		Decline bool              `json:"decline"`
	}{ids, sub.Fields, sub.OperationVersion, sub.PayloadHash, sub.Decline})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ResumeText is the engine line for the same task. It is not a grant.
func ResumeText(out Outcome) string {
	if out.Mode == "verify" {
		return "The person reports the manual step on this task's computer is done. Inspect the live screen and continue the same task only when the screen shows the result. The report itself is not the result."
	}
	return "A recorded selection continues the same task. Continue that task only."
}

func sameDecision(card Card, sub Submission) bool {
	if card.Decision == nil {
		return false
	}
	return fingerprint(Submission{
		ChoiceIDs:        card.Decision.ChoiceIDs,
		Fields:           card.Decision.Fields,
		OperationVersion: card.Decision.OperationVersion,
		PayloadHash:      card.Decision.PayloadHash,
		Decline:          card.Decision.Decline,
	}) == fingerprint(sub)
}
