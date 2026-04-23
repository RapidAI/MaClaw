package main

// tool_manage_skill.go provides the TUI implementation of the manage_skill
// tool handler. It is injected into CoreToolDeps.ExtraHandlers["manage_skill"]
// at startup, so the shared RegisterCoreTools picks it up automatically.
//
// The handler reuses existing TUI infrastructure:
//   - corelib/skill.ScanAllSkillDirs() for listing
//   - corelib/skill.ImportMarkdownSkillDir() for step resolution
//   - tui/commands.FileConfigStore for config persistence
//   - Hub HTTP API for search/install (direct HTTP, no GUI SkillHubClient)

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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"gopkg.in/yaml.v3"
)

// newManageSkillHandler creates the manage_skill handler bound to the TUI app.
func newManageSkillHandler(app *TUIApp) func(args map[string]interface{}) string {
	return func(args map[string]interface{}) string {
		action, _ := args["action"].(string)
		switch action {
		case "list":
			return skillList(app)
		case "search":
			return skillSearch(app, args)
		case "install":
			return skillInstall(app, args)
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
		b.WriteString(line + "\n")
	}
	return b.String()
}

// --- search ---

func skillSearch(app *TUIApp, args map[string]interface{}) string {
	query := sval(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	hubURL := app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=1", hubURL, url.QueryEscape(query)), nil)
	if err != nil {
		return fmt.Sprintf("搜索请求创建失败: %v", err)
	}
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("SkillHub 返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Skills []struct {
			ID, Name, Description, Version, Author, TrustLevel string
			Downloads                                           int
			Tags                                                []string
		} `json:"skills"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Sprintf("解析失败: %v", err)
	}
	if len(result.Skills) == 0 {
		return fmt.Sprintf("未找到匹配 \"%s\" 的 Skill。", query)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("搜索 \"%s\" — %d 个结果\n\n", query, result.Total))
	for _, s := range result.Skills {
		b.WriteString(fmt.Sprintf("- [%s] %s (v%s): %s (trust:%s, downloads:%d, hub:%s)\n",
			s.ID, s.Name, s.Version, s.Description, s.TrustLevel, s.Downloads, hubURL))
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
	hubURL := sval(args, "hub_url")
	if hubURL == "" {
		hubURL = app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
	}
	for _, s := range app.appConfig.NLSkills {
		if s.HubSkillID == skillID {
			return fmt.Sprintf("Skill '%s' 已安装", s.Name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v1/skills/%s/download", hubURL, url.PathEscape(skillID)), nil)
	if err != nil {
		return fmt.Sprintf("下载请求创建失败: %v", err)
	}
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Sprintf("下载失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("SkillHub 返回 HTTP %d", resp.StatusCode)
	}
	var full struct {
		Name, Description, Version, Author, TrustLevel string
		Triggers                                        []string `json:"triggers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&full); err != nil {
		return fmt.Sprintf("解析失败: %v", err)
	}
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}
	cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
		Name: full.Name, Description: full.Description,
		Status: "active", Source: "hub", SourceProject: hubURL,
		HubSkillID: skillID, HubVersion: full.Version, TrustLevel: full.TrustLevel,
		Triggers: full.Triggers, CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Sprintf("保存失败: %v", err)
	}
	app.appConfig = cfg
	return fmt.Sprintf("✅ Skill '%s' (v%s) 已安装  作者:%s  信任:%s",
		full.Name, full.Version, full.Author, full.TrustLevel)
}

// --- run ---

