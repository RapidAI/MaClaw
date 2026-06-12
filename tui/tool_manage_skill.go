package main

// tool_manage_skill.go provides the TUI implementation of the manage_skill
// tool handler. It is injected into CoreToolDeps.ExtraHandlers["manage_skill"]
// at startup, so the shared RegisterCoreTools picks it up automatically.
//
// The handler reuses existing TUI infrastructure:
//   - corelib/skill.ScanAllSkillDirs() for listing
//   - corelib/skill.ImportMarkdownSkillDir() for step resolution
//   - tui/commands.FileConfigStore for config persistence
//   - corelib/skill.HubClient for search/install (shared with GUI)

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"gopkg.in/yaml.v3"
)

// newManageSkillHandler creates the manage_skill handler bound to the TUI app.
func newManageSkillHandler(app *TUIApp) func(args map[string]interface{}) string {
	return func(args map[string]interface{}) string {
		action, _ := args["action"].(string)
		action = skill.NormalizeManageSkillAction(action)
		switch action {
		case "list":
			return skillList(app)
		case "search":
			return skillSearch(app, args)
		case "install":
			return skillInstall(app, args)
		case "uninstall":
			return skillUninstall(app, args)
		case "run":
			return skillRun(app, args)
		case "status":
			return skillStatus(args)
		case "upload":
			return skillUpload(app, args)
		case "validate":
			return skillValidate(app, args)
		case "patch":
			return skillPatch(app, args)
		case "history":
			return skillPatchHistory(app, args)
		case "maintenance_plan":
			return skillMaintenancePlan(app, args)
		case "execute_maintenance_plan":
			return skillExecuteMaintenancePlan(app, args)
		default:
			return skill.ManageSkillUnknownActionError(action)
		}
	}
}

