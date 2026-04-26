# Skill Runner 机制性修复

## 问题 1：Executable Skill 的 SKILL.md 文档不注入 LLM context

### 问题描述

Skill 有两层信息——执行层（skill.yaml steps）和语义层（SKILL.md）。Runner 只读执行层盲执行，LLM 只看到一行 description。SKILL.md 是信息黑洞——写了但没人读。

drawio-skill 的 SKILL.md 写了"先生成 XML 再调用 run.js 转换"，但 LLM 看不到这个文档，直接调用 run → 失败。

### 根因

已有的 knowledge skill 注入机制（`appendKnowledgeSkillSection`）只处理 `type: "knowledge"` 的 skill——这类 skill 没有执行步骤，只有文档。

但大量 executable skill 也有 SKILL.md（使用手册），描述了完整工作流程和前置条件。这些文档从来不注入到 LLM context。

**断裂点**：注入机制人为限制了 `type == "knowledge"` 过滤条件，把 executable skill 的文档排除在外。

### 修复

代码审查发现 `appendKnowledgeSkillSection` 已经包含 Category 2 分支（`s.Type != "knowledge" && s.SkillDir != ""` → `loadSkillDocContent`），以及名称匹配逻辑。**此问题已在之前的迭代中修复**。

剩余工作：
- `gui/app_nl_skills.go`：`NLSkillDefinition` 新增 `Mode`/`HasDocumentation` 字段，`List()` 填充，供前端 skill 列表展示
- `gui/skill_doc_inject_test.go`：6 个测试覆盖所有分支（knowledge / executable+SKILL.md / executable无SKILL.md / disabled / 不匹配 / token budget）

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
| `gui/im_system_prompt.go` | ✅ 已实现：`appendKnowledgeSkillSection` 同时处理 executable skill 的 SKILL.md + 名称匹配 |
| `gui/app_nl_skills.go` | ✅ 已实现：`NLSkillDefinition` 新增 `HasDocumentation` 字段；`List()` 填充 |
| `gui/skill_doc_inject_test.go` | ✅ 已实现：6 个测试覆盖所有分支 |

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

SKILL.md 的 frontmatter 解析器（`ParseMarkdownFrontmatter`）是简单的 `key: value` 行解析，只支持 7 个标量字段。而 skill.yaml 可以声明完整的元数据（triggers 列表、platforms 列表、requires 嵌套结构等）。只有 SKILL.md（无 skill.yaml）的 skill 无法声明这些字段，导致 trigger 匹配、平台过滤等机制对它们失效。

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


---

## 问题 4：Skill Runner 参数传递——模板替换与结构化参数之间的抽象层缺失

### 问题本质

Skill Runner 的参数传递是一个**字符串模板替换引擎**（`substituteSkillVariables`），但它被用来解决一个**结构化参数绑定**问题。这两者之间缺少一个抽象层——参数契约（Parameter Contract）。

当前的数据流：

```
LLM 调用 manage_skill(action=run, args={input:"xx.drawio", format:"png", output:"xx.png"})
  → normalizeSkillRunVars: args 展开为 vars = {input:"xx.drawio", format:"png", output:"xx.png"}
  → resolveSkillStep → substituteSkillVariables(command, vars)
  → command 模板: "node gen.js --description {content} --name diagram"
  → {content} 不在 vars 中 → stripUnresolvedSkillPlaceholders 静默剥离
  → 最终命令: "node gen.js --description  --name diagram"
  → input/format/output 在 vars 中存在但 command 模板无对应占位符 → 静默丢弃
```

**五个断裂点叠加**：

| 层级 | 断裂点 | 严重度 | 机制性根因 |
|------|--------|--------|-----------|
| 1. 参数契约缺失 | LLM 不知道 skill 期望什么参数名，skill 不知道 LLM 会传什么 | 🔴 P0 | 无参数 schema 声明 |
| 2. 静默丢弃 | 未被模板引用的 args 被无声丢弃，无警告无错误 | 🔴 P0 | `stripUnresolvedSkillPlaceholders` 无条件剥离 |
| 3. 单模板困境 | 一个 command 字符串无法表达多种执行模式 | 🟡 P1 | command 是标量而非按模式分发 |
| 4. 缓存不一致 | config.json 中的 skill 定义可能与磁盘不同步 | 🟡 P1 | identity key 用了可变的 Name 而非稳定的目录路径 |
| 5. 错误静默 | 空参数产生畸形命令，skill 脚本收到空字符串后产出垃圾结果 | 🟠 P2 | 无执行前参数完整性校验 |

### 根因分析

#### 根因 1（核心）：参数契约缺失——LLM 和 Skill 之间没有共享的参数 schema

`manage_skill` 工具定义告诉 LLM：

> `args`: Skill 运行参数（run 时按需传入）。Skill 命令中的 `{{key}}` 占位符会被替换为 args 中对应的值。

LLM 读到这段描述后，按语义构造 `args={input: "xx.drawio", format: "png", output: "xx.png"}`——这是合理的推断。

但 skill.yaml 的 command 模板使用的是 `{content}` 占位符——一个 LLM 完全不知道的名字。LLM 没有任何途径知道这个 skill 期望 `content` 而非 `input`。

**这不是 LLM 的错，也不是 skill 作者的错**。是系统缺少一个让双方对齐的机制——参数契约。

当前 skill.yaml 有 `required_args` 字段，但它只用于执行前校验（"缺少参数 xxx"），不参与 LLM 的工具定义生成。LLM 看不到 `required_args`，也看不到 command 模板中的占位符名称。

#### 根因 2：`substituteSkillVariables` 在错误的抽象层操作

`substituteSkillVariables` 是一个**字符串模板引擎**——它在 command 字符串中查找 `{key}`/`{{key}}`/`${key}` 模式并替换。这个设计有两个结构性问题：

1. **只替换模板中存在的占位符**：`vars` 中的 `input`/`output`/`format` 在 command 模板中没有对应占位符，被完全忽略。没有任何代码路径将这些值传递给 skill 脚本。

2. **未匹配的占位符被静默剥离**：`stripUnresolvedSkillPlaceholders` 将 `{content}` 替换为空字符串，产生 `--description  --name diagram`（`--description` 后跟空字符串）。skill 脚本收到畸形参数，产出垃圾结果（1 node, 0 edges 的空图），但不报错。LLM 看到"成功"的返回，以为任务完成了。

