# Skill Runner 机制性修复：统一 Skill 文档注入

## 问题本质

Skill 有两层信息——执行层（skill.yaml steps）和语义层（SKILL.md）。Runner 只读执行层盲执行，LLM 只看到一行 description。SKILL.md 是信息黑洞——写了但没人读。

drawio-skill 的 SKILL.md 写了"先生成 XML 再调用 run.js 转换"，但 LLM 看不到这个文档，直接调用 run → 失败。

## 根因

已有的 knowledge skill 注入机制（`appendKnowledgeSkillSection`）只处理 `type: "knowledge"` 的 skill——这类 skill 没有执行步骤，只有文档。

但大量 executable skill 也有 SKILL.md（使用手册），描述了完整工作流程和前置条件。这些文档从来不注入到 LLM context。

**断裂点**：knowledge skill 的注入机制人为限制了 `type == "knowledge"` 过滤条件，把 executable skill 的文档排除在外。

## 修复

扩展 `appendKnowledgeSkillSection`，从"只注入 knowledge 类型"扩展到"注入所有有文档的 skill"。同一个 trigger 匹配 + token budget 机制，零新概念。

### 两类 skill 统一注入

| 类别 | 内容来源 | 之前 | 之后 |
|------|---------|------|------|
| knowledge skill | `s.Content`（inline） | ✅ 注入 | ✅ 注入（不变） |
| executable skill + SKILL.md | `loadSkillDocContent(s.SkillDir)` | ❌ 不注入 | ✅ 注入 |
| executable skill 无 SKILL.md | 无 | ❌ 不注入 | ❌ 不注入（不变） |

### 匹配规则

1. Trigger 匹配（已有机制）：skill 的 triggers 与用户消息做关键词匹配
2. 名称匹配（新增）：用户消息包含 skill 名称时也匹配（覆盖"用 drawio-skill 画..."模式）

### 修改文件

| 文件 | 修改 |
|------|------|
| `gui/im_system_prompt.go` | `appendKnowledgeSkillSection` 扩展为同时处理 executable skill 的 SKILL.md；新增 `loadSkillDocForInjection`；skill 列表提示 LLM 阅读下方文档 |
| `gui/app_nl_skills.go` | `NLSkillDefinition` 新增 `Mode`/`HasDocumentation` 字段；`List()` 填充 |
| `gui/skill_doc_inject_test.go` | 6 个测试覆盖所有分支 |

### 验收标准

- 用户说"用 drawio skill 画北京5环图" → trigger "drawio" 匹配 → SKILL.md 注入 system prompt → LLM 读到"先生成 XML 再 run" → LLM 用 write_file 生成 XML → 调用 run → 成功
- knowledge skill 行为不变
- 无 SKILL.md 的 skill 不注入（零开销）
- disabled skill 不注入
- 不匹配的 skill 不注入
- token budget 机制不变

---

## 问题 2：SKILL.md Frontmatter 元数据完整性

### 问题描述

SKILL.md 中使用了 `{{city}}` 双花括号语法和 `{baseDir}` 占位符，但系统上传时检测到的绝对路径来自 Hub 注册时的 skill.yaml 配置（存储在 config.json 中，不在文件系统上）。用户需要在本地创建 skill.yaml 来覆盖它。

### 根因

两条解析路径（skill.yaml vs SKILL.md）的元数据覆盖范围不对等：

- **skill.yaml** 可以声明完整的 skill 元数据（triggers、platforms、mode、operations、requires 等）
- **SKILL.md frontmatter** 只支持 7 个标量字段（name、description、required_args、requires_env、shell、exec_mode、timeout）

`ParseMarkdownFrontmatter` 是简单的 `key: value` 行解析，不支持列表值、嵌套结构、布尔值。

### 修复：SKILL.md frontmatter 升级为 YAML 解析

#### `corelib/skill/skill_markdown.go`

