# PPT 生成阶段 write_file 被 isWorkflowProjectMutationPath 错误拦截

## 问题现象

PPT 设计工作流进入"PPT 生成"阶段后，LLM 尝试用 `write_file` 写入 Node.js 脚本（`.js` 文件）来生成 PPTX，被系统反复拒绝：
```
[agent-loop] rejected execution of workflow-blocked tool call "write_file"
(reason=artifact workflow phase cannot write into source/project paths)
```

Agent loop 跑了 29 轮迭代、近 30 分钟，最终被漂移检测器强制终止。

## 根因（机制层面）

### 问题出在哪

`validateWorkflowArtifactPhaseToolCall()` 对 `write_file` 的路径做了 `isWorkflowProjectMutationPath()` 检查。该函数用**文件扩展名**做判断：

```go
func isWorkflowProjectMutationPath(path string) bool {
    // ...
    for _, suffix := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ...} {
        if strings.HasSuffix(normalized, suffix) {
            return true
        }
    }
    return false
}
```

**设计意图**：防止 artifact generation 阶段（如 PPT 生成）意外修改项目源代码文件。

**实际效果**：任何扩展名为 `.js`/`.py`/`.ts` 等的文件**无论写到什么位置**（项目目录、临时目录、workspace）都被拦截。

### 为什么这是机制性错误

`isWorkflowProjectMutationPath` 试图用一个维度（扩展名）来同时判断两件不同的事：
1. **文件是不是源代码？**（看扩展名）
2. **写入操作是不是在修改项目？**（看路径位置）

这两件事是正交的：
- `.js` 文件可以是项目源代码（`src/app.js`），也可以是临时工具脚本（`$TEMP/generate_pptx.js`）
- 项目目录下的 `.pptx` 文件不是源代码，但确实在"修改项目"

**正确的判断标准是"路径位置"而非"文件扩展名"。**

artifact generation 阶段的安全语义是：
- 允许写入**产物文件**（.pptx、.pdf、.docx 等）到任何位置
- 允许写入**工具脚本**到临时目录（生成产物的手段）
- 禁止写入**项目源代码目录**（src/、app/、cmd/ 等）修改项目逻辑

当前实现把"手段"和"结果"混为一谈：拦截了"用于生成产物的工具脚本"，导致产物永远无法生成。

### 对比 `isMutationScopeAllowed`（agent service 层）

`corelib/agentservice/core_agent_executor.go` 的 `MutationScopeArtifact` 处理方式是**正确的**：
```go
if scope == v2.MutationScopeArtifact {
    switch name {
    case "edit_file", "task", "delegate_task", "ssh":
        return false  // 禁止这些工具
    }
    return true  // write_file、bash 等允许
}
```

它不对 write_file 做路径检查——因为 artifact 阶段的 write_file 就是用来写产物的，这是它的正常职责。GUI 层的 `validateWorkflowArtifactPhaseToolCall` 额外加了路径检查，与 agent service 层的策略不一致。

### 为什么 LLM 陷入死循环

1. `isWorkflowProjectMutationPath` 只看扩展名，不看路径位置
2. LLM 收到拒绝消息 "cannot write into source/project paths"
3. LLM 理解为"路径有问题"，换路径重试（项目目录 → workspace → temp 目录）
4. 但 LLM 不换扩展名——因为它要写的就是 `.js` 脚本（这是生成 PPTX 的唯一途径）
5. 所有路径的 `.js` 扩展名都被拦截 → 无限循环
6. 漂移检测器正确触发（所有返回结果相同），agent 停止

## 修复方案

### 修复 1（根因）：移除 `isWorkflowProjectMutationPath` 中的扩展名匹配

路径判断只看路径位置（目录前缀），不看扩展名。同时增加 Windows 盘符前缀的剥离（`D:\专利申请测试1\gen.js` → `专利申请测试1/gen.js`），确保绝对路径经过正确的前缀匹配。

```go
func isWorkflowProjectMutationPath(path string) bool {
    normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
    normalized = strings.TrimPrefix(normalized, "./")

    // Strip Windows drive letter prefix
    if len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/' {
        normalized = normalized[3:]
    }

    // Only directory-prefix matching determines "project mutation".
    for _, prefix := range []string{"src/", "app/", "cmd/", "internal/", "pkg/", "web/", "frontend/", "backend/"} {
        if strings.HasPrefix(normalized, prefix) {
            return true
        }
    }
    return false
}
```

### 修复 2（同类问题预防）：`workflowArtifactPhaseAllowedTools` 新增 `bash` 和 `list_directory`

修复 `write_file` 路径拦截后，LLM 下一步需要执行写好的脚本。`craft_tool` 在允许列表但可能因 runtime_missing 失败，此时 LLM 需要 `bash` 作为 fallback 执行路径。`list_directory` 用于验证产物是否生成。

与 agent service 层 `isMutationScopeAllowed(MutationScopeArtifact, "bash")` 返回 true 保持一致。

### 为什么这是机制性修复

1. **不依赖文件类型猜测**：不再假设 `.js` 就是源代码。artifact 阶段写入 `.js` 工具脚本（用于生成 PPTX）、`.py` 转换脚本（用于格式转换）都是合法的
2. **与 agent service 层一致**：`isMutationScopeAllowed(MutationScopeArtifact, "write_file")` 和 `isMutationScopeAllowed(MutationScopeArtifact, "bash")` 均返回 true，GUI 层不应再额外限制
3. **目录前缀是正确的信号**：项目源代码有明确的目录结构（src/、app/、cmd/ 等），这是区分"修改项目"和"生成产物"的正确边界
4. **消除整个 dead-loop 类别**：不再有"扩展名不可换但路径无效"的不可打破循环

## 漂移检测器为什么没有更早终止

漂移检测器表现正确。#48 的修复（`resultsAreChanging` 检查）不适用于这个场景——所有 write_file 调用返回的都是**完全相同**的拒绝消息（`[system rejected] artifact workflow phase cannot write into source/project paths`），所以 `resultsAreChanging` 正确返回 false，漂移正确触发。

第一次漂移在 iteration 25 触发（`needHuman=false, replanCount=1`），LLM 没有改变策略（它不知道问题出在扩展名上），第二次漂移在 iteration 29 触发（`needHuman=true, replanCount=2`），agent 终止。

漂移检测器的职责是"检测死循环并终止"——它做到了。问题不在漂移检测器，而在上游的 `isWorkflowProjectMutationPath` 产生了不应存在的死循环。

## 额外优化（非根因但有价值）

### 拒绝消息应包含具体原因

当前拒绝消息 `"artifact workflow phase cannot write into source/project paths"` 对 LLM 不可操作——它不知道是路径还是扩展名导致的拒绝。

建议改为：`"artifact workflow phase blocked write_file: path %q matches project source pattern (suffix: %s). Use a non-code extension or write to a temp directory."`

这样 LLM 至少知道该换扩展名而非换路径。

### #88 的 TruncationBlockedTools 机制应覆盖 policy rejection

当前 #88 只处理 `finish_reason=length` 截断的场景。但 `policy_rejected` 导致的反复失败是同一类问题——工具在当前场景下永远不可能成功。应该在连续 N 次 policy_rejected 后，像 #88 一样从工具列表中移除该工具并注入替代方案提示。
