package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultWriteMode = "overwrite"

type PathResolver func() string

func ResolveFileToolPath(path string, projectDirResolver PathResolver) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("缺少 path 参数")
	}
	p := strings.TrimSpace(path)
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if projectDirResolver != nil {
		if base := strings.TrimSpace(projectDirResolver()); base != "" {
			return filepath.Clean(filepath.Join(base, p)), nil
		}
	}
	cwd, err := os.Getwd()
	if err == nil && strings.TrimSpace(cwd) != "" {
		return filepath.Clean(filepath.Join(cwd, p)), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(p), nil
	}
	return filepath.Clean(filepath.Join(home, p)), nil
}

func NormalizeWriteMode(mode string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return DefaultWriteMode, nil
	}
	switch m {
	case "overwrite", "append":
		return m, nil
	default:
		return "", fmt.Errorf("不支持的 mode: %s", mode)
	}
}

func WriteTextFile(path, content, mode string) (int64, error) {
	m, err := NormalizeWriteMode(mode)
	if err != nil {
		return 0, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("创建目录失败 %s: %w", dir, err)
	}

	if m == "append" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, fmt.Errorf("打开文件失败 %s: %w", path, err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return 0, fmt.Errorf("写入内容失败: %w", err)
		}
	} else {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return 0, fmt.Errorf("写入文件失败 %s: %w", path, err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("写入验证失败 %s: %w", path, err)
	}
	return info.Size(), nil
}

type EditTextFileResult struct {
	Count int
	Size  int64
	Path  string
}

func EditTextFile(path, oldString, newString string, replaceAll bool) (*EditTextFileResult, error) {
	if oldString == "" {
		return nil, fmt.Errorf("缺少 old_string 参数")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}
	text := string(data)
	count := strings.Count(text, oldString)
	if count == 0 {
		return nil, fmt.Errorf("未找到要替换的内容")
	}
	updated := text
	applied := 1
	if replaceAll {
		updated = strings.ReplaceAll(text, oldString, newString)
		applied = count
	} else {
		updated = strings.Replace(text, oldString, newString, 1)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败 %s: %w", path, err)
	}

	// Verify write succeeded.
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("写入验证失败 %s: %w", path, err)
	}
	return &EditTextFileResult{Count: applied, Size: info.Size(), Path: path}, nil
}
