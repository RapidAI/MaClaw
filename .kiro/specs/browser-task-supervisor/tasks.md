# Implementation Plan: 浏览器任务监管与 OCR 集成

## Overview

为 MaClaw 新增 BrowserTaskSupervisor 监管层，提供浏览器任务的长程监管、成功验证、智能重试、RapidOCR 页面感知和操作录制回放能力。实现分 4 个 Phase，每个 Phase 可独立交付使用。

## Tasks

### Phase 1: 核心监管 + 任务验证 + 重试

- [x] 1. 定义核心类型和数据结构
  - [x] 1.1 创建 `corelib/browser/task_types.go`，定义 TaskSpec、StepSpec、CriterionSpec、TaskState、Checkpoint、VerifyResult、CriterionResult、FailureType、RetryDecision 等类型
    - _Requirements: 1.1, 2.1, 3.1_

  - [x] 1.2 在 `corelib/agent/background_loop_manager.go` 中新增 `SlotKindBrowser` 常量（值 4），在 `NewBackgroundLoopManager` 的 slotLimits 中添加 `SlotKindBrowser: 2`
    - _Requirements: 1.1_

- [x] 2. 实现 TaskVerifier
  - [x] 2.1 创建 `corelib/browser/task_verifier.go`，实现 TaskVerifier struct
    - 实现 `Verify(criteria []CriterionSpec) (*VerifyResult, error)`
    - 实现 `WaitForStable(timeout time.Duration) error`（通过 CDP 检测网络空闲）
    - 支持 5 种验证类型: dom_exists, dom_text, url_contains, url_matches, ocr_contains
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [ ]* 2.2 编写 TaskVerifier 单元测试
    - 测试每种 CriterionSpec 类型的验证逻辑（mock browser.Session）
    - 测试所有标准通过 → verified
    - 测试部分标准失败 → 返回失败详情
    - _Requirements: 2.3, 2.4_

- [x] 3. 实现 RetryStrategy
  - [x] 3.1 创建 `corelib/browser/retry_strategy.go`，实现 RetryStrategy struct
    - 实现 `Decide(failure FailureType, step StepSpec, stepRetryCount int, pageState *PageSnapshot) *RetryDecision`
    - 实现 `ClassifyFailure(err error, step StepSpec) FailureType`
    - ElementNotFound: 递增等待时间（5s → 10s → LLM）
    - Timeout: 递增 timeout（×2 → ×3 → 失败）
    - PageChanged: 截图分析 → LLM 重新规划
    - UnknownState: 截图 + OCR → LLM 决策
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

  - [ ]* 3.2 编写 RetryStrategy 单元测试
    - 测试每种 FailureType 的重试决策
    - 测试超过最大重试次数后返回 ShouldRetry=false
    - _Requirements: 3.2, 3.3_

- [x] 4. 实现 BrowserTaskSupervisor 核心
  - [x] 4.1 创建 `corelib/browser/task_supervisor.go`，实现 BrowserTaskSupervisor struct
    - 实现 `NewBrowserTaskSupervisor`
    - 实现 `Execute(spec TaskSpec) (*TaskState, error)` — 在 BackgroundLoop 中逐步执行
    - 实现 `GetState(taskID string) (*TaskState, bool)`
    - 实现 `Cancel(taskID string) error`
    - 每步执行后创建 Checkpoint（截图 + URL + 标题）
    - 每步执行后调用 TaskVerifier（如果步骤有 verify）
    - 失败时调用 RetryStrategy 决策
    - 通过 StatusEvent 推送进度
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [ ]* 4.2 编写 BrowserTaskSupervisor 集成测试
    - 测试完整任务执行流程（mock browser.Session）
    - 测试步骤超时触发重试
    - 测试任务成功和失败路径
    - _Requirements: 1.2, 1.4, 1.6_

