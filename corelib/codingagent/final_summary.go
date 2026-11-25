package codingagent

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FinalAnswerVerificationFact is host-observed verification evidence. The
// command must be one the host actually executed and already classified as a
// verifier; the model's prose is never promoted into this shape.
type FinalAnswerVerificationFact struct {
	Command        string
	Succeeded      bool
	AfterFinalEdit bool
}

// AppendFinalAnswerHostFacts fills narrow factual gaps in a model-written
// final answer. It deliberately does not explain the implementation — that is
// the model's job — but files changed and verifier outcomes are host facts and
// must not depend on whether the model remembered to mention them.
func AppendFinalAnswerHostFacts(summary string, filesCreated, filesModified []string, verification []FinalAnswerVerificationFact) string {
	summary = sanitizeFinalAnswerMentionedCommands(strings.TrimSpace(summary), verification)
	facts := make([]string, 0, 2)
	zh := finalSummaryMostlyChinese(summary)
	if fileFact := finalAnswerFileFact(summary, filesCreated, filesModified, zh); fileFact != "" {
		facts = append(facts, fileFact)
	}
	if verificationFact := finalAnswerVerificationFact(summary, verification, zh); verificationFact != "" {
		facts = append(facts, verificationFact)
	}
	if len(facts) == 0 {
		return summary
	}
	factText := strings.Join(facts, "\n\n")
	if summary == "" {
		return factText
	}
	return summary + "\n\n" + factText
}

// LooksLikeVerificationCommand is the host-neutral conservative verifier
// vocabulary used by surfaces that execute argv commands rather than the
// desktop shell audit. It intentionally favors precision over recall.
func LooksLikeVerificationCommand(command string) bool {
	fields := finalAnswerCommandWords(strings.ToLower(strings.TrimSpace(command)))
	if len(fields) == 0 {
		return false
	}
	joined := strings.Join(fields, " ")
	if strings.Contains(joined, "|| true") || strings.Contains(joined, "--help") || strings.Contains(joined, "--version") {
		return false
	}
	for len(fields) > 0 && finalAnswerEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) > 0 && strings.Trim(fields[0], "\"'`") == "env" {
		fields = fields[1:]
		for len(fields) > 0 {
			if finalAnswerEnvAssignment(fields[0]) || strings.HasPrefix(fields[0], "--unset=") {
				fields = fields[1:]
				continue
			}
			if fields[0] == "-u" || fields[0] == "--unset" {
				if len(fields) > 1 {
					fields = fields[2:]
				} else {
					fields = fields[1:]
				}
				continue
			}
			if strings.HasPrefix(fields[0], "-") {
				fields = fields[1:]
				continue
			}
			break
		}
	}
	if len(fields) == 0 {
		return false
	}
	base := strings.Trim(fields[0], "\"'`")
	if slash := strings.LastIndexAny(base, `/\\`); slash >= 0 {
		base = base[slash+1:]
	}
	base = strings.TrimSuffix(base, ".exe")
	args := fields[1:]
	hasArg := func(words ...string) bool {
		for _, word := range words {
			for _, arg := range args {
				if strings.Trim(arg, "\"'`") == word {
					return true
				}
			}
		}
		return false
	}
	switch base {
	case "go":
		return len(args) > 0 && (args[0] == "test" || args[0] == "build" || args[0] == "vet")
	case "npm", "pnpm", "yarn", "bun":
		if len(args) == 0 {
			return false
		}
		first := strings.Trim(args[0], "\"'`")
		if first == "run" {
			if len(args) < 2 {
				return false
			}
			first = strings.Trim(args[1], "\"'`")
		}
		switch first {
		case "test", "build", "lint", "typecheck", "tsc":
			return true
		}
		return false
	case "gcc", "g++", "cc", "c++", "clang", "clang++", "cl", "rustc", "javac":
		return true
	case "cargo":
		// cargo alone is ambiguous (fmt, doc, run are not verification);
		// keep only its verifier subcommands.
		return len(args) > 0 && (args[0] == "test" || args[0] == "build" || args[0] == "check" || args[0] == "clippy")
	case "pytest", "py.test", "ctest", "ctest3", "eslint", "tsc", "vitest", "jest", "phpunit":
		return true
	case "mvn", "mvnw", "gradle", "gradlew":
		return hasArg("test", "build", "check", "verify", "package")
	case "dotnet":
		return len(args) > 0 && (args[0] == "test" || args[0] == "build")
	case "python", "python3", "py":
		if len(args) >= 2 && strings.Trim(args[0], "\"'`") == "-m" {
			switch strings.Trim(args[1], "\"'`") {
			case "pytest", "py_compile", "unittest":
				return true
			}
		}
		return false
	case "make", "nmake":
		if len(args) == 0 {
			return true
		}
		switch strings.Trim(args[0], "\"'`") {
		case "test", "check", "verify":
			return true
		}
		return false
	case "cmake":
		return hasArg("--build")
	}
	return false
}

