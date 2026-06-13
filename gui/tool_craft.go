package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	craftRegisterPolicyAuto   = "auto"
	craftRegisterPolicyManual = "manual"
)

type craftRuntimeAvailability struct {
	Python     string
	Node       string
	Bash       string
	PowerShell string
}

type craftToolRequest struct {
	Task               string
	OriginalTask       string
	Instructions       string
	Language           string
	RuntimeLanguage    string
	WorkingDir         string
	ExpectedArtifacts  []string
	VerificationMode   string
	RegisterPolicy     string
	SkillName          string
	Timeout            int
	MaxAttempts        int
	ExtraEnv           map[string]string
	SaveAsSkill        bool
	ShouldAutoRegister bool
}

type craftAttemptResult struct {
	ScriptPath          string
	Script              string
	Language            string
	Output              string
	ExecErr             error
	VerificationStatus  craftVerificationStatus
	VerificationMessage string
	ArtifactPath        string
	Attempts            int
}

var (
	craftToolGenerateScriptFn = generateScript
	craftToolExecuteScriptFn  = executeScript
)

// craftedToolsDir returns the directory for storing crafted tool scripts.
func craftedToolsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maclaw", "data", "crafted_tools")
}

// toolCraftTool is the implementation of the "craft_tool" tool.
// It uses the LLM to generate a script that solves the described task,
// executes it, and optionally registers it as a reusable Skill.
func (h *IMMessageHandler) toolCraftTool(args map[string]interface{}, onProgress coretool.ProgressCallback) string {
	return h.toolCraftToolWithContext(context.Background(), args, onProgress)
}

func (h *IMMessageHandler) toolCraftToolWithContext(ctx context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
	if ctx == nil {
		ctx = context.Background()
	}
	output, err := executeCraftToolCoreWithContext(ctx, h.app, h.client, args, onProgress)
	if err != nil && strings.TrimSpace(output) == "" {
		return err.Error()
	}
	return output
}

func normalizeCraftToolArgs(args map[string]interface{}) (map[string]interface{}, error) {
	normalized := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		normalized[k] = v
	}
	task := strings.TrimSpace(stringVal(normalized, "task"))
	instructions := strings.TrimSpace(stringVal(normalized, "instructions"))
	if task == "" {
		task = instructions
	}
	if task == "" {
		return nil, fmt.Errorf("missing task parameter")
	}
	normalized["task"] = task
	artifacts := normalizeCraftArtifactList(normalized["expected_artifacts"])
	if output := strings.TrimSpace(stringVal(normalized, "output")); output != "" {
		task = strings.TrimSpace(task + "\n\n必须把最终生成文件写到这个精确路径：" + output + "\n不要写到其他默认目录。")
		normalized["expected_artifacts"] = appendUniqueArtifacts(artifacts, output)
	} else if len(artifacts) > 0 {
		normalized["expected_artifacts"] = artifacts
	}
	normalized["task"] = task
	return normalized, nil
}

func normalizeCraftArtifactList(raw interface{}) []string {
	switch typed := raw.(type) {
	case []string:
		return appendUniqueArtifacts(nil, typed...)
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				values = append(values, str)
			}
		}
		return appendUniqueArtifacts(nil, values...)
	case string:
		return appendUniqueArtifacts(nil, typed)
	default:
		return nil
	}
}

