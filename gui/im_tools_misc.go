package main

// Miscellaneous tools: MCP, skills, memory, templates, config, nickname,
// LLM provider switch, scheduled tasks, AgentNet, audit log, web search/fetch.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	mcputil "github.com/RapidAI/CodeClaw/corelib/mcp"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"gopkg.in/yaml.v3"
)

func (h *IMMessageHandler) toolListMCPTools(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	serverID, _ := args["server_id"].(string)

	// Build []mcputil.ToolEntry from both local and remote MCP sources.
	var entries []mcputil.ToolEntry

	// Local (stdio) MCP servers.
	if h.getLocalMCPManager() == nil {
		_ = h.getLocalMCPManager() // ensure
	}
	if mgr := h.getLocalMCPManager(); mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			for _, t := range ts.Tools {
				entries = append(entries, mcputil.ToolEntry{
					ServerName:   ts.ServerName,
					ServerID:     ts.ServerID,
					SourceType:   "local/stdio",
					HealthStatus: "running",
					ToolName:     t.Name,
					Description:  t.Description,
					InputSchema:  t.InputSchema,
				})
			}
		}
	}

	// Remote (HTTP) MCP servers.
	if registry := h.getMCPRegistry(); registry != nil {
		for _, s := range registry.ListServers() {
			tools := registry.GetServerTools(s.ID)
			for _, t := range tools {
				entries = append(entries, mcputil.ToolEntry{
					ServerName:   s.Name,
					ServerID:     s.ID,
					SourceType:   "remote/HTTP",
					HealthStatus: s.HealthStatus,
					ToolName:     t.Name,
					Description:  t.Description,
					InputSchema:  t.InputSchema,
				})
			}
		}
	}

	if len(entries) == 0 {
		return "没有已注册的 MCP Server"
	}

	filtered := mcputil.FilterTools(entries, query, serverID)
	if len(filtered) == 0 {
		return fmt.Sprintf("没有匹配的工具（共 %d 个工具已注册）", len(entries))
	}

	return mcputil.FormatToolList(filtered, query, serverID)
}

func (h *IMMessageHandler) toolCallMCPTool(args map[string]interface{}) string {
	serverRef, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if serverRef == "" || toolName == "" {
		return "缺少 server_id 或 tool_name 参数；server_id 支持 MCP Server 的 ID 或 Name"
	}

	// Extract tool arguments — handle both map and JSON string from LLM.
	var toolArgs map[string]interface{}
	switch v := args["arguments"].(type) {
	case map[string]interface{}:
		toolArgs = v
	case string:
		if v = strings.TrimSpace(v); v != "" {
			if err := json.Unmarshal([]byte(v), &toolArgs); err != nil {
				return fmt.Sprintf("arguments JSON 解析失败: %s", err.Error())
			}
		}
	}
	if toolArgs == nil {
		toolArgs = map[string]interface{}{}
	}

	if h.getLocalMCPManager() == nil {
		_ = h.getLocalMCPManager() // ensure
	}

	resolvedID, isLocal, err := h.resolveMCPServerRef(serverRef)
	if err != nil {
		// Fallback: LLM 可能把内置工具（如 ssh）误当作 MCP 工具调用。
		// 检查 server_id 或 tool_name 是否匹配已注册的内置工具，
		// 如果匹配则直接转发到内置工具执行，避免 LLM 陷入反复重试循环。
		builtinName := toolName
		if builtinName == "" {
			builtinName = serverRef
		}
		if h.registry != nil {
			for _, candidate := range []string{builtinName, serverRef} {
				if candidate == "" {
					continue
				}
				if tool, ok := h.registry.Get(candidate); ok {
					log.Printf("[call_mcp_tool] builtin fallback: LLM called call_mcp_tool(server_id=%q, tool_name=%q) but %q is a builtin tool — forwarding directly", serverRef, toolName, candidate)
					if tool.Handler != nil {
						return tool.Handler(toolArgs)
					}
					if tool.HandlerProg != nil {
						return tool.HandlerProg(toolArgs, nil)
					}
				}
			}
		}
		return fmt.Sprintf("MCP 调用失败: %s。可先用 list_mcp_tools 查看 Name (ID)", err.Error())
	}

	// Look up the target tool's InputSchema from the tools cache for local
	// validation. If the tool is not in the cache, skip validation (graceful
	// degradation per Req 3.7).
	var inputSchema map[string]interface{}
	if isLocal {
		if mgr := h.getLocalMCPManager(); mgr != nil {
			for _, ts := range mgr.GetAllTools() {
				if ts.ServerID == resolvedID {
					for _, t := range ts.Tools {
						if t.Name == toolName {
							inputSchema = t.InputSchema
							break
						}
					}
					break
				}
			}
		}
	} else {
		if registry := h.getMCPRegistry(); registry != nil {
			for _, t := range registry.GetServerTools(resolvedID) {
				if t.Name == toolName {
					inputSchema = t.InputSchema
					break
				}
			}
		}
	}

	// Validate arguments against the InputSchema before making the RPC call.
	if inputSchema != nil {
		if validationErrs := mcputil.ValidateArgs(inputSchema, toolArgs); len(validationErrs) > 0 {
			var msgs []string
			for _, ve := range validationErrs {
				msgs = append(msgs, ve.Message)
			}
			stdErr := mcputil.NewStandardError(resolvedID, toolName, mcputil.ErrValidation, strings.Join(msgs, "; "))
			return mcputil.FormatForLLM(nil, stdErr)
		}
	}

	if isLocal {
		mgr := h.getLocalMCPManager()
		if mgr == nil {
			return "本地 MCP Manager 未初始化"
		}
		result, err := mgr.CallTool(resolvedID, toolName, toolArgs)
		if err != nil {
			code, msg := mcputil.ClassifyError(err, 0, "")
			stdErr := mcputil.NewStandardError(resolvedID, toolName, code, msg)
			return mcputil.FormatForLLM(nil, stdErr)
		}
		resp, stdErr := mcputil.NormalizeResponse(resolvedID, toolName, result)
		if stdErr != nil {
			return mcputil.FormatForLLM(nil, stdErr)
		}
		return mcputil.FormatForLLM(resp, nil)
	}

	registry := h.getMCPRegistry()
	if registry == nil {
		return "MCP Registry 未初始化"
	}
	result, err := registry.CallTool(resolvedID, toolName, toolArgs)
	if err != nil {
		code, msg := mcputil.ClassifyError(err, 0, "")
		stdErr := mcputil.NewStandardError(resolvedID, toolName, code, msg)
		return mcputil.FormatForLLM(nil, stdErr)
	}
	resp, stdErr := mcputil.NormalizeResponse(resolvedID, toolName, result)
	if stdErr != nil {
		return mcputil.FormatForLLM(nil, stdErr)
	}
	return mcputil.FormatForLLM(resp, nil)
}

func (h *IMMessageHandler) toolListSkills() string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	skills := exec.List()

	var b strings.Builder

	// Show local skills grouped by namespace (Requirement 5.4).
	if len(skills) > 0 {
		// Group skills by publisher namespace.
		type nsGroup struct {
			publisher string
			skills    []NLSkillDefinition
		}
		groupMap := make(map[string]*nsGroup)
		var groupOrder []string
		for _, s := range skills {
			key := s.Publisher
			if key == "" {
				key = "__local__"
			}
			if _, ok := groupMap[key]; !ok {
				groupMap[key] = &nsGroup{publisher: s.Publisher}
				groupOrder = append(groupOrder, key)
			}
			groupMap[key].skills = append(groupMap[key].skills, s)
		}

		// Sort: local skills first, then namespaced groups alphabetically.
		sort.SliceStable(groupOrder, func(i, j int) bool {
			if groupOrder[i] == "__local__" {
				return true
			}
			if groupOrder[j] == "__local__" {
				return false
			}
			return groupOrder[i] < groupOrder[j]
		})

		b.WriteString("=== 本地已注册 Skill ===\n")
		for _, key := range groupOrder {
			g := groupMap[key]
			if key == "__local__" {
				b.WriteString("\n[Local]\n")
			} else {
				b.WriteString(fmt.Sprintf("\n[%s]\n", g.publisher))
			}
			for _, s := range g.skills {
				line := fmt.Sprintf("- %s", s.Name)
				// Show qualified name for namespaced skills
				if s.Publisher != "" {
					line = fmt.Sprintf("- %s:%s", s.Publisher, s.Name)
				}
				// Show directory name alias if different from display name
				if s.DirName != "" && s.DirName != s.Name {
					line += fmt.Sprintf(" (alias: %s)", s.DirName)
				}
				// Show [knowledge] type indicator for knowledge skills
				if s.Type == "knowledge" {
					line += " [knowledge]"
				}
				line += fmt.Sprintf(" [%s]: %s", s.Status, s.Description)
				if s.Source == "hub" {
					line += fmt.Sprintf(" (来源: Hub, trust: %s)", s.TrustLevel)
				} else if s.Source == "file" {
					line += " (来源: 本地文件)"
				}
				if s.UsageCount > 0 {
					successRate := float64(s.SuccessCount) / float64(s.UsageCount) * 100
					line += fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, successRate)
					// Flag skills needing improvement: failure rate > 30% with at least 10 usages
					if s.UsageCount >= 10 {
						failureRate := float64(s.FailureCount) / float64(s.UsageCount)
						if failureRate > 0.30 {
							line += " [needs_improvement]"
						}
					}
				}
				if s.LastError != "" {
					line += fmt.Sprintf(" (最近错误: %s)", s.LastError)
				}
				b.WriteString(line + "\n")
			}
		}
	} else {
		b.WriteString("本地没有已注册的 Skill。\n")
	}

	// If local skills are empty or few, also show Hub recommendations
	if len(skills) < 3 && h.getSkillHubClient() != nil {
		recs := h.getSkillHubClient().GetRecommendations()
		if len(recs) > 0 {
			b.WriteString("\n=== SkillHub 推荐 Skill（可用 install_skill_hub 安装）===\n")
			for _, r := range recs {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (trust: %s, downloads: %d, hub: %s)\n",
					r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.HubURL))
			}
		} else {
			b.WriteString("\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n")
		}
	}

	return b.String()
}

