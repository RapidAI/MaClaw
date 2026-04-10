
# MaClaw 插件系统文档

## 简介

MaClaw 插件系统是一个统一的扩展框架，允许用户通过多种方式扩展 MaClaw 的功能。插件系统将 MCP Server、NLSkill 和本地脚本等扩展机制统一为 `Plugin` 接口，通过三层发现机制（用户级、项目级、包级）和统一的 `PluginRegistry` 进行生命周期管理。

### 核心特性

- **统一接口**：所有插件类型（MCP、LocalMCP、NLSkill、Script、Native）共享相同的生命周期接口
- **三层发现**：支持用户级（`~/.maclaw/plugins/`）、项目级（`.maclaw/plugins/`）、包级（Go entry points）插件
- **声明式配置**：通过 `plugin.yaml` 文件声明插件元数据和配置
- **热管理**：支持运行时启用/禁用插件，无需重启 MaClaw
- **健康检查**：后台定期检查插件健康状态
- **安全机制**：项目级插件首次加载需用户确认信任

## 插件类型

MaClaw 支持以下插件类型：

| 类型 | 说明 | 适用场景 |
|------|------|----------|
| `mcp` | 远程 MCP Server（通过 HTTP/WS 连接） | 已有的 MCP 服务器 |
| `local_mcp` | 本地 stdio MCP Server（通过命令启动） | 本地 Node.js/Python MCP 服务 |
| `nlskill` | 自然语言技能（多步骤自动化） | 复杂的自动化任务流 |
| `script` | 脚本工具（shell/Python/Node.js） | 简单的命令行工具封装 |
| `native` | 原生 Go 插件（编译时注册） | 高性能或深度集成需求 |

## 插件结构

每个插件是一个目录，包含：

```
plugin-name/
├── plugin.yaml      # 插件配置文件（必需）
└── run.sh           # 示例脚本（仅 script 类型需要）
```

### plugin.yaml 格式

```yaml
name: my-plugin
version: "1.0.0"
description: "A custom plugin"
type: script
author: "Your Name"
tags: ["tool", "automation"]

script:
  command: "bash"
  args: ["run.sh"]
  timeout: 30
  tool_name: "my_tool"
  description: "My custom tool"
  input_schema:
    query:
      type: string
      description: "Input query"
  required: ["query"]
```

## 使用方法

### 1. 创建插件

```bash
# 创建一个 script 类型插件（项目级）
maclaw-tui plugin create --type script --scope project my-tool

# 创建一个 local_mcp 插件（用户级）
maclaw-tui plugin create --type local_mcp --scope user my-mcp
```

### 2. 编辑插件配置

编辑生成的 `plugin.yaml`，配置插件参数。

### 3. 启用插件

```bash
maclaw-tui plugin enable my-tool
```

### 4. 禁用插件

```bash
maclaw-tui plugin disable my-tool
```

### 5. 查看插件列表

```bash
maclaw-tui plugin list
maclaw-tui plugin list --json
maclaw-tui plugin info my-tool
```

## 插件开发

### Script 插件开发

Script 插件是最简单的插件类型，只需编写一个可执行脚本。

**输入格式：** JSON 对象通过 stdin 传入

**输出格式：** stdout 输出结果（纯文本）

**示例：`run.sh`**

```bash
#!/bin/bash
INPUT=$(cat)
QUERY=$(echo "$INPUT" | jq -r '.query // "default"')
echo "Processing: $QUERY"
curl -s "https://api.example.com/search?q=$QUERY"
```

### LocalMCP 插件开发

LocalMCP 插件需要实现 MCP 协议（JSON-RPC over stdio）。

**基本流程：**
1. 启动时接收 `initialize` 请求
2. 返回 capabilities
3. 发送 `notifications/initialized` 通知
4. 接收 `tools/list` 请求，返回工具列表
5. 接收 `tools/call` 请求，执行工具并返回结果

### Native 插件开发

Native 插件是用 Go 编写的高性能插件，需要实现 `plugin.EntryPointProvider` 接口。

```go
type MyPlugin struct{}

func (p *MyPlugin) Manifest() plugin.PluginManifest {
    return plugin.PluginManifest{
        Name:        "my-native-tool",
        Version:     "1.0.0",
        Description: "A native Go plugin",
        Type:        plugin.PluginTypeNative,
    }
}
```

## 配置参考

### plugin.yaml 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 插件名称（字母数字、`-`、`_`） |
| `version` | string | 否 | 版本号 |
| `description` | string | 否 | 插件描述 |
| `type` | string | 是 | 插件类型：`mcp`、`local_mcp`、`nlskill`、`script`、`native` |
| `author` | string | 否 | 作者 |
| `tags` | []string | 否 | 标签列表 |
| `settings` | map | 否 | 自定义设置 |

## 环境变量

插件配置支持 `${ENV_VAR}` 语法：

```yaml
mcp:
  auth_secret: "${API_TOKEN}"
```

## 安全机制

- 不要在 `plugin.yaml` 中明文存储密钥
- 使用 `${ENV_VAR}` 语法从环境变量读取
- 项目级插件需要用户显式信任

## 最佳实践

1. 使用小写字母和连字符命名插件
2. 使用语义化版本（1.0.0）
3. 为耗时操作设置合理的超时时间
4. 在 description 中详细说明插件功能
5. 使用环境变量存储敏感信息

## 故障排查

### 插件未加载

1. 检查 `plugin.yaml` 是否存在且格式正确
2. 运行 `maclaw-tui plugin list` 查看插件状态
3. 查看 MaClaw 日志中的错误信息

### 脚本插件执行失败

1. 确保脚本有执行权限（`chmod +x run.sh`）
2. 检查脚本的 shebang 行（`#!/bin/bash` 或 `#!/usr/bin/env python3`）
3. 验证输入 JSON 格式是否正确

## 示例插件

### GitHub 搜索工具（script）

```yaml
name: github-search
type: script
script:
  command: "python"
  args: ["search.py"]
  input_schema:
    query:
      type: string
```

```python
# search.py
import json, sys, requests
args = json.loads(sys.stdin.read())
query = args.get("query", "")
url = "https://api.github.com/search/repositories"
params = {"q": query}
print(requests.get(url, params=params).json())
```

## CLI 命令参考

| 命令 | 说明 |
|------|------|
| `maclaw-tui plugin list` | 列出所有插件 |
| `maclaw-tui plugin list --json` | JSON 格式输出 |
| `maclaw-tui plugin info <name>` | 查看插件详情 |
| `maclaw-tui plugin enable <name>` | 启用插件 |
| `maclaw-tui plugin disable <name>` | 禁用插件 |
| `maclaw-tui plugin create` | 创建新插件 |

