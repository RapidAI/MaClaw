package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

var runnerFileReferenceExts = map[string]bool{
	".bat":  true,
	".cjs":  true,
	".cmd":  true,
	".cts":  true,
	".go":   true,
	".js":   true,
	".jsx":  true,
	".mjs":  true,
	".mts":  true,
	".pl":   true,
	".ps1":  true,
	".psm1": true,
	".py":   true,
	".rb":   true,
	".sh":   true,
	".ts":   true,
	".tsx":  true,
}

const runnerFileDiagnosticReadLimit = 256 * 1024

type StepFileDiagnostic struct {
	Step     int
	Path     string
	Severity string
	Message  string
}

type commandFileReference struct {
	Path    string
	BaseDir string
}

type shellParseOptions struct {
	allowBacktickQuote          bool
	allowBackslashEscape        bool
	allowBacktickEscape         bool
	allowPowerShellContinuation bool
}

func shellParseOptionsForShell(shell string) shellParseOptions {
	isPowerShell := isPowerShellRunnerShell(shell)
	return shellParseOptions{
		allowBacktickQuote:          !isPowerShell,
		allowBackslashEscape:        !isPowerShell,
		allowBacktickEscape:         isPowerShell,
		allowPowerShellContinuation: isPowerShell,
	}
}

func FormatStepFileDiagnostics(diagnostics []StepFileDiagnostic) []string {
	if len(diagnostics) == 0 {
		return nil
	}
	formatted := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		message := strings.TrimSpace(diagnostic.Message)
		if message == "" {
			continue
		}
		if diagnostic.Step > 0 {
			message = fmt.Sprintf("step %d: %s", diagnostic.Step, message)
		}
		formatted = append(formatted, message)
	}
	return formatted
}

// CheckStepFileReferences validates local script/path references before a
// runner starts executing bash steps. It intentionally operates on runner
// normalized steps so YAML/Markdown compatibility aliases and {baseDir}
// references are handled the same way as execution.
func CheckStepFileReferences(entry *corelib.NLSkillEntry) error {
	_, err := CheckStepFileReferencesWithDiagnostics(entry)
	return err
}

// CheckStepFileReferencesWithDiagnostics validates local script/path
// references and returns non-blocking diagnostics for suspicious script files.
// Missing or inaccessible referenced files are still hard failures; encoding
// and migration signals are warnings because many scripts remain executable.
func CheckStepFileReferencesWithDiagnostics(entry *corelib.NLSkillEntry) ([]StepFileDiagnostic, error) {
	return CheckStepFileReferencesWithDiagnosticsAndExpectedOutputs(entry, nil)
}

// CheckStepFileReferencesWithDiagnosticsAndExpectedOutputs is like
// CheckStepFileReferencesWithDiagnostics, but permits declared output paths to
// be absent before execution. Some skills pass input and output as positional
// script arguments, so a conservative precheck must not treat the output target
// as an already-existing input file.
func CheckStepFileReferencesWithDiagnosticsAndExpectedOutputs(entry *corelib.NLSkillEntry, expectedOutputs []string) ([]StepFileDiagnostic, error) {
	if entry == nil {
		return nil, nil
	}
	expectedOutputSet := normalizedExpectedOutputSet(expectedOutputs)
	var diagnostics []StepFileDiagnostic
	for i, rawStep := range entry.Steps {
		step := NormalizeStepForRunnerCopy(rawStep, entry.SkillDir)
		if step.Action != "bash" {
			continue
		}
		command, _ := step.Params["command"].(string)
		if strings.TrimSpace(command) == "" {
			continue
		}
		baseDir := effectiveStepReferenceDir(step, entry.SkillDir)
		if err := checkStepWorkingDir(entry.Name, i+1, step, baseDir); err != nil {
			return diagnostics, err
		}
		fileRefs, err := commandFileReferencesForPrecheck(entry.Name, i+1, command, baseDir, stepPreferredShell(step))
		if err != nil {
			return diagnostics, err
		}
		for _, ref := range fileRefs {
			fullPath, ok := resolveCommandFileReference(ref.Path, ref.BaseDir)
			if !ok {
				continue
			}
			if expectedOutputSet[normalizeExpectedOutputPath(fullPath)] {
				continue
			}
			if _, err := os.Stat(fullPath); err != nil {
				if os.IsNotExist(err) {
					return diagnostics, fmt.Errorf("skill %q step %d references missing file: %s [action: inspect_skill]", entry.Name, i+1, fullPath)
				}
				return diagnostics, fmt.Errorf("skill %q step %d cannot access referenced file: %s: %v [action: inspect_skill]", entry.Name, i+1, fullPath, err)
			}
			diagnostics = append(diagnostics, inspectStepFileReference(i+1, fullPath)...)
		}
	}
	return diagnostics, nil
}

