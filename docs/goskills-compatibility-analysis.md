# GoSkills 兼容性对比分析 — MacLaw Skill Runner 改进建议

> 基于 https://github.com/smallnest/goskills 的对比分析。
> 已实施的改进标记为 ✅。

## 一、两个项目的核心差异

| 维度 | goskills (smallnest) | MacLaw (本项目) |
|------|---------------------|-----------------|
| Skill 格式 | Claude SKILL.md (frontmatter + markdown) + OpenAI skill.md | skill.yaml + SKILL.md (markdown 提取步骤) |
| 执行模型 | LLM 驱动的 tool-calling 循环（最多 20 轮） | 预定义步骤顺序执行（sequential / api_workflow / interactive） |
| 工具系统 | bash + tavily_search + 脚本工具 + MCP | bash + craft_tool + call_mcp_tool + 多种内置工具 |
| Shell 处理 | 统一 `bash -c`，无 Windows 适配 | 完整的 Windows 兼容（PowerShell/cmd/bash 自动选择） |
| MCP 集成 | 独立 MCP Client，`serverName__toolName` 命名 + 重连重试 | 通过 corelib MCP 注册表，工具名直接使用 |
| Skill 来源 | GitHub URL 下载 + 本地目录扫描 | Hub 安装 + 本地文件 + GitHub 搜索 + zip 导入 |
| 错误处理 | 简单的 error 返回 + LLM 自行修正 | 分类错误（exit code / ENOENT / 429 / timeout）+ 友好提示 |

## 二、goskills 中值得借鉴的设计

### 2.1 LLM 参与的 Skill 选择（Skill Selection via LLM）

goskills 的 `selectSkill()` 让 LLM 根据用户请求从可用 skill 列表中选择最合适的一个，而不是要求用户精确指定 skill 名称。

**当前状态**：MacLaw 的 `run_skill` 工具要求 LLM 传入精确的 `skill_name`，依赖 LLM 自己从 `list_skills` 结果中匹配。

**建议**：在 `toolRunSkill` 中增加模糊匹配 fallback。当精确匹配失败时，用 BM25 或简单的关键词匹配从已安装 skill 中找最接近的候选，返回建议而不是直接报错。`corelib/skill/scanner.go` 已有 `FindSimilarSkill()` 函数，可以直接复用。

**优先级**：P2（改善用户体验，减少 LLM 调用失败率）

### 2.2 MCP 工具调用的重连与重试机制

goskills 的 MCP Client 实现了：
- 连接错误检测（EOF / broken pipe / connection reset）
- 指数退避重连（1s → 2s → 3s）
- 最多 3 次重试

**当前状态**：MacLaw 的 MCP 工具调用没有连接级别的重试机制。MCP server 进程崩溃或连接断开后，工具调用直接失败。

**建议**：在 `corelib/remote/tool_resolver.go` 或 MCP 调用层增加连接错误检测和自动重连逻辑。参考 goskills 的 `isConnectionError()` 判断逻辑。

**优先级**：P1（MCP server 不稳定是实际生产问题）

### 2.3 LLM Tool Arguments 清洗（cleanToolArguments）

goskills 的 `cleanToolArguments()` 处理了多种 LLM 返回的畸形 JSON：
- 去除 ` ```json ``` ` 代码围栏
- 去除单引号包裹
- 修复过度转义的引号（`\"` → `"`）
- 修复不必要的单引号转义

**当前状态**：MacLaw 的工具参数解析直接 `json.Unmarshal`，没有对 LLM 返回的畸形 JSON 做预处理。小模型（如 DeepSeek、Qwen 等）经常返回带代码围栏或过度转义的 JSON。

**建议**：在 `corelib/tool/craft.go` 或工具调用入口处增加类似的参数清洗函数。这对支持更多 LLM provider 尤其重要。

**优先级**：P1（直接影响小模型的工具调用成功率）

### 2.4 Claude SKILL.md 格式兼容

goskills 支持两种 skill 格式：
1. Claude 格式：`SKILL.md` + YAML frontmatter（name/description/allowed-tools/tools）
2. OpenAI 格式：`skill.md`（纯 markdown，从目录名推断 name）

**当前状态**：MacLaw 的 scanner 支持 `skill.yaml` + `SKILL.md`（markdown 提取步骤），但不支持 Claude 原生的 SKILL.md frontmatter 格式。这意味着从 `awesome-claude-skills` 等社区仓库下载的 skill 无法直接使用。

