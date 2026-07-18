package main

// Miscellaneous tools: MCP, skills, memory, templates, config, nickname,
// LLM provider switch, scheduled tasks, audit log, web search/fetch.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	mcputil "github.com/RapidAI/CodeClaw/corelib/mcp"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
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
					HealthStatus: s.HealthStatus.String(),
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
	ownerID, explicitRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if explicitRuntimeOwner && ownerID == "" {
		return "MCP call failed: runtime owner is missing; isolated runtime will not fall back to desktop owner"
	}
	serverRef, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if isDisabledExternalCodingSessionTool(toolName) {
		return disabledExternalCodingSessionToolText(toolName)
	}
	if serverRef == "" || toolName == "" {
		if nested, err := mcpToolArgumentsFromAny(args["arguments"]); err == nil {
			promoteMCPRoutingFields(args, nested)
			serverRef, _ = args["server_id"].(string)
			toolName, _ = args["tool_name"].(string)
		}
	}
	if isDisabledExternalCodingSessionTool(toolName) {
		return disabledExternalCodingSessionToolText(toolName)
	}
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
			if err := json.Unmarshal([]byte(coretool.CleanToolArguments(v)), &toolArgs); err != nil {
				return fmt.Sprintf("arguments JSON 解析失败: %s", err.Error())
			}
		}
	}
	if toolArgs == nil {
		toolArgs = map[string]interface{}{}
	}
	promoteMCPRoutingFields(args, toolArgs)
	serverRef, _ = args["server_id"].(string)
	toolName, _ = args["tool_name"].(string)
	if serverRef == "" || toolName == "" {
		return "缺少 server_id 或 tool_name 参数；server_id 支持 MCP Server 的 ID 或 Name"
	}

	if builtin := h.builtinToolServerRef(serverRef); builtin != "" {
		return fmt.Sprintf("MCP 调用被拒绝: %q 是 MaClaw 内置工具，不是 MCP Server。请直接调用 %s 工具；call_mcp_tool 只允许调用外部 MCP Server。", builtin, builtin)
	}

	if h.getLocalMCPManager() == nil {
		_ = h.getLocalMCPManager() // ensure
	}

	resolvedID, isLocal, err := h.resolveMCPServerRef(serverRef)
	if err != nil {
		return fmt.Sprintf("MCP 调用失败: %s。可先用 list_mcp_tools 查看 Name (ID)", err.Error())
	}

	// Look up the target tool's InputSchema from the tools cache for local
	// validation. If the tool is not in the cache, skip validation (graceful
	// degradation per Req 3.7).
	var inputSchema map[string]interface{}
	inputSchema = h.lookupMCPInputSchema(resolvedID, toolName, isLocal)

	// Validate arguments against the InputSchema before making the RPC call.
	if inputSchema != nil {
		if validationErrs := mcputil.ValidateArgs(inputSchema, toolArgs); len(validationErrs) > 0 {
			if h.emitMCPToolAgentViewForOwner(serverRef, resolvedID, toolName, inputSchema, toolArgs, validationErrs, ownerID) {
				return mcpAgentViewCorrectionMessage
			}
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
		result, err := mgr.CallToolForOwner(ownerID, resolvedID, toolName, toolArgs)
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
	result, err := registry.CallToolForOwner(ownerID, resolvedID, toolName, toolArgs)
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

func (h *IMMessageHandler) builtinToolServerRef(serverRef string) string {
	candidate := strings.TrimSpace(serverRef)
	if candidate == "" {
		return ""
	}
	if h.registry != nil {
		if registered, ok := h.registry.Get(candidate); ok && registered != nil && (registered.Category == ToolCategoryBuiltin || registered.Category == ToolCategoryNonCode) {
			return candidate
		}
	}
	if coretool.IsBuiltinToolName(candidate) {
		return candidate
	}
	return ""
}

func (h *IMMessageHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "缺少 query 参数"
	}

	// Use the unified HubClient.SearchAllFiltered() which searches allowed sources
	// (SkillHub + ClawHub + GitHub) through a single code path. This ensures
	// the LLM tool call sees the same results as the GUI/TUI search panels.
	//
	// Resolve the SkillHub base URL from HubCenter (same as SkillHubClient).
	// If resolution fails, SearchAllFiltered still queries ClawHub and GitHub.
	hubURL := ""
	if h.app != nil {
		hubURL = NewSkillMarketClient(h.app).baseURL()
	}
	searcher := NewSkillSearcher(NewSkillMarketClient(h.app))
	results, err := searcher.SearchAll(context.Background(), query)
	degradedNote := ""
	if err != nil {
		if d, ok := AsSearchDegraded(err); ok {
			degradedNote = d.Error()
		} else {
			return err.Error()
		}
	}
	if len(results) == 0 {
		if degradedNote != "" {
			return fmt.Sprintf("在 SkillHub/ClawHub/GitHub 上均未找到与 %q 相关的 Skill（且搜索不完整：%s）", query, degradedNote)
		}
		if cskill.LooksLikeDownloadSearchQuery(query) {
			return fmt.Sprintf("在 SkillHub/ClawHub/GitHub 上均未找到与 %q 相关的 Skill。\n优先建议：通用 HTTP/PDF 下载请直接使用 download_file 或 web_fetch(save_path=...)，无需安装 Hub 下载 skill。", query)
		}
		return fmt.Sprintf("在 SkillHub/ClawHub/GitHub 上均未找到与 %q 相关的 Skill", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 个 Skill：\n", len(results)))
	if degradedNote != "" {
		b.WriteString("⚠️ " + degradedNote + "\n")
		b.WriteString("（部分源失败，结果可能不完整；空结果不等于「无匹配 skill」）\n")
	}
	if cskill.LooksLikeDownloadSearchQuery(query) {
		b.WriteString("优先建议：通用 HTTP/PDF 下载用内置 download_file / web_fetch(save_path=...)，不要为简单下载安装 ClawHub wget/curl/Paper Fetch。\n")
	}
	for _, r := range results {
		caution := ""
		if cskill.LooksLikeGenericDownloadSkill(r.Name, r.Description) {
			caution = " [caution: generic download — prefer download_file]"
		}
		switch r.Source {
		case "skillhub", "skillmarket":
			b.WriteString(fmt.Sprintf("- [SkillHub] ID: %s | %s: %s (trust: %s, downloads: %d)%s\n  安装: manage_skill(action=\"install\", skill_id=\"%s\", hub_url=\"%s\")\n",
				r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, caution, r.ID, hubURL))
		case "clawhub":
			b.WriteString(fmt.Sprintf("- [ClawHub] ID: %s | %s: %s (downloads: %d)%s\n  安装: manage_skill(action=\"install\", skill_id=\"%s\", hub_url=\"%s\")\n",
				r.ID, r.Name, r.Description, r.Downloads, caution, r.ID, cskill.ClawHubMirrorURL))
		case "github":
			b.WriteString(fmt.Sprintf("- [GitHub] %s: %s (repo: %s)%s\n  安装: manage_skill(action=\"install\", skill_id=\"%s\", hub_url=\"github\", install_ref=%q)\n",
				r.Name, r.Description, r.RepoURL, caution, r.Name, r.InstallRef))
		default:
			b.WriteString(fmt.Sprintf("- [%s] %s: %s%s\n", r.Source, r.Name, r.Description, caution))
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

	runtimePlatform := consumeRuntimePlatformFromToolArgs(args)
	installPolicyOwnerID, explicitRuntimeOwner := h.toolArgsOrCurrentRuntimePolicyOwnerState(args)
	if installPolicyOwnerID == "" && explicitRuntimeOwner {
		return "Skill 安装失败: runtime owner is missing; isolated runtime will not fall back to desktop owner"
	}

	// Determine effective source and check permission before downloading.
	var effectiveSource string
	switch {
	case strings.EqualFold(hubURL, "github"):
		effectiveSource = "github"
	case strings.Contains(hubURL, "clawhub") || strings.Contains(hubURL, "clawhub-mirror"):
		effectiveSource = "clawhub"
	default:
		effectiveSource = "skillhub"
	}
	guardArgs := map[string]interface{}{"action": "install", "source": effectiveSource, "skill_id": skillID, "hub_url": hubURL, "install_ref": args["install_ref"]}
	if h.app != nil {
		if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
			return reason
		}
	}
	if h.app != nil && !h.app.IsSkillSourceAllowed(effectiveSource) {
		allowed := h.app.GetAllowedSkillSources()
		return fmt.Sprintf("来源 '%s' 已被管理策略禁止。当前允许的来源: %v", effectiveSource, allowed)
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
		// If skill_id is in publisher.name format, resolve to internal UUID first
		// via the by-skill-id endpoint, then use UUID for file-based install.
		resolvedSkillID := skillID
		if cskill.IsValidSkillID(skillID) {
			resolved, resolveErr := cskill.DefaultHubClient().DownloadBySkillID(ctx, hubURL, skillID, "")
			if resolveErr == nil && resolved != nil && resolved.HubSkillID != "" {
				resolvedSkillID = resolved.HubSkillID // use internal UUID for InstallToDir
				log.Printf("[skill-install] resolved skill_id %s → UUID %s", skillID, resolvedSkillID)
			} else if resolveErr != nil {
				log.Printf("[skill-install] DownloadBySkillID(%s) failed, using original ID: %v", skillID, resolveErr)
			}
		}

		if h.getSkillHubClient() == nil {
			h.ensureSkillHubClient()
		}
		if h.getSkillHubClient() == nil {
			return "SkillHub 客户端未初始化"
		}
		// Prepare staging directory for file-backed skills.
		stagingDir, err = cskill.PrepareStagingDir(resolvedSkillID)
		if err != nil {
			return fmt.Sprintf("创建临时目录失败: %s", err.Error())
		}
		// Download and extract files to staging dir (not final location).
		entry, err = h.getSkillHubClient().InstallToDir(ctx, resolvedSkillID, hubURL, stagingDir)
		sourceLabel = "SkillHub"
	}

	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return fmt.Sprintf("安装失败 (%s): %s", sourceLabel, err.Error())
	}

	var installScanReport *cskill.ScanReport

	// Security review: pattern scan plus optional agent scan; policy decides whether findings block.
	// Developer mode records scan findings but never blocks installation.
	{
		// Determine the directory to scan: staging dir if available, else entry.SkillDir.
		scanDir := stagingDir
		if scanDir == "" {
			scanDir = entry.SkillDir
		}

		var scanReport *cskill.ScanReport
		if h.app == nil || !h.app.isRiskGuardrailOffMode() {
			scanner := NewSkillSecurityScanner(h.app, nil)
			scanReport = scanner.ScanInstallStaged(ctx, entry, scanDir, func(status string) {
				log.Printf("[skill-install] %s: %s", entry.Name, status)
			})
			installScanReport = scanReport
		}

		if h.app != nil && h.app.skillInstallScanShouldBlockForSource(scanReport, effectiveSource) {
			cskill.CleanupStaging(stagingDir)
			if h.getAuditLog() != nil {
				_ = h.getAuditLog().Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    scanReport.FinalLevel,
					PolicyAction: security.PolicyDeny,
					Result:       fmt.Sprintf("pre-install policy blocked skill %s: %s", entry.Name, scanReport.Summary),
				})
			}
			return FormatScanReportForUser(scanReport, entry.Name) +
				"\n" + localizedSkillInstallBlockedMessage(h.skillConfirmLang(), entry.Name, true)
		}
		if h.app != nil && h.app.skillInstallReviewNeedsConfirmationForSource(scanReport, effectiveSource) {
			platform := runtimePlatform
			confirmCtx := context.Background()
			if loopCtx := h.runtimeLoopContextForOwner(installPolicyOwnerID); loopCtx != nil {
				if platform == "" {
					platform = runtimePlatformFromLoopContext(loopCtx)
				}
				var cancel context.CancelFunc
				confirmCtx, cancel = loopCtx.Context()
				defer cancel()
			}

			allFactors := scanReport.PatternAssessment.Factors
			for _, f := range scanReport.Findings {
				allFactors = append(allFactors, f.Description)
			}

			confirmed := h.confirmRiskSkillInstall(
				confirmCtx, entry.Name, hubURL, scanReport.FinalLevel, allFactors, platform, installPolicyOwnerID,
			)

			if !confirmed {
				cskill.CleanupStaging(stagingDir) // reject → clean up staging
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     "hub_skill_install",
						RiskLevel:    scanReport.FinalLevel,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("user rejected skill %s: %s", entry.Name, scanReport.Summary),
					})
				}
				return FormatScanReportForUser(scanReport, entry.Name) +
					"\n" + localizedSkillInstallRejectedMessage(h.skillConfirmLang(), entry.Name)
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
	committedDir := ""
	if stagingDir != "" {
		finalDir, commitErr := cskill.CommitStaging(stagingDir, entry.Name)
		if commitErr != nil {
			cskill.CleanupStaging(stagingDir)
			return fmt.Sprintf("安装失败（提交到最终目录）: %s", commitErr.Error())
		}
		entry.SkillDir = finalDir
		committedDir = finalDir
	}

	// Verify package integrity BEFORE normalization (normalization modifies files,
	// which would invalidate the upload-time hashes in the manifest).
	if entry.SkillDir != "" {
		if manifest, _ := cskill.ReadPackageManifest(entry.SkillDir); manifest != nil {
			if verifyErr := cskill.VerifyPackageIntegrity(entry.SkillDir, manifest); verifyErr != nil {
				log.Printf("[skill-install] integrity check failed for %s: %v", entry.Name, verifyErr)
				if committedDir != "" {
					_ = os.RemoveAll(committedDir)
				}
				return fmt.Sprintf("安装失败（包完整性校验不通过）: %s\n\n可能原因：包在传输过程中被篡改或损坏。请重新安装。", verifyErr.Error())
			}
		}
	}

	preNormalizeScanHash := ""
	if installScanReport != nil {
		if hash, err := skillContentHash(entry); err == nil {
			preNormalizeScanHash = hash
		} else {
			log.Printf("[skill-install] failed to hash approved pre-normalize skill %s: %v", entry.Name, err)
		}
	}

	// Normalize downloaded skills before registration: repair portable paths,
	// remove packaging-only backups, and reload the disk definition so runtime
	// uses the improved version rather than the raw download snapshot.
	entry = h.app.normalizeInstalledSkill(entry)

	if installScanReport != nil && preNormalizeScanHash != "" {
		if err := writeSkillScanCacheForReportStatus(entry, entry.SkillDir, preNormalizeScanHash, installScanReport, skillScanCacheStatusAllowed); err != nil {
			if committedDir != "" {
				_ = os.RemoveAll(committedDir)
			}
			return fmt.Sprintf("瀹夎澶辫触锛堝啓鍏ュ畨鍏ㄦ壂鎻忕紦瀛橈級: %s", err.Error())
		}
	}

	// GUI runner compatibility: warn (and demote unrunnable download skills)
	// so the agent does not treat broken Hub packages as valid download tools.
	compat := cskill.AssessRunnerCompatibility(entry, cskill.RunnerBackendGUI)
	if !compat.Runnable && len(entry.Steps) > 0 {
		entry.Status = "needs_review"
		if strings.TrimSpace(entry.LastError) == "" {
			entry.LastError = "install precheck: " + cskill.FormatRunnerCompatReport(compat)
		}
		log.Printf("[skill-install] marked %s needs_review: %s", entry.Name, entry.LastError)
	}

	// Register locally.
	if err := h.getSkillExecutor().Register(*entry); err != nil {

		if committedDir != "" {
			_ = os.RemoveAll(committedDir)
		}
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
		riskLevel := security.RiskLow
		policyAction := security.PolicyAllow
		if installScanReport != nil {
			riskLevel = installScanReport.FinalLevel
			if h.app != nil {
				policyAction = h.app.skillInstallFinalAuditActionForSource(installScanReport, effectiveSource)
			} else if installScanReport.NeedsUserReview() {
				policyAction = security.PolicyAudit
			}
		}
		_ = h.getAuditLog().Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    riskLevel,
			PolicyAction: policyAction,
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

	lang := h.skillConfirmLang()
	var b strings.Builder
	b.WriteString(localizedSkillInstallSuccessSummary(lang, entry.Name, entry.Description, hubURL, entry.TrustLevel))
	if report := cskill.AssessRunnerCompatibility(entry, cskill.RunnerBackendGUI); len(report.Warnings) > 0 || !report.Runnable {
		b.WriteString("\n\n")
		b.WriteString(cskill.FormatRunnerCompatReport(report))
		if report.SuggestedAlt != "" {
			b.WriteString("\nHint: " + report.SuggestedAlt)
		}
	}

	if autoRun {
		// Do not auto-run skills that install precheck demoted to needs_review
		// (e.g. GUI-unsupported step actions). Agent should use download_file etc.
		if normalizeSkillEntryStatus(entry.Status) == skillEntryStatusNeedsReview {
			b.WriteString("\n\nAuto-run skipped: skill status is needs_review after runner compatibility precheck.")
			if report := cskill.AssessRunnerCompatibility(entry, cskill.RunnerBackendGUI); report.SuggestedAlt != "" {
				b.WriteString(" Prefer " + report.SuggestedAlt + ".")
			}
			return b.String()
		}
		runArgs := buildRunSkillArgs(args)
		if shouldSkipInstructionOnlyInstallAutoRun(entry, runArgs) {
			b.WriteString("\n\nAuto-run skipped: this Skill is instruction-only and no concrete user task context was provided, so running it now would generate a script from the documentation alone.\n")
			b.WriteString(fmt.Sprintf("Run it with the original request, for example: manage_skill(action=\"run\", name=\"%s\", user_prompt=\"<original user request>\")", entry.Name))
			return b.String()
		}
		b.WriteString(localizedSkillInstallAutoRunStarting(lang, entry.Name))
		// Pass user-supplied run arguments (input, output, args, etc.) to the
		// skill runner so the auto-run after install actually has the parameters
		// the user intended. Previously this passed nil, causing skills that
		// require parameters to fail on auto-run every time.
		h.ensureSkillRunner()
		runner := h.getSkillRunner()
		if runner == nil {
			b.WriteString("Skill Runner 未初始化")
			return b.String()
		}
		consumeRuntimePolicyOwnerIDFromToolArgs(args)
		runID, err := runner.StartRunForOwner(installPolicyOwnerID, entry.Name, runArgs)
		if err != nil {
			b.WriteString(fmt.Sprintf("执行启动失败: %s", err.Error()))
		} else {
			waitDuration := normalizeSkillRunWaitSeconds(args["wait_seconds"])
			status, statusErr := waitForSkillRunnerSnapshot(context.Background(), h.getSkillRunner(), runID, waitDuration)
			if statusErr != nil {
				b.WriteString(fmt.Sprintf("已启动（run_id=%s），但读取状态失败: %s", runID, statusErr.Error()))
			} else {
				appendSkillRunSummary(&b, status, runID)
			}
		}
	} else {
		b.WriteString(localizedSkillInstallRunHint(lang, entry.Name))
	}

	return b.String()
}

func shouldSkipInstructionOnlyInstallAutoRun(entry *corelib.NLSkillEntry, runArgs map[string]interface{}) bool {
	return isInstructionOnlySkillEntry(entry) && !hasInstallAutoRunUserContext(runArgs)
}

func hasInstallAutoRunUserContext(runArgs map[string]interface{}) bool {
	return runArgMapHasContent(runArgs)
}

func runArgHasContent(value interface{}) bool {
	return runArgValueHasContent("", value)
}

func runArgValueHasContent(key string, value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return false
		}
		if cskill.CanonicalRunVarKey(key) == "args" {
			if strings.HasPrefix(trimmed, "[") {
				return false
			}
			var parsed map[string]interface{}
			if json.Unmarshal([]byte(trimmed), &parsed) == nil {
				return runArgMapHasContent(parsed)
			}
			if strings.HasPrefix(trimmed, "{") {
				return false
			}
		}
		return true
	case map[string]interface{}:
		return runArgMapHasContent(typed)
	case map[string]string:
		converted := make(map[string]interface{}, len(typed))
		for key, value := range typed {
			converted[key] = value
		}
		return runArgMapHasContent(converted)
	case []interface{}:
		if cskill.CanonicalRunVarKey(key) == "args" {
			return false
		}
		for _, item := range typed {
			if runArgHasContent(item) {
				return true
			}
		}
		return false
	case []string:
		if cskill.CanonicalRunVarKey(key) == "args" {
			return false
		}
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
		return false
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed)) != ""
	}
}