func normalizedExpectedOutputSet(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		normalized := normalizeExpectedOutputPath(path)
		if normalized != "" {
			set[normalized] = true
		}
	}
	return set
}

func normalizeExpectedOutputPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || containsUnresolvedRunPlaceholder(path) || isRemoteURL(path) {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func checkStepWorkingDir(skillName string, stepIndex int, step corelib.NLSkillStep, resolvedWorkDir string) error {
	rawWorkDir, _ := step.Params["working_dir"].(string)
	if strings.TrimSpace(rawWorkDir) == "" || strings.TrimSpace(resolvedWorkDir) == "" || containsUnresolvedRunPlaceholder(rawWorkDir) {
		return nil
	}
	info, err := os.Stat(resolvedWorkDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill %q step %d references missing working_dir: %s [action: inspect_skill]", skillName, stepIndex, resolvedWorkDir)
		}
		return fmt.Errorf("skill %q step %d cannot access working_dir: %s: %v [action: inspect_skill]", skillName, stepIndex, resolvedWorkDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("skill %q step %d working_dir is not a directory: %s [action: inspect_skill]", skillName, stepIndex, resolvedWorkDir)
	}
	return nil
}

// CollectMissingStepFileReferences returns every local file/script reference
// in the skill's bash steps that does not exist on disk, as resolved absolute
// paths, de-duplicated in first-seen order.
//
// Unlike CheckStepFileReferences — which returns on the first missing file for
// a fast runtime precheck — this collects all problems in one pass so callers
// like the upload portability gate can surface every missing dependency at once
// (avoiding repeated fix/retry round trips). It shares the same reference
// extraction primitives as the runtime precheck, so the two stay consistent.
func CollectMissingStepFileReferences(entry *corelib.NLSkillEntry) []string {
	if entry == nil {
		return nil
	}
	var missing []string
	seen := make(map[string]bool)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		missing = append(missing, path)
	}
	for i, rawStep := range entry.Steps {
		step := NormalizeStepForRunnerCopy(rawStep, entry.SkillDir)
		if step.Action != "bash" {
			continue
		}
		command, _ := step.Params["command"].(string)
		if strings.TrimSpace(command) == "" {
			continue
		}
		baseDir := effectiveStepReferenceDir(step, entry.SkillDir)

		// Missing working_dir is also a completeness problem.
		if rawWD, _ := step.Params["working_dir"].(string); strings.TrimSpace(rawWD) != "" &&
			strings.TrimSpace(baseDir) != "" && !containsUnresolvedRunPlaceholder(rawWD) {
			if _, err := os.Stat(baseDir); err != nil && os.IsNotExist(err) {
				add(baseDir)
			}
		}

		// commandFileReferencesForPrecheck may return an error when a `cd`
		// target is missing; it still returns the references gathered so far,
		// so ignore the error and report what we found.
		fileRefs, _ := commandFileReferencesForPrecheck(entry.Name, i+1, command, baseDir, stepPreferredShell(step))
		for _, ref := range fileRefs {
			fullPath, ok := resolveCommandFileReference(ref.Path, ref.BaseDir)
			if !ok {
				continue
			}
			if _, err := os.Stat(fullPath); err != nil && os.IsNotExist(err) {
				add(fullPath)
			}
		}
	}
	return missing
}

