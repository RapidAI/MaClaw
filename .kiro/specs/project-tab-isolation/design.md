# 技术设计：任务隔离 Tab + 归档沉淀经验

## 架构概览

```
┌─────────────────────────────────────────────────────────────┐
│  AIAssistantPanel                                           │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  AITabBar (单行 + 溢出下拉)                          │    │
│  │  [AI 助手] [C++游戏▼] [论文综述] [▼ 更多(3)]        │    │
│  └─────────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────────┐    │
│  │  ActiveTabContent                                    │    │
│  │  - local Tab: 现有 AI 助手面板内容                    │    │
│  │  - project Tab: 独立对话 + 项目隔离上下文             │    │
│  │  - ve/group Tab: 现有数字员工/群聊内容                │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

## 数据模型变更

### 1. AITabTypes.ts 扩展

```typescript
// 新增 "project" 类型
export type AITabType = "local" | "ve" | "group" | "project";

export interface AITab {
    id: string;
    type: AITabType;
    title: string;
    veId?: string;
    participants?: string[];
    // --- 新增 project 字段 ---
    projectPath?: string;      // 绑定的项目路径（type="project" 时必填）
    projectName?: string;      // 显示名称
    archived?: boolean;        // 是否已归档（只读模式）
    closable: boolean;
}

export interface AITabState {
    history: unknown[];
    scrollTop: number;
    inputText: string;
    sessionId?: string;
    // --- 新增 ---
    projectPath?: string;      // 冗余存储，方便持久化恢复
    lastActiveAt?: number;     // 最后活跃时间戳（用于排序和清理）
}
```

### 2. 后端 Session 模型

**已有机制（无需新建）**：

后端通过 `SendAIAssistantMessage` 的 `ProjectPath` 参数实现 per-project 隔离：

```go
// gui/app_wails_bindings.go (已存在)
userID := desktopUserID
if req.ProjectPath != "" {
    userID = fmt.Sprintf("desktop-user:%s", req.ProjectPath)
}
```

所有下游组件（ConversationMemory、WorkflowEngine、DriftDetector、proactive recall）自动按 `userID` 隔离。前端只需在 Project Tab 发消息时传入 `project_path` 参数即可。

**不需要**：
- 不需要 `ProjectTabSessionManager`
- 不需要 `ProjectTabSession` 结构体
- 不需要新的 Wails binding（`SendAIAssistantMessage` 已支持 `project_path`）

### 3. 持久化格式

```
~/.maclaw/data/sessions/
├── tab_abc123.json          # 单个 Tab 的对话历史
├── tab_def456.json
└── _index.json              # Tab 列表索引（恢复 Tab 栏状态）
```

`_index.json` 结构：
```json
{
  "tabs": [
    {
      "id": "tab_abc123",
      "type": "project",
      "title": "C++ 打飞机游戏",
      "projectPath": "D:\\workprj\\test5",
      "lastActiveAt": 1714000000,
      "archived": false
    }
  ]
}
```

单个 Tab session 文件结构：
```json
{
  "tab_id": "tab_abc123",
  "project_path": "D:\\workprj\\test5",
  "conversation": [...],
  "scroll_top": 1234,
  "input_text": "",
  "created_at": "2026-05-10T10:00:00Z",
  "last_active_at": "2026-05-15T14:30:00Z"
}
```

## 核心流程设计

### 流程 1: 点击最近任务 → 创建 Project Tab

```
用户点击任务列表项
  → ProjectSearchPanel.onSelect(item)
  → 检查是否已有同 projectPath 的 Tab
    → 有: activateTab(existingTabId)
    → 无: createProjectTab(item.project_path, item.name)
      → 前端: 创建 AITab{type:"project", projectPath, title}
      → 前端: 激活新 Tab
      → 后端: CreateProjectTabSession(tabID, projectPath)
        → 检查磁盘是否有已保存的 session → 有则恢复
        → 无则从长期记忆召回项目上下文 → 注入初始系统消息
      → 前端: 收到初始消息后渲染
