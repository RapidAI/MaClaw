# Implementation Plan: 会话停滞检测与语义完成分析

## Overview

为 MaClaw 的 RemoteSessionManager 层增加会话停滞检测（StallDetector）和语义任务完成分析（CompletionAnalyzer）能力。实现分为：新增两个独立组件文件、扩展 RemoteSession 字段、在 output loop 和 tool handler 中集成、编写 gopter PBT 测试。

## Tasks

- [x] 1. 定义核心类型与 CompletionAnalyzer 实现
  - [x] 1.1 在 remote_types.go 中新增 StallState、CompletionLevel 类型和 RemoteSession 扩展字段
    - 新增 `StallState` 枚举（StallStateNormal, StallStateSuspected, StallStateStuck）
    - 新增 `CompletionLevel` 枚举（CompletionUncertain, CompletionCompleted, CompletionIncomplete）
    - 在 `RemoteSession` struct 中新增 `StallState`、`CompletionLevel`、`LastNudgeCount` 字段
    - _Requirements: 1.2, 4.3_

  - [x] 1.2 创建 session_completion_analyzer.go 实现 CompletionAnalyzer
    - 定义 `CompletionAnalyzerConfig` struct（AnalyzeLineCount 默认 50）
    - 实现 `NewCompletionAnalyzer` 构造函数
    - 实现 `Analyze(lines []string, tool string, sdkResult *SDKResultPayload) CompletionLevel` 方法
    - 完成信号匹配：``、`I've completed`、`已完成`、`All done`、`Successfully`、`Changes applied`
    - 未完成信号匹配：`I'll continue`、`接下来我会`、`Next, I'll`、`Let me continue`、`I need to`、`还需要`
    - Gemini ACP: `[gemini-acp] turn complete:` 后跟成功指示
    - SDK result: `is_error` 为 false 时倾向 completed
    - 空输入返回 CompletionUncertain
    - _Requirements: 4.1, 4.2, 4.3_

  - [ ]* 1.3 编写 CompletionAnalyzer 的 property-based 测试（Property 7）
    - **Property 7: Completion Analysis Classification**
    - 使用 gopter 生成随机输出行，随机插入完成/未完成信号
    - 验证结果为三个级别之一，且信号匹配正确
    - **Validates: Requirements 4.1, 4.2, 4.3**

  - [ ]* 1.4 编写 CompletionAnalyzer 的单元测试
    - 测试空输出返回 CompletionUncertain
    - 测试 SDK result（is_error=false）倾向 CompletionCompleted
    - 测试 Gemini ACP 完成标记识别
    - 测试混合信号场景（完成信号数 > 未完成信号数 → completed）
    - _Requirements: 4.1, 4.2, 4.3_