func runArgMapHasContent(values map[string]interface{}) bool {
	for key, value := range values {
		if installAutoRunContextSelectorKey(key) {
			continue
		}
		if runArgValueHasContent(key, value) {
			return true
		}
	}
	return false
}

func installAutoRunContextSelectorKey(key string) bool {
	key = cskill.CanonicalRunVarKey(key)
	if cskill.IsManageSkillRunnerControlKey(key) {
		return true
	}
	switch key {
	case "operation", "mode", "step", "steps", "output", "output_path", "output_file", "format", "source",
		"env", "extra_env", "environment", "working_dir", "language", "runtime_language", "timeout",
		"max_attempts", "verification_mode", "register_policy", "expected_artifacts", "save_as_skill":
		return true
	default:
		return false
	}
}

// stringVal extracts a string value from a map, returning "" if the key is
// missing or not a string.
func stringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
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

	// Skill ID immutability protection: reject patches that modify or remove the id field.
	if target.SkillID != "" {
		if idChanged := checkSkillIDChanged([]byte(modified), target.SkillID); idChanged != "" {
			return idChanged
		}
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
	h.refreshSkillIndexesAfterMutation(target.Name)

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

	// Skill ID immutability protection
	if target.SkillID != "" {
		if idChanged := checkSkillIDChanged(modified, target.SkillID); idChanged != "" {
			return idChanged
		}
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
	h.refreshSkillIndexesAfterMutation(target.Name)

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

	// Pre-upload portability gate (runs before the usage/quality gate so the
	// agent can fix path/completeness problems and retry). This auto-fixes safe
	// absolute paths in place ({baseDir}/$HOME), then reports any remaining
	// machine-specific absolute paths or missing bundled files. Skipped when
	// force=true so the agent can override after reviewing.
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
		// Force-refresh the skill scanner cache before upload pre-checks.
		// Stale cache data (up to 10 minutes old) can reference old file
		// paths (e.g., scripts/run.js when the actual file is scripts/run.py),
		// causing false "Missing bundled files" errors during upload preflight.
		if exec := h.getSkillExecutor(); exec != nil {
			exec.invalidateSkillCache()
		}
		// Pre-upload portability gate runs first so the agent fixes path /
		// completeness problems before the usage/quality checks.
		if gate := h.runUploadPortabilityGate(name); gate != "" {
			return gate
		}
		// Ensure skill has a valid skill_id before upload.
		// Auto-generates from uploader email + skill name if not declared.
		if idErr := h.ensureSkillIDForUpload(name); idErr != "" {
			return idErr
		}
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
	return fmt.Sprintf("Skill「%s」已上传到 SkillMarket，提交 ID: %s", name, submissionID)
}

// runUploadPortabilityGate runs the shared pre-upload portability preflight on
// the real skill directory. It returns a non-empty agent-readable message that
// must be returned to the agent (instead of uploading) when the skill is not
// portable; it returns "" when the skill is safe to upload. Auto-fixes are
// persisted to disk and indexes are refreshed so the fixed definition is used.
func (h *IMMessageHandler) runUploadPortabilityGate(name string) string {
	skillDir := h.resolveManagedSkillDir(name)
	if skillDir == "" {
		// No directory-backed skill (e.g. learned/crafted in-config skill);
		// nothing to sanitize. Let the normal upload path handle it.
		return ""
	}

	// Snapshot for rollback so a failed security scan after auto-fix cannot
	// leave the skill dir in a partially-modified state.
	snapshotDir, cleanupSnapshot, snapErr := snapshotSkillDirForRollback(skillDir)
	if snapErr != nil {
		return fmt.Sprintf("上传前可移植性检查失败（无法创建快照）: %s", snapErr.Error())
	}
	defer cleanupSnapshot()

	result, err := cskill.PrepareSkillForUpload(skillDir)
	if err != nil {
		return fmt.Sprintf("上传前可移植性检查失败: %s", err.Error())
	}

	// If auto-fix rewrote files, verify the writeback still passes the
	// install-grade security scan; roll back and report if not.
	if len(result.AutoFixed) > 0 {
		if scanErr := h.scanManagedSkillWriteback(skillDir, name); scanErr != nil {
			if restoreErr := restoreSkillDirFromSnapshot(skillDir, snapshotDir); restoreErr != nil {
				return fmt.Sprintf("可移植性自动修复后安全扫描未通过: %s；回滚失败: %v", scanErr.Error(), restoreErr)
			}
			return fmt.Sprintf("可移植性自动修复触发安全扫描拦截，已回滚改动: %s", scanErr.Error())
		}
		h.refreshSkillIndexesAfterMutation(name)
	}

	if !result.Portable() {
		// Do NOT rollback auto-fixed changes — they are correct and should persist.
		// Only report the remaining BlockingPaths/MissingFiles for the agent to fix.
		// (Rollback only happens above when the security scan rejects the auto-fix.)
		// Drop .bak files left by the fixer so packages stay clean when upload is still blocked.
		cleanupPortabilityBackupFiles(skillDir)
		return cskill.FormatUploadPreflight(result)
	}
	cleanupPortabilityBackupFiles(skillDir)
	return ""
}

func cleanupPortabilityBackupFiles(skillDir string) {
	for _, name := range []string{"skill.yaml.bak", "skill.yml.bak", "skill.json.bak", "skill.md.bak", "SKILL.md.bak"} {
		_ = os.Remove(filepath.Join(skillDir, name))
	}
}

// resolveManagedSkillDir resolves the on-disk directory of a registered skill
// by name, falling back to PrimarySkillsDir/<name>. Returns "" when no
// directory-backed skill is found.
func (h *IMMessageHandler) resolveManagedSkillDir(name string) string {
	if exec := h.getSkillExecutor(); exec != nil {
		for _, s := range exec.loadSkills() {
			if s.MatchesName(name) && strings.TrimSpace(s.SkillDir) != "" {
				return s.SkillDir
			}
		}
	}
	if primaryDir, err := cskill.PrimarySkillsDir(); err == nil {
		candidate := filepath.Join(primaryDir, name)
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ensureSkillIDForUpload ensures the skill has a valid skill_id before upload.
// If the skill doesn't have one, it is auto-generated from the uploader's email
// and the skill name, then persisted to skill.yaml.
// Returns an agent-readable error message if id generation fails, or "" on success.
func (h *IMMessageHandler) ensureSkillIDForUpload(name string) string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return ""
	}
	var target *corelib.NLSkillEntry
	for _, s := range exec.loadSkills() {
		if s.MatchesName(name) {
			cp := s
			target = &cp
			break
		}
	}
	if target == nil {
		return "" // skill not found — let the upload path report the proper error
	}

	// Already has a valid skill_id — nothing to do
	if target.SkillID != "" && cskill.IsValidSkillID(target.SkillID) {
		return ""
	}

	// Get uploader email from config
	uploaderEmail := ""
	if h.app != nil {
		cfg, err := h.app.LoadConfig()
		if err == nil {
			uploaderEmail = cfg.RemoteEmail
		}
	}

	skillID, err := cskill.EnsureSkillIDBeforeUpload(target, uploaderEmail)
	if err != nil {
		return fmt.Sprintf("Skill ID 生成失败: %s\n\n"+
			"请在 skill.yaml 中手动声明 id 字段（格式: publisher.skill-name，如 myname.%s）。\n"+
			"id 一旦上传将与你的账户绑定，不可更改。",
			err.Error(), cskill.SanitizeSkillNameForID(name))
	}

	// Refresh indexes so subsequent operations see the new id
	h.refreshSkillIndexesAfterMutation(name)

	log.Printf("[upload] skill %q assigned id %q", name, skillID)
	return ""
}

// checkSkillIDChanged verifies that a modified skill definition did not change
// or remove the skill_id field. Returns a non-empty error message if the id was
// tampered with, or "" if the id is preserved.
// Only checks top-level "id:" lines (no leading whitespace) to avoid matching
// nested fields like step params that might contain "id:".
func checkSkillIDChanged(modifiedContent []byte, originalID string) string {
	content := string(modifiedContent)
	for _, line := range strings.Split(content, "\n") {
		// Only match top-level id field (no leading whitespace)
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "id:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "id:"))
		value = strings.Trim(value, `"'`)
		if value == "" {
			// id field present but empty value
			return fmt.Sprintf("错误：patch 后 skill id 值为空（原 id: %s）。"+
				"skill id 不可删除。", originalID)
		}
		if !strings.EqualFold(value, originalID) {
			return fmt.Sprintf("错误：skill id 不可修改（当前 id: %s，patch 后变为: %s）。"+
				"如果需要更换 id，请创建新 skill 并迁移。", originalID, value)
		}
		return "" // id field found and matches
	}
	// id field not found in modified content — it was removed
	return fmt.Sprintf("错误：patch 后 skill id 字段丢失（原 id: %s）。"+
		"skill id 不可删除。请确保 patch 保留 id 字段。", originalID)
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

	snapshotDir, cleanupSnapshot, snapshotErr := snapshotSkillDirForRollback(skillDir)
	if snapshotErr != nil {
		return fmt.Sprintf("automatic repair snapshot failed: %s\n\n%s", snapshotErr.Error(), cskill.FormatPortabilityReport(report))
	}
	defer cleanupSnapshot()

	changes, fixErr := cskill.AutoFixPortability(skillDir)
	if fixErr != nil {
		return fmt.Sprintf("automatic repair failed: %s\n\n%s", fixErr.Error(), cskill.FormatPortabilityReport(report))
	}

	// Re-validate and run install-grade security scan before keeping writeback.
	finalReport, revalidateErr := cskill.ValidateSkillPortability(skillDir)
	if revalidateErr != nil {
		if restoreErr := restoreSkillDirFromSnapshot(skillDir, snapshotDir); restoreErr != nil {
			return fmt.Sprintf("repair re-validation failed: %s; rollback failed: %v\n\n%s", revalidateErr.Error(), restoreErr, cskill.FormatPortabilityChanges(changes))
		}
		return fmt.Sprintf("repair re-validation failed and changes were rolled back: %s\n\n%s", revalidateErr.Error(), cskill.FormatPortabilityChanges(changes))
	}
	if scanErr := h.scanManagedSkillWriteback(skillDir, name); scanErr != nil {
		if restoreErr := restoreSkillDirFromSnapshot(skillDir, snapshotDir); restoreErr != nil {
			return fmt.Sprintf("repair blocked by security scan: %s; rollback failed: %v\n\n%s", scanErr.Error(), restoreErr, cskill.FormatPortabilityChanges(changes))
		}
		return fmt.Sprintf("repair blocked by security scan and changes were rolled back: %s\n\n%s", scanErr.Error(), cskill.FormatPortabilityChanges(changes))
	}
	var b strings.Builder
	b.WriteString(cskill.FormatPortabilityChanges(changes))
	b.WriteByte('\n')
	b.WriteString(cskill.FormatPortabilityReport(finalReport))
	h.refreshSkillIndexesAfterMutation(name)
	return b.String()
}

func snapshotSkillDirForRollback(skillDir string) (string, func(), error) {
	if strings.TrimSpace(skillDir) == "" {
		return "", func() {}, fmt.Errorf("skill directory is empty")
	}
	parent := filepath.Dir(skillDir)
	snapshotDir, err := os.MkdirTemp(parent, ".skill-rollback-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(snapshotDir) }
	if err := copyDirContents(skillDir, snapshotDir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return snapshotDir, cleanup, nil
}

func restoreSkillDirFromSnapshot(skillDir, snapshotDir string) error {
	if strings.TrimSpace(skillDir) == "" || strings.TrimSpace(snapshotDir) == "" {
		return fmt.Errorf("skill directory and snapshot directory are required")
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return err
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	return copyDirContents(snapshotDir, skillDir)
}

func (h *IMMessageHandler) scanManagedSkillWriteback(skillDir, skillName string) error {
	entry, err := loadImportedSkillEntry(skillDir)
	if err != nil {
		return fmt.Errorf("reload repaired skill: %w", err)
	}
	if strings.TrimSpace(entry.Name) == "" {
		entry.Name = skillName
	}
	entry.SkillDir = skillDir
	if h != nil && h.app != nil && h.app.isRiskGuardrailOffMode() {
		h.app.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillUpdate,
			"manage_skill_autofix",
			security.RiskLow,
			security.PolicyAllow,
			fmt.Sprintf("risk guardrails off allowed skill autofix writeback for %s", entry.Name),
		)
		return nil
	}
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanStaged(context.Background(), entry, skillDir, func(status string) {
		if h != nil && h.app != nil {
			h.app.log(fmt.Sprintf("[manage_skill] security scan %s: %s", entry.Name, status))
		}
	})
	if report == nil {
		if h != nil && h.app != nil && !h.app.skillInstallMissingScanShouldBlock() {
			h.app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillUpdate,
				"manage_skill_autofix",
				security.RiskCritical,
				security.PolicyAudit,
				fmt.Sprintf("skill autofix writeback allowed for %s even though scan report was missing", entry.Name),
			)
			return nil
		}
		return fmt.Errorf("security scan produced no report")
	}
	if report.NeedsUserReview() {
		if h != nil && h.app != nil {
			h.app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillReject,
				"manage_skill_autofix",
				report.FinalLevel,
				security.PolicyDeny,
				fmt.Sprintf("skill autofix writeback requires manual review for %s: %s", entry.Name, report.Summary),
			)
		}
		return fmt.Errorf("level=%s summary=%s", report.FinalLevel, report.Summary)
	}
	if h != nil && h.app != nil && h.app.skillInstallScanShouldBlock(report) {
		if h != nil && h.app != nil {
			h.app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillReject,
				"manage_skill_autofix",
				report.FinalLevel,
				security.PolicyDeny,
				fmt.Sprintf("skill autofix writeback rejected for %s: %s", entry.Name, report.Summary),
			)
		}
		return fmt.Errorf("level=%s summary=%s", report.FinalLevel, report.Summary)
	}
	if err := writeSkillScanCacheForInstalledEntry(entry, report); err != nil {
		return fmt.Errorf("write skill scan cache: %w", err)
	}
	return nil
}
func (h *IMMessageHandler) toolParallelExecute(args map[string]interface{}) string {
	return "[system rejected] parallel_execute is disabled for external coding sessions. Coding tasks must run through the internal CodingSubAgent."
}