func (h *IMMessageHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "缺少 query 参数"
	}

	// Use the unified HubClient.SearchAll() which searches all three sources
	// (SkillHub + ClawHub + GitHub) through a single code path. This ensures
	// the LLM tool call sees the same results as the GUI/TUI search panels.
	//
	// Resolve the SkillHub base URL from HubCenter (same as SkillHubClient).
	// If resolution fails, SearchAll still queries ClawHub and GitHub.
	hubURL := ""
	if h.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if base, _, err := h.app.resolveHubCenterBaseURLCached(ctx, &http.Client{Timeout: 10 * time.Second}); err == nil {
			hubURL = base
		}
	}
	results := cskill.DefaultHubClient().SearchAll(context.Background(), hubURL, query)
	if len(results) == 0 {
		return fmt.Sprintf("在 SkillHub/ClawHub/GitHub 上均未找到与 %q 相关的 Skill", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 个 Skill：\n", len(results)))
	for _, r := range results {
		switch r.Source {
		case "skillhub":
			b.WriteString(fmt.Sprintf("- [SkillHub] ID: %s | %s: %s (trust: %s, downloads: %d)\n  安装: manage_skill(action=\"install\", skill_id=\"%s\", hub_url=\"%s\")\n",
				r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.ID, hubURL))
		case "clawhub":
			b.WriteString(fmt.Sprintf("- [ClawHub] ID: %s | %s: %s (downloads: %d)\n  安装: manage_skill(action=\"install\", skill_id=\"%s\", hub_url=\"%s\")\n",
				r.ID, r.Name, r.Description, r.Downloads, r.ID, cskill.ClawHubMirrorURL))
		case "github":
			b.WriteString(fmt.Sprintf("- [GitHub] %s: %s (repo: %s)\n  安装: manage_skill(action=\"install\", skill_id=\"%s\", hub_url=\"github\", install_ref=%q)\n",
				r.Name, r.Description, r.RepoURL, r.Name, r.InstallRef))
		default:
			b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", r.Source, r.Name, r.Description))
		}
	}
	return b.String()
}

func (h *IMMessageHandler) toolInstallSkillHub(args map[string]interface{}) string {
	skillID, _ := args["skill_id"].(string)
	hubURL, _ := args["hub_url"].(string)
	if skillID == "" {
		return "缺少 skill_id 参数"
	}
	if hubURL == "" {
		return "缺少 hub_url 参数"
	}

	if h.getSkillExecutor() == nil {
		return "Skill Executor 未初始化"
	}

	ctx := context.Background()
	var entry *corelib.NLSkillEntry
	var err error
	var sourceLabel string
	var stagingDir string // non-empty when files were extracted to staging

	switch {
	case strings.EqualFold(hubURL, "github"):
		installRef, _ := args["install_ref"].(string)
		if installRef == "" {
			return "GitHub Skill 安装缺少 install_ref 参数"
		}
		entry, err = cskill.DefaultHubClient().DownloadGitHub(ctx, installRef)
		sourceLabel = "GitHub"

	case strings.Contains(hubURL, "clawhub") || strings.Contains(hubURL, "clawhub-mirror"):
		entry, err = cskill.DefaultHubClient().DownloadClawHub(ctx, skillID)
		sourceLabel = "ClawHub"

	default:
		if h.getSkillHubClient() == nil {
			h.ensureSkillHubClient()
		}
		if h.getSkillHubClient() == nil {
			return "SkillHub 客户端未初始化"
		}
		// Prepare staging directory for file-backed skills.
		stagingDir, err = cskill.PrepareStagingDir(skillID)
		if err != nil {
			return fmt.Sprintf("创建临时目录失败: %s", err.Error())
		}
		// Download and extract files to staging dir (not final location).
		entry, err = h.getSkillHubClient().InstallToDir(ctx, skillID, hubURL, stagingDir)
		sourceLabel = "SkillHub"
	}

	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return fmt.Sprintf("安装失败 (%s): %s", sourceLabel, err.Error())
	}

	// Security review: pattern scan (hard floor) + agent scan (upgrade only).
	// Developer mode: skip entirely.
	if !h.isSecurityDeveloperMode() {
		// Determine the directory to scan: staging dir if available, else entry.SkillDir.
		scanDir := stagingDir
		if scanDir == "" {
			scanDir = entry.SkillDir
		}

		scanner := NewSkillSecurityScanner(h.app, nil)
		scanReport := scanner.ScanStaged(ctx, entry, scanDir, func(status string) {
			log.Printf("[skill-install] %s: %s", entry.Name, status)
		})

		if scanReport.IsDangerous() || scanReport.NeedsUserReview() {
			platform := ""
			if h.currentLoopCtx != nil {
				platform = h.currentLoopCtx.Platform
			}
			confirmCtx := context.Background()
			if h.currentLoopCtx != nil {
				var cancel context.CancelFunc
				confirmCtx, cancel = h.currentLoopCtx.Context()
				defer cancel()
			}

			allFactors := scanReport.PatternAssessment.Factors
			for _, f := range scanReport.Findings {
				allFactors = append(allFactors, f.Description)
			}

			confirmed := h.confirmCriticalRiskSkill(
				confirmCtx, entry.Name, hubURL, allFactors, platform, h.lastUserID,
			)

			if !confirmed {
				cskill.CleanupStaging(stagingDir) // reject → clean up staging
				riskLevel := security.RiskHigh
				if scanReport.IsDangerous() {
					riskLevel = security.RiskCritical
				}
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     "hub_skill_install",
						RiskLevel:    riskLevel,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("user rejected skill %s: %s", entry.Name, scanReport.Summary),
					})
				}
				return FormatScanReportForUser(scanReport, entry.Name) +
					fmt.Sprintf("\n⚠️ Skill %q 已拒绝安装。", entry.Name)
			}

			// User confirmed override.
			if h.getAuditLog() != nil {
				_ = h.getAuditLog().Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillInstall,
					ToolName:     "hub_skill_install",
					RiskLevel:    scanReport.FinalLevel,
					PolicyAction: security.PolicyUserOverride,
					Result:       fmt.Sprintf("user confirmed skill %s, scanned_by=%s, level=%s", entry.Name, scanReport.ScannedBy, scanReport.FinalLevel),
				})
			}
		}
	}

	// Commit staging → final location.
	if stagingDir != "" {
		finalDir, commitErr := cskill.CommitStaging(stagingDir, entry.Name)
		if commitErr != nil {
			cskill.CleanupStaging(stagingDir)
			return fmt.Sprintf("安装失败（提交到最终目录）: %s", commitErr.Error())
		}
		entry.SkillDir = finalDir
	}

	// Normalize downloaded skills before registration: repair portable paths,
	// remove packaging-only backups, and reload the disk definition so runtime
	// uses the improved version rather than the raw download snapshot.
	entry = h.app.normalizeInstalledSkill(entry)

	// Register locally.
	if err := h.getSkillExecutor().Register(*entry); err != nil {
		return fmt.Sprintf("注册失败: %s", err.Error())
	}
	go h.appInstallSkillDepsIfMissing(entry.SkillDir, entry.Name)

	// Refresh skill BM25 index so the router picks up the new skill
	// for skill-aware routing (enrichRunSkillDescription).
	if h.getAppToolRouter() != nil {
		h.getAppToolRouter().RefreshSkillIndex()
	}

	// Audit log
	_ = h.getAuditLog() // ensure
	if h.getAuditLog() != nil {
		_ = h.getAuditLog().Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    security.RiskLow,
			PolicyAction: security.PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s (%s) from %s, trust: %s", entry.Name, skillID, hubURL, entry.TrustLevel),
		})
	}

	// Auto-run: default to true unless explicitly set to false.
	autoRun := true
	if v, ok := args["auto_run"]; ok {
		switch val := v.(type) {
		case bool:
			autoRun = val
		case string:
			autoRun = strings.EqualFold(val, "true")
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("✅ 已成功安装 Skill「%s」\n描述: %s\n来源: %s\n信任等级: %s\n",
		entry.Name, entry.Description, hubURL, entry.TrustLevel))

	if autoRun {
		b.WriteString(fmt.Sprintf("\n正在立即执行 Skill「%s」...\n", entry.Name))
		// Pass user-supplied run arguments (input, output, args, etc.) to the
		// skill runner so the auto-run after install actually has the parameters
		// the user intended. Previously this passed nil, causing skills that
		// require parameters to fail on auto-run every time.
		runID, err := h.app.RunNLSkillAsync(entry.Name, buildRunSkillArgs(args))
		if err != nil {
			b.WriteString(fmt.Sprintf("执行启动失败: %s", err.Error()))
		} else {
			waitDuration := normalizeSkillRunWaitSeconds(args["wait_seconds"])
			status, statusErr := waitForSkillRunnerSnapshot(h.getSkillRunner(), runID, waitDuration)
			if statusErr != nil {
				b.WriteString(fmt.Sprintf("已启动（run_id=%s），但读取状态失败: %s", runID, statusErr.Error()))
			} else {
				appendSkillRunSummary(&b, status, runID)
			}
		}
	} else {
		b.WriteString(fmt.Sprintf("\n可以使用 manage_skill(action=\"run\", name=\"%s\") 执行", entry.Name))
	}

	return b.String()
}