func finalAnswerFileFact(summary string, filesCreated, filesModified []string, zh bool) string {
	created := uniqueFinalAnswerPaths(filesCreated)
	modified := uniqueFinalAnswerPaths(filesModified)
	if len(created) == 0 && len(modified) == 0 {
		return ""
	}
	lowerSummary := strings.ToLower(strings.ReplaceAll(summary, "\\", "/"))
	baseCounts := finalAnswerPathBaseCounts(created, modified)
	unmentionedCreated := make([]string, 0, len(created))
	for _, path := range created {
		if !finalAnswerMentionsPath(lowerSummary, path, baseCounts) {
			unmentionedCreated = append(unmentionedCreated, path)
		}
	}
	createdSet := make(map[string]bool, len(created))
	for _, path := range created {
		createdSet[path] = true
	}
	existingModified := make([]string, 0, len(modified))
	for _, path := range modified {
		if !createdSet[path] && !finalAnswerMentionsPath(lowerSummary, path, baseCounts) {
			existingModified = append(existingModified, path)
		}
	}
	if len(unmentionedCreated) == 0 && len(existingModified) == 0 {
		return ""
	}
	created = unmentionedCreated
	parts := make([]string, 0, 2)
	if len(created) > 0 {
		if zh {
			parts = append(parts, "新增 "+compactFinalAnswerPathList(created, zh))
		} else {
			parts = append(parts, "created "+compactFinalAnswerPathList(created, zh))
		}
	}
	if len(existingModified) > 0 {
		if zh {
			parts = append(parts, "更新 "+compactFinalAnswerPathList(existingModified, zh))
		} else {
			parts = append(parts, "updated "+compactFinalAnswerPathList(existingModified, zh))
		}
	}
	if zh {
		return "本轮实际改动：" + strings.Join(parts, "；") + "。"
	}
	return "Actual file changes: " + strings.Join(parts, "; ") + "."
}

func finalAnswerVerificationFact(summary string, verification []FinalAnswerVerificationFact, zh bool) string {
	if len(verification) == 0 {
		return ""
	}
	lowerSummary := strings.ToLower(strings.Join(strings.Fields(summary), " "))
	items := make([]string, 0, 2)
	seen := map[string]bool{}
	// Walk backwards so an earlier failed run does not hide the later passing
	// run of the same verifier; the latest execution is the evidence for the
	// final tree. Items are reversed back into chronological order below.
	for i := len(verification) - 1; i >= 0; i-- {
		fact := verification[i]
		key := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(fact.Command)), " "))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if !fact.AfterFinalEdit {
			continue
		}
		command := sanitizeFinalAnswerCommand(fact.Command)
		if command == "" {
			continue
		}
		if finalAnswerMentionsCommand(lowerSummary, fact.Command, command) {
			continue
		}
		if zh {
			outcome := "通过"
			if !fact.Succeeded {
				outcome = "失败"
			}
			items = append(items, fmt.Sprintf("`%s` %s", command, outcome))
		} else {
			outcome := "passed"
			if !fact.Succeeded {
				outcome = "failed"
			}
			items = append(items, fmt.Sprintf("`%s` %s", command, outcome))
		}
		if len(items) == 2 {
			break
		}
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	if len(items) == 0 {
		return ""
	}
	if zh {
		return "实际验证：" + strings.Join(items, "；") + "。"
	}
	return "Observed verification: " + strings.Join(items, "; ") + "."
}

