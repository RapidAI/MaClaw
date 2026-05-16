# Implementation Plan: GUI 启动与消息响应体验优化

## Overview

本实现计划将设计文档中的优化方案转化为可执行的编码任务。核心策略：先构建缓存和异步基础组件（ToolVersionCache、CachedSkillScanner），再重构启动流程和消息处理管道，最后添加前端进度指示器。每个任务聚焦单一组件，通过 property-based tests 验证正确性。

## Tasks

- [x] 1. 实现 ToolVersionCache 组件
  - [x] 1.1 创建 `gui/tool_version_cache.go`，实现 ToolVersionCache 结构体
    - 实现 `sync.Map` 存储 `cachedVersion` 条目（Version, CheckedAt, Installed, Path）
    - 实现 `GetCached(name string) *cachedVersion` 方法
    - 实现 `GetInstallStatus(name string) (installed bool, path string)` 方法，仅使用 `exec.LookPath` / file stat
    - 实现 `GetCachedToolNames() []string` 方法，返回已缓存的工具名列表
    - 实现 `RefreshAllAsync(tools []string, timeout time.Duration)` 方法，并行 goroutine 检测版本
    - 实现 JSON 持久化到 `~/.maclaw/data/tool_version_cache.json`（加载/保存）
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [x]* 1.2 Write property test for ToolVersionCache — Property 4: Hello without external process execution
    - **Property 4: Hello payload without external process execution**
    - **Validates: Requirements 8.1, 8.2, 8.3**
    - 使用 `rapid` 生成随机工具名列表，验证 `GetCachedToolNames` 和 `GetInstallStatus` 不调用 `exec.Command.Output()`

  - [x]* 1.3 Write unit tests for ToolVersionCache
    - 测试 `GetCached` 返回 nil 当无缓存时
    - 测试 `RefreshAllAsync` 并行执行且遵守 10s 超时
    - 测试 JSON 持久化 round-trip
    - 测试 `GetInstallStatus` 仅使用 LookPath
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

- [x] 2. 实现 CachedSkillScanner 组件
  - [x] 2.1 创建 `gui/cached_skill_scanner.go`，实现 CachedSkillScanner 结构体
    - 实现 `atomic.Pointer[skillCacheEntry]` 缓存（skills + createdAt + stale）
    - 实现 `Init(roots []string)` 方法，记录目录并启动后台扫描，<50ms 返回
    - 实现 `Get() []corelib.NLSkillEntry` 方法，返回缓存或空列表
    - 实现 30s TTL 检查，过期时返回 stale 数据并触发后台刷新
    - 实现 `Invalidate()` 方法，标记缓存失效并触发后台刷新
    - 实现 `sync.Once` / mutex 防止并发扫描
    - 实现扫描错误容忍（跳过错误目录，继续扫描其余目录）
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10_

  - [x]* 2.2 Write property test — Property 5: Skill cache TTL correctness
    - **Property 5: Skill cache TTL correctness**
    - **Validates: Requirements 2.4, 2.5**
    - 使用 `rapid` 生成随机 skill 列表，验证 30s 内 `Get()` 返回相同结果且不触发新扫描

  - [x]* 2.3 Write property test — Property 6: Stale-while-revalidate pattern
    - **Property 6: Stale-while-revalidate pattern**
    - **Validates: Requirements 2.6**
    - 验证过期缓存调用 `Get()` 立即返回 stale 数据且触发后台刷新

  - [x]* 2.4 Write property test — Property 7: Scan error resilience
    - **Property 7: Scan error resilience**
    - **Validates: Requirements 2.8**
    - 使用 `rapid` 生成包含错误目录的目录列表，验证跳过错误目录并返回有效结果

  - [x]* 2.5 Write property test — Property 8: Scan deduplication
    - **Property 8: Scan deduplication**
    - **Validates: Requirements 2.9**
    - 使用 `rapid` 生成并发调用模式，验证同时只有一个扫描操作执行

  - [x]* 2.6 Write property test — Property 9: Graceful degradation during scan
    - **Property 9: Graceful degradation during scan**
    - **Validates: Requirements 2.3, 2.10**
    - 验证扫描进行中且无缓存时返回空列表