- [x] 2. 实现 StallDetector 核心组件
  - [x] 2.1 创建 session_stall_detector.go 实现 StallDetector
    - 定义 `StallDetectorConfig` struct（StallTimeout 默认 45s, MaxNudgeCount 默认 3, NudgeMessages map, DefaultNudge "continue"）
    - 定义 `sessionStallState` 内部 struct（timer, stallState, nudgeCount, lastOutput, cancelCh）
    - 实现 `NewStallDetector` 构造函数
    - 实现 `StartMonitoring(sessionID, exec ExecutionHandle, tool string)` — 启动 per-session goroutine + timer
    - 实现 `StopMonitoring(sessionID)` — 停止监控，清除计时器和状态
    - 实现 `ResetTimer(sessionID, hasNewOutput bool)` — 重置计时器，recovering 时重置 nudge 计数器
    - 实现 `GetState(sessionID) StallState` 和 `GetNudgeCount(sessionID) int`
    - 实现 `IsNudgeEcho(sessionID, line string) bool` — 防御性 nudge echo 过滤
    - 实现 `Close()` — 停止所有监控
    - per-session goroutine 内部逻辑：timer 到期 → 检查 nudgeCount → 发送 nudge 或转 StallStateStuck
    - Write 失败时立即停止 nudge 并转 StallStateStuck
    - goroutine 使用 `defer recover()` 捕获 panic
    - Codex 工具跳过 nudge（one-shot 模式）
    - 每次 nudge 记录日志（session ID, nudge count, timestamp）
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 3.1, 3.2, 3.3, 3.4, 7.1, 7.2, 7.3, 7.4_

  - [x] 2.2 在 StallDetector 中实现状态变更回调机制
    - 新增 `OnStallStateChanged` 回调字段（用于 Hub 同步和 session 字段更新）
    - 在 nudge goroutine 中状态变更时调用回调
    - 回调内通过 `session.mu.Lock()` 安全更新 `session.StallState`、`session.LastNudgeCount`、`session.Summary.SuggestedAction`
    - _Requirements: 6.1, 6.3_

  - [ ]* 2.3 编写 StallDetector 的 property-based 测试（Property 1: Stall Timeout Detection）
    - 使用 gopter 生成随机 StallTimeout（1s-120s）和随机等待时间
    - 验证：等待时间 > StallTimeout 时状态为 StallStateSuspected
    - **Validates: Requirements 1.2**

  - [ ]* 2.4 编写 StallDetector 的 property-based 测试（Property 2: Timer Reset on New Output）
    - 生成随机 session，在 busy 状态下随机时间点发送输出
    - 验证：每次输出后 timer 重置，recovering 时 nudge counter 归零
    - **Validates: Requirements 1.1, 1.5, 2.5**

  - [ ]* 2.5 编写 StallDetector 的 property-based 测试（Property 3: Nudge Delivery via Unified Write Interface）
    - 生成随机工具类型（非 Codex），触发 stall
    - 验证：Write 被调用且参数为配置的 nudge 文本
    - **Validates: Requirements 2.1, 3.1**

  - [ ]* 2.6 编写 StallDetector 的 property-based 测试（Property 4: Nudge Rate Limiting and Maximum Count）
    - 生成随机 MaxNudgeCount（1-10）和 StallTimeout
    - 模拟持续无输出场景
    - 验证：nudge 间隔 >= StallTimeout，总次数 <= MaxNudgeCount
    - **Validates: Requirements 2.3, 2.4**

  - [ ]* 2.7 编写 StallDetector 的 property-based 测试（Property 5: Nudge Failure Stops Retries）
    - 配置 Write 在第 N 次调用时返回错误
    - 验证：错误后不再有 Write 调用，状态为 StallStateStuck
    - **Validates: Requirements 3.3**

  - [ ]* 2.8 编写 StallDetector 的单元测试
    - 测试 Codex 工具跳过 nudge（Validates: 3.2）
    - 测试默认配置值 StallTimeout=45s, MaxNudgeCount=3（Validates: 1.3）
    - 测试按工具类型配置不同 nudge 文本（Validates: 3.4）
    - 测试 nudge 日志记录包含 session ID、nudge count、timestamp（Validates: 2.7）
    - 测试 panic recovery 不影响 session 正常运行（Validates: 7.4）

- [x] 3. Checkpoint — 确保核心组件测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. 在 RemoteSessionManager 中集成 StallDetector 和 CompletionAnalyzer
  - [x] 4.1 扩展 RemoteSessionManager struct 并在 NewRemoteSessionManager 中初始化
    - 在 `RemoteSessionManager` struct 新增 `stallDetector *StallDetector` 和 `completionAnalyzer *CompletionAnalyzer` 字段
    - 在 `NewRemoteSessionManager` 中创建并初始化两个组件（默认配置）
    - 设置 StallDetector 的 `OnStallStateChanged` 回调，更新 session 字段并通过 hubClient 同步
    - _Requirements: 6.1, 6.2, 6.3_

  - [x] 4.2 在 runSDKOutputLoop 中集成 StallDetector 和 CompletionAnalyzer
    - `case "assistant"` 分支：调用 `m.stallDetector.StartMonitoring(s.ID, s.Exec, s.Tool)`
    - `case "result"` 分支：调用 `m.stallDetector.StopMonitoring(s.ID)`，然后调用 `m.completionAnalyzer.Analyze` 并更新 `s.CompletionLevel`
    - `case chunk` 分支：调用 `m.stallDetector.ResetTimer(s.ID, len(text) > 0)`
    - 完成度分析结果同步到 Hub（更新 SessionSummary）
    - _Requirements: 1.1, 1.4, 1.5, 4.1, 6.2_

  - [x] 4.3 在 runGeminiACPOutputLoop 中集成 StallDetector 和 CompletionAnalyzer
    - 检测到 `❯ ` 前缀（busy）时：调用 `m.stallDetector.StartMonitoring`
    - 检测到 `[gemini-acp] turn complete:` 时：调用 `m.stallDetector.StopMonitoring`，然后调用 `m.completionAnalyzer.Analyze` 并更新 `s.CompletionLevel`
    - 每次收到新 chunk：调用 `m.stallDetector.ResetTimer(s.ID, true)`
    - _Requirements: 1.1, 1.4, 1.5, 4.1_

  - [x] 4.4 在 runExitLoop 中集成 StallDetector 清理
    - 在 `runExitLoop` 的 defer 中调用 `m.stallDetector.StopMonitoring(s.ID)` 确保会话退出时释放资源
    - _Requirements: 7.2_

  - [ ]* 4.5 编写 property-based 测试（Property 11: Lifecycle Correctness）
    - 生成随机 session 状态转换序列（starting → running → busy → waiting_input → exited）
    - 验证：仅在 busy 状态时有活跃监控，其他状态无监控
    - **Validates: Requirements 1.4, 7.1, 7.2, 7.3**

  - [ ]* 4.6 编写 property-based 测试（Property 10: Hub Sync on State Changes）
    - 生成随机 stall state 转换序列
    - 验证：每次转换都触发 SendSessionSummary 调用，SuggestedAction 字段正确
    - **Validates: Requirements 6.1, 6.2, 6.3**

