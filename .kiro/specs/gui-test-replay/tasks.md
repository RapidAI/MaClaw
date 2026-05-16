# Implementation Plan: GUI Test Replay

## Overview

基于设计文档，按依赖关系分层实现：先修复多显示器截图基础设施，再构建 Accessibility Bridge 和 Input Simulator 平台层，然后实现 Image Matcher 和 Element Locator 定位层，最后构建 GUI Recorder/Replayer/TaskSupervisor 编排层和工具注册。所有代码使用 Go 语言，平台特定实现通过 build tags 隔离。

## Tasks

- [x] 1. 多显示器截图引擎（Screenshot Engine）
  - [x] 1.1 定义 DisplayInfo 结构体和 EnumDisplays 接口
    - 在 `corelib/remote/screenshot_multimon.go` 中定义 `DisplayInfo` 结构体（Index, Name, X, Y, Width, Height, Scale, Primary）
    - 定义 `EnumDisplays() ([]DisplayInfo, error)` 函数签名
    - 定义 `BuildMultiMonitorScreenshotCommand() string` 和 `BuildSingleMonitorScreenshotCommand(screenIndex int) string`
    - _Requirements: 1.1, 1.2, 1.3_

  - [x] 1.2 实现 Windows 多显示器枚举和截图
    - 在 `corelib/remote/screenshot_multimon_windows.go` 中实现 `EnumDisplays`，使用 `EnumDisplayMonitors` + `GetMonitorInfo`
    - 修改 `buildWindowsScreenshotCommand()` 使用虚拟桌面坐标（`SM_XVIRTUALSCREEN` 等）替代 `PrimaryScreen.Bounds`
    - 实现 `BuildSingleMonitorScreenshotCommand` 按 screen_index 截取单个显示器
    - 保留现有 5 级降级链，每级使用虚拟桌面坐标
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2_

  - [x] 1.3 实现 macOS 多显示器枚举和截图
    - 在 `corelib/remote/screenshot_multimon_darwin.go` 中实现 `EnumDisplays`，使用 `CGGetActiveDisplayList` + `CGDisplayBounds`
    - 实现多显示器拼接截图（`CGWindowListCreateImage(CGRectInfinite, ...)`）
    - 实现单显示器截图（`CGDisplayCreateImage(displayID)`）
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2_

  - [x] 1.4 实现 Linux 多显示器枚举和截图
    - 在 `corelib/remote/screenshot_multimon_linux.go` 中实现 `EnumDisplays`，X11 使用 `XRRGetScreenResources`，Wayland 使用 `wlr-screencopy`
    - 实现多显示器拼接（`scrot` 或 `grim` 默认截取整个虚拟桌面）
    - 实现单显示器截图（`grim -o <output>` 指定输出）
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 2.2_

  - [x] 1.5 实现 screen_index 越界错误和降级逻辑
    - 当 `screen_index >= len(displays)` 时返回包含实际显示器数量的错误信息
    - 多显示器拼接失败时降级为主显示器截图，返回结果附带 `degraded: true`
    - _Requirements: 1.4, 1.5_

  - [ ]* 1.6 编写多显示器截图属性测试
    - **Property 1: Multi-monitor stitched screenshot dimensions** — 使用 mock DisplayInfo 验证拼接截图尺寸等于所有显示器的虚拟桌面边界框
    - **Property 2: Single monitor screenshot selection** — 验证单显示器截图尺寸匹配指定显示器
    - **Property 3: Out-of-range screen index error** — 验证越界索引返回包含实际数量的错误
    - **Validates: Requirements 1.1, 1.3, 1.4**