// sanitizeFinalAnswerMentionedCommands rewrites a verifier invocation the
// model already mentioned when that exact host-observed invocation carries
// secrets or environment noise. Rewriting the mention is safer than merely
// suppressing the host fallback: the durable answer must not keep the raw
// credential just because the model happened to echo it first.
func sanitizeFinalAnswerMentionedCommands(summary string, verification []FinalAnswerVerificationFact) string {
	if summary == "" || len(verification) == 0 {
		return summary
	}
	for _, fact := range verification {
		raw := strings.TrimSpace(fact.Command)
		sanitized := sanitizeFinalAnswerCommand(raw)
		if raw == "" || sanitized == "" || raw == sanitized {
			continue
		}
		summary = strings.ReplaceAll(summary, raw, sanitized)
		if normalizedRaw := strings.Join(finalAnswerCommandWords(raw), " "); normalizedRaw != raw {
			summary = strings.ReplaceAll(summary, normalizedRaw, sanitized)
		}
	}
	return summary
}

// sanitizeFinalAnswerCommand keeps the verifier invocation useful without
// forwarding local environment secrets into a durable chat answer. Leading
// VAR=value assignments are execution noise; sensitive key=value arguments are
// redacted in place, as are values passed as the argument after a sensitive flag.
func sanitizeFinalAnswerCommand(command string) string {
	tokens := finalAnswerCommandTokens(strings.TrimSpace(command))
	for len(tokens) > 1 && finalAnswerEnvAssignment(tokens[0].value) {
		tokens = tokens[1:]
	}
	out := make([]string, len(tokens))
	redactedValue := make(map[int]bool, len(tokens))
	for i, token := range tokens {
		if redactedValue[i] {
			out[i] = "<redacted>"
			continue
		}
		value := redactFinalAnswerInlineAuthorization(token.value)
		value = redactFinalAnswerURLCredentials(value)
		surface := token.surface
		if value != token.value {
			surface = finalAnswerShellToken(value)
		}
		out[i] = surface
		// Flag forms carry leading dashes (--token=, -api-key=): the sensitive
		// key starts after them, so compare on the stripped spelling. Header
		// spellings often arrive as a separate token ending in a colon.
		lower := strings.ToLower(strings.TrimLeft(value, "-"))
		keyToken := strings.TrimSuffix(lower, ":")
		for _, key := range finalAnswerSensitiveKeys {
			if strings.HasPrefix(lower, key+"=") {
				if eq := strings.Index(value, "="); eq >= 0 {
					out[i] = value[:eq+1] + "<redacted>"
				} else {
					out[i] = "<redacted>"
				}
				break
			}
			if keyToken == key && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1].value, "-") {
				if strings.EqualFold(tokens[i+1].value, "bearer") {
					if i+2 < len(tokens) && !strings.HasPrefix(tokens[i+2].value, "-") {
						out[i+2] = "<redacted>"
						redactedValue[i+2] = true
					}
					break
				}
				out[i+1] = "<redacted>"
				redactedValue[i+1] = true
				break
			}
		}
		if lower == "bearer" && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1].value, "-") {
			out[i+1] = "<redacted>"
			redactedValue[i+1] = true
		}
	}
	command = strings.Join(out, " ")
	const maxRunes = 140
	runes := []rune(command)
	if len(runes) > maxRunes {
		command = string(runes[:maxRunes-1]) + "…"
	}
	return command
}

func redactFinalAnswerInlineAuthorization(value string) string {
	fields := strings.Fields(value)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "Authorization:") {
		return value
	}
	if len(fields) >= 3 && strings.EqualFold(fields[1], "Bearer") {
		return fields[0] + " " + fields[1] + " <redacted>"
	}
	return fields[0] + " <redacted>"
}

// redactFinalAnswerURLCredentials removes userinfo from URL authorities —
// verifier invocations sometimes carry a one-off https://user:token@host URL,
// which is just as sensitive as a named token flag.
func redactFinalAnswerURLCredentials(field string) string {
	scheme := strings.Index(field, "://")
	if scheme < 0 {
		return field
	}
	authorityStart := scheme + len("://")
	authorityEnd := len(field)
	if cut := strings.IndexAny(field[authorityStart:], "/?#"); cut >= 0 {
		authorityEnd = authorityStart + cut
	}
	authority := field[authorityStart:authorityEnd]
	at := strings.LastIndex(authority, "@")
	if at <= 0 {
		return field
	}
	return field[:authorityStart] + "<redacted>" + authority[at:] + field[authorityEnd:]
}

var finalAnswerSensitiveKeys = []string{
	"token", "secret", "password", "passwd", "apikey", "api_key", "api-key",
	"access_key", "access-key", "private_key", "private-key", "authorization", "auth",
}

