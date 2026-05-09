package skill

import (
	"fmt"
	"runtime"
	"strings"
)

// ErrorClass is the unified error classification enum.
// All error classification across GUI, TUI, and self-repair uses these values.
type ErrorClass string

const (
	ErrCommandNotFound   ErrorClass = "command_not_found"  // exit 9009 (Windows) / 127 (Unix)
	ErrRateLimit         ErrorClass = "rate_limit"         // HTTP 429
	ErrFileNotFound      ErrorClass = "file_not_found"     // ENOENT / no such file
	ErrTimeout           ErrorClass = "timeout"            // context deadline exceeded / signal killed
	ErrNetworkError      ErrorClass = "network_error"      // connection refused / reset / DNS
	ErrAuthError         ErrorClass = "auth_error"         // HTTP 401/403 / permission denied
	ErrMissingParam      ErrorClass = "missing_param"      // usage: / missing argument / required
	ErrMissingEnvVar     ErrorClass = "missing_env_var"    // environment variable not set
	ErrMissingDependency ErrorClass = "missing_dependency" // package/library dependency not installed
	ErrShebangWindows    ErrorClass = "shebang_windows"    // bash shebang in Windows CMD
	ErrShortPath         ErrorClass = "short_path"         // Windows 8.3 short path failure
	ErrUnsupportedAction ErrorClass = "unsupported_action" // step action unsupported by current runner
	ErrUnknown           ErrorClass = "unknown"
)

// ClassifiedError contains the classification result, user-friendly message,
// and metadata for downstream consumers (self-repair, nudge, capability gap).
type ClassifiedError struct {
	Class       ErrorClass
	UserMessage string
	Repairable  bool
	Retryable   bool
	ActionHint  string
}

type classificationRule struct {
	class       ErrorClass
	repairable  bool
	retryable   bool
	match       func(combined string, exitCode int) bool
	userMessage func(errMsg, command string, exitCode int) string
	actionHint  func(errMsg, command string) string
}

