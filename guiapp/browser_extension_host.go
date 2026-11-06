package guiapp

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const companionPaperHostArg = "companion-paper-host"

func isCompanionPaperHostArgv(args []string) bool {
	if len(args) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(args[0]))
	if base == "maclaw-companion-host" || base == "maclaw-companion-host.exe" {
		return true
	}
	for _, arg := range args[1:] {
		if arg == companionPaperHostArg || strings.HasPrefix(arg, "chrome-extension://") {
			return true
		}
	}
	return false
}

// runCompanionPaperHost reads one native-messaging frame and writes one reply.
// stdout is only that frame. The process exits before the window is created.
func runCompanionPaperHost(args []string) int {
	_ = args
	return runCompanionPaperHostIO(os.Stdin, os.Stdout)
}

func runCompanionPaperHostIO(in io.Reader, out io.Writer) (code int) {
	defer func() {
		if recover() != nil {
			_ = writeNativeMessage(out, map[string]any{"ok": false, "code": companionPaperCodeSaveFailed})
			code = 1
		}
	}()
	msg, err := readNativeMessage(in)
	if err != nil {
		_ = writeNativeMessage(out, map[string]any{"ok": false, "code": companionPaperCodeSaveFailed})
		return 1
	}
	reply := handleCompanionPaperFrame(msg)
	if err := writeNativeMessage(out, reply); err != nil {
		return 1
	}
	return 0
}

func handleCompanionPaperFrame(msg map[string]any) map[string]any {
	if msg == nil {
		return map[string]any{"ok": false, "code": companionPaperCodeSaveFailed}
	}
	if hello, _ := msg["hello"].(bool); hello {
		return handleCompanionPaperHello(msg)
	}
	raw, _ := msg["url"].(string)
	if title, ok := msg["title"].(string); ok {
		// The page title is not a filename, a library field, or a log line.
		_ = sanitizeCompanionPaperTitle(title)
	}
	if code := acceptCompanionPaper(raw); code != "" {
		return map[string]any{"ok": false, "code": code}
	}
	return map[string]any{"ok": true}
}

func handleCompanionPaperHello(msg map[string]any) map[string]any {
	if _, hasURL := msg["url"]; hasURL {
		return map[string]any{"ok": false, "code": companionPaperCodeSaveFailed}
	}
	version, _ := msg["version"].(string)
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 32 {
		return map[string]any{"ok": false, "code": companionPaperCodeSaveFailed}
	}
	browser, _ := msg["browser"].(string)
	switch browser {
	case "safari":
		return map[string]any{"ok": true}
	case "chrome", "edge":
		if err := recordBrowserExtensionHello(browser, version); err != nil {
			return map[string]any{"ok": false, "code": companionPaperCodeSaveFailed}
		}
		return map[string]any{"ok": true}
	default:
		return map[string]any{"ok": false, "code": companionPaperCodeSaveFailed}
	}
}

// sanitizeCompanionPaperTitle drops control characters and keeps at most 200
// Unicode scalars. Callers discard the result.
func sanitizeCompanionPaperTitle(raw string) string {
	var b strings.Builder
	n := 0
	for _, r := range raw {
		if unicode.IsControl(r) {
			continue
		}
		if n >= 200 {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func readNativeMessage(r io.Reader) (map[string]any, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n == 0 || n > 1024*1024 {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(buf, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func writeNativeMessage(w io.Writer, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(body))); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
