package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ---------------------------------------------------------------------------
// craft_tool — TUI stub: delegates to corelib craft execution
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) toolCraftTool(args map[string]interface{}) string {
	taskDesc, _ := args["task"].(string)
	if taskDesc == "" {
		return "缺少 task 参数"
	}

	// TUI 模式下 craft_tool 需要 LLM 生成脚本，能力有限。
	// 尝试检测语言并提示用户手动提供脚本。
	lang, _ := args["language"].(string)
	if lang == "" {
		lang = tool.DetectScriptLanguage(taskDesc)
	}

	return fmt.Sprintf("craft_tool 在 TUI 模式下功能受限（需要 LLM 动态生成脚本）。\n"+
		"建议：\n"+
		"1. 使用 bash 工具直接执行命令\n"+
		"2. 使用 write_file 写入脚本后用 bash 执行\n"+
		"检测到的推荐语言: %s\n"+
		"任务描述: %s", lang, taskDesc)
}

// ---------------------------------------------------------------------------
// generate_pdf — TUI stub: writes markdown to file (TUI 无 PDF 渲染能力)
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) toolGeneratePDF(args map[string]interface{}) string {
	content, _ := args["content"].(string)
	if content == "" {
		return "缺少 content 参数"
	}
	title, _ := args["title"].(string)
	if title == "" {
		title = "document"
	}

	outDir := corelib.EffectiveWorkspaceDir()

	safeName := tool.SanitizeFilename(title)
	if len(safeName) > 60 {
		safeName = safeName[:60]
	}
	ts := time.Now().Format("20060102_150405")
	outPath := filepath.Join(outDir, fmt.Sprintf("%s_%s.md", safeName, ts))

	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("写入文件失败: %v", err)
	}
	return fmt.Sprintf("TUI 模式不支持 PDF 生成，已将 Markdown 文档保存到: %s", outPath)
}

// ---------------------------------------------------------------------------
// open — 用操作系统默认程序打开文件或网址
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) toolOpen(args map[string]interface{}) string {
	target, _ := args["target"].(string)
	if target == "" {
		return "缺少 target 参数"
	}

	isURL := strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
	if !isURL {
		target = resolvePath(target)
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("打开失败: %s", err.Error())
	}
	go cmd.Wait()

	if isURL {
		return fmt.Sprintf("已用默认浏览器打开: %s", target)
	}
	return fmt.Sprintf("已用默认程序打开: %s", target)
}

// ---------------------------------------------------------------------------
// set_nickname — TUI stub: 保存昵称到配置
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) toolSetNickname(args map[string]interface{}) string {
	nickname, _ := args["nickname"].(string)
	if nickname == "" {
		return "缺少 nickname 参数"
	}

	dataDir := commands.ResolveDataDir()
	store := commands.NewFileConfigStore(dataDir)
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}
	cfg.RemoteNickname = nickname
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Sprintf("保存配置失败: %v", err)
	}
	return fmt.Sprintf("昵称已设置为: %s", nickname)
}

// ---------------------------------------------------------------------------
// list_providers — 列出指定工具的可用服务商
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) toolListProviders(args map[string]interface{}) string {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return "缺少 tool 参数"
	}

	dataDir := commands.ResolveDataDir()
	store := commands.NewFileConfigStore(dataDir)
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}

	// 从配置中查找工具的服务商列表
	var toolCfg corelib.ToolConfig
	switch strings.ToLower(toolName) {
	case "claude":
		toolCfg = cfg.Claude
	case "codex":
		toolCfg = cfg.Codex
	case "gemini":
		toolCfg = cfg.Gemini
	case "cursor":
		toolCfg = cfg.Cursor
	case "opencode":
		toolCfg = cfg.Opencode
	case "iflow":
		toolCfg = cfg.IFlow
	case "kilo":
		toolCfg = cfg.Kilo
	default:
		return fmt.Sprintf("不支持的工具: %s", toolName)
	}

	if len(toolCfg.Models) == 0 {
		return fmt.Sprintf("工具 %s 没有已配置的服务商", toolName)
	}

	var lines []string
	for _, model := range toolCfg.Models {
		if model.ApiKey == "" && model.ModelUrl == "" {
			continue
		}
		isDefault := ""
		if strings.EqualFold(model.ModelName, toolCfg.CurrentModel) {
			isDefault = " [当前默认]"
		}
		lines = append(lines, fmt.Sprintf("  - %s (model_id=%s)%s", model.ModelName, model.ModelId, isDefault))
	}
	if len(lines) == 0 {
		return fmt.Sprintf("工具 %s 没有可用的服务商", toolName)
	}

	return fmt.Sprintf("工具 %s 的可用服务商:\n%s", toolName, strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// get_skill_run — 查询 Skill 运行状态 (TUI stub)
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) toolGetSkillRun(args map[string]interface{}) string {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return "缺少 run_id 参数"
	}
	// TUI 模式下 Skill 运行是同步的，run_id 查询能力有限
	return fmt.Sprintf("TUI 模式下 Skill 运行为同步模式，run_id %s 的运行已完成或不可查询。请使用 run_skill 重新执行。", runID)
}

// ---------------------------------------------------------------------------
// RecordToolOutcome — 记录工具调用结果到 UsageTracker
// ---------------------------------------------------------------------------

func (h *TUIAgentHandler) recordToolOutcome(toolName string, userText string, success bool, followUp string) {
	if h.usageTracker == nil {
		return
	}
	tokens := extractQueryTokens(userText)
	h.usageTracker.RecordOutcome(toolName, tokens, success, followUp)
}

// extractQueryTokens splits user text into BM25-style tokens for usage tracking.
func extractQueryTokens(text string) []string {
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	if len(words) > 5 {
		words = words[:5]
	}
	return words
}

// ensureUsageTrackerSaved persists the usage tracker data if available.
func (h *TUIAgentHandler) ensureUsageTrackerSaved() {
	if h.usageTracker == nil {
		return
	}
	if err := h.usageTracker.Save(); err != nil {
		fmt.Printf("[TUI] usage tracker save failed: %v\n", err)
	}
}