// rules is the single source of truth for all error patterns.
// Order matters: first match wins.
var rules = []classificationRule{
	{
		class:      ErrUnsupportedAction,
		repairable: false,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "unsupported_step_action") ||
				strings.Contains(combined, "requires gui skill runner") ||
				strings.Contains(combined, "not supported by tui runner")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			msg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(errMsg), "unsupported_step_action:"))
			if msg == "" {
				return "The skill uses a step action that is not supported by this runner."
			}
			return msg
		},
		actionHint: func(errMsg, _ string) string {
			return unsupportedActionHint(errMsg)
		},
	},
	{
		class:      ErrCommandNotFound,
		repairable: true,
		retryable:  false,
		match: func(combined string, exitCode int) bool {
			return exitCode == 9009 || exitCode == 127 ||
				strings.Contains(combined, "command not found") ||
				strings.Contains(combined, "not recognized as an internal or external command") ||
				strings.Contains(combined, "not recognized as the name of") ||
				strings.Contains(combined, "is not recognized") ||
				strings.Contains(combined, "executable file not found in") ||
				(strings.Contains(combined, "command") && strings.Contains(combined, "was not found on path"))
		},
		userMessage: func(errMsg, command string, exitCode int) string {
			cmdName := commandNameForClassification(command, errMsg)
			if cmdName == "" {
				if exitCode > 0 {
					return fmt.Sprintf("Command was not found (exit %d). Ensure the required executable is installed and available on PATH. | %s", exitCode, errMsg)
				}
				return fmt.Sprintf("Command was not found. Ensure the required executable is installed and available on PATH. | %s", errMsg)
			}
			hint := fmt.Sprintf("Command %q was not found.", cmdName)
			if exitCode > 0 {
				hint = fmt.Sprintf("Command %q was not found (exit %d).", cmdName, exitCode)
			}
			switch strings.ToLower(cmdName) {
			case "python3", "python":
				hint += " Install Python 3.x and ensure it is on PATH. On Windows, install it from python.org or the Microsoft Store."
			case "pip", "pip3":
				hint += " Run python -m ensurepip --upgrade, or install pip with your Python distribution."
			case "node", "npm", "npx":
				hint += " Install Node.js: https://nodejs.org/."
			default:
				hint += " Install the dependency or add it to PATH."
			}
			return hint + " | " + errMsg
		},
		actionHint: func(errMsg, command string) string {
			cmdName := commandNameForClassification(command, errMsg)
			switch strings.ToLower(cmdName) {
			case "python3":
				return "[action: patch] Replace 'python3' with 'python' on Windows, or install python3 and add it to PATH."
			case "node", "npm", "npx":
				return "[action: inform_user] Node.js is required. Ask the user to install it from https://nodejs.org/."
			default:
				return "[action: patch] Fix the command dependency, or search for an alternative skill that does not require it."
			}
		},
	},
	{
		class:      ErrShebangWindows,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return runtime.GOOS == "windows" &&
				(strings.Contains(combined, "'#'") || strings.Contains(combined, "\"#\"")) &&
				strings.Contains(combined, "not recognized")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("A bash shebang was executed by Windows cmd/powershell as a command. Set preferred_shell: bash or use a cross-platform script. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: patch] Add preferred_shell: bash to the skill definition, or rewrite the step for the current shell."
		},
	},
	{
		class:      ErrShortPath,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return runtime.GOOS == "windows" && strings.Contains(combined, "~") &&
				(strings.Contains(combined, "enoent") || strings.Contains(combined, "no such file"))
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("Windows short-path resolution failed. Replace the '~' path with the full path and retry. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry] Retry with full filesystem paths instead of Windows 8.3 short paths."
		},
	},
	{
		class:      ErrRateLimit,
		repairable: false,
		retryable:  true,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "429") &&
				(strings.Contains(combined, "rate limit") ||
					strings.Contains(combined, "too many requests") ||
					strings.Contains(combined, "frequency limit") ||
					strings.Contains(combined, "\u9891\u7387\u9650\u5236"))
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("The API is rate limited (HTTP 429). Wait and retry later. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry_after 60s] Wait about 60 seconds, then retry."
		},
	},
	{
		class:      ErrTimeout,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "context deadline exceeded") ||
				strings.Contains(combined, "signal: killed") ||
				strings.Contains(combined, "timed out") ||
				strings.Contains(combined, "execution timeout") ||
				strings.Contains(combined, "timeout exceeded")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("The step timed out. Increase the timeout or check whether the script is waiting for input or blocking. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry_with_timeout] Retry with a larger timeout, or inspect the script for blocking operations."
		},
	},
	{
		class:      ErrNetworkError,
		repairable: false,
		retryable:  true,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "connection refused") ||
				strings.Contains(combined, "connection reset") ||
				strings.Contains(combined, "no such host") ||
				strings.Contains(combined, "network is unreachable") ||
				(strings.Contains(combined, "dns") && strings.Contains(combined, "lookup"))
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("Network connection failed. Check network connectivity and retry if the service is expected to be reachable. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry] Retry after checking network connectivity."
		},
	},
	{
		class:      ErrAuthError,
		repairable: false,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return (strings.Contains(combined, "401") && strings.Contains(combined, "unauthorized")) ||
				(strings.Contains(combined, "403") && strings.Contains(combined, "forbidden")) ||
				strings.Contains(combined, "permission denied") ||
				strings.Contains(combined, "access denied")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("Authentication or authorization failed. Check API keys, tokens, or access permissions. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: no_retry] Ask the user to provide valid credentials or API keys before retrying."
		},
	},
	{
		class:      ErrFileNotFound,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "enoent") ||
				(strings.Contains(combined, "no such file") && strings.Contains(combined, "directory")) ||
				strings.Contains(combined, "file not found") ||
				strings.Contains(combined, "\u6587\u4ef6\u4e0d\u5b58\u5728")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("A referenced file or directory was not found. Check the input paths and skill file references. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: check_args] Verify file path arguments and referenced skill files, then retry."
		},
	},
	{
		class:      ErrMissingEnvVar,
		repairable: false,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "environment variable") ||
				strings.Contains(combined, "env var") ||
				(strings.Contains(combined, "api_key") && strings.Contains(combined, "not set")) ||
				(strings.Contains(combined, "api_token") && strings.Contains(combined, "not set")) ||
				(strings.Contains(combined, "secret_key") && strings.Contains(combined, "not set"))
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("The skill is missing a required environment variable. Configure the required variable and retry. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: inform_user] Ask the user to configure the required API key or environment variable."
		},
	},
	{
		class:      ErrMissingDependency,
		repairable: false,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return (strings.Contains(combined, "required python package") && strings.Contains(combined, "is not installed")) ||
				(strings.Contains(combined, "required node package") && strings.Contains(combined, "is not installed")) ||
				isMissingPythonPackageMessage(combined) ||
				isMissingNodeESMPackageMessage(combined) ||
				isMissingNodePackageMessage(combined)
		},
		userMessage: func(errMsg, _ string, _ int) string {
			if name := missingDependencyNameFromMessage(errMsg); name != "" {
				installName := missingDependencyInstallName(missingDependencyKindFromMessage(errMsg), name)
				if installName != "" && installName != name {
					return fmt.Sprintf("The skill is missing package dependency %q (import %q). Install it and retry. | %s", installName, name, errMsg)
				}
				return fmt.Sprintf("The skill is missing package dependency %q. Install it and retry. | %s", firstNonEmptyString(installName, name), errMsg)
			}
			return fmt.Sprintf("The skill is missing a required package dependency. Install the package dependency and retry. | %s", errMsg)
		},
		actionHint: func(errMsg, _ string) string {
			return missingDependencyActionHint(errMsg)
		},
	},
	{
		class:      ErrMissingParam,
		repairable: true,
		retryable:  false,
		match: func(combined string, exitCode int) bool {
			if exitCode != 1 && exitCode != 2 {
				return false
			}
			return strings.Contains(combined, "usage:") ||
				strings.Contains(combined, "missing argument") ||
				strings.Contains(combined, "required argument") ||
				strings.Contains(combined, "no input") ||
				strings.Contains(combined, "missing required parameter") ||
				strings.Contains(combined, "\u7f3a\u5c11")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("The skill is missing a required parameter. Provide the required args and retry. | %s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: check_args] Inspect the skill parameters and pass the required args to manage_skill(action=\"run\")."
		},
	},
}