func sval(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

// --- list ---

func skillList(app *TUIApp) string {
	known := make(map[string]bool)
	var skills []corelib.NLSkillEntry
	for _, s := range app.appConfig.NLSkills {
		skills = append(skills, s)
		known[s.Name] = true
	}
	for _, s := range skill.ScanAllSkillDirs() {
		if !known[s.Name] {
			skills = append(skills, s)
			known[s.Name] = true
		}
	}
	if len(skills) == 0 {
		return "本地没有已注册的 Skill。\n提示：使用 manage_skill(action=\"search\", query=\"关键词\") 搜索并安装。"
	}
	var b strings.Builder
	b.WriteString("=== 本地已注册 Skill ===\n")
	for _, s := range skills {
		line := fmt.Sprintf("- %s", s.Name)
		if s.Publisher != "" {
			line = fmt.Sprintf("- %s:%s", s.Publisher, s.Name)
		}
		line += fmt.Sprintf(" [%s]: %s", s.Status, s.Description)
		if s.UsageCount > 0 {
			rate := float64(s.SuccessCount) / float64(s.UsageCount) * 100
			line += fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, rate)
		}
		if labels := tuiSkillHealthLabels(s); len(labels) > 0 {
			line += " " + strings.Join(labels, " ")
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func tuiSkillHealthLabels(s corelib.NLSkillEntry) []string {
	labels := make([]string, 0, 3)
	if s.UsageCount >= 3 && s.SuccessCount == 0 {
		labels = append(labels, "[needs_review]")
	} else if s.UsageCount >= 5 && float64(s.SuccessCount)/float64(s.UsageCount) >= 0.8 && strings.TrimSpace(s.LastError) == "" {
		labels = append(labels, "[healthy]")
	}
	if tuiSkillHasIncompleteContract(s) {
		labels = append(labels, "[missing_contract]")
	}
	return labels
}

func tuiSkillHasIncompleteContract(s corelib.NLSkillEntry) bool {
	return skill.HasIncompleteSkillContract(s.Type, s.Steps, s.Params, s.RequiredArgs)
}

func skillMaintenancePlan(app *TUIApp, args map[string]interface{}) string {
	skills := tuiCollectMaintenanceSkills(app)
	plan := skill.BuildSkillMaintenancePlan(skills, skill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      tuiIntArg(args, "stale_after_days"),
		MinFailureRuns:      tuiIntArg(args, "min_failure_runs"),
		MaxActions:          tuiIntArg(args, "max_actions"),
		DuplicateSimilarity: tuiFloatArg(args, "duplicate_similarity"),
	})
	payload := map[string]interface{}{
		"ok":                      true,
		"non_executing":           true,
		"boundary":                "read-only skill maintenance plan; no skill was modified, archived, merged, deleted, installed, or executed",
		"maintenance_plan_status": "local_skill_maintenance_plan_no_llm",
		"plan":                    plan,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("生成 Skill 维护计划失败: %v", err)
	}
	return string(data)
}

func skillExecuteMaintenancePlan(app *TUIApp, args map[string]interface{}) string {
	dryRun := tuiBoolArg(args, "dry_run", true)
	if !dryRun && !tuiBoolArg(args, "confirm", false) {
		return `{"ok":false,"dry_run":true,"error":"confirm=true is required when dry_run=false"}`
	}
	approvedActions := tuiStringListArg(args, "approved_actions")
	if !dryRun && len(approvedActions) == 0 {
		return `{"ok":false,"dry_run":true,"error":"approved_actions is required when dry_run=false"}`
	}
	skills := tuiCollectMaintenanceSkills(app)
	plan := skill.BuildSkillMaintenancePlan(skills, skill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      tuiIntArg(args, "stale_after_days"),
		MinFailureRuns:      tuiIntArg(args, "min_failure_runs"),
		MaxActions:          tuiIntArg(args, "max_actions"),
		DuplicateSimilarity: tuiFloatArg(args, "duplicate_similarity"),
	})
	updated, result := skill.ExecuteSkillMaintenancePlan(skills, plan, skill.SkillMaintenanceExecutionOptions{
		Now:                  time.Now(),
		DryRun:               dryRun,
		ApprovedActions:      approvedActions,
		AllowDuplicateRetire: tuiBoolArg(args, "allow_duplicate_retire", false),
	})
	repairTargets := tuiMaintenanceRepairTargets(updated, result)
	triggeredRepairs := 0
	if !dryRun && result.OK {
		app.appConfig.NLSkills = tuiPersistableMaintenanceSkills(updated)
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		if err := store.SaveConfig(app.appConfig); err != nil {
			return fmt.Sprintf("Skill maintenance execution failed: %v", err)
		}
		for _, target := range repairTargets {
			cp := target
			if commands.MaybeRepairSkillTUI(&cp, app.appConfig, store) {
				triggeredRepairs++
			}
		}
	}
	payload := map[string]interface{}{
		"ok":                           result.OK,
		"dry_run":                      result.DryRun,
		"boundary":                     result.Boundary,
		"error":                        result.Error,
		"plan_summary":                 plan.Summary,
		"self_repair_triggers_started": triggeredRepairs,
		"result":                       result,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Skill maintenance execution failed: %v", err)
	}
	return string(data)
}

func tuiMaintenanceRepairTargets(skills []corelib.NLSkillEntry, result skill.SkillMaintenanceExecutionResult) []corelib.NLSkillEntry {
	wanted := make(map[string]bool)
	for _, action := range result.Actions {
		if action.Action == skill.MaintenanceActionAttemptRepair && action.Status == skill.MaintenanceExecutionStatusQueued {
			wanted[action.Skill] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	targets := make([]corelib.NLSkillEntry, 0, len(wanted))
	for _, entry := range skills {
		for name := range wanted {
			if entry.MatchesName(name) || entry.Name == name {
				targets = append(targets, entry)
				delete(wanted, name)
				break
			}
		}
	}
	return targets
}

func tuiCollectMaintenanceSkills(app *TUIApp) []corelib.NLSkillEntry {
	known := make(map[string]bool)
	fileOverlays := make(map[string]corelib.NLSkillEntry)
	var skills []corelib.NLSkillEntry
	for _, s := range app.appConfig.NLSkills {
		key := tuiMaintenanceSkillKey(s)
		if strings.EqualFold(strings.TrimSpace(s.Source), "file") {
			fileOverlays[key] = s
			continue
		}
		skills = append(skills, s)
		known[key] = true
	}
	for _, s := range skill.ScanAllSkillDirsWithExternal(app.appConfig.ExternalSkillDirs) {
		key := tuiMaintenanceSkillKey(s)
		if overlay, ok := fileOverlays[key]; ok {
			s = tuiApplyFileSkillRuntimeOverlay(s, overlay)
			delete(fileOverlays, key)
		}
		if !known[key] {
			skills = append(skills, s)
			known[key] = true
		}
	}
	for key, overlay := range fileOverlays {
		if !known[key] {
			skills = append(skills, overlay)
		}
	}
	return skills
}

func tuiApplyFileSkillRuntimeOverlay(base, overlay corelib.NLSkillEntry) corelib.NLSkillEntry {
	base.Status = overlay.Status
	base.UsageCount = overlay.UsageCount
	base.SuccessCount = overlay.SuccessCount
	base.FailureCount = overlay.FailureCount
	base.WorkaroundCount = overlay.WorkaroundCount
	base.LastUsedAt = overlay.LastUsedAt
	base.LastError = overlay.LastError
	base.RepairAttemptCount = overlay.RepairAttemptCount
	base.LastRepairAt = overlay.LastRepairAt
	base.RepairHistory = append([]corelib.SkillRepairRecord(nil), overlay.RepairHistory...)
	return base
}

func tuiMaintenanceSkillKey(s corelib.NLSkillEntry) string {
	if strings.TrimSpace(s.SkillDir) != "" {
		return "dir:" + tuiMaintenanceSkillDirKey(s.SkillDir)
	}
	if strings.TrimSpace(s.Name) != "" {
		return "name:" + tuiMaintenanceSkillNameKey(s.Name)
	}
	return "dir_name:" + tuiMaintenanceSkillNameKey(s.DirName)
}

func tuiMaintenanceSkillDirKey(dir string) string {
	key := filepath.Clean(strings.TrimSpace(dir))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func tuiMaintenanceSkillNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func tuiPersistableMaintenanceSkills(skills []corelib.NLSkillEntry) []corelib.NLSkillEntry {
	filtered := make([]corelib.NLSkillEntry, 0, len(skills))
	for _, s := range skills {
		if strings.EqualFold(strings.TrimSpace(s.Source), "file") {
			if tuiFileSkillHasRuntimeOverlay(s) {
				filtered = append(filtered, corelib.NLSkillEntry{
					Name:               s.Name,
					Source:             "file",
					SkillDir:           s.SkillDir,
					Status:             tuiFileSkillOverlayStatus(s.Status),
					UsageCount:         s.UsageCount,
					SuccessCount:       s.SuccessCount,
					FailureCount:       s.FailureCount,
					WorkaroundCount:    s.WorkaroundCount,
					LastUsedAt:         s.LastUsedAt,
					LastError:          s.LastError,
					RepairAttemptCount: s.RepairAttemptCount,
					LastRepairAt:       s.LastRepairAt,
					RepairHistory:      append([]corelib.SkillRepairRecord(nil), s.RepairHistory...),
				})
			}
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

func tuiFileSkillOverlayStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" || strings.EqualFold(status, "active") {
		return ""
	}
	return status
}

func tuiFileSkillHasRuntimeOverlay(s corelib.NLSkillEntry) bool {
	return s.UsageCount > 0 ||
		s.SuccessCount > 0 ||
		s.FailureCount > 0 ||
		s.WorkaroundCount > 0 ||
		strings.TrimSpace(s.LastUsedAt) != "" ||
		strings.TrimSpace(s.LastError) != "" ||
		tuiFileSkillOverlayStatus(s.Status) != "" ||
		s.RepairAttemptCount > 0 ||
		strings.TrimSpace(s.LastRepairAt) != "" ||
		len(s.RepairHistory) > 0
}

func tuiIntArg(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

func tuiFloatArg(args map[string]interface{}, key string) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case json.Number:
		if n, err := v.Float64(); err == nil {
			return n
		}
	}
	return 0
}

func tuiBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

func tuiStringListArg(args map[string]interface{}, key string) []string {
	switch v := args[key].(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	}
	return nil
}

// --- search ---

func skillSearch(app *TUIApp, args map[string]interface{}) string {
	query := sval(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	hubURL := app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
	allowedSources, policyErr := tuiAllowedSkillSearchSourcesForPolicy(app.appConfig, query)
	if policyErr != nil {
		return policyErr.Error()
	}

	// Multi-node failover using shared infrastructure from tui/commands.
	// This is the single source of truth for failover logic.
	hubURL = commands.ResolveHubCenterWithFailover(app.appConfig, hubURL, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := skill.DefaultHubClient()
	results := client.SearchAllFiltered(ctx, hubURL, query, allowedSources)

	// Rerank by local execution history: demote skills with poor local
	// success rates or disabled status (P2 #A signal feedback).
	localSkills := skill.ScanAllSkillDirs()
	localSkills = append(localSkills, app.appConfig.NLSkills...)
	results = skill.RerankByLocalHistory(results, localSkills)

	if len(results) == 0 {
		return fmt.Sprintf("未找到匹配 \"%s\" 的 Skill。", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("搜索 \"%s\" — %d 个结果（SkillHub + ClawHub + GitHub）\n\n", query, len(results)))
	for _, s := range results {
		sourceLabel := "SkillHub"
		switch s.Source {
		case "clawhub":
			sourceLabel = "ClawHub"
		case "github":
			sourceLabel = "GitHub"
		}
		b.WriteString(fmt.Sprintf("- [%s] %s (v%s): %s (source:%s, trust:%s, downloads:%d)\n",
			s.ID, s.Name, s.Version, s.Description, sourceLabel, s.TrustLevel, s.Downloads))
	}
	b.WriteString("\n使用 manage_skill(action=\"install\", skill_id=\"<ID>\") 安装。")
	return b.String()
}

// --- install ---

func skillInstall(app *TUIApp, args map[string]interface{}) string {
	skillID := sval(args, "skill_id")
	if skillID == "" {
		return "缺少 skill_id 参数"
	}
	source := sval(args, "source") // explicit: "clawhub", "github", or empty
	hubURL := sval(args, "hub_url")

	// Infer source from hub_url when source is not explicitly set.
	// This aligns with the GUI install path and the install instructions
	// generated by toolSearchSkillHub (which uses hub_url, not source).
	if source == "" && hubURL != "" {
		switch {
		case strings.EqualFold(hubURL, "github"):
			source = "github"
		case strings.Contains(hubURL, "clawhub") || strings.Contains(hubURL, "clawhub-mirror"):
			source = "clawhub"
		}
	}

	// Determine effective source for permission check.
	effectiveSource := source
	if effectiveSource == "" {
		effectiveSource = "skillhub"
	}
	guardArgs := map[string]interface{}{"action": "install", "skill_id": skillID, "source": effectiveSource, "hub_url": hubURL, "install_ref": sval(args, "install_ref")}
	if ok, reason := enforceClientSecurityPolicy(app.appConfig, "manage_skill", guardArgs); !ok {
		return reason
	}

	// Enterprise-only install policy: when enabled, only enterprise Hub source is allowed.
	if reason, blocked := app.appConfig.CapabilityMarketPolicy.RejectNonEnterpriseInstall(effectiveSource, app.appConfig.RemoteHubURL); blocked {
		return reason
	}

	// Check if source is allowed by policy/config.
	if !tuiSkillSourceAllowedByPolicy(app.appConfig, effectiveSource) {
		return fmt.Sprintf("❌ 来源 '%s' 已被管理策略禁止。当前允许的来源: %v", effectiveSource, app.appConfig.SkillSourcesAllowed)
	}

	recordTUIDeveloperSkillRisk(app.appConfig, effectiveSource, "install", guardArgs)

	if hubURL == "" {
		hubURL = app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
	}

	// Check if already installed.
	for _, s := range app.appConfig.NLSkills {
		if s.HubSkillID == skillID || (s.Source == "clawhub" && strings.EqualFold(s.Name, skillID)) ||
			(s.Source == "github" && strings.EqualFold(s.Name, skillID)) {
			return fmt.Sprintf("Skill '%s' 已安装", s.Name)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := skill.DefaultHubClient()
	var entry *corelib.NLSkillEntry
	var err error

	switch source {
	case "clawhub":
		entry, err = client.DownloadClawHub(ctx, skillID)
	case "github":
		installRef := sval(args, "install_ref")
		if installRef == "" {
			return "GitHub Skill 缺少 install_ref 参数"
		}
		entry, err = client.DownloadGitHub(ctx, installRef)
	default:
		entry, err = client.DownloadSkillHub(ctx, hubURL, skillID)
	}
	if err != nil {
		return fmt.Sprintf("安装失败: %v", err)
	}

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}
	cfg.NLSkills = append(cfg.NLSkills, *entry)
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Sprintf("保存失败: %v", err)
	}
	app.appConfig = cfg

	sourceLabel := "SkillHub"
	switch source {
	case "clawhub":
		sourceLabel = "ClawHub"
	case "github":
		sourceLabel = "GitHub"
	}
	return fmt.Sprintf("✅ Skill '%s' 已安装 (来源: %s)", entry.Name, sourceLabel)
}

// --- uninstall ---

func skillUninstall(app *TUIApp, args map[string]interface{}) string {
	name := sval(args, "name")
	if name == "" {
		return "缺少 name 参数（要卸载的 Skill 名称）"
	}

	// Remove from config.
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}

	found := false
	for i, s := range cfg.NLSkills {
		if s.MatchesName(name) {
			cfg.NLSkills = append(cfg.NLSkills[:i], cfg.NLSkills[i+1:]...)
			found = true
			break
		}
	}

	if found {
		if err := store.SaveConfig(cfg); err != nil {
			return fmt.Sprintf("保存配置失败: %v", err)
		}
		app.appConfig = cfg
	}

	// Remove on-disk directories (using unfiltered scanner so any format is covered).
	dirRemoved := false
	for _, root := range skill.SkillScanRootsWithExternal(cfg.ExternalSkillDirs) {
		for _, s := range skill.ScanSkillDirAll(root) {
			if s.Name == name || s.DirName == name {
				if s.SkillDir != "" {
					if err := os.RemoveAll(s.SkillDir); err != nil {
						log.Printf("[skill-uninstall] failed to remove %s: %v", s.SkillDir, err)
					} else {
						dirRemoved = true
					}
				}
			}
		}
	}

	if !found && !dirRemoved {
		return fmt.Sprintf("Skill '%s' 未找到（不在配置中，也不在磁盘上）", name)
	}

	return fmt.Sprintf("✅ Skill '%s' 已卸载（配置和目录已清理）", name)
}

// --- run ---

func skillRun(app *TUIApp, args map[string]interface{}) string {
	return skillRunDetailed(app, args).Output
}

type tuiSkillRunResult struct {
	Output   string
	OK       bool
	Captured map[string]string
}

func skillRunDetailed(app *TUIApp, args map[string]interface{}) tuiSkillRunResult {
	internalPipelineCall := skill.IsInternalPipelineRunArgs(args)
	name := sval(args, "name")
	if name == "" {
		return tuiSkillRunResult{Output: "缺少 name 参数"}
	}
	entry := findSkillEntry(app, name)
	if entry == nil {
		// Fuzzy match fallback.
		if similar, score := skill.FindSimilarSkill(name, 0.3); similar != nil {
			return tuiSkillRunResult{Output: fmt.Sprintf("Skill '%s' 不存在。你是否指的是 %q？(%.0f%% 匹配)", name, similar.Name, score*100)}
		}
		return tuiSkillRunResult{Output: fmt.Sprintf("Skill '%s' 不存在", name)}
	}
	// Status checks (aligned with GUI StartRun).
	if entry.Status == "needs_setup" {
		return tuiSkillRunResult{Output: fmt.Sprintf("Skill '%s' 需要设置。安装未完成（缺少依赖或文件）。请检查 Skill 目录并完成配置", name)}
	}
	if entry.Status == "disabled" {
		return tuiSkillRunResult{Output: fmt.Sprintf("Skill '%s' 已禁用", name)}
	}
	if entry.SkillDir != "" {
		if err := skill.HydrateRunMetadataFromDir(entry); err != nil {
			log.Printf("[skill-run-tui] hydrate skill metadata from %q failed: %v", entry.SkillDir, err)
		}
	}
	if skill.IsKnowledgeSkillType(entry.Type) {
		return tuiSkillRunResult{Output: skill.FormatNoExecutableStepsMessage(name, entry, skill.RunnerBackendTUI)}
	}
	skill.NormalizeSkillForRunner(entry)
	templateVars := normalizeTUIRunSkillVars(args)
	extraEnv := extractTUIRunExtraEnv(args)

	// Mechanism: For contractless skills (no declared params, no required_args,
	// no {{placeholders}}), fold LLM-provided args into the "input" carrier key.
	if skill.IsContractlessSkill(entry) {
		skill.FoldUnconsumedArgsToInput(templateVars, entry.Params)
	}

	if skill.IsPipelineSkill(entry) {
		result := skillRunPipelineDetailed(app, entry, args, templateVars)
		if !internalPipelineCall {
			updateTUISkillRunStats(name, entry, result.OK, result.Output)
		}
		return result
	}
	if len(entry.Steps) == 0 {
		return tuiSkillRunResult{Output: skill.FormatNoExecutableStepsMessage(name, entry, skill.RunnerBackendTUI)}
	}

	if len(entry.RequiredCredentialFiles) > 0 {
		missing := remote.ValidateCredentialFiles(entry.RequiredCredentialFiles)
		if len(missing) > 0 {
			log.Printf("[skill-run-tui] credential pre-check: %d missing credential file(s)", len(missing))
			return tuiSkillRunResult{Output: fmt.Sprintf("Skill '%s' needs setup: missing credential file(s): %s. Please create the required credential files before running this skill",
				name, strings.Join(missing, ", "))}
		}
	}

	// Windows 8.3 short path normalization — must happen before requirement
	// checks so that NpmChecker/NpmFixer get the resolved long path.
	if runtime.GOOS == "windows" && entry.SkillDir != "" {
		if resolved, err := filepath.EvalSymlinks(entry.SkillDir); err == nil {
			entry.SkillDir = resolved
		}
	}

	prep, prepErr := skill.PrepareRunnerExecution(entry, templateVars, args, extraEnv, skill.RunnerBackendTUI)
	if prepErr != nil {
		return tuiSkillRunResult{Output: prepErr.Error()}
	}
	selectedSteps := prep.SelectedSteps
	skillParams := prep.Params
	warnings := prep.Warnings
	fileWarnings := prep.FileWarnings
	for _, v := range prep.RequirementWarnings {
		log.Printf("[skill-run-tui] requirement warning for %q: %s", name, v.Message)
	}
	for _, warning := range fileWarnings {
		log.Printf("[skill-run-tui] file warning for %q: %s", name, warning)
	}

	// ── Step execution with variable capture ──
	vars := make(map[string]string)
	for k, v := range templateVars {
		vars[k] = v
	}

	var results []string
	if warningOutput := skill.PrefixOutputWithWarnings("", warnings); warningOutput != "" {
		results = append(results, warningOutput)
	}
	ok := true
	execCtx := context.Background()
	execCancel := func() {}
	if entry.GlobalTimeout > 0 {
		execCtx, execCancel = context.WithTimeout(context.Background(), time.Duration(entry.GlobalTimeout)*time.Second)
	}
	defer execCancel()
	for i, step := range entry.Steps {
		if len(selectedSteps) > 0 {
			if step.Label == "" || !skill.StepLabelSelected(step.Label, selectedSteps) {
				results = append(results, fmt.Sprintf("[Step %d/%d] ⏭ skipped (not selected)", i+1, len(entry.Steps)))
				continue
			}
		}
		// Conditional execution (when field).
		if step.When != "" {
			if !skill.EvaluateStepWhen(step.When, vars) {
				results = append(results, fmt.Sprintf("[Step %d/%d] ⏭ skipped (when=%q)", i+1, len(entry.Steps), step.When))
				continue
			}
		}

		// Resolve step: parameter binding + template substitution + CLI args + working_dir.
		// Uses the shared corelib/skill.ResolveStep for full parameter contract support.
		step = withTUISkillPreferredShell(step, entry.PreferredShell)
		resolveResult, resolveErr := skill.ResolveStep(step, vars, entry.SkillDir, skillParams, quoteTUIRunValueForStep(step))
		if resolveErr != nil {
			results = append(results, fmt.Sprintf("[Step %d/%d] ✗ %s", i+1, len(entry.Steps), resolveErr.Error()))
			ok = false
			if step.OnError != "continue" {
				break
			}
			continue
		}
		step = resolveResult.Step
		step = skill.PrepareResolvedStepEnv(step, entry.RequiredEnv, extraEnv)

		out, err := execStepWithContext(execCtx, step, entry.SkillDir)
		if len(step.Capture) > 0 && out != "" {
			captured := skill.CaptureOutputVariables(out, step.Capture)
			for k, v := range captured {
				vars[k] = v
				log.Printf("[skill-run-tui] captured %s=%q from step %d", k, v, i+1)
			}
		}

		if err != nil {
			// Classify the error for actionable feedback.
			exitCode := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			ce := skill.ClassifyStepError(exitCode, out, err.Error(), sval(step.Params, "command"))
			errMsg := fmt.Sprintf("[Step %d/%d] ✗ %s", i+1, len(entry.Steps), ce.UserMessage)
			if ce.ActionHint != "" {
				errMsg += "\n  " + ce.ActionHint
			}
			results = append(results, errMsg+"\n"+out)
			ok = false
			if step.OnError != "continue" {
				break
			}
		} else {
			if len(out) > 500 {
				out = out[:500] + "..."
			}
			results = append(results, fmt.Sprintf("[Step %d/%d] ✓\n%s", i+1, len(entry.Steps), out))
		}
	}

	if !internalPipelineCall {
		lastError := ""
		if len(results) > 0 {
			lastError = results[len(results)-1]
		}
		updateTUISkillRunStats(name, entry, ok, lastError)
	}

	var b strings.Builder
	for _, r := range results {
		b.WriteString(r + "\n")
	}
	if ok {
		b.WriteString("✓ 执行完成")
	} else {
		b.WriteString("✗ 执行失败")
	}
	return tuiSkillRunResult{Output: b.String(), OK: ok, Captured: cloneTUIStringMap(vars)}
}

func cloneTUIStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func updateTUISkillRunStats(name string, entry *corelib.NLSkillEntry, ok bool, lastError string) {
	if entry == nil {
		return
	}
	entry.UsageCount++
	entry.LastUsedAt = time.Now().Format(time.RFC3339)
	if ok {
		entry.SuccessCount++
		entry.LastError = ""
	} else {
		entry.FailureCount++
		entry.LastError = formatTUISkillRunLastError(lastError)
	}
	persistStats(name, entry)
}

func formatTUISkillRunLastError(lastError string) string {
	lastError = strings.TrimSpace(lastError)
	if lastError == "" {
		return ""
	}
	if strings.Contains(lastError, "[class:") {
		return skill.TruncateFormattedErrorForStorage(lastError, 500)
	}
	ce := skill.ClassifyStepError(0, "", lastError, "")
	formatted := skill.FormatErrorForLLM(ce)
	return skill.TruncateFormattedErrorForStorage(formatted, 500)
}

func skillStatus(args map[string]interface{}) string {
	runID := sval(args, "run_id")
	if runID == "" {
		return "缺少 run_id 参数"
	}
	return fmt.Sprintf("TUI 模式下 Skill 同步执行，run_id '%s' 的结果已在 run 返回值中。", runID)
}

func skillRunPipeline(app *TUIApp, entry *corelib.NLSkillEntry, args map[string]interface{}, vars map[string]string) string {
	return skillRunPipelineDetailed(app, entry, args, vars).Output
}

func skillRunPipelineDetailed(app *TUIApp, entry *corelib.NLSkillEntry, args map[string]interface{}, vars map[string]string) tuiSkillRunResult {
	if entry == nil {
		return tuiSkillRunResult{Output: "skill entry is nil"}
	}
	if vars == nil {
		vars = map[string]string{}
	}
	extraEnv := extractTUIRunExtraEnv(args)
	prep, err := skill.PreparePipelineRunnerExecution(entry, vars, args, extraEnv, skill.RunnerBackendTUI)
	if err != nil {
		return tuiSkillRunResult{Output: err.Error()}
	}
	for _, warning := range prep.RequirementWarnings {
		log.Printf("[skill-run-tui] pipeline requirement warning for %q: %s", entry.Name, warning.Message)
	}
	pipelineArgs := skill.WithPipelineRunStack(args, entry.Name)
	runCtx := context.Background()
	cancel := func() {}
	if entry.GlobalTimeout > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, time.Duration(entry.GlobalTimeout)*time.Second)
	}
	defer cancel()
	runner := &skill.PipelineRunner{Executor: tuiSkillPipelineExecutor{app: app, baseArgs: pipelineArgs}}
	result, err := runner.Run(runCtx, entry.Pipeline, vars)
	output := skill.PrefixOutputWithWarnings(skill.FormatPipelineResult(result), prep.Warnings)
	if err != nil {
		if strings.TrimSpace(output) == "" || output == "Pipeline status: unknown\n" {
			output = err.Error()
		} else if !strings.Contains(output, err.Error()) {
			output = strings.TrimRight(output, "\n") + "\n" + err.Error()
		}
		return tuiSkillRunResult{Output: output, Captured: pipelineCapturedVars(result)}
	}
	return tuiSkillRunResult{Output: output, OK: result != nil && result.Status == "completed", Captured: pipelineCapturedVars(result)}
}

func pipelineCapturedVars(result *skill.PipelineResult) map[string]string {
	if result == nil || len(result.Vars) == 0 {
		return nil
	}
	return cloneTUIStringMap(result.Vars)
}

type tuiSkillPipelineExecutor struct {
	app      *TUIApp
	baseArgs map[string]interface{}
}

func (e tuiSkillPipelineExecutor) RunSubSkill(ctx context.Context, skillName string, params map[string]string) (map[string]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	args := skill.BuildPipelineSubSkillRunArgs(e.baseArgs, params)
	args["name"] = skillName
	result := skillRunDetailed(e.app, args)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, result.Output, ctxErr
	}
	captured := cloneTUIStringMap(result.Captured)
	if captured == nil {
		captured = map[string]string{}
	}
	if strings.TrimSpace(result.Output) != "" {
		captured["output"] = result.Output
	}
	if !result.OK {
		return captured, result.Output, fmt.Errorf("%s", result.Output)
	}
	return captured, result.Output, nil
}

// --- helpers ---

func normalizeTUIRunSkillVars(args map[string]interface{}) map[string]string {
	return skill.NormalizeRunVars(args)
}

func extractTUIRunExtraEnv(args map[string]interface{}) map[string]string {
	return skill.ExtractRunExtraEnvFromArgs(args)
}

func mergeTUIExtraEnvParam(params map[string]interface{}, extraEnv map[string]string) {
	skill.MergeExtraEnvParam(params, extraEnv)
}

func collectTUISkillProvidedEnv(entry *corelib.NLSkillEntry) map[string]string {
	return skill.CollectSkillProvidedEnv(entry)
}

func applyTUIRunInputInference(entry *corelib.NLSkillEntry, vars map[string]string, args map[string]interface{}) {
	skill.ApplyRunInputInference(entry, vars, args)
}

func substVars(cmd string, sa map[string]interface{}, in, out string) string {
	for _, pair := range [][2]string{{"input", in}, {"output", out}} {
		if pair[1] != "" {
			cmd = strings.ReplaceAll(cmd, "{{"+pair[0]+"}}", pair[1])
			cmd = strings.ReplaceAll(cmd, "${"+pair[0]+"}", pair[1])
		}
	}
	for k, v := range sa {
		s := fmt.Sprintf("%v", v)
		cmd = strings.ReplaceAll(cmd, "{{"+k+"}}", s)
		cmd = strings.ReplaceAll(cmd, "${"+k+"}", s)
	}
	return cmd
}

func quoteTUIRunValueForShell(input string) string {
	return quoteTUIRunValueForPreferredShell(input, defaultTUIShellPreference())
}

func quoteTUIRunValueForStep(step corelib.NLSkillStep) func(string) string {
	preferred, _ := step.Params["preferred_shell"].(string)
	return func(input string) string {
		return quoteTUIRunValueForPreferredShell(input, preferred)
	}
}

func quoteTUIRunValueForPreferredShell(input, preferredShell string) string {
	preferredShell = normalizeTUIShellPreference(preferredShell)
	return skill.QuoteForShellPreference(input, preferredShell)
}

func normalizeTUIShellPreference(preferredShell string) string {
	preferredShell = strings.ToLower(strings.TrimSpace(preferredShell))
	switch preferredShell {
	case "cmd", "cmd.exe", "windows", "win_cmd":
		return "cmd"
	case "powershell", "pwsh", "ps", "ps1":
		return "powershell"
	case "bash", "sh", "zsh":
		return "bash"
	default:
		return defaultTUIShellPreference()
	}
}

func defaultTUIShellPreference() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

func withTUISkillPreferredShell(step corelib.NLSkillStep, preferredShell string) corelib.NLSkillStep {
	preferredShell = strings.TrimSpace(preferredShell)
	if preferredShell == "" {
		return step
	}
	if step.Params == nil {
		step.Params = map[string]interface{}{}
	} else {
		cp := make(map[string]interface{}, len(step.Params)+1)
		for k, v := range step.Params {
			cp[k] = v
		}
		step.Params = cp
	}
	if _, exists := step.Params["preferred_shell"]; !exists {
		step.Params["preferred_shell"] = preferredShell
	}
	return step
}

func execStep(step corelib.NLSkillStep, dir string) (string, error) {
	return execStepWithContext(context.Background(), step, dir)
}

func execStepWithContext(parentCtx context.Context, step corelib.NLSkillStep, dir string) (string, error) {
	if err := skill.EnsureStepActionSupported(skill.RunnerBackendTUI, step.Action); err != nil {
		return "", err
	}
	step.Action = skill.NormalizeStepActionName(step.Action)
	cmd, _ := step.Params["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return "", fmt.Errorf("bash step missing command parameter")
	}
	timeout := skill.RunnerStepTimeoutSeconds(step.Params, corelib.DefaultAgentTimeoutSec, corelib.MaxAgentTimeoutSec)
	wd, _ := step.Params["working_dir"].(string)
	if wd == "" {
		wd = dir
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeout)*time.Second)
	defer cancel()
	var sh string
	var sa []string
	if runtime.GOOS == "windows" {
		switch normalizeTUIShellPreference(sval(step.Params, "preferred_shell")) {
		case "bash":
			var err error
			sh, err = exec.LookPath("bash.exe")
			if err != nil {
				sh, err = exec.LookPath("sh.exe")
			}
			if err != nil {
				return "", fmt.Errorf("bash shell not found for TUI skill step; install Git for Windows or set preferred_shell: powershell")
			}
			sa = []string{"-lc", cmd}
		case "cmd":
			sh = os.Getenv("ComSpec")
			if sh == "" {
				sh = "cmd.exe"
			}
			sa = []string{"/d", "/s", "/c", cmd}
		default:
			sh, sa = "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", cmd}
		}
	} else {
		sh, sa = "bash", []string{"-c", cmd}
	}
	c := exec.CommandContext(ctx, sh, sa...)
	if wd != "" {
		c.Dir = wd
	}
	c.Env = skill.BuildCommandEnv(tuiBaseCommandEnv(), step.Params)
	var so, se bytes.Buffer
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	var b strings.Builder
	if so.Len() > 0 {
		out := so.String()
		if len(out) > 8192 {
			out = out[:8192] + "\n...(truncated)"
		}
		b.WriteString(out)
	}
	if se.Len() > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		errOut := se.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n...(truncated)"
		}
		b.WriteString("[stderr] " + errOut)
	}
	return b.String(), err
}