func formatQueuedSessionResultLine(key string, sr SessionResult) string {
	status := sr.Status.String()
	skipped := isQueuedSessionSkipped(sr)
	if skipped {
		status = "skipped"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- %s: tool=%s status=%s", key, sr.Tool, status))
	if sr.SessionID != "" {
		b.WriteString(fmt.Sprintf(" session=%s", sr.SessionID))
	}
	if sr.Error != "" {
		label := "error"
		value := sr.Error
		if skipped {
			label = "reason"
			value = queuedSessionSkipReason(sr)
		}
		b.WriteString(fmt.Sprintf(" %s=%s", label, value))
	}
	return b.String()
}

func queuedSessionSkipReason(sr SessionResult) string {
	const prefix = "skipped:"
	errorText := strings.TrimSpace(sr.Error)
	if strings.HasPrefix(strings.ToLower(errorText), prefix) {
		return strings.TrimSpace(errorText[len(prefix):])
	}
	return errorText
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
	for _, tool := range []string{"claude", "codex", "opencode", "iflow", "kilo"} {
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
	bashMinTimeout          = 240
	bashDefaultTimeout      = corelib.DefaultAgentTimeoutSec
	bashMaxTimeout          = 600
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
	if timeout < bashMinTimeout {
		timeout = bashMinTimeout
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
// to the effective user working directory. When p is empty, returns that base.
func resolvePath(p string) string {
	return resolvePathWithBase(p, corelib.EffectiveWorkspaceDir())
}

func resolvePathWithBase(p, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = corelib.EffectiveWorkspaceDir()
	}
	if p == "" {
		return filepath.Clean(base)
	}
	p = corelib.ExpandHomePath(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// pathContainedInBase reports whether absPath is the base directory itself or a
// descendant. Used to keep download_file / web_fetch(save_path) inside workdir.
func pathContainedInBase(absPath, base string) bool {
	absPath = filepath.Clean(strings.TrimSpace(absPath))
	base = filepath.Clean(strings.TrimSpace(base))
	if absPath == "" || base == "" {
		return false
	}
	// Windows paths are case-insensitive; normalize before Rel so C:\Work vs
	// c:\work\file is treated as contained.
	if runtime.GOOS == "windows" {
		absPath = strings.ToLower(absPath)
		base = strings.ToLower(base)
	}
	rel, err := filepath.Rel(base, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../")
}

// resolveDownloadSavePath sanitizes a relative save_path, resolves against base,
// and rejects absolute paths that escape the session workbench.
// Returns (absPath, errMsg). errMsg non-empty means the caller should return it.
func resolveDownloadSavePath(savePath, base string) (string, string) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = corelib.EffectiveWorkspaceDir()
	}
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		return "", "缺少 save_path"
	}
	expanded := corelib.ExpandHomePath(savePath)
	if !filepath.IsAbs(expanded) {
		savePath = sanitizeDownloadSavePath(savePath)
	} else {
		savePath = expanded
	}
	abs := resolvePathWithBase(savePath, base)
	if !pathContainedInBase(abs, base) {
		return "", fmt.Sprintf("save_path 必须位于工作目录内: workdir=%s path=%s", base, abs)
	}
	return abs, ""
}

// sanitizeDownloadSavePath cleans a user/agent-provided relative save path:
// drops ".." segments and sanitizes each component. Absolute paths should be
// handled by resolveDownloadSavePath instead.
func sanitizeDownloadSavePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	// Normalize separators then sanitize each component.
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			// Drop parent traversal segments in relative save paths.
			continue
		}
		out = append(out, sanitizeDownloadFileName(part))
	}
	if len(out) == 0 {
		return "download.bin"
	}
	return strings.Join(out, string(filepath.Separator))
}