// ClassifyStepError is the unified error classification function.
func ClassifyStepError(exitCode int, output, errMsg, command string) ClassifiedError {
	combined := strings.ToLower(output + " " + errMsg)
	embeddedHint := extractEmbeddedActionHint(output + " " + errMsg)

	for _, rule := range rules {
		if rule.match(combined, exitCode) {
			classificationText := errMsg
			if rule.class == ErrMissingDependency {
				classificationText = firstNonEmptyString(strings.TrimSpace(output+" "+errMsg), errMsg)
			}
			hint := defaultActionHint(rule.class)
			if rule.actionHint != nil {
				hint = rule.actionHint(classificationText, command)
			}
			if embeddedHint != "" {
				hint = embeddedHint
			}
			return ClassifiedError{
				Class:       rule.class,
				UserMessage: rule.userMessage(classificationText, command, exitCode),
				Repairable:  rule.repairable,
				Retryable:   rule.retryable,
				ActionHint:  hint,
			}
		}
	}

	return ClassifiedError{
		Class:       ErrUnknown,
		UserMessage: errMsg,
		Repairable:  true,
		Retryable:   false,
		ActionHint:  firstNonEmptyString(embeddedHint, "[action: inspect] Inspect the step output and decide the next repair action from the concrete failure."),
	}
}

