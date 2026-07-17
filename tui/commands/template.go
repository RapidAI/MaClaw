package commands

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote")

// RunTemplate 执行 template 子命令。
func RunTemplate(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui template <list|create|delete>")
	}
	mgr, err := templateManager(dataDir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return templateList(mgr, args[1:])
	case "create":
		return templateCreate(mgr, args[1:])
	case "delete":
		return templateDelete(mgr, args[1:])
	default:
		return NewUsageError("unknown template action: %s", args[0])
	}
}

func templateManager(dataDir string) (*remote.SessionTemplateManager, error) {
	path := filepath.Join(dataDir, "session_templates.json")
	return remote.NewSessionTemplateManager(path)
}

func templateList(mgr *remote.SessionTemplateManager, args []string) error {
	fs := flag.NewFlagSet("template list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	templates := mgr.List()
	if *jsonOut {
		return PrintJSON(templates)
	}
	if len(templates) == 0 {
		Println("No templates.")
		return nil
	}
	Printf("%-20s %-12s %-40s %s\n", "NAME", "TOOL", "PROJECT", "CREATED")
	Println(strings.Repeat("-", 80))
	for _, t := range templates {
		project := t.ProjectPath
		if len(project) > 40 {
			project = "..." + project[len(project)-37:]
		}
		Printf("%-20s %-12s %-40s %s\n", t.Name, t.Tool, project, t.CreatedAt)
	}
	return nil
}

func templateCreate(mgr *remote.SessionTemplateManager, args []string) error {
	fs := flag.NewFlagSet("template create", flag.ExitOnError)
	name := fs.String("name", "", "模板名称（必填）")
	tool := fs.String("tool", "", "工具名称（必填）")
	project := fs.String("project", "", "项目路径")
	model := fs.String("model", "", "模型配置")
	yolo := fs.Bool("yolo", false, "YOLO 模式")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *name == "" || *tool == "" {
		return NewUsageError("usage: template create --name <name> --tool <tool> [--project <path>]")
	}

	tpl := remote.SessionTemplate{
		Name:        *name,
		Tool:        *tool,
		ProjectPath: *project,
		ModelConfig: *model,
		YoloMode:    *yolo,
	}
	if err := mgr.Create(tpl); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"name": *name, "status": "created"})
	}
	Printf("Template %q created.\n", *name)
	return nil
}

func templateDelete(mgr *remote.SessionTemplateManager, args []string) error {
	fs := flag.NewFlagSet("template delete", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: template delete <name>")
	}
	name := fs.Arg(0)
	if err := mgr.Delete(name); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"name": name, "status": "deleted"})
	}
	Printf("Template %q deleted.\n", name)
	return nil
}

// ResolveDataDir 获取数据目录。
func ResolveDataDir() string {
	dir := os.Getenv("MACLAW_DATA_DIR")
	if dir == "" {
		dir = corelib.MaclawBaseDir()
	}
	return dir
}
