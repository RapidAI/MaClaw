package guiapp

import (
	"path/filepath"
	"strings"
)

// remoteAttachmentMarker starts the line a remote controller writes before
// the absolute paths of the files it attached. The phone's messageWithAttachments
// emits "附件：/附件（N 个）：" followed by "- <path>" lines.
const remoteAttachmentMarker = "附件"

// remoteAttachmentTurnPrefix heads the instruction the desktop agent reads.
// It is not shown to the person: the user row keeps the paths.
const remoteAttachmentTurnPrefix = "【远控附件】对方通过远控随消息附带了下面的文件，已保存到本任务目录，可直接用绝对路径读取（不要用相对路径）:"

// extractRemoteAttachmentPaths pulls the attachment block out of one remote
// message. A path is accepted only when it lives inside this task's
// directory (the only place a remote upload writes) and is a single
// attachments/*/<file> entry; anything else stays in the text as typed.
//
// It returns the message without the attachment block, and the paths.
func extractRemoteAttachmentPaths(taskDir, text string) (string, []string) {
	text = strings.TrimSpace(text)
	if text == "" || !strings.Contains(text, remoteAttachmentMarker) {
		return text, nil
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	var paths []string
	inBlock := false
	sawBlock := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if !inBlock && isRemoteAttachmentHeader(line) {
			inBlock = true
			sawBlock = true
			continue
		}
		if inBlock {
			if path, ok := remoteAttachmentPathLine(taskDir, line); ok {
				paths = append(paths, path)
				continue
			}
			// First line that is not an attachment entry ends the block; it
			// still belongs to the message.
			inBlock = false
		}
		kept = append(kept, raw)
	}
	if !sawBlock || len(paths) == 0 {
		return text, nil
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), paths
}

// isRemoteAttachmentHeader matches "附件：", "附件:" and the counted form
// "附件（3 个）：" or "附件(3 个):". A bare "附件" is prose, not a header.
func isRemoteAttachmentHeader(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, remoteAttachmentMarker) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, remoteAttachmentMarker))
	if strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "：") {
		return true
	}
	if !strings.HasPrefix(rest, "（") && !strings.HasPrefix(rest, "(") {
		return false
	}
	if !strings.Contains(rest, "个") {
		return false
	}
	return strings.Contains(rest, "）：") || strings.Contains(rest, "）:") ||
		strings.Contains(rest, "):") || strings.Contains(rest, ")：")
}

func remoteAttachmentPathLine(taskDir, line string) (string, bool) {
	if !strings.HasPrefix(line, "- ") {
		return "", false
	}
	candidate := strings.TrimSpace(strings.TrimPrefix(line, "- "))
	if candidate == "" || !filepath.IsAbs(candidate) {
		return "", false
	}
	taskDir = filepath.Clean(taskDir)
	if taskDir == "" || taskDir == "." {
		return "", false
	}
	clean := filepath.Clean(candidate)
	if !pathWithinDir(filepath.Join(taskDir, "attachments"), clean) {
		return "", false
	}
	return clean, true
}

// remoteAttachmentTurnPrompt is the text the agent actually receives: what
// the person typed, then the exact files.
func remoteAttachmentTurnPrompt(paths []string, personText string) string {
	var b strings.Builder
	b.WriteString(remoteAttachmentTurnPrefix)
	b.WriteString("\n")
	for _, p := range paths {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	if personText = strings.TrimSpace(personText); personText != "" {
		b.WriteString("\n对方的话：\n")
		b.WriteString(personText)
	}
	return strings.TrimSpace(b.String())
}

// remoteControlDisplayWithAttachments is the user row the controlled
// desktop shows: the person's text plus the attachment paths, never the
// instruction prefix. An attachment-only message still gets a row, so the
// person recognises what was sent.
func remoteControlDisplayWithAttachments(turnText string, paths []string) string {
	if len(paths) == 0 {
		return turnText
	}
	var b strings.Builder
	body := strings.TrimSpace(strings.TrimPrefix(turnText, remoteAttachmentTurnPrefix))
	if idx := strings.Index(body, "\n对方的话：\n"); idx >= 0 {
		body = strings.TrimSpace(body[idx+len("\n对方的话：\n"):])
	} else {
		// An attachment-only turn has no "对方的话：" section: the body left
		// after the prefix is just the "- <path>" lines, which the attachment
		// list below already shows. Drop them so the row does not repeat.
		var kept []string
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "- ") {
				continue
			}
			kept = append(kept, line)
		}
		body = strings.TrimSpace(strings.Join(kept, "\n"))
	}
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString(remoteAttachmentMarker)
	b.WriteString("（")
	b.WriteString(itoaFast(len(paths)))
	b.WriteString(" 个）：\n")
	for _, p := range paths {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func itoaFast(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