type finalAnswerCommandToken struct {
	surface string
	value   string
}

// finalAnswerCommandTokens splits a shell-like command display into words
// while preserving each word's original surface. It understands the quoting
// and backslash escaping the coding surfaces use in practice; it is a lexer
// for redaction/display, not a shell executor.
func finalAnswerCommandTokens(command string) []finalAnswerCommandToken {
	var tokens []finalAnswerCommandToken
	var value strings.Builder
	start := -1
	inSingle := false
	inDouble := false
	escaped := false
	flush := func(end int) {
		if start < 0 {
			return
		}
		tokens = append(tokens, finalAnswerCommandToken{
			surface: command[start:end],
			value:   value.String(),
		})
		start = -1
		value.Reset()
	}
	for i, r := range command {
		if start < 0 {
			if unicode.IsSpace(r) {
				continue
			}
			start = i
		}
		if escaped {
			value.WriteRune(r)
			escaped = false
			continue
		}
		if inSingle {
			if r == '\'' {
				inSingle = false
			} else {
				value.WriteRune(r)
			}
			continue
		}
		if inDouble {
			if r == '\\' {
				if finalAnswerBackslashEscapes(command, i, true) {
					escaped = true
				} else {
					value.WriteRune(r)
				}
				continue
			}
			if r == '"' {
				inDouble = false
			} else {
				value.WriteRune(r)
			}
			continue
		}
		switch {
		case unicode.IsSpace(r):
			flush(i)
		case r == '\'':
			inSingle = true
		case r == '"':
			inDouble = true
		case r == '\\':
			if finalAnswerBackslashEscapes(command, i, false) {
				escaped = true
			} else {
				value.WriteRune(r)
			}
		default:
			value.WriteRune(r)
		}
	}
	flush(len(command))
	return tokens
}

func finalAnswerBackslashEscapes(command string, slash int, inDouble bool) bool {
	if slash+1 >= len(command) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(command[slash+1:])
	if inDouble {
		return next == '$' || next == '`' || next == '"' || next == 92 || next == 10
	}
	return unicode.IsSpace(next) || next == 39 || next == '"' || next == 92 || next == '$' || next == '`'
}

func finalAnswerCommandWords(command string) []string {
	tokens := finalAnswerCommandTokens(strings.TrimSpace(command))
	words := make([]string, 0, len(tokens))
	for _, token := range tokens {
		words = append(words, token.value)
	}
	return words
}

func finalAnswerShellToken(text string) string {
	if text == "" {
		return "''"
	}
	if !strings.ContainsAny(text, " \t\n\r\"'\\$`&|;<>*?[](){}") {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'"
}

func finalAnswerEnvAssignment(field string) bool {
	eq := strings.Index(field, "=")
	if eq <= 0 {
		return false
	}
	for _, r := range field[:eq] {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

func finalAnswerMentionsCommand(lowerSummary, rawCommand, sanitizedCommand string) bool {
	for _, command := range []string{rawCommand, sanitizedCommand} {
		forms := []string{
			strings.Join(finalAnswerCommandWords(strings.TrimSpace(command)), " "),
			strings.Join(strings.Fields(strings.TrimSpace(command)), " "),
		}
		for _, normalized := range forms {
			normalized = strings.ToLower(normalized)
			if normalized != "" && finalAnswerContainsCommandToken(lowerSummary, normalized) {
				return true
			}
		}
	}
	return false
}

// finalAnswerContainsCommandToken matches a whole command mention. A plain
// substring match would treat "go test ./pkg2" as evidence that the host's
// "go test ./pkg" had already been reported.
func finalAnswerContainsCommandToken(text, needle string) bool {
	if needle == "" {
		return false
	}
	for offset := 0; offset < len(text); {
		idx := strings.Index(text[offset:], needle)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(needle)
		if finalAnswerCommandBoundaryBefore(text, start) && finalAnswerCommandBoundaryAfter(text, end) {
			return true
		}
		offset = start + 1
	}
	return false
}

func finalAnswerCommandBoundaryBefore(text string, start int) bool {
	if start <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return !finalAnswerCommandRune(r)
}

func finalAnswerCommandBoundaryAfter(text string, end int) bool {
	if end >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[end:])
	if r == '.' {
		// A period immediately followed by another command character is part of
		// a longer argument (pkg.test); otherwise it is much more often sentence
		// punctuation after the command.
		if end+1 >= len(text) {
			return true
		}
		next, _ := utf8.DecodeRuneInString(text[end+1:])
		return !unicode.IsLetter(next) && !unicode.IsDigit(next)
	}
	return !finalAnswerCommandRune(r)
}

func finalAnswerCommandRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-/", r) || r == 92
}

func finalAnswerMentionsPath(lowerSummary, path string, baseCounts map[string]int) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	if normalized == "" {
		return false
	}
	if finalAnswerContainsPathToken(lowerSummary, normalized) {
		return true
	}
	base := finalAnswerPathBase(normalized)
	// A bare filename is enough when it is unambiguous in this change set. When
	// two touched files share the name, mentioning that name does not tell the
	// host which one the model meant, so the exact path must still be added.
	return base != "" && baseCounts[base] == 1 && finalAnswerContainsPathToken(lowerSummary, base)
}