func appendUniqueArtifacts(base []string, artifacts ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(artifacts))
	result := make([]string, 0, len(base)+len(artifacts))
	for _, item := range append(base, artifacts...) {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func boolArg(args map[string]interface{}, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	if v, ok := args[key].(bool); ok {
		return v
	}
	if v, ok := args[key].(string); ok {
		if value, ok := coerceToolBoolToken(v); ok {
			return value
		}
	}
	return fallback
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if parsed, err := parseCraftInt(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func floatArg(args map[string]interface{}, key string, fallback float64) float64 {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func parseCraftInt(raw string) (int, error) {
	var value int
	_, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &value)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func executeCraftToolCore(app *App, client *http.Client, args map[string]interface{}, onProgress coretool.ProgressCallback) (string, error) {
	return executeCraftToolCoreWithContext(context.Background(), app, client, args, onProgress)
}

func executeCraftToolCoreWithContext(ctx context.Context, app *App, client *http.Client, args map[string]interface{}, onProgress coretool.ProgressCallback) (string, error) {
	normalizedArgs, err := normalizeCraftToolArgs(args)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("app not initialized")
	}
	cfg := app.GetMaclawLLMConfig()
	providerName := cfg.ProviderName
	providerURL := cfg.URL
	requestTimeout := time.Duration(cfg.EffectiveTimeoutSec()) * time.Second
	if requestTimeout <= 0 {
		requestTimeout = 60 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	runtimes := detectAvailableScriptRuntimes()
	request := buildCraftToolRequest(normalizedArgs, runtimes)
	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	if request.RuntimeLanguage == "" {
		result := buildCraftFailureResult(request, craftAttemptResult{
			Language:            request.Language,
			VerificationStatus:  craftVerificationRuntimeMissing,
			VerificationMessage: "未找到可用脚本运行时，请显式指定 language 或安装 python/node/bash/powershell。",
		}, providerName, providerURL)
		return result, fmt.Errorf("no runtime available for craft_tool")
	}

	attempts := request.MaxAttempts
	if attempts <= 0 {
		attempts = 2
	}
	if attempts > 3 {
		attempts = 3
	}

	var lastAttempt craftAttemptResult
	for attempt := 1; attempt <= attempts; attempt++ {
		request.Language = request.RuntimeLanguage
		if attempt == 1 {
			sendProgress(fmt.Sprintf("🧠 正在分析任务并生成脚本（第 %d/%d 次）...", attempt, attempts))
		} else {
			sendProgress(fmt.Sprintf("🛠️ 首次执行未通过，正在基于错误信息修复脚本（第 %d/%d 次）...", attempt, attempts))
		}
		script, genErr := generateScriptWithContext(ctx, cfg, request, runtimes, lastAttempt, client)
		if genErr != nil {
			lastAttempt = craftAttemptResult{Language: request.Language, Attempts: attempt, VerificationStatus: craftVerificationExecutionFailed, VerificationMessage: genErr.Error()}
			break
		}
		if strings.TrimSpace(script) == "" {
			lastAttempt = craftAttemptResult{Language: request.Language, Attempts: attempt, VerificationStatus: craftVerificationExecutionFailed, VerificationMessage: "LLM 未能生成有效脚本"}
			break
		}

		sendProgress("Security scanning generated script before execution...")
		if report, scanErr := scanCraftedScriptBeforeExecution(ctx, app, request.OriginalTask, script, request.Language, sendProgress); scanErr != nil {
			lastAttempt = craftAttemptResult{Language: request.Language, Script: script, Attempts: attempt, VerificationStatus: craftVerificationExecutionFailed, VerificationMessage: scanErr.Error()}
			if app != nil {
				app.emitSkillInstallProgress(generateSkillName(request.OriginalTask), "blocked", "Crafted script blocked before execution by security scan.", report)
			}
			break
		}
		sendProgress("Saving generated script...")
		scriptPath, saveErr := saveScript(script, request.Language, request.OriginalTask)
		if saveErr != nil {
			lastAttempt = craftAttemptResult{Language: request.Language, Attempts: attempt, VerificationStatus: craftVerificationExecutionFailed, VerificationMessage: saveErr.Error()}
			break
		}

		sendProgress(fmt.Sprintf("🚀 正在执行脚本（%s，第 %d/%d 次，超时 %ds）...", request.Language, attempt, attempts, request.Timeout))
		output, execErr := executeScriptWithContext(ctx, scriptPath, request.Language, request.WorkingDir, request.Timeout, runtimes, request.ExtraEnv)
		lastAttempt = craftAttemptResult{
			ScriptPath: scriptPath,
			Script:     script,
			Language:   request.Language,
			Output:     output,
			ExecErr:    execErr,
			Attempts:   attempt,
		}
		verification := verifyCraftExecution(request, lastAttempt)
		lastAttempt.VerificationStatus = verification.VerificationStatus
		lastAttempt.VerificationMessage = verification.VerificationMessage
		lastAttempt.ArtifactPath = verification.ArtifactPath
		if verification.VerificationStatus == craftVerificationPassed {
			result := buildCraftSuccessResult(app, request, lastAttempt, sendProgress)
			return result, nil
		}
		if !shouldRetryCraftAttempt(request, lastAttempt, attempt, attempts) {
			break
		}
	}

	result := buildCraftFailureResult(request, lastAttempt, providerName, providerURL)
	return result, fmt.Errorf("%s", firstNonEmptyCraftText(lastAttempt.VerificationMessage, "craft_tool execution failed"))
}

func buildCraftToolRequest(args map[string]interface{}, runtimes craftRuntimeAvailability) craftToolRequest {
	originalTask := strings.TrimSpace(firstNonEmptyCraftText(stringVal(args, "task"), stringVal(args, "instructions")))
	request := craftToolRequest{
		Task:              strings.TrimSpace(stringVal(args, "task")),
		OriginalTask:      originalTask,
		Instructions:      strings.TrimSpace(stringVal(args, "instructions")),
		Language:          strings.TrimSpace(stringVal(args, "language")),
		WorkingDir:        strings.TrimSpace(stringVal(args, "working_dir")),
		ExpectedArtifacts: normalizeCraftArtifactList(args["expected_artifacts"]),
		VerificationMode:  strings.TrimSpace(stringVal(args, "verification_mode")),
		RegisterPolicy:    strings.TrimSpace(stringVal(args, "register_policy")),
		SkillName:         strings.TrimSpace(stringVal(args, "skill_name")),
		Timeout:           resolveCraftToolTimeout(args, originalTask),
		MaxAttempts:       intArg(args, "max_attempts", 2),
		ExtraEnv:          cskill.ExtractRunExtraEnvFromArgs(args),
		SaveAsSkill:       boolArg(args, "save_as_skill", true),
	}
	if request.RegisterPolicy == "" {
		request.RegisterPolicy = craftRegisterPolicyAuto
	}
	request.RuntimeLanguage = chooseCraftLanguage(request, runtimes)
	request.ShouldAutoRegister = shouldAutoRegisterCraftRequest(request)
	return request
}

func chooseCraftLanguage(request craftToolRequest, runtimes craftRuntimeAvailability) string {
	explicit := strings.ToLower(strings.TrimSpace(request.Language))
	if explicit != "" {
		if runtimeSupportedForLanguage(explicit, runtimes) {
			return explicit
		}
		return ""
	}
	return detectScriptLanguageWithRuntime(request.Task, runtimes)
}

func runtimeSupportedForLanguage(language string, runtimes craftRuntimeAvailability) bool {
	switch normalizeCraftLanguageKind(language) {
	case craftLanguagePython:
		return runtimes.Python != ""
	case craftLanguageNode:
		return runtimes.Node != ""
	case craftLanguagePowerShell:
		return runtimes.PowerShell != ""
	default:
		return runtimes.Bash != "" || runtimes.PowerShell != ""
	}
}

func shouldAutoRegisterCraftRequest(request craftToolRequest) bool {
	if !request.SaveAsSkill {
		return false
	}
	if strings.EqualFold(request.RegisterPolicy, craftRegisterPolicyManual) {
		return false
	}
	if len(request.ExpectedArtifacts) > 0 {
		return false
	}
	if strings.TrimSpace(request.Instructions) != "" && strings.TrimSpace(request.Language) == "" {
		return false
	}
	return true
}

func detectAvailableScriptRuntimes() craftRuntimeAvailability {
	runtimes := craftRuntimeAvailability{
		Python:     firstAvailableLookPath("python3", "python"),
		Node:       firstAvailableLookPath("node"),
		Bash:       firstAvailableLookPath("bash"),
		PowerShell: firstAvailableLookPath("powershell", "pwsh"),
	}
	// On Windows, exec.LookPath may find python in cmd.exe PATH but not in
	// Git Bash (sh.exe) PATH. If LookPath found python, verify it's a real
	// executable (not the Microsoft Store stub) and resolve its absolute path
	// so it works in any shell environment.
	if runtime.GOOS == "windows" && runtimes.Python != "" {
		runtimes.Python = resolveWindowsPythonPath(runtimes.Python)
	}
	return runtimes
}

// resolveWindowsPythonPath ensures the Python path is an absolute path that
// works in any shell (cmd.exe, sh.exe, PowerShell). On Windows, exec.LookPath
// may return a relative name like "python" that only resolves in cmd.exe's PATH
// but not in Git Bash. This function uses `cmd /c where python` to find the
// real absolute path, filtering out Microsoft Store stubs.
func resolveWindowsPythonPath(lookPathResult string) string {
	// If already absolute, just filter out Store stubs.
	if filepath.IsAbs(lookPathResult) {
		if strings.Contains(strings.ToLower(lookPathResult), "windowsapps") {
			// Store stub — try to find real python via cmd /c where
			return findRealPythonViaCMD()
		}
		return lookPathResult
	}
	// Relative name (e.g. "python") — resolve to absolute via cmd /c where.
	absPath := findRealPythonViaCMD()
	if absPath != "" {
		return absPath
	}
	return lookPathResult // fallback to whatever LookPath found
}

// findRealPythonViaCMD uses `cmd /c where python` to discover the absolute
// path of the Python executable. This works even when the current shell is
// Git Bash (sh.exe) whose PATH doesn't include the Python install directory.
// Filters out Microsoft Store stubs (WindowsApps).
// Result is cached via sync.OnceValue to avoid spawning subprocesses on every call.
var findRealPythonViaCMD = sync.OnceValue(func() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	// Try python3 first, then python
	for _, name := range []string{"python3", "python"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, "cmd", "/c", "where", name)
		out, err := cmd.Output()
		cancel()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			p := strings.TrimSpace(line)
			if p == "" {
				continue
			}
			// Skip Microsoft Store stub
			if strings.Contains(strings.ToLower(p), "windowsapps") {
				continue
			}
			// Verify it's a real executable
			if _, err := os.Stat(p); err == nil {
				log.Printf("[craft_tool] resolved Python path via cmd /c where: %s", p)
				return p
			}
		}
	}
	return ""
})

func firstAvailableLookPath(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func generateScript(cfg corelib.MaclawLLMConfig, request craftToolRequest, runtimes craftRuntimeAvailability, previous craftAttemptResult, client *http.Client) (string, error) {
	return generateScriptWithContext(context.Background(), cfg, request, runtimes, previous, client)
}

var craftToolRetryInitialBackoff = 2 * time.Second

func generateScriptWithContext(ctx context.Context, cfg corelib.MaclawLLMConfig, request craftToolRequest, runtimes craftRuntimeAvailability, previous craftAttemptResult, client *http.Client) (string, error) {
	sysPrompt := buildCraftSystemPrompt(request, runtimes, previous)
	userPrompt := buildCraftUserPrompt(request, previous)
	messages := []interface{}{
		map[string]string{"role": "system", "content": sysPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}
	requestTimeout := time.Duration(cfg.EffectiveTimeoutSec()) * time.Second

	// Bug #2 fix: Retry with exponential backoff on HTTP 429 (rate limit).
	// This prevents craft_tool from failing immediately when the LLM API
	// returns a rate limit error, which is common when multiple skills
	// are executed in quick succession.
	const maxRetries = 3
	backoff := craftToolRetryInitialBackoff
	if backoff <= 0 {
		backoff = 2 * time.Second
	}
	var lastErr error
	isCode1234 := false
	for retry := 0; retry <= maxRetries; retry++ {
		if retry > 0 {
			if isCode1234 {
				log.Printf("[craft_tool] 智谱 code:1234 网络错误, retrying in %v (attempt %d/%d)", backoff, retry+1, maxRetries+1)
			} else {
				log.Printf("[craft_tool] HTTP 429 rate limit, retrying in %v (attempt %d/%d)", backoff, retry+1, maxRetries+1)
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
			backoff *= 2 // exponential backoff: 2s, 4s, 8s
		}
		isCode1234 = false // reset on each iteration
		result, err := doSimpleLLMRequest(ctx, cfg, messages, client, requestTimeout)
		if err != nil {
			errMsg := err.Error()
			if classifyCraftAPIError(errMsg) == craftAPIErrorRateLimit {
				lastErr = err
				continue // retry on 429
			}
			// 智谱 API code:1234 transient "网络错误" — retryable
			if classifyCraftAPIError(errMsg) == craftAPIErrorCode1234Transient {
				isCode1234 = true
				lastErr = err
				continue // retry on 智谱 code:1234
			}
			return "", err // non-retryable error, fail immediately
		}
		if result.Content == "" {
			return "", fmt.Errorf("LLM 未返回内容")
		}
		script := stripScriptCodeFences(result.Content)
		return script, nil
	}
	// All retries exhausted
	if isCode1234 {
		return "", fmt.Errorf("智谱 API 服务端临时故障（code:1234），已重试 %d 次仍失败。请稍后再试。", maxRetries)
	}
	return "", fmt.Errorf("API 调用过于频繁 (HTTP 429)，已重试 %d 次仍失败。请稍后再试。原始错误: %v", maxRetries, lastErr)
}

func buildCraftSystemPrompt(request craftToolRequest, runtimes craftRuntimeAvailability, previous craftAttemptResult) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("你是一个脚本生成器。只输出可执行的 %s 脚本源码，不要解释，不要 markdown 代码块。", request.RuntimeLanguage))
	builder.WriteString("\n要求：")
	builder.WriteString("\n1. 优先使用标准库和系统已有能力，不要假设可以安装新依赖。")
	builder.WriteString("\n2. 若必须依赖外部程序或库，先在脚本中检测；缺失时输出清晰错误并以非零状态退出。")
	builder.WriteString("\n3. 若任务要求写文件，必须写到用户指定的精确路径，不要写到默认目录。")
	builder.WriteString("\n4. 脚本必须适合非交互执行，失败时打印明确错误到 stderr 并返回非零退出码。")
	builder.WriteString("\n5. 若能自检，请在成功后打印最终产物路径或简短成功标记。")
	builder.WriteString("\n6. 不要输出说明、注释块模板或伪代码。")
	builder.WriteString("\n运行时可用性：")
	builder.WriteString("\n- python: " + craftRuntimeLabel(runtimes.Python))
	builder.WriteString("\n- node: " + craftRuntimeLabel(runtimes.Node))
	builder.WriteString("\n- bash: " + craftRuntimeLabel(runtimes.Bash))
	builder.WriteString("\n- powershell: " + craftRuntimeLabel(runtimes.PowerShell))
	if previous.Attempts > 0 {
		builder.WriteString("\n这是修复轮次，请基于上次失败原因修复，不要重复同样错误。")
		if previous.VerificationStatus == craftVerificationArtifactMissing && previous.ArtifactPath != "" {
			builder.WriteString("\n这是一次产物定向修复：不要只打印 artifact 行或成功提示，必须真正创建该文件，并保证该路径能被文件系统检测到。")
			builder.WriteString("\n若脚本成功，必须在 stdout 输出精确一行：artifact: ")
			builder.WriteString(previous.ArtifactPath)
		}
	}
	return builder.String()
}

func buildCraftUserPrompt(request craftToolRequest, previous craftAttemptResult) string {
	var builder strings.Builder
	builder.WriteString("任务描述：\n")
	builder.WriteString(request.Task)
	if request.WorkingDir != "" {
		builder.WriteString("\n\n工作目录：\n")
		builder.WriteString(request.WorkingDir)
	}
	if len(request.ExpectedArtifacts) > 0 {
		builder.WriteString("\n\n期望产物：\n")
		for _, item := range request.ExpectedArtifacts {
			builder.WriteString("- ")
			builder.WriteString(item)
			builder.WriteString("\n")
		}
	}
	if previous.Attempts > 0 {
		builder.WriteString("\n\n修复要求：\n")
		builder.WriteString("- 必须基于上一次失败原因修复，不要重复之前的输出路径或成功判定逻辑。\n")
		if previous.VerificationStatus == craftVerificationArtifactMissing && previous.ArtifactPath != "" {
			builder.WriteString("- 这次必须把最终产物写到这个精确路径：")
			builder.WriteString(previous.ArtifactPath)
			builder.WriteString("\n")
			builder.WriteString("- 成功后必须输出一行：artifact: ")
			builder.WriteString(previous.ArtifactPath)
			builder.WriteString("\n")
			builder.WriteString("- 不要只打印成功信息，必须确保该路径上的文件真实存在。\n")
		}
		builder.WriteString("\n上一次脚本：\n")
		builder.WriteString(previous.Script)
		builder.WriteString("\n\n上一次执行输出：\n")
		builder.WriteString(previous.Output)
		builder.WriteString("\n\n失败原因：\n")
		builder.WriteString(previous.VerificationMessage)
	}
	return builder.String()
}

func craftRuntimeLabel(path string) string {
	if strings.TrimSpace(path) == "" {
		return "unavailable"
	}
	return path
}

func shouldRetryCraftAttempt(request craftToolRequest, attempt craftAttemptResult, currentAttempt, maxAttempts int) bool {
	if currentAttempt >= maxAttempts {
		return false
	}
	if attempt.VerificationStatus == craftVerificationRuntimeMissing {
		return false
	}
	if attempt.VerificationStatus == craftVerificationArtifactMissing {
		message := strings.ToLower(firstNonEmptyCraftText(attempt.VerificationMessage, attempt.Output))
		if attempt.ArtifactPath == "" {
			return false
		}
		if classifyCraftArtifactFailureSignal(message) == craftFailureSignalArtifactNotReported {
			return false
		}
		return true
	}
	message := strings.ToLower(firstNonEmptyCraftText(attempt.VerificationMessage, errorText(attempt.ExecErr), attempt.Output))
	if classifyCraftFailureSignal(message).DisallowsRetry() {
		return false
	}
	if request.WorkingDir != "" {
		if classifyCraftFailureSignal(message) == craftFailureSignalEnvironment {
			return false
		}
	}
	return true
}

func buildCraftSuccessResult(app *App, request craftToolRequest, attempt craftAttemptResult, sendProgress func(string)) string {
	var result strings.Builder
	result.WriteString(fmt.Sprintf("📝 脚本语言: %s\n", attempt.Language))
	result.WriteString(fmt.Sprintf("📁 脚本路径: %s\n", attempt.ScriptPath))
	result.WriteString(fmt.Sprintf("attempts: %d\n", attempt.Attempts))
	result.WriteString(fmt.Sprintf("verification: %s\n", craftVerificationPassed))
	if attempt.ArtifactPath != "" {
		result.WriteString(fmt.Sprintf("artifact: %s\n", attempt.ArtifactPath))
	}
	if output := truncateCraftOutput(attempt.Output); output != "" {
		result.WriteString(fmt.Sprintf("\n--- 执行输出 ---\n%s\n", output))
	}
	result.WriteString("\n✅ 脚本执行成功")
	if attempt.VerificationMessage != "" {
		result.WriteString("\n")
		result.WriteString(attempt.VerificationMessage)
	}
	if request.ShouldAutoRegister && app.skillExecutor != nil {
		sendProgress("📦 正在注册为 Skill...")
		result.WriteString("\n")
		result.WriteString(registerCraftedSkillEntry(app, request.OriginalTask, request.SkillName, attempt.ScriptPath, attempt.Language))

		// Persist the crafted script to disk as a reusable skill (async).
		// This complements the in-memory registration above — the persisted
		// skill survives app restarts and can be discovered by ScanSkillDir.
		go func() {
			skillsRoot, err := cskill.PrimarySkillsDir()
			if err != nil {
				log.Printf("[craft-persist] cannot determine skills dir: %v", err)
				return
			}
			scriptContent, readErr := os.ReadFile(attempt.ScriptPath)
			if readErr != nil {
				log.Printf("[craft-persist] cannot read script %s: %v", attempt.ScriptPath, readErr)
				return
			}
			persistResult, persistErr := cskill.PersistCraftedSkill(skillsRoot, request.OriginalTask, string(scriptContent), attempt.Language)
			if persistErr != nil {
				log.Printf("[craft-persist] failed to persist crafted skill: %v", persistErr)
				return
			}
			action := "created"
			if persistResult.IsUpdate {
				action = "updated"
			}
			log.Printf("[craft-persist] %s skill %q at %s", action, persistResult.SkillName, persistResult.SkillDir)
		}()
	} else if request.SaveAsSkill {
		result.WriteString("\n📦 默认未自动注册为 Skill：该脚本更像一次性任务或强输出绑定结果。")
	}
	return result.String()
}

// humanizeCraftAPIError replaces raw JSON API error patterns in a message
// with human-readable Chinese summaries. If no pattern matches, the original
// message is returned unchanged.
func humanizeCraftAPIError(message string) string {
	switch classifyCraftAPIError(message) {
	case craftAPIErrorCode1234, craftAPIErrorCode1234Transient:
		return "API 服务端临时故障（code:1234），请稍后重试"
	case craftAPIErrorResponse:
		return "API 返回错误响应，请检查配置或稍后重试"
	case craftAPIErrorRateLimit:
		return "API 调用频率超限，请稍后重试"
	default:
		return message
	}
}

func buildCraftFailureResult(request craftToolRequest, attempt craftAttemptResult, providerName string, providerURL string) string {
	var result strings.Builder
	language := firstNonEmptyCraftText(attempt.Language, request.RuntimeLanguage, request.Language)
	if language != "" {
		result.WriteString(fmt.Sprintf("📝 脚本语言: %s\n", language))
	}
	if attempt.ScriptPath != "" {
		result.WriteString(fmt.Sprintf("📁 脚本路径: %s\n", attempt.ScriptPath))
	}
	if attempt.Attempts > 0 {
		result.WriteString(fmt.Sprintf("attempts: %d\n", attempt.Attempts))
	}
	status := firstNonEmptyCraftText(attempt.VerificationStatus.String(), craftVerificationExecutionFailed.String())
	result.WriteString(fmt.Sprintf("verification: %s\n", status))
	if providerName != "" || providerURL != "" {
		if providerName != "" && providerURL != "" {
			result.WriteString(fmt.Sprintf("provider: %s (%s)\n", providerName, providerURL))
		} else if providerName != "" {
			result.WriteString(fmt.Sprintf("provider: %s\n", providerName))
		} else {
			result.WriteString(fmt.Sprintf("provider: %s\n", providerURL))
		}
	}
	category, advice := classifyCraftFailure(request, attempt)
	result.WriteString(fmt.Sprintf("failure_category: %s\n", category))
	if category == craftFailureCategoryArtifact && attempt.ArtifactPath != "" {
		result.WriteString(fmt.Sprintf("expected_artifact: %s\n", attempt.ArtifactPath))
	}
	if attempt.ArtifactPath != "" {
		result.WriteString(fmt.Sprintf("artifact: %s\n", attempt.ArtifactPath))
	}
	if output := truncateCraftOutput(attempt.Output); output != "" {
		result.WriteString(fmt.Sprintf("\n--- 执行输出 ---\n%s\n", output))
	}
	message := strings.TrimSpace(firstNonEmptyCraftText(attempt.VerificationMessage, errorText(attempt.ExecErr), "脚本执行失败"))
	message = humanizeCraftAPIError(message)
	if message != "" {
		result.WriteString("\n⚠️ ")
		result.WriteString(message)
		result.WriteString("\n")
	}
	if advice != "" {
		result.WriteString("💡 ")
		result.WriteString(advice)
		result.WriteString("\n")
	}
	if attempt.ScriptPath != "" {
		result.WriteString("脚本已保存，你可以手动修改后重新执行；若任务需要多轮探索或代码库改造，应改走内部 CodingSubAgent。")
	}
	return strings.TrimSpace(result.String())
}

func truncateCraftOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	if len(output) > 4096 {
		return output[:4096] + "\n... (输出已截断)"
	}
	return output
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func classifyCraftFailure(request craftToolRequest, attempt craftAttemptResult) (craftFailureCategory, string) {
	if attempt.VerificationStatus == craftVerificationArtifactMissing {
		message := strings.ToLower(firstNonEmptyCraftText(attempt.VerificationMessage, attempt.Output))
		switch classifyCraftArtifactFailureSignal(message) {
		case craftFailureSignalArtifactReportedMissing:
			return craftFailureCategoryArtifact, "脚本声称已经生成产物，但磁盘上找不到该文件；请检查脚本是否写入了临时目录、错误目录，或写入后又被清理。"
		case craftFailureSignalArtifactNotReported:
			if attempt.ArtifactPath != "" {
				return craftFailureCategoryArtifact, fmt.Sprintf("脚本没有明确回报产物路径，且预期文件不存在；请强制脚本把结果写到指定路径：%s", attempt.ArtifactPath)
			}
			return craftFailureCategoryArtifact, "脚本没有明确回报产物路径；请输出 artifact: <path>，或补充明确 output / expected_artifacts。"
		default:
			if attempt.ArtifactPath != "" {
				return craftFailureCategoryArtifact, fmt.Sprintf("脚本已运行但目标产物不存在，请确认脚本把结果写到指定路径：%s", attempt.ArtifactPath)
			}
			return craftFailureCategoryArtifact, "脚本已运行但没有生成可验证的目标产物，请补充明确 output 或 expected_artifacts。"
		}
	}
	if attempt.VerificationStatus == craftVerificationRuntimeMissing {
		return craftFailureCategoryEnvironment, "当前环境缺少可用脚本运行时，请安装 python/node/bash/powershell 或显式指定可用 language。"
	}

	message := strings.ToLower(firstNonEmptyCraftText(attempt.VerificationMessage, errorText(attempt.ExecErr), attempt.Output))
	switch classifyCraftFailureSignal(message) {
	case craftFailureSignalPermission:
		return craftFailureCategoryPermission, "这是权限或认证问题，继续自动重试通常无效；请先补齐权限、凭据或登录态。"
	case craftFailureSignalEnvironment:
		return craftFailureCategoryEnvironment, "这是运行环境或外部依赖问题，建议先修复网络、证书、目录或相关依赖后再重试。"
	case craftFailureSignalCapabilityBoundary:
		return craftFailureCategoryCapability, "该任务超出单脚本自动化边界，应改走内部 CodingSubAgent 进行多步探索或代码库级修改。"
	case craftFailureSignalScript:
		return craftFailureCategoryScript, "这更像脚本本身的可修复错误，可以调整脚本内容、依赖导入或命令后再试。"
	}
	if normalizeCraftVerificationModeKind(request.VerificationMode).RequiresArtifact() {
		return craftFailureCategoryArtifact, "当前任务要求生成可验证产物，请确认脚本确实输出了目标文件。"
	}
	return craftFailureCategoryUnknown, "未能自动归类失败原因，请结合脚本输出与保存的脚本内容继续排查。"
}

func verifyCraftExecution(request craftToolRequest, attempt craftAttemptResult) craftAttemptResult {
	result := attempt
	expectedArtifactPath := firstExpectedArtifact(request.ExpectedArtifacts)
	reportedArtifactPath := detectCraftArtifactPath(attempt.Output)
	artifactPath := strings.TrimSpace(firstNonEmptyCraftText(expectedArtifactPath, reportedArtifactPath))
	result.ArtifactPath = artifactPath
	if attempt.ExecErr != nil {
		result.VerificationStatus = craftVerificationExecutionFailed
		result.VerificationMessage = firstNonEmptyCraftText(errorText(attempt.ExecErr), "脚本执行失败")
		return result
	}
	// 仅在有预期产物时才做输出可疑性检查。
	// 纯输出型脚本（诊断、echo 等）exit code == 0 即视为成功。
	if len(request.ExpectedArtifacts) > 0 || normalizeCraftVerificationModeKind(request.VerificationMode).RequiresArtifact() {
		outputLower := strings.ToLower(attempt.Output)
		if isSuspiciousCraftOutputSignal(classifyCraftFailureSignal(outputLower)) {
			result.VerificationStatus = craftVerificationOutputSuspicious
			result.VerificationMessage = "脚本输出包含明显错误信号，自动验收未通过。"
			return result
		}
		if shouldAcceptStdoutOnlyCraftResult(request, expectedArtifactPath, reportedArtifactPath, attempt) {
			result.VerificationStatus = craftVerificationPassed
			result.VerificationMessage = "script succeeded with stdout and no declared or reported artifact path; accepted as stdout-only result"
			return result
		}
		if info, err := os.Stat(artifactPath); err == nil && !info.IsDir() && info.Size() > 0 {
			result.VerificationStatus = craftVerificationPassed
			result.VerificationMessage = fmt.Sprintf("已验证目标产物存在：%s（%d bytes）", artifactPath, info.Size())
			return result
		}
		result.VerificationStatus = craftVerificationArtifactMissing
		switch {
		case reportedArtifactPath != "":
			if info, err := os.Stat(reportedArtifactPath); err == nil {
				switch {
				case info.IsDir():
					result.VerificationMessage = fmt.Sprintf("脚本报告了产物路径，但该路径是目录不是文件：%s", reportedArtifactPath)
				case info.Size() == 0:
					result.VerificationMessage = fmt.Sprintf("脚本报告了产物路径，但生成的是空文件：%s", reportedArtifactPath)
				default:
					result.VerificationMessage = fmt.Sprintf("脚本报告了产物路径，但文件校验未通过：%s", reportedArtifactPath)
				}
			} else {
				result.VerificationMessage = fmt.Sprintf("脚本报告了产物路径，但文件不存在：%s", reportedArtifactPath)
			}
		case expectedArtifactPath != "":
			if info, err := os.Stat(expectedArtifactPath); err == nil {
				switch {
				case info.IsDir():
					result.VerificationMessage = fmt.Sprintf("脚本未报告产物路径，且预期路径是目录不是文件：%s", expectedArtifactPath)
				case info.Size() == 0:
					result.VerificationMessage = fmt.Sprintf("脚本未报告产物路径，且预期路径上只有空文件：%s", expectedArtifactPath)
				default:
					result.VerificationMessage = fmt.Sprintf("脚本未报告产物路径，且预期产物校验未通过：%s", expectedArtifactPath)
				}
			} else {
				result.VerificationMessage = fmt.Sprintf("脚本未报告产物路径，且预期产物不存在：%s", expectedArtifactPath)
			}
		default:
			result.VerificationMessage = "脚本已运行，但既未报告产物路径，也未检测到预期产物。"
		}
		return result
	}
	result.VerificationStatus = craftVerificationPassed
	if artifactPath != "" {
		result.VerificationMessage = fmt.Sprintf("脚本执行成功，并检测到产物：%s", artifactPath)
	} else {
		result.VerificationMessage = "脚本执行成功。"
	}
	return result
}

func shouldAcceptStdoutOnlyCraftResult(request craftToolRequest, expectedArtifactPath, reportedArtifactPath string, attempt craftAttemptResult) bool {
	if !normalizeCraftVerificationModeKind(request.VerificationMode).RequiresArtifact() {
		return false
	}
	if len(request.ExpectedArtifacts) > 0 || expectedArtifactPath != "" || reportedArtifactPath != "" {
		return false
	}
	return strings.TrimSpace(attempt.Output) != ""
}

func firstExpectedArtifact(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.TrimSpace(paths[0])
}

func detectCraftArtifactPath(text string) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "artifact:") {
			candidate := strings.TrimSpace(strings.TrimPrefix(trimmed, "artifact:"))
			candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "Artifact:"))
			if candidate != "" {
				return strings.Trim(candidate, "`\"'“”")
			}
		}
	}
	return detectArtifactPathFromText(text)
}