func tuiBaseCommandEnv() []string {
	return tuiBaseCommandEnvFrom(os.Environ())
}

func tuiBaseCommandEnvFrom(base []string) []string {
	return coretool.AppendUTF8Env(base)
}

func persistStats(name string, e *corelib.NLSkillEntry) {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return
	}
	found := false
	for j := range cfg.NLSkills {
		if cfg.NLSkills[j].MatchesName(name) {
			cfg.NLSkills[j].UsageCount = e.UsageCount
			cfg.NLSkills[j].SuccessCount = e.SuccessCount
			cfg.NLSkills[j].FailureCount = e.FailureCount
			cfg.NLSkills[j].LastUsedAt = e.LastUsedAt
			cfg.NLSkills[j].LastError = e.LastError
			found = true
			break
		}
	}
	if !found && strings.EqualFold(strings.TrimSpace(e.Source), "file") && strings.TrimSpace(e.SkillDir) != "" {
		cfg.NLSkills = append(cfg.NLSkills, tuiPersistableMaintenanceSkills([]corelib.NLSkillEntry{*e})...)
	}
	_ = store.SaveConfig(cfg)
	recordTUISkillUsageExperience(e, okFromSkillLastError(e))
}

func okFromSkillLastError(e *corelib.NLSkillEntry) bool {
	return e != nil && strings.TrimSpace(e.LastError) == ""
}