1. `frontmatterKeyAliases` map：定义 `requires_env → required_env` 等别名映射。新增别名只需改这一个 map，所有消费方自动生效
2. `extractFrontmatterBlock()`：提取 frontmatter 原始文本块，`ParseMarkdownFrontmatter` 和 `ParseMarkdownFrontmatterYAML` 共用
3. `ParseMarkdownFrontmatterYAML()`：YAML 解析后立即应用 `frontmatterKeyAliases` 规范化别名键。下游代码只看到规范键名
4. `ParseMarkdownFrontmatter()`：简单解析器同样应用别名规范化（向后兼容的外部调用方也受益）
5. `skillFrontmatterMetadata` 结构体 + `extractSkillMetadata(yamlFM)`：**单一提取点**，从 YAML map 提取所有类型化元数据。消除 `parseSkillMarkdownDocument` 和 `buildCraftToolFallback` 之间的字段提取代码重复
6. `parseSkillMarkdownDocument()`：重写为调用 `extractSkillMetadata()`，不再手动逐字段提取
7. `ParseMarkdownSkill()`（craft_tool 路径）：补全 triggers/platforms/mode/producesArtifact/requiresPython/requiresNode 传递
8. 辅助函数：`yamlStringList()`、`yamlString()`、`yamlBool()`、`yamlInt()`
9. `parsedSkillMarkdown` 结构体新增 7 个字段

#### `corelib/skill/scanner.go`

10. `buildCraftToolFallback()`：重写为调用 `extractSkillMetadata()`，消除与 `parseSkillMarkdownDocument` 的代码重复
11. `loadSkillFromDir()` 的 skill.yaml 优先覆盖逻辑新增 `Mode`、`ExecMode`、`GlobalTimeout` 字段

#### `corelib/skill/skill_markdown_yaml_frontmatter_test.go`（新文件）

18 个新增测试：
- YAML 解析：列表值、布尔值、嵌套结构、内联列表语法、向后兼容、无 frontmatter
- 别名规范化机制：`requires_env` → `required_env` 在 YAML 和简单解析器中均生效；规范键不被别名覆盖
- `extractSkillMetadata`：全字段提取、nil map 安全
- `parseSkillMarkdownDocument`：YAML triggers/platforms/mode/requires、required_args 列表语法
- `ImportMarkdownSkillDir`：YAML frontmatter triggers+platforms、opts 覆盖 frontmatter、mode+produces_artifact+requires_gui
- `buildCraftToolFallback`：YAML frontmatter、无 triggers 默认用 name

### 机制性设计

**问题本质**：两条解析路径（简单 `key: value` vs YAML）× 多个消费方（`parseSkillMarkdownDocument`、`buildCraftToolFallback`、未来新增的消费方）= 每个消费方都需要手动处理别名和类型转换。这是 O(parsers × consumers × aliases) 的维护成本。

**修复原则**：
1. **别名在解析边界规范化**（`frontmatterKeyAliases`）——下游代码永远只看到规范键名。新增别名改一个 map，不改任何消费方
2. **字段提取集中到一个函数**（`extractSkillMetadata`）——新增字段改一个函数，不改任何消费方
3. **YAML 解析器是唯一的类型化数据源**——简单解析器只为外部向后兼容调用方保留，内部代码不依赖它做字段提取

### SKILL.md 示例（升级后）

```markdown
---
name: weather-query
description: 查询指定城市的天气信息
triggers:
  - 天气
  - weather
  - 查天气
required_args: city
requires_env: WEATHER_API_KEY
shell: bash
platforms:
  - windows
  - macos
  - linux
requires:
  python:
    - requests
---

# Weather Query

查询指定城市的实时天气信息。

## 使用方法

```bash
python3 "{baseDir}/scripts/get_weather.py" "{{city}}"
```
```

### 优先级规则

1. `MarkdownSkillOptions`（调用方传入，通常来自 skill.yaml）> SKILL.md frontmatter
2. SKILL.md YAML frontmatter > SKILL.md 简单 frontmatter（向后兼容）
3. 无 frontmatter 时使用默认值

### 验收标准

- ✅ 只有 SKILL.md（无 skill.yaml）的 skill 可以声明 triggers、platforms、mode、requires 等完整元数据
- ✅ 现有的简单 frontmatter（`key: value`）继续正常工作
- ✅ skill.yaml 存在时，其字段覆盖 SKILL.md frontmatter 的同名字段
- ✅ Hub 安装的 skill 在本地有 SKILL.md 时，SKILL.md 的元数据生效
- ✅ `requires_env` 别名在解析边界规范化为 `required_env`，下游代码无需双键检查
- ✅ 所有 50 个 corelib/skill 测试通过（32 个现有 + 18 个新增）
- ✅ GUI 和 TUI 编译通过


---