func commandFileReferencesForPrecheck(skillName string, stepIndex int, command, baseDir, shell string) ([]commandFileReference, error) {
	currentBaseDir := baseDir
	var dirStack []string
	var refs []commandFileReference
	for _, line := range commandPrecheckLinesForShell(command, shell) {
		for _, segment := range splitCommandSegments(line) {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}
			nextBaseDir, changed, err := resolveCommandDirectoryChange(skillName, stepIndex, segment, currentBaseDir, shell, &dirStack)
			if err != nil {
				return refs, err
			}
			if changed {
				currentBaseDir = nextBaseDir
				continue
			}
			for _, ref := range extractCommandFileReferencesFromSegmentForShell(segment, shell) {
				refs = append(refs, commandFileReference{Path: ref, BaseDir: currentBaseDir})
			}
		}
	}
	return refs, nil
}

func resolveCommandDirectoryChange(skillName string, stepIndex int, segment, baseDir, shell string, dirStack *[]string) (string, bool, error) {
	fields := splitCommandFieldsForShell(segment, shell)
	commandIndex := firstShellCommandFieldIndex(fields)
	if commandIndex < 0 {
		return baseDir, false, nil
	}
	command := strings.TrimLeft(normalizeInferredCommandName(fields[commandIndex]), "(")
	if command == "popd" {
		if dirStack == nil || len(*dirStack) == 0 {
			return baseDir, true, nil
		}
		last := len(*dirStack) - 1
		nextBaseDir := (*dirStack)[last]
		*dirStack = (*dirStack)[:last]
		return nextBaseDir, true, nil
	}
	if command != "cd" && command != "pushd" {
		return baseDir, false, nil
	}
	dirIndex := commandIndex + 1
	if dirIndex < len(fields) && strings.EqualFold(fields[dirIndex], "/d") {
		dirIndex++
	}
	for dirIndex < len(fields) && strings.HasPrefix(fields[dirIndex], "-") && fields[dirIndex] != "-" {
		dirIndex++
	}
	if dirIndex >= len(fields) {
		return baseDir, true, nil
	}
	rawDir := trimCommandPathToken(fields[dirIndex])
	if rawDir == "" || rawDir == "-" || containsUnresolvedRunPlaceholder(rawDir) || isRemoteURL(rawDir) {
		return baseDir, true, nil
	}
	fullPath, ok := resolveCommandFileReference(rawDir, baseDir)
	if !ok {
		return baseDir, true, nil
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return baseDir, true, fmt.Errorf("skill %q step %d references missing directory: %s [action: inspect_skill]", skillName, stepIndex, fullPath)
		}
		return baseDir, true, fmt.Errorf("skill %q step %d cannot access referenced directory: %s: %v [action: inspect_skill]", skillName, stepIndex, fullPath, err)
	}
	if !info.IsDir() {
		return baseDir, true, fmt.Errorf("skill %q step %d references non-directory cd target: %s [action: inspect_skill]", skillName, stepIndex, fullPath)
	}
	if command == "pushd" && dirStack != nil {
		*dirStack = append(*dirStack, baseDir)
	}
	return filepath.Clean(fullPath), true, nil
}

func firstShellCommandFieldIndex(fields []string) int {
	for i, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if isShellEnvAssignmentField(field) {
			continue
		}
		return i
	}
	return -1
}

func isShellEnvAssignmentField(field string) bool {
	field = strings.TrimSpace(field)
	if field == "" || strings.HasPrefix(field, "-") {
		return false
	}
	idx := strings.Index(field, "=")
	if idx <= 0 {
		return false
	}
	name := field[:idx]
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func inspectStepFileReference(step int, fullPath string) []StepFileDiagnostic {
	if !isRunnerTextScriptPath(fullPath) {
		return nil
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, runnerFileDiagnosticReadLimit+1))
	if err != nil || len(data) == 0 {
		return nil
	}
	if len(data) > runnerFileDiagnosticReadLimit {
		data = data[:runnerFileDiagnosticReadLimit]
	}

	var diagnostics []StepFileDiagnostic
	if !utf8.Valid(data) {
		diagnostics = append(diagnostics, StepFileDiagnostic{
			Step:     step,
			Path:     fullPath,
			Severity: "warning",
			Message:  fmt.Sprintf("referenced script %s is not valid UTF-8; Windows UTF-8 runner mode may expose encoding issues [action: inspect_skill]", fullPath),
		})
		return diagnostics
	}
	if looksLikeMojibakeText(string(data)) {
		diagnostics = append(diagnostics, StepFileDiagnostic{
			Step:     step,
			Path:     fullPath,
			Severity: "warning",
			Message:  fmt.Sprintf("referenced script %s contains text that looks like encoding mojibake; review comments/strings before publishing [action: inspect_skill]", fullPath),
		})
	}
	return diagnostics
}