// registerCraftedSkill registers a crafted script as a reusable NLSkillEntry.
func (h *IMMessageHandler) registerCraftedSkill(task, skillName, scriptPath, language string) string {
	return registerCraftedSkillEntry(h.app, task, skillName, scriptPath, language)
}

func registerCraftedSkillEntry(app *App, task, skillName, scriptPath, language string) string {
	if skillName == "" {
		skillName = generateSkillName(task)
	}
	runCmd := buildRunCommand(scriptPath, language)

	// Extract parameter schema from the generated script so the skill has
	// proper params for BindParams alias resolution and LLM context injection.
	// Without this, craft_tool-generated skills have no params schema and
	// the LLM doesn't know what arguments to pass when reusing the skill.
	var skillParams []corelib.NLSkillParam
	var requiredEnv []string
	var requiresNode []string
	var requiresPython []string
	if scriptContent, err := os.ReadFile(scriptPath); err == nil {
		source := string(scriptContent)
		skillParams = cskill.ExtractScriptParams(source, language)
		if len(skillParams) > 0 {
			log.Printf("[craft-register] extracted %d params from script %s for skill %q", len(skillParams), scriptPath, skillName)
			runCmd = cskill.AppendRunParamPlaceholders(runCmd, skillParams)
		}
		if requires := cskill.ExtractScriptRequires(source, language); requires != nil {
			requiresPython = append(requiresPython, requires.Python...)
			requiresNode = append(requiresNode, requires.Node...)
			if len(requiresPython) > 0 || len(requiresNode) > 0 {
				log.Printf("[craft-register] extracted dependencies from script %s for skill %q: python=%v node=%v", scriptPath, skillName, requiresPython, requiresNode)
			}
		}
		requiredEnv = cskill.ExtractScriptRequiredEnv(source, language)
		if len(requiredEnv) > 0 {
			log.Printf("[craft-register] extracted required env from script %s for skill %q: %v", scriptPath, skillName, requiredEnv)
		}
	}

	entry := corelib.NLSkillEntry{
		Name:        skillName,
		Description: task,
		Triggers:    extractTriggerKeywords(task),
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": runCmd,
				"timeout": float64(corelib.DefaultAgentTimeoutSec),
			},
		}},
		Params:         skillParams,
		RequiredEnv:    requiredEnv,
		RequiresNode:   requiresNode,
		RequiresPython: requiresPython,
		Status:         "active",
		CreatedAt:      time.Now().Format(time.RFC3339),
		Source:         "crafted",
		TrustLevel:     "agent-created",
	}
	register := func(candidate corelib.NLSkillEntry) (*cskill.ScanReport, error) {
		var scanDir string
		if strings.TrimSpace(scriptPath) != "" {
			var err error
			scanDir, err = os.MkdirTemp("", "maclaw-crafted-skill-scan-*")
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(scanDir)
			data, err := os.ReadFile(scriptPath)
			if err != nil {
				return nil, err
			}
			scanPath := filepath.Join(scanDir, filepath.Base(scriptPath))
			if err := os.WriteFile(scanPath, data, 0o600); err != nil {
				return nil, err
			}
		}
		var report *cskill.ScanReport
		if app == nil || !app.isRiskGuardrailOffMode() {
			scanner := cskill.NewSecurityScanner(nil)
			report = scanner.ScanInstallStaged(context.Background(), &candidate, scanDir, func(status string) {
				app.emitSkillInstallProgress(candidate.Name, "scanning", status, nil)
			})
		}
		if err := app.admitManualSkillInstall(context.Background(), &candidate, "crafted skill", report); err != nil {
			return report, err
		}
		if err := app.skillExecutor.Register(candidate); err != nil {
			return report, err
		}
		return report, nil
	}
	_, err := register(entry)
	if err != nil {
		if classifyCraftSkillRegistrationError(err).NeedsUniqueNameRetry() {
			entry.Name = skillName + "_" + time.Now().Format("0102_1504")
			_, err2 := register(entry)
			if err2 != nil {
				return fmt.Sprintf("⚠️ Skill 注册失败: %s", err2.Error())
			}
			return fmt.Sprintf("📦 已注册为 Skill「%s」，下次可直接用 run_skill 执行", entry.Name)
		}
		return fmt.Sprintf("⚠️ Skill 注册失败: %s", err.Error())
	}
	return fmt.Sprintf("📦 已注册为 Skill「%s」，下次可直接用 run_skill 执行", entry.Name)
}