func unsupportedActionHint(errMsg string) string {
	lower := strings.ToLower(errMsg)
	if strings.Contains(lower, "tui") || strings.Contains(lower, "requires gui skill runner") {
		return "[action: open_gui] Run this skill in the GUI runner, or add executable bash steps before using TUI."
	}
	return "[action: patch] Fix the skill step action or add a step supported by the current runner."
}

func defaultActionHint(class ErrorClass) string {
	switch class {
	case ErrCommandNotFound:
		return "[action: patch] Fix the command dependency."
	case ErrRateLimit:
		return "[action: retry_after 60s] Wait, then retry."
	case ErrFileNotFound:
		return "[action: check_args] Check file path arguments."
	case ErrTimeout:
		return "[action: retry_with_timeout] Retry with a larger timeout."
	case ErrNetworkError:
		return "[action: retry] Retry after checking network connectivity."
	case ErrAuthError:
		return "[action: no_retry] Credentials are required before retrying."
	case ErrMissingParam:
		return "[action: check_args] Provide the required parameters."
	case ErrMissingEnvVar:
		return "[action: inform_user] Configure the required environment variables."
	case ErrMissingDependency:
		return "[action: install_dependency] Install the missing package dependency."
	case ErrUnsupportedAction:
		return unsupportedActionHint("")
	default:
		return "[action: inspect] Inspect the step output and classify the failure."
	}
}

// FormatErrorForLLM formats a ClassifiedError into a string that the LLM can
// consume. Includes the error class tag, user-facing message, and action hint.
func FormatErrorForLLM(ce ClassifiedError) string {
	var b strings.Builder
	userMessage := strings.TrimSpace(ce.UserMessage)
	actionHint := strings.TrimSpace(ce.ActionHint)
	if actionHint != "" {
		userMessage = strings.TrimSpace(strings.ReplaceAll(userMessage, actionHint, ""))
	}
	b.WriteString(errorClassPrefix)
	b.WriteString(string(ce.Class))
	b.WriteString(errorClassSuffix)
	if userMessage != "" {
		b.WriteByte(' ')
		b.WriteString(userMessage)
	}
	if actionHint != "" {
		b.WriteString("\n")
		b.WriteString(actionHint)
	}
	return b.String()
}

// TruncateFormattedErrorForStorage caps a formatted error while preserving the
// leading class tag and the final action hint line when possible.
func TruncateFormattedErrorForStorage(formatted string, maxLen int) string {
	formatted = strings.TrimSpace(formatted)
	if maxLen <= 0 || len(formatted) <= maxLen {
		return formatted
	}
	actionLine := ""
	actionIdx := -1
	for _, idx := range lineStartIndexes(formatted) {
		line := formatted[idx:]
		if end := strings.IndexAny(line, "\r\n"); end >= 0 {
			line = line[:end]
		}
		if strings.Contains(line, "[action:") {
			actionLine = strings.TrimSpace(line)
			actionIdx = idx
		}
	}
	if actionLine == "" {
		return truncateUTF8Bytes(formatted, maxLen)
	}
	prefix := strings.TrimSpace(formatted)
	if actionIdx >= 0 {
		prefix = strings.TrimSpace(formatted[:actionIdx])
	}
	if len(actionLine)+5 >= maxLen {
		return compactFormattedErrorWithAction(prefix, actionLine, maxLen)
	}
	suffix := "\n...\n" + actionLine
	budget := maxLen - len(suffix)
	if budget <= 0 {
		return truncateUTF8Bytes(formatted, maxLen)
	}
	if len(prefix) > budget {
		if classTag := formattedErrorClassTag(prefix); classTag != "" && len(classTag) <= budget {
			prefix = classTag
		} else {
			prefix = truncateUTF8Bytes(prefix, budget)
		}
	}
	return strings.TrimSpace(prefix) + suffix
}

