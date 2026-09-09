package security

import (
	"regexp"
	"sort"
	"strings"
)

const auditRedactedValue = "[REDACTED]"

var (
	// Assignment form: `key=value` and `key: value`.
	//
	// Keywords tolerate one separator character inside them so that
	// `API Key`, `API-Key` and `API_Key` all match. The prefix and suffix
	// deliberately exclude spaces: if they allowed them, `compact-secret API
	// Secret:` would match as one long key and the real secret
	// (`compact-secret`) would survive while only the tail got rewritten.
	auditSensitiveAssignmentRe = regexp.MustCompile(`(?i)([A-Za-z0-9_-]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API[\s_-]?KEY|JWT|AUTHORIZATION|COOKIE|ENCRYPTION[\s_-]?KEY|ACCESS[\s_-]?KEY|REFRESH[\s_-]?KEY|PRIVATE[\s_-]?KEY)[A-Za-z0-9_-]*[ \t]*[:=][ \t]*)(?:'[^']*'|"[^"]*"|[^\s;&|,'"]+)`)
	auditSensitiveJSONFieldRe  = regexp.MustCompile(`(?i)(((?:"|')[A-Z0-9_ -]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API[_-]?KEY|JWT|AUTHORIZATION|COOKIE|ENCRYPTION[_-]?KEY|ACCESS[_-]?KEY|REFRESH[_-]?KEY|PRIVATE[_-]?KEY)[A-Z0-9_ -]*(?:"|')\s*:\s*))(?:'[^']*'|"[^"]*"|[^\s,}\];&|]+)`)
	// Flag form: `--password x`, `-token=abc`, `/password x`.
	//
	// The leading boundary group is load-bearing. Without it the `-?` prefix
	// happily consumes the hyphen belonging to the *previous* word, so
	// `compact-secret API` was read as flag `-secret` with value `API` — the
	// real secret survived and an innocent adjacent word was redacted in its
	// place. Confirmed by TestRedactSensitiveStringDoesNotMisreadHyphenatedWord.
	auditSensitiveFlagRe        = regexp.MustCompile(`(?i)((?:^|[\s"'` + "`" + `;=|,(])--?(?:password|passwd|secret|token|api-key|apikey|jwt|authorization|cookie|access-key|refresh-key|private-key|encryption-key)(?:[ \t]*=[ \t]*|[ \t]+)|(?:^|[\s=])/(?:password|passwd|token)(?:[ \t]*=[ \t]*|[ \t]+))(?:'[^']*'|"[^"]*"|[^\s;&|'"]+)`)
	auditSensitiveHeaderRe      = regexp.MustCompile(`(?i)((?:authorization|cookie)\s*:\s*)(?:'[^']*'|"[^"]*"|[^"'\r\n;&|]+)`)
	auditSensitiveURLUserInfoRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s/@:]+:)[^\s/@]+(@)`)
)

// SanitizeSensitiveValue returns a JSON-safe copy of value with obvious secret
// material redacted. It is intended for persistence and diagnostics, not for
// command execution paths that still need original user-provided values.
func SanitizeSensitiveValue(key string, value interface{}) interface{} {
	return sanitizeAuditValue(key, value)
}

// RedactSensitiveString redacts obvious secret-bearing substrings while keeping
// command/header shape useful for diagnosis.
func RedactSensitiveString(value string) string {
	return redactAuditString(value)
}

// SanitizeAuditEntry returns a copy of entry with secret-bearing argument
// values redacted. It is intentionally applied at the persistence boundary so
// all audit writers keep diagnostic shape without leaking credentials.
func SanitizeAuditEntry(entry AuditEntry) AuditEntry {
	entry.Arguments = sanitizeAuditMap(entry.Arguments)
	entry.OutputSnippet = redactAuditString(entry.OutputSnippet)
	entry.Result = redactAuditString(entry.Result)
	return entry
}

// RedactMetadataMap returns a copy of in (typical Service recordAudit metadata,
// i.e. map[string]string) with secret-bearing values redacted. Use it at every
// audit persistence boundary that does not go through SanitizeAuditEntry.
//
// Fix for P0-3 of the 2026-09-08 review: Service.recordAudit previously passed
// metadata through cloneMap verbatim, leaking password / token / api_key values
// into agentservice_state.json. This helper makes the Service path match the
// AuditLog.Log path.
//
// Secret-named keys are replaced outright; every other value is scanned for
// embedded `key=value` / `key: value` / flag-shaped secrets.
//
// History worth keeping: an earlier version of this function passed non-secret
// values through untouched because redactAuditString was lossy — it replaced
// only the keyword (`API Key = display-key` became `[REDACTED] Key =
// display-key`) and misread `compact-secret API` as a `-secret` flag, so
// running it here corrupted the text into a shape the stronger export-time
// redactor no longer recognised and leaked *more* than doing nothing. Those
// regex defects are fixed (see auditSensitiveFlagRe);
// TestRedactSensitiveStringIsIdempotent guards the chaining property this
// depends on. Do not reintroduce a partial redactor here.
func RedactMetadataMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if isSensitiveAuditKey(key) {
			out[key] = auditRedactedValue
			continue
		}
		// Safe to scan here now that redactAuditString is complete and
		// idempotent (see TestRedactSensitiveStringIsIdempotent). It previously
		// only replaced the keyword and left the secret in place, which made
		// chained redaction leak more than doing nothing.
		out[key] = redactAuditString(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeAuditMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = sanitizeAuditValue(k, v)
	}
	return out
}