func scanCraftedScriptBeforeExecution(ctx context.Context, app *App, task, script, language string, sendProgress func(string)) (*cskill.ScanReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(script) == "" {
		return nil, fmt.Errorf("generated script is empty")
	}
	skillName := generateSkillName(task)
	scanDir, err := os.MkdirTemp("", "maclaw-crafted-skill-prescan-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scanDir)
	scanPath := filepath.Join(scanDir, "script"+scriptExtension(language))
	if err := os.WriteFile(scanPath, []byte(script), 0o600); err != nil {
		return nil, err
	}
	entry := corelib.NLSkillEntry{
		Name:        skillName,
		Description: task,
		Steps:       []corelib.NLSkillStep{{Action: "script_prescan"}},
		Source:      "crafted",
		TrustLevel:  "agent-created",
	}
	if app != nil && app.isRiskGuardrailOffMode() {
		app.emitSkillInstallProgress(skillName, "scan-complete", "Risk guardrails are off; generated script allowed.", nil)
		app.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"craft_tool_prescan",
			security.RiskLow,
			security.PolicyAllow,
			fmt.Sprintf("risk guardrails off allowed crafted script before execution for skill %s", skillName),
		)
		return nil, nil
	}
	if app != nil {
		app.emitSkillInstallProgress(skillName, "scan-start", "Security scanning generated script before execution.", nil)
	}
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanInstallStaged(ctx, &entry, scanDir, func(status string) {
		if sendProgress != nil {
			sendProgress(status)
		}
		if app != nil {
			app.emitSkillInstallProgress(skillName, "scanning", status, nil)
		}
	})
	if report == nil {
		if app != nil && !app.skillInstallMissingScanShouldBlock() {
			app.emitSkillInstallProgress(skillName, "scan-complete", "Generated script security scan did not produce a report; current policy allows execution.", nil)
			app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillInstall,
				"craft_tool_prescan",
				security.RiskCritical,
				security.PolicyAudit,
				fmt.Sprintf("current policy allowed crafted script before execution for skill %s even though scan report was missing", skillName),
			)
			return nil, nil
		}
		return nil, fmt.Errorf("crafted script security scan failed")
	}
	if app != nil && app.isSecurityDeveloperMode() {
		app.emitSkillInstallProgress(skillName, "approved", "Developer mode enabled; generated script scan will not block execution.", report)
		level := report.FinalLevel
		if report.IsDangerous() {
			level = security.RiskCritical
		}
		app.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"craft_tool_prescan",
			level,
			security.PolicyAudit,
			fmt.Sprintf("developer mode allowed crafted script before execution for skill %s: %s", skillName, report.Summary),
		)
		return report, nil
	}
	if app != nil && app.skillInstallScanShouldBlock(report) {
		if app != nil {
			level := report.FinalLevel
			if report.IsDangerous() {
				level = security.RiskCritical
			}
			app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillReject,
				"craft_tool_prescan",
				level,
				security.PolicyDeny,
				fmt.Sprintf("crafted script rejected before execution for skill %s: %s", skillName, report.Summary),
			)
		}
		return report, fmt.Errorf("crafted script security scan blocked execution: level=%s summary=%s", report.FinalLevel, report.Summary)
	}
	if app != nil && app.skillInstallReviewNeedsConfirmation(report) {
		app.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"craft_tool_prescan",
			report.FinalLevel,
			security.PolicyAudit,
			fmt.Sprintf("crafted script scan recorded risk for skill %s and allowed execution by current policy: %s", skillName, report.Summary),
		)
	}
	if app != nil {
		status := "Generated script security scan passed."
		if report.NeedsUserReview() {
			status = "Generated script security scan recorded risk and allowed execution by current policy."
		}
		app.emitSkillInstallProgress(skillName, "scan-complete", status, report)
	}
	return report, nil
}

