package main

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
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
				return "推送成功"
			}
			return out
		},
	})

	// --- File search tool ---
	registry.Register(RegisteredTool{
		Name:        "search_files",
		Description: "搜索项目目录中的文件内容（纯 Go 实现，支持正则表达式）。自动跳过二进制文件、.git、node_modules 等目录。限制：最多返回 50 条匹配，单次搜索超时 60 秒。",
		Category:    ToolCategoryNonCode,
		Tags:        []string{"file", "search", "grep"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"project_path": map[string]string{"type": "string", "description": "要搜索的目录路径。必须是具体的项目目录（如 D:\\myproject），不要传用户家目录或磁盘根目录。留空则使用当前工作目录。"},
			"pattern":      map[string]string{"type": "string", "description": "搜索模式，支持 Go 正则表达式语法。如果正则无效则按字面子串匹配。"},
			"file_pattern": map[string]string{"type": "string", "description": "文件名过滤 glob（如 *.go, *.py, *.md）。留空搜索所有文本文件。"},
		},
		Required: []string{"pattern"},
		Source:   "non_code",
		HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			pattern := stringVal(args, "pattern")
			if pattern == "" {
				return "缺少 pattern 参数"
			}
			return searchFilesInProjectCtx(ctx, path, pattern, stringVal(args, "file_pattern"))
		},
	})

	registerCurrentDateTimeTool(registry, ToolCategoryNonCode, "non_code")

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
		HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
			path := stringVal(args, "project_path")
			if path == "" {
				path = app.getCurrentProjectPath()
			}
			return checkProjectHealthCtx(ctx, path)
		},
	})
}

func registerCurrentDateTimeTool(registry *ToolRegistry, category ToolCategory, source string) {
	if registry == nil {
		return
	}
	registry.Register(RegisteredTool{
		Name:        "current_datetime",
		Description: "Get current date and time with year, month, day, weekday, ISO week number, hour, minute, second, and timezone.",
		Category:    category,
		Tags:        []string{"time", "date", "datetime", "clock", "week"},
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      source,
		ExecutionContract: map[string]interface{}{
			"capabilities":            []interface{}{"time"},
			"deterministic":           true,
			"supports_direct":         true,
			"requires_agent_planning": false,
			"avg_latency_ms":          5,
		},
		Handler: func(args map[string]interface{}) string {
			now := time.Now()
			isoYear, isoWeek := now.ISOWeek()
			return fmt.Sprintf(
				"%04d-%02d-%02d %s ISO week %04d-W%02d %02d:%02d:%02d (timezone: %s)",
				now.Year(), int(now.Month()), now.Day(),
				now.Weekday().String(), isoYear, isoWeek,
				now.Hour(), now.Minute(), now.Second(),
				now.Location().String(),
			)
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
// searchMatch is a single grep result (file + line + content).
type searchMatch struct {
	Path string
	Line int
	Text string
}

func searchFilesInProject(projectPath, pattern, filePattern string) string {
	return searchFilesInProjectCtx(context.Background(), projectPath, pattern, filePattern)
}

func searchFilesInProjectCtx(parent context.Context, projectPath, pattern, filePattern string) string {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return "search cancelled"
	}
	if projectPath == "" {
		return "未指定项目路径"
	}

	// Quick sanity check: reject overly broad paths that would waste time.
	// Common mistake: LLM passes user home dir or drive root as project_path.
	if isOverlyBroadSearchPath(projectPath) {
		return fmt.Sprintf("project_path=%q 范围过大（用户家目录或磁盘根目录）。请指定具体的项目目录，如 %s 下的某个子文件夹。", projectPath, projectPath)
	}

	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()

	re, err := regexp.Compile(pattern)
	if err != nil {
		// Pattern is not valid regex — fall back to literal substring match.
		re = nil
	}

	var (
		results []searchMatch
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, runtime.NumCPU()) // concurrency limiter
		count   atomic.Int32
	)

	const (
		maxResults  = 50
		maxFileSize = 1 << 20 // 1 MB
		maxLineLen  = 500
	)

	done := func() bool {
		return count.Load() >= maxResults
	}

	_ = filepath.WalkDir(projectPath, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil || done() {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".gocache" ||
				name == "__pycache__" || name == ".venv" || name == "vendor" ||
				name == ".maclaw" || name == ".claude" || name == ".aicoder" ||
				name == "dist" || name == "build" || name == ".next" {
				return filepath.SkipDir
			}
			return nil
		}
		// File pattern filter
		if filePattern != "" {
			matched, _ := filepath.Match(filePattern, d.Name())
			if !matched {
				return nil
			}
		}
		// Skip large files
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize || info.Size() == 0 {
			return nil
		}
		// Skip likely binary extensions
		if isBinaryExtension(d.Name()) {
			return nil
		}

		wg.Add(1)
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			return filepath.SkipAll
		}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			if ctx.Err() != nil || done() {
				return
			}

			f, err := os.Open(path)
			if err != nil {
				return
			}
			defer f.Close()

			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 256*1024)
			lineNum := 0
			for scanner.Scan() {
				if ctx.Err() != nil || done() {
					return
				}
				lineNum++
				line := scanner.Text()
				matched := false
				if re != nil {
					matched = re.MatchString(line)
				} else {
					matched = strings.Contains(line, pattern)
				}
				if matched {
					rel, _ := filepath.Rel(projectPath, path)
					if rel == "" {
						rel = path
					}
					text := line
					if len(text) > maxLineLen {
						text = text[:maxLineLen] + "..."
					}
					mu.Lock()
					if len(results) < maxResults {
						results = append(results, searchMatch{Path: rel, Line: lineNum, Text: text})
						count.Add(1)
					}
					mu.Unlock()
					if count.Load() >= maxResults {
						return
					}
					// Continue scanning for more matches in same file (up to global limit)
				}
			}
		}()
		return nil
	})

	wg.Wait()

	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("[search_files] timed out (60s) path=%s pattern=%q found=%d", projectPath, pattern, len(results))
		if len(results) > 0 {
			return formatSearchResults(results, true)
		}
		return "搜索超时（60秒），目录可能过大。请缩小搜索范围或指定更具体的 project_path。"
	}

	if ctx.Err() == context.Canceled {
		return "search_files cancelled"
	}

	if len(results) == 0 {
		return "未找到匹配结果"
	}
	return formatSearchResults(results, false)
}

