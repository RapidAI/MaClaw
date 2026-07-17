package survey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func NewAnonymitySalt() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func ComputeRespondentKey(anonymous bool, saltB64, staffID string) (string, error) {
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return "", fmt.Errorf("staff id required")
	}
	if !anonymous {
		return staffID, nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(saltB64))
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid anonymity salt")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(staffID))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func GenerateShortCode() (string, error) {
	raw := make([]byte, ShortCodeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(ShortCodeLen)
	for _, r := range raw {
		b.WriteByte(CrockfordAlphabet[int(r)%len(CrockfordAlphabet)])
	}
	return b.String(), nil
}

func NormalizeShortCode(in string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(in))
	// Crockford confusables: O→0, I/L→1 (common when users retype short codes).
	s = strings.Map(func(r rune) rune {
		switch r {
		case 'O':
			return '0'
		case 'I', 'L':
			return '1'
		default:
			return r
		}
	}, s)
	if len(s) != ShortCodeLen {
		return "", fmt.Errorf("%w: must be %d characters", ErrInvalidShortCode, ShortCodeLen)
	}
	for _, r := range s {
		if !strings.ContainsRune(CrockfordAlphabet, r) {
			return "", fmt.Errorf("%w: invalid character", ErrInvalidShortCode)
		}
	}
	return s, nil
}

func SessionKey(platform, userID string) string {
	return strings.TrimSpace(platform) + ":" + strings.TrimSpace(userID)
}

// GroupSessionKey scopes an IM session to one group so the same user can run
// independent sessions in different groups (and in p2p) without interference.
func GroupSessionKey(platform, groupID, userID string) string {
	return strings.TrimSpace(platform) + ":" + strings.TrimSpace(groupID) + ":" + strings.TrimSpace(userID)
}

// IMSessionKey picks the session key for an inbound IM message: group chats
// get a group-scoped key, p2p keeps the legacy per-user key.
func IMSessionKey(platform, chatType, groupID, userID string) string {
	ct := strings.ToLower(strings.TrimSpace(chatType))
	gid := strings.TrimSpace(groupID)
	isP2P := ct == "p2p" || ct == "private" || (ct == "" && gid == "")
	if !isP2P && gid != "" {
		return GroupSessionKey(platform, gid, userID)
	}
	return SessionKey(platform, userID)
}

// Sentinel answer-parse errors so IM replies can be localized by key instead
// of matching English detail strings.
var (
	ErrEmptyAnswer           = errors.New("empty answer")
	ErrOptionIndexOutOfRange = errors.New("option index out of range")
	ErrAmbiguousOption       = errors.New("ambiguous option")
	ErrUnknownOption         = errors.New("unknown option")
	ErrRatingNotInteger      = errors.New("rating must be an integer")
	ErrRatingOutOfRange      = errors.New("rating out of range")
	ErrAnswerRequired        = errors.New("required")
	ErrTextTooLong           = errors.New("text too long")
	ErrUnsupportedQType      = errors.New("unsupported type")
)

// LocalizedAnswerError maps a ParseAnswer error to a localized short message.
func LocalizedAnswerError(q Question, err error, lang string) string {
	min, max := 1, 5
	if q.Min != nil {
		min = *q.Min
	}
	if q.Max != nil {
		max = *q.Max
	}
	switch {
	case errors.Is(err, ErrEmptyAnswer):
		return tr(lang, msgErrEmptyAnswer)
	case errors.Is(err, ErrOptionIndexOutOfRange):
		return tr(lang, msgErrOptionRange)
	case errors.Is(err, ErrAmbiguousOption):
		return tr(lang, msgErrAmbiguous)
	case errors.Is(err, ErrUnknownOption):
		return tr(lang, msgErrUnknownOption)
	case errors.Is(err, ErrRatingNotInteger):
		return tr(lang, msgErrRatingInt)
	case errors.Is(err, ErrRatingOutOfRange):
		return tr(lang, msgErrRatingRange, min, max)
	case errors.Is(err, ErrAnswerRequired):
		return tr(lang, msgErrRequired)
	case errors.Is(err, ErrTextTooLong):
		return tr(lang, msgErrTextTooLong)
	case errors.Is(err, ErrUnsupportedQType):
		return tr(lang, msgErrUnsupportedType)
	default:
		return err.Error()
	}
}

func NormalizeQuestions(qs []Question) []Question {
	out := make([]Question, 0, len(qs))
	for i, q := range qs {
		q.Position = i
		if strings.TrimSpace(q.ID) == "" {
			q.ID = fmt.Sprintf("q%d", i+1)
		}
		q.Type = strings.TrimSpace(q.Type)
		q.Title = strings.TrimSpace(q.Title)
		for j := range q.Options {
			if strings.TrimSpace(q.Options[j].ID) == "" {
				q.Options[j].ID = fmt.Sprintf("opt%d", j+1)
			}
			q.Options[j].Label = strings.TrimSpace(q.Options[j].Label)
		}
		out = append(out, q)
	}
	return out
}