// stripScriptCodeFences removes ```lang ... ``` wrappers from LLM output.
func stripScriptCodeFences(s string) string {
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

// saveScript writes the script to the crafted_tools directory and returns
// the full path.
func saveScript(script, language, task string) (string, error) {
	dir := craftedToolsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}
	ext := scriptExtension(language)
	ts := time.Now().Format("20060102_150405")
	safeName := sanitizeFilename(task)
	if len(safeName) > 40 {
		safeName = safeName[:40]
	}
	filename := fmt.Sprintf("%s_%s%s", ts, safeName, ext)
	path := filepath.Join(dir, filename)
	perm := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		perm = 0o755
	}
	if err := os.WriteFile(path, []byte(script), perm); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

// executeScript runs a script file and returns its output.
func executeScript(scriptPath, language, workingDir string, timeout int, runtimes craftRuntimeAvailability) (string, error) {
	return executeScriptWithContext(context.Background(), scriptPath, language, workingDir, timeout, runtimes, nil)
}

func executeScriptWithContext(parent context.Context, scriptPath, language, workingDir string, timeout int, runtimes craftRuntimeAvailability, extraEnv map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd, err := buildCraftExecCommand(ctx, scriptPath, language, runtimes)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(workingDir) != "" {
		cmd.Dir = workingDir
	} else {
		// Default to user-configured dir or ~/.maclaw/workspace.
		cmd.Dir = corelib.EffectiveWorkspaceDir()
	}
	cmd.Env = cskill.BuildCommandEnv(coretool.AppendUTF8Env(os.Environ()), map[string]interface{}{"extra_env": extraEnv})
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)
	runErr := cmd.Run()

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
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("[error] timeout after %ds", timeout))
		}
		return b.String(), runErr
	}
	return b.String(), nil
}

