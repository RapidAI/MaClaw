# 编程智能体可感知状态

## 目标

当 MaClaw 把编程任务交给编程智能体执行时，用户和自动化监控都应该能明确知道“现在是编程智能体在处理代码任务”，而不是只看到普通的主 Agent 忙碌状态。

## 可见位置

- 聊天进度流：显示完整任务状态行，包括编程智能体、阶段、任务 ID 和任务标题。
- AI 标题栏：显示紧凑状态徽标，适合用户在面板顶部快速确认当前执行者。
- 侧边栏系统监控：显示“任务状态”行，适合持续观察后台执行。
- 底部状态栏：显示单行紧凑状态，避免占用主工作区空间。
- 输入框 busy hint：当编程智能体活跃时，提示文本切换为编程智能体状态。

## 状态阶段

- `starting`：编程智能体启动任务。
- `running`：正在执行代码修改、命令或验证。
- `retrying`：失败后进入受控重试。
- `completed`：任务完成。
- `failed`：任务失败。
- `skipped`：任务被跳过。
- `result`：返回任务结果摘要。
- `unknown`：前端识别到编程智能体消息，但后端阶段暂未纳入已知枚举。

活跃阶段为 `starting`、`running`、`retrying`、`unknown`。终态阶段为 `completed`、`failed`、`skipped`、`result`。标题栏、侧边栏和底部状态栏只展示活跃状态，避免历史完成消息造成“还在运行”的错觉。

## 诊断选择器

所有状态表面都带有稳定的 DOM 标记：

```text
data-agent="coding"
data-active="true|false"
data-event="task_status|tool_started"
data-terminal="true|false"
data-phase="running"
data-task-id="T2"
data-run-id="42"
data-turn-id="coding-turn-42-T2"
data-change-count="3"
data-file-count="3"
data-tool-outcome="success|failed|blocked"
data-tool-outcome-state="success|failed|blocked|unknown"
data-tool-duration-ms="1250"
data-tool-count="2"
data-variant="chat-progress|title-bar|sidebar|status-bar"
```

后端优先发送结构化事件：

```text
Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T2","title":"Fix parser","run_id":"42","turn_id":"coding-turn-42-T2"}
```

旧格式 `Coding Agent: running T2 - Fix parser` 仍保留前端兼容解析能力，便于历史消息和旧插件继续显示。

`run_id` 标识任务编排器的一次运行轮次，`turn_id` 标识一次具体的编程智能体执行 turn。任务状态、工具调用、diff 变更、后续审批事件应共享同一个 `turn_id`，监控 UI 可以据此把一组事件聚合为同一条编程智能体活动。

当前事件类型：

- `task_status`: 任务阶段变化，例如 starting/running/completed/failed/retrying/skipped/result。
- `tool_started`: 编程智能体开始调用工具，`detail` 为工具名。
- `tool_finished`: 编程智能体完成工具调用，`detail` 为工具名，`outcome` 为 `success`、`failed` 或 `blocked`。
  - `duration_ms` 表示本次工具调用耗时，单位毫秒。
  - `summary` 只在失败或阻断时携带简短首行原因，避免成功路径产生噪音。
- `guardrail_summary`: 编程智能体触发安全边界拦截时发送，`outcome` 为 `blocked`，`summary` 为首个拦截摘要，`count` 为拦截次数。
- `verification_summary`: 编程智能体完成验证状态归纳，`outcome` 为 `passed`、`failed`、`missing` 或 `not_needed`，`summary` 为简短原因，`count` 为识别到的验证命令数。
- `exploration_summary`: 编程智能体完成探索状态归纳，`outcome` 为 `explored`、`read_only`、`missing` 或 `not_needed`，`summary` 为简短原因，`count` 为成功搜索次数。
- `diff_check`: 编程智能体完成最终 diff 自检状态归纳，`outcome` 为 `checked`、`skipped` 或 `failed`，`summary` 为简短原因，`count` 为修改文件数。
- `diff_updated`: 编程智能体成功写入或编辑文件，`detail` 为变更文件与当前变更文件数。
- `diff_summary`: 编程智能体完成最终 diff 自检，`count` 为变更文件数，`files` 为变更文件列表，`detail` 为压缩摘要。

CSS 类名也保持稳定：

```text
coding-agent-status
coding-agent-status--title-bar
coding-agent-status--running
```

前端测试和后续监控可以使用 `codingAgentStatusSelector(...)` 生成选择器，例如：

```text
.coding-agent-status[data-variant="title-bar"][data-phase="retrying"][data-task-id="T9"]
.coding-agent-status[data-turn-id="coding-turn-42-T2"]
```

侧边栏会额外渲染编程智能体任务卡片：

```text
[data-testid="sidebar-coding-agent-card"][data-turn-id="coding-turn-42-T2"]
```

任务卡片聚合同一个 `turn_id` 下的最新状态、最近工具轨迹、变更文件、diff 摘要，并暴露：