- [x] 5. 注册 Phase 1 工具
  - [x] 5.1 在 `corelib/browser/tools.go` 中注册 `browser_task_run`、`browser_task_status`、`browser_task_verify` 三个工具
    - browser_task_run: 接受 steps JSON + success_criteria JSON，调用 Supervisor.Execute
    - browser_task_status: 接受 task_id，调用 Supervisor.GetState
    - browser_task_verify: 接受 criteria JSON，调用 Supervisor.Verify
    - _Requirements: 8.1, 8.2_

- [x] 6. Checkpoint — Phase 1 核心监管可用
  - Ensure all tests pass, ask the user if questions arise.

### Phase 2: OCR 能力集成

- [x] 7. 定义 OCR 接口和类型
  - [x] 7.1 创建 `corelib/browser/ocr_provider.go`，定义 OCRProvider 接口、OCRResult struct、FormatForLLM 函数
    - _Requirements: 4.1, 4.7_

- [x] 8. 实现 RapidOCR Sidecar
  - [x] 8.1 创建 `corelib/browser/ocr_rapidocr.go`，实现 RapidOCRSidecar struct
    - 实现 `EnsureReady()` — 自动检测 python3/pip → pip install → 写入 ocr_server.py → 启动 sidecar
    - 安装目录: `~/.maclaw/ocr/`
    - 安装命令: `python3 -m pip install --user rapidocr-onnxruntime`
    - ocr_server.py 内容由 Go 代码内嵌字符串写入
    - 实现 `Recognize(pngBase64 string) ([]OCRResult, error)` — JSON-RPC 通信
    - 实现 `IsAvailable() bool` — ping 检查
    - 实现 `Close()` — 停止 sidecar
    - 空闲 5 分钟自动退出（idleTimer）
    - 安装过程通过 StatusEvent 通知 UI
    - _Requirements: 4.2, 4.3, 4.5_

  - [ ]* 8.2 编写 RapidOCRSidecar 单元测试
    - 测试安装检测逻辑（mock exec.Command）
    - 测试 sidecar 启动/停止
    - 测试 JSON 协议通信
    - 测试空闲超时自动退出
    - _Requirements: 4.3, 4.5_

- [x] 9. 实现 LLM Vision Provider
  - [x] 9.1 创建 `corelib/browser/ocr_llm_vision.go`，实现 LLMVisionProvider struct
    - 实现 `Recognize(pngBase64 string) ([]OCRResult, error)` — 发送截图给 LLM，解析返回的文本
    - _Requirements: 4.6_

- [x] 10. 实现 Composite OCR Provider
  - [x] 10.1 创建 `corelib/browser/ocr_composite.go`，实现 CompositeOCRProvider
    - Fallback 链: RapidOCR → LLM Vision → 空结果
    - _Requirements: 4.4_

  - [ ]* 10.2 编写 CompositeOCRProvider 单元测试
    - 测试第一个 provider 成功时直接返回
    - 测试第一个失败时 fallback 到第二个
    - 测试全部失败时返回空结果
    - _Requirements: 4.4_

- [x] 11. 注册 OCR 工具并集成到 Verifier
  - [x] 11.1 在 `corelib/browser/tools.go` 中注册 `browser_ocr` 工具
    - 首次调用时自动触发 RapidOCR 安装
    - 返回 OCR 结果的 FormatForLLM 格式
    - _Requirements: 8.1, 8.3_

  - [x] 11.2 将 OCRProvider 注入 TaskVerifier，使 `ocr_contains` 验证类型可用
    - _Requirements: 2.2_

- [x] 12. Checkpoint — Phase 2 OCR 能力可用
  - Ensure all tests pass, ask the user if questions arise.

### Phase 3: 操作录制与回放