func formatSearchResults(results []searchMatch, truncated bool) string {
	var sb strings.Builder
	suffix := ""
	if truncated {
		suffix = "（搜索超时，结果可能不完整）"
	}
	sb.WriteString(fmt.Sprintf("找到 %d 个匹配%s:\n", len(results), suffix))
	for _, m := range results {
		line := fmt.Sprintf("%s:%d: %s\n", m.Path, m.Line, m.Text)
		if sb.Len()+len(line) > 5000 {
			sb.WriteString("...(结果已截断)\n")
			break
		}
		sb.WriteString(line)
	}
	return sb.String()
}

// isOverlyBroadSearchPath returns true if the path is a drive root, user home
// directory, or other location known to contain hundreds of thousands of files.
// Searching these wastes time and produces irrelevant results.
func isOverlyBroadSearchPath(path string) bool {
	clean := filepath.Clean(path)

	// Unix root (or Windows path separator root)
	if clean == "/" || clean == "\\" || clean == "//" {
		return true
	}

	// Drive root: C:\, D:\ (Windows)
	vol := filepath.VolumeName(clean)
	if vol != "" && (clean == vol+string(filepath.Separator) || clean == vol) {
		return true
	}

	// User home directory (Windows: C:\Users\xxx, Unix: /home/xxx or /Users/xxx)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.EqualFold(filepath.Clean(home), clean) {
			return true
		}
	}

	// Windows: C:\Users itself (parent of all user profiles)
	if vol != "" {
		usersDir := filepath.Clean(vol + `\Users`)
		if strings.EqualFold(usersDir, clean) {
			return true
		}
	}

	return false
}

// isBinaryExtension returns true for file extensions that are almost certainly binary.
func isBinaryExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".exe", ".dll", ".so", ".dylib", ".bin", ".obj", ".o", ".a", ".lib",
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".svg", ".webp", ".tif", ".tiff",
		".mp3", ".mp4", ".avi", ".mov", ".mkv", ".flac", ".wav", ".ogg", ".webm",
		".zip", ".gz", ".tar", ".rar", ".7z", ".bz2", ".xz", ".zst",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		".pyc", ".pyo", ".class", ".jar", ".war",
		".db", ".sqlite", ".sqlite3", ".ldb",
		".pack", ".idx":
		return true
	}
	return false
}

// checkProjectHealth checks if a project can build/compile.
// Build commands have a 30-second timeout to avoid blocking the agent.
func checkProjectHealth(projectPath string) string {
	return checkProjectHealthCtx(context.Background(), projectPath)
}

func checkProjectHealthCtx(parent context.Context, projectPath string) string {
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return "check_health cancelled"
	}
	if projectPath == "" {
		return "未指定项目路径"
	}

	var results []string

	// Check for Go project.
	if _, err := os.Stat(filepath.Join(projectPath, "go.mod")); err == nil {
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "vet", "./...")
		cmd.Dir = projectPath
		hideCommandWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.Canceled {
				results = append(results, "check_health cancelled")
			} else if ctx.Err() == context.DeadlineExceeded {
				results = append(results, "Go vet 超时（30s），项目可能较大")
			} else {
				results = append(results, fmt.Sprintf("Go vet 发现问题:\n%s", string(out)))
			}
		} else {
			results = append(results, "Go vet 通过")
		}
	}

	// Check for Node.js project.
	if _, err := os.Stat(filepath.Join(projectPath, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(projectPath, "node_modules")); os.IsNotExist(err) {
			results = append(results, "Node.js: node_modules 不存在，需要 npm install")
		} else {
			results = append(results, "Node.js: 依赖已安装")
		}
	}

	// Check for Python project.
	if _, err := os.Stat(filepath.Join(projectPath, "requirements.txt")); err == nil {
		results = append(results, "Python 项目（requirements.txt 存在）")
	}

	if len(results) == 0 {
		return "未检测到已知项目类型"
	}
	return strings.Join(results, "\n")
}

// getCurrentProjectPath returns the default path for non-code tools when the
// caller omits project_path. Prefer the top-bar working directory so git/etc.
// operate where the user is working, not an unrelated Projects-list entry.
func (a *App) getCurrentProjectPath() string {
	if a == nil {
		return ""
	}
	if dir := strings.TrimSpace(a.EffectiveDesktopWorkingDir()); dir != "" {
		return dir
	}
	return a.GetCurrentProjectPath()
}