- [x] 3. Checkpoint - 基础组件验证
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. 重构 startup() 为非阻塞模型
  - [x] 4.1 修改 `gui/app.go` 中的 `startup()` 函数
    - 将 `ensureInteractionInfra` + `ensureIMHandler` 保持同步执行（快速，无网络 I/O）
    - 在 Hub Connect 之前调用 `markAIAssistantReady()`
    - 将 Hub Connect 移到后台 goroutine（`go a.asyncHubConnect()`）
    - 无 Hub 凭据时直接 `markAIAssistantReady()` 不尝试连接
    - 保持 `initEarlyClassifier()` 在 return 前同步执行
    - _Requirements: 1.1, 1.2, 1.3, 1.5, 1.7_

  - [x] 4.2 实现 `asyncHubConnect()` 后台连接方法
    - 在后台 goroutine 中执行 WebSocket dial + sendMachineAuth + 读取 auth response
    - Auth 成功后 emit `"ai-assistant-init-progress"` with `"ready"`
    - Auth 失败/超时（>10s）后标记降级模式，本地功能正常
    - Auth 成功后在独立 goroutine 中执行 `sendMachineHelloLocked()`
    - _Requirements: 1.2, 1.4, 1.5, 1.6_

  - [x]* 4.3 Write property test — Property 1: Non-blocking startup
    - **Property 1: Non-blocking startup**
    - **Validates: Requirements 1.2, 1.5**
    - 使用 `rapid` 生成随机 Hub 配置，验证 `startup()` 返回时间 < Hub auth 完成时间

  - [x]* 4.4 Write property test — Property 2: Missing credentials immediate ready
    - **Property 2: Missing credentials immediate ready**
    - **Validates: Requirements 1.3**
    - 使用 `rapid` 生成凭据组合（至少一个为空），验证同步调用 `markAIAssistantReady()`

  - [x]* 4.5 Write property test — Property 3: Hub failure graceful degradation
    - **Property 3: Hub failure graceful degradation**
    - **Validates: Requirements 1.6**
    - 使用 `rapid` 生成失败类型（dial error, auth rejection, timeout），验证降级模式

- [x] 5. 优化 sendMachineHelloLocked
  - [x] 5.1 修改 `gui/remote_hub_client.go` 中的 `sendMachineHelloLocked()`
    - 移除 `listRemoteToolMetadataForApp` 调用（它执行外部进程）
    - 改为使用 `ToolVersionCache.GetCachedToolNames()` 获取工具名列表
    - 发送 hello 后异步调用 `toolVersionCache.RefreshAllAsync(toolNames, 10*time.Second)`
    - 确保函数在 500ms 内完成（无外部进程执行）
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [x] 5.2 集成 CachedSkillScanner 到 tool router
    - 修改 `corelib/tool/router.go`，在 Skill 列表获取处使用 CachedSkillScanner
    - 在 Skill 安装/删除时调用 `Invalidate()`
    - _Requirements: 2.4, 2.5, 2.7_

- [x] 6. Checkpoint - 启动流程验证
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. 实现消息处理管道优化
  - [x] 7.1 实现立即进度反馈 — 修改 `gui/im_message_handler.go`
    - 在 `handleIMMessageWithLoop` 入口处（preflight/entry_context 之前）调用 `onProgress("正在思考...")`
    - 确保 progress callback 在消息接收后 <100ms 内被调用
    - _Requirements: 3.1, 3.2, 3.3_

  - [x]* 7.2 Write property test — Property 10: Immediate progress emission
    - **Property 10: Immediate progress emission**
    - **Validates: Requirements 3.1**
    - 使用 `rapid` 生成随机消息，验证 progress callback 在 preflight 之前被调用

  - [x] 7.3 缩短 UIC tree channel 超时 — 修改 `gui/im_message_handler_workflow.go`
    - 将 UIC tree channel 超时从 3s 减少到 1.5s
    - 超时时返回 label="ambiguous", confidence=0.0, Degraded=true
    - _Requirements: 4.1, 4.2_

  - [x]* 7.4 Write property test — Property 11: UIC timeout degradation
    - **Property 11: UIC timeout degradation**
    - **Validates: Requirements 4.2**
    - 使用 `rapid` 生成慢分类场景，验证超时后返回 degraded result

  - [x] 7.5 实现短消息 UIC fusion 跳过
    - 修改 `gui/im_message_handler_workflow.go` 中的 `resolveIMEntryContext`
    - 当消息 < 10 runes（`utf8.RuneCountInString` after trim）时跳过 L2 embedding + L3 tree
    - 保留 L1 keyword matching 用于快速路径意图检测
    - 添加 DEBUG 级别日志记录跳过原因
    - _Requirements: 4.3, 4.4, 4.5_

  - [x]* 7.6 Write property test — Property 12: Short message fusion skip with L1 preservation
    - **Property 12: Short message fusion skip with L1 preservation**
    - **Validates: Requirements 4.3, 4.4**
    - 使用 `rapid` 生成 <10 rune 的消息，验证跳过 fusion 但保留 L1 keyword matching

