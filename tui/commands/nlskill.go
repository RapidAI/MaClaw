package commands

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// RunNLSkill 执行 nlskill 子命令（NL 技能管理）。
func RunNLSkill(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui nlskill <list|add|remove|enable|disable|execute>")
	}
	switch args[0] {
	case "list":
		return nlskillList(args[1:])
	case "add":
		return nlskillAdd(args[1:])
	case "remove":
		return nlskillRemove(args[1:])
	case "enable":
		return nlskillToggle(args[1:], "active")
	case "disable":
		return nlskillToggle(args[1:], "disabled")
	case "execute":
		return nlskillExecute(args[1:])
	default:
		return NewUsageError("unknown nlskill action: %s", args[0])
	}
}

func nlskillList(args []string) error {
	fs := flag.NewFlagSet("nlskill list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(cfg.NLSkills)
	}

	if len(cfg.NLSkills) == 0 {
		fmt.Println("无 NL 技能。")
		return nil
	}

	fmt.Printf("%-20s %-8s %-8s %-6s %-30s %s\n", "NAME", "STATUS", "SOURCE", "USES", "TRIGGERS", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 100))
	for _, s := range cfg.NLSkills {
		triggers := strings.Join(s.Triggers, ", ")
		fmt.Printf("%-20s %-8s %-8s %-6d %-30s %s\n",
			TruncateDisplay(s.Name, 20),
			s.Status,
			s.Source,
			s.UsageCount,
			TruncateDisplay(triggers, 30),
			TruncateDisplay(s.Description, 30))
	}
	return nil
}

