package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type ClaudeAdapter struct {
	app *App
}

func NewClaudeAdapter(app *App) *ClaudeAdapter {
	return &ClaudeAdapter{app: app}
}

func (a *ClaudeAdapter) ProviderName() string {
	return "claude"
}

func (a *ClaudeAdapter) ExecutionMode() ExecutionMode {
	return ExecModeSDK
}

func (a *ClaudeAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("claude")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("claude is not installed")
	}

	// Ensure Claude Code's onboarding/first-run wizard has been marked
	// as complete so it doesn't block the session with interactive prompts.
	// Pass the API key so it gets added to customApiKeyResponses.approved,
	// preventing the interactive API key confirmation dialog that would
	// cause an immediate exit with code 1 in SDK mode.
	apiKey := spec.Env["ANTHROPIC_AUTH_TOKEN"]
	if err := ensureClaudeOnboardingComplete(a.app, spec.ProjectPath, apiKey); err != nil {
		if a.app != nil {
			a.app.log(fmt.Sprintf("[claude-adapter] onboarding pre-check warning: %v", err))
		}
	}

	commandPath := a.resolveClaudeExecutable(status.Path)
	env := a.buildCommandEnv(spec.Env)

	// SDK mode: use stream-json for structured communication
	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	// Permission handling via SDK protocol
	if spec.YoloMode {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		// Use stdio permission prompt tool so we can handle approvals
		args = append(args, "--permission-prompt-tool", "stdio")
	}

	// Inject screenshot capability instructions so Claude Code knows
	// it can capture screenshots using platform-native tools.
	if prompt := buildScreenshotSystemPrompt(); prompt != "" {
		args = append(args, "--append-system-prompt", prompt)
	}

	return CommandSpec{
		Command: commandPath,
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}

func (a *ClaudeAdapter) resolveClaudeExecutable(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		exePath := filepath.Join(filepath.Dir(cleaned), "claude.exe")
		if _, err := os.Stat(exePath); err == nil {
			return exePath
		}
	}
	return cleaned
}

// buildScreenshotSystemPrompt returns platform-specific instructions that
// teach Claude Code how to capture screenshots using its Bash tool.
func buildScreenshotSystemPrompt() string {
	switch runtime.GOOS {
	case "windows":
		return `You have the ability to capture screenshots of the desktop or specific application windows. This is useful for debugging GUI applications, verifying visual output, or showing the user what's on screen.

To capture a FULL SCREEN screenshot, run this PowerShell command via Bash:
powershell -NoProfile -NonInteractive -Command "Add-Type -AssemblyName System.Drawing; Add-Type -AssemblyName System.Windows.Forms; $bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; $bmp = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size); $g.Dispose(); $ms = New-Object System.IO.MemoryStream; $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose(); $path = Join-Path $env:TEMP ('screenshot_' + (Get-Date -Format 'yyyyMMdd_HHmmss') + '.png'); [System.IO.File]::WriteAllBytes($path, $ms.ToArray()); $ms.Dispose(); Write-Output $path"

To capture a SPECIFIC WINDOW by title (partial match), run:
powershell -NoProfile -NonInteractive -Command "Add-Type -AssemblyName System.Drawing; Add-Type @'
using System; using System.Runtime.InteropServices; using System.Text;
public class WinAPI {
  public struct RECT { public int Left, Top, Right, Bottom; }
  [DllImport(\"user32.dll\")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
  [DllImport(\"user32.dll\")] public static extern bool EnumWindows(EnumWindowsProc proc, IntPtr lParam);
  [DllImport(\"user32.dll\", CharSet=CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder sb, int count);
  [DllImport(\"user32.dll\")] public static extern bool IsWindowVisible(IntPtr hWnd);
}
'@; $target = 'WINDOW_TITLE_HERE'; $found = $null; [WinAPI]::EnumWindows({ param($h,$l); if ([WinAPI]::IsWindowVisible($h)) { $sb = New-Object Text.StringBuilder 256; [WinAPI]::GetWindowText($h, $sb, 256) | Out-Null; if ($sb.ToString() -like ('*' + $target + '*')) { $script:found = $h } } return $true }, [IntPtr]::Zero) | Out-Null; if (-not $found) { Write-Error 'Window not found'; exit 1 }; $r = New-Object WinAPI+RECT; [WinAPI]::GetWindowRect($found, [ref]$r) | Out-Null; $w = $r.Right - $r.Left; $h2 = $r.Bottom - $r.Top; $bmp = New-Object Drawing.Bitmap($w, $h2); $g = [Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($r.Left, $r.Top, 0, 0, (New-Object Drawing.Size($w,$h2))); $g.Dispose(); $ms = New-Object IO.MemoryStream; $bmp.Save($ms, [Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose(); $path = Join-Path $env:TEMP ('screenshot_' + (Get-Date -Format 'yyyyMMdd_HHmmss') + '.png'); [IO.File]::WriteAllBytes($path, $ms.ToArray()); $ms.Dispose(); Write-Output $path"
Replace WINDOW_TITLE_HERE with the actual window title substring.

The command outputs the path to the saved PNG file. You can then read the file to show it to the user.
When the user asks you to take a screenshot or check what's on screen, use these commands.`
	case "darwin":
		return `You have the ability to capture screenshots. To capture the full screen: screencapture -x /tmp/screenshot.png
To capture a specific window: screencapture -x -w /tmp/screenshot.png
The file is saved as PNG. When the user asks to take a screenshot or check what's on screen, use these commands.`
	case "linux":
		return `You have the ability to capture screenshots. To capture the full screen: scrot /tmp/screenshot.png (or: import -window root /tmp/screenshot.png)
When the user asks to take a screenshot or check what's on screen, use these commands.`
	default:
		return ""
	}
}

func (a *ClaudeAdapter) buildCommandEnv(base map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range base {
		env[k] = v
	}

	home, _ := os.UserHomeDir()
	localToolPath := filepath.Join(home, ".cceasy", "tools")
	npmPath := filepath.Join(os.Getenv("AppData"), "npm")
	nodePath := `C:\Program Files\nodejs`
	gitCmdPath := `C:\Program Files\Git\cmd`
	gitBinPath := `C:\Program Files\Git\bin`
	gitUsrBinPath := `C:\Program Files\Git\usr\bin`

	basePath := env["PATH"]
	if strings.TrimSpace(basePath) == "" {
		basePath = os.Getenv("PATH")
	}
	env["PATH"] = strings.Join([]string{
		localToolPath,
		npmPath,
		nodePath,
		gitCmdPath,
		gitBinPath,
		gitUsrBinPath,
		basePath,
	}, ";")

	if env["CLAUDE_CODE_USE_COLORS"] == "" {
		env["CLAUDE_CODE_USE_COLORS"] = "true"
	}
	if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] == "" {
		env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "64000"
	}
	if env["CLAUDE_CODE_DISABLE_TERMINAL_TITLE"] == "" {
		env["CLAUDE_CODE_DISABLE_TERMINAL_TITLE"] = "1"
	}

	return env
}
