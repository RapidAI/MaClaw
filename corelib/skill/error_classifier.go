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
	ErrCommandNotFound ErrorClass = "command_not_found" // exit 9009 (Windows) / 127 (Unix)
	ErrRateLimit       ErrorClass = "rate_limit"        // HTTP 429
	ErrFileNotFound    ErrorClass = "file_not_found"    // ENOENT / no such file
	ErrTimeout         ErrorClass = "timeout"           // context deadline exceeded / signal killed
	ErrNetworkError    ErrorClass = "network_error"     // connection refused / reset / DNS
	ErrAuthError       ErrorClass = "auth_error"        // HTTP 401/403 / permission denied
	ErrMissingParam    ErrorClass = "missing_param"     // usage: / missing argument / required
	ErrMissingEnvVar   ErrorClass = "missing_env_var"   // environment variable not set
	ErrShebangWindows  ErrorClass = "shebang_windows"   // bash shebang in Windows CMD
	ErrShortPath       ErrorClass = "short_path"        // Windows 8.3 short path failure
	ErrUnknown         ErrorClass = "unknown"
)

// ClassifiedError contains the classification result, user-friendly message,
// and metadata for downstream consumers (self-repair, nudge, capability gap).
type ClassifiedError struct {
	Class       ErrorClass // machine-readable classification
	UserMessage string     // user-friendly error message (Chinese)
	Repairable  bool       // worth attempting LLM-driven self-repair?
	Retryable   bool       // worth automatic retry (transient error)?
	ActionHint  string     // machine-readable action suggestion for LLM
}

// classificationRule defines a single error pattern and its classification.
type classificationRule struct {
	class      ErrorClass
	repairable bool
	retryable  bool
	// match returns true if this rule applies. Receives lowercase combined
	// output (stdout+stderr+error message) and the exit code.
	match func(combined string, exitCode int) bool
	// userMessage builds the user-facing message. Receives the original
	// (non-lowered) error message and the command string.
	userMessage func(errMsg, command string, exitCode int) string
	// actionHint builds a machine-readable action suggestion for the LLM.
	// When nil, a default hint is generated from the class.
	actionHint func(errMsg, command string) string
}

// rules is the single source of truth for all error patterns.
// Order matters: first match wins.
var rules = []classificationRule{
	// --- Command not found (exit 9009 on Windows, 127 on Unix) ---
	{
		class:      ErrCommandNotFound,
		repairable: true,
		retryable:  false,
		match: func(combined string, exitCode int) bool {
			return exitCode == 9009 || exitCode == 127
		},
		userMessage: func(errMsg, command string, exitCode int) string {
			cmdName := firstWord(command)
			if cmdName == "" {
				return fmt.Sprintf("命令未找到 (exit %d)。请确认已安装并在 PATH 中。 | %s", exitCode, errMsg)
			}
			hint := fmt.Sprintf("命令 %q 未找到 (exit %d)。", cmdName, exitCode)
			switch strings.ToLower(cmdName) {
			case "python3", "python":
				hint += " 请安装 Python 3.x 并确保在 PATH 中。Windows 用户请从 python.org 安装。"
			case "pip", "pip3":
				hint += " 请运行: python -m ensurepip --upgrade"
			case "node", "npm", "npx":
				hint += " 请安装 Node.js: https://nodejs.org/"
			default:
				hint += " 请确认已安装并在 PATH 中。"
			}
			return hint + " | " + errMsg
		},
		actionHint: func(_, command string) string {
			cmdName := firstWord(command)
			switch strings.ToLower(cmdName) {
			case "python3":
				return "[action: patch] 使用 manage_skill(action=\"patch\") 将命令中的 'python3' 替换为 'python'，或搜索不依赖此命令的替代 Skill"
			case "node", "npm", "npx":
				return "[action: inform_user] 需要安装 Node.js，请告知用户从 https://nodejs.org/ 安装"
			default:
				return fmt.Sprintf("[action: patch] 使用 manage_skill(action=\"patch\") 修复命令依赖，或使用 manage_skill(action=\"search\") 搜索替代 Skill")
			}
		},
	},
	// --- Bash shebang on Windows ---
	{
		class:      ErrShebangWindows,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return (strings.Contains(combined, "'#'") || strings.Contains(combined, "\"#\"")) &&
				strings.Contains(combined, "not recognized")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("Bash 脚本的 shebang 行在 Windows CMD 中被当作命令执行。建议设置 preferred_shell: bash 或改用跨平台脚本。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: patch] 使用 manage_skill(action=\"patch\") 在 skill.yaml 中添加 preferred_shell: bash"
		},
	},
	// --- Windows 8.3 short path ---
	{
		class:      ErrShortPath,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return runtime.GOOS == "windows" && strings.Contains(combined, "~") &&
				(strings.Contains(combined, "enoent") || strings.Contains(combined, "no such file"))
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("Windows 8.3 短路径解析失败，文件路径中的 '~' 缩写无法被识别。建议使用完整路径。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry] 使用完整路径重试，避免 Windows 8.3 短路径"
		},
	},
	// --- Rate limit (HTTP 429) ---
	{
		class:      ErrRateLimit,
		repairable: false,
		retryable:  true,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "429") &&
				(strings.Contains(combined, "rate limit") ||
					strings.Contains(combined, "too many requests") ||
					strings.Contains(combined, "频率限制"))
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("API 调用过于频繁 (HTTP 429)，请稍后再试。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry_after 60s] API 限流，等待 60 秒后重试"
		},
	},
	// --- Timeout ---
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
			return fmt.Sprintf("步骤执行超时，可能是脚本挂起。建议增加 timeout 参数或检查脚本是否有阻塞操作。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry_with_timeout] 增大超时时间重试，或检查脚本是否有阻塞操作"
		},
	},
	// --- Network error ---
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
			return fmt.Sprintf("网络连接失败，请检查网络状态。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: retry] 网络错误，可直接重试"
		},
	},
	// --- Auth error ---
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
			return fmt.Sprintf("认证或权限错误，请检查 API Key 或访问权限。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: no_retry] 认证失败，需要用户提供正确的凭证或 API Key"
		},
	},
	// --- File not found (ENOENT) ---
	{
		class:      ErrFileNotFound,
		repairable: true,
		retryable:  false,
		match: func(combined string, _ int) bool {
			return strings.Contains(combined, "enoent") ||
				(strings.Contains(combined, "no such file") && strings.Contains(combined, "directory")) ||
				strings.Contains(combined, "文件不存在")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("输入文件不存在，请检查文件路径是否正确。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: check_args] 检查传入的文件路径参数是否正确，确认文件存在后重试"
		},
	},
	// --- Missing environment variable ---
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
			return fmt.Sprintf("Skill 可能缺少必需的环境变量。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: inform_user] 需要设置环境变量，请告知用户配置所需的 API Key 或环境变量"
		},
	},
	// --- Missing parameter ---
	{
		class:      ErrMissingParam,
		repairable: true,
		retryable:  false,
		match: func(combined string, exitCode int) bool {
			if exitCode != 1 && exitCode != 2 {
				return false
			}
			return strings.Contains(combined, "usage:") || strings.Contains(combined, "usage：") ||
				strings.Contains(combined, "missing argument") ||
				strings.Contains(combined, "required argument") ||
				strings.Contains(combined, "no input") || strings.Contains(combined, "缺少")
		},
		userMessage: func(errMsg, _ string, _ int) string {
			return fmt.Sprintf("Skill 可能缺少必需参数。%s", errMsg)
		},
		actionHint: func(_, _ string) string {
			return "[action: check_args] 检查 Skill 需要的参数，使用 manage_skill(action=\"run\", name=\"...\", input=\"...\") 传入正确参数"
		},
	},
}

