package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunGitCmd executes a git command in the given directory.
func RunGitCmd(dir string, args ...string) (string, error) {
	cmd := Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// SearchFilesInProject searches for a pattern in project files.
func SearchFilesInProject(projectPath, pattern, filePattern string) string {
	if projectPath == "" {
		return "未指定项目路径"
	}

	args := []string{"-n", "--max-count=50", pattern}
	if filePattern != "" {
		args = append(args, "-g", filePattern)
	}
	args = append(args, projectPath)

	cmd := Command("rg", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		result := string(out)
		if len(result) > 5000 {
			result = result[:5000] + "\n...(结果已截断)"
		}
		return result
	}

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
		if info.Size() > 1024*1024 {
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

// CheckProjectHealth checks if a project can build/compile.
func CheckProjectHealth(projectPath string) string {
	if projectPath == "" {
		return "未指定项目路径"
	}

	var results []string

	if _, err := os.Stat(filepath.Join(projectPath, "go.mod")); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := CommandContext(ctx, "go", "vet", "./...")
		cmd.Dir = projectPath
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

	if _, err := os.Stat(filepath.Join(projectPath, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(projectPath, "node_modules")); os.IsNotExist(err) {
			results = append(results, "⚠️ Node.js: node_modules 不存在，需要 npm install")
		} else {
			results = append(results, "✅ Node.js: 依赖已安装")
		}
	}

	if _, err := os.Stat(filepath.Join(projectPath, "requirements.txt")); err == nil {
		results = append(results, "ℹ️ Python 项目（requirements.txt 存在）")
	}

	if len(results) == 0 {
		return "未检测到已知项目类型"
	}
	return strings.Join(results, "\n")
}

// StringVal extracts a string value from a map by key.
func StringVal(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
