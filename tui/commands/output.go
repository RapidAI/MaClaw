package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
)

// ExitCode 标准退出码。
const (
	ExitOK    = 0 // 成功
	ExitError = 1 // 运行时错误
	ExitUsage = 2 // 用法错误（参数不对）
)

// IsTTY 检测 stdout 是否连接到终端。
// 无 TTY 时 CLI 应自动禁用颜色输出。
func IsTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// UsageError 表示用法错误（退出码 2）。
type UsageError struct {
	Msg string
}

func (e *UsageError) Error() string { return e.Msg }

// NewUsageError 创建用法错误。
func NewUsageError(format string, args ...interface{}) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// TruncateDisplay 截断字符串用于表格显示，n 必须 >= 4。
func TruncateDisplay(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

// PrintJSON 将值以 JSON 格式输出到 CLI stdout（CaptureOutput 可捕获）。
// Holds the stdio read-lock for the full encode so CaptureOutput cannot swap
// the target mid-write (same guarantee as Println/Printf).
func PrintJSON(v interface{}) error {
	stdioMu.RLock()
	defer stdioMu.RUnlock()
	enc := json.NewEncoder(currentStdoutLocked())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
