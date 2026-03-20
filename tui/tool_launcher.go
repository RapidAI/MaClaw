package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/tui/commands"
	tea "github.com/charmbracelet/bubbletea"
)

// TUIToolLauncher 实现 corelib/tool.ToolLauncher 接口。
// 交互模式下使用 tea.ExecProcess 暂挂 TUI 并前台执行工具；
// headless 模式下直接 exec.Command 并输出到 stdout。
type TUIToolLauncher struct {
	program  *tea.Program
	headless bool
}

// NewTUIToolLauncher 创建 TUI 工具启动器。
func NewTUIToolLauncher(headless bool) *TUIToolLauncher {
	return &TUIToolLauncher{headless: headless}
}

// SetProgram 绑定 Bubble Tea Program（交互模式需要）。
func (l *TUIToolLauncher) SetProgram(p *tea.Program) {
	l.program = p
}

// Launch 实现 tool.ToolLauncher 接口。
func (l *TUIToolLauncher) Launch(ctx context.Context, opts tool.LaunchOptions) error {
	if opts.Mode == tool.LaunchHeadless || l.headless {
		return l.launchHeadless(ctx, opts)
	}
	return l.launchInteractive(ctx, opts)
}

// SupportsMode 实现 tool.ToolLauncher 接口。
func (l *TUIToolLauncher) SupportsMode(mode tool.ToolLaunchMode) bool {
	switch mode {
	case tool.LaunchInteractive:
		return !l.headless && l.program != nil
	case tool.LaunchHeadless:
		return true
	default:
		return false
	}
}

// launchInteractive 使用 tea.ExecProcess 暂挂 TUI 前台执行。
func (l *TUIToolLauncher) launchInteractive(_ context.Context, opts tool.LaunchOptions) error {
	if l.program == nil {
		return fmt.Errorf("no tea.Program bound, cannot launch interactive tool")
	}

	cmd := exec.Command(opts.Tool, opts.Args...)
	cmd.Dir = opts.ProjectDir
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if len(cmd.Env) > 0 {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}

	// tea.ExecProcess 会暂挂 TUI，将终端交给子进程
	cb := tea.ExecProcess(cmd, func(err error) tea.Msg {
		return toolFinishedMsg{name: opts.Tool, err: err}
	})
	l.program.Send(cb)
	return nil
}

// launchHeadless 直接执行命令并输出到 stdout/stderr。
func (l *TUIToolLauncher) launchHeadless(_ context.Context, opts tool.LaunchOptions) error {
	cmd := exec.Command(opts.Tool, opts.Args...)
	cmd.Dir = opts.ProjectDir
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if len(cmd.Env) > 0 {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// toolFinishedMsg 工具执行完成的消息。
type toolFinishedMsg struct {
	name string
	err  error
}

// LaunchToolByName 加载配置、构建环境变量、启动指定工具。
// 这是 TUI 版本的 GUI LaunchTool 等价物。
func (l *TUIToolLauncher) LaunchToolByName(ctx context.Context, toolName, projectDir string, yoloMode, adminMode bool) error {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if projectDir == "" {
		projectDir = resolveProjectDir(cfg)
	}
	projectDir = filepath.Clean(projectDir)

	env, _, err := buildToolEnv(toolName, cfg, projectDir)
	if err != nil {
		return fmt.Errorf("构建环境变量失败: %w", err)
	}

	tn := normalizeToolName(toolName)
	args := buildToolArgs(tn, projectDir, yoloMode, adminMode)

	opts := tool.LaunchOptions{
		ProjectDir: projectDir,
		Tool:       tn,
		Env:        env,
		Args:       args,
		YoloMode:   yoloMode,
		AdminMode:  adminMode,
	}
	if l.headless {
		opts.Mode = tool.LaunchHeadless
	}
	return l.Launch(ctx, opts)
}

// resolveProjectDir 从配置中获取当前项目路径。
func resolveProjectDir(cfg corelib.AppConfig) string {
	for _, p := range cfg.Projects {
		if p.Id == cfg.CurrentProject {
			return p.Path
		}
	}
	if len(cfg.Projects) > 0 {
		return cfg.Projects[0].Path
	}
	home, _ := os.UserHomeDir()
	return home
}

// buildToolArgs 构建工具启动参数。
func buildToolArgs(tool, projectDir string, yoloMode, adminMode bool) []string {
	var args []string
	switch tool {
	case "claude":
		args = append(args, "--output-format", "stream-json")
		if yoloMode {
			args = append(args, "--dangerously-skip-permissions")
		}
	case "codex":
		if yoloMode {
			args = append(args, "--full-auto")
		}
	case "gemini":
		// gemini CLI 无特殊参数
	}
	return args
}
