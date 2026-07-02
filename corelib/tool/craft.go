package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AppendUTF8Env appends environment variables that force child processes to
// use UTF-8 encoding for stdin/stdout/stderr. This prevents GBK/CP936
// mojibake on Chinese Windows systems where the default code page is not
// UTF-8. The variables are always set (harmless on non-Windows) so that
// Python, Node, and PowerShell all produce UTF-8 output.
func AppendUTF8Env(base []string) []string {
	// PYTHONIOENCODING: forces Python's stdin/stdout/stderr to UTF-8.
	// PYTHONUTF8: Python 3.7+ UTF-8 mode (affects open() default encoding too).
	// NODE_OPTIONS: not needed — Node always uses UTF-8 for stdout.
	// POWERSHELL: [Console]::OutputEncoding is set via command wrapper instead.
	//
	// If the caller's environment already defines these variables, the
	// existing values are preserved (user overrides are respected).
	env := make([]string, 0, len(base)+2)
	hasPyIO, hasPyUTF8 := false, false
	for _, e := range base {
		switch {
		case strings.HasPrefix(e, "PYTHONIOENCODING="):
			hasPyIO = true
		case strings.HasPrefix(e, "PYTHONUTF8="):
			hasPyUTF8 = true
		}
		env = append(env, e)
	}
	if !hasPyIO {
		env = append(env, "PYTHONIOENCODING=utf-8")
	}
	if !hasPyUTF8 {
		env = append(env, "PYTHONUTF8=1")
	}
	return env
}

// CraftedToolsDir returns the directory for storing crafted tool scripts.
func CraftedToolsDir() string {
	base := maclawBaseDirFallback()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "data", "crafted_tools")
}

// StripScriptCodeFences removes ```lang ... ``` wrappers from LLM output.
func StripScriptCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// SaveScript writes the script to the crafted_tools directory and returns the full path.
func SaveScript(script, language, task string) (string, error) {
	dir := CraftedToolsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}
	ext := ScriptExtension(language)
	ts := time.Now().Format("20060102_150405")
	safeName := SanitizeFilename(task)
	if len(safeName) > 40 {
		safeName = safeName[:40]
	}
	filename := fmt.Sprintf("%s_%s%s", ts, safeName, ext)
	path := filepath.Join(dir, filename)
	perm := os.FileMode(0644)
	if runtime.GOOS != "windows" {
		perm = 0755
	}
	if err := os.WriteFile(path, []byte(script), perm); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

// ExecuteScript runs a script file and returns its output.
func ExecuteScript(scriptPath, language string, timeout int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch language {
	case "python":
		cmd = CommandContext(ctx, "python3", scriptPath)
		if runtime.GOOS == "windows" {
			cmd = CommandContext(ctx, "python", scriptPath)
		}
	case "node", "javascript":
		cmd = CommandContext(ctx, "node", scriptPath)
	case "powershell":
		cmd = CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	default:
		if runtime.GOOS == "windows" {
			if _, err := exec.LookPath("bash"); err == nil {
				cmd = CommandContext(ctx, "bash", scriptPath)
			} else {
				cmd = CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
			}
		} else {
			cmd = CommandContext(ctx, "bash", scriptPath)
		}
	}

	// Force UTF-8 encoding for subprocess I/O on Windows to prevent
	// GBK/CP936 mojibake when scripts output non-ASCII text.
	cmd.Env = AppendUTF8Env(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[error] timeout after %ds", timeout))
		}
		return b.String(), err
	}
	return b.String(), nil
}

// DetectScriptLanguage guesses the best script language based on the task description and OS.
func DetectScriptLanguage(task string) string {
	lower := strings.ToLower(task)
	if strings.Contains(lower, "python") || strings.Contains(lower, "pip") ||
		strings.Contains(lower, "pandas") || strings.Contains(lower, "requests") {
		return "python"
	}
	if strings.Contains(lower, "node") || strings.Contains(lower, "npm") ||
		strings.Contains(lower, "javascript") {
		return "node"
	}
	for _, word := range strings.Fields(lower) {
		if word == "js" {
			return "node"
		}
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

// ScriptExtension returns the file extension for a script language.
func ScriptExtension(language string) string {
	switch language {
	case "python":
		return ".py"
	case "node", "javascript":
		return ".js"
	case "powershell":
		return ".ps1"
	default:
		return ".sh"
	}
}

// SanitizeFilename removes characters that are invalid in filenames.
func SanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '/' || r == '\\' {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if result == "" {
		var h uint32
		for _, r := range s {
			h = h*31 + uint32(r)
		}
		return fmt.Sprintf("task_%08x", h)
	}
	return result
}

// GenerateSkillName creates a short skill name from the task description.
func GenerateSkillName(task string) string {
	name := task
	if len(name) > 30 {
		name = name[:30]
	}
	name = SanitizeFilename(name)
	if name == "" {
		name = "crafted_tool"
	}
	return "craft_" + strings.ToLower(name)
}

// ExtractTriggerKeywords extracts simple trigger keywords from a task description.
func ExtractTriggerKeywords(task string) []string {
	words := strings.Fields(task)
	var triggers []string
	seen := make(map[string]bool)
	for _, w := range words {
		w = strings.ToLower(strings.Trim(w, "，。！？、"))
		if len(w) > 1 && !seen[w] {
			triggers = append(triggers, w)
			seen[w] = true
		}
		if len(triggers) >= 5 {
			break
		}
	}
	return triggers
}

// BuildRunCommand returns the shell command to execute a saved script.
func BuildRunCommand(scriptPath, language string) string {
	switch language {
	case "python":
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("python \"%s\"", scriptPath)
		}
		return fmt.Sprintf("python3 \"%s\"", scriptPath)
	case "node", "javascript":
		return fmt.Sprintf("node \"%s\"", scriptPath)
	case "powershell":
		return fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File \"%s\"", scriptPath)
	default:
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File \"%s\"", scriptPath)
		}
		return fmt.Sprintf("bash \"%s\"", scriptPath)
	}
}