// ClassifyStepError is the unified error classification function.
// Both GUI and TUI skill runners should call this single function.
//
// Parameters:
//   - exitCode: process exit code (0 = success, 9009/127 = command not found, etc.)
//   - output: combined stdout+stderr output from the step
//   - errMsg: the error message string (from err.Error() or similar)
//   - command: the command string that was executed (for context in user messages)
//
// Returns a ClassifiedError with the classification, user message, and metadata.
func ClassifyStepError(exitCode int, output, errMsg, command string) ClassifiedError {
	combined := strings.ToLower(output + " " + errMsg)

	for _, rule := range rules {
		if rule.match(combined, exitCode) {
			hint := ""
			if rule.actionHint != nil {
				hint = rule.actionHint(errMsg, command)
			} else {
				hint = defaultActionHint(rule.class)
			}
			return ClassifiedError{
				Class:       rule.class,
				UserMessage: rule.userMessage(errMsg, command, exitCode),
				Repairable:  rule.repairable,
				Retryable:   rule.retryable,
				ActionHint:  hint,
			}
		}
	}

	return ClassifiedError{
		Class:       ErrUnknown,
		UserMessage: errMsg,
		Repairable:  true, // unknown errors are worth trying to repair
		Retryable:   false,
		ActionHint:  "[action: inspect] 检查步骤输出，判断失败原因后决定下一步操作",
	}
}

// defaultActionHint returns a generic action hint for a given error class.
// Used when a rule does not define a custom actionHint function.
func defaultActionHint(class ErrorClass) string {
	switch class {
	case ErrCommandNotFound:
		return "[action: patch] 使用 manage_skill(action=\"patch\") 修复命令依赖"
	case ErrRateLimit:
		return "[action: retry_after 60s] 等待后重试"
	case ErrFileNotFound:
		return "[action: check_args] 检查文件路径参数"
	case ErrTimeout:
		return "[action: retry_with_timeout] 增大超时时间重试"
	case ErrNetworkError:
		return "[action: retry] 网络错误，可直接重试"
	case ErrAuthError:
		return "[action: no_retry] 认证失败，需要用户提供凭证"
	case ErrMissingParam:
		return "[action: check_args] 检查并传入正确参数"
	case ErrMissingEnvVar:
		return "[action: inform_user] 需要设置环境变量"
	default:
		return "[action: inspect] 检查步骤输出，判断失败原因"
	}
}

// FormatErrorForLLM formats a ClassifiedError into a string that the LLM can
// consume. Includes the error class tag (for machine parsing by self-repair),
// the user-friendly message, and the action hint.
//
// Format: "[class: <class>] <userMessage>\n<actionHint>"
// The class tag is parsed by ExtractErrorClass() — both use errorClassPrefix/Suffix.
func FormatErrorForLLM(ce ClassifiedError) string {
	var b strings.Builder
	b.WriteString(errorClassPrefix)
	b.WriteString(string(ce.Class))
	b.WriteString(errorClassSuffix)
	b.WriteByte(' ')
	b.WriteString(ce.UserMessage)
	if ce.ActionHint != "" {
		b.WriteString("\n")
		b.WriteString(ce.ActionHint)
	}
	return b.String()
}

// errorClassPrefix and errorClassSuffix define the format of the class tag
// embedded in FormatErrorForLLM output. ExtractErrorClass uses the same
// constants to parse it back. Single source of truth for the wire format.
const (
	errorClassPrefix = "[class: "
	errorClassSuffix = "]"
)

// firstWord extracts the first whitespace-delimited word from s.
func firstWord(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}
