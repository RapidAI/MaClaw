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
	return filepath.Join(home, ".cceasy", "crafted_tools")
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
	task := stringVal(args, "task")
	if task == "" {
		return "缺少 task 参数：请描述你需要完成的任务"
	}

	language := stringVal(args, "language")
	if language == "" {
		language = detectScriptLanguage(task)
	}

	saveAsSkill := true
	if v, ok := args["save_as_skill"].(bool); ok {
		saveAsSkill = v
	}
	// Also handle string "false"
	if v, ok := args["save_as_skill"].(string); ok && strings.ToLower(v) == "false" {
		saveAsSkill = false
	}

	skillName := stringVal(args, "skill_name")

	timeout := 60
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > 300 {
			timeout = 300
		}
	}

	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	// Step 1: Generate the script via LLM.
	sendProgress("🧠 正在分析任务并生成脚本...")
	cfg := h.app.GetMaclawLLMConfig()
	script, err := generateScript(cfg, task, language, h.client)
	if err != nil {
		return fmt.Sprintf("脚本生成失败: %s", err.Error())
	}
	if strings.TrimSpace(script) == "" {
		return "LLM 未能生成有效脚本"
	}

	// Step 2: Save script to disk.
	sendProgress("💾 正在保存脚本...")
	scriptPath, err := saveScript(script, language, task)
	if err != nil {
		return fmt.Sprintf("脚本保存失败: %s", err.Error())
	}

	// Step 3: Execute the script.
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
		return result.String()
	}

	result.WriteString("\n✅ 脚本执行成功")

	// Step 4: Optionally register as a Skill.
	if saveAsSkill && h.app.skillExecutor != nil {
		sendProgress("📦 正在注册为 Skill...")
		regResult := h.registerCraftedSkill(task, skillName, scriptPath, language)
		result.WriteString("\n")
		result.WriteString(regResult)
	}

	return result.String()
}

// registerCraftedSkill registers a crafted script as a reusable NLSkillEntry.
func (h *IMMessageHandler) registerCraftedSkill(task, skillName, scriptPath, language string) string {
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

	if err := h.app.skillExecutor.Register(entry); err != nil {
		// If name conflicts, try with a suffix.
		if strings.Contains(err.Error(), "already exists") {
			entry.Name = skillName + "_" + time.Now().Format("0102_1504")
			if err2 := h.app.skillExecutor.Register(entry); err2 != nil {
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
	sysPrompt := fmt.Sprintf(`你是一个脚本生成专家。用户会描述一个任务，你需要生成一个 %s 脚本来完成它。

规则：
- 只输出脚本代码，不要任何解释、markdown 标记或代码块标记
- 脚本必须是完整可执行的
- 包含必要的错误处理
- 如果需要安装依赖，在脚本开头用注释说明，或在脚本中自动安装
- 输出结果到 stdout，错误到 stderr
- 脚本应该是幂等的（多次执行结果一致）`, language)

	messages := []interface{}{
		map[string]string{"role": "system", "content": sysPrompt},
		map[string]string{"role": "user", "content": fmt.Sprintf("请生成一个 %s 脚本来完成以下任务：\n\n%s", language, task)},
	}

	result, err := doSimpleLLMRequest(cfg, messages, client, 60*time.Second)
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
