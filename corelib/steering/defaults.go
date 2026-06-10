package steering

import (
	"log"
	"os"
	"path/filepath"
)

// defaultFiles maps filename → default content for first-time setup.
// These are extracted from the hardcoded rules in gui/im_system_prompt.go.
// Users can freely modify these files; EnsureDefaults never overwrites.
var defaultFiles = map[string]string{
	"encoding-guidance.md": defaultEncodingGuidance,
	"ssh-operations.md":    defaultSSHOperations,
}

// EnsureDefaults creates default steering files in the user directory if they
// don't already exist. This is called once during app startup. It never
// overwrites user-modified files.
func EnsureDefaults(userDir string) error {
	if userDir == "" {
		return nil
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return err
	}
	for name, content := range defaultFiles {
		path := filepath.Join(userDir, name)
		if _, err := os.Stat(path); err == nil {
			continue // user file exists, don't overwrite
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			log.Printf("[steering] failed to write default %s: %v", name, err)
			continue
		}
		log.Printf("[steering] created default file: %s", path)
	}
	return nil
}

const defaultEncodingGuidance = `---
inclusion: always
priority: 90
---
# 文件编码规范

- write_file 工具始终以 UTF-8 编码写入，直接写中文即可
- bash 工具在 Windows 上已自动设置 UTF-8 输出编码
- 写入大文件（>3000 字符）时，使用 mode=append 分块写入
- Python 脚本写文件时始终指定 encoding='utf-8'
- 不要因为怀疑编码问题而反复尝试不同方案
`

const defaultSSHOperations = `---
inclusion: contextMatch
contextKeywords: ['ssh', '服务器', '远程', '部署', '运维', 'server', 'deploy']
priority: 50
---
# SSH 操作规范

- 优先使用内置 ssh 工具，禁止通过 bash 调用 ssh/scp/rsync 命令
- 长命令（安装/编译/下载）必须用 exec_background，不要用 exec
- exec_background 提交后等 10-30 秒再 check_task，不要频繁轮询
- 用户提供了密码时必须传 password 参数，不要省略
- 密钥认证失败时应询问用户密码并用 password 参数重试
`
