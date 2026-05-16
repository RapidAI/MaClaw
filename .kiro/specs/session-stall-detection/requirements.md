# Requirements Document

## Introduction

MaClaw 管理多种编程工具会话（Claude Code SDK、Gemini ACP、Codex 等），在远程模式下存在两个核心问题：

1. **会话停滞（Stall）**：编程工具处于 `busy` 状态但长时间无新输出，可能是工具内部卡住或等待某种隐式确认。人类用户通常发送"继续"来 nudge 工具恢复工作，但 MaClaw 缺乏自动检测和 nudge 机制。
2. **任务完成判断**：当前仅依赖状态转换（`busy` → `waiting_input`）判断一轮完成，但无法判断用户的整体任务是否真正完成。编程工具可能在中途停下等待输入，而实际任务只完成了一半。MaClaw 需要语义级别的完成检测，分析输出内容判断任务完成度。

本特性在 `RemoteSessionManager` 层面实现，对上层 Agent（MaClaw LLM）透明，对下层编程工具无感知。

## Glossary

- **Stall_Detector**: 停滞检测器，监控 `busy` 状态会话的输出活跃度，判断是否进入停滞状态
- **Auto_Nudger**: 自动 nudge 组件，当检测到停滞时向编程工具发送轻量级恢复消息
- **Completion_Analyzer**: 语义完成分析器，分析会话输出内容判断用户任务的完成程度
- **Session_Manager**: `RemoteSessionManager`，管理所有编程工具会话的生命周期
- **Output_Pipeline**: 输出处理管道，处理编程工具的原始输出并生成摘要、事件等
- **Nudge_Message**: 发送给编程工具的轻量级恢复消息（如"继续"），用于唤醒停滞的工具
- **Stall_Timeout**: 停滞超时阈值，`busy` 状态下无新输出超过此时间即判定为停滞
- **Completion_Signal**: 完成信号，从输出内容中提取的语义标记，指示任务完成或未完成
- **Session_Hint**: `get_session_output` 返回给 Agent 的提示文本，指导 Agent 下一步操作
- **ExecutionHandle**: 编程工具进程的抽象接口，提供 `Write`、`Output`、`Exit` 等方法

## Requirements

### Requirement 1: 会话停滞检测

**User Story:** 作为 MaClaw 系统，我需要检测编程工具会话在 `busy` 状态下是否停滞（长时间无新输出），以便触发自动恢复机制。

#### Acceptance Criteria

1. WHILE 会话状态为 `busy`，THE Stall_Detector SHALL 持续监控该会话最后一次收到新输出的时间戳
2. WHEN 会话状态为 `busy` 且距离最后一次新输出超过 Stall_Timeout，THE Stall_Detector SHALL 将该会话标记为"疑似停滞"
3. THE Stall_Timeout SHALL 具有可配置的默认值（默认 45 秒），且支持按工具类型设置不同阈值
4. WHEN 会话状态从 `busy` 转换为其他状态（`waiting_input`、`exited`、`error`），THE Stall_Detector SHALL 立即清除该会话的停滞标记和计时器
5. WHEN 会话在 `busy` 状态下收到新输出，THE Stall_Detector SHALL 重置该会话的停滞计时器

### Requirement 2: 自动 Nudge 机制

**User Story:** 作为 MaClaw 系统，我需要在检测到会话停滞时自动发送 nudge 消息唤醒编程工具，模拟人类发送"继续"的行为。

#### Acceptance Criteria

1. WHEN Stall_Detector 将会话标记为"疑似停滞"，THE Auto_Nudger SHALL 通过 ExecutionHandle 的 `Write` 方法向编程工具发送 Nudge_Message
2. THE Nudge_Message SHALL 使用通用的自然语言文本（如"continue"），兼容 Claude Code SDK、Gemini ACP 及未来工具
3. THE Auto_Nudger SHALL 在每次 nudge 后等待至少一个 Stall_Timeout 周期，避免连续发送多条 nudge 消息
4. THE Auto_Nudger SHALL 对单个会话设置最大 nudge 次数上限（默认 3 次），超过上限后停止 nudge 并更新 Session_Hint 提示 Agent 介入
5. WHEN Auto_Nudger 发送 nudge 后会话恢复输出（收到新的非空输出），THE Auto_Nudger SHALL 重置该会话的 nudge 计数器
6. THE Auto_Nudger 的操作 SHALL 对 Agent（MaClaw LLM）完全透明——不在 `get_session_output` 的输出行中显示 nudge 消息，不改变会话状态
7. THE Auto_Nudger SHALL 在日志中记录每次 nudge 操作，包含会话 ID、nudge 次数和时间戳

### Requirement 3: 工具适配层

**User Story:** 作为 MaClaw 系统，我需要 nudge 机制兼容所有支持的编程工具，且不依赖工具特定的协议细节。

#### Acceptance Criteria