func appendSkillRunSummary(b *strings.Builder, status *SkillRunStatus, runID string) {
	if b == nil {
		return
	}
	b.WriteString("## 运行信息\n")
	b.WriteString(fmt.Sprintf("- run_id: %s\n", runID))
	if status == nil {
		b.WriteString("- status: unknown\n")
		return
	}
	b.WriteString(fmt.Sprintf("- skill: %s\n", status.Skill))
	b.WriteString(fmt.Sprintf("- status: %s\n", status.Status))
	// session_ready: explicit signal for callers to know if session_id is available.
	// Only emit when the skill actually involves sessions to avoid confusing the
	// LLM with "session_ready: false" on pure-bash skills like weather-query.
	sessionReady := status.Session != nil && strings.TrimSpace(status.Session.SessionID) != ""
	if sessionReady || status.Status == "running" {
		b.WriteString(fmt.Sprintf("- session_ready: %v\n", sessionReady))
	}
	if status.Summary.CurrentStep != "" {
		b.WriteString(fmt.Sprintf("- current_step: %s (%s)\n", status.Summary.CurrentStep, firstNonEmptySkillRunStatus(status.Summary.CurrentStepStatus, "running")))
	}
	if status.Summary.LastCompletedStep != "" {
		b.WriteString(fmt.Sprintf("- last_completed_step: %s\n", status.Summary.LastCompletedStep))
	}
	if status.Summary.NeedsArtifactVerification {
		b.WriteString("## 结果说明\n")
		b.WriteString("- 这是一个仅提供 SKILL.md 指导的 skill；当前结果只表示脚本已生成并执行。\n")
		if status.Summary.ArtifactPath != "" {
			b.WriteString(fmt.Sprintf("- 目标产物: %s\n", status.Summary.ArtifactPath))
			switch status.Summary.ArtifactStatus {
			case "verified":
				b.WriteString("- 产物已自动验证存在。\n")
			case "missing":
				b.WriteString("- 目标产物尚未生成到该路径；当前不能算成功交付。\n")
			default:
				b.WriteString("- 宿主尚未完成产物验证，请继续观察。\n")
			}
		} else {
			b.WriteString("- 宿主尚未定位目标产物路径；如果目标是 PPT/PDF，请继续检查输出文件。\n")
		}
	}
	if status.Summary.ArtifactPath != "" {
		b.WriteString(fmt.Sprintf("- artifact_path: %s\n", status.Summary.ArtifactPath))
	}
	if status.Summary.ArtifactStatus != "" {
		b.WriteString(fmt.Sprintf("- artifact_status: %s\n", status.Summary.ArtifactStatus))
	}
	if status.Summary.LastErrorSnippet != "" {
		b.WriteString(fmt.Sprintf("- last_error: %s\n", status.Summary.LastErrorSnippet))
	}
	// Step-level progress: show each step's status AND output for visibility
	// into intermediate execution stages (addresses P1-5 from LibTV report).
	// Previously only status/duration/error were shown; step.Output was stored
	// but never returned to the LLM, causing "session_ready: false" confusion
	// for non-session skills (e.g. weather-query) where the actual result is
	// in stdout.
	//
	// Budget: cap total step output to ~4096 chars to avoid bloating LLM context
	// when a skill has many steps. Individual steps are capped at 2048 chars.
	const maxStepOutputLen = 2048
	const maxTotalOutputLen = 4096
	totalOutputLen := 0
	if len(status.Steps) > 0 {
		b.WriteString(fmt.Sprintf("- total_steps: %d\n", len(status.Steps)))
		b.WriteString("## 步骤进度\n")
		for i, step := range status.Steps {
			label := step.Name
			if label == "" {
				label = step.Action
			}
			line := fmt.Sprintf("- step %d: %s → %s", i+1, label, step.Status)
			if step.DurationMs > 0 {
				line += fmt.Sprintf(" (%dms)", step.DurationMs)
			}
			if step.Error != "" {
				errSnippet := step.Error
				if len(errSnippet) > 80 {
					errSnippet = errSnippet[:80] + "..."
				}
				line += fmt.Sprintf(" [error: %s]", errSnippet)
			}
			b.WriteString(line + "\n")
			// Return step output so the LLM can see actual results (e.g.
			// weather data, API responses) instead of just "success".
			if stepOut := strings.TrimSpace(step.Output); stepOut != "" && totalOutputLen < maxTotalOutputLen {
				remaining := maxTotalOutputLen - totalOutputLen
				limit := maxStepOutputLen
				if remaining < limit {
					limit = remaining
				}
				runes := []rune(stepOut)
				b.WriteString("```\n")
				if len(runes) > limit {
					b.WriteString(string(runes[:limit]))
					b.WriteString("\n... (truncated)\n")
					totalOutputLen += limit
				} else {
					b.WriteString(stepOut)
					b.WriteString("\n")
					totalOutputLen += len(runes)
				}
				b.WriteString("```\n")
			}
		}
	}
	if status.Session != nil {
		b.WriteString("## 会话信息\n")
		if strings.TrimSpace(status.Session.SessionID) != "" {
			b.WriteString(fmt.Sprintf("- session_id: %s\n", status.Session.SessionID))
		}
		if strings.TrimSpace(status.Session.Tool) != "" {
			b.WriteString(fmt.Sprintf("- tool: %s\n", status.Session.Tool))
		}
		if strings.TrimSpace(status.Session.ProjectPath) != "" {
			b.WriteString(fmt.Sprintf("- project_path: %s\n", status.Session.ProjectPath))
		}
		if strings.TrimSpace(status.Session.Status) != "" {
			b.WriteString(fmt.Sprintf("- session_status: %s\n", status.Session.Status))
		}
		if strings.TrimSpace(status.Session.ResumeSessionID) != "" {
			b.WriteString(fmt.Sprintf("- resume_session_id: %s\n", status.Session.ResumeSessionID))
		}
	}
	// Session progress: show what the session's internal AI agent is doing.
	if status.SessionProgress != nil {
		sp := status.SessionProgress
		b.WriteString("## 会话内部进度\n")
		b.WriteString(fmt.Sprintf("- session_status: %s\n", sp.SessionStatus))
		if sp.CurrentTask != "" {
			b.WriteString(fmt.Sprintf("- current_action: %s\n", sp.CurrentTask))
		}
		if sp.ProgressSummary != "" {
			b.WriteString(fmt.Sprintf("- progress: %s\n", sp.ProgressSummary))
		}
		if sp.LastResult != "" {
			b.WriteString(fmt.Sprintf("- last_result: %s\n", sp.LastResult))
		}
		if sp.LastCommand != "" {
			b.WriteString(fmt.Sprintf("- last_command: %s\n", sp.LastCommand))
		}
		if sp.WaitingForUser {
			b.WriteString("- ⚠️ 会话内部 agent 正在等待输入\n")
		}
		b.WriteString(fmt.Sprintf("- poll_count: %d\n", sp.PollCount))
		if sp.UpdatedAt != "" {
			b.WriteString(fmt.Sprintf("- updated_at: %s\n", sp.UpdatedAt))
		}
	}
	b.WriteString("## 下一步\n")
	if sessionReady && status.SessionProgress != nil {
		b.WriteString("- session 内部进度已自动监控，继续调用 get_skill_run(run_id) 即可查看最新状态。\n")
	} else if sessionReady {
		b.WriteString("- session_id 已就绪；先调用 get_skill_run(run_id) 确认当前状态，再使用 query_session / send_and_observe 观察会话输出。\n")
	} else if status.Status == "running" {
		b.WriteString("- 使用 get_skill_run(run_id) 继续观察执行进度。\n")
	} else if status.Status == "success" || status.Status == "failed" {
		// Skill has finished — step outputs are already shown above.
		// No need to direct the LLM to poll or wait for session_ready.
		if status.Status == "failed" {
			// Include action hint from the last failed step to guide the LLM
			// on what to do next (retry, patch, search alternative, etc.).
			for i := len(status.Steps) - 1; i >= 0; i-- {
				if status.Steps[i].Status == "failed" && status.Steps[i].Error != "" {
					ce := cskill.ClassifyStepError(
						status.Steps[i].ExitCode,
						status.Steps[i].Output,
						status.Steps[i].Error,
						status.Steps[i].CommandResolved,
					)
					if ce.ActionHint != "" {
						b.WriteString(fmt.Sprintf("- 建议操作: %s\n", ce.ActionHint))
					}
					if ce.Retryable {
						b.WriteString("- 此错误可重试（transient error）\n")
					}
					break
				}
			}
			if status.SelfRepairPending {
				b.WriteString("- ⚙️ 系统正在自动修复此 Skill，建议等待 10 秒后使用 manage_skill(action=\"status\", run_id=\"" + runID + "\") 检查修复状态，再重试执行。\n")
			} else {
				b.WriteString("- Skill 执行失败，步骤输出已在上方显示。请根据建议操作决定下一步。\n")
			}
		} else {
			b.WriteString("- Skill 已执行完毕，步骤输出已在上方显示。请直接基于输出内容回复用户。\n")
		}
	} else {
		b.WriteString("- 使用 get_skill_run(run_id) 查看最终结果。\n")
	}
}

func firstNonEmptySkillRunStatus(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func emitSkillRunProgress(onProgress tool.ProgressCallback, status *SkillRunStatus) {
	if onProgress == nil || status == nil {
		return
	}
	switch {
	case status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "":
		onProgress("🚀 Skill 已绑定会话，可继续观察输出...")
	case status.Status == "success":
		onProgress("✅ Skill 已执行完成，正在整理结果...")
	case status.Status == "failed":
		onProgress("❌ Skill 执行失败，正在整理错误摘要...")
	case status.Summary.CurrentStep != "":
		onProgress(fmt.Sprintf("⏳ Skill 正在执行步骤：%s", status.Summary.CurrentStep))
	default:
		onProgress("⏳ Skill 正在运行，等待状态快照...")
	}
}

func normalizeSkillRunWaitSeconds(raw interface{}) time.Duration {
	seconds := 2.0
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			seconds = v
		}
	case int:
		if v > 0 {
			seconds = float64(v)
		}
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds * float64(time.Second))
}