func skillRun(app *TUIApp, args map[string]interface{}) string {
	name := sval(args, "name")
	if name == "" {
		return "缺少 name 参数"
	}
	entry := findSkillEntry(app, name)
	if entry == nil {
		return fmt.Sprintf("Skill '%s' 不存在", name)
	}
	if entry.Status == "disabled" {
		return fmt.Sprintf("Skill '%s' 已禁用", name)
	}
	if len(entry.Steps) == 0 && entry.SkillDir != "" {
		if imp, err := skill.ImportMarkdownSkillDir(entry.SkillDir, skill.MarkdownSkillOptions{NameFallback: entry.Name}); err == nil && imp != nil {
			entry.Steps = imp.Steps
		}
	}
	if len(entry.Steps) == 0 {
		return fmt.Sprintf("Skill '%s' 没有可执行步骤", name)
	}
	skillArgs, _ := args["args"].(map[string]interface{})
	input, output := sval(args, "input"), sval(args, "output")
	var results []string
	ok := true
	for i, step := range entry.Steps {
		if cmd, _ := step.Params["command"].(string); cmd != "" {
			// Copy params to avoid mutating the original entry.
			cp := make(map[string]interface{}, len(step.Params))
			for k, v := range step.Params {
				cp[k] = v
			}
			cp["command"] = substVars(cmd, skillArgs, input, output)
			step.Params = cp
		}
		out, err := execStep(step, entry.SkillDir)
		if err != nil {
			results = append(results, fmt.Sprintf("[Step %d/%d] ✗ %v\n%s", i+1, len(entry.Steps), err, out))
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
	entry.UsageCount++
	entry.LastUsedAt = time.Now().Format(time.RFC3339)
	if ok {
		entry.SuccessCount++
	} else {
		entry.FailureCount++
	}
	persistStats(name, entry)
	var b strings.Builder
	for _, r := range results {
		b.WriteString(r + "\n")
	}
	if ok {
		b.WriteString("✓ 执行完成")
	} else {
		b.WriteString("✗ 执行失败")
	}
	return b.String()
}

func skillStatus(args map[string]interface{}) string {
	runID := sval(args, "run_id")
	if runID == "" {
		return "缺少 run_id 参数"
	}
	return fmt.Sprintf("TUI 模式下 Skill 同步执行，run_id '%s' 的结果已在 run 返回值中。", runID)
}

// --- helpers ---

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

func execStep(step corelib.NLSkillStep, dir string) (string, error) {
	cmd, _ := step.Params["command"].(string)
	if step.Action != "bash" || cmd == "" {
		return "", fmt.Errorf("action %q 在 TUI 下不支持", step.Action)
	}
	timeout := 30
	if t, ok := step.Params["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}
	if timeout > 120 {
		timeout = 120
	}
	wd, _ := step.Params["working_dir"].(string)
	if wd == "" {
		wd = dir
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	var sh string
	var sa []string
	if runtime.GOOS == "windows" {
		sh, sa = "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", cmd}
	} else {
		sh, sa = "bash", []string{"-c", cmd}
	}
	c := exec.CommandContext(ctx, sh, sa...)
	if wd != "" {
		c.Dir = wd
	}
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

func persistStats(name string, e *corelib.NLSkillEntry) {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return
	}
	for j := range cfg.NLSkills {
		if cfg.NLSkills[j].MatchesName(name) {
			cfg.NLSkills[j].UsageCount = e.UsageCount
			cfg.NLSkills[j].SuccessCount = e.SuccessCount
			cfg.NLSkills[j].FailureCount = e.FailureCount
			cfg.NLSkills[j].LastUsedAt = e.LastUsedAt
			break
		}
	}
	_ = store.SaveConfig(cfg)
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

	// Pre-upload portability validation.
	report, err := skill.ValidateSkillPortability(entry.SkillDir)
	if err != nil {
		return fmt.Sprintf("可移植性验证失败: %s", err.Error())
	}
	if report.Summary.Errors > 0 {
		return fmt.Sprintf("上传被阻止: 发现 %d 个可移植性错误。\n%s\n\n💡 使用 manage_skill(action=\"validate\", name=\"%s\", auto_fix=true) 尝试自动修复",
			report.Summary.Errors, skill.FormatPortabilityReport(report), name)
	}

	// Resolve email.
	email := strings.TrimSpace(app.appConfig.RemoteEmail)
	if email == "" {
		return "未配置 remote_email，无法上传到 SkillMarket。请先在配置中设置邮箱。"
	}

	// Package skill directory into a zip.
	zipPath, err := packageSkillDirToZip(entry)
	if err != nil {
		return fmt.Sprintf("打包失败: %s", err.Error())
	}
	defer os.Remove(zipPath)

	// Upload via HTTP multipart POST (same API as smSubmit).
	hubURL := app.appConfig.SkillMarketBaseURL(remote.DefaultRemoteHubCenterURL)
	submissionID, err := submitSkillZip(hubURL, zipPath, email)
	if err != nil {
		return fmt.Sprintf("上传失败: %s", err.Error())
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
func submitSkillZip(hubURL, zipPath, email string) (string, error) {
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
		return fmt.Sprintf("在 Skill 目录中未找到 skill.yaml 或 skill.json: %s", entry.SkillDir)
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

// findSkillDefFile locates skill.yaml or skill.json in a skill directory.
func findSkillDefFile(skillDir string) (string, string) {
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath, "yaml"
	}
	jsonPath := filepath.Join(skillDir, "skill.json")
	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath, "json"
	}
	return "", ""
}

// validateSkillContent checks YAML/JSON validity.
func validateSkillContent(data []byte, format string) string {
	switch format {
	case "yaml":
		var m map[string]interface{}
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Sprintf("YAML 验证失败: %s", err.Error())
		}
	case "json":
		if !json.Valid(data) {
			return "JSON 验证失败: 内容不是有效的 JSON"
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
