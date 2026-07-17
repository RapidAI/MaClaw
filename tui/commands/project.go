package commands

import (
	"flag"
	"strings"
	"github.com/RapidAI/CodeClaw/corelib/project")

// RunProject 执行 project 子命令。
func RunProject(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui project <create|list|delete|switch>")
	}
	store := NewFileConfigStore(dataDir)
	switch args[0] {
	case "create":
		return projectCreate(store, args[1:])
	case "list":
		return projectList(store, args[1:])
	case "delete":
		return projectDelete(store, args[1:])
	case "switch":
		return projectSwitch(store, args[1:])
	default:
		return NewUsageError("unknown project action: %s", args[0])
	}
}

func projectCreate(store *FileConfigStore, args []string) error {
	fs := flag.NewFlagSet("project create", flag.ExitOnError)
	name := fs.String("name", "", "项目名称（必填）")
	path := fs.String("path", "", "项目路径（必填）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *name == "" || *path == "" {
		return NewUsageError("usage: project create --name <name> --path <path>")
	}

	res, err := project.Create(store, *name, *path)
	if err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "created"})
	}
	Printf("Project %q created at %s (id: %s)\n", res.Name, res.Path, res.Id)
	return nil
}

func projectList(store *FileConfigStore, args []string) error {
	fs := flag.NewFlagSet("project list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	items, err := project.List(store)
	if err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(items)
	}

	if len(items) == 0 {
		Println("No projects.")
		return nil
	}

	Printf("  %-25s %-20s %s\n", "ID", "NAME", "PATH")
	Println(strings.Repeat("-", 80))
	for _, p := range items {
		marker := " "
		if p.Current {
			marker = "*"
		}
		Printf("%s %-25s %-20s %s\n", marker, p.Id, p.Name, p.Path)
	}
	return nil
}

func projectDelete(store *FileConfigStore, args []string) error {
	fs := flag.NewFlagSet("project delete", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: project delete <name-or-id>")
	}

	res, err := project.Delete(store, fs.Arg(0))
	if err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(map[string]string{"id": res.Id, "name": res.Name, "status": "deleted"})
	}
	Printf("Project %q deleted.\n", res.Name)
	return nil
}

func projectSwitch(store *FileConfigStore, args []string) error {
	fs := flag.NewFlagSet("project switch", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: project switch <name-or-id>")
	}

	res, err := project.Switch(store, fs.Arg(0))
	if err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(map[string]string{"id": res.Id, "name": res.Name, "status": "switched"})
	}
	Printf("Switched to project %q (%s)\n", res.Name, res.Path)
	return nil
}
