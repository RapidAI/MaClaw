package memory

import (
	"errors"
	"strings"
	"unicode"
)

type MemoryGovernanceAction string

const (
	MemoryGovernanceAccept     MemoryGovernanceAction = "accept"
	MemoryGovernanceQuarantine MemoryGovernanceAction = "quarantine"
	MemoryGovernanceReject     MemoryGovernanceAction = "reject"
)

var ErrMemoryCandidateRejected = errors.New("memory candidate rejected")

type MemoryGovernanceDecision struct {
	Action  MemoryGovernanceAction `json:"action"`
	Score   int                    `json:"score"`
	Reasons []string               `json:"reasons,omitempty"`
}

func AssessMemoryCandidate(entry Entry, contextHint string) MemoryGovernanceDecision {
	content := strings.TrimSpace(entry.Content)
	decision := MemoryGovernanceDecision{Action: MemoryGovernanceReject}
	add := func(score int, reason string) {
		decision.Score += score
		if reason != "" {
			decision.Reasons = append(decision.Reasons, reason)
		}
	}
	if content == "" {
		add(-10, "empty content")
		return decision
	}
	runeLen := len([]rune(content))
	if runeLen < 12 {
		add(-3, "too short")
	} else if runeLen >= 40 {
		add(2, "self-contained length")
	}
	if containsExplicitCredentialValue(content) {
		add(-10, "contains explicit credential or temporary secret")
		return decision
	}
	if redactSecretsInMemory(content) != content {
		add(-10, "contains sensitive material after redaction")
		return decision
	}

	canonical := MapToCanonical(entry.Category)
	switch canonical {
	case CategoryPreference, CategoryInstruction, CategoryUserFact:
		add(4, "durable user/profile category")
	case CategoryProjectKnowledge, CategoryTaskArtifact:
		add(4, "durable project/task category")
	case CategorySelfIdentity, CategoryConversationSummary, CategorySessionCheckpoint, CategoryProfile:
		add(2, "structured memory category")
	default:
		add(0, "general category")
	}

	lower := strings.ToLower(content + " " + strings.Join(entry.Tags, " ") + " " + contextHint)
	if containsAny(lower, []string{"prefer", "preference", "always", "never", "habit", "style", "instruction"}) {
		add(3, "preference or standing instruction signal")
	}
	if containsAny(lower, []string{"project", "repo", "module", "api", "endpoint", "config", "version", "database", "docker", "kubernetes", "test command", "build command"}) {
		add(3, "technical/project signal")
	}
	if len(entry.Entities) > 0 || len(entry.Tags) > 0 {
		add(1, "has retrieval anchors")
	}
	if looksLikePathOrCommand(content) {
		add(2, "contains path/command/version-like evidence")
	}
	if containsAny(lower, []string{"i will ", "i'll ", "i am going to", "currently", "just ran", "ran test", "tool call", "current task"}) {
		add(-4, "execution narration or transient task state")
	}
	if containsAny(lower, []string{"hello", "thanks", "thank you", "ok", "okay"}) && runeLen < 30 {
		add(-4, "greeting or acknowledgement")
	}

	switch {
	case decision.Score >= 4:
		decision.Action = MemoryGovernanceAccept
	case decision.Score >= 1:
		decision.Action = MemoryGovernanceQuarantine
	default:
		decision.Action = MemoryGovernanceReject
	}
	return decision
}

func (s *Store) SaveGovernedWithContext(entry Entry, contextHint string) (MemoryGovernanceDecision, error) {
	decision := AssessMemoryCandidate(entry, contextHint)
	switch decision.Action {
	case MemoryGovernanceAccept:
		return decision, s.SaveWithContext(entry, contextHint)
	case MemoryGovernanceQuarantine:
		entry.Status = StatusDormant
		entry.Tags = mergeTags(entry.Tags, []string{memoryCandidateTag})
		if entry.SourceType == "" {
			entry.SourceType = memoryCandidateTag
		}
		return decision, s.SaveWithContext(entry, contextHint)
	default:
		return decision, ErrMemoryCandidateRejected
	}
}

func looksLikePathOrCommand(content string) bool {
	if strings.Contains(content, ":\\") || strings.Contains(content, "/") || strings.Contains(content, "`") {
		return true
	}
	for _, token := range strings.Fields(content) {
		if strings.Contains(token, ".") && len(token) >= 4 {
			return true
		}
		if hasDigitAndLetter(token) {
			return true
		}
	}
	return false
}

func hasDigitAndLetter(s string) bool {
	hasDigit := false
	hasLetter := false
	for _, r := range s {
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	return hasDigit && hasLetter
}

func containsExplicitCredentialValue(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	explicitPatterns := []string{
		"password=", "password:", "password is",
		"passwd=", "passwd:", "passwd is",
		"passcode=", "passcode:", "passcode is",
		"otp=", "otp:", "otp is",
		"token=", "token:", "token is",
		"secret=", "secret:", "secret is",
		"api key=", "api key:", "api key is",
		"密码=", "密码:", "密码：", "密码是",
		"验证码=", "验证码:", "验证码：", "验证码是",
		"口令=", "口令:", "口令：", "口令是",
	}
	for _, pattern := range explicitPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	if strings.Contains(lower, "private key") || strings.Contains(lower, "-----begin") {
		return true
	}
	credentialKeywords := []string{
		"password", "passwd", "passcode", "otp", "verification code", "token",
		"access token", "refresh token", "api key", "secret", "private key",
		"密码", "验证码", "动态口令", "口令",
	}
	expirySignals := []string{
		"valid for", "expires", "expire", "one hour", "1 hour", "temporary",
		"time-limited", "ttl", "临时", "有效期", "一小时", "1小时",
	}
	return containsAny(lower, credentialKeywords) && containsAny(lower, expirySignals)
}