// finalAnswerContainsPathToken matches a path as a token rather than as an
// arbitrary substring. Plain strings.Contains would treat "prefix.go" as a
// mention of "fix.go" and suppress the host's factual file list. A trailing
// period is intentionally a boundary: in prose it is much more often sentence
// punctuation than part of the path, and a rare duplicate fact is safer than
// dropping a real file fact.
func finalAnswerContainsPathToken(text, needle string) bool {
	if needle == "" {
		return false
	}
	for offset := 0; offset < len(text); {
		idx := strings.Index(text[offset:], needle)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(needle)
		if finalAnswerPathBoundaryBefore(text, start) && finalAnswerPathBoundaryAfter(text, end) {
			return true
		}
		offset = start + 1
	}
	return false
}

func finalAnswerPathBoundaryBefore(text string, start int) bool {
	if start <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	if (r == '/' || r == '\\') && start >= 2 {
		// `./fix.go` is a normal way to mention a root-relative file; do not
		// treat its leading separator as evidence of a longer different path.
		if prev, _ := utf8.DecodeLastRuneInString(text[:start-1]); prev == '.' {
			return true
		}
	}
	return !finalAnswerPathRune(r)
}

func finalAnswerPathBoundaryAfter(text string, end int) bool {
	if end >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[end:])
	return !finalAnswerPathRune(r)
}

func finalAnswerPathRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/' || r == '\\'
}

func finalAnswerPathBase(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func finalAnswerPathBaseCounts(pathGroups ...[]string) map[string]int {
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, paths := range pathGroups {
		for _, path := range paths {
			normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			counts[finalAnswerPathBase(normalized)]++
		}
	}
	return counts
}

func uniqueFinalAnswerPaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func compactFinalAnswerPathList(paths []string, zh bool) string {
	const maxShown = 4
	separator := "、"
	if !zh {
		separator = ", "
	}
	quoted := make([]string, len(paths))
	for i, path := range paths {
		quoted[i] = "`" + path + "`"
	}
	if len(quoted) <= maxShown {
		return strings.Join(quoted, separator)
	}
	shown := strings.Join(quoted[:maxShown], separator)
	if zh {
		return fmt.Sprintf("%s 等 %d 个文件", shown, len(paths))
	}
	return fmt.Sprintf("%s, and %d more", shown, len(paths)-maxShown)
}

func finalSummaryMostlyChinese(text string) bool {
	han, letters := 0, 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	for _, token := range strings.Fields(text) {
		if finalSummaryTechnicalToken(token) {
			continue
		}
		for _, r := range token {
			if unicode.Is(unicode.Latin, r) {
				letters++
			}
		}
	}
	if han == 0 {
		return false
	}
	// Paths and one borrowed word inside an English summary must not flip the
	// fact sentences to Chinese; require the Han script to actually dominate
	// the prose. Technical tokens such as paths do not count as prose.
	return letters == 0 || han*4 > letters
}

func finalSummaryTechnicalToken(token string) bool {
	trimmed := strings.Trim(token, "`'\"()[]{}<>")
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "-") || strings.ContainsAny(trimmed, `/\=`) {
		return true
	}
	runes := []rune(trimmed)
	for i, r := range runes {
		if r != '.' || i == 0 || i+1 >= len(runes) {
			continue
		}
		prev := runes[i-1]
		next := runes[i+1]
		if (unicode.IsLetter(prev) || unicode.IsDigit(prev)) && (unicode.IsLetter(next) || unicode.IsDigit(next)) {
			return true
		}
	}
	return false
}