func isRunnerTextScriptPath(path string) bool {
	return runnerFileReferenceExts[strings.ToLower(filepath.Ext(path))]
}

func looksLikeMojibakeText(text string) bool {
	if strings.ContainsRune(text, '\uFFFD') {
		return true
	}
	markers := []string{
		"\u9225", "\u951b", "\u9286", "\u7f01\u6fc6", "\u74ba", "\u9428", "\u5a34\u5b05",
	}
	count := 0
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			count++
			if count >= 2 {
				return true
			}
		}
	}
	return false
}

// ExtractCommandFileReferences returns local file/script references from a
// shell command. It is deliberately conservative: bare executables are left to
// requirement inference, while absolute paths, ./ and ../ paths, and script
// extension files are checked as local references.
func ExtractCommandFileReferences(command string) []string {
	return extractCommandFileReferencesForShell(command, "")
}

func extractCommandFileReferencesForShell(command, shell string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, line := range commandPrecheckLinesForShell(command, shell) {
		for _, segment := range splitCommandSegments(line) {
			for _, candidate := range extractCommandFileReferencesFromSegmentForShell(segment, shell) {
				if candidate == "" || seen[candidate] {
					continue
				}
				seen[candidate] = true
				refs = append(refs, candidate)
			}
		}
	}
	return refs
}

func commandPrecheckLines(command string) []string {
	return commandPrecheckLinesForShell(command, "")
}

func commandPrecheckLinesForShell(command, shell string) []string {
	var lines []string
	var heredocEnd string
	var continued strings.Builder
	var quoted strings.Builder
	var multilineQuote rune
	parseOptions := shellParseOptionsForShell(shell)
	for _, line := range strings.Split(command, "\n") {
		trimmed := strings.TrimSpace(line)
		if heredocEnd != "" {
			if trimmed == heredocEnd {
				heredocEnd = ""
			}
			continue
		}
		logicalLine := line
		if continued.Len() > 0 {
			logicalLine = continued.String() + strings.TrimLeft(line, " \t")
		}
		if quoted.Len() > 0 {
			logicalLine = strings.TrimLeft(logicalLine, " \t")
		}
		commentStrippedLine := logicalLine
		lineQuoteState := rune(0)
		if quoted.Len() == 0 {
			commentStrippedLine = stripShellLineCommentWithOptions(logicalLine, parseOptions)
			lineQuoteState = shellLineQuoteStateWithOptions(commentStrippedLine, 0, parseOptions)
			if prefix, ok := trimShellLineContinuation(logicalLine, parseOptions.allowPowerShellContinuation); ok && lineQuoteState == 0 {
				continued.Reset()
				continued.WriteString(prefix)
				continued.WriteByte(' ')
				continue
			}
			if continued.Len() > 0 {
				continued.Reset()
			}
		}
		if quoted.Len() > 0 {
			quoted.WriteByte('\n')
			quoted.WriteString(logicalLine)
			multilineQuote = shellLineQuoteStateWithOptions(logicalLine, multilineQuote, parseOptions)
			if multilineQuote != 0 {
				continue
			}
			logicalLine = quoted.String()
			quoted.Reset()
		} else {
			logicalLine = commentStrippedLine
			multilineQuote = lineQuoteState
			if multilineQuote != 0 {
				quoted.WriteString(logicalLine)
				continue
			}
		}
		if strings.TrimSpace(logicalLine) == "" {
			continue
		}
		lines = append(lines, logicalLine)
		if end, ok := shellHeredocEndMarker(logicalLine); ok {
			heredocEnd = end
		}
	}
	if continued.Len() > 0 {
		lines = append(lines, strings.TrimSpace(continued.String()))
	}
	if quoted.Len() > 0 {
		lines = append(lines, strings.TrimSpace(quoted.String()))
	}
	return lines
}