- [x] 8. 实现 Task-Context LLM 优化
  - [x] 8.1 修改 `resolveIMEntryContext` 中的 Task-Context 逻辑
    - 当 `len(history) < 5` 时跳过 Task_Context_LLM 调用，默认 TaskNew
    - 当 `len(history) >= 5` 时并行执行 Task-Context LLM 和 system prompt 构建
    - Task-Context LLM 超时从 8s 减少到 2s
    - 超时或错误时默认 TaskContinue
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x]* 8.2 Write property test — Property 13: Task-Context skip for short history
    - **Property 13: Task-Context skip for short history**
    - **Validates: Requirements 5.1**
    - 使用 `rapid` 生成 <5 entries 的历史，验证不调用 LLM 且默认 TaskNew

  - [x]* 8.3 Write property test — Property 14: Task-Context failure fallback
    - **Property 14: Task-Context failure fallback**
    - **Validates: Requirements 5.4**
    - 使用 `rapid` 生成超时/错误场景，验证默认 TaskContinue

- [x] 9. Checkpoint - 消息处理管道验证
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. 实现消息处理独立于 Hub
  - [x] 10.1 确保 IMMessageHandler 不依赖 Send_Hello 完成
    - 验证 `handleIMMessageWithLoop` 在 `warmupDone=true` 时不检查 Hub 连接状态
    - 确保 `sendMachineHelloLocked` 在 `markAIAssistantReady` 之后的独立 goroutine 中执行
    - 确保 Send_Hello 执行路径与 IMMessageHandler 消息处理路径无共享 mutex
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [x]* 10.2 Write property test — Property 15: Message processing independence from Hub
    - **Property 15: Message processing independence from Hub**
    - **Validates: Requirements 6.1, 6.2, 6.5, 6.6**
    - 使用 `rapid` 生成各种 Hub 状态（未连接/连接中/已连接/失败），验证消息处理不受影响

- [x] 11. 实现前端进度指示器
  - [x] 11.1 更新前端组件显示即时进度
    - 在消息提交后立即显示 "正在思考..." 状态指示器
    - 第一个 LLM token 到达时替换进度指示器为流式内容
    - 错误发生时替换为错误消息
    - 120s 超时时替换为超时消息
    - 处理 `"ai-assistant-init-progress"` 事件（connecting/ready/degraded 状态）
    - _Requirements: 3.2, 3.3, 3.4, 3.5, 3.6, 7.1_

- [x] 12. Final checkpoint - 端到端验证
  - Ensure all tests pass, ask the user if questions arise.
  - 验证启动时间 <3s（本地配置可读、无同步网络 I/O）
  - 验证进度指示器在消息提交后 <200ms 显示
  - 验证 tool routing 在 warm cache 下 <10ms

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using `rapid` (pgregory.net/rapid)
- Unit tests validate specific examples and edge cases
- All Go code should follow existing project conventions (see `corelib/skill/skill_5patterns_test.go` for rapid usage patterns)
- ToolVersionCache 持久化路径: `~/.maclaw/data/tool_version_cache.json`
- CachedSkillScanner 无持久化，每次启动后台扫描

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "2.2", "2.3", "2.4", "2.5", "2.6"] },
    { "id": 2, "tasks": ["4.1", "4.2", "5.1", "5.2"] },
    { "id": 3, "tasks": ["4.3", "4.4", "4.5"] },
    { "id": 4, "tasks": ["7.1", "7.3", "7.5", "8.1", "10.1"] },
    { "id": 5, "tasks": ["7.2", "7.4", "7.6", "8.2", "8.3", "10.2"] },
    { "id": 6, "tasks": ["11.1"] }
  ]
}
```