// downloadFileNameFromURL derives a safe local filename from a URL.
// Uses net/url + path.Base (slash paths) so Windows filepath.Base is not applied
// to "https://host/a/b.pdf" (which has no '\' and would return the whole URL).
func downloadFileNameFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "download.bin"
	}
	if u, err := url.Parse(rawURL); err == nil {
		scheme := strings.ToLower(u.Scheme)
		if scheme == "http" || scheme == "https" || scheme == "ftp" {
			name := pathpkg.Base(strings.TrimSuffix(u.Path, "/"))
			if name == "" || name == "." || name == "/" {
				return "download.bin"
			}
			return sanitizeDownloadFileName(name)
		}
	}
	// Fallback for non-URL / relative strings.
	s := strings.TrimSpace(strings.SplitN(rawURL, "?", 2)[0])
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return sanitizeDownloadFileName(s)
}

// sanitizeDownloadFileName strips path separators and Windows-illegal characters
// from a URL-derived basename so download_file never writes outside workdir.
func sanitizeDownloadFileName(name string) string {
	name = strings.TrimSpace(name)
	// Use slash-based basename first: filepath.Base treats "a:b.pdf" as a drive
	// relative path on Windows and would drop the "a" segment.
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" || name == "." {
		return "download.bin"
	}
	// Replace characters illegal on Windows and awkward on all platforms.
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch r {
		case '<', '>', ':', '"', '|', '?', '*', '/', '\\':
			b.WriteByte('_')
		default:
			if r < 32 {
				b.WriteByte('_')
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.Trim(out, " .")
	if out == "" || out == "." || out == ".." {
		return "download.bin"
	}
	// Cap extreme lengths (NTFS max component ~255).
	if len(out) > 200 {
		ext := filepath.Ext(out)
		base := strings.TrimSuffix(out, ext)
		if len(base) > 200-len(ext) {
			base = base[:200-len(ext)]
		}
		out = base + ext
	}
	return out
}

// toolDownloadBaseDir is the session workbench used for downloads. Same
// resolution as bash/write_file (project tab owner → top-bar working directory).
func (h *IMMessageHandler) toolDownloadBaseDir() string {
	owner := ""
	if h != nil {
		owner = h.currentRuntimeOrLegacyPolicyOwnerID()
	}
	return h.toolDownloadBaseDirForOwner(owner)
}

// toolDownloadBaseDirForOwner resolves download landing dir for a session owner.
func (h *IMMessageHandler) toolDownloadBaseDirForOwner(ownerID string) string {
	if h != nil {
		if wd := strings.TrimSpace(h.resolveToolWorkDirForOwner("", ownerID)); wd != "" {
			return wd
		}
		if h.app != nil {
			if wd := strings.TrimSpace(h.app.EffectiveWorkingDirForOwner(ownerID)); wd != "" {
				return wd
			}
		}
	}
	return corelib.EffectiveWorkspaceDir()
}

// toolGeneratePDF generates a PDF from Markdown content and returns it as a
// base64-encoded file payload. Only intended for coding workflow documents
// (requirements, design, task plan).
func (h *IMMessageHandler) toolGeneratePDF(args map[string]interface{}) string {
	content := stringVal(args, "content")
	title := stringVal(args, "title")
	docType := stringVal(args, "doc_type")
	phaseID := stringVal(args, "phase_id")

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

	dt := workflowPDFDocTypeFromMetadata(docType, phaseID)

	b64Data, fileName, err := gen.GenerateAndEncode(dt, title, content)
	if err != nil {
		return fmt.Sprintf("PDF 生成失败: %s", err.Error())
	}

	if msgFlag := workflowDocDeliveryMessagePayloadFlag(args); msgFlag != "" {
		return fmt.Sprintf("[file_base64|%s|application/pdf|%s]%s", fileName, msgFlag, b64Data)
	}
	return fmt.Sprintf("[file_base64|%s|application/pdf]%s", fileName, b64Data)
}

// toolMemory merges save/list/delete/recall memory operations into a single tool.
func (h *IMMessageHandler) toolMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "long-term memory is not initialized"
	}
	var projectPath string
	if h.contextResolver != nil {
		projectPath, _ = h.contextResolver.ResolveProject()
	}
	ownerID, explicitRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if ownerID == "" && explicitRuntimeOwner {
		return "memory owner is missing; isolated runtime will not fall back to desktop memory"
	}
	if ownerID == "" {
		if _, explicitRuntime := h.currentRuntimePolicyOwnerState(); explicitRuntime {
			return "memory owner is missing; isolated runtime will not fall back to desktop memory"
		}
	}
	if ownerID == "" {
		ownerID = desktopUserID
	}
	return corememory.HandleTool(h.memoryStore, args, corememory.ToolOptions{
		ProjectPath: projectPath,
		ContextHint: h.buildMemoryContextHintForUser(ownerID),
		OwnerID:     ownerID,
		LoopID:      h.currentLoopIDForUser(ownerID),
		AfterWrite: func() {
			if h.app != nil {
				h.app.triggerMemoryPipelineSoon(45 * time.Second)
			}
			// Invalidate the frozen memory snapshot so the next message's
			// system prompt reflects the newly saved/deleted memory.
			// Without this, user_fact changes (e.g. "remember my name is X")
			// are invisible to the LLM until the next /new or topic switch.
			h.RefreshMemorySnapshot(ownerID)
		},
	})
}