- `data-turn-id`: 当前编程 turn。
- `data-tool`: 最近一次工具调用。
- `data-tool-outcome`: 最近一次工具调用结果，常见值为 `success`、`failed`、`blocked`。
- `data-tool-outcome-state`: 标准化后的工具结果状态，未知值归一为 `unknown`。
- `data-tool-duration-ms`: 最近一次工具调用耗时，单位毫秒。
- `data-tool-count`: 当前卡片展示的最近工具轨迹数量，最多 3 个。
- `data-guardrail-status`: 编程智能体安全边界归纳原始状态。
- `data-guardrail-state`: 标准化后的安全边界状态，未知值归一为 `unknown`。
- `data-guardrail-count`: 本 turn 中安全边界拦截次数。
- `data-exploration-status`: 编程智能体探索归纳原始状态。
- `data-exploration-state`: 标准化后的探索状态，未知值归一为 `unknown`。
- `data-exploration-count`: 本 turn 中成功搜索次数。
- `data-verification-status`: 编程智能体验证归纳原始状态。
- `data-verification-state`: 标准化后的验证状态，未知值归一为 `unknown`。
- `data-verification-count`: 本 turn 中识别到的验证命令数。
- `data-diff-check-status`: 编程智能体 diff 自检原始状态。
- `data-diff-check-state`: 标准化后的 diff 自检状态，未知值归一为 `unknown`。
- `data-change-count`: diff 摘要里的变更文件数。
- `data-file-count`: 当前卡片展示/聚合到的文件数。

卡片正文会显示 `轨迹/Trace` 行，按时间顺序展示最近 3 个工具，例如 `read_file Success 80ms -> apply_patch Blocked 300ms (outside project) -> test`。未完成的工具只显示工具名，完成后补充结果、耗时和异常摘要。每个工具段都会暴露 `data-tool-trace-name`、`data-tool-trace-outcome`、`data-tool-trace-outcome-state`、`data-tool-trace-summary`，便于 UI 自动化和监控直接定位失败或阻断的工具。

卡片正文会显示 `边界/Guard` 行，例如 `Blocked (1)`，用于提示用户编程智能体被安全策略拦截过，摘要可从 `data-guardrail-summary` 读取。

卡片正文会显示 `探索/Explore` 行，例如 `Explored (2)`、`Read` 或 `Missing`，用于提示用户编程智能体是否先搜索/读取再修改。

卡片正文也会显示 `验证/Verify` 行，例如 `Passed (1)`、`Failed (2)` 或 `Not run`，用于提示用户编程智能体是否完成了命令级验证。

卡片正文还会显示 `Diff 自检/Diff check` 行，例如 `Checked` 或 `Skipped`，用于提示用户编程智能体是否完成最终 diff 范围自检。

卡片会附带 outcome class，便于样式和测试定位：

```text
.coding-agent-turn-card--success
.coding-agent-turn-card--failed
.coding-agent-turn-card--blocked
.coding-agent-turn-card--unknown
```

## 回归要求

- 后端新增编程智能体阶段时，必须同步更新前端 phase 枚举和解析测试。
- 新增 UI 状态表面时，必须复用共享状态组件或共享 data/class helper。
- 终态消息可以保留在聊天历史中，但不能在活跃监控位置持续显示。

## 2026-05-08 补充：命令状态

- `command_summary`: 编程智能体完成 bash 命令归纳时发送，`outcome` 为 `passed`、`failed` 或 `none`，`summary` 为命令总数和失败摘要，`count` 为本轮命令数。
- 侧栏卡片新增 `Commands/命令` 行，例如 `Passed (2)`、`Failed (1)` 或 `None`，帮助用户判断编程智能体是否真的运行过命令以及是否有失败。
- 侧栏卡片新增稳定属性：`data-command-status`、`data-command-state`、`data-command-count`，命令摘要可从 `data-command-summary` 读取。
## 2026-05-08 补充：质量总览状态

- `quality_summary`: 编程智能体完成本轮质量归纳时发送，`outcome` 为 `passed`、`warning` 或 `failed`，`summary` 为问题摘要，`count` 为失败/警告项数量。
- 质量总览把安全边界、命令、探索、验证、diff 自检合并成一个总灯：安全边界或验证失败为 `failed`，普通命令失败或有改动但缺少探索/验证/diff 自检为 `warning`，其余为 `passed`。
- 侧栏卡片新增 `Quality/质量` 行，例如 `Passed`、`Warning (2)` 或 `Failed (1)`，并暴露 `data-quality-status`、`data-quality-state`、`data-quality-count` 与 `data-quality-summary`。

## 2026-05-08 补充：文件动作状态

- `file_activity_summary`: 编程智能体完成文件动作归纳时发送，`outcome` 为 `changed`、`read_only` 或 `none`，`detail` 为 `read N / modified N / created N`，`summary` 为文件动作摘要，`count` 为本轮读/改/建动作数。
- 侧栏卡片新增 `Activity/文件动作` 行，例如 `Changed (read 2 / modified 1 / created 1)`，用于让用户确认编程智能体是否读过文件、改过文件、新建过文件。
- 侧栏卡片新增稳定属性：`data-file-activity-status`、`data-file-activity-state`、`data-file-activity-count`，摘要和明细可从 `data-file-activity-summary`、`data-file-activity-detail` 读取。
