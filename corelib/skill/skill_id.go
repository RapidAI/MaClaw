package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
)

// ────────────────────────────────────────────────────────────────────────────
// Skill ID format: <publisher>.<skill-name>
//
// Rules:
//   - publisher: 3-64 chars, [a-z0-9][a-z0-9-]{1,62}[a-z0-9]
//   - skill-name: 2-64 chars, [a-z0-9][a-z0-9-]{0,62}[a-z0-9]
//   - Full ID: 6-129 chars (3 + 1 dot + 2 minimum, 64 + 1 + 64 maximum)
//   - Examples: lovstudio.any2pdf, zhangsan-a1b2.paper-translator
//
// The publisher prefix is derived from the uploader's email at first upload
// time. The skill-name is derived from the skill's Name field.
// Once uploaded, the ID is immutable (bound to the uploader's account).
// ────────────────────────────────────────────────────────────────────────────

// skillIDRe validates the full skill ID format.
// Publisher: 3-64 chars. Pattern: [a-z0-9][a-z0-9-]{1,62}[a-z0-9] → min 3 chars.
// Skill-name: 2-64 chars. Pattern: [a-z0-9][a-z0-9-]{0,62}[a-z0-9] → min 2 chars.
var skillIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]\.[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// IsValidSkillID checks if the given string is a valid skill ID
// in the format "publisher.skill-name".
func IsValidSkillID(id string) bool {
	if len(id) < 6 || len(id) > 129 { // min: 3(pub) + 1(dot) + 2(name) = 6; max: 64+1+64 = 129
		return false
	}
	return skillIDRe.MatchString(id)
}

// ParseSkillID splits a skill ID into publisher and name components.
// Returns ("", "", false) for invalid IDs.
func ParseSkillID(id string) (publisher, name string, valid bool) {
	if !IsValidSkillID(id) {
		return "", "", false
	}
	dot := strings.IndexByte(id, '.')
	if dot < 0 {
		return "", "", false
	}
	return id[:dot], id[dot+1:], true
}

// DerivePublisher generates a publisher prefix from an email address.
// Format: <email-prefix>-<4-hex-hash>
// The hash suffix disambiguates same-prefix emails from different domains.
// Example: "zhangsan@gmail.com" → "zhangsan-a1b2"
func DerivePublisher(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}

	at := strings.IndexByte(email, '@')
	var prefix string
	if at > 0 {
		prefix = email[:at]
	} else {
		prefix = email
	}

	// Sanitize: keep only a-z, 0-9, -
	prefix = sanitizePublisherChars(prefix)
	if len(prefix) < 2 {
		prefix = "user"
	}

	// 4-hex suffix from SHA256 of the full email (disambiguates same-prefix different-domain)
	h := sha256.Sum256([]byte(email))
	suffix := hex.EncodeToString(h[:2]) // 4 hex chars = 2 bytes

	publisher := prefix + "-" + suffix
	// Cap at 64 chars
	if len(publisher) > 64 {
		publisher = publisher[:64]
	}
	// Ensure it doesn't end with '-'
	publisher = strings.TrimRight(publisher, "-")
	return publisher
}

// DeriveSkillID generates a complete skill ID from an email and skill name.
// Returns "" if either input is empty.
func DeriveSkillID(email, skillName string) string {
	publisher := DerivePublisher(email)
	if publisher == "" {
		return ""
	}
	name := SanitizeSkillNameForID(skillName)
	if name == "" {
		return ""
	}
	id := publisher + "." + name
	if len(id) > 129 {
		// Truncate name part to fit
		maxName := 129 - len(publisher) - 1
		if maxName < 2 {
			return ""
		}
		name = name[:maxName]
		name = strings.TrimRight(name, "-")
		id = publisher + "." + name
	}
	return id
}

// SanitizeSkillNameForID converts a display name to a valid skill-name component.
// Removes non-ASCII, lowercases, replaces spaces/underscores with hyphens,
// collapses multiple hyphens, trims.
func SanitizeSkillNameForID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	var b strings.Builder
	prevHyphen := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		case unicode.Is(unicode.Han, r):
			// Skip CJK characters — they can't be in ID
			// If the entire name is CJK, we'll fall back below
		default:
			// Skip other chars
		}
	}

	result := strings.TrimRight(b.String(), "-")
	if len(result) < 2 {
		// Name was mostly CJK or too short — use a hash
		h := sha256.Sum256([]byte(name))
		return "skill-" + hex.EncodeToString(h[:4]) // 8 hex chars
	}
	if len(result) > 64 {
		result = result[:64]
		result = strings.TrimRight(result, "-")
	}
	return result
}

// sanitizePublisherChars keeps only [a-z0-9-] from the input, collapsing
// consecutive separators into a single hyphen.
func sanitizePublisherChars(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r == '-', r == '_', r == '.':
			if b.Len() > 0 && !prevHyphen { // don't start with hyphen, don't double
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	result := strings.TrimRight(b.String(), "-")
	return result
}