// buildMemoryContextHint extracts recent conversation text (last 5 user+assistant
// messages) to provide alias and context terms for tag enrichment during memory save.
func (h *IMMessageHandler) buildMemoryContextHint() string {
	if _, explicitRuntime := h.currentRuntimePolicyOwnerState(); explicitRuntime {
		return ""
	}
	return h.buildMemoryContextHintForUser(desktopUserID)
}

func (h *IMMessageHandler) buildMemoryContextHintForUser(userID string) string {
	if h.memory == nil {
		return ""
	}
	if userID == "" {
		userID = desktopUserID
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

// currentLoopIDForUser returns the active agent loop ID for the given user,
// or empty string if no loop is running. Used to populate ToolOptions.LoopID
// for scroll session scoping.
func (h *IMMessageHandler) currentLoopIDForUser(userID string) string {
	if ctx := h.getSessionLoopCtx(userID); ctx != nil {
		return ctx.ID
	}
	return ""
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
		if yolo, err := strconv.ParseBool(strings.TrimSpace(yoloStr)); err == nil {
			tpl.YoloMode = yolo
		}
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
	name := stringVal(args, "template_name")
	if name == "" {
		return "\u7f3a\u5c11 template_name \u53c2\u6570"
	}
	if h.templateManager == nil {
		return "\u6a21\u677f\u7ba1\u7406\u5668\u672a\u521d\u59cb\u5316"
	}
	if _, err := h.templateManager.Get(name); err != nil {
		return fmt.Sprintf("\u83b7\u53d6\u6a21\u677f\u5931\u8d25: %s", err.Error())
	}
	return "[system rejected] launch_template is disabled for external coding sessions. Coding tasks must run through the internal CodingSubAgent."
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
	actionText := stringVal(args, "action")
	action := normalizeManageConfigAction(actionText)
	switch action {
	case manageConfigActionGet:
		return h.toolGetConfig(args)
	case manageConfigActionSet:
		return h.toolUpdateConfig(args)
	case manageConfigActionBatch:
		return h.toolBatchUpdateConfig(args)
	case manageConfigActionSchema:
		return h.toolListConfigSchema()
	case manageConfigActionExport:
		return h.toolExportConfig()
	case manageConfigActionImport:
		return h.toolImportConfig(args)
	default:
		return fmt.Sprintf("未知 manage_config action: %s（支持: get/set/batch/schema/export/import）", action)
	}
}

// toolManageTemplate dispatches the merged manage_template tool to individual handlers.
func (h *IMMessageHandler) toolManageTemplate(args map[string]interface{}) string {
	actionText := stringVal(args, "action")
	action := normalizeManageTemplateAction(actionText)
	switch action {
	case manageTemplateActionCreate:
		return h.toolCreateTemplate(args)
	case manageTemplateActionList:
		return h.toolListTemplates()
	case manageTemplateActionLaunch:
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
	actionText := stringVal(args, "action")
	// Tool schema has both "action" (CRUD verb) and "task_action" (what the job
	// should do). Only swap when the model put the CRUD verb in task_action —
	// never overwrite action=create with natural-language work content.
	if ta := stringVal(args, "task_action"); ta != "" {
		if normalized := normalizeManageScheduleAction(ta); normalized != manageScheduleActionUnknown {
			args["task_action"] = actionText
			actionText = ta
			args["action"] = ta
		}
	}
	action := normalizeManageScheduleAction(actionText)
	switch action {
	case manageScheduleActionCreate:
		return h.toolCreateScheduledTask(args)
	case manageScheduleActionList:
		return h.toolListScheduledTasks()
	case manageScheduleActionDelete:
		return h.toolDeleteScheduledTask(args)
	case manageScheduleActionUpdate:
		return h.toolUpdateScheduledTask(args)
	case manageScheduleActionListTargets:
		return h.toolListScheduleDeliveryTargets(args)
	default:
		return fmt.Sprintf("未知 manage_schedule action: %s（支持: create/list/delete/update/list_targets）", actionText)
	}
}

// toolSetMaxIterations allows the agent to dynamically adjust the max
// iterations for the current conversation loop. This does NOT change the
// persisted config — it only affects the in-flight loop.
func (h *IMMessageHandler) toolSetMaxIterations(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if !hasRuntimeOwner {
		return "set_max_iterations failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	if hasRuntimeOwner && ownerID == "" {
		return "set_max_iterations failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	n, ok := args["max_iterations"].(float64)
	if !ok || n < 1 {
		return fmt.Sprintf("缺少或无效的 max_iterations 参数（需要 %d-%d 的整数）", config.MinAgentIterations, config.MaxAgentIterationsCap)
	}
	// Use the single source of truth for value normalization.
	limit := config.EffectiveMaxIterations(int(n))
	reason := stringVal(args, "reason")
	if ctx := h.runtimeLoopContextForOwner(ownerID); ctx != nil {
		ctx.SetMaxIterations(limit)
	}

	if reason != "" {
		return fmt.Sprintf("已将当前任务最大轮数调整为 %d（仅本次任务生效，原因: %s）", limit, reason)
	}
	return fmt.Sprintf("已将当前任务最大轮数调整为 %d（仅本次任务生效）", limit)
}

// ---------------------------------------------------------------------------
// Nickname (set_nickname) tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSetNickname(args map[string]interface{}) string {
	requestText := stringVal(args, "_user_text")
	if requestText == "" && h != nil {
		requestText, _ = h.currentRuntimeTaskTextOrLegacy()
	}
	if h == nil || !isExplicitNicknameRequest(requestText) {
		return "[system rejected] set_nickname 仅在用户明确要求改名或给你起名字时可用。不要主动给自己起昵称；昵称为空时等待 Hub 自动分配。"
	}
	nickname := strings.TrimSpace(stringVal(args, "nickname"))
	if nickname == "" {
		return "nickname 不能为空"
	}
	// Persist only nickname so concurrent settings edits are not overwritten.
	if cfg, err := h.loadConfig(); err == nil {
		if strings.TrimSpace(cfg.RemoteNickname) == nickname {
			return fmt.Sprintf("昵称已经是「%s」，无需重复上报。", nickname)
		}
	}
	if h.app != nil {
		_ = h.app.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.RemoteNickname = nickname
		})
	}
	// Send to Hub via WebSocket.
	if hc := h.getHubClient(); hc != nil {
		if err := hc.SendNicknameUpdate(nickname); err != nil {
			log.Printf("[set_nickname] SendNicknameUpdate error: %v", err)
			return fmt.Sprintf("昵称已保存到本地（%s），但上报 Hub 失败：%v", nickname, err)
		}
	}
	return fmt.Sprintf("昵称已更新为「%s」，Hub 已同步。", nickname)
}

func isExplicitNicknameRequest(text string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return false
	}
	patterns := []string{
		"你叫", "以后叫你", "以后你叫", "以后就叫", "叫你", "给你起名", "给你起个名", "给你取名", "给你取个名",
		"把你的昵称", "把你昵称", "你的昵称改", "昵称改为", "昵称改成", "昵称设为", "昵称设置为", "昵称叫",
		"你的名字是", "名字叫", "改名叫", "重命名为",
		"call you", "your name is", "rename you", "set your nickname", "change your nickname", "nickname is", "you are named",
	}
	for _, pattern := range patterns {
		if strings.Contains(s, pattern) {
			return true
		}
	}
	return false
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
	return fmt.Sprintf("已将 LLM 服务商切换为 %s (model=%s)", match.Name, match.Model)
}