**建议**：在 `corelib/skill/scanner.go` 的 `loadSkillFromDir` 中增加对 Claude SKILL.md frontmatter 格式的解析。检测到 `---` frontmatter 时，提取 `name`、`description`、`allowed-tools`、`tools` 等字段，将 `tools` 中定义的脚本映射为 bash 步骤。

**实现要点**：
- 解析 YAML frontmatter 中的 `tools` 数组，每个 tool 有 `name`、`script`、`description`、`parameters`
- 将 `scripts/` 目录下的脚本自动映射为 bash 步骤
- `allowed-tools` 字段可映射为 skill 的 `required_env` 或工具白名单

**优先级**：P1（直接扩大可用 skill 生态）

### 2.5 GitHub Skill 下载功能

goskills 的 `download` 命令支持从 GitHub URL 直接下载 skill：
- 解析 `github.com/owner/repo/tree/branch/path` 格式
- 递归下载目录和文件
- 自动替换路径引用（`~/.claude/skills` → `~/.goskills/skills`）
- 支持 data URL（base64 编码内容）

**当前状态**：MacLaw 的 `github_search.go` 已有 GitHub 搜索功能，但下载后的 skill 需要手动适配格式。没有自动路径替换。

**建议**：在 GitHub skill 导入流程中增加路径替换逻辑，将 `~/.claude/skills` 替换为本项目的 skill 目录路径。同时在导入时自动检测 skill 格式（Claude SKILL.md vs skill.yaml）并做必要的转换。

**优先级**：P2

### 2.6 脚本工具自动发现（Script Tool Auto-Discovery）

goskills 的 `GenerateToolDefinitions()` 自动扫描 skill 的 `scripts/` 目录，为每个脚本生成对应的工具定义：
- `.py` → `python3 script.py`
- `.ts`/`.js` → `npx tsx script.ts`
- 其他 → `bash script.sh`
- 支持 SKILL.md 中显式定义工具参数 schema

**当前状态**：MacLaw 的 skill 步骤必须在 `skill.yaml` 中显式定义每个 bash 命令。`scripts/` 目录下的脚本不会被自动发现。

**建议**：在 `loadSkillFromDir` 中，当 skill 目录包含 `scripts/` 子目录但 `skill.yaml` 没有定义步骤时，自动扫描 `scripts/` 并生成 bash 步骤。这样 Claude 社区的 skill 可以开箱即用。

**优先级**：P2

## 三、MacLaw 已有但 goskills 缺失的能力（无需改动）

以下是 MacLaw 已经做得比 goskills 好的地方，确认无需回退：

1. **Windows 兼容性**：goskills 的 `bash.go` 硬编码 `bash -c`，在 Windows 上完全不可用。MacLaw 已有完整的 Windows shell 选择、8.3 短路径解析、shebang 处理。
2. **步骤间状态传递**：goskills 没有 capture/vars 机制，步骤间无法传递数据。MacLaw 已实现。
3. **条件执行**：goskills 没有 `on_failure`/`on_success`/`when` 条件。MacLaw 已实现。
4. **轮询机制**：goskills 没有 poll 支持。MacLaw 已实现。
5. **安全策略**：goskills 只有简单的危险命令黑名单。MacLaw 有完整的 risk assessor + 安全工具白名单。
6. **错误分类**：goskills 的错误直接透传给 LLM。MacLaw 有详细的错误分类和友好提示。
7. **使用统计**：goskills 没有 usage tracking。MacLaw 已实现。
8. **Operation 路由**：goskills 没有 api_workflow 模式。MacLaw 已实现。

## 四、具体改进建议汇总

### P1（高优先级）

| # | 改进项 | 涉及文件 | 工作量 |
|---|--------|----------|--------|
| 1 | Claude SKILL.md frontmatter 格式兼容 | `corelib/skill/scanner.go` | 中 |
| 2 | MCP 工具调用重连重试 | MCP 调用层 | 小 |
| 3 | LLM Tool Arguments 清洗 | `corelib/tool/craft.go` 或工具入口 | 小 |

### P2（中优先级）

| # | 改进项 | 涉及文件 | 工作量 |
|---|--------|----------|--------|
| 4 | Skill 模糊匹配 fallback | `tui/agent_tools.go`, `gui/skill_runner.go` | 小 |
| 5 | GitHub 导入路径自动替换 | `corelib/skill/github_search.go` | 小 |
| 6 | scripts/ 目录自动发现 | `corelib/skill/scanner.go` | 中 |

### P3（低优先级）