func buildCraftExecCommand(ctx context.Context, scriptPath, language string, runtimes craftRuntimeAvailability) (*exec.Cmd, error) {
	switch normalizeCraftLanguageKind(language) {
	case craftLanguagePython:
		if runtimes.Python == "" {
			return nil, fmt.Errorf("python runtime not found")
		}
		// Use the absolute path from runtimes (resolved via cmd /c where on
		// Windows) to ensure Python is found regardless of which shell is active.
		return exec.CommandContext(ctx, runtimes.Python, scriptPath), nil
	case craftLanguageNode:
		if runtimes.Node == "" {
			return nil, fmt.Errorf("node runtime not found")
		}
		return exec.CommandContext(ctx, runtimes.Node, scriptPath), nil
	case craftLanguagePowerShell:
		if runtimes.PowerShell == "" {
			return nil, fmt.Errorf("powershell runtime not found")
		}
		return exec.CommandContext(ctx, runtimes.PowerShell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath), nil
	default:
		if runtimes.Bash != "" {
			return exec.CommandContext(ctx, runtimes.Bash, scriptPath), nil
		}
		if runtimes.PowerShell != "" {
			return exec.CommandContext(ctx, runtimes.PowerShell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath), nil
		}
		return nil, fmt.Errorf("no shell runtime found")
	}
}