// ---------------------------------------------------------------------------
// Scheduled task tool implementations
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	name := stringVal(args, "name")
	// Prefer task_action (work content). "action" is the CRUD verb for manage_schedule
	// and must not be used as the scheduled payload when task_action is set.
	taskAction := stringVal(args, "task_action")
	if taskAction == "" {
		// Legacy / alias path: create_scheduled_task may pass content as "action".
		if normalizeManageScheduleAction(stringVal(args, "action")) == manageScheduleActionUnknown {
			taskAction = stringVal(args, "action")
		}
	}
	if name == "" || taskAction == "" {
		return "缺少 name 或 task_action 参数（task_action 为到点要执行的内容）"
	}
	action := taskAction
	hour := scheduleIntArg(args, "hour", -1)
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := scheduleIntArg(args, "minute", 0)
	dow := scheduleIntArg(args, "day_of_week", -1)
	dom := scheduleIntArg(args, "day_of_month", -1)
	intervalMin := scheduleIntArg(args, "interval_minutes", 0)

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
	d, derr := h.parseAndResolveScheduleDelivery(args)
	if derr != nil {
		return derr.Error()
	}
	t.Delivery = d

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

	// Format next run time for display (reuse calendar Get when possible).
	task := h.scheduledTaskManager.Get(id)
	if task == nil {
		return fmt.Sprintf("定时任务已创建（ID: %s）", id)
	}
	extra := ""
	if s := scheduler.SummarizeDelivery(task.Delivery); s != "" {
		extra = "\n推送: " + s
	}
	if task.NextRunAt != nil {
		return fmt.Sprintf("定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s%s", id, name, action, task.NextRunAt.Format("2006-01-02 15:04"), extra)
	}
	return fmt.Sprintf("定时任务已创建（ID: %s）%s", id, extra)
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
		b.WriteString(fmt.Sprintf("[%s] %s\n", t.ID, t.Name))
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
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			b.WriteString(fmt.Sprintf("\n   推送: %s", s))
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
	return "定时任务已删除"
}