func shellLineQuoteStateWithOptions(line string, quote rune, options shellParseOptions) rune {
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if quote != '\'' && options.allowBacktickEscape && r == '`' && i+1 < len(runes) {
				i++
				continue
			}
			if quote != '\'' && options.allowBackslashEscape && r == '\\' && i+1 < len(runes) {
				i++
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if options.allowBackslashEscape && r == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		if options.allowBacktickEscape && r == '`' && i+1 < len(runes) {
			i++
			continue
		}
		if r == '\'' || r == '"' || (options.allowBacktickQuote && r == '`') {
			quote = r
		}
	}
	return quote
}

func trimShellLineContinuation(line string, allowPowerShellContinuation bool) (string, bool) {
	trimmed := strings.TrimRight(line, " \t\r")
	if trimmed == "" {
		return line, false
	}
	runes := []rune(trimmed)
	if allowPowerShellContinuation {
		backticks := 0
		for i := len(runes) - 1; i >= 0 && runes[i] == '`'; i-- {
			backticks++
		}
		if backticks%2 == 1 {
			return string(runes[:len(runes)-1]), true
		}
		return line, false
	}
	backslashes := 0
	for i := len(runes) - 1; i >= 0 && runes[i] == '\\'; i-- {
		backslashes++
	}
	if backslashes%2 == 0 {
		return line, false
	}
	return string(runes[:len(runes)-1]), true
}

func stepPreferredShell(step corelib.NLSkillStep) string {
	for _, key := range []string{"preferred_shell", "shell"} {
		if value, ok := step.Params[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isPowerShellRunnerShell(shell string) bool {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
}

func stripShellLineCommentWithOptions(line string, options shellParseOptions) string {
	var quote rune
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if quote != '\'' && options.allowBacktickEscape && r == '`' && i+1 < len(runes) {
				i++
				continue
			}
			if quote != '\'' && options.allowBackslashEscape && r == '\\' && i+1 < len(runes) {
				i++
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if options.allowBackslashEscape && r == '\\' && i+1 < len(runes) {
			i++
			continue
		}
		if options.allowBacktickEscape && r == '`' && i+1 < len(runes) {
			i++
			continue
		}
		if r == '\'' || r == '"' || (options.allowBacktickQuote && r == '`') {
			quote = r
			continue
		}
		if r == '#' && isShellCommentStart(runes, i) {
			return strings.TrimRightFunc(string(runes[:i]), unicode.IsSpace)
		}
	}
	return line
}

func isShellCommentStart(runes []rune, idx int) bool {
	if idx <= 0 {
		return true
	}
	prev := runes[idx-1]
	return unicode.IsSpace(prev) || isShellCommandSeparator(prev) || prev == '('
}

func shellHeredocEndMarker(line string) (string, bool) {
	var quote rune
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			quote = r
			continue
		}
		if r != '<' || i+1 >= len(runes) || runes[i+1] != '<' {
			continue
		}
		i += 2
		if i < len(runes) && runes[i] == '-' {
			i++
		}
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			return "", false
		}
		var b strings.Builder
		var delimiterQuote rune
		if runes[i] == '\'' || runes[i] == '"' {
			delimiterQuote = runes[i]
			i++
		}
		for i < len(runes) {
			r = runes[i]
			if delimiterQuote != 0 {
				if r == delimiterQuote {
					break
				}
				b.WriteRune(r)
				i++
				continue
			}
			if unicode.IsSpace(r) || isShellCommandSeparator(r) || r == '<' || r == '>' {
				break
			}
			if r == '\\' && i+1 < len(runes) {
				i++
				b.WriteRune(runes[i])
				i++
				continue
			}
			b.WriteRune(r)
			i++
		}
		marker := strings.TrimSpace(b.String())
		return marker, marker != ""
	}
	return "", false
}