## 问题 3：活跃工作流期间 Coding Tool Gate 误剥离 bash/write_file

### 问题描述

用户在活跃编码工作流期间发送无关请求（如"将 weather-query skill 上传 market"），LLM 找不到 `bash` 和 `write_file` 工具，被迫使用 `office(write_excel)` 写 YAML 文件（失败），然后尝试 `discover_tool`、`ssh localhost`、`manage_skill(run)` 等各种 workaround，最终在 33 次迭代后被 NeedsConfirm gate 强制返回。

### 根因

两层工具过滤叠加，且 `SkipNeedsConfirmGate` 旁路只覆盖了一层：

1. **Workflow Tool Filter**（`applyWorkflowToolFilter`）：按 `DocOnlyAllowedTools` 白名单过滤。`bash` 和 `write_file` 在白名单中 ✅。`SkipNeedsConfirmGate=true` 时跳过 ✅（#42 已修复）。

2. **Coding Tool Gate**（`gateConfig.active` 处的定义过滤）：按 `codingToolBlocklist` 黑名单过滤。`bash` 和 `write_file` 在黑名单中 ❌。`SkipNeedsConfirmGate` 不检查 ❌。

当 `handlePendingConfirm` 将消息分类为 "other" 并设置 `SkipNeedsConfirmGate=true` 时，Workflow Tool Filter 被跳过（工具保留），但 Coding Tool Gate 仍然执行（`bash`/`write_file` 被剥离）。LLM 收到的工具列表没有 `bash` 和 `write_file`。

### 附带问题：`discover_tool` 排除核心工具

LLM 在找不到 `bash` 后调用 `discover_tool(need="bash 执行本地命令")`，但 `discover_tool` 的 BM25 搜索显式跳过 `CoreToolNames`（包括 `bash`），返回了 browser 工具而非 `bash`。LLM 更加困惑。

### 修复

#### 1. Coding Tool Gate 感知 `SkipNeedsConfirmGate`（`gui/im_message_handler.go`）

三处 `gateConfig.active` 检查新增 `&& !ctx.SkipNeedsConfirmGate` 条件：
- 工具定义过滤（初始）
- 工具定义过滤（recover 后重新过滤）
- 工具调用拦截（`applyCodingToolGate`）

当 `handlePendingConfirm` 判定消息与工作流无关时，coding gate 不剥离工具定义，也不拦截工具调用。

**修改前**：
```go
if gateConfig.active {
    // filter codingToolBlocklist from tool definitions / tool calls
}
```

**修改后**：
```go
if gateConfig.active && !ctx.SkipNeedsConfirmGate {
    // filter codingToolBlocklist from tool definitions / tool calls
}
```

注意：`gateConfig.active` 在 agent loop 中还有其他用途（SteeringWorkflowDetector 激活、suggest_maximize 发射、ask_user 转换等），这些是 UI/UX 行为，不影响工具可用性，不需要旁路。NeedsConfirm gate 的两个分支（no-tool 和 tool）已有独立的 `SkipNeedsConfirmGate` 检查。

#### 2. `discover_tool` 包含核心工具（`gui/tool_discover.go`）

移除 `if tool.CoreToolNames[t.Name] { continue }` 跳过逻辑。核心工具参与 BM25 搜索，结果中标记为 "(core, already available)"。当 LLM 搜索 "bash" 时，`bash` 出现在结果中而非被跳过。

### 机制性分析

这不是 workaround——`SkipNeedsConfirmGate` 是 `handlePendingConfirm` 的语义判定结果（"消息与工作流无关"），它应该同时旁路所有工作流相关的工具过滤机制。之前只旁路了 Workflow Tool Filter，遗漏了 Coding Tool Gate。修复后两层过滤统一受 `SkipNeedsConfirmGate` 控制。

### 验收标准

- 活跃编码工作流期间发送"上传 skill 到 market" → `bash` 和 `write_file` 在 LLM 工具列表中，且 LLM 调用它们时不被拦截
- `discover_tool(need="bash")` → 返回 `bash (core, already available)` 而非 browser 工具
- 编码工作流三阶段（需求→设计→任务分解）中，coding gate 正常剥离编码工具（`SkipNeedsConfirmGate=false`）
- 所有 20 个 CodingGate 测试 + 6 个 SkillDocInjection 测试 + 3 个 RouteTools 测试通过