- [x] 2. Checkpoint — 确保截图引擎测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 3. Accessibility Bridge 跨平台桥接层
  - [x] 3.1 定义 Accessibility Bridge 接口和数据类型
    - 创建 `corelib/accessibility/bridge.go`，定义 `Element` 结构体（Role, Name, Value, Bounds, Children, Handle）
    - 定义 `Rect` 结构体和 `Bridge` 接口（EnumElements, FindElement, ClickElement, TypeInElement, GetValue, Close）
    - 定义 `NewBridge() Bridge` 工厂函数签名
    - _Requirements: 3.1, 3.2, 3.7, 3.8_

  - [x] 3.2 实现 Windows Accessibility Bridge
    - 创建 `corelib/accessibility/bridge_windows.go`，通过 UI Automation COM 接口（`IUIAutomation`, `IUIAutomationElement`）实现 Bridge
    - 使用 `go-ole` 或 syscall 调用 COM 接口
    - 实现 EnumElements（遍历控件树）、FindElement（按 role+name 查找）、ClickElement/TypeInElement（通过 UIA Pattern 执行操作）
    - 目标应用未暴露无障碍信息时返回空控件树而非错误
    - _Requirements: 3.1, 3.2, 3.3, 3.6, 3.7, 3.8_

  - [x] 3.3 实现 macOS Accessibility Bridge
    - 创建 `corelib/accessibility/bridge_darwin.go`，通过 CGo 调用 `AXUIElementCreateApplication` → `AXUIElementCopyAttributeValue`
    - 实现 EnumElements、FindElement、ClickElement/TypeInElement
    - 处理辅助功能权限不足的情况，返回明确的权限错误提示
    - _Requirements: 3.1, 3.2, 3.4, 3.6, 3.7, 3.8_

  - [x] 3.4 实现 Linux Accessibility Bridge
    - 创建 `corelib/accessibility/bridge_linux.go`，通过 `godbus/dbus` 包调用 AT-SPI D-Bus 接口（`org.a11y.atspi`）
    - 实现 EnumElements、FindElement、ClickElement/TypeInElement
    - _Requirements: 3.1, 3.2, 3.5, 3.6, 3.7, 3.8_

  - [ ]* 3.5 编写 Accessibility Bridge 属性测试
    - **Property 4: Accessibility element invariants** — 验证返回的 Element 有非空 Role 且 Bounds 的 Width/Height > 0
    - **Property 5: Accessibility find consistency** — 验证 FindElement(role, name) 返回匹配的元素
    - 使用 mock Bridge 实现进行测试
    - **Validates: Requirements 3.2, 3.7**

- [x] 4. 跨平台输入模拟器（Input Simulator）
  - [x] 4.1 定义 InputSimulator 接口
    - 创建 `corelib/guiautomation/input.go`，定义 `InputSimulator` 接口（Click, RightClick, DoubleClick, Type, KeyCombo, Scroll, DragDrop）
    - 定义 `NewInputSimulator() InputSimulator` 工厂函数签名
    - _Requirements: 10.1, 10.2, 10.3, 10.4_

  - [x] 4.2 实现 Windows Input Simulator
    - 创建 `corelib/guiautomation/input_windows.go`，通过 `SendInput` API (user32.dll) 实现鼠标点击、键盘输入、滚轮滚动、拖拽
    - 处理坐标超出屏幕范围的情况（裁剪到最近边界）
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

  - [x] 4.3 实现 macOS Input Simulator
    - 创建 `corelib/guiautomation/input_darwin.go`，通过 CGo 调用 `CGEventCreateMouseEvent` / `CGEventCreateKeyboardEvent`
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.6_

  - [x] 4.4 实现 Linux Input Simulator
    - 创建 `corelib/guiautomation/input_linux.go`，通过 XTest 扩展（`XTestFakeMotionEvent`, `XTestFakeButtonEvent`, `XTestFakeKeyEvent`）实现
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.7_

- [x] 5. Checkpoint — 确保平台层（Accessibility + Input）编译通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. Image Matcher 图像匹配定位器
  - [x] 6.1 实现 ImageMatcher 核心
    - 创建 `corelib/guiautomation/image_matcher.go`，定义 `MatchResult` 结构体和 `ImageMatcher` 结构体
    - 实现 `FindByImage`：滑动窗口 + NCC（Normalized Cross-Correlation）像素差异比较，不引入 OpenCV
    - 实现 `FindByText`：通过 `OCRProvider` 识别屏幕文字，返回包含目标文本的区域坐标
    - 置信度低于 0.6 时报告匹配失败（`Found: false`）
    - 支持 `searchRegion` 参数限定搜索范围
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

  - [ ]* 6.2 编写 Image Matcher 属性测试
    - **Property 12: Image matching with confidence threshold** — 验证已知位置的参考图片能被正确找到且置信度 ≥ 0.6，低置信度结果 Found=false
    - **Property 13: OCR text location** — 验证 FindByText 返回的坐标在 OCR 结果的 bounding box 内
    - **Validates: Requirements 7.1, 7.2, 7.3, 7.4**