func extractCommandFileReferencesFromSegment(segment string) []string {
	return extractCommandFileReferencesFromSegmentForShell(segment, "")
}

func extractCommandFileReferencesFromSegmentForShell(segment, shell string) []string {
	var refs []string
	fields := splitCommandFieldsForShell(segment, shell)
	for i := 0; i < len(fields); i++ {
		token := fields[i]
		if isInlineCodeFlagAt(fields, i) {
			if !strings.Contains(token, "=") {
				i++
			}
			continue
		}
		if isOutputPathFlagToken(token) {
			if !flagTokenHasInlineValue(token) {
				i++
			}
			continue
		}
		for _, candidate := range commandFileReferenceCandidates(token) {
			if candidate == "" {
				continue
			}
			refs = append(refs, candidate)
		}
	}
	return refs
}

func isInlineCodeFlagAt(fields []string, idx int) bool {
	if idx < 0 || idx >= len(fields) || !isInlineCodeFlagToken(fields[idx]) {
		return false
	}
	commandIndex := effectiveInlineCodeCommandIndex(fields, idx)
	if commandIndex < 0 {
		return false
	}
	exe := strings.ToLower(normalizeInferredCommandName(fields[commandIndex]))
	flag := normalizeInlineCodeFlag(fields[idx])
	switch exe {
	case "python", "python3", "py", "perl", "ruby", "php", "bash", "sh", "zsh":
		return flag == "-c"
	case "node", "deno", "bun":
		return flag == "-e" || flag == "--eval" || flag == "-p" || flag == "--print"
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return flag == "-c" || flag == "-command" || flag == "-encodedcommand"
	default:
		return false
	}
}

func effectiveInlineCodeCommandIndex(fields []string, flagIndex int) int {
	commandIndex := firstShellCommandFieldIndex(fields)
	for commandIndex >= 0 && commandIndex < flagIndex {
		if next := wrappedCommandIndex(fields, commandIndex); next > commandIndex && next < flagIndex {
			commandIndex = next
			continue
		}
		break
	}
	if commandIndex < 0 || commandIndex >= flagIndex {
		return -1
	}
	for i := commandIndex + 1; i < flagIndex; i++ {
		field := strings.TrimSpace(fields[i])
		if field == "" {
			continue
		}
		if field == "--" {
			return -1
		}
		if isInterpreterOptionValueConsumed(fields, commandIndex, i) {
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		return -1
	}
	return commandIndex
}

func isInterpreterOptionValueConsumed(fields []string, commandIndex, idx int) bool {
	if idx <= commandIndex || idx >= len(fields) {
		return false
	}
	prev := strings.TrimSpace(fields[idx-1])
	if prev == "" || strings.Contains(prev, "=") {
		return false
	}
	exe := strings.ToLower(normalizeInferredCommandName(fields[commandIndex]))
	flag := normalizeInlineCodeFlag(prev)
	switch exe {
	case "python", "python3", "py":
		switch flag {
		case "-m", "-W", "-X":
			return true
		}
	case "node":
		switch flag {
		case "-r", "--require", "--loader", "--import":
			return true
		}
	case "deno", "bun":
		switch flag {
		case "-c", "--config":
			return true
		}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		switch flag {
		case "-executionpolicy", "-ep", "-configurationname", "-configur", "-psconsolefile", "-pscon":
			return true
		}
	}
	return false
}

func isInlineCodeFlagToken(token string) bool {
	flag := normalizeInlineCodeFlag(token)
	switch flag {
	case "-c", "--command", "-command", "-encodedcommand", "-e", "--eval", "-p", "--print":
		return true
	default:
		return false
	}
}

func normalizeInlineCodeFlag(token string) string {
	token = strings.ToLower(strings.TrimSpace(strings.Trim(token, `"'`)))
	if idx := strings.Index(token, "="); idx >= 0 {
		token = token[:idx]
	}
	return token
}

func isOutputPathFlagToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(strings.Trim(token, `"'`)))
	if token == "" {
		return false
	}
	if idx := strings.IndexAny(token, "=:"); idx >= 0 {
		token = token[:idx]
	}
	switch token {
	case "-o", "--out", "--output", "--outfile", "--output-file", "--output_path", "--output-path",
		"--dest", "--destination", "--save", "--save-as", "--write", "--write-to",
		"/o", "/out", "/output", "/outfile", "/output-file", "/output_path", "/output-path",
		"/dest", "/destination", "/save", "/save-as", "/write", "/write-to":
		return true
	default:
		return false
	}
}

