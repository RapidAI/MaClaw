// Package botlog writes one append-only file per bot under
// ~/.maclaw/logs/bots/<bot-id>.log (or MACLAW_BOT_LOG_DIR).
//
// The shared maclaw.log is filtered and is often empty. A bot that cannot
// reach MaClawSrv then has no record of the status code or the dial error.
// These files keep the pipeline steps for that bot only: GUI relay, Hub
// desktop open, and the MaClawSrv turn. Tokens, passwords, and handoff
// URLs are redacted.
package botlog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

const (
	repeatWindow = 15 * time.Second
	maxValueLen  = 500
	maxNameLen   = 80
)

var (
	mu         sync.Mutex
	lastRepeat = map[string]time.Time{}

	bearerRE     = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{6,}`)
	skRE         = regexp.MustCompile(`\bsk-[A-Za-z0-9]{8,}\b`)
	jsonSecretRE = regexp.MustCompile(`(?i)"(access_token|api_key|api_secret|password|authorization|admin_secret)"\s*:\s*"[^"]*"`)
	handoffRE    = regexp.MustCompile(`/api/v1/desktop-handoff/[A-Za-z0-9._~-]+`)
	secretKeyRE  = regexp.MustCompile(`(?i)(token|secret|password|authorization|api[_-]?key|credential|bearer)`)
)

// FromMeta returns the bot id carried on a MaClawSrv message.
// An ordinary chat message has none, and nothing is written for it.
func FromMeta(meta map[string]string) string {
	if meta == nil {
		return ""
	}
	return strings.TrimSpace(meta["bot_id"])
}

// Host is the host of a URL. The rest of the URL, including userinfo and
// query tokens, is not returned.
func Host(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	// net/url is avoided here only for the userinfo strip below; parsing
	// keeps a key in the query out of the log.
	u, err := parseHost(raw)
	if err != nil || u == "" {
		return "unparsed"
	}
	return u
}

// Write appends one pipeline line for botID. An empty bot id is ignored.
// fields are key, value pairs. A trailing key without a value is ignored.
func Write(botID, stage string, err error, fields ...string) {
	write(botID, stage, err, false, fields...)
}

// WriteOnce is Write, except the same bot, stage, and error text are kept
// once per repeatWindow. Desktop polls use it so a down hub does not fill
// the file every few hundred milliseconds.
func WriteOnce(botID, stage string, err error, fields ...string) {
	write(botID, stage, err, true, fields...)
}

func write(botID, stage string, err error, once bool, fields ...string) {
	botID = strings.TrimSpace(botID)
	stage = strings.TrimSpace(stage)
	name := fileName(botID)
	if name == "" || stage == "" {
		return
	}
	errText := "none"
	if err != nil {
		errText = redact("", err.Error())
		if errText == "" {
			errText = "unknown"
		}
	}
	if once && repeated(botID, stage, errText) {
		return
	}
	line := formatLine(botID, stage, errText, fields)
	log.Printf("[bot-pipeline] %s", strings.TrimRight(line, "\n"))
	dir := logDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, openErr := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func formatLine(botID, stage, errText string, fields []string) string {
	var b strings.Builder
	b.WriteString(time.Now().Format(time.RFC3339Nano))
	b.WriteString(" stage=")
	b.WriteString(stage)
	b.WriteString(" bot=")
	b.WriteString(botID)
	for i := 0; i+1 < len(fields); i += 2 {
		key := strings.TrimSpace(fields[i])
		if key == "" || strings.ContainsAny(key, " \t=") {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(quote(redact(key, fields[i+1])))
	}
	b.WriteString(" err=")
	b.WriteString(quote(errText))
	b.WriteByte('\n')
	return b.String()
}

func quote(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\"") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func redact(key, value string) string {
	if strings.TrimSpace(value) != "" && secretKeyRE.MatchString(key) {
		return "[redacted]"
	}
	value = bearerRE.ReplaceAllString(value, "Bearer [redacted]")
	value = skRE.ReplaceAllString(value, "sk-[redacted]")
	value = jsonSecretRE.ReplaceAllString(value, `"$1":"[redacted]"`)
	value = handoffRE.ReplaceAllString(value, "/api/v1/desktop-handoff/[redacted]")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > maxValueLen {
		value = value[:maxValueLen] + "…"
	}
	return value
}

func repeated(botID, stage, errText string) bool {
	key := botID + "\x00" + stage + "\x00" + errText
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if len(lastRepeat) > 256 {
		lastRepeat = map[string]time.Time{}
	}
	if prev, ok := lastRepeat[key]; ok && now.Sub(prev) < repeatWindow {
		return true
	}
	lastRepeat[key] = now
	return false
}

func fileName(botID string) string {
	if botID == "" || botID == "." || botID == ".." {
		return ""
	}
	var b strings.Builder
	for _, r := range botID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" || name == "." || name == ".." || strings.Trim(name, ".") == "" {
		return ""
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	return name + ".log"
}

func logDir() string {
	if dir := strings.TrimSpace(os.Getenv("MACLAW_BOT_LOG_DIR")); dir != "" {
		return dir
	}
	if testBinary() {
		return filepath.Join(os.TempDir(), "maclaw-botlog-test")
	}
	return filepath.Join(maclawpath.LogsDir(), "bots")
}

func testBinary() bool {
	name := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.Contains(name, ".test")
}