func (h *IMMessageHandler) toolUpdateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	// Manager.Update uses key "action" for work content. manage_schedule also
	// passes CRUD action=update in the same map — never persist that as the job body.
	if ta := stringVal(args, "task_action"); ta != "" {
		args["action"] = ta
	} else if normalizeManageScheduleAction(stringVal(args, "action")) != manageScheduleActionUnknown {
		delete(args, "action")
	}
	// Full replace or partial patch (fail_on_error) without wiping delivery.
	var current *scheduler.TaskDelivery
	if cur := h.scheduledTaskManager.Get(id); cur != nil {
		current = cur.Delivery
	}
	if err := scheduler.PrepareDeliveryForUpdate(current, args, func(a map[string]interface{}) (*scheduler.TaskDelivery, error) {
		return h.parseAndResolveScheduleDelivery(a)
	}); err != nil {
		return err.Error()
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
		extra := ""
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			extra = "\n推送: " + s
		}
		return fmt.Sprintf("定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s%s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next, extra)
	}
	return "定时任务已更新"
}

// toolListScheduleDeliveryTargets lists push destinations for a channel (generic).
// Args: channel (default lansenger), query (optional name/id filter).
func (h *IMMessageHandler) toolListScheduleDeliveryTargets(args map[string]interface{}) string {
	if h.app == nil {
		return "应用未初始化，无法查询投递目标"
	}
	channel := scheduler.DefaultDeliveryChannel(firstNonEmptyArg(
		stringVal(args, "channel"),
		stringVal(args, "platform"),
	))
	query := firstNonEmptyArg(
		stringVal(args, "query"),
		stringVal(args, "group_name"),
		stringVal(args, "name"),
	)
	text, err := h.app.listScheduleDeliveryTargets(channel, query)
	if err != nil {
		return fmt.Sprintf("查询投递目标失败: %s", err.Error())
	}
	return text
}

// parseAndResolveScheduleDelivery builds delivery from args (full object or shorthand)
// and resolves group_name → group_id via the channel's target catalog.
func (h *IMMessageHandler) parseAndResolveScheduleDelivery(args map[string]interface{}) (*scheduler.TaskDelivery, error) {
	d, err := parseScheduleDeliveryArgs(args)
	if err != nil {
		return nil, fmt.Errorf("delivery 配置无效: %w", err)
	}
	if d == nil {
		return nil, nil
	}
	if h.app == nil {
		if err := d.EnsureResolved(); err != nil {
			return nil, fmt.Errorf("%w（无法查询通道目标：app 未初始化）", err)
		}
		return d, nil
	}
	if err := h.app.resolveScheduleDelivery(d); err != nil {
		return nil, err
	}
	return d, nil
}

// scheduleArgsTouchDelivery reports whether args intend to set/clear delivery.
func scheduleArgsTouchDelivery(args map[string]interface{}) bool {
	return scheduler.ArgsTouchDelivery(args)
}

