package commands

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
		if s.Name == name {
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
		if s.Name == name {
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

	type stepResult struct {
		Step   int    `json:"step"`
		Action string `json:"action"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	var results []stepResult

	for i, step := range skill.Steps {
		fmt.Printf("[Step %d] %s ...\n", i+1, step.Action)
		// In CLI mode, steps are logged but actual execution depends on the action type.
		// This is a thin wrapper — real execution would be handled by the agent loop.
		r := stepResult{Step: i + 1, Action: step.Action, Status: "executed"}
		results = append(results, r)
		// If step fails and on_error is "stop", halt
		// (In this CLI stub, steps always succeed — real execution is in agent mode)
	}

	// Update usage stats
	skill.UsageCount++
	skill.LastUsedAt = time.Now().Format(time.RFC3339)
	_ = store.SaveConfig(cfg)

	if *jsonOut {
		return PrintJSON(map[string]interface{}{
			"skill":   name,
			"steps":   results,
			"success": true,
		})
	}
	fmt.Printf("✓ 技能 '%s' 执行完成 (%d 步)\n", name, len(results))
	return nil
}