const (
	MaxQuestionsPerSurvey = 50
	MaxOptionsPerQuestion = 30
	MaxQuestionTitleRunes = 500
	MaxOptionLabelRunes   = 200
	MaxTargetCount        = 1_000_000
	MaxGroupIDRunes       = 128
	MaxGroupNameRunes     = 200
	MaxBindingsPerRequest = 50
)

func ValidateDraftQuestions(qs []Question) error {
	if len(qs) == 0 {
		return fmt.Errorf("at least one question is required")
	}
	if len(qs) > MaxQuestionsPerSurvey {
		return fmt.Errorf("too many questions (max %d)", MaxQuestionsPerSurvey)
	}
	seenQ := make(map[string]struct{}, len(qs))
	for _, q := range qs {
		qid := strings.TrimSpace(q.ID)
		if qid == "" {
			return fmt.Errorf("question id required")
		}
		if _, dup := seenQ[qid]; dup {
			return fmt.Errorf("duplicate question id %q", qid)
		}
		seenQ[qid] = struct{}{}
		if q.Title == "" {
			return fmt.Errorf("question title required")
		}
		if len([]rune(q.Title)) > MaxQuestionTitleRunes {
			return fmt.Errorf("question title too long (max %d)", MaxQuestionTitleRunes)
		}
		switch q.Type {
		case "single_choice", "multi_choice":
			if len(q.Options) < 2 {
				return fmt.Errorf("choice question needs at least 2 options")
			}
			if len(q.Options) > MaxOptionsPerQuestion {
				return fmt.Errorf("too many options (max %d)", MaxOptionsPerQuestion)
			}
			seenO := make(map[string]struct{}, len(q.Options))
			for _, o := range q.Options {
				label := strings.TrimSpace(o.Label)
				if label == "" {
					return fmt.Errorf("choice option label required")
				}
				if len([]rune(label)) > MaxOptionLabelRunes {
					return fmt.Errorf("option label too long (max %d)", MaxOptionLabelRunes)
				}
				oid := strings.TrimSpace(o.ID)
				if oid == "" {
					return fmt.Errorf("option id required")
				}
				if _, dup := seenO[oid]; dup {
					return fmt.Errorf("duplicate option id %q", oid)
				}
				seenO[oid] = struct{}{}
			}
		case "text":
			if q.MaxLength != nil {
				if *q.MaxLength < 0 {
					return fmt.Errorf("text max_length must be >= 0")
				}
				// Cap abusive client values (IM/export stay usable).
				if *q.MaxLength > 10000 {
					return fmt.Errorf("text max_length must be <= 10000")
				}
			}
		case "rating":
			min, max := 1, 5
			if q.Min != nil {
				min = *q.Min
			}
			if q.Max != nil {
				max = *q.Max
			}
			if min > max {
				return fmt.Errorf("rating min > max")
			}
			if min < -1000 || max > 1000 {
				return fmt.Errorf("rating range out of bounds")
			}
		default:
			return fmt.Errorf("unsupported question type %q", q.Type)
		}
	}
	return nil
}

// ValidateSettingsIn rejects impossible settings values from clients.
func ValidateSettingsIn(s SettingsIn) error {
	if s.TargetCount < 0 {
		return fmt.Errorf("target_count must be >= 0")
	}
	if s.TargetCount > MaxTargetCount {
		return fmt.Errorf("target_count too large (max %d)", MaxTargetCount)
	}
	return nil
}

// normalizeFullwidthDigits maps ０-９ → 0-9 so mobile IM fullwidth input parses.
func normalizeFullwidthDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '０' && r <= '９' {
			b.WriteRune(r - '０' + '0')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ParseChoiceToken maps user input to a single option id.
func ParseChoiceToken(q Question, token string) (string, error) {
	token = strings.TrimSpace(normalizeFullwidthDigits(token))
	if token == "" {
		return "", ErrEmptyAnswer
	}
	// 1-based index
	if n, err := strconv.Atoi(token); err == nil {
		if n >= 1 && n <= len(q.Options) {
			return q.Options[n-1].ID, nil
		}
		return "", ErrOptionIndexOutOfRange
	}
	low := strings.ToLower(token)
	var matches []string
	for _, o := range q.Options {
		if strings.EqualFold(o.ID, token) || strings.ToLower(o.Label) == low {
			matches = append(matches, o.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", ErrAmbiguousOption
	}
	return "", ErrUnknownOption
}

func ParseMultiChoice(q Question, text string) ([]string, error) {
	text = strings.TrimSpace(normalizeFullwidthDigits(text))
	if text == "" {
		return nil, ErrEmptyAnswer
	}
	// split on comma, whitespace, Chinese顿号
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || unicode.IsSpace(r)
	})
	seen := map[string]struct{}{}
	var ids []string
	for _, f := range fields {
		id, err := ParseChoiceToken(q, f)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, ErrEmptyAnswer
	}
	sort.Strings(ids)
	return ids, nil
}

func ParseRating(q Question, text string) (int, error) {
	text = strings.TrimSpace(normalizeFullwidthDigits(text))
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, ErrRatingNotInteger
	}
	min, max := 1, 5
	if q.Min != nil {
		min = *q.Min
	}
	if q.Max != nil {
		max = *q.Max
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%w [%d,%d]", ErrRatingOutOfRange, min, max)
	}
	return n, nil
}

