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
		return NewUsageError("usage: maclaw-tui nlskill <list|add|remove|enable|disable>")
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
