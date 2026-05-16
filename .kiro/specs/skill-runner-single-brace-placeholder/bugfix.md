# Bug: Skill Runner `{input}` 单花括号占位符未替换

## 问题描述

通过 `manage_skill` 标准运行路径执行 Skill 时，步骤命令中的单花括号占位符（如 `{input}`、`{output}`）不会被替换为实际值。

用户看到的错误信息：`未提供的参数: {input`（注意残留的 `{`）。

实际命令模板：`node "{baseDir}/run_rapidocr.js" "{input}"`

## 根因（两层）

### 第一层：参数预检误报

`detectImplicitRequiredArgs()` 使用 `unresolvedSkillPlaceholderPattern` 正则扫描命令中的占位符，但：
- 正则不匹配 `{key}` 单花括号（只匹配 `{{key}}` 和 `${key}`）
- key 提取的 `TrimPrefix` 链缺少 `TrimPrefix(key, "{")`，导致 `{input}` 被提取为 `{input`（残留开头花括号）
- `vars["{input"]` 查不到值 → 报"缺少参数"

TUI 的 `implicitArgReTUI` 正则同样不匹配 `{key}`。

### 第二层：变量替换遗漏

即使绕过预检，`substituteSkillVariables()` 的替换循环只处理 `{{key}}` 和 `${key}`，不处理 `{key}` → 脚本收到字面量 `"{input}"`。

## 修复（6 处）

### gui/skill_runner.go
1. `substituteSkillVariables()`：替换循环新增 `"{" + key + "}"` 模式（排在 `{{key}}` 和 `${key}` 之后，避免部分消费）
2. `unresolvedSkillPlaceholderPattern`：正则新增 `\{[a-zA-Z_][a-zA-Z0-9_]*\}` 分支
3. `detectImplicitRequiredArgs()`：key 提取新增 `TrimPrefix(key, "{")`

### tui/agent_tools.go
4. `substituteSkillVariables()`：同步新增 `{key}` 替换
5. `substituteSkillVariablesRaw()`：同步新增 `{key}` 替换
6. `implicitArgReTUI` 正则：新增第三个捕获组 `\{([a-zA-Z_]\w*)\}`；提取逻辑新增 `m[3]`

## 不受影响

- `{baseDir}` / `{base_dir}`：在 SKILL.md 导入阶段由 `resolveBaseDirInBlock()` 单独处理，运行时不会出现
- `{{key}}` 和 `${key}` 行为不变
- 替换顺序保证 `{{input}}` 不会被 `{input}` 部分消费

## 测试覆盖

- GUI：8 个替换测试 + 3 个检测测试
- TUI：7 个替换测试 + 2 个检测测试
- corelib/skill：14 个 SKILL.md 解析测试（回归验证）
