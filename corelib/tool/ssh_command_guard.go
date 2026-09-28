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
var httpURLPattern = regexp.MustCompile(`(?is)https?://`)
var nonIdempotentHTTPPattern = regexp.MustCompile(`(?is)(?:-X\s*['"]?(?:POST|PUT|PATCH|DELETE)|--request(?:\s+|=)['"]?(?:POST|PUT|PATCH|DELETE)|--method(?:=|\s+)['"]?(?:POST|PUT|PATCH|DELETE)|\s-d(?:\s|['"@=])|--data(?:\b|[-=])|--json\b|--post-data\b|--post-file\b|\b-Method\s*['"]?(?:POST|PUT|PATCH|DELETE)|\bMethod\s*[:=]?\s*['"]?(?:POST|PUT|PATCH|DELETE))`)

// A header or hashtable value that is itself a browser session credential.
// Anchored to the token so a JSON body can mention cookie or csrf without
// looking like a Cookie / X-CSRF header. Authorization is not in this set.
var browserSessionHeaderName = regexp.MustCompile(`(?i)^(?:cookie|x-csrf[a-z0-9-]*|x-xsrf[a-z0-9-]*|_xsrf|z_c0)$`)
var browserSessionHeaderToken = regexp.MustCompile(`(?i)^(?:cookie\s*[:=]|x-csrf[a-z0-9-]*\s*[:=]|x-xsrf[a-z0-9-]*\s*[:=]|_xsrf\s*[:=]|z_c0\s*[:=])`)
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
// replay a logged-in browser session (cookie, CSRF/XSRF, site session ids)
// with a non-idempotent method. These calls bypass page state and browser
// verification; retries can duplicate publishes, posts, payments, or other
// user-visible side effects. Authorization and Bearer headers are not session
// replay: they are how a user-supplied API key calls an HTTP API.
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
	if !httpURLPattern.MatchString(command) {
		return false
	}
	// Method and session credential have to be on the same client command.
	// A cookie GET, or ffmpeg -b after &&, must not decide the API POST.
	for _, raw := range splitShellCommandSegments(command) {
		toks := shellLikeFields(raw)
		client := segmentHTTPClient(toks)
		if client == "" || !segmentMutates(raw, client, toks) {
			continue
		}
		for i, tok := range toks {
			if tokenReplaysBrowserSession(tok, client) || spacedSessionHeader(toks, i) {
				return true
			}
		}
	}
	return false
}

func segmentMutates(raw, client string, toks []string) bool {
	if nonIdempotentHTTPPattern.MatchString(raw) {
		return true
	}
	if client != "curl" {
		return false
	}
	for _, tok := range toks {
		if curlTokenMutates(tok) {
			return true
		}
	}
	return false
}

func curlTokenMutates(tok string) bool {
	lower := strings.ToLower(tok)
	if lower == "--form" || strings.HasPrefix(lower, "--form=") ||
		lower == "--form-string" || strings.HasPrefix(lower, "--form-string=") ||
		lower == "--upload-file" || strings.HasPrefix(lower, "--upload-file=") {
		return true
	}
	return curlShortPosts(tok)
}

func segmentHTTPClient(seg []string) string {
	for i := 0; i < len(seg); {
		name := strings.ToLower(strings.TrimSuffix(commandBaseName(seg[i]), ".exe"))
		switch name {
		case "sudo":
			i = skipSudoOptions(seg, i+1)
		case "command", "nohup", "setsid":
			i = skipDashTokens(seg, i+1)
		case "exec":
			i = skipExecOptions(seg, i+1)
		case "timeout":
			i = skipTimeoutLead(seg, i+1)
		case "env":
			i++
			for i < len(seg) && envPrefixArg(seg[i]) {
				i++
			}
		case "stdbuf":
			i = skipStdbufOptions(seg, i+1)
		default:
			return httpClientKind(seg[i])
		}
	}
	return ""
}

func httpClientKind(path string) string {
	switch strings.ToLower(strings.TrimSuffix(commandBaseName(path), ".exe")) {
	case "curl":
		return "curl"
	case "wget":
		return "wget"
	case "iwr", "invoke-webrequest", "invoke-restmethod", "irm":
		return "powershell"
	default:
		return ""
	}
}

