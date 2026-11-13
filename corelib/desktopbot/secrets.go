package desktopbot

import (
	"regexp"
	"strings"
)

// SecretMetaPrefix names a message-metadata key that carries one secret
// value from Hub to MaClawSrv ("bot_secret.SSH_PASSWORD"). MaClawSrv removes
// every such key before the message is stored.
const SecretMetaPrefix = "bot_secret."

// SecretSavedMark is what replaces a secret value in the text. It is also how
// an already-redacted text is recognised, so redacting twice keeps the first
// value instead of capturing the mark itself.
const SecretSavedMark = "（已保存为 "

var (
	secretNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	// A label such as 密码 / SSH_PASSWORD / token, then ":", "=", 是 or 为,
	// then the value up to the next space or separator.
	inlineSecretRe = regexp.MustCompile(`(?i)(?:^|[\s,，;；(（])((?:[A-Z][A-Z0-9_]{0,40})?(?:PASSWORD|PASSWD|SECRET|TOKEN)|密码|口令)(\s*[:：=]\s*|\s*(?:是|为)\s*)([^\s,，;；。、）)]+)`)
	// A question about a password is not a password.
	notSecretValueRe = regexp.MustCompile(`^(?:什么|多少|啥|哪个|哪些|几位|多长|对的|错的|正确|错误|空的?|不对|不正确|\*+)[?？。!！]*$|[?？]`)
)

// ValidSecretName is the environment-variable shape a secret is kept under.
func ValidSecretName(name string) bool {
	return secretNameRe.MatchString(strings.TrimSpace(name))
}

// CaptureInlineSecrets removes secret values a person typed into a message
// ("密码：xxx", "SITE_TOKEN=xxx") and returns them by name. The returned text
// never contains a value. Text that was already redacted is left as it is.
func CaptureInlineSecrets(text string) (string, map[string]string) {
	found := map[string]string{}
	redacted := inlineSecretRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := inlineSecretRe.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		value := strings.Trim(parts[3], `"'“”‘’`)
		if value == "" || strings.HasPrefix(parts[3], "（") || strings.HasPrefix(parts[3], "(") || notSecretValueRe.MatchString(value) {
			return match
		}
		name := strings.ToUpper(strings.TrimSpace(parts[1]))
		name = strings.NewReplacer("密码", "", "口令", "").Replace(name)
		name = strings.Trim(name, "_")
		switch name {
		case "", "PASSWORD", "PASSWD", "SECRET", "TOKEN":
			name = "SSH_PASSWORD"
		}
		if !ValidSecretName(name) {
			name = "SSH_PASSWORD"
		}
		found[name] = value
		prefix := match[:strings.Index(match, parts[1])]
		return prefix + name + "=" + SecretSavedMark + name + "，在环境变量里，不要再索要，不要打印）"
	})
	return redacted, found
}