func waitForSkillRunnerSnapshot(runner *SkillRunner, runID string, timeout time.Duration) (*SkillRunStatus, error) {
	if runner == nil {
		return nil, fmt.Errorf("skill runner not initialized")
	}
	deadline := time.Now().Add(timeout)
	// Track whether we've seen any step progress to decide if we should
	// extend the wait for session_id binding.
	sawStepProgress := false
	for {
		status, err := runner.GetRunStatus(runID)
		if err != nil {
			return nil, err
		}
		if status != nil {
			if status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "" {
				return status, nil
			}
			if status.Status != "running" {
				return status, nil
			}
			if status.Summary.ArtifactStatus == "verified" || status.Summary.ArtifactStatus == "missing" {
				return status, nil
			}
			for _, step := range status.Steps {
				if step.Status == "success" || step.Status == "failed" {
					// A step completed but session_id not yet bound — extend
					// deadline by up to 10s to give create_session time to
					// propagate the session meta. This addresses P0-1 where
					// run_skill returns session_id=null because the snapshot
					// was taken before SetRunSessionMeta completed.
					if !sawStepProgress {
						sawStepProgress = true
						extended := time.Now().Add(10 * time.Second)
						if extended.After(deadline) {
							deadline = extended
						}
					}
					// If session is still not bound after extension, return
					// what we have so the caller can poll via get_skill_run.
					if time.Now().After(deadline) {
						return status, nil
					}
					break // check again after sleep
				}
			}
		}
		if time.Now().After(deadline) {
			return status, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *IMMessageHandler) toolRunSkill(args map[string]interface{}, onProgress tool.ProgressCallback) string {
	h.ensureSkillRunner()
	runner := h.getSkillRunner()
	if runner == nil {
		return "Skill Runner 未初始化"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缺少 name 参数"
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf("🚀 正在启动 Skill「%s」...", name))
	}
	waitDuration := normalizeSkillRunWaitSeconds(args["wait_seconds"])
	runID, err := runner.StartRun(name, buildRunSkillArgs(args))
	if err != nil {
		return fmt.Sprintf("Skill 启动失败: %s", err.Error())
	}
	if onProgress != nil {
		onProgress("⏳ Skill 已启动，正在等待状态快照...")
	}
	status, err := waitForSkillRunnerSnapshot(runner, runID, waitDuration)
	if err != nil {
		return fmt.Sprintf("Skill 已启动，但读取状态失败: %s（run_id=%s）", err.Error(), runID)
	}
	emitSkillRunProgress(onProgress, status)
	var b strings.Builder
	b.WriteString("✅ Skill 已启动\n")
	appendSkillRunSummary(&b, status, runID)
	return strings.TrimRight(b.String(), "\n")
}

func (h *IMMessageHandler) toolGetSkillRun(args map[string]interface{}) string {
	h.ensureSkillRunner()
	runner := h.getSkillRunner()
	if runner == nil {
		return "Skill Runner 未初始化"
	}
	runID, _ := args["run_id"].(string)
	if strings.TrimSpace(runID) == "" {
		return "缺少 run_id 参数"
	}
	waitDuration := normalizeSkillRunWaitSeconds(args["wait_seconds"])
	status, err := waitForSkillRunnerSnapshot(runner, runID, waitDuration)
	if err != nil {
		return fmt.Sprintf("读取 Skill 状态失败: %s（run_id=%s）", err.Error(), runID)
	}
	var b strings.Builder
	b.WriteString("🔎 Skill 状态查询结果\n")
	appendSkillRunSummary(&b, status, runID)
	return strings.TrimRight(b.String(), "\n")
}

// buildRunSkillArgsExcludeKeys lists manage_skill parameters that are NOT
// passed to the skill runner. Everything else is transparently forwarded,
// so new run-time parameters (user_prompt, future additions) work without
// touching this function.
var buildRunSkillArgsExcludeKeys = map[string]bool{
	"action":       true, // manage_skill dispatch key
	"query":        true, // search action
	"skill_id":     true, // install action
	"hub_url":      true, // install action
	"install_ref":  true, // install action (GitHub source)
	"auto_run":     true, // install action
	"wait_seconds": true, // handled by caller before StartRun
	"run_id":       true, // status action
	"auto_fix":     true, // validate action
	"force":        true, // upload action
	"name":         true, // consumed by caller to resolve skill
	"skill_name":   true, // patch action
	"mode":         true, // patch action
	"step_index":   true, // patch step mode
	"field":        true, // patch step mode
	"value":        true, // patch step mode
}

func buildRunSkillArgs(args map[string]interface{}) map[string]interface{} {
	runArgs := map[string]interface{}{}
	for k, v := range args {
		if buildRunSkillArgsExcludeKeys[k] {
			continue
		}
		runArgs[k] = v
	}
	if len(runArgs) == 0 {
		return nil
	}
	return runArgs
}

// stringVal extracts a string value from a map, returning "" if the key is
// missing or not a string.
func stringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// toolManageSkill dispatches the merged manage_skill tool to individual handlers.
func (h *IMMessageHandler) toolManageSkill(args map[string]interface{}, onProgress tool.ProgressCallback) string {
	action := stringVal(args, "action")
	switch action {
	case "list":
		return h.toolListSkills()
	case "search":
		return h.toolSearchSkillHub(args)
	case "install":
		return h.toolInstallSkillHub(args)
	case "run":
		return h.toolRunSkill(args, onProgress)
	case "status":
		return h.toolGetSkillRun(args)
	case "upload":
		return h.toolUploadSkill(args)
	case "validate":
		return h.toolValidateSkill(args)
	case "patch":
		return h.toolPatchSkill(args)
	case "history":
		return h.toolSkillPatchHistory(args)
	default:
		return cskill.ManageSkillUnknownActionError(action)
	}
}

// patchRecord represents a single patch applied to a skill definition file.
type patchRecord struct {
	Timestamp string `json:"timestamp"`
	Find      string `json:"find"`
	Replace   string `json:"replace"`
	Reason    string `json:"reason,omitempty"`
}

// toolPatchSkill performs a targeted find-and-replace on a skill's YAML/JSON
// definition file. It validates the result before saving and appends an audit
// record to .patches.json in the skill directory.
func (h *IMMessageHandler) toolPatchSkill(args map[string]interface{}) string {
	skillName := stringVal(args, "skill_name")
	if skillName == "" {
		return "缺少 skill_name 参数"
	}

	// Dispatch by mode: "text" (default, find-and-replace) or "step" (structured).
	mode := stringVal(args, "mode")
	if mode == "" {
		mode = "text"
	}

	switch mode {
	case "step":
		return h.toolPatchSkillStructured(skillName, args)
	case "text":
		return h.toolPatchSkillText(skillName, args)
	default:
		return fmt.Sprintf("未知 patch mode: %q（支持: text/step）", mode)
	}
}

func (h *IMMessageHandler) toolPatchSkillText(skillName string, args map[string]interface{}) string {
	find := stringVal(args, "find")
	if find == "" {
		return "text patch requires find"
	}
	replaceStr := stringVal(args, "replace")
	reason := stringVal(args, "reason")

	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor is not initialized"
	}

	exec.mu.RLock()
	skills := exec.loadSkills()
	exec.mu.RUnlock()

	var target *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].MatchesName(skillName) {
			target = &skills[i]
			break
		}
	}
	if target == nil {
		return fmt.Sprintf("skill %q not found", skillName)
	}
	if target.SkillDir == "" {
		return fmt.Sprintf("skill %q has no backing directory", skillName)
	}

	defPath, defFormat := findSkillDefinitionFile(target.SkillDir)
	if defPath == "" {
		return fmt.Sprintf("skill definition not found in %s", target.SkillDir)
	}

	content, err := os.ReadFile(defPath)
	if err != nil {
		return fmt.Sprintf("read skill definition failed: %s", err.Error())
	}

	count := strings.Count(string(content), find)
	if count == 0 {
		return fmt.Sprintf("no match found in skill definition: %q", find)
	}
	if count > 1 {
		return fmt.Sprintf("ambiguous match: found %d occurrences of %q", count, find)
	}

	modified := strings.Replace(string(content), find, replaceStr, 1)
	if validationErr := validateSkillFileContent([]byte(modified), defFormat); validationErr != "" {
		return fmt.Sprintf("patched skill definition is invalid; refused to save: %s", validationErr)
	}

	if err := fileutil.AtomicWriteFile(defPath, []byte(modified), 0644); err != nil {
		return fmt.Sprintf("save skill definition failed: %s", err.Error())
	}

	log.Printf("[skill-patch] patched %s in %s", skillName, defPath)
	if auditErr := appendPatchRecord(target.SkillDir, patchRecord{
		Timestamp: time.Now().Format(time.RFC3339),
		Find:      find,
		Replace:   replaceStr,
		Reason:    reason,
	}); auditErr != nil {
		log.Printf("[skill-patch] warning: failed to write audit trail: %v", auditErr)
	}

	return fmt.Sprintf("skill %q patched successfully", skillName)
}

