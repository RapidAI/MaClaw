package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// RunSystem 执行 system 子命令。
func RunSystem(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui system <info|python-envs>")
	}
	switch args[0] {
	case "info":
		return systemInfo(args[1:])
	case "python-envs":
		return systemPythonEnvs(args[1:])
	default:
		return NewUsageError("unknown system action: %s", args[0])
	}
}

func systemInfo(args []string) error {
	fs := flag.NewFlagSet("system info", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	home, _ := os.UserHomeDir()
	dataDir := ResolveDataDir()

	info := map[string]interface{}{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"go":       runtime.Version(),
		"cpus":     runtime.NumCPU(),
		"home":     home,
		"data_dir": dataDir,
	}

	if *jsonOut {
		return PrintJSON(info)
	}
	fmt.Println("系统信息:")
	fmt.Printf("  OS:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go:       %s\n", runtime.Version())
	fmt.Printf("  CPUs:     %d\n", runtime.NumCPU())
	fmt.Printf("  Home:     %s\n", home)
	fmt.Printf("  DataDir:  %s\n", dataDir)
	return nil
}

func systemPythonEnvs(args []string) error {
	fs := flag.NewFlagSet("system python-envs", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	type pyEnv struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	var envs []pyEnv

	// 检测系统 Python
	for _, cmd := range []string{"python3", "python"} {
		if path, err := exec.LookPath(cmd); err == nil {
			envs = append(envs, pyEnv{Name: cmd, Path: path, Type: "system"})
		}
	}

	// 检测 conda 环境
	condaCmd := findConda()
	if condaCmd != "" {
		out, err := exec.Command(condaCmd, "env", "list", "--json").Output()
		if err == nil {
			// 简单解析 conda env list 输出
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "\"") && !strings.Contains(line, "envs") {
					p := strings.Trim(line, "\",")
					if p != "" {
						name := "base"
						parts := strings.Split(p, string(os.PathSeparator))
						if len(parts) > 0 {
							name = parts[len(parts)-1]
						}
						envs = append(envs, pyEnv{Name: name, Path: p, Type: "conda"})
					}
				}
			}
		}
	}

	if *jsonOut {
		return PrintJSON(envs)
	}
	if len(envs) == 0 {
		fmt.Println("未检测到 Python 环境。")
		return nil
	}
	fmt.Printf("%-20s %-10s %s\n", "NAME", "TYPE", "PATH")
	fmt.Println(strings.Repeat("-", 60))
	for _, e := range envs {
		fmt.Printf("%-20s %-10s %s\n", e.Name, e.Type, e.Path)
	}
	return nil
}

func findConda() string {
	for _, cmd := range []string{"conda", "mamba", "micromamba"} {
		if path, err := exec.LookPath(cmd); err == nil {
			return path
		}
	}
	return ""
}