- [x] 7. Element Locator 统一定位入口
  - [x] 7.1 实现 ElementLocator 三层降级定位
    - 创建 `corelib/guiautomation/locator.go`，定义 `LocateStrategy`、`LocateResult`、`ElementLocator` 结构体
    - 实现 `Locate(step GUIRecordedStep) (*LocateResult, error)`：先尝试 Accessibility Bridge，失败则尝试 ImageMatcher，最后降级到坐标
    - 每层尝试失败时记录日志，继续下一层
    - _Requirements: 5.2, 5.3, 5.4, 5.5_

  - [ ]* 7.2 编写 ElementLocator 属性测试
    - **Property 7: Locator priority ordering** — 验证三层定位策略的优先级：accessibility > image > coordinate
    - 使用 mock Bridge 和 mock ImageMatcher 控制各层的成功/失败
    - **Validates: Requirements 5.2**

- [ ] 8. GUI 数据模型和 JSON 序列化
  - [x] 8.1 定义 GUI 自动化数据类型
    - 创建 `corelib/guiautomation/types.go`，定义 `GUIRecordedFlow`、`GUIRecordedStep`、`AccessibilityRef`、`GUICriterionSpec`、`GUITaskSpec`、`GUIStepSpec`、`GUITaskState`、`GUICheckpoint` 等结构体
    - 按设计文档中的 Data Models 部分实现所有字段和 JSON tag
    - _Requirements: 4.2, 4.3, 8.1_

  - [x] 8.2 实现 GUIRecordedFlow 的 JSON 序列化与反序列化
    - 实现 `SaveFlow(flow *GUIRecordedFlow, dir string) error`：将 flow 保存为格式化 JSON（带缩进），截图快照保存为外部 PNG 文件（`snapshots/step_NNN.png`），JSON 中仅存相对路径引用
    - 实现 `LoadFlow(dir string, name string) (*GUIRecordedFlow, error)`：加载 JSON 并验证快照文件存在
    - _Requirements: 8.1, 8.2, 8.4_

  - [ ]* 8.3 编写 JSON round-trip 属性测试
    - **Property 14: Snapshot external storage** — 验证保存的 JSON 不含 base64 图片数据，snapshot_ref 指向存在的文件
    - **Property 15: Flow JSON round-trip** — 验证序列化再反序列化产生等价对象
    - **Validates: Requirements 8.2, 8.3**

- [x] 9. Checkpoint — 确保定位层和数据模型测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. GUI Recorder 录制器
  - [x] 10.1 实现 GUIRecorder 核心逻辑
    - 创建 `corelib/guiautomation/recorder.go`，实现 `GUIRecorder` 结构体
    - 实现 `Start() error`：进入录制模式，初始化步骤列表
    - 实现 `RecordStep(action, windowTitle string, coords [2]int, text string) error`：同时记录三种定位信息（Accessibility 控件标识、截图快照、屏幕坐标），Accessibility 获取失败时留空不中断，截图失败时留空不中断
    - 实现 `Stop(name, description string) (*GUIRecordedFlow, error)`：序列化为 JSON 保存到 `~/.maclaw/gui_flows/`
    - 实现 `ListFlows()` 和 `LoadFlow(name)`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6_

  - [ ]* 10.2 编写 GUIRecorder 属性测试
    - **Property 6: Recorded step completeness** — 验证录制的每个步骤有非空 Action、非负 Timestamp、非空 WindowTitle、有效 Coords
    - 使用 mock Accessibility Bridge 和 mock Screenshot 函数
    - **Validates: Requirements 4.2, 4.3**

- [x] 11. GUI Task Supervisor 任务监督器
  - [x] 11.1 实现 GUITaskSupervisor
    - 创建 `corelib/guiautomation/supervisor.go`，实现 `GUITaskSupervisor` 结构体
    - 复用 `browser.BrowserTaskSupervisor` 的暂停/恢复/取消模式（通过 channel 信号）
    - 实现 `Execute(spec GUITaskSpec) (*GUITaskState, error)`：逐步执行，每步通过 ElementLocator 定位 + InputSimulator 执行操作
    - 实现 `Pause`、`Resume`、`Cancel` 方法
    - 实现 `GetState` 方法
    - 每步执行后记录 GUICheckpoint（截图 + 控件状态 + 使用的定位策略）
    - _Requirements: 6.1, 6.4, 6.5_

  - [x] 11.2 实现 GUIRetryStrategy
    - 创建 `corelib/guiautomation/retry_strategy.go`
    - 最多 3 次重试；元素未找到时增加等待时间；超时时延长超时限制
    - 重试 2 次仍失败时构建 LLM 上下文（截图 + OCR 文本 + 失败信息）请求适配建议
    - _Requirements: 6.1, 6.2, 6.3_

  - [ ]* 11.3 编写 Supervisor 属性测试
    - **Property 9: Retry strategy correctness** — 验证重试策略：count < 3 允许重试，count ≥ 3 拒绝，element-not-found 增加等待，timeout 延长超时
    - **Property 10: Task state transitions** — 验证 Pause→paused, Resume→running, Cancel→cancelled 状态转换
    - **Property 11: Checkpoint recording** — 验证 N 步任务完成后有 N 个检查点，每个有有效 StepIndex、非零 Timestamp、非空 Strategy
    - **Validates: Requirements 6.1, 6.2, 6.4, 6.5**