根因相同：`substituteSkillVariables` 不区分"模板中声明了但未提供值的参数"和"调用方提供了但模板未声明的参数"。两种情况都被静默处理。

#### 根因 3：单 command 模板无法表达多执行模式

drawio-skill 的 generate.js 支持三种模式（`--description` / `--input` / `--xml`），但 skill.yaml 只有一个 `command` 字段写死了 `--description {content}`。这不是 drawio-skill 特有的问题——任何支持多种操作模式的 skill 都会遇到。

#### 根因 4：`loadSkills` 用可变的 Name 做 identity key

`loadSkills` 的 `known` map 按 `Name` 索引。但 `Name` 是显示字段，可以被 SKILL.md frontmatter 覆盖、被用户修改、被 Hub 安装时加 publisher 前缀。当 config 中的 Name 与磁盘 YAML 的 Name 不一致时，`shouldHydrateSkillFromFile` 匹配失败，磁盘修改不生效。

### 修复方案：参数契约层——单一代码路径，零 workaround

#### 核心设计原则

**消除双路径**。不做"有 schema 走路径 A，无 schema 走路径 B"的分支。而是：**无显式 schema 时，从 command 模板自动合成 schema**。所有 skill 都经过同一条 `BindParams` 路径。

```
skill.yaml 有 params 字段  →  使用显式 schema
skill.yaml 无 params 字段  →  从 command 模板中提取占位符，合成隐式 schema
                               ↓
                          统一进入 BindParams
```

这消除了：
- `content` ← `input` fallback（workaround：猜测映射关系）
- `noAutoAppendKeys` 硬编码排除列表（workaround：手动维护哪些键不追加）
- Phase A / Phase B 双路径（每个 fix 要改两处）
- `autoAppendUnconsumedArgs` 仅限 bash 步骤（workaround：其他步骤类型被遗漏）

#### 修复 1：Skill 参数 Schema 声明（`corelib/types.go` + `corelib/skill/scanner.go`）

skill.yaml 新增可选的 `params` 字段：

```yaml
name: drawio-skill
description: 生成 drawio 图表
params:
  - name: description
    description: "图表的自然语言描述"
    aliases: [content, input, text]
    cli_flag: "--description"
  - name: input
    description: "已有的 .drawio 文件路径"
    cli_flag: "--input"
  - name: format
    description: "输出格式: png/svg/pdf"
    cli_flag: "--format"
    default: "png"
  - name: output
    description: "输出文件路径"
    cli_flag: "--output"
steps:
  - action: bash
    params:
      command: "node {baseDir}/generate.js"
```

**数据结构**：

```go
// corelib/skill/scanner.go
type SkillYAMLParam struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description,omitempty"`
    Aliases     []string `yaml:"aliases,omitempty"`
    CLIFlag     string   `yaml:"cli_flag,omitempty"`
    Default     string   `yaml:"default,omitempty"`
    Required    bool     `yaml:"required,omitempty"`
}

// SkillYAMLFile 新增：
Params []SkillYAMLParam `yaml:"params,omitempty"`
// ParseSkillYAMLFile 的 knownKeys 新增 "params": true

// corelib/types.go
type NLSkillParam struct {
    Name        string   `json:"name"`
    Description string   `json:"description,omitempty"`
    Aliases     []string `json:"aliases,omitempty"`
    CLIFlag     string   `json:"cli_flag,omitempty"`
    Default     string   `json:"default,omitempty"`
    Required    bool     `json:"required,omitempty"`
    Synthetic   bool     `json:"synthetic,omitempty"` // true = 从模板自动合成
}

// NLSkillEntry 新增：
Params []NLSkillParam `json:"params,omitempty"`
```

注意 `Synthetic` 字段——标记参数是显式声明的还是自动合成的。LLM context 注入时，合成参数标注"(从模板推断)"，提示 LLM 这个名字可能不准确。

#### 修复 2：Schema 合成——从 command 模板自动提取参数（`corelib/skill/param_synthesize.go`，新文件）

**这是消除双路径的关键**。当 skill 没有显式 `params` 时，扫描所有 step 的 command 模板，提取占位符名称，合成隐式 schema：

```go
// SynthesizeParams 从 skill 的 steps 中提取所有占位符，合成参数 schema。
// 扫描每个 step 的所有 Params 字符串值（不仅是 command），因为
// resolveSkillValue 会递归替换 Params 中所有字符串的占位符。
// 返回的 params 标记 Synthetic=true，且 CLIFlag 为空（占位符替换模式，非 CLI 追加模式）。
func SynthesizeParams(steps []corelib.NLSkillStep, requiredArgs []string) []corelib.NLSkillParam {
    seen := make(map[string]bool)
    var params []corelib.NLSkillParam

    requiredSet := make(map[string]bool, len(requiredArgs))
    for _, a := range requiredArgs { requiredSet[a] = true }

    for _, step := range steps {
        // 递归扫描 Params 中所有字符串值的占位符
        extractPlaceholdersFromValue(step.Params, func(key string) {
            if key == "" || key == "baseDir" || key == "base_dir" || seen[key] {
                return
            }
            seen[key] = true
            params = append(params, corelib.NLSkillParam{
                Name:      key,
                Required:  requiredSet[key],
                Synthetic: true,
            })
        })
    }
    return params
}