// detectScriptLanguage guesses the best script language based on the task
// description and the current OS.
func detectScriptLanguage(task string) string {
	return detectScriptLanguageWithRuntime(task, detectAvailableScriptRuntimes())
}

func detectScriptLanguageWithRuntime(task string, runtimes craftRuntimeAvailability) string {
	lower := strings.ToLower(task)
	prefersPython := strings.Contains(lower, "python") || strings.Contains(lower, "pip") || strings.Contains(lower, "pandas") || strings.Contains(lower, "requests") || strings.Contains(lower, "csv") || strings.Contains(lower, "excel") || strings.Contains(lower, "xlsx") || strings.Contains(lower, "json") || strings.Contains(lower, "api")
	prefersNode := strings.Contains(lower, "node") || strings.Contains(lower, "npm") || strings.Contains(lower, "javascript") || strings.Contains(lower, "typescript")
	for _, word := range strings.Fields(lower) {
		if word == "js" {
			prefersNode = true
		}
	}
	prefersShell := strings.Contains(lower, "shell") || strings.Contains(lower, "bash") || strings.Contains(lower, "powershell") || strings.Contains(lower, "command") || strings.Contains(lower, "目录") || strings.Contains(lower, "文件")
	if prefersNode && runtimes.Node != "" {
		return "node"
	}
	if prefersPython && runtimes.Python != "" {
		return "python"
	}
	if prefersShell {
		if runtime.GOOS == "windows" && runtimes.PowerShell != "" {
			return "powershell"
		}
		if runtimes.Bash != "" {
			return "bash"
		}
	}
	// Default language selection: on Windows, prefer Node.js over Python
	// because Node.js is discoverable in all shell environments (cmd.exe,
	// sh.exe, PowerShell), while Python may only be in cmd.exe's PATH.
	// This avoids "exit status 9009" errors when craft_tool scripts run
	// in Git Bash where Python is not on PATH.
	if runtime.GOOS == "windows" {
		if runtimes.Node != "" && !prefersShell {
			return "node"
		}
		if runtimes.Python != "" && !prefersShell {
			return "python"
		}
		if runtimes.PowerShell != "" {
			return "powershell"
		}
		if runtimes.Bash != "" {
			return "bash"
		}
	} else {
		if runtimes.Python != "" && !prefersShell {
			return "python"
		}
		if runtimes.Bash != "" {
			return "bash"
		}
	}
	if runtimes.Node != "" {
		return "node"
	}
	return ""
}

func normalizeCraftLanguage(language string) string {
	return normalizeCraftLanguageKind(language).String()
}

// scriptExtension returns the file extension for a script language.
func scriptExtension(language string) string {
	return normalizeCraftLanguageKind(language).ScriptExtension()
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

// generateSkillName creates a short skill name from the task description.
func generateSkillName(task string) string {
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
	switch normalizeCraftLanguageKind(language) {
	case craftLanguagePython:
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("python \"%s\"", scriptPath)
		}
		return fmt.Sprintf("python3 \"%s\"", scriptPath)
	case craftLanguageNode:
		return fmt.Sprintf("node \"%s\"", scriptPath)
	case craftLanguagePowerShell:
		return fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File \"%s\"", scriptPath)
	default:
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File \"%s\"", scriptPath)
		}
		return fmt.Sprintf("bash \"%s\"", scriptPath)
	}
}

func firstNonEmptyCraftText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
