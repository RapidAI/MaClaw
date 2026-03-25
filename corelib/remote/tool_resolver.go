package remote

import (
	"os"
	"path/filepath"
	"runtime"
)

const toolsSubDir = ".maclaw/data/tools"

// ToolsDir 返回私有工具安装目录 ~/.maclaw/data/tools。
// 如果无法获取用户主目录，返回空字符串。
func ToolsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, toolsSubDir)
}

// PackageName 返回工具对应的 npm 包名。
// 非 npm 安装的工具返回空字符串。
func PackageName(toolName string) string {
	switch NormalizeRemoteToolName(toolName) {
	case "gemini":
		return "@google/gemini-cli"
	case "codex":
		return "@openai/codex"
	case "opencode":
		if runtime.GOOS == "windows" {
			return "opencode-windows-x64"
		}
		return "opencode-ai"
	case "codebuddy":
		return "@tencent-ai/codebuddy-code"
	case "iflow":
		return "@iflow-ai/iflow-cli"
	case "kilo":
		return "@kilocode/cli"
	default:
		return ""
	}
}

// BinaryNames 返回工具可能的可执行文件名列表。
func BinaryNames(toolName string) []string {
	name := NormalizeRemoteToolName(toolName)
	meta, ok := BuiltinToolInfos[name]
	base := name
	if ok {
		base = meta.BinaryName
	}

	switch name {
	case "claude":
		return []string{base, "claude-code"}
	case "codex":
		return []string{base, "openai"}
	case "opencode":
		if runtime.GOOS == "windows" {
			return []string{base, "opencode-windows-x64"}
		}
		return []string{base}
	case "codebuddy":
		return []string{"codebuddy", "codebuddy-code"}
	case "iflow":
		return []string{"iflow"}
	case "kilo":
		return []string{"kilo", "kilocode"}
	case "cursor":
		return []string{"cursor-agent", "agent"}
	case "gemini":
		return []string{"gemini"}
	default:
		return []string{base}
	}
}

// ResolveToolPath 在 ~/.maclaw/data/tools 私有目录下查找工具可执行文件。
// 返回找到的完整路径和是否找到。
func ResolveToolPath(toolName string) (string, bool) {
	name := NormalizeRemoteToolName(toolName)
	toolsRoot := ToolsDir()
	if toolsRoot == "" {
		return "", false
	}
	binNames := BinaryNames(name)

	for _, bn := range binNames {
		var path string
		if runtime.GOOS == "windows" {
			path = resolveWindows(toolsRoot, name, bn)
		} else {
			path = resolveUnix(toolsRoot, name, bn)
		}
		if path != "" {
			return path, true
		}
	}
	return "", false
}

func resolveWindows(toolsRoot, toolName, bn string) string {
	possiblePaths := []string{
		filepath.Join(toolsRoot, bn+".exe"),
		filepath.Join(toolsRoot, bn+".cmd"),
		filepath.Join(toolsRoot, bn+".bat"),
		filepath.Join(toolsRoot, bn+".ps1"),
		filepath.Join(toolsRoot, "bin", bn+".exe"),
		filepath.Join(toolsRoot, "bin", bn+".cmd"),
		filepath.Join(toolsRoot, bn),
		filepath.Join(toolsRoot, "bin", bn),
	}

	// opencode 特殊路径
	if toolName == "opencode" {
		possiblePaths = append(possiblePaths,
			filepath.Join(toolsRoot, "node_modules", "opencode-windows-x64", "bin", "opencode.exe"))
	}

	// node_modules 通用检查
	if pkgName := PackageName(toolName); pkgName != "" {
		base := filepath.Join(toolsRoot, "node_modules", pkgName, "bin", bn)
		possiblePaths = append(possiblePaths, base, base+".js")
	}

	for _, p := range possiblePaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func resolveUnix(toolsRoot, toolName, bn string) string {
	// 先查 tools root（原生安装如 claude），再查 bin/（npm 安装）
	rootPath := filepath.Join(toolsRoot, bn)
	if info, err := os.Stat(rootPath); err == nil && !info.IsDir() {
		return rootPath
	}

	binPath := filepath.Join(toolsRoot, "bin", bn)
	if info, err := os.Stat(binPath); err == nil && !info.IsDir() {
		return binPath
	}

	// Fallback: node_modules bin
	if pkgName := PackageName(toolName); pkgName != "" {
		modBin := filepath.Join(toolsRoot, "node_modules", pkgName, "bin", bn)
		if info, err := os.Stat(modBin); err == nil && !info.IsDir() {
			return modBin
		}
	}
	return ""
}

// ToolStatus 工具检测状态（GUI/TUI 共用）。
type ToolStatus struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Installed   bool   `json:"installed"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
}

// DetectAllTools 检测所有已知工具的安装状态。
func DetectAllTools() []ToolStatus {
	var tools []ToolStatus
	for _, name := range ToolOrder {
		meta, ok := BuiltinToolInfos[name]
		if !ok {
			continue
		}
		ts := ToolStatus{
			Name:        name,
			DisplayName: meta.DisplayName,
		}
		if path, found := ResolveToolPath(name); found {
			ts.Installed = true
			ts.Path = path
		}
		tools = append(tools, ts)
	}
	return tools
}
