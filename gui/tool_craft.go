package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// craftedToolsDir returns the directory for storing crafted tool scripts.
func craftedToolsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maclaw", "data", "crafted_tools")
}

// toolCraftTool is the implementation of the "craft_tool" tool.
// It uses the LLM to generate a script that solves the described task,
// executes it, and optionally registers it as a reusable Skill.
//
// Parameters:
//   - task: what the script should accomplish (required)
//   - language: script language — "python", "bash", "powershell", "node" (optional, auto-detected)
//   - save_as_skill: if true, register the script as a Skill after successful execution (default true)
//   - skill_name: name for the Skill (optional, auto-generated from task)
//   - timeout: execution timeout in seconds (optional, default 60, max 300)
func (h *IMMessageHandler) toolCraftTool(args map[string]interface{}, onProgress ProgressCallback) string {
	output, _ := executeCraftToolCore(h.app, h.client, args, onProgress)
	return output
}

func normalizeCraftToolArgs(args map[string]interface{}) (map[string]interface{}, error) {
	normalized := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		normalized[k] = v
	}
	task := strings.TrimSpace(stringVal(normalized, "task"))
	if task == "" {
		task = strings.TrimSpace(stringVal(normalized, "instructions"))
	}
	if task == "" {
		return nil, fmt.Errorf("missing task parameter")
	}
	if output := strings.TrimSpace(stringVal(normalized, "output")); output != "" {
		task = strings.TrimSpace(task + "\n\n必须把最终生成文件写到这个精确路径：" + output + "\n不要写到其他默认目录。")
	}
	normalized["task"] = task
	return normalized, nil
}

func executeCraftToolCore(app *App, client *http.Client, args map[string]interface{}, onProgress ProgressCallback) (string, error) {
	normalizedArgs, err := normalizeCraftToolArgs(args)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("app not initialized")
	}
	cfg := app.GetMaclawLLMConfig()
	requestTimeout := time.Duration(cfg.EffectiveTimeoutSec()) * time.Second
	if requestTimeout <= 0 {
		requestTimeout = 60 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	task := stringVal(normalizedArgs, "task")
	language := stringVal(normalizedArgs, "language")
	if language == "" {
		language = detectScriptLanguage(task)
	}

	saveAsSkill := true
	if v, ok := normalizedArgs["save_as_skill"].(bool); ok {
		saveAsSkill = v
	}
	if v, ok := normalizedArgs["save_as_skill"].(string); ok && strings.ToLower(v) == "false" {
		saveAsSkill = false
	}

	skillName := stringVal(normalizedArgs, "skill_name")
	timeout := resolveCraftToolTimeout(normalizedArgs, task)

	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	sendProgress("🧠 正在分析任务并生成脚本...")
	script, err := generateScript(cfg, task, language, client)
	if err != nil {
		return fmt.Sprintf("脚本生成失败: %s", err.Error()), err
	}
	if strings.TrimSpace(script) == "" {
		return "LLM 未能生成有效脚本", fmt.Errorf("LLM 未能生成有效脚本")
	}

	sendProgress("💾 正在保存脚本...")
	scriptPath, err := saveScript(script, language, task)
	if err != nil {
		return fmt.Sprintf("脚本保存失败: %s", err.Error()), err
	}

	sendProgress(fmt.Sprintf("🚀 正在执行脚本 (%s, 超时 %ds)...", language, timeout))
	output, execErr := executeScript(scriptPath, language, timeout)

	var result strings.Builder
	result.WriteString(fmt.Sprintf("📝 脚本语言: %s\n", language))
	result.WriteString(fmt.Sprintf("📁 脚本路径: %s\n", scriptPath))

	if output != "" {
		if len(output) > 4096 {
			output = output[:4096] + "\n... (输出已截断)"
		}
		result.WriteString(fmt.Sprintf("\n--- 执行输出 ---\n%s\n", output))
	}

	if execErr != nil {
		result.WriteString(fmt.Sprintf("\n⚠️ 执行出错: %s\n", execErr.Error()))
		result.WriteString("脚本已保存，你可以手动修改后重新执行。")
		return result.String(), execErr
	}

	result.WriteString("\n✅ 脚本执行成功")
	if saveAsSkill && app.skillExecutor != nil {
		sendProgress("📦 正在注册为 Skill...")
		regResult := registerCraftedSkillEntry(app, task, skillName, scriptPath, language)
		result.WriteString("\n")
		result.WriteString(regResult)
	}

	return result.String(), nil
}

// registerCraftedSkill registers a crafted script as a reusable NLSkillEntry.
func (h *IMMessageHandler) registerCraftedSkill(task, skillName, scriptPath, language string) string {
	return registerCraftedSkillEntry(h.app, task, skillName, scriptPath, language)
}