```

### 流程 2: Project Tab 中发送消息

```
用户在 Project Tab 中输入消息
  → sendMessage(text, {tabId, projectPath})
  → 后端: SendAIAssistantMessageForTab(tabID, projectPath, text)
    → 查找 ProjectTabSession
    → buildSystemPrompt 使用 session.ProjectPath（非全局 currentProject）
    → appendProactiveRecall 使用 session.ProjectPath（严格项目过滤）
    → runAgentLoop 使用 session.conversation（独立历史）
    → 工具执行的 workDir = session.ProjectPath
  → 响应通过事件推送到前端对应 Tab
```

### 流程 3: Tab 切换

```
用户点击另一个 Tab
  → saveTabState(currentTabId, {history, scrollTop, inputText})
  → activateTab(targetTabId)
  → getTabState(targetTabId) → 恢复 UI 状态
  → 纯前端操作，不涉及后端调用
```

### 流程 4: 关闭 Project Tab

```
用户关闭 Project Tab
  → closeTab(tabId)
  → 后端: CloseProjectTabSession(tabID)
    → 持久化 conversation 到磁盘
    → 从内存中移除 session
  → 前端: 移除 Tab，切换到 local Tab
```

### 流程 5: 归档任务

```
用户右键 → 归档
  → 确认对话框
  → 后端: ArchiveProject(projectPath)
    → 1. 从 memoryStore 中收集该项目的所有 task_artifact + project_knowledge
    → 2. 调用 LLM 生成经验摘要（结构化 prompt）
    → 3. 保存摘要为新 entry:
         Category: project_knowledge
         Scope: ScopeGlobal
         Tags: ["archived_experience", projectPath, 关键技术词]
    → 4. 在 ProjectIndex 中标记 archived=true
    → 5. 如果有打开的 Project Tab → 标记为 archived（只读）
  → 前端: 刷新任务列表 + 关闭对应 Tab（或标记只读）
```

## 关键设计决策

### 决策 1: 后端消息路由——如何区分 local Tab 和 Project Tab 的消息

**方案**: 前端发送消息时携带 `tabId` 参数。后端根据 `tabId` 查找对应的 `ProjectTabSession`。

- `tabId == "local"` 或为空 → 走现有的 `IMMessageHandler` 路径（全局 currentProject）
- `tabId` 匹配某个 `ProjectTabSession` → 走隔离路径

**实现**: 新增 Wails binding `SendAIAssistantMessageForTab(tabID, text string)`，内部委托给 `ProjectTabSessionManager`。

### 决策 2: Project Tab 的 system prompt 构建

复用 `buildSystemPromptBase`，但覆盖以下参数：
- `contextResolver.ResolveProject()` → 返回 session.ProjectPath（非全局 config.CurrentProject）
- `appendProactiveRecall` 的 projectPath → session.ProjectPath
- 工作流引擎状态 → 按 tabID 隔离

**不复制代码**——通过注入 `contextResolver` 接口实现：

```go
type TabContextResolver struct {
    projectPath string
}

func (r *TabContextResolver) ResolveProject() (string, error) {
    return r.projectPath, nil
}
```

### 决策 3: 对话历史隔离

每个 `ProjectTabSession` 有自己的 `[]ConversationEntry`，不共享 `IMMessageHandler.memory`（那是 local Tab 的）。

- local Tab: 使用 `handler.memory`（现有行为不变）
- Project Tab: 使用 `session.conversation`（独立切片）

`saveConversationHistoryTimed` 对 Project Tab 写入 session 文件而非 `handler.memory`。

### 决策 4: Tab 栏溢出处理

```
可见区域（自动计算宽度）:
[AI 助手] [C++游戏] [论文综述] [▼ 3]