func recordTUISkillUsageExperience(entry *corelib.NLSkillEntry, success bool) {
	if entry == nil {
		return
	}
	trackerPath := coretool.DefaultUsageTrackerPath()
	if trackerPath == "" {
		return
	}
	tracker, err := coretool.NewUsageTracker(trackerPath)
	if err != nil {
		log.Printf("[skill-run-tui] usage tracker unavailable: %v", err)
		return
	}
	tokens := bm25.Tokenize(strings.TrimSpace(entry.Name + " " + entry.Description + " " + strings.Join(entry.Triggers, " ")))
	if len(tokens) > 5 {
		tokens = tokens[:5]
	}
	finalOutcome := "completed"
	followUp := "continue"
	errorClass := ""
	if !success {
		finalOutcome = "failed"
		followUp = "abandon"
		errorClass = skill.ExtractErrorClass(entry.LastError)
	}
	tracker.RecordExperience(coretool.ToolExperience{
		ToolName:     "skill:" + entry.Name,
		QueryTokens:  tokens,
		Success:      success,
		FollowUp:     followUp,
		TaskType:     "skill_execution",
		ErrorClass:   errorClass,
		FinalOutcome: finalOutcome,
	})
}

// --- upload ---

// skillUpload packages a local skill and uploads it to SkillMarket.
// Reuses the same HTTP API as tui/commands/skillmarket.go smSubmit,
// but operates on a skill name (not a pre-built zip path).
func skillUpload(app *TUIApp, args map[string]interface{}) string {
	name := sval(args, "name")
	if name == "" {
		return "缺少 name 参数（要上传的 Skill 名称）"
	}

	// Resolve skill entry.
	entry := findSkillEntry(app, name)
	if entry == nil {
		return fmt.Sprintf("未找到 Skill「%s」", name)
	}
	if entry.SkillDir == "" {
		return fmt.Sprintf("Skill「%s」没有关联的目录，无法打包上传", name)
	}

	// Pre-upload portability gate (shared with GUI): auto-fix safe absolute
	// paths in place, then block when machine-specific paths or missing
	// bundled files remain. Skipped when force=true.
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
		result, prepErr := skill.PrepareSkillForUpload(entry.SkillDir)
		if prepErr != nil {
			return fmt.Sprintf("上传前可移植性检查失败: %s", prepErr.Error())
		}
		if !result.Portable() {
			return skill.FormatUploadPreflight(result)
		}
	}

	// Resolve email.
	email := strings.TrimSpace(app.appConfig.RemoteEmail)
	if email == "" {
		return "未配置 remote_email，无法上传到 SkillMarket。请先在配置中设置邮箱。"
	}

	// Resolve auth token from config.
	authToken := strings.TrimSpace(app.appConfig.SkillMarketSessionToken)

	// Package skill directory into a zip.
	zipPath, err := packageSkillDirToZip(entry)
	if err != nil {
		return fmt.Sprintf("打包失败: %s", err.Error())
	}
	defer os.Remove(zipPath)

	// Upload via HTTP multipart POST (same API as smSubmit).
	hubURL := app.appConfig.SkillMarketBaseURL(remote.DefaultRemoteHubCenterURL)
	submissionID, err := submitSkillZip(hubURL, zipPath, email, authToken)
	if err != nil {
		// If 401 and no token, provide login guidance
		errMsg := err.Error()
		if strings.Contains(errMsg, "401") && authToken == "" {
			return fmt.Sprintf("上传失败: SkillMarket 要求认证。请先登录:\n"+
				"  maclaw-tui skillmarket login --email %s --password <密码>\n"+
				"  或使用邮件验证: maclaw-tui skillmarket lookup --email %s\n\n原始错误: %s",
				email, email, errMsg)
		}
		return fmt.Sprintf("上传失败: %s", errMsg)
	}

	return fmt.Sprintf("✅ Skill「%s」已上传到 SkillMarket，提交 ID: %s\n使用 CLI `maclaw-tui skillmarket status %s` 查看审核状态。",
		name, submissionID, submissionID)
}

