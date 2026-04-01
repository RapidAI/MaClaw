package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// registerNonCodeTools registers non-programming tools (Git, file ops, env)
// into the ToolRegistry. These complement the builtin tools with project-aware
// operations that go through the SecurityFirewall.
func registerNonCodeTools(registry *ToolRegistry, app *App) {
	// --- Git tools ---
	registry.Register(RegisteredTool{
		Name:        "git_status",
		Description: "查看当前 Git 仓库状态（简洁格式）",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"git", "vcs", "status"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "项目路径（可选，默认当前项目）"},
		},
		Source: "non_code",
		Handler: func(args map[string]interface{}) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			if path == "" {
				return "未指定项目路径"
			}
			out, err := runGitCmd(path, "status", "--porcelain", "-b")
			if err != nil {
				return fmt.Sprintf("git status 失败: %v", err)
			}
			if strings.TrimSpace(out) == "" {
				return "工作区干净，没有未提交的变更"
			}
			return out
		},
	})

	registry.Register(RegisteredTool{
		Name:        "git_diff",
		Description: "查看 Git 差异摘要",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"git", "vcs", "diff"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "项目路径"},
			"staged":       map[string]string{"type": "boolean", "description": "是否查看暂存区差异（默认 false）"},
		},
		Source: "non_code",
		Handler: func(args map[string]interface{}) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			if path == "" {
				return "未指定项目路径"
			}
			gitArgs := []string{"diff", "--stat"}
			if staged, ok := args["staged"].(bool); ok && staged {
				gitArgs = append(gitArgs, "--cached")
			}
			out, err := runGitCmd(path, gitArgs...)
			if err != nil {
				return fmt.Sprintf("git diff 失败: %v", err)
			}
			if strings.TrimSpace(out) == "" {
				return "没有差异"
			}
			// Limit output.
			if len(out) > 4000 {
				out = out[:4000] + "\n...(已截断)"
			}
			return out
		},
	})

	registry.Register(RegisteredTool{
		Name:        "git_commit",
		Description: "提交已跟踪文件的变更到 Git（git add -u && git commit）。如需包含新文件，请先手动 git add。",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"git", "vcs", "commit", "write"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "项目路径"},
			"message":      map[string]string{"type": "string", "description": "提交信息"},
		},
		Required: []string{"message"},
		Source:   "non_code",
		Handler: func(args map[string]interface{}) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			msg := stringVal(args, "message")
			if msg == "" {
				return "缺少 message 参数"
			}
			// Use -u to only stage tracked files; avoids accidentally committing untracked files.
			if _, err := runGitCmd(path, "add", "-u"); err != nil {
				return fmt.Sprintf("git add 失败: %v", err)
			}
			out, err := runGitCmd(path, "commit", "-m", msg)
			if err != nil {
				return fmt.Sprintf("git commit 失败: %v", err)
			}
			return out
		},
	})

	registry.Register(RegisteredTool{
		Name:        "git_push",
		Description: "推送到远程仓库（git push）",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"git", "vcs", "push", "write"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "项目路径"},
		},
		Source: "non_code",
		Handler: func(args map[string]interface{}) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			out, err := runGitCmd(path, "push")
			if err != nil {
				return fmt.Sprintf("git push 失败: %v", err)
			}
			if strings.TrimSpace(out) == "" {
				return "✅ 推送成功"
			}
			return out
		},
	})

	// --- File search tool ---
	registry.Register(RegisteredTool{
		Name:        "search_files",
		Description: "在项目中搜索文件内容（支持正则表达式）",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"file", "search", "grep"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "项目路径"},
			"pattern":      map[string]string{"type": "string", "description": "搜索模式（支持正则）"},
			"file_pattern": map[string]string{"type": "string", "description": "文件名过滤（如 *.go, *.py）"},
		},
		Required: []string{"pattern"},
		Source:   "non_code",
		Handler: func(args map[string]interface{}) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			pattern := stringVal(args, "pattern")
			if pattern == "" {
				return "缺少 pattern 参数"
			}
			return searchFilesInProject(path, pattern, stringVal(args, "file_pattern"))
		},
	})

	// --- Environment tools ---
	registry.Register(RegisteredTool{
		Name:        "check_health",
		Description: "检查项目健康状态（编译是否通过、依赖是否完整）",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"health", "check", "build", "test"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "项目路径"},
		},
		Source: "non_code",
		Handler: func(args map[string]interface{}) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			return checkProjectHealth(path)
		},
	})
}

// runGitCmd executes a git command in the given directory.
func runGitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// searchFilesInProject searches for a pattern in project files.
func searchFilesInProject(projectPath, pattern, filePattern string) string {
	if projectPath == "" {
		return "未指定项目路径"
	}

	// Try ripgrep first, fall back to simple search.
	args := []string{"-n", "--max-count=50", pattern}
	if filePattern != "" {
		args = append(args, "-g", filePattern)
	}
	args = append(args, projectPath)

	cmd := exec.Command("rg", args...)
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		result := string(out)
		if len(result) > 5000 {
			result = result[:5000] + "\n...(结果已截断)"
		}
		return result
	}

	// Fallback: simple file walk with strings.Contains.
	var results []string
	count := 0
	_ = filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if count >= 50 {
			return filepath.SkipAll
		}
		if filePattern != "" {
			matched, _ := filepath.Match(filePattern, info.Name())
			if !matched {
				return nil
			}
		}
		if info.Size() > 1024*1024 { // skip files > 1MB
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), pattern) {
			rel, _ := filepath.Rel(projectPath, path)
			results = append(results, rel)
			count++
		}
		return nil
	})

	if len(results) == 0 {
		return "未找到匹配结果"
	}
	return fmt.Sprintf("找到 %d 个匹配文件:\n%s", len(results), strings.Join(results, "\n"))
}

// checkProjectHealth checks if a project can build/compile.
// Build commands have a 30-second timeout to avoid blocking the agent.
func checkProjectHealth(projectPath string) string {
	if projectPath == "" {
		return "未指定项目路径"
	}

	var results []string

	// Check for Go project.
	if _, err := os.Stat(filepath.Join(projectPath, "go.mod")); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "vet", "./...")
		cmd.Dir = projectPath
		hideCommandWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				results = append(results, "⚠️ Go vet 超时（30s），项目可能较大")
			} else {
				results = append(results, fmt.Sprintf("❌ Go vet 发现问题:\n%s", string(out)))
			}
		} else {
			results = append(results, "✅ Go vet 通过")
		}
	}

	// Check for Node.js project.
	if _, err := os.Stat(filepath.Join(projectPath, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(projectPath, "node_modules")); os.IsNotExist(err) {
			results = append(results, "⚠️ Node.js: node_modules 不存在，需要 npm install")
		} else {
			results = append(results, "✅ Node.js: 依赖已安装")
		}
	}

	// Check for Python project.
	if _, err := os.Stat(filepath.Join(projectPath, "requirements.txt")); err == nil {
		results = append(results, "ℹ️ Python 项目（requirements.txt 存在）")
	}

	if len(results) == 0 {
		return "未检测到已知项目类型"
	}
	return strings.Join(results, "\n")
}

// getCurrentProjectPath is a convenience alias for GetCurrentProjectPath.
func (a *App) getCurrentProjectPath() string {
	return a.GetCurrentProjectPath()
}
