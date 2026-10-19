package desktopbot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// Sentences the model and the operator can act on. Internal codes such as
// not_password_field stay at the browser boundary and are translated only
// when a result is about to be shown to the model.

const (
	ToolResultSpillRunes = 4000
	FileWriteMaxBytes    = 200 << 10

	PlanPhaseNextStep      = "这一阶段只核对。等这个人确认后再调用。不要在这一轮用别的动作代替"
	PasswordFieldNextStep  = "这是密码框。不要输入，也不要改用像素。向这个人要秘密，提交后由 runtime 填入"
	DesktopActionInvalid   = "不认识这个 desktop 动作。用工具说明里列出的动作名再调用一次。不要改走 bash"
	FileWriteNeedsContent  = "file_write 缺少 content。带上 content 再调用一次"
	FileTooLarge           = "文件超过 200 KiB。缩小内容后再 file_write。不要改走 bash 分段写"
	FileEditNeedsOld       = "file_edit 缺少 old_string。先 file_read 这一路径，把要替换的原文放进 old_string 再调用"
	DesktopNeedsUser       = "这次调用没有用户。停手并说明桌面对不上人。重试同一调用不会修好"
	DesktopEmptyResult     = "动作没有返回。再调用一次同一个动作。不要改走像素"
	AdvertiseHostNextStep  = "没有对外地址。在 .env 里设置 DESKTOPD_ADVERTISE_HOST 并重启 desktopd，然后再 Ensure"
	DisplayInvalidNextStep = "没有可用的屏幕号。先做一次会打开会话的动作（截图、probe、app_open），再用它返回的屏幕号重试"
)

// MissingIdentityNextStep names which of bot, tenant, or user is absent.
func MissingIdentityNextStep(which string) string {
	switch which {
	case "bot", "tenant", "user":
	default:
		which = "user"
	}
	return "缺的是 " + which + "。补上那个字段再调用。用同一份请求体重试不会修好"
}

// ModelFacingError translates an internal refusal code at the tool boundary.
// Callers that still match the code, including secret fill, keep the original error.
func ModelFacingError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if msg == "not_password_field" || strings.HasSuffix(msg, ": not_password_field") {
		return fmt.Errorf("%s", PasswordFieldNextStep)
	}
	return err
}

var (
	bearerScrub = regexp.MustCompile(`(?i)bearer\s+\S+`)
	skScrub     = regexp.MustCompile(`sk-[A-Za-z0-9]{8,}`)
	tokenScrub  = regexp.MustCompile(`(?i)(token|password|secret|api[_-]?key)\s*[=:]\s*\S+`)
	hexScrub    = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
)

// ScrubSecrets removes bearer tokens, sk- keys, token assignments, and long hex
// from a line that will be shown. It does not print the removed value.
func ScrubSecrets(s string) string {
	s = bearerScrub.ReplaceAllString(s, "Bearer <redacted>")
	s = skScrub.ReplaceAllString(s, "sk-<redacted>")
	s = tokenScrub.ReplaceAllString(s, "$1=<redacted>")
	s = hexScrub.ReplaceAllString(s, "<redacted>")
	return s
}

// SanitizeTaskID keeps a task id inside a container path. Colons and other
// marks become underscores. ContainerPath rejects a colon.
func SanitizeTaskID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "task"
	}
	if len(out) > 80 {
		out = out[:80]
		out = strings.Trim(out, "._")
		if out == "" {
			return "task"
		}
	}
	return out
}

// ToolResultPath is the path the conversation keeps. It is the container path
// even when the bytes were written under a different host directory.
func ToolResultPath(taskID string, n int) string {
	if n < 1 {
		n = 1
	}
	return "/home/desktop/tasks/" + SanitizeTaskID(taskID) + "/out/" + fmt.Sprintf("%d.txt", n)
}

// ToolResultShown is the container path plus one short line of the body.
func ToolResultShown(path, body string) string {
	line := strings.TrimSpace(body)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	line = ScrubSecrets(line)
	line = strings.Join(strings.Fields(line), " ")
	if utf8.RuneCountInString(line) > 120 {
		line = string([]rune(line)[:120])
	}
	if strings.TrimSpace(line) == "" {
		line = "结果已写入文件。"
	}
	return path + "\n" + line
}

// SpillToolResult writes a long tool result under root and returns the
// canonical container path plus one line. A short body is returned unchanged
// and nothing is written. The caller keeps the original body when err != nil.
func SpillToolResult(root, taskID string, seq int, body string) (string, error) {
	if utf8.RuneCountInString(body) <= ToolResultSpillRunes {
		return body, nil
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("tool result spill root is empty")
	}
	if seq < 1 {
		seq = 1
	}
	id := SanitizeTaskID(taskID)
	dir := filepath.Join(root, "tasks", id, "out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	payload := body
	if len(payload) > FileWriteMaxBytes {
		payload = truncateUTF8Bytes(payload, FileWriteMaxBytes)
	}
	full := filepath.Join(dir, fmt.Sprintf("%d.txt", seq))
	if err := os.WriteFile(full, []byte(payload), 0o644); err != nil {
		return "", err
	}
	return ToolResultShown(ToolResultPath(id, seq), body), nil
}

func truncateUTF8Bytes(s string, max int) string {
	if len(s) <= max || max <= 0 {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

var (
	spillRootValue atomic.Value
	spillSeqs      sync.Map
)

// SetSpillRoot points tool-result spill at a host directory. An empty root
// clears the override. Production uses /home/desktop when that directory exists.
func SetSpillRoot(root string) {
	spillRootValue.Store(strings.TrimSpace(root))
}

// SpillRoot is the test or host override. Empty means the caller decides.
func SpillRoot() string {
	root, _ := spillRootValue.Load().(string)
	return root
}

// NextSpillSeq is the next file number for one conversation. It survives
// later turns in this process.
func NextSpillSeq(botID, conversationID string) int {
	key := botID + "\x00" + conversationID
	value, _ := spillSeqs.LoadOrStore(key, &atomic.Int64{})
	n := value.(*atomic.Int64).Add(1)
	if n < 1 {
		return 1
	}
	return int(n)
}

// ResetToolSessionState drops process-local spill counters, the spill root,
// and connector gates. Tests call it so cases do not share a conversation.
func ResetToolSessionState() {
	spillRootValue.Store("")
	spillSeqs = sync.Map{}
	resetConnectorGates()
}