// packageSkillDirToZip creates a temporary zip of the skill directory.
func packageSkillDirToZip(entry *corelib.NLSkillEntry) (string, error) {
	tmpFile, err := os.CreateTemp("", "skill-upload-*.zip")
	if err != nil {
		return "", err
	}
	zipPath := tmpFile.Name()
	tmpFile.Close()

	if err := zipDirectoryTUI(entry.SkillDir, zipPath); err != nil {
		os.Remove(zipPath)
		return "", err
	}
	return zipPath, nil
}

// zipDirectoryTUI packages srcDir into a zip file at zipPath.
func zipDirectoryTUI(srcDir, zipPath string) error {
	outFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}

	w := zip.NewWriter(outFile)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Skip hidden dirs like .git; .patches.json is fine to include.
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
			return filepath.SkipDir
		}
		if info.IsDir() {
			_, err := w.Create(rel + "/")
			return err
		}
		fw, err := w.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})

	// Close zip writer first (writes central directory), then the file.
	closeErr := w.Close()
	_ = outFile.Close()

	if walkErr != nil {
		return walkErr
	}
	return closeErr
}

// submitSkillZip uploads a zip file to the SkillMarket submit API.
func submitSkillZip(hubURL, zipPath, email, authToken string) (string, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("zip", filepath.Base(zipPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	_ = w.WriteField("email", email)
	w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+"/api/v1/skills/submit", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("submit failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		SubmissionID string `json:"submission_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.SubmissionID, nil
}

// --- validate ---

func skillValidate(app *TUIApp, args map[string]interface{}) string {
	name := sval(args, "name")
	if name == "" {
		return "缺少 name 参数（要验证的 Skill 名称）"
	}

	autoFix, _ := args["auto_fix"].(bool)

	// Resolve skill directory.
	skillDir := ""
	entry := findSkillEntry(app, name)
	if entry != nil {
		skillDir = entry.SkillDir
	}
	// Fallback: check PrimarySkillsDir/<name>.
	if skillDir == "" {
		if primaryDir, err := skill.PrimarySkillsDir(); err == nil {
			candidate := filepath.Join(primaryDir, name)
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				skillDir = candidate
			}
		}
	}
	if skillDir == "" {
		return fmt.Sprintf("未找到 Skill「%s」。使用 manage_skill(action=\"list\") 查看已安装的 Skill。", name)
	}

	report, err := skill.ValidateSkillPortability(skillDir)
	if err != nil {
		return fmt.Sprintf("验证失败: %s", err.Error())
	}

	if !autoFix {
		return skill.FormatPortabilityReport(report)
	}

	// Auto-fix → re-validate.
	changes, fixErr := skill.AutoFixPortability(skillDir)
	if fixErr != nil {
		return fmt.Sprintf("自动修复失败: %s\n\n%s", fixErr.Error(), skill.FormatPortabilityReport(report))
	}

	finalReport, revalidateErr := skill.ValidateSkillPortability(skillDir)
	if revalidateErr != nil {
		return fmt.Sprintf("修复后重新验证失败: %s\n\n%s", revalidateErr.Error(), skill.FormatPortabilityChanges(changes))
	}

	var b strings.Builder
	b.WriteString(skill.FormatPortabilityChanges(changes))
	b.WriteByte('\n')
	b.WriteString(skill.FormatPortabilityReport(finalReport))
	return b.String()
}

// --- patch ---

// tuiPatchRecord mirrors gui/im_tools_misc.go patchRecord.
type tuiPatchRecord struct {
	Timestamp string `json:"timestamp"`
	Find      string `json:"find"`
	Replace   string `json:"replace"`
	Reason    string `json:"reason,omitempty"`
}

func skillPatch(app *TUIApp, args map[string]interface{}) string {
	skillName := sval(args, "skill_name")
	if skillName == "" {
		return "缺少 skill_name 参数"
	}

	// Dispatch by mode: "text" (default) or "step" (structured).
	mode := sval(args, "mode")
	if mode == "" {
		mode = "text"
	}

	switch mode {
	case "step":
		return skillPatchStructured(app, skillName, args)
	case "text":
		return skillPatchText(app, skillName, args)
	default:
		return fmt.Sprintf("未知 patch mode: %q（支持: text/step）", mode)
	}
}

// skillPatchStructured performs a structured modification of a specific step field.
func skillPatchStructured(app *TUIApp, skillName string, args map[string]interface{}) string {
	entry := findSkillEntry(app, skillName)
	if entry == nil {
		return fmt.Sprintf("未找到 Skill「%s」", skillName)
	}
	if entry.SkillDir == "" {
		return fmt.Sprintf("Skill「%s」没有关联的目录，无法执行 patch", skillName)
	}

	defPath, defFormat := findSkillDefFile(entry.SkillDir)
	if defPath == "" || defFormat != "yaml" {
		return fmt.Sprintf("结构化 patch 仅支持 skill.yaml 格式（当前: %s）", defFormat)
	}

	content, err := os.ReadFile(defPath)
	if err != nil {
		return fmt.Sprintf("读取 Skill 定义文件失败: %s", err.Error())
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Sprintf("解析 skill.yaml 失败: %s", err.Error())
	}

	stepIdxRaw, ok := args["step_index"]
	if !ok {
		return "结构化 patch 缺少 step_index 参数"
	}
	stepIdx := 0
	switch v := stepIdxRaw.(type) {
	case float64:
		stepIdx = int(v)
	case int:
		stepIdx = v
	case string:
		fmt.Sscanf(v, "%d", &stepIdx)
	}

	field := sval(args, "field")
	if field == "" {
		return "结构化 patch 缺少 field 参数"
	}
	value := sval(args, "value")
	reason := sval(args, "reason")

	stepsRaw, ok := doc["steps"]
	if !ok {
		return "skill.yaml 中没有 steps 字段"
	}
	steps, ok := stepsRaw.([]interface{})
	if !ok {
		return "skill.yaml 中 steps 字段格式无效"
	}
	if stepIdx < 0 || stepIdx >= len(steps) {
		return fmt.Sprintf("step_index %d 超出范围（共 %d 个步骤）", stepIdx, len(steps))
	}
	step, ok := steps[stepIdx].(map[string]interface{})
	if !ok {
		return fmt.Sprintf("步骤 %d 格式无效", stepIdx)
	}

	paramsFields := map[string]bool{"command": true, "working_dir": true, "timeout": true}
	if paramsFields[field] {
		params, ok := step["params"].(map[string]interface{})
		if !ok {
			params = make(map[string]interface{})
			step["params"] = params
		}
		params[field] = value
	} else {
		step[field] = value
	}

	modified, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Sprintf("序列化 YAML 失败: %s", err.Error())
	}

	if validationErr := validateSkillContent(modified, defFormat); validationErr != "" {
		return fmt.Sprintf("patch 后的 skill 定义无效，已拒绝保存: %s", validationErr)
	}

	if err := fileutil.AtomicWriteFile(defPath, modified, 0644); err != nil {
		return fmt.Sprintf("保存 Skill 定义文件失败: %s", err.Error())
	}

	log.Printf("[skill-patch-step] patched %s step %d field %s in %s", skillName, stepIdx, field, defPath)

	if auditErr := appendTUIPatchRecord(entry.SkillDir, tuiPatchRecord{
		Timestamp: time.Now().Format(time.RFC3339),
		Find:      fmt.Sprintf("step[%d].%s", stepIdx, field),
		Replace:   value,
		Reason:    reason,
	}); auditErr != nil {
		log.Printf("[skill-patch-step] warning: failed to write audit trail: %v", auditErr)
	}

	return fmt.Sprintf("✅ Skill「%s」步骤 %d 的 %s 已修改为 %q", skillName, stepIdx, field, value)
}

// skillPatchText performs the original text-level find-and-replace patch.
func skillPatchText(app *TUIApp, skillName string, args map[string]interface{}) string {
	find := sval(args, "find")
	if find == "" {
		return "缺少 find 参数"
	}
	replaceVal, hasReplace := args["replace"]
	if !hasReplace {
		return "缺少 replace 参数"
	}
	replaceStr, _ := replaceVal.(string)
	reason := sval(args, "reason")

	entry := findSkillEntry(app, skillName)
	if entry == nil {
		return fmt.Sprintf("未找到 Skill「%s」", skillName)
	}
	if entry.SkillDir == "" {
		return fmt.Sprintf("Skill「%s」没有关联的目录，无法执行 patch", skillName)
	}

	defPath, defFormat := findSkillDefFile(entry.SkillDir)
	if defPath == "" {
		return fmt.Sprintf("在 Skill 目录中未找到 skill.yaml/skill.yml: %s", entry.SkillDir)
	}

	content, err := os.ReadFile(defPath)
	if err != nil {
		return fmt.Sprintf("读取 Skill 定义文件失败: %s", err.Error())
	}

	count := strings.Count(string(content), find)
	if count == 0 {
		return fmt.Sprintf("no match found: 在 Skill 定义文件中未找到「%s」", find)
	}
	if count > 1 {
		return fmt.Sprintf("ambiguous match: 找到 %d 处匹配「%s」，请提供更多上下文以精确定位", count, find)
	}

	modified := strings.Replace(string(content), find, replaceStr, 1)

	if validationErr := validateSkillContent([]byte(modified), defFormat); validationErr != "" {
		return fmt.Sprintf("patch 后的文件格式无效，已拒绝保存: %s", validationErr)
	}

	if err := fileutil.AtomicWriteFile(defPath, []byte(modified), 0644); err != nil {
		return fmt.Sprintf("保存 Skill 定义文件失败: %s", err.Error())
	}

	log.Printf("[skill-patch] patched %s in %s", skillName, defPath)

	if auditErr := appendTUIPatchRecord(entry.SkillDir, tuiPatchRecord{
		Timestamp: time.Now().Format(time.RFC3339),
		Find:      find,
		Replace:   replaceStr,
		Reason:    reason,
	}); auditErr != nil {
		log.Printf("[skill-patch] warning: failed to write audit trail: %v", auditErr)
	}

	return fmt.Sprintf("✅ Skill「%s」已成功 patch（替换了 1 处匹配）", skillName)
}

// --- history ---

func skillPatchHistory(app *TUIApp, args map[string]interface{}) string {
	skillName := sval(args, "skill_name")
	if skillName == "" {
		return "缺少 skill_name 参数"
	}

	entry := findSkillEntry(app, skillName)
	if entry == nil {
		return fmt.Sprintf("未找到 Skill「%s」", skillName)
	}
	if entry.SkillDir == "" {
		return fmt.Sprintf("Skill「%s」没有关联的目录", skillName)
	}

	patchesPath := filepath.Join(entry.SkillDir, ".patches.json")
	data, err := os.ReadFile(patchesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Skill「%s」没有 patch 历史记录", skillName)
		}
		return fmt.Sprintf("读取 patch 历史失败: %s", err.Error())
	}

	var records []tuiPatchRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Sprintf("解析 patch 历史失败: %s", err.Error())
	}
	if len(records) == 0 {
		return fmt.Sprintf("Skill「%s」没有 patch 历史记录", skillName)
	}

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