func compactFormattedErrorWithAction(prefix, actionLine string, maxLen int) string {
	actionLine = strings.TrimSpace(actionLine)
	if maxLen <= 0 {
		return ""
	}
	if actionLine == "" {
		return truncateUTF8Bytes(prefix, maxLen)
	}
	marker := "\n...\n"
	prefix = firstFormattedErrorLine(prefix)
	if prefix == "" || len(marker)+len("[action:") >= maxLen {
		return truncateUTF8Bytes(actionLine, maxLen)
	}
	if classTag := formattedErrorClassTag(prefix); classTag != "" && len(classTag)+len(marker)+len("[action:") <= maxLen {
		prefix = classTag
	} else {
		prefixBudget := maxLen / 3
		if prefixBudget < len(errorClassPrefix)+len(errorClassSuffix)+8 {
			prefixBudget = len(errorClassPrefix) + len(errorClassSuffix) + 8
		}
		if prefixBudget > len(prefix) {
			prefixBudget = len(prefix)
		}
		prefix = strings.TrimSpace(truncateUTF8Bytes(prefix, prefixBudget))
	}
	actionBudget := maxLen - len(prefix) - len(marker)
	if actionBudget <= 0 {
		return truncateUTF8Bytes(actionLine, maxLen)
	}
	return prefix + marker + truncateUTF8Bytes(actionLine, actionBudget)
}

func formattedErrorClassTag(text string) string {
	start := strings.Index(text, errorClassPrefix)
	if start < 0 {
		return ""
	}
	rest := text[start:]
	end := strings.Index(rest, errorClassSuffix)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end+len(errorClassSuffix)])
}

func firstFormattedErrorLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if end := strings.IndexAny(text, "\r\n"); end >= 0 {
		text = text[:end]
	}
	return strings.TrimSpace(text)
}

const (
	errorClassPrefix = "[class: "
	errorClassSuffix = "]"
)

func firstWord(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func commandNameForClassification(command, errMsg string) string {
	if name := commandNameFromErrorMessage(errMsg); name != "" {
		return name
	}
	for _, word := range extractCommandWords(command) {
		word = normalizeInferredCommandName(word)
		lower := strings.ToLower(word)
		if word == "" || isShellEnvAssignmentField(word) || shellBuiltins[lower] || skipInferredCommand(word) || commandWrapperNames[lower] {
			continue
		}
		return word
	}
	return firstWord(command)
}

func commandNameFromErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "the term '") && strings.Contains(lower, "' is not recognized") {
		start := strings.Index(lower, "the term '") + len("the term '")
		if end := strings.Index(message[start:], "'"); end >= 0 {
			return strings.TrimSpace(message[start : start+end])
		}
	}
	if strings.Contains(lower, ": command not found") {
		prefix := message[:strings.Index(lower, ": command not found")]
		if idx := strings.LastIndex(prefix, ":"); idx >= 0 {
			prefix = prefix[idx+1:]
		}
		return strings.Trim(strings.TrimSpace(prefix), "\"'`:,;")
	}
	fields := strings.Fields(strings.TrimSpace(message))
	for i := 0; i+2 < len(fields); i++ {
		if strings.EqualFold(fields[i], "command") && strings.EqualFold(fields[i+2], "was") {
			return strings.Trim(fields[i+1], "\"'`:,;")
		}
	}
	for i := 0; i+3 < len(fields); i++ {
		if strings.EqualFold(fields[i], "required") && strings.EqualFold(fields[i+1], "command") && strings.EqualFold(fields[i+3], "was") {
			return strings.Trim(fields[i+2], "\"'`:,;")
		}
	}
	return ""
}

func missingDependencyNameFromMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	lower := strings.ToLower(message)
	for _, marker := range []string{"no module named ", "cannot find module ", "cannot find package "} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			rest := strings.TrimSpace(message[idx+len(marker):])
			return strings.Trim(firstWord(rest), "\"'`:,;")
		}
	}
	for _, marker := range []string{"required python package ", "required node package "} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			rest := strings.TrimSpace(message[idx+len(marker):])
			end := strings.Index(strings.ToLower(rest), " is not installed")
			if end >= 0 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}
	return ""
}