func ParseText(q Question, text string) (string, error) {
	s := strings.TrimSpace(text)
	maxLen := 500
	if q.MaxLength != nil && *q.MaxLength > 0 {
		maxLen = *q.MaxLength
	}
	if q.Required && s == "" {
		return "", ErrAnswerRequired
	}
	if len([]rune(s)) > maxLen {
		return "", ErrTextTooLong
	}
	return s, nil
}

func ParseAnswer(q Question, text string) (any, error) {
	switch q.Type {
	case "single_choice":
		return ParseChoiceToken(q, text)
	case "multi_choice":
		return ParseMultiChoice(q, text)
	case "rating":
		return ParseRating(q, text)
	case "text":
		return ParseText(q, text)
	default:
		return nil, ErrUnsupportedQType
	}
}

// IsSkipToken reports optional-question skip replies (「跳过」 / skip).
func IsSkipToken(text string) bool {
	t := strings.TrimSpace(text)
	if t == "跳过" {
		return true
	}
	switch strings.ToLower(t) {
	case "skip", "-":
		return true
	default:
		return false
	}
}

// FormatQuestionPrompt renders one question for IM in the given language.
// The "previous question" hint is only shown when there is one to go back to.
func FormatQuestionPrompt(q Question, index, total int, lang string) string {
	var b strings.Builder
	b.WriteString(tr(lang, msgPromptProgress, index+1, total, q.Title))
	if q.Required {
		b.WriteString(" *")
	} else {
		b.WriteString(tr(lang, msgPromptOptional))
	}
	b.WriteString("\n")
	switch q.Type {
	case "single_choice", "multi_choice":
		for i, o := range q.Options {
			fmt.Fprintf(&b, "%d. %s\n", i+1, o.Label)
		}
		if q.Type == "multi_choice" {
			b.WriteString(tr(lang, msgPromptMulti))
		} else {
			b.WriteString(tr(lang, msgPromptSingle))
		}
	case "rating":
		min, max := 1, 5
		if q.Min != nil {
			min = *q.Min
		}
		if q.Max != nil {
			max = *q.Max
		}
		b.WriteString(tr(lang, msgPromptRating, min, max))
	case "text":
		b.WriteString(tr(lang, msgPromptText))
	}
	if !q.Required {
		b.WriteString(tr(lang, msgPromptSkipHint))
	}
	if index > 0 {
		b.WriteString(tr(lang, msgPromptTailPrev))
	} else {
		b.WriteString(tr(lang, msgPromptTailFirst))
	}
	return strings.TrimRight(b.String(), "\n")
}

func OptionLabelByID(q Question, id string) string {
	for _, o := range q.Options {
		if o.ID == id {
			return o.Label
		}
	}
	return id
}

// MultiLabelsInOptionOrder returns labels for selected ids in options array order.
func MultiLabelsInOptionOrder(q Question, ids []string) []string {
	set := map[string]struct{}{}
	for _, id := range ids {
		set[id] = struct{}{}
	}
	var labels []string
	for _, o := range q.Options {
		if _, ok := set[o.ID]; ok {
			labels = append(labels, o.Label)
		}
	}
	return labels
}

func AnswersToJSON(m map[string]any) (json.RawMessage, error) {
	if m == nil {
		m = map[string]any{}
	}
	return json.Marshal(m)
}

func JSONToAnswers(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func DeadlinePassed(s Settings, now time.Time) bool {
	if s.Deadline == nil {
		return false
	}
	return !now.Before(s.Deadline.UTC())
}

// FormatDeadlineLine returns a short localized line for IM, or empty if no deadline.
func FormatDeadlineLine(s Settings, lang string) string {
	if s.Deadline == nil {
		return ""
	}
	// Local-friendly UTC display with Z; desktop UI uses locale separately.
	return tr(lang, msgMetaDeadline, s.Deadline.UTC().Format("2006-01-02 15:04 UTC"))
}

// SurveyIntroMeta appends optional deadline/target hints under survey title for IM.
func SurveyIntroMeta(sv *Survey, lang string) string {
	if sv == nil {
		return ""
	}
	var parts []string
	if line := FormatDeadlineLine(sv.Settings, lang); line != "" {
		parts = append(parts, line)
	}
	if sv.Settings.TargetCount > 0 {
		parts = append(parts, tr(lang, msgMetaTarget, sv.Settings.TargetCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func IsControlWord(text string) string {
	t := strings.TrimSpace(text)
	switch t {
	case "取消":
		return "cancel"
	case "上一题":
		return "prev"
	case "修改":
		return "modify"
	}
	switch strings.ToLower(t) {
	case "cancel":
		return "cancel"
	case "prev", "back":
		return "prev"
	case "modify":
		return "modify"
	default:
		return ""
	}
}