// toolPatchSkillStructured performs a structured modification of a specific
// step field in the skill definition. This is more robust than text-level
// find-and-replace because it operates on the parsed YAML structure, not
// raw text — immune to formatting differences (indentation, quoting).
func (h *IMMessageHandler) toolPatchSkillStructured(skillName string, args map[string]interface{}) string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor is not initialized"
	}

	exec.mu.RLock()
	skills := exec.loadSkills()
	exec.mu.RUnlock()

	var target *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].MatchesName(skillName) {
			target = &skills[i]
			break
		}
	}
	if target == nil {
		return fmt.Sprintf("skill %q not found", skillName)
	}
	if target.SkillDir == "" {
		return fmt.Sprintf("skill %q has no backing directory", skillName)
	}

	defPath, defFormat := findSkillDefinitionFile(target.SkillDir)
	if defPath == "" {
		return fmt.Sprintf("structured patch requires skill.yaml or skill.yml in %s", target.SkillDir)
	}

	content, err := os.ReadFile(defPath)
	if err != nil {
		return fmt.Sprintf("read skill definition failed: %s", err.Error())
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Sprintf("parse skill.yaml/skill.yml failed: %s", err.Error())
	}

	stepIdxRaw, ok := args["step_index"]
	if !ok {
		return "structured patch requires step_index"
	}
	stepIdx := 0
	switch v := stepIdxRaw.(type) {
	case float64:
		stepIdx = int(v)
	case int:
		stepIdx = v
	case string:
		fmt.Sscanf(v, "%d", &stepIdx)
	default:
		return fmt.Sprintf("invalid step_index type: %T", stepIdxRaw)
	}

	field := stringVal(args, "field")
	if field == "" {
		return "structured patch requires field"
	}
	value := stringVal(args, "value")
	reason := stringVal(args, "reason")

	stepsRaw, ok := doc["steps"]
	if !ok {
		return fmt.Sprintf("%s has no steps field", filepath.Base(defPath))
	}
	steps, ok := stepsRaw.([]interface{})
	if !ok {
		return fmt.Sprintf("%s steps field has invalid format", filepath.Base(defPath))
	}
	if stepIdx < 0 || stepIdx >= len(steps) {
		return fmt.Sprintf("step_index %d out of range (steps=%d)", stepIdx, len(steps))
	}
	step, ok := steps[stepIdx].(map[string]interface{})
	if !ok {
		return fmt.Sprintf("step %d has invalid format", stepIdx)
	}

	paramsFields := map[string]bool{"command": true, "working_dir": true, "timeout": true}
	if paramsFields[field] {
		params, ok := step["params"].(map[string]interface{})
		if !ok {
			params = make(map[string]interface{})
			step["params"] = params
		}
		oldValue := fmt.Sprintf("%v", params[field])
		params[field] = value
		log.Printf("[skill-patch-step] step %d params.%s: %q -> %q", stepIdx, field, oldValue, value)
	} else {
		oldValue := fmt.Sprintf("%v", step[field])
		step[field] = value
		log.Printf("[skill-patch-step] step %d .%s: %q -> %q", stepIdx, field, oldValue, value)
	}

	var modified []byte
	modified, err = yaml.Marshal(doc)
	if err != nil {
		return fmt.Sprintf("serialize skill definition failed: %s", err.Error())
	}
	if validationErr := validateSkillFileContent(modified, defFormat); validationErr != "" {
		return fmt.Sprintf("patched skill definition is invalid: %s", validationErr)
	}

	if err := fileutil.AtomicWriteFile(defPath, modified, 0644); err != nil {
		return fmt.Sprintf("save skill definition failed: %s", err.Error())
	}

	log.Printf("[skill-patch-step] patched %s step %d field %s in %s", skillName, stepIdx, field, defPath)

	if auditErr := appendPatchRecord(target.SkillDir, patchRecord{
		Timestamp: time.Now().Format(time.RFC3339),
		Find:      fmt.Sprintf("step[%d].%s", stepIdx, field),
		Replace:   value,
		Reason:    reason,
	}); auditErr != nil {
		log.Printf("[skill-patch-step] warning: failed to write audit trail: %v", auditErr)
	}

	return fmt.Sprintf("skill %q step %d field %s updated to %q", skillName, stepIdx, field, value)
}

// toolSkillPatchHistory returns the patch history for a skill from .patches.json.
func (h *IMMessageHandler) toolSkillPatchHistory(args map[string]interface{}) string {
	skillName := stringVal(args, "skill_name")
	if skillName == "" {
		return "缺少 skill_name 参数"
	}

	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}

	exec.mu.RLock()
	skills := exec.loadSkills()
	exec.mu.RUnlock()

	var target *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].MatchesName(skillName) {
			target = &skills[i]
			break
		}
	}
	if target == nil {
		return fmt.Sprintf("未找到 Skill「%s」", skillName)
	}
	if target.SkillDir == "" {
		return fmt.Sprintf("Skill「%s」没有关联的目录", skillName)
	}

	patchesPath := filepath.Join(target.SkillDir, ".patches.json")
	data, err := os.ReadFile(patchesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Skill「%s」没有 patch 历史记录", skillName)
		}
		return fmt.Sprintf("读取 patch 历史失败: %s", err.Error())
	}

	var records []patchRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Sprintf("解析 patch 历史失败: %s", err.Error())
	}

	if len(records) == 0 {
		return fmt.Sprintf("Skill「%s」没有 patch 历史记录", skillName)
	}

	// Return in reverse chronological order.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Skill「%s」Patch 历史（共 %d 条）===\n", skillName, len(records)))
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		b.WriteString(fmt.Sprintf("\n[%s]\n", r.Timestamp))
		b.WriteString(fmt.Sprintf("  find:    %s\n", r.Find))
		b.WriteString(fmt.Sprintf("  replace: %s\n", r.Replace))
		if r.Reason != "" {
			b.WriteString(fmt.Sprintf("  reason:  %s\n", r.Reason))
		}
	}
	return b.String()
}

// findSkillDefinitionFile locates the skill definition file in a skill directory.
// Returns the path and format ("yaml" or "json"), or empty strings if not found.
func findSkillDefinitionFile(skillDir string) (string, string) {
	for _, candidate := range []struct {
		name   string
		format string
	}{
		{name: "skill.yaml", format: "yaml"},
		{name: "skill.yml", format: "yaml"},
	} {
		defPath := filepath.Join(skillDir, candidate.name)
		if _, err := os.Stat(defPath); err == nil {
			return defPath, candidate.format
		}
	}
	return "", ""
}

// validateSkillFileContent checks that the given content is valid YAML or JSON
// depending on the format. Returns an empty string on success, or an error
// description on failure.
func validateSkillFileContent(data []byte, format string) string {
	switch format {
	case "yaml":
		if _, err := cskill.ParseSkillDefinitionFile(data, format); err != nil {
			return fmt.Sprintf("skill definition validation failed: %s", err.Error())
		}
	default:
		return fmt.Sprintf("unknown file format: %s", format)
	}
	return ""
}

// appendPatchRecord appends a patch record to the .patches.json audit trail
// in the skill directory. Creates the file if it doesn't exist.
func appendPatchRecord(skillDir string, record patchRecord) error {
	patchesPath := filepath.Join(skillDir, ".patches.json")

	var records []patchRecord
	if data, err := os.ReadFile(patchesPath); err == nil {
		// File exists — parse existing records.
		if jsonErr := json.Unmarshal(data, &records); jsonErr != nil {
			// Corrupted file — start fresh but log the issue.
			log.Printf("[skill-patch] warning: corrupted .patches.json, starting fresh: %v", jsonErr)
			records = nil
		}
	}

	records = append(records, record)

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal patch records: %w", err)
	}

	return fileutil.AtomicWriteFile(patchesPath, data, 0644)
}

// toolUploadSkill uploads a local skill to SkillMarket.
func (h *IMMessageHandler) toolUploadSkill(args map[string]interface{}) string {
	name := stringVal(args, "name")
	if name == "" {
		return "缺少 name 参数（要上传的 Skill 名称）"
	}

	// Quality gate: prevent uploading untested or consistently failing skills.
	// Skip when force=true (user explicitly wants to upload regardless).
	force := false
	if v, ok := args["force"]; ok {
		switch val := v.(type) {
		case bool:
			force = val
		case string:
			force = strings.EqualFold(val, "true")
		}
	}
	if !force {
		if exec := h.getSkillExecutor(); exec != nil {
			exec.mu.RLock()
			skills := exec.loadSkills()
			exec.mu.RUnlock()
			for _, s := range skills {
				if s.MatchesName(name) {
					if s.UsageCount < 2 {
						return fmt.Sprintf("Skill「%s」尚未经过充分测试（使用 %d 次）。建议先执行几次确认可用后再上传。如需强制上传，请传入 force=true", name, s.UsageCount)
					}
					if s.SuccessCount == 0 {
						return fmt.Sprintf("Skill「%s」从未成功执行过（使用 %d 次，成功 0 次）。建议先修复后再上传。如需强制上传，请传入 force=true", name, s.UsageCount)
					}
					break
				}
			}
		}
	}

	submissionID, err := h.appUploadNLSkillToMarket(name)
	if err != nil {
		return fmt.Sprintf("上传失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ Skill「%s」已上传到 SkillMarket，提交 ID: %s", name, submissionID)
}

// toolValidateSkill validates a skill for portability issues and optionally auto-fixes them.
func (h *IMMessageHandler) toolValidateSkill(args map[string]interface{}) string {
	name := stringVal(args, "name")
	if name == "" {
		return "缺少 name 参数（要验证的 Skill 名称）"
	}

	autoFix, _ := args["auto_fix"].(bool)

	// Resolve skill directory from name using the skill executor (same pattern as packageSkillForMarket).
	skillDir := ""
	if h.getSkillExecutor() != nil {
		for _, s := range h.getSkillExecutor().loadSkills() {
			if s.Name == name {
				skillDir = s.SkillDir
				break
			}
		}
	}

	// Fallback: check PrimarySkillsDir/<name> if executor didn't find it or SkillDir was empty.
	if skillDir == "" {
		primaryDir, err := cskill.PrimarySkillsDir()
		if err == nil {
			candidate := filepath.Join(primaryDir, name)
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				skillDir = candidate
			}
		}
	}

	if skillDir == "" {
		return fmt.Sprintf("skill %q not found. Use manage_skill(action=\"list\") to see installed skills", name)
	}

	// Initial validation.
	report, err := cskill.ValidateSkillPortability(skillDir)
	if err != nil {
		return fmt.Sprintf("验证失败: %s", err.Error())
	}

	if !autoFix {
		return cskill.FormatPortabilityReport(report)
	}

	// Auto-fix flow: fix → re-validate.
	changes, fixErr := cskill.AutoFixPortability(skillDir)
	if fixErr != nil {
		return fmt.Sprintf("自动修复失败: %s\n\n%s", fixErr.Error(), cskill.FormatPortabilityReport(report))
	}

	// Re-validate after fixes.
	finalReport, revalidateErr := cskill.ValidateSkillPortability(skillDir)
	if revalidateErr != nil {
		return fmt.Sprintf("修复后重新验证失败: %s\n\n%s", revalidateErr.Error(), cskill.FormatPortabilityChanges(changes))
	}

	var b strings.Builder
	b.WriteString(cskill.FormatPortabilityChanges(changes))
	b.WriteByte('\n')
	b.WriteString(cskill.FormatPortabilityReport(finalReport))
	return b.String()
}

