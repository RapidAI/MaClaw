package survey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	if len(s) != ShortCodeLen {
		return "", fmt.Errorf("short code must be %d characters", ShortCodeLen)
	}
	for _, r := range s {
		if !strings.ContainsRune(CrockfordAlphabet, r) {
			return "", fmt.Errorf("invalid short code character")
		}
	}
	return s, nil
}

func SessionKey(platform, userID string) string {
	return strings.TrimSpace(platform) + ":" + strings.TrimSpace(userID)
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

func ValidateDraftQuestions(qs []Question) error {
	if len(qs) == 0 {
		return fmt.Errorf("at least one question is required")
	}
	for _, q := range qs {
		switch q.Type {
		case "single_choice", "multi_choice":
			if len(q.Options) < 2 {
				return fmt.Errorf("choice question needs at least 2 options")
			}
			for _, o := range q.Options {
				if strings.TrimSpace(o.Label) == "" {
					return fmt.Errorf("choice option label required")
				}
			}
		case "text":
			if q.MaxLength != nil && *q.MaxLength < 0 {
				return fmt.Errorf("text max_length must be >= 0")
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
		default:
			return fmt.Errorf("unsupported question type %q", q.Type)
		}
		if q.Title == "" {
			return fmt.Errorf("question title required")
		}
	}
	return nil
}

// ValidateSettingsIn rejects impossible settings values from clients.
func ValidateSettingsIn(s SettingsIn) error {
	if s.TargetCount < 0 {
		return fmt.Errorf("target_count must be >= 0")
	}
	return nil
}

// ParseChoiceToken maps user input to a single option id.
func ParseChoiceToken(q Question, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("empty answer")
	}
	// 1-based index
	if n, err := strconv.Atoi(token); err == nil {
		if n >= 1 && n <= len(q.Options) {
			return q.Options[n-1].ID, nil
		}
		return "", fmt.Errorf("option index out of range")
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
		return "", fmt.Errorf("ambiguous option")
	}
	return "", fmt.Errorf("unknown option")
}

func ParseMultiChoice(q Question, text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty answer")
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
		return nil, fmt.Errorf("empty answer")
	}
	sort.Strings(ids)
	return ids, nil
}

func ParseRating(q Question, text string) (int, error) {
	text = strings.TrimSpace(text)
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("rating must be an integer")
	}
	min, max := 1, 5
	if q.Min != nil {
		min = *q.Min
	}
	if q.Max != nil {
		max = *q.Max
	}
	if n < min || n > max {
		return 0, fmt.Errorf("rating out of range [%d,%d]", min, max)
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
		return "", fmt.Errorf("required")
	}
	if len([]rune(s)) > maxLen {
		return "", fmt.Errorf("text too long")
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
		return nil, fmt.Errorf("unsupported type")
	}
}

func FormatQuestionPrompt(q Question, index, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【%d/%d】%s", index+1, total, q.Title)
	if q.Required {
		b.WriteString(" *")
	}
	b.WriteString("\n")
	switch q.Type {
	case "single_choice", "multi_choice":
		for i, o := range q.Options {
			fmt.Fprintf(&b, "%d. %s\n", i+1, o.Label)
		}
		if q.Type == "multi_choice" {
			b.WriteString("（多选，用空格或逗号分隔序号）\n")
		} else {
			b.WriteString("（回复选项序号）\n")
		}
	case "rating":
		min, max := 1, 5
		if q.Min != nil {
			min = *q.Min
		}
		if q.Max != nil {
			max = *q.Max
		}
		fmt.Fprintf(&b, "请回复 %d–%d 的整数\n", min, max)
	case "text":
		b.WriteString("请直接输入文字\n")
	}
	b.WriteString("回复「取消」可退出；「上一题」可返回")
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

// FormatDeadlineLine returns a short Chinese line for IM, or empty if no deadline.
func FormatDeadlineLine(s Settings) string {
	if s.Deadline == nil {
		return ""
	}
	// Local-friendly UTC display with Z; desktop UI uses locale separately.
	return "截止：" + s.Deadline.UTC().Format("2006-01-02 15:04 UTC")
}

// SurveyIntroMeta appends optional deadline/target hints under survey title for IM.
func SurveyIntroMeta(sv *Survey) string {
	if sv == nil {
		return ""
	}
	var parts []string
	if line := FormatDeadlineLine(sv.Settings); line != "" {
		parts = append(parts, line)
	}
	if sv.Settings.TargetCount > 0 {
		parts = append(parts, fmt.Sprintf("目标回收：%d 份", sv.Settings.TargetCount))
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