// extractPlaceholdersFromValue 递归扫描 value 中所有字符串的占位符，
// 对每个找到的 key 调用 callback。与 resolveSkillValue 的递归结构对齐。
func extractPlaceholdersFromValue(value interface{}, callback func(key string)) {
    switch typed := value.(type) {
    case string:
        for _, m := range placeholderPattern.FindAllString(typed, -1) {
            callback(extractPlaceholderKey(m))
        }
    case map[string]interface{}:
        for _, item := range typed {
            extractPlaceholdersFromValue(item, callback)
        }
    case []interface{}:
        for _, item := range typed {
            extractPlaceholdersFromValue(item, callback)
        }
    }
}
```

**效果**：drawio-skill 的 `command: "node gen.js --description {content} --name diagram"` 自动合成 `params: [{name: "content", synthetic: true}]`。LLM 在 system prompt 中看到 `content (从模板推断)`，知道该传 `args={content: "北京5环图"}`。

#### 修复 3：参数绑定引擎——单一路径（`corelib/skill/param_bind.go`，新文件）

所有 skill（有显式 schema 或合成 schema）都经过同一个 `BindParams`：

```go
// BindParams 将 LLM 传入的 vars 通过 param schema 绑定。
// 返回：
//   resolvedVars — 别名已解析的变量 map（供模板替换使用）
//   cliArgs      — 需要追加到命令末尾的 CLI 参数（仅显式 schema 的 cli_flag 参数）
//   errors       — 必需参数缺失等硬错误（应阻止执行）
//   warnings     — 未声明参数等软警告（记录日志但不阻止）
func BindParams(params []NLSkillParam, vars map[string]string) (
    resolvedVars map[string]string, cliArgs []string,
    errors []string, warnings []string,
) {
    resolvedVars = make(map[string]string)
    consumed := make(map[string]bool)

    // Phase 1: 别名解析
    for _, p := range params {
        // 标记此 param 的所有名字（规范名 + 别名）为已消费
        allNames := append([]string{p.Name}, p.Aliases...)

        var matched string
        for _, name := range allNames {
            if v, ok := vars[name]; ok && v != "" {
                matched = v
                break
            }
        }
        if matched != "" {
            resolvedVars[p.Name] = matched
            for _, n := range allNames { consumed[n] = true }
        } else if p.Default != "" {
            resolvedVars[p.Name] = p.Default
        }
    }

    // Phase 2: CLI 参数构建（仅显式 schema 的 cli_flag 参数）
    // 合成参数（Synthetic=true）没有 CLIFlag，走模板占位符替换，不走 CLI 追加。
    // 注意：如果参数同时有 cli_flag 和模板占位符引用，resolveSkillStep 会在
    // 追加前过滤掉已被模板消费的 cliArgs，避免双重应用。
    for _, p := range params {
        v := resolvedVars[p.Name]
        if v == "" || p.CLIFlag == "" {
            continue
        }
        cliArgs = append(cliArgs, p.CLIFlag, QuoteForShell(v))
    }

    // Phase 3: 未消费参数处理
    for key, value := range vars {
        if consumed[key] || value == "" {
            continue
        }
        // 未被任何 param（含别名）消费的 vars 键
        warnings = append(warnings, fmt.Sprintf(
            "参数 %q 未被 skill 声明（已声明的参数: %s）",
            key, formatParamNames(params)))
    }

    // Phase 4: 必需参数校验
    for _, p := range params {
        if p.Required && resolvedVars[p.Name] == "" {
            errors = append(errors, fmt.Sprintf("必需参数 %q 未提供", p.Name))
        }
    }
    return
}
```

**关键区别**：`errors` 和 `warnings` 分离。`errors` 阻止执行（必需参数缺失），`warnings` 只记录日志（未声明参数）。之前的设计把两者混在一个 `warnings` 列表里，调用方无法区分。

#### 修复 4：`resolveSkillStep` 统一路径（`gui/skill_runner.go`）

**消除 Phase A / Phase B 分支**。所有 skill 走同一条路径：

```go
func resolveSkillStep(step corelib.NLSkillStep, vars map[string]string,
    skillDir string, params []corelib.NLSkillParam) (corelib.NLSkillStep, error) {

    // 统一路径：BindParams 处理别名解析 + 必需参数校验
    resolvedVars, cliArgs, errs, warns := skill.BindParams(params, vars)
    for _, w := range warns { log.Printf("[skill-runner] warning: %s", w) }
    if len(errs) > 0 {
        return step, fmt.Errorf("参数绑定失败: %s", strings.Join(errs, "; "))
    }

    // 模板替换（Layer 1，不变）——用 resolvedVars 替换 command 中的占位符
    resolved := step
    if p, ok := resolveSkillValue(step.Params, resolvedVars).(map[string]interface{}); ok {
        resolved.Params = p
    }

    // CLI 参数追加——过滤掉已被模板占位符消费的参数，避免双重应用。
    // 例如 params 声明 cli_flag="--format"，但 command 模板也有 {format} 占位符，
    // 模板替换已经把值填入了命令，不需要再追加 --format。
    if cmd, ok := resolved.Params["command"].(string); ok && len(cliArgs) > 0 {
        originalCmd, _ := step.Params["command"].(string)
        filtered := filterConsumedCLIArgs(cliArgs, params, originalCmd)
        if len(filtered) > 0 {
            resolved.Params["command"] = cmd + " " + strings.Join(filtered, " ")
        }
    }

    // ... 其余逻辑（craft_tool input/output 注入、working_dir 解析等）不变 ...
    return resolved, nil
}

