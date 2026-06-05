package tool

import (
	"regexp"
	"strings"
)

var rawSSHCommandPattern = regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*(?:(?:sudo|command|exec|nohup|setsid)\s+|env(?:\s+(?:-[^\s;&|()]+|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|()]+))*\s+|timeout\s+[^\s;&|()]+\s+|stdbuf(?:\s+[^\s;&|()]+)+\s+)*(?:\./|[\w]:[\\/][^\s;&|()]+[\\/])?(?:ssh|ssh\.exe|scp|scp\.exe|sftp|sftp\.exe)(?:\s|$)`)
var rawRsyncCommandPattern = regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*(?:(?:sudo|command|exec|nohup|setsid)\s+|env(?:\s+(?:-[^\s;&|()]+|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|()]+))*\s+|timeout\s+[^\s;&|()]+\s+|stdbuf(?:\s+[^\s;&|()]+)+\s+)*(?:\./|[\w]:[\\/][^\s;&|()]+[\\/])?(?:rsync|rsync\.exe)(?:\s|$)`)
var broadBrowserKillPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*taskkill(?:\.exe)?\b[^\r\n;&|]*/im\s+(?:chrome|chromium|msedge)(?:\.exe)?\b`),
	regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*wmic\b[^\r\n;&|]*\bprocess\b[^\r\n;&|]*(?:name\s*=\s*['"]?(?:chrome|chromium|msedge)(?:\.exe)?['"]?)[^\r\n;&|]*\bdelete\b`),
	regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*get-process\s+(?:chrome|chromium|msedge)\b[^\r\n;&|]*\|\s*stop-process\b`),
	regexp.MustCompile(`(?i)(^|[;&|()\r\n])\s*(?:get-process\s+(?:chrome|chromium|msedge)\b[^\r\n;&|]*\|\s*)?stop-process\b[^\r\n;&|]*(?:\b-name\s+(?:chrome|chromium|msedge)\b|\b(?:chrome|chromium|msedge)\b|\$_)`),
}
var browserHTTPClientPattern = regexp.MustCompile(`(?is)(^|[;&|()\r\n])\s*(?:curl|curl\.exe|wget|wget\.exe|Invoke-WebRequest|iwr|Invoke-RestMethod|irm)\b`)
var httpURLPattern = regexp.MustCompile(`(?is)https?://`)
var nonIdempotentHTTPPattern = regexp.MustCompile(`(?is)(?:-X\s*['"]?(?:POST|PUT|PATCH|DELETE)|--request(?:\s+|=)['"]?(?:POST|PUT|PATCH|DELETE)|\s-d\s|--data(?:\b|[-=])|--json\b|\b-Method\s*['"]?(?:POST|PUT|PATCH|DELETE)|\bMethod\s*[:=]?\s*['"]?(?:POST|PUT|PATCH|DELETE))`)
var browserAuthHTTPPattern = regexp.MustCompile(`(?is)(?:-H\s*['"]?(?:cookie|authorization|x-csrf|x-xsrf)|--header(?:\s+|=)['"]?(?:cookie|authorization|x-csrf|x-xsrf)|--cookie\b|\s-b\s|\b(?:cookie|authorization|x-csrf|x-xsrf|x-csrftoken|x-xsrftoken)\b\s*[:=]|_xsrf|xsrf|csrf|SESSIONID|Bearer\s|z_c0)`)
var shellBrowserAutomationCommandPattern = regexp.MustCompile(`(?is)(^|[;&|()\r\n])\s*(?:(?:sudo|command|exec|nohup|setsid)\s+|env(?:\s+(?:-[^\s;&|()]+|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|()]+))*\s+|timeout\s+[^\s;&|()]+\s+|stdbuf(?:\s+[^\s;&|()]+)+\s+)*(?:(?:npx|pnpm|yarn|bunx)\s+|(?:python|python3|py)\s+-m\s+)?(?:playwright|puppeteer|selenium|pyppeteer)(?:\s|$)`)
var shellBrowserAutomationTextPattern = regexp.MustCompile(`(?is)(connect_over_cdp|remote-debugging-port|chromium\.launch|firefox\.launch|webkit\.launch|async_playwright|sync_playwright|from\s+playwright|require\(['"]playwright|require\(['"]puppeteer|from\s+selenium|import\s+selenium|webdriver\.chrome|\.new_page\(\)|\.newpage\(\)|page\.screenshot|\.screenshot\(|browser\.close\(\)|127\.0\.0\.1:3888|--screenshot\b|run-playwright)`)