- [x] 13. 实现 BrowserRecorder
  - [x] 13.1 创建 `corelib/browser/recorder.go`，实现 BrowserRecorder struct
    - 实现 `Start()` — 通过 CDP 事件监听开始录制
    - 实现 `Stop(name, description string) (*RecordedFlow, error)` — 停止录制并保存到 `~/.maclaw/browser_flows/<name>.json`
    - 实现 `ListFlows() ([]RecordedFlow, error)` — 列出所有 flow
    - 实现 `LoadFlow(name string) (*RecordedFlow, error)` — 加载指定 flow
    - 通过 CDP `DOM.getNodeForLocation` 从坐标反推 CSS selector
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 13.2 创建 `corelib/browser/flow_types.go`，定义 RecordedFlow、RecordedStep、RecordedSnapshot 类型
    - _Requirements: 5.2, 5.3_

  - [ ]* 13.3 编写 BrowserRecorder 单元测试
    - 测试 RecordedFlow JSON 序列化/反序列化
    - 测试 flow 文件的保存/加载/列出
    - _Requirements: 5.2, 5.3_

- [x] 14. 实现 FlowReplayer
  - [x] 14.1 创建 `corelib/browser/replayer.go`，实现 FlowReplayer struct
    - 实现 `Replay(flow *RecordedFlow, overrides map[string]string) (*TaskState, error)`
    - RecordedFlow → TaskSpec 转换逻辑
    - selector 失败时的 fallback 链: 坐标 → OCR/DOM → LLM 自适应
    - 每步执行后与录制快照对比，检测偏离
    - 偏离时发给 LLM 决策（跳过/调整/中止）
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [ ]* 14.2 编写 FlowReplayer 单元测试
    - 测试 RecordedFlow → TaskSpec 转换
    - 测试 selector fallback 逻辑
    - _Requirements: 6.1, 6.2_

- [x] 15. 注册录制回放工具
  - [x] 15.1 在 `corelib/browser/tools.go` 中注册 `browser_record_start`、`browser_record_stop`、`browser_task_replay` 三个工具
    - browser_record_start: 调用 Recorder.Start
    - browser_record_stop: 接受 name 参数，调用 Recorder.Stop
    - browser_task_replay: 接受 name 参数，调用 Replayer.Replay
    - _Requirements: 8.1, 8.2_

- [x] 16. Checkpoint — Phase 3 录制回放可用
  - Ensure all tests pass, ask the user if questions arise.

### Phase 4: GUI/TUI 集成

- [x] 17. GUI 集成
  - [x] 17.1 在 `gui/tools_browser.go` 中初始化 BrowserTaskSupervisor 并注入到 tool registry
    - 创建 CompositeOCRProvider（RapidOCR + LLMVision）
    - 创建 BrowserTaskSupervisor 并连接 BackgroundLoopManager
    - 创建 BrowserRecorder 和 FlowReplayer
    - _Requirements: 7.1, 7.4_

  - [ ]* 17.2 在 GUI 前端添加浏览器任务状态面板（可选，后续迭代）
    - 显示当前步骤、总步骤数、任务状态、最近截图预览、重试次数
    - _Requirements: 7.1, 7.3_

- [ ] 18. TUI 集成
  - [ ] 18.1 在 `tui/agent_tools.go` 中初始化 BrowserTaskSupervisor 并注入到 tool registry
    - 与 GUI 相同的初始化逻辑
    - _Requirements: 7.2, 7.4_

  - [ ]* 18.2 在 TUI 中添加 `browser` 命令组（可选，后续迭代）
    - `browser record` — 开始/停止录制
    - `browser replay <name>` — 回放 flow
    - `browser status` — 查看当前任务状态
    - `browser list-flows` — 列出所有 flow
    - _Requirements: 7.2_

- [x] 19. Final checkpoint — 全部功能可用（GUI 已集成，TUI 待后续迭代）
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Phase 1 是最小可用版本，Phase 2-4 可按需迭代
- OCR sidecar 安装目录: `~/.maclaw/ocr/`
- 录制 flow 存储目录: `~/.maclaw/browser_flows/`
- RapidOCR 安装命令: `python3 -m pip install --user rapidocr-onnxruntime`
- SlotKindBrowser 默认并发 2，可通过 `SetSlotLimit` 动态调整
- 所有新增工具使用 `tool.CategoryBuiltin` 类别和 `browser` tag
- 实现语言: Go（OCR sidecar 为 Python）