// parseScheduleDeliveryArgs accepts either delivery object or shorthand fields:
//
//	delivery: {enabled, channel, targets:[...]}
//	group_name / delivery_group_name + optional channel / mention_user_ids
//	group_id / delivery_group_id
//	user_id / delivery_user_id
func parseScheduleDeliveryArgs(args map[string]interface{}) (*scheduler.TaskDelivery, error) {
	if args == nil {
		return nil, nil
	}
	if raw, ok := args["delivery"]; ok {
		if raw == nil {
			return nil, nil
		}
		// Explicit empty object / disabled
		if m, ok := raw.(map[string]interface{}); ok {
			if len(m) == 0 {
				return nil, nil
			}
			if en, ok := m["enabled"].(bool); ok && !en {
				return nil, nil
			}
		}
		d, err := scheduler.ParseDeliveryFromAny(raw)
		if err != nil {
			return nil, err
		}
		if d != nil {
			return d, nil
		}
		// Fall through to shorthand when delivery was empty/null-like.
	}

	// Shorthand: build a single-target delivery so the agent need not construct nested JSON.
	groupName := firstNonEmptyArg(
		stringVal(args, "group_name"),
		stringVal(args, "delivery_group_name"),
	)
	groupID := firstNonEmptyArg(
		stringVal(args, "group_id"),
		stringVal(args, "delivery_group_id"),
	)
	userID := firstNonEmptyArg(
		stringVal(args, "user_id"),
		stringVal(args, "delivery_user_id"),
	)
	if groupName == "" && groupID == "" && userID == "" {
		return nil, nil
	}
	channel := firstNonEmptyArg(stringVal(args, "channel"), stringVal(args, "delivery_channel"))
	if channel == "" {
		channel = scheduler.DeliveryChannelLansenger
	}
	d := &scheduler.TaskDelivery{
		Enabled: true,
		Channel: channel,
		On:      scheduler.DeliveryOnSuccess,
	}
	if b, ok := args["fail_on_error"].(bool); ok {
		d.FailOnError = b
	}
	if userID != "" && groupID == "" && groupName == "" {
		d.Targets = []scheduler.DeliveryTarget{{
			Kind:   scheduler.DeliveryKindUser,
			UserID: userID,
		}}
	} else {
		tg := scheduler.DeliveryTarget{
			Kind:      scheduler.DeliveryKindGroup,
			GroupID:   groupID,
			GroupName: groupName,
		}
		if mentions := stringVal(args, "mention_user_ids"); mentions != "" {
			tg.MentionUserIDs = splitDeliveryIDList(mentions)
		}
		if b, ok := args["mention_all"].(bool); ok && b {
			tg.MentionAll = true
		}
		d.Targets = []scheduler.DeliveryTarget{tg}
	}
	d.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

func splitDeliveryIDList(s string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func firstNonEmptyArg(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// scheduleIntArg reads int-like tool args (JSON float64, Go int, etc.).
func scheduleIntArg(args map[string]interface{}, key string, def int) int {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return def
		}
		return int(i)
	default:
		return def
	}
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

func (h *IMMessageHandler) imToolContext() (context.Context, context.CancelFunc) {
	if h != nil && h.currentLoopCtx != nil {
		return h.currentLoopCtx.Context()
	}
	return context.Background(), func() {}
}

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

	ctx, cancel := h.imToolContext()
	defer cancel()
	results, err := websearch.SearchWithProviderCtx(ctx, query, maxResults, provider)
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

// maxWebFetchDownloadBytes allows large PDFs / papers (logs saw 10MB+ arXiv PDFs).
const maxWebFetchDownloadBytes = 100 * 1024 * 1024

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
	// Accept save_path / output / dest so download-oriented callers share one path.
	savePath := ""
	for _, key := range []string{"save_path", "output", "dest", "path"} {
		if v := strings.TrimSpace(stringVal(args, key)); v != "" {
			savePath = v
			break
		}
	}
	// Same owner resolution as download_file / bash so containment checks agree.
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "web_fetch failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	if ownerID == "" && h != nil {
		ownerID = h.currentRuntimeOrLegacyPolicyOwnerID()
	}
	baseDir := h.toolDownloadBaseDirForOwner(ownerID)
	if savePath != "" {
		abs, errMsg := resolveDownloadSavePath(savePath, baseDir)
		if errMsg != "" {
			return errMsg
		}
		opts.SavePath = abs
		opts.MaxBytes = maxWebFetchDownloadBytes
	} else {
		// For text content, allow up to 2MB raw before extraction/windowing.
		opts.MaxBytes = 2 * 1024 * 1024
	}
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		opts.TimeoutS = int(t)
	} else {
		opts.TimeoutS = intArg(args, "timeout", corelib.DefaultAgentTimeoutSec)
	}
	if opts.TimeoutS < corelib.MinAgentTimeoutSec {
		opts.TimeoutS = corelib.MinAgentTimeoutSec
	}
	if opts.TimeoutS > corelib.MaxAgentTimeoutSec {
		opts.TimeoutS = corelib.MaxAgentTimeoutSec
	}

	// Use provider-aware fetch: TinyFish has better content extraction.
	// FetchWithProvider handles TinyFish routing, offset/maxChars windowing, and fallback.
	var fetchProvider corelib.WebSearchProvider
	if opts.SavePath == "" && h != nil && h.app != nil {
		searchCfg := h.app.GetWebSearchProviders()
		if searchCfg.Current == "tinyfish" {
			for _, p := range searchCfg.Providers {
				if p.Type == "tinyfish" && p.Key != "" {
					fetchProvider = p
					break
				}
			}
		}
	}

	ctx, cancel := h.imToolContext()
	defer cancel()
	result, err := websearch.FetchWithProviderCtx(ctx, rawURL, opts, fetchProvider)
	if err != nil {
		return fmt.Sprintf("抓取失败: %s", err.Error())
	}

	// If saved to file, return a stable absolute-path summary for the agent.
	if result.SavedTo != "" {
		saved := strings.TrimSpace(result.SavedTo)
		if saved == "" {
			saved = opts.SavePath
		}
		msg := strings.TrimSpace(result.Content)
		if msg == "" {
			msg = fmt.Sprintf("已保存到: %s", saved)
		}
		if saved != "" && !strings.Contains(msg, saved) {
			msg = fmt.Sprintf("%s\nsaved_path: %s", msg, saved)
		}
		if result.BytesRead > 0 && !strings.Contains(msg, "字节") && !strings.Contains(strings.ToLower(msg), "bytes") {
			msg = fmt.Sprintf("%s\nsize_bytes: %d", msg, result.BytesRead)
		}
		return msg
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

// toolDownloadFile downloads a URL into the session workbench directory.
// Prefer this (or web_fetch with save_path) over installing ClawHub wget/curl skills.
func (h *IMMessageHandler) toolDownloadFile(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "download_file failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	if ownerID == "" && h != nil {
		ownerID = h.currentRuntimeOrLegacyPolicyOwnerID()
	}
	rawURL := strings.TrimSpace(stringVal(args, "url"))
	if rawURL == "" {
		return "缺少 url 参数"
	}
	savePath := ""
	for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
		if v := strings.TrimSpace(stringVal(args, key)); v != "" {
			savePath = v
			break
		}
	}
	if savePath == "" {
		// Default filename from URL path under the working directory.
		savePath = downloadFileNameFromURL(rawURL)
	}
	base := h.toolDownloadBaseDirForOwner(ownerID)
	absPath, errMsg := resolveDownloadSavePath(savePath, base)
	if errMsg != "" {
		return errMsg
	}
	log.Printf("[download_file] url=%q save_path=%q workdir=%q abs=%q owner=%q", rawURL, savePath, base, absPath, ownerID)
	fetchArgs := map[string]interface{}{
		"url":       rawURL,
		"save_path": absPath, // absolute under workdir; toolWebFetch re-checks with same owner base
	}
	if t, ok := args["timeout"]; ok {
		fetchArgs["timeout"] = t
	}
	// Re-inject owner so toolWebFetch resolves the same workbench (consume removed it above).
	if strings.TrimSpace(ownerID) != "" {
		fetchArgs[registeredToolPolicyOwnerIDField] = ownerID
	}
	return h.toolWebFetch(fetchArgs)
}