func envPrefixArg(tok string) bool {
	if strings.HasPrefix(tok, "-") {
		return true
	}
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	for _, r := range tok[:eq] {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func skipDashTokens(seg []string, i int) int {
	for i < len(seg) && strings.HasPrefix(seg[i], "-") {
		if seg[i] == "--" {
			return i + 1
		}
		i++
	}
	return i
}

func skipSudoOptions(seg []string, i int) int {
	for i < len(seg) && strings.HasPrefix(seg[i], "-") {
		tok := seg[i]
		if tok == "--" {
			return i + 1
		}
		i++
		if sudoOptTakesArg(tok) && i < len(seg) && !strings.HasPrefix(seg[i], "-") {
			i++
		}
	}
	return i
}

func sudoOptTakesArg(tok string) bool {
	if strings.Contains(tok, "=") {
		return false
	}
	if strings.HasPrefix(tok, "--") {
		switch strings.ToLower(tok) {
		case "--user", "--group", "--prompt", "--close-from", "--chdir", "--host", "--role", "--type", "--other-user", "--command-timeout", "--chroot":
			return true
		default:
			return false
		}
	}
	return len(tok) == 2 && strings.ContainsRune("ugpCDhRrTtU", rune(tok[1]))
}

func skipExecOptions(seg []string, i int) int {
	for i < len(seg) && strings.HasPrefix(seg[i], "-") {
		tok := seg[i]
		if tok == "--" {
			return i + 1
		}
		i++
		if !strings.Contains(tok, "=") && (tok == "-a" || strings.EqualFold(tok, "--argv0")) && i < len(seg) && !strings.HasPrefix(seg[i], "-") {
			i++
		}
	}
	return i
}

func skipTimeoutLead(seg []string, i int) int {
	i = skipDashTokens(seg, i)
	if i < len(seg) && httpClientKind(seg[i]) == "" {
		i++
	}
	return i
}

func skipStdbufOptions(seg []string, i int) int {
	for i < len(seg) && strings.HasPrefix(seg[i], "-") {
		tok := seg[i]
		if tok == "--" {
			return i + 1
		}
		i++
		if len(tok) == 2 && (tok[1] == 'o' || tok[1] == 'i' || tok[1] == 'e') && i < len(seg) && !strings.HasPrefix(seg[i], "-") {
			i++
		}
	}
	return i
}

func tokenReplaysBrowserSession(tok, client string) bool {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return false
	}
	// curl -b is the cookie short option, including clusters (-sb, -fsSLb)
	// and glued values. wget -b is --background. -B is curl's ASCII flag.
	if client == "curl" && curlShortOptionReplays(tok) {
		return true
	}
	lower := strings.ToLower(tok)
	if strings.HasPrefix(lower, "--cookie-") {
		return false
	}
	if lower == "--cookie" || strings.HasPrefix(lower, "--cookie=") ||
		lower == "--load-cookies" || strings.HasPrefix(lower, "--load-cookies=") ||
		lower == "-websession" || strings.HasPrefix(lower, "-websession=") || strings.HasPrefix(lower, "-websession:") {
		return true
	}
	if rest := headerFlagPayload(tok); rest != "" {
		return browserSessionHeaderToken.MatchString(rest)
	}
	return browserSessionHeaderToken.MatchString(tok)
}

// spacedSessionHeader catches PowerShell @{ Cookie = "sid" }, where the name
// and the equals sign are separate tokens.
func spacedSessionHeader(toks []string, i int) bool {
	if !browserSessionHeaderName.MatchString(strings.TrimSpace(toks[i])) {
		return false
	}
	if i+1 >= len(toks) {
		return false
	}
	next := strings.TrimSpace(toks[i+1])
	return next == "=" || next == ":" || strings.HasPrefix(next, "=") || strings.HasPrefix(next, ":")
}

// curlShortOptionReplays walks one curl short-option cluster. b sends cookies.
// A letter that takes a value ends the cluster, so -obanner.mp4 and -dbody.json
// are a filename and a body. Only -H's value is a header; -h is help.
func curlShortOptionReplays(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return false
	}
	for i := 1; i < len(tok); i++ {
		c := tok[i]
		if c == 'b' {
			return true
		}
		if curlShortTakesArg(c) {
			if c == 'H' {
				return browserSessionHeaderToken.MatchString(tok[i+1:])
			}
			return false
		}
		if c < 'A' || (c > 'Z' && c < 'a') || c > 'z' {
			return false
		}
	}
	return false
}