// filterConsumedCLIArgs 从 cliArgs 中移除已被模板占位符消费的参数。
// cliArgs 是 [flag, value, flag, value, ...] 交替排列。
// 如果某个 param 的 Name 在 originalCmd 中有占位符引用（{name}/{{name}}/${name}），
// 说明模板替换已经处理了这个参数，CLI 追加会导致双重应用。
func filterConsumedCLIArgs(cliArgs []string, params []NLSkillParam, originalCmd string) []string {
    consumedFlags := make(map[string]bool)
    for _, p := range params {
        if p.CLIFlag != "" && commandReferencesKey(originalCmd, p.Name) {
            consumedFlags[p.CLIFlag] = true
        }
    }
    if len(consumedFlags) == 0 { return cliArgs }
    var filtered []string
    for i := 0; i < len(cliArgs); i += 2 {
        if i+1 < len(cliArgs) && !consumedFlags[cliArgs[i]] {
            filtered = append(filtered, cliArgs[i], cliArgs[i+1])
        }
    }
    return filtered
}
```

**为什么不需要 Phase B**：
- 旧 skill 无 `params` → `SynthesizeParams` 从 command 模板提取 `{content}` → 合成 `params=[{name:"content", synthetic:true}]`
- LLM 传 `args={content:"北京5环"}` → `BindParams` Phase 1 匹配 `content` → `resolvedVars={content:"北京5环"}`
- `resolveSkillValue` 替换 `{content}` → 命令正确
- LLM 传 `args={input:"xx.drawio"}` → `BindParams` Phase 1 不匹配 `content`（无别名）→ Phase 3 警告 `input 未被声明`
- 但 `resolvedVars` 中没有 `content` → `{content}` 未被替换 → `stripUnresolvedSkillPlaceholders` 剥离 → 命令畸形 → **但现在 Phase 4 会报错**（如果 `content` 在 `required_args` 中）

**关键**：`SynthesizeParams` 将 `required_args` 中的参数标记为 `Required=true`。当 LLM 传了错误的参数名导致必需占位符未被填充时，`BindParams` Phase 4 返回 error，`resolveSkillStep` 返回 error，`executeAsync` 将错误信息返回给 LLM。LLM 看到"必需参数 content 未提供"，知道该传 `content` 而非 `input`。

**不再需要**：
- ~~`content` ← `input` fallback~~（LLM 从 schema 摘要中看到正确的参数名）
- ~~`noAutoAppendKeys` 硬编码列表~~（合成 schema 的参数没有 `CLIFlag`，不会被追加）
- ~~`autoAppendUnconsumedArgs` 仅限 bash~~（统一路径，所有步骤类型都经过 BindParams）
- ~~Phase A / Phase B 双路径~~（只有一条路径）

#### 修复 5：Schema 注入到 LLM context（`gui/im_system_prompt.go`）

`appendKnowledgeSkillSection` 注入 SKILL.md 文档时，同时注入参数 schema 摘要。显式参数和合成参数区分标注：

```
### Skill: drawio-skill
参数:
  - description (别名: content, input, text): 图表的自然语言描述 [--description]
  - input: 已有的 .drawio 文件路径 [--input]
  - format: 输出格式 (默认: png) [--format]
  - output: 输出文件路径 [--output]
```

无显式 schema 的旧 skill：

```
### Skill: old-skill
参数 (从命令模板推断):
  - content (必需)