func registerCraftedSkillEntry(app *App, task, skillName, scriptPath, language string) string {
	if skillName == "" {
		// Auto-generate a short name from the task.
		skillName = generateSkillName(task)
	}

	// Build the shell command that runs this script.
	runCmd := buildRunCommand(scriptPath, language)

	entry := NLSkillEntry{
		Name:        skillName,
		Description: task,
		Triggers:    extractTriggerKeywords(task),
		Steps: []NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": runCmd,
					"timeout": float64(120),
				},
			},
		},
		Status:    "active",
		CreatedAt: time.Now().Format(time.RFC3339),
		Source:    "crafted",
	}

	if err := app.skillExecutor.Register(entry); err != nil {
		// If name conflicts, try with a suffix.
		if strings.Contains(err.Error(), "already exists") {
			entry.Name = skillName + "_" + time.Now().Format("0102_1504")
			if err2 := app.skillExecutor.Register(entry); err2 != nil {
				return fmt.Sprintf("⚠️ Skill 注册失败: %s", err2.Error())
			}
			return fmt.Sprintf("📦 已注册为 Skill「%s」，下次可直接用 run_skill 执行", entry.Name)
		}
		return fmt.Sprintf("⚠️ Skill 注册失败: %s", err.Error())
	}
	return fmt.Sprintf("📦 已注册为 Skill「%s」，下次可直接用 run_skill 执行", entry.Name)
}

// generateScript calls the LLM to produce a script for the given task.
func generateScript(cfg MaclawLLMConfig, task, language string, client *http.Client) (string, error) {
	sysPrompt := fmt.Sprintf("只输出可执行的%s脚本源码，不要解释，不要 markdown 代码块。", language)

	messages := []interface{}{
		map[string]string{"role": "system", "content": sysPrompt},
		map[string]string{"role": "user", "content": task},
	}

	requestTimeout := time.Duration(cfg.EffectiveTimeoutSec()) * time.Second
	result, err := doSimpleLLMRequest(context.Background(), cfg, messages, client, requestTimeout)
	if err != nil {
		return "", err
	}
	if result.Content == "" {
		return "", fmt.Errorf("LLM 未返回内容")
	}

	script := result.Content
	// Strip markdown code fences if the LLM wrapped the output.
	script = stripScriptCodeFences(script)
	return script, nil
}

// stripScriptCodeFences removes ```lang ... ``` wrappers from LLM output.
func stripScriptCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence line.
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	}
	// Remove closing fence.
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// saveScript writes the script to the crafted_tools directory and returns
// the full path.
func saveScript(script, language, task string) (string, error) {
	dir := craftedToolsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	ext := scriptExtension(language)
	// Use timestamp + sanitized task as filename.
	ts := time.Now().Format("20060102_150405")
	safeName := sanitizeFilename(task)
	if len(safeName) > 40 {
		safeName = safeName[:40]
	}
	filename := fmt.Sprintf("%s_%s%s", ts, safeName, ext)
	path := filepath.Join(dir, filename)

	// On Unix, make scripts executable.
	perm := os.FileMode(0644)
	if runtime.GOOS != "windows" {
		perm = 0755
	}
	if err := os.WriteFile(path, []byte(script), perm); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

// executeScript runs a script file and returns its output.
func executeScript(scriptPath, language string, timeout int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch language {
	case "python":
		cmd = exec.CommandContext(ctx, "python3", scriptPath)
		// Fallback to "python" on Windows.
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "python", scriptPath)
		}
	case "node", "javascript":
		cmd = exec.CommandContext(ctx, "node", scriptPath)
	case "powershell":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	default: // bash
		if runtime.GOOS == "windows" {
			// Prefer bash (e.g. Git Bash) for .sh scripts on Windows;
			// fall back to powershell only if bash is not available.
			if _, err := exec.LookPath("bash"); err == nil {
				cmd = exec.CommandContext(ctx, "bash", scriptPath)
			} else {
				cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
			}
		} else {
			cmd = exec.CommandContext(ctx, "bash", scriptPath)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

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

// detectScriptLanguage guesses the best script language based on the task
// description and the current OS.
func detectScriptLanguage(task string) string {
	lower := strings.ToLower(task)
	if strings.Contains(lower, "python") || strings.Contains(lower, "pip") ||
		strings.Contains(lower, "pandas") || strings.Contains(lower, "requests") {
		return "python"
	}
	if strings.Contains(lower, "node") || strings.Contains(lower, "npm") ||
		strings.Contains(lower, "javascript") {
		return "node"
	}
	// Check for standalone "js" — avoid matching "json", "adjusts", etc.
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

// scriptExtension returns the file extension for a script language.
func scriptExtension(language string) string {
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

// sanitizeFilename removes characters that are invalid in filenames.
// For CJK-only input (e.g. Chinese task descriptions), falls back to a
// short hash to avoid producing a generic "script" name.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '/' || r == '\\' {
			b.WriteRune('_')
		}
		// Skip other characters (Chinese, punctuation, etc.)
	}
	result := b.String()
	if result == "" {
		// Produce a short hash from the original string so different CJK
		// inputs get distinct filenames instead of all mapping to "script".
		var h uint32
		for _, r := range s {
			h = h*31 + uint32(r)
		}
		return fmt.Sprintf("task_%08x", h)
	}
	return result
}

// generateSkillName creates a short skill name from the task description.
func generateSkillName(task string) string {
	// Take first 30 chars, sanitize.
	name := task
	if len(name) > 30 {
		name = name[:30]
	}
	name = sanitizeFilename(name)
	if name == "" {
		name = "crafted_tool"
	}
	return "craft_" + strings.ToLower(name)
}

// extractTriggerKeywords extracts simple trigger keywords from a task description.
func extractTriggerKeywords(task string) []string {
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

// buildRunCommand returns the shell command to execute a saved script.
func buildRunCommand(scriptPath, language string) string {
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
