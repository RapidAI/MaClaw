package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/config"
)

// RunConfig 执行 config 子命令。
func RunConfig(args []string, hubURL, token string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui config <get|set|export|import|schema>")
	}

	// export/import/schema 走本地文件，不需要 Hub 连接
	switch args[0] {
	case "export":
		return configExport(args[1:])
	case "import":
		return configImport(args[1:])
	case "schema":
		return configSchema(args[1:])
	case "get":
		// 检查是否有 --local 标志
		for _, a := range args[1:] {
			if a == "--local" || a == "-local" {
				return configGetLocal(args[1:])
			}
		}
	case "set":
		for _, a := range args[1:] {
			if a == "--local" || a == "-local" {
				return configSetLocal(args[1:])
			}
		}
	}

	// get/set 走 Hub WebSocket
	client := NewHubClient(hubURL, token)
	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()

	switch args[0] {
	case "get":
		return configGetRemote(client, args[1:])
	case "set":
		return configSet(client, args[1:])
	default:
		return NewUsageError("unknown config action: %s", args[0])
	}
}

func configGetRemote(client *HubClient, args []string) error {
	fs := flag.NewFlagSet("config get", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	key := fs.Arg(0)
	payload := map[string]string{}
	if key != "" {
		payload["key"] = key
	}

	data, err := client.Request("cli.config_get", payload)
	if err != nil {
		return err
	}

	if *jsonOut {
		var v interface{}
		json.Unmarshal(data, &v)
		return PrintJSON(v)
	}

	var kv map[string]interface{}
	if err := json.Unmarshal(data, &kv); err != nil {
		fmt.Println(string(data))
		return nil
	}
	for k, v := range kv {
		fmt.Printf("%s = %v\n", k, v)
	}
	return nil
}

func configSet(client *HubClient, args []string) error {
	if len(args) < 2 {
		return NewUsageError("usage: config set <key> <value>")
	}
	key, value := args[0], args[1]

	_, err := client.Request("cli.config_set", map[string]string{
		"key":   key,
		"value": value,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", key, value)
	return nil
}

func configGetLocal(args []string) error {
	fs := flag.NewFlagSet("config get --local", flag.ExitOnError)
	local := fs.Bool("local", false, "从本地文件读取")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	_ = local // 已经确认是 local 模式

	section := fs.Arg(0)
	if section == "" {
		section = "all"
	}

	mgr := config.NewManager(NewFileConfigStore(ResolveDataDir()))
	result, err := mgr.GetConfig(section)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(result)
		return nil
	}
	fmt.Print(result)
	return nil
}

// ConfigSecurityReadOnlyFn is set by the main package. When it returns true,
// security-related config set operations are blocked (centralized security mode).
var ConfigSecurityReadOnlyFn func() bool

// securitySections lists config sections that are read-only under centralized security.
var securitySections = map[string]bool{
	"security": true,
	"guardrail": true,
	"sandbox": true,
	"network": true,
}

func configSetLocal(args []string) error {
	fs := flag.NewFlagSet("config set --local", flag.ExitOnError)
	local := fs.Bool("local", false, "写入本地文件")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	_ = local

	remaining := fs.Args()
	if len(remaining) < 3 {
		return NewUsageError("usage: config set --local <section> <key> <value>")
	}
	section, key, value := remaining[0], remaining[1], remaining[2]

	// Block security-related config changes when centralized security is active (Req 4.6)
	if securitySections[section] && ConfigSecurityReadOnlyFn != nil && ConfigSecurityReadOnlyFn() {
		return fmt.Errorf("安全设置已由 Hub 集中管控，无法本地修改")
	}

	mgr := config.NewManager(NewFileConfigStore(ResolveDataDir()))
	oldVal, err := mgr.UpdateConfig(section, key, value)
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{
			"section":   section,
			"key":       key,
			"old_value": oldVal,
			"new_value": value,
		})
	}
	fmt.Printf("%s.%s: %s → %s\n", section, key, oldVal, value)
	return nil
}

// --- 本地配置操作（不需要 Hub） ---

func localConfigManager() *config.Manager {
	store := NewFileConfigStore(ResolveDataDir())
	return config.NewManager(store)
}

func configExport(args []string) error {
	fs := flag.NewFlagSet("config export", flag.ExitOnError)
	outFile := fs.String("o", "", "输出文件路径（默认 stdout）")
	fs.Parse(args)

	mgr := localConfigManager()
	exported, err := mgr.ExportConfig()
	if err != nil {
		return err
	}
	if *outFile != "" {
		if err := os.WriteFile(*outFile, []byte(exported), 0o644); err != nil {
			return fmt.Errorf("write export file: %w", err)
		}
		fmt.Printf("Config exported to %s\n", *outFile)
		return nil
	}
	fmt.Println(exported)
	return nil
}

func configImport(args []string) error {
	fs := flag.NewFlagSet("config import", flag.ExitOnError)
	inFile := fs.String("f", "", "导入文件路径（必填）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *inFile == "" {
		return NewUsageError("usage: config import -f <file.json>")
	}

	data, err := os.ReadFile(*inFile)
	if err != nil {
		return fmt.Errorf("read import file: %w", err)
	}

	mgr := localConfigManager()
	report, err := mgr.ImportConfig(string(data))
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(report)
	}
	fmt.Printf("Import complete: %d applied, %d skipped\n", report.Applied, report.Skipped)
	for _, w := range report.Warnings {
		fmt.Printf("  %s\n", w)
	}
	return nil
}

func configSchema(args []string) error {
	fs := flag.NewFlagSet("config schema", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	mgr := localConfigManager()
	if *jsonOut {
		s, err := mgr.SchemaJSON()
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	}

	schema := mgr.GetSchema()
	for _, sec := range schema {
		fmt.Printf("[%s] %s\n", sec.Name, sec.Description)
		for _, k := range sec.Keys {
			def := ""
			if k.Default != "" {
				def = fmt.Sprintf(" (default: %s)", k.Default)
			}
			vals := ""
			if len(k.ValidValues) > 0 {
				vals = fmt.Sprintf(" [%s]", strings.Join(k.ValidValues, ", "))
			}
			fmt.Printf("  %-35s %s (%s)%s%s\n", k.Key, k.Description, k.Type, def, vals)
		}
		fmt.Println()
	}
	return nil
}