// curlShortPosts reports a short cluster that sends a body. -sd and -dbody
// are POST. -F is a form POST and -T is an upload. -Gd stays a GET.
func curlShortPosts(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return false
	}
	get := false
	for i := 1; i < len(tok); i++ {
		c := tok[i]
		if c == 'G' {
			get = true
			continue
		}
		if c == 'F' || c == 'T' {
			return true
		}
		if c == 'd' {
			return !get
		}
		if curlShortTakesArg(c) || c < 'A' || (c > 'Z' && c < 'a') || c > 'z' {
			return false
		}
	}
	return false
}

func curlShortTakesArg(c byte) bool {
	switch c {
	case 'A', 'c', 'C', 'd', 'D', 'e', 'E', 'F', 'h', 'H', 'K', 'm', 'o', 'P', 'Q', 'r', 't', 'T', 'u', 'U', 'w', 'x', 'X', 'y', 'Y', 'z':
		return true
	default:
		return false
	}
}

// headerFlagPayload returns the value glued to -H / --header. A spaced value
// is its own token and is checked directly. -Headers is PowerShell and keeps
// the value in the following token.
func headerFlagPayload(tok string) string {
	lower := strings.ToLower(tok)
	switch {
	case strings.HasPrefix(lower, "--header="):
		return strings.TrimSpace(tok[len("--header="):])
	case strings.HasPrefix(tok, "-H="):
		return strings.TrimSpace(tok[len("-H="):])
	case strings.HasPrefix(tok, "-H") && len(tok) > 2 && tok[2] != 'e' && tok[2] != 'E':
		return strings.TrimSpace(tok[2:])
	default:
		return ""
	}
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

// splitShellCommandSegments keeps each shell command's original text, including
// quotes, so method flags such as -d "{}" still match. ; & | ( ) separate
// commands. A newline does not: curl continuations stay one client. A semicolon
// inside @{ } stays too: that is a PowerShell hashtable entry, not a new command.
func splitShellCommandSegments(command string) []string {
	var segments []string
	var b strings.Builder
	var quote rune
	var prev rune
	hashtable := 0
	flush := func() {
		part := strings.TrimSpace(b.String())
		b.Reset()
		if part != "" {
			segments = append(segments, part)
		}
	}
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			b.WriteRune(r)
			prev = r
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
			b.WriteRune(r)
		case '{':
			if prev == '@' {
				hashtable++
			}
			b.WriteRune(r)
		case '}':
			if hashtable > 0 {
				hashtable--
			}
			b.WriteRune(r)
		case ';':
			if hashtable > 0 {
				b.WriteRune(r)
			} else {
				flush()
			}
		case '&', '|', '(', ')':
			flush()
		default:
			b.WriteRune(r)
		}
		prev = r
	}
	flush()
	return segments
}

func shellLikeFields(command string) []string {
	var fields []string
	for _, seg := range shellSegments(command) {
		fields = append(fields, seg...)
	}
	return fields
}

// shellSegments splits on ; & | ( ) the same way the shell does, and keeps
// quoted text in one token. Newlines stay inside a segment so a continued
// curl command is still one client.
func shellSegments(command string) [][]string {
	var segments [][]string
	var cur []string
	var b strings.Builder
	var quote rune
	flushTok := func() {
		if b.Len() == 0 {
			return
		}
		cur = append(cur, b.String())
		b.Reset()
	}
	flushSeg := func() {
		flushTok()
		if len(cur) == 0 {
			return
		}
		segments = append(segments, cur)
		cur = nil
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
			flushTok()
		case ';', '&', '|', '(', ')':
			flushSeg()
		default:
			b.WriteRune(r)
		}
	}
	flushSeg()
	return segments
}

func commandBaseName(path string) string {
	path = strings.TrimSpace(path)
	if idx := strings.LastIndexAny(path, `/\`); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