点击 "▼ 3" 展开下拉:
┌──────────────────────┐
│ 📌 SSH 服务器运维     ×│
│ 🔖 PPT 设计          ×│
│ 🔖 合同审查          ×│
└──────────────────────┘
```

- 可见 Tab 数量根据容器宽度动态计算（每个 Tab 最小宽度 100px）
- 溢出的 Tab 按 `lastActiveAt` 降序排列在下拉中
- local Tab 始终可见（第一个位置）

### 决策 5: 归档经验提取的 LLM Prompt

```
你是一个项目经验提取专家。请从以下项目记录中提取关键经验，生成结构化摘要。

项目名称：{projectName}
项目路径：{projectPath}

项目记录：
{entries_content}

请按以下格式输出经验摘要：

## 任务目标
（一句话描述这个项目/任务要做什么）

## 技术方案
（使用了什么技术栈、架构、工具）

## 关键决策
（做了哪些重要的技术/设计决策，为什么）

## 踩坑与解决
（遇到了什么问题，如何解决的）

## 产出物
（最终产出了什么文件/成果）

## 可复用经验
（未来类似项目可以直接复用的经验/模式/代码片段）
```

### 决策 6: 严格项目过滤模式

`RecallDynamic` 在 Project Tab 场景下的过滤逻辑：

```go
// 现有逻辑（软过滤）:
// ScopeProject + tags 不匹配 → 排除
// ScopeProject + tags 匹配 → 允许
// 非 ScopeProject → 始终允许

// Project Tab 严格模式（新增）:
// ScopeProject + tags 不匹配当前项目 → 排除
// ScopeProject + tags 匹配当前项目 → 允许
// ScopeGlobal → 允许（归档经验、通用知识）
// user_fact / preference → 允许（用户偏好始终可用）
// 其他项目的 project_knowledge → 排除
```

实现方式：`RecallDynamic` 新增 `strictProject bool` 参数（可变参数，向后兼容）。Project Tab 路径传 `strictProject=true`，local Tab 和 IM 通道传 `false`（默认行为不变）。

## 修改文件清单

### 前端

| 文件 | 变更 |
|------|------|
| `AITabTypes.ts` | 新增 `"project"` 类型 + `projectPath`/`archived` 字段 |
| `useAITabManager.ts` | 新增 `createProjectTab()` + 上限改 16 + 持久化逻辑 |
| `AITabBar.tsx` | 溢出下拉菜单 + 动态可见 Tab 计算 |
| `AITabItem.tsx` | project Tab 的图标/样式 + archived 只读标记 |
| `ProjectSearchPanel.tsx` | `onSelect` 从 `ResumeProject` 改为 `createProjectTab` |
| `AIAssistantPanel.tsx` | Project Tab 的消息发送路由 + 对话状态管理 |
| `AssistantActiveTabContent.tsx` | 新增 project Tab 的渲染分支 |

### 后端

| 文件 | 变更 |
|------|------|
| `gui/project_tab_session.go` | 新文件：`ProjectTabSessionManager` + `ProjectTabSession` |
| `gui/project_tab_session_persist.go` | 新文件：session 持久化（读写 JSON） |
| `gui/project_tab_archive.go` | 新文件：归档逻辑 + LLM 经验提取 |
| `gui/app_project_search.go` | `ResumeProject` 保留向后兼容 + 新增 `CreateProjectTabSession` / `CloseProjectTabSession` / `ArchiveProject` bindings |
| `gui/app_wails_bindings.go` | 注册新 bindings |
| `gui/im_message_handler.go` | `SendAIAssistantMessageForTab` 路由逻辑 |
| `gui/im_system_prompt.go` | `appendProactiveRecall` 支持 strictProject 模式 |
| `corelib/memory/store.go` | `RecallDynamic` 新增 `strictProject` 可变参数 |
| `corelib/memory/project_index.go` | `ProjectRecord` 新增 `Archived bool` + `SetArchived()` |

## 向后兼容

- local Tab 行为完全不变——`SendAIAssistantMessage`（无 tabId）走现有路径
- IM 通道不受影响——IM 消息不经过 Tab 系统
- `ResumeProject` binding 保留——旧版前端仍可调用（降级为在 local Tab 中切换）
- `RecallDynamic` 默认 `strictProject=false`——所有现有调用方行为不变