1. THE Auto_Nudger SHALL 通过统一的 `ExecutionHandle.Write` 接口发送 nudge，不区分底层工具类型（Claude Code SDK、Gemini ACP、Codex、OpenCode 等）
2. WHEN 编程工具为 Codex（one-shot 模式，进程执行完即退出），THE Auto_Nudger SHALL 跳过 nudge 操作，因为 Codex 不支持交互式输入
3. IF nudge 消息发送失败（`Write` 返回错误），THEN THE Auto_Nudger SHALL 记录错误日志并停止对该会话的后续 nudge 尝试
4. THE Auto_Nudger SHALL 支持按工具类型配置不同的 Nudge_Message 文本，同时提供合理的默认值

### Requirement 4: 语义任务完成检测

**User Story:** 作为 MaClaw 系统，我需要分析编程工具的输出内容，判断用户的整体任务是否已完成，而不仅仅依赖状态转换。

#### Acceptance Criteria

1. WHEN 会话状态转换为 `waiting_input`，THE Completion_Analyzer SHALL 分析最近 N 行输出内容（N 可配置，默认 50 行），判断任务完成度
2. THE Completion_Analyzer SHALL 识别以下完成信号（Completion_Signal）：
   - Claude Code SDK: `result` 消息中的 `is_error` 为 false、输出包含"✅"标记、输出包含"I've completed"/"已完成"等模式
   - Gemini ACP: `[gemini-acp] turn complete:` 标记后跟成功指示
   - 通用模式: 输出不包含"I'll continue..."/"接下来我会..."/"Next, I'll..."等未完成指示词
3. THE Completion_Analyzer SHALL 将分析结果分为三个级别：`completed`（任务完成）、`incomplete`（任务未完成，工具中途停下）、`uncertain`（无法确定）
4. WHEN Completion_Analyzer 判定为 `completed`，THE Session_Hint SHALL 显示"✅ 任务似乎已完成，可以查看结果"
5. WHEN Completion_Analyzer 判定为 `incomplete`，THE Session_Hint SHALL 显示"⚠️ 任务似乎未完成，建议发送「继续」让编程工具继续工作"
6. WHEN Completion_Analyzer 判定为 `uncertain`，THE Session_Hint SHALL 保持当前默认提示（"⚠️ 会话正在等待用户输入"）

### Requirement 5: Session Hint 增强

**User Story:** 作为 MaClaw Agent，我需要 `get_session_output` 返回更精确的操作提示，以便根据会话的实际状态做出正确决策。

#### Acceptance Criteria

1. WHEN 会话状态为 `busy` 且 Stall_Detector 未标记停滞，THE `get_session_output` SHALL 返回提示"⏳ 编程工具正在工作中，请等待后再检查进度"
2. WHEN 会话状态为 `busy` 且 Stall_Detector 已标记停滞且 Auto_Nudger 正在尝试恢复，THE `get_session_output` SHALL 返回提示"⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查"
3. WHEN 会话状态为 `busy` 且 Auto_Nudger 已达到最大 nudge 次数仍未恢复，THE `get_session_output` SHALL 返回提示"⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话"
4. WHEN 会话状态为 `waiting_input` 且 Completion_Analyzer 判定为 `completed`，THE `get_session_output` SHALL 返回提示"✅ 任务似乎已完成，可以查看结果"
5. WHEN 会话状态为 `waiting_input` 且 Completion_Analyzer 判定为 `incomplete`，THE `get_session_output` SHALL 返回提示"⚠️ 任务似乎未完成，建议发送「继续」让编程工具继续工作"
6. THE Session_Hint 增强 SHALL 不影响现有的 `exited`、`error`、`starting`、`running` 状态提示逻辑

### Requirement 6: 停滞与完成状态同步到 Hub

**User Story:** 作为远程用户（通过飞书/PWA），我需要在移动端看到会话的停滞状态和任务完成度信息。

#### Acceptance Criteria

1. WHEN Stall_Detector 标记会话为"疑似停滞"，THE Session_Manager SHALL 更新 `SessionSummary.SuggestedAction` 字段并通过 Hub 同步到移动端
2. WHEN Completion_Analyzer 产生分析结果，THE Session_Manager SHALL 更新 `SessionSummary` 的相关字段并通过 Hub 同步到移动端
3. WHEN Auto_Nudger 成功恢复会话（nudge 后收到新输出），THE Session_Manager SHALL 清除停滞相关的 `SuggestedAction` 并同步更新

### Requirement 7: 停滞检测的生命周期管理

**User Story:** 作为 MaClaw 系统，我需要停滞检测机制正确处理会话的创建和销毁，避免资源泄漏。

#### Acceptance Criteria

1. WHEN 新会话创建并进入 `busy` 状态，THE Stall_Detector SHALL 自动开始监控该会话
2. WHEN 会话被终止（kill）或退出（exit），THE Stall_Detector SHALL 立即停止监控并释放该会话相关的所有计时器和状态
3. THE Stall_Detector SHALL 不对 `starting` 或 `running` 状态的会话进行停滞检测——仅监控 `busy` 状态
4. IF Stall_Detector 或 Auto_Nudger 内部发生 panic，THEN THE Session_Manager SHALL 捕获 panic 并记录错误日志，不影响会话的正常运行
