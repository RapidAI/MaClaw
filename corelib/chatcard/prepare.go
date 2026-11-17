package chatcard

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	maxShotBytes = 8 << 20
	defaultTTL   = 2 * time.Hour
)

// Prepare checks a host open request and fills the ids the client must echo.
func Prepare(in OpenInput, now time.Time) (Card, []byte, error) {
	card := Card{
		TenantID:    strings.TrimSpace(in.TenantID),
		UserID:      strings.TrimSpace(in.UserID),
		BotID:       strings.TrimSpace(in.BotID),
		TaskID:      strings.TrimSpace(in.TaskID),
		RunID:       strings.TrimSpace(in.RunID),
		Kind:        strings.TrimSpace(in.Kind),
		Question:    strings.TrimSpace(in.Question),
		Choices:     in.Choices,
		Fields:      in.Fields,
		Operation:   in.Operation,
		Evidence:    in.Evidence,
		Lines:       in.Lines,
		Target:      in.Target,
		Shot:        in.Shot,
		LeaseID:     strings.TrimSpace(leaseOf(in)),
		DesktopMode: strings.TrimSpace(modeOf(in)),
		InputPaused: in.InputPaused,
		Status:      StatusPending,
		Spotlight:   false,
		Handoff:     HandoffFullPage,
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}
	if card.TenantID == "" || card.UserID == "" || card.BotID == "" || card.TaskID == "" || card.RunID == "" {
		return Card{}, nil, fmt.Errorf("%w: tenant, user, bot, task, and run are required", ErrInvalid)
	}
	if card.Question == "" {
		return Card{}, nil, fmt.Errorf("%w: question is required", ErrInvalid)
	}
	switch card.Kind {
	case KindConfirm, KindChoice, KindMulti, KindText, KindNativeHandoff:
	default:
		return Card{}, nil, fmt.Errorf("%w: unknown kind", ErrInvalid)
	}
	payload, hash, err := canonicalPayload(card.Operation.Payload)
	if err != nil {
		return Card{}, nil, fmt.Errorf("%w: operation payload", ErrInvalid)
	}
	card.Operation.Payload = payload
	card.Operation.Hash = hash
	if card.Operation.Version < 1 {
		card.Operation.Version = 1
	}
	if strings.TrimSpace(card.Operation.Kind) == "" {
		card.Operation.Kind = "continue"
	}
	if consequentialKind(card.Operation.Kind) || paymentWording(card.Question) {
		return Card{}, nil, fmt.Errorf("%w: this card cannot grant purchase, payment, send, or publish", ErrInvalid)
	}
	if err := validateFacts(&card); err != nil {
		return Card{}, nil, err
	}
	if err := validateShape(&card); err != nil {
		return Card{}, nil, err
	}
	var shot []byte
	if card.Kind == KindNativeHandoff && len(in.ShotBytes) > 0 {
		shot, err = validateShot(in.ShotBytes)
		if err != nil {
			return Card{}, nil, err
		}
		if strings.TrimSpace(card.LeaseID) == "" || (card.DesktopMode != "isolated" && card.DesktopMode != "shared") {
			return Card{}, nil, fmt.Errorf("%w: a screenshot needs this task's desktop lease", ErrInvalid)
		}
		if card.Shot == nil {
			card.Shot = &Shot{}
		}
		if card.Shot.BotID != "" && card.Shot.BotID != card.BotID {
			return Card{}, nil, fmt.Errorf("%w: screenshot bot does not match the card", ErrForbidden)
		}
		if card.Shot.TaskID != "" && card.Shot.TaskID != card.TaskID {
			return Card{}, nil, fmt.Errorf("%w: screenshot task does not match the card", ErrForbidden)
		}
		card.Shot.BotID = card.BotID
		card.Shot.TaskID = card.TaskID
		card.Shot.LeaseID = card.LeaseID
		card.Shot.DesktopMode = card.DesktopMode
		card.Shot.MIME = sniffImage(shot)
		sum := sha256.Sum256(shot)
		card.Shot.Digest = hex.EncodeToString(sum[:])
		if card.Shot.CapturedAt.IsZero() {
			card.Shot.CapturedAt = now.UTC()
		} else {
			card.Shot.CapturedAt = card.Shot.CapturedAt.UTC()
		}
		card.InputPaused = true
	}
	if card.Kind == KindNativeHandoff && len(shot) == 0 {
		// No picture was captured for this turn. Keep the instruction and,
		// when the lease is known, the button that opens that computer.
		// Do not invent an image.
		card.Shot = nil
		if card.LeaseID != "" && card.DesktopMode != "isolated" && card.DesktopMode != "shared" {
			return Card{}, nil, fmt.Errorf("%w: desktop mode must be isolated or shared", ErrInvalid)
		}
	}
	if in.ExpiresAt.IsZero() {
		card.ExpiresAt = now.UTC().Add(defaultTTL)
	} else {
		card.ExpiresAt = in.ExpiresAt.UTC()
	}
	card.ConsentVersion = 1
	card.ConsentHash = ConsentOf(card)
	return card, shot, nil
}

func consequentialKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "purchase", "pay", "payment", "checkout", "send", "publish":
		return true
	default:
		return consequentialOps[strings.ToLower(strings.TrimSpace(kind))]
	}
}

func paymentWording(text string) bool {
	t := strings.ToLower(text)
	for _, needle := range []string{"确认支付", "确认付款", "确认下单", "立即支付", "立即付款", "purchase", "pay now"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

// ConsentOf is the hash of everything the card shows. A changed line, option,
// price, address, origin, frame, or payload is a different hash.
func ConsentOf(card Card) string {
	doc := struct {
		Version     int             `json:"version"`
		TaskID      string          `json:"task_id"`
		RunID       string          `json:"run_id"`
		BotID       string          `json:"bot_id"`
		Kind        string          `json:"kind"`
		Question    string          `json:"question"`
		Choices     []Choice        `json:"choices,omitempty"`
		Fields      []Field         `json:"fields,omitempty"`
		Lines       []Line          `json:"lines,omitempty"`
		Target      *Target         `json:"target,omitempty"`
		OpKind      string          `json:"operation_kind"`
		OpVersion   int             `json:"operation_version"`
		OpPayload   json.RawMessage `json:"operation_payload,omitempty"`
		LeaseID     string          `json:"lease_id,omitempty"`
		DesktopMode string          `json:"desktop_mode,omitempty"`
	}{
		Version: card.ConsentVersion, TaskID: card.TaskID, RunID: card.RunID, BotID: card.BotID,
		Kind: card.Kind, Question: card.Question, Choices: card.Choices, Fields: card.Fields,
		Lines: card.Lines, Target: card.Target, OpKind: card.Operation.Kind, OpVersion: card.Operation.Version,
		OpPayload: card.Operation.Payload, LeaseID: card.LeaseID, DesktopMode: card.DesktopMode,
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validateShape(card *Card) error {
	switch card.Kind {
	case KindConfirm:
		if len(card.Choices) == 0 {
			card.Choices = []Choice{{ID: ChoiceApprove, Label: "确认"}, {ID: ChoiceDecline, Label: "取消"}}
		}
		if len(card.Choices) != 2 || card.Choices[0].ID != ChoiceApprove || card.Choices[1].ID != ChoiceDecline {
			return fmt.Errorf("%w: confirm choices must be approve and decline", ErrInvalid)
		}
	case KindChoice, KindMulti:
		if card.Kind == KindChoice && len(card.Choices) < 1 {
			return fmt.Errorf("%w: choice needs an option", ErrInvalid)
		}
		if card.Kind == KindMulti && len(card.Choices) < 2 {
			return fmt.Errorf("%w: multi_choice needs two options", ErrInvalid)
		}
		seen := map[string]bool{}
		for i := range card.Choices {
			id := strings.TrimSpace(card.Choices[i].ID)
			if id == "" {
				id = fmt.Sprintf("opt_%d", i)
				card.Choices[i].ID = id
			}
			if secretField(id) || secretField(card.Choices[i].Label) {
				return fmt.Errorf("%w: choices cannot collect a secret", ErrInvalid)
			}
			if seen[id] || strings.TrimSpace(card.Choices[i].Label) == "" {
				return fmt.Errorf("%w: choice id and label", ErrInvalid)
			}
			seen[id] = true
		}
	case KindText:
		if len(card.Fields) == 0 {
			card.Fields = []Field{{ID: "info", Label: card.Question}}
		}
		for _, field := range card.Fields {
			if !SafeField(field.ID) || strings.TrimSpace(field.Label) == "" {
				return fmt.Errorf("%w: text fields are limited to info, note, and detail", ErrInvalid)
			}
		}
	case KindNativeHandoff:
		card.Fields = nil
		card.Choices = []Choice{{ID: ChoiceCancel, Label: "取消"}, {ID: ChoiceCompleted, Label: "我已完成"}}
		card.Handoff = HandoffFullPage
		card.Spotlight = false
		if card.Operation.Kind == "" || consequentialOps[card.Operation.Kind] {
			// Completing the handoff never grants purchase, send, or publish.
			card.Operation.Kind = "verify_native"
		}
	}
	return nil
}

func validateFacts(card *Card) error {
	for _, line := range card.Lines {
		if consequentialLine(line.Text) || paymentWording(line.Text) {
			return fmt.Errorf("%w: a price, address, or recipient is not evidence on this card", ErrInvalid)
		}
	}
	// Client-supplied evidence text is not an authority. It is not stored.
	card.Evidence = nil
	return nil
}

func leaseOf(in OpenInput) string {
	if strings.TrimSpace(in.LeaseID) != "" {
		return in.LeaseID
	}
	if in.Shot != nil {
		return in.Shot.LeaseID
	}
	return ""
}

func modeOf(in OpenInput) string {
	if strings.TrimSpace(in.DesktopMode) != "" {
		return in.DesktopMode
	}
	if in.Shot != nil {
		return in.Shot.DesktopMode
	}
	return ""
}

func excerptHolds(excerpt, fact string) bool {
	excerpt = strings.TrimSpace(excerpt)
	fact = strings.TrimSpace(fact)
	return excerpt != "" && fact != "" && strings.Contains(excerpt, fact)
}

func validateShot(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > maxShotBytes {
		return nil, fmt.Errorf("%w: screenshot size", ErrInvalid)
	}
	if sniffImage(raw) == "" {
		return nil, fmt.Errorf("%w: screenshot must be a captured png or jpeg", ErrInvalid)
	}
	return append([]byte(nil), raw...), nil
}

func sniffImage(raw []byte) string {
	if len(raw) >= 8 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png"
	}
	if len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff {
		return "image/jpeg"
	}
	return ""
}

// DecodeShot accepts the base64 text the desktop tool already stored.
func DecodeShot(data string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
	if err != nil {
		return nil, fmt.Errorf("%w: screenshot encoding", ErrInvalid)
	}
	return validateShot(raw)
}

// FromAsk builds a card from an ask_user result. Secrets become a handoff
// with Cancel and I've completed, never a password field.
func FromAsk(in AskInput, now time.Time) (Card, []byte, error) {
	kind := strings.TrimSpace(in.InputType)
	secret := kind == "secret" || kind == "secret_fill" || secretField(in.SecretName) || secretField(in.Question)
	open := OpenInput{
		TenantID:  in.TenantID,
		UserID:    in.UserID,
		BotID:     in.BotID,
		TaskID:    in.TaskID,
		RunID:     in.RunID,
		Question:  in.Question,
		Evidence:  in.Evidence,
		Shot:      in.Shot,
		ShotBytes: in.ShotBytes,
	}
	if secret {
		open.Kind = KindNativeHandoff
		open.Question = firstText(in.Question, "请在这台电脑上完成这一步")
		open.Operation = Operation{Kind: "verify_native", Payload: json.RawMessage(`{"step":"native"}`)}
		open.InputPaused = true
		if lease := strings.TrimSpace(in.LeaseID); lease != "" {
			mode := strings.TrimSpace(in.DesktopMode)
			if mode != "isolated" && mode != "shared" {
				return Card{}, nil, fmt.Errorf("%w: desktop mode must be isolated or shared", ErrInvalid)
			}
			if open.Shot == nil {
				open.Shot = &Shot{}
			}
			open.Shot.LeaseID = lease
			open.Shot.DesktopMode = mode
		}
		if url := firstURL(in.Context); url != "" {
			open.Target = &Target{URL: url, Origin: originOf(url)}
			open.Lines = []Line{{Text: url, EvidenceID: ""}}
		}
		return Prepare(open, now)
	}
	switch kind {
	case "confirm":
		open.Kind = KindConfirm
		open.Operation = Operation{Kind: "continue", Payload: mustJSON(map[string]any{"question": open.Question})}
	case "multi_choice":
		open.Kind = KindMulti
		open.Choices = choiceList(in.Options)
		open.Operation = Operation{Kind: "continue", Payload: mustJSON(map[string]any{"options": in.Options})}
	case "choice":
		open.Kind = KindChoice
		open.Choices = choiceList(in.Options)
		open.Operation = Operation{Kind: "continue", Payload: mustJSON(map[string]any{"options": in.Options})}
	default:
		if len(in.Options) > 1 {
			open.Kind = KindChoice
			open.Choices = choiceList(in.Options)
			open.Operation = Operation{Kind: "continue", Payload: mustJSON(map[string]any{"options": in.Options})}
		} else {
			open.Kind = KindText
			open.Operation = Operation{Kind: "continue", Payload: mustJSON(map[string]any{"question": open.Question})}
		}
	}
	if strings.TrimSpace(open.Question) == "" {
		return Card{}, nil, fmt.Errorf("%w: question is required", ErrInvalid)
	}
	return Prepare(open, now)
}

func choiceList(options []string) []Choice {
	out := make([]Choice, 0, len(options))
	for i, opt := range options {
		label := strings.TrimSpace(opt)
		if label == "" {
			continue
		}
		out = append(out, Choice{ID: fmt.Sprintf("opt_%d", i), Label: label})
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstURL(text string) string {
	for _, field := range strings.Fields(text) {
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return strings.TrimRight(field, ".,);")
		}
	}
	return ""
}

func originOf(raw string) string {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// Public is the card the GUI may render. Image bytes stay on the server.
func Public(card Card) Card {
	out := card
	out.Evidence = append([]Evidence(nil), card.Evidence...)
	for i := range out.Evidence {
		out.Evidence[i].Excerpt = ""
	}
	if out.Shot != nil {
		shot := *out.Shot
		out.Shot = &shot
	}
	out.Decision = nil
	if card.Decision != nil {
		d := *card.Decision
		d.Fields = nil
		out.Decision = &d
	}
	return out
}