```

LLM 看到 `content (必需)` 后知道该传 `args={content: "..."}` 而非猜测。

#### 修复 6：`stripUnresolvedSkillPlaceholders` 改为 fail-loud（`corelib/skill/substitute.go`）

当前 `stripUnresolvedSkillPlaceholders` 无条件静默剥离所有未解析占位符。这是错误静默的根源。

改为：**`BindParams` Phase 4 已经校验了必需参数。到达 `stripUnresolvedSkillPlaceholders` 时，未解析的占位符只可能是非必需的可选参数（`Required=false`）。** 对这些可选占位符，剥离是安全的——skill 脚本应该能处理缺失的可选参数。

但如果 `BindParams` 被绕过（代码 bug），未解析的必需占位符到达这里，应该 panic 而非静默剥离。新增断言：

```go
func stripUnresolvedPlaceholders(text string, params []NLSkillParam) string {
    remaining := placeholderPattern.FindAllString(text, -1)
    if len(remaining) == 0 { return text }

    requiredNames := make(map[string]bool)
    for _, p := range params {
        if p.Required { requiredNames[p.Name] = true }
    }
    for _, m := range remaining {
        key := extractPlaceholderKey(m)
        if requiredNames[key] {
            // 不应该到达这里——BindParams Phase 4 应该已经拦截了
            log.Printf("[skill-runner] BUG: required placeholder {%s} reached strip phase — BindParams was bypassed?", key)
        }
    }
    return placeholderPattern.ReplaceAllString(text, "")
}
```

#### 修复 7：共享层提取到 corelib（GUI + TUI 单一实现）

| 函数 | 目标文件 | 说明 |
|------|---------|------|
| `SynthesizeParams` | `corelib/skill/param_synthesize.go` | 从 command 模板合成 schema |
| `BindParams` | `corelib/skill/param_bind.go` | 参数绑定引擎 |
| `SubstituteVariables` | `corelib/skill/substitute.go` | 模板替换（从 GUI 提取） |
| `QuoteForShell` | `corelib/skill/substitute.go` | Shell 引号（从 GUI 提取） |
| `NormalizeRunVars` | `corelib/skill/run_vars.go` | args 展开（从 GUI 提取） |
| `ResolveStep` + `filterConsumedCLIArgs` | `corelib/skill/resolve.go` | 步骤解析 + CLI 去重（从 GUI 提取） |

TUI 的 `toolRunSkill` 当前完全不做变量替换。提取到 corelib 后，TUI 调用 `skill.ResolveStep` 即可获得完整能力——别名解析、CLI 参数构建、必需参数校验、模板替换，全部复用。

#### 修复 8：缓存一致性——用目录路径做 identity key（`gui/app_nl_skills.go`）

`loadSkills` 的 `known` map 从按 `Name` 索引改为按 **skill 目录的绝对路径** 索引。目录路径是唯一稳定的标识符——不受 Name 修改、Hub publisher 前缀、SKILL.md frontmatter 覆盖的影响。

```go
func (e *SkillExecutor) loadSkills() []corelib.NLSkillEntry {
    // ...
    // 按目录路径索引，而非 Name
    knownByDir := make(map[string]int) // skillDir → index in skills
    knownByName := make(map[string]int) // Name → index（向后兼容）
    for i, s := range skills {
        if s.SkillDir != "" {
            knownByDir[filepath.Clean(s.SkillDir)] = i
        }
        knownByName[s.Name] = i
    }

    for _, fs := range fileSkills {
        cleanDir := filepath.Clean(fs.SkillDir)
        // 优先按目录匹配（稳定），回退到 Name 匹配（兼容）
        idx, found := knownByDir[cleanDir]
        if !found {
            idx, found = knownByName[fs.Name]
        }
        if found {
            configSkill := &skills[idx]
            if len(fs.Steps) > 0 {
                // 磁盘有 steps → 始终覆盖（磁盘是 source of truth）
                configSkill.Steps = fs.Steps
                configSkill.SkillDir = fs.SkillDir
                configSkill.Params = fs.Params // 同步覆盖 params
                // ... 其余字段覆盖逻辑不变 ...
            }
            continue
        }
        skills = append(skills, fs)
    }
    return skills
}
```

`shouldHydrateSkillFromFile` 不再需要——覆盖逻辑内联到 `loadSkills` 中，条件简化为 `len(fs.Steps) > 0`。消除了一个间接函数和它的匹配条件维护负担。

#### 修复 9：`executeAsync` 集成——schema 合成 + 绑定 + 执行

`executeAsync` 在执行步骤前，确保 skill 有 params schema（显式或合成）：

```go
func (r *SkillRunner) executeAsync(ctx context.Context, run *skillRun, skill *corelib.NLSkillEntry) {
    // ...
    // 确保 params schema 存在（显式或合成）
    params := skill.Params
    if len(params) == 0 {
        params = cskill.SynthesizeParams(skill.Steps, skill.RequiredArgs)
        if len(params) > 0 {
            log.Printf("[skill-runner] synthesized %d params from command templates: %v",
                len(params), paramNames(params))
        }
    }

    for i, step := range skill.Steps {
        // ...
        resolvedStep, err := resolveSkillStep(step, r.templateVarsForRun(run.status.RunID), skill.SkillDir, params)
        if err != nil {
            // 参数绑定失败（必需参数缺失等）→ 步骤失败，错误信息返回给 LLM
            run.status.Steps[i].Status = "failed"
            run.status.Steps[i].Error = err.Error()
            // ...
        }
        // ...
    }
}
```

### 修改文件

| 文件 | 修改 |
|------|------|
| `corelib/types.go` | 新增 `NLSkillParam`（含 `Synthetic` 字段）；`NLSkillEntry` 新增 `Params` |
| `corelib/skill/scanner.go` | 新增 `SkillYAMLParam`；`SkillYAMLFile.Params`；`knownKeys` 加 `"params"`；`loadSkillFromDir` 传递 params |
| `corelib/skill/param_synthesize.go` | 新文件：`SynthesizeParams` + `extractPlaceholdersFromValue`（递归扫描所有 Params 字符串值，与 `resolveSkillValue` 的递归结构对齐） |
| `corelib/skill/param_bind.go` | 新文件：`BindParams` 参数绑定引擎（errors/warnings 分离） |
| `corelib/skill/substitute.go` | 新文件：从 GUI 提取 `SubstituteVariables`、`QuoteForShell`；`stripUnresolvedPlaceholders` 加必需参数断言 |
| `corelib/skill/resolve.go` | 新文件：从 GUI 提取 `ResolveStep` + `filterConsumedCLIArgs`（统一路径，防止 cli_flag + 模板占位符双重应用） |
| `corelib/skill/run_vars.go` | `NormalizeRunVars` 提取为共享函数（**删除** `content` ← `input` fallback） |
| `gui/skill_runner.go` | `resolveSkillStep` 委托到 `corelib/skill/resolve.go`；`executeAsync` 集成 `SynthesizeParams`；**删除** `autoAppendUnconsumedArgs`、`noAutoAppendKeys`、`commandReferencesKey` |
| `gui/im_system_prompt.go` | `appendKnowledgeSkillSection` 注入参数 schema 摘要（区分显式/合成） |
| `gui/app_nl_skills.go` | `loadSkills` 按目录路径索引；**删除** `shouldHydrateSkillFromFile`（逻辑内联简化） |
| `tui/agent_tools.go` | `toolRunSkill` 调用 `corelib/skill.ResolveStep`（完整参数绑定能力） |
| `corelib/skill/param_synthesize_test.go` | 新文件：8 个测试 |
| `corelib/skill/param_bind_test.go` | 新文件：12 个测试 |
| `corelib/skill/substitute_test.go` | 新文件：6 个测试 |
| `corelib/skill/resolve_test.go` | 新文件：3 个测试（含 cli_flag + 模板占位符双重应用防护） |

### 测试矩阵

| 场景 | 输入 | skill 配置 | 期望结果 |
|------|------|-----------|---------|
| **显式 schema + 别名** | `args={content:"北京5环"}` | `params:[{name:description, aliases:[content], cli_flag:"--description"}]` | `gen.js --description "北京5环"` |
| **显式 schema + 直接匹配** | `args={input:"xx.drawio"}` | `params:[{name:input, cli_flag:"--input"}]` | `gen.js --input "xx.drawio"` |
| **显式 schema + 默认值** | `args={}` | `params:[{name:format, default:"png", cli_flag:"--format"}]` | `gen.js --format png` |
| **显式 schema + 未声明参数** | `args={foo:"bar"}` | `params:[{name:input}]` | 警告"foo 未被声明" |
| **显式 schema + 必需缺失** | `args={}` | `params:[{name:input, required:true}]` | 错误"必需参数 input 未提供"，步骤不执行 |
| **合成 schema + 正确参数名** | `args={content:"画图"}` | `command:"gen.js --desc {content}"` | `gen.js --desc "画图"` |
| **合成 schema + 错误参数名** | `args={input:"xx.drawio"}` | `command:"gen.js --desc {content}", required_args:[content]` | 错误"必需参数 content 未提供" |
| **合成 schema + 可选占位符** | `args={}` | `command:"gen.js --name {name}"` | `gen.js --name `（可选，静默剥离） |
| **合成 schema + LLM 看到提示** | — | `command:"gen.js {content}"` | system prompt 显示"content (从模板推断, 必需)" |
| **目录路径索引** | 修改 skill.yaml name 后 run | SkillDir 不变 | 磁盘覆盖 config |
| **Hub skill 修改** | Hub 安装后修改磁盘 skill.yaml | source=hub, SkillDir 匹配 | 磁盘覆盖 config |
| **TUI 完整绑定** | `args={city:"北京"}` | `command:"get_weather.py {{city}}"` | `get_weather.py "北京"` |
| **显式 schema + cli_flag + 模板占位符** | `args={format:"svg"}` | `params:[{name:format, cli_flag:"--format"}], command:"gen.js --format {format}"` | `gen.js --format "svg"`（不重复追加 --format） |
| **空 args + 无占位符** | `args={}` | `command:"echo hello"` | `echo hello`（不变） |

### 被删除的 workaround 清单

| 被删除的代码 | 为什么是 workaround | 被什么机制替代 |
|-------------|-------------------|--------------|
| `content` ← `input` fallback（`normalizeSkillRunVars`） | 硬编码猜测：假设 LLM 传 `input` 时 skill 期望 `content`。换个 skill 用 `text` 就失效 | `SynthesizeParams` 提取真实占位符名 → LLM 从 schema 摘要看到正确名字 |
| `noAutoAppendKeys` 硬编码列表 | 手动维护哪些键不追加。新增键要改这个列表 | 合成 schema 的参数没有 `CLIFlag`，不产生 CLI 追加。显式 schema 的参数由 `cli_flag` 控制 |
| `autoAppendUnconsumedArgs` 仅限 bash | 其他步骤类型（craft_tool、ssh）的未消费参数被静默丢弃 | 统一路径：所有步骤类型都经过 `BindParams`，未消费参数产生警告 |
| Phase A / Phase B 双路径 | 每个 fix 要改两处。Phase B 是旧的 broken 路径加补丁 | 单一路径：`SynthesizeParams` 保证所有 skill 都有 schema |
| `shouldHydrateSkillFromFile` Name 匹配 | Name 是可变的显示字段，匹配不稳定 | 目录路径索引：唯一稳定标识符 |

### 机制性分析

**为什么这是机制性修复而非 workaround**：

1. **单一代码路径**：所有 skill（有显式 schema / 无 schema / Hub 安装 / 本地文件）都经过 `SynthesizeParams → BindParams → SubstituteVariables` 同一条路径。未来的 fix 只需改一处。

2. **参数契约是声明式的**：skill 作者声明 `params`（或系统从模板自动合成），LLM 从 schema 摘要中读取。双方通过数据对齐，不通过启发式猜测。

3. **错误在执行前暴露**：必需参数缺失 → `BindParams` 返回 error → `resolveSkillStep` 返回 error → LLM 看到明确的错误信息（"必需参数 content 未提供"）。不再静默产出垃圾。

4. **零硬编码排除列表**：没有 `noAutoAppendKeys`，没有 `content` ← `input` 映射。参数行为完全由 schema 数据驱动。

5. **identity key 是不可变的**：目录路径不受 Name 修改、Hub publisher 前缀、frontmatter 覆盖的影响。

**扩展方式**：
- 新增参数类型（`type: file` 校验文件存在性、`type: bool` 布尔开关）→ 改 `BindParams`
- 新增 CLI 映射方式（`env_var: "INPUT_FILE"` 环境变量传递）→ 改 `BindParams`
- 新增 skill → 在 YAML 中声明 `params`，或不声明（自动合成）

### 验收标准

- 有显式 `params` 的 skill：别名解析 + CLI 参数构建 + 必需参数校验全部工作
- 无 `params` 的旧 skill：自动合成 schema → 占位符名称注入 LLM context → LLM 传正确参数名
- 必需参数缺失时 `resolveSkillStep` 返回 error，步骤不执行，错误信息返回给 LLM
- LLM 在 system prompt 中看到参数 schema 摘要（显式标注 / 合成标注"从模板推断"）
- 修改 skill.yaml 后立即 `run` 使用新定义（目录路径索引）
- TUI `toolRunSkill` 调用 `corelib/skill.ResolveStep` 获得完整参数绑定
- 代码中不存在 `noAutoAppendKeys`、`content` ← `input` fallback、Phase A/B 分支
- 所有 29 个新增测试 + 50 个现有 corelib/skill 测试通过
- GUI 和 TUI 编译通过

---

## 附录：maclaw 建议对照表

| # | maclaw 建议 | 说明 | 覆盖状态 | 对应修复 |
|---|-----------|------|---------|---------|
| ① | 读 skill.yaml | command 从 skill.yaml 读取，忽略 SKILL.md frontmatter | ✅ 已覆盖 | 修复 8：`loadSkills` 按目录路径索引，磁盘 skill.yaml 始终是 source of truth；修复 9：每次 `run` 重新扫描磁盘 |
| ② | 支持 `{args.xxx}` | 从 `manage_skill run` 的 `args` 字典中取值替换 | ✅ 已覆盖（超集） | 修复 1-4：`params` schema 声明 + `BindParams` 别名解析 + CLI 参数构建。比 `{args.xxx}` 更强——支持别名、默认值、必需校验、合成 schema |
| ③ | 注入 env | 把 `env` 字典注入到子进程 `process.env` | ✅ 已实现（代码已有） | 不在本文修复范围。`StartRun` 从 `runArgs["env"]` 提取 → bash 步骤通过 `params["extra_env"]` 注入 `cmd.Env`；非 bash 步骤通过 `os.Setenv` + defer restore 注入。工具定义已有 `env` 参数描述 |
| ④ | 不执行示例 | SKILL.md 中的 bash 代码块不自动执行，只作为文档 | ✅ 已实现（代码已有） | `isResolvedBlockExecutable()` 过滤使用示例（含未解析占位符、中文路径等）；问题 1 的 SKILL.md 注入是注入到 LLM system prompt 作为参考文档，不是执行 |


---

## 问题 5：本地 LLM API 代理难以启用——检测逻辑是 opt-in 而非 default-on

### 问题描述

Skill 运行时如果 `OPENAI_API_KEY` 等环境变量未设置，应该自动启用本地 LLM API 代理（已有实现 `corelib.NewOpenAIProxy`），将代理地址注入到子进程环境变量中。但实际上代理很难被激活——大量需要 LLM API 的 skill 在运行时因缺少 API key 而失败。

### 根因

`NeedsOpenAIProxyAuto` 的检测逻辑是**三层 opt-in 扫描**，每一层都有盲区：

```
Layer 1: required_env 包含 "OPENAI_API_KEY"  → 大多数 skill 不声明
Layer 2: step command 字符串包含 "OPENAI_API_KEY" → 命令行很少直接写环境变量名
Layer 3: 扫描 skillDir 下的脚本文件内容 → 有 5 个盲区（见下）
```

**Layer 3 的 5 个盲区**：

1. **库内部读取**：Python 的 `openai` 库在 `openai.Client()` 构造时内部读取 `OPENAI_API_KEY`，脚本中不出现这个字符串。`containsOpenAIEnvRef` 匹配不到
2. **目录限制**：只扫描 `skillDir`、`scripts/`、`src/`、`lib/`。脚本在 `utils/`、`tools/`、`bin/` 等目录下时漏扫
3. **文件数量限制**：`scanned > 30` 安全阀。大型 skill 超过 30 个脚本文件后停止扫描
4. **文件大小限制**：`info.Size() > 64*1024` 跳过。大文件中的引用被忽略
5. **间接引用**：脚本通过 `.env` 文件、`config.yaml`、`dotenv.load()` 等方式读取环境变量，不在 `.py`/`.js` 文件中直接引用

**根本问题**：检测逻辑试图通过**源码扫描猜测** skill 是否需要 LLM API。这是一个不可判定问题——你无法通过静态扫描确定一个程序是否会在运行时读取某个环境变量。

### 修复方案：反转默认值——从 opt-in 检测改为 opt-out 声明

#### 设计原则

**代理启动的成本很低**（监听一个本地端口），但不启动的代价很高（skill 因缺少 API key 失败）。所以默认值应该是**启动代理**，而非不启动。

当前逻辑：
```
默认不启动 → 检测到需要时启动（三层扫描，有盲区）
```

改为：
```
默认启动 → 检测到不需要时跳过（显式声明 no_llm_api: true）
```

#### 修复 1：反转 `NeedsOpenAIProxyAuto` 的默认值（`corelib/openai_proxy.go`）

```go
func NeedsOpenAIProxyAuto(requiredEnv []string, extraEnv map[string]string,
    steps []NLSkillStep, skillDir string, noLLMAPI bool) bool {

    // 快速路径：用户已通过 extraEnv 提供了凭据
    if hasUserProvidedOpenAIEnv(extraEnv) {
        return false
    }

    // 快速路径：进程环境已有有效的 OPENAI_API_KEY（非 stale sentinel）
    if v := os.Getenv("OPENAI_API_KEY"); v != "" && v != "sk-maclaw-local-proxy" {
        if _, explicitlyCleared := extraEnv["OPENAI_API_KEY"]; !explicitlyCleared {
            return false
        }
    }

    // skill 显式声明不需要 LLM API → 不启动代理
    if noLLMAPI {
        return false
    }

    // 默认：启动代理
    // 代理成本低（本地端口），不启动的代价高（skill 因缺 key 失败）
    return true
}
```

**关键变化**：删除了三层扫描逻辑（`explicitlyRequired` / `containsOpenAIEnvRef` / `scanSkillDirForOpenAIEnv`）。不再猜测 skill 是否需要 API——默认提供，不需要的 skill 自己声明 `no_llm_api: true`。

#### 修复 2：skill.yaml 新增 `no_llm_api` 声明（`corelib/skill/scanner.go`）

纯本地工具类 skill（如 drawio-skill、文件转换、QR 码生成等）不需要 LLM API，可以声明跳过代理：

```yaml
name: drawio-skill
no_llm_api: true   # 纯本地工具，不需要 LLM API 代理
steps:
  - action: bash
    params:
      command: "node {baseDir}/generate.js ..."
