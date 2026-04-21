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
	"coding-workflow.md":   defaultCodingWorkflow,
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

const defaultCodingWorkflow = `---
inclusion: always
priority: 10
---
# 修复原则

每个问题的修复，都要从机制上分析与修复，而不是做 workaround。Workaround 在换个场景会失效。每次提出修复方案时，先审视：这是不是机制上的通用修复？如果不是，重新设计。

# 任务类型判断

收到用户请求后，先判断任务类型：

- **编码任务**：涉及写代码、改代码、修 bug、设计架构、开发游戏/应用/工具 → 走编程任务三阶段流程
- **内容处理任务**：翻译、整理、总结、格式转换、字幕处理、文档梳理 → 直接执行，不要反复确认

# 反循环规则

- 已经搜索过的目录/文件，不要再搜索第二次
- 用户已经明确回答过的问题，不要再问
- 不要把"列出已完成的工作"当作"执行新工作"
- 如果发现自己在做重复的事情，立即停下来，直接执行核心任务
`

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