func (h *IMMessageHandler) toolParallelExecute(args map[string]interface{}) string {
	h.app.ensureOrchestrator()
	orch := h.app.orchestrator
	if orch == nil {
		return "Orchestrator 未初始化"
	}
	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok || len(tasksRaw) == 0 {
		return "缺少 tasks 参数"
	}
	var tasks []TaskRequest
	for _, t := range tasksRaw {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		tr := TaskRequest{
			Tool:        stringVal(tm, "tool"),
			Description: stringVal(tm, "description"),
			ProjectPath: stringVal(tm, "project_path"),
		}
		if tr.Tool == "" {
			continue
		}
		tasks = append(tasks, tr)
	}
	if len(tasks) == 0 {
		return "没有有效的任务"
	}
	result, err := orch.ExecuteParallel(tasks)
	if err != nil {
		return fmt.Sprintf("并行执行失败: %s", err.Error())
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %s: %s\n", result.TaskID, result.Summary))
	for key, sr := range result.Results {
		b.WriteString(fmt.Sprintf("- %s: tool=%s status=%s", key, sr.Tool, sr.Status))
		if sr.SessionID != "" {
			b.WriteString(fmt.Sprintf(" session=%s", sr.SessionID))
		}
		if sr.Error != "" {
			b.WriteString(fmt.Sprintf(" error=%s", sr.Error))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolRecommendTool(args map[string]interface{}) string {
	selector := h.getToolSelector()
	if selector == nil {
		return "ToolSelector 未初始化"
	}
	desc, _ := args["task_description"].(string)
	if desc == "" {
		return "缺少 task_description 参数"
	}
	// Build list of installed tools by checking if their binaries are on PATH.
	var installed []string
	for _, tool := range []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	name, reason := selector.Recommend(desc, installed)
	return fmt.Sprintf("推荐工具: %s\n理由: %s", name, reason)
}

// ---------------------------------------------------------------------------
// 本机直接操作工具 (bash, read_file, write_file, list_directory)
// ---------------------------------------------------------------------------

const (
	bashDefaultTimeout      = 30
	bashMaxTimeout          = 120
	bashPDFTimeout          = bashMaxTimeout
	craftToolDefaultTimeout = 90
	craftToolMaxTimeout     = 300
	craftToolPDFTimeout     = 180
	readFileMaxLines        = 200
	writeFileMaxSize        = 1 << 20 // 1 MB
)

func looksLikePDFRelatedWork(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	hints := []string{
		"pdf", ".pdf", "pandoc", "wkhtmltopdf", "weasyprint", "reportlab", "pdfkit",
		"markdown to pdf", "markdown->pdf", "markdown 转 pdf", "markdown转pdf",
		"生成 pdf", "生成pdf", "转 pdf", "转pdf", "导出 pdf", "导出pdf",
	}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func resolveBashTimeout(args map[string]interface{}, command string) int {
	timeout := bashDefaultTimeout
	explicit := false
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		explicit = true
	}
	if !explicit && looksLikePDFRelatedWork(command) {
		timeout = bashPDFTimeout
	}
	if timeout > bashMaxTimeout {
		timeout = bashMaxTimeout
	}
	return timeout
}

func resolveCraftToolTimeout(args map[string]interface{}, task string) int {
	timeout := craftToolDefaultTimeout
	explicit := false
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		explicit = true
	}
	if !explicit && looksLikePDFRelatedWork(task) {
		timeout = craftToolPDFTimeout
	}
	if timeout > craftToolMaxTimeout {
		timeout = craftToolMaxTimeout
	}
	return timeout
}

// resolvePath resolves a path, expanding ~ and making relative paths relative
// to ~/.maclaw/workspace. When p is empty, returns ~/.maclaw/workspace.
func resolvePath(p string) string {
	if p == "" {
		return corelib.EffectiveWorkspaceDir()
	}
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(corelib.EffectiveWorkspaceDir(), p)
	}
	return filepath.Clean(p)
}

// toolGeneratePDF generates a PDF from Markdown content and returns it as a
// base64-encoded file payload. Only intended for coding workflow documents
// (requirements, design, task plan).
func (h *IMMessageHandler) toolGeneratePDF(args map[string]interface{}) string {
	content := stringVal(args, "content")
	title := stringVal(args, "title")
	docType := stringVal(args, "doc_type")

	if strings.TrimSpace(content) == "" {
		return "缺少 content 参数（Markdown 格式的文档内容）"
	}
	if strings.TrimSpace(title) == "" {
		title = "文档"
	}

	// Lazily initialize and cache the doc generator on the App instance
	// to avoid repeated font detection on every call.
	h.app.ensureDocGenerator()
	gen := h.app.docGenerator
	if gen == nil || !gen.HasFont() {
		return "未找到可用的中文字体，无法生成 PDF。请改用 write_file 写入 Markdown 文件后用 send_file 发送。"
	}

	var dt swarm.DocType
	switch docType {
	case "requirements":
		dt = swarm.DocTypeRequirements
	case "design":
		dt = swarm.DocTypeDesign
	case "task_plan":
		dt = swarm.DocTypeTaskPlan
	default:
		dt = ""
	}

	b64Data, fileName, err := gen.GenerateAndEncode(dt, title, content)
	if err != nil {
		return fmt.Sprintf("PDF 生成失败: %s", err.Error())
	}

	return fmt.Sprintf("[file_base64|%s|application/pdf]%s", fileName, b64Data)
}

// toolMemory merges save/list/delete/recall memory operations into a single tool.
func (h *IMMessageHandler) toolMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "长期记忆未初始化"
	}

	action := stringVal(args, "action")
	switch action {
	case "recall":
		query := stringVal(args, "query")
		if query == "" {
			return "缺少 query 参数"
		}
		category := corememory.Category(stringVal(args, "category"))
		// Resolve current project path for affinity boosting.
		var projectPath string
		if h.contextResolver != nil {
			projectPath, _ = h.contextResolver.ResolveProject()
		}
		entries := h.memoryStore.RecallDynamic(query, category, projectPath)
		if len(entries) == 0 {
			return "没有找到相关记忆。"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("召回 %d 条相关记忆:\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", string(e.Category), e.Content))
		}
		// Touch access counts.
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		h.memoryStore.TouchAccess(ids)
		return b.String()

	case "save":
		content := stringVal(args, "content")
		if content == "" {
			return "缺少 content 参数"
		}
		category := stringVal(args, "category")
		if category == "" {
			category = "user_fact"
		}
		var tags []string
		if rawTags, ok := args["tags"]; ok {
			if tagSlice, ok := rawTags.([]interface{}); ok {
				for _, t := range tagSlice {
					if s, ok := t.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
			}
		}
		// Auto-enrich tags from content when LLM didn't provide any.
		if len(tags) == 0 {
			expanded := corememory.ExpandQuery(content)
			tags = expanded.Entities
		}
		entry := corememory.Entry{
			Content:  content,
			Category: corememory.Category(category),
			Tags:     tags,
			OwnerID:  h.lastUserID, // multi-tenant: associate with the current user
		}
		// Enrich tags from recent conversation context: extract entities
		// from the last few user+assistant messages so alias terms (e.g.
		// user calls api.rapidai.tech "4090服务器") become searchable tags.
		contextHint := h.buildMemoryContextHint()
		if err := h.memoryStore.SaveWithContext(entry, contextHint); err != nil {
			return fmt.Sprintf("保存记忆失败: %s", err.Error())
		}
		summary := content
		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}
		return fmt.Sprintf("已保存记忆: %s", summary)

	case "list":
		category := corememory.Category(stringVal(args, "category"))
		keyword := stringVal(args, "keyword")
		entries := h.memoryStore.List(category, keyword)
		if len(entries) == 0 {
			return "没有找到匹配的记忆条目。"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("找到 %d 条记忆:\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] (%s) %s", e.ID, e.Category, e.Content))
			if len(e.Tags) > 0 {
				b.WriteString(fmt.Sprintf(" 标签=%v", e.Tags))
			}
			b.WriteString("\n")
		}
		return b.String()

	case "delete":
		id := stringVal(args, "id")
		if id == "" {
			return "缺少 id 参数"
		}
		if err := h.memoryStore.Delete(id); err != nil {
			return fmt.Sprintf("删除记忆失败: %s", err.Error())
		}
		return fmt.Sprintf("已删除记忆: %s", id)

	default:
		return "action 参数无效，可选值: recall, save, list, delete"
	}
}

// buildMemoryContextHint extracts recent conversation text (last 5 user+assistant
// messages) to provide alias and context terms for tag enrichment during memory save.
func (h *IMMessageHandler) buildMemoryContextHint() string {
	if h.memory == nil {
		return ""
	}
	userID := h.lastUserID
	if userID == "" {
		userID = "desktop-user"
	}
	entries := h.memory.Load(userID)
	if len(entries) == 0 {
		return ""
	}

	// Take the last 10 entries (roughly 5 user+assistant pairs).
	start := len(entries) - 10
	if start < 0 {
		start = 0
	}

	var sb strings.Builder
	for _, e := range entries[start:] {
		role, _ := e.Role, ""
		if role != "user" && role != "assistant" {
			continue
		}
		text, ok := e.Content.(string)
		if !ok || text == "" {
			continue
		}
		// Truncate each message to avoid excessive context.
		runes := []rune(text)
		if len(runes) > 300 {
			text = string(runes[:300])
		}
		sb.WriteString(text)
		sb.WriteString(" ")
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Template Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "name")
	tool := stringVal(args, "tool")
	if name == "" || tool == "" {
		return "缺少 name 或 tool 参数"
	}

	tpl := remote.SessionTemplate{
		Name:        name,
		Tool:        tool,
		ProjectPath: stringVal(args, "project_path"),
		ModelConfig: stringVal(args, "model_config"),
	}

	// Parse yolo_mode (can arrive as bool or string).
	if yolo, ok := args["yolo_mode"].(bool); ok {
		tpl.YoloMode = yolo
	} else if yoloStr, ok := args["yolo_mode"].(string); ok {
		tpl.YoloMode = yoloStr == "true"
	}

	if err := h.templateManager.Create(tpl); err != nil {
		return fmt.Sprintf("创建模板失败: %s", err.Error())
	}
	return fmt.Sprintf("模板已创建: %s（工具=%s, 项目=%s）", name, tool, tpl.ProjectPath)
}

