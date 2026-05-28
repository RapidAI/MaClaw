package tool

import (
	"regexp"
	"strings"
)

var rawSSHCommandPattern = regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*(?:(?:sudo|command|exec|nohup|setsid)\s+|env(?:\s+(?:-[^\s;&|()]+|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|()]+))*\s+|timeout\s+[^\s;&|()]+\s+|stdbuf(?:\s+[^\s;&|()]+)+\s+)*(?:\./|[\w]:[\\/][^\s;&|()]+[\\/])?(?:ssh|ssh\.exe|scp|scp\.exe|sftp|sftp\.exe)(?:\s|$)`)
var rawRsyncCommandPattern = regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*(?:(?:sudo|command|exec|nohup|setsid)\s+|env(?:\s+(?:-[^\s;&|()]+|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|()]+))*\s+|timeout\s+[^\s;&|()]+\s+|stdbuf(?:\s+[^\s;&|()]+)+\s+)*(?:\./|[\w]:[\\/][^\s;&|()]+[\\/])?(?:rsync|rsync\.exe)(?:\s|$)`)

const rawSSHCommandRejection = "[system rejected] Raw ssh/scp/sftp/remote-rsync command execution through bash is disabled for SSH/server operations. Use the builtin ssh tool directly so MaClaw can manage sessions, credentials, timeouts, and process cleanup."

// RejectRawSSHCommand rejects shell commands that try to bypass the builtin ssh tool.
func RejectRawSSHCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if hasRawRemoteCommand(command) || hasNestedRawSSHCommand(command) {
		return rawSSHCommandRejection, true
	}
	return "", false
}

func hasRawRemoteCommand(command string) bool {
	return rawSSHCommandPattern.MatchString(command) || hasRawRemoteRsyncCommand(command)
}

func hasNestedRawSSHCommand(command string) bool {
	tokens := shellLikeFields(command)
	for i, tok := range tokens {
		shell := shellLauncherName(tok)
		if shell == "" {
			continue
		}
		if nested := nestedShellCommand(tokens[i+1:], shell); nested != "" {
			if hasRawRemoteCommand(nested) || hasRawRemoteCommand("; "+nested) {
				return true
			}
		}
	}
	return false
}

func hasRawRemoteRsyncCommand(command string) bool {
	for _, loc := range rawRsyncCommandPattern.FindAllStringIndex(command, -1) {
		segment := command[loc[0]:]
		if idx := strings.IndexAny(segment[1:], ";&|()\r\n"); idx >= 0 {
			segment = segment[:idx+1]
		}
		if rsyncSegmentHasRemoteOperand(shellLikeFields(segment)) {
			return true
		}
	}
	return false
}

func rsyncSegmentHasRemoteOperand(tokens []string) bool {
	seenRsync := false
	for _, tok := range tokens {
		if !seenRsync {
			name := strings.ToLower(strings.TrimSuffix(commandBaseName(strings.TrimSpace(tok)), ".exe"))
			if name == "rsync" {
				seenRsync = true
			}
			continue
		}
		if isRsyncRemoteOperand(tok) {
			return true
		}
	}
	return false
}

func isRsyncRemoteOperand(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.HasPrefix(token, "-") {
		return false
	}
	if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return false
	}
	if strings.Contains(token, "::") {
		return true
	}
	colon := strings.IndexByte(token, ':')
	if colon <= 0 {
		return false
	}
	if colon == 1 && ((token[0] >= 'A' && token[0] <= 'Z') || (token[0] >= 'a' && token[0] <= 'z')) {
		return false
	}
	return true
}

func nestedShellCommand(tokens []string, shell string) string {
	for i := 0; i < len(tokens); i++ {
		tok := strings.ToLower(strings.TrimSpace(tokens[i]))
		if tok == "" {
			continue
		}
		switch shell {
		case "cmd":
			if tok == "/c" || tok == "-c" {
				return strings.Join(tokens[i+1:], " ")
			}
		case "powershell", "pwsh":
			if tok == "-command" || tok == "-c" || tok == "/c" {
				return strings.Join(tokens[i+1:], " ")
			}
		case "bash", "sh", "zsh":
			trimmed := strings.TrimLeft(tok, "-")
			if strings.Contains(trimmed, "c") {
				return strings.Join(tokens[i+1:], " ")
			}
		}
	}
	return ""
}

func shellLauncherName(token string) string {
	name := strings.ToLower(strings.TrimSuffix(commandBaseName(strings.TrimSpace(token)), ".exe"))
	switch name {
	case "bash", "sh", "zsh", "powershell", "pwsh", "cmd":
		return name
	default:
		return ""
	}
}

func shellLikeFields(command string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	flush := func() {
		if b.Len() == 0 {
			return
		}
		fields = append(fields, b.String())
		b.Reset()
	}
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
		case ' ', '\t', '\r', '\n':
			flush()
		case ';', '&', '|', '(', ')':
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return fields
}

func commandBaseName(path string) string {
	path = strings.TrimSpace(path)
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