```

**`SkillYAMLFile` 新增字段**：
```go
NoLLMAPI bool `yaml:"no_llm_api,omitempty"`
```

**`NLSkillEntry` 新增字段**：
```go
NoLLMAPI bool `json:"no_llm_api,omitempty"`
```

`ParseSkillYAMLFile` 的 `knownKeys` 新增 `"no_llm_api": true`。

#### 修复 3：`executeAsync` 传递 `noLLMAPI` 标志

```go
needsProxy := corelib.NeedsOpenAIProxyAuto(
    skill.RequiredEnv, run.extraEnv,
    skill.Steps, skill.SkillDir,
    skill.NoLLMAPI,  // 新增参数
)
```

#### 修复 4：保留扫描逻辑作为日志诊断（不影响决策）

三层扫描逻辑不删除，但从**决策函数**降级为**诊断日志**。代理启动后，扫描结果写入日志供调试：

```go
// 代理启动后，记录诊断信息
if needsProxy {
    // ... start proxy ...
    // 诊断日志：记录 skill 是否显式声明了 OPENAI_API_KEY 需求
    diagExplicit := slices.Contains(skill.RequiredEnv, "OPENAI_API_KEY")
    diagCommandRef := scanCommandsForOpenAIRef(skill.Steps)
    diagFileRef := scanSkillDirForOpenAIEnv(skill.SkillDir)
    log.Printf("[openai-proxy] diag: explicit=%v commandRef=%v fileRef=%v",
        diagExplicit, diagCommandRef, diagFileRef)
}
```

### 修改文件

| 文件 | 修改 |
|------|------|
| `corelib/openai_proxy.go` | `NeedsOpenAIProxyAuto` 反转默认值：默认启动，`noLLMAPI=true` 时跳过；三层扫描降级为诊断日志 |
| `corelib/types.go` | `NLSkillEntry` 新增 `NoLLMAPI bool` |
| `corelib/skill/scanner.go` | `SkillYAMLFile` 新增 `NoLLMAPI`；`knownKeys` 加 `"no_llm_api"`；`loadSkillFromDir` 传递 |
| `gui/skill_runner.go` | `executeAsync` 传递 `skill.NoLLMAPI` 到 `NeedsOpenAIProxyAuto` |

### 测试矩阵

| 场景 | 条件 | 期望 |
|------|------|------|
| 无 env + 无声明 | `OPENAI_API_KEY` 未设置，skill 无 `no_llm_api` | ✅ 代理启动 |
| 无 env + no_llm_api | `OPENAI_API_KEY` 未设置，skill 声明 `no_llm_api: true` | ❌ 代理不启动 |
| 有 env（用户提供） | `OPENAI_API_KEY=sk-real-key` | ❌ 代理不启动（用户 key 优先） |
| 有 env（stale sentinel） | `OPENAI_API_KEY=sk-maclaw-local-proxy` | ✅ 代理启动（忽略 stale） |
| extraEnv 提供 | `extraEnv={OPENAI_API_KEY: "sk-xxx"}` | ❌ 代理不启动（调用方已提供） |
| Hub skill 无声明 | Hub 安装的 skill，无 `no_llm_api` | ✅ 代理启动（安全默认值） |

### 机制性分析

**为什么反转默认值是机制性修复**：

- **不可判定问题不用猜测解决**：静态扫描无法确定程序是否会在运行时读取环境变量。三层扫描是在做一个不可能完备的猜测。反转默认值后，不需要猜测——默认提供，不需要的自己声明
- **成本不对称决定默认值方向**：代理启动成本 ≈ 0（监听本地端口，不消耗 LLM token 直到被调用）。不启动的代价 = skill 失败 + 用户困惑 + LLM 重试循环。成本不对称时，默认值应该倒向低成本侧
- **`no_llm_api` 是声明式的**：skill 作者明确知道自己的 skill 是否需要 LLM API。让知道答案的人做声明，而非让系统猜测
- **向后兼容**：现有 skill 没有 `no_llm_api` 字段 → 默认值 `false` → 代理启动。行为从"可能不启动"变为"一定启动"，只会修复问题不会引入新问题

### 验收标准

- 无 `OPENAI_API_KEY` 环境变量 + 无 `no_llm_api` 声明的 skill → 代理自动启动，`OPENAI_API_KEY`/`OPENAI_BASE_URL`/`OPENAI_MODEL` 注入子进程
- 声明 `no_llm_api: true` 的纯本地 skill → 代理不启动
- 用户已设置有效 `OPENAI_API_KEY` → 代理不启动（用户 key 优先）
- 代码中不存在三层扫描的决策路径（扫描仅用于诊断日志）


---

## 问题 6：DeepSeek Thinking Mode `reasoning_content` 字段缺失导致 HTTP 400

### 问题描述

使用 `deepseek-reasoner` 模型时，多轮对话中 LLM 调用失败：

```
LLM 调用失败: OpenAI API 错误 (HTTP 400): {"error":{"message":"The `reasoning_content` in the thinking mode must be passed back to the API.","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}
```

### 实际 API 行为（实测验证）

通过直接调用 DeepSeek API 验证了 4 种场景：

| 场景 | reasoning_content 值 | HTTP 状态 |
|------|---------------------|----------|
| A: 完整回传 | `"Let me think..."` (原始值) | 200 ✅ |
| B: 截断回传 | `"Let me…(truncated)…think"` | 200 ✅ |
| C: 空字符串 | `""` | 200 ✅ |
| D: **字段缺失** | 字段不存在 | **400 ❌** |

**结论**：DeepSeek API 要求的是 `reasoning_content` **字段必须存在**（哪怕是空字符串），而不是值必须完整。唯一触发 400 的情况是字段在 JSON 中完全不存在。

### 根因

`corelib/agent/conversation_memory.go` 的 `ToMessage()` 方法：

```go
if e.ReasoningContent != "" {
    m["reasoning_content"] = e.ReasoningContent
}
```

当 `ReasoningContent` 是空字符串时（Go string 零值），`reasoning_content` 字段**不会被加入 map**。JSON 序列化后字段完全缺失。

**触发路径**：
1. 迭代 N：LLM 返回 `{reasoning_content: "...", tool_calls: [...]}` → 正确加入 conversation（当前轮次直接用 map，不经过 ToMessage）
2. 迭代 N 结束，history 保存到 memory（`ConversationEntry{ReasoningContent: "..."}` → 正确保存）
3. 下一次用户消息 → `memory.Load()` → `entry.ToMessage()` → 如果 `ReasoningContent` 在持久化/反序列化过程中变为空字符串 → 字段缺失 → 400

另一条触发路径：
1. `autoCompressConversation` 的 round-trip 将 `[]interface{}` 转换为 `ctxcompress.Message{Role, Content}` 再转回 `map[string]string{"role":..., "content":...}` → `reasoning_content` 和 `tool_calls` 字段完全消失
2. 后续 tool role 消息成为孤儿（无对应 tool_call_id 匹配）

### 修复

#### 1. `ToMessage()` 保证字段存在（`corelib/agent/conversation_memory.go`）

当 `ReasoningContent` 为空但 `ToolCalls` 非空时，显式设置 `reasoning_content: ""`：

```go
if e.ReasoningContent != "" {
    m["reasoning_content"] = e.ReasoningContent
} else if e.ToolCalls != nil {
    m["reasoning_content"] = ""
}
```

这是**唯一的根因修复点**。其他修改是纵深防御。

#### 2. `truncateAssistantContent` 保护 tool_calls 消息（`gui/im_conversation_trim.go`）

有 `tool_calls` 的 assistant 消息：完整保留 `reasoning_content`（虽然截断不触发 400，但保留完整推理链让模型能连贯推理）。无 `tool_calls` 的 assistant 消息：`delete(cp, "reasoning_content")` 完全移除（API 忽略此字段，移除比截断节省更多 token）。

#### 3. `autoCompressConversation` 跳过有 tool_calls 的对话（`gui/im_compress.go`）

有 `tool_calls` 的 conversation 不经过有损压缩。round-trip 不仅丢弃 `reasoning_content`，还丢弃 `tool_calls` 和 `tool_call_id`，导致 tool role 消息成为孤儿。这对**所有模型**都是问题，不仅仅是 DeepSeek。让 `trimConversation`（整组丢弃，不篡改字段）做 token 预算控制。

### 修改文件

| 文件 | 修改 | 性质 |
|------|------|------|
| `corelib/agent/conversation_memory.go` | `ToMessage()`：ToolCalls 非空时保证 `reasoning_content` 字段存在 | **根因修复**（跨轮次 history 加载路径） |
| `gui/im_message_handler.go` (×2) | 两处 inline assistant 消息构建：ToolCalls 非空时保证 `reasoning_content` 字段存在 | **根因修复**（同 loop 内迭代路径） |
| `corelib/agent/loop.go` | TUI RunLoop 的 assistant 消息构建：同上 | **根因修复**（TUI 路径） |
| `gui/im_conversation_trim.go` | `truncateAssistantContent()`：有 tool_calls 保留完整 reasoning；无 tool_calls 直接删除 | 纵深防御 + token 优化 |
| `gui/im_compress.go` | `autoCompressConversation()`：有 tool_calls 时跳过有损压缩 | 纵深防御 |

### 验收标准

- `deepseek-reasoner` 模型多轮对话 + tool_calls → 不再报 HTTP 400（实测验证通过）
- 跨轮次对话（history 加载 → ToMessage → 发送 API）→ `reasoning_content` 字段始终存在
- 无 tool_calls 的 assistant 消息 → `reasoning_content` 被完全移除（节省 token）
- `autoCompressConversation` 在有 tool_calls 消息时跳过，不破坏字段
- Kimi / GLM / OpenAI 等非 DeepSeek 模型 → 行为不变