func flagTokenHasInlineValue(token string) bool {
	_, ok := flagAssignmentValue(token)
	return ok
}

func commandFileReferenceCandidates(token string) []string {
	token = trimCommandPathToken(token)
	if token == "" || containsUnresolvedRunPlaceholder(token) || isRemoteURL(token) {
		return nil
	}
	if value, ok := flagAssignmentValue(token); ok {
		value = trimCommandPathToken(value)
		if isCommandFileReference(value) {
			return []string{value}
		}
		return nil
	}
	if strings.HasPrefix(token, "-") {
		return nil
	}
	if isSlashStyleShellOption(token) {
		return nil
	}
	if multilineQuotedNarrativeToken(token) {
		return nil
	}
	if !isCommandFileReference(token) {
		return nil
	}
	return []string{token}
}

func multilineQuotedNarrativeToken(token string) bool {
	token = strings.TrimSpace(strings.Trim(token, `"'`))
	return strings.ContainsAny(token, "\r\n")
}

func isSlashStyleShellOption(token string) bool {
	token = strings.TrimSpace(strings.Trim(token, `"'`))
	if len(token) < 2 || token[0] != '/' {
		return false
	}
	rest := token[1:]
	if rest == "" || strings.ContainsAny(rest, `/\`) || strings.ContainsAny(rest, ".:=") {
		return false
	}
	if len([]rune(rest)) > 3 {
		return false
	}
	for _, r := range rest {
		if !isShellFlagNamePart(r) {
			return false
		}
	}
	return true
}
func flagAssignmentValue(token string) (string, bool) {
	token = strings.TrimSpace(strings.Trim(token, `"'`))
	if token == "" {
		return "", false
	}
	if strings.HasPrefix(token, "-") {
		if idx := strings.IndexAny(token, "=:"); idx >= 0 && idx+1 < len(token) {
			return token[idx+1:], true
		}
		return "", false
	}
	if !strings.HasPrefix(token, "/") || len(token) < 3 {
		return "", false
	}
	runes := []rune(token)
	if len(runes) < 3 || !isShellFlagNameStart(runes[1]) {
		return "", false
	}
	for i := 2; i < len(runes); i++ {
		r := runes[i]
		if r == ':' || r == '=' {
			if i+1 < len(runes) {
				return string(runes[i+1:]), true
			}
			return "", false
		}
		if !isShellFlagNamePart(r) {
			return "", false
		}
	}
	return "", false
}

func isShellFlagNameStart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isShellFlagNamePart(r rune) bool {
	return isShellFlagNameStart(r) || r == '_' || r == '-' || r == '.'
}

func isCommandFileReference(token string) bool {
	token = trimCommandPathToken(token)
	if token == "" || containsUnresolvedRunPlaceholder(token) || isRemoteURL(token) {
		return false
	}
	if isShellGlobPattern(token) || isGoPackageEllipsisPattern(token) {
		return false
	}
	if token == "./" || token == `.\` || token == "../" || token == `..\` {
		return false
	}
	if packagePathIsAbs(token) || maclawpath.IsHomePath(token) {
		return true
	}
	if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") ||
		strings.HasPrefix(token, `.\`) || strings.HasPrefix(token, `..\`) {
		return true
	}
	return runnerFileReferenceExts[strings.ToLower(filepath.Ext(token))]
}

func isShellGlobPattern(token string) bool {
	return strings.ContainsAny(token, "*?[")
}

func isGoPackageEllipsisPattern(token string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(token))
	return normalized == "./..." || normalized == "../..." || strings.HasSuffix(normalized, "/...")
}

func splitCommandFields(command string) []string {
	return splitCommandFieldsWithOptions(command, shellParseOptionsForShell(""))
}

func splitCommandFieldsForShell(command, shell string) []string {
	return splitCommandFieldsWithOptions(command, shellParseOptionsForShell(shell))
}

func splitCommandFieldsWithOptions(command string, options shellParseOptions) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	skipNext := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		if skipNext {
			b.Reset()
			skipNext = false
			return
		}
		fields = append(fields, b.String())
		b.Reset()
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if quote != '\'' && options.allowBacktickEscape && r == '`' && i+1 < len(runes) {
				b.WriteRune(runes[i+1])
				i++
				continue
			}
			if quote != '\'' && options.allowBackslashEscape && r == '\\' && i+1 < len(runes) && runes[i+1] == '\\' {
				b.WriteRune(r)
				b.WriteRune(runes[i+1])
				i++
				continue
			}
			if quote != '\'' && options.allowBackslashEscape && r == '\\' && i+1 < len(runes) && isEscapableShellRune(runes[i+1]) {
				b.WriteRune(runes[i+1])
				i++
				continue
			}
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		if options.allowBackslashEscape && r == '\\' && i+1 < len(runes) && runes[i+1] == '\\' {
			b.WriteRune(r)
			b.WriteRune(runes[i+1])
			i++
			continue
		}
		if options.allowBackslashEscape && r == '\\' && i+1 < len(runes) && isEscapableShellRune(runes[i+1]) {
			b.WriteRune(runes[i+1])
			i++
			continue
		}
		if options.allowBacktickEscape && r == '`' && i+1 < len(runes) {
			b.WriteRune(runes[i+1])
			i++
			continue
		}
		switch {
		case r == '\'' || r == '"' || (options.allowBacktickQuote && r == '`'):
			quote = r
		case r == '>':
			flush()
			skipNext = true
			if i+1 < len(runes) && runes[i+1] == '>' {
				i++
			}
		case r == '<':
			flush()
			if i+1 < len(runes) && runes[i+1] == '<' {
				return fields
			}
		case unicode.IsSpace(r) || isShellCommandSeparator(r):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return fields
}

func isEscapableShellRune(r rune) bool {
	return unicode.IsSpace(r) || r == '\'' || r == '"' || r == '`' || r == '\\' ||
		isShellCommandSeparator(r) || r == '<' || r == '>' || r == '(' || r == ')'
}

func isShellCommandSeparator(r rune) bool {
	switch r {
	case ';', '&', '|':
		return true
	default:
		return false
	}
}

func trimCommandPathToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, `"'`)
	token = strings.TrimLeft(token, "(")
	token = strings.TrimRight(token, ".,;)]")
	return strings.TrimSpace(token)
}

