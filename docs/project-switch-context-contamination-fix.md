# 88. 项目切换后上下文混乱——`ResumeProject` 不更新 `current_project` + `InFlightTask` 不感知项目路径

## 来源

用户从左侧"最近项目"列表切换项目后点"继续"，maclaw 把多个不同项目的上下文混在一起。右侧面板显示 "Neither test5 nor goldensteal4 exist"——这些是其他项目的名字，与当前切换到的项目无关。

## 根因分析（四个独立问题叠加）

### 根因 1 (P0): `ResumeProject` 不更新 `config.CurrentProject`——`GetCurrentProjectPath()` 返回旧项目路径

**触发链路**：

1. 用户在左侧"最近项目"列表点击项目 B（如 "C++ 2D game"）
2. 前端调用 `ResumeProject(proj.project_path)` → 后端清理对话历史和 session 状态 ✅
3. 前端调用 `aiAssistant.sendMessage("📂 已切换到项目：C++ 2D game\n📁 D:\workprj\steave2")`
4. 后端 `handleIMMessageWithLoop` → `appendProactiveRecall` → `contextResolver.ResolveProject()` → `GetCurrentProjectPath()`
5. **`GetCurrentProjectPath()` 读取 `config.CurrentProject`——但 `ResumeProject` 从未更新这个字段**
6. 返回旧项目 A 的路径 → `RecallDynamic(msg, "", 旧项目A路径)` → 召回旧项目记忆 → 上下文混乱

**代码证据**：`ResumeProject` 做了清理但没更新 `config.CurrentProject`。前端侧边栏 `onClick` 也没调用 `handleProjectSwitch(projectId)` 来更新 config。两个函数各做了一半。

### 根因 2 (P0): 前端 config 缓存不同步——后端更新 `current_project` 后前端不知道

即使后端 `switchCurrentProjectByPath` 更新了 config，前端 React state 中的 `config.current_project` 仍是旧值。如果用户在切换后做任何触发 `SaveConfig` 的操作（改设置等），stale 的 `current_project` 会覆盖后端的正确值——与 #11/#23 相同的 config 竞态模式。

### 根因 3 (P1): `InFlightTask` 不存储项目路径——恢复时创建的 `UnfinishedTaskSlot` 缺少 `ProjectPath`

`SetInFlightTask(userID, task)` 只存储任务描述，不存储项目路径。进程被杀后恢复创建的 `UnfinishedTaskSlot` 的 `ProjectPath` 为空，切换项目后旧任务被无条件展示。

### 根因 4 (P1): `UnfinishedTaskSlot` 展示不检查当前项目路径

`handleIMMessageWithLoop` 展示 `UnfinishedTaskSlot` 提示时不检查 slot 的 `ProjectPath` 是否与当前项目匹配。切换项目后旧项目的 slot 仍被展示。

## 修复

### Fix 1: `ResumeProject` 更新 `config.CurrentProject`（`gui/app_project_search.go`）

- 新增 `switchCurrentProjectByPath(projectPath)` 方法：根据路径查找匹配的 `ProjectConfig`，更新 `config.CurrentProject`
- `ResumeProject` 在清理 session 之前调用此方法
- 使用 `LoadConfig → merge → SaveConfig` 模式避免覆盖并发修改
- 路径比较使用 `filepath.Clean` + `strings.EqualFold`（Windows 路径不区分大小写）

### Fix 2: 前端 config 同步——`config-changed` 事件（`gui/app_project_search.go`）

- `ResumeProject` 在 `switchCurrentProjectByPath` 后通过 `runtime.EventsEmit(a.ctx, "config-changed", cfg)` 通知前端
- 前端已有 `config-changed` 事件监听器（`App.tsx:2467`），自动调用 `setConfig(cfg)` 更新 React state
- 两个项目切换入口（侧边栏 `onClick` 和 `ProjectSearchPanel` `onSelect`）都经过 `ResumeProject`，统一覆盖

### Fix 3: `InFlightTask` 存储项目路径（`corelib/agent/conversation_memory.go`）

- `conversationSession` 新增 `inFlightProjectPath string` 字段
- `persistedSession` 新增 `InFlightProjectPath string` JSON 字段
- `SetInFlightTask` 签名改为 `SetInFlightTask(userID, task string, projectPath ...string)`（可变参数保持向后兼容）
- `ClearInFlightTask` 同时清除 `inFlightProjectPath`
- `ConsumeInFlightTask` 返回 `(task, projectPath)` 二元组
- `saveToDisk` / `loadFromDisk` 传递新字段

### Fix 4: `UnfinishedTaskSlot` 展示时检查项目路径匹配（`gui/im_message_handler.go`）

- 展示前比较 `slot.ProjectPath` 与 `getCurrentProjectPath()`
- 不匹配时跳过展示（slot 保留在内存中，切换回原项目时仍可恢复）
- 空 ProjectPath（旧数据）不触发过滤（向后兼容）

### Fix 5: `max_rounds` slot 创建补全 `ProjectPath`（`gui/im_message_handler.go`）

- `max_rounds` 达到上限时创建的 `UnfinishedTaskSlot` 新增 `ProjectPath: h.getCurrentProjectPath()`
- 与 `in_flight_recovery` 和 `session_exit` 路径一致

## 修改文件

| 文件 | 操作 | 说明 |
|------|------|------|
| `gui/app_project_search.go` | 修改 | `ResumeProject` 调用 `switchCurrentProjectByPath` + 发射 `config-changed` 事件；新增 `switchCurrentProjectByPath` 方法 |
| `corelib/agent/conversation_memory.go` | 修改 | `inFlightProjectPath` 字段；`SetInFlightTask` 可变参数；`ConsumeInFlightTask` 二元组返回；持久化 |
| `gui/im_message_handler.go` | 修改 | `SetInFlightTask` 传项目路径；`ConsumeInFlightTask` 二元组；slot 展示项目路径检查；`max_rounds` slot 补全 ProjectPath |
| `gui/frontend/src/App.tsx` | 无变更 | `config-changed` 事件监听器已存在，自动同步 |

## 验收标准

- 从侧边栏切换到项目 B → `GetCurrentProjectPath()` 返回项目 B 的路径
- 切换后 `RecallDynamic` 使用项目 B 的路径过滤记忆，不召回项目 A 的记忆
- 前端 `config.current_project` 通过 `config-changed` 事件同步更新
- 旧项目 A 的 `UnfinishedTaskSlot` 在项目 B 中不展示
- 切换回项目 A 时，`UnfinishedTaskSlot` 正常展示
- 进程被杀后恢复的 `UnfinishedTaskSlot` 包含正确的项目路径
- `max_rounds` 创建的 slot 包含正确的项目路径
- `SetInFlightTask` 可变参数保持向后兼容
- `corelib/agent` 和 `gui` 编译通过
- 所有相关测试通过（5 个 pre-existing RunAgentLoop 失败不受影响）
