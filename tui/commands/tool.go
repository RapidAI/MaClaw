package commands

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// RunTool 执行 tool 子命令。
func RunTool(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui tool <recommend|status>")
	}
	switch args[0] {
	case "recommend":
		return toolRecommend(args[1:])
	case "status":
		return toolStatus(args[1:])
	default:
		return NewUsageError("unknown tool action: %s", args[0])
	}
}

func toolRecommend(args []string) error {
	fs := flag.NewFlagSet("tool recommend", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	desc := strings.Join(fs.Args(), " ")
	if desc == "" {
		return NewUsageError("usage: maclaw-tui tool recommend <task description>")
	}

	// 检测已安装的工具
	knownTools := []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"}
	var installed []string
	for _, t := range knownTools {
		if _, err := exec.LookPath(t); err == nil {
			installed = append(installed, t)
		}
	}

	selector := tool.NewSelector()
	name, reason := selector.Recommend(desc, installed)

	if *jsonOut {
		return PrintJSON(map[string]string{
			"tool":      name,
			"reason":    reason,
			"installed": strings.Join(installed, ","),
		})
	}
	fmt.Printf("Recommended tool: %s\n", name)
	fmt.Printf("Reason: %s\n", reason)
	if len(installed) > 0 {
		fmt.Printf("Installed: %s\n", strings.Join(installed, ", "))
	} else {
		fmt.Println("Installed: (none detected)")
	}
	return nil
}

// DetectedTool 检测到的工具信息（供 TUI 视图复用）。
type DetectedTool struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Available   bool   `json:"available"`
	Path        string `json:"path,omitempty"`
}

// DetectTools 检测所有已知工具的安装状态。
func DetectTools() []DetectedTool {
	var tools []DetectedTool
	for _, name := range remote.ToolOrder {
		meta, ok := remote.BuiltinToolInfos[name]
		if !ok {
			continue
		}
		dt := DetectedTool{
			Name:        name,
			DisplayName: meta.DisplayName,
		}
		if path, err := exec.LookPath(meta.BinaryName); err == nil {
			dt.Available = true
			dt.Path = path
		}
		tools = append(tools, dt)
	}
	return tools
}

// DetectInstalledToolNames 返回已安装工具的名称列表。
func DetectInstalledToolNames() []string {
	tools := DetectTools()
	var names []string
	for _, t := range tools {
		if t.Available {
			names = append(names, t.Name)
		}
	}
	return names
}

func toolStatus(args []string) error {
	fs := flag.NewFlagSet("tool status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	tools := DetectTools()
	if *jsonOut {
		return PrintJSON(tools)
	}

	fmt.Printf("%-15s %-15s %-10s %s\n", "NAME", "DISPLAY", "STATUS", "PATH")
	fmt.Println(strings.Repeat("-", 65))
	for _, t := range tools {
		status := "✗ 未安装"
		if t.Available {
			status = "✓ 就绪"
		}
		fmt.Printf("%-15s %-15s %-10s %s\n", t.Name, t.DisplayName, status, t.Path)
	}
	return nil
}