- [x] 12. GUI Replayer 重放器
  - [x] 12.1 实现 GUIReplayer
    - 创建 `corelib/guiautomation/replayer.go`，实现 `GUIReplayer` 结构体
    - 实现 `Replay(flow *GUIRecordedFlow, overrides map[string]string) (*GUITaskState, error)`：将 flow 转换为 GUITaskSpec，通过 GUITaskSupervisor 执行
    - 实现 `flowToTaskSpec`：将 GUIRecordedStep 转换为 GUIStepSpec，应用 overrides 参数替换
    - 每步执行后等待 UI 稳定（无动画、无加载指示器）
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7_

  - [ ]* 12.2 编写 Replayer 属性测试
    - **Property 8: Override substitution in replay** — 验证 overrides map 中的值正确替换到对应步骤的文本字段
    - **Validates: Requirements 5.6**

- [x] 13. Checkpoint — 确保录制/重放/监督器测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [x] 14. GUI 测试工具注册
  - [x] 14.1 注册 GUI 测试工具到 Tool Registry
    - 创建 `corelib/guiautomation/tools.go`，实现 `RegisterTools(registry *tool.Registry)` 函数
    - 注册 `gui_record_start`、`gui_record_stop`、`gui_replay`、`gui_list_flows` 工具，每个工具提供中英文描述
    - 注册 `gui_click`、`gui_type`、`gui_screenshot` 原子操作工具
    - 所有工具 Tags 包含 `gui`、`test`、`automation`、`桌面`、`录制`
    - _Requirements: 9.1, 9.2, 9.4_

  - [x] 14.2 添加 GUI 关键词到工具路由
    - 在 `corelib/tool/builder.go` 的 `GroupKeywords` 中添加 GUI 相关关键词映射（"gui"、"桌面"、"录制"、"自动化" → `["gui", "test", "automation", "desktop"]`）
    - 确保 `DetectGroupTags` 能将 GUI 相关用户消息路由到 GUI 测试工具
    - _Requirements: 9.3_

  - [ ]* 14.3 编写工具注册属性测试
    - **Property 16: Tool registration completeness** — 验证每个注册的 GUI 工具有非空 Description 且 Tags 包含 "gui" 和 "test"
    - **Property 17: GUI tool routing by keywords** — 验证 GUI 相关关键词触发正确的 tag 匹配
    - **Validates: Requirements 9.2, 9.3**

- [x] 15. 集成接线：将 GUI 工具注册到应用启动流程
  - [x] 15.1 在 GUI 应用中初始化和注册 GUI 自动化组件
    - 在 `gui/tools_browser.go` 或新建 `gui/tools_gui_automation.go` 中，初始化 Accessibility Bridge、InputSimulator、ImageMatcher、ElementLocator、GUIRecorder、GUIReplayer、GUITaskSupervisor
    - 调用 `guiautomation.RegisterTools(registry)` 将 GUI 工具注册到全局 tool registry
    - 确保 GUI 工具与现有浏览器工具共存，不冲突
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [x] 15.2 在 TUI 中注册 GUI 自动化工具
    - 在 `tui/agent_tools.go` 中添加 GUI 自动化工具的注册调用
    - _Requirements: 9.1_

- [x] 16. Final Checkpoint — 确保所有测试通过，完整集成验证
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 平台特定实现（Windows/macOS/Linux）通过 Go build tags 隔离，每个平台独立编译
- 复用现有 `browser.OCRProvider`（RapidOCRSidecar）作为 OCR 引擎，不引入新依赖
- 复用现有 `browser.BrowserTaskSupervisor` 的暂停/恢复/重试模式，GUI 版本通过组合复用
- Property tests 使用 `testing/quick` 或 `pgregory.net/rapid` 库
- 集成测试标记为 `//go:build integration`，不在 CI 中自动运行