| # | 改进项 | 涉及文件 | 工作量 |
|---|--------|----------|--------|
| 7 | OpenAI skill.md 格式兼容 | `corelib/skill/scanner.go` | 小 |
| 8 | Skill 执行的 Loop 模式（交互式多轮） | `tui/agent_tools.go` | 中 |

## 五、P1-1 Claude SKILL.md 格式兼容的实现草案

```go
// corelib/skill/scanner.go 中新增

// parseClaudeSKILLMD 解析 Claude 原生 SKILL.md 格式（带 YAML frontmatter）
func parseClaudeSKILLMD(skillDir string, data []byte) (*corelib.NLSkillEntry, error) {
    marker := []byte("---")
    parts := bytes.SplitN(data, marker, 3)
    if len(parts) < 3 {
        return nil, fmt.Errorf("no YAML frontmatter found")
    }
    
    var meta struct {
        Name         string `yaml:"name"`
        Description  string `yaml:"description"`
        AllowedTools []string `yaml:"allowed-tools"`
        Model        string `yaml:"model"`
        Version      string `yaml:"version"`
        Tools        []struct {
            Name        string            `yaml:"name"`
            Script      string            `yaml:"script"`
            Description string            `yaml:"description"`
            Parameters  map[string]struct {
                Type        string `yaml:"type"`
                Description string `yaml:"description"`
                Required    bool   `yaml:"required"`
            } `yaml:"parameters"`
        } `yaml:"tools"`
    }
    if err := yaml.Unmarshal(parts[1], &meta); err != nil {
        return nil, err
    }
    
    body := strings.TrimSpace(string(parts[2]))
    
    // 将 tools 定义转换为 NLSkillStep
    var steps []corelib.NLSkillStep
    for _, t := range meta.Tools {
        scriptPath := t.Script
        if scriptPath == "" {
            // 尝试从 scripts/ 目录推断
            scriptPath = inferScriptPath(skillDir, t.Name)
        }
        if scriptPath != "" {
            cmd := buildScriptCommand(scriptPath)
            steps = append(steps, corelib.NLSkillStep{
                Action: "bash",
                Name:   t.Name,
                Params: map[string]interface{}{
                    "command": cmd,
                },
            })
        }
    }
    
    // 如果没有显式 tools，扫描 scripts/ 目录
    if len(steps) == 0 {
        steps = autoDiscoverScripts(skillDir)
    }
    
    return &corelib.NLSkillEntry{
        Name:        meta.Name,
        Description: meta.Description,
        Steps:       steps,
        Status:      "active",
        Source:      "file",
        SkillDir:    skillDir,
    }, nil
}
```

在 `loadSkillFromDir` 中，当 `skill.yaml` 不存在时，检查是否存在带 frontmatter 的 `SKILL.md`：

```go
// 在 loadSkillFromDir 的 markdown fallback 路径中
mdPath, mdErr := skillMarkdownPath(skillDir)
if mdErr == nil {
    data, _ := os.ReadFile(mdPath)
    if bytes.HasPrefix(bytes.TrimSpace(data), []byte("---")) {
        // Claude SKILL.md with frontmatter
        entry, err := parseClaudeSKILLMD(skillDir, data)
        if err == nil {
            return entry, mdPath, nil
        }
    }
    // 继续现有的 markdown 解析逻辑...
}
```

## 六、P1-3 Tool Arguments 清洗的实现草案

```go
// corelib/tool/sanitize.go

// CleanToolArguments 清洗 LLM 返回的工具参数 JSON，处理常见的畸形格式。
func CleanToolArguments(args string) string {
    args = strings.TrimSpace(args)
    
    // 去除代码围栏
    for _, fence := range []string{"```json", "```JSON", "```"} {
        if strings.HasPrefix(args, fence) {
            args = strings.TrimPrefix(args, fence)
            args = strings.TrimLeft(args, "\n\r\t ")
        }
        if strings.HasSuffix(args, fence) {
            args = strings.TrimSuffix(args, fence)
            args = strings.TrimRight(args, "\n\r\t ")
        }
    }
    
    // 去除单引号包裹
    if len(args) >= 2 && args[0] == '\'' && args[len(args)-1] == '\'' {
        args = args[1 : len(args)-1]
    }
    
    // 修复过度转义
    if strings.HasPrefix(args, `{\"`) || strings.HasPrefix(args, `[\"`) {
        args = strings.ReplaceAll(args, `\"`, `"`)
    }
    
    return strings.TrimSpace(args)
}
```