const rawSSHCommandRejection = "[system rejected] Raw ssh/scp/sftp/remote-rsync command execution through bash is disabled for SSH/server operations. Use the builtin ssh tool directly so MaClaw can manage sessions, credentials, timeouts, and process cleanup."
const broadBrowserKillRejection = "[system rejected] Broad Chrome/Edge process kill through bash is disabled during browser automation. Stop the browser session handle only; persistent browser process and login/cookies are preserved."
const browserSideEffectHTTPRejection = "[system rejected] Direct authenticated browser-side HTTP side effects through bash are disabled. Use the browser tool with the logged-in page only, then verify once before retrying."
const shellBrowserAutomationRejection = "[system rejected] Shell Playwright/Puppeteer/Selenium/CDP/screenshot browser automation is disabled. Use the stable browser tool/session mechanism so one managed profile preserves login/cookies and avoids duplicate tabs/processes."

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

// RejectBroadBrowserKillCommand rejects shell commands that kill every local
// browser process. Browser automation owns a managed profile, so cleanup must
// be scoped to that profile/session instead of terminating the user's Chrome.
func RejectBroadBrowserKillCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if hasBroadBrowserKillCommand(command) || hasNestedBroadBrowserKillCommand(command) {
		return broadBrowserKillRejection, true
	}
	return "", false
}

// RejectBrowserSideEffectHTTPCommand blocks curl/PowerShell HTTP calls that
// combine a non-idempotent method with browser-auth credentials. These calls
// bypass page state and browser verification; retries can duplicate publishes,
// posts, payments, or other user-visible side effects.
func RejectBrowserSideEffectHTTPCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if hasBrowserSideEffectHTTPCommand(command) || hasNestedBrowserSideEffectHTTPCommand(command) {
		return browserSideEffectHTTPRejection, true
	}
	return "", false
}

// RejectShellBrowserAutomationCommand blocks shell-driven browser automation
// stacks that create a second browser control plane. Stable browser work must
// use the managed browser tool/session so tabs, cookies, retries, and publish
// verification share one state model.
func RejectShellBrowserAutomationCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if hasShellBrowserAutomationCommand(command) || hasNestedShellBrowserAutomationCommand(command) {
		return shellBrowserAutomationRejection, true
	}
	return "", false
}

func hasShellBrowserAutomationCommand(command string) bool {
	if shellBrowserAutomationCommandPattern.MatchString(command) {
		return true
	}
	if !shellBrowserAutomationTextPattern.MatchString(command) {
		return false
	}
	return shellBrowserAutomationMarkerIsInExecutableContext(command)
}

func shellBrowserAutomationMarkerIsInExecutableContext(command string) bool {
	for _, tok := range shellLikeFields(command) {
		name := strings.ToLower(strings.TrimSuffix(commandBaseName(strings.TrimSpace(tok)), ".exe"))
		switch name {
		case "python", "python3", "py", "node", "deno", "bun", "powershell", "pwsh", "cmd", "bash", "sh", "zsh",
			"chrome", "chromium", "msedge", "google-chrome", "google-chrome-stable", "run-playwright":
			return true
		}
		if strings.Contains(name, "run-playwright") {
			return true
		}
	}
	return false
}

func hasNestedShellBrowserAutomationCommand(command string) bool {
	tokens := shellLikeFields(command)
	for i, tok := range tokens {
		shell := shellLauncherName(tok)
		if shell == "" {
			continue
		}
		if nested := nestedShellCommand(tokens[i+1:], shell); nested != "" {
			if hasShellBrowserAutomationCommand(nested) || hasShellBrowserAutomationCommand("; "+nested) {
				return true
			}
		}
	}
	return false
}

func hasBrowserSideEffectHTTPCommand(command string) bool {
	return browserHTTPClientPattern.MatchString(command) &&
		httpURLPattern.MatchString(command) &&
		nonIdempotentHTTPPattern.MatchString(command) &&
		browserAuthHTTPPattern.MatchString(command)
}

func hasNestedBrowserSideEffectHTTPCommand(command string) bool {
	tokens := shellLikeFields(command)
	for i, tok := range tokens {
		shell := shellLauncherName(tok)
		if shell == "" {
			continue
		}
		if nested := nestedShellCommand(tokens[i+1:], shell); nested != "" {
			if hasBrowserSideEffectHTTPCommand(nested) || hasBrowserSideEffectHTTPCommand("; "+nested) {
				return true
			}
		}
	}
	return false
}

func hasBroadBrowserKillCommand(command string) bool {
	for _, pattern := range broadBrowserKillPatterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}

func hasNestedBroadBrowserKillCommand(command string) bool {
	tokens := shellLikeFields(command)
	for i, tok := range tokens {
		shell := shellLauncherName(tok)
		if shell == "" {
			continue
		}
		if nested := nestedShellCommand(tokens[i+1:], shell); nested != "" {
			if hasBroadBrowserKillCommand(nested) || hasBroadBrowserKillCommand("; "+nested) {
				return true
			}
		}
	}
	return false
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