- [x] 5. Checkpoint — 确保集成测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. 增强 toolGetSessionOutput 的 Session Hint 逻辑
  - [x] 6.1 修改 im_message_handler.go 中 toolGetSessionOutput 的 busy 状态 hint
    - 读取 `session.StallState`，根据 StallStateNormal/StallStateSuspected/StallStateStuck 返回不同提示文本
    - StallStateNormal: "编程工具正在工作中，请等待后再检查进度"
    - StallStateSuspected: "编程工具输出暂停，系统正在尝试恢复，请稍后再检查"
    - StallStateStuck: "编程工具可能已卡住，建议发送具体指令或终止会话"
    - _Requirements: 5.1, 5.2, 5.3_

  - [x] 6.2 修改 im_message_handler.go 中 toolGetSessionOutput 的 waiting_input 状态 hint
    - 读取 `session.CompletionLevel`，根据 CompletionCompleted/CompletionIncomplete/CompletionUncertain 返回不同提示文本
    - CompletionCompleted: "任务似乎已完成，可以查看结果"
    - CompletionIncomplete: "任务似乎未完成，建议发送「继续」让编程工具继续工作"
    - CompletionUncertain: 保持现有默认提示（"会话正在等待用户输入"）
    - 确保不影响 exited、error、starting、running 状态的现有 hint 逻辑
    - _Requirements: 5.4, 5.5, 5.6_

  - [x] 6.3 修改 toolSendAndObserve 中的 busy 状态 hint
    - 与 toolGetSessionOutput 保持一致，使用 StallState 生成更精确的 busy hint
    - _Requirements: 5.1, 5.2, 5.3_

  - [ ]* 6.4 编写 property-based 测试（Property 8: Session Hint Mapping Correctness）
    - 生成随机 (status, stallState, completionLevel) 组合
    - 验证：toolGetSessionOutput 返回的 hint 文本匹配预期映射
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5**

  - [ ]* 6.5 编写 property-based 测试（Property 9: Existing Hint Preservation）
    - 生成随机 session，状态为 starting/running/exited/error
    - 验证：hint 输出与未添加 stall detection 前完全一致
    - **Validates: Requirements 5.6**

  - [ ]* 6.6 编写 property-based 测试（Property 6: Nudge Transparency）
    - 生成随机 session 输出行，混入 nudge 文本
    - 验证：toolGetSessionOutput 返回中不包含 nudge 文本
    - **Validates: Requirements 2.6**

- [x] 7. 集成测试与端到端验证
  - [ ]* 7.1 编写完整 stall → nudge → recovery 集成测试
    - 创建 mock session → 进入 busy → 无输出超时 → 验证 nudge 发送 → 模拟输出恢复 → 验证状态清除
    - _Requirements: 1.2, 2.1, 2.5, 6.3_

  - [ ]* 7.2 编写完整 stall → stuck 集成测试
    - 创建 mock session → 连续 3 次 nudge 无效 → 验证 StallStateStuck → 验证 hint 正确
    - _Requirements: 2.4, 5.3_

  - [ ]* 7.3 编写 completion analysis 集成测试
    - 创建 mock session → 进入 busy → 产生输出 → 转为 waiting_input → 验证 CompletionLevel 正确 → 验证 hint 正确
    - _Requirements: 4.1, 5.4, 5.5_

- [x] 8. Final checkpoint — 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 所有 property-based 测试使用 `github.com/leanovate/gopter` 库，每个 property 至少运行 100 次迭代
- StallDetector 不对 Codex（one-shot 模式）会话进行 nudge
- CompletionAnalyzer 是纯函数，不调用 LLM，零延迟零成本
- Nudge echo 过滤为防御性措施，当前 SDK/ACP 协议不会回显 nudge 文本
- 实现语言：Go