// --- shared helpers for new actions ---

// findSkillEntry resolves a skill by name from config + scanned dirs.
func findSkillEntry(app *TUIApp, name string) *corelib.NLSkillEntry {
	for i := range app.appConfig.NLSkills {
		if app.appConfig.NLSkills[i].MatchesName(name) {
			return &app.appConfig.NLSkills[i]
		}
	}
	for _, s := range skill.ScanAllSkillDirs() {
		if s.MatchesName(name) {
			cp := s
			return &cp
		}
	}
	return nil
}

// findSkillDefFile locates YAML skill definitions in a skill directory.
func findSkillDefFile(skillDir string) (string, string) {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		yamlPath := filepath.Join(skillDir, name)
		if _, err := os.Stat(yamlPath); err == nil {
			return yamlPath, "yaml"
		}
	}
	return "", ""
}

// validateSkillContent checks YAML validity.
func validateSkillContent(data []byte, format string) string {
	switch format {
	case "yaml":
		if _, err := skill.ParseSkillDefinitionFile(data, format); err != nil {
			return fmt.Sprintf("skill.yaml 验证失败: %s", err.Error())
		}
	default:
		return fmt.Sprintf("未知文件格式: %s", format)
	}
	return ""
}

// appendTUIPatchRecord appends a patch record to .patches.json audit trail.
func appendTUIPatchRecord(skillDir string, record tuiPatchRecord) error {
	patchesPath := filepath.Join(skillDir, ".patches.json")

	var records []tuiPatchRecord
	if data, err := os.ReadFile(patchesPath); err == nil {
		if jsonErr := json.Unmarshal(data, &records); jsonErr != nil {
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