func sanitizeAuditValue(key string, value interface{}) interface{} {
	if isSensitiveAuditKey(key) {
		return auditRedactedValue
	}
	switch v := value.(type) {
	case map[string]interface{}:
		return sanitizeAuditMap(v)
	case map[string]string:
		out := make(map[string]string, len(v))
		for kk, vv := range v {
			if isSensitiveAuditKey(kk) {
				out[kk] = auditRedactedValue
			} else {
				out[kk] = redactAuditString(vv)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = sanitizeAuditValue("", item)
		}
		return out
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = redactAuditString(item)
		}
		return out
	case string:
		return redactAuditString(v)
	default:
		return v
	}
}

func auditValueWouldRedactAtKey(key string, value interface{}) bool {
	if isSensitiveAuditKey(key) {
		return true
	}
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if auditValueWouldRedactAtKey("", item) {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if redactAuditString(item) != item {
				return true
			}
		}
	case string:
		return redactAuditString(v) != v
	}
	return false
}

func isSensitiveAuditKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"), " ", "_"))
	if normalized == "" {
		return false
	}
	fragments := []string{
		"password", "passwd", "pwd", "secret", "token", "api_key", "apikey", "jwt",
		"authorization", "cookie", "private_key", "access_key", "refresh_key", "encryption_key",
	}
	for _, fragment := range fragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func redactAuditString(value string) string {
	if value == "" {
		return value
	}
	redacted := auditSensitiveAssignmentRe.ReplaceAllString(value, `${1}`+auditRedactedValue)
	redacted = auditSensitiveJSONFieldRe.ReplaceAllString(redacted, `${1}"`+auditRedactedValue+`"`)
	redacted = auditSensitiveFlagRe.ReplaceAllString(redacted, `${1}`+auditRedactedValue)
	redacted = auditSensitiveHeaderRe.ReplaceAllString(redacted, `${1}`+auditRedactedValue)
	redacted = auditSensitiveURLUserInfoRe.ReplaceAllString(redacted, `${1}`+auditRedactedValue+`${2}`)
	return redacted
}

func RedactedAuditCategories(entry AuditEntry) []string {
	categories := map[string]struct{}{}
	collectSensitiveAuditKeys(categories, entry.Arguments)
	if redactAuditString(entry.Result) != entry.Result {
		categories["result"] = struct{}{}
	}
	if redactAuditString(entry.OutputSnippet) != entry.OutputSnippet {
		categories["output_snippet"] = struct{}{}
	}
	out := make([]string, 0, len(categories))
	for category := range categories {
		out = append(out, category)
	}
	sort.Strings(out)
	return out
}

func collectSensitiveAuditKeys(out map[string]struct{}, value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		for k, vv := range v {
			if auditValueWouldRedactAtKey(k, vv) {
				out[k] = struct{}{}
			}
			collectSensitiveAuditKeys(out, vv)
		}
	case map[string]string:
		for k, vv := range v {
			if isSensitiveAuditKey(k) || redactAuditString(vv) != vv {
				out[k] = struct{}{}
			}
		}
	case []interface{}:
		for _, item := range v {
			collectSensitiveAuditKeys(out, item)
		}
	case []string:
		for _, item := range v {
			if redactAuditString(item) != item {
				out["arguments"] = struct{}{}
			}
		}
	}
}
