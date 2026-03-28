package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
)

// RunSystem 执行 system 子命令。
func RunSystem(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui system <info|python-envs|python-status|python-ensure>")
	}
	switch args[0] {
	case "info":
		return systemInfo(args[1:])
	case "python-envs":
		return systemPythonEnvs(args[1:])
	case "python-status":
		return systemPythonStatus(args[1:])
	case "python-ensure":
		return systemPythonEnsure(args[1:])
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

// systemPythonStatus 显示 Python 环境检测状态。
func systemPythonStatus(args []string) error {
	fs := flag.NewFlagSet("system python-status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	st := pyenv.Detect()
	if *jsonOut {
		return PrintJSON(st)
	}

	fmt.Println("Python 环境状态:")
	if st.Available {
		label := "系统"
		if st.IsPrivate {
			label = "私有"
		}
		fmt.Printf("  Python:  ✓ v%s (%s) → %s\n", st.Version, label, st.PythonPath)
	} else {
		fmt.Println("  Python:  ✗ 未检测到 (>= 3.10)")
	}
	if st.UVAvailable {
		fmt.Printf("  uv:      ✓ → %s\n", st.UVPath)
	} else {
		fmt.Println("  uv:      ✗ 未安装")
	}
	if st.VenvReady {
		fmt.Printf("  venv:    ✓ → %s\n", st.VenvPath)
	} else {
		fmt.Println("  venv:    ✗ 未创建")
	}
	return nil
}

// systemPythonEnsure 确保 Python 环境就绪（检测 + 自动安装）。
func systemPythonEnsure(args []string) error {
	fs := flag.NewFlagSet("system python-ensure", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	st := pyenv.EnsureEnvironment(func(stage string, pct int, msg string) {
		fmt.Printf("[%s] %d%% %s\n", stage, pct, msg)
	})

	if *jsonOut {
		return PrintJSON(st)
	}

	if st.Error != "" {
		return fmt.Errorf("Python 环境安装失败: %s", st.Error)
	}
	fmt.Printf("✓ Python %s 就绪, venv: %s\n", st.Version, st.VenvPath)
	return nil
}