func (h *IMMessageHandler) toolListTemplates() string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	templates := h.templateManager.List()
	if len(templates) == 0 {
		return "当前没有会话模板。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个模板:\n", len(templates)))
	for _, t := range templates {
		b.WriteString(fmt.Sprintf("- %s: 工具=%s 项目=%s", t.Name, t.Tool, t.ProjectPath))
		if t.ModelConfig != "" {
			b.WriteString(fmt.Sprintf(" 模型=%s", t.ModelConfig))
		}
		if t.YoloMode {
			b.WriteString(" [Yolo]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolLaunchTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "template_name")
	if name == "" {
		return "缺少 template_name 参数"
	}

	tpl, err := h.templateManager.Get(name)
	if err != nil {
		return fmt.Sprintf("获取模板失败: %s", err.Error())
	}

	// Build args from template config and delegate to toolCreateSession.
	sessionArgs := map[string]interface{}{
		"tool":         tpl.Tool,
		"project_path": tpl.ProjectPath,
	}
	return h.toolCreateSession(sessionArgs)
}

// ---------------------------------------------------------------------------
// Config Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolGetConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	result, err := h.configManager.GetConfig(section, true)
	if err != nil {
		return fmt.Sprintf("读取配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	key := stringVal(args, "key")
	value := stringVal(args, "value")
	if section == "" || key == "" {
		return "缺少 section 或 key 参数"
	}

	oldValue, err := h.configManager.UpdateConfig(section, key, value)
	if err != nil {
		return fmt.Sprintf("修改配置失败: %s", err.Error())
	}
	return fmt.Sprintf("配置已更新: %s.%s\n旧值: %s\n新值: %s", section, key, oldValue, value)
}

func (h *IMMessageHandler) toolBatchUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	changesStr := stringVal(args, "changes")
	if changesStr == "" {
		return "缺少 changes 参数"
	}

	var changes []config.ConfigChange
	if err := json.Unmarshal([]byte(changesStr), &changes); err != nil {
		return fmt.Sprintf("解析 changes 参数失败: %s", err.Error())
	}
	if len(changes) == 0 {
		return "changes 列表为空"
	}

	if err := h.configManager.BatchUpdate(changes); err != nil {
		return fmt.Sprintf("批量更新配置失败: %s", err.Error())
	}
	return fmt.Sprintf("批量更新成功，共应用 %d 项变更", len(changes))
}

func (h *IMMessageHandler) toolListConfigSchema() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.SchemaJSON()
	if err != nil {
		return fmt.Sprintf("获取配置 Schema 失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolExportConfig() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.ExportConfig()
	if err != nil {
		return fmt.Sprintf("导出配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolImportConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	jsonData := stringVal(args, "json_data")
	if jsonData == "" {
		return "缺少 json_data 参数"
	}

	report, err := h.configManager.ImportConfig(jsonData)
	if err != nil {
		return fmt.Sprintf("导入配置失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("配置导入完成: 应用 %d 项, 跳过 %d 项", report.Applied, report.Skipped))
	if len(report.Warnings) > 0 {
		b.WriteString("\n警告:")
		for _, w := range report.Warnings {
			b.WriteString(fmt.Sprintf("\n  - %s", w))
		}
	}
	return b.String()
}

// ===================== Merged tool dispatchers =====================

// toolManageConfig dispatches the merged manage_config tool to individual handlers.
func (h *IMMessageHandler) toolManageConfig(args map[string]interface{}) string {
	action := stringVal(args, "action")
	switch action {
	case "get":
		return h.toolGetConfig(args)
	case "set":
		return h.toolUpdateConfig(args)
	case "batch":
		return h.toolBatchUpdateConfig(args)
	case "schema":
		return h.toolListConfigSchema()
	case "export":
		return h.toolExportConfig()
	case "import":
		return h.toolImportConfig(args)
	default:
		return fmt.Sprintf("未知 manage_config action: %s（支持: get/set/batch/schema/export/import）", action)
	}
}

// toolManageTemplate dispatches the merged manage_template tool to individual handlers.
func (h *IMMessageHandler) toolManageTemplate(args map[string]interface{}) string {
	action := stringVal(args, "action")
	switch action {
	case "create":
		return h.toolCreateTemplate(args)
	case "list":
		return h.toolListTemplates()
	case "launch":
		if _, ok := args["template_name"]; !ok {
			if name, ok := args["name"]; ok {
				args["template_name"] = name
			}
		}
		return h.toolLaunchTemplate(args)
	default:
		return fmt.Sprintf("未知 manage_template action: %s（支持: create/list/launch）", action)
	}
}

// toolManageSchedule dispatches the merged manage_schedule tool to individual handlers.
func (h *IMMessageHandler) toolManageSchedule(args map[string]interface{}) string {
	action := stringVal(args, "action")
	if ta, ok := args["task_action"]; ok {
		args["action"] = ta
	}
	switch action {
	case "create":
		return h.toolCreateScheduledTask(args)
	case "list":
		return h.toolListScheduledTasks()
	case "delete":
		return h.toolDeleteScheduledTask(args)
	case "update":
		return h.toolUpdateScheduledTask(args)
	default:
		return fmt.Sprintf("未知 manage_schedule action: %s（支持: create/list/delete/update）", action)
	}
}

// toolSetMaxIterations allows the agent to dynamically adjust the max
// iterations for the current conversation loop. This does NOT change the
// persisted config — it only affects the in-flight loop.
func (h *IMMessageHandler) toolSetMaxIterations(args map[string]interface{}) string {
	n, ok := args["max_iterations"].(float64)
	if !ok || n < 1 {
		return fmt.Sprintf("缺少或无效的 max_iterations 参数（需要 %d-%d 的整数）", config.MinAgentIterations, config.MaxAgentIterationsCap)
	}
	// Use the single source of truth for value normalization.
	limit := config.EffectiveMaxIterations(int(n))
	reason := stringVal(args, "reason")
	h.loopMaxOverride = limit
	// Also update the active LoopContext so background loops see the change.
	if h.currentLoopCtx != nil {
		h.currentLoopCtx.SetMaxIterations(limit)
	}

	if reason != "" {
		return fmt.Sprintf("✅ 已将当前任务最大轮数调整为 %d（仅本次任务生效，原因: %s）", limit, reason)
	}
	return fmt.Sprintf("✅ 已将当前任务最大轮数调整为 %d（仅本次任务生效）", limit)
}

// ---------------------------------------------------------------------------
// Nickname (set_nickname) tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSetNickname(args map[string]interface{}) string {
	nickname := strings.TrimSpace(stringVal(args, "nickname"))
	if nickname == "" {
		return "❌ nickname 不能为空"
	}
	// Persist to local config.
	cfg, err := h.loadConfig()
	if err == nil {
		cfg.RemoteNickname = nickname
		_ = h.saveConfig(cfg)
	}
	// Send to Hub via WebSocket.
	if hc := h.getHubClient(); hc != nil {
		if err := hc.SendNicknameUpdate(nickname); err != nil {
			log.Printf("[set_nickname] SendNicknameUpdate error: %v", err)
			return fmt.Sprintf("⚠️ 昵称已保存到本地（%s），但上报 Hub 失败：%v", nickname, err)
		}
	}
	return fmt.Sprintf("✅ 昵称已更新为「%s」，Hub 已同步。", nickname)
}

// ---------------------------------------------------------------------------
// LLM provider switch tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSwitchLLMProvider(args map[string]interface{}) string {
	providerName := stringVal(args, "provider")
	if providerName == "" {
		// No provider specified — list available providers and current selection.
		info := h.getMaclawLLMProviders()
		var b strings.Builder
		b.WriteString(fmt.Sprintf("当前 LLM 服务商: %s\n可用服务商:\n", info.Current))
		for _, p := range info.Providers {
			if p.URL == "" && p.Key == "" && p.Model == "" {
				continue // skip unconfigured custom slots
			}
			marker := ""
			if p.Name == info.Current {
				marker = " [当前]"
			}
			b.WriteString(fmt.Sprintf("  - %s (model=%s)%s\n", p.Name, p.Model, marker))
		}
		return b.String()
	}

	info := h.getMaclawLLMProviders()

	// Collect only configured providers for matching.
	var configured []corelib.MaclawLLMProvider
	for _, p := range info.Providers {
		if p.URL != "" || p.Key != "" || p.Model != "" {
			configured = append(configured, p)
		}
	}

	// Match: exact (case-insensitive) first, then substring fallback.
	needle := strings.ToLower(strings.TrimSpace(providerName))
	var match *corelib.MaclawLLMProvider
	for i := range configured {
		if strings.ToLower(configured[i].Name) == needle {
			match = &configured[i]
			break
		}
	}
	if match == nil {
		// Fuzzy: check if provider name contains the needle (not the reverse,
		// to avoid short provider names matching arbitrary long input).
		for i := range configured {
			lower := strings.ToLower(configured[i].Name)
			if strings.Contains(lower, needle) {
				match = &configured[i]
				break
			}
		}
	}

	if match == nil {
		var names []string
		for _, p := range configured {
			names = append(names, p.Name)
		}
		return fmt.Sprintf("未找到服务商 %q，可用: %s", providerName, strings.Join(names, ", "))
	}

	if match.Name == info.Current {
		return fmt.Sprintf("当前已经是 %s，无需切换", match.Name)
	}

	if err := h.saveMaclawLLMProviders(info.Providers, match.Name); err != nil {
		return fmt.Sprintf("切换失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ 已将 LLM 服务商切换为 %s (model=%s)", match.Name, match.Model)
}

// ---------------------------------------------------------------------------
// Scheduled task tool implementations
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	name := stringVal(args, "name")
	action := stringVal(args, "action")
	if name == "" || action == "" {
		return "缺少 name 或 action 参数"
	}
	hour := -1
	if v, ok := args["hour"].(float64); ok {
		hour = int(v)
	}
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := 0
	if v, ok := args["minute"].(float64); ok {
		minute = int(v)
	}
	dow := -1
	if v, ok := args["day_of_week"].(float64); ok {
		dow = int(v)
	}
	dom := -1
	if v, ok := args["day_of_month"].(float64); ok {
		dom = int(v)
	}

	intervalMin := 0
	if v, ok := args["interval_minutes"].(float64); ok {
		intervalMin = int(v)
	}

	t := scheduler.ScheduledTask{
		Name:            name,
		Action:          action,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       dow,
		DayOfMonth:      dom,
		IntervalMinutes: intervalMin,
		StartDate:       stringVal(args, "start_date"),
		EndDate:         stringVal(args, "end_date"),
		TaskType:        stringVal(args, "task_type"),
	}

	id, err := h.scheduledTaskManager.Add(t)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}

	// Notify frontend to refresh the scheduled tasks panel.
	h.emitAppEvent("scheduled-tasks-changed")

	// 非一次性任务同步到系统日历
	if created := h.scheduledTaskManager.Get(id); created != nil && scheduler.IsRecurringTask(created) {
		go func() {
			if err := scheduler.SyncTaskToSystemCalendar(created); err != nil {
				h.appLog(fmt.Sprintf("[scheduled-task] calendar sync failed: %v", err))
			}
		}()
	}

	// Format next run time for display.
	if task := h.scheduledTaskManager.Get(id); task != nil && task.NextRunAt != nil {
		return fmt.Sprintf("✅ 定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s", id, name, action, task.NextRunAt.Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("✅ 定时任务已创建（ID: %s）", id)
}

func (h *IMMessageHandler) toolListScheduledTasks() string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	tasks := h.scheduledTaskManager.List()
	if len(tasks) == 0 {
		return "当前没有定时任务。"
	}

	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个定时任务：\n\n", len(tasks)))
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("📋 [%s] %s\n", t.ID, t.Name))
		b.WriteString(fmt.Sprintf("   操作: %s\n", t.Action))

		// Schedule description
		var sched string
		if t.IntervalMinutes > 0 {
			sched = fmt.Sprintf("每%s（首次 %02d:%02d）", scheduler.FormatInterval(t.IntervalMinutes), t.Hour, t.Minute)
		} else {
			sched = fmt.Sprintf("每天 %02d:%02d", t.Hour, t.Minute)
			if t.DayOfWeek >= 0 && t.DayOfWeek <= 6 {
				sched = fmt.Sprintf("每%s %02d:%02d", weekdays[t.DayOfWeek], t.Hour, t.Minute)
			}
			if t.DayOfMonth > 0 {
				sched = fmt.Sprintf("每月%d号 %02d:%02d", t.DayOfMonth, t.Hour, t.Minute)
			}
		}
		if t.StartDate != "" || t.EndDate != "" {
			sched += fmt.Sprintf("（%s ~ %s）", t.StartDate, t.EndDate)
		}
		b.WriteString(fmt.Sprintf("   时间: %s\n", sched))
		b.WriteString(fmt.Sprintf("   状态: %s", t.Status))
		if t.NextRunAt != nil {
			b.WriteString(fmt.Sprintf(" | 下次执行: %s", t.NextRunAt.Format("2006-01-02 15:04")))
		}
		if t.RunCount > 0 {
			b.WriteString(fmt.Sprintf(" | 已执行 %d 次", t.RunCount))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolDeleteScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	name := stringVal(args, "name")
	if id == "" && name == "" {
		return "请提供 id 或 name 参数"
	}
	var err error
	if id != "" {
		err = h.scheduledTaskManager.Delete(id)
	} else {
		err = h.scheduledTaskManager.DeleteByName(name)
	}
	if err != nil {
		return fmt.Sprintf("删除失败: %s", err.Error())
	}
	h.emitAppEvent("scheduled-tasks-changed")
	return "✅ 定时任务已删除"
}

func (h *IMMessageHandler) toolUpdateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	err := h.scheduledTaskManager.Update(id, args)
	if err != nil {
		return fmt.Sprintf("更新失败: %s", err.Error())
	}
	h.emitAppEvent("scheduled-tasks-changed")
	// Show updated task info.
	if t := h.scheduledTaskManager.Get(id); t != nil {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("✅ 定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next)
	}
	return "✅ 定时任务已更新"
}

// ---------- AgentNet Knowledge Tools ----------

func (h *IMMessageHandler) toolAgentNetSearch(args map[string]interface{}) string {
	if h.getAgentNetClient() == nil || !h.getAgentNetClient().IsRunning() {
		return "智网未连接，请先在设置中启用 AgentNet"
	}
	query := stringVal(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	entries, err := h.getAgentNetClient().SearchKnowledge(query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(entries) == 0 {
		return fmt.Sprintf("未找到与「%s」相关的知识条目", query)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔍 智网知识搜索「%s」— 找到 %d 条:\n\n", query, len(entries)))
	for i, e := range entries {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("... 还有 %d 条结果\n", len(entries)-10))
			break
		}
		b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, e.Title))
		if e.Body != "" {
			body := e.Body
			if len(body) > 200 {
				body = body[:200] + "…"
			}
			b.WriteString(fmt.Sprintf("   %s\n", body))
		}
		if e.Author != "" {
			b.WriteString(fmt.Sprintf("   — %s", e.Author))
		}
		if e.Domain != "" {
			b.WriteString(fmt.Sprintf(" [%s]", e.Domain))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolAgentNetPublish(args map[string]interface{}) string {
	if h.getAgentNetClient() == nil || !h.getAgentNetClient().IsRunning() {
		return "智网未连接，请先在设置中启用 AgentNet"
	}
	title := stringVal(args, "title")
	body := stringVal(args, "body")
	if title == "" {
		return "缺少 title 参数"
	}
	if body == "" {
		return "缺少 body 参数"
	}
	entry, err := h.getAgentNetClient().PublishKnowledge(title, body)
	if err != nil {
		return fmt.Sprintf("发布失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ 知识已发布到智网\nID: %s\n标题: %s", entry.ID, entry.Title)
}

func (h *IMMessageHandler) toolQueryAuditLog(args map[string]interface{}) string {
	if h.app == nil {
		return "审计日志未初始化"
	}
	_ = h.getAuditLog() // ensure
	if h.getAuditLog() == nil {
		return "审计日志未初始化"
	}

	filter := security.AuditFilter{}
	if since := stringVal(args, "since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.StartTime = &t
		}
	}
	if until := stringVal(args, "until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.EndTime = &t
		}
	}
	if tn := stringVal(args, "tool_name"); tn != "" {
		filter.ToolName = tn
	}
	if rl := stringVal(args, "risk_level"); rl != "" {
		filter.RiskLevels = []security.RiskLevel{security.RiskLevel(rl)}
	}

	entries, err := h.getAuditLog().Query(filter)
	if err != nil {
		return fmt.Sprintf("查询失败: %s", err.Error())
	}

	limit := 20
	if l, ok := args["limit"]; ok {
		if lf, ok := l.(float64); ok && lf > 0 {
			limit = int(lf)
		}
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	if len(entries) == 0 {
		return "没有找到匹配的审计记录"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条审计记录:\n\n", len(entries)))
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("%d. [%s] %s | 风险: %s | 决策: %s | 结果: %s\n",
			i+1, e.Timestamp.Format("01-02 15:04"), e.ToolName, e.RiskLevel, e.PolicyAction, e.Result))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Web Search & Fetch Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolWebSearch(args map[string]interface{}) string {
	query := stringVal(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	maxResults := 8
	if n, ok := args["max_results"].(float64); ok && n > 0 {
		maxResults = int(n)
	}

	searchCfg := h.app.GetWebSearchProviders()
	provider := corelib.WebSearchProvider{Type: searchCfg.Current}
	for _, p := range searchCfg.Providers {
		if strings.EqualFold(strings.TrimSpace(p.Type), strings.TrimSpace(searchCfg.Current)) {
			provider = p
			break
		}
	}

	results, err := websearch.SearchWithProvider(query, maxResults, provider)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(results) == 0 {
		return "未找到相关结果"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("搜索 \"%s\" 找到 %d 条结果:\n\n", query, len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (h *IMMessageHandler) toolWebFetch(args map[string]interface{}) string {
	rawURL := stringVal(args, "url")
	if rawURL == "" {
		return "缺少 url 参数"
	}

	offset := intArg(args, "offset", 0)
	maxChars := intArg(args, "max_chars", 16384)
	if _, ok := args["max_chars"]; ok && maxChars <= 0 {
		maxChars = 0
	}
	opts := &websearch.FetchOptions{Offset: offset, MaxChars: maxChars}
	if renderJS, ok := args["render_js"].(bool); ok {
		opts.RenderJS = renderJS
	}
	if savePath := stringVal(args, "save_path"); savePath != "" {
		opts.SavePath = resolvePath(savePath)
		opts.MaxBytes = 10 * 1024 * 1024 // 10MB for file downloads
	} else {
		// For text content, allow up to 2MB raw before extraction/windowing.
		opts.MaxBytes = 2 * 1024 * 1024
	}
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		opts.TimeoutS = int(t)
	} else {
		opts.TimeoutS = intArg(args, "timeout", 30)
	}

	result, err := websearch.Fetch(rawURL, opts)
	if err != nil {
		return fmt.Sprintf("抓取失败: %s", err.Error())
	}

	// If saved to file, return short message
	if result.SavedTo != "" {
		return result.Content
	}

	start := offset
	if start < 0 {
		start = 0
	}
	end := start + len([]rune(result.Content))

	var sb strings.Builder
	if result.Title != "" {
		sb.WriteString(fmt.Sprintf("标题: %s\n", result.Title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", result.URL))
	sb.WriteString(fmt.Sprintf("类型: %s | 大小: %d 字节\n", result.ContentType, result.BytesRead))
	sb.WriteString(fmt.Sprintf("已读取: %d-%d / %d 字符\n", start, end, result.TotalChars))
	sb.WriteString(fmt.Sprintf("truncated: %t | has_more: %t | next_offset: %d\n\n", result.Truncated, result.HasMore, result.NextOffset))
	sb.WriteString(result.Content)
	if result.HasMore {
		sb.WriteString(fmt.Sprintf("\n\n--- 完整性信号 ---\nhas_more: true\nnext_offset: %d\n继续读取时请传入 offset=%d\n", result.NextOffset, result.NextOffset))
	}
	return sb.String()
}