func containsUnresolvedRunPlaceholder(s string) bool {
	return len(ExtractPlaceholderKeys(s)) > 0 || strings.Contains(s, "{{") || strings.Contains(s, "${")
}

func isRemoteURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.Contains(lower, "://") || strings.HasPrefix(lower, "data:")
}

func effectiveStepReferenceDir(step corelib.NLSkillStep, skillDir string) string {
	workDir, _ := step.Params["working_dir"].(string)
	workDir = strings.TrimSpace(workDir)
	skillDir = strings.TrimSpace(skillDir)
	if workDir == "" {
		return skillDir
	}
	if skillDir != "" {
		workDir = resolveBaseDirInBlock(workDir, skillDir)
	}
	if containsUnresolvedRunPlaceholder(workDir) {
		return ""
	}
	if !filepath.IsAbs(workDir) && skillDir != "" {
		workDir = filepath.Join(skillDir, filepath.FromSlash(filepath.ToSlash(workDir)))
	}
	return filepath.Clean(workDir)
}

func resolveCommandFileReference(ref, baseDir string) (string, bool) {
	ref = trimCommandPathToken(ref)
	if ref == "" {
		return "", false
	}
	if expanded := maclawpath.ExpandHomePath(ref); expanded != ref {
		return expanded, true
	}
	if packagePathIsAbs(ref) {
		return filepath.Clean(ref), true
	}
	if strings.TrimSpace(baseDir) == "" {
		return "", false
	}
	return filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(filepath.ToSlash(ref)))), true
}