func nlskillAdd(args []string) error {
	fs := flag.NewFlagSet("nlskill add", flag.ExitOnError)
	name := fs.String("name", "", "技能名称（必填）")
	desc := fs.String("desc", "", "技能描述")
	triggers := fs.String("triggers", "", "触发词（逗号分隔）")
	fs.Parse(args)

	if *name == "" {
		return NewUsageError("usage: nlskill add --name <name> [--desc <desc>] [--triggers <t1,t2>]")
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 检查重名
	for _, s := range cfg.NLSkills {
		if s.Name == *name {
			return fmt.Errorf("NL 技能 '%s' 已存在", *name)
		}
	}

	entry := corelib.NLSkillEntry{
		Name:        *name,
		Description: *desc,
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "manual",
	}
	if *triggers != "" {
		entry.Triggers = strings.Split(*triggers, ",")
	}

	cfg.NLSkills = append(cfg.NLSkills, entry)
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("✓ NL 技能 '%s' 已添加\n", *name)
	return nil
}

func nlskillRemove(args []string) error {
	fs := flag.NewFlagSet("nlskill remove", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: nlskill remove <name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	found := false
	for i, s := range cfg.NLSkills {
		if s.MatchesName(name) {
			cfg.NLSkills = append(cfg.NLSkills[:i], cfg.NLSkills[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("NL 技能 '%s' 不存在", name)
	}

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("NL 技能 '%s' 已移除。\n", name)
	return nil
}

func nlskillToggle(args []string, status string) error {
	fs := flag.NewFlagSet("nlskill toggle", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: nlskill enable|disable <name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	found := false
	for i, s := range cfg.NLSkills {
		if s.MatchesName(name) {
			cfg.NLSkills[i].Status = status
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("NL 技能 '%s' 不存在", name)
	}

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("NL 技能 '%s' 状态已设为 %s。\n", name, status)
	return nil
}

func nlskillExecute(args []string) error {
	fs := flag.NewFlagSet("nlskill execute", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: nlskill execute <skill-name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find the skill
	var skill *corelib.NLSkillEntry
	for i := range cfg.NLSkills {
		if cfg.NLSkills[i].Name == name {
			skill = &cfg.NLSkills[i]
			break
		}
	}
	if skill == nil {
		return fmt.Errorf("NL 技能 '%s' 不存在", name)
	}
	if skill.Status == "disabled" {
		return fmt.Errorf("NL 技能 '%s' 已禁用", name)
	}
	if len(skill.Steps) == 0 {
		return fmt.Errorf("NL 技能 '%s' 没有定义步骤", name)
	}

	// ── Unified requirement pre-check ──
	reqs := cskill.ExtractRequirements(skill, cskill.DefaultCheckContext())
	if len(reqs) > 0 {
		registry := cskill.DefaultRegistry()
		violations := registry.CheckAll(reqs)
		remaining := registry.FixAll(violations)
		errors := cskill.FilterErrors(remaining)
		if len(errors) > 0 {
			return fmt.Errorf("skill %q 前置条件未满足: %s", name, cskill.FormatViolations(errors))
		}
	}

	type stepResult struct {
		Step   int    `json:"step"`
		Action string `json:"action"`
		Status string `json:"status"`
		Output string `json:"output,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	var results []stepResult
	success := true
	hasFailure := false

	for i, step := range skill.Steps {
		fmt.Printf("[Step %d] %s ...\n", i+1, step.Action)

		output, execErr := tui_executeStep(step, skill.SkillDir)
		r := stepResult{Step: i + 1, Action: step.Action}

		if execErr != nil {
			r.Status = "failed"
			r.Error = execErr.Error()
			r.Output = output
			results = append(results, r)
			hasFailure = true
			fmt.Printf("  ✗ %s\n", execErr.Error())
			if step.OnError != "continue" {
				success = false
				break
			}
		} else {
			r.Status = "success"
			r.Output = output
			results = append(results, r)
			if output != "" {
				// 只显示前 200 字符
				display := output
				if len(display) > 200 {
					display = display[:200] + "..."
				}
				fmt.Printf("  ✓ %s\n", display)
			} else {
				fmt.Printf("  ✓ done\n")
			}
		}
	}

	// Update usage stats
	if hasFailure {
		success = false
	}
	skill.UsageCount++
	skill.LastUsedAt = time.Now().Format(time.RFC3339)
	if success {
		skill.SuccessCount++
		skill.LastError = ""
	} else {
		for _, r := range results {
			if r.Error != "" {
				skill.LastError = r.Error
				break
			}
		}
	}
	_ = store.SaveConfig(cfg)

	// Attempt self-repair for skills with low success rate.
	if !success && skill.LastError != "" {
		maybeRepairSkillTUI(skill, cfg, store)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{
			"skill":   name,
			"steps":   results,
			"success": success,
		})
	}
	if success {
		fmt.Printf("✓ 技能 '%s' 执行完成 (%d 步)\n", name, len(results))
	} else {
		fmt.Printf("✗ 技能 '%s' 执行失败\n", name)
	}
	return nil
}

// tui_executeStep 在 TUI 模式下执行单个 skill step。
// TUI 模式只支持 bash 类型的 step，其他类型需要 GUI 环境。
func tui_executeStep(step corelib.NLSkillStep, skillDir string) (string, error) {
	switch step.Action {
	case "bash":
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return tui_executeBashStep(command, step.Params, skillDir)

	case "create_session", "send_and_observe", "control_session":
		return "", fmt.Errorf("external coding-session action %q is disabled", step.Action)

	case "send_input", "call_mcp_tool":
		return "", fmt.Errorf("action %q 仅在 GUI 模式下可用", step.Action)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

// tui_executeBashStep 在 TUI 模式下执行 bash 命令。
func tui_executeBashStep(command string, params map[string]interface{}, skillDir string) (string, error) {
	cfg, err := NewFileConfigStore(ResolveDataDir()).LoadConfig()
	if err == nil {
		if ok, reason := clientsecurity.EnforceConfig(cfg, "bash", map[string]interface{}{"command": command}); !ok {
			return "", fmt.Errorf("%s", reason)
		}
	}
	timeout := corelib.DefaultAgentTimeoutSec
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = corelib.NormalizeAgentTimeoutSec(int(t))
	}

	workDir, _ := params["working_dir"].(string)
	if workDir == "" && skillDir != "" {
		workDir = skillDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		out := stdout.String()
		if len(out) > 8192 {
			out = out[:8192] + "\n... (truncated)"
		}
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		errOut := stderr.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n... (truncated)"
		}
		b.WriteString("[stderr] ")
		b.WriteString(errOut)
	}
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[error] timeout after %ds", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[error] %v", runErr))
		}
		return b.String(), runErr
	}
	if b.Len() == 0 {
		return "(completed, no output)", nil
	}
	return b.String(), nil
}