func missingDependencyActionHint(message string) string {
	name := missingDependencyNameFromMessage(message)
	switch missingDependencyKindFromMessage(message) {
	case "python":
		name = missingDependencyInstallName("python", name)
		if name == "" {
			return "[action: install_dependency] Install the missing Python package with pip, then retry the skill."
		}
		return fmt.Sprintf("[action: install_dependency] Install Python package %s with pip, then retry the skill.", name)
	case "node":
		name = missingDependencyInstallName("node", name)
		if name == "" {
			return "[action: install_dependency] Install the missing Node package with npm, then retry the skill."
		}
		return fmt.Sprintf("[action: install_dependency] Install Node package %s with npm, then retry the skill.", name)
	default:
		if name == "" {
			return "[action: install_dependency] Install the missing package dependency, then retry the skill."
		}
		return fmt.Sprintf("[action: install_dependency] Install package %s, then retry the skill.", name)
	}
}

func missingDependencyInstallName(kind, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	switch kind {
	case "python":
		if pkg := pythonImportPackageName(pythonTopLevelImportName(name)); pkg != "" {
			return pkg
		}
	case "node":
		if pkg := nodePackageName(name); pkg != "" {
			return pkg
		}
	}
	return name
}

func missingDependencyKindFromMessage(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "required python package"),
		strings.Contains(lower, "modulenotfounderror:"),
		strings.Contains(lower, "importerror: no module named"),
		strings.Contains(lower, "no module named "):
		return "python"
	case strings.Contains(lower, "required node package"),
		strings.Contains(lower, "err_module_not_found"),
		strings.Contains(lower, "cannot find module"):
		return "node"
	default:
		return ""
	}
}

func isLocalNodeModuleReference(combined string) bool {
	if !strings.Contains(combined, "cannot find module") {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	return isLocalModulePath(name)
}

func isLocalPythonModuleReference(combined string) bool {
	if !strings.Contains(combined, "no module named ") {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	if name == "" {
		return false
	}
	return isLocalModulePath(name)
}

func isMissingPythonPackageMessage(combined string) bool {
	if !(strings.Contains(combined, "modulenotfounderror:") ||
		strings.Contains(combined, "importerror: no module named") ||
		strings.Contains(combined, "no module named ")) {
		return false
	}
	if isLocalPythonModuleReference(combined) {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	top := pythonTopLevelImportName(name)
	return top == "" || !pythonStdlibModules[top]
}

func isMissingNodePackageMessage(combined string) bool {
	if !strings.Contains(combined, "cannot find module") {
		return false
	}
	if isLocalNodeModuleReference(combined) {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	return name == "" || nodePackageName(name) != ""
}

func isMissingNodeESMPackageMessage(combined string) bool {
	if !strings.Contains(combined, "err_module_not_found") || !strings.Contains(combined, "cannot find package") {
		return false
	}
	name := missingDependencyNameFromMessage(combined)
	return name == "" || nodePackageName(name) != ""
}

func isLocalModulePath(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return true
	}
	if len(name) >= 2 && name[1] == ':' && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return true
	}
	return strings.Contains(name, `\`)
}

func truncateUTF8Bytes(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	last := 0
	for idx := range s {
		if idx > maxLen {
			break
		}
		last = idx
	}
	if last == 0 {
		return ""
	}
	return s[:last]
}

func lineStartIndexes(s string) []int {
	indexes := []int{0}
	for i, r := range s {
		if r == '\n' {
			indexes = append(indexes, i+1)
		}
	}
	return indexes
}

var commandWrapperNames = map[string]bool{
	"env": true, "sudo": true, "doas": true, "exec": true, "time": true, "nohup": true,
}

func extractEmbeddedActionHint(text string) string {
	start := strings.Index(text, "[action:")
	if start < 0 {
		return ""
	}
	rest := text[start:]
	end := strings.Index(rest, "]")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	lineEnd := strings.IndexAny(rest[end+1:], "\r\n")
	if lineEnd < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end+1+lineEnd])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
